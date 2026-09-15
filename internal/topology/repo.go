package topology

// RepoProvenance classifies what adopt could actually PROVE about a dev
// service's baseline commit content (docs/spec-workflows.md §8 GLC-7).
// Neither value claims the baseline tree matches the running appVersion's
// build input — that relation is unproven in both cases; the platform
// surface that would let ZCP verify it (the appVersion's sourceService /
// deployFiles) isn't modeled on platform.ServiceStack/AppVersionEvent.
type RepoProvenance string

const (
	// RepoProvenanceInitialized means AdoptBaseline found no content HEAD
	// (no repo yet, an unborn HEAD, or a HEAD over the empty tree) and
	// brought the repo to the same commit-ready state bootstrap leaves a
	// fresh service in: init-if-missing, identity set-if-absent, an empty
	// marker HEAD if none was reachable. It never stages or commits the
	// files it found — the working tree stays exactly as adopt found it,
	// uncommitted. zcp never commits user files.
	RepoProvenanceInitialized RepoProvenance = "initialized"
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
