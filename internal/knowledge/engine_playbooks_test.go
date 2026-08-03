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
		excludedURIs   []string
		wantURI        string
		docs           map[string]*Document
	}{
		{
			name:           "matching playbook remains direct fetch only",
			query:          "deploy app",
			limit:          10,
			excludedPrefix: "zerops://playbooks/",
			excludedURIs: []string{
				"zerops://playbooks/onboarding",
				"zerops://playbooks/other-playbook",
			},
			wantURI: "zerops://guides/deploy-app",
			docs: map[string]*Document{
				"zerops://playbooks/onboarding": {
					URI:     "zerops://playbooks/onboarding",
					Title:   "Deploy App Service Playbook",
					Content: "# Deploy App Service Playbook\n\nDeploy an app service. Deploy app service.",
				},
				"zerops://playbooks/other-playbook": {
					URI:     "zerops://playbooks/other-playbook",
					Title:   "Deploy App Workflow Playbook",
					Content: "# Deploy App Workflow Playbook\n\nDeploy an app service with this deploy app workflow guide.",
				},
				"zerops://guides/deploy-app": {
					URI:     "zerops://guides/deploy-app",
					Title:   "Deploy App Guide",
					Content: "# Deploy App Guide\n\nDeploy an app with this ordinary guide.",
				},
			},
		},
		{
			name:           "orientation playbook remains direct fetch only",
			query:          "private network hostname subdomain",
			limit:          10,
			excludedPrefix: "zerops://playbooks/",
			excludedURIs: []string{
				"zerops://playbooks/orientation",
			},
			wantURI: "zerops://guides/private-network",
			docs: map[string]*Document{
				"zerops://playbooks/orientation": {
					URI:   "zerops://playbooks/orientation",
					Title: "Getting oriented: Zerops & ZCP",
					Content: "# Getting oriented: Zerops & ZCP\n\nA project is a private network; " +
						"services reach each other by hostname. A subdomain URL is the public door to the stage service.",
				},
				"zerops://guides/private-network": {
					URI:   "zerops://guides/private-network",
					Title: "Private Network Guide",
					Content: "# Private Network Guide\n\nServices share a private network and reach " +
						"each other by hostname; a subdomain URL exposes a service publicly.",
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
			resultURIs := make(map[string]struct{}, len(results))
			for _, result := range results {
				resultURIs[result.URI] = struct{}{}
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
			for _, excludedURI := range tt.excludedURIs {
				if _, found := resultURIs[excludedURI]; found {
					t.Errorf("Search returned excluded playbook %q", excludedURI)
				}
			}
		})
	}
}
