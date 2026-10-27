// Tests for: tools/lease.go, tools/dev_server.go + ops — a lease whose
// container stops answering is said to be not answering, with the reason when
// it is recognizable: a container at its process limit cannot start the
// command's process (fork answers EAGAIN, "resource temporarily unavailable")
// — docs/spec-workflows.md §8 DP-21, DP-19, DP-24, DP-4.
package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
)

// processLimitAnswer is what ssh into a container at its process limit
// answers (live, 2026-10-25, `ssh webpool-5 echo` from the zcp container):
// the container's ssh server cannot start the shell, and the call exits 127.
const processLimitAnswer = "fork/exec /bin/bash: resource temporarily unavailable"

func processLimitErr(t *testing.T, alias string) error {
	t.Helper()
	exit127 := exec.CommandContext(context.Background(), "sh", "-c", "exit 127").Run()
	return &platform.SSHExecError{Hostname: alias, Output: processLimitAnswer, Err: exit127}
}

// wedgingSSH answers every command until wedge is set, and then only the
// process limit's refusal.
func wedgingSSH(t *testing.T, wedge *atomic.Bool) *leaseStubSSH {
	t.Helper()
	return &leaseStubSSH{fn: func(string) ([]byte, error) {
		if wedge.Load() {
			return []byte(processLimitAnswer), processLimitErr(t, "devpool-3")
		}
		return nil, nil
	}}
}

// TestLeaseTool_ProcessLimit_SaysTheLeaseIsNotAnswering: once a lease's
// container reached its process limit, status marks it notAnswering with the
// reason and nextActions says to tell the person rather than wait on it; a
// sync from it and a release of it say it is not answering, and why (live,
// 2026-10-25: a helper looped `until ssh webpool-5 …` for 20 minutes and its
// orchestrator waited on it).
func TestLeaseTool_ProcessLimit_SaysTheLeaseIsNotAnswering(t *testing.T) {
	t.Parallel()
	var wedge atomic.Bool
	srv, _ := leaseToolServerSSH(t, &fakeOrchestrator{}, wedgingSSH(t, &wedge))
	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "build-stamp"}); res.IsError {
		t.Fatalf("acquire failed: %s", getTextContent(t, res))
	}
	wedge.Store(true)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "status"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status failed: %s", text)
	}
	var got struct {
		Leases []struct {
			Alias        string `json:"alias"`
			NotAnswering string `json:"notAnswering"`
		} `json:"leases"`
		NextActions string `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(got.Leases) != 1 || !strings.Contains(got.Leases[0].NotAnswering, "process limit") {
		t.Errorf("leases = %+v, want devpool-3 notAnswering for its process limit", got.Leases)
	}
	for _, want := range []string{"devpool-3", "not answering", "tell the person"} {
		if !strings.Contains(got.NextActions, want) {
			t.Errorf("nextActions %q does not say %q", got.NextActions, want)
		}
	}

	for _, call := range []map[string]any{
		{"action": "sync", "lease": "devpool-3"},
		{"action": "release", "lease": "devpool-3"},
	} {
		res := callTool(t, srv, "zerops_lease", call)
		text := getTextContent(t, res)
		if !res.IsError {
			t.Errorf("%v on a lease that does not answer succeeded: %s", call, text)
			continue
		}
		for _, want := range []string{"not answering", "process limit"} {
			if !strings.Contains(text, want) {
				t.Errorf("%v: refusal %q does not say %q", call, text, want)
			}
		}
	}
}

// TestDevServerTool_Lease_ProcessLimit_SaysNotAnswering: a dev-server call on
// a lease whose container cannot start a process says the lease is not
// answering and why, not a bare "exit status 127" (live, 2026-10-25: "read dev
// servers on webpool-5: ssh webpool-5: exit status 127").
func TestDevServerTool_Lease_ProcessLimit_SaysNotAnswering(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeDevpoolLeaseFile(t, dir)
	writePoolMeta(t, dir)
	var wedge atomic.Bool
	wedge.Store(true)
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, DevServerDeps{Client: leasePoolMock(true), HTTPClient: okHTTP, ProjectID: "proj-1", SSH: wedgingSSH(t, &wedge), StateDir: dir, vouch: vouchHeld})

	res := callTool(t, srv, "zerops_dev_server", map[string]any{"action": "status", "hostname": "devpool-3"})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("a dev-server status on a lease that does not answer succeeded: %s", text)
	}
	for _, want := range []string{"devpool-3", "not answering", "process limit"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %q does not say %q", text, want)
		}
	}
}

// TestLeaseTool_NotAnswering_RidesEveryEnvelopeUntilACallReachesIt: a lease a
// call found not answering — here a sync, at the container's process limit —
// is notAnswering, with why, in the envelope's leases[] of every later call,
// one that runs nothing in the lease too (a rollout): the envelope carries
// what zcp recorded, never an ssh read of its own. The first call that reaches
// the container again (status, once the processes filling it ended) clears it
// (docs/spec-mate.md §2.8a.3).
func TestLeaseTool_NotAnswering_RidesEveryEnvelopeUntilACallReachesIt(t *testing.T) {
	t.Parallel()
	var wedge atomic.Bool
	ssh := wedgingSSH(t, &wedge)
	srv, _ := leaseToolServerSSH(t, &fakeOrchestrator{}, ssh)
	envelopeNotAnswering := func(args map[string]any) string {
		t.Helper()
		text := getTextContent(t, callTool(t, srv, "zerops_lease", args))
		var got struct {
			Envelope struct {
				Leases []struct {
					Alias        string `json:"alias"`
					NotAnswering string `json:"notAnswering"`
				} `json:"leases"`
			} `json:"envelope"`
		}
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatalf("%v: result is not JSON: %v\n%s", args, err, text)
		}
		if len(got.Envelope.Leases) != 1 || got.Envelope.Leases[0].Alias != "devpool-3" {
			t.Fatalf("%v: envelope leases = %+v, want devpool-3\n%s", args, got.Envelope.Leases, text)
		}
		return got.Envelope.Leases[0].NotAnswering
	}
	rollout := map[string]any{"action": "rollout", "pool": "devpool"}

	if why := envelopeNotAnswering(map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "build-stamp"}); why != "" {
		t.Fatalf("acquire's envelope says devpool-3 is not answering: %q", why)
	}
	wedge.Store(true)
	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "sync", "lease": "devpool-3"}); !res.IsError {
		t.Fatalf("a sync from a lease that does not answer succeeded: %s", getTextContent(t, res))
	}

	ssh.mu.Lock()
	sent := len(ssh.calls)
	ssh.mu.Unlock()
	if why := envelopeNotAnswering(rollout); !strings.Contains(why, "process limit") {
		t.Errorf("after the sync found it at its process limit, a rollout's envelope says devpool-3 notAnswering %q, want its process limit", why)
	}
	ssh.mu.Lock()
	if extra := ssh.calls[sent:]; len(extra) > 0 {
		t.Errorf("the rollout's envelope ran ssh in the lease: %q", extra)
	}
	ssh.mu.Unlock()

	wedge.Store(false)
	if why := envelopeNotAnswering(map[string]any{"action": "status"}); why != "" {
		t.Errorf("status reached devpool-3, but its envelope still says notAnswering %q", why)
	}
	if why := envelopeNotAnswering(rollout); why != "" {
		t.Errorf("status reached devpool-3, but a later rollout's envelope still says notAnswering %q", why)
	}
}
