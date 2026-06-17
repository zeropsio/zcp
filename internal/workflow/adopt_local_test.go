// Tests for: adopt_local.go — LocalAutoAdopt, FormatLocalStateNote.
// Package workflow uses global state (os.Getpid, file-based sessions)
// so most tests here do NOT use t.Parallel(); pure-string formatter
// tests can — they don't touch state.
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
	// Phase 9 parity: zero-runtime adoption no longer pre-picks a close-mode.
	// Container bootstrap leaves CloseDeployMode=unset until strategy-review
	// fires; local should match — pre-confirming `manual` hid the `auto` option
	// from the post-deploy review path. (Delivery via git push is a separate
	// dimension, not a close-mode value — F1.)
	if result.Meta.CloseDeployMode != topology.CloseModeUnset {
		t.Errorf("CloseDeployMode = %q, want unset (let strategy-review prompt)", result.Meta.CloseDeployMode)
	}
	if result.Meta.CloseDeployModeConfirmed {
		t.Errorf("CloseDeployModeConfirmed = true, want false (unset is unconfirmed by definition)")
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
	// Phase 9 parity regression: multi-runtime adoption already left
	// CloseDeployMode=unset; pin it explicitly so a future "fix" doesn't
	// re-introduce the asymmetry between zero- and multi-runtime branches.
	if result.Meta.CloseDeployMode != topology.CloseModeUnset {
		t.Errorf("CloseDeployMode = %q, want unset (review prompts post-link)", result.Meta.CloseDeployMode)
	}
	if result.Meta.CloseDeployModeConfirmed {
		t.Errorf("CloseDeployModeConfirmed = true, want false")
	}
}

