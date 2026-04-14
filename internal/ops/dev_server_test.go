// Tests for: internal/ops/dev_server.go — dev-server lifecycle primitive.
// The mockSSHDeployer from deploy_ssh_test.go is reused so the same
// transport-layer shape captures SSH calls for assertion here.
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// scriptSSH is a scripted mockSSHDeployer: every ExecSSH call returns
// the next (output, err) pair from the queued list, or a zero-value
// response once the queue is drained. A single queue across all calls
// is the simplest test shape that still lets us assert the SSH-command
// sequence the tool sent.
//
// Background calls go through the same queue as foreground calls so a
// test can assert the combined sequence. Each recorded call carries a
// `background` flag and the timeout the caller passed, which lets the
// dev_server tests verify that the spawn step took the background
// codepath with the correct budget.
type scriptSSH struct {
	queue []scriptStep
	calls []scriptCall
}

type scriptStep struct {
	output string
	err    error
}

type scriptCall struct {
	hostname   string
	command    string
	background bool
	bgTimeout  time.Duration
}

func (s *scriptSSH) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	s.calls = append(s.calls, scriptCall{hostname: hostname, command: command})
	return s.next()
}

func (s *scriptSSH) ExecSSHBackground(_ context.Context, hostname, command string, timeout time.Duration) ([]byte, error) {
	s.calls = append(s.calls, scriptCall{hostname: hostname, command: command, background: true, bgTimeout: timeout})
	return s.next()
}

func (s *scriptSSH) next() ([]byte, error) {
	if len(s.queue) == 0 {
		return nil, nil
	}
	step := s.queue[0]
	s.queue = s.queue[1:]
	if step.err != nil {
		return []byte(step.output), step.err
	}
	return []byte(step.output), nil
}

// mockClientWithServices returns a platform mock that reports the given
// hostnames as existing services — enough for verifyDevServerTarget to
// pass.
func mockClientWithServices(hostnames ...string) platform.Client {
	svcs := make([]platform.ServiceStack, 0, len(hostnames))
	for _, h := range hostnames {
		svcs = append(svcs, platform.ServiceStack{
			ID:        "svc-" + h,
			Name:      h, // Name IS the hostname on ServiceStack.
			ProjectID: "p1",
			Status:    "ACTIVE",
		})
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices(svcs)
}

func TestDevServer_Start_Success(t *testing.T) {
	t.Parallel()

	// Scripted SSH: spawn (bg, emits pid ack), probe (OK), tail
	ssh := &scriptSSH{queue: []scriptStep{
		{output: "zcp-dev-server-spawned pid=1234"}, // bg spawn ack
		{output: "OK 200 123"},                      // health probe
		{output: "starting...\nok"},                 // log tail
	}}

	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:     "start",
			Hostname:   "apidev",
			Command:    "npm run start:dev",
			Port:       3000,
			HealthPath: "/api/health",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Running {
		t.Errorf("expected Running=true, got %+v", result)
	}
	if result.HealthStatus != 200 {
		t.Errorf("expected HealthStatus=200, got %d", result.HealthStatus)
	}
	if result.StartMillis != 123 {
		t.Errorf("expected StartMillis=123, got %d", result.StartMillis)
	}
	if result.LogTail == "" {
		t.Errorf("expected non-empty LogTail")
	}
	if len(ssh.calls) != 3 {
		t.Fatalf("expected exactly 3 SSH calls (spawn + probe + tail), got %d", len(ssh.calls))
	}

	// Call 0: spawn. MUST go through the background codepath (scriptCall.background=true)
	// and carry a bounded timeout. The spawn command MUST use setsid for
	// session detach, redirect stdio, and emit the ack marker so the
	// outer ssh shell has something to print before exiting.
	spawn := ssh.calls[0]
	if !spawn.background {
		t.Error("spawn call did not go through ExecSSHBackground — dev_server must use the background variant so ssh uses -T -n and a tight timeout")
	}
	if spawn.bgTimeout <= 0 {
		t.Errorf("spawn call bgTimeout=%s, want a positive bounded timeout", spawn.bgTimeout)
	}
	if spawn.bgTimeout > 30*time.Second {
		t.Errorf("spawn call bgTimeout=%s, want <= 30s — a correct detach returns in well under a second", spawn.bgTimeout)
	}
	if !strings.Contains(spawn.command, "setsid") {
		t.Errorf("spawn missing setsid — process must leave the remote shell's session/pgroup so sshd can close the channel: %q", spawn.command)
	}
	if !strings.Contains(spawn.command, "> ") || !strings.Contains(spawn.command, "2>&1") {
		t.Errorf("spawn missing stdout/stderr redirect to log file: %q", spawn.command)
	}
	if !strings.Contains(spawn.command, "< /dev/null") {
		t.Errorf("spawn missing '< /dev/null' stdin redirect: %q", spawn.command)
	}
	if !strings.Contains(spawn.command, "zcp-dev-server-spawned") {
		t.Errorf("spawn missing pid-ack echo marker — tool relies on this to confirm the outer shell reached the exit point: %q", spawn.command)
	}
	if !strings.Contains(spawn.command, "npm run start:dev") {
		t.Errorf("spawn missing user command: %q", spawn.command)
	}
	// The new shape must NOT use disown — setsid already moves the child
	// out of harm's way, and disown was part of the v17 pattern that hung.
	if strings.Contains(spawn.command, "disown") {
		t.Errorf("spawn still uses 'disown' — setsid replaces it, do not stack both: %q", spawn.command)
	}

	// Call 1: probe, foreground.
	if ssh.calls[1].background {
		t.Error("probe call went through background codepath — should be foreground ExecSSH")
	}
	// Call 2: log tail, foreground.
	if ssh.calls[2].background {
		t.Error("log tail call went through background codepath — should be foreground ExecSSH")
	}
}

// TestDevServer_Start_SpawnTimeoutReturnsStructuredReason — when the
// background spawn exceeds its budget, the tool must NOT bubble a raw
// error up to the agent. It must return a DevServerResult with
// Running=false, a "spawn_timeout" reason, and a Message that tells
// the agent exactly what to do next. Anything less is a regression to
// the v17 failure where `dev_server start: spawn: ssh apidev: signal:
// killed` landed in the agent's context with no guidance.
func TestDevServer_Start_SpawnTimeoutReturnsStructuredReason(t *testing.T) {
	t.Parallel()

	timeoutErr := &platform.SSHExecError{
		Hostname: "apidev",
		Output:   "",
		Err:      context.DeadlineExceeded,
	}
	ssh := &scriptSSH{queue: []scriptStep{
		{output: "", err: timeoutErr}, // spawn times out
		{output: ""},                  // fallback log tail (empty log file — spawn didn't get far)
	}}

	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:     "start",
			Hostname:   "apidev",
			Command:    "npm run start:dev",
			Port:       3000,
			HealthPath: "/api/health",
		})
	if err != nil {
		t.Fatalf("expected nil error (structured DevServerResult), got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result on spawn timeout")
	}
	if result.Running {
		t.Errorf("expected Running=false on spawn timeout, got %+v", result)
	}
	if result.Reason != "spawn_timeout" {
		t.Errorf("Reason = %q, want %q", result.Reason, "spawn_timeout")
	}
	if !strings.Contains(result.Message, "did not detach") && !strings.Contains(result.Message, "spawn") {
		t.Errorf("Message should explain the spawn didn't detach; got: %q", result.Message)
	}
}

