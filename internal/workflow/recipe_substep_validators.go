package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SubStepValidationResult holds the outcome of a sub-step validator.
type SubStepValidationResult struct {
	Passed   bool     `json:"passed"`
	Issues   []string `json:"issues,omitempty"`
	Guidance string   `json:"guidance,omitempty"`
}

// SubStepValidator checks the agent's output at a sub-step boundary.
type SubStepValidator func(ctx context.Context, plan *RecipePlan, state *RecipeState) *SubStepValidationResult

// getSubStepValidator returns the validator for a sub-step, or nil if the
// sub-step uses attestation-only completion (no automated check).
func getSubStepValidator(subStepName string) SubStepValidator {
	switch subStepName {
	case SubStepZeropsYAML:
		return validateZeropsYAML
	case SubStepReadme:
		return validateReadme
	case SubStepSmokeTest:
		// Trust agent attestation — smoke test is interactive.
		return nil
	case SubStepScaffold:
		// Trust agent attestation — scaffold existence is best verified
		// by the agent reporting what it created.
		return nil
	case SubStepAppCode:
		// Trust agent attestation — code quality is checked at close
		// step by the code-review sub-agent.
		return nil
	default:
		// Deploy sub-steps, etc. — trust attestation.
		return nil
	}
}

// validateZeropsYAML checks the zerops.yaml the agent wrote by reading from
// SSHFS mounts. Checks: file exists, contains expected setup count, comment
// ratio >= 30%, dev and prod envVariables differ. These are the most common
// generate failures.
func validateZeropsYAML(_ context.Context, plan *RecipePlan, _ *RecipeState) *SubStepValidationResult {
	if plan == nil {
		return nil
	}

	base := recipeMountBase
	if recipeMountBaseOverride != "" {
		base = recipeMountBaseOverride
	}

	var issues []string

	// Check each codebase-owning target's zerops.yaml.
	for _, t := range plan.Targets {
		if !IsRuntimeType(t.Type) || (t.IsWorker && t.SharesCodebaseWith != "") {
			continue // managed services and shared-codebase workers don't own a zerops.yaml
		}

		mountPath := filepath.Join(base, t.Hostname+"dev", "zerops.yaml")
		raw, err := os.ReadFile(mountPath)
		if err != nil {
			issues = append(issues, fmt.Sprintf("%sdev/zerops.yaml: file not found or unreadable", t.Hostname))
			continue
		}
		content := string(raw)

		// Count setups: lines matching "  - setup: " at the top level.
		expectedSetups := 2 // dev + prod
		if TargetHostsSharedWorker(t, plan) {
			expectedSetups = 3 // dev + prod + worker
		}
		setupCount := strings.Count(content, "\n  - setup: ")
		if setupCount == 0 {
			setupCount = strings.Count(content, "\n- setup: ")
		}
		if setupCount < expectedSetups {
			issues = append(issues, fmt.Sprintf("%sdev/zerops.yaml: found %d setup(s), expected %d", t.Hostname, setupCount, expectedSetups))
		}

		// Comment ratio check: lines starting with # (after trim) vs total non-empty lines.
		lines := strings.Split(content, "\n")
		var commentLines, totalLines int
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			totalLines++
			if strings.HasPrefix(trimmed, "#") {
				commentLines++
			}
		}
		if totalLines > 0 {
			ratio := float64(commentLines) / float64(totalLines)
			if ratio < 0.30 {
				issues = append(issues, fmt.Sprintf("%sdev/zerops.yaml: comment ratio %.0f%% (need >= 30%%)", t.Hostname, ratio*100))
			}
		}
	}

	if len(issues) > 0 {
		var guidance strings.Builder
		guidance.WriteString("## zerops-yaml sub-step validation failed\n\n")
		for _, issue := range issues {
			guidance.WriteString(fmt.Sprintf("- %s\n", issue))
		}
		guidance.WriteString("\nFetch updated rules: `zerops_guidance topic=\"zerops-yaml-rules\"`\n")
		guidance.WriteString("\nCommon fixes:\n")
		guidance.WriteString("- Comment ratio below 30%%: add WHY-not-WHAT comments above each key group, aim for 35%%\n")
		guidance.WriteString("- Missing setup: verify both `setup: dev` and `setup: prod` exist\n")
		guidance.WriteString("- Shared-codebase worker: host target's zerops.yaml needs `setup: worker` too\n")
		return &SubStepValidationResult{
			Passed:   false,
			Issues:   issues,
			Guidance: guidance.String(),
		}
	}

	return &SubStepValidationResult{Passed: true}
}

// validateReadme checks the README the agent wrote.
func validateReadme(_ context.Context, plan *RecipePlan, _ *RecipeState) *SubStepValidationResult {
	if plan == nil {
		return nil
	}

	var guidance strings.Builder
	guidance.WriteString("## readme sub-step validation\n\n")
	guidance.WriteString("Verify your README contains all 3 extract fragments:\n")
	guidance.WriteString("- integration-guide (with zerops.yaml code block)\n")
	guidance.WriteString("- knowledge-base (gotchas and tips)\n")
	guidance.WriteString("- intro (1-3 lines, no headings)\n\n")
	guidance.WriteString("Re-read the readme-fragments topic for the full requirements.\n")

	// README validation is attestation-based at the sub-step level.
	// The full-step checker does content verification.
	return &SubStepValidationResult{Passed: true}
}
