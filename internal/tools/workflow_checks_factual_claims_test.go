package tools

import (
	"strings"
	"testing"
)

// TestCheckFactualClaims validates the factual-claim linter against the
// exact failure mode found in v5/v8/v10: a declarative numeric claim in a
// YAML comment that contradicts the adjacent value in the same service
// block. The linter must fail those cases and pass when either the claim
// matches the value or the comment is aspirational (consider/bump/upgrade).
func TestCheckFactualClaims(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		content    string
		wantFail   bool
		wantDetail string // substring that must appear in the failure detail
	}{
		{
			name: "v10 pattern — '10 GB quota' comment next to objectStorageSize: 1",
			content: `
  # S3-compatible object storage with 10 GB quota — production file storage.
  - hostname: storage
    type: object-storage
    priority: 10
    objectStorageSize: 1
    objectStoragePolicy: private
`,
			wantFail:   true,
			wantDetail: "10 GB",
		},
		{
			name: "matching claim — '1 GB storage' next to objectStorageSize: 1",
			content: `
  # S3-compatible object storage sized at 1 GB for the demo dataset.
  - hostname: storage
    type: object-storage
    priority: 10
    objectStorageSize: 1
    objectStoragePolicy: private
`,
			wantFail: false,
		},
		{
			name: "subjunctive skip — 'bump to 10 GB' next to objectStorageSize: 1",
			content: `
  # Object storage. 1 GB default — bump to 10 GB via the GUI if usage grows.
  - hostname: storage
    type: object-storage
    priority: 10
    objectStorageSize: 1
    objectStoragePolicy: private
`,
			wantFail: false,
		},
		{
			name: "no numeric claim — pure prose",
			content: `
  # S3-compatible object storage for production file uploads.
  - hostname: storage
    type: object-storage
    priority: 10
    objectStorageSize: 1
    objectStoragePolicy: private
`,
			wantFail: false,
		},
		{
			name: "minContainers mismatch — comment says 3 but YAML has 2",
			content: `
  # NestJS API with minContainers 3 for high availability.
  - hostname: api
    type: nodejs@24
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/example-api
    minContainers: 2
`,
			wantFail:   true,
			wantDetail: "minContainers",
		},
		{
			name: "minContainers match — comment says 2 and YAML has 2",
			content: `
  # NestJS API with minContainers 2 for rolling-deploy availability.
  - hostname: api
    type: nodejs@24
    zeropsSetup: prod
    buildFromGit: https://github.com/zerops-recipe-apps/example-api
    minContainers: 2
`,
			wantFail: false,
		},
		{
			name: "sibling-block bleed — comment on cache (no Size field) must NOT match storage.objectStorageSize",
			// Comment belongs to the 'cache' service, which has no Size field.
			// The next service down is 'storage' with objectStorageSize: 1. The
			// adjacent-value scan must stop at the second list item (sibling
			// boundary) so the 10 GB claim is orphaned, not matched against a
			// different service's unrelated field.
			content: `
  # Valkey cache sized for 10 GB working set — keys live in RAM.
  - hostname: cache
    type: valkey@7.2
    priority: 10
    mode: NON_HA
  - hostname: storage
    type: object-storage
    priority: 10
    objectStorageSize: 1
    objectStoragePolicy: private
`,
			wantFail: false,
		},
		{
			name: "v5 pattern — '5 GB quota; bump in the GUI' is aspirational (bump) → skip",
			content: `
  # Object storage for production uploads. 5 GB quota; bump in the GUI when
  # usage approaches the limit.
  - hostname: storage
    type: object-storage
    priority: 10
    objectStorageSize: 1
    objectStoragePolicy: private
`,
			wantFail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checks := checkFactualClaims(tt.content, "env0_import")
			var gotFail bool
			var allDetails strings.Builder
			for _, c := range checks {
				if c.Status == "fail" {
					gotFail = true
					allDetails.WriteString(c.Detail)
					allDetails.WriteByte('\n')
				}
			}
			if gotFail != tt.wantFail {
				t.Errorf("gotFail=%v wantFail=%v\nchecks=%+v", gotFail, tt.wantFail, checks)
			}
			if tt.wantFail && tt.wantDetail != "" && !strings.Contains(allDetails.String(), tt.wantDetail) {
				t.Errorf("expected failure detail to contain %q, got %q", tt.wantDetail, allDetails.String())
			}
		})
	}
}
