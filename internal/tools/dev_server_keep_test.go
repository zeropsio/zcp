// Tests for: dev_server_keep.go — the dev servers zcp keeps (docs/spec-
// workflows.md §8 O4): what zerops_dev_server starts is brought back by zcp
// after its container restarts or is redeployed, until the agent stops it.

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
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
// reads as the container's state: its identity (one per container life),
// whether something listens on the dev port, and whether a spawned dev server
// comes up.
type keepSSH struct {
	mu         sync.Mutex
	identity   string // "" → the identity read fails (container unreachable)
	listening  bool
	probeFails bool
	stopFails  bool
	// aliveBeforeSpawn is the pidfile's process before this test's first
	// spawn — a server from an earlier bring-back still starting. After a
	// spawn, the spawned process is alive.
	aliveBeforeSpawn bool
	onSpawn          func()
	spawns           []string
	kills            int
}

func (s *keepSSH) ExecSSH(_ context.Context, _ string, command string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case strings.Contains(command, "/proc/1/stat"):
		if s.identity == "" {
			return nil, errors.New("ssh: connect to host: connection refused")
		}
		// The remote prints "<hostname> <boot id> <init start time>"; zcp
		// names the life "<hostname>/<boot id>/<init start time>".
		return []byte(strings.ReplaceAll(s.identity, "/", " ") + "\n"), nil
	case strings.Contains(command, "/proc/net/tcp"):
		if s.listening {
			return []byte("listening\n"), nil
		}
		return []byte("free\n"), nil
	case strings.Contains(command, "curl"):
		if s.probeFails {
			return []byte("FAIL 000"), errors.New("exit status 1")
		}
		return []byte("OK 200 42"), nil
	case strings.Contains(command, "kill -0"):
		if len(s.spawns) > 0 || s.aliveBeforeSpawn {
			return []byte("alive\n"), nil
		}
		return []byte("dead\n"), nil
	case strings.Contains(command, "then kill 4242"):
		s.kills++
		return nil, nil
	case strings.Contains(command, "pkill"), strings.Contains(command, "fuser"):
		if s.stopFails {
			return nil, errors.New("ssh: connect to host: connection reset")
		}
		return []byte("stopped"), nil
	case strings.Contains(command, "tail -n"):
		return []byte("ready in 2s"), nil
	}
	return nil, nil
}

func (s *keepSSH) ExecSSHBackground(_ context.Context, _ string, command string, _ time.Duration) ([]byte, error) {
	s.mu.Lock()
	s.spawns = append(s.spawns, command)
	hook := s.onSpawn
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	return []byte("zcp-dev-server-spawned pid=4242"), nil
}

func (s *keepSSH) spawnCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.spawns)
}

func (s *keepSSH) killCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kills
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

func (u *recordingUnits) callCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.calls)
}

var _ ops.UnitRegistrar = (*recordingUnits)(nil)

// keptAppdev is appdev's kept dev server as started in the container life
// "appdev-1/boot-a/100".
func keptAppdev() workflow.KeptDevServer {
	return workflow.KeptDevServer{
		Hostname:   "appdev",
		Command:    "npm run dev",
		Port:       3000,
		HealthPath: "/",
		Container:  "appdev-1/boot-a/100",
		StartedAt:  "2026-09-29T20:20:00Z",
	}
}

// keptAppdevStarted is keptAppdev as a pointer, for table fields.
func keptAppdevStarted() *workflow.KeptDevServer {
	rec := keptAppdev()
	return &rec
}

// keptAppdevRestored is appdev's kept server after a bring-back in the life
// "appdev-1/boot-b/900" that ended as given, at the given moment; its spawn
// reported pid 4242.
func keptAppdevRestored(at time.Time, running bool, reason string) *workflow.KeptDevServer {
	rec := keptAppdev()
	rec.Container = "appdev-1/boot-b/900"
	rec.LastRestore = &workflow.DevServerRestore{At: at.UTC().Format(time.RFC3339), Running: running, Reason: reason, Attempts: 1, PID: 4242}
	return &rec
}

