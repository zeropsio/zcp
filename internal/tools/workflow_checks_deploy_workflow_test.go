package tools

import (
	"context"
	"fmt"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestCheckDeployResult_NilState_ReturnsNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	checker := checkDeployResult(mock, "proj-1")
	result, err := checker(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for nil state")
	}
}

func TestCheckDeployResult_EmptyTargets_ReturnsNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	state := &workflow.DeployState{Targets: []workflow.DeployTarget{}}
	checker := checkDeployResult(mock, "proj-1")
	result, err := checker(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for empty targets")
	}
}

func TestCheckDeployResult_ServiceRunning_Passes(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "ACTIVE", SubdomainAccess: true},
	})
	state := &workflow.DeployState{
		Targets: []workflow.DeployTarget{
			{Hostname: "appdev", Role: workflow.DeployRoleDev},
		},
	}
	checker := checkDeployResult(mock, "proj-1")
	result, err := checker(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected pass for ACTIVE service, got: %+v", result)
	}
}

func TestCheckDeployResult_ServiceNotFound_Fails(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{})
	state := &workflow.DeployState{
		Targets: []workflow.DeployTarget{
			{Hostname: "appdev", Role: workflow.DeployRoleDev},
		},
	}
	checker := checkDeployResult(mock, "proj-1")
	result, err := checker(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail when service not found")
	}
}

func TestCheckDeployResult_ReadyToDeploy_Fails(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "s1", Name: "appdev", Status: "READY_TO_DEPLOY"},
	})
	state := &workflow.DeployState{
		Targets: []workflow.DeployTarget{
			{Hostname: "appdev", Role: workflow.DeployRoleDev},
		},
	}
	checker := checkDeployResult(mock, "proj-1")
	result, err := checker(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail for READY_TO_DEPLOY")
	}
	// Should contain diagnostic hint.
	for _, c := range result.Checks {
		if c.Status == statusFail && c.Name == "appdev_status" {
			if c.Detail == "" {
				t.Error("expected diagnostic detail for READY_TO_DEPLOY")
			}
			return
		}
	}
	t.Error("expected appdev_status fail check")
}

func TestCheckDeployResult_NoSubdomain_Fails(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			ID: "s1", Name: "appdev", Status: "ACTIVE",
			SubdomainAccess: false,
			Ports:           []platform.Port{{Port: 3000}},
		},
	})
	state := &workflow.DeployState{
		Targets: []workflow.DeployTarget{
			{Hostname: "appdev", Role: workflow.DeployRoleDev},
		},
	}
	checker := checkDeployResult(mock, "proj-1")
	result, err := checker(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected fail when subdomain not enabled")
	}
}

func TestCheckDeployResult_ListServicesError_ReturnsError(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock().WithError("ListServices", fmt.Errorf("API down"))
	state := &workflow.DeployState{
		Targets: []workflow.DeployTarget{
			{Hostname: "appdev", Role: workflow.DeployRoleDev},
		},
	}
	checker := checkDeployResult(mock, "proj-1")
	_, err := checker(context.Background(), state)
	if err == nil {
		t.Fatal("expected error from ListServices failure")
	}
}

func TestCheckDeployPrepare_NilState_ReturnsNil(t *testing.T) {
	t.Parallel()
	checker := checkDeployPrepare(nil, "", t.TempDir())
	result, err := checker(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for nil state")
	}
}

func TestBuildDeployStepChecker_Prepare_NonNil(t *testing.T) {
	t.Parallel()
	checker := buildDeployStepChecker(workflow.DeployStepPrepare, nil, "", t.TempDir())
	if checker == nil {
		t.Error("expected non-nil checker for prepare step")
	}
}

func TestBuildDeployStepChecker_Deploy_NonNil(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	checker := buildDeployStepChecker(workflow.DeployStepDeploy, mock, "proj-1", t.TempDir())
	if checker == nil {
		t.Error("expected non-nil checker for deploy step")
	}
}

func TestBuildDeployStepChecker_Verify_Nil(t *testing.T) {
	t.Parallel()
	checker := buildDeployStepChecker(workflow.DeployStepVerify, nil, "", t.TempDir())
	if checker != nil {
		t.Error("expected nil checker for verify step")
	}
}
