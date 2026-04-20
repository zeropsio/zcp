package check

import (
	"context"
	"fmt"
	"io"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
)

// ig-per-item-code enforces that every H3 inside the integration-guide
// fragment carries at least one fenced code block. Showcase-tier only —
// minimal IG shapes sometimes have prose-only H3s legitimately.
func init() {
	registerCheck("ig-per-item-code", runIGPerItemCode)
}

func runIGPerItemCode(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("ig-per-item-code", stderr)
	cf := addCommonFlags(fs)
	hostname := fs.String("hostname", "", "codebase hostname (required)")
	showcase := fs.Bool("showcase", false, "treat as showcase tier (apply stricter floors)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *hostname == "" {
		fmt.Fprintln(stderr, "ig-per-item-code: --hostname is required")
		return 1
	}
	readmePath, content, err := readHostnameReadme(cf.path, *hostname)
	if err != nil {
		fmt.Fprintf(stderr, "ig-per-item-code: reading %s: %v\n", readmePath, err)
		return 1
	}
	checks := opschecks.CheckIGPerItemCode(ctx, content, *showcase)
	return emitResults(stdout, cf.json, checks)
}