// keptAppdevSpawnNeverRan is appdev's kept server after a bring-back whose
// spawn never reached the container: it started no process, and the pidfile
// it would have replaced may be an earlier life's.
func keptAppdevSpawnNeverRan() *workflow.KeptDevServer {
	rec := keptAppdevRestored(time.Now().Add(-time.Minute), false, "spawn_error")
	rec.LastRestore.PID = 0
	return rec
}

func devServerToolServer(t *testing.T, dir string, ssh ops.SSHDeployer, units ops.UnitRegistrar) *mcp.Server {
	t.Helper()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "proj-1", Name: "test"}).
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "nodejs@22"}}})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDevServer(srv, mock, nil, "proj-1", ssh, dir, units)
	return srv
}

type devServerToolResp struct {
	Running  bool     `json:"running"`
	Kept     *bool    `json:"kept"`
	Message  string   `json:"message"`
	Warnings []string `json:"warnings"`
}

func callDevServerTool(t *testing.T, srv *mcp.Server, args map[string]any) (devServerToolResp, *mcp.CallToolResult) {
	t.Helper()
	result := callTool(t, srv, "zerops_dev_server", args)
	var resp devServerToolResp
	if !result.IsError {
		if err := json.Unmarshal([]byte(getTextContent(t, result)), &resp); err != nil {
			t.Fatalf("parse: %v", err)
		}
	}
	return resp, result
}

