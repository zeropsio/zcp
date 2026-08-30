# Controls & Content — the project pane

## Thesis
Rebuild the z3 shell as the Zerops GUI's own two-pane page: a left **controls pane** of stacked flat cards that describe the project (backlink + status dot + name, threads, the service map grouped Runtimes/Data/Infrastructure, quick actions, and the mint "Zerops Control Plane" card with its Terminal/Cloud IDE pills and the `Claude Code · AUTHORIZED` tray), and a right **content pane** that holds the conversation instead of service cards. This is the right shape for Zerops Code because the product model already says so: one z3 environment is one Zerops project (brief D3), services are rows not projects, and the thing a Zerops user knows by heart is app.zerops.io's project detail — org pill + ⌘K pill top-left, a 480px column of cards with the zcp panel in it, the work on the right (`libs/zui/src/content/content.component.scss:14`, screenshot `05-project-detail-full.png`). T3's thread list exists because T3's unit is a folder; on Zerops the left column should show what exists and where the agent is, with threads as one card among the project's cards. The cost is real and is stated: T3's `Sidebar.tsx` (3960 lines, virtualised, dnd-kit pinned reorder, snooze shelves) leaves the product path and a new pane is written against `Sidebar.logic.ts`'s pure resolvers; the GUI's 480px column and its infinite pulse/ripple do not survive — the continuity is of grammar (tint steps, micro-labels, dot+label, pills, blue action / teal identity), not of pixels.

## Information architecture
## Top-level objects
Account (orgs) → **Project** (= the z3 environment; the word "environment" never reaches copy; `$environmentId` stays in route keys per D6) → **Thread** (= one agent PID = one zcp work session). **Services** are rows of the project's map, never navigation nodes. **Devices** hang off the project (clients connected to its container, labelled `Zerops Code · <browser> on <os>`, spec-z3 §4.8). The T3 "project" (`/var/www`) is invisible (D3).

## Shell (web ≥ 1280)
Two panes, the GUI's grammar (`R1 §1`): a floating app bar at the top of the left pane — **org/project pill** (mark + name + chevron; opens the org/project switcher, the GUI's 640px menu pattern) and a **⌘K pill**; below it the **controls pane** (default 24rem, resizable 20–30rem via the existing `Sidebar resizable` options, `apps/web/src/components/AppSidebarLayout.tsx:214-224`, `threadSidebarWidth.ts`); to the right the **content pane** (the thread, `ChatView`). The pane is `collapsible="offcanvas"` as today, toggled by the existing fixed `SidebarControl` and by the `Z` shortcut that today opens the right-panel "Zerops" surface (`RightPanelTabs.tsx:268-345`). The GUI's 480px is not copied: 480 + the composer's `max-w-3xl` + a right panel does not fit 1440, and the pane holds dense rows, not prose cards.

