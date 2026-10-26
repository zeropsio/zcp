// Tests for: internal/platform/org_reader.go — org mode reads HQ's Chief
// overview with the project key in the header, and OrgReads, exactly the
// requests it makes (docs/spec-mate.md §2.11, §3.11).
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

// recordedRequest is one request a fake HQ received.
type recordedRequest struct {
	Method, Path, Auth, Query, Body string
}

// fakeHQ answers the overview the way HQ does: the two searches in the
// platform's page shape, the members as persons, recording each request.
// answers maps "METHOD path" to a ready response; the others answer empty.
func fakeHQ(t *testing.T, answers map[string]fakeAnswer) (*httptest.Server, func() []recordedRequest) {
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
		if !ok {
			answer = fakeAnswer{Status: http.StatusOK, Body: `{"items":[],"totalHits":0,"members":[]}`}
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

type fakeAnswer struct {
	Status int
	Body   string
}

// matchesOrgRead reports whether a request is the one read names.
func matchesOrgRead(read OrgRead, method, path string) bool {
	return read.Method == method && read.Path == path
}

// TestOrgReads_AreExactlyWhatOrgModeReads drives every OrgReader method
// against a recording HQ: each request it sends is one of OrgReads, and each
// of OrgReads is sent — the allowlist neither short of what org mode reads nor
// wider than it, at HQ's own paths.
func TestOrgReads_AreExactlyWhatOrgModeReads(t *testing.T) {
	t.Parallel()
	srv, seen := fakeHQ(t, nil)
	r, err := OpenOrgReader(OrgAccess{Origin: srv.URL, Key: "project-key"})
	if err != nil {
		t.Fatalf("OpenOrgReader: %v", err)
	}
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"ListProjects": func() error { _, err := r.ListProjects(ctx); return err },
		"ListServices": func() error { _, err := r.ListServices(ctx, "p1"); return err },
		"ListMembers":  func() error { _, err := r.ListMembers(ctx); return err },
	} {
		if err := call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}

	hit := make(map[OrgRead]bool)
	for _, req := range seen() {
		i := slices.IndexFunc(OrgReads(), func(read OrgRead) bool { return matchesOrgRead(read, req.Method, req.Path) })
		if i < 0 {
			t.Errorf("org mode read %s %s, which OrgReads does not list", req.Method, req.Path)
			continue
		}
		hit[OrgReads()[i]] = true
	}
	want := []OrgRead{
		{Method: http.MethodPost, Path: "/api/chief/zerops/project/search"},
		{Method: http.MethodPost, Path: "/api/chief/zerops/service-stack/search"},
		{Method: http.MethodGet, Path: "/api/chief/members"},
	}
	if !reflect.DeepEqual(OrgReads(), want) {
		t.Errorf("OrgReads = %v, want exactly projects, services and members %v", OrgReads(), want)
	}
	for _, read := range OrgReads() {
		if !hit[read] {
			t.Errorf("OrgReads lists %s %s, which org mode never reads", read.Method, read.Path)
		}
	}
}

// TestOrgReader_EveryReadPresentsTheProjectKeyInTheHeaderOnly: the key is the
// Authorization header's, "Project <key>", on every read, and in no path,
// query or body (TC-3).
func TestOrgReader_EveryReadPresentsTheProjectKeyInTheHeaderOnly(t *testing.T) {
	t.Parallel()
	srv, seen := fakeHQ(t, nil)
	r, err := OpenOrgReader(OrgAccess{Origin: srv.URL + "/", Key: "sekret-key"})
	if err != nil {
		t.Fatalf("OpenOrgReader: %v", err)
	}
	ctx := context.Background()
	if _, err := r.ListProjects(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ListServices(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ListMembers(ctx); err != nil {
		t.Fatal(err)
	}
	reqs := seen()
	if len(reqs) != 3 {
		t.Fatalf("requests = %+v, want one per read", reqs)
	}
	for _, req := range reqs {
		if req.Auth != "Project sekret-key" {
			t.Errorf("%s %s carried Authorization %q, want %q", req.Method, req.Path, req.Auth, "Project sekret-key")
		}
		if strings.Contains(req.Path+req.Query+req.Body, "sekret-key") {
			t.Errorf("%s %s carries the key outside the header: %+v", req.Method, req.Path, req)
		}
	}
}

// TestOrgReader_ReadsTheOverviewAsHQAnswersIt: the project and service pages
// map to the platform's types, services page past the search's page size, and
// members come as persons with their roles.
func TestOrgReader_ReadsTheOverviewAsHQAnswersIt(t *testing.T) {
	t.Parallel()
	srv, _ := fakeHQ(t, map[string]fakeAnswer{
		"POST /api/chief/zerops/project/search": {Status: http.StatusOK, Body: `{"items":[` +
			`{"id":"p1","clientId":"org","name":"Shop - Ada","status":"ACTIVE","tagList":["mate"],"description":"","envList":[]},` +
			`{"id":"p2","clientId":"org","name":"Shop - production","status":"ACTIVE","tagList":[],"description":"","envList":[]}],"totalHits":2}`},
		"GET /api/chief/members": {Status: http.StatusOK, Body: `{"members":[` +
			`{"userId":"u1","fullName":"Dana","role":"ADMIN","status":"ACTIVE"},` +
			`{"userId":"u2","fullName":"Eli","role":"READ_ONLY","status":"INVITED"}]}`},
	})
	r, err := OpenOrgReader(OrgAccess{Origin: srv.URL, Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
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
}

// TestOrgReader_AnHQRefusalIsNamedWithItsCode: a project the mark no longer
// names is refused by HQ; the reader says so in HQ's words, not as a Zerops
// failure.
func TestOrgReader_AnHQRefusalIsNamedWithItsCode(t *testing.T) {
	t.Parallel()
	srv, _ := fakeHQ(t, map[string]fakeAnswer{
		"GET /api/chief/members": {Status: http.StatusForbidden, Body: `{"code":"forbidden","reason":"not_chief_project"}`},
	})
	r, err := OpenOrgReader(OrgAccess{Origin: srv.URL, Key: "k"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.ListMembers(context.Background())
	var pe *PlatformError
	if !errors.As(err, &pe) || !strings.Contains(pe.Message, "not_chief_project") {
		t.Fatalf("err = %v, want a platform error naming not_chief_project", err)
	}
}

func TestOpenOrgReader_RefusesWhatIsNoHQ(t *testing.T) {
	t.Parallel()
	for _, access := range []OrgAccess{
		{Origin: "hq.example", Key: "k"},
		{Origin: "ftp://hq.example", Key: "k"},
		{Origin: "https://", Key: "k"},
		{Origin: "", Key: "k"},
		{Origin: "https://hq.example", Key: " "},
	} {
		t.Run(access.Origin+"|"+access.Key, func(t *testing.T) {
			t.Parallel()
			_, err := OpenOrgReader(access)
			var pe *PlatformError
			if !errors.As(err, &pe) || pe.Code != ErrInvalidParameter {
				t.Fatalf("err = %v, want %s", err, ErrInvalidParameter)
			}
		})
	}
}
