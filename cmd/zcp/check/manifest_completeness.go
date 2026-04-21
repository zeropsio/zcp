package check

import (
	"context"
	"fmt"
	"io"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
)

// manifest-completeness loads ZCP_CONTENT_MANIFEST.json from --mount-root
// and the facts log from --facts=<path>, then calls the predicate. The
// predicate gracefully passes when --facts is empty or the log is
// unreadable (test context), so the shim does not enforce the flag —
// an author running locally without the log still sees a useful
// result.
func init() {
	registerCheck("manifest-completeness", runManifestCompleteness)
}

func runManifestCompleteness(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("manifest-completeness", stderr)
	cf := addCommonFlags(fs)
	mountRoot := fs.String("mount-root", "", "recipe project root (alias for --path)")
	facts := fs.String("facts", "", "path to the session facts log (optional; absent => graceful pass per predicate contract)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	root := resolveMountRoot(cf.path, *mountRoot)
	manifest, err := opschecks.LoadContentManifest(root)
	if err != nil {
		fmt.Fprintf(stderr, "manifest-completeness: load manifest: %v\n", err)
		return 1
	}
	checks := opschecks.CheckManifestCompleteness(ctx, manifest, *facts)
	return emitResults(stdout, cf.json, checks)
}
