// Tests for: tools/gitea_pull_request.go — the pull request that lands a
// Mate's branch on its repository's protected `main`.
//
// A1 wires the repository in a pass where the branch cannot exist yet — it is
// created locally there and pushed for the first time by a deploy — so the
// pull request has TWO honest triggers: the push that puts the branch on the
// remote, and a later reconcile pass that finds a branch there with no open
// request. Measured on a live Mate 2026-09-16: wiring-only meant a Mate
// pushed and nothing ever opened.
package tools

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// writeWiredGiteaPairMeta seeds a pair A1 has already finished with: the
// repository is there, git-push is configured, and the branch exists only
// locally — the state every Mate sits in between bootstrap and its first
// deploy.
func writeWiredGiteaPairMeta(t *testing.T, stateDir, remoteURL string) {
	t.Helper()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		StageHostname:    "appstage",
		BootstrapSession: "test",
		BootstrappedAt:   "2026-09-16",
		GitPushState:     topology.GitPushConfigured,
		RemoteURL:        remoteURL,
		BuildIntegration: topology.BuildIntegrationActions,
		Gitea: &workflow.GiteaRepoRef{
			FullName:      "acme/appdev",
			Branch:        "mate/mate-p1",
			DefaultBranch: "main",
		},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// TestGitPushDeploy_OpensThePullRequest is the first trigger: the push is
// what puts `mate/{bot}` on the remote, so it is the first moment a pull
// request CAN exist — and the Mate is done pushing, so nothing else will open
// it.
func TestGitPushDeploy_OpensThePullRequest(t *testing.T) {
	tests := []struct {
		name string
		// pullsOpen is what Gitea's open-pull-request list answers.
		pullsOpen string
		// intent is the open work session's, "" for no session.
		intent      string
		wantCreates int
		wantNumber  int
		// wantTitle is what the request is opened as, when one is.
		wantTitle string
		// openTitle is what the request already open is called; wantRetitle is
		// what it is renamed to, "" for left alone.
		openTitle   string
		wantRetitle string
	}{
		{name: "no request open yet", pullsOpen: `[]`, wantCreates: 1, wantNumber: 3, wantTitle: "Mate: appdev"},
		{
			name:        "titled after the task, like the commit",
			pullsOpen:   `[]`,
			intent:      "Show how many todos are still open, above the list.\nThen check the count on the stage.",
			wantCreates: 1, wantNumber: 3,
			wantTitle: "Show how many todos are still open, above the list.",
		},
		{
			name: "one is already open",
			pullsOpen: `[{"number":9,"state":"open","head":{"ref":"mate/mate-p1",` +
				`"repo":{"full_name":"acme/appdev"}},"base":{"ref":"main"}}]`,
			wantCreates: 0, wantNumber: 9,
		},
		{
			// The owner's timeline, 2026-09-17: two Mates' rows read "Mate:
			// appdev". A request opened with no session to name it takes the
			// task's words from the first delivery that has them.
			name: "an open one still under zcp's own words is named after the task",
			pullsOpen: `[{"number":9,"state":"open","head":{"ref":"mate/mate-p1",` +
				`"repo":{"full_name":"acme/appdev"}},"base":{"ref":"main"}}]`,
			intent:      "Add a due date to each todo.",
			openTitle:   "Mate: appdev",
			wantCreates: 0, wantNumber: 9, wantRetitle: "Add a due date to each todo.",
		},
		{
			name: "an open one somebody named keeps its name",
			pullsOpen: `[{"number":9,"state":"open","head":{"ref":"mate/mate-p1",` +
				`"repo":{"full_name":"acme/appdev"}},"base":{"ref":"main"}}]`,
			intent:      "Then sort them by due date.",
			openTitle:   "Add a due date to each todo.",
			wantCreates: 0, wantNumber: 9,
		},
		{
			name: "an open one is left alone when no session says what the work is",
			pullsOpen: `[{"number":9,"state":"open","head":{"ref":"mate/mate-p1",` +
				`"repo":{"full_name":"acme/appdev"}},"base":{"ref":"main"}}]`,
			openTitle:   "Mate: appdev",
			wantCreates: 0, wantNumber: 9,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeGitea()
			fake.pullsOpen = tt.pullsOpen
			fake.pullTitle = tt.openTitle
			gitea := fake.start(t)

			stateDir := t.TempDir()
			remote := gitea.URL + "/acme/appdev.git"
			writeWiredGiteaPairMeta(t, stateDir, remote)
			if tt.intent != "" {
				ws := workflow.NewWorkSession("proj-1", "container", tt.intent, []string{"appdev"})
				if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
					t.Fatalf("SaveWorkSession: %v", err)
				}
				t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })
			}
			t.Setenv("GITEA_URL", gitea.URL)
			t.Setenv("MATE_BROKER_URL", gitea.URL)
			t.Setenv("GITEA_TOKEN", giteaBotToken)

			mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
			ssh := &stubSSHWithCommands{tokenOutput: []byte("1"), committedOutput: []byte("1")}
			authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
			RegisterDeploySSH(srv, mock, gitea.Client(), "proj-1", ssh, authInfo, nil,
				runtime.Info{InContainer: true, ProjectID: "proj-1", GiteaURL: gitea.URL},
				stateDir, testDeployEngine(t), nil)

			text := getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{
				"targetService": "appdev",
				"strategy":      "git-push",
			}))
			if fake.pullCreates != tt.wantCreates {
				t.Errorf("pull requests created = %d, want %d\n%s", fake.pullCreates, tt.wantCreates, text)
			}
			if tt.wantCreates > 0 && (len(fake.pullTitles) == 0 || fake.pullTitles[0] != tt.wantTitle) {
				t.Errorf("pull request titled %q, want %q", strings.Join(fake.pullTitles, " | "), tt.wantTitle)
			}
			if got := strings.Join(fake.pullRetitles, " | "); got != tt.wantRetitle {
				t.Errorf("the open request was renamed %q, want %q", got, tt.wantRetitle)
			}
			if !strings.Contains(text, `"pullRequest"`) {
				t.Errorf("the push must report the pull request it landed in:\n%s", text)
			}
			// Nothing builds from a Mate's branch: the push is not watched for a
			// build, and the agent is never offered an integration to wire.
			if strings.Contains(text, "build-integration") || strings.Contains(text, "NOT_OBSERVED") {
				t.Errorf("a push to the group's Gitea must not watch for a build or offer an integration:\n%s", text)
			}
			if !strings.Contains(text, "merge") {
				t.Errorf("the push must say the person merges the request:\n%s", text)
			}
			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
			if meta == nil || meta.Gitea == nil || meta.Gitea.PullRequest != tt.wantNumber {
				t.Fatalf("the pair must record pull request #%d, got %+v", tt.wantNumber, meta.Gitea)
			}
		})
	}
}

