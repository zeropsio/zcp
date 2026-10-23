// Tests for: tools/lease.go — zerops_lease action=acquire with pools: one
// lease per pool on one branch, listed in the pools' order (DP-17).
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolOrchestrator is one pool's orchestrator on the live wire shapes: one
// container (number), bearer-checked, lease-checked on the header.
type poolOrchestrator struct {
	pool, token string
	number      int

	mu       sync.Mutex
	reserved bool
	acquires int
	releases int
	// busy refuses every release (container-busy, retryable).
	busy bool
}

func (o *poolOrchestrator) lease() string { return o.pool + "-lease-0123456789abcdef" }

func (o *poolOrchestrator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+o.token {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"notAuthorized","message":"missing or wrong API token"}}`))
		return
	}
	n := strconv.Itoa(o.number)
	id := "c-" + o.pool
	container := func(withLease bool) string {
		lock := ""
		if o.reserved {
			lock = "reserved"
		}
		c := `{"id":"` + id + `","name":"node-id-` + n + `","number":` + n + `,"hostname":"node-id-` + n + `.runtime.` + o.pool + `.zerops","status":"ACTIVE",` +
			`"serviceId":"svc-` + o.pool + `","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + lock + `","dirty":false`
		if withLease {
			c += `,"lease":"` + o.lease() + `"`
		}
		return c + `}`
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/container":
		_, _ = w.Write([]byte(`[` + container(false) + `]`))
	case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
		o.acquires++
		o.reserved = true
		_, _ = w.Write([]byte(container(true)))
	case r.Method == http.MethodPost && r.URL.Path == "/container/"+id+"/release" && r.Header.Get("X-Swarm-Lease") == o.lease():
		o.releases++
		if o.busy {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"work in progress","meta":[{"metadata":{"reason":["container-busy"],"retryable":["true"]}}]}}`))
			return
		}
		o.reserved = false
		_, _ = w.Write([]byte(container(false)))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// groupLeaseServer is a project holding two pools, hqpool (#3) and webpool
// (#7), each behind its own orchestrator.
func groupLeaseServer(t *testing.T) (*mcp.Server, map[string]*poolOrchestrator) {
	t.Helper()
	srv, orchs, _ := groupLeaseServerSSH(t, nil)
	return srv, orchs
}

