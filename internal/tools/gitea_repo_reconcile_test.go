// Tests for: tools/gitea_repo_reconcile.go — A1, git per dev pair as early
// as possible (guide 2.1). Fakes only: a broker and a Gitea on httptest, an
// SSH stub, the platform mock.
package tools

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	userStatus   int
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
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			if r.Method == http.MethodPost {
				f.pullCreates++
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

// TestDefaultPushBranch pins A5 at the delivery site: the default is `main`
// everywhere except a Gitea pair, whose `main` is protected.
func TestDefaultPushBranch(t *testing.T) {
	t.Parallel()

	plain := t.TempDir()
	if err := workflow.WriteServiceMeta(plain, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeSimple,
		BootstrapSession: "t", BootstrappedAt: "2026-09-16",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	if got := defaultPushBranch(plain, "appdev"); got != "main" {
		t.Errorf("a plain pair defaults to %q, want main", got)
	}
	if got := defaultPushBranch(plain, "nobody"); got != "main" {
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
	if got := defaultPushBranch(gitea, "appdev"); got != "mate/mate-p1" {
		t.Errorf("a Gitea pair defaults to %q, want mate/mate-p1", got)
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
	if !strings.Contains(joined, "checkout") || !strings.Contains(joined, "mate/mate-p1") {
		t.Fatalf("A1 must leave the push source on its own branch; commands were:\n%s", joined)
	}
}
