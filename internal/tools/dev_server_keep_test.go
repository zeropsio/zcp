// Tests for: dev_server_keep.go — the dev servers zcp keeps (docs/spec-
// workflows.md §8 O4): what zerops_dev_server starts is brought back by zcp
// after its container restarts or is redeployed, until the agent stops it.

package tools

import (
	"context"
	"encoding/json"
	"errors"
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

// keepSSH answers by what a command does rather than by call order, so a test
// reads as the container's state: its identity (one per container life), and
// whether a spawned dev server comes up.
type keepSSH struct {
	mu          sync.Mutex
	identity    string // "" → the identity read fails (container unreachable)
	probeFails  bool
	spawns      []string
	identityErr error
}

func (s *keepSSH) ExecSSH(_ context.Context, _ string, command string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case strings.Contains(command, "/proc/1/stat"):
		if s.identity == "" {
			return nil, errors.New("ssh: connect to host: connection refused")
		}
		// The remote prints "<hostname> <init start time>"; zcp names the
		// life "<hostname>/<init start time>".
		return []byte(strings.Replace(s.identity, "/", " ", 1) + "\n"), s.identityErr
	case strings.Contains(command, "curl"):
		if s.probeFails {
			return []byte("FAIL 000"), errors.New("exit status 1")
		}
		return []byte("OK 200 42"), nil
	case strings.Contains(command, "kill -0"):
		return []byte("alive\n"), nil
	case strings.Contains(command, "tail -n"):
		return []byte("ready in 2s"), nil
	}
	return nil, nil
}

func (s *keepSSH) ExecSSHBackground(_ context.Context, _ string, command string, _ time.Duration) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spawns = append(s.spawns, command)
	return []byte("zcp-dev-server-spawned pid=4242"), nil
}

func (s *keepSSH) spawnCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.spawns)
}

// recordingUnits is a fake ops.UnitRegistrar.
type recordingUnits struct {
	mu    sync.Mutex
	calls [][2]string
	err   error
}

func (u *recordingUnits) EnsureUnit(_ context.Context, name, command string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls = append(u.calls, [2]string{name, command})
	return u.err
}

// keptAppdev is appdev's kept dev server as started in the container life
// "appdev-1/100".
func keptAppdev() workflow.KeptDevServer {
	return workflow.KeptDevServer{
		Hostname:   "appdev",
		Command:    "npm run dev",
		Port:       3000,
		HealthPath: "/",
		Container:  "appdev-1/100",
		StartedAt:  "2026-09-29T20:20:00Z",
	}
}

// keptAppdevStarted is keptAppdev as a pointer, for table fields.
func keptAppdevStarted() *workflow.KeptDevServer {
	rec := keptAppdev()
	return &rec
}

