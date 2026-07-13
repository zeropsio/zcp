package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/topology"
)

// buildGuide assembles a step guide by synthesizing knowledge atoms against a
// minimal StateEnvelope derived from the live BootstrapState.
//
// Under Option A bootstrap never iterates — infra verification hard-stops and
// escalates to the user on any retry. iteration>0 short-circuits atom synthesis
// and returns the hard-stop message so the agent surfaces it cleanly.
//
// knowledge.Provider is accepted for signature stability with earlier callers
// but no longer consulted: runtime briefings were redundant with the runtime
// guide atoms and added 2x the token cost without new information.
func (b *BootstrapState) buildGuide(step string, iteration int, env Environment, _ knowledge.Provider) string {
	if iteration > 0 {
		return buildBootstrapHardStop(step, b.lastAttestation())
	}

	if step == StepClose && (b.Plan == nil || len(b.Plan.Targets) == 0) {
		return closeGuidance
	}

	// Errors here are build-time defects (corpus-load failure, malformed
	// atom, unknown placeholder). Surface them in the guidance text rather
	// than silently dropping to "" — silent empty guidance is
	// indistinguishable from intentional silence and masks defects until
	// they cause downstream LLM mis-decisions. Pinned by
	// TestBuildGuide_SynthesisErrorPropagates (plan §4 Phase 2 C3).
	corpus, err := LoadAtomCorpus()
	if err != nil {
		return fmt.Sprintf("## ERROR: failed to load atom corpus\n\n%v\n\nThis is a build-time defect — report it. Bootstrap guidance is unavailable until the corpus loads cleanly.", err)
	}
	envelope := b.synthesisEnvelope(step, env)
	matches, err := Synthesize(envelope, corpus)
	if err != nil {
		return fmt.Sprintf("## ERROR: atom synthesis failed for step %q\n\n%v\n\nThis is a build-time defect — report it. Run `make lint-local` to verify the corpus.", step, err)
	}
	// Budget backstop (R1): demote the lowest-relevance atoms to a head rather
	// than letting an oversized bootstrap-step payload hit the transport cap.
	matches = ComposeUnderBudget(matches, corpus, ComposeBodyBudget)
	var composition bootstrapGuideComposition
	for index, match := range matches {
		if index > 0 {
			composition.separator()
		}
		composition.append("atom", match.AtomID, match.Body)
	}

	// Env var catalog is dynamic data — not expressible as a static atom.
	// Injected at close so the develop handoff carries the authoritative key
	// list even if the provision attestation was lost to compaction. Develop
	// re-reads this from session state when writing zerops.yaml.
	if step == StepClose && len(b.DiscoveredEnvVars) > 0 {
		if composition.Len() > 0 {
			composition.separator()
		}
		composition.append("dynamic", "workflow.formatEnvVarsForGuide", formatEnvVarsForGuide(b.DiscoveredEnvVars))
	}

	// Recipe import YAML is dynamic data — it depends on the matched recipe.
	// Injected at discover (so the agent sees the shape ZCP will derive the
	// plan from — it confirms or renames, it does NOT author the plan) and at
	// provision (so the agent feeds it to zerops_import without scraping it out
	// of the atom body).
	//
	// At provision the YAML is REWRITTEN from the recipe shape + the overrides
	// reconciled at discover (RewriteRecipeImportYAMLFromShape) so a rename /
	// EXISTS-flip collision recovery flows end-to-end: ZCP rewrites the recipe
	// YAML, the agent copies the rewritten block into zerops_import, and the
	// services land with the intended hostnames. Discover shows the recipe
	// verbatim (the overrides aren't reconciled until the plan is submitted).
	if (step == StepDiscover || step == StepProvision) && b.Route == BootstrapRouteRecipe && b.RecipeMatch != nil && b.RecipeMatch.ImportYAML != "" {
		if composition.Len() > 0 {
			composition.separator()
		}
		match := b.RecipeMatch
		if step == StepProvision {
			// Rewrite the import YAML from the recipe shape + the overrides
			// reconciled at discover (hostname renames + managed EXISTS-drops).
			// Single owner: the derived plan and this YAML both come from
			// shape+overrides, so the import lands exactly the planned shape.
			var overrides RecipeShapeOverrides
			if b.RecipeOverrides != nil {
				overrides = *b.RecipeOverrides
			}
			if rewritten, err := RewriteRecipeImportYAMLFromShape(b.RecipeMatch.ImportYAML, overrides); err == nil {
				// Local mode (Theme 1): drop services declaring
				// zeropsSetup: dev. The agent's CWD replaces the SSH-in dev
				// runtime; provisioning a Zerops dev service is redundant.
				// buildFromGit on the remaining stage runtime is preserved —
				// Zerops API rejects pipelineConfig without the source URL.
				if env == EnvLocal {
					if localized, lerr := LocalizeRecipeImportYAML(rewritten); lerr == nil {
						rewritten = localized
					}
				}
				rewrittenMatch := *b.RecipeMatch
				rewrittenMatch.ImportYAML = rewritten
				match = &rewrittenMatch
			}
			// A rewrite failure on a valid corpus recipe is impossible
			// (BootstrapCompleteRecipePlan parsed the same YAML at discover);
			// the verbatim fallback still surfaces something actionable.
		}
		composition.append("dynamic", "workflow.formatRecipeImportYAMLForGuide", formatRecipeImportYAMLForGuide(match, step))
	}
	composition.record()
	return composition.String()
}

