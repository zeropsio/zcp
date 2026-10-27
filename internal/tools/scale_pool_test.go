// Tests for: scale.go — zerops_scale lowering a pool's maxContainers: the
// containers it would delete are read first, and a lowering over a lease
// needs a confirmDestructive acknowledgment.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// scaleSentClient records every autoscaling change sent to the platform.
type scaleSentClient struct {
	*platform.Mock
	mu   sync.Mutex
	sent []platform.AutoscalingParams
}

func (c *scaleSentClient) SetAutoscaling(ctx context.Context, id string, p platform.AutoscalingParams) (*platform.Process, error) {
	c.mu.Lock()
	c.sent = append(c.sent, p)
	c.mu.Unlock()
	return c.Mock.SetAutoscaling(ctx, id, p)
}

func (c *scaleSentClient) sends() []platform.AutoscalingParams {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]platform.AutoscalingParams(nil), c.sent...)
}

// poolContainersOrchestrator lists containers c-1..c-<count> of devpool,
// those in leased reserved.
func poolContainersOrchestrator(count int, leased ...int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+leaseTestToken || r.Method != http.MethodGet || r.URL.Path != "/container" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		parts := make([]string, 0, count)
		for n := 1; n <= count; n++ {
			lock := ""
			for _, l := range leased {
				if l == n {
					lock = "reserved"
				}
			}
			parts = append(parts, fmt.Sprintf(`{"id":"c-%d","number":%d,"hostname":"node-id-%d.runtime.devpool.zerops","status":"ACTIVE","lockState":%q,"dirty":false,"current":true}`, n, n, n, lock))
		}
		_, _ = w.Write([]byte("[" + strings.Join(parts, ",") + "]"))
	})
}

// scalePoolServer is zerops_scale over a project holding devpool (maximum 5
// containers). With orch set, the tool reads the pool's orchestrator through
// it (WithPoolOrchestratorsForScale); without, it has no orchestrator client.
func scalePoolServer(t *testing.T, orch http.Handler) (srv *mcp.Server, client *scaleSentClient, stateDir string) {
	t.Helper()
	stateDir = t.TempDir()
	pool := swarmPoolService("svc-pool", "devpool", "ACTIVE")
	pool.CustomAutoscaling = &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: 5}
	client = &scaleSentClient{Mock: platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{pool}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}})}
	srv = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	if orch == nil {
		RegisterScale(srv, client, "proj-1", nil)
		return srv, client, stateDir
	}
	hs := httptest.NewServer(orch)
	t.Cleanup(hs.Close)
	factory := func(_, token string) ops.SwarmAPI { return platform.NewSwarmClient(hs.URL, token, hs.Client()) }
	RegisterScale(srv, client, "proj-1", nil, WithPoolOrchestratorsForScale(stateDir, factory))
	return srv, client, stateDir
}

// scaleRefusal is the error wire of a diagnose-before-destruct refusal.
type scaleRefusal struct {
	Code         string `json:"code"`
	Suggestion   string `json:"suggestion"`
	WouldDestroy *struct {
		Operation string   `json:"operation"`
		Targets   []string `json:"targets"`
		Loss      struct {
			Containers []string `json:"containers"`
		} `json:"wouldDestroy"`
		Diagnoses []struct {
			Hostname string `json:"hostname"`
			Cause    string `json:"cause"`
		} `json:"diagnoses"`
		Retry *struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		} `json:"retryCall"`
	} `json:"wouldDestroy"`
}

func parseScaleRefusal(t *testing.T, res *mcp.CallToolResult) scaleRefusal {
	t.Helper()
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("want a refusal, got: %s", text)
	}
	var r scaleRefusal
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatalf("refusal is not JSON: %v\n%s", err, text)
	}
	return r
}

