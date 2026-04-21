package checks

import (
	"context"
	"strings"
	"testing"
)

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
