package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// An existing-project launch records the org that owns its target, so the
// launch window's later steps act in that org rather than the source's.
func TestLaunchExistingProject_RecordsTheOrgOwningTheTargetProject(t *testing.T) {
	stateDir := t.TempDir()
	installLaunchGateReady(t, stateDir, "app", canonicalLaunchTestRemoteURL)
	sourceClient := pLP3MockClient()
	sourceClient.BindOrg("org-source")

	targetMock := existingTargetMock(expectedExistingProjectID, nil).
		WithUserInfo(&platform.UserInfo{Memberships: []platform.ClientMembership{{ClientID: "org-target", ClientUserID: "cu-target"}}}).
		WithProjects([]platform.Project{{ID: expectedExistingProjectID, Name: "existing-prod-project", ClientID: "org-target"}})
	defer setExistingProdTokenClientFactory(func(_, _ string) (platform.Client, error) {
		return targetMock, nil
	})()

	if _, _, err := handleLaunchProduction(context.Background(), "source-project-id", sourceClient, nil, nil, existingCompleteInput(),
		stateDir, pLP3ContainerRuntime(), pLP3SSHFrozen(), ""); err != nil {
		t.Fatalf("handleLaunchProduction: %v", err)
	}
	state, err := readLaunchState(stateDir, generateLaunchID("source-project-id", "myapp-prod"))
	if err != nil || state == nil {
		t.Fatalf("read launch state: %v", err)
	}
	if state.TargetClientID != "org-target" {
		t.Errorf("TargetClientID = %q, want org-target", state.TargetClientID)
	}
}

// targetOrgAPI fakes the Zerops API behind a launch token for a target
// project (prod-1, services app + db) owned by org-target: searches answer
// only for the org they name, and the project delete finishes at once.
func targetOrgAPI(t *testing.T, memberships ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(r.URL.Path, "/api/rest/public/")
		switch {
		case path == "user/info":
			entries := make([]string, 0, len(memberships))
			for _, org := range memberships {
				entries = append(entries, fmt.Sprintf(`{"id":"cu-%s","clientId":%q,"userId":"tok-1"}`, org, org))
			}
			fmt.Fprintf(w, `{"id":"tok-1","email":"a@b.com","fullName":"T","clientUserList":[%s]}`, strings.Join(entries, ","))
		case path == "service-stack/search":
			var body struct {
				Search []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"search"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			for _, term := range body.Search {
				if term.Name == "clientId" && term.Value == "org-target" {
					fmt.Fprint(w, `{"items":[`+
						`{"id":"svc-app","name":"app","projectId":"prod-1","clientId":"org-target","status":"ACTIVE"},`+
						`{"id":"svc-db","name":"db","projectId":"prod-1","clientId":"org-target","status":"ACTIVE"}]}`)
					return
				}
			}
			fmt.Fprint(w, `{"items":[]}`)
		case path == "project/prod-1" && r.Method == http.MethodDelete:
			fmt.Fprint(w, `{"id":"del-1","status":"RUNNING","actionName":"project.delete"}`)
		case path == "project/prod-1":
			fmt.Fprint(w, `{"id":"prod-1","clientId":"org-target","name":"myapp-prod","status":"ACTIVE"}`)
		case path == "process/del-1":
			fmt.Fprint(w, `{"id":"del-1","status":"FINISHED","actionName":"project.delete"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"code":"notFound","message":"not found"}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLaunchWindow_ExistingProjectInAnotherOrg_ProdOpsConfirmAndResetReachTheTargetsServices(t *testing.T) {
	memberships := map[string][]string{
		"a token in the target's org only":             {"org-target"},
		"a token in the source's org and the target's": {"org-source", "org-target"},
	}
	for name, orgs := range memberships {
		t.Run(name, func(t *testing.T) {
			srv := targetOrgAPI(t, orgs...)
			source := platform.NewMock()
			source.BindOrg("org-source")
			seed := func(t *testing.T, status topology.LaunchProductionStatus) string {
				t.Helper()
				stateDir := t.TempDir()
				if err := writeLaunchState(stateDir, &launchState{
					LaunchID:          generateLaunchID("src-proj", "myapp-prod"),
					SourceProjectID:   "src-proj",
					TargetProjectName: "myapp-prod",
					TargetProjectID:   "prod-1",
					TargetClientID:    "org-target",
					RuntimeProds:      []launchRuntimeProd{{ProdHostname: "app"}},
					Status:            status,
				}); err != nil {
					t.Fatalf("writeLaunchState: %v", err)
				}
				return stateDir
			}

			t.Run("prod-ops status lists the target's services", func(t *testing.T) {
				result, _, _ := handleLaunchProdOps(context.Background(), "src-proj", source, nil, WorkflowInput{
					ProductionProjectName: "myapp-prod", ProdOperation: "status", LaunchKey: "lk",
				}, seed(t, topology.LaunchStatusLaunched), srv.URL)
				body := getTextContent(t, result)
				if result.IsError || !strings.Contains(body, "svc-app") {
					t.Errorf("status must list the target's services, got: %s", body)
				}
			})
			t.Run("confirm-production sees the target's runtimes live", func(t *testing.T) {
				result, _, _ := handleLaunchConfirmProduction(context.Background(), "src-proj", source, WorkflowInput{
					ProductionProjectName: "myapp-prod", LaunchKey: "lk",
				}, seed(t, topology.LaunchStatusLaunched), srv.URL)
				body := getTextContent(t, result)
				if strings.Contains(body, "MISSING") || strings.Contains(body, "liveness unverified") {
					t.Errorf("confirm must see app ACTIVE in the target, got: %s", body)
				}
			})
			t.Run("reset deletes the target project", func(t *testing.T) {
				result, _, _ := handleLaunchReset(context.Background(), seed(t, topology.LaunchStatusFailed), "src-proj", source, WorkflowInput{
					ProductionProjectName: "myapp-prod",
					LaunchKey:             "lk",
					ConfirmDestructive:    &DestructiveAck{Operation: launchResetOperation, AcknowledgedTargets: []string{"myapp-prod", "prod-1"}},
				}, srv.URL)
				if report := unmarshalResetReport(t, result); report.DeletedProjectID != "prod-1" {
					t.Errorf("reset must delete prod-1, got %+v", report)
				}
			})
		})
	}
}
