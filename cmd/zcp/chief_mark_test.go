// Tests for: cmd/zcp/main.go chiefAccess — `zcp serve` reads the Chief mark
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
)

func TestChiefAccess_TheMarkDecidesWhetherThisProjectServesOrgMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     int
		body       string
		wantChief  bool
		wantStderr string
	}{
		{name: "a mark naming this project is the Chief", status: 200, body: `{"projectId":"p-me","chief":{"projectId":"p-me"}}`, wantChief: true},
		{name: "a mark naming another project is no Chief", status: 200, body: `{"projectId":"p-me","chief":{"projectId":"p-other"}}`},
		{name: "no mark is no Chief", status: 200, body: `{"projectId":"p-me","chief":null}`},
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

			access, chief := chiefAccess(context.Background(), hqServer.Client(), id, &stderr)

			if chief != tt.wantChief {
				t.Fatalf("chief = %v, want %v (stderr %q)", chief, tt.wantChief, stderr.String())
			}
			if path != "/api/mate/self" || authorization != "Project project-key" {
				t.Errorf("read %s with %q, want GET /api/mate/self as the project", path, authorization)
			}
			if chief && (access.Origin != hqServer.URL || access.Key != "project-key") {
				t.Errorf("access = %+v, want HQ's origin and the project key", access)
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
	if _, chief := chiefAccess(context.Background(), http.DefaultClient, hq.Identity{}, &stderr); chief || stderr.Len() != 0 {
		t.Errorf("chief = %v, stderr = %q; want neither", chief, stderr.String())
	}
}
