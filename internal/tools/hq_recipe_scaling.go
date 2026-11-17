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
	"github.com/zeropsio/zcp/internal/schema"
)

// A tier on main of the recipe repository is the group's, and the additive
// reconcile never proposes over it (reconcileGroupRecipe). A service's scale
// is what a Mate learns by running the app — a Meilisearch that ran out of
// memory reindexing at the recipe's 1 GB — so a scale change has its own,
// narrow way back to the group: the scale tool says when the recipe on main
// differs and names the call, and that call proposes ONE host's
// verticalAutoscaling block in each tier file as the Mate's change in the
// recipe repository, the recipe's floors applied (bundle.ApplyResourceFloor),
// which HQ lands by itself unless its merge would change an environment made
// from a tier (recipeLanding). Nothing is proposed on its own: a
// Mate's experiment with its own scale is not the group's until someone says
// so. This Mate keeps one recipe proposal open at a time, so a scaling
// proposal and the additive one never write over each other: whichever finds
// the other open leaves it as it is and says so.

// recipeScalingTitle is the title of a scaling proposal's change.
func recipeScalingTitle(host string) string {
	return "Mate: " + host + "'s scale in the group recipe"
}

// recipeScalingTier is one tier file on main that names a scaling
// proposal's host: the hostname the tier runs it under (host, the composer's
// name for its part there), what the proposal writes to it — that entry's
// block replaced, or main's own body when nothing changes or the splice
// refused (why, in refused) — and the keys that change.
type recipeScalingTier struct {
	path    string
	host    string
	body    string
	changes []bundle.ScalingChange
	refused string
}

// recipeScalingPlan is a scaling proposal's tiers, and the tiers of the
// composed recipe main holds that have no entry for the proposal's host
// (absent), which it leaves alone.
type recipeScalingPlan struct {
	tiers  []recipeScalingTier
	absent []string
}

// tierHostname is the hostname the tier at path runs host under: the
// composer's name for host's part there (tierHosts), else host's own.
func tierHostname(tierHosts map[string]map[string]string, path, host string) string {
	if name, ok := tierHosts[path][host]; ok {
		return name
	}
	return host
}

// planRecipeScaling is, for each tier file of the composed recipe that main
// holds and that names host under the tier's own hostname for it
// (tierHostname: a pair's stage `appstage` is `app` in a group environment),
// what replacing that entry's verticalAutoscaling block with the composed
// one would change. Tiers are read at main, the commit a proposal is written
// over.
func planRecipeScaling(ctx context.Context, scratch *hq.Scratch, main string, files []recipe.File, tierHosts map[string]map[string]string, host string) (recipeScalingPlan, error) {
	var plan recipeScalingPlan
	for _, file := range files {
		if !strings.HasSuffix(file.Path, "/import.yaml") {
			continue
		}
		mainBody, found, err := scratch.File(ctx, main, file.Path)
		if err != nil {
			return recipeScalingPlan{}, fmt.Errorf("could not read %q of the recipe repository %q (%w)", file.Path, hq.RecipeRepo, err)
		}
		if !found {
			continue
		}
		name := tierHostname(tierHosts, file.Path, host)
		if !bundle.TierNamesHost(mainBody, name) {
			plan.absent = append(plan.absent, tierTitle(file.Path))
			continue
		}
		tier := recipeScalingTier{path: file.Path, host: name, body: mainBody}
		if changes := bundle.HostScalingChanges(mainBody, file.Body, name); len(changes) > 0 {
			spliced, err := bundle.SpliceHostScaling(mainBody, file.Body, name)
			switch {
			case err != nil:
				tier.refused = err.Error()
			case spliced != mainBody:
				tier.body, tier.changes = spliced, changes
			}
		}
		plan.tiers = append(plan.tiers, tier)
	}
	return plan, nil
}

// changedTiers reads the tiers a proposal changes as "Tier: key old → new",
// or "Tier (name): …" where the tier runs host under another name.
func changedTiers(tiers []recipeScalingTier, host string) []string {
	var out []string
	for _, tier := range tiers {
		if len(tier.changes) > 0 {
			title := tierTitle(tier.path)
			if tier.host != host {
				title += " (" + tier.host + ")"
			}
			out = append(out, title+": "+scalingChangeText(tier.changes))
		}
	}
	return out
}

