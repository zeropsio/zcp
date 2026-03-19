// Tests for: embedded content structure contracts — validate assumptions about markdown structure.
package knowledge

import (
	"strings"
	"testing"
)

func TestCore_H2Sections_ContainsImportYmlSchema(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	if _, ok := sections["import.yml Schema"]; !ok {
		t.Error("core.md missing H2 section 'import.yml Schema'")
	}
}

func TestCore_H2Sections_ContainsZeropsYmlSchema(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	if _, ok := sections["zerops.yml Schema"]; !ok {
		t.Error("core.md missing H2 section 'zerops.yml Schema'")
	}
}

func TestCore_H2Sections_ContainsRulesAndPitfalls(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	if _, ok := sections["Rules & Pitfalls"]; !ok {
		t.Error("core.md missing H2 section 'Rules & Pitfalls'")
	}
}

func TestCore_H2Sections_ContainsSchemaRules(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	if _, ok := sections["Schema Rules"]; !ok {
		t.Error("core.md missing H2 section 'Schema Rules'")
	}
}

func TestCore_ImportYmlSchema_ContainsPreprocessorFunctions(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	importSchema, ok := sections["import.yml Schema"]
	if !ok {
		t.Fatal("core.md missing H2 section 'import.yml Schema'")
	}
	if !strings.Contains(importSchema, "Preprocessor Functions") {
		t.Error("'import.yml Schema' section should contain 'Preprocessor Functions' as H3 subsection")
	}
}

func TestCore_SchemaRules_ContainsDeployFilesAndTilde(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/core")
	if err != nil {
		t.Fatalf("Get core: %v", err)
	}
	sections := doc.H2Sections()
	rules, ok := sections["Schema Rules"]
	if !ok {
		t.Fatal("core.md missing H2 section 'Schema Rules'")
	}
	if !strings.Contains(rules, "deployFiles") {
		t.Error("'Schema Rules' should mention 'deployFiles'")
	}
}

func TestRuntimeNormalizer_AllMapped_FilesExist(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}

	for base, slug := range runtimeNormalizer {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			uri := "zerops://runtimes/" + slug
			if _, err := store.Get(uri); err != nil {
				t.Errorf("runtime normalizer maps %q -> %q but document %q not found", base, slug, uri)
			}
		})
	}
}

func TestServices_H2Sections_ContainsWiringSyntax(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/services")
	if err != nil {
		t.Fatalf("Get services: %v", err)
	}
	sections := doc.H2Sections()
	if _, ok := sections["Wiring Syntax"]; !ok {
		t.Error("services.md missing H2 section 'Wiring Syntax'")
	}
}

func TestServices_H2Sections_ContainsAllNormalizedServices(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/services")
	if err != nil {
		t.Fatalf("Get services: %v", err)
	}
	sections := doc.H2Sections()

	for base, normalized := range serviceNormalizer {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			if _, ok := sections[normalized]; !ok {
				t.Errorf("services.md missing H2 section %q (mapped from %q)", normalized, base)
			}
		})
	}
}

func TestOperations_DecisionSections_Accessible(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	doc, err := store.Get("zerops://themes/operations")
	if err != nil {
		t.Fatalf("Get operations: %v", err)
	}
	h2 := doc.H2Sections()
	decisions, ok := h2["Service Selection Decisions"]
	if !ok {
		t.Fatal("operations.md missing H2 section 'Service Selection Decisions'")
	}
	h3 := parseH3Sections(decisions)

	for _, name := range []string{"Choose Database", "Choose Cache", "Choose Queue", "Choose Search", "Choose Runtime Base"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, ok := h3[name]; !ok {
				t.Errorf("operations.md 'Service Selection Decisions' missing H3 subsection %q", name)
			}
		})
	}
}

func TestGetBriefing_RealisticStack_SizeReasonable(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}
	briefing, err := store.GetBriefing("nodejs@22", []string{"postgresql@16", "valkey@7.2"}, nil)
	if err != nil {
		t.Fatalf("GetBriefing: %v", err)
	}
	// Briefing should be substantial but not enormous.
	if len(briefing) < 500 {
		t.Errorf("briefing too small (%d chars), expected >500 for realistic stack", len(briefing))
	}
	if len(briefing) > 100000 {
		t.Errorf("briefing too large (%d chars), expected <100000", len(briefing))
	}
}
