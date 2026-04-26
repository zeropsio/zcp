package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/topology"
)

// Response is the data passed to RenderStatus. It carries the envelope plus
// the synthesised guidance and the typed plan. The MCP status tool builds
// this struct and hands it here; no other renderer produces status blocks.
type Response struct {
	Envelope StateEnvelope `json:"envelope"`
	Guidance []string      `json:"guidance,omitempty"`
	Plan     *Plan         `json:"plan,omitempty"`
}

// RenderStatus produces the markdown status block from a Response. Section
// order is stable: Phase → Services → Progress → Blockers → Guidance →
// Next. Each section is skipped when it has no content, keeping the
// output compact. Blockers is a one-line call-to-action surfaced above
// the (large) Guidance section so the auto-close gate is visible
// without scrolling past atoms.
func RenderStatus(resp Response) string {
	var b strings.Builder
	b.WriteString("## Status\n")

	renderPhase(&b, resp.Envelope)
	renderServices(&b, resp.Envelope)
	renderProgressAndBlockers(&b, resp.Envelope)
	renderGuidance(&b, resp.Guidance)
	renderPlan(&b, resp.Plan)

	return b.String()
}

// renderProgressAndBlockers renders the per-service Progress block (the
// deploy/verify status lines from the work session) and, if auto-close
// is blocked, a one-line call-to-action above the Guidance section. Both
// derive from the same pass over ws.Services so there is one source of
// truth per host.
//
// Guidance can be hundreds of lines of atoms; without the blockers line
// the agent easily scrolls past the bottom-of-output Next pointer.
// Surfacing it here puts the immediate next step right after Progress.
func renderProgressAndBlockers(b *strings.Builder, env StateEnvelope) {
	ws := env.WorkSession
	if ws == nil || ws.ClosedAt != nil || len(ws.Services) == 0 {
		return
	}
	hasActivity := len(ws.Deploys) > 0 || len(ws.Verifies) > 0

	type hostStatus struct {
		host       string
		deployText string
		verifyText string
		deployOK   bool
		verifyOK   bool
	}
	statuses := make([]hostStatus, len(ws.Services))
	var pending []string
	needsDeploy := false
	needsVerify := false

	for i, host := range ws.Services {
		deploys := ws.Deploys[host]
		verifies := ws.Verifies[host]
		st := hostStatus{host: host}
		st.deployText, st.deployOK = lastAttemptText(deploys, "deploy")
		st.verifyText, st.verifyOK = lastAttemptText(verifies, "verify")
		statuses[i] = st
		if st.deployOK && st.verifyOK {
			continue
		}
		pending = append(pending, host)
		if !st.deployOK {
			needsDeploy = true
		} else if !st.verifyOK {
			needsVerify = true
		}
	}

	if hasActivity {
		fmt.Fprintln(b, "Progress:")
		for _, st := range statuses {
			fmt.Fprintf(b, "  - %s: %s, %s\n", st.host, st.deployText, st.verifyText)
		}
	}
	if len(pending) == 0 {
		return
	}
	ready := len(ws.Services) - len(pending)
	fmt.Fprintf(b, "→ Auto-close blocked: %d/%d ready, pending %s. %s\n",
		ready, len(ws.Services),
		strings.Join(pending, ", "),
		blockerNextAction(pending[0], needsDeploy, needsVerify))
}

// lastAttemptText returns the human-readable "<kind> <state>" suffix for
// the last attempt of a host and whether that attempt succeeded. Shared
// between the Progress line rendering and the blocker-gate counters.
//
// On failure, the Reason from AttemptInfo (when populated) appears after
// the "<kind> failed" prefix so the LLM sees the actionable diagnosis
// without a separate logs round-trip. Phase 1 (C1) of the pipeline-repair
// plan: this is the surface that recovers the failed-deploy reason
// post-compaction.
func lastAttemptText(attempts []AttemptInfo, kind string) (string, bool) {
	if len(attempts) == 0 {
		return kind + " pending", false
	}
	last := attempts[len(attempts)-1]
	if last.Success {
		return kind + " ok", true
	}
	if last.Reason != "" {
		return kind + " failed: " + last.Reason, false
	}
	return kind + " failed", false
}

// blockerNextAction suggests the concrete tool call that clears the
// first blocker. Deploy always precedes verify (no point verifying an
// un-deployed service), so the suggestion order follows that. The
// default branch is unreachable while callers gate on len(pending) > 0.
func blockerNextAction(host string, needsDeploy, needsVerify bool) string {
	switch {
	case needsDeploy:
		return fmt.Sprintf("Next: zerops_deploy targetService=%q", host)
	case needsVerify:
		return fmt.Sprintf("Next: zerops_verify serviceHostname=%q", host)
	default:
		return ""
	}
}

// renderPhase is one line: the phase identifier plus the work session intent
// when present. The phase string is the same token used in the envelope JSON
// so the LLM can pattern-match both formats.
func renderPhase(b *strings.Builder, env StateEnvelope) {
	switch env.Phase {
	case PhaseDevelopActive:
		if env.WorkSession != nil {
			fmt.Fprintf(b, "Phase: develop-active — intent: %q\n", env.WorkSession.Intent)
			return
		}
		fmt.Fprintln(b, "Phase: develop-active")
	case PhaseDevelopClosed:
		if env.WorkSession != nil {
			fmt.Fprintf(b, "Phase: develop-closed-auto — intent: %q (all services done)\n", env.WorkSession.Intent)
			return
		}
		fmt.Fprintln(b, "Phase: develop-closed-auto")
	case PhaseIdle, PhaseBootstrapActive, PhaseRecipeActive, PhaseStrategySetup, PhaseExportActive:
		fmt.Fprintf(b, "Phase: %s\n", env.Phase)
	}
}

