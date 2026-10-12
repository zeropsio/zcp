// Tests for: internal/platform/org_reader.go — org mode through ZCP_ORG_API,
// HQ's Chief proxy, and OrgReads, the reads it forwards (docs/spec-mate.md
// §2.11).
package platform

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// recordedRequest is one request a fake API received.
type recordedRequest struct {
	Method, Path, Auth, Body string
}

// recordingAPI is a fake Zerops API answering every read org mode makes with
// an empty page, recording each request. The token resolution read answers
// with two organizations.
func recordingAPI(t *testing.T) (*httptest.Server, func() []recordedRequest) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []recordedRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, recordedRequest{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: string(body)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/user/info"):
			_, _ = w.Write([]byte(`{"id":"u1","clientUserList":[{"id":"cu1","clientId":"org1"},{"id":"cu2","clientId":"org2"}]}`))
		case strings.HasSuffix(r.URL.Path, "/log"):
			_, _ = w.Write([]byte(`{"url":"GET logs.example/x"}`))
		case strings.HasSuffix(r.URL.Path, "/search"):
			_, _ = w.Write([]byte(`{"items":[],"totalHits":0}`))
		default:
			_, _ = w.Write([]byte(`{"items":[],"envs":[],"list":[],"userDataList":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

// orgReaderCalls drives every read method of OrgReader once, by name, so a
// method added to the reader (pinned by TestOrgReader_ExposesNoWriteMethod)
// is driven here too.
func orgReaderCalls(r *OrgReader) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"ListProjects":  func(ctx context.Context) error { _, err := r.ListProjects(ctx); return err },
		"ListServices":  func(ctx context.Context) error { _, err := r.ListServices(ctx, "p1"); return err },
		"GetProjectEnv": func(ctx context.Context) error { _, err := r.GetProjectEnv(ctx, "p1"); return err },
		"GetServiceEnv": func(ctx context.Context) error { _, err := r.GetServiceEnv(ctx, "s1"); return err },
		"GetProjectLog": func(ctx context.Context) error { _, err := r.GetProjectLog(ctx, "p1"); return err },
		"SearchProcesses": func(ctx context.Context) error {
			_, err := r.SearchProcesses(ctx, "", 10)
			return err
		},
		"SearchAppVersions": func(ctx context.Context) error {
			_, err := r.SearchAppVersions(ctx, "p1", 10)
			return err
		},
		"ListPublicHTTPRoutings": func(ctx context.Context) error {
			_, err := r.ListPublicHTTPRoutings(ctx, "p1")
			return err
		},
		"ListActiveServiceTypeVersions": func(ctx context.Context) error {
			_, err := r.ListActiveServiceTypeVersions(ctx)
			return err
		},
	}
}

func driveOrgReader(t *testing.T, r *OrgReader) {
	t.Helper()
	calls := orgReaderCalls(r)
	want := slices.DeleteFunc(slices.Clone(orgReaderMethods), func(m string) bool { return m == "OrgID" })
	if got := slices.Sorted(maps.Keys(calls)); !slices.Equal(got, want) {
		t.Fatalf("driven methods = %v, want every read method %v", got, want)
	}
	for _, name := range want {
		if err := calls[name](context.Background()); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// matchesOrgRead reports whether a request under the public API root is the
// read pattern names: the same method, and the same path with {id} standing
// for any one segment.
func matchesOrgRead(read OrgRead, method, path string) bool {
	if read.Method != method {
		return false
	}
	want, got := strings.Split(read.Path, "/"), strings.Split(path, "/")
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if want[i] == "{id}" && got[i] != "" {
			continue
		}
		if want[i] != got[i] {
			return false
		}
	}
	return true
}

// TestOrgReads_AreExactlyWhatOrgModeReads drives every OrgReader method
// against a recording API: each request it sends is one of OrgReads, and
// each of OrgReads is sent — the list HQ's Chief proxy allows is neither
// short of what org mode reads nor wider than it.
func TestOrgReads_AreExactlyWhatOrgModeReads(t *testing.T) {
	t.Parallel()
	srv, seen := recordingAPI(t)
	r, err := OpenOrgReader(context.Background(), OrgAccess{Token: "tok", OrgAPI: srv.URL + "/api/chief/zerops", OrgID: "org2"})
	if err != nil {
		t.Fatalf("OpenOrgReader: %v", err)
	}
	driveOrgReader(t, r)

	hit := make(map[OrgRead]bool)
	for _, req := range seen() {
		path := strings.TrimPrefix(req.Path, "/api/chief/zerops")
		i := slices.IndexFunc(OrgReads(), func(read OrgRead) bool { return matchesOrgRead(read, req.Method, path) })
		if i < 0 {
			t.Errorf("org mode read %s %s, which OrgReads does not list", req.Method, path)
			continue
		}
		hit[OrgReads()[i]] = true
	}
	for _, read := range OrgReads() {
		if !hit[read] {
			t.Errorf("OrgReads lists %s %s, which org mode never reads", read.Method, read.Path)
		}
	}
}

// TestOpenOrgReader_ThroughOrgAPI: with ZCP_ORG_API set every read goes to
// it, under its path, with the credential as the bearer; the token's org is
// never resolved through /user/info (the proxy does not serve it), so the
// org must be named; and every search still pins the org's clientId.
func TestOpenOrgReader_ThroughOrgAPI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		orgAPI   string // appended to the fake server's URL
		orgID    string
		wantCode string
	}{
		{name: "a base URL with a path", orgAPI: "/api/chief/zerops", orgID: "org2"},
		{name: "a base URL with a trailing slash", orgAPI: "/api/chief/zerops/", orgID: "org2"},
		{name: "no org named", orgAPI: "/api/chief/zerops", wantCode: ErrOrgAmbiguous},
		{name: "only blanks named", orgAPI: "/api/chief/zerops", orgID: "  ", wantCode: ErrOrgAmbiguous},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv, seen := recordingAPI(t)
			r, err := OpenOrgReader(context.Background(), OrgAccess{Token: "chief-cred", APIHost: "api.region.example", OrgAPI: srv.URL + tt.orgAPI, OrgID: tt.orgID})
			if tt.wantCode != "" {
				var pe *PlatformError
				if !errors.As(err, &pe) || pe.Code != tt.wantCode {
					t.Fatalf("err = %v, want %s", err, tt.wantCode)
				}
				if n := len(seen()); n != 0 {
					t.Fatalf("a refused start sent %d requests", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("OpenOrgReader: %v", err)
			}
			if r.OrgID() != "org2" {
				t.Fatalf("OrgID = %q, want org2", r.OrgID())
			}
			driveOrgReader(t, r)
			reqs := seen()
			if len(reqs) == 0 {
				t.Fatal("no read reached ZCP_ORG_API")
			}
			for _, req := range reqs {
				if !strings.HasPrefix(req.Path, "/api/chief/zerops/") || strings.Contains(req.Path, "/api/rest/public") {
					t.Errorf("%s %s is not under ZCP_ORG_API's path", req.Method, req.Path)
				}
				if strings.HasSuffix(req.Path, "/user/info") {
					t.Errorf("org mode resolved the token's org through %s", req.Path)
				}
				if req.Auth != "Bearer chief-cred" {
					t.Errorf("%s %s carried Authorization %q, want the credential as the bearer", req.Method, req.Path, req.Auth)
				}
				if req.Method == http.MethodPost && !strings.Contains(req.Path, "service-stack-type") &&
					(!strings.Contains(req.Body, `"clientId"`) || !strings.Contains(req.Body, `"org2"`)) {
					t.Errorf("search %s not pinned to org2: %s", req.Path, req.Body)
				}
			}
		})
	}
}

func TestOpenOrgReader_RefusesAnOrgAPIThatIsNoURL(t *testing.T) {
	t.Parallel()
	for _, orgAPI := range []string{"hq.example/api/chief/zerops", "ftp://hq.example/x", "https://"} {
		t.Run(orgAPI, func(t *testing.T) {
			t.Parallel()
			_, err := OpenOrgReader(context.Background(), OrgAccess{Token: "tok", OrgAPI: orgAPI, OrgID: "org2"})
			var pe *PlatformError
			if !errors.As(err, &pe) || pe.Code != ErrInvalidParameter {
				t.Fatalf("err = %v, want %s", err, ErrInvalidParameter)
			}
		})
	}
}
