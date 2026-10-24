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

// GitCredential writes the Git credential the Mate's server would hold from
// HQ, where zcp reads it (hq.GitCredentialPath, under the test's HOME).
func GitCredential(t *testing.T, credential string) {
	t.Helper()
	path := hq.GitCredentialPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(credential), 0o600); err != nil {
		t.Fatal(err)
	}
}
