package workflow

import (
	"fmt"
	"os"
	"strings"

	"github.com/zeropsio/zcp/internal/content"
	"github.com/zeropsio/zcp/internal/knowledge"
)

// getCoreSection extracts an H2 section from the core knowledge document.
// Recipe-local helper; when the recipe pipeline moves to the atom corpus
// this helper disappears with it.
func getCoreSection(kp knowledge.Provider, name string) string {
	doc, err := kp.Get("zerops://themes/core")
	if err != nil {
		return ""
	}
	sections := doc.H2Sections()
	if s, ok := sections[name]; ok {
		return s
	}
	return ""
}

// buildGuide assembles step-specific guidance with knowledge injection for recipe workflow.
// Follows deploy's pattern: own method on RecipeState, not via assembleGuidance (which is bootstrap-only).
//
// sessionID (v8.96 §6.2) is threaded down to buildSubStepGuide so the
// "Prior discoveries" block can be prepended to delegation briefs whose
// topic opts in via GuidanceTopic.IncludePriorDiscoveries. Empty sessionID
// is tolerated — buildSubStepGuide simply skips the prepend.
func (r *RecipeState) buildGuide(step string, iteration int, kp knowledge.Provider, sessionID string) string {
	// Iteration delta for deploy retries — replaces normal guidance.
	// Phase C: adaptive delta layers failure-pattern-specific guidance on
	// top of the shape-aware reminders.
	if iteration > 0 && step == RecipeStepDeploy {
		if delta := r.buildAdaptiveRetryDelta(step, iteration); delta != "" {
			return delta
		}
		// Fallback to non-adaptive deploy delta.
		if delta := buildDeployRetryDelta(r.Plan, iteration, r.lastAttestation()); delta != "" {
			return delta
		}
	}
	// Iteration delta for generate retries.
	if iteration > 0 && step == RecipeStepGenerate {
		if delta := r.buildAdaptiveRetryDelta(step, iteration); delta != "" {
			return delta
		}
		if delta := buildGenerateRetryDelta(r.Plan, r.lastAttestation()); delta != "" {
			return delta
		}
	}

	// Sub-step guidance: if a sub-step is active, return focused guidance
	// for that sub-step instead of the full skeleton.
	if r.CurrentStep < len(r.Steps) {
		currentStep := r.Steps[r.CurrentStep]
		if currentStep.hasSubSteps() {
			if !currentStep.allSubStepsComplete() {
				if subGuide := r.buildSubStepGuide(step, currentStep.currentSubStepName(), sessionID); subGuide != "" {
					// Prepend a sub-step progress line.
					progress := fmt.Sprintf("### Sub-step %d/%d: %s\n\n",
						currentStep.CurrentSubStep+1, len(currentStep.SubSteps), currentStep.currentSubStepName())
					return progress + subGuide
				}
				return buildSubStepMissingMappingNote(step, currentStep.currentSubStepName())
			}
			// Terminal-substep branch — every substep complete, agent
			// is waiting to call `complete step=X`. Emit a compact
			// prompt instead of falling through to the ~40 KB step
			// monolith (already delivered at step-transition).
			return buildAllSubstepsCompleteMessage(step, currentStep)
		}
	}

	// Static guidance from recipe.md.
	guide := resolveRecipeGuidance(step, r.Tier, r.Plan)

	// Knowledge injection.
	if extra := assembleRecipeKnowledge(step, r.Plan, r.DiscoveredEnvVars, kp); extra != "" {
		guide += "\n\n---\n\n" + extra
	}

	return guide
}

// lastAttestation returns the attestation from the most recently completed step.
func (r *RecipeState) lastAttestation() string {
	for i := r.CurrentStep - 1; i >= 0; i-- {
		if r.Steps[i].Attestation != "" {
			return r.Steps[i].Attestation
		}
	}
	return ""
}

