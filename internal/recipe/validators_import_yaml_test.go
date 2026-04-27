package recipe

import (
	"context"
	"strings"
	"testing"
)

// TestImportYamlComments_FabricatedFieldName pins run-15 F.5: the
// engine refuses comment tokens that look like yaml field paths but
// don't appear in the parsed yaml. Run-14 shipped `project_env_vars`
// (snake_case) preambles when the actual schema field is
// `project.envVariables` (camelCase) — structurally invisible to a
// porter searching the yaml.
func TestImportYamlComments_FabricatedFieldName(t *testing.T) {
	t.Parallel()

	const yamlBody = `# Stage tier — single-replica production validation.
# project_env_vars carry shared config across services.
project:
  name: example
  envVariables:
    APP_KEY: foo
services:
  - hostname: api
    type: nodejs@22
`
	plan := &Plan{
		Slug:        "example",
		EnvComments: map[string]EnvComments{"3": {Project: "shared config"}},
	}
	inputs := SurfaceInputs{Plan: plan}
	vs, err := validateEnvImportComments(context.Background(), "3 — Stage/import.yaml", []byte(yamlBody), inputs)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "env-yaml-fabricated-field-name") {
		t.Errorf("expected env-yaml-fabricated-field-name on `project_env_vars`, got %+v", vs)
	}
	// Sanity: the message names the offending token.
	for _, v := range vs {
		if v.Code == "env-yaml-fabricated-field-name" && !strings.Contains(v.Message, "project_env_vars") {
			t.Errorf("violation message should name the fabricated token: %q", v.Message)
		}
	}
}

// TestImportYamlComments_RealFieldName_Passes is the negative side of
// the cross-check: a comment naming `project.envVariables` (the real
// camelCase path) passes silently.
func TestImportYamlComments_RealFieldName_Passes(t *testing.T) {
	t.Parallel()

	const yamlBody = `# Stage tier — project.envVariables holds shared config.
# envVariables propagate to every service in the project.
project:
  name: example
  envVariables:
    APP_KEY: foo
services:
  - hostname: api
    type: nodejs@22
`
	plan := &Plan{
		Slug:        "example",
		EnvComments: map[string]EnvComments{"3": {Project: "shared config"}},
	}
	vs, err := validateEnvImportComments(context.Background(), "3 — Stage/import.yaml", []byte(yamlBody), SurfaceInputs{Plan: plan})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "env-yaml-fabricated-field-name") {
		t.Errorf("real field name `project.envVariables` should pass; got %+v", vs)
	}
}

// TestImportYamlComments_AudienceVoiceLeak pins the audience-voice
// patrol — comments naming "recipe author" or "during scaffold" emit
// notice (porter-facing voice rule).
func TestImportYamlComments_AudienceVoiceLeak(t *testing.T) {
	t.Parallel()

	const yamlBody = `# Stage tier — single-replica setup.
# The recipe author chose APP_KEY at the project level so sessions survive container churn.
project:
  name: example
services:
  - hostname: api
    type: nodejs@22
`
	plan := &Plan{Slug: "example", EnvComments: map[string]EnvComments{"3": {Project: "shared config"}}}
	vs, err := validateEnvImportComments(context.Background(), "3 — Stage/import.yaml", []byte(yamlBody), SurfaceInputs{Plan: plan})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if !containsCode(vs, "env-yaml-audience-voice-leak") {
		t.Errorf("expected env-yaml-audience-voice-leak on 'recipe author' phrase, got %+v", vs)
	}
}

// TestImportYamlComments_EnglishProse_NoFalsePositive confirms the
// regex shape filter (must contain `_` or `.`) keeps single-word
// English prose out of the fabricated-field flag.
func TestImportYamlComments_EnglishProse_NoFalsePositive(t *testing.T) {
	t.Parallel()

	const yamlBody = `# Stage tier — production validation under realistic load.
# Containers replicate across nodes for failure tolerance.
project:
  name: example
services:
  - hostname: api
    type: nodejs@22
`
	plan := &Plan{Slug: "example", EnvComments: map[string]EnvComments{"3": {Project: "shared config"}}}
	vs, err := validateEnvImportComments(context.Background(), "3 — Stage/import.yaml", []byte(yamlBody), SurfaceInputs{Plan: plan})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "env-yaml-fabricated-field-name") {
		t.Errorf("English prose comments should not emit fabricated-field violations; got %+v", vs)
	}
}
