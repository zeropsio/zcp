package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/update"
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
	version string
	// log is the tail of a failed supervisor's own output.
	log string
	// reloadErr is a running supervisor's refusal of the reload an acquire
	// asked for after its overlay (an invalid declaration, say).
	reloadErr error
}

// leaseSupervisorLog is where a supervisor zcp starts writes its own output,
// beside its state in the zerops user's home ($HOME/.zcp/devsup), as the
// agent addresses it; the reset at release takes it away.
const leaseSupervisorLog = "~/.zcp/devsup/supervisor.log"

// LeaseSupervisorRelease holds the companion asset of the zcp that takes a
// lease. A development build has no companion release asset.
type LeaseSupervisorRelease struct {
	checker *update.Checker
	dir     string
}

func NewLeaseSupervisorRelease(version string) LeaseSupervisorRelease {
	if !update.IsRelease(version) {
		return LeaseSupervisorRelease{}
	}
	return LeaseSupervisorRelease{
		checker: update.NewChecker(version),
		dir:     filepath.Join(runtime.HomeDir(), ".zcp", "releases", version),
	}
}

// Hold saves the verified asset at container boot. The same operation can
// recover a missing asset when a lease is taken.
func (r LeaseSupervisorRelease) Hold(ctx context.Context) error {
	if r.checker == nil {
		return nil
	}
	return r.checker.HoldAsset(ctx, DevsupAsset, r.path())
}

func (r LeaseSupervisorRelease) path() string {
	return filepath.Join(r.dir, DevsupAsset)
}

func (r LeaseSupervisorRelease) load(ctx context.Context, fetch bool) ([]byte, error) {
	path := r.path()
	asset, err := os.ReadFile(path)
	if fetch && errors.Is(err, os.ErrNotExist) {
		if err := r.Hold(ctx); err != nil {
			return nil, fmt.Errorf("release asset unavailable: %w", err)
		}
		asset, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("read held release asset: %w", err)
	}
	return asset, nil
}

const (
	leaseSupervisorVersionRead = `if command -v zcp-devsup >/dev/null 2>&1; then zcp-devsup ctl version --json 2>/dev/null; c=$?; echo; echo "@@zcp-devsup version $c"; else echo '@@zcp-devsup version 127'; fi`
	leaseSupervisorStatus      = `zcp-devsup ctl status --json 2>/dev/null`
	leaseSupervisorStatusRead  = leaseSupervisorStatus + `; c=$?; echo; echo "@@zcp-devsup status $c"`
)

func swapLeaseSupervisor(ctx context.Context, d LeaseDeps, alias string, load func() ([]byte, error)) error {
	if d.SupervisorRelease.checker == nil {
		return nil
	}
	readCtx, cancel := context.WithTimeout(ctx, containerIdentityTimeout)
	defer cancel()
	out, err := d.SSH.ExecSSH(readCtx, alias, leaseSupervisorVersionRead)
	if err != nil {
		return fmt.Errorf("read image supervisor: %w", err)
	}
	return d.SupervisorRelease.swap(ctx, d.SSH, alias, out, false, load)
}

// swap changes the installed executable only when the answering supervisor,
// if any, is idle. Before signaling it the copy rechecks the state from the
// same control socket under the lock DP-13 uses to start an instance.
func (r LeaseSupervisorRelease) swap(ctx context.Context, ssh SSHDeployer, host string, versionOut []byte, warm bool, load func() ([]byte, error)) error {
	sections, err := splitDevsupSections(string(versionOut))
	if err != nil {
		return fmt.Errorf("read image supervisor version: %w", err)
	}
	reply := sections["version"]
	if reply.code == devsupCodeAbsent {
		return nil
	}
	var installed SupervisorVersion
	if reply.code != devsupCodeOK || json.Unmarshal([]byte(reply.body), &installed) != nil || installed.Version == "" {
		return errors.New("image supervisor did not report its version")
	}
	want := r.checker.CurrentVersion
	if installed.Version == want {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, leaseSupervisorTimeout)
	defer cancel()
	status, err := idleLeaseSupervisor(callCtx, ssh, host)
	if err != nil {
		return fmt.Errorf("inspect the image supervisor: %w", err)
	}
	asset, err := load()
	if err != nil {
		return fmt.Errorf("load the acquiring zcp supervisor: %w", err)
	}
	out, err := ssh.ExecSSHStdin(callCtx, host, leaseSupervisorSwapCommand(installed.Version, want, status, warm), asset)
	if err != nil {
		return fmt.Errorf("copy release supervisor over ssh: %w", err)
	}
	if strings.TrimSpace(string(out)) != want {
		return errors.New("the container did not confirm the installed supervisor version")
	}
	return nil
}

type idleSupervisor struct {
	pid    int
	status string
}

