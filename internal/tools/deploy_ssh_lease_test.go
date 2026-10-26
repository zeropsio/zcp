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

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *leaseSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
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
	// rt is the container the deploy runs in.
	rt runtime.Info
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
	return leaseDeployRig{stateDir: stateDir, alias: alias, mock: mock, rt: runtime.Info{InContainer: true, ServiceName: "zcp"}}
}

func (r leaseDeployRig) deploy(t *testing.T, ssh ops.SSHDeployer, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, r.mock, okHTTP, "proj-1", ssh, authInfo, nil, r.rt, r.stateDir, testDeployEngine(t), nil, DevsupSource{}, withVouchHeld())
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

// poolTargetDeploy deploys from the plain dev service appdev to the pool
// webpool, in a project before its first bootstrap: the adoption gate is not
// active yet (requireAdoption keeps refusing a pool target in an adopted
// project unless the source is a lease of that pool).
func poolTargetDeploy(t *testing.T, ssh ops.SSHDeployer) (*mcp.CallToolResult, string) {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return poolTargetDeployIn(t, ssh, stateDir)
}

// poolTargetDeployIn is poolTargetDeploy over a prepared state directory.
func poolTargetDeployIn(t *testing.T, ssh ops.SSHDeployer, stateDir string) (*mcp.CallToolResult, string) {
	t.Helper()
	return poolDeployCall(t, ssh, stateDir, map[string]any{"sourceService": "appdev", "targetService": "webpool"})
}

// poolDeployCall runs zerops_deploy with args in a project holding the dev
// service appdev and the pool webpool.
func poolDeployCall(t *testing.T, ssh ops.SSHDeployer, stateDir string, args map[string]any) (*mcp.CallToolResult, string) {
	t.Helper()
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", Status: serviceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{ID: "svc-pool", Name: "webpool", Status: serviceStatusRunning, ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1"}},
		}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-pool", ProjectID: "proj-1", ServiceStackID: "svc-pool", Status: statusActive, Sequence: 1},
		})
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{InContainer: true, ServiceName: "zcp"}, stateDir, testDeployEngine(t), nil, DevsupSource{}, withVouchHeld())
	result := callTool(t, srv, "zerops_deploy", args)
	return result, getTextContent(t, result)
}

// assertPoolDeployResult is what every finished deploy to the pool webpool
// says and does not do, whatever its source (docs/spec-workflows.md §8 DP-7,
// PS-3): it reaches only containers the pool creates later, points at the
// rollout, and runs no verify, dev-server or public-access step on the pool.
func assertPoolDeployResult(t *testing.T, parsed ops.DeployResult) {
	t.Helper()
	if !parsed.ReachesNewContainersOnly {
		t.Error("reachesNewContainersOnly = false for a deploy whose target is a pool")
	}
	if !strings.Contains(parsed.Message, "containers webpool creates after it") {
		t.Errorf("message %q does not say it changes only containers webpool creates after it", parsed.Message)
	}
	if !strings.Contains(parsed.NextActions, `zerops_lease action=rollout pool="webpool"`) {
		t.Errorf("next actions %q do not point at zerops_lease action=rollout pool=\"webpool\"", parsed.NextActions)
	}
	for _, banned := range []string{"Successfully deployed to webpool", "acquire a fresh lease", "verify your service"} {
		if strings.Contains(parsed.Message, banned) || strings.Contains(parsed.NextActions, banned) {
			t.Errorf("message %q / next actions %q must not say %q", parsed.Message, parsed.NextActions, banned)
		}
	}
	if parsed.SSHReady {
		t.Error("sshReady = true: the pool was probed by service hostname as one container (PS-3)")
	}
	if parsed.PublicAccess != nil || parsed.SubdomainAccessEnabled || parsed.SubdomainURL != "" {
		t.Errorf("a deploy to the pool decided public access on it: %+v", parsed.PublicAccess)
	}
	for _, w := range parsed.Warnings {
		if strings.Contains(w, "subdomain") || strings.Contains(w, "public access") {
			t.Errorf("a deploy to the pool ran a public-access step: warning %q", w)
		}
	}
}

func TestDeployTool_ToPool_SaysOnlyNewContainers(t *testing.T) {
	t.Parallel()

	ssh := &leaseSSH{}
	result, text := poolTargetDeploy(t, ssh)
	if result.IsError {
		t.Fatalf("a deploy from a dev service to a pool must run: %s", text)
	}
	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if parsed.Status != statusDeployed || parsed.SourceService != "appdev" || parsed.TargetService != "webpool" {
		t.Errorf("result = %s → %s %s, want appdev → webpool %s", parsed.SourceService, parsed.TargetService, parsed.Status, statusDeployed)
	}
	assertPoolDeployResult(t, parsed)
	if !strings.Contains(parsed.Message, "keep their version") {
		t.Errorf("message %q must say the containers webpool already runs keep their version", parsed.Message)
	}
	if !ssh.hosts()["appdev"] {
		t.Errorf("the push did not run on the source appdev; hosts = %v", ssh.hosts())
	}
	if ssh.hosts()["webpool"] {
		t.Error("an ssh call reached the pool by service hostname — a random container (PS-3)")
	}
}

