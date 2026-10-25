// Tests for: the read-only `git status` probes tools/ runs on a dev service —
// launch's push proof, git-push-setup's porcelain summary and a git-push
// deploy's dirty check — each run with GIT_OPTIONAL_LOCKS=0, as every other
// status zcp runs on a dev service does (docs/spec-workflows.md §8 DM-7).
package tools

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// optionalLocksOffStatus is a `git status` that cannot refresh and rewrite
// the checkout's index.
var optionalLocksOffStatus = regexp.MustCompile(`GIT_OPTIONAL_LOCKS=0 git(?: -C '[^']*')? status --porcelain`)

func TestToolStatusProbes_RunWithOptionalLocksOff(t *testing.T) {
	t.Parallel()
	ssh := &containerSSHStub{}
	if _, err := readLaunchPushProofContainer(context.Background(), ssh, "appdev", "https://github.com/example/app.git", "main"); err != nil {
		t.Fatalf("push proof: %v", err)
	}
	_ = gitPushPorcelainSummary(context.Background(), ssh, "appdev", "/var/www")
	commands := append(slices.Clone(ssh.commands), gitStatusPorcelainCmd("/var/www"))

	statuses := 0
	for _, cmd := range commands {
		n := strings.Count(cmd, "status --porcelain")
		statuses += n
		if got := len(optionalLocksOffStatus.FindAllString(cmd, -1)); got != n {
			t.Errorf("a git status without GIT_OPTIONAL_LOCKS=0: %s", cmd)
		}
	}
	if statuses != 3 {
		t.Fatalf("read %d git status probes, want the push proof's, the porcelain summary's and the dirty check's: %q", statuses, commands)
	}
}