// TestRestoreKeptDevServer pins the bring-back: the kept start runs again, the
// way the agent made it, exactly once per new container life, and never when
// the container is the one it was started in (a crash is the agent's to see).
func TestRestoreKeptDevServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		kept        *workflow.KeptDevServer
		identity    string
		probeFails  bool
		wantResult  bool
		wantRunning bool
	}{
		{name: "nothing kept", kept: nil, identity: "appdev-1/900"},
		{name: "same container life — a crashed or stopped-by-hand server stays down", kept: keptAppdevStarted(), identity: "appdev-1/100"},
		{name: "container unreachable", kept: keptAppdevStarted(), identity: ""},
		{name: "restarted — brought back", kept: keptAppdevStarted(), identity: "appdev-1/900", wantResult: true, wantRunning: true},
		{name: "redeployed — brought back", kept: keptAppdevStarted(), identity: "appdev-2/77", wantResult: true, wantRunning: true},
		{name: "brought back but it does not come up", kept: keptAppdevStarted(), identity: "appdev-1/900", probeFails: true, wantResult: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tt.kept != nil {
				if err := workflow.KeepDevServer(dir, *tt.kept); err != nil {
					t.Fatalf("KeepDevServer: %v", err)
				}
			}
			ssh := &keepSSH{identity: tt.identity, probeFails: tt.probeFails}

			got := restoreKeptDevServer(context.Background(), ssh, dir, "appdev")
			if (got != nil) != tt.wantResult {
				t.Fatalf("restore result = %+v, want present=%v", got, tt.wantResult)
			}
			if !tt.wantResult {
				if ssh.spawnCount() != 0 {
					t.Fatalf("nothing to bring back, yet %d spawn(s)", ssh.spawnCount())
				}
				return
			}
			if got.Running != tt.wantRunning {
				t.Errorf("Running = %v, want %v (%s)", got.Running, tt.wantRunning, got.Message)
			}
			if ssh.spawnCount() != 1 || !strings.Contains(ssh.spawns[0], "npm run dev") {
				t.Fatalf("want one spawn of the kept command, got %v", ssh.spawns)
			}
			rec, _ := workflow.KeptDevServerFor(dir, "appdev")
			if rec == nil || rec.Container != tt.identity {
				t.Fatalf("the kept record moves to the new container life, got %+v", rec)
			}
			if rec.LastRestore == nil || rec.LastRestore.Running != tt.wantRunning {
				t.Errorf("LastRestore = %+v, want running=%v", rec.LastRestore, tt.wantRunning)
			}

			// The keeper's next tick in the same life finds nothing to do.
			if again := restoreKeptDevServer(context.Background(), ssh, dir, "appdev"); again != nil {
				t.Errorf("a second bring-back in the same container life: %+v", again)
			}
			if ssh.spawnCount() != 1 {
				t.Errorf("spawns after the second pass = %d, want 1", ssh.spawnCount())
			}
		})
	}
}

// TestKeepDevServers_OnePass restores every kept server whose container cycled
// and reports only those.
func TestKeepDevServers_OnePass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, rec := range []workflow.KeptDevServer{keptAppdev(), {Hostname: "apidev", Command: "go run .", Port: 8080, Container: "apidev-1/5"}} {
		if err := workflow.KeepDevServer(dir, rec); err != nil {
			t.Fatalf("KeepDevServer: %v", err)
		}
	}
	// Both answer with the same fake identity: appdev's changed, apidev's too.
	ssh := &keepSSH{identity: "apidev-1/5"}
	restored := KeepDevServers(context.Background(), ssh, dir)
	if len(restored) != 1 || restored[0].Hostname != "appdev" {
		t.Fatalf("restored = %+v, want only appdev (apidev runs in the same container life)", restored)
	}
}

