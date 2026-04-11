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
	recipeResearchBlocks []sectionBlock

	recipeProvisionBlocks []sectionBlock

	// recipeGenerateBlocks — populated in Phase 5a (mechanical wrap, all
	// predicates nil: every block emits, zero behavior change). Phase 5b
	// switches predicates to real values (see recipe_plan_predicates.go)
	// to gate dual-runtime / showcase / bundler content.
	recipeGenerateBlocks = []sectionBlock{
		{Name: "container-state"},
		{Name: "where-to-write-files-single"},
		{Name: "where-to-write-files-multi"},
		{Name: "what-to-generate-showcase"},
		{Name: "two-kinds-of-import-yaml"},
		{Name: "execution-order"},
		{Name: "zerops-yaml-header"},
		{Name: "dual-runtime-url-shapes"},
		{Name: "dual-runtime-consumption"},
		{Name: "project-env-vars-pointer"},
		{Name: "dual-runtime-what-not-to-do"},
		{Name: "setup-dev-rules"},
		{Name: "serve-only-dev-override"},
		{Name: "dev-dep-preinstall"},
		{Name: "dev-server-host-check"},
		{Name: "setup-prod-rules"},
		{Name: "worker-setup-block"},
		{Name: "shared-across-setups"},
		{Name: "env-example-preservation"},
		{Name: "framework-env-conventions"},
		{Name: "dashboard-skeleton"},
		{Name: "asset-pipeline-consistency"},
		{Name: "readme-with-fragments"},
		{Name: "code-quality"},
		{Name: "pre-deploy-checklist"},
		{Name: "completion"},
	}

	recipeDeployBlocks []sectionBlock

	recipeFinalizeBlocks []sectionBlock

	recipeCloseBlocks []sectionBlock
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