// TestRestoreKeptDevServer pins the bring-back: the kept start runs again, the
// way the agent made it, once per new container life (and again only after a
// bring-back that did not come up) — never over a server that already
// listens, never when the container is the one it was started in.
func TestRestoreKeptDevServer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		kept             *workflow.KeptDevServer
		identity         string
		listening        bool
		probeFails       bool
		aliveBeforeSpawn bool
		wantResult       bool
		wantRunning      bool
		wantSpawn        bool
	}{
		{name: "nothing kept", kept: nil, identity: "appdev-1/boot-b/900"},
		{name: "same container life — a crashed or stopped-by-hand server stays down", kept: keptAppdevStarted(), identity: "appdev-1/boot-a/100"},
		{name: "container unreachable", kept: keptAppdevStarted(), identity: ""},
		{name: "restarted — brought back", kept: keptAppdevStarted(), identity: "appdev-1/boot-b/900", wantResult: true, wantRunning: true, wantSpawn: true},
		{name: "redeployed — brought back", kept: keptAppdevStarted(), identity: "appdev-2/boot-c/77", wantResult: true, wantRunning: true, wantSpawn: true},
		{name: "brought back but it does not come up", kept: keptAppdevStarted(), identity: "appdev-1/boot-b/900", probeFails: true, wantResult: true, wantSpawn: true},
		{name: "something already listens — left as it is", kept: keptAppdevStarted(), identity: "appdev-1/boot-b/900", listening: true, wantResult: true, wantRunning: true},
		{name: "a failed bring-back is retried", kept: keptAppdevRestored(time.Now().Add(-time.Minute), false, "health_probe_connection_refused"), identity: "appdev-1/boot-b/900", wantResult: true, wantRunning: true, wantSpawn: true},
		{name: "a retry that finds its port answering starts nothing beside it", kept: keptAppdevRestored(time.Now().Add(-time.Minute), false, ops.ReasonHealthProbeTimeout), identity: "appdev-1/boot-b/900", listening: true, wantResult: true, wantRunning: true},
		{name: "a bring-back that came up — a later crash stays down", kept: keptAppdevRestored(time.Now().Add(-time.Hour), true, ""), identity: "appdev-1/boot-b/900"},
		{name: "no retry while the last bring-back's process still starts", kept: keptAppdevRestored(time.Now().Add(-time.Minute), false, ops.ReasonHealthProbeTimeout), identity: "appdev-1/boot-b/900", aliveBeforeSpawn: true},
		{name: "a bring-back that started no process is retried whatever an old pidfile names", kept: keptAppdevSpawnNeverRan(), identity: "appdev-1/boot-b/900", aliveBeforeSpawn: true, wantResult: true, wantRunning: true, wantSpawn: true},
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
			ssh := &keepSSH{identity: tt.identity, listening: tt.listening, probeFails: tt.probeFails, aliveBeforeSpawn: tt.aliveBeforeSpawn}

			got := restoreKeptDevServer(context.Background(), ssh, dir, "appdev", "")
			if (got != nil) != tt.wantResult {
				t.Fatalf("restore result = %+v, want present=%v", got, tt.wantResult)
			}
			if (ssh.spawnCount() == 1) != tt.wantSpawn || ssh.spawnCount() > 1 {
				t.Fatalf("spawns = %v, want spawned=%v", ssh.spawns, tt.wantSpawn)
			}
			if tt.wantSpawn && !strings.Contains(ssh.spawns[0], "npm run dev") {
				t.Errorf("the kept command is what runs again: %q", ssh.spawns[0])
			}
			if !tt.wantResult {
				if tt.kept != nil {
					if now, _ := workflow.KeptDevServerFor(dir, "appdev"); now.LastRestore != nil && tt.kept.LastRestore != nil && now.LastRestore.Attempts != tt.kept.LastRestore.Attempts {
						t.Errorf("a pass that starts nothing spends no attempt: %+v", now.LastRestore)
					}
				}
				return
			}
			if got.Running != tt.wantRunning {
				t.Errorf("Running = %v, want %v (%s)", got.Running, tt.wantRunning, got.Message)
			}
			rec, _ := workflow.KeptDevServerFor(dir, "appdev")
			if rec == nil || rec.Container != tt.identity {
				t.Fatalf("the kept record follows the container life, got %+v", rec)
			}
			if rec.LastRestore == nil || rec.LastRestore.Running != tt.wantRunning || rec.LastRestore.Reason == workflow.DevServerRestoring {
				t.Errorf("LastRestore = %+v, want the finished outcome running=%v", rec.LastRestore, tt.wantRunning)
			}
			if tt.wantSpawn && rec.LastRestore != nil && rec.LastRestore.PID != 4242 {
				t.Errorf("the bring-back keeps the pid its spawn reported: %+v", rec.LastRestore)
			}

			// The keeper's next pass, straight after, finds nothing to do.
			spawned := ssh.spawnCount()
			if again := restoreKeptDevServer(context.Background(), ssh, dir, "appdev", ""); again != nil {
				t.Errorf("a second bring-back straight after: %+v", again)
			}
			if ssh.spawnCount() != spawned {
				t.Errorf("spawns after the second pass = %d, want %d", ssh.spawnCount(), spawned)
			}
		})
	}
}

// TestRestoreKeptDevServer_SlowBringBackThatCameUp: a bring-back that did not
// answer in time (a first compile) whose process lives on is left to start,
// and once it answers — its port listens, or, for a worker without an HTTP
// probe, its process lives — the keeper's next pass records it up without
// starting anything. A later crash in that container life then stays down for
// the agent, like any other.
func TestRestoreKeptDevServer_SlowBringBackThatCameUp(t *testing.T) {
	t.Parallel()
	worker := func() *workflow.KeptDevServer {
		rec := keptAppdevRestored(time.Now().Add(-time.Minute), false, "liveness_check_error")
		rec.Port, rec.HealthPath, rec.NoHTTPProbe = 0, "", true
		return rec
	}
	tests := []struct {
		name      string
		kept      *workflow.KeptDevServer
		listening bool
		wantUp    bool
	}{
		{name: "an HTTP server still compiling", kept: keptAppdevRestored(time.Now().Add(-time.Minute), false, ops.ReasonHealthProbeTimeout)},
		{name: "an HTTP server whose port listens now", kept: keptAppdevRestored(time.Now().Add(-time.Minute), false, ops.ReasonHealthProbeTimeout), listening: true, wantUp: true},
		{name: "a worker whose process lives", kept: worker(), wantUp: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := workflow.KeepDevServer(dir, *tt.kept); err != nil {
				t.Fatalf("KeepDevServer: %v", err)
			}
			ssh := &keepSSH{identity: tt.kept.Container, aliveBeforeSpawn: true, listening: tt.listening}

			if got := restoreKeptDevServer(context.Background(), ssh, dir, "appdev", ""); got != nil {
				t.Fatalf("nothing is brought back while its process lives: %+v", got)
			}
			rec, _ := workflow.KeptDevServerFor(dir, "appdev")
			if rec.LastRestore == nil || rec.LastRestore.Running != tt.wantUp || rec.LastRestore.Attempts != 1 {
				t.Fatalf("LastRestore = %+v, want running=%v with no attempt spent", rec.LastRestore, tt.wantUp)
			}
			if !tt.wantUp {
				return
			}

			// It crashes, or is killed by hand, in the same container life.
			ssh.mu.Lock()
			ssh.aliveBeforeSpawn, ssh.listening = false, false
			ssh.mu.Unlock()
			if got := restoreKeptDevServer(context.Background(), ssh, dir, "appdev", ""); got != nil {
				t.Errorf("a crash in its own container life stays down: %+v", got)
			}
			if ssh.spawnCount() != 0 {
				t.Errorf("spawns = %d, want none", ssh.spawnCount())
			}
		})
	}
}

