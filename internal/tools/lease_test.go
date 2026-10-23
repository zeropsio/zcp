// Tests for: tools/lease.go — zerops_lease MCP tool handler.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
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
	case r.Method == http.MethodPost && r.URL.Path == "/container/rollout":
		// Live shape: a bearer-only call (no lease) without force. The
		// reserved container is skipped as held-by-other; one outdated free
		// container is replaced.
		if r.Header.Get("X-Swarm-Lease") != "" || r.URL.RawQuery != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"rolling out over held containers requires the admin token - present it in the X-Swarm-Admin-Token header"}}`))
			return
		}
		skipped := `[]`
		if o.reserved {
			skipped = `[{"id":"c-abc","reason":"held-by-other"}]`
		}
		_, _ = w.Write([]byte(`{"activeAppVersionId":"av-2","replaced":["c-old"],` +
			`"created":[{"id":"c-new","name":"node-id-5","number":5,"hostname":"node-id-5.runtime.devpool.zerops","appVersionId":"av-2","current":true,"lockState":"","dirty":false}],` +
			`"retiring":[],"skipped":` + skipped + `,"createErrors":[]}`))
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
		{"action": "rollout", "pool": "devpool"},
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
		{"unknown action", map[string]any{"action": "renew"}, platform.ErrInvalidParameter, "acquire, status, release or rollout"},
		{"rollout without pool", map[string]any{"action": "rollout"}, platform.ErrInvalidParameter, "rollout needs pool"},
		{"rollout an unknown pool", map[string]any{"action": "rollout", "pool": "nosuch"}, platform.ErrServiceNotFound, "nosuch"},
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

// TestLeaseTool_Status_CarriesActivity: zerops_lease action=status reports,
// for the held lease, its activity — the checkout's last change, acquiredAt,
// when it was read, no preview facts yet — and its unsaved work, from one read
// of its checkout; the result's envelope never repeats that read
// (docs/spec-workflows.md §8 DP-21).
func TestLeaseTool_Status_CarriesActivity(t *testing.T) {
	t.Parallel()
	changed := time.Date(2026, 10, 23, 13, 0, 0, 0, time.UTC)
	ssh := &leaseStubSSH{fn: func(command string) ([]byte, error) {
		if strings.Contains(command, "@@zcp-lease activity") {
			return fmt.Appendf(nil, "@@zcp-lease activity %d \n@@zcp-lease status\n?? notes.txt\n@@zcp-lease unpushed\n2\n", changed.Unix()), nil
		}
		return nil, nil
	}}
	srv, _ := leaseToolServerSSH(t, &fakeOrchestrator{}, ssh)
	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feature-x"}); res.IsError {
		t.Fatalf("acquire failed: %s", getTextContent(t, res))
	}

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "status"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status failed: %s", text)
	}
	var got struct {
		Leases []struct {
			Alias      string          `json:"alias"`
			State      string          `json:"state"`
			AcquiredAt string          `json:"acquiredAt"`
			Activity   json.RawMessage `json:"activity"`
			Unsaved    json.RawMessage `json:"unsaved"`
		} `json:"leases"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(got.Leases) != 1 || got.Leases[0].Alias != "devpool-3" || got.Leases[0].State != "held" {
		t.Fatalf("leases = %+v, want the held devpool-3", got.Leases)
	}
	l := got.Leases[0]
	var activity map[string]any
	if err := json.Unmarshal(l.Activity, &activity); err != nil {
		t.Fatalf("activity is not an object: %v\n%s", err, text)
	}
	if activity["lastChangeAt"] != "2026-10-23T13:00:00Z" || activity["acquiredAt"] != l.AcquiredAt || activity["readAt"] == nil {
		t.Errorf("activity = %v, want lastChangeAt 2026-10-23T13:00:00Z, acquiredAt %s and a readAt", activity, l.AcquiredAt)
	}
	for _, absent := range []string{"previewLastConnAt", "previewOpenConns", "note"} {
		if _, ok := activity[absent]; ok {
			t.Errorf("activity carries %s: %v", absent, activity)
		}
	}
	if string(l.Unsaved) != `{"files":["?? notes.txt"],"fileCount":1,"unpushedCommits":2}` {
		t.Errorf("unsaved = %s, want notes.txt and 2 commits", l.Unsaved)
	}
	reads := 0
	ssh.mu.Lock()
	for _, c := range ssh.calls {
		if strings.Contains(c, "@@zcp-lease activity") {
			reads++
		}
	}
	ssh.mu.Unlock()
	if reads != 1 {
		t.Errorf("the checkout was read %d times, want once (the envelope never reads it)", reads)
	}
	for _, secret := range []string{leaseTestLease, leaseTestToken} {
		if strings.Contains(text, secret) {
			t.Errorf("status result carries a secret")
		}
	}
}

