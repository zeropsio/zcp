// Tests for: topology/repo.go — the adopt-baseline provenance vocabulary
//
//	(docs/spec-workflows.md §8 GLC-7).
package topology

import "testing"

// TestRepoProvenance_Values pins the two provenance strings: they are the
// wire/on-disk representation (ServiceMeta.Repo.Provenance JSON, the
// envelope's repo.provenance field) — a value drifting silently would
// break both without a compile error.
func TestRepoProvenance_Values(t *testing.T) {
	if RepoProvenanceInitialized != "initialized" {
		t.Errorf("RepoProvenanceInitialized = %q, want %q", RepoProvenanceInitialized, "initialized")
	}
	if RepoProvenanceExisting != "existing" {
		t.Errorf("RepoProvenanceExisting = %q, want %q", RepoProvenanceExisting, "existing")
	}
}
