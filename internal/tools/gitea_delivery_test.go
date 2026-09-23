package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

func TestAWiredMatePlansOnlyStandardPairs(t *testing.T) {
	t.Parallel()
	simple := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "todoapp", Type: "nodejs@22", BootstrapMode: topology.PlanModeSimple}}}
	devOnly := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: topology.PlanModeDev}}}
	pair := []workflow.BootstrapTarget{{Runtime: workflow.RuntimeTarget{DevHostname: "appdev", ExplicitStage: "appstage", Type: "nodejs@22", BootstrapMode: topology.PlanModeStandard}}}

	if pe := giteaPairPlanError(simple, false); pe != nil {
		t.Fatalf("a Mate without Gitea keeps zcp's own rules, got %q", pe.Message)
	}
	if pe := giteaPairPlanError(pair, true); pe != nil {
		t.Fatalf("a standard pair passes, got %q", pe.Message)
	}
	for name, plan := range map[string][]workflow.BootstrapTarget{"simple": simple, "dev-only": devOnly} {
		pe := giteaPairPlanError(plan, true)
		if pe == nil {
			t.Fatalf("%s: a wired Mate refuses a runtime with no stage half", name)
		}
		if !strings.Contains(pe.Message, "dev/stage pair") || !strings.Contains(pe.Message, plan[0].Runtime.DevHostname) {
			t.Fatalf("%s: the refusal names the rule and the target, got %q", name, pe.Message)
		}
		if !strings.Contains(pe.Suggestion, `bootstrapMode="standard"`) || !strings.Contains(pe.Suggestion, "stageHostname") {
			t.Fatalf("%s: the fix is the standard pair with a stage hostname, got %q", name, pe.Suggestion)
		}
	}
	if got := giteaStageNameFor("appdev"); got != "appstage" {
		t.Fatalf("appdev's stage is appstage, got %q", got)
	}
	if got := giteaStageNameFor("todoapp"); got != "todoappstage" {
		t.Fatalf("todoapp's stage is todoappstage, got %q", got)
	}
}

// TestLaunchProduction_WiredMateRefusesAndNamesTheProjectsPage is the
// backlog fix (plans/backlog/mate-wired-production-intent-misroutes-to-launch.md):
// launch-production creates its own, ungrouped production project and has
// no case for a group's, so a wired Mate refuses the whole workflow rather
// than let it try. The next step says what is TRUE of this Mate's own
// pairs — an open pull request by number, or the projects page once
// nothing is open — never a generic lecture.
func TestLaunchProduction_WiredMateRefusesAndNamesTheProjectsPage(t *testing.T) {
	t.Parallel()

	if refusal := giteaLaunchProductionRefusal(context.Background(), nil, t.TempDir(), false); refusal != nil {
		t.Fatalf("an unwired Mate keeps zcp's own launch-production, got a refusal: %q", resultText(t, refusal))
	}

	t.Run("nothing open", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		refusal := giteaLaunchProductionRefusal(context.Background(), nil, stateDir, true)
		if refusal == nil {
			t.Fatal("a wired Mate refuses launch-production")
		}
		text := resultText(t, refusal)
		if !strings.Contains(text, "wired_mate_production_is_the_groups") {
			t.Fatalf("refusal carries no structured reason, got %q", text)
		}
		if !strings.Contains(text, "projects page") {
			t.Fatalf("the next step names the projects page, got %q", text)
		}
	})

	t.Run("an open pull request is named", func(t *testing.T) {
		t.Parallel()
		stateDir := t.TempDir()
		if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
			Hostname: "appdev",
			Gitea:    &workflow.GiteaRepoRef{FullName: "acme/appdev", Branch: "mate/bot", PullRequest: 3},
		}); err != nil {
			t.Fatalf("write service meta: %v", err)
		}
		text := resultText(t, giteaLaunchProductionRefusal(context.Background(), nil, stateDir, true))
		if !strings.Contains(text, "#3") || !strings.Contains(text, "acme/appdev") {
			t.Fatalf("the next step names the open pull request, got %q", text)
		}
		if !strings.Contains(text, "merge") {
			t.Fatalf("the next step says to merge it, got %q", text)
		}
	})

	// TestLaunchProduction_WiredMateRefusesAndNamesTheProjectsPage's own
	// "already merged" case sits in the function below: it needs a fake
	// Gitea server and t.Setenv, which cannot share a process with this
	// function's own t.Parallel() subtests.
}

