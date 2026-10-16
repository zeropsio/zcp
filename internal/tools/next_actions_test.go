// Tests for: next_actions.go — NextActions constants and functions.
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/topology"
)

// TestDeployFailedResponseFields_NoBuildLogsContradiction pins H3a: when a
// build fails before any logs were captured (sub-10s exits), the deploy
// response carries `suggestion`, `nextActions`, and `failureClassification.
// suggestedAction` — all three reach the agent. None of them may tell the
// agent to read buildLogs (they don't exist), or the response is
// self-contradicting.
//
// Surfaced by greenfield-fullstack-multi-runtime in eval suite
// 20260505-151844: suggestion correctly said "build logs unavailable",
// nextActions said "check buildLogs in response for build output",
// classifier baseline.SuggestedAction said "Read buildLogs for the exact
// stderr". Three fields, two contradictions.
func TestDeployFailedResponseFields_NoBuildLogsContradiction(t *testing.T) {
	t.Parallel()

	// P7: suggestion + nextActions are now SOURCED from the classifier owner
	// (deploy_poll), so all three fields carry the same string and the
	// no-contradiction guarantee lives at the owner. Assert the classifier's
	// no-logs build/prepare baselines never point the agent at buildLogs
	// without acknowledging their absence.
	tests := []struct {
		name  string
		phase ops.DeployFailurePhase
	}{
		{"build-failed", ops.PhaseBuild},
		{"prepare-failed", ops.PhasePrepare},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cls := ops.ClassifyDeployFailure(ops.FailureInput{Phase: tt.phase}) // no logs supplied
			if cls == nil {
				t.Fatalf("%s: classifier returned nil for a known phase", tt.name)
			}
			body := strings.ToLower(cls.SuggestedAction)
			if strings.Contains(body, "buildlogs") || strings.Contains(body, "build logs") {
				if !strings.Contains(body, "unavailable") && !strings.Contains(body, "not captured") &&
					!strings.Contains(body, "no logs") && !strings.Contains(body, "before producing logs") {
					t.Errorf("%s: classifier SuggestedAction references buildLogs without acknowledging absence: %q",
						tt.name, cls.SuggestedAction)
				}
			}
		})
	}
}

func TestNextActions_ContainToolNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		action   string
		wantTool string
	}{
		{"deploy_success_verify", nextActionDeploySuccess, "zerops_verify"},
		{"import_success_discover", nextActionImportSuccess, "zerops_discover"},
		{"import_success_workflow", nextActionImportSuccess, "workflow"},
		{"import_partial_events", nextActionImportPartial, "zerops_events"},
		{"import_partial_workflow", nextActionImportPartial, "zerops_workflow"},
		{"manage_start_discover", nextActionManageStart, "zerops_discover"},
		{"manage_stop_manage", nextActionManageStop, "zerops_manage"},
		{"manage_restart_logs", nextActionManageRestart, "zerops_logs"},
		{"manage_reload_logs", nextActionManageReload, "zerops_logs"},
		{"manage_connect_discover", nextActionManageConnect, "zerops_discover"},
		{"manage_disconnect_discover", nextActionManageDisconnect, "zerops_discover"},
		{"scale_discover", nextActionScaleSuccess, "zerops_discover"},
		{"subdomain_enable_verify", nextActionSubdomainEnable, "zerops_verify"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(tt.action, tt.wantTool) {
				t.Errorf("nextAction %q should contain tool name %q", tt.action, tt.wantTool)
			}
		})
	}
}