// TestGitPushDeploy_NonGiteaRemoteOpensNothing — the pull request is a Gitea
// act. A pair pushing to a remote of the user's own gets no request and no
// call to anything.
func TestGitPushDeploy_NonGiteaRemoteOpensNothing(t *testing.T) {
	fake := newFakeGitea()
	gitea := fake.start(t)

	stateDir := t.TempDir()
	writeWiredGiteaPairMeta(t, stateDir, "https://github.com/someone/their-app.git")
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	mock := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
	ssh := &stubSSHWithCommands{tokenOutput: []byte("1"), committedOutput: []byte("1")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, gitea.Client(), "proj-1", ssh, authInfo, nil,
		runtime.Info{InContainer: true, ProjectID: "proj-1", GiteaURL: gitea.URL},
		stateDir, testDeployEngine(t), nil)

	text := getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
	}))
	if fake.pullCreates != 0 {
		t.Errorf("a push to someone else's remote must open nothing:\n%s", text)
	}
}

// TestReconcileGitea_OpensThePullRequestOnceTheBranchIsThere is the second
// trigger: a Mate that pushed before this existed, or a push whose
// pull-request call failed, catches up on the next bootstrap/adopt pass —
// but only once the branch is actually on the remote, because Gitea refuses
// a request whose head it cannot resolve.
func TestReconcileGitea_OpensThePullRequestOnceTheBranchIsThere(t *testing.T) {
	tests := []struct {
		name         string
		branchExists bool
		wantCreates  int
		wantNumber   int
		wantReport   []string
	}{
		{
			name: "the branch is on the remote", branchExists: true,
			wantCreates: 1, wantNumber: 3,
			wantReport: []string{"pull request #3", "acme/appdev"},
		},
		{
			name: "the Mate has not pushed yet", branchExists: false,
			wantCreates: 0, wantNumber: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeGitea()
			fake.branchExists = tt.branchExists
			gitea := fake.start(t)

			stateDir := t.TempDir()
			writeWiredGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
			envPath := writeLiveEnvFile(t, map[string]string{
				"GITEA_URL": gitea.URL, "MATE_BROKER_URL": gitea.URL, "GITEA_TOKEN": giteaBotToken,
			})
			client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

			report := reconcileGiteaRepositories(
				context.Background(), client, gitea.Client(), giteaReconcileSSH(),
				runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir, envPath,
			)
			joined := strings.Join(report, " | ")
			for _, want := range tt.wantReport {
				if !strings.Contains(joined, want) {
					t.Errorf("report missing %q: %s", want, joined)
				}
			}
			if fake.pullCreates != tt.wantCreates {
				t.Errorf("pull requests created = %d, want %d (report: %s)", fake.pullCreates, tt.wantCreates, joined)
			}
			// A wired pair never goes back to the broker for a second
			// repository, whatever this pass does about the pull request.
			if len(fake.repoRequests) != 0 {
				t.Errorf("a wired pair must ask the broker for nothing, got %v", fake.repoRequests)
			}
			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
			if meta == nil || meta.Gitea == nil || meta.Gitea.PullRequest != tt.wantNumber {
				t.Fatalf("the pair must record pull request #%d, got %+v", tt.wantNumber, meta.Gitea)
			}
		})
	}
}

