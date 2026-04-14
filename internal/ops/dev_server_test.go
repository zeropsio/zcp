// Tests for: internal/ops/dev_server.go — dev-server lifecycle primitive.
// The mockSSHDeployer from deploy_ssh_test.go is reused so the same
// transport-layer shape captures SSH calls for assertion here.
package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// scriptSSH is a scripted mockSSHDeployer: every ExecSSH call returns
// the next (output, err) pair from the queued list, or a zero-value
// response once the queue is drained. A single queue across all calls
// is the simplest test shape that still lets us assert the SSH-command
// sequence the tool sent.
type scriptSSH struct {
	queue []scriptStep
	calls []scriptCall
}

type scriptStep struct {
	output string
	err    error
}

type scriptCall struct {
	hostname string
	command  string
}

func (s *scriptSSH) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	s.calls = append(s.calls, scriptCall{hostname: hostname, command: command})
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

	// Scripted SSH: rm, spawn, probe (returns OK), tail
	ssh := &scriptSSH{queue: []scriptStep{
		{output: ""},                // rm -f logFile
		{output: ""},                // nohup spawn
		{output: "OK 200 123"},      // health probe
		{output: "starting...\nok"}, // log tail
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
	if len(ssh.calls) < 3 {
		t.Fatalf("expected at least 3 SSH calls, got %d", len(ssh.calls))
	}
	// Critical SSH shape assertion: the spawn call MUST include all three
	// detachment primitives — nohup, `< /dev/null`, and `& disown`.
	// Missing any one of them reintroduces the 120s SSH-channel-hold bug.
	spawn := ssh.calls[1].command
	if !strings.Contains(spawn, "nohup ") {
		t.Errorf("spawn missing nohup: %q", spawn)
	}
	if !strings.Contains(spawn, "< /dev/null") {
		t.Errorf("spawn missing '< /dev/null' stdin redirect: %q", spawn)
	}
	if !strings.Contains(spawn, "& disown") {
		t.Errorf("spawn missing '& disown': %q", spawn)
	}
	if !strings.Contains(spawn, "npm run start:dev") {
		t.Errorf("spawn missing user command: %q", spawn)
	}
}

func TestDevServer_Start_HealthProbeFails(t *testing.T) {
	t.Parallel()

	ssh := &scriptSSH{queue: []scriptStep{
		{output: ""},         // rm
		{output: ""},         // spawn
		{output: "FAIL 000"}, // probe returns connection refused
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

	// stop + rm + spawn + probe + logTail
	ssh := &scriptSSH{queue: []scriptStep{
		{output: "stopped"},    // stop phase
		{output: ""},           // rm
		{output: ""},           // spawn
		{output: "OK 204 500"}, // probe (204 also counts as ready)
		{output: "ok"},         // log tail
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
	if len(ssh.calls) != 5 {
		t.Fatalf("expected 5 SSH calls (stop + rm + spawn + probe + tail), got %d", len(ssh.calls))
	}
	// First call is the stop phase.
	if !strings.Contains(ssh.calls[0].command, "pkill") {
		t.Errorf("expected first call to be stop (pkill), got %q", ssh.calls[0].command)
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
