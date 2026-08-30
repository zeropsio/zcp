# v2_0b8afea1eeea9d17ff5f00102bf9b5ac24d36c6f8672179d5a3d41be161ce264

## Summary
1. One token source already exists and already reaches mobile: `packages/shared/src/themePalettes.ts` (57 THEME_COLOR_ROLES, 5 OKLCH built-ins) feeds web via `applyThemePalette` → `--app-theme-*` → index.css `html[data-theme-id]` remap, and mobile via `apps/mobile/src/lib/mobileTheme.ts::createMobileThemeVariables` + `scripts/generate-uniwind-themes.mts` (a real generator with a byte-equality drift test). What is NOT sourced from it is the DEFAULT look: web index.css `:root`/dark block (l.1388-1494), mobile `global.css` hand block, `index.html` boot palettes, `themePreview.ts`, `themePalette.ts` T3_CODE_* hex mirror, desktop `DesktopWindow.ts` bg/titlebar colours, `appearanceFonts.ts` — six hand-synced copies. Desktop has no colour tokens at all (DesktopAppBranding = names only; icons via Icon Composer `.icon` projects). Marketing shares nothing.
2. Recommended: option (a) — add `ZEROPS_THEME` (light+dark) to themePalettes.ts, make it the default on both clients through the existing theme path, then generate the boot/preview/desktop copies from it and delete the hand-written default blocks; drift test = existing mobile pattern + role-coverage + contrast assertions.
3. Primary/action in z3 = Zerops blue #0077cc (AA 4.66 both ways; Zerops' own CTA/focus colour); teal/mint is brand + `update` + connected states only — teal on white is 2.03, never text; focus ring #0077cc/#5ab3ff. Provider accents stay per-instance identity (rows/cards, never CTAs); drop blue #2563eb as the fallback swatch.
4. Invented roles (no Zerops counterpart): messageSurface (reuse `--color-zerops-core-blue` #e3f4ff / rgba(88,166,255,.15)), update (teal), sidebarRowSelected (= card white/#141918), warningForeground/Surface, dark error #e57373, terminal selection/scrollbars.
5. Risks: theme colours must be opaque (Zerops is alpha-heavy → flatten per surface); T3 Chat pink fills omitted roles; ANSI-16 and Pierre syntax colours are not themeable; status palette is hue-only (needs glyph+label); Roboto must be self-hosted (web) / bundled (mobile) — keep lucide + SF Symbols, do not ship Material Icons webfont.

## Report
# R5 — Cross-client design-system feasibility (z3 ← Zerops tokens)

Legend: **[V]** verified in code/screens (path:line cited) · **[I]** inferred/derived · **[N]** invented, no Zerops counterpart.

## 1. Token topology today

```
packages/shared/src/themePalettes.ts   [V] 57 roles (l.18-76), ThemeDefinition (l.81-93), 5 built-ins t3-chat/grove/ocean/ember/iris in OKLCH
  │  exported via packages/shared/package.json "./themePalettes", "./themePreview"
  ├─► WEB   apps/web/src/themePalette.ts  APP_THEME_VARIABLES role→"--app-theme-<kebab>" (l.1766-1824)
  │         applyThemePalette() sets them inline on <html> + data-theme-id (l.1854-1877)
  │         index.css html[data-theme-id]:not([data-theme-id=""]) remaps --app-theme-* → --background/--primary/--ring/… (l.1552-1616)
  │         @theme inline maps semantic vars → Tailwind --color-* (l.145-207), text/border roles via --contrast-* (l.1874-1986)
  │         DEFAULT LOOK ("T3 Code") is NOT a ThemeDefinition: hand-written :root + @variant dark (l.1388-1494) on Tailwind palette vars,
  │         plus [data-app-sidebar] override block (l.1499-1541)
  ├─► MOBILE apps/mobile/src/lib/mobileTheme.ts createMobileThemeVariables: 57 roles → 63 "--color-*" vars, OKLCH→hex (l.208-283)
  │         apps/mobile/scripts/generate-uniwind-themes.mts → generated-uniwind-themes.css (@variant <id>-<light|dark> ×10 + ADAPTIVE_COLORS
  │         Tailwind pairs l.56-138), generated-uniwind-theme-names.json, generated-uniwind-default-theme-variables.json (parsed OUT of global.css)
  │         DEFAULT LOOK ("t3-code", MOBILE_DEFAULT_THEME_ID themePalettes l.4) = hand-written global.css @variant light/dark (l.12-208)
  │         terminalTheme.ts: built-ins override bg/fg/cursor/border only; ANSI-16 fixed Pierre palette (l.21-101)
  └─► scripts/mobile-showcase.config.ts (theme-id validation only)
HAND-SYNCED COPIES (drift surfaces):
  • apps/web/index.html boot script: SPLASH_COLORS, DEFAULT_THEME_PALETTES (T3 Chat), BUILT_IN_THEME_PALETTES ("Keep this small boot-time copy in sync", l.13-100+), <meta theme-color #0a0a0a> l.9
  • apps/web/src/themePalette.ts T3_CODE_LIGHT/DARK_THEME_COLORS — flattened hex mirror of index.css defaults (l.307-432), used by getStandardThemeColors
  • packages/shared/src/themePreview.ts STANDARD_THEME_PREVIEW_COLORS (#fcfcfc/#0a0a0a, messageAction #4f46e5/#8b9cff) l.10-22
  • apps/web/src/appearanceFonts.ts DEFAULT_SANS/CODE_FONT_STACK ↔ index.css @theme --font-* (l.136-143, "mirrored in appearanceFonts.ts")
  • apps/desktop/src/window/DesktopWindow.ts getInitialWindowBackgroundColor #0a0a0a/#ffffff (l.128-130), titlebar symbol #1f2937/#f8fafc (l.34-35); preview/Manager.ts #111111 (l.2818)
DESKTOP: no colour tokens. DesktopAppBranding = {baseName, stageLabel, displayName} only (packages/contracts/src/ipc.ts l.175-185) → apps/web/src/branding.ts APP_BASE_NAME fallback "T3 Code" (l.19).
ICONS: scripts/export-brand-icons.ts + scripts/lib/brand-assets.ts — Icon Composer `.icon` project per brand (assets/{dev,nightly,prod}/app-icon.icon) → iOS/macOS/universal 1024 PNG, favicon 16/32/ico, apple-touch, windows .ico; `pnpm icons:export` / `icons:check` (root package.json l.19-20); apply-web-brand-assets.ts copies favicons per channel into apps/web/dist.
MARKETING: apps/marketing (Astro) imports @t3tools/shared only for t3ProjectFile schema; styles inline with literal #09090b/#fff — no token sharing.
```

Generator status **[V]**: exactly one exists (mobile). Its drift test `generate-uniwind-themes.test.ts` "keeps the committed outputs current" (l.13-25) re-renders and byte-compares committed outputs; `--check` flag exists (l.246-256); it runs via `vp test`, not in a prebuild hook; `pnpm --filter @t3tools/mobile generate` is manual (apps/mobile/package.json l.42). Web roles never consumed anywhere in apps/web **[V grep]**: `textMuted`, `accent`, `accentForeground`, `terminalScrollbar`, `terminalScrollbarHover` (web derives `--ring` from `focus` and `--primary` from `messageAction`, index.css l.1571/1600). Mobile consumes all 57.

## 2. Roles → T3 Code values → proposed Zerops mapping

T3 values = `T3_CODE_LIGHT/DARK_THEME_COLORS` (themePalette.ts l.314-432, the flattened default). Zerops sources: light `apps/zerops/src/styles/app.scss :root` (l.59-89), `base/_theme.scss :root` (l.58-150+), live `theme-tokens.json` (light only, `htmlClass:""`); dark `libs/zef/src/styles/base/_dark-theme.scss` header (l.4-8: canvas #0c0f0e, surface #141918, raised #1b2220, overlay #1e2624, text #e9eeec, secondary #9faea9, faint #5e6e69, mint #00e5c0 / fill rgba(0,229,192,.14) / text-on-fill #58efd4), `base/_theme.scss` ramp (l.32-48) and `.zef-dark-theme` (l.411-560), app.scss l.92-100. Brand map `libs/zef/src/styles/_theme.scss` $colors l.61-82 (identityAlpha #00ccbb, identityBlack #1a1a1a, identityRed #cc0011, identityBlue #0077cc, identityGreen #00cc55, identityPurple #cc0077, identityPink #bb00cc, warn = mat orange 400 #ffa726); Material theme primary = teal, accent = blue, warn = mat red (l.36-38). Mint panel token confirmed: `--color-zerops-light-green: #bcfffa` (app.scss l.88; live JSON) — the pale-green zcp panel in shot 05 reads closer to `#e8f7ec` (`--z-light17-green2` / `--z-project-empty-button-bg`); which selector paints it is unverified (see gaps). All theme colours must be **opaque** (themePalette.ts l.310-312) → alphas below are flattened **[I]**.

| # | Role (used by) | T3 light / dark | Zerops light | Zerops dark | Note |
|---|---|---|---|---|---|
| 1 | canvas (`--background`, mobile screen/status-bar) | #fcfcfc / #0a0a0a | #eceff3 | #0c0f0e | [V] `--bg`, `--z-app-background` |
| 2 | chrome (app chrome bg, mobile sheet) | same | #eceff3 | #0c0f0e | Zerops header floats on canvas (shots 03/10) |
| 3 | toolbar (`--toolbar-background`, mobile header@.97) | same | #eceff3 | #0c0f0e | " |
| 4 | toolbarForeground | #27272a / #f5f5f5 | #1a1a1a | #e9eeec | [V] identityBlack / `--z-app-text` |
| 5 | toolbarBorder | #e4e4e7 / #191919 | #d4dfea | #262f2d | [V] `--z-form-border`, `$color-green-11` hairline |
| 6 | toolbarControl (⌘K pill, mobile header controls) | #ffffff / #191919 | #e3e6ea | #1b2220 | [I] `--z-search-trigger-bg` 4% black flattened; dark = raised |
| 7 | toolbarControlForeground | #27272a / #f5f5f5 | #1a1a1a | #e9eeec | |
| 8 | toolbarControlHover | #f4f4f5 / #141414 | #d9dce0 | #2a3331 | [I] 8% black flattened; [V] `$color-dark-green-2` hover |
| 9 | surface (`--card`, mobile card-alt) | #ffffff / #111111 | #ffffff | #141918 | [V] card white / `$color-black-1` |
| 10 | surfaceRaised (composer glass, mobile card/input) | #fcfcfc / #141414 | #ffffff | #1b2220 | light: Zerops lifts by shadow not tone (`c-soft-elevation`) |
| 11 | surfaceOverlay (`--popover`, mobile glass) | #ffffff / #191919 | #ffffff | #1e2624 | [V] mat-autocomplete panel |
| 12 | text (`--foreground`) | #27272a / #f5f5f5 | #1a1a1a | #e9eeec | 15.1 / 15.2 |
| 13 | textMuted (web unused; mobile fg-secondary, chevron) | #71717b / #818181 | #5f6a72 | #9faea9 | [V] `--color-mid`; 7.7 dark |
| 14 | border (`--border`) | #e4e4e7 / #191919 | #e0e0e0 | #262f2d | [V] `--z-grey300-darkgreen2` light |
| 15 | input (input border) | #d4d4d8 / #1e1e1e | #d4dfea | #2a3331 | [V] `--z-form-border`; `--z-grey300-darkgreen2` dark |
| 16 | focus (`--ring`) | #1b4ed8 / #346bf1 | #0077cc | #5ab3ff | [V] `--zcp-wizard-focus-ring` both modes |
| 17 | accent (web unused; mobile primary btn, switch track, md-link) | #1b4ed8 / #346bf1 | #0077cc | #58a6ff | dark: Zerops blue family rgba(88,166,255); note Zerops mat slide-toggle is teal — accept blue |
| 18 | accentForeground | #ffffff / #ffffff | #ffffff | #0c0f0e | white on #58a6ff fails AA → dark text |
| 19 | secondary (secondary btn/chip, mobile switch-off) | #fafafa / #141414 | #f3f5f7 | #232b29 | [V] c-neu-card / dark flat button |
| 20 | secondaryForeground | #27272a / #f5f5f5 | #1a1a1a | #e9eeec | |
| 21 | muted (`--muted`, mobile subtle/blockquote) | #fafafa / #141414 | #f2f5f7 | #151b1a | [V] light-3 / dark-green-1 |
| 22 | mutedForeground | #71717b / #818181 | #5f6a72 | #9faea9 | ≈5.5 on white [I] |
| 23 | placeholder | #71717b / #818181 | #757575 | #5e6e69 | [V] dark placeholder (3.31 — below AA, Zerops' own choice) |
| 24 | secondaryLabel (tertiary text, mobile icon-subtle) | same | #7d8891 | #7d8c88 | [V] dark form-field label; light [I] |
| 25 | iconMuted | same | #5f6a72 | #9faea9 | |
| 26 | error (`--destructive`) | #fb2c36 / #fb414a | #cc0011 | #e57373 | [V] identityRed; #cc0011 on #141918 = 3.03 → [N] mat red 300 dark |
| 27 | errorForeground | #c10007 / #ff6467 | #cc0011 | #e57373 | 5.87 light |
| 28 | errorSurface | #fcebec / #301214 | #fdefef | #2f1717 | [V] `--z-notification-red`, `.is-red` |
| 29 | warning | #fe9a00 / #fe9a00 | #ffa726 | #ffa726 | [V] `warn` mat orange 400 (1.94 on white — indicator only) |
| 30 | warningForeground | #bb4d00 / #ffb900 | #bb4d00 | #ffb74d | [N] Zerops has no orange text token; keep T3's AA pair |
| 31 | warningSurface | #fcf4e8 / #312108 | #fdf6ef | #3a301a | [V] `--z-notification-warning`; dark [N] 16% mix rule |
| 32 | update (update pill) | #1b4ed8 / #346bf1 | #02b1a3 | #00e5c0 | [N] brand-positive, non-action → teal (`--color-zerops-dark`, mint) |
| 33 | updateForeground | #1b4ed8 / #51a2ff | #007e72 | #58efd4 | [V] text-on-mint-fill |
| 34 | updateSurface | #e0e6f7 / #121b34 | #def8f6 | #113630 | [V] `--z-dashboard-select-button-bg` rgba(0,204,187,.129) flattened; mint fill flattened |
| 35 | accentSurface (`--accent`: hover rows, chips; mobile skill bg) | #f4f4f5 / #141414 | #f0f3f5 | #232b29 | [V] `--z-project-detail-service-card-bg`; dark = tooltip/flat-btn |
| 36 | accentSurfaceForeground | #18181b / #f5f5f5 | #1a1a1a | #e9eeec | |
| 37 | messageSurface (user bubble) | #f4f4f5 / #141414 | #e3f4ff | #1e2e3b | [N] reuse `--color-zerops-core-blue` (#e3f4ff / rgba(88,166,255,.15) flattened over #141918) — "you" reads blue-tinted, agent on white card; neutral alt #f3f5f7/#1b2220 |
| 38 | messageForeground | #27272a / #f5f5f5 | #1a1a1a | #e9eeec | |
| 39 | messageAction (→ web `--primary`: CTAs, send, switches, sliders) | #1b4ed8 / #346bf1 | #0077cc | #0077cc | [V] `--zcp-wizard-cta-bg` both modes; 4.66 with white |
| 40 | messageActionForeground | #ffffff / #ffffff | #ffffff | #ffffff | |
| 41 | messageActionHover | #3160db / #3061d9 | #005fa3 | #008ff5 | [V] `--zcp-wizard-cta-bg-hover`; dark = lighten 8% [I] |
| 42 | codeBackground (code, diffs `--diffs-bg`, mobile md-code) | #ffffff / #111111 | #f2f5f7 | #121716 | [V] `--z-light1-dark1`, `$color-black-3` |
| 43 | codeForeground | #27272a / #f5f5f5 | #1a1a1a | #dce4e1 | [V] mat-option dark text |
| 44 | sidebar (thread list, mobile drawer) | #fafafa / #000000 | #eceff3 | #0c0f0e | Zerops left column = canvas with white cards (shot 03) |
| 45 | sidebarForeground | #27272a / #f1f3f7 | #1a1a1a | #e9eeec | |
| 46 | sidebarMutedForeground | #71717b / #a3a3a3 | #4f5a62 | #9faea9 | light darker than #5f6a72 so `--sidebar-icon-color` (60% mix, index.css l.94-98) clears 3:1 [I] |
| 47 | sidebarControlSurface (search field) | #f4f4f5 / #0a0a0a | #e3e6ea | #151b1a | |
| 48 | sidebarRowHover | #fcfcfc / #131313 | #e3e6ea | #151b1a | `--z-search-trigger-bg` 4%/6% flattened |
| 49 | sidebarRowActive | #ffffff / #1a1b1b | #dfe2e6 | #1b2220 | |
| 50 | sidebarRowSelected | #ffffff / #111111 | #ffffff | #141918 | [N] selected thread = a Zerops card |
| 51 | sidebarBorder | #e4e4e7 / #141414 | #e0e0e0 | #262f2d | |
| 52 | terminalBackground | #fcfcfc / #0a0a0a | #f2f5f7 | #0c0f0e | code surface light; canvas dark |
| 53 | terminalForeground | #27272a / #f5f5f5 | #1a1a1a | #e9eeec | |
| 54 | terminalCursor | #26384e / #b4cbff | #007e72 | #00e5c0 | teal #00ccbb on white = 2.03 → darker teal light |
| 55 | terminalSelection | #d0d6dd / #343a47 | #cce4f5 | #213951 | [N] blue 20% / rgba(88,166,255,.28) flattened |
| 56 | terminalScrollbar | #d6d6d6 / #222222 | #d4d8dc | #2a3331 | [N] |
| 57 | terminalScrollbarHover | #bdbdbd / #363636 | #b9bec3 | #3a4442 | [N] |

Non-themeable web colours that also need Zerops values (index.css l.1429-1432, 1480-1482; comment l.1544-1548 "Success, info, provider, and channel identity colors remain independent"): `--success` #00cc55 (indicator; 2.15 on white → text needs `--success-foreground` ≈ #0f7a38 [N, ≈5.4] / #56d364 dark [V `--zcp-wizard-badge-icon`]); `--info` = blue #0077cc / #58a6ff (Zerops uses blue informationally; collides with primary by design). Tag purple: `--z-menu-notifications-badge-bg #d08ff9b9`, region tag `#fcf0ff` bg + purple text (shots), `--color-purple-light #ba7cc5`, dark badge `#6100a4ed` — keep as a Zerops-card-local token, not a theme role.

## 3. Collision analysis

**[V] T3**: `--primary` oklch(0.488 .217 264) ≈ #1b4ed8 light / oklch(0.571 .21 264) ≈ #346bf1 dark (index.css l.1405/1468); `--ring: var(--primary)` (l.1427); `--update: var(--primary)` (l.1436); `--message-action: var(--primary)`; comment l.1566-1570: solid controls (CTAs, switches, sliders, send) share the action colour. Under a theme file they split: `--primary ← messageAction`, `--ring ← focus`, `--update ← update` — so a three-way split is already supported. Provider accents: user-chosen per instance (`accentColor`, packages/contracts/src/providerInstance.ts l.127; swatches ProviderAccentColorPicker.tsx l.12-19: #2563eb #16a34a #ea580c #dc2626 #7c3aed #0891b2; FALLBACK = blue #2563eb l.21), painted only on ModelListRow/ModelPickerContent/ProviderInstanceCard. Fixed brand marks: Claude #d97757 (Icons.tsx l.503, TraitsPicker.tsx l.506, usageProviders.ts l.24), Codex = `var(--contrast-foreground)`, Grok = neutral mix (usageProviders.ts l.16-33).

**[V] Zerops**: Material primary = teal #00ccbb, accent = blue #0077cc (`libs/zef/src/styles/_theme.scss` l.36-38); live: badges/stepper/datepicker selected = #00ccbb, CTAs/links/active tags/focus = #0077cc (`--zcp-wizard-cta-bg`, `--z-project-status-active-tag-bg`, `--z-filter-color-active`, `--zcp-wizard-focus-ring`); shots 03/05/10: every button/link is blue, teal only in the logo tile, dark mint on menu icon. Dark theme header: "mint reserved as the single brand accent" (_dark-theme.scss l.4-8); dark flat primary button = mint 14% fill + #58efd4 text (l.78-82); selected option = rgba(88,166,255,.12) + #6cb8ff (l.144-147).

**Proposal — primary = blue, brand = teal.** Contrast (computed): #0077cc↔white 4.66 both ways (AA); #00ccbb on white 2.03, on #eceff3 1.76, white on #00ccbb 2.03 → teal cannot be a text colour or a white-label button in light; #1a1a1a on #00ccbb 8.58 (a teal button would need dark text — not a Zerops idiom in light). Dark: #00e5c0 on #141918 10.97, #58efd4 on flattened mint fill 9.23, #6cb8ff 8.41, #0077cc solid 3.82 vs card (non-text OK) and 4.66 under white text. So: `messageAction`/`--primary` #0077cc, `focus` #0077cc/#5ab3ff, `update`+`updateSurface` teal/mint, brand marks (logo, favicon, splash accent, notification badge count, "authorized/connected" state dots, dark menu icon) teal/mint. Provider accents coexist because they never reach CTAs: keep the picker but change the fallback from #2563eb (indistinguishable from primary) to a neutral or teal-dark #02b1a3, and keep Claude #d97757 as an icon fill only on light surfaces (3.12 on white; 5.69 on #141918). Warning vs Claude: both oranges (#ffa726 vs #d97757) — a lifecycle-strip warning must be a filled badge with text, the Claude mark stays the asterisk glyph.

## 4. Generator design options

**(a) One TS source → generated web/mobile/boot/desktop blocks (recommended).** Build on: `themePalettes.ts` (add `ZEROPS_THEME: ThemeDefinition` with `variants.dark`, `sidebarArtwork:false`; add "zerops" to BUILT_IN_THEME_IDS and set `MOBILE_DEFAULT_THEME_ID`), `mobileTheme.ts::createMobileThemeVariables` (already the roles→mobile projection), `generate-uniwind-themes.mts` (already renders `@variant zerops-light/dark` the moment the id is in BUILT_IN_THEME_IDS), `themePalette.ts::serializeThemeFile` (l.1751) for a `.json` theme fixture, `themeColorToNativeColor`/`themeContrastRatio` (themePalette.ts l.957, mobileTheme.ts l.151) for assertions. Files to create: `scripts/generate-theme-tokens.ts` (root, mirrors `icons:export`/`icons:check` shape, root package.json l.19-20) emitting (1) `apps/web/src/generated-theme.css` — `:root{--app-theme-*}` + `@variant dark{…}` so the default look IS the theme and index.css l.1388-1494 shrinks to the semantic mapping; (2) `apps/mobile/global.css` default `@variant light/dark` block (or drop it and make the runtime name `zerops-<appearance>` in `mobileThemeRuntime.ts::getMobileUniwindThemeName` l.41-46); (3) `apps/web/public/boot-theme.json` or an inlined snippet replacing index.html `DEFAULT_THEME_PALETTES`/`BUILT_IN_THEME_PALETTES`/`SPLASH_COLORS` (l.21-100+) and `<meta theme-color>`; (4) `packages/shared/src/generated/brandColors.ts` consumed by `themePreview.ts` STANDARD_THEME_PREVIEW_COLORS and `DesktopWindow.ts` initial bg/titlebar symbol colours (l.128-130, l.34-35); (5) `themePalette.ts::getDefaultThemeColors` (l.1224) and `T3_CODE_*` (l.314-432) → ZEROPS_THEME. Hook: `"generate:theme"` + `"generate:theme:check"` root scripts; CI `lint` job runs `--check` (mobile-fingerprint-check.yml shows the pattern); `vp test` drift test. Drift test asserts: byte-equality of every generated file vs re-render; `Object.keys(ZEROPS_THEME.colors)` ⊇ THEME_COLOR_ROLES for light and dark; contrast ≥4.5 for text/canvas, text/surface, messageActionForeground/messageAction, accentSurfaceForeground/accentSurface, errorForeground/errorSurface, warningForeground/warningSurface, updateForeground/updateSurface; ≥3.0 for focus/canvas; every value opaque hex/OKLCH (no alpha).

**(b) Hand parity + drift test.** Nothing to build on except the T3_CODE_* mirror, which is exactly the failure mode (stale hex copy of CSS `color-mix`/`--alpha` expressions that vitest cannot evaluate). A test would have to parse index.css and resolve Tailwind vars — brittle. Reject.

**(c) Web-only theme + mobile hand-port.** Cheapest MVP (edit index.css + global.css by hand), but discards the only working generator and leaves six copies to rot; mobile is "catch-up" in the brief (D6) so this is tempting — ship it only as step 0 and converge to (a).

Recommendation: (a), staged: step 1 add ZEROPS_THEME and select it by default through the existing `data-theme-id` path (no generator yet; mobile via `pnpm generate`); step 2 generator + copies; step 3 delete hand-written defaults and the T3 Chat fallbacks (`html[data-theme-id="t3-chat"]` grain l.2038-2041 can go too).

## 5. Beyond colour

| Axis | T3 [V] | Zerops [V] | Recommendation |
|---|---|---|---|
| UI font | web: system stack (index.css l.139-143, appearanceFonts.ts l.20-21); mobile: bundled DM Sans 400/500/700 (`@expo-google-fonts/dm-sans`, app.config.ts l.109-111, global.css l.215-217) | Roboto 300-900 + Roboto Mono via Google Fonts (index.html l.34), `$fontMain: 'Roboto'` (_theme.scss l.55), mat typography Roboto/"Helvetica Neue" | Web: self-host Roboto 400/500/700 woff2 (CSP/offline containers; no third-party font fetch), `--font-sans: Roboto, -apple-system, …`; Settings→Appearance override keeps working. Mobile: swap DM Sans → `@expo-google-fonts/roboto` (same bundling path; Android has Roboto natively, iOS needs the bundle); re-check `src/lib/typography.ts` scale (x-height differs). Not system-SF-on-iOS: the brief is "looks like Zerops". |
| Code/mono | SF Mono/Menlo/Consolas (appearanceFonts.ts l.25-26); mobile `monospace` on Android | Roboto Mono; DejaVu Sans Mono self-hosted for logs (customFonts.scss) | Keep T3 stacks (diff/terminal are perf-sensitive, Ghostty canvas takes its own font); optional Roboto Mono for Zerops cards only. |
| Radius | `--radius` 0.625rem=10px → sm6/md8/lg10/xl14/2xl18 (index.css l.1390, 202-207); `--control-radius` 8px (l.91) | buttons 17px pill on 38px line-height (`_material.scss` l.13-20); `.mat-card` 10px !important (l.65-67), neu-card 12px, dialog 16px (l.87-91, dark l.120); zui scss histogram 8px×68, 6px×28, 4px×24, 10px×15, 12px×8 | Keep `--radius` 10px (= mat-card); buttons → `rounded-full` on primary/secondary variants; dialogs 16px (2xl 18 is close — or add a `--radius-dialog`); chips 8px. |
| Spacing | Tailwind 4px grid, compact insets 0.5-0.75rem (l.91-102) | `--bu: 24px` base unit (app.scss l.60), cards padded in 24/12 multiples | Keep T3 density for chat/sidebar; use 24/12 only inside Zerops surfaces (service map, cards, strip). |
| Icons | web `lucide-react ^0.564.0` (apps/web/package.json l.42); mobile SF Symbols via `expo-symbols` (53 files) + 1 Tabler file, no lucide | Material Icons Outlined webfont (index.html l.35; `fontSet="material-icons-outlined"` ×233, `<mat-icon>` in 232 files) + custom SVG marks (zui `antigravity-mark`, `agent-strip`) | Keep lucide + SF Symbols (both 1.5-2px outline families, visually compatible with Material Outlined; a webfont adds a blocking font + layout shift, against AGENTS.md perf rule). Import Zerops brand/service-type SVGs as React components; map the ~15 icons the Zerops surfaces need to lucide equivalents in one table. |

## 6. Risks

1. **Contrast slider math** (index.css l.1874-1986; appearanceContrast.ts l.3-10): `contrast<100` mixes each foreground toward its own surface in oklab (`--appearance-contrast-base`), `>100` mixes toward black/white (`--appearance-contrast-target`, l.84/116, chosen by `.dark`, not canvas luminance); borders get a quarter boost toward foreground. Accents/primary are untouched, so teal/blue never shift. With Zerops values the derived `--sidebar-icon-color` (60% of sidebar-muted over sidebar, l.94-98) lands ≈2.7:1 in light with #5f6a72 → use #4f5a62 (row 46). `--contrast-message-foreground` on the blue-tinted messageSurface stays ≥14:1.
2. **Teal legibility**: light — 2.03 on white / 1.76 on canvas → never text, thin lines, or white-label buttons; #007e72 is 4.97 on white but 4.31 on #eceff3 (fails 4.5 for body text on canvas → #006b61 [N] or keep teal text on white cards only). Dark — mint #00e5c0 10.97, fine; `#0077cc` solid on #141918 3.82 (OK for a filled control, not for blue text — use #6cb8ff/#58a6ff for text).
3. **Colour-blind safety**: Zerops statuses are hue-only mat 400s (green/orange/red/blue, `_theme.scss` l.86-120); #00cc55 vs #ffa726 vs #cc0011 are deuteranopia-confusable; the Zerops GUI pairs every dot with a label ("ACTIVE", shots 03/05). Service map + lifecycle strip must carry glyph+label per state; blue info vs blue primary is CVD-safe.
4. **Opaque-only palette**: theme files store opaque OKLCH (themePalette.ts l.310-312); Zerops is alpha-heavy (4%/6%/8% overlays, rgba fills). Flattened values are correct only over the surface they were flattened on (hover 4% over white ≠ over canvas) — the generator must flatten per intended surface and the table above records that assumption.
5. **Fallback pink**: `getDefaultThemeColors` = T3 Chat (l.1224-1226) fills omitted roles; index.html DEFAULT_THEME_PALETTES (l.37-50) hardcodes T3 Chat for the pre-mount paint → a partial Zerops theme flashes pink. Both must point at ZEROPS_THEME.
6. **Diff/terminal palettes are not themed**: Pierre diffs get only `themeType` light/dark (DiffPanel.tsx l.963) and a forced `--diffs-bg` (index.css l.2420-2427) — syntax and +/- colours are Pierre's; web terminal reads only bg/fg/cursor/selection from CSS vars (ThreadTerminalDrawer.tsx l.156-202), ANSI-16 = Ghostty defaults; mobile likewise (terminalTheme.ts fixed Pierre ANSI, l.21-101). Zerops dark canvas #0c0f0e with default ANSI blue may be low-contrast; adding ANSI roles means growing THEME_COLOR_ROLES (out of scope here).
7. **Mobile bubble**: default iMessage blue `--color-user-bubble` #007aff becomes messageSurface tint with dark text once themed; `readableMessageAccent` (mobileTheme.ts l.164-197) already solves skill-text contrast.
8. **Desktop**: only hard-coded window bg/titlebar colours (DesktopWindow.ts l.34-35, 128-130) and icon projects; a Zerops `.icon` per brand (dev/nightly/prod, brand-assets.ts l.1-32) is required or `icons:check` fails; `theme-color` meta and manifest colours too.
9. **Performance**: colour changes are free; the only cost is a webfont — self-host, preload, `font-display: swap`; do not add the Material Icons webfont.

## Key facts
- THEME_COLOR_ROLES has 57 roles; ThemeDefinition = {id,label,appearance,colors,variants?,collection?,sidebarArtwork?,managed?}; five built-ins (t3-chat, grove, ocean, ember, iris) stored as OKLCH strings — _/Users/macbook/Documents/Zerops-MCP/z3/packages/shared/src/themePalettes.ts l.1-93, 730-748_
- The web default ('T3 Code') look is NOT a ThemeDefinition: it is a hand-written :root + @variant dark block on Tailwind palette vars; --primary is oklch(0.488 0.217 264) light / oklch(0.571 0.21 264) dark; --ring, --update and --message-action all default to --primary — _/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/index.css l.1388-1494_
- A theme file is applied by setting --app-theme-<role> inline on <html>; the html[data-theme-id] block remaps them so --primary ← messageAction, --ring ← focus, --update ← update, --accent ← accentSurface — _/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/themePalette.ts l.1766-1877; index.css l.1552-1616_
- Web never consumes --app-theme-text-muted, -accent, -accent-foreground, -terminal-scrollbar, -terminal-scrollbar-hover (grep over apps/web/src returned 0 hits) — _grep var(--app-theme-*) over /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src_
- Mobile DOES consume themePalettes: createMobileThemeVariables maps the 57 roles onto ~63 --color-* variables (OKLCH→hex), and generate-uniwind-themes.mts renders @variant <id>-<light|dark> blocks into generated-uniwind-themes.css with a byte-equality drift test and a --check flag; the mobile DEFAULT 't3-code' look is a hand-written block in global.css — _/Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/lib/mobileTheme.ts l.208-300; apps/mobile/scripts/generate-uniwind-themes.mts l.140-262; generate-uniwind-themes.test.ts l.13-25; apps/mobile/global.css l.6-210_
- Hand-synced copies of default colours exist in apps/web/index.html (SPLASH_COLORS, DEFAULT_THEME_PALETTES, BUILT_IN_THEME_PALETTES, 'Keep this small boot-time copy in sync'), themePalette.ts T3_CODE_LIGHT/DARK_THEME_COLORS, themePreview.ts STANDARD_THEME_PREVIEW_COLORS, appearanceFonts.ts font stacks, DesktopWindow.ts (#0a0a0a/#ffffff bg, #1f2937/#f8fafc titlebar symbols) — _index.html l.13-100+; themePalette.ts l.307-432; packages/shared/src/themePreview.ts l.10-22; apps/web/src/appearanceFonts.ts l.20-26; apps/desktop/src/window/DesktopWindow.ts l.34-35,128-130_
- DesktopAppBranding carries only {baseName, stageLabel, displayName}; desktop has no colour tokens; app icons come from Icon Composer .icon projects per brand exported by scripts/export-brand-icons.ts (pnpm icons:export / icons:check) — _/Users/macbook/Documents/Zerops-MCP/z3/packages/contracts/src/ipc.ts l.175-185; scripts/lib/brand-assets.ts l.1-32; package.json l.19-20_
- apps/marketing (Astro) imports @t3tools/shared only for t3ProjectFile; its styles use literal #09090b/#fff — no token sharing — _/Users/macbook/Documents/Zerops-MCP/z3/apps/marketing/package.json l.13; apps/marketing/src/pages/schema/t3.json.ts l.3; grep of layouts_
- Provider accent colours are per-instance user choices (accentColor, swatches #2563eb #16a34a #ea580c #dc2626 #7c3aed #0891b2, fallback blue) painted on model rows/instance cards; Claude brand mark is fixed #d97757; Codex uses var(--contrast-foreground) — _packages/contracts/src/providerInstance.ts l.127; apps/web/src/components/settings/ProviderAccentColorPicker.tsx l.12-21; apps/web/src/components/usage/usageProviders.ts l.16-33; Icons.tsx l.503_
- Zerops Material theme: primary = teal #00ccbb, accent = blue #0077cc, warn = mat red; brand map identityAlpha #00ccbb, identityBlack #1a1a1a, identityRed #cc0011, identityBlue #0077cc, identityGreen #00cc55, identityPurple #cc0077, identityPink #bb00cc, warn = mat orange 400; fonts Roboto / DejaVu Sans Mono — _/Users/macbook/Documents/Zerops-MCP/frontend-legacy/libs/zef/src/styles/_theme.scss l.3-82_
- Zerops light: --bg #eceff3, --z-app-text #1a1a1a, --color-zerops-light-green #bcfffa, --z-search-trigger-bg rgb(0 0 0/4%), --zcp-wizard-cta-bg #0077cc (hover #005fa3), --zcp-wizard-focus-ring #0077cc, --z-notification-red #fdefef, --z-notification-warning #fdf6ef, --z-form-border 2px solid #d4dfea; live capture is light-only (htmlClass '') — _frontend-legacy/apps/zerops/src/styles/app.scss l.59-89; base/_theme.scss l.58-150; scratchpad/shots/gui-nonmember/theme-tokens.json_
- Zerops dark ramp: canvas #0c0f0e, surface/card #141918, raised #1b2220, overlay #1e2624, secondary surface #151b1a, hover border #2a3331, hairline #262f2d, text #e9eeec, secondary #9faea9, faint #5e6e69; mint #00e5c0 is 'the single brand accent' with fill rgba(0,229,192,.14) and text-on-fill #58efd4; dark selection uses rgba(88,166,255,.12)/#6cb8ff; CTA stays #0077cc — _frontend-legacy/libs/zef/src/styles/base/_dark-theme.scss l.4-8,69-82,144-147; apps/zerops/src/styles/base/_theme.scss l.32-48,411-560_
- Zerops radii: buttons 17px pill (38px line-height), .mat-card 10px !important, c-neu-card 12px, dialogs 16px; icons Material Icons Outlined webfont (fontSet="material-icons-outlined" ×233); fonts loaded from fonts.googleapis.com — _frontend-legacy/libs/zef/src/styles/base/_material.scss l.13-20,65-67,87-91; apps/zerops/src/index.html l.34-35; grep counts_
- Computed WCAG ratios: #00ccbb on white 2.03, on #eceff3 1.76; #007e72 on white 4.97, on #eceff3 4.31; #0077cc↔white 4.66; #00e5c0 on #141918 10.97; #58efd4 on flattened mint fill (#113630) 9.23; #6cb8ff on #141918 8.41; #0077cc on #141918 3.82; #00cc55 on white 2.15; #cc0011 on white 5.87, on #141918 3.03; #e57373 on #141918 ≈7; #d97757 on white 3.12, on #141918 5.69; #ffa726 on white 1.94 — _python WCAG luminance computation run in this session over the token values_
- Contrast slider: base<100% mixes foreground toward its surface in oklab, boost>100% mixes toward black/white (target chosen by .dark), borders get boost/4; only foreground/border roles are affected — _/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/appearanceContrast.ts l.3-10; index.css l.79-121,1874-1986_
- Web terminal takes only bg/fg/cursor/selection from CSS vars (ANSI-16 = Ghostty default); mobile terminal overrides only bg/fg/cursor/border over a fixed Pierre ANSI palette; Pierre diffs get only themeType light/dark plus a forced --diffs-bg — _apps/web/src/components/ThreadTerminalDrawer.tsx l.156-202; apps/mobile/src/features/terminal/terminalTheme.ts l.21-101; apps/web/src/components/DiffPanel.tsx l.963; index.css l.2420-2427_
- Theme colours are stored opaque ('theme colors are stored as opaque OKLCH tokens'); getDefaultThemeColors fills omitted roles with T3 Chat; standardStatusColors keys dark/light off canvas luminance < 0.179 — _/Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/themePalette.ts l.307-313,753-812,1224-1226_
- Web icon library is lucide-react ^0.564.0; mobile uses expo-symbols SymbolView in 53 files and @tabler/icons-react-native in 1, no lucide; mobile bundles DM Sans 400/500/700 — _apps/web/package.json l.42; grep over apps/mobile/src; apps/mobile/app.config.ts l.109-111; apps/mobile/package.json_

## Gaps
- theme-tokens.json is light-only (htmlClass ""); dark values in this report come from SCSS sources (base/_theme.scss .zef-dark-theme, zef _dark-theme.scss, app.scss html.zef-dark-theme) — a live dark capture (html.zef-dark-theme) would confirm computed values and any runtime overrides.
- Which selector paints the pale-green ZCP panel in 05-project-detail.png (candidates: --z-light17-green2 #e8f7ec, --z-project-empty-button-bg, --z-routing-card-bg) and whether --color-zerops-light-green #bcfffa has any consumer — needs `grep -rn 'zerops-light-green\|light17-green2' frontend-legacy/apps frontend-legacy/libs` and a DOM inspection of the zcp card.
- Ghostty's default ANSI-16 palette on web (apps/web/src/terminal/ghostty/core.ts) was not read; the dark-terminal contrast risk is inferred.
- The zui button component's own radius (libs/zui/src/… button scss) was not located; the 17px pill comes from the zef Material override (_material.scss l.13-20) which applies to mat-button classes.
- How useTheme.ts toggles the .dark class when a themed dark variant is applied (assumed from resolveThemeAppearance; not read) — matters for --appearance-contrast-target selection.
- Flattened alpha values in the mapping (#e3e6ea, #d9dce0, #1e2e3b, #3a301a, #213951, #cce4f5, #008ff5, #0f7a38, ≈5.5 for #5f6a72) were derived by hand/estimate; recompute in the generator with an exact flatten function before committing.
- The mobile typography scale (src/lib/typography.ts) was not read; the DM Sans → Roboto swap needs its metrics checked.
- Provider accent usage sites beyond ModelListRow/ModelPickerContent/ProviderInstanceCard/AddProviderInstanceDialog were not enumerated.
