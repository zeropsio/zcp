// Tests for: tools/gitea_recipe_reconcile.go — A2, the recipe in the group
// repo: the tiers main lacks, proposed from a branch cut at main's tip
// (guide 2.2). Fakes only: a Gitea on httptest and the platform mock.
package tools

import (
	"context"
	"crypto/sha1" //nolint:gosec // git's object hash; identity, not a security primitive
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// fakeGroupGitea is the group repo's half of Gitea: an identity, the group
// repo and the bot's fork of it with their branches, and the pull requests on
// the group repo. A commit id is a hash of its branch's tree, so a branch cut
// from main starts at main's id and a commit moves it.
type fakeGroupGitea struct {
	// compareUnanswered makes Gitea not answer the compare.
	compareUnanswered bool
	// branches is repo → branch → path → body.
	branches map[string]map[string]map[string]string
	// pulls is every pull request on acme/group, by number; nextPull is the
	// number the next one takes.
	pulls    map[int]*fakeGroupPull
	nextPull int

	forkPosts   int
	syncs       int
	branchPosts int
	commits     int
	pullPosts   int
	// lastCommitPaths names what the most recent commit carried.
	lastCommitPaths []string
}

type fakeGroupPull struct {
	number           int
	poster           string
	headRepo, branch string
	open             bool
}

const (
	fakeGroupRepo = "acme/group"
	fakeBotFork   = "mate-p1/group"
)

func newFakeGroupGitea() *fakeGroupGitea {
	return &fakeGroupGitea{
		branches: map[string]map[string]map[string]string{
			fakeGroupRepo: {"main": {"README.md": "# acme\n"}},
		},
		pulls:    map[int]*fakeGroupPull{},
		nextPull: 11,
	}
}

// withMain replaces the group repo's main — what the group's people merged.
func (f *fakeGroupGitea) withMain(files map[string]string) *fakeGroupGitea {
	f.branches[fakeGroupRepo]["main"] = maps.Clone(files)
	return f
}

// withFork gives the bot a fork whose main is fork and whose branches are the
// given ones — the state an earlier zcp left.
func (f *fakeGroupGitea) withFork(forkMain map[string]string, branches map[string]map[string]string) *fakeGroupGitea {
	f.branches[fakeBotFork] = map[string]map[string]string{"main": maps.Clone(forkMain)}
	for name, files := range branches {
		f.branches[fakeBotFork][name] = maps.Clone(files)
	}
	return f
}

// withOpenPull records an open pull request on the group repo.
func (f *fakeGroupGitea) withOpenPull(number int, poster, headRepo, branch string) {
	f.pulls[number] = &fakeGroupPull{number: number, poster: poster, headRepo: headRepo, branch: branch, open: true}
}

// merge lands a pull request the way the broker does: its branch's files on
// main, the request closed.
func (f *fakeGroupGitea) merge(number int) {
	pr := f.pulls[number]
	main := f.branches[fakeGroupRepo]["main"]
	maps.Copy(main, f.branches[pr.headRepo][pr.branch])
	pr.open = false
}

// openPulls lists the numbers still open, ascending.
func (f *fakeGroupGitea) openPulls() []int {
	var open []int
	for number, pr := range f.pulls {
		if pr.open {
			open = append(open, number)
		}
	}
	slices.Sort(open)
	return open
}

// proposal is the branch of the one open pull request the bot has.
func (f *fakeGroupGitea) proposal(t *testing.T) (string, map[string]string) {
	t.Helper()
	var found *fakeGroupPull
	for _, pr := range f.pulls {
		if pr.open && pr.poster == "mate-p1" {
			if found != nil {
				t.Fatalf("the bot has two open proposals: #%d and #%d", found.number, pr.number)
			}
			found = pr
		}
	}
	if found == nil {
		t.Fatal("the bot has no open proposal")
	}
	return found.branch, f.branches[found.headRepo][found.branch]
}

func fakeGroupHead(files map[string]string) string {
	keys := slices.Sorted(maps.Keys(files))
	var b strings.Builder
	for _, path := range keys {
		b.WriteString(path + "\x00" + giteaBlobSHAForTest(files[path]) + "\n")
	}
	return giteaBlobSHAForTest(b.String())
}

// resolve reads a ref as Gitea does: a branch, or a commit a branch is at.
func (f *fakeGroupGitea) resolve(repo, ref string) (map[string]string, bool) {
	if files, ok := f.branches[repo][ref]; ok {
		return files, true
	}
	for _, files := range f.branches[repo] {
		if fakeGroupHead(files) == ref {
			return files, true
		}
	}
	return nil, false
}

func (f *fakeGroupGitea) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token "+giteaBotToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		write := func(status int, body any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(body)
		}
		for _, serve := range []func(*http.Request, string, func(int, any)) bool{
			f.serveRepos, f.serveRefs, f.serveContents, f.servePulls,
		} {
			if serve(r, path, write) {
				return
			}
		}
		write(http.StatusNotFound, map[string]string{"message": "unhandled " + path})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// serveRepos answers the identity, the broker, and the fork's existence and
