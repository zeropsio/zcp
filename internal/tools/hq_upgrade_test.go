// Tests for: a pair that takes this zcp mid-change from the released one —
// which recorded the Mate's local branch mate/<id> where this zcp records
// the change's branch, HQ binding the change to mate/<id>/<n>
// (tools/hq_change.go upgradeReleasedBranch, hqOpenChangeWork), run against a
// fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// releasedOpenChange leaves the pair as the released zcp left it mid-change:
// the work on the Mate's local branch mate/<id>, its change opened naming no
// branch and the work pushed to the change's branch, the record naming the
// local branch and no way the code is kept.
func (l *hqLab) releasedOpenChange() hq.Change {
	l.t.Helper()
	l.git("checkout", "-q", "-b", "mate/"+labMate)
	l.write(map[string]string{"login.js": "half a fix\n"})
	change := l.openChange()
	if err := workflow.UpsertServiceMeta(l.stateDir, "appdev", func(meta *workflow.ServiceMeta, _ bool) error {
		meta.HQ.Change, meta.HQ.Branch, meta.HQ.Origin = change.Number, "mate/"+labMate, nil
		return nil
	}); err != nil {
		l.t.Fatal(err)
	}
	return change
}

// renameCommand and pushCommand are the git commands the words give to take
// a branch an older zcp kept the work on, and to push it.
var (
	renameCommand = regexp.MustCompile("`(git branch -m [^`]*)`")
	pushCommand   = regexp.MustCompile("`(git push origin [^`]*)`")
)

// TestUpgrade_AnOpenChangeTheReleasedZcpRecordedKeepsStatusAndPushWorking: a
// plain HQ-only Mate holding an open change the released zcp recorded takes
// this zcp. Status names the change's branch, never the Mate's local one HQ
// refuses; develop's start says the work goes on there rather than on a
// branch cut from main, with commands that work on that checkout, and the
// work pushed as it says reaches the change by fast-forward. open-change
// from a branch cut from main answers the change with the same way onto it.
func TestUpgrade_AnOpenChangeTheReleasedZcpRecordedKeepsStatusAndPushWorking(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.mock.WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})
	change := lab.releasedOpenChange()
	const branch = "mate/" + labMate + "/1"
	before := lab.remoteHead(branch)

	status := lab.status()

	if !strings.Contains(status, "change=1") || !strings.Contains(status, "changeBranch="+branch) {
		t.Errorf("status does not name change #1 on %q:\n%s", branch, status)
	}
	if got := lab.meta().HQ.Branch; got != branch {
		t.Errorf("the pair records the change on %q, want %q", got, branch)
	}

	text := lab.developStart()

	if !strings.Contains(text, "change #1") || !strings.Contains(text, branch) || !strings.Contains(text, "rather than on a branch cut from") {
		t.Fatalf("develop's start does not say the work goes on change #1's branch %q:\n%s", branch, text)
	}
	_, words, found := strings.Cut(text, "change #1 in")
	if !found {
		t.Fatalf("develop's start names no change #1 in a repository:\n%s", text)
	}
	rename, push := renameCommand.FindStringSubmatch(words), pushCommand.FindStringSubmatch(words)
	if rename == nil || push == nil {
		t.Fatalf("the words give no command that takes the branch on this checkout, or pushes it:\n%s", text)
	}
	if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && "+rename[1]); err != nil {
		t.Fatalf("taking the branch as the words say (%s): %v\n%s", rename[1], err, out)
	}
	lab.write(map[string]string{"login.js": "the whole fix\n"})
	lab.commit("Carry the fix on")
	if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && "+push[1]); err != nil {
		t.Fatalf("pushing as the words say (%s): %v\n%s", push[1], err, out)
	}
	if got, want := lab.remoteHead(branch), lab.git("rev-parse", "HEAD"); got != want {
		t.Errorf("change #1's branch is at %q, want the carried-on work %q", got, want)
	}
	// The carried-on work builds on what the released zcp pushed.
	lab.git("merge-base", "--is-ancestor", before, "HEAD")
	if c := lab.hq.change(change.Number); c == nil || c.State != hq.ChangeOpen {
		t.Errorf("change #1 is not open: %+v", c)
	}

	lab.git("checkout", "-q", "-b", "fix-signup", "main")
	answer, answered, isError := lab.openChangeFrom("fix-signup", "Fix the signup")
	if isError || answer.Created || answer.PullRequest != 1 || answer.Branch != branch {
		t.Fatalf("open-change = %s, want change #1 on %q answered", answered, branch)
	}
	if !strings.Contains(answer.Message, "git fetch origin "+branch) {
		t.Errorf("open-change does not say how to take %q: %s", branch, answer.Message)
	}
}
