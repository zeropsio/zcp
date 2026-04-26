package workflow

import (
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
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
		{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Mode: topology.ModeDev, Strategy: "push-git"},
		{Hostname: "db", RuntimeClass: topology.RuntimeManaged},
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
		{Hostname: "appdev", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: true, Mode: topology.ModeDev},
		{Hostname: "legacy", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: false},
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
		{Hostname: "legacy", RuntimeClass: topology.RuntimeDynamic, Bootstrapped: false},
	}
	plan := BuildPlan(env)
	if plan.Primary.Args["intent"] != "adopt" {
		t.Errorf("primary intent = %q, want adopt", plan.Primary.Args["intent"])
	}
}

func TestBuildPlan_IdleOrphanOnly_PrimaryReset(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseIdle)
	env.IdleScenario = IdleOrphan
	env.OrphanMetas = []OrphanMeta{
		{Hostname: "ghostdev", Reason: OrphanReasonLiveDeleted},
	}
	plan := BuildPlan(env)
	if plan.Primary.Args["action"] != "reset" {
		t.Errorf("primary action = %q, want reset", plan.Primary.Args["action"])
	}
	if plan.Primary.Args["workflow"] != "bootstrap" {
		t.Errorf("primary workflow = %q, want bootstrap (reset is bootstrap-scoped)", plan.Primary.Args["workflow"])
	}
	if len(plan.Alternatives) != 1 {
		t.Fatalf("alternatives = %d, want 1 (start bootstrap)", len(plan.Alternatives))
	}
	if plan.Alternatives[0].Args["action"] != "start" {
		t.Errorf("alt[0] action = %q, want start", plan.Alternatives[0].Args["action"])
	}
}

func TestBuildPlan_IdleOrphanPlusLiveService_NotResetPrimary(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseIdle)
	env.Services = []ServiceSnapshot{
		{Hostname: "alive", RuntimeClass: topology.RuntimeManaged}, // managed dep
	}
	env.OrphanMetas = []OrphanMeta{
		{Hostname: "ghost", Reason: OrphanReasonLiveDeleted},
	}
	plan := BuildPlan(env)
	// Live managed dep suppresses orphan-reset routing (matches deriveIdleScenario).
	// Falls through to "no bootstrapped, no adoptable" → bootstrap primary.
	if plan.Primary.Args["action"] == "reset" {
		t.Errorf("primary action = reset; should not route to reset when live service present")
	}
	if plan.Primary.Args["workflow"] != "bootstrap" || plan.Primary.Args["action"] != "start" {
		t.Errorf("primary = %+v, want start bootstrap", plan.Primary.Args)
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
	if plan.Primary.Args["targetService"] != "appdev" {
		t.Errorf("targetService = %q, want appdev", plan.Primary.Args["targetService"])
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
	if plan.Primary.Args["targetService"] != "appdev" {
		t.Errorf("targetService = %q, want appdev", plan.Primary.Args["targetService"])
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
		if got.Primary.Args["targetService"] != first.Primary.Args["targetService"] {
			t.Fatalf("iteration %d picked %q, original picked %q", i, got.Primary.Args["targetService"], first.Primary.Args["targetService"])
		}
	}
}

// TestBuildPlan_PerServicePopulatedMultiScope pins the per-service breakdown
// that the render layer shows under `Per service:`. All services have pending
// work → the map carries one entry per hostname.
func TestBuildPlan_PerServicePopulatedMultiScope(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "ship multi-service",
		Services: []string{"apidev", "webdev"},
		Deploys: map[string][]AttemptInfo{
			"apidev": {{Success: true, Iteration: 1}},
		},
		// webdev has no deploy yet → deploy action
		// apidev has deploy ok, no verify → verify action
	}
	plan := BuildPlan(env)
	if len(plan.PerService) != 2 {
		t.Fatalf("PerService = %d entries, want 2: %+v", len(plan.PerService), plan.PerService)
	}
	if plan.PerService["apidev"].Tool != "zerops_verify" {
		t.Errorf("apidev PerService tool = %q, want zerops_verify", plan.PerService["apidev"].Tool)
	}
	if plan.PerService["webdev"].Tool != "zerops_deploy" {
		t.Errorf("webdev PerService tool = %q, want zerops_deploy", plan.PerService["webdev"].Tool)
	}
}

// TestBuildPlan_PerServiceSkipsGreen pins the "green services are excluded"
// rule: a fully deployed+verified service does not appear in PerService.
// The remaining pending service still surfaces.
func TestBuildPlan_PerServiceSkipsGreen(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "mixed",
		Services: []string{"apidev", "webdev"},
		Deploys: map[string][]AttemptInfo{
			"apidev": {{Success: true, Iteration: 1}},
		},
		Verifies: map[string][]AttemptInfo{
			"apidev": {{Success: true, Iteration: 1}},
		},
	}
	plan := BuildPlan(env)
	if _, ok := plan.PerService["apidev"]; ok {
		t.Error("green service apidev must be excluded from PerService")
	}
	if plan.PerService["webdev"].Tool != "zerops_deploy" {
		t.Errorf("webdev tool = %q, want zerops_deploy", plan.PerService["webdev"].Tool)
	}
}

