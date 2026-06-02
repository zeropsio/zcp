package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Structure-only validation for export/launch.
//
// Export and launch bundles carry service types + build/run bases that come
// from a live Discover of the user's RUNNING services (or a refreshed launch
// plan) — they are platform-sourced and therefore already valid; the platform
// re-validates them authoritatively at re-import. Re-checking those values
// against the binary's frozen enum is a redundant gate that can only produce
// false-negatives (a type/base newer than the embedded snapshot would reject a
// bundle the platform already runs).
//
// So export/launch validate against a STRUCTURE-ONLY schema: the embedded
// schema with the value-membership enums removed at exactly the three
// volatile nodes (services[].type, zerops[].build.base, zerops[].run.base)
// while every structural guard is preserved — additionalProperties:false
// (field-typo catch), required, the stable enums (corePackage / location /
// objectStoragePolicy / cpuMode), and the build.base string-or-array TYPE
// contract. A non-string base or a typo'd field still rejects.
//
// We strip by node-path (not by post-filtering jsonschema errors): the
// build.base oneOf{string,array} otherwise emits both an enum error and an
// "expected array" alternate-branch error at the same instance path, and an
// array item emits its enum error at a sub-path — neither path- nor
// message-filtering survives that error tree cleanly. Removing the enum from
// the compiled schema sidesteps it entirely.

var (
	structureCompileOnce   sync.Once
	importStructureSchema  *jsonschema.Schema
	zeropsStructureSchema  *jsonschema.Schema
	structureCompileErrors []error
)

// compileStructureSchemas builds the structure-only variants of the two
// embedded schemas once, on first use. Errors surface lazily as a single
// ValidationError from the validators, mirroring compileEmbeddedSchemas.
func compileStructureSchemas() {
	structureCompileOnce.Do(func() {
		importBytes, err := stripVolatileEnums(embeddedImportSchema, stripImportEnums)
		if err != nil {
			structureCompileErrors = append(structureCompileErrors, fmt.Errorf("strip import enums: %w", err))
			return
		}
		zeropsBytes, err := stripVolatileEnums(embeddedZeropsSchema, stripZeropsEnums)
		if err != nil {
			structureCompileErrors = append(structureCompileErrors, fmt.Errorf("strip zerops enums: %w", err))
			return
		}

		c := jsonschema.NewCompiler()
		if err := c.AddResource("import-structure.json", bytes.NewReader(importBytes)); err != nil {
			structureCompileErrors = append(structureCompileErrors, fmt.Errorf("add import structure schema: %w", err))
			return
		}
		if err := c.AddResource("zerops-structure.json", bytes.NewReader(zeropsBytes)); err != nil {
			structureCompileErrors = append(structureCompileErrors, fmt.Errorf("add zerops structure schema: %w", err))
			return
		}
		if importStructureSchema, err = c.Compile("import-structure.json"); err != nil {
			structureCompileErrors = append(structureCompileErrors, fmt.Errorf("compile import structure schema: %w", err))
		}
		if zeropsStructureSchema, err = c.Compile("zerops-structure.json"); err != nil {
			structureCompileErrors = append(structureCompileErrors, fmt.Errorf("compile zerops structure schema: %w", err))
		}
	})
}

// stripVolatileEnums unmarshals a JSON schema, applies a node-path-precise
// enum-stripping transform, and re-marshals it. The transform deletes only
// the value-membership enum at the volatile nodes; the surrounding `type`
// (and `oneOf` shape for build.base) is preserved so the structure contract
// still validates.
func stripVolatileEnums(raw []byte, transform func(map[string]any)) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal schema: %w", err)
	}
	transform(doc)
	return json.Marshal(doc)
}

// stripImportEnums removes the services[].type enum (keeping type:string and
// the conditional allOf[].if discriminators, which simply do not fire for an
// unknown type).
func stripImportEnums(doc map[string]any) {
	typeNode := navigatePath(doc, "properties", "services", "items", "properties", "type")
	delete(typeNode, "enum")
}

// stripZeropsEnums removes the run.base enum and the build.base oneOf enums
// (string branch + array-items branch), preserving every type contract.
func stripZeropsEnums(doc map[string]any) {
	setup := navigatePath(doc, "properties", "zerops", "items", "properties")
	if setup == nil {
		return
	}

	runBase := navigatePath(setup, "run", "properties", "base")
	delete(runBase, "enum")

	buildBase := navigatePath(setup, "build", "properties", "base")
	if buildBase == nil {
		return
	}
	oneOf, _ := buildBase["oneOf"].([]any)
	for _, branch := range oneOf {
		b, ok := branch.(map[string]any)
		if !ok {
			continue
		}
		delete(b, "enum")
		if items, ok := b["items"].(map[string]any); ok {
			delete(items, "enum")
		}
	}
}

// ValidateImportYAMLStructure validates import.yaml against the structure-only
// schema (service-type enum removed). Used by export/launch where types are
// platform-sourced. Same error shape as ValidateImportYAML.
func ValidateImportYAMLStructure(content string) []ValidationError {
	compileStructureSchemas()
	if len(structureCompileErrors) > 0 {
		return []ValidationError{{Message: fmt.Sprintf("import structure schema compile failed: %v", structureCompileErrors[0])}}
	}
	if importStructureSchema == nil {
		return []ValidationError{{Message: "import structure schema unavailable"}}
	}
	doc, errs := yamlToJSONForValidate(content)
	if errs != nil {
		return errs
	}
	stripped := stripPreprocessorHeaderRoot(doc)
	if err := importStructureSchema.Validate(stripped); err != nil {
		return formatJSONSchemaErrors(err)
	}
	return nil
}

// ValidateZeropsYAMLStructure validates zerops.yaml against the structure-only
// schema (build/run base enums removed) and, when requiredSetup is non-empty,
// confirms the setup is present. Used by export where the zerops.yaml body is
// the user's committed, already-deployed file.
func ValidateZeropsYAMLStructure(content string, requiredSetup string) []ValidationError {
	compileStructureSchemas()
	if len(structureCompileErrors) > 0 {
		return []ValidationError{{Message: fmt.Sprintf("zerops structure schema compile failed: %v", structureCompileErrors[0])}}
	}
	if zeropsStructureSchema == nil {
		return []ValidationError{{Message: "zerops structure schema unavailable"}}
	}
	doc, errs := yamlToJSONForValidate(content)
	if errs != nil {
		return errs
	}
	if err := zeropsStructureSchema.Validate(doc); err != nil {
		errs = append(errs, formatJSONSchemaErrors(err)...)
	}
	if requiredSetup != "" {
		if missing := setupAbsentError(doc, requiredSetup); missing != nil {
			errs = append(errs, *missing)
		}
	}
	return errs
}