// TestLeaseTool_Rollout_ResultCarriesEnvelope pins that action=rollout answers
// with what the rollout did — counts, each skip with its reason, the lease it
// skipped by alias — and with the lifecycle envelope, which still lists the
// lease (a rollout never takes it).
func TestLeaseTool_Rollout_ResultCarriesEnvelope(t *testing.T) {
	t.Parallel()
	srv, _ := leaseToolServer(t, &fakeOrchestrator{})
	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feature-x"}); res.IsError {
		t.Fatalf("acquire failed: %s", getTextContent(t, res))
	}

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "rollout", "pool": "devpool"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("rollout failed: %s", text)
	}
	var got struct {
		Action  string `json:"action"`
		Rollout struct {
			Pool     string `json:"pool"`
			Replaced int    `json:"replaced"`
			Created  int    `json:"created"`
			Retiring int    `json:"retiring"`
			Skipped  int    `json:"skipped"`
			Skips    []struct {
				ContainerID string `json:"containerId"`
				Lease       string `json:"lease"`
				Reason      string `json:"reason"`
			} `json:"skips"`
			Note string `json:"note"`
		} `json:"rollout"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("rollout result is not JSON: %v\n%s", err, text)
	}
	r := got.Rollout
	if got.Action != "rollout" || r.Pool != "devpool" {
		t.Errorf("action/pool = %q/%q, want rollout/devpool", got.Action, r.Pool)
	}
	if r.Replaced != 1 || r.Created != 1 || r.Retiring != 0 || r.Skipped != 1 {
		t.Errorf("replaced/created/retiring/skipped = %d/%d/%d/%d, want 1/1/0/1\n%s", r.Replaced, r.Created, r.Retiring, r.Skipped, text)
	}
	if len(r.Skips) != 1 || r.Skips[0].Lease != "devpool-3" || r.Skips[0].Reason != "held-by-other" {
		t.Errorf("skips = %+v, want the lease devpool-3 skipped as held-by-other", r.Skips)
	}
	if !strings.Contains(r.Note, "keep their version") {
		t.Errorf("note %q must say a lease keeps its version", r.Note)
	}
	env, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("no top-level envelope in\n%s", text)
	}
	if aliases := env.LeaseList(); len(aliases) != 1 || aliases[0].Alias != "devpool-3" {
		t.Errorf("envelope leases = %+v, want devpool-3 still held", aliases)
	}
	for _, secret := range []string{leaseTestLease, leaseTestToken} {
		if strings.Contains(text, secret) {
			t.Errorf("rollout result carries a secret: %s", text)
		}
	}
}

// TestLeaseTool_UnmountedLease_EnvelopeSaysWhatStatusSays: a held lease whose
// mount at /var/www/<alias> is gone reads `unmounted` in action=status, and
// the envelope's leases[] carries that same state, never `held` (DP-2; live: a
// lease with no mount read held in the envelope, docs/spec-mate.md §2.8a.3).
func TestLeaseTool_UnmountedLease_EnvelopeSaysWhatStatusSays(t *testing.T) {
	t.Parallel()
	hs := httptest.NewServer(&fakeOrchestrator{reserved: true})
	t.Cleanup(hs.Close)
	stateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stateDir, "leases"), 0o700); err != nil {
		t.Fatal(err)
	}
	rec := `{"poolHostname":"devpool","poolServiceId":"svc-pool","containerId":"c-abc","containerNumber":3,` +
		`"containerHostname":"node-id-3.runtime.devpool.zerops","lease":"` + leaseTestLease + `","branch":"feature-x","acquiredAt":"2026-10-23T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(stateDir, "leases", "devpool-3.json"), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}})
	factory := func(_, token string) ops.SwarmAPI { return platform.NewSwarmClient(hs.URL, token, hs.Client()) }
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	// The stub mounter finds nothing mounted at /var/www/devpool-3.
	RegisterLease(srv, mock, "proj-1", stateDir, factory, newStubMounter(), nil, runtime.Info{})

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "status"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status: %s", text)
	}
	var got leaseToolResult
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	if len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateUnmounted {
		t.Fatalf("status leases = %+v, want devpool-3 unmounted", got.Leases)
	}
	env, ok := workflow.ExtractEnvelope(text)
	if !ok || len(env.LeaseList()) != 1 {
		t.Fatalf("envelope leases missing in\n%s", text)
	}
	if l := env.LeaseList()[0]; l.Alias != "devpool-3" || l.State != ops.LeaseStateUnmounted {
		t.Errorf("envelope lease = %+v, want devpool-3 %s as status reports it", l, ops.LeaseStateUnmounted)
	}
}

