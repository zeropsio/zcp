package tools

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// Develop guidance: hq_change_SENTENCES.md. Each case meets the dev service
// appdev's checkout as the copy planner reads it — dirty on another branch,
// clean on a handed-over one, not read — and a dev pool, webpool, which has
// none.

func occupiedCopy() *workflow.WhereCopy {
	return &workflow.WhereCopy{Name: "appdev", Branch: "other", Dirty: true, HasHQ: true}
}

func handedOverCopy() *workflow.WhereCopy {
	step := workflow.EditStep{Action: workflow.EditHandedOver, Head: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Number: 6}
	return &workflow.WhereCopy{Name: "appdev", Branch: "other", HasHQ: true, Step: &step}
}

func unreadCopy() *workflow.WhereCopy {
	return &workflow.WhereCopy{Name: "appdev", Branch: "not read", WorkUnread: "checkout did not answer", HasHQ: true}
}

// plannedRoute is appdev's route with its checkout read as plan, or the dev
// pool's for none.
func plannedRoute(plan *workflow.WhereCopy) repoRoute {
	if plan == nil {
		return repoRoute{pool: "webpool"}
	}
	read := *plan
	return repoRoute{host: "appdev"}.readingCopy(func(string, string) workflow.WhereCopy { return read }, "app")
}

// witness: internal/tools/hq_change.go | route.take("<branch>"), hqBase → route.git("fetch origin <branch>", "checkout <branch>"), hqBase
func TestHQChange_DGOpen_TakesABranchOnlyWhereTheCheckoutMayMove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		copy *workflow.WhereCopy
		take string
	}{
		{"dirty on another branch", occupiedCopy(), "occupied by other: ask the person"},
		{"clean and handed over", handedOverCopy(), "`zcp git appdev fetch origin <branch> && zcp git appdev checkout <branch>`"},
		{"not read", unreadCopy(), "unknown: checkout appdev branch was not read"},
		{"dev pool", nil, "`zerops_lease action=\"acquire\" pool=\"webpool\" branch=\"<branch>\"`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text := openChangesWords([]workflow.HQChange{{Number: 7, Branch: "feature"}}, plannedRoute(tc.copy), time.Time{})
			if want := "work on a change stays on its own branch (" + tc.take + ")"; !strings.Contains(text, want) {
				t.Errorf("open changes = %q; want %q", text, want)
			}
		})
	}
}

// witness: internal/tools/hq_repo_host.go | return r.moved(branch, "checkout "+hqBase, → return r.git("checkout "+hqBase,
func TestHQChange_DGNewWork_CutsFromMainOnlyWhereTheCheckoutMayMove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		copy *workflow.WhereCopy
		cut  string
	}{
		{"dirty on another branch", occupiedCopy(), "occupied by other: ask the person"},
		{"clean and handed over", handedOverCopy(), "`zcp git appdev checkout main && zcp git appdev fetch origin && zcp git appdev merge --ff-only origin/main && zcp git appdev checkout -b <branch>`"},
		{"not read", unreadCopy(), "unknown: checkout appdev branch was not read"},
		{"dev pool", nil, "`zerops_lease action=\"acquire\" pool=\"webpool\" branch=\"<branch>\"`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			text := openChangesWords([]workflow.HQChange{{Number: 7, Branch: "feature"}}, plannedRoute(tc.copy), time.Time{})
			if want := "cut from an up-to-date \"main\" (" + tc.cut + "), opening its own change"; !strings.Contains(text, want) {
				t.Errorf("open changes = %q; want %q", text, want)
			}
		})
	}
}

// witness: internal/tools/hq_change.go | route.moved(hqBase, "checkout "+hqBase → route.git("checkout "+hqBase
func TestHQChange_DGMerged_BringsMainUpToDateOnlyWhereTheCheckoutMayMove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		copy *workflow.WhereCopy
		main string
	}{
		{"dirty on another branch", occupiedCopy(), "bring it up to date in its dev service appdev (occupied by other: ask the person), and cut the next work's branch from it"},
		{"clean and handed over", handedOverCopy(), "bring it up to date in its dev service appdev (`zcp git appdev checkout main && zcp git appdev fetch origin && zcp git appdev merge --ff-only origin/main`), and cut the next work's branch from it"},
		{"not read", unreadCopy(), "bring it up to date in its dev service appdev (unknown: checkout appdev branch was not read), and cut the next work's branch from it"},
		{"dev pool", nil, "cut the next work's branch from it in a lease of the dev pool webpool (`zerops_lease action=\"acquire\" pool=\"webpool\" branch=\"<branch>\"` cuts a new branch from \"main\")"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stateDir, app := t.TempDir(), "app-1"
			c := workflow.HQChange{AppID: app, Repo: "app", Number: 7, Branch: "feature"}
			if err := workflow.RecordHQChange(stateDir, c); err != nil {
				t.Fatal(err)
			}
			state := hq.MateState{AppID: &app, Changes: []hq.MateChange{{Repo: "app", Number: 7, Branch: "feature", State: hq.ChangeMerged}}}
			if text := learnChangeLanding(stateDir, "https://hq.example", c, state, plannedRoute(tc.copy)); !strings.Contains(text, tc.main) {
				t.Errorf("merge note = %q; want %q", text, tc.main)
			}
		})
	}
}

