package tools

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/workflow"
)

// buildRecipeStepChecker returns a step checker for the given recipe step.
// kp is the knowledge provider from the engine — used by generate- and
// deploy-step checks that need to read the injected chain recipe (e.g. the
// predecessor-as-floor knowledge-base check). Pass nil in tests that
// don't need chain access; checks that depend on it no-op gracefully.
//
// Deploy-step checker: fires on full-step completion and validates the
// per-codebase READMEs written during the post-verify `readmes` sub-step.
// v14 moved README content from generate → post-deploy so the gotchas
// section can narrate debug experience rather than speculation; the
// validation moved with it. Generate is now purely a zerops.yaml +
// scaffold sanity check — no README content is inspected there.
// factsLogPathFn resolves the canonical facts-log path for the active
// recipe session. Passed into buildRecipeStepChecker by the handler so
// deploy-step checks (the content-manifest completeness sub-check in
// particular) can cross-reference the writer's manifest entries against
// every distinct FactRecord.Title the agent recorded. Nil or an empty
// string from the resolver → sub-check D passes with a skip note (test
// context or no active session).
func buildRecipeStepChecker(ctx context.Context, step, _, stateDir string, schemaCache *schema.Cache, kp knowledge.Provider, factsLogPathFn func() string) workflow.RecipeStepChecker {
	switch step {
	case workflow.RecipeStepGenerate:
		var validFields *schema.ValidFields
		if schemaCache != nil {
			if schemas := schemaCache.Get(ctx); schemas != nil && schemas.ZeropsYml != nil {
				validFields = schema.ExtractValidFields(schemas.ZeropsYml)
			}
		}
		return checkRecipeGenerate(stateDir, validFields, kp)
	case workflow.RecipeStepDeploy:
		return checkRecipeDeployReadmes(stateDir, kp, factsLogPathFn)
	case workflow.RecipeStepFinalize:
		return checkRecipeFinalizeFromState(stateDir)
	}
	return nil
}

// checkRecipeFinalizeFromState returns a finalize checker that derives outputDir
// from stateDir (project root) or falls back to state.OutputDir if set.
func checkRecipeFinalizeFromState(stateDir string) workflow.RecipeStepChecker {
	return func(ctx context.Context, plan *workflow.RecipePlan, state *workflow.RecipeState) (*workflow.StepCheckResult, error) {
		// Use explicit OutputDir if set, otherwise derive from stateDir.
		outputDir := ""
		if state != nil && state.OutputDir != "" {
			outputDir = state.OutputDir
		} else if stateDir != "" {
			outputDir = projectRootFromState(stateDir)
		}
		checker := checkRecipeFinalize(outputDir)
		return checker(ctx, plan, state)
	}
}

// Fragment marker patterns (errata E5: colon separator, trailing hash).
var placeholderRe = regexp.MustCompile(`(?i)(PLACEHOLDER_\w+|<your-[^>]+>|TODO\b)`)

// Required fragment names for recipe READMEs.
var requiredFragments = []string{"integration-guide", "knowledge-base", "intro"}

// checkRecipeGenerate validates the generate step for recipe workflow.
// Extends bootstrap's checkGenerate with recipe-specific fragment quality checks.
// validFields, when non-nil, enables zerops.yaml field validation against the live JSON schema.
// kp, when non-nil, enables the predecessor-as-floor check on each codebase's README.
func checkRecipeGenerate(stateDir string, validFields *schema.ValidFields, kp knowledge.Provider) workflow.RecipeStepChecker {
	return func(ctx context.Context, plan *workflow.RecipePlan, state *workflow.RecipeState) (*workflow.StepCheckResult, error) {
		if plan == nil {
			return nil, nil
		}

		projectRoot := projectRootFromState(stateDir)

		var checks []workflow.StepCheck

		// kp is unused at generate time now that the predecessor-floor
		// check moved to the deploy-step checker alongside the rest of
		// the README content validation. Keeping the parameter so the
		// signature matches buildRecipeStepChecker's switch — future
		// generate-level checks that need knowledge access land here.
		_ = kp

		// Collect ALL non-worker runtime targets (frontend + API in dual-runtime recipes).
		var appTargets []workflow.RecipeTarget
		for _, t := range plan.Targets {
			if workflow.IsRuntimeType(t.Type) && !t.IsWorker {
				appTargets = append(appTargets, t)
			}
		}
		if len(appTargets) == 0 && len(plan.Targets) > 0 {
			appTargets = []workflow.RecipeTarget{plan.Targets[0]}
		}

		// Separate-codebase workers ship their own README and must also
		// clear the predecessor-floor check. Shared-codebase workers
		// (SharesCodebaseWith != "") live inside the host codebase and
		// don't have a standalone README, so they're skipped — the host's
		// README is already covered by the appTargets loop below.
		var workerTargets []workflow.RecipeTarget
		for _, t := range plan.Targets {
			if workflow.IsRuntimeType(t.Type) && t.IsWorker && t.SharesCodebaseWith == "" {
				workerTargets = append(workerTargets, t)
			}
		}

		// Check zerops.yaml for each app target.
		for _, appTarget := range appTargets {
			checks = append(checks, checkRecipeGenerateCodebase(ctx, projectRoot, appTarget, plan, validFields, false)...)
		}

		// v8.94: separate-codebase worker targets have their OWN zerops.yaml
		// on their OWN mount (workerdev/). v28 shipped nine self-shadow lines
		// in workerdev/zerops.yaml because this loop did not exist — worker
		// targets were collected into workerTargets and then discarded. The
		// same floors that apply to app targets (yml exists, dev+prod setups,
		// schema-field validity, env_self_shadow, no-premature-README) apply
		// here. Shared-codebase workers are excluded at collection time
		// (SharesCodebaseWith != "") — their setup lives inside the host
		// codebase's zerops.yaml, already covered by the app-target loop's
		// checkRecipeSetups → app_worker_setup check.
		for _, workerTarget := range workerTargets {
			checks = append(checks, checkRecipeGenerateCodebase(ctx, projectRoot, workerTarget, plan, validFields, true)...)
		}

		// C-6: symbol-contract env-var consistency. Recipe-wide cross-
		// codebase diff against plan.SymbolContract.EnvVarsByKind. Closes
		// the v34 DB_PASS vs DB_PASSWORD class per principle P3. Fires at
		// generate-complete (here) AND at deploy-complete (in
		// checkRecipeDeployReadmes) — check-rewrite.md §16 adds it at two
		// trigger points because the v34 class surfaced both at initial
		// scaffold and after later inline edits that re-introduced the
		// divergence.
		checks = append(checks, checkSymbolContractEnvVarConsistency(projectRoot, plan.SymbolContract)...)

		// v8.97 Fix 4: stamp surface-derived coupling hints on every
		// failed check with a populated ReadSurface before computing the
		// pass status. Coupling is derived from shared ReadSurface, not
		// from a hand-maintained cluster table.
		checks = workflow.StampCoupling(checks)
		allPassed := checksAllPassed(checks)
		summary := "recipe generate checks passed"
		if !allPassed {
			summary = "recipe generate checks failed"
		}
		result := &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}
		workflow.AnnotateNextRoundPrediction(result)
		return result, nil
	}
}