// TestBuildPlan_PerServiceSingleServiceStillPopulated asserts the map is
// populated even for a single-service scope — the render layer decides
// whether to emit the section based on len > 1. Keeping BuildPlan uniform
// lets renderers or JSON consumers inspect per-service state directly.
func TestBuildPlan_PerServiceSingleServiceStillPopulated(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "solo",
		Services: []string{"appdev"},
	}
	plan := BuildPlan(env)
	if len(plan.PerService) != 1 {
		t.Fatalf("PerService = %d entries, want 1: %+v", len(plan.PerService), plan.PerService)
	}
	if plan.PerService["appdev"].Tool != "zerops_deploy" {
		t.Errorf("appdev PerService tool = %q, want zerops_deploy", plan.PerService["appdev"].Tool)
	}
}

// TestBuildPlan_PerServiceNilWhenAllGreen pins the close-suggest branch:
// once every scope service is green, BuildPlan recommends close and
// PerService is nil (no pending work).
func TestBuildPlan_PerServiceNilWhenAllGreen(t *testing.T) {
	t.Parallel()

	env := planEnvelope(PhaseDevelopActive)
	env.WorkSession = &WorkSessionSummary{
		Intent:   "done",
		Services: []string{"appdev"},
		Deploys: map[string][]AttemptInfo{
			"appdev": {{Success: true}},
		},
		Verifies: map[string][]AttemptInfo{
			"appdev": {{Success: true}},
		},
	}
	plan := BuildPlan(env)
	if plan.PerService != nil {
		t.Errorf("PerService should be nil when all services are green, got %+v", plan.PerService)
	}
	if plan.Primary.Args["action"] != "close" {
		t.Errorf("primary action = %q, want close", plan.Primary.Args["action"])
	}
}

// TestBuildPlan_FailureTargetedRationale pins Phase 1 (C1) of the
// pipeline-repair plan: when the last deploy attempt carries a Reason +
// FailureClass, BuildPlan's Primary action surfaces the failure shape in
// its Rationale string. Without this, the Plan reads "No successful
// deploy recorded" even when the actual issue is a build timeout — losing
// the actionable signal post-compaction.
func TestBuildPlan_FailureTargetedRationale(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		failureClass   FailureClass
		reason         string
		wantInRational []string
	}{
		{
			name:           "build_failure",
			failureClass:   FailureClassBuild,
			reason:         "build timeout after 15min",
			wantInRational: []string{"build", "timeout"},
		},
		{
			name:           "start_failure",
			failureClass:   FailureClassStart,
			reason:         "container didn't start",
			wantInRational: []string{"start", "didn't start"},
		},
		{
			name:           "config_failure",
			failureClass:   FailureClassConfig,
			reason:         "zerops.yaml: deployFiles required",
			wantInRational: []string{"config", "deployFiles"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvLocal,
				WorkSession: &WorkSessionSummary{
					Intent:   "fix",
					Services: []string{"apidev"},
					Deploys: map[string][]AttemptInfo{
						"apidev": {{
							At:           time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC),
							Success:      false,
							Reason:       tc.reason,
							FailureClass: tc.failureClass,
						}},
					},
				},
			}

			plan := BuildPlan(env)

			if plan.Primary.Tool != "zerops_deploy" {
				t.Errorf("Primary.Tool = %q, want zerops_deploy", plan.Primary.Tool)
			}
			if plan.Primary.Args["targetService"] != "apidev" {
				t.Errorf("Primary.targetService = %q, want apidev", plan.Primary.Args["targetService"])
			}
			for _, want := range tc.wantInRational {
				if !strings.Contains(plan.Primary.Rationale, want) {
					t.Errorf("Rationale missing %q\nGot: %s", want, plan.Primary.Rationale)
				}
			}
		})
	}
}

// TestBuildPlan_PerServiceNilOutsideDevelopActive pins the scope: only the
// develop-active branch populates PerService. Other phases (idle, closed-auto,
// bootstrap) leave the field nil.
func TestBuildPlan_PerServiceNilOutsideDevelopActive(t *testing.T) {
	t.Parallel()

	phases := []Phase{PhaseIdle, PhaseDevelopClosed, PhaseBootstrapActive, PhaseRecipeActive}
	for _, p := range phases {
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			plan := BuildPlan(planEnvelope(p))
			if plan.PerService != nil {
				t.Errorf("phase %s: PerService = %+v, want nil", p, plan.PerService)
			}
		})
	}
}
