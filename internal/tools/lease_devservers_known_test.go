// Tests for: every envelope's leases carry their last-known dev servers and
// when they were read (devServersReadAt), recorded by every zcp path that
// reads a lease's supervisor — an acquire, zerops_dev_server — so a client
// that takes a later envelope keeps each workspace's preview
// (docs/spec-mate.md §2.8a.3).
package tools

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// leaseServersIn is alias's lease in env: its dev servers by name → status,
// and devServersReadAt.
func leaseServersIn(t *testing.T, env *workflow.StateEnvelope, alias string) (map[string]string, string) {
	t.Helper()
	if env == nil {
		t.Fatal("no envelope")
	}
	for _, l := range env.LeaseList() {
		if l.Alias != alias {
			continue
		}
		servers := map[string]string{}
		for _, d := range l.DevServers {
			servers[d.Name] = d.Status
		}
		return servers, l.DevServersReadAt
	}
	t.Fatalf("the envelope carries no lease %s: %+v", alias, env.LeaseList())
	return nil, ""
}

// TestLeaseTool_Acquire_EveryLaterEnvelopeKeepsTheLeasesDevServers: an
// acquire starts each lease's supervisor and reads it once (DP-30); the
// acquire's own envelope carries each lease's dev servers with
// devServersReadAt, and so does an envelope of any later call that read no
// supervisor (a verify's), from zcp's record of that read.
func TestLeaseTool_Acquire_EveryLaterEnvelopeKeepsTheLeasesDevServers(t *testing.T) {
	t.Parallel()
	ssh, _, web := newRewireToolSSH()
	web.Running, web.Servers = false, nil
	web.Settle = map[string]int{"web": 100}
	srv, _, stateDir := groupLeaseServerSSH(t, ssh)
	before := time.Now().UTC().Truncate(time.Second)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"hqpool", "webpool"}, "repo": leaseTestRepo, "branch": "feat-x"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire: %s", text)
	}
	acquired, ok := workflow.ExtractEnvelope(text)
	if !ok {
		t.Fatalf("no envelope in\n%s", text)
	}
	later := freshEnvelope(context.Background(), stateDir, nil, "", runtime.Info{InContainer: true, ServiceName: "zcp"})
	for name, env := range map[string]*workflow.StateEnvelope{"the acquire's": &acquired, "a later call's": later} {
		for alias, want := range map[string]string{"hqpool-3": "api", "webpool-7": "web"} {
			servers, readAt := leaseServersIn(t, env, alias)
			if _, ok := servers[want]; !ok || len(servers) != 1 {
				t.Errorf("%s envelope: %s devServers = %v, want %s", name, alias, servers, want)
			}
			at, err := time.Parse(time.RFC3339, readAt)
			if err != nil || at.Before(before) || at.Location() != time.UTC {
				t.Errorf("%s envelope: %s devServersReadAt = %q, want the acquire's read, RFC 3339 UTC", name, alias, readAt)
			}
		}
	}
}

// TestDevServer_LeaseRead_EveryLaterEnvelopeKeepsIt: a zerops_dev_server call
// on a lease's alias reads its supervisor; an envelope of a later call that
// read none still carries what it read, with when.
func TestDevServer_LeaseRead_EveryLaterEnvelopeKeepsIt(t *testing.T) {
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()
	dir := t.TempDir()
	writeDevpoolLeaseFile(t, dir)
	writePoolMeta(t, dir)
	mock := leasePoolMock(true)
	c := devservertest.NewSupervised(`{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`)
	c.Servers["web"].PreviewPort = 9003
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, DevServerDeps{Client: mock, HTTPClient: okHTTP, ProjectID: "proj-1", SSH: c, StateDir: dir, vouch: vouchHeld,
		RtInfo: runtime.Info{InContainer: true, ServiceName: "zcp"}, CarriesParts: true})

	if res := callTool(t, srv, "zerops_dev_server", map[string]any{"action": "start", "hostname": "devpool-3"}); res.IsError {
		t.Fatalf("start: %s", getTextContent(t, res))
	}

	later := freshEnvelope(context.Background(), dir, mock, "proj-1", runtime.Info{InContainer: true, ServiceName: "zcp"})
	servers, readAt := leaseServersIn(t, later, "devpool-3")
	if servers["web"] != "running" || readAt == "" {
		t.Errorf("a later envelope's lease = %v read at %q, want web running and when it was read", servers, readAt)
	}
}