// TestReconcileGitea_RecordedPullRequestIsNotReopened — once the number is on
// the meta, no later pass asks Gitea about it at all. The repository was
// already stateful this way; the request is now too.
func TestReconcileGitea_RecordedPullRequestIsNotReopened(t *testing.T) {
	fake := newFakeGitea()
	fake.branchExists = true
	gitea := fake.start(t)

	stateDir := t.TempDir()
	writeWiredGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
	envPath := writeLiveEnvFile(t, map[string]string{
		"GITEA_URL": gitea.URL, "MATE_BROKER_URL": gitea.URL, "GITEA_TOKEN": giteaBotToken,
	})
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
	rt := runtime.Info{InContainer: true, ProjectID: "p1"}

	for pass := 1; pass <= 3; pass++ {
		reconcileGiteaRepositories(context.Background(), client, gitea.Client(), giteaReconcileSSH(), rt, stateDir, envPath)
	}
	if fake.pullCreates != 1 {
		t.Errorf("pull request created %d times across three passes, want exactly 1", fake.pullCreates)
	}
}

// TestAbsorbLandedPullRequestOnCheckout_OnlyOnACleanCheckoutOfTheMatesBranch
// pins the guard: the catch-up absorb runs the Gitea sync command only when
// the checkout is exactly the state it is safe to touch — clean, and on the
// Mate's own branch — and is silent otherwise, best-effort by design.
func TestAbsorbLandedPullRequestOnCheckout_OnlyOnACleanCheckoutOfTheMatesBranch(t *testing.T) {
	tests := []struct {
		name         string
		porcelain    string
		branch       string
		porcelainErr error
		branchErr    error
		wantSynced   bool
	}{
		{name: "clean tree on the Mate's branch runs the sync", porcelain: "", branch: "mate/mate-p1", wantSynced: true},
		{name: "a dirty tree is left alone", porcelain: " M index.js\n", branch: "mate/mate-p1", wantSynced: false},
		{name: "a checkout on some other branch is left alone", porcelain: "", branch: "main", wantSynced: false},
		{name: "an SSH failure reading status is left alone", porcelain: "", branch: "mate/mate-p1", porcelainErr: errors.New("ssh: broken"), wantSynced: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &workflow.ServiceMeta{
				Hostname: "appdev",
				Gitea:    &workflow.GiteaRepoRef{FullName: "acme/appdev", Branch: "mate/mate-p1", DefaultBranch: "main"},
			}
			ssh := &containerSSHStub{dispatch: func(cmd string) ([]byte, error) {
				switch {
				case strings.Contains(cmd, "status --porcelain"):
					return []byte(tt.porcelain), tt.porcelainErr
				case strings.Contains(cmd, "rev-parse --abbrev-ref HEAD"):
					return []byte(tt.branch), tt.branchErr
				default:
					return []byte("ok"), nil
				}
			}}
			absorbLandedPullRequestOnCheckout(context.Background(), ssh, m, "squash-sha", "branch-tip-sha")

			synced := false
			for _, cmd := range ssh.commands {
				if strings.Contains(cmd, "squash-sha") {
					synced = true
				}
			}
			if synced != tt.wantSynced {
				t.Errorf("sync command sent = %v, want %v; commands: %v", synced, tt.wantSynced, ssh.commands)
			}
		})
	}
}

// writeLandedGiteaPairMeta seeds the state this whole pass exists for: a pair
// that is wired, has pushed, and recorded the number of the request its work
// is waiting in.
func writeLandedGiteaPairMeta(t *testing.T, stateDir, remoteURL string, number int) {
	t.Helper()
	writeWiredGiteaPairMeta(t, stateDir, remoteURL)
	if err := workflow.UpsertServiceMeta(stateDir, "appdev",
		func(meta *workflow.ServiceMeta, _ bool) error {
			meta.Gitea.PullRequest = number
			return nil
		}); err != nil {
		t.Fatalf("UpsertServiceMeta: %v", err)
	}
}

