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
		WithProcess(&platform.Process{ID: "proc-delete-svc-1", Status: "FINISHED"})
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

func writeTempFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
