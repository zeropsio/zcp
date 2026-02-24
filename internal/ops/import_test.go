// Tests for: plans/analysis/ops.md § import
package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestImport_DeletingServiceConflict_WaitsAndSucceeds(t *testing.T) {
	t.Parallel()

	callCount := 0
	mock := &deletingMock{
		Mock: importMock(),
		listServicesFunc: func() []platform.ServiceStack {
			callCount++
			if callCount <= 2 {
				// First 2 polls: service still DELETING.
				return []platform.ServiceStack{
					{ID: "svc-old", Name: "api", Status: "DELETING"},
				}
			}
			// After 2 polls: service gone.
			return []platform.ServiceStack{}
		},
	}

	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	result, err := Import(context.Background(), mock, "proj-1", content, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProjectID != "proj-1" {
		t.Errorf("expected projectId=proj-1, got %s", result.ProjectID)
	}
	if callCount < 3 {
		t.Errorf("expected at least 3 ListServices calls (2 DELETING + 1 clear), got %d", callCount)
	}
}

func TestImport_DeletingServiceConflict_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	t.Parallel()

	mock := &deletingMock{
		Mock: importMock(),
		listServicesFunc: func() []platform.ServiceStack {
			// Always DELETING — never clears.
			return []platform.ServiceStack{
				{ID: "svc-old", Name: "api", Status: "DELETING"},
			}
		},
	}

	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Import(ctx, mock, "proj-1", content, "", nil)
	if err == nil {
		t.Fatal("expected timeout error for stuck DELETING service")
	}

	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrAPITimeout {
		t.Errorf("expected code %s, got %s", platform.ErrAPITimeout, pe.Code)
	}
}

func TestImport_DeletingServiceNoConflict(t *testing.T) {
	t.Parallel()

	callCount := 0
	mock := &deletingMock{
		Mock: importMock(),
		listServicesFunc: func() []platform.ServiceStack {
			callCount++
			// DELETING service has different hostname — no conflict.
			return []platform.ServiceStack{
				{ID: "svc-old", Name: "other", Status: "DELETING"},
			}
		},
	}

	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	result, err := Import(context.Background(), mock, "proj-1", content, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProjectID != "proj-1" {
		t.Errorf("expected projectId=proj-1, got %s", result.ProjectID)
	}
	// Should check only once — no conflict, no polling.
	if callCount != 1 {
		t.Errorf("expected 1 ListServices call (no conflict), got %d", callCount)
	}
}

func TestExtractHostnames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  map[string]any
		want []string
	}{
		{
			name: "single service",
			doc: map[string]any{
				"services": []any{
					map[string]any{"hostname": "api", "type": "nodejs@22"},
				},
			},
			want: []string{"api"},
		},
		{
			name: "multiple services",
			doc: map[string]any{
				"services": []any{
					map[string]any{"hostname": "api", "type": "nodejs@22"},
					map[string]any{"hostname": "db", "type": "postgresql@16"},
				},
			},
			want: []string{"api", "db"},
		},
		{
			name: "no services key",
			doc:  map[string]any{},
			want: nil,
		},
		{
			name: "services without hostname",
			doc: map[string]any{
				"services": []any{
					map[string]any{"type": "nodejs@22"},
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractHostnames(tt.doc)
			if len(got) != len(tt.want) {
				t.Fatalf("extractHostnames() = %v, want %v", got, tt.want)
			}
			for i, h := range got {
				if h != tt.want[i] {
					t.Errorf("hostname[%d] = %s, want %s", i, h, tt.want[i])
				}
			}
		})
	}
}

// deletingMock wraps platform.Mock but overrides ListServices to simulate DELETING services.
type deletingMock struct {
	*platform.Mock
	listServicesFunc func() []platform.ServiceStack
}

func (d *deletingMock) ListServices(_ context.Context, _ string) ([]platform.ServiceStack, error) {
	return d.listServicesFunc(), nil
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
