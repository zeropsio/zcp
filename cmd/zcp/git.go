package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// `zcp git <host> <git args…>` runs one git command inside the dev service
// host, over ssh, in that service's checkout (ops.RunBrokeredGit): git's
// stdout and stderr pass through byte for byte, stdin too when the argv makes
// git read it (`commit -F -`, ops.BrokeredGitReadsStdin), and the exit code
// is git's. Ctrl-C or SIGTERM stop the call; there is no
// deadline of its own, since a push of a large repository takes as long as it
// takes. It is a passthrough of git's own streams, so it writes nothing to
// stdout itself and runs before CLI telemetry.

const gitUsage = "usage: zcp git <host> [-c <key>=<value>]... [-C <dir under /tmp/>] <git subcommand> [<args>...]"

// Exit codes of `zcp git` that are not git's own.
const (
	// gitExitRefused: the call was refused before anything ran — by its
	// arguments, zcp's records, or, for a lease alias, the lease check
	// (docs/spec-workflows.md §8 DP-2): no lease record, a lease that is
	// lost, one the orchestrator did not answer about or this zcp cannot ask,
	// and a parked one. Each of those is the check's answer, not a failure to
	// read zcp's own records, and nothing reached the container — a refusal.
	gitExitRefused = 2
	// gitExitFailed: zcp's record of the host could not be read.
	gitExitFailed = 1
	// gitExitStopped: Ctrl-C or SIGTERM stopped the call.
	gitExitStopped = 130
	// gitExitSSHFailed: ssh failed, so git gave no answer.
	gitExitSSHFailed = 255
)

// gitCmdDeps is what `zcp git` runs on: the container it runs in, the
// directory of zcp's records, the ssh seam and the caller's streams.
type gitCmdDeps struct {
	rt       runtime.Info
	stateDir string
	ssh      ops.SSHStdioRunner
	// hqIdentity reads the Mate's HQ identity — only for a relayed call,
	// which answers from it and hqHTTP, the client that reaches HQ. nil
	// reads none.
	hqIdentity func() hq.Identity
	hqHTTP     hq.Doer
	// leaseDeps builds what the lease check of a lease alias asks the pool's
	// orchestrator with (resolveInitLeaseDeps), only once the alias has a
	// lease record; nil builds no orchestrator, so every lease is refused as
	// one this zcp cannot ask about.
	leaseDeps func(context.Context) (ops.LeaseDeps, error)
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
}

// isGit recognizes `zcp git` before CLI telemetry can add output to git's
// streams or delay its exit.
func isGit(args []string) bool {
	return len(args) >= 1 && args[0] == "git"
}

// runGit is `zcp git` in this container: the runtime read from the live env
// store (the process's own environment may be a unit's), zcp's records from
// their fixed directory, SIGPIPE handled by handleSIGPIPE, and the call
// stopped by Ctrl-C or SIGTERM.
func runGit(args []string) int {
	handleSIGPIPE()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	lookup := mate.LiveLookup(mate.LiveEnvStorePath)
	return runGitCmd(ctx, args, gitCmdDeps{
		rt:         runtime.DetectFrom(lookup),
		stateDir:   brokeredGitStateDir(),
		ssh:        platform.NewSystemSSHDeployer(),
		hqIdentity: func() hq.Identity { return mate.HQIdentity(lookup) },
		hqHTTP:     &http.Client{},
		leaseDeps:  resolveInitLeaseDeps,
		stdin:      os.Stdin,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
	})
}

// handleSIGPIPE catches SIGPIPE, into a channel nobody reads: a relayed call
// writes git's stdout itself, and once SIGPIPE is caught a write to a stdout
// whose reader went away (`zcp git … | head -1`) fails with EPIPE — the copy
// ends, closes its pipe and the call revokes its tokens — instead of killing
// zcp. Caught, not ignored: an ignored disposition survives exec, a caught
// one is reset to the default in every child, so ssh still dies of SIGPIPE
// when it writes to a gone reader — and the remote git with its session —
// rather than keeping the session open and discarding what git goes on
// producing (on a partial clone, fetching every blob lazily).
func handleSIGPIPE() {
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
}

