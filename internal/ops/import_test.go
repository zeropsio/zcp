// Tests for: plans/analysis/ops.md § import
package ops

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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

	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
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

// Recipe import templates carry a top-level `project:` block (used by the
// create-new-project flow). When an agent copies that template into
// zerops_import within an existing project, the preflight rejects with
// IMPORT_HAS_PROJECT. The recovery hint must explicitly name the env-var
// fallback path because most occurrences of `project:` carry envVariables
// the agent needs to preserve — surfaced by Tier-2 eval
// `bootstrap-user-forces-classic` (2026-04-30): agent stripped the block
// but then asked what to do with the project-level envVariables.
func TestImport_RejectsProjectBlockWithDirectiveHint(t *testing.T) {
	t.Parallel()
	content := `project:
  name: myproject
  envVariables:
    APP_KEY: <@generateRandomString(<32>)>
services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	mock := platform.NewMock()
	_, err := Import(context.Background(), mock, "proj-1", content, "", false)
	if err == nil {
		t.Fatal("expected IMPORT_HAS_PROJECT rejection")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrImportHasProject {
		t.Errorf("expected code %s, got %s", platform.ErrImportHasProject, pe.Code)
	}
	for _, want := range []string{
		"zerops_env",      // names the recovery tool
		`scope="project"`, // names the right scope (raw string — actual hint is unescaped)
		"preprocessor",    // explains directives are passed literally
	} {
		if !strings.Contains(pe.Suggestion, want) {
			t.Errorf("suggestion missing %q; got %q", want, pe.Suggestion)
		}
	}
}

// TestImport_ProjectEnvVariablesOnly_AppliedInline verifies the whitelist
// behavior: project.envVariables in import YAML is applied via the
// project-env channel (EnvSet) before service creation, eliminating the
// historical 2-step "set project envs, then import services" workflow.
// See plans/multi-runtime-audit-followup.md §5 Fix B1.
func TestImport_ProjectEnvVariablesOnly_AppliedInline(t *testing.T) {
	t.Parallel()

	mock := importMock()

	content := `project:
  envVariables:
    APP_KEY: secret123
    DB_PASSWORD: hunter2
services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	wantKeys := []string{"APP_KEY", "DB_PASSWORD"}
	if !reflect.DeepEqual(result.ProjectEnvsSet, wantKeys) {
		t.Errorf("ProjectEnvsSet = %v, want %v", result.ProjectEnvsSet, wantKeys)
	}

	if len(mock.CapturedProjectEnvCreations) != 2 {
		t.Fatalf("expected 2 project env creations, got %d: %+v", len(mock.CapturedProjectEnvCreations), mock.CapturedProjectEnvCreations)
	}
	gotValues := map[string]string{}
	for _, c := range mock.CapturedProjectEnvCreations {
		if c.ProjectID != "proj-1" {
			t.Errorf("expected projectID=proj-1 on env creation, got %s", c.ProjectID)
		}
		gotValues[c.Key] = c.Content
	}
	if gotValues["APP_KEY"] != "secret123" || gotValues["DB_PASSWORD"] != "hunter2" {
		t.Errorf("unexpected env values captured: %+v", gotValues)
	}

	// The 'project:' block must not reach the API call — services-only YAML.
	if strings.Contains(mock.CapturedImportYAML, "project:") {
		t.Errorf("expected 'project:' stripped from the YAML sent to ImportServices, got:\n%s", mock.CapturedImportYAML)
	}
}

