package tools

import (
	"context"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// checkSymbolContractEnvVarConsistency — tool-layer thin wrapper
// (post-C-7d) around opschecks.CheckSymbolContractEnvVarConsistency.
// The v34-class cross-scaffold env-var diff + sibling map + source-file
// scan all moved into the ops/checks package. See check-rewrite.md §16
// (new architecture-level check landed in C-6; predicate migrated to
// ops/checks in C-7d).
func checkSymbolContractEnvVarConsistency(ctx context.Context, projectRoot string, contract workflow.SymbolContract) []workflow.StepCheck {
	return opschecks.CheckSymbolContractEnvVarConsistency(ctx, projectRoot, contract)
}
