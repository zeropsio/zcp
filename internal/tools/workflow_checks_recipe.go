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
// kp is the knowledge provider from the engine — used by generate-step
// checks that need to read the injected chain recipe (e.g. the
// predecessor-as-floor knowledge-base check). Pass nil in tests that
// don't need chain access; checks that depend on it no-op gracefully.
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

		// Pre-resolve the predecessor recipe's gotcha stems once for the
		// whole run — every codebase README on this plan compares against
		// the same predecessor baseline, so we only hit the knowledge store
		// once per check invocation. Empty when kp is nil (test context) or
		// when there is no direct predecessor (hello-world tier).
		predecessorStems := workflow.PredecessorGotchaStems(plan, kp)

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

			// Check README fragments for this target's codebase.
			readmePath := filepath.Join(ymlDir, "README.md")
			readmeContent, readErr := os.ReadFile(readmePath)
			if readErr != nil {
				checks = append(checks, workflow.StepCheck{
					Name: hostname + "_readme_exists", Status: statusFail,
					Detail: fmt.Sprintf("README.md not found at %s", readmePath),
				})
			} else {
				checks = append(checks, workflow.StepCheck{
					Name: hostname + "_readme_exists", Status: statusPass,
				})
				checks = append(checks, checkReadmeFragments(string(readmeContent))...)

				// Comment specificity check on the integration guide's YAML
				// block — showcase only. Companion to comment_ratio; fails
				// on boilerplate-heavy comments even when the ratio passes.
				if !appTarget.IsWorker {
					if igContent := extractFragmentContent(string(readmeContent), "integration-guide"); igContent != "" {
						if yamlBlock := extractYAMLBlock(igContent); yamlBlock != "" {
							specChecks := checkCommentSpecificity(yamlBlock, plan)
							for i := range specChecks {
								specChecks[i].Name = hostname + "_" + specChecks[i].Name
							}
							checks = append(checks, specChecks...)
						}
					}
				}

				// Integration-guide code-block check: showcase READMEs must
				// contain at least one non-YAML code block in the integration
				// guide — application code a user adjusts to run on Zerops
				// (trust proxy, bind 0.0.0.0, Vite config allowedHosts, etc.).
				// The v12 audit found most READMEs were 95% zerops.yaml
				// comments with only one real code-adjustment section; the
				// floor forces every integration guide to earn its keep.
				// Worker targets commonly have no user-facing code changes,
				// so the check is scoped to non-worker targets.
				if !appTarget.IsWorker {
					codeChecks := checkIntegrationGuideCodeBlocks(string(readmeContent), plan)
					for i := range codeChecks {
						codeChecks[i].Name = hostname + "_" + codeChecks[i].Name
					}
					checks = append(checks, codeChecks...)
				}

				// Predecessor-as-floor check: the showcase's knowledge-base
				// must contain net-new gotchas, not clones of the injected
				// predecessor recipe's Gotchas section. Per-target so a
				// dual-runtime recipe can't pass by piling every new gotcha
				// into the API README while the frontend clones only.
				floorChecks := checkKnowledgeBaseExceedsPredecessor(string(readmeContent), plan, predecessorStems)
				for i := range floorChecks {
					floorChecks[i].Name = hostname + "_" + floorChecks[i].Name
				}
				checks = append(checks, floorChecks...)
			}
		}

		// Per-worker-target predecessor-floor loop. Separate-codebase workers
		// ship their own README; the appTargets loop above intentionally filters
		// them out (`!t.IsWorker`) because the zerops.yaml + dev/prod setup
		// shape checks are app-specific. The floor check is codebase-agnostic
		// — any README with a knowledge-base fragment can be measured — so we
		// run it here with the same per-hostname name prefixing the app loop uses.
		for _, workerTarget := range workerTargets {
			hostname := workerTarget.Hostname
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
				// Non-existence of a separate-codebase worker README is already
				// a problem — but the existing appTargets loop reports missing
				// app READMEs, and the same structural expectation applies here.
				checks = append(checks, workflow.StepCheck{
					Name: hostname + "_readme_exists", Status: statusFail,
					Detail: fmt.Sprintf("README.md not found at %s", readmePath),
				})
				continue
			}
			floorChecks := checkKnowledgeBaseExceedsPredecessor(string(readmeContent), plan, predecessorStems)
			for i := range floorChecks {
				floorChecks[i].Name = hostname + "_" + floorChecks[i].Name
			}
			checks = append(checks, floorChecks...)
		}

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
			detail := fmt.Sprintf("missing fragment markers for %q", name)
			if hasStart && !hasEnd {
				detail = fmt.Sprintf("fragment %q has start marker but missing end marker", name)
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
