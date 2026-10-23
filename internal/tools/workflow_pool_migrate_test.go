package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolMigrateSSH answers the dev service's worktree list and records every
// command it is asked to run, so a test can prove the read side ran only
// read-only git.
type poolMigrateSSH struct {
	mu        sync.Mutex
	worktrees string
	err       error
	commands  []string
}

func (s *poolMigrateSSH) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, hostname+": "+command)
	if s.err != nil {
		return nil, s.err
	}
	return []byte(s.worktrees), nil
}

func (s *poolMigrateSSH) ExecSSHBackground(_ context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, "background "+hostname+": "+command)
	return nil, errors.New("pool-migrate never runs a background command")
}

func (s *poolMigrateSSH) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.commands)
}

const poolMigrateMainTreeOnly = "worktree /var/www\nHEAD 1111111111111111111111111111111111111111\nbranch refs/heads/main\n"

func poolMigrateRuntime(hostname, id string) platform.ServiceStack {
	return platform.ServiceStack{
		ID:   id,
		Name: hostname,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "nodejs@22",
			ServiceStackTypeCategoryName: "USER",
		},
		Status: "ACTIVE",
		CustomAutoscaling: &platform.CustomAutoscaling{
			HorizontalMinCount: 1, HorizontalMaxCount: 1,
			CPUMode: "SHARED", MinCPU: 1, MaxCPU: 4,
			MinRAM: 0.5, MaxRAM: 8, MinDisk: 1, MaxDisk: 20,
		},
	}
}

func poolMigratePool(hostname, id string) platform.ServiceStack {
	return platform.ServiceStack{
		ID:   id,
		Name: hostname,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeVersionName:  "swarm@1",
			ServiceStackTypeCategoryName: "USER",
		},
		Status: "ACTIVE",
	}
}

// poolMigratePairMeta is a single-part variant-A pair, wired to a git origin.
func poolMigratePairMeta() *workflow.ServiceMeta {
	return &workflow.ServiceMeta{
		Hostname:       "appdev",
		Mode:           topology.PlanModeStandard,
		Stages:         []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		GitPushState:   topology.GitPushConfigured,
		RemoteURL:      "https://github.com/example/app.git",
		BootstrappedAt: "2026-10-01T00:00:00Z",
	}
}

type poolMigrateFixture struct {
	meta     *workflow.ServiceMeta
	services []platform.ServiceStack
	// search, when set, is what the lagging ES search returns.
	search    []platform.ServiceStack
	processes []platform.Process
	ssh       *poolMigrateSSH
	rt        runtime.Info
	input     WorkflowInput
}

func newPoolMigrateFixture() poolMigrateFixture {
	return poolMigrateFixture{
		meta: poolMigratePairMeta(),
		services: []platform.ServiceStack{
			poolMigrateRuntime("appdev", "svc-dev"),
			poolMigrateRuntime("appstage", "svc-stage"),
		},
		ssh:   &poolMigrateSSH{worktrees: poolMigrateMainTreeOnly},
		rt:    runtime.Info{InContainer: true, ServiceName: "zcp"},
		input: WorkflowInput{Workflow: "pool-migrate", Service: "appdev"},
	}
}