// TestDeploySuccessNextActions pins the post-DS-01 navigation contract:
// the next-action pointer branches on `topology.IsDeferredStart` so
// dev-mode dynamic deploys point at `zerops_dev_server action=start`
// (idle runtime, dev process not yet spawned) while every other shape
// points at `zerops_verify` (runtime is or will be auto-running).
//
// Pre-2026-05-06 the function returned a single unified pointer at
// `zerops_logs severity=ERROR` for every shape — agents on dev-mode
// dynamic deploys read "check logs" and called verify next, hit a
// 502 from the idle container, and started debugging the wrong
// layer. The branch is navigation, not state assertion: no claim
// about whether the dev server IS running, only about which tool to
// call next.
//
// DS-01 forbid list still applies — no SSH-shell embedding, no
// liveness claims, no runtime-version-specific quirks. Branching on
// `IsDeferredStart` is the canonical post-DS-01 predicate.
func TestDeploySuccessNextActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		result         *ops.DeployResult
		mode           topology.Mode
		class          topology.RuntimeClass
		wantContains   string
		wantNotContain []string
	}{
		{
			name:           "dev_mode_dynamic_points_at_dev_server",
			result:         &ops.DeployResult{SourceService: "appdev", TargetService: "appdev", TargetServiceType: "nodejs@22"},
			mode:           topology.ModeDev,
			class:          topology.RuntimeDynamic,
			wantContains:   "zerops_dev_server action=start",
			wantNotContain: []string{"NOT running", "idle start", "Built-in webserver", "ssh ", "SSH session"},
		},
		{
			name:         "standard_mode_dynamic_points_at_dev_server",
			result:       &ops.DeployResult{SourceService: "appdev", TargetService: "appdev", TargetServiceType: "nodejs@22"},
			mode:         topology.ModeStandard,
			class:        topology.RuntimeDynamic,
			wantContains: "zerops_dev_server action=start",
		},
		{
			name:         "stage_mode_dynamic_points_at_verify",
			result:       &ops.DeployResult{SourceService: "appdev", TargetService: "appstage", TargetServiceType: "nodejs@22"},
			mode:         topology.ModeStage,
			class:        topology.RuntimeDynamic,
			wantContains: nextActionDeploySuccess,
		},
		{
			name:         "simple_mode_dynamic_points_at_verify",
			result:       &ops.DeployResult{TargetService: "worker", TargetServiceType: "go@1"},
			mode:         topology.ModeSimple,
			class:        topology.RuntimeDynamic,
			wantContains: nextActionDeploySuccess,
		},
		{
			name:         "implicit_webserver_points_at_verify",
			result:       &ops.DeployResult{SourceService: "appdev", TargetService: "appdev", TargetServiceType: "php-nginx@8.4"},
			mode:         topology.ModeDev,
			class:        topology.RuntimeImplicitWeb,
			wantContains: nextActionDeploySuccess,
		},
		{
			name:         "static_runtime_points_at_verify",
			result:       &ops.DeployResult{SourceService: "webdev", TargetService: "webdev", TargetServiceType: "static"},
			mode:         topology.ModeDev,
			class:        topology.RuntimeStatic,
			wantContains: nextActionDeploySuccess,
		},
		{
			name:         "no_meta_falls_back_to_verify",
			result:       &ops.DeployResult{TargetService: "appdev", TargetServiceType: "nodejs@22"},
			mode:         "",
			class:        "",
			wantContains: nextActionDeploySuccess,
		},
	}

	dsForbid := []string{"NOT running", "idle start", "auto-start", "Built-in webserver", "ssh ", "SSH session"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := deploySuccessNextActions(tt.result, tt.mode, tt.class)
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("deploySuccessNextActions(%s, %s) = %q, want contains %q",
					tt.mode, tt.class, got, tt.wantContains)
			}
			for _, phrase := range dsForbid {
				if strings.Contains(got, phrase) {
					t.Errorf("deploySuccessNextActions must not assert runtime state or embed SSH; contained %q in output: %s",
						phrase, got)
				}
			}
			for _, phrase := range tt.wantNotContain {
				if strings.Contains(got, phrase) {
					t.Errorf("case-specific forbidden phrase %q surfaced in: %s", phrase, got)
				}
			}
		})
	}
}

// TestDeployNextActions_NamesTheDevServersTheSupervisorStarted: a dev setup
// under the supervisor starts its declared servers itself, so a deploy's reply
// names them and never calls the container idle.
func TestDeployNextActions_NamesTheDevServersTheSupervisorStarted(t *testing.T) {
	t.Parallel()
	decl := `{"version":1,"servers":[{"name":"server","port":3000,"command":"a","cwd":"."},{"name":"web","port":5173,"command":"b","cwd":"."}]}`
	c := devservertest.NewSupervised(decl)
	got := deployNextActionsFor(t.Context(), c, "/root", &ops.DeployResult{TargetService: "matedev"}, topology.ModeDev, topology.RuntimeDynamic)
	for _, want := range []string{"server", "web", "zerops_verify"} {
		if !strings.Contains(got, want) {
			t.Errorf("nextActions = %q, want it to name %q", got, want)
		}
	}
	if strings.Contains(got, "idle") || strings.Contains(got, "action=start") {
		t.Errorf("nextActions = %q, must not call a supervised container idle", got)
	}

	idle := deployNextActionsFor(t.Context(), &devservertest.Container{}, "/root", &ops.DeployResult{TargetService: "matedev"}, topology.ModeDev, topology.RuntimeDynamic)
	if !strings.Contains(idle, "idle") {
		t.Errorf("a container without the supervisor keeps today's pointer, got %q", idle)
	}
}

