// Tests for: ops/pool_migrate_lease.go — the leases zcp holds of a part's pool
// on the work's branch, read from its own records.
package ops

import (
	"slices"
	"testing"
)

func TestPoolMigrateLeases_PoolRepoAndBranchFromRecords(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	const origin = "https://github.com/example/app.git"
	records := []leaseRecord{
		{PoolHostname: "apppool", PoolServiceID: "svc-pool", ContainerID: "c-2", ContainerNumber: 2, Lease: "l2", Branch: "feat-x", Repo: "https://github.com/example/app", AcquiredAt: "2026-10-23T12:00:00Z"},
		{PoolHostname: "apppool", PoolServiceID: "svc-pool", ContainerID: "c-1", ContainerNumber: 1, Lease: "l1", Branch: "feat-x", Repo: "https://github.com/example/app.git", AcquiredAt: "2026-10-23T11:00:00Z"},
		// Another branch, another repository, another pool: none is the
		// work's lease.
		{PoolHostname: "apppool", PoolServiceID: "svc-pool", ContainerID: "c-3", ContainerNumber: 3, Lease: "l3", Branch: "other", Repo: origin, AcquiredAt: "2026-10-23T12:00:00Z"},
		{PoolHostname: "apppool", PoolServiceID: "svc-pool", ContainerID: "c-4", ContainerNumber: 4, Lease: "l4", Branch: "feat-x", Repo: "https://github.com/example/other.git", AcquiredAt: "2026-10-23T12:00:00Z"},
		{PoolHostname: "webpool", PoolServiceID: "svc-web", ContainerID: "c-5", ContainerNumber: 5, Lease: "l5", Branch: "feat-x", Repo: origin, AcquiredAt: "2026-10-23T12:00:00Z"},
		// An empty branch is main, as an acquire without one is.
		{PoolHostname: "apppool", PoolServiceID: "svc-pool", ContainerID: "c-6", ContainerNumber: 6, Lease: "l6", Repo: origin, AcquiredAt: "2026-10-23T12:00:00Z"},
	}
	for _, r := range records {
		if err := writeLeaseRecord(stateDir, r); err != nil {
			t.Fatal(err)
		}
	}
	aliases := func(t *testing.T, branch string) []string {
		t.Helper()
		got, err := PoolMigrateLeases(stateDir, "apppool", origin, branch)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(got))
		for _, l := range got {
			out = append(out, l.Alias)
		}
		return out
	}
	if got, want := aliases(t, "feat-x"), []string{"apppool-1", "apppool-2"}; !slices.Equal(got, want) {
		t.Errorf("leases on feat-x = %q, want %q (same repository however the URL is spelled, by container number)", got, want)
	}
	if got, want := aliases(t, "main"), []string{"apppool-6"}; !slices.Equal(got, want) {
		t.Errorf("leases on main = %q, want %q", got, want)
	}
	if got, err := PoolMigrateLeases(t.TempDir(), "apppool", origin, "feat-x"); err != nil || len(got) != 0 {
		t.Errorf("no records = %v, %v; want none and no error", got, err)
	}
}