// brokeredGitStateDir is where zcp keeps its records in a zcp container —
// fixed, never the caller's working directory, which `zcp git` may be run
// from anywhere.
func brokeredGitStateDir() string {
	return filepath.Join(topology.CodeMountRoot, ".zcp", "state")
}

// runGitCmd runs `zcp git` with args — everything after "git" — on d. A call
// is refused, with exit 2 and one line saying why, before any ssh call when
// the host is no other service of the project, the git arguments are not
// accepted (ops.ParseBrokeredGitArgs), it runs outside a container, or zcp's
// records say the host holds no checkout of its own: a stage (its dev's
// repository is the pair's) or either hostname of a pair keyed by its dev
// pool (a lease holds the checkout). A host the records name runs git in the
// directory its deploys run in (a variant-B dev service: the code volume's
// mount); a service they do not name — an unadopted one — in /var/www. A
// lease alias is asked about first (gitLeaseTarget).
//
// In a Mate container a call to an HQ-wired pair — or to a lease of a pool
// pair wired to HQ — is relayed (ops.RunBrokeredGitRelayed): zcp answers
// git's credential requests over the ssh session from the Mate's identity,
// the record's HQ origin and the call's argv (brokeredGitRelayFor). Such a
// call is also refused, with exit 2, for git's own credential subcommands, a
// user -c that decides credentials or transport (ops.CheckRelayedGitArgs),
// and a stdin over the 1 MiB it forwards.
func runGitCmd(ctx context.Context, args []string, d gitCmdDeps) int {
	refuse := func(msg string) int { return gitRefuse(d, msg) }
	if len(args) == 0 {
		return refuse("no host given; " + gitUsage)
	}
	host := args[0]
	if why := gitHostRefusal(host); why != "" {
		return refuse(why)
	}
	gitArgs, err := ops.ParseBrokeredGitArgs(args[1:])
	if err != nil {
		return refuse(err.Error() + "; " + gitUsage)
	}
	if !d.rt.InContainer {
		return refuse("runs only in a Zerops container, the zcp service, which reaches " + host + " over the project's ssh")
	}
	if host == d.rt.ServiceName {
		return refuse(fmt.Sprintf("%q is this container itself; zcp git runs git in another service's checkout", host))
	}
	target, stop, stopped := gitTargetOf(ctx, host, d)
	if stopped {
		return stop
	}
	meta, dir := target.meta, target.dir
	var code int
	if isRelayedGit(d.rt, meta) {
		if err := ops.CheckRelayedGitArgs(gitArgs); err != nil {
			return refuse(err.Error())
		}
		var id hq.Identity
		if d.hqIdentity != nil {
			id = d.hqIdentity()
		}
		relay := brokeredGitRelayFor(meta, id, d.hqHTTP, gitArgs)
		code, err = ops.RunBrokeredGitRelayed(ctx, d.ssh, host, dir, gitArgs, relay, d.stdin, d.stdout, d.stderr)
		switch {
		case errors.Is(err, ops.ErrBrokeredGitStdinOverCap):
			return refuse(err.Error())
		case errors.Is(err, ops.ErrBrokeredGitStdinUnread), errors.Is(err, ops.ErrBrokeredGitSetup):
			fmt.Fprintf(d.stderr, "zcp git: %v\n", err)
			return gitExitFailed
		case errors.Is(err, ops.ErrBrokeredGitStdinStopped):
			fmt.Fprintf(d.stderr, "zcp git: stopped while reading stdin; nothing ran in %s\n", host)
			return gitExitStopped
		}
	} else {
		code, err = ops.RunBrokeredGit(ctx, d.ssh, host, dir, gitArgs, d.stdin, d.stdout, d.stderr)
	}
	if err == nil {
		return code
	}
	if ctx.Err() != nil {
		fmt.Fprintf(d.stderr, "zcp git: stopped; git may still be running in %s, and any lock it holds there clears by itself when it finishes\n", host)
		return gitExitStopped
	}
	fmt.Fprintf(d.stderr, "zcp git: %v\n", err)
	return gitExitSSHFailed
}

