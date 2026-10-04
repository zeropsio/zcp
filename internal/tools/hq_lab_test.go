package tools

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cgi" //nolint:gosec // G504: the fake serves git's own http-backend through CGI, in a test
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	// nil — are answered failStatus with failCode instead of being served.
	failing    int
	failStatus int
	failCode   string
	failOn     func(*http.Request) bool
	// down: HQ does not answer at all — every connection is dropped.
	down bool
	// untitled: HQ from before the Mate's state named its changes' titles.
	untitled bool
	calls    []string
}

func newFakeHQ(t *testing.T) *fakeHQ {
	t.Helper()
	requireGitForLab(t)
	f := &fakeHQ{
		t: t, root: t.TempDir(), appID: labApp,
		repos: map[string]bool{}, attachments: map[string][]byte{},
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
		status, code := f.failStatus, f.failCode
		f.mu.Unlock()
		if status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "1")
		}
		writeHQJSON(w, status, map[string]string{"code": code})
		return
	}
	f.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/git/") {
		user, password, ok := r.BasicAuth()
		if !ok || user != hq.GitUser || password != labCredential {
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
	if r.Header.Get("Authorization") != "Mate "+labCredential {
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
			Repo  string `json:"repo"`
			Title string `json:"title"`
			Tree  string `json:"tree"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if !f.repos[f.appID+"/"+in.Repo] {
			refuse(http.StatusNotFound, "repo_not_found", "repo_not_found")
			return
		}
		if in.Tree != "" && in.Tree == f.git(r.Context(), f.repoDir(f.appID, in.Repo), "rev-parse", "main^{tree}") {
			writeHQJSON(w, http.StatusOK, hq.OpenedChange{Reason: "nothing_to_deliver"})
			return
		}
		if open := f.find(f.appID, in.Repo, 0, hq.ChangeOpen); open != nil {
			writeHQJSON(w, http.StatusOK, hq.OpenedChange{Change: *open, Created: false})
			return
		}
		number := 1
		for _, c := range f.changes {
			if c.AppID == f.appID && c.Repo == in.Repo {
				number = max(number, c.Number+1)
			}
		}
		now := time.Now().UTC().Format(time.RFC3339)
		change := hq.Change{AppID: f.appID, Repo: in.Repo, Number: number, MateProjectID: labMate, Title: in.Title,
			State: hq.ChangeOpen, OpenedAt: now, UpdatedAt: now, Mergeability: "unknown"}
		f.changes = append(f.changes, change)
		f.writeRefs()
		writeHQJSON(w, http.StatusOK, hq.OpenedChange{Change: change, Created: true})
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
		if c.AppID != f.appID {
			continue
		}
		head := c.Head
		if c.State == hq.ChangeOpen {
			head = f.branchHead(ctx, c.AppID, c.Repo, c.Number)
		}
		change := hq.MateChange{Repo: c.Repo, Number: c.Number, State: c.State, Head: head, MergedSha: c.MergedSha, LandedHead: c.LandedHead}
		if !f.untitled {
			change.Title = &c.Title
		}
		mine = append(mine, change)
	}
	slices.SortFunc(mine, func(a, b hq.MateChange) int {
		if a.Repo != b.Repo {
			return strings.Compare(a.Repo, b.Repo)
		}
		return b.Number - a.Number
	})
	state["changes"] = mine
	return state
}

// find is the Mate's change of the application; number 0 is any, state ""
// any.
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
	hook := "#!/bin/sh\nrepo=$(basename \"$(pwd)\")\nwhile read old new ref; do grep -qxF \"$repo $ref\" \"$FAKE_HQ_REFS\" || { echo \"$ref is not an open change of this Mate\" >&2; exit 1; }; done\n"
	if err := os.WriteFile(filepath.Join(dir, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		f.t.Fatal(err)
	}
	f.repos[app+"/"+name] = true
	return dir
}

func (f *fakeHQ) repoDir(app, name string) string { return filepath.Join(f.root, app, name+".git") }

// writeRefs writes the branches a push may move, by repository: the open
// changes'. f.mu is held, or the fake is not serving yet.
func (f *fakeHQ) writeRefs() {
	var refs []string
	for _, c := range f.changes {
		if c.State == hq.ChangeOpen {
			refs = append(refs, fmt.Sprintf("%s.git refs/heads/mate/%s/%d", c.Repo, c.MateProjectID, c.Number))
		}
	}
	if err := os.WriteFile(f.refsFile, []byte(strings.Join(refs, "\n")+"\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeHQ) branchHead(ctx context.Context, app, repo string, number int) *string {
	out, err := exec.CommandContext(ctx, "git", "-C", f.repoDir(app, repo), "rev-parse", "-q", "--verify", //nolint:gosec // G204: a test's own git
		fmt.Sprintf("refs/heads/mate/%s/%d", labMate, number)).Output()
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

// merge squashes the Mate's change #1 in appdev onto `main`, the way HQ's
// merge does, and records it merged; it answers the squash.
func (f *fakeHQ) merge() string { return f.mergeChange("appdev", 1) }

// mergeChange squashes the Mate's change number in repo onto `main`: one
// commit on `main` holding the merge of `main` and the change's head.
func (f *fakeHQ) mergeChange(repo string, number int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	change := f.find(f.appID, repo, number, hq.ChangeOpen)
	head := f.branchHead(f.t.Context(), f.appID, repo, number)
	if change == nil || head == nil {
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

// callTotal is how many times HQ was called at all.
func (f *fakeHQ) callTotal() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
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
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failing, f.failStatus, f.failCode, f.failOn = n, status, code, on
}

// neverAccept makes HQ's address one whose listener never accepts: the
// kernel completes the TCP handshake into its backlog and TLS never starts,
// as for an HQ that drops every packet. HQ does not come back.
func (f *fakeHQ) neverAccept() {
	f.t.Helper()
	address := f.srv.Listener.Addr().String()
	f.srv.Close()
	listener, err := (&net.ListenConfig{}).Listen(f.t.Context(), "tcp", address)
	if err != nil {
		f.t.Fatalf("listen again on %s: %v", address, err)
	}
	f.t.Cleanup(func() { _ = listener.Close() })
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
	t        *testing.T
	mock     *platform.Mock
	dirs     map[string]string
	home     string
	caFile   string
	commands []string
	// pause, when set, is called before each command runs: a test holds a
	// command mid-way with it.
	pause func(command string)
}

func (s *labSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	s.commands = append(s.commands, command)
	if s.pause != nil {
		s.pause(command)
	}
	dir, ok := s.dirs[hostname]
	if !ok {
		return nil, fmt.Errorf("no host %s in the lab", hostname)
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(command, "/var/www", dir)) //nolint:gosec // G204: the commands under test, in a temp checkout
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + s.home, "GIT_CONFIG_NOSYSTEM=1", "GIT_SSL_CAINFO=" + s.caFile,
		"GIT_TOKEN=" + s.gitToken(ctx, "svc-"+hostname),
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, &platform.SSHExecError{Hostname: hostname, Output: string(out), Err: err}
	}
	return out, nil
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
	// waits are the waits between a delivery step's tries, recorded rather
	// than waited out.
	waits []time.Duration
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
		rt: runtime.Info{InContainer: true, ProjectID: labMate}}
	prevAttempts, prevDelay, prevRetry := gitPushSessionAuthAttempts, gitPushSessionAuthDelay, deliveryRetry
	gitPushSessionAuthAttempts, gitPushSessionAuthDelay = 2, 0
	deliveryRetry = hqRetry{waits: prevRetry.waits, pause: func(_ context.Context, d time.Duration) error {
		lab.waits = append(lab.waits, d)
		return nil
	}}
	t.Cleanup(func() {
		gitPushSessionAuthAttempts, gitPushSessionAuthDelay, deliveryRetry = prevAttempts, prevDelay, prevRetry
	})
	lab.git("init", "-q", "-b", "main")
	tree := lab.git("mktree")
	lab.git("update-ref", "HEAD", lab.git("-c", "user.name=zcp", "-c", "user.email=zcp@example.invalid", "commit-tree", tree, "-m", "zcp init"))
	lab.mock = platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}, {ID: "svc-appstage", Name: "appstage"}})
	lab.ssh = &labSSH{t: t, mock: lab.mock, dirs: map[string]string{"appdev": lab.pair}, home: t.TempDir(), caFile: fake.caFile}
	if err := workflow.WriteServiceMeta(lab.stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, StageHostname: "appstage",
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
	return reconcileHQRepositories(l.t.Context(), l.mock, l.hq.srv.Client(), l.ssh, l.rt, l.stateDir)
}

// deliver is a successful deploy of the pair's stage half.
func (l *hqLab) deliver() *hqDelivery {
	l.t.Helper()
	return deliverHQPair(l.t.Context(), l.mock, l.hq.srv.Client(), l.ssh, l.rt, l.stateDir, "appstage")
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

// descends reports whether, in the pair's repository in app, ancestor is an
// ancestor of the branch.
func (l *hqLab) descends(app, ancestor, branch string) bool {
	return exec.CommandContext(l.t.Context(), "git", "-C", l.hq.repoDir(app, "appdev"), //nolint:gosec // G204: a test's own git
		"merge-base", "--is-ancestor", ancestor, "refs/heads/"+branch).Run() == nil
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
	allowed := filepath.Join(f.t.TempDir(), "main-only")
	if err := os.WriteFile(allowed, []byte(repo+".git refs/heads/main\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
	cmd := exec.CommandContext(f.t.Context(), "git", "-C", clone, "push", "-q", "origin", "main")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "FAKE_HQ_REFS="+allowed)
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("land on main: %v\n%s", err, out)
	}
}
