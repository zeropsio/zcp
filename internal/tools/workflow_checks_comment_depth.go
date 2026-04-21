// Tests for: internal/tools/workflow_checks_comment_depth.go — env
// import.yaml comment depth rubric. This check grades comments on
// whether they explain WHY a decision was made, not WHAT the field
// does. Post-C-7d the predicate lives in internal/ops/checks; this
// file keeps a thin wrapper for the existing tool-layer callers.
package tools

import (
	"context"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkCommentDepth — tool-layer thin wrapper (post-C-7d) around
// opschecks.CheckCommentDepth. The predicate + reasoningMarkers +
// thresholds all moved into the ops/checks package.
func checkCommentDepth(ctx context.Context, content, prefix string) []workflow.StepCheck {
	return opschecks.CheckCommentDepth(ctx, content, prefix)
}
