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
	"slices"
	"sort"
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
	// syncStatus, createBranchStatus and closeStatus override the
	// merge-upstream, branch-create and pull-request close responses.
	syncStatus         int
	createBranchStatus int
	closeStatus        int
	// posters is pull number → the login that opened it; a pull request
	// missing here was opened by its head repository's owner.
	posters  map[int]string
	nextPull int
}

func newFakeGitea(t *testing.T) *fakeGitea {
	t.Helper()
	return &fakeGitea{
		t:       t,
		files:   map[string]map[string]map[string]string{},
		forks:   map[string]string{},
		pulls:   map[string]int{},
		posters: map[int]string{},
	}
}

// branchHead is the fake's commit id for a branch: a hash of its tree, so a
// branch cut from another starts at the same id and a commit moves it.
func (f *fakeGitea) branchHead(repo, branch string) string {
	files, ok := f.files[repo][branch]
	if !ok {
		return ""
	}
	return treeHead(files)
}

func treeHead(files map[string]string) string {
	keys := make([]string, 0, len(files))
	for path := range files {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, path := range keys {
		b.WriteString(path + "\x00" + gitBlobSHA(files[path]) + "\n")
	}
	return gitBlobSHA(b.String())
}

// resolve reads a ref of repo as Gitea does: a branch name, or a commit id
// one of its branches is at.
func (f *fakeGitea) resolve(repo, ref string) (map[string]string, bool) {
	if files, ok := f.files[repo][ref]; ok {
		return files, true
	}
	for _, files := range f.files[repo] {
		if treeHead(files) == ref {
			return files, true
		}
	}
	return nil, false
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

	if f.serveProposal(r, path, write) {
		return
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
		files, ok := f.resolve(repo, branch)
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
				poster, _, _ := strings.Cut(repo, "/")
				if login, ok := f.posters[number]; ok {
					poster = login
				}
				open = append(open, map[string]any{
					"number": number, "state": "open",
					"user": map[string]any{"login": poster},
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

// serveProposal answers what a recipe proposal needs beyond the contents
// API: branch reads and creates, the fork sync, and a pull-request close.
func (f *fakeGitea) serveProposal(r *http.Request, path string, write func(int, any)) bool {
	switch {
	// GET /repos/{o}/{r}/branches/{branch}
	case r.Method == http.MethodGet && strings.Contains(path, "/branches/"):
		repo, branch, _ := strings.Cut(strings.TrimPrefix(path, "repos/"), "/branches/")
		head := f.branchHead(repo, branch)
		if head == "" {
			write(http.StatusNotFound, map[string]string{"message": "branch does not exist"})
			return true
		}
		write(http.StatusOK, map[string]any{"name": branch, "commit": map[string]any{"id": head}})

	// POST /repos/{o}/{r}/branches
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/branches"):
		repo := strings.TrimSuffix(strings.TrimPrefix(path, "repos/"), "/branches")
		var body struct {
			New string `json:"new_branch_name"` //nolint:tagliatelle // Gitea's wire schema
			Old string `json:"old_ref_name"`    //nolint:tagliatelle // Gitea's wire schema
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if f.createBranchStatus != 0 {
			write(f.createBranchStatus, map[string]string{"message": "forced"})
			return true
		}
		if _, exists := f.files[repo][body.New]; exists {
			write(http.StatusConflict, map[string]string{"message": "branch already exists"})
			return true
		}
		files, ok := f.resolve(repo, body.Old)
		if !ok {
			write(http.StatusNotFound, map[string]string{"message": "old ref does not exist"})
			return true
		}
		f.seed(repo, body.New, maps2(files))
		write(http.StatusCreated, map[string]any{"name": body.New, "commit": map[string]any{"id": treeHead(files)}})

	// POST /repos/{o}/{r}/merge-upstream
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/merge-upstream"):
		fork := strings.TrimSuffix(strings.TrimPrefix(path, "repos/"), "/merge-upstream")
		var body struct {
			Branch string `json:"branch"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if f.syncStatus != 0 {
			write(f.syncStatus, map[string]string{"message": "forced"})
			return true
		}
		upstream, isFork := f.forks[fork]
		if !isFork {
			write(http.StatusBadRequest, map[string]string{"message": "not a fork"})
			return true
		}
		f.seed(fork, body.Branch, maps2(f.files[upstream][body.Branch]))
		write(http.StatusOK, map[string]string{"merge_type": "fast-forward"})

	// PATCH /repos/{o}/{r}/pulls/{n}
	case r.Method == http.MethodPatch && strings.Contains(path, "/pulls/"):
		_, number, _ := strings.Cut(path, "/pulls/")
		var body struct {
			State string `json:"state"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if f.closeStatus != 0 {
			write(f.closeStatus, map[string]string{"message": "forced"})
			return true
		}
		for key, open := range f.pulls {
			if fmt.Sprint(open) == number && body.State == "closed" {
				delete(f.pulls, key)
			}
		}
		write(http.StatusCreated, map[string]any{"number": number, "state": body.State})

	default:
		return false
	}
	return true
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

// A proposal diffs against the upstream's main as it is NOW, so the reader
// takes main's tip and the tree AT that tip — one commit, never a tree that
// moved between two reads.
func TestReadGiteaBranchFiles_TipAndItsTree(t *testing.T) {
	t.Parallel()
	mainFiles := map[string]string{"README.md": "# acme\n", "4 — Small Production/import.yaml": "project:\n"}
	tests := []struct {
		name      string
		branch    string
		wantHead  string
		wantPaths []string
	}{
		{name: "main is there — its tip and every path", branch: "main", wantHead: treeHead(mainFiles), wantPaths: []string{"4 — Small Production/import.yaml", "README.md"}},
		{name: "no such branch — nothing, and not an error", branch: "develop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGitea(t)
			fake.seed("acme/group", "main", maps2(mainFiles))
			server := httptest.NewServer(fake)
			defer server.Close()

			got, err := ReadGiteaBranchFiles(context.Background(), server.Client(), server.URL, "tok", "acme/group", tt.branch)
			if err != nil {
				t.Fatalf("ReadGiteaBranchFiles: %v", err)
			}
			if got.Head != tt.wantHead {
				t.Errorf("Head = %q, want %q", got.Head, tt.wantHead)
			}
			if strings.Join(got.Paths, "|") != strings.Join(tt.wantPaths, "|") {
				t.Errorf("Paths = %v, want %v", got.Paths, tt.wantPaths)
			}
			if tt.wantHead != "" && fake.callsTo(http.MethodGet, "/git/trees/"+tt.wantHead) != 1 {
				t.Errorf("the tree was not read at the tip it reported: %v", fake.calls)
			}
		})
	}
}

func TestReadGiteaBranchFiles_AFailedReadIsAnError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := ReadGiteaBranchFiles(context.Background(), srv.Client(), srv.URL, "tok", "acme/group", "main"); err == nil {
		t.Fatal("a 500 on the branch read must be an error, not an empty main")
	}
}

func TestGiteaRecipeBranch_NamedAfterTheCommitItIsCutFrom(t *testing.T) {
	t.Parallel()
	tests := []struct{ head, want string }{
		{"3f2a9c1b7d4e5f60718293a4b5c6d7e8f9012345", "recipe/3f2a9c1b7d4e"},
		{"  3f2a9c1b7d4e5f60718293a4b5c6d7e8f9012345 ", "recipe/3f2a9c1b7d4e"},
		{"abc", "recipe/abc"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := GiteaRecipeBranch(tt.head); got != tt.want {
			t.Errorf("GiteaRecipeBranch(%q) = %q, want %q", tt.head, got, tt.want)
		}
	}
}

// A proposal branch starts at the upstream's main, whatever the fork held:
// an old fork's main is behind the group's (a person merged since), and a
// branch cut from it would carry that difference into the pull request.
func TestEnsureGiteaProposalBranch_CutAtTheUpstreamsTip(t *testing.T) {
	t.Parallel()
	upstreamMain := map[string]string{"README.md": "# acme\n", "4 — Small Production/import.yaml": "hand-written\n"}
	staleMain := map[string]string{"README.md": "# acme\n"}
	tests := []struct {
		name               string
		forkMain           map[string]string
		branchThere        bool
		syncStatus         int
		createBranchStatus int
		wantCreated        bool
		wantErr            string
		wantSyncs          int
		wantCreates        int
	}{
		{name: "already cut — no write at all", forkMain: upstreamMain, branchThere: true},
		{name: "a fork at main — synced as a no-op, then cut", forkMain: upstreamMain, wantCreated: true, wantSyncs: 1, wantCreates: 1},
		{name: "a fork behind main — synced first, then cut at main's tip", forkMain: staleMain, wantCreated: true, wantSyncs: 1, wantCreates: 1},
		{name: "the sync is refused — reported, nothing cut", forkMain: staleMain, syncStatus: http.StatusInternalServerError, wantErr: "status 500", wantSyncs: 1},
		{name: "a concurrent pass cut it first — read as there", forkMain: upstreamMain, createBranchStatus: http.StatusConflict, wantSyncs: 1, wantCreates: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGitea(t)
			fake.syncStatus = tt.syncStatus
			fake.createBranchStatus = tt.createBranchStatus
			fake.seed("acme/group", "main", maps2(upstreamMain))
			fake.forks["bot/group"] = "acme/group"
			fake.seed("bot/group", "main", maps2(tt.forkMain))
			at := treeHead(upstreamMain)
			branch := GiteaRecipeBranch(at)
			if tt.branchThere {
				fake.seed("bot/group", branch, maps2(upstreamMain))
			}
			server := httptest.NewServer(fake)
			defer server.Close()

			created, err := EnsureGiteaProposalBranch(context.Background(), server.Client(), server.URL, "tok",
				"bot/group", branch, "main", at)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("EnsureGiteaProposalBranch: %v", err)
			}
			if created != tt.wantCreated {
				t.Errorf("created = %v, want %v", created, tt.wantCreated)
			}
			if n := fake.callsTo(http.MethodPost, "/merge-upstream"); n != tt.wantSyncs {
				t.Errorf("merge-upstream POSTs = %d, want %d", n, tt.wantSyncs)
			}
			if n := fake.callsTo(http.MethodPost, "/bot/group/branches"); n != tt.wantCreates {
				t.Errorf("branch POSTs = %d, want %d", n, tt.wantCreates)
			}
			if tt.wantCreated && fake.branchHead("bot/group", branch) != at {
				t.Errorf("the branch starts at %q, want the upstream's tip %q", fake.branchHead("bot/group", branch), at)
			}
		})
	}
}

// A proposal a Mate's bot no longer stands behind is closed, and only its:
// one cut from an older main, or one whose files main now carries. A person's
// pull request, or another Mate's, is never touched.
func TestCloseGiteaPullRequests_OnlyThePostersOthers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		open        map[string]int
		posters     map[int]string
		keep        string
		closeStatus int
		wantClosed  []int
		wantOpen    []int
		wantErr     string
	}{
		{
			name:       "the bot's other proposals closed, the kept one and everyone else's left",
			open:       map[string]int{"bot/group:mate/bot": 3, "bot/group:recipe/aaa": 4, "bot/group:recipe/bbb": 5, "other/group:mate/other": 6, "acme/group:fix-readme": 7},
			posters:    map[int]string{7: "alice"},
			keep:       "recipe/bbb",
			wantClosed: []int{3, 4}, wantOpen: []int{5, 6, 7},
		},
		{
			name:       "nothing kept — every open proposal of the bot closed",
			open:       map[string]int{"bot/group:mate/bot": 3, "other/group:mate/other": 6},
			wantClosed: []int{3}, wantOpen: []int{6},
		},
		{
			name:     "none of the bot's open — nothing closed",
			open:     map[string]int{"other/group:mate/other": 6},
			wantOpen: []int{6},
		},
		{
			name:        "a refused close — reported",
			open:        map[string]int{"bot/group:mate/bot": 3},
			closeStatus: http.StatusForbidden,
			wantErr:     "status 403", wantOpen: []int{3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeGitea(t)
			fake.closeStatus = tt.closeStatus
			fake.seed("acme/group", "main", map[string]string{})
			maps.Copy(fake.pulls, tt.open)
			maps.Copy(fake.posters, tt.posters)
			server := httptest.NewServer(fake)
			defer server.Close()

			closed, err := CloseGiteaPullRequests(context.Background(), server.Client(), server.URL, "tok",
				"acme/group", "bot", "main", tt.keep)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("CloseGiteaPullRequests: %v", err)
			}
			if !slices.Equal(closed, tt.wantClosed) {
				t.Errorf("closed = %v, want %v", closed, tt.wantClosed)
			}
			open := make([]int, 0, len(fake.pulls))
			for _, number := range fake.pulls {
				open = append(open, number)
			}
			sort.Ints(open)
			if !slices.Equal(open, tt.wantOpen) {
				t.Errorf("still open = %v, want %v", open, tt.wantOpen)
			}
		})
	}
}
