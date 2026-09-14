package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRedeployAppVersion_EmptyFields_SendNull pins the wire shape of the
// GF-8 rollback body (docs/spec-workflows.md §8 R2): an empty yaml/setup
// argument goes out as JSON null — the platform ignores null for a BACKUP
// version but rejects "" with invalidUserInput ("zeropsYamlSetup … value
// should not be empty", live 2026-09-14). Non-empty arguments (the R2
// recovery path) are still sent verbatim.
func TestRedeployAppVersion_EmptyFields_SendNull(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		yaml, setup string
		wantYaml    any
		wantSetup   any
	}{
		{name: "rollback sends null", yaml: "", setup: "", wantYaml: nil, wantSetup: nil},
		{name: "recovery sends both", yaml: "run:\n  base: nodejs@22\n", setup: "prod", wantYaml: "run:\n  base: nodejs@22\n", wantSetup: "prod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var gotBody map[string]any
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_ = json.NewDecoder(r.Body).Decode(&gotBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"proc-1","status":"PENDING","actionName":"stack.deploy.backup"}`))
			}))
			t.Cleanup(srv.Close)

			z, err := NewZeropsClient("fake-token", srv.URL)
			if err != nil {
				t.Fatalf("NewZeropsClient: %v", err)
			}
			if _, err := z.RedeployAppVersion(context.Background(), "av-backup", tt.yaml, tt.setup); err != nil {
				t.Fatalf("RedeployAppVersion: %v", err)
			}
			if gotPath != "/api/rest/public/app-version/av-backup/deploy" {
				t.Errorf("path = %q, want /api/rest/public/app-version/av-backup/deploy", gotPath)
			}
			for _, k := range []string{"zeropsYaml", "zeropsYamlSetup"} {
				if _, present := gotBody[k]; !present {
					t.Errorf("body lacks key %q: %v", k, gotBody)
				}
			}
			if gotBody["zeropsYaml"] != tt.wantYaml {
				t.Errorf("zeropsYaml = %#v, want %#v", gotBody["zeropsYaml"], tt.wantYaml)
			}
			if gotBody["zeropsYamlSetup"] != tt.wantSetup {
				t.Errorf("zeropsYamlSetup = %#v, want %#v", gotBody["zeropsYamlSetup"], tt.wantSetup)
			}
		})
	}
}
