package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// phantomRecipePrefix is the directory-name prefix the v33 class invented
// under /var/www: the LLM, reasoning about "where does the recipe output
// live", manufactured `/var/www/recipe-<slug>/` trees in parallel with the
// canonical per-hostname mounts. The phantom tree accumulated stale copies
// of generated files + out-of-date READMEs that downstream agents then
// re-consumed as authoritative.
//
// Canonical output tree (positive allow-list per principle P8): one mount
// per declared hostname at depth 1 (`{host}dev`, `{host}stage`, `db`,
// `cache`, etc.), plus `environments/` at depth 1 for import.yaml packs.
// Anything matching the phantom prefix at depth ≤2 is a violation.
const phantomRecipePrefix = "recipe-"

// canonicalOutputTreeMaxDepth is the walk depth for the phantom-tree scan.
// The canonical tree only places per-hostname dirs at depth 1 under the
// recipe project root (which maps to /var/www at SSH time); the v33 class
// landed its phantom dirs at depths 1 and 2 (`/var/www/recipe-<slug>/` and
// `/var/www/<host>/recipe-<slug>/`). Depth 2 is the ceiling; deeper
// `recipe-*` directories (e.g. an npm package named `@pkg/recipe-helper`)
// are legitimate package naming and out of this check's scope.
const canonicalOutputTreeMaxDepth = 2

// checkCanonicalOutputTreeOnly walks mountRoot up to depth 2 and fails on
// any directory whose basename begins with `recipe-`. mountRoot is the
// recipe-project root at test time and `/var/www` under SSH at container
// time. Graceful pass when mountRoot does not exist.
//
// Emits a single check (`canonical_output_tree_only`) — the violation is
// a whole-recipe concern, not a per-codebase concern, so the name stays
// un-prefixed (same style as `cross_readme_gotcha_uniqueness`).
func checkCanonicalOutputTreeOnly(mountRoot string) []workflow.StepCheck {
	info, err := os.Stat(mountRoot)
	if err != nil || !info.IsDir() {
		return []workflow.StepCheck{{
			Name:   "canonical_output_tree_only",
			Status: statusPass,
		}}
	}

	var phantoms []string
	_ = filepath.WalkDir(mountRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // best-effort scan; missing files mean "no phantom there", not a check failure
		}
		if path == mountRoot {
			return nil
		}
		rel, relErr := filepath.Rel(mountRoot, path)
		if relErr != nil {
			return nil //nolint:nilerr // an unrepresentable rel path means we can't report this entry; keep walking
		}
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		if depth > canonicalOutputTreeMaxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), phantomRecipePrefix) {
			phantoms = append(phantoms, filepath.ToSlash(rel))
		}
		return nil
	})

	if len(phantoms) == 0 {
		return []workflow.StepCheck{{
			Name:   "canonical_output_tree_only",
			Status: statusPass,
		}}
	}

	sort.Strings(phantoms)
	return []workflow.StepCheck{{
		Name:   "canonical_output_tree_only",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"phantom `recipe-*` directories at or below depth %d under the project root: %s. The canonical output tree places per-hostname dirs at depth 1 (apidev/, appdev/, workerdev/, db/, environments/) — anything matching `recipe-*` is the v33 invention class (LLM-manufactured parallel tree that downstream agents then re-consumed as authoritative). Remove these directories before close: ssh {host} \"rm -rf %s\" (run per phantom path against the container mount).",
			canonicalOutputTreeMaxDepth, strings.Join(phantoms, ", "), strings.Join(phantoms, " "),
		),
	}}
}
