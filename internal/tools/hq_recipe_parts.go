package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/runtime"
)

// A tier on main is the group's, and the additive reconcile never proposes
// over it (ReconcileGroupRecipe). A change to the app's set of parts — a
// stage a pair gained or lost (spec-mate §2.8a.4) — still has to reach it, or
// production never runs the new part and keeps running a removed one. So it
// has a narrow way back, the scaling proposal's (hq_recipe_scaling.go): the
// reconcile says which part main's tiers lack or still carry and names the
// call, and that call proposes adding or removing that ONE part's service in
// each tier file on main, every other line as main has it, as the Mate's
// change in the recipe repository a person reviews and merges (§2.8a.5). The
// service added is the composer's entry, under the hostname the tier holds
// for the part or the one the naming rule gives a new part — never one
// recomputed for a service a tier already carries.

// recipeServiceTitle is the title of a part's proposal.
func recipeServiceTitle(host string) string {
	return "Mate: " + host + " in the group recipe"
}

// recipePart is a part of the app as the recipe names it: the repository a
// service builds from and the setup it builds.
type recipePart struct{ repo, setup string }

// recipeServiceTier is what a part's proposal writes to one tier file on
// main: main's body with the part's service added or removed, or main's own
// when nothing changes or the edit refused (why, in refused).
type recipeServiceTier struct {
	path, body     string
	added, removed string
	refused        string
}

// recipePartOf is the part host names in this Mate's composed tiers or in
// main's — a stage of the Mate's, or a tier's own runtime — and whether one
// does.
func recipePartOf(ctx context.Context, comp groupRecipeComposition, host string) (recipePart, bool, error) {
	for _, file := range recipeTierFiles(comp.files) {
		bodies := []string{file.Body}
		mainBody, found, err := comp.scratch.File(ctx, comp.main, file.Path)
		if err != nil {
			return recipePart{}, false, fmt.Errorf("could not read %q of the recipe repository %q (%w)", file.Path, hq.RecipeRepo, err)
		}
		if found {
			bodies = append(bodies, mainBody)
		}
		for _, body := range bodies {
			for _, held := range bundle.HeldRuntimes(body) {
				if held.Hostname == host {
					return recipePart{held.RepoURL, held.SetupName}, true, nil
				}
			}
		}
	}
	return recipePart{}, false, nil
}

// recipeTierFiles are the composed recipe's tier files.
func recipeTierFiles(files []recipe.File) []recipe.File {
	var tiers []recipe.File
	for _, file := range files {
		if strings.HasSuffix(file.Path, "/import.yaml") {
			tiers = append(tiers, file)
		}
	}
	return tiers
}

// planRecipeService is, for each tier file on main that carries a service of
// part's repository, what bringing part into line with this Mate does: its
// service added where the composed tier has it and main does not, removed
// where main has it and the composed tier does not. A tier that carries no
// service of the repository is a tier people wrote for it themselves, and is
// left alone; a dev half is never removed.
func planRecipeService(ctx context.Context, comp groupRecipeComposition, part recipePart) ([]recipeServiceTier, error) {
	devs := map[string]bool{}
	for _, r := range comp.inputs.Runtimes {
		devs[r.DevHostname] = true
	}
	var tiers []recipeServiceTier
	for _, file := range recipeTierFiles(comp.files) {
		mainBody, found, err := comp.scratch.File(ctx, comp.main, file.Path)
		if err != nil {
			return nil, fmt.Errorf("could not read %q of the recipe repository %q (%w)", file.Path, hq.RecipeRepo, err)
		}
		if !found {
			continue
		}
		onMain, carried, anchor := "", false, ""
		for _, held := range bundle.HeldRuntimes(mainBody) {
			if held.RepoURL != part.repo {
				continue
			}
			carried, anchor = true, held.Hostname
			if held.SetupName == part.setup {
				onMain = held.Hostname
			}
		}
		if !carried {
			continue
		}
		composed := ""
		for _, held := range bundle.HeldRuntimes(file.Body) {
			if held.RepoURL == part.repo && held.SetupName == part.setup {
				composed = held.Hostname
			}
		}
		tier := recipeServiceTier{path: file.Path, body: mainBody}
		var edited string
		switch {
		case composed != "" && onMain == "":
			edited, err = bundle.AddTierService(mainBody, file.Body, composed, anchor)
			tier.added = composed
		case composed == "" && onMain != "" && !devs[onMain]:
			edited, err = bundle.RemoveTierService(mainBody, onMain)
			tier.removed = onMain
		default:
			tiers = append(tiers, tier)
			continue
		}
		if err != nil {
			tier.added, tier.removed, tier.refused = "", "", err.Error()
		} else {
			tier.body = edited
		}
		tiers = append(tiers, tier)
	}
	return tiers, nil
}

