package tools

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLiveEnvFile renders the container's live env store — the file a step
// reads because a running process cannot see a variable written after it
// started.
func writeLiveEnvFile(t *testing.T, env map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env.json")
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal live env: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write live env: %v", err)
	}
	return path
}

// assertNoSecretOnDisk walks everything ZCP wrote under stateDir and fails if
// secret is in any of it. A git credential's one home is a sensitive service
// env on the push source, written by git-push-setup and read by the
// credential helper from the session environment; a copy in a state file
// would outlive every rotation.
func assertNoSecretOnDisk(t *testing.T, stateDir, secret string) {
	t.Helper()
	err := filepath.WalkDir(stateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(body), secret) {
			t.Errorf("a secret leaked into %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk state dir: %v", err)
	}
}
