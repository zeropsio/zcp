// Tests for: what a pass, status, develop's start and the handoffs say of a
// Mate's several open changes (tools/hq_change.go hqChangesPass,
// hqChangeStanding, hq_change_status.go, hq_pair.go, workflow_release.go),
// run against a fake HQ that serves real git (hq_lab_test.go).
package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// wirePoolPair records a pair keyed by the dev pool pool, its stage stage,
// wired to the lab's repository — a part of the same monorepo.
func (l *hqLab) wirePoolPair(pool, stage string) {
	l.t.Helper()
	if err := workflow.UpsertServiceMeta(l.stateDir, pool, func(m *workflow.ServiceMeta, _ bool) error {
		m.Hostname, m.Mode, m.Stages = pool, topology.PlanModeStandard, []topology.StageRef{{Hostname: stage}}
		m.BootstrapSession, m.BootstrappedAt, m.GitPushState = "test", "2026-10-02", topology.GitPushConfigured
		m.DevPool = &workflow.DevPoolRef{ServiceID: "svc-" + pool}
		m.HQ = &workflow.HQRepoRef{AppID: labApp, Repo: "appdev", CredentialFingerprint: hq.GitCredentialFingerprint(labCredential)}
		return nil
	}); err != nil {
		l.t.Fatal(err)
	}
}

// passReport runs a pass past the pair's backoff and answers its lines.
func (l *hqLab) passReport() string {
	l.t.Helper()
	elapseHQBackoff(l.t, l.stateDir)
	return strings.Join(l.wire(), "\n")
}

// describedChanges is twoOpenChanges with both changes described, ready for
// review.
func describedChanges(t *testing.T) *hqLab {
	t.Helper()
	lab := twoOpenChanges(t)
	for _, branch := range []string{"fix-login", "add-signup"} {
		if text, isError := lab.describeOn(branch); isError || !strings.Contains(text, `"described":true`) {
			t.Fatalf("describe %s: %s", branch, text)
		}
	}
	return lab
}