// TestRestoreKeptDevServer_StoppedMeanwhile: the agent's stop forgets the kept
// server before it kills; a bring-back that was mid-start when that happened
// kills the process it just spawned instead of leaving a stopped server up.
func TestRestoreKeptDevServer_StoppedMeanwhile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.KeepDevServer(dir, keptAppdev()); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	ssh := &keepSSH{identity: "appdev-1/boot-b/900"}
	ssh.onSpawn = func() { _ = workflow.ForgetDevServer(dir, "appdev") }

	if got := restoreKeptDevServer(context.Background(), ssh, dir, "appdev", ""); got != nil {
		t.Fatalf("a server stopped mid-bring-back is not reported as brought back: %+v", got)
	}
	if ssh.killCount() != 1 {
		t.Errorf("the spawned process is killed again, kills = %d", ssh.killCount())
	}
}

// TestRestoreKeptDevServer_UnreadableIndexKillsNothing: only a record read
// without error and found gone means "stopped" — a store that cannot be read
// says nothing about the server, which keeps running.
func TestRestoreKeptDevServer_UnreadableIndexKillsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.KeepDevServer(dir, keptAppdev()); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	ssh := &keepSSH{identity: "appdev-1/boot-b/900"}
	ssh.onSpawn = func() {
		if err := os.WriteFile(filepath.Join(dir, "dev-servers.json"), []byte("{not json"), 0o600); err != nil {
			t.Error(err)
		}
	}
	_ = restoreKeptDevServer(context.Background(), ssh, dir, "appdev", "")
	if ssh.killCount() != 0 {
		t.Errorf("an unreadable index must not kill the fresh server, kills = %d", ssh.killCount())
	}
}

// TestRestoreKeptDevServerAfterDeploy_NeverClaimsThePreDeployLife: the old
// container can still answer right after DEPLOYED. Even when its life holds a
// retry-able failed bring-back, the deploy starts nothing in that dying
// container — it waits for the new one.
func TestRestoreKeptDevServerAfterDeploy_NeverClaimsThePreDeployLife(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	failed := keptAppdevRestored(time.Now().Add(-time.Minute), false, "health_probe_connection_refused")
	if err := workflow.KeepDevServer(dir, *failed); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	before, _ := workflow.KeptDevServerFor(dir, "appdev")
	ssh := &keepSSH{identity: failed.Container} // the old container still answers
	if got := restoreKeptDevServerAfterDeploy(context.Background(), ssh, dir, "appdev", before, time.Millisecond); got != nil {
		t.Fatalf("nothing is reported from the old container: %+v", got)
	}
	if ssh.spawnCount() != 0 {
		t.Errorf("nothing starts in the dying container, spawns = %d", ssh.spawnCount())
	}
}

