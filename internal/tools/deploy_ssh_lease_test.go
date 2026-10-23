// Tests for: tools/deploy_ssh.go + tools/guard.go — zerops_deploy whose source
// is a lease alias (docs/spec-workflows.md §8 DP-1, PS-3, §4.9).
package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// leaseSSH records every ssh call by host. It answers a repo probe as "no
// repo" (the realistic default of stubSSH) and everything else "ok".
type leaseSSH struct {
	mu    sync.Mutex
	calls []leaseSSHCall
}

type leaseSSHCall struct{ host, command string }

func (s *leaseSSH) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	s.mu.Lock()
	s.calls = append(s.calls, leaseSSHCall{host: host, command: command})
	s.mu.Unlock()
	if strings.Contains(command, "rev-parse --verify HEAD") && strings.Contains(command, "git status --porcelain") {
		return nil, errStubSSHNoRepo
	}
	return []byte("ok"), nil
}

func (s *leaseSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s *leaseSSH) hosts() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for _, c := range s.calls {
		out[c.host] = true
	}
	return out
}

func (s *leaseSSH) anyCommandContains(sub string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.calls {
		if strings.Contains(c.command, sub) {
			return true
		}
	}
	return false
}

// leaseDeployRig is a project with an adopted standard pair (appdev/appstage),
// a dev pool (webpool) and one recorded lease webpool-3, whose checkout holds
// a zerops.yaml under the project root's mount of the alias.
type leaseDeployRig struct {
	stateDir string
	alias    string
	mock     *platform.Mock
}

