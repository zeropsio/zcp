package check

import (
	"context"
	"fmt"
	"io"

	"github.com/zeropsio/zcp/internal/ops"
	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
)

// env-self-shadow detects `KEY: ${KEY}` patterns in run.envVariables
// where the self-reference silently evaluates to empty — a v29 class
// that broke fresh deploys until one scaffold was rewritten.
func init() {
	registerCheck("env-self-shadow", runEnvSelfShadow)
}

func runEnvSelfShadow(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("env-self-shadow", stderr)
	cf := addCommonFlags(fs)
	hostname := fs.String("hostname", "", "codebase hostname (required)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *hostname == "" {
		fmt.Fprintln(stderr, "env-self-shadow: --hostname is required")
		return 1
	}
	ymlDir := resolveHostnameDir(cf.path, *hostname)
	doc, err := ops.ParseZeropsYml(ymlDir)
	if err != nil {
		fmt.Fprintf(stderr, "env-self-shadow: %v\n", err)
		return 1
	}
	entry := doc.FindEntry("dev")
	if entry == nil {
		fmt.Fprintf(stderr, "env-self-shadow: %s/zerops.yaml has no setup: dev entry\n", ymlDir)
		return 1
	}
	checks := opschecks.CheckEnvSelfShadow(ctx, *hostname, entry)
	return emitResults(stdout, cf.json, checks)
}
