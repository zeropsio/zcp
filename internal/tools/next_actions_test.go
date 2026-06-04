// Tests for: next_actions.go — NextActions constants and functions.
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
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
