// Tests for: tools/lease.go — zerops_lease hands every other lease of a
// branch the sibling overlay recomputed when a lease joins or leaves the
// branch, and names each in the result's rewired; status marks wiring that
// names a container no lease of the branch holds (docs/spec-workflows.md §8
// "Dev pools (DP)").
package tools

import (
	"context"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
)

// rewireToolDeclaration has each part reach the other's lease of the branch
// and runs one server.
func rewireToolDeclaration(server, port string) string {
	return `{"version":1,"siblingEnv":{"HQ_HOST":"hqpool","WEB_HOST":"webpool"},"servers":[{"name":"` + server + `","port":` + port + `,"command":"run ` + server + `"}]}`
}

// rewireToolSSH is the group server's leased containers, each running
// zcp-devsup: a checkout is cut new, the release guard reads a clean checkout,
// status's read of a checkout answers no change with the supervisor's own
// answer as its supervisor part, and every supervisor command goes to the
// lease's container.
type rewireToolSSH struct {
	mu         sync.Mutex
	containers map[string]*devservertest.Container
}

func (s *rewireToolSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	c := s.containers[host]
	s.mu.Unlock()
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

func (s *rewireToolSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func newRewireToolSSH() (*rewireToolSSH, *devservertest.Container, *devservertest.Container) {
	hq, web := devservertest.NewSupervised(rewireToolDeclaration("api", "8080")), devservertest.NewSupervised(rewireToolDeclaration("web", "3000"))
	return &rewireToolSSH{containers: map[string]*devservertest.Container{"hqpool-3": hq, "webpool-7": web}}, hq, web
}

// TestLeaseTool_Release_RewiresTheBranchsOtherLeases: releasing hqpool-3 first
// hands webpool-7 the overlay without it, and the result names webpool-7 in
// rewired with the overlay, the servers restarted and the milliseconds — never
// a lease string.
func TestLeaseTool_Release_RewiresTheBranchsOtherLeases(t *testing.T) {
	t.Parallel()
	ssh, _, web := newRewireToolSSH()
	srv, orchs, _ := groupLeaseServerSSH(t, ssh)
	acquireFeatX(t, srv)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "lease": "hqpool-3"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("release: %s", text)
	}
	for _, o := range orchs {
		if strings.Contains(text, o.lease()) {
			t.Fatalf("result carries a lease string: %s", text)
		}
	}
	got := leaseResultOf(t, text)
	if len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateReleased {
		t.Fatalf("leases = %+v, want hqpool-3 released", got.Leases)
	}
	if len(got.Rewired) != 1 || got.Rewired[0].Alias != "webpool-7" {
		t.Fatalf("rewired = %+v, want webpool-7", got.Rewired)
	}
	r := got.Rewired[0]
	want := map[string]string{"WEB_HOST": "node-id-7.runtime.webpool.zerops"}
	if r.SiblingEnv == nil || !maps.Equal(r.SiblingEnv.Set, want) || !slices.Equal(r.Restarted, []string{"web"}) {
		t.Errorf("rewire = %+v, want %v and web restarted", r, want)
	}
	if !strings.Contains(text, `"ms":`) {
		t.Errorf("the rewire carries no ms: %s", text)
	}
	if got := web.Overlay(); !maps.Equal(got, want) {
		t.Errorf("webpool-7's overlay = %v, want %v", got, want)
	}
}

// TestLeaseTool_Acquire_JoiningGroup_NamesTheRewire: an acquire that adds a
// lease to the branch names, in rewired, each lease it handed the new overlay.
func TestLeaseTool_Acquire_JoiningGroup_NamesTheRewire(t *testing.T) {
	t.Parallel()
	ssh, hq, _ := newRewireToolSSH()
	srv, _, _ := groupLeaseServerSSH(t, ssh)
	for _, pool := range []string{"hqpool", "webpool"} {
		res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": pool, "repo": leaseTestRepo, "branch": "feat-x"})
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("acquire %s: %s", pool, text)
		}
		got := leaseResultOf(t, text)
		if pool == "hqpool" {
			if len(got.Rewired) != 0 {
				t.Errorf("the branch's first lease rewired %+v", got.Rewired)
			}
			continue
		}
		if len(got.Rewired) != 1 || got.Rewired[0].Alias != "hqpool-3" || !slices.Equal(got.Rewired[0].Restarted, []string{"api"}) {
			t.Errorf("rewired = %+v, want hqpool-3 with api restarted", got.Rewired)
		}
	}
	want := map[string]string{"HQ_HOST": "node-id-3.runtime.hqpool.zerops", "WEB_HOST": "node-id-7.runtime.webpool.zerops"}
	if got := hq.Overlay(); !maps.Equal(got, want) {
		t.Errorf("hqpool-3's overlay = %v, want %v", got, want)
	}
}

// TestLeaseTool_Release_RewireFailed_NextActionsNameTheCall: a lease the
// rewire could not reach never holds the release up; nextActions names it
// and the call that sends the overlay again.
func TestLeaseTool_Release_RewireFailed_NextActionsNameTheCall(t *testing.T) {
	t.Parallel()
	ssh, _, web := newRewireToolSSH()
	srv, _, _ := groupLeaseServerSSH(t, ssh)
	acquireFeatX(t, srv)
	web.EnvRefusal = "env: the state directory is read-only"

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "lease": "hqpool-3", "force": true})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("a sibling's failure blocked the release: %s", text)
	}
	got := leaseResultOf(t, text)
	if len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateReleased {
		t.Fatalf("leases = %+v, want hqpool-3 released", got.Leases)
	}
	for _, want := range []string{"webpool-7", `zerops_dev_server action=restart hostname="webpool-7" name="web"`} {
		if !strings.Contains(got.NextActions, want) {
			t.Errorf("nextActions %q lacks %q", got.NextActions, want)
		}
	}
}

// TestLeaseTool_Status_StaleWiring_Named: hqpool-3's lease ending outside zcp
// leaves webpool-7's servers wired to a container the pool hands to the next
// acquirer: status marks webpool-7 wiring: stale with the call that fixes it,
// and sends nothing.
func TestLeaseTool_Status_StaleWiring_Named(t *testing.T) {
	t.Parallel()
	ssh, _, web := newRewireToolSSH()
	srv, orchs, _ := groupLeaseServerSSH(t, ssh)
	acquireFeatX(t, srv)
	before := web.Sent()
	orchs["hqpool"].mu.Lock()
	orchs["hqpool"].reserved = false // released elsewhere
	orchs["hqpool"].mu.Unlock()

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "status"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status: %s", text)
	}
	got := leaseResultOf(t, text)
	var webView ops.LeaseView
	for _, v := range got.Leases {
		if v.Alias == "webpool-7" {
			webView = v
		}
	}
	if webView.Wiring != ops.LeaseWiringStale {
		t.Fatalf("webpool-7 = %+v, want wiring stale", webView)
	}
	for _, want := range []string{"HQ_HOST", "hqpool-3", `zerops_dev_server action=restart hostname="webpool-7" name="web"`} {
		if !strings.Contains(webView.Note, want) {
			t.Errorf("note %q lacks %q", webView.Note, want)
		}
	}
	if !strings.Contains(text, `"wiring":"stale"`) {
		t.Errorf("result lacks wiring: stale: %s", text)
	}
	for _, cmd := range web.Sent()[len(before):] {
		if strings.Contains(cmd, "ctl 'env'") || strings.Contains(cmd, "ctl 'reload'") || strings.Contains(cmd, "ctl 'restart'") {
			t.Errorf("status sent %q", cmd)
		}
	}
}
