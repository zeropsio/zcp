package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// devKeyedPair is a dev/stage pair as the retire finds it: keyed by its dev
// service, every carried and every dropped field set.
func devKeyedPair() *ServiceMeta {
	return &ServiceMeta{
		Hostname:                   "appdev",
		Mode:                       topology.PlanModeStandard,
		Stages:                     []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		CloseDeployMode:            topology.CloseModeAuto,
		CloseDeployModeConfirmed:   true,
		GitPushState:               topology.GitPushConfigured,
		RemoteURL:                  "https://hq.example/a1/appdev.git",
		BuildIntegration:           topology.BuildIntegrationActions,
		TrackedRef:                 "main",
		BuildIntegrationVerifiedAt: "2026-10-02T10:00:00Z",
		BootstrappedAt:             "2026-10-01T10:00:00Z",
		FirstDeployedAt:            "2026-10-01T11:00:00Z",
		PrimarySetupName:           "dev",
		ProdSetupName:              "prod",
		ServesHTTPByHost:           map[string]bool{"appdev": true, "appstage": false},
		ProdLaunches:               []ProdLaunchRef{{ProdProjectID: "prod-1", ProdHostname: "app", LaunchedAt: "2026-10-03T10:00:00Z"}},
		PublicAccess: map[string]topology.PublicAccessRecord{
			"appdev":   {Intent: topology.PublicAccessSubdomain, SubdomainEnabledByZcpAt: "2026-10-01T11:01:00Z"},
			"appstage": {Intent: topology.PublicAccessAuto, SubdomainEnabledByZcpAt: "2026-10-01T11:02:00Z"},
		},
		Repo: &topology.Repo{BaselineAppVersion: "av-1", Provenance: topology.RepoProvenanceExisting},
		HQ: &HQRepoRef{
			AppID: "app-1", Repo: "appdev", WiredAt: "2026-10-01T12:00:00Z",
			CredentialFingerprint: "fp-dev", Change: 7, Branch: "feat-x",
		},
	}
}

func appPoolMove() PoolPairMove {
	return PoolPairMove{Dev: "appdev", Pool: "apppool", PoolServiceID: "pool-id", PoolSetup: "pool"}
}