// checkRecipeGenerateCodebase runs the per-codebase generate-time floors for
// a single runtime target (app or separate-codebase worker). Returns the full
// check set so the caller can accumulate across targets. The workerOnly flag
// skips checks that don't apply to separate-codebase workers (currently: the
// shared-worker setup expectation inside checkRecipeSetups is still valid
// because TargetHostsSharedWorker returns false for a worker target itself,
// so no branch is needed there — the flag is reserved for future divergence).
func checkRecipeGenerateCodebase(ctx context.Context, projectRoot string, target workflow.RecipeTarget, plan *workflow.RecipePlan, validFields *schema.ValidFields, workerOnly bool) []workflow.StepCheck {
	_ = workerOnly
	hostname := target.Hostname

	// Try mount paths: {hostname}dev (standard mode), {hostname} (bare),
	// then project root. Separate-codebase workers use {hostname}dev
	// (workerdev/) — same convention as apps in standard mode.
	ymlDir := projectRoot
	for _, candidate := range []string{hostname + "dev", hostname} {
		mountPath := filepath.Join(projectRoot, candidate)
		if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
			ymlDir = mountPath
			break
		}
	}

	doc, parseErr := ops.ParseZeropsYml(ymlDir)
	if parseErr != nil {
		return []workflow.StepCheck{{
			Name: hostname + "_zerops_yml_exists", Status: statusFail,
			Detail: fmt.Sprintf("zerops.yaml not found for %s: %v", hostname, parseErr),
		}}
	}

	checks := []workflow.StepCheck{{
		Name: hostname + "_zerops_yml_exists", Status: statusPass,
	}}
	checks = append(checks, checkRecipeSetups(doc, target, plan)...)

	// Validate zerops.yaml fields against the live JSON schema.
	checks = append(checks, checkZeropsYmlFields(ctx, ymlDir, validFields)...)

	// v8.94: env_self_shadow floor covers every runtime target's zerops.yaml,
	// not just the bootstrap flow's checkGenerateEntry path. v28 evidence:
	// workerdev shipped nine self-shadow lines and zero worker-prefixed
	// checks fired at generate-complete because the recipe path never called
	// checkEnvSelfShadow. Runs against both dev and prod entries — a shadow
	// in either breaks cross-service env resolution when that setup deploys.
	for _, setupName := range []string{workflow.RecipeSetupDev, workflow.RecipeSetupProd} {
		entry := doc.FindEntry(setupName)
		if entry == nil {
			continue // missing setup already reported by checkRecipeSetups
		}
		shadowCheck := checkEnvSelfShadow(ctx, hostname, entry)
		// Dev + prod share the same check name; keep the fail so the agent
		// sees the list of offenders once. A dev fail plus a prod pass should
		// still surface as a fail.
		if existing := findCheck(checks, hostname+"_env_self_shadow"); existing != nil {
			if shadowCheck.Status == statusFail {
				*existing = shadowCheck
			}
		} else {
			checks = append(checks, shadowCheck)
		}
	}

	// README content validation moves to the deploy-step checker. v14 forbids
	// README.md on the mount at generate-complete time; detect and surface
	// so the agent deletes the scaffolder's stub before the checker re-runs.
	readmePath := filepath.Join(ymlDir, "README.md")
	if _, err := os.Stat(readmePath); err == nil {
		checks = append(checks, workflow.StepCheck{
			Name: hostname + "_no_premature_readme", Status: statusFail,
			Detail: fmt.Sprintf("%s/README.md exists at generate-complete time — v14 moves README writing to the post-deploy `readmes` sub-step. Delete %s; the agent will write the real README after verify-stage using lived debug experience for the gotchas section.", hostname, readmePath),
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: hostname + "_no_premature_readme", Status: statusPass,
		})
	}

	// v8.95: scaffold-artifact leak check. Walks the codebase mount for
	// common scaffold-phase residue (scripts/, verify/, preflight/,
	// scaffold-*) that isn't referenced by the codebase's own zerops.yaml.
	// v29's apidev shipped scripts/preship.sh because no rule forbade it;
	// this turns the rule into a hard gate at generate-complete. Pass both
	// the parsed doc (for structured reference scan) and the raw YAML (for
	// substring fallback on unmodeled fields like initCommands).
	rawYAMLData, _ := os.ReadFile(filepath.Join(ymlDir, "zerops.yaml"))
	if rawYAMLData == nil {
		rawYAMLData, _ = os.ReadFile(filepath.Join(ymlDir, "zerops.yml"))
	}
	checks = append(checks, checkScaffoldArtifactLeak(ymlDir, doc, string(rawYAMLData), hostname)...)

	// C-6: visual-style ASCII-only check. Reads {hostname}dev/zerops.yaml
	// and fails on Unicode Box Drawing codepoints (U+2500..U+257F). Closes
	// the v33 class per principle P8 — visual separators are ASCII-only.
	checks = append(checks, checkVisualStyleASCIIOnly(ymlDir, hostname)...)

	return checks
}

