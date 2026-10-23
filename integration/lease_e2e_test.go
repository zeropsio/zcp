// Tests for: integration — what the live end-to-end run of dev-pool leases in a
// Mate found, through the full MCP server against a mock platform and a fake
// pool orchestrator (docs/spec-workflows.md §8 DP-2, DP-18, DP-19).

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/server"
)

// leaseE2EPoolMock is a project holding the pool devpool, its API_TOKEN set.
func leaseE2EPoolMock() *platform.Mock {
	return defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
}

// TestLease_Integration_RepoAsGiven_ClonedVerbatim: an acquire of `…/repo.git`
// clones that URL, never its canonical `…/repo`, which a plain git server
// answers with "repository not found" (DP-18).
func TestLease_Integration_RepoAsGiven_ClonedVerbatim(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir) and a swap of
	// http.DefaultTransport (the orchestrator).
	t.Chdir(t.TempDir())
	routeLeaseOrchestrator(t)
	ssh := &stageLeaseSSH{}
	session, cleanup := setupTestServerWithDeploy(t, leaseE2EPoolMock(), defaultLogFetcher(), ssh)
	defer cleanup()

	const given = "https://git.example.com/team/repo.git"
	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": given, "branch": "feat"})
	if text := getTextContent(t, res); res.IsError {
		t.Fatalf("acquire: %s", text)
	}
	at, checkout := ssh.first("fetch -q --prune origin")
	if at < 0 {
		t.Fatal("the checkout did not run")
	}
	if !strings.Contains(checkout.command, "git remote add origin '"+given+"'") {
		t.Errorf("the checkout does not clone %s as given:\n%s", given, checkout.command)
	}
	if strings.Contains(checkout.command, "'https://git.example.com/team/repo'") {
		t.Errorf("the checkout clones the canonical form:\n%s", checkout.command)
	}
}

