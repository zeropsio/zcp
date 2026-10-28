package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops/bundle"
	"github.com/zeropsio/zcp/internal/platform"
)

// A Mate's recipe change is zcp's only while its branch holds what zcp last
// pushed there. Anyone else's commit on it — the Mate's agent editing a tier
// it disagrees with, a person in HQ — makes the change theirs, and zcp pushes
// nothing over it again: on flip.studio's change #2 (2026-10-28) the agent
// pushed stage and production without the owner's cloud agent, and every
// pass after pushed zcp's composition back over it.
//
// What tells zcp's pushes from others' is the tree zcp last pushed on the
// change, which it keeps (recipePushedDir): who authored a commit cannot tell
// (the agent's commits carry zcp's own robot identity, ops.DeployGitIdentity),
// nor can its subject (the agent words it as it likes). A pass then pushes
// only when what it composes differs from that tree — never because the
// branch differs from the composition. A change no record names — pushed by a
// zcp from before the record, or handed over from a Mate that is gone — is
// read once by zcp's mark (bundle.TierStamp): it is zcp's while every tier
// file it changes beyond the main it last took in is exactly as a zcp wrote
// it, and the record is kept from then on.

// recipePushedDir keeps, per change, the tree zcp last pushed on it:
// <stateDir>/hq-recipe-pushed/<appID>-<number>.json.
const recipePushedDir = "hq-recipe-pushed"

// recipePushedRecord is the tree zcp last pushed on a recipe change.
type recipePushedRecord struct {
	Tree string `json:"tree"`
}

func recipePushedPath(stateDir, appID string, number int) string {
	return filepath.Join(stateDir, recipePushedDir, appID+"-"+strconv.Itoa(number)+".json")
}

// readRecipePushed is the tree zcp last pushed on change number of appID's
// recipe repository; ok is false when no record names it.
func readRecipePushed(stateDir, appID string, number int) (tree string, ok bool) {
	data, err := os.ReadFile(recipePushedPath(stateDir, appID, number))
	if err != nil {
		return "", false
	}
	var record recipePushedRecord
	if json.Unmarshal(data, &record) != nil || record.Tree == "" {
		return "", false
	}
	return record.Tree, true
}

// writeRecipePushed keeps tree as the one zcp last pushed on change number.
// Best-effort: a record that cannot be written leaves the next pass reading
// the change as someone else's, which pushes nothing over anything.
func writeRecipePushed(stateDir, appID string, number int, tree string) {
	path := recipePushedPath(stateDir, appID, number)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: recipe change record: %v\n", err)
		return
	}
	data, err := json.Marshal(recipePushedRecord{Tree: tree})
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: recipe change record: %v\n", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: recipe change record: %v\n", err)
	}
}

// recipeChangeIsZcps reports whether the branch of change number, at head,
// still holds what zcp last pushed there: its tree the recorded one, or, with
// no record, every tier file it changes beyond its merge base with main
// carrying an intact zcp mark.
func recipeChangeIsZcps(ctx context.Context, comp groupRecipeComposition, number int, head string) (bool, error) {
	held, err := comp.scratch.Tree(ctx, head)
	if err != nil {
		return false, err
	}
	if pushed, ok := readRecipePushed(comp.stateDir, comp.appID, number); ok {
		return held == pushed, nil
	}
	base, err := comp.scratch.MergeBase(ctx, head, comp.main)
	if err != nil {
		return false, err
	}
	paths := make([]string, 0, len(hq.RecipeTierPaths))
	for _, path := range hq.RecipeTierPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		body, found, err := comp.scratch.File(ctx, head, path)
		if err != nil {
			return false, err
		}
		if !found {
			continue
		}
		was, wasFound, err := comp.scratch.File(ctx, base, path)
		if err != nil {
			return false, err
		}
		if wasFound && was == body {
			continue
		}
		if _, intact, stamped := bundle.TierStamp(body); !stamped || !intact {
			return false, nil
		}
	}
	return true, nil
}

// recipeEditedLine is what a pass says of change number, at url, whose
// branch holds a commit zcp did not write.
func recipeEditedLine(number int, url string) string {
	return fmt.Sprintf("change #%d (%s) of the recipe repository %q holds a commit zcp did not write — the Mate's own edit or a person's — so the change is theirs and zcp pushes nothing over it: it lands or closes in HQ as they leave it",
		number, url, hq.RecipeRepo)
}

// recipeEditedRefusal is a targeted proposal (what, e.g. "The scaling
// proposal") refused because the Mate's change it would go on holds a commit
// zcp did not write.
func recipeEditedRefusal(what string, comp groupRecipeComposition, written recipeWrite) *mcp.CallToolResult {
	url := comp.hqc.ChangeURL(comp.appID, hq.RecipeRepo, written.number)
	return convertError(platform.NewPlatformError(platform.ErrInvalidParameter,
		fmt.Sprintf("%s was not made: this Mate's change #%d in the recipe repository %q holds a commit zcp did not write — the Mate's own edit or a person's — and zcp pushes nothing over it.",
			what, written.number, hq.RecipeRepo),
		fmt.Sprintf("Edit the change's branch %q yourself, or have change #%d merged or closed in HQ (%s) and call again.", written.branch, written.number, url)),
		WithRecoveryStatus())
}
