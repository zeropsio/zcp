package workflow

import "testing"

// TestPlanPredicates table-drives every predicate against representative
// plan shapes. The goal is not exhaustive coverage of combinatorial inputs
// but a quick correctness check plus a regression guard: if a predicate's
// true/false boundary shifts, this test flags it.
func TestPlanPredicates(t *testing.T) {
	t.Parallel()

	dual := fixtureForShape(ShapeDualRuntimeShowcase)
	fullStack := fixtureForShape(ShapeFullStackShowcase)
	minimal := fixtureForShape(ShapeBackendMinimal)
	hello := fixtureForShape(ShapeHelloWorld)

	tests := []struct {
		name string
		pred func(*RecipePlan) bool
		plan *RecipePlan
		want bool
	}{
		// isDualRuntime
		{"isDualRuntime/dual-runtime showcase", isDualRuntime, dual, true},
		{"isDualRuntime/fullstack showcase", isDualRuntime, fullStack, false},
		{"isDualRuntime/backend minimal", isDualRuntime, minimal, false},
		{"isDualRuntime/hello world", isDualRuntime, hello, false},
		{"isDualRuntime/nil", isDualRuntime, nil, false},

		// hasWorker
		{"hasWorker/dual-runtime", hasWorker, dual, true},
		{"hasWorker/fullstack", hasWorker, fullStack, true},
		{"hasWorker/minimal", hasWorker, minimal, false},
		{"hasWorker/hello", hasWorker, hello, false},
		{"hasWorker/nil", hasWorker, nil, false},

		// hasSharedCodebaseWorker
		{"hasSharedCodebase/dual-runtime (separate)", hasSharedCodebaseWorker, dual, false},
		{"hasSharedCodebase/fullstack (shared)", hasSharedCodebaseWorker, fullStack, true},
		{"hasSharedCodebase/minimal", hasSharedCodebaseWorker, minimal, false},
		{"hasSharedCodebase/nil", hasSharedCodebaseWorker, nil, false},

		// hasSeparateCodebaseWorker
		{"hasSeparateCodebase/dual-runtime", hasSeparateCodebaseWorker, dual, true},
		{"hasSeparateCodebase/fullstack", hasSeparateCodebaseWorker, fullStack, false},
		{"hasSeparateCodebase/minimal", hasSeparateCodebaseWorker, minimal, false},
		{"hasSeparateCodebase/nil", hasSeparateCodebaseWorker, nil, false},

		// hasServeOnlyProd
		{"hasServeOnlyProd/dual-runtime (static frontend)", hasServeOnlyProd, dual, true},
		{"hasServeOnlyProd/fullstack (php-nginx)", hasServeOnlyProd, fullStack, false},
		{"hasServeOnlyProd/minimal (php-nginx)", hasServeOnlyProd, minimal, false},
		{"hasServeOnlyProd/hello world", hasServeOnlyProd, hello, false},

		// hasBundlerDevServer — matches primary framework prefix OR a
		// dual-runtime recipe with a static frontend (the frontend is a
		// bundler-based SPA in disguise, regardless of what p.Framework
		// names the API as).
		{"hasBundlerDev/nestjs (dual)", hasBundlerDevServer, dual, true}, // dual-runtime + static frontend → frontend is bundler-based
		{"hasBundlerDev/laravel fullstack", hasBundlerDevServer, fullStack, false},
		{"hasBundlerDev/laravel minimal", hasBundlerDevServer, minimal, false},
		{"hasBundlerDev/nil", hasBundlerDevServer, nil, false},

		// hasManagedServiceCatalog
		{"hasManaged/dual-runtime", hasManagedServiceCatalog, dual, true},
		{"hasManaged/fullstack", hasManagedServiceCatalog, fullStack, true},
		{"hasManaged/minimal (postgresql)", hasManagedServiceCatalog, minimal, true},
		{"hasManaged/hello (postgresql)", hasManagedServiceCatalog, hello, true},
		{"hasManaged/nil", hasManagedServiceCatalog, nil, false},

		// hasMultipleCodebases
		{"hasMultipleCodebases/dual-runtime", hasMultipleCodebases, dual, true},
		{"hasMultipleCodebases/fullstack (shared worker)", hasMultipleCodebases, fullStack, false},
		{"hasMultipleCodebases/minimal", hasMultipleCodebases, minimal, false},
		{"hasMultipleCodebases/hello", hasMultipleCodebases, hello, false},

		// isShowcase
		{"isShowcase/dual-runtime", isShowcase, dual, true},
		{"isShowcase/fullstack", isShowcase, fullStack, true},
		{"isShowcase/minimal", isShowcase, minimal, false},
		{"isShowcase/hello", isShowcase, hello, false},
		{"isShowcase/nil", isShowcase, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.pred(tt.plan); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestHasBundlerDevServer_PrefixMatch asserts the framework prefix match —
// a recipe with framework "next-intl" should still fire the bundler rule
// because next.js is the underlying dev server.
func TestHasBundlerDevServer_PrefixMatch(t *testing.T) {
	t.Parallel()
	for _, fw := range []string{"next", "nextjs", "next-intl", "React-Router", "astro"} {
		plan := &RecipePlan{Framework: fw}
		if !hasBundlerDevServer(plan) {
			t.Errorf("framework %q should match bundler dev server list", fw)
		}
	}
	for _, fw := range []string{"laravel", "nestjs", "rails", "django", ""} {
		plan := &RecipePlan{Framework: fw}
		if hasBundlerDevServer(plan) {
			t.Errorf("framework %q should NOT match bundler dev server list", fw)
		}
	}
}
