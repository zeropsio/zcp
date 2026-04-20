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

// factual-claims verifies declarative numeric claims in import.yaml
// comments ("10 GB quota", "minContainers 3") match the adjacent YAML
// value in the same service block. Same env-folder resolution shape
// as comment-depth.
func init() {
	registerCheck("factual-claims", runFactualClaims)
}

func runFactualClaims(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("factual-claims", stderr)
	cf := addCommonFlags(fs)
	envIndex := fs.Int("env", -1, "environment index (0..5; required)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *envIndex < 0 || *envIndex >= workflow.EnvTierCount() {
		fmt.Fprintf(stderr, "factual-claims: --env must be in [0,%d)\n", workflow.EnvTierCount())
		return 1
	}
	folder := workflow.EnvFolder(*envIndex)
	importPath := filepath.Join(cf.path, folder, "import.yaml")
	data, err := os.ReadFile(importPath)
	if err != nil {
		fmt.Fprintf(stderr, "factual-claims: reading %s: %v\n", importPath, err)
		return 1
	}
	prefix := folder + "_import"
	checks := opschecks.CheckFactualClaims(ctx, string(data), prefix)
	return emitResults(stdout, cf.json, checks)
}
