// Tests for: the readers of a Mate's changes that moved from the pair's
// record to the change ledger (workflow.HQChange) — pool-migrate's work
// branch.
package tools

import (
	"slices"
	"testing"

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
