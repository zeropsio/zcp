package workflow

import (
	"testing"
	"time"
)

// planEnvelope returns a minimal envelope with the requested phase. Helpers
// below layer on services / work session state.
func planEnvelope(phase Phase) StateEnvelope {
	return StateEnvelope{
		Phase:       phase,
		Environment: EnvLocal,
		Generated:   time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC),
	}
}

func TestBuildPlan_IdleEmptyProject(t *testing.T) {
	t.Parallel()

	plan := BuildPlan(planEnvelope(PhaseIdle))
	if plan.Primary.Tool != "zerops_workflow" {
		t.Errorf("tool = %q, want zerops_workflow", plan.Primary.Tool)
	}
	if plan.Primary.Args["workflow"] != "bootstrap" {
		t.Errorf("workflow arg = %q, want bootstrap", plan.Primary.Args["workflow"])
	}
	if plan.Secondary != nil {
		t.Errorf("secondary = %+v, want nil for empty-idle", plan.Secondary)
	}
}

func TestBuildPlan_IdleBootstrappedOnly(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseIdle)
	env.Services = []ServiceSnapshot{
		{Hostname: "appdev", RuntimeClass: RuntimeDynamic, Bootstrapped: true, Mode: ModeDev, Strategy: "push-git"},
		{Hostname: "db", RuntimeClass: RuntimeManaged},
	}
	plan := BuildPlan(env)
	if plan.Primary.Args["workflow"] != "develop" {
		t.Errorf("workflow = %q, want develop", plan.Primary.Args["workflow"])
	}
	if len(plan.Alternatives) != 1 {
		t.Errorf("alternatives = %d, want 1 (add-services only; no adoptable)", len(plan.Alternatives))
	}
}

func TestBuildPlan_IdleWithAdoptableAndBootstrapped(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseIdle)
	env.Services = []ServiceSnapshot{
		{Hostname: "appdev", RuntimeClass: RuntimeDynamic, Bootstrapped: true, Mode: ModeDev},
		{Hostname: "legacy", RuntimeClass: RuntimeDynamic, Bootstrapped: false},
	}
	plan := BuildPlan(env)
	if plan.Primary.Args["workflow"] != "develop" {
		t.Errorf("primary workflow = %q, want develop", plan.Primary.Args["workflow"])
	}
	if len(plan.Alternatives) != 2 {
		t.Fatalf("alternatives = %d, want [adopt, add-services]", len(plan.Alternatives))
	}
	if plan.Alternatives[0].Args["intent"] != "adopt" {
		t.Errorf("alt[0] intent = %q, want adopt", plan.Alternatives[0].Args["intent"])
	}
}

func TestBuildPlan_IdleOnlyUnmanagedRuntimes(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseIdle)
	env.Services = []ServiceSnapshot{
		{Hostname: "legacy", RuntimeClass: RuntimeDynamic, Bootstrapped: false},
	}
	plan := BuildPlan(env)
	if plan.Primary.Args["intent"] != "adopt" {
		t.Errorf("primary intent = %q, want adopt", plan.Primary.Args["intent"])
	}
}

func TestBuildPlan_DevelopClosedAutoRecommendsCloseAndStartNext(t *testing.T) {
	t.Parallel()

	plan := BuildPlan(planEnvelope(PhaseDevelopClosed))
	if plan.Primary.Args["action"] != "close" {
		t.Errorf("primary action = %q, want close", plan.Primary.Args["action"])
	}
	if plan.Secondary == nil || plan.Secondary.Args["action"] != "start" {
		t.Errorf("secondary = %+v, want start", plan.Secondary)
	}
}

func TestBuildPlan_DevelopActiveDeployPending(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "fix login",
		Services: []string{"appdev"},
	}
	plan := BuildPlan(env)
	if plan.Primary.Tool != "zerops_deploy" {
		t.Errorf("tool = %q, want zerops_deploy", plan.Primary.Tool)
	}
	if plan.Primary.Args["hostname"] != "appdev" {
		t.Errorf("hostname = %q, want appdev", plan.Primary.Args["hostname"])
	}
}

func TestBuildPlan_DevelopActiveVerifyPending(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "fix login",
		Services: []string{"appdev"},
		Deploys: map[string][]AttemptInfo{
			"appdev": {{Success: true, Iteration: 1}},
		},
	}
	plan := BuildPlan(env)
	if plan.Primary.Tool != "zerops_verify" {
		t.Errorf("tool = %q, want zerops_verify", plan.Primary.Tool)
	}
}

func TestBuildPlan_DevelopActiveAllGreenSuggestsClose(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "fix login",
		Services: []string{"appdev"},
		Deploys: map[string][]AttemptInfo{
			"appdev": {{Success: true}},
		},
		Verifies: map[string][]AttemptInfo{
			"appdev": {{Success: true}},
		},
	}
	plan := BuildPlan(env)
	if plan.Primary.Args["action"] != "close" {
		t.Errorf("all-green session primary = %q, want close", plan.Primary.Args["action"])
	}
}

func TestBuildPlan_DevelopActiveFailedAttemptRoutesToLogs(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "ship release",
		Services: []string{"appdev"},
		Deploys: map[string][]AttemptInfo{
			"appdev": {{Success: true}, {Success: false, Iteration: 2}},
		},
		Verifies: map[string][]AttemptInfo{
			"appdev": {{Success: true}},
		},
	}
	plan := BuildPlan(env)
	if plan.Primary.Tool != "zerops_deploy" {
		t.Errorf("with last deploy failed, expected deploy retry, got %q", plan.Primary.Tool)
	}
	if plan.Primary.Args["hostname"] != "appdev" {
		t.Errorf("hostname = %q, want appdev", plan.Primary.Args["hostname"])
	}
}

func TestBuildPlan_BootstrapActive(t *testing.T) {
	t.Parallel()

	plan := BuildPlan(planEnvelope(PhaseBootstrapActive))
	if plan.Primary.Args["workflow"] != "bootstrap" || plan.Primary.Args["action"] != "iterate" {
		t.Errorf("primary args = %+v, want bootstrap/iterate", plan.Primary.Args)
	}
}

func TestBuildPlan_RecipeActive(t *testing.T) {
	t.Parallel()

	plan := BuildPlan(planEnvelope(PhaseRecipeActive))
	if plan.Primary.Args["workflow"] != "recipe" || plan.Primary.Args["action"] != "iterate" {
		t.Errorf("primary args = %+v, want recipe/iterate", plan.Primary.Args)
	}
}

func TestBuildPlan_UnknownPhaseReturnsZero(t *testing.T) {
	t.Parallel()

	plan := BuildPlan(StateEnvelope{Phase: "ghost"})
	if !plan.Primary.IsZero() {
		t.Errorf("unknown phase should yield empty plan, got %+v", plan.Primary)
	}
}

func TestBuildPlan_DeterministicByHostnameOrder(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	// Two services, both needing deploy. The plan should pick the first by
	// the work session's declared ordering (not by hash iteration).
	env.WorkSession = &WorkSessionSummary{
		Intent:   "migrate",
		Services: []string{"alpha", "beta"},
	}
	first := BuildPlan(env)
	for i := range 5 {
		got := BuildPlan(env)
		if got.Primary.Args["hostname"] != first.Primary.Args["hostname"] {
			t.Fatalf("iteration %d picked %q, original picked %q", i, got.Primary.Args["hostname"], first.Primary.Args["hostname"])
		}
	}
}
