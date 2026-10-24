// Tests for: integration — an acquire's result carries each lease's dev
// servers as zcp left them, with the preview URL of each lease's own preview
// port and the sibling values its servers were handed (docs/spec-workflows.md
// §8 "Dev pools (DP)").

package integration_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// previewPool is a swarm pool whose subdomain is on and whose run.ports carry
// the preview ports 8001–8010 with HTTP.
func previewPool(id, name string) platform.ServiceStack {
	svc := platform.ServiceStack{ID: id, Name: name, Status: "ACTIVE", SubdomainAccess: true,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}
	for p := 8001; p <= 8010; p++ {
		svc.Ports = append(svc.Ports, platform.Port{Port: p, Protocol: "tcp", HTTPSupport: true})
	}
	return svc
}

// previewGroupServer is the full MCP server over hqpool #3 (stable number 1)
// and webpool #7 (stable number 2), each container running zcp-devsup with
// decl(server, port); it answers acquire and release through each pool's
// orchestrator.
func previewGroupServer(t *testing.T, decl func(server, port string) string) (call func(map[string]any) string, hq, web *devservertest.Container) {
	t.Helper()
	hqTarget, _, _ := groupPoolOrchestratorReleasing(t, "hqpool", "3")
	webTarget, _, _ := groupPoolOrchestratorReleasing(t, "webpool", "7")
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoutes{targets: map[string]*url.URL{"orch.hqpool.zerops": hqTarget, "orch.webpool.zerops": webTarget}, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test-project", Status: "ACTIVE", SubdomainHost: "abc1.prg1.zerops.app"}).
		WithServices([]platform.ServiceStack{previewPool("svc-hqpool", "hqpool"), previewPool("svc-webpool", "webpool")}).
		WithServiceEnv("svc-hqpool", []platform.ServiceEnvVar{{ID: "env-h", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}}).
		WithServiceEnv("svc-webpool", []platform.ServiceEnvVar{{ID: "env-w", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	hq, web = devservertest.NewSupervised(decl("api", "8080")), devservertest.NewSupervised(decl("web", "3000"))
	hq.StableNumber, web.StableNumber = 1, 2
	ssh := &rewireLeaseSSH{containers: map[string]*devservertest.Container{"hqpool-3": hq, "webpool-7": web}}
	session, cleanup := setupTestServerWithDeploy(t, mock, defaultLogFetcher(), ssh)
	t.Cleanup(cleanup)
	return func(args map[string]any) string {
		t.Helper()
		res := callAndGetResult(t, session, "zerops_lease", args)
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("zerops_lease %v: %s", args, text)
		}
		if strings.Contains(text, leaseIntLease) || strings.Contains(text, leaseIntToken) {
			t.Fatalf("result carries a secret: %s", text)
		}
		return text
	}, hq, web
}

// acquiredLeases is an acquire's result as the agent reads it.
type acquiredLeases struct {
	Leases []struct {
		Alias      string `json:"alias"`
		DevServers []struct {
			Name   string `json:"name"`
			URL    string `json:"url"`
			Status string `json:"status"`
		} `json:"devServers"`
		SiblingEnv *struct {
			Set map[string]string `json:"set"`
		} `json:"siblingEnv"`
	} `json:"leases"`
	NextActions string `json:"nextActions"`
}

func readAcquired(t *testing.T, text string) acquiredLeases {
	t.Helper()
	var out acquiredLeases
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return out
}

// TestLease_Integration_Acquire_CarriesEachLeasesDevServers: through the full
// MCP server, a group acquire names per lease its dev server, its status as
// the acquire left it (web still compiling: starting, with the status call in
// nextActions), its preview URL on the lease's own preview port, and the
// sibling hostnames its servers were handed.
func TestLease_Integration_Acquire_CarriesEachLeasesDevServers(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport, as in lease_test.go.
	t.Chdir(t.TempDir())
	call, _, web := previewGroupServer(t, func(server, port string) string {
		return `{"version":1,"previewPortBase":8000,"siblingEnv":{"HQ_HOST":"hqpool","WEB_HOST":"webpool"},"servers":[{"name":"` + server + `","port":` + port + `,"command":"run ` + server + `"}]}`
	})
	// webpool-7 is a reused container: zcp starts its supervisor, and web is
	// still compiling its first answer when the acquire ends.
	web.Running, web.Servers = false, nil
	web.Settle = map[string]int{"web": 100}

	got := readAcquired(t, call(map[string]any{"action": "acquire", "pools": []string{"hqpool", "webpool"}, "repo": "https://github.com/acme/app", "branch": "feat-x"}))

	want := map[string]string{
		"hqpool-3":  "api running https://hqpool-abc1-8001.prg1.zerops.app",
		"webpool-7": "web starting https://webpool-abc1-8002.prg1.zerops.app",
	}
	siblings := map[string]string{"HQ_HOST": "node-id-3.runtime.hqpool.zerops", "WEB_HOST": "node-id-7.runtime.webpool.zerops"}
	if len(got.Leases) != 2 {
		t.Fatalf("leases = %+v, want hqpool-3 and webpool-7", got.Leases)
	}
	for _, l := range got.Leases {
		if len(l.DevServers) != 1 || l.DevServers[0].Name+" "+l.DevServers[0].Status+" "+l.DevServers[0].URL != want[l.Alias] {
			t.Errorf("%s devServers = %+v, want %s", l.Alias, l.DevServers, want[l.Alias])
		}
		if l.SiblingEnv == nil || !maps.Equal(l.SiblingEnv.Set, siblings) {
			t.Errorf("%s siblingEnv = %+v, want %v", l.Alias, l.SiblingEnv, siblings)
		}
	}
	if !strings.Contains(got.NextActions, `zerops_dev_server action=status hostname="webpool-7" name="web"`) {
		t.Errorf("nextActions %q should name the status call for web", got.NextActions)
	}
}
