// Tests for: internal/platform/org_reader.go — org mode reads the
// organization from the Zerops API with the project's key, never from HQ
// (docs/spec-mate.md §2.11, OM-4).
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// recordedRequest is one request the fake Zerops API received.
type recordedRequest struct {
	Method, Path, Auth, Query, Body string
}

func TestOrgReader_ListMembers_PagesTheOrganisationSearch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		secondPage string
		wantIDs    []string
		wantError  bool
	}{
		{name: "all pages", secondPage: `{"items":[{"userId":"u3","status":"ACTIVE","roleCode":"OWNER","user":{"fullName":"Three"}}],"totalHits":3}`, wantIDs: []string{"u1", "u2", "u3"}},
		{name: "incomplete search", secondPage: `{"items":[],"totalHits":3}`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method + " " + r.URL.Path {
				case "GET /api/rest/public/project/pChief":
					_, _ = io.WriteString(w, `{"id":"pChief","clientId":"org1","name":"Chief","status":"ACTIVE"}`)
				case "GET /api/rest/public/client/org1/user/list":
					_, _ = io.WriteString(w, `{"clientUserList":[]}`)
				case "POST /api/rest/public/client-user/search":
					var query struct {
						Search []struct {
							Name     string `json:"name"`
							Operator string `json:"operator"`
							Value    string `json:"value"`
						} `json:"search"`
						Sort []struct {
							Name string `json:"name"`
						} `json:"sort"`
						Offset int `json:"offset"`
					}
					if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
						t.Errorf("decode search: %v", err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if len(query.Search) != 1 || query.Search[0].Name != "clientId" || query.Search[0].Operator != "eq" || query.Search[0].Value != "org1" || len(query.Sort) != 1 || query.Sort[0].Name != "id" {
						t.Errorf("member search = %+v, want the organisation filter and id sort", query)
					}
					switch query.Offset {
					case 0:
						_, _ = io.WriteString(w, `{"items":[{"userId":"u1","status":"ACTIVE","roleCode":"ADMIN","user":{"fullName":"One"}},{"userId":"u2","status":"ACTIVE","roleCode":"BASIC_USER","user":{"fullName":"Two"}}],"totalHits":3}`)
					case 2:
						_, _ = io.WriteString(w, tc.secondPage)
					default:
						t.Errorf("unexpected search offset %d", query.Offset)
						w.WriteHeader(http.StatusBadRequest)
					}
				default:
					w.WriteHeader(http.StatusForbidden)
				}
			}))
			defer srv.Close()
			members, err := openTestOrgReader(t, srv, "project-key").ListMembers(context.Background())
			if tc.wantError {
				if err == nil {
					t.Fatal("incomplete member search succeeded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]string, len(members))
			for i, member := range members {
				ids[i] = member.UserID
			}
			if !slices.Equal(ids, tc.wantIDs) {
				t.Errorf("member IDs = %v, want %v", ids, tc.wantIDs)
			}
		})
	}
}

type fakeAnswer struct {
	Status int
	Body   string
}

// chiefProject is the Chief's own project; org1 owns it.
const chiefProject = "pChief"

