package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// `zcp git <host> <git args…>` runs one git command inside the dev service
// host, over ssh, in that service's checkout (ops.RunBrokeredGit): git's
// stdout and stderr pass through byte for byte, stdin too (`commit -F -`), and
// the exit code is git's. Ctrl-C or SIGTERM stop the call; there is no
// deadline of its own, since a push of a large repository takes as long as it
// takes. It is a passthrough of git's own streams, so it writes nothing to
// stdout itself and runs before CLI telemetry.

const gitUsage = "usage: zcp git <host> [-c <key>=<value>]... [-C <dir under /tmp/>] <git subcommand> [<args>...]"

// Exit codes of `zcp git` that are not git's own.
const (
	// gitExitRefused: the call was refused before anything ran.
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
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
}

// isGit recognizes `zcp git` before CLI telemetry can add output to git's
// streams or delay its exit.
func isGit(args []string) bool {
	return len(args) >= 1 && args[0] == "git"
}

// runGit is `zcp git` in this container: the runtime read from the live env
// store (the process's own environment may be a unit's), zcp's records from
// their fixed directory, and the call stopped by Ctrl-C or SIGTERM.
func runGit(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runGitCmd(ctx, args, gitCmdDeps{
		rt:       runtime.DetectFrom(mate.LiveLookup(mate.LiveEnvStorePath)),
		stateDir: brokeredGitStateDir(),
		ssh:      platform.NewSystemSSHDeployer(),
		stdin:    os.Stdin,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
	})
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
// mount); one they do not name — a lease alias, an unadopted service — in
// /var/www.
func runGitCmd(ctx context.Context, args []string, d gitCmdDeps) int {
	refuse := func(msg string) int {
		fmt.Fprintln(d.stderr, "zcp git: "+msg)
		return gitExitRefused
	}
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
	meta, err := workflow.FindServiceMeta(d.stateDir, host)
	if err != nil {
		fmt.Fprintf(d.stderr, "zcp git: read zcp's record of %s: %v\n", host, err)
		return gitExitFailed
	}
	dir := topology.CodeMountRoot
	if meta != nil {
		if meta.DevIsPool() {
			return refuse(fmt.Sprintf("%q belongs to the pair keyed by the dev pool %q, which holds no checkout of its own (its hostname reaches a random one of its containers); check the branch out in a lease, zerops_lease action=\"acquire\" pool=%q, and run git in the lease it names: zcp git %s-<n> ...", host, meta.Hostname, meta.Hostname, meta.Hostname))
		}
		if _, isStage := meta.StageFor(host); isStage {
			return refuse(fmt.Sprintf("%q is the stage of %s and holds no checkout of the pair's repository; run git in its dev service: zcp git %s ...", host, meta.Hostname, meta.Hostname))
		}
		if wd := meta.WorkingDirFor(host); wd != "" {
			dir = wd
		}
	}
	code, err := ops.RunBrokeredGit(ctx, d.ssh, host, dir, gitArgs, d.stdin, d.stdout, d.stderr)
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
