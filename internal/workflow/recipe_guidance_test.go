package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
)

// RecipeShape identifies a named fixture plan used by the cap sweep and the
// audit composition harness. Each shape represents a different delivery-
// relevant plan geometry: hello-world (narrow, no chain predecessor), backend
// minimal (full-stack single runtime), full-stack showcase (shared-codebase
// worker + full managed stack), dual-runtime showcase (separate frontend +
// API + separate-codebase worker).
type RecipeShape int

const (
	ShapeHelloWorld          RecipeShape = iota // nodejs-hello-world — tier 0, 2 services
	ShapeBackendMinimal                         // laravel-minimal    — tier 1, full-stack, no worker
	ShapeFullStackShowcase                      // laravel-showcase   — tier 2, full-stack + worker + 5 managed
	ShapeDualRuntimeShowcase                    // nestjs-showcase    — tier 2, API-first, separate worker codebase
)

// fixtureForShape returns a RecipePlan matching the named shape. The fixtures
// here are the single source of truth for the cap sweep and audit harness —
// changing one changes what every subsequent phase measures against. Keep
// them representative of real-world recipes of each tier.
func fixtureForShape(s RecipeShape) *RecipePlan {
	switch s {
	case ShapeHelloWorld:
		return &RecipePlan{
			Slug:        "nodejs-hello-world",
			Framework:   "nodejs",
			RuntimeType: "nodejs@22",
			Tier:        RecipeTierHelloWorld,
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
	case ShapeBackendMinimal:
		return &RecipePlan{
			Slug:        "laravel-minimal",
			Framework:   "laravel",
			RuntimeType: "php-nginx@8.3",
			Tier:        RecipeTierMinimal,
			Research: ResearchData{
				ServiceType:    "php-nginx",
				PackageManager: "composer",
				HTTPPort:       80,
				BuildCommands:  []string{"composer install"},
				DeployFiles:    []string{"."},
				StartCommand:   "php-fpm",
			},
			Targets: []RecipeTarget{
				{Hostname: "app", Type: "php-nginx@8.3"},
				{Hostname: "db", Type: "postgresql@17"},
			},
		}
	case ShapeFullStackShowcase:
		return &RecipePlan{
			Slug:        "laravel-showcase",
			Framework:   "laravel",
			RuntimeType: "php-nginx@8.3",
			Tier:        RecipeTierShowcase,
			Research: ResearchData{
				ServiceType:    "php-nginx",
				PackageManager: "composer",
				HTTPPort:       80,
				BuildCommands:  []string{"composer install", "npm ci", "npm run build"},
				DeployFiles:    []string{"."},
				StartCommand:   "php-fpm",
				CacheLib:       "redis",
				SessionDriver:  "redis",
				QueueDriver:    "horizon",
				StorageDriver:  "s3",
				SearchLib:      "meilisearch",
			},
			Targets: []RecipeTarget{
				{Hostname: "app", Type: "php-nginx@8.3"},
				{Hostname: "worker", Type: "php@8.3", IsWorker: true, SharesCodebaseWith: "app"},
				{Hostname: "db", Type: "postgresql@17"},
				{Hostname: "cache", Type: "valkey@8"},
				{Hostname: "queue", Type: "nats@2.12"},
				{Hostname: "storage", Type: "object-storage"},
				{Hostname: "search", Type: "meilisearch@1.10"},
			},
		}
	case ShapeDualRuntimeShowcase:
		plan := testDualRuntimePlan()
		// Match the reshuffled default for API-first: separate worker codebase.
		for i := range plan.Targets {
			if plan.Targets[i].IsWorker {
				plan.Targets[i].SharesCodebaseWith = ""
			}
		}
		return plan
	}
	return nil
}

// showcaseStepCaps are the per-shape, per-step byte caps for the recipe
// detailedGuide, set in Phase 11 from measured post-P10 numbers +
// ~1.5-2 KB headroom per cell. Each column is non-decreasing from narrow
// (hello-world) to wide (dual-runtime-showcase) — the monotonicity
// invariant test below enforces the caps are consistent with the fact
// that wider shapes legitimately carry more content.
//
// Measured numbers (post-Phase-10, before these caps landed):
//
//	shape                  research provision generate deploy finalize close
//	hello-world            2.7      16.1      16.1     18.2   15.5     12.0 KB
//	backend-minimal        2.7      16.1      23.8     18.2   15.5     12.0
//	fullstack-showcase     8.2      16.1      35.2     33.6   15.5     12.0
//	dual-runtime-showcase  8.2      17.1      39.5     33.6   15.5     12.0
//
// Regression guard: if a predicate accidentally fires on hello-world, its
// cap blows. If a new block is added without its predicate, its cap
// blows. If content grows >1.5 KB in a section, its cap blows.
//
// research cap bumps (post v18 — Features schema work): the feature
// declaration guidance added ~1 KB to research-minimal and ~2 KB to
// research-showcase (Features contract explainer + example). Caps
// were raised from 5→7 KB (narrow) and 10→13 KB (wide). Other sections
// gained smaller amounts (client-code-observable-failure,
// init-script-loud-failure, feature-sweep blocks) — generate and
// deploy caps bumped +2 KB each.
var showcaseStepCaps = map[RecipeShape]map[string]int{
	ShapeHelloWorld: {
		RecipeStepResearch: 7 * 1024,
		// v8.96 +1 KB: git-config-mount block adds the "every agent runs
		// git container-side" framing + .git/index.lock concurrency rule
		// (load-bearing — closes the v31 ~90s git-lock cost driver).
		// v8.104 +1 KB: post-scaffold re-init rule — names that the
		// scaffold subagent deletes /var/www/.git/ before returning and
		// main-agent therefore re-runs git init before each post-
		// scaffold commit. Closes the v33 `fatal: not a git repository`
		// ~6s cost per run and the sequencing gap it exposed.
		RecipeStepProvision: 20 * 1024,
		RecipeStepGenerate:  24 * 1024,
		RecipeStepDeploy:    26 * 1024,
		RecipeStepFinalize:  18 * 1024,
		RecipeStepClose:     14 * 1024,
	},
	ShapeBackendMinimal: {
		RecipeStepResearch:  7 * 1024,
		RecipeStepProvision: 20 * 1024, // v8.96 +1 KB; v8.104 +1 KB — see ShapeHelloWorld note
		RecipeStepGenerate:  32 * 1024,
		RecipeStepDeploy:    26 * 1024,
		RecipeStepFinalize:  18 * 1024,
		RecipeStepClose:     14 * 1024,
	},
	ShapeFullStackShowcase: {
		// v8.100 +1 KB: research-minimal gained the top-level-fields list
		// (v8.99) + object-vs-string submission note (v8.100). Both are
		// doc fixes that prevent first-call schema rejections observed
		// in the wild, worth the headroom.
		RecipeStepResearch:  15 * 1024,
		RecipeStepProvision: 20 * 1024, // v8.96 +1 KB; v8.104 +1 KB — see ShapeHelloWorld note
		RecipeStepGenerate:  45 * 1024,
		RecipeStepDeploy:    44 * 1024,
		RecipeStepFinalize:  18 * 1024,
		RecipeStepClose:     14 * 1024,
	},
	ShapeDualRuntimeShowcase: {
		RecipeStepResearch:  15 * 1024, // v8.100 +1 KB — see ShapeFullStackShowcase note
		RecipeStepProvision: 23 * 1024,
		RecipeStepGenerate:  48 * 1024,
		RecipeStepDeploy:    44 * 1024,
		RecipeStepFinalize:  18 * 1024,
		RecipeStepClose:     14 * 1024,
	},
}

// advanceShowcaseStateTo returns a RecipeState with steps [0..step-1] marked
// complete and `step` in progress. Plan, discoveredEnvVars, and outputDir are
// populated as they would be at that point in a real showcase run.
func advanceShowcaseStateTo(step string, plan *RecipePlan) *RecipeState {
	rs := NewRecipeState()
	rs.Tier = RecipeTierShowcase
	rs.Plan = plan
	stepOrder := []string{
		RecipeStepResearch,
		RecipeStepProvision,
		RecipeStepGenerate,
		RecipeStepDeploy,
		RecipeStepFinalize,
		RecipeStepClose,
	}
	for i, s := range stepOrder {
		if s == step {
			rs.CurrentStep = i
			rs.Steps[i].Status = stepInProgress
			if i >= 2 { // env vars discovered at provision completion
				rs.DiscoveredEnvVars = realisticDiscoveredEnvs()
			}
			if i >= 4 { // outputDir exists by finalize
				rs.OutputDir = "/tmp/zcprecipator/nestjs-showcase"
			}
			return rs
		}
		rs.Steps[i].Status = stepComplete
		rs.Steps[i].Attestation = "test fixture: " + s + " done"
	}
	return rs
}

// realisticDiscoveredEnvs mirrors what zerops_discover returns for a full
// showcase stack (db + cache + queue + storage + search).
func realisticDiscoveredEnvs() map[string][]string {
	return map[string][]string{
		"db":      {"hostname", "port", "user", "password", "dbName", "connectionString"},
		"cache":   {"hostname", "port", "password", "connectionString"},
		"queue":   {"hostname", "port", "user", "password", "connectionString"},
		"storage": {"apiHost", "apiUrl", "accessKeyId", "secretAccessKey", "bucketName"},
		"search":  {"hostname", "port", "masterKey", "defaultAdminKey", "defaultSearchKey"},
	}
}

// TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap is the correctness gate
// on assembled guide size across every named fixture shape. It builds the
// full detailedGuide for every step (including chain-recipe injection via
// the real embedded knowledge store) and asserts each shape/step combo is
// under its target cap.
//
// Replaces the old single-fixture sweep — a flat cap for "the showcase" test
// could not see narrow-recipe regressions where a predicate accidentally
// fires on hello-world. The Phase 11 refactor adds a monotonicity assertion
// on top of this (narrower ≤ wider) as a predicate bug guard.
func TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap(t *testing.T) {
	t.Parallel()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("embedded store: %v", err)
	}
	shapes := []struct {
		name  string
		shape RecipeShape
	}{
		{"hello-world", ShapeHelloWorld},
		{"backend-minimal", ShapeBackendMinimal},
		{"fullstack-showcase", ShapeFullStackShowcase},
		{"dual-runtime-showcase", ShapeDualRuntimeShowcase},
	}
	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			t.Parallel()
			plan := fixtureForShape(sh.shape)
			caps := showcaseStepCaps[sh.shape]
			for step, capVal := range caps {
				t.Run(step, func(t *testing.T) {
					t.Parallel()
					rs := advanceShowcaseStateTo(step, plan)
					resp := rs.BuildResponse("sess-"+sh.name+"-"+step, "Create a "+sh.name+" recipe", 0, EnvLocal, store)
					if resp.Current == nil {
						t.Fatalf("no Current on response")
					}
					guide := resp.Current.DetailedGuide
					if len(guide) == 0 {
						t.Fatalf("empty detailedGuide")
					}
					if len(guide) > capVal {
						t.Errorf("shape %q step %q detailedGuide = %d bytes (%.1f KB), cap = %d bytes (%.0f KB)",
							sh.name, step, len(guide), float64(len(guide))/1024, capVal, float64(capVal)/1024)
					}
				})
			}
		})
	}
}

