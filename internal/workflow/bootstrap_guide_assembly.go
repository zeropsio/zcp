package workflow

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// buildGuide assembles a step guide by synthesizing knowledge atoms against a
// minimal StateEnvelope derived from the live BootstrapState. Iteration-delta
// guidance short-circuits atom synthesis for retries — failure ladders are
// step-scoped to deploy and not expressible as atoms today.
//
// knowledge.Provider is accepted for signature stability with earlier callers
// but no longer consulted: runtime briefings were redundant with the runtime
// guide atoms and added 2x the token cost without new information.
func (b *BootstrapState) buildGuide(step string, iteration int, env Environment, _ knowledge.Provider) string {
	if iteration > 0 {
		if delta := BuildIterationDelta(step, iteration, b.Plan, b.lastAttestation()); delta != "" {
			return delta
		}
	}

	if step == StepClose && (b.Plan == nil || len(b.Plan.Targets) == 0) {
		return closeGuidance
	}

	corpus, err := LoadAtomCorpus()
	if err != nil {
		return ""
	}
	envelope := b.synthesisEnvelope(step, env)
	bodies, err := Synthesize(envelope, corpus)
	if err != nil {
		return ""
	}
	out := strings.Join(bodies, "\n\n---\n\n")

	// Env var catalog is dynamic data — not expressible as a static atom.
	// Re-injected at generate so the agent has the authoritative key list
	// even if the provision attestation was lost to compaction.
	if step == StepGenerate && len(b.DiscoveredEnvVars) > 0 {
		if out != "" {
			out += "\n\n---\n\n"
		}
		out += formatEnvVarsForGuide(b.DiscoveredEnvVars)
	}
	return out
}

// synthesisEnvelope builds the minimal StateEnvelope that atom filtering
// needs during a bootstrap step. Phase is always bootstrap-active; route
// is inferred from the plan (adopt when every target pre-exists, classic
// otherwise — recipe bootstraps run a separate conductor and don't reach
// buildGuide). Each standard-mode runtime emits two snapshots (dev + stage)
// so stage-scoped atoms match.
func (b *BootstrapState) synthesisEnvelope(step string, env Environment) StateEnvelope {
	services := make([]ServiceSnapshot, 0)
	if b.Plan != nil {
		for _, t := range b.Plan.Targets {
			services = append(services, planTargetSnapshots(t)...)
		}
	}
	route := BootstrapRouteClassic
	if b.Plan != nil && b.Plan.IsAllExisting() {
		route = BootstrapRouteAdopt
	}
	return StateEnvelope{
		Phase:       PhaseBootstrapActive,
		Environment: env,
		Services:    services,
		Bootstrap:   &BootstrapSessionSummary{Route: route, Step: step},
	}
}

// planTargetSnapshots turns a plan target into one (dev/simple) or two
// (standard = dev + stage) service snapshots keyed by envelope Mode. The
// RuntimeClass assumes the target has ports (true for every dynamic or
// implicit-webserver runtime we bootstrap today); static plans keep the
// same classification because atoms filter on runtime presence, not shape.
func planTargetSnapshots(t BootstrapTarget) []ServiceSnapshot {
	mode := planModeToEnvelopeMode(t.Runtime.EffectiveMode())
	runtimeClass := classifyEnvelopeRuntime(t.Runtime.Type, true)
	snaps := []ServiceSnapshot{{
		Hostname:     t.Runtime.DevHostname,
		TypeVersion:  t.Runtime.Type,
		RuntimeClass: runtimeClass,
		Mode:         mode,
	}}
	if mode == ModeStandard {
		if stage := t.Runtime.StageHostname(); stage != "" {
			snaps[0].StageHostname = stage
			snaps = append(snaps, ServiceSnapshot{
				Hostname:     stage,
				TypeVersion:  t.Runtime.Type,
				RuntimeClass: runtimeClass,
				Mode:         ModeStage,
			})
		}
	}
	return snaps
}

