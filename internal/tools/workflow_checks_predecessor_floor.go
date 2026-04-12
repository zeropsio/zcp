package tools

import (
	"fmt"

	"github.com/zeropsio/zcp/internal/workflow"
)

// minNetNewGotchas is the floor for showcase-tier knowledge-base fragments.
// A showcase always adds managed services and architectural patterns that
// the predecessor recipe does not cover; two net-new gotchas is the minimum
// proof that the agent narrated something from the actual build instead of
// re-stating the predecessor's baseline.
const minNetNewGotchas = 2

// checkKnowledgeBaseExceedsPredecessor is the predecessor-as-floor check.
// It reads the knowledge-base fragment from a codebase README, extracts
// the bolded gotcha stems, and counts how many don't match any stem in
// the injected predecessor recipe's Gotchas section.
//
// The rule: for showcase-tier recipes, at least minNetNewGotchas stems
// must be net-new. Predecessor stems are a starting inventory — the agent
// may re-use the ones that still apply to the showcase's library and
// architecture choices, drop the ones that don't, and MUST add narrated
// gotchas for the services and patterns the predecessor doesn't cover.
//
// The check is a no-op for minimal/hello-world tiers (their predecessors
// are small and forcing "net-new" at that level produces noise) and when
// the predecessor has no extractable Gotchas section (no baseline means
// nothing to compare against; the existing knowledge_base_gotchas check
// still enforces that the section is present and non-empty).
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
	if netNew >= minNetNewGotchas {
		return []workflow.StepCheck{{
			Name:   "knowledge_base_exceeds_predecessor",
			Status: statusPass,
			Detail: fmt.Sprintf("%d of %d gotchas are net-new vs predecessor", netNew, len(emitted)),
		}}
	}
	cloned := len(emitted) - netNew
	return []workflow.StepCheck{{
		Name:   "knowledge_base_exceeds_predecessor",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"only %d net-new gotcha(s) (required %d) — %d of %d emitted stems clone the injected predecessor recipe. The predecessor's gotchas are a starting inventory, not the answer: re-evaluate each against this recipe's library and architecture choices (keep the ones that still apply, drop the ones that don't), then add gotchas narrated from THIS build — services and platform behaviors the predecessor doesn't cover, decisions you made, bugs you hit during generate/deploy.",
			netNew, minNetNewGotchas, cloned, len(emitted),
		),
	}}
}