// TestImport_ProjectNonEnvFields_Rejected pins the whitelist boundary: any
// project.* field OTHER than envVariables still hard-rejects with
// ErrImportHasProject, naming the offending key, and does so BEFORE any
// env writes happen (reject-before-mutate — a partially-applied project
// block would be worse than the old blanket rejection). Full project YAML
// (preprocessor, scaling, name, ...) belongs to project creation (zcli or
// web UI), not zerops_import which operates inside an existing project.
func TestImport_ProjectNonEnvFields_Rejected(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()

	content := `project:
  envVariables:
    APP_KEY: secret
  preprocessor:
    enabled: true
services:
  - hostname: db
    type: postgresql@17
    mode: NON_HA
`
	_, err := Import(context.Background(), mock, "proj-1", content, "", false)
	if err == nil {
		t.Fatal("expected rejection for project.preprocessor, got success")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrImportHasProject {
		t.Errorf("expected code %s, got %s", platform.ErrImportHasProject, pe.Code)
	}
	if !strings.Contains(pe.Message, "project.preprocessor") {
		t.Errorf("error message should name the offending key, got: %v", pe.Message)
	}
	if len(mock.CapturedProjectEnvCreations) != 0 {
		t.Errorf("expected no project env writes on rejection (reject-before-mutate), got %+v", mock.CapturedProjectEnvCreations)
	}
}

func TestImport_NoInput(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	_, err := Import(context.Background(), mock, "proj-1", "", "", false)
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
	_, err := Import(context.Background(), mock, "proj-1", "content", "/some/path", false)
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
	_, err := Import(context.Background(), mock, "proj-1", "", "/nonexistent/file.yml", false)
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

	result, err := Import(context.Background(), mock, "proj-1", "", fp, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(result.Processes))
	}
}

// Version/mode warning tests were removed in W6 of
// plans/api-validation-plumbing.md. Those duplicated what the Zerops API
// catches at import time; field-level detail now reaches the LLM via
// PlatformError.APIMeta rather than ZCP-side warnings.

func TestImport_EnvVariablesAtServiceLevel_Warning(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: app
    type: nodejs@22
    envVariables:
      MY_VAR: hello
`
	mock := importMock()
	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "envVariables") && strings.Contains(w, "silently dropped") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected envVariables warning, got: %v", result.Warnings)
	}
}

func TestImport_NilTypes_NoWarnings(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: ruby@3.2
`
	mock := importMock()
	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
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
	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
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

	_, err := Import(ctx, mock, "proj-1", content, "", false)
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
	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
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

// TestImport_InvalidHostname was removed in W6 — the API now validates
// hostname format server-side (`serviceStackNameInvalid`). The LLM
// receives the rule via the bootstrap-provision-rules atom (W7) before
// generating the YAML. Coverage for the API's rejection shape lives in
// internal/platform/zerops_errors_test.go and in the server E2E pair.

func TestImport_ServiceError_Surfaced(t *testing.T) {
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
					Error: &platform.APIError{
						Code:    "SERVICE_LIMIT_REACHED",
						Message: "maximum number of services reached",
					},
				},
			},
		})

	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ServiceErrors) != 1 {
		t.Fatalf("expected 1 service error, got %d", len(result.ServiceErrors))
	}
	se := result.ServiceErrors[0]
	if se.Service != "api" {
		t.Errorf("expected service=api, got %s", se.Service)
	}
	if se.Code != "SERVICE_LIMIT_REACHED" {
		t.Errorf("expected code=SERVICE_LIMIT_REACHED, got %s", se.Code)
	}
	if se.Message != "maximum number of services reached" {
		t.Errorf("expected message='maximum number of services reached', got %s", se.Message)
	}
}