func (f poolMigrateFixture) run(t *testing.T) (map[string]any, *platform.Mock) {
	t.Helper()
	stateDir := t.TempDir()
	if f.meta != nil {
		if err := workflow.WriteServiceMeta(stateDir, f.meta); err != nil {
			t.Fatalf("seed meta: %v", err)
		}
	}
	mock := platform.NewMock().WithServicesDirect(f.services).WithProjectProcesses(f.processes)
	if f.search != nil {
		mock.WithServices(f.search)
	} else {
		mock.WithServices(f.services)
	}
	result, typed, err := handlePoolMigrate(context.Background(), "proj-1", mock, f.ssh, f.input, stateDir, f.rt)
	if err != nil {
		t.Fatalf("handlePoolMigrate: %v", err)
	}
	if typed != nil {
		t.Fatalf("typed output must stay nil, got %T", typed)
	}
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("unexpected error result: %s", text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return out, mock
}

func poolMigrateNextContent(t *testing.T, out map[string]any) string {
	t.Helper()
	next, ok := out["next"].(map[string]any)
	if !ok {
		t.Fatalf("no next call in %v", out)
	}
	if next["tool"] != "zerops_import" {
		t.Fatalf("next tool = %v, want zerops_import", next["tool"])
	}
	args, _ := next["args"].(map[string]any)
	content, _ := args["content"].(string)
	if content == "" {
		t.Fatalf("next call carries no import content: %v", next)
	}
	return content
}

func poolMigrateReasons(out map[string]any) []string {
	var reasons []string
	refusals, _ := out["refusals"].([]any)
	for _, r := range refusals {
		if m, ok := r.(map[string]any); ok {
			reasons = append(reasons, m["reason"].(string))
		}
	}
	return reasons
}

func poolMigrateRefusalMessage(out map[string]any, reason string) string {
	refusals, _ := out["refusals"].([]any)
	for _, r := range refusals {
		if m, ok := r.(map[string]any); ok && m["reason"] == reason {
			msg, _ := m["message"].(string)
			return msg
		}
	}
	return ""
}

func TestPoolMigrate_StepFromLiveState_NoPool_Provision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		mutate      func(*poolMigrateFixture)
		wantService string
		wantPool    string
		wantStages  []any
	}{
		{
			name:        "pair",
			wantService: "appdev",
			wantPool:    "apppool",
			wantStages:  []any{"appstage"},
		},
		{
			name: "dev-only service",
			mutate: func(f *poolMigrateFixture) {
				f.meta.Mode = topology.PlanModeDev
				f.meta.Stages = nil
				f.services = f.services[:1]
			},
			wantService: "appdev",
			wantPool:    "apppool",
		},
		{
			name:        "stage hostname names the pair's dev",
			mutate:      func(f *poolMigrateFixture) { f.input.Service = "appstage" },
			wantService: "appdev",
			wantPool:    "apppool",
			wantStages:  []any{"appstage"},
		},
		{
			name:        "pool hostname given",
			mutate:      func(f *poolMigrateFixture) { f.input.Pool = "webpool" },
			wantService: "appdev",
			wantPool:    "webpool",
			wantStages:  []any{"appstage"},
		},
		{
			name: "dev hostname without a dev suffix",
			mutate: func(f *poolMigrateFixture) {
				f.meta.Hostname = "web"
				f.meta.Mode = topology.PlanModeDev
				f.meta.Stages = nil
				f.services = []platform.ServiceStack{poolMigrateRuntime("web", "svc-web")}
				f.input.Service = "web"
			},
			wantService: "web",
			wantPool:    "webpool",
		},
		{
			name: "in a Mate, an HQ-wired pair",
			mutate: func(f *poolMigrateFixture) {
				f.rt.MateEnabled = true
				f.meta.GitPushState = ""
				f.meta.RemoteURL = ""
				f.meta.HQ = &workflow.HQRepoRef{AppID: "app-1", Repo: "appdev"}
			},
			wantService: "appdev",
			wantPool:    "apppool",
			wantStages:  []any{"appstage"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPoolMigrateFixture()
			if tt.mutate != nil {
				tt.mutate(&f)
			}
			out, _ := f.run(t)

			if out["workflow"] != "pool-migrate" || out["phase"] != "pool-migrate-active" {
				t.Fatalf("workflow/phase = %v/%v", out["workflow"], out["phase"])
			}
			if out["status"] != "step" || out["step"] != "provision" {
				t.Fatalf("status/step = %v/%v, want step/provision; refusals %v", out["status"], out["step"], out["refusals"])
			}
			if out["service"] != tt.wantService || out["pool"] != tt.wantPool {
				t.Fatalf("service/pool = %v/%v, want %s/%s", out["service"], out["pool"], tt.wantService, tt.wantPool)
			}
			if got, _ := out["stages"].([]any); !reflect.DeepEqual(got, tt.wantStages) {
				t.Fatalf("stages = %v, want %v", got, tt.wantStages)
			}
			if g, _ := out["guidance"].(string); !strings.Contains(g, "pool") {
				t.Fatalf("guidance does not carry the pool-migrate atom: %q", g)
			}

			content := poolMigrateNextContent(t, out)
			for _, want := range []string{
				"hostname: " + tt.wantPool,
				"type: swarm@1",
				"minContainers: 0",
				"maxContainers: 3",
				"verticalAutoscaling:",
				"minRam: 0.5",
				"maxRam: 8",
				"sshIsolation: vpn service@zcp",
			} {
				if !strings.Contains(content, want) {
					t.Errorf("stanza lacks %q:\n%s", want, content)
				}
			}
			// A service-level envVariables block is deprecated, so the pool's
			// own env rides its vault; a credential never rides the stanza.
			for _, never := range []string{"envVariables", "envSecrets", "GIT_TOKEN"} {
				if strings.Contains(content, never) {
					t.Errorf("stanza carries %q:\n%s", never, content)
				}
			}
			if errs := schema.ValidateImportYAML(content); len(errs) > 0 {
				t.Fatalf("stanza fails the import schema: %v\n%s", errs, content)
			}

			steps, _ := out["steps"].([]any)
			wantSteps := []string{"provision", "env", "image", "move", "lease", "ready-to-retire", "retiring", "retired"}
			if len(steps) != len(wantSteps) {
				t.Fatalf("steps = %v, want the eight named steps", steps)
			}
			for i, s := range steps {
				m := s.(map[string]any)
				wantState := "pending"
				if i == 0 {
					wantState = "current"
				}
				if m["step"] != wantSteps[i] || m["state"] != wantState {
					t.Errorf("steps[%d] = %v, want %s/%s", i, m, wantSteps[i], wantState)
				}
			}
		})
	}
}