// absentTiers says which tiers have no entry for host and are left alone.
func absentTiers(absent []string, host string) string {
	if len(absent) == 0 {
		return ""
	}
	return fmt.Sprintf(" %s run no %s, and stay as they are.", strings.Join(absent, ", "), host)
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
	schemas *schema.Cache,
) string {
	comp, _, ok := composeGroupRecipe(ctx, client, httpClient, rt, stateDir, schemas)
	if !ok {
		return ""
	}
	defer comp.close()
	plan, err := planRecipeScaling(ctx, comp.scratch, comp.main, comp.files, comp.tierHosts, host)
	parts := changedTiers(plan.tiers, host)
	if err != nil || len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("The group recipe on %q of the recipe repository %q still writes %s differently (%s). To carry this scale into the recipe the group's next Mates and environments are made from, call zerops_workflow action=\"group-recipe\" scaling=%q — it proposes only %s's verticalAutoscaling, floors applied, as the Mate's recipe change.",
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
	schemas *schema.Cache,
) (*mcp.CallToolResult, any, error) {
	comp, outcome, ok := composeGroupRecipe(ctx, client, httpClient, rt, stateDir, schemas)
	if !ok {
		reason := outcome.Blocked
		if reason == "" {
			reason = outcome.Line
		}
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			"The scaling proposal was not made: "+reason,
			"Retry once this Mate is linked to its HQ and a pair has its repository there."), WithRecoveryStatus()), nil, nil
	}
	defer comp.close()
	var plan recipeScalingPlan
	written, err := writeRecipeChange(ctx, comp, openRecipeChange(comp.state), recipeScalingTitle(host),
		"recipe: "+host+"'s scale, from "+comp.inputs.MateProjectName,
		func(ctx context.Context, scratch *hq.Scratch, main string, _ []string) (map[string]string, error) {
			planned, err := planRecipeScaling(ctx, scratch, main, comp.files, comp.tierHosts, host)
			plan = planned
			files := make(map[string]string, len(planned.tiers))
			for _, tier := range planned.tiers {
				files[tier.path] = tier.body
			}
			return files, err
		})
	if err != nil {
		return scalingProposalFailed(err.Error()), nil, nil
	}
	tiers := plan.tiers
	changed := changedTiers(tiers, host)
	result := map[string]any{"groupRepo": hq.RecipeRepo}
	if written.edited && len(changed) > 0 {
		return recipeEditedRefusal("The scaling proposal", comp, written), nil, nil
	}
	if written.other != nil && len(changed) > 0 {
		return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("The scaling proposal was not made: this Mate's change #%d in the recipe repository %q (%q) is open, and this Mate keeps one recipe proposal open at a time.",
				written.other.number, hq.RecipeRepo, written.other.title),
			fmt.Sprintf("Retry once change #%d is merged or closed (%s).", written.other.number,
				comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.other.number))), WithRecoveryStatus()), nil, nil
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
		result["message"] = message + refusedTiers(tiers) + absentTiers(plan.absent, host) + recipeEditSteer
		return jsonResult(result), nil, nil
	}
	result["branch"] = written.branch
	result["tiers"] = changed
	result["message"] = fmt.Sprintf("Proposed %s's scale to the recipe repository %q as change #%d (%s): it changes only %s's verticalAutoscaling (%s). %s.%s",
		host, hq.RecipeRepo, written.number, result["changeUrl"], host, strings.Join(changed, "; "), recipeLanding, refusedTiers(tiers)+absentTiers(plan.absent, host))
	return jsonResult(result), nil, nil
}

func scalingProposalFailed(reason string) *mcp.CallToolResult {
	return convertError(platform.NewPlatformError(platform.ErrAPIError,
		"The scaling proposal was not made: "+reason,
		"Retry the call; the project is unaffected."), WithRecoveryStatus())
}
