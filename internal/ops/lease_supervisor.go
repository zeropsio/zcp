package ops

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// A lease always has its supervisor (docs/spec-workflows.md §8 DP-12). A pool
// container released with reset=true comes back as its image and Zerops never
// runs its run.start again, and zcp releases every lease with a reset (DP-4),
// so a reused container runs no zcp-devsup unless zcp starts it: after an
// acquire's checkout, and once when a dev-server call on a lease finds it
// installed but not answering. The start is run.start by hand — `zcp-devsup
// run` in the lease's work dir, as the ssh session's user with the session's
// env (the pool's service env and ZEROPS_StableNumber, DP-6) — detached so it
// outlives the ssh call. A dev service is never started this way: its
// supervisor is its dev setup's run.start (docs/spec-mate.md §2.8a.2).

// leaseSupervisorWait is how many seconds a started supervisor gets for its
// control socket to answer; binding it takes milliseconds.
const leaseSupervisorWait = 10

// leaseSupervisorTimeout bounds one ensure call: the lock wait (60 s), the
// check and the last wait's `ctl status` (each may wait out ctl's own 60 s
// exchange bound, for a supervisor that holds the socket while it boots its
// servers) and the wait itself.
const leaseSupervisorTimeout = 4 * time.Minute

// leaseSupervisorOutcome is what ensuring a lease's supervisor found or did.
type leaseSupervisorOutcome string

const (
	// leaseSupervisorRunning: one answered; nothing was started.
	leaseSupervisorRunning leaseSupervisorOutcome = "running"
	// leaseSupervisorStarted: none answered; zcp started one and it answers.
	leaseSupervisorStarted leaseSupervisorOutcome = "started"
	// leaseSupervisorAbsent: the pool's image carries no zcp-devsup; nothing
	// was started (the pool is not a dev pool).
	leaseSupervisorAbsent leaseSupervisorOutcome = "absent"
	// leaseSupervisorUnresponsive: a supervisor holds the control socket but
	// did not answer in time; a second is never started beside it.
	leaseSupervisorUnresponsive leaseSupervisorOutcome = "unresponsive"
	// leaseSupervisorFailed: zcp started one and its socket never answered.
	leaseSupervisorFailed leaseSupervisorOutcome = "failed"
)

// leaseSupervisorResult is one ensure.
type leaseSupervisorResult struct {
	outcome leaseSupervisorOutcome
	// log is the tail of a failed supervisor's own output.
	log string
}

// leaseSupervisorLog is where a supervisor zcp starts writes its own output,
// beside its state in the zerops user's home ($HOME/.zcp/devsup), as the
// agent addresses it; the reset at release takes it away.
const leaseSupervisorLog = "~/.zcp/devsup/supervisor.log"

