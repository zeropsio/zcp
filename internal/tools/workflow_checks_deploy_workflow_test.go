package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
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
	checker := buildDeployStepChecker(workflow.DeployStepExecute, mock, "proj-1", t.TempDir())
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

func TestCheckDeployPrepare_StageDeployFiles_SkippedForStage(t *testing.T) {
	t.Parallel()
	// Simulate container env: project root at dir, dev mount at dir/appdev/.
	dir := t.TempDir()
	stateDir := dir + "/.zcp/state"

	// Create dev mount with zerops.yaml and source files.
	mountDir := dir + "/appdev"
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// zerops.yaml: dev deploys [.], prod deploys cherry-picked paths.
	zeropsYml := `zerops:
  - setup: dev
    build:
      base: php@8.4
      deployFiles: [.]
    run:
      base: php-nginx@8.4
  - setup: prod
    build:
      base: php@8.4
      buildCommands:
        - composer install --no-dev
      deployFiles:
        - ./index.php
        - ./vendor
    run:
      base: php-nginx@8.4
`
	if err := os.WriteFile(mountDir+"/zerops.yaml", []byte(zeropsYml), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create index.php in mount (source file exists on dev).
	if err := os.WriteFile(mountDir+"/index.php", []byte("<?php echo 'hi';"), 0o644); err != nil {
		t.Fatal(err)
	}
	// vendor/ does NOT exist — it's a build artifact created by composer install.
	// Stage checker must not fail on this.

	state := &workflow.DeployState{
		Targets: []workflow.DeployTarget{
			{Hostname: "appdev", Role: workflow.DeployRoleDev},
			{Hostname: "appstage", Role: workflow.DeployRoleStage},
		},
	}

	checker := checkDeployPrepare(nil, "", stateDir)
	result, err := checker(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should pass — stage deploy_files must not be validated.
	if !result.Passed {
		var fails []string
		for _, c := range result.Checks {
			if c.Status == statusFail {
				fails = append(fails, fmt.Sprintf("%s: %s", c.Name, c.Detail))
			}
		}
		t.Errorf("expected pass, got failures: %v", fails)
	}

	// Verify no appstage_deploy_files check exists at all.
	for _, c := range result.Checks {
		if c.Name == "appstage_deploy_files" {
			t.Errorf("stage deploy_files check should be skipped, but found: %s", c.Detail)
		}
	}
}

func TestCheckDeployPrepare_DevDeployFiles_CheckedAgainstMount(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateDir := dir + "/.zcp/state"

	mountDir := dir + "/appdev"
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Dev setup with cherry-picked deployFiles (not [.]).
	zeropsYml := `zerops:
  - setup: dev
    build:
      base: php@8.4
      deployFiles:
        - ./index.php
        - ./config.php
    run:
      base: php-nginx@8.4
`
	if err := os.WriteFile(mountDir+"/zerops.yaml", []byte(zeropsYml), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create index.php but NOT config.php — should fail.
	if err := os.WriteFile(mountDir+"/index.php", []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}

	state := &workflow.DeployState{
		Targets: []workflow.DeployTarget{
			{Hostname: "appdev", Role: workflow.DeployRoleDev},
		},
	}

	checker := checkDeployPrepare(nil, "", stateDir)
	result, err := checker(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Should fail — config.php missing on mount path.
	if result.Passed {
		t.Error("expected fail: config.php doesn't exist on mount")
	}

	// The failure should mention config.php.
	found := false
	for _, c := range result.Checks {
		if c.Name == "appdev_deploy_files" && c.Status == statusFail {
			found = true
			if !strings.Contains(c.Detail, "config.php") {
				t.Errorf("expected detail to mention config.php, got: %s", c.Detail)
			}
		}
	}
	if !found {
		t.Error("expected appdev_deploy_files fail check")
	}
}
