package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/schema"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// poolMigrateSSH answers the dev service's census with output and records
// every command it is asked to run, so a test can prove the read side ran
// only the read-only census. The env step's secret read runs for real in a
// local shell whose environment is env — the dev container's session env —
// or, with envErr, fails with envOutput as the session's output. The image
// step's read of the commit answers image, or fails with imageErr.
type poolMigrateSSH struct {
	mu        sync.Mutex
	output    string
	err       error
	env       map[string]string
	envErr    error
	envOutput string
	image     string
	imageErr  error
	commands  []string
}

// poolMigrateEnvReadMarker starts the env step's secret read.
const poolMigrateEnvReadMarker = "'zcp-env 1'"

func (s *poolMigrateSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	s.commands = append(s.commands, hostname+": "+command)
	census, censusErr := s.output, s.err
	env, envErr, envOutput := s.env, s.envErr, s.envOutput
	image, imageErr := s.image, s.imageErr
	s.mu.Unlock()
	if strings.Contains(command, poolMigrateImageReadMarker) {
		if imageErr != nil {
			return nil, imageErr
		}
		return []byte(image), nil
	}
	if !strings.Contains(command, poolMigrateEnvReadMarker) {
		if censusErr != nil {
			return nil, censusErr
		}
		return []byte(census), nil
	}
	if envErr != nil {
		return []byte(envOutput), &platform.SSHExecError{Hostname: hostname, Output: envOutput, Err: envErr}
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
	}
	return out, nil
}

// ExecSSHStdin answers one command as ExecSSH does; the input is not read.
func (s *poolMigrateSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
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

const poolMigrateHead = "1111111111111111111111111111111111111111"

// poolMigrateCensusFor is what the census prints for a dev service whose main
// checkout at /var/www is on branch (detached for ""), tracking
// remote/remoteBranch when remote is set, with nothing unsaved; worktrees adds
// linked worktrees in `git worktree list --porcelain` form and extra adds
// census lines.
func poolMigrateCensusFor(branch, remote, remoteBranch, worktrees string, extra ...string) string {
	var b strings.Builder
	b.WriteString("zcp-census 1\n")
	porcelain := "worktree /var/www\nHEAD " + poolMigrateHead + "\n"
	if branch == "" {
		porcelain += "detached\n"
	} else {
		porcelain += "branch refs/heads/" + branch + "\n"
	}
	if worktrees != "" {
		porcelain += "\n" + worktrees
	}
	for line := range strings.SplitSeq(strings.TrimRight(porcelain, "\n"), "\n") {
		b.WriteString("W " + line + "\n")
	}
	head := "(detached)"
	if branch != "" {
		head = branch
		upstream := "\t"
		if remote != "" {
			upstream = remote + "\trefs/heads/" + remoteBranch
		}
		b.WriteString("B refs/heads/" + branch + "\t" + upstream + "\n")
		b.WriteString("U refs/heads/" + branch + "\t0\n")
	} else {
		b.WriteString("D " + poolMigrateHead + "\t0\n")
	}
	b.WriteString("RN 0\n")
	b.WriteString("T /var/www\nTS # branch.oid " + poolMigrateHead + "\nTS # branch.head " + head + "\n")
	if remote != "" {
		b.WriteString("TS # branch.upstream " + remote + "/" + remoteBranch + "\nTS # branch.ab +0 -0\n")
	}
	for _, line := range extra {
		b.WriteString(line + "\n")
	}
	b.WriteString("zcp-census-end\n")
	return b.String()
}

// poolMigrateCleanCensus is a dev service whose main checkout is on main,
// pushed, with nothing unsaved and no linked worktree.
func poolMigrateCleanCensus() string {
	return poolMigrateCensusFor("main", "origin", "main", "")
}

const poolMigrateCrewAda = "worktree /var/www/.crew/ada\nHEAD 2222222222222222222222222222222222222222\nbranch refs/heads/crew/ada\n"

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
	// env holds each service's service env rows, by service id.
	env       map[string][]platform.ServiceEnvVar
	ssh       *poolMigrateSSH
	rt        runtime.Info
	input     WorkflowInput
	hqAddress func() string
	// devsup is the zcp-devsup source the image step names a line from.
	devsup DevsupSource
	// stateDir and mock persist across the fixture's calls once built.
	stateDir string
	mock     *platform.Mock
}

func newPoolMigrateFixture() poolMigrateFixture {
	return poolMigrateFixture{
		meta: poolMigratePairMeta(),
		services: []platform.ServiceStack{
			poolMigrateRuntime("appdev", "svc-dev"),
			poolMigrateRuntime("appstage", "svc-stage"),
		},
		env: map[string][]platform.ServiceEnvVar{
			"svc-pool": poolMigratePoolOwnEnv(),
		},
		ssh:   &poolMigrateSSH{output: poolMigrateCleanCensus(), image: poolMigrateImageReadFor(poolMigrateReadyYAML(), poolMigrateReadyDeclaration)},
		rt:    runtime.Info{InContainer: true, ServiceName: "zcp"},
		input: WorkflowInput{Workflow: "pool-migrate", Service: "appdev"},
	}
}

