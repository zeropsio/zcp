package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestWorkspaceRepos_TheDevHalfOfEveryContainerPair: the repositories a zcp
// container checks out under its workspace are each pair's dev half, mounted
// at /var/www/<host> — every mode a container mounts (standard, dev, simple,
// a meta older than modes), complete or still bootstrapping. Never a stage (it
// is built from the dev half's repository), never a variant-B pair's further
// dev services (they mount the same volume), never a pool-keyed pair (its
// hostname names a pool zcp never mounts; its checkouts are leases, each bound
// to the conversation that took it — docs/spec-workflows.md §5.1), never a
// local topology. A variant-B pair's code lives on its volume, which shows
// under the dev half's mount at its subpath.
func TestWorkspaceRepos_TheDevHalfOfEveryContainerPair(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	for _, m := range []*ServiceMeta{
		{Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}, {Hostname: "workerstage"}}, BootstrappedAt: "2026-10-01"},
		{Hostname: "botdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-01"},
		{Hostname: "site", Mode: topology.PlanModeSimple, BootstrappedAt: "2026-10-01"},
		{Hostname: "legacy", BootstrappedAt: "2026-10-01"},
		{Hostname: "newdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "newstage"}}},
		{Hostname: "webdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "webstage"}}, BootstrappedAt: "2026-10-01",
			CodeVolume: &topology.CodeVolume{Hostname: "code", MountPath: "/var/www/ws", DevServices: []topology.DevServiceRef{{Hostname: "admindev"}}}},
		{Hostname: "apipool", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "apistage"}}, BootstrappedAt: "2026-10-01",
			DevPool: &DevPoolRef{ServiceID: "pool-1", MigratedFrom: &MigratedDev{Hostname: "apidev"}}},
		{Hostname: "myproject", Mode: topology.PlanModeLocalOnly, BootstrappedAt: "2026-10-01"},
		{Hostname: "localdev", Mode: topology.PlanModeLocalStage, Stages: []topology.StageRef{{Hostname: "localstage"}}, BootstrappedAt: "2026-10-01"},
	} {
		if err := WriteServiceMeta(stateDir, m); err != nil {
			t.Fatalf("write meta %s: %v", m.Hostname, err)
		}
	}

	got, err := WorkspaceRepos(stateDir)
	if err != nil {
		t.Fatalf("WorkspaceRepos: %v", err)
	}
	want := []string{"appdev", "botdev", "legacy", "newdev", "site", "webdev/ws"}
	if !slices.Equal(got, want) {
		t.Errorf("WorkspaceRepos = %v, want %v", got, want)
	}
}

// TestWorkspaceRepos_FollowsThePairs: a pair written is listed, a pair
// deleted is gone, a pair moved onto its dev pool leaves the list.
func TestWorkspaceRepos_FollowsThePairs(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	list := func(step string, want ...string) {
		t.Helper()
		got, err := WorkspaceRepos(stateDir)
		if err != nil {
			t.Fatalf("%s: WorkspaceRepos: %v", step, err)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: WorkspaceRepos = %v, want %v", step, got, want)
		}
	}
	list("no state")

	for _, m := range []*ServiceMeta{
		{Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}}, BootstrappedAt: "2026-10-01"},
		{Hostname: "botdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-01"},
	} {
		if err := WriteServiceMeta(stateDir, m); err != nil {
			t.Fatal(err)
		}
	}
	list("two pairs", "appdev", "botdev")

	if err := DeleteServiceMeta(stateDir, "botdev"); err != nil {
		t.Fatal(err)
	}
	list("a pair deleted", "appdev")

	if _, err := MigratePairToPool(stateDir, PoolPairMove{Dev: "appdev", Pool: "apppool", PoolServiceID: "pool-1"}); err != nil {
		t.Fatalf("MigratePairToPool: %v", err)
	}
	list("the pair moved onto its pool")
}

// TestWorkspaceRepos_UnreadableRecord_Errors: a pair record zcp cannot read
// is an error, never a shorter list — a caller must not drop a repository's
// import because one file is broken.
func TestWorkspaceRepos_UnreadableRecord_Errors(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := WriteServiceMeta(stateDir, &ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeDev}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "services", "broken.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := WorkspaceRepos(stateDir); err == nil {
		t.Errorf("WorkspaceRepos = %v, nil; want an error naming the unreadable record", got)
	}
}