**Controls pane, card order (top → bottom), each a flat `#f3f5f7` card radius 10, no shadow:**
1. **Project header** — `‹ Projects` backlink (13px), status dot + `ACTIVE` micro-label, project name 20px/500, chips `FULL ACCESS` / `EU CENTRAL (PRG1)`, right-aligned globe (open the project's public URL list) and `⋮` (Open in Zerops, Sign out). Sticky with the GUI's white slab-on-scroll effect done with `position: sticky` and a background, not a transform loop (`project-detail.page.scss:63-113` is the source; the animation is not).
2. **Threads** — this project's threads: pinned, live, settled, snoozed, in that order, virtualised rows (status dot / pill from `Sidebar.logic.ts::resolveThreadStatusPill`, `:653`), `+ New` white pill, `All threads (N) ›` expands the card to fill the pane (the pane's "focus mode": one card takes over, which is how T3's dedicated sidebar survives inside the new shell). Reverse states kept: pin/unpin, snooze/unsnooze, archive → Settings › Archive (AGENTS.md "Reverse states").
3. **Services** — the map. Header `Services · 3 · live|polling` (the liveness tri-state from `zerops/serviceMap.ts` `liveness`); groups `RUNTIMES / DATA / INFRASTRUCTURE` as 10px tracked kickers (order pinned in `serviceMap.ts:20-24`, rule Z3F-2). Row: dot + hostname 14px/500 + type label (`nodejs@22`, OS prefix stripped, `zeropsTypeLabel`), right side: `[mnt]` info-chip when `mounted`, `Open ›` when `subdomainUrl`; the stage half folded under its dev row (`devPartnerOf`); a `production: <name> ›` line when the envelope reports `feedsProduction`. Lifecycle marks per host from the **active thread's** envelope (`pending / deployed ✓ / verified ✓ / deploy failed`) sit on the rows — the map is project-wide, the marks are thread-scoped, and the card says so with a kicker `THREAD · Document Kanban API`. D4's visibility rule renders here: `thread Y also works on kanbandev` under a row two threads touch (not implemented today — `grep 'also works on' apps/web/src/zerops` → 0; needs a per-service thread index the lifecycle feed already contains per thread). Row click → a popover (GUI `sat-popover` grammar) with read-only/caller-bound actions only: Open URL, Files at `/var/www/<host>`, Terminal, "Show logs" (prefills the composer), Data Console (Data rows). Nothing in the pane mutates the platform (brief §4 rule 3; `ZeropsQuickActions.test.tsx` "cannot reach Zerops or the RPC layer at all" becomes the pane-wide assertion).
4. **Quick actions** — the white-pill 2-column grid from the mint panel's button grammar (`zagent-service-card.feature.html:93-133`), fed by `zerops/quickActions.ts`; each pill prefills the composer and stops.
5. **Zerops Control Plane** — the mint `#e8f7ec` card, header `● ACTIVE  zcp`, pills `Terminal` (z3's own terminal drawer, cwd `/var/www`), `Cloud IDE` (code-server on the same 8080 origin, opened as a right-panel surface or a new tab), `SSH ›` and `Desktop IDEs ›` link out to the GUI service page because both need VPN, which is out of scope (D6 "Out of scope: VPN anywhere"). The edge-to-edge **auth tray**: one row per coding agent, `Claude Code · AUTHORIZED` (green haloed dot) / `Codex · NOT AUTHORIZED` (orange), read from provider status; the `+` and the Authorize action arrive with S7 — until then the row's `⋮` offers `Authorize in Zerops ›`. **Devices** is the tray's last row (`this Chrome · +1 more`) opening Settings › Devices.

**Content pane** = the thread (see `web`). The lifecycle **strip** stays beside `ChatHeader` (spec §5.4 pins the mount; it needs pending-question state one level up) and is the *compact* reading of `zeropsStripState`; the Services card carries the *expanded* reading. One selector, two renderings — never two state machines.

## What happens to T3's three navigation devices
- **Sidebar** (`Sidebar.tsx`): leaves the Zerops product path. `AppSidebarLayout.tsx:227-233` already branches `isOnSettings ? SettingsSidebarNav : legacySidebarEnabled ? LegacyThreadSidebar : ThreadSidebar`; the pane is a fourth branch keyed on the environment being a Zerops one (descriptor advertises `zerops-identity`, spec §3.5). `ThreadSidebar` is kept, untouched, for a hand-paired non-Zerops server (spec §3 "Outside a Zerops project, nothing changes"). The `SidebarUtilityMenu` (Settings / Pull Requests / Zerops / Usage, `SidebarChrome.tsx:218-233`) collapses to a Settings gear in the project header's `⋮`; the `Zerops` item and the `/zerops` route go away (the picker is `/`).
- **Right panel** (`rightPanelStore.ts:17-27`): kinds become `diff | files | file | preview | terminal | agents | data-console`. `zerops` is deleted (the map lives in the pane); `pull-request` is hidden on Zerops environments (D4: commit/push/PR is zcp's pipeline, never two). `data-console` embeds the Managed Data Console (`docs/spec-dataconsole.md`, embed reach, caller-bound writes) for a selected Data row. The panel is what the GUI's detached code-server overlay is — a surface that covers exactly the content column (`R1 §9`) — so Cloud IDE opening there is continuity, not invention.
- **Command palette** (`CommandPalette.tsx`): kept, restyled to the GUI palette (scope chip = project name), groups Threads / Projects / Services / Quick actions / Surfaces / Settings. `new-thread-in` stays (scoped to the project); `add-project` (`:1257-1382`, folder browsing) is removed on Zerops — there is no folder to add — and `New project` (Zerops) takes its slot.

## The flow
`/` signed-out → **Sign in with Zerops** (email/password, TOTP per spec §4.1) → `/` signed-in → **Projects picker** (GUI dashboard cards; groups Connected / Ready to connect / Preparing / Not available from `zerops/candidates.ts`; `Enable Zerops Code` white pill on a `predates-z3|unreachable` candidate, restart ≈19 s rendered as a blue `RESTARTING` dot for ≤30 s, 502s swallowed, Z3C-5) → **New project** (name + org; the card appears in `PREPARING` with the waiter phrase `awaiting-project → awaiting-container → awaiting-health`, elapsed, `Stop waiting`; `pool-exhausted` is its own card state, not an error; `timed-out` offers `Keep waiting` / `Check it in Zerops ›`) → identity connect (silent re-mint every 900 s, spec §3.3 — the user never sees a logout) → **thread** with the onboarding prompt composed, not sent (Z3C-8), the strip reading `no services yet`. A **second device** signs in with the same Zerops account; `Connect another device` (Devices) offers the one-time link/QR for a device without a Zerops login — the word "pairing" appears nowhere.

## Empty states (each in the GUI's inline-empty-state idiom, mono heading at .6)
- No projects (fresh account): one `PREPARING` card, or the `New project` card alone.
- Project with no services: Services card shows `Nothing here yet — tell the agent what to build` + `Build something` pill (the single quick action `quickActions.ts:48-56`); strip `no services yet`.
- Thread before any `zerops_*` tool: no strip, rows carry no marks.
- `available:false` (no zcp binary): Services, Quick actions and Control Plane cards are absent; the pane shows Project header + Threads — indistinguishable from a non-Zerops server, by design (Z3F-1).
- `degraded:true`: rows keep last-good values under a one-line orange note; doorbell down: one quiet grey line `updating every few seconds` (§5.4).
- Feed over cap / undecodable card: the generic tool block (Z3F-7).

## Reverse states
Pane collapse/expand; card focus/unfocus; pin/unpin, snooze/unsnooze, archive/unarchive; fold/unfold the stage row; Stop waiting / Keep waiting; clear the composed first prompt; sign out; revoke a device; close a panel surface; `Enable Zerops Code` has no undo (a restart), so the pill states its cost (`restarts the container · ~20 s`).

## Phone (< 768px, `useIsMobile`)
The pane does not become a Sheet (`ui/sidebar.tsx:107-145` today). It becomes the **Project home** route: the same cards, same order, full-width, and the thread is a separate full-screen route with `‹ tst` as its back label — the web mirrors the native app's stack (see `mobile`). Between 768 and 1279 the pane is offcanvas, the thread is full width, and the header strip keeps the lifecycle visible.

## Visual system
## Tokens (light / dark) — the `ZEROPS_THEME` `ThemeDefinition` (R5 option a), roles from `packages/shared/src/themePalettes.ts:18-77`
| Role → semantic var | Light | Dark | Source |
|---|---|---|---|
| canvas → `--background`, sidebar | `#eceff3` | `#0c0f0e` | `apps/zerops/src/styles/base/_theme.scss:60,374` |
| surface → `--card` (pane cards, Zerops cards) | `#f3f5f7` | `#141918` | `_material.scss:22-35`, `_dark-theme.scss:4-8` |
| surfaceRaised (composer shell, white tiles, pills) | `#ffffff` | `#1b2220` | `--z-service-stack-tile-bg` |
| surfaceOverlay → `--popover` | `#ffffff` | `#1e2624` | dark menus/selects |
| text / textMuted / secondaryLabel | `#1a1a1a` / `#5f6a72` / `#7d8891` | `#e9eeec` / `#9faea9` / `#7d8c88` | R5 rows 12-24 |
| border / input | `#e0e0e0` / `#d4dfea` | `#262f2d` / `#2a3331` | `--z-form-border` |
| messageAction → `--primary` (send, CTAs, switches) | `#0077cc` (hover `#005fa3`) | `#0077cc` (hover `#008ff5`) | `--zcp-wizard-cta-bg`; 4.66:1 on white |
| focus → `--ring` | `#0077cc` | `#5ab3ff` | `--zcp-wizard-focus-ring` |
| accentSurface → `--accent` (hover/selected rows) | `#f0f3f5` | `#232b29` | |
| messageSurface (user bubble) | `#e3f4ff` | `#1e2e3b` | `--color-zerops-core-blue` flattened |
| update / updateSurface / updateForeground (brand-positive) | `#02b1a3` / `#def8f6` / `#007e72` | `#00e5c0` / `#113630` / `#58efd4` | mint fill + text-on-fill |
| error / errorSurface | `#cc0011` / `#fdefef` | `#e57373` / `#2f1717` | identityRed; dark deepened for AA |
| warning / warningSurface / warningForeground | `#ffa726` / `#fff4e0` / `#bb4d00` | `#ffa726` / `#3a301a` / `#ffb74d` | needs-you orange |
| codeBackground / terminalBackground | `#f2f5f7` / `#f2f5f7` | `#121716` / `#0c0f0e` | |
| sidebarRowHover / sidebarRowSelected | `#e3e6ea` / `#ffffff` | `#151b1a` / `#141918` | a selected thread is a card |

Deviation from R5 row 9 (surface = white): the pane needs the GUI's three-step tint (canvas → card → white tile) to read as Zerops, so `--card` is `#f3f5f7` and white is `surfaceRaised`. Teal is never text on light (`#00ccbb` on white 2.03:1; `#007e72` is the text-safe deepening, `z3/docs/internals/zerops/verified.md:20`). Non-role colours the pane needs — mint panel `#e8f7ec` / `≈#1a281e`, running tint `#eff9fd`, ready tint `#e8f7ec`, region purple `rgba(156,39,176,.15)/#7b1fa2`, access green `rgba(76,175,80,.15)/#388e3c`, status dot palette, authorized `#2e7d32`, unauthorized `#b26a00` — live in a `--zerops-*` block beside `--success/--info` (index.css `:1429-1432`, which the file already says stay theme-independent) and are emitted by the same generator.

## Type
Roboto 400/500/700 self-hosted woff2 (`appearanceFonts.ts`, `index.css @theme :139-144`; mobile `app.config.ts:241-264` DM Sans → Roboto). Scale: body 14; row/hostname 14/500; card title 16/500; project name 20/500 (GUI "large"); picker card name 20/500 with `›` at .2; description 13/1.75 at .7 (`.u-desc`); **micro-label 10px/600 uppercase .06em** for kickers (`RUNTIMES`, `THREAD · …`, `WAITING FOR YOU`) and 10px/500 uppercase .5px at .7 for status words — the GUI's 8px status label (`status-icon-base.component.scss:1426`) is raised to 10 as the floor. Mono stays T3's stacks for diff/terminal (perf; Ghostty and Pierre own their fonts) and Roboto Mono only for hostnames/URLs/log lines inside Zerops cards.

## Shape
`--radius` 10px (= `.mat-card`), cards borderless in light, 1px `rgba(255,255,255,.06)` in dark; `--control-radius` → `rounded-full` on `ui/button.tsx` primary/secondary/outline (the GUI forces 17px pills, `_material.scss:13-19`); chips `ui/badge.tsx` radius 10, 10px uppercase, tinted; info-chips (`mnt`, `Control Plane`) 24px white radius 8 with a 14px icon; rows radius 8; dialog 16 (`dialog-styles.ts`), light scrim white `.79` (the GUI's), dark `rgba(4,7,6,.72)`; key chips `ui/kbd.tsx` → `#efefef` border `#adb3b9` radius 3; composer keeps its 22px shell and 16px strip (`index.css:993-1060` clip-path geometry is hard-coded px by design); selected/active = **2px `#0077cc` outline** (`menu-item-card.component.scss:17-23`), dark `rgba(0,229,192,.4)`; depth by tint only — `c-soft-elevation` grey triple shadow reserved for popovers/dialogs.

## Status vocabulary (dot + word, always both — hue-only fails CVD, R5 risk 3)
Service: `ACTIVE/RUNNING` green-400 `#66bb6a`; transient (`CREATING`, `*_BUILD_*`, `UPGRADING`…) blue-400 `#42a5f5` + duty-cycled pulse; `READY_TO_DEPLOY` blue-300 settled, no pulse; `STOPPED` orange-400 `#ffa726`; `FAILED/*_FAILED` red-400 `#ef5350`; `DELETED` grey-400. Adoption: `mounted` white info-chip; `adoptable` orange note; `zcp-self` `Control Plane` chip. Lifecycle tones: idle grey / active blue / **waiting orange on `#fff4e0`** / done green on `#e8f7ec`. Process steps in import/deploy cards: 17px icon in a 2px-bordered white circle on a `30px 1fr` grid — queued grey, running blue, finished green, failed red (`process-step-state.component.scss`). Agent auth: `AUTHORIZED` `#2e7d32` with the 3px halo, `NOT AUTHORIZED` `#b26a00`.

## Icons
Web lucide (`apps/web/package.json`), mobile SF Symbols with the Tabler map (`AppSymbol.tsx:86-189`); no Material Icons webfont (blocking font, layout shift — AGENTS.md perf). One semantic table `zeropsIcons` (open-url, mounted, terminal, cloud-ide, console, logs, device, authorized, region, access…) resolved per client. Zerops mark/wordmark from `libs/zui/src/logo/` as React SVG; provider marks `currentColor` at 18px; the 87 service icons (`libs/zef/src/shared-assets/custom-icons/`) at 14px .8 opacity on Runtimes/Data rows — the GUI sets no icon-per-row convention (R6 §2), so this is a choice for scannability, not continuity.

## Motion budget
Easing `cubic-bezier(.4,0,.2,1)`, 200ms colour/opacity, 250ms borders; route enter staggers pane 300ms@200 / content 300ms@350 with a 5px lift (`outer-route-animation`) — once per navigation. In-flight pulse = index.css's existing `steps()`-timed `status-pulse` (`:145-268`), never the GUI's infinite `pulse/ripple/bounce/shimmer/bokeh`; `prefers-reduced-motion` disables all of it. Sticky header uses `position: sticky`. No animated gradients, no repainting halos.

## Mapping onto primitives
`ui/button.tsx` (pill variants, `compact` white pill for the panel grid), `badge.tsx` (chips), `card.tsx` (flat surface), `input.tsx` (white, 1px `#d4dfea`), `kbd.tsx`, `toast.tsx` (snack colours green-400/blue-400/orange-400/`#cc0011`, radius 8, min-width 320), `dialog-styles.ts`, `sheet.tsx`, `sidebar.tsx:799-830` (row styles → pane rows), `popover.tsx`/`menu.tsx` (grey soft elevation). The pane cards are new components, not `ui/*` changes. Roles web never consumed (`textMuted`, `accent`, R5 §1) gain consumers in the pane.

## Web
## Landing (signed out, same bundle everywhere)
Today the container-served bundle renders T3's "Pair with this environment" because `isHostedStaticApp()` is false there (`hostedPairing.ts:35-46`) and `__root.tsx:62-83` falls to `requires-auth` → `/pair`. The single-product rule: the `requires-auth` branch renders the Zerops landing whenever the origin's descriptor lists `zerops-identity` in `bootstrapMethods` (spec §3.5) — no build flag, no hosted-URL check; `/pair` survives only for a descriptor without it. Layout: canvas `#eceff3`, centred 400px white card radius 16 (the GUI login, `01-login.png`): mark + "Zerops Code" in Roboto 500, "Sign in with your Zerops account", email, password, `Sign in` blue pill; TOTP as a second step; `Create account` → registration with Turnstile or the `app.zerops.io` hand-off (Z3C-3); GitHub/GitLab sign-in pills only when the OAuth return path is proven (spec §4.1 covers password/TOTP only — until then they link to the GUI). Footer: `Not using Zerops? Connect a server manually` → the old token form. Replaces `ZeropsLandingShell`'s `rounded-3xl border-border/55 bg-card/20` frame.

## Picker (`/`, signed in)
```
+--------------------------------------+-----------------------------------------------------------+
| [Z Karel v]   [ Search  ^K ]         |  Search by project name...           Created v    [=] [==]|
+--------------------------------------+-----------------------------------------------------------+
|  K   KRLS                            |  +-------------------------+  +-------------------------+ |
|      org . 3 projects                |  | * ACTIVE                |  | * ACTIVE                | |
|                                      |  | tst                   > |  | z3-eval               > | |
| +-- Projects ----------------------+ |  | [EU CENTRAL (PRG1)]     |  | [EU CENTRAL (PRG1)]     | |
| | One Zerops project = one place   | |  | zcp . CONNECTED         |  | zcp . READY TO CONNECT  | |
| | your agent builds, deploys and   | |  | 2 threads . 1 working   |  | ( Enable Zerops Code )  | |
| | verifies.                        | |  +-------------------------+  +-------------------------+ |
| | ( + New project )                | |  +-------------------------+  +-------------------------+ |
| +----------------------------------+ |  | * PREPARING             |  | * STOPPED               | |
|                                      |  | shop                    |  | old-demo                | |
| Account                              |  | setting up your project |  | project is not active   | |
|  ai@keyup.cz          ( Sign out )   |  | ~ creating container    |  | Open in Zerops >        | |
|  Open Zerops >                       |  |   0:42  ( Stop waiting )|  |                         | |
|                                      |  +-------------------------+  +-------------------------+ |
| Devices                              |                                                           |
|  This Chrome on macOS                |                                                           |
|  ( Connect another device )          |                                                           |
+--------------------------------------+-----------------------------------------------------------+
```
Content header = the GUI's `zui-header-actions` (search field, `Created ↓` sort pill, card/list toggle). Cards `#f3f5f7` radius 10, 2-column ≥1200, 1-column below; per card: status dot + word, name 20/500 with `›`, region chip, container line from `candidates.ts` (`CONNECTED / READY TO CONNECT / PREPARING / NOT AVAILABLE · <reason>`), a thread summary line for connected projects (`2 threads · 1 working`, from the environment's thread projection), one action pill. The `PREPARING` card is the provisioning waiter (`ZeropsProvisioningPanel` states → card states). Controls pane: org card, the GUI's blue-outlined "Projects" info card with `+ New project` (screenshot `03-after-login.png`), Account, Devices. Multiple orgs → the org pill switches; every zcp container in every org is a card (Z3C-2).

## Thread view
```
+--------------------------------------+-----------------------------------------------------------+
| [Z tst v]   [ Search  ^K ]           | Document Kanban API   * DEVELOPING KANBANDEV   IDE  >_  []|
+--------------------------------------+-----------------------------------------------------------+
| < Projects                           |                                                           |
| * ACTIVE                             |         +- you ------------------------------------------+|
| tst                        (o)  (:)  |         | Vytvor mi kanban                              | |
| [FULL ACCESS] [EU CENTRAL (PRG1)]    |         +------------------------------------------------+|
|                                      |                                                           |
| Threads                    ( + New ) |  I'll set up kanbandev, kanbanstage and db first.         |
| * Document Kanban API          5m    |                                                           |
|   Fix auth redirect         2h  ok   |  +- PLAN . 3 services -----------------------------------+|
|   Add Redis cache          snoozed   |  | kanbandev     nodejs@22          * CREATING          | |
|   All threads (12) >                 |  | kanbanstage   nodejs@22          * CREATING          | |
|                                      |  | db            postgresql@18      * ACTIVE            | |
| Services              3 . live       |  | 1 of 3 ready                                         | |
|  RUNTIMES                            |  +-------------------------------------------------------+|
|  * kanbandev  nodejs@22  [mnt] Open> |                                                           |
|      kanbanstage         o pending   |  Worked for 40s >                                         |
|  DATA                                |                                                           |
|  * db  postgresql@18   [ Console ]   |  +- DEPLOY . kanbandev ----------------------------------+|
|  INFRASTRUCTURE                      |  | build ok   deploy ok   [ kanbandev-xxxx.zerops.app > ]||
|  * zcp  Zerops Control Plane         |  | verify     HTTP 200 ok                               | |
|                                      |  +-------------------------------------------------------+|
| Quick actions                        |                                                           |
|  ( Deploy )  ( Show logs )           | +--------------------------------------------------------+|
|  ( Add Redis )                       | | Ask for changes...                                    | |
|                                      | |                                                       | |
| == Zerops Control Plane  * ACTIVE == | | Claude Code v   High v   Full access v            (^) | |
| |  ( Terminal )   ( Cloud IDE )    | | +--------------------------------------------------------+|
| |  ( SSH > )    ( Desktop IDEs > ) | |        kanbandev . main                    2 changed files|
| |----------------------------------| |                                                           |
| |  Claude Code        * AUTHORIZED | |                                                           |
| |  Codex          o NOT AUTHORIZED | |                                                           |
| |  Devices   this Chrome . +1 more | |                                                           |
| ====================================  |                                                           |
+--------------------------------------+-----------------------------------------------------------+
```
**Header** (52px `WorkspacePageHeader`, `--workspace-topbar-height`): the breadcrumb `project / thread` (`ChatHeader.tsx`) becomes thread title only — the project is already the pane's headline; inline rename stays. Beside it the **strip**: dot + 10px uppercase phrase (`zeropsStripState`, phrases pinned in `zerops/strip.ts`), tone-coloured; `waiting for you` on `#fff4e0`. Right: `IDE` (was `OpenInPicker` → Cloud IDE), terminal toggle, panel toggle (`PanelLayoutControls`). `GitActionsControl` (Commit/Push) is hidden on Zerops (D4). Header actions already use a container query (`@3xl/header-actions`, `ChatHeader.tsx:383`); the strip truncates from the right before the actions do.

**Timeline** (`MessagesTimeline.tsx`, LegendList): user bubble `messageSurface` blue tint on the right; assistant prose on canvas; the `Worked for 40s ›` work-group fold stays for generic tools, but **Zerops cards are hoisted out of the fold** as their own timeline rows (today `ZeropsToolCard` mounts at `:2605` inside the collapsed "Used N tools" group — `verified.md:425` calls this a product call; the answer is yes). Cards on `#f3f5f7` radius 10: kicker `PLAN · 3 services` / `DEPLOY · kanbandev` / `VERIFY · kanbandev` / `IMPORT` / `MOUNT` / `SUBDOMAIN` / `ERROR · SSH_DEPLOY_FAILED`; plan = hostname/type/status table with a `1 of 3 ready` footer; import = process steps in bordered circles; deploy = `build ✓ deploy ✓` + URL as a white info-chip with the globe; verify = `HTTP 200 ✓`; error = `#fdefef` frame, mono log lines in a 200px scroll (`zui-errors-printer`), hint, `network` chip. Every card is a total decoder; the generic block on `undefined` (Z3F-7).

**Question card** (`ComposerPendingUserInputPanel.tsx`): kicker `WAITING FOR YOU` orange; options as white tiles on the card, selected = 2px `#0077cc` outline + `#f1f9ffa8`; digit key chips; descriptions 13px .7; `Other…` as a text tile; multi-select as checkboxes; the strip mirrors `waiting for you`. **Credential card**: `#fff4e0` frame, `GIT_TOKEN · kanbandev`, a password field, `Save to service` blue pill, note `stored as a service secret — never shown to the agent`; saved → `saved ✓` chip with `Replace`. The value goes card → server → service secret (brief §8.3); the composer never sees it.

**Composer** (`ChatComposer.tsx`): shell and geometry kept; white `surfaceRaised`, `Coding agent` label instead of provider, send button `#0077cc`. The `BranchToolbar` (`Local checkout · main`) becomes the **context strip**: `kanbandev · main` — the mounted repo this thread's checkpoints target (spec §6.4), `2 services` when multi-repo — plus the changed-files count. Quick-action pills move above the composer only when the pane is collapsed (the existing `composerHandleContext` insertion).

## Right panel
Inline vs sheet is a **container query on the main column** (≥1040px main width), not the fixed 981px media query (`rightPanelLayout.ts:1`) — a media query cannot see whether the pane is open; T3 already uses container queries in the header. Surfaces: Files (root `/var/www`, top-level folders = mounted hostnames, spec §6.1; the `mnt` chip deep-links here), Diff (grouped by service), Terminal, Browser (desktop only — now the way to preview a subdomain in-app), Agents, Data Console (enabled when a Data row exists). Restyled to the pane's card grammar; the picker grid (`03-right-panel-picker.png`) keeps its shape with 10px kickers and grey hover.

## Settings
Sections (`settingsSearch.ts:28-38`): General, Appearance (theme library now defaults to "Zerops"; T3 Chat/Grove/… remain selectable), Keybindings, **Coding agents** (Providers; the same AUTHORIZED tray + model defaults; `Authorize in Zerops ›` until S7), Integrations, **Devices** (Connections: rows `Zerops Code · Chrome on macOS · this device`, revoke, `Connect another device`), **Account** (Zerops: email, org chips, Sign out, Open Zerops), Archive. Source Control is hidden on Zerops (identity/PAT/push are zcp's). The settings nav in the pane is a card list with the project header replaced by `‹ Back`.

## Command palette
The GUI palette skin (`R1 §1`: 16/18px search row, teal scope chip with the project name, 10px/700 group titles at .45, 40px rows radius 8, key-chip footer). Groups: Threads (this project) / Projects (switch, New project) / Services (Open URL, Files, Terminal, Logs, Console per row) / Quick actions / Surfaces / Settings / Theme. Shortcut `Z` toggles the pane.

## Desktop
Desktop is the same bundle in Electron, one smoke build (D6). What it adds or changes:

- **Window chrome.** Window Controls Overlay is already wired (`index.css:124-137` `.wco` overrides, `env(titlebar-area-*)`, `--workspace-titlebar-control-size`, `COLLAPSED_SIDEBAR_TITLEBAR_INSET_CLASS` in `workspaceTitlebar.ts`). The pane's floating app bar (org pill + ⌘K) sits in the titlebar area: the GUI's 90px top padding for the controls pane becomes `env(titlebar-area-height)` + 24px. Initial window background and titlebar symbol colours come from the generated brand tokens instead of `DesktopWindow.ts:34-35,128-130` literals (`#0a0a0a/#ffffff`, `#1f2937/#f8fafc`) — canvas `#eceff3`/`#0c0f0e`, symbols `#1a1a1a`/`#e9eeec`.
- **Naming.** `APP_BASE_NAME` in `apps/desktop/src/app/DesktopEnvironment.ts:88-110` → "Zerops Code" (menu bar, About, notifications); `DesktopAppBranding` carries names only (`packages/contracts/src/ipc.ts:175-185`). The `t3code://app` / `t3code-dev://app` origins stay allowlisted (spec §2.7) — the scheme rename is out of scope by D6 and the user never sees it.
- **Icons.** A Zerops `.icon` project per channel under `assets/{dev,nightly,prod}/app-icon.icon` (`scripts/lib/brand-assets.ts:1-32`; `pnpm icons:export` / `icons:check` fail without it). The mark (`libs/zui/src/logo/`) on a canvas-coloured tile; stage pill for dev/nightly stays.
- **Remote-only build.** T3 desktop bundles a server runner (AGENTS.md "Desktop… bundles the server runner"). On Zerops the desktop is a client of a container: the first screen is the Zerops sign-in, the origin picker is the project picker, the local-folder paths (`add-project` browse, `wsl-folder`) are hidden in Zerops mode, and the bundled runner is dormant — kept for a hand-paired server, not shown. The Browser surface ("Only available in the desktop app") becomes the in-app preview of a deployed subdomain.
- **Update posture.** The container upgrades on restart (spec §2.6); the desktop bundle auto-updates on its own cadence. A newer server descriptor must not strand an older desktop: `basePath` is optional in the descriptor (Z3D-7) and the client tolerates a mismatch with a named error, so a version-skew banner ("your app is older than this project's Zerops Code") is the only desktop-specific UI.
- Honest scope: no desktop-only design work beyond the above; if the smoke build shows WCO clipping the pane's pill bar, the bar drops below the titlebar rather than the design bending.

## Mobile
## What the phone is for
The notification-driven loop: a Live Activity says the agent is working → tap → read the strip → answer the question card or approve → glance at the map → open the deployed URL → send a one-line follow-up. It is a **watch-and-answer** surface, not an authoring or review station (ReviewSheet and the terminal stay because they exist, at their current fidelity). Mobile inherits the door through `packages/client-runtime` (D6, S5 acceptance "reaches a thread with no code typed"); it has zero Zerops code on `main` today (R3 §6).

## Translating the DNA without fighting native
Keep everything R3 lists as must-stay: native-stack with flat thread routes and iOS 26 header morphing (`Stack.tsx:229-233`), Liquid Glass headers/composer (`GlassSurface.tsx`), form sheets with detents, `UIMenu`, the Mail search toolbar, keyboard stack, haptics, SF Symbols ↔ Tabler, Android in-flow `AndroidScreenHeader`/`AndroidAnchoredMenu`/FAB. The Zerops DNA enters through: the 65 `--color-*` tokens generated from `ZEROPS_THEME` (canvas `#eceff3` → `--color-screen`, cards `#f3f5f7`/white tiles, primary `#262626` → `#0077cc`, bubble `#007aff` → `messageSurface` `#e3f4ff` with dark text), Roboto in place of DM Sans (`app.config.ts:241-264`, `global.css:215-217`), radii 24 → 16 on `SettingsSection`/cards/`EmptyState` (R3 tier C), the micro-label + dot-and-word status grammar in `StatusPill`/`ConnectionStatusDot`, pills for actions. Glass tint from `--color-glass-tint` becomes canvas-tinted, so the iOS header reads as the GUI's grey. No Material components on iOS; the GUI is Angular Material, the app ports its palette and typography, not its widgets. Android keeps its token-styled chrome; the FAB is the blue `New thread`.

## Screens
1. **Sign in with Zerops** — email/password, TOTP; registration via a Turnstile WebView or the `app.zerops.io` hand-off (Z3C-3). Replaces `ConnectionsNewRouteScreen`'s host + code fields for Zerops; the QR/code path stays as `Connect manually`.
2. **Projects** — Home when the account has several: GUI dashboard cards (dot + word, name, region chip, container line, one action pill: `Open` / `Enable Zerops Code` / `Stop waiting`); `New project` as a form sheet. `useZeropsCandidates`/`provisioning`/`containerHealth` promoted to client-runtime.
3. **Project home** — Home for one project, or after picking. The pane's cards as sections in the same order: lifecycle line, Threads, Services, quick actions, Control Plane. `HomeScreen.tsx` already groups threads by project (`ThreadListGroupHeader`); on Zerops the grouping is fixed to the one project and the header becomes the project header.
4. **Thread** — `ThreadDetailScreen`; the strip is the native header **subtitle** (iOS 26 editor-style title, `Stack.tsx:89-100`), `waiting for you` in orange; Zerops cards inline in `ThreadFeed` (same decoders); question card = `PendingUserInputCard` extended with descriptions, `Other`, multi-select; credential card = a form sheet with a secure field (`expo-secure-store` never involved — the value goes to the server once), never the composer.
5. **Services sheet** — the map as a form sheet (detents `[0.55, 0.92]`), rows with `Open ›` / `Terminal` / `Logs` (prefill) / `Data Console` (in-app browser to the caller-bound console). Reached from the header's map button and from the lifecycle line.
6. **Control Plane sheet** — `Terminal` (`ThreadTerminal` route), `Cloud IDE` (in-app browser to the container origin; the code-server password is a service env the user's own token can read — verify the login accepts it before promising one-tap), agent rows with `AUTHORIZED` state (read-only until S7), Devices.
7. **Settings** — Account (Zerops), Devices, Coding agents, Appearance, Archive; `settings-sheet-targets.ts` gains `SettingsAccount`, `SettingsDevices`.

## Wireframes
Project home:
```
+--------------------------------------+
| < Projects       tst          (:)    |
| * ACTIVE   [EU CENTRAL (PRG1)]       |
|--------------------------------------|
| * DEVELOPING KANBANDEV               |
|   kanbandev deployed ok . stage pend |
|--------------------------------------|
| THREADS                    ( + New ) |
| * Document Kanban API         5m  >  |
|   Fix auth redirect        2h ok  >  |
|   Add Redis cache         snoozed >  |
|   All threads (12)                >  |
|--------------------------------------|
| SERVICES              3 . live    >  |
| * kanbandev   nodejs@22      Open >  |
|     kanbanstage          o pending   |
| * db          postgresql@18          |
| * zcp         Control Plane          |
|--------------------------------------|
| (Deploy) (Show logs) (Add Redis)     |
|--------------------------------------|
| ZEROPS CONTROL PLANE      * ACTIVE   |
| ( Terminal )    ( Cloud IDE )        |
| Claude Code           * AUTHORIZED   |
+--------------------------------------+
```
Thread:
```
+--------------------------------------+
| <   Document Kanban API              |
|     * WAITING FOR YOU                |
|--------------------------------------|
|          +-----------------------+   |
|          | Vytvor mi kanban      |   |
|          +-----------------------+   |
| I'll set up three services first.    |
|                                      |
| +- PLAN . 3 services -------------+  |
| | kanbandev   nodejs@22 * CREATING|  |
| | kanbanstage nodejs@22 * CREATING|  |
| | db          postgres  * ACTIVE  |  |
| +---------------------------------+  |
|                                      |
| +- WAITING FOR YOU ---------------+  |
| | Confirm the plan?               |  |
| | [1] Confirm                     |  |
| | [2] Change the plan             |  |
| | [3] Other...                    |  |
| +---------------------------------+  |
|                                      |
|--------------------------------------|
| ( Ask for changes...          (^) )  |
| Claude Code v          kanbandev .   |
+--------------------------------------+
```
Services sheet (map + strip):
```
+--------------------------------------+
|               ----                   |
| Services               3 . live      |
|--------------------------------------|
| RUNTIMES                             |
| * kanbandev     nodejs@22            |
|   mounted  /var/www/kanbandev        |
|   [ Open > ] [ Terminal ] [ Logs ]   |
|   o kanbanstage      pending         |
|--------------------------------------|
| DATA                                 |
| * db            postgresql@18        |
|   [ Data Console ]                   |
|--------------------------------------|
| INFRASTRUCTURE                       |
| * zcp     Zerops Control Plane       |
|   Claude Code         * AUTHORIZED   |
|--------------------------------------|
| updating every few seconds           |
+--------------------------------------+
```

## Tablet
`AdaptiveWorkspaceLayout.tsx` already splits into a list pane + content + an inspector pane (`workspace-inspector-pane.tsx`). The list pane becomes the Project home sections (the web pane), the content the thread, the inspector the Services map — the same three columns as web ≥1440, so the mental model transfers across devices.

## Live Activity
Title "Zerops Code", subtitle = the strip phrase (`developing kanbandev`, `waiting for you`), logo asset replaced (`assets/widget/T3Mark.svg` → the Zerops mark, frame ratio checked at `AgentActivity.tsx:233`); SwiftUI semantic colours stay, phase tints map to the status palette.

## Cross-client system
## One source
`ZEROPS_THEME: ThemeDefinition` (light + `variants.dark`, `sidebarArtwork: false`) in `packages/shared/src/themePalettes.ts`, made the default on both clients through the existing theme path (`useTheme.ts:39-45` default snapshot, `MOBILE_DEFAULT_THEME_ID`), then a root `scripts/generate-theme-tokens.ts` (R5 §4a) emitting: `apps/web/src/generated-theme.css` (`:root` + `@variant dark` `--app-theme-*`, so `index.css:1388-1494` shrinks to the semantic remap), `apps/mobile/global.css` light/dark blocks (the existing `generate-uniwind-themes.mts` already renders every other variant), the `index.html` boot palette + `SPLASH_COLORS` + `<meta theme-color>`, `packages/shared/src/generated/brandColors.ts` for `themePreview.ts` and `DesktopWindow.ts`, and the `--zerops-*` non-role block. Six hand-synced copies (R5 §1) become zero. The web-unconsumed roles (`textMuted`, `accent`) get consumers in the pane so the role set is fully live on both clients.

## Shared vs per-client
Shared (`packages/client-runtime`, pure TS, no components — R3 §7): the door and flow (`zerops/{api,session,registration,newProject}` already there) plus the promotion of `apps/web/src/zerops/{candidates,provisioning,containerHealth,firstPrompt,strip,serviceMap,quickActions,cards/payloads,activityResult}.ts` (all UI-free; R3 §6 names them as candidates); a `zeropsStatus.ts` (platform status → tone, adoption → chip) and `zeropsCopy.ts` (group titles, pill labels, status words, the pinned strip phrases) so no phrase has a second copy. Per client: the shell (web pane vs mobile sections), primitives (`ui/*` vs uniwind + native modules), the icon table resolved per library, glass. The brand SVGs live in `packages/shared` and are rendered by each client's SVG path.

## Drift guards
1. Generator byte-equality + `--check` in CI (the mobile pattern, `generate-uniwind-themes.test.ts`), extended to every emitted file.
2. Role coverage (`Object.keys(colors) ⊇ THEME_COLOR_ROLES`, light and dark) and contrast assertions: ≥4.5 text/canvas, text/surface, `messageActionForeground/messageAction`, `errorForeground/errorSurface`, `warningForeground/warningSurface`, `updateForeground/updateSurface`; ≥3.0 `focus/canvas`; every value opaque.
3. Copy lint: a test over user-facing string literals in `apps/{web,mobile}/src` forbidding `T3 Code`, `T3 Connect`, `Tailscale`, `pairing`, `worktree`, `Local checkout`, user-facing `environment`, and `control plane` as a self-description — the shape of zcp's content lints (`TestNoBareZeropsURIInAgentContent`), with an identifier allowlist.
4. Phrase parity: `strip.test.ts` asserts the journey phrases verbatim; mobile imports the same module, so there is nothing to drift.
5. Status vocabulary: a table test that every `SETTLED_ZEROPS_SERVICE_STATUSES` entry and the transient class maps to a tone and a word; web `statusVariant` (`ZeropsServiceMap.tsx:26`) and mobile `StatusPill` both read it.
6. Pane purity: the pane's whole module graph imports no mutating RPC (the `ZeropsQuickActions.test.tsx` assertion widened to `components/project-pane/**`).

## Visual regression harness
Web: a Playwright screenshot suite over fixture states (the same fixtures `ZeropsServiceMap.test.tsx` / `strip.test.ts` use, feeds mocked) at 390 / 1024 / 1440 × light/dark, committed PNGs with a pixel threshold, run in CI — not agent computer-use (AGENTS.md). Mobile: the existing showcase harness (`scripts/mobile-showcase.ts`, `docs/operations/mobile-app-store-screenshots.md`: Home, Thread, Terminal, Review, SettingsEnvironments per theme id) extended with Project home and the Services sheet, run on the `zerops` id. Both suites are also the "hit every surface" evidence a PR needs.

## Migration
| Phase | What | Files / areas (cite) | Effort |
|---|---|---|---|
| **0 — Skin + vocabulary** | `ZEROPS_THEME` as default on both clients (R5 option a, step 1); Roboto self-hosted / bundled; `ui/*` primitives to pills, chips, flat cards, key chips, snacks; brand touchpoints; copy sweep + copy lint | `themePalettes.ts`, `useTheme.ts:39-45`, `index.html` boot palettes, `themePalette.ts` `T3_CODE_*`, `ThemePreviewCircles.tsx`, `appearanceFonts.ts`, `index.css @theme`; mobile `global.css`, `app.config.ts:241-264`; R2 §4's ~15 brand files, R3 §5's ~40 (storage keys excluded); R2 track (c)'s ~18 `ui/*` files; `AuthSurfaceShell.tsx` gradient; accent swatch fallback (`ProviderAccentColorPicker.tsx:21`) | M |
| **1 — One door, one bundle** | Zerops landing on the container origin keyed on the descriptor's `bootstrapMethods`; picker as dashboard cards on `/`; provisioning as a card state; `/zerops` route folded away | `routes/__root.tsx:62-95`, `routes/-chatIndexView.ts`, `routes/_chat.index.tsx`, `ZeropsLandingShell.tsx`, `ZeropsProjectPicker.tsx`, `ZeropsProvisioningPanel.tsx`, `clientMetadata.ts:80-94`, `routes/zerops.tsx` (delete), `ZeropsProjectsPage.tsx` (delete) — R2 §5 change set | M |
| **2 — The controls pane** | `components/project-pane/*` (ProjectHeaderCard, ThreadsCard, ServicesCard, QuickActionsCard, ControlPlaneCard); fourth branch at `AppSidebarLayout.tsx:227-233`; pane widths; `zerops` right-panel kind removed, `pull-request` hidden on Zerops, `data-console` added; palette groups; utility menu; header breadcrumb → title; strip as display + pane echo; per-service thread index for D4's "also works on" | `AppSidebarLayout.tsx`, `threadSidebarWidth.ts`, `rightPanelStore.ts:17-27`, `RightPanelTabs.tsx:268-345`, `ZeropsPanel.tsx` (delete), `ZeropsServiceMap.tsx` → rows, `ZeropsLifecycleStrip.tsx`, `CommandPalette.tsx`, `SidebarChrome.tsx:147-233`, `ChatHeader.tsx`, `DraftHeroHeadline.tsx`; ThreadsCard built on `Sidebar.logic.ts` resolvers (`resolveThreadStatusPill`, `sortThreadsForSidebar`, `resolveSidebarThreadStatus`) — R2 track (d) | L |
| **3 — Thread content** | Zerops cards hoisted out of the work-group fold; card restyle + import-steps and credential kinds; question card restyle; context strip replaces `BranchToolbar`; `GitActionsControl` hidden on Zerops; `OpenInPicker` → Cloud IDE; Files root grouped by hostname | `MessagesTimeline.tsx:2605` + row kinds, `ZeropsToolCard.tsx`, `zerops/cards/payloads.ts`, `ComposerPendingUserInputPanel.tsx`, `ChatView.tsx:7158-7188`, `FileBrowserPanel`; credential verb on the server is owner-designed (brief §8.3, S6/S7) | M |
| **4 — Mobile** | Promote `apps/web/src/zerops/*` pure TS to `client-runtime`; S5 flows (sign-in, picker, waiter, Enable); Project home, Services sheet, Control Plane sheet; cards in `ThreadFeed`; settings targets; DM Sans → Roboto; radii; bubble | R3 §6 seam map (`App.tsx`, `Stack.tsx`, `settings-sheet-targets.ts`, both `SettingsRouteScreen` trees), `HomeScreen.tsx`, `ThreadDetailScreen.tsx`, `PendingUserInputCard`, `AgentActivity.tsx`, `remoteRegistration.ts:505-506`, `authClientMetadata.ts`; R3 tiers A–D | L |
| **5 — Desktop smoke** | Names, window colours from generated tokens, `.icon` projects, WCO offsets for the pane bar, hide local-folder paths in Zerops mode | `DesktopEnvironment.ts:88-110`, `DesktopWindow.ts:34-35,128-130`, `assets/*/app-icon.icon`, `index.css:124-137` | S |
| **6 — Generator + harness** | `scripts/generate-theme-tokens.ts`, delete hand-written defaults, drift tests, Playwright + showcase suites | R5 §4a file list; `scripts/mobile-showcase.ts` | M |

Order: 0 → 1 → 2 → 3 in one line (each ships alone and is visible); 4 after 2 (it copies the pane's card order); 5 any time after 0; 6 alongside 2. Phase 0 already makes every screenshot "look Zerops"; phase 2 is where the angle earns its name.

**Throwaway:** `ZeropsPanel.tsx` (58 lines), the `zerops` right-panel kind, `routes/zerops.tsx` + `ZeropsProjectsPage.tsx`, `ZeropsLifecycleStrip` as a button, the sidebar's `Zerops` utility item, every `rounded-xl border-border/55 bg-card/20` ad-hoc frame, `HostedStaticOnboardingState`/`NoProjectsHero`/`IndexDraftLanding` on the Zerops path. **Not throwaway:** every logic module under `apps/web/src/zerops/` (strip, serviceMap, quickActions, cards, candidates, provisioning, containerHealth, firstPrompt) — they are the assets and move up, not out. **Liability:** `Sidebar.tsx` stays for the non-Zerops path; it is 3960 lines nobody on the product path exercises, and it should be deleted the day hand-paired servers are dropped.

## Risks
1. **The sidebar rewrite loses features silently.** `Sidebar.tsx` carries search, multi-select, dnd-kit pinned reorder, snooze shelves, jump hints, prewarm — none exported as a library. *Mitigation:* ThreadsCard v1 is a declared subset (pin/unpin via menu, snooze/unsnooze, archive, search through ⌘K) built on `Sidebar.logic.ts`'s exported resolvers; DnD reorder returns in v2; "focus mode" gives power users a full-height list. Reverse states are a checklist item.
2. **Width budget breaks at 1280–1440.** Pane 384 + main 640 (`THREAD_MAIN_CONTENT_MIN_WIDTH`) + a 400px panel = 1424. *Mitigation:* the pane is offcanvas below 1280; the right panel's inline threshold is a container query on the main column, so a wide pane pushes the panel into the sheet instead of crushing the composer. The GUI's 480 is explicitly not copied.
3. **Two lifecycle readings drift.** The header strip and the Services card both render the envelope. *Mitigation:* one selector (`zeropsStripState` + a new per-host selector in `serviceMap.ts`), one test file asserting both readings from one fixture; the card never computes a phrase.
4. **The angle demotes threads.** T3 users live in a cross-project thread list; here threads belong to one project and the cross-project view is the picker. *Mitigation:* picker cards show `N threads · M working`; ⌘K searches threads across projects; the picker is one click away in the project header. Stated as a trade, not hidden.
5. **The mint panel promises what S7 has not built.** An `AUTHORIZED` tray with no Authorize action is a one-way door; SSH and Desktop IDEs need VPN. *Mitigation:* v1 rows are read-only status with `Authorize in Zerops ›`; SSH/Desktop IDEs are link-outs labelled as such; the four-pill grammar is kept so S7 fills it without moving anything.
6. **No Zerops mobile precedent.** The phone sections are invented. *Mitigation:* they mirror the pane's card order exactly, so the desktop web is the precedent; native chrome is untouched (R3 must-stay list); the showcase harness pins the result.
7. **Perf regressions in the pane.** Every topology snapshot re-renders rows; the GUI's animations loop. *Mitigation:* rows memoised on `serviceId+status+mounted+subdomainUrl`; snapshots publish only on change (spec §5.1); only `steps()` duty-cycled pulses; no metrics graphs (the feed has none — the GUI's cores/RAM tiles are not promised).
8. **Live push is unproven (Z3F-8) and a stale map lies.** *Mitigation:* the liveness line (`live | polling`) is always rendered; a running tool is attributed to the map, not a row (`serviceMap.ts` comment); the 90s nudge window after a tool completes is the fallback the user sees as "updating every few seconds".

## Signature moments
- The same event in both panes: `zerops_deploy` finishes, the Services row flips from a pulsing blue `CREATING` to `● ACTIVE` and grows an `Open ›` chip on the left, while the deploy card on the right shows `build ✓ deploy ✓` and the URL info-chip — one topology snapshot, one envelope, no spinner lying.
- The mint `Zerops Control Plane` card at the foot of the pane, white `Terminal` / `Cloud IDE` pills, and the `Claude Code · AUTHORIZED` row with the green haloed dot — pixel-for-grammar the card a Zerops user already has open in the GUI tab (`05-project-detail-full.png`).
- The project picker as the GUI dashboard: `#f3f5f7` cards, dot + `ACTIVE`, 20px name, purple region chip, and on an old container a white `Enable Zerops Code` pill that turns into a blue `RESTARTING` dot for twenty seconds and then into `Open`.
- The strip beside the thread title in 10px tracked uppercase — `KANBANDEV DEPLOYED ✓ VERIFIED ✓ · KANBANSTAGE PENDING` — the GUI's micro-label grammar carrying a coding agent's state on a chat surface.
- A question card that is a Zerops card: white option tiles on grey, the chosen one outlined 2px `#0077cc`, digit key chips, `Other…` as a tile, and an orange `WAITING FOR YOU` kicker echoed by the strip and by the phone's header subtitle.
- The phone header subtitle that tells you why it buzzed: `● WAITING FOR YOU` under the thread title in an iOS 26 glass header, one tap from the answer.
