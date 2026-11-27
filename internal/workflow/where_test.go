package workflow

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/topology"
)

// WH-COPY — The where text names each working copy with its part, branch, what is uncommitted and unpushed, its change and each dev server's state, the caller's own first and marked, and is empty when there is nothing to name.
// witness: internal/workflow/where.go | worktree.Name == alias → worktree.Part == alias
func TestWhere_WHCOPY_NamesWorkingCopies(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	two := 2
	change := &HQChangeLine{Number: 42, Branch: "feat/b", Labels: []string{"draft", "behind main"}}
	copies := []WhereCopy{
		{Name: "pool-1", Part: "api", Branch: "feat/a", Lease: true, State: "held", Unsaved: &topology.LeaseUnsaved{FileCount: 2, UnpushedCommits: 3}, HasHQ: true, DevServers: []topology.DevServerState{{Name: "api", Status: "running", URL: "http://api:3000"}, {Name: "jobs", Status: "stopped"}}},
		{Name: "pool-2", Part: "web", Branch: "feat/b", Lease: true, State: "held", Unsaved: &topology.LeaseUnsaved{}, HasHQ: true, Change: change, DevServers: []topology.DevServerState{}},
		{Name: "webdev", Part: "web", Branch: "main", Dirty: true, DevServers: []topology.DevServerState{{Name: "web", Status: "starting"}}},
	}
	header := "Where the work stands, read by zcp at 2026-11-25T12:00:00Z. A tool result you received after that is newer."
	first := "pool-1: part api, branch feat/a, 2 files uncommitted, 3 commits unpushed, no change yet; dev server api running at http://api:3000, dev server jobs stopped"
	second := "pool-2: part web, branch feat/b, clean, change #42 (draft · behind main); no dev server"
	dev := "webdev: part web, branch main, uncommitted changes; dev server web starting"
	for _, tc := range []struct {
		name  string
		facts WhereFacts
		want  string
	}{
		{"named second", WhereFacts{ReadAt: at, Lease: "pool-2", Copies: copies}, header + "\n" + strings.Replace(second, "pool-2:", "pool-2 (this conversation's):", 1) + "\n" + first + "\n" + dev},
		{"no alias", WhereFacts{ReadAt: at, Copies: copies}, header + "\n" + first + "\n" + second + "\n" + dev},
		{"unknown alias", WhereFacts{ReadAt: at, Lease: "missing", Copies: copies}, header + "\n" + first + "\n" + second + "\n" + dev},
		{"base has unpushed commits", WhereFacts{ReadAt: at, Copies: []WhereCopy{{Name: "appdev", Part: "api", Branch: "main", NothingToHandOver: true, BaseUnpushed: &two, DevServers: []topology.DevServerState{}}}}, header + "\nappdev: part api, branch main, 2 commits unpushed; no dev server"},
		{"base unpushed unread", WhereFacts{ReadAt: at, Copies: []WhereCopy{{Name: "appdev", Part: "api", Branch: "main", NothingToHandOver: true, DevServers: []topology.DevServerState{}}}}, header + "\nappdev: part api, branch main, unpushed commits not read; no dev server"},
		{"base dirty and unpushed", WhereFacts{ReadAt: at, Copies: []WhereCopy{{Name: "appdev", Part: "api", Branch: "main", Dirty: true, NothingToHandOver: true, BaseUnpushed: &two, DevServers: []topology.DevServerState{}}}}, header + "\nappdev: part api, branch main, uncommitted changes, 2 commits unpushed; no dev server"},
		{"parked and landed", WhereFacts{ReadAt: at, Copies: []WhereCopy{{Name: "pool-3", Part: "api", Branch: "feat/c", Lease: true, State: "parked", Unverified: true, HasHQ: true, Change: &HQChangeLine{Number: 43, LandedWith: 41}, ServersUnread: "lease parked"}}, Changes: []WhereChange{{HQChangeLine: HQChangeLine{Number: 44, Branch: "feat/d", Labels: []string{"draft"}}}}}, header + "\n" + "pool-3: part api, branch feat/c, parked (unverified), change #43 merged as part of #41; dev servers not read (lease parked)\nchange #44 feat/d (draft): no working copy"},
		{"empty", WhereFacts{ReadAt: at}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RenderWhere(tc.facts); got != tc.want {
				t.Errorf("where text:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// WH-DETACHED — A detached working copy's branch is named as detached, while a named branch keeps its name.
// witness: internal/workflow/where.go | branch = "(detached)" → branch = ""
func TestWhere_WHDetached_NamesTheBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		branch string
		want   string
	}{
		{branch: "", want: "dev: part api, branch (detached), clean; no dev server"},
		{branch: "feature", want: "dev: part api, branch feature, clean; no dev server"},
	} {
		t.Run(tc.branch, func(t *testing.T) {
			t.Parallel()
			workcopy := WhereCopy{Name: "dev", Part: "api", Branch: tc.branch, DevServers: []topology.DevServerState{}}
			if got := whereCopyLine(workcopy, false, 0); got != tc.want {
				t.Errorf("line = %q; want %q", got, tc.want)
			}
		})
	}
}

// witness: internal/workflow/where.go | if count == 1 { → if count == 0 {
func TestWhere_WHCounts_NamesSingularAndPluralWork(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		count int
		want  string
	}{
		{"one", 1, "pool-1: part api, branch feat, 1 file uncommitted, 1 commit unpushed; no dev server"},
		{"many", 2, "pool-1: part api, branch feat, 2 files uncommitted, 2 commits unpushed; no dev server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			worktree := WhereCopy{Name: "pool-1", Part: "api", Branch: "feat", Lease: true, State: "held", Unsaved: &topology.LeaseUnsaved{FileCount: tc.count, UnpushedCommits: tc.count}, DevServers: []topology.DevServerState{}}
			if got := whereCopyLine(worktree, false, 0); got != tc.want {
				t.Errorf("line = %q, want %q", got, tc.want)
			}
		})
	}
}

// WH-UNREAD — A fact that was not read says not read with why, never clean, none or nothing.
// witness: internal/workflow/where.go | work = "working copy not read (no answer)" → work = "clean"
func TestWhere_WHUNREAD_ExplainsMissingFacts(t *testing.T) {
	t.Parallel()
	facts := WhereFacts{
		ReadAt: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC),
		Copies: []WhereCopy{{Name: "pool-1", Part: "api", Branch: "feat", Lease: true, State: "held", ServersUnread: "ssh refused"}},
		Stages: []WhereStage{{Hostname: "apistage", Part: "api", Unread: "platform refused"}},
	}
	want := "Where the work stands, read by zcp at 2026-11-25T12:00:00Z. A tool result you received after that is newer.\n" +
		"pool-1: part api, branch feat, working copy not read (no answer); dev servers not read (ssh refused)\n" +
		"stage apistage (part api) version not read (platform refused)"
	if got := RenderWhere(facts); got != want {
		t.Errorf("where text:\n%s\nwant:\n%s", got, want)
	}
}

