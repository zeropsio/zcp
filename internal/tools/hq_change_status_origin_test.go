// Tests for: tools/hq_change_status.go — status's fresh read of the Mate's
// own state reaches the HQ the container presents itself to, the one the
// installed Mate declares among them (docs/spec-mate.md §3.5).
package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestStatusHQChanges_ReadsTheHQTheInstalledMateDeclares: status names each
// change the ledger keeps by what the Mate's state says of it now, read fresh
// from the HQ the container presents itself to — with no origin in its
// environment, the one the installed Mate declares (mate.HQIdentity) — under
// the project key. A change merged since the last pass reads merged, never as
// last read.
func TestStatusHQChanges_ReadsTheHQTheInstalledMateDeclares(t *testing.T) {
	// Non-parallel: HOME and the identity's variables are process-wide.
	t.Setenv("HOME", t.TempDir())
	t.Setenv(hq.EnvOrigin, "")
	t.Setenv(hq.EnvKey, "the-project-key")
	t.Setenv(hq.EnvProjectID, labMate)
	var (
		mu    sync.Mutex
		asked []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.Header.Get("Authorization")+" "+r.Method+" "+r.URL.Path)
		mu.Unlock()
		writeHQJSON(w, http.StatusOK, map[string]any{
			"projectId": labMate, "name": "Ada", "face": "f", "appId": labApp,
			"changes": []map[string]any{{"repo": "appsdev", "number": 2, "state": hq.ChangeMerged, "branch": "pool-previews",
				"head": nil, "mergedSha": "5a0d9c1", "landedHead": "9f1e2b3"}},
		})
	}))
	t.Cleanup(srv.Close)
	pkg := filepath.Join(mate.CurrentLink(), "node_modules", mate.PackageName, "package.json")
	if err := os.MkdirAll(filepath.Dir(pkg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pkg, []byte(`{"version":"0.16.0","mate":{"hqOrigin":"`+srv.URL+`/"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := workflow.RecordHQChange(stateDir, workflow.HQChange{AppID: labApp, Repo: "appsdev", Number: 2, Branch: "pool-previews"}); err != nil {
		t.Fatal(err)
	}

	changes, asLastRead := statusHQChanges(context.Background(), srv.Client(), runtime.Info{InContainer: true, MateEnabled: true}, stateDir, srv.URL, time.Now())

	if asLastRead {
		t.Errorf("status named the ledger's changes as last read: the HQ the installed Mate declares was not read (asked %v)", asked)
	}
	if len(changes) != 1 || len(changes[0].Changes) != 1 || !changes[0].Changes[0].Merged {
		t.Errorf("changes = %+v, want appsdev #2 merged", changes)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := "Project the-project-key GET /api/mate/self"; len(asked) != 1 || asked[0] != want {
		t.Errorf("HQ was asked %v, want once: %s", asked, want)
	}
}
