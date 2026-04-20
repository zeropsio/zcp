package tools

import (
	"context"
	"fmt"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// minNetNewGotchas was the floor for showcase-tier knowledge-base
// fragments before the v8.78 reform. The check is now informational
// only — predecessor overlap is fine — and the constant is removed
// to satisfy the unused-symbol lint.

// checkKnowledgeBaseExceedsPredecessor is the predecessor-overlap
// inventory check. It reads the knowledge-base fragment from a codebase
// README, extracts the bolded gotcha stems, and reports how many overlap
// with the injected predecessor recipe's Gotchas section.
//
// v8.78 reform — this check no longer FAILS on predecessor overlap.
// Standalone recipes are read in isolation; including the most-relevant
// predecessor gotchas alongside net-new ones is correct, not a regression.
// The check now always passes (when applicable) and emits the count as
// informational detail.
//
// Skipped for minimal/hello-world tiers and when the predecessor has no
// extractable Gotchas section (the existing knowledge_base_gotchas check
// still enforces section presence + non-emptiness).
//
// Authenticity classifier ride-along is unchanged — the synthetic-stem
// floor still fires here.
// hostname identifies the codebase whose README is being inspected;
// passed through to checkKnowledgeBaseAuthenticity so the v8.96
// structured ReadSurface field can name `{hostname}/README.md`. Callers
// that don't have a hostname (predecessor-floor unit tests) can pass the
// empty string — the diagnostics still populate the file path with a
// generic "README.md".
func checkKnowledgeBaseExceedsPredecessor(ctx context.Context, content string, plan *workflow.RecipePlan, predecessorStems []string, hostname string) []workflow.StepCheck {
	if plan == nil || plan.Tier != workflow.RecipeTierShowcase {
		return nil
	}
	if len(predecessorStems) == 0 {
		return nil
	}
	kbContent := extractFragmentContent(content, "knowledge-base")
	if kbContent == "" {
		return nil
	}
	emitted := workflow.ExtractGotchaStems(kbContent)
	if len(emitted) == 0 {
		return nil
	}
	netNew := workflow.CountNetNewGotchas(emitted, predecessorStems)
	authenticityChecks := checkKnowledgeBaseAuthenticity(ctx, kbContent, hostname)
	checks := make([]workflow.StepCheck, 0, 1+len(authenticityChecks))
	checks = append(checks, workflow.StepCheck{
		Name:   "knowledge_base_exceeds_predecessor",
		Status: statusPass,
		Detail: fmt.Sprintf("%d of %d gotchas are net-new vs predecessor (overlap is fine — service-coverage check enforces category breadth)", netNew, len(emitted)),
	})
	checks = append(checks, authenticityChecks...)
	return checks
}

// checkKnowledgeBaseAuthenticity — tool-layer thin wrapper (post-C-7b)
// around opschecks.CheckKnowledgeBaseAuthenticity. minAuthenticGotchas,
// the shape-classifier threshold, and the structured fail payload all
// moved into the ops/checks package alongside the predicate.
func checkKnowledgeBaseAuthenticity(ctx context.Context, kbContent, hostname string) []workflow.StepCheck {
	return opschecks.CheckKnowledgeBaseAuthenticity(ctx, kbContent, hostname)
}
