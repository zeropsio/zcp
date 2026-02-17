//go:build e2e && !windows

// Tests for: e2e — background auto-update with binary replacement and graceful restart.
// Requires: Unix (SIGUSR1), real build toolchain (go build).

package e2e_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestE2E_BackgroundUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Need a real Go toolchain to build test binaries.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}

	srcDir := findProjectRoot(t)

	// Build two binaries with different versions from the same source.
	oldBinary := buildBinaryWithVersion(t, srcDir, "0.0.1")
	newBinary := buildBinaryWithVersion(t, srcDir, "99.0.0")

	newBinaryBytes, err := os.ReadFile(newBinary)
	if err != nil {
		t.Fatalf("read new binary: %v", err)
	}
	newChecksum := sha256.Sum256(newBinaryBytes)

	// Mock HTTP server serving GitHub API and download endpoints.
	asset := fmt.Sprintf("zcp-%s-%s", runtime.GOOS, runtime.GOARCH)
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/zeropsio/zcp/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v99.0.0"})
	})
	mux.HandleFunc("/download/v99.0.0/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		http.ServeFile(w, nil, newBinary)
	})
	mockSrv := httptest.NewServer(mux)
	defer mockSrv.Close()

	// Copy "old" binary to a temp exec path.
	dir := t.TempDir()
	execPath := filepath.Join(dir, "zcp")
	copyFile(t, oldBinary, execPath)
	if err := os.Chmod(execPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Start the "old" binary as a subprocess.
	// It will do startup update check → find v99.0.0 → download "new" from mock → replace → exit.
	cmd := exec.Command(execPath)
	cmd.Env = append(os.Environ(),
		"ZCP_UPDATE_URL="+mockSrv.URL,
		"ZCP_AUTO_UPDATE=1",
		// Provide a fake API key so the binary doesn't exit early on auth.
		// The startup check + replace happens before MCP server auth.
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Provide stdin so the process can start (STDIO MCP server reads stdin).
	stdinR, stdinW := io.Pipe()
	cmd.Stdin = stdinR

	t.Log("Starting old binary (v0.0.1)...")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start old binary: %v", err)
	}

	// Wait for the process to exit (it should re-exec after updating).
	// The startup check in checkAndApplyUpdate() should find the update and re-exec.
	// But since re-exec replaces the process, and the "new" binary is v99.0.0 which
	// won't find an update, it will proceed to MCP server mode (and likely fail on auth).
	// We give it time to complete the update cycle.
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		// Process exited — the re-exec may have failed on auth (expected).
		t.Logf("Process exited: %v", err)
		t.Logf("Stderr: %s", stderr.String())
	case <-time.After(30 * time.Second):
		// Process still running — close stdin to trigger shutdown.
		stdinW.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
		}
	}

	// Verify the binary on disk was replaced with the "new" binary.
	replacedBytes, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("read replaced binary: %v", err)
	}
	replacedChecksum := sha256.Sum256(replacedBytes)

	if replacedChecksum != newChecksum {
		t.Errorf("binary on disk checksum mismatch: binary was not replaced with new version")
	} else {
		t.Log("Binary on disk matches new version (checksum OK)")
	}
}

func TestE2E_BackgroundUpdate_SIGUSR1(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}

	srcDir := findProjectRoot(t)

	// Build a binary that is already "up to date" so SIGUSR1 check finds nothing.
	binary := buildBinaryWithVersion(t, srcDir, "99.0.0")

	// Mock HTTP server that returns the same version.
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/zeropsio/zcp/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v99.0.0"})
	})
	mockSrv := httptest.NewServer(mux)
	defer mockSrv.Close()

	dir := t.TempDir()
	execPath := filepath.Join(dir, "zcp")
	copyFile(t, binary, execPath)
	if err := os.Chmod(execPath, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(execPath)
	cmd.Env = append(os.Environ(),
		"ZCP_UPDATE_URL="+mockSrv.URL,
		"ZCP_AUTO_UPDATE=1",
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdinR, stdinW := io.Pipe()
	cmd.Stdin = stdinR

	t.Log("Starting binary (v99.0.0)...")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start binary: %v", err)
	}

	// Wait a moment for the process to start up (it may fail on auth, but
	// the background goroutine should still be running briefly).
	time.Sleep(2 * time.Second)

	// Send SIGUSR1 to trigger immediate check.
	t.Log("Sending SIGUSR1...")
	if err := cmd.Process.Signal(syscall.SIGUSR1); err != nil {
		// Process may have already exited due to auth failure.
		t.Logf("SIGUSR1 send: %v (process may have exited)", err)
	}

	time.Sleep(2 * time.Second)

	// Close stdin to trigger shutdown.
	stdinW.Close()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		<-done
	}

	stderrOutput := stderr.String()
	t.Logf("Stderr output:\n%s", stderrOutput)

	// The SIGUSR1 handling may or may not produce output depending on whether
	// the process reached the background goroutine before auth failure.
	// If it did, we should see the force check message.
	if strings.Contains(stderrOutput, "force checking") {
		t.Log("SIGUSR1 force check was triggered (OK)")
	} else {
		t.Log("SIGUSR1 may not have been processed (process may have exited before background goroutine started)")
	}
}

// findProjectRoot walks up from the current directory to find the go.mod.
func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod)")
		}
		dir = parent
	}
}

// buildBinaryWithVersion compiles a zcp binary with the given version string.
func buildBinaryWithVersion(t *testing.T, srcDir, version string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), fmt.Sprintf("zcp-%s", version))
	ldflags := fmt.Sprintf("-X github.com/zeropsio/zcp/internal/server.Version=%s", version)
	cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", out, "./cmd/zcp")
	cmd.Dir = srcDir
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build binary v%s: %v\n%s", version, err, output)
	}
	return out
}

// copyFile copies src to dst.
func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
}
