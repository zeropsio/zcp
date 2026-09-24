// Tests for: tools/gitea_repo_reconcile.go — A1, git per dev pair as early
// as possible (guide 2.1). Fakes only: a broker and a Gitea on httptest, an
// SSH stub, the platform mock.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

const giteaBotToken = "gitea-bot-token-value"

// fakeGitea stands in for BOTH the broker and the Gitea instance — they are
// two origins in production, but one httptest server can answer both paths
// and the code under test never assumes they are the same host.
type fakeGitea struct {
	repoStatus   int
	repoBody     string
	repoRequests []string
	pullsOpen    string
	pullCreates  int
	// pullTitles is the title of every request created, in order.
	pullTitles []string
	// pullTitle is what the open request is called; pullRetitles is every
	// title it was given afterwards, in order.
	pullTitle    string
	pullRetitles []string
	userStatus   int
	// branchExists is whether the Mate's branch is on the remote — false
	// until the pair's first push, which is the state A1 leaves behind.
	branchExists bool
	// pullState and pullMerged are what a read of ONE request answers, for
	// the pass that asks what became of the request a pair recorded. "open"
	// by default, which is every other test's state.
	pullState  string
	pullMerged bool
	// pullMergeCommit and pullMergeHead are what a merged read answers for
	// merge_commit_sha / head.sha — Gitea's own words for what landed the
	// request, which a delivery needs to absorb a squash losslessly.
	pullMergeCommit string
	pullMergeHead   string
	// pullReads counts those reads, so a settled pair can be shown to ask
	// once per backoff window rather than once per pass.
	pullReads int
}

func newFakeGitea() *fakeGitea {
	return &fakeGitea{
		repoStatus: http.StatusOK,
		repoBody:   `{"fullName":"acme/appdev","cloneUrl":"REPLACED","defaultBranch":"main","created":true}`,
		pullsOpen:  `[]`,
		userStatus: http.StatusOK,
	}
}

