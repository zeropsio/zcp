// Tests for: tools/lease.go — zerops_lease MCP tool handler.
package tools

import (
	"context"
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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	leaseTestToken = "orch-api-token-0001"
	leaseTestLease = "0123456789abcdef0123456789abcdef"
	leaseTestRepo  = "https://github.com/acme/app"
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
	return leaseToolServerSSH(t, orch, nil)
}

// leaseStubSSH answers the commands zerops_lease runs in a leased container.
type leaseStubSSH struct {
	mu    sync.Mutex
	fn    func(command string) ([]byte, error)
	calls []string
}

func (s *leaseStubSSH) ExecSSH(_ context.Context, _, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, command)
	fn := s.fn
	s.mu.Unlock()
	if fn == nil {
		return nil, nil
	}
	return fn(command)
}

func (s *leaseStubSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *leaseStubSSH) sawCommand(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

// leaseToolServerSSH is leaseToolServer with ssh into the leased containers.
func leaseToolServerSSH(t *testing.T, orch http.Handler, ssh ops.SSHDeployer) (srv *mcp.Server, stateDir string) {
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
	RegisterLease(srv, mock, "proj-1", stateDir, factory, nil, ssh, runtime.Info{})
	return srv, stateDir
}

func TestLeaseTool_ResultNeverCarriesLease(t *testing.T) {
	t.Parallel()
	srv, stateDir := leaseToolServer(t, &fakeOrchestrator{})

	acquire := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feature-x"})
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
	callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo})

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
	RegisterLease(srv, mock, "proj-1", stateDir, ops.NewSwarmFactory(&http.Client{Transport: dnsFailTransport{}}), nil, nil, runtime.Info{})

	for _, args := range []map[string]any{
		{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo},
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
		{"acquire without repo or default", map[string]any{"action": "acquire", "pool": "devpool"}, platform.ErrInvalidParameter, "needs repo"},
		{"acquire with a credential in the repo", map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://u:tok@github.com/a/b"}, platform.ErrInvalidParameter, "clone URL"},
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

func TestLeaseTool_ReleaseForceParam(t *testing.T) {
	t.Parallel()

	t.Run("schema offers force and no reset", func(t *testing.T) {
		t.Parallel()
		srv, _ := leaseToolServer(t, &fakeOrchestrator{})
		ctx := context.Background()
		st, ct := mcp.NewInMemoryTransports()
		ss, err := srv.Connect(ctx, st, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer ss.Close()
		session, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0.1"}, nil).Connect(ctx, ct, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		list, err := session.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range list.Tools {
			if tool.Name != "zerops_lease" {
				continue
			}
			raw, _ := json.Marshal(tool.InputSchema)
			var schema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"action", "pool", "branch", "repo", "lease", "force"} {
				if _, ok := schema.Properties[want]; !ok {
					t.Errorf("schema lacks %q: %s", want, raw)
				}
			}
			if _, ok := schema.Properties["reset"]; ok {
				t.Errorf("schema still offers reset; every release resets: %s", raw)
			}
			return
		}
		t.Fatal("zerops_lease is not registered")
	})

	for _, tt := range []struct {
		name        string
		force       any // absent when nil
		wantRefused bool
	}{
		{"omitted is refused while work is unsaved", nil, true},
		{"false is refused", false, true},
		{"string false is refused", "false", true},
		{"true releases", true, false},
		{"string true releases", "true", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var releaseQuery string
			sawRelease := false
			inner := &fakeOrchestrator{}
			ssh := &leaseStubSSH{fn: func(command string) ([]byte, error) {
				switch {
				case strings.Contains(command, "status --porcelain"):
					return []byte("?? notes.txt\n"), nil
				case strings.Contains(command, "rev-list --count"):
					return []byte("1\n"), nil
				}
				return nil, nil
			}}
			srv, stateDir := leaseToolServerSSH(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/release") {
					mu.Lock()
					releaseQuery, sawRelease = r.URL.RawQuery, true
					mu.Unlock()
				}
				inner.ServeHTTP(w, r)
			}), ssh)
			if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo}); res.IsError {
				t.Fatalf("acquire failed: %s", getTextContent(t, res))
			}
			args := map[string]any{"action": "release", "lease": "devpool-3"}
			if tt.force != nil {
				args["force"] = tt.force
			}
			res := callTool(t, srv, "zerops_lease", args)
			text := getTextContent(t, res)
			mu.Lock()
			defer mu.Unlock()
			_, recErr := os.Stat(filepath.Join(stateDir, "leases", "devpool-3.json"))
			if tt.wantRefused {
				if !res.IsError || !strings.Contains(text, "notes.txt") || !strings.Contains(text, "1 commit") || !strings.Contains(text, "force") {
					t.Errorf("result = (error %v) %s, want a refusal naming notes.txt, the commit and force", res.IsError, text)
				}
				if sawRelease || recErr != nil {
					t.Errorf("a refused release reached the orchestrator (%v) or dropped the record (%v)", sawRelease, recErr)
				}
				return
			}
			if res.IsError {
				t.Fatalf("release failed: %s", text)
			}
			if releaseQuery != "reset=true" {
				t.Errorf("release query = %q, want reset=true (every release resets)", releaseQuery)
			}
			if ssh.sawCommand("status --porcelain") {
				t.Error("force still read the container's git state")
			}
			if !errors.Is(recErr, os.ErrNotExist) {
				t.Errorf("record survives a forced release: %v", recErr)
			}
		})
	}
}

