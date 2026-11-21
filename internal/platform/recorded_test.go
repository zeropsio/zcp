package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// recording is the file shape e2e/record_test.go (TestRecord) writes: Zerops's
// own answer to one endpoint, with where it came from.
type recording struct {
	Provenance struct {
		Producer string `json:"producer"`
		Schema   string `json:"schema"`
	} `json:"$provenance"`
	Value json.RawMessage `json:"value"`
}

// recordedRoute answers one recorded endpoint: its method, its path template
// with each {param} matching one segment, and the recorded body.
type recordedRoute struct {
	file   string
	method string
	path   *regexp.Regexp
	body   []byte
	served bool
}

// recordedTransport answers the client's requests in process from the
// recordings; a request no recording answers fails the case.
type recordedTransport struct {
	t      *testing.T
	routes []*recordedRoute
}

func (rt *recordedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
	for _, route := range rt.routes {
		if route.method == req.Method && route.path.MatchString(req.URL.Path) {
			route.served = true
			return &http.Response{
				Status:        "200 OK",
				StatusCode:    http.StatusOK,
				Proto:         "HTTP/1.1",
				ProtoMajor:    1,
				ProtoMinor:    1,
				Header:        http.Header{"Content-Type": []string{"application/json"}},
				Body:          io.NopCloser(bytes.NewReader(route.body)),
				ContentLength: int64(len(route.body)),
				Request:       req,
			}, nil
		}
	}
	rt.t.Errorf("no recording answers %s %s", req.Method, req.URL.Path)
	return nil, fmt.Errorf("no recording answers %s %s", req.Method, req.URL.Path)
}

// loadRecordings reads every recording TestRecord wrote as routes and as each
// one's raw value by schema. go test runs a package's tests in its directory,
// and the path is relative to it: the file's own path is no help, since
// scripts/gate builds with -trimpath, which leaves runtime.Caller the module
// path instead of one on disk.
func loadRecordings(t *testing.T) ([]*recordedRoute, map[string]json.RawMessage) {
	t.Helper()
	dir := filepath.Join("..", "..", "e2e", "recordings")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read recordings: %v", err)
	}
	param := regexp.MustCompile(`\\\{[^}]+\\\}`)
	var routes []*recordedRoute
	raw := map[string]json.RawMessage{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var rec recording
		if err := json.Unmarshal(data, &rec); err != nil {
			t.Fatalf("decode %s: %v", e.Name(), err)
		}
		method, tmpl, ok := strings.Cut(rec.Provenance.Schema, " ")
		if rec.Provenance.Producer != "zerops" || !ok || len(rec.Value) == 0 {
			t.Fatalf("%s: needs $provenance producer zerops, schema \"METHOD /path\" and a value", e.Name())
		}
		pattern := param.ReplaceAllString(regexp.QuoteMeta(tmpl), `[^/]+`)
		routes = append(routes, &recordedRoute{
			file: e.Name(), method: method, path: regexp.MustCompile("^" + pattern + "$"), body: rec.Value,
		})
		raw[rec.Provenance.Schema] = rec.Value
	}
	if len(routes) == 0 {
		t.Fatalf("no recordings in %s", dir)
	}
	return routes, raw
}

// decodeRaw reads a recording's raw value into the case's own view of the wire.
func decodeRaw(t *testing.T, raw map[string]json.RawMessage, schema string, into any) {
	t.Helper()
	value, ok := raw[schema]
	if !ok {
		t.Fatalf("no recording of %s", schema)
	}
	if err := json.Unmarshal(value, into); err != nil {
		t.Fatalf("decode the recorded %s: %v", schema, err)
	}
}

// rawService is a service as the recording's wire JSON holds it.
type rawService struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	ProjectID string `json:"projectId"`
	Ports     []struct {
		Port        int    `json:"port"`
		Protocol    string `json:"protocol"`
		Scheme      string `json:"scheme"`
		PortRouting *bool  `json:"portRouting"`
		HTTPRouting *bool  `json:"httpRouting"`
	} `json:"ports"`
}

// checkService compares a decoded service with the recording's wire JSON:
// hostname, status, project and every port.
func checkService(t *testing.T, read string, got ServiceStack, want rawService) {
	t.Helper()
	if got.ID != want.ID || got.Name != want.Name || got.Status != want.Status || got.ProjectID != want.ProjectID {
		t.Errorf("%s: service decoded as id %q hostname %q status %q project %q, recorded id %q hostname %q status %q project %q",
			read, got.ID, got.Name, got.Status, got.ProjectID, want.ID, want.Name, want.Status, want.ProjectID)
	}
	if len(got.Ports) != len(want.Ports) {
		t.Errorf("%s: service %q decoded %d ports, recorded %d", read, want.Name, len(got.Ports), len(want.Ports))
		return
	}
	for i, p := range want.Ports {
		portRouting := p.PortRouting != nil && *p.PortRouting
		httpRouting := p.HTTPRouting != nil && *p.HTTPRouting
		g := got.Ports[i]
		if g.Port != p.Port || g.Protocol != p.Protocol || g.Scheme != p.Scheme ||
			g.Public != (portRouting || httpRouting) || g.HTTPSupport != httpRouting {
			t.Errorf("%s: service %q port %d decoded as %+v, recorded port %d protocol %q scheme %q portRouting %v httpRouting %v",
				read, want.Name, i, g, p.Port, p.Protocol, p.Scheme, portRouting, httpRouting)
		}
	}
}