// resolveRecipeGuidance extracts the appropriate static sections from recipe.md.
// tier is the session-level tier (set at RecipeStart, before the plan exists).
func resolveRecipeGuidance(step, tier string, plan *RecipePlan) string {
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		fmt.Fprintf(os.Stderr, "recipe guidance: failed to load recipe.md: %v\n", err)
		return ""
	}

	// Prefer plan tier (set after research), fall back to session tier (set at start).
	effectiveTier := tier
	if plan != nil && plan.Tier != "" {
		effectiveTier = plan.Tier
	}

	switch step {
	case RecipeStepResearch:
		// Showcase: send showcase section FIRST (overrides reference loading),
		// then minimal base (framework identity, build pipeline, decision tree).
		// The showcase section explicitly says "REPLACES the loading above."
		if effectiveTier == RecipeTierShowcase {
			showcase := extractSection(md, "research-showcase")
			minimal := extractSection(md, "research-minimal")
			return showcase + "\n\n---\n\n" + minimal
		}
		return extractSection(md, "research-minimal")

	case RecipeStepGenerate:
		// Phase A: return skeleton instead of composed blocks.
		// The skeleton references topics; the agent fetches them via zerops_guidance.
		if skeleton := extractSection(md, "generate-skeleton"); skeleton != "" {
			composed := composeSkeleton(skeleton, recipeGenerateTopics, plan)
			if eager := InjectEagerTopics(recipeGenerateTopics, plan); eager != "" {
				composed += "\n\n---\n\n" + eager
			}
			return composed
		}
		// Fallback: compose blocks as before (safety net during migration).
		var parts []string
		if body := extractSection(md, "generate"); body != "" {
			parts = append(parts, composeSection(body, recipeGenerateBlocks, plan))
		}
		if planNeedsFragmentsDeepDive(plan) {
			if s := extractSection(md, "generate-fragments"); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n\n---\n\n")
		}
		return ""

	case RecipeStepProvision:
		// Provision: composed through predicate-gated blocks (Phase 6).
		body := extractSection(md, "provision")
		return composeSection(body, recipeProvisionBlocks, plan)

	case RecipeStepDeploy:
		// Phase A: return skeleton instead of composed blocks.
		if skeleton := extractSection(md, "deploy-skeleton"); skeleton != "" {
			composed := composeSkeleton(skeleton, recipeDeployTopics, plan)
			if eager := InjectEagerTopics(recipeDeployTopics, plan); eager != "" {
				composed += "\n\n---\n\n" + eager
			}
			return composed
		}
		body := extractSection(md, "deploy")
		return composeSection(body, recipeDeployBlocks, plan)

	case RecipeStepFinalize:
		// Phase A: return skeleton instead of composed blocks.
		if skeleton := extractSection(md, "finalize-skeleton"); skeleton != "" {
			return composeSkeleton(skeleton, recipeFinalizeTopics, plan)
		}
		body := extractSection(md, "finalize")
		return composeSection(body, recipeFinalizeBlocks, plan)

	case RecipeStepClose:
		// Phase A: return skeleton instead of composed blocks.
		if skeleton := extractSection(md, "close-skeleton"); skeleton != "" {
			return composeSkeleton(skeleton, recipeCloseTopics, plan)
		}
		body := extractSection(md, "close")
		return composeSection(body, recipeCloseBlocks, plan)

	default:
		return extractSection(md, step)
	}
}

