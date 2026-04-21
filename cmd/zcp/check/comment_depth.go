package check

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// comment-depth grades the WHY-reasoning in an environment import.yaml's
// comments. --env=N resolves via workflow.EnvFolder to the canonical
// folder name (e.g. "0 — AI Agent") whose import.yaml the predicate
// consumes. Prefix passed to the predicate matches the gate's
// `{folder}_import` convention so the emitted check name is stable
// across gate and shim.
func init() {
	registerCheck("comment-depth", runCommentDepth)
}

func runCommentDepth(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("comment-depth", stderr)
	cf := addCommonFlags(fs)
	envIndex := fs.Int("env", -1, "environment index (0..5; required)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *envIndex < 0 || *envIndex >= workflow.EnvTierCount() {
		fmt.Fprintf(stderr, "comment-depth: --env must be in [0,%d)\n", workflow.EnvTierCount())
		return 1
	}
	folder := workflow.EnvFolder(*envIndex)
	importPath := filepath.Join(cf.path, folder, "import.yaml")
	data, err := os.ReadFile(importPath)
	if err != nil {
		fmt.Fprintf(stderr, "comment-depth: reading %s: %v\n", importPath, err)
		return 1
	}
	prefix := folder + "_import"
	checks := opschecks.CheckCommentDepth(ctx, string(data), prefix)
	return emitResults(stdout, cf.json, checks)
}
