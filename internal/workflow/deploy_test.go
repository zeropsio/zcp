package workflow

import (
	"strings"
	"testing"
)

func TestNewDeployState_StandardMode(t *testing.T) {
	t.Parallel()
	targets := []DeployTarget{
		{Hostname: "appdev", Role: DeployRoleDev, Status: deployTargetPending},
		{Hostname: "appstage", Role: DeployRoleStage, Status: deployTargetPending},
	}
	ds := NewDeployState(targets, PlanModeStandard, StrategyPushDev)

	if !ds.Active {
		t.Error("expected Active")
	}
	if ds.Mode != PlanModeStandard {
		t.Errorf("Mode: want standard, got %s", ds.Mode)
	}
	if len(ds.Steps) != 3 {
		t.Fatalf("Steps: want 3, got %d", len(ds.Steps))
	}
	if len(ds.Targets) != 2 {
		t.Fatalf("Targets: want 2, got %d", len(ds.Targets))
	}
	if ds.Targets[0].Role != DeployRoleDev {
		t.Error("first target should be dev")
	}
	if ds.Targets[1].Role != DeployRoleStage {
		t.Error("second target should be stage")
	}
}

func TestNewDeployState_SimpleMode(t *testing.T) {
	t.Parallel()
	targets := []DeployTarget{
		{Hostname: "app", Role: DeployRoleSimple, Status: deployTargetPending},
	}
	ds := NewDeployState(targets, PlanModeSimple, StrategyPushDev)

	if len(ds.Targets) != 1 {
		t.Fatalf("Targets: want 1, got %d", len(ds.Targets))
	}
	if ds.Targets[0].Role != DeployRoleSimple {
		t.Error("target should be simple")
	}
}

func TestDeployState_CompleteStep_Sequence(t *testing.T) {
	t.Parallel()
	ds := NewDeployState([]DeployTarget{
		{Hostname: "app", Role: DeployRoleSimple, Status: deployTargetPending},
	}, PlanModeSimple, StrategyPushDev)
	ds.Steps[0].Status = stepInProgress

	// Complete prepare.
	if err := ds.CompleteStep(DeployStepPrepare, "Config checked, zerops.yml valid"); err != nil {
		t.Fatalf("complete prepare: %v", err)
	}
	if ds.CurrentStepName() != DeployStepExecute {
		t.Errorf("after prepare: want execute, got %s", ds.CurrentStepName())
	}

	// Complete execute.
	if err := ds.CompleteStep(DeployStepExecute, "Deployed successfully to all targets"); err != nil {
		t.Fatalf("complete execute: %v", err)
	}
	if ds.CurrentStepName() != DeployStepVerify {
		t.Errorf("after execute: want verify, got %s", ds.CurrentStepName())
	}

	// Complete verify.
	if err := ds.CompleteStep(DeployStepVerify, "All targets healthy, verification passed"); err != nil {
		t.Fatalf("complete verify: %v", err)
	}
	if ds.Active {
		t.Error("should be inactive after all steps")
	}
}

func TestDeployState_CompleteStep_WrongStep(t *testing.T) {
	t.Parallel()
	ds := NewDeployState(nil, PlanModeSimple, StrategyPushDev)
	ds.Steps[0].Status = stepInProgress

	err := ds.CompleteStep(DeployStepExecute, "some attestation text here")
	if err == nil {
		t.Fatal("expected error for wrong step")
	}
	if !strings.Contains(err.Error(), "prepare") {
		t.Errorf("error should mention current step 'prepare', got: %s", err.Error())
	}
}

func TestDeployState_ResetForIteration(t *testing.T) {
	t.Parallel()
	ds := NewDeployState([]DeployTarget{
		{Hostname: "app", Role: DeployRoleSimple, Status: deployTargetPending},
	}, PlanModeSimple, StrategyPushDev)

	// Complete all steps.
	ds.Steps[0].Status = stepInProgress
	for _, name := range []string{DeployStepPrepare, DeployStepExecute, DeployStepVerify} {
		if err := ds.CompleteStep(name, "Completed "+name+" step successfully here"); err != nil {
			t.Fatalf("complete %s: %v", name, err)
		}
	}
	if ds.Active {
		t.Fatal("precondition: should be inactive")
	}

	ds.ResetForIteration()

	if !ds.Active {
		t.Error("should be active after reset")
	}
	if ds.CurrentStep != 1 {
		t.Errorf("CurrentStep: want 1 (execute), got %d", ds.CurrentStep)
	}
	if ds.Steps[0].Status != stepComplete {
		t.Error("prepare should stay complete")
	}
	if ds.Steps[1].Status != stepInProgress {
		t.Error("execute should be in_progress")
	}
	if ds.Targets[0].Status != deployTargetPending {
		t.Error("targets should be reset to pending")
	}
}

