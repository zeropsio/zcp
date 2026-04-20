package workflow

import "github.com/zeropsio/zcp/internal/knowledge"

// StoreRecipeCorpus adapts knowledge.Store to the RecipeCorpus interface so
// the bootstrap route selector can use the embedded recipe documents without
// pulling the full knowledge API into the workflow package.
//
// FindViableMatch returns the top-scoring candidate when its confidence
// clears MinRecipeConfidence; otherwise nil. The full candidate list (for
// atoms that want to offer a narrow choice to the LLM) is available via
// Candidates.
type StoreRecipeCorpus struct {
	Store *knowledge.Store
}

// NewStoreRecipeCorpus builds a corpus backed by the given knowledge store.
// Returns nil when store is nil so callers (including SelectBootstrapRoute)
// fall back to classic routing without a nil-check dance.
func NewStoreRecipeCorpus(store *knowledge.Store) *StoreRecipeCorpus {
	if store == nil {
		return nil
	}
	return &StoreRecipeCorpus{Store: store}
}

// FindViableMatch implements RecipeCorpus. Delegates to Candidates and
// returns the top entry only when it clears MinRecipeConfidence.
func (c *StoreRecipeCorpus) FindViableMatch(intent string) (*RecipeMatch, error) {
	if c == nil || c.Store == nil {
		return nil, nil //nolint:nilnil // absence sentinel — matches interface contract
	}
	candidates := c.Store.FindRecipeCandidates(intent, 1)
	if len(candidates) == 0 {
		return nil, nil //nolint:nilnil
	}
	top := candidates[0]
	if top.Confidence < MinRecipeConfidence {
		return nil, nil //nolint:nilnil
	}
	mode, _ := InferRecipeShape(top.ImportYAML)
	return &RecipeMatch{Slug: top.Slug, Confidence: top.Confidence, ImportYAML: top.ImportYAML, Mode: mode}, nil
}

// Candidates exposes the full ranked candidate list for atoms that want to
// offer a narrow selection to the LLM (happy path: top-3 with confidences).
func (c *StoreRecipeCorpus) Candidates(intent string, maxResults int) []knowledge.RecipeCandidate {
	if c == nil || c.Store == nil {
		return nil
	}
	return c.Store.FindRecipeCandidates(intent, maxResults)
}

// LookupRecipe returns the import YAML + metadata for a named recipe. Used
// by the bootstrap conductor after the LLM picks a slug from the candidate
// list to pull the canonical import YAML for zerops_import.
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
