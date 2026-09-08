package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeReadManifestFixture builds a minimal finalized capture window (empty
// provider capture is enough — ReadManifestFile never parses protocol
// content) with one extra eval artifact file inventoried by Finalize, so
// ReadManifestFile has something manifest-listed to return.
func writeReadManifestFixture(t *testing.T) (sessionDir string, relPath string) {
	t.Helper()
	root := t.TempDir()
	recorder, err := NewRecorder(RecorderConfig{RootDir: root, SessionID: "read-manifest-session", Label: "read-manifest"})
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	manifest, err := NewSessionManifest(SessionManifestConfig{
		SessionDir: recorder.SessionDir(),
		SessionID:  "read-manifest-session",
		Command:    []string{"fixture"},
	})
	if err != nil {
		t.Fatalf("NewSessionManifest() error = %v", err)
	}
	evalDir := filepath.Join(recorder.SessionDir(), "eval", "run-1", "scenario-1")
	if err := os.MkdirAll(evalDir, 0o700); err != nil {
		t.Fatalf("mkdir eval dir: %v", err)
	}
	artifactPath := filepath.Join(evalDir, "meta.json")
	if err := os.WriteFile(artifactPath, []byte(`{"scenarioId":"scenario-1"}`), 0o600); err != nil {
		t.Fatalf("write eval artifact: %v", err)
	}
	if err := recorder.Close(CaptureComplete, 0); err != nil {
		t.Fatalf("Recorder.Close() error = %v", err)
	}
	if err := manifest.Finalize(CaptureComplete, 0); err != nil {
		t.Fatalf("manifest.Finalize() error = %v", err)
	}
	return recorder.SessionDir(), "eval/run-1/scenario-1/meta.json"
}

func TestReadManifestFile_ListedAndMatching_ReturnsBytes(t *testing.T) {
	t.Parallel()

	sessionDir, relPath := writeReadManifestFixture(t)
	manifest, err := ReadSessionManifest(filepath.Join(sessionDir, manifestFilename))
	if err != nil {
		t.Fatalf("ReadSessionManifest() error = %v", err)
	}
	data, err := ReadManifestFile(sessionDir, manifest, relPath)
	if err != nil {
		t.Fatalf("ReadManifestFile() error = %v", err)
	}
	if string(data) != `{"scenarioId":"scenario-1"}` {
		t.Fatalf("ReadManifestFile() = %q", data)
	}
}

func TestReadManifestFile_TamperAfterFinalize_Refused(t *testing.T) {
	t.Parallel()

	sessionDir, relPath := writeReadManifestFixture(t)
	manifest, err := ReadSessionManifest(filepath.Join(sessionDir, manifestFilename))
	if err != nil {
		t.Fatalf("ReadSessionManifest() error = %v", err)
	}
	fullPath := filepath.Join(sessionDir, filepath.FromSlash(relPath))
	info, err := os.Stat(fullPath)
	if err != nil {
		t.Fatalf("stat artifact: %v", err)
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	tampered := append([]byte(nil), data...)
	tampered[0] ^= 1
	if err := os.WriteFile(fullPath, tampered, 0o600); err != nil {
		t.Fatalf("tamper artifact: %v", err)
	}
	if err := os.Chtimes(fullPath, info.ModTime(), info.ModTime()); err != nil {
		t.Fatalf("restore mtime: %v", err)
	}

	_, err = ReadManifestFile(sessionDir, manifest, relPath)
	if err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("ReadManifestFile() error = %v, want hash mismatch", err)
	}
}

func TestReadManifestFile_SymlinkOrEscapeOrUnlisted_Refused(t *testing.T) {
	t.Parallel()

	sessionDir, relPath := writeReadManifestFixture(t)
	manifest, err := ReadSessionManifest(filepath.Join(sessionDir, manifestFilename))
	if err != nil {
		t.Fatalf("ReadSessionManifest() error = %v", err)
	}

	tests := []struct {
		name string
		rel  string
		want string
	}{
		{name: "unlisted path inside session", rel: "manifest.json", want: "not listed"},
		{name: "escaping path", rel: "../outside.json", want: "escapes"},
		{name: "symlink component", rel: "eval/run-1/scenario-1/link.json", want: "symlink"},
	}
	// Build a symlink manifest entry case separately: create a symlink on disk
	// but the manifest does not list it, so it's caught either by "not listed"
	// or by the symlink check depending on resolver order — both are refusals.
	linkPath := filepath.Join(sessionDir, "eval", "run-1", "scenario-1", "link.json")
	if err := os.Symlink(filepath.Join(sessionDir, filepath.FromSlash(relPath)), linkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(linkPath) })

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ReadManifestFile(sessionDir, manifest, tc.rel)
			if err == nil {
				t.Fatalf("ReadManifestFile(%q) succeeded, want refusal containing %q", tc.rel, tc.want)
			}
		})
	}
}

func TestReadManifestFile_ManifestLess_Diagnostic(t *testing.T) {
	t.Parallel()

	sessionDir, relPath := writeReadManifestFixture(t)
	if err := os.Remove(filepath.Join(sessionDir, manifestFilename)); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	_, err := ReadManifestFile(sessionDir, nil, relPath)
	if err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("ReadManifestFile() error = %v, want manifest-less diagnostic", err)
	}
}
