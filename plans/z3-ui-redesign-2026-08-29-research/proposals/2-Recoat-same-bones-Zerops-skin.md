# Recoat — same bones, Zerops skin

## Thesis
z3 already owns the one mechanism a skin needs: a `ThemeDefinition` of 57 roles in `packages/shared/src/themePalettes.ts:18-77` that reaches web through `applyThemePalette` → `--app-theme-*` → the `html[data-theme-id]` remap (`apps/web/src/index.css:1552-1617`) and mobile through `createMobileThemeVariables` + the uniwind generator (`apps/mobile/src/lib/mobileTheme.ts:208-283`, R5 §1). The only thing not driven by it is the default look, hand-written on both clients (`index.css:1388-1494`, `apps/mobile/global.css:6-210`). The angle: register ZEROPS as a built-in theme and make it the default on web and mobile; restyle the ~18 `components/ui/*` primitives toward Zerops shapes (pills, flat tone-on-tone cards, Roboto, dot+label status); replace the brand touchpoints and sweep the vocabulary; and polish only the Zerops surfaces that already exist (landing, picker, provisioning, strip, map, cards, quick actions, Settings → Zerops) into Zerops idioms. T3's IA — sidebar of projects/threads, hero composer, right-panel surfaces — stays exactly as built. This is right for Zerops Code now because S4 and S6 are building on that IA in the same files (`ZeropsProjectPicker`, `ZeropsServiceMap`, `ZeropsLifecycleStrip`); a structural redesign would collide with two live streams, while a skin is orthogonal, lands as its own slices, and yields a product that reads as Zerops at every glance point the GUI user already recognises (brand pill, ⌘K pill, dot+label, mint panel). The cost is stated, not hidden: afterwards the product is still shaped like T3 — a thread inbox with a hero composer — and the seams (a T3 "project" called `www`, the map living in a side panel, cards folded under "Used N tools", "Local checkout" in the footer) remain visible to anyone who knows T3.

## Information architecture
## Shell — unchanged, restyled
Sidebar 256px / min 208 (`apps/web/src/components/threadSidebarWidth.ts`), topbar 52px (`index.css:79-121`), main column, right panel inline ≥981px and a Sheet below (`rightPanelLayout.ts:1`, `RightPanelSheet.tsx`; R2 §1–2). The sidebar plays the GUI's "controls pane" role in spirit only: brand pill on top, ⌘K pill, thread rows as flat white cards on the `#eceff3` canvas (R1 §1).

Top-level objects, in user words: **project** (= z3 environment, brief D3), **thread**, **services** (rows of the map), **coding agent** (T3 provider), **device**. "Environment" never appears in copy (R6 §3.2).

## Where each Zerops thing lives
| Thing | Home under Angle A | Component today (R2 §5) |
|---|---|---|
| Sign in / register / TOTP | index route when no environment (`routes/-chatIndexView.ts`) **and, new,** the `requires-auth` branch of the container bundle | `ZeropsLandingShell`, `ZeropsHostedLanding` |
| Picker · New project · provisioning | `/zerops` page; sidebar utility item renamed **Projects** (`SidebarChrome.tsx` `SidebarUtilityMenu`) | `ZeropsProjectsPage`, `ZeropsProjectPicker`, `ZeropsProvisioningPanel` |
| Lifecycle strip | beside `ChatHeader` in the 52px topbar, right of the breadcrumb, as a dot+label pill | `ZeropsLifecycleStrip` (`ChatView.tsx:6906-6928`) |
| Service map | right-panel kind `zerops` (`rightPanelStore.ts:17-27`), add-menu label **Services**, shortcut Z (`RightPanelTabs.tsx:268-345`) | `ZeropsPanel` + `ZeropsServiceMap` |
| Cards plan/import/mount/deploy/verify/subdomain/error | timeline rows via the total decoder | `ZeropsToolCard` at `MessagesTimeline.tsx:2605` |
| Question card | composer-attached pending panel | `chat/ComposerPendingUserInputPanel.tsx` |
| Quick actions | pills above the composer, prefill only (Z3F-7) | `ZeropsQuickActions` |
| Credential card | does not exist (S6); the skin fixes its look: a mint panel in the timeline, value → z3 server → service secret | — |
| Data Console | does not exist in z3 (grep = 0, R2 §5); smallest home is a ninth right-panel kind `data-console` (iframe, caller-bound) — S6 work, not skin | — |
| Account · orgs · sign out | Settings → Zerops | `settings/ZeropsSettings.tsx` |
| Devices | Settings → Connections renamed **Devices**; rows labelled `Zerops Code · <browser> on <os>` (spec §4.8) | `ConnectionsSettings` |

## The flow (spec-z3 §4)
1. Open `https://<zcp>-8080.prg1.zerops.app/z3/`. Today the container-served bundle falls through `__root.tsx:62-83` → `requires-auth` → `/pair` and shows "Pair with this environment" (`PairingRouteSurface.tsx:41-160`); only the separately hosted bundle passes `isHostedStaticApp()` (`hostedPairing.ts:35-46`). Angle A's **one structural requirement**: when the origin answers the z3 descriptor (`/.well-known/t3/environment`, §4.5), `requires-auth` renders the Zerops sign-in card; the token form survives as a footer link "Connect another device with a one-time link" — the mechanism of §3.5 without the word. This is S4 gate work (R2 §5 lists the change set: gate branch, identity connect against `window.location.origin`, `resolveChatIndexView` widened, `clientMetadata` keyed on Zerops, Turnstile allowlist or the app.zerops.io hand-off). A skin cannot paper over it; without it there is no single product experience.
2. Sign-in → picker: Connected / Ready to connect / Preparing / Not available (`ZeropsProjectPicker.tsx:178-231`). "Enable Zerops Code" turns the row into `◌ RESTARTING · 12 s` for the ~19 s / 502 window, capped at 30 s (§4.5, R6 §4), then `● READY · Open`.
3. New project → provisioning rendered as Zerops process steps (awaiting-project 60 s → awaiting-container 300 s → awaiting-health 30 s, caps shown as `0:41 / 5:00`); `pool-exhausted` is a plain state card, `timed-out` keeps "Keep waiting" / "Check it in the Zerops GUI" (existing copy, §4.4).
4. Identity connect → thread, onboarding prompt composed, never sent (§4.8, Z3C-8). The hero headline reads the Zerops project name; the dotted project menu is hidden when the environment holds one project — the D3 case (`chat/DraftHeroHeadline.tsx:158-170`).
5. Second device: Devices → "Connect another device" shows the one-time link + QR; mobile scans it with the existing `ConnectionsNewRouteScreen` camera (R3 §1) until S5 lands identity sign-in there.

## Empty states
- No services yet: strip `● NO SERVICES YET` (grey); Services panel shows the three group kickers and one line "the agent proposes services when you ask for something to build", plus the quick actions. The GUI's 170px grey disc + mono heading (R1 §6) is Material legacy and is not ported; `ui/empty.tsx` with the mark suffices.
- Feed off (`available:false`, Z3F-1): one sentence, no spinner, no retry. Degraded: last-good rows + one warning line. Doorbell down: one quiet line (§5.4).
- New-account first thread: the auto-bootstrap thread defaults to Codex and the picker reads "No models found" until typed (R6 §4) — default to the first authenticated coding agent; a copy/default fix, not IA.

## Reverse states (AGENTS.md "Reverse states")
Sign out (Settings → Zerops); Stop waiting / Back to projects (provisioning); close panel; forget a device (Devices); pick another theme (Zerops stays a library card marked Default); the one-time-link connect fallback the POC kept as `ManualConnectFallback`; "Show hidden tools" to unfold cards.

