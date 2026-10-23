// Tests for: tools/lease.go, tools/dev_server.go — a lease always has its
// supervisor (docs/spec-workflows.md §8 DP-13): zerops_lease acquire starts
// zcp-devsup in a reused container and says when the pool's image has none;
// zerops_dev_server on a lease whose supervisor is gone starts it again.
package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
)

func TestLeaseTool_Acquire_EnsuresTheSupervisor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		answer   string
		wantNote []string // empty: no note
	}{
		{name: "a reused container gets one started", answer: "@@zcp-devsup check 2\n@@zcp-devsup started 0\n"},
		{name: "no zcp-devsup in the pool's image", answer: "@@zcp-devsup check 127\n", wantNote: []string{"zcp-devsup", "not a dev pool", "no dev servers"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ssh := &leaseStubSSH{fn: func(command string) ([]byte, error) {
				if strings.Contains(command, "zcp-devsup run") {
					return []byte(tt.answer), nil
				}
				return nil, nil
			}}
			srv, _ := leaseToolServerSSH(t, &fakeOrchestrator{}, ssh)

			res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feature"})
			text := getTextContent(t, res)
			if res.IsError {
				t.Fatalf("acquire failed: %s", text)
			}
			if !ssh.sawCommand("zcp-devsup run") {
				t.Errorf("acquire never ensured the supervisor: %v", ssh.calls)
			}
			var got leaseToolResult
			if err := json.Unmarshal([]byte(text), &got); err != nil {
				t.Fatalf("result is not JSON: %v\n%s", err, text)
			}
			if len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateHeld {
				t.Fatalf("leases = %+v, want one held", got.Leases)
			}
			note := got.Leases[0].Note
			if len(tt.wantNote) == 0 && note != "" {
				t.Errorf("note = %q, want none", note)
			}
			for _, want := range tt.wantNote {
				if !strings.Contains(note, want) {
					t.Errorf("note %q lacks %q", note, want)
				}
			}
			for _, secret := range []string{leaseTestLease, leaseTestToken} {
				if strings.Contains(text, secret) {
					t.Errorf("result carries a secret: %s", text)
				}
			}
		})
	}
}

func TestDevServerTool_LeaseWhoseSupervisorIsGone_StartsIt(t *testing.T) {
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	writeDevpoolLeaseFile(t, dir)
	// A reused container: zcp-devsup is in the image, nothing runs it.
	c := &devservertest.Container{Installed: true, SupervisorVersion: "v9.300.0", Formats: []int{1},
		Declaration: `{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, DevServerDeps{Client: leasePoolMock(true), HTTPClient: okHTTP, ProjectID: "proj-1", SSH: c, StateDir: dir})

	result := callTool(t, srv, "zerops_dev_server", map[string]any{"action": "status", "hostname": "devpool-3"})
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("status failed: %s", text)
	}
	var got struct {
		Running bool   `json:"running"`
		Reason  string `json:"reason"`
		Pool    string `json:"pool"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("parse result: %v\n%s", err, text)
	}
	if c.SupervisorStarts != 1 {
		t.Errorf("supervisors started = %d, want one", c.SupervisorStarts)
	}
	if !got.Running || got.Reason == "no_supervisor" || got.Pool != "devpool" {
		t.Errorf("result = %+v, want web running under the supervisor zcp started", got)
	}
}
