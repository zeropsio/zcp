//go:build unix

// Tests for: ops/lease_supervisor.go — the command that starts a lease's
// supervisor, run by a local sh against a stub zcp-devsup (docs/spec-workflows.md
// §8 DP-13): it starts one only when none answers, as run.start would (in the
// work dir, with the session's env), detached, and never two at once.
package ops

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// stubDevsupScript stands in for zcp-devsup: `ctl status` exits $STUB_STATUS
// (2, no supervisor, when unset) until a supervisor it started has bound,
// then 0; `ctl reload` is recorded and exits $STUB_RELOAD (0 when unset);
// `ctl env` is recorded with its arguments and exits $STUB_ENV (0 when unset);
// `run` records its pid, cwd and ZEROPS_StableNumber, binds and stays up — or,
// with STUB_RUN_FAILS set, says why on stderr and exits. Each run, env and
// reload is also appended, in turn, to the order file.
const stubDevsupScript = `#!/bin/sh
s="$STUB_DIR"
case "$1" in
run)
  if [ -n "$STUB_RUN_FAILS" ]; then echo "zcp-devsup: control socket: permission denied" >&2; exit 1; fi
  echo "$$ $(pwd) ${ZEROPS_StableNumber:-none}" >> "$s/runs"
  echo run >> "$s/order"
  : > "$s/bound"
  exec sleep 300
  ;;
ctl)
  if [ "$2" = env ]; then shift 2; echo "env $*" >> "$s/order"; if [ -n "$STUB_ENV" ]; then echo '{"code":1,"error":"env: refused"}'; exit "$STUB_ENV"; fi; exit 0; fi
  if [ "$2" = reload ]; then echo reload >> "$s/reloads"; echo reload >> "$s/order"; exit "${STUB_RELOAD:-0}"; fi
  if [ -f "$s/bound" ]; then exit 0; fi
  exit "${STUB_STATUS:-2}"
  ;;
esac
exit 1
`

// localContainer runs each command zcp would send over ssh in a local shell,
// with env as the ssh session's environment.
type localContainer struct {
	shell string
	env   []string
}

func (l localContainer) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, l.shell, "-c", command) //nolint:gosec // a test shell running the command under test
	cmd.Env = l.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, &platform.SSHExecError{Hostname: host, Output: string(out), Err: err}
	}
	return out, nil
}

func (l localContainer) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return l.ExecSSH(ctx, host, command)
}

// pathWithout is $PATH less every directory holding name, so a zcp-devsup
// installed on this machine never answers for the stub.
func pathWithout(name string) []string {
	var dirs []string
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if _, err := os.Stat(filepath.Join(d, name)); err == nil {
			continue
		}
		dirs = append(dirs, d)
	}
	return dirs
}

// devsupHarness is a leased container played by a local shell: a work dir
// whose name needs quoting, a $HOME, and optionally the stub zcp-devsup.
type devsupHarness struct {
	workDir, home, stubDir string
	container              localContainer
}

// posixShells are the shells on this machine the command is run under: sh,
// and dash (Ubuntu's /bin/sh) where it exists.
func posixShells() []string {
	shells := []string{"sh"}
	if _, err := exec.LookPath("dash"); err == nil {
		shells = append(shells, "dash")
	}
	return shells
}