func idleLeaseSupervisor(ctx context.Context, ssh SSHDeployer, host string) (idleSupervisor, error) {
	out, err := ssh.ExecSSH(ctx, host, leaseSupervisorStatusRead)
	if err != nil {
		return idleSupervisor{}, fmt.Errorf("read supervisor before swapping: %w", err)
	}
	sections, err := splitDevsupSections(string(out))
	if err != nil {
		return idleSupervisor{}, fmt.Errorf("read supervisor before swapping: %w", err)
	}
	status, ok := sections["status"]
	if ok && status.code == devsupCodeNoSupervisor {
		return idleSupervisor{}, nil
	}
	var state devsup.State
	if !ok || status.code != devsupCodeOK || json.Unmarshal([]byte(status.body), &state) != nil ||
		state.Supervisor.Pid <= 1 || state.Servers == nil {
		return idleSupervisor{}, errors.New("the supervisor did not confirm it was idle")
	}
	for _, server := range state.Servers {
		if server.Status != devsup.StatusStopped || server.Desired != devsup.DesiredStopped || server.Pid != 0 || server.Pgid != 0 {
			return idleSupervisor{}, errors.New("the existing supervisor has active dev servers")
		}
	}
	return idleSupervisor{pid: state.Supervisor.Pid, status: status.body}, nil
}

func leaseSupervisorLock(seconds int) string {
	return `exec 9>>"$d/ensure.lock" || exit 1; ` + leaseFlockWait("9", seconds)
}

// leaseSupervisorSwapCommand stages stdin beside the image executable, so a
// failed transfer leaves the image file available and publishing is atomic.
// The remote warm checkout lock excludes a completed or concurrent checkout.
// Publish before TERM because zenv can rerun run.start as soon as the old
// process exits. Keep its executable open so the signal and exit check still
// identify it after the rename. The process is not the SSH shell's child
// and ctl exposes no shutdown receipt, so this call checks its exit for up to
// five seconds.
func leaseSupervisorSwapCommand(installed, want string, idle idleSupervisor, warm bool) string {
	q := shellQuote
	steps := []string{
		`refuse() { printf '%s\n' "$1" >&2; exit 1; }`,
		`bin=/usr/local/bin/zcp-devsup`,
		`[ "$(command -v zcp-devsup)" = "$bin" ] || refuse 'the dev pool binary is outside its image path'`,
	}
	if warm {
		wd := q(LeaseWorkDir)
		steps = append(steps,
			"exec "+leaseCheckoutLockFD+"<"+wd,
			"flock -n "+leaseCheckoutLockFD+" || refuse 'a checkout or clone holds the work directory'",
			"[ ! -e "+q(LeaseWorkDir+"/.git")+" ] || [ -f "+q(LeaseWorkDir+"/.git/"+leaseWarmStateFile)+" ] || refuse 'a lease already checked out the work directory'",
		)
	}
	steps = append(steps,
		`d="$HOME/.zcp/devsup"; mkdir -p "$d" || exit 1`,
		leaseSupervisorLock(5)+` || refuse 'the supervisor lock is busy'`,
		`current=$(zcp-devsup ctl version) || exit 1`,
		`if [ "$current" = `+q(want)+` ]; then cat >/dev/null; printf '%s\n' "$current"; exit 0; fi`,
		`[ "$current" = `+q(installed)+` ] || refuse 'the installed supervisor changed during delivery'`,
		`tmp=$(sudo -n mktemp "$bin.swap.XXXXXX") || refuse 'cannot stage the supervisor beside the image binary'`,
		`trap 'sudo -n rm -f -- "$tmp"' EXIT`,
		`sudo -n tee "$tmp" >/dev/null && sudo -n chmod 0755 "$tmp" || refuse 'cannot copy the release binary'`,
		`[ "$("$tmp" ctl version)" = `+q(want)+` ] || refuse 'the copied binary did not report the acquiring zcp version'`,
		`state=$(`+leaseSupervisorStatus+`); code=$?`,
	)
	var stop []string
	if idle.pid == 0 {
		steps = append(steps, `[ "$code" -eq 2 ] || refuse 'a supervisor appeared during delivery'`)
	} else {
		steps = append(steps,
			`[ "$code" -eq 0 ] && [ "$state" = `+q(idle.status)+` ] || refuse 'the supervisor changed or started servers during delivery'`,
			"pid="+strconv.Itoa(idle.pid),
			`exec 8<"$bin" || refuse 'cannot open the image supervisor executable'`,
			`imageExe=/proc/$$/fd/8`,
			`[ "/proc/$pid/exe" -ef "$imageExe" ] || refuse 'the reported supervisor is no longer the image process'`,
		)
		stop = []string{
			`if [ "/proc/$pid/exe" -ef "$imageExe" ]; then`,
			`  kill -TERM "$pid" || refuse 'cannot end the idle image supervisor'`,
			`  end=$(( $(date +%s) + 5 ))`,
			`  while [ "/proc/$pid/exe" -ef "$imageExe" ]; do [ "$(date +%s)" -lt "$end" ] || refuse 'the idle image supervisor did not exit'; sleep 0.1; done`,
			`fi`,
		}
	}
	publish := `sudo -n mv -f "$tmp" "$bin" || refuse 'cannot activate the release supervisor'`
	steps = append(append(steps, publish), stop...)
	steps = append(steps, `printf '%s\n' `+q(want))
	return strings.Join(steps, "\n")
}

