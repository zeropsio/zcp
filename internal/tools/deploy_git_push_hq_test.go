// Tests for: tools/deploy_git_push.go — a git-push to this Mate's HQ is
// refused for plain git, run against a fake HQ that serves real git
// (hq_lab_test.go).
package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
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
			for _, want := range []string{"did not run", "a branch of your own", "never force-push", "`zcp git appdev push origin "} {
				if !strings.Contains(text, want) {
					t.Errorf("the refusal must say %q:\n%s", want, text)
				}
			}
			if strings.Contains(text, "replace-remote") || strings.Contains(text, "--force") || strings.Contains(text, "`git ") {
				t.Errorf("a refusal never offers a force:\n%s", text)
			}
			if lab.hq.change(1) != nil || lab.remoteHead("main") != mainBefore || lab.remoteHead("feature") != "" {
				t.Errorf("nothing may be opened or pushed")
			}
		})
	}
}

// TestGitPushToHQ_PlainProject_KeepsTheCheckoutPushWords: a project without a
// Mate wired to HQ through the project door keeps its git on the mounted
// checkout, reading HQ through `zcp hq git` (owner decision Q1,
// docs/spec-mate.md §6.7) — `zcp git` has no Mate identity to answer it with
// there — so the git-push refusal says the push as it did before `zcp git`,
// byte for byte, and a Mate's names `zcp git`.
func TestGitPushToHQ_PlainProject_KeepsTheCheckoutPushWords(t *testing.T) {
	t.Parallel()
	lab := newHQLab(t)
	lab.rt.MateEnabled = false
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(meta *workflow.ServiceMeta) error {
		meta.GitPushState = topology.GitPushConfigured
		meta.RemoteURL = lab.hq.srv.URL + "/git/" + labOrg + "/" + labApp + "/appdev.git"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	text := getTextContent(t, callTool(t, lab.gitPushTool(), "zerops_deploy", map[string]any{"targetService": "appdev", "strategy": "git-push"}))

	var refusal struct {
		Suggestion string `json:"suggestion"`
	}
	if err := json.Unmarshal([]byte(text), &refusal); err != nil {
		t.Fatalf("not a refusal: %v\n%s", err, text)
	}
	const want = "In appdev's checkout, commit on a branch of your own and run `git push origin <branch>` — never push \"main\" and never force-push: \"main\" moves only when a person merges. Deploy the pair directly (strategy \"ssh\") to run the code."
	if refusal.Suggestion != want {
		t.Errorf("suggestion = %q, want the words before zcp git %q", refusal.Suggestion, want)
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