func TestDevIsPool_OnlyAMarkedMeta(t *testing.T) {
	t.Parallel()
	var none *ServiceMeta
	cases := []struct {
		name string
		meta *ServiceMeta
		want bool
	}{
		{"nil meta", none, false},
		{"a dev-keyed pair", devKeyedPair(), false},
		{"a pool-keyed pair", &ServiceMeta{Hostname: "apppool", DevPool: &DevPoolRef{ServiceID: "pool-id"}}, true},
	}
	for _, tc := range cases {
		if got := tc.meta.DevIsPool(); got != tc.want {
			t.Errorf("%s: DevIsPool = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestMigratePairToPool_KeysThePairOnThePool pins design 2's carry/drop: the
// pair is keyed by the pool and marked, every pair-wide stamp and the stage's
// own records stay, and every fact about the dev service's own container
// leaves the pair (kept aside under the marker, never read as the pool's).
func TestMigratePairToPool_KeysThePairOnThePool(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteServiceMeta(dir, devKeyedPair()); err != nil {
		t.Fatal(err)
	}

	got, err := MigratePairToPool(dir, appPoolMove())
	if err != nil {
		t.Fatalf("MigratePairToPool: %v", err)
	}
	onDisk, err := ReadServiceMeta(dir, "apppool")
	if err != nil || onDisk == nil {
		t.Fatalf("the pool-keyed file: %+v, %v", onDisk, err)
	}
	if !reflect.DeepEqual(got, onDisk) {
		t.Errorf("returned meta differs from the file:\n got %+v\nfile %+v", got, onDisk)
	}

	was := devKeyedPair()
	want := devKeyedPair()
	want.Hostname = "apppool"
	want.PrimarySetupName = "pool"
	want.Repo = nil
	want.ServesHTTPByHost = map[string]bool{"appstage": false}
	want.PublicAccess = map[string]topology.PublicAccessRecord{
		"apppool":  {Intent: topology.PublicAccessSubdomain},
		"appstage": was.PublicAccess["appstage"],
	}
	want.HQ.CredentialFingerprint = ""
	serves := true
	devAccess := was.PublicAccess["appdev"]
	want.DevPool = &DevPoolRef{
		ServiceID: "pool-id",
		MigratedFrom: &MigratedDev{
			Hostname: "appdev", PrimarySetupName: "dev", Repo: was.Repo,
			ServesHTTP: &serves, PublicAccess: &devAccess, CredentialFingerprint: "fp-dev",
		},
	}
	if !reflect.DeepEqual(onDisk, want) {
		t.Errorf("pool-keyed pair:\n got %+v\nwant %+v", onDisk, want)
	}
	if !onDisk.DevIsPool() {
		t.Error("the pool-keyed pair is not marked as a pool dev half")
	}
}

// TestMigratePairToPool_RenamesTheFileInOneLock pins the re-key on disk: the
// dev-keyed file is gone, the stage resolves to the pool-keyed pair, and the
// dev's hostname names no pair any more (a later service taking it never
// becomes a dev half).
func TestMigratePairToPool_RenamesTheFileInOneLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteServiceMeta(dir, devKeyedPair()); err != nil {
		t.Fatal(err)
	}
	if _, err := MigratePairToPool(dir, appPoolMove()); err != nil {
		t.Fatalf("MigratePairToPool: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "services", "appdev.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("services/appdev.json still there (err=%v)", err)
	}
	stage, err := FindServiceMeta(dir, "appstage")
	if err != nil || stage == nil || stage.Hostname != "apppool" {
		t.Errorf("FindServiceMeta(appstage) = %+v, %v; want the pool-keyed pair", stage, err)
	}
	if dev, err := FindServiceMeta(dir, "appdev"); err != nil || dev != nil {
		t.Errorf("FindServiceMeta(appdev) = %+v, %v; want no pair", dev, err)
	}
	if IsKnownService(dir, "appdev") {
		t.Error("the retired dev's hostname is still a known service")
	}
	metas, err := ListServiceMetas(dir)
	if err != nil || len(metas) != 1 {
		t.Fatalf("ListServiceMetas = %d metas, %v; want exactly one pair", len(metas), err)
	}
	if got := metas[0].Hostnames(); !reflect.DeepEqual(got, []string{"apppool", "appstage"}) {
		t.Errorf("Hostnames = %v, want [apppool appstage]", got)
	}
}

func TestMigratePairToPool_DevOnlyService_KeepsItsMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dev := NewServiceMeta("partdev", topology.PlanModeDev)
	dev.BootstrappedAt = "2026-10-01T10:00:00Z"
	if err := WriteServiceMeta(dir, dev); err != nil {
		t.Fatal(err)
	}
	got, err := MigratePairToPool(dir, PoolPairMove{Dev: "partdev", Pool: "partpool", PoolServiceID: "p"})
	if err != nil {
		t.Fatalf("MigratePairToPool: %v", err)
	}
	if got.Hostname != "partpool" || got.Mode != topology.PlanModeDev || got.HasStage() || !got.DevIsPool() {
		t.Errorf("dev-only service moved as %+v", got)
	}
}

func TestMigratePairToPool_NotAMovablePair_Refused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		meta  func() *ServiceMeta
		move  PoolPairMove
		other *ServiceMeta
		want  string
	}{
		{"no pair keyed by the dev", nil, appPoolMove(), nil, "not found"},
		{"simple mode", func() *ServiceMeta { m := devKeyedPair(); m.Mode, m.Stages = topology.PlanModeSimple, nil; return m }, appPoolMove(), nil, "simple"},
		{"local mode", func() *ServiceMeta { m := devKeyedPair(); m.Mode = topology.PlanModeLocalStage; return m }, appPoolMove(), nil, "local-stage"},
		{"variant B", func() *ServiceMeta {
			m := devKeyedPair()
			m.CodeVolume = &topology.CodeVolume{Hostname: "code", MountPath: "/var/www/code"}
			return m
		}, appPoolMove(), nil, "code volume"},
		{"bootstrap not finished", func() *ServiceMeta { m := devKeyedPair(); m.BootstrappedAt = ""; return m }, appPoolMove(), nil, "bootstrap"},
		{"already a pool pair", func() *ServiceMeta {
			m := devKeyedPair()
			m.DevPool = &DevPoolRef{ServiceID: "other"}
			return m
		}, appPoolMove(), nil, "already"},
		{"the pool is one of the pair's stages", devKeyedPair, PoolPairMove{Dev: "appdev", Pool: "appstage"}, nil, "stage"},
		{"another pair keyed by the pool hostname", devKeyedPair, appPoolMove(), &ServiceMeta{Hostname: "apppool", Mode: topology.PlanModeDev, BootstrappedAt: "t"}, "apppool"},
		{"another pair names the pool hostname", devKeyedPair, appPoolMove(), &ServiceMeta{Hostname: "webdev", Mode: topology.PlanModeStandard, Stages: []topology.StageRef{{Hostname: "apppool"}}, BootstrappedAt: "t"}, "webdev"},
		{"dev and pool the same hostname", devKeyedPair, PoolPairMove{Dev: "appdev", Pool: "appdev"}, nil, "two hostnames"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tc.meta != nil {
				if err := WriteServiceMeta(dir, tc.meta()); err != nil {
					t.Fatal(err)
				}
			}
			if tc.other != nil {
				if err := WriteServiceMeta(dir, tc.other); err != nil {
					t.Fatal(err)
				}
			}
			before := metaFiles(t, dir)
			_, err := MigratePairToPool(dir, tc.move)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a refusal naming %q", err, tc.want)
			}
			if after := metaFiles(t, dir); !reflect.DeepEqual(after, before) {
				t.Errorf("a refused move changed the metas:\nbefore %v\n after %v", before, after)
			}
		})
	}
}