// TestDeployTool_ToPool_AdoptedProject_NotGated: a pool is never adopted
// (PS-5), so in a project the gate guards — one bootstrap already ran — a
// deploy to a pool from an adopted dev service must not be refused with an
// "adopt it first" no adopt can satisfy.
func TestDeployTool_ToPool_AdoptedProject_NotGated(t *testing.T) {
	t.Parallel()

	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:       "appdev",
		Mode:           topology.ModeDev,
		BootstrappedAt: "2026-10-23T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	result, text := poolTargetDeployIn(t, &leaseSSH{}, stateDir)
	if result.IsError {
		t.Fatalf("a deploy to a pool in an adopted project must run: %s", text)
	}
	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	assertPoolDeployResult(t, parsed)
}

// TestDeployTool_PoolAsSource_Refused: a pool's own hostname reaches a random
// container (PS-3), so a deploy never runs from it — neither a self-deploy
// that names only the pool nor one naming it as the source — and nothing
// reaches the pool over ssh; the refusal points at a lease alias.
func TestDeployTool_PoolAsSource_Refused(t *testing.T) {
	t.Parallel()
	for _, args := range []map[string]any{
		{"targetService": "webpool"},
		{"sourceService": "webpool", "targetService": "webpool"},
		{"sourceService": "webpool", "targetService": "appdev"},
	} {
		for _, adopted := range []bool{false, true} {
			stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if adopted {
				if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{Hostname: "appdev", Mode: topology.ModeDev, BootstrappedAt: "2026-10-23T00:00:00Z"}); err != nil {
					t.Fatal(err)
				}
			}
			ssh := &leaseSSH{}
			result, text := poolDeployCall(t, ssh, stateDir, args)
			if !result.IsError {
				t.Errorf("%v (adopted %v): a deploy from a pool's hostname ran: %s", args, adopted, text)
			}
			if ssh.hosts()["webpool"] {
				t.Errorf("%v (adopted %v): an ssh call reached the pool by its hostname — a random container (PS-3)", args, adopted)
			}
			if !result.IsError || !strings.Contains(text, "zerops_lease") {
				t.Errorf("%v (adopted %v): the refusal %s should point at a lease alias (zerops_lease)", args, adopted, text)
			}
		}
	}
}

// TestDeployTool_LeaseSource_GitPushRefused: a lease works on a branch of its
// own and pushes it itself; the git-push strategy pushes the target's tracked
// branch (main by default), so from a lease it would put the feature branch
// on the stage's delivery branch. It is refused before any ssh call, pointing
// at the agent's own push and at the ssh-mode deploy.
func TestDeployTool_LeaseSource_GitPushRefused(t *testing.T) {
	t.Parallel()

	rig := newLeaseDeployRig(t)
	ssh := &leaseSSH{}
	result, text := rig.deploy(t, ssh, map[string]any{"sourceService": rig.alias, "targetService": "appstage", "strategy": "git-push"})
	if !result.IsError {
		t.Fatalf("a git-push from a lease ran: %s", text)
	}
	for _, want := range []string{"git push", rig.alias} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal %s lacks %q", text, want)
		}
	}
	if len(ssh.hosts()) != 0 {
		t.Errorf("ssh calls ran before the refusal: %v", ssh.hosts())
	}
}

// TestDeployTool_LeaseToOwnPool_NextActionsNamesRollout: a release and a new
// acquire can hand back the same outdated container, so the finished deploy
// points at the rollout and says the lease keeps its version until it is
// released and the pool is rolled out again — never that a fresh lease gets
// the new version.
func TestDeployTool_LeaseToOwnPool_NextActionsNamesRollout(t *testing.T) {
	t.Parallel()

	rig := newLeaseDeployRig(t)
	result, text := rig.deploy(t, &leaseSSH{}, map[string]any{"sourceService": rig.alias, "targetService": "webpool"})
	if result.IsError {
		t.Fatalf("a deploy from a lease to its own pool is allowed: %s", text)
	}
	var parsed ops.DeployResult
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	assertPoolDeployResult(t, parsed)
	for _, want := range []string{"webpool-3 keeps its version", "released", "rolled out again"} {
		if !strings.Contains(parsed.NextActions, want) {
			t.Errorf("next actions %q lack %q", parsed.NextActions, want)
		}
	}
	if strings.Contains(parsed.NextActions, "release webpool-3 and acquire") {
		t.Errorf("next actions %q still claim a fresh lease gets the new version", parsed.NextActions)
	}
}
