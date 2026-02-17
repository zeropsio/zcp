// Tests for: internal/update/apply.go — download and atomic binary replacement.

package update

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestApply_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("atomic rename test not applicable on Windows")
	}
	t.Parallel()

	binaryContent := []byte("#!/bin/sh\necho new-version\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(binaryContent)
	}))
	defer srv.Close()

	// Create a fake current binary.
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "zcp")
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	info := &Info{
		Available:   true,
		DownloadURL: srv.URL + "/zcp-darwin-arm64",
	}

	err := Apply(t.Context(), info, binaryPath, srv.Client())
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}

	// Verify the binary was replaced.
	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binaryContent) {
		t.Errorf("binary content = %q, want %q", got, binaryContent)
	}

	// Verify executable permission.
	fi, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&0o111 == 0 {
		t.Error("binary should be executable")
	}
}

func TestApply_DownloadError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "zcp")
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	info := &Info{
		Available:   true,
		DownloadURL: srv.URL + "/missing",
	}

	err := Apply(t.Context(), info, binaryPath, srv.Client())
	if err == nil {
		t.Fatal("expected error on 404")
	}

	// Original binary should be untouched.
	got, _ := os.ReadFile(binaryPath)
	if string(got) != "old" {
		t.Error("original binary should not be modified on failure")
	}
}

func TestApply_NotAvailable(t *testing.T) {
	t.Parallel()

	info := &Info{Available: false}
	err := Apply(t.Context(), info, "/nonexistent", nil)
	if err != nil {
		t.Errorf("Apply with Available=false should be no-op, got: %v", err)
	}
}

func TestApply_NetworkError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "zcp")
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	info := &Info{
		Available:   true,
		DownloadURL: "http://127.0.0.1:1/zcp", // connection refused
	}

	client := &http.Client{Timeout: 1}
	err := Apply(t.Context(), info, binaryPath, client)
	if err == nil {
		t.Fatal("expected error on network failure")
	}

	// Original binary should be untouched.
	got, _ := os.ReadFile(binaryPath)
	if string(got) != "old" {
		t.Error("original binary should not be modified on failure")
	}
}

func TestCanWrite_WritableDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if !CanWrite(dir) {
		t.Error("CanWrite should return true for writable temp dir")
	}
}

func TestCanWrite_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not effective on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root can write to any directory")
	}
	t.Parallel()

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if CanWrite(dir) {
		t.Error("CanWrite should return false for read-only dir")
	}
}

func TestCanWrite_NonexistentDir(t *testing.T) {
	t.Parallel()

	if CanWrite("/nonexistent-dir-that-does-not-exist") {
		t.Error("CanWrite should return false for nonexistent dir")
	}
}
