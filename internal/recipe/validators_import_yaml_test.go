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

// Run-21 §A4 — three context-based escapes for the fabricated-field
// validator: backtick-wrapped tokens, `${...}` alias interpolations,
// and file-extension tails. Each test pins one escape against an
// otherwise-fabrication-shaped token.

func TestImportYamlComments_BacktickWrapped_Suppressed(t *testing.T) {
	t.Parallel()
	const yamlBody = `# The api publishes ` + "`tasks.created`" + ` and the worker
# subscribes through the queue group ` + "`workers`" + `.
project:
  name: example
services:
  - hostname: api
    type: nodejs@22
`
	plan := &Plan{Slug: "example", EnvComments: map[string]EnvComments{"0": {Project: "n/a"}}}
	vs, err := validateEnvImportComments(context.Background(), "0/import.yaml", []byte(yamlBody), SurfaceInputs{Plan: plan})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	for _, v := range vs {
		if v.Code == "env-yaml-fabricated-field-name" && strings.Contains(v.Message, "tasks.created") {
			t.Errorf("backtick-wrapped `tasks.created` should not flag as fabricated yaml field: %+v", v)
		}
	}
}

func TestImportYamlComments_AliasInterpolation_Suppressed(t *testing.T) {
	t.Parallel()
	const yamlBody = `# Client code reads ${cache_hostname} and ${cache_port}
# to address the managed cache.
project:
  name: example
services:
  - hostname: cache
    type: valkey@7.2
`
	plan := &Plan{Slug: "example", EnvComments: map[string]EnvComments{"0": {Project: "n/a"}}}
	vs, err := validateEnvImportComments(context.Background(), "0/import.yaml", []byte(yamlBody), SurfaceInputs{Plan: plan})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	for _, v := range vs {
		if v.Code == "env-yaml-fabricated-field-name" {
			if strings.Contains(v.Message, "cache_hostname") || strings.Contains(v.Message, "cache_port") {
				t.Errorf("`${cache_hostname}` interpolation should not flag as fabricated yaml field: %+v", v)
			}
		}
	}
}

func TestImportYamlComments_FileExtension_Suppressed(t *testing.T) {
	t.Parallel()
	const yamlBody = `# SPA generates config.json at runtime; vite.config.js is
# build-time only.
project:
  name: example
services:
  - hostname: api
    type: nodejs@22
`
	plan := &Plan{Slug: "example", EnvComments: map[string]EnvComments{"0": {Project: "n/a"}}}
	vs, err := validateEnvImportComments(context.Background(), "0/import.yaml", []byte(yamlBody), SurfaceInputs{Plan: plan})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	for _, v := range vs {
		if v.Code == "env-yaml-fabricated-field-name" {
			if strings.Contains(v.Message, "config.json") || strings.Contains(v.Message, "vite.config.js") {
				t.Errorf("filename `%s` should not flag as fabricated yaml field: %+v", v.Message, v)
			}
		}
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