// TestRecipe_DetailedGuide_MonotonicityInvariant enforces that each step's
// static guidance size is non-decreasing as shapes widen:
//
//	hello-world ≤ backend-minimal ≤ fullstack-showcase ≤ dual-runtime-showcase
//
// Phase A: skeleton steps (generate, deploy, finalize, close) are tested on
// resolveRecipeGuidance (the skeleton + predicate filtering) rather than the
// full BuildResponse (which includes knowledge injection). Knowledge injection
// is shape-dependent and not necessarily monotonic — e.g. fullstack-showcase
// has more discovered env vars than dual-runtime-showcase, but dual-runtime
// has more topic markers in the skeleton. The skeleton's predicate filtering
// is what monotonicity guards: a predicate bug that fires on the wrong shape
// breaks the invariant.
//
// Non-skeleton steps (research, provision) are still tested on the full
// BuildResponse because their guide IS the full composed content.
func TestRecipe_DetailedGuide_MonotonicityInvariant(t *testing.T) {
	t.Parallel()
	store, err := knowledge.GetEmbeddedStore()
	if err != nil {
		t.Fatalf("embedded store: %v", err)
	}
	shapes := []struct {
		name  string
		shape RecipeShape
	}{
		{"hello-world", ShapeHelloWorld},
		{"backend-minimal", ShapeBackendMinimal},
		{"fullstack-showcase", ShapeFullStackShowcase},
		{"dual-runtime-showcase", ShapeDualRuntimeShowcase},
	}

	// Skeleton steps: check monotonicity on resolveRecipeGuidance only.
	skeletonSteps := map[string]bool{
		RecipeStepGenerate: true, RecipeStepDeploy: true,
		RecipeStepFinalize: true, RecipeStepClose: true,
	}

	steps := []string{
		RecipeStepResearch, RecipeStepProvision, RecipeStepGenerate,
		RecipeStepDeploy, RecipeStepFinalize, RecipeStepClose,
	}
	for _, step := range steps {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			sizes := make([]int, len(shapes))
			for i, sh := range shapes {
				plan := fixtureForShape(sh.shape)
				if skeletonSteps[step] {
					// Skeleton: measure static guidance only.
					guide := resolveRecipeGuidance(step, plan.Tier, plan)
					sizes[i] = len(guide)
				} else {
					// Non-skeleton: measure full BuildResponse.
					rs := advanceShowcaseStateTo(step, plan)
					resp := rs.BuildResponse("sess-mono-"+sh.name+"-"+step, "m", 0, EnvLocal, store)
					if resp.Current == nil {
						t.Fatalf("%s: no Current on response", sh.name)
					}
					sizes[i] = len(resp.Current.DetailedGuide)
				}
			}
			for i := 1; i < len(shapes); i++ {
				if sizes[i] < sizes[i-1] {
					t.Errorf("monotonicity violated at step %q: %s=%d > %s=%d (wider shape must have ≥ content)",
						step, shapes[i-1].name, sizes[i-1], shapes[i].name, sizes[i])
				}
			}
		})
	}
}

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

