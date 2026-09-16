package ops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/recipe"
)

func TestGroupRepoFullName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"org and repo", "acme/apidev", "acme/group"},
		{"the group repo itself", "acme/group", "acme/group"},
		{"no slash", "apidev", ""},
		{"empty", "", ""},
		{"empty org", "/apidev", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := GroupRepoFullName(tc.in); got != tc.want {
				t.Errorf("GroupRepoFullName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// gitBlobSHA must agree with git itself, or the idempotence compare silently
// re-commits identical bytes on every pass.
func TestGitBlobSHA_AgreesWithGit(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	for _, body := range []string{"", "# group\n", "project:\n  name: acme\n", "0 — AI Agent"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "f")
		if err := writeFile(path, body); err != nil {
			t.Fatalf("write: %v", err)
		}
		out, err := exec.CommandContext(t.Context(), "git", "hash-object", path).Output()
		if err != nil {
			t.Fatalf("git hash-object: %v", err)
		}
		want := strings.TrimSpace(string(out))
		if got := gitBlobSHA(body); got != want {
			t.Errorf("gitBlobSHA(%q) = %s, want %s", body, got, want)
		}
	}
}

// fakeGitea is enough of Gitea's API for the group-repo path: a fork registry,
// one branch per repository holding path→body, and an open pull-request list.
type fakeGitea struct {
	t *testing.T
	// files is repo → branch → path → body.
	files map[string]map[string]map[string]string
	// forks is fork full name → upstream full name.
	forks map[string]string
	// pulls is the open list on the upstream, keyed by "headRepo:headRef".
	pulls map[string]int
	// calls records the method+path of every request, in order.
	calls []string
	// forkStatus overrides the fork POST response (0 = the default 202).
	forkStatus int
	nextPull   int
}

func newFakeGitea(t *testing.T) *fakeGitea {
	t.Helper()
	return &fakeGitea{
		t:     t,
		files: map[string]map[string]map[string]string{},
		forks: map[string]string{},
		pulls: map[string]int{},
	}
}

func (f *fakeGitea) seed(repo, branch string, files map[string]string) {
	if f.files[repo] == nil {
		f.files[repo] = map[string]map[string]string{}
	}
	f.files[repo][branch] = files
}

func (f *fakeGitea) commitPaths(repo, branch string) []string {
	out := make([]string, 0, len(f.files[repo][branch]))
	for path := range f.files[repo][branch] {
		out = append(out, path)
	}
	return out
}

// changeFilesBody is the shape of POST /repos/{o}/{r}/contents.
type changeFilesBody struct {
	Branch    string `json:"branch"`
	NewBranch string `json:"new_branch"` //nolint:tagliatelle // Gitea's wire schema
	Message   string `json:"message"`
	Files     []struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Content   string `json:"content"`
	} `json:"files"`
}

func (f *fakeGitea) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	write := func(status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		encoded, err := json.Marshal(body)
		if err != nil {
			f.t.Fatalf("encode fake response: %v", err)
		}
		_, _ = w.Write(encoded)
	}

	switch {
	// POST /repos/{o}/{r}/forks
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/forks"):
		upstream := strings.TrimSuffix(strings.TrimPrefix(path, "repos/"), "/forks")
		fork := "bot/" + strings.SplitN(upstream, "/", 2)[1]
		if f.forkStatus != 0 {
			write(f.forkStatus, map[string]string{"message": "forced"})
			return
		}
		if _, exists := f.forks[fork]; exists {
			write(http.StatusConflict, map[string]string{"message": "repository is already forked by user"})
			return
		}
		f.forks[fork] = upstream
		f.seed(fork, "main", maps2(f.files[upstream]["main"]))
		write(http.StatusAccepted, map[string]any{"full_name": fork, "default_branch": "main"})

	// GET /repos/{o}/{r}
	case r.Method == http.MethodGet && strings.Count(path, "/") == 2 && strings.HasPrefix(path, "repos/"):
		repo := strings.TrimPrefix(path, "repos/")
		if _, ok := f.files[repo]; !ok {
			write(http.StatusNotFound, map[string]string{"message": "not found"})
			return
		}
		write(http.StatusOK, map[string]any{"full_name": repo, "default_branch": "main"})

	// GET /repos/{o}/{r}/git/trees/{ref}
	case r.Method == http.MethodGet && strings.Contains(path, "/git/trees/"):
		repo, ref, _ := strings.Cut(strings.TrimPrefix(path, "repos/"), "/git/trees/")
		branch := strings.ReplaceAll(ref, "%2F", "/")
		files, ok := f.files[repo][branch]
		if !ok {
			// What Gitea 1.27.2 actually answers for a ref it cannot
			// resolve — 400, not 404 (measured live 2026-09-16 against
			// a fork whose `mate/{bot}` branch did not exist yet).
			write(http.StatusBadRequest, map[string]string{"message": "sha not found [" + branch + "]"})
			return
		}
		tree := []map[string]any{}
		for p, body := range files {
			tree = append(tree, map[string]any{"path": p, "type": "blob", "sha": gitBlobSHA(body)})
		}
		write(http.StatusOK, map[string]any{"tree": tree, "truncated": false})

	// POST /repos/{o}/{r}/contents
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/contents"):
		repo := strings.TrimSuffix(strings.TrimPrefix(path, "repos/"), "/contents")
		var body changeFilesBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			write(http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		target := body.Branch
		if body.NewBranch != "" {
			f.seed(repo, body.NewBranch, maps2(f.files[repo][body.Branch]))
			target = body.NewBranch
		}
		files := f.files[repo][target]
		for _, file := range body.Files {
			_, exists := files[file.Path]
			if (file.Operation == "create") == exists {
				write(http.StatusUnprocessableEntity, map[string]string{
					"message": fmt.Sprintf("operation %q on path %q (exists=%v)", file.Operation, file.Path, exists)})
				return
			}
			files[file.Path] = decodeB64(f.t, file.Content)
		}
		write(http.StatusCreated, map[string]any{"commit": map[string]string{"sha": "deadbeef"}})

	// GET|POST /repos/{o}/{r}/pulls
	case strings.HasSuffix(path, "/pulls"):
		if r.Method == http.MethodGet {
			open := []map[string]any{}
			for key, number := range f.pulls {
				repo, ref, _ := strings.Cut(key, ":")
				open = append(open, map[string]any{
					"number": number, "state": "open",
					"head": map[string]any{"ref": ref, "repo": map[string]any{"full_name": repo}},
					"base": map[string]any{"ref": "main"},
				})
			}
			write(http.StatusOK, open)
			return
		}
		var body struct {
			Head  string `json:"head"`
			Base  string `json:"base"`
			Title string `json:"title"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		owner, ref, _ := strings.Cut(body.Head, ":")
		f.nextPull++
		f.pulls[owner+"/group:"+ref] = f.nextPull
		write(http.StatusCreated, map[string]any{"number": f.nextPull,
			"html_url": "https://gitea.example/acme/group/pulls/" + fmt.Sprint(f.nextPull)})

	default:
		write(http.StatusNotFound, map[string]string{"message": "unhandled " + r.Method + " " + path})
	}
}

func maps2(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

func (f *fakeGitea) callsTo(method, substr string) int {
	n := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, method+" ") && strings.Contains(call, substr) {
			n++
		}
	}
	return n
}

func TestEnsureGiteaFork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		seedFork   bool
		forkStatus int
		wantErr    string
		wantPosts  int
	}{
		{name: "not forked yet — created", wantPosts: 1},
		{name: "already forked — found, no create", seedFork: true, wantPosts: 0},
		{name: "forbidden — reported, never fatal here", forkStatus: http.StatusForbidden, wantErr: "status 403", wantPosts: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGitea(t)
			fake.forkStatus = tc.forkStatus
			fake.seed("acme/group", "main", map[string]string{"README.md": "# acme\n"})
			if tc.seedFork {
				fake.forks["bot/group"] = "acme/group"
				fake.seed("bot/group", "main", map[string]string{"README.md": "# acme\n"})
			}
			server := httptest.NewServer(fake)
			defer server.Close()

			got, err := EnsureGiteaFork(context.Background(), server.Client(), server.URL, "tok", "acme/group", "bot")
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("EnsureGiteaFork: %v", err)
			}
			if got != "bot/group" {
				t.Errorf("fork = %q, want bot/group", got)
			}
			if n := fake.callsTo(http.MethodPost, "/forks"); n != tc.wantPosts {
				t.Errorf("fork POSTs = %d, want %d", n, tc.wantPosts)
			}
		})
	}
}

func recipeFiles(pairs ...string) []recipe.File {
	out := make([]recipe.File, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, recipe.File{Path: pairs[i], Body: pairs[i+1]})
	}
	return out
}

func TestPublishGiteaFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		branchFiles   map[string]string // nil = the branch does not exist yet
		files         []recipe.File
		wantCommitted bool
		wantPosts     int
		wantFinal     map[string]string
	}{
		{
			name:          "no branch yet — one commit branching off the base",
			files:         recipeFiles("README.md", "# acme\n", "0 — AI Agent/import.yaml", "project:\n"),
			wantCommitted: true, wantPosts: 1,
			wantFinal: map[string]string{"README.md": "# acme\n", "0 — AI Agent/import.yaml": "project:\n"},
		},
		{
			name:          "identical export — no commit at all",
			branchFiles:   map[string]string{"README.md": "# acme\n", "0 — AI Agent/import.yaml": "project:\n"},
			files:         recipeFiles("README.md", "# acme\n", "0 — AI Agent/import.yaml", "project:\n"),
			wantCommitted: false, wantPosts: 0,
			wantFinal: map[string]string{"README.md": "# acme\n", "0 — AI Agent/import.yaml": "project:\n"},
		},
		{
			name:          "one file changed — one commit carrying only it",
			branchFiles:   map[string]string{"README.md": "# acme\n", "0 — AI Agent/import.yaml": "project:\n"},
			files:         recipeFiles("README.md", "# acme\n", "0 — AI Agent/import.yaml", "project:\n  name: acme\n"),
			wantCommitted: true, wantPosts: 1,
			wantFinal: map[string]string{"README.md": "# acme\n", "0 — AI Agent/import.yaml": "project:\n  name: acme\n"},
		},
		{
			name:          "a new tier appears — created beside the ones that stayed",
			branchFiles:   map[string]string{"README.md": "# acme\n"},
			files:         recipeFiles("README.md", "# acme\n", "4 — Small Production/import.yaml", "project:\n"),
			wantCommitted: true, wantPosts: 1,
			wantFinal: map[string]string{"README.md": "# acme\n", "4 — Small Production/import.yaml": "project:\n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGitea(t)
			fake.seed("bot/group", "main", map[string]string{"README.md": "# upstream\n"})
			if tc.branchFiles != nil {
				fake.seed("bot/group", "mate/bot", maps2(tc.branchFiles))
			}
			server := httptest.NewServer(fake)
			defer server.Close()

			committed, err := PublishGiteaFiles(context.Background(), server.Client(), server.URL, "tok",
				"bot/group", "mate/bot", "main", "recipe: the group export", tc.files)
			if err != nil {
				t.Fatalf("PublishGiteaFiles: %v", err)
			}
			if committed != tc.wantCommitted {
				t.Errorf("committed = %v, want %v", committed, tc.wantCommitted)
			}
			if n := fake.callsTo(http.MethodPost, "/contents"); n != tc.wantPosts {
				t.Errorf("contents POSTs = %d, want %d (calls: %v)", n, tc.wantPosts, fake.calls)
			}
			got := fake.files["bot/group"]["mate/bot"]
			if len(got) != len(tc.wantFinal) {
				t.Fatalf("branch holds %v, want %v", fake.commitPaths("bot/group", "mate/bot"), tc.wantFinal)
			}
			for path, body := range tc.wantFinal {
				if got[path] != body {
					t.Errorf("%s = %q, want %q", path, got[path], body)
				}
			}
		})
	}
}

// A cross-fork pull request: the create names `{owner}:{branch}`, the open
// list reports the branch alone with the fork beside it — matching on the ref
// without the repository would find a same-name branch on a stranger's fork.
func TestEnsureGiteaPullRequest_CrossFork(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		seed        map[string]int
		wantCreated bool
		wantNumber  int
	}{
		{name: "none open — created", wantCreated: true, wantNumber: 1},
		{name: "already open from this fork — reused", seed: map[string]int{"bot/group:mate/bot": 7}, wantNumber: 7},
		{name: "same branch on another fork — not ours", seed: map[string]int{"other/group:mate/bot": 7}, wantCreated: true, wantNumber: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGitea(t)
			fake.seed("acme/group", "main", map[string]string{})
			maps.Copy(fake.pulls, tc.seed)
			server := httptest.NewServer(fake)
			defer server.Close()

			number, created, err := EnsureGiteaPullRequest(context.Background(), server.Client(), server.URL, "tok",
				"acme/group", "bot/group", "mate/bot", "main", "Mate: the group recipe")
			if err != nil {
				t.Fatalf("EnsureGiteaPullRequest: %v", err)
			}
			if created != tc.wantCreated || number != tc.wantNumber {
				t.Errorf("number/created = %d/%v, want %d/%v", number, created, tc.wantNumber, tc.wantCreated)
			}
		})
	}
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}

func decodeB64(t *testing.T, s string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("file content is not base64: %v", err)
	}
	return string(raw)
}

// TestGiteaTreeBlobs_MissingRefIsNotAnError pins the shape Gitea 1.27.2
// actually answers with when a ref does not exist — 400 "sha not found",
// where every other API says 404. A 400 that says anything else stays an
// error: this must not swallow a malformed request.
func TestGiteaTreeBlobs_MissingRefIsNotAnError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		status    int
		body      string
		wantExist bool
		wantErr   bool
	}{
		{"missing ref", http.StatusBadRequest, `{"message":"sha not found [mate/bot]"}`, false, false},
		{"not found", http.StatusNotFound, `{"message":"not found"}`, false, false},
		{"some other bad request", http.StatusBadRequest, `{"message":"invalid page size"}`, false, true},
		{"a tree", http.StatusOK, `{"tree":[{"path":"README.md","type":"blob","sha":"abc"}],"truncated":false}`, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			blobs, exists, err := giteaTreeBlobs(context.Background(), srv.Client(), srv.URL+"/api/v1", "t", "acme/group", "mate/bot")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if exists != tt.wantExist {
				t.Errorf("branchExists = %v, want %v", exists, tt.wantExist)
			}
			if !tt.wantExist && len(blobs) != 0 {
				t.Errorf("an absent ref must read as an empty tree, got %v", blobs)
			}
		})
	}
}
