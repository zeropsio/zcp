// Scenario walkthrough tests. Each test mirrors one of the §14 scenarios
// from plans/instruction-delivery-rewrite.md and asserts the end-to-end
// envelope → plan → synthesis shape a user session would produce.
//
// These are intentionally higher-level than corpus_coverage_test.go: the
// coverage test verifies a single envelope→synthesize leg, these tests walk
// through state transitions (start of session, mid-session, failure tier)
// and assert the plan shape alongside atom output.
package workflow

import (
	"strings"
	"testing"
	"time"
)

func TestScenario_S1_NewProjectRecipeMatch(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	// S1 before: new project, Phase=idle, no services.
	before := StateEnvelope{
		Phase:       PhaseIdle,
		Environment: EnvContainer,
	}
	plan := BuildPlan(before)
	if plan.Primary.Tool != "zerops_workflow" ||
		plan.Primary.Args["action"] != "start" ||
		plan.Primary.Args["workflow"] != "bootstrap" {
		t.Errorf("S1 idle: expected primary=zerops_workflow start bootstrap, got tool=%q args=%v",
			plan.Primary.Tool, plan.Primary.Args)
	}
	bodies, err := Synthesize(before, corpus)
	if err != nil {
		t.Fatalf("Synthesize idle: %v", err)
	}
	if len(bodies) == 0 {
		t.Fatal("S1 idle: expected atoms to match, got none")
	}

	// S1 after start: bootstrap-active, Route=recipe, Step=provision.
	// Matches bootstrap_recipe_provision coverage fixture — so atoms
	// mentioning import + ACTIVE should synthesize.
	after := StateEnvelope{
		Phase:       PhaseBootstrapActive,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			TypeVersion:  "laravel@11",
			RuntimeClass: RuntimeDynamic,
			Mode:         ModeStandard,
		}},
		Bootstrap: &BootstrapSessionSummary{
			Route: BootstrapRouteRecipe,
			Step:  StepProvision,
			RecipeMatch: &RecipeMatch{
				Slug:       "laravel-dashboard",
				Confidence: 0.91,
			},
		},
	}
	planAfter := BuildPlan(after)
	if planAfter.Primary.Tool != "zerops_workflow" ||
		planAfter.Primary.Args["action"] != "iterate" ||
		planAfter.Primary.Args["workflow"] != "bootstrap" {
		t.Errorf("S1 bootstrap-active: expected iterate bootstrap primary, got tool=%q args=%v",
			planAfter.Primary.Tool, planAfter.Primary.Args)
	}
	bodiesAfter, err := Synthesize(after, corpus)
	if err != nil {
		t.Fatalf("Synthesize bootstrap-active: %v", err)
	}
	joined := strings.Join(bodiesAfter, "\n")
	for _, phrase := range []string{"zerops_import", "ACTIVE"} {
		if !strings.Contains(joined, phrase) {
			t.Errorf("S1 bootstrap-active: expected atom body to contain %q", phrase)
		}
	}
}

func TestScenario_S5_MixedBootstrappedAndUnmanaged(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	env := StateEnvelope{
		Phase:       PhaseIdle,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{
			{
				Hostname:     "db",
				TypeVersion:  "postgresql@16",
				RuntimeClass: RuntimeManaged,
				Bootstrapped: false, // managed services don't need meta
			},
			{
				Hostname:     "laraveldev",
				TypeVersion:  "php-apache@8.3",
				RuntimeClass: RuntimeDynamic,
				Mode:         ModeDev,
				Bootstrapped: true,
			},
			{
				Hostname:     "newruntime",
				TypeVersion:  "nodejs@22",
				RuntimeClass: RuntimeDynamic,
				Bootstrapped: false, // adoptable — runtime without ServiceMeta
			},
		},
	}

	plan := BuildPlan(env)

	// Mixed bootstrapped + adoptable → primary develop, alternatives must
	// include both adopt and add-services. This matches the code contract
	// (plan.go planIdle, bootstrapped>0 branch). §14 spec proposes a
	// different primary (adopt) — that divergence is on spec-audit agenda.
	if plan.Primary.Tool != "zerops_workflow" ||
		plan.Primary.Args["action"] != "start" ||
		plan.Primary.Args["workflow"] != "develop" {
		t.Errorf("S5: expected primary=develop start, got tool=%q args=%v",
			plan.Primary.Tool, plan.Primary.Args)
	}
	if len(plan.Alternatives) < 2 {
		t.Fatalf("S5: expected ≥2 alternatives (adopt + add services), got %d: %+v",
			len(plan.Alternatives), plan.Alternatives)
	}
	var sawAdopt, sawAdd bool
	for _, alt := range plan.Alternatives {
		switch alt.Label {
		case "Adopt unmanaged runtimes":
			sawAdopt = true
		case "Add more services":
			sawAdd = true
		}
	}
	if !sawAdopt {
		t.Error("S5: expected 'Adopt unmanaged runtimes' in alternatives")
	}
	if !sawAdd {
		t.Error("S5: expected 'Add more services' in alternatives")
	}

	bodies, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if len(bodies) == 0 {
		t.Fatal("S5: expected at least one idle-phase atom to synthesize")
	}
}

