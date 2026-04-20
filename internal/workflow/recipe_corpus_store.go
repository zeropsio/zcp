package workflow

import "github.com/zeropsio/zcp/internal/knowledge"

// StoreRecipeCorpus adapts knowledge.Store to the RecipeCorpus interface so
// the bootstrap route builder can use the embedded recipe documents without
// pulling the full knowledge API into the workflow package.
//
// The store is scored against the intent via knowledge.FindRecipeCandidates;
// each candidate becomes a RecipeMatch with mode inferred from its import
// YAML shape. The caller (BuildBootstrapRouteOptions) handles confidence
// filtering and option ordering.
type StoreRecipeCorpus struct {
	Store *knowledge.Store
}

// NewStoreRecipeCorpus builds a corpus backed by the given knowledge store.
// Returns nil when store is nil so callers fall back to classic-only routing
// without a nil-check dance.
func NewStoreRecipeCorpus(store *knowledge.Store) *StoreRecipeCorpus {
	if store == nil {
		return nil
	}
	return &StoreRecipeCorpus{Store: store}
}

// FindRankedMatches implements RecipeCorpus. Delegates to
// knowledge.FindRecipeCandidates and converts each candidate into a
// RecipeMatch with mode inferred from its import YAML shape. Nil corpus
// or an empty/zero-limit request returns an empty slice (never an error —
// the interface reserves errors for genuine lookup failures).
func (c *StoreRecipeCorpus) FindRankedMatches(intent string, limit int) ([]RecipeMatch, error) {
	if c == nil || c.Store == nil || limit <= 0 {
		return nil, nil
	}
	candidates := c.Store.FindRecipeCandidates(intent, limit)
	if len(candidates) == 0 {
		return nil, nil
	}
	out := make([]RecipeMatch, 0, len(candidates))
	for _, cand := range candidates {
		mode, _ := InferRecipeShape(cand.ImportYAML)
		out = append(out, RecipeMatch{
			Slug:        cand.Slug,
			Title:       cand.Title,
			Description: cand.Description,
			Confidence:  cand.Confidence,
			ImportYAML:  cand.ImportYAML,
			Mode:        mode,
		})
	}
	return out, nil
}

// LookupRecipe returns the import YAML + metadata for a named recipe. Used
// by the bootstrap conductor after the LLM picks a slug from the candidate
// list so the provision guide can pull the canonical import YAML without
// consulting the corpus again.
func (c *StoreRecipeCorpus) LookupRecipe(slug string) *knowledge.RecipeCandidate {
	if c == nil || c.Store == nil || slug == "" {
		return nil
	}
	doc, err := c.Store.Get("zerops://recipes/" + slug)
	if err != nil || doc == nil || doc.ImportYAML == "" {
		return nil
	}
	return &knowledge.RecipeCandidate{
		Slug:        slug,
		Title:       doc.Title,
		Confidence:  1.0,
		Description: doc.Description,
		Languages:   doc.Languages,
		Frameworks:  doc.Frameworks,
		ImportYAML:  doc.ImportYAML,
	}
}
