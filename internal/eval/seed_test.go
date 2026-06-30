package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestSeedEmpty_DeletesNonSystemServices(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER"}},
		}).
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", Status: "FINISHED"}).
		WithDeleteRemovesService(true)
	tmp := t.TempDir()

	if err := SeedEmpty(context.Background(), mock, "proj-1", tmp); err != nil {
		t.Fatalf("SeedEmpty: %v", err)
	}

	if mock.CallCounts["DeleteService"] != 1 {
		t.Errorf("DeleteService calls: got %d, want 1", mock.CallCounts["DeleteService"])
	}
}

func TestSeedImported_CallsImportServices(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithImportResult(&platform.ImportResult{
		ProjectID: "proj-1",
		ServiceStacks: []platform.ImportedServiceStack{
			{ID: "svc-new", Name: "app"},
		},
	})

	fixture := writeTempFixture(t, "services:\n  - hostname: app\n    type: nodejs@22\n")
	tmp := t.TempDir()

	if err := SeedImported(context.Background(), mock, "proj-1", fixture, tmp, "abc123"); err != nil {
		t.Fatalf("SeedImported: %v", err)
	}

	if mock.CapturedImportYAML == "" {
		t.Fatal("expected ImportServices to be called with yaml content")
	}
	if !strings.Contains(mock.CapturedImportYAML, "hostname: app") {
		t.Errorf("import yaml missing fixture content: %q", mock.CapturedImportYAML)
	}
}

func TestSeedImported_MissingFixture_Errors(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	tmp := t.TempDir()

	err := SeedImported(context.Background(), mock, "proj-1", "/nonexistent/fixture.yaml", tmp, "abc")
	if err == nil {
		t.Fatal("expected error for missing fixture")
	}
	if !strings.Contains(err.Error(), "fixture") {
		t.Errorf("error should mention fixture, got: %v", err)
	}
}

// TestSeedSettled_TolerantOfFailed pins the recovery-seed contract: an import
// process ending FAILED is NOT a seed error, and a service stuck in FAILED is
// reported as settled (not transitional). Mirror SeedImported_CallsImportServices
// with a tweaked process state machine.
func TestSeedSettled_TolerantOfFailed(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID: "proj-1",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-failed", Name: "api", Processes: []platform.Process{{ID: "proc-import-1"}}},
			},
		}).
		WithProcess(&platform.Process{ID: "proc-import-1", Status: "FAILED"})
	// SeedSettled calls SeedEmpty first; mock starts empty so no delete dance is
	// needed. After ImportServices the test arranges ListServices to report the
	// api stack in FAILED so waitAllSettled returns immediately.
	mock = mock.WithServices([]platform.ServiceStack{
		{ID: "svc-failed", Name: "api", Status: "FAILED",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER"}},
	})
	// Note: the mock's ListServices returns this list both before AND after
	// the cleanup step. Cleanup tries to delete each non-system service; the
	// helper below registers the delete-process FINISHED so cleanup stops
	// looping. WithDeleteRemovesService removes the service from the mock's
	// list when the delete completes — leaving an empty list that
	// post-import switches back to the FAILED snapshot below.
	mock = mock.WithDeleteRemovesService(true)
	mock = mock.WithProcess(&platform.Process{ID: "proc-delete-svc-failed", Status: "FINISHED"})

	fixture := writeTempFixture(t, "services:\n  - hostname: api\n    type: python@3.12\n")
	tmp := t.TempDir()

	if err := SeedSettled(context.Background(), mock, "proj-1", fixture, tmp, "abc"); err != nil {
		t.Fatalf("SeedSettled: %v (FAILED process should be tolerated)", err)
	}
}

// TestSeedSettled_TolerantOfCanceled pins the recovery-seed contract for
// import-orchestration parents. Empirically Zerops marks the parent process
// CANCELED (not FAILED) when a child deploy fails — both are terminal and both
// are valid outcomes for a recovery scenario.
func TestSeedSettled_TolerantOfCanceled(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID: "proj-1",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-x", Name: "api", Processes: []platform.Process{{ID: "proc-canceled"}}},
			},
		}).
		WithProcess(&platform.Process{ID: "proc-canceled", Status: "CANCELED"}).
		WithDeleteRemovesService(true).
		WithProcess(&platform.Process{ID: "proc-delete-svc-x", Status: "FINISHED"})
	mock = mock.WithServices([]platform.ServiceStack{
		{ID: "svc-x", Name: "api", Status: "FAILED",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER"}},
	})

	fixture := writeTempFixture(t, "services:\n  - hostname: api\n    type: python@3.12\n")
	tmp := t.TempDir()

	if err := SeedSettled(context.Background(), mock, "proj-1", fixture, tmp, "abc"); err != nil {
		t.Fatalf("SeedSettled: %v (CANCELED process should be tolerated)", err)
	}
}

func TestIsTerminalServiceStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status string
		want   bool
	}{
		{"ACTIVE", true},
		{"FAILED", true},
		{"READY_TO_DEPLOY", true},
		{"RUNNING", true},
		{"STOPPED", true},
		{"NEW", false},
		{"BUILDING", false},
		{"DEPLOYING", false},
		{"CREATING", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.status, func(t *testing.T) {
			t.Parallel()
			if got := isTerminalServiceStatus(c.status); got != c.want {
				t.Errorf("isTerminalServiceStatus(%q) = %v, want %v", c.status, got, c.want)
			}
		})
	}
}

func TestSeedImported_InterpolatesSuiteID(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithImportResult(&platform.ImportResult{ProjectID: "proj-1"})

	fixture := writeTempFixture(t, "services:\n  - hostname: app-${suiteId}\n    type: nodejs@22\n")
	tmp := t.TempDir()

	if err := SeedImported(context.Background(), mock, "proj-1", fixture, tmp, "abc123"); err != nil {
		t.Fatalf("SeedImported: %v", err)
	}

	if !strings.Contains(mock.CapturedImportYAML, "app-abc123") {
		t.Errorf("suite id not interpolated, got: %q", mock.CapturedImportYAML)
	}
	if strings.Contains(mock.CapturedImportYAML, "${suiteId}") {
		t.Errorf("placeholder still present: %q", mock.CapturedImportYAML)
	}
}

// TestSeedBuilding_ReturnsWhileBuildRunning pins that ModeBuilding returns as
// soon as the stack.build process is RUNNING — it does NOT poll the build to
// FINISHED (which would defeat the race). The mock keeps the build process
// permanently RUNNING; if SeedBuilding waited for completion this test would
// hang and fail the package timeout.
func TestSeedBuilding_ReturnsWhileBuildRunning(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID: "proj-1",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-app", Name: "appdev", Processes: []platform.Process{
					{ID: "create-proc", ActionName: "stack.create"},
					{ID: "build-proc", ActionName: "stack.build"},
				}},
			},
		}).
		WithProcess(&platform.Process{ID: "build-proc", Status: "RUNNING", ActionName: "stack.build"})

	fixture := writeTempFixture(t, "services:\n  - hostname: appdev\n    type: nodejs@22\n    buildFromGit: https://example/r\n    zeropsSetup: helloworld\n")
	tmp := t.TempDir()

	if err := SeedBuilding(context.Background(), mock, "proj-1", fixture, tmp, "s1"); err != nil {
		t.Fatalf("SeedBuilding: %v", err)
	}
	if !strings.Contains(mock.CapturedImportYAML, "buildFromGit") {
		t.Errorf("import yaml missing buildFromGit: %q", mock.CapturedImportYAML)
	}
}

// TestSeedBuilding_NoBuildProcess_Errors pins that ModeBuilding rejects a
// non-buildFromGit fixture (no stack.build process to be mid-flight).
func TestSeedBuilding_NoBuildProcess_Errors(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithImportResult(&platform.ImportResult{
		ProjectID: "proj-1",
		ServiceStacks: []platform.ImportedServiceStack{
			{ID: "svc-db", Name: "db", Processes: []platform.Process{{ID: "create-proc", ActionName: "stack.create"}}},
		},
	})

	fixture := writeTempFixture(t, "services:\n  - hostname: db\n    type: postgresql@16\n")
	tmp := t.TempDir()

	err := SeedBuilding(context.Background(), mock, "proj-1", fixture, tmp, "s1")
	if err == nil {
		t.Fatal("expected error: ModeBuilding requires a buildFromGit fixture")
	}
	if !strings.Contains(err.Error(), "buildFromGit") {
		t.Errorf("error should mention buildFromGit, got: %v", err)
	}
}

func writeTempFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