func TestScenario_S3_AdoptOnlyUnmanaged(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	// S3: project has runtime services but no ServiceMeta for any of them.
	// handleDevelopBriefing would try auto-adopt — but the typed-plan leg
	// sees a pure adoptable set and must route to adopt, not develop.
	env := StateEnvelope{
		Phase:       PhaseIdle,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			TypeVersion:  "nodejs@22",
			RuntimeClass: RuntimeDynamic,
			Bootstrapped: false,
		}},
	}

	plan := BuildPlan(env)

	if plan.Primary.Tool != "zerops_workflow" ||
		plan.Primary.Args["action"] != "start" ||
		plan.Primary.Args["workflow"] != "develop" ||
		plan.Primary.Args["intent"] != "adopt" {
		t.Errorf("S3 only-unmanaged: expected primary=adopt-via-develop, got tool=%q args=%v",
			plan.Primary.Tool, plan.Primary.Args)
	}
	if plan.Primary.Label != "Adopt unmanaged runtimes" {
		t.Errorf("S3: expected primary label 'Adopt unmanaged runtimes', got %q", plan.Primary.Label)
	}

	bodies, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	joined := strings.Join(bodies, "\n")
	// idle-adopt-entry atom is the load-bearing one for this scenario: it
	// tells the agent the adopt route reads live services + writes ServiceMeta.
	for _, phrase := range []string{"Adopt them before deploying", "ServiceMeta"} {
		if !strings.Contains(joined, phrase) {
			t.Errorf("S3: expected synthesized body to contain %q", phrase)
		}
	}
}

func TestScenario_S4_DevelopStrategyUnset(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	// S4: an open WorkSession exists but the service lost its strategy (e.g.
	// recreated out-of-band). ComputeEnvelope sets Strategy=StrategyUnset on
	// the snapshot, and the pipeline must surface develop-strategy-unset
	// before any deploy.
	now := time.Now().UTC()
	env := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			TypeVersion:  "nodejs@22",
			RuntimeClass: RuntimeDynamic,
			Mode:         ModeDev,
			Strategy:     StrategyUnset,
			Bootstrapped: true,
		}},
		WorkSession: &WorkSessionSummary{
			Intent:    "fix auth",
			Services:  []string{"appdev"},
			CreatedAt: now.Add(-1 * time.Minute),
		},
	}

	bodies, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	joined := strings.Join(bodies, "\n")
	for _, phrase := range []string{"Strategy selection required", `action="strategy"`} {
		if !strings.Contains(joined, phrase) {
			t.Errorf("S4: expected synthesized body to contain %q", phrase)
		}
	}

	// Plan still routes to deploy for the first service — there is no
	// strategy-aware branch in planDevelopActive. The atom guidance is the
	// gate, not the plan. Documenting the divergence so a future refactor
	// (spec-audit item) knows the test expects the current behaviour.
	plan := BuildPlan(env)
	if plan.Primary.Tool != "zerops_deploy" {
		t.Errorf("S4: expected primary=zerops_deploy (current code contract — strategy gate lives in the atom, not the plan), got tool=%q", plan.Primary.Tool)
	}
	if plan.Primary.Args["hostname"] != "appdev" {
		t.Errorf("S4: expected primary hostname=appdev, got %q", plan.Primary.Args["hostname"])
	}
}

