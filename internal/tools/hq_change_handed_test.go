// Tests for: a change a person hands this Mate from a Mate that is gone
// (tools/hq_change.go hqTakeHanded, hq_change_open.go, hq_recipe.go), run
// against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// status is zerops_workflow action="status" in the lab.
func (l *hqLab) status() string {
	l.t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, l.mock, l.hq.srv.Client(), labMate, nil,
		workflow.NewEngine(l.stateDir, workflow.EnvContainer, nil), nil, l.stateDir, "", nil, l.ssh, l.rt, "")
	return getTextContent(l.t, callTool(l.t, srv, "zerops_workflow", map[string]any{"action": "status"}))
}

// takeCommand is the git command the words say takes the change's branch.
var takeCommand = regexp.MustCompile("`(git fetch [^`]*)`")

// TestHandedChange_IsThePairsAndTheWorkGoesOnItsBranch: a change a person
// hands this Mate is its repository's change from the next pass on — kept in
// the ledger with its branch and said once with how to take that branch. Its
// branch is where the work goes, whether its Mate named it or it is that
// Mate's default, pushed to or not yet: the agent takes it as the words say
// and pushes there, and on an HQ that keeps one open change per repository
// open-change answers the handed change without asking HQ to open another.
func TestHandedChange_IsThePairsAndTheWorkGoesOnItsBranch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		branch string // "" is the gone Mate's default, mate/p-gone/<n>
		work   map[string]string
		want   string
	}{
		{name: "on a branch its Mate named", branch: "fix-login", work: map[string]string{"login.js": "half a fix\n"}, want: "fix-login"},
		{name: "on its Mate's default branch", work: map[string]string{"login.js": "half a fix\n"}, want: "mate/p-gone/1"},
		{name: "on a branch nothing was pushed to yet", branch: "fix-signup", want: "fix-signup"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.mock.WithServices([]platform.ServiceStack{
				{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
				{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			})
			number := lab.hq.handOver("appdev", "Fix the login", tc.branch, tc.work)

			text := lab.developStart()

			if !strings.Contains(text, "change #1") || !strings.Contains(text, "handed") || !strings.Contains(text, tc.want) {
				t.Fatalf("develop's start does not say change #1 on %q was handed to this Mate:\n%s", tc.want, text)
			}
			if kept := lab.kept(number); kept == nil || kept.Branch != tc.want || kept.Source != workflow.HQChangeFromHanded {
				t.Errorf("the ledger keeps %+v, want handed #%d on %q", kept, number, tc.want)
			}
			if again := lab.developStart(); strings.Contains(again, "handed") {
				t.Errorf("the hand-over is said again on the next start:\n%s", again)
			}
			if status := lab.status(); !strings.Contains(status, "HQ appdev: #1 "+tc.want) {
				t.Errorf("status does not name change #1 on %q:\n%s", tc.want, status)
			}

			answer, answered, isError := lab.openChangeFrom("other-work", "Other work")
			if isError || answer.Created || answer.PullRequest != number || answer.Branch != tc.want || !strings.Contains(answer.Message, tc.want) {
				t.Errorf("open-change = %s, want handed change #%d on %q answered", answered, number, tc.want)
			}
			if n := lab.hq.callCount("POST /api/mate/changes"); n != 0 {
				t.Errorf("open-change asked HQ to open a change %d time(s) while the Mate holds a handed one", n)
			}

			take := takeCommand.FindStringSubmatch(text)
			if take == nil {
				t.Fatalf("the words give no command that takes the branch:\n%s", text)
			}
			if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && "+take[1]); err != nil {
				t.Fatalf("taking the branch as the words say (%s): %v\n%s", take[1], err, out)
			}
			lab.write(map[string]string{"login.js": "the whole fix\n"})
			lab.git("add", "-A")
			lab.git("-c", "user.name=mate", "-c", "user.email=mate@example.invalid", "commit", "-qm", "Carry the fix on")
			if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && git push -q origin "+answer.Branch); err != nil {
				t.Fatalf("pushing to the branch open-change names: %v\n%s", err, out)
			}
			if got, want := lab.remoteHead(tc.want), lab.git("rev-parse", "HEAD"); got != want {
				t.Errorf("the handed change's branch is at %q, want the carried-on work %q", got, want)
			}
			if change := lab.hq.change(number); change == nil || lab.hq.branchHead(t.Context(), change) == nil {
				t.Error("the handed change does not hold the pushed work")
			}
			if tc.work != nil && lab.git("rev-list", "--count", "HEAD", "^main") != "2" {
				t.Error("the carried-on work does not build on the gone Mate's")
			}
		})
	}
}

// TestGroupRecipe_AHandedProposalMovesOnItsOwnBranch: a recipe proposal a
// person hands this Mate from a Mate that is gone is its proposal now, and
// zcp moves it on the branch HQ keeps it on — never one of this Mate's own,
// which HQ binds to no change.
func TestGroupRecipe_AHandedProposalMovesOnItsOwnBranch(t *testing.T) {
	lab := newRecipeLab(t)
	number := lab.hq.handOver(hq.RecipeRepo, hq.RecipeProposalTitle, "", map[string]string{"NOTES.md": "the gone Mate's start\n"})
	handedHead := lab.hq.recipeHead("mate/p-gone/1")

	outcome := lab.proposeRecipe(lab.mock)

	if outcome.Created || !outcome.Committed || outcome.Change != number || outcome.Branch != "mate/p-gone/1" {
		t.Fatalf("outcome = %+v, want handed change #%d moved on mate/p-gone/1", outcome, number)
	}
	if !lab.hq.recipeHolds(handedHead, "mate/p-gone/1") {
		t.Error("the handed proposal's branch did not move forward from the gone Mate's push")
	}
	if got := lab.hq.recipeFiles("mate/p-gone/1"); got["README.md"] == "" {
		t.Errorf("the handed proposal's branch does not carry the recipe: %v", added(lab.hq.recipeFiles("main"), got))
	}
	if own := lab.hq.git(t.Context(), lab.hq.repoDir(labApp, hq.RecipeRepo), "for-each-ref", "--format=%(refname)", "refs/heads/"+recipeBranch(number)); own != "" {
		t.Errorf("zcp pushed %s, a branch no change is bound to", own)
	}
}