func TestPoolMigrate_StepFromLiveState_PoolPresent_Env(t *testing.T) {
	t.Parallel()
	f := newPoolMigrateFixture()
	f.services = append(f.services, poolMigratePool("apppool", "svc-pool"))
	out, _ := f.run(t)

	if out["status"] != "step" || out["step"] != "env" {
		t.Fatalf("status/step = %v/%v, want step/env", out["status"], out["step"])
	}
	if _, has := out["next"]; has {
		t.Fatalf("the env step is not run by this zcp, so no next call is named: %v", out["next"])
	}
	if s, _ := out["nextUnavailable"].(string); s == "" {
		t.Fatal("the env step must say why no next call is named")
	}
	steps, _ := out["steps"].([]any)
	if first := steps[0].(map[string]any); first["state"] != "done" {
		t.Fatalf("provision = %v, want done", first)
	}
	if second := steps[1].(map[string]any); second["state"] != "current" {
		t.Fatalf("env = %v, want current", second)
	}
}

func TestPoolMigrate_StepFromLiveState_PoolImportInFlight_Provision(t *testing.T) {
	t.Parallel()
	f := newPoolMigrateFixture()
	f.services = append(f.services, poolMigratePool("apppool", "svc-pool"))
	f.processes = []platform.Process{{
		ID: "proc-import", ActionName: "stack.create", Status: "RUNNING",
		ServiceStacks: []platform.ServiceStackRef{{ID: "svc-pool", Name: "apppool"}},
		Created:       "2026-10-23T10:00:00Z",
	}}
	out, _ := f.run(t)

	if out["step"] != "provision" {
		t.Fatalf("step = %v, want provision while the pool's import runs", out["step"])
	}
	next, _ := out["next"].(map[string]any)
	args, _ := next["args"].(map[string]any)
	if next["tool"] != "zerops_process" || args["action"] != "wait" || args["service"] != "apppool" {
		t.Fatalf("next = %v, want zerops_process action=wait service=apppool", next)
	}
}

// TestPoolMigrate_StepFromLiveState_DecidesOnDirectReads pins that the step is
// read from the direct service list, never the lagging search: a pool just
// imported is absent from the search for seconds.
func TestPoolMigrate_StepFromLiveState_DecidesOnDirectReads(t *testing.T) {
	t.Parallel()
	f := newPoolMigrateFixture()
	f.search = slices.Clone(f.services) // the search has not indexed the pool yet
	f.services = append(f.services, poolMigratePool("apppool", "svc-pool"))
	out, mock := f.run(t)

	if out["step"] != "env" {
		t.Fatalf("step = %v, want env: the direct read lists the pool", out["step"])
	}
	if n := mock.CallCount("ListServices"); n != 0 {
		t.Fatalf("the lagging search was read %d times", n)
	}
}