## What stays T3-shaped, deliberately — the seams
- **The sidebar project row.** Threads group under a T3 project whose name is `www` (auto-bootstrap, R6 §4). Skin mitigation: on identity-door environments the row label is the Zerops project name (one lookup in `Sidebar.logic.ts`), the "All projects" scope menu and the `add-project` palette intent (`CommandPalette.tsx:476-478`) are hidden. The row keeps T3's folder-shaped semantics and timestamp; a Zerops user will still ask "what is this project row".
- **The hero composer** remains the landing of every new thread. The GUI has no equivalent; it is the biggest visual tell that this is T3.
- **The map is a side panel**, one `Z` away, not the page. The GUI puts services in the content pane. Recommended one-line default: open the `zerops` panel on first landing in an identity-door environment (`rightPanelStore` default).
- **Cards render inside the "Used N tools" fold** (R6 §4, verified.md). Escaping the fold is a `MessagesTimeline` change and an S6 product call.
- **Footer "Local checkout · main"** (`BranchToolbar`, `ChatView.tsx:7158-7188`): renamed `<service> · main` once the multi-repo driver (spec §6) reports it; until then hidden on Zerops environments.
- **Pull requests, Commit ▾, worktree copy** are hidden on Zerops environments (D4: never two commit pipelines), not redesigned.

## Visual system
## Tokens — the ZEROPS ThemeDefinition
Values from R1 §2 and R5 §2 (alphas flattened per surface; theme colours must be opaque, `themePalette.ts:310-312`). Roles = `THEME_COLOR_ROLES` (`packages/shared/src/themePalettes.ts:18-77`); web consumers `index.css:1552-1617` (`--primary ← messageAction`, `--ring ← focus`, `--accent ← accentSurface`), mobile `mobileTheme.ts:208-283` (57 roles → 65 `--color-*`).

| Role | Light | Dark | z3 consumer |
|---|---|---|---|
| canvas · chrome · toolbar | `#eceff3` | `#0c0f0e` | `--background`, topbar, mobile `screen`/`status-bar` |
| surface | `#ffffff` | `#141918` | `--card`, thread rows, map cards |
| surfaceRaised | `#ffffff` | `#1b2220` | composer glass, banners |
| surfaceOverlay | `#ffffff` | `#1e2624` | `--popover`, menus, dialogs |
| secondary · muted | `#f3f5f7` · `#f2f5f7` | `#232b29` · `#151b1a` | secondary pills, chips, blockquote |
| accentSurface / Foreground | `#f0f3f5` / `#1a1a1a` | `#232b29` / `#e9eeec` | `--accent` hover, selected rows |
| text · textMuted | `#1a1a1a` · `#5f6a72` | `#e9eeec` · `#9faea9` | `--foreground`, `--muted-foreground` |
| placeholder · secondaryLabel · iconMuted | `#757575` · `#7d8891` · `#5f6a72` | `#5e6e69` · `#7d8c88` · `#9faea9` | |
| border · input · toolbarBorder | `#e0e0e0` · `#d4dfea` · `#d4dfea` | `#262f2d` · `#2a3331` · `#262f2d` | |
| toolbarControl / Hover / Foreground | `#e3e6ea` / `#d9dce0` / `#1a1a1a` | `#1b2220` / `#2a3331` / `#e9eeec` | header controls, ⌘K pill |
| messageAction / Hover / Foreground → `--primary` | `#0077cc` / `#005fa3` / `#ffffff` | `#0077cc` / `#008ff5` / `#ffffff` | send, CTAs, switches, sliders (4.66:1 both ways) |
| focus → `--ring` | `#0077cc` | `#5ab3ff` | |
| accent / accentForeground (mobile primary btn, md-link) | `#0077cc` / `#ffffff` | `#58a6ff` / `#0c0f0e` | web-unconsumed role (R2 §3) |
| messageSurface / Foreground | `#e3f4ff` / `#1a1a1a` | `#1e2e3b` / `#e9eeec` | user bubble = Zerops core-blue tint |
| update / Foreground / Surface | `#02b1a3` / `#007e72` / `#def8f6` | `#00e5c0` / `#58efd4` / `#113630` | update pills — the one role where teal/mint carries meaning |
| error / errorForeground / errorSurface | `#cc0011` / `#cc0011` / `#fdefef` | `#e57373` / `#e57373` / `#2f1717` | |
| warning / Foreground / Surface | `#ffa726` / `#bb4d00` / `#fdf6ef` | `#ffa726` / `#ffb74d` / `#3a301a` | |
| codeBackground / Foreground | `#f2f5f7` / `#1a1a1a` | `#121716` / `#dce4e1` | code, `--diffs-bg` |
| sidebar / Foreground / Muted | `#eceff3` / `#1a1a1a` / `#4f5a62` | `#0c0f0e` / `#e9eeec` / `#9faea9` | muted darkened so `--sidebar-icon-color` (60 % mix, `index.css:94-98`) clears 3:1 |
| sidebarControlSurface · RowHover · RowActive · RowSelected · Border | `#e3e6ea` · `#e3e6ea` · `#dfe2e6` · `#ffffff` · `#e0e0e0` | `#151b1a` · `#151b1a` · `#1b2220` · `#141918` · `#262f2d` | selected thread = a white card |
| terminalBackground / Foreground / Cursor / Selection / Scrollbar / Hover | `#f2f5f7` / `#1a1a1a` / `#007e72` / `#cce4f5` / `#d4d8dc` / `#b9bec3` | `#0c0f0e` / `#e9eeec` / `#00e5c0` / `#213951` / `#2a3331` / `#3a4442` | ANSI-16 stays Ghostty default (R5 risk 6) |

Non-theme colours that change with it (`index.css:1429-1432, 1480-1482`): `--success #00cc55` + `--success-foreground #0f7a38 / #56d364`; `--info #0077cc / #58a6ff`. Brand teal `#00ccbb` / mint `#00e5c0` appear only as the mark, the sidebar brand-pill tint `rgba(0,204,187,.13)`, the palette scope chip, update pills, and connected/authorized dots — never as text on light (2.03:1, R5 §3). Provider accent fallback `#2563eb` (`ProviderAccentColorPicker.tsx:21`) → neutral; teal excluded from the swatches.

**Zerops-surface tints** — not theme roles (the theme is opaque-only and every new role touches the mobile generator): `--zerops-panel #e8f7ec / rgba(76,175,80,.10)`, `--zerops-panel-needs-you #fff4e0 / rgba(232,163,61,.12)`, `--zerops-panel-processing #fff9e6`, `--zerops-panel-failed #fdefef / #2f1717`, `--zerops-panel-running #eff9fd / #1e2e3b`; chips access-green `rgba(76,175,80,.15)/#388e3c`, region-purple `rgba(156,39,176,.15)/#7b1fa2`, info-chip `rgba(255,255,255,.9)` (R1 §6). Web: a `[data-zerops-surface]` block in `index.css`; mobile: entries in `ADAPTIVE_COLORS` (`scripts/generate-uniwind-themes.mts:56-138`, R3 §2). A user on "Grove" keeps mint panels — they are brand, like the Claude mark.