// serviceTierEdits reads the tiers a proposal changes as "Tier: adds x".
func serviceTierEdits(tiers []recipeServiceTier) []string {
	var out []string
	for _, tier := range tiers {
		switch {
		case tier.added != "":
			out = append(out, tierTitle(tier.path)+": adds "+tier.added)
		case tier.removed != "":
			out = append(out, tierTitle(tier.path)+": removes "+tier.removed)
		}
	}
	return out
}

// refusedServiceTiers reads the tiers left as main has them, and why.
func refusedServiceTiers(tiers []recipeServiceTier) string {
	var out []string
	for _, tier := range tiers {
		if tier.refused != "" {
			out = append(out, tierTitle(tier.path)+" ("+tier.refused+")")
		}
	}
	if len(out) == 0 {
		return ""
	}
	return " Left as main has it: " + strings.Join(out, "; ") + "."
}

// recipePartSteer is what the reconcile says when main's tiers do not carry
// this Mate's parts as it runs them: for each part a tier lacks or still
// carries, the call that proposes it — named by the Mate's stage, as the AI
// Agent tier lists it, or else by a tier's runtime. A part whose proposal is
// the Mate's open change already is not said again. "" when every tier
// carries the parts as the Mate runs them.
func recipePartSteer(ctx context.Context, comp groupRecipeComposition) (string, error) {
	type named struct {
		part recipePart
		host string
	}
	var candidates []named
	seen := map[recipePart]bool{}
	for _, file := range recipeTierFiles(comp.files) {
		mainBody, _, err := comp.scratch.File(ctx, comp.main, file.Path)
		if err != nil {
			return "", fmt.Errorf("could not read %q of the recipe repository %q (%w)", file.Path, hq.RecipeRepo, err)
		}
		for _, held := range append(bundle.HeldRuntimes(file.Body), bundle.HeldRuntimes(mainBody)...) {
			part := recipePart{held.RepoURL, held.SetupName}
			if !seen[part] {
				seen[part] = true
				candidates = append(candidates, named{part, held.Hostname})
			}
		}
	}
	open := openRecipeChange(comp.state)
	var calls []string
	for _, c := range candidates {
		tiers, err := planRecipeService(ctx, comp, c.part)
		if err != nil {
			return "", err
		}
		edits := serviceTierEdits(tiers)
		if len(edits) == 0 || open != nil && open.Title != nil && *open.Title == recipeServiceTitle(c.host) {
			continue
		}
		calls = append(calls, fmt.Sprintf("zerops_workflow action=\"group-recipe\" service=%q (%s)", c.host, strings.Join(edits, "; ")))
	}
	if len(calls) == 0 {
		return "", nil
	}
	return fmt.Sprintf("the tiers on %q of the recipe repository %q do not carry this Mate's parts as it runs them — to propose each as a change the person reviews, call %s",
		hqBase, hq.RecipeRepo, strings.Join(calls, ", then ")), nil
}

