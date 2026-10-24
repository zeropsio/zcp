package ops

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
)

// A lease always has its supervisor (docs/spec-workflows.md §8 DP-13). A pool
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
	// reloadErr is a running supervisor's refusal of the reload an acquire
	// asked for (an invalid declaration, say); envErr its refusal of the
	// overlay the step sent.
	reloadErr, envErr error
}

// leaseSupervisorLog is where a supervisor zcp starts writes its own output,
// beside its state in the zerops user's home ($HOME/.zcp/devsup), as the
// agent addresses it; the reset at release takes it away.
const leaseSupervisorLog = "~/.zcp/devsup/supervisor.log"

// leaseSupervisorCommand starts `zcp-devsup run` in workDir unless a
// supervisor already holds the control socket, and waits up to waitSeconds
// for the one it started to answer; with reload, a supervisor that answers is
// reloaded instead, so it takes the declaration a checkout just put there.
// env, when set, is a `ctl env` argv (lease_wiring.go) sent to a supervisor
// that answers before its reload, and to the one it started once that answers
// — that one booted the declared servers already and restarts them under it.
// It answers in devsup sections: "check" carries `ctl status`'s exit code
// before anything is started (127 without the binary); "env" the overlay's
// code (its JSON refusal above it); "reloaded" the reload's code (likewise);
// "started" the code after the start; "log" the tail of the started
// supervisor's output when it does not answer.
//
// Only `ctl status` exit 2 (no supervisor: the socket is absent or refused)
// starts one: 0 answers, and 5 holds the socket unanswering, so a second would
// fight it for the socket and the servers' ports. Where flock exists the check
// and the start run under a lock in the state dir, so concurrent calls start
// one. The supervisor runs in its own session (setsid; nohup where there is
// none), stdin from /dev/null, its output appended to leaseSupervisorLog and
// the lock's descriptor closed, so it holds nothing of the ssh session and
// outlives it. workDir and every env argument are shell-quoted; nothing else
// is composed in.
func leaseSupervisorCommand(workDir string, waitSeconds int, reload bool, env []string) string {
	status := `zcp-devsup ctl status --json >/dev/null 2>&1`
	run := `zcp-devsup run </dev/null >>"$d/supervisor.log" 2>&1 9>&-`
	lines := []string{
		`if ! command -v zcp-devsup >/dev/null 2>&1; then echo '@@zcp-devsup check 127'; exit 0; fi`,
		`d="$HOME/.zcp/devsup"`,
		`mkdir -p "$d" || exit 1`,
	}
	sendEnv := ""
	if len(env) > 0 {
		quoted := make([]string, 0, len(env))
		for _, a := range env {
			quoted = append(quoted, shellQuote(a))
		}
		lines = append(lines, `send_env() { e=$(zcp-devsup ctl `+strings.Join(quoted, " ")+` --json 2>/dev/null); ec=$?; if [ "$ec" -ne 0 ]; then printf '%s\n' "$e"; fi; echo "@@zcp-devsup env $ec"; }`)
		sendEnv = "send_env; "
	}
	lines = append(lines,
		`if command -v flock >/dev/null 2>&1; then exec 9>>"$d/ensure.lock"; flock -w 60 9 || :; fi`,
		status+`; c=$?`,
		`echo "@@zcp-devsup check $c"`,
	)
	switch {
	case reload:
		lines = append(lines, `if [ "$c" -eq 0 ]; then `+sendEnv+`r=$(zcp-devsup ctl reload --json 2>/dev/null); rc=$?; if [ "$rc" -ne 0 ]; then printf '%s\n' "$r"; fi; echo "@@zcp-devsup reloaded $rc"; fi`)
	case sendEnv != "":
		lines = append(lines, `if [ "$c" -eq 0 ]; then send_env; fi`)
	}
	lines = append(lines,
		`if [ "$c" -ne 2 ]; then exit 0; fi`,
		`cd `+shellQuote(workDir)+` || exit 1`,
		`if command -v setsid >/dev/null 2>&1; then setsid `+run+` & else nohup `+run+` & fi`,
		`end=$(( $(date +%s) + `+strconv.Itoa(waitSeconds)+` ))`,
		`while :; do `+status+`; c=$?; if [ "$c" -ne 2 ] || [ "$(date +%s)" -ge "$end" ]; then break; fi; sleep 0.2 2>/dev/null || sleep 1; done`,
		`echo "@@zcp-devsup started $c"`,
	)
	if sendEnv != "" {
		lines = append(lines, `if [ "$c" -eq 0 ]; then send_env; fi`)
	}
	return strings.Join(append(lines,
		`if [ "$c" -ne 0 ]; then tail -n 20 "$d/supervisor.log" 2>/dev/null; echo '@@zcp-devsup log 0'; fi`,
	), "\n")
}

