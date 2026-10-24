// Tests for: tools/verify.go — zerops_verify of a lease alias
// (docs/spec-workflows.md §8 DP-1, PS-3).
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
)

// hostSSH records the host of every call to a wrapped container and can make
// the plain reachability probe fail.
type hostSSH struct {
	inner      ops.SSHDeployer
	probeErr   error
	mu         sync.Mutex
	hosts      []string
	commands   []string
	ctlReached bool
}

func (h *hostSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	h.mu.Lock()
	h.hosts = append(h.hosts, host)
	h.commands = append(h.commands, command)
	if strings.Contains(command, "zcp-devsup ctl status --json") {
		h.ctlReached = true
	}
	h.mu.Unlock()
	if command == "true" && h.probeErr != nil {
		return nil, h.probeErr
	}
	return h.inner.ExecSSH(ctx, host, command)
}

func (h *hostSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return h.ExecSSH(ctx, host, command)
}

func verifyLeaseCall(t *testing.T, stateDir string, ssh ops.SSHDeployer, host string) (*mcp.CallToolResult, string) {
	t.Helper()
	// The project holds no service named like the lease alias: resolving it
	// as a service could only fail.
	mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-pool", Name: "webpool", Status: serviceStatusRunning,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}}})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterVerify(srv, mock, platform.NewMockLogFetcher(), "proj-1", stateDir, runtime.Info{InContainer: true}, ssh, verifyVouchHeld())
	result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": host})
	return result, getTextContent(t, result)
}

func checkByName(vr ops.VerifyResult, name string) []ops.CheckResult {
	var out []ops.CheckResult
	for _, c := range vr.Checks {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

func TestVerify_LeaseAlias_ReportsLease(t *testing.T) {
	t.Parallel()

	const alias = "webpool-3"
	tests := []struct {
		name          string
		container     func() *devservertest.Container
		probeErr      error
		wantStatus    string
		wantReachable string // status of the ssh_reachable check
		wantServers   map[string]string
		wantDevCheck  string // status of the one "dev_servers" note, when no server is listed
		wantCtl       bool
	}{
		{
			name:          "supervisor answers",
			container:     func() *devservertest.Container { return devservertest.NewSupervised(webAndDocs) },
			wantStatus:    ops.StatusHealthy,
			wantReachable: ops.CheckPass,
			wantServers:   map[string]string{"web": ops.CheckPass, "docs": ops.CheckPass},
			wantCtl:       true,
		},
		{
			name:          "no supervisor is no failure",
			container:     func() *devservertest.Container { return &devservertest.Container{} },
			wantStatus:    ops.StatusHealthy,
			wantReachable: ops.CheckPass,
			wantDevCheck:  ops.CheckInfo,
			wantCtl:       true,
		},
		{
			name:          "container does not answer over ssh",
			container:     func() *devservertest.Container { return devservertest.NewSupervised(webAndDocs) },
			probeErr:      &platform.SSHExecError{Hostname: alias, Err: errors.New("exit status 255"), Output: "ssh: connect to host webpool-3 port 22: Connection refused"},
			wantStatus:    ops.StatusUnhealthy,
			wantReachable: ops.CheckFail,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
			writeLeaseFile(t, stateDir)
			ssh := &hostSSH{inner: tt.container(), probeErr: tt.probeErr}

			result, text := verifyLeaseCall(t, stateDir, ssh, alias)
			if result.IsError {
				t.Fatalf("verify of a recorded lease must not error: %s", text)
			}
			var vr ops.VerifyResult
			if err := json.Unmarshal([]byte(text), &vr); err != nil {
				t.Fatalf("parse result: %v", err)
			}
			if vr.Hostname != alias || vr.Type != ops.VerifyTypeLease {
				t.Errorf("hostname %q type %q, want %s as a %s", vr.Hostname, vr.Type, alias, ops.VerifyTypeLease)
			}
			if vr.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q; checks %+v", vr.Status, tt.wantStatus, vr.Checks)
			}
			identity := checkByName(vr, "lease")
			if len(identity) != 1 || !strings.Contains(identity[0].Detail, "pool webpool") || !strings.Contains(identity[0].Detail, "node-id-3.runtime.webpool.zerops") {
				t.Errorf("lease check = %+v, want the pool and the container", identity)
			}
			reach := checkByName(vr, "ssh_reachable")
			if len(reach) != 1 || reach[0].Status != tt.wantReachable {
				t.Fatalf("ssh_reachable = %+v, want %s", reach, tt.wantReachable)
			}
			if tt.probeErr != nil && !strings.Contains(reach[0].Detail, "Connection refused") {
				t.Errorf("unreachable detail %q lacks the ssh stderr", reach[0].Detail)
			}
			for name, want := range tt.wantServers {
				found := false
				for _, c := range checkByName(vr, "dev_server") {
					if c.Server == name {
						found = true
						if c.Status != want {
							t.Errorf("dev server %s = %s, want %s", name, c.Status, want)
						}
					}
				}
				if !found {
					t.Errorf("no dev_server check for %s; checks %+v", name, vr.Checks)
				}
			}
			if tt.wantDevCheck != "" {
				notes := checkByName(vr, "dev_servers")
				if len(notes) != 1 || notes[0].Status != tt.wantDevCheck {
					t.Errorf("dev_servers = %+v, want one %s note", notes, tt.wantDevCheck)
				}
			}
			for _, h := range ssh.hosts {
				if h != alias {
					t.Errorf("ssh reached %q, want only the alias %q", h, alias)
				}
			}
			if ssh.ctlReached != tt.wantCtl {
				t.Errorf("supervisor status read = %v, want %v", ssh.ctlReached, tt.wantCtl)
			}
		})
	}
}

func TestVerify_LeaseAlias_NoSSH_Refused(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	writeLeaseFile(t, stateDir)
	result, text := verifyLeaseCall(t, stateDir, nil, "webpool-3")
	if !result.IsError || !strings.Contains(text, "lease") {
		t.Fatalf("verify of a lease without SSH access = error %v, %s; want a refusal naming the lease", result.IsError, text)
	}
}

// TestVerify_UnknownHost_Unchanged: a hostname that is neither a service nor a
// recorded lease keeps today's SERVICE_NOT_FOUND, with or without a lease
// record in the state directory.
func TestVerify_UnknownHost_Unchanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		host string
	}{
		{name: "unknown service", host: "ghost"},
		{name: "alias shape without a record", host: "webpool-9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
			writeLeaseFile(t, stateDir)
			ssh := &hostSSH{inner: devservertest.NewSupervised(webAndDocs)}
			result, text := verifyLeaseCall(t, stateDir, ssh, tt.host)
			if !result.IsError || !strings.Contains(text, string(platform.ErrServiceNotFound)) {
				t.Fatalf("verify %q = error %v, %s; want SERVICE_NOT_FOUND", tt.host, result.IsError, text)
			}
			if len(ssh.hosts) != 0 {
				t.Errorf("ssh calls = %v, want none", ssh.hosts)
			}
		})
	}
}