func TestDeployState_BuildResponse(t *testing.T) {
	t.Parallel()
	ds := NewDeployState([]DeployTarget{
		{Hostname: "appdev", Role: DeployRoleDev, Status: deployTargetPending},
	}, PlanModeStandard, StrategyPushDev)
	ds.Steps[0].Status = stepInProgress

	resp := ds.BuildResponse("sess-1", "deploy app", 0, EnvContainer)
	if resp.SessionID != "sess-1" {
		t.Errorf("SessionID: want sess-1, got %s", resp.SessionID)
	}
	if resp.Progress.Total != 3 {
		t.Errorf("Progress.Total: want 3, got %d", resp.Progress.Total)
	}
	if resp.Current == nil {
		t.Fatal("Current should not be nil")
	}
	if resp.Current.Name != DeployStepPrepare {
		t.Errorf("Current.Name: want prepare, got %s", resp.Current.Name)
	}
	if len(resp.Targets) != 1 {
		t.Errorf("Targets: want 1, got %d", len(resp.Targets))
	}
}

func TestBuildDeployTargets_Standard(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{
		{
			Hostname:       "appdev",
			Mode:           PlanModeStandard,
			StageHostname:  "appstage",
			DeployStrategy: StrategyPushDev,
		},
	}
	targets, mode, strategy := BuildDeployTargets(metas)

	if mode != PlanModeStandard {
		t.Errorf("mode: want standard, got %s", mode)
	}
	if strategy != StrategyPushDev {
		t.Errorf("strategy: want push-dev, got %s", strategy)
	}
	if len(targets) != 2 {
		t.Fatalf("targets: want 2, got %d", len(targets))
	}
	if targets[0].Hostname != "appdev" || targets[0].Role != DeployRoleDev {
		t.Errorf("target[0]: want appdev/dev, got %s/%s", targets[0].Hostname, targets[0].Role)
	}
	if targets[1].Hostname != "appstage" || targets[1].Role != DeployRoleStage {
		t.Errorf("target[1]: want appstage/stage, got %s/%s", targets[1].Hostname, targets[1].Role)
	}
}

func TestBuildDeployTargets_Simple(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{
		{
			Hostname:       "app",
			Mode:           PlanModeSimple,
			DeployStrategy: StrategyCICD,
		},
	}
	targets, mode, strategy := BuildDeployTargets(metas)

	if mode != PlanModeSimple {
		t.Errorf("mode: want simple, got %s", mode)
	}
	if strategy != StrategyCICD {
		t.Errorf("strategy: want ci-cd, got %s", strategy)
	}
	if len(targets) != 1 {
		t.Fatalf("targets: want 1, got %d", len(targets))
	}
	if targets[0].Role != DeployRoleSimple {
		t.Errorf("target role: want simple, got %s", targets[0].Role)
	}
}

func TestBuildDeployTargets_Empty(t *testing.T) {
	t.Parallel()
	targets, mode, strategy := BuildDeployTargets(nil)
	if targets != nil {
		t.Error("expected nil targets for nil metas")
	}
	if mode != "" {
		t.Error("expected empty mode for nil metas")
	}
	if strategy != "" {
		t.Error("expected empty strategy for nil metas")
	}
}

func TestBuildDeployTargets_PopulatesStrategy(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{
		{
			Hostname:       "appdev",
			Mode:           PlanModeStandard,
			StageHostname:  "appstage",
			DeployStrategy: StrategyPushDev,
		},
	}
	targets, _, strategy := BuildDeployTargets(metas)

	if strategy != StrategyPushDev {
		t.Errorf("strategy: want push-dev, got %s", strategy)
	}
	// Both dev and stage targets should have strategy set.
	if len(targets) != 2 {
		t.Fatalf("targets: want 2, got %d", len(targets))
	}
	if targets[0].Strategy != StrategyPushDev {
		t.Errorf("target[0].Strategy: want push-dev, got %q", targets[0].Strategy)
	}
	if targets[1].Strategy != StrategyPushDev {
		t.Errorf("target[1].Strategy: want push-dev, got %q", targets[1].Strategy)
	}
}

func TestNewDeployState_CapturesStrategy(t *testing.T) {
	t.Parallel()
	targets := []DeployTarget{
		{Hostname: "app", Role: DeployRoleSimple, Status: deployTargetPending, Strategy: StrategyCICD},
	}
	ds := NewDeployState(targets, PlanModeSimple, StrategyCICD)

	if ds.Strategy != StrategyCICD {
		t.Errorf("Strategy: want ci-cd, got %s", ds.Strategy)
	}
}
