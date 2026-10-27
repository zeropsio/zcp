// Tests for: integration — zerops_scale raising a dev pool's maxContainers
// past the preview ports the pool opens, through the full MCP server.

package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestScale_Integration_PoolRaisePastThePreviewPorts_Refused: a pool zcp
// leases from declares preview ports 3001-3003 for its 3 containers; raising
// it to 4 is refused, naming port 3004 and the person's call, and nothing is
// sent (Kara's run, 2026-10-25: the raise went through and container 4's
// preview answered 502).
func TestScale_Integration_PoolRaisePastThePreviewPorts_Refused(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport (routePools).
	t.Chdir(t.TempDir())
	routePools(t, map[string]http.Handler{"orch.devpool.zerops": poolOrchestrator(3, 3, map[int]bool{1: true, 2: true, 3: true})})
	recordIntLease(t, "devpool", "svc-pool", 3, "feat-x")
	pool := intPool("svc-pool", "devpool", "ACTIVE", 3)
	for p := 3001; p <= 3003; p++ {
		pool.Ports = append(pool.Ports, platform.Port{Port: p, Protocol: "tcp", Scheme: "http"})
	}
	mock := defaultMock().
		WithServices([]platform.ServiceStack{pool}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_scale", map[string]any{"serviceHostname": "devpool", "maxContainers": 4})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("a raise past the preview ports went through: %s", text)
	}
	var refusal struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(text), &refusal); err != nil {
		t.Fatalf("refusal is not JSON: %v\n%s", err, text)
	}
	if refusal.Code != platform.ErrPrerequisiteMissing {
		t.Errorf("code = %q, want %s", refusal.Code, platform.ErrPrerequisiteMissing)
	}
	for _, want := range []string{"3004", "ask the person", "run.ports"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, leaseIntToken) || strings.Contains(text, leaseIntLease) {
		t.Fatal("scale refusal carries a secret")
	}
}
