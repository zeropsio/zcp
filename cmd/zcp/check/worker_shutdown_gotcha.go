package check

import (
	"context"
	"fmt"
	"io"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// worker-shutdown-gotcha fails when a separate-codebase worker's
// README has no gotcha covering graceful shutdown on SIGTERM during
// rolling deploys. Mirrors worker-queue-group-gotcha's --is-worker /
// --shares-codebase-with flags since both predicates apply the same
// opt-in contract.
func init() {
	registerCheck("worker-shutdown-gotcha", runWorkerShutdownGotcha)
}

func runWorkerShutdownGotcha(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("worker-shutdown-gotcha", stderr)
	cf := addCommonFlags(fs)
	hostname := fs.String("hostname", "", "worker hostname (required)")
	isWorker := fs.Bool("is-worker", false, "treat target as a worker (required for the check to fire)")
	sharesCodebaseWith := fs.String("shares-codebase-with", "", "host hostname this worker shares a codebase with (present => skip)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *hostname == "" {
		fmt.Fprintln(stderr, "worker-shutdown-gotcha: --hostname is required")
		return 1
	}
	readmePath, content, err := readHostnameReadme(cf.path, *hostname)
	if err != nil {
		fmt.Fprintf(stderr, "worker-shutdown-gotcha: reading %s: %v\n", readmePath, err)
		return 1
	}
	target := workflow.RecipeTarget{
		Hostname:           *hostname,
		IsWorker:           *isWorker,
		SharesCodebaseWith: *sharesCodebaseWith,
	}
	checks := opschecks.CheckWorkerShutdownGotcha(ctx, *hostname, content, target)
	return emitResults(stdout, cf.json, checks)
}