// ensureLeaseSupervisor makes sure zcp-devsup runs in the leased container
// behind alias, leaving one that answers alone: a dev-server call never
// reloads (with the declaration missing from a checkout a reload would stop
// every server).
func ensureLeaseSupervisor(ctx context.Context, ssh SSHDeployer, alias string) (leaseSupervisorResult, error) {
	return ensureSupervisorIn(ctx, ssh, alias, leaseWorkDir, leaseSupervisorWait, false, nil)
}

// ensureLeaseSupervisorAfterCheckout is ensureLeaseSupervisor for an acquire:
// a supervisor that answers — run.start's in a container the platform just
// created, which booted on an empty /var/www — is reloaded, so the declared
// servers start as they do in a reused container; env (nil: none) is the
// servers' sibling overlay, sent before that reload or right after a start.
func ensureLeaseSupervisorAfterCheckout(ctx context.Context, ssh SSHDeployer, alias string, env []string) (leaseSupervisorResult, error) {
	return ensureSupervisorIn(ctx, ssh, alias, leaseWorkDir, leaseSupervisorWait, true, env)
}

// ensureSupervisorIn is ensureLeaseSupervisor with the work dir, the wait,
// the reload and the overlay given.
func ensureSupervisorIn(ctx context.Context, ssh SSHDeployer, alias, workDir string, waitSeconds int, reload bool, env []string) (leaseSupervisorResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, leaseSupervisorTimeout)
	defer cancel()
	out, err := ssh.ExecSSH(callCtx, alias, leaseSupervisorCommand(workDir, waitSeconds, reload, env))
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
	r, err := ensureOutcome(alias, out, check, sections, reload)
	if a, sent := sections["env"]; err == nil && sent && a.code != devsupCodeOK {
		r.envErr = ctlFailure(a)
	}
	return r, err
}

// ensureOutcome reads what an ensure found or did from its answer's sections.
func ensureOutcome(alias string, out []byte, check devsupAnswer, sections map[string]devsupAnswer, reload bool) (leaseSupervisorResult, error) {
	switch check.code {
	case devsupCodeAbsent:
		return leaseSupervisorResult{outcome: leaseSupervisorAbsent}, nil
	case devsupCodeOK:
		return runningSupervisor(alias, sections, reload)
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

// runningSupervisor reads the answer about a supervisor that answered: with
// reload, the reload's own answer.
func runningSupervisor(alias string, sections map[string]devsupAnswer, reload bool) (leaseSupervisorResult, error) {
	r := leaseSupervisorResult{outcome: leaseSupervisorRunning}
	if !reload {
		return r, nil
	}
	reloaded, ok := sections["reloaded"]
	if !ok {
		return leaseSupervisorResult{}, fmt.Errorf("reload zcp-devsup on %s: no answer", alias)
	}
	if reloaded.code != devsupCodeOK {
		r.reloadErr = ctlFailure(reloaded)
	}
	return r, nil
}

// leaseSupervisorNote is what an acquire says about the lease's supervisor:
// nothing when one answers. A supervisor that is missing or will not start
// never fails the acquire; the lease holds code either way.
func leaseSupervisorNote(alias string, r leaseSupervisorResult, err error) string {
	if err != nil {
		return fmt.Sprintf("zcp could not make sure the dev-server supervisor runs in the container (%s); zerops_dev_server starts it on its next call", sshFailureText(err))
	}
	switch r.outcome {
	case leaseSupervisorRunning, leaseSupervisorStarted:
		if r.reloadErr != nil {
			return fmt.Sprintf("the dev-server supervisor did not take the checked-out %s (%v); zerops_dev_server action=status hostname=%q names what is wrong with it", devsup.DeclarationPath, r.reloadErr, alias)
		}
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
	return ". Its output: " + boundedTail(log)
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

// noteTextLimit is how much of an output a note carries.
const noteTextLimit = 600

// boundedText trims s to what a note carries, keeping its start.
func boundedText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > noteTextLimit {
		return s[:noteTextLimit] + "…"
	}
	return s
}

// boundedTail trims an output to what a note carries, keeping its end (where
// a failure says why).
func boundedTail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > noteTextLimit {
		return "…" + s[len(s)-noteTextLimit:]
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
