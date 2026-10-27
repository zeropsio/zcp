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
// of every runtime — every other line as main has it, as the Mate's recipe
// change. It only ever adds: a managed service a tier carries and
// the Mate's project does not may be one people want there, so nothing
// removes it. Merging the change imports the service into every stage or
// production environment made from a tier it changes, so such a change waits
// for a person to merge it (recipeLanding).

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
// was closed without merging, none of the Mate's changes there merged after
// it (recipeClosedProposal), and no merge before it superseded it
// (recipeSuperseded). A later merge — a tier added, another service carried
// — changes main, and what the proposal would be is worth asking about
// again. scratch holds main of the recipe repository at main; the closed
// change's branch is read into it only when a merge came before it.
func recipeDeclined(ctx context.Context, hqc hq.Client, scratch *hq.Scratch, main string, state hq.MateState, title string) bool {
	closed := recipeClosedProposal(state, title)
	if closed == nil {
		return false
	}
	earlier := recipeEarlierMerges(state, *closed)
	return len(earlier) == 0 || !recipeSuperseded(ctx, hqc, scratch, main, *closed, earlier)
}

// recipeClosedProposal is the Mate's latest change titled title in the
// recipe repository when it was closed without merging and none of the
// Mate's changes there merged after it; nil otherwise.
func recipeClosedProposal(state hq.MateState, title string) *hq.MateChange {
	var latest *hq.MateChange
	for i := range state.Changes {
		if c := &state.Changes[i]; c.Repo == hq.RecipeRepo && c.Title != nil && *c.Title == title && (latest == nil || c.Number > latest.Number) {
			latest = c
		}
	}
	if latest == nil || latest.State != hq.ChangeClosed || slices.ContainsFunc(state.Changes, func(c hq.MateChange) bool {
		return c.Repo == hq.RecipeRepo && c.State == hq.ChangeMerged && c.Number > latest.Number
	}) {
		return nil
	}
	return latest
}

// recipeEarlierMerges are the Mate's changes in the recipe repository that
// were opened before closed — HQ numbers a repository's changes as they open
// — and merged, each naming the commit its landing left on main.
func recipeEarlierMerges(state hq.MateState, closed hq.MateChange) []hq.MateChange {
	var merged []hq.MateChange
	for _, c := range state.Changes {
		if c.Repo == hq.RecipeRepo && c.State == hq.ChangeMerged && c.Number < closed.Number && c.MergedSha != nil {
			merged = append(merged, c)
		}
	}
	return merged
}

// recipeSuperseded reports whether a merge in earlier superseded closed, a
// duplicate of it rather than a proposal a person turned down: closed was
// composed against a main from before that merge landed — the commit the
// landing left on main is not in closed's history, as with the second of two
// passes at once — or closed holds nothing main lacks, as once a pass
// brought such a duplicate to main's tree and Core closed it empty. The
// Mate's state names no time a settled change opened, merged or closed, so
// git tells. Whatever git cannot tell — a branch HQ no longer has, an HQ
// that does not answer — leaves the close a decline.
func recipeSuperseded(ctx context.Context, hqc hq.Client, scratch *hq.Scratch, main string, closed hq.MateChange, earlier []hq.MateChange) bool {
	if closed.Head == nil || scratch == nil {
		return false
	}
	if _, found, err := scratch.Fetch(ctx, hqc.BranchOf(closed.Number, closed.Branch)); err != nil || !found {
		return false
	}
	head := *closed.Head
	for _, merged := range earlier {
		if landed, err := scratch.IsAncestor(ctx, *merged.MergedSha, head); err == nil && !landed {
			return true
		}
	}
	return recipeCarriedByMain(ctx, scratch, main, head)
}

// recipeCarriedByMain reports whether main holds every file rev holds, as
// rev holds it; false when git cannot tell.
func recipeCarriedByMain(ctx context.Context, scratch *hq.Scratch, main, rev string) bool {
	paths, err := scratch.Paths(ctx, rev)
	if err != nil {
		return false
	}
	files := make(map[string]string, len(paths))
	for _, path := range paths {
		body, found, err := scratch.File(ctx, rev, path)
		if err != nil || !found {
			return false
		}
		files[path] = body
	}
	with, err := scratch.TreeWith(ctx, main, files)
	if err != nil {
		return false
	}
	mainTree, err := scratch.Tree(ctx, main)
	return err == nil && with == mainTree
}

// recipeManagedSteer is what the reconcile says when tiers on main lack a
// managed service this Mate's project runs: for each, the call that
// proposes it — or, for the tiers zcp's edit cannot write, that it is added
// to them by hand — and that merging the change imports it into every stage
// or production environment made from those tiers, so it waits for a person
// unless none is. A service whose proposal is the Mate's open change already,
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
		if len(edits) == 0 || open != nil && open.Title != nil && *open.Title == title || recipeDeclined(ctx, comp.hqc, comp.scratch, comp.main, comp.state, title) {
			continue
		}
		calls = append(calls, fmt.Sprintf("zerops_workflow action=\"group-recipe\" service=%q (%s)", m.Hostname, strings.Join(edits, "; ")))
	}
	if len(calls)+len(byHand) == 0 {
		return "", nil
	}
	line := fmt.Sprintf("the tiers on %q of the recipe repository %q lack managed services this Mate's project runs, so the environments made from them would come up without them", hqBase, hq.RecipeRepo)
	if len(calls) > 0 {
		line += " — to propose each as the Mate's recipe change, call " + strings.Join(calls, ", then ")
	}
	if len(byHand) > 0 {
		if len(calls) > 0 {
			line += "; "
		} else {
			line += " — "
		}
		line += "zcp's edit cannot write into some of them as they are written, so add these to those tiers by hand: " + strings.Join(byHand, "; ")
	}
	merging := "Merging the change imports it"
	if len(calls)+len(byHand) > 1 {
		merging = "Merging each change imports its service"
	}
	return line + ". " + merging + " into every stage or production environment made from those tiers, so it waits for a person to merge it unless none is made from them", nil
}
