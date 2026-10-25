package ops

import (
	"fmt"
	"strings"
	"testing"
)

// TestLeaseBranches_EveryRecordWithItsRepositoryAndBranch: what a change's
// targeting and status read of zcp's leases — every record, by alias, with
// its repository as LeaseGroup groups it (an older record's is its pool's
// default) and its branch (none is main) — from the records alone, never the
// lease itself.
func TestLeaseBranches_EveryRecordWithItsRepositoryAndBranch(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	write := func(pool string, n int64, repo, branch string) {
		t.Helper()
		rec := leaseRecord{
			PoolHostname: pool, PoolServiceID: "svc-" + pool, ContainerID: fmt.Sprintf("c-%s-%d", pool, n), ContainerNumber: n,
			ContainerHostname: fmt.Sprintf("node-id-%d.runtime.%s.zerops", n, pool), Lease: testLease, Branch: branch, Repo: repo,
			AcquiredAt: "2026-10-25T10:00:00Z",
		}
		if err := writeLeaseRecord(stateDir, rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := storePoolDefaultRepo(stateDir, "boxpool", "https://hq.example.com/git/app-1/appsdev.git"); err != nil {
		t.Fatal(err)
	}
	write("hqpool", 4, "https://hq.example.com/git/app-1/appsdev.git", "badge")
	write("boxpool", 6, "", "badge") // predates keeping the repository
	write("webpool", 3, "https://hq.example.com/git/app-1/appsdev", "")

	got, err := LeaseBranches(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 0, len(got))
	for _, l := range got {
		lines = append(lines, l.Alias+" "+l.Repo+" "+l.Branch)
	}
	want := []string{
		"boxpool-6 https://hq.example.com/git/app-1/appsdev badge",
		"hqpool-4 https://hq.example.com/git/app-1/appsdev badge",
		"webpool-3 https://hq.example.com/git/app-1/appsdev main",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("leases =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if dump := fmt.Sprintf("%+v", got); strings.Contains(dump, testLease) {
		t.Errorf("the listing carries a lease: %s", dump)
	}

	none, err := LeaseBranches(t.TempDir())
	if err != nil || len(none) != 0 {
		t.Errorf("no records = %v (%v), want none", none, err)
	}
}
