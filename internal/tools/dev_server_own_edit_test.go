// Tests for: dev_server_migrate.go — what zcp edits in a person's repository
// unasked to run the dev servers under zcp-devsup: as little as it can, never
// a moving release, and landed as a commit of its own (docs/spec-mate.md
// §2.8a.2 "Binary and delivery").

package tools

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// olderDevsupURL is a release older than testDevsupURL, the newest.
const olderDevsupURL = "https://github.com/zeropsio/zcp/releases/download/v9.220.0/zcp-devsup-linux-amd64"

// ownEditDeployServer registers zerops_deploy for appdev, a dev-mode dev
// service deployed before when deployed, on the container c.
func ownEditDeployServer(t *testing.T, c ops.SSHDeployer, deployed bool) *mcp.Server {
	t.Helper()
	dir := t.TempDir()
	meta := &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeDev, BootstrapSession: "sess1",
		BootstrappedAt: "2026-10-14", PrimarySetupName: "dev",
	}
	if deployed {
		meta.FirstDeployedAt = "2026-10-14T20:00:00Z"
	}
	if err := workflow.WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{{ID: "svc-1", Name: "appdev", ProjectID: "proj-1", Status: serviceStatusRunning,
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "bun@1.3"}}}).
		WithAppVersionEvents([]platform.AppVersionEvent{{ID: "av-1", ProjectID: "proj-1", ServiceStackID: "svc-1", Status: statusActive, Sequence: 1}})
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", c, &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"},
		nil, runtime.Info{}, dir, testDeployEngine(t), nil, DevsupSource{Latest: func(context.Context) (string, error) { return testDevsupURL, nil }})
	return srv
}

// pinnedDevSetup is todaysDevSetup run under the supervisor zcp-devsup url
// installs.
func pinnedDevSetup(t *testing.T, url string) string {
	t.Helper()
	out, _, _, err := ops.AdoptSupervisorYAML([]byte(todaysDevSetup), "dev", "appdev", url, "")
	if err != nil {
		t.Fatalf("AdoptSupervisorYAML: %v", err)
	}
	return string(out)
}

// TestDeploy_ADevSetupThatAlreadyRunsTheSupervisor_KeepsTheReleaseItPins: a
// dev setup that already installs zcp-devsup — main's, put under the
// supervisor by another Mate of the group — keeps the release it names, on the
// dev service's first deploy and on every later one whose supervisor runs what
// zcp writes. zcp edits nothing, commits nothing and says nothing of it: a
// newer release out since is no reason to change the person's file in the
// middle of their change (Kaďák 2026-10-27, Filip 2026-10-28).
func TestDeploy_ADevSetupThatAlreadyRunsTheSupervisor_KeepsTheReleaseItPins(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		deployed bool
		// running: a supervisor answers on appdev and runs declaration
		// format 1, the one zcp writes.
		running bool
	}{
		{name: "the dev service's first deploy", deployed: false},
		{name: "a later deploy whose supervisor runs what zcp writes", deployed: true, running: true},
		{name: "a later deploy with no supervisor answering", deployed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pinned := pinnedDevSetup(t, olderDevsupURL)
			c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: pinned, Git: &devservertest.Repo{Branch: "feat-x"}}
			if tt.running {
				c.Installed, c.Running, c.SupervisorVersion, c.Formats = true, true, "v9.220.0", []int{1}
			}
			srv := ownEditDeployServer(t, c, tt.deployed)

			result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "appdev", "setup": "dev"})
			text := getTextContent(t, result)
			if result.IsError {
				t.Fatalf("zerops_deploy: %s", text)
			}
			if c.YAML != pinned {
				t.Errorf("zerops.yml changed:\n%s\nwant it as written:\n%s", c.YAML, pinned)
			}
			if len(c.Git.Commits) != 0 {
				t.Errorf("zcp committed %q, want nothing", c.Git.Commits)
			}
			if strings.Contains(text, "re-pinned") || strings.Contains(text, "under the dev-server supervisor") {
				t.Errorf("the deploy speaks of an edit it did not make: %s", text)
			}
		})
	}
}

