---
version: alpha
name: Zerops
description: >-
  Developer-first cloud platform. Confident teal on near-black, Material 3
  tokens, dense and content-led. Closer to a well-lit terminal than a SaaS
  marketing page.

colors:
  # Identity (static, never theme-flips)
  identity-zerops-green: "#00CCBB"
  identity-purple: "#CC0077"
  identity-pink: "#BB00CC"
  identity-green: "#00CC55"
  identity-red: "#CC0011"

  # Material 3 — primary (teal seed #00CCBB)
  primary: "#00A49A"
  on-primary: "#FFFFFF"
  primary-container: "#9CF2E5"
  on-primary-container: "#00352F"

  # Material 3 — secondary (#3477C5)
  secondary: "#0B5FAC"
  on-secondary: "#FFFFFF"
  secondary-container: "#D4E3FF"

  # Material 3 — tertiary (#CC0077)
  tertiary: "#B8006B"
  on-tertiary: "#FFFFFF"
  tertiary-container: "#FFD9E4"

  # Surface & neutral
  surface: "#F8F9FF"
  on-surface: "#161C25"
  on-surface-variant: "#3B4A47"
  surface-container-low: "#EBF1FE"
  surface-container: "#DDE3EF"
  surface-container-high: "#D4DAE7"
  outline: "#6B7A77"
  outline-variant: "#BACAC6"

  # Error
  error: "#BE0E14"
  on-error: "#FFFFFF"
  error-container: "#FFDAD5"

  # Backgrounds & cards
  background-light: "#F0F0F0"
  background-dark: "#00100F"
  card-bg-light: "#F3F5F7"
  card-bg-dark: "#1A1F2E"
  code-block-bg: "#171717"

  # Status
  status-running: "{colors.identity-green}"
  status-warning: "{colors.tertiary}"
  status-error: "{colors.error}"

  # Metric series (categorical, hue-separated)
  metric-cpu: "#2196F3"
  metric-ram: "#009688"
  metric-disc: "#E91E63"
  metric-ramdisk: "#8BC34A"

typography:
  display-lg:   { fontFamily: Roboto,    fontSize: 57px, fontWeight: 400, lineHeight: 64px }
  headline-lg:  { fontFamily: Geologica, fontSize: 32px, fontWeight: 500, lineHeight: 40px }
  headline-md:  { fontFamily: Geologica, fontSize: 24px, fontWeight: 500, lineHeight: 32px }
  title-md:     { fontFamily: Roboto,    fontSize: 16px, fontWeight: 500, lineHeight: 24px }
  body-lg:      { fontFamily: Roboto,    fontSize: 16px, fontWeight: 400, lineHeight: 24px }
  body-md:      { fontFamily: Roboto,    fontSize: 14px, fontWeight: 400, lineHeight: 20px }
  body-sm:      { fontFamily: Roboto,    fontSize: 12px, fontWeight: 400, lineHeight: 16px }
  label-lg:     { fontFamily: Roboto,    fontSize: 14px, fontWeight: 500, lineHeight: 20px }
  label-md:     { fontFamily: Roboto,    fontSize: 12px, fontWeight: 500, lineHeight: 16px }
  code:         { fontFamily: "JetBrains Mono", fontSize: 13px, fontWeight: 400, lineHeight: 20px }
  editorial:    { fontFamily: "Instrument Serif", fontSize: 32px, fontWeight: 400, lineHeight: 40px }

rounded:
  none: 0px
  sm: 4px
  md: 8px
  lg: 12px   # canonical card radius
  xl: 20px
  full: 9999px

spacing: { 1: 4px, 2: 8px, 3: 12px, 4: 16px, 6: 24px, 8: 32px, 12: 48px, 16: 64px, 24: 96px }

components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.label-lg}"
    rounded: "{rounded.full}"
    height: 40px
    padding: 0 24px
  card-base:
    backgroundColor: "{colors.card-bg-light}"
    rounded: "{rounded.lg}"
    padding: 24px
  input-field:
    backgroundColor: "{colors.surface-container-high}"
    typography: "{typography.body-lg}"
    rounded: "{rounded.sm}"
    height: 56px
    padding: 8px 16px
  status-pill:
    typography: "{typography.label-md}"
    rounded: "{rounded.full}"
    padding: 2px 10px
  code-block:
    backgroundColor: "{colors.code-block-bg}"
    textColor: "#FFFFFF"
    typography: "{typography.code}"
    rounded: "{rounded.md}"
    padding: 16px
---

# Zerops Design System

## Overview

Zerops is a developer-first cloud platform. The visual identity must read as **technically credible** (engineers are the audience) and **quietly confident** (no purple gradients, no AI-shimmer). The product is the magic — the UI gets out of the way.

ZCP, agent environments, and recipes are framed as **one control plane viewed from different angles** — never as separate products.

This spec is **framework-neutral**. Every showcase recipe — Laravel monoliths, NestJS+SPA splits, Rails, Django, future frameworks — lands with the same Material 3 token language regardless of code shape. The token contract is identical; the per-component class/element name is whatever is idiomatic for the framework you ship.

## Colors

Three layers, used in this order:

1. **Material 3 tokens** (`primary`, `on-surface`, `surface-container`) — auto-flip on `color-scheme`. Cover ~80% of UI.
2. **Identity tokens** (`identity-*`) — static brand swatches, never theme-flip. For brand marks and status pills where meaning must survive dark/light.
3. **Functional tokens** (`status-*`, `metric-*`) — runtime state and chart series.

Hardcoded hex (`bg-[#00ccbb]`, `bg-teal-500`, `text-gray-700`) is always wrong.

Brand seeds: primary `#00CCBB` · secondary `#3477C5` · tertiary `#CC0077` · error `#D42422`. Dark mode is driven by `color-scheme` on `<html>` plus `.dark-mode` / `.light-mode` overrides.

## Typography

- **Roboto** — body, labels, dense data
- **Geologica** — display, headlines, marketing
- **JetBrains Mono** — code, env vars, infra IDs (always)
- **Instrument Serif** — editorial moments only, never inside product UI

Signature pairing: `headline-lg` (Geologica) over `body-lg` (Roboto). Geometric sans + transitional serif is the strongest brand mark in the type system.

## Layout

Base spacing unit **4px** (`--spacing: 0.25rem`). Card padding `24px`. Marketing section gutter `64–96px`. Dense data gap `8px`.

Breakpoints: `m` 810px, `l` 1440px. The mid breakpoint is intentionally late — product UI is data-dense and benefits from horizontal room.

**Rule:** raw utility classes for layout, semantic tokens for color. Tokens flow through whatever theme-extension mechanism the framework provides (Tailwind config, CSS variables, Material `--mdc-*`).

```html
<div class="flex gap-4 p-6">
  <span class="text-on-surface">…</span>
  <button class="bg-primary">…</button>
</div>
```

## Elevation & Depth

Shallow system. Depth comes from **surface tone** (`surface-container-low` → `surface-container-high`), not shadow. In product UI, prefer `outline-variant` over shadow — shadow on a dense dashboard is noise.

Shadow is reserved for pop-overs/dialogs (Material defaults) and marketing recipe cards (a five-stop layered stack — soft, photographic, not computational).

## Shapes

| Token | Use |
|---|---|
| `rounded.sm` (4px) | Inputs, chips |
| `rounded.md` (8px) | Code blocks |
| `rounded.lg` (12px) | **Canonical card radius** |
| `rounded.xl` (20px) | Hero / recipe cards |
| `rounded.full` | Buttons, status pills, avatars |

Buttons are pill (Material 3); content surfaces are 12px. That contrast is intentional.

## Components

Built on a token-first Material 3 design language; framework binding is the codebase's choice. The token block above captures Zerops-specific values; component implementation maps to whatever the framework idiomatically provides.

Principles that hold regardless of framework:

- Primary actions = **filled solid button** (the framework's most-emphasis variant). Never an outlined-only button as the primary CTA.
- Cards use the framework's theme-aware card background variable (resolves correctly across light/dark), not a raw token literal.
- Code blocks stay dark in light mode. Syntax contrast > surface harmony.
- Status pills use `identity-*` directly so "running = green" survives theme flips.
- App bar sits on `background-dark` in both themes — navigation is a constant anchor.
- Use the framework's component-token override API (Material `--mdc-*` CSS variables, Tailwind `data-*` attributes, shadcn CSS variables, Bootstrap utility classes — whatever the framework provides). Never stack `!important` rules.
- Scoped re-skins via wrapper class: `<div class="amber">`, `<div class="sky">`, `<div class="green">`.

### Per-framework component lineage

The same tokens project onto different framework component vocabularies. Pick the lineage your codebase ships:

- **Angular + Material 3**: `mat-flat-button` (primary), `mat-card` (12px override), `mat-form-field` (4px override). Override via `--mdc-*` CSS variables.
- **React/Vue + Tailwind**: `<button class="bg-primary text-on-primary rounded-full px-6 h-10">`. Apply tokens via Tailwind theme extension (`tailwind.config.js`) — never hardcode hex.
- **React/Vue + shadcn or Radix**: extend the default theme via CSS variables (`--primary`, `--card-bg`, `--radius-card`). Match the token names from the table above.
- **Laravel Blade + Tailwind**: same as React/Vue + Tailwind. Tokens flow through `tailwind.config.js`; Blade components consume them via class composition.

The token contract is identical across lineages; the per-component class/element name is framework-idiomatic.

## Do's and Don'ts

**Do**

- Reach for Material tokens first; fall back to `identity-*` only when meaning must survive theme flips.
- Use 12px when in doubt about card radius.
- Pair Geologica with Roboto for editorial→functional rhythm.
- Apply the framework's idiomatic token-override API for component theming (CSS variables, theme-extension config, design-system primitives).

**Don't**

- Hardcode hex or use Tailwind palette colors (`bg-teal-500`, `text-gray-700`).
- Use `dark:` variants for theme color switching — tokens handle that.
- Stack drop shadows on product UI cards (marketing only).
- Use `metric-*` as brand colors — it's a categorical chart series.
- Mix `tertiary` and `identity-pink` — they look alike, serve different roles.
- Reach for purple gradients, Inter/system fonts, or AI-shimmer decorations.
- Frame ZCP, agent envs, and recipes as different products.