// assembleRecipeKnowledge gathers step-relevant knowledge from the knowledge store.
// Recipe workflow is about CREATING framework-specific knowledge — the agent discovers
// and documents framework pitfalls. But PLATFORM invariants (lifecycle phases, port
// ranges, hostname conventions, mode immutability) are NOT framework discoveries —
// they are Zerops rules and must be injected so the agent does not re-derive them
// wrong. Both schema field references AND Rules & Pitfalls are delivered; only the
// framework-specific layer is left for the agent to research.
// All knowledge retrieval is best-effort — errors are silently skipped.
func assembleRecipeKnowledge(step string, plan *RecipePlan, discoveredEnvVars map[string][]string, kp knowledge.Provider) string {
	if kp == nil {
		return ""
	}
	var parts []string

	// Research, Deploy, and Finalize receive no knowledge injection:
	//   - Research: agent fills a form from its own training data + on-demand
	//     zerops_knowledge calls.
	//   - Deploy: static guidance + the iteration loop cover the deploy flow.
	//   - Finalize: agent calls generate-finalize with structured envComments
	//     + projectEnvVariables — it does not hand-write YAML here. The
	//     import.yaml Schema injection that used to live at finalize was
	//     vestigial and Phase 7 of the reshuffle removed it.
	switch step {
	case RecipeStepProvision:
		// Provision-phase rules: service creation, dev/stage patterns, hostname
		// conventions, scaling/immutability, and the Env Vars H3. The Env Vars
		// block is byte-identical to the copy injected at Generate — provision
		// needs the rules when writing import.yaml; generate needs them when
		// writing zerops.yaml. Duplication is sanctioned to keep each step's
		// guidance self-contained.
		//
		// import.yaml Schema is NO LONGER eager-injected as of Phase 8. The
		// inline provision-schema-inline block in recipe.md covers the
		// workspace-import field set the agent actually writes, and points at
		// `zerops_knowledge scope="theme" query="import.yaml Schema"` for
		// exotic fields.
		if s := getCoreSection(kp, "Provision Rules"); s != "" {
			parts = append(parts, "## Provision Rules\n\n"+s)
		}

	case RecipeStepGenerate:
		// Recipe knowledge chain: the source of truth for "how to write zerops.yaml
		// for this framework." Direct predecessor: full content (working zerops.yaml
		// + gotchas). Earlier ancestors: gotchas only.
		if plan != nil {
			if chain := recipeKnowledgeChain(plan, kp); chain != "" {
				parts = append(parts, chain)
			}
		}
		// Discovered env vars: real variable names from provisioned services.
		if len(discoveredEnvVars) > 0 {
			parts = append(parts, formatEnvVarsForGuide(discoveredEnvVars))
		}
		// zerops.yaml Schema injection has been removed as of Phase 8. Hello-
		// world recipes (no chain predecessor) fall back to the inline
		// generate-schema-pointer block in recipe.md, which names the
		// zerops_knowledge call for on-demand retrieval. Recipes with a
		// predecessor already have a working zerops.yaml from the chain.
		//
		// Generate Rules (core.md lifecycle-phase H2) is intentionally NOT
		// injected at generate. The chain recipe demonstrates the same rules
		// in practice as a working zerops.yaml, and the agent already received
		// the rules it needed at provision (the Provision Rules slice). Re-
		// injecting at generate would triple-teach the same content (recipe.md
		// static text + chain example + Generate Rules) for ~11 KB of response
		// bloat. The agent can fetch a specific rule on demand via
		// `zerops_knowledge scope="theme" query="core"` if it needs one.
		//
		// Multi-base knowledge: platform-level explanation of build/run
		// asymmetry, zsc install, and the startWithoutCode trap. This is
		// complementary to the `multi-base-dev` topic (which covers the
		// recipe.md instructional block about dev-dep preinstall). The
		// knowledge snippet explains HOW the platform works; the topic
		// block explains WHAT to write. Both are needed.
		if needsMultiBaseGuidance(plan) {
			parts = append(parts, multiBaseGuidance())
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// buildDeployRetryDelta returns a focused delta for iteration > 0 at
// deploy. Layered:
//
//  1. Generic tier escalation via BuildIterationDelta — preserves the
//     bootstrap ladder (tier 1: diagnose+fix; tier 2: systematic check;
//     tier 3: stop and ask user).
//  2. Universal recipe-deploy retry reminders — fresh-container-on-
//     redeploy, DEPLOY_FAILED metadata reading, the zsc execOnce burn-
//     on-failure trap. Every recipe hits these regardless of shape.
//  3. Dimension-gated shape reminders. Each of these corresponds to a
//     real failure mode documented in the deploy section of recipe.md;
//     the delta surfaces the pointer, the full guide remains the source
//     of truth. Predicates keyed off recipe_plan_predicates.go.
//
// The dimensions covered:
//
//   - isDualRuntime — API-first deploy ordering (apidev before appdev)
//     and Step 3a log-reading across both containers.
//   - hasBundlerDevServer — bundler dev-server port collision (pgrep
//     before starting a second instance).
//   - hasSharedCodebaseWorker — worker dies with host on redeploy;
//     restart the queue consumer via the host's SSH session; logs live
//     on the host target's hostname.
//   - hasSeparateCodebaseWorker — worker owns its own container and
//     its own redeploy lifecycle; logs live on workerdev.
//   - isShowcase — sub-agent dispatch is one-shot post-verification; do
//     not respawn it inside the retry loop.
//
// Deliberately NOT hardcoded to specific frameworks or service
// hostnames: the text refers to `{host}dev` / `workerdev` as shape
// identifiers, not as literal plan values. The actual hostnames come
// from the agent's plan.
func buildDeployRetryDelta(plan *RecipePlan, iteration int, lastAttestation string) string {
	if iteration == 0 {
		return ""
	}

	var sb strings.Builder

	// Tier 1: generic escalation ladder (ITERATION n / PREVIOUS / tier guidance).
	if base := BuildIterationDelta(RecipeStepDeploy, iteration, nil, lastAttestation); base != "" {
		sb.WriteString(base)
		sb.WriteString("\n\n")
	}

	// Tier 2: universal recipe-deploy reminders.
	sb.WriteString("## Universal retry reminders\n\n")
	sb.WriteString("- **Redeploy = fresh container.** Every background process you started via SSH (asset dev server, queue consumer, any framework watcher) is gone after a redeploy. Re-run Step 2 in full — start ALL dev processes — before re-verifying.\n")
	sb.WriteString("- **Read `DEPLOY_FAILED` metadata, not buildLogs.** If the deploy response's status is `DEPLOY_FAILED`, the failing `initCommand` is named in `error.meta[].metadata.command`; its stderr is in runtime logs (`zerops_logs serviceHostname={service} severity=ERROR since=5m`), NOT in `buildLogs`.\n")
	sb.WriteString("- **The `zsc execOnce` burn-on-failure trap.** `execOnce` keys on `${appVersionId}`; if the first attempt crashed the container mid-`initCommand`, the retry with the same version ID will silently SKIP the command, and you'll see no seed/migration output in the retry logs. Recovery: touch a source file (whitespace change is enough) to force a new `appVersionId`, then redeploy.\n")

	// Shape-gated reminders. Each block is keyed off one predicate and
	// surfaces a failure mode that the full deploy guide teaches but the
	// generic tier ladder can't.
	if isDualRuntime(plan) {
		sb.WriteString("\n## Dual-runtime order\n\n")
		sb.WriteString("- Deploy order is non-negotiable: `apidev` first, then verify it (subdomain enable + API health check 200), THEN deploy `appdev`. The frontend's build bakes the API URL into its bundle at build time — if the API is down or unverified when `appdev` builds, the bundle ships broken and the fix-redeploy loop restarts here.\n")
		sb.WriteString("- Step 3a reads logs from BOTH `apidev` AND `appdev`. Migration and seed output typically live in the API container, not the frontend — look there when verifying `initCommands` ran.\n")
	}

	if hasBundlerDevServer(plan) {
		sb.WriteString("\n## Bundler dev server port collision\n\n")
		sb.WriteString("- Before starting the asset dev server, `pgrep` for an existing process. The first-deploy handler or a prior retry iteration may have left one running; a second instance silently falls back to an incremented port (Vite 5173 → 5174, webpack 8080 → 8081, etc.), and the public subdomain still routes to the original port. Symptom: new code doesn't take effect and the old behavior keeps appearing.\n")
		sb.WriteString("- If you need to restart the dev server after a config change, `pkill` it first, then start once. Don't skip the kill step.\n")
	}

	if hasSharedCodebaseWorker(plan) {
		sb.WriteString("\n## Shared-codebase worker\n\n")
		sb.WriteString("- Your worker process runs as an SSH-launched background task on the host target's dev container (the target named by `worker.sharesCodebaseWith`). When you redeploy the host target, BOTH the web server and the queue consumer die. Restart them in the Step 2 sequence — primary server first, then the worker. Skipping the worker restart is a common retry trap because the redeploy response looks green.\n")
		sb.WriteString("- Worker logs live in the HOST target's log stream (`zerops_logs serviceHostname={host target's dev hostname}`), not in a separate worker service. If you're searching for worker output on `workerdev`, you won't find it — there is no `workerdev` for this shape.\n")
	}

	if hasSeparateCodebaseWorker(plan) {
		sb.WriteString("\n## Separate-codebase worker\n\n")
		sb.WriteString("- `workerdev` is an independent container with its own zerops.yaml, its own mount, and its own redeploy lifecycle. Redeploying the app target does NOT touch the worker. Conversely, a fix applied only to the app won't reach the worker until you redeploy `workerdev` too.\n")
		sb.WriteString("- After any `workerdev` redeploy, restart the queue consumer SSH process on `workerdev` — same Step 2c shape as the initial deploy. Worker logs live on `zerops_logs serviceHostname=workerdev`, not on the app's log stream.\n")
	}

	if isShowcase(plan) {
		sb.WriteString("\n## Showcase sub-agent dispatch\n\n")
		sb.WriteString("- The feature sub-agent (Step 4b) is dispatched exactly once, AFTER `appdev` verification passes. If you're retrying a pre-verification failure, you have not yet reached the sub-agent phase — fix the deploy/verify issue first. If you're retrying a post-dispatch failure, do NOT respawn the sub-agent inside the retry loop; fix feature code yourself on the mount, redeploy, and re-run verification.\n")
		sb.WriteString("- The browser walk (Step 4c) runs AFTER the sub-agent completes. A pre-walk failure means the walk is not yet due; a walk failure means fix-on-mount + redeploy + re-walk, and that counts against the 3-iteration budget.\n")
	}

	sb.WriteString("\n## Source of truth\n\n")
	sb.WriteString("The full Dev deployment flow block in this session's deploy guide is authoritative for step ordering and command shapes. This delta surfaces only the retry-specific failure modes — go back to the full guide if you need the step details.\n")

	return sb.String()
}

// buildGenerateRetryDelta returns a focused delta for iteration > 0 at
// generate. The agent has already read the full generate composition once;
// on retry they need (a) a reminder of what they attested to last time,
// (b) shape-specific failure modes filtered through the plan's predicates,
// and (c) a pointer back to the chain recipe as the source of truth for
// zerops.yaml shape.
//
// Deliberately NOT using BuildIterationDelta — that's a generic escalation
// emitter suited for deploy where "try again with more focus" is the right
// posture. Generate retries benefit from shape-specific failure-mode
// reminders keyed off recipe_plan_predicates.go.
func buildGenerateRetryDelta(plan *RecipePlan, lastAttestation string) string {
	var sb strings.Builder
	sb.WriteString("## Generate — Retry\n\n")
	sb.WriteString("You've already read the full generate guide this session. This is a focused delta.\n\n")

	if lastAttestation != "" {
		sb.WriteString("### What you attested to last iteration\n\n```\n")
		sb.WriteString(lastAttestation)
		sb.WriteString("\n```\n\n")
	}

	sb.WriteString("### Common retry causes\n\n")
	sb.WriteString("- Comment ratio <30% in zerops.yaml — recount, aim for 35%.\n")
	sb.WriteString("- Env var references used guessed names — the provision-step attestation has the authoritative list.\n")
	sb.WriteString("- Missing `setup: dev` block for at least one deployable target.\n")
	sb.WriteString("- dev and prod envVariables bit-identical — mode flags must differ (a structural check fails otherwise).\n")
	sb.WriteString("- README missing one of the three extract fragments.\n")

	if isDualRuntime(plan) {
		sb.WriteString("- Dual-runtime URL references in `run.envVariables` using hardcoded hosts instead of `${STAGE_*}` / `${DEV_*}`.\n")
	}
	if hasBundlerDevServer(plan) {
		sb.WriteString("- Dev-server host-check not updated — framework config still rejects `.zerops.app`.\n")
	}
	if hasSharedCodebaseWorker(plan) {
		sb.WriteString("- Missing `setup: worker` block in the host target's zerops.yaml.\n")
	}
	if needsMultiBaseGuidance(plan) {
		sb.WriteString("- Dev `buildCommands` missing the secondary-runtime dependency install (asset pipeline).\n")
	}

	sb.WriteString("\n### Source of truth\n\n")
	sb.WriteString("The injected chain recipe's `## zerops.yaml template` section (from your first read-through this session) is authoritative for shape. Re-read it and diff against your output before submitting.\n")
	return sb.String()
}

// buildAdaptiveRetryDelta returns a failure-pattern-aware retry delta.
// Phase C: instead of generic retry reminders, surfaces the specific sub-step
// failures the agent hit and the topics they missed. Returns "" if no
// failure patterns are recorded (falls through to the non-adaptive delta).
func (r *RecipeState) buildAdaptiveRetryDelta(step string, iteration int) string {
	if len(r.FailurePatterns) == 0 {
		return ""
	}

	// Filter to failures relevant to this step.
	var relevant []FailurePattern
	for _, fp := range r.FailurePatterns {
		// Sub-step names implicitly belong to their parent step.
		if subStepToTopic(step, fp.SubStep, r.Plan) != "" {
			relevant = append(relevant, fp)
		}
	}
	if len(relevant) == 0 {
		return ""
	}

	var sb strings.Builder

	// Generic tier escalation.
	if base := BuildIterationDelta(step, iteration, nil, r.lastAttestation()); base != "" {
		sb.WriteString(base)
		sb.WriteString("\n\n")
	}

	// Failure-specific guidance.
	for _, fp := range relevant {
		sb.WriteString(fmt.Sprintf("## Previous failure: %s\n\n", fp.SubStep))
		for _, issue := range fp.Issues {
			sb.WriteString(fmt.Sprintf("- %s\n", issue))
		}
		topicID := subStepToTopic(step, fp.SubStep, r.Plan)
		if topicID != "" {
			sb.WriteString(fmt.Sprintf("\nFetch updated rules: `zerops_guidance topic=%q`\n", topicID))
		}
	}

	// Topics the agent didn't fetch but should have.
	missing := r.missingCriticalTopics(step)
	if len(missing) > 0 {
		sb.WriteString("\n## Topics you may have missed\n\n")
		for _, t := range missing {
			sb.WriteString(fmt.Sprintf("- `zerops_guidance topic=%q` — %s\n", t.ID, t.Description))
		}
	}

	return sb.String()
}

// missingCriticalTopics returns topics the agent should have fetched for
// this step but didn't, based on GuidanceAccess records.
func (r *RecipeState) missingCriticalTopics(step string) []*GuidanceTopic {
	accessed := make(map[string]bool, len(r.GuidanceAccess))
	for _, entry := range r.GuidanceAccess {
		accessed[entry.TopicID] = true
	}

	topics := AllTopicsForStep(step)
	var missing []*GuidanceTopic
	for _, t := range topics {
		if accessed[t.ID] {
			continue
		}
		// Only suggest topics whose predicates fire for this plan.
		if t.Predicate != nil && !t.Predicate(r.Plan) {
			continue
		}
		missing = append(missing, t)
	}
	return missing
}

// buildSubStepGuide returns the relevant topic content for a sub-step.
// This is the focused guidance the agent receives when working within
// sub-step orchestration — pre-loaded topic content instead of requiring
// the agent to call zerops_guidance manually.
//
// sessionID (v8.96 §6.2) is the recipe session ID. When the resolved
// topic opts into IncludePriorDiscoveries, a "Prior discoveries" block
// of upstream-recorded downstream-scoped facts is prepended to the
// returned content. Empty sessionID means "skip the prepend" (test
// fixtures, sessions with no upstream substep history).
func (r *RecipeState) buildSubStepGuide(step, subStep, sessionID string) string {
	topicID := subStepToTopic(step, subStep, r.Plan)
	if topicID == "" {
		return ""
	}
	resolved, err := ResolveTopic(topicID, r.Plan)
	if err != nil || resolved == "" {
		return ""
	}
	if TopicIncludesPriorDiscoveries(topicID) {
		if block := BuildPriorDiscoveriesBlock(sessionID, subStep); block != "" {
			resolved = block + "\n\n---\n\n" + resolved
		}
	}
	return resolved
}

// buildAllSubstepsCompleteMessage returns a compact prompt for the state
// where every sub-step of `step` is marked complete and the agent is
// waiting to call `complete step=<step>` to trigger the full-step checks.
// Prevents fall-through to the ~40 KB step monolith that was already
// delivered at the step-transition response.
func buildAllSubstepsCompleteMessage(step string, currentStep RecipeStep) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### All sub-steps complete (%d/%d)\n\n",
		len(currentStep.SubSteps), len(currentStep.SubSteps))
	fmt.Fprintf(&b, "Every sub-step of `%s` is marked complete:\n\n", step)
	for _, ss := range currentStep.SubSteps {
		fmt.Fprintf(&b, "- %s\n", ss.Name)
	}
	fmt.Fprintf(&b, "\n**Next action**: call `zerops_workflow action=\"complete\" step=%q attestation=\"<summary of the step\">` to trigger the full-step checks and advance to the next step.\n\n", step)
	b.WriteString("The full `" + step + "` guide was delivered at the step-transition response; it is already in your context. If you need to re-consult specific topics, fetch them via `zerops_guidance topic=\"<id>\"` on demand — do not expect the full guide to be re-delivered here.\n")
	return b.String()
}

// buildSubStepMissingMappingNote is a defensive fallback for the case
// where a substep has no subStepToTopic mapping OR its topic resolved to
// empty content. Returns a short diagnostic rather than flooding the
// context with the full step monolith.
func buildSubStepMissingMappingNote(step, subStep string) string {
	return fmt.Sprintf(
		"### Sub-step: %s\n\n"+
			"(No focused guidance topic is currently mapped for this sub-step. "+
			"The step-level guide was delivered at the step-transition response "+
			"and is still in your context. If you need specific guidance for "+
			"`%s`, fetch a relevant topic via `zerops_guidance topic=\"<id>\"`.)\n\n"+
			"**Next action**: proceed with the sub-step work as described in the step guide, then call `zerops_workflow action=\"complete\" step=%q substep=%q attestation=\"<what you did>\"`.\n",
		subStep, subStep, step, subStep,
	)
}

// Topic ID constants used in sub-step → topic mapping.
const topicDeployFlow = "deploy-flow"

// subStepToTopic maps a sub-step name to the primary topic the agent needs.
// The plan is used for shape-dependent routing (e.g., app-code maps to
// dashboard-skeleton for showcase, execution-order for other tiers).
func subStepToTopic(step, subStep string, plan *RecipePlan) string {
	switch step {
	case RecipeStepGenerate:
		switch subStep {
		case SubStepScaffold:
			return "where-to-write"
		case SubStepAppCode:
			if isShowcase(plan) {
				return "dashboard-skeleton"
			}
			return "execution-order"
		case SubStepSmokeTest:
			return "smoke-test"
		case SubStepZeropsYAML:
			return "zerops-yaml-rules"
		}
	case RecipeStepDeploy:
		switch subStep {
		case SubStepDeployDev, SubStepStartProcs, SubStepInitCommands:
			return topicDeployFlow
		case SubStepVerifyDev:
			return "deploy-target-verification"
		case SubStepSubagent:
			return "subagent-brief"
		case SubStepSnapshotDev:
			// Durability re-deploy: same flow as initial deploy-dev,
			// but with the feature sub-agent's output now on-disk.
			return topicDeployFlow
		case SubStepFeatureSweepDev:
			return "feature-sweep-dev"
		case SubStepBrowserWalk:
			return "browser-walk"
		case SubStepCrossDeploy:
			return "stage-deploy"
		case SubStepVerifyStage:
			return "deploy-target-verification"
		case SubStepFeatureSweepStage:
			return "feature-sweep-stage"
		case SubStepReadmes:
			// v8.94: content-authoring-brief replaces readme-fragments for
			// showcase recipes. The new brief embeds surface contracts + fact
			// classification + citation map — a rubric the dispatched
			// sub-agent evaluates every fact against before routing it to a
			// surface. readme-fragments stays available as an on-demand
			// topic for the fragment marker-format reference; content-
			// authoring-brief is showcase-predicated (the full set of six
			// surfaces only exists on showcase recipes), so minimal/hello-
			// world tiers fall back to readme-fragments as before.
			if isShowcase(plan) {
				return "content-authoring-brief"
			}
			return "readme-fragments"
		}
	}
	return ""
}

// composeSkeleton takes a skeleton template and filters [topic: id] markers
// based on the topic registry's predicates against the plan. Lines containing
// a topic reference whose predicate is false are removed. Numbered list items
// are renumbered after filtering to avoid gaps. The skeleton is returned as a
// compact, imperative document that lists execution steps and references
// topics by ID.
func composeSkeleton(skeleton string, topics []*GuidanceTopic, plan *RecipePlan) string {
	// Build a predicate lookup for the topics relevant to this skeleton.
	predicates := make(map[string]func(*RecipePlan) bool, len(topics))
	for _, t := range topics {
		predicates[t.ID] = t.Predicate
	}

	var out []string
	for line := range strings.SplitSeq(skeleton, "\n") {
		// Check if this line contains a [topic: ...] marker.
		if idx := strings.Index(line, "[topic: "); idx >= 0 {
			end := strings.Index(line[idx:], "]")
			if end > 0 {
				topicID := strings.TrimSpace(line[idx+8 : idx+end])
				pred, known := predicates[topicID]
				if known && pred != nil && !pred(plan) {
					// Predicate is false — skip this line.
					continue
				}
			}
		}
		out = append(out, line)
	}

	// Renumber top-level numbered list items (lines matching "N. ") to
	// close gaps from filtered lines. Sub-items ("   - ...") are untouched.
	n := 0
	for i, line := range out {
		if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			// Find the ". " after the number.
			if dotIdx := strings.Index(line, ". "); dotIdx > 0 && dotIdx <= 2 {
				n++
				out[i] = fmt.Sprintf("%d%s", n, line[dotIdx:])
			}
		}
	}

	return strings.Join(out, "\n")
}

// planNeedsFragmentsDeepDive reports whether a plan should receive the
// README fragment writing-style deep-dive (generate-fragments section).
// Hello-world recipes have a simple 1-section README that the chain
// recipe demonstrates in full — the 6KB deep-dive is dead weight.
//
// Same nil-plan default as planNeedsDashboardSpec: include when unknown.
func planNeedsFragmentsDeepDive(plan *RecipePlan) bool {
	if plan == nil {
		return true
	}
	return !strings.HasSuffix(plan.Slug, "-hello-world")
}
