package platform

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProjectAdminMintProjectKey_BasicUserOnTheNewProjectInItsOrg: the
// deploy key a launch mints is NO_ACCESS in the launch's organisation and
// BASIC_USER on exactly the project the launch created.
func TestProjectAdminMintProjectKey_BasicUserOnTheNewProjectInItsOrg(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody struct {
		RoleCode          string `json:"roleCode"`
		CanCreateProjects bool   `json:"canCreateProjects"`
		Projects          []struct {
			ProjectID string `json:"projectId"`
			RoleCode  string `json:"roleCode"`
		} `json:"projects"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.Method + " " + r.URL.Path
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read mint body: %v", err)
		}
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("decode mint body %q: %v", raw, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"deploy-tok","name":"zcp-deploy-myapp-prod","roleCode":"NO_ACCESS","token":"deploy-secret"}`))
	}))
	t.Cleanup(srv.Close)

	z, err := NewZeropsClient("person-token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	p := &projectAdminClient{zerops: z, clientID: "org-1", clientUserID: "cu-1"}

	got, err := p.MintProjectKey(context.Background(), "new-prod-id", "zcp-deploy-myapp-prod")
	if err != nil {
		t.Fatalf("MintProjectKey: %v", err)
	}
	if got != (MintedToken{Token: "deploy-secret", TokenID: "deploy-tok"}) {
		t.Errorf("MintProjectKey = %+v, want the minted deploy key", got)
	}
	if want := "POST /api/rest/public/client/org-1/integration-token"; gotPath != want {
		t.Errorf("mint call = %q, want %q", gotPath, want)
	}
	if gotBody.RoleCode != "NO_ACCESS" || gotBody.CanCreateProjects {
		t.Errorf("organisation rights = %q canCreateProjects=%v, want NO_ACCESS without project creation", gotBody.RoleCode, gotBody.CanCreateProjects)
	}
	if len(gotBody.Projects) != 1 || gotBody.Projects[0].ProjectID != "new-prod-id" || gotBody.Projects[0].RoleCode != "BASIC_USER" {
		t.Errorf("projects = %+v, want exactly new-prod-id as BASIC_USER", gotBody.Projects)
	}
}
