// Tests for: next_actions.go — NextActions constants and functions.
package tools

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

func TestNextActions_ContainToolNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		action   string
		wantTool string
	}{
		{"deploy_success_logs", nextActionDeploySuccess, "zerops_logs"},
		{"deploy_build_fail", nextActionDeployBuildFail, "buildLogs"},
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

// TestDeploySuccessNextActions_Unified pins the honest-state contract
// (DS-01): deploySuccessNextActions returns the same unified next-action
// for every runtime class and deploy shape. It does NOT construct SSH
// commands, does NOT claim "server NOT running", does NOT claim
// "auto-start". Dev-server lifecycle guidance is owned by atoms (they
// prescribe zerops_dev_server in container env, harness background task
// in local env). See plans/dev-server-canonical-primitive.md.
func TestDeploySuccessNextActions_Unified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result *ops.DeployResult
	}{
		{
			name: "self_deploy_dynamic",
			result: &ops.DeployResult{
				SourceService:     "appdev",
				TargetService:     "appdev",
				TargetServiceType: "nodejs@22",
			},
		},
		{
			name: "self_deploy_implicit_webserver",
			result: &ops.DeployResult{
				SourceService:     "appdev",
				TargetService:     "appdev",
				TargetServiceType: "php-nginx@8.4",
			},
		},
		{
			name: "cross_deploy_dev_to_stage",
			result: &ops.DeployResult{
				SourceService:     "appdev",
				TargetService:     "appstage",
				TargetServiceType: "nodejs@22",
			},
		},
		{
			name: "self_deploy_static",
			result: &ops.DeployResult{
				SourceService:     "webdev",
				TargetService:     "webdev",
				TargetServiceType: "static",
			},
		},
	}

	forbidden := []string{
		"NOT running",
		"idle start",
		"auto-start",
		"Built-in webserver",
		"ssh ",
		"SSH session",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := deploySuccessNextActions(tt.result)
			if got != nextActionDeploySuccess {
				t.Errorf("deploySuccessNextActions(%s) = %q, want unified nextActionDeploySuccess (%q)",
					tt.name, got, nextActionDeploySuccess)
			}
			for _, phrase := range forbidden {
				if strings.Contains(got, phrase) {
					t.Errorf("deploySuccessNextActions must not assert runtime state or embed SSH; contained %q in output: %s",
						phrase, got)
				}
			}
		})
	}
}
