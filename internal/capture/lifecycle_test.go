package capture

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLifecycleRecorder_RecordsEvalBindingAndTerminalStatus(t *testing.T) {
	t.Parallel()

	sessionDir := t.TempDir()
	recorder, err := NewLifecycleRecorder(sessionDir, "capture-test")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	markers := []LifecycleMarker{
		{Kind: LifecycleEvalRunStart, EvalRunID: "suite-1"},
		{Kind: LifecycleScenarioStart, EvalRunID: "suite-1", ScenarioRunID: "weather"},
		{Kind: LifecycleInvocationStart, EvalRunID: "suite-1", ScenarioRunID: "weather", InvocationID: "initial-1", Phase: "agent.initial"},
		{Kind: LifecycleInvocationBind, EvalRunID: "suite-1", ScenarioRunID: "weather", InvocationID: "initial-1", Phase: "agent.initial", ClaudeSessionID: "claude-session-1"},
		{Kind: LifecycleInvocationEnd, EvalRunID: "suite-1", ScenarioRunID: "weather", InvocationID: "initial-1", Phase: "agent.initial", Status: CaptureComplete},
		{Kind: LifecycleScenarioEnd, EvalRunID: "suite-1", ScenarioRunID: "weather", Status: CaptureComplete},
		{Kind: LifecycleEvalRunEnd, EvalRunID: "suite-1", Status: CaptureComplete},
	}
	for _, marker := range markers {
		if _, err := recorder.Mark(marker); err != nil {
			t.Fatalf("Mark(%s) error = %v", marker.Kind, err)
		}
	}
	if err := recorder.Close(CaptureComplete); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	records, err := ReadLifecycleRecords(filepath.Join(sessionDir, lifecycleFilename))
	if err != nil {
		t.Fatalf("ReadLifecycleRecords() error = %v", err)
	}
	if len(records) != len(markers)+2 {
		t.Fatalf("record count = %d, want %d", len(records), len(markers)+2)
	}
	if records[0].Kind != LifecycleStreamStart || records[len(records)-1].Kind != LifecycleStreamEnd {
		t.Fatalf("terminal records = first:%s last:%s", records[0].Kind, records[len(records)-1].Kind)
	}
	bound := records[4]
	if bound.Kind != LifecycleInvocationBind || bound.ClaudeSessionID != "claude-session-1" || bound.InvocationID != "initial-1" {
		t.Fatalf("binding record = %+v", bound)
	}
	if records[len(records)-1].Status != CaptureComplete {
		t.Fatalf("terminal status = %q", records[len(records)-1].Status)
	}
	info, err := os.Stat(filepath.Join(sessionDir, lifecycleFilename))
	if err != nil {
		t.Fatalf("stat lifecycle: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("lifecycle mode = %#o, want 0600", got)
	}
}

func TestLifecycleRecorder_RejectsIncompleteInvocationBinding(t *testing.T) {
	t.Parallel()

	recorder, err := NewLifecycleRecorder(t.TempDir(), "capture-test")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	defer func() { _ = recorder.Close(CapturePartial) }()

	_, err = recorder.Mark(LifecycleMarker{Kind: LifecycleInvocationBind, EvalRunID: "suite-1", InvocationID: "initial-1"})
	if err == nil {
		t.Fatal("Mark() error = nil, want missing scenario/session validation")
	}
}

func TestControlServer_MarkerStatusAndShutdownRoundTrip(t *testing.T) {
	t.Parallel()

	sessionDir := t.TempDir()
	recorder, err := NewLifecycleRecorder(sessionDir, "capture-control")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	shutdown := make(chan struct{}, 1)
	socketDir := shortSocketDir(t)
	server, err := StartControlServer(context.Background(), ControlServerConfig{
		SocketPath: filepath.Join(socketDir, "control.sock"),
		Token:      "control-secret",
		Recorder:   recorder,
		Status: ControlStatus{
			CaptureID: "capture-control",
			Status:    CaptureRunning,
			ProxyURL:  "http://127.0.0.1:43210",
			ProcessID: 4242,
		},
		Shutdown: func() { shutdown <- struct{}{} },
	})
	if err != nil {
		t.Fatalf("StartControlServer() error = %v", err)
	}
	defer func() {
		_ = server.Close(context.Background())
		_ = recorder.Close(CaptureComplete)
	}()

	client := NewControlClient(server.SocketPath(), "control-secret")
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.CaptureID != "capture-control" || status.ProxyURL != "http://127.0.0.1:43210" || status.ProcessID != 4242 {
		t.Fatalf("status = %+v", status)
	}
	stored, err := client.Mark(context.Background(), LifecycleMarker{Kind: LifecycleEvalRunStart, EvalRunID: "suite-1"})
	if err != nil {
		t.Fatalf("Mark() error = %v", err)
	}
	if stored.Seq != 2 || stored.EvalRunID != "suite-1" {
		t.Fatalf("stored marker = %+v", stored)
	}
	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("shutdown callback was not invoked")
	}
}

func TestControlServer_RejectsWrongToken(t *testing.T) {
	t.Parallel()

	recorder, err := NewLifecycleRecorder(t.TempDir(), "capture-control")
	if err != nil {
		t.Fatalf("NewLifecycleRecorder() error = %v", err)
	}
	defer func() { _ = recorder.Close(CapturePartial) }()
	server, err := StartControlServer(context.Background(), ControlServerConfig{
		SocketPath: filepath.Join(shortSocketDir(t), "control.sock"),
		Token:      "right-token",
		Recorder:   recorder,
		Status:     ControlStatus{CaptureID: "capture-control", Status: CaptureRunning},
	})
	if err != nil {
		t.Fatalf("StartControlServer() error = %v", err)
	}
	defer func() { _ = server.Close(context.Background()) }()

	_, err = NewControlClient(server.SocketPath(), "wrong-token").Status(context.Background())
	if err == nil || !errors.Is(err, ErrControlUnauthorized) {
		t.Fatalf("Status() error = %v, want ErrControlUnauthorized", err)
	}
}

func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "zcp-capture-control-") //nolint:usetesting // short absolute path is required by Unix socket limits on macOS
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