// renderServices prints one line per service with its type, mode, strategy,
// and stage pair when applicable. Empty Services list prints "Services: none".
func renderServices(b *strings.Builder, env StateEnvelope) {
	if len(env.Services) == 0 {
		fmt.Fprintln(b, "Services: none")
		return
	}
	names := make([]string, len(env.Services))
	for i, svc := range env.Services {
		names[i] = svc.Hostname
	}
	fmt.Fprintf(b, "Services: %s\n", strings.Join(names, ", "))
	for _, svc := range env.Services {
		fmt.Fprintf(b, "  - %s\n", renderServiceLine(svc))
	}
}

func renderServiceLine(svc ServiceSnapshot) string {
	parts := []string{fmt.Sprintf("%s (%s)", svc.Hostname, svc.TypeVersion)}
	switch svc.RuntimeClass {
	case topology.RuntimeManaged:
		parts = append(parts, "managed")
	case topology.RuntimeUnknown:
		parts = append(parts, "unknown runtime")
	case topology.RuntimeDynamic, topology.RuntimeStatic, topology.RuntimeImplicitWeb:
		if svc.Bootstrapped {
			parts = append(parts, renderBootstrappedFields(svc))
		} else {
			parts = append(parts, "not bootstrapped")
		}
	}
	if svc.Status != "" && svc.Status != "ACTIVE" {
		parts = append(parts, "["+svc.Status+"]")
	}
	return strings.Join(parts, " — ")
}

func renderBootstrappedFields(svc ServiceSnapshot) string {
	fields := []string{"bootstrapped=true", "mode=" + string(svc.Mode)}
	if svc.Strategy == "" || svc.Strategy == topology.StrategyUnset {
		fields = append(fields, "strategy=unset")
	} else {
		fields = append(fields, "strategy="+string(svc.Strategy))
	}
	if svc.StageHostname != "" {
		fields = append(fields, "stage="+svc.StageHostname)
	}
	if svc.Deployed {
		fields = append(fields, "deployed=true")
	} else {
		fields = append(fields, "deployed=false")
	}
	return strings.Join(fields, ", ")
}

// renderGuidance dumps the synthesized atom bodies as a single section. The
// synthesiser already ordered them by priority — we just wrap with a header.
func renderGuidance(b *strings.Builder, guidance []string) {
	if len(guidance) == 0 {
		return
	}
	fmt.Fprintln(b, "Guidance:")
	for _, item := range guidance {
		fmt.Fprintln(b, indentLines(item, "  "))
	}
}

// indentLines prefixes every non-empty line with indent. Empty lines stay
// empty so paragraph breaks survive.
func indentLines(body, indent string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

// renderPlan prints the typed Plan with explicit Primary / Secondary /
// Alternatives markers — the fix for D6 where three bullets were rendered
// without priority. The tokens (▸, ◦, ·) are chosen for visual hierarchy:
// filled triangle = primary, open circle = secondary, dot = alternative.
//
// The Per service: section renders only when len(PerService) > 1 — a single
// service is already named in Primary, so a duplicate row would waste tokens.
// Hostnames are sorted so the output is deterministic across calls.
func renderPlan(b *strings.Builder, plan *Plan) {
	if plan == nil || plan.Primary.IsZero() {
		return
	}
	fmt.Fprintln(b, "Next:")
	fmt.Fprintf(b, "  ▸ Primary: %s\n", formatAction(plan.Primary))
	if plan.Secondary != nil && !plan.Secondary.IsZero() {
		fmt.Fprintf(b, "  ◦ Secondary: %s\n", formatAction(*plan.Secondary))
	}
	if len(plan.PerService) > 1 {
		hosts := make([]string, 0, len(plan.PerService))
		for host := range plan.PerService {
			hosts = append(hosts, host)
		}
		sort.Strings(hosts)
		fmt.Fprintln(b, "  · Per service:")
		for _, host := range hosts {
			fmt.Fprintf(b, "      - %s: %s\n", host, formatAction(plan.PerService[host]))
		}
	}
	if len(plan.Alternatives) > 0 {
		fmt.Fprintln(b, "  · Alternatives:")
		for _, alt := range plan.Alternatives {
			fmt.Fprintf(b, "      - %s\n", formatAction(alt))
		}
	}
}

// formatAction renders one NextAction as "Label — tool(args)". Args are
// sorted for determinism (map iteration would otherwise vary run-to-run).
func formatAction(a NextAction) string {
	if len(a.Args) == 0 {
		return fmt.Sprintf("%s — %s", a.Label, a.Tool)
	}
	keys := make([]string, 0, len(a.Args))
	for k := range a.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%q", k, a.Args[k]))
	}
	return fmt.Sprintf("%s — %s %s", a.Label, a.Tool, strings.Join(pairs, " "))
}