func (f *fakeGitea) start(t *testing.T) *httptest.Server {
	t.Helper()
	// TLS because git-push-setup refuses a non-HTTPS remote: a PAT must
	// never travel in cleartext, and the broker's cloneUrl is https in
	// production (gitea-mate docs/vocabulary.md).
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token "+giteaBotToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/api/v1/user":
			w.WriteHeader(f.userStatus)
			_, _ = w.Write([]byte(`{"login":"mate-p1","id":7}`))
		case r.URL.Path == "/mate/repository":
			buf := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(buf)
			f.repoRequests = append(f.repoRequests, string(buf))
			w.WriteHeader(f.repoStatus)
			_, _ = w.Write([]byte(f.repoBody))
		case strings.Contains(r.URL.Path, "/branches/"):
			if !f.branchExists {
				// Gitea 1.27.2 answers a branch it cannot resolve with 404.
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(`{"name":"mate/mate-p1"}`))
		case strings.Contains(r.URL.Path, "/pulls/"):
			if r.Method == http.MethodPatch {
				var edit struct {
					Title string `json:"title"`
				}
				_ = json.NewDecoder(r.Body).Decode(&edit)
				f.pullRetitles = append(f.pullRetitles, edit.Title)
				f.pullTitle = edit.Title
			}
			if r.Method == http.MethodGet {
				f.pullReads++
			}
			state := f.pullState
			if state == "" {
				state = "open"
			}
			body, _ := json.Marshal(map[string]any{
				"number": 9, "state": state, "merged": f.pullMerged, "title": f.pullTitle,
				"merge_commit_sha": f.pullMergeCommit,
				"head":             map[string]any{"sha": f.pullMergeHead},
			})
			_, _ = w.Write(body)
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			if r.Method == http.MethodPost {
				f.pullCreates++
				var created struct {
					Title string `json:"title"`
				}
				_ = json.NewDecoder(r.Body).Decode(&created)
				f.pullTitles = append(f.pullTitles, created.Title)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"number":3,"state":"open"}`))
				return
			}
			_, _ = w.Write([]byte(f.pullsOpen))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	f.repoBody = strings.Replace(f.repoBody, "REPLACED", srv.URL+"/acme/appdev", 1)
	t.Cleanup(srv.Close)
	return srv
}

// writeGiteaPairMeta seeds one bootstrapped, not-yet-remoted pair.
func writeGiteaPairMeta(t *testing.T, stateDir string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-09-16",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// giteaReconcileSSH answers the git-push-setup chain the way a healthy
// container would.
func giteaReconcileSSH() *containerSSHStub {
	return &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "cur_email=$(git config user.email)") {
				return []byte("ZCP_EMAIL_SEEDED\nZCP_NAME_SEEDED\n"), nil
			}
			return []byte("ok"), nil
		},
	}
}

// TestReconcileGiteaRepositories_Table is the hook table A1 asks for: Gitea
// up, the variables not present yet, and the broker refusing — plus the
// invariant that binds them, one repository per pair.
func TestReconcileGiteaRepositories_Table(t *testing.T) {
	tests := []struct {
		name         string
		env          map[string]string
		repoStatus   int
		repoBody     string
		wantReport   []string
		wantRequests int
		wantWired    bool
	}{
		{
			name:         "gitea up",
			repoStatus:   http.StatusOK,
			wantReport:   []string{"repository acme/appdev wired", `"mate/mate-p1"`, "pull request #3"},
			wantRequests: 1,
			wantWired:    true,
		},
		{
			name:       "variables not present yet",
			env:        map[string]string{},
			wantReport: []string{"waiting for Gitea", "GITEA_URL", "MATE_BROKER_URL", "GITEA_TOKEN"},
		},
		{
			name:       "only the token has not landed",
			env:        map[string]string{"GITEA_URL": "x", "MATE_BROKER_URL": "x"},
			wantReport: []string{"waiting for Gitea", "GITEA_TOKEN"},
		},
		{
			name:         "broker refuses: the name is taken",
			repoStatus:   http.StatusConflict,
			repoBody:     `{"error":"taken"}`,
			wantReport:   []string{"refuses a repository named", "appdev"},
			wantRequests: 1,
		},
		{
			name:         "broker refuses: not registered",
			repoStatus:   http.StatusForbidden,
			repoBody:     `{"error":"not_registered"}`,
			wantReport:   []string{"does not know this project yet", "nothing else is blocked"},
			wantRequests: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()
			writeGiteaPairMeta(t, stateDir)

			fake := newFakeGitea()
			if tt.repoStatus != 0 {
				fake.repoStatus = tt.repoStatus
			}
			if tt.repoBody != "" {
				fake.repoBody = tt.repoBody
			}
			srv := fake.start(t)

			env := tt.env
			if env == nil {
				env = map[string]string{
					"GITEA_URL":       srv.URL,
					"MATE_BROKER_URL": srv.URL,
					"GITEA_TOKEN":     giteaBotToken,
				}
			}
			ssh := giteaReconcileSSH()
			client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

			report := reconcileGiteaRepositories(
				context.Background(), client, srv.Client(), ssh,
				runtime.Info{InContainer: true, ProjectID: "p1"},
				stateDir, writeLiveEnvFile(t, env),
			)
			joined := strings.Join(report, " | ")
			for _, want := range tt.wantReport {
				if !strings.Contains(joined, want) {
					t.Errorf("report missing %q: %s", want, joined)
				}
			}
			if len(fake.repoRequests) != tt.wantRequests {
				t.Errorf("broker calls = %d, want %d", len(fake.repoRequests), tt.wantRequests)
			}

			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
			if tt.wantWired {
				if meta == nil || meta.Gitea == nil {
					t.Fatalf("pair should carry its Gitea repository, got %+v", meta)
				}
				if meta.Gitea.Branch != "mate/mate-p1" {
					t.Errorf("branch = %q, want mate/mate-p1 — never main", meta.Gitea.Branch)
				}
				if meta.Gitea.Branch == "main" || meta.Gitea.DefaultBranch != "main" {
					t.Errorf("the Mate's branch must not be the protected default: %+v", meta.Gitea)
				}
			} else if meta != nil && meta.Gitea != nil {
				t.Errorf("a pair the broker did not serve must stay unwired, got %+v", meta.Gitea)
			}

			// Whatever happened, the token is nowhere on disk.
			assertNoTokenOnDisk(t, stateDir)
		})
	}
}

// TestReconcileGiteaRepositories_OneRepositoryPerPair pins the invariant a
// reconcile has to earn: running again asks the broker for nothing.
func TestReconcileGiteaRepositories_OneRepositoryPerPair(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaPairMeta(t, stateDir)
	fake := newFakeGitea()
	srv := fake.start(t)
	envPath := writeLiveEnvFile(t, map[string]string{
		"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
	})
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
	rt := runtime.Info{InContainer: true, ProjectID: "p1"}

	for pass := 1; pass <= 3; pass++ {
		reconcileGiteaRepositories(context.Background(), client, srv.Client(), giteaReconcileSSH(), rt, stateDir, envPath)
	}
	if len(fake.repoRequests) != 1 {
		t.Errorf("broker asked %d times across three passes, want exactly 1", len(fake.repoRequests))
	}
	if fake.pullCreates != 1 {
		t.Errorf("pull request created %d times, want exactly 1", fake.pullCreates)
	}
}

// TestReconcileGiteaRepositories_BacksOff pins that an unproductive pass is
// not repeated immediately: the variables have not landed, and a burst of
// agent tool calls must not become a burst of broker calls.
func TestReconcileGiteaRepositories_BacksOff(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaPairMeta(t, stateDir)
	fake := newFakeGitea()
	srv := fake.start(t)
	envPath := writeLiveEnvFile(t, map[string]string{})
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
	rt := runtime.Info{InContainer: true, ProjectID: "p1"}

	first := reconcileGiteaRepositories(context.Background(), client, srv.Client(), giteaReconcileSSH(), rt, stateDir, envPath)
	if len(first) != 1 {
		t.Fatalf("first pass should report once, got %v", first)
	}
	second := reconcileGiteaRepositories(context.Background(), client, srv.Client(), giteaReconcileSSH(), rt, stateDir, envPath)
	if len(second) != 0 {
		t.Errorf("a pass inside the backoff window must stay silent, got %v", second)
	}
}

func TestGiteaAttemptDue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }

	tests := []struct {
		name  string
		state giteaPairState
		want  bool
	}{
		{name: "never tried", want: true},
		{name: "corrupt timestamp runs", state: giteaPairState{Attempts: 3, LastAttemptAt: "not a time"}, want: true},
		{name: "just tried", state: giteaPairState{Attempts: 1, LastAttemptAt: at(5 * time.Second)}},
		{name: "first backoff elapsed", state: giteaPairState{Attempts: 1, LastAttemptAt: at(time.Minute)}, want: true},
		{name: "second backoff not elapsed", state: giteaPairState{Attempts: 2, LastAttemptAt: at(45 * time.Second)}},
		{name: "capped, elapsed", state: giteaPairState{Attempts: 40, LastAttemptAt: at(11 * time.Minute)}, want: true},
		{name: "capped, not elapsed", state: giteaPairState{Attempts: 40, LastAttemptAt: at(9 * time.Minute)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := giteaAttemptDue(tt.state, now); got != tt.want {
				t.Errorf("giteaAttemptDue(%+v) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

// TestReconcileGiteaRepositories_LocalIsNoOp — ZCP on a developer's machine
// is not a Mate; it has no bot, no broker and no container to push from.
func TestReconcileGiteaRepositories_LocalIsNoOp(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaPairMeta(t, stateDir)
	fake := newFakeGitea()
	srv := fake.start(t)
	envPath := writeLiveEnvFile(t, map[string]string{
		"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
	})
	report := reconcileGiteaRepositories(
		context.Background(), platform.NewMock(), srv.Client(), giteaReconcileSSH(),
		runtime.Info{}, stateDir, envPath,
	)
	if report != nil || len(fake.repoRequests) != 0 {
		t.Errorf("local mode must do nothing, got report=%v requests=%v", report, fake.repoRequests)
	}
}

// TestReconcileGiteaRepositories_LeavesAUserRemoteAlone — a pair already
// pointed at the user's own remote is theirs; ZCP does not move it.
func TestReconcileGiteaRepositories_LeavesAUserRemoteAlone(t *testing.T) {
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        "https://github.com/someone/their-app.git",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-09-16",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	fake := newFakeGitea()
	srv := fake.start(t)
	envPath := writeLiveEnvFile(t, map[string]string{
		"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
	})
	report := reconcileGiteaRepositories(
		context.Background(), platform.NewMock(), srv.Client(), giteaReconcileSSH(),
		runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir, envPath,
	)
	if report != nil || len(fake.repoRequests) != 0 {
		t.Errorf("a user-owned remote must be left alone, got report=%v requests=%v", report, fake.repoRequests)
	}
}

// TestDefaultPushBranch pins A5 at the delivery site: the default is the
// recorded tracked ref (GF-7), then — on a Gitea pair, whose `main` is
// protected — the Mate's own branch, and `main` everywhere else.
func TestDefaultPushBranch(t *testing.T) {
	t.Parallel()

	plain := t.TempDir()
	if err := workflow.WriteServiceMeta(plain, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeSimple,
		BootstrapSession: "t", BootstrappedAt: "2026-09-16",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	if got := resolveTrackedBranch(plain, "appdev", ""); got != "main" {
		t.Errorf("a plain pair defaults to %q, want main", got)
	}
	if got := resolveTrackedBranch(plain, "nobody", ""); got != "main" {
		t.Errorf("an unknown service defaults to %q, want main", got)
	}

	gitea := t.TempDir()
	if err := workflow.WriteServiceMeta(gitea, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeSimple,
		BootstrapSession: "t", BootstrappedAt: "2026-09-16",
		Gitea: &workflow.GiteaRepoRef{FullName: "acme/appdev", Branch: "mate/mate-p1", DefaultBranch: "main"},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	if got := resolveTrackedBranch(gitea, "appdev", ""); got != "mate/mate-p1" {
		t.Errorf("a Gitea pair defaults to %q, want mate/mate-p1", got)
	}

	tracked := t.TempDir()
	if err := workflow.WriteServiceMeta(tracked, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeSimple,
		BootstrapSession: "t", BootstrappedAt: "2026-09-16",
		TrackedRef: "release",
		Gitea:      &workflow.GiteaRepoRef{FullName: "acme/appdev", Branch: "mate/mate-p1", DefaultBranch: "main"},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	if got := resolveTrackedBranch(tracked, "appdev", ""); got != "release" {
		t.Errorf("a recorded tracked ref wins, got %q, want release", got)
	}
	if got := resolveTrackedBranch(tracked, "appdev", "hotfix"); got != "hotfix" {
		t.Errorf("an explicit branch wins, got %q, want hotfix", got)
	}

	// But never the protected base. A pair wired before it had a branch kept
	// `main` as its tracked ref, and every git-push deploy then pushed the
	// Mate's commits straight at protected `main` — rejected non-fast-forward
	// the moment anybody merged, with the agent left to work around it by hand
	// (the owner, 2026-09-18: "it keeps running into this as well").
	base := t.TempDir()
	if err := workflow.WriteServiceMeta(base, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeSimple,
		BootstrapSession: "t", BootstrappedAt: "2026-09-16",
		TrackedRef: "main",
		Gitea:      &workflow.GiteaRepoRef{FullName: "acme/appdev", Branch: "mate/mate-p1", DefaultBranch: "main"},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	for _, asked := range []string{"", "main"} {
		if got := resolveTrackedBranch(base, "appdev", asked); got != "mate/mate-p1" {
			t.Errorf("a wired pair asked for %q pushes to %q, want mate/mate-p1", asked, got)
		}
	}

	// A pair with no Gitea has no other branch to offer, so what it was asked
	// for stands.
	if got := resolveTrackedBranch(plain, "appdev", "main"); got != "main" {
		t.Errorf("a plain pair asked for main pushes to %q, want main", got)
	}
}

// writeLiveEnvFile renders the container's live env store — the file the step
// reads because a running process cannot see a variable written after it
// started.
func writeLiveEnvFile(t *testing.T, env map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env.json")
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal live env: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write live env: %v", err)
	}
	return path
}

// assertNoTokenOnDisk walks everything ZCP wrote and fails if the bot token
// is in any of it. The token's one home is a sensitive service env on the
// push source, written by git-push-setup and read by the credential helper
// from the session environment (A6); a copy in a state file would outlive
// every rotation the broker performs.
func assertNoTokenOnDisk(t *testing.T, stateDir string) {
	t.Helper()
	err := filepath.WalkDir(stateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(body), giteaBotToken) {
			t.Errorf("the bot token leaked into %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk state dir: %v", err)
	}
}

// TestBootstrapClose_WiresTheGiteaRepository pins WHERE A1 runs. Provision
// writes PARTIAL metas (no BootstrappedAt — writeProvisionMetas), so a pass
// that reconciles there sees no pair at all; the metas become complete at the
// bootstrap's terminal step. Live-verified 2026-09-16 on a real Mate: a
// classic bootstrap said nothing about Gitea at provision and left
// action="group-recipe" answering "no pair has its Gitea repository yet".
func TestBootstrapClose_WiresTheGiteaRepository(t *testing.T) {
	stateDir := t.TempDir()
	fake := newFakeGitea()
	srv := fake.start(t)
	t.Setenv("GITEA_URL", srv.URL)
	t.Setenv("MATE_BROKER_URL", srv.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	client := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE"},
		{ID: "svc-appstage", Name: "appstage", Status: "READY_TO_DEPLOY"},
	})
	engine := workflow.NewEngine(stateDir, workflow.EnvContainer, nil)
	mcpSrv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(mcpSrv, client, srv.Client(), "p1", nil, engine, nil, stateDir, "zcp",
		nil, giteaReconcileSSH(), runtime.Info{InContainer: true, ProjectID: "p1"}, "")

	callTool(t, mcpSrv, "zerops_workflow", map[string]any{
		"action": "start", "workflow": "bootstrap", "route": "classic", "intent": "a Mate's first pair",
	})
	callTool(t, mcpSrv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "discover",
		"plan": []map[string]any{{"runtime": map[string]any{
			"devHostname": "appdev", "stageHostname": "appstage",
			"type": "nodejs@22", "bootstrapMode": "standard",
		}}},
	})
	provision := getTextContent(t, callTool(t, mcpSrv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "provision", "attestation": "The pair is up.",
	}))
	closed := getTextContent(t, callTool(t, mcpSrv, "zerops_workflow", map[string]any{
		"action": "complete", "step": "close", "attestation": "Bootstrap closed.",
	}))

	if !strings.Contains(closed, "repository acme/appdev wired") {
		t.Errorf("close must report the repository A1 wired.\nprovision: %s\nclose: %s",
			truncateForTest(provision), truncateForTest(closed))
	}
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta == nil || meta.Gitea == nil || meta.Gitea.FullName != "acme/appdev" {
		t.Fatalf("the pair must carry its Gitea repository once bootstrap is closed, got %+v", meta)
	}
	if len(fake.repoRequests) != 1 {
		t.Errorf("broker asked %d times, want exactly 1", len(fake.repoRequests))
	}
	assertNoTokenOnDisk(t, stateDir)
}

func truncateForTest(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

// TestReconcileGiteaRepositories_PutsThePairOnItsBranch pins what A1 owes the
// push source. The Mate's branch is decided here and recorded on the meta,
// and `git push -u origin <branch>` needs a LOCAL branch of that name: a pair
// whose git init left it on `main` answers "src refspec mate/{bot} does not
// match any" and can never deliver (live-verified 2026-09-16 — the first
// zerops_deploy strategy="git-push" on a real Mate failed exactly there).
//
// The branch must also DESCEND from the repository's protected base: the
// broker's `main` is born with an initial commit, so a branch cut from zcp's
// own fresh history shares no commit with it and Gitea refuses both `merge`
// and `squash` on the pull request (measured the same day).
func TestReconcileGiteaRepositories_PutsThePairOnItsBranch(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaPairMeta(t, stateDir)
	fake := newFakeGitea()
	srv := fake.start(t)
	ssh := giteaReconcileSSH()
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

	reconcileGiteaRepositories(
		context.Background(), client, srv.Client(), ssh,
		runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir,
		writeLiveEnvFile(t, map[string]string{
			"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
		}),
	)

	joined := strings.Join(ssh.commands, "\n")
	for _, want := range []string{"mate/mate-p1", "fetch --no-tags origin 'main'", `commit-tree "HEAD^{tree}" -p HEAD -p FETCH_HEAD`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("A1 must branch the push source off the protected base (missing %q); commands were:\n%s", want, joined)
		}
	}
}

// elapseGiteaBackoff moves the pair's last attempt an hour back, so the next
// pass is due whatever the attempt count.
func elapseGiteaBackoff(t *testing.T, stateDir, hostname string) {
	t.Helper()
	state := readGiteaPairState(stateDir, hostname)
	state.LastAttemptAt = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	writeGiteaPairState(stateDir, hostname, state)
}

// giteaBranchStepSSH answers like giteaReconcileSSH, except that the branch
// step refuses with *refusal while it is set — the way a recipe pair's README
// mode clash failed it on a live Mate (test - Gita, 2026-09-24).
func giteaBranchStepSSH(refusal *string) *containerSSHStub {
	return &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			if strings.Contains(cmd, "cur_email=$(git config user.email)") {
				return []byte("ZCP_EMAIL_SEEDED\nZCP_NAME_SEEDED\n"), nil
			}
			if *refusal != "" && strings.Contains(cmd, `commit-tree "HEAD^{tree}"`) {
				return []byte("ZCP_BRANCH_REFUSED:" + *refusal + "\n"), errors.New("exit status 5")
			}
			return []byte("ok"), nil
		},
	}
}

// TestReconcileGiteaRepositories_RetriesAPairWhoseBranchStepFailed is D2: the
// git-push-setup stamp lands before the branch step, so a failed branch step
// left a Gitea remote with no record — which the next pass read as a remote
// the user chose, and never touched again. A productive pass then starts the
// backoff afresh.
func TestReconcileGiteaRepositories_RetriesAPairWhoseBranchStepFailed(t *testing.T) {
	stateDir := t.TempDir()
	writeGiteaPairMeta(t, stateDir)
	fake := newFakeGitea()
	srv := fake.start(t)
	envPath := writeLiveEnvFile(t, map[string]string{
		"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
	})
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
	rt := runtime.Info{InContainer: true, ProjectID: "p1"}
	refusal := "ZCP_BASE_NOT_SEED"
	ssh := giteaBranchStepSSH(&refusal)

	for pass := 1; pass <= 2; pass++ {
		report := reconcileGiteaRepositories(context.Background(), client, srv.Client(), ssh, rt, stateDir, envPath)
		if joined := strings.Join(report, " | "); !strings.Contains(joined, "ZCP_BASE_NOT_SEED") {
			t.Errorf("pass %d must name the refused state, got %q", pass, joined)
		}
		elapseGiteaBackoff(t, stateDir, "appdev")
	}
	if meta, _ := workflow.FindServiceMeta(stateDir, "appdev"); meta == nil || meta.Gitea != nil {
		t.Fatalf("a refused branch step must leave the pair unrecorded, got %+v", meta)
	}

	refusal = ""
	report := reconcileGiteaRepositories(context.Background(), client, srv.Client(), ssh, rt, stateDir, envPath)
	if len(fake.repoRequests) != 3 {
		t.Errorf("broker asked %d times, want 3 — every pass after a failed branch step retries", len(fake.repoRequests))
	}
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta == nil || meta.Gitea == nil || meta.Gitea.Branch != "mate/mate-p1" {
		t.Fatalf("the retried pair must end wired, got %+v; report %v", meta, report)
	}
	if got := readGiteaPairState(stateDir, "appdev").Attempts; got != 1 {
		t.Errorf("a productive pass must start the backoff afresh, attempts = %d, want 1", got)
	}
}

// TestReconcileGiteaRepositories_ARefusalNamesItsRemedy: a pass that only
// said "retrying on the next pass" left a pair stuck for good — nothing that
// retries changes the state the branch step refused. Each refusal is reported
// with the one thing to do about it.
func TestReconcileGiteaRepositories_ARefusalNamesItsRemedy(t *testing.T) {
	for _, tc := range []struct {
		refusal string
		remedy  string
	}{
		{"ZCP_BASE_NOT_SEED", "git fetch origin 'main' && git merge --allow-unrelated-histories --no-edit FETCH_HEAD"},
		{"ZCP_BRANCH_ELSEWHERE", "git checkout 'mate/mate-p1'"},
		{"ZCP_STAGED_CHANGES", "git restore --staged ."},
		{"ZCP_UNTRACKED_COLLISION app.js", "(app.js): move or delete them"},
	} {
		t.Run(tc.refusal, func(t *testing.T) {
			stateDir := t.TempDir()
			writeGiteaPairMeta(t, stateDir)
			fake := newFakeGitea()
			srv := fake.start(t)
			refusal := tc.refusal
			report := reconcileGiteaRepositories(context.Background(),
				platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}}),
				srv.Client(), giteaBranchStepSSH(&refusal),
				runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir,
				writeLiveEnvFile(t, map[string]string{
					"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
				}))
			joined := strings.Join(report, " | ")
			if !strings.Contains(joined, tc.remedy) {
				t.Errorf("the report must name the remedy %q, got %q", tc.remedy, joined)
			}
			if strings.Contains(joined, "next pass") {
				t.Errorf("a refusal the pass cannot change must not be told to wait for one: %q", joined)
			}
		})
	}
}

// TestReconcileGiteaRepositories_AGiteaRemoteNamingAnotherRepositoryIsTheUsersAndNeverReachesTheBroker:
// the half-wired rule covers only this pair's own repository. A remote on the
// same Gitea whose repository is not named after the pair is one the user
// chose: no pass asks the broker for a repository on its behalf — the broker
// would create one — and nothing rewrites the remote.
func TestReconcileGiteaRepositories_AGiteaRemoteNamingAnotherRepositoryIsTheUsersAndNeverReachesTheBroker(t *testing.T) {
	stateDir := t.TempDir()
	fake := newFakeGitea()
	srv := fake.start(t)
	theirs := srv.URL + "/someone/their-app.git"
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, StageHostname: "appstage",
		GitPushState: topology.GitPushConfigured, RemoteURL: theirs, TrackedRef: "main",
		BootstrapSession: "test", BootstrappedAt: "2026-09-16",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	ssh := giteaReconcileSSH()
	report := reconcileGiteaRepositories(context.Background(),
		platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}}),
		srv.Client(), ssh, runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir,
		writeLiveEnvFile(t, map[string]string{
			"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
		}))
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta == nil || meta.Gitea != nil || meta.RemoteURL != theirs {
		t.Fatalf("another repository's remote must be left as it was, got %+v", meta)
	}
	if len(ssh.commands) != 0 {
		t.Errorf("nothing may run on the pair, ran:\n%s", strings.Join(ssh.commands, "\n"))
	}
	if len(fake.repoRequests) != 0 {
		t.Errorf("the broker must not be asked for a repository, asked %d times", len(fake.repoRequests))
	}
	if len(report) != 0 {
		t.Errorf("the user's own remote is nothing to report, got %v", report)
	}
}

// TestReconcileGiteaRepositories_AGiteaRemoteWithoutARecordIsOurs is the
// mirror of LeavesAUserRemoteAlone: a remote on this Mate's own Gitea with no
// record is a wiring that stopped half-way, not a remote the user chose.
func TestReconcileGiteaRepositories_AGiteaRemoteWithoutARecordIsOurs(t *testing.T) {
	stateDir := t.TempDir()
	fake := newFakeGitea()
	srv := fake.start(t)
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        srv.URL + "/acme/appdev",
		TrackedRef:       "main",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-09-16",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
	report := reconcileGiteaRepositories(
		context.Background(), client, srv.Client(), giteaReconcileSSH(),
		runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir,
		writeLiveEnvFile(t, map[string]string{
			"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
		}),
	)
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta == nil || meta.Gitea == nil || meta.Gitea.FullName != "acme/appdev" {
		t.Fatalf("a Gitea remote with no record must be wired again, got %+v; report %v", meta, report)
	}
}

// wireGiteaPairOnMain wires one pair against a fake Gitea from a container
// whose working tree is on the seed's main when git-push-setup runs, the
// state it is in on a live Mate before the branch step.
func wireGiteaPairOnMain(t *testing.T, stateDir string) {
	t.Helper()
	writeGiteaPairMeta(t, stateDir)
	fake := newFakeGitea()
	srv := fake.start(t)
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
	ssh := &containerSSHStub{
		dispatch: func(cmd string) ([]byte, error) {
			switch {
			case strings.Contains(cmd, "cur_email=$(git config user.email)"):
				return []byte("ZCP_EMAIL_SEEDED\nZCP_NAME_SEEDED\n"), nil
			case strings.Contains(cmd, "git symbolic-ref --short HEAD"):
				return []byte("main\n"), nil
			}
			return []byte("ok"), nil
		},
	}
	report := reconcileGiteaRepositories(
		context.Background(), client, srv.Client(), ssh,
		runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir,
		writeLiveEnvFile(t, map[string]string{
			"GITEA_URL": srv.URL, "MATE_BROKER_URL": srv.URL, "GITEA_TOKEN": giteaBotToken,
		}),
	)
	if meta, _ := workflow.FindServiceMeta(stateDir, "appdev"); meta == nil || meta.Gitea == nil {
		t.Fatalf("the pair must end wired, got %+v; report %v", meta, report)
	}
}

// TestReconcileGiteaRepositories_AWiredPairsTrackedRefStaysMain: wiring
// records the repository's base as the tracked ref, as it always has. The
// release freshness check compares HEAD with that ref, so a Mate's unmerged
// branch never becomes what a release tags; git-push still goes to the
// Mate's branch through notTheProtectedBase.
func TestReconcileGiteaRepositories_AWiredPairsTrackedRefStaysMain(t *testing.T) {
	stateDir := t.TempDir()
	wireGiteaPairOnMain(t, stateDir)
	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta.TrackedRef != "main" {
		t.Errorf("tracked ref = %q, want main", meta.TrackedRef)
	}
	if got := resolveTrackedBranch(stateDir, "appdev", ""); got != "mate/mate-p1" {
		t.Errorf("a default git-push must still go to the Mate's branch, got %q", got)
	}
}

// TestHandleRelease_AWiredGiteaPairIsRefused: the Mate's pushed branch is not
// main, so the freshness check against the tracked ref fails and no tag is
// pushed at an unmerged commit of the group's repository.
func TestHandleRelease_AWiredGiteaPairIsRefused(t *testing.T) {
	// non-parallel: stubs the package-level push-proof reader.
	stateDir := t.TempDir()
	wireGiteaPairOnMain(t, stateDir)

	var askedRef string
	prev := launchPushProofReader
	launchPushProofReader = func(_ context.Context, _ ops.SSHDeployer, _ runtime.Info, _ string, _ string, ref string) (LaunchPushProofResult, error) {
		askedRef = ref
		if ref == "mate/mate-p1" {
			return LaunchPushProofResult{LocalHead: "branchhead", RemoteHead: "branchhead"}, nil
		}
		return LaunchPushProofResult{LocalHead: "branchhead", RemoteHead: "mainhead"}, nil
	}
	t.Cleanup(func() { launchPushProofReader = prev })

	ssh := &containerSSHStub{dispatch: func(string) ([]byte, error) { return []byte("ok"), nil }}
	result, _, _ := handleRelease(context.Background(), ssh,
		WorkflowInput{Service: "appdev", ReleaseVersion: "v1.0.0"}, stateDir, runtime.Info{InContainer: true})
	if askedRef != "main" {
		t.Errorf("freshness compared against %q, want main", askedRef)
	}
	if result == nil || !result.IsError {
		t.Fatalf("release of a wired pair must be refused, got %+v", result)
	}
	for _, cmd := range ssh.commands {
		if strings.Contains(cmd, "git tag") {
			t.Errorf("no tag may be pushed, but ran:\n%s", cmd)
		}
	}
}

// TestGiteaRemoteIsThePairs: a remote without a record counts as a half-way
// wiring only when it names the repository the broker would hand the pair.
func TestGiteaRemoteIsThePairs(t *testing.T) {
	t.Parallel()
	const gitea = "https://gitea.example.com"
	tests := []struct {
		name   string
		remote string
		org    string
		want   bool
	}{
		{name: "the pair's repository, org unknown", remote: gitea + "/acme/appdev.git", want: true},
		{name: "the pair's repository in the known org", remote: "https://tok@GITEA.example.com/acme/appdev/", org: "acme", want: true},
		{name: "a repository named otherwise", remote: gitea + "/acme/their-app.git"},
		{name: "the pair's name in another org", remote: gitea + "/someone/appdev.git", org: "acme"},
		{name: "another host", remote: "https://github.com/acme/appdev.git"},
		{name: "no org segment", remote: gitea + "/appdev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := giteaRemoteIsThePairs(tt.remote, gitea, "appdev", tt.org); got != tt.want {
				t.Errorf("giteaRemoteIsThePairs(%q, org %q) = %v, want %v", tt.remote, tt.org, got, tt.want)
			}
		})
	}
}