func TestFormatLocalStateNote_Shapes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		metas       []*ServiceMeta
		services    []platform.ServiceStack
		projectName string
		want        []string // substrings that must appear
		absent      []string // substrings that must NOT appear
	}{
		{
			name:  "nil metas → empty note",
			metas: nil,
			want:  []string{},
		},
		{
			name:  "container-only metas → empty note",
			metas: []*ServiceMeta{{Hostname: "appdev", Mode: topology.PlanModeStandard}},
			want:  []string{},
		},
		{
			name: "local-stage linked",
			metas: []*ServiceMeta{
				{Hostname: "myproject", StageHostname: "apistage", Mode: topology.PlanModeLocalStage},
			},
			services: []platform.ServiceStack{
				{Name: "apistage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
				{Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
				{Name: "cache", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "valkey@7.2"}},
			},
			projectName: "myproject",
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
			name: "local-only with multiple runtimes leads with adopt-local",
			metas: []*ServiceMeta{
				{Hostname: "myproject", Mode: topology.PlanModeLocalOnly},
			},
			services: []platform.ServiceStack{
				{Name: "api", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
				{Name: "web", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
				{Name: "worker", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			},
			projectName: "myproject",
			want: []string{
				`"myproject"`,
				"local-only",
				"BEFORE running develop",
				"adopt-local",
				"api, web, worker",
				"`auto`",
			},
		},
		{
			name: "local-only with no runtimes",
			metas: []*ServiceMeta{
				{Hostname: "myproject", Mode: topology.PlanModeLocalOnly},
			},
			services: []platform.ServiceStack{
				{Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}},
			},
			projectName: "myproject",
			want: []string{
				`"myproject"`,
				"local-only",
				"No Zerops runtime",
				"manual",
				// F1: git push is presented as the delivery dimension
				// (git-push-setup), NOT as a close-mode value.
				"git-push-setup",
				"db",
			},
			absent: []string{"BEFORE running develop"},
		},
		{
			name: "projectName fallback uses meta hostname when empty",
			metas: []*ServiceMeta{
				{Hostname: "fallback-name", Mode: topology.PlanModeLocalOnly},
			},
			services:    nil,
			projectName: "",
			want:        []string{`"fallback-name"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FormatLocalStateNote(tt.metas, tt.services, tt.projectName)
			if len(tt.want) == 0 && len(tt.absent) == 0 {
				if got != "" {
					t.Errorf("expected empty note; got %q", got)
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

// TestFormatLocalStateNote_CloseModeAutoManualNotGitPush pins F1 on the
// local-adoption note (an agent-facing TELL wired into the server adoption
// message): close-mode options present manual/auto only — git push appears as
// the delivery dimension (action="git-push-setup"), never as a close-mode VALUE
// choice. Pre-F1 both branches led the close-mode menu with `git-push`.
func TestFormatLocalStateNote_CloseModeAutoManualNotGitPush(t *testing.T) {
	t.Parallel()
	cases := [][]platform.ServiceStack{
		// no-runtime branch
		{{Name: "db", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@16"}}},
		// has-runtime branch
		{{Name: "api", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}}},
	}
	for _, services := range cases {
		note := FormatLocalStateNote(
			[]*ServiceMeta{{Hostname: "myproject", Mode: topology.PlanModeLocalOnly}},
			services, "myproject",
		)
		// git push must be the delivery dimension, not a close-mode option menu.
		if !strings.Contains(note, "git-push-setup") {
			t.Errorf("note should point git push delivery at git-push-setup; got:\n%s", note)
		}
		if !strings.Contains(note, "manual") {
			t.Errorf("note should present manual close-mode; got:\n%s", note)
		}
		// The retired close-mode-menu framing (git-push as a push-to-remote
		// CHOICE) must be gone.
		if strings.Contains(note, "push to an external remote") {
			t.Errorf("note still frames git-push as a close-mode choice; got:\n%s", note)
		}
	}
}

// TestRunLocalAutoAdopt_SecondCall_StillSurfacesNote pins that calling
// the auto-adopt path against an already-adopted state dir still emits
// a current-state note. Pre-Phase-10 the second call returned "" (the
// guard `if len(existing) > 0 { return "" }` short-circuited every
// subsequent server start) — agents joining an existing local-only
// project never saw the actionable adopt-local hint.
//
// Driven through workflow.LocalAutoAdopt + ListServiceMetas +
// FormatLocalStateNote (the same pipeline runLocalAutoAdopt invokes)
// so the test stays in the workflow package without needing the server
// layer.
func TestRunLocalAutoAdopt_SecondCall_StillSurfacesNote(t *testing.T) {
	dir := t.TempDir()
	mock := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "myproject"}).
		WithServices([]platform.ServiceStack{
			{Name: "api", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
			{Name: "web", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		})

	// First call: adoption fires.
	if _, err := LocalAutoAdopt(context.Background(), mock, "p1", dir); err != nil {
		t.Fatalf("first LocalAutoAdopt: %v", err)
	}
	metas, err := ListServiceMetas(dir)
	if err != nil {
		t.Fatalf("list metas: %v", err)
	}
	noteFirst := FormatLocalStateNote(metas, []platform.ServiceStack{
		{Name: "api", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "web", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}, "myproject")
	if noteFirst == "" {
		t.Fatal("first-call note must not be empty")
	}

	// Second call: state dir already populated — runLocalAutoAdopt would
	// re-list metas and re-format. The note must still emit.
	metasAgain, err := ListServiceMetas(dir)
	if err != nil {
		t.Fatalf("re-list metas: %v", err)
	}
	noteSecond := FormatLocalStateNote(metasAgain, []platform.ServiceStack{
		{Name: "api", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "web", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}, "myproject")
	if noteSecond == "" {
		t.Fatal("second-call note must still emit (Phase-10 fix); got empty")
	}
	if !strings.Contains(noteSecond, "adopt-local") {
		t.Errorf("second-call note missing actionable adopt-local hint; got:\n%s", noteSecond)
	}
}

// TestFormatLocalStateNote_LocalOnlyMultiRuntime_LeadsWithAdoptLocal pins
// the actionable-recovery-up-front shape. Pre-Phase-10 the adopt-local
// hint sat at the END of the second sentence — agents skim-reading the
// note often missed it and tried `develop` against an unlinked project.
func TestFormatLocalStateNote_LocalOnlyMultiRuntime_LeadsWithAdoptLocal(t *testing.T) {
	t.Parallel()
	metas := []*ServiceMeta{{Hostname: "myproject", Mode: topology.PlanModeLocalOnly}}
	services := []platform.ServiceStack{
		{Name: "api", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		{Name: "web", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
	}
	got := FormatLocalStateNote(metas, services, "myproject")
	beforeIdx := strings.Index(got, "BEFORE running develop")
	closeModeIdx := strings.Index(got, "Close-mode options")
	if beforeIdx < 0 {
		t.Fatalf("note missing 'BEFORE running develop' lead-in; got:\n%s", got)
	}
	if closeModeIdx < 0 {
		t.Fatalf("note missing 'Close-mode options'; got:\n%s", got)
	}
	if beforeIdx >= closeModeIdx {
		t.Errorf("'BEFORE running develop' should appear before 'Close-mode options'; got:\n%s", got)
	}
}
