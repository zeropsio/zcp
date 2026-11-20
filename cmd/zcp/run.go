package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// `zcp run <host> <command> [<args>…]` runs one command inside the service
// host, over ssh (ops.RunInService): where the service's code, runtime and
// dependencies are, not in the zcp container over its sshfs mount of them.
// It runs in the folder of the service the caller stands in on the mount
// (ops.ServiceRunDir), else in the service's checkout, found as `zcp git`
// finds it (gitTargetOf: the same refusals, the same lease check). Each
// argument reaches the command as data; a shell line goes in as
// `sh -c '<line>'`. stdin, stdout and stderr are the command's, and so is the
// exit code; zcp's own are `zcp git`'s (gitExitRefused and the others).
// Ctrl-C or SIGTERM stop the call, not the command: a time limit belongs on
// the command itself, under the caller's own limit (`zcp run <host>
// timeout 540 …` under a 600 s one). No credential is
// relayed: git against the Mate's HQ stays `zcp git`'s. Like `zcp git` it
// writes nothing to stdout itself and runs before CLI telemetry. A reader of
// its output that goes away (`zcp run … | head -1`) ends the call quietly
// with runExitReaderGone, as it ends any command writing into a pipe.

const runUsage = "usage: zcp run <host> [--] <command> [<args>...]"

// runVerbName is the subcommand.
const runVerbName = "run"

// runExitReaderGone: the reader of the call's stdout or stderr went away —
// the exit status of a command a shell says SIGPIPE ended.
const runExitReaderGone = 128 + int(syscall.SIGPIPE)

// readerGone passes the command's output on and, at the first write that
// fails, stops the call: left alone, ssh fails on the same closed pipe with
// its own 255, which zcp would report as ssh not reaching the service.
type readerGone struct {
	w    io.Writer
	stop context.CancelFunc
	gone *atomic.Bool
}

func (r readerGone) Write(p []byte) (int, error) {
	n, err := r.w.Write(p)
	if err != nil {
		r.gone.Store(true)
		r.stop()
	}
	return n, err
}

// runHostVerb is `zcp run`'s.
func runHostVerb() hostVerb {
	return hostVerb{name: runVerbName, usage: runUsage, elsewhere: "zcp run runs a command in another service", what: "the command"}
}

// isRun recognizes `zcp run` before CLI telemetry can add output to the
// command's streams or delay its exit.
func isRun(args []string) bool {
	return len(args) >= 1 && args[0] == runVerbName
}

// runRun is `zcp run` in this container: the runtime read from the live env
// store, zcp's records from their fixed directory, the folder the caller
// stands in, and the call stopped by Ctrl-C or SIGTERM.
func runRun(args []string) int {
	handleSIGPIPE()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// A folder that cannot be read names no folder of a mount: the checkout.
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return runRunCmd(ctx, args, cwd, gitCmdDeps{
		verb:      runHostVerb(),
		rt:        runtime.DetectFrom(mate.LiveLookup(mate.LiveEnvStorePath)),
		stateDir:  brokeredGitStateDir(),
		ssh:       platform.NewSystemSSHDeployer(),
		leaseDeps: resolveInitLeaseDeps,
		stdin:     os.Stdin,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
	})
}

// runRunCmd runs `zcp run` with args — everything after "run" — from cwd on
// d. A call is refused, with exit 2 and one line saying why, before any ssh
// call for what `zcp git` refuses a host for (gitHostRefusal, gitTargetOf),
// for no command, and outside a container.
func runRunCmd(ctx context.Context, args []string, cwd string, d gitCmdDeps) int {
	refuse := func(msg string) int { return gitRefuse(d, msg) }
	if len(args) == 0 {
		return refuse("no host given; " + runUsage)
	}
	host := args[0]
	if why := gitHostRefusal(host, d.hostVerb()); why != "" {
		return refuse(why)
	}
	argv := args[1:]
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return refuse("no command given; " + runUsage)
	}
	if !d.rt.InContainer {
		return refuse("runs only in a Zerops container, the zcp service, which reaches " + host + " over the project's ssh")
	}
	if host == d.rt.ServiceName {
		return refuse(fmt.Sprintf("%q is this container itself; %s", host, d.hostVerb().elsewhere))
	}
	target, stop, stopped := gitTargetOf(ctx, host, d)
	if stopped {
		return stop
	}
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	var gone atomic.Bool
	stdout := readerGone{w: d.stdout, stop: stopRun, gone: &gone}
	stderr := readerGone{w: d.stderr, stop: stopRun, gone: &gone}
	code, err := ops.RunInService(runCtx, d.ssh, host, ops.ServiceRunDir(host, target.dir, cwd), argv, d.stdin, stdout, stderr)
	if gone.Load() {
		return runExitReaderGone
	}
	if err == nil {
		return code
	}
	if ctx.Err() != nil {
		fmt.Fprintf(d.stderr, "zcp run: stopped; the command may still be running in %s\n", host)
		return gitExitStopped
	}
	fmt.Fprintf(d.stderr, "zcp run: %v\n", err)
	return gitExitSSHFailed
}
