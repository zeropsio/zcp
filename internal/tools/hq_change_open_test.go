// Tests for: tools/hq_change_open.go — the agent opens its change from a
// branch of its own, run against a fake HQ that serves real git
// (hq_lab_test.go).
package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
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
	if kept := lab.kept(1); kept == nil || kept.Branch != "fix-login" {
		t.Errorf("the ledger keeps %+v, want change #1 on fix-login", kept)
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

// A branch HQ keeps for itself, one HQ cannot name, or one another Mate's
// open change holds, opens nothing and says what to do instead; so does a
// call that names no branch.
func TestOpenChange_RefusesABranchThatIsNotTheAgentsOwn(t *testing.T) {
	tests := []struct {
		name, branch string
		// refusal, when set, is the {code, reason} HQ answers the open with.
		refusal *hq.RefusedError
		want    string
	}{
		{name: "no branch", branch: " ", want: "branch="},
		{name: "main", branch: "main", want: "a branch of your own"},
		{name: "main as a ref", branch: "refs/heads/main", want: "a branch of your own"},
		{name: "a tag", branch: "refs/tags/v1", want: "a branch of your own"},
		{name: "HQ's mate/ namespace", branch: "mate/fix-login", want: "a branch of your own"},
		{name: "not a branch name", branch: "fix login", want: "not a branch name"},
		{name: "a name HQ cannot take", branch: "fix-login",
			refusal: &hq.RefusedError{Status: http.StatusBadRequest, Code: "invalid", Reason: "invalid_ref"}, want: "not a branch name"},
		{name: "another Mate's change", branch: "shared", want: "another open change holds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.hq.openForAnotherMate("appdev", "shared")
			if tt.refusal != nil {
				lab.hq.refuseWith(tt.refusal.Status, tt.refusal.Code, tt.refusal.Reason, 1, func(r *http.Request) bool {
					return r.Method == http.MethodPost && r.URL.Path == "/api/mate/changes"
				})
			}

			_, text, isError := lab.openChangeFrom(tt.branch, "Fix the login")

			if !isError || !strings.Contains(text, tt.want) {
				t.Errorf("want a refusal naming %q, got isError=%v: %s", tt.want, isError, text)
			}
			if kept := lab.ledger(); len(kept) != 0 {
				t.Errorf("a refused open recorded %+v", kept)
			}
		})
	}
}

// On an HQ that keeps one open change per repository, opening another answers
// the open one, which stays on its own branch, and says so; nothing is kept
// for the branch asked for.
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
	for _, c := range lab.ledger() {
		if c.Branch == "add-signup" {
			t.Errorf("the ledger keeps change #%d for the branch asked for", c.Number)
		}
	}

	// An HQ that does not name the open change's branch leaves it unnamed,
	// never the branch just asked for.
	lab.hq.mu.Lock()
	lab.hq.unnamedBranches = true
	lab.hq.mu.Unlock()
	answer, text, isError = lab.openChangeFrom("add-signup", "Add the signup")
	if isError || answer.Created || answer.PullRequest != 1 || answer.Branch != "" || strings.Contains(answer.Message, "add-signup") {
		t.Errorf("want change #1 answered with no branch named, got: %s", text)
	}
}

// The agent cuts new work from main while the Mate's change is open: the
// answer says the work goes on the open change's branch and how to take it,
// and the work taken and pushed as it says reaches the change by
// fast-forward — never a push HQ refuses.
func TestOpenChange_AnOpenChangeTakesTheWorkOntoItsBranch(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.pushBranch("fix-login", map[string]string{"login.js": "the fix\n"})
	if first, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !first.Created {
		t.Fatalf("the first open: %s", text)
	}
	before := lab.remoteHead("fix-login")
	lab.git("checkout", "-q", "-b", "add-signup", "main")

	answer, text, isError := lab.openChangeFrom("add-signup", "Add the signup")

	if isError || answer.Created || answer.Branch != "fix-login" {
		t.Fatalf("want change #1 on fix-login answered: %s", text)
	}
	take, push := takeCommand.FindStringSubmatch(answer.Message), pushCommand.FindStringSubmatch(answer.Message)
	if take == nil || push == nil {
		t.Fatalf("the answer gives no command that takes the open change's branch, or pushes it: %s", answer.Message)
	}
	if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && "+take[1]); err != nil {
		t.Fatalf("taking the branch as the answer says (%s): %v\n%s", take[1], err, out)
	}
	lab.write(map[string]string{"signup.js": "the signup\n"})
	lab.commit("Add the signup")
	if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && "+push[1]); err != nil {
		t.Fatalf("pushing as the answer says (%s): %v\n%s", push[1], err, out)
	}
	if got, want := lab.remoteHead("fix-login"), lab.git("rev-parse", "HEAD"); got != want {
		t.Errorf("fix-login is at %q, want the work pushed onto it %q", got, want)
	}
	lab.git("merge-base", "--is-ancestor", before, "HEAD")
}

