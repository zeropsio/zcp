package recipe

import (
	"slices"
	"strings"
	"testing"
)

// TestBuildDesignTokenTable_EmitsCompactBytes pins the byte-budget
// contract: the inline design-token table must stay compact enough to
// fit alongside every other feature-brief atom under the
// FeatureBriefCap. Target ~500 bytes; hard ceiling 600 with buffer for
// future token additions before the brief saturates. See the
// cap-pressure regression test below for the integration assertion.
func TestBuildDesignTokenTable_EmitsCompactBytes(t *testing.T) {
	t.Parallel()
	out := BuildDesignTokenTable()
	if got := len(out); got >= 600 {
		t.Errorf("BuildDesignTokenTable size = %d bytes, want < 600", got)
	}
	if len(out) == 0 {
		t.Fatal("BuildDesignTokenTable returned empty")
	}
}

// TestBuildDesignTokenTable_IncludesLoadBearingTokens pins the content
// contract: the table must surface the canonical Material 3 primary,
// the identity-zerops-green seed, the JetBrains Mono code-font
// teaching, the 12px card-radius rule, and the no-purple-gradient
// prohibition. These are the cross-recipe-consistency anchors that hold
// regardless of framework binding. Plus the pointer back to the full
// theme spec so the agent knows where the per-framework component
// lineages live.
func TestBuildDesignTokenTable_IncludesLoadBearingTokens(t *testing.T) {
	t.Parallel()
	out := BuildDesignTokenTable()
	for _, anchor := range []string{
		"Design tokens",
		"#00A49A",                       // primary
		"#00CCBB",                       // identity teal
		"JetBrains Mono",                // code font
		"Geologica",                     // headline font
		"12px",                          // canonical card radius
		"pill",                          // button shape language
		"purple gradients",              // prohibition
		"zerops://themes/design-system", // pointer for full spec
	} {
		if !strings.Contains(out, anchor) {
			t.Errorf("BuildDesignTokenTable missing load-bearing anchor %q", anchor)
		}
	}
}

// TestFeatureBrief_LoadsDesignTokens_WhenShowcaseAndHasUI — the
// design-system table must reach the feature brief whenever the
// showcase tier has a UI codebase (frontend SPA OR Laravel-style
// monolith). Two sub-cases pin both shapes.
func TestFeatureBrief_LoadsDesignTokens_WhenShowcaseAndHasUI(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan() // showcase + frontend "app" codebase
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if !slices.Contains(brief.Parts, "design-tokens") {
		t.Errorf("feature brief Parts missing design-tokens (got %v)", brief.Parts)
	}
	if !strings.Contains(brief.Body, "#00A49A") {
		t.Errorf("feature brief missing primary-color anchor")
	}
}

func TestFeatureBrief_LoadsDesignTokens_WhenShowcaseAndMonolith(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "laravel-showcase",
		Framework: "laravel",
		Tier:      "showcase",
		Codebases: []Codebase{
			{Hostname: "app", Role: RoleMonolith, BaseRuntime: "php-nginx@8"},
		},
		Services: []Service{
			{Hostname: "db", Type: "postgresql@18", Kind: ServiceKindManaged, Priority: 10},
		},
	}
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if !slices.Contains(brief.Parts, "design-tokens") {
		t.Errorf("feature brief Parts missing design-tokens for monolith plan (got %v)", brief.Parts)
	}
}

// TestFeatureBrief_OmitsDesignTokens_WhenNoUI — showcase tier without a
// UI codebase (api + worker only) doesn't ship anything visible; the
// design-token table is dead weight.
func TestFeatureBrief_OmitsDesignTokens_WhenNoUI(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug:      "synth-showcase-headless",
		Framework: "synth",
		Tier:      "showcase",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
			{Hostname: "worker", Role: RoleWorker, BaseRuntime: "nodejs@22", IsWorker: true},
		},
	}
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if slices.Contains(brief.Parts, "design-tokens") {
		t.Errorf("feature brief unexpectedly loaded design-tokens for headless plan (Parts=%v)", brief.Parts)
	}
}

// TestFeatureBrief_OmitsDesignTokens_WhenNotShowcase — minimal /
// hello-world tiers don't ship a styled UI; the design-token mandate
// is a showcase-tier concern.
func TestFeatureBrief_OmitsDesignTokens_WhenNotShowcase(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan() // has frontend codebase
	plan.Tier = "minimal"
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if slices.Contains(brief.Parts, "design-tokens") {
		t.Errorf("feature brief unexpectedly loaded design-tokens for non-showcase tier (Parts=%v)", brief.Parts)
	}
}

// TestFeatureBrief_CapWithDesignTokens_StaysUnderLimit — pre-design-
// system the showcase + worker + frontend feature brief sat at 21,956
// bytes against a 22,528 cap (572-byte headroom). The design-token
// table must fit under that headroom. Hard regression assertion: any
// future addition that would push the brief over FeatureBriefCap is
// caught here before landing.
func TestFeatureBrief_CapWithDesignTokens_StaysUnderLimit(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan() // showcase + worker + frontend (highest-load shape)
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	if brief.Bytes > FeatureBriefCap {
		t.Errorf("feature brief %d bytes over cap %d (design-tokens load pushed it past the limit)",
			brief.Bytes, FeatureBriefCap)
	}
	t.Logf("Feature brief showcase+worker+frontend: %d bytes (cap %d, headroom %d)",
		brief.Bytes, FeatureBriefCap, FeatureBriefCap-brief.Bytes)
}