func TestImport_ServiceError_MetaPropagated(t *testing.T) {
	t.Parallel()
	// When the API rejects a specific service-stack with field-level detail
	// in Meta, that detail must reach the LLM via ImportResult.ServiceErrors.
	// Regression guard: silent-drop of Meta was the root cause of F#7.
	content := `services:
  - hostname: storage
    type: object-storage
    mode: NON_HA
    objectStorageSize: 1
    objectStoragePolicy: private
`
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{
					ID:   "svc-storage",
					Name: "storage",
					Error: &platform.APIError{
						Code:    "projectImportInvalidParameter",
						Message: "Invalid parameter provided.",
						Meta: []platform.APIMetaItem{
							{
								Code:  "projectImportInvalidParameter",
								Error: "Invalid parameter provided.",
								Metadata: map[string][]string{
									"storage.mode": {"mode not supported"},
								},
							},
						},
					},
				},
			},
		})

	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ServiceErrors) != 1 {
		t.Fatalf("expected 1 service error, got %d", len(result.ServiceErrors))
	}
	se := result.ServiceErrors[0]
	if len(se.Meta) != 1 {
		t.Fatalf("expected 1 meta item, got %d", len(se.Meta))
	}
	got := se.Meta[0].Metadata["storage.mode"]
	if len(got) != 1 || got[0] != "mode not supported" {
		t.Errorf("storage.mode meta = %v, want [\"mode not supported\"]", got)
	}
	if se.Meta[0].Code != "projectImportInvalidParameter" {
		t.Errorf("meta.Code = %q, want projectImportInvalidParameter", se.Meta[0].Code)
	}
}

func TestImport_MixedSuccessAndError(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
  - hostname: db
    type: postgresql@16
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
						{ID: "proc-1", ActionName: "serviceStackImport", Status: "PENDING"},
					},
				},
				{
					ID:   "svc-2",
					Name: "db",
					Error: &platform.APIError{
						Code:    "QUOTA_EXCEEDED",
						Message: "disk quota exceeded",
					},
				},
			},
		})

	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(result.Processes))
	}
	if len(result.ServiceErrors) != 1 {
		t.Fatalf("expected 1 service error, got %d", len(result.ServiceErrors))
	}
	if result.ServiceErrors[0].Service != "db" {
		t.Errorf("expected error service=db, got %s", result.ServiceErrors[0].Service)
	}
}

func TestImport_AllErrors(t *testing.T) {
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
					Error: &platform.APIError{
						Code:    "ERR1",
						Message: "error one",
					},
				},
				{
					ID:   "svc-2",
					Name: "db",
					Error: &platform.APIError{
						Code:    "ERR2",
						Message: "error two",
					},
				},
			},
		})

	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Processes) != 0 {
		t.Errorf("expected 0 processes, got %d", len(result.Processes))
	}
	if len(result.ServiceErrors) != 2 {
		t.Fatalf("expected 2 service errors, got %d", len(result.ServiceErrors))
	}
}

func TestImport_FailReason_Mapped(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	reason := "build compilation error"
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
							Status:     "FAILED",
							FailReason: &reason,
						},
					},
				},
			},
		})

	result, err := Import(context.Background(), mock, "proj-1", content, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Processes) != 1 {
		t.Fatalf("expected 1 process, got %d", len(result.Processes))
	}
	p := result.Processes[0]
	if p.FailReason == nil {
		t.Fatal("expected failReason to be mapped, got nil")
	}
	if *p.FailReason != reason {
		t.Errorf("expected failReason=%q, got %q", reason, *p.FailReason)
	}
}

func TestImport_ZeropsYamlPassthrough(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{
			name: "zeropsYaml as string passes through",
			content: `services:
  - hostname: app
    type: nodejs@22
    mode: NON_HA
    zeropsYaml: |
      zerops:
        - setup: app
          run:
            start: node index.js
`,
		},
		{
			name: "zeropsYaml as nested map passes through",
			content: `services:
  - hostname: app
    type: nodejs@22
    mode: NON_HA
    zeropsYaml:
      zerops:
        - setup: app
          run:
            start: node index.js
`,
		},
		{
			name: "zeropsYaml as nested map with complex shell commands",
			content: `services:
  - hostname: app
    type: nodejs@22
    mode: NON_HA
    zeropsYaml:
      zerops:
        - setup: app
          build:
            buildCommands:
              - node -e "console.log('hello: world')"
          run:
            start: node index.js
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := importMock()
			_, err := Import(context.Background(), mock, "proj-1", tt.content, "", false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			// YAML must be passed through byte-for-byte — no re-serialization.
			if mock.CapturedImportYAML != tt.content {
				t.Errorf("YAML was modified during import.\nInput:  %q\nOutput: %q", tt.content, mock.CapturedImportYAML)
			}
		})
	}
}

func TestImport_Override_InjectsFlag(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: appdev
    type: php-nginx@8.4
    startWithoutCode: true
  - hostname: db
    type: postgresql@18
`
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-1", Name: "appdev", Processes: []platform.Process{{ID: "proc-1", ActionName: "serviceStackImport", Status: "PENDING"}}},
				{ID: "svc-2", Name: "db", Processes: []platform.Process{{ID: "proc-2", ActionName: "serviceStackImport", Status: "PENDING"}}},
			},
		})

	if _, err := Import(context.Background(), mock, "proj-1", content, "", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	captured := mock.CapturedImportYAML
	if !strings.Contains(captured, "override: true") {
		t.Errorf("expected 'override: true' in captured YAML, got:\n%s", captured)
	}
	// Both services must carry the flag.
	if strings.Count(captured, "override: true") < 2 {
		t.Errorf("expected override:true on every service (2), got %d occurrences in:\n%s",
			strings.Count(captured, "override: true"), captured)
	}
}

