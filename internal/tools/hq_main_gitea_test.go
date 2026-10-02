// Tests for: tools/hq_main_gitea.go — a pair main's zcp wired to its
// organization's Gitea moves to HQ, which the migration brought its
// repository and its open pull request into; any other remote stays the
// user's own. Run against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// labGiteaURL is the old Gitea of the lab's organization. Nothing answers
// there: a pass that reached for it would fail, and nothing may write to it.
const (
	labGiteaURL   = "https://gitea.example.invalid"
	labGiteaToken = "the-gitea-token"
	labGiteaOrg   = "acme"
)

// mainsPair is the lab's pair as main left it, and what the migration made of
// it in HQ.
type mainsPair struct {
	// remote is the pair's origin on the old Gitea.
	remote string
	// pullHead is the head of the Mate's open pull request, #3 — HQ's change
	// #3 now; head is the checkout's HEAD, one commit past it.
	pullHead, head string
}

// fromMain makes the lab's pair one main's zcp wired to the organization's
// Gitea as repo: its origin there, as main's broker answered a clone URL (no
// `.git`), its GIT_TOKEN the Mate's Gitea token, the helper main persisted
// for the Gitea's host, and main's record of it with its pull request. The
// Mate works on mate/mate-p-mate: its pull request #3 holds one commit, and
// one more is committed but not pushed. HQ's import brought the repository —
// with a main another Mate's merge has since moved past the checkout's — and
// the pull request as the Mate's open change #3.
func (l *hqLab) fromMain(repo string) mainsPair {
	l.t.Helper()
	l.t.Setenv("GITEA_URL", labGiteaURL)
	pair := mainsPair{remote: labGiteaURL + "/" + labGiteaOrg + "/" + repo}
	l.write(map[string]string{"index.js": "the app\n"})
	l.commit("The app")
	l.git("checkout", "-q", "-b", "mate/mate-"+labMate)
	l.write(map[string]string{"todo.js": "todos\n"})
	l.commit("Add todos")
	pair.pullHead = l.git("rev-parse", "HEAD")
	l.write(map[string]string{"due.js": "due dates\n"})
	l.commit("Add due dates")
	pair.head = l.git("rev-parse", "HEAD")
	l.git("remote", "add", "origin", pair.remote)
	l.git("config", "credential.https://gitea.example.invalid.helper", `!f() { test "$1" = get && { echo username=oauth2; echo "password=$GIT_TOKEN"; }; }; f`)

	l.hq.importRepo(repo, l.pair, map[int]string{3: pair.pullHead})
	l.hq.landOnMain(repo, map[string]string{"other.js": "another Mate's work\n"})

	l.mock.WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{{ID: "e1", Key: "GIT_TOKEN", Content: labGiteaToken}})
	if err := workflow.UpdateServiceMeta(l.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.GitPushState, m.RemoteURL, m.TrackedRef = topology.GitPushConfigured, pair.remote, "main"
		m.MainGitea = &workflow.MainGiteaRepo{FullName: labGiteaOrg + "/" + repo, PullRequest: 3}
		return nil
	}); err != nil {
		l.t.Fatal(err)
	}
	return pair
}

