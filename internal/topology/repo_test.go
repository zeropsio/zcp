// Tests for: topology/repo.go — provenance classification for the
// repo-always adopt baseline (docs/spec-workflows.md §4.10).
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
