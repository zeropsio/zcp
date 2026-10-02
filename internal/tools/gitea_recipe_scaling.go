package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/recipe"
	"github.com/zeropsio/zcp/internal/runtime"
)

// A tier on the group repo's main is the group's, and the additive reconcile
// never proposes over it (reconcileGiteaGroupRecipe). A service's scale is
// what a Mate learns by running the app — a Meilisearch that ran out of
// memory reindexing at the recipe's 1 GB — so a scale change has its own,
// narrow way back to the group: the scale tool says when the recipe on main
// differs and names the call, and that call proposes ONE host's
// verticalAutoscaling block in each tier file as a pull request the person
// reviews, the recipe's floors applied (bundle.ApplyResourceFloor). Nothing
// is proposed on its own: a Mate's experiment with its own scale is not the
// group's until someone says so.

// recipeScalingTitle heads a scaling proposal's pull request.
func recipeScalingTitle(host string) string {
	return "Mate: " + host + "'s scale in the group recipe"
}

// recipeScalingTier is one tier file on main that names a scaling
// proposal's host: what the proposal writes to it — host's block replaced,
// or main's own body when nothing changes or the splice refused (why, in
// refused) — and the keys that change.
type recipeScalingTier struct {
	path    string
	body    string
	changes []bundle.ScalingChange
	refused string
}

// planRecipeScaling composes the recipe from this Mate's project and, for
// each tier file on main that names host, what replacing host's
// verticalAutoscaling block with the composed one would change. Tiers are
// read at main's head, the commit a proposal branch is cut from.
func planRecipeScaling(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir, liveEnvPath, host string,
) (groupRecipeComposition, []recipeScalingTier, giteaRecipeOutcome, bool) {
	comp, early, ok := composeGroupRecipe(ctx, client, httpClient, rt, stateDir, liveEnvPath)
	if !ok {
		return comp, nil, early, false
	}
	var tiers []recipeScalingTier
	for _, file := range comp.files {
		if !strings.HasSuffix(file.Path, "/import.yaml") || !slices.Contains(comp.main.Paths, file.Path) {
			continue
		}
		mainBody, found, err := ops.ReadGiteaFile(ctx, httpClient, comp.wiring.GiteaURL, comp.wiring.Token, comp.groupRepo, comp.main.Head, file.Path)
		if err != nil {
			return comp, nil, giteaRecipeOutcome{GroupRepo: comp.groupRepo, Line: fmt.Sprintf("could not read %s@%s:%s (%v)", comp.groupRepo, comp.base, file.Path, err)}, false
		}
		if !found || !bundle.TierNamesHost(mainBody, host) {
			continue
		}
		tier := recipeScalingTier{path: file.Path, body: mainBody}
		if changes := bundle.HostScalingChanges(mainBody, file.Body, host); len(changes) > 0 {
			spliced, err := bundle.SpliceHostScaling(mainBody, file.Body, host)
			switch {
			case err != nil:
				tier.refused = err.Error()
			case spliced != mainBody:
				tier.body, tier.changes = spliced, changes
			}
		}
		tiers = append(tiers, tier)
	}
	return comp, tiers, giteaRecipeOutcome{GroupRepo: comp.groupRepo}, true
}

// changedTiers reads the tiers a proposal changes as "Tier: key old → new".
func changedTiers(tiers []recipeScalingTier) []string {
	var out []string
	for _, tier := range tiers {
		if len(tier.changes) > 0 {
			out = append(out, tierTitle(tier.path)+": "+scalingChangeText(tier.changes))
		}
	}
	return out
}

// refusedTiers reads the tiers left as main has them, and why.
func refusedTiers(tiers []recipeScalingTier) string {
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

// groupRecipeScalingSteer is what the scale tool (and an import override)
// adds to its answer once it changed host: the group recipe's old and new
// values per tier and the call that proposes them, or "" when the recipe on
// main would not change — or when this is no Mate with a group recipe at all.
func groupRecipeScalingSteer(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir, liveEnvPath, host string,
) string {
	comp, tiers, _, ok := planRecipeScaling(ctx, client, httpClient, rt, stateDir, liveEnvPath, host)
	parts := changedTiers(tiers)
	if !ok || len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("The group recipe on %s@%s still writes %s differently (%s). To carry this scale into the recipe the group's next Mates and environments are made from, call zerops_workflow action=\"group-recipe\" scaling=%q — it proposes only %s's verticalAutoscaling, floors applied, as a pull request the person reviews.",
		comp.groupRepo, comp.base, host, strings.Join(parts, "; "), host, host)
}

// scalingChangeText reads changes as "minRam 2 → 4, minFreeRamGB 0.5 → 1".
func scalingChangeText(changes []bundle.ScalingChange) string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		from, to := c.From, c.To
		if from == "" {
			from = "unset"
		}
		if to == "" {
			to = "unset"
		}
		out = append(out, c.Key+" "+from+" → "+to)
	}
	return strings.Join(out, ", ")
}