## Type
- **Roboto** 400/500/700, self-hosted woff2 (latin + latin-ext), preloaded, `font-display: swap` — no Google Fonts fetch from a container origin. Web: `index.css @theme:139-144` + `appearanceFonts.ts:20-26`; mobile: `Roboto-Regular/Medium/Bold` via `@expo-google-fonts/roboto` replacing DM Sans in `app.config.ts:108-112,241-264`, `global.css:213-217`, and `withAndroidModernAlertDialog.cjs` (R3 §2, R5 §5). The `font-t3-*` utilities rename.
- Web scale: body 14 (`sm:text-sm`; 16 on touch as today); names 20/500 with `:port` at .6 opacity; section kickers 10/600 uppercase `.06em` at .45 alpha; **status labels 10/600 uppercase `.06em`** — the GUI's 8px (R1 §3) is below the readable floor on web and mobile, a deliberate deviation; chips 10/500 uppercase `.5px`; card descriptions 13/1.75 at .7. Mobile keeps `--text-3xs…3xl` 11–30 (`src/lib/typography.ts`).
- Mono: keep T3's SF Mono/Menlo stack for code, diff and terminal (perf, R5 §5); Roboto Mono only inside Zerops cards for hostnames and URLs.

## Shape
- `--radius` stays 10px (= `.mat-card`, R1 §4); `--control-radius` stays 8px for inputs, menus, icon buttons.
- **Pills:** `ui/button.tsx:38-60` variants `default`, `secondary`, `outline`, `destructive`, `glass` → `rounded-full`; `ghost`, `ghost-muted`, `link` and every `icon*` size keep 8px. `ui/badge.tsx:17-37` → `rounded-[10px]` chips; `success/warning/error/info` take the GUI chip tints, `outline` becomes the white info-chip. `Card`/`CardFrame` (`ui/card.tsx`) → 8px, borderless and shadowless in light, 1px `rgba(255,255,255,.06)` in dark. The grey `c-soft-elevation` triple shadow only on `popover`, `menu`, `dialog` (`dialog-styles.ts` radius 16); T3's dark `dialog-backdrop` stays — the GUI's white scrim is legacy (R1 §10). Sheet: 16px top corners. Composer: 22px/16px clip-path untouched (hard-coded geometry, `index.css:993-1060`). `kbd` 3px key-chip. Toast (`ui/toast.tsx`) 8px solid snack colours `#66bb6a/#42a5f5/#ffa726/#cc0011`, dark `#203b26/#3a2c14/#3a1d1a`. Tooltip white, 12/500, soft shadow.
- Selection = **2px `#0077cc` outline** (`rgba(0,229,192,.4)` dark) on the selected thread row, picker row and question option — never a filled background.

## Status vocabulary
One component, `StatusDot` — web `components/ui/status-dot.tsx`, mobile `components/StatusDot.tsx` generalising `ConnectionStatusDot.tsx:23-41` — renders `● LABEL`: 8px dot, radius 1000px, label 10/600 uppercase. Pulsing means in flight and uses the existing duty-cycled `status-pulse` steps() keyframes (`index.css:145-268`), never an `infinite` opacity loop (AGENTS.md Taste).

| Tone | Dot | Used by |
|---|---|---|
| idle | grey-400 `#bdbdbd` | strip idle, PENDING |
| active (pulse) | blue-400 `#42a5f5` | strip active, CREATING/UPGRADING/BUILDING, RESTARTING, `<tool> running` |
| waiting (pulse) | orange-400 `#ffa726` | strip "waiting for you", STOPPED; unauthorized agent `#b26a00` |
| done | green-400 `#66bb6a` | strip done, ACTIVE, verified; AUTHORIZED `#2e7d32` + 3px halo |
| failed | red-400 `#ef5350` | FAILED/ACTION_FAILED/CONTAINER_FAILED, deploy failed |
| ready | blue-300 `#64b5f6` | READY_TO_DEPLOY |

Every dot carries its label (hue-only statuses are CVD-confusable, R5 risk 3). Process steps in cards: 17px glyph in a 2px-bordered white circle on a `30px 1fr` grid — queued `hourglass`, running `play` blue with a pulsing ring, done `check` green, failed `!` red (R1 §6), in lucide.

## Iconography
lucide (web) and SF Symbols/Tabler (mobile `AppSymbol.tsx:86-189`) stay; no Material Icons webfont (R5 §5). One 15-glyph table for Zerops surfaces: services `layers`/`square.stack.3d.up`, terminal `terminal`, Cloud IDE `code-2`/`chevron.left.forwardslash.chevron.right`, open URL `external-link`/`arrow.up.right.square`, mounted `folder-open`, key `key-round`, restart `rotate-cw`, region `globe`, agent `bot`, waiting `circle-help`, deploy `rocket`, verify `check-circle-2`, data `database`, infrastructure `server`, project `hexagon`. Provider marks: port the zui `currentColor` SVGs (R6 §2 — Codex CC0, Cursor official glyph, Grok/Antigravity MIT; confirm Claude/Ubuntu provenance) for agent rows; T3's coloured `Icons.tsx` marks stay in the model picker. The mark: the 4-path SVG from `frontend-legacy/libs/zui/src/logo/` as `ZeropsMark` on web and mobile (`react-native-svg`, the `withUniwind(Path)` pattern of `T3Wordmark.tsx`). The 87 service-type SVGs are not used in the map — the GUI's own cards show none (R6 §2).

## Motion budget
Route fade ≤200ms; popover 2px translate 150ms; easing `cubic-bezier(.4,0,.2,1)`; pulse = `status-pulse` 1500ms duty-cycled. Not ported: ripple rings, `bounceHorizontal`, shimmer sweep, bokeh, `pulseFullReverse`, the sticky-header pseudo-element (R1 §7). Kept: the composer view-transition morph (`index.css:8-76`); `prefers-reduced-motion` disables pulses (`index.css:2513`).

