// Tests for: tools/lease.go — zerops_lease at a pool's limits: a full pool,
// a stopped pool, one pool of several that cannot be read.
package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// writeLeaseRecordFile writes zcp's record of a lease the way a successful
// acquire leaves it (.zcp/state/leases/<alias>.json, mode 0600).
func writeLeaseRecordFile(t *testing.T, stateDir, pool, poolServiceID, containerID string, number int, branch string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := map[string]any{
		"poolHostname": pool, "poolServiceId": poolServiceID, "containerId": containerID,
		"containerNumber": number, "containerHostname": fmt.Sprintf("node-id-%d.runtime.%s.zerops", number, pool),
		"lease": leaseTestLease, "branch": branch, "acquiredAt": "2026-10-23T18:00:00Z",
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.json", pool, number)), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func swarmPoolService(id, name, status string) platform.ServiceStack {
	return platform.ServiceStack{ID: id, Name: name, Status: status,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}
}

// TestLeaseTool_Status_OneStoppedPool_OthersStillListed: a status of every
// pool answers even when one pool is stopped. That pool's lease is zcp's own
// record, marked unverified in the result and in the envelope, and the pool
// is named under unreadPools with why; the other pool is reconciled as usual.
func TestLeaseTool_Status_OneStoppedPool_OthersStillListed(t *testing.T) {
	t.Parallel()
	hs := httptest.NewServer(&fakeOrchestrator{reserved: true})
	t.Cleanup(hs.Close)
	stateDir := t.TempDir()
	writeLeaseRecordFile(t, stateDir, "devpool", "svc-pool", "c-abc", 3, "feat-a")
	writeLeaseRecordFile(t, stateDir, "stoppool", "svc-stop", "c-2", 2, "feat-b")
	token := []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{swarmPoolService("svc-pool", "devpool", "ACTIVE"), swarmPoolService("svc-stop", "stoppool", "STOPPED")}).
		WithServiceEnv("svc-pool", token).WithServiceEnv("svc-stop", token)
	factory := func(baseURL, tok string) ops.SwarmAPI {
		if baseURL == "http://orch.devpool.zerops" {
			return platform.NewSwarmClient(hs.URL, tok, hs.Client())
		}
		return platform.NewSwarmClient(baseURL, tok, &http.Client{Transport: dnsFailTransport{}})
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLease(srv, mock, "proj-1", stateDir, factory, nil, nil, runtime.Info{})

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "status"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status failed over one stopped pool: %s", text)
	}
	var got struct {
		Leases []struct {
			Alias      string `json:"alias"`
			State      string `json:"state"`
			Unverified bool   `json:"unverified"`
			Note       string `json:"note"`
		} `json:"leases"`
		UnreadPools []struct {
			Pool string `json:"pool"`
			Note string `json:"note"`
		} `json:"unreadPools"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	states := map[string]string{}
	for _, l := range got.Leases {
		state := l.State
		if l.Unverified {
			state += " (unverified)"
		}
		states[l.Alias] = state
		if l.Alias == "stoppool-2" && !strings.Contains(l.Note, "zerops_manage action=start") {
			t.Errorf("stoppool-2 note %q does not say how to start the pool", l.Note)
		}
	}
	if states["devpool-3"] != "held" || states["stoppool-2"] != "held (unverified)" || len(states) != 2 {
		t.Errorf("leases = %v, want devpool-3 held and stoppool-2 held (unverified)", states)
	}
	if len(got.UnreadPools) != 1 || got.UnreadPools[0].Pool != "stoppool" || !strings.Contains(got.UnreadPools[0].Note, "stopped") {
		t.Errorf("unreadPools = %+v, want stoppool, stopped", got.UnreadPools)
	}

	env, ok := workflow.ExtractEnvelope(text)
	if !ok || env.Leases == nil {
		t.Fatalf("no envelope with leases in\n%s", text)
	}
	unverified := map[string]bool{}
	for _, l := range env.LeaseList() {
		unverified[l.Alias] = l.Unverified
	}
	if len(unverified) != 2 || unverified["devpool-3"] || !unverified["stoppool-2"] {
		t.Errorf("envelope unverified = %v, want only stoppool-2 unverified", unverified)
	}
	for _, secret := range []string{leaseTestLease, leaseTestToken} {
		if strings.Contains(text, secret) {
			t.Error("result carries a secret")
		}
	}
}

// fullPoolOrchestrator is a pool of 10 containers, all reserved: an acquire
// is refused at once with the live body.
type fullPoolOrchestrator struct{}

func (fullPoolOrchestrator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+leaseTestToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	const full = "all 10 container(s) of the pool that can serve the request are taken and another could not be created - retry once one is released"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/pool":
		_, _ = w.Write([]byte(`{"minimumContainerCount":0,"maximumContainerCount":10}`))
	case r.Method == http.MethodGet && r.URL.Path == "/container":
		parts := make([]string, 0, 10)
		for n := 1; n <= 10; n++ {
			parts = append(parts, fmt.Sprintf(`{"id":"c-%d","number":%d,"hostname":"node-id-%d.runtime.devpool.zerops","status":"ACTIVE","lockState":"reserved","dirty":false,"current":true}`, n, n, n))
		}
		_, _ = w.Write([]byte("[" + strings.Join(parts, ",") + "]"))
	case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"` + full + `","meta":[{"error":"` + full + `","code":"containerAction",` +
			`"metadata":{"message":["[containerAction] Service is already at maximum number of containers"],"reason":["all-reserved"],"retryable":["true"]}}]}}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// TestLeaseTool_Acquire_FullPool_SaysHowFull: an acquire on a full pool is
// refused at once, nothing is recorded, and the refusal says how full the
// pool is, that the orchestrator queues nothing, which lease is zcp's own,
// and — the pool being at the per-pool ceiling — that raising its maximum
// will not help.
func TestLeaseTool_Acquire_FullPool_SaysHowFull(t *testing.T) {
	t.Parallel()
	srv, stateDir := leaseToolServer(t, fullPoolOrchestrator{})
	writeLeaseRecordFile(t, stateDir, "devpool", "svc-pool", "c-4", 4, "feat-a")

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feat-b"})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("an acquire on a full pool succeeded: %s", text)
	}
	for _, want := range []string{"is full", "10 of at most 10", "yours: devpool-4", "9 held by other holders", "does not queue",
		"zerops_lease action=release lease=devpool-4", "Raising maxContainers will not help", `"apiCode":"all-reserved"`} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal lacks %q:\n%s", want, text)
		}
	}
	for _, banned := range []string{"may succeed in a moment", leaseTestToken} {
		if strings.Contains(text, banned) {
			t.Errorf("refusal carries %q:\n%s", banned, text)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(stateDir, "leases"))
	if len(entries) != 1 {
		t.Errorf("records = %d, want only the one there was", len(entries))
	}
}

// TestLeaseTool_StoppedPool_SaysStartIt: an orchestrator that does not
// answer because its pool is stopped is reported as a stopped pool, with how
// to start it, never as zcp running outside the project.
func TestLeaseTool_StoppedPool_SaysStartIt(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{swarmPoolService("svc-pool", "devpool", "STOPPED")}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterLease(srv, mock, "proj-1", t.TempDir(), ops.NewSwarmFactory(&http.Client{Transport: dnsFailTransport{}}), nil, nil, runtime.Info{})

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
		for _, want := range []string{"the pool is stopped (status STOPPED)", `zerops_manage action=start serviceHostname=\"devpool\"`, "/var/www", "/tmp"} {
			if !strings.Contains(text, want) {
				t.Errorf("%v: result lacks %q:\n%s", args["action"], want, text)
			}
		}
		if strings.Contains(text, "only inside the project") {
			t.Errorf("%v: a stopped pool is not an outside-the-project error:\n%s", args["action"], text)
		}
	}
}
