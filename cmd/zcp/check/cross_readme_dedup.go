package check

import (
	"context"
	"fmt"
	"io"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
)

// cross-readme-dedup walks every `{host}dev/README.md` under --path,
// builds a map[host]readme, and calls the predicate. Pairwise gotcha
// uniqueness is enforced across every README; dupes fail.
func init() {
	registerCheck("cross-readme-dedup", runCrossReadmeDedup)
}

func runCrossReadmeDedup(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("cross-readme-dedup", stderr)
	cf := addCommonFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 1
	}
	root := resolveMountRoot(cf.path, "")
	readmes, err := collectReadmesByHost(root)
	if err != nil {
		fmt.Fprintf(stderr, "cross-readme-dedup: walking %s: %v\n", root, err)
		return 1
	}
	checks := opschecks.CheckCrossReadmeGotchaUniqueness(ctx, readmes)
	return emitResults(stdout, cf.json, checks)
}
