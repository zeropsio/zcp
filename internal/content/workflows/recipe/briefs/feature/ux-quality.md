# ux-quality

The dashboard is the observable surface every downstream verification reads — sweep, browser walk, close-step review. Polished output is part of the work, not a late-pass concern.

## Contract discipline (verify what's installed)

- Every package your imports reference must appear in the codebase's lockfile at the version `package.json` (or the language-equivalent manifest) declares. When installing a new package, run the install command over SSH, then Read the lockfile once to confirm the resolution before writing imports that depend on the version's API shape.
- When `buildCommands` in the recipe's `zerops.yaml` compile assets (JS / CSS), the primary view / template loads the compiled outputs through the framework's standard asset-inclusion mechanism. Inline `<style>` or `<script>` blocks that bypass the build output are forbidden; a build step producing assets nobody loads is dead code.
- Pin new packages from the same framework family to the major version the framework's core package declares in the lockfile.

## Dashboard UX standards (positive form)

- Every feature reachable from the dashboard at `<element data-feature="{F.uiTestId}">` on its outer wrapper.
- Every section has a heading, a short description, and a styled body.
- Styled form controls: padding, border-radius, consistent sizing, focus ring, button hover. Use the scaffolded CSS tokens; avoid browser defaults.
- Visual hierarchy: headings delineated, consistent vertical rhythm, tables with headers + cell padding + alternating row shading.
- Status feedback: success / error flash after submissions, loading text for async operations, meaningful empty states.
- Readable data: aligned columns, humanized timestamps (`new Date(iso).toLocaleString()`), monospace for identifiers.
- System font stack, generous whitespace, monochrome palette plus one accent, mobile-responsive via the existing grid.
- Dynamic content rendered via the framework's auto-escaping template expression; never raw-HTML output on user-influenced data.
- Modern framework runes / reactive patterns (Svelte 5 runes, React hooks, Vue Composition API) — no legacy syntax the current major deprecates.

## Loud-failure surface (positive form)

Every async section explicitly handles the four render states — loading, error, empty, populated. The error state is visible (red banner / toast in the `[data-error]` slot with the error message), never silently folded into the empty state. Every fetch wrapper checks `res.ok` before calling `.json()`; every JSON path verifies the response content-type contains `application/json`; every array-consuming store declares a `[]` default and parses defensively (`Array.isArray(data.items) ? data.items : []`). Init-phase scripts (seed, migrate, search-sync) must throw / exit non-zero on any side-effect failure; exit 0 is a promise that every declared side effect is durable.
