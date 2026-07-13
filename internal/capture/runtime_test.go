package capture

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntime_ProviderControlLifecycleAndManifestCloseAsOneWindow(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()

	socketDir := shortSocketDir(t)
	runtime, err := StartRuntime(context.Background(), RuntimeConfig{
		RootDir:       t.TempDir(),
		CaptureID:     "runtime-window",
		Label:         "runtime-test",
		ListenAddr:    "127.0.0.1:0",
		UpstreamURL:   upstream.URL,
		ControlSocket: filepath.Join(socketDir, "control.sock"),
		ControlToken:  "runtime-secret",
		Command:       []string{"zcp", "capture", "on"},
		Build:         CaptureBuildInfo{Version: "test"},
	})
	if err != nil {
		t.Fatalf("StartRuntime() error = %v", err)
	}

	client := NewControlClient(runtime.ControlSocket(), "runtime-secret")
	defer client.Close()
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.CaptureID != "runtime-window" || status.ProxyURL != runtime.ProxyURL() || status.SessionDir != runtime.SessionDir() {
		t.Fatalf("status = %+v", status)
	}
	if _, err := client.Mark(context.Background(), LifecycleMarker{Kind: LifecycleEvalRunStart, EvalRunID: "suite-runtime"}); err != nil {
		t.Fatalf("Mark() error = %v", err)
	}

	metadata := `{"device_id":"device","account_uuid":"account","session_id":"claude-runtime"}`
	body := `{"model":"claude-test","metadata":{"user_id":` + quoteJSONForTest(t, metadata) + `},"messages":[]}`
	response, err := http.Post(runtime.ProxyURL()+"/v1/messages", "application/json", strings.NewReader(body)) //nolint:noctx // local bounded test servers
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()

	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-runtime.ShutdownRequested():
	case <-time.After(time.Second):
		t.Fatal("runtime did not surface control shutdown")
	}
	if status, err := runtime.Close(CaptureComplete); err != nil || status != CaptureComplete {
		t.Fatalf("Close() = status %q, err %v", status, err)
	}

	manifest, err := ReadSessionManifest(filepath.Join(runtime.SessionDir(), manifestFilename))
	if err != nil {
		t.Fatalf("ReadSessionManifest() error = %v", err)
	}
	if manifest.Status != CaptureComplete || manifest.ChildExitCode != nil {
		t.Fatalf("manifest lifecycle = %+v", manifest)
	}
	kinds := map[string]bool{}
	for _, file := range manifest.Files {
		kinds[file.Kind] = true
	}
	if !kinds[ManifestFileProvider] || !kinds[ManifestFileLifecycle] {
		t.Fatalf("manifest files = %+v", manifest.Files)
	}
	if _, err := os.Stat(runtime.ControlSocket()); !os.IsNotExist(err) {
		t.Fatalf("control socket remains after close: %v", err)
	}

	report, err := InspectSession(runtime.SessionDir())
	if err != nil {
		t.Fatalf("InspectSession() error = %v", err)
	}
	if len(report.ClaudeSessions) != 1 || report.ClaudeSessions[0].SessionID != "claude-runtime" {
		t.Fatalf("Claude sessions = %+v", report.ClaudeSessions)
	}
}

func quoteJSONForTest(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(data)
}
