// Tests for: ops/git/remote_branch.go — whether a commit is on a remote's
// branch as a checkout last saw it, against a real local git repository.
package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
)

// TestCommitOnRemoteBranch_ReadsTheCheckoutsOwnRemoteTrackingRef pins the check
// behind a direct deploy's container-ahead warning: a commit is on the
// remote's branch when the checkout's refs/remotes/<remote>/<branch> is that
// commit or a descendant of it — read from the checkout alone, never fetched,
// so a branch the checkout never saw, or a commit it cannot name, reads off.
func TestCommitOnRemoteBranch_ReadsTheCheckoutsOwnRemoteTrackingRef(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	commits := make([]string, 0, 3)
	for i := range 3 {
		git("commit", "-q", "--allow-empty", "-m", fmt.Sprintf("c%d", i))
		commits = append(commits, git("rev-parse", "HEAD"))
	}
	git("update-ref", "refs/remotes/origin/main", commits[1])

	r := &recordingRunner{}
	tests := []struct {
		name, branch, sha string
		want              bool
	}{
		{name: "an ancestor of the remote's branch", branch: "main", sha: commits[0], want: true},
		{name: "the remote's branch itself", branch: "main", sha: commits[1], want: true},
		{name: "ahead of the remote's branch", branch: "main", sha: commits[2]},
		{name: "a branch the checkout never saw", branch: "feature", sha: commits[0]},
		{name: "a commit the checkout cannot name", branch: "main", sha: "0123456789abcdef0123456789abcdef01234567"},
		{name: "no commit", branch: "main", sha: ""},
	}
	for _, tt := range tests {
		if got := CommitOnRemoteBranch(context.Background(), r, dir, "origin", tt.branch, tt.sha); got != tt.want {
			t.Errorf("%s: CommitOnRemoteBranch = %v, want %v", tt.name, got, tt.want)
		}
	}
	for _, script := range r.scripts() {
		if strings.Contains(script, "fetch") || strings.Contains(script, "ls-remote") {
			t.Errorf("the check reached the remote: %s", script)
		}
	}
}

// recordingRunner is LocalRunner, recording every script it runs.
type recordingRunner struct {
	mu   sync.Mutex
	runs []string
}

func (r *recordingRunner) Run(ctx context.Context, dir, script string) (string, string, error) {
	r.mu.Lock()
	r.runs = append(r.runs, script)
	r.mu.Unlock()
	return LocalRunner{}.Run(ctx, dir, script)
}

func (r *recordingRunner) scripts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.runs)
}
