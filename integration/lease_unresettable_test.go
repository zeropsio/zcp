// Tests for: integration — a container the platform created for the pool's
// minContainers cannot be reset (no-primary-snapshot), so zerops_lease deletes
// it with its lease instead of releasing it unreset (docs/spec-workflows.md §8
// DP-15), through the full MCP server against a mock platform and a fake pool
// orchestrator.

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
)

func TestLease_Integration_UnresettableContainerIsDeletedNotReleasedUnreset(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir the server resolves from the cwd)
	// and a swap of http.DefaultTransport (the seam to the orchestrator host).
	t.Chdir(t.TempDir())
	var mu sync.Mutex
	reserved, unresetRelease, deleted := false, false, ""
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lock := ""
		if reserved {
			lock = "reserved"
		}
		container := `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE",` +
			`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + lock + `","dirty":false`
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/container":
			_, _ = w.Write([]byte(`[` + container + `}]`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/container/"):
			_, _ = w.Write([]byte(container + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
			reserved = true
			_, _ = w.Write([]byte(strings.Replace(container, `"lockState":""`, `"lockState":"reserved"`, 1) + `,"lease":"` + leaseIntLease + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/release" && r.URL.Query().Get("reset") == "true":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"container has no primary snapshot, so it cannot be reset","meta":[{"metadata":{"reason":["no-primary-snapshot"],"retryable":["false"]}}]}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/release":
			unresetRelease = true
			_, _ = w.Write([]byte(container + `}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/container/c-abc" && r.Header.Get("X-Swarm-Lease") == leaseIntLease && r.URL.RawQuery == "":
			reserved, deleted = false, "c-abc"
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer orch.Close()
	target, err := url.Parse(orch.URL)
	if err != nil {
		t.Fatal(err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoute{host: "orch.devpool.zerops", target: target, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	c := devservertest.NewSupervised("")
	session, cleanup := setupTestServerWithDeploy(t, supervisorPoolMock(), defaultLogFetcher(), c)
	defer cleanup()

	call := func(args map[string]any) supervisorLeaseResult {
		t.Helper()
		res := callAndGetResult(t, session, "zerops_lease", args)
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("zerops_lease %v failed: %s", args["action"], text)
		}
		for _, secret := range []string{leaseIntLease, leaseIntToken} {
			if strings.Contains(text, secret) {
				t.Fatalf("zerops_lease %v result carries a secret: %s", args["action"], text)
			}
		}
		var out supervisorLeaseResult
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("result is not JSON: %v\n%s", err, text)
		}
		return out
	}
	call(map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app"})
	got := call(map[string]any{"action": "release", "lease": "devpool-3", "force": true})
	if len(got.Leases) != 1 || got.Leases[0].State != "released" {
		t.Fatalf("release = %+v, want devpool-3 released", got.Leases)
	}
	for _, want := range []string{"could not be reset", "deleted", "minContainers"} {
		if !strings.Contains(got.Leases[0].Note, want) {
			t.Errorf("note %q lacks %q", got.Leases[0].Note, want)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if unresetRelease {
		t.Error("the container was released without a reset")
	}
	if deleted != "c-abc" {
		t.Errorf("the unresettable container was not deleted with its lease")
	}
	if _, err := os.Stat(filepath.Join(".zcp", "state", "leases", "devpool-3.json")); !os.IsNotExist(err) {
		t.Errorf("the record survives the deleted container: %v", err)
	}
}