func TestPoolMigrate_NotACandidate_Refused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		mutate     func(*poolMigrateFixture)
		wantReason string
		wantInMsg  []string
	}{
		{
			name:       "simple mode",
			mutate:     func(f *poolMigrateFixture) { f.meta.Mode = topology.PlanModeSimple; f.meta.Stages = nil },
			wantReason: "simple-mode",
			wantInMsg:  []string{"appdev", "simple"},
		},
		{
			name: "local mode",
			mutate: func(f *poolMigrateFixture) {
				f.meta.Mode = topology.PlanModeLocalStage
				f.meta.Hostname = "appstage"
				f.meta.Stages = nil
				f.input.Service = "appstage"
			},
			wantReason: "local-mode",
			wantInMsg:  []string{"appstage"},
		},
		{
			name: "a pool",
			mutate: func(f *poolMigrateFixture) {
				f.services = append(f.services, poolMigratePool("devpool", "svc-devpool"))
				f.input.Service = "devpool"
			},
			wantReason: "pool-or-box",
			wantInMsg:  []string{"devpool"},
		},
		{
			name: "a Box",
			mutate: func(f *poolMigrateFixture) {
				box := poolMigratePool("sandbox", "svc-box")
				box.ServiceStackTypeInfo.ServiceStackTypeVersionName = "ubuntu/zbox@1"
				f.services = append(f.services, box)
				f.input.Service = "sandbox"
			},
			wantReason: "pool-or-box",
			wantInMsg:  []string{"sandbox"},
		},
		{
			name:       "no origin outside a Mate",
			mutate:     func(f *poolMigrateFixture) { f.meta.GitPushState = topology.GitPushUnconfigured; f.meta.RemoteURL = "" },
			wantReason: "no-origin",
			wantInMsg:  []string{"appdev", "git-push-setup"},
		},
		{
			name:       "an origin leases cannot clone",
			mutate:     func(f *poolMigrateFixture) { f.meta.RemoteURL = "git@github.com:example/app.git" },
			wantReason: "no-origin",
			wantInMsg:  []string{"https"},
		},
		{
			name:       "in a Mate, a pair not wired to HQ",
			mutate:     func(f *poolMigrateFixture) { f.rt.MateEnabled = true },
			wantReason: "no-origin",
			wantInMsg:  []string{"appdev", "HQ"},
		},
		{
			name:       "not in a container",
			mutate:     func(f *poolMigrateFixture) { f.rt = runtime.Info{} },
			wantReason: "not-in-container",
		},
		{
			name: "crew worktrees",
			mutate: func(f *poolMigrateFixture) {
				f.ssh.worktrees = poolMigrateMainTreeOnly +
					"\nworktree /var/www/.crew/ada\nHEAD 2222222222222222222222222222222222222222\nbranch refs/heads/crew/ada\n" +
					"\nworktree /var/www/.crew/bob\nHEAD 3333333333333333333333333333333333333333\ndetached\nprunable gitdir file points to non-existent location\n"
			},
			wantReason: "crew-worktrees",
			wantInMsg:  []string{"appdev", "ada", "bob"},
		},
		{
			name: "dev service unreadable over SSH",
			mutate: func(f *poolMigrateFixture) {
				f.ssh.err = &platform.SSHExecError{Hostname: "appdev", Output: "fatal: not a git repository", Err: errors.New("exit status 128")}
			},
			wantReason: "dev-unreadable",
			wantInMsg:  []string{"appdev", "not a git repository"},
		},
		{
			name:       "not adopted",
			mutate:     func(f *poolMigrateFixture) { f.meta = nil },
			wantReason: "not-adopted",
			wantInMsg:  []string{"appdev"},
		},
		{
			name:       "bootstrap not finished",
			mutate:     func(f *poolMigrateFixture) { f.meta.BootstrappedAt = "" },
			wantReason: "not-adopted",
			wantInMsg:  []string{"appdev", "bootstrap"},
		},
		{
			name: "variant B",
			mutate: func(f *poolMigrateFixture) {
				f.meta.CodeVolume = &topology.CodeVolume{Hostname: "code", MountPath: "/var/www/code"}
			},
			wantReason: "variant-b",
			wantInMsg:  []string{"appdev"},
		},
		{
			name: "several parts",
			mutate: func(f *poolMigrateFixture) {
				f.meta.Stages = append(f.meta.Stages, topology.StageRef{Hostname: "workerstage", SetupName: "worker"})
				f.services = append(f.services, poolMigrateRuntime("workerstage", "svc-worker"))
			},
			wantReason: "multi-part",
			wantInMsg:  []string{"appstage", "workerstage"},
		},
		{
			name:       "dev service gone",
			mutate:     func(f *poolMigrateFixture) { f.services = f.services[1:] },
			wantReason: "dev-not-live",
			wantInMsg:  []string{"appdev"},
		},
		{
			name: "pool hostname taken by another service",
			mutate: func(f *poolMigrateFixture) {
				f.services = append(f.services, poolMigrateRuntime("apppool", "svc-other"))
			},
			wantReason: "pool-hostname-taken",
			wantInMsg:  []string{"apppool", "pool="},
		},
		{
			name:       "pool hostname not a hostname",
			mutate:     func(f *poolMigrateFixture) { f.input.Pool = "App-Pool" },
			wantReason: "pool-hostname-invalid",
			wantInMsg:  []string{"App-Pool"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPoolMigrateFixture()
			tt.mutate(&f)
			out, _ := f.run(t)

			if out["status"] != "not-a-candidate" {
				t.Fatalf("status = %v, want not-a-candidate; out %v", out["status"], out)
			}
			if reasons := poolMigrateReasons(out); !slices.Contains(reasons, tt.wantReason) {
				t.Fatalf("reasons = %v, want %s", reasons, tt.wantReason)
			}
			msg := poolMigrateRefusalMessage(out, tt.wantReason)
			for _, want := range tt.wantInMsg {
				if !strings.Contains(msg, want) {
					t.Errorf("refusal %s message %q does not name %q", tt.wantReason, msg, want)
				}
			}
			if _, has := out["next"]; has {
				t.Errorf("a refusal names no next step: %v", out["next"])
			}
			if _, has := out["step"]; has {
				t.Errorf("a refusal names no step: %v", out["step"])
			}
		})
	}
}

