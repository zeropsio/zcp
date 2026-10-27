// Tests for: several open changes per Mate per repository, one per branch
// (tools/hq_change_open.go, hq_change_description.go, hq_change_target.go)
// — against an HQ that keeps one per branch and against today's, which keeps
// one per repository — run against a fake HQ that serves real git
// (hq_lab_test.go).
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// openFrom is open-change naming a service — a pair or a lease alias — and
// maybe a branch.
func (l *hqLab) openFrom(service, branch, title string) (changeOpenResult, string, bool) {
	l.t.Helper()
	res, typed, err := handleOpenChange(context.Background(), l.hq.srv.Client(), l.rt, l.stateDir, WorkflowInput{Service: service, Branch: branch, Title: title})
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

// pushFromMain cuts branch from main in the pair's checkout, commits files on
// it and pushes it, as the agent's own git does for a new piece of work.
func (l *hqLab) pushFromMain(branch string, files map[string]string) {
	l.t.Helper()
	l.git("checkout", "-q", "main")
	l.pushBranch(branch, files)
}

// leaseOf writes zcp's record of a lease alias <pool>-<number> of the pair's
// repository in HQ on branch, as an acquire leaves it.
func (l *hqLab) leaseOf(pool string, number int, branch string) string {
	l.t.Helper()
	hqc, ok := openHQ(l.hq.srv.Client(), l.rt)
	if !ok {
		l.t.Fatal("the lab's Mate is not linked")
	}
	repo, err := hqc.RepoURL(labApp, "appdev")
	if err != nil {
		l.t.Fatal(err)
	}
	writeHQLeaseRecord(l.t, l.stateDir, pool, number, repo, branch)
	return fmt.Sprintf("%s-%d", pool, number)
}

// branchesOf is the ledger's changes as "#n@branch", by number.
func branchesOf(changes []workflow.HQChange) string {
	out := make([]string, 0, len(changes))
	for _, c := range changes {
		out = append(out, fmt.Sprintf("#%d@%s", c.Number, c.Branch))
	}
	return strings.Join(out, " ")
}

// TestOpenChange_SecondBranchOpensItsOwnChange: on an HQ that keeps an open
// change per branch, a second branch's work opens a change of its own beside
// the first, both kept in the ledger, and nothing says a Mate has one open
// change per repository.
func TestOpenChange_SecondBranchOpensItsOwnChange(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
	if first, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !first.Created || first.PullRequest != 1 {
		t.Fatalf("the first open: %s", text)
	}
	lab.pushFromMain("add-signup", map[string]string{"signup.js": "the signup\n"})

	answer, text, isError := lab.openChangeFrom("add-signup", "Add the signup")

	if isError || !answer.Created || answer.PullRequest != 2 || answer.Branch != "add-signup" {
		t.Fatalf("want change #2 opened on add-signup, got: %s", text)
	}
	if !strings.Contains(answer.Message, `Change #2 is open in "appdev" for the branch "add-signup"`) || strings.Contains(answer.Message, "one open change per repository") {
		t.Errorf("message = %s", answer.Message)
	}
	if got := branchesOf(lab.ledger()); got != "#1@fix-login #2@add-signup" {
		t.Errorf("ledger = %s, want #1@fix-login #2@add-signup", got)
	}
	for _, n := range []int{1, 2} {
		if c := lab.hq.change(n); c == nil || c.State != "open" {
			t.Errorf("change #%d in HQ = %+v, want open", n, c)
		}
	}
}

// TestOpenChange_SameBranchAnswered: opening the branch of a change already
// open answers that change — it stays, its work goes on by pushing to it and
// describing it again — and opens nothing.
func TestOpenChange_SameBranchAnswered(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
	if first, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !first.Created {
		t.Fatalf("the first open: %s", text)
	}

	answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login again")

	if isError || answer.Created || answer.PullRequest != 1 || answer.Branch != "fix-login" {
		t.Fatalf("want change #1 on fix-login answered: %s", text)
	}
	for _, want := range []string{`Change #1 on "fix-login" is already open: it stays`, "`git push origin fix-login`", `action="describe-change"`} {
		if !strings.Contains(answer.Message, want) {
			t.Errorf("message lacks %q: %s", want, answer.Message)
		}
	}
	if strings.Contains(answer.Message, "one open change per repository") {
		t.Errorf("message still says one open change per repository: %s", answer.Message)
	}
	if got := branchesOf(lab.ledger()); got != "#1@fix-login" {
		t.Errorf("ledger = %s, want #1@fix-login", got)
	}
}

// TestOpenChange_FromLeaseAlias: a lease alias names the repository and the
// branch: open-change from it needs no branch, and describe-change by it
// describes that branch's change.
func TestOpenChange_FromLeaseAlias(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	alias := lab.leaseOf("hqpool", 4, "badge")
	lab.pushFromMain("badge", map[string]string{"badge.js": "the badge\n"})

	answer, text, isError := lab.openFrom(alias, "", "Show a test badge")

	if isError || !answer.Created || answer.PullRequest != 1 || answer.Branch != "badge" || answer.Service != alias {
		t.Fatalf("want change #1 opened on badge from %s, got: %s", alias, text)
	}
	if kept := lab.kept(1); kept == nil || kept.Branch != "badge" {
		t.Errorf("the ledger keeps %+v, want change #1 on badge", kept)
	}
	if words, isError := lab.describe(alias, describedWords); isError || !strings.Contains(words, `"described":true`) || !strings.Contains(words, `"pullRequest":1`) {
		t.Errorf("describing by the lease: %s", words)
	}
}

// TestOpenChange_OldHQAnswersTheOpenOne_RecordsNothingForTheAskedBranch: an
// HQ that keeps one open change per repository answers its open change on
// another branch: zcp says today's words, keeps that change on its own
// branch, and records nothing for the branch asked for — a describe of that
// branch finds no change.
func TestOpenChange_OldHQAnswersTheOpenOne_RecordsNothingForTheAskedBranch(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
	if first, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !first.Created {
		t.Fatalf("the first open: %s", text)
	}
	lab.pushFromMain("add-signup", map[string]string{"signup.js": "the signup\n"})

	answer, text, isError := lab.openChangeFrom("add-signup", "Add the signup")

	if isError || answer.Created || answer.PullRequest != 1 || answer.Branch != "fix-login" {
		t.Fatalf("want change #1 on fix-login answered: %s", text)
	}
	if !strings.Contains(answer.Message, "one open change per repository") || !strings.Contains(answer.Message, "fix-login") {
		t.Errorf("the old HQ's answer lacks today's words: %s", answer.Message)
	}
	if got := branchesOf(lab.ledger()); got != "#1@fix-login" {
		t.Errorf("ledger = %s, want only #1@fix-login", got)
	}
	words, isError := lab.describeOn("add-signup")
	if isError || !strings.Contains(words, `"described":false`) || !strings.Contains(words, "add-signup") {
		t.Errorf("describing the branch asked for: %s", words)
	}
	if lab.hq.change(1).Body != "" {
		t.Error("words for add-signup went onto change #1")
	}
}

// TestOpenChange_TooManyOpen_NamesThem: past as many open changes as HQ keeps
// a Mate in a repository, nothing opens, and the refusal names the open ones
// from the Mate's state and what frees one.
func TestOpenChange_TooManyOpen_NamesThem(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	for i := 1; i <= labOpenCap; i++ {
		if answer, text, isError := lab.openChangeFrom(fmt.Sprintf("work-%d", i), fmt.Sprintf("Work %d", i)); isError || !answer.Created {
			t.Fatalf("open #%d: %s", i, text)
		}
	}

	_, text, isError := lab.openChangeFrom("work-6", "Work 6")

	if !isError {
		t.Fatalf("want a refusal, got: %s", text)
	}
	for i := 1; i <= labOpenCap; i++ {
		if want := fmt.Sprintf(`#%d on \"work-%d\"`, i, i); !strings.Contains(text, want) {
			t.Errorf("the refusal does not name %s: %s", want, text)
		}
	}
	for _, want := range []string{"Nothing was opened", "merge or close", `action=\"close-change\"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal lacks %q: %s", want, text)
		}
	}
	if n := len(lab.ledger()); n != labOpenCap {
		t.Errorf("the ledger keeps %d changes, want %d", n, labOpenCap)
	}
}

// TestOpenChange_PairsSharingARepositoryOpenAChangePerBranch: the Kara case —
// four pairs wired to one repository are one repository, so open-change names
// none of them; a second branch then opens its own change on an HQ that keeps
// one per branch, and on today's HQ is answered with the open one and kept
// nowhere.
func TestOpenChange_PairsSharingARepositoryOpenAChangePerBranch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		perBranch bool
		want      string
	}{
		{name: "an HQ that keeps one per branch", perBranch: true, want: "#1@pool-previews #2@mate-test-badge"},
		{name: "an HQ that keeps one per repository", want: "#1@pool-previews"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lab := newHQLab(t)
			if tc.perBranch {
				lab.hq.keepOpenPerBranch()
			}
			lab.wire()
			for _, pool := range []string{"hqpool", "webpool", "boxpool"} {
				if err := workflow.UpsertServiceMeta(lab.stateDir, pool, func(m *workflow.ServiceMeta, _ bool) error {
					m.Hostname, m.Mode, m.Stages = pool, topology.PlanModeStandard, []topology.StageRef{{Hostname: pool + "stage"}}
					m.BootstrapSession, m.BootstrappedAt, m.GitPushState = "test", "2026-10-02", topology.GitPushConfigured
					m.HQ = &workflow.HQRepoRef{AppID: labApp, Repo: "appdev"}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if answer, text, isError := lab.openFrom("", "pool-previews", "Pool previews"); isError || !answer.Created {
				t.Fatalf("the first open, naming no pair: %s", text)
			}

			_, text, isError := lab.openFrom("", "mate-test-badge", "A test badge")

			if isError || strings.Contains(text, "several pairs") {
				t.Fatalf("the second branch was refused: %s", text)
			}
			if got := branchesOf(lab.ledger()); got != tc.want {
				t.Errorf("ledger = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestDescribeChange_ByBranch: with several changes open, describe-change by
// branch describes that one and leaves the others as they are.
func TestDescribeChange_ByBranch(t *testing.T) {
	lab := twoOpenChanges(t)

	text, isError := lab.describeOn("add-signup")

	if isError || !strings.Contains(text, `"described":true`) || !strings.Contains(text, `"pullRequest":2`) {
		t.Fatalf("want change #2 described: %s", text)
	}
	if body := lab.hq.change(2).Body; body != describedWords {
		t.Errorf("change #2's body = %q", body)
	}
	if body := lab.hq.change(1).Body; body != "" {
		t.Errorf("change #1 took words written for #2: %q", body)
	}
}

// TestDescribeChange_SeveralOpenNoBranch_Refused: with several changes open
// and no branch named, nothing is written — the refusal lists each open
// change with the branch that names it.
func TestDescribeChange_SeveralOpenNoBranch_Refused(t *testing.T) {
	lab := twoOpenChanges(t)

	text, isError := lab.describe("", describedWords)

	if !isError {
		t.Fatalf("want a refusal, got: %s", text)
	}
	for _, want := range []string{`branch=\"fix-login\"`, `#1 \"Fix the login\"`, `branch=\"add-signup\"`, `#2 \"Add the signup\"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the refusal lacks %s: %s", want, text)
		}
	}
	for _, n := range []int{1, 2} {
		if calls := lab.hq.callCount(fmt.Sprintf("PATCH /api/mate/changes/appdev/%d", n)); calls != 0 || lab.hq.change(n).Body != "" {
			t.Errorf("change #%d was written %d time(s): %q", n, calls, lab.hq.change(n).Body)
		}
	}
	for _, c := range lab.ledger() {
		if c.Description != nil {
			t.Errorf("change #%d keeps %+v after a refusal", c.Number, c.Description)
		}
	}
}