// A deploy's reply names every declared dev server with its status — a server
// that crashed after the container was replaced too, with its exit and how
// to get it back — and, when the deploy did not carry git-ignored paths, says
// to reinstall them. It never calls a container with declared servers idle.
func TestDeployNextActions_NamesEveryDeclaredDevServerACrashedOneWithItsExit(t *testing.T) {
	t.Parallel()
	decl := `{"version":1,"servers":[{"name":"web","port":3000,"command":"a","cwd":"."},{"name":"lingui","port":3001,"command":"b","cwd":"."}]}`
	c := devservertest.NewSupervised(decl)
	c.Crash = map[string]bool{"lingui": true}
	c.Redeploy()
	result := &ops.DeployResult{TargetService: "appdev", NotCarried: &git.NotCarried{Count: 1, Sample: []string{"node_modules"}}}

	got := deployNextActionsFor(t.Context(), c, "/var/www", result, topology.ModeDev, topology.RuntimeDynamic)

	for _, want := range []string{"web running", "lingui crashed (exit 1)", "node_modules", "action=logs", `name="lingui"`, "zerops_verify"} {
		if !strings.Contains(got, want) {
			t.Errorf("nextActions = %q, want it to contain %q", got, want)
		}
	}

	c.Crash = map[string]bool{"web": true, "lingui": true}
	c.Redeploy()
	allDown := deployNextActionsFor(t.Context(), c, "/var/www", &ops.DeployResult{TargetService: "appdev"}, topology.ModeDev, topology.RuntimeDynamic)
	if strings.Contains(allDown, "idle") || !strings.Contains(allDown, "web crashed") {
		t.Errorf("nextActions = %q, want both crashed servers named, never 'idle'", allDown)
	}
}

// Four Spring servers answered "starting" in the deploy reply; one crashed a
// minute later and nobody was told. The reply waits, within the start budget,
// for each starting server's first running or crashed answer and names the
// crashed one with its exit and the way back.
func TestDeployNextActions_WaitsForEachStartingDevServerAndNamesOneThatCrashes(t *testing.T) {
	t.Parallel()
	decl := `{"version":1,"servers":[{"name":"gateway","port":8080,"command":"a","cwd":"."},{"name":"vets","port":8083,"command":"b","cwd":"."}]}`
	c := devservertest.NewSupervised(decl)
	c.Crash = map[string]bool{"vets": true}
	c.Settle = map[string]int{"gateway": 1, "vets": 2}
	c.Redeploy()

	got := deployNextActionsFor(t.Context(), c, "/var/www", &ops.DeployResult{TargetService: "petclinicdev"}, topology.ModeDev, topology.RuntimeDynamic)

	for _, want := range []string{"gateway running", "vets crashed (exit 1)", "action=logs", `name="vets"`, "zerops_verify"} {
		if !strings.Contains(got, want) {
			t.Errorf("nextActions = %q, want it to contain %q", got, want)
		}
	}
	if strings.Contains(got, "starting") {
		t.Errorf("nextActions = %q, says starting for a server that answered within the budget", got)
	}
}

// In variant B the code volume's declaration also holds the other dev
// services' servers: a deploy's reply names only the deployed service's.
func TestDeployNextActions_AVariantBDeploy_NamesOnlyThatDevServicesServers(t *testing.T) {
	t.Parallel()
	decl := `{"version":1,"servers":[` +
		`{"name":"web","port":3000,"command":"a","cwd":"apps/web","service":"webdev"},` +
		`{"name":"api","port":8080,"command":"b","cwd":"apps/api","service":"apidev"}]}`
	c := devservertest.NewSupervised(decl)
	delete(c.Servers, "api")

	got := deployNextActionsFor(t.Context(), c, "/var/www/ws", &ops.DeployResult{TargetService: "webdev"}, topology.ModeDev, topology.RuntimeDynamic)

	if want := "Dev servers on webdev after the deploy: web running."; !strings.Contains(got, want) {
		t.Errorf("nextActions = %q, want it to contain %q", got, want)
	}
	if strings.Contains(got, "api") {
		t.Errorf("nextActions = %q, names apidev's api on webdev", got)
	}
}
