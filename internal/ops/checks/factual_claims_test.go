package checks

import (
	"context"
	"strings"
	"testing"
)

// TestEnvCommentFactuality_RejectsInventedNumber is the Cx-ENV-COMMENT-
// PRINCIPLE RED→GREEN guard. v37 F-21: writer invented numeric claims
// (2 GB quotas, minContainers: 3) that did not match the platform-auto-
// generated YAML. The check fires, but its detail must name BOTH the
// claimed string AND the actual YAML value in the form
// `comment claims "N <unit>" but adjacent YAML has <key>: M` so a
// fix-cycle reader can diff the two side-by-side rather than parsing
// a generic mismatch sentence.
func TestEnvCommentFactuality_RejectsInventedNumber(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: store
    type: object-storage
    # 2 GB quota sized for showcase traffic
    objectStorageSize: 1`
	got := CheckFactualClaims(context.Background(), content, "env_import")
	failCount := 0
	var failDetail string
	for _, c := range got {
		if c.Status == "fail" {
			failCount++
			failDetail = c.Detail
		}
	}
	if failCount != 1 {
		t.Fatalf("expected exactly 1 fail, got %d — checks: %+v", failCount, got)
	}
	wantFragments := []string{
		`claims "2 GB"`,
		`objectStorageSize: 1`,
	}
	for _, w := range wantFragments {
		if !strings.Contains(failDetail, w) {
			t.Errorf("detail missing specific-mismatch fragment %q; got:\n%s", w, failDetail)
		}
	}
}

// TestEnvCommentFactuality_AcceptsQualitativePhrasing pins the flip
// side of Cx-3: a comment that describes a tier qualitatively without
// naming a number that would contradict the YAML passes cleanly. The
// Factuality rule in env-comment-rules.md defaults to this phrasing —
// any regression loosens the writer-side discipline.
func TestEnvCommentFactuality_AcceptsQualitativePhrasing(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: app
    type: nodejs@22
    # single-replica production with rolling-deploy risk;
    # promote to HA for zero-downtime rotation
    minContainers: 1`
	got := CheckFactualClaims(context.Background(), content, "env_import")
	for _, c := range got {
		if c.Status == "fail" {
			t.Errorf("qualitative comment should not fail; got fail: %s", c.Detail)
		}
	}
}

func TestCheckFactualClaims_Table(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		content    string
		wantFail   int
		wantDetail []string
	}{
		{
			name: "no claims passes",
			content: `services:
  - hostname: db
    type: postgresql`,
			wantFail: 0,
		},
		{
			name: "matching claim passes",
			content: `services:
  - hostname: store
    type: object-storage
    # 5 GB quota for development data sets
    objectStorageSize: 5`,
			wantFail: 0,
		},
		{
			name: "mismatched storage quota fails",
			content: `services:
  - hostname: store
    type: object-storage
    # 10 GB quota sized for production
    objectStorageSize: 5`,
			wantFail:   1,
			wantDetail: []string{"10", "5", "objectStorageSize"},
		},
		{
			name: "aspirational comment bypasses check",
			content: `services:
  - hostname: store
    type: object-storage
    # bump to 50 GB via GUI when usage grows; baseline is lower
    objectStorageSize: 5`,
			wantFail: 0,
		},
		{
			name: "minContainers mismatch fails",
			content: `services:
  - hostname: api
    # minContainers 2 provides rolling deploys
    minContainers: 1`,
			wantFail:   1,
			wantDetail: []string{"minContainers", "1", "2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CheckFactualClaims(context.Background(), tt.content, "env_import")
			shim := make([]stepCheckShim, 0, len(got))
			for _, c := range got {
				shim = append(shim, stepCheckShim{Name: c.Name, Status: c.Status, Detail: c.Detail})
			}
			failCount := 0
			for _, c := range shim {
				if c.Status == "fail" {
					failCount++
				}
			}
			if failCount != tt.wantFail {
				t.Errorf("got %d fails, want %d: %+v", failCount, tt.wantFail, shim)
			}
			if tt.wantFail > 0 {
				for _, w := range tt.wantDetail {
					found := false
					for _, c := range shim {
						if c.Status == "fail" && strings.Contains(c.Detail, w) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("no fail detail contained %q; got %+v", w, shim)
					}
				}
			}
		})
	}
}
