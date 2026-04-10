package workflow

import (
	"strings"
	"testing"
)

// testHelloWorldPlan returns a hello-world tier plan. The recipe system
// treats hello-world as a slug-suffix form of minimal tier — there's no
// dedicated tier constant — so this helper exists only in tests.
func testHelloWorldPlan() *RecipePlan {
	return &RecipePlan{
		Framework:   "nodejs",
		Tier:        RecipeTierMinimal,
		Slug:        "nodejs-hello-world",
		RuntimeType: "nodejs@22",
		Research: ResearchData{
			ServiceType:    "nodejs",
			PackageManager: "npm",
			HTTPPort:       3000,
			BuildCommands:  []string{"npm ci"},
			DeployFiles:    []string{"."},
			StartCommand:   "node server.js",
		},
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "nodejs@22"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
}

// TestResolveRecipeGuidance_Generate_ShowcaseUnderSizeCap guards against
// the generate step bloating back to the 49KB+ that forced the v7 agent
// to pipe the tool response through jq+python+fold just to read it.
// The cap is set to 40KB — well below the prior 49KB but with enough
// slack for future rule additions before another cleanup is needed.
func TestResolveRecipeGuidance_Generate_ShowcaseUnderSizeCap(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()
	guide := resolveRecipeGuidance(RecipeStepGenerate, RecipeTierShowcase, plan)

	const sizeCap = 40 * 1024
	if len(guide) == 0 {
		t.Fatal("showcase generate guide is empty")
	}
	if len(guide) > sizeCap {
		t.Errorf("showcase generate guide is %d bytes, cap is %d bytes — cleanup regressed", len(guide), sizeCap)
	}
}

// TestResolveRecipeGuidance_Generate_HelloWorldDramaticallySmaller locks
// in the practical benefit of tier-gating: a hello-world guide must be
// substantially smaller than a showcase guide, because hello-world skips
// both the dashboard spec and the fragment writing deep-dive. The
// threshold is set to the compressed generate section's byte count plus
// slack — any regression that re-injects the gated blocks for hello-world
// will blow through this cap.
func TestResolveRecipeGuidance_Generate_HelloWorldDramaticallySmaller(t *testing.T) {
	t.Parallel()

	plan := testHelloWorldPlan()
	guide := resolveRecipeGuidance(RecipeStepGenerate, RecipeTierMinimal, plan)

	// Hello-world should be under 30KB — the compressed generate section
	// itself is ~26KB, leaving ~4KB of headroom for future growth.
	const helloCap = 30 * 1024
	if len(guide) == 0 {
		t.Fatal("hello-world generate guide is empty")
	}
	if len(guide) > helloCap {
		t.Errorf("hello-world generate guide is %d bytes, cap is %d bytes — a gated block probably leaked back in", len(guide), helloCap)
	}
}

// TestResolveRecipeGuidance_Generate_HelloWorldSkipsDashboardSpec asserts
// that hello-world plans do NOT receive the dashboard implementation spec.
// Hello-world has no feature-section dashboard — it has a single endpoint
// that returns the framework name, and carrying the 12KB dashboard spec
// for it wastes context every time.
func TestResolveRecipeGuidance_Generate_HelloWorldSkipsDashboardSpec(t *testing.T) {
	t.Parallel()

	plan := testHelloWorldPlan()
	guide := resolveRecipeGuidance(RecipeStepGenerate, RecipeTierMinimal, plan)

	if guide == "" {
		t.Fatal("hello-world generate guide is empty")
	}
	// The dashboard spec section anchors are unambiguous strings — if any
	// of them appear, the dashboard spec leaked into a hello-world guide.
	leaks := []string{
		"### Required endpoints",
		"### Dashboard style",
		"### Showcase dashboard — file architecture",
		"### Asset pipeline consistency",
	}
	for _, anchor := range leaks {
		if strings.Contains(guide, anchor) {
			t.Errorf("hello-world generate guide contains %q — dashboard spec should be gated out for hello-world slugs", anchor)
		}
	}
}

// TestResolveRecipeGuidance_Generate_HelloWorldSkipsFragmentsDeepDive asserts
// that hello-world plans do NOT receive the README fragment writing-style
// deep-dive. Hello-world recipes have a simple 1-section README that the
// chain recipe demonstrates in full — the 6KB deep-dive is dead weight.
func TestResolveRecipeGuidance_Generate_HelloWorldSkipsFragmentsDeepDive(t *testing.T) {
	t.Parallel()

	plan := testHelloWorldPlan()
	guide := resolveRecipeGuidance(RecipeStepGenerate, RecipeTierMinimal, plan)

	// The deep-dive's unambiguous anchor is the H2 heading that only exists
	// inside the generate-fragments section.
	if strings.Contains(guide, "## Fragment Quality Requirements") {
		t.Error("hello-world generate guide contains 'Fragment Quality Requirements' — generate-fragments should be gated out for hello-world slugs")
	}
}

// TestResolveRecipeGuidance_Generate_ShowcaseKeepsDashboardAndFragments
// is the positive counterpart: showcase plans MUST still receive both the
// dashboard spec and the fragment deep-dive. Without this test the
// tier-gate could silently degrade showcase coverage.
func TestResolveRecipeGuidance_Generate_ShowcaseKeepsDashboardAndFragments(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()
	guide := resolveRecipeGuidance(RecipeStepGenerate, RecipeTierShowcase, plan)

	if !strings.Contains(guide, "### Required endpoints") {
		t.Error("showcase generate guide missing 'Required endpoints' — dashboard spec dropped incorrectly")
	}
	if !strings.Contains(guide, "### Dashboard style") {
		t.Error("showcase generate guide missing 'Dashboard style' — dashboard spec dropped incorrectly")
	}
	if !strings.Contains(guide, "## Fragment Quality Requirements") {
		t.Error("showcase generate guide missing 'Fragment Quality Requirements' — generate-fragments dropped incorrectly")
	}
}

// TestResolveRecipeGuidance_ResearchShowcase_NoFrameworkHardcoding guards
// against recipe.md regressing to framework-specific worker-decision
// examples. The old guidance listed four specific framework+queue-library
// pairings (Laravel+Horizon, Rails+Sidekiq, Django+Celery, NestJS+BullMQ)
// which nudged the agent toward the listed answer instead of applying the
// underlying rule. The rule must be principle-based; examples that ground
// terms (full-stack/Blade, API-first) are fine — leading examples that
// resolve the decision are not.
func TestResolveRecipeGuidance_ResearchShowcase_NoFrameworkHardcoding(t *testing.T) {
	t.Parallel()

	guide := resolveRecipeGuidance(RecipeStepResearch, RecipeTierShowcase, nil)

	// Forbidden strings: each is a concrete framework+library pairing that
	// prescribes a shared-codebase answer. If any re-appears, the principle
	// rule has been diluted again.
	forbidden := []string{
		"Laravel + Horizon",
		"Rails + Sidekiq",
		"Django + Celery",
		"NestJS + BullMQ",
		"same-repo processor",
		"nest start",
		"rails runner",
		"python manage.py",
	}
	for _, needle := range forbidden {
		if strings.Contains(guide, needle) {
			t.Errorf("research-showcase guide contains framework-hardcoded decision leader %q — decision must be principle-based", needle)
		}
	}
}

// TestResolveRecipeGuidance_Generate_NoFrameworkPortHardcoding guards
// against the specific NestJS port hint ("3000 is NestJS default") that
// appeared twice in the old generate section. Port numbers that match
// a specific framework's default push the agent toward that framework
// as a reference implementation — they should always be documented as
// generic "substitute your API's actual HTTP port" instructions.
func TestResolveRecipeGuidance_Generate_NoFrameworkPortHardcoding(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()
	guide := resolveRecipeGuidance(RecipeStepGenerate, RecipeTierShowcase, plan)

	forbidden := []string{
		"is the NestJS default",
		"NestJS default",
	}
	for _, needle := range forbidden {
		if strings.Contains(guide, needle) {
			t.Errorf("generate guide contains framework-hardcoded port hint %q — ports must be described generically", needle)
		}
	}
}

// TestResolveRecipeGuidance_Generate_NoFrameworkWorkerRuleThumb guards
// against the rule-of-thumb list of framework CLI names that used to
// appear in the worker codebase decision section. These names (`artisan`,
// `rails runner`, `python manage.py`, `nest start`) resolved the decision
// instead of teaching the principle.
func TestResolveRecipeGuidance_Generate_NoFrameworkWorkerRuleThumb(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()
	guide := resolveRecipeGuidance(RecipeStepGenerate, RecipeTierShowcase, plan)

	// The generate section itself no longer discusses the shared/separate
	// decision — that lives in research-showcase. If a future edit adds
	// framework-named worker CLIs back into generate, catch it.
	forbidden := []string{
		"artisan horizon",
		"bundle exec sidekiq",
		"nest start",
	}
	for _, needle := range forbidden {
		if strings.Contains(guide, needle) {
			t.Errorf("generate guide contains framework CLI %q — worker decision must stay principle-based", needle)
		}
	}
}
