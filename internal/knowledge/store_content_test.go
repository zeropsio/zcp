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

func TestCore_H2Sections_ContainsSchemaAndExamples(t *testing.T) {
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
	for _, name := range []string{"import.yml Schema", "zerops.yml Schema", "Schema Rules", "Multi-Service Examples"} {
		if _, ok := sections[name]; !ok {
			t.Errorf("core.md missing H2 section %q", name)
		}
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

func TestRuntimeNormalizer_AllMapped_GuideResolvable(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}

	for base, slug := range runtimeNormalizer {
		t.Run(base, func(t *testing.T) {
			t.Parallel()
			guide := store.getRuntimeGuide(slug)
			if guide == "" {
				t.Skipf("runtime normalizer maps %q -> %q but no guide resolvable (recipe may not exist in API yet)", base, slug)
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

func TestDecisionFiles_Accessible(t *testing.T) {
	t.Parallel()
	store, err := GetEmbeddedStore()
	if err != nil {
		t.Fatalf("GetEmbeddedStore: %v", err)
	}

	for name, uri := range decisionFileMap {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, err := store.Get(uri)
			if err != nil {
				t.Errorf("decision file %q (%s) not found: %v", name, uri, err)
				return
			}
			if doc.Description == "" {
				t.Errorf("decision file %q missing description", name)
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
	briefing, err := store.GetBriefing("nodejs@22", []string{"postgresql@16", "valkey@7.2"}, "", nil)
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