// writeLeaseE2ERecord records lease devpool-3 the way zerops_lease does, in the
// state directory the server resolves from the cwd.
func writeLeaseE2ERecord(t *testing.T) {
	t.Helper()
	dir := filepath.Join(".zcp", "state", "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := `{"poolHostname":"devpool","poolServiceId":"svc-pool","containerId":"c-abc","containerNumber":3,` +
		`"containerHostname":"node-id-3.runtime.devpool.zerops","lease":"` + leaseIntLease + `","branch":"feat","acquiredAt":"2026-10-23T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "devpool-3.json"), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
}

// previewRoute answers the pool's per-port subdomains itself: 200 at ok, 502
// at every other port of the pool (no server forwards there for this lease),
// recording each address asked. Other traffic stays on next.
type previewRoute struct {
	mu    *sync.Mutex
	ok    string
	asked *[]string
	next  http.RoundTripper
}

func (p previewRoute) RoundTrip(r *http.Request) (*http.Response, error) {
	if !strings.HasPrefix(r.URL.Host, "devpool-abc1-") {
		return p.next.RoundTrip(r)
	}
	p.mu.Lock()
	*p.asked = append(*p.asked, "https://"+r.URL.Host)
	p.mu.Unlock()
	status := http.StatusBadGateway
	if "https://"+r.URL.Host == p.ok {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
}

// TestLease_Integration_DevServerStart_ProbesTheLeasesOwnPreview: a start on a
// lease whose preview port is 9003 probes the pool's subdomain on 9003 — the
// previewUrl it reports — and never the pool's first declared port, which
// forwards to another container: no "not HTTP-ready" warning about it (DP-19).
func TestLease_Integration_DevServerStart_ProbesTheLeasesOwnPreview(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir), a swap of http.DefaultTransport
	// (the subdomains) and the package-level readiness timing.
	t.Chdir(t.TempDir())
	restore := ops.OverrideHTTPReadyConfigForTest(time.Millisecond, 200*time.Millisecond)
	defer restore()
	writeLeaseE2ERecord(t)

	const preview = "https://devpool-abc1-9003.prg1.zerops.app"
	var mu sync.Mutex
	var asked []string
	orig := http.DefaultTransport
	http.DefaultTransport = previewRoute{mu: &mu, ok: preview, asked: &asked, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	pool := platform.ServiceStack{
		ID: "svc-pool", Name: "devpool", ProjectID: "proj-1", Status: "ACTIVE", SubdomainAccess: true,
		Ports: []platform.Port{
			{Port: 9001, Protocol: "tcp", HTTPSupport: true}, {Port: 9002, Protocol: "tcp", HTTPSupport: true},
			{Port: 9003, Protocol: "tcp", HTTPSupport: true},
		},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "swarm@1"},
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test-project", Status: "ACTIVE", SubdomainHost: "abc1.prg1.zerops.app"}).
		WithServices([]platform.ServiceStack{pool}).
		WithService(&pool)
	c := devservertest.NewSupervised(`{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`)
	c.Servers["web"].PreviewPort = 9003
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), c)
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_dev_server", map[string]any{"action": "start", "hostname": "devpool-3"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("start: %s", text)
	}
	var got struct {
		PreviewURL string   `json:"previewUrl"`
		Warnings   []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if got.PreviewURL != preview {
		t.Errorf("previewUrl = %q, want %q", got.PreviewURL, preview)
	}
	for _, w := range got.Warnings {
		if strings.Contains(w, "HTTP-ready") {
			t.Errorf("warning about another address: %s", w)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	seen := map[string]bool{}
	for _, u := range asked {
		seen[u] = true
	}
	if !seen[preview] || len(seen) != 1 {
		t.Errorf("probed %v, want only the lease's own address %s", slices.Sorted(maps.Keys(seen)), preview)
	}
}

// routeReservedOrchestrator answers devpool's orchestrator with container #3
// reserved — the lease writeLeaseE2ERecord records.
func routeReservedOrchestrator(t *testing.T) {
	t.Helper()
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/container" {
			_, _ = w.Write([]byte(`[{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE",` +
				`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"reserved","dirty":false}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
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

// TestLease_Integration_UnmountedLease_EnvelopeSaysWhatStatusSays: in a
// container whose mount of a held lease is gone, zerops_lease action=status
// reads it `unmounted`, and so does the envelope's leases[] — the same state,
// never `held` (DP-2, docs/spec-mate.md §2.8a.3).
func TestLease_Integration_UnmountedLease_EnvelopeSaysWhatStatusSays(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir) and a swap of
	// http.DefaultTransport (the orchestrator).
	t.Chdir(t.TempDir())
	routeReservedOrchestrator(t)
	writeLeaseE2ERecord(t)

	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("knowledge store: %v", err)
	}
	authInfo := &auth.Info{ProjectID: "proj-1", Token: "test-token", APIHost: "localhost", Region: "prg1", ClientID: "client-1", ProjectName: "test-project"}
	// nopMounter finds nothing mounted: /var/www/devpool-3 is gone.
	srv := server.New(context.Background(), leaseE2EPoolMock(), authInfo, store, defaultLogFetcher(), nil, &nopMounter{}, runtime.Info{}, nil)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.MCPServer().Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "integration-test", Version: "0.1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()

	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "status"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status: %s", text)
	}
	var got struct {
		Leases []struct {
			Alias string `json:"alias"`
			State string `json:"state"`
		} `json:"leases"`
		Envelope struct {
			Leases []struct {
				Alias string `json:"alias"`
				State string `json:"state"`
			} `json:"leases"`
		} `json:"envelope"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(got.Leases) != 1 || got.Leases[0].State != "unmounted" {
		t.Fatalf("status leases = %+v, want devpool-3 unmounted", got.Leases)
	}
	if len(got.Envelope.Leases) != 1 || got.Envelope.Leases[0].Alias != "devpool-3" || got.Envelope.Leases[0].State != "unmounted" {
		t.Errorf("envelope leases = %+v, want devpool-3 unmounted as status reports it", got.Envelope.Leases)
	}
}
