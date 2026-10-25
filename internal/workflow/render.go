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
// order is stable: Phase → Services → Leases → Changes → Progress → Blockers →
// Guidance → Next. Each section is skipped when it has no content, keeping the
// output compact. Blockers is a one-line call-to-action surfaced above
// the (large) Guidance section so the auto-close gate is visible
// without scrolling past atoms.
func RenderStatus(resp Response) string {
	var b strings.Builder
	b.WriteString("## Status\n")

	renderPhase(&b, resp.Envelope)
	renderServices(&b, resp.Envelope)
	renderLeases(&b, resp.Envelope)
	renderHQChanges(&b, resp.Envelope)
	renderProgressAndBlockers(&b, resp.Envelope)
	renderProdSourceControlSignpost(&b, resp.Envelope)
	renderGuidance(&b, resp.Guidance)
	renderPlan(&b, resp.Plan)

	return b.String()
}

// renderProdSourceControlSignpost surfaces the dev/stage→production
// source-of-truth discontinuity EARLY (RC-C). The dev/stage happy path
// deploys directly (no user-owned git remote); launch-production hard-requires
// one (git-push-setup is the only thing that establishes it — launch and
// export both only consume a remote). Nothing in the dev/stage flow bridges
// that, so a developer who wants prod discovers the gap only at the launch
// wall (the e3 failure). Fire ONLY when the session intent explicitly signals
// production AND a required service still has git-push unconfigured — so
// the 90% of sessions that never launch see no friction. Pure over the
// envelope's stable Intent + GitPushState; render-layer (not atom-capped).
func renderProdSourceControlSignpost(b *strings.Builder, env StateEnvelope) {
	ws := env.WorkSession
	if ws == nil || ws.ClosedAt != nil || env.Phase != PhaseDevelopActive {
		return
	}
	if !intentSignalsProduction(ws.Intent) {
		return
	}
	byHost := make(map[string]ServiceSnapshot, len(env.Services))
	for _, s := range env.Services {
		byHost[s.Hostname] = s
	}
	unconfigured := false
	for _, h := range ws.Services {
		if role := ws.Roles[h]; role != "" && role != RoleRequired {
			continue
		}
		s, ok := byHost[h]
		if !ok {
			continue
		}
		if s.GitPushState == "" || s.GitPushState == topology.GitPushUnconfigured {
			unconfigured = true
			break
		}
	}
	if !unconfigured {
		return
	}
	fmt.Fprintln(b, "Production note: launch-production builds from a USER-OWNED git remote, but this project deploys directly (git-push not configured) — the dev/stage flow never creates a remote. Run `zerops_workflow action=\"git-push-setup\"` (your repo + PAT) before launch; configure it now or know it is required at the prod boundary. Deferring prod is fine — surface this so it is not a surprise later.")
}

// intentSignalsProduction reports whether the work-session intent explicitly
// mentions production / launch (RC-C trigger). Deliberately conservative —
// matches whole production-signaling words, not the bare "prod" substring
// (which would false-positive on "product"), so the signpost only fires when
// the user actually pointed at prod.
func intentSignalsProduction(intent string) bool {
	low := strings.ToLower(intent)
	for _, kw := range []string{"production", "launch", "go live", "go-live", "to prod", "into prod", "prod project", "prod environment"} {
		if strings.Contains(low, kw) {
			return true
		}
	}
	return false
}