func newDevsupHarness(t *testing.T, shell string, installed bool, extraEnv ...string) devsupHarness {
	t.Helper()
	root := t.TempDir()
	h := devsupHarness{
		workDir: filepath.Join(root, "var www's"),
		home:    filepath.Join(root, "home"),
		stubDir: filepath.Join(root, "stub"),
	}
	for _, d := range []string{h.workDir, h.home, h.stubDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := pathWithout("zcp-devsup")
	if installed {
		if err := os.WriteFile(filepath.Join(h.stubDir, "zcp-devsup"), []byte(stubDevsupScript), 0o755); err != nil {
			t.Fatal(err)
		}
		path = append([]string{h.stubDir}, path...)
	}
	env := append([]string{
		"HOME=" + h.home, "PATH=" + strings.Join(path, string(os.PathListSeparator)),
		"STUB_DIR=" + h.stubDir, "ZEROPS_StableNumber=3",
	}, extraEnv...)
	h.container = localContainer{shell: shell, env: env}
	t.Cleanup(h.killRuns)
	return h
}

// stubRun is one supervisor the stub saw started.
type stubRun struct {
	pid         int
	cwd, stable string
}

// runs are the supervisors the stub saw started ("<pid> <cwd> <stable>" lines;
// the cwd may hold spaces).
func (h devsupHarness) runs(t *testing.T) []stubRun {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.stubDir, "runs"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return parseStubRuns(string(raw))
}

func parseStubRuns(raw string) []stubRun {
	var out []stubRun
	for line := range strings.SplitSeq(strings.TrimSpace(raw), "\n") {
		pidText, rest, ok := strings.Cut(line, " ")
		i := strings.LastIndexByte(rest, ' ')
		pid, err := strconv.Atoi(pidText)
		if !ok || i < 0 || err != nil {
			continue
		}
		out = append(out, stubRun{pid: pid, cwd: rest[:i], stable: rest[i+1:]})
	}
	return out
}

func (h devsupHarness) killRuns() {
	raw, err := os.ReadFile(filepath.Join(h.stubDir, "runs"))
	if err != nil {
		return
	}
	for _, r := range parseStubRuns(string(raw)) {
		if r.pid > 1 {
			_ = syscall.Kill(r.pid, syscall.SIGKILL)
		}
	}
}

func TestEnsureLeaseSupervisor_StartsOneOnlyWhenNoneAnswers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		installed bool
		env       []string
		want      leaseSupervisorOutcome
		wantRuns  int
		wantLog   string
	}{
		{name: "no zcp-devsup in the image starts nothing", installed: false, want: leaseSupervisorAbsent},
		{name: "a supervisor that answers is never joined by a second", installed: true, env: []string{"STUB_STATUS=0"}, want: leaseSupervisorRunning},
		{name: "a supervisor holding the socket unanswering is never joined by a second", installed: true, env: []string{"STUB_STATUS=5"}, want: leaseSupervisorUnresponsive},
		{name: "none runs: one is started and answers", installed: true, want: leaseSupervisorStarted, wantRuns: 1},
		{name: "none runs: the started one does not come up and says why", installed: true, env: []string{"STUB_RUN_FAILS=1"}, want: leaseSupervisorFailed, wantLog: "permission denied"},
	}
	for _, shell := range posixShells() {
		for _, tt := range tests {
			t.Run(shell+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				h := newDevsupHarness(t, shell, tt.installed, tt.env...)

				got, err := ensureSupervisorIn(context.Background(), h.container, "devpool-3", h.workDir, 2, false, nil)
				if err != nil {
					t.Fatalf("ensure: %v", err)
				}
				if got.outcome != tt.want {
					t.Errorf("outcome = %q, want %q (%+v)", got.outcome, tt.want, got)
				}
				if runs := h.runs(t); len(runs) != tt.wantRuns {
					t.Errorf("supervisors started = %d (%v), want %d", len(runs), runs, tt.wantRuns)
				}
				if tt.wantLog != "" && !strings.Contains(got.log, tt.wantLog) {
					t.Errorf("log = %q, want the started supervisor's own output %q", got.log, tt.wantLog)
				}
			})
		}
	}
}

func TestEnsureLeaseSupervisor_StartsItAsRunStartWouldAndDetached(t *testing.T) {
	t.Parallel()
	for _, shell := range posixShells() {
		t.Run(shell, func(t *testing.T) {
			t.Parallel()
			testStartsItAsRunStartWould(t, shell)
		})
	}
}

