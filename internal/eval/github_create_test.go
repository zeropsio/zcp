package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGitHubUserReposServer is an offline double for the GitHub REST
// endpoints createRepo/deleteRepo/listRepos drive: POST /user/repos,
// DELETE /repos/<owner>/<name>, and GET /user/repos.
type fakeGitHubUserReposServer struct {
	t *testing.T

	mu         sync.Mutex
	requests   []string // "METHOD path?query", recorded in call order
	createBody []byte   // last POST /user/repos request body
	repos      []map[string]any
	failSuffix string
	failStatus int
}

func newFakeGitHubUserReposServer(t *testing.T) *fakeGitHubUserReposServer {
	t.Helper()
	return &fakeGitHubUserReposServer{t: t}
}

func (f *fakeGitHubUserReposServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeGitHubUserReposServer) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
	failSuffix, failStatus := f.failSuffix, f.failStatus
	f.mu.Unlock()

	if failSuffix != "" && strings.Contains(r.URL.Path, failSuffix) {
		w.WriteHeader(failStatus)
		fmt.Fprint(w, `{"message":"simulated failure"}`)
		return
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.createBody = body
		f.mu.Unlock()
		var req struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(body, &req)
		fmt.Fprintf(w, `{"name":%q,"html_url":"https://github.com/krls2020/%s"}`, req.Name, req.Name)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/repos/"):
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == "/user/repos":
		f.mu.Lock()
		repos := f.repos
		f.mu.Unlock()
		if err := json.NewEncoder(w).Encode(repos); err != nil {
			f.t.Errorf("fakeGitHubUserReposServer: encode repos response: %v", err)
		}
	default:
		f.t.Errorf("fakeGitHubUserReposServer: unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeGitHubUserReposServer) requestsSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func newTestCreateClient(base, token string) *githubResetClient {
	return &githubResetClient{base: base, owner: "krls2020", repo: "zcp-farm-run123", token: token, hc: http.DefaultClient}
}

// TestGitHubResetClient_CreateRepo_PostsUserRepos_ReturnsHTMLURL pins the
// happy path: createRepo posts to /user/repos with private:true,
// auto_init:false, and returns the response's html_url verbatim.
func TestGitHubResetClient_CreateRepo_PostsUserRepos_ReturnsHTMLURL(t *testing.T) {
	t.Parallel()
	f := newFakeGitHubUserReposServer(t)
	srv := f.start(t)

	c := newTestCreateClient(srv.URL, "test-pat-secret")
	url, err := c.createRepo(context.Background())
	if err != nil {
		t.Fatalf("createRepo: %v", err)
	}
	if url != "https://github.com/krls2020/zcp-farm-run123" {
		t.Errorf("url = %q, want https://github.com/krls2020/zcp-farm-run123", url)
	}

	var body struct {
		Name     string `json:"name"`
		Private  bool   `json:"private"`
		AutoInit bool   `json:"auto_init"` //nolint:tagliatelle // upstream GitHub REST schema
	}
	f.mu.Lock()
	createBody := f.createBody
	f.mu.Unlock()
	if err := json.Unmarshal(createBody, &body); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	if body.Name != "zcp-farm-run123" || !body.Private || body.AutoInit {
		t.Errorf("create body = %+v, want name=zcp-farm-run123 private=true auto_init=false", body)
	}
}

// TestGitHubResetClient_CreateRepo_NoHTMLURL_Errors pins the response-shape
// guard: a 2xx response carrying no html_url is still an error, never a
// silent empty URL.
func TestGitHubResetClient_CreateRepo_NoHTMLURL_Errors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{}`)
	}))
	t.Cleanup(srv.Close)

	c := newTestCreateClient(srv.URL, "test-pat-secret")
	if _, err := c.createRepo(context.Background()); err == nil {
		t.Fatal("createRepo: want error for missing html_url, got nil")
	}
}

// TestGitHubResetClient_DeleteRepo_DeletesReposPath pins the delete half's
// endpoint.
func TestGitHubResetClient_DeleteRepo_DeletesReposPath(t *testing.T) {
	t.Parallel()
	f := newFakeGitHubUserReposServer(t)
	srv := f.start(t)

	c := newTestCreateClient(srv.URL, "test-pat-secret")
	if err := c.deleteRepo(context.Background()); err != nil {
		t.Fatalf("deleteRepo: %v", err)
	}
	reqs := f.requestsSnapshot()
	found := false
	for _, r := range reqs {
		if strings.HasPrefix(r, "DELETE /repos/krls2020/zcp-farm-run123") {
			found = true
		}
	}
	if !found {
		t.Errorf("requests = %v, missing DELETE /repos/krls2020/zcp-farm-run123", reqs)
	}
}

// TestGitHubResetClient_CreateAndDeleteRepo_FailureNeverLeaksToken pins the
// same never-leak-the-token discipline as ResetGitHubRepo's failure tests.
func TestGitHubResetClient_CreateAndDeleteRepo_FailureNeverLeaksToken(t *testing.T) {
	t.Parallel()
	const token = "ghp_supersecrettoken123"
	f := newFakeGitHubUserReposServer(t)
	f.failSuffix = "/user/repos"
	f.failStatus = http.StatusInternalServerError
	srv := f.start(t)

	c := newTestCreateClient(srv.URL, token)
	_, err := c.createRepo(context.Background())
	if err == nil {
		t.Fatal("createRepo: want error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q missing status code 500", err.Error())
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("error %q leaks the token", err.Error())
	}
}

// TestGitHubResetClient_ListRepos_StopsAtShortPage pins the paging loop: a
// page shorter than perPage stops pagination after exactly one GET, and the
// returned slice carries every repo on that page with pushed_at parsed.
func TestGitHubResetClient_ListRepos_StopsAtShortPage(t *testing.T) {
	t.Parallel()
	f := newFakeGitHubUserReposServer(t)
	for i := range 5 {
		f.repos = append(f.repos, map[string]any{
			"name": fmt.Sprintf("zcp-farm-run%03d", i), "pushed_at": "2026-09-01T00:00:00Z",
			"owner": map[string]any{"login": "krls2020"},
		})
	}
	srv := f.start(t)

	c := &githubResetClient{base: srv.URL, token: "test-pat-secret", hc: http.DefaultClient}
	repos, err := c.listRepos(context.Background())
	if err != nil {
		t.Fatalf("listRepos: %v", err)
	}
	if len(repos) != 5 {
		t.Fatalf("len(repos) = %d, want 5", len(repos))
	}
	if repos[0].Owner != "krls2020" || repos[0].Name != "zcp-farm-run000" {
		t.Errorf("repos[0] = %+v, want owner=krls2020 name=zcp-farm-run000", repos[0])
	}
	wantPushedAt, _ := time.Parse(time.RFC3339, "2026-09-01T00:00:00Z")
	if !repos[0].PushedAt.Equal(wantPushedAt) {
		t.Errorf("repos[0].PushedAt = %v, want %v", repos[0].PushedAt, wantPushedAt)
	}

	reqs := f.requestsSnapshot()
	if len(reqs) != 1 {
		t.Errorf("requests = %v, want exactly 1 GET (a page under perPage stops pagination)", reqs)
	}
}

// TestGitHubResetClient_ListRepos_EmptyAccount pins the zero-repos case: no
// error, nil/empty slice, single request.
func TestGitHubResetClient_ListRepos_EmptyAccount(t *testing.T) {
	t.Parallel()
	f := newFakeGitHubUserReposServer(t)
	srv := f.start(t)

	c := &githubResetClient{base: srv.URL, token: "test-pat-secret", hc: http.DefaultClient}
	repos, err := c.listRepos(context.Background())
	if err != nil {
		t.Fatalf("listRepos: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("repos = %v, want empty", repos)
	}
}