// TestOpenChange_ForgetsTheChangesTheStateItReadNamesSettled: on an HQ that
// keeps one open change per repository, change #1 merges and the agent opens
// #2 before any pass learned it. open-change forgets #1 by the Mate's state it
// read anyway, and says the merge once, so a describe naming no branch then
// puts its words on #2 at the first call.
func TestOpenChange_ForgetsTheChangesTheStateItReadNamesSettled(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
	if first, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !first.Created {
		t.Fatalf("the first open: %s", text)
	}
	lab.hq.mergeChange("appdev", 1)
	lab.pushFromMain("add-signup", map[string]string{"signup.js": "the signup\n"})

	answer, text, isError := lab.openChangeFrom("add-signup", "Add the signup")

	if isError || !answer.Created || answer.PullRequest != 2 {
		t.Fatalf("want change #2 opened: %s", text)
	}
	if lab.kept(1) != nil {
		t.Error("open-change kept merged #1, which the Mate's state it read names merged")
	}
	if !strings.Contains(text, "#1 is merged") {
		t.Errorf("open-change does not say #1 merged, which no pass said yet: %s", text)
	}
	if described, isError := lab.describe("appdev", describedWords); isError || !strings.Contains(described, `"described":true`) {
		t.Errorf("a describe naming no branch did not put its words on #2: %s", described)
	}
}

// An HQ from before changes were bound to a branch they name ignores the
// branch asked for and binds the change to the Mate's own: the answer names
// that branch and says the work goes there, never the one asked for.
func TestOpenChange_AnHQBeforeNamedBranchesNamesTheBranchItBound(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.hq.mu.Lock()
	lab.hq.unnamedBranches = true
	lab.hq.mu.Unlock()
	lab.pushBranch("fix-signup", map[string]string{"signup.js": "the signup\n"})

	answer, text, isError := lab.openChangeFrom("fix-signup", "Fix the signup")

	const bound = "mate/" + labMate + "/1"
	if isError || !answer.Created || answer.Branch != bound {
		t.Fatalf("want change #1 opened on %q, got: %s", bound, text)
	}
	if kept := lab.kept(1); kept == nil || kept.Branch != bound {
		t.Errorf("the ledger keeps %+v, want change #1 on %q", kept, bound)
	}
	push := pushCommand.FindStringSubmatch(answer.Message)
	if push == nil {
		t.Fatalf("the answer gives no push: %s", answer.Message)
	}
	if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && "+push[1]); err != nil {
		t.Fatalf("pushing as the answer says (%s): %v\n%s", push[1], err, out)
	}
	if got, want := lab.remoteHead(bound), lab.git("rev-parse", "fix-signup"); got != want {
		t.Errorf("%s is at %q, want the work %q", bound, got, want)
	}
}

// In a repository whose code is kept on GitHub, the change's pull request is
// HQ's: the answer says HQ opens it on GitHub from the agent's branch and the
// person merges it in Mate, and records the way the code is kept. Where HQ
// keeps the code, the answer is as it always was.
func TestOpenChange_InARepositoryKeptOnGitHubThePullRequestIsHQs(t *testing.T) {
	const github = "https://github.com/acme/app"
	for _, tc := range []struct {
		name    string
		origin  *hq.RepoOrigin
		message string
		want    []string
	}{
		{
			name: "kept on GitHub", origin: &hq.RepoOrigin{URL: github, Mode: hq.OriginOnGitHub},
			want: []string{
				`Change #1 is open in "appdev" for the branch "fix-login"`,
				"HQ opens its pull request on GitHub from that branch",
				"the person merges it in Mate",
				"`git push origin fix-login`",
				`action="describe-change"`,
			},
		},
		{
			name:    "kept in HQ",
			message: "Change #1 is open in \"appdev\" for the branch \"fix-login\": push the work there (`git push origin fix-login`) — never force-push — and write what it does and how you checked it with zerops_workflow action=\"describe-change\"; the person reviews it by that.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			if tc.origin != nil {
				lab.hq.keepOn(*tc.origin)
			}
			lab.pushBranch("fix-login", map[string]string{"login.js": "the fix\n"})

			answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login")

			if isError || !answer.Created {
				t.Fatalf("want change #1 opened: %s", text)
			}
			if tc.message != "" && answer.Message != tc.message {
				t.Errorf("message = %q, want %q", answer.Message, tc.message)
			}
			for _, want := range tc.want {
				if !strings.Contains(answer.Message, want) {
					t.Errorf("message lacks %q: %s", want, answer.Message)
				}
			}
			if tc.origin != nil {
				if got := lab.meta().HQ.Origin; got == nil || got.Mode != tc.origin.Mode {
					t.Errorf("the pair's record of how its code is kept = %+v, want %q", got, tc.origin.Mode)
				}
			}
		})
	}
}
