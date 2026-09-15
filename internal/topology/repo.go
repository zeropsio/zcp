package topology

// RepoProvenance classifies what adopt could actually PROVE about a dev
// service's baseline commit content (docs/spec-workflows.md §8 GLC-7).
// Neither value claims the baseline tree matches the running appVersion's
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
	// RepoProvenanceExisting means adoption preserved a
	// pre-existing HEAD that already carried content — AdoptBaseline
	// trusted it as-is rather than minting a commit. Its relation to the
	// running appVersion is unproven.
	RepoProvenanceExisting RepoProvenance = "existing"
)

// Repo records which appVersion a dev service was running at adoption,
// and which RepoProvenance case produced
// that baseline (docs/spec-workflows.md §8 GLC-7).
type Repo struct {
	BaselineAppVersion string         `json:"baselineAppVersion,omitempty"`
	Provenance         RepoProvenance `json:"provenance,omitempty"`
}