func TestLeaseTool_CheckoutFailure_SurfacesGitStderrAndContract(t *testing.T) {
	t.Parallel()
	ssh := &leaseStubSSH{fn: func(string) ([]byte, error) {
		return nil, &platform.SSHExecError{
			Hostname: "devpool-3", Output: "fatal: Authentication failed for 'https://github.com/acme/app/'",
			Err: errors.New("exit status 128"),
		}
	}}
	var mu sync.Mutex
	var releaseQuery string
	inner := &fakeOrchestrator{}
	srv, stateDir := leaseToolServerSSH(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/release") {
			mu.Lock()
			releaseQuery = r.URL.RawQuery
			mu.Unlock()
		}
		inner.ServeHTTP(w, r)
	}), ssh)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feature"})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("want an error result, got: %s", text)
	}
	for _, want := range []string{platform.ErrGitTokenInvalid, "git stderr", "Authentication failed", "user-held secret"} {
		if !strings.Contains(text, want) {
			t.Errorf("result %q does not mention %q", text, want)
		}
	}
	for _, banned := range []string{leaseTestLease, leaseTestToken} {
		if strings.Contains(text, banned) {
			t.Errorf("result carries a secret: %s", text)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if releaseQuery != "reset=true" {
		t.Errorf("release query = %q, want the failed checkout's container released with reset=true", releaseQuery)
	}
	if entries, _ := os.ReadDir(filepath.Join(stateDir, "leases")); len(entries) != 0 {
		t.Errorf("a failed checkout left %d entr(ies) in the lease directory", len(entries))
	}
}

func TestLeaseDeps_Mounter_WiresHostSide(t *testing.T) {
	t.Parallel()
	mounter := nopMounterForRegistry{}
	ssh := &leaseStubSSH{}

	wired := leaseDeps(platform.NewMock(), "proj-1", "/state", nil, mounter, ssh)
	if wired.Host.Mounter != mounter || wired.Host.Run == nil {
		t.Errorf("Host = %+v, want the server's mounter and the sudo runner (container mode: aliases and mounts)", wired.Host)
	}
	if wired.SSH != ops.SSHDeployer(ssh) {
		t.Errorf("SSH = %v, want the server's ssh deployer", wired.SSH)
	}

	bare := leaseDeps(platform.NewMock(), "proj-1", "/state", nil, nil, nil)
	if bare.Host.Mounter != nil || bare.Host.Run != nil || bare.SSH != nil {
		t.Errorf("without a mounter the host side must do nothing, got Host %+v SSH %v", bare.Host, bare.SSH)
	}
}

// TestLeaseTool_ResultCarriesEnvelope pins that acquire, status and release each
// answer with the lifecycle envelope under the top-level `envelope` key, built
// after the operation: acquire lists the new lease, release no longer does.
func TestLeaseTool_ResultCarriesEnvelope(t *testing.T) {
	t.Parallel()
	srv, _ := leaseToolServer(t, &fakeOrchestrator{})

	steps := []struct {
		name string
		args map[string]any
		want []string // aliases the envelope lists afterwards
	}{
		{"acquire", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feature-x"}, []string{"devpool-3"}},
		{"status", map[string]any{"action": "status"}, []string{"devpool-3"}},
		{"release", map[string]any{"action": "release", "lease": "devpool-3"}, nil},
	}
	for _, st := range steps {
		res := callTool(t, srv, "zerops_lease", st.args)
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("%s: unexpected error: %s", st.name, text)
		}
		env, ok := workflow.ExtractEnvelope(text)
		if !ok {
			t.Fatalf("%s: no top-level envelope in\n%s", st.name, text)
		}
		if env.Leases == nil {
			t.Fatalf("%s: envelope did not read the leases: %s", st.name, text)
		}
		got := make([]string, 0, len(env.LeaseList()))
		for _, l := range env.LeaseList() {
			got = append(got, l.Alias)
		}
		if strings.Join(got, ",") != strings.Join(st.want, ",") {
			t.Errorf("%s: envelope leases = %v, want %v", st.name, got, st.want)
		}
		if st.want == nil && !strings.Contains(text, `"leases":[]`) {
			t.Errorf("%s: a read-none envelope must carry \"leases\":[]\n%s", st.name, text)
		}
		for _, secret := range []string{leaseTestLease, leaseTestToken} {
			if strings.Contains(text, secret) {
				t.Errorf("%s: result carries a secret", st.name)
			}
		}
	}
}
