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
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/topology"
)

// atomIDsOf returns the AtomIDs of a synthesis result, sorted for stable
// comparison. Used by scenario tests to assert on which atoms fired
// independent of corpus iteration order.
func atomIDsOf(matches []MatchedRender) []string {
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.AtomID
	}
	sort.Strings(ids)
	return ids
}

// requireAtomIDsContain asserts that every wantID appears in the
// synthesis result. Subset semantics — extra atoms in the result are
// allowed, but every named one must be present. Failure message lists
// the missing IDs alongside the full actual set so the next-step fix
// is obvious.
func requireAtomIDsContain(t *testing.T, label string, matches []MatchedRender, wantIDs ...string) {
	t.Helper()
	got := atomIDsOf(matches)
	have := make(map[string]bool, len(got))
	for _, id := range got {
		have[id] = true
	}
	var missing []string
	for _, w := range wantIDs {
		if !have[w] {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%s: expected atom IDs to include %v; missing %v; actual IDs: %v",
			label, wantIDs, missing, got)
	}
}

// requireAtomIDsExact asserts the result is exactly the given set
// (sorted). Use only when the scenario contract is "these atoms and no
// others" — most scenarios should use requireAtomIDsContain.
func requireAtomIDsExact(t *testing.T, label string, matches []MatchedRender, wantIDs ...string) {
	t.Helper()
	got := atomIDsOf(matches)
	want := append([]string(nil), wantIDs...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s: atom IDs mismatch\n  want: %v\n  got:  %v", label, want, got)
	}
}

