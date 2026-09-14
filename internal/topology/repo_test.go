// Tests for: topology/repo.go — the adopt-baseline provenance vocabulary
// and tag naming (docs/spec-workflows.md §8 GLC-7).
package topology

import "testing"

// TestRepoProvenance_Values pins the two provenance strings: they are the
// wire/on-disk representation (ServiceMeta.Repo.Provenance JSON, the
// envelope's repo.provenance field) — a value drifting silently would
// break both without a compile error.
func TestRepoProvenance_Values(t *testing.T) {
	if RepoProvenanceSnapshot != "snapshot" {
		t.Errorf("RepoProvenanceSnapshot = %q, want %q", RepoProvenanceSnapshot, "snapshot")
	}
	if RepoProvenanceExisting != "existing" {
		t.Errorf("RepoProvenanceExisting = %q, want %q", RepoProvenanceExisting, "existing")
	}
}

func TestBaselineTagName_PrefixesWithZcpBaseline(t *testing.T) {
	got := BaselineTagName("av-123")
	want := "zcp/baseline/av-123"
	if got != want {
		t.Errorf("BaselineTagName(av-123) = %q, want %q", got, want)
	}
}

func TestBaselineIDFromTag_Table(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"single tag with newline", "zcp/baseline/av-123\n", "av-123"},
		{"single tag no newline", "zcp/baseline/av-123", "av-123"},
		{"empty output", "", ""},
		{"whitespace only", "   \n", ""},
		{"unrelated tag", "some-other-tag\n", ""},
		{"multiple tags takes first", "zcp/baseline/av-1\nzcp/baseline/av-2\n", "av-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BaselineIDFromTag(tt.in)
			if got != tt.want {
				t.Errorf("BaselineIDFromTag(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
