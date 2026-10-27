package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/tools"
	"github.com/zeropsio/zcp/internal/topology"
)

// `zcp lease release --branch <branch> [--repo <repo URL>] --json` is the
// Mate server's release of a conversation's branch on its archive
// (docs/spec-workflows.md §8 DP-35, docs/spec-mate.md MA-6): the tool's branch
// release over the deps the MCP server runs it on, never forced. It reads no
// stdin — a verb run() did not know would fall through to serve mode and wait
// on it — and writes its one answer to stdout; exit 0 released, 3 refused, 1
// anything else (tools.LeaseReleaseAnswer).

const leaseUsage = "usage: zcp lease release --branch <branch> [--repo <repo URL>] --json"

// leaseCallBudget bounds one `zcp lease release` end to end, inside the
// Mate's own 3 minute bound for it: a release waits up to 2 minutes for a
// sibling's release of the same branch (DP-4) before it refuses.
const leaseCallBudget = 150 * time.Second

// leaseBackgroundWait bounds the wait, once the answer is written, for the
// warm policy's background work the release started (a pool's top-up create,
// a clone into the container kept warm, DP-27/DP-32); work cut short is the
// next acquire's or release's to finish, and status says what never answered.
const leaseBackgroundWait = 20 * time.Second

// runLeaseCmd implements `zcp lease release`. args is everything after
// "lease"; resolve builds the lease operations' deps in this container.
func runLeaseCmd(args []string, stdout io.Writer, resolve func(context.Context) (ops.LeaseDeps, error)) int {
	asJSON := false
	for _, a := range args {
		if a == "--json" || a == "-json" {
			asJSON = true
		}
	}
	fail := func(msg string) int {
		printLease(stdout, tools.LeaseReleaseAnswer{Error: msg}, asJSON)
		return tools.LeaseReleaseExitFailed
	}
	if len(args) == 0 || args[0] != "release" {
		return fail(leaseUsage)
	}
	for _, a := range args[1:] {
		if name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-"); strings.HasPrefix(a, "-") && name == "force" {
			return fail("zcp lease release never forces: a lease holding unsaved work keeps its branch, and only the agent's zerops_lease action=release force=true, once the person agreed, discards it. " + leaseUsage)
		}
	}
	fs := flag.NewFlagSet("lease release", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	branch := fs.String("branch", "", "the branch whose leases to release")
	repo := fs.String("repo", "", "the repository's clone URL, when zcp leases the branch for several")
	fs.Bool("json", false, "answer as JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return fail(err.Error() + ". " + leaseUsage)
	}
	if fs.NArg() > 0 {
		return fail("unexpected argument " + fs.Arg(0) + ". " + leaseUsage)
	}
	if strings.TrimSpace(*branch) == "" {
		return fail("--branch names the branch whose leases to release. " + leaseUsage)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, leaseCallBudget)
	defer cancel()
	deps, err := resolve(ctx)
	if err != nil {
		return fail(err.Error())
	}
	var background sync.WaitGroup
	if deps.Warm != nil {
		deps.Warm.Go = background.Go
	}
	answer, code := tools.ReleaseLeaseBranchAnswer(ctx, deps, *repo, *branch)
	printLease(stdout, answer, asJSON)
	waitWithin(&background, leaseBackgroundWait)
	return code
}

// waitWithin waits for wg, at most d.
func waitWithin(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
}

// printLease writes answer to stdout: JSON under --json, else one line.
func printLease(stdout io.Writer, answer tools.LeaseReleaseAnswer, asJSON bool) {
	if asJSON {
		data, err := json.Marshal(answer)
		if err != nil {
			data = []byte(`{"error":"marshal the answer"}`)
		}
		_, _ = fmt.Fprintln(stdout, string(data))
		return
	}
	switch {
	case answer.Error != "":
		_, _ = fmt.Fprintln(stdout, "lease release: "+answer.Error)
	case len(answer.Refused) > 0:
		names := make([]string, 0, len(answer.Refused))
		for _, r := range answer.Refused {
			names = append(names, r.Lease)
		}
		_, _ = fmt.Fprintln(stdout, "lease release refused, nothing released: "+strings.Join(names, ", ")+" keep the branch (--json says why)")
	case len(answer.Released) == 0:
		_, _ = fmt.Fprintln(stdout, "lease release: no lease of the branch is held")
	default:
		names := make([]string, 0, len(answer.Released))
		for _, r := range answer.Released {
			names = append(names, r.Lease+" ("+r.Outcome+")")
		}
		_, _ = fmt.Fprintln(stdout, "lease release: released "+strings.Join(names, ", "))
	}
}

// resolveLiveLeaseDeps builds the lease operations' deps the way the Mate's
// service builds its platform client (tools.KeepMateDevGit): the container's
// live env store — the Mate server's own environment carries neither the key
// nor the project — falling back to this process's environment; the state dir
// the MCP server keeps under the code mount; the pools' orchestrators, the
// lease's mount and ssh as zerops_lease reaches them (tools.LeaseDepsFor).
func resolveLiveLeaseDeps(ctx context.Context) (ops.LeaseDeps, error) {
	lookup := mate.LiveLookup(mate.LiveEnvStorePath)
	rt := runtime.DetectFrom(lookup)
	if !rt.InContainer {
		return ops.LeaseDeps{}, errors.New("zcp lease release runs inside the project's zcp container: a pool's orchestrator answers only there")
	}
	key := lookup(hq.EnvKey)
	if key == "" {
		return ops.LeaseDeps{}, fmt.Errorf("no %s in this container's environment", hq.EnvKey)
	}
	client, err := platform.NewProjectClient(ctx, key, mate.ResolveAPIHost(lookup("ZCP_API_HOST")), rt.ProjectID)
	if err != nil {
		return ops.LeaseDeps{}, fmt.Errorf("build the platform client: %w", err)
	}
	stateDir := filepath.Join(topology.CodeMountRoot, ".zcp", "state")
	swarm := ops.NewSwarmFactory(&http.Client{Timeout: ops.LeaseCallTimeout})
	return tools.LeaseDepsFor(client, rt.ProjectID, stateDir, swarm, platform.NewSystemMounter(), platform.NewSystemSSHDeployer(), rt), nil
}