// planModeToEnvelopeMode translates the plan's string mode into the envelope
// Mode enum. Standard's dev half gets ModeStandard; the stage half is emitted
// separately by planTargetSnapshots with ModeStage.
func planModeToEnvelopeMode(mode string) Mode {
	switch mode {
	case PlanModeDev:
		return ModeDev
	case PlanModeSimple:
		return ModeSimple
	case PlanModeStandard, "":
		return ModeStandard
	}
	return Mode(mode)
}

// closeGuidance is the static guidance for the administrative close step
// when the plan is empty (managed-only bootstraps) — no atom corpus path
// covers that edge case, so we keep the short inline message.
const closeGuidance = `Bootstrap is complete. All services are deployed and healthy.

Complete this step to finalize bootstrap:
→ zerops_workflow action="complete" step="close" attestation="Bootstrap finalized, services operational"

After closing, choose a deployment strategy for each service before deploying again.`

// formatEnvVarsForGuide formats discovered env vars as a markdown catalog table
// for guide injection. Shape mirrors the attestation the provision step tells
// the agent to write (Phase 3 reshuffle): one row per service, raw keys in one
// column for attestation parity and cross-service reference forms in another
// column for direct copy-paste into run.envVariables.
func formatEnvVarsForGuide(envVars map[string][]string) string {
	hostnames := make([]string, 0, len(envVars))
	for h := range envVars {
		hostnames = append(hostnames, h)
	}
	sort.Strings(hostnames)

	var sb strings.Builder
	sb.WriteString("## Discovered Managed-Service Env Var Catalog\n\n")
	sb.WriteString("Recorded at provision via `zerops_discover includeEnvs=true`. **These are the authoritative names** — do not guess alternative spellings; unknown cross-service references resolve to literal strings at runtime and fail silently.\n\n")
	sb.WriteString("| Service | Keys | Cross-service reference shape |\n")
	sb.WriteString("|---|---|---|\n")
	for _, hostname := range hostnames {
		vars := envVars[hostname]
		keys := strings.Join(vars, ", ")
		refs := make([]string, len(vars))
		for i, v := range vars {
			refs[i] = "`${" + hostname + "_" + v + "}`"
		}
		fmt.Fprintf(&sb, "| `%s` | %s | %s |\n", hostname, keys, strings.Join(refs, " "))
	}
	sb.WriteString("\n**Usage**: reference these in `run.envVariables` of your app's zerops.yaml. They resolve at deploy time — they are NOT active as OS env vars on a dev container that was started with `startWithoutCode: true`.\n")
	return sb.String()
}

const bootstrapCompleteMsg = "Bootstrap complete."

// BuildTransitionMessage creates a summary message when bootstrap completes.
// Includes service list, transition hint, and router offerings.
func BuildTransitionMessage(state *WorkflowState) string {
	if state == nil || state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return bootstrapCompleteMsg
	}

	// Adoption: all targets are existing services — no code was generated or deployed.
	if state.Bootstrap.Plan.IsAllExisting() {
		return buildAdoptionTransitionMessage(state)
	}

	// Managed-only: no runtime targets, just managed services.
	if len(state.Bootstrap.Plan.Targets) == 0 {
		return bootstrapCompleteMsg + "\n\nManaged services provisioned. No runtime services to deploy." +
			"\n\nAvailable operations:\n" +
			"- Scale: `zerops_scale serviceHostname=\"...\"`\n" +
			"- Env vars: `zerops_env action=\"set|delete\"` (reload after: `zerops_manage action=\"reload\"`)\n" +
			"- Investigate: `zerops_workflow action=\"start\" workflow=\"develop\"`"
	}

	var sb strings.Builder
	sb.WriteString(bootstrapCompleteMsg + "\n\n## Services\n\n")
	writeServiceList(&sb, state.Bootstrap.Plan)

	sb.WriteString("\nInfrastructure is verified — services running with a verification server (hello-world). No application code has been written yet.\n\n")
	writeDeployModelPrimer(&sb)

	sb.WriteString("To implement the user's application, start the develop workflow:\n")
	sb.WriteString("`zerops_workflow action=\"start\" workflow=\"develop\"`\n\n")

	sb.WriteString("## What's Next?\n\n")
	sb.WriteString("Infrastructure is ready and verified. Choose your next workflow:\n\n")
	writeOfferingsFooter(&sb, state)

	return sb.String()
}