// TestScaleTool_PoolLoweringOverALease_NeedsConfirmDestructive: lowering a
// pool's maxContainers so that a leased container would be deleted is refused
// with what would go — the lease, whose it is, the call that acknowledges it
// — and nothing is sent; an acknowledgment that does not name exactly the
// leases diagnosed is refused again; the matching one proceeds and the result
// names what was deleted.
func TestScaleTool_PoolLoweringOverALease_NeedsConfirmDestructive(t *testing.T) {
	t.Parallel()
	srv, client, stateDir := scalePoolServer(t, poolContainersOrchestrator(5, 5))
	writeLeaseRecordFile(t, stateDir, "devpool", "svc-pool", "c-5", 5, "feat-x")

	r := parseScaleRefusal(t, callTool(t, srv, "zerops_scale", map[string]any{"serviceHostname": "devpool", "maxContainers": 4}))
	if r.Code != platform.ErrDiagnosisRequired || r.WouldDestroy == nil {
		t.Fatalf("refusal = %+v, want %s with wouldDestroy", r, platform.ErrDiagnosisRequired)
	}
	wd := r.WouldDestroy
	if wd.Operation != "pool-shrink" || strings.Join(wd.Targets, ",") != "devpool-5" || strings.Join(wd.Loss.Containers, ",") != "devpool-5" {
		t.Errorf("wouldDestroy = %+v, want operation pool-shrink, target devpool-5, container devpool-5", wd)
	}
	if len(wd.Diagnoses) != 1 || wd.Diagnoses[0].Hostname != "devpool-5" || !strings.Contains(wd.Diagnoses[0].Cause, "your lease, branch feat-x") {
		t.Errorf("diagnoses = %+v, want devpool-5 as your lease on feat-x", wd.Diagnoses)
	}
	if wd.Retry == nil || wd.Retry.Tool != "zerops_scale" || wd.Retry.Args["maxContainers"] != float64(4) || wd.Retry.Args["confirmDestructive"] == nil {
		t.Errorf("retryCall = %+v, want the same zerops_scale call with confirmDestructive", wd.Retry)
	}
	for _, want := range []string{"zerops_lease action=release lease=devpool-5", "maxContainers 5", `confirmDestructive={"operation":"pool-shrink","acknowledgedTargets":["devpool-5"]}`, "/var/www"} {
		if !strings.Contains(r.Suggestion, want) {
			t.Errorf("suggestion %q lacks %q", r.Suggestion, want)
		}
	}
	if n := len(client.sends()); n != 0 {
		t.Fatalf("a refused lowering sent %d change(s) to the platform", n)
	}

	wrong := map[string]any{"serviceHostname": "devpool", "maxContainers": 4,
		"confirmDestructive": map[string]any{"operation": "pool-shrink", "acknowledgedTargets": []string{"devpool-4"}}}
	if r := parseScaleRefusal(t, callTool(t, srv, "zerops_scale", wrong)); r.Code != platform.ErrDiagnosisRequired {
		t.Errorf("an acknowledgment naming another container = %+v, want a refusal", r)
	}
	if n := len(client.sends()); n != 0 {
		t.Fatalf("a mismatched acknowledgment sent %d change(s)", n)
	}

	acked := map[string]any{"serviceHostname": "devpool", "maxContainers": 4,
		"confirmDestructive": map[string]any{"operation": "pool-shrink", "acknowledgedTargets": []string{"devpool-5"}}}
	res := callTool(t, srv, "zerops_scale", acked)
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("the acknowledged lowering was refused: %s", text)
	}
	sent := client.sends()
	if len(sent) != 1 || sent[0].HorizontalMaxCount == nil || *sent[0].HorizontalMaxCount != 4 {
		t.Fatalf("sent = %+v, want one change to maxContainers 4", sent)
	}
	if !strings.Contains(text, "deletes devpool-5 (your lease, branch feat-x)") {
		t.Errorf("result does not name what the lowering deletes:\n%s", text)
	}
}

// TestScaleTool_PoolLoweringOverFreeContainers_ProceedsAndNamesThem: a
// lowering that deletes only free containers needs no acknowledgment, and the
// result names the containers that go.
func TestScaleTool_PoolLoweringOverFreeContainers_ProceedsAndNamesThem(t *testing.T) {
	t.Parallel()
	srv, client, _ := scalePoolServer(t, poolContainersOrchestrator(5, 1))

	res := callTool(t, srv, "zerops_scale", map[string]any{"serviceHostname": "devpool", "maxContainers": 3})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("a lowering over free containers was refused: %s", text)
	}
	if len(client.sends()) != 1 {
		t.Errorf("sent %d change(s), want one", len(client.sends()))
	}
	if !strings.Contains(text, "deletes devpool-5 and devpool-4") {
		t.Errorf("result does not name the free containers that go:\n%s", text)
	}
}

// TestScaleTool_PoolLoweringUnchecked_NeedsConfirmDestructive: a zcp that
// cannot ask the pool's orchestrator says so and treats any lowering of the
// maximum as one over a lease: refused until acknowledged for the pool.
func TestScaleTool_PoolLoweringUnchecked_NeedsConfirmDestructive(t *testing.T) {
	t.Parallel()
	srv, client, _ := scalePoolServer(t, nil)

	r := parseScaleRefusal(t, callTool(t, srv, "zerops_scale", map[string]any{"serviceHostname": "devpool", "maxContainers": 3}))
	if r.Code != platform.ErrDiagnosisRequired || r.WouldDestroy == nil || strings.Join(r.WouldDestroy.Targets, ",") != "devpool" {
		t.Fatalf("refusal = %+v, want %s naming the pool", r, platform.ErrDiagnosisRequired)
	}
	if !strings.Contains(r.Suggestion, "cannot ask") {
		t.Errorf("suggestion %q does not say the orchestrator could not be asked", r.Suggestion)
	}
	if len(client.sends()) != 0 {
		t.Fatal("an unchecked lowering was sent")
	}
	acked := map[string]any{"serviceHostname": "devpool", "maxContainers": 3,
		"confirmDestructive": map[string]any{"operation": "pool-shrink", "acknowledgedTargets": []string{"devpool"}}}
	if res := callTool(t, srv, "zerops_scale", acked); res.IsError {
		t.Fatalf("the acknowledged lowering was refused: %s", getTextContent(t, res))
	}
	if len(client.sends()) != 1 {
		t.Errorf("sent %d change(s), want one", len(client.sends()))
	}
	// A raise deletes nothing: no orchestrator read, no acknowledgment.
	if res := callTool(t, srv, "zerops_scale", map[string]any{"serviceHostname": "devpool", "maxContainers": 8}); res.IsError {
		t.Errorf("a raise was refused: %s", getTextContent(t, res))
	}
}
