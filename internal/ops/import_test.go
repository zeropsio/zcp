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

func TestImport_DryRun_Valid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		content  string
		wantSvcs int
	}{
		{
			name: "two services",
			content: `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
  - hostname: db
    type: postgresql@16
    mode: NON_HA
`,
			wantSvcs: 2,
		},
		{
			name: "single service",
			content: `services:
  - hostname: web
    type: nginx@1.26
    mode: NON_HA
`,
			wantSvcs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock()
			result, err := Import(context.Background(), mock, "proj-1", tt.content, "", true, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			dr, ok := result.(*ImportDryRunResult)
			if !ok {
				t.Fatalf("expected *ImportDryRunResult, got %T", result)
			}
			if !dr.DryRun {
				t.Error("expected DryRun=true")
			}
			if !dr.Valid {
				t.Error("expected Valid=true")
			}
			if len(dr.Services) != tt.wantSvcs {
				t.Errorf("expected %d services, got %d", tt.wantSvcs, len(dr.Services))
			}
		})
	}
}

func TestImport_DryRun_InvalidYAML(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	_, err := Import(context.Background(), mock, "proj-1", "{{invalid yaml", "", true, nil)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidImportYml {
		t.Errorf("expected code %s, got %s", platform.ErrInvalidImportYml, pe.Code)
	}
}

func TestImport_DryRun_MissingServices(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	_, err := Import(context.Background(), mock, "proj-1", "foo: bar\n", "", true, nil)
	if err == nil {
		t.Fatal("expected error for missing services key")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidImportYml {
		t.Errorf("expected code %s, got %s", platform.ErrInvalidImportYml, pe.Code)
	}
}

func TestImport_DryRun_HasProjectSection(t *testing.T) {
	t.Parallel()
	content := `project:
  name: myproject
services:
  - hostname: api
    type: nodejs@22
`
	mock := platform.NewMock()
	_, err := Import(context.Background(), mock, "proj-1", content, "", true, nil)
	if err == nil {
		t.Fatal("expected error for project section")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrImportHasProject {
		t.Errorf("expected code %s, got %s", platform.ErrImportHasProject, pe.Code)
	}
}

func TestImport_Real_Success(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	mock := platform.NewMock().
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

	result, err := Import(context.Background(), mock, "proj-1", content, "", false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rr, ok := result.(*ImportRealResult)
	if !ok {
		t.Fatalf("expected *ImportRealResult, got %T", result)
	}
	if rr.ProjectID != "proj-1" {
		t.Errorf("expected projectId=proj-1, got %s", rr.ProjectID)
	}
	if rr.ProjectName != "myproject" {
		t.Errorf("expected projectName=myproject, got %s", rr.ProjectName)
	}
	if len(rr.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(rr.Processes))
	}
	p := rr.Processes[0]
	if p.ProcessID != "proc-1" {
		t.Errorf("expected processId=proc-1, got %s", p.ProcessID)
	}
	if p.Service != "api" {
		t.Errorf("expected service=api, got %s", p.Service)
	}
	if p.ServiceID != "svc-1" {
		t.Errorf("expected serviceId=svc-1, got %s", p.ServiceID)
	}
}

func TestImport_NoInput(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	_, err := Import(context.Background(), mock, "proj-1", "", "", true, nil)
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
	_, err := Import(context.Background(), mock, "proj-1", "content", "/some/path", true, nil)
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
	_, err := Import(context.Background(), mock, "proj-1", "", "/nonexistent/file.yml", true, nil)
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

	result, err := Import(context.Background(), mock, "proj-1", "", fp, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rr, ok := result.(*ImportRealResult)
	if !ok {
		t.Fatalf("expected *ImportRealResult, got %T", result)
	}
	if len(rr.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(rr.Processes))
	}
}

// --- Version Validation Tests ---

func TestImport_DryRun_VersionWarnings(t *testing.T) {
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
				{Name: "nodejs@22", Status: "ACTIVE"},
			},
		},
	}
	mock := platform.NewMock()
	result, err := Import(context.Background(), mock, "proj-1", content, "", true, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dr, ok := result.(*ImportDryRunResult)
	if !ok {
		t.Fatalf("expected *ImportDryRunResult, got %T", result)
	}
	if len(dr.Warnings) == 0 {
		t.Fatal("expected version warnings for ruby@3.2")
	}
	found := false
	for _, w := range dr.Warnings {
		if strings.Contains(w, "ruby@3.2") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning mentioning ruby@3.2, got: %v", dr.Warnings)
	}
}

func TestImport_DryRun_ModeWarnings(t *testing.T) {
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
				{Name: "postgresql@16", Status: "ACTIVE"},
			},
		},
	}
	mock := platform.NewMock()
	result, err := Import(context.Background(), mock, "proj-1", content, "", true, types)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dr, ok := result.(*ImportDryRunResult)
	if !ok {
		t.Fatalf("expected *ImportDryRunResult, got %T", result)
	}
	if len(dr.Warnings) == 0 {
		t.Fatal("expected mode warning for postgresql without mode")
	}
	found := false
	for _, w := range dr.Warnings {
		if strings.Contains(w, "mode") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about missing mode, got: %v", dr.Warnings)
	}
}

func TestImport_DryRun_NilTypes_NoWarnings(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: ruby@3.2
`
	mock := platform.NewMock()
	result, err := Import(context.Background(), mock, "proj-1", content, "", true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dr, ok := result.(*ImportDryRunResult)
	if !ok {
		t.Fatalf("expected *ImportDryRunResult, got %T", result)
	}
	if len(dr.Warnings) != 0 {
		t.Errorf("expected no warnings with nil types, got: %v", dr.Warnings)
	}
}

func TestImport_Real_APIError(t *testing.T) {
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

	_, err := Import(context.Background(), mock, "proj-1", content, "", false, nil)
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
