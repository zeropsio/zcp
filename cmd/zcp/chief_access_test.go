// Tests for: cmd/zcp/main.go chiefAccess — `zcp serve` reads the Chief it answers to
// from HQ at start (docs/spec-mate.md §3.11).
package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestChiefAccess_TheChiefHQNamesDecidesWhetherThisProjectServesOrgMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     int
		body       string
		wantChief  bool
		wantStderr string
	}{
		{name: "a Chief naming this project is the Chief", status: 200, body: `{"projectId":"p-me","chief":{"projectId":"p-me"}}`, wantChief: true},
		{name: "a Chief naming another project is no Chief", status: 200, body: `{"projectId":"p-me","chief":{"projectId":"p-other"}}`},
		{name: "no Chief named is no Chief", status: 200, body: `{"projectId":"p-me","chief":null}`},
		{name: "an HQ that refuses is no Chief, and says so", status: 500, body: `{}`, wantStderr: "serving the single-project surface"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var authorization, path string
			hqServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authorization, path = r.Header.Get("Authorization"), r.URL.Path
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(hqServer.Close)
			id := hq.Identity{Origin: hqServer.URL, Key: "project-key", ProjectID: "p-me"}
			var stderr bytes.Buffer

			access, chief := chiefAccess(context.Background(), hqServer.Client(), id, orgCredentials{APIHost: "api.example"}, &stderr)

			if chief != tt.wantChief {
				t.Fatalf("chief = %v, want %v (stderr %q)", chief, tt.wantChief, stderr.String())
			}
			if path != "/api/mate/self" || authorization != "Project project-key" {
				t.Errorf("read %s with %q, want GET /api/mate/self as the project", path, authorization)
			}
			if chief && (access != platform.OrgAccess{APIHost: "api.example", ProjectID: "p-me", Token: "project-key"}) {
				t.Errorf("access = %+v, want the Zerops API, this project and the project key", access)
			}
			if !strings.Contains(stderr.String(), tt.wantStderr) || (tt.wantStderr == "" && stderr.Len() != 0) {
				t.Errorf("stderr = %q, want it to hold %q", stderr.String(), tt.wantStderr)
			}
		})
	}
}

func TestChiefAccess_ANoIdentityContainerIsNoChiefAndSaysNothing(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	if _, chief := chiefAccess(context.Background(), http.DefaultClient, hq.Identity{}, orgCredentials{}, &stderr); chief || stderr.Len() != 0 {
		t.Errorf("chief = %v, stderr = %q; want neither", chief, stderr.String())
	}
}

// TestChiefAccess_ThePersonsTokenReadsWhileAPersonDrives_TheProjectKeyOtherwise:
// org mode reads Zerops with the credential its session holds — the person's
// own token when a person drives it, the project's key for its own project
// otherwise — and never HQ's origin (docs/spec-mate.md §2.11, OM-4).
func TestChiefAccess_ThePersonsTokenReadsWhileAPersonDrives_TheProjectKeyOtherwise(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		personToken string
		wantToken   string
	}{
		{name: "a person drives the session: their token", personToken: "person-token", wantToken: "person-token"},
		{name: "nobody drives it: the project key", personToken: "", wantToken: "project-key"},
		{name: "a blank token is no token: the project key", personToken: "  ", wantToken: "project-key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hqServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"projectId":"p-me","chief":{"projectId":"p-me"}}`))
			}))
			t.Cleanup(hqServer.Close)
			id := hq.Identity{Origin: hqServer.URL, Key: "project-key", ProjectID: "p-me"}
			var stderr bytes.Buffer

			access, chief := chiefAccess(context.Background(), hqServer.Client(), id,
				orgCredentials{APIHost: "api.example", PersonToken: tt.personToken}, &stderr)

			want := platform.OrgAccess{APIHost: "api.example", ProjectID: "p-me", Token: tt.wantToken}
			if !chief || access != want {
				t.Errorf("chief = %v, access = %+v; want the Chief reading with %+v", chief, access, want)
			}
		})
	}
}