func TestImport_Override_Disabled_Passthrough(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: appdev
    type: php-nginx@8.4
`
	mock := importMock()
	if _, err := Import(context.Background(), mock, "proj-1", content, "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.CapturedImportYAML != content {
		t.Errorf("YAML must pass through byte-for-byte when override=false.\nInput:  %q\nOutput: %q", content, mock.CapturedImportYAML)
	}
}

// TestImport_Override_ReplacementWarning pins B10: when override=true is
// used and one or more target hostnames already exist in the project, the
// response Warnings explicitly name the replaced services. The replacement
// is destructive (container, deployed code, env vars, SSHFS mount all torn
// down) — leaving it silent let the agent in `develop-ambiguous-state`
// blow away their scaffolded zerops.yaml + index.php without notice.
func TestImport_Override_ReplacementWarning(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: appdev
    type: php-nginx@8.4
    startWithoutCode: true
  - hostname: brandnew
    type: nodejs@22
`
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appdev", Status: "READY_TO_DEPLOY"},
		}).
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-1", Name: "appdev"},
				{ID: "svc-2", Name: "brandnew"},
			},
		})

	result, err := Import(context.Background(), mock, "proj-1", content, "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found string
	for _, w := range result.Warnings {
		if strings.Contains(w, "override=true REPLACED") {
			found = w
			break
		}
	}
	if found == "" {
		t.Fatalf("expected destructive-replacement warning when override=true hits a pre-existing service.\nWarnings: %+v", result.Warnings)
	}
	if !strings.Contains(found, "appdev") {
		t.Errorf("warning must name the replaced hostname (appdev), got: %s", found)
	}
	// `brandnew` is not pre-existing — it's a fresh import with override=true (a
	// no-op for that service), so it must NOT appear in the warning.
	if strings.Contains(found, "brandnew") {
		t.Errorf("warning must NOT name brand-new hostnames (no replacement happened for them), got: %s", found)
	}
}

// TestImport_Override_NoExistingServices_NoWarning pins the inverse: when
// override=true is set but NONE of the target hostnames pre-exist, no
// destructive warning fires (the override flag is a no-op for those calls).
func TestImport_Override_NoExistingServices_NoWarning(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: freshapp
    type: nodejs@22
    startWithoutCode: true
`
	mock := platform.NewMock().
		WithImportResult(&platform.ImportResult{
			ProjectID:   "proj-1",
			ProjectName: "myproject",
			ServiceStacks: []platform.ImportedServiceStack{
				{ID: "svc-1", Name: "freshapp"},
			},
		})

	result, err := Import(context.Background(), mock, "proj-1", content, "", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range result.Warnings {
		if strings.Contains(w, "override=true REPLACED") {
			t.Errorf("must not emit destructive warning when no service was actually replaced, got: %s", w)
		}
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

	_, err := Import(context.Background(), mock, "proj-1", content, "", false)
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
