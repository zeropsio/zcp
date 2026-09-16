// Tests for: git_push_rejection.go + the GIT_PUSH_NON_FAST_FORWARD branch
// in deploy_git_push.go (handleGitPush) and deploy_local_git.go
// (handleLocalGitPush). docs/spec-workflows.md §12 GF-11.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// errFakeExit1 stubs the *exec.ExitError a real SSH command failure would
// carry — only its text matters to the classifier.
var errFakeExit1 = errors.New("exit status 1")

// gitBareRemoteFixture creates a bare git repo (a local "remote") with no
// commits, for real-git non-fast-forward tests. Skips when git isn't on
// PATH.
func gitBareRemoteFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH — skipping real-git test")
	}
	dir := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir remote: %v", err)
	}
	runGitFixtureCmd(t, dir, "init", "--bare", "-q")
	return dir
}

// gitWorkingRepoFixture creates an independent local repo (its own initial
// commit, unrelated to any other fixture repo) with origin wired at
// remoteURL. Two repos built this way pushing to the same remote produce a
// genuine non-fast-forward rejection on the second push — real git output,
// no stubbing.
func gitWorkingRepoFixture(t *testing.T, remoteURL, commitMsg string) string {
	t.Helper()
	workDir := t.TempDir()
	runGitFixtureCmd(t, workDir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte(commitMsg+"\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitFixtureCmd(t, workDir, "add", "-A")
	runGitFixtureCmd(t, workDir, "commit", "-q", "-m", commitMsg)
	runGitFixtureCmd(t, workDir, "remote", "add", "origin", remoteURL)
	return workDir
}

func runGitFixtureCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	//nolint:gosec // test-only, inputs are t.TempDir paths / literal args
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// stubSSHDivergence stubs ops.SSHDeployer for GIT_PUSH_NON_FAST_FORWARD
// tests: the push itself is rejected (pushErr), then the divergence probe
// (fetch/rev-list/merge-base) that classifies it runs against canned
// responses. Command dispatch is substring-based, distinct from
// stubSSHWithCommands's catch-all-is-push routing (which would misroute
// these new probe commands).
type stubSSHDivergence struct {
	pushErr      error
	remoteAhead  string // rev-list --count HEAD..FETCH_HEAD output
	localAhead   string // rev-list --count FETCH_HEAD..HEAD output
	mergeBaseErr error  // non-nil => unrelated histories
	fetchErr     error  // non-nil => the fetch step itself fails

	commands []string // every command string executed, for assertions
}

func (s *stubSSHDivergence) ExecSSH(_ context.Context, _ string, command string) ([]byte, error) {
	s.commands = append(s.commands, command)
	switch {
	case strings.Contains(command, "rev-parse HEAD"):
		return []byte("1"), nil
	case strings.Contains(command, `test -n "$GIT_TOKEN"`):
		return []byte("1"), nil
	case strings.Contains(command, "zerops.yaml") || strings.Contains(command, "zerops.yml"):
		return nil, nil
	case strings.Contains(command, "status --porcelain"):
		return nil, nil
	case strings.Contains(command, "'fetch'"):
		return nil, s.fetchErr
	case strings.Contains(command, "'HEAD..FETCH_HEAD'"):
		return []byte(s.remoteAhead), nil
	case strings.Contains(command, "'FETCH_HEAD..HEAD'"):
		return []byte(s.localAhead), nil
	case strings.Contains(command, "'merge-base'"):
		if s.mergeBaseErr != nil {
			return nil, s.mergeBaseErr
		}
		return []byte("abc123"), nil
	default:
		// The actual `git push` invocation.
		return nil, s.pushErr
	}
}

func (s *stubSSHDivergence) ExecSSHBackground(_ context.Context, _, _ string, _ time.Duration) ([]byte, error) {
	return nil, nil
}

func registerGitPushDivergenceTool(t *testing.T, stateDir string, ssh *stubSSHDivergence) *mcp.Server {
	t.Helper()
	setupDeployedService(t, stateDir, "appdev", "")
	markGitPushConfigured(t, stateDir, "appdev")

	mock := platform.NewMock()
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, mock, okHTTP, "proj-1", ssh, authInfo, nil, runtime.Info{}, stateDir, testDeployEngine(t), nil)
	return srv
}

// TestGitPush_NonFastForward_Classified pins the core GF-11 contract: a
// rejected push whose stderr matches git's non-fast-forward family (any of
// the three common phrasings) is classified as GIT_PUSH_NON_FAST_FORWARD —
// not the generic SSH_DEPLOY_FAILED/DEPLOY_FAILED — carrying the structured
// divergence + next-options payload. Container and local paths both covered.
func TestGitPush_NonFastForward_Classified(t *testing.T) {
	t.Parallel()

	phrasings := []struct {
		name   string
		stderr string
	}{
		{
			name:   "rejected-fetch-first",
			stderr: " ! [rejected]        main -> main (fetch first)\nerror: failed to push some refs to 'https://github.com/example/repo'",
		},
		{
			name:   "updates-were-rejected",
			stderr: "hint: Updates were rejected because the remote contains work that you do not\nhint: have locally.",
		},
		{
			name:   "non-fast-forward-word",
			stderr: "! [remote rejected] main -> main (non-fast-forward)",
		},
	}

	for _, tc := range phrasings {
		t.Run("container/"+tc.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			ssh := &stubSSHDivergence{
				pushErr:     &platform.SSHExecError{Output: tc.stderr, Err: errFakeExit1},
				remoteAhead: "2",
				localAhead:  "0",
			}
			srv := registerGitPushDivergenceTool(t, stateDir, ssh)

			result := callTool(t, srv, "zerops_deploy", map[string]any{
				"targetService": "appdev",
				"strategy":      "git-push",
				"remoteUrl":     "https://github.com/example/repo",
			})
			if !result.IsError {
				t.Fatalf("expected an error result, got success: %s", getTextContent(t, result))
			}
			text := getTextContent(t, result)
			if !strings.Contains(text, `"code":"GIT_PUSH_NON_FAST_FORWARD"`) {
				t.Errorf("expected code=GIT_PUSH_NON_FAST_FORWARD, got: %s", text)
			}
			if !strings.Contains(text, `"gitPushRejection"`) {
				t.Errorf("expected a gitPushRejection payload, got: %s", text)
			}
			if !strings.Contains(text, `"remoteAhead":2`) {
				t.Errorf("expected remoteAhead=2 from the probe, got: %s", text)
			}
		})
	}

	t.Run("local", func(t *testing.T) {
		t.Parallel()
		// Two independent local repos pushing the same commit-less bare
		// remote: repo A pushes first (remote now ahead of B), repo B's
		// push is genuinely rejected non-fast-forward by real git.
		remoteDir := gitBareRemoteFixture(t)
		repoA := gitWorkingRepoFixture(t, remoteDir, "from-a")
		repoB := gitWorkingRepoFixture(t, remoteDir, "from-b")
		runGitFixtureCmd(t, repoA, "push", "-q", "origin", "main")

		stateDir := t.TempDir()
		setupDeployedService(t, stateDir, "myproject", "")
		markGitPushConfigured(t, stateDir, "myproject")

		result, _, err := handleLocalGitPush(
			context.Background(), nil, "proj-test", auth.Info{Email: "t@t.com", FullName: "test"},
			DeployLocalInput{
				TargetService: "myproject",
				WorkingDir:    repoB,
				Strategy:      deployStrategyGitPush,
				Branch:        "main",
			},
			stateDir,
		)
		if err != nil {
			t.Fatalf("handleLocalGitPush: %v", err)
		}
		if !result.IsError {
			t.Fatalf("expected non-fast-forward rejection, got success: %s", getTextContent(t, result))
		}
		text := getTextContent(t, result)
		if !strings.Contains(text, `"code":"GIT_PUSH_NON_FAST_FORWARD"`) {
			t.Errorf("expected code=GIT_PUSH_NON_FAST_FORWARD, got: %s", text)
		}
		if !strings.Contains(text, `"gitPushRejection"`) {
			t.Errorf("expected a gitPushRejection payload, got: %s", text)
		}
	})
}

