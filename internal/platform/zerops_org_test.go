package platform_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// Tests for: the org (client) a ZeropsClient targets — GetUserInfo carries
// every membership, never one collapsed to clientUserList[0].

func TestGetUserInfo_CarriesEveryMembershipInPlatformOrder(t *testing.T) {
	t.Parallel()
	srv := newUserInfoServer(t, [][2]string{
		{"cu-a", "org-a"},
		{"cu-b", "org-b"},
	})
	client, err := platform.NewZeropsClient("token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}

	info, err := client.GetUserInfo(context.Background())
	if err != nil {
		t.Fatalf("GetUserInfo: %v", err)
	}

	want := []platform.ClientMembership{
		{ClientID: "org-a", ClientUserID: "cu-a"},
		{ClientID: "org-b", ClientUserID: "cu-b"},
	}
	if !reflect.DeepEqual(info.Memberships, want) {
		t.Errorf("Memberships = %+v, want %+v", info.Memberships, want)
	}
}

// orgAPI is a fake Zerops API for a token in several orgs: /user/info
// lists memberships, projects are read by id or searched by clientId, and
// every search body is captured so a test can see which org it targeted.
type orgAPI struct {
	memberships [][2]string        // (clientUserID, clientID)
	projects    map[string]string  // projectID → owning clientID
	searches    chan searchRequest // every POST …/search, in order
}

type searchRequest struct {
	Path     string
	ClientID string
}

func newOrgAPI(t *testing.T, memberships [][2]string, projects map[string]string) (*orgAPI, *httptest.Server) {
	t.Helper()
	api := &orgAPI{memberships: memberships, projects: projects, searches: make(chan searchRequest, 16)}
	srv := httptest.NewServer(http.HandlerFunc(api.serve))
	t.Cleanup(srv.Close)
	return api, srv
}

func (a *orgAPI) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	const prefix = "/api/rest/public/"
	path := strings.TrimPrefix(r.URL.Path, prefix)
	switch {
	case path == "user/info":
		entries := make([]string, 0, len(a.memberships))
		for _, m := range a.memberships {
			entries = append(entries, fmt.Sprintf(`{"id":%q,"clientId":%q,"userId":"user-1"}`, m[0], m[1]))
		}
		fmt.Fprintf(w, `{"id":"user-1","email":"a@b.com","fullName":"ZCP","clientUserList":[%s]}`, strings.Join(entries, ","))
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
		a.searches <- searchRequest{Path: path, ClientID: clientID}
		items := []string{}
		if path == "project/search" {
			for id, owner := range a.projects {
				if owner == clientID {
					items = append(items, fmt.Sprintf(`{"id":%q,"clientId":%q,"name":"p-%s","status":"ACTIVE"}`, id, owner, id))
				}
			}
		}
		fmt.Fprintf(w, `{"items":[%s]}`, strings.Join(items, ","))
	case strings.HasPrefix(path, "project/"):
		id := strings.TrimPrefix(path, "project/")
		owner, ok := a.projects[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"code":"projectNotFound","message":"not found"}}`)
			return
		}
		fmt.Fprintf(w, `{"id":%q,"clientId":%q,"name":"p-%s","status":"ACTIVE"}`, id, owner, id)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestProjectReads_CarryTheOrgThatOwnsTheProject(t *testing.T) {
	t.Parallel()
	_, srv := newOrgAPI(t,
		[][2]string{{"cu-a", "org-a"}, {"cu-b", "org-b"}},
		map[string]string{"proj-b": "org-b"},
	)
	client, err := platform.NewZeropsClient("token", srv.URL)
	if err != nil {
		t.Fatalf("NewZeropsClient: %v", err)
	}
	ctx := context.Background()

	got, err := client.GetProject(ctx, "proj-b")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.ClientID != "org-b" {
		t.Errorf("GetProject ClientID = %q, want org-b", got.ClientID)
	}

	listed, err := client.ListProjects(ctx, "org-b")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(listed) != 1 || listed[0].ClientID != "org-b" {
		t.Errorf("ListProjects = %+v, want one project owned by org-b", listed)
	}
}

func TestListServices_SearchesTheOrgOfTheBoundProject(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		memberships [][2]string
		bind        string
		wantClient  string
		wantCode    string
	}{
		{
			name:        "a client bound to the project's org searches that org, not the first membership",
			memberships: [][2]string{{"cu-a", "org-a"}, {"cu-b", "org-b"}},
			bind:        "org-b",
			wantClient:  "org-b",
		},
		{
			name:        "a single-org token searches its only org, as before",
			memberships: [][2]string{{"cu-a", "org-a"}},
			wantClient:  "org-a",
		},
		{
			name:        "a token in several orgs with no bound project refuses instead of guessing",
			memberships: [][2]string{{"cu-a", "org-a"}, {"cu-b", "org-b"}},
			wantCode:    platform.ErrOrgAmbiguous,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api, srv := newOrgAPI(t, tt.memberships, nil)
			client, err := platform.NewZeropsClient("token", srv.URL)
			if err != nil {
				t.Fatalf("NewZeropsClient: %v", err)
			}
			if tt.bind != "" {
				client.BindOrg(tt.bind)
			}

			_, err = client.ListServices(context.Background(), "proj-b")
			if tt.wantCode != "" {
				var pe *platform.PlatformError
				if !errors.As(err, &pe) || pe.Code != tt.wantCode {
					t.Fatalf("ListServices error = %v, want code %s", err, tt.wantCode)
				}
				if len(api.searches) != 0 {
					t.Errorf("a refused call must not search, got %d searches", len(api.searches))
				}
				return
			}
			if err != nil {
				t.Fatalf("ListServices: %v", err)
			}
			got := <-api.searches
			if got.Path != "service-stack/search" || got.ClientID != tt.wantClient {
				t.Errorf("search = %+v, want service-stack/search with clientId %s", got, tt.wantClient)
			}
		})
	}
}

func TestAccountClient_SearchesTheConfiguredOrgNotTheFirstMembership(t *testing.T) {
	t.Parallel()
	api, srv := newOrgAPI(t, [][2]string{{"cu-a", "org-a"}, {"cu-b", "org-b"}}, nil)
	admin, err := platform.NewProjectAdminClientForClient("token", srv.URL, "org-b")
	if err != nil {
		t.Fatalf("NewProjectAdminClientForClient: %v", err)
	}
	t.Cleanup(admin.Close)

	if _, err := admin.ListServices(context.Background(), "proj-b"); err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if got := <-api.searches; got.ClientID != "org-b" {
		t.Errorf("service search clientId = %q, want org-b", got.ClientID)
	}
}

// The mock answers OrgID the way ZeropsClient does, so tool tests see the
// real refusal for an unbound token in several orgs.
func TestMockOrgID_AnswersLikeTheRealClient(t *testing.T) {
	t.Parallel()
	two := &platform.UserInfo{Memberships: []platform.ClientMembership{{ClientID: "org-a"}, {ClientID: "org-b"}}}
	tests := []struct {
		name     string
		info     *platform.UserInfo
		bind     string
		want     string
		wantCode string
	}{
		{name: "a bound org wins", info: two, bind: "org-b", want: "org-b"},
		{name: "an unbound single-org token answers its only org", info: &platform.UserInfo{Memberships: []platform.ClientMembership{{ClientID: "org-a"}}}, want: "org-a"},
		{name: "an unbound token in several orgs refuses", info: two, wantCode: platform.ErrOrgAmbiguous},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := platform.NewMock().WithUserInfo(tt.info)
			if tt.bind != "" {
				m.BindOrg(tt.bind)
			}
			got, err := m.OrgID(context.Background())
			if tt.wantCode != "" {
				var pe *platform.PlatformError
				if !errors.As(err, &pe) || pe.Code != tt.wantCode {
					t.Fatalf("OrgID error = %v, want code %s", err, tt.wantCode)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("OrgID = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

// The Mate unit builds its clients from the container's key and project;
// like the CLI (auth.Resolve), they target the org that owns the project.
func TestNewProjectClient_TargetsTheOrgOwningTheProject(t *testing.T) {
	t.Parallel()
	api, srv := newOrgAPI(t,
		[][2]string{{"cu-a", "org-a"}, {"cu-b", "org-b"}},
		map[string]string{"proj-b": "org-b"},
	)
	client, err := platform.NewProjectClient(context.Background(), "token", srv.URL, "proj-b")
	if err != nil {
		t.Fatalf("NewProjectClient: %v", err)
	}
	if _, err := client.GetProjectEnv(context.Background(), "proj-b"); err != nil {
		t.Fatalf("GetProjectEnv: %v", err)
	}
	if got := <-api.searches; got.ClientID != "org-b" {
		t.Errorf("env search clientId = %q, want org-b", got.ClientID)
	}
}