// findCheck returns a pointer to the first check with the given name, or nil.
// Used by checkRecipeGenerateCodebase to merge dev+prod env_self_shadow
// results into a single accumulator entry.
func findCheck(checks []workflow.StepCheck, name string) *workflow.StepCheck {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}

// checkRecipeDeployReadmes validates per-codebase README content at
// deploy-step completion. This used to live inside checkRecipeGenerate,
// but v14 moved README writing out of generate entirely: the scaffold
// sub-agent brief forbids READMEs, main-agent generate writes only
// zerops.yaml + app code + smoke test, and the `readmes` sub-step at
// the end of deploy is the only place READMEs get written. The check
// path follows the content — fragments, integration-guide code blocks,
// comment specificity, predecessor-floor, and knowledge-base authenticity
// all fire here. Failing any of them holds the deploy step in-progress
// so the agent can iterate on the README narration without rolling back
// the infrastructure work.
//
// kp is the knowledge provider used to pull the direct-predecessor
// recipe's gotcha stems for the predecessor-floor check. Nil kp in test
// contexts no-ops that check gracefully.
//
// factsLogPathFn (v8.95) resolves the writer subagent's session facts-log
// path so the content-manifest completeness sub-check can cross-reference
// every recorded FactRecord.Title against the manifest entries. Pass nil
// for test contexts — sub-check D handles that by passing with a skip note.
func checkRecipeDeployReadmes(stateDir string, kp knowledge.Provider, factsLogPathFn func() string) workflow.RecipeStepChecker {
	return func(ctx context.Context, plan *workflow.RecipePlan, _ *workflow.RecipeState) (*workflow.StepCheckResult, error) {
		if plan == nil {
			return nil, nil
		}
		projectRoot := projectRootFromState(stateDir)
		predecessorStems := workflow.PredecessorGotchaStems(plan, kp)

		var appTargets []workflow.RecipeTarget
		for _, t := range plan.Targets {
			if workflow.IsRuntimeType(t.Type) && !t.IsWorker {
				appTargets = append(appTargets, t)
			}
		}
		if len(appTargets) == 0 && len(plan.Targets) > 0 {
			appTargets = []workflow.RecipeTarget{plan.Targets[0]}
		}
		var workerTargets []workflow.RecipeTarget
		for _, t := range plan.Targets {
			if workflow.IsRuntimeType(t.Type) && t.IsWorker && t.SharesCodebaseWith == "" {
				workerTargets = append(workerTargets, t)
			}
		}

		var checks []workflow.StepCheck
		// Collect the README content per hostname while iterating so the
		// cross-README dedup check (below) can run without re-reading from
		// disk. Worker and app codebases both contribute; both are subject
		// to the uniqueness rule — a NATS fact in apidev's README cannot
		// also appear in workerdev's.
		readmesByHost := map[string]string{}

		for _, target := range appTargets {
			checks = append(checks, checkCodebaseReadme(ctx, projectRoot, target, plan, predecessorStems, false, readmesByHost)...)
		}
		for _, target := range workerTargets {
			checks = append(checks, checkCodebaseReadme(ctx, projectRoot, target, plan, predecessorStems, true, readmesByHost)...)
		}

		// Cross-README gotcha-stem uniqueness: forces each distinct fact
		// to live in exactly one codebase's published README. Without
		// this check, each per-codebase authenticity floor rewarded the
		// agent for stamping the same 3 gotchas into every README —
		// v15's nestjs-showcase had NATS/SSHFS/execOnce appearing in both
		// api + worker READMEs. Returns a single check name ("cross_
		// readme_gotcha_uniqueness") so the failure is scoped to the
		// whole recipe, not any one codebase.
		checks = append(checks, checkCrossReadmeGotchaUniqueness(readmesByHost)...)

		// v8.95: content-manifest enforcement. The writer subagent emits
		// ZCP_CONTENT_MANIFEST.json at the recipe root before returning;
		// this check reads it and enforces classification consistency
		// (DISCARD-class routing), manifest honesty (discarded facts
		// must not appear as gotchas), and completeness (every distinct
		// FactRecord.Title must have a manifest entry). factsLogPathFn
		// resolves the session's facts-log path — nil/empty → sub-check
		// D skips gracefully.
		factsLogPath := ""
		if factsLogPathFn != nil {
			factsLogPath = factsLogPathFn()
		}
		checks = append(checks, checkWriterContentManifest(ctx, projectRoot, readmesByHost, factsLogPath)...)

		// C-6: manifest_route_to_populated. Every manifest entry must carry
		// a non-empty routed_to matching the FactRouteTo* enum. Closes
		// v34's DB_PASS class where claude_md-routed facts leaked into the
		// published gotcha list because the drift was visible in the
		// manifest but nothing gated on it. Principle P5.
		checks = append(checks, checkManifestRouteToPopulated(filepath.Join(projectRoot, opschecks.ManifestFileName))...)

		// C-6: symbol-contract env-var consistency at deploy.readmes.
		// Second firing point per check-rewrite.md §16 — the first is at
		// generate-complete in checkRecipeGenerate. After inline edits
		// during deploy rounds may re-introduce cross-scaffold env-var
		// divergence that the initial scaffold got right.
		checks = append(checks, checkSymbolContractEnvVarConsistency(projectRoot, plan.SymbolContract)...)

		// v8.97 Fix 4: stamp surface-derived coupling hints.
		checks = workflow.StampCoupling(checks)
		allPassed := checksAllPassed(checks)
		summary := "recipe deploy README checks passed"
		if !allPassed {
			summary = "recipe deploy README checks failed"
		}
		result := &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}
		workflow.AnnotateNextRoundPrediction(result)
		return result, nil
	}
}