// TestLaunchProductionNextStep_AFreshlyMergedRequestIsNotNamedAsOpen is the
// measured incident (`Mate: weatherdev (#4)` merged on Gitea, "merge #4
// first" still said): a pair's recorded PullRequest is only cleared by the
// backoff-gated reconcile pass, so the next-step message must freshen it
// itself — giteaLearnLanding — before ever naming it as open.
func TestLaunchProductionNextStep_AFreshlyMergedRequestIsNotNamedAsOpen(t *testing.T) {
	fake := newFakeGitea()
	fake.pullState = "closed"
	fake.pullMerged = true
	fake.pullMergeCommit = "squash-sha"
	fake.pullMergeHead = "branch-tip-sha"
	gitea := fake.start(t)

	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "weatherdev",
		Gitea:    &workflow.GiteaRepoRef{FullName: "acme/weatherdev", Branch: "mate/bot", DefaultBranch: "main", PullRequest: 4},
	}); err != nil {
		t.Fatalf("write service meta: %v", err)
	}
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	text := resultText(t, giteaLaunchProductionRefusal(context.Background(), gitea.Client(), stateDir, true))
	if strings.Contains(text, "#4") || strings.Contains(text, "merge ") {
		t.Fatalf("a request already merged on Gitea must not be named as open, got %q", text)
	}
	if !strings.Contains(text, "projects page") {
		t.Fatalf("a merged request means the code reached main — the next step names the projects page, got %q", text)
	}
}

// TestAStageDeployOfAWiredPairDeliversItself is the owner's run of 2026-09-17:
// "build a todo app" has to end with a pull request without the person saying
// how code travels ("no person is ever going to say this"). The pair's stage
// half running a deploy is the moment the work is shippable, so zcp commits
// the dev half's tree, pushes the Mate's branch and opens the request itself;
// the dev half's own deploys deliver nothing.
func TestAStageDeployOfAWiredPairDeliversItself(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		sshOutput  string
		wantSSH    bool
		wantCreate int
		wantLine   []string
		wantNil    bool
	}{
		{
			name: "the stage half delivers", target: "appstage", sshOutput: "ok",
			wantSSH: true, wantCreate: 1,
			wantLine: []string{"mate/mate-p1", "acme/appdev", "pull request #3", "/acme/appdev/pulls/3"},
		},
		{name: "the dev half delivers nothing", target: "appdev", wantNil: true},
		{
			name: "an unignored node_modules stops it", target: "appstage",
			sshOutput: "ZCP_UNIGNORED: node_modules", wantSSH: true, wantCreate: 0,
			wantLine: []string{"node_modules", ".gitignore", "appstage"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeGitea()
			gitea := fake.start(t)
			stateDir := t.TempDir()
			writeWiredGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev")
			t.Setenv("GITEA_URL", gitea.URL)
			t.Setenv("MATE_BROKER_URL", gitea.URL)
			t.Setenv("GITEA_TOKEN", giteaBotToken)

			var hosts []string
			ssh := &hostRecordingSSH{output: tt.sshOutput, hosts: &hosts}
			delivery := deliverGiteaPair(context.Background(), platform.NewMock(), gitea.Client(), ssh,
				runtime.Info{InContainer: true, ProjectID: "proj-1"}, stateDir, tt.target)

			if tt.wantNil {
				if delivery != nil || len(hosts) != 0 {
					t.Fatalf("want no delivery and no SSH, got %+v on %v", delivery, hosts)
				}
				return
			}
			if delivery == nil {
				t.Fatal("want a delivery")
			}
			if tt.wantSSH && (len(hosts) == 0 || hosts[0] != "appdev") {
				t.Fatalf("the push runs in the dev half's checkout, got %v", hosts)
			}
			if fake.pullCreates != tt.wantCreate {
				t.Errorf("pull requests created = %d, want %d", fake.pullCreates, tt.wantCreate)
			}
			for _, want := range tt.wantLine {
				if !strings.Contains(delivery.Line, want) {
					t.Errorf("the line misses %q:\n%s", want, delivery.Line)
				}
			}
			if strings.Contains(delivery.Line, "git-push") || strings.Contains(delivery.Line, "build-integration") {
				t.Errorf("the line must ask the agent for no push and no integration:\n%s", delivery.Line)
			}
		})
	}

	t.Run("a Mate without Gitea delivers nothing", func(t *testing.T) {
		stateDir := t.TempDir()
		writeWiredGiteaPairMeta(t, stateDir, "https://git.example/acme/appdev")
		t.Setenv("GITEA_URL", "")
		t.Setenv("MATE_BROKER_URL", "")
		t.Setenv("GITEA_TOKEN", "")
		var hosts []string
		if d := deliverGiteaPair(context.Background(), platform.NewMock(), nil, &hostRecordingSSH{hosts: &hosts},
			runtime.Info{InContainer: true}, stateDir, "appstage"); d != nil || len(hosts) != 0 {
			t.Fatalf("want nothing, got %+v on %v", d, hosts)
		}
	})
}