// twoOpenChanges is a lab on an HQ that keeps an open change per branch, with
// change #1 open on fix-login and #2 on add-signup.
func twoOpenChanges(t *testing.T) *hqLab {
	t.Helper()
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
	if answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !answer.Created {
		t.Fatalf("open fix-login: %s", text)
	}
	lab.pushFromMain("add-signup", map[string]string{"signup.js": "the signup\n"})
	if answer, text, isError := lab.openChangeFrom("add-signup", "Add the signup"); isError || !answer.Created {
		t.Fatalf("open add-signup: %s", text)
	}
	return lab
}

// TestHandedChange_AnotherBranchOpensItsOwnChange: on an HQ that keeps an open
// change per branch, a handed change is answered only for its own branch;
// another branch opens its own change, and the answer names the handed one,
// which stays open on its branch.
func TestHandedChange_AnotherBranchOpensItsOwnChange(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	number := lab.hq.handOver("appdev", "Fix the login", "fix-login", map[string]string{"login.js": "half a fix\n"})

	answer, text, isError := lab.openChangeFrom("other-work", "Other work")

	if isError || !answer.Created || answer.PullRequest == number || answer.Branch != "other-work" {
		t.Fatalf("want a change of its own opened on other-work, got: %s", text)
	}
	if !strings.Contains(answer.Message, fmt.Sprintf("#%d", number)) || !strings.Contains(answer.Message, "fix-login") {
		t.Errorf("the answer does not name handed change #%d on fix-login: %s", number, answer.Message)
	}

	posts := lab.hq.callCount("POST /api/mate/changes")
	again, text, isError := lab.openChangeFrom("fix-login", "Fix the login")
	if isError || again.Created || again.PullRequest != number || again.Branch != "fix-login" {
		t.Errorf("open-change on the handed change's branch = %s, want #%d answered", text, number)
	}
	if lab.hq.callCount("POST /api/mate/changes") != posts {
		t.Error("open-change asked HQ to open a change on the handed change's branch")
	}
	if kept := lab.kept(number); kept == nil || kept.Branch != "fix-login" || kept.Source != workflow.HQChangeFromHanded {
		t.Errorf("the ledger keeps %+v, want handed #%d on fix-login", kept, number)
	}
}

// TestDescribeChange_ALandedChangeNamesTheOneStillOpen: a describe naming no
// branch, while a change the ledger keeps merged meanwhile, writes nothing —
// the words may have been for the merged one — and names the branch of the
// change still open, the call that describes it.
func TestDescribeChange_ALandedChangeNamesTheOneStillOpen(t *testing.T) {
	lab := twoOpenChanges(t)
	lab.hq.mergeChange("appdev", 1)

	text, isError := lab.describe("", describedWords)

	if isError || !strings.Contains(text, "#1 is merged") || !strings.Contains(text, "not written") {
		t.Fatalf("want #1's merge said and nothing written: %s", text)
	}
	if !strings.Contains(text, `branch=\"add-signup\"`) {
		t.Errorf("the answer does not name the open change's branch: %s", text)
	}
	if body := lab.hq.change(2).Body; body != "" {
		t.Errorf("words went onto #2 with no branch named: %q", body)
	}
}