// handleGroupRecipeService is `zerops_workflow action="group-recipe"
// service=<host>`: the Mate's change in the recipe repository, titled for
// host, that adds host's part's service to each tier file on main lacking it,
// or removes it from each still carrying it, and changes nothing else. Every
// tier that carries the part's repository is written as this proposal has
// it, over main: an earlier proposal for the same host moves forward on the
// same change, and one with nothing left to propose is brought to main's
// tree.
func handleGroupRecipeService(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir, host string,
) (*mcp.CallToolResult, any, error) {
	comp, outcome, ok := composeGroupRecipe(ctx, client, httpClient, rt, stateDir)
	if !ok {
		reason := outcome.Blocked
		if reason == "" {
			reason = outcome.Line
		}
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			"The service proposal was not made: "+reason,
			"Retry once this Mate is linked to its HQ and a pair has its repository there."), WithRecoveryStatus()), nil, nil
	}
	defer comp.close()
	// A managed service the Mate's project runs is proposed for any app
	// (hq_recipe_managed.go); anything else is a part of the app.
	what := "the managed service " + host
	plan := func(ctx context.Context) ([]recipeServiceTier, error) { return planRecipeManaged(ctx, comp, host) }
	if !recipeManagedService(comp, host) {
		part, found, err := recipePartOf(ctx, comp, host)
		if err != nil {
			return serviceProposalFailed(err.Error()), nil, nil
		}
		if open := openRecipeChange(comp.state); !found && open != nil && open.Title != nil && *open.Title == recipeServiceTitle(host) {
			return withdrawRecipeService(ctx, comp, open, host), nil, nil
		}
		if !found {
			return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
				fmt.Sprintf("The service proposal was not made: this Mate's project runs no managed service %q, and neither this Mate's recipe nor the tiers on %q of the recipe repository %q name a service %q built from a repository with a setup.", host, hqBase, hq.RecipeRepo, host),
				"Name a managed service this Mate's project runs, a stage of this Mate's pair, or a runtime a tier on main carries."), WithRecoveryStatus()), nil, nil
		}
		what = host + "'s part"
		plan = func(ctx context.Context) ([]recipeServiceTier, error) { return planRecipeService(ctx, comp, part) }
	}
	var tiers []recipeServiceTier
	written, err := writeRecipeChange(ctx, comp, openRecipeChange(comp.state), recipeServiceTitle(host),
		"recipe: "+host+" in the group recipe, from "+comp.inputs.MateProjectName,
		func(ctx context.Context, _ *hq.Scratch, _ string, _ []string) (map[string]string, error) {
			planned, err := plan(ctx)
			tiers = planned
			files := make(map[string]string, len(planned))
			for _, tier := range planned {
				files[tier.path] = tier.body
			}
			return files, err
		})
	if err != nil {
		return serviceProposalFailed(err.Error()), nil, nil
	}
	edits := serviceTierEdits(tiers)
	result := map[string]any{"groupRepo": hq.RecipeRepo}
	if written.other != nil && len(edits) > 0 {
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("The service proposal was not made: this Mate's change #%d in the recipe repository %q (%q) is open, and HQ keeps one open change per Mate per repository.",
				written.other.number, hq.RecipeRepo, written.other.title),
			fmt.Sprintf("Retry once change #%d is merged or closed (%s).", written.other.number,
				comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.other.number))), WithRecoveryStatus()), nil, nil
	}
	if written.number != 0 {
		result["change"] = written.number
		result["changeUrl"] = comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.number)
	}
	if len(edits) == 0 {
		message := fmt.Sprintf("The tiers on %q of the recipe repository %q already carry %s as this Mate runs it — nothing to propose.", hqBase, hq.RecipeRepo, what)
		if written.pushed {
			message += fmt.Sprintf(" Change #%d, which proposed it before, now adds nothing.", written.number)
		}
		result["message"] = message + refusedServiceTiers(tiers)
		return jsonResult(result), nil, nil
	}
	result["branch"] = written.branch
	result["tiers"] = edits
	message := fmt.Sprintf("Proposed %s to the recipe repository %q as change #%d (%s): %s — every other line of each tier as main has it. The person reviews and merges it like any recipe change.%s",
		what, hq.RecipeRepo, written.number, result["changeUrl"], strings.Join(edits, "; "), refusedServiceTiers(tiers))
	if recipeManagedService(comp, host) {
		message += fmt.Sprintf(" It shapes only the environments made from the recipe later: the stage and production environments already made need %s added themselves, by a person or the Chief.", host)
	}
	result["message"] = message
	return jsonResult(result), nil, nil
}

func serviceProposalFailed(reason string) *mcp.CallToolResult {
	return convertError(platform.NewPlatformError(platform.ErrAPIError,
		"The service proposal was not made: "+reason,
		"Retry the call; the project is unaffected."), WithRecoveryStatus())
}

// withdrawRecipeService is a service= call for host while the Mate's open
// change in the recipe repository is host's proposal and nothing names host
// any more — a managed service the Mate has since deleted: the change is
// brought to main's tree, so it adds nothing and Core closes it.
func withdrawRecipeService(ctx context.Context, comp groupRecipeComposition, open *hq.MateChange, host string) *mcp.CallToolResult {
	written, err := writeRecipeChange(ctx, comp, open, recipeServiceTitle(host),
		"recipe: "+host+" withdrawn, from "+comp.inputs.MateProjectName,
		func(context.Context, *hq.Scratch, string, []string) (map[string]string, error) {
			return map[string]string{}, nil
		})
	if err != nil {
		return serviceProposalFailed(err.Error())
	}
	url := comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.number)
	return jsonResult(map[string]any{
		"groupRepo": hq.RecipeRepo,
		"change":    written.number,
		"changeUrl": url,
		"message": fmt.Sprintf("This Mate's project no longer runs %s, so change #%d (%s), which proposed it to the recipe repository %q, is withdrawn: it now adds nothing.",
			host, written.number, url, hq.RecipeRepo),
	})
}
