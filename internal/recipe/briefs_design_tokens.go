package recipe

// BuildDesignTokenTable returns a compact inline summary of the Zerops
// Material 3 design system, suitable for splicing into the feature
// brief on showcase + UI variants. The full theme (per-framework
// component lineages, do/don't details, every typography size) lives
// at `zerops://themes/design-system` and is pulled on demand via
// `zerops_knowledge`. This composer carries only the load-bearing
// cross-recipe-consistency anchors so framework-specific feature work
// (laravel-showcase view layer, nestjs-showcase SPA panels) lands with
// the same color/type/radius vocabulary regardless of code shape.
//
// Byte budget: target ~500 bytes, hard ceiling 600 (pinned by
// TestBuildDesignTokenTable_EmitsCompactBytes). Any addition that
// pushes past the ceiling either condenses an existing line or moves
// to the on-demand spec.
//
// Design choice: this composer emits hand-curated prose against
// hardcoded values from internal/knowledge/themes/design-system.md
// frontmatter rather than parsing the YAML at compose time. The values
// are stable across every showcase recipe by construction (the brand
// is the brand), and a parser would add ~2 KB of yaml.v3 dependency
// surface for zero behavioral benefit. If the tokens drift, the table
// drifts here; the canonical-spec test
// TestStore_DesignSystemThemeIsFrameworkNeutral pins the same anchors
// from the spec body so the two stay in sync.
func BuildDesignTokenTable() string {
	return "## Design tokens (Zerops design system)\n\n" +
		"Full spec: `zerops://themes/design-system` (per-framework lineages, do/don't).\n\n" +
		"Colors:\n" +
		"- Material auto-flip: primary #00A49A on #FFFFFF, surface #F8F9FF on #161C25\n" +
		"- Identity static: teal #00CCBB, green #00CC55, tertiary #B8006B, error #BE0E14\n\n" +
		"Type: Roboto body, Geologica headlines, JetBrains Mono code.\n" +
		"Shapes: 12px cards, pill buttons, 24px card padding.\n\n" +
		"Avoid: hardcoded hex, Tailwind palette, `dark:` variants, drop shadows on product UI, purple gradients, AI-shimmer.\n"
}