// TestEnvelopeLeaseHost_ChecksTheHostSideInsideAContainerOnly: the envelope
// every JSON tool result carries checks a held lease's alias and mount on the
// host side zerops_lease status checks them on — inside a container the
// system mounter and the alias file, elsewhere nothing (where a lease has
// neither).
func TestEnvelopeLeaseHost_ChecksTheHostSideInsideAContainerOnly(t *testing.T) {
	t.Parallel()
	in := envelopeLeaseHost(runtime.Info{InContainer: true})
	if _, ok := in.Mounter.(*platform.SystemMounter); !ok || in.Run == nil {
		t.Errorf("in a container: host = %+v, want the system mounter and the alias-file runner", in)
	}
	if out := envelopeLeaseHost(runtime.Info{}); out.Mounter != nil || out.Run != nil {
		t.Errorf("outside a container: host = %+v, want none", out)
	}
}

// guardSSH answers the release guard's reads per leased container: files are
// an alias's `git status --porcelain` lines (clean when absent), and no
// container holds an unpushed commit; every other command answers nothing.
type guardSSH struct {
	mu    sync.Mutex
	files map[string]string
	reads []string // the aliases whose git status was read
}

func (s *guardSSH) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	switch {
	case strings.Contains(command, "status --porcelain"):
		s.mu.Lock()
		defer s.mu.Unlock()
		s.reads = append(s.reads, host)
		return []byte(s.files[host]), nil
	case strings.Contains(command, "rev-list --count"):
		return []byte("0\n"), nil
	}
	return nil, nil
}

func (s *guardSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// releaseCounts is how many releases each pool's orchestrator was asked for.
func releaseCounts(orchs map[string]*poolOrchestrator) map[string]int {
	out := map[string]int{}
	for name, o := range orchs {
		o.mu.Lock()
		out[name] = o.releases
		o.mu.Unlock()
	}
	return out
}

// leaseResultOf parses a zerops_lease result.
func leaseResultOf(t *testing.T, text string) leaseToolResult {
	t.Helper()
	var got leaseToolResult
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return got
}

// acquireFeatX leases webpool-7 and hqpool-3 on feat-x in one call.
func acquireFeatX(t *testing.T, srv *mcp.Server) {
	t.Helper()
	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"webpool", "hqpool"}, "repo": leaseTestRepo, "branch": "feat-x"})
	if res.IsError {
		t.Fatalf("group acquire failed: %s", getTextContent(t, res))
	}
}