// TestKeepDevServers_OnePass restores every kept server whose container cycled
// and reports only those.
func TestKeepDevServers_OnePass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, rec := range []workflow.KeptDevServer{keptAppdev(), {Hostname: "apidev", Command: "go run .", Port: 8080, Container: "apidev-1/boot-d/5"}} {
		if err := workflow.KeepDevServer(dir, rec); err != nil {
			t.Fatalf("KeepDevServer: %v", err)
		}
	}
	// Both answer with the same fake identity: appdev's changed, apidev's not.
	ssh := &keepSSH{identity: "apidev-1/boot-d/5"}
	restored := KeepDevServers(context.Background(), ssh, dir)
	if len(restored) != 1 || restored[0].Hostname != "appdev" {
		t.Fatalf("restored = %+v, want only appdev (apidev runs in the same container life)", restored)
	}
}

// TestRunDevServerKeeper runs passes until its context ends and logs each
// bring-back — the keeper unit's whole life.
func TestRunDevServerKeeper(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.KeepDevServer(dir, keptAppdev()); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	ssh := &keepSSH{identity: "appdev-1/boot-b/900"}
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

// TestDevServerTool_StartKeepsItAndStopForgetsIt pins the agent's control: a
// successful start is kept (and the keeper made sure of), a failed one leaves
// what was kept, and stop forgets it.
func TestDevServerTool_StartKeepsItAndStopForgetsIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	units := &recordingUnits{}
	ssh := &keepSSH{identity: "appdev-1/boot-a/100"}
	srv := devServerToolServer(t, dir, ssh, units)

	resp, result := callDevServerTool(t, srv, map[string]any{
		"action": "start", "hostname": "appdev", "command": "npm run dev", "port": 3000,
	})
	if result.IsError {
		t.Fatalf("start: %s", getTextContent(t, result))
	}
	if !resp.Running || resp.Kept == nil || !*resp.Kept {
		t.Fatalf("a successful start is kept: %+v", resp)
	}
	if !strings.Contains(resp.Message, "restart") || !strings.Contains(resp.Message, "redeploy") {
		t.Errorf("the message says zcp brings it back after a restart or a redeploy: %q", resp.Message)
	}
	rec, _ := workflow.KeptDevServerFor(dir, "appdev")
	if rec == nil || rec.Command != "npm run dev" || rec.Port != 3000 || rec.Container != "appdev-1/boot-a/100" {
		t.Fatalf("kept record = %+v", rec)
	}
	if units.callCount() != 1 || units.calls[0][0] != devServerKeeperUnit(dir) || units.calls[0][1] != "zcp dev-server keep --state-dir "+dir {
		t.Fatalf("the keeper unit is ensured once with the state dir: %+v", units.calls)
	}

	// A failed start leaves what zcp keeps unchanged.
	ssh.probeFails = true
	if _, failed := callDevServerTool(t, srv, map[string]any{
		"action": "start", "hostname": "appdev", "command": "npm run broken", "port": 3000,
	}); failed.IsError {
		t.Fatalf("failed start is a result, not an error: %s", getTextContent(t, failed))
	}
	if rec, _ := workflow.KeptDevServerFor(dir, "appdev"); rec == nil || rec.Command != "npm run dev" {
		t.Fatalf("a failed start must not replace the kept one, got %+v", rec)
	}

	stopped, result := callDevServerTool(t, srv, map[string]any{"action": "stop", "hostname": "appdev", "port": 3000})
	if result.IsError {
		t.Fatalf("stop: %s", getTextContent(t, result))
	}
	if stopped.Kept == nil || *stopped.Kept {
		t.Errorf("stop reports kept=false: %+v", stopped)
	}
	if rec, _ := workflow.KeptDevServerFor(dir, "appdev"); rec != nil {
		t.Errorf("stop forgets the kept dev server, got %+v", rec)
	}
}

