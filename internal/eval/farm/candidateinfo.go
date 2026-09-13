package farm

import (
	"bytes"
	"debug/buildinfo"
)

// MinCorpusMarkers is the refusal threshold for CorpusMarkerCount
// (docs/spec-eval-farm.md FM-66): a candidate binary embedding fewer than
// this many `guiSlug: "` recipe markers was built from a tree whose recipe
// corpus was never pulled (internal/knowledge/recipes/*.md is gitignored —
// CLAUDE.md "Knowledge sync"), not from a real local build (47 markers as
// of 2026-09-13).
const MinCorpusMarkers = 20

// CorpusMarkerCount counts occurrences of the literal `guiSlug: "` in data
// — the recipe frontmatter field internal/knowledge/documents.go embeds
// from disk at build time (`//go:embed … all:recipes …`), one per recipe.
// A candidate binary built from a worktree/fresh clone with no recipe .md
// files on disk embeds zero (docs/spec-eval-farm.md FM-66).
func CorpusMarkerCount(data []byte) int {
	return bytes.Count(data, []byte(`guiSlug: "`))
}

// CandidateInfoKey returns the bucket key `farm push --candidate` stores a
// candidate binary's build info under: the literal
// "candidates/<sha256>.info.json", sibling to (never nested under) the
// binary's own "candidates/<sha256>/zcp" object (docs/spec-eval-farm.md
// §3.3).
func CandidateInfoKey(sha256 string) string {
	return "candidates/" + sha256 + ".info.json"
}

// CandidateInfoFromBuildInfo maps a candidate binary's embedded Go build
// info (debug/buildinfo) to a CandidateInfo: vcs.revision -> Revision,
// vcs.modified == "true" -> Modified, vcs.time -> Time, and the top-level
// Go version -> GoVersion (docs/spec-eval-farm.md §3.3). It returns nil
// when bi is nil or carries no vcs.revision setting — "a candidate built
// without VCS stamping has no candidateInfo; readers then show the sha"
// (§3.3).
func CandidateInfoFromBuildInfo(bi *buildinfo.BuildInfo) *CandidateInfo {
	if bi == nil {
		return nil
	}
	var revision, vcsTime string
	var modified bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		case "vcs.time":
			vcsTime = s.Value
		}
	}
	if revision == "" {
		return nil
	}
	return &CandidateInfo{
		Revision:  revision,
		Modified:  modified,
		Time:      vcsTime,
		GoVersion: bi.GoVersion,
	}
}
