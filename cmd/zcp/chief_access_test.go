// Tests for: cmd/zcp/main.go chiefAccess — `zcp serve` reads the Chief it answers to
// from the Mate's state at start (docs/spec-mate.md §2.11).
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestChiefAccess_TheChiefHQNamesDecidesWhetherThisProjectServesOrgMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       string
		wantChief  bool
		wantStderr string
	}{
		{name: "a Chief naming this project is the Chief", body: `{"link":"linked","mate":{"rev":1,"projectId":"p-me","appId":null,"repos":[],"changes":[],"chief":{"projectId":"p-me"}}}`, wantChief: true},
		{name: "a Chief naming another project is no Chief", body: `{"link":"linked","mate":{"rev":1,"projectId":"p-me","appId":null,"repos":[],"changes":[],"chief":{"projectId":"p-other"}}}`},
		{name: "no Chief named is no Chief", body: `{"link":"linked","mate":{"rev":1,"projectId":"p-me","appId":null,"repos":[],"changes":[],"chief":null}}`},
		{name: "an unreadable state is no Chief, and says so", body: `{}`, wantStderr: "serving the single-project surface"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			release, err := hq.LockMateState(filepath.Join(dir, "state.lock"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(release)
			id := hq.Identity{Key: "project-key", ProjectID: "p-me", State: hq.NewFileMateStateSource(path, "p-me")}
			var stderr bytes.Buffer

			access, chief := chiefAccess(id, "api.example", &stderr)

			if chief != tt.wantChief {
				t.Fatalf("chief = %v, want %v (stderr %q)", chief, tt.wantChief, stderr.String())
			}
			if chief && (access != platform.OrgAccess{APIHost: "api.example", ProjectID: "p-me", Token: "project-key"}) {
				t.Errorf("access = %+v, want the Zerops API, this project and the project key", access)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) || (tt.wantStderr == "" && stderr.Len() != 0) {
				t.Errorf("stderr = %q, want it to hold %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestChiefAccess_ANoIdentityContainerIsNoChiefAndSaysNothing(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	if _, chief := chiefAccess(hq.Identity{}, "", &stderr); chief || stderr.Len() != 0 {
		t.Errorf("chief = %v, stderr = %q; want neither", chief, stderr.String())
	}
}
