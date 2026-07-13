package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionManifest_RunningThenFinalizedWithPrivateRawInventory(t *testing.T) {
	t.Parallel()

	sessionDir := filepath.Join(t.TempDir(), "capture-session")
	if err := os.MkdirAll(filepath.Join(sessionDir, "mcp"), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sessionDir, "eval", "suite-1", "weather"), 0o700); err != nil {
		t.Fatalf("MkdirAll(eval) error = %v", err)
	}
	providerBytes := []byte("{\"kind\":\"session.start\"}\n")
	mcpBytes := []byte("{\"kind\":\"mcp.stream.start\"}\n")
	lifecycleBytes := []byte("{\"kind\":\"lifecycle.stream.start\"}\n")
	evalBytes := []byte("prompt\n")
	if err := os.WriteFile(filepath.Join(sessionDir, "provider.jsonl"), providerBytes, 0o600); err != nil {
		t.Fatalf("write provider fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "mcp", "zcp-4242.jsonl"), mcpBytes, 0o600); err != nil {
		t.Fatalf("write MCP fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, lifecycleFilename), lifecycleBytes, 0o600); err != nil {
		t.Fatalf("write lifecycle fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "eval", "suite-1", "weather", "task-prompt.txt"), evalBytes, 0o600); err != nil {
		t.Fatalf("write eval fixture: %v", err)
	}

	manifest, err := NewSessionManifest(SessionManifestConfig{
		SessionDir: sessionDir,
		SessionID:  "capture-session",
		Label:      "weather",
		Command:    []string{"zcp", "eval", "behavioral", "run", "--id", "weather"},
		Build: CaptureBuildInfo{
			Version: "v-test",
			Commit:  "abc123",
			Built:   "2026-07-13T00:00:00Z",
		},
		Provider: ProviderManifestInfo{
			Origin:   "https://api.anthropic.com",
			ProxyURL: "http://127.0.0.1:43210",
		},
	})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}

	info, err := os.Stat(manifest.Path())
	if err != nil {
		t.Fatalf("stat manifest: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("manifest mode = %#o, want 0600", got)
	}
	running, err := ReadSessionManifest(manifest.Path())
	if err != nil {
		t.Fatalf("ReadSessionManifest(running) error = %v", err)
	}
	if running.FormatVersion != ManifestFormat1 || running.Status != CaptureRunning || running.EndedAt != nil || running.ChildExitCode != nil {
		t.Fatalf("running manifest = %+v", running)
	}
	if len(running.Files) != 0 {
		t.Fatalf("running file inventory = %+v, want empty until finalization", running.Files)
	}

	if err := manifest.Finalize(CaptureComplete, 0); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	final, err := ReadSessionManifest(manifest.Path())
	if err != nil {
		t.Fatalf("ReadSessionManifest(final) error = %v", err)
	}
	if final.Status != CaptureComplete || final.EndedAt == nil || final.ChildExitCode == nil || *final.ChildExitCode != 0 {
		t.Fatalf("final lifecycle = %+v", final)
	}
	if len(final.Files) != 4 {
		t.Fatalf("file inventory count = %d, want provider + lifecycle + MCP + eval", len(final.Files))
	}
	providerHash := sha256.Sum256(providerBytes)
	mcpHash := sha256.Sum256(mcpBytes)
	lifecycleHash := sha256.Sum256(lifecycleBytes)
	evalHash := sha256.Sum256(evalBytes)
	wantFiles := []ManifestFile{
		{Kind: ManifestFileEval, Path: "eval/suite-1/weather/task-prompt.txt", SizeBytes: int64(len(evalBytes)), SHA256: hex.EncodeToString(evalHash[:])},
		{Kind: ManifestFileLifecycle, Path: lifecycleFilename, SizeBytes: int64(len(lifecycleBytes)), SHA256: hex.EncodeToString(lifecycleHash[:])},
		{Kind: ManifestFileMCP, Path: "mcp/zcp-4242.jsonl", SizeBytes: int64(len(mcpBytes)), SHA256: hex.EncodeToString(mcpHash[:])},
		{Kind: ManifestFileProvider, Path: "provider.jsonl", SizeBytes: int64(len(providerBytes)), SHA256: hex.EncodeToString(providerHash[:])},
	}
	for index, want := range wantFiles {
		if final.Files[index] != want {
			t.Fatalf("file[%d] = %+v, want %+v", index, final.Files[index], want)
		}
	}
}

func TestRecoverUncleanSessionManifest_InventoriesCrashPrefix(t *testing.T) {
	t.Parallel()

	sessionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionDir, recordsFilename), nil, 0o600); err != nil {
		t.Fatalf("write empty provider prefix: %v", err)
	}
	manifest, err := NewSessionManifest(SessionManifestConfig{SessionDir: sessionDir, SessionID: "crashed-session"})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	recovered, err := RecoverUncleanSessionManifest(sessionDir)
	if err != nil {
		t.Fatalf("RecoverUncleanSessionManifest() error = %v", err)
	}
	if !recovered {
		t.Fatal("RecoverUncleanSessionManifest() recovered = false")
	}
	document, err := ReadSessionManifest(manifest.Path())
	if err != nil {
		t.Fatalf("ReadSessionManifest() error = %v", err)
	}
	if document.Status != CaptureUnclean || document.EndedAt == nil || len(document.Files) != 1 || document.Files[0].Path != recordsFilename {
		t.Fatalf("recovered manifest = %+v", document)
	}
}

func TestSessionManifest_FinalizeDaemonHasNoChildExitCode(t *testing.T) {
	t.Parallel()

	sessionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sessionDir, recordsFilename), []byte("provider\n"), 0o600); err != nil {
		t.Fatalf("write provider fixture: %v", err)
	}
	manifest, err := NewSessionManifest(SessionManifestConfig{SessionDir: sessionDir, SessionID: "daemon-session"})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	if err := manifest.FinalizeDaemon(CaptureComplete); err != nil {
		t.Fatalf("FinalizeDaemon() error = %v", err)
	}
	document, err := ReadSessionManifest(manifest.Path())
	if err != nil {
		t.Fatalf("ReadSessionManifest() error = %v", err)
	}
	if document.ChildExitCode != nil || document.Status != CaptureComplete {
		t.Fatalf("daemon manifest lifecycle = %+v", document)
	}
}

func TestSessionManifest_RejectsRawFileOutsideSession(t *testing.T) {
	t.Parallel()

	sessionDir := filepath.Join(t.TempDir(), "session")
	if err := os.Mkdir(sessionDir, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	manifest, err := NewSessionManifest(SessionManifestConfig{SessionDir: sessionDir, SessionID: "session"})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	if err := manifest.addFile(filepath.Join(sessionDir, "..", "outside.jsonl"), ManifestFileProvider); err == nil {
		t.Fatal("addFile() accepted path outside capture session")
	}
}