// TestHQPass_MergedChangeNamesItsLeasesAndStages: a merged change's note names
// its branch, the leases zcp still holds on that branch — which zcp releases
// on its own once nothing unsaved is left in them, so the note names no
// release for the agent to run — and the stages of the repository's pairs,
// which track main. A pass given no release releases nothing.
func TestHQPass_MergedChangeNamesItsLeasesAndStages(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	lab.wirePoolPair("hqpool", "hqstage")
	lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
	if answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !answer.Created {
		t.Fatalf("open: %s", text)
	}
	lab.leaseOf("hqpool", 4, "fix-login")
	lab.leaseOf("boxpool", 6, "fix-login")
	lab.leaseOf("hqpool", 5, "other-work")
	lab.hq.mergeChange("appdev", 1)

	report := lab.passReport()

	for _, want := range []string{"change #1 is merged", `"fix-login"`, "boxpool-6", "hqpool-4",
		"zcp releases a merged or closed change's leases on its own", "appstage", "hqstage", `sha="origin/main"`} {
		if !strings.Contains(report, want) {
			t.Errorf("the merged note lacks %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, ops.LeaseReleaseBranchCall("fix-login", "", false)) {
		t.Errorf("the merged note names a release for the agent to run:\n%s", report)
	}
	if strings.Contains(report, "hqpool-5") {
		t.Errorf("the merged note names a lease of another branch:\n%s", report)
	}
	if entries, _ := os.ReadDir(filepath.Join(lab.stateDir, "leases")); len(entries) != 3 {
		t.Errorf("the pass released a lease: %d records left, want 3", len(entries))
	}
}

// TestHQPass_OtherChangeBehindAfterMerge_ToldOnce: once one change merges, HQ
// judges the others again; a pass says once that another is behind main and
// still merges cleanly, and how to check it with main — merge origin/main in,
// check, push, describe again — and the next pass says nothing more of it.
func TestHQPass_OtherChangeBehindAfterMerge_ToldOnce(t *testing.T) {
	lab := describedChanges(t)
	if report := lab.passReport(); strings.Contains(report, "#2") {
		t.Fatalf("a ready change in step with main is worth no words:\n%s", report)
	}
	lab.hq.mergeChange("appdev", 1)

	report := lab.passReport()

	for _, want := range []string{"change #1 is merged", `change #2 on "add-signup" is behind main and still merges cleanly`,
		"`zcp git appdev fetch origin && zcp git appdev merge origin/main`", "`zcp git appdev push origin add-signup`", `action="describe-change" branch="add-signup"`} {
		if !strings.Contains(report, want) {
			t.Errorf("the pass lacks %q:\n%s", want, report)
		}
	}
	if again := lab.passReport(); strings.Contains(again, "#2") {
		t.Errorf("the next pass says #2's standing again:\n%s", again)
	}
}

// TestHQPass_ConflictToldWithMergeNotRebase: a change that no longer merges
// cleanly once another merged is told so, with a merge of origin/main and its
// conflicts resolved — never a rebase, never a force.
func TestHQPass_ConflictToldWithMergeNotRebase(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	lab.pushFromMain("fix-login", map[string]string{"app.js": "one way\n"})
	lab.pushFromMain("rework", map[string]string{"app.js": "another way\n"})
	for _, branch := range []string{"fix-login", "rework"} {
		if answer, text, isError := lab.openChangeFrom(branch, "Work on "+branch); isError || !answer.Created {
			t.Fatalf("open %s: %s", branch, text)
		}
		if text, isError := lab.describeOn(branch); isError {
			t.Fatalf("describe %s: %s", branch, text)
		}
	}
	lab.passReport()
	lab.hq.mergeChange("appdev", 1)

	report := lab.passReport()

	for _, want := range []string{`change #2 on "rework" no longer merges cleanly with main`, "in its dev service appdev", "`zcp git appdev fetch origin && zcp git appdev merge origin/main`", "resolve", "never rebase", "never force"} {
		if !strings.Contains(report, want) {
			t.Errorf("the conflict note lacks %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "git rebase") || strings.Contains(report, "--force") {
		t.Errorf("the conflict note asks for a rebase or a force:\n%s", report)
	}
}

// TestHQChangeStanding_SaysOnlyWhatChanged: a change's standing is said only
// when it is worth words — a draft with something pushed, behind main, a
// conflict — and only when it differs from what the agent was last told;
// an HQ that judges nothing says nothing.
func TestHQChangeStanding_SaysOnlyWhatChanged(t *testing.T) {
	t.Parallel()
	yes, no := true, false
	head := "1111111111111111111111111111111111111111"
	at := func(ready *bool, mergeability string, behind *bool, pushed bool) hq.MateChange {
		c := hq.MateChange{Repo: "appsdev", Number: 6, State: hq.ChangeOpen, Branch: "badge", Ready: ready, Mergeability: mergeability, Behind: behind}
		if pushed {
			c.Head = &head
		}
		return c
	}
	told := func(ready *bool, mergeability string, behind bool) *workflow.HQChangeTold {
		return &workflow.HQChangeTold{State: hq.ChangeOpen, Ready: ready, Mergeability: mergeability, Behind: behind}
	}
	tests := []struct {
		name   string
		now    hq.MateChange
		told   *workflow.HQChangeTold
		leases []string
		want   []string // "" none: nothing said
	}{
		{name: "ready, clean, in step", now: at(&yes, "clean", &no, true)},
		{name: "a draft with a push", now: at(&no, "clean", &no, true), want: []string{"change #6 on \"badge\" is a draft", `action="describe-change" branch="badge"`}},
		{name: "a draft told already", now: at(&no, "clean", &no, true), told: told(&no, "clean", false)},
		{name: "nothing pushed yet", now: at(&no, "unknown", &no, false)},
		{name: "behind and clean, with a lease", now: at(&yes, "clean", &yes, true), told: told(&yes, "clean", false), leases: []string{"hqpool-4"},
			want: []string{"behind main and still merges cleanly", "`ssh hqpool-4 \"cd /var/www && git fetch origin && git merge origin/main\"`",
				"`ssh hqpool-4 \"cd /var/www && git push origin badge\"`", "zerops_lease action=sync lease=hqpool-4"}},
		{name: "behind and clean, in its dev service", now: at(&yes, "clean", &yes, true), told: told(&yes, "clean", false),
			want: []string{"behind main and still merges cleanly", "in its dev service appsdev", "`zcp git appsdev fetch origin && zcp git appsdev merge origin/main`",
				"`zcp git appsdev push origin badge`"}},
		{name: "behind told already", now: at(&yes, "clean", &yes, true), told: told(&yes, "clean", true)},
		{name: "a conflict after behind", now: at(&yes, "conflict", &yes, true), told: told(&yes, "clean", true),
			want: []string{"no longer merges cleanly", "never rebase"}},
		{name: "an HQ that judges nothing", now: at(nil, "", nil, true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := workflow.HQChange{AppID: labApp, Repo: "appsdev", Number: 6, Branch: "badge", Told: tt.told}
			words, now := hqChangeStanding(c, tt.now, appsdevRoute(t), tt.leases)
			if strings.Contains(words, "(`git ") || strings.Contains(words, "own shell") {
				t.Errorf("words %q run git outside the dev service or a lease", words)
			}
			if len(tt.want) == 0 && words != "" {
				t.Errorf("said %q, want nothing", words)
			}
			for _, want := range tt.want {
				if !strings.Contains(words, want) {
					t.Errorf("words %q lack %q", words, want)
				}
			}
			if now.State != hq.ChangeOpen || now.Mergeability != tt.now.Mergeability {
				t.Errorf("told = %+v, want the standing read", now)
			}
		})
	}
}

// appsdevRoute is where git runs for appsdev, a repository wired to the pair
// of that name.
func appsdevRoute(t *testing.T) repoRoute {
	t.Helper()
	stateDir := t.TempDir()
	writeWiredPair(t, stateDir, "appsdev", "appsdev")
	return repoDevHost(stateDir, labApp, "appsdev")
}

// The commits of a change HQ brought main into (protocol.md, "A stacked change
// after a squash"): its head before, HQ's merge commit, the squash it brought
// in, and a push past HQ's merge commit.
const (
	corePushFrom   = "1111111111111111111111111111111111111111"
	corePushMerge  = "2222222222222222222222222222222222222222"
	corePushSquash = "3333333333333333333333333333333333333333"
	corePushLater  = "4444444444444444444444444444444444444444"
)

// corePushed is change #6 on badge, ready and clean, at head, carrying HQ's
// push bringing main in after #5 merged when push is set.
func corePushed(head string, push bool, behind bool) hq.MateChange {
	yes := true
	c := hq.MateChange{Repo: "appsdev", Number: 6, State: hq.ChangeOpen, Branch: "badge", Head: &head,
		Ready: &yes, Mergeability: hq.MergeClean, Behind: &behind}
	if push {
		c.CorePush = &hq.CorePush{At: "2026-10-28T10:00:00Z", From: corePushFrom, Head: corePushMerge, Landed: 5, Squash: corePushSquash}
	}
	return c
}

// TestHQChangeStanding_CorePushAtHead_SaidOnce: HQ bringing main into a
// change by a merge commit of its own leaves the agent's branch where it was,
// so its next push is refused as non-fast-forward. While HQ's merge commit is
// the change's head the standing says so once — fetch and fast-forward, where
// the branch is checked out or in each lease of it — and never again for that
// commit; a push past it, or a change without the field, reads as today.
func TestHQChangeStanding_CorePushAtHead_SaidOnce(t *testing.T) {
	t.Parallel()
	const line = "HQ brought main into #6 after #5 merged: fetch and fast-forward `badge` before your next push"
	const ff = "`zcp git appsdev fetch origin && zcp git appsdev merge --ff-only origin/badge`"
	tests := []struct {
		name     string
		now      hq.MateChange
		leases   []string
		want     []string
		never    []string
		wantHead string
	}{
		{name: "at head, the checkout", now: corePushed(corePushMerge, true, false),
			want:  []string{line, ff, "in its dev service appsdev", "`-C /tmp/wt-badge`", "non-fast-forward", "nothing to describe again"},
			never: []string{"git rebase", "--force", "behind main", "your own shell", "never over ssh", "/tmp/badge"}, wantHead: corePushMerge},
		{name: "at head, leases of it", now: corePushed(corePushMerge, true, false), leases: []string{"boxpool-6", "hqpool-4"},
			want: []string{line,
				"`ssh boxpool-6 \"cd /var/www && git fetch origin && git merge --ff-only origin/badge\"`",
				"`ssh hqpool-4 \"cd /var/www && git fetch origin && git merge --ff-only origin/badge\"`"},
			never: []string{"in your own shell", "zcp git"}, wantHead: corePushMerge},
		{name: "at head, and main moved on past the squash", now: corePushed(corePushMerge, true, true),
			want: []string{line, ff, "is behind main"}, wantHead: corePushMerge},
		{name: "pushed past HQ's merge commit", now: corePushed(corePushLater, true, false), never: []string{"HQ brought main"}},
		{name: "no corePush, in step", now: corePushed(corePushLater, false, false), never: []string{"HQ brought main"}},
		{name: "no corePush, behind main", now: corePushed(corePushLater, false, true),
			want: []string{`change #6 on "badge" is behind main and still merges cleanly`}, never: []string{"HQ brought main"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yes := true
			c := workflow.HQChange{AppID: labApp, Repo: "appsdev", Number: 6, Branch: "badge",
				Told: &workflow.HQChangeTold{State: hq.ChangeOpen, Ready: &yes, Mergeability: hq.MergeClean}}

			words, now := hqChangeStanding(c, tt.now, appsdevRoute(t), tt.leases)

			if len(tt.want) == 0 && words != "" {
				t.Errorf("said %q, want nothing", words)
			}
			for _, want := range tt.want {
				if !strings.Contains(words, want) {
					t.Errorf("words %q lack %q", words, want)
				}
			}
			for _, never := range tt.never {
				if strings.Contains(words, never) {
					t.Errorf("words %q say %q", words, never)
				}
			}
			if strings.Contains(words, "HQ brought main") && strings.Contains(words, "behind main") &&
				strings.Index(words, "HQ brought main") > strings.Index(words, "behind main") {
				t.Errorf("words %q bring main in before the fast-forward HQ's push asks for", words)
			}
			if now.CoreHead != tt.wantHead {
				t.Errorf("told HQ's merge commit %q, want %q", now.CoreHead, tt.wantHead)
			}

			c.Told = &now
			if again, _ := hqChangeStanding(c, tt.now, appsdevRoute(t), tt.leases); again != "" {
				t.Errorf("the next pass says again: %q", again)
			}
		})
	}
}

// TestHQPass_CorePushAtHead_ToldOnce: a change cut from another's branch is
// squashed past when that one merges; HQ brings main into it by a merge
// commit of its own, a fast-forward of its branch that keeps it described.
// The pass says once that HQ brought main in — fetch and fast-forward the
// branch before the next push — never that it is behind main or a draft, and
// the next pass says nothing more of it. The command it names takes HQ's
// merge commit, and a push from there goes through.
func TestHQPass_CorePushAtHead_ToldOnce(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
	if answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !answer.Created {
		t.Fatalf("open fix-login: %s", text)
	}
	// Cut from fix-login: it needs the fix.
	lab.pushBranch("add-signup", map[string]string{"signup.js": "the signup\n"})
	if answer, text, isError := lab.openChangeFrom("add-signup", "Add the signup"); isError || !answer.Created {
		t.Fatalf("open add-signup: %s", text)
	}
	for _, branch := range []string{"fix-login", "add-signup"} {
		if text, isError := lab.describeOn(branch); isError || !strings.Contains(text, `"described":true`) {
			t.Fatalf("describe %s: %s", branch, text)
		}
	}
	lab.passReport()
	squash := lab.hq.mergeChange("appdev", 1)
	merge := lab.hq.bringMainIn("appdev", 2, 1, squash)

	report := lab.passReport()

	for _, want := range []string{"change #1 is merged",
		"HQ brought main into #2 after #1 merged: fetch and fast-forward `add-signup` before your next push",
		"`zcp git appdev fetch origin && zcp git appdev merge --ff-only origin/add-signup`", "nothing to describe again"} {
		if !strings.Contains(report, want) {
			t.Errorf("the pass lacks %q:\n%s", want, report)
		}
	}
	for _, never := range []string{`change #2 on "add-signup" is behind main`, `change #2 on "add-signup" is a draft`} {
		if strings.Contains(report, never) {
			t.Errorf("the pass says %q of a change HQ brought main into:\n%s", never, report)
		}
	}
	if kept := lab.kept(2); kept == nil || kept.Told == nil || kept.Told.CoreHead != merge {
		t.Errorf("the ledger keeps #2 told %+v, want HQ's merge commit %s", kept, merge)
	}
	if again := lab.passReport(); strings.Contains(again, "#2") {
		t.Errorf("the next pass says #2 again:\n%s", again)
	}

	lab.git("checkout", "-q", "add-signup")
	if out, err := lab.ssh.ExecSSH(t.Context(), "appdev", "cd /var/www && git fetch origin && git merge --ff-only origin/add-signup"); err != nil {
		t.Fatalf("the fast-forward the pass names: %v\n%s", err, out)
	}
	if head := lab.git("rev-parse", "HEAD"); head != merge {
		t.Fatalf("the fast-forward left add-signup at %s, want HQ's merge commit %s", head, merge)
	}
	lab.pushBranch("add-signup", map[string]string{"signup.js": "the signup, polished\n"})
}

// TestLeaseGroupChanges_CorePushAtHead_SaysItInEachLease: lease status names
// a group's change HQ brought main into the way the pass says it — fetch and
// fast-forward the branch before the next push, in each lease of the group,
// over ssh — read from the Mate's state now, or from what the pass last told
// while HQ does not answer; a change without it reads as today.
func TestLeaseGroupChanges_CorePushAtHead_SaysItInEachLease(t *testing.T) {
	t.Parallel()
	stateDir := kara(t)
	yes := true
	if err := workflow.RecordHQChange(stateDir, workflow.HQChange{AppID: labApp, Repo: "appsdev", Number: 6, Branch: "mate-test-badge",
		Told: &workflow.HQChangeTold{State: hq.ChangeOpen, Ready: &yes, Mergeability: hq.MergeClean, CoreHead: corePushMerge, CoreLanded: 5}}); err != nil {
		t.Fatal(err)
	}
	if err := workflow.RecordHQChange(stateDir, workflow.HQChange{AppID: labApp, Repo: "appsdev", Number: 2, Branch: "pool-previews",
		Told: &workflow.HQChangeTold{State: hq.ChangeOpen, Ready: &yes, Mergeability: hq.MergeClean}}); err != nil {
		t.Fatal(err)
	}
	views := []ops.LeaseView{{Alias: "hqpool-4", Branch: "mate-test-badge"}, {Alias: "boxpool-6", Branch: "mate-test-badge"}, {Alias: "hqpool-3", Branch: "pool-previews"}}
	head, previews := corePushMerge, corePushLater
	app := labApp
	badge := corePushed(head, true, false)
	badge.Branch = "mate-test-badge"
	state := &hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{badge,
		{Repo: "appsdev", Number: 2, State: hq.ChangeOpen, Branch: "pool-previews", Head: &previews, Ready: &yes, Mergeability: hq.MergeClean}}}

	for name, read := range map[string]func() *hq.MateState{
		"read now":     func() *hq.MateState { return state },
		"as last read": func() *hq.MateState { return nil },
	} {
		got := leaseGroupChanges(stateDir, targetHQ, views, read)

		for _, want := range []string{"group appsdev@mate-test-badge: #6 (ready)",
			"HQ brought main into #6 after #5 merged: fetch and fast-forward `mate-test-badge` before your next push",
			"`ssh boxpool-6 \"cd /var/www && git fetch origin && git merge --ff-only origin/mate-test-badge\"`",
			"`ssh hqpool-4 \"cd /var/www && git fetch origin && git merge --ff-only origin/mate-test-badge\"`",
			"group appsdev@pool-previews: #2 (ready)"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: lease status lacks %q: %s", name, want, got)
			}
		}
		if strings.Count(got, "HQ brought main") != 1 || strings.Contains(got, "hqpool-3 \"") {
			t.Errorf("%s: lease status says HQ's push of another group's change: %s", name, got)
		}
	}
}

// TestLeaseSyncRefusal_AfterHQsPush_NamesTheCause: a lease sync's push
// refused as non-fast-forward names HQ's own push as the cause when the
// change on the lease's branch carries it at its head — bring origin's branch
// in over ssh, a fast-forward when the lease has no commits of its own since,
// never a rebase, then sync again; the Mate's state is read once, and only
// for that refusal. Any other refusal, a change without it, or no reader
// leave the refusal as it is.
func TestLeaseSyncRefusal_AfterHQsPush_NamesTheCause(t *testing.T) {
	t.Parallel()
	stateDir := kara(t)
	app := labApp
	nonFF := func() error {
		return platform.NewPlatformError(platform.ErrGitPushNonFastForward,
			"Origin's mate-test-badge has commits lease hqpool-4 lacks ([rejected] (fetch first)), so nothing was pushed and no other lease was touched",
			"Integrate origin/mate-test-badge in hqpool-4 over ssh, then sync again: zerops_lease action=sync lease=hqpool-4")
	}
	stateWith := func(push bool) func() *hq.MateState {
		badge := corePushed(corePushMerge, push, false)
		badge.Branch = "mate-test-badge"
		return func() *hq.MateState {
			return &hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{badge}}
		}
	}

	reads := 0
	read := stateWith(true)
	named := corePushSyncRefusal(nonFF(), stateDir, targetHQ, "hqpool-4", func() *hq.MateState { reads++; return read() })

	pe := refusalOf(t, named)
	for _, want := range []string{"HQ brought main into #6 after #5 merged",
		"`ssh hqpool-4 \"cd /var/www && git fetch origin && git merge --no-edit origin/mate-test-badge\"`",
		"never rebase", ops.LeaseSyncCall("hqpool-4")} {
		if !strings.Contains(pe.Suggestion, want) {
			t.Errorf("the refusal's suggestion lacks %q: %s", want, pe.Suggestion)
		}
	}
	if pe.Code != platform.ErrGitPushNonFastForward || !strings.Contains(pe.Message, "fetch first") {
		t.Errorf("the refusal lost its code or message: %+v", pe)
	}
	if reads != 1 {
		t.Errorf("the Mate's state was read %d times, want once", reads)
	}

	if plain := refusalOf(t, corePushSyncRefusal(nonFF(), stateDir, targetHQ, "hqpool-4", stateWith(false))); strings.Contains(plain.Suggestion, "HQ brought main") {
		t.Errorf("a change without HQ's push names it: %s", plain.Suggestion)
	}
	if none := refusalOf(t, corePushSyncRefusal(nonFF(), stateDir, targetHQ, "hqpool-4", nil)); strings.Contains(none.Suggestion, "HQ brought main") {
		t.Errorf("with no reader the refusal names HQ's push: %s", none.Suggestion)
	}
	other := platform.NewPlatformError(platform.ErrPermissionDenied, "Origin refused branch mate-test-badge", "Push a branch of your own")
	reads = 0
	if got := corePushSyncRefusal(other, stateDir, targetHQ, "hqpool-4", func() *hq.MateState { reads++; return read() }); got != other || reads != 0 {
		t.Errorf("another refusal was changed (%v) or read the Mate's state %d times", got, reads)
	}
}

// TestDevelopStart_ListsTheOpenChangesOnAnHQThatKeepsOnePerBranch: on an HQ
// that keeps an open change per branch, develop's start lists the
// repository's open changes by number and branch, and says new work goes on
// a branch of its own — never that work goes on an open change's branch.
func TestDevelopStart_ListsTheOpenChangesOnAnHQThatKeepsOnePerBranch(t *testing.T) {
	lab := twoOpenChanges(t)
	lab.mock.WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})

	text := lab.developStart()

	for _, want := range []string{`#1 on "fix-login"`, `#2 on "add-signup"`, "new work goes on a branch of its own", "cut from an up-to-date",
		"`zcp git appdev fetch origin <branch> && zcp git appdev checkout <branch>`",
		"`zcp git appdev checkout main && zcp git appdev fetch origin && zcp git appdev merge --ff-only origin/main && zcp git appdev checkout -b <branch>`"} {
		if !strings.Contains(text, want) {
			t.Errorf("develop's start lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "rather than on a branch cut from") {
		t.Errorf("develop's start sends new work onto an open change's branch:\n%s", text)
	}
}

// TestDevelopStart_ListsEveryOpenChangeOfTheMate_RecordedOrNot: develop's
// start lists every open change the Mate's state names in the repository —
// one opened outside this zcp, which its ledger never recorded, too — since
// every one of them counts toward the open changes HQ keeps a Mate there.
func TestDevelopStart_ListsEveryOpenChangeOfTheMate_RecordedOrNot(t *testing.T) {
	lab := twoOpenChanges(t)
	lab.mock.WithServices([]platform.ServiceStack{
		{ID: "svc-appdev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{ID: "svc-appstage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	})
	hqc, ok := openHQ(lab.hq.srv.Client(), lab.rt)
	if !ok {
		t.Fatal("the lab's Mate is not linked")
	}
	// A released zcp opens its change naming no branch: HQ's default one.
	opened, err := hqc.OpenChange(t.Context(), "appdev", "Opened by a released zcp", "")
	if err != nil || !opened.Created {
		t.Fatalf("open the default change: %+v (%v)", opened, err)
	}

	text := lab.developStart()

	for _, want := range []string{`#1 on "fix-login"`, `#2 on "add-signup"`, `#3 on "mate/` + labMate + `/3"`} {
		if !strings.Contains(text, want) {
			t.Errorf("develop's start lacks %q:\n%s", want, text)
		}
	}
}

// TestStatus_OneBlockPerRepository: status names a repository's changes in one
// block — each by number and branch, its standing as last told, and the
// leases on its branch — and the branches leases hold with no change yet;
// pairs no longer each name a change.
func TestStatus_OneBlockPerRepository(t *testing.T) {
	lab := twoOpenChanges(t)
	lab.leaseOf("hqpool", 4, "fix-login")
	lab.leaseOf("boxpool", 6, "fix-login")
	lab.leaseOf("boxpool", 7, "build-stamp")
	if text, isError := lab.describeOn("add-signup"); isError {
		t.Fatalf("describe: %s", text)
	}
	// This block's ready-only expectation is for a change HQ has judged clean.
	lab.hq.mu.Lock()
	lab.hq.rejudge("appdev")
	lab.hq.mu.Unlock()
	lab.passReport()

	status := lab.status()

	want := "HQ appdev: #1 fix-login (draft) — leases boxpool-6, hqpool-4; #2 add-signup (ready); branch build-stamp — leases boxpool-7: no change yet"
	if !strings.Contains(status, want) {
		t.Errorf("status lacks the repository's block\n  %s\n%s", want, status)
	}
	for _, never := range []string{"change=", "changeBranch="} {
		if strings.Contains(status, never) {
			t.Errorf("a pair still names %q:\n%s", never, status)
		}
	}
}

// TestStatus_ChangesReadFresh_AsLastReadWhileHQIsDown: status reads the
// Mate's own state once for its Changes section, so a change merged since the
// last pass reads merged, never open, and another one HQ judged behind main
// since reads behind — while no pass has run. The read writes nothing: the
// next pass still says the merge once. HQ not answering leaves the ledger's
// section, labelled as last read.
func TestStatus_ChangesReadFresh_AsLastReadWhileHQIsDown(t *testing.T) {
	lab := describedChanges(t)
	lab.leaseOf("hqpool", 4, "fix-login")
	lab.passReport()
	lab.hq.mergeChange("appdev", 1)
	reads := lab.hq.callCount("GET /api/mate/self")

	status := lab.status()

	for _, want := range []string{"#1 fix-login — leases hqpool-4: merged", "#2 add-signup (ready · behind main)"} {
		if !strings.Contains(status, want) {
			t.Errorf("status lacks %q:\n%s", want, status)
		}
	}
	if strings.Contains(status, "#1 fix-login (") {
		t.Errorf("status names merged #1 with an open change's standing:\n%s", status)
	}
	if n := lab.hq.callCount("GET /api/mate/self") - reads; n != 1 {
		t.Errorf("status read the Mate's state %d times, want once", n)
	}
	if lab.kept(1) == nil {
		t.Fatal("status forgot change #1: saying the merge, once, is the pass's")
	}
	if report := lab.passReport(); !strings.Contains(report, "change #1 is merged") {
		t.Errorf("the pass after status does not say the merge:\n%s", report)
	}

	lab.hq.mergeChange("appdev", 2)
	lab.hq.setDown(true)
	status = lab.status()

	if !strings.Contains(status, "as last read") || !strings.Contains(status, "#2 add-signup (ready · behind main)") {
		t.Errorf("with HQ down status does not name the ledger's changes as last read:\n%s", status)
	}
}

// TestLaunchProduction_ListsEveryOpenChangeOnce: four pairs on one repository
// with two open changes: the launch refusal names the repository's changes
// once each, never once per pair.
func TestLaunchProduction_ListsEveryOpenChangeOnce(t *testing.T) {
	lab := twoOpenChanges(t)
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

	text := resultText(t, hqLaunchProductionRefusal(context.Background(), lab.hq.srv.Client(), lab.rt, lab.stateDir, true))

	if !strings.Contains(text, "appdev #1 and #2") {
		t.Errorf("the refusal does not name appdev #1 and #2: %s", text)
	}
	// The refusal carries its words twice (guidance and blocker): each copy
	// names #1 once.
	if n, copies := strings.Count(text, "#1"), strings.Count(text, "Tell the person to merge"); n != copies {
		t.Errorf("the refusal names #1 %d times in %d copies of its words: %s", n, copies, text)
	}
}

// TestLeaseGroupChanges_NamesEachGroupsChange: zerops_lease status names each
// group's change from the ledger, with no call to HQ — its number and its
// standing as last told — and a group whose branch has none yet.
func TestLeaseGroupChanges_NamesEachGroupsChange(t *testing.T) {
	t.Parallel()
	stateDir := kara(t)
	no := false
	if err := workflow.RecordHQChange(stateDir, workflow.HQChange{AppID: labApp, Repo: "appsdev", Number: 6, Branch: "mate-test-badge",
		Told: &workflow.HQChangeTold{State: hq.ChangeOpen, Ready: &no}}); err != nil {
		t.Fatal(err)
	}
	views := []ops.LeaseView{{Alias: "hqpool-4", Branch: "mate-test-badge"}, {Alias: "boxpool-6", Branch: "mate-test-badge"}, {Alias: "hqpool-3", Branch: "pool-previews"}}

	got := leaseGroupChanges(stateDir, targetHQ, views, nil)

	for _, want := range []string{"group appsdev@mate-test-badge: #6 (draft)", "group appsdev@pool-previews: no change yet"} {
		if !strings.Contains(got, want) {
			t.Errorf("lease status lacks %q: %s", want, got)
		}
	}
	if strings.Count(got, "mate-test-badge") != 1 {
		t.Errorf("a group is named once per lease, not once: %s", got)
	}
}

// TestLeaseStatus_MergedBranchSaysSo_NotNoChangeYet: a lease on a branch whose
// change merged is never said to have no change yet, which invites a new
// change on merged work. Lease status reads the Mate's state once and says
// the change merged — release the lease, or start new work from main —
// whether a pass told the merge and forgot the change, or none ran since;
// status names the merged change on that branch the same way.
func TestLeaseStatus_MergedBranchSaysSo_NotNoChangeYet(t *testing.T) {
	lab := twoOpenChanges(t)
	told := lab.leaseOf("hqpool", 4, "fix-login")
	unseen := lab.leaseOf("boxpool", 6, "add-signup")
	lab.hq.mergeChange("appdev", 1)
	if report := lab.passReport(); !strings.Contains(report, "change #1 is merged") || lab.kept(1) != nil {
		t.Fatalf("the pass did not tell #1's merge and forget it:\n%s", report)
	}
	lab.hq.mergeChange("appdev", 2)
	views := []ops.LeaseView{{Alias: told, Branch: "fix-login"}, {Alias: unseen, Branch: "add-signup"}}

	got := leaseGroupChanges(lab.stateDir, lab.hq.srv.URL, views, func() *hq.MateState { return hqStateNow(t.Context(), lab.hq.srv.Client(), lab.rt) })

	for _, want := range []string{"group appdev@fix-login: change #1 merged — release this lease", "group appdev@add-signup: change #2 merged — release this lease", "or start new work from main"} {
		if !strings.Contains(got, want) {
			t.Errorf("lease status lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, "no change yet") || strings.Contains(got, "as last read") {
		t.Errorf("lease status reads a merged branch as having no change, or as last read: %s", got)
	}
	status := lab.status()
	if !strings.Contains(status, "#1 fix-login — leases hqpool-4: merged") || strings.Contains(status, "no change yet") {
		t.Errorf("status does not name #1 merged on fix-login:\n%s", status)
	}
}

// TestLeaseRelease_ResultNamesTheReleasedBranchsChange: a lease release's
// result names the change on each released lease's branch, read the way lease
// status reads it — once, through the reader it is handed — so a change merged
// since the last pass reads merged, never still open (rehearsal 2026-10-25, O4:
// the agent told the person a merged change was open), naming the branch's
// leases still held with the branch release; an open change reads open with
// its standing. HQ not answering, it is the ledger's, as last read.
func TestLeaseRelease_ResultNamesTheReleasedBranchsChange(t *testing.T) {
	lab := twoOpenChanges(t)
	released := lab.leaseOf("webpool", 4, "fix-login")
	kept := lab.leaseOf("apipool", 4, "fix-login")
	other := lab.leaseOf("webpool", 5, "add-signup")
	lab.hq.mergeChange("appdev", 1)
	hqc, ok := openHQ(lab.hq.srv.Client(), lab.rt)
	if !ok {
		t.Fatal("the lab's Mate is not linked")
	}
	remote, err := hqc.RepoURL(labApp, "appdev")
	if err != nil {
		t.Fatal(err)
	}
	repo := topology.RepoIdentity(remote)
	// The release dropped the records of the leases it released.
	for _, alias := range []string{released, other} {
		if err := os.Remove(filepath.Join(lab.stateDir, "leases", alias+".json")); err != nil {
			t.Fatal(err)
		}
	}
	views := []ops.LeaseView{
		{Alias: released, Repo: repo, Branch: "fix-login", State: ops.LeaseStateReleased},
		{Alias: other, Repo: repo, Branch: "add-signup", State: ops.LeaseStateReleased},
	}
	reads := 0

	got := leaseReleaseChanges(lab.stateDir, lab.hq.srv.URL, views, func() *hq.MateState {
		reads++
		return hqStateNow(t.Context(), lab.hq.srv.Client(), lab.rt)
	})

	for _, want := range []string{"group appdev@fix-login: change #1 merged", kept, ops.LeaseReleaseBranchCall("fix-login", "", false),
		"or start new work from main", "group appdev@add-signup: #2"} {
		if !strings.Contains(got, want) {
			t.Errorf("the release result lacks %q: %s", want, got)
		}
	}
	if strings.Contains(got, released) || strings.Contains(got, "as last read") || strings.Contains(got, "no change yet") {
		t.Errorf("the release result names the released lease as held, or reads HQ as unanswered: %s", got)
	}
	if reads != 1 {
		t.Errorf("the Mate's state was read %d times, want once", reads)
	}

	unanswered := leaseReleaseChanges(lab.stateDir, lab.hq.srv.URL, views[1:], func() *hq.MateState { return nil })
	if !strings.Contains(unanswered, "as last read") || !strings.Contains(unanswered, "group appdev@add-signup: #2") {
		t.Errorf("with HQ not answering the release result is not the ledger's, as last read: %s", unanswered)
	}
}

// TestLeaseRelease_MergedChangeNamesTheStageCall: a stage tracks main, so the
// release of a branch whose change is merged names the call that puts main on
// the repository's stage — the merged note may have reached the agent before
// the last merge only (global HQ 2026-10-26, D7: the stage stayed one merge
// behind main after the agent released the last branch's leases). A branch
// whose change is still open, or a repository whose pairs have no stage,
// names no stage call.
func TestLeaseRelease_MergedChangeNamesTheStageCall(t *testing.T) {
	lab := twoOpenChanges(t)
	merged := lab.leaseOf("webpool", 4, "fix-login")
	open := lab.leaseOf("webpool", 5, "add-signup")
	lab.hq.mergeChange("appdev", 1)
	hqc, ok := openHQ(lab.hq.srv.Client(), lab.rt)
	if !ok {
		t.Fatal("the lab's Mate is not linked")
	}
	remote, err := hqc.RepoURL(labApp, "appdev")
	if err != nil {
		t.Fatal(err)
	}
	repo := topology.RepoIdentity(remote)
	for _, alias := range []string{merged, open} {
		if err := os.Remove(filepath.Join(lab.stateDir, "leases", alias+".json")); err != nil {
			t.Fatal(err)
		}
	}
	views := []ops.LeaseView{
		{Alias: merged, Repo: repo, Branch: "fix-login", State: ops.LeaseStateReleased},
		{Alias: open, Repo: repo, Branch: "add-signup", State: ops.LeaseStateReleased},
	}
	read := func() *hq.MateState { return hqStateNow(t.Context(), lab.hq.srv.Client(), lab.rt) }

	got := leaseReleaseChanges(lab.stateDir, lab.hq.srv.URL, views, read)

	mergedLine, openLine, found := strings.Cut(got, "group appdev@add-signup")
	if !found {
		t.Fatalf("the release result names no line for add-signup: %s", got)
	}
	for _, want := range []string{"group appdev@fix-login: change #1 merged", "appstage", `zerops_deploy sourceService=`, `targetService="appstage" sha="origin/main"`} {
		if !strings.Contains(mergedLine, want) {
			t.Errorf("the merged branch's line lacks %q: %s", want, mergedLine)
		}
	}
	if strings.Contains(openLine, "zerops_deploy") || strings.Contains(openLine, "appstage") {
		t.Errorf("the open change's line names a stage call: %s", openLine)
	}

	if err := workflow.UpsertServiceMeta(lab.stateDir, "appdev", func(m *workflow.ServiceMeta, _ bool) error {
		m.Stages = nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if noStage := leaseReleaseChanges(lab.stateDir, lab.hq.srv.URL, views[:1], read); !strings.Contains(noStage, "change #1 merged") || strings.Contains(noStage, "zerops_deploy") {
		t.Errorf("with no stage the release result still names a stage call, or not the merge: %s", noStage)
	}
}

// TestStatusHQChanges_OutsideAMate_AsLastRead: outside a Mate (an enrollment
// left in $HOME, pairs keeping their HQ records) status reads nothing of HQ,
// so the ledger's Changes section is labelled as last read, never as now.
func TestStatusHQChanges_OutsideAMate_AsLastRead(t *testing.T) {
	t.Parallel()
	stateDir := kara(t)
	if err := workflow.RecordHQChange(stateDir, workflow.HQChange{AppID: labApp, Repo: "appsdev", Number: 2, Branch: "pool-previews"}); err != nil {
		t.Fatal(err)
	}

	changes, asLastRead, _ := statusHQChanges(context.Background(), nil, runtime.Info{InContainer: true}, stateDir, targetHQ, time.Now(), true)

	if len(changes) == 0 || !asLastRead {
		t.Errorf("outside a Mate status names %+v with asLastRead=%v, want the ledger's, as last read", changes, asLastRead)
	}
	if changes, asLastRead, _ := statusHQChanges(context.Background(), nil, runtime.Info{InContainer: true}, t.TempDir(), targetHQ, time.Now(), true); len(changes) != 0 || asLastRead {
		t.Errorf("with nothing to name status says %+v, asLastRead=%v", changes, asLastRead)
	}
}

// TestHandleRelease_NamesTheOpenChangesItLeavesOut: a release takes main only,
// so the person's handoff names the open changes the ledger keeps, per
// repository, as not in it.
func TestHandleRelease_NamesTheOpenChangesItLeavesOut(t *testing.T) {
	t.Parallel()
	stateDir := kara(t)
	for _, n := range []int{2, 6} {
		if err := workflow.RecordHQChange(stateDir, workflow.HQChange{AppID: labApp, Repo: "appsdev", Number: n, Branch: "b"}); err != nil {
			t.Fatal(err)
		}
	}

	result, _, _ := handleRelease(context.Background(), nil, "", nil, nil, WorkflowInput{}, stateDir, runtime.Info{MateEnabled: true})

	text := resultText(t, result)
	for _, want := range []string{"release-person-required", "appsdev #2 and #6", "not in"} {
		if !strings.Contains(text, want) {
			t.Errorf("the handoff lacks %q: %s", want, text)
		}
	}
}

// release is zerops_workflow action="release" in the lab.
func (l *hqLab) release() string {
	l.t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0.1"}, nil)
	RegisterWorkflow(srv, l.mock, l.hq.srv.Client(), labMate, nil,
		workflow.NewEngine(l.stateDir, workflow.EnvContainer, nil), nil, l.stateDir, "", nil, l.ssh, l.rt, "")
	return getTextContent(l.t, callTool(l.t, srv, "zerops_workflow", map[string]any{"action": "release"}))
}

// TestHandleRelease_LeavesOutTheChangesHQSaysSettled: the person merges a
// change and asks for a release before any pass forgot it. The handoff reads
// the Mate's state once and names only the changes still open — never the
// merged one as left out — and writes nothing, so the pass still says the
// merge once. HQ not answering, it names the ledger's, as last read.
func TestHandleRelease_LeavesOutTheChangesHQSaysSettled(t *testing.T) {
	lab := twoOpenChanges(t)
	lab.hq.mergeChange("appdev", 1)
	reads := lab.hq.callCount("GET /api/mate/self")

	text := lab.release()

	if !strings.Contains(text, "release-person-required") || !strings.Contains(text, "appdev #2 are not in it") {
		t.Errorf("the handoff does not name open #2 as left out: %s", text)
	}
	if strings.Contains(text, "#1") {
		t.Errorf("the handoff names merged #1 as an open change left out: %s", text)
	}
	if n := lab.hq.callCount("GET /api/mate/self") - reads; n != 1 {
		t.Errorf("the handoff read the Mate's state %d times, want once", n)
	}
	if lab.kept(1) == nil {
		t.Error("the handoff forgot change #1: saying the merge, once, is the pass's")
	}

	lab.hq.setDown(true)
	text = lab.release()

	if !strings.Contains(text, "as last read") || !strings.Contains(text, "#1 and #2") {
		t.Errorf("with HQ down the handoff does not name the ledger's changes as last read: %s", text)
	}
}

// TestHQPass_ChangesTakeTheirTurnWithNoWiredPair: a Mate whose work is only in
// leases wires no pair (a dev-pool-only project). Its pass still takes the
// changes' turn while the ledger or an HQ lease names a repository: a change
// merged since is learned, said and forgotten, and a change a person handed
// the Mate in a leased repository is taken — never held for a pair's turn
// that never comes.
func TestHQPass_ChangesTakeTheirTurnWithNoWiredPair(t *testing.T) {
	lab := newHQLab(t)
	lab.hq.keepOpenPerBranch()
	lab.wire()
	lab.pushFromMain("fix-login", map[string]string{"login.js": "the fix\n"})
	if answer, text, isError := lab.openChangeFrom("fix-login", "Fix the login"); isError || !answer.Created {
		t.Fatalf("open: %s", text)
	}
	lab.leaseOf("hqpool", 4, "fix-login")
	if err := os.Remove(filepath.Join(lab.stateDir, "services", "appdev.json")); err != nil {
		t.Fatal(err)
	}
	lab.hq.mergeChange("appdev", 1)

	report := strings.Join(lab.wire(), "\n")

	if !strings.Contains(report, "change #1 is merged") || !strings.Contains(report, "hqpool-4") {
		t.Errorf("with no wired pair the pass does not say #1 merged, naming its lease:\n%s", report)
	}
	if lab.kept(1) != nil {
		t.Error("with no wired pair the pass kept merged #1")
	}

	number := lab.hq.handOver("appdev", "Add the signup", "add-signup", map[string]string{"signup.js": "half a signup\n"})
	report = strings.Join(lab.wire(), "\n")

	if !strings.Contains(report, "handed") || lab.kept(number) == nil {
		t.Errorf("with an empty ledger and a lease of appdev the pass did not take handed #%d:\n%s", number, report)
	}
}