// TestResolveRecipeGuidance_Generate_HelloWorldSmaller locks in the benefit
// of tier-gating: a hello-world guide must be substantially smaller than a
// showcase guide. Hello-world skips the fragment writing deep-dive. The
// threshold is set to the generate section's byte count plus slack — any
// regression that re-injects the gated block for hello-world blows through
// this cap. Post-reshuffle the cap is 32 KB because Phase 4 inlined the
// dashboard skeleton-write table into generate (~1.5 KB) and Phase 8 added
// the dev-server env var rule (~1.4 KB).
func TestResolveRecipeGuidance_Generate_HelloWorldSmaller(t *testing.T) {
	t.Parallel()

	plan := testHelloWorldPlan()
	guide := resolveRecipeGuidance(RecipeStepGenerate, RecipeTierMinimal, plan)

	const helloCap = 32 * 1024
	if len(guide) == 0 {
		t.Fatal("hello-world generate guide is empty")
	}
	if len(guide) > helloCap {
		t.Errorf("hello-world generate guide is %d bytes, cap is %d bytes — a gated block probably leaked back in", len(guide), helloCap)
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

// TestResolveRecipeGuidance_Generate_ShowcaseKeepsFragments verifies that
// showcase plans receive the skeleton with showcase-gated topic markers
// (dashboard-skeleton, recipe-types). Phase A: the fragment deep-dive is
// no longer inlined — it lives in a topic block fetched via zerops_guidance.
func TestResolveRecipeGuidance_Generate_ShowcaseKeepsFragments(t *testing.T) {
	t.Parallel()

	plan := testShowcasePlan()
	guide := resolveRecipeGuidance(RecipeStepGenerate, RecipeTierShowcase, plan)

	// With skeletons: check that showcase-gated topic markers are present.
	if !strings.Contains(guide, "[topic: dashboard-skeleton]") {
		t.Error("showcase generate skeleton missing '[topic: dashboard-skeleton]'")
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

// TestRecipeGenerate_HelloWorld_OmitsShowcaseBlocks asserts that hello-world
// plans do not receive gated topic markers in the generate skeleton.
// Phase A: with skeletons, this checks that predicate-gated [topic: ...]
// markers are filtered out for hello-world.
func TestRecipeGenerate_HelloWorld_OmitsShowcaseBlocks(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeHelloWorld)
	guide := resolveRecipeGuidance(RecipeStepGenerate, plan.Tier, plan)
	// Gated topic markers that should be removed for hello-world:
	for _, shouldNotContain := range []string{
		"[topic: dual-runtime-urls]",
		"[topic: dashboard-skeleton]",
		"[topic: worker-setup]",
		"[topic: serve-only-dev]",
	} {
		if strings.Contains(guide, shouldNotContain) {
			t.Errorf("hello-world generate skeleton contains %q, should be omitted", shouldNotContain)
		}
	}
}

// TestRecipeGenerate_BackendMinimal_OmitsDualRuntimeContent asserts that
// single-runtime minimal recipes do not receive dual-runtime topic markers
// in the generate skeleton.
func TestRecipeGenerate_BackendMinimal_OmitsDualRuntimeContent(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeBackendMinimal)
	guide := resolveRecipeGuidance(RecipeStepGenerate, plan.Tier, plan)
	for _, shouldNotContain := range []string{
		"[topic: dual-runtime-urls]",
		"[topic: serve-only-dev]",
	} {
		if strings.Contains(guide, shouldNotContain) {
			t.Errorf("backend-minimal generate skeleton contains %q", shouldNotContain)
		}
	}
}

// TestBuildGenerateRetryDelta_IsShort asserts that the Phase 10 retry
// delta stays under 5 KB across every shape. The delta must be MUCH
// smaller than the full generate composition (~16-40 KB depending on
// shape) — it's the whole point of gating iteration > 0 through a delta
// function instead of re-emitting the full guide.
func TestBuildGenerateRetryDelta_IsShort(t *testing.T) {
	t.Parallel()
	for _, shape := range []RecipeShape{
		ShapeHelloWorld,
		ShapeBackendMinimal,
		ShapeFullStackShowcase,
		ShapeDualRuntimeShowcase,
	} {
		t.Run(fmt.Sprint(shape), func(t *testing.T) {
			t.Parallel()
			plan := fixtureForShape(shape)
			delta := buildGenerateRetryDelta(plan, "test attestation: wrote zerops.yaml for app+api+worker")
			if delta == "" {
				t.Fatal("delta should be non-empty")
			}
			const capBytes = 5 * 1024
			if len(delta) > capBytes {
				t.Errorf("shape %v retry delta %d B > %d B cap", shape, len(delta), capBytes)
			}
			if !strings.Contains(delta, "Retry") {
				t.Errorf("shape %v retry delta missing 'Retry' marker", shape)
			}
			if !strings.Contains(delta, "test attestation") {
				t.Errorf("shape %v retry delta missing last-attestation passthrough", shape)
			}
		})
	}
}

// TestBuildGenerateRetryDelta_ShapeSpecificBranches exercises each of the
// four predicate-gated branches in buildGenerateRetryDelta. The retry
// delta adds a shape-specific bullet when the plan satisfies
// isDualRuntime, hasBundlerDevServer, hasSharedCodebaseWorker, or
// needsMultiBaseGuidance — and must NOT add the bullet when the
// predicate is false. TestBuildGenerateRetryDelta_IsShort only checks the
// length cap and the always-on markers; this test guards against a
// predicate misfire that would leak a wrong-shape reminder into the
// narrowest recipe.
//
// Multi-base note: fullstack-showcase fixture has BuildCommands
// `composer install + npm ci + npm run build` on a php-nginx primary →
// needsMultiBaseGuidance fires. The older `hasMultiBaseBuildCommand`
// predicate keyed on BuildBases (empty in fixtures) and never fired; the
// unification means this test now asserts the multi-base bullet IS
// present for fullstack-showcase.
//
// Each needle below is unique to its branch (checked against the function
// body in recipe_guidance.go). If the text of any reminder is edited,
// update both places.
func TestBuildGenerateRetryDelta_ShapeSpecificBranches(t *testing.T) {
	t.Parallel()

	const (
		dualRuntimeNeedle  = "Dual-runtime URL references"
		bundlerHostNeedle  = "Dev-server host-check not updated"
		sharedWorkerNeedle = "Missing `setup: worker` block"
		multiBaseNeedle    = "secondary-runtime dependency install"
	)

	tests := []struct {
		name    string
		plan    *RecipePlan
		must    []string
		mustNot []string
	}{
		{
			// hello-world: no predicate fires — delta must contain only
			// always-on reminders.
			name: "hello-world/all-predicates-false",
			plan: fixtureForShape(ShapeHelloWorld),
			mustNot: []string{
				dualRuntimeNeedle, bundlerHostNeedle,
				sharedWorkerNeedle, multiBaseNeedle,
			},
		},
		{
			// backend-minimal: single runtime, no worker, no bundler — same
			// as hello-world from the predicate perspective.
			name: "backend-minimal/all-predicates-false",
			plan: fixtureForShape(ShapeBackendMinimal),
			mustNot: []string{
				dualRuntimeNeedle, bundlerHostNeedle,
				sharedWorkerNeedle, multiBaseNeedle,
			},
		},
		{
			// fullstack-showcase: shared-codebase worker → setup: worker
			// reminder fires. BuildCommands include `npm ci` on a php-nginx
			// primary → needsMultiBaseGuidance fires → multi-base bullet
			// fires AND hasBundlerDevServer fires (multi-base implies a
			// secondary JS dev server needing host-check config). Not
			// dual-runtime — that one must stay out.
			name: "fullstack-showcase/shared-worker-and-multibase-and-bundler",
			plan: fixtureForShape(ShapeFullStackShowcase),
			must: []string{sharedWorkerNeedle, multiBaseNeedle, bundlerHostNeedle},
			mustNot: []string{
				dualRuntimeNeedle,
			},
		},
		{
			// dual-runtime-showcase: isDualRuntime fires. hasBundlerDevServer
			// also fires via the widened rule (dual-runtime + static frontend
			// → frontend runs a bundler dev server). The worker is SEPARATE
			// codebase here (see fixtureForShape) so setup:worker does NOT
			// fire. BuildBases is empty so multi-base does NOT fire.
			name: "dual-runtime-showcase/dual-and-bundler",
			plan: fixtureForShape(ShapeDualRuntimeShowcase),
			must: []string{dualRuntimeNeedle, bundlerHostNeedle},
			mustNot: []string{
				sharedWorkerNeedle, multiBaseNeedle,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			delta := buildGenerateRetryDelta(tt.plan, "prior attestation")
			for _, needle := range tt.must {
				if !strings.Contains(delta, needle) {
					t.Errorf("delta missing required needle %q", needle)
				}
			}
			for _, needle := range tt.mustNot {
				if strings.Contains(delta, needle) {
					t.Errorf("delta contains forbidden needle %q — predicate misfired", needle)
				}
			}
		})
	}
}

// TestBuildGuide_Generate_Iteration1_ReturnsDelta asserts that calling
// buildGuide with iteration > 0 returns the retry delta instead of the
// skeleton. Phase A: with skeletons, the delta may be larger than the
// compact skeleton — the invariant is that the delta DIFFERS from the
// skeleton (it's a focused retry document, not the step's guide).
func TestBuildGuide_Generate_Iteration1_ReturnsDelta(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeBackendMinimal)
	rs := advanceShowcaseStateTo(RecipeStepGenerate, plan)
	full := rs.buildGuide(RecipeStepGenerate, 0, nil, "")
	delta := rs.buildGuide(RecipeStepGenerate, 1, nil, "")
	if len(delta) == 0 || len(full) == 0 {
		t.Fatalf("empty guide: full=%d delta=%d", len(full), len(delta))
	}
	if !strings.Contains(delta, "Generate — Retry") {
		t.Error("delta missing retry header")
	}
	// The delta must be a different document from the skeleton.
	if delta == full {
		t.Error("delta is identical to full guide — retry should return focused delta")
	}
}

// TestBuildDeployRetryDelta_IsShortAndShaped asserts that the deploy
// retry delta stays under a generous cap across every shape and carries
// the universal markers (tier escalation header, universal reminders,
// source-of-truth pointer). Layer-by-layer correctness is covered by
// TestBuildDeployRetryDelta_ShapeSpecificBranches below.
func TestBuildDeployRetryDelta_IsShortAndShaped(t *testing.T) {
	t.Parallel()
	for _, shape := range []RecipeShape{
		ShapeHelloWorld,
		ShapeBackendMinimal,
		ShapeFullStackShowcase,
		ShapeDualRuntimeShowcase,
	} {
		t.Run(fmt.Sprint(shape), func(t *testing.T) {
			t.Parallel()
			plan := fixtureForShape(shape)
			delta := buildDeployRetryDelta(plan, 1, "test attestation: deployed appdev, initCommands failed")
			if delta == "" {
				t.Fatal("delta should be non-empty")
			}
			const capBytes = 8 * 1024
			if len(delta) > capBytes {
				t.Errorf("shape %v deploy retry delta %d B > %d B cap", shape, len(delta), capBytes)
			}
			// Tier 1: bootstrap escalation ladder — ITERATION marker + PREVIOUS field.
			if !strings.Contains(delta, "ITERATION 1") {
				t.Errorf("shape %v deploy retry delta missing 'ITERATION 1' header", shape)
			}
			if !strings.Contains(delta, "test attestation") {
				t.Errorf("shape %v deploy retry delta missing last-attestation passthrough", shape)
			}
			// Tier 2: universal reminders — every shape sees these.
			for _, needle := range []string{
				"Redeploy = fresh container",
				"DEPLOY_FAILED",
				"zsc execOnce",
				"Source of truth",
			} {
				if !strings.Contains(delta, needle) {
					t.Errorf("shape %v deploy retry delta missing universal needle %q", shape, needle)
				}
			}
		})
	}
}

// TestBuildDeployRetryDelta_ShapeSpecificBranches exercises each of the
// predicate-gated branches in buildDeployRetryDelta — dual-runtime
// order, bundler port collision, shared-codebase worker, separate-
// codebase worker, showcase sub-agent dispatch. Each needle is unique
// to its branch. If the text of any reminder is edited, update both
// places.
func TestBuildDeployRetryDelta_ShapeSpecificBranches(t *testing.T) {
	t.Parallel()

	const (
		dualRuntimeNeedle    = "Deploy order is non-negotiable"
		bundlerHostNeedle    = "Bundler dev server port collision"
		sharedWorkerNeedle   = "Shared-codebase worker"
		separateWorkerNeedle = "Separate-codebase worker"
		showcaseNeedle       = "Showcase sub-agent dispatch"
	)

	tests := []struct {
		name    string
		plan    *RecipePlan
		must    []string
		mustNot []string
	}{
		{
			// hello-world: no worker, no dual-runtime, no bundler, not
			// showcase — every shape-gated branch stays out.
			name: "hello-world/no-shape-branches",
			plan: fixtureForShape(ShapeHelloWorld),
			mustNot: []string{
				dualRuntimeNeedle, bundlerHostNeedle,
				sharedWorkerNeedle, separateWorkerNeedle, showcaseNeedle,
			},
		},
		{
			// backend-minimal: same predicate profile as hello-world for
			// deploy shape — no worker, no dual-runtime, no bundler.
			name: "backend-minimal/no-shape-branches",
			plan: fixtureForShape(ShapeBackendMinimal),
			mustNot: []string{
				dualRuntimeNeedle, bundlerHostNeedle,
				sharedWorkerNeedle, separateWorkerNeedle, showcaseNeedle,
			},
		},
		{
			// fullstack-showcase: shared-codebase worker (laravel + Horizon
			// pattern) + showcase + bundler dev server (multi-base: secondary
			// JS runtime for asset compilation runs a dev server). Not
			// dual-runtime.
			name: "fullstack-showcase/shared-worker-bundler-plus-showcase",
			plan: fixtureForShape(ShapeFullStackShowcase),
			must: []string{sharedWorkerNeedle, showcaseNeedle, bundlerHostNeedle},
			mustNot: []string{
				dualRuntimeNeedle, separateWorkerNeedle,
			},
		},
		{
			// dual-runtime-showcase: dual-runtime + bundler (widened rule
			// fires via dual-runtime + static frontend) + separate-codebase
			// worker (fixture default) + showcase.
			name: "dual-runtime-showcase/dual-bundler-separate-showcase",
			plan: fixtureForShape(ShapeDualRuntimeShowcase),
			must: []string{
				dualRuntimeNeedle, bundlerHostNeedle,
				separateWorkerNeedle, showcaseNeedle,
			},
			mustNot: []string{sharedWorkerNeedle},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			delta := buildDeployRetryDelta(tt.plan, 1, "prior attestation")
			for _, needle := range tt.must {
				if !strings.Contains(delta, needle) {
					t.Errorf("delta missing required needle %q", needle)
				}
			}
			for _, needle := range tt.mustNot {
				if strings.Contains(delta, needle) {
					t.Errorf("delta contains forbidden needle %q — predicate misfired", needle)
				}
			}
		})
	}
}