func testStartsItAsRunStartWould(t *testing.T, shell string) {
	t.Helper()
	h := newDevsupHarness(t, shell, true)

	begun := time.Now()
	got, err := ensureSupervisorIn(context.Background(), h.container, "devpool-3", h.workDir, 5, false, nil)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if got.outcome != leaseSupervisorStarted {
		t.Fatalf("outcome = %q, want started (%+v)", got.outcome, got)
	}
	// The stub supervisor stays up 300 s and ensure waits at most 5 s for its
	// socket: a minute tells the two apart even when a loaded machine spawns
	// the script's shells slowly (seen: 15 s under the full test battery).
	if took := time.Since(begun); took > time.Minute {
		t.Errorf("ensure took %s: it must return once the socket answers, not wait for the supervisor to end", took)
	}
	runs := h.runs(t)
	if len(runs) != 1 {
		t.Fatalf("runs = %v, want one", runs)
	}
	wantDir, _ := filepath.EvalSymlinks(h.workDir)
	gotDir, _ := filepath.EvalSymlinks(runs[0].cwd)
	if gotDir != wantDir {
		t.Errorf("the supervisor ran in %q, want the work dir %q (run.start's cwd)", gotDir, wantDir)
	}
	if runs[0].stable != "3" {
		t.Errorf("ZEROPS_StableNumber = %q, want the session's 3 (its preview port depends on it)", runs[0].stable)
	}
	// It outlives the call that started it.
	if err := syscall.Kill(runs[0].pid, 0); err != nil {
		t.Errorf("the supervisor (pid %d) did not outlive the ssh call: %v", runs[0].pid, err)
	}
	if _, err := os.Stat(filepath.Join(h.home, ".zcp", "devsup", "supervisor.log")); err != nil {
		t.Errorf("its output is not kept in $HOME/.zcp/devsup/supervisor.log: %v", err)
	}

	// Asked again, it finds the one it started and starts no second.
	again, err := ensureSupervisorIn(context.Background(), h.container, "devpool-3", h.workDir, 5, false, nil)
	if err != nil || again.outcome != leaseSupervisorRunning {
		t.Errorf("second ensure = %+v, %v; want running", again, err)
	}
	if runs := h.runs(t); len(runs) != 1 {
		t.Errorf("a second ensure started another supervisor: %v", runs)
	}
}

func TestEnsureLeaseSupervisor_ConcurrentCallsStartOne(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("flock"); err != nil {
		t.Skip("no flock on this machine: the lock is taken only where flock exists")
	}
	h := newDevsupHarness(t, "sh", true)

	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			if _, err := ensureSupervisorIn(context.Background(), h.container, "devpool-3", h.workDir, 5, false, nil); err != nil {
				t.Errorf("ensure: %v", err)
			}
		})
	}
	wg.Wait()
	if runs := h.runs(t); len(runs) != 1 {
		t.Errorf("three concurrent ensures started %d supervisors (%v), want one", len(runs), runs)
	}
}

func TestEnsureLeaseSupervisor_CommandQuotesItsWorkDir(t *testing.T) {
	t.Parallel()
	cmd := leaseSupervisorCommand("/var/www/it's; touch x", 10, true, []string{"env", "--set", "HQ_HOST=it's; touch y"})
	if !strings.Contains(cmd, "cd "+shellQuote("/var/www/it's; touch x")) {
		t.Errorf("the work dir is not shell-quoted:\n%s", cmd)
	}
	if !strings.Contains(cmd, "zcp-devsup ctl 'env' '--set' "+shellQuote("HQ_HOST=it's; touch y")+" --json") {
		t.Errorf("the overlay is not shell-quoted:\n%s", cmd)
	}
	for _, banned := range []string{"pkill", "pgrep", "GIT_TOKEN"} {
		if strings.Contains(cmd, banned) {
			t.Errorf("command contains %q:\n%s", banned, cmd)
		}
	}
}