// TestGitPush_NonFastForward_NeverForces pins GF-11's hard boundary: none
// of the commands zcp itself executes while classifying a non-fast-forward
// rejection (the divergence probe) ever carries --force — the three named
// options are choices for the agent/user to run, never something zcp runs
// on their behalf.
func TestGitPush_NonFastForward_NeverForces(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	ssh := &stubSSHDivergence{
		pushErr:     &platform.SSHExecError{Output: " ! [rejected]        main -> main (fetch first)", Err: errFakeExit1},
		remoteAhead: "1",
		localAhead:  "0",
	}
	srv := registerGitPushDivergenceTool(t, stateDir, ssh)

	callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
		"remoteUrl":     "https://github.com/example/repo",
	})

	if len(ssh.commands) == 0 {
		t.Fatal("expected at least one SSH command to have run")
	}
	for _, cmd := range ssh.commands {
		if strings.Contains(cmd, "--force") {
			t.Errorf("zcp executed a command carrying --force: %s", cmd)
		}
	}
}

// TestConvertError_NonFastForward pins the wire shape: GIT_PUSH_NON_FAST_FORWARD
// is a catalog error code, and the gitPushRejection payload carries exactly
// the three named options (rebase, merge, replace-remote), none pre-executed.
func TestConvertError_NonFastForward(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	ssh := &stubSSHDivergence{
		pushErr:     &platform.SSHExecError{Output: " ! [rejected]        main -> main (fetch first)", Err: errFakeExit1},
		remoteAhead: "3",
		localAhead:  "1",
	}
	srv := registerGitPushDivergenceTool(t, stateDir, ssh)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
		"remoteUrl":     "https://github.com/example/repo",
	})
	text := getTextContent(t, result)

	var wire ErrorWire
	if err := json.Unmarshal([]byte(text), &wire); err != nil {
		t.Fatalf("unmarshal ErrorWire: %v\nbody: %s", err, text)
	}
	if wire.Code != platform.ErrGitPushNonFastForward {
		t.Errorf("expected code %q, got %q", platform.ErrGitPushNonFastForward, wire.Code)
	}
	if wire.GitPushRejection == nil {
		t.Fatal("expected a non-nil GitPushRejection payload")
	}
	if wire.GitPushRejection.RemoteAhead != 3 || wire.GitPushRejection.LocalAhead != 1 {
		t.Errorf("expected remoteAhead=3 localAhead=1, got %+v", wire.GitPushRejection)
	}
	wantNames := map[string]bool{"rebase": false, "merge": false, "replace-remote": false}
	if len(wire.GitPushRejection.Next) != 3 {
		t.Fatalf("expected exactly 3 next options, got %d: %+v", len(wire.GitPushRejection.Next), wire.GitPushRejection.Next)
	}
	for _, opt := range wire.GitPushRejection.Next {
		if _, ok := wantNames[opt.Name]; !ok {
			t.Errorf("unexpected option name %q", opt.Name)
		}
		wantNames[opt.Name] = true
		if opt.Command == "" {
			t.Errorf("option %q carries no command", opt.Name)
		}
		if strings.Contains(opt.Command, "git push") && opt.Name != "replace-remote" {
			t.Errorf("option %q should not itself be a push command: %s", opt.Name, opt.Command)
		}
	}
	for name, seen := range wantNames {
		if !seen {
			t.Errorf("missing option %q", name)
		}
	}
	if !strings.Contains(wire.Suggestion, "never force-push") && !strings.Contains(wire.Suggestion, "never force-push or merge") {
		t.Errorf("suggestion should state zcp never force-pushes on its own: %s", wire.Suggestion)
	}
}
