package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
)

// A Mate's group recipe is what a second Mate (Add Mate), the group's stage
// and its production are built from: a project with no recipe on its
// recipe repository's main breaks every one of them (the Mate Web Mate,
// 2026-10-27, whose group repository stayed empty). So a pass that cannot
// propose a tier main lacks — a part whose stage setup nothing records and
// no zerops.yaml tells — makes it a required step: on the bootstrap
// response, beside a develop start's result, and at the head of status
// (StateEnvelope.Required), each naming what is missing and the call that
// fixes it. Every recipe pass — a zcp start, a bootstrap close, an adopt, a
// develop start, the agent's group-recipe — tries again, and the step is
// gone on the pass that proposes the tiers or finds main carrying them.

// recipeRequiredFile records the steps the last recipe pass found required,
// beside the per-pair records under the HQ state directory: a name with a
// dash, which no hostname has. Status reads it — it calls HQ for nothing.
const recipeRequiredFile = "group-recipe.json"

// recipeRequiredRecord is what recipeRequiredFile holds.
type recipeRequiredRecord struct {
	Required []string `json:"required"`
}

// recipeRequiredStep is the required step for the tiers main lacks (tiers,
// by directory) that blockers withhold: what is missing per part, and the
// way out — the stage's setup named by set-default-setup (chosen from the
// setups its zerops.yaml declares when it declares several), or, with no
// zerops.yaml read, the code with its zerops.yaml where zcp reads it (the
// dev checkout, or main of its repository in HQ for a part whose dev half
// is a pool, which holds no checkout) or a deploy of the stage, which
// records its setup. pooled names the dev halves that are pools.
func recipeRequiredStep(blockers []bundle.GroupRecipeBlocker, pooled map[string]bool, tiers []string) string {
	ways := make([]string, 0, len(blockers))
	for _, b := range blockers {
		target := firstNonEmptySetup(b.Stage, b.Runtime)
		choice := "<the setup " + target + " builds>"
		if len(b.Declared) > 0 {
			choice = "<one of " + strings.Join(b.Declared, ", ") + ">"
		}
		name := fmt.Sprintf(`zerops_workflow action="set-default-setup" targetService=%q setup=%q stageSetup=%q`, target, b.DevSetup, choice)
		way := fmt.Sprintf("part %q of %q: %s — name the setup its stage builds: %s", target, b.Runtime, b.Why, name)
		if len(b.Declared) == 0 {
			where := "in the dev checkout"
			if pooled[b.Runtime] {
				where = "on main of its repository in HQ"
			}
			way += fmt.Sprintf(", or put the code with its zerops.yaml %s (zcp reads it there) or deploy the stage once (zerops_deploy targetService=%q records its setup)", where, target)
		}
		ways = append(ways, way)
	}
	return fmt.Sprintf("the group recipe — the tiers a second Mate and the group's stage and production are built from — cannot be proposed: main of the recipe repository lacks %s, and %s. Then call zerops_workflow action=\"group-recipe\", or let the next develop start try again.",
		strings.Join(tiers, ", "), strings.Join(ways, "; "))
}

// readRecipeRequired is what the last recipe pass recorded as required;
// none when it recorded nothing or the record cannot be read.
func readRecipeRequired(stateDir string) []string {
	data, err := os.ReadFile(filepath.Join(stateDir, hqStateDir, recipeRequiredFile))
	if err != nil {
		return nil
	}
	var record recipeRequiredRecord
	if json.Unmarshal(data, &record) != nil {
		return nil
	}
	return record.Required
}

// writeRecipeRequired records required, and removes the record when nothing
// is. Best-effort: a record that cannot be written costs status the step
// until the next pass, never the pass.
func writeRecipeRequired(stateDir string, required []string) {
	path := filepath.Join(stateDir, hqStateDir, recipeRequiredFile)
	if len(required) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "zcp: group recipe record: %v\n", err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: group recipe record: %v\n", err)
		return
	}
	data, err := json.Marshal(recipeRequiredRecord{Required: required})
	if err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: group recipe record: %v\n", err)
	}
}

// recipeTiersLacking are the tier directories main (holding paths) lacks, of
// those the recipe composes, in tier order.
func recipeTiersLacking(paths []string) []string {
	var lacking []string
	for _, tier := range recipeTierDirs() {
		carried := false
		for _, path := range paths {
			if strings.HasPrefix(path, tier+"/") {
				carried = true
				break
			}
		}
		if !carried {
			lacking = append(lacking, tier)
		}
	}
	return lacking
}

// recipeTierDirs are the recipe's tier directories in tier order, as HQ
// names their files (hq.RecipeTierPaths).
func recipeTierDirs() []string {
	dirs := make([]string, 0, len(hq.RecipeTierPaths))
	for _, tier := range []string{hq.RecipeTierMate, hq.RecipeTierStage, hq.RecipeTierProduction} {
		dir, _, _ := strings.Cut(hq.RecipeTierPaths[tier], "/")
		dirs = append(dirs, dir)
	}
	return dirs
}
