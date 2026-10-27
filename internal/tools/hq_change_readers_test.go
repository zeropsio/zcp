// Tests for: the readers of a Mate's changes that moved from the pair's
// record to the change ledger (workflow.HQChange) — pool-migrate's work
// branch.
package tools

import (
	"slices"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

// TestPoolMigrateWorkBranch_TheLedgersOneChangeOfTheRepository: in a Mate the
// work's branch is the one open change the ledger keeps in the dev's
// repository; with several, none names the work, and the dev checkout's own
// branch does.
func TestPoolMigrateWorkBranch_TheLedgersOneChangeOfTheRepository(t *testing.T) {
	t.Parallel()
	meta := &workflow.ServiceMeta{Hostname: "appdev", HQ: &workflow.HQRepoRef{AppID: labApp, Repo: "appdev"}}
	census := &ops.PoolMigrateCensus{Trees: []ops.PoolMigrateTree{{Branch: "feat"}}}
	rt := runtime.Info{MateEnabled: true}
	one := []workflow.HQChange{
		{AppID: labApp, Repo: "appdev", Number: 7, Branch: "fix-login"},
		{AppID: labApp, Repo: "apidev", Number: 2, Branch: "api-work"},
	}
	if got := poolMigrateWorkBranchOf(meta, census, rt, one); got.Branch != "fix-login" || got.Source != poolMigrateWorkFromChange {
		t.Errorf("work branch = %+v, want fix-login from the change", got)
	}
	several := append(slices.Clone(one), workflow.HQChange{AppID: labApp, Repo: "appdev", Number: 8, Branch: "add-signup"})
	if got := poolMigrateWorkBranchOf(meta, census, rt, several); got.Branch != "feat" || got.Source != poolMigrateWorkFromCurrent {
		t.Errorf("work branch with several changes = %+v, want the checkout's own feat", got)
	}
}

// TestLearnChangeLanding_ReadsAMissingChangeByTheStatesWindow: a change the
// ledger keeps that the Mate's state no longer lists is read by that state's
// window. An older HQ lists only the Mate's newest ten in a repository: a
// change older than ten newer ones listed may be open, so it stays kept and
// nothing is said; one missing from a window that is not full reads closed.
// An HQ that keeps an open change per branch lists every open one: a missing
// change settled, and is said no longer open — never closed without merging.
func TestLearnChangeLanding_ReadsAMissingChangeByTheStatesWindow(t *testing.T) {
	t.Parallel()
	app := labApp
	newer := func(n int) []hq.MateChange {
		listed := make([]hq.MateChange, 0, n)
		for i := range n {
			listed = append(listed, hq.MateChange{Repo: "appsdev", Number: 20 - i, State: hq.ChangeMerged})
		}
		return listed
	}
	tests := []struct {
		name      string
		state     hq.MateState
		wantWords string // "" nothing said
		wantKept  bool
	}{
		{name: "an older HQ's full window", state: hq.MateState{AppID: &app, Changes: newer(hq.MateChangesPerRepo)}, wantKept: true},
		{name: "an older HQ's window not full", state: hq.MateState{AppID: &app, Changes: newer(3)}, wantWords: "closed without merging"},
		{name: "an HQ that lists every open change", state: hq.MateState{AppID: &app, Changes: newer(hq.MateChangesPerRepo), OpenPerBranch: true},
			wantWords: "no longer open"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			c := workflow.HQChange{AppID: labApp, Repo: "appsdev", Number: 2, Branch: "pool-previews"}
			if err := workflow.RecordHQChange(stateDir, c); err != nil {
				t.Fatal(err)
			}

			words := learnChangeLanding(stateDir, targetHQ, c, tt.state)

			if tt.wantWords == "" && words != "" || !strings.Contains(words, tt.wantWords) {
				t.Errorf("said %q, want %q", words, tt.wantWords)
			}
			if strings.Contains(words, "closed without merging") && tt.state.OpenPerBranch {
				t.Errorf("a change settled out of the window is said closed: %q", words)
			}
			if _, found, err := workflow.ReadHQChange(stateDir, labApp, "appsdev", 2); err != nil || found != tt.wantKept {
				t.Errorf("kept = %v (%v), want %v", found, err, tt.wantKept)
			}
		})
	}
}