// buildBootstrapHardStop returns the escalation message when bootstrap is
// retried. Infrastructure verification doesn't iterate — if discover or
// provision fails, the issue is in the plan or the API, not something that
// rerunning the step will fix. Surface to the user rather than looping.
func buildBootstrapHardStop(step, lastAttestation string) string {
	var sb strings.Builder
	sb.WriteString("STOP — bootstrap retry attempted.\n\n")
	sb.WriteString("Infrastructure verification does not iterate. Bootstrap owns provisioning and registration only; if the ")
	sb.WriteString(step)
	sb.WriteString(" step failed, something is wrong with the plan or the Zerops API — rerunning the same step will not fix it.\n\n")
	if lastAttestation != "" {
		sb.WriteString("Last attestation: ")
		sb.WriteString(lastAttestation)
		sb.WriteString("\n\n")
	}
	sb.WriteString("Report to the user:\n")
	sb.WriteString("1. The current error (from the CheckResult or recent API responses).\n")
	sb.WriteString("2. What you attempted in the failing step.\n")
	sb.WriteString("3. Ask whether to adjust the plan, debug the API failure, or escalate.\n\n")
	sb.WriteString("Do NOT rerun the step without user input.")
	return sb.String()
}

// synthesisEnvelope builds the minimal StateEnvelope that atom filtering
// needs during a bootstrap step. Phase is always bootstrap-active; route
// is taken from BootstrapState.Route when set (recipe) and otherwise
// inferred from the plan (adopt when every target pre-exists, classic
// otherwise). Each standard-mode runtime emits two snapshots (dev + stage)
// so stage-scoped atoms match.
//
// Per-hostname Status is populated from BootstrapState.DiscoveredStatuses
// (captured at the most recent provision check) so atoms gated on
// serviceStatus (e.g. develop-ready-to-deploy.md gated on READY_TO_DEPLOY)
// fire correctly during bootstrap-active phase. Map may be empty before the
// first provision check or in tests — snapshots then carry Status="" and
// status-gated atoms simply don't match, which is the safe default.
func (b *BootstrapState) synthesisEnvelope(step string, env Environment) StateEnvelope {
	services := make([]ServiceSnapshot, 0)
	if b.Plan != nil {
		for _, t := range b.Plan.Targets {
			services = append(services, planTargetSnapshots(t, b.DiscoveredStatuses)...)
		}
	}
	route := b.Route
	if route == "" {
		route = BootstrapRouteClassic
		if b.Plan != nil && b.Plan.IsAllExisting() {
			route = BootstrapRouteAdopt
		}
	}
	summary := &BootstrapSessionSummary{Route: route, Step: step, RecipeMatch: b.RecipeMatch}
	return StateEnvelope{
		Phase:       PhaseBootstrapActive,
		Environment: env,
		Services:    services,
		Bootstrap:   summary,
	}
}

// planTargetSnapshots turns a plan target into one (dev/simple) or two
// (standard = dev + stage) service snapshots keyed by envelope Mode. The
// RuntimeClass assumes the target has ports (true for every dynamic or
// implicit-webserver runtime we bootstrap today); static plans keep the
// same classification because atoms filter on runtime presence, not shape.
//
// `statuses` carries per-hostname platform Status captured at the most recent
// provision check (BootstrapState.DiscoveredStatuses). Status is populated on
// each snapshot when the hostname is present in the map; absence yields
// Status="" which is the safe default (status-gated atoms simply don't match).
func planTargetSnapshots(t BootstrapTarget, statuses map[string]string) []ServiceSnapshot {
	mode := planModeToEnvelopeMode(t.Runtime.EffectiveMode())
	runtimeClass := classifyEnvelopeRuntime(t.Runtime.Type)
	snaps := []ServiceSnapshot{{
		Hostname:     t.Runtime.DevHostname,
		TypeVersion:  t.Runtime.Type,
		RuntimeClass: runtimeClass,
		Mode:         mode,
		Status:       statuses[t.Runtime.DevHostname],
	}}
	if mode == topology.ModeStandard {
		if stage := t.Runtime.StageHostname(); stage != "" {
			snaps[0].StageHostname = stage
			// Cross-type pair: the stage half may differ from the dev Type
			// (nodejs dev → static stage), so the stage snapshot carries the
			// stage's own type + class — not the dev runtimeClass computed above.
			stageType := t.Runtime.StageEffectiveType()
			snaps = append(snaps, ServiceSnapshot{
				Hostname:     stage,
				TypeVersion:  stageType,
				RuntimeClass: classifyEnvelopeRuntime(stageType),
				Mode:         topology.ModeStage,
				Status:       statuses[stage],
			})
		}
	}
	return snaps
}