func TestScenario_S1_NewProjectRecipeMatch(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	// S1 before: new project, Phase=idle, no services.
	before := StateEnvelope{
		Phase:        PhaseIdle,
		Environment:  EnvContainer,
		IdleScenario: IdleEmpty,
	}
	plan := BuildPlan(before)
	if plan.Primary.Tool != "zerops_workflow" ||
		plan.Primary.Args["action"] != "start" ||
		plan.Primary.Args["workflow"] != "bootstrap" {
		t.Errorf("S1 idle: expected primary=zerops_workflow start bootstrap, got tool=%q args=%v",
			plan.Primary.Tool, plan.Primary.Args)
	}
	matchesBefore, err := Synthesize(before, corpus)
	if err != nil {
		t.Fatalf("Synthesize idle: %v", err)
	}
	// idle-bootstrap-entry is load-bearing for an empty project — it routes
	// the agent into the bootstrap workflow.
	requireAtomIDsContain(t, "S1 idle", matchesBefore, "idle-bootstrap-entry")

	// S1 after start: bootstrap-active, Route=recipe, Step=provision.
	// Matches bootstrap_recipe_provision coverage fixture — so atoms
	// mentioning import + ACTIVE should synthesize.
	after := StateEnvelope{
		Phase:       PhaseBootstrapActive,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			TypeVersion:  "laravel@11",
			RuntimeClass: topology.RuntimeDynamic,
			Mode:         topology.ModeStandard,
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
	matchesAfter, err := Synthesize(after, corpus)
	if err != nil {
		t.Fatalf("Synthesize bootstrap-active: %v", err)
	}
	// bootstrap-recipe-import drives "use zerops_import to provision";
	// bootstrap-intro is the orienting frame for the recipe route.
	requireAtomIDsContain(t, "S1 bootstrap-active", matchesAfter,
		"bootstrap-intro", "bootstrap-recipe-import")
}

func TestScenario_S5_MixedBootstrappedAndUnmanaged(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	env := StateEnvelope{
		Phase:        PhaseIdle,
		Environment:  EnvContainer,
		IdleScenario: IdleBootstrapped,
		Services: []ServiceSnapshot{
			{
				Hostname:     "db",
				TypeVersion:  "postgresql@16",
				RuntimeClass: topology.RuntimeManaged,
				Bootstrapped: false, // managed services don't need meta
			},
			{
				Hostname:     "laraveldev",
				TypeVersion:  "php-apache@8.3",
				RuntimeClass: topology.RuntimeDynamic,
				Mode:         topology.ModeDev,
				Bootstrapped: true,
			},
			{
				Hostname:     "newruntime",
				TypeVersion:  "nodejs@22",
				RuntimeClass: topology.RuntimeDynamic,
				Bootstrapped: false, // adoptable — runtime without ServiceMeta
			},
		},
	}

	plan := BuildPlan(env)

	// Mixed bootstrapped + adoptable → primary develop, alternatives must
	// include both adopt and add-services. Matches `planIdle` bootstrapped>0
	// branch and spec §14 S5 / spec-scenarios §1.4.
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

	matches, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// Bootstrapped + adoptable mix → idle-develop-entry routes the agent
	// to the develop workflow with adopt as an alternative.
	requireAtomIDsContain(t, "S5", matches, "idle-develop-entry")
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
		Phase:        PhaseIdle,
		Environment:  EnvContainer,
		IdleScenario: IdleAdopt,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			TypeVersion:  "nodejs@22",
			RuntimeClass: topology.RuntimeDynamic,
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

	matches, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// idle-adopt-entry is the load-bearing atom for the adopt-only branch:
	// it tells the agent the adopt route attaches tracking to existing
	// services so they show as bootstrapped afterward.
	requireAtomIDsContain(t, "S3", matches, "idle-adopt-entry")
}

func TestScenario_S4_DevelopStrategyReviewAfterFirstDeploy(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	// S4: first deploy has landed (FirstDeployedAt stamped → Deployed=true)
	// but the user never confirmed an ongoing strategy. The strategy-review
	// atom fires now — before first deploy it would be premature, since the
	// first deploy always uses the default mechanism regardless of strategy.
	now := time.Now().UTC()
	env := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			TypeVersion:  "nodejs@22",
			RuntimeClass: topology.RuntimeDynamic,
			Mode:         topology.ModeDev,
			Strategy:     topology.StrategyUnset,
			Bootstrapped: true,
			Deployed:     true,
		}},
		WorkSession: &WorkSessionSummary{
			Intent:    "fix auth",
			Services:  []string{"appdev"},
			CreatedAt: now.Add(-1 * time.Minute),
		},
	}

	matches, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// develop-strategy-review is the load-bearing atom for the
	// after-first-deploy / strategy-unset gate.
	requireAtomIDsContain(t, "S4", matches, "develop-strategy-review")

	// Plan routes to deploy as long as no deploy attempt is recorded in the
	// work session. The strategy-review gate is expressed by the atom layer.
	plan := BuildPlan(env)
	if plan.Primary.Tool != "zerops_deploy" {
		t.Errorf("S4: expected primary=zerops_deploy, got tool=%q", plan.Primary.Tool)
	}
	if plan.Primary.Args["targetService"] != "appdev" {
		t.Errorf("S4: expected primary targetService=appdev, got %q", plan.Primary.Args["targetService"])
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
			RuntimeClass: topology.RuntimeDynamic,
			Mode:         topology.ModeDev,
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

	matches, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// develop-closed-auto explains the auto-complete close state and the
	// reclaim-the-slot guidance.
	requireAtomIDsContain(t, "S7", matches, "develop-closed-auto")
}

// TestScenario_S2_IdleBootstrappedReady pins the bootstrapped-only idle
// branch of planIdle: only the add-services alternative (no adopt, since
// there is nothing adoptable). Distinct from S5 which has a mixed service
// set, and from S3 which has no bootstrapped services at all.
func TestScenario_S2_IdleBootstrappedReady(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	env := StateEnvelope{
		Phase:        PhaseIdle,
		Environment:  EnvContainer,
		IdleScenario: IdleBootstrapped,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			TypeVersion:  "nodejs@22",
			RuntimeClass: topology.RuntimeDynamic,
			Mode:         topology.ModeDev,
			Bootstrapped: true,
		}},
	}

	plan := BuildPlan(env)

	if plan.Primary.Tool != "zerops_workflow" ||
		plan.Primary.Args["action"] != "start" ||
		plan.Primary.Args["workflow"] != "develop" {
		t.Errorf("S2: expected primary=start develop, got tool=%q args=%v",
			plan.Primary.Tool, plan.Primary.Args)
	}
	// Only the add-services alternative — no adopt since nothing is adoptable.
	if len(plan.Alternatives) != 1 {
		t.Fatalf("S2: expected exactly 1 alternative (add-services), got %d: %+v",
			len(plan.Alternatives), plan.Alternatives)
	}
	if plan.Alternatives[0].Label != "Add more services" {
		t.Errorf("S2: expected sole alternative 'Add more services', got %q",
			plan.Alternatives[0].Label)
	}

	matches, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// Bootstrapped-only idle → idle-develop-entry routes to develop.
	requireAtomIDsContain(t, "S2", matches, "idle-develop-entry")
}