// buildAdoptionTransitionMessage creates a summary for pure-adoption bootstraps.
// Existing services keep their code and configuration — no hello-world was deployed.
func buildAdoptionTransitionMessage(state *WorkflowState) string {
	var sb strings.Builder
	sb.WriteString(bootstrapCompleteMsg + " Services adopted — existing code and configuration preserved.\n\n## Services\n\n")
	writeServiceList(&sb, state.Bootstrap.Plan)
	sb.WriteString("\n")
	writeDeployModelPrimer(&sb)
	sb.WriteString("## What's Next?\n\n")
	writeOfferingsFooter(&sb, state)

	return sb.String()
}

func writeServiceList(sb *strings.Builder, plan *ServicePlan) {
	for _, t := range plan.Targets {
		mode := t.Runtime.EffectiveMode()
		fmt.Fprintf(sb, "- **%s** (%s, %s mode)\n", t.Runtime.DevHostname, t.Runtime.Type, mode)
		if mode == PlanModeStandard {
			fmt.Fprintf(sb, "  Stage: **%s**\n", t.Runtime.StageHostname())
		}
		for _, d := range t.Dependencies {
			fmt.Fprintf(sb, "  - %s (%s)\n", d.Hostname, d.Type)
		}
	}
}

func writeDeployModelPrimer(sb *strings.Builder) {
	sb.WriteString("## Deploy Model (read before developing)\n\n")
	sb.WriteString("- **Deploy = new container** — each deploy replaces the container. Only `deployFiles` content persists.\n")
	sb.WriteString("- **Code on SSHFS mount** — write code to the local mount (`/var/www/{hostname}/`), not via SSH into the container.\n")
	sb.WriteString("- **prepareCommands need `sudo`** — containers run as `zerops` user. Use `sudo apk add` / `sudo apt-get install`.\n")
	sb.WriteString("- **Build ≠ Run** — build container has `build.base`, run container has `run.base`. Install runtime packages in `run.prepareCommands`.\n\n")
}

func writeOfferingsFooter(sb *strings.Builder, state *WorkflowState) {
	offerings := routeFromBootstrapState(state)
	for i, o := range offerings {
		num := 'A' + rune(i)
		fmt.Fprintf(sb, "**%c) %s**\n", num, titleCase(o.Workflow))
		if o.Hint != "" {
			fmt.Fprintf(sb, "   → `%s`\n", o.Hint)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("**Other operations:**\n")
	sb.WriteString("- Scale: `zerops_scale serviceHostname=\"...\"`\n")
	sb.WriteString("- Env vars: `zerops_env action=\"set|delete\"` (reload after: `zerops_manage action=\"reload\"`)\n")
}

// routeFromBootstrapState generates workflow offerings based on bootstrap state.
// Returns up to 3 primary offerings (develop, cicd, and utilities).
func routeFromBootstrapState(state *WorkflowState) []FlowOffering {
	if state == nil || state.Bootstrap == nil || state.Bootstrap.Plan == nil {
		return nil
	}
	offerings := []FlowOffering{
		{
			Workflow: "develop",
			Priority: 1,
			Hint:     `zerops_workflow action="start" workflow="develop"`,
		},
		{
			Workflow: "cicd",
			Priority: 2,
			Hint:     `zerops_workflow action="start" workflow="cicd"`,
		},
	}
	offerings = appendUtilities(offerings)
	return offerings
}

// titleCase capitalizes the first letter of a word (replacement for deprecated strings.Title).
func titleCase(s string) string {
	if len(s) == 0 {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
