package knowledge

import (
	"slices"
	"sort"
	"strings"
)

// RecipeCandidate is one ranked recipe suggestion for a given intent.
// Emitted by FindRecipeCandidates and consumed by the bootstrap matcher
// atom, which presents the candidates to the LLM for final selection.
type RecipeCandidate struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Confidence  float64  `json:"confidence"`
	Description string   `json:"description,omitempty"`
	Languages   []string `json:"languages,omitempty"`
	Frameworks  []string `json:"frameworks,omitempty"`
	ImportYAML  string   `json:"-"`
}

// FindRecipeCandidates ranks recipe documents against the user's intent and
// returns up to `max` candidates sorted by confidence (descending, ties
// broken alphabetically by slug). Recipes without an ImportYAML are skipped
// — they cannot drive the recipe-bootstrap happy path regardless of score.
//
// Scoring, in order of precedence:
//
//	exact slug token            1.00
//	framework slug in intent    0.95
//	language slug in intent     0.85
//
// An intent containing multiple hits takes the highest score. Empty intent
// or zero `max` returns nil.
func (s *Store) FindRecipeCandidates(intent string, maxResults int) []RecipeCandidate {
	intent = strings.TrimSpace(intent)
	if intent == "" || maxResults <= 0 || s == nil {
		return nil
	}

	tokens := tokenizeIntent(intent)
	if len(tokens) == 0 {
		return nil
	}

	var candidates []RecipeCandidate
	const recipePrefix = "zerops://recipes/"
	for uri, doc := range s.docs {
		slug, ok := strings.CutPrefix(uri, recipePrefix)
		if !ok || doc.ImportYAML == "" {
			continue
		}
		score := scoreRecipe(slug, doc.Frameworks, doc.Languages, tokens)
		if score == 0 {
			continue
		}
		candidates = append(candidates, RecipeCandidate{
			Slug:        slug,
			Title:       doc.Title,
			Confidence:  score,
			Description: doc.Description,
			Languages:   doc.Languages,
			Frameworks:  doc.Frameworks,
			ImportYAML:  doc.ImportYAML,
		})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Confidence != candidates[j].Confidence {
			return candidates[i].Confidence > candidates[j].Confidence
		}
		return candidates[i].Slug < candidates[j].Slug
	})

	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}
	return candidates
}

// tokenizeIntent splits intent into lowercase word tokens plus the original
// lowercase phrase for exact-slug fallback matches ("laravel-minimal" must
// hit as a single token, even though it looks like a two-word phrase).
func tokenizeIntent(intent string) []string {
	lower := strings.ToLower(intent)
	replacer := strings.NewReplacer(
		",", " ",
		".", " ",
		"!", " ",
		"?", " ",
		":", " ",
		";", " ",
		"'", " ",
		"\"", " ",
		"(", " ",
		")", " ",
	)
	normalized := replacer.Replace(lower)
	fields := strings.Fields(normalized)
	// The full normalized phrase is also a token so hyphenated slugs match.
	fields = append(fields, lower)
	return fields
}

// scoreRecipe returns the highest matching band for a recipe given the
// tokenized intent. Returns 0 when nothing matches so the caller can skip.
func scoreRecipe(slug string, frameworks, languages, tokens []string) float64 {
	slugLower := strings.ToLower(slug)
	best := 0.0
	if slices.Contains(tokens, slugLower) {
		return 1.0
	}
	for _, fw := range frameworks {
		if slices.Contains(tokens, strings.ToLower(fw)) {
			if 0.95 > best {
				best = 0.95
			}
		}
	}
	for _, lang := range languages {
		if slices.Contains(tokens, strings.ToLower(lang)) {
			if 0.85 > best {
				best = 0.85
			}
		}
	}
	// Common synonyms: users say "node" and "nodejs" where the corpus tags
	// are "node-js". Keep this list tiny — anything broader belongs in the
	// LLM layer, not a keyword matcher.
	synonyms := map[string]string{
		"node":   "node-js",
		"nodejs": "node-js",
		"nestjs": "nest-js",
		"nextjs": "next-js",
		"net":    "dotnet",
		"go":     "golang",
	}
	for _, t := range tokens {
		canonical, ok := synonyms[t]
		if !ok {
			continue
		}
		for _, fw := range frameworks {
			if strings.EqualFold(fw, canonical) && 0.95 > best {
				best = 0.95
			}
		}
		for _, lang := range languages {
			if strings.EqualFold(lang, canonical) && 0.85 > best {
				best = 0.85
			}
		}
	}
	return best
}
