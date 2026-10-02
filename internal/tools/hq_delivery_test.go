// Tests for: tools/hq_delivery.go — a deploy of a wired pair's stage half
// delivers the dev half's work as a change in HQ, run against a fake HQ that
// serves real git (hq_lab_test.go).
package tools

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// startSession opens a work session with intent, as the agent's task.
func startSession(t *testing.T, stateDir, intent string) {
	t.Helper()
	ws := workflow.NewWorkSession(labMate, string(workflow.EnvContainer), intent, []string{"appdev"})
	if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })
}

// TestAStageDeployOfAWiredPairDeliversItself is the owner's run of
// 2026-09-17: "build a todo app" has to end with a change without the person
// saying how code travels. The stage half's deploy commits the dev half's
// tree with the task's words, opens the change and pushes its branch; the dev
// half's own deploys deliver nothing.
func TestAStageDeployOfAWiredPairDeliversItself(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	startSession(t, lab.stateDir, "Build a todo app\nwith a list")
	lab.write(map[string]string{"index.js": "the app\n", ".gitignore": "node_modules/\n"})

	if d := deliverHQPair(t.Context(), lab.mock, lab.hq.srv.Client(), lab.ssh, lab.rt, lab.stateDir, "appdev"); d != nil {
		t.Fatalf("the dev half delivers nothing, got %+v", d)
	}
	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil {
		t.Fatalf("want a delivery through a change, got %+v", delivery)
	}
	change := lab.hq.change(1)
	if change == nil || change.Title != "Build a todo app" {
		t.Fatalf("change #1 in HQ = %+v, want it titled with the task", change)
	}
	head := lab.remoteHead("mate/p-mate/1")
	if head == "" || head != lab.git("rev-parse", "HEAD") {
		t.Fatalf("the change's branch is at %q, want the checkout's HEAD", head)
	}
	if !lab.descends(labApp, "main", "mate/p-mate/1") {
		t.Errorf("the change's branch must descend from main")
	}
	if got := lab.git("log", "-1", "--format=%s"); got != "Build a todo app" {
		t.Errorf("the commit reads %q, want the task's words", got)
	}
	wantURL := lab.hq.srv.URL + "/changes/" + labApp + "/appdev/1"
	if delivery.Change.URL != wantURL || delivery.Change.Number != 1 || delivery.Change.Branch != "mate/p-mate/1" || !delivery.Change.Created {
		t.Errorf("change = %+v, want #1 at %s", delivery.Change, wantURL)
	}
	for _, want := range []string{wantURL, "change #1", `zerops_workflow action="describe-change" service="appdev"`, "Tell the person that link"} {
		if !strings.Contains(delivery.Line, want) {
			t.Errorf("the line misses %q:\n%s", want, delivery.Line)
		}
	}
	for _, never := range []string{"git-push", "build-integration", "pull request"} {
		if strings.Contains(delivery.Line, never) {
			t.Errorf("the line must not say %q:\n%s", never, delivery.Line)
		}
	}
	if lab.meta().HQ.Change != 1 {
		t.Errorf("the change's number is not on the pair: %+v", lab.meta().HQ)
	}
}

// TestAStageDeployAbsorbsTheMatesOwnMergedChange is the squash landing at the
// delivery: HQ merges a change by squashing it, and the squash shares no
// history with the branch that became it. The delivery reads the Mate's own
// state fresh — no pass has to have run since the merge — folds the squash in
// as a real merge, and opens the next change for the new work, never a
// conflict against history the squash discarded.
func TestAStageDeployAbsorbsTheMatesOwnMergedChange(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	if d := lab.deliver(); d == nil || d.Change == nil {
		t.Fatalf("first delivery: %+v", d)
	}
	squash := lab.hq.merge()

	lab.write(map[string]string{"footer.js": "the footer\n"})
	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil || delivery.Change.Number != 2 {
		t.Fatalf("want the work on change #2, got %+v", delivery)
	}
	if !strings.Contains(delivery.Line, "Change #1 is merged") {
		t.Errorf("the delivery must say what it learned of change #1:\n%s", delivery.Line)
	}
	if !lab.descends(labApp, squash, "mate/p-mate/2") {
		t.Errorf("change #2 must take the squash in as a real merge")
	}
	files := lab.git("ls-tree", "-r", "--name-only", "HEAD")
	for _, want := range []string{"index.js", "footer.js"} {
		if !strings.Contains(files, want) {
			t.Errorf("the delivered tree misses %q: %s", want, files)
		}
	}
	if hqRecord := lab.meta().HQ; hqRecord.Landed != nil || hqRecord.Change != 2 {
		t.Errorf("the pair's record = %+v, want change #2 and the landing forgotten", hqRecord)
	}
}

