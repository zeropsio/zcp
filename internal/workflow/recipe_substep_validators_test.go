package workflow

import (
	"context"
	"testing"
)

// TestValidateFeatureSubagent covers the new attestation-enforcing validator
// wired to SubStepSubagent at the deploy step. v11 shipped a scaffold-quality
// frontend because the main agent autonomously decided step 4b was "already
// done" and never dispatched the feature sub-agent; the validator removes
// that autonomy — the step can only be marked complete via an attestation
// describing what the feature sub-agent did.
//
// The validator must:
//   - pass when the attestation is a meaningful description (>= 40 chars)
//   - fail when the attestation is missing (empty string)
//   - fail when the attestation is boilerplate or too short to describe work
//
// Length alone is not a perfect proxy, but it is a sharp proxy: v11's skip
// would have attested "already done" or similar, which is under 40 chars.
// The threshold forces the agent to name what was produced, which also
// makes human review of session logs usable ("feature sub-agent added
// styled dispatch form, typed Task interface, refresh button, task-count
// status badge in JobsSection.svelte").
func TestValidateFeatureSubagent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		attestation string
		wantPassed  bool
	}{
		{
			name:        "empty attestation fails",
			attestation: "",
			wantPassed:  false,
		},
		{
			name:        "short attestation fails",
			attestation: "already done",
			wantPassed:  false,
		},
		{
			name:        "boilerplate attestation fails",
			attestation: "dispatched sub-agent",
			wantPassed:  false,
		},
		{
			name:        "meaningful attestation passes",
			attestation: "feature sub-agent added styled JobsSection.svelte with typed Task interface, dispatch form, refresh button, and pending-task badge",
			wantPassed:  true,
		},
	}

	plan := &RecipePlan{Tier: RecipeTierShowcase}
	state := &RecipeState{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := validateFeatureSubagent(context.Background(), plan, state, tt.attestation)
			if result == nil {
				t.Fatal("validator returned nil")
			}
			if result.Passed != tt.wantPassed {
				t.Errorf("attestation %q: passed=%v, want %v. Issues: %v", tt.attestation, result.Passed, tt.wantPassed, result.Issues)
			}
		})
	}
}

// TestGetSubStepValidator_FeatureSubagent locks in the wiring: the
// SubStepSubagent constant must resolve to the new validator. Without this,
// the validator exists but is never called — regression-proofs the
// "MANDATORY" enforcement the v11 failure revealed.
func TestGetSubStepValidator_FeatureSubagent(t *testing.T) {
	t.Parallel()
	v := getSubStepValidator(SubStepSubagent)
	if v == nil {
		t.Fatal("getSubStepValidator(SubStepSubagent) returned nil — validator not wired")
	}
}

// TestGetSubStepValidator_FeatureSweep locks the wiring for both
// feature-sweep sub-steps (dev and stage) — both must route through
// validateFeatureSweep. Without this, the sub-step exists in the
// sequence but nothing enforces the content-type / status contract.
func TestGetSubStepValidator_FeatureSweep(t *testing.T) {
	t.Parallel()
	if getSubStepValidator(SubStepFeatureSweepDev) == nil {
		t.Error("getSubStepValidator(SubStepFeatureSweepDev) returned nil — feature-sweep-dev validator not wired")
	}
	if getSubStepValidator(SubStepFeatureSweepStage) == nil {
		t.Error("getSubStepValidator(SubStepFeatureSweepStage) returned nil — feature-sweep-stage validator not wired")
	}
}

