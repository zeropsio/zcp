package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// Gate B end-to-end: classic-bootstrap meta lands with empty
// PrimarySetupName; first deploy resolves setup via role+hostname
// matching AND writes back to the meta cache so subsequent deploys
// hit the Gate B cache instead of re-resolving.
func TestDeployPreFlight_GateB_FirstDeployWritesBackResolvedSetup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scaffoldServiceYaml(t, dir, "apidev", `zerops:
  - setup: dev
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: development
  - setup: prod
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: production
`)
	// Classic-bootstrap meta: no PrimarySetupName.
	meta := &workflow.ServiceMeta{
		Hostname:         "apidev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	resolved, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "apidev", "apidev", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || !result.Passed {
		t.Fatalf("expected preflight pass; got: %+v", result)
	}
	if resolved != "dev" {
		t.Errorf("resolvedSetup: got %q, want dev (role-based fallback)", resolved)
	}

	// Cache write-back: subsequent ReadServiceMeta sees PrimarySetupName="dev".
	cached, _ := workflow.ReadServiceMeta(stateDir, "apidev")
	if cached.PrimarySetupName != "dev" {
		t.Errorf("PrimarySetupName after first deploy: got %q, want dev (Gate B write-back)",
			cached.PrimarySetupName)
	}
}

// Cache-hit path: once Gate B has written the cache, subsequent
// preflights short-circuit on meta.SetupNameFor — no need to re-walk
// the yaml-based resolution. The yaml may still be parsed for env-ref
// validation, but setup name comes from cache.
func TestDeployPreFlight_GateB_SubsequentDeployUsesCachedSetup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Yaml has only "user-customized-setup" (NOT dev/prod) — so role
	// fallback would fail. Cache-hit must bypass that.
	scaffoldServiceYaml(t, dir, "apidev", `zerops:
  - setup: user-customized-setup
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
      envVariables:
        NODE_ENV: development
`)
	// Meta pre-populated with the user-chosen setup.
	meta := &workflow.ServiceMeta{
		Hostname:         "apidev",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
		PrimarySetupName: "user-customized-setup",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	resolved, result, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "apidev", "apidev", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || !result.Passed {
		t.Fatalf("expected preflight pass (cache hit); got: %+v", result)
	}
	if resolved != "user-customized-setup" {
		t.Errorf("resolvedSetup: got %q, want user-customized-setup (cache hit)", resolved)
	}
}

// Multi-block ambiguity returns the typed blocker so deploy_local /
// deploy_batch surface RequiresSetupInputResponse — verified end-to-end
// against the meta state (no write-back) + error type.
func TestDeployPreFlight_GateB_MultiSetupAmbiguity_ReturnsBlocker(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scaffoldServiceYaml(t, dir, "frontend", `zerops:
  - setup: web
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/main.js
      ports:
        - port: 3000
  - setup: api
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      start: node dist/api.js
      ports:
        - port: 4000
`)
	// Hostname "frontend" doesn't match "web" or "api"; role for a
	// PlanModeDev singleton is DeployRoleDev → "dev" — also no match.
	meta := &workflow.ServiceMeta{
		Hostname:         "frontend",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-04-01T00:00:00Z",
	}
	if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
		t.Fatal(err)
	}

	mock := platform.NewMock()
	_, _, err := deployPreFlight(context.Background(), mock, "proj-1", stateDir, "frontend", "frontend", "", "")
	if err == nil {
		t.Fatal("expected ErrRequiresSetupInput, got nil")
	}
	var blocker *workflow.ErrRequiresSetupInput
	if !errors.As(err, &blocker) {
		t.Fatalf("error type: want *workflow.ErrRequiresSetupInput, got %T", err)
	}
	if blocker.TargetHostname != "frontend" {
		t.Errorf("TargetHostname: got %q, want frontend", blocker.TargetHostname)
	}
	if len(blocker.AvailableSetups) != 2 {
		t.Errorf("AvailableSetups: got %v, want [web api]", blocker.AvailableSetups)
	}

	// Meta cache must stay empty on miss — no half-correct write-back.
	cached, _ := workflow.ReadServiceMeta(stateDir, "frontend")
	if cached.PrimarySetupName != "" {
		t.Errorf("PrimarySetupName: got %q, want empty (no write-back on miss)",
			cached.PrimarySetupName)
	}
}
