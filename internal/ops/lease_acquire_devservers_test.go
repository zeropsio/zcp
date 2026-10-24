// Tests for: ops/lease_group.go and ops/lease_prepare.go — an acquire's
// result carries, for each lease it took, the dev servers zcp left running
// there as the supervisor reports them at the end of the acquire — running,
// or starting while their port does not answer yet — with the preview
// server's preview URL and the sibling values the servers were handed, so no
// status call per lease is needed to learn them (docs/spec-workflows.md §8
// "Dev pools (DP)").
package ops

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// acquirePreviewDeclaration is rewireDeclaration with each lease's preview on
// 8000 + its container's stable number.
func acquirePreviewDeclaration(server, port string) string {
	return `{"version":1,"previewPortBase":8000,"siblingEnv":{"HQ_HOST":"hqpool","WEB_HOST":"webpool"},"servers":[{"name":"` + server + `","port":` + port + `,"command":"run ` + server + `"}]}`
}

// previewSubdomainHost is the project's subdomain host the preview URLs are
// built on.
const previewSubdomainHost = "abc1.prg1.zerops.app"

// previewPoolService is a pool whose subdomain is on and whose run.ports
// carry the preview ports 8001–8010 with HTTP.
func previewPoolService(id, name string) platform.ServiceStack {
	svc := poolService(id, name)
	svc.SubdomainAccess = true
	for p := 8001; p <= 8010; p++ {
		svc.Ports = append(svc.Ports, platform.Port{Port: p, Protocol: "tcp", HTTPSupport: true})
	}
	return svc
}

// previewMock is the group fixture's project with the preview pools.
func previewMock() *platform.Mock {
	return platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "proj", SubdomainHost: previewSubdomainHost}).
		WithServices([]platform.ServiceStack{previewPoolService("svc-hq", groupHQPool), previewPoolService("svc-web", groupWebPool)}).
		WithServiceEnv("svc-hq", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "hq-token", Sensitive: true}}).
		WithServiceEnv("svc-web", []platform.ServiceEnvVar{{Key: "API_TOKEN", Content: "web-token", Sensitive: true}})
}

// acquiredView is a lease of an acquire's result as the agent reads it.
type acquiredView struct {
	Alias      string `json:"alias"`
	Reused     bool   `json:"reused"`
	Note       string `json:"note"`
	DevServers []struct {
		Name   string `json:"name"`
		Port   int    `json:"port"`
		URL    string `json:"url"`
		Status string `json:"status"`
	} `json:"devServers"`
	SiblingEnv *struct {
		Set                map[string]string `json:"set"`
		LeftToContainerEnv []string          `json:"leftToContainerEnv"`
	} `json:"siblingEnv"`
}

// acquiredViews reads views as their JSON says, by alias.
func acquiredViews(t *testing.T, views []LeaseView) map[string]acquiredView {
	t.Helper()
	b, err := json.Marshal(views)
	if err != nil {
		t.Fatal(err)
	}
	var out []acquiredView
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	byAlias := map[string]acquiredView{}
	for _, v := range out {
		byAlias[v.Alias] = v
	}
	return byAlias
}

