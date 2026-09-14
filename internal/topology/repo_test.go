// Tests for: topology/repo.go — provenance classification for the
// repo-always adopt baseline (docs/spec-workflows.md §8 GLC-7).
package topology

import "testing"

func TestClassifyProvenance_Table(t *testing.T) {
	tests := []struct {
		name          string
		sourceService string
		deployFiles   []string
		want          RepoProvenance
	}{
		{"sourceService set", "appdev", nil, RepoProvenanceArtifactOnly},
		{"sourceService set with deployFiles dot", "appdev", []string{"."}, RepoProvenanceArtifactOnly},
		{"no sourceService, deployFiles unset", "", nil, RepoProvenanceSource},
		{"no sourceService, deployFiles exactly dot", "", []string{"."}, RepoProvenanceSource},
		{"no sourceService, deployFiles narrower", "", []string{"dist"}, RepoProvenanceArtifactOnly},
		{"no sourceService, deployFiles multiple", "", []string{"dist", "public"}, RepoProvenanceArtifactOnly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyProvenance(tt.sourceService, tt.deployFiles)
			if got != tt.want {
				t.Errorf("ClassifyProvenance(%q, %v) = %q, want %q", tt.sourceService, tt.deployFiles, got, tt.want)
			}
		})
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
