// Tests for: integration — an acquire's result carries each lease's dev
// servers as zcp left them, with the preview URL of each lease's own preview
// port and the sibling values its servers were handed (docs/spec-workflows.md
// §8 "Dev pools (DP)").

package integration_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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

// previewLeaseSSH is rewireLeaseSSH whose checkout answers each container's
// stable number (its ZEROPS_StableNumber) from slots.
type previewLeaseSSH struct {
	rewireLeaseSSH
	mu    sync.Mutex
	slots map[string]int
}

func (s *previewLeaseSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	out, err := s.rewireLeaseSSH.ExecSSH(ctx, host, command)
	if strings.Contains(command, "fetch -q --prune origin") {
		s.mu.Lock()
		if n := s.slots[host]; n > 0 {
			out = append(out, []byte("@@zcp-lease slot "+strconv.Itoa(n)+"\n")...)
		}
		s.mu.Unlock()
	}
	return out, err
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *previewLeaseSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *previewLeaseSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// previewGroupServer is the full MCP server over hqpool #3 (stable number 1)
// and webpool #7 (stable number 2), each container running zcp-devsup with
// decl(server, port); it answers acquire and release through each pool's
// orchestrator, and hands back webpool's container.
func previewGroupServer(t *testing.T, decl func(server, port string) string) (call func(map[string]any) string, web *devservertest.Container) {
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
	hq, web := devservertest.NewSupervised(decl("api", "8080")), devservertest.NewSupervised(decl("web", "3000"))
	hq.StableNumber, web.StableNumber = 1, 2
	ssh := &previewLeaseSSH{
		rewireLeaseSSH: rewireLeaseSSH{containers: map[string]*devservertest.Container{"hqpool-3": hq, "webpool-7": web}},
		slots:          map[string]int{"hqpool-3": 1, "webpool-7": 2},
	}
	session, cleanup := setupLeaseTestServer(t, mock, defaultLogFetcher(), ssh)
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
	}, web
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
	call, web := previewGroupServer(t, func(server, port string) string {
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

// TestLease_Integration_SiblingPreviewEnv_FollowsTheSiblingsContainer: through
// the full MCP server, a declaration's siblingPreviewEnv hands web's servers
// hq's preview URL — the address a browser reaches it on — beside the
// container hostname siblingEnv hands them: the acquire names both per lease;
// when hqpool-3 leaves the branch, web is rewired without either and
// restarted, and the result says so.
func TestLease_Integration_SiblingPreviewEnv_FollowsTheSiblingsContainer(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport, as in lease_test.go.
	t.Chdir(t.TempDir())
	call, web := previewGroupServer(t, func(server, port string) string {
		return `{"version":1,"previewPortBase":8000,"siblingEnv":{"HQ_HOST":"hqpool"},"siblingPreviewEnv":{"HQ_URL":"hqpool"},` +
			`"servers":[{"name":"` + server + `","port":` + port + `,"command":"run ` + server + `"}]}`
	})
	acquire := map[string]any{"action": "acquire", "pools": []string{"hqpool", "webpool"}, "repo": "https://github.com/acme/app", "branch": "feat-x"}

	got := readAcquired(t, call(acquire))
	want := map[string]string{"HQ_HOST": "node-id-3.runtime.hqpool.zerops", "HQ_URL": "https://hqpool-abc1-8001.prg1.zerops.app"}
	for _, l := range got.Leases {
		if l.SiblingEnv == nil || !maps.Equal(l.SiblingEnv.Set, want) {
			t.Errorf("%s siblingEnv = %+v, want %v", l.Alias, l.SiblingEnv, want)
		}
	}
	if !maps.Equal(web.Overlay(), want) {
		t.Errorf("webpool-7's overlay = %v, want %v", web.Overlay(), want)
	}

	var released struct {
		Rewired []struct {
			Alias      string `json:"alias"`
			SiblingEnv struct {
				LeftToContainerEnv []string `json:"leftToContainerEnv"`
			} `json:"siblingEnv"`
			Restarted []string `json:"restarted"`
		} `json:"rewired"`
	}
	text := call(map[string]any{"action": "release", "lease": "hqpool-3"})
	if err := json.Unmarshal([]byte(text), &released); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(released.Rewired) != 1 || released.Rewired[0].Alias != "webpool-7" ||
		!slices.Equal(released.Rewired[0].SiblingEnv.LeftToContainerEnv, []string{"HQ_HOST", "HQ_URL"}) || !slices.Equal(released.Rewired[0].Restarted, []string{"web"}) {
		t.Errorf("release rewired = %+v, want webpool-7 with HQ_HOST and HQ_URL left to its container's env and web restarted", released.Rewired)
	}
	if len(web.Overlay()) != 0 {
		t.Errorf("webpool-7's overlay = %v, want none once hqpool-3 left", web.Overlay())
	}
}