## Web
## Sidebar (`Sidebar.tsx`, `sidebar/SidebarChrome.tsx`)
Header: brand pill — `ZeropsMark` 20px on a `rgba(0,204,187,.13)` pill, radius 17px, + "Code" 14/500 muted (`SidebarChrome.tsx:80-118`; `T3Wordmark` deleted). Stage art off (`sidebarArtwork:false`; `SidebarStageBackdrop` renders only for Dev/Nightly anyway, R2 §4). Search row → ⌘K pill: `rgb(0 0 0/4%)`, radius 80, 12/600 label, key-chip. New-thread icon stays. Project row: hexagon + Zerops project name + `● ACTIVE` (the container's topology status) + timestamp. Thread rows: 13/500 title; second line = the strip label as `● DEVELOPING KANBANDEV` in tone colour, replacing T3's emerald/violet/teal status text (`components/ThreadStatusIndicators.tsx:83-131,427`); snooze blue (`Sidebar.tsx:1293,3857-3866`) → `--info`; provider mark 14px currentColor at right. Selected row = white card + 2px blue outline. Utility row: Settings, Pull requests (hidden on Zerops), **Projects** (cloud icon → `/zerops`), Usage.

## Thread view
- **Topbar 52px.** Breadcrumb `⬡ kanban / Build the kanban` (`chat/ChatHeader.tsx:296-380`; `ProjectFavicon` → hexagon). The **strip** as a pill right of it: `● DEVELOPING KANBANDEV` (blue, pulsing) · `● WAITING FOR YOU` (orange) · `● TASK COMPLETE` (green) · `● ZEROPS_DEPLOY RUNNING`; phrases verbatim from `zerops/strip.ts` (pinned by `strip.test.ts`), uppercase by CSS only; `ZeropsLifecycleStrip.tsx:47-59` swaps its `Spinner` for `StatusDot`; click still opens the Services panel. Right: `Open ▾` restricted to Cloud IDE on Zerops, `Commit ▾` hidden (D4), panel toggles.
- **Timeline** (`chat/MessagesTimeline.tsx`): user bubble on `messageSurface` blue tint, 16px; assistant prose on the canvas; turn folds. Zerops cards = flat white cards with a 10/600 uppercase kicker (`PLAN`, `IMPORT · 3 SERVICES`, `DEPLOY KANBANDEV`, `VERIFY`), tinted per state (running `#eff9fd`, failed `#fdefef`, done white). Deploy card: process-step glyphs → URL chip (white info-chip, green globe) → `✓ HTTP 200`. Error card: red kicker, log lines on `codeBackground`, hint, `network` chip (live shapes, R6 §4). Undecodable → generic tool block (Z3F-7), unchanged.
- **Question card** (`chat/ComposerPendingUserInputPanel.tsx`): white card, orange `● WAITING FOR YOU` kicker; options as rows, the chosen one with the 2px blue outline; digits 1–9 and arrows kept; `Other…` field; `Answer` blue pill. No mint — it is a question, not infrastructure.
- **Credential card** (new, S6): `--zerops-panel` mint card, key icon, one masked field, `Save to kanbandev` blue pill, then `✓ Saved · kanbandev · GIT_TOKEN`. The value never renders in the timeline (brief §4 rule 4).
- **Composer** (`chat/ChatComposer.tsx`): 22px glass shell unchanged; model picker shows the agent mark + "Claude Code"; quick actions become white flat pills above the shell (`ZeropsQuickActions` → `Button variant="outline" size="compact"`, now pill); send button `#0077cc`, round; context ring unchanged.

## Right panel — Services (`ZeropsPanel.tsx`, `ZeropsServiceMap.tsx`)
Group kickers 10/600 uppercase at .45: RUNTIMES / DATA / INFRASTRUCTURE (`zerops/serviceMap.ts:20-24`). Rows (today `rounded-xl border-border/55 bg-card/20`, `ZeropsServiceMap.tsx:67`) → flat `#f3f5f7` cards (dark `#141918` + hairline), 8px, padding 16: line 1 `● ACTIVE` + `kanbandev:3000` 20/500 with `:3000` at .6 + `→` at .2 opening the subdomain; line 2 type label + chips `Domain public` (green globe) / `Mounted /var/www/kanbandev` / `stage: kanbanstage` / `prod ↗`; failed rows red-tinted; transient rows pulse blue. The zcp row is the **mint panel**: `● ACTIVE zcp:8080`, "Zerops Control Plane v1", white pills `Terminal` (opens the z3 terminal drawer) / `Cloud IDE` (code-server on the same origin, new tab) / `Data Console` (when S6 adds it), then the auth tray `✱ Claude Code · ● AUTHORIZED` from T3's provider status — the GUI's `zagent-service-card` (R1 §9), minus "Desktop IDEs" (VPN is out of scope, D5). Footer: one quiet line for doorbell state.

## Landing, picker, provisioning
Landing (`zerops/landing/ZeropsLandingShell.tsx`): centred 400px white card, radius 16, on the canvas; mono `ZeropsMark` + `ZEROPS CODE` 10/600 kicker; "Sign in with your Zerops account"; fields with 6px white outlines; `Sign in` blue pill; `GitHub` `#24292e` / `GitLab` `#9b51e0` pills hand off to app.zerops.io like registration (no OAuth in `client-runtime/zerops`, R3 §6); "Create account" → hand-off (Turnstile, Z3C-3); TOTP as a second card. Picker rows → flat cards with `StatusDot` and a right-aligned action pill. Provisioning → the process-step column with caps, `Keep waiting` / `Check it in the Zerops GUI`.

## Settings and palette
Sections (`settings/settingsSearch.ts:28-38`): General · Appearance (Zerops = "Default" card; T3 Chat, Grove, Ocean, Ember, Iris stay) · Keybindings · **Coding agents** (Providers) · Integrations · Source Control (worktree rows hidden on Zerops) · **Devices** (Connections) · Zerops (account, orgs, sign out, "Open the Zerops GUI") · Archive. Command palette (`CommandPalette.tsx`): teal-tinted scope chip 6px, group titles 10/700 uppercase, rows 40px radius 8; `add-project` hidden on Zerops; new commands "Show services", "Open <hostname>".

## Wireframes

Sign-in (container origin, `requires-auth` with a z3 descriptor):
```
┌────────────────────────────────────────────────────────────────────────────────────┐
│ (Z) Code                                                                            │
│                                                                                     │
│                      ┌───────────────────────────────────────┐                      │
│                      │  [Z]  ZEROPS CODE                     │  mono mark, 10px caps│
│                      │  Sign in with your Zerops account     │  16/500              │
│                      │  ┌─────────────────────────────────┐  │                      │
│                      │  │ email                           │  │  white, 6px radius   │
│                      │  └─────────────────────────────────┘  │                      │
│                      │  ┌─────────────────────────────────┐  │                      │
│                      │  │ password                        │  │                      │
│                      │  └─────────────────────────────────┘  │                      │
│                      │  (          Sign in                )  │  #0077cc pill        │
│                      │  ( GitHub )         ( GitLab )        │  hand-off pills      │
│                      │  New to Zerops?  Create account       │  → app.zerops.io     │
│                      └───────────────────────────────────────┘                      │
│                       Connect another device with a one-time link                   │
└────────────────────────────────────────────────────────────────────────────────────┘
```

Picker (`/zerops`):
```
┌────────────────────────────────────────────────────────────────────────────────────┐
│ (Z) Code   Projects                                       ( ⟳ Refresh ) ( + New )   │
│                                                                                     │
│  CONNECTED                                                                          │
│  ┌───────────────────────────────────────────────────────────────────────────────┐  │
│  │ ● ACTIVE   kanban       zcp:8080 · Zerops Control Plane v1        ( Open )   │  │
│  └───────────────────────────────────────────────────────────────────────────────┘  │
│  READY TO CONNECT                                                                   │
│  ┌───────────────────────────────────────────────────────────────────────────────┐  │
│  │ ● ACTIVE   shop         zcp:8080 · older container      ( Enable Zerops Code )│  │
│  └───────────────────────────────────────────────────────────────────────────────┘  │
│  ┌───────────────────────────────────────────────────────────────────────────────┐  │
│  │ ◌ RESTARTING  blog      zcp:8080 · 12 s                                       │  │
│  └───────────────────────────────────────────────────────────────────────────────┘  │
│  PREPARING                                                                          │
│  ┌───────────────────────────────────────────────────────────────────────────────┐  │
│  │ ◌ CREATING  new-shop    waiting for the container · 0:41 / 5:00               │  │
│  └───────────────────────────────────────────────────────────────────────────────┘  │
│  NOT AVAILABLE                                                                      │
│  ┌───────────────────────────────────────────────────────────────────────────────┐  │
│  │ ● STOPPED  legacy       project stopped                                       │  │
│  └───────────────────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────────────────┘
```

Thread view with the Services panel open (≥981px):
```
┌──────────────┬────────────────────────────────────────────────────┬──────────────────┐
│ (Z) Code     │ ⬡ kanban / Build the kanban  ● DEVELOPING KANBANDEV │ Services   ⤢ ▭ ▯ │
│ (⌕ Search ⌘K)│                                                    │                  │
│              │                    ┌──────────────────────────────┐│ RUNTIMES         │
│ ⬡ kanban  ●  │                    │ Vytvoř mi kanban             ││ ┌──────────────┐ │
│ ┌──────────┐ │                    └──────────────────────────────┘│ │● ACTIVE      │ │
│ │Build the │ │ I'll propose three services first.                 │ │kanbandev:3000│ │
│ │kanban    │ │ ┌ PLAN ──────────────────────────────────────────┐ │ │Node.js 22    │ │
│ │● DEVELOP…│ │ │ ○ kanbandev    nodejs@22      dev              │ │ │[Domain public]│ │
│ └──────────┘ │ │ ○ kanbanstage  nodejs@22      stage            │ │ │[Mounted]     │ │
│  Add auth    │ │ ○ db           postgresql@16                   │ │ └──────────────┘ │
│  ● WAITING…  │ │ ( Confirm )  ( Change )                        │ │ ┌──────────────┐ │
│              │ └────────────────────────────────────────────────┘ │ │◌ CREATING    │ │
│              │ Used 4 tools ▸                                     │ │kanbanstage   │ │
│              │ ┌ DEPLOY KANBANDEV ──────────────────────────────┐ │ └──────────────┘ │
│              │ │ ✓ build 41 s   ✓ deploy   ✓ subdomain          │ │ DATA             │
│              │ │ [◎ kanbandev-24cb.prg1.zerops.app ↗]           │ │ ┌──────────────┐ │
│              │ │ ✓ HTTP 200                                     │ │ │● ACTIVE  db  │ │
│              │ └────────────────────────────────────────────────┘ │ │PostgreSQL 16 │ │
│              │                                                    │ └──────────────┘ │
│              │ ( Deploy ) ( Show logs ) ( Add Redis )             │ INFRASTRUCTURE   │
│              │ ┌──────────────────────────────────────────────┐   │ ┌──────────────┐ │
│              │ │ Ask, or / for commands                       │   │ │● ACTIVE      │ │
│              │ │                                              │   │ │zcp:8080      │ │
│              │ │ ✱ Claude Code ▾   High ▾   Full access ▾  (↑)│   │ │(Terminal)    │ │
│              │ └──────────────────────────────────────────────┘   │ │(Cloud IDE)   │ │
│ ⚙  ☁  ▥      │                                                    │ │✱ Claude Code │ │
│              │                                                    │ │  ● AUTHORIZED│ │
└──────────────┴────────────────────────────────────────────────────┴──────────────────┘
```

Question card and credential card in the timeline:
```
┌ ● WAITING FOR YOU · How should deploys be delivered? ─────────────────────────┐
│ (1) ┌ Auto ─────────────────────────────────────────────────────────────────┐ │
│     │ commit and push after every verified deploy                           │ │ 2px #0077cc
│     └───────────────────────────────────────────────────────────────────────┘ │
│ (2)   Manual — I push myself                                                  │
│ (3)   Other…  [                                                      ]        │
│                                                              ( Answer )       │
└───────────────────────────────────────────────────────────────────────────────┘
┌ ⚿ GIT_TOKEN · kanbandev ──────────────────────────────────────────────────────┐ mint
│ A GitHub token with repo scope. Stored as a service secret; the agent never    │
│ sees it.                                                                      │
│ [ ●●●●●●●●●●●●●●●●●●●●●●●●●●●●●● ]                     ( Save to kanbandev )  │
│ ✓ Saved · kanbandev · GIT_TOKEN                                               │
└───────────────────────────────────────────────────────────────────────────────┘
```

## Desktop
Desktop is the same bundle in Electron (brief D6; S5 "one smoke build, a written cost note, not a product"). Under Angle A it adds nothing visual of its own — desktop has no colour tokens (`DesktopAppBranding` = names only, `packages/contracts/src/ipc.ts:175-185`; R5 §1) — and changes four things:

- **Titlebar / WCO.** `index.css:124-137` `.wco` overrides `env(titlebar-area-*)` and `@custom-variant wco` @6 stay; the sidebar brand pill sits under the traffic lights at `--workspace-titlebar-content-left` (`workspaceTitlebar.ts`). Window chrome colours in `apps/desktop/src/window/DesktopWindow.ts:34-35,128-130` (`#0a0a0a/#ffffff` background, `#1f2937/#f8fafc` titlebar symbols) become canvas `#eceff3/#0c0f0e` and text `#1a1a1a/#e9eeec`, generated from ZEROPS_THEME (R5 §4 file 4); `preview/Manager.ts:2818` `#111111` → canvas. The first paint before React uses the same boot palette as web (`index.html` `SPLASH_COLORS`).
- **Icons.** A Zerops `.icon` Icon Composer project per brand (`assets/{dev,nightly,prod}/app-icon.icon`, exported by `scripts/lib/brand-assets.ts:1-32`, `pnpm icons:export` / `icons:check`) or the check fails; needs macOS tooling (R2 §8b, R5 §1). Windows `.ico`, macOS `.icns`, favicons all derive from it.
- **Naming.** `apps/desktop/src/app/DesktopEnvironment.ts:88-110` `APP_BASE_NAME` → "Zerops Code", stage label kept (Dev/Alpha/Nightly/Latest pill pattern survives per R6 §3.2); toast "Open T3 Code in the desktop app" (`routes/_chat.tsx:130-134`) swept. Deep-link origins `t3code://app`, `t3code-dev://app` stay on the z3 allowlist — the scheme rename is out of scope (D6 notes; spec §2.7).
- **Remote-only build (S5).** Local spawn, SSH launch and Tailscale removed from the Zerops build; keychain, deep link, updater and the preview webview kept. The "Browser" right-panel kind — "Only available in the desktop app" on web (`RightPanelTabs.tsx`) — gains a Zerops default: the first service with a subdomain URL, a read-only surface allowed by brief §4 rule 3. Nothing else on desktop is Zerops-aware; the map, strip and cards are the web ones.

Honest seam: the smoke build will still show "T3 Code" in the macOS About panel and the Electron menu until the packager metadata (`apps/desktop` build config) is swept — a mechanical item outside the web bundle, listed in migration phase 3.

## Mobile
## What the phone is for
Watching and steering, not building infrastructure: see which thread needs you (`● WAITING FOR YOU`), answer a question card, approve, read a deploy card and open the URL, glance at the service map, and — via the Live Activity T3 already ships (`widgets/AgentActivity.tsx`, R3 §5) — see the strip phrase on the lock screen. Sign-in and the picker exist so a phone reaches a thread with no code typed (S5 acceptance); registration hands off to app.zerops.io (Turnstile is mandatory, Z3C-3; a WebView is S5's call). New project stays on web.

## Screens (native-stack, flat thread routes kept — `src/Stack.tsx:229-233`)
1. **Sign in** — replaces the Clerk `SettingsAuth` slot: mark, email/password, TOTP card; register → hand-off. Lives in `LocalSettingsRouteScreen` and `ConfiguredSettingsRouteScreen` alike (R3 §6 POC seam map).
2. **Projects** — the picker as a formSheet (detents `[0.7, 0.92]`), the same four groups; "Enable Zerops Code" → `◌ RESTARTING`.
3. **Home** (`HomeScreen.tsx`) — threads grouped by Zerops project; row second line = strip label as `StatusDot`; iOS Mail-style search toolbar and native menus untouched; Android `AndroidHomeFab`.
4. **Thread** (`ThreadDetailScreen.tsx`) — the strip as a glass pill pinned under the native header (an `LiquidGlassView`/`GlassSurface` chip, tap → map sheet); cards inside `ThreadFeed` next to `PendingApprovalCard`/`PendingUserInputCard`.
5. **Services** — a new formSheet (detents `[0.55, 0.92]`), the map grouped RUNTIMES/DATA/INFRASTRUCTURE with the mint zcp panel; `Terminal` → `ThreadTerminal` route; `Cloud IDE` → external browser.
6. **Settings** — Zerops (account, sign out), **Devices** (was Environments/Connections), Appearance (Zerops = Default card, orbs from `themePreview.ts`).
7. Kept as is: ThreadTerminal, ThreadReview (diff grouped by service comes with S3), ThreadFiles (tree rooted at `/var/www/<host>`), Git sheets hidden on Zerops (D4), Usage, Archive.

## How the DNA translates without fighting native (R3 §4)
- **Canvas and cards:** `--color-screen #eceff3 / #0c0f0e`, cards white `#ffffff / #141918`, radius 24 → **16** on `SettingsSection`, `ConnectionsRouteScreen` cards, `EmptyState`, `ThemeCard` (R3 tier C); no shadows (`ConnectionSheetButton` shadow literals removed). The `GlassSurface` composer keeps its 32px pill↔card morph — Liquid Glass is chrome, Zerops tone is content.
- **Primary:** `--color-primary #262626 / #f5f5f5` (`global.css:42,141`) → `#0077cc` with white text; `--color-user-bubble #007aff` (`global.css:95`) → `messageSurface` `#e3f4ff` with dark text (`readableMessageAccent` handles skill text, R5 risk 7). Switch tracks stay iOS green — a system control.
- **Type:** Roboto registered as native families; SF stays in native `UINavigationBar` titles (not overridable without custom header views) — an accepted mixed line on iOS.
- **Status:** `StatusDot` from `ConnectionStatusDot`; iOS-system tints in `threadPresentation.ts:30-123`, swipe actions `#ff2d55/#5856d6/#007aff`, PR tints → adaptive tokens.
- **Icons:** SF Symbols via `AppSymbol`; the glyph map above; `ZeropsMark` SVG replaces `T3Wordmark` in `CompactBrandTitle.tsx`, `HomeHeader.tsx:219`, `BrandMark.tsx`, `LoadingScreen`.
- **Untouched:** header morphing, form-sheet detents/grabber, `UIMenu`/`AndroidAnchoredMenu`, keyboard stack, haptics, predictive back, native composer caret (`T3ComposerEditorView.swift:763` stays `systemBlue` unless edited), Live Activity SwiftUI semantic colours (only the `T3Mark.svg` asset and "T3 Code" title change).
- **Android:** in-flow `AndroidScreenHeader` takes the tokens automatically; `withAndroidModernPopupMenu.cjs:67-70` and `withAndroidModernAlertDialog.cjs:33-46` need their hand-copied palette updated (R3 §2).

## Wireframes (≤40 cols)

Home:
```
┌────────────────────────────────────┐
│ (Z) Code               ⌕      ⋯    │
│                                    │
│ ⬡ KANBAN                 ● ACTIVE  │
│ ┌────────────────────────────────┐ │
│ │ Build the kanban          5m   │ │
│ │ ● DEVELOPING KANBANDEV    ✱    │ │
│ └────────────────────────────────┘ │
│ ┌────────────────────────────────┐ │
│ │ Add auth                  1h   │ │
│ │ ● WAITING FOR YOU         ✱    │ │
│ └────────────────────────────────┘ │
│                                    │
│ ⬡ SHOP                   ● ACTIVE  │
│ ┌────────────────────────────────┐ │
│ │ Checkout flow             2d   │ │
│ │ ● TASK COMPLETE           ◇    │ │
│ └────────────────────────────────┘ │
│                                    │
│                            ( + )   │
│  ⌕ Search                          │
└────────────────────────────────────┘
```

Thread:
```
┌────────────────────────────────────┐
│ ‹ kanban   Build the kanban    ⋯   │
│ (● DEVELOPING KANBANDEV · services)│
│                                    │
│          ┌───────────────────────┐ │
│          │ Vytvoř mi kanban      │ │
│          └───────────────────────┘ │
│ I'll propose three services first. │
│ ┌ PLAN ──────────────────────────┐ │
│ │ ○ kanbandev    nodejs@22       │ │
│ │ ○ kanbanstage  nodejs@22       │ │
│ │ ○ db           postgresql@16   │ │
│ │ ( Confirm )     ( Change )     │ │
│ └────────────────────────────────┘ │
│ ┌ DEPLOY KANBANDEV ──────────────┐ │
│ │ ✓ build ✓ deploy ✓ HTTP 200    │ │
│ │ [◎ kanbandev-24cb…zerops.app ↗]│ │
│ └────────────────────────────────┘ │
│                                    │
│ ┌────────────────────────────────┐ │
│ │ Ask…                     (↑)   │ │
│ └────────────────────────────────┘ │
└────────────────────────────────────┘
```

Services sheet (map + strip):
```
┌────────────────────────────────────┐
│               ──                   │
│ Services       ● DEVELOPING        │
│                                    │
│ RUNTIMES                           │
│ ┌────────────────────────────────┐ │
│ │ ● ACTIVE  kanbandev:3000    ↗  │ │
│ │ Node.js 22 · mounted           │ │
│ └────────────────────────────────┘ │
│ ┌────────────────────────────────┐ │
│ │ ◌ CREATING  kanbanstage        │ │
│ └────────────────────────────────┘ │
│ DATA                               │
│ ┌────────────────────────────────┐ │
│ │ ● ACTIVE  db · PostgreSQL 16   │ │
│ └────────────────────────────────┘ │
│ INFRASTRUCTURE                     │
│ ┌────────────────────────────────┐ │
│ │ ● ACTIVE  zcp:8080             │ │
│ │ ( Terminal )  ( Cloud IDE )    │ │
│ │ ✱ Claude Code   ● AUTHORIZED   │ │
│ └────────────────────────────────┘ │
│ live updates paused · a few s late │
└────────────────────────────────────┘
```

## Tablet
`AdaptiveWorkspaceLayout` split view stays: `ThreadNavigationSidebar` (left, thread list with the same rows) + thread detail. The map goes where files already go on iPad — an inspector pane beside `thread-file-navigator-pane.tsx` / `workspace-inspector-pane.tsx` (R3 §1) — so on a 12.9" iPad the layout reads like the web three-column thread view. `WorkspaceEmptyDetail` becomes the quick-actions empty state.

## Honest gaps
Mobile has zero Zerops code on `main` (R3 §1): every Zerops screen above is S5/S6 work; the skin only prescribes how they look and which native containers they use. The pure-TS web modules (`apps/web/src/zerops/{candidates,provisioning,containerHealth,firstPrompt,serviceMap,strip,cards,quickActions}.ts`) must be promoted to `packages/client-runtime` first (R3 §6). Roboto on iOS reads slightly non-native; the brief's "looks like Zerops" wins.

## Cross-client system
## One source
`packages/shared/src/themePalettes.ts` gains `ZEROPS_THEME: ThemeDefinition` (light + `variants.dark`, `sidebarArtwork:false`), its id in `BUILT_IN_THEME_IDS`, `MOBILE_THEME_IDS`, `BUILT_IN_THEMES`, and `MOBILE_DEFAULT_THEME_ID = "zerops"` (`themePalettes.ts:1-12, 741-747`). It drives:
- **web** — `applyThemePalette` (`themePalette.ts:1854-1877`) → `--app-theme-*` → `index.css:1552-1617` semantic remap; default selection in `useTheme.ts:39-45` and the `index.html` boot script.
- **mobile** — `createMobileThemeVariables` + `generate-uniwind-themes.mts` emit `@variant zerops-light/dark` the moment the id is registered (R3 §3); `mobileThemeRuntime.ts:41-46` resolves the default name.
- **desktop** — window colours (`DesktopWindow.ts`) and the boot/splash palette, generated (R5 §4 option a).

Step 2 (after the default is stable): the root generator `scripts/generate-theme-tokens.ts` emits `apps/web/src/generated-theme.css`, the mobile `global.css` default block, the `index.html` boot snippet, and `packages/shared/src/generated/brandColors.ts` for `themePreview.ts` and `DesktopWindow.ts`; the six hand-synced copies (R5 §1) are deleted. Until then the copies are updated by hand and pinned by tests.

## Shared vs per-client
| Shared (`packages/shared`, `client-runtime`) | Per client |
|---|---|
| Colour roles (`ZEROPS_THEME`), preview orbs (`themePreview.ts`), stage label pattern | Shapes: Tailwind classes on `ui/*` (web) vs uniwind classes + native containers (mobile) |
| Strip phrases (`strip.ts` promoted to `client-runtime`, S5 needs it anyway) and the `tone → status role` table for `StatusDot` | Font registration (woff2 vs expo-font), icon families (lucide vs SF/Tabler), glyph map table per client |
| Vocabulary glossary (a TS constants module: Projects, Devices, Coding agents, Services, Cloud IDE) consumed by both settings trees and the palette | Zerops-surface tints (`[data-zerops-surface]` vars on web; `ADAPTIVE_COLORS` on mobile) |
| Card payload decoders (`zerops/cards/*` promoted) | Card rendering (React DOM vs RN `ThreadFeed` items) |

## Drift guards
- Existing: `generate-uniwind-themes.test.ts` byte-equality of committed outputs (R3 §3); `themeBoot.test.ts`, `themePalette.test.ts` pin boot values (R2 §8); `branding.test.ts`, `clientMetadata.test.ts`, `SidebarStageBackdrop.test.tsx` pin names.
- Added: (1) role coverage — `Object.keys(ZEROPS_THEME.colors) ⊇ THEME_COLOR_ROLES` for both halves; (2) contrast assertions with `themeContrastRatio` (`themePalette.ts:957`): ≥4.5 for text/canvas, text/surface, messageActionForeground/messageAction, accentSurfaceForeground/accentSurface, error/warning/update foreground over their surfaces, ≥3.0 focus/canvas, every value opaque (R5 §4); (3) a copy lint over user-facing strings — no "T3 Code", "T3 Connect", "Tailscale", "pairing", "worktree", "Local checkout", "environment" in `.tsx` literals outside identifiers, allowlist file for the pairing fallback — the zcp pattern of `TestNoBareZeropsURIInAgentContent`; (4) no Tailwind palette utilities (`text-blue-*`, `bg-emerald-*`…) under `apps/web/src/components/zerops/**` and `apps/mobile/src/features/zerops/**`; (5) `icons:check` covers the Zerops `.icon` projects.

## Visual regression harness
Neither R2 nor R3 cites a pixel-diff CI in the fork. Proposal: web — Playwright against a mock environment (the `test-t3-app` fixture AGENTS.md names) capturing sign-in, picker, thread+Services panel, question/credential cards at 1440 and 390 px, light and dark, Zerops theme and one non-Zerops theme, with a 0.5 % pixel tolerance; mobile — the existing showcase harness (`scripts/mobile-showcase.ts`, captures Home/Thread/Terminal/Review/SettingsEnvironments per theme id, R3 §1) extended with the `zerops` id and the Services sheet, PNGs compared in CI. Both run on the skin slices only, not repo-wide (AGENTS.md "Do not run repo-wide checks").

## Migration
Effort classes: **S** ≤ 1 day one slice · **M** 2–4 days · **L** > 1 week. Ordering rule: tokens and primitives first (touch no `zerops/**` files, so S4/S6 keep merging cleanly); Zerops-surface polish last, as the closing S6 slice.

### Phase 0 — ZEROPS as a selectable theme (S, no throwaway)
`packages/shared/src/themePalettes.ts` (+`ZEROPS_THEME`, ids), `apps/web/src/themePalette.ts:59-73` reserved ids/labels, `apps/web/index.html` boot palette copy, `settings/ThemePreviewCircles.tsx`, `index.css @layer components` stage pigments (none — `sidebarArtwork:false`), mobile `pnpm generate` + the name-list assertion in `generate-uniwind-themes.test.ts`. Sources: R2 §3 "Built-in registration", R3 §3 "Adding a zerops theme", R5 §4 step 1. Independent of every stream.

### Phase 1 — default look + fonts (M)
Web: `useTheme.ts:39-45` `DEFAULT_THEME_SNAPSHOT`, `readThemePreference` fallback, `index.html` `isKnownTheme ? storedTheme : "system"` and `SPLASH_COLORS`, `getDefaultThemeColors` (`themePalette.ts:1224`) and `DEFAULT_THEME_PALETTES` → ZEROPS (R5 risk 5), `ThemeSettings.tsx:664-686` default card, `manifest.webmanifest` + `theme-color`, self-hosted Roboto in `apps/web/public` + `index.css:139-144` + `appearanceFonts.ts:20-26`, `--success/--info` literals, provider fallback swatch (`ProviderAccentColorPicker.tsx:12-21`). Mobile: `MOBILE_DEFAULT_THEME_ID`, `mobileThemeRuntime.ts:41-46`, `global.css` default block values (until the generator), `app.config.ts` fonts + splash/adaptive colours, `withAndroidModern{PopupMenu,AlertDialog}.cjs`, `clerk-theme.json` while Clerk lives. Tests: `themeBoot.test.ts`, `themePalette.test.ts`, the new contrast/coverage test. Sources: R2 §8(a), R3 §8 tier A, R5 §2–4.

### Phase 2 — primitives + status vocabulary (M)
`ui/button.tsx` (pill variants), `badge.tsx`, `card.tsx`, `input.tsx`, `select.tsx`, `toggle.tsx`, `menu.tsx`, `popover.tsx`, `tooltip.tsx`, `dialog-styles.ts`, `sheet.tsx`, `sidebar.tsx:799-830`, `toast.tsx`, `alert.tsx`, `empty.tsx`, `kbd.tsx`, `switch.tsx`, `input-group.tsx`; header control overrides `index.css:1746-1785`; new `ui/status-dot.tsx`; snapshot tests `button.test.tsx`, `menu.test.tsx`, `sidebar.test.tsx`, `command.test.tsx` (R2 §8(c)). Replace the palette literals that sit on the Zerops path: `ThreadStatusIndicators.tsx:83-131,427`, `Sidebar.tsx:1293,3857-3866`, `ChatMarkdown.tsx:310-345` alerts, `CommandPaletteResults.tsx:74`; leave `ResourceTelemetryDiagnostics` (38) and `pullRequestPresentation` (21) — not on the Zerops path. Mobile: `StatusDot` from `ConnectionStatusDot`, radii 24→16 on the four components, `threadPresentation.ts`, swipe/PR tints → adaptive tokens (R3 §8 tier C). Sidebar chrome: brand pill + ⌘K pill (`SidebarChrome.tsx:80-118`, the search row in `Sidebar.tsx`).

### Phase 3 — brand + vocabulary (M, mechanical + asset production)
Web: `branding.ts:20-27`, `index.html:399-466` + `SplashScreen.tsx`, `public/*` favicons via `assets/{dev,nightly,prod}/app-icon.icon` + `scripts/lib/brand-assets.ts` (macOS Icon Composer), `AuthSurfaceShell.tsx:17-32` gradient → canvas, `PairingRouteSurface.tsx` copy (interim, replaced by phase 4), `_chat.tsx:130-134`, `_chat.index.tsx:185`, `ThemeSettings.tsx:852`, `settingsSearch.ts:28-38` labels (Devices, Coding agents, Projects), `RightPanelTabs.tsx:268-345` "Services", `clientMetadata.ts:80-94`; tests `branding.test.ts`, `clientMetadata.test.ts`, `SidebarStageBackdrop.test.tsx`. Desktop: `DesktopEnvironment.ts:88-110`, `DesktopWindow.ts`, packager metadata. Mobile: `app.config.ts:65-90` names (bundle ids, scheme, storage keys stay — D6, R3 §5 "rename = migration"), `BrandMark`/`T3Wordmark`/`CompactBrandTitle` → `ZeropsMark`, `assets/widget/T3Mark.svg` + `AgentActivity.tsx:232-236`, `remoteRegistration.ts:505-506`, `authClientMetadata.ts:10`, `mobileBranding.ts`, 26 string files. Add the copy-lint test. Sources: R2 §4, R3 §5, R6 §3.

### Phase 4 — Zerops surfaces in Zerops idiom (M; S4/S6-coupled, last)
Landing shell, picker rows, provisioning steps, strip pill (`ZeropsLifecycleStrip.tsx:47-59`), map cards + mint zcp panel (`ZeropsServiceMap.tsx`), `ZeropsToolCard` `CardFrame` tints + process steps, quick-action pills, question-card outline selection, credential card (new, with S6's zcp verb), `DraftHeroHeadline.tsx:158-170` copy, `Sidebar.logic.ts` project label on identity-door environments, `rightPanelStore` default-open (optional), palette intents. Prerequisite from S4: the container-bundle `requires-auth` branch (`__root.tsx:62-83`, `hostedPairing.ts:35-46`, `-chatIndexView.ts`). Sources: R2 §5, spec §4–5.

### Phase 5 — mobile Zerops screens (L; = S5 + S6 mobile, not skin)
Promote `apps/web/src/zerops/*.ts` to `client-runtime`; sign-in/TOTP, picker sheet, provisioning, Services sheet, strip chip, cards in `ThreadFeed`, Devices, first-prompt compose (R3 §6, §8 tier D). The skin contributes the component specs above.

### Generator (M, after phase 1 settles)
`scripts/generate-theme-tokens.ts` + `generate:theme:check` in CI; delete `index.css:1388-1494` literals, `T3_CODE_*_THEME_COLORS`, `STANDARD_THEME_PREVIEW_COLORS`, the mobile hand block, the T3 Chat boot fallback (R5 §4 steps 2–3).

### What is throwaway
None of phases 0–3. In phase 4: the `PairingRouteSurface` copy patch (dies with the S4 gate branch); the `DraftHeroHeadline`/`Sidebar.logic.ts` label patches and the right-panel container of the map (a structural angle later promotes the map to a page — the row card survives, the panel wrapper does not); the `[data-zerops-surface]` tint vars become theme roles if a later angle grows `THEME_COLOR_ROLES`.

## Risks
1. **It reads as "T3 with a coat".** The hero composer and the folder-shaped project row are the strongest T3 tells and Angle A keeps both. Mitigation: concentrate the Zerops DNA at the four glance points — brand pill + ⌘K pill in the sidebar header, `● DOT LABEL` in the topbar strip and thread rows, the mint zcp panel in the map, tinted process-step cards in the timeline — and open the Services panel by default on identity-door environments. Accepted residual: a T3 user recognises the bones; a Zerops user recognises the surfaces.

2. **The `www` project row and "environment" vocabulary leak.** D3 makes the T3 project invisible in theory; the sidebar still groups by it (`Sidebar.tsx`, 3,960 lines, virtualised + DnD — R2 §8(d) rates real IA edits High). Mitigation: label override from environment metadata in `Sidebar.logic.ts`, hide the scope menu and `add-project` intent on identity-door environments, copy lint for "environment". Residual: the folder icon and "All projects" affordance exist in code paths a non-Zerops environment still needs.

3. **Single-bundle gate.** Without S4's `requires-auth` branch the container bundle keeps showing a token form on first open (`__root.tsx:62-83`); no skin fixes that. Mitigation: sequence phase 4 behind the gate; interim copy patch so the form at least says "Connect with a one-time link", never "Pair".

4. **Theme mechanics.** Opaque-only theme colours vs alpha-heavy Zerops (R5 risk 4): flatten per surface in the generator, record the surface each value was flattened on. T3 Chat pink fills omitted roles and the boot script (`getDefaultThemeColors` 1224, `index.html DEFAULT_THEME_PALETTES`) — both must point at ZEROPS or a partial theme flashes pink (R5 risk 5). Contrast slider math derives `--sidebar-icon-color` at ≈2.7:1 with the naïve muted value — use `#4f5a62` (R5 risk 1).

5. **Contrast and CVD.** Teal is 2.03:1 on white — never text, never a white-label pill in light; `#0077cc` primary passes both ways; `#58a6ff` needs dark text; `#cc0011` on the dark card is 3.03 → `#e57373` (R5 §3). Zerops statuses are hue-only Material 400s; every dot carries its label and the strip never relies on colour alone.

6. **Performance.** A webfont is the only new cost: self-host, preload, subset, `font-display: swap`; keep lucide/SF, no Material Icons font (R5 risk 9). The GUI's infinite pulse/ripple/bounce/shimmer are replaced by the duty-cycled `status-pulse` keyframes or removed (AGENTS.md Taste, R1 §7). Glass stays within the existing `surface-glass`/`dropdown-glass` utilities.

7. **Native tension on mobile.** Pills and flat cards next to iOS 26 Liquid Glass and native headers; Roboto next to SF in the navigation bar; a blue-tinted user bubble where iOS users expect iMessage blue. Mitigation: glass = chrome, Zerops tone = content; radii 16 not 8 on sheets; keep system controls (switch, caret) system-coloured; the showcase harness screenshots catch a wrong-looking screen before TestFlight.

8. **Stream collision.** S4 and S6 edit `ZeropsProjectPicker`, `ZeropsServiceMap`, `ZeropsLifecycleStrip`, `ZeropsToolCard` right now; a skin slice landing mid-stream produces merge churn and double work. Mitigation: phases 0–3 touch no `zerops/**` file; phase 4 is the last S6 slice and uses only primitives that already changed in phase 2, so the diff is class names, not structure.

## Signature moments
- The sidebar header: the Zerops mark on a teal-tinted pill with "Code" beside it and the ⌘K search pill below — the first two things a GUI user sees on the dashboard, now the first two things in Zerops Code.
- The strip beside the thread title reads exactly like a GUI status — `● DEVELOPING KANBANDEV` with a blue pulsing dot, `● WAITING FOR YOU` in orange, `● TASK COMPLETE` in green — the same dot-plus-tiny-uppercase-word the GUI uses for `● ACTIVE`.
- The Services panel: flat #f3f5f7 cards with `kanbandev:3000` at 20/500 and the port at .6 opacity, `Domain public` and `Mounted` chips, and the zcp row as the mint #e8f7ec panel with white `Terminal` / `Cloud IDE` pills and `✱ Claude Code · ● AUTHORIZED` with the haloed green dot — the GUI's ZCP card inside the agent app.
- A deploy in the timeline: process-step circles turning from blue `play` to green `check`, then a white info-chip with the green globe and the subdomain URL, then `✓ HTTP 200` — the GUI's pipeline glyphs narrating the agent's work.
- The credential card: a mint panel with a key icon, one masked field and `Save to kanbandev`, resolving to `✓ Saved · kanbandev · GIT_TOKEN` — the token visibly bypasses the conversation.
- `Enable Zerops Code` on an older container: the picker row becomes `◌ RESTARTING · 12 s` with a pulsing blue dot for the 502 window, then `● READY · Open` — the ~19 s restart shown as a status, not an error.
