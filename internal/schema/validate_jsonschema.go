package schema

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
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

	// Publication diagnostics are populated by ImportValidator. Existing floor
	// validators keep their Path/Message contract.
	SchemaPath string
	Service    string
	Correction string
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

// ImportValidator validates against one cached publication, identified by its
// source URL and the SHA-256 of its canonical parsed JSON. It is immutable and
// safe for concurrent readers; it never substitutes the embedded floor.
type ImportValidator struct {
	SourceURL string
	Digest    string
	compiled  *jsonschema.Schema
	raw       map[string]any
}

func compileImportPublication(sourceURL string, raw map[string]any) (*ImportValidator, error) {
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode published import schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	// All authority is the fetched publication. An unknown external reference
	// makes validation unavailable instead of doing unbounded remote I/O here.
	compiler.LoadURL = func(url string) (io.ReadCloser, error) {
		return nil, fmt.Errorf("published schema reference %q is not cached", url)
	}
	if err := compiler.AddResource(sourceURL, bytes.NewReader(body)); err != nil {
		return nil, fmt.Errorf("read published import schema: %w", err)
	}
	compiled, err := compiler.Compile(sourceURL)
	if err != nil {
		return nil, fmt.Errorf("compile published import schema: %w", err)
	}
	return &ImportValidator{SourceURL: sourceURL, Digest: fmt.Sprintf("%x", sha256.Sum256(body)), compiled: compiled, raw: raw}, nil
}

// Validate returns schema findings with instance and schema JSON pointers,
// service identity when the offending instance belongs to a service, and a
// correction derived from that same schema. Obtain the validator through Cache.
func (v *ImportValidator) Validate(content string) []ValidationError {
	doc, errs := yamlToJSONForValidate(content)
	if errs != nil {
		for i := range errs {
			errs[i].Correction = "Correct the tier YAML syntax before proposing it."
		}
		return errs
	}
	if err := v.compiled.Validate(doc); err != nil {
		var schemaErr *jsonschema.ValidationError
		if !errors.As(err, &schemaErr) {
			return []ValidationError{{Message: err.Error(), Correction: "Correct the tier to satisfy the published import schema."}}
		}
		var findings []ValidationError
		for _, leaf := range collectValidationLeaves(schemaErr) {
			finding := ValidationError{Path: leaf.InstanceLocation, Message: leaf.Message, SchemaPath: leaf.KeywordLocation}
			if parts := strings.Split(strings.TrimPrefix(finding.Path, "/"), "/"); len(parts) >= 2 && parts[0] == "services" {
				if index, err := strconv.Atoi(parts[1]); err == nil {
					if services, ok := pointerValue(doc, "/services").([]any); ok && index >= 0 && index < len(services) {
						if service, ok := services[index].(map[string]any); ok {
							finding.Service, _ = service["hostname"].(string)
						}
					}
				}
			}
			finding.Correction = v.correction(doc, finding)
			findings = append(findings, finding)
		}
		return findings
	}
	return nil
}

func (v *ImportValidator) correction(doc any, failure ValidationError) string {
	correction := "Correct " + failure.Path + " to satisfy " + failure.SchemaPath + "."
	if !strings.HasSuffix(failure.SchemaPath, "/additionalProperties") {
		return correction
	}
	object, ok := pointerValue(doc, failure.Path).(map[string]any)
	if !ok {
		return correction
	}
	parent := strings.TrimSuffix(failure.SchemaPath, "/additionalProperties")
	properties, ok := pointerValue(v.raw, parent+"/properties").(map[string]any)
	if !ok {
		return correction
	}
	var actions []string
	for field := range object {
		if _, allowed := properties[field]; allowed {
			continue
		}
		paths := nestedPropertyPaths(properties, field, "")
		if len(paths) == 1 {
			actions = append(actions, "Move "+field+" to "+paths[0]+".")
		} else {
			actions = append(actions, "Remove unsupported field "+field+".")
		}
	}
	sort.Strings(actions)
	if len(actions) > 0 {
		return strings.Join(actions, " ")
	}
	return correction
}

// nestedPropertyPaths follows schema properties, never a zcp field list.
func nestedPropertyPaths(properties map[string]any, field, prefix string) []string {
	var paths []string
	for name, value := range properties {
		node, ok := value.(map[string]any)
		if !ok {
			continue
		}
		path := prefix + name
		if name == field {
			paths = append(paths, path)
		}
		if children, ok := node["properties"].(map[string]any); ok {
			paths = append(paths, nestedPropertyPaths(children, field, path+".")...)
		}
	}
	return paths
}

func pointerValue(doc any, pointer string) any {
	if pointer == "" {
		return doc
	}
	current := doc
	for encoded := range strings.SplitSeq(strings.TrimPrefix(pointer, "/"), "/") {
		part := strings.ReplaceAll(strings.ReplaceAll(encoded, "~1", "/"), "~0", "~")
		switch value := current.(type) {
		case map[string]any:
			current = value[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) {
				return nil
			}
			current = value[index]
		default:
			return nil
		}
	}
	return current
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
		return nil, []ValidationError{{Message: fmt.Sprintf("yaml content is empty at read time (raw bytes received=%d)", len(content))}}
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