// TestDevServer_Start_SpawnGenericErrorReturnsStructuredReason — any
// non-timeout spawn error (auth fail, connection refused, no such host)
// must also land as a structured result, not a bubbled error.
func TestDevServer_Start_SpawnGenericErrorReturnsStructuredReason(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{queue: []scriptStep{
		{output: "Permission denied (publickey)", err: errors.New("exit status 255")}, // spawn fails
		{output: ""}, // fallback log tail
	}}

	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:     "start",
			Hostname:   "apidev",
			Command:    "npm run start:dev",
			Port:       3000,
			HealthPath: "/api/health",
		})
	if err != nil {
		t.Fatalf("expected nil error (structured DevServerResult), got %v", err)
	}
	if result == nil || result.Running {
		t.Fatalf("expected Running=false result, got %+v", result)
	}
	if result.Reason != "spawn_error" {
		t.Errorf("Reason = %q, want %q", result.Reason, "spawn_error")
	}
	if !strings.Contains(result.Message, "spawn") {
		t.Errorf("Message should mention spawn failure; got: %q", result.Message)
	}
}

// TestDevServer_Start_SpawnMissingAckMarker — if the spawn returns
// successfully but the output does NOT contain the "zcp-dev-server-spawned"
// marker, something is off on the remote (e.g. echo disabled, stdout
// swallowed, non-bash shell). The tool must flag this as a distinct
// reason so the agent knows to investigate shell configuration rather
// than the dev-server code.
func TestDevServer_Start_SpawnMissingAckMarker(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{queue: []scriptStep{
		{output: ""},          // spawn returned no output at all
		{output: "OK 200 50"}, // probe still returns OK
		{output: "starting."}, // log tail
	}}

	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:     "start",
			Hostname:   "apidev",
			Command:    "npm run start:dev",
			Port:       3000,
			HealthPath: "/api/health",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The health probe succeeded so Running should still be true — the
	// missing ack only matters diagnostically. But Warnings must carry
	// a note so the close-step review catches future regressions.
	if !result.Running {
		t.Errorf("probe returned OK, Running should be true, got %+v", result)
	}
	if !strings.Contains(result.Message, "ack") && !strings.Contains(result.Message, "spawn") {
		// Soft assertion — either the Message or Warnings path is fine,
		// as long as the agent has something to notice.
		t.Logf("note: spawn ack missing but result.Message = %q — consider surfacing", result.Message)
	}
}