// TestAStageDeployAbsorbsAFreshMergeWithoutWaitingForAReconcilePass is the
// squash-landing bug, at the delivery: a stage deploy must not depend on a
// reconcile pass having already learned the merge (the passes are backoff-
// gated, giteaAttemptDue) — it reads the recorded pull request's outcome
// itself, and once it learns of a squash landing it absorbs it (S, H) into
// the pushed history rather than letting the ordinary take-the-base-in merge
// read it as two unrelated histories that both add the same files (MB-26).
// A successful delivery then forgets the landing — it has been absorbed, or
// proven to need no absorbing, either way.
func TestAStageDeployAbsorbsAFreshMergeWithoutWaitingForAReconcilePass(t *testing.T) {
	fake := newFakeGitea()
	fake.branchExists = true
	fake.pullState = "closed"
	fake.pullMerged = true
	fake.pullMergeCommit = "squash-sha"
	fake.pullMergeHead = "branch-tip-sha"
	gitea := fake.start(t)

	stateDir := t.TempDir()
	writeLandedGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	var commands []string
	ssh := &scriptedSSH{respond: func(_, command string) string {
		commands = append(commands, command)
		return "ok"
	}}
	delivery := deliverGiteaPair(context.Background(), platform.NewMock(), gitea.Client(), ssh,
		runtime.Info{InContainer: true, ProjectID: "proj-1"}, stateDir, "appstage")
	if delivery == nil {
		t.Fatal("want a delivery")
	}

	var absorbed bool
	for _, cmd := range commands {
		if strings.Contains(cmd, "squash-sha") && strings.Contains(cmd, "branch-tip-sha") {
			absorbed = true
		}
	}
	if !absorbed {
		t.Errorf("the delivery command must carry the landing it read itself, without a reconcile pass: %v", commands)
	}

	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta == nil || meta.Gitea == nil {
		t.Fatal("meta vanished")
	}
	// The merged request (#4) is forgotten; the delivery's own push opens
	// the NEXT one for the new work it just carried.
	if meta.Gitea.PullRequest == 4 {
		t.Errorf("the merged request must be forgotten, still recorded as #%d", meta.Gitea.PullRequest)
	}
	if meta.Gitea.Landed != nil {
		t.Errorf("a successfully delivered landing must be forgotten, got %+v", meta.Gitea.Landed)
	}
	// The OLD request's fate (#4 merged) is news independent of this
	// delivery's own outcome — nothing else would ever say it once the
	// number is off meta.Gitea.PullRequest.
	if !strings.Contains(delivery.Line, "pull request #4 is merged") {
		t.Errorf("the delivery must fold in what it learned about the old request:\n%s", delivery.Line)
	}
}

