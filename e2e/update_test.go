//go:build e2e && !windows

// Tests for: e2e — async auto-update with binary replacement and idle-wait graceful restart.
// Requires: Unix, real build toolchain (go build).

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
	"testing"
	"time"
)

func TestE2E_AsyncUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

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
	// MCP server starts immediately. The background goroutine:
	//   check → find v99.0.0 → download "new" from mock → replace binary →
	//   wait for idle → trigger graceful shutdown.
	cmd := exec.Command(execPath)
	cmd.Env = append(os.Environ(),
		"ZCP_UPDATE_URL="+mockSrv.URL,
		"ZCP_AUTO_UPDATE=1",
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

	// Wait for the process to exit. The async update goroutine should:
	// 1. Download the new binary and replace on disk
	// 2. Wait for idle (no MCP requests in-flight — immediate since we sent none)
	// 3. Call shutdown → context cancel → MCP server exits
	// The process may also exit early due to auth failure, but the binary
	// should still be replaced since the update goroutine runs concurrently.
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		t.Logf("Process exited: %v", err)
		t.Logf("Stderr: %s", stderr.String())
	case <-time.After(30 * time.Second):
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

	// Verify stderr mentions the update.
	stderrOutput := stderr.String()
	if strings.Contains(stderrOutput, "updated") {
		t.Log("Stderr confirms update was applied")
	}
}

func TestE2E_AsyncUpdate_NoUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not in PATH")
	}

	srcDir := findProjectRoot(t)

	// Build a binary that is already up to date.
	binary := buildBinaryWithVersion(t, srcDir, "99.0.0")

	// Mock HTTP server returns the same version.
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

	originalBytes, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	originalChecksum := sha256.Sum256(originalBytes)

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

	// Give it time to start and check for updates.
	time.Sleep(3 * time.Second)

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

	// Binary should NOT have been replaced.
	currentBytes, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	currentChecksum := sha256.Sum256(currentBytes)

	if currentChecksum != originalChecksum {
		t.Error("binary was unexpectedly modified when no update was available")
	} else {
		t.Log("Binary unchanged (no update available) — OK")
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
