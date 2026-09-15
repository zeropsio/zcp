package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeGitHubServer is an offline double for the GitHub REST Git Data API
// endpoints ResetGitHubRepo drives: blob/tree/commit creation, the
// refs/heads/main force-update, and the matching-refs list + delete calls.
type fakeGitHubServer struct {
	t *testing.T

	mu         sync.Mutex
	requests   []string // "METHOD path", recorded in call order
	branches   []string // seeded branch names ("main" included explicitly by the test)
	tags       []string // seeded tag names
	mainPatch  []byte   // last PATCH refs/heads/main request body
	failSuffix string   // non-empty: this path suffix returns failStatus instead of succeeding
	failStatus int
}

func newFakeGitHubServer(t *testing.T) *fakeGitHubServer {
	t.Helper()
	return &fakeGitHubServer{t: t}
}

func (f *fakeGitHubServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeGitHubServer) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	failSuffix, failStatus := f.failSuffix, f.failStatus
	f.mu.Unlock()

	if failSuffix != "" && strings.HasSuffix(r.URL.Path, failSuffix) {
		w.WriteHeader(failStatus)
		fmt.Fprint(w, `{"message":"simulated failure"}`)
		return
	}

	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
		fmt.Fprint(w, `{"sha":"blob-sha-1"}`)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/trees"):
		fmt.Fprint(w, `{"sha":"tree-sha-1"}`)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
		fmt.Fprint(w, `{"sha":"commit-sha-1"}`)
	case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/git/refs/heads/main"):
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.mainPatch = body
		f.mu.Unlock()
		fmt.Fprint(w, `{"ref":"refs/heads/main"}`)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git/matching-refs/heads/"):
		f.writeRefs(w, "heads", f.branches)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git/matching-refs/tags/"):
		f.writeRefs(w, "tags", f.tags)
	case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/git/refs/"):
		w.WriteHeader(http.StatusNoContent)
	default:
		f.t.Errorf("fakeGitHubServer: unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeGitHubServer) writeRefs(w http.ResponseWriter, kind string, names []string) {
	f.mu.Lock()
	refs := make([]map[string]string, 0, len(names))
	for _, n := range names {
		refs = append(refs, map[string]string{"ref": "refs/" + kind + "/" + n})
	}
	f.mu.Unlock()
	if err := json.NewEncoder(w).Encode(refs); err != nil {
		f.t.Errorf("fakeGitHubServer: encode refs response: %v", err)
	}
}

func (f *fakeGitHubServer) requestsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func newTestResetClient(base string, token string) *githubResetClient {
	return &githubResetClient{base: base, owner: "acme", repo: "widgets", token: token, hc: http.DefaultClient}
}

// TestResetGitHubRepo_ForceUpdatesMainToNewBaselineCommit pins the happy
// path: main is force-replaced (sha of the freshly built commit, force:
// true) with the fixed baseline blob/tree/commit sequence.
func TestResetGitHubRepo_ForceUpdatesMainToNewBaselineCommit(t *testing.T) {
	t.Parallel()
	f := newFakeGitHubServer(t)
	f.branches = []string{"main"}
	srv := f.start(t)

	c := newTestResetClient(srv.URL, "test-pat-secret")
	if err := c.reset(context.Background()); err != nil {
		t.Fatalf("reset: %v", err)
	}

	f.mu.Lock()
	body := f.mainPatch
	f.mu.Unlock()
	if body == nil {
		t.Fatal("no PATCH refs/heads/main request was made")
	}
	var patch struct {
		SHA   string `json:"sha"`
		Force bool   `json:"force"`
	}
	if err := json.Unmarshal(body, &patch); err != nil {
		t.Fatalf("decode PATCH refs/heads/main body: %v", err)
	}
	if patch.SHA != "commit-sha-1" || !patch.Force {
		t.Errorf("PATCH refs/heads/main body = %+v, want sha=commit-sha-1 force=true", patch)
	}
}