// TestDevServerTool_StopOnlyForgetsTheServerItNames: a stop aimed at another
// server on the same container leaves the kept one kept; a stop whose kill
// fails has already forgotten it — nothing brings a stopped server back.
func TestDevServerTool_StopOnlyForgetsTheServerItNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       map[string]any
		stopFails  bool
		wantKept   bool
		wantErrOut bool
	}{
		{name: "another port", args: map[string]any{"port": 5173}, wantKept: true},
		{name: "another process", args: map[string]any{"processMatch": "vite"}, wantKept: true},
		{name: "its port", args: map[string]any{"port": 3000}, wantKept: false},
		{name: "its command", args: map[string]any{"command": "npm run dev"}, wantKept: false},
		{name: "another port, but its process", args: map[string]any{"port": 5173, "processMatch": "npm run dev"}, wantKept: false},
		{name: "its port, and the kill fails", args: map[string]any{"port": 3000}, stopFails: true, wantKept: false, wantErrOut: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := workflow.KeepDevServer(dir, keptAppdev()); err != nil {
				t.Fatalf("KeepDevServer: %v", err)
			}
			srv := devServerToolServer(t, dir, &keepSSH{identity: "appdev-1/boot-a/100", stopFails: tt.stopFails}, nil)
			args := map[string]any{"action": "stop", "hostname": "appdev"}
			maps.Copy(args, tt.args)
			_, result := callDevServerTool(t, srv, args)
			if result.IsError != tt.wantErrOut {
				t.Fatalf("IsError = %v, want %v: %s", result.IsError, tt.wantErrOut, getTextContent(t, result))
			}
			rec, _ := workflow.KeptDevServerFor(dir, "appdev")
			if (rec != nil) != tt.wantKept {
				t.Errorf("kept after the stop = %v, want %v", rec != nil, tt.wantKept)
			}
		})
	}
}

// TestDevServerTool_StartTakesTheContainerLifeOver: after a restart the agent
// often starts the server itself. That start takes the new container life
// over before it spawns, so the keeper's pass in the same moment finds nothing
// to claim — one server, not two fighting for the port.
func TestDevServerTool_StartTakesTheContainerLifeOver(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.KeepDevServer(dir, keptAppdev()); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	ssh := &keepSSH{identity: "appdev-1/boot-b/900"} // the container restarted
	srv := devServerToolServer(t, dir, ssh, nil)

	// The keeper's pass lands while the agent's start is spawning.
	var keeperClaimed *ops.DevServerResult
	ssh.onSpawn = func() {
		keeperClaimed = restoreKeptDevServer(context.Background(), &keepSSH{identity: "appdev-1/boot-b/900"}, dir, "appdev", "")
	}
	resp, result := callDevServerTool(t, srv, map[string]any{
		"action": "start", "hostname": "appdev", "command": "npm run dev", "port": 3000,
	})
	if result.IsError || !resp.Running {
		t.Fatalf("start: %s", getTextContent(t, result))
	}
	if keeperClaimed != nil {
		t.Errorf("the keeper claimed a life the agent's start took over: %+v", keeperClaimed)
	}
	if ssh.spawnCount() != 1 {
		t.Errorf("one spawn, the agent's; got %d", ssh.spawnCount())
	}
	rec, _ := workflow.KeptDevServerFor(dir, "appdev")
	if rec == nil || rec.Container != "appdev-1/boot-b/900" || rec.LastRestore != nil {
		t.Errorf("the agent's start owns the new life: %+v", rec)
	}
}

