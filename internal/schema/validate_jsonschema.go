package schema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

//go:embed testdata/import_yml_schema.json
var embeddedImportSchema []byte

//go:embed testdata/zerops_yml_schema.json
var embeddedZeropsSchema []byte

// ValidationError describes a single JSON Schema validation failure.
// Path is the JSON pointer to the offending field (e.g., "/services/0/buildFromGit").
// Message is the schema error from the validator. The struct deliberately
// stays minimal so callers can render it however suits their surface.
type ValidationError struct {
	Path    string
	Message string
}

// Error renders the violation in human-readable form for logs / errors.
func (v ValidationError) Error() string {
	if v.Path == "" {
		return v.Message
	}
	return fmt.Sprintf("%s: %s", v.Path, v.Message)
}

// compiledSchemas caches the parsed import + zerops.yaml schemas built from
// the embedded testdata. Compilation is deferred to first use so package
// init stays cheap; subsequent validations reuse the compiled trees.
var (
	compileOnce   sync.Once
	importSchema  *jsonschema.Schema
	zeropsSchema  *jsonschema.Schema
	compileErrors []error
)

// compileEmbeddedSchemas parses the two embedded JSON Schemas into
// jsonschema.Schema trees ready for Validate calls. Errors during
// compilation surface lazily on first call to ValidateImportYAML /
// ValidateZeropsYAML — the validator returns them as ValidationError
// pointing at the root with a "schema unavailable" message rather than
// panicking.
func compileEmbeddedSchemas() {
	compileOnce.Do(func() {
		c := jsonschema.NewCompiler()
		if err := c.AddResource("import.json", bytes.NewReader(embeddedImportSchema)); err != nil {
			compileErrors = append(compileErrors, fmt.Errorf("add embedded import schema: %w", err))
			return
		}
		if err := c.AddResource("zerops.json", bytes.NewReader(embeddedZeropsSchema)); err != nil {
			compileErrors = append(compileErrors, fmt.Errorf("add embedded zerops schema: %w", err))
			return
		}
		var err error
		if importSchema, err = c.Compile("import.json"); err != nil {
			compileErrors = append(compileErrors, fmt.Errorf("compile import schema: %w", err))
		}
		if zeropsSchema, err = c.Compile("zerops.json"); err != nil {
			compileErrors = append(compileErrors, fmt.Errorf("compile zerops schema: %w", err))
		}
	})
}

// ValidateImportYAML parses content as YAML, converts to a generic JSON
// shape, and validates against the embedded import-project-yml schema.
// Returns the slice of errors; empty slice on success. Empty content,
// parse failures, and schema-compile failures all surface as a single
// ValidationError so callers don't need branch-by-branch error handling.
//
// Per plan §6 Phase 5: the bundle's import.yaml MUST schema-validate
// before publish. Wired into ops.BuildBundle as a Phase B step that
// populates ExportBundle.Errors.
func ValidateImportYAML(content string) []ValidationError {
	compileEmbeddedSchemas()
	if len(compileErrors) > 0 {
		return []ValidationError{{Message: fmt.Sprintf("import schema compile failed: %v", compileErrors[0])}}
	}
	if importSchema == nil {
		return []ValidationError{{Message: "import schema unavailable"}}
	}
	doc, errs := yamlToJSONForValidate(content)
	if errs != nil {
		return errs
	}
	stripped := stripPreprocessorHeaderRoot(doc)
	if err := importSchema.Validate(stripped); err != nil {
		return formatJSONSchemaErrors(err)
	}
	return nil
}

// ValidateZeropsYAML parses content as YAML, validates against the
// embedded zerops-yml schema, AND when requiredSetup is non-empty
// confirms the body's `zerops[].setup` list contains that name. The
// setup-presence check is structural (already guarded by ops.BuildBundle's
// pre-flight), but pinning it again here lets Phase 5 surface a single
// errors slice for the agent without re-running the verifier.
func ValidateZeropsYAML(content string, requiredSetup string) []ValidationError {
	compileEmbeddedSchemas()
	if len(compileErrors) > 0 {
		return []ValidationError{{Message: fmt.Sprintf("zerops schema compile failed: %v", compileErrors[0])}}
	}
	if zeropsSchema == nil {
		return []ValidationError{{Message: "zerops schema unavailable"}}
	}
	doc, errs := yamlToJSONForValidate(content)
	if errs != nil {
		return errs
	}
	if err := zeropsSchema.Validate(doc); err != nil {
		errs = append(errs, formatJSONSchemaErrors(err)...)
	}
	if requiredSetup != "" {
		if missing := setupAbsentError(doc, requiredSetup); missing != nil {
			errs = append(errs, *missing)
		}
	}
	return errs
}

