// Tests for: integration — a lease always has its supervisor
// (docs/spec-workflows.md §8 DP-13), through the full MCP server against a mock
// platform, a fake pool orchestrator and a fake leased container: a reused
// container (reset at its last release, so its run.start never runs again)
// gets zcp-devsup started at acquire, and again by zerops_dev_server after it
// is gone; a pool whose image has none is named as no dev pool.

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// supervisorPoolOrchestrator is a pool orchestrator holding container #3 and
// routed into http.DefaultTransport for the test's life.
func supervisorPoolOrchestrator(t *testing.T) {
	t.Helper()
	var mu sync.Mutex
	reserved := false
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lock := ""
		if reserved {
			lock = "reserved"
		}
		container := `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE",` +
			`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + lock + `","dirty":false`
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/container":
			_, _ = w.Write([]byte(`[` + container + `}]`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/container/"):
			_, _ = w.Write([]byte(container + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
			reserved = true
			_, _ = w.Write([]byte(strings.Replace(container, `"lockState":""`, `"lockState":"reserved"`, 1) + `,"lease":"` + leaseIntLease + `"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(orch.Close)
	target, err := url.Parse(orch.URL)
	if err != nil {
		t.Fatal(err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoute{host: "orch.devpool.zerops", target: target, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })
}

func supervisorPoolMock() *platform.Mock {
	return defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
}

type supervisorLeaseResult struct {
	Leases []struct {
		Alias   string `json:"alias"`
		State   string `json:"state"`
		Note    string `json:"note"`
		Prepare *struct {
			Command string `json:"command"`
			OK      bool   `json:"ok"`
		} `json:"prepare"`
	} `json:"leases"`
}

func TestLease_Integration_ReusedContainerGetsItsSupervisor(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir the server resolves from the cwd)
	// and a swap of http.DefaultTransport (the seam to the orchestrator host).
	t.Chdir(t.TempDir())
	supervisorPoolOrchestrator(t)
	// A reused container: zcp-devsup is in the image, nothing runs it.
	// Its branch declares the incremental install its lockfile needs.
	c := &devservertest.Container{Installed: true, SupervisorVersion: "v9.300.0", Formats: []int{1},
		Declaration: `{"version":1,"prepare":"npm install --prefer-offline","servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`}
	session, cleanup := setupTestServerWithDeploy(t, supervisorPoolMock(), defaultLogFetcher(), c)
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feature-x"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire failed: %s", text)
	}
	var acquired supervisorLeaseResult
	if err := json.Unmarshal([]byte(text), &acquired); err != nil {
		t.Fatalf("acquire result is not JSON: %v\n%s", err, text)
	}
	if len(acquired.Leases) != 1 || acquired.Leases[0].State != "held" || acquired.Leases[0].Note != "" {
		t.Fatalf("acquire = %+v, want devpool-3 held with no note", acquired.Leases)
	}
	if c.SupervisorStarts != 1 {
		t.Fatalf("supervisors started at acquire = %d, want one", c.SupervisorStarts)
	}
	// The prepare ran once, before the supervisor started the declared servers.
	if p := acquired.Leases[0].Prepare; p == nil || !p.OK || p.Command != "npm install --prefer-offline" || len(c.Prepared) != 1 {
		t.Fatalf("prepare = %+v (ran %v), want the declared command run once, ok", p, c.Prepared)
	}
	prepare, start := -1, -1
	for i, cmd := range c.Sent() {
		switch {
		case strings.Contains(cmd, "/bin/sh -c ") && prepare < 0:
			prepare = i
		case strings.Contains(cmd, "zcp-devsup run") && start < 0:
			start = i
		}
	}
	if prepare < 0 || start < prepare {
		t.Errorf("the prepare (%d) did not run before the supervisor started (%d)", prepare, start)
	}

	status := func() map[string]any {
		t.Helper()
		res := callAndGetResult(t, session, "zerops_dev_server", map[string]any{"action": "status", "hostname": "devpool-3"})
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("dev server status failed: %s", text)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("status result is not JSON: %v\n%s", err, text)
		}
		return out
	}
	if got := status(); got["running"] != true || c.SupervisorStarts != 1 {
		t.Fatalf("status = %v (starts %d), want web running under the one supervisor", got, c.SupervisorStarts)
	}

	// The container restarts (or the supervisor dies): nothing starts it but zcp.
	c.Running = false
	if got := status(); got["running"] != true || got["reason"] == "no_supervisor" {
		t.Fatalf("status after the supervisor went = %v, want it started again and web running", got)
	}
	if c.SupervisorStarts != 2 {
		t.Errorf("supervisors started = %d, want a second one after the first went", c.SupervisorStarts)
	}
}

func TestLease_Integration_PoolWithoutZcpDevsup_SaysNotADevPool(t *testing.T) {
	// Non-parallel: t.Chdir and the http.DefaultTransport swap (see above).
	t.Chdir(t.TempDir())
	supervisorPoolOrchestrator(t)
	// A pool used as a sandbox for running code: its image has no zcp-devsup.
	c := &devservertest.Container{}
	session, cleanup := setupTestServerWithDeploy(t, supervisorPoolMock(), defaultLogFetcher(), c)
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire failed: %s", text)
	}
	var acquired supervisorLeaseResult
	if err := json.Unmarshal([]byte(text), &acquired); err != nil {
		t.Fatalf("acquire result is not JSON: %v\n%s", err, text)
	}
	if len(acquired.Leases) != 1 || acquired.Leases[0].State != "held" {
		t.Fatalf("acquire = %+v, want devpool-3 held", acquired.Leases)
	}
	for _, want := range []string{"zcp-devsup", "not a dev pool"} {
		if !strings.Contains(acquired.Leases[0].Note, want) {
			t.Errorf("note %q lacks %q", acquired.Leases[0].Note, want)
		}
	}
	if c.SupervisorStarts != 0 {
		t.Errorf("supervisors started = %d, want none without the binary", c.SupervisorStarts)
	}
}
