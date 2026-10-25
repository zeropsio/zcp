package tools

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/cgi" //nolint:gosec // G504: the fake serves git's own http-backend through CGI, in a test
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The lab every HQ delivery test runs in: a fake HQ that obeys its contract
// (@t3tools/shared/hqChanges) — the Mate's API, and git over HTTPS with HQ's
// ref rules, served by git's own http-backend — and a pair whose dev half's
// checkout is a real repository the SSH stand-in runs every command in.

const (
	labCredential = "the-mate-credential"
	labMate       = "p-mate"
	labApp        = "app-1"
)

// fakeHQ is HQ as a Mate meets it. A Mate pushes only to the branch of its
// own open change, only forward; nobody deletes a branch; `main` moves only
// by HQ's merge (merge). Every 503 it answers carries Retry-After.
type fakeHQ struct {
	t        *testing.T
	root     string
	refsFile string
	srv      *httptest.Server
	caFile   string

	mu    sync.Mutex
	appID string
	// appName is the application's name HQ answers, "" for none.
	appName     string
	repos       map[string]bool
	changes     []hq.Change
	attachments map[string][]byte
	// failing is how many of the next requests failOn matches — any, when
	// nil — are answered failStatus with failCode, and failReason when one
	// is set, instead of being served.
	failing    int
	failStatus int
	failCode   string
	failReason string
	failOn     func(*http.Request) bool
	// down: HQ does not answer at all — every connection is dropped.
	down bool
	// reposless: HQ from before the Mate's state named its repositories.
	reposless bool
	// untitled: HQ from before the Mate's state named its changes' titles.
	untitled bool
	calls    []string
	// credential is the one Mate credential HQ takes now: enrolling again
	// revokes the one before.
	credential string
	// unnamedBranches: HQ from before changes were bound to a branch they
	// name — it binds a change to the Mate's own branch whatever is asked,
	// and answers it without naming its branch.
	unnamedBranches bool
	// origins are the origins connected to the application's repositories,
	// by name; a repository without one is kept by HQ alone.
	origins map[string]hq.RepoOrigin
	// handed are the changes a person handed the lab's Mate from a Mate that
	// is gone, by repo#number.
	handed map[string]bool
	// perBranch: HQ keeps a Mate an open change per branch it names, beside
	// its one default change opened naming none, at most labOpenCap open in a
	// repository; its state says so (openPerBranch), lists every open change
	// and each one's standing. Off, it is today's HQ: one open change per
	// repository, answered to any opener.
	perBranch bool
	// described is the head each change was described at, by repo#number: a
	// change is ready while its branch is still there.
	described map[string]string
}

// labOpenCap is how many open changes a Mate holds in a repository on an HQ
// that keeps one per branch.
const labOpenCap = 5

