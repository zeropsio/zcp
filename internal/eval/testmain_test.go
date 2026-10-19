package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestMain removes the per-test-binary shared directory of fake
// executables after every test ran.
func TestMain(m *testing.M) {
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
		dir, err := os.MkdirTemp("", "zcp-eval-test-shared-") //nolint:usetesting // outlives each test; TestMain removes it
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
