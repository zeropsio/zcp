// Tests for: internal/platform/org_reader.go — org mode's read-only door to
// HQ's Chief overview (docs/spec-mate.md §2.11).
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
// and a name here must read as a read: org mode makes no write call, ever —
// every change is the person's own, made from their client.
var orgReaderMethods = []string{
	"ListMembers",
	"ListProjects",
	"ListServices",
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
		if !strings.HasPrefix(name, "List") {
			t.Errorf("OrgReader.%s does not read as a read (List)", name)
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