// checkCodebaseReadme runs every README content check against a single
// codebase mount. workerOnly=true skips checks that don't apply to worker
// codebases (integration-guide code-block floor, comment specificity — both
// app-facing concerns). The predecessor-floor check runs on every codebase
// regardless, so a dual-runtime recipe can't pile every net-new gotcha
// into one README while the others clone the predecessor.
//
// readmesByHost is an output map collected by checkRecipeDeployReadmes
// so the cross-README dedup check can run once across all codebases
// without re-reading files from disk. Passing it through here keeps the
// filesystem walk shape identical to the existing iteration loop.
func checkCodebaseReadme(ctx context.Context, projectRoot string, target workflow.RecipeTarget, plan *workflow.RecipePlan, predecessorStems []string, workerOnly bool, readmesByHost map[string]string) []workflow.StepCheck {
	hostname := target.Hostname
	ymlDir := projectRoot
	for _, candidate := range []string{hostname + "dev", hostname} {
		mountPath := filepath.Join(projectRoot, candidate)
		if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
			ymlDir = mountPath
			break
		}
	}

	readmePath := filepath.Join(ymlDir, "README.md")
	readmeContent, readErr := os.ReadFile(readmePath)
	if readErr != nil {
		// Still run the CLAUDE.md check — a missing README.md does not
		// imply CLAUDE.md is also missing, and reporting both failures
		// on the same iteration surfaces the full picture instead of
		// making the agent discover them one at a time.
		claudeChecks := checkCLAUDEMdExists(projectRoot, target, plan)
		checks := make([]workflow.StepCheck, 0, 1+len(claudeChecks))
		checks = append(checks, workflow.StepCheck{
			Name:   hostname + "_readme_exists",
			Status: statusFail,
			Detail: fmt.Sprintf("README.md not found at %s — write it during the deploy `readmes` sub-step using the exact fragment markers shown in topic=\"readme-fragments\". The required markers are `<!-- #ZEROPS_EXTRACT_START:intro# -->` / `<!-- #ZEROPS_EXTRACT_END:intro# -->` (and the same pattern for `integration-guide` and `knowledge-base`). Do not invent your own marker syntax.", readmePath),
		})
		checks = append(checks, claudeChecks...)
		return checks
	}
	if readmesByHost != nil {
		readmesByHost[hostname] = string(readmeContent)
	}

	var checks []workflow.StepCheck
	checks = append(checks, workflow.StepCheck{
		Name: hostname + "_readme_exists", Status: statusPass,
	})
	checks = append(checks, checkReadmeFragments(string(readmeContent), hostname)...)

	if !workerOnly {
		if igContent := extractFragmentContent(string(readmeContent), "integration-guide"); igContent != "" {
			if yamlBlock := extractYAMLBlock(igContent); yamlBlock != "" {
				specChecks := checkCommentSpecificity(ctx, yamlBlock, plan)
				for i := range specChecks {
					specChecks[i].Name = hostname + "_" + specChecks[i].Name
				}
				checks = append(checks, specChecks...)
			}
		}
		codeChecks := checkIntegrationGuideCodeBlocks(ctx, string(readmeContent), plan)
		for i := range codeChecks {
			codeChecks[i].Name = hostname + "_" + codeChecks[i].Name
		}
		checks = append(checks, codeChecks...)

		perItemChecks := checkIntegrationGuidePerItemCodeBlock(ctx, string(readmeContent), plan)
		for i := range perItemChecks {
			perItemChecks[i].Name = hostname + "_" + perItemChecks[i].Name
		}
		checks = append(checks, perItemChecks...)
	}

	floorChecks := checkKnowledgeBaseExceedsPredecessor(ctx, string(readmeContent), plan, predecessorStems, hostname)
	for i := range floorChecks {
		floorChecks[i].Name = hostname + "_" + floorChecks[i].Name
	}
	checks = append(checks, floorChecks...)

	// Gotcha-restates-guide: a gotcha whose normalized stem matches an
	// integration-guide H3 heading (other than the boilerplate zerops.yaml
	// block) is restatement-bloat. Must be rewritten to focus on symptom,
	// not topic, or deleted. Runs for every codebase — workers can restate
	// their own guide items too.
	checks = append(checks, checkGotchaRestatesGuide(hostname, string(readmeContent))...)

	// Worker production correctness: separate-codebase worker targets must
	// cover queue-group semantics and graceful shutdown in their README
	// gotchas. These are Zerops-specific in their consequence (`minContainers
	// > 1` replica count — whether for throughput or HA/rolling-deploy —
	// triggers the queue-group bug; SIGTERM timing during rolling deploys
	// triggers the shutdown bug) even though the remediation is framework-
	// level. Shared-codebase workers skip this check; their operational
	// knowledge lives in the host target README.
	checks = append(checks, checkWorkerProductionCorrectness(ctx, hostname, string(readmeContent), target)...)
	checks = append(checks, checkWorkerDrainCodeBlock(hostname, string(readmeContent), target)...)

	// CLAUDE.md: repo-local dev-loop operations guide. Lives alongside
	// README.md on the mount, not extracted. Required for every tier
	// because every recipe ships a dev container that needs a repo-local
	// "how to work this" answer.
	checks = append(checks, checkCLAUDEMdExists(projectRoot, target, plan)...)

	return checks
}

