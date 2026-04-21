package check

import (
	"context"
	"fmt"
	"io"

	"github.com/zeropsio/zcp/internal/ops"
	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
)

// env-refs validates cross-service env-variable references in a single
// codebase's zerops.yaml. The shim reads {path}/{hostname}dev/zerops.yaml
// (with fallback to {path}/{hostname}/) and calls the predicate with an
// empty discoveredEnvVars map + empty liveHostnames — the shim version
// flags malformed references regardless of platform state.
//
// Gate path: state.DiscoveredEnvVars is populated from the live API.
// Shim path: author runs against a checkout before provisioning, so
// the check only catches shape errors (malformed `${...}` syntax,
// missing host prefix). That's the useful author-side feedback.
func init() {
	registerCheck("env-refs", runEnvRefs)
}

func runEnvRefs(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("env-refs", stderr)
	cf := addCommonFlags(fs)
	hostname := fs.String("hostname", "", "codebase hostname (required)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *hostname == "" {
		fmt.Fprintln(stderr, "env-refs: --hostname is required")
		return 1
	}
	ymlDir := resolveHostnameDir(cf.path, *hostname)
	doc, err := ops.ParseZeropsYml(ymlDir)
	if err != nil {
		fmt.Fprintf(stderr, "env-refs: %v\n", err)
		return 1
	}
	entry := doc.FindEntry("dev")
	if entry == nil {
		fmt.Fprintf(stderr, "env-refs: %s/zerops.yaml has no setup: dev entry\n", ymlDir)
		return 1
	}
	checks := opschecks.CheckEnvRefs(ctx, *hostname, entry, map[string][]string{}, nil)
	return emitResults(stdout, cf.json, checks)
}