func leaseSupervisorSwapNote(err error) string {
	if err == nil {
		return ""
	}
	return "zcp could not swap to its release supervisor (" + boundedText(sshFailureText(err)) + ")"
}

// leaseSupervisorCommand starts `zcp-devsup run` in workDir unless a
// supervisor already holds the control socket, and waits up to waitSeconds
// for the one it started to answer. It answers in devsup sections: "check"
// carries `ctl status`'s state and exit code before anything is started (127
// without the binary); "started" the state and code after the start; "log" the tail of the
// started supervisor's output when it does not answer. The servers' sibling
// overlay and an acquire's reload are separate exchanges after it
// (afterLeaseCheckout): the overlay is computed and sent under the group's
// lock, which this command, whose start may wait seconds, never holds.
//
// Only `ctl status` exit 2 (no supervisor: the socket is absent or refused)
// starts one: 0 answers, and 5 holds the socket unanswering, so a second would
// fight it for the socket and the servers' ports. Where flock exists the check
// and the start run under a lock in the state dir, so concurrent calls start
// one. The supervisor runs in its own session (setsid; nohup where there is
// none), stdin from /dev/null, its output appended to leaseSupervisorLog and
// the lock's descriptor closed, so it holds nothing of the ssh session and
// outlives it, in a scope of its own (ownScopeStep) with the dev servers it
// starts. workDir is shell-quoted; nothing else is composed in.
func leaseSupervisorCommand(workDir string, waitSeconds int) string {
	status := `state=$(` + leaseSupervisorStatus + `); c=$?`
	report := `if [ "$c" -eq 0 ]; then printf '%s\n' "$state"; fi`
	run := `zcp-devsup run </dev/null >>"$d/supervisor.log" 2>&1 9>&-`
	return ownScopeStep(ownScopeDevsup) + strings.Join([]string{
		`if ! command -v zcp-devsup >/dev/null 2>&1; then echo '@@zcp-devsup check 127'; exit 0; fi`,
		`d="$HOME/.zcp/devsup"`,
		`mkdir -p "$d" || exit 1`,
		`if command -v flock >/dev/null 2>&1; then ` + leaseSupervisorLock(60) + ` || exit 1; fi`,
		status,
		report,
		`echo "@@zcp-devsup check $c"`,
		`if [ "$c" -ne 2 ]; then exit 0; fi`,
		`cd ` + shellQuote(workDir) + ` || exit 1`,
		`if command -v setsid >/dev/null 2>&1; then setsid ` + run + ` & else nohup ` + run + ` & fi`,
		`end=$(( $(date +%s) + ` + strconv.Itoa(waitSeconds) + ` ))`,
		`while :; do ` + status + `; if [ "$c" -ne 2 ] || [ "$(date +%s)" -ge "$end" ]; then break; fi; sleep 0.2 2>/dev/null || sleep 1; done`,
		report,
		`echo "@@zcp-devsup started $c"`,
		`if [ "$c" -ne 0 ]; then tail -n 20 "$d/supervisor.log" 2>/dev/null; echo '@@zcp-devsup log 0'; fi`,
	}, "\n")
}

// ensureLeaseSupervisor makes sure zcp-devsup runs in the leased container
// behind alias, leaving one that answers alone: a dev-server call never
// reloads (with the declaration missing from a checkout a reload would stop
// every server); an acquire reloads one that answered by its own call, after
// the overlay (afterLeaseCheckout).
func ensureLeaseSupervisor(ctx context.Context, ssh SSHDeployer, alias string) (leaseSupervisorResult, error) {
	return ensureSupervisorIn(ctx, ssh, alias, LeaseWorkDir, leaseSupervisorWait)
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
	return ensureOutcome(alias, out, check, sections)
}

// ensureOutcome reads what an ensure found or did from its answer's sections.
func ensureOutcome(alias string, out []byte, check devsupAnswer, sections map[string]devsupAnswer) (leaseSupervisorResult, error) {
	switch check.code {
	case devsupCodeAbsent:
		return leaseSupervisorResult{outcome: leaseSupervisorAbsent}, nil
	case devsupCodeOK:
		return leaseSupervisorResult{outcome: leaseSupervisorRunning, version: runningSupervisorVersion(check)}, nil
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
		return leaseSupervisorResult{outcome: leaseSupervisorStarted, version: runningSupervisorVersion(started)}, nil
	case devsupCodeUnresponsive:
		return leaseSupervisorResult{outcome: leaseSupervisorUnresponsive}, nil
	}
	return leaseSupervisorResult{outcome: leaseSupervisorFailed, log: sections["log"].body}, nil
}

func runningSupervisorVersion(answer devsupAnswer) string {
	var state struct {
		Supervisor devsup.SupervisorInfo `json:"supervisor"`
	}
	if json.Unmarshal([]byte(answer.body), &state) != nil {
		return ""
	}
	return state.Supervisor.Version
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
