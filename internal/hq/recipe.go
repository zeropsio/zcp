package hq

import (
	"context"
	"net/http"
	"net/url"
)

// The application's recipe (SPEC §3.2c): one directory per tier on `main` of
// its recipe repository, each with the whole-project import.yaml that
// creates it. HQ answers a tier by the client's name for it.

// RecipeTierMate is the AI Agent tier, `0 — AI Agent/import.yaml`: the
// project a Mate of the application is.
const RecipeTierMate = "mate"

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