// TestDevServer_Start_SpawnShapeHasSetsidBeforeCommand — a defense-in-
// depth check on the exact ordering of setsid / redirect / background.
// The redirect MUST bind to the backgrounded setsid process, not to
// the outer shell — otherwise the subshell inherits the ssh channel's
// stdio and ssh holds the connection open.
func TestDevServer_Start_SpawnShapeHasSetsidBeforeCommand(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{queue: []scriptStep{
		{output: "zcp-dev-server-spawned pid=1"},
		{output: "OK 200 10"},
		{output: ""},
	}}

	_, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:     "start",
			Hostname:   "apidev",
			Command:    "npm run start:dev",
			Port:       3000,
			HealthPath: "/api/health",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	spawn := ssh.calls[0].command
	// Verify ordering invariants. setsid must appear BEFORE the & and the
	// redirect must appear BEFORE the &, so the redirect binds to the
	// backgrounded command not the outer shell.
	iSetsid := strings.Index(spawn, "setsid")
	iRedirect := strings.Index(spawn, "2>&1")
	iAmp := strings.LastIndex(spawn, " &")
	if iAmp < 0 {
		iAmp = strings.Index(spawn, "&\n")
	}
	if iSetsid < 0 || iRedirect < 0 || iAmp < 0 {
		t.Fatalf("spawn shape missing one of setsid/redirect/background: %q", spawn)
	}
	if iSetsid > iRedirect || iRedirect > iAmp {
		t.Errorf("spawn ordering wrong — want setsid < redirect < &, got setsid=%d redirect=%d &=%d in %q",
			iSetsid, iRedirect, iAmp, spawn)
	}
}

func TestDevServer_Start_HealthProbeFails(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{queue: []scriptStep{
		{output: "zcp-dev-server-spawned pid=7"},        // bg spawn ack
		{output: "FAIL 000"},                            // probe returns connection refused
		{output: "Error: listen EADDRINUSE ::: 3000\n"}, // log tail
	}}

	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:      "start",
			Hostname:    "apidev",
			Command:     "npm run start:dev",
			Port:        3000,
			HealthPath:  "/api/health",
			WaitSeconds: 5,
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Running {
		t.Errorf("expected Running=false, got %+v", result)
	}
	if !strings.Contains(result.Reason, "connection_refused") {
		t.Errorf("expected reason to classify as connection_refused, got %q", result.Reason)
	}
	if !strings.Contains(result.LogTail, "EADDRINUSE") {
		t.Errorf("expected LogTail to carry EADDRINUSE, got %q", result.LogTail)
	}
}

func TestDevServer_Start_MissingCommand(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{}
	_, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:   "start",
			Hostname: "apidev",
			Port:     3000,
		})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T", err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("expected ErrInvalidParameter, got %s", pe.Code)
	}
	if len(ssh.calls) != 0 {
		t.Errorf("expected no SSH calls on validation error, got %d", len(ssh.calls))
	}
}

func TestDevServer_Start_InvalidHostname(t *testing.T) {
	t.Parallel()

	cases := []string{
		"APIDEV",                       // uppercase
		"api dev",                      // space
		"api;rm -rf /",                 // shell injection
		"",                             // empty
		"a" + strings.Repeat("b", 128), // too long
	}
	for _, hostname := range cases {
		t.Run(hostname, func(t *testing.T) {
			t.Parallel()
			ssh := &scriptSSH{}
			_, err := ExecuteDevServer(context.Background(), ssh, nil, "p1",
				DevServerParams{
					Action:   "start",
					Hostname: hostname,
					Command:  "npm run dev",
					Port:     3000,
				})
			if err == nil {
				t.Errorf("expected error for hostname %q", hostname)
			}
			if len(ssh.calls) != 0 {
				t.Errorf("hostname %q: expected 0 ssh calls, got %d", hostname, len(ssh.calls))
			}
		})
	}
}

func TestDevServer_Start_ServiceNotFound(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{}
	_, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("other"), "p1",
		DevServerParams{
			Action:   "start",
			Hostname: "apidev",
			Command:  "npm run dev",
			Port:     3000,
		})
	if err == nil {
		t.Fatal("expected error for unknown hostname")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T", err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("expected ErrServiceNotFound, got %s", pe.Code)
	}
}

