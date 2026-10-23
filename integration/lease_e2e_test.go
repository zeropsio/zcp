// Tests for: integration — what the live end-to-end run of dev-pool leases in a
// Mate found, through the full MCP server against a mock platform and a fake
// pool orchestrator (docs/spec-workflows.md §8 DP-2, DP-17…).

package integration_test

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
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
// answers with "repository not found" (DP-17).
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
// forwards to another container: no "not HTTP-ready" warning about it (DP-18).
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