// TestGitPushDeploy_AbsorbsALandingBeforeItPushes is the manual-push half of
// the squash-landing fix: PR #2 in the live incident was opened by exactly
// this path — an ordinary `zerops_deploy strategy="git-push"` on the dev
// half, not a stage deploy — and Gitea computes a pull request's
// mergeability itself, independent of whether zcp's own push succeeds. So a
// push onto a wired pair must absorb a fresh landing of THIS Mate's own
// earlier pull request BEFORE it pushes, exactly as deliverGiteaPair does,
// or the pull request it then opens/touches shows a false conflict to
// whoever looks at it.
func TestGitPushDeploy_AbsorbsALandingBeforeItPushes(t *testing.T) {
	fake := newFakeGitea()
	fake.branchExists = true
	fake.pullState = "closed"
	fake.pullMerged = true
	fake.pullMergeCommit = "squash-sha"
	fake.pullMergeHead = "branch-tip-sha"
	gitea := fake.start(t)

	stateDir := t.TempDir()
	writeLandedGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	ssh := &stubSSHWithCommands{tokenOutput: []byte("1"), committedOutput: []byte("1"), pushOutput: []byte("ok")}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, platform.NewMock(), gitea.Client(), "proj-1", ssh, authInfo, nil,
		runtime.Info{InContainer: true, ProjectID: "proj-1", GiteaURL: gitea.URL},
		stateDir, testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
	})
	if result.IsError {
		t.Fatalf("want success, got error: %s", getTextContent(t, result))
	}

	if ssh.absorbCalls != 1 {
		t.Fatalf("the absorb/sync must run exactly once before the push, got %d calls: %v", ssh.absorbCalls, ssh.commands)
	}
	if ssh.pushCalls != 1 {
		t.Fatalf("the push must still run, got %d calls", ssh.pushCalls)
	}

	var absorbIdx, pushIdx = -1, -1
	for i, cmd := range ssh.commands {
		if strings.Contains(cmd, "fetch --no-tags -q origin") && absorbIdx < 0 {
			absorbIdx = i
			if !strings.Contains(cmd, "squash-sha") || !strings.Contains(cmd, "branch-tip-sha") {
				t.Errorf("the absorb command must carry the landing this push learned itself, got:\n%s", cmd)
			}
		}
		if strings.Contains(cmd, "push -u origin") && pushIdx < 0 {
			pushIdx = i
		}
	}
	if absorbIdx < 0 || pushIdx < 0 || absorbIdx > pushIdx {
		t.Fatalf("the absorb must run BEFORE the push, got absorb@%d push@%d in %v", absorbIdx, pushIdx, ssh.commands)
	}

	// PR #4 is merged and closed — the push must open the NEXT one (#3, the
	// fake's next number), and that request's branch already carries the
	// absorbed landing (proven at the ops layer by
	// TestBuildGiteaDeliveryCommand_AbsorbsASquashLanding*; this asserts zcp
	// ran the absorb before the request-opening push, not the git result).
	if fake.pullCreates != 1 {
		t.Errorf("a new pull request must open after the merged one, got %d creates", fake.pullCreates)
	}

	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta == nil || meta.Gitea == nil {
		t.Fatal("meta vanished")
	}
	if meta.Gitea.Landed != nil {
		t.Errorf("an absorbed landing must be forgotten, got %+v", meta.Gitea.Landed)
	}

	// The OLD request's fate (#4 merged) is news independent of this
	// push's own outcome — surfaced as a warning since a plain git-push
	// response has no delivery line of its own.
	text := getTextContent(t, result)
	if !strings.Contains(text, "pull request #4 is merged") {
		t.Errorf("the push must surface what it learned about the old request:\n%s", text)
	}
}

// TestGitPushDeploy_AbsorbConflictGivesTheManualAbsorbSequence pins item 2 of
// the judge's review, on the plain-push path: a REAL conflict inside the
// absorb's own S^1 merge must not be answered with the plain
// `git fetch origin && git merge origin/<base>` advice — that would
// recreate the very conflict it is meant to resolve, since the base still
// would not contain S's content. It needs the manual absorb sequence.
func TestGitPushDeploy_AbsorbConflictGivesTheManualAbsorbSequence(t *testing.T) {
	fake := newFakeGitea()
	fake.branchExists = true
	fake.pullState = "closed"
	fake.pullMerged = true
	fake.pullMergeCommit = "squash-sha"
	fake.pullMergeHead = "branch-tip-sha"
	gitea := fake.start(t)

	stateDir := t.TempDir()
	writeLandedGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	ssh := &stubSSHWithCommands{
		tokenOutput:     []byte("1"),
		committedOutput: []byte("1"),
		absorbOutput:    []byte("ZCP_ABSORB_CONFLICT:index.js"),
	}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, platform.NewMock(), gitea.Client(), "proj-1", ssh, authInfo, nil,
		runtime.Info{InContainer: true, ProjectID: "proj-1", GiteaURL: gitea.URL},
		stateDir, testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
	})
	if !result.IsError {
		t.Fatalf("a real absorb conflict must block the push, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	for _, want := range []string{"git merge squash-sha^1", "resolve it and commit", "git merge -s ours squash-sha"} {
		if !strings.Contains(text, want) {
			t.Errorf("the error misses %q:\n%s", want, text)
		}
	}
	// L3: news about the OLD request (#4, merged) must not be lost just
	// because THIS run's own absorb hit a conflict — nothing else would
	// ever say it once the number is off meta.Gitea.PullRequest.
	if !strings.Contains(text, "pull request #4 is merged") {
		t.Errorf("the error must still surface what the absorb learned about the old request:\n%s", text)
	}
	if ssh.pushCalls != 0 {
		t.Errorf("a real conflict must stop the push outright, got %d push calls", ssh.pushCalls)
	}
}