// checkRecipeSetups validates zerops.yaml has all required setup entries for
// the given target. Every runtime target needs dev + prod. Only a target that
// HOSTS a shared-codebase worker (worker.Type base matches target.Type base)
// additionally needs a worker setup. In dual-runtime recipes, that's the API
// target only — the frontend's zerops.yaml must NOT be forced to carry a
// worker setup it does not own.
// zerops.yaml uses generic names (`setup: dev`, `setup: prod`, `setup: worker`).
// The deploy tool's --setup param maps hostname→setup at cross-deploy time.
func checkRecipeSetups(doc *ops.ZeropsYmlDoc, target workflow.RecipeTarget, plan *workflow.RecipePlan) []workflow.StepCheck {
	hostname := target.Hostname
	var checks []workflow.StepCheck

	// Dev setup: try "dev" (correct), then legacy fallbacks.
	var devEntry *ops.ZeropsYmlEntry
	var foundName string
	for _, name := range []string{"dev", hostname + "dev", hostname} {
		if e := doc.FindEntry(name); e != nil {
			devEntry = e
			foundName = name
			break
		}
	}
	if devEntry == nil {
		checks = append(checks, workflow.StepCheck{
			Name: hostname + "_setup", Status: statusFail,
			Detail: fmt.Sprintf("no dev setup entry found in zerops.yaml (tried: dev, %sdev, %s)", hostname, hostname),
		})
		return checks
	}
	checks = append(checks, workflow.StepCheck{
		Name: hostname + "_setup", Status: statusPass,
		Detail: fmt.Sprintf("found setup: %s", foundName),
	})

	// Prod setup: required — generate step writes BOTH dev and prod so the
	// file matches the README integration-guide fragment exactly.
	prodEntry := doc.FindEntry(workflow.RecipeSetupProd)
	if prodEntry == nil {
		checks = append(checks, workflow.StepCheck{
			Name: hostname + "_prod_setup", Status: statusFail,
			Detail: "no prod setup entry found in zerops.yaml — generate step writes BOTH dev and prod setups. The prod block is what cross-deploys to stage and is the reference the README integration-guide documents for end users.",
		})
		return checks
	}
	checks = append(checks, workflow.StepCheck{
		Name: hostname + "_prod_setup", Status: statusPass,
	})

	// Dev/prod env-mode divergence: identical envVariables maps mean the dev
	// container behaves exactly like prod (caches enabled, stack traces hidden),
	// hiding iteration feedback from the agent.
	checks = append(checks, checkRecipeDevProdDivergence(devEntry, prodEntry)...)

	// Worker setup: required ONLY when this target hosts a shared-codebase
	// worker. In dual-runtime recipes the frontend does not host the worker —
	// the API does. Forcing the frontend to carry a dummy worker block would
	// leak an implementation detail of one codebase into an unrelated one.
	if workflow.TargetHostsSharedWorker(target, plan) {
		workerEntry := doc.FindEntry(workflow.RecipeSetupWorker)
		if workerEntry == nil {
			checks = append(checks, workflow.StepCheck{
				Name:   hostname + "_worker_setup",
				Status: statusFail,
				Detail: fmt.Sprintf("a worker target shares %s's codebase but %s/zerops.yaml has no setup: worker — the worker's build+start lives in the host codebase's zerops.yaml as a third setup", hostname, hostname),
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: hostname + "_worker_setup", Status: statusPass,
			})
		}
	}

	return checks
}

