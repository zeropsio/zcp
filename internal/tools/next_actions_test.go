// Tests for: next_actions.go — NextActions constants contain correct tool names.
package tools

import (
	"strings"
	"testing"
)

func TestNextActions_ContainToolNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		action   string
		wantTool string
	}{
		{"deploy_success_subdomain", nextActionDeploySuccess, "zerops_subdomain"},
		{"deploy_success_logs", nextActionDeploySuccess, "zerops_logs"},
		{"deploy_build_fail", nextActionDeployBuildFail, "zerops_logs"},
		{"import_success_discover", nextActionImportSuccess, "zerops_discover"},
		{"import_success_subdomain", nextActionImportSuccess, "zerops_subdomain"},
		{"import_partial_events", nextActionImportPartial, "zerops_events"},
		{"env_set_reload", nextActionEnvSetSuccess, "zerops_manage"},
		{"env_delete_reload", nextActionEnvDeleteSuccess, "zerops_manage"},
		{"manage_reload_logs", nextActionManageReload, "zerops_logs"},
		{"scale_discover", nextActionScaleSuccess, "zerops_discover"},
		{"subdomain_enable_logs", nextActionSubdomainEnable, "zerops_logs"},
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
