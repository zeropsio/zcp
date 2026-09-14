package topology

import "strings"

// RepoProvenance classifies what adopt could actually PROVE about a dev
// service's baseline commit content (docs/spec-workflows.md §8 GLC-7).
// Neither value claims the tagged tree matches the running appVersion's
// build input — that relation is unproven in both cases; the platform
// surface that would let ZCP verify it (the appVersion's sourceService /
// deployFiles) isn't modeled on platform.ServiceStack/AppVersionEvent.
type RepoProvenance string

const (
	// RepoProvenanceSnapshot means AdoptBaseline minted the baseline
	// commit itself, from whatever files it found on disk at adopt time
	// (no repo yet, an unborn HEAD, or a HEAD over the empty tree). It is
	// a snapshot of the working tree adopt found, never a claim about
	// what built the running appVersion.
	RepoProvenanceSnapshot RepoProvenance = "snapshot"
	// RepoProvenanceExisting means the baseline tag landed on a
	// pre-existing HEAD that already carried content — AdoptBaseline
	// trusted it as-is rather than minting a commit. Its relation to the
	// running appVersion is unproven.
	RepoProvenanceExisting RepoProvenance = "existing"
)

// Repo is the adopt-time marker recording which appVersion a dev service's
// git baseline was tagged against, and which RepoProvenance case produced
// that baseline (docs/spec-workflows.md §8 GLC-7).
type Repo struct {
	BaselineAppVersion string         `json:"baselineAppVersion,omitempty"`
	Provenance         RepoProvenance `json:"provenance,omitempty"`
}

// BaselineTagName returns the git tag name AdoptBaseline creates/moves to
// mark the appVersion a dev service was adopted at (refs/tags/zcp/baseline/<id>).
func BaselineTagName(appVersionID string) string {
	return "zcp/baseline/" + appVersionID
}

// baselineTagPrefix is BaselineTagName's fixed prefix, factored out so
// BaselineIDFromTag stays the single place that strips it back off.
const baselineTagPrefix = "zcp/baseline/"

// BaselineIDFromTag extracts the appVersion id from a `git tag --list
// 'zcp/baseline/*'` line (one tag name, optionally with trailing
// whitespace/newline). Returns "" when tagOutput doesn't name a
// zcp/baseline/* tag at all — including empty output (no tag found).
func BaselineIDFromTag(tagOutput string) string {
	line := strings.TrimSpace(tagOutput)
	if line == "" {
		return ""
	}
	// `git tag --points-at` can list more than one tag per line (rare,
	// but possible if a caller ever double-tags); the baseline is always
	// the first one recorded.
	if idx := strings.IndexAny(line, "\n"); idx >= 0 {
		line = line[:idx]
	}
	if !strings.HasPrefix(line, baselineTagPrefix) {
		return ""
	}
	return strings.TrimPrefix(line, baselineTagPrefix)
}
