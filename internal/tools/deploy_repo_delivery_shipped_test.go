package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestRepoDeliveryDivergenceWarning_OnlyWhenTheShippedCommitIsNotOnTheTrackedBranch
// pins when a direct deploy of a push-delivering pair leaves the container
// ahead of the configured repository: only when what it shipped is not on the
// pair's tracked branch of origin as the source's checkout last saw it. A
// clean commit on that branch warns nothing; a commit ahead of it, uncommitted
// changes on top, a branch the checkout never saw, no commit, or a checkout
// zcp cannot read keep the warning.
func TestRepoDeliveryDivergenceWarning_OnlyWhenTheShippedCommitIsNotOnTheTrackedBranch(t *testing.T) {
	t.Parallel()
	repo := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "pushed")
	pushed := run("rev-parse", "HEAD")
	run("update-ref", "refs/remotes/origin/main", pushed)
	run("commit", "-q", "--allow-empty", "-m", "not pushed")
	ahead := run("rev-parse", "HEAD")
	run("update-ref", "refs/remotes/origin/feat", ahead)

	tests := []struct {
		name       string
		trackedRef string
		shipped    deployShipped
		wantWarn   bool
	}{
		{name: "a clean commit on the tracked branch", shipped: deployShipped{sha: pushed, repo: git.LocalRunner{}, dir: repo}},
		{name: "the tracked ref recorded, not HEAD's branch", trackedRef: "feat", shipped: deployShipped{sha: ahead, repo: git.LocalRunner{}, dir: repo}},
		{name: "a commit ahead of the tracked branch", shipped: deployShipped{sha: ahead, repo: git.LocalRunner{}, dir: repo}, wantWarn: true},
		{name: "uncommitted changes on top", shipped: deployShipped{sha: pushed, dirty: true, repo: git.LocalRunner{}, dir: repo}, wantWarn: true},
		{name: "a tracked branch the checkout never saw", trackedRef: "release", shipped: deployShipped{sha: pushed, repo: git.LocalRunner{}, dir: repo}, wantWarn: true},
		{name: "no commit", shipped: deployShipped{repo: git.LocalRunner{}, dir: repo}, wantWarn: true},
		{name: "no checkout zcp can read", shipped: deployShipped{sha: pushed}, wantWarn: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			seedL1Meta(t, stateDir, "appdev", topology.PlanModeStandard, "appstage", topology.BuildIntegrationNone)
			if tt.trackedRef != "" {
				if err := workflow.UpdateServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta) error {
					m.TrackedRef = tt.trackedRef
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			warn := repoDeliveryDivergenceWarning(context.Background(), stateDir, "appstage", tt.shipped)
			if got := warn != ""; got != tt.wantWarn {
				t.Fatalf("warning = %q, want a warning %v", warn, tt.wantWarn)
			}
			if tt.wantWarn && !strings.Contains(warn, `strategy="git-push"`) {
				t.Errorf("warning %q does not name the reconcile push", warn)
			}
		})
	}
}
