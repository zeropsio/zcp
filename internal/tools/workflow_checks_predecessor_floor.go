package tools

import (
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// minNetNewGotchas is the floor for showcase-tier knowledge-base fragments.
// A showcase always adds managed services and architectural patterns that
// the predecessor recipe does not cover; three net-new gotchas matches the
// v7 gold-standard baseline (apidev had 3 clones + 3 net-new). The earlier
// floor of 2 admitted v11's apidev with 4 clones + 2 net-new, which still
// read as scaffold-quality commentary; 3 forces the agent to narrate at
// least one additional build-specific gotcha.
const minNetNewGotchas = 3

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
	var checks []workflow.StepCheck
	netNew := workflow.CountNetNewGotchas(emitted, predecessorStems)
	if netNew >= minNetNewGotchas {
		checks = append(checks, workflow.StepCheck{
			Name:   "knowledge_base_exceeds_predecessor",
			Status: statusPass,
			Detail: fmt.Sprintf("%d of %d gotchas are net-new vs predecessor", netNew, len(emitted)),
		})
	} else {
		cloned := len(emitted) - netNew
		checks = append(checks, workflow.StepCheck{
			Name:   "knowledge_base_exceeds_predecessor",
			Status: statusFail,
			Detail: fmt.Sprintf(
				"only %d net-new gotcha(s) (required %d) — %d of %d emitted stems clone the injected predecessor recipe. The predecessor's gotchas are a starting inventory, not the answer: re-evaluate each against this recipe's library and architecture choices (keep the ones that still apply, drop the ones that don't), then add gotchas narrated from THIS build — services and platform behaviors the predecessor doesn't cover, decisions you made, bugs you hit during generate/deploy.",
				netNew, minNetNewGotchas, cloned, len(emitted),
			),
		})
	}
	checks = append(checks, checkKnowledgeBaseAuthenticity(kbContent)...)
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