// TestPoolMigrate_NotACandidate_SimpleModeSkipsSSH pins that a service the
// meta already rules out is never reached over SSH.
func TestPoolMigrate_NotACandidate_SimpleModeSkipsSSH(t *testing.T) {
	t.Parallel()
	f := newPoolMigrateFixture()
	f.meta.Mode = topology.PlanModeSimple
	f.meta.Stages = nil
	f.run(t)
	if cmds := f.ssh.recorded(); len(cmds) != 0 {
		t.Fatalf("a simple service was reached over SSH: %v", cmds)
	}
}

func TestPoolMigrate_MissingService_NamesCandidates(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, poolMigratePairMeta()); err != nil {
		t.Fatal(err)
	}
	mock := platform.NewMock()
	result, _, err := handlePoolMigrate(context.Background(), "proj-1", mock, &poolMigrateSSH{}, WorkflowInput{Workflow: "pool-migrate"}, stateDir, runtime.Info{InContainer: true, ServiceName: "zcp"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("a call without service must be an error naming the input")
	}
	text := getTextContent(t, result)
	for _, want := range []string{"service", "appdev"} {
		if !strings.Contains(text, want) {
			t.Errorf("error %q does not name %q", text, want)
		}
	}
}

// poolMigrateReadOnlyCalls are the platform reads the read side may make.
var poolMigrateReadOnlyCalls = map[string]bool{
	"ListServicesDirect":        true,
	"GetProjectProcessesDirect": true,
	"GetService":                true,
}

func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		entry := info.Mode().String()
		if !d.IsDir() {
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry += " " + string(b)
		}
		out[rel] = entry
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return out
}