// poolMigratePoolOwnEnv is what a freshly imported pool holds: its own
// orchestrator tokens (read masked) and its sshIsolation.
func poolMigratePoolOwnEnv() []platform.ServiceEnvVar {
	return []platform.ServiceEnvVar{
		{ID: "p-api", Key: "API_TOKEN", Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true},
		{ID: "p-admin", Key: "ADMIN_TOKEN", Content: platform.MaskedEnvValue, Type: platform.ServiceEnvUser, Sensitive: true},
		{ID: "p-ssh", Key: "sshIsolation", Content: "vpn service@zcp", Type: platform.ServiceEnvUser},
	}
}

// build seeds the state dir and the mock the fixture describes, once.
func (f *poolMigrateFixture) build(t *testing.T) {
	t.Helper()
	if f.stateDir == "" {
		f.stateDir = t.TempDir()
		if f.meta != nil {
			if err := workflow.WriteServiceMeta(f.stateDir, f.meta); err != nil {
				t.Fatalf("seed meta: %v", err)
			}
		}
	}
	if f.mock != nil {
		return
	}
	f.mock = platform.NewMock().WithServicesDirect(f.services).WithProjectProcesses(f.processes)
	if f.search != nil {
		f.mock.WithServices(f.search)
	} else {
		f.mock.WithServices(f.services)
	}
	for id, rows := range f.env {
		f.mock.WithServiceEnv(id, rows)
		f.mock.WithProcess(&platform.Process{ID: "proc-envset-" + id, Status: platform.ProcessStatusFinished})
		for _, row := range rows {
			f.mock.WithProcess(&platform.Process{ID: "proc-envdel-" + row.ID, Status: platform.ProcessStatusFinished})
		}
	}
}

func (f *poolMigrateFixture) deps() poolMigrateDeps {
	return poolMigrateDeps{projectID: "proj-1", client: f.mock, ssh: f.ssh, stateDir: f.stateDir, rt: f.rt, hqAddress: f.hqAddress, devsup: f.devsup}
}

// run is the read side's call; runEnv is action="pool-env".
func (f *poolMigrateFixture) run(t *testing.T) (map[string]any, *platform.Mock) {
	t.Helper()
	return f.call(t, poolMigrateRun)
}

func (f *poolMigrateFixture) runEnv(t *testing.T) (map[string]any, *platform.Mock) {
	t.Helper()
	return f.call(t, poolMigrateEnvRun)
}

func (f *poolMigrateFixture) call(t *testing.T, handler func(context.Context, poolMigrateDeps, WorkflowInput) (*mcp.CallToolResult, any, error)) (map[string]any, *platform.Mock) {
	t.Helper()
	f.build(t)
	mock := f.mock
	result, typed, err := handler(context.Background(), f.deps(), f.input)
	if err != nil {
		t.Fatalf("pool-migrate: %v", err)
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
	next, _ := out["next"].(map[string]any)
	args, _ := next["args"].(map[string]any)
	want := map[string]any{"workflow": "pool-migrate", "action": "pool-env", "service": "appdev", "pool": "apppool"}
	if next["tool"] != "zerops_workflow" || !reflect.DeepEqual(args, want) {
		t.Fatalf("next = %v, want zerops_workflow %v", next, want)
	}
	if _, has := out["nextUnavailable"]; has {
		t.Fatalf("the env step names its call: %v", out["nextUnavailable"])
	}
	if g, _ := out["guidance"].(string); !strings.Contains(g, "### Give a part's pool the dev service's env") {
		t.Errorf("guidance does not carry the env step's atom")
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
				f.ssh.output = poolMigrateCensusFor("main", "origin", "main", poolMigrateCrewAda+
					"\nworktree /var/www/.crew/bob\nHEAD 3333333333333333333333333333333333333333\ndetached\nprunable gitdir file points to non-existent location\n",
					"T /var/www/.crew/ada", "TS # branch.head crew/ada", "T /var/www/.crew/bob", "TM")
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
			name: "census could not finish",
			mutate: func(f *poolMigrateFixture) {
				f.ssh.output = "zcp-census 1\nfatal: index file corrupt\nzcp-census-error status /var/www\nzcp-census-end\n"
			},
			wantReason: "dev-unreadable",
			wantInMsg:  []string{"appdev", "index file corrupt"},
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
	"GetAppVersionUserData":     true,
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
			f.ssh.output = poolMigrateCensusFor("main", "origin", "main", poolMigrateCrewAda, "T /var/www/.crew/ada", "TS # branch.head crew/ada")
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
				t.Fatalf("ssh commands = %v, want exactly the census", cmds)
			}
			if want := "appdev: " + ops.PoolMigrateCensusCommand("/var/www"); cmds[0] != want {
				t.Fatalf("ssh command = %q, want the census %q", cmds[0], want)
			}
		})
	}
}

