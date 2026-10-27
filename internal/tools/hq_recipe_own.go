package tools

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/recipe"
)

// A tier on main is the group's (D30): zcp proposes over none a person or
// another Mate wrote, since one Mate's view replaced the medusa group's
// hand-written tiers once. But a tier zcp itself wrote from this Mate, and
// nobody changed since, still says what zcp composed — and a Mate's project
// changes shape after its first recipe lands: its pairs move onto dev pools,
// a part is added or removed, a runtime changes type. Left additive, such a
// tier described a dev service for good (Zerops Apps and Noola, 2026-10-27).
// So zcp proposes its OWN tier anew when the project's shape changed, as the
// Mate's recipe change a person reviews — it changes a file main carries, so
// Core does not land it by itself.
//
// Its own: the tier's mark (bundle.StampTier) names this Mate and still
// matches the file; or, for a tier written before tiers carried the mark,
// every commit of main's history that changed the file is Core's squash of
// one of this Mate's recipe proposals — authored hq.SquashAuthorEmail of this
// Mate, titled as zcp titles them. Anything else — a person's change landed
// over it, another Mate's zcp, a mark another Mate's or one the file no
// longer matches — is the group's alone.

// recipeOwnSubject is the subject Core's squash gives one of zcp's recipe
// proposals: the additive one's title, a part's or a managed service's, or a
// scale's, with the change's number.
var recipeOwnSubject = regexp.MustCompile(`^(` + regexp.QuoteMeta(hq.RecipeProposalTitle) +
	`|Mate: \S+ in the group recipe|Mate: \S+'s scale in the group recipe) \(#\d+\)$`)

// recipeOwnedTiers reads which tier files on main zcp wrote from the Mate
// mateID and nobody changed since, by path. A tier main does not hold is not
// in it.
func recipeOwnedTiers(ctx context.Context, scratch *hq.Scratch, main, mateID string) (map[string]bool, error) {
	owned := map[string]bool{}
	if main == "" || mateID == "" {
		return owned, nil
	}
	paths := make([]string, 0, len(hq.RecipeTierPaths))
	for _, path := range hq.RecipeTierPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		body, found, err := scratch.File(ctx, main, path)
		if err != nil {
			return nil, fmt.Errorf("could not read %q of the recipe repository %q (%w)", path, hq.RecipeRepo, err)
		}
		if !found {
			continue
		}
		if mate, intact, stamped := bundle.TierStamp(body); stamped {
			owned[path] = intact && mate == mateID
			continue
		}
		history, err := scratch.History(ctx, main, path)
		if err != nil {
			return nil, fmt.Errorf("could not read who wrote %q of the recipe repository %q (%w)", path, hq.RecipeRepo, err)
		}
		owned[path] = len(history) > 0 && !slices.ContainsFunc(history, func(c hq.Commit) bool {
			return c.AuthorEmail != hq.SquashAuthorEmail(mateID) || !recipeOwnSubject.MatchString(c.Subject)
		})
	}
	return owned, nil
}

// recipeReplacements are the composed tier files to propose over main's:
// each tier zcp owns there (comp.owned) whose shape — the services it
// creates, bundle.TierShape — differs from what this Mate composes now. A
// scale, a variable or a comment is no reason: those have their own
// targeted proposals, and a tier of the same shape stays as main has it.
func recipeReplacements(ctx context.Context, comp groupRecipeComposition) ([]recipe.File, error) {
	var replaced []recipe.File
	for _, file := range recipeTierFiles(comp.files) {
		if !comp.owned[file.Path] {
			continue
		}
		mainBody, found, err := comp.scratch.File(ctx, comp.main, file.Path)
		if err != nil {
			return nil, fmt.Errorf("could not read %q of the recipe repository %q (%w)", file.Path, hq.RecipeRepo, err)
		}
		if found && !slices.Equal(bundle.TierShape(mainBody), bundle.TierShape(file.Body)) {
			replaced = append(replaced, file)
		}
	}
	return replaced, nil
}
