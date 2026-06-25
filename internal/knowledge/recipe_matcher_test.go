package knowledge

import (
	"slices"
	"testing"
)

// testCorpus returns an in-memory Store with a handful of recipe documents
// shaped like the production corpus (frontmatter taxonomy + ImportYAML).
func testCorpus(t *testing.T) *Store {
	t.Helper()
	docs := map[string]*Document{
		"zerops://recipes/laravel-minimal": {
			Path:       "recipes/laravel-minimal.md",
			URI:        "zerops://recipes/laravel-minimal",
			Title:      "Laravel Minimal on Zerops",
			Languages:  []string{"php"},
			Frameworks: []string{"laravel"},
			ImportYAML: "project:\n  name: laravel-minimal-agent\nservices: []\n",
		},
		"zerops://recipes/laravel-showcase": {
			Path:       "recipes/laravel-showcase.md",
			URI:        "zerops://recipes/laravel-showcase",
			Title:      "Laravel Showcase on Zerops",
			Languages:  []string{"php"},
			Frameworks: []string{"laravel"},
			ImportYAML: "project:\n  name: laravel-showcase-agent\nservices: []\n",
		},
		"zerops://recipes/php-hello-world": {
			Path:       "recipes/php-hello-world.md",
			URI:        "zerops://recipes/php-hello-world",
			Title:      "PHP Hello World",
			Languages:  []string{"php"},
			ImportYAML: "project:\n  name: php-hello-world-agent\nservices: []\n",
		},
		"zerops://recipes/bun-hello-world": {
			Path:       "recipes/bun-hello-world.md",
			URI:        "zerops://recipes/bun-hello-world",
			Title:      "Bun Hello World",
			Languages:  []string{"bun"},
			ImportYAML: "project:\n  name: bun-hello-world-agent\nservices: []\n",
		},
		"zerops://recipes/no-yaml-recipe": {
			Path:      "recipes/no-yaml-recipe.md",
			URI:       "zerops://recipes/no-yaml-recipe",
			Title:     "No YAML Recipe",
			Languages: []string{"python"},
			// ImportYAML empty — not a viable bootstrap candidate.
		},
	}
	store, err := NewStore(docs)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestFindRecipeCandidates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		intent     string
		wantSlugs  []string // in rank order
		maxResults int
	}{
		{
			name:       "framework_keyword_matches_only_framework_recipes",
			intent:     "Laravel app",
			maxResults: 3,
			wantSlugs:  []string{"laravel-minimal", "laravel-showcase"},
		},
		{
			name:       "exact_slug_matches_only_that_recipe",
			intent:     "laravel-minimal",
			maxResults: 3,
			wantSlugs:  []string{"laravel-minimal"},
		},
		{
			// Users name recipes by branded form ("zerops-laravel-minimal");
			// the matcher must alias that to the corpus slug "laravel-minimal"
			// so the route-menu surfaces the matching recipe instead of
			// falling through to classic.
			name:       "branded_slug_aliases_to_corpus_slug",
			intent:     "Deploy a Kanban app from zerops-laravel-minimal recipe. Dev service only.",
			maxResults: 3,
			wantSlugs:  []string{"laravel-minimal"},
		},
		{
			name:       "branded_slug_bare",
			intent:     "zerops-laravel-minimal",
			maxResults: 3,
			wantSlugs:  []string{"laravel-minimal"},
		},
		{
			name:       "language_keyword_returns_all_language_matches_framework_wins_tiebreak",
			intent:     "PHP backend",
			maxResults: 3,
			wantSlugs:  []string{"laravel-minimal", "laravel-showcase", "php-hello-world"},
		},
		{
			name:       "no_keyword_match_returns_nothing",
			intent:     "weather dashboard",
			maxResults: 3,
			wantSlugs:  nil,
		},
		{
			name:       "empty_intent_returns_nothing",
			intent:     "",
			maxResults: 3,
			wantSlugs:  nil,
		},
		{
			name:       "max_results_caps_output",
			intent:     "php laravel",
			maxResults: 1,
			wantSlugs:  []string{"laravel-minimal"},
		},
		{
			name:       "recipe_without_import_yaml_is_skipped",
			intent:     "python script",
			maxResults: 3,
			wantSlugs:  nil, // no-yaml-recipe would match but lacks ImportYAML
		},
	}

	store := testCorpus(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := store.FindRecipeCandidates(tt.intent, tt.maxResults)
			if len(got) != len(tt.wantSlugs) {
				t.Fatalf("FindRecipeCandidates(%q): got %d candidates, want %d (%v)",
					tt.intent, len(got), len(tt.wantSlugs), slugs(got))
			}
			for i, want := range tt.wantSlugs {
				if got[i].Slug != want {
					t.Errorf("rank[%d]: got %q, want %q", i, got[i].Slug, want)
				}
			}
		})
	}
}