// leaseSupervisorCommand starts `zcp-devsup run` in workDir unless a
// supervisor already holds the control socket, and waits up to waitSeconds
// for the one it started to answer. It answers in devsup sections: "check"
// carries `ctl status`'s exit code before anything is started (127 without
// the binary); "started" the code after the start; "log" the tail of the
// started supervisor's output when it does not answer.
//
// Only `ctl status` exit 2 (no supervisor: the socket is absent or refused)
// starts one: 0 answers, and 5 holds the socket unanswering, so a second would
// fight it for the socket and the servers' ports. Where flock exists the check
// and the start run under a lock in the state dir, so concurrent calls start
// one. The supervisor runs in its own session (setsid; nohup where there is
// none), stdin from /dev/null, its output appended to leaseSupervisorLog and
// the lock's descriptor closed, so it holds nothing of the ssh session and
// outlives it. workDir is shell-quoted; nothing else is composed in.
func leaseSupervisorCommand(workDir string, waitSeconds int) string {
	status := `zcp-devsup ctl status --json >/dev/null 2>&1`
	run := `zcp-devsup run </dev/null >>"$d/supervisor.log" 2>&1 9>&-`
	return strings.Join([]string{
		`if ! command -v zcp-devsup >/dev/null 2>&1; then echo '@@zcp-devsup check 127'; exit 0; fi`,
		`d="$HOME/.zcp/devsup"`,
		`mkdir -p "$d" || exit 1`,
		`if command -v flock >/dev/null 2>&1; then exec 9>>"$d/ensure.lock"; flock -w 60 9 || :; fi`,
		status + `; c=$?`,
		`echo "@@zcp-devsup check $c"`,
		`if [ "$c" -ne 2 ]; then exit 0; fi`,
		`cd ` + shellQuote(workDir) + ` || exit 1`,
		`if command -v setsid >/dev/null 2>&1; then setsid ` + run + ` & else nohup ` + run + ` & fi`,
		`end=$(( $(date +%s) + ` + strconv.Itoa(waitSeconds) + ` ))`,
		`while :; do ` + status + `; c=$?; if [ "$c" -ne 2 ] || [ "$(date +%s)" -ge "$end" ]; then break; fi; sleep 0.2 2>/dev/null || sleep 1; done`,
		`echo "@@zcp-devsup started $c"`,
		`if [ "$c" -ne 0 ]; then tail -n 20 "$d/supervisor.log" 2>/dev/null; echo '@@zcp-devsup log 0'; fi`,
	}, "\n")
}

// ensureLeaseSupervisor makes sure zcp-devsup runs in the leased container
// behind alias.
func ensureLeaseSupervisor(ctx context.Context, ssh SSHDeployer, alias string) (leaseSupervisorResult, error) {
	return ensureSupervisorIn(ctx, ssh, alias, leaseWorkDir, leaseSupervisorWait)
}

// ensureSupervisorIn is ensureLeaseSupervisor with the work dir and the wait
// given.
func ensureSupervisorIn(ctx context.Context, ssh SSHDeployer, alias, workDir string, waitSeconds int) (leaseSupervisorResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, leaseSupervisorTimeout)
	defer cancel()
	out, err := ssh.ExecSSH(callCtx, alias, leaseSupervisorCommand(workDir, waitSeconds))
	if err != nil {
		return leaseSupervisorResult{}, fmt.Errorf("start zcp-devsup on %s: %w", alias, err)
	}
	sections, err := splitDevsupSections(string(out))
	if err != nil {
		return leaseSupervisorResult{}, fmt.Errorf("start zcp-devsup on %s: %w", alias, err)
	}
	check, ok := sections["check"]
	if !ok {
		return leaseSupervisorResult{}, fmt.Errorf("start zcp-devsup on %s: unreadable answer %q", alias, strings.TrimSpace(string(out)))
	}
	switch check.code {
	case devsupCodeAbsent:
		return leaseSupervisorResult{outcome: leaseSupervisorAbsent}, nil
	case devsupCodeOK:
		return leaseSupervisorResult{outcome: leaseSupervisorRunning}, nil
	case devsupCodeUnresponsive:
		return leaseSupervisorResult{outcome: leaseSupervisorUnresponsive}, nil
	case devsupCodeNoSupervisor:
	default:
		return leaseSupervisorResult{}, fmt.Errorf("start zcp-devsup on %s: zcp-devsup ctl status exited %d", alias, check.code)
	}
	started, ok := sections["started"]
	if !ok {
		return leaseSupervisorResult{}, fmt.Errorf("start zcp-devsup on %s: unreadable answer %q", alias, strings.TrimSpace(string(out)))
	}
	switch started.code {
	case devsupCodeOK:
		return leaseSupervisorResult{outcome: leaseSupervisorStarted}, nil
	case devsupCodeUnresponsive:
		return leaseSupervisorResult{outcome: leaseSupervisorUnresponsive}, nil
	}
	return leaseSupervisorResult{outcome: leaseSupervisorFailed, log: sections["log"].body}, nil
}