// checkRecipeDevProdDivergence flags dev and prod run.envVariables maps that
// are bit-identical. The two setups exist to carry different values for
// framework mode flags (APP_ENV, NODE_ENV, DEBUG, LOG_LEVEL). Bit-equal maps
// mean the copy-paste was not adjusted — dev will act as prod, masking bugs.
func checkRecipeDevProdDivergence(devEntry, prodEntry *ops.ZeropsYmlEntry) []workflow.StepCheck {
	devEnv := devEntry.Run.EnvVariables
	prodEnv := prodEntry.Run.EnvVariables
	// If either side has no run.envVariables block, framework defaults carry
	// the mode signal — nothing to compare.
	if len(devEnv) == 0 || len(prodEnv) == 0 {
		return nil
	}
	if maps.Equal(devEnv, prodEnv) {
		return []workflow.StepCheck{{
			Name:   "dev_prod_env_divergence",
			Status: statusFail,
			Detail: "dev and prod setups have bit-identical run.envVariables — the dev container will behave exactly like prod (caches enabled, stack traces hidden during iteration). Differentiate them with the framework's mode flag (APP_ENV=local vs production, NODE_ENV=development vs production, DEBUG=true vs false, etc.)",
		}}
	}
	return []workflow.StepCheck{{
		Name: "dev_prod_env_divergence", Status: statusPass,
	}}
}

