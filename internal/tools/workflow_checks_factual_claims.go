package tools

import (
	"context"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkFactualClaims — tool-layer thin wrapper (post-C-7d) around
// opschecks.CheckFactualClaims. All comment-parsing and YAML-adjacent
// field lookup logic moved into the ops/checks package.
func checkFactualClaims(ctx context.Context, content, prefix string) []workflow.StepCheck {
	return opschecks.CheckFactualClaims(ctx, content, prefix)
}