// TestGitPushDeploy_UnprovableConflictGivesTheManualAbsorbSequenceToo covers
// a container git too old for `merge-tree --write-tree` (item 6): the
// absorb falls through unproven, the ordinary step then hits the same false
// conflict, and the push must recognize that shape and give the manual
// sequence rather than the plain fetch+merge that just failed.
func TestGitPushDeploy_UnprovableConflictGivesTheManualAbsorbSequenceToo(t *testing.T) {
	fake := newFakeGitea()
	fake.branchExists = true
	fake.pullState = "closed"
	fake.pullMerged = true
	fake.pullMergeCommit = "squash-sha"
	fake.pullMergeHead = "branch-tip-sha"
	gitea := fake.start(t)

	stateDir := t.TempDir()
	writeLandedGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	ssh := &stubSSHWithCommands{
		tokenOutput:     []byte("1"),
		committedOutput: []byte("1"),
		absorbOutput:    []byte("ZCP_ABSORB_UNPROVABLE\nZCP_MERGE_CONFLICT:index.js"),
	}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, platform.NewMock(), gitea.Client(), "proj-1", ssh, authInfo, nil,
		runtime.Info{InContainer: true, ProjectID: "proj-1", GiteaURL: gitea.URL},
		stateDir, testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
	})
	if !result.IsError {
		t.Fatalf("want an error, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "may be this Mate's own squashed pull request") {
		t.Errorf("an unprovable landing behind an ordinary conflict must be named as a possible cause:\n%s", text)
	}
	if !strings.Contains(text, "git merge squash-sha^1") {
		t.Errorf("the error misses the manual sequence:\n%s", text)
	}
	if ssh.pushCalls != 0 {
		t.Errorf("must not push, got %d push calls", ssh.pushCalls)
	}
}

// TestGitPushDeploy_AbsorbConflictBlocksThePush is the other half: a REAL
// conflict from the absorb/sync step (not the false squash-vs-history one)
// must stop the push outright — pushing on top of a checkout the sync left
// mid-way would be worse than the false conflict this whole fix exists to
// avoid.
func TestGitPushDeploy_AbsorbConflictBlocksThePush(t *testing.T) {
	fake := newFakeGitea()
	fake.branchExists = true
	fake.pullState = "closed"
	fake.pullMerged = true
	fake.pullMergeCommit = "squash-sha"
	fake.pullMergeHead = "branch-tip-sha"
	gitea := fake.start(t)

	stateDir := t.TempDir()
	writeLandedGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	ssh := &stubSSHWithCommands{
		tokenOutput:     []byte("1"),
		committedOutput: []byte("1"),
		absorbOutput:    []byte("ZCP_MERGE_CONFLICT:index.js"),
		absorbErr:       errors.New("exit status 4"),
	}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, platform.NewMock(), gitea.Client(), "proj-1", ssh, authInfo, nil,
		runtime.Info{InContainer: true, ProjectID: "proj-1", GiteaURL: gitea.URL},
		stateDir, testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
	})
	if !result.IsError {
		t.Fatalf("a real conflict must block the push, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "index.js") {
		t.Errorf("the error must name the conflicting file, got:\n%s", text)
	}
	if !strings.Contains(text, "push again") {
		t.Errorf("the error must say the fix is to resolve it and push again, got:\n%s", text)
	}
	if ssh.pushCalls != 0 {
		t.Errorf("a real conflict must stop the push outright, got %d push calls", ssh.pushCalls)
	}

	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta == nil || meta.Gitea == nil || meta.Gitea.Landed == nil {
		t.Fatal("the unabsorbed landing must still be recorded, so the next attempt retries it")
	}
}

