package check

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
)

// manifest-honesty loads ZCP_CONTENT_MANIFEST.json from --mount-root,
// walks the per-hostname README tree, and calls the predicate. Fails
// when a DISCARD-marked fact's Jaccard-similar stem leaks into any
// shipped README. Accepts --mount-root alongside the shared --path
// (both names map to the same root; --mount-root wins when both given).
func init() {
	registerCheck("manifest-honesty", runManifestHonesty)
}

func runManifestHonesty(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("manifest-honesty", stderr)
	cf := addCommonFlags(fs)
	mountRoot := fs.String("mount-root", "", "recipe project root (alias for --path)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	root := resolveMountRoot(cf.path, *mountRoot)
	manifest, err := opschecks.LoadContentManifest(root)
	if err != nil {
		fmt.Fprintf(stderr, "manifest-honesty: load manifest: %v\n", err)
		return 1
	}
	readmes, err := collectReadmesByHost(root)
	if err != nil {
		fmt.Fprintf(stderr, "manifest-honesty: walking %s: %v\n", root, err)
		return 1
	}
	checks := opschecks.CheckManifestHonesty(ctx, manifest, readmes)
	return emitResults(stdout, cf.json, checks)
}

// resolveMountRoot picks --mount-root when provided, falling back to
// the shared --path (default "."). Normalizes to absolute path for
// downstream predicates that Rel-path against the root.
func resolveMountRoot(commonPath, mountRoot string) string {
	root := commonPath
	if mountRoot != "" {
		root = mountRoot
	}
	if root == "" {
		root = "."
	}
	if abs, err := filepath.Abs(root); err == nil {
		return abs
	}
	return root
}

// collectReadmesByHost walks mount-root for per-codebase README.md
// files and returns a map keyed by the trimmed-hostname ("apidev" ->
// "api"). Uses the same `{host}dev` naming convention the gate relies
// on. Folders without a README.md or whose name doesn't match the
// convention are skipped silently.
func collectReadmesByHost(root string) (map[string]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		var host string
		switch {
		case strings.HasSuffix(name, "dev"):
			host = strings.TrimSuffix(name, "dev")
		default:
			// Non-"*dev" dirs are env folders or unrelated — skip.
			continue
		}
		if host == "" {
			continue
		}
		readmePath := filepath.Join(root, name, "README.md")
		data, readErr := os.ReadFile(readmePath)
		if readErr != nil {
			continue
		}
		out[host] = string(data)
	}
	return out, nil
}
