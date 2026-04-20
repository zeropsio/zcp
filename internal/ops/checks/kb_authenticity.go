package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

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

// CheckKnowledgeBaseAuthenticity runs the shape classifier over each
// emitted gotcha and fails when fewer than minAuthenticGotchas qualify
// as authentic (platform-anchored or failure-mode described). The v12
// audit found that ~half of emitted gotchas were scaffold-self-referential
// narration — architectural descriptions, credential restatements, or
// quirks of the scaffold's own code that a clean-slate integrator would
// never hit. The net-new floor alone can't catch these because synthetic
// gotchas have novel tokens relative to the predecessor. The authenticity
// check is the shape-based complement.
//
// Returns nil when the fragment has no extractable gotcha entries —
// the presence-of-fragment concern is a separate check's surface, and
// emitting a pass here on a missing fragment would be misleading.
func CheckKnowledgeBaseAuthenticity(_ context.Context, kbContent, hostname string) []workflow.StepCheck {
	entries := workflow.ExtractGotchaEntries(kbContent)
	if len(entries) == 0 {
		return nil
	}
	authentic := workflow.CountAuthenticGotchas(entries)
	if authentic >= minAuthenticGotchas {
		return []workflow.StepCheck{{
			Name:   "knowledge_base_authenticity",
			Status: StatusPass,
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
	readmePath := "README.md"
	if hostname != "" {
		readmePath = hostname + "/README.md"
	}
	syntheticList := strings.Join(synthetic, ", ")
	return []workflow.StepCheck{{
		Name:        "knowledge_base_authenticity",
		Status:      StatusFail,
		ReadSurface: fmt.Sprintf("%s knowledge-base fragment — bolded gotcha stems", readmePath),
		Required:    fmt.Sprintf("≥%d gotchas classified ShapeAuthentic (platform-anchored OR concrete failure mode)", minAuthenticGotchas),
		Actual:      fmt.Sprintf("%d authentic / %d total (%d synthetic)", authentic, len(entries), len(synthetic)),
		HowToFix: fmt.Sprintf(
			"Rewrite the synthetic stems in %s knowledge-base fragment to name a Zerops platform constraint (execOnce, L7 balancer, ${env_var}, httpSupport, base: static) AND/OR a concrete failure mode the reader would observe ('fails with DNS errors', 'returns empty results', 'Blocked request'). Replace any architectural narration ('Shared database with the API') with a real trap you hit during deploy. Synthetic stems: %s.",
			readmePath, syntheticList,
		),
		Detail: fmt.Sprintf(
			"only %d authentic gotcha(s) (required %d) — %d of %d read as scaffold-self-referential narration. Synthetic stems: %s.",
			authentic, minAuthenticGotchas, len(synthetic), len(entries), syntheticList,
		),
	}}
}
