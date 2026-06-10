package tools

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestRecordProdLaunchBackRefs pins the F4 post-launch back-reference:
// a launched finalize writes the prod project identity + ProdSetupName
// onto every promoted runtime's source meta, append-if-new (idempotent
// resume must not duplicate).
func TestRecordProdLaunchBackRefs(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "t",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	state := &launchState{
		TargetProjectID:   "prod-proj-id",
		TargetProjectName: "myapp-prod",
	}
	runtimes := []resolvedLaunchRuntime{{
		ChoiceHostname: "appstage",
		PushHostname:   "appdev",
		ProdHostname:   "app",
		SetupName:      "prod",
	}}

	recordProdLaunchBackRefs(stateDir, state, runtimes)
	recordProdLaunchBackRefs(stateDir, state, runtimes) // idempotent re-run

	meta, _ := workflow.ReadServiceMeta(stateDir, "appdev")
	if len(meta.ProdLaunches) != 1 {
		t.Fatalf("expected exactly 1 back-ref after double record, got %d: %+v", len(meta.ProdLaunches), meta.ProdLaunches)
	}
	ref := meta.ProdLaunches[0]
	if ref.ProdProjectID != "prod-proj-id" || ref.ProdHostname != "app" || ref.LaunchedAt == "" {
		t.Errorf("back-ref shape: %+v", ref)
	}
	if meta.ProdSetupName != "prod" {
		t.Errorf("ProdSetupName must be stamped from the promotion's setup; got %q", meta.ProdSetupName)
	}
}

// TestResolveLaunchSetupName_ProdSetupNameWins pins the F4 cascade
// extension: a recorded ProdSetupName outranks Stage/Primary derivation
// (a re-launch reuses the proven identity).
func TestResolveLaunchSetupName_ProdSetupNameWins(t *testing.T) {
	t.Parallel()
	meta := &workflow.ServiceMeta{
		ProdSetupName:    "production",
		StageSetupName:   "prod",
		PrimarySetupName: "appdev",
	}
	if got := resolveLaunchSetupName(LaunchPromotableInput{}, "", meta); got != "production" {
		t.Errorf("ProdSetupName must win the cascade; got %q", got)
	}
	if got := resolveLaunchSetupName(LaunchPromotableInput{ProdSetupNameOverride: "x"}, "", meta); got != "x" {
		t.Errorf("explicit override must still outrank everything; got %q", got)
	}
}