// writeLeaseFile records the lease webpool-3 the way zcp's own record
// reads (.zcp/state/leases/<alias>.json) — written as bytes, so the test
// shares no code with the reader.
func writeLeaseFile(t *testing.T, stateDir string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir leases: %v", err)
	}
	const pool, n = "webpool", "3"
	record := `{"poolHostname":"` + pool + `","poolServiceId":"svc-pool","containerId":"c-` + n + `","containerNumber":` + n + `,` +
		`"containerHostname":"node-id-` + n + `.runtime.` + pool + `.zerops","lease":"secret-lease-value","branch":"feat-x","acquiredAt":"2026-10-23T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, pool+"-"+n+".json"), []byte(record), 0o600); err != nil {
		t.Fatalf("write lease record: %v", err)
	}
}

func newLeaseDeployRig(t *testing.T) leaseDeployRig {
	t.Helper()
	projectRoot := t.TempDir()
	stateDir := filepath.Join(projectRoot, ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:                 "appdev",
		Mode:                     topology.PlanModeStandard,
		Stages:                   []topology.StageRef{{Hostname: "appstage"}},
		BootstrapSession:         "test-session",
		BootstrappedAt:           "2026-10-23T12:00:00Z",
		CloseDeployMode:          topology.CloseModeAuto,
		CloseDeployModeConfirmed: true,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	const alias = "webpool-3"
	mount := filepath.Join(projectRoot, alias)
	if err := os.MkdirAll(mount, 0o755); err != nil {
		t.Fatalf("mkdir mount: %v", err)
	}
	yaml := "zerops:\n  - setup: prod\n    build:\n      base: nodejs@22\n      deployFiles: [.]\n    run:\n      ports:\n        - port: 3000\n          httpSupport: true\n      start: node server.js\n"
	if err := os.WriteFile(filepath.Join(mount, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	writeLeaseFile(t, stateDir)
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", Status: serviceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{ID: "svc-stage", Name: "appstage", Status: serviceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{ID: "svc-pool", Name: "webpool", Status: serviceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-stage", ProjectID: "proj-1", ServiceStackID: "svc-stage", Status: statusActive, Sequence: 1},
			{ID: "av-pool", ProjectID: "proj-1", ServiceStackID: "svc-pool", Status: statusActive, Sequence: 1},
		})
	return leaseDeployRig{stateDir: stateDir, alias: alias, mock: mock}
}

func (r leaseDeployRig) deploy(t *testing.T, ssh ops.SSHDeployer, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, r.mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{InContainer: true, ServiceName: "zcp"}, r.stateDir, testDeployEngine(t), nil, DevsupSource{})
	result := callTool(t, srv, "zerops_deploy", args)
	return result, getTextContent(t, result)
}

func TestDeployTool_LeaseSource_NotRefusedByAdoptionGuard(t *testing.T) {
	t.Parallel()

	rig := newLeaseDeployRig(t)
	ssh := &leaseSSH{}
	result, text := rig.deploy(t, ssh, map[string]any{"sourceService": rig.alias, "targetService": "appstage"})
	if result.IsError {
		t.Fatalf("a deploy from a recorded lease must not be refused: %s", text)
	}
	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.SourceService != rig.alias || parsed.TargetService != "appstage" || parsed.Status != statusDeployed {
		t.Errorf("result = source %q target %q status %q, want %s → appstage deployed", parsed.SourceService, parsed.TargetService, parsed.Status, rig.alias)
	}
	if parsed.ReachesNewContainersOnly {
		t.Error("a deploy to the stage must not carry the pool-image note")
	}
	if !ssh.hosts()[rig.alias] {
		t.Errorf("no ssh call reached the lease alias; hosts = %v", ssh.hosts())
	}
	if ssh.hosts()["webpool"] {
		t.Error("an ssh call reached the pool by service hostname (a random container)")
	}
	// A lease's supervisor is not migrated by this deploy (the source is not
	// the target): nothing reads or rewrites a dev setup.
	if ssh.anyCommandContains("zcp-devsup") && ssh.anyCommandContains("run.start") {
		t.Error("a lease deploy touched a dev setup")
	}
}

// TestRequireAdoption_Lease: only a recorded lease alias is exempt, and the
// pool is a deploy target only beside a lease of its own.
func TestRequireAdoption_Lease(t *testing.T) {
	t.Parallel()

	rig := newLeaseDeployRig(t)
	rt := runtime.Info{InContainer: true}
	tests := []struct {
		name      string
		hostnames []string
		wantBlock bool
	}{
		{name: "lease source to stage", hostnames: []string{"appstage", "webpool-3"}},
		{name: "lease source to its own pool", hostnames: []string{"webpool", "webpool-3"}},
		{name: "pool alone", hostnames: []string{"webpool"}, wantBlock: true},
		{name: "alias shape without a record", hostnames: []string{"appstage", "webpool-9"}, wantBlock: true},
		{name: "unknown service", hostnames: []string{"appstage", "ghost"}, wantBlock: true},
		{name: "adopted pair only", hostnames: []string{"appstage", "appdev"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			blocked := requireAdoption(rig.stateDir, rt, nil, tt.hostnames...)
			if (blocked != nil) != tt.wantBlock {
				t.Fatalf("requireAdoption(%v) blocked = %v, want %v", tt.hostnames, blocked != nil, tt.wantBlock)
			}
		})
	}
}

func TestDeployTool_LeaseToOwnPool_SaysOnlyNewContainers(t *testing.T) {
	t.Parallel()

	rig := newLeaseDeployRig(t)
	ssh := &leaseSSH{}
	result, text := rig.deploy(t, ssh, map[string]any{"sourceService": rig.alias, "targetService": "webpool"})
	if result.IsError {
		t.Fatalf("a deploy from a lease to its own pool is allowed: %s", text)
	}
	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if !parsed.ReachesNewContainersOnly {
		t.Error("reachesNewContainersOnly = false")
	}
	for _, want := range []string{"containers webpool creates after it", "webpool-3 keeps the version it runs", "not updated"} {
		if !strings.Contains(parsed.Message, want) {
			t.Errorf("message %q lacks %q", parsed.Message, want)
		}
	}
	for _, banned := range []string{"Successfully deployed to webpool", "your dev", "runs the new code"} {
		if strings.Contains(parsed.Message, banned) || strings.Contains(parsed.NextActions, banned) {
			t.Errorf("message %q / next actions %q must not say %q", parsed.Message, parsed.NextActions, banned)
		}
	}
	if parsed.SSHReady {
		t.Error("sshReady = true: the pool was probed by service hostname as one container (PS-3)")
	}
	if ssh.hosts()["webpool"] {
		t.Error("an ssh call reached the pool by service hostname — a random container (PS-3)")
	}
	if parsed.PublicAccess != nil || parsed.SubdomainAccessEnabled {
		t.Errorf("a deploy to the pool must not decide public access on it: %+v", parsed.PublicAccess)
	}
}

func TestDeployTool_UnrecordedAliasSource_KeepsTodaysError(t *testing.T) {
	t.Parallel()

	rig := newLeaseDeployRig(t)
	ssh := &leaseSSH{}
	result, text := rig.deploy(t, ssh, map[string]any{"sourceService": "webpool-9", "targetService": "appstage"})
	if !result.IsError {
		t.Fatalf("an alias zcp holds no record for must stay refused, got: %s", text)
	}
	if !strings.Contains(text, "not adopted") {
		t.Errorf("error = %s, want the adoption refusal", text)
	}
	if len(ssh.calls) != 0 {
		t.Errorf("ssh calls = %d, want 0", len(ssh.calls))
	}
}
