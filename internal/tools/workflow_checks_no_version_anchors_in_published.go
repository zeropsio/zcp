package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/internal/workflow"
)

// versionAnchorRegexp matches `v\d+(\.\d+)*` with word boundaries so it
// fires on v33, v8.86, v8.104 etc. (recipe-internal release anchors) but
// not on arbitrary "v"-prefixed identifiers embedded in longer words.
// Principle P6: version anchors belong in archival changelogs, not in
// published recipe content consumed by porters.
var versionAnchorRegexp = regexp.MustCompile(`\bv\d+(?:\.\d+)*\b`)

// versionAnchorScanGlobs names the globs inside a recipe project root the
// check reads. Per check-rewrite.md §16: `{host}/README.md`,
// `{host}/CLAUDE.md`, `environments/*/README.md`. The `{host}` pass here is
// any direct subdirectory of mountRoot (finalize-step runs after every
// per-codebase mount is finalized) — the check doesn't need the plan to
// know which subdirs are hostnames.
var versionAnchorScanGlobs = []string{
	"*/README.md",
	"*/CLAUDE.md",
	"environments/*/README.md",
}

// maxVersionAnchorExamples caps the number of (path, match) examples we
// surface in Detail so the error message stays readable even when the
// regression is wide. The full count is always reported separately.
const maxVersionAnchorExamples = 10

// checkNoVersionAnchorsInPublishedContent scans the mountRoot project tree
// for v33-class version leakage in published porter-facing content.
//
// mountRoot is the recipe project root (stateDir's parent) at test time
// and `/var/www`-analogous at container time. Graceful pass when the root
// does not exist — finalize-complete runs after all codebase mounts are
// resolved; a missing root indicates an upstream issue the close-entry
// trigger will surface via other checks.
func checkNoVersionAnchorsInPublishedContent(mountRoot string) []workflow.StepCheck {
	info, err := os.Stat(mountRoot)
	if err != nil || !info.IsDir() {
		return []workflow.StepCheck{{
			Name:   "no_version_anchors_in_published_content",
			Status: statusPass,
		}}
	}

	type hit struct {
		path  string
		match string
	}
	seen := map[string]bool{}
	var hits []hit
	for _, pattern := range versionAnchorScanGlobs {
		matches, _ := filepath.Glob(filepath.Join(mountRoot, pattern))
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			rel, relErr := filepath.Rel(mountRoot, m)
			if relErr != nil {
				rel = m
			}
			rel = filepath.ToSlash(rel)
			data, readErr := os.ReadFile(m)
			if readErr != nil {
				continue
			}
			for _, match := range versionAnchorRegexp.FindAllString(string(data), -1) {
				hits = append(hits, hit{path: rel, match: match})
			}
		}
	}
	if len(hits) == 0 {
		return []workflow.StepCheck{{
			Name:   "no_version_anchors_in_published_content",
			Status: statusPass,
		}}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].path != hits[j].path {
			return hits[i].path < hits[j].path
		}
		return hits[i].match < hits[j].match
	})
	examples := make([]string, 0, maxVersionAnchorExamples)
	for i, h := range hits {
		if i >= maxVersionAnchorExamples {
			break
		}
		examples = append(examples, fmt.Sprintf("%s: %q", h.path, h.match))
	}
	more := ""
	if len(hits) > maxVersionAnchorExamples {
		more = fmt.Sprintf(" (+%d more)", len(hits)-maxVersionAnchorExamples)
	}
	return []workflow.StepCheck{{
		Name:   "no_version_anchors_in_published_content",
		Status: statusFail,
		Detail: fmt.Sprintf(
			"%d version-anchor match(es) in published porter-facing content: %s%s. Recipe-internal release anchors (v33, v8.86, v20→v23) belong in archival changelogs, not in per-codebase README / CLAUDE.md / environments/*/README.md surfaces. Principle P6: published content is dateless. Remove or rephrase — a porter cloning this recipe has no context for \"per v34 we rotated the creds\"; write the invariant directly (\"the creds rotate when...\") and drop the version anchor.",
			len(hits), strings.Join(examples, "; "), more,
		),
	}}
}
