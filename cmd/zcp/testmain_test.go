package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestMain runs zcp's main() when this binary is started with a zcp command
// line — by a CLI test (zcpBinary) or by zcp re-executing itself — and the
// tests otherwise: go test always starts a test binary with -test.* flags.
// After the tests it removes the per-test-binary shared directory (the
// candidate copy and the fake executables).
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		main()
		os.Exit(0)
	}
	code := m.Run()
	if sharedTest.dir != "" {
		_ = os.RemoveAll(sharedTest.dir)
	}
	os.Exit(code)
}

// sharedTest holds files every test in this binary may reuse.
var sharedTest struct {
	mu   sync.Mutex
	dir  string
	fake map[string]string // body sha256 → shared executable path
}

func sharedTestDir(t *testing.T) string {
	t.Helper()
	sharedTest.mu.Lock()
	defer sharedTest.mu.Unlock()
	if sharedTest.dir == "" {
		dir, err := os.MkdirTemp("", "zcp-cmd-test-shared-") //nolint:usetesting // outlives each test; TestMain removes it
		if err != nil {
			t.Fatalf("create shared test dir: %v", err)
		}
		sharedTest.dir = dir
	}
	return sharedTest.dir
}

// installFakeExecutable places an executable with body at path. macOS
// assesses every newly created executable on its first exec — about a
// second when idle and several under load — so path is a hard link to one
// read-only file per distinct body, assessed once per test binary. The
// file is read-only so a test that rewrites path fails instead of changing
// every other test's fake.
func installFakeExecutable(t *testing.T, path, body string) {
	t.Helper()
	shared := sharedFakeExecutable(t, body)
	if err := os.Link(shared, path); err != nil {
		t.Fatalf("link fake executable %s: %v", path, err)
	}
}

func sharedFakeExecutable(t *testing.T, body string) string {
	t.Helper()
	dir := sharedTestDir(t)
	sum := sha256.Sum256([]byte(body))
	key := hex.EncodeToString(sum[:])
	sharedTest.mu.Lock()
	defer sharedTest.mu.Unlock()
	if path, ok := sharedTest.fake[key]; ok {
		return path
	}
	path := filepath.Join(dir, "fake-"+key[:16])
	if err := os.WriteFile(path, []byte(body), 0o500); err != nil {
		t.Fatalf("write shared fake executable: %v", err)
	}
	if sharedTest.fake == nil {
		sharedTest.fake = make(map[string]string)
	}
	sharedTest.fake[key] = path
	return path
}
