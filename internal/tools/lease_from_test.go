// Tests for: tools/lease.go + ops/lease_group.go + ops/lease_checkout.go —
// zerops_lease action=acquire from=<branch>: a branch new to origin is cut
// from a base branch the agent names, single or group, instead of main; a
// base origin lacks is refused, and an existing branch is never rewritten
// (docs/spec-workflows.md §8 DP-6, DP-17).
package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// localGitSSH runs a lease's checkout in a local directory per alias against
// a local bare origin, standing in for the container: the command's
// /var/www is the alias's directory, and leaseTestRepo is origin. Every other
// command answers nothing.
type localGitSSH struct {
	t      *testing.T
	origin string
	mu     sync.Mutex
	dirs   map[string]string
}

func (s *localGitSSH) dir(alias string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirs == nil {
		s.dirs = map[string]string{}
	}
	if s.dirs[alias] == "" {
		s.dirs[alias] = s.t.TempDir()
	}
	return s.dirs[alias]
}

func (s *localGitSSH) ExecSSH(ctx context.Context, alias, command string) ([]byte, error) {
	if !strings.Contains(command, "fetch -q --prune origin") {
		return nil, nil
	}
	command = strings.ReplaceAll(command, "'/var/www'", "'"+s.dir(alias)+"'")
	command = strings.ReplaceAll(command, "'"+leaseTestRepo+"'", "'"+s.origin+"'")
	cmd := exec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec // running the composed checkout is the test
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, &platform.SSHExecError{Hostname: alias, Output: string(out), Err: err}
	}
	return out, nil
}

func (s *localGitSSH) ExecSSHStdin(ctx context.Context, alias, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, alias, command)
}

func (s *localGitSSH) ExecSSHBackground(context.Context, string, string, time.Duration) ([]byte, error) {
	return nil, nil
}

// head is the commit alias's checkout is on.
func (s *localGitSSH) head(alias string) string {
	return fromGit(s.t, s.dir(alias), "rev-parse", "HEAD")
}

func fromGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // a test's own git
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// originWithABase is a bare origin holding main, pool-previews — main and one
// commit more, the branch the agent bases new work on — and feat, a branch
// of its own; it answers the origin and each branch's commit.
func originWithABase(t *testing.T) (origin, mainSHA, basedSHA, featSHA string) {
	t.Helper()
	origin = filepath.Join(t.TempDir(), "origin.git")
	fromGit(t, t.TempDir(), "init", "-q", "--bare", "-b", "main", origin)
	work := t.TempDir()
	fromGit(t, work, "init", "-q", "-b", "main")
	commit := func(file string) string {
		if err := os.WriteFile(filepath.Join(work, file), []byte(file), 0o600); err != nil {
			t.Fatal(err)
		}
		fromGit(t, work, "add", "-A")
		fromGit(t, work, "commit", "-q", "-m", file)
		return fromGit(t, work, "rev-parse", "HEAD")
	}
	mainSHA = commit("README")
	fromGit(t, work, "checkout", "-q", "-b", "pool-previews")
	basedSHA = commit("previews.txt")
	fromGit(t, work, "checkout", "-q", "-b", "feat", "main")
	featSHA = commit("feat.txt")
	fromGit(t, work, "push", "-q", origin, "main", "pool-previews", "feat")
	return origin, mainSHA, basedSHA, featSHA
}