// TestReconcile_TellsTheMateWhatBecameOfItsPullRequest is the pull side of the
// feedback loop.
//
// The merge that ends a Mate's work is made in Gitea's UI, by a colleague, by
// a script, or by the app's button — none of them passes through this
// process. So nothing is pushed here: a pass asks git what became of the
// request the pair recorded, which is right for all four.
func TestReconcile_TellsTheMateWhatBecameOfItsPullRequest(t *testing.T) {
	tests := []struct {
		name       string
		pullState  string
		pullMerged bool
		// wantNumber is what the pair still records afterwards; 0 means the
		// number was forgotten, so the next delivery opens the next request.
		wantNumber   int
		wantReport   []string
		wantSilent   bool
		wantLanded   bool
		wantCommit   string
		wantLandHead string
	}{
		{
			// The ordinary state: still waiting on somebody, and worth no words.
			name: "still open", pullState: "open",
			wantNumber: 4, wantSilent: true,
		},
		{
			// A merge lands as a landing to absorb, not just a cleared number —
			// Gitea squashes by default (MB-26), and only the recorded
			// merge_commit_sha/head.sha let the next delivery fold it in
			// losslessly instead of reading it as two unrelated histories.
			name: "merged", pullState: "closed", pullMerged: true,
			wantNumber: 0,
			wantReport: []string{"pull request #4 is merged", `"main"`, "absorbs the landing", "opens a new request"},
			wantLanded: true, wantCommit: "squash-sha", wantLandHead: "branch-tip-sha",
		},
		{
			// Closed and merged mean opposite things to the Mate that opened
			// it: work delivered against work refused.
			name: "closed without merging", pullState: "closed",
			wantNumber: 0,
			wantReport: []string{"closed without merging", "nothing of it is on"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeGitea()
			fake.branchExists = true
			fake.pullState = tt.pullState
			fake.pullMerged = tt.pullMerged
			fake.pullMergeCommit = "squash-sha"
			fake.pullMergeHead = "branch-tip-sha"
			gitea := fake.start(t)

			stateDir := t.TempDir()
			writeLandedGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git", 4)
			envPath := writeLiveEnvFile(t, map[string]string{
				"GITEA_URL": gitea.URL, "MATE_BROKER_URL": gitea.URL, "GITEA_TOKEN": giteaBotToken,
			})
			client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})

			report := reconcileGiteaRepositories(
				context.Background(), client, gitea.Client(), giteaReconcileSSH(),
				runtime.Info{InContainer: true, ProjectID: "p1"}, stateDir, envPath,
			)
			joined := strings.Join(report, " | ")
			if tt.wantSilent && joined != "" {
				t.Errorf("an open request is the ordinary state and worth no words, got: %s", joined)
			}
			for _, want := range tt.wantReport {
				if !strings.Contains(joined, want) {
					t.Errorf("report missing %q: %s", want, joined)
				}
			}
			meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
			if meta == nil || meta.Gitea == nil || meta.Gitea.PullRequest != tt.wantNumber {
				t.Fatalf("recorded request = %+v, want #%d", meta.Gitea, tt.wantNumber)
			}
			landed := meta.Gitea.Landed
			if tt.wantLanded && (landed == nil || landed.Commit != tt.wantCommit || landed.Head != tt.wantLandHead) {
				t.Fatalf("recorded landing = %+v, want commit=%q head=%q", landed, tt.wantCommit, tt.wantLandHead)
			}
			if !tt.wantLanded && landed != nil {
				t.Fatalf("no landing should be recorded here, got %+v", landed)
			}
			// Whatever became of the request, a wired pair never goes back to
			// the broker for a second repository.
			if len(fake.repoRequests) != 0 {
				t.Errorf("a wired pair must ask the broker for nothing, got %v", fake.repoRequests)
			}
		})
	}
}

// TestReconcile_AsksAboutASettledRequestOnABackoff pins the cost of the loop:
// the passes are agent tool calls and arrive in bursts, so a settled pair must
// reach Gitea once per window, not once per call.
func TestReconcile_AsksAboutASettledRequestOnABackoff(t *testing.T) {
	fake := newFakeGitea()
	fake.branchExists = true
	fake.pullState = "open"
	gitea := fake.start(t)

	stateDir := t.TempDir()
	writeLandedGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git", 4)
	envPath := writeLiveEnvFile(t, map[string]string{
		"GITEA_URL": gitea.URL, "MATE_BROKER_URL": gitea.URL, "GITEA_TOKEN": giteaBotToken,
	})
	client := platform.NewMock().WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev"}})
	rt := runtime.Info{InContainer: true, ProjectID: "p1"}

	for range 5 {
		reconcileGiteaRepositories(context.Background(), client, gitea.Client(), giteaReconcileSSH(), rt, stateDir, envPath)
	}
	if fake.pullReads != 1 {
		t.Errorf("a burst of five passes asked Gitea %d times, want 1", fake.pullReads)
	}
}