// creation.
func (f *fakeGroupGitea) serveRepos(r *http.Request, path string, write func(int, any)) bool {
	switch {
	case path == "user":
		write(http.StatusOK, map[string]any{"login": "mate-p1", "id": 7})
	case r.Method == http.MethodPost && path == "mate/repository":
		write(http.StatusOK, map[string]any{
			"fullName": "acme/appdev", "cloneUrl": "https://gitea.example/acme/appdev.git", "defaultBranch": "main"})
	case r.Method == http.MethodGet && path == "repos/"+fakeBotFork:
		if _, forked := f.branches[fakeBotFork]; !forked {
			write(http.StatusNotFound, map[string]string{"message": "not found"})
			return true
		}
		write(http.StatusOK, map[string]any{"full_name": fakeBotFork, "default_branch": "main"})
	case r.Method == http.MethodPost && path == "repos/"+fakeGroupRepo+"/forks":
		f.forkPosts++
		f.branches[fakeBotFork] = map[string]map[string]string{"main": maps.Clone(f.branches[fakeGroupRepo]["main"])}
		write(http.StatusAccepted, map[string]any{"full_name": fakeBotFork, "default_branch": "main"})
	case r.Method == http.MethodPost && path == "repos/"+fakeBotFork+"/merge-upstream":
		f.syncs++
		var body struct {
			Branch string `json:"branch"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.branches[fakeBotFork][body.Branch] = maps.Clone(f.branches[fakeGroupRepo][body.Branch])
		write(http.StatusOK, map[string]string{"merge_type": "fast-forward"})
	default:
		return false
	}
	return true
}

// serveRefs answers branch reads and creates, and tree reads by branch or
// commit.
func (f *fakeGroupGitea) serveRefs(r *http.Request, path string, write func(int, any)) bool {
	switch {
	case r.Method == http.MethodGet && strings.Contains(path, "/branches/"):
		repo, branch, _ := strings.Cut(strings.TrimPrefix(path, "repos/"), "/branches/")
		files, ok := f.branches[repo][branch]
		if !ok {
			write(http.StatusNotFound, map[string]string{"message": "branch does not exist"})
			return true
		}
		write(http.StatusOK, map[string]any{"name": branch, "commit": map[string]any{"id": fakeGroupHead(files)}})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/branches"):
		repo := strings.TrimSuffix(strings.TrimPrefix(path, "repos/"), "/branches")
		f.branchPosts++
		var body struct {
			New string `json:"new_branch_name"` //nolint:tagliatelle // Gitea's wire schema
			Old string `json:"old_ref_name"`    //nolint:tagliatelle // Gitea's wire schema
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, exists := f.branches[repo][body.New]; exists {
			write(http.StatusConflict, map[string]string{"message": "branch already exists"})
			return true
		}
		files, ok := f.resolve(repo, body.Old)
		if !ok {
			write(http.StatusNotFound, map[string]string{"message": "old ref does not exist"})
			return true
		}
		f.branches[repo][body.New] = maps.Clone(files)
		write(http.StatusCreated, map[string]any{"name": body.New})
	case r.Method == http.MethodGet && strings.Contains(path, "/git/trees/"):
		repo, ref, _ := strings.Cut(strings.TrimPrefix(path, "repos/"), "/git/trees/")
		files, ok := f.resolve(repo, ref)
		if !ok {
			write(http.StatusNotFound, map[string]string{"message": "branch does not exist"})
			return true
		}
		tree := []map[string]any{}
		for p, body := range files {
			tree = append(tree, map[string]any{"path": p, "type": "blob", "sha": giteaBlobSHAForTest(body)})
		}
		write(http.StatusOK, map[string]any{"tree": tree})
	default:
		return false
	}
	return true
}

// serveContents answers the multi-file commit.
func (f *fakeGroupGitea) serveContents(r *http.Request, path string, write func(int, any)) bool {
	if r.Method != http.MethodPost || !strings.HasSuffix(path, "/contents") {
		return false
	}
	repo := strings.TrimSuffix(strings.TrimPrefix(path, "repos/"), "/contents")
	var body struct {
		Branch    string `json:"branch"`
		NewBranch string `json:"new_branch"` //nolint:tagliatelle // Gitea's wire schema
		Files     []struct {
			Operation string `json:"operation"`
			Path      string `json:"path"`
			Content   string `json:"content"`
		} `json:"files"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	target := body.Branch
	if body.NewBranch != "" {
		f.branches[repo][body.NewBranch] = maps.Clone(f.branches[repo][body.Branch])
		target = body.NewBranch
	}
	f.commits++
	f.lastCommitPaths = nil
	for _, file := range body.Files {
		raw, _ := base64.StdEncoding.DecodeString(file.Content)
		f.branches[repo][target][file.Path] = string(raw)
		f.lastCommitPaths = append(f.lastCommitPaths, file.Path)
	}
	write(http.StatusCreated, map[string]any{"commit": map[string]string{"sha": "abc123"}})
	return true
}

// servePulls answers the compare and the group repo's pull requests: the
// open list, a create, a close.
func (f *fakeGroupGitea) servePulls(r *http.Request, path string, write func(int, any)) bool {
	switch {
	case strings.Contains(path, "/compare/"):
		if f.compareUnanswered {
			write(http.StatusNotFound, map[string]string{"message": "no compare"})
			return true
		}
		_, spec, _ := strings.Cut(path, "/compare/")
		baseRef, head, _ := strings.Cut(spec, "...")
		owner, branch, _ := strings.Cut(head, ":")
		base, headFiles := f.branches[fakeGroupRepo][baseRef], f.branches[owner+"/group"][branch]
		ahead := 0
		for p, body := range headFiles {
			if base[p] != body {
				ahead = 1
			}
		}
		write(http.StatusOK, map[string]any{"total_commits": ahead, "commits": []any{}})
	case r.Method == http.MethodPatch && strings.Contains(path, "/pulls/"):
		_, number, _ := strings.Cut(path, "/pulls/")
		var body struct {
			State string `json:"state"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, pr := range f.pulls {
			if fmt.Sprint(pr.number) == number && body.State == "closed" {
				pr.open = false
			}
		}
		write(http.StatusCreated, map[string]any{"number": number, "state": body.State})
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/pulls"):
		open := []map[string]any{}
		for _, number := range f.openPulls() {
			pr := f.pulls[number]
			open = append(open, map[string]any{
				"number": pr.number, "state": "open",
				"user": map[string]any{"login": pr.poster},
				"head": map[string]any{"ref": pr.branch, "repo": map[string]any{"full_name": pr.headRepo}},
				"base": map[string]any{"ref": "main"},
			})
		}
		write(http.StatusOK, open)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/pulls"):
		var body struct {
			Head string `json:"head"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		owner, branch, _ := strings.Cut(body.Head, ":")
		f.pullPosts++
		number := f.nextPull
		f.nextPull++
		f.pulls[number] = &fakeGroupPull{number: number, poster: "mate-p1", headRepo: owner + "/group", branch: branch, open: true}
		write(http.StatusCreated, map[string]any{"number": number})
	default:
		return false
	}
	return true
}

// writeGiteaWiredPairMeta seeds a pair that already HAS its repository (A1
// ran) — the state A2 starts from.
func writeGiteaWiredPairMeta(t *testing.T, stateDir string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-09-16",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://gitea.example/acme/appdev.git",
		PrimarySetupName: "api",
		// Both halves deployed: the group tiers build the stage half's setup,
		// and a stage setup nothing records is withheld rather than guessed.
		StageSetupName: "prod",
		Gitea: &workflow.GiteaRepoRef{
			FullName: "acme/appdev", Branch: "mate/mate-p1", DefaultBranch: "main",
		},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

func recipeReconcileClient() *platform.Mock {
	return platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "acme-mate-1", Status: "ACTIVE"}).
		WithServicesDirect([]platform.ServiceStack{
			{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
			{ID: "svc-db", Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql:single@18", ServiceStackTypeCategoryName: "USER"}},
		})
}

// TestReconcileGiteaGroupRecipe_Table is A2's hook table: no Gitea yet, a
// first export.
func TestReconcileGiteaGroupRecipe_Table(t *testing.T) {
	tests := []struct {
		name          string
		noWiring      bool
		noRepoYet     bool
		wantReport    []string
		wantForkPosts int
		wantCommits   int
		wantPullPosts int
	}{
		{
			name:          "first export — forked, committed, a pull request opened",
			wantReport:    []string{"recipe", "acme/group", "#11"},
			wantForkPosts: 1, wantCommits: 1, wantPullPosts: 1,
		},
		{
			name:     "the variables have not landed — nothing touched",
			noWiring: true,
		},
		{
			name:      "no pair has a repository yet — A1 has not run, so there is nothing to export",
			noRepoYet: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			if tc.noRepoYet {
				writeGiteaPairMeta(t, stateDir)
			} else {
				writeGiteaWiredPairMeta(t, stateDir)
			}
			fake := newFakeGroupGitea()
			srv := fake.start(t)

			env := map[string]string{"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken}
			if tc.noWiring {
				env = map[string]string{}
			}
			report := reconcileGiteaGroupRecipe(
				context.Background(), recipeReconcileClient(), srv.Client(),
				runtime.Info{InContainer: true, ProjectID: "p1"},
				stateDir, writeLiveEnvFile(t, env),
			)
			for _, want := range tc.wantReport {
				if !strings.Contains(report, want) {
					t.Errorf("report %q is missing %q", report, want)
				}
			}
			if len(tc.wantReport) == 0 && report != "" {
				t.Errorf("report = %q, want nothing to say", report)
			}
			if fake.forkPosts != tc.wantForkPosts {
				t.Errorf("fork POSTs = %d, want %d", fake.forkPosts, tc.wantForkPosts)
			}
			if fake.commits != tc.wantCommits {
				t.Errorf("commits = %d, want %d", fake.commits, tc.wantCommits)
			}
			if fake.pullPosts != tc.wantPullPosts {
				t.Errorf("pull-request POSTs = %d, want %d", fake.pullPosts, tc.wantPullPosts)
			}
			assertNoTokenOnDisk(t, stateDir)
		})
	}
}

// An open proposal follows the project until it lands: an identical export
// makes no commit, and a changed one lands on the SAME pull request — its
// files are still ones main lacks, so the request still only adds.
func TestReconcileGiteaGroupRecipe_IdempotentThenUpdates(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaWiredPairMeta(t, stateDir)
	fake := newFakeGroupGitea()
	srv := fake.start(t)
	envPath := writeLiveEnvFile(t, map[string]string{
		"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
	})
	rt := runtime.Info{InContainer: true, ProjectID: "p1"}
	ctx := context.Background()

	if report := reconcileGiteaGroupRecipe(ctx, recipeReconcileClient(), srv.Client(), rt, stateDir, envPath); report == "" {
		t.Fatal("the first pass said nothing")
	}
	if fake.commits != 1 || fake.pullPosts != 1 {
		t.Fatalf("first pass: commits=%d pulls=%d, want 1/1", fake.commits, fake.pullPosts)
	}

	// Same project, same export: nothing to commit, and no second pull request.
	reconcileGiteaGroupRecipe(ctx, recipeReconcileClient(), srv.Client(), rt, stateDir, envPath)
	if fake.commits != 1 {
		t.Errorf("an identical export committed again: commits=%d", fake.commits)
	}
	if fake.pullPosts != 1 || fake.forkPosts != 1 || fake.branchPosts != 1 {
		t.Errorf("a second pass re-opened/re-forked/re-cut: pulls=%d forks=%d branches=%d", fake.pullPosts, fake.forkPosts, fake.branchPosts)
	}

	// A new service appears — the branch takes another commit, on the same
	// pull request.
	changed := recipeReconcileClient().WithServicesDirect([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-db", Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql:single@18", ServiceStackTypeCategoryName: "USER"}},
		{ID: "svc-cache", Name: "cache", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey:single@7", ServiceStackTypeCategoryName: "USER"}},
	})
	report := reconcileGiteaGroupRecipe(ctx, changed, srv.Client(), rt, stateDir, envPath)
	if fake.commits != 2 {
		t.Errorf("a changed export did not commit: commits=%d", fake.commits)
	}
	if fake.pullPosts != 1 {
		t.Errorf("a changed export opened a SECOND pull request: pulls=%d", fake.pullPosts)
	}
	if !strings.Contains(report, "#11") {
		t.Errorf("report %q does not name the pull request it updated", report)
	}
	for _, path := range fake.lastCommitPaths {
		if !strings.HasSuffix(path, "import.yaml") && !strings.HasSuffix(path, "README.md") {
			t.Errorf("the update commit touched %q, which is not the recipe's", path)
		}
	}
	_, proposal := fake.proposal(t)
	assertOnlyAdds(t, fake.branches[fakeGroupRepo]["main"], proposal)
	// zcp never writes the group's environments.yaml — the app does.
	if _, wrote := proposal["environments.yaml"]; wrote {
		t.Error("zcp wrote environments.yaml into the group repo")
	}
	assertNoTokenOnDisk(t, stateDir)
}

// assertOnlyAdds fails unless branch is main plus files main does not have:
// the only kind of pull request the group's broker lands by itself, and the
// only kind that cannot rewrite a tier the group already has.
func assertOnlyAdds(t *testing.T, main, branch map[string]string) {
	t.Helper()
	for path, body := range main {
		got, ok := branch[path]
		switch {
		case !ok:
			t.Errorf("the proposal removes %q, which main carries", path)
		case got != body:
			t.Errorf("the proposal modifies %q, which main carries", path)
		}
	}
}

// giteaBlobSHAForTest is git's object hash, mirrored here so the fake's tree
// answers what the code under test compares against.
func giteaBlobSHAForTest(body string) string {
	sum := sha1.New() //nolint:gosec // git's object hash is SHA-1 by definition
	fmt.Fprintf(sum, "blob %d\x00", len(body))
	sum.Write([]byte(body))
	return hex.EncodeToString(sum.Sum(nil))
}

// handWrittenTiers is a group repo's main after its people wrote the tiers
// themselves — project env, secrets, the production setups — the state the
// medusa group was in when its second Mate's proposal replaced it (2026-09-26).
func handWrittenTiers() map[string]string {
	return map[string]string{
		"README.md":                                   "# acme\n",
		"environments.yaml":                           "environments: []\n",
		"0 — AI Agent/import.yaml":                    "project:\n  name: acme-mate\nservices:\n  - hostname: appdev\n    zeropsSetup: appdev\n",
		"3 — Stage/import.yaml":                       "project:\n  name: acme stage\n  envVariables:\n    APP_ENV: stage\nservices:\n  - hostname: app\n    zeropsSetup: appprod\n",
		"4 — Small Production/import.yaml":            "project:\n  name: acme production\n  envVariables:\n    APP_ENV: production\nservices:\n  - hostname: app\n    zeropsSetup: appprod\n    envSecrets:\n      APP_KEY: <@generateRandomString(<32>)>\n",
		"5 — Highly-available Production/import.yaml": "project:\n  name: acme ha\nservices:\n  - hostname: app\n    zeropsSetup: appprod\n",
	}
}

func without(files map[string]string, dir string) map[string]string {
	out := maps.Clone(files)
	for path := range out {
		if strings.HasPrefix(path, dir+"/") {
			delete(out, path)
		}
	}
	return out
}

// staleRecipe is what an earlier zcp left on the bot's fork: the whole recipe
// as that Mate composed it, on the branch every pass reused.
func staleRecipe() map[string]string {
	return map[string]string{
		"README.md":                        "# acme\n\n- **AI Agent** …\n",
		"0 — AI Agent/import.yaml":         "project:\n  name: acme-mate-2\n",
		"3 — Stage/import.yaml":            "project:\n  name: acme stage\nservices:\n  - hostname: mailpit\n",
		"4 — Small Production/import.yaml": "project:\n  name: acme production\nservices:\n  - hostname: app\n    zeropsSetup: appdev\n",
	}
}

// A Mate proposes only what the group repo's main lacks, a tier at a time: a
// tier on main is the group's, hand-written or merged earlier, and a second
// Mate's proposal replaced the medusa group's hand-written tiers — and the
// broker merged it (2026-09-26). The first recipe of a group still lands
// whole; a group that has every tier gets nothing, not even a fork.
func TestReconcileGiteaGroupRecipe_ProposesOnlyWhatMainLacks(t *testing.T) {
	stageFiles := []string{"3 — Stage/README.md", "3 — Stage/import.yaml"}
	tests := []struct {
		name  string
		setup func(*fakeGroupGitea)
		// wantProposed is what the proposal adds to main, sorted; nil for no
		// proposal at all.
		wantProposed  []string
		wantForkPosts int
		wantPullPosts int
		wantOnMain    bool
		wantClosed    []int
		wantOpen      []int
		wantReport    []string
	}{
		{
			name:  "the broker's seed — the whole recipe, and the seed's README stays",
			setup: func(*fakeGroupGitea) {},
			wantProposed: []string{
				"0 — AI Agent/README.md", "0 — AI Agent/import.yaml",
				"3 — Stage/README.md", "3 — Stage/import.yaml",
				"4 — Small Production/README.md", "4 — Small Production/import.yaml",
			},
			wantForkPosts: 1, wantPullPosts: 1, wantOpen: []int{11},
			wantReport: []string{"#11", "acme/group", "3 — Stage"},
		},
		{
			name:       "hand-written tiers on main — nothing proposed, nothing forked, nothing said",
			setup:      func(f *fakeGroupGitea) { f.withMain(handWrittenTiers()) },
			wantOnMain: true,
		},
		{
			name:          "main lacks only Stage — only Stage is proposed",
			setup:         func(f *fakeGroupGitea) { f.withMain(without(handWrittenTiers(), "3 — Stage")) },
			wantProposed:  stageFiles,
			wantForkPosts: 1, wantPullPosts: 1, wantOpen: []int{11},
			wantReport: []string{"#11", "3 — Stage"},
		},
		{
			name: "an old proposal's branch on the fork — the new one starts at main, the old request is closed",
			setup: func(f *fakeGroupGitea) {
				f.withMain(without(handWrittenTiers(), "3 — Stage")).
					withFork(map[string]string{"README.md": "# acme\n"}, map[string]map[string]string{"mate/mate-p1": staleRecipe()}).
					withOpenPull(9, "mate-p1", fakeBotFork, "mate/mate-p1")
			},
			wantProposed:  stageFiles,
			wantPullPosts: 1, wantClosed: []int{9}, wantOpen: []int{11},
			wantReport: []string{"#11", "#9"},
		},
		{
			name: "every tier on main and an old proposal still open — closed, nothing new",
			setup: func(f *fakeGroupGitea) {
				f.withMain(handWrittenTiers()).
					withFork(map[string]string{"README.md": "# acme\n"}, map[string]map[string]string{"mate/mate-p1": staleRecipe()}).
					withOpenPull(9, "mate-p1", fakeBotFork, "mate/mate-p1")
			},
			wantOnMain: true, wantClosed: []int{9},
			wantReport: []string{"already carries every tier", "#9"},
		},
		{
			name: "a person's pull request on the group repo is theirs — left open",
			setup: func(f *fakeGroupGitea) {
				f.withMain(handWrittenTiers()).withOpenPull(8, "alice", fakeGroupRepo, "tune-production")
			},
			wantOnMain: true, wantOpen: []int{8},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			writeGiteaWiredPairMeta(t, stateDir)
			fake := newFakeGroupGitea()
			tt.setup(fake)
			mainBefore := maps.Clone(fake.branches[fakeGroupRepo]["main"])
			srv := fake.start(t)
			envPath := writeLiveEnvFile(t, map[string]string{
				"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
			})

			outcome := giteaGroupRecipeOutcome(context.Background(), recipeReconcileClient(), srv.Client(),
				runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir, envPath)

			if outcome.Blocked != "" {
				t.Fatalf("blocked: %s", outcome.Blocked)
			}
			if outcome.OnMain != tt.wantOnMain {
				t.Errorf("OnMain = %v, want %v (line %q)", outcome.OnMain, tt.wantOnMain, outcome.Line)
			}
			if !slices.Equal(outcome.Closed, tt.wantClosed) {
				t.Errorf("closed = %v, want %v", outcome.Closed, tt.wantClosed)
			}
			if got := fake.openPulls(); !slices.Equal(got, tt.wantOpen) {
				t.Errorf("open pull requests = %v, want %v", got, tt.wantOpen)
			}
			if fake.forkPosts != tt.wantForkPosts || fake.pullPosts != tt.wantPullPosts {
				t.Errorf("fork/pull POSTs = %d/%d, want %d/%d", fake.forkPosts, fake.pullPosts, tt.wantForkPosts, tt.wantPullPosts)
			}
			if !maps.Equal(fake.branches[fakeGroupRepo]["main"], mainBefore) {
				t.Error("zcp wrote the group repo's main")
			}
			for _, want := range tt.wantReport {
				if !strings.Contains(outcome.Line, want) {
					t.Errorf("line %q is missing %q", outcome.Line, want)
				}
			}
			if len(tt.wantReport) == 0 && outcome.Line != "" {
				t.Errorf("line = %q, want nothing to say", outcome.Line)
			}
			if tt.wantProposed == nil {
				if fake.commits != 0 {
					t.Errorf("commits = %d, want none", fake.commits)
				}
				return
			}
			branch, proposal := fake.proposal(t)
			if want := "recipe/" + fakeGroupHead(mainBefore)[:12]; branch != want || outcome.Branch != want {
				t.Errorf("proposal branch = %q (outcome %q), want %q — cut from main's tip", branch, outcome.Branch, want)
			}
			assertOnlyAdds(t, mainBefore, proposal)
			var added []string
			for path := range proposal {
				if _, onMain := mainBefore[path]; !onMain {
					added = append(added, path)
				}
			}
			slices.Sort(added)
			if !slices.Equal(added, tt.wantProposed) {
				t.Errorf("the proposal adds %v, want %v", added, tt.wantProposed)
			}
		})
	}
}

// Main moves under an open proposal — a person merges a hand-written tier —
// and the proposal would carry a file main now has. The pass cuts a fresh
// branch from the new main with only what it still lacks and closes the old
// request: a branch left from the older main would diff as a modification.
func TestReconcileGiteaGroupRecipe_MainMovedUnderAnOpenProposal_ReplacedFromTheNewMain(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaWiredPairMeta(t, stateDir)
	fake := newFakeGroupGitea()
	srv := fake.start(t)
	envPath := writeLiveEnvFile(t, map[string]string{
		"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
	})
	rt := runtime.Info{InContainer: true, ProjectID: "p1"}
	ctx := context.Background()

	first := giteaGroupRecipeOutcome(ctx, recipeReconcileClient(), srv.Client(), rt, stateDir, envPath)
	if first.PullNumber != 11 || !first.Created {
		t.Fatalf("first pass: %+v", first)
	}
	production := handWrittenTiers()["4 — Small Production/import.yaml"]
	fake.branches[fakeGroupRepo]["main"]["4 — Small Production/import.yaml"] = production
	mainNow := maps.Clone(fake.branches[fakeGroupRepo]["main"])

	second := giteaGroupRecipeOutcome(ctx, recipeReconcileClient(), srv.Client(), rt, stateDir, envPath)
	if !slices.Equal(second.Closed, []int{11}) || second.PullNumber != 12 || !second.Created {
		t.Fatalf("second pass: closed %v, pull #%d created=%v, want #11 closed and #12 opened (line %q)",
			second.Closed, second.PullNumber, second.Created, second.Line)
	}
	if got := fake.openPulls(); !slices.Equal(got, []int{12}) {
		t.Errorf("open pull requests = %v, want [12]", got)
	}
	branch, proposal := fake.proposal(t)
	if branch == first.Branch {
		t.Errorf("the replacement reused %q, the branch cut from the older main", branch)
	}
	assertOnlyAdds(t, mainNow, proposal)
	if proposal["4 — Small Production/import.yaml"] != production {
		t.Error("the replacement carries a production tier of its own over the one a person merged")
	}
	if _, ok := proposal["4 — Small Production/README.md"]; ok {
		t.Error("the replacement adds a README to the production tier the person wrote")
	}
	for _, want := range []string{"#12", "#11"} {
		if !strings.Contains(second.Line, want) {
			t.Errorf("line %q is missing %q", second.Line, want)
		}
	}
}

// The agent-facing surface answers on every pass, including the one that
// changed nothing — being asked is itself a reason to say where the recipe is,
// or that main already has it.
func TestHandleGroupRecipe_Table(t *testing.T) {
	tests := []struct {
		name       string
		main       map[string]string
		noWiring   bool
		twice      bool
		wantErr    bool
		wantPulls  int
		wantFields map[string]string
		wantText   []string
	}{
		{
			name:       "proposed",
			wantPulls:  1,
			wantFields: map[string]string{"groupRepo": "acme/group", "fork": "mate-p1/group"},
			wantText:   []string{"pull request #11", "/acme/group/pulls/11", "recipe/"},
		},
		{
			name:      "asked again, nothing changed — still answers with the pull request",
			twice:     true,
			wantPulls: 1,
			wantText:  []string{"already proposed", "pull request #11", "/acme/group/pulls/11"},
		},
		{
			name:       "main already carries every tier — answers that, and proposes nothing",
			main:       handWrittenTiers(),
			wantFields: map[string]string{"groupRepo": "acme/group"},
			wantText:   []string{"already carries every tier", `"onMain":true`},
		},
		{
			name:     "the variables have not landed",
			noWiring: true,
			wantErr:  true,
			wantText: []string{"was not proposed", "GITEA_URL"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			writeGiteaWiredPairMeta(t, stateDir)
			fake := newFakeGroupGitea()
			if tc.main != nil {
				fake.withMain(tc.main)
			}
			srv := fake.start(t)
			env := map[string]string{"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken}
			if tc.noWiring {
				env = map[string]string{}
			}
			envPath := writeLiveEnvFile(t, env)
			rt := runtime.Info{InContainer: true, ProjectID: "p1"}
			ctx := context.Background()

			if tc.twice {
				if _, _, err := handleGroupRecipe(ctx, recipeReconcileClient(), srv.Client(), rt, stateDir, envPath); err != nil {
					t.Fatalf("first pass: %v", err)
				}
			}
			res, typed, err := handleGroupRecipe(ctx, recipeReconcileClient(), srv.Client(), rt, stateDir, envPath)
			if err != nil {
				t.Fatalf("handleGroupRecipe: %v", err)
			}
			if typed != nil {
				t.Error("a handler's typed output must stay nil — machine state rides in the text")
			}
			if res.IsError != tc.wantErr {
				t.Errorf("IsError = %v, want %v", res.IsError, tc.wantErr)
			}
			text := resultText(t, res)
			for _, want := range tc.wantText {
				if !strings.Contains(text, want) {
					t.Errorf("result is missing %q:\n%s", want, text)
				}
			}
			for key, want := range tc.wantFields {
				if !strings.Contains(text, `"`+key+`": "`+want+`"`) && !strings.Contains(text, `"`+key+`":"`+want+`"`) {
					t.Errorf("result is missing %s=%s:\n%s", key, want, text)
				}
			}
			if fake.pullPosts != tc.wantPulls {
				t.Errorf("pull-request POSTs = %d, want %d across %d passes", fake.pullPosts, tc.wantPulls, 1+boolToInt(tc.twice))
			}
			assertNoTokenOnDisk(t, stateDir)
		})
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// resultText is a tool result's model-facing text — the only surface machine
// state rides on (the typed second return stays nil).
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatal("the result carries no content")
	}
	var b strings.Builder
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// TestReconcileGiteaGroupRecipe_OpensNothingMainAlreadyHas is the owner's run
// of 2026-09-17: the recipe was re-proposed after a stage deploy with nothing
// new — main had it from the request the broker had just merged — and the
// pass still opened a pull request, which Gitea marked empty and the broker
// then failed to merge every three minutes. Once the proposal is merged main
// carries every tier, and the pass proposes and opens nothing; a request a
// person closed without merging leaves main without them, so it is opened
// again, as is one whose compare Gitea does not answer.
func TestReconcileGiteaGroupRecipe_OpensNothingMainAlreadyHas(t *testing.T) {
	tests := []struct {
		name              string
		merged            bool
		compareUnanswered bool
		wantPulls         int
	}{
		{name: "the broker merged it — main carries every tier", merged: true, wantPulls: 1},
		{name: "a person closed it unmerged — main still lacks them", wantPulls: 2},
		{name: "a Gitea that does not answer the compare", compareUnanswered: true, wantPulls: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			writeGiteaWiredPairMeta(t, stateDir)
			fake := newFakeGroupGitea()
			srv := fake.start(t)
			envPath := writeLiveEnvFile(t, map[string]string{
				"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
			})
			rt := runtime.Info{InContainer: true, ProjectID: "p1"}
			ctx := context.Background()

			reconcileGiteaGroupRecipe(ctx, recipeReconcileClient(), srv.Client(), rt, stateDir, envPath)
			if fake.commits != 1 || fake.pullPosts != 1 {
				t.Fatalf("first pass: commits=%d pulls=%d, want 1/1", fake.commits, fake.pullPosts)
			}
			if tt.merged {
				fake.merge(11)
			} else {
				fake.pulls[11].open = false
			}
			fake.compareUnanswered = tt.compareUnanswered

			report := reconcileGiteaGroupRecipe(ctx, recipeReconcileClient(), srv.Client(), rt, stateDir, envPath)
			if fake.commits != 1 {
				t.Errorf("an identical export committed again: commits=%d", fake.commits)
			}
			if fake.pullPosts != tt.wantPulls {
				t.Errorf("pull requests opened = %d, want %d (report %q)", fake.pullPosts, tt.wantPulls, report)
			}
			if tt.merged && report != "" {
				t.Errorf("a merged recipe is nothing to report on a pass, got %q", report)
			}
		})
	}
}