// TestReconcileHQRepositories_MovesAPairOffMainsGitea: the pair's repository
// is the one HQ's import brought under the name main's record gives it;
// origin moves there with the old Gitea kept as zerops-original-origin, the
// pair's GIT_TOKEN becomes the Mate credential and the old host's helper
// goes, and the checkout is named the Mate's branch where it is — its open
// work and its unpushed commit kept. A second pass asks HQ nothing.
func TestReconcileHQRepositories_MovesAPairOffMainsGitea(t *testing.T) {
	for _, repo := range []string{"appdev", "shop"} {
		t.Run(repo, func(t *testing.T) {
			lab := newHQLab(t)
			pair := lab.fromMain(repo)

			report := lab.wire()

			if len(report) != 1 || !strings.Contains(report[0], `repository "`+repo+`" wired in HQ`) {
				t.Fatalf("report = %q", report)
			}
			want := lab.hq.srv.URL + "/git/" + labApp + "/" + repo + ".git"
			meta := lab.meta()
			if meta.HQ == nil || meta.HQ.AppID != labApp || meta.HQ.Repo != repo || meta.HQ.Branch != "mate/"+labMate {
				t.Fatalf("the pair's HQ record = %+v", meta.HQ)
			}
			if meta.MainGitea != nil || meta.RemoteURL != want || meta.GitPushState != topology.GitPushConfigured {
				t.Errorf("record: main's %+v, remote %q, git-push %s", meta.MainGitea, meta.RemoteURL, meta.GitPushState)
			}
			if got := lab.git("remote", "get-url", "origin"); got != want {
				t.Errorf("origin = %q, want %q", got, want)
			}
			if got := lab.git("remote", "get-url", "zerops-original-origin"); got != pair.remote {
				t.Errorf("zerops-original-origin = %q, want the old Gitea's %q", got, pair.remote)
			}
			if got := lab.git("rev-parse", "--abbrev-ref", "HEAD"); got != "mate/"+labMate {
				t.Errorf("the checkout is on %q", got)
			}
			if got := lab.git("rev-parse", "HEAD"); got != pair.head {
				t.Errorf("the checkout moved: HEAD %s, want %s", got, pair.head)
			}
			if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != labCredential {
				t.Errorf("the push source holds %q, want the Mate credential", held)
			}
			if err := exec.CommandContext(t.Context(), "git", "-C", lab.pair, "config", "--get-regexp", `credential\.https://gitea\.`).Run(); err == nil { //nolint:gosec // G204: a test's own git
				t.Errorf("the old Gitea's helper is still there:\n%s", lab.git("config", "--list"))
			}
			assertNoSecretOnDisk(t, lab.stateDir, labCredential)

			elapseHQBackoff(t, lab.stateDir)
			before := lab.hq.callCount("POST /api/mate/repos")
			lab.wire()
			if lab.hq.callCount("POST /api/mate/repos") != before || lab.git("rev-parse", "HEAD") != pair.head {
				t.Errorf("a second pass asked HQ for the repository again or moved the checkout")
			}
		})
	}
}

// TestReconcileHQRepositories_LeavesARemoteMainDidNotSet: only the very
// remote main's zcp set moves. One on another host, in another org, a
// repository main's record does not name, or any remote on a container main
// never wired, is the user's own — HQ is asked nothing, and the pair keeps
// its remote and its token.
func TestReconcileHQRepositories_LeavesARemoteMainDidNotSet(t *testing.T) {
	tests := []struct {
		name     string
		giteaURL string
		remote   string
		record   string
		// sibling is another pair's record, which names the group's org.
		sibling string
	}{
		{name: "a GitHub remote", giteaURL: labGiteaURL, remote: "https://github.com/acme/appdev.git"},
		{name: "another Gitea's host", giteaURL: labGiteaURL, remote: "https://gitea.other.invalid/acme/appdev.git", record: "acme/appdev"},
		{name: "another org on the old Gitea", giteaURL: labGiteaURL, remote: labGiteaURL + "/other/appdev.git", sibling: "acme/apidev"},
		{name: "a repository main's record does not name", giteaURL: labGiteaURL, remote: labGiteaURL + "/acme/shop.git", record: "acme/appdev"},
		{name: "no GITEA_URL", remote: labGiteaURL + "/acme/appdev.git", record: "acme/appdev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lab := newHQLab(t)
			t.Setenv("GITEA_URL", tt.giteaURL)
			lab.mock.WithServiceEnv("svc-appdev", []platform.ServiceEnvVar{{ID: "e1", Key: "GIT_TOKEN", Content: labGiteaToken}})
			if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
				m.GitPushState, m.RemoteURL = topology.GitPushConfigured, tt.remote
				if tt.record != "" {
					m.MainGitea = &workflow.MainGiteaRepo{FullName: tt.record}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if tt.sibling != "" {
				if err := workflow.WriteServiceMeta(lab.stateDir, &workflow.ServiceMeta{
					Hostname: "apidev", MainGitea: &workflow.MainGiteaRepo{FullName: tt.sibling},
				}); err != nil {
					t.Fatal(err)
				}
			}

			if report := lab.wire(); report != nil || len(lab.hq.calls) != 0 {
				t.Errorf("the user's own remote must be left alone, got report=%v calls=%v", report, lab.hq.calls)
			}
			if meta := lab.meta(); meta.RemoteURL != tt.remote || meta.HQ != nil {
				t.Errorf("the pair's record moved: remote %q, HQ %+v", meta.RemoteURL, meta.HQ)
			}
			if held := lab.ssh.gitToken(t.Context(), "svc-appdev"); held != labGiteaToken {
				t.Errorf("the push source holds %q, want its token untouched", held)
			}
		})
	}
}

