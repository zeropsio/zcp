package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/workflow"
)

// buildRecipeStepChecker returns a step checker for the given recipe step.
func buildRecipeStepChecker(step, _, stateDir string) workflow.RecipeStepChecker {
	switch step {
	case workflow.RecipeStepGenerate:
		return checkRecipeGenerate(stateDir)
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
			outputDir = filepath.Dir(filepath.Dir(stateDir))
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
func checkRecipeGenerate(stateDir string) workflow.RecipeStepChecker {
	return func(_ context.Context, plan *workflow.RecipePlan, state *workflow.RecipeState) (*workflow.StepCheckResult, error) {
		if plan == nil {
			return nil, nil
		}

		projectRoot := filepath.Dir(filepath.Dir(stateDir))

		var checks []workflow.StepCheck

		// Find the app target (first target with role "app").
		var appHostname string
		for _, t := range plan.Targets {
			if workflow.IsRuntimeType(t.Type) && !t.IsWorker {
				appHostname = t.Hostname
				break
			}
		}
		if appHostname == "" && len(plan.Targets) > 0 {
			appHostname = plan.Targets[0].Hostname
		}

		// Check zerops.yaml existence and structure.
		// Try mount paths: {hostname}dev (standard mode), {hostname} (bare), then project root.
		ymlDir := projectRoot
		for _, candidate := range []string{appHostname + "dev", appHostname} {
			mountPath := filepath.Join(projectRoot, candidate)
			if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
				ymlDir = mountPath
				break
			}
		}

		doc, parseErr := ops.ParseZeropsYml(ymlDir)
		if parseErr != nil {
			checks = append(checks, workflow.StepCheck{
				Name: "zerops_yml_exists", Status: statusFail,
				Detail: fmt.Sprintf("zerops.yaml not found: %v", parseErr),
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: "zerops_yml_exists", Status: statusPass,
			})
			checks = append(checks, checkRecipeSetups(doc, appHostname, plan)...)
		}

		// Check README fragments.
		readmePath := filepath.Join(ymlDir, "README.md")
		readmeContent, readErr := os.ReadFile(readmePath)
		if readErr != nil {
			checks = append(checks, workflow.StepCheck{
				Name: "readme_exists", Status: statusFail,
				Detail: fmt.Sprintf("README.md not found at %s", readmePath),
			})
		} else {
			checks = append(checks, workflow.StepCheck{
				Name: "readme_exists", Status: statusPass,
			})
			checks = append(checks, checkReadmeFragments(string(readmeContent))...)
		}

		allPassed := true
		for i := range checks {
			if checks[i].Status == statusFail {
				allPassed = false
				break
			}
		}
		summary := "recipe generate checks passed"
		if !allPassed {
			summary = "recipe generate checks failed"
		}
		return &workflow.StepCheckResult{
			Passed: allPassed, Checks: checks, Summary: summary,
		}, nil
	}
}

// checkRecipeSetups validates zerops.yaml has both dev and prod setup entries.
// Recipes write BOTH setups at generate time — single-write avoids drift between
// the file that deploys and the README integration-guide fragment that documents
// it. zerops.yaml uses generic names: `setup: dev` and `setup: prod`.
// The deploy tool's --setup param maps hostname→setup at cross-deploy time.
func checkRecipeSetups(doc *ops.ZeropsYmlDoc, hostname string, _ *workflow.RecipePlan) []workflow.StepCheck {
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
	prodEntry := doc.FindEntry("prod")
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
	if recipeEnvMapsEqual(devEnv, prodEnv) {
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

// recipeEnvMapsEqual reports whether two env var maps carry the same keys and values.
func recipeEnvMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
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
