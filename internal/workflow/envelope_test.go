package workflow

import (
	"encoding/json"
	"testing"
	"time"
)

// TestStateEnvelope_JSONRoundtrip confirms that every envelope fixture
// round-trips losslessly through JSON. This is the compaction-safety
// invariant: the same envelope must serialize identically on every call.
func TestStateEnvelope_JSONRoundtrip(t *testing.T) {
	t.Parallel()

	fixed := time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC)
	cases := []StateEnvelope{
		// idle / empty
		{Phase: PhaseIdle, Environment: EnvLocal, Generated: fixed},
		// idle / container
		{Phase: PhaseIdle, Environment: EnvContainer, SelfService: &SelfService{Hostname: "zcp"}, Generated: fixed},
		// bootstrap-active with recipe
		{
			Phase:       PhaseBootstrapActive,
			Environment: EnvLocal,
			Project:     ProjectSummary{ID: "p1", Name: "weather"},
			Recipe:      &RecipeSessionSummary{Slug: "laravel-dashboard", Confidence: 0.91},
			Generated:   fixed,
		},
		// bootstrap-active with recipe route summary
		{
			Phase:       PhaseBootstrapActive,
			Environment: EnvContainer,
			Project:     ProjectSummary{ID: "p1", Name: "weather"},
			Bootstrap: &BootstrapSessionSummary{
				Route:       BootstrapRouteRecipe,
				Intent:      "laravel dashboard",
				RecipeMatch: &RecipeMatch{Slug: "laravel-dashboard", Confidence: 0.91},
			},
			Recipe:    &RecipeSessionSummary{Slug: "laravel-dashboard", Confidence: 0.91},
			Generated: fixed,
		},
		// bootstrap-active classic route (no recipe match)
		{
			Phase:       PhaseBootstrapActive,
			Environment: EnvLocal,
			Project:     ProjectSummary{ID: "p1", Name: "api"},
			Bootstrap:   &BootstrapSessionSummary{Route: BootstrapRouteClassic, Intent: "node api + postgres"},
			Generated:   fixed,
		},
		// bootstrap-active adopt route
		{
			Phase:       PhaseBootstrapActive,
			Environment: EnvLocal,
			Project:     ProjectSummary{ID: "p1", Name: "legacy"},
			Bootstrap:   &BootstrapSessionSummary{Route: BootstrapRouteAdopt, Closed: true},
			Generated:   fixed,
		},
		// develop-active, dev mode, push-dev, dynamic
		{
			Phase:       PhaseDevelopActive,
			Environment: EnvLocal,
			Project:     ProjectSummary{ID: "p1", Name: "weather"},
			Services: []ServiceSnapshot{{
				Hostname:      "appdev",
				TypeVersion:   "nodejs@20",
				RuntimeClass:  RuntimeDynamic,
				Status:        "ACTIVE",
				Bootstrapped:  true,
				Mode:          ModeDev,
				Strategy:      StrategyPushDev,
				StageHostname: "appstage",
			}},
			WorkSession: &WorkSessionSummary{
				Intent:    "add login",
				Services:  []string{"appdev"},
				CreatedAt: fixed,
				Deploys: map[string][]AttemptInfo{
					"appdev": {{At: fixed, Success: true, Iteration: 1}},
				},
			},
			Generated: fixed,
		},
		// recipe-active
		{
			Phase:       PhaseRecipeActive,
			Environment: EnvLocal,
			Project:     ProjectSummary{ID: "p1", Name: "weather"},
			Recipe:      &RecipeSessionSummary{Slug: "laravel-dashboard", Confidence: 0.91},
			Generated:   fixed,
		},
	}

	for i, env := range cases {
		data, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("case %d marshal: %v", i, err)
		}
		var got StateEnvelope
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("case %d unmarshal: %v", i, err)
		}
		data2, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("case %d remarshal: %v", i, err)
		}
		if string(data) != string(data2) {
			t.Errorf("case %d roundtrip differs:\nfirst:  %s\nsecond: %s", i, data, data2)
		}
	}
}

// TestNextAction_IsZero verifies the sentinel used by Plan validators.
func TestNextAction_IsZero(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a    NextAction
		zero bool
	}{
		{"empty", NextAction{}, true},
		{"label_only", NextAction{Label: "x"}, false},
		{"tool_only", NextAction{Tool: "zerops_workflow"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.a.IsZero(); got != c.zero {
				t.Errorf("IsZero()=%v, want=%v", got, c.zero)
			}
		})
	}
}