// groupLeaseServerSSH is groupLeaseServer with ssh into the leased containers
// and the state directory the leases are recorded under.
func groupLeaseServerSSH(t *testing.T, ssh ops.SSHDeployer) (*mcp.Server, map[string]*poolOrchestrator, string) {
	t.Helper()
	orchs := map[string]*poolOrchestrator{
		"hqpool":  {pool: "hqpool", token: "hq-orch-token-0001", number: 3},
		"webpool": {pool: "webpool", token: "web-orch-token-0001", number: 7},
	}
	urls := map[string]string{}
	for name, o := range orchs {
		hs := httptest.NewServer(o)
		t.Cleanup(hs.Close)
		urls["http://orch."+name+".zerops"] = hs.URL
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{
			{ID: "svc-hqpool", Name: "hqpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-webpool", Name: "webpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-hqpool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: orchs["hqpool"].token, Sensitive: true}}).
		WithServiceEnv("svc-webpool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: orchs["webpool"].token, Sensitive: true}})
	factory := func(baseURL, token string) ops.SwarmAPI {
		return platform.NewSwarmClient(urls[baseURL], token, http.DefaultClient)
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	stateDir := t.TempDir()
	RegisterLease(srv, mock, "proj-1", stateDir, factory, nil, ssh, runtime.Info{})
	return srv, orchs, stateDir
}

// TestLeaseTool_GroupAcquire_MembersInPoolsOrder: acquire with pools leases
// one container per pool on the branch and lists them in the pools' order —
// the order Mate binds the first of — each with its acquiredAt, never a lease
// or a bearer; the envelope lists both, with acquiredAt.
func TestLeaseTool_GroupAcquire_MembersInPoolsOrder(t *testing.T) {
	t.Parallel()
	srv, orchs := groupLeaseServer(t)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"webpool", "hqpool"}, "repo": leaseTestRepo, "branch": "feat-x"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("group acquire failed: %s", text)
	}
	for _, o := range orchs {
		for _, secret := range []string{o.lease(), o.token} {
			if strings.Contains(text, secret) {
				t.Errorf("result carries a secret: %s", text)
			}
		}
	}
	var got struct {
		Leases      []ops.LeaseView `json:"leases"`
		NextActions string          `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(got.Leases) != 2 || got.Leases[0].Alias != "webpool-7" || got.Leases[1].Alias != "hqpool-3" {
		t.Fatalf("leases = %+v, want webpool-7 then hqpool-3 (the pools' order)", got.Leases)
	}
	for _, v := range got.Leases {
		if v.State != ops.LeaseStateHeld || v.Branch != "feat-x" || v.AcquiredAt == "" || v.Reused {
			t.Errorf("lease = %+v, want held on feat-x with its acquiredAt", v)
		}
	}
	for _, alias := range []string{"webpool-7", "hqpool-3"} {
		if !strings.Contains(got.NextActions, alias) {
			t.Errorf("nextActions %q should name %s", got.NextActions, alias)
		}
	}
	env, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("no envelope in\n%s", text)
	}
	if leases := env.LeaseList(); len(leases) != 2 || leases[0].AcquiredAt == "" || leases[1].AcquiredAt == "" {
		t.Errorf("envelope leases = %+v, want both with acquiredAt", leases)
	}

	// The same branch on a pool zcp already holds: the held lease, reused.
	res = callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "hqpool", "repo": leaseTestRepo, "branch": "feat-x"})
	text = getTextContent(t, res)
	if res.IsError {
		t.Fatalf("second acquire failed: %s", text)
	}
	got.Leases = nil
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(got.Leases) != 1 || got.Leases[0].Alias != "hqpool-3" || !got.Leases[0].Reused || !strings.Contains(text, `"reused":true`) {
		t.Errorf("second acquire = %s, want hqpool-3 reused", text)
	}
	orchs["hqpool"].mu.Lock()
	acquires := orchs["hqpool"].acquires
	orchs["hqpool"].mu.Unlock()
	if acquires != 1 {
		t.Errorf("hqpool's orchestrator reserved %d containers, want 1", acquires)
	}
}

// TestLeaseTool_GroupAcquire_BadInput_TypedRefusal: pool and pools are one
// or the other, and pools names each pool once.
func TestLeaseTool_GroupAcquire_BadInput_TypedRefusal(t *testing.T) {
	t.Parallel()
	srv, orchs := groupLeaseServer(t)
	// Read once every case, run in parallel, has ended.
	t.Cleanup(func() {
		for name, o := range orchs {
			o.mu.Lock()
			n := o.acquires
			o.mu.Unlock()
			if n != 0 {
				t.Errorf("%s reserved %d container(s) on bad input", name, n)
			}
		}
	})
	for _, tt := range []struct {
		name     string
		args     map[string]any
		wantText string
	}{
		{"pool and pools", map[string]any{"action": "acquire", "pool": "hqpool", "pools": []string{"webpool"}, "repo": leaseTestRepo}, "pool or pools"},
		{"pools empty", map[string]any{"action": "acquire", "pools": []string{}, "repo": leaseTestRepo}, "needs pool"},
		{"a pool twice", map[string]any{"action": "acquire", "pools": []string{"hqpool", "hqpool"}, "repo": leaseTestRepo}, "hqpool"},
		{"pools on a release", map[string]any{"action": "release", "pools": []string{"hqpool"}, "lease": "hqpool-3"}, "pools"},
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

// The schema offers pools as an array of pool hostnames.
func TestLeaseTool_Schema_OffersPools(t *testing.T) {
	t.Parallel()
	srv, _ := groupLeaseServer(t)
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
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]struct {
				Type  string `json:"type"`
				Items *struct {
					Type string `json:"type"`
				} `json:"items"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		p, ok := schema.Properties["pools"]
		if !ok || p.Type != "array" || p.Items == nil || p.Items.Type != "string" {
			t.Errorf("pools = %+v, want an array of strings: %s", p, raw)
		}
		return
	}
	t.Fatal("zerops_lease is not registered")
}
