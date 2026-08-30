# Zerops Code — One Map, One Phrase, Three Shells

# Zerops Code — One Map, One Phrase, Three Shells

*A UI concept for the z3 clients (web · desktop · mobile). Written for the owner. Every structural claim cites a file.*

---

## 0. Summary

The panel picked **system-first**. I keep its spine — one token source, one written component vocabulary, drift enforced by failing tests — and I overturn its sequence, drop its new workspace package, and give it the structural thesis it was missing.

The thesis, in one line: **the project's state never scrolls away, the map is a first-class surface, and one string says the same thing on every shell at the same instant.**

Three decisions carry the whole concept:

1. **State goes vertical, not horizontal.** agent-console was right that a Zerops user must always see where the agent is; its 280px rail was wrong. Verified arithmetic against the real constants: sidebar 256 + thread min 640 (`apps/web/src/components/threadSidebarWidth.ts:2-4`) + rail 280 + a right panel at `w-[min(42vw,28rem)]` (`apps/web/src/rightPanelLayout.ts:2`) = 1576–1624px, past a 1440px MacBook. A **28px full-width lifecycle band** under the 52px header costs zero horizontal pixels and does the same job. The rail dies; its thesis lives.

2. **The map is default-open, and it degrades by container query.** skin-first left it one keystroke away — all three judges called that fatal against the brief's "recognise it instantly", and the GUI screenshot `05-project-detail-full.png` shows the services column is the *first* thing a Zerops user sees. project-first-ia made it a standing pane by deleting `ZeropsPanel.tsx` (58 lines) and the `zerops` right-panel kind (`apps/web/src/rightPanelStore.ts:17-27`) — files S6 is editing right now. The middle path: **keep the kind, open it by default on a Zerops project**, and replace the fixed `RIGHT_PANEL_INLINE_LAYOUT_MEDIA_QUERY = "(max-width: 980px)"` with a container query on the main column, following the precedent `ChatHeader.tsx:293` already sets with `@container/header-actions`.

3. **Day-one recognition does not live under `zerops/**` at all.** This is the resolution of the concept's central contradiction and the reason system-first's sequence inverts. Four surfaces carry almost all the "this is Zerops" signal, and three of them are outside the directories S4/S6 own: the sidebar header (`components/sidebar/SidebarChrome.tsx:80-118`), the primitives (`components/ui/*`), the composer's needs-you tone (**one ternary**, `chat/ChatComposer.tsx:2979`), and the lifecycle band (`ChatView.tsx:6906-6928` plus an 87-line strip component). So the product can look unmistakably Zerops in two phases while the streams that own `ZeropsProjectPicker`, `ZeropsServiceMap` and `ZeropsToolCard` keep merging cleanly.

Two corrections to the proposals, both verified in code this session:

- **The question card is already docked at the composer.** `ChatComposer.tsx:2996` and `:3027` render `ComposerPendingUserInputPanel` inside `.chat-composer-top-drawer`, and that drawer already has a four-tone tint system keyed to `--error/--info/--success/--warning` (`index.css:929-947`). agent-console proposed relocating something that is already located; the actual work is tone (`info` → `warning`) and anatomy.
- **The fold-escape has a precedent in the same function.** `MessagesTimeline.logic.ts:934-940` already exempts agent-spawn rows from the collapsed tool group, with the comment *"a running fleet must never hide behind a '+N tool calls' toggle"*. Zerops milestone cards take the same predicate. The open product call in `verified.md` has a cheap, in-idiom answer.

What this costs, honestly: Roboto self-hosting, a theme-generator with `--check` in CI, an Icon Composer `.icon` project per channel that needs macOS tooling, and a mobile client that has **zero Zerops code on `main`** (`grep -ri zerops apps/mobile/src` → 0) — so every phone screen here is a specification, not a restyle.

---

## 1. Design principles — the Zerops DNA, applied

Five traits make the GUI recognisable (R1 §10). Each becomes a rule with a consequence in the fork.

| # | Zerops trait | Rule for Zerops Code | What it costs |
|---|---|---|---|
| P1 | **Depth by tint, not shadow.** Canvas `#eceff3` → card `#f3f5f7` → white tiles; `.mat-card` shadow is `none` (`frontend-legacy/libs/zef/src/styles/base/_material.scss:22-49`) | Cards are flat and borderless in light, 1px `rgba(255,255,255,.06)` in dark. Shadows only on popovers and dialogs. Every ad-hoc `rounded-xl border-border/55 bg-card/20` frame in `components/zerops/*` is replaced by one `FlatCard`. | The glass composer keeps its shadowed shell — it is chrome, and its 22px clip-path geometry is hard-coded by design (`index.css:993-1060`). Accepted inconsistency. |
| P2 | **A rigid micro-label grammar.** 8–10px uppercase 500/600 tracked labels beside 20px/500 names, `:port` at .6 (`service-stack-basic-info.component.scss:6`) | One `MicroLabel` primitive: 10px/600 uppercase, `.06em`, 45% opacity. Group titles, status words, card kickers, chip text all go through it. | **Deliberate deviation: 10px, not 8.** The GUI's 8px status label (`status-icon-base.component.scss:1426`) is below the readable floor on a high-DPI laptop and impossible on a phone. On mobile it is `micro` = 11px (`apps/mobile/src/lib/typography.ts`). |
| P3 | **Status is a dot plus a tiny uppercase word**, pulse means in flight | One `StatusDot` + `MicroLabel` pair on every client — `● ACTIVE`, `◌ CREATING`, `○ WAITING FOR YOU`. **Never a bare dot**: the GUI's status palette is hue-only Material 400s and `#00cc55`/`#ffa726`/`#cc0011` are deuteranopia-confusable (R5 risk 3). | Pulse uses the existing duty-cycled `steps()` keyframes (`index.css:238-251`), never the GUI's `pulse 1500ms infinite`. AGENTS.md:155 forbids continuously repainting animations, and the GUI has five of them. |
| P4 | **Pills everywhere**, 10px-radius tinted chips | `ui/button.tsx` `default`/`secondary` and every CTA become `rounded-full`; `ui/badge.tsx` grows `chip` and `status` variants; `ghost`/`icon` keep 8px. | 17px-on-38px-line-height is a Material legacy artefact; we take the *shape*, not the metric. |
| P5 | **Blue `#0077cc` acts, teal identifies.** Teal is 2.03:1 on white (R5 §3 computed) | `--primary ← messageAction = #0077cc`. Teal/mint only as: the mark, the app-bar pill tint `rgba(0,204,187,.13)`, the `update` role, and connected/authorized dots. A contrast test *fails the build* on teal foreground over a light surface. | Provider accent swatch fallback moves off `#2563eb` (`settings/ProviderAccentColorPicker.tsx:12-21`) — it is indistinguishable from primary. |

Two more principles the brief imposes rather than the GUI:

- **P6 — The client renders, the agent mutates.** Nothing in the map, band, cards or quick actions reaches a mutating RPC (brief §4 rule 3). The assertion already exists — `ZeropsQuickActions.test.tsx:31` *"cannot reach Zerops or the RPC layer at all"* — and is widened to every Zerops surface module. Read-only and caller-bound surfaces stay allowed: Open URL, files over the mount, terminal, logs, Data Console.
- **P7 — Every state carries a phrase, never a blank.** The phrases are pinned in `apps/web/src/zerops/strip.ts` and asserted verbatim in `strip.test.ts`. Absent is not empty: when `available:false` the map is *gone*, not "no data" (`ZeropsPanel.tsx:44-56` already makes this distinction and names why).

---

## 2. Information architecture — web and desktop

### 2.1 Nouns the user sees

Account (one or more Zerops orgs) → **Project** (= the z3 environment; the word "environment" never renders) → **Thread**. **Services** are rows of the project's map, never navigation. **Devices** replaces Settings → Connections. **Coding agents** replaces Providers. The T3 project record at `/var/www` is invisible (brief D3).

### 2.2 Shell — T3's three columns, Zerops' grammar

Sidebar 256 / min 208 · thread (min 640) · right panel inline or sheet. Nothing is added to the horizontal budget. What changes:

```
┌────────────────┬──────────────────────────────────────────┬─────────────────┐
│ SIDEBAR 256    │ HEADER 52px  breadcrumb · actions        │ RIGHT PANEL     │
│  ▸ mark pill   ├──────────────────────────────────────────┤  (map default   │
│  ▸ ⌘K pill     │ BAND 28px  ● DEVELOPING KANBANDEV        │   on a Zerops   │
│  ▸ thread rows ├──────────────────────────────────────────┤   project)      │
│    with ● dot  │ TIMELINE (virtualised)                   │                 │
│    + phrase    │                                          │                 │
│                │ COMPOSER  ← question / credential dock    │                 │
│  ⚙ ☁ ▥         │ CONTEXT STRIP  kanban · kanbandev  (…)   │                 │
└────────────────┴──────────────────────────────────────────┴─────────────────┘
```

