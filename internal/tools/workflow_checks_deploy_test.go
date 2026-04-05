package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestCheckDeploy_NilPlan_ReturnsNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	checker := checkDeploy(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for nil plan")
	}
}

func TestCheckDeploy_ListServicesError_ReturnsError(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithError("ListServices", fmt.Errorf("API down"))
	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22"},
		}},
	}
	checker := checkDeploy(mock, nil, "proj-1", nil)
	_, err := checker(context.Background(), plan, nil)
	if err == nil {
		t.Fatal("expected error from ListServices failure")
	}
}

func TestCheckDeploy_EmptyTargets_ReturnsNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	plan := &workflow.ServicePlan{Targets: []workflow.BootstrapTarget{}}
	checker := checkDeploy(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for empty targets (managed-only)")
	}
}

func TestCheckDeploy_DevNotRunning_Fails(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "DEPLOY_FAILED"},
	})
	plan := &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "simple"},
		}},
	}
	checker := checkDeploy(mock, nil, "proj-1", nil)
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail when dev service is not RUNNING")
	}
	hasFail := false
	for _, c := range result.Checks {
		if c.Status == statusFail {
			hasFail = true
		}
	}
	if !hasFail {
		t.Error("expected at least one fail check")
	}
}

func TestBuildStepChecker_StepDeploy_NonNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	checker := buildStepChecker(workflow.StepDeploy, mock, nil, "proj-1", nil, nil, t.TempDir())
	if checker == nil {
		t.Error("expected non-nil checker for StepDeploy")
	}
}

func TestCheckDevProdEnvDivergence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		yaml         string
		wantStatus   string
		wantInDetail string
	}{
		{
			name: "identical_app_env_flagged",
			yaml: `zerops:
  - setup: dev
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
  - setup: prod
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
`,
			wantStatus:   statusFail,
			wantInDetail: "APP_ENV=production",
		},
		{
			name: "divergent_app_env_passes",
			yaml: `zerops:
  - setup: dev
    run:
      envVariables:
        APP_ENV: local
        APP_DEBUG: "true"
  - setup: prod
    run:
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
`,
			wantStatus: statusPass,
		},
		{
			name: "only_dev_no_check",
			yaml: `zerops:
  - setup: dev
    run:
      envVariables:
        APP_ENV: local
`,
			wantStatus: "",
		},
		{
			name: "rails_identical_flagged",
			yaml: `zerops:
  - setup: dev
    run:
      envVariables:
        RAILS_ENV: production
  - setup: prod
    run:
      envVariables:
        RAILS_ENV: production
`,
			wantStatus:   statusFail,
			wantInDetail: "RAILS_ENV=production",
		},
		{
			name: "non_mode_keys_ignored",
			yaml: `zerops:
  - setup: dev
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        APP_ENV: local
  - setup: prod
    run:
      envVariables:
        DB_HOST: ${db_hostname}
        APP_ENV: production
`,
			wantStatus: statusPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			doc, err := ops.ParseZeropsYml(dir)
			if err != nil {
				t.Fatal(err)
			}
			checks := checkDevProdEnvDivergence(doc)
			if tt.wantStatus == "" {
				if len(checks) != 0 {
					t.Errorf("expected no checks, got %+v", checks)
				}
				return
			}
			if len(checks) != 1 {
				t.Fatalf("expected 1 check, got %d: %+v", len(checks), checks)
			}
			if checks[0].Status != tt.wantStatus {
				t.Errorf("status = %q, want %q (detail: %s)", checks[0].Status, tt.wantStatus, checks[0].Detail)
			}
			if tt.wantInDetail != "" && !strings.Contains(checks[0].Detail, tt.wantInDetail) {
				t.Errorf("detail = %q, want to contain %q", checks[0].Detail, tt.wantInDetail)
			}
		})
	}
}