// TestDeploy_ADevSetupWhoseSupervisorCannotRunWhatZcpWrites_IsRePinnedAndSaysWhy:
// only a supervisor that cannot run the declaration format zcp writes moves
// the dev setup's pin — to the newest release, inside the deploy the agent
// asked for — and the deploy says why, then lands the edit as a commit of its
// own before the push, so the deploy ships it from git.
func TestDeploy_ADevSetupWhoseSupervisorCannotRunWhatZcpWrites_IsRePinnedAndSaysWhy(t *testing.T) {
	t.Parallel()
	c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: pinnedDevSetup(t, olderDevsupURL), Git: &devservertest.Repo{Branch: "feat-x"},
		Installed: true, Running: true, SupervisorVersion: "v9.220.0"}
	srv := ownEditDeployServer(t, c, true)

	result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "appdev", "setup": "dev"})
	text := getTextContent(t, result)
	if result.IsError {
		t.Fatalf("zerops_deploy: %s", text)
	}
	if want := pinnedDevSetup(t, testDevsupURL); c.YAML != want {
		t.Errorf("zerops.yml after the deploy:\n%s\nwant the newest pin and nothing else changed:\n%s", c.YAML, want)
	}
	for _, want := range []string{
		`zcp re-pinned zcp-devsup in setup \"dev\" of zerops.yml from v9.220.0 to v9.300.0`,
		`cannot run declaration format 1`,
		`zcp committed zerops.yml on branch feat-x as a commit of its own`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the deploy does not say %q: %s", want, text)
		}
	}
	if len(c.Git.Commits) != 1 || !strings.Contains(c.Git.Commits[0], "zcp: install zcp-devsup v9.300.0 in setup dev") {
		t.Errorf("commits = %q, want one of zcp's own", c.Git.Commits)
	}
	committedBeforePush(t, c)
}

// committedBeforePush fails unless zcp's commit ran before the zcli push.
func committedBeforePush(t *testing.T, c *devservertest.Container) {
	t.Helper()
	sent := c.Sent()
	commit := slices.IndexFunc(sent, func(cmd string) bool { return strings.Contains(cmd, "@@zcp-own-edit committed") })
	push := slices.IndexFunc(sent, func(cmd string) bool { return strings.Contains(cmd, "zcli push") })
	if commit < 0 || push < 0 || commit > push {
		t.Errorf("zcp's commit is command %d and the push %d; want the commit first, so the deploy ships it from git", commit, push)
	}
}