// TestAStageDeployWithNothingBeyondMainOpensNoChange: a change is opened only
// for work `main` does not already have.
func TestAStageDeployWithNothingBeyondMainOpensNoChange(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.deliver()
	lab.hq.merge()
	if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && git fetch -q origin && git reset -q --hard origin/main"); err != nil {
		t.Fatalf("reset the checkout onto main: %v\n%s", err, out)
	}

	delivery := lab.deliver()
	if delivery == nil || delivery.Change != nil {
		t.Fatalf("want a delivery with no change, got %+v", delivery)
	}
	if !strings.Contains(delivery.Line, "nothing differs from it, so no change is open") {
		t.Errorf("line:\n%s", delivery.Line)
	}
	if lab.hq.change(2) != nil {
		t.Errorf("no change may be opened for a checkout main already has")
	}
}

// TestADeliveryHQCouldNotReachIsFinishedByAPass: a delivery HQ did not
// refuse, only could not be reached for, is kept as pending, and the next
// pass finishes it once HQ answers — no one has to deploy again (SPEC
// §3.2a, §6.2.3).
func TestADeliveryHQCouldNotReachIsFinishedByAPass(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	startSession(t, lab.stateDir, "Add a footer")
	lab.write(map[string]string{"footer.js": "the footer\n"})

	lab.hq.setDown(true)
	delivery := lab.deliver()
	if delivery == nil || delivery.Change != nil || !strings.Contains(delivery.Line, "finishes by itself once HQ answers") {
		t.Fatalf("want a pending delivery, got %+v", delivery)
	}
	if pending := lab.meta().HQ.Pending; pending == nil || pending.Title != "Add a footer" {
		t.Fatalf("the pending delivery = %+v, want it titled with the task", pending)
	}
	if got := lab.git("log", "-1", "--format=%s"); got != "Add a footer" {
		t.Errorf("the work is committed in the checkout even while HQ is away, HEAD reads %q", got)
	}

	// A pass while HQ is still away changes nothing and says nothing.
	elapseHQBackoff(t, lab.stateDir)
	if report := lab.wire(); len(report) != 0 {
		t.Errorf("a pass while HQ is away says %q", report)
	}

	lab.hq.setDown(false)
	_ = workflow.DeleteWorkSession(lab.stateDir, os.Getpid())
	elapseHQBackoff(t, lab.stateDir)
	report := lab.wire()
	if len(report) != 1 || !strings.Contains(report[0], "is done") || !strings.Contains(report[0], "change #1") {
		t.Fatalf("report = %q, want the pending delivery done", report)
	}
	change := lab.hq.change(1)
	if change == nil || change.Title != "Add a footer" {
		t.Fatalf("change #1 = %+v, want it opened under the delivery's own title", change)
	}
	if head := lab.remoteHead("mate/p-mate/1"); head != lab.git("rev-parse", "HEAD") {
		t.Errorf("change #1's branch is at %q, want the checkout's HEAD", head)
	}
	if hqRecord := lab.meta().HQ; hqRecord.Pending != nil || hqRecord.Change != 1 {
		t.Errorf("the pair's record = %+v, want change #1 and nothing pending", hqRecord)
	}
}

