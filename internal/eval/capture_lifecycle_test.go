package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/capture"
)

func TestExtractSessionIDFromStream_UsesClaudeSystemIdentity(t *testing.T) {
	t.Parallel()

	stream := []byte("{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"user-sim-session\"}\n{\"type\":\"assistant\",\"message\":{}}\n")
	sessionID, err := extractSessionIDFromStream(stream)
	if err != nil {
		t.Fatalf("extractSessionIDFromStream() error = %v", err)
	}
	if sessionID != "user-sim-session" {
		t.Fatalf("session ID = %q", sessionID)
	}
}

func TestRunnerCaptureLifecycle_LateBindsInvocationWithoutChangingProtocol(t *testing.T) {
	t.Parallel()

	sessionDir := t.TempDir()
	recorder, err := capture.NewLifecycleRecorder(sessionDir, "capture-eval")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	socketDir, err := os.MkdirTemp("/tmp", "zcp-eval-control-") //nolint:usetesting // short absolute path is required by Unix socket limits on macOS
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	server, err := capture.StartControlServer(context.Background(), capture.ControlServerConfig{
		SocketPath: filepath.Join(socketDir, "control.sock"),
		Token:      "eval-secret",
		Recorder:   recorder,
		Status: capture.ControlStatus{
			CaptureID:  "capture-eval",
			Status:     capture.CaptureRunning,
			ProxyURL:   "http://127.0.0.1:43210",
			SessionDir: sessionDir,
			ProcessID:  os.Getpid(),
		},
	})
	if err != nil {
		t.Fatalf("StartControlServer() error = %v", err)
	}
	connection := &capture.Connection{
		CaptureID:  "capture-eval",
		ProxyURL:   "http://127.0.0.1:43210",
		SessionDir: sessionDir,
		Control:    capture.NewControlClient(server.SocketPath(), "eval-secret"),
	}
	runner := NewRunner(RunnerConfig{Capture: connection}, nil, nil, "project")
	ctx := context.Background()

	runner.beginCaptureEvalRun(ctx, "suite-1")
	runner.captureScenarioStart(ctx, "suite-1", "weather")
	invocation := runner.captureInvocationStart(ctx, "suite-1", "weather", "weather/agent.initial", "agent.initial", "")
	invocation.Bind(ctx, "claude-session-1")
	invocation.End(ctx, capture.CaptureComplete, nil)
	runner.captureScenarioEnd(ctx, "suite-1", "weather", capture.CaptureComplete, nil)
	runner.endCaptureEvalRun(ctx, "suite-1", capture.CaptureComplete, nil)

	connection.Close()
	if err := server.Close(context.Background()); err != nil {
		t.Fatalf("ControlServer.Close() error = %v", err)
	}
	if err := recorder.Close(capture.CaptureComplete); err != nil {
		t.Fatalf("LifecycleRecorder.Close() error = %v", err)
	}
	records, err := capture.ReadLifecycleRecords(filepath.Join(sessionDir, "lifecycle.jsonl"))
	if err != nil {
		t.Fatalf("ReadLifecycleRecords() error = %v", err)
	}
	var bind *capture.LifecycleRecord
	for index := range records {
		if records[index].Kind == capture.LifecycleInvocationBind {
			bind = &records[index]
			break
		}
	}
	if bind == nil || bind.ClaudeSessionID != "claude-session-1" || bind.InvocationID != "weather/agent.initial" || bind.Phase != "agent.initial" {
		t.Fatalf("binding record = %+v", bind)
	}
}
