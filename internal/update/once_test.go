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
	"sync/atomic"
	"testing"
	"time"
)

func TestOnce_DevVersion_Skips(t *testing.T) {
	t.Parallel()

	var shutdownCalled atomic.Bool
	var logBuf syncBuffer

	Once(t.Context(), "dev", NewIdleWaiter(), func() { shutdownCalled.Store(true) }, &logBuf)

	if shutdownCalled.Load() {
		t.Error("shutdown should never be called for dev version")
	}
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

	var shutdownCalled atomic.Bool
	var logBuf syncBuffer

	Once(t.Context(), "0.1.0", NewIdleWaiter(), func() { shutdownCalled.Store(true) }, &logBuf)

	if shutdownCalled.Load() {
		t.Error("shutdown should NOT be called when no update available")
	}
}

func TestOnce_UpdateAvailable_AppliesAndShutdown(t *testing.T) {
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

	var shutdownCalled atomic.Bool
	var logBuf syncBuffer

	waiter := NewIdleWaiter()

	OnceWithOpts(t.Context(), OnceOpts{
		CurrentVersion: "0.1.0",
		Waiter:         waiter,
		Shutdown:       func() { shutdownCalled.Store(true) },
		LogOutput:      &logBuf,
		BinaryPath:     binaryPath,
		CacheDir:       t.TempDir(),
	})

	if !shutdownCalled.Load() {
		t.Error("shutdown should have been called after update")
	}

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

	var shutdownCalled atomic.Bool
	var logBuf syncBuffer

	OnceWithOpts(t.Context(), OnceOpts{
		CurrentVersion: "0.1.0",
		Waiter:         NewIdleWaiter(),
		Shutdown:       func() { shutdownCalled.Store(true) },
		LogOutput:      &logBuf,
		BinaryPath:     binaryPath,
		CacheDir:       t.TempDir(),
	})

	if shutdownCalled.Load() {
		t.Error("shutdown should NOT be called when dir not writable")
	}
}

func TestOnce_WaitsForIdle_BeforeShutdown(t *testing.T) {
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

	waiter := NewIdleWaiter()
	// Simulate a long-running request.
	waiter.active.Add(1)

	var shutdownCalled atomic.Bool
	var logBuf syncBuffer

	cacheDir := t.TempDir()

	done := make(chan struct{})
	go func() {
		OnceWithOpts(t.Context(), OnceOpts{
			CurrentVersion: "0.1.0",
			Waiter:         waiter,
			Shutdown:       func() { shutdownCalled.Store(true) },
			LogOutput:      &logBuf,
			BinaryPath:     binaryPath,
			CacheDir:       cacheDir,
		})
		close(done)
	}()

	// Binary should already be replaced but shutdown should not have been called yet.
	time.Sleep(100 * time.Millisecond)
	if shutdownCalled.Load() {
		t.Fatal("shutdown should NOT be called while request is in-flight")
	}

	// Finish the in-flight request.
	waiter.done()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Once should complete after idle")
	}

	if !shutdownCalled.Load() {
		t.Error("shutdown should have been called after idle")
	}
}

func TestOnce_ContextCancelled_DuringIdleWait(t *testing.T) {
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

	waiter := NewIdleWaiter()
	waiter.active.Add(1) // simulate never-ending request

	var shutdownCalled atomic.Bool
	var logBuf syncBuffer

	ctx, cancel := context.WithCancel(context.Background())

	cacheDir := t.TempDir()

	done := make(chan struct{})
	go func() {
		OnceWithOpts(ctx, OnceOpts{
			CurrentVersion: "0.1.0",
			Waiter:         waiter,
			Shutdown:       func() { shutdownCalled.Store(true) },
			LogOutput:      &logBuf,
			BinaryPath:     binaryPath,
			CacheDir:       cacheDir,
		})
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Once should return when context is cancelled")
	}

	if shutdownCalled.Load() {
		t.Error("shutdown should NOT be called when context is cancelled during idle wait")
	}
}
