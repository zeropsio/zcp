package tools

import (
	"context"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// C-9 (2026-04-20): `checkKnowledgeBaseExceedsPredecessor` was deleted
// in this commit. It was informational-only after the v8.78 rollback
// (predecessor overlap is fine; standalone recipes are read in
// isolation) and carried zero gate value. The authenticity check
// `checkKnowledgeBaseAuthenticity` is the upstream replacement —
// it grades gotcha shape (platform-anchored or failure-mode described)
// rather than predecessor-overlap count, and now fires directly from
// `checkCodebaseReadme` without the exceeds-predecessor wrapper.
//
// `workflow.PredecessorGotchaStems` and `workflow.CountNetNewGotchas`
// remain in the workflow package for now; their only consumer in
// production code was this file. Left in place for tests + future
// uses; C-15 cleanup may remove them if they remain unreferenced.

// checkKnowledgeBaseAuthenticity — tool-layer thin wrapper (post-C-7b)
// around opschecks.CheckKnowledgeBaseAuthenticity. minAuthenticGotchas,
// the shape-classifier threshold, and the structured fail payload all
// moved into the ops/checks package alongside the predicate.
func checkKnowledgeBaseAuthenticity(ctx context.Context, kbContent, hostname string) []workflow.StepCheck {
	return opschecks.CheckKnowledgeBaseAuthenticity(ctx, kbContent, hostname)
}
