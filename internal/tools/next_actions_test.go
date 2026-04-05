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

func TestDeploySuccessNextActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		result       *ops.DeployResult
		wantContains string
		wantAbsent   string
	}{
		{
			name: "self_deploy_dynamic_warns_server_not_running",
			result: &ops.DeployResult{
				SourceService:     "appdev",
				TargetService:     "appdev",
				TargetServiceType: "nodejs@22",
			},
			wantContains: "NOT running",
		},
		{
			name: "self_deploy_implicit_mentions_autostart",
			result: &ops.DeployResult{
				SourceService:     "appdev",
				TargetService:     "appdev",
				TargetServiceType: "php-nginx@8.4",
			},
			wantContains: "auto-start",
			wantAbsent:   "NOT running",
		},
		{
			name: "cross_deploy_uses_standard",
			result: &ops.DeployResult{
				SourceService:     "appdev",
				TargetService:     "appstage",
				TargetServiceType: "nodejs@22",
			},
			wantContains: "zerops_logs",
			wantAbsent:   "NOT running",
		},
		{
			name: "self_deploy_static_mentions_autostart",
			result: &ops.DeployResult{
				SourceService:     "webdev",
				TargetService:     "webdev",
				TargetServiceType: "static",
			},
			wantContains: "auto-start",
			wantAbsent:   "NOT running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := deploySuccessNextActions(tt.result)
			if tt.wantContains != "" && !strings.Contains(got, tt.wantContains) {
				t.Errorf("deploySuccessNextActions() = %q, want to contain %q", got, tt.wantContains)
			}
			if tt.wantAbsent != "" && strings.Contains(got, tt.wantAbsent) {
				t.Errorf("deploySuccessNextActions() = %q, should NOT contain %q", got, tt.wantAbsent)
			}
		})
	}
}
