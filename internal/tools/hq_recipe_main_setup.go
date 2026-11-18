package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/hq"
)

// A pair's zerops.yaml decides what the recipe composes — the setup a stage
// builds when none is recorded, what each part's build reads, a part's type
// when its stage does not run — and a pool holds no checkout zcp can read,
// nor does a dev checkout before develop puts the code in. Its repository's
// main in HQ does, so the recipe reads it there: the root's zerops.yaml, else
// its zerops.yml. Main also says how a part is developed: it holds the part's
// pool setup or not (ops.CheckPoolSetups over that zerops.yaml and the
// dev-server declaration), so the reader answers both files of one head.

// recipeMainSetupDir keeps, per repository, the two files its main held at a
// head, under the HQ state directory (a subdirectory no pair's hostname can
// name), so a pass reads a large repository's tree only once its main moved.
const recipeMainSetupDir = "main-setup"

// recipeMainSetup is what a repository's main holds of its parts' setup: its
// root's zerops.yaml (else zerops.yml) and its dev-server declaration
// (devsup.DeclarationPath), each "" for none.
type recipeMainSetup struct {
	ZeropsYAML  string `json:"zeropsYaml"`
	Declaration string `json:"declaration"`
}

// recipeMainSetupRecord is what a repository's main held at Head.
type recipeMainSetupRecord struct {
	Head  string          `json:"head"`
	Setup recipeMainSetup `json:"setup"`
}

// recipeRepoSetup reads a repository's setup files off its main in HQ. A nil
// error is HQ's answer: both "" when main holds neither, or when the
// repository is empty or has no main. An error is a main that could not be
// read, which proves nothing about what it holds.
type recipeRepoSetup func(ctx context.Context, repo string) (recipeMainSetup, error)

// recipeMainSetupReader is the reader of a repository's setup files off its
// main in HQ for the application appID: the head asked first (no fetch), the
// record kept for that head answering when it has one, else the tip alone
// fetched into a scratch of zcp's own and its files read. The record is
// written only when every file read answered, so a read cut short never
// stands for an absent file at that head.
func recipeMainSetupReader(hqc hq.Client, appID, stateDir string) recipeRepoSetup {
	return func(ctx context.Context, repo string) (recipeMainSetup, error) {
		scratch, err := hqc.OpenScratch(ctx, appID, repo)
		if err != nil {
			return recipeMainSetup{}, fmt.Errorf("main of %s: %w", repo, err)
		}
		defer scratch.Close()
		head, found, err := scratch.RemoteHead(ctx, hqBase)
		if err != nil {
			return recipeMainSetup{}, fmt.Errorf("main of %s: %w", repo, err)
		}
		if !found {
			return recipeMainSetup{}, nil
		}
		path := filepath.Join(stateDir, hqStateDir, recipeMainSetupDir, appID+"-"+repo+".json")
		if kept, ok := readRecipeMainSetup(path); ok && kept.Head == head {
			return kept.Setup, nil
		}
		tip, found, err := scratch.FetchTip(ctx, hqBase)
		if err != nil {
			return recipeMainSetup{}, fmt.Errorf("main of %s: %w", repo, err)
		}
		if !found {
			return recipeMainSetup{}, nil
		}
		var setup recipeMainSetup
		for _, name := range []string{"zerops.yaml", "zerops.yml"} {
			file, ok, err := scratch.File(ctx, tip, name)
			if err != nil {
				return recipeMainSetup{}, fmt.Errorf("main of %s: %s: %w", repo, name, err)
			}
			if ok {
				setup.ZeropsYAML = strings.TrimRight(file, "\n")
				break
			}
		}
		file, ok, err := scratch.File(ctx, tip, devsup.DeclarationPath)
		if err != nil {
			return recipeMainSetup{}, fmt.Errorf("main of %s: %s: %w", repo, devsup.DeclarationPath, err)
		}
		if ok {
			setup.Declaration = file
		}
		writeRecipeMainSetup(path, recipeMainSetupRecord{Head: tip, Setup: setup})
		return setup, nil
	}
}

func readRecipeMainSetup(path string) (recipeMainSetupRecord, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return recipeMainSetupRecord{}, false
	}
	var record recipeMainSetupRecord
	if json.Unmarshal(data, &record) != nil {
		return recipeMainSetupRecord{}, false
	}
	return record, true
}

// writeRecipeMainSetup keeps the record. Best-effort: one that cannot be
// written costs the next pass a fetch, never a recipe.
func writeRecipeMainSetup(path string, record recipeMainSetupRecord) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: recipe main setup record: %v\n", err)
		return
	}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: recipe main setup record: %v\n", err)
	}
}
