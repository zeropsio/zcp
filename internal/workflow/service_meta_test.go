package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func TestWriteServiceMeta_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	meta := &ServiceMeta{
		Hostname:         "appdev",
		Mode:             "standard",
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeAuto,
		BootstrapSession: "abc123",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}

	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	path := filepath.Join(dir, "services", "appdev.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var got ServiceMeta
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Hostname != "appdev" {
		t.Errorf("hostname: want appdev, got %s", got.Hostname)
	}
	if got.StageHostname != "appstage" {
		t.Errorf("stageHostname: want appstage, got %s", got.StageHostname)
	}
	if got.CloseDeployMode != topology.CloseModeAuto {
		t.Errorf("closeDeployMode: want %s, got %s", topology.CloseModeAuto, got.CloseDeployMode)
	}
}

func TestWriteServiceMeta_CreatesDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	meta := &ServiceMeta{
		Hostname:         "db",
		BootstrapSession: "abc123",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}

	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "services"))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Error("services should be a directory")
	}
}

func TestReadServiceMeta_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	original := &ServiceMeta{
		Hostname:         "appdev",
		Mode:             "standard",
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeAuto,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}

	if err := WriteServiceMeta(dir, original); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	got, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if got.CloseDeployMode != topology.CloseModeAuto {
		t.Errorf("closeDeployMode: want %s, got %s", topology.CloseModeAuto, got.CloseDeployMode)
	}
}

func TestReadServiceMeta_NotFound_ReturnsNil(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	got, err := ReadServiceMeta(dir, "nonexistent")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent meta")
	}
}

func TestServiceMeta_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta ServiceMeta
	}{
		{
			"full",
			ServiceMeta{
				Hostname:         "apidev",
				Mode:             "standard",
				StageHostname:    "apistage",
				CloseDeployMode:  topology.CloseModeAuto,
				BootstrapSession: "sess123",
				BootstrappedAt:   "2026-03-04T12:00:00Z",
			},
		},
		{
			"minimal",
			ServiceMeta{
				Hostname:         "db",
				BootstrapSession: "sess123",
				BootstrappedAt:   "2026-03-04T12:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.meta)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got ServiceMeta
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.Hostname != tt.meta.Hostname {
				t.Errorf("hostname: want %s, got %s", tt.meta.Hostname, got.Hostname)
			}
			if got.CloseDeployMode != tt.meta.CloseDeployMode {
				t.Errorf("closeDeployMode: want %s, got %s", tt.meta.CloseDeployMode, got.CloseDeployMode)
			}
		})
	}
}

func TestServiceMeta_NoDeployFlowField(t *testing.T) {
	t.Parallel()

	// Verify DeployFlow is not serialized — the field should not exist.
	meta := &ServiceMeta{
		Hostname:         "appdev",
		CloseDeployMode:  topology.CloseModeAuto,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["deployFlow"]; ok {
		t.Error("deployFlow field should not exist in JSON output")
	}
}

func TestListServiceMetas_SameDeserializationAsRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	original := &ServiceMeta{
		Hostname:         "appdev",
		Mode:             "standard",
		StageHostname:    "appstage",
		CloseDeployMode:  topology.CloseModeGitPush,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	if err := WriteServiceMeta(dir, original); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	single, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}

	list, err := ListServiceMetas(dir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 meta from list, got %d", len(list))
	}

	// Both paths must produce identical results.
	if single.Hostname != list[0].Hostname {
		t.Errorf("Hostname: Read=%q List=%q", single.Hostname, list[0].Hostname)
	}
	if single.Mode != list[0].Mode {
		t.Errorf("Mode: Read=%q List=%q", single.Mode, list[0].Mode)
	}
	if single.StageHostname != list[0].StageHostname {
		t.Errorf("StageHostname: Read=%q List=%q", single.StageHostname, list[0].StageHostname)
	}
	if single.CloseDeployMode != list[0].CloseDeployMode {
		t.Errorf("CloseDeployMode: Read=%q List=%q", single.CloseDeployMode, list[0].CloseDeployMode)
	}
	if single.BootstrapSession != list[0].BootstrapSession {
		t.Errorf("BootstrapSession: Read=%q List=%q", single.BootstrapSession, list[0].BootstrapSession)
	}
	if single.BootstrappedAt != list[0].BootstrappedAt {
		t.Errorf("BootstrappedAt: Read=%q List=%q", single.BootstrappedAt, list[0].BootstrappedAt)
	}
}

