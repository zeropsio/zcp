// Tests for: tools/hq_change_open.go — the agent opens its change from a
// branch of its own, run against a fake HQ that serves real git
// (hq_lab_test.go).
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// openChangeFrom is zerops_workflow action="open-change" in the lab.
func (l *hqLab) openChangeFrom(branch, title string) (changeOpenResult, string, bool) {
	l.t.Helper()
	res, typed, err := handleOpenChange(context.Background(), l.hq.srv.Client(), l.stateDir, WorkflowInput{Service: "appdev", Branch: branch, Title: title})
	if err != nil {
		l.t.Fatalf("handleOpenChange: %v", err)
	}
	if typed != nil {
		l.t.Error("a handler's typed output must stay nil — machine state rides in the text")
	}
	text := resultText(l.t, res)
	var answer changeOpenResult
	if !res.IsError {
		if err := json.Unmarshal([]byte(text), &answer); err != nil {
			l.t.Fatalf("answer is not JSON: %v\n%s", err, text)
		}
	}
	return answer, text, res.IsError
}

// pushBranch commits the pair's work and pushes it to branch, as the agent's
// own git does.
func (l *hqLab) pushBranch(branch string, files map[string]string) {
	l.t.Helper()
	l.git("checkout", "-q", "-B", branch)
	l.write(files)
	l.git("add", "-A")
	l.git("-c", "user.name=mate", "-c", "user.email=mate@example.invalid", "commit", "-qm", "The work")
	if out, err := l.ssh.ExecSSH(l.t.Context(), "appdev", "cd /var/www && git push -q origin "+branch); err != nil {
		l.t.Fatalf("pushing %s: %v\n%s", branch, err, out)
	}
}

// The agent pushes a branch of its own and opens its change from it: HQ binds
// the change to that branch, the pair records it, and the change can be
// described for the person's review.
func TestOpenChange_OpensTheChangeOnTheAgentsOwnBranch(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.pushBranch("fix-login", map[string]string{"login.js": "the fix\n"})

	answer, text, isError := lab.openChangeFrom("refs/heads/fix-login", "Fix the login")

	if isError || !answer.Created || answer.PullRequest != 1 || answer.Branch != "fix-login" {
		t.Fatalf("want change #1 opened on fix-login, got: %s", text)
	}
	if !strings.Contains(answer.PullRequestURL, "/appdev/") || !strings.Contains(answer.Message, `action="describe-change"`) {
		t.Errorf("the answer names no link or next step: %s", text)
	}
	if got := lab.meta().HQ.Change; got != 1 {
		t.Errorf("the pair records change #%d, want #1", got)
	}
	hqc, _ := openHQ(lab.hq.srv.Client())
	state, err := hqc.Self(t.Context())
	if err != nil || len(state.Changes) != 1 || state.Changes[0].Head == nil || *state.Changes[0].Head != lab.git("rev-parse", "HEAD") {
		t.Fatalf("HQ's change does not hold the pushed branch: %+v (%v)", state.Changes, err)
	}
	if words, isError := lab.describe("appdev", describedWords); isError || !strings.Contains(words, `"described":true`) {
		t.Errorf("the opened change could not be described: %s", words)
	}
}

// A branch HQ keeps for itself, or another Mate's open change holds, opens
// nothing and says what to do instead; so does a call that names no branch.
func TestOpenChange_RefusesABranchThatIsNotTheAgentsOwn(t *testing.T) {
	tests := []struct {
		name, branch string
		want         string
	}{
		{name: "no branch", branch: " ", want: "branch="},
		{name: "main", branch: "main", want: "a branch of your own"},
		{name: "main as a ref", branch: "refs/heads/main", want: "a branch of your own"},
		{name: "a tag", branch: "refs/tags/v1", want: "a branch of your own"},
		{name: "not a branch name", branch: "fix login", want: "not a branch name"},
		{name: "another Mate's change", branch: "shared", want: "another open change holds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.hq.openForAnotherMate("appdev", "shared")

			_, text, isError := lab.openChangeFrom(tt.branch, "Fix the login")

			if !isError || !strings.Contains(text, tt.want) {
				t.Errorf("want a refusal naming %q, got isError=%v: %s", tt.want, isError, text)
			}
			if got := lab.meta().HQ.Change; got != 0 {
				t.Errorf("a refused open recorded change #%d", got)
			}
		})
	}
}

// A Mate has one open change per repository: opening another answers the
// open one, which stays on its own branch, and says so.
func TestOpenChange_TheOpenChangeStays(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	if first, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !first.Created {
		t.Fatalf("the first open: %s", text)
	}

	answer, text, isError := lab.openChangeFrom("add-signup", "Add the signup")

	if isError || answer.Created || answer.PullRequest != 1 || answer.Branch != "fix-login" {
		t.Fatalf("want change #1 on fix-login answered, not opened: %s", text)
	}
	if !strings.Contains(answer.Message, "already open") || !strings.Contains(answer.Message, "fix-login") {
		t.Errorf("the answer does not say the open change stays on its branch: %s", answer.Message)
	}
}
