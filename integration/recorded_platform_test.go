// Tests for: integration — the mock platform's recorded Zerops reads.
//
// The mock platform answers the endpoints e2e/record_test.go lists only from
// the recordings it wrote in e2e/recordings/: each is served on a local
// listener at its endpoint and decoded by zcp's real ZeropsClient, and the
// decoded answers seed platform.Mock. A recording that no longer decodes is
// re-recorded (`ZCP_RECORD=1 go test ./e2e -tags e2e -run TestRecord`),
// never hand-edited.

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// recordingsDir is TestRecord's output, located from this file so a test
// that changes its working directory still reads it.
func recordingsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("locate the recordings: no caller information")
	}
	return filepath.Join(filepath.Dir(file), "..", "e2e", "recordings")
}

// zeropsRecording is the recording file shape TestRecord writes.
type zeropsRecording struct {
	Provenance struct {
		Producer string `json:"producer"`
		Revision string `json:"revision"`
		Via      string `json:"via"`
		Schema   string `json:"schema"`
	} `json:"$provenance"`
	Value json.RawMessage `json:"value"`
}

// recordedRoute answers one recorded endpoint: its method, its path template
// with each {param} matching one segment, and the recorded body.
type recordedRoute struct {
	method string
	path   *regexp.Regexp
	body   []byte
}

// recordedPlatform is what zcp's real client decodes from the recordings.
type recordedPlatform struct {
	userInfo *platform.UserInfo
	project  *platform.Project
	services []platform.ServiceStack // the service-stack search
	// raw is each recording's value by schema, for expectations read
	// straight from the wire rather than through the decoder under test.
	raw map[string]json.RawMessage
}

// mock seeds a platform.Mock with the recorded reads. The direct list names
// the same services as the search (loadRecordedPlatform checks it), so the
// mock answers both from the search.
func (r *recordedPlatform) mock() *platform.Mock {
	return platform.NewMock().
		WithUserInfo(r.userInfo).
		WithProject(r.project).
		WithServices(r.services)
}

// service is the recorded service with the given hostname.
func (r *recordedPlatform) service(t *testing.T, hostname string) platform.ServiceStack {
	t.Helper()
	for _, svc := range r.services {
		if svc.Name == hostname {
			return svc
		}
	}
	t.Fatalf("the recorded project holds no service %q: re-record against an e2e project that holds one", hostname)
	return platform.ServiceStack{}
}

// recordedProjectID is the recorded project's id, for a seed that names the
// project defaultMock serves.
func recordedProjectID(t *testing.T) string {
	t.Helper()
	return loadRecordedPlatform(t).project.ID
}

// loadRecordedPlatform reads every recording, serves each at its endpoint
// and reads them back through zcp's real client.
func loadRecordedPlatform(t *testing.T) *recordedPlatform {
	t.Helper()
	dir := recordingsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read recordings: %v", err)
	}
	rec := &recordedPlatform{raw: map[string]json.RawMessage{}}
	var routes []recordedRoute
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var file zeropsRecording
		if err := json.Unmarshal(data, &file); err != nil {
			t.Fatalf("decode %s: %v", e.Name(), err)
		}
		p := file.Provenance
		if p.Producer != "zerops" || p.Revision == "" || p.Via == "" || p.Schema == "" || len(file.Value) == 0 {
			t.Fatalf("%s: $provenance needs producer zerops, revision, via and schema, and a value", e.Name())
		}
		method, tmpl, ok := strings.Cut(p.Schema, " ")
		if !ok {
			t.Fatalf("%s: schema %q is not \"METHOD /path\"", e.Name(), p.Schema)
		}
		pattern := regexp.MustCompile(`\\\{[^}]+\\\}`).ReplaceAllString(regexp.QuoteMeta(tmpl), `[^/]+`)
		routes = append(routes, recordedRoute{method: method, path: regexp.MustCompile("^" + pattern + "$"), body: file.Value})
		rec.raw[p.Schema] = file.Value
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, route := range routes {
			if route.method == r.Method && route.path.MatchString(r.URL.Path) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(route.body)
				return
			}
		}
		http.Error(w, `{"error":{"code":"notRecorded","message":"no recording for this endpoint"}}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.raw["GET /api/rest/public/project/{id}"], &project); err != nil {
		t.Fatalf("decode the recorded project's id: %v", err)
	}
	ctx := context.Background()
	client, err := platform.NewZeropsClient("recorded", srv.URL)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if rec.userInfo, err = client.GetUserInfo(ctx); err != nil {
		t.Fatalf("user info: %v", err)
	}
	if rec.project, err = client.GetProject(ctx, project.ID); err != nil {
		t.Fatalf("project: %v", err)
	}
	client.BindOrg(rec.project.ClientID)
	if rec.services, err = client.ListServices(ctx, rec.project.ID); err != nil {
		t.Fatalf("service search: %v", err)
	}
	direct, err := client.ListServicesDirect(ctx, rec.project.ID)
	if err != nil {
		t.Fatalf("project services: %v", err)
	}
	if got, want := serviceIDs(direct), serviceIDs(rec.services); got != want {
		t.Fatalf("the direct list names services %s, the search %s: re-record once the search caught up", got, want)
	}
	// The by-id recording was read for the direct list's first service by
	// name; it decodes to a service of the recorded project.
	service, err := client.GetService(ctx, "recorded")
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	if service.ID == "" || service.ProjectID != rec.project.ID {
		t.Fatalf("the by-id recording decodes to service %q of project %q, want one of project %q",
			service.ID, service.ProjectID, rec.project.ID)
	}
	return rec
}

// serviceIDs is the sorted, comma-joined ids of services.
func serviceIDs(services []platform.ServiceStack) string {
	ids := make([]string, 0, len(services))
	for _, s := range services {
		ids = append(ids, s.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// recordedUserHostnames lists the hostnames of the project's direct service
// list that Zerops does not mark isSystem, read from the recording's wire JSON.
func recordedUserHostnames(t *testing.T, rec *recordedPlatform) []string {
	t.Helper()
	var list struct {
		List []struct {
			Name     string `json:"name"`
			IsSystem bool   `json:"isSystem"`
		} `json:"list"`
	}
	if err := json.Unmarshal(rec.raw["GET /api/rest/public/project/{id}/service-stack"], &list); err != nil {
		t.Fatalf("decode the recorded service list: %v", err)
	}
	names := make([]string, 0, len(list.List))
	for _, s := range list.List {
		if !s.IsSystem {
			names = append(names, s.Name)
		}
	}
	return names
}

// witness: internal/ops/discover.go | if services[i].IsSystem() { → if !services[i].IsSystem() {
func TestRecordedPlatform_DiscoverListsEveryRecordedUserService(t *testing.T) {
	t.Parallel()

	rec := loadRecordedPlatform(t)
	hostnames := recordedUserHostnames(t, rec)
	if len(hostnames) == 0 {
		t.Fatal("the recorded project lists no user service: re-record against an e2e project that holds one")
	}

	session, cleanup := setupTestServer(t, rec.mock(), defaultLogFetcher())
	defer cleanup()

	text := callAndGetText(t, session, "zerops_discover", nil)
	var discovered ops.DiscoverResult
	if err := json.Unmarshal([]byte(text), &discovered); err != nil {
		t.Fatalf("discover answer is not JSON: %v", err)
	}
	got := map[string]bool{}
	for _, s := range discovered.Services {
		got[s.Hostname] = true
	}
	for _, h := range hostnames {
		if h == "" || !got[h] {
			t.Errorf("discover does not list the recorded service %q", h)
		}
	}
}