// planModeToEnvelopeMode translates the plan's mode into the envelope
// Mode enum. Standard's dev half gets ModeStandard; the stage half is emitted
// separately by planTargetSnapshots with ModeStage.
func planModeToEnvelopeMode(mode topology.Mode) topology.Mode {
	switch mode {
	case topology.PlanModeDev:
		return topology.ModeDev
	case topology.PlanModeSimple:
		return topology.ModeSimple
	case topology.PlanModeStandard, "":
		return topology.ModeStandard
	case topology.ModeStage, topology.PlanModeLocalStage, topology.PlanModeLocalOnly:
		// Local modes and the envelope-only ModeStage pass through
		// unchanged — the plan flow doesn't emit them (plan inputs only
		// carry standard/dev/simple), but an explicit case silences the
		// exhaustive linter and documents the boundary.
		return mode
	}
	return mode
}

// closeGuidance is the static guidance for the administrative close step
// when the plan is empty (managed-only bootstraps) — no atom corpus path
// covers that edge case, so we keep the short inline message.
const closeGuidance = `Bootstrap is complete. All managed services are provisioned and running.

Complete this step to finalize bootstrap:
→ zerops_workflow action="complete" step="close" attestation="Bootstrap finalized — managed services provisioned"

No runtime services to deploy. Start develop if you need to adopt a runtime, or stop here.`

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

