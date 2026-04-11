package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// TestCatalog_CoversAllMarkdownBlocks asserts that every <block name="...">
// tag in recipe.md has a corresponding entry in its section catalog, and
// vice versa — every catalog entry resolves to a block in the markdown.
//
// Initially (through Phase 3) the recipe.md has no block tags, so this
// test is trivially green. It starts enforcing as Phases 5/6/7 convert
// each section. The test name and behaviour stay stable across phases so
// that a regression — either an orphaned markdown block or an orphaned
// catalog entry — fails loudly.
func TestCatalog_CoversAllMarkdownBlocks(t *testing.T) {
	t.Parallel()
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe workflow: %v", err)
	}
	type sec struct {
		name    string
		catalog []sectionBlock
	}
	sections := []sec{
		{"research-minimal", recipeResearchBlocks},
		{"research-showcase", recipeResearchBlocks},
		{"provision", recipeProvisionBlocks},
		{"generate", recipeGenerateBlocks},
		{"deploy", recipeDeployBlocks},
		{"finalize", recipeFinalizeBlocks},
		{"close", recipeCloseBlocks},
	}
	for _, s := range sections {
		body := ExtractSection(md, s.name)
		if body == "" {
			continue
		}
		blocks := ExtractBlocks(body)
		inCatalog := make(map[string]bool, len(s.catalog))
		for _, cb := range s.catalog {
			inCatalog[cb.Name] = true
		}
		// Every markdown block must be in the catalog.
		inMarkdown := make(map[string]bool, len(blocks))
		for _, b := range blocks {
			if b.Name == "" {
				continue
			}
			inMarkdown[b.Name] = true
			if len(s.catalog) > 0 && !inCatalog[b.Name] {
				t.Errorf("section %q has <block name=%q> not in catalog", s.name, b.Name)
			}
		}
		// Every catalog entry must resolve to a markdown block (once the
		// catalog is non-empty).
		for _, cb := range s.catalog {
			if !inMarkdown[cb.Name] {
				t.Errorf("section %q catalog entry %q has no matching <block> in recipe.md", s.name, cb.Name)
			}
		}
	}
}
