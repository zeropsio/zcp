// Tests for: ops/lease_wiring.go — siblingPreviewEnv hands a part's dev
// servers the preview URL of the branch's lease on another pool, the address a
// browser reaches it on, resolved wherever the hostname form is: at a lease's
// start, in an acquire's supervisor step and at every rewire. The preview port
// follows the container's stable number (it moved from -8001 to -8002 when a
// part joined again on another container), so it is resolved from zcp's
// records, never hand-copied (docs/spec-workflows.md §8 "Dev pools (DP)").
package ops

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// previewWiringState is wiringState with each record's container stable
// number — 0 leaves it out, as a record written before zcp kept it — written
// as the record files are.
func previewWiringState(t *testing.T, hqStable, webStable int) string {
	t.Helper()
	dir := filepath.Join(t.TempDir())
	leases := filepath.Join(dir, "leases")
	if err := os.MkdirAll(leases, 0o700); err != nil {
		t.Fatal(err)
	}
	stable := func(n int) string {
		if n == 0 {
			return ""
		}
		return `,"stableNumber":` + strconv.Itoa(n)
	}
	for alias, rec := range map[string]string{
		"hqpool-7": `{"poolHostname":"hqpool","poolServiceId":"svc-hq","containerId":"c-7","containerNumber":7,"containerHostname":"node-id-7.runtime.hqpool.zerops",` +
			`"lease":"hq-lease","branch":"feat-x","repo":"` + wiringRepo + `","acquiredAt":"2026-10-23T11:00:00Z"` + stable(hqStable) + `}`,
		"webpool-3": `{"poolHostname":"webpool","poolServiceId":"svc-web","containerId":"c-3","containerNumber":3,"containerHostname":"node-id-3.runtime.webpool.zerops",` +
			`"lease":"web-lease","branch":"feat-x","repo":"` + wiringRepo + `","acquiredAt":"2026-10-23T12:00:00Z"` + stable(webStable) + `}`,
		"apipool-2": `{"poolHostname":"apipool","poolServiceId":"svc-api","containerId":"c-2","containerNumber":2,"containerHostname":"node-id-2.runtime.apipool.zerops",` +
			`"lease":"api-lease","branch":"other","repo":"` + wiringRepo + `","acquiredAt":"2026-10-23T10:00:00Z","stableNumber":4}`,
	} {
		if err := os.WriteFile(filepath.Join(leases, alias+".json"), []byte(rec), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// siblingPreviewClient is the project of hqpool, webpool and apipool, each
// pool's subdomain on and its preview ports 8001–8010 carrying HTTP — but
// hqpool's when hqNoHTTP.
func siblingPreviewClient(hqNoHTTP bool) *platform.Mock {
	hq := previewPoolService("svc-hq", "hqpool")
	if hqNoHTTP {
		hq.Ports = nil
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", SubdomainHost: previewSubdomainHost}).
		WithServices([]platform.ServiceStack{hq, previewPoolService("svc-web", "webpool"), previewPoolService("svc-api", "apipool")})
}

// siblingPreviewDeclaration declares web and the variables its servers read:
// hq's container hostname (the form a server-side call takes), hq's preview
// URL, its own, and the api's (a pool feat-x holds no lease on).
func siblingPreviewDeclaration(base string) string {
	return `{"version":1,` + base + `"siblingEnv":{"HQ_HOST":"hqpool"},"siblingPreviewEnv":{"HQ_URL":"hqpool","SELF_URL":"webpool","API_URL":"apipool"},` +
		`"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`
}

// TestDevServer_LeaseSiblingPreviewEnv_HandsTheSiblingsPreviewURL: a web part
// that reaches HQ from the page needs HQ's preview URL, not its container
// hostname, which only server-side code resolves. A start on webpool-3 hands
// its supervisor, beside HQ_HOST, HQ_URL set to hqpool-7's preview URL —
// https, the pool's subdomain on previewPortBase + hqpool-7's stable number,
// as zerops_dev_server status reports it — and SELF_URL its own. A variable
// whose URL zcp cannot build keeps the container's own value and the message
// says why: a pool the branch holds no lease on, a record written before zcp
// kept the container's stable number, a declaration without previewPortBase,
// a pool whose preview port carries no HTTP.
func TestDevServer_LeaseSiblingPreviewEnv_HandsTheSiblingsPreviewURL(t *testing.T) {
	t.Parallel()
	hqHost := "node-id-7.runtime.hqpool.zerops"
	hqURL, selfURL := "https://hqpool-abc1-8002.prg1.zerops.app", "https://webpool-abc1-8001.prg1.zerops.app"
	tests := []struct {
		name               string
		base               string
		hqStable, webStbl  int
		hqNoHTTP           bool
		wantSet            map[string]string
		wantLeft, wantText []string
	}{
		{
			name: "resolved", base: `"previewPortBase":8000,`, hqStable: 2, webStbl: 1,
			wantSet:  map[string]string{"HQ_HOST": hqHost, "HQ_URL": hqURL, "SELF_URL": selfURL},
			wantLeft: []string{"API_URL"}, wantText: []string{"HQ_URL=" + hqURL, "SELF_URL=" + selfURL, "API_URL", "apipool"},
		},
		{
			name: "a record written before zcp kept the stable number", base: `"previewPortBase":8000,`, webStbl: 1,
			wantSet:  map[string]string{"HQ_HOST": hqHost, "SELF_URL": selfURL},
			wantLeft: []string{"API_URL", "HQ_URL"}, wantText: []string{"HQ_URL", "hqpool-7", "stable number"},
		},
		{
			name: "a declaration without previewPortBase", hqStable: 2, webStbl: 1,
			wantSet:  map[string]string{"HQ_HOST": hqHost},
			wantLeft: []string{"API_URL", "HQ_URL", "SELF_URL"}, wantText: []string{"HQ_URL", "previewPortBase"},
		},
		{
			name: "hqpool's preview port carries no HTTP", base: `"previewPortBase":8000,`, hqStable: 2, webStbl: 1, hqNoHTTP: true,
			wantSet:  map[string]string{"HQ_HOST": hqHost, "SELF_URL": selfURL},
			wantLeft: []string{"API_URL", "HQ_URL"}, wantText: []string{"HQ_URL", "8002"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(siblingPreviewDeclaration(tt.base))
			result, err := ExecuteDevServer(context.Background(), c, siblingPreviewClient(tt.hqNoHTTP), "p1",
				DevServerParams{Action: "start", Hostname: "webpool-3", Name: "web", StateDir: previewWiringState(t, tt.hqStable, tt.webStbl)})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			if !maps.Equal(c.Env, tt.wantSet) {
				t.Errorf("the supervisor's overlay = %v, want %v", c.Env, tt.wantSet)
			}
			if result.SiblingEnv == nil || !maps.Equal(result.SiblingEnv.Set, tt.wantSet) || !slices.Equal(result.SiblingEnv.LeftToContainerEnv, tt.wantLeft) {
				t.Errorf("siblingEnv = %+v, want set %v and %v left to the container's env", result.SiblingEnv, tt.wantSet, tt.wantLeft)
			}
			for _, want := range tt.wantText {
				if !strings.Contains(result.Message, want) {
					t.Errorf("message %q lacks %q", result.Message, want)
				}
			}
			if env := firstSent(c.Sent(), "zcp-devsup ctl 'env'"); env < 0 || firstSent(c.Sent(), "zcp-devsup ctl 'start' 'web'") < env {
				t.Errorf("ctl env must come before the start: %v", c.Sent())
			}
		})
	}
}

// previewRewireDeclaration has each part reach the other's lease by container
// hostname and by preview URL.
func previewRewireDeclaration(server, port string) string {
	return `{"version":1,"previewPortBase":8000,"siblingEnv":{"HQ_HOST":"hqpool","WEB_HOST":"webpool"},"siblingPreviewEnv":{"HQ_URL":"hqpool","WEB_URL":"webpool"},` +
		`"servers":[{"name":"` + server + `","port":` + port + `,"command":"run ` + server + `"}]}`
}

// newPreviewRewireFixture is the rewire fixture over the preview pools, each
// container on previewRewireDeclaration: hqpool's container has stable number
// 1, webpool's 2.
func newPreviewRewireFixture(t *testing.T) *rewireFixture {
	t.Helper()
	rf := newRewireFixture(t)
	rf.deps.Client = previewMock()
	rf.hq = devservertest.NewSupervised(previewRewireDeclaration("api", "8080"))
	rf.web = devservertest.NewSupervised(previewRewireDeclaration("web", "3000"))
	rf.ssh.containers = map[string]*devservertest.Container{"hqpool-3": rf.hq, "webpool-7": rf.web}
	rf.ssh.slots = map[string]int{"hqpool-3": 1, "webpool-7": 2}
	return rf
}

// TestLeaseAcquire_SiblingPreviewEnv_FollowsTheSiblingsContainer: the preview
// URL is resolved where the container hostname is — each lease's supervisor
// step, every rewire — from the stable number the checkout read in the
// container. When hqpool-3 leaves the branch, web's HQ_URL falls back to the
// container's own value and web restarts; when hq joins again on a container
// with another stable number, web gets the new URL and restarts again. The
// container hostname form stays as it was.
func TestLeaseAcquire_SiblingPreviewEnv_FollowsTheSiblingsContainer(t *testing.T) {
	t.Parallel()
	rf := newPreviewRewireFixture(t)
	hqURL, webURL := "https://hqpool-abc1-8001.prg1.zerops.app", "https://webpool-abc1-8002.prg1.zerops.app"

	rf.acquire(t, groupHQPool, groupWebPool)
	both := map[string]string{"HQ_HOST": rewireHQHost, "WEB_HOST": rewireWebHost, "HQ_URL": hqURL, "WEB_URL": webURL}
	for alias, c := range map[string]*devservertest.Container{"hqpool-3": rf.hq, "webpool-7": rf.web} {
		if got := c.Overlay(); !maps.Equal(got, both) {
			t.Errorf("%s's overlay after the acquire = %v, want %v", alias, got, both)
		}
	}

	before, _ := rf.web.Restarts("web")
	change, err := LeaseReleaseChange(context.Background(), rf.deps, "hqpool-3", true)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	without := map[string]string{"WEB_HOST": rewireWebHost, "WEB_URL": webURL}
	if got := rf.web.Overlay(); !maps.Equal(got, without) {
		t.Errorf("webpool-7's overlay after hqpool-3 left = %v, want %v", got, without)
	}
	r := rewireOf(t, change.Rewired, "webpool-7")
	if r.SiblingEnv == nil || !slices.Equal(r.SiblingEnv.LeftToContainerEnv, []string{"HQ_HOST", "HQ_URL"}) || !slices.Equal(r.Restarted, []string{"web"}) {
		t.Errorf("rewire = %+v, want HQ_HOST and HQ_URL left to the container's env and web restarted", r)
	}

	// hq joins again on the pool's next container, #4, in slot 2.
	rf.ssh.mu.Lock()
	rf.ssh.containers["hqpool-4"] = devservertest.NewSupervised(previewRewireDeclaration("api", "8080"))
	rf.ssh.slots["hqpool-4"] = 2
	rf.ssh.mu.Unlock()
	change = rf.acquire(t, groupHQPool)
	moved := map[string]string{"HQ_HOST": nodeHostnameOf(groupHQPool, 4), "WEB_HOST": rewireWebHost, "HQ_URL": "https://hqpool-abc1-8002.prg1.zerops.app", "WEB_URL": webURL}
	if got := rf.web.Overlay(); !maps.Equal(got, moved) {
		t.Errorf("webpool-7's overlay after hq joined again = %v, want %v", got, moved)
	}
	if r := rewireOf(t, change.Rewired, "webpool-7"); r.SiblingEnv == nil || !maps.Equal(r.SiblingEnv.Set, moved) || !slices.Equal(r.Restarted, []string{"web"}) {
		t.Errorf("rewire = %+v, want %v and web restarted", r, moved)
	}
	if n, _ := rf.web.Restarts("web"); n != before+2 {
		t.Errorf("web restarts = %d, want %d: once for the leave, once for the join", n, before+2)
	}
}

// TestLeaseRelease_SiblingDeclarationUnreadable_KeepsThePreviewURLsOfTheGroup:
// a sibling whose declaration cannot be read keeps the preview URLs of pools
// the group still holds a lease on, as it keeps their container hostnames,
// and drops the ones of the lease that leaves.
func TestLeaseRelease_SiblingDeclarationUnreadable_KeepsThePreviewURLsOfTheGroup(t *testing.T) {
	t.Parallel()
	rf := newPreviewRewireFixture(t)
	rf.acquire(t, groupHQPool, groupWebPool)
	rf.web.Declaration = `{"version":1,"servers":[{"name":"web","port":3000}]}` // an edit broke it

	if _, err := LeaseReleaseChange(context.Background(), rf.deps, "hqpool-3", true); err != nil {
		t.Fatalf("release: %v", err)
	}
	want := map[string]string{"WEB_HOST": rewireWebHost, "WEB_URL": "https://webpool-abc1-8002.prg1.zerops.app"}
	if got := rf.web.Overlay(); !maps.Equal(got, want) {
		t.Errorf("webpool-7's overlay = %v, want %v: only hqpool-3's values dropped", got, want)
	}
}
