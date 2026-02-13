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

	err := Apply(info, binaryPath, srv.Client())
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

	err := Apply(info, binaryPath, srv.Client())
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
	err := Apply(info, "/nonexistent", nil)
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
	err := Apply(info, binaryPath, client)
	if err == nil {
		t.Fatal("expected error on network failure")
	}

	// Original binary should be untouched.
	got, _ := os.ReadFile(binaryPath)
	if string(got) != "old" {
		t.Error("original binary should not be modified on failure")
	}
}
