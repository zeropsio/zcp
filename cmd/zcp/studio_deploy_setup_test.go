package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// resolveDeploySetup is the seam the Studio cockpit deploy uses to pick the
// zerops.yaml setup-block name. These tests pin the container-mode bug fix: the
// setup name MUST come from the ServiceMeta single owner (or the working-dir
// yaml), NEVER default to the hostname — a hostname default rejected
// semantic-named setups (appdev → dev/prod) with the platform's
// "The setup was not found".

// writeDeployFixture lays down a state dir with a service meta and a working
// dir with a zerops.yaml, returning (stateDir, workingDir).
func writeDeployFixture(t *testing.T, meta *workflow.ServiceMeta, yaml string) (string, string) {
	t.Helper()
	root := t.TempDir()
	stateDir := filepath.Join(root, ".zcp", "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		if err := workflow.WriteServiceMeta(stateDir, meta); err != nil {
			t.Fatalf("write meta: %v", err)
		}
	}
	workingDir := filepath.Join(root, "src")
	if err := os.MkdirAll(workingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if yaml != "" {
		if err := os.WriteFile(filepath.Join(workingDir, "zerops.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return stateDir, workingDir
}

const devProdYAML = `zerops:
  - setup: prod
    build:
      base: php@8.4
    run:
      base: php-nginx@8.4
  - setup: dev
    build:
      base: php@8.4
    run:
      base: php-nginx@8.4
`

// TestResolveDeploySetup_StandardPairUsesMetaNotHostname is the core regression:
// a standard dev/stage pair whose yaml setups are dev+prod must resolve from the
// meta (appdev→dev, appstage→prod) — NOT to the hostname, which matches neither.
func TestResolveDeploySetup_StandardPairUsesMetaNotHostname(t *testing.T) {
	meta := &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		PrimarySetupName: "dev",
		StageSetupName:   "prod",
		BootstrappedAt:   "2026-06-25",
	}
	stateDir, workingDir := writeDeployFixture(t, meta, devProdYAML)

	got, err := resolveDeploySetup(context.Background(), nil, stateDir, "appdev", workingDir)
	if err != nil {
		t.Fatalf("appdev resolve: unexpected error: %v", err)
	}
	if got != "dev" {
		t.Errorf("appdev → setup %q, want \"dev\" (from meta.primarySetupName, not hostname)", got)
	}

	gotStage, err := resolveDeploySetup(context.Background(), nil, stateDir, "appstage", workingDir)
	if err != nil {
		t.Fatalf("appstage resolve: unexpected error: %v", err)
	}
	if gotStage != "prod" {
		t.Errorf("appstage → setup %q, want \"prod\" (from meta.stageSetupName via pair-keyed lookup)", gotStage)
	}
}

// TestResolveDeploySetup_SimpleHostnameMatch covers the case that already worked
// (setup name == hostname) — it must keep working through the meta path.
func TestResolveDeploySetup_SimpleHostnameMatch(t *testing.T) {
	meta := &workflow.ServiceMeta{
		Hostname:         "snake",
		Mode:             topology.PlanModeSimple,
		PrimarySetupName: "snake",
		BootstrappedAt:   "2026-06-26",
	}
	yaml := `zerops:
  - setup: snake
    build:
      base: nodejs@22
    run:
      base: nginx@1.22
`
	stateDir, workingDir := writeDeployFixture(t, meta, yaml)

	got, err := resolveDeploySetup(context.Background(), nil, stateDir, "snake", workingDir)
	if err != nil {
		t.Fatalf("snake resolve: unexpected error: %v", err)
	}
	if got != "snake" {
		t.Errorf("snake → setup %q, want \"snake\"", got)
	}
}

// TestResolveDeploySetup_NoMetaMultiSetupSurfacesChoices proves the honest
// fallback: with no meta and a multi-setup yaml where nothing matches the
// hostname (and no mode hint to drive suffix matching), the resolver returns the
// structured blocker carrying the available setups — the cockpit then prints them
// instead of reproducing the cryptic "setup not found".
func TestResolveDeploySetup_NoMetaMultiSetupSurfacesChoices(t *testing.T) {
	stateDir, workingDir := writeDeployFixture(t, nil, devProdYAML)

	_, err := resolveDeploySetup(context.Background(), nil, stateDir, "appdev", workingDir)
	var blocker *workflow.ErrRequiresSetupInput
	if !errors.As(err, &blocker) {
		t.Fatalf("no-meta multi-setup: got err %v, want *ErrRequiresSetupInput", err)
	}
	want := map[string]bool{"prod": true, "dev": true}
	for _, s := range blocker.AvailableSetups {
		delete(want, s)
	}
	if len(want) != 0 {
		t.Errorf("blocker.AvailableSetups=%v, want both prod and dev", blocker.AvailableSetups)
	}
}