func newFakeHQ(t *testing.T) *fakeHQ {
	t.Helper()
	requireGitForLab(t)
	f := &fakeHQ{
		t: t, root: t.TempDir(), appID: labApp, credential: labCredential,
		repos: map[string]bool{}, attachments: map[string][]byte{}, origins: map[string]hq.RepoOrigin{},
		handed: map[string]bool{}, described: map[string]string{},
	}
	f.refsFile = filepath.Join(t.TempDir(), "open-refs")
	f.writeRefs()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	backend := &cgi.Handler{
		Path: gitPath, Args: []string{"http-backend"}, Root: "/git",
		Env: []string{
			"GIT_PROJECT_ROOT=" + f.root, "GIT_HTTP_EXPORT_ALL=1", "FAKE_HQ_REFS=" + f.refsFile,
			"HOME=" + t.TempDir(), "GIT_CONFIG_NOSYSTEM=1",
		},
		InheritEnv: []string{"PATH"},
	}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.serve(w, r, backend)
	}))
	t.Cleanup(f.srv.Close)
	f.caFile = filepath.Join(t.TempDir(), "hq.pem")
	if err := os.WriteFile(f.caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fakeHQ) serve(w http.ResponseWriter, r *http.Request, backend http.Handler) {
	f.mu.Lock()
	f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	if f.down {
		f.mu.Unlock()
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
		return
	}
	if f.failing > 0 && (f.failOn == nil || f.failOn(r)) {
		f.failing--
		status, refusal := f.failStatus, map[string]string{"code": f.failCode}
		if f.failReason != "" {
			refusal["reason"] = f.failReason
		}
		f.mu.Unlock()
		if status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		writeHQJSON(w, status, refusal)
		return
	}
	credential := f.credential
	f.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/git/") {
		user, password, ok := r.BasicAuth()
		if !ok || user != hq.GitUser || password != credential {
			w.Header().Set("WWW-Authenticate", `Basic realm="HQ"`)
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		app := f.appID
		f.mu.Unlock()
		if !strings.HasPrefix(r.URL.Path, "/git/"+app+"/") || app == "" {
			http.Error(w, "App read access refused", http.StatusForbidden)
			return
		}
		backend.ServeHTTP(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Mate "+credential {
		writeHQJSON(w, http.StatusUnauthorized, map[string]string{"code": "mate_credential_required"})
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.api(w, r)
}

func writeHQJSON(w http.ResponseWriter, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// hqProtectedBranch is a branch HQ keeps from a change: main, a full ref,
// or HQ's mate/ namespace.
var hqProtectedBranch = regexp.MustCompile(`^(?:main$|mate(?:$|/)|refs/)`)

// hqValidRef is HQ's validRef: a ref its git host takes.
func hqValidRef(ref string) bool {
	if strings.ContainsFunc(ref, func(r rune) bool { return r <= 32 || r == 127 || strings.ContainsRune("~^:?*[\\", r) }) ||
		strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.HasSuffix(ref, ".") {
		return false
	}
	parts := strings.Split(ref, "/")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// api answers the Mate's side of the contract; f.mu is held.
func (f *fakeHQ) api(w http.ResponseWriter, r *http.Request) {
	refuse := func(status int, code, reason string) {
		writeHQJSON(w, status, map[string]string{"code": code, "reason": reason})
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/mate/self":
		writeHQJSON(w, http.StatusOK, f.self(r.Context()))
	case f.appID == "":
		refuse(http.StatusForbidden, "forbidden", "not_in_app")
	case r.Method == http.MethodPost && r.URL.Path == "/api/mate/repos":
		var in struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.ensureRepo(r.Context(), f.appID, in.Name)
		writeHQJSON(w, http.StatusOK, hq.Repo{AppID: f.appID, Name: in.Name})
	case r.Method == http.MethodPost && r.URL.Path == "/api/mate/changes":
		var in struct {
			Repo   string `json:"repo"`
			Title  string `json:"title"`
			Branch string `json:"branch"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if !f.repos[f.appID+"/"+in.Repo] {
			refuse(http.StatusNotFound, "repo_not_found", "repo_not_found")
			return
		}
		if open := f.answeredOpen(in.Repo, in.Branch); open != nil {
			answered := *open
			if f.unnamedBranches {
				answered.Branch = ""
			}
			writeHQJSON(w, http.StatusOK, hq.OpenedChange{Change: answered, Created: false})
			return
		}
		number := 1
		for _, c := range f.changes {
			if c.AppID == f.appID && c.Repo == in.Repo {
				number = max(number, c.Number+1)
			}
		}
		// HQ's own checks, in its order and with its answers: a branch is a
		// short name, never main, a full ref or one in HQ's mate/ namespace.
		branch := fmt.Sprintf("refs/heads/mate/%s/%d", labMate, number)
		if f.unnamedBranches {
			in.Branch = ""
		}
		if in.Branch != "" {
			branch = "refs/heads/" + in.Branch
		}
		switch {
		case in.Branch != "" && hqProtectedBranch.MatchString(in.Branch):
			refuse(http.StatusConflict, "conflict", "protected_ref")
			return
		case in.Branch != "" && !hqValidRef(branch):
			refuse(http.StatusBadRequest, "invalid", "invalid_ref")
			return
		case f.heldBranch(in.Repo, branch):
			refuse(http.StatusConflict, "conflict", "branch_taken")
			return
		case f.perBranch && f.openCount(in.Repo) >= labOpenCap:
			refuse(http.StatusConflict, "conflict", "too_many_open")
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)
		change := hq.Change{AppID: f.appID, Repo: in.Repo, Number: number, MateProjectID: labMate, Title: in.Title,
			Branch: branch, State: hq.ChangeOpen, OpenedAt: now, UpdatedAt: now, Mergeability: "unknown"}
		f.changes = append(f.changes, change)
		f.writeRefs()
		answered := change
		if f.unnamedBranches {
			answered.Branch = ""
		}
		writeHQJSON(w, http.StatusOK, hq.OpenedChange{Change: answered, Created: true})
	case len(parts) >= 5 && parts[2] == "changes":
		number, _ := strconv.Atoi(parts[4])
		change := f.find(f.appID, parts[3], number, "")
		switch {
		case change == nil:
			refuse(http.StatusNotFound, "change_not_found", "change_not_found")
		case change.State != hq.ChangeOpen:
			refuse(http.StatusConflict, "conflict", "change_not_open")
		case r.Method == http.MethodPatch && len(parts) == 5:
			var edit hq.ChangeEdit
			_ = json.NewDecoder(r.Body).Decode(&edit)
			if edit.Title != nil {
				change.Title = *edit.Title
			}
			if edit.Body != nil {
				change.Body = *edit.Body
				if head := f.branchHead(r.Context(), change); head != nil {
					f.described[fmt.Sprintf("%s#%d", change.Repo, change.Number)] = *head
				}
			}
			writeHQJSON(w, http.StatusOK, *change)
		case r.Method == http.MethodPost && len(parts) == 6 && parts[5] == "attachments":
			png, _ := io.ReadAll(r.Body)
			if r.Header.Get("Content-Type") != "image/png" || !strings.HasPrefix(string(png), "\x89PNG") {
				refuse(http.StatusBadRequest, "invalid", "not_png")
				return
			}
			id := fmt.Sprintf("att-%d", len(f.attachments)+1)
			f.attachments[id] = png
			writeHQJSON(w, http.StatusOK, hq.Attachment{ID: id,
				Path: fmt.Sprintf("/api/apps/%s/changes/%s/%d/attachments/%s", f.appID, change.Repo, change.Number, id)})
		default:
			refuse(http.StatusNotFound, "not_found", "not_found")
		}
	default:
		refuse(http.StatusNotFound, "not_found", "not_found")
	}
}

// self is GET /api/mate/self: the Mate's latest changes per repository of the
// application it is in, newest first, with their heads as git has them.
func (f *fakeHQ) self(ctx context.Context) map[string]any {
	state := map[string]any{"projectId": labMate, "name": "Ada", "face": "face-1",
		"standupRequestedBy": nil, "closedOff": false, "appId": nil, "changes": []hq.MateChange{}}
	if f.appID == "" {
		return state
	}
	state["appId"] = f.appID
	if f.appName != "" {
		state["appName"] = f.appName
	}
	var mine []hq.MateChange
	for _, c := range f.changes {
		if c.AppID != f.appID || c.MateProjectID != labMate {
			continue
		}
		head := c.Head
		if c.State == hq.ChangeOpen {
			head = f.branchHead(ctx, &c)
		}
		key := fmt.Sprintf("%s#%d", c.Repo, c.Number)
		// HQ stores a change's branch by its short name.
		change := hq.MateChange{Repo: c.Repo, Number: c.Number, State: c.State, Head: head, MergedSha: c.MergedSha, LandedHead: c.LandedHead,
			Branch: strings.TrimPrefix(c.Branch, "refs/heads/"), Handed: f.handed[key]}
		if !f.untitled {
			change.Title = &c.Title
		}
		if f.perBranch {
			ready := head != nil && f.described[key] == *head
			behind := c.Behind
			change.Ready, change.Mergeability, change.Behind, change.UpdatedAt = &ready, c.Mergeability, &behind, c.UpdatedAt
		}
		mine = append(mine, change)
	}
	slices.SortFunc(mine, func(a, b hq.MateChange) int {
		if a.Repo != b.Repo {
			return strings.Compare(a.Repo, b.Repo)
		}
		return b.Number - a.Number
	})
	state["changes"] = f.window(mine)
	if f.perBranch {
		state["openPerBranch"] = true
	}
	// HQ's shape: {name} for a repository HQ alone keeps, {name, origin:
	// {url, mode}} for one with an origin connected, by name.
	repos := []map[string]any{}
	for key := range f.repos {
		app, name, _ := strings.Cut(key, "/")
		if app != f.appID {
			continue
		}
		repo := map[string]any{"name": name}
		if origin, ok := f.origins[name]; ok {
			repo["origin"] = map[string]string{"url": origin.URL, "mode": origin.Mode}
		}
		repos = append(repos, repo)
	}
	slices.SortFunc(repos, func(a, b map[string]any) int { return strings.Compare(a["name"].(string), b["name"].(string)) })
	if !f.reposless {
		state["repos"] = repos
	}
	return state
}

// keepOn connects origin to appdev, as a person connecting it in HQ does.
func (f *fakeHQ) keepOn(origin hq.RepoOrigin) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.origins["appdev"] = origin
}

// keepInHQ disconnects appdev's origin: HQ alone keeps its code.
func (f *fakeHQ) keepInHQ() {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.origins, "appdev")
}

// window is the Mate's changes its state lists, newest first per
// repository: on an HQ that keeps an open change per branch every open one
// and the newest hq.MateChangesPerRepo settled ones; on today's HQ the newest
// hq.MateChangesPerRepo in any state.
func (f *fakeHQ) window(mine []hq.MateChange) []hq.MateChange {
	listed := []hq.MateChange{}
	counted := map[string]int{}
	for _, c := range mine {
		if f.perBranch && c.State == hq.ChangeOpen {
			listed = append(listed, c)
			continue
		}
		if counted[c.Repo] < hq.MateChangesPerRepo {
			counted[c.Repo]++
			listed = append(listed, c)
		}
	}
	return listed
}

// answeredOpen is the open change HQ answers an opener naming branch in repo
// with, nil when it opens one: today's HQ answers the Mate's one open change
// whatever is named; one that keeps an open change per branch answers the
// Mate's change on the branch named, or — none named — its default change.
func (f *fakeHQ) answeredOpen(repo, branch string) *hq.Change {
	if !f.perBranch || f.unnamedBranches {
		return f.findOf(labMate, f.appID, repo)
	}
	want := "refs/heads/" + branch
	for i := range f.changes {
		c := &f.changes[i]
		if c.MateProjectID != labMate || c.AppID != f.appID || c.Repo != repo || c.State != hq.ChangeOpen {
			continue
		}
		if branch != "" && c.Branch == want {
			return c
		}
		if branch == "" && c.Branch == fmt.Sprintf("refs/heads/mate/%s/%d", labMate, c.Number) {
			return c
		}
	}
	return nil
}

// openCount is how many open changes the lab's Mate holds in repo.
func (f *fakeHQ) openCount(repo string) int {
	n := 0
	for _, c := range f.changes {
		if c.MateProjectID == labMate && c.AppID == f.appID && c.Repo == repo && c.State == hq.ChangeOpen {
			n++
		}
	}
	return n
}

// keepOpenPerBranch makes HQ one that keeps an open change per branch.
func (f *fakeHQ) keepOpenPerBranch() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.perBranch = true
}

// findOf is mate's open change in repo of the application, nil for none.
func (f *fakeHQ) findOf(mate, app, repo string) *hq.Change {
	for i := range f.changes {
		if c := &f.changes[i]; c.MateProjectID == mate && c.AppID == app && c.Repo == repo && c.State == hq.ChangeOpen {
			return c
		}
	}
	return nil
}

// heldBranch reports whether an open change in repo is bound to branch.
func (f *fakeHQ) heldBranch(repo, branch string) bool {
	for _, c := range f.changes {
		if c.AppID == f.appID && c.Repo == repo && c.State == hq.ChangeOpen && c.Branch == branch {
			return true
		}
	}
	return false
}

// openForAnotherMate is another Mate's open change in repo, on branch.
func (f *fakeHQ) openForAnotherMate(repo, branch string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC().Format(time.RFC3339)
	f.changes = append(f.changes, hq.Change{AppID: f.appID, Repo: repo, Number: 100 + len(f.changes), MateProjectID: "p-other",
		Title: "Another Mate's work", Branch: "refs/heads/" + branch, State: hq.ChangeOpen, OpenedAt: now, UpdatedAt: now})
	f.writeRefs()
}

// handOver is the open change titled title, on branch, of a Mate that is
// gone, handed to the lab's Mate by a person, as HQ's hand-over does: the
// Mate's own now, on the branch it was on — "" is the gone Mate's default.
// files, when there are any, are the gone Mate's work, pushed there before.
// It answers the change's number.
func (f *fakeHQ) handOver(repo, title, branch string, files map[string]string) int {
	f.t.Helper()
	f.seed(repo)
	f.mu.Lock()
	defer f.mu.Unlock()
	number := 1
	for _, c := range f.changes {
		if c.AppID == f.appID && c.Repo == repo {
			number = max(number, c.Number+1)
		}
	}
	if branch == "" {
		branch = fmt.Sprintf("mate/p-gone/%d", number)
	}
	if files != nil {
		clone := filepath.Join(f.t.TempDir(), "gone")
		f.git(f.t.Context(), "", "clone", "-q", f.repoDir(f.appID, repo), clone)
		for name, body := range files {
			if err := os.WriteFile(filepath.Join(clone, name), []byte(body), 0o600); err != nil {
				f.t.Fatal(err)
			}
		}
		f.git(f.t.Context(), clone, "add", "-A")
		f.git(f.t.Context(), clone, "-c", "user.name=gone", "-c", "user.email=gone@example.invalid", "commit", "-qm", "The gone Mate's work")
		cmd := exec.CommandContext(f.t.Context(), "git", "-C", clone, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "FAKE_HQ_AS_HQ=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			f.t.Fatalf("the gone Mate's push: %v\n%s", err, out)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	f.changes = append(f.changes, hq.Change{AppID: f.appID, Repo: repo, Number: number, MateProjectID: labMate, Title: title,
		Branch: "refs/heads/" + branch, State: hq.ChangeOpen, OpenedAt: now, UpdatedAt: now, Mergeability: "unknown"})
	f.handed[fmt.Sprintf("%s#%d", repo, number)] = true
	f.writeRefs()
	return number
}

// find is a change of the application; number 0 is any, state "" any.
func (f *fakeHQ) find(app, repo string, number int, state string) *hq.Change {
	for i := range f.changes {
		c := &f.changes[i]
		if c.AppID == app && c.Repo == repo && (number == 0 || c.Number == number) && (state == "" || c.State == state) {
			return c
		}
	}
	return nil
}

// ensureRepo makes the bare repository app/name, `main` born with one commit
// of the empty tree, the way HQ makes one.
func (f *fakeHQ) ensureRepo(ctx context.Context, app, name string) {
	if f.repos[app+"/"+name] {
		return
	}
	dir := f.initRepo(ctx, app, name)
	tree := f.git(ctx, dir, "hash-object", "-w", "-t", "tree", "/dev/null")
	seed := f.git(ctx, dir, "-c", "user.name=HQ", "-c", "user.email=hq@hq.invalid", "commit-tree", tree, "-m", "Initial commit")
	f.git(ctx, dir, "update-ref", "refs/heads/main", seed)
}

// initRepo makes the bare repository app/name with no branch yet, keeping
// HQ's ref rules, and answers its directory.
func (f *fakeHQ) initRepo(ctx context.Context, app, name string) string {
	dir := f.repoDir(app, name)
	f.git(ctx, "", "init", "--bare", "-q", "-b", "main", dir)
	for _, kv := range [][2]string{{"http.receivepack", "true"}, {"receive.denyNonFastForwards", "true"}, {"receive.denyDeletes", "true"}} {
		f.git(ctx, dir, "config", kv[0], kv[1])
	}
	// A Mate pushes any branch but main, a tag, or another Mate's open
	// change's; HQ itself (FAKE_HQ_AS_HQ) moves anything.
	hook := "#!/bin/sh\n[ -n \"$FAKE_HQ_AS_HQ\" ] && exit 0\nrepo=$(basename \"$(pwd)\")\nwhile read old new ref; do\n" +
		"case $ref in refs/heads/main|refs/tags/*) echo \"$ref is HQ's\" >&2; exit 1;; esac\n" +
		"grep -qxF \"$repo $ref\" \"$FAKE_HQ_REFS\" && { echo \"$ref is another Mate's open change\" >&2; exit 1; }\ndone\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		f.t.Fatal(err)
	}
	f.repos[app+"/"+name] = true
	return dir
}

func (f *fakeHQ) repoDir(app, name string) string { return filepath.Join(f.root, app, name+".git") }

// writeRefs writes the branches the lab's Mate may not push, by repository:
// other Mates' open changes'. f.mu is held, or the fake is not serving yet.
func (f *fakeHQ) writeRefs() {
	var refs []string
	for _, c := range f.changes {
		if c.State == hq.ChangeOpen && c.MateProjectID != labMate {
			refs = append(refs, fmt.Sprintf("%s.git %s", c.Repo, c.Branch))
		}
	}
	if err := os.WriteFile(f.refsFile, []byte(strings.Join(refs, "\n")+"\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// branchHead is where change's branch is, nil while it is not pushed.
func (f *fakeHQ) branchHead(ctx context.Context, change *hq.Change) *string {
	out, err := exec.CommandContext(ctx, "git", "-C", f.repoDir(change.AppID, change.Repo), "rev-parse", "-q", "--verify", //nolint:gosec // G204: a test's own git
		change.Branch).Output()
	if err != nil {
		return nil
	}
	head := strings.TrimSpace(string(out))
	return &head
}

func (f *fakeHQ) git(ctx context.Context, dir string, args ...string) string {
	f.t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// reenroll is the Mate enrolling again, as it does after zcp is redeployed:
// HQ takes credential from now on and revokes the one before, and the
// enrollment on disk holds it.
func (l *hqLab) reenroll(credential string) {
	l.t.Helper()
	l.hq.mu.Lock()
	l.hq.credential = credential
	l.hq.mu.Unlock()
	if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: l.hq.srv.URL, HQProjectID: "hq1", ProjectID: labMate, Credential: credential}); err != nil {
		l.t.Fatal(err)
	}
}

// merge squashes the Mate's change #1 in appdev onto `main`, the way HQ's
// merge does, and records it merged; it answers the squash.
func (f *fakeHQ) merge() string { return f.mergeChange("appdev", 1) }

// mergeChange squashes the Mate's change number in repo onto `main`: one
// commit on `main` holding the merge of `main` and the change's head.
func (f *fakeHQ) mergeChange(repo string, number int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	change := f.find(f.appID, repo, number, hq.ChangeOpen)
	if change == nil {
		f.t.Fatalf("no open change #%d in %s", number, repo)
	}
	head := f.branchHead(f.t.Context(), change)
	if head == nil {
		f.t.Fatalf("no open change #%d with a head in %s", number, repo)
	}
	dir := f.repoDir(f.appID, repo)
	tree := f.git(f.t.Context(), dir, "merge-tree", "--write-tree", "main", *head)
	squash := f.git(f.t.Context(), dir, "-c", "user.name=HQ", "-c", "user.email=hq@hq.invalid",
		"commit-tree", tree, "-p", "main", "-m", fmt.Sprintf("%s (#%d)", change.Title, number))
	f.git(f.t.Context(), dir, "update-ref", "refs/heads/main", squash)
	change.State, change.Head, change.MergedSha, change.LandedHead = hq.ChangeMerged, head, &squash, head
	f.writeRefs()
	return squash
}

// seed makes repo in the Mate's application, the way HQ makes one asked for.
func (f *fakeHQ) seed(repo string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureRepo(f.t.Context(), f.appID, repo)
}

// close closes the Mate's change number in appdev without merging it.
func (f *fakeHQ) close(number int) { f.closeChange("appdev", number) }

// closeChange closes the Mate's change number in repo without merging it.
func (f *fakeHQ) closeChange(repo string, number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.find(f.appID, repo, number, hq.ChangeOpen).State = hq.ChangeClosed
	f.writeRefs()
}

// moveMate is HQ now holding the Mate in another application.
func (f *fakeHQ) moveMate(app string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appID = app
}

// setUntitled makes HQ one from before a change's title came with the Mate's
// state.
func (f *fakeHQ) setUntitled(untitled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.untitled = untitled
}

// callCount is how many times HQ was called as call, "METHOD /path".
func (f *fakeHQ) callCount(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}

// standby makes the next n requests on matches meet a standby — any request,
// API or git, when on is nil.
func (f *fakeHQ) standby(n int, on func(*http.Request) bool) {
	f.answerWith(http.StatusServiceUnavailable, "not_active", n, on)
}

// answerWith makes HQ answer the next n requests on matches status with code
// rather than serve them — any request, API or git, when on is nil.
func (f *fakeHQ) answerWith(status int, code string, n int, on func(*http.Request) bool) {
	f.refuseWith(status, code, "", n, on)
}

// refuseWith makes HQ refuse the next n requests on matches with status,
// code and reason, as HQ's {code, reason} refusals read.
func (f *fakeHQ) refuseWith(status int, code, reason string, n int, on func(*http.Request) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failing, f.failStatus, f.failCode, f.failReason, f.failOn = n, status, code, reason, on
}

// setDown makes HQ stop answering, or answer again.
func (f *fakeHQ) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

// change is the Mate's change number in appdev of the application it was
// first in.
func (f *fakeHQ) change(number int) *hq.Change { return f.changeIn("appdev", number) }

// changeIn is the Mate's change number in repo of the application it was
// first in.
func (f *fakeHQ) changeIn(repo string, number int) *hq.Change {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.find(labApp, repo, number, ""); c != nil {
		copied := *c
		return &copied
	}
	return nil
}

// labSSH runs every command a host is sent in that host's checkout, standing
// in for an SSH session on its container: `/var/www` is the checkout, and
// the session's environment carries the GIT_TOKEN the platform holds for the
// service now — what a fresh session reads.
type labSSH struct {
	t      *testing.T
	mock   *platform.Mock
	dirs   map[string]string
	home   string
	caFile string
	// mu guards commands: status reads a checkout concurrently.
	mu       sync.Mutex
	commands []string
	// pause, when set, is called before each command runs: a test holds a
	// command mid-way with it.
	pause func(command string)
	// sessionToken, when set, is the GIT_TOKEN a session running command
	// sees, given the platform's current one: a test makes a written secret
	// reach sessions late with it.
	sessionToken func(command, current string) string
}

func (s *labSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.mu.Lock()
	s.commands = append(s.commands, command)
	s.mu.Unlock()
	if s.pause != nil {
		s.pause(command)
	}
	dir, ok := s.dirs[hostname]
	if !ok {
		return nil, fmt.Errorf("no host %s in the lab", hostname)
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(command, "/var/www", dir)) //nolint:gosec // G204: the commands under test, in a temp checkout
	cmd.Dir = dir
	token := s.gitToken(ctx, "svc-"+hostname)
	if s.sessionToken != nil {
		token = s.sessionToken(command, token)
	}
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + s.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_SSL_CAINFO=" + s.caFile,
		"GIT_TOKEN=" + token,
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
	}
	return out, nil
}

func (s *labSSH) ExecSSHStdin(ctx context.Context, hostname, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

func (s *labSSH) ExecSSHBackground(ctx context.Context, hostname, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, hostname, command)
}

func (s *labSSH) gitToken(ctx context.Context, serviceID string) string {
	envs, _ := s.mock.GetServiceEnv(ctx, serviceID)
	for _, env := range envs {
		if env.Key == "GIT_TOKEN" {
			return env.Content
		}
	}
	return ""
}

// hqLab is a Mate enrolled with the fake HQ, holding one bootstrapped
// appdev/appstage pair whose dev half's checkout is fresh from `zcp init`.
type hqLab struct {
	t        *testing.T
	hq       *fakeHQ
	stateDir string
	pair     string
	mock     *platform.Mock
	ssh      *labSSH
	rt       runtime.Info
	// describeWaits are the waits between a describe's tries, recorded
	// rather than waited out.
	describeWaits []time.Duration
}

func newHQLab(t *testing.T) *hqLab {
	t.Helper()
	fake := newFakeHQ(t)
	t.Setenv("HOME", t.TempDir())
	// zcp's own git against HQ trusts the fake's certificate.
	t.Setenv("GIT_SSL_CAINFO", fake.caFile)
	if err := hq.SaveEnrollment(hq.EnrollmentPath(), hq.Enrollment{HQ: fake.srv.URL, HQProjectID: "hq1", ProjectID: labMate, Credential: labCredential}); err != nil {
		t.Fatal(err)
	}
	lab := &hqLab{t: t, hq: fake, stateDir: t.TempDir(), pair: t.TempDir(),
		rt: runtime.Info{InContainer: true, ProjectID: labMate, MateEnabled: true}}
	prevAttempts, prevDelay := gitPushSessionAuthAttempts, gitPushSessionAuthDelay
	prevDescribeRetry := describeRetry
	gitPushSessionAuthAttempts, gitPushSessionAuthDelay = 2, 0
	describeRetry = hqRetry{waits: prevDescribeRetry.waits, pause: func(_ context.Context, d time.Duration) error {
		lab.describeWaits = append(lab.describeWaits, d)
		return nil
	}}
	t.Cleanup(func() {
		gitPushSessionAuthAttempts, gitPushSessionAuthDelay = prevAttempts, prevDelay
		describeRetry = prevDescribeRetry
	})
	lab.git("init", "-q", "-b", "main")
	tree := lab.git("mktree")
	lab.git("update-ref", "HEAD", lab.git("-c", "user.name=zcp", "-c", "user.email=zcp@example.invalid", "commit-tree", tree, "-m", "zcp init"))
	lab.mock = platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}, {ID: "svc-appstage", Name: "appstage"}})
	lab.ssh = &labSSH{t: t, mock: lab.mock, dirs: map[string]string{"appdev": lab.pair}, home: t.TempDir(), caFile: fake.caFile}
	if err := workflow.WriteServiceMeta(lab.stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}},
		BootstrapSession: "test", BootstrappedAt: "2026-10-02",
	}); err != nil {
		t.Fatal(err)
	}
	return lab
}

// git runs git in the pair's checkout.
func (l *hqLab) git(args ...string) string {
	l.t.Helper()
	cmd := exec.CommandContext(l.t.Context(), "git", append([]string{"-C", l.pair}, args...)...) //nolint:gosec // G204: a test's own git
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		l.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// write puts files into the pair's working tree.
func (l *hqLab) write(files map[string]string) {
	l.t.Helper()
	for name, body := range files {
		path := filepath.Join(l.pair, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			l.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			l.t.Fatal(err)
		}
	}
}

// wire runs a repository pass, which gives the pair its repository in HQ.
func (l *hqLab) wire() []string {
	l.t.Helper()
	return reconcileHQRepositories(l.t.Context(), l.mock, l.hq.srv.Client(), l.ssh, l.rt, l.stateDir, hqPassBackedOff)
}

// openChange puts the pair's work up as the Mate's change, "Build the app",
// the way plain git
// and HQ's API do, with nothing of zcp's: the working tree committed, the
// change opened in HQ, HEAD pushed to the change's branch. It answers the
// change as HQ opened it — or the open one HQ answered with.
func (l *hqLab) openChange() hq.Change {
	l.t.Helper()
	const title = "Build the app"
	if l.git("status", "--porcelain") != "" {
		l.commit(title)
	}
	hqc, ok := openHQ(l.hq.srv.Client())
	if !ok {
		l.t.Fatal("the lab's Mate is not enrolled")
	}
	opened, err := hqc.OpenChange(l.t.Context(), "appdev", title, "")
	if err != nil {
		l.t.Fatalf("opening the change: %v", err)
	}
	push := "cd /var/www && git push -q origin HEAD:refs/heads/" + hqc.ChangeBranch(opened.Change.Number)
	if out, err := l.ssh.ExecSSH(l.t.Context(), "appdev", push); err != nil {
		l.t.Fatalf("pushing the change's branch: %v\n%s", err, out)
	}
	return opened.Change
}

// meta is the pair's record as it is on disk.
func (l *hqLab) meta() *workflow.ServiceMeta {
	l.t.Helper()
	m, err := workflow.FindServiceMeta(l.stateDir, "appdev")
	if err != nil || m == nil {
		l.t.Fatalf("the pair's record: %v", err)
	}
	return m
}

// remoteHead is the commit a branch of the pair's repository in the
// application it was first in is at, "" when it is not there.
func (l *hqLab) remoteHead(branch string) string {
	out, err := exec.CommandContext(l.t.Context(), "git", "-C", l.hq.repoDir(labApp, "appdev"), //nolint:gosec // G204: a test's own git
		"rev-parse", "-q", "--verify", "refs/heads/"+branch).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func requireGitForLab(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("runs real git against a fake HQ")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// landOnMain puts another Mate's merged work on the repository's main, the
// way an earlier merge of HQ's would have.
func (f *fakeHQ) landOnMain(repo string, files map[string]string) {
	f.t.Helper()
	f.seed(repo)
	clone := filepath.Join(f.t.TempDir(), "other")
	f.git(f.t.Context(), "", "clone", "-q", f.repoDir(f.appID, repo), clone)
	for name, body := range files {
		path := filepath.Join(clone, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			f.t.Fatal(err)
		}
	}
	f.git(f.t.Context(), clone, "add", "-A")
	f.git(f.t.Context(), clone, "-c", "user.name=other", "-c", "user.email=other@example.invalid", "commit", "-qm", "Another Mate's work")
	cmd := exec.CommandContext(f.t.Context(), "git", "-C", clone, "push", "-q", "origin", "main")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "FAKE_HQ_AS_HQ=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("land on main: %v\n%s", err, out)
	}
}

// ledger is every change zcp keeps for the lab's Mate, as on disk.
func (l *hqLab) ledger() []workflow.HQChange {
	l.t.Helper()
	changes, err := workflow.ListHQChanges(l.stateDir)
	if err != nil {
		l.t.Fatalf("the change ledger: %v", err)
	}
	return changes
}

// kept is the ledger's change number of appdev, nil for none.
func (l *hqLab) kept(number int) *workflow.HQChange {
	l.t.Helper()
	c, found, err := workflow.ReadHQChange(l.stateDir, labApp, "appdev", number)
	if err != nil {
		l.t.Fatalf("change #%d in the ledger: %v", number, err)
	}
	if !found {
		return nil
	}
	return &c
}

// record keeps change number of appdev on branch in the ledger, as an
// open-change leaves it.
func (l *hqLab) record(number int, branch string) {
	l.t.Helper()
	if err := workflow.RecordHQChange(l.stateDir, workflow.HQChange{AppID: labApp, Repo: "appdev", Number: number, Branch: branch, Source: workflow.HQChangeFromOpen}); err != nil {
		l.t.Fatal(err)
	}
}

// forget drops change number of appdev from the ledger, as a record never
// written leaves it.
func (l *hqLab) forget(number int) {
	l.t.Helper()
	if err := workflow.ForgetHQChange(l.stateDir, labApp, "appdev", number); err != nil {
		l.t.Fatal(err)
	}
}

// openRecorded opens the pair's work as the Mate's change (openChange) and
// keeps it in the ledger on the branch HQ bound it to; it answers its number.
func (l *hqLab) openRecorded() int {
	l.t.Helper()
	c := l.openChange()
	l.record(c.Number, strings.TrimPrefix(c.Branch, "refs/heads/"))
	return c.Number
}
