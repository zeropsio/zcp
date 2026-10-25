// Tests for: platform/zbox.go — a workspace answer that names no domains
// list is never read as "no domains" (docs/spec-workflows.md §8 PS-13).
package platform

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestBoxClient_WorkspaceWithoutDomainsList_Refused: PUT /workspace/domains
// replaces the WHOLE list, so setup adds the ingress host to every domain the
// workspace answers. A 2xx whose body is empty, null, or carries no `domains`
// key says nothing about the domains — read as none, setup's PUT would wipe
// every domain the person registered. Such an answer is an error; an answer
// that names the list — empty, or null — is the list.
func TestBoxClient_WorkspaceWithoutDomainsList_Refused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		body        string
		wantErr     bool
		wantDomains int
	}{
		{name: "empty body", body: "", wantErr: true},
		{name: "null", body: "null", wantErr: true},
		{name: "empty object", body: "{}", wantErr: true},
		{name: "no domains key", body: `{"version":"zerops-v3.1.0","policy":{}}`, wantErr: true},
		{name: "an empty list", body: `{"domains":[],"version":"v"}`},
		{name: "a null list", body: `{"domains":null,"version":"v"}`},
		{name: "one domain", body: boxWorkspaceFixture, wantDomains: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			var mints int
			var mu sync.Mutex
			c := NewBoxClient(staticMint(srv.URL, &mints, &mu), srv.Client())

			ws, err := c.Workspace(context.Background())

			if (err != nil) != tt.wantErr {
				t.Fatalf("Workspace = %+v, %v; want error %v", ws, err, tt.wantErr)
			}
			if !tt.wantErr && len(ws.Domains) != tt.wantDomains {
				t.Errorf("domains = %+v, want %d", ws.Domains, tt.wantDomains)
			}
		})
	}
}
