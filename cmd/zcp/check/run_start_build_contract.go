package check

import (
	"context"
	"fmt"
	"io"

	"github.com/zeropsio/zcp/internal/ops"
	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
)

// run-start-build-contract verifies that run.start is not accidentally
// a build-time command (npm run build, tsc, etc). Predicate emits only
// on fail — when run.start is shaped correctly the shim prints SKIP.
func init() {
	registerCheck("run-start-build-contract", runRunStartBuildContract)
}

func runRunStartBuildContract(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("run-start-build-contract", stderr)
	cf := addCommonFlags(fs)
	hostname := fs.String("hostname", "", "codebase hostname (required)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *hostname == "" {
		fmt.Fprintln(stderr, "run-start-build-contract: --hostname is required")
		return 1
	}
	ymlDir := resolveHostnameDir(cf.path, *hostname)
	doc, err := ops.ParseZeropsYml(ymlDir)
	if err != nil {
		fmt.Fprintf(stderr, "run-start-build-contract: %v\n", err)
		return 1
	}
	entry := doc.FindEntry("dev")
	if entry == nil {
		fmt.Fprintf(stderr, "run-start-build-contract: %s/zerops.yaml has no setup: dev entry\n", ymlDir)
		return 1
	}
	checks := opschecks.CheckRunStartBuildContract(ctx, *hostname, entry)
	return emitResults(stdout, cf.json, checks)
}
