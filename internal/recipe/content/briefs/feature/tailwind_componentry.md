# Tailwind + componentry (frontend pass)

You are the frontend feature sub-agent. Your scope is the SPA codebase
(or the view-rendering codebase of a monolith). Every panel you ship
adopts the Zerops design tokens — the same color, type, and shape
vocabulary across every showcase recipe regardless of framework
binding.

The full design-system spec (per-framework component lineages,
do/don't details, every typography size) lives at
`zerops://themes/design-system` and is fetched on demand via
`zerops_knowledge uri=zerops://themes/design-system`. This atom carries
the load-bearing Tailwind composition rules so the agent doesn't
improvise hex codes or palette names.

## Tailwind class composition for the design tokens

Every token from the design-system theme has a Tailwind utility shape.
Author panels with these utilities, NOT with the default Tailwind
palette (no `bg-blue-600`, no `text-purple-500`, no `dark:` variants).

| Token                    | Tailwind class shape             | When to use |
|--------------------------|----------------------------------|-------------|
| `--zerops-primary`       | `bg-[var(--zerops-primary)]`     | Primary action buttons, primary nav |
| `--zerops-teal`          | `bg-[var(--zerops-teal)]`        | Brand identity surfaces (logos, hero) |
| `--zerops-radius-card`   | `rounded-[12px]`                 | Cards, panels, modal containers |
| `--zerops-font-head`     | `font-[Geologica]`               | Headlines, page titles |
| `--zerops-font-body`     | `font-[Roboto]`                  | Body copy, labels |
| `--zerops-font-mono`     | `font-[JetBrains_Mono]`          | Code blocks, inline code |

Configure the tokens once in `src/styles.css` (or `app.css`) at the SPA
root, then reference via the CSS variable shape above. Do NOT hardcode
hex values in component files.

## shadcn/ui + Radix + MUI integration

When the recipe scaffolds ship a component library:

- **shadcn/ui** — install via `npx shadcn-ui@latest add <component>`;
  shadcn drops a tailwind-friendly component into `src/components/ui/`.
  Override its color tokens by editing the component's class list to
  use `bg-[var(--zerops-primary)]` etc. instead of `bg-primary`.
- **Radix UI** — Radix is unstyled by design; wire each Radix primitive
  through the same Tailwind utility shapes. The Radix `Dialog.Content`
  + `Card` combination is the canonical panel container.
- **MUI** — MUI's theme system overrides the design tokens at the
  `ThemeProvider` boundary. Pass the Zerops palette to
  `createTheme({ palette: { primary: { main: '#00A49A' } } })` so MUI
  components render with the recipe's color vocabulary.

## Don't hardcode hex; adapt-path examples for non-Tailwind stacks

For CSS-in-JS (Emotion, styled-components):

```js
const Button = styled.button`
  background: var(--zerops-primary);
  border-radius: var(--zerops-radius-card);
`;
```

For vanilla CSS / Sass:

```css
.panel {
  background: var(--zerops-primary);
  border-radius: var(--zerops-radius-card);
}
```

The token names are stable across stacks; the syntax for referencing
them adapts to the binding. NEVER inline `#00A49A` — when the design
system updates the token, every recipe re-imports the new value
automatically; hardcoded hex breaks the cross-recipe consistency
contract.

## What NOT to do

- Don't introduce purple gradients, drop shadows on product UI, or
  AI-shimmer effects (those are explicitly excluded from the
  design-system theme).
- Don't add `dark:` variant classes — Material auto-flip handles light
  vs dark via the `--zerops-primary-on` token, which is already
  contrast-correct on either background.
- Don't ship Tailwind palette colors (`bg-blue-500`, `text-red-700`).
  The design tokens are the only sanctioned color vocabulary.
