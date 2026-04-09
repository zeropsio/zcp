package schema

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// ValidFields holds the set of valid property names at each zerops.yaml level.
// Extracted from the live JSON schema so validation stays current without code changes.
type ValidFields struct {
	Setup  map[string]bool // top-level entry: setup, build, deploy, run, extends
	Build  map[string]bool // build section fields
	Deploy map[string]bool // deploy section fields
	Run    map[string]bool // run section fields
}

// FieldError describes an unknown field found in zerops.yaml.
type FieldError struct {
	Setup   string // setup name (e.g., "prod")
	Section string // "build", "deploy", "run", or "" for top-level
	Field   string // the unknown field name
}

// Error returns a human-readable description.
func (e FieldError) Error() string {
	if e.Section == "" {
		return fmt.Sprintf("setup %q: unknown top-level field %q", e.Setup, e.Field)
	}
	return fmt.Sprintf("setup %q: unknown field %q under %s", e.Setup, e.Field, e.Section)
}

// ExtractValidFields extracts valid property names from the zerops.yaml JSON schema.
// Returns nil if the schema is nil or has no raw data.
func ExtractValidFields(s *ZeropsYmlSchema) *ValidFields {
	if s == nil || s.Raw == nil {
		return nil
	}

	setup := navigatePath(s.Raw, "properties", "zerops", "items", "properties")
	if setup == nil {
		return nil
	}

	return &ValidFields{
		Setup:  propertyNames(setup),
		Build:  propertyNames(navigatePath(setup, "build", "properties")),
		Deploy: propertyNames(navigatePath(setup, "deploy", "properties")),
		Run:    propertyNames(navigatePath(setup, "run", "properties")),
	}
}

// ValidateZeropsYmlRaw checks for unknown fields in raw zerops.yaml content.
// Parses the YAML as untyped maps and validates field names against the schema.
// Returns nil if validFields is nil or content is unparseable.
func ValidateZeropsYmlRaw(yamlContent []byte, vf *ValidFields) []FieldError {
	if vf == nil {
		return nil
	}

	var doc map[string]any
	if err := yaml.Unmarshal(yamlContent, &doc); err != nil {
		return nil
	}

	zerops, ok := doc["zerops"].([]any)
	if !ok {
		return nil
	}

	var errs []FieldError
	for _, item := range zerops {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}

		setupName, _ := entry["setup"].(string)

		// Check top-level fields.
		for _, k := range sortedKeys(entry) {
			if !vf.Setup[k] {
				errs = append(errs, FieldError{Setup: setupName, Field: k})
			}
		}

		// Check build fields.
		if build, ok := entry["build"].(map[string]any); ok {
			for _, k := range sortedKeys(build) {
				if !vf.Build[k] {
					errs = append(errs, FieldError{Setup: setupName, Section: "build", Field: k})
				}
			}
		}

		// Check deploy fields.
		if deploy, ok := entry["deploy"].(map[string]any); ok {
			for _, k := range sortedKeys(deploy) {
				if !vf.Deploy[k] {
					errs = append(errs, FieldError{Setup: setupName, Section: "deploy", Field: k})
				}
			}
		}

		// Check run fields.
		if run, ok := entry["run"].(map[string]any); ok {
			for _, k := range sortedKeys(run) {
				if !vf.Run[k] {
					errs = append(errs, FieldError{Setup: setupName, Section: "run", Field: k})
				}
			}
		}
	}
	return errs
}

// propertyNames extracts keys from a JSON schema properties map.
func propertyNames(node map[string]any) map[string]bool {
	if node == nil {
		return nil
	}
	names := make(map[string]bool, len(node))
	for k := range node {
		names[k] = true
	}
	return names
}

// sortedKeys returns map keys in sorted order for deterministic error output.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
