// Tests for: internal/platform/org_reader.go — org mode's read-only door to
// the Zerops API (docs/spec-mate.md §2.11).
package platform

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// orgReaderMethods is the whole method set org mode may call. A method added
// to OrgReader fails TestOrgReader_ExposesNoWriteMethod until it is named here,
// and a name here must read as a read: org mode makes no write call to the
// Zerops API, ever — every change is the person's own, made from their client.
var orgReaderMethods = []string{
	"GetPerson",
	"GetProjectEnv",
	"GetProjectLog",
	"GetServiceEnv",
	"ListActiveServiceTypeVersions",
	"ListPortRoutings",
	"ListProjects",
	"ListPublicHTTPRoutings",
	"ListServices",
	"OrgID",
	"SearchAppVersions",
	"SearchCostBreakdown",
	"SearchCosts",
	"SearchProcesses",
	"SearchResourceUse",
}

func TestOrgReader_ExposesNoWriteMethod(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeFor[*OrgReader]()
	got := make([]string, 0, typ.NumMethod())
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	if !reflect.DeepEqual(got, orgReaderMethods) {
		t.Fatalf("OrgReader methods = %v\nwant exactly %v — org mode reads; a write belongs to the person's client", got, orgReaderMethods)
	}
	for _, name := range got {
		if name == "OrgID" {
			continue
		}
		if !strings.HasPrefix(name, "Get") && !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Search") {
			t.Errorf("OrgReader.%s does not read as a read (Get/List/Search)", name)
		}
	}
}

func TestReadOnlyTransport_RefusesWrites(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		method  string
		path    string
		allowed bool
	}{
		{name: "a GET reads", method: http.MethodGet, path: "/api/rest/public/project/p1/env", allowed: true},
		{name: "a search POST reads", method: http.MethodPost, path: "/api/rest/public/service-stack/search", allowed: true},
		{name: "a cost search POST reads", method: http.MethodPost, path: "/api/rest/public/transaction-debit/cost-search", allowed: true},
		{name: "a grouped search POST reads", method: http.MethodPost, path: "/api/rest/public/stats-history/group-by-search", allowed: true},
		{name: "a restart PUT is refused", method: http.MethodPut, path: "/api/rest/public/service-stack/s1/restart", allowed: false},
		{name: "an env POST is refused", method: http.MethodPost, path: "/api/rest/public/project/p1/env", allowed: false},
		{name: "an import POST is refused", method: http.MethodPost, path: "/api/rest/public/project/p1/service-stack/import", allowed: false},
		{name: "a DELETE is refused", method: http.MethodDelete, path: "/api/rest/public/service-stack/s1", allowed: false},
		{name: "a PATCH is refused", method: http.MethodPatch, path: "/api/rest/public/project/p1", allowed: false},
		{name: "a POST whose path only contains search is refused", method: http.MethodPost, path: "/api/rest/public/search/p1/restart", allowed: false},
		{name: "a POST to a name merely ending in search is refused", method: http.MethodPost, path: "/api/rest/public/project/p1/research", allowed: false},
		{name: "a POST to a bare -search is refused", method: http.MethodPost, path: "/api/rest/public/project/-search", allowed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sent := false
			rt := readOnlyTransport{base: roundTripFunc(func(*http.Request) (*http.Response, error) {
				sent = true
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			})}
			req, err := http.NewRequestWithContext(context.Background(), tt.method, "https://api.example"+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := rt.RoundTrip(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			if tt.allowed {
				if err != nil || !sent {
					t.Fatalf("allowed request: err=%v sent=%v", err, sent)
				}
				return
			}
			if sent {
				t.Fatal("a write reached the network")
			}
			var pe *PlatformError
			if !errors.As(err, &pe) || pe.Code != ErrOrgWriteRefused {
				t.Fatalf("err = %v, want %s", err, ErrOrgWriteRefused)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResolveOrgID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		reach    []string
		named    string
		want     string
		wantCode string
	}{
		{name: "the one org a token reaches", reach: []string{"o1"}, want: "o1"},
		{name: "a named org the token reaches", reach: []string{"o1", "o2"}, named: "o2", want: "o2"},
		{name: "several orgs and none named", reach: []string{"o1", "o2"}, wantCode: ErrOrgAmbiguous},
		{name: "a named org the token does not reach", reach: []string{"o1"}, named: "o9", wantCode: ErrOrgNotReachable},
		{name: "a token that reaches no org", reach: nil, wantCode: ErrOrgNotReachable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveOrgID(tt.reach, tt.named)
			if tt.wantCode != "" {
				var pe *PlatformError
				if !errors.As(err, &pe) || pe.Code != tt.wantCode {
					t.Fatalf("err = %v, want %s", err, tt.wantCode)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("resolveOrgID = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

// TestOrgReader_EveryCallReadsInsideTheOrg drives every OrgReader method
// against a recording API: each request is a GET or a search, and each search
// is filtered by the org's clientId — a token reaching other organizations
// never reads them through org mode.
func TestOrgReader_EveryCallReadsInsideTheOrg(t *testing.T) {
	t.Parallel()
	srv, seen := recordingAPI(t)
	ctx := context.Background()
	r, err := OpenOrgReader(ctx, OrgAccess{Token: "tok", APIHost: srv.URL, OrgID: "org2"})
	if err != nil {
		t.Fatalf("OpenOrgReader: %v", err)
	}
	if r.OrgID() != "org2" {
		t.Fatalf("OrgID = %q, want org2", r.OrgID())
	}
	driveOrgReader(t, r)
	if _, err := r.ListServices(ctx, ""); err != nil {
		t.Fatalf("ListServices across the org: %v", err)
	}
	for _, req := range seen() {
		switch {
		case req.Method == http.MethodGet:
		case req.Method == http.MethodPost && isSearchPath(req.Path):
			if strings.Contains(req.Path, "service-stack-type") {
				continue
			}
			if !strings.Contains(req.Body, `"clientId"`) || !strings.Contains(req.Body, `"org2"`) || strings.Contains(req.Body, `"org1"`) {
				t.Errorf("search %s not scoped to org2: %s", req.Path, req.Body)
			}
		default:
			t.Errorf("org mode sent a write: %s %s", req.Method, req.Path)
		}
	}
}
