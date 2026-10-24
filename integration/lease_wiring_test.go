// Tests for: integration — a part's dev servers reach the same branch's
// sibling workspaces by container hostname (docs/spec-workflows.md §8 "Dev
// pools (DP)", docs/spec-mate.md §2.8a.1): through the full MCP server, a
// dev-server start in a lease hands its supervisor the overlay the
// declaration's siblingEnv names before it starts the server.

package integration_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	vouchFor(t, "webpool") // the pool's orchestrator gives zcp its lease (DP-2)
	pool := platform.ServiceStack{
		ID: "svc-web", Name: "webpool", ProjectID: "proj-1", Status: "ACTIVE",
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "swarm@1"},
	}
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test-project", Status: "ACTIVE", SubdomainHost: "abc1.prg1.zerops.app"}).
		WithServices([]platform.ServiceStack{pool}).
		WithService(&pool).
		WithServiceEnv("svc-web", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
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

// rewireLeaseSSH is the leased containers #3 of hqpool and #7 of webpool,
// each running zcp-devsup: a checkout is cut new, the release guard reads a
// clean checkout, status's read of a checkout answers no change with the
// supervisor's own answer as its supervisor part, and every supervisor
// command goes to the lease's container.
type rewireLeaseSSH struct {
	containers map[string]*devservertest.Container
}

func (s *rewireLeaseSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	c := s.containers[host]
	switch {
	case strings.Contains(command, "fetch -q --prune origin"):
		return []byte("@@zcp-lease base 9f8e7d6c5b4a39281706f5e4d3c2b1a098765432\n"), nil
	case strings.Contains(command, "@@zcp-lease activity"):
		out, err := c.ExecSSH(ctx, host, "@@zcp-devsup version")
		return append([]byte("@@zcp-lease activity 1792756800 \n@@zcp-lease status\n@@zcp-lease unpushed\n0\n@@zcp-lease preview\n"), out...), err
	case strings.Contains(command, "status --porcelain"):
		return nil, nil
	case strings.Contains(command, "rev-list --count"):
		return []byte("0\n"), nil
	case c != nil:
		return c.ExecSSH(ctx, host, command)
	}
	return nil, nil
}

func (s *rewireLeaseSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// TestLease_Integration_Rewire_ReleaseFirstRewiresStatusNamesStale: through
// the full MCP server, releasing hqpool-3 of branch feat-x first hands
// webpool-7 the overlay without it and names the rewire; once hqpool-3 is
// leased again and its lease then ends outside zcp, status marks webpool-7's
// wiring stale and changes nothing.
func TestLease_Integration_Rewire_ReleaseFirstRewiresStatusNamesStale(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport, as in lease_test.go.
	t.Chdir(t.TempDir())

	hqTarget, _, _ := groupPoolOrchestratorReleasing(t, "hqpool", "3")
	webTarget, _, _ := groupPoolOrchestratorReleasing(t, "webpool", "7")
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoutes{targets: map[string]*url.URL{"orch.hqpool.zerops": hqTarget, "orch.webpool.zerops": webTarget}, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })
	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-hqpool", Name: "hqpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-webpool", Name: "webpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-hqpool", []platform.ServiceEnvVar{{ID: "env-h", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}}).
		WithServiceEnv("svc-webpool", []platform.ServiceEnvVar{{ID: "env-w", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	decl := func(server, port string) string {
		return `{"version":1,"siblingEnv":{"HQ_HOST":"hqpool","WEB_HOST":"webpool"},"servers":[{"name":"` + server + `","port":` + port + `,"command":"run ` + server + `"}]}`
	}
	web := devservertest.NewSupervised(decl("web", "3000"))
	ssh := &rewireLeaseSSH{containers: map[string]*devservertest.Container{"hqpool-3": devservertest.NewSupervised(decl("api", "8080")), "webpool-7": web}}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), ssh)
	defer cleanup()

	type result struct {
		Leases []struct {
			Alias  string `json:"alias"`
			State  string `json:"state"`
			Wiring string `json:"wiring"`
			Note   string `json:"note"`
		} `json:"leases"`
		Rewired []struct {
			Alias      string `json:"alias"`
			SiblingEnv struct {
				Set map[string]string `json:"set"`
			} `json:"siblingEnv"`
			Restarted []string `json:"restarted"`
		} `json:"rewired"`
	}
	call := func(args map[string]any) result {
		t.Helper()
		res := callAndGetResult(t, session, "zerops_lease", args)
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("zerops_lease %v: %s", args, text)
		}
		if strings.Contains(text, leaseIntLease) || strings.Contains(text, leaseIntToken) {
			t.Fatalf("result carries a secret: %s", text)
		}
		var out result
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("result is not JSON: %v\n%s", err, text)
		}
		return out
	}
	acquire := map[string]any{"action": "acquire", "pools": []string{"hqpool", "webpool"}, "repo": "https://github.com/acme/app", "branch": "feat-x"}
	call(acquire)

	got := call(map[string]any{"action": "release", "lease": "hqpool-3"})
	want := map[string]string{"WEB_HOST": "node-id-7.runtime.webpool.zerops"}
	if len(got.Rewired) != 1 || got.Rewired[0].Alias != "webpool-7" || !maps.Equal(got.Rewired[0].SiblingEnv.Set, want) || len(got.Rewired[0].Restarted) != 1 {
		t.Errorf("release rewired = %+v, want webpool-7 with %v and web restarted", got.Rewired, want)
	}
	if !maps.Equal(web.Overlay(), want) {
		t.Errorf("webpool-7's overlay = %v, want %v", web.Overlay(), want)
	}

	call(acquire) // hqpool-3 again: webpool-7 is rewired with it
	// hqpool-3's lease ends outside zcp: its orchestrator releases it.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, hqTarget.String()+"/container/c-hqpool/release?reset=true", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+leaseIntToken)
	req.Header.Set("X-Swarm-Lease", "hqpool-"+leaseIntLease)
	resp, err := orig.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	sent := len(web.Sent())

	status := call(map[string]any{"action": "status"})
	var stale bool
	for _, l := range status.Leases {
		if l.Alias == "webpool-7" {
			stale = l.Wiring == "stale" && strings.Contains(l.Note, "HQ_HOST") && strings.Contains(l.Note, `zerops_dev_server action=restart hostname="webpool-7"`)
		}
	}
	if !stale {
		t.Errorf("status = %+v, want webpool-7 wiring stale naming HQ_HOST and the call", status.Leases)
	}
	for _, cmd := range web.Sent()[sent:] {
		if strings.Contains(cmd, "ctl 'env'") || strings.Contains(cmd, "ctl 'reload'") {
			t.Errorf("status sent %q", cmd)
		}
	}
}
