package check

import (
	"context"
	"fmt"
	"io"

	opschecks "github.com/zeropsio/zcp/internal/ops/checks"
	"github.com/zeropsio/zcp/internal/workflow"
)

// worker-queue-group-gotcha fails when a separate-codebase worker's
// README has no gotcha covering queue-group semantics under
// `minContainers > 1`. The predicate is a no-op when the target is not
// a worker or shares its codebase; the shim exposes both facts as
// flags so the author can opt in/out explicitly.
//
// --is-worker (default false): without it the predicate returns nil
// (SKIP). Present: predicate enforces the gotcha presence.
// --shares-codebase-with=<host>: when set the predicate skips (shared-
// codebase workers inherit the host target's README).
func init() {
	registerCheck("worker-queue-group-gotcha", runWorkerQueueGroupGotcha)
}

func runWorkerQueueGroupGotcha(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("worker-queue-group-gotcha", stderr)
	cf := addCommonFlags(fs)
	hostname := fs.String("hostname", "", "worker hostname (required)")
	isWorker := fs.Bool("is-worker", false, "treat target as a worker (required for the check to fire)")
	sharesCodebaseWith := fs.String("shares-codebase-with", "", "host hostname this worker shares a codebase with (present => skip)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *hostname == "" {
		fmt.Fprintln(stderr, "worker-queue-group-gotcha: --hostname is required")
		return 1
	}
	readmePath, content, err := readHostnameReadme(cf.path, *hostname)
	if err != nil {
		fmt.Fprintf(stderr, "worker-queue-group-gotcha: reading %s: %v\n", readmePath, err)
		return 1
	}
	target := workflow.RecipeTarget{
		Hostname:           *hostname,
		IsWorker:           *isWorker,
		SharesCodebaseWith: *sharesCodebaseWith,
	}
	checks := opschecks.CheckWorkerQueueGroupGotcha(ctx, *hostname, content, target)
	return emitResults(stdout, cf.json, checks)
}
