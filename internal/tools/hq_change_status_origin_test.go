// Tests for: tools/hq_change_status.go — status reads the state kept for this Mate.
package tools

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestStatusHQChanges_ReadsTheHQTheInstalledMateDeclares(t *testing.T) {
	// non-parallel: HOME and the identity's variables are process-wide.
	t.Setenv("HOME", t.TempDir())
	t.Setenv(hq.EnvProjectID, labMate)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(hq.EnvOrigin, srv.URL)
	t.Setenv(hq.EnvKey, "the-project-key")
	path := hq.MateStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"link":"linked","mate":{"rev":2,"projectId":"p-mate","appId":"app-1","repos":[],"chief":null,"changes":[{"repo":"appsdev","number":2,"state":"merged","branch":"pool-previews","mergedSha":"5a0d9c1"}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()
	if err := workflow.RecordHQChange(stateDir, workflow.HQChange{AppID: labApp, Repo: "appsdev", Number: 2, Branch: "pool-previews"}); err != nil {
		t.Fatal(err)
	}
	changes, asLastRead, problems, standing, state := statusHQChanges(runtime.Info{InContainer: true, MateEnabled: true}, stateDir, srv.URL, time.Now(), true)
	if asLastRead || len(problems) != 0 || standing == "" || state == nil || len(changes) != 1 || len(changes[0].Changes) != 1 || !changes[0].Changes[0].Merged {
		t.Errorf("changes = %+v, last read = %v, want appsdev #2 merged", changes, asLastRead)
	}
	if requests != 0 {
		t.Errorf("status asked HQ %d times, want none", requests)
	}
}