// TestPoolMigrate_ReadSide_InAMate_OldPairChangeRecord_ReadNotMoved: a pair
// meta an older zcp wrote carries its change on the pair (HQ.Change,
// HQ.Branch). Every other reader of the Mate's changes moves such a record
// into the ledger first; the read side writes nothing (PM-1), so it reads the
// record where it lies — the work's branch is still that change's — and
// neither the ledger nor the meta moves.
func TestPoolMigrate_ReadSide_InAMate_OldPairChangeRecord_ReadNotMoved(t *testing.T) {
	t.Parallel()
	f := newPoolMigrateFixture()
	f.rt.MateEnabled = true
	f.meta.GitPushState = ""
	f.meta.RemoteURL = ""
	f.meta.HQ = &workflow.HQRepoRef{AppID: "app-1", Repo: "appdev", Change: 7, Branch: "fix-login"}
	f.ssh.output = poolMigrateCensusFor("feat", "origin", "feat", "")
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, f.meta); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, stateDir)
	mock := platform.NewMock().WithServicesDirect(f.services).WithServices(f.services)
	result, _, err := handlePoolMigrate(context.Background(), "proj-1", mock, f.ssh, f.input, stateDir, f.rt)
	if err != nil {
		t.Fatal(err)
	}
	if after := snapshotTree(t, stateDir); !reflect.DeepEqual(before, after) {
		t.Fatalf("the read side changed the state dir:\nbefore %v\nafter  %v", before, after)
	}
	var out struct {
		WorkBranch *poolMigrateWorkBranch `json:"workBranch"`
	}
	if err := json.Unmarshal([]byte(getTextContent(t, result)), &out); err != nil {
		t.Fatal(err)
	}
	if out.WorkBranch == nil || out.WorkBranch.Branch != "fix-login" || out.WorkBranch.Source != poolMigrateWorkFromChange {
		t.Fatalf("workBranch = %+v, want fix-login, the pair record's change", out.WorkBranch)
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

// poolMigrateUnsavedCensus is a dev service holding one of each kind of
// unsaved work in its main checkout and repository.
func poolMigrateUnsavedCensus() string {
	return poolMigrateCensusFor("main", "origin", "main", "",
		"B refs/heads/feat\t\t",
		"U refs/heads/feat\t2",
		"S stash@{0}\t4444444444444444444444444444444444444444\tOn main: wip",
		"RN 1",
		"RS 5555555555555555555555555555555555555555\treset away",
		"X refs/t3/checkpoints/thread-1/turn/1",
		"TS 1 M. N... 100644 100644 100644 aaaa bbbb lib.txt",
		"TS 1 .M N... 100644 100644 100644 aaaa cccc app.txt",
		"TS ? notes/",
		"TS ? .env.local",
		"TE .env.local",
		"TI node_modules/",
	)
}

// TestPoolMigrate_Census_Rendered pins that every step of a candidate carries
// the census of the dev service's unsaved work, each item named, with the move
// step's guidance on how to move it.
func TestPoolMigrate_Census_Rendered(t *testing.T) {
	t.Parallel()
	for _, step := range []string{"provision", "env"} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			f := newPoolMigrateFixture()
			f.ssh.output = poolMigrateUnsavedCensus()
			if step == "env" {
				f.services = append(f.services, poolMigratePool("apppool", "svc-pool"))
			}
			out, _ := f.run(t)
			if out["step"] != step {
				t.Fatalf("step = %v, want %s; refusals %v", out["step"], step, out["refusals"])
			}
			census, ok := out["census"].(map[string]any)
			if !ok {
				t.Fatalf("no census in %v", out)
			}
			if census["clean"] != false {
				t.Errorf("census clean = %v, want false", census["clean"])
			}
			text, _ := json.Marshal(census)
			for _, want := range []string{
				`"path":"/var/www"`, `"branch":"main"`,
				`"staged":{"count":1,"paths":["lib.txt"]}`,
				`"modified":{"count":1,"paths":["app.txt"]}`,
				`"untracked":{"count":1,"paths":["notes/"]}`,
				`"envFiles":[".env.local"]`,
				`"ignored":{"count":1,"paths":["node_modules/"]}`,
				`"branches":[{"name":"feat","unpushed":2}]`,
				`"ref":"stash@{0}"`, `"message":"On main: wip"`,
				`"subject":"reset away"`,
				`"t3Refs":{"checkpoints":1}`,
			} {
				if !strings.Contains(string(text), want) {
					t.Errorf("census lacks %s:\n%s", want, text)
				}
			}
			if reflog, _ := census["reflogOnly"].(map[string]any); reflog["count"] != float64(1) {
				t.Errorf("reflogOnly = %v, want a count of 1", census["reflogOnly"])
			}
			guidance, _ := out["guidance"].(string)
			if !strings.Contains(guidance, "### Move a part's work") {
				t.Errorf("guidance does not carry the move step's atom:\n%s", guidance)
			}
		})
	}
}

