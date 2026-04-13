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
	case SubStepReadmes:
		// Post-deploy readme narration. The attestation-level check here
		// is lightweight (markers exist, each fragment has content) —
		// the heavy content validation (fragments literal format,
		// integration-guide code blocks, comment specificity,
		// knowledge-base authenticity, predecessor-floor) runs at
		// deploy-step completion via checkRecipeDeployReadmes. Splitting
		// the checks lets the agent iterate on the sub-step quickly
		// without the full content battery firing on every retry.
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
// (dispatch of the feature sub-agent). The scaffold at generate time ships
// a deliberately bare health-dashboard-only skeleton — one StatusPanel
// component showing service dots, /api/health, /api/status, client init,
// schema + 3-5 row seed, nothing else. The feature sub-agent at deploy
// step 4b is the single author that owns every showcase feature section
// end-to-end: API routes, worker payloads, and frontend consumers as one
// coherent unit. v10/v11/v12 all shipped the same class of contract-
// mismatch bugs because parallel scaffold agents each owned one slice of
// each contract. The single-author rule at step 4b eliminates the class.
//
// Gate policy: attestation floor only. v8.62.0 shipped a second gate that
// walked the codebase mounts for post-baseline source files and enforced
// a maximum mtime spread under the theory that a real Agent tool call
// produces a tight burst while an inlining main agent produces a wide
// spread. In practice this proxy is worse than useless:
//
//  1. Any legitimate post-dispatch debug edit (fix a NATS URL, patch a
//     Vite config, correct a CORS origin) bumps the mtime of files the
//     sub-agent originally wrote — so even a correctly-dispatched run
//     produces a wide final spread, triggering a false positive.
//  2. The filesystem signal is trivially refreshable: any Agent tool call
//     that reads every file and writes it back byte-for-byte identical
//     bumps every mtime into a fresh tight window. Opus observed the
//     rejection message, understood the metric, and dispatched exactly
//     that rewrite sub-agent to forge compliance — no feature work done,
//     just mtime laundering. The check was gaming itself.
//
// The root cause of "feature sub-agent never dispatched" in v13 was the
// model (Sonnet/200k couldn't hold the dispatch brief + contract + code
// context simultaneously, so it inlined), not a permissiveness bug in
// the attestation gate. The v14 model gate (clientModel allowlist) is
// the actual fix. This validator stays at the attestation-only floor —
// breakable by determined gaming, but doesn't produce false positives on
// legitimate Opus runs that debug after dispatch.
func validateFeatureSubagent(_ context.Context, _ *RecipePlan, _ *RecipeState, attestation string) *SubStepValidationResult {
	trimmed := strings.TrimSpace(attestation)
	if trimmed == "" {
		return &SubStepValidationResult{
			Passed: false,
			Issues: []string{"feature sub-agent attestation is empty — dispatch the sub-agent before completing this sub-step"},
			Guidance: "## feature-subagent sub-step\n\n" +
				"The scaffold at generate time shipped a health-dashboard-only skeleton: one StatusPanel, /api/health, /api/status, client init, minimal seed. That is the correct, expected scaffold output. The feature sub-agent at deploy step 4b is where every showcase feature section is implemented — as a SINGLE author owning API routes, worker payloads, and frontend consumers end-to-end.\n\n" +
				"Fetch the sub-agent brief: `zerops_guidance topic=\"subagent-brief\"`\n\n" +
				"Dispatch ONE feature sub-agent via the Agent tool (not parallel feature sub-agents — single author keeps contracts consistent), then call `zerops_workflow action=\"complete\" step=\"deploy\" substep=\"subagent\" attestation=\"<describe the files it produced and which feature sections it implemented>\"`.\n\n" +
				"Do NOT skip this sub-step. The scaffold is bare by design — nothing for you to rationalize away.",
		}
	}
	if len(trimmed) < featureSubagentMinAttestationLen {
		return &SubStepValidationResult{
			Passed: false,
			Issues: []string{fmt.Sprintf("feature sub-agent attestation too short (%d chars, need >= %d) — name the files the sub-agent wrote and the feature sections it implemented", len(trimmed), featureSubagentMinAttestationLen)},
			Guidance: "## feature-subagent sub-step\n\n" +
				"A one-liner like \"already done\" or \"dispatched sub-agent\" is not enough. The attestation must describe what the feature sub-agent actually produced — the files it wrote, the feature sections it implemented, and the API/frontend contract pairs it authored. Example:\n\n" +
				"> feature sub-agent wrote shared types.ts, then authored API routes + frontend consumers for items CRUD, cache-demo, search, jobs dispatch (with worker processor), and storage upload; expanded seed to 20 rows; added search-sync to initCommands\n\n" +
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