func TestListServiceMetas_MultipleMetas(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	metas := []*ServiceMeta{
		{Hostname: "appdev", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
		{Hostname: "db", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
		{Hostname: "cache", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
	}
	for _, m := range metas {
		if err := WriteServiceMeta(dir, m); err != nil {
			t.Fatalf("WriteServiceMeta(%s): %v", m.Hostname, err)
		}
	}

	got, err := ListServiceMetas(dir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 metas, got %d", len(got))
	}

	// Sort by hostname for deterministic comparison.
	sort.Slice(got, func(i, j int) bool { return got[i].Hostname < got[j].Hostname })
	wantHostnames := []string{"appdev", "cache", "db"}
	for i, want := range wantHostnames {
		if got[i].Hostname != want {
			t.Errorf("metas[%d].Hostname: want %s, got %s", i, want, got[i].Hostname)
		}
	}
}

func TestListServiceMetas_EmptyDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create the services directory but leave it empty.
	if err := os.MkdirAll(filepath.Join(dir, "services"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := ListServiceMetas(dir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 metas, got %d", len(got))
	}
}

func TestListServiceMetas_NonExistentDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Don't create the services directory — should return empty, no error.
	got, err := ListServiceMetas(dir)
	if err != nil {
		t.Fatalf("ListServiceMetas: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want 0 metas for nonexistent dir, got %d", len(got))
	}
}

func TestDeleteServiceMeta_Success(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	meta := &ServiceMeta{
		Hostname:         "appdev",
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	if err := WriteServiceMeta(dir, meta); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	// Verify it exists.
	got, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if got == nil {
		t.Fatal("expected meta to exist before delete")
	}

	// Delete it.
	if err := DeleteServiceMeta(dir, "appdev"); err != nil {
		t.Fatalf("DeleteServiceMeta: %v", err)
	}

	// Verify it's gone.
	got, err = ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatalf("ReadServiceMeta after delete: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete")
	}
}

func TestDeleteServiceMeta_NotFound_NoError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Deleting a non-existent meta should not error (idempotent).
	if err := DeleteServiceMeta(dir, "nonexistent"); err != nil {
		t.Fatalf("DeleteServiceMeta should be idempotent, got: %v", err)
	}
}

func TestDeleteServiceMeta_NoServicesDir_NoError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// No services/ directory at all — should not error.
	if err := DeleteServiceMeta(dir, "anything"); err != nil {
		t.Fatalf("DeleteServiceMeta should be idempotent, got: %v", err)
	}
}

func TestServiceMeta_IsComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		meta     ServiceMeta
		wantDone bool
	}{
		{
			"complete meta with BootstrappedAt",
			ServiceMeta{Hostname: "appdev", BootstrappedAt: "2026-03-04T12:00:00Z"},
			true,
		},
		{
			"incomplete meta without BootstrappedAt",
			ServiceMeta{Hostname: "appdev"},
			false,
		},
		{
			"empty BootstrappedAt string",
			ServiceMeta{Hostname: "appdev", BootstrappedAt: ""},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.meta.IsComplete(); got != tt.wantDone {
				t.Errorf("IsComplete(): want %v, got %v", tt.wantDone, got)
			}
		})
	}
}