// TestScenario_S6_DevelopDeployOKPendingVerify pins the
// deploy-succeeded/verify-not-yet branch of planDevelopActive. Branch 2
// passes (no deploy needed) and branch 3 fires (verify pending).
// TestScenario_StandardPair_FirstDeploy_PromoteToStage pins the BuildPlan
// behavior for the F#2 / pair-keyed-invariant flow: after the agent deploys +
// verifies the dev half of a container+standard pair, BuildPlan must direct
// the next deploy at the stage half. Before the ManagedRuntimeIndex
// consolidation, scope=[dev, stage] was rejected upstream and this envelope
// shape was unreachable; after the fix, scope carries both halves and plan
// dispatch exits the first-deploy branch only when both are verified.
func TestScenario_StandardPair_FirstDeploy_PromoteToStage(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	env := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{
			{
				Hostname:      "appdev",
				TypeVersion:   "nodejs@22",
				RuntimeClass:  topology.RuntimeDynamic,
				Mode:          topology.ModeStandard,
				StageHostname: "appstage",
				Strategy:      "push-dev",
				Bootstrapped:  true,
				Deployed:      true,
			},
			{
				Hostname:     "appstage",
				TypeVersion:  "nodejs@22",
				RuntimeClass: topology.RuntimeDynamic,
				Mode:         topology.ModeStage,
				Strategy:     "push-dev",
				Bootstrapped: true,
				Deployed:     false,
			},
		},
		WorkSession: &WorkSessionSummary{
			Intent:    "first deploy + promote to stage",
			Services:  []string{"appdev", "appstage"},
			CreatedAt: now.Add(-10 * time.Minute),
			Deploys: map[string][]AttemptInfo{
				"appdev": {{At: now.Add(-5 * time.Minute), Success: true, Iteration: 1}},
			},
			Verifies: map[string][]AttemptInfo{
				"appdev": {{At: now.Add(-3 * time.Minute), Success: true, Iteration: 1}},
			},
		},
	}

	plan := BuildPlan(env)

	if plan.Primary.Tool != "zerops_deploy" {
		t.Errorf("standard pair promote: expected primary=zerops_deploy, got tool=%q", plan.Primary.Tool)
	}
	if plan.Primary.Args["targetService"] != "appstage" {
		t.Errorf("standard pair promote: expected targetService=appstage, got %q",
			plan.Primary.Args["targetService"])
	}
}

func TestScenario_S6_DevelopDeployOKPendingVerify(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	env := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			TypeVersion:  "nodejs@22",
			RuntimeClass: topology.RuntimeDynamic,
			Mode:         topology.ModeDev,
			Strategy:     "push-dev",
			Bootstrapped: true,
		}},
		WorkSession: &WorkSessionSummary{
			Intent:    "add /health endpoint",
			Services:  []string{"appdev"},
			CreatedAt: now.Add(-5 * time.Minute),
			Deploys: map[string][]AttemptInfo{
				"appdev": {
					{At: now.Add(-2 * time.Minute), Success: true, Iteration: 1},
				},
			},
			// No Verifies map → needsVerify("appdev") fires after deploy ok.
		},
	}

	plan := BuildPlan(env)

	if plan.Primary.Tool != "zerops_verify" {
		t.Errorf("S6: expected primary=zerops_verify, got tool=%q", plan.Primary.Tool)
	}
	if plan.Primary.Args["serviceHostname"] != "appdev" {
		t.Errorf("S6: expected primary serviceHostname=appdev, got %q", plan.Primary.Args["serviceHostname"])
	}
}

// TestScenario_S10_RecipeActive pins the recipe-active plan. The recipe
// conductor handles its own guidance; BuildPlan only provides the iterate
// pointer so the agent can advance.
func TestScenario_S10_RecipeActive(t *testing.T) {
	t.Parallel()

	env := StateEnvelope{
		Phase:       PhaseRecipeActive,
		Environment: EnvContainer,
		Recipe: &RecipeSessionSummary{
			Slug: "laravel-minimal",
		},
	}

	plan := BuildPlan(env)
	if plan.Primary.Tool != "zerops_workflow" ||
		plan.Primary.Args["action"] != "iterate" ||
		plan.Primary.Args["workflow"] != "recipe" {
		t.Errorf("S10: expected primary=iterate recipe, got tool=%q args=%v",
			plan.Primary.Tool, plan.Primary.Args)
	}
}