// TestLeaseTool_ReleaseBranch_ReleasesEveryLeaseOfTheBranch: release with
// branch reads every lease of the branch, then releases each with a reset;
// the result lists them all, never a lease or a bearer, and the envelope lists
// none left.
func TestLeaseTool_ReleaseBranch_ReleasesEveryLeaseOfTheBranch(t *testing.T) {
	t.Parallel()
	ssh := &guardSSH{}
	srv, orchs, stateDir := groupLeaseServerSSH(t, ssh)
	acquireFeatX(t, srv)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "branch": "feat-x"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("release of the branch failed: %s", text)
	}
	for _, o := range orchs {
		for _, secret := range []string{o.lease(), o.token} {
			if strings.Contains(text, secret) {
				t.Errorf("result carries a secret: %s", text)
			}
		}
	}
	got := leaseResultOf(t, text)
	if got.Action != "release" || len(got.Leases) != 2 || got.Leases[0].Alias != "hqpool-3" || got.Leases[1].Alias != "webpool-7" {
		t.Fatalf("leases = %+v, want hqpool-3 then webpool-7 (earliest acquired first)", got.Leases)
	}
	for _, v := range got.Leases {
		if v.State != ops.LeaseStateReleased || v.Error != "" {
			t.Errorf("lease = %+v, want released", v)
		}
	}
	ssh.mu.Lock()
	reads := slices.Sorted(slices.Values(ssh.reads))
	ssh.mu.Unlock()
	if strings.Join(reads, ",") != "hqpool-3,webpool-7" {
		t.Errorf("guard read %v, want every lease of the branch", reads)
	}
	if n := releaseCounts(orchs); n["hqpool"] != 1 || n["webpool"] != 1 {
		t.Errorf("releases = %v, want one per pool", n)
	}
	if entries, _ := os.ReadDir(filepath.Join(stateDir, "leases")); slices.ContainsFunc(entries, func(e os.DirEntry) bool { return !e.IsDir() }) {
		t.Errorf("records left after the branch's release: %v", entries)
	}
	env, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("no envelope in\n%s", text)
	}
	if leases := env.LeaseList(); len(leases) != 0 {
		t.Errorf("envelope leases = %+v, want none left", leases)
	}
}

// TestLeaseTool_ReleaseBranch_OneUnsaved_RefusesAll: one lease of the branch
// holding uncommitted files refuses the whole branch, naming that lease and
// its files and the forced release of the branch; nothing reaches an
// orchestrator. force=true then releases every lease.
func TestLeaseTool_ReleaseBranch_OneUnsaved_RefusesAll(t *testing.T) {
	t.Parallel()
	srv, orchs, stateDir := groupLeaseServerSSH(t, &guardSSH{files: map[string]string{"webpool-7": "?? notes.txt\n"}})
	acquireFeatX(t, srv)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "branch": "feat-x"})
	text := getTextContent(t, res)
	if !res.IsError || !strings.Contains(text, platform.ErrPrerequisiteMissing) {
		t.Fatalf("result = (error %v) %s, want a PREREQUISITE_MISSING refusal", res.IsError, text)
	}
	for _, want := range []string{"webpool-7", "notes.txt", "hqpool-3", "zerops_lease action=release branch=feat-x force=true"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %s lacks %q", text, want)
		}
	}
	if n := releaseCounts(orchs); n["hqpool"] != 0 || n["webpool"] != 0 {
		t.Errorf("releases = %v, want none: a refused branch releases nothing", n)
	}
	for _, alias := range []string{"hqpool-3", "webpool-7"} {
		if _, err := os.Stat(filepath.Join(stateDir, "leases", alias+".json")); err != nil {
			t.Errorf("record %s gone after a refused release: %v", alias, err)
		}
	}

	res = callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "branch": "feat-x", "force": "true"})
	text = getTextContent(t, res)
	if res.IsError {
		t.Fatalf("forced release failed: %s", text)
	}
	if got := leaseResultOf(t, text); len(got.Leases) != 2 || got.Leases[0].State != ops.LeaseStateReleased || got.Leases[1].State != ops.LeaseStateReleased {
		t.Errorf("forced release = %+v, want both released", got.Leases)
	}
}

