// Tests for: tools/lease.go — zerops_lease MCP tool handler.
package tools

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

const (
	leaseTestToken = "orch-api-token-0001"
	leaseTestLease = "0123456789abcdef0123456789abcdef"
)

// fakeOrchestrator is a pool orchestrator speaking the live wire shapes: one
// container (#3), bearer-checked, lease-checked on the header.
type fakeOrchestrator struct {
	mu        sync.Mutex
	reserved  bool
	echoLease bool // refuse a release, echoing the lease in the message
}

func (o *fakeOrchestrator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+leaseTestToken {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"notAuthorized","message":"missing or wrong API token"}}`))
		return
	}
	state := func() string {
		if o.reserved {
			return "reserved"
		}
		return ""
	}
	container := func(withLease bool) string {
		c := `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE",` +
			`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + state() + `","dirty":false`
		if withLease {
			c += `,"lease":"` + leaseTestLease + `"`
		}
		return c + `}`
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/container":
		_, _ = w.Write([]byte(`[` + container(false) + `]`))
	case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
		o.reserved = true
		_, _ = w.Write([]byte(container(true)))
	case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/release":
		if r.Header.Get("X-Swarm-Lease") != leaseTestLease || r.URL.Query().Has("lease") {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"nope","meta":[{"metadata":{"reason":["unknown-lease"],"retryable":["false"]}}]}}`))
			return
		}
		if o.echoLease {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"container busy for lease ` + leaseTestLease + `","meta":[{"metadata":{"reason":["container-busy"],"retryable":["true"]}}]}}`))
			return
		}
		o.reserved = false
		_, _ = w.Write([]byte(container(false)))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// leaseToolServer is a project holding one swarm pool, an orchestrator behind
// the pool's factory, and a state dir.
func leaseToolServer(t *testing.T, orch http.Handler) (srv *mcp.Server, stateDir string) {
	t.Helper()
	hs := httptest.NewServer(orch)
	t.Cleanup(hs.Close)
	stateDir = t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}})
	factory := func(_, token string) ops.SwarmAPI { return platform.NewSwarmClient(hs.URL, token, hs.Client()) }
	srv = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLease(srv, mock, "proj-1", stateDir, factory)
	return srv, stateDir
}

func TestLeaseTool_ResultNeverCarriesLease(t *testing.T) {
	t.Parallel()
	srv, stateDir := leaseToolServer(t, &fakeOrchestrator{})

	acquire := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "branch": "feature-x"})
	status := callTool(t, srv, "zerops_lease", map[string]any{"action": "status"})
	release := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "lease": "devpool-3"})

	// The release only succeeds with the real lease in the header (the fake
	// refuses otherwise), so the lease reached the orchestrator; the record
	// that held it is gone afterwards.
	if _, err := os.Stat(filepath.Join(stateDir, "leases", "devpool-3.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("record should be gone after release: %v", err)
	}
	for name, res := range map[string]*mcp.CallToolResult{"acquire": acquire, "status": status, "release": release} {
		text := getTextContent(t, res)
		if res.IsError {
			t.Errorf("%s: unexpected error: %s", name, text)
		}
		for _, secret := range []string{leaseTestLease, leaseTestToken} {
			if strings.Contains(text, secret) {
				t.Errorf("%s result carries a secret: %s", name, text)
			}
		}
	}
	var got struct {
		Leases []ops.LeaseView `json:"leases"`
	}
	if err := json.Unmarshal([]byte(getTextContent(t, acquire)), &got); err != nil {
		t.Fatalf("acquire result is not JSON: %v", err)
	}
	if len(got.Leases) != 1 || got.Leases[0].Alias != "devpool-3" || got.Leases[0].ContainerHostname != "node-id-3.runtime.devpool.zerops" ||
		got.Leases[0].State != ops.LeaseStateHeld || got.Leases[0].Branch != "feature-x" {
		t.Errorf("acquire leases = %+v, want held devpool-3 on node-id-3.runtime.devpool.zerops for feature-x", got.Leases)
	}
	if status := getTextContent(t, status); !strings.Contains(status, `"devpool-3"`) || !strings.Contains(status, `"held"`) {
		t.Errorf("status = %s, want devpool-3 held", status)
	}
	if rel := getTextContent(t, release); !strings.Contains(rel, `"released"`) {
		t.Errorf("release = %s, want state released", rel)
	}
}

