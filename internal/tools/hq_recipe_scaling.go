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

// A tier on main of the recipe repository is the group's, and the additive
// reconcile never proposes over it (reconcileGroupRecipe). A service's scale
// is what a Mate learns by running the app — a Meilisearch that ran out of
// memory reindexing at the recipe's 1 GB — so a scale change has its own,
// narrow way back to the group: the scale tool says when the recipe on main
// differs and names the call, and that call proposes ONE host's
// verticalAutoscaling block in each tier file as the Mate's change in the
// recipe repository, which a person reviews and merges, the recipe's floors
// applied (bundle.ApplyResourceFloor). Nothing is proposed on its own: a
// Mate's experiment with its own scale is not the group's until someone says
// so. HQ keeps one open change per Mate per repository, so a scaling
// proposal and the additive one never write over each other: whichever finds
// the other open leaves it as it is and says so.

// recipeScalingTitle is the title of a scaling proposal's change.
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

// planRecipeScaling is, for each tier file of the composed recipe that main
// holds and that names host, what replacing host's verticalAutoscaling block
// with the composed one would change. Tiers are read at main, the commit a
// proposal is written over.
func planRecipeScaling(ctx context.Context, scratch *hq.Scratch, main string, files []recipe.File, host string) ([]recipeScalingTier, error) {
	var tiers []recipeScalingTier
	for _, file := range files {
		if !strings.HasSuffix(file.Path, "/import.yaml") {
			continue
		}
		mainBody, found, err := scratch.File(ctx, main, file.Path)
		if err != nil {
			return nil, fmt.Errorf("could not read %q of the recipe repository %q (%w)", file.Path, hq.RecipeRepo, err)
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
	return tiers, nil
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
// main would not change — or when this is no Mate with a recipe repository
// to read.
func groupRecipeScalingSteer(
	ctx context.Context,
	client platform.Client,
	httpClient ops.HTTPDoer,
	rt runtime.Info,
	stateDir, host string,
) string {
	comp, _, ok := composeGroupRecipe(ctx, client, httpClient, rt, stateDir)
	if !ok {
		return ""
	}
	scratch, err := comp.hqc.OpenScratch(ctx, comp.appID, hq.RecipeRepo)
	if err != nil {
		return ""
	}
	defer scratch.Close()
	main, found, err := scratch.Fetch(ctx, hqBase)
	if err != nil || !found {
		return ""
	}
	tiers, err := planRecipeScaling(ctx, scratch, main, comp.files, host)
	parts := changedTiers(tiers)
	if err != nil || len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("The group recipe on %q of the recipe repository %q still writes %s differently (%s). To carry this scale into the recipe the group's next Mates and environments are made from, call zerops_workflow action=\"group-recipe\" scaling=%q — it proposes only %s's verticalAutoscaling, floors applied, as a change the person reviews.",
		hqBase, hq.RecipeRepo, host, strings.Join(parts, "; "), host, host)
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

// tierTitle is a tier file's directory, as the recipe repository names it.
func tierTitle(path string) string {
	dir, _, _ := strings.Cut(path, "/")
	return dir
}

// handleGroupRecipeScaling is `zerops_workflow action="group-recipe"
// scaling=<host>`: the Mate's change in the recipe repository, titled for
// host, that changes host's verticalAutoscaling block, and nothing else, in
// each tier file whose block differs from this Mate's (floors applied).
// Every tier that names host is written as this proposal has it, over main:
// an earlier proposal for the same host moves forward on the same change,
// and one with nothing left to propose is brought to main's tree.
func handleGroupRecipeScaling(
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
			"The scaling proposal was not made: "+reason,
			"Retry once this Mate is enrolled with its HQ and a pair has its repository there."), WithRecoveryStatus()), nil, nil
	}
	var tiers []recipeScalingTier
	written, err := writeRecipeChange(ctx, comp.hqc, comp.appID, openRecipeChange(comp.state), recipeScalingTitle(host),
		"recipe: "+host+"'s scale, from "+comp.inputs.MateProjectName,
		func(ctx context.Context, scratch *hq.Scratch, main string, _ []string) (map[string]string, error) {
			planned, err := planRecipeScaling(ctx, scratch, main, comp.files, host)
			tiers = planned
			files := make(map[string]string, len(planned))
			for _, tier := range planned {
				files[tier.path] = tier.body
			}
			return files, err
		})
	if err != nil {
		return scalingProposalFailed(err.Error()), nil, nil
	}
	changed := changedTiers(tiers)
	result := map[string]any{"groupRepo": hq.RecipeRepo}
	if written.other != nil && len(changed) > 0 {
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("The scaling proposal was not made: this Mate's change #%d in the recipe repository %q (%q) is open, and HQ keeps one open change per Mate per repository.",
				written.other.Number, hq.RecipeRepo, written.other.Title),
			fmt.Sprintf("Retry once change #%d is merged or closed (%s).", written.other.Number,
				comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.other.Number))), WithRecoveryStatus()), nil, nil
	}
	if written.number != 0 {
		result["change"] = written.number
		result["changeUrl"] = comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.number)
	}
	if len(changed) == 0 {
		message := fmt.Sprintf("The group recipe on %q of the recipe repository %q already writes %s as this Mate runs it (floors applied), or names no %s — nothing to propose.",
			hqBase, hq.RecipeRepo, host, host)
		if written.pushed {
			message += fmt.Sprintf(" Change #%d, which proposed it before, now adds nothing.", written.number)
		}
		result["message"] = message + refusedTiers(tiers)
		return jsonResult(result), nil, nil
	}
	result["branch"] = comp.hqc.ChangeBranch(written.number)
	result["tiers"] = changed
	result["message"] = fmt.Sprintf("Proposed %s's scale to the recipe repository %q as change #%d (%s): it changes only %s's verticalAutoscaling (%s). The person reviews and merges it like any recipe change.%s",
		host, hq.RecipeRepo, written.number, result["changeUrl"], host, strings.Join(changed, "; "), refusedTiers(tiers))
	return jsonResult(result), nil, nil
}

func scalingProposalFailed(reason string) *mcp.CallToolResult {
	return convertError(platform.NewPlatformError(platform.ErrAPIError,
		"The scaling proposal was not made: "+reason,
		"Retry the call; the project is unaffected."), WithRecoveryStatus())
}