// witness: internal/tools/hq_repo_host.go | return r.moved(branch, "fetch origin "+hqBase, → return r.git("fetch origin "+hqBase,
func TestHQChange_DGHanded_TakesOrCutsAHandedChangeOnlyWhereTheCheckoutMayMove(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		copy      *workflow.WhereCopy
		take, cut string
	}{
		{"dirty on another branch", occupiedCopy(), "occupied by other: ask the person", "occupied by other: ask the person"},
		{"clean and handed over", handedOverCopy(), "`zcp git appdev fetch origin fix-login && zcp git appdev checkout fix-login`", "`zcp git appdev fetch origin main && zcp git appdev checkout -b add-signup origin/main`"},
		{"not read", unreadCopy(), "unknown: checkout appdev branch was not read", "unknown: checkout appdev branch was not read"},
		{"dev pool", nil, "`zerops_lease action=\"acquire\" pool=\"webpool\" branch=\"fix-login\"`", "`zerops_lease action=\"acquire\" pool=\"webpool\" branch=\"add-signup\"`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app, head := "app-1", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			state := hq.MateState{AppID: &app, OpenPerBranch: true, Changes: []hq.MateChange{
				{Repo: "app", Number: 2, State: hq.ChangeOpen, Handed: true, Branch: "fix-login", Head: &head},
				{Repo: "app", Number: 3, State: hq.ChangeOpen, Handed: true, Branch: "add-signup"},
			}}
			text := strings.Join(hqTakeHanded(t.TempDir(), state, app, "app", nil, plannedRoute(tc.copy)), "\n")
			for _, want := range []string{"take that branch (" + tc.take + ")", "cut it from \"main\" (" + tc.cut + ")"} {
				if !strings.Contains(text, want) {
					t.Errorf("handed changes = %q; want %q", text, want)
				}
			}
		})
	}
}

// witness: internal/tools/hq_change.go | if _, free := route.mayMove(branch); !free { → if _, free := route.mayMove(branch); free {
func TestHQChange_DGSingle_TakesTheOpenChangeOnlyWhereTheCheckoutMayMove(t *testing.T) {
	t.Parallel()
	hqc, err := hq.Open(http.DefaultClient, hq.Identity{Origin: "https://hq.example", Key: "key", ProjectID: "proj-1", OrgID: "org-1"})
	if err != nil {
		t.Fatal(err)
	}
	branch := "mate/proj-1/1"
	rename := "rename that one instead (`zcp git appdev branch -m mate/proj-1 mate/proj-1/1 && zcp git appdev checkout mate/proj-1/1`)"
	for _, tc := range []struct {
		name    string
		copy    *workflow.WhereCopy
		take    string
		renames bool
	}{
		{"dirty on another branch", occupiedCopy(), "occupied by other: ask the person", false},
		{"clean and handed over", handedOverCopy(), "`zcp git appdev fetch origin mate/proj-1/1 && zcp git appdev checkout mate/proj-1/1`", true},
		{"not read", unreadCopy(), "unknown: checkout appdev branch was not read", false},
		{"dev pool", nil, "`zerops_lease action=\"acquire\" pool=\"webpool\" branch=\"mate/proj-1/1\"`", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			route := plannedRoute(tc.copy)
			for _, text := range []string{
				hqOpenChangeWork(route, hqc, workflow.HQChange{Number: 1, Repo: "app", Branch: branch}),
				answeredOpenChange(route, hqc, "app", "add-signup", branch, 1),
			} {
				if !strings.Contains(text, "take it ("+tc.take+")") || strings.Contains(text, rename) != tc.renames {
					t.Errorf("words = %q; want take it (%s), the rename given: %v", text, tc.take, tc.renames)
				}
			}
		})
	}
}

// checkoutSSH answers a dev service's checkout reads: the head read with
// head, branch and work, the base read with unpushed — or err to each.
type checkoutSSH struct {
	head, branch, work, unpushed string
	err                          error
}

func (s checkoutSSH) ExecSSH(_ context.Context, _, command string) ([]byte, error) {
	switch {
	case s.err != nil:
		return nil, s.err
	case strings.Contains(command, "symbolic-ref"):
		return []byte("checkout-head:" + s.head + "\ncheckout-branch:" + s.branch + "\n" + s.work + "\n"), nil
	}
	return []byte(s.unpushed + "\n"), nil
}

func (s checkoutSSH) ExecSSHStdin(ctx context.Context, host, command string, _ []byte) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func (s checkoutSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

// witness: internal/tools/where.go | g.branches, g.refs, g.hqReads, time.Now()) → g.branches, nil, g.hqReads, time.Now())
func TestHQChange_DGRead_ReadsTheCheckoutAsWhereDoes(t *testing.T) {
	t.Parallel()
	head := "cccccccccccccccccccccccccccccccccccccccc"
	for _, tc := range []struct {
		name string
		ssh  checkoutSSH
		take string
	}{
		{"clean main with nothing unpushed", checkoutSSH{head: head, branch: "main", work: "checkout-clean", unpushed: "0"}, "`zcp git appdev fetch origin feature && zcp git appdev checkout feature`"},
		{"main with an unpushed commit", checkoutSSH{head: head, branch: "main", work: "checkout-clean", unpushed: "1"}, "occupied by main: ask the person"},
		{"uncommitted work on main", checkoutSSH{head: head, branch: "main", work: "checkout-dirty", unpushed: "0"}, "occupied by main: ask the person"},
		{"a checkout that does not answer", checkoutSSH{err: errors.New("connection refused")}, "unknown: checkout appdev branch was not read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			read := classicCopyRead(context.Background(), tc.ssh, runtime.Info{MateEnabled: true}, t.TempDir())
			if got := (repoRoute{host: "appdev"}).readingCopy(read, "app").take("feature"); got != tc.take {
				t.Errorf("take = %q; want %q", got, tc.take)
			}
		})
	}
}