// TestDevServerTool_StartWaitsForARunningBringBack: the keeper claimed the
// container's new life and is starting the server; an agent start in that
// moment starts nothing beside it and says so.
func TestDevServerTool_StartWaitsForARunningBringBack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.KeepDevServer(dir, keptAppdev()); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	if _, claimed, _ := workflow.ClaimKeptDevServer(dir, "appdev", "appdev-1/boot-b/900", time.Now()); !claimed {
		t.Fatal("keeper claim")
	}
	ssh := &keepSSH{identity: "appdev-1/boot-b/900"}
	srv := devServerToolServer(t, dir, ssh, nil)

	resp, result := callDevServerTool(t, srv, map[string]any{
		"action": "start", "hostname": "appdev", "command": "npm run dev", "port": 3000,
	})
	if result.IsError {
		t.Fatalf("start: %s", getTextContent(t, result))
	}
	if ssh.spawnCount() != 0 {
		t.Fatalf("no second copy beside a running bring-back, spawns = %d", ssh.spawnCount())
	}
	if resp.Running || !strings.Contains(resp.Message, "bringing back") {
		t.Errorf("the answer says zcp is bringing it back: %+v", resp)
	}
	rec, _ := workflow.KeptDevServerFor(dir, "appdev")
	if rec == nil || rec.LastRestore == nil || rec.LastRestore.Reason != workflow.DevServerRestoring {
		t.Errorf("the keeper's claim stands: %+v", rec)
	}
}

// TestDevServerTool_UnreadableContainer_KeepsNothing: when the container life
// cannot be read, the start is not kept — and neither is an earlier start on
// the host, which would otherwise come back in its place after a restart.
func TestDevServerTool_UnreadableContainer_KeepsNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := workflow.KeepDevServer(dir, keptAppdev()); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	srv := devServerToolServer(t, dir, &keepSSH{identity: ""}, nil)
	resp, result := callDevServerTool(t, srv, map[string]any{
		"action": "start", "hostname": "appdev", "command": "npm run dev -- --inspect", "port": 3000,
	})
	if result.IsError || !resp.Running {
		t.Fatalf("start: %s", getTextContent(t, result))
	}
	if resp.Kept == nil || *resp.Kept || len(resp.Warnings) == 0 {
		t.Errorf("kept=false with a warning: %+v", resp)
	}
	if rec, _ := workflow.KeptDevServerFor(dir, "appdev"); rec != nil {
		t.Errorf("nothing stays kept on the host, got %+v", rec)
	}
}

// TestDevServerTool_KeeperUnavailable_StillKeptWithWarning: when the keeper
// cannot be registered the start is still kept — zcp's own deploys bring it
// back — and the response says what is lost.
func TestDevServerTool_KeeperUnavailable_StillKeptWithWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	units := &recordingUnits{err: errors.New("sudo: a password is required")}
	srv := devServerToolServer(t, dir, &keepSSH{identity: "appdev-1/boot-a/100"}, units)

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

