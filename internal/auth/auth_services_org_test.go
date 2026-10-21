// OMIT t.Parallel(): tests use t.Setenv to modify global env vars (ZCP_API_KEY, projectId).
package auth_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// serveSeveralOrgs fakes the Zerops API for a user in two orgs whose one
// project (proj-b, service "api") lives in the second: every search
// answers only for the org it names, as the platform's do.
func serveSeveralOrgs(t *testing.T) *httptest.Server {
	t.Helper()
	const owner = "org-owner"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/rest/public/")
		switch {
		case path == "user/info":
			fmt.Fprint(w, `{"id":"user-1","email":"a@b.com","fullName":"ZCP","clientUserList":[`+
				`{"id":"cu-first","clientId":"org-first","userId":"user-1"},`+
				`{"id":"cu-owner","clientId":"org-owner","userId":"user-1"}]}`)
		case path == "project/proj-b":
			fmt.Fprintf(w, `{"id":"proj-b","clientId":%q,"name":"b","status":"ACTIVE"}`, owner)
		case strings.HasSuffix(path, "/search"):
			var body struct {
				Search []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"search"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			clientID := ""
			for _, term := range body.Search {
				if term.Name == "clientId" {
					clientID = term.Value
				}
			}
			switch {
			case clientID != owner:
				fmt.Fprint(w, `{"items":[]}`)
			case path == "project/search":
				fmt.Fprintf(w, `{"items":[{"id":"proj-b","clientId":%q,"name":"b","status":"ACTIVE"}]}`, owner)
			case path == "service-stack/search":
				fmt.Fprintf(w, `{"items":[{"id":"svc-api","name":"api","projectId":"proj-b","clientId":%q,"status":"ACTIVE"}]}`, owner)
			default:
				fmt.Fprint(w, `{"items":[]}`)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Deploy, verify, subdomain and logs all find their target through
// ops.LookupService, so finding the service is what this pins.
func TestSeveralOrganizations_ProjectOutsideTheFirst_DeployVerifyAndLogsFindItsServices(t *testing.T) {
	tests := []struct {
		name     string
		injected string
	}{
		{name: "in a container, where the platform injects the project", injected: "proj-b"},
		{name: "on a laptop, where the one project is found across every org"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serveSeveralOrgs(t)
			t.Setenv("ZCP_API_KEY", "some-token")
			t.Setenv("ZCP_API_HOST", srv.URL)
			t.Setenv("serviceId", map[bool]string{true: "svc-zcp", false: ""}[tt.injected != ""])
			t.Setenv("projectId", tt.injected)

			client, err := platform.NewZeropsClient("some-token", srv.URL)
			if err != nil {
				t.Fatalf("NewZeropsClient: %v", err)
			}
			ctx := context.Background()
			info, err := auth.Resolve(ctx, client)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			svc, err := ops.LookupService(ctx, client, info.ProjectID, "api")
			if err != nil {
				t.Fatalf("LookupService: %v", err)
			}
			if svc.ID != "svc-api" {
				t.Errorf("service id = %q, want svc-api", svc.ID)
			}
		})
	}
}
