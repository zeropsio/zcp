package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestDeployPreFlight_ValidConfig_Passes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write zerops.yaml at project root.
	yaml := `zerops:
  - setup: dev
    build:
      base: nodejs@22
    run:
      start: node index.js
      envVariables:
        NODE_ENV: development
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// Write ServiceMeta.
	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             "dev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Passed {
		t.Errorf("expected passed, got failed: %s", result.Summary)
		for _, c := range result.Checks {
			if c.Status == statusFail {
				t.Errorf("  %s: %s", c.Name, c.Detail)
			}
		}
	}
}

func TestDeployPreFlight_MissingZeropsYaml_Fails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             "dev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	// No zerops.yaml written.
	mock := platform.NewMock()
	result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Passed {
		t.Error("expected fail when zerops.yaml missing")
	}
	hasYmlCheck := false
	for _, c := range result.Checks {
		if c.Name == "zerops_yml_exists" && c.Status == statusFail {
			hasYmlCheck = true
		}
	}
	if !hasYmlCheck {
		t.Error("expected zerops_yml_exists fail check")
	}
}

func TestDeployPreFlight_MissingSetupEntry_Fails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// zerops.yaml with "prod" setup but no "dev" setup.
	yaml := `zerops:
  - setup: prod
    build:
      base: nodejs@22
    run:
      start: node index.js
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             "dev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Passed {
		t.Error("expected fail when setup entry missing")
	}
	hasSetupCheck := false
	for _, c := range result.Checks {
		if c.Name == "appdev_setup" && c.Status == statusFail {
			hasSetupCheck = true
		}
	}
	if !hasSetupCheck {
		t.Error("expected appdev_setup fail check")
	}
}

func TestDeployPreFlight_ExplicitSetup_Passes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	yaml := `zerops:
  - setup: prod
    build:
      base: nodejs@22
    run:
      start: node index.js
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	// Simple mode service — role "simple" maps to "prod" only via explicit param.
	meta := &workflow.ServiceMeta{
		Hostname:         "app",
		Mode:             "simple",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "app", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Passed {
		t.Errorf("expected passed with explicit setup=prod, got: %s", result.Summary)
	}
}

func TestDeployPreFlight_EmptyStateDir_ReturnsNil(t *testing.T) {
	t.Parallel()

	result, err := deployPreFlight(context.Background(), nil, "", "", "appdev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when stateDir is empty")
	}
}

func TestDeployPreFlight_NoMeta_ReturnsNil(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	result, err := deployPreFlight(context.Background(), nil, "", stateDir, "unknown", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when no ServiceMeta exists")
	}
}

func TestDeployPreFlight_DeployFilesMissing_Fails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	yaml := `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: dist
    run:
      start: node index.js
`
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             "dev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "appdev", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// deployFiles validation may or may not flag this depending on whether
	// the "dist" directory exists in the project root. Since it doesn't,
	// this should fail.
	hasDeployFilesCheck := false
	for _, c := range result.Checks {
		if c.Name == "appdev_deploy_files" && c.Status == statusFail {
			hasDeployFilesCheck = true
		}
	}
	if !hasDeployFilesCheck {
		t.Error("expected appdev_deploy_files fail check for missing dist directory")
	}
}
