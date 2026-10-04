// Tests for: tools/hq_git_push.go — a git-push of a wired pair to this
// Mate's HQ goes to its change, run against a fake HQ that serves real git
// (hq_lab_test.go).
package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/workflow"
)

// gitPushTool is zerops_deploy over the lab.
func (l *hqLab) gitPushTool() *mcp.Server {
	l.t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, l.mock, l.hq.srv.Client(), labMate, l.ssh,
		&auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}, nil,
		l.rt, l.stateDir, testDeployEngine(l.t), nil)
	return srv
}

// commit commits the pair's whole tree under message.
func (l *hqLab) commit(message string) {
	l.t.Helper()
	l.git("add", "-A")
	l.git("-c", "user.name=mate", "-c", "user.email=mate@example.invalid", "commit", "-qm", message)
}

// TestGitPushToHQ_DeliversCommittedWorkAsTheChange: a push to HQ is a
// delivery of the committed work. HQ takes it only on the change's branch, so
// the push takes main in, opens the change and pushes there; nothing builds
// from it, so no build is watched and no integration offered, and nothing is
// recorded as a deploy.
func TestGitPushToHQ_DeliversCommittedWorkAsTheChange(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	ws := workflow.NewWorkSession(labMate, string(workflow.EnvContainer), "Add a due date to each todo.", []string{"appdev"})
	if err := workflow.SaveWorkSession(lab.stateDir, ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(lab.stateDir, os.Getpid()) })
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.commit("Add a due date")
	srv := lab.gitPushTool()

	text := getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "appdev", "strategy": "git-push"}))
	if !strings.Contains(text, `"status":"PUSHED"`) || !strings.Contains(text, `"pullRequest"`) || !strings.Contains(text, `"branch":"mate/p-mate/1"`) {
		t.Fatalf("the push must land on change #1:\n%s", text)
	}
	if change := lab.hq.change(1); change == nil || change.Title != "Add a due date to each todo." {
		t.Fatalf("change #1 = %+v, want it titled with the task", change)
	}
	if head := lab.remoteHead("mate/p-mate/1"); head != lab.git("rev-parse", "HEAD") {
		t.Errorf("change #1's branch is at %q, want the checkout's HEAD", head)
	}
	for _, never := range []string{"build-integration", "NOT_OBSERVED", "pull request"} {
		if strings.Contains(text, never) {
			t.Errorf("a push to HQ must not say %q:\n%s", never, text)
		}
	}
	if !strings.Contains(text, "merges it") {
		t.Errorf("the push must say the person merges the change:\n%s", text)
	}
	if !strings.Contains(text, "may merge it at any moment") || !strings.Contains(text, "next stage deploy folds the merge in") {
		t.Errorf("the push must prepare the agent for a merge while it still works:\n%s", text)
	}
	if loaded, err := workflow.LoadWorkSession(lab.stateDir, os.Getpid()); err == nil && len(loaded.Deploys["appdev"]) != 0 {
		t.Errorf("a push to HQ is no deploy, recorded %+v", loaded.Deploys["appdev"])
	}

	// The same HEAD again: nothing to push, the change found, not doubled.
	text = getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "appdev", "strategy": "git-push"}))
	if !strings.Contains(text, `"status":"NOTHING_TO_PUSH"`) || lab.hq.change(2) != nil {
		t.Errorf("a second push of the same HEAD must find change #1 and push nothing:\n%s", text)
	}
}

// TestGitPushToHQ_HQNotAnsweringFailsFast: a push HQ cannot serve fails as
// the tool's error within the call, after three tries, saying the step, HQ's
// address and the way on; the commit stays in the checkout, and pushing again
// once HQ answers delivers it.
func TestGitPushToHQ_HQNotAnsweringFailsFast(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.commit("the app")
	srv := lab.gitPushTool()

	lab.hq.setDown(true)
	result := callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "appdev", "strategy": "git-push"})
	text := getTextContent(t, result)
	if !result.IsError {
		t.Fatalf("a push HQ could not serve must be the tool's error:\n%s", text)
	}
	for _, want := range []string{
		"git-push from appdev has not reached HQ",
		"HQ at " + lab.hq.srv.URL + ` could not be reached to take \"main\" in (3 tries; the last: `,
		"the work stays committed in appdev's checkout, and pushing again delivers it",
		"tell the person HQ is not answering",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the error misses %q:\n%s", want, text)
		}
	}

	lab.hq.setDown(false)
	text = getTextContent(t, callTool(t, srv, "zerops_deploy", map[string]any{"targetService": "appdev", "strategy": "git-push"}))
	if !strings.Contains(text, `"status":"PUSHED"`) || lab.remoteHead("mate/p-mate/1") != lab.git("rev-parse", "HEAD") {
		t.Errorf("pushing again once HQ answers must deliver the commit:\n%s", text)
	}
}

// TestGitPushToHQ_ABranchOfItsOwnIsRefused: HQ takes the Mate's push only on
// its change's branch, and main moves only by HQ's merge — refused before git
// runs, so a rejection can never read as a reason to force-push.
func TestGitPushToHQ_ABranchOfItsOwnIsRefused(t *testing.T) {
	for _, branch := range []string{"main", "feature"} {
		t.Run(branch, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			lab.write(map[string]string{"index.js": "the app\n"})
			lab.commit("the app")

			text := getTextContent(t, callTool(t, lab.gitPushTool(), "zerops_deploy", map[string]any{
				"targetService": "appdev", "strategy": "git-push", "branch": branch,
			}))
			if !strings.Contains(text, "did not run") || !strings.Contains(text, "Push without a branch") {
				t.Errorf("the push to %q must be refused, naming the way:\n%s", branch, text)
			}
			if strings.Contains(text, "replace-remote") || strings.Contains(text, "--force") {
				t.Errorf("a refusal never offers a force:\n%s", text)
			}
			if lab.hq.change(1) != nil || lab.remoteHead(branch) != "" && branch != "main" {
				t.Errorf("nothing may be opened or pushed")
			}
		})
	}
}