// TestADeliveryWaitsOutAStandby: through a deploy of HQ two instances run
// side by side, and the standby answers git 503 with no Retry-After git can
// show. The push is tried again, never read as a failure.
func TestADeliveryWaitsOutAStandby(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.hq.standby(2, func(r *http.Request) bool {
		return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-receive-pack")
	})

	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil {
		t.Fatalf("want the delivery through once the standby is gone, got %+v", delivery)
	}
	if head := lab.remoteHead("mate/p-mate/1"); head == "" {
		t.Errorf("change #1's branch was never pushed")
	}
}

// TestADeliveryFollowsTheMateToAnotherApplication: HQ now holds the Mate in
// another application, so the delivery wires the pair to the repository of
// the same name there — made if new, its origin pointed at it, the Mate's
// branch joined onto its main — and delivers there. The change in the old
// application stays where it is.
func TestADeliveryFollowsTheMateToAnotherApplication(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.write(map[string]string{"index.js": "the app\n"})
	lab.deliver()

	lab.hq.moveMate("app-2")
	lab.write(map[string]string{"footer.js": "the footer\n"})
	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil || delivery.Change.Number != 1 {
		t.Fatalf("want change #1 in the new application, got %+v", delivery)
	}
	if !strings.Contains(delivery.Change.URL, "/changes/app-2/appdev/1") {
		t.Errorf("the change's address = %q, want it in app-2", delivery.Change.URL)
	}
	if !strings.Contains(delivery.Line, "change #1 stays in the one it was opened in") {
		t.Errorf("the line must say the old change stays:\n%s", delivery.Line)
	}
	meta := lab.meta()
	if meta.HQ.AppID != "app-2" || meta.HQ.Change != 1 || !strings.Contains(lab.git("remote", "get-url", "origin"), "/git/app-2/appdev.git") {
		t.Errorf("the pair is not wired to app-2: %+v, origin %s", meta.HQ, lab.git("remote", "get-url", "origin"))
	}
	if !lab.descends("app-2", "main", "mate/p-mate/1") {
		t.Errorf("the change in app-2 must descend from its main")
	}
	if old := lab.hq.change(1); old == nil || old.State != "open" {
		t.Errorf("the change in the old application = %+v, want it left open there", old)
	}
}

// TestADeliveryBringsTheCredentialToTheCurrentOne: a re-enrollment replaces
// the Mate credential, and the copy the push source holds goes stale with
// it. The delivery rewrites the copy and proves it before anything pushes.
func TestADeliveryBringsTheCredentialToTheCurrentOne(t *testing.T) {
	lab := newHQLab(t)
	lab.wire()
	lab.mock.WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{{ID: "ud-git-token", Key: "GIT_TOKEN", Content: "a-credential-hq-revoked", Sensitive: true}})
	lab.write(map[string]string{"index.js": "the app\n"})

	delivery := lab.deliver()
	if delivery == nil || delivery.Change == nil {
		t.Fatalf("want the delivery through with the current credential, got %+v", delivery)
	}
	if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != labCredential {
		t.Errorf("the push source holds %q, want the current credential", held)
	}
}