// TestLeaseAcquire_ResultCarriesEachDevServerAsTheAcquireLeftIt: after an
// acquire the agent needed one zerops_dev_server status per lease to learn its
// servers' health and preview URL. Each lease an acquire takes — in a group,
// alone, or joining a branch mid-way — now carries its dev servers as one read
// of its supervisor at the end of the acquire finds them: hq's api running,
// web's server still starting (its port does not answer yet — the status call
// is the follow-up, nothing is waited for), the preview server's preview URL
// on its own preview port, and the sibling values the servers were handed. A
// lease returned reused was not touched and carries none; a pool without
// zcp-devsup serves no dev server.
func TestLeaseAcquire_ResultCarriesEachDevServerAsTheAcquireLeftIt(t *testing.T) {
	t.Parallel()
	siblings := map[string]string{"HQ_HOST": rewireHQHost, "WEB_HOST": rewireWebHost}
	hqURL, webURL := "https://hqpool-abc1-8001.prg1.zerops.app", "https://webpool-abc1-8002.prg1.zerops.app"
	tests := []struct {
		name string
		// first is an acquire made before the one whose result is read.
		first, pools []string
		// noDevsup: hqpool's image carries no zcp-devsup.
		noDevsup bool
		want     map[string]struct {
			server, status, url string
			port                int
		}
		wantReused []string
	}{
		{
			name: "a group acquire", pools: []string{groupHQPool, groupWebPool},
			want: map[string]struct {
				server, status, url string
				port                int
			}{"hqpool-3": {"api", "running", hqURL, 8080}, "webpool-7": {"web", "starting", webURL, 3000}},
		},
		{
			name: "a part joining the branch mid-way", first: []string{groupHQPool}, pools: []string{groupWebPool},
			want: map[string]struct {
				server, status, url string
				port                int
			}{"webpool-7": {"web", "starting", webURL, 3000}},
		},
		{
			name: "a lease returned reused and one taken", first: []string{groupHQPool}, pools: []string{groupHQPool, groupWebPool},
			want: map[string]struct {
				server, status, url string
				port                int
			}{"webpool-7": {"web", "starting", webURL, 3000}},
			wantReused: []string{"hqpool-3"},
		},
		{
			name: "a pool without zcp-devsup", pools: []string{groupHQPool, groupWebPool}, noDevsup: true,
			want: map[string]struct {
				server, status, url string
				port                int
			}{"webpool-7": {"web", "starting", webURL, 3000}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rf := newRewireFixture(t)
			rf.deps.Client = previewMock()
			rf.hq = devservertest.NewSupervised(acquirePreviewDeclaration("api", "8080"))
			rf.web = devservertest.NewSupervised(acquirePreviewDeclaration("web", "3000"))
			rf.hq.StableNumber, rf.web.StableNumber = 1, 2
			rf.web.Settle = map[string]int{"web": 100} // still compiling its first answer
			if tt.noDevsup {
				rf.hq = &devservertest.Container{Declaration: acquirePreviewDeclaration("api", "8080")}
			}
			rf.ssh.containers = map[string]*devservertest.Container{"hqpool-3": rf.hq, "webpool-7": rf.web}
			if tt.first != nil {
				rf.acquire(t, tt.first...)
			}
			reads := len(rf.ssh.sent("webpool-7", "@@zcp-devsup listen"))

			change := rf.acquire(t, tt.pools...)

			views := acquiredViews(t, change.Leases)
			for alias, want := range tt.want {
				v := views[alias]
				if len(v.DevServers) != 1 {
					t.Fatalf("%s devServers = %+v, want %s alone", alias, v.DevServers, want.server)
				}
				if got := v.DevServers[0]; got.Name != want.server || got.Status != want.status || got.URL != want.url || got.Port != want.port {
					t.Errorf("%s dev server = %+v, want %s %s on :%d at %s", alias, got, want.server, want.status, want.port, want.url)
				}
				if v.SiblingEnv == nil || !maps.Equal(v.SiblingEnv.Set, siblings) {
					t.Errorf("%s siblingEnv = %+v, want %v", alias, v.SiblingEnv, siblings)
				}
			}
			for _, alias := range tt.wantReused {
				if v := views[alias]; !v.Reused || v.DevServers != nil || v.SiblingEnv != nil {
					t.Errorf("%s = %+v, want reused with no dev servers read", alias, v)
				}
			}
			if tt.noDevsup {
				if v := views["hqpool-3"]; v.DevServers != nil || v.SiblingEnv != nil || !strings.Contains(v.Note, "not a dev pool") {
					t.Errorf("hqpool-3 = %+v, want no dev servers and the note that the pool is not a dev pool", v)
				}
			}
			if n := len(rf.ssh.sent("webpool-7", "@@zcp-devsup listen")) - reads; n != 1 {
				t.Errorf("webpool-7's dev servers were read %d times by the acquire, want once: nothing is waited for", n)
			}
		})
	}
}

// TestLeaseAcquire_DevServerNotAnsweringYet_NamedForTheStatusCall: a server
// still starting when the acquire ends is reported starting — never waited for
// — and a crashed one crashed; neither fails the acquire.
func TestLeaseAcquire_DevServerNotAnsweringYet_NamedForTheStatusCall(t *testing.T) {
	t.Parallel()
	rf := newRewireFixture(t)
	rf.deps.Client = previewMock()
	rf.hq = devservertest.NewSupervised(`{"version":1,"servers":[{"name":"api","port":8080,"command":"run api"},{"name":"worker","command":"run worker"}]}`)
	rf.hq.Crash = map[string]bool{"worker": true}
	rf.hq.Settle = map[string]int{"api": 100}
	rf.ssh.containers = map[string]*devservertest.Container{"hqpool-3": rf.hq}

	change := rf.acquire(t, groupHQPool)

	v := acquiredViews(t, change.Leases)["hqpool-3"]
	got := map[string]string{}
	for _, s := range v.DevServers {
		got[s.Name] = s.Status
	}
	want := map[string]string{"api": string(devsup.StatusStarting), "worker": string(devsup.StatusCrashed)}
	if !maps.Equal(got, want) {
		t.Errorf("devServers = %v, want %v", got, want)
	}
	if v.SiblingEnv != nil {
		t.Errorf("siblingEnv = %+v, want none: the declaration names no sibling variable", v.SiblingEnv)
	}
}
