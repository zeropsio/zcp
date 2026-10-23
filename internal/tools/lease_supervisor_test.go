// Tests for: tools/lease.go, tools/dev_server.go — a lease always has its
// supervisor (docs/spec-workflows.md §8 DP-13): zerops_lease acquire starts
// zcp-devsup in a reused container and says when the pool's image has none;
// zerops_dev_server on a lease whose supervisor is gone starts it again.
package tools

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// TestLeaseTool_Release_Unresettable_DeletesWithTheLease: a container the
// platform created for minContainers answers a reset with no-primary-snapshot;
// zerops_lease deletes it with the lease in the header instead of releasing it
// unreset, and the result says why (docs/spec-workflows.md §8 DP-15).
func TestLeaseTool_Release_Unresettable_DeletesWithTheLease(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var unresetRelease bool
	var deleteLease string
	inner := &fakeOrchestrator{}
	srv, stateDir := leaseToolServerSSH(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/release") && r.URL.Query().Get("reset") == "true":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"container has no primary snapshot, so it cannot be reset","meta":[{"metadata":{"reason":["no-primary-snapshot"],"retryable":["false"]}}]}}`))
			return
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/release"):
			unresetRelease = true
		case r.Method == http.MethodDelete && r.URL.Path == "/container/c-abc":
			deleteLease = r.Header.Get("X-Swarm-Lease")
			if r.URL.RawQuery != "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}
		inner.ServeHTTP(w, r)
	}), &leaseStubSSH{})

	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo}); res.IsError {
		t.Fatalf("acquire failed: %s", getTextContent(t, res))
	}
	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "lease": "devpool-3", "force": true})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("release failed: %s", text)
	}
	var got leaseToolResult
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	if len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateReleased {
		t.Fatalf("leases = %+v, want devpool-3 released", got.Leases)
	}
	for _, want := range []string{"could not be reset", "deleted", "minContainers"} {
		if !strings.Contains(got.Leases[0].Note, want) {
			t.Errorf("note %q lacks %q", got.Leases[0].Note, want)
		}
	}
	for _, secret := range []string{leaseTestLease, leaseTestToken} {
		if strings.Contains(text, secret) {
			t.Errorf("result carries a secret: %s", text)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if unresetRelease {
		t.Error("the container was released without a reset: the next holder would get its files")
	}
	if deleteLease != leaseTestLease {
		t.Errorf("DELETE carried lease header %q, want the lease", deleteLease)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "leases", "devpool-3.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the record survives the deleted container: %v", err)
	}
}