// checkReadmeFragments validates README.md contains required fragment
// markers and quality. The hostname identifies which codebase's README
// is being inspected — it appears in v8.96 structured-diagnostic
// ReadSurface fields so the author can disambiguate when the same check
// fires across multiple codebases. The check Names themselves stay bare
// (e.g. "comment_ratio") for backwards compatibility with existing
// tests; ReadSurface carries the host context.
func checkReadmeFragments(content, hostname string) []workflow.StepCheck {
	var checks []workflow.StepCheck

	// Check required fragment markers.
	for _, name := range requiredFragments {
		startTag := fmt.Sprintf("#ZEROPS_EXTRACT_START:%s#", name)
		endTag := fmt.Sprintf("#ZEROPS_EXTRACT_END:%s#", name)

		hasStart := strings.Contains(content, startTag)
		hasEnd := strings.Contains(content, endTag)

		if hasStart && hasEnd {
			checks = append(checks, workflow.StepCheck{
				Name: "fragment_" + name, Status: statusPass,
			})
		} else {
			// Show the agent the literal marker format we are searching
			// for. v14 debugging wasted 20 minutes on a run that invented
			// `<!-- FRAGMENT:intro:start -->` from imagination because the
			// error message said "missing" without specifying the shape.
			expected := fmt.Sprintf("<!-- #ZEROPS_EXTRACT_START:%s# -->\n...content...\n<!-- #ZEROPS_EXTRACT_END:%s# -->", name, name)
			detail := fmt.Sprintf("missing fragment markers for %q. Expected the EXACT literal markers (not a close approximation):\n%s\nSee `zerops_guidance topic=\"readme-fragments\"` for the full fragment writing rules.", name, expected)
			if hasStart && !hasEnd {
				detail = fmt.Sprintf("fragment %q has the `<!-- #ZEROPS_EXTRACT_START:%s# -->` marker but is missing the matching `<!-- #ZEROPS_EXTRACT_END:%s# -->` closer.", name, name, name)
			}
			checks = append(checks, workflow.StepCheck{
				Name: "fragment_" + name, Status: statusFail,
				Detail: detail,
			})
		}
	}

	// Check integration-guide fragment has zerops.yaml code block.
	igContent := extractFragmentContent(content, "integration-guide")
	if igContent != "" {
		if strings.Contains(igContent, "```yaml") || strings.Contains(igContent, "```yml") {
			checks = append(checks, workflow.StepCheck{
				Name: "integration_guide_yaml", Status: statusPass,
			})
			// Check comment ratio in the YAML code block.
			yamlBlock := extractYAMLBlock(igContent)
			if yamlBlock != "" {
				ratio := commentRatio(yamlBlock)
				if ratio >= 0.3 {
					checks = append(checks, workflow.StepCheck{
						Name: "comment_ratio", Status: statusPass,
						Detail: fmt.Sprintf("%.0f%% comments", ratio*100),
					})
				} else {
					readSurface := fmt.Sprintf(
						"embedded YAML in %s/README.md (#ZEROPS_EXTRACT_START:integration-guide fragment)",
						hostname,
					)
					coupled := []string{hostname + "/zerops.yaml"}
					checks = append(checks, workflow.StepCheck{
						Name:        "comment_ratio",
						Status:      statusFail,
						Detail:      fmt.Sprintf("comment ratio %.0f%% is below 30%% minimum", ratio*100),
						ReadSurface: readSurface,
						Required:    "≥30% of YAML lines comment-only",
						Actual:      fmt.Sprintf("%.0f%%", ratio*100),
						CoupledWith: coupled,
						HowToFix: fmt.Sprintf(
							"Add `#` comment lines inside the integration-guide YAML block in %s/README.md until the comment-only ratio reaches 30%%. "+
								"This block usually mirrors %s/zerops.yaml (IG step 1) — keep both files byte-identical when you edit, otherwise the next round will fail again on the unsynced surface.",
							hostname, hostname,
						),
					})
				}
				// Section-heading comment check on zerops.yaml.
				checks = append(checks, checkSectionHeadingComments(yamlBlock, "zerops_yaml")...)
			}
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: "integration_guide_yaml", Status: statusFail,
				Detail: "integration-guide fragment must contain a zerops.yaml code block",
			})
		}
	}

	// Check fragment heading levels — content inside markers must use H3, not H2.
	// H2 section titles go OUTSIDE markers in the README.
	for _, name := range []string{"integration-guide", "knowledge-base"} {
		fc := extractFragmentContent(content, name)
		if fc == "" {
			continue
		}
		for line := range strings.SplitSeq(fc, "\n") {
			if strings.HasPrefix(line, "## ") && !strings.HasPrefix(line, "### ") {
				checks = append(checks, workflow.StepCheck{
					Name: "fragment_" + name + "_heading_level", Status: statusFail,
					Detail: fmt.Sprintf("fragment %q has H2 heading inside markers — use H3 (###). H2 section titles go OUTSIDE markers.", name),
				})
				break
			}
		}
	}

	// Check blank line after start marker.
	for _, name := range requiredFragments {
		marker := fmt.Sprintf("#ZEROPS_EXTRACT_START:%s#", name)
		_, afterMarker, found := strings.Cut(content, marker)
		if !found {
			continue
		}
		// Skip to end of marker line, then check next line is blank.
		if _, rest, ok := strings.Cut(afterMarker, "\n"); ok {
			nextLine, _, _ := strings.Cut(rest, "\n")
			if nextLine != "" && !strings.HasPrefix(nextLine, "<!--") {
				readPath := "README.md"
				if hostname != "" {
					readPath = hostname + "/README.md"
				}
				checks = append(checks, workflow.StepCheck{
					Name:        "fragment_" + name + "_blank_after_marker",
					Status:      statusFail,
					Detail:      fmt.Sprintf("fragment %q needs a blank line after the start marker", name),
					ReadSurface: fmt.Sprintf("%s — line immediately after `<!-- #ZEROPS_EXTRACT_START:%s# -->`", readPath, name),
					Required:    "exactly one blank line between the start marker and the fragment body",
					Actual:      fmt.Sprintf("non-blank content on the line after the %q start marker", name),
					HowToFix: fmt.Sprintf(
						"Insert a blank line in %s between `<!-- #ZEROPS_EXTRACT_START:%s# -->` and the first content line. The extractor that publishes the fragment to zerops.io/recipes treats the marker line as a comment and needs the blank separator before the rendered body starts.",
						readPath, name,
					),
				})
			}
		}
	}

	// Check knowledge-base fragment has Gotchas section.
	kbContent := extractFragmentContent(content, "knowledge-base")
	if kbContent != "" {
		if strings.Contains(kbContent, "### Gotchas") || strings.Contains(kbContent, "## Gotchas") {
			checks = append(checks, workflow.StepCheck{
				Name: "knowledge_base_gotchas", Status: statusPass,
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: "knowledge_base_gotchas", Status: statusFail,
				Detail: "knowledge-base fragment must include a Gotchas section",
			})
		}
	}

	// Check intro fragment constraints.
	introContent := extractFragmentContent(content, "intro")
	if introContent != "" {
		lines := nonEmptyLines(introContent)
		if len(lines) > 3 {
			checks = append(checks, workflow.StepCheck{
				Name: "intro_length", Status: statusFail,
				Detail: fmt.Sprintf("intro must be 1-3 lines, got %d", len(lines)),
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: "intro_length", Status: statusPass,
			})
		}
		if strings.Contains(introContent, "#") {
			checks = append(checks, workflow.StepCheck{
				Name: "intro_no_titles", Status: statusFail,
				Detail: "intro must not contain markdown titles",
			})
		}
	}

	// Check no placeholders anywhere in README.
	if matches := placeholderRe.FindAllString(content, -1); len(matches) > 0 {
		checks = append(checks, workflow.StepCheck{
			Name: "no_placeholders", Status: statusFail,
			Detail: fmt.Sprintf("found placeholder strings: %s", strings.Join(uniqueStrings(matches), ", ")),
		})
	} else {
		checks = append(checks, workflow.StepCheck{
			Name: "no_placeholders", Status: statusPass,
		})
	}

	return checks
}

