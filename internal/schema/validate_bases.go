package schema

import "gopkg.in/yaml.v3"

// Embedded returns the schemas parsed from the committed embedded bytes (the
// deterministic floor). Exported so callers that have no live schema cache
// (recipe-authoring gates in the sim, tests) can still run base-existence
// checks against a stable, offline source. Never panics; may be nil only if
// the committed bytes are malformed (a build-time invariant).
func Embedded() *Schemas { return embeddedSchemas() }

// CheckZeropsBasesLive validates that every zerops[].build.base and
// zerops[].run.base value EXISTS in the given schema's enum lists. It is the
// "did the author hallucinate a base?" check, factored out of the embedded
// jsonschema so it can run against a LIVE (short-TTL) *Schemas instead of the
// frozen embedded enum — a brand-new-but-real base then passes, while a
// hallucinated one still rejects. Membership matches the embedded jsonschema's
// semantics exactly: build.base against the full build-base value set,
// run.base against the full run-base set.
//
// Structure (string-or-array shape, additionalProperties, required) is NOT
// checked here — that stays with ValidateZeropsYAMLStructure. Returns nil when
// schemas is unusable (caller should fall back to schema.Embedded()).
func CheckZeropsBasesLive(content string, schemas *Schemas) []ValidationError {
	if schemas == nil || schemas.ZeropsYml == nil {
		return nil
	}
	// Membership is equivalence-aware (HasBuildBase/HasRunBase) so a bare
	// authored base (`php@8.4`) matches a composite-only live enum
	// (`alpine/php@8.4`); a hallucinated base (wrong version/name) still
	// rejects. Plain set lookup here false-rejected every bare authored base
	// once the live fetch returned the curated composite-only schema.
	if len(schemas.ZeropsYml.BuildBases) == 0 || len(schemas.ZeropsYml.RunBases) == 0 {
		return nil
	}

	var doc map[string]any
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil // malformed yaml is a structural concern handled elsewhere
	}
	list, ok := doc["zerops"].([]any)
	if !ok {
		return nil
	}

	var errs []ValidationError
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		setupName, _ := entry["setup"].(string)

		if build, ok := entry["build"].(map[string]any); ok {
			for _, base := range baseValues(build["base"]) {
				if !schemas.HasBuildBase(base) {
					errs = append(errs, baseViolation(setupName, "build.base", base))
				}
			}
		}
		if run, ok := entry["run"].(map[string]any); ok {
			for _, base := range baseValues(run["base"]) {
				if !schemas.HasRunBase(base) {
					errs = append(errs, baseViolation(setupName, "run.base", base))
				}
			}
		}
	}
	return errs
}

// baseValues normalizes a base field that may be a single string or a list of
// strings into a slice of non-empty strings.
func baseValues(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func baseViolation(setup, field, value string) ValidationError {
	loc := field
	if setup != "" {
		loc = setup + " " + field
	}
	return ValidationError{
		Path:    loc,
		Message: "value " + quote(value) + " is not an available " + field + " on the platform",
	}
}

func quote(s string) string { return "\"" + s + "\"" }
