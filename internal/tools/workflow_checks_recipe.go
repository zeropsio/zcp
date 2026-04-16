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
func buildRecipeStepChecker(ctx context.Context, step, _, stateDir string, schemaCache *schema.Cache, kp knowledge.Provider) workflow.RecipeStepChecker {
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
		return checkRecipeDeployReadmes(stateDir, kp)
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
	return func(_ context.Context, plan *workflow.RecipePlan, state *workflow.RecipeState) (*workflow.StepCheckResult, error) {
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
			hostname := appTarget.Hostname

			// Try mount paths: {hostname}dev (standard mode), {hostname} (bare), then project root.
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
				checks = append(checks, workflow.StepCheck{
					Name: hostname + "_zerops_yml_exists", Status: statusFail,
					Detail: fmt.Sprintf("zerops.yaml not found for %s: %v", hostname, parseErr),
				})
				continue
			}
			checks = append(checks, workflow.StepCheck{
				Name: hostname + "_zerops_yml_exists", Status: statusPass,
			})
			checks = append(checks, checkRecipeSetups(doc, appTarget, plan)...)

			// Validate zerops.yaml fields against the live JSON schema.
			checks = append(checks, checkZeropsYmlFields(ymlDir, validFields)...)

			// README content validation — fragments, integration-guide
			// code blocks, comment specificity, predecessor floor,
			// authenticity — all move to the deploy-step checker. v14
			// writes READMEs during the post-verify `readmes` sub-step so
			// the gotchas section narrates real debug experience; the
			// checker follows the content. See checkRecipeDeployReadmes.
			//
			// Additionally, v14 generate explicitly forbids README.md on
			// the mount at generate-complete time: the scaffold sub-agent
			// brief has README in its DO-NOT-WRITE list. Detect and
			// surface so the agent deletes the scaffolder's stub before
			// the checker re-runs.
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
		}

		// Legacy: the workerTargets predecessor-floor loop lived here in
		// v13. It moved to checkRecipeDeployReadmes alongside the other
		// README content checks — v14 READMEs don't exist until the
		// post-deploy readmes sub-step.
		_ = workerTargets

		allPassed := checksAllPassed(checks)
		summary := "recipe generate checks passed"
		if !allPassed {
			summary = "recipe generate checks failed"
		}
		return &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}, nil
	}
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
func checkRecipeDeployReadmes(stateDir string, kp knowledge.Provider) workflow.RecipeStepChecker {
	return func(_ context.Context, plan *workflow.RecipePlan, _ *workflow.RecipeState) (*workflow.StepCheckResult, error) {
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
			checks = append(checks, checkCodebaseReadme(projectRoot, target, plan, predecessorStems, false, readmesByHost)...)
		}
		for _, target := range workerTargets {
			checks = append(checks, checkCodebaseReadme(projectRoot, target, plan, predecessorStems, true, readmesByHost)...)
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

		allPassed := checksAllPassed(checks)
		summary := "recipe deploy README checks passed"
		if !allPassed {
			summary = "recipe deploy README checks failed"
		}
		return &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}, nil
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
func checkCodebaseReadme(projectRoot string, target workflow.RecipeTarget, plan *workflow.RecipePlan, predecessorStems []string, workerOnly bool, readmesByHost map[string]string) []workflow.StepCheck {
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
	checks = append(checks, checkReadmeFragments(string(readmeContent))...)

	if !workerOnly {
		if igContent := extractFragmentContent(string(readmeContent), "integration-guide"); igContent != "" {
			if yamlBlock := extractYAMLBlock(igContent); yamlBlock != "" {
				specChecks := checkCommentSpecificity(yamlBlock, plan)
				for i := range specChecks {
					specChecks[i].Name = hostname + "_" + specChecks[i].Name
				}
				checks = append(checks, specChecks...)
			}
		}
		codeChecks := checkIntegrationGuideCodeBlocks(string(readmeContent), plan)
		for i := range codeChecks {
			codeChecks[i].Name = hostname + "_" + codeChecks[i].Name
		}
		checks = append(checks, codeChecks...)

		perItemChecks := checkIntegrationGuidePerItemCodeBlock(string(readmeContent), plan)
		for i := range perItemChecks {
			perItemChecks[i].Name = hostname + "_" + perItemChecks[i].Name
		}
		checks = append(checks, perItemChecks...)

		// Per-IG-item standalone (v8.78): each `### N.` block must
		// stand alone with its own code block + platform-anchor in the
		// first prose paragraph. v20 apidev IG #2 ("Binding to
		// 0.0.0.0") leaned on IG #1's zerops.yaml comments for the
		// why; this rule forces every block to teach independently.
		checks = append(checks, checkPerIGItemStandalone(string(readmeContent), hostname)...)
	}

	floorChecks := checkKnowledgeBaseExceedsPredecessor(string(readmeContent), plan, predecessorStems)
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
	checks = append(checks, checkWorkerProductionCorrectness(hostname, string(readmeContent), target)...)
	checks = append(checks, checkWorkerDrainCodeBlock(hostname, string(readmeContent), target)...)

	// CLAUDE.md: repo-local dev-loop operations guide. Lives alongside
	// README.md on the mount, not extracted. Required for every tier
	// because every recipe ships a dev container that needs a repo-local
	// "how to work this" answer.
	checks = append(checks, checkCLAUDEMdExists(projectRoot, target, plan)...)

	// Reality-check (v8.78): every file path and every declared symbol
	// referenced in README knowledge-base/integration-guide and CLAUDE.md
	// must either exist in the codebase OR be framed as advisory in the
	// surrounding prose. Catches the v20 class of "we documented behavior
	// we didn't ship" — appdev gotcha cited `_nginx.json` proxy fix that
	// wasn't shipped, workerdev watchdog gotcha imperatively prescribed a
	// `setInterval` watchdog that no symbol in src/ implemented. Reads
	// CLAUDE.md from the same mount as README.md.
	claudePath := filepath.Join(ymlDir, "CLAUDE.md")
	claudeBody, _ := os.ReadFile(claudePath)
	checks = append(checks, checkContentReality(ymlDir, hostname, string(readmeContent), string(claudeBody))...)

	// Causal-anchor (v8.78): every gotcha must be load-bearing — name a
	// SPECIFIC Zerops mechanism (not generic "container"/"envVariables")
	// AND describe a CONCRETE failure mode (HTTP code, quoted error,
	// strong symptom verb). Catches the v20 class of generic-platform
	// advice mis-anchored as Zerops gotcha — e.g. ".env file in repo
	// overrides Zerops-managed values" which mentions envVariables but
	// describes no platform-caused failure mode. Per-bullet enforcement:
	// every gotcha must pass, not just a quorum.
	if kbBody := extractFragmentContent(string(readmeContent), "knowledge-base"); kbBody != "" {
		checks = append(checks, checkCausalAnchor(kbBody, hostname)...)
		checks = append(checks, checkServiceCoverage(kbBody, plan, hostname, target.IsWorker)...)
	}

	// CLAUDE.md vs README consistency (v8.78): procedures in CLAUDE.md
	// must not use code-level mechanisms the README's gotchas
	// explicitly forbid for production. CLAUDE.md is the ambient
	// context an agent reads when operating the codebase; if it
	// teaches a pattern the README warns against, the agent will
	// propagate it into prod-affecting changes. v20 apidev had this:
	// README forbade `synchronize: true` in production; CLAUDE.md
	// reset-state used `ds.synchronize()` as a dev shortcut.
	checks = append(checks, checkClaudeReadmeConsistency(string(readmeContent), string(claudeBody), hostname)...)

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

// checkReadmeFragments validates README.md contains required fragment markers and quality.
func checkReadmeFragments(content string) []workflow.StepCheck {
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
					checks = append(checks, workflow.StepCheck{
						Name: "comment_ratio", Status: statusFail,
						Detail: fmt.Sprintf("comment ratio %.0f%% is below 30%% minimum", ratio*100),
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
				checks = append(checks, workflow.StepCheck{
					Name: "fragment_" + name + "_blank_after_marker", Status: statusFail,
					Detail: fmt.Sprintf("fragment %q needs a blank line after the start marker", name),
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

// codeBlockFenceRe matches the opening fence of a fenced code block:
// three backticks followed by an optional language tag on the same line.
var codeBlockFenceRe = regexp.MustCompile("(?m)^\\s*```([a-zA-Z0-9+_-]*)")

// nonYamlCodeLanguages are the language tags that count as "application
// code adjustment" in an integration guide. YAML and variants are
// explicitly excluded — every integration guide already has a zerops.yaml
// block, and that alone doesn't show what the user needs to change in
// their app code. Shell is accepted because some recipes document
// setup commands (e.g. "run npm install before first deploy").
var nonYamlCodeLanguages = map[string]bool{
	"typescript": true,
	"ts":         true,
	"javascript": true,
	"js":         true,
	"jsx":        true,
	"tsx":        true,
	"svelte":     true,
	"vue":        true,
	"python":     true,
	"py":         true,
	"php":        true,
	"go":         true,
	"golang":     true,
	"ruby":       true,
	"rb":         true,
	"rust":       true,
	"rs":         true,
	"java":       true,
	"kotlin":     true,
	"swift":      true,
	"bash":       true,
	"sh":         true,
	"shell":      true,
	"zsh":        true,
	"dockerfile": true,
	"nginx":      true,
}

// checkIntegrationGuideCodeBlocks verifies the integration guide contains
// at least one non-YAML code block — real application code a user adjusts
// to run on Zerops (trust proxy, host-check settings, framework-specific
// bind-address changes). The v12 audit found most integration guides were
// 95% zerops.yaml comments with only one real code-adjustment section;
// the floor forces every integration guide to document at least one
// concrete change the user makes to their own application code.
//
// Scoped to showcase tier: minimal recipes often have no application-side
// changes to document and would fail the check for no good reason.
func checkIntegrationGuideCodeBlocks(content string, plan *workflow.RecipePlan) []workflow.StepCheck {
	if plan == nil || plan.Tier != workflow.RecipeTierShowcase {
		return nil
	}
	igContent := extractFragmentContent(content, "integration-guide")
	if igContent == "" {
		return nil
	}
	matches := codeBlockFenceRe.FindAllStringSubmatch(igContent, -1)
	if len(matches) == 0 {
		return nil // checkReadmeFragments already reports "no yaml block"
	}
	// Each match opens a fence. The pairs alternate (open/close); the
	// language tag only appears on the opener. We just count unique
	// non-empty language tags that fall into the accepted set.
	nonYaml := 0
	var seenLangs []string
	for _, m := range matches {
		lang := strings.ToLower(strings.TrimSpace(m[1]))
		if lang == "" {
			continue // closing fence or untagged fence
		}
		if nonYamlCodeLanguages[lang] {
			nonYaml++
			seenLangs = append(seenLangs, lang)
		}
	}
	if nonYaml == 0 {
		return []workflow.StepCheck{{
			Name:   "integration_guide_code_adjustment",
			Status: statusFail,
			Detail: "integration guide has zerops.yaml only — add at least one non-YAML code block showing an actual application-code change a user must make to run on Zerops. Examples: Express trust-proxy + 0.0.0.0 bind (typescript), Vite allowedHosts (js), Laravel CORS config (php), Django ALLOWED_HOSTS (py). The integration guide must document what the USER changes in their OWN code, not just the zerops.yaml they copy-paste.",
		}}
	}
	return []workflow.StepCheck{{
		Name:   "integration_guide_code_adjustment",
		Status: statusPass,
		Detail: fmt.Sprintf("%d non-YAML code block(s): %s", nonYaml, strings.Join(uniqueStrings(seenLangs), ", ")),
	}}
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
func checkIntegrationGuidePerItemCodeBlock(content string, plan *workflow.RecipePlan) []workflow.StepCheck {
	if plan == nil || plan.Tier != workflow.RecipeTierShowcase {
		return nil
	}
	igContent := extractFragmentContent(content, "integration-guide")
	if igContent == "" {
		return nil
	}
	sections := splitByH3(igContent)
	if len(sections) < 2 {
		return []workflow.StepCheck{{
			Name:   "integration_guide_per_item_code",
			Status: statusPass,
		}}
	}

	var missing []string
	for _, sec := range sections {
		if sectionHasFencedBlock(sec.Body) {
			continue
		}
		missing = append(missing, sec.Heading)
	}
	if len(missing) == 0 {
		return []workflow.StepCheck{{
			Name:   "integration_guide_per_item_code",
			Status: statusPass,
			Detail: fmt.Sprintf("%d IG items, all with fenced code blocks", len(sections)),
		}}
	}
	return []workflow.StepCheck{{
		Name:   "integration_guide_per_item_code",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"integration-guide H3 section(s) with no fenced code block: %s. Every IG step must carry a code diff — recipe.md says 'one IG item per real code adjustment, with the code diff'. If a step describes a config placement (envVariables section, where to put a var), show the before/after as a fenced block. If a step is prose-only, either delete it or fold its content into a neighbouring step that has code.",
			strings.Join(missing, " | "),
		),
	}}
}

// igSection is one H3 section inside the integration-guide fragment.
type igSection struct {
	Heading string // the H3 heading text (without the ### prefix)
	Body    string // the content between this H3 and the next one (or end-of-fragment)
}

// splitByH3 splits markdown content into sections keyed by H3 headings.
// Content before the first H3 is dropped (usually a fragment start marker).
func splitByH3(content string) []igSection {
	lines := strings.Split(content, "\n")
	var sections []igSection
	var current *igSection
	var body strings.Builder
	flush := func() {
		if current == nil {
			return
		}
		current.Body = body.String()
		sections = append(sections, *current)
		current = nil
		body.Reset()
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			flush()
			heading := strings.TrimSpace(strings.TrimPrefix(trimmed, "### "))
			current = &igSection{Heading: heading}
			continue
		}
		if current != nil {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	flush()
	return sections
}

// sectionHasFencedBlock returns true when the body contains at least
// one complete fenced code block (``` ... ```), any language. Each
// opener/closer emits a regex match, so a complete block produces ≥2
// matches.
func sectionHasFencedBlock(body string) bool {
	matches := codeBlockFenceRe.FindAllStringIndex(body, -1)
	return len(matches) >= 2
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

// specificityMarkers are tokens that signal a comment is earning its
// keep — it explains WHY, names a concrete platform behavior, or points
// at a failure mode the reader would otherwise trip on. A comment
// containing any of these is "specific"; comments without any marker are
// boilerplate ("npm ci for reproducible builds" is the archetype — true
// of every recipe, load-bearing on none).
//
// The list is intentionally coarse: we want to accept real comments
// without stylistic hand-wringing, not grade them for prose quality.
// Every v12 API zerops.yaml comment already clears this bar.
var specificityMarkers = []string{
	// causal / reasoning
	"because", "so that", "otherwise", "prevents", "required", "ensures",
	"needed", "must", "without",
	// failure mode
	"fails", "breaks", "crashes", "silent", "race", "502", "401", "cold start",
	"blocked", "drops", "empty",
	// platform constraints
	"zerops", "execonce", "l7", "balancer", "httpsupport", "advisory lock",
	"0.0.0.0", "subdomain", "reverse proxy", "terminates ssl", "trust proxy",
	"vxlan", "${",
	// framework×platform intersection signals
	"build time", "build-time", "runtime", "os-level", "horizontal container",
	"multi-container", "fresh container", "stateless", "bundle",
}

// commentSpecificityRatio measures how many comment lines in a zerops.yaml
// block clear the specificity floor. A comment is specific when it
// contains at least one specificityMarker (after lowercasing). The check
// that consumes this ratio requires both an absolute floor (at least
// minSpecificComments specific comments) and a proportional floor (at
// least specificCommentRatio of all comments are specific).
func commentSpecificityRatio(yamlContent string) (specific, total int, ratio float64) {
	for line := range strings.SplitSeq(yamlContent, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		total++
		lower := strings.ToLower(trimmed)
		for _, marker := range specificityMarkers {
			if strings.Contains(lower, marker) {
				specific++
				break
			}
		}
	}
	if total == 0 {
		return 0, 0, 0
	}
	return specific, total, float64(specific) / float64(total)
}

// minSpecificComments is the absolute floor — even a very short
// zerops.yaml must have at least this many non-boilerplate comments to
// pass the specificity check.
const minSpecificComments = 3

// specificCommentRatio is the proportional floor — even a very long
// zerops.yaml with many comments must have at least this fraction that
// clear the specificity bar. Tuned low enough that v12 API's comments
// easily pass; tight enough that copy-paste boilerplate fails.
const specificCommentRatio = 0.25

// checkCommentSpecificity is the companion to commentRatio. commentRatio
// measures how many comments are PRESENT; specificity measures how many
// are LOAD-BEARING. The v12 session had comments like "npm ci for
// reproducible builds" and "cache node_modules between builds" that
// pass the 30%-present ratio but read as generic boilerplate that could
// appear in any recipe. A reader learning Zerops from the integration
// guide needs to see comments that explain Zerops-specific constraints
// — execOnce on multi-container deploys, L7 balancer behavior, the
// $-interpolated credential injection, the tilde deployFiles suffix.
func checkCommentSpecificity(yamlBlock string, plan *workflow.RecipePlan) []workflow.StepCheck {
	if plan == nil || plan.Tier != workflow.RecipeTierShowcase {
		return nil
	}
	specific, total, ratio := commentSpecificityRatio(yamlBlock)
	if total == 0 {
		return nil
	}
	if specific >= minSpecificComments && ratio >= specificCommentRatio {
		return []workflow.StepCheck{{
			Name:   "comment_specificity",
			Status: statusPass,
			Detail: fmt.Sprintf("%d of %d comments are specific (%.0f%%)", specific, total, ratio*100),
		}}
	}
	return []workflow.StepCheck{{
		Name:   "comment_specificity",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"comment specificity too low: %d of %d are specific (%.0f%%, need >= %d and >= %.0f%%). Specific means the comment explains WHY (because/so that/prevents/required/fails/breaks), or names a Zerops platform term (execOnce, L7 balancer, ${env_var}, httpSupport, 0.0.0.0, subdomain, advisory lock, trust proxy, cold start, build time, horizontal container). Generic lines like \"npm ci for reproducible builds\" or \"cache node_modules between builds\" pass the ratio check but teach the reader nothing Zerops-specific. Rewrite boilerplate comments to explain what would break without them.",
			specific, total, ratio*100, minSpecificComments, specificCommentRatio*100,
		),
	}}
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

// checkZeropsYmlFields validates zerops.yaml field names against the live JSON schema.
// Catches import.yaml-only fields (e.g. verticalAutoscaling) that agents
// incorrectly add to zerops.yaml, and any other hallucinated field names.
func checkZeropsYmlFields(ymlDir string, validFields *schema.ValidFields) []workflow.StepCheck {
	if validFields == nil {
		return nil
	}

	// Read raw content — ParseZeropsYml uses typed structs which silently drop unknown fields.
	raw, err := ops.ReadZeropsYmlRaw(ymlDir)
	if err != nil {
		return nil // file-not-found already reported by the existence check
	}

	fieldErrs := schema.ValidateZeropsYmlRaw(raw, validFields)
	if len(fieldErrs) == 0 {
		return []workflow.StepCheck{{
			Name: "zerops_yml_schema_fields", Status: statusPass,
		}}
	}

	details := make([]string, len(fieldErrs))
	for i, e := range fieldErrs {
		details[i] = e.Error()
	}
	return []workflow.StepCheck{{
		Name:   "zerops_yml_schema_fields",
		Status: statusFail,
		Detail: fmt.Sprintf("zerops.yaml contains fields not in the platform schema (these belong in import.yaml or don't exist): %s", strings.Join(details, "; ")),
	}}
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