// leaseSupervisorNote is what an acquire says about the lease's supervisor:
// nothing when one answers. A supervisor that is missing or will not start
// never fails the acquire; the lease holds code either way.
func leaseSupervisorNote(r leaseSupervisorResult, err error) string {
	if err != nil {
		return fmt.Sprintf("zcp could not make sure the dev-server supervisor runs in the container (%s); zerops_dev_server starts it on its next call", sshFailureText(err))
	}
	switch r.outcome {
	case leaseSupervisorRunning, leaseSupervisorStarted:
		return ""
	case leaseSupervisorAbsent:
		return leaseNoDevsupSentence
	case leaseSupervisorUnresponsive:
		return "the dev-server supervisor (zcp-devsup) runs in the container but did not answer in time — it may be out of memory or CPU; zerops_dev_server reads it again"
	case leaseSupervisorFailed:
		return "zcp started the dev-server supervisor (zcp-devsup) in the container but it did not answer; zerops_dev_server starts it again on its next call" + supervisorOutputClause(r.log)
	}
	return ""
}

// leaseNoDevsupSentence names a pool whose image carries no zcp-devsup.
const leaseNoDevsupSentence = "the pool's image carries no zcp-devsup, so this lease serves no dev servers and no preview — the pool is not a dev pool (zerops_dev_server refuses its leases)"

// leaseNoSupervisorDetail is a read of a lease whose supervisor does not
// answer, for a caller that starts nothing (verify): a lease's supervisor is
// zcp's to start, never a matter of the pool's run.start.
func leaseNoSupervisorDetail(alias string, installed bool) string {
	if !installed {
		return leaseNoDevsupSentence
	}
	return fmt.Sprintf("no zcp-devsup answers on %s (nothing started it since the container last started); zerops_dev_server action=status hostname=%q starts it", alias, alias)
}

// supervisorOutputClause quotes a failed supervisor's own output, "" when it
// wrote none.
func supervisorOutputClause(log string) string {
	if log = strings.TrimSpace(log); log == "" {
		return ""
	}
	return ". Its output: " + boundedText(log)
}

// sshFailureText is err with the remote output an *SSHExecError keeps apart
// (its Error() drops it), bounded.
func sshFailureText(err error) string {
	msg := err.Error()
	var sshErr *platform.SSHExecError
	if errors.As(err, &sshErr) {
		if out := strings.TrimSpace(sshErr.Output); out != "" {
			msg += ": " + boundedText(out)
		}
	}
	return msg
}

// boundedText trims s to what a note carries.
func boundedText(s string) string {
	const limit = 600
	s = strings.TrimSpace(s)
	if len(s) > limit {
		return s[:limit] + "…"
	}
	return s
}

// recoverLeaseSupervisor starts the supervisor of a lease whose container has
// zcp-devsup installed but runs none — a reused container restarted since its
// acquire, or a supervisor that died — once, and reads the dev servers again.
// Any other read stands. why says, for a supervisor that still does not
// answer, what zcp tried and what it saw.
func recoverLeaseSupervisor(ctx context.Context, ssh SSHDeployer, alias, root string, ds *DevServers, readErr error) (*DevServers, string, error) {
	var noSup *NoSupervisorError
	if !errors.As(readErr, &noSup) || !noSup.Installed {
		return ds, "", readErr
	}
	r, err := ensureLeaseSupervisor(ctx, ssh, alias)
	switch {
	case err != nil:
		return ds, "zcp could not start it: " + sshFailureText(err), readErr
	case r.outcome == leaseSupervisorAbsent:
		return ds, "", &NoSupervisorError{Hostname: alias}
	case r.outcome == leaseSupervisorFailed:
		return ds, "zcp started zcp-devsup there but it did not answer" + supervisorOutputClause(r.log), readErr
	}
	// Started, or one answers or holds the socket now: read it as it is.
	ds, err = ReadDevServers(ctx, ssh, alias, root)
	return ds, "", err
}
