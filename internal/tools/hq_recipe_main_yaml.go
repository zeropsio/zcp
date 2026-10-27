package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
)

// A pair's zerops.yaml decides what the recipe composes — the setup a stage
// builds when none is recorded, what each part's build reads, a part's type
// when its stage does not run — and a pool holds no checkout zcp can read,
// nor does a dev checkout before develop puts the code in. Its repository's
// main in HQ does, so the recipe reads it there: the root's zerops.yaml, else
// its zerops.yml.

// recipeMainYAMLDir keeps, per repository, the zerops.yaml its main held at
// a head, under the HQ state directory (a subdirectory no pair's hostname can
// name), so a pass reads a large repository's tree only once its main moved.
const recipeMainYAMLDir = "zerops-yaml"

// recipeMainYAMLRecord is what a repository's main held at Head: its root's
// zerops.yaml, "" for none.
type recipeMainYAMLRecord struct {
	Head string `json:"head"`
	Body string `json:"body"`
}

// recipeMainYAML is the reader of a repository's zerops.yaml off its main in
// HQ for the application appID: the head asked first (no fetch), the record
// kept for that head answering when it has one, else the tip alone fetched
// into a scratch of zcp's own and its file read. Any failure reads as none.
func recipeMainYAML(hqc hq.Client, appID, stateDir string) recipeRepoYAML {
	return func(ctx context.Context, repo string) string {
		scratch, err := hqc.OpenScratch(ctx, appID, repo)
		if err != nil {
			return ""
		}
		defer scratch.Close()
		head, found, err := scratch.RemoteHead(ctx, hqBase)
		if err != nil || !found {
			return ""
		}
		path := filepath.Join(stateDir, hqStateDir, recipeMainYAMLDir, appID+"-"+repo+".json")
		if kept, ok := readRecipeMainYAML(path); ok && kept.Head == head {
			return kept.Body
		}
		tip, found, err := scratch.FetchTip(ctx, hqBase)
		if err != nil || !found {
			return ""
		}
		body := ""
		for _, name := range []string{"zerops.yaml", "zerops.yml"} {
			if file, ok, err := scratch.File(ctx, tip, name); err == nil && ok {
				body = strings.TrimRight(file, "\n")
				break
			}
		}
		writeRecipeMainYAML(path, recipeMainYAMLRecord{Head: tip, Body: body})
		return body
	}
}

func readRecipeMainYAML(path string) (recipeMainYAMLRecord, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return recipeMainYAMLRecord{}, false
	}
	var record recipeMainYAMLRecord
	if json.Unmarshal(data, &record) != nil {
		return recipeMainYAMLRecord{}, false
	}
	return record, true
}

// writeRecipeMainYAML keeps the record. Best-effort: one that cannot be
// written costs the next pass a fetch, never a recipe.
func writeRecipeMainYAML(path string, record recipeMainYAMLRecord) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: recipe zerops.yaml record: %v\n", err)
		return
	}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: recipe zerops.yaml record: %v\n", err)
	}
}