// TestADeliveryRefusedByItsGitSaysWhatToDo: a delivery its git stops by name
// leaves the checkout whole, pushes nothing, opens nothing, and says the one
// thing to do — never a force.
func TestADeliveryRefusedByItsGitSaysWhatToDo(t *testing.T) {
	tests := []struct {
		name  string
		setup func(lab *hqLab)
		files map[string]string
		want  []string
	}{
		{
			name:  "main moved on and changes the same lines",
			setup: func(lab *hqLab) { lab.hq.landOnMain("appdev", map[string]string{"index.js": "somebody else's line\n"}) },
			files: map[string]string{"index.js": "my line\n"},
			want:  []string{"has moved on", "index.js", "git fetch origin && git merge origin/main"},
		},
		{
			name:  "an unignored dependency directory",
			files: map[string]string{"index.js": "the app\n", "node_modules/express/index.js": "a dependency\n"},
			want:  []string{"node_modules", ".gitignore", "deploy appstage again"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			if tt.setup != nil {
				tt.setup(lab)
			}
			lab.write(tt.files)
			before := lab.git("rev-parse", "HEAD")

			delivery := lab.deliver()
			if delivery == nil || delivery.Change != nil {
				t.Fatalf("want a refused delivery, got %+v", delivery)
			}
			for _, want := range tt.want {
				if !strings.Contains(delivery.Line, want) {
					t.Errorf("the line misses %q:\n%s", want, delivery.Line)
				}
			}
			if strings.Contains(delivery.Line, "--force") || strings.Contains(delivery.Line, "rebase") {
				t.Errorf("a refusal never offers a force or a rebase:\n%s", delivery.Line)
			}
			if lab.hq.change(1) != nil || lab.remoteHead("mate/p-mate/1") != "" {
				t.Errorf("nothing may be opened or pushed")
			}
			if state := lab.git("status", "--porcelain=v1", "--untracked-files=no"); strings.Contains(state, "UU") {
				t.Errorf("the checkout must be left whole, not half-merged: %q", state)
			}
			if tt.setup == nil && lab.git("rev-parse", "HEAD") != before {
				t.Errorf("nothing may be committed before the refusal")
			}
		})
	}
}

// TestAWiredPairDeploysDirectlyAndIsNeverSentToPush: nothing builds a Mate's
// services from its change, so a wired pair's direct deploy proceeds and is
// never told to push instead — its stage deploy delivers by itself.
func TestAWiredPairDeploysDirectlyAndIsNeverSentToPush(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, StageHostname: "appstage",
		BootstrapSession: "test", BootstrappedAt: "2026-10-02", FirstDeployedAt: "2026-10-02T09:00:00Z",
		GitPushState: topology.GitPushConfigured, RemoteURL: "https://hq.example/git/a1/appdev.git",
		BuildIntegration: topology.BuildIntegrationActions,
		HQ:               &workflow.HQRepoRef{AppID: "a1", Repo: "appdev", Branch: "mate/p1"},
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

// refusingPushSSH answers a change's push the way git reports HQ refusing
// it — a ref HQ refused carries HQ's own reason — and everything else as a
// healthy container would.
type refusingPushSSH struct{ reason string }

func (s refusingPushSSH) ExecSSH(_ context.Context, host, command string) ([]byte, error) {
	if !strings.Contains(command, "push -u origin") {
		return []byte("ok"), nil
	}
	out := " ! [remote rejected] HEAD -> mate/p-mate/1 (" + s.reason + ")\nerror: failed to push some refs\n"
	return []byte(out), &platform.SSHExecError{Hostname: host, Output: out, Err: errors.New("exit status 1")}
}

func (s refusingPushSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// TestShipChange_AChangeSettledBeforeItsPushIsOwedAgain: a change merged or
// closed between its opening and its push is refused per ref with HQ's
// reason; the number is forgotten and the delivery stays owed, so the next
// delivery, push or pass opens the next change.
func TestShipChange_AChangeSettledBeforeItsPushIsOwedAgain(t *testing.T) {
	for _, reason := range []string{"change_closed", "unknown_change"} {
		t.Run(reason, func(t *testing.T) {
			lab := newHQLab(t)
			lab.wire()
			hqc, err := hq.Open(lab.hq.srv.Client(), hq.EnrollmentPath())
			if err != nil {
				t.Fatal(err)
			}
			shipped := shipChange(t.Context(), refusingPushSSH{reason: reason}, lab.stateDir, hqc, lab.meta(), "Add a footer", 1)
			if !shipped.pending || shipped.ref != nil || !strings.Contains(shipped.line, reason) {
				t.Fatalf("shipped = %+v, want the delivery owed again", shipped)
			}
			if record := lab.meta().HQ; record.Change != 0 || record.Pending == nil || record.Pending.Title != "Add a footer" {
				t.Errorf("the pair's record = %+v, want no change and the delivery pending", record)
			}
		})
	}
}