// TestDevServerTool_StartKeepsItAndStopForgetsIt pins the agent's control: a
// successful start is kept (and the keeper made sure of), a failed one leaves
// what was kept, and stop forgets it.
func TestDevServerTool_StartKeepsItAndStopForgetsIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test"}).
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "nodejs@22"}}})
	units := &recordingUnits{}
	ssh := &keepSSH{identity: "appdev-1/100"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, mock, nil, "proj-1", ssh, dir, units)

	result := callTool(t, srv, "zerops_dev_server", map[string]any{
		"action": "start", "hostname": "appdev", "command": "npm run dev", "port": 3000,
	})
	if result.IsError {
		t.Fatalf("start: %s", getTextContent(t, result))
	}
	var resp struct {
		Running bool   `json:"running"`
		Kept    *bool  `json:"kept"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !resp.Running || resp.Kept == nil || !*resp.Kept {
		t.Fatalf("a successful start is kept: %+v", resp)
	}
	if !strings.Contains(resp.Message, "restart") || !strings.Contains(resp.Message, "redeploy") {
		t.Errorf("the message says zcp brings it back after a restart or a redeploy: %q", resp.Message)
	}
	rec, _ := workflow.KeptDevServerFor(dir, "appdev")
	if rec == nil || rec.Command != "npm run dev" || rec.Port != 3000 || rec.Container != "appdev-1/100" {
		t.Fatalf("kept record = %+v", rec)
	}
	if len(units.calls) != 1 || units.calls[0][0] != devServerKeeperUnit || !strings.Contains(units.calls[0][1], "zcp dev-server keep --state-dir "+dir) {
		t.Fatalf("the keeper unit is ensured once with the state dir: %+v", units.calls)
	}

	// A failed start leaves what zcp keeps unchanged.
	ssh.probeFails = true
	failed := callTool(t, srv, "zerops_dev_server", map[string]any{
		"action": "start", "hostname": "appdev", "command": "npm run broken", "port": 3000,
	})
	if failed.IsError {
		t.Fatalf("failed start is a result, not an error: %s", getTextContent(t, failed))
	}
	if rec, _ := workflow.KeptDevServerFor(dir, "appdev"); rec == nil || rec.Command != "npm run dev" {
		t.Fatalf("a failed start must not replace the kept one, got %+v", rec)
	}

	stopped := callTool(t, srv, "zerops_dev_server", map[string]any{
		"action": "stop", "hostname": "appdev", "port": 3000,
	})
	if stopped.IsError {
		t.Fatalf("stop: %s", getTextContent(t, stopped))
	}
	resp.Kept = nil
	if err := json.Unmarshal([]byte(getTextContent(t, stopped)), &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.Kept == nil || *resp.Kept {
		t.Errorf("stop reports kept=false: %s", getTextContent(t, stopped))
	}
	if rec, _ := workflow.KeptDevServerFor(dir, "appdev"); rec != nil {
		t.Errorf("stop forgets the kept dev server, got %+v", rec)
	}
}

// TestDevServerTool_KeeperUnavailable_StillKeptWithWarning: when the keeper
// cannot be registered the start is still kept — zcp's own deploys bring it
// back — and the response says what is lost.
func TestDevServerTool_KeeperUnavailable_StillKeptWithWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test"}).
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "ACTIVE"}})
	units := &recordingUnits{err: errors.New("sudo: a password is required")}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, mock, nil, "proj-1", &keepSSH{identity: "appdev-1/100"}, dir, units)

	result := callTool(t, srv, "zerops_dev_server", map[string]any{
		"action": "start", "hostname": "appdev", "command": "npm run dev", "port": 3000,
	})
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("start: %s", text)
	}
	if rec, _ := workflow.KeptDevServerFor(dir, "appdev"); rec == nil {
		t.Fatal("still kept without the keeper")
	}
	if !strings.Contains(text, "keeper") {
		t.Errorf("the response names the missing keeper: %s", text)
	}
}

var _ ops.UnitRegistrar = (*recordingUnits)(nil)

// TestBringBackKeptDevServer pins what a deploy onto a dev container with a
// kept dev server reports: the deploy replaced the container, so the server is
// started again at once, and the next step follows from whether it came up.
func TestBringBackKeptDevServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		kept         bool
		probeFails   bool
		wantListener bool
		wantNext     string
	}{
		{name: "nothing kept — the deploy's own next step stands", kept: false, wantNext: "deploy's own"},
		{name: "kept and back up", kept: true, wantListener: true, wantNext: "zerops_verify"},
		{name: "kept but not up", kept: true, probeFails: true, wantNext: "action=restart"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tt.kept {
				if err := workflow.KeepDevServer(dir, keptAppdev()); err != nil {
					t.Fatalf("KeepDevServer: %v", err)
				}
			}
			ssh := &keepSSH{identity: "appdev-2/77", probeFails: tt.probeFails}
			result := &ops.DeployResult{Status: statusDeployed, TargetService: "appdev", NextActions: "deploy's own"}

			listener := bringBackKeptDevServer(context.Background(), ssh, dir, "appdev", result)
			if listener != tt.wantListener {
				t.Errorf("listener = %v, want %v", listener, tt.wantListener)
			}
			if (result.DevServer != nil) != tt.kept {
				t.Fatalf("DevServer = %+v, want present=%v", result.DevServer, tt.kept)
			}
			if !strings.Contains(result.NextActions, tt.wantNext) {
				t.Errorf("NextActions = %q, want it to contain %q", result.NextActions, tt.wantNext)
			}
		})
	}
}

// TestDeployTool_SSHMode_BringsBackKeptDevServer: a self-deploy onto a dev
// container whose dev server zcp keeps answers with that server started again
// — the agent no longer restarts it after every deploy.
func TestDeployTool_SSHMode_BringsBackKeptDevServer(t *testing.T) {
	t.Parallel()
	projectRoot := t.TempDir()
	stateDir := filepath.Join(projectRoot, ".zcp", "state")
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-09-29",
		PrimarySetupName: "appdev",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mountDir := filepath.Join(projectRoot, "appdev")
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		t.Fatalf("mkdir mount: %v", err)
	}
	yaml := "zerops:\n  - setup: appdev\n    build:\n      base: nodejs@22\n      deployFiles: [.]\n    run:\n      ports:\n        - port: 3000\n          httpSupport: true\n      start: zsc noop --silent\n"
	if err := os.WriteFile(filepath.Join(mountDir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	if err := workflow.KeepDevServer(stateDir, keptAppdev()); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", Status: serviceStatusRunning,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}}}).
		WithAppVersionEvents([]platform.AppVersionEvent{
			{ID: "av-2", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 2},
		})
	ssh := &deployKeepSSH{keepSSH: keepSSH{identity: "appdev-2/77"}}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "appdev"})
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("deploy: %s", text)
	}
	var parsed struct {
		Status      string               `json:"status"`
		NextActions string               `json:"nextActions"`
		DevServer   *ops.DevServerResult `json:"devServer"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Status != statusDeployed {
		t.Fatalf("status = %s, want DEPLOYED: %s", parsed.Status, text)
	}
	if parsed.DevServer == nil || !parsed.DevServer.Running {
		t.Fatalf("the kept dev server is back after the deploy: %s", text)
	}
	if !strings.Contains(parsed.NextActions, "zerops_verify") {
		t.Errorf("next step is to verify, got %q", parsed.NextActions)
	}
	if rec, _ := workflow.KeptDevServerFor(stateDir, "appdev"); rec == nil || rec.Container != "appdev-2/77" {
		t.Errorf("the kept record follows the new container: %+v", rec)
	}
}