// fakeZerops answers the public API the way Zerops does for org mode's reads:
// the Chief's own project (owned by org1), the searches for projects,
// services, and members,
// recording each request. answers maps "METHOD path" to a ready response; the
// others answer an empty page.
func fakeZerops(t *testing.T, answers map[string]fakeAnswer) (*httptest.Server, func() []recordedRequest) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []recordedRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, recordedRequest{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Query: r.URL.RawQuery, Body: string(body)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		answer, ok := answers[r.Method+" "+r.URL.Path]
		switch {
		case ok:
		case r.Method == http.MethodGet && r.URL.Path == "/api/rest/public/project/"+chiefProject:
			answer = fakeAnswer{Status: http.StatusOK, Body: `{"id":"pChief","clientId":"org1","name":"Chief - Ada","status":"ACTIVE"}`}
		default:
			answer = fakeAnswer{Status: http.StatusOK, Body: `{"items":[],"totalHits":0}`}
		}
		w.WriteHeader(answer.Status)
		_, _ = io.WriteString(w, answer.Body)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

func openTestOrgReader(t *testing.T, srv *httptest.Server, token string) *OrgReader {
	t.Helper()
	r, err := OpenOrgReader(OrgAccess{APIHost: srv.URL, ProjectID: chiefProject, Token: token})
	if err != nil {
		t.Fatalf("OpenOrgReader: %v", err)
	}
	return r
}

// readEverything drives every OrgReader method once.
func readEverything(t *testing.T, r *OrgReader) {
	t.Helper()
	ctx := context.Background()
	if _, err := r.ListProjects(ctx); err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if _, err := r.ListServices(ctx, ""); err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if _, err := r.ListServices(ctx, "p1"); err != nil {
		t.Fatalf("ListServices(p1): %v", err)
	}
	if _, err := r.ListMembers(ctx); err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
}

// TestOrgReader_ReadsOnlyTheAllowlistFromZerops: every request org mode sends
// goes to the Zerops public API and is one of the allowlist's reads — the
// organization of its own project, the organization's projects and
// services, its members — and each of them is sent. Nothing goes to an HQ
// path.
func TestOrgReader_ReadsOnlyTheAllowlistFromZerops(t *testing.T) {
	t.Parallel()
	srv, seen := fakeZerops(t, nil)
	readEverything(t, openTestOrgReader(t, srv, "project-key"))

	allowlist := []string{
		"GET /api/rest/public/project/pChief",
		"POST /api/rest/public/project/search",
		"POST /api/rest/public/service-stack/search",
		"POST /api/rest/public/client-user/search",
	}
	hit := map[string]bool{}
	for _, req := range seen() {
		read := req.Method + " " + req.Path
		if !slices.Contains(allowlist, read) {
			t.Errorf("org mode read %s, outside the allowlist %v", read, allowlist)
		}
		hit[read] = true
	}
	for _, read := range allowlist {
		if !hit[read] {
			t.Errorf("org mode never read %s", read)
		}
	}
}

// TestOrgReader_EveryReadPresentsTheSessionCredentialInTheHeaderOnly: the
// credential the session holds is the bearer of every read and appears in no
// path, query or body.
func TestOrgReader_EveryReadPresentsTheSessionCredentialInTheHeaderOnly(t *testing.T) {
	t.Parallel()
	srv, seen := fakeZerops(t, nil)
	readEverything(t, openTestOrgReader(t, srv, "sekret-token"))

	reqs := seen()
	if len(reqs) == 0 {
		t.Fatal("org mode read nothing")
	}
	for _, req := range reqs {
		if req.Auth != "Bearer sekret-token" {
			t.Errorf("%s %s carried Authorization %q, want %q", req.Method, req.Path, req.Auth, "Bearer sekret-token")
		}
		if strings.Contains(req.Path+req.Query+req.Body, "sekret-token") {
			t.Errorf("%s %s carries the credential outside the header: %+v", req.Method, req.Path, req)
		}
	}
}

// TestOrgReader_ReadsTheOverviewAsZeropsAnswersIt: every search is pinned to
// the organization owning the Chief's own project (read once), projects keep
// their tags, services page past the search's page size, and members are the
// people with their roles — an integration token's member row is no person.
func TestOrgReader_ReadsTheOverviewAsZeropsAnswersIt(t *testing.T) {
	t.Parallel()
	srv, seen := fakeZerops(t, map[string]fakeAnswer{
		"POST /api/rest/public/project/search": {Status: http.StatusOK, Body: `{"items":[` +
			`{"id":"p1","clientId":"org1","name":"Shop - Ada","status":"ACTIVE","tagList":["mate"],"description":"","envList":[]},` +
			`{"id":"p2","clientId":"org1","name":"Shop - production","status":"ACTIVE","tagList":[],"description":"","envList":[]}],"totalHits":2}`},
		"POST /api/rest/public/client-user/search": {Status: http.StatusOK, Body: `{"items":[` +
			`{"id":"cu1","clientId":"org1","userId":"u1","status":"ACTIVE","roleCode":"ADMIN","user":{"email":"dana@example.com","fullName":"Dana"}},` +
			`{"id":"cu2","clientId":"org1","userId":"u2","status":"INVITED","roleCode":"READ_ONLY","user":{"email":"eli@example.com","fullName":"Eli"}},` +
			`{"id":"cu3","clientId":"org1","userId":"u3","status":"ACTIVE","roleCode":"BASIC_USER","user":{"email":"token-abc@zerops.io","fullName":"Shop - Ada key"}}],"totalHits":3}`},
	})
	r := openTestOrgReader(t, srv, "t")
	ctx := context.Background()

	projects, err := r.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	gotProjects := make([]string, len(projects))
	for i, p := range projects {
		gotProjects[i] = fmt.Sprintf("%s|%s|%s|%v", p.ID, p.Name, p.Status, p.Tags)
	}
	if want := []string{"p1|Shop - Ada|ACTIVE|[mate]", "p2|Shop - production|ACTIVE|[]"}; !slices.Equal(gotProjects, want) {
		t.Errorf("projects = %v, want %v", gotProjects, want)
	}
	if _, err := r.ListServices(ctx, "p1"); err != nil {
		t.Fatalf("ListServices: %v", err)
	}

	members, err := r.ListMembers(ctx)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	want := []OverviewMember{
		{UserID: "u1", FullName: "Dana", Role: "ADMIN", Status: "ACTIVE"},
		{UserID: "u2", FullName: "Eli", Role: "READ_ONLY", Status: "INVITED"},
	}
	if !reflect.DeepEqual(members, want) {
		t.Errorf("members = %+v, want %+v", members, want)
	}

	ownReads := 0
	for _, req := range seen() {
		switch req.Path {
		case "/api/rest/public/project/pChief":
			ownReads++
		case "/api/rest/public/project/search", "/api/rest/public/service-stack/search", "/api/rest/public/client-user/search":
			if !strings.Contains(req.Body, `"name":"clientId","operator":"eq","value":"org1"`) {
				t.Errorf("%s searched %s, want it pinned to clientId org1", req.Path, req.Body)
			}
		}
		if req.Path == "/api/rest/public/service-stack/search" && !strings.Contains(req.Body, `"name":"projectId","operator":"eq","value":"p1"`) {
			t.Errorf("service search %s, want it cut to project p1", req.Body)
		}
	}
	if ownReads != 1 {
		t.Errorf("read the Chief's own project %d times, want once", ownReads)
	}
}

// TestOrgReader_AZeropsRefusalIsShownOnceInItsOwnWords: what the credential
// may not see, Zerops refuses (403, 400); org mode says so with Zerops' code
// and asks once — no retry, no other credential.
func TestOrgReader_AZeropsRefusalIsShownOnceInItsOwnWords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		read   string
		status int
		code   string
		call   func(*OrgReader) error
	}{
		{name: "members refused 403", read: "POST /api/rest/public/client-user/search", status: http.StatusForbidden, code: "insufficientPermissions",
			call: func(r *OrgReader) error { _, err := r.ListMembers(context.Background()); return err }},
		{name: "members refused 400", read: "POST /api/rest/public/client-user/search", status: http.StatusBadRequest, code: "userNotFound",
			call: func(r *OrgReader) error { _, err := r.ListMembers(context.Background()); return err }},
		{name: "projects refused 403", read: "POST /api/rest/public/project/search", status: http.StatusForbidden, code: "insufficientPermissions",
			call: func(r *OrgReader) error { _, err := r.ListProjects(context.Background()); return err }},
		{name: "own project refused 400", read: "GET /api/rest/public/project/pChief", status: http.StatusBadRequest, code: "projectNotFound",
			call: func(r *OrgReader) error { _, err := r.ListServices(context.Background(), ""); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, seen := fakeZerops(t, map[string]fakeAnswer{
				tt.read: {Status: tt.status, Body: `{"error":{"code":"` + tt.code + `","message":"no"}}`},
			})
			err := tt.call(openTestOrgReader(t, srv, "t"))
			var pe *PlatformError
			if !errors.As(err, &pe) || pe.APICode != tt.code {
				t.Fatalf("err = %v, want a platform error carrying Zerops' %s", err, tt.code)
			}
			asked := 0
			for _, req := range seen() {
				if req.Method+" "+req.Path == tt.read {
					asked++
				}
			}
			if asked != 1 {
				t.Errorf("asked %s %d times, want once", tt.read, asked)
			}
		})
	}
}

func TestOpenOrgReader_RefusesWithoutACredentialOrAProject(t *testing.T) {
	t.Parallel()
	for _, access := range []OrgAccess{
		{APIHost: "api.example", ProjectID: chiefProject, Token: " "},
		{APIHost: "api.example", ProjectID: "", Token: "t"},
	} {
		t.Run(access.ProjectID+"|"+access.Token, func(t *testing.T) {
			t.Parallel()
			_, err := OpenOrgReader(access)
			var pe *PlatformError
			if !errors.As(err, &pe) || pe.Code != ErrInvalidParameter {
				t.Fatalf("err = %v, want %s", err, ErrInvalidParameter)
			}
		})
	}
}