// yamlToJSONForValidate parses content as YAML and converts it to the
// nested map[string]any / []any shape the jsonschema validator expects.
// Round-tripping via JSON marshal/unmarshal coerces map[interface{}]interface{}
// (legacy yaml.v2 shape) and any other non-JSON-friendly types into
// validator-acceptable forms; yaml.v3 already returns map[string]any
// for object nodes, so the round-trip is mostly a no-op but keeps the
// guarantee uniform.
func yamlToJSONForValidate(content string) (any, []ValidationError) {
	if strings.TrimSpace(content) == "" {
		return nil, []ValidationError{{Message: "yaml content is empty"}}
	}
	var raw any
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return nil, []ValidationError{{Message: fmt.Sprintf("parse yaml: %v", err)}}
	}
	canonical, err := json.Marshal(raw)
	if err != nil {
		return nil, []ValidationError{{Message: fmt.Sprintf("normalize yaml→json: %v", err)}}
	}
	var doc any
	if err := json.Unmarshal(canonical, &doc); err != nil {
		return nil, []ValidationError{{Message: fmt.Sprintf("decode normalized json: %v", err)}}
	}
	return doc, nil
}

// stripPreprocessorHeaderRoot is a no-op placeholder — `#zeropsPreprocessor=on`
// is a YAML comment, so yaml.Unmarshal already drops it from the parsed
// document. The explicit wrapper is here as a documentation anchor in
// case future schema changes require structural stripping.
func stripPreprocessorHeaderRoot(doc any) any {
	return doc
}

// setupAbsentError walks a parsed zerops.yaml document and returns a
// ValidationError when no entry under `zerops[]` has `setup: <required>`.
// Returns nil when the setup is present (or when the document shape
// doesn't even reach the `zerops:` list — that's a schema-validate
// failure handled by the upstream Validate call).
func setupAbsentError(doc any, required string) *ValidationError {
	root, ok := doc.(map[string]any)
	if !ok {
		return nil
	}
	list, ok := root["zerops"].([]any)
	if !ok {
		return nil
	}
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := entry["setup"].(string); ok && name == required {
			return nil
		}
	}
	return &ValidationError{
		Path:    "/zerops",
		Message: fmt.Sprintf("required setup %q is not present", required),
	}
}

// formatJSONSchemaErrors flattens a *jsonschema.ValidationError tree
// into a slice of ValidationError. Each leaf cause becomes one entry;
// the top-level error becomes the first entry when no causes exist.
// Path is the JSON pointer the validator emits; Message is the error
// description.
func formatJSONSchemaErrors(err error) []ValidationError {
	var jsErr *jsonschema.ValidationError
	if !errors.As(err, &jsErr) {
		return []ValidationError{{Message: err.Error()}}
	}
	leaves := collectValidationLeaves(jsErr)
	if len(leaves) == 0 {
		return []ValidationError{{
			Path:    jsErr.InstanceLocation,
			Message: jsErr.Message,
		}}
	}
	out := make([]ValidationError, 0, len(leaves))
	for _, leaf := range leaves {
		out = append(out, ValidationError{
			Path:    leaf.InstanceLocation,
			Message: leaf.Message,
		})
	}
	return out
}

// collectValidationLeaves walks the jsonschema.ValidationError tree
// depth-first and returns the deepest causes. Internal-node errors are
// usually meta-summaries ("doesn't validate") that obscure the actual
// failing field; the leaves carry the actionable detail.
func collectValidationLeaves(err *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if err == nil {
		return nil
	}
	if len(err.Causes) == 0 {
		return []*jsonschema.ValidationError{err}
	}
	var out []*jsonschema.ValidationError
	for _, c := range err.Causes {
		out = append(out, collectValidationLeaves(c)...)
	}
	return out
}