// deployKeepSSH is keepSSH that also answers a zcli-push deploy: the source
// has no git repo, and every other deploy command succeeds.
type deployKeepSSH struct {
	keepSSH
}

func (s *deployKeepSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	switch {
	case strings.Contains(command, "rev-parse --verify HEAD") && strings.Contains(command, "git status --porcelain"):
		return nil, errStubSSHNoRepo
	case strings.Contains(command, "/proc/1/stat"), strings.Contains(command, "curl"),
		strings.Contains(command, "kill -0"), strings.Contains(command, "tail -n"):
		return s.keepSSH.ExecSSH(ctx, hostname, command)
	}
	return []byte("ok"), nil
}

// TestRunDevServerKeeper runs passes until its context ends and logs each
// bring-back — the keeper unit's whole life.
func TestRunDevServerKeeper(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.KeepDevServer(dir, keptAppdev()); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	ssh := &keepSSH{identity: "appdev-1/900"}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var log strings.Builder
	RunDevServerKeeper(ctx, ssh, dir, 10*time.Millisecond, &log)

	if ssh.spawnCount() != 1 {
		t.Fatalf("one bring-back across all passes, got %d spawns", ssh.spawnCount())
	}
	if !strings.Contains(log.String(), "appdev") || !strings.Contains(log.String(), "npm run dev") {
		t.Errorf("the bring-back is logged: %q", log.String())
	}
}
