// Tests for: plans/analysis/ops.md § validate
package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

func TestValidate_ZeropsYml_Valid(t *testing.T) {
	t.Parallel()
	content := `zerops:
  - run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
`
	result, err := Validate(content, "", "zerops.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected Valid=true, errors: %v", result.Errors)
	}
	if result.Type != "zerops.yml" {
		t.Errorf("expected type=zerops.yml, got %s", result.Type)
	}
}

func TestValidate_ZeropsYml_MissingKey(t *testing.T) {
	t.Parallel()
	content := `app:
  run:
    base: nodejs@22
`
	result, err := Validate(content, "", "zerops.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false for missing zerops key")
	}
	if len(result.Errors) == 0 {
		t.Error("expected at least one validation error")
	}
}

func TestValidate_ZeropsYml_EmptyArray(t *testing.T) {
	t.Parallel()
	content := `zerops: []
`
	result, err := Validate(content, "", "zerops.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false for empty zerops array")
	}
	if len(result.Errors) == 0 {
		t.Error("expected at least one validation error")
	}
}

func TestValidate_ZeropsYml_BadSyntax(t *testing.T) {
	t.Parallel()
	_, err := Validate("{{bad yaml", "", "zerops.yml")
	if err == nil {
		t.Fatal("expected error for bad YAML syntax")
	}
	pe, ok := err.(*platform.PlatformError)
	if !ok {
		t.Fatalf("expected *PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidZeropsYml {
		t.Errorf("expected code %s, got %s", platform.ErrInvalidZeropsYml, pe.Code)
	}
}

func TestValidate_ImportYml_Valid(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	result, err := Validate(content, "", "import.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected Valid=true, errors: %v", result.Errors)
	}
	if result.Type != "import.yml" {
		t.Errorf("expected type=import.yml, got %s", result.Type)
	}
}

func TestValidate_ImportYml_HasProject(t *testing.T) {
	t.Parallel()
	content := `project:
  name: myproject
services:
  - hostname: api
    type: nodejs@22
`
	_, err := Validate(content, "", "import.yml")
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

func TestValidate_ImportYml_MissingServices(t *testing.T) {
	t.Parallel()
	content := `foo: bar
`
	result, err := Validate(content, "", "import.yml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected Valid=false for missing services key")
	}
	if len(result.Errors) == 0 {
		t.Error("expected at least one validation error")
	}
}

func TestValidate_AutoDetect_Import(t *testing.T) {
	t.Parallel()
	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	result, err := Validate(content, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != "import.yml" {
		t.Errorf("expected auto-detected type=import.yml, got %s", result.Type)
	}
}

func TestValidate_AutoDetect_Zerops(t *testing.T) {
	t.Parallel()
	content := `zerops:
  - run:
      base: nodejs@22
`
	result, err := Validate(content, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != "zerops.yml" {
		t.Errorf("expected auto-detected type=zerops.yml, got %s", result.Type)
	}
}

func TestValidate_FileRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "zerops.yml")
	yamlContent := `zerops:
  - run:
      base: nodejs@22
`
	if err := os.WriteFile(fp, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	result, err := Validate("", fp, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected Valid=true, errors: %v", result.Errors)
	}
	if result.File != fp {
		t.Errorf("expected File=%s, got %s", fp, result.File)
	}
}

func TestValidate_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := Validate("", "/nonexistent/zerops.yml", "")
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

func TestValidate_NoInput(t *testing.T) {
	t.Parallel()
	_, err := Validate("", "", "")
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

func TestValidate_BothInputs(t *testing.T) {
	t.Parallel()
	_, err := Validate("content", "/some/path", "")
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

func TestValidate_AutoDetect_FromFilename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	fp := filepath.Join(dir, "import.yml")
	content := `services:
  - hostname: api
    type: nodejs@22
    mode: NON_HA
`
	if err := os.WriteFile(fp, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	result, err := Validate("", fp, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != "import.yml" {
		t.Errorf("expected type=import.yml from filename, got %s", result.Type)
	}
}
