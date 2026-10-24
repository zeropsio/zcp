// Tests for: integration — zerops_lease through the full MCP server once the
// container a lease named is another holder's (docs/spec-workflows.md §8
// DP-2), against a mock platform and a fake pool orchestrator.

package integration_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestLease_Integration_LeaseReleasedOutsideAndRetaken_DroppedAsLost: the
// live 4g sequence. zcp acquires devpool-3; the lease is released outside zcp
// and another consumer reserves the container (its lockSince later than the
// one zcp recorded). Status says lost, naming another holder; a plain release
// drops the record as lost without a release call to the orchestrator; the
// next status reports the container as reserved by a holder zcp has no record
// of, and nothing ever carries a secret.
func TestLease_Integration_LeaseReleasedOutsideAndRetaken_DroppedAsLost(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir the server resolves from the cwd)
	// and a swap of http.DefaultTransport (the seam to the orchestrator host).
	t.Chdir(t.TempDir())

	const otherLease = "another-consumers-lease-0123456789"
	var mu sync.Mutex
	lockState, lockSince, holder := "", "", ""
	releases := 0
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		since := "null"
		if lockSince != "" {
			since = `"` + lockSince + `"`
		}
		container := `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE",` +
			`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + lockState + `","lockSince":` + since + `,"dirty":false`
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/container":
			_, _ = w.Write([]byte(`[` + container + `}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/container/c-abc":
			_, _ = w.Write([]byte(container + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
			lockState, lockSince, holder = "reserved", "2026-10-24T11:01:00.88865963Z", leaseIntLease
			_, _ = w.Write([]byte(`{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE","serviceId":"svc-pool",` +
				`"current":true,"lockState":"reserved","lockSince":"` + lockSince + `","dirty":false,"lease":"` + leaseIntLease + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/release":
			releases++
			if lockState == "reserved" && r.Header.Get("X-Swarm-Lease") != holder {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"container is reserved by another consumer - pass its lease, or ?force=true to take it over","meta":[{"metadata":{"reason":["held-by-other"],"retryable":["true"]}}]}}`))
				return
			}
			lockState, lockSince, holder = "", "", ""
			_, _ = w.Write([]byte(container + `}`))
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

	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	type view struct {
		Alias string `json:"alias"`
		State string `json:"state"`
		Note  string `json:"note"`
	}
	call := func(args map[string]any) []view {
		t.Helper()
		res := callAndGetResult(t, session, "zerops_lease", args)
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("zerops_lease %v failed: %s", args["action"], text)
		}
		for _, secret := range []string{leaseIntLease, leaseIntToken, otherLease} {
			if strings.Contains(text, secret) {
				t.Fatalf("zerops_lease %v result carries a secret: %s", args["action"], text)
			}
		}
		var out struct {
			Leases []view `json:"leases"`
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("result is not JSON: %v\n%s", err, text)
		}
		return out.Leases
	}

	if got := call(map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "lv-bogus"}); len(got) != 1 || got[0].State != "held" {
		t.Fatalf("acquire = %+v, want devpool-3 held", got)
	}
	mu.Lock()
	lockState, lockSince, holder = "reserved", "2026-10-24T11:02:04.783475573Z", otherLease
	mu.Unlock()

	got := call(map[string]any{"action": "status", "pool": "devpool"})
	if len(got) != 1 || got[0].Alias != "devpool-3" || got[0].State != "lost" || !strings.Contains(got[0].Note, "another holder") {
		t.Fatalf("status = %+v, want devpool-3 lost, naming another holder", got)
	}
	got = call(map[string]any{"action": "release", "lease": "devpool-3"})
	if len(got) != 1 || got[0].State != "lost" || !strings.Contains(got[0].Note, "record is dropped") {
		t.Fatalf("release = %+v, want devpool-3 lost, its record dropped", got)
	}
	mu.Lock()
	if releases != 0 {
		t.Errorf("orchestrator releases = %d, want none: the container is another holder's", releases)
	}
	mu.Unlock()
	if _, err := os.Stat(filepath.Join(".zcp", "state", "leases", "devpool-3.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("record survives the release: %v", err)
	}
	if got := call(map[string]any{"action": "status", "pool": "devpool"}); len(got) != 1 || got[0].State != "unknown-holder" {
		t.Errorf("status after = %+v, want devpool-3 an unknown holder's", got)
	}
}
