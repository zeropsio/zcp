// Package hqtest makes a test's container a Mate linked to a fake HQ: the
// environment a Mate's zcp reads its identity from, and the Git credential
// file its server keeps.
package hqtest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
)

// Link gives the test's container the identity of project projectID at the
// HQ at origin, presenting key. It sets the process environment through
// t.Setenv, so the test is not parallel.
func Link(t *testing.T, origin, projectID, key string) {
	t.Helper()
	t.Setenv(hq.EnvOrigin, origin)
	t.Setenv(hq.EnvProjectID, projectID)
	t.Setenv(hq.EnvKey, key)
}

// State writes a linked server's literal Mate state for a non-parallel test.
func State(t *testing.T, mate string) {
	t.Helper()
	path := hq.MateStatePath()
	release, err := hq.LockMateState(hq.MateStateLockPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	if err := os.WriteFile(path, []byte(`{"link":"linked","mate":`+mate+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

// GitCredential writes a credential the Mate's server would hold to the file
// at path, mode 0600, as the server does.
func GitCredential(t *testing.T, path, credential string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(credential), 0o600); err != nil {
		t.Fatal(err)
	}
}
