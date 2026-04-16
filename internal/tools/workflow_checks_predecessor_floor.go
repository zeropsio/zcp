package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// minNetNewGotchas was the floor for showcase-tier knowledge-base
// fragments before the v8.78 reform. The check is now informational
// only — predecessor overlap is fine — and the constant is removed
// to satisfy the unused-symbol lint. The new authoritative gate for
// "this codebase ships enough gotchas" is checkServiceCoverage, which
// requires at least one gotcha per managed-service category in the plan.

// minAuthenticGotchas is the shape-classifier floor. Even when net-new
// gotcha tokens don't overlap the predecessor, the content can still be
// scaffold-self-referential narration ("Shared database with the API",
// "NATS authentication"). The authenticity floor requires at least 3
// gotchas to score as ShapeAuthentic — meaning they mention a platform
// anchor (Zerops, L7, execOnce, ${env_var}) AND/OR describe a concrete
// failure mode (fails with, returns error, blocked request). The v12
// audit of nestjs-showcase found roughly half of emitted gotchas were
// synthetic; this floor is what forces the classifier threshold to
// matter in the generate check.
const minAuthenticGotchas = 3

// checkKnowledgeBaseExceedsPredecessor is the predecessor-overlap
// inventory check. It reads the knowledge-base fragment from a codebase
// README, extracts the bolded gotcha stems, and reports how many overlap
// with the injected predecessor recipe's Gotchas section.
//
// v8.78 reform — this check no longer FAILS on predecessor overlap.
// Standalone recipes are read in isolation; including the most-relevant
// predecessor gotchas alongside net-new ones is correct, not a regression.
// The check now always passes (when applicable) and emits the count as
// informational detail. The authoritative gate for "this codebase covers
// enough" is now checkServiceCoverage, which requires at least one gotcha
// per managed-service category present in the plan — overlap is fine,
// gaps are not.
//
// Skipped for minimal/hello-world tiers and when the predecessor has no
// extractable Gotchas section (the existing knowledge_base_gotchas check
// still enforces section presence + non-emptiness).
//
// Authenticity classifier ride-along is unchanged — the synthetic-stem
// floor still fires here.
func checkKnowledgeBaseExceedsPredecessor(content string, plan *workflow.RecipePlan, predecessorStems []string) []workflow.StepCheck {
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
	authenticityChecks := checkKnowledgeBaseAuthenticity(kbContent)
	checks := make([]workflow.StepCheck, 0, 1+len(authenticityChecks))
	checks = append(checks, workflow.StepCheck{
		Name:   "knowledge_base_exceeds_predecessor",
		Status: statusPass,
		Detail: fmt.Sprintf("%d of %d gotchas are net-new vs predecessor (overlap is fine — service-coverage check enforces category breadth)", netNew, len(emitted)),
	})
	checks = append(checks, authenticityChecks...)
	return checks
}

// checkKnowledgeBaseAuthenticity runs the shape classifier over each
// emitted gotcha and fails when fewer than minAuthenticGotchas qualify
// as authentic (platform-anchored or failure-mode described). The v12
// audit found that ~half of emitted gotchas were scaffold-self-referential
// narration — architectural descriptions, credential restatements, or
// quirks of the scaffold's own code that a clean-slate integrator would
// never hit. The net-new floor alone can't catch these because synthetic
// gotchas have novel tokens relative to the predecessor. The authenticity
// check is the shape-based complement.
func checkKnowledgeBaseAuthenticity(kbContent string) []workflow.StepCheck {
	entries := workflow.ExtractGotchaEntries(kbContent)
	if len(entries) == 0 {
		return nil
	}
	authentic := workflow.CountAuthenticGotchas(entries)
	if authentic >= minAuthenticGotchas {
		return []workflow.StepCheck{{
			Name:   "knowledge_base_authenticity",
			Status: statusPass,
			Detail: fmt.Sprintf("%d of %d gotchas are authentic (platform-anchored or failure-mode described)", authentic, len(entries)),
		}}
	}
	// Build a short list of the synthetic stems so the retry knows which
	// entries to rewrite or replace.
	var synthetic []string
	for _, e := range entries {
		if workflow.ClassifyGotcha(e.Stem, e.Body) == workflow.ShapeSynthetic {
			synthetic = append(synthetic, e.Stem)
		}
	}
	return []workflow.StepCheck{{
		Name:   "knowledge_base_authenticity",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"only %d authentic gotcha(s) (required %d) — %d of %d read as scaffold-self-referential narration rather than real integration traps. Synthetic stems: %s. A real gotcha describes either (a) a Zerops platform constraint (execOnce, L7 balancer, ${env_var} references, base: static, httpSupport), or (b) a concrete failure mode a user would observe (\"fails with DNS errors\", \"returns empty results\", \"Blocked request\", \"shadows env var\"), or both. Rewrite each synthetic stem to name the trap + symptom, OR replace it with a gotcha you actually hit during generate/deploy. Architectural narration (\"Shared database with the API\"), credential descriptions (\"NATS authentication\"), and obvious restatements (\"Static base has no Node runtime\") do not count.",
			authentic, minAuthenticGotchas, len(synthetic), len(entries),
			strings.Join(synthetic, ", "),
		),
	}}
}
