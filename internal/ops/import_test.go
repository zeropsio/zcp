// Tests for: plans/analysis/ops.md § import
package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// importMock returns a mock with a standard successful import result.
func importMock() *platform.Mock {
	return platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{
					ID:   "svc-1",
					Name: "api",
					Processes: []platform.Process{
						{
							ID:         "proc-1",
							ActionName: "serviceStackImport",
							Status:     "PENDING",
						},
					},
				},
			},
		})
}

func TestImport_Success(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	mock := importMock()

	result, err := Import(context.Background(), mock, "proj-1", content, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProjectID != "proj-1" {
		t.Errorf("expected projectId=proj-1, got %s", result.ProjectID)
	}
	if result.ProjectName != "myproject" {
		t.Errorf("expected projectName=myproject, got %s", result.ProjectName)
	}
	if len(result.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(result.Processes))
	}
	p := result.Processes[0]
	if p.ProcessID != "proc-1" {
		t.Errorf("expected processId=proc-1, got %s", p.ProcessID)
	}
	if p.Service != "api" {
		t.Errorf("expected service=api, got %s", p.Service)
	}
	if p.ServiceID != "svc-1" {
		t.Errorf("expected serviceId=svc-1, got %s", p.ServiceID)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings, got %v", result.Warnings)
	}
}

func TestImport_NoInput(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	_, err := Import(context.Background(), mock, "proj-1", "", "", nil)
	if err == nil {
		t.Fatal("expected error when neither content nor filePath provided")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidUsage {
		t.Errorf("expected code %s, got %s", platform.ErrInvalidUsage, pe.Code)
	}
}

func TestImport_BothInputs(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	_, err := Import(context.Background(), mock, "proj-1", "content", "/some/path", nil)
	if err == nil {
		t.Fatal("expected error when both content and filePath provided")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidUsage {
		t.Errorf("expected code %s, got %s", platform.ErrInvalidUsage, pe.Code)
	}
}

func TestImport_FileNotFound(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	_, err := Import(context.Background(), mock, "proj-1", "", "/nonexistent/file.yml", nil)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrFileNotFound {
		t.Errorf("expected code %s, got %s", platform.ErrFileNotFound, pe.Code)
	}
}

func TestImport_FileRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "import.yml")
	content := `services:
  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
`
	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{
					ID:   "svc-cache",
					Name: "cache",
					Processes: []platform.Process{
						{ID: "proc-cache", ActionName: "serviceStackImport", Status: "PENDING"},
					},
				},
			},
		})

	result, err := Import(context.Background(), mock, "proj-1", "", fp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(result.Processes))
	}
}

// --- Version Validation Tests ---

func TestImport_VersionWarnings(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: ruby@3.2
`
	types := []platform.ServiceStackType{
		{
			Name:     "Node.js",
			Category: "USER",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "nodejs@22", Status: statusActive},
			},
		},
	}
	mock := importMock()
	result, err := Import(context.Background(), mock, "proj-1", content, "", types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected version warnings for ruby@3.2")
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "ruby@3.2") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning mentioning ruby@3.2, got: %v", result.Warnings)
	}
}

func TestImport_ModeWarnings(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: db
    type: postgresql@16
`
	types := []platform.ServiceStackType{
		{
			Name:     "PostgreSQL",
			Category: "STANDARD",
			Versions: []platform.ServiceStackTypeVersion{
				{Name: "postgresql@16", Status: statusActive},
			},
		},
	}
	mock := importMock()
	result, err := Import(context.Background(), mock, "proj-1", content, "", types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected mode warning for postgresql without mode")
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "mode") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about missing mode, got: %v", result.Warnings)
	}
}

func TestImport_NilTypes_NoWarnings(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: ruby@3.2
`
	mock := importMock()
	result, err := Import(context.Background(), mock, "proj-1", content, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings with nil types, got: %v", result.Warnings)
	}
}

func TestImport_APIError(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	mock := platform.NewMock().
		WithError("ImportServices", &platform.PlatformError{
			Code:    platform.ErrAPIError,
			Message: "import failed",
		})

	_, err := Import(context.Background(), mock, "proj-1", content, "", nil)
	if err == nil {
		t.Fatal("expected error from API")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrAPIError {
		t.Errorf("expected code %s, got %s", platform.ErrAPIError, pe.Code)
	}
}