// TestScenario_S11_StrategySetupEmptyPlan pins the stateless-synthesis contract
// for the strategy-setup phase: synthesis emits the push-git setup atom
// chain; Plan stays empty because the handler (handleStrategy) delivers the
// atom directly in its response, not via Plan.
//
// Under plan phase A.6 the monolithic strategy-push-git atom is replaced by
// five service-scoped atoms (intro, 2x push per env, 2x trigger per type);
// the envelope therefore carries a synthetic service snapshot reflecting
// the service being configured.
func TestScenario_S11_StrategySetupEmptyPlan(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	env := StateEnvelope{
		Phase:       PhaseStrategySetup,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{{
			Hostname:     "appdev",
			Bootstrapped: true,
			Strategy:     topology.StrategyPushGit,
			Trigger:      topology.TriggerUnset, // pre-trigger call → intro atom should fire
			Mode:         topology.ModeDev,
		}},
	}

	plan := BuildPlan(env)
	if plan.Primary.Tool != "" {
		t.Errorf("S11: expected empty Plan (stateless synthesis contract), got tool=%q", plan.Primary.Tool)
	}
	if plan.Secondary != nil || len(plan.Alternatives) != 0 {
		t.Errorf("S11: expected no secondary/alternatives, got secondary=%v alts=%d", plan.Secondary, len(plan.Alternatives))
	}

	matches, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// strategy-push-git-intro is the entry atom for the push-git setup
	// chain; downstream atoms (push/trigger per env+type) follow.
	requireAtomIDsContain(t, "S11", matches, "strategy-push-git-intro")
}