// TestTokenizeIntent_DottedFrameworkCollapses pins that a dotted framework
// name survives tokenization as the collapsed form the synonym map keys on.
// Wave-7 recipe-nextjs-ssr: "Next.js" tokenized to ["next","js"], so the
// nextjs->next-js synonym never fired and every nextjs recipe scored 0.
func TestTokenizeIntent_DottedFrameworkCollapses(t *testing.T) {
	t.Parallel()
	tokens := tokenizeIntent("Next.js 15 app with SSR (nodejs runtime)")
	for _, want := range []string{"nextjs", "next", "js", "nodejs"} {
		if !slices.Contains(tokens, want) {
			t.Errorf("tokenizeIntent missing %q; got %v", want, tokens)
		}
	}
}

// TestScoreRecipe_DottedFrameworkSynonymFires pins that a "Next.js" intent
// scores a next-js framework recipe at the 0.95 framework band (via the
// nextjs->next-js synonym), above the 0.85 language band a bare nodejs recipe
// rides — so the route-menu ranks the framework match first instead of
// dropping it to 0 and degrading to classic.
func TestScoreRecipe_DottedFrameworkSynonymFires(t *testing.T) {
	t.Parallel()
	tokens := tokenizeIntent("Next.js 15 app with SSR (nodejs runtime), plus a small PostgreSQL database")
	nextjs := scoreRecipe("nextjs-ssr-hello-world", []string{"next-js"}, nil, tokens)
	if nextjs < 0.95 {
		t.Errorf("next-js framework recipe should score >=0.95 for a Next.js intent, got %v", nextjs)
	}
	nodejs := scoreRecipe("nodejs-hello-world", nil, []string{"node-js"}, tokens)
	if nextjs <= nodejs {
		t.Errorf("framework match (%v) must outrank bare language match (%v)", nextjs, nodejs)
	}
}

// TestTokenizeIntent_SeparatorSplitsWeldedNames pins that a non-space
// separator the enumerated replacers miss (`/`, `+`, `&`, …) does not weld two
// names into one dead token. "PHP/Laravel" tokenized to the single
// "php/laravel", matching neither the laravel framework nor the php language,
// so the recipe scored 0 and degraded to classic — the same failure class as
// the Next.js `.` collapse, one separator later.
func TestTokenizeIntent_SeparatorSplitsWeldedNames(t *testing.T) {
	t.Parallel()
	tokens := tokenizeIntent("server-rendered PHP/Laravel web app, Vue+Vite front end")
	for _, want := range []string{"php", "laravel", "vue", "vite"} {
		if !slices.Contains(tokens, want) {
			t.Errorf("tokenizeIntent missing %q; got %v", want, tokens)
		}
	}
}

// TestScoreRecipe_SlashSeparatedFrameworkMatches pins that a "PHP/Laravel"
// intent scores the laravel framework recipe at the 0.95 band instead of 0, so
// the route-menu surfaces the recipe rather than dropping the user to classic.
func TestScoreRecipe_SlashSeparatedFrameworkMatches(t *testing.T) {
	t.Parallel()
	tokens := tokenizeIntent("Kanban board: server-rendered PHP/Laravel web app backed by PostgreSQL")
	laravel := scoreRecipe("laravel-minimal", []string{"laravel"}, []string{"php"}, tokens)
	if laravel < 0.95 {
		t.Errorf("laravel framework recipe should score >=0.95 for a PHP/Laravel intent, got %v", laravel)
	}
}

func TestFindRecipeCandidates_Confidence(t *testing.T) {
	t.Parallel()

	store := testCorpus(t)

	tests := []struct {
		name       string
		intent     string
		wantTop    string
		wantScore  float64
		scoreDelta float64
	}{
		{
			name:       "exact_slug_hits_1_0",
			intent:     "laravel-minimal",
			wantTop:    "laravel-minimal",
			wantScore:  1.0,
			scoreDelta: 0.001,
		},
		{
			name:       "framework_hit_above_0_9",
			intent:     "Laravel",
			wantTop:    "laravel-minimal",
			wantScore:  0.95,
			scoreDelta: 0.05,
		},
		{
			name:       "language_hit_around_0_8",
			intent:     "php",
			wantTop:    "laravel-minimal", // alphabetical tiebreak among php recipes
			wantScore:  0.85,
			scoreDelta: 0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := store.FindRecipeCandidates(tt.intent, 1)
			if len(got) == 0 {
				t.Fatalf("no match for %q", tt.intent)
			}
			if got[0].Slug != tt.wantTop {
				t.Errorf("top slug: got %q, want %q", got[0].Slug, tt.wantTop)
			}
			diff := got[0].Confidence - tt.wantScore
			if diff < -tt.scoreDelta || diff > tt.scoreDelta {
				t.Errorf("confidence: got %.3f, want %.3f ± %.3f", got[0].Confidence, tt.wantScore, tt.scoreDelta)
			}
		})
	}
}

func slugs(m []RecipeCandidate) []string {
	out := make([]string, len(m))
	for i, c := range m {
		out[i] = c.Slug
	}
	return out
}
