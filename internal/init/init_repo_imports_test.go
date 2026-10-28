// Tests for: init.go generateAgentContext — in a container the workspace
// CLAUDE.md imports the CLAUDE.md of each repository zcp mounts under it, so
// a session opened in /var/www reads every repository's own instructions at
// start (docs/spec-workflows.md §5.1).
package init_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	zcpinit "github.com/zeropsio/zcp/internal/init"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

const (
	beginMarker = "<!-- ZCP:BEGIN -->\n"
	endMarker   = "<!-- ZCP:END -->\n"
)

// writePairs writes one pair record per meta under baseDir's state directory.
func writePairs(t *testing.T, baseDir string, metas ...*workflow.ServiceMeta) string {
	t.Helper()
	stateDir := filepath.Join(baseDir, ".zcp", "state")
	for _, m := range metas {
		if err := workflow.WriteServiceMeta(stateDir, m); err != nil {
			t.Fatalf("write pair %s: %v", m.Hostname, err)
		}
	}
	return stateDir
}

func readCLAUDEMD(t *testing.T, baseDir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(baseDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	return string(raw)
}

// TestRun_Container_CLAUDEMDImportsEachPairsRepository: zcp init writes one
// @<host>/CLAUDE.md line per pair's dev half after zcp's own @AGENTS.md — a
// stage never, a pool-keyed pair and a held lease never (a lease is the
// checkout of the conversation that took it) — and a re-run follows the pairs
// added and removed since. What the user wrote around the managed block stays
// byte for byte.
func TestRun_Container_CLAUDEMDImportsEachPairsRepository(t *testing.T) {
	// Not parallel — mutates HOME, the command runner and the lease state dir.
	t.Setenv("HOME", t.TempDir())
	stubContainerCommands(t)
	dir := t.TempDir()
	stateDir := writePairs(t, dir,
		&workflow.ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "appstage"}}, BootstrappedAt: "2026-10-01"},
		&workflow.ServiceMeta{Hostname: "botdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-01"},
		&workflow.ServiceMeta{Hostname: "apipool", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "apistage"}}, BootstrappedAt: "2026-10-01",
			DevPool: &workflow.DevPoolRef{ServiceID: "pool-1"}},
	)
	zcpinit.SetSSHFSMountBase(t.TempDir())
	zcpinit.SetLeaseStateDir(stateDir)
	zcpinit.SetLeaseAliasFile(filepath.Join(t.TempDir(), "zcp-leases.conf"))
	t.Cleanup(func() { zcpinit.ResetSSHFSMountBase(); zcpinit.ResetLeaseStateDir(); zcpinit.ResetLeaseAliasFile() })
	writeLeaseRecordFile(t, stateDir, "apipool", 3, "node-id-3.runtime.apipool.zerops")

	above := "# Franta\n\nUse the team's branch naming.\n\n"
	below := "\n## Notes\n\n- see @docs/runbook.md for the on-call steps\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(above+beginMarker+"@AGENTS.md\n"+endMarker+below), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := runtime.Info{InContainer: true, ServiceName: "zcp"}

	if err := zcpinit.Run(dir, rt, zcpinit.WithLeaseCheck(leaseCheckOurs)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := above + beginMarker + "@AGENTS.md\n@appdev/CLAUDE.md\n@botdev/CLAUDE.md\n" + endMarker + below
	if got := readCLAUDEMD(t, dir); got != want {
		t.Errorf("CLAUDE.md after init =\n%s\nwant\n%s", got, want)
	}

	if err := workflow.DeleteServiceMeta(stateDir, "botdev"); err != nil {
		t.Fatal(err)
	}
	writePairs(t, dir, &workflow.ServiceMeta{Hostname: "kanbandev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-02"})

	if err := zcpinit.Run(dir, rt, zcpinit.WithLeaseCheck(leaseCheckOurs)); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	want = above + beginMarker + "@AGENTS.md\n@appdev/CLAUDE.md\n@kanbandev/CLAUDE.md\n" + endMarker + below
	if got := readCLAUDEMD(t, dir); got != want {
		t.Errorf("CLAUDE.md after the pairs changed =\n%s\nwant\n%s", got, want)
	}

	agents, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(agents), "/CLAUDE.md\n") {
		t.Errorf("AGENTS.md carries a repository import; other agents do not read @-imports:\n%s", agents)
	}
}

// TestRun_Local_CLAUDEMDImportsNoRepository: locally the working directory is
// the repository itself and zcp mounts nothing under it, so CLAUDE.md keeps
// zcp's @AGENTS.md alone whatever pair records exist.
func TestRun_Local_CLAUDEMDImportsNoRepository(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writePairs(t, dir,
		&workflow.ServiceMeta{Hostname: "myproject", Mode: topology.PlanModeLocalStage, Stages: []topology.StageRef{{Hostname: "appstage"}}, BootstrappedAt: "2026-10-01"},
		&workflow.ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-01"},
	)

	if err := zcpinit.Run(dir, runtime.Info{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := readCLAUDEMD(t, dir), beginMarker+"@AGENTS.md\n"+endMarker; got != want {
		t.Errorf("CLAUDE.md =\n%s\nwant\n%s", got, want)
	}
}

// TestRun_Container_UnreadablePairRecords_StillWritesTheAgentContext: the
// agent context is the step zcp init must deliver (a run.init command gates
// the container start on it), so a pair record it cannot read costs only the
// imports — CLAUDE.md keeps @AGENTS.md, the repositories' CLAUDE.md load on
// demand as before — and never the boot.
func TestRun_Container_UnreadablePairRecords_StillWritesTheAgentContext(t *testing.T) {
	// Not parallel — mutates HOME and the command runner.
	t.Setenv("HOME", t.TempDir())
	stubContainerCommands(t)
	dir := t.TempDir()
	stateDir := writePairs(t, dir, &workflow.ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeDev, BootstrappedAt: "2026-10-01"})
	if err := os.WriteFile(filepath.Join(stateDir, "services", "broken.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := zcpinit.Run(dir, runtime.Info{InContainer: true, ServiceName: "zcp"}); err != nil {
		t.Fatalf("Run failed on an unreadable pair record: %v", err)
	}
	if got, want := readCLAUDEMD(t, dir), beginMarker+"@AGENTS.md\n"+endMarker; got != want {
		t.Errorf("CLAUDE.md =\n%s\nwant\n%s", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Errorf("AGENTS.md not written: %v", err)
	}
}
