# One Map, Three Shells

## Thesis
z3 does not get three skins; it gets one source and three native implementations of it. The source is small and already half-built: a `ZEROPS_THEME` ThemeDefinition in `packages/shared/src/themePalettes.ts` (57 roles, light + dark) plus a brand package (mark and wordmark as path data, a fixed status-tone table, type/radius constants, a service-icon subset, provider marks) from which every client's colour, boot splash, desktop titlebar/window colours, favicons and app icons are generated — the mobile pipeline (`createMobileThemeVariables` → `generate-uniwind-themes.mts` → byte-equality drift test) proves the mechanism works, and web already remaps the same roles onto its semantic vars under `html[data-theme-id]`. On top of the tokens sits a written component vocabulary — StatusDot, MicroLabel, Chip, Pill, FlatCard, MintPanel, LifecycleStrip, ServiceRow, ZeropsCard, QuestionCard, CredentialCard — that names anatomy and states, not pixels, so web implements it with Base UI + Tailwind, iOS with React Native inside Liquid Glass headers and form sheets, Android with its token-styled in-flow chrome, desktop with Electron's window-controls overlay. This is right for Zerops Code because (1) the Zerops GUI's recognisability is a grammar — cool grey canvas, flat tone-on-tone cards, dot + tiny uppercase word, pills, blue action / teal identity, graphite-green dark with one mint accent — and a grammar survives translation into any material, whereas a pixel port of an Angular Material desktop app into iOS would fail both the brief ("looks like Zerops") and the platform; (2) after the hard-fork freeze the dominant long-term risk is drift between three shells maintained by agents, and a generator with `--check` in CI, role-coverage and contrast assertions, and a no-literal-colour lint on the Zerops surfaces turns coherence from a review habit into a failing test; (3) the parts of T3 users already trust — the composer geometry, virtualised timeline, iOS 26 glass, WCO titlebar — are chrome, and chrome is exactly what a token-and-vocabulary system leaves alone.

## Information architecture
## Shell

Web keeps T3's three-column working surface — sidebar 256/208 min (`apps/web/src/components/threadSidebarWidth.ts`), thread, right panel inline ≥981px / sheet below (`rightPanelLayout.ts:1`) — because it is the surface the product is used in all day. The Zerops GUI's 480px controls pane + `width=1200` viewport is legacy the concept does not inherit (R1 §10). What *is* inherited is the shell grammar: the sidebar paints canvas (`sidebar` role = `#eceff3`/`#0c0f0e`, R5 row 44 — "Zerops left column = canvas with white cards"), its header is a 40px row holding the **Zerops mark pill** (account/org menu, teal-tinted `#00ccbb21`, replaces `T3Wordmark` + stage art in `sidebar/SidebarChrome.tsx:80-118` and `SidebarStageBackdrop.tsx`) and the **⌘K pill**; the selected thread is a white card (`sidebarRowSelected` = `surface`), a project header is a card with a status dot. The header row stays 52px (`--workspace-topbar-height`, `index.css:79-121`, WCO override @124) and hosts breadcrumb, strip pill and actions.

## Top-level objects (user-facing nouns)

Account (one or more Zerops orgs) → **Project** (= T3 environment; the word "environment" never appears — brief D3, R6 §3.2) → **Thread**. **Services** are an attribute of a project (the map), not navigation. **Devices** (Settings → Devices, replaces Connections, labels `Zerops Code · <browser> on <os>`, spec-z3 §4.8). **Coding agents** (Settings, replaces Providers). The T3 "project" record at `/var/www` is invisible (D3).

## Navigation

- Sidebar: projects → threads; T3's shelves (pinned/live/settled/snoozed, `Sidebar.tsx`) stay; each project card carries its live thread's strip phrase, so the sidebar doubles as a fleet view.
- Header: `<project> / <thread>` breadcrumb (`chat/ChatHeader.tsx`); **lifecycle strip** as a status pill beside it (it already mounts beside, not inside, `ChatHeader` — `ChatView.tsx:6906-6928`, spec-z3 §5.4); actions: **Cloud IDE** (replaces the `OpenInPicker` editor list), **Map** (toggles right-panel kind `zerops`, shortcut Z per `RightPanelTabs.tsx` add-menu), panel controls. T3's `GitActionsControl` (Commit & push) is hidden on Zerops projects — commit+push is zcp's (D4).
- Command palette (`CommandPalette.tsx`): Zerops-style scope chip (`kanban`), intents `New thread in <project>`, `Open service map`, `Open <hostname>` (subdomain), `Open Cloud IDE`, `Show logs for <host>` (composer prefill), `Switch project`.

## Where the Zerops surfaces live

| Surface | Web | Phone | Tablet/desktop |
|---|---|---|---|
| Service map | right panel kind `zerops` (`rightPanelStore.ts:17-27`), auto-opened the first time a Zerops project connects; header Map toggle | form sheet (detents [0.55, 0.92], `native/sheet-surface.ts`) from the header ▤ item | iPad inspector pane (`features/layout/workspace-inspector-pane.tsx`); desktop = web |
| Lifecycle strip | header pill (`ZeropsLifecycleStrip.tsx`) | header subtitle line (the `WorkspaceConnectionTitle` swap slot) | same as web |
| Cards (plan/import/mount/deploy/verify/subdomain/error) | timeline under the tool call (`MessagesTimeline.tsx:2605`, `ZeropsToolCard.tsx`). Product call: deploy, verify, error and question cards **escape** the "Used N tools" fold (they are the narrative); import and mount stay folded | `ThreadFeed.tsx` native card rows | same |
| Question card | composer pending panel (`ComposerPendingUserInputPanel.tsx`), needs-you tint | `PendingUserInputCard` + "Other" | same |
| Credential card | same slot as the question card, distinct anatomy: secure field, target host, "Store on <host>"; value → z3 server → zcp verb (owner-designed) → service secret, never the chat (brief §4 rule 4, §8.3) | form sheet with `secureTextEntry`; never enters SQLite thread state | same |
| Quick actions | composer context strip — the row where `BranchToolbar` / "Local checkout · main" sits today (`ChatView.tsx:7158-7188`) becomes `kanban · kanbandev  (Deploy) (Show logs) (Add Redis)`; prefill only (Z3F-7) | horizontal chip row above the composer | same |
| Data Console | new right-panel kind `data`: iframe of `zcp studio console serve` (spec-dataconsole embed reach; caller-bound write token stays out of z3 — read-only v1), opened from a Data row's "Data Console" pill | "Open in Zerops" link in v1 (an embedded editor in a phone web view is a later decision) | inline like web |
| Terminal / Files / Diff | existing kinds; file tree grouped by service hostname (`/var/www/<host>`, spec-z3 §6.1); diff grouped by service (D4) | existing routes | existing |

## Sign-in → project → thread

One landing component, two entry modes: **hosted** (`isHostedStaticApp()`, all projects across every org) and **origin** (the container-served bundle: a descriptor-probe branch of the `requires-auth` gate in `routes/__root.tsx:62-83` renders the Zerops landing instead of `/pair` when the origin serves `/z3/.well-known/t3/environment`; the project is pre-selected from the descriptor; "Switch project" links to hosted). R2 §5 lists the exact seams: `hostedPairing.ts:35-46`, `routes/-chatIndexView.ts`, `connectZeropsIdentity` against `window.location.origin`, `clientMetadata.ts` keyed on Zerops not `hosted`. Steps: sign in (email/password/TOTP, GitHub/GitLab pills as in `01-login.png`; registration hands off to app.zerops.io when Turnstile refuses — Z3C-3) → **picker** grouped Connected / Ready to connect / Preparing / Not available with the reason named (`zerops/candidates.ts`) → **New project** (name + one pill per org) → **provisioning** card: `awaiting-project` (60 s) → `awaiting-container` (300 s) → `awaiting-health` (30 s), plus `pool-exhausted` (a state), `needs-enable`, `timed-out` (retryable) (Z3C-4) → **readiness**: "Enable Zerops Code" = restart ≈ 19 s, 502 rendered as "restarting", cap 30 s (Z3C-5, spec-z3 §2.5) → identity connect (silent, 900 s re-mint invisible) → **thread** with the onboarding prompt composed, not sent (Z3C-8); the draft hero keeps T3's "What should we build in kanban?" with the dotted project menu (`chat/DraftHeroHeadline.tsx`). Second device: Devices → "Connect another device" → one-time token as QR/link (`ui/qr-code.tsx` exists); the word "pairing" never renders.

## Empty states (each is a phrase from the pinned vocabulary, never blank)

