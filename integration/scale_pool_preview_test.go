// Tests for: integration — zerops_scale raising a dev pool's maxContainers
// past the preview ports the pool opens, through the full MCP server: the
// scale runs and its result says which containers have no preview.

package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestScale_Integration_PoolRaisePastThePreviewPorts_ScalesAndSaysSo: a pool
// zcp leases from declares preview ports 3001-3003 for its 3 containers;
// raising it to 4 goes through, and the result says that container 4 has no
// preview port and how to give it one (Kara's run: the raise went through and
// nothing said a lease in container 4 had no preview).
func TestScale_Integration_PoolRaisePastThePreviewPorts_ScalesAndSaysSo(t *testing.T) {
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
	if res.IsError {
		t.Fatalf("a raise past the preview ports was refused; it is a notice: %s", text)
	}
	var got struct {
		Notice string `json:"notice"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	for _, want := range []string{"maxContainers 4", "3 preview ports", "Container 4 has no preview port", "3004", "run.ports", "maxContainers=3"} {
		if !strings.Contains(got.Notice, want) {
			t.Errorf("notice lacks %q:\n%s", want, got.Notice)
		}
	}
	if strings.Contains(text, leaseIntToken) || strings.Contains(text, leaseIntLease) {
		t.Fatal("scale result carries a secret")
	}
}
