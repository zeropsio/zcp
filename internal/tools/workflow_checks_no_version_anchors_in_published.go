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
//
// Post-Cx-VERSION-ANCHOR-SHARPEN the regex is paired with two sharpen
// filters applied in scanForVersionAnchors: (a) fenced code blocks are
// excluded so YAML examples that reference identifier strings like
// `bootstrap-seed-v1` don't trip the check, and (b) a match whose
// character immediately preceding the `v` is `-` is a compound
// identifier (the regex match is the tail of a hyphenated slug), not
// an anchor, and is also skipped.
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

// scanForVersionAnchors strips fenced code blocks from content, then
// scans the prose for version-anchor matches. A match whose immediately
// preceding character is `-` is treated as a compound identifier tail
// (e.g. `bootstrap-seed-v1`, `nestjs-minimal-v3`) and skipped — such
// identifiers are slug-class tokens, not recipe-run anchors a porter
// would have no context for.
//
// Fenced-code detection is line-oriented and triggers on a line whose
// trimmed prefix starts with three or more backticks (same shape Markdown
// renderers accept). Nested fences are not handled — no production atom
// or README uses them today — and indented code blocks are intentionally
// out of scope: the sharpen targets YAML/bash examples, where the fenced
// form dominates.
func scanForVersionAnchors(content string) []string {
	lines := strings.Split(content, "\n")
	var prose strings.Builder
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			prose.WriteByte('\n')
			continue
		}
		if inFence {
			prose.WriteByte('\n')
			continue
		}
		prose.WriteString(line)
		prose.WriteByte('\n')
	}
	proseText := prose.String()

	var matches []string
	for _, idx := range versionAnchorRegexp.FindAllStringIndex(proseText, -1) {
		start, end := idx[0], idx[1]
		// Preceding character check — compound identifier tails have `-`
		// (e.g. `bootstrap-seed-v1`). Only the boundary character matters;
		// the regex already anchors on a word boundary so the left side
		// is either document start, whitespace, or punctuation we accept.
		if start > 0 && proseText[start-1] == '-' {
			continue
		}
		matches = append(matches, proseText[start:end])
	}
	return matches
}

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
			for _, match := range scanForVersionAnchors(string(data)) {
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
