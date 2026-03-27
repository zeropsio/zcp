// Tests for: internal/update/once.go — transparent async auto-update.

package update

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOnce_DevVersion_Skips(t *testing.T) {
	t.Parallel()

	var logBuf syncBuffer

	Once(t.Context(), "dev", &logBuf)

	if logBuf.String() != "" {
		t.Errorf("expected no log output for dev version, got: %s", logBuf.String())
	}
}

func TestOnce_NoUpdate_Returns(t *testing.T) {
	// Cannot use t.Parallel() — t.Setenv modifies process environment.

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v0.1.0"})
	}))
	defer srv.Close()
	t.Setenv("ZCP_UPDATE_URL", srv.URL)

	var logBuf syncBuffer

	// Use OnceWithOpts with isolated CacheDir to prevent stale cache from
	// other test runs (e.g., v99.0.0 from TestOnce_UpdateAvailable) causing
	// a false "updated" log.
	OnceWithOpts(t.Context(), OnceOpts{
		CurrentVersion: "0.1.0",
		LogOutput:      &logBuf,
		CacheDir:       t.TempDir(),
	})

	if strings.Contains(logBuf.String(), "updated") {
		t.Error("should NOT log 'updated' when no update available")
	}
}

func TestOnce_UpdateAvailable_AppliesWithoutShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("atomic rename not applicable on Windows")
	}
	// Cannot use t.Parallel() — t.Setenv modifies process environment.

	newBinary := []byte("#!/bin/sh\necho v2\n")
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/zeropsio/zcp/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v99.0.0"})
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(newBinary)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	t.Setenv("ZCP_UPDATE_URL", srv.URL)

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "zcp")
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	var logBuf syncBuffer

	OnceWithOpts(t.Context(), OnceOpts{
		CurrentVersion: "0.1.0",
		LogOutput:      &logBuf,
		BinaryPath:     binaryPath,
		CacheDir:       t.TempDir(),
	})

	// Binary should be replaced on disk.
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, newBinary) {
		t.Errorf("binary content = %q, want %q", data, newBinary)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "updated") {
		t.Errorf("expected log to mention 'updated', got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "active on next restart") {
		t.Errorf("expected log to mention 'active on next restart', got: %s", logOutput)
	}
}

func TestOnce_NotWritable_Skips(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not effective on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root can write to any directory")
	}
	// Cannot use t.Parallel() — t.Setenv modifies process environment.

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v99.0.0"})
	}))
	defer srv.Close()
	t.Setenv("ZCP_UPDATE_URL", srv.URL)

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "zcp")
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	var logBuf syncBuffer

	OnceWithOpts(t.Context(), OnceOpts{
		CurrentVersion: "0.1.0",
		LogOutput:      &logBuf,
		BinaryPath:     binaryPath,
		CacheDir:       t.TempDir(),
	})

	// Binary should NOT be replaced.
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte("old")) {
		t.Errorf("binary should not have been modified, got: %q", data)
	}
}

func TestOnce_ContextCancelled_NoUpdate(t *testing.T) {
	// Cannot use t.Parallel() — t.Setenv modifies process environment.

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v99.0.0"})
	}))
	defer srv.Close()
	t.Setenv("ZCP_UPDATE_URL", srv.URL)

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "zcp")
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	var logBuf syncBuffer

	OnceWithOpts(ctx, OnceOpts{
		CurrentVersion: "0.1.0",
		LogOutput:      &logBuf,
		BinaryPath:     binaryPath,
		CacheDir:       t.TempDir(),
	})

	// Binary should NOT be replaced (context was cancelled before download).
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte("old")) {
		t.Errorf("binary should not have been modified when context cancelled, got: %q", data)
	}
}
