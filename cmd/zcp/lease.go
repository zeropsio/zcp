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
)

// These Mate server verbs run against the MCP server's lease state and read no stdin.
const (
	leaseAcquireUsage = "usage: zcp lease acquire --pool <pool> --branch <branch> [--repo <repo URL>] --json"
	leaseReleaseUsage = "usage: zcp lease release --branch <branch> [--repo <repo URL>] --json or zcp lease release --lease-id <id> --json"
	// zcp starts warm-clone itself (ops.LeaseDeps.HoldWarmClone).
	leaseWarmCloneUsage = "usage: zcp lease warm-clone --pool <pool> --container <container id>"
	leaseUsage          = leaseAcquireUsage + "\n" + leaseReleaseUsage + "\n" + leaseWarmCloneUsage
)

// The verb supplies its name when the container prerequisite fails.
var errLeaseOutsideContainer = errors.New("runs inside the project's zcp container: a pool's orchestrator answers only there")

// leaseCallBudget bounds one `zcp lease release` end to end, inside the
// Mate's own 3 minute bound for it: a release waits up to 2 minutes for a
// sibling's release of the same branch (DP-4) before it refuses.
const leaseCallBudget = 150 * time.Second

// leaseReservationStart is how long after launch a `zcp lease acquire` may
// still start its reservation; later it reserves nothing and refuses (DP-39).
// With the reservation's own deadline (ops.LeaseCallTimeout) every
// reservation it starts is answered and recorded within about 700 s.
const leaseReservationStart = 60 * time.Second

// leaseAcquireBudget bounds one `zcp lease acquire` from launch: the
// reservation's latest start, its own deadline, and 240 s for the record, the
// alias, the mount and the checkout, so a reservation answered late is still
// checked out rather than handed back. A hand-back in progress runs to its
// end. The Mate allows 960 s before it ends the process (DP-39).
const leaseAcquireBudget = 900 * time.Second

// leaseBackgroundWait bounds the wait, once the answer is written, for the
// warm policy's background work the release started (a pool's top-up create,
// a clone into the container kept warm, DP-27/DP-32); work cut short is the
// next acquire's or release's to finish, and status says what never answered.
// A clone of a repository on the Mate's HQ is no such work: a zcp process of
// its own holds it (runLeaseWarmClone).
const leaseBackgroundWait = 20 * time.Second