// WH-STAGE — A stage's line says the name of the version it runs exactly as the platform holds it, and says when it runs none.
// witness: internal/workflow/where.go | version := "runs " + stage.Version → version := "runs " + strings.Split(stage.Version, " ")[0]
func TestWhere_WHSTAGE_PreservesActiveVersionName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ version, want string }{
		{"main 7e2d4c1", "runs main 7e2d4c1"},
		{"feat/x 1a2b3c4-dirty", "runs feat/x 1a2b3c4-dirty"},
		{"release 20260930", "runs release 20260930"},
		{"", "runs no deployed version"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			facts := WhereFacts{ReadAt: time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC), Stages: []WhereStage{{Hostname: "apistage", Part: "api", Version: tc.version}}}
			want := "Where the work stands, read by zcp at 2026-11-25T12:00:00Z. A tool result you received after that is newer.\nstage apistage (part api) " + tc.want
			if got := RenderWhere(facts); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// WH-SIZE — The text never passes 1024 bytes: line 1 and the caller's copy stay, later whole lines give way to one line counting them.
// witness: internal/workflow/where.go | lines = lines[:len(lines)-1] → lines = lines[1:]
func TestWhere_WHSIZE_KeepsNamedCopyAndWholeLines(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	aliases := []string{"pool-01", "pool-02", "pool-03", "pool-04", "pool-05", "pool-06", "pool-07", "pool-08", "pool-09", "pool-10", "pool-11", "pool-12", "pool-13", "pool-14", "pool-15", "pool-16", "pool-17", "pool-18", "pool-19", "pool-20", "pool-21", "pool-22", "pool-23", "pool-24", "pool-25", "pool-26", "pool-27", "pool-28", "pool-29", "pool-30"}
	for _, branch := range []string{"feature-with-a-long-name-for-work", "feature-\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c\u754c"} {
		t.Run(branch, func(t *testing.T) {
			t.Parallel()
			facts := WhereFacts{ReadAt: at, Lease: "pool-30"}
			for _, alias := range aliases {
				facts.Copies = append(facts.Copies, WhereCopy{Name: alias, Part: "api", Branch: branch, Lease: true, State: "held", Unsaved: &topology.LeaseUnsaved{}, DevServers: []topology.DevServerState{}})
			}
			got := RenderWhere(facts)
			lines := strings.Split(got, "\n")
			if len(got) > WhereBudgetBytes || !utf8.ValidString(got) || !strings.HasPrefix(lines[0], "Where the work stands, read by zcp at ") || !strings.HasPrefix(lines[1], "pool-30 (this conversation's):") || !strings.HasPrefix(lines[2], "pool-01:") {
				t.Fatalf("bound or order: %d bytes\n%s", len(got), got)
			}
			wantLast := fmt.Sprintf("%d more lines: zerops_workflow action=\"status\" lists them.", 31-len(lines)+1)
			if lines[len(lines)-1] != wantLast {
				t.Errorf("last line %q, want %q", lines[len(lines)-1], wantLast)
			}
		})
	}
	{ // A long branch and reason must leave the named step and labels visible.
		facts := WhereFacts{ReadAt: at, Lease: "pool-1", Copies: []WhereCopy{{
			Name: "pool-1", Part: "api", Branch: strings.Repeat("\u754c", 255), Lease: true, State: "held",
			Unsaved: &topology.LeaseUnsaved{}, HasHQ: true,
			Change: &HQChangeLine{Number: 7, Labels: []string{"ready", "GitHub's checks failed", "Could not check the build (" + strings.Repeat("\u754c", 300) + ")"}},
			Step:   &EditStep{Action: "handed over", Head: "abcdef0", Number: 7}, DevServers: []topology.DevServerState{},
		}}}
		got := RenderWhere(facts)
		if len(got) > WhereBudgetBytes || !utf8.ValidString(got) || !strings.Contains(got, "pool-1 (this conversation's):") || !strings.Contains(got, "handed over at abcdef0") || !strings.Contains(got, "GitHub's checks failed") || !strings.Contains(got, "Could not check the build") {
			t.Errorf("oversized named copy lost its step or labels: %d bytes\n%s", len(got), got)
		}
	}
	t.Run("oversized server list", func(t *testing.T) {
		t.Parallel()
		servers := make([]topology.DevServerState, 40)
		for i := range servers {
			servers[i] = topology.DevServerState{Name: "api", Status: "running"}
		}
		facts := WhereFacts{ReadAt: at, Lease: "pool-1", Copies: []WhereCopy{{Name: "pool-1", Part: "api", Branch: "feature", Lease: true, State: "held", Unsaved: &topology.LeaseUnsaved{}, DevServers: servers}}}
		got := RenderWhere(facts)
		if len(got) > WhereBudgetBytes || !strings.Contains(got, "pool-1 (this conversation's):") || !strings.Contains(got, "dev server api running") || !strings.HasSuffix(got, "more (zcp status)") {
			t.Errorf("oversized server list: %d bytes\n%s", len(got), got)
		}
	})
}

type editPlanCase struct {
	name       string
	copies     []EditCopy
	change     *EditChange
	fresh      bool
	beforeEdit bool
	want       EditStep
}

func runEditCases(t *testing.T, cases []editPlanCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := PlanEdit(tc.copies, tc.change, tc.fresh, tc.beforeEdit); got != tc.want {
				t.Errorf("step = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// witness: internal/workflow/where.go | return EditStep{Action: "acquire"} → return EditStep{Action: EditUnknown}
func TestPlanEdit_WHAcquire_NoCopy(t *testing.T) {
	t.Parallel()
	runEditCases(t, []editPlanCase{{name: "none", fresh: true, want: EditStep{Action: "acquire"}}})
	text := RenderWhere(WhereFacts{EditSteps: true, Changes: []WhereChange{{HQChangeLine: HQChangeLine{Number: 7, Branch: "feature"}, Step: MissingEditCopyStep("feature", nil)}}})
	if !strings.Contains(text, "change #7 feature: no working copy; step: acquire") {
		t.Errorf("where should show the acquire step: %s", text)
	}
}

// witness: internal/workflow/where.go | return EditStep{Action: "checkout " + branch, Copy: classic.Name} → return EditStep{Action: "acquire"}
func TestWhere_WHCheckout_ClassicBranchNeedsCheckout(t *testing.T) {
	t.Parallel()
	zero := 0
	for _, tc := range []struct {
		name, want string
		classic    *WhereCopy
	}{
		{"classic", "change #7 feature: no working copy; step: checkout feature in appdev", &WhereCopy{Name: "appdev", Branch: "other", Step: &EditStep{Action: EditHandedOver}}},
		{"clean main", "change #7 feature: no working copy; step: checkout feature in appdev", &WhereCopy{Name: "appdev", Branch: "main", NothingToHandOver: true, BaseUnpushed: &zero}},
		{"pool", "change #7 feature: no working copy; step: acquire", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			facts := WhereFacts{EditSteps: true, Changes: []WhereChange{{HQChangeLine: HQChangeLine{Number: 7, Branch: "feature"}, Step: MissingEditCopyStep("feature", tc.classic)}}}
			if got := RenderWhere(facts); !strings.Contains(got, tc.want) {
				t.Errorf("where = %q; want %q", got, tc.want)
			}
		})
	}
}

// witness: internal/workflow/where.go | if *classic.BaseUnpushed > 0 { → if *classic.BaseUnpushed < 0 {
func TestWhere_WHOccupied_ClassicBranchCannotBeMoved(t *testing.T) {
	t.Parallel()
	zero, one := 0, 1
	for _, tc := range []struct {
		name    string
		classic WhereCopy
		want    EditStep
	}{
		{"dirty even if handed over", WhereCopy{Name: "appdev", Branch: "other", Dirty: true, Step: &EditStep{Action: EditHandedOver}}, EditStep{Action: "occupied by other: ask the person"}},
		{"dirty main", WhereCopy{Name: "appdev", Branch: "main", Dirty: true, NothingToHandOver: true, BaseUnpushed: &zero}, EditStep{Action: "occupied by main: ask the person"}},
		{"unpushed main", WhereCopy{Name: "appdev", Branch: "main", NothingToHandOver: true, BaseUnpushed: &one}, EditStep{Action: "occupied by main: ask the person"}},
		{"main unpushed unread", WhereCopy{Name: "appdev", Branch: "main", NothingToHandOver: true}, EditStep{Action: EditUnknown, Reason: "checkout appdev unpushed commits were not read"}},
		{"main branch unread", WhereCopy{Name: "appdev", Branch: "main", WorkUnread: "no answer"}, EditStep{Action: EditUnknown, Reason: "checkout appdev branch was not read"}},
		{"dirty detached", WhereCopy{Name: "appdev", Dirty: true}, EditStep{Action: "occupied by (detached): ask the person"}},
		{"clean detached", WhereCopy{Name: "appdev"}, EditStep{Action: EditUnknown, Reason: "checkout appdev is not on a branch"}},
		{"clean but not handed over", WhereCopy{Name: "appdev", Branch: "other", Step: &EditStep{Action: "describe change"}}, EditStep{Action: "occupied by other: ask the person"}},
		{"handed over but behind", WhereCopy{Name: "appdev", Branch: "other", Step: &EditStep{Action: EditRefreshBase, ReadyToSwitch: true}}, EditStep{Action: "checkout feature", Copy: "appdev"}},
		{"undescribed and behind", WhereCopy{Name: "appdev", Branch: "other", Step: &EditStep{Action: EditRefreshBase}}, EditStep{Action: "occupied by other: ask the person"}},
		{"unread handover", WhereCopy{Name: "appdev", Branch: "other"}, EditStep{Action: EditUnknown, Reason: "checkout appdev handover has not been checked"}},
		{"HQ unread", WhereCopy{Name: "appdev", Branch: "other", Step: &EditStep{Action: EditUnknown, Reason: "HQ was not read"}}, EditStep{Action: EditUnknown, Reason: "HQ was not read"}},
		{"checkout unread", WhereCopy{Name: "appdev", WorkUnread: "no answer"}, EditStep{Action: EditUnknown, Reason: "checkout appdev branch was not read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := MissingEditCopyStep("feature", &tc.classic); got != tc.want {
				t.Errorf("step = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// witness: internal/workflow/where.go | if workcopy := copies[0]; workcopy.Unread != "" { → if workcopy := copies[0]; workcopy.Unread == "" {
func TestPlanEdit_WHCopyRead_UnreadCopy(t *testing.T) {
	t.Parallel()
	runEditCases(t, []editPlanCase{
		{name: "checkout", copies: []EditCopy{{Name: "pool-1", Unread: "checkout was not read"}}, fresh: true, want: EditStep{Action: EditUnknown, Copy: "pool-1", Reason: "checkout was not read"}},
	})
}

// witness: internal/workflow/where.go | return EditStep{Action: EditUnknown, Reason: "HQ was not read"} → return EditStep{Action: "open change with a description"}
func TestPlanEdit_WHFresh_LastHeardIsUnknown(t *testing.T) {
	t.Parallel()
	runEditCases(t, []editPlanCase{{name: "last heard", copies: []EditCopy{{Name: "pool-1", Ref: &topology.BranchRef{Head: "head", Published: true}}}, want: EditStep{Action: EditUnknown, Reason: "HQ was not read"}}})
}

// witness: internal/workflow/where.go | return EditStep{Action: "commit", Copy: workcopy.Name} → return EditStep{Action: "push", Copy: workcopy.Name}
func TestPlanEdit_WHCommit_DirtyBeforePush(t *testing.T) {
	t.Parallel()
	runEditCases(t, []editPlanCase{{name: "dirty and behind", copies: []EditCopy{{Name: "pool-1", Dirty: true, Ref: &topology.BranchRef{Head: "head", Published: true, Behind: 1}}}, fresh: true, want: EditStep{Action: "commit", Copy: "pool-1"}}})
}

// witness: internal/workflow/where.go | return EditStep{Action: "pull", Copy: workcopy.Name} → return EditStep{Action: "push", Copy: workcopy.Name}
func TestPlanEdit_WHPull_BehindBranch(t *testing.T) {
	t.Parallel()
	runEditCases(t, []editPlanCase{{name: "behind", copies: []EditCopy{{Name: "pool-1", Ref: &topology.BranchRef{Head: "head", Published: true, Behind: 2}}}, fresh: true, want: EditStep{Action: "pull", Copy: "pool-1"}}})
}

// witness: internal/workflow/where.go | return EditStep{Action: "pull then push", Copy: workcopy.Name} → return EditStep{Action: "pull", Copy: workcopy.Name}
func TestPlanEdit_WHDiverged_PullBeforePush(t *testing.T) {
	t.Parallel()
	runEditCases(t, []editPlanCase{
		{name: "diverged", copies: []EditCopy{{Name: "pool-1", Ref: &topology.BranchRef{Head: "head", Published: true, Ahead: 1, Behind: 2}}}, fresh: true, want: EditStep{Action: "pull then push", Copy: "pool-1"}},
		{name: "remote object missing", copies: []EditCopy{{Name: "pool-2", Ref: &topology.BranchRef{Head: "head", Published: true, HistoryUnknown: true}}}, fresh: true, want: EditStep{Action: "pull then push if needed", Copy: "pool-2"}},
	})
}

// witness: internal/workflow/where.go | if !workcopy.Ref.Published || workcopy.Ref.Ahead > 0 { → if !workcopy.Ref.Published && workcopy.Ref.Ahead > 0 {
func TestPlanEdit_WHPush_BranchRefNotOtherBranches(t *testing.T) {
	t.Parallel()
	runEditCases(t, []editPlanCase{
		{name: "unpublished", copies: []EditCopy{{Name: "pool-1", Ref: &topology.BranchRef{Head: "head"}}}, fresh: true, want: EditStep{Action: "push", Copy: "pool-1"}},
		{name: "ahead of own branch", copies: []EditCopy{{Name: "pool-2", Ref: &topology.BranchRef{Head: "head", Published: true, Ahead: 1}}}, fresh: true, want: EditStep{Action: "push", Copy: "pool-2"}},
	})
}

// witness: internal/workflow/where.go | return EditStep{Action: "open change with a description", Head: copies[0].Ref.Head} → return EditStep{Action: "handed over", Head: copies[0].Ref.Head}
func TestPlanEdit_WHOpen_PublishedWithoutChange(t *testing.T) {
	t.Parallel()
	runEditCases(t, []editPlanCase{{name: "no open change", copies: []EditCopy{{Name: "pool-1", Ref: &topology.BranchRef{Head: "head", Published: true}}}, fresh: true, want: EditStep{Action: "open change with a description", Head: "head"}}})
}

// witness: internal/workflow/where.go | change.Head != copies[0].Ref.Head → change.Head == copies[0].Ref.Head
func TestPlanEdit_WHHead_ChangeAtAnotherHead(t *testing.T) {
	t.Parallel()
	ready := true
	runEditCases(t, []editPlanCase{{name: "other head", copies: []EditCopy{{Name: "pool-1", Ref: &topology.BranchRef{Head: "new", Published: true}}}, change: &EditChange{Number: 7, Head: "old", Ready: &ready}, fresh: true, want: EditStep{Action: EditUnknown, Reason: "HQ's change head or readiness was not read at this head"}}})
}

// witness: internal/workflow/where.go | if !*change.Ready { → if *change.Ready {
func TestPlanEdit_WHDescribe_DraftAtHead(t *testing.T) {
	t.Parallel()
	ready := false
	runEditCases(t, []editPlanCase{{name: "draft", copies: []EditCopy{{Name: "pool-1", Ref: &topology.BranchRef{Head: "head", Published: true}}}, change: &EditChange{Number: 7, Head: "head", Ready: &ready}, fresh: true, want: EditStep{Action: "describe change", Number: 7, Head: "head"}}})
}

// witness: internal/workflow/where.go | return EditStep{Action: EditHandedOver, Head: change.Head, Number: change.Number} → return EditStep{Action: "describe change", Head: change.Head, Number: change.Number}
func TestPlanEdit_WHHanded_ReadyAtHead(t *testing.T) {
	t.Parallel()
	ready := true
	copies := []EditCopy{{Name: "pool-1", Ref: &topology.BranchRef{Head: "head", Published: true}}}
	for _, tc := range []struct {
		name  string
		build *hq.ReviewBuild
	}{
		{"no result", nil},
		{"taken", &hq.ReviewBuild{Head: "head", State: "awaiting"}},
		{"passing", &hq.ReviewBuild{Head: "head", State: "passing"}},
		{"unavailable", &hq.ReviewBuild{Head: "head", State: "unavailable"}},
		{"failure at another head", &hq.ReviewBuild{Head: "old", State: "failing"}},
	} {
		runEditCases(t, []editPlanCase{{name: tc.name, copies: copies, change: &EditChange{Number: 7, Head: "head", Ready: &ready, ReviewBuild: tc.build}, fresh: true, want: EditStep{Action: "handed over", Number: 7, Head: "head"}}})
	}
}

// witness: internal/workflow/where.go | build.State == "failing" → build.State != "failing"
func TestPlanEdit_WHRepair_FailedReviewAtHead(t *testing.T) {
	t.Parallel()
	ready := true
	copies := []EditCopy{{Name: "appdev", Ref: &topology.BranchRef{Head: "head", Published: true}}}
	runEditCases(t, []editPlanCase{
		{name: "failed parts", copies: copies, change: &EditChange{Number: 7, Repo: "app", Head: "head", Ready: &ready, ReviewBuild: &hq.ReviewBuild{Head: "head", State: "failing", Parts: []hq.ReviewBuildPart{{Setup: "web", Outcome: "failed"}, {Setup: "db", Outcome: "ok"}, {Setup: "api", Outcome: "failed"}}}}, fresh: true, want: EditStep{Action: `read failure (zerops_check_log repo="app" change=7 setup="web"; omit setup to list and read other failed parts), fix and verify in the same draft, commit and push; the build retries automatically`, Copy: "appdev"}},
		{name: "no setup provided", copies: copies, change: &EditChange{Number: 7, Repo: "app", Head: "head", Ready: &ready, ReviewBuild: &hq.ReviewBuild{Head: "head", State: "failing"}}, fresh: true, want: EditStep{Action: `read failure (zerops_check_log repo="app" change=7; then read each failed part with setup), fix and verify in the same draft, commit and push; the build retries automatically`, Copy: "appdev"}},
	})
}

// witness: internal/workflow/where.go | if beforeEdit && change.Behind { → if beforeEdit && !change.Behind {
func TestPlanEdit_WHBase_RefreshOnlyBeforeTheNextEdit(t *testing.T) {
	t.Parallel()
	ready, notReady := true, false
	copies := []EditCopy{{Name: "appdev", Ref: &topology.BranchRef{Head: "head", Published: true}}}
	runEditCases(t, []editPlanCase{
		{name: "behind before edit", copies: copies, change: &EditChange{Number: 7, Head: "head", Ready: &ready, Behind: true}, fresh: true, beforeEdit: true, want: EditStep{Action: EditRefreshBase, Copy: "appdev", ReadyToSwitch: true}},
		{name: "behind undescribed before edit", copies: copies, change: &EditChange{Number: 7, Head: "head", Ready: &notReady, Behind: true}, fresh: true, beforeEdit: true, want: EditStep{Action: EditRefreshBase, Copy: "appdev"}},
		{name: "current before edit", copies: copies, change: &EditChange{Number: 7, Head: "head", Ready: &ready}, fresh: true, beforeEdit: true, want: EditStep{Action: EditHandedOver, Head: "head", Number: 7}},
		{name: "behind at handover", copies: copies, change: &EditChange{Number: 7, Head: "head", Ready: &ready, Behind: true}, fresh: true, want: EditStep{Action: EditHandedOver, Head: "head", Number: 7}},
	})
}

// witness: internal/workflow/where.go | if change.ChecksGivenUp { → if !change.ChecksGivenUp {
func TestPlanEdit_WHGivenUp_AsksThePersonAfterFailedTries(t *testing.T) {
	t.Parallel()
	ready := true
	copies := []EditCopy{{Name: "appdev", Ref: &topology.BranchRef{Head: "head", Published: true}}}
	change := &EditChange{Number: 7, Repo: "app", Head: "head", Ready: &ready, ChecksGivenUp: true, ReviewBuild: &hq.ReviewBuild{Head: "head", State: "failing"}}
	runEditCases(t, []editPlanCase{{name: "exhausted", copies: copies, change: change, fresh: true, beforeEdit: true, want: EditStep{Action: "ask the person about the failed build; the draft stays open", Copy: "appdev"}}})
}

// witness: internal/workflow/where.go | head = head[:7] → head = head[:8]
func TestEditStep_WHStepWords_NamesStepAndShortCommit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		step   EditStep
		want   []string
		absent string
	}{
		{"copy", EditStep{Action: "push", Copy: "pool-2"}, []string{"push", "pool-2"}, ""},
		{"unread", EditStep{Action: EditUnknown, Reason: "HQ was not read"}, []string{"unknown", "HQ was not read"}, ""},
		{"short head", EditStep{Action: EditHandedOver, Head: "abcd", Number: 7}, []string{"handed over", "abcd", "#7"}, ""},
		{"full head", EditStep{Action: EditHandedOver, Head: "abcdef0123456789", Number: 7}, []string{"handed over", "abcdef0", "#7"}, "abcdef01"},
		{"acquire", EditStep{Action: "acquire"}, []string{"acquire"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.step.Words()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("step = %q, want %q", got, want)
				}
			}
			if tc.absent != "" && strings.Contains(got, tc.absent) {
				t.Fatalf("step = %q, must not include %q", got, tc.absent)
			}
		})
	}
}

// witness: internal/workflow/where.go | base += "step: " + worktree.Step.Words() + "; " → base += ""
func TestWhere_WHEditLine_ShowsStepAndReview(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	facts := WhereFacts{ReadAt: at, Lease: "pool-1", Copies: []WhereCopy{{Name: "pool-1", Part: "api", Branch: "feature", Lease: true, State: "held", Unsaved: &topology.LeaseUnsaved{}, HasHQ: true, Change: &HQChangeLine{Number: 7, Labels: []string{"Build passed"}}, Step: &EditStep{Action: "handed over", Head: "abcdef0", Number: 7}, DevServers: []topology.DevServerState{}}}}
	got := RenderWhere(facts)
	if !strings.Contains(got, "handed over at abcdef0") || !strings.Contains(got, "Build passed") {
		t.Errorf("where omitted the step or review verdict: %s", got)
	}
}

// witness: internal/workflow/where.go | return EditStep{Action: "read", Copy: workcopy.Name} → return EditStep{Action: "handed over", Copy: workcopy.Name}
func TestPlanEdit_WHGroup_AllCopiesBeforeHandover(t *testing.T) {
	t.Parallel()
	ready := true
	runEditCases(t, []editPlanCase{
		{name: "other unread", copies: []EditCopy{{Name: "pool-1", Ref: &topology.BranchRef{Head: "head", Published: true}}, {Name: "pool-2", Unread: "checkout unread"}}, change: &EditChange{Number: 7, Head: "head", Ready: &ready}, fresh: true, want: EditStep{Action: "read", Copy: "pool-2"}},
		{name: "own work remains actionable", copies: []EditCopy{{Name: "pool-1", Dirty: true, Ref: &topology.BranchRef{Head: "head", Published: true}}, {Name: "pool-2", Unread: "checkout unread"}}, change: &EditChange{Number: 7, Head: "head", Ready: &ready}, fresh: true, want: EditStep{Action: "commit", Copy: "pool-1"}},
		{name: "other dirty", copies: []EditCopy{{Name: "pool-1", Ref: &topology.BranchRef{Head: "head", Published: true}}, {Name: "pool-2", Dirty: true, Ref: &topology.BranchRef{Head: "head", Published: true}}}, change: &EditChange{Number: 7, Head: "head", Ready: &ready}, fresh: true, want: EditStep{Action: "commit", Copy: "pool-2"}},
	})
}
