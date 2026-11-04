// Tests for: internal/platform/org_reader.go — org mode reads the
// organization from the Zerops API with the credential its session holds,
// never from HQ (docs/spec-mate.md §2.11, OM-4).
package platform

import (
	"context"
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

type fakeAnswer struct {
	Status int
	Body   string
}

// chiefProject is the Chief's own project; org1 owns it.
const chiefProject = "pChief"

// fakeZerops answers the public API the way Zerops does for org mode's reads:
// the Chief's own project (owned by org1), the two searches, the member list,
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
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/user/list"):
			answer = fakeAnswer{Status: http.StatusOK, Body: `{"clientUserList":[]}`}
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
	readEverything(t, openTestOrgReader(t, srv, "person-token"))

	allowlist := []string{
		"GET /api/rest/public/project/pChief",
		"POST /api/rest/public/project/search",
		"POST /api/rest/public/service-stack/search",
		"GET /api/rest/public/client/org1/user/list",
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
		"GET /api/rest/public/client/org1/user/list": {Status: http.StatusOK, Body: `{"clientUserList":[` +
			`{"id":"cu1","clientId":"org1","userId":"u1","status":"ACTIVE","roleCode":"ADMIN","user":{"email":"dana@example.com","fullName":"Dana"}},` +
			`{"id":"cu2","clientId":"org1","userId":"u2","status":"INVITED","roleCode":"READ_ONLY","user":{"email":"eli@example.com","fullName":"Eli"}},` +
			`{"id":"cu3","clientId":"org1","userId":"u3","status":"ACTIVE","roleCode":"BASIC_USER","user":{"email":"token-abc@zerops.io","fullName":"Shop - Ada key"}}]}`},
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
		case "/api/rest/public/project/search", "/api/rest/public/service-stack/search":
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
		{name: "members refused 403", read: "GET /api/rest/public/client/org1/user/list", status: http.StatusForbidden, code: "insufficientPermissions",
			call: func(r *OrgReader) error { _, err := r.ListMembers(context.Background()); return err }},
		{name: "members refused 400", read: "GET /api/rest/public/client/org1/user/list", status: http.StatusBadRequest, code: "userNotFound",
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