// TestEnsureDevServerKeeper: a zcp start puts the keeper back whenever dev
// servers are kept — one unit per state directory — and never for a state
// directory a unit's command line cannot carry.
func TestEnsureDevServerKeeper(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	empty := t.TempDir()
	units := &recordingUnits{}
	if err := EnsureDevServerKeeper(ctx, units, empty); err != nil || units.callCount() != 0 {
		t.Fatalf("nothing kept, no keeper: err=%v calls=%v", err, units.calls)
	}

	dir := t.TempDir()
	if err := workflow.KeepDevServer(dir, keptAppdev()); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	if err := EnsureDevServerKeeper(ctx, units, dir); err != nil {
		t.Fatalf("EnsureDevServerKeeper: %v", err)
	}
	if units.callCount() != 1 || units.calls[0][0] != devServerKeeperUnit(dir) || !strings.HasPrefix(units.calls[0][0], "devservers-") {
		t.Fatalf("keeper unit for the state dir: %+v", units.calls)
	}
	if devServerKeeperUnit(dir) == devServerKeeperUnit(empty) {
		t.Error("each state directory has its own keeper unit")
	}

	odd := filepath.Join(t.TempDir(), "with space")
	if err := os.MkdirAll(odd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workflow.KeepDevServer(odd, keptAppdev()); err != nil {
		t.Fatalf("KeepDevServer: %v", err)
	}
	if err := EnsureDevServerKeeper(ctx, units, odd); err == nil {
		t.Error("a state directory with a space cannot go on the unit's command line")
	}
}

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
			ssh := &keepSSH{identity: "appdev-2/boot-c/77", probeFails: tt.probeFails}
			result := &ops.DeployResult{Status: statusDeployed, TargetService: "appdev", NextActions: "deploy's own"}

			before, _ := workflow.KeptDevServerFor(dir, "appdev")
			listener := bringBackKeptDevServer(context.Background(), ssh, dir, "appdev", before, result)
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

// TestRestoreKeptDevServerAfterDeploy_KeeperGotThereFirst: the keeper's pass
// can reach a freshly deployed container before the deploy does and claim its
// life. The deploy then reports the keeper's bring-back — or, while that is
// still starting, says so — never nothing, which would send the agent to start
// a second copy onto a taken port. An outcome left from an earlier cycle is
// never reported as this one's.
func TestRestoreKeptDevServerAfterDeploy_KeeperGotThereFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		keeperDone  bool
		wantRunning bool
		wantReason  string
	}{
		{name: "the keeper's bring-back is done", keeperDone: true, wantRunning: true},
		{name: "the keeper is still starting it", keeperDone: false, wantReason: reasonKeeperRestoring},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			// Kept with an outcome from an earlier cycle: up, back then.
			if err := workflow.KeepDevServer(dir, *keptAppdevRestored(time.Now().Add(-time.Hour), true, "")); err != nil {
				t.Fatalf("KeepDevServer: %v", err)
			}
			before, _ := workflow.KeptDevServerFor(dir, "appdev")

			// The keeper claims the deployed container's life first.
			if _, claimed, _ := workflow.ClaimKeptDevServer(dir, "appdev", "appdev-2/boot-c/77", time.Now()); !claimed {
				t.Fatal("keeper claim")
			}
			if tt.keeperDone {
				if err := workflow.RecordDevServerRestore(dir, "appdev", "appdev-2/boot-c/77", workflow.DevServerRestore{At: time.Now().UTC().Format(time.RFC3339), Running: true}); err != nil {
					t.Fatal(err)
				}
			}

			ssh := &keepSSH{identity: "appdev-2/boot-c/77"}
			got := restoreKeptDevServerAfterDeploy(context.Background(), ssh, dir, "appdev", before, time.Millisecond)
			if ssh.spawnCount() != 0 {
				t.Fatalf("the deploy must not start a second copy, got %d spawns", ssh.spawnCount())
			}
			if got == nil || got.Running != tt.wantRunning || got.Reason != tt.wantReason {
				t.Fatalf("deploy report = %+v, want running=%v reason=%q", got, tt.wantRunning, tt.wantReason)
			}
		})
	}
}

// TestKeptDevServerNextActions: the next step after a bring-back follows from
// how it went — a server that did not answer within the wait may still be
// compiling (or the keeper may still be starting it), which is not the same as
// one that fell over.
func TestKeptDevServerNextActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ds   ops.DevServerResult
		want string
	}{
		{name: "answers", ds: ops.DevServerResult{Running: true}, want: "zerops_verify"},
		{name: "still starting", ds: ops.DevServerResult{Reason: ops.ReasonHealthProbeTimeout}, want: "action=status"},
		{name: "the keeper is starting it", ds: ops.DevServerResult{Reason: reasonKeeperRestoring}, want: "action=status"},
		{name: "fell over", ds: ops.DevServerResult{Reason: "health_probe_connection_refused"}, want: "action=restart"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := keptDevServerNextActions("appdev", &tt.ds); !strings.Contains(got, tt.want) {
				t.Errorf("next actions %q, want it to contain %q", got, tt.want)
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
	ssh := &deployKeepSSH{keepSSH: keepSSH{identity: "appdev-2/boot-c/77"}}
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
	if rec, _ := workflow.KeptDevServerFor(stateDir, "appdev"); rec == nil || rec.Container != "appdev-2/boot-c/77" {
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
	case strings.Contains(command, "/proc/1/stat"), strings.Contains(command, "/proc/net/tcp"),
		strings.Contains(command, "curl"), strings.Contains(command, "kill -0"), strings.Contains(command, "tail -n"):
		return s.keepSSH.ExecSSH(ctx, hostname, command)
	}
	return []byte("ok"), nil
}