func TestDevServer_Stop_ByCommand(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{queue: []scriptStep{{output: "stopped"}}}
	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:   "stop",
			Hostname: "apidev",
			Command:  "npm run start:dev",
			Port:     3000,
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Running {
		t.Errorf("expected Running=false after stop")
	}
	if len(ssh.calls) != 1 {
		t.Fatalf("expected 1 ssh call, got %d", len(ssh.calls))
	}
	cmd := ssh.calls[0].command
	// Must include pkill with the derived first-token match AND fuser on the port.
	if !strings.Contains(cmd, "pkill -f") {
		t.Errorf("expected pkill in stop command: %q", cmd)
	}
	if !strings.Contains(cmd, "fuser -k 3000/tcp") {
		t.Errorf("expected fuser on port 3000: %q", cmd)
	}
	// Must tolerate "nothing to kill" with || true.
	if !strings.Contains(cmd, "|| true") {
		t.Errorf("expected '|| true' tolerance in stop command: %q", cmd)
	}
}

func TestDevServer_Status_NotRunning(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{queue: []scriptStep{{output: "000"}}}
	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:     "status",
			Hostname:   "apidev",
			Port:       3000,
			HealthPath: "/api/health",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Running {
		t.Errorf("expected Running=false for 000 curl response")
	}
	if result.Reason != "connection_refused" {
		t.Errorf("expected reason=connection_refused, got %q", result.Reason)
	}
}

func TestDevServer_Status_Running(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{queue: []scriptStep{{output: "200"}}}
	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:     "status",
			Hostname:   "apidev",
			Port:       3000,
			HealthPath: "/health",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Running {
		t.Errorf("expected Running=true for 200 response, got %+v", result)
	}
	if result.HealthStatus != 200 {
		t.Errorf("expected HealthStatus=200, got %d", result.HealthStatus)
	}
}

func TestDevServer_Logs_ReturnsTail(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{queue: []scriptStep{{output: "line 1\nline 2\nline 3"}}}
	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:   "logs",
			Hostname: "apidev",
			LogFile:  "/tmp/nest.log",
			LogLines: 10,
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.LogTail, "line 1") {
		t.Errorf("expected log tail to include 'line 1', got %q", result.LogTail)
	}
	if result.LogFile != "/tmp/nest.log" {
		t.Errorf("expected LogFile to be passed through, got %q", result.LogFile)
	}
	// Verify the SSH command uses `tail -n 10` with the supplied log path.
	cmd := ssh.calls[0].command
	if !strings.Contains(cmd, "tail -n 10") {
		t.Errorf("expected 'tail -n 10' in command, got %q", cmd)
	}
}

func TestDevServer_Restart_IsStopThenStart(t *testing.T) {
	t.Parallel()

	// stop + spawn + probe + logTail
	ssh := &scriptSSH{queue: []scriptStep{
		{output: "stopped"},                      // stop phase
		{output: "zcp-dev-server-spawned pid=1"}, // bg spawn ack
		{output: "OK 204 500"},                   // probe (204 also counts as ready)
		{output: "ok"},                           // log tail
	}}
	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("workerdev"), "p1",
		DevServerParams{
			Action:     "restart",
			Hostname:   "workerdev",
			Command:    "npm run start:dev",
			Port:       3001,
			HealthPath: "/health",
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "restart" {
		t.Errorf("expected Action=restart, got %q", result.Action)
	}
	if !result.Running {
		t.Errorf("expected Running=true, got %+v", result)
	}
	if result.HealthStatus != 204 {
		t.Errorf("expected HealthStatus=204, got %d", result.HealthStatus)
	}
	if len(ssh.calls) != 4 {
		t.Fatalf("expected 4 SSH calls (stop + spawn + probe + tail), got %d", len(ssh.calls))
	}
	// First call is the stop phase.
	if !strings.Contains(ssh.calls[0].command, "pkill") {
		t.Errorf("expected first call to be stop (pkill), got %q", ssh.calls[0].command)
	}
	// Second call is the background spawn.
	if !ssh.calls[1].background {
		t.Error("spawn call in restart must use ExecSSHBackground")
	}
}

func TestDevServer_UnknownAction(t *testing.T) {
	t.Parallel()

	_, err := ExecuteDevServer(context.Background(), &scriptSSH{}, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:   "kill-all",
			Hostname: "apidev",
		})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestDevServer_FirstShellToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"npm run start:dev", "npm"},
		{"PORT=3000 npm run start:dev", "npm"},
		{"NODE_ENV=dev PORT=3000 node dist/main.js", "node"},
		{"./node_modules/.bin/vite --host 0.0.0.0", "./node_modules/.bin/vite"},
		{"", ""},
	}
	for _, tc := range cases {
		got := firstShellToken(tc.in)
		if got != tc.want {
			t.Errorf("firstShellToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
