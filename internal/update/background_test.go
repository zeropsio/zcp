// Tests for: internal/update/background.go — background auto-update goroutine.

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
	"sync/atomic"
	"testing"
	"time"
)

func TestTryApplyUpdate_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("atomic rename not applicable on Windows")
	}
	t.Parallel()

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

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "zcp")
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	var shutdownCalled atomic.Bool
	shutdown := func() { shutdownCalled.Store(true) }

	var logBuf bytes.Buffer
	got := tryApplyUpdate(context.Background(), "0.1.0", binaryPath, shutdown, &logBuf, srv.URL, srv.Client())

	if !got {
		t.Error("tryApplyUpdate should return true on success")
	}
	if !shutdownCalled.Load() {
		t.Error("shutdown should have been called")
	}

	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, newBinary) {
		t.Errorf("binary content = %q, want %q", data, newBinary)
	}
}

func TestTryApplyUpdate_NoUpdate(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v0.1.0"})
	}))
	defer srv.Close()

	var shutdownCalled atomic.Bool
	shutdown := func() { shutdownCalled.Store(true) }

	var logBuf bytes.Buffer
	got := tryApplyUpdate(context.Background(), "0.1.0", filepath.Join(t.TempDir(), "zcp"), shutdown, &logBuf, srv.URL, srv.Client())

	if got {
		t.Error("tryApplyUpdate should return false when no update available")
	}
	if shutdownCalled.Load() {
		t.Error("shutdown should NOT have been called")
	}
}

func TestTryApplyUpdate_NotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not effective on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root can write to any directory")
	}
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v99.0.0"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "zcp")
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	var shutdownCalled atomic.Bool
	shutdown := func() { shutdownCalled.Store(true) }

	var logBuf bytes.Buffer
	got := tryApplyUpdate(context.Background(), "0.1.0", binaryPath, shutdown, &logBuf, srv.URL, srv.Client())

	if got {
		t.Error("tryApplyUpdate should return false when dir not writable")
	}
	if shutdownCalled.Load() {
		t.Error("shutdown should NOT have been called")
	}
	if !strings.Contains(logBuf.String(), "not writable") {
		t.Errorf("log should mention 'not writable', got: %s", logBuf.String())
	}
}

func TestTryApplyUpdate_DownloadError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("atomic rename not applicable on Windows")
	}
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/zeropsio/zcp/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v99.0.0"})
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "zcp")
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	var shutdownCalled atomic.Bool
	shutdown := func() { shutdownCalled.Store(true) }

	var logBuf bytes.Buffer
	got := tryApplyUpdate(context.Background(), "0.1.0", binaryPath, shutdown, &logBuf, srv.URL, srv.Client())

	if got {
		t.Error("tryApplyUpdate should return false on download error")
	}
	if shutdownCalled.Load() {
		t.Error("shutdown should NOT have been called")
	}
	if logBuf.Len() == 0 {
		t.Error("log should contain error message")
	}
}

func TestBackground_DevVersion_Skips(t *testing.T) {
	t.Parallel()

	var shutdownCalled atomic.Bool
	shutdown := func() { shutdownCalled.Store(true) }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var logBuf bytes.Buffer
	Background(ctx, "dev", shutdown, &logBuf, nil)

	// Give goroutine a moment to potentially start (it shouldn't).
	time.Sleep(50 * time.Millisecond)
	cancel()

	if shutdownCalled.Load() {
		t.Error("shutdown should never be called for dev version")
	}
}

func TestBackground_ContextCancelled(t *testing.T) {
	t.Parallel()

	var shutdownCalled atomic.Bool
	shutdown := func() { shutdownCalled.Store(true) }

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var logBuf bytes.Buffer
	Background(ctx, "0.1.0", shutdown, &logBuf, nil)

	// Give goroutine a moment to exit.
	time.Sleep(50 * time.Millisecond)

	if shutdownCalled.Load() {
		t.Error("shutdown should NOT be called when context cancelled")
	}
}

func TestBackground_ForceChannel(t *testing.T) {
	// Cannot use t.Parallel() because t.Setenv modifies process environment.

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v0.1.0"}) // same version, no update
	}))
	defer srv.Close()

	var shutdownCalled atomic.Bool
	shutdown := func() { shutdownCalled.Store(true) }

	forceCh := make(chan struct{}, 1)
	var logBuf bytes.Buffer

	// Set ZCP_UPDATE_URL so the background check uses our mock server.
	t.Setenv("ZCP_UPDATE_URL", srv.URL)

	Background(t.Context(), "0.1.0", shutdown, &logBuf, forceCh)

	// Trigger immediate check via force channel.
	forceCh <- struct{}{}

	// Wait for the check to complete.
	time.Sleep(200 * time.Millisecond)

	// Should have logged something about the check.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "checking") {
		t.Errorf("expected log to mention 'checking', got: %s", logOutput)
	}

	// No update available, so shutdown should NOT be called.
	if shutdownCalled.Load() {
		t.Error("shutdown should NOT be called when no update available")
	}
}