| Where | State | Rendering |
|---|---|---|
| Picker | no projects | one dashed "New project" card (Zerops `__project-add-button` idiom) |
| Picker | pool exhausted | Preparing card: "Zerops is preparing containers — try again in a while" (fact, not error) |
| Thread | no envelope yet | strip absent (not "idle") — `zeropsStripState` returns undefined |
| Map | feed `available:false` | panel kind hidden entirely (`ZeropsServiceMap` returns null; "absent, not empty") |
| Map | `degraded:true` | last-good rows + warning line (`data-zerops-map-degraded`) |
| Map | doorbell `false` | one quiet 10px line "live updates reconnecting" (spec §5.4) |
| Map | zero services | "no services yet" + the Build quick action |
| Sidebar | no threads | draft hero |
| Container | pre-z3 / unreachable | same "Enable Zerops Code" offer (Z3C-5) |
| Container | restarting | strip pill `◔ RESTARTING` for ≤30 s |

## Reverse states (AGENTS.md "a one-way door is a bug")

Sign in ↔ sign out (Settings → Zerops); connect device ↔ remove device (Devices); open map ↔ close panel, pin ↔ unpin; strip pill click ↔ close; question answered ↔ "Other" free text; quick action prefill ↔ clear composer; credential stored ↔ **Replace** (a new card instance; revoke stays in the Zerops GUI — stated, not hidden); Zerops default theme ↔ user theme ↔ **Reset to Zerops**; Cloud IDE opens in a new tab (no in-app state to reverse); "Enable Zerops Code" has **no reverse** — it is an upgrade; the picker says so ("restarts the container, ≈20 s").

## Visual system
## Tokens — `ZEROPS_THEME` on the 57 `THEME_COLOR_ROLES` (`packages/shared/src/themePalettes.ts:18-76`)

Values are opaque (theme colours are stored opaque, `apps/web/src/themePalette.ts:310-312`); Zerops' alpha tints are flattened over the surface they sit on, and the generator records which surface (R5 risk 4). Sources: light `frontend-legacy/apps/zerops/src/styles/app.scss:59-89` + `base/_theme.scss`, live `theme-tokens.json`; dark `libs/zef/src/styles/base/_dark-theme.scss:4-8`.

| Role(s) | Light | Dark | Note |
|---|---|---|---|
| canvas, chrome, toolbar, sidebar, terminalBackground(dark) | `#eceff3` | `#0c0f0e` | the Zerops canvas; `theme-color` meta and `--boot-*` follow |
| surface (`--card`), sidebarRowSelected | `#ffffff` | `#141918` | white tile / dark surface |
| muted, codeBackground, terminalBackground(light) | `#f2f5f7` | `#151b1a` / code `#121716` | Zerops card tone `#f3f5f7` family |
| secondary, accentSurface | `#f3f5f7` / `#f0f3f5` | `#232b29` | flat secondary pill; hover rows |
| surfaceRaised (composer glass) | `#ffffff` | `#1b2220` | light lifts by shadow, dark by tone |
| surfaceOverlay (`--popover`) | `#ffffff` | `#1e2624` | menus/dialogs |
| text, toolbarForeground, sidebarForeground | `#1a1a1a` | `#e9eeec` | identityBlack |
| textMuted, mutedForeground, iconMuted | `#5f6a72` | `#9faea9` | ≈5.5 / 7.7 |
| sidebarMutedForeground | `#4f5a62` | `#9faea9` | darker so the derived `--sidebar-icon-color` (60% mix, `index.css:94-98`) clears 3:1 |
| placeholder, secondaryLabel | `#757575` / `#7d8891` | `#5e6e69` / `#7d8c88` | |
| border, sidebarBorder | `#e0e0e0` | `#262f3d`→`#262f2d` | hairline |
| input | `#d4dfea` | `#2a3331` | `--z-form-border` |
| toolbarControl / Hover, sidebarControlSurface, sidebarRowHover | `#e3e6ea` / `#d9dce0` | `#1b2220` / `#2a3331` | ⌘K pill = 4%/8% black flattened |
| sidebarRowActive | `#dfe2e6` | `#1b2220` | |
| focus (`--ring`) | `#0077cc` | `#5ab3ff` | `--zcp-wizard-focus-ring` |
| messageAction (`--primary`), messageActionHover | `#0077cc` / `#005fa3` | `#0077cc` / `#008ff5` | the ACTION colour; 4.66 with white both ways |
| messageActionForeground | `#ffffff` | `#ffffff` | |
| accent, accentForeground (mobile primary, switch track, md-link) | `#0077cc` / `#ffffff` | `#58a6ff` / `#0c0f0e` | white on `#58a6ff` fails AA → dark text |
| accentSurfaceForeground, secondaryForeground | `#1a1a1a` | `#e9eeec` | |
| messageSurface, messageForeground | `#e3f4ff` / `#1a1a1a` | `#1e2e3b` / `#e9eeec` | user bubble = Zerops core-blue tint; no iMessage blue |
| error, errorForeground, errorSurface | `#cc0011` / `#cc0011` / `#fdefef` | `#e57373` / `#e57373` / `#2f1717` | `#cc0011` on `#141918` is 3.03 → mat red 300 in dark |
| warning, warningForeground, warningSurface | `#ffa726` / `#bb4d00` / `#fdf6ef` | `#ffa726` / `#ffb74d` / `#3a301a` | orange is indicator-only (1.94 on white) |
| update, updateForeground, updateSurface | `#02b1a3` / `#007e72` / `#def8f6` | `#00e5c0` / `#58efd4` / `#113630` | the ONE teal/mint use in chrome: update pills |
| codeForeground, terminalForeground | `#1a1a1a` | `#dce4e1` / `#e9eeec` | |
| terminalCursor, terminalSelection | `#007e72` / `#cce4f5` | `#00e5c0` / `#213951` | |
| terminalScrollbar / Hover | `#d4d8dc` / `#b9bec3` | `#2a3331` / `#3a4442` | |

Contrast facts the generator asserts (R5 §3, computed): `#0077cc`↔white 4.66; `#00ccbb` on white 2.03 and on `#eceff3` 1.76 → **teal is never text, never a white-label button in light**; `#007e72` on white 4.97 (on canvas 4.31 → teal text only on white cards); `#00e5c0` on `#141918` 10.97; `#58efd4` on `#113630` 9.23; `#6cb8ff` on `#141918` 8.41; `#00cc55` on white 2.15 → success text needs `#0f7a38`.

