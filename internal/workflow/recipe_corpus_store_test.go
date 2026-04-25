// Tests for: StoreRecipeCorpus — adapter from knowledge.Store to the
// RecipeCorpus interface that BuildBootstrapRouteOptions consumes.
package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
	"github.com/zeropsio/zcp/internal/topology"
)

// newTestCorpus returns a RecipeCorpus backed by an in-memory store for the
// common "two-laravel + one-php" shape the bootstrap route tests need.
func newTestCorpus(t *testing.T) *StoreRecipeCorpus {
	t.Helper()
	docs := map[string]*knowledge.Document{
		"zerops://recipes/laravel-minimal": {
			URI:        "zerops://recipes/laravel-minimal",
			Title:      "Laravel Minimal",
			Languages:  []string{"php"},
			Frameworks: []string{"laravel"},
			ImportYAML: "services:\n  - hostname: appdev\n    type: php-nginx@8.4\n    zeropsSetup: dev\n  - hostname: appstage\n    type: php-nginx@8.4\n    zeropsSetup: prod\n",
		},
		"zerops://recipes/php-hello-world": {
			URI:        "zerops://recipes/php-hello-world",
			Title:      "PHP Hello World",
			Languages:  []string{"php"},
			ImportYAML: "services:\n  - hostname: app\n    type: php-nginx@8.4\n    zeropsSetup: prod\n",
		},
	}
	store, err := knowledge.NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return NewStoreRecipeCorpus(store)
}

func TestStoreRecipeCorpus_FindRankedMatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		intent        string
		limit         int
		wantEmpty     bool
		wantTopSlug   string
		wantMinTopCnf float64
	}{
		{
			name:          "framework_hit",
			intent:        "Laravel weather dashboard",
			limit:         3,
			wantTopSlug:   "laravel-minimal",
			wantMinTopCnf: 0.9,
		},
		{
			name:          "language_hit",
			intent:        "php service",
			limit:         3,
			wantTopSlug:   "laravel-minimal",
			wantMinTopCnf: 0.85,
		},
		{
			name:      "no_keyword_match",
			intent:    "weather dashboard",
			limit:     3,
			wantEmpty: true,
		},
		{
			name:      "empty_intent",
			intent:    "",
			limit:     3,
			wantEmpty: true,
		},
		{
			name:      "zero_limit",
			intent:    "laravel",
			limit:     0,
			wantEmpty: true,
		},
	}

	corpus := newTestCorpus(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			matches, err := corpus.FindRankedMatches(tt.intent, tt.limit)
			if err != nil {
				t.Fatalf("FindRankedMatches: %v", err)
			}
			if tt.wantEmpty {
				if len(matches) != 0 {
					t.Fatalf("want empty, got %+v", matches)
				}
				return
			}
			if len(matches) == 0 {
				t.Fatalf("want match for %q, got none", tt.intent)
			}
			if matches[0].Slug != tt.wantTopSlug {
				t.Errorf("top slug: got %q, want %q", matches[0].Slug, tt.wantTopSlug)
			}
			if matches[0].Confidence < tt.wantMinTopCnf {
				t.Errorf("top confidence: got %.2f, want ≥ %.2f", matches[0].Confidence, tt.wantMinTopCnf)
			}
		})
	}
}

func TestStoreRecipeCorpus_FindRankedMatches_SetsMode(t *testing.T) {
	t.Parallel()
	corpus := newTestCorpus(t)
	matches, err := corpus.FindRankedMatches("Laravel weather dashboard", 1)
	if err != nil {
		t.Fatalf("FindRankedMatches: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected match")
	}
	if matches[0].Mode != topology.PlanModeStandard {
		t.Errorf("mode: got %q, want %q", matches[0].Mode, topology.PlanModeStandard)
	}
}

func TestStoreRecipeCorpus_FindRankedMatches_LimitCaps(t *testing.T) {
	t.Parallel()
	corpus := newTestCorpus(t)
	// "php" matches both laravel-minimal and php-hello-world.
	matches, err := corpus.FindRankedMatches("php", 1)
	if err != nil {
		t.Fatalf("FindRankedMatches: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("limit=1 should produce exactly one match, got %d", len(matches))
	}
}

func TestStoreRecipeCorpus_LookupRecipe(t *testing.T) {
	t.Parallel()
	corpus := newTestCorpus(t)

	got := corpus.LookupRecipe("laravel-minimal")
	if got == nil {
		t.Fatal("expected non-nil lookup")
	}
	if got.Slug != "laravel-minimal" {
		t.Errorf("slug: got %q", got.Slug)
	}
	if got.ImportYAML == "" {
		t.Error("expected non-empty ImportYAML")
	}
	if missing := corpus.LookupRecipe("not-a-recipe"); missing != nil {
		t.Errorf("expected nil for missing slug, got %+v", missing)
	}
}

func TestStoreRecipeCorpus_NilSafe(t *testing.T) {
	t.Parallel()
	var corpus *StoreRecipeCorpus
	if matches, _ := corpus.FindRankedMatches("laravel", 3); matches != nil {
		t.Errorf("nil corpus should return nil matches, got %+v", matches)
	}
	if r := corpus.LookupRecipe("laravel"); r != nil {
		t.Errorf("nil corpus should return nil recipe, got %+v", r)
	}
}