// TestDeliverHQPair_ContinuesTheChangeMainsMigrationBrought: a stage deploy
// is the first moment a migrated Mate's work moves, with no pass run since
// the switch. The delivery moves the pair off main's Gitea and continues the
// change HQ's import made of its open pull request — no second change — with
// the pull request, the unpushed commit and the new work all on it.
func TestDeliverHQPair_ContinuesTheChangeMainsMigrationBrought(t *testing.T) {
	lab := newHQLab(t)
	pair := lab.fromMain("appdev")
	lab.write(map[string]string{"notes.js": "the work since the switch\n"})

	d := lab.deliver()

	if d == nil || d.Change == nil {
		t.Fatalf("the delivery reached no change: %+v", d)
	}
	if d.Change.Number != 3 || d.Change.Created {
		t.Fatalf("the delivery went to change #%d (created %v), want the imported #3 continued:\n%s", d.Change.Number, d.Change.Created, d.Line)
	}
	if c := lab.hq.change(4); c != nil {
		t.Errorf("a second change was opened: %+v", c)
	}
	head := lab.remoteHead("mate/" + labMate + "/3")
	if head != lab.git("rev-parse", "HEAD") {
		t.Errorf("change #3 is at %q, want the checkout's HEAD", head)
	}
	for _, kept := range []string{pair.pullHead, pair.head} {
		if !lab.descends(labApp, kept, "mate/"+labMate+"/3") {
			t.Errorf("change #3 lost %s", kept)
		}
	}
	if got := lab.git("show", "HEAD:notes.js"); got != "the work since the switch" {
		t.Errorf("the new work is not committed on the change: %q", got)
	}
	if meta := lab.meta(); meta.HQ == nil || meta.HQ.Change != 3 || meta.MainGitea != nil {
		t.Errorf("the pair's record = HQ %+v, main's %+v", meta.HQ, meta.MainGitea)
	}
}

// TestGitPushToHQ_APairOnMainsGiteaPushesToItsChange: a git-push of a pair
// still on main's Gitea never reaches the old Gitea. It moves the pair to HQ
// first and pushes to the change HQ's import made of its open pull request.
func TestGitPushToHQ_APairOnMainsGiteaPushesToItsChange(t *testing.T) {
	lab := newHQLab(t)
	lab.fromMain("appdev")
	ws := workflow.NewWorkSession(labMate, string(workflow.EnvContainer), "Add notes.", []string{"appdev"})
	if err := workflow.SaveWorkSession(lab.stateDir, ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(lab.stateDir, os.Getpid()) })
	lab.write(map[string]string{"notes.js": "notes\n"})
	lab.commit("Add notes")

	text := getTextContent(t, callTool(t, lab.gitPushTool(), "zerops_deploy", map[string]any{"targetService": "appdev", "strategy": "git-push"}))

	if !strings.Contains(text, `"status":"PUSHED"`) || !strings.Contains(text, `"branch":"mate/p-mate/3"`) {
		t.Fatalf("the push must land on the imported change #3:\n%s", text)
	}
	if head := lab.remoteHead("mate/" + labMate + "/3"); head != lab.git("rev-parse", "HEAD") {
		t.Errorf("change #3 is at %q, want the checkout's HEAD", head)
	}
	if got := lab.git("remote", "get-url", "origin"); !strings.HasPrefix(got, lab.hq.srv.URL+"/git/") {
		t.Errorf("origin = %q, want the repository in HQ", got)
	}
}