// extractFragmentContent extracts content between ZEROPS_EXTRACT_START and END markers.
// Looks for the full HTML comment form: <!-- #ZEROPS_EXTRACT_START:name# -->
func extractFragmentContent(content, name string) string {
	startMarker := fmt.Sprintf("#ZEROPS_EXTRACT_START:%s#", name)
	endMarker := fmt.Sprintf("#ZEROPS_EXTRACT_END:%s#", name)

	startIdx := strings.Index(content, startMarker)
	if startIdx < 0 {
		return ""
	}
	// Find the end of the start marker line (skip past closing -->).
	afterStart := startIdx + len(startMarker)
	lineEnd := strings.Index(content[afterStart:], "\n")
	if lineEnd < 0 {
		return ""
	}
	contentStart := afterStart + lineEnd + 1

	// Find the end marker, but look for the <!-- that precedes it.
	endIdx := strings.Index(content[contentStart:], endMarker)
	if endIdx < 0 {
		return ""
	}
	// Walk back from the endMarker to find the start of its <!-- comment.
	extractEnd := contentStart + endIdx
	for extractEnd > contentStart && content[extractEnd-1] != '\n' {
		extractEnd--
	}
	return strings.TrimSpace(content[contentStart:extractEnd])
}

// checkIntegrationGuideCodeBlocks — tool-layer thin wrapper (post-C-7a)
// around opschecks.CheckIGCodeAdjustment. The predicate lives in
// internal/ops/checks so the (future) zcp check ig-code-adjustment CLI
// shim and the server-side gate share one implementation. This wrapper
// maps the plan pointer into the bare isShowcase bool the predicate
// expects; ctx is threaded from the step-checker closure so contextcheck
// stays quiet even though the predicate itself is a pure computation.
func checkIntegrationGuideCodeBlocks(ctx context.Context, content string, plan *workflow.RecipePlan) []workflow.StepCheck {
	isShowcase := plan != nil && plan.Tier == workflow.RecipeTierShowcase
	return opschecks.CheckIGCodeAdjustment(ctx, content, isShowcase)
}

// checkIntegrationGuidePerItemCodeBlock enforces that every H3 heading
// inside the integration-guide fragment carries at least one fenced
// code block in its section. Catches the v18 appdev regression where
// IG step 3 was prose-only while v7/v14/v15 had a code diff for every
// step.
//
// Recipe.md already tells the agent "### 2. Step Title (for each code
// adjustment you actually made) … with the code diff". The existing
// integration_guide_code_adjustment check only enforces ≥1 non-YAML
// block in the whole fragment — it passes even when half the items are
// prose-only. This per-item floor enforces what the template says:
// one item per real code adjustment, each with a code block.
//
// Single-H3 IG sections are allowed (minimal shape). The check fires
// when there are ≥2 H3 headings AND any heading after the first has
// no fenced code block in its section.
//
// Scoped to showcase tier (minimal recipes have simpler IG shapes that
// don't always need per-item code blocks).
// checkIntegrationGuidePerItemCodeBlock — tool-layer thin wrapper
// (post-C-7b) around opschecks.CheckIGPerItemCode. splitByH3 +
// sectionHasFencedBlock helpers moved into the ops/checks package
// alongside the predicate.
func checkIntegrationGuidePerItemCodeBlock(ctx context.Context, content string, plan *workflow.RecipePlan) []workflow.StepCheck {
	isShowcase := plan != nil && plan.Tier == workflow.RecipeTierShowcase
	return opschecks.CheckIGPerItemCode(ctx, content, isShowcase)
}

// extractYAMLBlock extracts content from the first ```yaml ... ``` block.
func extractYAMLBlock(content string) string {
	start := strings.Index(content, "```yaml")
	if start < 0 {
		start = strings.Index(content, "```yml")
	}
	if start < 0 {
		return ""
	}
	// Skip past the opening fence line.
	lineEnd := strings.Index(content[start:], "\n")
	if lineEnd < 0 {
		return ""
	}
	blockStart := start + lineEnd + 1

	end := strings.Index(content[blockStart:], "```")
	if end < 0 {
		return ""
	}
	return content[blockStart : blockStart+end]
}

// commentRatio calculates the ratio of comment lines to total non-empty lines.
func commentRatio(yamlContent string) float64 {
	lines := strings.Split(yamlContent, "\n")
	total := 0
	comments := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		total++
		if strings.HasPrefix(trimmed, "#") {
			comments++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(comments) / float64(total)
}

// checkCommentSpecificity — tool-layer thin wrapper (post-C-7b) around
// opschecks.CheckCommentSpecificity. specificityMarkers, thresholds,
// and the ratio computation moved into the ops/checks package alongside
// the predicate.
func checkCommentSpecificity(ctx context.Context, yamlBlock string, plan *workflow.RecipePlan) []workflow.StepCheck {
	isShowcase := plan != nil && plan.Tier == workflow.RecipeTierShowcase
	return opschecks.CheckCommentSpecificity(ctx, yamlBlock, isShowcase)
}

// nonEmptyLines returns non-empty lines from content.
func nonEmptyLines(content string) []string {
	var result []string
	for line := range strings.SplitSeq(content, "\n") {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}
	return result
}

// checkZeropsYmlFields — tool-layer thin wrapper (post-C-7b) around
// opschecks.CheckZeropsYmlFields.
func checkZeropsYmlFields(ctx context.Context, ymlDir string, validFields *schema.ValidFields) []workflow.StepCheck {
	return opschecks.CheckZeropsYmlFields(ctx, ymlDir, validFields)
}

// uniqueStrings returns unique strings from a slice.
func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	var result []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