// leaseResult is the leases an acquire answered.
func leaseResult(t *testing.T, text string) []struct {
	Alias, Branch, Note string
} {
	t.Helper()
	var got struct {
		Leases []struct {
			Alias, Branch, Note string
		} `json:"leases"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return got.Leases
}

// TestLeaseTool_Acquire_From_CutsTheNewBranchFromTheBase: a branch origin
// does not have yet is cut from origin/<from>, not main — one lease, or every
// lease of a group from the one commit the first was cut from (live, 2026-10-25:
// an agent cut two branches off an open change's branch by pushing raw refs
// over ssh, since an acquire would have cut them from main).
func TestLeaseTool_Acquire_From_CutsTheNewBranchFromTheBase(t *testing.T) {
	t.Parallel()
	origin, mainSHA, basedSHA, _ := originWithABase(t)

	t.Run("one lease", func(t *testing.T) {
		t.Parallel()
		ssh := &localGitSSH{t: t, origin: origin}
		srv, _ := leaseToolServerSSH(t, &fakeOrchestrator{}, ssh)
		res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "built-in-a-mate", "from": "pool-previews"})
		if text := getTextContent(t, res); res.IsError {
			t.Fatalf("acquire failed: %s", text)
		}
		if got := ssh.head("devpool-3"); got != basedSHA {
			t.Errorf("devpool-3 is on %s, want pool-previews' %s (main is %s)", got, basedSHA, mainSHA)
		}
	})

	t.Run("a group", func(t *testing.T) {
		t.Parallel()
		ssh := &localGitSSH{t: t, origin: origin}
		srv, _, _ := groupLeaseServerSSH(t, ssh)
		res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pools": []string{"hqpool", "webpool"}, "repo": leaseTestRepo, "branch": "build-stamp", "from": "pool-previews"})
		if text := getTextContent(t, res); res.IsError {
			t.Fatalf("group acquire failed: %s", text)
		}
		for _, alias := range []string{"hqpool-3", "webpool-7"} {
			if got := ssh.head(alias); got != basedSHA {
				t.Errorf("%s is on %s, want pool-previews' %s (main is %s)", alias, got, basedSHA, mainSHA)
			}
		}
	})
}

// TestLeaseTool_Acquire_From_BaseNotOnOrigin_Refused: a from origin does not
// have is refused, naming it, and the container goes back: nothing is cut
// from another commit instead.
func TestLeaseTool_Acquire_From_BaseNotOnOrigin_Refused(t *testing.T) {
	t.Parallel()
	origin, _, _, _ := originWithABase(t)
	ssh := &localGitSSH{t: t, origin: origin}
	orch := &fakeOrchestrator{}
	srv, stateDir := leaseToolServerSSH(t, orch, ssh)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "build-stamp", "from": "no-such-base"})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("an acquire from a base origin lacks succeeded: %s", text)
	}
	if !strings.Contains(text, "no-such-base") {
		t.Errorf("the refusal does not name the base: %s", text)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "leases", "devpool-3.json")); err == nil {
		t.Errorf("a lease record stayed after the refused checkout: %s", text)
	}
}

// TestLeaseTool_Acquire_From_ExistingBranch_NeverRewritten: a branch origin
// already has is checked out as it is there whatever from says, and the
// lease says from was not applied.
func TestLeaseTool_Acquire_From_ExistingBranch_NeverRewritten(t *testing.T) {
	t.Parallel()
	origin, _, _, featSHA := originWithABase(t)
	ssh := &localGitSSH{t: t, origin: origin}
	srv, _ := leaseToolServerSSH(t, &fakeOrchestrator{}, ssh)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feat", "from": "pool-previews"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire failed: %s", text)
	}
	if got := ssh.head("devpool-3"); got != featSHA {
		t.Errorf("devpool-3 is on %s, want origin's feat %s, never rewritten", got, featSHA)
	}
	leases := leaseResult(t, text)
	if len(leases) != 1 || !strings.Contains(leases[0].Note, "from") || !strings.Contains(leases[0].Note, "pool-previews") {
		t.Errorf("leases = %+v, want a note saying from=pool-previews was not applied", leases)
	}
}

// TestLeaseTool_Acquire_From_BadInput_Refused: from names a branch a lease
// takes, only on an acquire, never the branch itself and never for main,
// which no acquire cuts; each is refused before any container is taken.
func TestLeaseTool_Acquire_From_BadInput_Refused(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "not a branch name", args: map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feat-y", "from": "--upload-pack=x"}, want: "from"},
		{name: "the branch itself", args: map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feat-y", "from": "feat-y"}, want: "from"},
		{name: "main", args: map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "from": "pool-previews"}, want: "main"},
		{name: "on a status", args: map[string]any{"action": "status", "from": "pool-previews"}, want: "from"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			orch := &fakeOrchestrator{}
			srv, _ := leaseToolServerSSH(t, orch, &leaseStubSSH{})
			res := callTool(t, srv, "zerops_lease", tt.args)
			text := getTextContent(t, res)
			if !res.IsError || !strings.Contains(text, tt.want) {
				t.Errorf("result = %s (error %v), want a refusal naming %q", text, res.IsError, tt.want)
			}
			orch.mu.Lock()
			reserved := orch.reserved
			orch.mu.Unlock()
			if reserved {
				t.Errorf("a container was taken for a refused input")
			}
		})
	}
}
