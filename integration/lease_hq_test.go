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

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

const (
	leaseIntHQ           = "https://hq.example.invalid"
	leaseIntHQRepo       = leaseIntHQ + "/git/a1/web"
	leaseIntHQCredential = "the-mate-credential-0001"
)

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
// enrolled with its HQ, a lease of a repository on it is checked out as HQ's
// git user `mate` — the fetch and the helper the repository keeps under HQ's
// scope — and a git-push deploy from that lease is refused with HQ's branch
// rule. Neither result carries the Mate credential.
func TestLease_Integration_HQRepo_ChecksOutAsMateAndPushesByHQsRule(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir), t.Setenv (HOME holds the
	// enrollment) and a swap of http.DefaultTransport (the orchestrator).
	t.Chdir(t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: leaseIntHQ, ProjectID: "proj-1", Credential: leaseIntHQCredential}); err != nil {
		t.Fatal(err)
	}
	routeLeaseOrchestrator(t)

	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-1", Name: "app", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	ssh := &stageLeaseSSH{}
	session, cleanup := setupLeaseTestServer(t, mock, defaultLogFetcher(), ssh)
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
	case !strings.Contains(checkout.command, "echo username=mate;") || strings.Contains(checkout.command, "username=oauth2"):
		t.Errorf("the checkout of a repository on HQ does not authenticate as mate:\n%s", checkout.command)
	case !strings.Contains(checkout.command, "credential.https://hq.example.invalid.helper"):
		t.Errorf("the checkout keeps no helper under HQ's scope:\n%s", checkout.command)
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
		if strings.Contains(out, leaseIntHQCredential) || strings.Contains(out, leaseIntLease) {
			t.Errorf("a secret reached %s", out)
		}
	}
}