// TestMigratePairToPool_Resume pins the retire's resume: a move cut between
// writing the pool-keyed file and removing the dev-keyed one finishes, and a
// finished move is a no-op — never a second carry over what HQ wrote since.
func TestMigratePairToPool_Resume(t *testing.T) {
	t.Parallel()
	t.Run("a half-done move finishes", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := WriteServiceMeta(dir, devKeyedPair()); err != nil {
			t.Fatal(err)
		}
		moved, err := devKeyedPair().keyedOnPool(appPoolMove())
		if err != nil {
			t.Fatal(err)
		}
		moved.HQ.Change = 8 // HQ wrote to the pool-keyed pair before the cut
		if err := WriteServiceMeta(dir, moved); err != nil {
			t.Fatal(err)
		}
		got, err := MigratePairToPool(dir, appPoolMove())
		if err != nil {
			t.Fatalf("MigratePairToPool: %v", err)
		}
		if got.HQ.Change != 8 {
			t.Errorf("the finished move lost HQ's write: change %d", got.HQ.Change)
		}
		if files := metaFiles(t, dir); !reflect.DeepEqual(files, []string{"apppool.json"}) {
			t.Errorf("files = %v, want only apppool.json", files)
		}
	})
	t.Run("a finished move is a no-op", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := WriteServiceMeta(dir, devKeyedPair()); err != nil {
			t.Fatal(err)
		}
		first, err := MigratePairToPool(dir, appPoolMove())
		if err != nil {
			t.Fatal(err)
		}
		again, err := MigratePairToPool(dir, appPoolMove())
		if err != nil {
			t.Fatalf("second MigratePairToPool: %v", err)
		}
		if !reflect.DeepEqual(first, again) {
			t.Errorf("a repeated move changed the pair:\nfirst %+v\nagain %+v", first, again)
		}
	})
}