// TestValidateFeatureSweep covers the attestation contract the
// deploy-phase sweep enforces. The validator is attestation-based —
// the agent runs the curls, the engine enforces the reporting format
// so "already done" / "all features pass" style escape hatches fail.
//
// The critical case is "v18 nginx SPA fallback": an /api/* request
// returns 200 + text/html. The validator must reject the attestation
// even though the status is technically 2xx.
func TestValidateFeatureSweep(t *testing.T) {
	t.Parallel()

	// Plan with two api-surface features and one ui-only feature.
	// The sweep must require sweep entries for the api-surface ones
	// only (ui-only is observed in the browser walk, not curl).
	plan := &RecipePlan{
		Tier: RecipeTierShowcase,
		Features: []RecipeFeature{
			{
				ID:          "items-crud",
				Description: "DB-backed items endpoint.",
				Surface:     []string{FeatureSurfaceAPI, FeatureSurfaceUI, FeatureSurfaceDB},
				HealthCheck: "/api/items",
				UITestID:    "items-crud",
				Interaction: "click submit",
				MustObserve: "row count up",
			},
			{
				ID:          "search-items",
				Description: "Meilisearch full-text search.",
				Surface:     []string{FeatureSurfaceAPI, FeatureSurfaceUI, FeatureSurfaceSearch},
				HealthCheck: "/api/search",
				UITestID:    "search-items",
				Interaction: "type query",
				MustObserve: "hit count > 0",
			},
			{
				// UI-only feature (no api surface) — must not be required
				// by the sweep.
				ID:          "ui-splash",
				Description: "Splash screen panel.",
				Surface:     []string{FeatureSurfaceUI},
				UITestID:    "ui-splash",
				Interaction: "observe",
				MustObserve: "splash visible",
			},
		},
	}
	state := &RecipeState{}

	tests := []struct {
		name        string
		attestation string
		wantPassed  bool
		wantIssue   string
	}{
		{
			name:        "empty plan-less sweep — nil plan passes",
			attestation: "",
			wantPassed:  true,
		},
		{
			name: "all features pass with correct format",
			attestation: "items-crud: 200 application/json\n" +
				"search-items: 200 application/json\n",
			wantPassed: true,
		},
		{
			name: "missing feature is rejected",
			attestation: "items-crud: 200 application/json\n" +
				"// forgot search-items\n",
			wantPassed: false,
			wantIssue:  "search-items",
		},
		{
			name: "v18 nginx-SPA-fallback — 200 with text/html rejected",
			attestation: "items-crud: 200 application/json\n" +
				"search-items: 200 text/html\n",
			wantPassed: false,
			wantIssue:  "text/html",
		},
		{
			name: "500 on any feature rejected even if others pass",
			attestation: "items-crud: 200 application/json\n" +
				"search-items: 500 application/json\n",
			wantPassed: false,
			wantIssue:  "5xx",
		},
		{
			name: "404 rejected (bad route)",
			attestation: "items-crud: 200 application/json\n" +
				"search-items: 404 application/json\n",
			wantPassed: false,
			wantIssue:  "4xx",
		},
		{
			name: "missing application/json token rejected",
			attestation: "items-crud: 200 application/json\n" +
				"search-items: 200\n",
			wantPassed: false,
			wantIssue:  "application/json",
		},
		{
			name: "missing 2xx token rejected",
			attestation: "items-crud: 200 application/json\n" +
				"search-items: application/json\n",
			wantPassed: false,
			wantIssue:  "2xx",
		},
		{
			name: "case-insensitive content-type token passes",
			attestation: "items-crud: 200 Application/JSON; charset=utf-8\n" +
				"search-items: 200 application/json\n",
			wantPassed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Nil plan path uses its own plan.
			var usePlan *RecipePlan
			if tt.name != "empty plan-less sweep — nil plan passes" {
				usePlan = plan
			}
			result := validateFeatureSweep(context.Background(), usePlan, state, tt.attestation)
			if result == nil {
				t.Fatal("validator returned nil")
			}
			if result.Passed != tt.wantPassed {
				t.Errorf("Passed=%v, want %v. Issues: %v", result.Passed, tt.wantPassed, result.Issues)
			}
			if tt.wantIssue != "" {
				found := false
				for _, issue := range result.Issues {
					if containsFold(issue, tt.wantIssue) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected issue containing %q, got: %v", tt.wantIssue, result.Issues)
				}
			}
		})
	}
}

// containsFold is a case-insensitive substring check used by the
// feature-sweep tests to match issue messages that may vary in casing.
func containsFold(haystack, needle string) bool {
	h := []rune(haystack)
	n := []rune(needle)
	if len(n) == 0 {
		return true
	}
	for i := 0; i+len(n) <= len(h); i++ {
		match := true
		for j, r := range n {
			if toLower(h[i+j]) != toLower(r) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}