// tierTitle is a tier file's directory, as the group repo names it.
func tierTitle(path string) string {
	dir, _, _ := strings.Cut(path, "/")
	return dir
}

// handleGroupRecipeScaling is `zerops_workflow action="group-recipe"
// scaling=<host>`: a pull request on the group repo that changes host's
// verticalAutoscaling block, and nothing else, in each tier file whose block
// differs from this Mate's (floors applied). Cut from main's tip on the bot's
// fork; an earlier scaling proposal for the same host is closed first.
func handleGroupRecipeScaling(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir, liveEnvPath, host string,
) (*mcp.CallToolResult, any, error) {
	comp, tiers, outcome, ok := planRecipeScaling(ctx, client, httpClient, rt, stateDir, liveEnvPath, host)
	if !ok {
		reason := outcome.Blocked
		if reason == "" {
			reason = outcome.Line
		}
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			"The scaling proposal was not made: "+reason,
			"Retry once the Mate's Gitea variables have landed and a pair has its repository."), WithRecoveryStatus()), nil, nil
	}
	wiring := comp.wiring
	branch := ops.GiteaRecipeBranch(comp.main.Head) + "-scaling-" + host
	title := recipeScalingTitle(host)
	changed := changedTiers(tiers)
	if len(changed) == 0 {
		// An open proposal for host went stale: main already says it.
		if _, err := ops.CloseGiteaPullRequests(ctx, httpClient, wiring.GiteaURL, wiring.Token, comp.groupRepo, comp.bot, title, comp.base, ""); err != nil {
			return scalingProposalFailed(fmt.Sprintf("could not close the earlier proposal for %s (%v)", host, err)), nil, nil
		}
		return jsonResult(map[string]any{
			"groupRepo": comp.groupRepo,
			"message": fmt.Sprintf("The group recipe on %s@%s already writes %s as this Mate runs it (floors applied), or names no %s — nothing to propose.%s",
				comp.groupRepo, comp.base, host, host, refusedTiers(tiers)),
		}), nil, nil
	}

	fork, err := ops.EnsureGiteaFork(ctx, httpClient, wiring.GiteaURL, wiring.Token, comp.groupRepo, comp.bot)
	if err != nil {
		return scalingProposalFailed(fmt.Sprintf("could not fork %s (%v)", comp.groupRepo, err)), nil, nil
	}
	if _, err := ops.CloseGiteaPullRequests(ctx, httpClient, wiring.GiteaURL, wiring.Token, comp.groupRepo, comp.bot, title, comp.base, branch); err != nil {
		return scalingProposalFailed(fmt.Sprintf("could not close the earlier proposal for %s (%v)", host, err)), nil, nil
	}
	if _, err := ops.EnsureGiteaProposalBranch(ctx, httpClient, wiring.GiteaURL, wiring.Token, fork, branch, comp.base, comp.main.Head); err != nil {
		return scalingProposalFailed(fmt.Sprintf("could not cut %s@%s (%v)", fork, branch, err)), nil, nil
	}
	// Every tier that names host is written as this proposal has it — a
	// branch reused from an earlier proposal on the same head would
	// otherwise keep that proposal's scale on a tier this one leaves alone.
	files := make([]recipe.File, 0, len(tiers))
	for _, tier := range tiers {
		files = append(files, recipe.File{Path: tier.path, Body: tier.body})
	}
	if _, err := ops.PublishGiteaFiles(ctx, httpClient, wiring.GiteaURL, wiring.Token, fork, branch, comp.base,
		"recipe: "+host+"'s scale, from "+comp.inputs.MateProjectName, files); err != nil {
		return scalingProposalFailed(fmt.Sprintf("could not write the proposal to %s (%v)", fork, err)), nil, nil
	}
	number, _, err := ops.EnsureGiteaPullRequest(ctx, httpClient, wiring.GiteaURL, wiring.Token, comp.groupRepo, fork, branch, comp.base, title)
	if err != nil {
		return scalingProposalFailed(fmt.Sprintf("the proposal is on %s@%s but opening its pull request failed (%v)", fork, branch, err)), nil, nil
	}
	result := map[string]any{
		"groupRepo":   comp.groupRepo,
		"fork":        fork,
		"branch":      branch,
		"tiers":       changed,
		"pullRequest": number,
		"message": fmt.Sprintf("Proposed %s's scale to %s as pull request #%d: it changes only %s's verticalAutoscaling (%s). The person reviews and merges it like any recipe change.%s",
			host, comp.groupRepo, number, host, strings.Join(changed, "; "), refusedTiers(tiers)),
	}
	if number != 0 {
		result["pullRequestUrl"] = fmt.Sprintf("%s/%s/pulls/%d", strings.TrimRight(wiring.GiteaURL, "/"), comp.groupRepo, number)
	}
	return jsonResult(result), nil, nil
}

func scalingProposalFailed(reason string) *mcp.CallToolResult {
	return convertError(platform.NewPlatformError(platform.ErrAPIError,
		"The scaling proposal was not made: "+reason,
		"Retry the call; the project is unaffected."), WithRecoveryStatus())
}