// TestGitPushDeploy_DirtyTreeBlocksThePushWithACommitFirstMessage pins L2
// of the judge's re-review: uncommitted changes in a file the absorb's S^1
// merge would touch make git refuse to even start — no unmerged index
// entry results, so a caller checking only ops.GiteaAbsorbConflict's file
// list would read the bare ZCP_ABSORB_DIRTY marker as "no conflict" and
// push unabsorbed anyway. The dirty case must be recognized on its own and
// block the push with a commit-first message.
func TestGitPushDeploy_DirtyTreeBlocksThePushWithACommitFirstMessage(t *testing.T) {
	fake := newFakeGitea()
	fake.branchExists = true
	fake.pullState = "closed"
	fake.pullMerged = true
	fake.pullMergeCommit = "squash-sha"
	fake.pullMergeHead = "branch-tip-sha"
	gitea := fake.start(t)

	stateDir := t.TempDir()
	writeLandedGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev.git")
	t.Setenv("GITEA_URL", gitea.URL)
	t.Setenv("MATE_BROKER_URL", gitea.URL)
	t.Setenv("GITEA_TOKEN", giteaBotToken)

	ssh := &stubSSHWithCommands{
		tokenOutput:     []byte("1"),
		committedOutput: []byte("1"),
		absorbOutput:    []byte("ZCP_ABSORB_DIRTY"),
		absorbErr:       errors.New("exit status 6"),
	}
	authInfo := &auth.Info{Token: "t", APIHost: "api.app-prg1.zerops.io", Region: "prg1"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterDeploySSH(srv, platform.NewMock(), gitea.Client(), "proj-1", ssh, authInfo, nil,
		runtime.Info{InContainer: true, ProjectID: "proj-1", GiteaURL: gitea.URL},
		stateDir, testDeployEngine(t), nil)

	result := callTool(t, srv, "zerops_deploy", map[string]any{
		"targetService": "appdev",
		"strategy":      "git-push",
	})
	if !result.IsError {
		t.Fatalf("a dirty checkout must block the push, got success: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if !strings.Contains(text, "uncommitted changes") || !strings.Contains(text, "commit") {
		t.Errorf("the error must say uncommitted changes block the landed PR and to commit first, got:\n%s", text)
	}
	if !strings.Contains(text, "push again") {
		t.Errorf("the error must say to push again after committing, got:\n%s", text)
	}
	if !strings.Contains(text, "pull request #4 is merged") {
		t.Errorf("the error must still surface what the absorb learned about the old request:\n%s", text)
	}
	if ssh.pushCalls != 0 {
		t.Errorf("a dirty checkout must stop the push outright, got %d push calls", ssh.pushCalls)
	}

	meta, _ := workflow.FindServiceMeta(stateDir, "appdev")
	if meta == nil || meta.Gitea == nil || meta.Gitea.Landed == nil {
		t.Fatal("the unabsorbed landing must still be recorded, so the next attempt retries it")
	}
}

// TestAStageDeployDirtyTreeGivesACommitFirstMessage is the delivery-path
// half of the dirty-checkout case above — unreachable in ordinary
// operation (BuildGiteaDeliveryCommand commits everything before the
// absorb runs) but handled defensively, the same way.
func TestAStageDeployDirtyTreeGivesACommitFirstMessage(t *testing.T) {
	stateDir := t.TempDir()
	writeWiredGiteaPairMeta(t, stateDir, "https://git.example/acme/appdev")
	if err := workflow.UpsertServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta, _ bool) error {
		m.Gitea.Landed = &workflow.LandedPullRequest{Commit: "squash-sha", Head: "branch-tip-sha"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITEA_URL", "https://git.example")
	t.Setenv("MATE_BROKER_URL", "https://git.example")
	t.Setenv("GITEA_TOKEN", giteaBotToken)
	var hosts []string
	ssh := &hostRecordingSSH{output: "ZCP_ABSORB_DIRTY", hosts: &hosts}

	delivery := deliverGiteaPair(context.Background(), platform.NewMock(), okHTTP, ssh,
		runtime.Info{InContainer: true, ProjectID: "proj-1"}, stateDir, "appstage")
	if delivery == nil {
		t.Fatal("want a delivery")
	}
	if !strings.Contains(delivery.Line, "uncommitted changes") || !strings.Contains(delivery.Line, "Commit") {
		t.Errorf("the line must say uncommitted changes block the landed PR and to commit first:\n%s", delivery.Line)
	}
}

// TestAStageDeployAbsorbConflictGivesTheManualAbsorbSequence pins item 2 of
// the judge's review: the plain `git fetch origin && git merge origin/<base>`
// advice, given for a REAL conflict inside the absorb's own S^1 merge, would
// recreate the exact false add/add conflict the absorb exists to prevent —
// the base still would not contain S's content. The advice for THIS
// conflict shape has to be the manual absorb sequence instead.
func TestAStageDeployAbsorbConflictGivesTheManualAbsorbSequence(t *testing.T) {
	stateDir := t.TempDir()
	writeWiredGiteaPairMeta(t, stateDir, "https://git.example/acme/appdev")
	if err := workflow.UpsertServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta, _ bool) error {
		m.Gitea.Landed = &workflow.LandedPullRequest{Commit: "squash-sha", Head: "branch-tip-sha"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITEA_URL", "https://git.example")
	t.Setenv("MATE_BROKER_URL", "https://git.example")
	t.Setenv("GITEA_TOKEN", giteaBotToken)
	var hosts []string
	ssh := &hostRecordingSSH{output: "ZCP_ABSORB_CONFLICT:index.js", hosts: &hosts}

	delivery := deliverGiteaPair(context.Background(), platform.NewMock(), okHTTP, ssh,
		runtime.Info{InContainer: true, ProjectID: "proj-1"}, stateDir, "appstage")
	if delivery == nil {
		t.Fatal("want a delivery")
	}
	for _, want := range []string{"git merge squash-sha^1", "resolve it and commit", "git merge -s ours squash-sha", "git fetch origin && git merge origin/main"} {
		if !strings.Contains(delivery.Line, want) {
			t.Errorf("the line misses %q:\n%s", want, delivery.Line)
		}
	}
	if strings.Contains(delivery.Line, "In appdev's checkout run `git fetch origin && git merge origin/main`, resolve it, then deploy") {
		t.Errorf("the plain fetch+merge advice recreates the conflict it is meant to resolve, must not be given alone:\n%s", delivery.Line)
	}
}

// TestAStageDeployUnprovableConflictGivesTheManualAbsorbSequenceToo covers a
// container git too old for `merge-tree --write-tree` (item 6): the absorb
// silently falls through unproven, the ordinary step then hits the same
// false conflict, and the delivery must recognize that shape (the
// unprovable marker alongside an ordinary conflict, with a landing
// recorded) and give the same manual sequence, not send the agent back into
// the plain fetch+merge that just failed.
func TestAStageDeployUnprovableConflictGivesTheManualAbsorbSequenceToo(t *testing.T) {
	stateDir := t.TempDir()
	writeWiredGiteaPairMeta(t, stateDir, "https://git.example/acme/appdev")
	if err := workflow.UpsertServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta, _ bool) error {
		m.Gitea.Landed = &workflow.LandedPullRequest{Commit: "squash-sha", Head: "branch-tip-sha"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITEA_URL", "https://git.example")
	t.Setenv("MATE_BROKER_URL", "https://git.example")
	t.Setenv("GITEA_TOKEN", giteaBotToken)
	var hosts []string
	ssh := &hostRecordingSSH{output: "ZCP_ABSORB_UNPROVABLE\nZCP_MERGE_CONFLICT:index.js", hosts: &hosts}

	delivery := deliverGiteaPair(context.Background(), platform.NewMock(), okHTTP, ssh,
		runtime.Info{InContainer: true, ProjectID: "proj-1"}, stateDir, "appstage")
	if delivery == nil {
		t.Fatal("want a delivery")
	}
	if !strings.Contains(delivery.Line, "may be this Mate's own squashed pull request") {
		t.Errorf("an unprovable landing behind an ordinary conflict must be named as a possible cause:\n%s", delivery.Line)
	}
	// L1(b): unprovable means nothing ever verified S's content — `-s ours`
	// here would silently discard whatever it actually was. The advice must
	// be a plain merge instead, resolved on its own merits.
	for _, want := range []string{"git merge squash-sha^1", "git merge squash-sha`", "never `-s ours`"} {
		if !strings.Contains(delivery.Line, want) {
			t.Errorf("the line misses %q:\n%s", want, delivery.Line)
		}
	}
	if strings.Contains(delivery.Line, "-s ours squash-sha") {
		t.Errorf("an unprovable landing must never be told to -s ours — nothing proved it safe:\n%s", delivery.Line)
	}
}

// A wired pair's direct deploys are the Mate's own: nothing a Gitea workflow
// runs ever rebuilds a Mate's service, so no integration may turn them into
// push-delivery-required, and no warning may send the agent to push by hand.
func TestAWiredPairDeploysDirectlyAndIsNeverSentToPush(t *testing.T) {
	stateDir := t.TempDir()
	writeWiredGiteaPairMeta(t, stateDir, "https://git.example/acme/appdev")
	if err := workflow.UpsertServiceMeta(stateDir, "appdev", func(m *workflow.ServiceMeta, _ bool) error {
		m.FirstDeployedAt = "2026-09-17T15:00:00Z"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"appdev", "appstage"} {
		if r := repoDeliveryRedirect(stateDir, target, "", false); r != nil {
			t.Errorf("%s: a wired pair's direct deploy must proceed", target)
		}
		if w := repoDeliveryDivergenceWarning(stateDir, target); w != "" {
			t.Errorf("%s: no push-by-hand warning on a wired pair, got %q", target, w)
		}
	}
}

// hostRecordingSSH answers every command with one output and records which
// container each ran in.
type hostRecordingSSH struct {
	output string
	hosts  *[]string
}

func (s *hostRecordingSSH) ExecSSH(_ context.Context, host, _ string) ([]byte, error) {
	*s.hosts = append(*s.hosts, host)
	if strings.Contains(s.output, "ZCP_UNIGNORED:") {
		return []byte(s.output), errors.New("exit status 3")
	}
	return []byte(s.output), nil
}

func (s *hostRecordingSSH) ExecSSHBackground(_ context.Context, host, _ string, _ time.Duration) ([]byte, error) {
	*s.hosts = append(*s.hosts, host)
	return []byte("ok"), nil
}

// oldGiteaWorkflow is what zcp wrote before D27: the broker's deploy action at
// v1, no dispatch, and here a Test step the project filled in.
const oldGiteaWorkflow = `name: Zerops deploy
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Test
        # The project's own.
        run: |
          npm ci
          npm test
      - name: Deploy through the broker
        uses: zeropsio/gitea-mate/actions/deploy@v1
        with:
          environment: stage
          service: app
`

// Wiring writes the workflow once and never runs again for a wired pair, so the
// delivery — the one moment every wired pair passes through — is where the file
// follows zcp (measured 2026-09-18: two Mates whose branches still named the
// broker's old deploy action after D27, and nothing that would ever change it).
func TestADeliveryBringsTheWorkflowToThisZcps(t *testing.T) {
	tests := []struct {
		name      string
		existing  string
		wantWrite bool
		want      []string
		wantNot   []string
	}{
		{
			name: "an earlier zcp's workflow is replaced, its Test step kept", existing: oldGiteaWorkflow,
			wantWrite: true,
			want:      []string{"uses: " + giteaBrokerDeployAction, "workflow_dispatch", "npm ci\n          npm test", "# The project's own."},
			wantNot:   []string{"actions/deploy@v1", "no test command configured", "environment: stage"},
		},
		{
			name: "a missing file is written", existing: "",
			wantWrite: true,
			want:      []string{"uses: " + giteaBrokerDeployAction, "no test command configured"},
		},
		{
			name:      "a file that names this zcp's deploy action is the project's, whatever else it says",
			existing:  strings.Replace(giteaWorkflowYAML(), `run: echo "no test command configured"`, "run: make test", 1) + "# a person's note\n",
			wantWrite: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeGitea()
			gitea := fake.start(t)
			stateDir := t.TempDir()
			writeWiredGiteaPairMeta(t, stateDir, gitea.URL+"/acme/appdev")
			t.Setenv("GITEA_URL", gitea.URL)
			t.Setenv("MATE_BROKER_URL", gitea.URL)
			t.Setenv("GITEA_TOKEN", giteaBotToken)

			var commands []string
			ssh := &scriptedSSH{respond: func(host, command string) string {
				if host != "appdev" {
					t.Errorf("everything runs in the dev half's checkout, got %q", host)
				}
				commands = append(commands, command)
				if strings.Contains(command, "cat ") {
					return tt.existing
				}
				return "ok"
			}}
			if d := deliverGiteaPair(context.Background(), platform.NewMock(), gitea.Client(), ssh,
				runtime.Info{InContainer: true, ProjectID: "proj-1"}, stateDir, "appstage"); d == nil {
				t.Fatal("want a delivery")
			}

			written, committed := "", -1
			for i, command := range commands {
				if strings.Contains(command, "base64 -d") {
					if committed >= 0 {
						t.Fatal("the workflow is written before the tree is committed, not after")
					}
					written = decodeWrittenFile(t, command)
				}
				if strings.Contains(command, "git add -A") {
					committed = i
				}
			}
			if committed < 0 {
				t.Fatal("the delivery committed nothing")
			}
			if (written != "") != tt.wantWrite {
				t.Fatalf("workflow written = %v, want %v", written != "", tt.wantWrite)
			}
			for _, want := range tt.want {
				if !strings.Contains(written, want) {
					t.Errorf("the workflow written misses %q:\n%s", want, written)
				}
			}
			for _, not := range tt.wantNot {
				if strings.Contains(written, not) {
					t.Errorf("the workflow written still carries %q:\n%s", not, written)
				}
			}
		})
	}
}

// decodeWrittenFile reads the body out of a BuildWriteRepoFileCommand line.
func decodeWrittenFile(t *testing.T, command string) string {
	t.Helper()
	_, rest, found := strings.Cut(command, "printf %s '")
	if !found {
		t.Fatalf("no file body in %q", command)
	}
	encoded, _, _ := strings.Cut(rest, "'")
	body, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("the file body does not decode: %v", err)
	}
	return string(body)
}

// scriptedSSH answers each command with what respond says.
type scriptedSSH struct {
	respond func(host, command string) string
}

func (s *scriptedSSH) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	return []byte(s.respond(host, command)), nil
}

func (s *scriptedSSH) ExecSSHBackground(_ context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return []byte(s.respond(host, command)), nil
}
