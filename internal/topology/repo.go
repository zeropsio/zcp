package topology

// RepoProvenance classifies where a dev service's baseline commit content
// actually came from at adopt time (docs/spec-workflows.md §4.10).
type RepoProvenance string

const (
	// RepoProvenanceSource means the running appVersion was built directly
	// from this service's own working tree (no sourceService, deployFiles
	// either unset or exactly `.`). The baseline commit's tree can be
	// trusted to match what's actually deployed.
	RepoProvenanceSource RepoProvenance = "source"
	// RepoProvenanceArtifactOnly means the running appVersion was built
	// elsewhere (a sourceService is set, or deployFiles names a narrower
	// path than `.`) — the working tree may not fully reflect what's
	// running, so the baseline tag records a starting point, not a proof
	// of parity.
	RepoProvenanceArtifactOnly RepoProvenance = "artifact-only"
)

// Repo is the adopt-time marker recording which appVersion a dev service's
// git baseline was tagged against, and whether that baseline's tree is
// known to match the deployed content (docs/spec-workflows.md §4.10).
type Repo struct {
	BaselineAppVersion string         `json:"baselineAppVersion,omitempty"`
	Provenance         RepoProvenance `json:"provenance,omitempty"`
}

// ClassifyProvenance classifies a running appVersion's provenance from its
// sourceService (set when the build was a cross-deploy from another
// service) and deployFiles (the deploy-time file-selection list). A
// non-empty sourceService always means artifact-only — the tree adopt
// finds locally was never the build input. Absent a sourceService,
// deployFiles narrower than the whole tree (anything other than unset or
// exactly `["."]`) also means artifact-only — the deploy shipped only a
// subset of the working tree, so the two can diverge. Everything else is
// source.
func ClassifyProvenance(sourceService string, deployFiles []string) RepoProvenance {
	if sourceService != "" {
		return RepoProvenanceArtifactOnly
	}
	if len(deployFiles) == 0 {
		return RepoProvenanceSource
	}
	if len(deployFiles) == 1 && deployFiles[0] == "." {
		return RepoProvenanceSource
	}
	return RepoProvenanceArtifactOnly
}

// BaselineTagName returns the git tag name AdoptBaseline creates/moves to
// mark the appVersion a dev service was adopted at (refs/tags/zcp/baseline/<id>).
func BaselineTagName(appVersionID string) string {
	return "zcp/baseline/" + appVersionID
}
