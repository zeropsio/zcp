// Tests for: adopt_local.go — LocalAutoAdopt, MigrateLegacyLocalMetas,
// FormatAdoptionNote. Package workflow uses global state (os.Getpid,
// file-based sessions) so these do NOT use t.Parallel().
package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestLocalAutoAdopt_ExistingState_NoOp(t *testing.T) {
	dir := t.TempDir()
	// Seed an existing meta so adoption should skip.
	if err := WriteServiceMeta(dir, &ServiceMeta{
		Hostname: "myproject", Mode: PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "myproject"})

	result, err := LocalAutoAdopt(context.Background(), mock, "p1", dir)
	if err != nil {
		t.Fatalf("LocalAutoAdopt: %v", err)
	}
	if result != nil {
		t.Errorf("existing state should produce nil result; got %+v", result)
	}
}

func TestLocalAutoAdopt_NoRuntimes_LocalOnly(t *testing.T) {
	dir := t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "myproject"}).
		WithServices([]platform.ServiceStack{
			{
				ID: "db-1", Name: "db", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "postgresql@16",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})

	result, err := LocalAutoAdopt(context.Background(), mock, "p1", dir)
	if err != nil {
		t.Fatalf("LocalAutoAdopt: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result; adoption must succeed")
	}
	if result.Meta.Mode != PlanModeLocalOnly {
		t.Errorf("Mode = %q, want %q", result.Meta.Mode, PlanModeLocalOnly)
	}
	if result.Meta.Hostname != "myproject" {
		t.Errorf("Hostname = %q, want project name", result.Meta.Hostname)
	}
	if result.Meta.StageHostname != "" {
		t.Errorf("StageHostname = %q, want empty (no runtime)", result.Meta.StageHostname)
	}
	if result.Meta.DeployStrategy != StrategyManual {
		t.Errorf("DeployStrategy = %q, want manual (local-only default)", result.Meta.DeployStrategy)
	}
	if result.StageAutoLinked {
		t.Error("StageAutoLinked = true, want false (no runtime)")
	}
	if len(result.Managed) != 1 || result.Managed[0] != "db" {
		t.Errorf("Managed = %v, want [db]", result.Managed)
	}
}

func TestLocalAutoAdopt_OneRuntime_LocalStageLinked(t *testing.T) {
	dir := t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "myproject"}).
		WithServices([]platform.ServiceStack{
			{
				ID: "rt-1", Name: "apistage", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})

	result, err := LocalAutoAdopt(context.Background(), mock, "p1", dir)
	if err != nil {
		t.Fatalf("LocalAutoAdopt: %v", err)
	}
	if result.Meta.Mode != PlanModeLocalStage {
		t.Errorf("Mode = %q, want local-stage", result.Meta.Mode)
	}
	if result.Meta.StageHostname != "apistage" {
		t.Errorf("StageHostname = %q, want apistage", result.Meta.StageHostname)
	}
	if !result.StageAutoLinked {
		t.Error("StageAutoLinked = false, want true")
	}
	// Runtime was ACTIVE at adoption — FirstDeployedAt should be stamped so
	// develop doesn't re-enter the first-deploy branch on an already-deployed
	// service.
	if result.Meta.FirstDeployedAt == "" {
		t.Error("FirstDeployedAt empty — must be stamped for adopted+ACTIVE runtime")
	}
}

func TestLocalAutoAdopt_OneRuntime_ReadyToDeploy_NoStamp(t *testing.T) {
	dir := t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "myproject"}).
		WithServices([]platform.ServiceStack{
			{
				ID: "rt-1", Name: "apistage", Status: "READY_TO_DEPLOY",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})

	result, err := LocalAutoAdopt(context.Background(), mock, "p1", dir)
	if err != nil {
		t.Fatalf("LocalAutoAdopt: %v", err)
	}
	if result.Meta.FirstDeployedAt != "" {
		t.Errorf("FirstDeployedAt stamped for non-ACTIVE runtime; got %q", result.Meta.FirstDeployedAt)
	}
}

