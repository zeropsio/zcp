package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/knowledge"
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

func TestStoreRecipeCorpus_FindViableMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		intent          string
		wantNil         bool
		wantSlug        string
		wantConfAtLeast float64
	}{
		{
			name:            "framework_hit_passes_threshold",
			intent:          "Laravel weather dashboard",
			wantSlug:        "laravel-minimal",
			wantConfAtLeast: 0.9,
		},
		{
			name:            "language_hit_passes_threshold",
			intent:          "php service",
			wantSlug:        "laravel-minimal",
			wantConfAtLeast: 0.85,
		},
		{
			name:    "no_keyword_match_returns_nil",
			intent:  "weather dashboard",
			wantNil: true,
		},
		{
			name:    "empty_intent_returns_nil",
			intent:  "",
			wantNil: true,
		},
	}

	corpus := newTestCorpus(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			match, err := corpus.FindViableMatch(tt.intent)
			if err != nil {
				t.Fatalf("FindViableMatch: %v", err)
			}
			if tt.wantNil {
				if match != nil {
					t.Fatalf("want nil match, got %+v", match)
				}
				return
			}
			if match == nil {
				t.Fatalf("want match for %q, got nil", tt.intent)
			}
			if match.Slug != tt.wantSlug {
				t.Errorf("slug: got %q, want %q", match.Slug, tt.wantSlug)
			}
			if match.Confidence < tt.wantConfAtLeast {
				t.Errorf("confidence: got %.2f, want ≥ %.2f", match.Confidence, tt.wantConfAtLeast)
			}
		})
	}
}

func TestStoreRecipeCorpus_FindViableMatch_SetsMode(t *testing.T) {
	t.Parallel()
	corpus := newTestCorpus(t)
	match, err := corpus.FindViableMatch("Laravel weather dashboard")
	if err != nil {
		t.Fatalf("FindViableMatch: %v", err)
	}
	if match == nil {
		t.Fatal("expected match")
	}
	if match.Mode != PlanModeStandard {
		t.Errorf("mode: got %q, want %q", match.Mode, PlanModeStandard)
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
	if match, _ := corpus.FindViableMatch("laravel"); match != nil {
		t.Errorf("nil corpus should return nil, got %+v", match)
	}
	if cs := corpus.Candidates("laravel", 3); cs != nil {
		t.Errorf("nil corpus should return nil candidates, got %+v", cs)
	}
	if r := corpus.LookupRecipe("laravel"); r != nil {
		t.Errorf("nil corpus should return nil recipe, got %+v", r)
	}
}
