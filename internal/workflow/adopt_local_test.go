// Tests for: adopt_local.go — LocalAutoAdopt, FormatAdoptionNote.
// Package workflow uses global state (os.Getpid, file-based sessions)
// so these do NOT use t.Parallel().
package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

func TestLocalAutoAdopt_ExistingState_NoOp(t *testing.T) {
	dir := t.TempDir()
	// Seed an existing meta so adoption should skip.
	if err := WriteServiceMeta(dir, &ServiceMeta{
		Hostname: "myproject", Mode: topology.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
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
	if result.Meta.Mode != topology.PlanModeLocalOnly {
		t.Errorf("Mode = %q, want %q", result.Meta.Mode, topology.PlanModeLocalOnly)
	}
	if result.Meta.Hostname != "myproject" {
		t.Errorf("Hostname = %q, want project name", result.Meta.Hostname)
	}
	if result.Meta.StageHostname != "" {
		t.Errorf("StageHostname = %q, want empty (no runtime)", result.Meta.StageHostname)
	}
	if result.Meta.CloseDeployMode != topology.CloseModeManual {
		t.Errorf("CloseDeployMode = %q, want manual (local-only default)", result.Meta.CloseDeployMode)
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
	if result.Meta.Mode != topology.PlanModeLocalStage {
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
	if result.Meta.Mode != topology.PlanModeLocalOnly {
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
					Hostname: "myproject", StageHostname: "apistage", Mode: topology.PlanModeLocalStage,
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
				Meta:             &ServiceMeta{Hostname: "myproject", Mode: topology.PlanModeLocalOnly},
				UnlinkedRuntimes: []string{"api", "web", "worker"},
			},
			want: []string{
				`"myproject"`,
				"local-only",
				"api, web, worker",
				"adopt-local",
				"`auto`",
			},
		},
		{
			name: "no runtime",
			result: &AdoptionResult{
				Meta:    &ServiceMeta{Hostname: "myproject", Mode: topology.PlanModeLocalOnly},
				Managed: []string{"db"},
			},
			want: []string{
				`"myproject"`,
				"local-only",
				"No Zerops runtime",
				"git-push",
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