func TestEnsureLeaseSupervisor_ReloadsARunningOneOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		reload      bool
		env         []string
		wantReloads int
		wantErr     string
	}{
		// After a checkout the running supervisor takes the new declaration.
		{name: "asked (acquire): reloaded", reload: true, wantReloads: 1},
		{name: "asked, refused: the result says so", reload: true, env: []string{"STUB_RELOAD=4"}, wantReloads: 1, wantErr: "4"},
		// A dev-server call never reloads: with the declaration missing from a
		// checkout, a reload would stop every server.
		{name: "not asked (a dev-server call): left alone", reload: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newDevsupHarness(t, "sh", true, append([]string{"STUB_STATUS=0"}, tt.env...)...)

			got, err := ensureSupervisorIn(context.Background(), h.container, "devpool-3", h.workDir, 2, tt.reload, nil)
			if err != nil {
				t.Fatalf("ensure: %v", err)
			}
			if got.outcome != leaseSupervisorRunning || len(h.runs(t)) != 0 {
				t.Errorf("outcome = %q, runs %v; want the running one kept, none started", got.outcome, h.runs(t))
			}
			raw, _ := os.ReadFile(filepath.Join(h.stubDir, "reloads"))
			if n := strings.Count(string(raw), "reload"); n != tt.wantReloads {
				t.Errorf("reloads = %d, want %d", n, tt.wantReloads)
			}
			if tt.wantErr == "" && got.reloadErr != nil {
				t.Errorf("reload error = %v, want none", got.reloadErr)
			}
			if tt.wantErr != "" && (got.reloadErr == nil || !strings.Contains(got.reloadErr.Error(), tt.wantErr)) {
				t.Errorf("reload error = %v, want one naming %q", got.reloadErr, tt.wantErr)
			}
		})
	}
}

func TestEnsureLeaseSupervisor_SendsTheOverlayBeforeTheReloadOrAfterTheStart(t *testing.T) {
	t.Parallel()
	env := []string{"env", "--set", "HQ_HOST=node-id-7.runtime.hqpool.zerops", "--set", "WEB_HOST=it's"}
	const sent = "env --set HQ_HOST=node-id-7.runtime.hqpool.zerops --set WEB_HOST=it's --json"
	tests := []struct {
		name      string
		stubEnv   []string
		want      leaseSupervisorOutcome
		wantOrder []string
		wantErr   bool
	}{
		// A running supervisor (run.start's, in a container the platform just
		// created) takes the overlay before the reload starts the servers.
		{name: "one answers: the overlay, then the reload", stubEnv: []string{"STUB_STATUS=0"}, want: leaseSupervisorRunning, wantOrder: []string{sent, "reload"}},
		// A reused container's supervisor boots the servers itself; the overlay
		// follows at once and it restarts them under it.
		{name: "none answers: the start, then the overlay", want: leaseSupervisorStarted, wantOrder: []string{"run", sent}},
		{name: "a refused overlay: still reloaded, the refusal kept", stubEnv: []string{"STUB_STATUS=0", "STUB_ENV=1"}, want: leaseSupervisorRunning, wantOrder: []string{sent, "reload"}, wantErr: true},
	}
	for _, shell := range posixShells() {
		for _, tt := range tests {
			t.Run(shell+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				h := newDevsupHarness(t, shell, true, tt.stubEnv...)

				got, err := ensureSupervisorIn(context.Background(), h.container, "devpool-3", h.workDir, 5, true, env)
				if err != nil {
					t.Fatalf("ensure: %v", err)
				}
				if got.outcome != tt.want || got.reloadErr != nil {
					t.Errorf("outcome = %q (reload error %v), want %q", got.outcome, got.reloadErr, tt.want)
				}
				raw, _ := os.ReadFile(filepath.Join(h.stubDir, "order"))
				if order := strings.Split(strings.TrimSpace(string(raw)), "\n"); !slices.Equal(order, tt.wantOrder) {
					t.Errorf("order = %q, want %q", order, tt.wantOrder)
				}
				if tt.wantErr != (got.envErr != nil) || (tt.wantErr && !strings.Contains(got.envErr.Error(), "refused")) {
					t.Errorf("env error = %v, want an error: %v", got.envErr, tt.wantErr)
				}
			})
		}
	}
}