// TestDeliverHQPair_AbsorbsTheMergeMainRecorded: a pull request merged on
// main whose squash the checkout had not absorbed yet is HQ's merged change
// now, its squash on HQ's `main`. Main's record of that landing comes along
// to HQ's, so the next delivery absorbs it before it takes `main` in — the
// squash shares no history with the branch that became it, and the Mate's
// later edit to the same file would otherwise read as a conflict (MB-26).
func TestDeliverHQPair_AbsorbsTheMergeMainRecorded(t *testing.T) {
	lab := newHQLab(t)
	pair := lab.fromMain("appdev")
	squash := lab.hq.mergeChange("appdev", 3)
	if err := workflow.UpdateServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta) error {
		m.MainGitea.Landed = &workflow.LandedChange{Commit: squash, Head: pair.pullHead}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lab.write(map[string]string{"todo.js": "todos, with due dates\n"})

	d := lab.deliver()

	if d == nil || d.Change == nil || d.Change.Number != 4 || !d.Change.Created {
		t.Fatalf("the delivery must open the next change: %+v", d)
	}
	if !lab.descends(labApp, squash, "mate/"+labMate+"/4") {
		t.Errorf("change #4 does not carry main's squash absorbed")
	}
	if got := lab.git("show", "HEAD:todo.js"); got != "todos, with due dates" {
		t.Errorf("todo.js on the change = %q, want the Mate's later edit", got)
	}
	if meta := lab.meta(); meta.HQ == nil || meta.HQ.Landed != nil {
		t.Errorf("the landing stays recorded after the delivery absorbed it: %+v", meta.HQ)
	}
}

// TestCarryMainGitea: what main's record still owes comes along to the pair's
// HQ record — a landing not absorbed yet, and the Mate's kept words for the
// change that carries its pull request's number now — and nothing the HQ
// record holds already is overwritten.
func TestCarryMainGitea(t *testing.T) {
	t.Parallel()
	landed := &workflow.LandedChange{Commit: "s1", Head: "h1"}
	tests := []struct {
		name  string
		prior workflow.HQRepoRef
		main  *workflow.MainGiteaRepo
		want  workflow.HQRepoRef
	}{
		{name: "no record of main's", prior: workflow.HQRepoRef{Repo: "appdev"}, want: workflow.HQRepoRef{Repo: "appdev"}},
		{
			name: "its pull request is its change",
			main: &workflow.MainGiteaRepo{PullRequest: 3},
			want: workflow.HQRepoRef{Change: 3},
		},
		{
			name: "a landing and words for a pull request",
			main: &workflow.MainGiteaRepo{Landed: landed, ChangeDescription: &workflow.MainGiteaChangeDescription{Text: "Adds todos.", PullRequest: 3}},
			want: workflow.HQRepoRef{Landed: landed, ChangeDescription: &workflow.ChangeDescription{Text: "Adds todos.", Change: 3}},
		},
		{
			name: "words for the next one",
			main: &workflow.MainGiteaRepo{ChangeDescription: &workflow.MainGiteaChangeDescription{Text: "Next."}},
			want: workflow.HQRepoRef{ChangeDescription: &workflow.ChangeDescription{Text: "Next."}},
		},
		{
			name:  "HQ's own record wins",
			prior: workflow.HQRepoRef{Change: 5, Landed: &workflow.LandedChange{Commit: "s2", Head: "h2"}, ChangeDescription: &workflow.ChangeDescription{Text: "HQ's."}},
			main:  &workflow.MainGiteaRepo{PullRequest: 3, Landed: landed, ChangeDescription: &workflow.MainGiteaChangeDescription{Text: "main's"}},
			want:  workflow.HQRepoRef{Change: 5, Landed: &workflow.LandedChange{Commit: "s2", Head: "h2"}, ChangeDescription: &workflow.ChangeDescription{Text: "HQ's."}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.prior
			carryMainGitea(&got, tt.main)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("record = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestDeliverHQPair_LearnsAMergeMainNeverSaw: a pull request merged on the
// old Gitea after main's zcp last looked is in main's record only as its
// number. It comes along as the pair's change on record, so the first
// delivery asks HQ what became of it — HQ's import holds it merged — and
// absorbs its squash before it takes `main` in.
func TestDeliverHQPair_LearnsAMergeMainNeverSaw(t *testing.T) {
	lab := newHQLab(t)
	lab.fromMain("appdev")
	squash := lab.hq.mergeChange("appdev", 3)
	lab.write(map[string]string{"todo.js": "todos, with due dates\n"})

	d := lab.deliver()

	if d == nil || d.Change == nil || d.Change.Number != 4 || !d.Change.Created {
		t.Fatalf("the delivery must open the next change: %+v", d)
	}
	if !lab.descends(labApp, squash, "mate/"+labMate+"/4") {
		t.Errorf("change #4 does not carry the squash absorbed")
	}
	if !strings.Contains(d.Line, "change #3 is merged") {
		t.Errorf("the delivery does not say what became of #3:\n%s", d.Line)
	}
}
