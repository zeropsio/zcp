// Tests for: tools/deploy_git_push.go — a git-push to this Mate's HQ is
// refused for plain git, run against a fake HQ that serves real git
// (hq_lab_test.go).
package tools

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
)

// gitPushTool is zerops_deploy over the lab.
func (l *hqLab) gitPushTool() *mcp.Server {
	l.t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, l.mock, l.hq.srv.Client(), labMate, l.ssh,
		&auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}, nil,
		l.rt, l.stateDir, testDeployEngine(l.t), nil, DevsupSource{})
	return srv
}

// commit commits the pair's whole tree under message.
func (l *hqLab) commit(message string) {
	l.t.Helper()
	l.git("add", "-A")
	l.git("-c", "user.name=mate", "-c", "user.email=mate@example.invalid", "commit", "-qm", message)
}

// TestGitPushToHQ_RefusedForPlainGitOnABranch: a Mate's code reaches HQ by
// plain git on a branch of the agent's own, and main moves only when a person
// merges — so a git-push deploy to HQ, which would push the tracked branch,
// runs nothing, opens nothing, and says the way: a branch, never main, never
// a force.
func TestGitPushToHQ_RefusedForPlainGitOnABranch(t *testing.T) {
	t.Parallel()
	for _, branch := range []string{"", "main", "feature"} {
		t.Run("branch="+branch, func(t *testing.T) {
			t.Parallel()
			lab := newHQLab(t)
			lab.wire()
			lab.write(map[string]string{"index.js": "the app\n"})
			lab.commit("the app")
			mainBefore := lab.remoteHead("main")

			args := map[string]any{"targetService": "appdev", "strategy": "git-push"}
			if branch != "" {
				args["branch"] = branch
			}
			text := getTextContent(t, callTool(t, lab.gitPushTool(), "zerops_deploy", args))
			for _, want := range []string{"did not run", "a branch of your own", "never force-push"} {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal must say %q:\n%s", want, text)
				}
			}
			if strings.Contains(text, "replace-remote") || strings.Contains(text, "--force") {
				t.Errorf("a refusal never offers a force:\n%s", text)
			}
			if lab.hq.change(1) != nil || lab.remoteHead("main") != mainBefore || lab.remoteHead("feature") != "" {
				t.Errorf("nothing may be opened or pushed")
			}
		})
	}
}

// TestGitPushFromAPairWiredToHQ_RefusedWhateverTheRemote: the dev service of a
// pair wired to HQ holds the Mate's HQ credential as GIT_TOKEN, so a git-push
// deploy to any other remote — GitHub, say — would hand it to a host it is not
// for. It runs nothing there and says the plain-git way.
func TestGitPushFromAPairWiredToHQ_RefusedWhateverTheRemote(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.commit("the app")
	ran := len(lab.ssh.commands)

	text := getTextContent(t, callTool(t, lab.gitPushTool(), "zerops_deploy", map[string]any{
		"targetService": "appdev", "strategy": "git-push", "remoteUrl": "https://github.com/acme/app",
	}))

	if !strings.Contains(text, "did not run") || !strings.Contains(text, "a branch of your own") {
		t.Errorf("want the plain-git refusal:\n%s", text)
	}
	for _, command := range lab.ssh.commands[ran:] {
		if strings.Contains(command, "github.com") || strings.Contains(command, "GIT_TOKEN") {
			t.Errorf("the dev service ran %q for another remote", command)
		}
	}
}
