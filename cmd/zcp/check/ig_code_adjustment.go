package check

import (
	"context"
	"fmt"
	"io"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
)

// ig-code-adjustment verifies the integration-guide fragment inside a
// hostname's README carries at least one non-YAML fenced code block at
// the showcase tier — the trap being a scaffold that pastes just the
// zerops.yaml without the per-framework adjustment diff.
//
// --showcase flag mirrors the tier gate the gate-side wrapper applies;
// when absent, the predicate's minimal-tier path skips.
func init() {
	registerCheck("ig-code-adjustment", runIGCodeAdjustment)
}

func runIGCodeAdjustment(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("ig-code-adjustment", stderr)
	cf := addCommonFlags(fs)
	hostname := fs.String("hostname", "", "codebase hostname (required)")
	showcase := fs.Bool("showcase", false, "treat as showcase tier (apply stricter floors)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *hostname == "" {
		fmt.Fprintln(stderr, "ig-code-adjustment: --hostname is required")
		return 1
	}
	readmePath, content, err := readHostnameReadme(cf.path, *hostname)
	if err != nil {
		fmt.Fprintf(stderr, "ig-code-adjustment: reading %s: %v\n", readmePath, err)
		return 1
	}
	checks := opschecks.CheckIGCodeAdjustment(ctx, content, *showcase)
	return emitResults(stdout, cf.json, checks)
}