- **Sidebar** — `AppSidebarLayout.tsx:229-236` already branches `isOnSettings ? SettingsSidebarNav : legacySidebarEnabled ? LegacyThreadSidebar : ThreadSidebar`; we do **not** add a fourth branch. `Sidebar.tsx` (3960 lines: virtualisation, dnd-kit pinned reorder, snooze shelves, prewarm) stays exactly as built. Two surgical edits: the header gets the mark pill + ⌘K pill (`sidebar/SidebarChrome.tsx:80-118`), and the row's second line becomes `StatusDot` + the strip phrase instead of T3's emerald/violet status text (`ThreadStatusIndicators.tsx:83-131,427`). **This is the single most important scope decision in the concept** — see §11 D1.
- **Header** — 52px (`index.css:107`, WCO override `:125`). Breadcrumb `⬡ kanban / Build the kanban`, then Cloud IDE (replacing the `OpenInPicker` editor list), map toggle, panel controls. `GitActionsControl` (Commit ▾) is hidden on Zerops projects — brief D4, never two commit pipelines.
- **Band** — new, 28px, full width, tinted by tone, rendered directly under `WorkspacePageHeader` in `ChatView.tsx`. Content is `zeropsStripState(lifecycle, {pendingUserInput})`, verbatim. Clicking it focuses the map (or opens the sheet).
- **Right panel** — kinds stay `diff | files | file | preview | terminal | pull-request | agents | zerops`, plus a new `data-console`. `pull-request` hidden on Zerops. **`zerops` opens by default** the first time a Zerops project connects for a thread; `rightPanelStore` already persists per thread, so the reverse state (closing it) sticks.
- **Context strip** — the slot at `ChatView.tsx:7178-7188` that today renders `BranchToolbar` ("Local checkout · main"). On Zerops it reads `kanban · kanbandev` (the mounted repo this thread's checkpoints target, spec §6.4) plus quick-action pills.

### 2.3 Where each Zerops thing lives

| Thing | Web home | Component today |
|---|---|---|
| Sign in / TOTP / register | `/` when signed out, **and** the container origin's `requires-auth` branch | `ZeropsLandingShell.tsx` |
| Picker · New project · provisioning | `/` when signed in; `/zerops` folds away | `ZeropsProjectPicker`, `ZeropsProjectsPage`, `ZeropsProvisioningPanel` |
| Lifecycle phrase | band + sidebar row subtitle + map sheet header | `ZeropsLifecycleStrip.tsx` (87 lines) |
| Service map | right-panel kind `zerops`, default-open | `ZeropsPanel` (58 l) + `ZeropsServiceMap` (149 l) |
| Milestone cards | timeline, **outside** the collapsed tool group | `ZeropsToolCard` at `MessagesTimeline.tsx:2605` |
| Question card | composer top drawer — **already there** | `ComposerPendingUserInputPanel` via `ChatComposer.tsx:2996,3027` |
| Credential card | same drawer, distinct anatomy | does not exist (S6) |
| Quick actions | context strip, prefill only | `ZeropsQuickActions.tsx` |
| Data Console | new right-panel kind `data-console` (iframe, caller-bound, read-only v1) | does not exist |
| Devices | Settings → Devices | `ConnectionsSettings` |
| Account / orgs / sign out | Settings → Zerops | `ZeropsSettings.tsx` |

### 2.4 Reverse states (AGENTS.md:80 — "a one-way door is a bug")

Sign in ↔ sign out · connect device ↔ remove device · map open ↔ closed (per thread) · band click ↔ panel close · question answered ↔ "Other" free text ↔ reopen from the band · quick-action prefill ↔ clear composer · credential stored ↔ **Replace** (revoke stays in the GUI — stated, not hidden) · Zerops theme ↔ any library theme ↔ **Reset to Zerops** · pin/unpin, snooze/unsnooze, archive/unarchive all survive untouched because `Sidebar.tsx` survives untouched.

One action has no reverse and the UI says so: **"Enable Zerops Code" restarts the container (~20 s)**. The pill carries the cost as a sub-label.

---

## 3. Visual system and token mapping

### 3.1 The mechanism

`ZEROPS_THEME: ThemeDefinition` (57 roles × light + `variants.dark`, `sidebarArtwork: false`) in `packages/shared/src/themePalettes.ts` (roles at `:18-80`, verified count 57), registered in `BUILT_IN_THEME_IDS`, `MOBILE_THEME_IDS`, `BUILT_IN_THEMES`, and made the default on both clients. Web: `applyThemePalette` → `--app-theme-*` → the `html[data-theme-id]` remap at `index.css:1552-1617`. Mobile: `createMobileThemeVariables` (57 roles → 65 `--color-*`) → `generate-uniwind-themes.mts`, which emits `@variant zerops-light/dark` the moment the id is registered. Desktop: generated `brandColors.ts` feeding `DesktopWindow.ts:34-35,128-130`.

### 3.2 Token map (abridged — full 57 rows follow R5 §2)

| Role group | Light | Dark | Consumer |
|---|---|---|---|
| canvas · chrome · toolbar · sidebar | `#eceff3` | `#0c0f0e` | `--background`, topbar, mobile `screen`/`status-bar` |
| surface · sidebarRowSelected | `#ffffff` | `#141918` | `--card`; a selected thread is a Zerops card |
| surfaceRaised · surfaceOverlay | `#ffffff` · `#ffffff` | `#1b2220` · `#1e2624` | composer glass, `--popover` |
| secondary · muted · accentSurface | `#f3f5f7` · `#f2f5f7` · `#f0f3f5` | `#232b29` · `#151b1a` · `#232b29` | flat pills, chips, hover rows |
| text · textMuted · placeholder · secondaryLabel | `#1a1a1a` · `#5f6a72` · `#757575` · `#7d8891` | `#e9eeec` · `#9faea9` · `#5e6e69` · `#7d8c88` | |
| **sidebarMutedForeground** | `#4f5a62` | `#9faea9` | darker than `textMuted` so the derived `--sidebar-icon-color` (60% mix, `index.css:94-98`) clears 3:1 |
| border · input · toolbarBorder | `#e0e0e0` · `#d4dfea` · `#d4dfea` | `#262f2d` · `#2a3331` · `#262f2d` | |
| toolbarControl / Hover · sidebarControlSurface | `#e3e6ea` / `#d9dce0` | `#1b2220` / `#2a3331` | ⌘K pill (4%/8% black flattened) |
| **messageAction → `--primary`** / Hover / Fg | `#0077cc` / `#005fa3` / `#fff` | `#0077cc` / `#008ff5` / `#fff` | send, CTAs, switches — 4.66:1 both ways |
| focus → `--ring` | `#0077cc` | `#5ab3ff` | |
| accent / accentForeground (mobile primary, md-link) | `#0077cc` / `#ffffff` | `#58a6ff` / `#0c0f0e` | dark: white on `#58a6ff` fails AA → dark text |
| messageSurface / Fg | `#e3f4ff` / `#1a1a1a` | `#1e2e3b` / `#e9eeec` | user bubble = Zerops core-blue tint, not iMessage blue |
| **update** / Fg / Surface | `#02b1a3` / `#007e72` / `#def8f6` | `#00e5c0` / `#58efd4` / `#113630` | the one place teal carries meaning in chrome |
| error / Fg / Surface | `#cc0011` / `#cc0011` / `#fdefef` | `#e57373` / `#e57373` / `#2f1717` | `#cc0011` on `#141918` is 3.03 → mat red 300 in dark |
| warning / Fg / Surface | `#ffa726` / `#bb4d00` / `#fdf6ef` | `#ffa726` / `#ffb74d` / `#3a301a` | orange is indicator-only (1.94 on white) |
| codeBackground / terminal* | `#f2f5f7` | `#121716` / `#0c0f0e` | cursor `#007e72` / `#00e5c0` |

### 3.3 Fixed brand tokens — and why there is **no** `packages/brand`

system-first put status tones, mark path data, the icon map and the copy glossary in a new workspace package. Two of three judges called that ceremony, and I side with them: the *separation* is right, the *package boundary* is not. These values live in **`packages/shared/src/zeropsBrand.ts`**, beside `themePalettes.ts`, exported from the same package — no new workspace member, no build wiring, no row in the fork.md zone table (`docs/internals/zerops/fork.md §3`), no `imported.lock` interaction.

The separation still does its job: because these are **not** `THEME_COLOR_ROLES`, a user on "Grove" keeps the Zerops status grammar and the mint zcp panel. The precedent already exists — `index.css:1429-1432` keeps `--success` and `--info` theme-independent with a comment saying so.

| Fixed token | Light | Dark |
|---|---|---|
| `ok` — dot / text / surface | `#66bb6a` / `#2e7d32` / `#e8f7ec` | `#56d364` / `#56d364` / `#1a281e` |
| `busy` — dot / surface | `#42a5f5` / `#eff9fd` | `#58a6ff` / `#1e2e3b` |
| `attention` — dot / text / surface | `#ffa726` / `#b26a00` / `#fff4e0` | `#e8a33d` / `#ffb74d` / `#3a301a` |
| `failed` — dot / surface | `#ef5350` / `#fdefef` | `#f47067` / `#3a1d1a` |
| `off` — dot | `#bdbdbd` | `#5e6e69` |
| mint panel (zcp) | `#e8f7ec` | `rgba(76,175,80,.10)` |
| chip: access-green / region-purple / info-chip | `rgba(76,175,80,.15)` `#388e3c` / `rgba(156,39,176,.15)` `#7b1fa2` / `rgba(255,255,255,.9)` | dark equivalents |
| identity teals | mark `#3cbdb2` + `#00b1a3`; pill tint `rgba(0,204,187,.13)` | mint `#00e5c0` |

Platform status collapses onto the five tones by a table: `ACTIVE/RUNNING`→ok · `READY_TO_DEPLOY`→busy settled (no pulse) · `STOPPED/REPAIRING`→attention · `*FAILED/DELETING`→failed · `DELETED`→off · everything `transient`→busy + pulse. This replaces the Material 100–400 ladder (R1 §2), which collapses to four hues anyway.

### 3.4 Type, shape, icons, motion

- **Roboto** 400/500/700, self-hosted woff2 in `apps/web/public/fonts`, preloaded, `font-display: swap` — no Google Fonts fetch from a container origin. `index.css @theme:139-144` + `appearanceFonts.ts:20-26`. Mobile: `@expo-google-fonts/roboto` replacing DM Sans through the same `expo-font` path (`app.config.ts:108-112,241-264`), plus `global.css:215-217` and the alert-dialog plugin font (`plugins/withAndroidModernAlertDialog.cjs`). Mono stays T3's SF Mono/Menlo stack for code, diff and terminal (perf); Roboto Mono only for hostnames and log lines inside Zerops cards.
- **Scale (web):** body 14 · row hostname 14/500 with `:port` at 60% · card title 14/500 · project name 20/500 · description 13/1.6 at 70% · MicroLabel 10/600 uppercase `.06em` · draft hero 32/400.
- **Shape:** `--radius` stays `0.625rem` = 10px (= `.mat-card`); `--control-radius` 8px; dialogs 16px (`ui/dialog-styles.ts`); chips 10px; info-chips 8px; key chip 3px (`ui/kbd.tsx` → `#efefef` / border `#adb3b9`). **Selection = 2px `#0077cc` outline** (`menu-item-card.component.scss:17-23`), `rgba(0,229,192,.4)` in dark — never a filled background. The dark dialog scrim stays; the GUI's white scrim is legacy.
- **Icons:** lucide on web, SF Symbols → Tabler on Android (`components/AppSymbol.tsx:86-189`). **No Material Icons webfont** — a blocking font plus layout shift, against AGENTS.md:155. One ~15-glyph map in `zeropsBrand.ts` resolves per client. The Zerops mark ships as path data (4 paths, viewBox `0 0 42.27 50.48`, `libs/zui/src/logo/`) rendered by `<svg>` on web and `react-native-svg` on mobile — the pattern `T3Wordmark.tsx` already uses. The 87 service-type SVGs are **not** used in map rows: the GUI's own cards show no per-type logo (R6 §2), so their absence is faithful and their presence would be an invention. Ship without them; revisit if scannability complains.
- **Motion budget:** easing `cubic-bezier(.4,0,.2,1)`; 150/200/300ms. In-flight = the existing `status-pulse` `steps(6)` keyframes (`index.css:238-251`). **Deleted, not ported:** ripple rings, `bounceHorizontal`, shimmer, bokeh, `pulseFullReverse`, the sticky-header scale. Mobile: `ConnectionStatusDot`'s infinite `withRepeat` becomes a 3-frame duty cycle. `prefers-reduced-motion` disables all of it (`index.css:2513`).

---

## 4. Web screens

### 4.1 Sign-in — one bundle, one door

Today the container-served bundle shows T3's pairing page: `isHostedStaticApp()` is false there (`hostedPairing.ts:34-45` — no `VITE_HOSTED_APP_CHANNEL`, origin ≠ `VITE_HOSTED_APP_URL`), so `__root.tsx:78-82` resolves `requires-auth` and `_chat.tsx` redirects to `/pair`. **The one structural requirement this concept cannot design around:** the `requires-auth` branch must render the Zerops landing when the origin answers the z3 descriptor (`/z3/.well-known/t3/environment`, spec §4.5). That is S4's gate work; design coordinates, does not own.

```
┌────────────────────────────────────────────────────────────────────────────────┐
│                                                                                │
│                            [Z]  ZEROPS   Code                                  │
│                 Cloud platform for humans and their coding agents              │
│                                                                                │
│                  ┌──────────────────────────────────────────┐                  │
│                  │  (        Login using GitHub          )  │  #24292e         │
│                  │  (        Login using GitLab          )  │  #9b51e0         │
│                  │  ──────────────── or ──────────────────  │                  │
│                  │  ┌ Email ───────────────────────────┐    │  white, 6px,     │
│                  │  └──────────────────────────────────┘    │  1px #d4dfea     │
│                  │  ┌ Password ────────────────────  ◉ ┐    │                  │
│                  │  └──────────────────────────────────┘    │                  │
│                  │  (        Login using email          )   │  #0077cc pill    │
│                  └──────────────────────────────────────────┘                  │
│           Don't have an account yet?  [ Create a new Zerops account ]          │
│                                                                                │
│              Not using Zerops?  Connect a server with a one-time link          │
└────────────────────────────────────────────────────────────────────────────────┘
```

On a container origin one line sits under the wordmark: `● ACTIVE  kanban · zcp` — the project the descriptor named. The word "pairing" appears nowhere; the mechanism (§3.5) survives as "Connect a server with a one-time link". `AuthSurfaceShell`'s blue gradient `#1e61de→#17348e` (`AuthSurfaceShell.tsx:17-32`) is deleted.

### 4.2 Picker — the GUI dashboard, transplanted

This is project-first-ia's best surface and the concept takes it whole: the org pill, the card anatomy (dot + word, name 20/500, `FULL ACCESS` / `EU CENTRAL (PRG1)` chips), and the dashed `+ New project` card matching the GUI's `__project-add-button` idiom. It lands on a route nothing else owns, so it inherits none of that proposal's sidebar risk.

```
┌────────────────────────────────────────────────────────────────────────────────┐
│ [Z] KRLS ▾   ( ⌕  ⌘K )      Projects        ⌕ Search projects…    Created ↓    │
├────────────────────────────────────────────────────────────────────────────────┤
│ CONNECTED                                                                      │
│ ┌──────────────────────────────────┐  ┌──────────────────────────────────┐     │
│ │ ● ACTIVE   kanban             ›  │  │ ● ACTIVE   shop               ›  │     │
│ │ [FULL ACCESS] [EU CENTRAL (PRG1)]│  │ [FULL ACCESS] [EU CENTRAL (PRG1)]│     │
│ │ zcp · ready · 3 services         │  │ zcp · ready · 5 services         │     │
│ │ 2 threads · 1 working            │  │ task complete                    │     │
│ │ (            Open            )   │  │ (            Open            )   │     │
│ └──────────────────────────────────┘  └──────────────────────────────────┘     │
│ READY TO CONNECT                                                               │
│ ┌──────────────────────────────────┐  ┌──────────────────────────────────┐     │
│ │ ● ACTIVE   legacy                │  │ ◌ RESTARTING  blog        12 s   │     │
│ │ zcp · needs Zerops Code          │  │ upgrading the container          │     │
│ │ (   Enable Zerops Code   ) ~20 s │  │                                  │     │
│ └──────────────────────────────────┘  └──────────────────────────────────┘     │
│ PREPARING                                                                      │
│ ┌──────────────────────────────────┐  ┌ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┐     │
│ │ ◌ CREATING  new-shop             │      ＋  New project                      │
│ │ waiting for the container        │  └ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┘     │
│ │ 0:41 / 5:00   ( Stop waiting )   │                                           │
│ └──────────────────────────────────┘                                           │
│ NOT AVAILABLE   demo — project STOPPED  ·  tools — no public subdomain          │
└────────────────────────────────────────────────────────────────────────────────┘
```

Groups and reasons come from `apps/web/src/zerops/candidates.ts`; the `PREPARING` card *is* the provisioning waiter (`awaiting-project` 60 s → `awaiting-container` 300 s → `awaiting-health` 30 s), with the cap shown as elapsed/cap. `pool-exhausted` is a card state with copy, not an error (Z3C-4). Every zcp container in every org is one card, including several in one project (Z3C-2).

### 4.3 Thread — band, timeline, dock, map

```
┌──────────────┬────────────────────────────────────────────────┬────────────────┐
│ [Z] Code     │ ⬡ kanban / Build the kanban   [Cloud IDE] ▤ ▯ │ SERVICES     × │
│ ( ⌕  ⌘K )    ├────────────────────────────────────────────────┤ live · 2 s ago │
│              │ ● DEVELOPING KANBANDEV                    2 ▸ │ RUNTIMES       │
│ ⬡ kanban ●   ├────────────────────────────────────────────────┤ ┌────────────┐ │
│ ┌──────────┐ │           ┌──────────────────────────────────┐ │ │● ACTIVE    │ │
│ │Build the │ │           │ Vytvoř mi kanban.                │ │ │kanbandev   │ │
│ │kanban    │ │           └──────────────────────────────────┘ │ │      :3000 │ │
│ │● DEVELOP…│ │ I'll propose three services first.             │ │nodejs@22   │ │
│ └──────────┘ │ ┌ PLAN · 3 SERVICES ───────────────────────┐   │ │[MOUNTED]   │ │
│  Add auth    │ │ ○ kanbandev    nodejs@22       dev       │   │ │   ( Open ) │ │
│  ○ WAITING…  │ │ ○ kanbanstage  nodejs@22       stage     │   │ │└ ○ READY   │ │
│              │ │ ○ db           postgresql@18             │   │ │  kanbanstage││
│ ⬡ shop ●     │ └──────────────────────────────────────────┘   │ └────────────┘ │
│  Fix cart    │ ▸ Read 3 files and ran 2 commands              │ DATA           │
│  ◐ 1d        │ ┌ DEPLOY · KANBANDEV ──────────────────────┐   │ ┌────────────┐ │
│              │ │ ✓ build 41 s  ✓ deploy  ✓ subdomain      │   │ │● ACTIVE db │ │
│              │ │ [ ◎ kanbandev-24cb.prg1.zerops.app  ↗ ]  │   │ │postgresql  │ │
│              │ └──────────────────────────────────────────┘   │ │(Data Consl)│ │
│              │ ┌ VERIFY · KANBANDEV ──────────────────────┐   │ └────────────┘ │
│              │ │ ✓ HTTP 200 · healthy                     │   │ INFRASTRUCTURE │
│              │ └──────────────────────────────────────────┘   │ ╔════════════╗ │
│              │ ┌────────────────────────────────────────┐     │ ║● ACTIVE zcp║ │
│              │ │ Ask for changes…                       │     │ ║(Web Term.) ║ │
│              │ │ ✳ Claude Code ▾ High ▾ Full access ▾(↑)│     │ ║(Cloud IDE) ║ │
│              │ └────────────────────────────────────────┘     │ ║(SSH)(Dsktp)║ │
│ ⚙  ☁  ▥      │  kanban · kanbandev  (Deploy)(Logs)(Redis)     │ ║✳ Claude    ║ │
│              │                                                │ ║  AUTHORIZED║ │
└──────────────┴────────────────────────────────────────────────┴════════════════┘
```

The band is the only tinted stripe on screen. Its right end shows the pending-question count and a chevron that focuses the map. Below 1040px of main-column width the map becomes a sheet and the chevron opens that instead.

### 4.4 The dock — question and credential

Already the shipped location; the work is tone and anatomy.

```
│  ┌ ○ WAITING FOR YOU ─────────────────────────────────────────── 1 of 2 ┐  │  #fff4e0 tint,
│  │ How should this deploy when the task closes?                         │  │  orange outline
│  │ ⌜1⌝ ● auto     commit + push + deploy after every verified change     │  │  ← 2px #0077cc
│  │ ⌜2⌝ ○ manual   I'll say when                                         │  │
│  │ ⌜3⌝ ○ Other…   ______________________________________                │  │
│  │                                              ( Skip )  ( Answer ↵ )  │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│  ┌ ⚿ CREDENTIAL · GIT_TOKEN for kanbandev ──────────────────────────────┐  │  mint #e8f7ec
│  │ Stored as a service secret on kanbandev. Never enters this thread.   │  │
│  │ [ •••••••••••••••••••••••••••••••••••••• ]   ( Store on kanbandev )  │  │
│  │ Or set it in the Zerops GUI ↗                                        │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
```

**Until the zcp storage verb exists, the credential card renders only the GUI deep link** — no field, no button. This is user-journey's graft and it is the right call: a masked field that goes nowhere would imply a chat-paste path the brief forbids (§4 rule 4).

### 4.5 Settings and palette

Sections (`settings/settingsSearch.ts:28-38`): General · Appearance · Keybindings · **Coding agents** · Integrations · Source control (hidden on Zerops) · **Devices** · **Zerops** · Archive. Appearance keeps the whole theme library — Zerops is the *default card*, not the only one; "Reset to Zerops" is one click; "Environment identification" (stage artwork) is removed since `sidebarArtwork: false`.

Command palette restyled to the GUI's palette grammar (16/18px search row, teal-tinted scope chip at 6px radius, 10/700 uppercase group titles at .45, 40px rows at 8px, key-chip footer). New intents: *New thread in \<project\>*, *Open service map*, *Open \<hostname\> ↗*, *Show logs for \<host\>* (prefill), *Open Cloud IDE*, *Switch project*, *New project*. Removed on Zerops: `add-project` folder browsing (`CommandPalette.tsx:476-478`) — there is no folder to add.

---

## 5. The Zerops surfaces in detail

### 5.1 Service map

Header: `SERVICES` MicroLabel plus a liveness line — `live · updated 2 s ago` / `polling` / `last read failed · retrying`. Feed availability is three things, not one boolean (spec §5.4): `available:false` → the surface is **absent**, not empty; `degraded:true` → last-good rows plus one orange line; doorbell down → **one quiet grey line**, never the degraded banner.

Groups `RUNTIMES / DATA / INFRASTRUCTURE` in that pinned order (`zerops/serviceMap.ts:20-25`, comment: *"what the user builds, what it stores, what runs it"*).

**ServiceRow anatomy** — replacing today's `rounded-xl border-border/55 bg-card/20` (`ZeropsServiceMap.tsx:67`):

```
● ACTIVE                                          ← StatusDot + MicroLabel
kanbandev:3000                                    ← 14/500, :port at 60%
nodejs@22   [MOUNTED]                   ( Open )  ← type, chip, white pill
└ ○ READY   kanbanstage                           ← the dev↔stage pair, folded
production: shop-prod ↗                           ← from the envelope
```

A Data row gains a `Data Console` pill. The zcp row under Infrastructure is the **MintPanel** — `#e8f7ec` at 6px, header `● ACTIVE zcp`, four white flat pills *Web Terminal · SSH · Cloud IDE · Desktop IDEs* in the GUI's own 2-column grid (`zagent-service-card.feature.html:93-133`), then the edge-to-edge auth tray with `✳ Claude Code · AUTHORIZED` in `#2e7d32` and the 3px haloed dot. SSH and Desktop IDEs link out to the GUI service page — both need VPN, which is out of scope (brief D6). The Authorize action arrives with S7; until then the row's `⋮` offers *Authorize in Zerops ↗*.

**The three-state ladder** (agent-console's graft, applied to a surface that already exists rather than a new column): inline column when the main content column is ≥1040px · sheet below · absent when the feed says `available:false`. Named, tested, not an ad-hoc squeeze.

### 5.2 Lifecycle band

28px, full width, tone tint, 12/500. Content is `zeropsStripState` verbatim — phrases pinned in `strip.ts` and asserted in `strip.test.ts`, unknown phase shown as itself, never blank. Precedence is the spec's: *waiting for you* › *\<tool\> running* › phase. The strip today swaps in a `Spinner` for the active tone (`ZeropsLifecycleStrip.tsx:57`); it becomes a `StatusDot` with the duty-cycled pulse.

`ZeropsStripLine` is already split from its feed-reading container specifically so its markup can be tested without a live registry — the band reuses that split rather than fighting it.

### 5.3 Cards, and the fold

Kinds `plan / import / mount / deploy / verify / subdomain / error` from `zerops/cards/payloads.ts`, decoded totally, falling back to the generic tool block (Z3F-7). Restyled as **process shells**: 8px radius, a tone-tinted header stripe with a 10/600 uppercase kicker (`DEPLOY · KANBANDEV`), 17px step glyphs in 2px-bordered circles on a `30px 1fr` grid (queued `clock` grey → running `play` blue → done `check` green → failed `!` red), body 12/400 lh 1.7, a white info-chip with a green globe for the URL, log lines on `codeBackground` in a 200px scroll for errors.

**The fold decision, and its mechanism.** `verified.md` leaves "do Zerops cards escape the collapsed work group" open. The answer is yes for milestones, and the code already knows how. `MessagesTimeline.logic.ts:857-860` collapses a group when `onlyToolEntries` — every entry is tool-like, has no `agentSpawn`, and is not an error. At `:934-940` the overflow filter exempts spawn rows outright, with the comment *"Agent-spawn CTA rows are always visible: a running fleet must never hide behind a '+N tool calls' toggle."* Adding a `zeropsMilestone` predicate alongside `agentSpawn` gives plan/import/deploy/verify/error the same exemption, in the same idiom, in one function. `mount` and `subdomain` stay folded — and note the collapsed row's label is a *generated* summary from `summarizeToolGroup` (`MessagesTimeline.logic.ts:346`) plus `+N previous tool calls`, not the literal "Used N tools" every proposal quoted.

### 5.4 Question, credential, quick actions

Question card: keys 1–9, arrow navigation, option descriptions, a visible `Other…` field, multi-select as checkboxes — all present in `ComposerPendingUserInputPanel.tsx`. The change is `data-variant` (`ChatComposer.tsx:2979`) so a pending question tints the drawer with the needs-you orange rather than info blue, plus the option-tile anatomy and the digit key chips.

Quick actions prefill the composer and stop. The module graph reaches no mutating RPC and the test that says so (`ZeropsQuickActions.test.tsx:31`) is widened to the band and the map.

### 5.5 Data Console and the hand-offs

A ninth right-panel kind `data-console`, opened from a Data row: an iframe of the caller-bound console on the same 8080 origin (`zcp/docs/spec-dataconsole.md`, embed reach). **Read-only in v1** — the write path is caller-bound on a per-request `X-Write-Token`, and threading that through an embed is an S6 design task, not a skin.

Cloud IDE opens code-server on the same origin in a new tab (its cookie gate is its own). Web Terminal opens z3's own terminal surface with cwd `/var/www`. SSH copies the command. "Open in editor" (`OpenInPicker`) is relabelled **Cloud IDE** everywhere.

---

## 6. Mobile — a Zerops language for a device Zerops has never designed for

The GUI is `<meta viewport width=1200>` with zero app-shell media queries (`frontend-legacy/apps/zerops/src/index.html:23`). `12-mobile-dashboard.png` is a scaled desktop. **There is no precedent to copy, so the phone is where this concept defines Zerops rather than borrowing it.**

### 6.1 Three laws

1. **Anything the OS draws stays OS-drawn.** Native-stack with flat thread routes so iOS 26 header morphing works (`Stack.tsx:229-233`), transparent glass `UINavigationBar` with scroll-edge effects and the `"editor"` title style (`:89-100`), native header items from the JSX DSL taking SF Symbols only (`native/StackHeader.tsx:219-224`), form sheets with detents and grabber, `UIMenu`, the Mail search toolbar, the keyboard stack, haptics, predictive back, Android's in-flow `AndroidScreenHeader`/`AndroidAnchoredMenu`/FAB.
2. **Anything z3 draws inside those containers takes the Zerops grammar.** Canvas `#eceff3`/`#0c0f0e`, white cards at **16px** (today 24 — `SettingsSection`, `ConnectionsRouteScreen`, `EmptyState`, `ThemeCard`), no card shadows, `● ACTIVE` dot-and-word, MicroLabel at 11px, pills for actions, primary `#0077cc` (today near-black `#262626`), user bubble `#e3f4ff` with dark text (today iMessage `#007aff`).
3. **One phrase, four places, one function.** The strip string appears simultaneously in the thread row subtitle, the thread header subtitle, the map sheet header, and the **Live Activity** — all from `strip.ts` once it is promoted to `packages/client-runtime`. Nothing is summarised twice.

Law 3 is the mobile thesis. It is also the one feature the desktop cannot match, and it is free: the feed already exists, and `widgets/AgentActivity.tsx:59-93` already renders a phase with tints — it just renders T3's phase vocabulary instead of the Zerops phrase.

### 6.2 What the phone is for

Watch, answer, nudge. See which thread needs you, answer a question card, approve a plan, read a deploy card and open the URL, glance at the map, send one sentence. Long specs are typed on a laptop. Every screen is built so a thread can be understood from its header alone.

### 6.3 Screens

| Route | Shape |
|---|---|
| **Sign in** (new, S5) | Zerops email/password + TOTP as a form sheet; registration hands off to `app.zerops.io` or a Turnstile WebView (Z3C-3). Replaces `ConnectionsNewRouteScreen`'s host+code fields for Zerops; the manual one-time-link path stays below a divider |
| **Home** | `HomeScreen.tsx` already groups threads by project (`ThreadListGroupHeader`). Groups become Zerops project cards: `● ACTIVE kanban · 3 svcs` with the project's live phrase, then thread rows whose **second line is the strip phrase + StatusDot** |
| **Projects** | The picker as a form sheet with the four candidate groups and one action pill per row; `Enable Zerops Code` carries the ~20 s note |
| **Thread** | `ThreadDetailScreen.tsx`; **header subtitle = the strip phrase** (the `WorkspaceConnectionTitle` slot); Zerops cards inline in `ThreadFeed`; right header item ▤ opens the map sheet |
| **Map sheet** (new) | Form sheet, detents `[0.55, 0.92]`; header `kanban · SERVICES` + the phrase; groups as MicroLabels; ServiceRows; the MintPanel for zcp |
| **Settings** | Zerops account · Projects · **Devices** · Coding agents · Appearance (Zerops = default card) · Archive; `settings-sheet-targets.ts` gains two targets |
| Unchanged | `ThreadTerminal`, `ThreadReview`, `ThreadFiles` (tree rooted per service hostname), Usage, Archive; Git sheets hidden on Zerops (D4) |

**The answer surface is already right.** `ThreadDetailScreen.tsx:696-735` renders `PendingUserInputCard` in place of the composer, with the comment *"The questionnaire replaces the composer, so it must pad the home indicator the composer normally covers."* The docked-question pattern the desktop is adopting is native on the phone already. It needs the needs-you tone, option descriptions and `Other` — not relocation.

### 6.4 Wireframes

```
HOME                                   THREAD                                 MAP SHEET
┌──────────────────────────────┐  ┌──────────────────────────────┐  ┌──────────────────────────────┐
│ [Z] Code            ⌕    ⋯   │  │ ‹ kanban  Build the kanban   │  │             ━━━━             │
├──────────────────────────────┤  │   ● DEVELOPING KANBANDEV ▤ ⋯ │  │ kanban · SERVICES      Done  │
│ ⬡ KANBAN     ● ACTIVE 3 svcs │  ├──────────────────────────────┤  │ ● DEVELOPING KANBANDEV       │
│ ┌──────────────────────────┐ │  │        ┌───────────────────┐ │  ├──────────────────────────────┤
│ │ Build the kanban     5m  │ │  │        │ Vytvoř mi kanban. │ │  │ RUNTIMES                     │
│ │ ● DEVELOPING KANBANDEV ✳ │ │  │        └───────────────────┘ │  │ ┌──────────────────────────┐ │
│ └──────────────────────────┘ │  │ Proposed three services.     │  │ │ ● ACTIVE  kanbandev:3000 │ │
│ ┌──────────────────────────┐ │  │ ┌ PLAN · 3 SERVICES ───────┐ │  │ │ nodejs@22 · mounted      │ │
│ │ Add auth             1h  │ │  │ │ ○ kanbandev   nodejs@22  │ │  │ │                 ( Open ) │ │
│ │ ○ WAITING FOR YOU      ✳ │ │  │ │ ○ kanbanstage nodejs@22  │ │  │ │ └ ○ READY  kanbanstage   │ │
│ └──────────────────────────┘ │  │ │ ○ db          postgres   │ │  │ └──────────────────────────┘ │
│                              │  │ └──────────────────────────┘ │  │ DATA                         │
│ ⬡ SHOP       ● ACTIVE 5 svcs │  │ ┌ DEPLOY · KANBANDEV ──────┐ │  │ ┌──────────────────────────┐ │
│ ┌──────────────────────────┐ │  │ │ ✓ build ✓ deploy         │ │  │ │ ● ACTIVE db postgres@18  │ │
│ │ Fix cart             2d  │ │  │ │ [◎ kanbandev-24cb…app ↗] │ │  │ │           ( Data Console)│ │
│ │ ● TASK COMPLETE        ◇ │ │  │ └──────────────────────────┘ │  │ └──────────────────────────┘ │
│ └──────────────────────────┘ │  │ ┌ VERIFY ──────────────────┐ │  │ INFRASTRUCTURE               │
│                              │  │ │ ✓ HTTP 200 · healthy     │ │  │ ╔══════════════════════════╗ │
│ ⬡ BLOG       ◌ CREATING      │  │ └──────────────────────────┘ │  │ ║ ● ACTIVE  zcp            ║ │
│ setting up infrastructure    │  │ (Deploy)(Logs)(Add Redis)    │  │ ║ (Web Terminal)(Cloud IDE)║ │
│                              │  │ ╭──────────────────────────╮ │  │ ║ ✳ Claude Code            ║ │
│                      ( ＋ )  │  │ │ Ask for changes…    (↑)  │ │  │ ║        ● AUTHORIZED      ║ │
│  ⌕ Search                    │  │ ╰──────────────────────────╯ │  │ ╚══════════════════════════╝ │
└──────────────────────────────┘  └──────────────────────────────┘  │ live · updated 2 s ago       │
                                                                    └──────────────────────────────┘
```

### 6.5 Tablet

`AdaptiveWorkspaceLayout.tsx` already splits list · detail · inspector (`workspace-inspector-pane.tsx`, today the file navigator). The inspector takes the map, so a 12.9" iPad reads exactly like the web three-column thread view and the mental model transfers.

### 6.6 The seams, stated

- **Roboto next to SF.** Native bar titles and glass header items are system-drawn and cannot cheaply take a custom face. Roboto in content, SF in the bar. If the showcase captures read wrong, the fallback is Roboto only inside cards and the feed — an owner decision on real screenshots, not on paper (§11 D5).
- **The composer caret stays iOS blue** unless `modules/t3-composer-editor/ios/T3ComposerEditorView.swift:763` is edited (`tintColor = UIColor.systemBlue`).
- **Everything above is downstream of a promotion.** `apps/web/src/zerops/{candidates,provisioning,containerHealth,firstPrompt,serviceMap,strip,quickActions,cards/*}.ts` are UI-free TypeScript and must move to `packages/client-runtime` before a single phone screen is built. `client-runtime` already exports `./zerops` (api, session, registration, newProject); web imports it 12×, mobile 0×.

---

## 7. Desktop

Same bundle in Electron, one smoke build (brief D6). Desktop adds nothing visual of its own — `DesktopAppBranding` is names only (`packages/contracts/src/ipc.ts:175-185`) — and changes five things.

1. **First frame.** `DesktopWindow.ts:128-130` `getInitialWindowBackgroundColor` returns `#0a0a0a`/`#ffffff`; titlebar symbols are `#1f2937`/`#f8fafc` (`:34-35`). Both come from the generated `brandColors.ts` instead: canvas `#eceff3`/`#0c0f0e`, symbols `#1a1a1a`/`#e9eeec`. `TITLEBAR_HEIGHT = 40` already matches the Zerops app-bar height.
2. **WCO.** `index.css:124-137` already overrides the topbar with `env(titlebar-area-*)`; the sidebar's mark pill sits inside the reserved inset via `COLLAPSED_SIDEBAR_TITLEBAR_INSET_CLASS` (`workspaceTitlebar.ts`). Nothing new — but the smoke build must confirm the pill is not clipped, and if it is, the pill drops below the titlebar rather than the design bending.
3. **Naming.** `APP_BASE_NAME` in `branding.ts:20-27` and `apps/desktop/src/app/DesktopEnvironment.ts:88-110` → "Zerops Code"; `ElectronMenu.ts` labels; the stage pill pattern (Dev/Nightly/Latest) survives as a MicroLabel with no artwork. The `t3code://` URL scheme stays (D6, out of scope) — it is the one T3 residue a user can meet, in the OS "open with" dialog. Record it in `hacks.md`.
4. **Icons.** A Zerops `.icon` Icon Composer project per channel under `assets/{dev,nightly,prod}/app-icon.icon` (`scripts/lib/brand-assets.ts:1-32`), or `pnpm icons:check` fails. **This needs macOS tooling and is an owner/desk task, not agent work.**
5. **The one surface that earns desktop's existence.** The `preview` right-panel kind is "Only available in the desktop app" on web. On Zerops it becomes the in-app preview of a deployed subdomain: a map row's `Open` offers "Preview here" on desktop, and so does the verify card's URL chip. Per `fork.md §4` desktop also loses the local backend (`t3 serve` spawn) and Tailscale, keeping SSH launch, keychain, deep link and updater.

**One colour, two update pills.** Desktop self-updates; the container upgrades on restart. Both render the `update` role (teal/mint) — "Zerops Code 0.2 available · Restart" and "Enable the new Zerops Code on kanban · restarts the container" — so mint keeps exactly one meaning across shells.

Proof: extend `apps/desktop/scripts/smoke-test.mjs` with `webContents.capturePage()` into `artifacts/desktop/` for both appearances. A reviewed artifact, not a gate.

---

## 8. Cross-client design system and governance

### 8.1 One source, three projections

```
packages/shared/src/themePalettes.ts   ZEROPS_THEME (57 roles, light + variants.dark)
packages/shared/src/zeropsBrand.ts     status tones · mint panel · chip tints · mark path data
                                       · icon map · radius/type constants · copy glossary
scripts/generate-theme-tokens.ts       root, beside export-brand-icons.ts
   ├─► apps/web/src/generated/zerops-theme.css    :root{--app-theme-*} + @variant dark
   │        → index.css keeps only the semantic remap (1552-1617); the hand-written
   │          :root/.dark blocks (1388-1494) and [data-app-sidebar] (1499-1541) are DELETED
   ├─► apps/web/index.html boot snippet           SPLASH_COLORS / DEFAULT_THEME_PALETTES /
   │        BUILT_IN_THEME_PALETTES / <meta theme-color>  ("keep this small copy in sync")
   ├─► apps/web/public/manifest.webmanifest       theme_color / background_color
   ├─► packages/shared/src/generated/brandColors.ts  → themePreview.ts, DesktopWindow.ts,
   │        app.config.ts splash, withAndroidModern{PopupMenu,AlertDialog}.cjs, Live Activity
   ├─► apps/mobile/global.css @variant light/dark  (extend renderVariant, which today emits
   │        adaptive vars only — generate-uniwind-themes.mts:166-167)
   └─► apps/mobile/src/generated-uniwind-*.{css,json}   unchanged pipeline
scripts/export-brand-icons.ts          .icon per channel → app icons, favicons, apple-touch
```

Six hand-synced copies (R5 §1) become zero. The generator's shape mirrors the one that already works and already has a byte-equality drift test: `apps/mobile/scripts/generate-uniwind-themes.test.ts` *"keeps the committed outputs current"*, with a `--check` flag. Root scripts `generate:theme` / `generate:theme:check`, wired into CI beside `icons:check`.

**Honest scope note:** the existing generator emits one target. This one emits six heterogeneous targets (web CSS, mobile CSS, a boot HTML snippet, a TS module, a manifest, and the desktop colours). z3-engineer is right that this is an **L** phase, not an **M**. That is precisely why it is sequenced last (§10 P4) rather than first.

### 8.2 Shared vs per-client

| Shared — one copy | Per client — native implementation |
|---|---|
| Colour roles + fixed brand tokens + radius/type constants + icon map + mark path data + copy glossary (`packages/shared`) | Primitives: web `components/ui/*` (Base UI + cva); mobile RN components + native module colour payloads |
| Pure behaviour promoted to `packages/client-runtime/src/zerops/`: `strip.ts`, `serviceMap.ts`, `cards/{payloads,decode}.ts`, `quickActions.ts`, `candidates.ts`, `provisioning.ts`, `containerHealth.ts`, `firstPrompt.ts` | Layout, navigation, sheets, glass, menus, keyboard, haptics, WCO |
| The written component vocabulary: **StatusDot · MicroLabel · Chip · Pill · FlatCard · MintPanel · LifecycleBand · ServiceRow · ZeropsCard · QuestionCard · CredentialCard** — anatomy and states, never pixels | Pixels |
| The screenshot scene list (which states every client must be able to show) | The capture tool |

No UI-shape code is shared today (`client-runtime` is pure TS, zero React components) and this concept keeps it that way. Sharing components across RN and DOM would fight both shells.

### 8.3 Drift guards — tests, not review habits

1. **Byte-equality** of every generated file against a fresh render, plus `generate:theme --check` in the CI Check job.
2. **Role coverage + contrast.** `Object.keys(ZEROPS_THEME.colors) ⊇ THEME_COLOR_ROLES` for both appearances; every value opaque (theme colours are stored opaque, `themePalette.ts:310-312`); ≥4.5 for text/canvas, text/surface, `messageActionForeground/messageAction`, `accentSurfaceForeground/accentSurface`, and each of error/warning/update foreground-on-surface; ≥3.0 for `focus/canvas`; and `sidebarMutedForeground` at a 60% mix over `sidebar` ≥3.0, which is the `--sidebar-icon-color` derivation at `index.css:94-98`. Uses the existing `themeContrastRatio` (`themePalette.ts:957`).
3. **No-literal lint**, Zerops directories first with zero tolerance, then repo-wide with an allowlist: no `#hex`, no `rgba(`, no Tailwind palette utility under `components/zerops/**`, `components/ui/**`, `apps/mobile/src/features/zerops/**`. Allowlist: brand icons (`Icons.tsx`, `ProviderIcon.tsx`, `SourceControlIcon.tsx`), terminal ANSI, shiki/Pierre themes. **Today's burn-down: 217 Tailwind palette utilities on web** (hotspots `ResourceTelemetryDiagnostics.tsx` 38, `Sidebar.tsx` 34, `Sidebar.logic.ts` 32, `pullRequestPresentation.tsx` 21) **and 155 hex literals in 18 mobile files.**
4. **Vocabulary lint** over user-facing string literals in `apps/{web,mobile}/src`: no `T3 Code`, `T3 Connect`, `Tailscale`, `pairing`, `worktree`, `Local checkout`, `environment` as a noun in JSX text, or `control plane` as a self-description. Identifier allowlist; `t3code:` storage keys exempt (D6). This is zcp's own `TestNoBareZeropsURIInAgentContent` pattern.
5. **Phrase parity is structural**, not tested: once `strip.ts` is in `client-runtime` both clients import the same function.
6. **Status-vocabulary parity**: a table test that every `SETTLED_ZEROPS_SERVICE_STATUSES` entry plus the transient branch maps to a tone and a word in both clients.
7. **Perf guard**: no `infinite` keyframe without `steps()` timing outside the allowlist; no `Spinner` mounted in a map row or the band.
8. **No-mutation guard** widened from `ZeropsQuickActions.test.tsx:31` to the band, the map and the credential card (which may reach exactly one server RPC — the zcp verb).

### 8.4 Visual regression

Mobile already has the harness: `pnpm screenshots:mobile` (`scripts/mobile-showcase.ts`) drives the real app against seeded servers, and `SHOWCASE_THEMES = MOBILE_THEME_IDS` (`scripts/mobile-showcase.config.ts:15`) — so registering `zerops` captures it automatically. Add scenes: Projects, Map sheet, Thread-with-question, Thread-with-credential, by seeding an envelope and a `user-input.requested` into `mobile-showcase-environment.ts`.

Web has none. Add `scripts/web-showcase.ts` reusing the same seeded environment, driving Chromium through landing · picker · thread (idle/developing/waiting/done/error) · map · settings · palette at 1440×900 and 390×844, light and dark.

**Policy: captures are reviewed PR artifacts, not a pixel gate.** Font rasterisation differs per OS and a gate would flake. The gate is §8.3.

### 8.5 Where the decisions live

Design decisions land in `zcp/docs/spec-z3.md` as a new **§8 "The client design system"** (§7 is claimed by the SPI in the freeze checklist), holding invariants as DS-rows pinned by the tests above:

- **DS-1** Every Zerops chrome colour is a `ZEROPS_THEME` role or a `zeropsBrand.ts` token. *(lint 3)*
- **DS-2** Generated copies are byte-equal to the source. *(`generate:theme --check`)*
- **DS-3** Primary is blue; teal/mint is identity + `update` only. *(contrast test)*
- **DS-4** Every status carries a dot **and** a word. *(component test)*
- **DS-5** No infinite animation outside the duty-cycled keyframes. *(perf guard)*
- **DS-6** The lifecycle phrase comes from one shared function. *(structural)*
- **DS-7** No Zerops surface module imports a mutating RPC. *(no-mutation guard)*

The working spec — anatomy and states per vocabulary component, the icon map, the copy glossary — lives in `z3/docs/internals/zerops/design-system.md` as a dated ledger file, like the others. `packages/shared/src/zeropsBrand.ts` needs no new zone row: `packages/shared` is already **owned core** (`fork.md §3`).

---

## 9. Journey storyboard — brief §7, on a laptop and on a phone

**"Vytvoř mi kanban", side by side.**

| Agent (zcp) | Web | Phone |
|---|---|---|
| discover: proposes `kanbandev` + `kanbanstage` (nodejs) + `db` (postgresql), asks to confirm | Band turns orange: `○ WAITING FOR YOU`. The composer drawer tints `#fff4e0` and shows the three services with types, `Confirm` / `Change` / `Other…`, digit keys | Header subtitle `○ WAITING FOR YOU`; the question card takes the composer slot; the Live Activity on the lock screen says the same three words |
| `zerops_import` → processes; `zerops_mount kanbandev` | Band: `● SETTING UP INFRASTRUCTURE · IMPORT`. Import card escapes the fold with per-service step circles. Map rows appear pulsing blue `◌ CREATING`, then settle green `● ACTIVE` one after another; `kanbandev` grows `[MOUNTED]` | Row subtitle follows the band; pull up the map sheet to watch the same three rows settle |
| close: evidence written; develop starts | Band: `● INFRASTRUCTURE READY · 3 SERVICES` → `● DEVELOPING KANBANDEV` | same string, same instant |
| writes `zerops.yaml` + code under `/var/www/kanbandev/` | Files panel tree rooted per hostname; turn checkpoint on the `kanbandev` repo; diff grouped by service | Review sheet, diff grouped by service |
| `zerops_deploy kanbandev` → build → subdomain | Deploy card: step circles blue → green, then a white info-chip with a green globe and the subdomain. **In the same frame** the map row gains `( Open )` | Deploy card in the feed; tap the chip to open the site in Safari |
| `zerops_verify` | Verify card `✓ HTTP 200 · healthy`; band `KANBANDEV DEPLOYED ✓ VERIFIED ✓ · KANBANSTAGE PENDING` | header subtitle carries the same phrase, truncated from the right |
| close-mode decision; git-push needs a PAT | Question card (auto / manual) in the dock; then the **credential card** — mint, one masked field, `Store on kanbandev`, and a timeline receipt with no value in it | Credential card as a form sheet with a secure field; the value goes to the server once and is never in thread state |
| auto-close derived | Band: `✓ TASK COMPLETE`; next-task shortcut | Live Activity ends on `task complete` |

**The signature moments, named:**

- *The band flips.* `setting up infrastructure · import` becomes `infrastructure ready · 3 services` as the three map rows settle from pulsing blue to green, one after another — the GUI's process grammar and the thread agreeing in the same second, without a click.
- *Three surfaces, one fact.* The deploy card grows a URL chip, the map row grows `Open`, the verify card prints `HTTP 200 ✓` — from one topology snapshot and one envelope, with no spinner lying.
- *Everything turns orange at once.* The band, the composer drawer, the sidebar row, and the phone's Dynamic Island — and the user answers by pressing `2`.
- *The mint panel, where a Zerops user expects it.* `● ACTIVE zcp` with white `Web Terminal` / `Cloud IDE` pills and `✳ Claude Code · AUTHORIZED` with its 3px haloed green dot — the same card that is open in the next browser tab.
- *"Enable Zerops Code" on an old container.* The picker row itself counts `◌ RESTARTING · 12 s` through the ~14 s of L7 502, then slides into Connected. A restart rendered as a calm state, not an error.

---

## 10. Migration

**Effort:** S ≤ 1 slice · M 2–4 slices · L a stream.

**The ordering rule, adopted from skin-first and endorsed by all three judges, enforced as a PR gate:** phases P0–P1 touch **no file under `apps/web/src/components/zerops/**` or `apps/web/src/zerops/**`**. S4 owns the auth-gate branch (`__root.tsx`, `hostedPairing.ts`, `-chatIndexView.ts`); S6 owns `ZeropsPanel.tsx`, `ZeropsServiceMap.tsx`, `ZeropsToolCard.tsx`, `ZeropsProjectPicker.tsx`. Design does not land there until those streams stabilise.

**The re-ordering, and why I overturn the winner.** system-first put P4 "Zerops surfaces in Zerops idiom" behind three infrastructure phases. zerops-designer's fatal is correct: the visible payoff would arrive no earlier than the cheapest plan's, after weeks of net-new scope. The resolution is not to move the Zerops surfaces earlier — the collision rule forbids that — but to recognise that **the recognition surfaces are not in those directories**. So P1 delivers a product that is unmistakably Zerops without touching a single S4/S6 file, and the generator becomes hygiene at P4 where it belongs.

| Phase | What | Files | Effort | Throwaway |
|---|---|---|---|---|
| **P0 — words and marks** | Product name, mark pill replacing the wordmark, splash, favicons, titles, client labels, section renames (Devices, Coding agents), vocabulary lint. **No colour change.** | web `branding.ts:20-27`, `sidebar/SidebarChrome.tsx:80-118`, `SplashScreen.tsx`, `index.html:434,459-464`, `routes/_chat.tsx:130-134`, `_chat.index.tsx:185`, `ThemeSettings.tsx:852`, `clientMetadata.ts:80-94`, `settingsSearch.ts:28-38`, delete `SidebarStageBackdrop.tsx`; desktop `DesktopEnvironment.ts:88-110`, `ElectronMenu.ts`; mobile `BrandMark`/`T3Wordmark`→`ZeropsMark`, `CompactBrandTitle`, `mobileBranding.ts`, `authClientMetadata.ts:10`, `remoteRegistration.ts:505-506`, `app.config.ts:65-90` names (bundle ids + `t3code://` stay — D6), ~26 string files; tests `branding.test.ts`, `clientMetadata.test.ts`, `SidebarStageBackdrop.test.tsx` | S–M | none |
| **P1 — the Zerops look, without touching `zerops/**`** | `ZEROPS_THEME` + `zeropsBrand.ts`; default on both clients via the existing theme path (`useTheme.ts:39-45`, boot script, `getDefaultThemeColors` at `themePalette.ts:1224`, `MOBILE_DEFAULT_THEME_ID`); Roboto self-hosted / bundled; primitives to pills/chips/flat cards/key chips/snacks (`ui/button.tsx`, `badge.tsx`, `card.tsx`, `input.tsx`, `dialog-styles.ts`, `sidebar.tsx:799-830`, `toast.tsx`, `kbd.tsx`, `empty.tsx` + new `status-dot.tsx`, `micro-label.tsx`); **the lifecycle band** in `ChatView.tsx`; **the question-card tone** (`ChatComposer.tsx:2979`); sidebar row subtitle + StatusDot (`ThreadStatusIndicators.tsx:83-131`); mobile radii 24→16, `ControlPill` primary blue, `StatusPill` tones, user bubble | ~25 web + ~10 mobile; contrast/coverage tests; snapshot tests `button.test.tsx`, `menu.test.tsx`, `sidebar.test.tsx`, `command.test.tsx` | M | the hand-edited boot palette block (P4 generates it) |
| **P2 — Zerops surfaces in Zerops idiom** *(S4/S6-coupled; last thing before the generator)* | ServiceRow + MintPanel + liveness line (`ZeropsServiceMap.tsx`); default-open map + container-query ladder (`rightPanelStore`, `rightPanelLayout.ts`); milestone fold escape (`MessagesTimeline.logic.ts:857,934`); card process shells (`ZeropsToolCard.tsx`); landing + picker card anatomy + provisioning (`ZeropsLandingShell`, `ZeropsProjectPicker`, `ZeropsProvisioningPanel`); question-card option tiles; **credential card (GUI-link-only until the verb exists)**; `data-console` panel kind; quick actions into the context strip (`ChatView.tsx:7178`); `GitActionsControl` hidden; palette intents. **Prerequisite from S4:** the descriptor-keyed `requires-auth` branch | `components/zerops/**` (~9 files), `rightPanelStore.ts:17-27`, `RightPanelTabs.tsx:268-345`, `MessagesTimeline.logic.ts`, `ChatView.tsx`, `CommandPalette.tsx` | M | every `rounded-xl border-border/55 bg-card/20` frame; `routes/zerops.tsx` + `ZeropsProjectsPage.tsx` fold into `/` |
| **P3 — mobile Zerops screens** | Promote `apps/web/src/zerops/*.ts` to `client-runtime` **first** (blocks everything else); sign-in/TOTP/registration sheet; Projects picker; provisioning; Enable; Home project cards + row subtitles; thread header subtitle; cards in `ThreadFeed`; map sheet; Devices; Live Activity payload off `strip.ts`; showcase scenes | `App.tsx`, `Stack.tsx`, `settings-sheet-targets.ts`, both `SettingsRouteScreen` trees, `HomeScreen.tsx`, `ThreadDetailScreen.tsx`, `PendingUserInputCard`, `AgentActivity.tsx:232-236` | L | the mobile pairing-code screens for Zerops projects (kept as the manual fallback) |
| **P4 — generator, deletion, harness** | `scripts/generate-theme-tokens.ts` + `theme-tokens.test.ts` + CI `--check`; delete `index.css:1388-1541`, `T3_CODE_*_THEME_COLORS` (`themePalette.ts:307-432`), `STANDARD_THEME_PREVIEW_COLORS`, `DesktopWindow.ts` literals, `app.config.ts` splash, the Android plugin palettes, the mobile hand block; no-literal lint repo-wide with the 217/155 burn-down; `scripts/web-showcase.ts` | as §8.1 | L *(not M — six heterogeneous targets)* | none |
| **P5 — desktop smoke** | Window colours from the generator, `.icon` projects per channel, `APP_BASE_NAME`, drop the local backend from the Zerops build, `capturePage()` in `smoke-test.mjs` | `DesktopWindow.ts:34-35,128-130`, `assets/*/app-icon.icon`, `DesktopEnvironment.ts` | S | none |

Order rationale: P0 is free and removes the most visible wrongness. P1 is where the product starts *looking* like Zerops and it collides with nobody. P2 waits for S4/S6 and is the last product phase. P3 is gated on the promotion, not on design. P4 is hygiene that P1 already works without — and putting it last is the one place this concept openly overrules its own winner. P5 any time after P1.

**Not throwaway, ever:** every module under `apps/web/src/zerops/` — strip, serviceMap, quickActions, cards, candidates, provisioning, containerHealth, firstPrompt. They are the assets; they move up to `client-runtime`, not out.

---

## 11. Open decisions for the owner

**D1 — `Sidebar.tsx` stays. Confirm.** This concept keeps T3's cross-project thread list whole (3960 lines: virtualisation, dnd-kit pinned reorder, snooze shelves, prewarm) and changes only its header and row subtitle. project-first-ia's project-scoped pane was the highest brand-fidelity option and all three judges called its thread demotion a workflow regression. If you would rather have the GUI's card column than the fleet view, say so now — it changes P1 and P2 completely, not later.

**D2 — Which milestone cards escape the fold?** The concept says plan / import / deploy / verify / error escape; mount / subdomain stay inside and are named in the group summary. This is the open product call in `verified.md`, and it is a taste question about how loud a 20-tool turn should be.

**D3 — Does the map open by default?** The concept says yes on a Zerops project, remembered per thread thereafter. The cost is one less column of thread width by default on a 1280px laptop. The alternative is skin-first's keystroke, which two judges called fatal against the brief.

**D4 — The credential verb.** Its shape is owner-designed (brief §8.3). Until it exists the card renders only a GUI deep link — no field. Confirm that is acceptable for the first release rather than blocking the card entirely.

**D5 — Roboto on iOS.** Roboto in content next to SF in the system-drawn nav bar is a visible seam. Decide on real showcase captures, not on paper. Fallback: Roboto only inside cards and the feed.

**D6 — Data Console v1 is read-only.** Write authority is caller-bound on a per-request `X-Write-Token`; threading that through an embed is an S6 design task. Confirm read-only ships first.

**D7 — Service icons in map rows.** The GUI's own service cards show no per-type logo, so shipping without the 87-icon set is faithful and shipping with it is an invention. Concept ships without. Overrule if scannability matters more than continuity.

**D8 — OpenCode.** T3 ships the driver; the Zerops GUI defines `opencode-ai` but never surfaces it and has no zui mark. Decide whether it appears in the coding-agent list.

**D9 — `packages/brand` is not created.** Fixed brand tokens go in `packages/shared/src/zeropsBrand.ts`. If you want the harder boundary (a real package, its own build, its own zone row), say so — it is defensible, it is just not free.

---

## 12. Risks

1. **P1 lands and it still reads as T3.** The hero composer and the folder-shaped project row are T3's strongest tells and this concept keeps both. *Mitigation:* the recognition is concentrated at five glance points — mark pill + ⌘K pill, the tinted band, `● UPPERCASE` on every row, the mint zcp panel, the tinted process-step cards — and P2's picker is a literal GUI page. *Residual, accepted:* a T3 user recognises the bones; a Zerops user recognises the surfaces.

2. **The single-bundle gate is auth core, and design does not own it.** Without S4's descriptor-keyed `requires-auth` branch, the container bundle keeps showing a token form on first open. No skin fixes that. *Mitigation:* P2 is sequenced behind it; an interim copy patch makes the form say "Connect with a one-time link", never "Pair"; the manual fallback must stay reachable outside Zerops projects and is a test.

3. **The generator is bigger than it looks.** Six heterogeneous outputs against one existing precedent that emits one. *Mitigation:* it is P4, after the product already works; `--check` in CI makes forgetting cheap rather than silent. If it slips, nothing visible slips with it.

4. **Opaque roles vs an alpha-heavy source.** Theme colours must be opaque (`themePalette.ts:310-312`); Zerops paints 4%/8% overlays. A value flattened over white is wrong over canvas. *Mitigation:* the generator flattens per *declared* surface and records which; contrast runs against the flattened pair; hover states are expressed as roles (`sidebarRowHover`, `toolbarControlHover`), never as alpha utilities.

5. **The pink flash.** `getDefaultThemeColors` (`themePalette.ts:1224`) fills omitted roles from T3 Chat, and `index.html`'s `DEFAULT_THEME_PALETTES` hardcodes it for the pre-mount paint. A partial Zerops theme flashes pink. Both must point at `ZEROPS_THEME` in the same slice that registers it.

6. **Contrast and colour-blindness.** Teal is 2.03:1 on white and 1.76 on canvas — never text, never a white-label pill in light. `#cc0011` on `#141918` is 3.03 → mat red 300 in dark. `#00cc55` is 2.15 on white → success text needs `#0f7a38`. Zerops statuses are hue-only Material 400s. *Mitigation:* every one of these is a failing assertion in guard 2, and DS-4 makes the dot+word pairing a component test, not a habit.

7. **Drift by a thousand literals.** 217 palette utilities on web and 155 hex literals on mobile, growing under agent maintenance. *Mitigation:* zero tolerance in the Zerops directories at P2, repo-wide with an allowlist at P4; the counts are the burn-down chart. *Honest cost:* every UI slice brief must say "tokens only", and the lint is what enforces it when the brief forgets.

8. **Feed liveness is unproven (Z3F-8), and a stale map lies.** A default-open map that lags is worse than a tab you open on purpose. *Mitigation:* the liveness line is always rendered (`live · updated N s ago` / `polling` / `last read failed`); a running tool is attributed to the map, not to a row, because `ZeropsRecentTool` carries no hostname and `serviceMap.ts:66-74` says so in a comment; a service created in the web GUI appearing without a refresh is the S6 acceptance test and the map is where it must show.

9. **The mint panel promises what S7 has not built.** An `AUTHORIZED` tray with no Authorize action, and SSH / Desktop IDEs that need a VPN that is out of scope. *Mitigation:* v1 rows are read-only status with `Authorize in Zerops ↗`; SSH and Desktop IDEs are labelled link-outs; the four-pill grammar is kept intact so S7 fills it without moving anything.

10. **Mobile is designed on paper.** Zero Zerops code on `main`, no screenshot of the native app exists, Turnstile-in-a-WebView is unverified. *Mitigation:* the phone concept is deliberately thin — subtitles, a sheet, cards, a Live Activity — and every piece rides on logic promoted from web first, so the first build proves the promotion before it proves the design. The showcase harness catches a wrong-looking screen before TestFlight.

11. **Performance.** One webfont, a new band, pulses. *Mitigation:* Roboto self-hosted, subset, preloaded, `swap`; no Material Icons webfont; the band and the map add zero `backdrop-filter` surfaces; topology publishes only on change (spec §5.1) and rows memoise on `serviceId+status+mounted+subdomainUrl`; only `steps()` keyframes; the timeline stays virtualised.

12. **A theme library that can un-Zerops the product.** A user on "Grove" beside a mint zcp panel looks odd. *Mitigation:* the Zerops surfaces read status tones and chip tints from `zeropsBrand.ts`, not from roles, so the grammar survives any theme; "Reset to Zerops" is one click. *Not mitigated, accepted:* the user chose it.


## Load-bearing claims
1. The question card is ALREADY docked at the composer on web, inside `.chat-composer-top-drawer`, whose tone is a single `data-variant` ternary — so agent-console's 'dock the question card' graft is a tone change, not a relocation. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/chat/ChatComposer.tsx:2979 (data-variant), :2996 and :3027 (two ComposerPendingUserInputPanel mounts); tint definitions at apps/web/src/index.css:929-947. NOTE: grep treats ChatComposer.tsx as binary — use `grep -a`._
2. The fold-escape for Zerops milestone cards has an exact in-file precedent: agent-spawn rows are already exempted from the collapsed tool group with the comment 'a running fleet must never hide behind a +N tool calls toggle'. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/chat/MessagesTimeline.logic.ts:857-860 (onlyToolEntries predicate) and :928-940 (overflowCandidates filter + comment)._
3. The collapsed work-group label is a generated summary plus '+N previous tool calls', NOT the literal 'Used N tools' that all four proposals quote. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/chat/MessagesTimeline.logic.ts:346-364 (summarizeToolGroup); apps/web/src/components/chat/MessagesTimeline.tsx:1572-1604 ('+{hiddenCount} previous {labelNoun}')._
4. agent-console's rail cannot fit: sidebar 256 + thread min 640 + rail 280 + right panel (max 28rem = 448) = 1624px, past a 1440px laptop. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/threadSidebarWidth.ts:2-4 (16*16=256, 13*16=208, 40*16=640); apps/web/src/rightPanelLayout.ts:2 (w-[min(42vw,28rem)] min-w-80 max-w-[28rem])._
5. The right panel's inline/sheet switch is a fixed viewport media query at 980px, and ChatHeader already sets a container-query precedent, so swapping to a container query on the main column is cheap and in-idiom. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/rightPanelLayout.ts:1 (RIGHT_PANEL_INLINE_LAYOUT_MEDIA_QUERY = '(max-width: 980px)'); apps/web/src/components/chat/ChatHeader.tsx:293 (@container/header-actions) and :384 (@3xl/header-actions)._
6. Keeping Sidebar.tsx whole is a scope decision worth 3960 lines of shipped behaviour; the concept changes only its header and the row's second line. — _verify: `wc -l /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/Sidebar.tsx` = 3960; the branch it plugs into is apps/web/src/components/AppSidebarLayout.tsx:229-236 (isOnSettings ? SettingsSidebarNav : legacySidebarEnabled ? LegacyThreadSidebar : ThreadSidebar); header at apps/web/src/components/sidebar/SidebarChrome.tsx:80-118._
7. ZeropsPanel.tsx (58 lines) and the `zerops` right-panel kind are small, live, and owned by S6 — so project-first-ia's phase-2 deletion of them is the highest-collision move of the four proposals, and this concept keeps both. — _verify: `wc -l /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/zerops/ZeropsPanel.tsx` = 58; RIGHT_PANEL_KINDS at apps/web/src/rightPanelStore.ts:17-27 includes 'zerops'; S6 ownership in /Users/macbook/Documents/Zerops-MCP/zcp/CLAUDE.local.md and plans/z3-brief-2026-08-28.md §6 S6._
8. THEME_COLOR_ROLES is exactly 57 roles, and registering a ZEROPS_THEME id makes the mobile uniwind generator emit `@variant zerops-light/dark` automatically and makes the mobile showcase harness capture it automatically. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/packages/shared/src/themePalettes.ts:18-80 (count the quoted role strings = 57); apps/mobile/scripts/generate-uniwind-themes.mts:164-183; scripts/mobile-showcase.config.ts:15 (SHOWCASE_THEMES = MOBILE_THEME_IDS)._
9. On mobile the question card ALREADY replaces the composer, with a comment saying so — the phone's answer surface needs tone and 'Other', not relocation. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src/features/threads/ThreadDetailScreen.tsx:696-735, including the comment 'The questionnaire replaces the composer, so it must pad the home indicator the composer normally covers.'_
10. Mobile has zero Zerops code on main, so every phone screen in this concept is downstream of promoting the UI-free web modules to packages/client-runtime. — _verify: `grep -ril zerops /Users/macbook/Documents/Zerops-MCP/z3/apps/mobile/src` → 0 files; the modules to promote are listed by `ls /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/zerops/`; client-runtime already exports ./zerops (packages/client-runtime/src/zerops/)._
11. The container-served bundle falls through to /pair today because isHostedStaticApp() is false there; a descriptor-keyed requires-auth branch is the one structural prerequisite design cannot supply. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/hostedPairing.ts:34-45; apps/web/src/routes/__root.tsx:62-83; apps/web/src/routes/-chatIndexView.ts; screenshot scratchpad/shots/t3-live/15-zerops-page.png shows the current signed-out Zerops page._
12. The in-flight pulse can be Zerops-looking without violating AGENTS.md's no-continuous-repaint rule, because duty-cycled steps()-timed keyframes already exist and are used for exactly this. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/index.css:238-251 (@keyframes status-pulse with steps(6)); :150 (--animate-status-pulse); AGENTS.md:155 ('No continuously repainting animations')._
13. The no-mutation guard the concept widens to the band and map already exists as a named test on quick actions. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/components/zerops/ZeropsQuickActions.test.tsx:31 — it('cannot reach Zerops or the RPC layer at all')._
14. Adding packages/brand is avoidable: packages/shared is already 'owned core' in the fork's zone table, so zeropsBrand.ts needs no new workspace member, build wiring, or zone row. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/docs/internals/zerops/fork.md §3 zone table (Owned core includes packages/{contracts,client-runtime,shared,ssh}); packages/shared/src/themePalettes.ts and themePreview.ts already sit there and are consumed by both clients._
15. Desktop's only colour surface is two constants plus an initial background function, so generating them is a three-line change once brandColors.ts exists. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/desktop/src/window/DesktopWindow.ts:33-35 (TITLEBAR_HEIGHT 40, TITLEBAR_LIGHT_SYMBOL_COLOR #1f2937, TITLEBAR_DARK_SYMBOL_COLOR #f8fafc) and :128-130 (getInitialWindowBackgroundColor #0a0a0a/#ffffff); packages/contracts/src/ipc.ts:175-185 (DesktopAppBranding = names only)._
16. The service map's group order and the 'a running tool belongs to the map, not a row' rule are already decided in code with reasons, so the concept inherits rather than invents them. — _verify: /Users/macbook/Documents/Zerops-MCP/z3/apps/web/src/zerops/serviceMap.ts:20-25 (GROUP_ORDER + 'Reading order: what the user builds, what it stores, what runs it') and :66-74 (runningTool comment)._

## Owner decisions
- D1 — Sidebar.tsx stays whole (3960 lines: virtualisation, dnd-kit pinned reorder, snooze shelves, prewarm); only its header and row subtitle change. Confirm, or choose project-first-ia's project-scoped card column instead — that changes P1 and P2 completely and must be decided before P1, not after.
- D2 — Which Zerops cards escape the collapsed work group. Concept: plan / import / deploy / verify / error escape; mount / subdomain stay folded and are named in the group summary. This is the open product call recorded in verified.md.
- D3 — Does the service map open by default on a Zerops project? Concept: yes, remembered per thread thereafter. Cost is one column of thread width on a 1280px laptop; the alternative (a keystroke) was called fatal by two of three judges.
- D4 — The credential card renders only a 'set it in the Zerops GUI' deep link until the zcp storage verb exists (verb shape is owner-designed, brief §8.3). Confirm that ships rather than blocking the card.
- D5 — Roboto in content next to SF in iOS system-drawn nav bars. Decide on real showcase captures, not on paper. Fallback: Roboto only inside cards and the feed.
- D6 — Data Console v1 is read-only in the embed; write authority is caller-bound on a per-request X-Write-Token and threading it through an iframe is S6 design work. Confirm read-only first.
- D7 — Service-type icons in map rows: the Zerops GUI's own service cards show none, so shipping without the 87-icon set is faithful and shipping with it is an invention. Concept ships without.
- D8 — Does OpenCode appear in the coding-agent list? T3 ships the driver; the Zerops GUI defines opencode-ai, never surfaces it, and has no zui mark.
- D9 — No `packages/brand`: fixed brand tokens live in packages/shared/src/zeropsBrand.ts (same package, no new workspace member, no zone row). Overrule if you want the harder package boundary — it is defensible, just not free.
- D10 — The generator is an L phase emitting six heterogeneous targets against one existing single-target precedent, and it is sequenced LAST (P4), overturning the winning proposal's own order. Confirm you would rather have the visible product at P1 than the token infrastructure first.