func TestPoolMigrate_Census_CleanDevSaysSo(t *testing.T) {
	t.Parallel()
	f := newPoolMigrateFixture()
	out, _ := f.run(t)
	census, _ := out["census"].(map[string]any)
	if census["clean"] != true {
		t.Fatalf("census = %v, want clean", census)
	}
}

func TestPoolMigrate_WorkBranch_FromChangeCurrentOrUpstream(t *testing.T) {
	t.Parallel()
	inMate := func(f *poolMigrateFixture) {
		f.rt.MateEnabled = true
		f.meta.GitPushState = ""
		f.meta.RemoteURL = ""
		f.meta.HQ = &workflow.HQRepoRef{AppID: "app-1", Repo: "appdev"}
	}
	tests := []struct {
		name        string
		mutate      func(*poolMigrateFixture)
		wantBranch  string
		wantSource  string
		wantMissing []string
	}{
		{
			name: "in a Mate, the open change's branch",
			mutate: func(f *poolMigrateFixture) {
				inMate(f)
				f.meta.HQ.Change, f.meta.HQ.Branch = 7, "fix-login"
				f.ssh.output = poolMigrateCensusFor("feat", "origin", "feat", "")
			},
			wantBranch: "fix-login", wantSource: "change",
		},
		{
			name: "in a Mate without a change, the current branch of its own",
			mutate: func(f *poolMigrateFixture) {
				inMate(f)
				f.ssh.output = poolMigrateCensusFor("feat", "", "", "")
			},
			wantBranch: "feat", wantSource: "current",
		},
		{
			name: "in a Mate on main",
			mutate: func(f *poolMigrateFixture) {
				inMate(f)
			},
			wantMissing: []string{`"main"`, "git switch -c"},
		},
		{
			name: "in a Mate on the Mate's own branch",
			mutate: func(f *poolMigrateFixture) {
				inMate(f)
				f.ssh.output = poolMigrateCensusFor("mate/proj-1", "origin", "mate/proj-1", "")
			},
			wantMissing: []string{`"mate/proj-1"`, "git switch -c"},
		},
		{
			name: "outside a Mate, the current branch's upstream on origin",
			mutate: func(f *poolMigrateFixture) {
				f.ssh.output = poolMigrateCensusFor("work", "origin", "feature-x", "")
			},
			wantBranch: "feature-x", wantSource: "upstream",
		},
		{
			name: "outside a Mate, a branch with no upstream",
			mutate: func(f *poolMigrateFixture) {
				f.ssh.output = poolMigrateCensusFor("feat", "", "", "")
			},
			wantMissing: []string{`"feat"`, "git push -u origin feat"},
		},
		{
			name: "outside a Mate, an upstream on another remote",
			mutate: func(f *poolMigrateFixture) {
				f.ssh.output = poolMigrateCensusFor("feat", "fork", "feat", "")
			},
			wantMissing: []string{"fork/feat", "origin", "git push -u origin feat"},
		},
		{
			name: "a detached checkout",
			mutate: func(f *poolMigrateFixture) {
				f.ssh.output = poolMigrateCensusFor("", "", "", "")
			},
			wantMissing: []string{"no branch", "git switch -c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newPoolMigrateFixture()
			tt.mutate(&f)
			out, _ := f.run(t)
			if out["status"] != "step" {
				t.Fatalf("status = %v, want step; refusals %v", out["status"], out["refusals"])
			}
			wb, ok := out["workBranch"].(map[string]any)
			if !ok {
				t.Fatalf("no workBranch in %v", out)
			}
			if tt.wantBranch != "" {
				if wb["branch"] != tt.wantBranch || wb["source"] != tt.wantSource {
					t.Fatalf("workBranch = %v, want %s from %s", wb, tt.wantBranch, tt.wantSource)
				}
				if _, has := wb["missing"]; has {
					t.Errorf("a named work's branch carries missing: %v", wb)
				}
				return
			}
			if _, has := wb["branch"]; has {
				t.Fatalf("workBranch = %v, want none named", wb)
			}
			missing, _ := wb["missing"].(string)
			for _, want := range tt.wantMissing {
				if !strings.Contains(missing, want) {
					t.Errorf("missing %q does not name %q", missing, want)
				}
			}
		})
	}
}
