package tools

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestCheckDeploy_NilPlan_ReturnsNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	checker := checkDeploy(mock, "proj-1")
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
	checker := checkDeploy(mock, "proj-1")
	_, err := checker(context.Background(), plan, nil)
	if err == nil {
		t.Fatal("expected error from ListServices failure")
	}
}

func TestCheckDeploy_EmptyTargets_ReturnsPass(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	plan := &workflow.ServicePlan{Targets: []workflow.BootstrapTarget{}}
	checker := checkDeploy(mock, "proj-1")
	result, err := checker(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for empty targets plan")
	}
	if !result.Passed {
		t.Errorf("expected pass for empty targets, got fail: %s", result.Summary)
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
	checker := checkDeploy(mock, "proj-1")
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