// formatRecipeImportYAMLForGuide renders the matched recipe's canonical
// import YAML as a fenced block with adjacent instructions. The agent must
// strip the `project:` section (zerops_import rejects it) and set any
// project-level env vars via `zerops_env` before the import call.
//
// At discover the guide is derive-and-confirm: ZCP derives the plan from the
// recipe (the owner) and the agent authors nothing — it only adjusts what the
// recipe leaves open (collision rename, already-existing managed dep, or an
// explicit dev-only narrowing). At provision it instructs the import of the
// already-rewritten YAML.
func formatRecipeImportYAMLForGuide(match *RecipeMatch, step string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Recipe — %q\n\n", match.Slug)

	if step == StepDiscover {
		// Derive-and-confirm: ZCP derives the plan from the recipe; the agent
		// authors nothing. It only adjusts what the recipe can't decide
		// (collision rename + already-existing managed dep + dev-only narrowing).
		// No bootstrapMode authoring — the mode is the recipe's, derived.
		sb.WriteString("ZCP derives the provisioning plan from this recipe — you do NOT author or mode-tag a plan. To accept the recipe as-is, complete the discover step with NO plan:\n\n")
		sb.WriteString("→ `zerops_workflow action=\"complete\" step=\"discover\"` (omit `plan`)\n\n")
		sb.WriteString("Submit a `plan` ONLY to adjust what the recipe leaves to you:\n")
		sb.WriteString("- rename a runtime hostname that collides with an existing service, or\n")
		sb.WriteString("- mark a managed dependency you already have as `resolution: \"EXISTS\"`.\n\n")
		sb.WriteString("Managed-service hostnames cannot be renamed (the app repo references them via `${hostname_*}`).\n\n")
		sb.WriteString("If the user EXPLICITLY asked for dev only (skip the paid staging service), narrow a standard recipe by adding `recipeNarrow=\"dev-only\"` to the same complete call — ZCP provisions the dev container + managed deps and skips the stage. Do NOT narrow by default.\n\nThe recipe's canonical import YAML, for reference:\n\n")
	} else {
		sb.WriteString("Provision the recipe's services from the YAML below (already rewritten with any hostname/resolution choices from your plan):\n\n")
		sb.WriteString("1. If the YAML has a `project:` block with `envVariables`, set those at the project level FIRST: `zerops_env action=\"set\" scope=\"project\" ...`.\n")
		sb.WriteString("2. Call `zerops_import` with the `services:` section ONLY — the import tool rejects YAML that includes `project:`.\n")
		sb.WriteString("3. Poll `zerops_discover` until every service reports `ACTIVE`. Recipes build from `buildFromGit`, so first provision can take 2–5 minutes while Zerops clones and builds.\n\n")
	}

	sb.WriteString("```yaml\n")
	sb.WriteString(match.ImportYAML)
	if !strings.HasSuffix(match.ImportYAML, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("```\n")
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

	// A recipe whose import YAML declares buildFromGit is cloned, BUILT, and
	// DEPLOYED from the recipe repo AT IMPORT — its runtimes reach ACTIVE and
	// serve the curated code immediately. The classic "nothing deployed, go
	// scaffold" narrative below is false for them, and the agent faithfully
	// relayed it: declared the task done at bootstrap-close without surfacing
	// the live subdomain URLs or running verify (Wave-5 finding). The
	// buildFromGit-vs-startWithoutCode distinction is already in the recipe
	// import YAML the handler holds (line ~72 branches on the same source for
	// the discover guide) — derive the close tell from it too.
	if state.Bootstrap.Route == BootstrapRouteRecipe && state.Bootstrap.RecipeMatch != nil &&
		planHasBuildFromGit(state.Bootstrap.Plan) {
		sb.WriteString("\nThe recipe's app was cloned, built, and DEPLOYED from git at import — its runtimes reach ACTIVE and serve the curated code immediately (this is buildFromGit, NOT the startWithoutCode empty-container path; it is NOT awaiting a first deploy). Before declaring done: run `zerops_discover` to read each runtime's live subdomain URL, then `zerops_verify serviceHostname=\"<host>\"` to confirm it serves. (Local mode: the dev runtime lives in your working directory instead — develop owns it.)\n\n")
		sb.WriteString("Next: `zerops_workflow action=\"start\" workflow=\"develop\"` to iterate on the already-running app. Platform invariants surface via the develop-active atoms on the first call.\n")
		return sb.String()
	}

	sb.WriteString("\nInfrastructure is provisioned — runtimes are mounted and managed dependencies are running. No application code has been written yet, and nothing has been deployed. Dev-mode runtimes are idle (startWithoutCode); stage runtimes wait at READY_TO_DEPLOY for the first cross-deploy.\n\n")
	sb.WriteString("Next: `zerops_workflow action=\"start\" workflow=\"develop\"` — develop owns scaffolding, code, first deploy, and verify. Platform invariants (deploy-replaces-container, SSHFS mount path, sudo, build/run split) surface via the develop-active atoms on the first call.\n")

	return sb.String()
}

// planHasBuildFromGit reports whether any derived plan target is a
// buildFromGit recipe runtime. Replaces a brittle
// strings.Contains(ImportYAML, "buildFromGit") substring probe (B4): the
// derived plan carries the parsed-shape BuildFromGit per target, so the close
// tell derives from the same owner as the deployed-state stamp instead of a
// raw-YAML text match (which a comment could spoof).
func planHasBuildFromGit(plan *ServicePlan) bool {
	if plan == nil {
		return false
	}
	for _, t := range plan.Targets {
		if t.Runtime.BuildFromGit != "" {
			return true
		}
	}
	return false
}

// buildAdoptionTransitionMessage creates a summary for pure-adoption bootstraps.
// Existing services keep their code and configuration — no hello-world was deployed.
func buildAdoptionTransitionMessage(state *WorkflowState) string {
	var sb strings.Builder
	sb.WriteString(bootstrapCompleteMsg + " Services adopted — existing code and configuration preserved.\n\n## Services\n\n")
	writeServiceList(&sb, state.Bootstrap.Plan)
	sb.WriteString("\nNext: `zerops_workflow action=\"start\" workflow=\"develop\"` — develop reads each service's existing code and runs the iterate-edit-deploy loop. Platform invariants surface via the develop-active atoms on the first call.\n")

	return sb.String()
}

func writeServiceList(sb *strings.Builder, plan *ServicePlan) {
	for _, t := range plan.Targets {
		mode := t.Runtime.EffectiveMode()
		fmt.Fprintf(sb, "- **%s** (%s, %s mode)\n", t.Runtime.DevHostname, t.Runtime.Type, mode)
		if mode == topology.PlanModeStandard {
			// Render the stage type when it differs (cross-type pair, e.g.
			// nodejs dev → static stage) so the summary doesn't imply the dev
			// type for a stage that is actually something else.
			if t.Runtime.StageType != "" {
				fmt.Fprintf(sb, "  Stage: **%s** (%s)\n", t.Runtime.StageHostname(), t.Runtime.StageType)
			} else {
				fmt.Fprintf(sb, "  Stage: **%s**\n", t.Runtime.StageHostname())
			}
		}
		for _, d := range t.Dependencies {
			fmt.Fprintf(sb, "  - %s (%s)\n", d.Hostname, d.Type)
		}
	}
}