// TestScenario_S12_ExportActiveEmptyPlan mirrors S11 for the export phase.
// Same stateless contract: empty Plan, atom bodies drive guidance.
func TestScenario_S12_ExportActiveEmptyPlan(t *testing.T) {
	t.Parallel()

	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	env := StateEnvelope{
		Phase:       PhaseExportActive,
		Environment: EnvContainer,
	}

	plan := BuildPlan(env)
	if plan.Primary.Tool != "" {
		t.Errorf("S12: expected empty Plan (stateless workflow contract), got tool=%q", plan.Primary.Tool)
	}
	if plan.Secondary != nil || len(plan.Alternatives) != 0 {
		t.Errorf("S12: expected no secondary/alternatives, got secondary=%v alts=%d", plan.Secondary, len(plan.Alternatives))
	}

	matches, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// Export phase has exactly one atom — exact-match contract.
	requireAtomIDsExact(t, "S12", matches, "export")
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
			RuntimeClass: topology.RuntimeDynamic,
			Mode:         topology.ModeDev,
			Strategy:     "push-dev",
			Bootstrapped: true,
			Deployed:     true,
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

	// Last deploy failed → needsDeploy matches the host
	// and primary is deploy. Tier guidance rides along through atoms, not
	// a distinct plan branch. See spec §14 S8 / spec-scenarios §3.3.
	if plan.Primary.Tool != "zerops_deploy" {
		t.Errorf("S8 iter-3 failed: expected primary=zerops_deploy, got tool=%q", plan.Primary.Tool)
	}
	if plan.Primary.Args["targetService"] != "appdev" {
		t.Errorf("S8: expected primary targetService=appdev, got %q", plan.Primary.Args["targetService"])
	}

	matches, err := Synthesize(env, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	// Develop-active push-dev atoms are load-bearing at deploy iteration —
	// SSH mechanics and iteration tier guidance both belong to this set.
	requireAtomIDsContain(t, "S8", matches,
		"develop-push-dev-deploy-container",
		"develop-push-dev-workflow-dev")
}

// TestScenario_PinCoverage_AllAtomsReachable is the Phase 8 G2 pin-density
// closure (per `plans/atom-corpus-hygiene-2026-04-26.md` §15.3 G2). It
// synthesises against a panel of representative envelopes covering every
// phase × axis combination and asserts that every atom in the corpus is
// pinned by at least one `requireAtomIDsContain` arg.
//
// The aggregation across envelopes is the practical mechanism: the union
// of synthesise results from the panel below covers the corpus. The
// AST-based pin-density gate (`corpus_pin_density_test.go::pinnedAtomIDs`)
// scans for atom-IDs as `requireAtomIDs*` literal-string args; this test
// inventories all 79 atom IDs explicitly so the scan picks them up.
//
// When a hygiene phase deletes an atom, also remove its ID from the
// args list below so the test continues to enforce coverage of the
// post-edit corpus.
//
//nolint:maintidx // intentionally one big inventory; bulk-pin pattern is the point
func TestScenario_PinCoverage_AllAtomsReachable(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}

	// ── Envelope panel ─────────────────────────────────────────────────
	// Each entry's Synthesize result is appended to `union`. The post-edit
	// `requireAtomIDsContain` covers every atom expected on at least one of
	// these envelopes.

	envelopes := []struct {
		label string
		env   StateEnvelope
	}{
		// Idle scenarios — entry atoms.
		{"idle/empty", StateEnvelope{Phase: PhaseIdle, Environment: EnvContainer, IdleScenario: IdleEmpty}},
		{"idle/bootstrapped", StateEnvelope{Phase: PhaseIdle, Environment: EnvContainer, IdleScenario: IdleBootstrapped}},
		{"idle/adopt", StateEnvelope{Phase: PhaseIdle, Environment: EnvContainer, IdleScenario: IdleAdopt}},
		{"idle/incomplete", StateEnvelope{Phase: PhaseIdle, Environment: EnvContainer, IdleScenario: IdleIncomplete}},

		// Bootstrap routes × steps × environments.
		{"bootstrap/classic/discover/dynamic/container", StateEnvelope{
			Phase: PhaseBootstrapActive, Environment: EnvContainer,
			Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepDiscover},
			Services:  []ServiceSnapshot{{Hostname: "app", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Bootstrapped: true}},
		}},
		{"bootstrap/classic/discover/static/container", StateEnvelope{
			Phase: PhaseBootstrapActive, Environment: EnvContainer,
			Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepDiscover},
			Services:  []ServiceSnapshot{{Hostname: "app", TypeVersion: "static", RuntimeClass: topology.RuntimeStatic, Mode: topology.ModeStandard, Bootstrapped: true}},
		}},
		{"bootstrap/classic/discover/local", StateEnvelope{
			Phase: PhaseBootstrapActive, Environment: EnvLocal,
			Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepDiscover},
			Services:  []ServiceSnapshot{{Hostname: "app", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Bootstrapped: true}},
		}},
		{"bootstrap/recipe/match", StateEnvelope{
			Phase: PhaseBootstrapActive, Environment: EnvContainer,
			Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteRecipe, Step: StepDiscover},
			Services:  []ServiceSnapshot{{Hostname: "app", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Bootstrapped: true}},
		}},
		{"bootstrap/adopt/discover", StateEnvelope{
			Phase: PhaseBootstrapActive, Environment: EnvContainer,
			Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteAdopt, Step: StepDiscover},
			Services:  []ServiceSnapshot{{Hostname: "app", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Bootstrapped: true}},
		}},
		{"bootstrap/classic/provision/container", StateEnvelope{
			Phase: PhaseBootstrapActive, Environment: EnvContainer,
			Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepProvision},
			Services:  []ServiceSnapshot{{Hostname: "app", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Bootstrapped: true}},
		}},
		{"bootstrap/classic/provision/local", StateEnvelope{
			Phase: PhaseBootstrapActive, Environment: EnvLocal,
			Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepProvision},
			Services:  []ServiceSnapshot{{Hostname: "app", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Bootstrapped: true}},
		}},
		{"bootstrap/classic/close", StateEnvelope{
			Phase: PhaseBootstrapActive, Environment: EnvContainer,
			Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteClassic, Step: StepClose},
			Services:  []ServiceSnapshot{{Hostname: "app", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Bootstrapped: true}},
		}},
		{"bootstrap/resume/idle", StateEnvelope{Phase: PhaseIdle, Environment: EnvContainer, IdleScenario: IdleIncomplete}},
		{"bootstrap/recipe/close", StateEnvelope{
			Phase: PhaseBootstrapActive, Environment: EnvContainer,
			Bootstrap: &BootstrapSessionSummary{Route: BootstrapRouteRecipe, Step: StepClose},
			Services:  []ServiceSnapshot{{Hostname: "app", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Bootstrapped: true}},
		}},

		// Develop-active first-deploy.
		{"develop-active/first-deploy/standard/container", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
				{Hostname: "appstage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
			},
		}},
		{"develop-active/first-deploy/implicit-webserver", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "php-nginx@8.4", RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeStandard, StageHostname: "appstage", Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
				{Hostname: "appstage", TypeVersion: "php-nginx@8.4", RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeStage, Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
			},
		}},
		{"develop-active/first-deploy/local", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvLocal,
			Services: []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
				{Hostname: "appstage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
			},
		}},

		// Develop-active deployed iterations across modes/strategies/triggers.
		{"develop-active/push-dev/dev/container", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev, Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true, Status: "READY_TO_DEPLOY"}},
		}},
		{"develop-active/push-dev/simple/container", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "go@1.22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple, Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true}},
		}},
		{"develop-active/push-dev/standard/container", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true}},
		}},
		{"develop-active/push-dev/dev/local", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvLocal,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeLocalStage, Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true}},
		}},
		{"develop-active/push-dev/local-mode-dev-deployed", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvLocal,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev, Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true}},
		}},
		{"develop-active/push-dev/standard/local-deployed", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvLocal,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true}},
		}},
		{"develop-active/push-git/standard/local-deployed", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvLocal,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Strategy: topology.StrategyPushGit, Trigger: topology.TriggerWebhook, Bootstrapped: true, Deployed: true}},
		}},
		{"develop-active/first-deploy/implicit-webserver-local", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvLocal,
			Services: []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "php-nginx@8.4", RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeStandard, StageHostname: "appstage", Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
				{Hostname: "appstage", TypeVersion: "php-nginx@8.4", RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeStage, Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
			},
		}},
		{"develop-active/push-git/standard/container", StateEnvelope{
			// Two-snapshot pair (dev + stage) per deploy-decomp P3 §G5 ship
			// gate. Future close-mode-git-push atom (Phase 8, modes:
			// [standard, simple, local-stage, local-only]) renders ONCE for
			// the dev-half hostname; the stage-half (Mode=stage) does not
			// match. Without the pair fixture, single-render regressions
			// could re-introduce the original P1 double-render bug.
			//
			// BuildIntegration=webhook + Deployed=true makes this fixture
			// also pin develop-record-external-deploy (deploy-decomp P7
			// atom for record-deploy bridge guidance — frontmatter
			// buildIntegrations: [webhook, actions]).
			Phase: PhaseDevelopActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Strategy: topology.StrategyPushGit, Trigger: topology.TriggerWebhook, CloseDeployMode: topology.CloseModeGitPush, GitPushState: topology.GitPushConfigured, BuildIntegration: topology.BuildIntegrationWebhook, Bootstrapped: true, Deployed: true},
				{Hostname: "appstage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Strategy: topology.StrategyPushGit, Trigger: topology.TriggerWebhook, CloseDeployMode: topology.CloseModeGitPush, GitPushState: topology.GitPushConfigured, BuildIntegration: topology.BuildIntegrationWebhook, Bootstrapped: true, Deployed: true},
			},
		}},
		{"develop-active/manual/dev/container", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev, Strategy: topology.StrategyManual, Bootstrapped: true, Deployed: true}},
		}},
		{"develop-active/static/standard/container", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "static", RuntimeClass: topology.RuntimeStatic, Mode: topology.ModeStandard, Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true}},
		}},
		{"develop-active/implicit-webserver/standard/container", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "php-nginx@8.4", RuntimeClass: topology.RuntimeImplicitWeb, Mode: topology.ModeStandard, Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true}},
		}},
		{"develop-active/standard-pair-deployed", StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{
				{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true},
				{Hostname: "appstage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true},
			},
		}},

		// Develop-closed-auto.
		{"develop-closed-auto", StateEnvelope{Phase: PhaseDevelopClosed, Environment: EnvContainer}},

		// Strategy-setup.
		{"strategy-setup/push-git/unset/container", StateEnvelope{
			Phase: PhaseStrategySetup, Environment: EnvContainer,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Strategy: topology.StrategyPushGit, Trigger: topology.TriggerUnset, Bootstrapped: true, Deployed: false}},
		}},
		{"strategy-setup/push-git/actions/container", StateEnvelope{
			Phase: PhaseStrategySetup, Environment: EnvContainer,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Strategy: topology.StrategyPushGit, Trigger: topology.TriggerActions, Bootstrapped: true, Deployed: false}},
		}},
		{"strategy-setup/push-git/webhook/container", StateEnvelope{
			Phase: PhaseStrategySetup, Environment: EnvContainer,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Strategy: topology.StrategyPushGit, Trigger: topology.TriggerWebhook, Bootstrapped: true, Deployed: false}},
		}},
		{"strategy-setup/push-git/local", StateEnvelope{
			Phase: PhaseStrategySetup, Environment: EnvLocal,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Strategy: topology.StrategyPushGit, Trigger: topology.TriggerWebhook, Bootstrapped: true, Deployed: false}},
		}},

		// Export.
		{"export-active", StateEnvelope{
			Phase: PhaseExportActive, Environment: EnvContainer,
			Services: []ServiceSnapshot{{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: true}},
		}},
	}

	var union []MatchedRender
	for _, e := range envelopes {
		matches, err := Synthesize(e.env, corpus)
		if err != nil {
			t.Errorf("Synthesize(%s): %v", e.label, err)
			continue
		}
		union = append(union, matches...)
	}

	// Pin every atom that's currently in the `knownUnpinnedAtoms` allowlist
	// (per `corpus_pin_density_test.go`). When this passes, those atoms are
	// no longer "unpinned" — they appear as args to a `requireAtomIDsContain`
	// call (this one), which the AST-based pin-density gate counts.
	requireAtomIDsContain(t, "Phase 8 G2 pin-coverage closure", union,
		// bootstrap-* (16 atoms)
		"bootstrap-adopt-discover",
		"bootstrap-classic-plan-dynamic",
		"bootstrap-classic-plan-static",
		"bootstrap-close",
		"bootstrap-discover-local",
		"bootstrap-env-var-discovery",
		"bootstrap-mode-prompt",
		"bootstrap-provision-local",
		"bootstrap-provision-rules",
		"bootstrap-recipe-close",
		"bootstrap-recipe-match",
		"bootstrap-resume",
		"bootstrap-route-options",
		"bootstrap-runtime-classes",
		"bootstrap-verify",
		"bootstrap-wait-active",
		// develop-* (47 atoms; some pinned already in earlier scenarios)
		"develop-api-error-meta",
		"develop-auto-close-semantics",
		"develop-change-drives-deploy",
		"develop-checklist-dev-mode",
		"develop-checklist-simple-mode",
		"develop-close-manual",
		"develop-close-push-dev-dev",
		"develop-close-push-dev-local",
		"develop-close-push-dev-simple",
		"develop-close-push-dev-standard",
		"develop-close-push-git-container",
		"develop-close-push-git-local",
		"develop-deploy-files-self-deploy",
		"develop-deploy-modes",
		"develop-dev-server-reason-codes",
		"develop-dev-server-triage",
		"develop-dynamic-runtime-start-container",
		"develop-dynamic-runtime-start-local",
		"develop-env-var-channels",
		"develop-first-deploy-asset-pipeline-container",
		"develop-first-deploy-asset-pipeline-local",
		"develop-first-deploy-env-vars",
		"develop-first-deploy-execute",
		"develop-first-deploy-intro",
		"develop-first-deploy-promote-stage",
		"develop-first-deploy-scaffold-yaml",
		"develop-first-deploy-verify",
		"develop-first-deploy-write-app",
		"develop-http-diagnostic",
		"develop-implicit-webserver",
		"develop-intro",
		"develop-knowledge-pointers",
		"develop-local-workflow",
		"develop-manual-deploy",
		"develop-mode-expansion",
		"develop-platform-rules-common",
		"develop-platform-rules-container",
		"develop-platform-rules-local",
		"develop-push-dev-deploy-local",
		"develop-push-dev-workflow-simple",
		"develop-push-git-deploy",
		"develop-push-git-deploy-local",
		"develop-ready-to-deploy",
		"develop-record-external-deploy", // deploy-decomp P7

		"develop-static-workflow",
		"develop-strategy-awareness",
		"develop-verify-matrix",
		// strategy-* (4 unpinned atoms)
		"strategy-push-git-push-container",
		"strategy-push-git-push-local",
		"strategy-push-git-trigger-actions",
		"strategy-push-git-trigger-webhook",
	)
}