// TestBuildDeployRetryDelta_TierEscalation asserts the delta still carries
// the bootstrap iteration-tier escalation under its shape reminders.
// iteration 1-2 carries the diagnose/fix tier-1 prose; iteration 3-4 the
// systematic-check tier-2 prose; iteration 5+ the STOP-and-ask tier-3
// prose. This guards against an accidental drop of the BuildIterationDelta
// wrap.
func TestBuildDeployRetryDelta_TierEscalation(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeHelloWorld)
	cases := []struct {
		iter   int
		needle string
	}{
		{1, `DIAGNOSE: zerops_logs`},
		{3, "PREVIOUS FIXES FAILED"},
		{5, "STOP. Multiple fixes failed"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("iter%d", tc.iter), func(t *testing.T) {
			t.Parallel()
			delta := buildDeployRetryDelta(plan, tc.iter, "test")
			if !strings.Contains(delta, tc.needle) {
				t.Errorf("iteration %d delta missing tier needle %q", tc.iter, tc.needle)
			}
		})
	}
}

// TestRecipeGenerate_DualRuntimeShowcase_IncludesAllRelevant asserts that the
// widest shape (dual-runtime showcase) receives all shape-gated topic markers
// in the generate skeleton. Phase A: checks topic markers instead of block
// content.
func TestRecipeGenerate_DualRuntimeShowcase_IncludesAllRelevant(t *testing.T) {
	t.Parallel()
	plan := fixtureForShape(ShapeDualRuntimeShowcase)
	guide := resolveRecipeGuidance(RecipeStepGenerate, plan.Tier, plan)
	for _, mustContain := range []string{
		"[topic: dual-runtime-urls]",
		"[topic: dashboard-skeleton]",
		"[topic: zerops-yaml-rules]",
		"[topic: where-to-write]",
	} {
		if !strings.Contains(guide, mustContain) {
			t.Errorf("dual-runtime-showcase generate skeleton missing %q", mustContain)
		}
	}
}