**Fixed brand tokens outside the theme** (a second, small table in `packages/brand`, not in `THEME_COLOR_ROLES`, so custom themes are not forced to define them and the Zerops surfaces keep their grammar under any theme — the same status web gives `--success`/`--info` today, `index.css:1429-1432`): status tones `ok` (dot `#66bb6a`/`#56d364`, text `#2e7d32`/`#56d364`, surface `#e8f7ec`/`#1a281e`), `busy` (blue `#42a5f5`/`#58a6ff`, surface `#eff9fd`/`#1e2e3b`), `attention` (orange `#ffa726`/`#e8a33d`, text `#b26a00`/`#ffb74d`, surface `#fff4e0`/`#3a301a`), `failed` (red `#ef5350`/`#f47067`, surface `#fdefef`/`#3a1d1a`), `off` (grey `#bdbdbd`/`#5e6e69`); identity teals `#3cbdb2`/`#00b1a3` (mark), mint `#00e5c0`; chip tints access-green `rgba(76,175,80,.15)`/`#388e3c`, region-purple `rgba(156,39,176,.15)`/`#7b1fa2`, info-chip white `.9`. Platform statuses collapse onto the five tones by a table (ACTIVE→ok; CREATING/UPGRADING/*_BUILD_*→busy; STOPPED/REPAIRING/needs-you→attention; *FAILED/DELETING→failed; RESTARTING/RELOADING→off-pulse), replacing the Material 100–400 ladder (R1 §2).

## Type

- Family: **Roboto** 400/500/700, self-hosted woff2 in `apps/web/public/fonts` (`font-display: swap`, preload; no Google Fonts fetch — containers may be offline), default in `appearanceFonts.ts:20-21` and `@theme --font-sans`; mobile `@expo-google-fonts/roboto` replacing dm-sans in `app.config.ts:108-112` and `global.css:215-217` (`font-t3-*` utilities renamed `font-medium/bold`). Mono stays T3's stack (`SF Mono, Menlo, Consolas`, `appearanceFonts.ts:25-26`) for code/diff/terminal; **hostnames are Roboto 500, not mono** (GUI: 20px/500 with `:port` at .6, `service-stack-basic-info.component.scss:6`).
- Web scale (T3's `text-base sm:text-sm` touch bump kept): body 14; row hostname 14/500 (`:port` at 60%); header name 20/500; card title 14/500; description 13/1.6 at 70% (`.u-desc`); **MicroLabel 10px/600 uppercase, tracking .06em, 45% opacity** (GUI 8–10px; 8 is unreadable on high-DPI laptops, 10 is what `zagent` and the palette group titles use); dialog title 16/600; draft hero 32/400 Roboto.
- Mobile scale is `MOBILE_TYPOGRAPHY` unchanged (`src/lib/typography.ts`: micro 11, caption 12, label 13, footnote 14, body 16, headline 18, title 21, largeTitle 26, display 30) — MicroLabel = `micro` (11, never below iOS legibility), hostname = `headline` 500, project name = `title`.

## Shape

- `--radius` stays 0.625rem = 10px = `.mat-card` (`index.css:1390`); `--control-radius` 8px = Zerops tile/chip/snack radius. **Pills** (`rounded-full`) for the `default` and `secondary` Button variants and every CTA (`ui/button.tsx` variant map; 38px line-height pills in `_material.scss:13-19`); outline/ghost/icon buttons keep 8. Status chips `rounded-[10px]`, info chips 8 (`ui/badge.tsx` gains `chip` + `status` variants). Dialogs 16px (`dialog-styles.ts`; the dark scrim stays — Zerops' white scrim is legacy). Composer keeps its 22px clip-path shell (`ChatComposer.tsx:3122`, `index.css:993-1060`) — it reads as a rounded card and the geometry is hard-coded by design. Cards borderless in light, 1px `rgba(255,255,255,.06)` in dark; **selection = 2px `#0077cc` outline** (`menu-item-card.component.scss:17-23`), mint `rgba(0,229,192,.4)` in dark. Mobile: cards 16px (Zerops 10–12 scaled for touch; today 24 — `SettingsSection`, `ConnectionsRouteScreen`), `ControlPill` already `rounded-full` (`ControlPill.tsx`), `GlassSurface` keeps 32 (native geometry, `GlassSurface.tsx:45`). Shadows: none on cards; the grey-tinted triple shadow only on popovers/dialogs (`c-soft-elevation`).

## Status vocabulary (the same four atoms on every client)

`StatusDot` (8/10/12px, radius full, tone from the fixed table; `pulse` modifier = in flight) + `MicroLabel` (the uppercase word) = **`● ACTIVE`**; `Chip` (tinted 10px pill, 10px/500 uppercase .5px: FULL ACCESS, EU CENTRAL, MOUNTED, DOMAIN PUBLIC); `Halo` (8px dot with 3px `rgba(46,125,50,.18)` ring = AUTHORIZED). Every state carries glyph + word, never colour alone (deuteranopia: `#00cc55` vs `#ffa726` vs `#cc0011` confusable — R5 risk 3). Strip tones map: idle→off/ok, active→busy, waiting→attention, done→ok.

## Iconography

Web **lucide** (1.5–2px outline, visually compatible with Material Outlined), mobile **SF Symbols** with the existing Tabler map for Android (`components/AppSymbol.tsx:86-189`); the Material Icons webfont never ships (blocking font + layout shift, AGENTS.md). One table in `packages/brand/icons.ts` maps the ~15 glyphs the Zerops surfaces use: `web_asset`→`app-window`/`macwindow`, `terminal`→`terminal`/`terminal`, `web`→`globe`, `desktop_windows`→`monitor`/`desktopcomputer`, `open_in_new`→`external-link`/`arrow.up.right.square`, `content_copy`→`copy`/`doc.on.doc`, `attachment`→`paperclip`, `tune`→`sliders-horizontal`/`slider.horizontal.3`, `search`, `arrow_back_ios`→`chevron-left`, `more_vert`→`ellipsis-vertical`, `key`→`key-round`, `sync`→`refresh-cw`, `add_circle_outline`→`circle-plus`, `info`. Brand: Zerops mark (4 paths, viewBox 0 0 42.27 50.48, `libs/zui/src/logo/`) and wordmark (`logo-full`) as path data — rendered by `<svg>` on web and `react-native-svg` on mobile, the way `T3Wordmark.tsx` already does. Service-type icons: a subset of the 87 SVGs (`libs/zef/src/shared-assets/custom-icons/`) for the map rows — an optional convention, the GUI cards are text-only (R6 §2). Provider marks `currentColor` at 1em, coloured by context (`rgba(0,0,0,.75)` rows; Claude `#d97757` only as an icon fill on light, 3.12 on white). Provider accent swatches: fallback moves from `#2563eb` (indistinguishable from primary) to neutral; `#0891b2` cyan dropped (`ProviderAccentColorPicker.tsx:12-21`).

## Motion budget

Easing `cubic-bezier(.4,0,.2,1)` (Zerops standard, R1 §7); durations 150/200/300. In-flight pulse uses the existing duty-cycled `steps()` keyframes (`status-pulse`, `status-ping`, `index.css:145-268`) on web; on mobile `ConnectionStatusDot`'s infinite reanimated `withRepeat` (`ConnectionStatusDot.tsx`) becomes a 3-frame duty cycle (1.5 s on / 3 s rest) or a static halo under reduced motion. **Deleted**: ripple rings, bounce arrows, shimmer, bokeh, sticky-header scale (all infinite in the GUI). Route enter = T3's view-transition hero→docked only. Glass stays at the existing utilities (`surface-glass`, `dialog-glass`, `dropdown-glass`).

## Mapping onto `apps/web/src/components/ui/*`

| Vocabulary | Web primitive | Mobile primitive |
|---|---|---|
| Pill / CTA | `button.tsx` `default`/`secondary` → `rounded-full` | `ControlPill` `primary`/`pill` |
| Chip / status chip | `badge.tsx` + variants `chip`, `status` | `StatusPill` (already `rounded-full`) |
| StatusDot + MicroLabel | new `status-dot.tsx`, `micro-label.tsx` | new `StatusDot.tsx`, `MicroLabel.tsx` (replaces hard-coded `ConnectionStatusDot` hexes) |
| FlatCard | `card.tsx` (`bg-card`, no shadow, 10px) | `View className="bg-card rounded-2xl"` |
| MintPanel (zcp row) | `card.tsx` tone `ok` surface | same |
| Key chip | `kbd.tsx` → `#efefef`/border `#adb3b9`/3px | n/a |
| Menu, Popover, Tooltip | `menu.tsx`, `popover.tsx`, `tooltip.tsx` on `surfaceOverlay` + soft shadow | native `UIMenu` / `AndroidAnchoredMenu` |
| Dialog / Sheet | `dialog-styles.ts` 16px, `sheet.tsx` | native form sheets |
| Toast / snack | `toast.tsx` on status-tone solids | `ErrorBanner` |
| Empty | `empty.tsx` mono heading at 60% | `EmptyState` |

## Web
## Landing (hosted and origin)

`ZeropsLandingShell.tsx` becomes the Zerops login card (`01-login.png`): mark + grey ZEROPS wordmark + "Code", subtitle from marketing ("Cloud platform for humans and their coding agents"), a white 10px card on canvas holding pill CTAs — GitHub `#24292e`, GitLab `#9b51e0`, `or` rule, outlined Email/Password fields (`ui/input.tsx`, 6px, `#d4dfea` border), blue `#0077cc` "Login using email" pill — and the two footer link-chips. TOTP is a second card with a 6-cell `input-group`. Origin mode adds one line under the wordmark: `● ACTIVE  kanban · zcp` (the project the descriptor named). `AuthSurfaceShell`'s blue gradient (`#1e61de→#17348e`, `AuthSurfaceShell.tsx:17-32`) is deleted.

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                                  │
│                                     Z ZEROPS   Code                                              │
│                        Cloud platform for humans and their coding agents                         │
│                                                                                                  │
│                          ┌────────────────────────────────────────┐                              │
│                          │   (      Login using GitHub         )  │                              │
│                          │   (      Login using GitLab         )  │                              │
│                          │   ───────────────  or  ─────────────── │                              │
│                          │   ┌ Email ───────────────────────────┐ │                              │
│                          │   └──────────────────────────────────┘ │                              │
│                          │   ┌ Password ─────────────────────  ◉┐ │                              │
│                          │   └──────────────────────────────────┘ │                              │
│                          │   (       Login using email         )  │                              │
│                          └────────────────────────────────────────┘                              │
│                     Don't have an account yet?  [Create a new Zerops account]                    │
│                                                                                                  │
└──────────────────────────────────────────────────────────────────────────────────────────────────┘
```

## Picker (`/zerops`, `ZeropsProjectsPage.tsx` + `ZeropsProjectPicker.tsx`)

The Zerops dashboard in miniature (`03-after-login.png`): app-bar row with the org pill and ⌘K pill, content header with search field + `Created ↓` sort pill, then **project cards** (white, 10px, borderless) in a responsive grid, grouped by the four candidate states as MicroLabel group titles. Card anatomy: StatusDot + MicroLabel + project name 20/500 + `›`; chips FULL ACCESS / EU CENTRAL (PRG1); one line `zcp · Zerops Code ready · N services`; one pill: `Open` (Connected), `Connect`, `Enable Zerops Code` with "≈20 s, restarts the container" (Ready to connect), a progress MicroLabel `setting up infrastructure · 1:12 / 5:00` (Preparing — the waiter caps from spec §4.4 shown as elapsed/cap), reason text (Not available). `New project` is a dashed card (`__project-add-button` idiom) opening the name + org-pill form inline. Every candidate is one card, including several zcp containers in one project (Z3C-2).

```
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│ Z KRLS ▾   ⌕ ⌘K        Projects                        ⌕ Search projects…          Created ↓     │
├──────────────────────────────────────────────────────────────────────────────────────────────────┤
│ CONNECTED                                                                                        │
│ ┌──────────────────────────────────────┐  ┌──────────────────────────────────────┐               │
│ │ ● ACTIVE  kanban                  ›  │  │ ● ACTIVE  shop                    ›  │               │
│ │ FULL ACCESS   EU CENTRAL (PRG1)      │  │ FULL ACCESS   EU CENTRAL (PRG1)      │               │
│ │ zcp · Zerops Code ready · 3 services │  │ zcp · Zerops Code ready · 5 services │               │
│ │ (            Open            )       │  │ (            Open            )       │               │
│ └──────────────────────────────────────┘  └──────────────────────────────────────┘               │
│ READY TO CONNECT                                                                                 │
│ ┌──────────────────────────────────────┐                                                         │
│ │ ● ACTIVE  legacy                     │                                                         │
│ │ zcp · needs Zerops Code              │                                                         │
│ │ (     Enable Zerops Code     ) ~20 s │                                                         │
│ └──────────────────────────────────────┘                                                         │
│ PREPARING                                                                                        │
│ ┌──────────────────────────────────────┐  ┌ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┐                │
│ │ ◔ CREATING  blog                     │      ＋  New project                                    │
│ │ setting up infrastructure · 1:12/5:00│  └ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┘                │
│ └──────────────────────────────────────┘                                                         │
│ NOT AVAILABLE   demo — project STOPPED   ·   tools — no public subdomain                         │
└──────────────────────────────────────────────────────────────────────────────────────────────────┘
```

## Thread view (`ChatView.tsx`)

- **Header (52px)**: breadcrumb `kanban / Build the kanban board` (project favicon replaced by a StatusDot for the project); the **strip pill** — a 28px pill on `toolbarControl` with StatusDot + MicroLabel phrase (`● DEVELOPING kanbandev`, `○ WAITING FOR YOU` on attention tint, `✓ TASK COMPLETE` on ok tint, `◔ zerops_deploy RUNNING` busy); click opens the map panel; phrases verbatim from `zerops/strip.ts`. Actions: `Cloud IDE` outline pill, Map toggle, panel controls. On projects that are not Zerops the strip is absent and `GitActionsControl` returns.
- **Timeline** (`MessagesTimeline.tsx`, LegendList, untouched mechanics): user bubble on `messageSurface` `#e3f4ff` 2xl; assistant markdown; Zerops cards as FlatCards with a tone stripe: plan/question `#fff4e0`, import/deploy-in-progress `#eff9fd` with the 30px-grid process steps idiom (17px glyph in a 2px-bordered circle: schedule/play/done/priority_high → lucide `clock`/`play`/`check`/`alert`), deploy done + verify `#e8f7ec` with `✓ HTTP 200 · healthy` check rows and a URL chip (`UrlChip`, info tint), structured error `#fdefef` with code chip `SSH_DEPLOY_FAILED`, hint line, `network` chip, log lines in a 200px scroll (the `zui-errors-printer` idiom); undecodable → generic tool block (Z3F-7).
- **Question card** (`ComposerPendingUserInputPanel.tsx`): header MicroLabel `● WAITING FOR YOU`, question 14/500, options as white pills with a digit key chip, descriptions 13/70%, multi-select as checkboxes, visible `Other…` free-text pill, arrow keys.
- **Credential card**: same frame, attention tint, one secure field, target `Store as service secret on kanbandev`, blue pill `Store`; after success a green MicroLabel `STORED · kanbandev` and a `Replace` link; nothing of the value in the thread.
- **Composer** (`ChatComposer.tsx`): unchanged geometry; send button `messageAction` blue; model row reads `✳ Claude Code ▾ · High ▾ · Full access ▾` (provider → coding agent; defaults to the first authenticated agent — R6 §4 landing defect). The context strip under it becomes the **Zerops context strip**: `kanban · kanbandev` + quick-action chips (`ZeropsQuickActions.tsx` outline pills, prefill only). "Local checkout · main" and worktree UI are gone (D4).

```
┌────────────┬───────────────────────────────────────────────────────────────────┬─────────────────┐
│ Z KRLS ▾   │ kanban / Build the kanban board   ● DEVELOPING kanbandev  [IDE][▤]│ SERVICES     ×  │
│ ⌕ ⌘K       ├───────────────────────────────────────────────────────────────────┤ RUNTIMES        │
│            │        ┌ you ────────────────────────────────────────────────┐    │ ● ACTIVE        │
│ PROJECTS   │        │ Vytvoř mi kanban.                                   │    │ kanbandev:3000  │
│ ┌────────┐ │        └─────────────────────────────────────────────────────┘    │ nodejs@22       │
│ │● kanban│ │  Proposed: kanbandev + kanbanstage (nodejs@22), db (postgresql).  │ mounted  (Open) │
│ │3 svcs  │ │  ┌ question ─ ● WAITING FOR YOU ────────────────────────────────┐ │ └○ READY        │
│ └────────┘ │  │ Create these 3 services?                                     │ │   kanbanstage   │
│ ●Build the │  │ (1 Confirm) (2 Change) (3 Other…)                            │ │ DATA            │
│  kanban now│  └──────────────────────────────────────────────────────────────┘ │ ● ACTIVE db     │
│  Add auth  │  ┌ deploy kanbandev ─ ✓ deployed · 41s ─ ↗ kanbandev-x.zerops.app┐│ postgresql@18   │
│  2h        │  └──────────────────────────────────────────────────────────────┘ │ (Data Console)  │
│            │  ┌ verify kanbandev ─ ✓ HTTP 200 · healthy ─────────────────────┐ │ INFRASTRUCTURE  │
│ ● shop     │  └──────────────────────────────────────────────────────────────┘ │ ┌─────────────┐ │
│  Fix cart  │                                                                   │ │● ACTIVE zcp │ │
│  ◐ 1d      │  ┌───────────────────────────────────────────────────────────────┐│ │(Web Termin.)│ │
│            │  │ Ask for changes…                                              ││ │(Cloud IDE)  │ │
│            │  │ ✳ Claude Code ▾ · High ▾ · Full access ▾                 (↑)  ││ │✳ Claude Code│ │
│            │  └───────────────────────────────────────────────────────────────┘│ │  AUTHORIZED │ │
│ ⚙  ☁  ▮    │   kanban · kanbandev   (Deploy) (Show logs) (Add Redis)           │ └─────────────┘ │
└────────────┴───────────────────────────────────────────────────────────────────┴─────────────────┘
```

## Right panel — service map (kind `zerops`, `ZeropsPanel.tsx` / `ZeropsServiceMap.tsx`)

Header `SERVICES` MicroLabel + live indicator (`live` / `polling` one quiet line / degraded warning). Groups RUNTIMES / DATA / INFRASTRUCTURE as MicroLabel titles (order from `serviceMap.ts:20-24`, Z3F-2). **ServiceRow** anatomy: StatusDot + MicroLabel status (platform word, collapsed to a tone; spinner glyph while `transient`) · hostname 14/500 with `:port` at 60% · type `nodejs@22` 12px muted · chips MOUNTED (`mountPath`) · `Open` white pill when `subdomainUrl` · stage row indented with `└` and its own dot (dev↔stage pair) · `production: <link>` line. A DATA row gains a `Data Console` pill (opens kind `data`). The zcp row under INFRASTRUCTURE is the **MintPanel**: `#e8f7ec` 6px, header `● ACTIVE zcp`, the four white pills (Web Terminal → T3 terminal panel, SSH → copies the command, Cloud IDE → code-server, Desktop IDEs → popover), and the auth tray with `✳ Claude Code · AUTHORIZED` haloed dot. Add-menu entry text: "Services — the project's live map" (replaces "See the project's services"). Other kinds (`diff/files/file/preview/terminal/pull-request/agents`) unchanged except `pull-request` hidden on Zerops projects.

## Settings

Sidebar nav sections (`settingsSearch.ts:28-38`): General, Appearance, Keybindings, **Coding agents**, Integrations, Source control, **Devices**, **Zerops**, Archive. Appearance: the "Zerops" card is the standard card (light/dark orbs from `themePreview.ts` fed by generated brand colours), the library (T3 Chat, Grove…) stays but the copy reads "Choose how Zerops Code looks"; contrast and glass sliders unchanged; "Environment identification" row removed (no stage art). Zerops page (`ZeropsSettings.tsx`): account card (email, org chips, Sign out), Projects link, "Reset to Zerops theme". Devices (`ConnectionsSettings`): device cards `Zerops Code · Safari on iOS` with last-seen MicroLabel and a `Remove` link; `Connect another device` pill → QR + link (the one-time-token path, never the word pairing).

## Command palette

`CommandPalette.tsx` restyled to the Zerops palette (`06`/`07` shots): search row 16/18px, scope chip in teal tint `rgba(0,204,187,.13)` 6px, group titles MicroLabel at 45%, 40px rows 8px radius, 28px icon tiles, key chips in the footer (`kbd.tsx`). Zerops intents listed in IA.

## Desktop
Desktop is the web bundle in Electron with four additions and one subtraction.

- **Titlebar / WCO.** Keep `titleBarStyle: "hiddenInset"` + traffic lights at (16, 18) on macOS and `hidden` + `titleBarOverlay` elsewhere (`apps/desktop/src/window/DesktopWindow.ts:216-223`); the overlay's symbol colours (`TITLEBAR_LIGHT_SYMBOL_COLOR #1f2937`, `DARK #f8fafc`, `:34-35`) and the initial window background (`getInitialWindowBackgroundColor` `#0a0a0a`/`#ffffff`, `:128-130`) are replaced by imports from the generated `packages/shared/src/generated/brandColors.ts` (canvas `#eceff3`/`#0c0f0e`, text `#1a1a1a`/`#e9eeec`) so the first frame is already the Zerops canvas. `TITLEBAR_HEIGHT 40` matches the Zerops app-bar height; the web `.wco` block (`index.css:124-137`) keeps its `env(titlebar-area-*)` overrides — the sidebar header's mark pill sits inside the reserved inset (`COLLAPSED_SIDEBAR_TITLEBAR_INSET_CLASS`, `workspaceTitlebar.ts`). Window title `Zerops Code — <project>` via `DocumentTitleSync` (`__root.tsx:203-219`).
- **Icons.** One Icon Composer project per channel — `assets/{dev,nightly,prod}/app-icon.icon` (`scripts/lib/brand-assets.ts:1-32`) — rebuilt around the Zerops mark: prod = two-teal mark on white/graphite, nightly = mint-on-graphite, dev = outline "blueprint". `pnpm icons:export` regenerates macOS/iOS/universal PNGs, Windows `.ico`, favicons, apple-touch; `icons:check` gates CI (add it to the Check job in `.github/workflows/ci.yml`, next to `imported-lock --check`). `assets/prod/logo.svg` becomes the Zerops mark SVG. Needs macOS tooling (`assets/README.md`) — an owner/desk task, not agent work.
- **Naming.** `DesktopAppBranding {baseName, stageLabel, displayName}` (`packages/contracts/src/ipc.ts:175-185`) is set from `apps/desktop/src/app/DesktopEnvironment.ts:88-110` → `baseName "Zerops Code"`, stage pill Dev/Nightly/Latest kept as a MicroLabel in the sidebar header (no artwork). `branding.ts:20-27` fallback, `ElectronMenu.ts` labels ("About Zerops Code", "Check for updates"), `manifest.webmanifest`/`index.html` `theme-color` follow the generator. URL scheme `t3code://` stays (D6 out of scope) — the only T3 residue a user can meet, in the OS "open with" dialog; record it in `hacks.md`.
- **Remote-only.** Per `fork.md §4` desktop loses the local backend (`t3 serve` spawn) and keeps SSH launch, keychain, deep link, updater; the product flow is the hosted one in a window: sign in with Zerops → picker → thread. The one desktop-only surface that earns its place: the **Browser** right-panel kind (`PreviewPanel`, "Only available in the desktop app" in `03-right-panel-picker.png`) becomes the in-app preview of a deployed subdomain — a map row's `Open` offers "Preview here" on desktop, the verify card's URL chip too.
- **Two update pills, one colour.** Desktop self-updates (updater kept); the container's z3 updates through a restart. Both render the `update` role (teal/mint) — "Zerops Code 0.2 available · Restart" for desktop, "Enable the new Zerops Code on kanban · restarts the container" for the project — so mint keeps its single meaning (brand-positive, non-action) across shells.
- **Subtraction.** `SidebarStageBackdrop` art, Tailscale exposure settings/IPC (already a deletion slice), the local-backend settings pane.
- **Proof.** `apps/desktop/scripts/smoke-test.mjs` boots Electron and greps fatal patterns; extend it with `webContents.capturePage()` into `artifacts/desktop/` for both appearances — a visual smoke, reviewed not gated.

## Mobile
## What the phone is for

Watching and steering, not authoring: read the strip, answer a question card, approve a plan, store a credential, open the deployed URL, glance at the map, send a short follow-up, peek at the terminal or a diff. Long specs are typed on the laptop. Every screen is built so a thread can be understood from its header alone.

## Zerops mobile design language (defined here — the GUI has no mobile precedent, `index.html` `width=1200`)

The rule: **native containers, Zerops contents.** Everything that is a container stays native and untouched (R3 §4 "must stay native"): native-stack with flat thread routes and iOS 26 header morphing (`Stack.tsx:229-233`), transparent glass `UINavigationBar` with scroll-edge effects and `"editor"` title style (`Stack.tsx:89-100`), native header items from the JSX DSL with SF Symbols only (`native/StackHeader.tsx:219-224`), form sheets with detents and grabber, the Mail-style search toolbar, `UIMenu`, the keyboard stack, haptics, Live Activities, predictive back, Android `AndroidScreenHeader`/`AndroidAnchoredMenu`/FAB. What sits inside them takes the Zerops grammar through tokens and the vocabulary:

- Screen = canvas `#eceff3` (`--color-screen ← canvas`), cards white 16px borderless (`--color-card ← surfaceRaised`), dark = `#0c0f0e`/`#141918` (`createMobileThemeVariables`, `mobileTheme.ts:208-283` — every one of the 65 tokens derives from the 57 roles, so `ZEROPS_THEME` reaches headers, sheets, glass tint, composer, markdown, bubble, switches and the navigation theme with no per-screen work).
- Type: Roboto 400/500/700 in content; **native bar titles and glass header items keep SF** (they are system-drawn) — a deliberate seam, stated in the spec, not hidden.
- MicroLabel at 11px (`micro`), StatusDot 8px + halo; `StatusPill` keeps `rounded-full` and takes tone classes from the fixed status table instead of `threadPresentation.ts`'s iOS system hexes (`:30-123`) and `ConnectionStatusDot`'s literals.
- Action colour: `ControlPill primary` = `bg-primary ← accent` `#0077cc` white text (today near-black); `user-bubble ← messageSurface` `#e3f4ff` with dark text (no iMessage blue). Teal appears only in the mark and the update pill.
- Glass tint `--color-glass-tint ← surfaceOverlay @ .22` — on the grey canvas the glass reads cool and slightly green in dark, which is the Zerops dark ramp doing its job.
- Motion: reanimated pulses duty-cycled; haptics untouched.
- Android: the same tokens through `AndroidScreenHeader` (`bg-header`, 44dp round `bg-subtle` buttons — already pill-shaped) and the two config plugins (`withAndroidModernPopupMenu.cjs:67-70`, `withAndroidModernAlertDialog.cjs:33-46`) whose copied palette values become generated.

## Screens (route names from `src/Stack.tsx`)

| Route | Zerops shape |
|---|---|
| `Home` | Projects (= environments) as white cards: `● ACTIVE kanban · 3 svcs`, the live thread's strip phrase as a second line, then threads (`ThreadListRow`) with relative time; `CompactBrandTitle` lockup → Zerops mark + "Code" (+ stage MicroLabel); search stays the iOS 26 toolbar; FAB / `＋` = new thread |
| Sign in (new, S5) | the login card as a form sheet: GitHub/GitLab pills, email/password, TOTP; registration = Turnstile in a web view or the app.zerops.io hand-off |
| Picker (new) | `SettingsEnvironments` becomes **Projects**: cards with the four candidate groups; `Enable Zerops Code` pill with the 20 s note; provisioning progress as a MicroLabel; replaces `ConnectionsNewRouteScreen`'s host + code fields for Zerops (the manual path stays reachable below a divider — poc-findings `ManualConnectFallback`) |
| `Thread` | glass header: back + project, title; **subtitle line = strip** (`● DEVELOPING kanbandev`, the `WorkspaceConnectionTitle` slot); right items ▤ (map sheet) and ⋯; feed cards per the vocabulary; `PendingUserInputCard` gains descriptions + `Other…`; credential card = a form sheet with a secure field; quick-action chips above the composer; composer `GlassSurface` unchanged |
| Map (new, sheet) | detents [0.55, 0.92]; header `kanban · SERVICES` + strip; groups as MicroLabels; ServiceRow cards; `Open` opens Safari; `Data Console` = "Open in Zerops" v1; MintPanel for zcp with Web Terminal → `ThreadTerminal`, Cloud IDE → Safari |
| `ThreadTerminal`, `ThreadReview`, `ThreadFiles` | unchanged; files grouped by service hostname; diff by service |
| `SettingsSheet` | sections: Zerops account, Projects, Devices, Coding agents, Appearance (Zerops is the standard card), Archive |
| Live Activity | title "Zerops Code", logo asset = Zerops mark (`plugins/withWidgetLogoAsset.cjs:23-26`, frame ratio in `AgentActivity.tsx:233`), phase tints from the fixed status table |

```
┌──────────────────────────────────────┐
│ Z Code                    ⌕    ⋯     │
├──────────────────────────────────────┤
│ PROJECTS                             │
│ ┌──────────────────────────────────┐ │
│ │ ● ACTIVE  kanban        3 svcs   │ │
│ │ ● DEVELOPING kanbandev           │ │
│ │  Build the kanban board     now  │ │
│ │  Add auth                    2h  │ │
│ └──────────────────────────────────┘ │
│ ┌──────────────────────────────────┐ │
│ │ ● ACTIVE  shop          5 svcs   │ │
│ │ ○ WAITING FOR YOU                │ │
│ │  Fix cart                    1d  │ │
│ └──────────────────────────────────┘ │
│ ┌──────────────────────────────────┐ │
│ │ ◔ CREATING  blog                 │ │
│ │  setting up infrastructure       │ │
│ └──────────────────────────────────┘ │
│                                      │
│                              ( ＋ )  │
└──────────────────────────────────────┘
```

```
┌──────────────────────────────────────┐
│ ‹ kanban   Build the kanban board    │
│   ● DEVELOPING kanbandev      ▤  ⋯   │
├──────────────────────────────────────┤
│            ┌───────────────────────┐ │
│            │ Vytvoř mi kanban.     │ │
│            └───────────────────────┘ │
│ Proposed kanbandev, kanbanstage, db. │
│ ┌──────────────────────────────────┐ │
│ │ ● WAITING FOR YOU                │ │
│ │ Create these 3 services?         │ │
│ │ (Confirm) (Change) (Other…)      │ │
│ └──────────────────────────────────┘ │
│ ┌──────────────────────────────────┐ │
│ │ ✓ deployed kanbandev · 41s       │ │
│ │ ↗ kanbandev-x.zerops.app         │ │
│ └──────────────────────────────────┘ │
│ ┌──────────────────────────────────┐ │
│ │ ✓ HTTP 200 · healthy             │ │
│ └──────────────────────────────────┘ │
│ (Deploy) (Show logs) (Add Redis)     │
│ ╭──────────────────────────────────╮ │
│ │ Ask for changes…           (↑)   │ │
│ ╰──────────────────────────────────╯ │
└──────────────────────────────────────┘
```

```
┌──────────────────────────────────────┐
│                ───                   │
│ kanban · SERVICES              Done  │
│ ● DEVELOPING kanbandev               │
├──────────────────────────────────────┤
│ RUNTIMES                             │
│ ┌──────────────────────────────────┐ │
│ │ ● ACTIVE  kanbandev:3000         │ │
│ │ nodejs@22 · mounted      (Open)  │ │
│ │ └ ○ READY  kanbanstage           │ │
│ └──────────────────────────────────┘ │
│ DATA                                 │
│ ┌──────────────────────────────────┐ │
│ │ ● ACTIVE  db   postgresql@18     │ │
│ │                  (Data Console)  │ │
│ └──────────────────────────────────┘ │
│ INFRASTRUCTURE                       │
│ ┌──────────────────────────────────┐ │
│ │ ● ACTIVE  zcp                    │ │
│ │ (Web Terminal)  (Cloud IDE)      │ │
│ │ ✳ Claude Code · AUTHORIZED       │ │
│ └──────────────────────────────────┘ │
│ live · updated 2 s ago               │
└──────────────────────────────────────┘
```

## Tablet

`AdaptiveWorkspaceLayout` split view (`features/layout/AdaptiveWorkspaceLayout.tsx`): sidebar column = Home (projects → threads) on canvas, detail = thread, and the **inspector pane** (`workspace-inspector-pane.tsx`, today the file navigator) gains the map as a persistent third column — the closest a tablet gets to the web three-column view. `WorkspaceEmptyDetail` shows the project card + strip when no thread is open.

## Where this is weak

Mobile has zero Zerops code on `main` (`grep -ri zerops apps/mobile/src` → 0) and every new screen above depends on promoting the UI-free web modules (`apps/web/src/zerops/{candidates,provisioning,containerHealth,firstPrompt,serviceMap,strip,cards/payloads,quickActions}.ts`) into `packages/client-runtime` (R3 §6). The design can be specified now; it cannot be built before that promotion. Roboto next to SF in the same header is a visible seam; the alternative (SF everywhere on iOS) would make the phone the one Zerops surface that does not look like Zerops, and the brief chose "looks like Zerops".

## Cross-client system
## One source, three projections

```
packages/shared/src/themePalettes.ts   ZEROPS_THEME (57 roles, light + variants.dark)  ── the ONLY place a Zerops chrome colour is typed
packages/brand/                        mark/wordmark path data · status tones · chip tints · radius/type constants ·
                                       icon map (Material→lucide/SF) · service-icon subset · provider marks · copy glossary
scripts/generate-theme-tokens.ts       (root, beside export-brand-icons.ts; `generate:theme` / `generate:theme:check`)
   ├─► apps/web/src/generated/zerops-theme.css      :root{--app-theme-*} + @variant dark  → index.css keeps only the semantic remap (l.1552-1616); the hand-written :root/.dark blocks (l.1388-1494) and [data-app-sidebar] literals are deleted
   ├─► apps/web/index.html boot snippet             SPLASH_COLORS / DEFAULT_THEME_PALETTES / BUILT_IN_THEME_PALETTES["zerops"] / <meta theme-color> (today "Keep this small boot-time copy in sync", l.13-100)
   ├─► apps/web/public/manifest.webmanifest         theme_color / background_color
   ├─► packages/shared/src/generated/brandColors.ts consumed by themePreview.ts STANDARD_THEME_PREVIEW_COLORS, DesktopWindow.ts (bg + titlebar symbols), mobile app.config.ts splash (#ffffff/#0a0a0a today), withAndroidModern{PopupMenu,AlertDialog}.cjs, Live Activity tints
   ├─► apps/mobile/global.css @variant light/dark   rendered from ZEROPS_THEME by extending renderVariant (generate-uniwind-themes.mts:166-167 emits adaptive vars only today); MOBILE_DEFAULT_THEME_ID = "zerops"
   └─► apps/mobile/src/generated-uniwind-*.{css,json}  unchanged pipeline (createMobileThemeVariables, mobileTheme.ts:208-283)
scripts/export-brand-icons.ts          Icon Composer .icon per channel → app icons, favicons, apple-touch (pnpm icons:export / icons:check)
```

Web then selects `zerops` as the default in `useTheme.ts:39-45` (`DEFAULT_THEME_SNAPSHOT.theme`), the boot script's `isKnownTheme ? stored : "zerops"`, `getDefaultThemeColors` (`themePalette.ts:1224`), and the T3 Chat fallbacks that today paint a partial theme pink (R5 risk 5). The "T3 Code" standard card in `ThemePreviewCircles.tsx:58-67` becomes "Zerops".

## Shared vs per-client

| Shared (one copy) | Per client (native implementation) |
|---|---|
| colour roles + fixed brand tokens; radius/type constants; icon map; mark path data; copy glossary (`packages/brand`) | primitives: web `components/ui/*` (Base UI + cva), mobile RN components + native module colour payloads |
| pure behaviour: `strip.ts`, `serviceMap.ts`, `cards/payloads.ts`, `quickActions.ts`, `candidates.ts`, `provisioning.ts`, `containerHealth.ts`, `firstPrompt.ts` — promoted to `packages/client-runtime/src/zerops/` so web and mobile render the same phrase for the same envelope | layout, navigation, sheets, glass, menus, keyboard, haptics, WCO |
| the vocabulary spec (anatomy + states per component) | pixels |
| screenshot scene list (which states every client must be able to show) | the capture tool |

No UI-shape code is shared today (R3 §7: `client-runtime` is pure TS) and the concept keeps it that way — sharing components across RN and DOM would fight both shells.

## Drift guards (tests, not reviews)

1. `scripts/theme-tokens.test.ts` — byte-equality of every generated file against a fresh render (the pattern of `generate-uniwind-themes.test.ts` "keeps the committed outputs current"); `Object.keys(ZEROPS_THEME.colors)` ⊇ `THEME_COLOR_ROLES` for both appearances; every value opaque; contrast ≥4.5 for text/canvas, text/surface, messageActionForeground/messageAction, accentSurfaceForeground/accentSurface, errorForeground/errorSurface, warningForeground/warningSurface, updateForeground/updateSurface, statusText/statusSurface per tone; ≥3.0 focus/canvas; `sidebarMutedForeground` 60%-mix over `sidebar` ≥3.0 (the `--sidebar-icon-color` derivation, `index.css:94-98`).
2. `generate:theme --check` and `icons:check` in the CI **Check** job (`.github/workflows/ci.yml`, after `imported-lock --check`), same shape as the existing `--check` flag in the mobile generator.
3. **No-literal lint** on the Zerops surfaces first, then repo-wide with an allowlist: a regex test (the `z3-zone-architecture.test.ts` style — no AST) that `apps/web/src/components/zerops/**`, `apps/web/src/components/ui/**`, `apps/mobile/src/features/zerops/**` and the vocabulary components contain no `#hex`, no `rgba(`, no Tailwind palette utility (`(bg|text|border)-(blue|emerald|violet|amber|…)-\d`). Allowlist: brand icons (`Icons.tsx`, `ProviderIcon.tsx`, `SourceControlIcon.tsx`), terminal ANSI, shiki/Pierre themes. Today's counts to burn down: 217 palette utilities on web (hotspots `ResourceTelemetryDiagnostics.tsx` 38, `Sidebar.tsx` 34, `Sidebar.logic.ts` 32, `pullRequestPresentation.tsx` 21, `ChatMarkdown.tsx` 15, `ThreadStatusIndicators.tsx` 14), 155 hex literals in 18 mobile files.
4. **Vocabulary lint**: forbidden user-facing words (`T3 Code`, `T3 Connect`, `Tailscale`, `pairing`, `worktree`, `Local checkout`, `environment` as a noun in JSX text, `control plane` applied to z3) over string literals in `apps/{web,mobile}/src/**` — heuristic (quoted strings and JSX text only, identifiers excluded), with an allowlist for the identity-door copy that must say "Zerops Control Plane" about the container.
5. **Strip parity**: `strip.test.ts` already asserts phrases verbatim; once promoted to `client-runtime` both clients import the same function, so parity is structural.
6. **Default-is-a-theme guard**: a test that `index.css` contains no `:root { --background:` literal and `global.css` no hand-written `@variant light {` colour — the generated files are the only defaults.

## Visual regression harness

- **Mobile**: exists — `pnpm screenshots:mobile` (`scripts/mobile-showcase.ts`, `docs/operations/mobile-app-store-screenshots.md`) drives the real app against seeded disposable servers, per theme (`SHOWCASE_THEMES = MOBILE_THEME_IDS`, so `zerops` is captured automatically) and appearance; add scenes Projects (picker), Map sheet, Thread-with-question-card, Thread-with-credential-card by seeding an envelope and a `user-input.requested` into the showcase environment (`scripts/mobile-showcase-environment.ts`).
- **Web**: none today (`apps/web/vite.config.ts` has only the unit project). Add `scripts/web-showcase.ts` reusing `mobile-showcase-environment.ts`'s seeded servers, driving Chromium (Playwright or vitest browser mode) through landing, picker, thread (idle / developing / waiting / done / error card), map panel, settings, palette, at 1440×900 and 390×844, light + dark → `artifacts/web/`.
- **Desktop**: `smoke-test.mjs` + `capturePage()` (see Desktop).
- **Policy**: captures are CI artifacts attached to the PR (AGENTS.md already requires before/after images for UI changes), diffed perceptually and *reviewed*; they are not a pixel gate — font rasterisation differs per OS and a gate would flake. The gate is the token/lint tests above; the harness catches what tokens cannot (a card that lost its stripe).

## Where it lives, and governance

- Zones (`docs/internals/zerops/fork.md §3`): `packages/shared/src/themePalettes.ts`, `apps/web/src/components/ui/**`, `apps/mobile/**`, `apps/desktop/**` are **owned core**; `packages/brand/`, `apps/web/src/components/zerops/**`, `apps/web/src/zerops/**` are **owned product** (add `packages/brand/**` to the table; the zone test needs no new import rule since brand imports nothing). The ported provider zone is untouched.
- Design decisions land in `zcp/docs/spec-z3.md` (fork.md §5 "design → spec-z3.md"): a new section **"The client design system"** (§8 — §7 is claimed by the SPI in the freeze checklist) holding the invariants as DS-1… rows pinned by the tests above: DS-1 every Zerops chrome colour is a `ZEROPS_THEME` role or a `packages/brand` token (lint); DS-2 generated copies are byte-equal to the source (`generate:theme --check`); DS-3 primary is blue, teal/mint is identity-and-update only (contrast test forbids teal text on light canvas); DS-4 every status carries dot + word (component test); DS-5 no infinite animation (grep for `infinite` outside the duty-cycled keyframes; mobile `withRepeat(-1)` allowlist); DS-6 the strip phrase is one shared function.
- The working spec — anatomy and states per vocabulary component, the icon map, the copy glossary — lives in `docs/internals/zerops/design-system.md` in the fork (reference, dated, like the other ledger files); user-visible vocabulary in `docs/user/`.
- Ownership: one writer edits the ledger; every token change is a PR touching `themePalettes.ts` or `packages/brand` plus regenerated files, with the harness captures attached — the same loop as `imported.lock`.

## Where the angle is weak

A shared *spec* is not shared *code*: two implementations of QuestionCard can still diverge in behaviour (keyboard handling, "Other" placement). The mitigation is that behaviour lives in `client-runtime` and the spec names states, not that pixels match — pixel parity is explicitly not the goal, native parity is. And the generator adds a build step agents must remember; the `--check` in CI is what makes forgetting cheap rather than silent.

## Migration
Effort classes: S ≤ 1 slice, M 2–4 slices, L a stream. Each phase is independently shippable; "throwaway" names work a later phase deletes.

| Phase | What | Files / areas (citing R2/R3/R5) | Effort | Throwaway |
|---|---|---|---|---|
| **P0 — words and marks** | Product name, wordmark, titles, client labels, vocabulary lint; no colour change | web `branding.ts:20-27`, `sidebar/SidebarChrome.tsx:80-118` (mark pill replaces `T3Wordmark`), `SplashScreen.tsx`, `index.html:434,459-464`, `routes/_chat.tsx:130-134`, `_chat.index.tsx:185`, `ThemeSettings.tsx:852`, `PairingRouteSurface.tsx` copy, `clientMetadata.ts:80-94`, `settingsSearch.ts:28-38` (Devices, Coding agents), `SidebarStageBackdrop.tsx` (delete); desktop `DesktopEnvironment.ts:88-110`, `ElectronMenu.ts`; mobile `BrandMark.tsx`, `T3Wordmark.tsx`→`ZeropsMark.tsx`, `CompactBrandTitle.tsx`, `mobileBranding.ts`, `authClientMetadata.ts:10`, `remoteRegistration.ts:505-506`, `app.config.ts:65-90` names (bundle ids and `t3code://` stay — D6), 26 string files (R3 §5); tests `branding.test.ts`, `clientMetadata.test.ts`, `SidebarStageBackdrop.test.tsx` | S–M | none |
| **P1 — the theme, by the existing path** (R5 option a, step 1) | `ZEROPS_THEME` (57 roles × 2) in `packages/shared/src/themePalettes.ts`; id in `BUILT_IN_THEME_IDS`, `RESERVED_THEME_IDS` (`themePalette.ts:59-73`), `MOBILE_DEFAULT_THEME_ID`; web default via `useTheme.ts:39-45` + boot script default; `getDefaultThemeColors` → ZEROPS; fixed brand tokens as `--success/--info/--status-*` values in `index.css:1429-1432` and mobile `threadPresentation.ts`/`ConnectionStatusDot.tsx`; provider swatch fallback (`ProviderAccentColorPicker.tsx:12-21`); `pnpm --filter @t3tools/mobile generate`; generator test name list | M | the hand-edited `BUILT_IN_THEME_PALETTES["zerops"]` block in `index.html` (P2 generates it) |
| **P2 — generator + brand package, delete the hand copies** | `packages/brand/` (tones, mark paths, constants, icon map); `scripts/generate-theme-tokens.ts` + `theme-tokens.test.ts` + `generate:theme(:check)` root scripts + CI Check step; outputs listed in crossClientSystem; delete `index.css:1388-1494` literals, `[data-app-sidebar]` @1494-1541, `T3_CODE_*_THEME_COLORS` (`themePalette.ts:307-432`), `STANDARD_THEME_PREVIEW_COLORS`, `DesktopWindow.ts:34-35,128-130` literals, `app.config.ts:305-317` splash, plugin palettes (`withAndroidModern*.cjs`), `global.css` hand block (extend `renderVariant`, `generate-uniwind-themes.mts:166-167`); no-literal lint on `components/zerops/**` and `ui/**` | M | none |
| **P3 — primitives and type** | web `ui/button.tsx` (pill default/secondary), `badge.tsx` (chip/status), `card.tsx`, `input.tsx`, `dialog-styles.ts` (16px), `sidebar.tsx:799-830`, `toast.tsx`, `kbd.tsx`, `empty.tsx`, `menu/popover/tooltip` shadows; new `status-dot.tsx`, `micro-label.tsx`; Roboto woff2 in `apps/web/public/fonts`, `appearanceFonts.ts:20-21`, `@theme` @139-144; snapshot tests `button.test.tsx`, `menu.test.tsx`, `sidebar.test.tsx`, `command.test.tsx`; mobile Roboto in `app.config.ts:108-112,241-264` + `global.css:215-217` + alert-dialog plugin font, radii 24→16 (`SettingsSection`, `ConnectionsRouteScreen`, `EmptyState`, `ThemeCard`), `ControlPill` primary blue, `StatusPill` tones, new `StatusDot`/`MicroLabel`; 217-utility sweep on web hotspots, 155-literal sweep on mobile (R2 §3, R3 §2) | M | none |
| **P4 — Zerops surfaces to the vocabulary (web)** | strip pill (`ZeropsLifecycleStrip.tsx`), map (`ZeropsServiceMap.tsx`/`ZeropsPanel.tsx`: ServiceRow, MintPanel, Data Console pill), cards (`ZeropsToolCard.tsx`: tone stripes, process-step idiom, escape the fold in `MessagesTimeline.tsx:2605`), question card (`ComposerPendingUserInputPanel.tsx`), **credential card** (new; verb shape is owner-designed), **`data` panel kind** (`rightPanelStore.ts:17-27`, `RightPanelTabs.tsx`), quick-action context strip (`ChatView.tsx:7158-7188`), landing + picker + provisioning (`components/zerops/landing/*`, `ZeropsProjectPicker.tsx`, `ZeropsProvisioningPanel.tsx`), settings pages, palette intents, `GitActionsControl` hidden on Zerops; single-bundle landing gate (`__root.tsx:62-83`, `hostedPairing.ts:35-46`, `-chatIndexView.ts`) — S4 territory, coordinated not owned by design; web capture script | L | today's ad-hoc `rounded-xl border-border/55 bg-card/20` frames in every `components/zerops/*` file |
| **P5 — mobile catch-up + desktop assets** | promote `apps/web/src/zerops/*` pure modules to `client-runtime` (R3 §6, blocks everything below); sign-in/TOTP/registration sheet, Projects picker, provisioning, "Enable Zerops Code", identity connect replacing the code field (`ConnectionsNewRouteScreen.tsx`), strip subtitle, map sheet, feed cards, question "Other", credential sheet, quick-action chips, settings sections, Live Activity mark/tints; showcase scenes; Icon Composer `.icon` projects (macOS, owner) + `icons:export`; WCO/window colours from generated brand colours; desktop capture in `smoke-test.mjs` | L | the mobile pairing-code screens for Zerops projects (kept only as the manual fallback) |

Order rationale: P0 is free and removes the most visible wrongness ("T3 Code" in a Zerops product). P1 before P2 because the theme path is proven and P2's generator needs real values to assert against. P3 before P4 so the Zerops surfaces are built from primitives that already have the right shape. P5 last because it is gated on S4/S5 promotions, not on design.

## Risks
1. **Default theme vs the theme library.** Keeping T3's library (T3 Chat, Grove, VS Code import) means a user can un-Zerops the chrome. Mitigation: Zerops is the default and the standard card; the Zerops surfaces read status tones and chip tints from `packages/brand`, not from roles, so the strip, map and cards keep the Zerops grammar under any theme; "Reset to Zerops" is one click. Not mitigated: a Grove sidebar next to a mint zcp panel looks odd — acceptable, the user chose it.

2. **Opaque roles vs an alpha-heavy source.** Zerops paints 4%/8% overlays and `rgba` fills; theme colours must be opaque (`themePalette.ts:310-312`). A value flattened over white is wrong over canvas. Mitigation: the generator flattens with an exact function per declared surface and records the surface in the token comment; the contrast test runs against the flattened pair, and hover states are expressed as roles (`sidebarRowHover`, `toolbarControlHover`) rather than alpha utilities.

3. **Teal legibility and blue collisions.** `#00ccbb` is 2.03 on white; primary blue `#0077cc` collides with provider swatch `#2563eb` and info blue. Mitigation: blue = action/selection/info by design (Zerops does the same), teal/mint = mark + update pill + AUTHORIZED halo only, enforced by a contrast assertion that fails on teal foreground over light canvas; swatch fallback moves to neutral and `#0891b2` is dropped; the Claude mark stays an asterisk glyph so warning orange (`#ffa726`) never reads as Claude (`#d97757`).

4. **Roboto on iOS.** A non-system font inside iOS 26 glass headers is visible, and the native bar title cannot cheaply take it. Mitigation: Roboto in content, SF in system-drawn bar items, stated as a seam in the spec; DM Sans → Roboto goes through the same `expo-font` path (`app.config.ts:108-112`) so it is a data change; metrics re-checked against `MOBILE_TYPOGRAPHY`. Fallback if the seam reads wrong in the showcase captures: Roboto only inside cards and the feed, SF for everything else — a smaller "looks like Zerops" that the owner decides on real screenshots.

5. **Surfaces the tokens do not reach.** Terminal ANSI-16 (Ghostty default web, Pierre fixed mobile), shiki/Pierre syntax, diff ±colours, the iOS composer caret (`UIColor.systemBlue`, `T3ComposerEditorView.swift:763`). Mitigation: bg/fg/cursor/selection already follow roles; the rest is allowlisted in the lint and listed in the spec as "code semantics, not chrome"; ANSI roles are a later extension of `THEME_COLOR_ROLES`. The dark canvas `#0c0f0e` under default ANSI blue is unverified — capture it in the harness before deciding.

6. **Drift by a thousand literals.** 217 palette utilities on web and 155 hex literals on mobile will keep growing under agent maintenance. Mitigation: the no-literal lint lands on the Zerops directories in P2 (zero tolerance where the brand shows), repo-wide with an allowlist in P3; the counts are the burn-down chart. Honest cost: every UI slice brief must say "tokens only" and the lint is what enforces it when the brief is forgotten.

7. **Screenshot gating.** Pixel gates flake across OS font rasterisation and simulator versions. Mitigation: token/lint tests are the gate; captures are reviewed artifacts on the PR (AGENTS.md already demands before/after images); the mobile harness's deterministic seeds (`mobile-showcase-environment.ts`) are reused for web so scenes are comparable across shells.

8. **Performance regressions from "looking like Zerops".** The GUI's infinite pulse/ripple/shimmer/bounce and sticky-header scale, plus a webfont. Mitigation: motion budget forbids infinite loops (DS-5 grep), reuses the duty-cycled `steps()` keyframes, keeps glass at existing utilities, ships no Material Icons font, self-hosts Roboto with `swap` + preload; the timeline stays virtualised and cards are plain DOM. Mobile: reanimated pulses converted to a 3-frame cycle.

9. **Mobile depends on stream work, not design.** Every phone screen above waits for S5's promotion of the pure Zerops modules to `client-runtime` and for the door on mobile. Mitigation: the vocabulary and tokens ship first (P1–P3 reach mobile through the existing pipeline with zero screens), so the day S5 lands the screens are assembled from finished parts rather than designed under pressure.

## Signature moments
- The strip is a Zerops status pill: `● DEVELOPING kanbandev` in the thread header uses exactly the dot + tiny uppercase word every Zerops card shows as `● ACTIVE` — and the same phrase, from the same shared function, appears in the phone header subtitle and the desktop window at the same instant.
- The map is the Zerops project page in miniature: Runtimes / Data / Infrastructure as tracked micro-labels, `kanbandev:3000` in Roboto 500 with the port dimmed, a white `Open` pill, and under Infrastructure the mint `#e8f7ec` zcp panel with Web Terminal / SSH / Cloud IDE / Desktop IDEs pills and `✳ Claude Code · AUTHORIZED` with its haloed dot — continuity with the GUI a user already has open in the next tab.
- Deploy → verify reads as the platform's own pipeline: a blue-tinted running card with the 30px-grid step circles, then a green card `✓ deployed · 41 s` with the URL chip and `✓ HTTP 200 · healthy`, while the map row gains `Open` in the same second.
- A question card turns everything orange at once: `○ WAITING FOR YOU` in the card, in the strip, in the sidebar project card and in the phone's Live Activity, with pill options, digit key chips and a visible `Other…` — and a credential ask is visibly a different card whose value never lands in the chat.
- Sign-in is the Zerops login card — grey ZEROPS wordmark, GitHub black and GitLab purple pills, the blue `Login using email` pill — followed by a picker that is the Zerops dashboard's project cards with FULL ACCESS and EU CENTRAL chips and a dashed `New project` card, on the same cool grey canvas.
- Dark mode is the graphite-green ramp (`#0c0f0e / #141918 / #1b2220`) with one mint accent: the send button stays blue, the AUTHORIZED halo is green, and the only mint on screen is the Zerops mark and the `Zerops Code update available` pill — on web, in Electron's titlebar first frame, and behind iOS glass.
