// Tests for: scale.go — zerops_scale raising a dev pool's maxContainers past
// the preview ports the pool opens.

package tools

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestScaleTool_PoolRaisePastThePreviewPorts_RefusedWithTheStep: raising the
// maximum of a pool zcp leases from past the HTTP ports it declares (3001-3003
// for 3 containers) is refused before anything is sent — the wire names the
// port the new container would answer on, the run.ports change and pool
// deploy that open it, and that a raise is the person's call; a raise the
// ports cover goes through.
func TestScaleTool_PoolRaisePastThePreviewPorts_RefusedWithTheStep(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	pool := swarmPoolService("svc-pool", "devpool", "ACTIVE")
	pool.CustomAutoscaling = &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: 3}
	for p := 3001; p <= 3003; p++ {
		pool.Ports = append(pool.Ports, platform.Port{Port: p, Protocol: "tcp", Scheme: "http"})
	}
	client := &scaleSentClient{Mock: platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
		WithServices([]platform.ServiceStack{pool}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}})}
	hs := httptest.NewServer(poolContainersOrchestrator(3, 1, 2, 3))
	t.Cleanup(hs.Close)
	factory := func(_, token string) ops.SwarmAPI { return platform.NewSwarmClient(hs.URL, token, hs.Client()) }
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterScale(srv, client, "proj-1", nil, WithPoolOrchestratorsForScale(stateDir, factory))
	writeLeaseRecordFile(t, stateDir, "devpool", "svc-pool", "c-3", 3, "feat-x")

	res := callTool(t, srv, "zerops_scale", map[string]any{"serviceHostname": "devpool", "maxContainers": 4})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("a raise past the preview ports went through: %s", text)
	}
	var refusal struct {
		Code       string `json:"code"`
		Error      string `json:"error"`
		Suggestion string `json:"suggestion"`
	}
	if err := json.Unmarshal([]byte(text), &refusal); err != nil {
		t.Fatalf("refusal is not JSON: %v\n%s", err, text)
	}
	if refusal.Code != platform.ErrPrerequisiteMissing {
		t.Errorf("code = %q, want %s", refusal.Code, platform.ErrPrerequisiteMissing)
	}
	wire := refusal.Error + " " + refusal.Suggestion
	for _, want := range []string{"3004", "502", "ask the person", "run.ports", "httpSupport: true", `targetService="devpool"`} {
		if !strings.Contains(wire, want) {
			t.Errorf("refusal lacks %q:\n%s", want, text)
		}
	}
	if n := len(client.sends()); n != 0 {
		t.Fatalf("a refused raise sent %d change(s) to the platform", n)
	}

	res = callTool(t, srv, "zerops_scale", map[string]any{"serviceHostname": "devpool", "maxContainers": 3, "minRam": 4})
	if res.IsError {
		t.Fatalf("a scale that raises nothing was refused: %s", getTextContent(t, res))
	}
}