// runLeaseCmd dispatches acquire and release. args is everything after
// "lease"; resolve builds the lease operations' deps in this container.
func runLeaseCmd(args []string, stdout io.Writer, resolve func(context.Context) (ops.LeaseDeps, error)) int {
	launch := time.Now()
	acquire := len(args) > 0 && args[0] == "acquire"
	asJSON := acquire
	for _, a := range args {
		if a == "--json" || a == "-json" {
			asJSON = true
		}
	}
	failWith := func(err error) int {
		printLease(stdout, tools.LeaseReleaseFailedAnswer(err), asJSON)
		return tools.LeaseReleaseExitFailed
	}
	// A usage refusal carries a code and its class as every failure does.
	fail := func(msg string) int {
		return failWith(platform.NewPlatformError(platform.ErrInvalidUsage, msg, ""))
	}
	if len(args) > 0 && args[0] == "warm-clone" {
		return runLeaseWarmClone(args[1:], stdout, resolve)
	}
	if len(args) == 0 || (!acquire && args[0] != "release") {
		return fail(leaseUsage)
	}
	usage := leaseReleaseUsage
	budget := leaseCallBudget
	if acquire {
		usage = leaseAcquireUsage
		budget = leaseAcquireBudget
	}
	for _, a := range args[1:] {
		if name := strings.TrimLeft(strings.SplitN(a, "=", 2)[0], "-"); strings.HasPrefix(a, "-") && name == "force" && !acquire {
			return fail("zcp lease release never forces: a lease holding unsaved work keeps its branch, and only the agent's zerops_lease action=release force=true, once the person agreed, discards it. " + usage)
		}
	}
	fs := flag.NewFlagSet("lease "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	branch := fs.String("branch", "", "the branch to acquire or release")
	var pool, leaseID string
	if acquire {
		fs.StringVar(&pool, "pool", "", "the pool service hostname")
	} else {
		fs.StringVar(&leaseID, "lease-id", "", "the id returned by the writer’s acquire")
	}
	repo := fs.String("repo", "", "the repository's clone URL, when zcp leases the branch for several")
	fs.Bool("json", false, "answer as JSON")
	if err := fs.Parse(args[1:]); err != nil {
		if acquire {
			return fail(usage)
		}
		return fail(err.Error() + ". " + usage)
	}
	if acquire && (fs.NArg() > 0 || strings.TrimSpace(pool) == "" || strings.TrimSpace(*branch) == "") {
		return fail(usage)
	}
	if fs.NArg() > 0 {
		return fail("unexpected argument " + fs.Arg(0) + ". " + usage)
	}
	provided := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { provided[f.Name] = true })
	if provided["lease-id"] {
		if strings.TrimSpace(leaseID) == "" || provided["branch"] || provided["repo"] {
			return fail("--lease-id requires a nonempty id and cannot be combined with --branch or --repo. " + usage)
		}
	} else if strings.TrimSpace(*branch) == "" {
		return fail("--branch names the branch whose leases to release. " + usage)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithDeadline(ctx, launch.Add(budget))
	defer cancel()
	deps, err := resolve(ctx)
	if errors.Is(err, errLeaseOutsideContainer) {
		err = fmt.Errorf("zcp lease %s %w", args[0], err)
	}
	if err != nil {
		return failWith(err)
	}
	var background sync.WaitGroup
	if deps.Warm != nil {
		deps.Warm.Go = background.Go
	}
	var code int
	if acquire {
		var answer tools.LeaseAcquireAnswer
		answer, code = tools.AcquireLeaseAnswer(ctx, deps, pool, *repo, *branch, launch.Add(leaseReservationStart))
		printLeaseJSON(stdout, answer)
	} else {
		var answer tools.LeaseReleaseAnswer
		if leaseID != "" {
			answer, code = tools.ReleaseLeaseIDAnswer(ctx, deps, leaseID)
		} else {
			answer, code = tools.ReleaseLeaseBranchAnswer(ctx, deps, *repo, *branch)
		}
		printLease(stdout, answer, asJSON)
	}
	waitWithin(&background, leaseBackgroundWait)
	return code
}

// runLeaseWarmClone is `zcp lease warm-clone`: the process that holds the
// relayed session of one warm clone of a repository on the Mate's HQ until
// the clone ends (ops.LeaseWarmCloneHeld, DP-32). Its outcome is the pool's
// warm clone record; stdout, which its starter discards, gets only an error.
func runLeaseWarmClone(args []string, stdout io.Writer, resolve func(context.Context) (ops.LeaseDeps, error)) int {
	fs := flag.NewFlagSet("lease warm-clone", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pool := fs.String("pool", "", "the pool service hostname")
	container := fs.String("container", "", "the warm container's id")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 || strings.TrimSpace(*pool) == "" || strings.TrimSpace(*container) == "" {
		_, _ = fmt.Fprintln(stdout, leaseWarmCloneUsage)
		return tools.LeaseReleaseExitFailed
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	deps, err := resolve(ctx)
	if err == nil {
		err = ops.LeaseWarmCloneHeld(ctx, deps, *pool, *container)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "lease warm-clone: "+err.Error())
		return tools.LeaseReleaseExitFailed
	}
	return 0
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
		printLeaseJSON(stdout, answer)
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

func printLeaseJSON(stdout io.Writer, answer any) {
	data, err := json.Marshal(answer)
	if err != nil {
		data = []byte(`{"error":"marshal the answer"}`)
	}
	_, _ = fmt.Fprintln(stdout, string(data))
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
		return ops.LeaseDeps{}, errLeaseOutsideContainer
	}
	key := lookup(hq.EnvKey)
	if key == "" {
		return ops.LeaseDeps{}, fmt.Errorf("no %s in this container's environment", hq.EnvKey)
	}
	client, err := platform.NewProjectClient(ctx, key, mate.ResolveAPIHost(lookup("ZCP_API_HOST")), rt.ProjectID)
	if err != nil {
		return ops.LeaseDeps{}, fmt.Errorf("build the platform client: %w", err)
	}
	stateDir := runtime.ContainerStateDir
	swarm := ops.NewSwarmFactory(&http.Client{Timeout: ops.LeaseCallTimeout})
	return tools.LeaseDepsFor(client, rt.ProjectID, stateDir, swarm, platform.NewSystemMounter(), platform.NewSystemSSHDeployer(), rt), nil
}
