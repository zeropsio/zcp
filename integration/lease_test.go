// Tests for: integration — zerops_lease through the full MCP server, against
// a mock platform and a fake pool orchestrator.

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

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	leaseIntToken = "orch-api-token-0001"
	leaseIntLease = "0123456789abcdef0123456789abcdef"
)

// orchestratorRoute sends every request for host to target, leaving other
// traffic on the original transport. The server builds its orchestrator
// client over the default transport, so swapping it is the only seam the real
// wiring offers.
type orchestratorRoute struct {
	host   string
	target *url.URL
	next   http.RoundTripper
}

func (o orchestratorRoute) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != o.host {
		return o.next.RoundTrip(r)
	}
	c := r.Clone(r.Context())
	c.URL.Scheme, c.URL.Host = o.target.Scheme, o.target.Host
	return o.next.RoundTrip(c)
}

func TestLease_Integration_AcquireStatusRelease(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir the server resolves from the cwd)
	// and a swap of http.DefaultTransport (the seam to the orchestrator host).
	t.Chdir(t.TempDir())

	var mu sync.Mutex
	reserved, resetOnRelease := false, ""
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"code":"notAuthorized","message":"missing or wrong API token"}}`))
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
		case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
			reserved = true
			_, _ = w.Write([]byte(strings.Replace(container, `"lockState":""`, `"lockState":"reserved"`, 1) + `,"lease":"` + leaseIntLease + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/release" && r.Header.Get("X-Swarm-Lease") == leaseIntLease && !r.URL.Query().Has("lease"):
			reserved, resetOnRelease = false, r.URL.Query().Get("reset")
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
			{ID: "svc-1", Name: "app", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	type leaseResult struct {
		Action string `json:"action"`
		Leases []struct {
			Alias             string `json:"alias"`
			Pool              string `json:"pool"`
			ContainerHostname string `json:"containerHostname"`
			Branch            string `json:"branch"`
			State             string `json:"state"`
		} `json:"leases"`
	}
	call := func(args map[string]any) leaseResult {
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
		var out leaseResult
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("result is not JSON: %v\n%s", err, text)
		}
		return out
	}

	got := call(map[string]any{"action": "acquire", "pool": "devpool", "branch": "feature-x"})
	if len(got.Leases) != 1 || got.Leases[0].Alias != "devpool-3" || got.Leases[0].State != "held" ||
		got.Leases[0].ContainerHostname != "node-id-3.runtime.devpool.zerops" || got.Leases[0].Branch != "feature-x" {
		t.Fatalf("acquire = %+v, want held devpool-3 on node-id-3.runtime.devpool.zerops for feature-x", got.Leases)
	}
	recPath := filepath.Join(".zcp", "state", "leases", "devpool-3.json")
	info, err := os.Stat(recPath)
	if err != nil {
		t.Fatalf("lease record missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("record mode = %o, want 600", info.Mode().Perm())
	}

	got = call(map[string]any{"action": "status"})
	if len(got.Leases) != 1 || got.Leases[0].Alias != "devpool-3" || got.Leases[0].State != "held" {
		t.Fatalf("status = %+v, want the one held devpool-3", got.Leases)
	}

	got = call(map[string]any{"action": "release", "lease": "devpool-3"})
	if len(got.Leases) != 1 || got.Leases[0].State != "released" {
		t.Fatalf("release = %+v, want devpool-3 released", got.Leases)
	}
	mu.Lock()
	gotReset := resetOnRelease
	mu.Unlock()
	if gotReset != "true" {
		t.Errorf("release reset = %q, want true (the default)", gotReset)
	}
	if _, err := os.Stat(recPath); !os.IsNotExist(err) {
		t.Errorf("record still present after release: %v", err)
	}

	got = call(map[string]any{"action": "status"})
	if len(got.Leases) != 0 {
		t.Errorf("status after release = %+v, want none", got.Leases)
	}
}