func TestScenario_S7_DevelopClosedAuto(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	// S7: every scope service deployed+verified, auto-close fired. Plan must
	// offer close as primary (reclaim the slot) and start-next as secondary
	// (keep momentum). Atom develop-closed-auto explains the state.
	now := time.Now().UTC()
	closedAt := now.Add(-30 * time.Second)
	env := StateEnvelope{
		Phase:       PhaseDevelopClosed,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			TypeVersion:  "nodejs@22",
			RuntimeClass: RuntimeDynamic,
			Mode:         ModeDev,
			Strategy:     "push-dev",
			Bootstrapped: true,
		}},
		WorkSession: &WorkSessionSummary{
			Intent:      "fix login bug",
			Services:    []string{"appdev"},
			CreatedAt:   now.Add(-10 * time.Minute),
			ClosedAt:    &closedAt,
			CloseReason: CloseReasonAutoComplete,
		},
	}

	plan := BuildPlan(env)

	if plan.Primary.Tool != "zerops_workflow" ||
		plan.Primary.Args["action"] != "close" ||
		plan.Primary.Args["workflow"] != "develop" {
		t.Errorf("S7 closed-auto: expected primary=close develop, got tool=%q args=%v",
			plan.Primary.Tool, plan.Primary.Args)
	}
	if plan.Secondary == nil {
		t.Fatal("S7: expected Secondary action (start next task), got nil")
	}
	if plan.Secondary.Args["action"] != "start" || plan.Secondary.Args["workflow"] != "develop" {
		t.Errorf("S7: expected secondary=start develop, got args=%v", plan.Secondary.Args)
	}

	bodies, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	joined := strings.Join(bodies, "\n")
	for _, phrase := range []string{"auto-closed", `action="close"`} {
		if !strings.Contains(joined, phrase) {
			t.Errorf("S7: expected synthesized body to contain %q", phrase)
		}
	}
}

func TestScenario_S8_DevelopIterationFailure(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	now := time.Now().UTC()
	env := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			TypeVersion:  "nodejs@22",
			RuntimeClass: RuntimeDynamic,
			Mode:         ModeDev,
			Strategy:     "push-dev",
			Bootstrapped: true,
		}},
		WorkSession: &WorkSessionSummary{
			Intent:    "fix auth flow",
			Services:  []string{"appdev"},
			CreatedAt: now.Add(-10 * time.Minute),
			Deploys: map[string][]AttemptInfo{
				"appdev": {
					{At: now.Add(-8 * time.Minute), Success: true, Iteration: 1},
					{At: now.Add(-5 * time.Minute), Success: false, Iteration: 2},
					{At: now.Add(-2 * time.Minute), Success: false, Iteration: 3},
				},
			},
		},
	}

	plan := BuildPlan(env)

	// Current code contract: last deploy failed → firstServiceNeedingDeploy
	// returns that host and primary is deploy. §14 says fix-and-retry, but
	// the deploy path is what BuildPlan actually dispatches. Spec-audit
	// task #18 tracks reconciling the two.
	if plan.Primary.Tool != "zerops_deploy" {
		t.Errorf("S8 iter-3 failed: expected primary=zerops_deploy, got tool=%q", plan.Primary.Tool)
	}
	if plan.Primary.Args["hostname"] != "appdev" {
		t.Errorf("S8: expected primary hostname=appdev, got %q", plan.Primary.Args["hostname"])
	}

	bodies, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	joined := strings.Join(bodies, "\n")
	// Develop-active push-dev atoms are load-bearing at deploy iteration:
	// push-dev SSH mechanics and iteration tier guidance both belong here.
	for _, phrase := range []string{"Push-Dev Deploy Strategy", "SSH"} {
		if !strings.Contains(joined, phrase) {
			t.Errorf("S8: expected synthesized body to contain %q", phrase)
		}
	}
}
