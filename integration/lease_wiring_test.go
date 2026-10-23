// Tests for: integration — a part's dev servers reach the same branch's
// sibling workspaces by container hostname (docs/spec-workflows.md §8 "Dev
// pools (DP)", docs/spec-mate.md §2.8a.1): through the full MCP server, a
// dev-server start in a lease hands its supervisor the overlay the
// declaration's siblingEnv names before it starts the server.

package integration_test

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// writeWiringRecords records branch feat-x of acme/app on hqpool-7 and
// webpool-3 — one group — in the state directory the server resolves from the
// cwd.
func writeWiringRecords(t *testing.T) {
	t.Helper()
	dir := filepath.Join(".zcp", "state", "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for alias, rec := range map[string]string{
		"hqpool-7": `{"poolHostname":"hqpool","poolServiceId":"svc-hq","containerId":"c-7","containerNumber":7,"containerHostname":"node-id-7.runtime.hqpool.zerops",` +
			`"lease":"hq-secret","branch":"feat-x","repo":"https://github.com/acme/app","acquiredAt":"2026-10-23T11:00:00Z"}`,
		"webpool-3": `{"poolHostname":"webpool","poolServiceId":"svc-web","containerId":"c-3","containerNumber":3,"containerHostname":"node-id-3.runtime.webpool.zerops",` +
			`"lease":"web-secret","branch":"feat-x","repo":"https://github.com/acme/app","acquiredAt":"2026-10-23T12:00:00Z"}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, alias+".json"), []byte(rec), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestLease_Integration_DevServerStart_SendsTheSiblingOverlay: web's start in
// webpool-3 sets HQ_HOST to hqpool-7's container hostname on the supervisor
// before the start, and the result names it and the variable left to the
// container's env.
func TestLease_Integration_DevServerStart_SendsTheSiblingOverlay(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir the server resolves from the cwd).
	t.Chdir(t.TempDir())
	writeWiringRecords(t)
	pool := platform.ServiceStack{
		ID: "svc-web", Name: "webpool", ProjectID: "proj-1", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "swarm@1"},
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test-project", Status: "ACTIVE", SubdomainHost: "abc1.prg1.zerops.app"}).
		WithServices([]platform.ServiceStack{pool}).
		WithService(&pool)
	c := devservertest.NewSupervised(`{"version":1,"siblingEnv":{"HQ_HOST":"hqpool","API_HOST":"apipool"},"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`)
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), c)
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_dev_server", map[string]any{"action": "start", "hostname": "webpool-3", "name": "web"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("start: %s", text)
	}
	var got struct {
		Running    bool `json:"running"`
		SiblingEnv struct {
			Set                map[string]string `json:"set"`
			LeftToContainerEnv []string          `json:"leftToContainerEnv"`
		} `json:"siblingEnv"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	want := map[string]string{"HQ_HOST": "node-id-7.runtime.hqpool.zerops"}
	if !got.Running || !maps.Equal(got.SiblingEnv.Set, want) || len(got.SiblingEnv.LeftToContainerEnv) != 1 || got.SiblingEnv.LeftToContainerEnv[0] != "API_HOST" {
		t.Errorf("result = %+v, want web running with %v set and API_HOST left to the container's env", got, want)
	}
	if !maps.Equal(c.Env, want) {
		t.Errorf("the supervisor's overlay = %v, want %v", c.Env, want)
	}
	env, start := -1, -1
	for i, cmd := range c.Sent() {
		switch {
		case strings.Contains(cmd, "zcp-devsup ctl 'env'") && env < 0:
			env = i
		case strings.Contains(cmd, "zcp-devsup ctl 'start' 'web'") && start < 0:
			start = i
		}
	}
	if env < 0 || start < 0 || env > start {
		t.Errorf("ctl env (%d) must come before the start (%d): %v", env, start, c.Sent())
	}
}
