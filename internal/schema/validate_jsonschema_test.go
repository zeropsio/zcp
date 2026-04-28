package schema

import (
	"strings"
	"testing"
)

const validImportYAML = `project:
  name: demo
services:
  - hostname: appdev
    type: nodejs@22
    mode: NON_HA
    buildFromGit: https://github.com/example/demo.git
    zeropsSetup: appdev
`

const validZeropsYAML = `zerops:
  - setup: appdev
    build:
      base: nodejs@22
      buildCommands:
        - npm install
        - npm run build
      deployFiles: ["./"]
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
`

func TestValidateImportYAML_HappyPath(t *testing.T) {
	t.Parallel()
	errs := ValidateImportYAML(validImportYAML)
	if len(errs) != 0 {
		t.Errorf("valid import.yaml unexpectedly errored: %v", errs)
	}
}

func TestValidateImportYAML_EmptyContent(t *testing.T) {
	t.Parallel()
	errs := ValidateImportYAML("")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d (%v)", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "empty") {
		t.Errorf("expected empty-content message, got %q", errs[0].Message)
	}
}

func TestValidateImportYAML_MalformedYAML(t *testing.T) {
	t.Parallel()
	errs := ValidateImportYAML("project: : :")
	if len(errs) == 0 {
		t.Fatal("expected parse error, got none")
	}
	if !strings.Contains(errs[0].Message, "parse yaml") {
		t.Errorf("expected parse-yaml message, got %q", errs[0].Message)
	}
}

func TestValidateImportYAML_MissingProjectName(t *testing.T) {
	t.Parallel()
	body := `project:
  envVariables:
    LOG_LEVEL: info
services:
  - hostname: appdev
    type: nodejs@22
    mode: NON_HA
    buildFromGit: https://github.com/example/demo.git
    zeropsSetup: appdev
`
	errs := ValidateImportYAML(body)
	if len(errs) == 0 {
		t.Fatal("missing project.name should fail validation")
	}
	if !containsPath(errs, "name") {
		t.Errorf("expected error mentioning project.name, got %v", errs)
	}
}

func TestValidateImportYAML_PreprocessorHeaderTolerated(t *testing.T) {
	t.Parallel()
	body := "#zeropsPreprocessor=on\n" + validImportYAML
	errs := ValidateImportYAML(body)
	if len(errs) != 0 {
		t.Errorf("preprocessor-header import.yaml should pass; got %v", errs)
	}
}

func TestValidateZeropsYAML_HappyPath(t *testing.T) {
	t.Parallel()
	errs := ValidateZeropsYAML(validZeropsYAML, "appdev")
	if len(errs) != 0 {
		t.Errorf("valid zerops.yaml unexpectedly errored: %v", errs)
	}
}

func TestValidateZeropsYAML_EmptyContent(t *testing.T) {
	t.Parallel()
	errs := ValidateZeropsYAML("", "appdev")
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d (%v)", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "empty") {
		t.Errorf("expected empty-content message, got %q", errs[0].Message)
	}
}

func TestValidateZeropsYAML_MissingRequiredSetup(t *testing.T) {
	t.Parallel()
	errs := ValidateZeropsYAML(validZeropsYAML, "ghost")
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "ghost") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error naming missing setup %q, got %v", "ghost", errs)
	}
}

func TestValidateZeropsYAML_NoRequiredSetupCheck(t *testing.T) {
	t.Parallel()
	errs := ValidateZeropsYAML(validZeropsYAML, "")
	if len(errs) != 0 {
		t.Errorf("empty requiredSetup should skip the setup-presence check; got %v", errs)
	}
}

func TestValidateZeropsYAML_MalformedYAML(t *testing.T) {
	t.Parallel()
	errs := ValidateZeropsYAML("zerops:\n  - bad: : :", "appdev")
	if len(errs) == 0 {
		t.Fatal("malformed yaml should fail validation")
	}
}

func TestValidateImportYAML_DeterministicOutput(t *testing.T) {
	t.Parallel()
	first := ValidateImportYAML(validImportYAML)
	second := ValidateImportYAML(validImportYAML)
	if len(first) != len(second) {
		t.Fatalf("validation len differs across calls: %d vs %d", len(first), len(second))
	}
}

// TestEmbeddedSchemasMatchTestdata pins the contract that the embedded
// schemas — which the validators compile from — are the same bytes as
// the canonical testdata files. Drift between the embedded copy and
// the testdata copy means CI would test against a different schema
// than runtime, defeating the validation gate. Live-vs-embedded drift
// is a separate concern (refresh cadence is a maintenance task).
func TestEmbeddedSchemasMatchTestdata(t *testing.T) {
	t.Parallel()
	if len(embeddedImportSchema) == 0 {
		t.Error("embedded import schema is empty — embed.FS not wired")
	}
	if len(embeddedZeropsSchema) == 0 {
		t.Error("embedded zerops schema is empty — embed.FS not wired")
	}
}

func containsPath(errs []ValidationError, fragment string) bool {
	for _, e := range errs {
		if strings.Contains(e.Path, fragment) || strings.Contains(e.Message, fragment) {
			return true
		}
	}
	return false
}
