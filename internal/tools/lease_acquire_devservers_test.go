// Tests for: tools/lease.go — zerops_lease action=acquire carries, per lease it
// took, the dev servers zcp left there and the sibling values they were
// handed, and nextActions names the status call for a server still starting
// (docs/spec-workflows.md §8 "Dev pools (DP)").
package tools

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// TestLeaseTool_Acquire_CarriesEachLeasesDevServers: a group acquire's result
// names, per lease, each dev server with its status as the acquire left it
// and the sibling values its servers run with — never a lease string — and
// nextActions names zerops_dev_server action=status for the server whose port
// did not answer yet.
func TestLeaseTool_Acquire_CarriesEachLeasesDevServers(t *testing.T) {
	t.Parallel()
	ssh, _, web := newRewireToolSSH()
	// webpool-7 is a reused container: zcp starts its supervisor, and web is
	// still compiling its first answer when the acquire ends.
	web.Running, web.Servers = false, nil
	web.Settle = map[string]int{"web": 100}
	srv, orchs, _ := groupLeaseServerSSH(t, ssh)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"hqpool", "webpool"}, "repo": leaseTestRepo, "branch": "feat-x"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire: %s", text)
	}
	for _, o := range orchs {
		if strings.Contains(text, o.lease()) || strings.Contains(text, o.token) {
			t.Fatalf("result carries a secret: %s", text)
		}
	}
	var got struct {
		Leases []struct {
			Alias      string `json:"alias"`
			DevServers []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"devServers"`
			SiblingEnv *struct {
				Set map[string]string `json:"set"`
			} `json:"siblingEnv"`
		} `json:"leases"`
		NextActions string `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	want := map[string]string{"hqpool-3": "api running", "webpool-7": "web starting"}
	siblings := map[string]string{"HQ_HOST": "node-id-3.runtime.hqpool.zerops", "WEB_HOST": "node-id-7.runtime.webpool.zerops"}
	if len(got.Leases) != 2 {
		t.Fatalf("leases = %+v, want hqpool-3 and webpool-7", got.Leases)
	}
	for _, l := range got.Leases {
		if len(l.DevServers) != 1 || l.DevServers[0].Name+" "+l.DevServers[0].Status != want[l.Alias] {
			t.Errorf("%s devServers = %+v, want %s", l.Alias, l.DevServers, want[l.Alias])
		}
		if l.SiblingEnv == nil || !maps.Equal(l.SiblingEnv.Set, siblings) {
			t.Errorf("%s siblingEnv = %+v, want %v", l.Alias, l.SiblingEnv, siblings)
		}
	}
	if !strings.Contains(got.NextActions, `zerops_dev_server action=status hostname="webpool-7" name="web"`) {
		t.Errorf("nextActions %q should name the status call for web, still starting", got.NextActions)
	}
	if strings.Contains(got.NextActions, `hostname="hqpool-3" name="api"`) {
		t.Errorf("nextActions %q names api, which runs", got.NextActions)
	}
}

// TestLeaseTool_Acquire_SwitchesThePoolsPublicAccessOn: an acquire that starts
// a lease's dev servers itself runs the public-access step zerops_dev_server's
// start runs — once per pool, the eligibility and the enable-once stamp as
// ever — so the lease's preview answers from outside and devServers carries
// its URL, though the pool reports its preview port without httpSupport while
// its subdomain is off. In all three sessions of 2026-10-27 every preview
// answered 502 after an acquire until the agent enabled each pool by hand.
func TestLeaseTool_Acquire_SwitchesThePoolsPublicAccessOn(t *testing.T) {
	t.Parallel()
	web := devservertest.NewSupervised(`{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"run web"}]}`)
	web.StableNumber = 1
	ssh := &rewireToolSSH{containers: map[string]*devservertest.Container{"webpool-7": web}}
	srv, _, _, mock := groupLeaseServerWith(t, ssh, func(m *platform.Mock) {
		pool := platform.ServiceStack{
			ID: "svc-webpool", Name: "webpool", ProjectID: "proj-1", Status: "ACTIVE",
			Ports:                []platform.Port{{Port: 9001, Protocol: "tcp"}, {Port: 9002, Protocol: "tcp"}, {Port: 9003, Protocol: "tcp"}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"},
		}
		m.WithProject(&platform.Project{ID: "proj-1", Name: "proj", Status: statusActive, SubdomainHost: "abc1.prg1.zerops.app"}).
			WithService(&pool).
			WithProcess(&platform.Process{ID: "proc-subdomain-enable-svc-webpool", Status: statusFinished})
	})

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "webpool", "repo": leaseTestRepo, "branch": "feat-x"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire: %s", text)
	}
	if n := mock.CallCounts["EnableSubdomainAccess"]; n != 1 {
		t.Fatalf("EnableSubdomainAccess calls = %d, want 1 — the pool's, once: %s", n, text)
	}
	var got struct {
		Leases []struct {
			Alias      string `json:"alias"`
			DevServers []struct {
				Name string `json:"name"`
				URL  string `json:"url"`
			} `json:"devServers"`
		} `json:"leases"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(got.Leases) != 1 || len(got.Leases[0].DevServers) != 1 || got.Leases[0].DevServers[0].URL != "https://webpool-abc1-9001.prg1.zerops.app" {
		t.Fatalf("leases = %+v, want webpool-7's web at its preview URL", got.Leases)
	}
}