func TestLocalAutoAdopt_MultipleRuntimes_LocalOnlyWithEnumeration(t *testing.T) {
	dir := t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "myproject"}).
		WithServices([]platform.ServiceStack{
			{
				ID: "rt-1", Name: "api", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
			{
				ID: "rt-2", Name: "web", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
			{
				ID: "rt-3", Name: "worker", Status: "ACTIVE",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{
					ServiceStackTypeVersionName:  "nodejs@22",
					ServiceStackTypeCategoryName: "USER",
				},
			},
		})

	result, err := LocalAutoAdopt(context.Background(), mock, "p1", dir)
	if err != nil {
		t.Fatalf("LocalAutoAdopt: %v", err)
	}
	// Ambiguous runtimes never block adoption of the local dir itself.
	if result.Meta.Mode != PlanModeLocalOnly {
		t.Errorf("Mode = %q, want local-only (ambiguous → no auto-link)", result.Meta.Mode)
	}
	if result.Meta.StageHostname != "" {
		t.Errorf("StageHostname = %q, want empty (refused to guess)", result.Meta.StageHostname)
	}
	if result.StageAutoLinked {
		t.Error("StageAutoLinked = true, want false")
	}
	if len(result.UnlinkedRuntimes) != 3 {
		t.Errorf("UnlinkedRuntimes = %v, want 3 hostnames", result.UnlinkedRuntimes)
	}
	// Order doesn't matter but all three must be present.
	want := map[string]bool{"api": true, "web": true, "worker": true}
	for _, h := range result.UnlinkedRuntimes {
		delete(want, h)
	}
	if len(want) > 0 {
		t.Errorf("missing runtimes in enumeration: %v", want)
	}
}

func TestFormatAdoptionNote_Shapes(t *testing.T) {
	tests := []struct {
		name   string
		result *AdoptionResult
		want   []string // substrings that must appear
		absent []string // substrings that must NOT appear
	}{
		{
			name:   "nil result → empty note",
			result: nil,
			want:   []string{},
		},
		{
			name: "stage auto-linked",
			result: &AdoptionResult{
				Meta: &ServiceMeta{
					Hostname: "myproject", StageHostname: "apistage", Mode: PlanModeLocalStage,
				},
				StageAutoLinked: true,
				Managed:         []string{"db", "cache"},
			},
			want: []string{
				`"myproject"`,
				"local-stage",
				"apistage",
				"db, cache",
				"zcli vpn up",
			},
			absent: []string{"adopt-local"},
		},
		{
			name: "multiple runtimes → user picks",
			result: &AdoptionResult{
				Meta:             &ServiceMeta{Hostname: "myproject", Mode: PlanModeLocalOnly},
				UnlinkedRuntimes: []string{"api", "web", "worker"},
			},
			want: []string{
				`"myproject"`,
				"local-only",
				"api, web, worker",
				"adopt-local",
				"push-dev",
			},
		},
		{
			name: "no runtime",
			result: &AdoptionResult{
				Meta:    &ServiceMeta{Hostname: "myproject", Mode: PlanModeLocalOnly},
				Managed: []string{"db"},
			},
			want: []string{
				`"myproject"`,
				"local-only",
				"No Zerops runtime",
				"push-git",
				"manual",
				"db",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAdoptionNote(tt.result)
			if tt.result == nil {
				if got != "" {
					t.Errorf("nil result must produce empty note; got %q", got)
				}
				return
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("note missing %q; got:\n%s", want, got)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("note must not contain %q; got:\n%s", absent, got)
				}
			}
		})
	}
}

// TestMigrateLegacyLocalMetas rewrites pre-A.4 shape to the new layout.
func TestMigrateLegacyLocalMetas_RewritesShape(t *testing.T) {
	dir := t.TempDir()
	// Legacy shape: Hostname=<stage-host>, Environment=local, Mode=standard.
	legacy := &ServiceMeta{
		Hostname:          "appstage",
		Mode:              PlanModeStandard,
		Environment:       string(EnvLocal),
		BootstrappedAt:    "2026-01-01",
		DeployStrategy:    StrategyPushDev,
		StrategyConfirmed: true,
	}
	if err := WriteServiceMeta(dir, legacy); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "myproject"})

	metas, _ := ListServiceMetas(dir)
	if err := MigrateLegacyLocalMetas(context.Background(), mock, "p1", dir, metas); err != nil {
		t.Fatalf("MigrateLegacyLocalMetas: %v", err)
	}

	// New-shape file exists, keyed by project name.
	got, err := ReadServiceMeta(dir, "myproject")
	if err != nil || got == nil {
		t.Fatalf("new-shape meta missing: got=%+v err=%v", got, err)
	}
	if got.Mode != PlanModeLocalStage {
		t.Errorf("Mode = %q, want local-stage", got.Mode)
	}
	if got.Hostname != "myproject" {
		t.Errorf("Hostname = %q, want project name", got.Hostname)
	}
	if got.StageHostname != "appstage" {
		t.Errorf("StageHostname = %q, want appstage", got.StageHostname)
	}
	// User-authored fields preserved.
	if got.DeployStrategy != StrategyPushDev {
		t.Errorf("DeployStrategy lost: got %q", got.DeployStrategy)
	}
	if !got.StrategyConfirmed {
		t.Error("StrategyConfirmed lost")
	}

	// Old-shape file deleted.
	old, _ := ReadServiceMeta(dir, "appstage")
	if old != nil {
		t.Errorf("old stage-keyed meta should be deleted; still present: %+v", old)
	}
}

// Idempotent: second run on already-migrated state is a no-op.
func TestMigrateLegacyLocalMetas_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := WriteServiceMeta(dir, &ServiceMeta{
		Hostname: "myproject", Mode: PlanModeLocalStage, StageHostname: "appstage",
		Environment: string(EnvLocal), BootstrappedAt: "2026-01-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "myproject"})
	metas, _ := ListServiceMetas(dir)
	if err := MigrateLegacyLocalMetas(context.Background(), mock, "p1", dir, metas); err != nil {
		t.Fatalf("MigrateLegacyLocalMetas (second run): %v", err)
	}
	got, _ := ReadServiceMeta(dir, "myproject")
	if got == nil || got.Mode != PlanModeLocalStage || got.StageHostname != "appstage" {
		t.Errorf("migration clobbered already-migrated meta: %+v", got)
	}
}

// Container metas are not touched by the migration.
func TestMigrateLegacyLocalMetas_IgnoresContainerMetas(t *testing.T) {
	dir := t.TempDir()
	if err := WriteServiceMeta(dir, &ServiceMeta{
		Hostname: "appdev", Mode: PlanModeStandard, StageHostname: "appstage",
		Environment: string(EnvContainer), BootstrappedAt: "2026-01-01",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	mock := platform.NewMock().WithProject(&platform.Project{ID: "p1", Name: "myproject"})
	metas, _ := ListServiceMetas(dir)
	if err := MigrateLegacyLocalMetas(context.Background(), mock, "p1", dir, metas); err != nil {
		t.Fatalf("MigrateLegacyLocalMetas: %v", err)
	}
	got, _ := ReadServiceMeta(dir, "appdev")
	if got == nil || got.Mode != PlanModeStandard {
		t.Errorf("container meta rewritten: %+v", got)
	}
}