// TestRevertPoolPair_InverseOfWhatTheFileHoldsNow pins the abort: the file is
// renamed back and the inverse applied to what it holds now, so a write since
// the move is kept and the dev's own facts come back from the marker — never
// a restore of the whole pair from a snapshot.
func TestRevertPoolPair_InverseOfWhatTheFileHoldsNow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteServiceMeta(dir, devKeyedPair()); err != nil {
		t.Fatal(err)
	}
	if _, err := MigratePairToPool(dir, appPoolMove()); err != nil {
		t.Fatal(err)
	}
	// HQ moved on while the pair was keyed by the pool.
	if err := UpdateServiceMeta(dir, "apppool", func(m *ServiceMeta) error {
		m.HQ.Change, m.HQ.Branch = 9, "feat-y"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	got, err := RevertPoolPair(dir, appPoolMove())
	if err != nil {
		t.Fatalf("RevertPoolPair: %v", err)
	}
	want := devKeyedPair()
	want.HQ.Change, want.HQ.Branch = 9, "feat-y"
	onDisk, err := ReadServiceMeta(dir, "appdev")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(onDisk, want) || !reflect.DeepEqual(got, want) {
		t.Errorf("reverted pair:\n got %+v\nwant %+v", onDisk, want)
	}
	if files := metaFiles(t, dir); !reflect.DeepEqual(files, []string{"appdev.json"}) {
		t.Errorf("files = %v, want only appdev.json", files)
	}

	again, err := RevertPoolPair(dir, appPoolMove())
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Errorf("a repeated revert = %+v, %v; want the dev-keyed pair unchanged", again, err)
	}
}

func TestRevertPoolPair_HalfDoneMove_TakesThePoolKeyedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteServiceMeta(dir, devKeyedPair()); err != nil {
		t.Fatal(err)
	}
	moved, err := devKeyedPair().keyedOnPool(appPoolMove())
	if err != nil {
		t.Fatal(err)
	}
	moved.HQ.Change = 8
	if err := WriteServiceMeta(dir, moved); err != nil {
		t.Fatal(err)
	}
	got, err := RevertPoolPair(dir, appPoolMove())
	if err != nil {
		t.Fatalf("RevertPoolPair: %v", err)
	}
	if got.Hostname != "appdev" || got.HQ.Change != 8 || got.DevIsPool() {
		t.Errorf("reverted %+v; want the dev-keyed pair carrying the pool file's write", got)
	}
	if files := metaFiles(t, dir); !reflect.DeepEqual(files, []string{"appdev.json"}) {
		t.Errorf("files = %v, want only appdev.json", files)
	}
}

func TestRevertPoolPair_NothingToRevertTo_Refused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		meta *ServiceMeta
		want string
	}{
		{"no pair at all", nil, "not found"},
		{"a part provisioned as a pool", &ServiceMeta{Hostname: "apppool", Mode: topology.PlanModeDev, BootstrappedAt: "t", DevPool: &DevPoolRef{ServiceID: "p"}}, "never keyed by"},
		{"a pool pair moved from another dev", &ServiceMeta{Hostname: "apppool", Mode: topology.PlanModeDev, BootstrappedAt: "t", DevPool: &DevPoolRef{MigratedFrom: &MigratedDev{Hostname: "webdev"}}}, "webdev"},
		{"a pair keyed by the pool hostname that is no pool pair", &ServiceMeta{Hostname: "apppool", Mode: topology.PlanModeDev, BootstrappedAt: "t"}, "not a pool"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if tc.meta != nil {
				if err := WriteServiceMeta(dir, tc.meta); err != nil {
					t.Fatal(err)
				}
			}
			before := metaFiles(t, dir)
			_, err := RevertPoolPair(dir, appPoolMove())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a refusal naming %q", err, tc.want)
			}
			if after := metaFiles(t, dir); !reflect.DeepEqual(after, before) {
				t.Errorf("a refused revert changed the metas:\nbefore %v\n after %v", before, after)
			}
		})
	}
}

// TestModeFor_PoolDevHalf_IsModeless pins that a mode projection of the pool
// half reads like a service with no meta — no dev-runtime mode for a pool —
// while its stage keeps the stage mode.
func TestModeFor_PoolDevHalf_IsModeless(t *testing.T) {
	t.Parallel()
	m, err := devKeyedPair().keyedOnPool(appPoolMove())
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ModeFor("apppool"); got != "" {
		t.Errorf("ModeFor(apppool) = %q, want \"\"", got)
	}
	if got := m.ModeFor("appstage"); got != topology.ModeStage {
		t.Errorf("ModeFor(appstage) = %q, want stage", got)
	}
	if got := devKeyedPair().ModeFor("appdev"); got != topology.ModeStandard {
		t.Errorf("a dev-keyed pair's ModeFor(appdev) = %q, want standard", got)
	}
}

// metaFiles lists the services directory's files, sorted.
func metaFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, "services"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