// TestServiceMeta_IsDeployed covers the thin wrapper. FirstDeployedAt is
// stamped elsewhere (RecordDeployAttempt for session-driven deploys,
// LocalAutoAdopt for pre-existing ACTIVE services); this test only pins
// the read-side behavior.
func TestServiceMeta_IsDeployed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta ServiceMeta
		want bool
	}{
		{
			"stamped meta",
			ServiceMeta{Hostname: "appdev", BootstrappedAt: "2026-04-18", FirstDeployedAt: "2026-04-20"},
			true,
		},
		{
			"bootstrap-complete but never deployed",
			ServiceMeta{Hostname: "appdev", BootstrappedAt: "2026-04-18"},
			false,
		},
		{
			"zero-value meta",
			ServiceMeta{},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.meta.IsDeployed(); got != tt.want {
				t.Errorf("IsDeployed() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPruneServiceMetas_SkipsEmptyHostname pins the F5 guard: a corrupt/partial
// meta file whose Hostname is empty must NOT leak into the returned deleted list
// as "" (the cleanedUpOrphanMetas:[""] artifact). Reproduces it with a raw
// on-disk meta carrying an empty hostname.
func TestPruneServiceMetas_SkipsEmptyHostname(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	servicesDir := filepath.Join(dir, "services")
	if err := os.MkdirAll(servicesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A valid live meta plus a corrupt empty-hostname meta file.
	if err := WriteServiceMeta(dir, &ServiceMeta{
		Hostname: "app", Mode: topology.ModeSimple, BootstrappedAt: "2026-05-06",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(servicesDir, "corrupt.json"), []byte(`{"hostname":""}`), 0o644); err != nil {
		t.Fatalf("write corrupt meta: %v", err)
	}

	pruned := PruneServiceMetas(dir, map[string]bool{"app": true})

	for _, h := range pruned {
		if h == "" {
			t.Errorf("prune leaked an empty-hostname entry into the deleted list: %#v", pruned)
		}
	}
}

func TestPruneServiceMetas_RemovesStaleEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		diskMetas     []*ServiceMeta
		liveHostnames []string
		wantRemaining []string
	}{
		{
			"removes stale metas not in live project",
			[]*ServiceMeta{
				{Hostname: "appdev", Mode: "standard", StageHostname: "appstage", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
				{Hostname: "docs", Mode: "simple", BootstrapSession: "s2", BootstrappedAt: "2026-03-04T12:00:00Z"},
				{Hostname: "ghost", BootstrapSession: "s3", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			[]string{"appdev", "appstage", "db"},
			[]string{"appdev"},
		},
		{
			"keeps meta when stage hostname is live",
			[]*ServiceMeta{
				{Hostname: "appdev", Mode: "standard", StageHostname: "appstage", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			[]string{"appstage"}, // only stage exists
			[]string{"appdev"},
		},
		{
			"removes all when none are live",
			[]*ServiceMeta{
				{Hostname: "old1", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
				{Hostname: "old2", BootstrapSession: "s2", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			[]string{"unrelated"},
			nil,
		},
		{
			"keeps all when all are live",
			[]*ServiceMeta{
				{Hostname: "a", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
				{Hostname: "b", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			[]string{"a", "b"},
			[]string{"a", "b"},
		},
		{
			"empty live set removes all",
			[]*ServiceMeta{
				{Hostname: "x", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			nil,
			nil,
		},
		{
			// Regression: local-only metas use project-name as Hostname (set by
			// LocalAutoAdopt to project.Name), not a service name. liveHostnames
			// holds service names — the project name is never in it. Pruning
			// against this predicate falsely orphaned every local-only meta on
			// every develop_start, surfacing as spurious ADOPT_REQUIRED
			// rejections. local-only and local-stage are project-keyed; they
			// must be skipped entirely by the orphan check.
			"local-only meta is project-keyed and survives prune",
			[]*ServiceMeta{
				{Hostname: "eval-zcp", Mode: topology.PlanModeLocalOnly, BootstrapSession: "", BootstrappedAt: "2026-05-06"},
			},
			[]string{"app", "db", "zcp"}, // services in the project — none equal project name
			[]string{"eval-zcp"},
		},
		{
			// Sharper local-stage case: even when neither projectName nor
			// stageHostname is in liveHostnames (e.g. user deleted the linked
			// stage runtime in the dashboard), the local meta must still
			// survive — it's project-keyed and the agent re-links via
			// `adopt-local`. Pruning it would silently kick the project back
			// to "no meta" → ADOPT_REQUIRED on the next develop_start.
			"local-stage meta survives even when stage host is gone",
			[]*ServiceMeta{
				{Hostname: "eval-zcp", Mode: topology.PlanModeLocalStage, StageHostname: "deleted-stage", BootstrappedAt: "2026-05-06"},
			},
			[]string{"app", "db"}, // stage host disappeared
			[]string{"eval-zcp"},
		},
		{
			"local-only meta survives even when its project-name happens to collide with no service",
			[]*ServiceMeta{
				{Hostname: "myproject", Mode: topology.PlanModeLocalOnly, BootstrappedAt: "2026-05-06"},
				{Hostname: "stalecontainer", Mode: topology.ModeStandard, StageHostname: "stalecontainerstage", BootstrappedAt: "2026-05-06"},
			},
			[]string{"app", "db"}, // neither container hostname is live → container meta pruned, local-only kept
			[]string{"myproject"},
		},
		{
			// Explicit container-mode regression: dev meta whose hostname is
			// no longer live MUST still be pruned. The local-mode skip is
			// narrow (PlanModeLocalOnly + PlanModeLocalStage only); every
			// container Mode (dev / standard / stage / simple / "") must
			// continue to flow through the live-hostname predicate.
			"container dev meta still prunes when stale",
			[]*ServiceMeta{
				{Hostname: "apidev", Mode: topology.ModeDev, BootstrappedAt: "2026-05-06"},
				{Hostname: "appdev", Mode: topology.ModeDev, BootstrappedAt: "2026-05-06"},
			},
			[]string{"apidev", "db"}, // appdev gone
			[]string{"apidev"},
		},
		{
			"container simple meta still prunes when stale",
			[]*ServiceMeta{
				{Hostname: "docs", Mode: topology.ModeSimple, BootstrappedAt: "2026-05-06"},
			},
			[]string{"app", "db"}, // docs gone
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			for _, m := range tt.diskMetas {
				if err := WriteServiceMeta(dir, m); err != nil {
					t.Fatalf("WriteServiceMeta(%s): %v", m.Hostname, err)
				}
			}

			live := make(map[string]bool, len(tt.liveHostnames))
			for _, h := range tt.liveHostnames {
				live[h] = true
			}

			pruned := PruneServiceMetas(dir, live)

			remaining, err := ListServiceMetas(dir)
			if err != nil {
				t.Fatalf("ListServiceMetas: %v", err)
			}

			gotHostnames := make([]string, 0, len(remaining))
			for _, m := range remaining {
				gotHostnames = append(gotHostnames, m.Hostname)
			}
			sort.Strings(gotHostnames)
			sort.Strings(tt.wantRemaining)

			if len(gotHostnames) != len(tt.wantRemaining) {
				t.Fatalf("remaining metas: want %v, got %v", tt.wantRemaining, gotHostnames)
			}
			for i := range gotHostnames {
				if gotHostnames[i] != tt.wantRemaining[i] {
					t.Errorf("remaining[%d]: want %s, got %s", i, tt.wantRemaining[i], gotHostnames[i])
				}
			}

			// Verify pruned count matches removed count.
			wantPruned := len(tt.diskMetas) - len(tt.wantRemaining)
			if len(pruned) != wantPruned {
				t.Errorf("pruned count: want %d, got %d (deleted=%v)", wantPruned, len(pruned), pruned)
			}
		})
	}
}

func TestIsKnownService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metas    []*ServiceMeta
		hostname string
		want     bool
	}{
		{
			"direct hostname match",
			[]*ServiceMeta{
				{Hostname: "appdev", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			"appdev",
			true,
		},
		{
			"stage hostname match",
			[]*ServiceMeta{
				{Hostname: "appdev", StageHostname: "appstage", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			"appstage",
			true,
		},
		{
			"unknown hostname",
			[]*ServiceMeta{
				{Hostname: "appdev", StageHostname: "appstage", BootstrapSession: "s1", BootstrappedAt: "2026-03-04T12:00:00Z"},
			},
			"docs",
			false,
		},
		{
			"incomplete meta still counts",
			[]*ServiceMeta{
				{Hostname: "appdev", BootstrapSession: "s1"},
			},
			"appdev",
			true,
		},
		{
			"empty stateDir returns false",
			nil,
			"appdev",
			false,
		},
		{
			"empty hostname returns false",
			[]*ServiceMeta{
				{Hostname: "appdev", BootstrapSession: "s1"},
			},
			"",
			false,
		},
		{
			"no metas at all",
			nil,
			"anything",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stateDir string
			if tt.name != "empty stateDir returns false" {
				stateDir = t.TempDir()
				for _, m := range tt.metas {
					if err := WriteServiceMeta(stateDir, m); err != nil {
						t.Fatalf("WriteServiceMeta: %v", err)
					}
				}
			}

			got := IsKnownService(stateDir, tt.hostname)
			if got != tt.want {
				t.Errorf("IsKnownService(%q): want %v, got %v", tt.hostname, tt.want, got)
			}
		})
	}
}

func TestManagedRuntimeIndex(t *testing.T) {
	t.Parallel()

	standardPair := &ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             topology.PlanModeStandard,
		BootstrapSession: "s1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	devOnly := &ServiceMeta{
		Hostname:         "api",
		Mode:             topology.PlanModeDev,
		BootstrapSession: "s2",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	simple := &ServiceMeta{
		Hostname:         "worker",
		Mode:             topology.PlanModeSimple,
		BootstrapSession: "s3",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	localStage := &ServiceMeta{
		Hostname:         "myproject",
		StageHostname:    "appstage",
		Mode:             topology.PlanModeLocalStage,
		BootstrapSession: "s4",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	incomplete := &ServiceMeta{
		Hostname:         "orphan",
		BootstrapSession: "s5",
		// BootstrappedAt intentionally empty — IsComplete() == false.
	}

	t.Run("standard pair maps both keys to same pointer", func(t *testing.T) {
		t.Parallel()
		idx := ManagedRuntimeIndex([]*ServiceMeta{standardPair})
		if len(idx) != 2 {
			t.Fatalf("want 2 entries, got %d: %v", len(idx), idx)
		}
		if idx["appdev"] != standardPair {
			t.Errorf("idx[appdev] = %p, want %p", idx["appdev"], standardPair)
		}
		if idx["appstage"] != standardPair {
			t.Errorf("idx[appstage] = %p, want %p", idx["appstage"], standardPair)
		}
		if idx["appdev"] != idx["appstage"] {
			t.Errorf("both keys must point at the same *ServiceMeta")
		}
	})

	t.Run("dev-only meta has one key", func(t *testing.T) {
		t.Parallel()
		idx := ManagedRuntimeIndex([]*ServiceMeta{devOnly})
		if len(idx) != 1 {
			t.Fatalf("want 1 entry, got %d", len(idx))
		}
		if idx["api"] != devOnly {
			t.Errorf("idx[api] mismatch")
		}
	})

	t.Run("simple mode has one key", func(t *testing.T) {
		t.Parallel()
		idx := ManagedRuntimeIndex([]*ServiceMeta{simple})
		if len(idx) != 1 {
			t.Fatalf("want 1 entry, got %d", len(idx))
		}
	})

	t.Run("local-stage inverted mode maps both keys", func(t *testing.T) {
		t.Parallel()
		idx := ManagedRuntimeIndex([]*ServiceMeta{localStage})
		if len(idx) != 2 {
			t.Fatalf("want 2 entries, got %d: %v", len(idx), idx)
		}
		if idx["myproject"] != localStage {
			t.Errorf("idx[myproject] mismatch — inverted mode uses project name as Hostname")
		}
		if idx["appstage"] != localStage {
			t.Errorf("idx[appstage] mismatch")
		}
	})

	t.Run("incomplete meta is NOT filtered by helper (caller's predicate)", func(t *testing.T) {
		t.Parallel()
		idx := ManagedRuntimeIndex([]*ServiceMeta{incomplete})
		if len(idx) != 1 {
			t.Fatalf("want 1 entry (helper does not filter on IsComplete), got %d", len(idx))
		}
		if idx["orphan"] != incomplete {
			t.Errorf("idx[orphan] mismatch")
		}
	})

	t.Run("nil meta is skipped", func(t *testing.T) {
		t.Parallel()
		idx := ManagedRuntimeIndex([]*ServiceMeta{nil, devOnly, nil})
		if len(idx) != 1 {
			t.Fatalf("want 1 entry, got %d", len(idx))
		}
	})

	t.Run("empty hostname is skipped, never poisons map", func(t *testing.T) {
		t.Parallel()
		blank := &ServiceMeta{Hostname: "", StageHostname: "appstage"}
		idx := ManagedRuntimeIndex([]*ServiceMeta{blank, devOnly})
		if _, present := idx[""]; present {
			t.Errorf("empty-string key must never appear in index")
		}
		if len(idx) != 1 {
			t.Fatalf("want 1 entry (only devOnly valid), got %d: %v", len(idx), idx)
		}
	})

	t.Run("duplicate hostname: last meta wins (documented map semantics)", func(t *testing.T) {
		t.Parallel()
		first := &ServiceMeta{Hostname: "app", Mode: topology.PlanModeDev}
		second := &ServiceMeta{Hostname: "app", Mode: topology.PlanModeSimple}
		idx := ManagedRuntimeIndex([]*ServiceMeta{first, second})
		if len(idx) != 1 {
			t.Fatalf("want 1 entry, got %d", len(idx))
		}
		if idx["app"] != second {
			t.Errorf("want second (last-write-wins), got %p", idx["app"])
		}
	})

	t.Run("nil input returns empty map (no panic)", func(t *testing.T) {
		t.Parallel()
		idx := ManagedRuntimeIndex(nil)
		if idx == nil {
			t.Fatalf("must return non-nil map even on nil input")
		}
		if len(idx) != 0 {
			t.Fatalf("want empty map, got %d entries", len(idx))
		}
	})

	t.Run("empty slice returns empty map", func(t *testing.T) {
		t.Parallel()
		idx := ManagedRuntimeIndex([]*ServiceMeta{})
		if len(idx) != 0 {
			t.Fatalf("want empty map, got %d", len(idx))
		}
	})

	t.Run("multiple non-colliding metas merge correctly", func(t *testing.T) {
		t.Parallel()
		// Distinct hostnames — realistic multi-stack project state.
		pairA := &ServiceMeta{Hostname: "appdev", StageHostname: "appstage", Mode: topology.PlanModeStandard}
		pairB := &ServiceMeta{Hostname: "apidev", StageHostname: "apistage", Mode: topology.PlanModeStandard}
		idx := ManagedRuntimeIndex([]*ServiceMeta{pairA, devOnly, simple, pairB})
		// 2 (pairA) + 1 (devOnly) + 1 (simple) + 2 (pairB) = 6
		if len(idx) != 6 {
			t.Fatalf("want 6 entries across 4 metas, got %d: %v", len(idx), idx)
		}
		wantPairs := map[string]*ServiceMeta{
			"appdev":   pairA,
			"appstage": pairA,
			"api":      devOnly,
			"worker":   simple,
			"apidev":   pairB,
			"apistage": pairB,
		}
		for h, want := range wantPairs {
			if idx[h] != want {
				t.Errorf("idx[%q] = %p, want %p", h, idx[h], want)
			}
		}
	})
}

func TestServiceMeta_NoStatusFieldInJSON(t *testing.T) {
	t.Parallel()

	meta := &ServiceMeta{
		Hostname:         "appdev",
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-03-04T12:00:00Z",
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["status"]; ok {
		t.Error("status field should NOT exist in JSON — Status was removed")
	}
	if _, ok := raw["type"]; ok {
		t.Error("type field should NOT exist in JSON — Type was removed")
	}
	if _, ok := raw["decisions"]; ok {
		t.Error("decisions field should NOT exist in JSON — replaced by closeDeployMode + gitPushState + buildIntegration")
	}
	if _, ok := raw["dependencies"]; ok {
		t.Error("dependencies field should NOT exist in JSON — removed (never read in production)")
	}
}

func TestServiceMeta_PrimaryRole(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		meta ServiceMeta
		want topology.Mode
	}{
		{
			"container_standard_returns_dev",
			ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeStandard, StageHostname: "appstage"},
			topology.DeployRoleDev,
		},
		{
			"container_dev_returns_dev",
			ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeDev},
			topology.DeployRoleDev,
		},
		{
			"container_simple_returns_simple",
			ServiceMeta{Hostname: "app", Mode: topology.PlanModeSimple},
			topology.DeployRoleSimple,
		},
		{
			// Local metas use local-stage / local-only exclusively, never
			// standard. PrimaryRole on a local-stage meta is still Dev
			// because the dev half of the topology lives on the user's
			// machine — the Zerops side is the stage runtime.
			"local_stage_returns_stage",
			ServiceMeta{Hostname: "myproject", StageHostname: "appstage", Mode: topology.PlanModeLocalStage},
			topology.DeployRoleDev,
		},
		{
			"local_dev_returns_dev",
			ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeDev},
			topology.DeployRoleDev,
		},
		{
			"local_simple_returns_simple",
			ServiceMeta{Hostname: "app", Mode: topology.PlanModeSimple},
			topology.DeployRoleSimple,
		},
		{
			"empty_mode_defaults_to_standard",
			ServiceMeta{Hostname: "appdev", StageHostname: "appstage"},
			topology.DeployRoleDev,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.meta.PrimaryRole(); got != tt.want {
				t.Errorf("PrimaryRole() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServiceMeta_RoleFor(t *testing.T) {
	t.Parallel()
	meta := ServiceMeta{
		Hostname: "appdev", Mode: topology.PlanModeStandard, StageHostname: "appstage",
	}
	tests := []struct {
		hostname string
		want     topology.Mode
	}{
		{"appdev", topology.DeployRoleDev},
		{"appstage", topology.DeployRoleStage},
		{"unrelated", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			t.Parallel()
			if got := meta.RoleFor(tt.hostname); got != tt.want {
				t.Errorf("RoleFor(%q) = %q, want %q", tt.hostname, got, tt.want)
			}
		})
	}
}

// TestServiceMeta_ModeFor pins the target-relative envelope-Mode projection
// that lifts RoleFor's role into the Mode vocabulary. Replaces the prior
// private resolveEnvelopeMode (whose unit tests lived in
// compute_envelope_test.go); promoted to a method so deploy_poll +
// deploy_subdomain + subdomain (which previously read meta.Mode directly
// and misclassified pair-keyed metas on the stage half) share one source
// of truth with the envelope projection.
func TestServiceMeta_ModeFor(t *testing.T) {
	t.Parallel()

	standardPair := &ServiceMeta{
		Hostname: "appdev", StageHostname: "appstage", Mode: topology.PlanModeStandard,
	}
	localStagePair := &ServiceMeta{
		Hostname: "myproject", StageHostname: "apistage", Mode: topology.PlanModeLocalStage,
	}
	devOnly := &ServiceMeta{Hostname: "appdev", Mode: topology.PlanModeDev}
	simple := &ServiceMeta{Hostname: "app", Mode: topology.PlanModeSimple}
	localOnly := &ServiceMeta{Hostname: "myproject", Mode: topology.PlanModeLocalOnly}

	tests := []struct {
		name     string
		meta     *ServiceMeta
		hostname string
		want     topology.Mode
	}{
		// Standard pair — both halves project distinctly so deploy/subdomain
		// surfaces don't read the dev-half's deferred-start onto the stage
		// half (the bug fixed by promoting this projection).
		{"standard_dev_half", standardPair, "appdev", topology.ModeStandard},
		{"standard_stage_half", standardPair, "appstage", topology.ModeStage},

		// Local-stage pair — stage half MUST stay ModeLocalStage (not
		// ModeStage) so atoms / predicates that key on local-* keep
		// matching when target is the local-stage pair's stage hostname.
		// The project-name "host" of a local-stage meta is not a deployable
		// target and projects as "" (no role).
		{"local_stage_pair_stage_half", localStagePair, "apistage", topology.ModeLocalStage},
		{"local_stage_pair_project_name_no_role", localStagePair, "myproject", ""},

		// Single-runtime metas — no stage half; PrimaryRole drives the
		// projection and m.Mode passes through.
		{"dev_only", devOnly, "appdev", topology.ModeDev},
		{"simple", simple, "app", topology.ModeSimple},

		// Local-only — project-keyed; m.Hostname is the project name, no
		// per-service role. Deploy doesn't apply but the helper must
		// gracefully return "" rather than synthesizing a misleading mode.
		{"local_only_project_name_no_role", localOnly, "myproject", ""},

		// Unrelated host + nil meta — both must return "" so callers fall
		// through to the generic "no meta available" branch (e.g.
		// resolveDeployTargetTopology's empty-tuple return).
		{"unrelated_hostname", standardPair, "other", ""},
		{"empty_hostname", standardPair, "", ""},
		{"nil_meta", nil, "appdev", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.meta.ModeFor(tt.hostname); got != tt.want {
				t.Errorf("ModeFor(%+v, %q) = %q, want %q", tt.meta, tt.hostname, got, tt.want)
			}
		})
	}
}

// TestServiceMeta_ModeFor_StandardPairStageHalf_NotDeferredStart pins the
// regression that motivated promoting this method: a standard pair stage
// deploy with a dynamic runtime previously read meta.Mode == ModeStandard
// and matched topology.IsDeferredStart, producing a misleading "start dev
// server" next-action on a runtime that runs run.start. ModeFor returns
// ModeStage so IsDeferredStart returns false — the canonical "verify"
// pointer applies.
func TestServiceMeta_ModeFor_StandardPairStageHalf_NotDeferredStart(t *testing.T) {
	t.Parallel()
	meta := &ServiceMeta{
		Hostname: "appdev", StageHostname: "appstage", Mode: topology.PlanModeStandard,
	}
	got := meta.ModeFor("appstage")
	if got != topology.ModeStage {
		t.Fatalf("standard-pair stage half ModeFor = %q, want %q", got, topology.ModeStage)
	}
	if topology.IsDeferredStart(got, topology.RuntimeDynamic) {
		t.Errorf("IsDeferredStart(%q, RuntimeDynamic) = true, want false (stage runs run.start, not zsc-noop)",
			got)
	}
}

// TestServiceMeta_ModeFor_StandardPairDevHalf_StillDeferredStart guards the
// dev half — its semantics did not change. Standard pair dev half + dynamic
// runtime IS deferred-start (zsc-noop until zerops_dev_server start).
func TestServiceMeta_ModeFor_StandardPairDevHalf_StillDeferredStart(t *testing.T) {
	t.Parallel()
	meta := &ServiceMeta{
		Hostname: "appdev", StageHostname: "appstage", Mode: topology.PlanModeStandard,
	}
	got := meta.ModeFor("appdev")
	if got != topology.ModeStandard {
		t.Fatalf("standard-pair dev half ModeFor = %q, want %q", got, topology.ModeStandard)
	}
	if !topology.IsDeferredStart(got, topology.RuntimeDynamic) {
		t.Errorf("IsDeferredStart(%q, RuntimeDynamic) = false, want true (dev half is zsc-noop)", got)
	}
}

func TestServiceMeta_Hostnames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		meta ServiceMeta
		want []string
	}{
		{"dev_only", ServiceMeta{Hostname: "appdev"}, []string{"appdev"}},
		{"dev_and_stage", ServiceMeta{Hostname: "appdev", StageHostname: "appstage"}, []string{"appdev", "appstage"}},
		{"simple", ServiceMeta{Hostname: "app"}, []string{"app"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.meta.Hostnames()
			if len(got) != len(tt.want) {
				t.Fatalf("Hostnames() len = %d, want %d (%v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Hostnames()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestServiceMeta_IsAdopted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		meta ServiceMeta
		want bool
	}{
		{
			"adopted_complete_empty_session",
			ServiceMeta{Hostname: "appdev", BootstrapSession: "", BootstrappedAt: "2026-04-18"},
			true,
		},
		{
			"fresh_bootstrap_session_set",
			ServiceMeta{Hostname: "appdev", BootstrapSession: "sess1", BootstrappedAt: "2026-04-18"},
			false,
		},
		{
			"orphan_incomplete_empty_session_not_adopted",
			ServiceMeta{Hostname: "appdev", BootstrapSession: "", BootstrappedAt: ""},
			false,
		},
		{
			"incomplete_with_session_not_adopted",
			ServiceMeta{Hostname: "appdev", BootstrapSession: "sess1", BootstrappedAt: ""},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.meta.IsAdopted(); got != tt.want {
				t.Errorf("IsAdopted() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPushSourceCheckFor pins the four-result discrimination introduced when
// the boolean IsPushSourceFor was replaced. The legacy form collapsed every
// non-OK case onto a single false, which produced the "X instead of X"
// rendering when handlers spliced both input.Service and meta.Hostname into
// the error message — both were the same value for standalone ModeDev.
// The new form returns:
//
//   - PushSourceOK: hostname == meta.Hostname AND mode supports push.
//   - PushSourceIsStageHalf: hostname == meta.StageHostname (legitimate "push
//     from dev half" remediation case).
//   - PushSourceModeUnsupported: hostname == meta.Hostname BUT mode is ModeDev
//     standalone or ModeStage — handler renders "run mode-expansion first".
//   - PushSourceUnknownHost: hostname is neither meta.Hostname nor
//     meta.StageHostname — meta lookup matched a different service.
func TestPushSourceCheckFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		meta     *ServiceMeta
		hostname string
		want     topology.PushSourceResult
	}{
		{
			name:     "nil meta returns unknown",
			meta:     nil,
			hostname: "appdev",
			want:     topology.PushSourceUnknownHost,
		},
		{
			name:     "empty hostname returns unknown",
			meta:     &ServiceMeta{Hostname: "appdev", Mode: topology.ModeStandard},
			hostname: "",
			want:     topology.PushSourceUnknownHost,
		},
		{
			name:     "standard pair dev half is OK",
			meta:     &ServiceMeta{Hostname: "appdev", StageHostname: "appstage", Mode: topology.ModeStandard},
			hostname: "appdev",
			want:     topology.PushSourceOK,
		},
		{
			name:     "standard pair stage half is stage-half result",
			meta:     &ServiceMeta{Hostname: "appdev", StageHostname: "appstage", Mode: topology.ModeStandard},
			hostname: "appstage",
			want:     topology.PushSourceIsStageHalf,
		},
		{
			name:     "standalone ModeDev returns mode-unsupported (regression for X-instead-of-X bug)",
			meta:     &ServiceMeta{Hostname: "remindersdev", Mode: topology.ModeDev},
			hostname: "remindersdev",
			want:     topology.PushSourceModeUnsupported,
		},
		{
			name:     "standalone ModeStage as meta.Hostname returns mode-unsupported",
			meta:     &ServiceMeta{Hostname: "appstage", Mode: topology.ModeStage},
			hostname: "appstage",
			want:     topology.PushSourceModeUnsupported,
		},
		{
			name:     "ModeSimple is OK",
			meta:     &ServiceMeta{Hostname: "app", Mode: topology.ModeSimple},
			hostname: "app",
			want:     topology.PushSourceOK,
		},
		{
			name:     "ModeLocalStage is OK",
			meta:     &ServiceMeta{Hostname: "app", Mode: topology.ModeLocalStage},
			hostname: "app",
			want:     topology.PushSourceOK,
		},
		{
			// Phase 12 carve-out: local-stage's StageHostname IS a valid
			// deploy target. Pre-fix the meta returned IsStageHalf for
			// any stage-hostname match, suggesting the user "push from
			// the dev half" — but for local-stage the dev half is the
			// user's CWD (m.Hostname), not a Zerops service that can
			// receive a deploy. Returning OK lets the deploy proceed.
			name:     "ModeLocalStage stage hostname is OK (local-stage carve-out)",
			meta:     &ServiceMeta{Hostname: "myproject", StageHostname: "apistage", Mode: topology.ModeLocalStage},
			hostname: "apistage",
			want:     topology.PushSourceOK,
		},
		{
			name:     "ModeLocalOnly is OK",
			meta:     &ServiceMeta{Hostname: "app", Mode: topology.ModeLocalOnly},
			hostname: "app",
			want:     topology.PushSourceOK,
		},
		{
			name:     "unknown hostname returns unknown-host",
			meta:     &ServiceMeta{Hostname: "appdev", StageHostname: "appstage", Mode: topology.ModeStandard},
			hostname: "otherservice",
			want:     topology.PushSourceUnknownHost,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.meta.PushSourceCheckFor(tt.hostname); got != tt.want {
				t.Errorf("PushSourceCheckFor(%q) = %v, want %v", tt.hostname, got, tt.want)
			}
		})
	}
}
