package farm

import "debug/buildinfo"

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