// TestLeaseTool_ReleaseBranch_OrchestratorFailsOne_PerMemberResult: an
// orchestrator that will not release one lease leaves it held and recorded
// while the others go; the result is per lease, the failed one with its
// error, nextActions names the call that finishes the branch, and the
// envelope still lists it. The same call again finishes the branch.
func TestLeaseTool_ReleaseBranch_OrchestratorFailsOne_PerMemberResult(t *testing.T) {
	t.Parallel()
	srv, orchs, _ := groupLeaseServerSSH(t, &guardSSH{})
	acquireFeatX(t, srv)
	setBusy := func(busy bool) {
		orchs["webpool"].mu.Lock()
		orchs["webpool"].busy = busy
		orchs["webpool"].mu.Unlock()
	}
	setBusy(true)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "branch": "feat-x"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("a failure of one lease is that lease's result, not the call's: %s", text)
	}
	got := leaseResultOf(t, text)
	if len(got.Leases) != 2 || got.Leases[0].Alias != "hqpool-3" || got.Leases[1].Alias != "webpool-7" {
		t.Fatalf("leases = %+v, want hqpool-3 then webpool-7", got.Leases)
	}
	if hq := got.Leases[0]; hq.State != ops.LeaseStateReleased || hq.Error != "" {
		t.Errorf("hqpool-3 = %+v, want released", hq)
	}
	if web := got.Leases[1]; web.State != ops.LeaseStateHeld || !strings.Contains(web.Error, "work in progress") {
		t.Errorf("webpool-7 = %+v, want held with the orchestrator's refusal as its error", web)
	}
	if !strings.Contains(text, `"error":`) {
		t.Errorf("the failed lease carries no error key: %s", text)
	}
	if !strings.Contains(got.NextActions, "webpool-7") || !strings.Contains(got.NextActions, "zerops_lease action=release branch=feat-x") {
		t.Errorf("nextActions %q should name webpool-7 and the call that finishes the branch", got.NextActions)
	}
	env, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("no envelope in\n%s", text)
	}
	if leases := env.LeaseList(); len(leases) != 1 || leases[0].Alias != "webpool-7" {
		t.Errorf("envelope leases = %+v, want webpool-7 still held", leases)
	}

	setBusy(false)
	res = callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "branch": "feat-x"})
	text = getTextContent(t, res)
	if res.IsError {
		t.Fatalf("the second call failed: %s", text)
	}
	if again := leaseResultOf(t, text); len(again.Leases) != 1 || again.Leases[0].Alias != "webpool-7" || again.Leases[0].State != ops.LeaseStateReleased {
		t.Errorf("second call = %+v, want webpool-7 alone, released", again.Leases)
	}
}

// TestLeaseTool_ReleaseBranch_BadInput_TypedRefusal: a release names one
// lease or one branch, never both; a branch zcp holds no lease of, or a repo
// that is no clone URL, is refused before any release.
func TestLeaseTool_ReleaseBranch_BadInput_TypedRefusal(t *testing.T) {
	t.Parallel()
	srv, orchs, _ := groupLeaseServerSSH(t, &guardSSH{})
	acquireFeatX(t, srv)
	// Read once every case, run in parallel, has ended.
	t.Cleanup(func() {
		if n := releaseCounts(orchs); n["hqpool"] != 0 || n["webpool"] != 0 {
			t.Errorf("releases = %v on bad input, want none", n)
		}
	})
	for _, tt := range []struct {
		name     string
		args     map[string]any
		wantText string
	}{
		{"lease and branch", map[string]any{"action": "release", "lease": "hqpool-3", "branch": "feat-x"}, "lease or branch"},
		{"neither lease nor branch", map[string]any{"action": "release", "repo": leaseTestRepo}, "needs lease or branch"},
		{"a branch zcp holds no lease of", map[string]any{"action": "release", "branch": "nosuch"}, "nosuch"},
		{"a repo that is no clone URL", map[string]any{"action": "release", "branch": "feat-x", "repo": "git@github.com:acme/app.git"}, "clone URL"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := callTool(t, srv, "zerops_lease", tt.args)
			text := getTextContent(t, res)
			if !res.IsError || !strings.Contains(text, platform.ErrInvalidParameter) || !strings.Contains(text, tt.wantText) {
				t.Errorf("result = (error %v) %s, want INVALID_PARAMETER mentioning %q", res.IsError, text, tt.wantText)
			}
		})
	}
}
