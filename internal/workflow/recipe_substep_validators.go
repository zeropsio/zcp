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
// Receives the attestation so validators can reject empty or boilerplate
// completions; receives plan + state for validators that walk the mounted
// filesystem or inspect recipe shape.
type SubStepValidator func(ctx context.Context, plan *RecipePlan, state *RecipeState, attestation string) *SubStepValidationResult

// getSubStepValidator returns the validator for a sub-step, or nil if the
// sub-step uses attestation-only completion (no automated check).
func getSubStepValidator(subStepName string) SubStepValidator {
	switch subStepName {
	case SubStepZeropsYAML:
		return validateZeropsYAML
	case SubStepReadme:
		return validateReadme
	case SubStepSubagent:
		// Feature sub-agent dispatch at deploy step 4b. v11 shipped a
		// scaffold-quality frontend because the main agent autonomously
		// decided step 4b was "already done" and never dispatched the
		// feature sub-agent. The validator forces a non-trivial attestation
		// describing what the feature sub-agent produced, eliminating the
		// "already done" escape.
		return validateFeatureSubagent
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

// featureSubagentMinAttestationLen is the minimum attestation length the
// feature-subagent sub-step accepts. Empty, one-word, and "already done"-
// class attestations are rejected; anything above the floor must actually
// name what the feature sub-agent produced. The number is a proxy, not a
// perfect check — but it is sharp enough to block the v11 skip and force
// the agent to narrate its dispatch.
const featureSubagentMinAttestationLen = 40

// validateFeatureSubagent enforces the deploy-step sub-step "subagent"
// (dispatch of the feature sub-agent). v11 shipped a scaffold-quality
// frontend because the main agent read the existing scaffold code, decided
// the features were "already complete", and skipped step 4b — the
// "MANDATORY for Type 4 showcase" label was prose, not a forcing function.
// The validator rejects empty/short attestations so deploy cannot complete
// until the agent actually dispatches the sub-agent and describes what it
// produced. See docs/implementation-v11-findings.md for the v7-vs-v11
// component comparison that motivated the fix.
func validateFeatureSubagent(_ context.Context, _ *RecipePlan, _ *RecipeState, attestation string) *SubStepValidationResult {
	trimmed := strings.TrimSpace(attestation)
	if trimmed == "" {
		return &SubStepValidationResult{
			Passed: false,
			Issues: []string{"feature sub-agent attestation is empty — dispatch the sub-agent before completing this sub-step"},
			Guidance: "## feature-subagent sub-step\n\n" +
				"Type 4 showcase recipes require the feature sub-agent to fill in the dashboard UX even if the scaffold code looks complete. The scaffold brief at generate time is intentionally narrow (see `zerops_guidance topic=\"scaffold-subagent-brief\"`); the feature sub-agent's job is the rich UX — styled forms, tables with history, contextual hints, typed interfaces, error flashes, empty states, `$effect` hooks that auto-load data.\n\n" +
				"Fetch the sub-agent brief: `zerops_guidance topic=\"subagent-brief\"`\n\n" +
				"Dispatch the feature sub-agent via the Agent tool, then call `zerops_workflow action=\"complete\" step=\"deploy\" substep=\"subagent\" attestation=\"<describe the files it produced and what features it implemented>\"`.\n\n" +
				"Do NOT skip this sub-step based on \"the scaffold code looks complete\" — v11 shipped a scaffold as a dashboard for exactly that reason. See docs/implementation-v11-findings.md.",
		}
	}
	if len(trimmed) < featureSubagentMinAttestationLen {
		return &SubStepValidationResult{
			Passed: false,
			Issues: []string{fmt.Sprintf("feature sub-agent attestation too short (%d chars, need >= %d) — name the files the sub-agent wrote and the features it implemented", len(trimmed), featureSubagentMinAttestationLen)},
			Guidance: "## feature-subagent sub-step\n\n" +
				"A one-liner like \"already done\" or \"dispatched sub-agent\" is not enough. The attestation must describe what the feature sub-agent actually produced — the files it wrote, the features it implemented against live services. Example:\n\n" +
				"> feature sub-agent added styled JobsSection.svelte with typed Task interface, dispatch form, refresh button, and pending-task badge\n\n" +
				"This becomes part of the session log and the close-step review uses it to verify the deploy step ran to completion.",
		}
	}
	return &SubStepValidationResult{Passed: true}
}

// validateZeropsYAML checks the zerops.yaml the agent wrote by reading from
// SSHFS mounts. Checks: file exists, contains expected setup count, comment
// ratio >= 30%, dev and prod envVariables differ. These are the most common
// generate failures.
func validateZeropsYAML(_ context.Context, plan *RecipePlan, _ *RecipeState, _ string) *SubStepValidationResult {
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
func validateReadme(_ context.Context, plan *RecipePlan, _ *RecipeState, _ string) *SubStepValidationResult {
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