// checkServices compares a decoded list with the recorded list's services of
// the project, in the recorded order.
func checkServices(t *testing.T, read string, got []ServiceStack, recorded []rawService, projectID string) {
	t.Helper()
	var want []rawService
	for _, s := range recorded {
		if s.ProjectID == projectID {
			want = append(want, s)
		}
	}
	if len(want) == 0 {
		t.Fatalf("%s: the recording holds no service of project %q: re-record against an e2e project that holds one", read, projectID)
	}
	if len(got) != len(want) {
		t.Fatalf("%s: decoded %d services, recorded %d", read, len(got), len(want))
	}
	for i := range want {
		checkService(t, read, got[i], want[i])
	}
}

// RC-DECODE — Every recording in `e2e/recordings/` decodes through ZeropsClient's own reads into the user, the project and every recorded service with its hostname, status and ports as the recording holds them.
// witness: internal/platform/zerops_mappers.go | Status:            s.Status.String(), → Status:            "",
func TestRecorded_RCDecode_EveryRecordingDecodesAsRecorded(t *testing.T) {
	t.Parallel()
	const (
		userSchema    = "GET /api/rest/public/user/info"
		projectSchema = "GET /api/rest/public/project/{id}"
		searchSchema  = "POST /api/rest/public/service-stack/search"
		directSchema  = "GET /api/rest/public/project/{id}/service-stack"
		serviceSchema = "GET /api/rest/public/service-stack/{id}"
	)
	routes, raw := loadRecordings(t)

	var user struct {
		ID             string `json:"id"`
		FullName       string `json:"fullName"`
		Email          string `json:"email"`
		ClientUserList []struct {
			ID       string `json:"id"`
			ClientID string `json:"clientId"`
		} `json:"clientUserList"`
	}
	decodeRaw(t, raw, userSchema, &user)
	var project struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Status   string `json:"status"`
		ClientID string `json:"clientId"`
	}
	decodeRaw(t, raw, projectSchema, &project)
	var search struct {
		Items []rawService `json:"items"`
	}
	decodeRaw(t, raw, searchSchema, &search)
	var direct struct {
		List []rawService `json:"list"`
	}
	decodeRaw(t, raw, directSchema, &direct)
	var service rawService
	decodeRaw(t, raw, serviceSchema, &service)

	client := newZeropsClientOn(func() string { return "recorded" }, "https://zerops.recorded.invalid/",
		&http.Client{Transport: &recordedTransport{t: t, routes: routes}})
	ctx := context.Background()

	gotUser, err := client.GetUserInfo(ctx)
	if err != nil {
		t.Fatalf("user info: %v", err)
	}
	if gotUser.UserID != user.ID || gotUser.FullName != user.FullName || gotUser.Email != user.Email {
		t.Errorf("user decoded as id %q name %q email %q, recorded id %q name %q email %q",
			gotUser.UserID, gotUser.FullName, gotUser.Email, user.ID, user.FullName, user.Email)
	}
	if len(gotUser.Memberships) != len(user.ClientUserList) {
		t.Errorf("user decoded %d memberships, recorded %d", len(gotUser.Memberships), len(user.ClientUserList))
	} else {
		for i, m := range user.ClientUserList {
			if g := gotUser.Memberships[i]; g.ClientID != m.ClientID || g.ClientUserID != m.ID {
				t.Errorf("membership %d decoded as %+v, recorded client %q clientUser %q", i, g, m.ClientID, m.ID)
			}
		}
	}

	gotProject, err := client.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	if gotProject.ID != project.ID || gotProject.Name != project.Name ||
		gotProject.Status != project.Status || gotProject.ClientID != project.ClientID {
		t.Errorf("project decoded as id %q name %q status %q client %q, recorded id %q name %q status %q client %q",
			gotProject.ID, gotProject.Name, gotProject.Status, gotProject.ClientID,
			project.ID, project.Name, project.Status, project.ClientID)
	}

	client.BindOrg(project.ClientID)
	searched, err := client.ListServices(ctx, project.ID)
	if err != nil {
		t.Fatalf("service search: %v", err)
	}
	checkServices(t, "service search", searched, search.Items, project.ID)

	listed, err := client.ListServicesDirect(ctx, project.ID)
	if err != nil {
		t.Fatalf("project services: %v", err)
	}
	checkServices(t, "project services", listed, direct.List, project.ID)

	gotService, err := client.GetService(ctx, service.ID)
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	checkService(t, "service by id", *gotService, service)

	for _, route := range routes {
		if !route.served {
			t.Errorf("recording %s (%s %s) was read by none of the client's reads", route.file, route.method, route.path)
		}
	}
}