// TestResetGitHubRepo_DeletesOtherBranchesAndTags_KeepsMain pins the
// cleanup half: every branch except main and every tag is deleted; main
// itself is never targeted by a DELETE call.
func TestResetGitHubRepo_DeletesOtherBranchesAndTags_KeepsMain(t *testing.T) {
	t.Parallel()
	f := newFakeGitHubServer(t)
	f.branches = []string{"main", "feature-x", "wip"}
	f.tags = []string{"v1", "v2"}
	srv := f.start(t)

	c := newTestResetClient(srv.URL, "test-pat-secret")
	if err := c.reset(context.Background()); err != nil {
		t.Fatalf("reset: %v", err)
	}

	reqs := f.requestsSnapshot()
	wantDeleted := []string{
		"DELETE /repos/acme/widgets/git/refs/heads/feature-x",
		"DELETE /repos/acme/widgets/git/refs/heads/wip",
		"DELETE /repos/acme/widgets/git/refs/tags/v1",
		"DELETE /repos/acme/widgets/git/refs/tags/v2",
	}
	for _, want := range wantDeleted {
		if !slices.Contains(reqs, want) {
			t.Errorf("requests %v missing %q", reqs, want)
		}
	}
	for _, r := range reqs {
		if r == "DELETE /repos/acme/widgets/git/refs/heads/main" {
			t.Errorf("main branch was deleted: %v", reqs)
		}
	}
}

// TestResetGitHubRepo_FailureAtEachStep_SurfacesStatusAndEndpoint_NeverToken
// is table-driven over every Git Data API call reset() makes: whichever
// step fails, the returned error names the HTTP status and the endpoint
// path, and never contains the token value (which rides only the request's
// Authorization header).
func TestResetGitHubRepo_FailureAtEachStep_SurfacesStatusAndEndpoint_NeverToken(t *testing.T) {
	t.Parallel()
	const token = "ghp_supersecrettoken123"
	tests := []struct {
		name         string
		failSuffix   string
		wantEndpoint string
	}{
		{"blob create fails", "/git/blobs", "/git/blobs"},
		{"tree create fails", "/git/trees", "/git/trees"},
		{"commit create fails", "/git/commits", "/git/commits"},
		{"main force-update fails", "/git/refs/heads/main", "/git/refs/heads/main"},
		{"list heads fails", "/git/matching-refs/heads/", "/git/matching-refs/heads/"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFakeGitHubServer(t)
			f.branches = []string{"main", "feature-x"}
			f.tags = []string{"v1"}
			f.failSuffix = tc.failSuffix
			f.failStatus = http.StatusInternalServerError
			srv := f.start(t)

			c := newTestResetClient(srv.URL, token)
			err := c.reset(context.Background())
			if err == nil {
				t.Fatal("reset: want error, got nil")
			}
			if !strings.Contains(err.Error(), "500") {
				t.Errorf("error %q missing status code 500", err.Error())
			}
			if !strings.Contains(err.Error(), tc.wantEndpoint) {
				t.Errorf("error %q missing endpoint %q", err.Error(), tc.wantEndpoint)
			}
			if strings.Contains(err.Error(), token) {
				t.Errorf("error %q leaks the token", err.Error())
			}
		})
	}
}

// TestParseGitHubRepo covers the owner/repo extraction ResetGitHubRepo
// relies on: a clean URL, a trailing slash, a ".git" suffix, and the two
// rejected shapes (wrong host, missing repo segment).
func TestParseGitHubRepo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                string
		url                 string
		wantOwner, wantRepo string
		wantErr             bool
	}{
		{name: "clean url", url: "https://github.com/krls2020/eval2", wantOwner: "krls2020", wantRepo: "eval2"},
		{name: "trailing slash", url: "https://github.com/krls2020/eval2/", wantOwner: "krls2020", wantRepo: "eval2"},
		{name: "dot git suffix", url: "https://github.com/krls2020/eval2.git", wantOwner: "krls2020", wantRepo: "eval2"},
		{name: "wrong host", url: "https://gitlab.com/krls2020/eval2", wantErr: true},
		{name: "missing repo segment", url: "https://github.com/krls2020", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			owner, repo, err := parseGitHubRepo(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseGitHubRepo(%q): want error, got nil", tc.url)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGitHubRepo(%q): %v", tc.url, err)
			}
			if owner != tc.wantOwner || repo != tc.wantRepo {
				t.Errorf("parseGitHubRepo(%q) = (%q, %q), want (%q, %q)", tc.url, owner, repo, tc.wantOwner, tc.wantRepo)
			}
		})
	}
}