// gitRefuse says why a call is refused, in one line, and is its exit code.
func gitRefuse(d gitCmdDeps, msg string) int {
	fmt.Fprintln(d.stderr, "zcp git: "+msg)
	return gitExitRefused
}

// gitRecordUnread says zcp could not read its record of host, and is the
// exit code.
func gitRecordUnread(d gitCmdDeps, host string, err error) int {
	fmt.Fprintf(d.stderr, "zcp git: read zcp's record of %s: %v\n", host, err)
	return gitExitFailed
}

// gitTarget is where a call runs git — the directory, in the container ssh
// reaches by the call's host — and the record a relayed call answers from,
// nil for none.
type gitTarget struct {
	dir  string
	meta *workflow.ServiceMeta
}

// gitTargetOf is the target of a call to host, or — stopped true — the exit
// code of a call that ends here, its line said: refused (a pool pair, a
// stage, a lease gitLeaseTarget refuses) or a record zcp could not read.
func gitTargetOf(ctx context.Context, host string, d gitCmdDeps) (target gitTarget, code int, stopped bool) {
	if platform.ValidateLeaseAlias(host) == nil {
		return gitLeaseTarget(ctx, host, d)
	}
	meta, err := workflow.FindServiceMeta(d.stateDir, host)
	if err != nil {
		return gitTarget{}, gitRecordUnread(d, host, err), true
	}
	target = gitTarget{dir: topology.CodeMountRoot, meta: meta}
	if meta == nil {
		return target, 0, false
	}
	if meta.DevIsPool() {
		return gitTarget{}, gitRefuse(d, fmt.Sprintf("%q belongs to the pair keyed by the dev pool %q, which holds no checkout of its own (its hostname reaches a random one of its containers); check the branch out in a lease, zerops_lease action=\"acquire\" pool=%q, and run git in the lease it names: zcp git %s-<n> ...", host, meta.Hostname, meta.Hostname, meta.Hostname)), true
	}
	if _, isStage := meta.StageFor(host); isStage {
		return gitTarget{}, gitRefuse(d, fmt.Sprintf("%q is the stage of %s and holds no checkout of the pair's repository; run git in its dev service: zcp git %s ...", host, meta.Hostname, meta.Hostname)), true
	}
	if wd := meta.WorkingDirFor(host); wd != "" {
		target.dir = wd
	}
	return target, 0, false
}

// gitLeaseWhat is what a refused lease check says zcp git would have done.
const gitLeaseWhat = "reached by zcp git"

