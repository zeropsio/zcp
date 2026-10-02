package hq

import (
	"context"
	"net/http"
	"net/url"
)

// The application's recipe (SPEC §3.2c; @t3tools/shared/hqRecipe, which
// these mirror): one directory per tier on `main` of its recipe repository,
// each with the whole-project import.yaml that creates it. HQ answers a tier
// by its name.

// RecipeRepo is an application's recipe repository, beside its services':
// the name is reserved for it.
const RecipeRepo = "group"

// RecipeProposalTitle is the exact title of a Mate's recipe proposal: zcp
// writes it, the client knows a proposal by it.
const RecipeProposalTitle = "Mate: the group's import files"

// The tiers, by the name HQ answers them by: a Mate's, a stage's, a
// production's.
const (
	RecipeTierMate       = "mate"
	RecipeTierStage      = "stage"
	RecipeTierProduction = "production"
)

// RecipeTierPaths are each tier's import file in the recipe repository.
var RecipeTierPaths = map[string]string{
	RecipeTierMate:       "0 — AI Agent/import.yaml",
	RecipeTierStage:      "3 — Stage/import.yaml",
	RecipeTierProduction: "4 — Small Production/import.yaml",
}

// The states a tier is in on `main`.
const (
	RecipePresent = "present"
	RecipeAbsent  = "absent"
)

// RecipeTier is a tier as `main` of the recipe repository holds it:
// ImportYAML and the MainHead it was read at when present.
type RecipeTier struct {
	State      string `json:"state"`
	ImportYAML string `json:"importYaml"`
	MainHead   string `json:"mainHead"`
}

// RecipeTier reads tier of the application HQ holds the Mate in.
func (c Client) RecipeTier(ctx context.Context, tier string) (RecipeTier, error) {
	var read RecipeTier
	err := c.call.json(ctx, http.MethodGet, "/api/mate/recipe/"+url.PathEscape(tier), c.authorization(), nil, &read)
	return read, err
}
