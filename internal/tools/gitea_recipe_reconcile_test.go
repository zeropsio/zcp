// Tests for: tools/gitea_recipe_reconcile.go — A2, the recipe in the group
// repo, proposed and kept current (guide 2.2). Fakes only: a Gitea on
// httptest and the platform mock.
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
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// fakeGroupGitea is the group repo's half of Gitea: an identity, a fork
// registry, one branch per repository, and an open pull-request list.
type fakeGroupGitea struct {
	// branches is repo → branch → path → body.
	branches map[string]map[string]map[string]string
	forked   bool
	// pullNumber is 0 until one is opened.
	pullNumber int
	forkPosts  int
	commits    int
	pullPosts  int
	// lastCommitPaths names what the most recent commit carried.
	lastCommitPaths []string
}

func newFakeGroupGitea() *fakeGroupGitea {
	return &fakeGroupGitea{branches: map[string]map[string]map[string]string{
		"acme/group":    {"main": {"README.md": "# acme\n"}},
		"mate-p1/group": {},
	}}
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
		switch {
		case path == "user":
			write(http.StatusOK, map[string]any{"login": "mate-p1", "id": 7})

		case r.Method == http.MethodPost && path == "mate/repository":
			write(http.StatusOK, map[string]any{
				"fullName": "acme/appdev", "cloneUrl": "https://gitea.example/acme/appdev.git", "defaultBranch": "main"})

		case r.Method == http.MethodGet && path == "repos/mate-p1/group":
			if !f.forked {
				write(http.StatusNotFound, map[string]string{"message": "not found"})
				return
			}
			write(http.StatusOK, map[string]any{"full_name": "mate-p1/group", "default_branch": "main"})

		case r.Method == http.MethodPost && path == "repos/acme/group/forks":
			f.forkPosts++
			f.forked = true
			f.branches["mate-p1/group"]["main"] = map[string]string{"README.md": "# acme\n"}
			write(http.StatusAccepted, map[string]any{"full_name": "mate-p1/group", "default_branch": "main"})

		case r.Method == http.MethodGet && strings.Contains(path, "/git/trees/"):
			repo, ref, _ := strings.Cut(strings.TrimPrefix(path, "repos/"), "/git/trees/")
			branch := strings.ReplaceAll(ref, "%2F", "/")
			files, ok := f.branches[repo][branch]
			if !ok {
				write(http.StatusNotFound, map[string]string{"message": "branch does not exist"})
				return
			}
			tree := []map[string]any{}
			for p, body := range files {
				tree = append(tree, map[string]any{"path": p, "type": "blob", "sha": giteaBlobSHAForTest(body)})
			}
			write(http.StatusOK, map[string]any{"tree": tree})

		case r.Method == http.MethodPost && strings.HasSuffix(path, "/contents"):
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
				seed := map[string]string{}
				maps.Copy(seed, f.branches[repo][body.Branch])
				f.branches[repo][body.NewBranch] = seed
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

		case strings.HasSuffix(path, "/pulls"):
			if r.Method == http.MethodGet {
				open := []map[string]any{}
				if f.pullNumber != 0 {
					open = append(open, map[string]any{
						"number": f.pullNumber, "state": "open",
						"head": map[string]any{"ref": "mate/mate-p1", "repo": map[string]any{"full_name": "mate-p1/group"}},
						"base": map[string]any{"ref": "main"},
					})
				}
				write(http.StatusOK, open)
				return
			}
			f.pullPosts++
			f.pullNumber = 11
			write(http.StatusCreated, map[string]any{"number": 11})

		default:
			write(http.StatusNotFound, map[string]string{"message": "unhandled " + path})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
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
// first export, an unchanged pass, and a changed one.
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

// An identical export makes no commit, and a changed one lands on the SAME
// pull request — the two halves of "proposed and kept current".
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
	if fake.pullPosts != 1 || fake.forkPosts != 1 {
		t.Errorf("a second pass re-opened/re-forked: pulls=%d forks=%d", fake.pullPosts, fake.forkPosts)
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
	// zcp never writes the group's environments.yaml — the app does.
	if _, wrote := fake.branches["mate-p1/group"]["mate/mate-p1"]["environments.yaml"]; wrote {
		t.Error("zcp wrote environments.yaml into the group repo")
	}
	assertNoTokenOnDisk(t, stateDir)
}

// giteaBlobSHAForTest is git's object hash, mirrored here so the fake's tree
// answers what the code under test compares against.
func giteaBlobSHAForTest(body string) string {
	sum := sha1.New() //nolint:gosec // git's object hash is SHA-1 by definition
	fmt.Fprintf(sum, "blob %d\x00", len(body))
	sum.Write([]byte(body))
	return hex.EncodeToString(sum.Sum(nil))
}
