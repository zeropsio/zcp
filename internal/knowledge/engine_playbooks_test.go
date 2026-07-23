package knowledge

import (
	"strings"
	"testing"
)

func TestSearch_ExcludesPlaybooks_NoHits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		query          string
		limit          int
		excludedPrefix string
		wantURI        string
		docs           map[string]*Document
	}{
		{
			name:           "matching playbook remains direct fetch only",
			query:          "deploy app",
			limit:          10,
			excludedPrefix: "zerops://playbooks/",
			wantURI:        "zerops://guides/deploy-app",
			docs: map[string]*Document{
				"zerops://playbooks/onboarding": {
					URI:     "zerops://playbooks/onboarding",
					Title:   "Deploy App Service Playbook",
					Content: "# Deploy App Service Playbook\n\nDeploy an app service. Deploy app service.",
				},
				"zerops://guides/deploy-app": {
					URI:     "zerops://guides/deploy-app",
					Title:   "Deploy App Guide",
					Content: "# Deploy App Guide\n\nDeploy an app with this ordinary guide.",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store, err := NewStore(tt.docs)
			if err != nil {
				t.Fatalf("NewStore: %v", err)
			}

			results := store.Search(tt.query, tt.limit)
			foundWanted := false
			for _, result := range results {
				if strings.HasPrefix(result.URI, tt.excludedPrefix) {
					t.Errorf("Search returned direct-fetch-only playbook %q", result.URI)
				}
				if result.URI == tt.wantURI {
					foundWanted = true
				}
			}
			if !foundWanted {
				t.Errorf("Search did not return ordinary guide %q; results: %+v", tt.wantURI, results)
			}
		})
	}
}
