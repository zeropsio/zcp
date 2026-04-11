package workflow

import "strings"

// sectionBlock pairs a block name with a predicate. A nil predicate means
// "always include". The name must match a <block name="..."> tag in the
// corresponding section of recipe.md — TestCatalog_CoversAllMarkdownBlocks
// enforces this at build time.
type sectionBlock struct {
	Name      string
	Predicate func(*RecipePlan) bool
}

// Registered catalogs — filled in Phases 5a/6a/7a as each section is
// converted. Empty catalogs are a no-op: composeSection returns the raw
// section body verbatim, so unconverted sections still work.
var (
	recipeResearchBlocks  []sectionBlock
	recipeProvisionBlocks []sectionBlock
	recipeGenerateBlocks  []sectionBlock
	recipeDeployBlocks    []sectionBlock
	recipeFinalizeBlocks  []sectionBlock
	recipeCloseBlocks     []sectionBlock
)

// composeSection takes the raw body of a <section> and a catalog, extracts
// its <block> children, filters by predicate, and returns the composed
// body. If the catalog is empty, the raw body is returned unchanged — so
// callers can route every section through composeSection without breaking
// unconverted sections.
//
// Composition order is strictly the catalog order, with the preamble (if
// present) always first. Blocks whose predicate returns false are silently
// dropped. Blocks in the markdown that don't appear in the catalog are
// also dropped — the catalog-coverage test prevents this happening
// accidentally.
//
// Consumed by Phase 5/6/7 of the recipe size-reduction refactor (each
// section conversion routes through this function once its catalog is
// populated).
//
//nolint:unused // Infrastructure for Phases 5-7 (docs/implementation-recipe-size-reduction.md).
func composeSection(sectionBody string, catalog []sectionBlock, plan *RecipePlan) string {
	if len(catalog) == 0 {
		return sectionBody
	}
	blocks := ExtractBlocks(sectionBody)
	byName := make(map[string]string, len(blocks))
	for _, b := range blocks {
		byName[b.Name] = b.Body
	}
	var out []string
	if preamble, ok := byName[""]; ok && preamble != "" {
		out = append(out, preamble)
	}
	for _, sb := range catalog {
		body, ok := byName[sb.Name]
		if !ok || body == "" {
			continue
		}
		if sb.Predicate != nil && !sb.Predicate(plan) {
			continue
		}
		out = append(out, body)
	}
	return strings.Join(out, "\n\n")
}
