// Tests for: scale.go — zerops_scale raising a dev pool's maxContainers past
// the preview ports the pool opens; lease.go — zerops_lease status of a pool
// already past them.

package tools

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// TestScaleTool_PoolRaisePastThePreviewPorts_ScalesAndSaysSo: raising the
// maximum of a pool zcp leases from past the HTTP ports it declares (3001-3003
// for 3 containers) is sent — the pool is the person's infrastructure — and
// the result's notice says so on the wire: maxContainers against the preview
// ports, the container with no preview, and both fixes (the run.ports change
// and pool deploy, or maxContainers at the port count). A raise the ports
// cover says nothing of preview ports.
func TestScaleTool_PoolRaisePastThePreviewPorts_ScalesAndSaysSo(t *testing.T) {
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
	if res.IsError {
		t.Fatalf("a raise past the preview ports was refused; it is a notice: %s", text)
	}
	var got struct {
		Notice string `json:"notice"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	for _, want := range []string{"Pool 'devpool': maxContainers 4", "3 preview ports (3001-3003)", "Container 4 has no preview port", "3004",
		"run.ports", "httpSupport: true", `targetService="devpool"`, `zerops_scale serviceHostname="devpool" maxContainers=3`} {
		if !strings.Contains(got.Notice, want) {
			t.Errorf("notice lacks %q:\n%s", want, got.Notice)
		}
	}
	if n := len(client.sends()); n != 1 {
		t.Fatalf("the raise sent %d change(s) to the platform, want 1", n)
	}

	res = callTool(t, srv, "zerops_scale", map[string]any{"serviceHostname": "devpool", "maxContainers": 3, "minRam": 4})
	text = getTextContent(t, res)
	if res.IsError {
		t.Fatalf("a scale within the preview ports was refused: %s", text)
	}
	if strings.Contains(text, "preview port") {
		t.Errorf("a scale within the preview ports speaks of them: %s", text)
	}
}

// TestLeaseTool_Status_PoolPastItsPreviewPorts_SaysSo: a status of a pool zcp
// leases from whose maxContainers (4) runs past the preview ports it declares
// (3001-3003) names the pool under poolNotices with the same words a scale
// uses — whether the pool is named or every pool is listed; a pool within its
// ports carries no notice.
func TestLeaseTool_Status_PoolPastItsPreviewPorts_SaysSo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		max  int32
		pool string
		want bool
	}{
		{name: "the named pool past its ports", max: 4, pool: "devpool", want: true},
		{name: "every pool listed, one past its ports", max: 4, want: true},
		{name: "a pool within its ports", max: 3, pool: "devpool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hs := httptest.NewServer(&fakeOrchestrator{reserved: true})
			t.Cleanup(hs.Close)
			stateDir := t.TempDir()
			writeLeaseRecordFile(t, stateDir, "devpool", "svc-pool", "c-abc", 3, "feat-a")
			pool := swarmPoolService("svc-pool", "devpool", "ACTIVE")
			pool.CustomAutoscaling = &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: tt.max}
			for p := 3001; p <= 3003; p++ {
				pool.Ports = append(pool.Ports, platform.Port{Port: p, Protocol: "tcp", Scheme: "http"})
			}
			mock := platform.NewMock().
				WithProject(&platform.Project{ID: "proj-1", Name: "proj"}).
				WithServices([]platform.ServiceStack{pool}).
				WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: leaseTestToken, Sensitive: true}})
			factory := func(_, tok string) ops.SwarmAPI { return platform.NewSwarmClient(hs.URL, tok, hs.Client()) }
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			registerLeaseTool(t, srv, mock, "proj-1", stateDir, factory, nil, nil, runtime.Info{})

			args := map[string]any{"action": "status"}
			if tt.pool != "" {
				args["pool"] = tt.pool
			}
			res := callTool(t, srv, "zerops_lease", args)
			text := getTextContent(t, res)
			if res.IsError {
				t.Fatalf("status failed: %s", text)
			}
			var got struct {
				PoolNotices []struct {
					Pool string `json:"pool"`
					Note string `json:"note"`
				} `json:"poolNotices"`
			}
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, text)
			}
			if !tt.want {
				if len(got.PoolNotices) != 0 || strings.Contains(text, "preview port") {
					t.Errorf("a pool within its preview ports carries a notice: %s", text)
				}
				return
			}
			if len(got.PoolNotices) != 1 || got.PoolNotices[0].Pool != "devpool" {
				t.Fatalf("poolNotices = %+v, want one for devpool:\n%s", got.PoolNotices, text)
			}
			for _, want := range []string{"Pool 'devpool': maxContainers 4", "3 preview ports (3001-3003)", "Container 4 has no preview port", "3004",
				"run.ports", `zerops_scale serviceHostname="devpool" maxContainers=3`} {
				if !strings.Contains(got.PoolNotices[0].Note, want) {
					t.Errorf("note lacks %q:\n%s", want, got.PoolNotices[0].Note)
				}
			}
		})
	}
}
