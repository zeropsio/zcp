package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
)

// A managed service the Mate adds to its project — a database, a cache, a
// storage its app's zerops.yaml now reads the variables of — is part of the
// app, and a tier on main that lacks it makes environments whose app does
// not start. The additive reconcile never proposes over a tier on main
// (reconcileGroupRecipe), and a person cannot write main of the recipe
// repository, so it takes the part proposal's way (hq_recipe_parts.go) for
// any app, one part or several: the reconcile names the call, and
// `group-recipe service=<host>` proposes that ONE service added to each tier
// file on main lacking it — the composer's entry, each tier's policy applied
// (the Small Production tier's highly available variant, the AI Agent and
// Stage tiers' as the Mate runs it), placed among the managed services ahead
// of every runtime — every other line as main has it, as the Mate's change a
// person reviews. It only ever adds: a managed service a tier carries and
// the Mate's project does not may be one people want there, so nothing
// removes it. The change shapes environments made from the recipe later; a
// stage or production already made needs the service added itself.

// recipeManagedService reports whether host is a managed service this
// Mate's project runs.
func recipeManagedService(comp groupRecipeComposition, host string) bool {
	return slices.ContainsFunc(comp.inputs.ManagedServices, func(m bundle.ManagedServiceEntry) bool { return m.Hostname == host })
}

// planRecipeManaged is, for each tier file on main, what adding the managed
// service host does: the composed tier's entry for it added where main's
// tier, read as YAML, lacks it, main's own body where it lists host already
// or the edit refused (why, in refused).
func planRecipeManaged(ctx context.Context, comp groupRecipeComposition, host string) ([]recipeServiceTier, error) {
	var tiers []recipeServiceTier
	for _, file := range recipeTierFiles(comp.files) {
		mainBody, found, err := comp.scratch.File(ctx, comp.main, file.Path)
		if err != nil {
			return nil, fmt.Errorf("could not read %q of the recipe repository %q (%w)", file.Path, hq.RecipeRepo, err)
		}
		if !found || !bundle.TierListsService(file.Body, host) {
			continue
		}
		tier := recipeServiceTier{path: file.Path, body: mainBody}
		if !bundle.TierListsService(mainBody, host) {
			if edited, err := bundle.AddTierServiceInRank(mainBody, file.Body, host); err != nil {
				tier.refused = err.Error()
			} else {
				tier.body, tier.added = edited, host
			}
		}
		tiers = append(tiers, tier)
	}
	return tiers, nil
}

// recipeDeclined reports whether a person declined the proposal titled
// title: the Mate's latest change of that title in the recipe repository
// was closed without merging, and none of the Mate's changes there merged
// after it. A later merge — a tier added, another service carried — changes
// main, and what the proposal would be is worth asking about again.
func recipeDeclined(state hq.MateState, title string) bool {
	declined := 0
	for _, c := range state.Changes {
		if c.Repo == hq.RecipeRepo && c.Title != nil && *c.Title == title && c.Number > declined {
			if c.State != hq.ChangeClosed {
				return false
			}
			declined = c.Number
		}
	}
	if declined == 0 {
		return false
	}
	return !slices.ContainsFunc(state.Changes, func(c hq.MateChange) bool {
		return c.Repo == hq.RecipeRepo && c.State == hq.ChangeMerged && c.Number > declined
	})
}

// recipeManagedSteer is what the reconcile says when tiers on main lack a
// managed service this Mate's project runs: for each, the call that
// proposes it — or, for the tiers zcp's edit cannot write, that it is added
// to them by hand — and that the environments already made need it added
// themselves. A service whose proposal is the Mate's open change already,
// or one a person declined (recipeDeclined), is not said again. "" when
// every tier on main lists every managed service.
func recipeManagedSteer(ctx context.Context, comp groupRecipeComposition) (string, error) {
	open := openRecipeChange(comp.state)
	var calls, byHand []string
	for _, m := range comp.inputs.ManagedServices {
		tiers, err := planRecipeManaged(ctx, comp, m.Hostname)
		if err != nil {
			return "", err
		}
		var refused []string
		for _, tier := range tiers {
			if tier.refused != "" {
				refused = append(refused, tierTitle(tier.path))
			}
		}
		if len(refused) > 0 {
			byHand = append(byHand, fmt.Sprintf("%s (%s)", m.Hostname, strings.Join(refused, ", ")))
		}
		title := recipeServiceTitle(m.Hostname)
		edits := serviceTierEdits(tiers)
		if len(edits) == 0 || open != nil && open.Title != nil && *open.Title == title || recipeDeclined(comp.state, title) {
			continue
		}
		calls = append(calls, fmt.Sprintf("zerops_workflow action=\"group-recipe\" service=%q (%s)", m.Hostname, strings.Join(edits, "; ")))
	}
	if len(calls)+len(byHand) == 0 {
		return "", nil
	}
	line := fmt.Sprintf("the tiers on %q of the recipe repository %q lack managed services this Mate's project runs, so the environments made from them would come up without them", hqBase, hq.RecipeRepo)
	if len(calls) > 0 {
		line += " — to propose each as a change the person reviews, call " + strings.Join(calls, ", then ")
	}
	if len(byHand) > 0 {
		if len(calls) > 0 {
			line += "; "
		} else {
			line += " — "
		}
		line += "zcp's edit cannot write into some of them as they are written, so add these to those tiers by hand: " + strings.Join(byHand, "; ")
	}
	them := "it"
	if len(calls)+len(byHand) > 1 {
		them = "them"
	}
	return line + fmt.Sprintf(". The recipe only shapes environments made from it later: the stage and production environments already made need %s added themselves, by a person or the Chief", them), nil
}
