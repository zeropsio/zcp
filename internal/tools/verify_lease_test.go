// Tests for: tools/verify.go — zerops_verify of a lease alias
// (docs/spec-workflows.md §8 DP-1, PS-3).
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (h *hostSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return h.ExecSSH(ctx, host, command)
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

// previewAnswer answers every request with status and records the URLs.
type previewAnswer struct {
	mu     sync.Mutex
	status int
	urls   []string
}

func (p *previewAnswer) Do(req *http.Request) (*http.Response, error) {
	p.mu.Lock()
	p.urls = append(p.urls, req.URL.String())
	p.mu.Unlock()
	return &http.Response{StatusCode: p.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("<html>bad gateway</html>"))}, nil
}

// TestVerify_LeaseAlias_ProbesThePreviewFromOutside: a lease whose preview
// server has a preview URL — its pool's subdomain on its preview port — is
// verified from outside too: a preview answering 502 degrades the lease,
// never "healthy" while the person's link is dark (Noola, 2026-10-27: six
// leases healthy, six previews 502).
func TestVerify_LeaseAlias_ProbesThePreviewFromOutside(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     int
		wantCheck  string
		wantStatus string
	}{
		{name: "the preview answers", status: http.StatusOK, wantCheck: ops.CheckPass, wantStatus: ops.StatusHealthy},
		{name: "the preview answers 502", status: http.StatusBadGateway, wantCheck: ops.CheckFail, wantStatus: ops.StatusDegraded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
			writeLeaseFile(t, stateDir)
			web := devservertest.NewSupervised(`{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"run web"}]}`)
			web.StableNumber = 3
			pool := platform.ServiceStack{ID: "svc-pool", Name: "webpool", ProjectID: "proj-1", Status: serviceStatusRunning, SubdomainAccess: true,
				Ports:                []platform.Port{{Port: 9001, Protocol: "tcp"}, {Port: 9002, Protocol: "tcp"}, {Port: 9003, Protocol: "tcp"}},
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}}
			mock := platform.NewMock().WithServices([]platform.ServiceStack{pool}).WithService(&pool).
				WithProject(&platform.Project{ID: "proj-1", Name: "proj", Status: statusActive, SubdomainHost: "abc1.prg1.zerops.app"})
			answer := &previewAnswer{status: tt.status}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterVerify(srv, mock, platform.NewMockLogFetcher(), "proj-1", stateDir, runtime.Info{InContainer: true}, &hostSSH{inner: web}, verifyVouchHeld(), verifyBoxHTTP(answer))
			result := callTool(t, srv, "zerops_verify", map[string]any{"serviceHostname": "webpool-3"})
			text := getTextContent(t, result)
			if result.IsError {
				t.Fatalf("verify: %s", text)
			}
			var vr ops.VerifyResult
			if err := json.Unmarshal([]byte(text), &vr); err != nil {
				t.Fatalf("parse result: %v", err)
			}
			const url = "https://webpool-abc1-9003.prg1.zerops.app/"
			var preview *ops.CheckResult
			for i, c := range vr.Checks {
				if c.URL == url {
					preview = &vr.Checks[i]
				}
			}
			if preview == nil || preview.Status != tt.wantCheck || preview.Server != "web" {
				t.Fatalf("checks = %+v, want web's preview %s probed from outside: %s", vr.Checks, url, tt.wantCheck)
			}
			if vr.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", vr.Status, tt.wantStatus)
			}
		})
	}
}