// gitLeaseTarget is the target of a call to the lease alias host. Only zcp's
// own lease records make an alias reachable, so one zcp holds no record of
// is refused, nobody asked. Every other is asked about before anything
// reaches its container (docs/spec-workflows.md §8 DP-2): through
// initLeaseCheck — ops.LeaseTargetChecked over the API client and
// orchestrators zcp init's lease restore builds — one renew of the record's
// lease. A lost lease is refused naming the release that drops its record,
// an unverified one — the orchestrator did not answer, or this zcp cannot ask
// it — naming why and the status that asks again, and a parked one (DP-28),
// which nothing answers on, naming the unpark: each a refusal, exit 2. A
// lease the orchestrator gives zcp runs git in its checkout,
// ops.LeaseWorkDir; a relayed call to it answers from the record of the pair
// keyed by its pool.
func gitLeaseTarget(ctx context.Context, host string, d gitCmdDeps) (target gitTarget, code int, stopped bool) {
	noLease := fmt.Sprintf("%q is no lease this zcp holds: only zcp's own lease records make an alias reachable; zerops_lease action=status lists the leases it holds, and zerops_lease action=\"acquire\" takes one", host)
	if _, held, err := ops.LeaseTarget(d.stateDir, host); err != nil {
		return gitTarget{}, gitRecordUnread(d, host, err), true
	} else if !held {
		return gitTarget{}, gitRefuse(d, noLease), true
	}
	resolve := d.leaseDeps
	if resolve == nil {
		resolve = func(context.Context) (ops.LeaseDeps, error) { return ops.LeaseDeps{}, nil }
	}
	lease, held, err := initLeaseCheck(resolve)(ctx, d.stateDir, host, gitLeaseWhat)
	var refusal *platform.PlatformError
	switch {
	case errors.As(err, &refusal):
		return gitTarget{}, gitRefuse(d, refusalLine(err)), true
	case err != nil:
		return gitTarget{}, gitRecordUnread(d, host, err), true
	case !held:
		return gitTarget{}, gitRefuse(d, noLease), true
	case lease.Parked:
		return gitTarget{}, gitRefuse(d, refusalLine(ops.LeaseParkedError(lease, "zcp git"))), true
	}
	meta, err := workflow.FindServiceMeta(d.stateDir, lease.Pool)
	if err != nil {
		return gitTarget{}, gitRecordUnread(d, lease.Pool, err), true
	}
	if !meta.DevIsPool() || meta.Hostname != lease.Pool {
		meta = nil
	}
	return gitTarget{dir: ops.LeaseWorkDir, meta: meta}, 0, false
}

// refusalLine is a refusal's message and, for a platform refusal, what to do
// about it, on one line.
func refusalLine(err error) string {
	line := err.Error()
	var pe *platform.PlatformError
	if errors.As(err, &pe) && pe.Suggestion != "" {
		line = pe.Message + "; " + pe.Suggestion
	}
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(line)
}

// isRelayedGit reports whether a call answered from meta — the record naming
// the host, or for a lease the pair keyed by its pool — is relayed: in a Mate
// container, every subcommand of a pair wired to an HQ repository — wired
// through or not — since its checkout is a partial clone whose lazy fetches
// ask for a credential on any of them.
func isRelayedGit(rt runtime.Info, meta *workflow.ServiceMeta) bool {
	return rt.MateEnabled && meta != nil && meta.HQ != nil && meta.HQ.Repo != ""
}

// brokeredGitRelayFor is what a relayed call to meta's pair answers from:
// the Mate's identity — HQ's host, any path — the record's HQ origin, which
// zcp writes from HQ's own state, or, while the record names none, HQ's live
// origin of the record's repository; and the URLs args write literally.
// Never the record's RemoteURL, written from the checkout's live origin.
func brokeredGitRelayFor(meta *workflow.ServiceMeta, id hq.Identity, httpClient hq.Doer, args ops.BrokeredGitArgs) ops.BrokeredGitRelay {
	relay := ops.BrokeredGitRelay{Identity: id, HTTPClient: httpClient}
	if origin := meta.HQ.Origin; origin != nil && origin.URL != "" {
		relay.Repos = append(relay.Repos, origin.URL)
	} else {
		relay.LiveOrigin = &ops.BrokeredGitHQRepo{AppID: meta.HQ.AppID, Repo: meta.HQ.Repo}
	}
	relay.Repos = append(relay.Repos, ops.BrokeredGitArgvURLs(args, id.Origin)...)
	return relay
}

// gitHostRefusal says why host is no service `zcp git` may run git in — a
// loopback name or address, or no service hostname or lease alias at all —
// and "" when it may be one. The container's own hostname is refused by the
// caller, which knows it.
func gitHostRefusal(host string) string {
	switch host {
	case "localhost", "localhost4", "localhost6":
		return fmt.Sprintf("%q is this container itself; zcp git runs git in another service's checkout", host)
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return fmt.Sprintf("%q is this container itself; zcp git runs git in another service's checkout", host)
	}
	if platform.ValidateMountName(host) != nil {
		return fmt.Sprintf("%q is no service hostname or lease alias; %s", host, gitUsage)
	}
	return ""
}
