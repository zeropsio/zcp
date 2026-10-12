// Tests for: internal/auth/auth.go — ResolveOrgCredentials, org mode's
// credential read (docs/spec-mate.md §2.11).
package auth

import (
	"errors"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestResolveOrgCredentials(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		env      map[string]string
		want     OrgCredentials
		wantCode string
	}{
		{
			name: "the org token alone, production host",
			env:  map[string]string{"ZCP_ORG_TOKEN": "org-tok", "ZCP_API_KEY": "project-tok"},
			want: OrgCredentials{Token: "org-tok", APIHost: "api.app-prg1.zerops.io"},
		},
		{
			name: "a named org",
			env:  map[string]string{"ZCP_ORG_TOKEN": "org-tok", "ZCP_ORG_ID": " org1 "},
			want: OrgCredentials{Token: "org-tok", APIHost: "api.app-prg1.zerops.io", OrgID: "org1"},
		},
		{
			name: "the API host follows a devel container",
			env:  map[string]string{"ZCP_ORG_TOKEN": "org-tok", "zeropsSubdomain": "https://zcp-1df2-8080.app-tatami.zerops.dev"},
			want: OrgCredentials{Token: "org-tok", APIHost: "api.app-tatami.zerops.dev"},
		},
		{
			name: "an explicit API host wins",
			env:  map[string]string{"ZCP_ORG_TOKEN": "org-tok", "ZCP_API_HOST": "api.example"},
			want: OrgCredentials{Token: "org-tok", APIHost: "api.example"},
		},
		{
			// HQ's Chief proxy: the org API is read as given, trimmed; the
			// org it needs is OpenOrgReader's to refuse when missing.
			name: "an org API",
			env:  map[string]string{"ZCP_ORG_TOKEN": "chief-cred", "ZCP_ORG_API": " https://hq.example/api/chief/zerops ", "ZCP_ORG_ID": "org1"},
			want: OrgCredentials{Token: "chief-cred", APIHost: "api.app-prg1.zerops.io", OrgAPI: "https://hq.example/api/chief/zerops", OrgID: "org1"},
		},
		{
			// The project's own token is never borrowed for the org.
			name:     "no org token",
			env:      map[string]string{"ZCP_API_KEY": "project-tok"},
			wantCode: platform.ErrAuthRequired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ResolveOrgCredentials(func(k string) string { return tt.env[k] })
			if tt.wantCode != "" {
				var pe *platform.PlatformError
				if !errors.As(err, &pe) || pe.Code != tt.wantCode {
					t.Fatalf("err = %v, want %s", err, tt.wantCode)
				}
				return
			}
			if err != nil || *got != tt.want {
				t.Fatalf("ResolveOrgCredentials = %+v, %v; want %+v", got, err, tt.want)
			}
		})
	}
}
