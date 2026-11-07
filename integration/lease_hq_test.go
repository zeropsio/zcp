// Tests for: integration — zerops_lease and zerops_deploy for a lease of a
// repository on the Mate's HQ, through the full MCP server
// (docs/spec-workflows.md §8 DP-12).

package integration_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

const (
	leaseIntHQ     = "https://hq.example.invalid"
	leaseIntHQRepo = leaseIntHQ + "/git/org-1/a1/web"
	leaseIntHQKey  = "the-project-key-0001"
)

// leaseIntHQLink links the test's container to the HQ at leaseIntHQ, the way
// a Mate's container presents itself to its HQ: its identity, the project
// key leaseIntHQKey among it.
func leaseIntHQLink(t *testing.T) *runtime.HQLink {
	t.Helper()
	return &runtime.HQLink{Origin: leaseIntHQ, ProjectID: "proj-1", Key: leaseIntHQKey}
}

// routeLeaseOrchestrator answers devpool's orchestrator with one container
// that an acquire reserves.
func routeLeaseOrchestrator(t *testing.T) {
	t.Helper()
	var mu sync.Mutex
	reserved := false
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
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/renew"):
			if !reserved || r.Header.Get("X-Swarm-Lease") != leaseIntLease {
				writeIntUnknownLease(w)
				return
			}
			_, _ = w.Write([]byte(container + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
			reserved = true
			_, _ = w.Write([]byte(strings.Replace(container, `"lockState":""`, `"lockState":"reserved"`, 1) + `,"lease":"` + leaseIntLease + `"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(orch.Close)
	target, err := url.Parse(orch.URL)
	if err != nil {
		t.Fatal(err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoute{host: "orch.devpool.zerops", target: target, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })
}

// TestLease_Integration_HQRepo_ChecksOutAsMateAndPushesByHQsRule: in a Mate
// linked to its HQ, a lease of a repository on it is checked out as HQ's
// git user `mate` — the relayed session's helper answers the fetch with the
// Mate's project key, so the checkout names no helper of its own and keeps
// none — and a git-push deploy from that lease is refused with HQ's branch
// rule. Neither result carries the project key.
func TestLease_Integration_HQRepo_ChecksOutAsMateAndPushesByHQsRule(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir) and a swap of
	// http.DefaultTransport (the orchestrator).
	t.Chdir(t.TempDir())
	routeLeaseOrchestrator(t)

	mock := defaultMock(t).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-1", Name: "app", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	ssh := &stageLeaseSSH{}
	session, cleanup := setupTestServerIn(t, mock, defaultLogFetcher(), ssh, runtime.Info{InContainer: true, HQ: leaseIntHQLink(t)})
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseIntHQRepo + ".git", "branch": "feat"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire: %s", text)
	}
	at, checkout := ssh.first("fetch -q --prune origin")
	switch {
	case at < 0:
		t.Fatal("the checkout did not run")
	case checkout.hostname != "devpool-3":
		t.Errorf("the checkout ran on %q, want the alias devpool-3", checkout.hostname)
	case strings.Contains(checkout.command, "credential."):
		t.Errorf("the checkout of a repository on HQ names a helper of its own, which would clear the relayed session's:\n%s", checkout.command)
	}

	res = callAndGetResult(t, session, "zerops_deploy", map[string]any{"sourceService": "devpool-3", "targetService": "app", "strategy": "git-push"})
	refusal := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("a git-push from a lease ran: %s", refusal)
	}
	for _, want := range []string{"branch of your own", `never push \"main\"`, "never force-push"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("refusal %s lacks %q", refusal, want)
		}
	}
	for _, out := range []string{text, refusal, checkout.command} {
		if strings.Contains(out, leaseIntHQKey) || strings.Contains(out, leaseIntLease) {
			t.Errorf("a secret reached %s", out)
		}
	}
}