// transientRequiredHosts returns the required (RoleRequired) service hostnames
// that are deferred-start — dev-mode dynamic runtimes served only by the
// zerops_dev_server process (RC-A′). Their deploy+verify "pass" reflects a
// live dev process, not the app's own start: the dev service's supervisor
// starts a declared server again after a restart or redeploy (spec-workflows
// §8 O4), but a crash leaves it down until it is started. Pure over the snapshot's STABLE
// (mode, class) — reads no liveness — so the envelope stays byte-deterministic
// (the constraint the derived-close model depends on for compaction safety).
func transientRequiredHosts(env StateEnvelope) []string {
	ws := env.WorkSession
	if ws == nil {
		return nil
	}
	byHost := make(map[string]ServiceSnapshot, len(env.Services))
	for _, s := range env.Services {
		byHost[s.Hostname] = s
	}
	var out []string
	for _, h := range ws.Services {
		if role := ws.Roles[h]; role != "" && role != RoleRequired {
			continue
		}
		s, ok := byHost[h]
		if !ok {
			continue
		}
		if topology.IsDeferredStart(s.Mode, s.RuntimeClass) {
			out = append(out, h)
		}
	}
	return out
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
	// RC-B: the completion denominator is the REQUIRED services only. Deferred /
	// out-of-scope services stay visible (Progress line + a reminder) but never
	// gate auto-close, so "iterate dev only, leave staging" reads honestly
	// instead of stalling forever at 0/2.
	statuses := make([]hostStatus, 0, len(ws.Services))
	var pending []string
	var excluded []string
	needsDeploy := false
	needsVerify := false

	for _, host := range ws.Services {
		if role := ws.Roles[host]; role != "" && role != RoleRequired {
			excluded = append(excluded, fmt.Sprintf("%s (%s)", host, role))
			continue
		}
		deploys := ws.Deploys[host]
		verifies := ws.Verifies[host]
		st := hostStatus{host: host}
		st.deployText, st.deployOK = lastAttemptText(deploys, "deploy")
		st.verifyText, st.verifyOK = lastAttemptText(verifies, "verify")
		// A verify that passed before the latest deploy is stale — the deploy
		// replaced the container (B3/F60). Demote it so the blocker line and
		// the auto-close gate agree the service still needs re-verify.
		if st.verifyOK && staleVerify(deploys, verifies) {
			st.verifyOK = false
			st.verifyText = "verify stale (passed before the last deploy — re-verify)"
		}
		statuses = append(statuses, st)
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
	if len(excluded) > 0 {
		fmt.Fprintf(b, "Out of scope this session (not blocking close): %s\n", strings.Join(excluded, ", "))
	}
	// RC-A′: durability legibility while the session is still open. A dev-mode
	// dynamic service is live only via zerops_dev_server — name that here so the
	// agent doesn't report the URL as durably shipped (the e2 failure).
	if transient := transientRequiredHosts(env); len(transient) > 0 {
		fmt.Fprintf(b, "Durability: %s served via zerops_dev_server (dev-mode) — live now; zcp starts the dev server again after a restart or redeploy, but a crash leaves it down until it is started. Use simple mode for an always-on service.\n", strings.Join(transient, ", "))
	}
	if len(pending) > 0 {
		required := len(statuses)
		ready := required - len(pending)
		fmt.Fprintf(b, "→ Auto-close blocked: %d/%d ready, pending %s. %s\n",
			ready, required,
			strings.Join(pending, ", "),
			blockerNextAction(pending[0], needsDeploy, needsVerify, env))
	}
	// Close-mode is the gate's third input — surface it in the HEAD too, not
	// just the guidance wall (B5). It fires even when pending==0 (deploy+verify
	// green but close-mode unset still blocks auto-close — the silent-head hole,
	// B5-N1). The blockerNextAction line above only names deploy|verify.
	if unset := closeModeUnsetHosts(env); len(unset) > 0 {
		fmt.Fprintf(b, "→ DECISION required: close-mode unset on %s — %s\n",
			strings.Join(unset, ", "), CloseModeCallExample(unset))
	}
}

// closeModeUnsetHosts returns the required in-scope hosts whose CloseDeployMode
// is empty/unset — the services blocking auto-close on the close-mode axis.
func closeModeUnsetHosts(env StateEnvelope) []string {
	ws := env.WorkSession
	if ws == nil {
		return nil
	}
	byHost := make(map[string]ServiceSnapshot, len(env.Services))
	for _, s := range env.Services {
		byHost[s.Hostname] = s
	}
	var unset []string
	for _, h := range ws.Services {
		if role := ws.Roles[h]; role != "" && role != RoleRequired {
			continue
		}
		s, ok := byHost[h]
		if !ok {
			continue
		}
		if s.CloseDeployMode == "" || s.CloseDeployMode == topology.CloseModeUnset {
			unset = append(unset, h)
		}
	}
	return unset
}

// CloseModeCallExample renders the canonical close-mode call for the given
// hosts. Single owner so the head DECISION line, the auto-close gate Reason,
// and the close-mode handler Hint cannot drift in syntax (B5: previously
// emitted from 5 hand-authored sites in 2 placeholder styles).
func CloseModeCallExample(hosts []string) string {
	pairs := make([]string, 0, len(hosts))
	for _, h := range hosts {
		pairs = append(pairs, fmt.Sprintf("%q:%q", h, topology.CloseModeAuto))
	}
	return fmt.Sprintf(`zerops_workflow action="close-mode" closeMode={%s}`, strings.Join(pairs, ","))
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
// A stage whose pair's dev half is a pool gets the call the plan names for
// it (poolStageDeployNext), never a deploy with no source.
func blockerNextAction(host string, needsDeploy, needsVerify bool, env StateEnvelope) string {
	switch {
	case needsDeploy:
		if next := poolStageDeployNext(host, env); next != "" {
			return next
		}
		// RC-A′ / F8: state WHY deploy is needed. A live SSHFS/dev-server edit
		// is visible now but is NOT in the deployed artefact — it reverts on the
		// next container cycle. Deploy is what makes the change durable, even
		// when it already renders. Without this rationale agents read the hint
		// as redundant for an already-visible edit and ship a phantom change.
		return fmt.Sprintf("Next: zerops_deploy targetService=%q (un-deployed edits revert on a container cycle — deploy makes the change durable)", host)
	case needsVerify:
		return fmt.Sprintf("Next: zerops_verify serviceHostname=%q", host)
	default:
		return ""
	}
}

// poolStageDeployNext is the auto-close line's deploy call for host when its
// pair's dev half is a pool, resolved as deployActionFor resolves the plan's:
// the cross-deploy from a held lease of the pool, else acquiring one first
// (docs/spec-workflows.md §13 PM-10). "" for any other host, and for a manual
// close-mode, which the generic line covers.
func poolStageDeployNext(host string, env StateEnvelope) string {
	intent := ResolveWithLeases(snapshotByHostname(env.Services, host), env.Services, env.LeaseList())
	switch {
	case intent.SourcePool == "" || intent.Delivery == DeployDeliveryManual:
		return ""
	case intent.PushSource == "":
		return fmt.Sprintf("Next: zerops_lease action=\"acquire\" pool=%q (%s deploys from a lease of its dev pool, never from the pool itself)", intent.SourcePool, host)
	}
	return fmt.Sprintf("Next: zerops_deploy sourceService=%q targetService=%q setup=%q (un-deployed edits revert on a container cycle — deploy makes the change durable)",
		intent.PushSource, host, intent.BuildSetup)
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
			if transient := transientRequiredHosts(env); len(transient) > 0 {
				// RC-A′: the session completed (agent did the work), but a
				// dev-mode dynamic required service is live only via the dev
				// server — kept across restarts and redeploys, down after a
				// crash. Say so instead of "all services done", which reads as
				// an always-on start.
				fmt.Fprintf(b, "Phase: develop-closed-auto — intent: %q (live via dev-server, not an always-on start: %s — zcp brings it back after a restart or redeploy, a crash leaves it down; switch to simple mode for an always-on service)\n",
					env.WorkSession.Intent, strings.Join(transient, ", "))
				return
			}
			fmt.Fprintf(b, "Phase: develop-closed-auto — intent: %q (all services done)\n", env.WorkSession.Intent)
			return
		}
		fmt.Fprintln(b, "Phase: develop-closed-auto")
	case PhaseIdle, PhaseBootstrapActive, PhaseStrategySetup, PhaseExportActive, PhaseLaunchProductionActive, PhasePoolMigrateActive:
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

// renderLeases prints the pool-container leases this project holds, one line
// each — `<alias> · <pool> · <branch> · <state>` — and nothing at all when
// there are none, so a lease-less project's status is byte-identical to what
// it was before leases existed. The state is rendered verbatim (never
// enumerated here); a state read from zcp's own record because the
// orchestrator did not answer is marked unverified. A lease string has no
// field in LeaseSnapshot, so it cannot appear here (DP-3).
func renderLeases(b *strings.Builder, env StateEnvelope) {
	leases := env.LeaseList()
	if len(leases) == 0 {
		return
	}
	fmt.Fprintln(b, "Leases:")
	for _, l := range leases {
		branch := l.Branch
		if branch == "" {
			branch = "-"
		}
		state := l.State
		if l.Unverified {
			state += " (unverified)"
		}
		fmt.Fprintf(b, "  - %s · %s · %s · %s\n", l.Alias, l.Pool, branch, state)
	}
}

func renderServiceLine(svc ServiceSnapshot) string {
	parts := []string{fmt.Sprintf("%s (%s)", svc.Hostname, svc.TypeVersion)}
	switch svc.RuntimeClass {
	case topology.RuntimeManaged:
		parts = append(parts, "managed")
	case topology.RuntimePool:
		parts = append(parts, "pool")
		if svc.DevIsPool {
			parts = append(parts, renderDevPoolFields(svc))
		}
	case topology.RuntimeSandbox:
		parts = append(parts, "sandbox")
	case topology.RuntimeUnknown:
		parts = append(parts, "unknown runtime")
	case topology.RuntimeDynamic, topology.RuntimeStatic, topology.RuntimeImplicitWeb:
		if svc.Bootstrapped {
			parts = append(parts, renderBootstrappedFields(svc))
		} else {
			// deployed= comes from the platform alone here: a runtime imported
			// without code is ACTIVE like a running app, and the status tag
			// below is omitted for ACTIVE.
			parts = append(parts, fmt.Sprintf("not bootstrapped, deployed=%t", svc.Deployed))
		}
		if svc.RuntimeClass == topology.RuntimeDynamic || svc.RuntimeClass == topology.RuntimeImplicitWeb {
			if hint := renderBasePrefixHint(svc); hint != "" {
				parts = append(parts, hint)
			}
		}
	}
	if svc.Status != "" && svc.Status != StatusActive {
		parts = append(parts, "["+svc.Status+"]")
	}
	return strings.Join(parts, " — ")
}

// renderBasePrefixHint returns the zerops.yaml base-prefix rule for a
// runtime service whose live type carries a known OS prefix ("" when it
// doesn't — a bare/legacy TypeVersion has no prefix to derive the rule
// from). Stated on the envelope service line itself, derived from the live
// type: the rule reaches every develop path regardless of deploy state,
// unlike a first-deploy-only atom gated on never-deployed, which an adopted
// service already running deployed code never sees.
//
// The rule is for a base the agent adds. zcp needs no particular OS (its
// zcp-devsup binary is static), and a base a zerops.yaml already declares
// is the repo's choice: run.base sets the service OS on each deploy, so an
// import-time ubuntu/ type beside a bare nodejs@22 (alpine) is not a
// conflict, and switching it changes the build image (ubuntu's global npm
// directory needs sudo). So the rule is withheld when the repo declares its
// bases (ServiceSnapshot.DeclaresBase).
//
// Excluded on purpose: static runtimes (`run.base` is the bare special
// token `static`, not an OS-prefixed base — the build base OS is the
// builder's choice) and managed services (never carry an OS prefix).
func renderBasePrefixHint(svc ServiceSnapshot) string {
	prefix := topology.OSPrefix(svc.TypeVersion)
	if prefix == "" || svc.DeclaresBase {
		return ""
	}
	bare := topology.CanonicalBareForm(svc.TypeVersion)
	return fmt.Sprintf(
		"base prefix %s/ — a zerops.yaml base you add takes it on BOTH build.base and run.base (legacy %s + os: %s ≡ %s); run.base sets the service OS on every deploy, so bases that already deploy stay as they are",
		prefix, bare, prefix, svc.TypeVersion,
	)
}

func renderBootstrappedFields(svc ServiceSnapshot) string {
	fields := []string{"bootstrapped=true", "mode=" + string(svc.Mode)}
	closeMode := svc.CloseDeployMode
	if closeMode == "" {
		closeMode = topology.CloseModeUnset
	}
	fields = append(fields, "closeMode="+string(closeMode))
	gitPush := svc.GitPushState
	if gitPush == "" {
		gitPush = topology.GitPushUnconfigured
	}
	fields = append(fields, "gitPush="+string(gitPush))
	buildIntegration := svc.BuildIntegration
	if buildIntegration == "" {
		buildIntegration = topology.BuildIntegrationNone
	}
	fields = append(fields, "buildIntegration="+string(buildIntegration))
	if svc.StageHostname != "" {
		fields = append(fields, "stage="+svc.StageHostname)
	}
	if svc.Deployed {
		fields = append(fields, "deployed=true")
	} else {
		fields = append(fields, "deployed=false")
	}
	// Every remote is named without the credentials a URL may carry.
	if svc.RemoteURL != "" {
		fields = append(fields, "remoteUrl="+topology.RedactRepoURLCredentials(svc.RemoteURL))
	}
	fields = append(fields, hqFields(svc)...)
	if len(svc.FeedsProduction) > 0 {
		fields = append(fields, "feedsProduction="+strings.Join(svc.FeedsProduction, "; "))
	}
	return strings.Join(fields, ", ")
}

// renderDevPoolFields is the line of a pool keying its pair: the pair it is
// the dev half of — every stage and the HQ record — and no claim of a mode,
// deploy state or setup of its own (a pool runs leases, not one container).
func renderDevPoolFields(svc ServiceSnapshot) string {
	fields := []string{"dev half of a pair"}
	if stages := svc.PairStageHostnames(); len(stages) > 0 {
		fields = append(fields, "stage="+strings.Join(stages, ","))
	}
	fields = append(fields, hqFields(svc)...)
	return strings.Join(fields, ", ")
}

// hqFields are a pair dev half's HQ record fields, none outside HQ.
func hqFields(svc ServiceSnapshot) []string {
	var fields []string
	if svc.HQRepo != "" {
		fields = append(fields, "hqRepo="+svc.HQRepo)
	}
	switch {
	case svc.HQRepo == "":
	case svc.HQOrigin != nil && svc.HQOrigin.Mode != "":
		fields = append(fields, "origin.mode="+svc.HQOrigin.Mode)
	default:
		// Not read yet, or from an HQ older than modes.
		fields = append(fields, "origin.mode=unknown")
	}
	if svc.HQOrigin != nil && svc.HQOrigin.URL != "" {
		fields = append(fields, "origin="+topology.RedactRepoURLCredentials(svc.HQOrigin.URL))
	}
	if svc.HQCheckoutOrigin != "" {
		checkout := "checkoutOrigin=" + topology.RedactRepoURLCredentials(svc.HQCheckoutOrigin)
		if svc.HQOriginPending != "" {
			checkout += " (pending: develop's start points it at " + topology.RedactRepoURLCredentials(svc.HQOriginPending) + ")"
		}
		fields = append(fields, checkout)
	}
	return fields
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
// sorted for determinism (map iteration would otherwise vary run-to-run); a
// list argument (ListArg) is written unquoted, as the tool call takes it.
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
		if isListArg(a.Args[k]) {
			pairs = append(pairs, k+"="+a.Args[k])
			continue
		}
		pairs = append(pairs, fmt.Sprintf("%s=%q", k, a.Args[k]))
	}
	return fmt.Sprintf("%s — %s %s", a.Label, a.Tool, strings.Join(pairs, " "))
}