func TestPoolMigrate_ReadSideWritesNothing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*poolMigrateFixture)
	}{
		{name: "provision"},
		{name: "env", mutate: func(f *poolMigrateFixture) {
			f.services = append(f.services, poolMigratePool("apppool", "svc-pool"))
		}},
		{name: "crew refusal", mutate: func(f *poolMigrateFixture) {
			f.ssh.worktrees = poolMigrateMainTreeOnly + "\nworktree /var/www/.crew/ada\nHEAD 2222222222222222222222222222222222222222\nbranch refs/heads/crew/ada\n"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPoolMigrateFixture()
			if tt.mutate != nil {
				tt.mutate(&f)
			}
			stateDir := t.TempDir()
			if err := workflow.WriteServiceMeta(stateDir, f.meta); err != nil {
				t.Fatal(err)
			}
			before := snapshotTree(t, stateDir)
			mock := platform.NewMock().WithServicesDirect(f.services).WithServices(f.services)
			if _, _, err := handlePoolMigrate(context.Background(), "proj-1", mock, f.ssh, f.input, stateDir, f.rt); err != nil {
				t.Fatal(err)
			}
			if after := snapshotTree(t, stateDir); !reflect.DeepEqual(before, after) {
				t.Fatalf("the read side changed the state dir:\nbefore %v\nafter  %v", before, after)
			}
			// Every call has returned, so CallCounts is safe to range over.
			for method, n := range mock.CallCounts {
				if n > 0 && !poolMigrateReadOnlyCalls[method] {
					t.Errorf("the read side called %s %d times", method, n)
				}
			}
			cmds := f.ssh.recorded()
			if len(cmds) != 1 {
				t.Fatalf("ssh commands = %v, want exactly the worktree read", cmds)
			}
			if want := "appdev: git -C '/var/www' worktree list --porcelain"; cmds[0] != want {
				t.Fatalf("ssh command = %q, want %q", cmds[0], want)
			}
		})
	}
}

// TestPoolMigrate_RepeatCall_SameStep pins the recovery contract: the flow
// keeps no state, so a call repeated after compaction lands on the same step
// with the same stanza.
func TestPoolMigrate_RepeatCall_SameStep(t *testing.T) {
	t.Parallel()
	f := newPoolMigrateFixture()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, f.meta); err != nil {
		t.Fatal(err)
	}
	mock := platform.NewMock().WithServicesDirect(f.services).WithServices(f.services)
	call := func() string {
		result, _, err := handlePoolMigrate(context.Background(), "proj-1", mock, f.ssh, f.input, stateDir, f.rt)
		if err != nil {
			t.Fatal(err)
		}
		return getTextContent(t, result)
	}
	if first, second := call(), call(); first != second {
		t.Fatalf("a repeated call answered differently:\n%s\n%s", first, second)
	}
}

// TestPoolMigrate_Dispatch_ReachesTheFlow pins both call shapes an agent uses:
// workflow="pool-migrate" alone, and with action="start".
func TestPoolMigrate_Dispatch_ReachesTheFlow(t *testing.T) {
	t.Parallel()
	for _, args := range []map[string]any{
		{"workflow": "pool-migrate", "service": "appdev"},
		{"action": "start", "workflow": "pool-migrate", "service": "appdev"},
	} {
		f := newPoolMigrateFixture()
		stateDir := t.TempDir()
		if err := workflow.WriteServiceMeta(stateDir, f.meta); err != nil {
			t.Fatal(err)
		}
		mock := platform.NewMock().WithServicesDirect(f.services).WithServices(f.services)
		engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
		srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
		RegisterWorkflow(srv, mock, nil, "proj-1", nil, engine, nil, stateDir, "zcp", nil, f.ssh, f.rt, "")

		result := callTool(t, srv, "zerops_workflow", args)
		text := getTextContent(t, result)
		if result.IsError {
			t.Fatalf("args %v: error result %s", args, text)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("args %v: %v\n%s", args, err, text)
		}
		if out["workflow"] != "pool-migrate" || out["step"] != "provision" {
			t.Fatalf("args %v: workflow/step = %v/%v", args, out["workflow"], out["step"])
		}
	}
}