// TestDeploy_ZcpsOwnEditToADevSetup_LandsAsACommitOfItsOwn: when zcp must edit
// the repository — a dev setup without the supervisor, on the dev service's
// first deploy — it says once which file and why, and lands it as a commit of
// its own on the checkout's work branch, so the edit rides with that branch's
// change and main carries it once the person merges it. It commits only its
// own lines, skips the repository's commit checks (a deploy never waits on the
// app's typecheck) and signs as zcp. On main, which only a merged change
// moves, beside uncommitted lines of the person's in the same file, on a
// detached HEAD, or when git refuses, it commits nothing and says what to
// commit.
func TestDeploy_ZcpsOwnEditToADevSetup_LandsAsACommitOfItsOwn(t *testing.T) {
	t.Parallel()
	const subject = "zcp: run setup dev under the dev-server supervisor (zcp-devsup v9.300.0)"
	tests := []struct {
		name        string
		repo        *devservertest.Repo
		wantCommit  bool
		wantSays    []string
		wantNotSays []string
	}{
		{
			name:       "on a work branch",
			repo:       &devservertest.Repo{Branch: "feat-x"},
			wantCommit: true,
			wantSays:   []string{`zcp committed zerops.yml on branch feat-x as a commit of its own`, subject},
		},
		{
			name:     "on main, which only a merged change moves",
			repo:     &devservertest.Repo{Branch: "main"},
			wantSays: []string{`zerops.yml is not committed: the checkout is on main`, `git commit -m '` + subject + `' -- zerops.yml`},
		},
		{
			name:     "beside uncommitted lines of the person's in the same file",
			repo:     &devservertest.Repo{Branch: "feat-x", Uncommitted: []string{"zerops.yml"}},
			wantSays: []string{`zerops.yml is not committed: it also holds uncommitted lines of yours`, subject},
		},
		{
			name:     "on a detached HEAD",
			repo:     &devservertest.Repo{},
			wantSays: []string{`zerops.yml is not committed: HEAD is detached`, `git commit -m '` + subject + `' -- zerops.yml`},
		},
		{
			name:     "git refuses the commit",
			repo:     &devservertest.Repo{Branch: "feat-x", CommitFails: "fatal: Unable to create '/var/www/.git/index.lock': File exists."},
			wantSays: []string{`zerops.yml is not committed: the commit failed`, `index.lock`, `git commit -m '` + subject + `' -- zerops.yml`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: todaysDevSetup, Git: tt.repo}
			srv := ownEditDeployServer(t, c, false)

			result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "appdev", "setup": "dev"})
			text := getTextContent(t, result)
			if result.IsError {
				t.Fatalf("zerops_deploy: %s", text)
			}
			if !strings.Contains(c.YAML, "start: zcp-devsup run") {
				t.Fatalf("the dev setup was not put under the supervisor:\n%s", c.YAML)
			}
			if !strings.Contains(text, `setup \"dev\" in zerops.yml now installs and runs zcp-devsup`) {
				t.Errorf("the deploy does not say what changed: %s", text)
			}
			for _, want := range tt.wantSays {
				if !strings.Contains(text, jsonEscaped(want)) {
					t.Errorf("the deploy does not say %q: %s", want, text)
				}
			}
			if strings.Contains(text, "with your next change") {
				t.Errorf("the deploy leaves the edit to some next change: %s", text)
			}
			if !tt.wantCommit {
				if len(c.Git.Commits) != 0 {
					t.Errorf("zcp committed %q, want nothing", c.Git.Commits)
				}
				return
			}
			if len(c.Git.Commits) != 1 {
				t.Fatalf("commits = %q, want exactly one", c.Git.Commits)
			}
			commit := c.Git.Commits[0]
			for _, want := range []string{"--no-verify", "'zerops.yml'", "Zerops Agent", subject} {
				if !strings.Contains(commit, want) {
					t.Errorf("zcp's commit %q lacks %q", commit, want)
				}
			}
			committedBeforePush(t, c)
		})
	}
}

// jsonEscaped is s as it reads inside a JSON string.
func jsonEscaped(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// TestDevServer_TheMoveOntoTheSupervisor_CommitsBothFilesOnTheWorkBranch: the
// first dev-server start that moves a dev setup onto the supervisor edits
// zerops.yaml and declares the server; on a work branch both land as one
// commit of zcp's own before the redeploy, and the start says so.
func TestDevServer_TheMoveOntoTheSupervisor_CommitsBothFilesOnTheWorkBranch(t *testing.T) {
	t.Parallel()
	c := &devservertest.Container{YAMLFile: "zerops.yml", YAML: todaysDevSetup, Git: &devservertest.Repo{Branch: "feat-x"}}
	srv, _ := migrationToolServer(t, c, statusActive)

	a := callDevServer(t, srv, map[string]any{"action": "start", "hostname": "appdev", "command": "bun --watch index.ts", "port": 3000})
	if !a.Running {
		t.Fatalf("after the move the server is not up: %+v", a)
	}
	if !strings.Contains(a.Message, "zcp committed zerops.yml and .zcp/dev-servers.json on branch feat-x as a commit of its own") ||
		strings.Contains(a.Message, "with your next change") {
		t.Errorf("message = %q, want it to say zcp committed both files on the work branch", a.Message)
	}
	if len(c.Git.Commits) != 1 || !strings.Contains(c.Git.Commits[0], "'zerops.yml'") || !strings.Contains(c.Git.Commits[0], "'.zcp/dev-servers.json'") {
		t.Errorf("commits = %q, want one holding both files", c.Git.Commits)
	}
	committedBeforePush(t, c)
}