func TestLeaseTool_RefusalNeverCarriesLease(t *testing.T) {
	t.Parallel()
	srv, _ := leaseToolServer(t, &fakeOrchestrator{echoLease: true})
	callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool"})

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "lease": "devpool-3"})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("want a refusal (container busy), got: %s", text)
	}
	if strings.Contains(text, leaseTestLease) || strings.Contains(text, leaseTestToken) {
		t.Errorf("refusal carries a secret: %s", text)
	}
	if !strings.Contains(text, "busy") {
		t.Errorf("refusal %s should say why", text)
	}
}

// dnsFailTransport fails every request the way a resolver does outside the
// project, where orch.<pool>.zerops does not resolve.
type dnsFailTransport struct{}

func (dnsFailTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, &net.DNSError{Err: "no such host", Name: r.URL.Hostname(), IsNotFound: true}
}

func TestLeaseTool_OrchestratorUnreachable_Explains(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLease(srv, mock, "proj-1", stateDir, ops.NewSwarmFactory(&http.Client{Transport: dnsFailTransport{}}))

	for _, args := range []map[string]any{
		{"action": "acquire", "pool": "devpool"},
		{"action": "status", "pool": "devpool"},
	} {
		res := callTool(t, srv, "zerops_lease", args)
		text := getTextContent(t, res)
		if !res.IsError {
			t.Errorf("%v: want an error result, got: %s", args["action"], text)
		}
		for _, want := range []string{"only inside the project", "orch.devpool.zerops", platform.ErrNetworkError} {
			if !strings.Contains(text, want) {
				t.Errorf("%v: result %q does not mention %q", args["action"], text, want)
			}
		}
		for _, banned := range []string{"goroutine", "panic", ".go:", leaseTestToken} {
			if strings.Contains(text, banned) {
				t.Errorf("%v: result carries %q: %s", args["action"], banned, text)
			}
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(stateDir, "leases")); len(entries) != 0 {
		t.Errorf("an unreachable acquire left %d record(s)", len(entries))
	}
}

func TestLeaseTool_BadInput_TypedRefusal(t *testing.T) {
	t.Parallel()
	srv, stateDir := leaseToolServer(t, &fakeOrchestrator{})

	tests := []struct {
		name     string
		args     map[string]any
		wantCode string
		wantText string
	}{
		{"unknown action", map[string]any{"action": "renew"}, platform.ErrInvalidParameter, "acquire, status or release"},
		{"acquire without pool", map[string]any{"action": "acquire"}, platform.ErrInvalidParameter, "needs pool"},
		{"release without lease", map[string]any{"action": "release"}, platform.ErrInvalidParameter, "needs lease"},
		{"release path-shaped alias", map[string]any{"action": "release", "lease": "../../etc/x"}, platform.ErrInvalidParameter, "not a lease alias"},
		{"release unrecorded alias", map[string]any{"action": "release", "lease": "devpool-9"}, platform.ErrInvalidParameter, "No lease record"},
		{"acquire a non-pool", map[string]any{"action": "acquire", "pool": "nosuch"}, platform.ErrServiceNotFound, "nosuch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := callTool(t, srv, "zerops_lease", tt.args)
			text := getTextContent(t, res)
			if !res.IsError || !strings.Contains(text, tt.wantCode) || !strings.Contains(text, tt.wantText) {
				t.Errorf("result = (error %v) %s, want an error with code %s mentioning %q", res.IsError, text, tt.wantCode, tt.wantText)
			}
		})
	}
	if entries, _ := os.ReadDir(filepath.Join(stateDir, "leases")); len(entries) != 0 {
		t.Errorf("bad input left %d lease record(s)", len(entries))
	}
}

func TestLeaseTool_ResetDefaultsTrueAndFlexBool(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		reset     any // absent when nil
		wantQuery string
	}{
		{"omitted resets", nil, "reset=true"},
		{"false does not", false, ""},
		{"string false does not", "false", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var releaseQuery string
			inner := &fakeOrchestrator{}
			srv, _ := leaseToolServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/release") {
					mu.Lock()
					releaseQuery = r.URL.RawQuery
					mu.Unlock()
				}
				inner.ServeHTTP(w, r)
			}))
			callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool"})
			args := map[string]any{"action": "release", "lease": "devpool-3"}
			if tt.reset != nil {
				args["reset"] = tt.reset
			}
			res := callTool(t, srv, "zerops_lease", args)
			if res.IsError {
				t.Fatalf("release failed: %s", getTextContent(t, res))
			}
			mu.Lock()
			defer mu.Unlock()
			if releaseQuery != tt.wantQuery {
				t.Errorf("release query = %q, want %q", releaseQuery, tt.wantQuery)
			}
		})
	}
}
