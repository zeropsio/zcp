// Tests for: integration — the tools that reach a lease's container over ssh
// (zerops_dev_server, zerops_verify, zerops_deploy) through the full MCP
// server: each asks the pool's orchestrator whose the container is first
// (docs/spec-workflows.md §8 DP-2), and a lease that is not zcp's — another
// holder took the container — or that the orchestrator does not answer about
// is refused before anything reaches the container.

package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// reachOrch is a pool orchestrator holding container #3 of devpool: an
// acquire reserves it for leaseIntLease; a renew answers its holder's lease
// and refuses any other unknown-lease (live, 2026-10-24); renewFails answers
// every renew 500.
type reachOrch struct {
	mu         sync.Mutex
	reserved   bool
	holder     string
	renewFails bool
}

func (o *reachOrch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	lock := ""
	if o.reserved {
		lock = "reserved"
	}
	container := `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE",` +
		`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + lock + `","dirty":false`
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/container":
		_, _ = w.Write([]byte(`[` + container + `}]`))
	case r.Method == http.MethodGet && r.URL.Path == "/container/c-abc":
		_, _ = w.Write([]byte(container + `,"snapshots":1}`))
	case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
		o.reserved, o.holder = true, leaseIntLease
		_, _ = w.Write([]byte(strings.Replace(container, `"lockState":""`, `"lockState":"reserved"`, 1) + `,"lease":"` + leaseIntLease + `"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/renew":
		switch {
		case o.renewFails:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"internalServerError","message":"the orchestrator could not read its reservations"}}`))
		case !o.reserved || r.Header.Get("X-Swarm-Lease") != o.holder:
			writeIntUnknownLease(w)
		default:
			_, _ = w.Write([]byte(container + `}`))
		}
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (o *reachOrch) set(fn func(o *reachOrch)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fn(o)
}

// reachSSH is a leased container that records every host zcp reaches.
type reachSSH struct {
	*devservertest.Container
	mu    sync.Mutex
	hosts []string
}

func (s *reachSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	s.hosts = append(s.hosts, host)
	s.mu.Unlock()
	return s.Container.ExecSSH(ctx, host, command)
}

func (s *reachSSH) ExecSSHBackground(ctx context.Context, host, command string, timeout time.Duration) ([]byte, error) {
	s.mu.Lock()
	s.hosts = append(s.hosts, host)
	s.mu.Unlock()
	return s.Container.ExecSSHBackground(ctx, host, command, timeout)
}

func (s *reachSSH) forget() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hosts = nil
}

func (s *reachSSH) reached(host string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.hosts {
		if h == host {
			return true
		}
	}
	return false
}

// TestLease_Integration_LeaseNotOurs_ToolsNeverReachIt: the live recheck of
// 2026-10-24 — a lease released outside zcp whose container another holder
// reserved, then zerops_dev_server ssh'd into it (and started zcp-devsup in
// the other holder's container) and zerops_verify called it healthy. Every
// tool that reaches a lease's container asks the orchestrator first: another
// holder's is refused naming the release that drops the record, and one the
// orchestrator does not answer about is refused naming the status that asks
// again — no ssh reaches the container and no supervisor starts in it.
func TestLease_Integration_LeaseNotOurs_ToolsNeverReachIt(t *testing.T) {
	for _, tt := range []struct {
		name string
		end  func(o *reachOrch)
		want string
	}{
		{name: "another holder took it", want: "zerops_lease action=release lease=devpool-3",
			end: func(o *reachOrch) { o.holder = "another-consumers-lease-0123456789" }},
		{name: "the orchestrator does not answer", want: "zerops_lease action=status",
			end: func(o *reachOrch) { o.renewFails = true }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Non-parallel: t.Chdir (the state dir) and a swap of
			// http.DefaultTransport (the orchestrator host).
			t.Chdir(t.TempDir())
			orch := &reachOrch{}
			hs := httptest.NewServer(orch)
			t.Cleanup(hs.Close)
			target, err := url.Parse(hs.URL)
			if err != nil {
				t.Fatal(err)
			}
			orig := http.DefaultTransport
			http.DefaultTransport = orchestratorRoute{host: "orch.devpool.zerops", target: target, next: orig}
			t.Cleanup(func() { http.DefaultTransport = orig })

			c := &devservertest.Container{Installed: true, SupervisorVersion: "v9.300.0", Formats: []int{1},
				Declaration: `{"version":1,"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`, YAMLFile: "zerops.yaml", YAML: devservertest.SupervisedYAML}
			ssh := &reachSSH{Container: c}
			mock := defaultMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
					{ID: "svc-1", Name: "app", Status: "RUNNING", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
				}).
				WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
			session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), ssh)
			defer cleanup()

			if res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feature-x"}); res.IsError {
				t.Fatalf("acquire: %s", getTextContent(t, res))
			}
			orch.set(tt.end)
			ssh.forget()
			c.Stop() // the other holder's container runs no supervisor of zcp's
			starts := c.SupervisorStarts

			for _, call := range []struct {
				tool string
				args map[string]any
			}{
				{"zerops_dev_server", map[string]any{"action": "status", "hostname": "devpool-3"}},
				{"zerops_dev_server", map[string]any{"action": "start", "hostname": "devpool-3", "name": "web"}},
				{"zerops_verify", map[string]any{"serviceHostname": "devpool-3"}},
				{"zerops_deploy", map[string]any{"sourceService": "devpool-3", "targetService": "app"}},
			} {
				res := callAndGetResult(t, session, call.tool, call.args)
				text := getTextContent(t, res)
				if !res.IsError || !strings.Contains(text, tt.want) {
					t.Errorf("%s %v = (error %v) %s; want a refusal naming %q", call.tool, call.args, res.IsError, text, tt.want)
				}
				if strings.Contains(text, leaseIntLease) {
					t.Errorf("%s result carries the lease: %s", call.tool, text)
				}
			}
			if ssh.reached("devpool-3") {
				t.Errorf("a tool reached devpool-3 over ssh, a container zcp cannot vouch for")
			}
			if c.SupervisorStarts != starts {
				t.Errorf("zcp started a supervisor in a container that is not its lease's (%d → %d)", starts, c.SupervisorStarts)
			}
		})
	}
}
