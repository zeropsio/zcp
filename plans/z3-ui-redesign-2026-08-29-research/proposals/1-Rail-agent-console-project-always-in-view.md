# Rail — agent console, project always in view

## Thesis
Keep T3's thread exactly where it is — virtualised timeline, glass composer, checkpoints, right-panel surfaces are the parts worth forking — and wrap it in one persistent Zerops layer that never scrolls away: a lifecycle band under the 52px header that reads the envelope in the GUI's process-shell tints, a standing project rail on the right that is the GUI's project-detail column compressed to 280px (Runtimes / Data / Infrastructure rows with dot + uppercase label, the running tool, hand-offs), Zerops cards unfolded into the timeline as process shells, question and credential cards docked at the composer, and the quick-action dock in the slot where "Local checkout · main" sits today. The brief's own acceptance narrative is a two-column table (agent ↔ client, `plans/z3-brief-2026-08-28.md §7`); Rail makes the client column a permanent fixture instead of a right-panel tab you have to remember to open. It is right for Zerops Code because a Zerops user's mental model is "a project is a set of services with a status" (`shots/gui-nonmember/05-project-detail.png`), every turn of the agent exists to change that state, and the state already arrives as two read-only feeds (`docs/spec-z3.md §5.1–5.2`) — so the day-to-day question "what exists, what is live, where is the agent" is answered by a glance, not a click, while the thread stays the only place work happens.

## Information architecture
## Top-level objects

Account (Zerops identity) → **Projects** (one Zerops project = one z3 environment, D3) → **Threads**; per project: **Services** (the map), **Devices**. The T3 project (`/var/www`) is never shown. "Environment", "worktree", "checkout", "pairing" do not appear (R6 §3.2 glossary).

## Shell (web ≥1440)

Three columns, one canvas `#eceff3`: **sidebar 256px** (`components/threadSidebarWidth.ts`) · **thread** (min 640) · **project rail 280px** (new) · optional **right panel** (existing `RightPanelTabs`, inline ≥981px per `rightPanelLayout.ts:1`). The rail is a new standing column, not a right-panel kind: the right panel is for *work surfaces* (diff/files/terminal/preview/data console) that the user opens and closes; the rail is *state* and has no close affordance in the ordinary sense — only a collapse to a 44px dot column (⌘⇧Z, header toggle, palette "Toggle project rail"; reverse state is the same toggle).

**Sidebar** = threads of the *current* project, grouped Today/Yesterday/Older, each row carrying its strip phrase as the subtitle (`zeropsStripState`, `apps/web/src/zerops/strip.ts`) and the tone dot. Top of the sidebar is the **project pill** — the GUI's org pill (`app-bar.container.scss:90-108`) transplanted: Zerops mark on a teal-tinted pill, project name, chevron → switch project / New project / Open in Zerops GUI / Sign out. This replaces T3's "All projects ▾" scope menu (`Sidebar.tsx`); multi-environment stays because each container is its own server, but the user sees "projects". Utility row at the bottom: Settings · Devices · Usage (T3's `SidebarUtilityMenu`, `sidebar/SidebarChrome.tsx`, minus the Zerops item — the rail replaces `/zerops`).

**Thread column** top to bottom: `WorkspacePageHeader` (breadcrumb "kanban-shop / Build a kanban board", rename, actions) → **lifecycle band** (28px, full width, tinted by tone; today's `ZeropsLifecycleStrip` is a button beside the header, `ChatView.tsx:6906-6928`) → `MessagesTimeline` → composer with docked question/credential cards above it → **quick-action dock** in the `BranchToolbar` slot (`ChatView.tsx:7180`; today "Local checkout · main", vocabulary that must go anyway).

**Rail** top to bottom: project header (dot + ACTIVE, name 20/500, FULL ACCESS / EU CENTRAL chips as in `05-project-detail.png`) → three groups with rows (`ZeropsService`: hostname, `type`, `status`, `mounted`, `subdomainUrl`, `adoptionState`, `packages/contracts/src/zerops.ts:82-100`), the dev↔stage pair folded under the dev row (`ZeropsServiceMap.tsx:69-71`), prod project as a link (`:74-77`) → **running-tool line** ("⟳ zerops_deploy · 0:42", `ZeropsServiceMap.tsx:116-119`) → feed line (the doorbell tri-state as one quiet line — "updated 3 s ago · polling", never the degraded banner, spec §5.4) → footer: Devices count, Data Console.

## Where each Zerops surface lives

| Surface | Home | Opens |
|---|---|---|
| Lifecycle phrase | band under header; sidebar row subtitle; phone row subtitle | click → rail focus / phone project sheet |
| Service map | rail (≥1440); dot column (1280–1439); right-panel kind `zerops` (<1280, existing `rightPanelStore.ts:17-27`) | row → row actions |
| Zerops cards | timeline, **outside** the "Used N tools" fold for plan/import/deploy/verify/error; mount/subdomain stay folded but named in the fold header. (Whether cards escape the fold is recorded as an open product call in `z3/docs/internals/zerops/verified.md`; Rail takes the position that milestones escape.) | card → rail row highlight |
| Question card | docked above the composer, inside its `max-w-3xl` column; band says "waiting for you" (precedence pinned in `strip.ts`) | keys 1–9, arrows, "Other" (`ComposerPendingUserInputPanel.tsx`) |
| Credential card | same dock, distinct lock header; value → server → service secret via the zcp verb (S6, owner-designed); timeline gets a receipt with no value | "Or set it in the Zerops GUI ↗" as the caller-bound fallback until the verb exists |
| Quick actions | dock under the composer; prompts only (`zerops/quickActions.ts`, `ZeropsQuickActions.test.tsx` pins no RPC) | prefill |
| Data Console | new right-panel kind `data-console`, opened from a Data row's "Browse" (embed on the same 8080 origin, `docs/spec-dataconsole.md` embed reach) | |
| Cloud IDE / Web Terminal / SSH | rail row actions on the zcp row (the GUI's four pills, `zagent-service-card.feature.html:98-127`): Cloud IDE → new tab (code-server behind its cookie gate); Terminal → T3's own terminal surface; SSH → copy command | |
| Logs | quick action "Show logs for X" (agent reads) + rail row "Logs" → terminal running the read-only command | |
| Devices | Settings → Devices (was Connections); "Connect another device" = the one-time-token path, never "pairing" (spec §3.5, §4.8) | |

## Flow

**Sign in → project → thread.** One bundle, one gate. Today the container-served bundle falls to `/pair` because `isHostedStaticApp()` is false there (`hostedPairing.ts:35-46`, `__root.tsx:62-83`; R2 §5). Rail requires: an origin-aware branch of the `requires-auth` state — when the origin answers the z3 descriptor (`/z3/.well-known/t3/environment`, spec §4.5), render the Zerops sign-in (`ZeropsLandingShell`) and run identity connect against `window.location.origin`, skipping the picker (this origin *is* the project; "Other projects" links to the hosted client). Hosted client: sign in (email/password/TOTP, GitHub/GitLab as on `01-login.png`) → **picker** grouped Connected / Ready to connect / Preparing / Not available (`ZeropsProjectPicker.tsx`) with the readiness offer "Enable Zerops Code" for both `predates-z3` and `unreachable` (Z3C-5) → the restart shown *in the row* as "restarting · 12 s" (502 window rendered calm, spec §2.5, cap 30 s) → thread with the onboarding prompt composed, not sent (Z3C-8). **New project** = a row-shaped form in the picker (name + org), then the provisioning waiter states as the same row moving groups: Preparing (awaiting-project → awaiting-container → awaiting-health, with the cap shown as "40 s of ~5 min") → Connected. `pool-exhausted` is a row state with copy, not an error (Z3C-4).

## Empty and reverse states

- **No services yet**: rail shows the three group headers with "nothing here yet" under Runtimes and the single "Build something" chip in the dock (`quickActions.ts` bootstrap branch); band "no services yet".
- **Not a Zerops project** (`available:false`): rail, band and dock are absent; the thread is plain T3 ("Outside a Zerops project, nothing changes", spec §3). The manual one-time-token connect stays reachable from Devices.
- **Feed degraded**: last-good rows stay, one line "last read failed · retrying" in the rail footer (Z3F-1); never blank.
- **Bootstrap held elsewhere**: band "infra session held by thread X" (brief §7), thread X linked.
- **Two threads on one service**: the rail row shows "also: Fix login redirect" under the hostname, never a lock (D4).
- **Reverse**: rail collapse ↔ expand; question dismissed ↔ reopened from the band; credential card cancelled ↔ the fallback link stays; restart "restarting" ↔ "ready"; Data Console kind closes like any surface; snooze/unsnooze and archive/unarchive keep T3's existing pairs.

## Visual system
## Tokens (ZEROPS_THEME, light / dark) — mapped onto `THEME_COLOR_ROLES` (`packages/shared/src/themePalettes.ts:18-77`), values from R5 §2

| Role → consumer | Light | Dark |
|---|---|---|
| canvas / chrome / toolbar / sidebar → `--background`, `--app-chrome-background`, `--toolbar-background`, sidebar | `#eceff3` | `#0c0f0e` |
| surface → `--card` (rail rows, cards) | `#ffffff` | `#141918` |
| surfaceRaised → composer glass, band | `#ffffff` | `#1b2220` |
| surfaceOverlay → `--popover` | `#ffffff` | `#1e2624` |
| text / toolbarForeground → `--foreground` | `#1a1a1a` | `#e9eeec` |
| mutedForeground / iconMuted / textMuted | `#5f6a72` | `#9faea9` |
| sidebarMutedForeground (60% mix must clear 3:1, `index.css:94-98`) | `#4f5a62` | `#9faea9` |
| border / sidebarBorder / toolbarBorder | `#e0e0e0` / `#d4dfea` | `#262f2d` |
| input | `#d4dfea` | `#2a3331` |
| focus → `--ring` | `#0077cc` | `#5ab3ff` |
| messageAction → **`--primary`** (send, CTAs, switches) | `#0077cc` (hover `#005fa3`) | `#0077cc` (hover `#008ff5`) |
| accent (mobile primary button, md-link) | `#0077cc` | `#58a6ff` (fg `#0c0f0e`) |
| accentSurface → `--accent` (hover rows, chips) | `#f0f3f5` | `#232b29` |
| secondary / muted | `#f3f5f7` / `#f2f5f7` | `#232b29` / `#151b1a` |
| messageSurface (user bubble) | `#e3f4ff` | `#1e2e3b` |
| update / updateForeground / updateSurface (teal = brand-positive, never action) | `#02b1a3` / `#007e72` / `#def8f6` | `#00e5c0` / `#58efd4` / `#113630` |
| error / errorSurface → `--destructive` | `#cc0011` / `#fdefef` | `#e57373` / `#2f1717` |
| warning / warningForeground / warningSurface | `#ffa726` / `#bb4d00` / `#fdf6ef` | `#ffa726` / `#ffb74d` / `#3a301a` |
| codeBackground / terminalBackground | `#f2f5f7` | `#121716` / `#0c0f0e` |
| terminalCursor | `#007e72` | `#00e5c0` |
| sidebarRowSelected (a Zerops card) | `#ffffff` | `#141918` |
| toolbarControl (⌘K pill, flattened 4% black) | `#e3e6ea` | `#1b2220` |

Non-role colours the Zerops surfaces need (kept local to `apps/web/src/components/zerops/`, not theme roles): `--success` `#00cc55` indicator with text `#0f7a38` / dark `#56d364`; **process-shell tints** (R1 §6) for band and card headers — running `#eff9fd`, needs-you `#fff4e0`, done `#e8f7ec` (the mint panel), failed `#fdefef`, idle `#f7f7f7`; dark flattened over `#141918` by the generator (R5 risk 4 — flatten per surface, never reuse a light alpha). Teal `#00ccbb` is identity only: mark, project pill tint `#00ccbb21`, connected dots, favicon; never text (2.03:1 on white, R5 §3).

## Type

Roboto 400/500/700 self-hosted, `font-display: swap`, `--font-sans` in `index.css:139-144` + `appearanceFonts.ts:20-26`; mobile swaps DM Sans → Roboto in `app.config.ts:241-264` and `global.css:215-217` (R5 §5). Scale (web): project name 20/500 with `:port` at .6; thread title 15/500; body 15/400 (T3's current density stays — Zerops' 14 is for a console, not prose); rail row hostname 13/500, type 12/400 muted; card body 12/400 lh 1.7 (the GUI's process-step text); **micro-label 10/600 uppercase .06em** at .7 opacity — the GUI's 8px status label (`status-icon-base.component.scss`) is raised to 10px; 8px fails on high-DPI laptops and is the one GUI habit not worth keeping. Mono: keep T3's stacks for diff/terminal (`appearanceFonts.ts:25-26`); hostnames are Roboto 500, not mono, exactly as the GUI sets them.

## Shape

`--radius` stays 0.625rem (= mat-card 10px); `--control-radius` stays 8px for inputs. Rules: primary/secondary **buttons are pills** (`rounded-full`, `ui/button.tsx` base class change); chips 10px with tints (green access, purple region, blue tag, white info-chip); cards and rail rows 8px; dialogs 16px (`ui/dialog-styles.ts`); the composer keeps its 22px shell and 16px strip (`index.css:993-1060`, hard-coded by design — do not touch); band 0px (it is a stripe, not a card). Depth by tint, not shadow: `--card` on canvas, white tiles inside cards; shadows only on popovers/dialogs (`dropdown-glass`, `dialog-glass` keep their existing blur budget, `index.css:270-355`). Selected row = 2px `#0077cc` outline (`menu-item-card.component.scss:17-23`) — used for the active thread row and the rail row a card points at.

## Status vocabulary (one table, both clients)

| Platform state (`SETTLED_ZEROPS_SERVICE_STATUSES`, `contracts/zerops.ts:67-77`) | Dot | Label | Motion |
|---|---|---|---|
| ACTIVE / RUNNING | green-400 `#66bb6a` | ACTIVE | none |
| READY_TO_DEPLOY | blue-300 `#64b5f6` | READY TO DEPLOY | none |
| STOPPED | orange-400 `#ffa726` | STOPPED | none |
| FAILED / ACTION_FAILED / CONTAINER_FAILED / REPAIR_FAILED | red-400 `#ef5350` | FAILED | none |
| DELETED | grey-400 `#bdbdbd` | DELETED | none |
| `transient: true` (anything else) | blue-200 `#90caf9` | status verbatim, lowercased, `_`→space | duty-cycled `status-pulse` (`index.css:145-268`) |

Band tones (`ZeropsStripTone`): idle grey `#f7f7f7`, active `#eff9fd` + one 12px spinner (`ZeropsLifecycleStrip.tsx:58` already), waiting `#fff4e0` + `#b26a00` text, done `#e8f7ec` + `#2e7d32`. Every dot has a label (CVD; the GUI never shows a bare dot, `05-project-detail.png`). Adoption: `mounted` → folder glyph + "mounted"; `zcp-self` → "Control Plane" chip; agent rows keep "AUTHORIZED" in `#2e7d32` with the haloed dot (`zagent-service-card.feature.scss`), unauthorized `#b26a00` pulsing duty-cycled.

## Iconography

lucide on web (`apps/web/package.json`), SF Symbols → Tabler on Android (`components/AppSymbol.tsx:86-189`); no Material Icons webfont (blocking font, layout shift — AGENTS.md perf). Zerops mark as an inline SVG component (4 paths, `libs/zui/src/logo/`), mono variant for the phone header. Provider marks from `libs/zui/src/*-mark/` as `currentColor` React/RN SVGs. The 87 service icons (`libs/zef/src/shared-assets/custom-icons/`) are optional 14px leading glyphs in rail rows; the GUI shows none on cards (R6 §2), so their absence is also faithful — ship without them first.

## Motion budget

Standard easing `cubic-bezier(.4,0,.2,1)`. Route/column enter 200–300ms translateY 4px (GUI's stagger, `outer-route-animation`). Band tone change: background 200ms. Rail row status change: dot colour 250ms, nothing else. In-flight: `steps()`-timed `status-pulse` only, one per row, none on the band beyond the spinner. Forbidden: infinite ripple/bokeh/shimmer/bounce (GUI leftovers, R1 §7), animated gradients, per-row spinners, the "Used N tools" live mask beyond T3's existing `live-activity-focus`. `prefers-reduced-motion` disables all of it (`index.css:2513`).

## Web
## Thread view (≥1440)

```
┌ sidebar 256 ──────────┬──────────────── thread ────────────────────────┬── project rail 280 ───┐
│ [Z] kanban-shop  ▾    │ kanban-shop / Build a kanban board     ⌘K ◫ ▯ │ ● ACTIVE  kanban-shop │
│ ⌕ Search        ✎ New │────────────────────────────────────────────────│ FULL ACCESS·EU CENTRAL│
│                       │ ⟳ developing kanbandev                  ● 2 ▸ │───────────────────────│
│ TODAY                 │────────────────────────────────────────────────│ RUNTIMES              │
│ ● Build a kanban board│  you  Vytvoř mi kanban.                        │ ● kanbandev  nodejs@22│
│   developing kanbandev│                                                │   ACTIVE · mounted    │
│ ○ Fix login redirect  │  ┌ PLAN ────────────────────────── confirm ✓ ┐│   ↳ kanbanstage ○ READY│
│   task complete       │  │ kanbandev nodejs@22  kanbanstage nodejs@22 ││ DATA                  │
│                       │  │ db postgresql@16                           ││ ● db  postgresql@16   │
│ YESTERDAY             │  └────────────────────────────────────────────┘│   ACTIVE      [Browse]│
│ ○ Add Redis cache     │  ┌ IMPORT ───────────────────── 3/3 finished ┐│ INFRASTRUCTURE        │
│                       │  │ ✓ kanbandev  ✓ kanbanstage  ✓ db           ││ ● zcp  Control Plane  │
│                       │  └────────────────────────────────────────────┘│   ACTIVE  [IDE] [Term]│
│                       │  agent  Infrastructure is up. Writing the app… │───────────────────────│
│                       │  ▸ Used 6 tools · 40s                          │ ⟳ zerops_deploy       │
│                       │  ┌ DEPLOY kanbandev ──────── build ▮▮▮▮▯ 80% ┐│   running · 0:42      │
│                       │  └────────────────────────────────────────────┘│ updated 3 s ago · live│
│                       │ ┌────────────────────────────────────────────┐ │───────────────────────│
│                       │ │ Ask for changes…                            │ │ Devices 2             │
│                       │ │ ✳ Claude Code ▾ · High ▾            (↑)     │ │ Data Console          │
│                       │ └────────────────────────────────────────────┘ │                       │
│ ⚙ Settings ☁ Devices  │  [Deploy kanbandev] [Show logs] [Add Redis]     │                       │
└───────────────────────┴────────────────────────────────────────────────┴───────────────────────┘
```

**Header** (`chat/ChatHeader.tsx`): breadcrumb project name (from the topology `project.name`, never the T3 project), thread title with rename; actions reduce to New thread, Cloud IDE (was `OpenInPicker` "Open in editor"), panel toggles. `GitActionsControl` ("Commit") is hidden on Zerops projects (D4: never two commit pipelines) — checkpoints/diff/restore stay.

**Band**: 28px, full thread width, tone tint, 12/500 text, right end shows the pending-question count and a chevron that focuses the rail (or opens the `zerops` panel below 1280). Content = `zeropsStripState(lifecycle, {pendingUserInput})` verbatim (`strip.ts`); unknown phase shown as itself.

**Timeline** (`chat/MessagesTimeline.tsx`): user bubble on `messageSurface` blue-tint (`#e3f4ff`), assistant on the card white; Zerops cards (`ZeropsToolCard.tsx`, kinds error/deploy/verify/import/mount/subdomain/plan per `zerops/cards/payloads.ts:30-85`) restyled as **process shells**: 8px radius, header stripe in the tone tint with a 10px uppercase kind label ("DEPLOY · kanbandev"), 17px step glyphs on a 30px grid (`process-step-state.component.scss`: running ▶ blue, done ✓ green, failed ! red, queued ◌ grey), body 12/400 lh 1.7, footer chips (URL chip = white info-chip with the globe; "Open pipeline detail" pill tinted `#c9f1fd`/`#c9fddd`/`#fdd3c9` by outcome). Undecodable → generic tool block (Z3F-7). Plan card carries Confirm / Change, which are *prompts* into the composer, not calls.

**Composer** (`chat/ChatComposer.tsx`): unchanged geometry; provider picker relabelled "coding agent"; send button on `--primary` blue. Above it, docked cards:

```
│  ┌ WAITING FOR YOU ────────────────────────────────────────────── 1 of 2 ┐ │
│  │ How should this deploy when the task closes?                          │ │
│  │ (1) ● auto     commit + push + deploy after every verified change     │ │
│  │ (2) ○ manual   I'll say when                                          │ │
│  │ (3) ○ Other…   _____________________________________                  │ │
│  │                                                     [Skip]  [Answer ↵]│ │
│  └───────────────────────────────────────────────────────────────────────┘ │
│  ┌ 🔒 CREDENTIAL · GIT_TOKEN for kanbandev ──────────────────────────────┐ │
│  │ Stored as a service secret on kanbandev. Never enters this thread.    │ │
│  │ [ •••••••••••••••••••••••••••••••••••••••• ]     [Store on kanbandev] │ │
│  │ Or set it in the Zerops GUI ↗                                         │ │
│  └───────────────────────────────────────────────────────────────────────┘ │
```

Question card = `ComposerPendingUserInputPanel.tsx` (digits, arrows, "Other", collapsible) in the `#fff4e0` needs-you tint. Credential card is new: lock header, masked field, one button, a receipt line in the timeline afterwards ("credential stored · GIT_TOKEN on kanbandev") with no value. Until the zcp verb exists the field is absent and only the GUI link renders — never a chat paste.

**Quick-action dock**: replaces `BranchToolbar` (`ChatView.tsx:7180`); white flat pills 12/500 (the mint panel's pills, `_c-button.scss`), contents from `zeropsQuickActions(topology)`, hidden when the feed is unavailable.

**Rail**: rows 44px, hostname 13/500 + type 12 muted, second line dot+label+chips; group headers 10/600 uppercase at .45. Row actions on hover/focus: Open ↗ (subdomain), Browse (Data → `data-console` kind), Logs, and on the zcp row Cloud IDE / Terminal / SSH. Clicking a row highlights the cards in the timeline that name that hostname (2px blue outline, 600ms). Between 1280 and 1439 the rail collapses to a 44px dot column (one dot per service, tooltip = row); below 1280 it is the existing right-panel kind `zerops` (`RightPanelTabs.tsx` add-menu, shortcut Z) and the band's chevron opens it.

**Right panel** (`RightPanelTabs.tsx`, `rightPanelStore.ts`): kinds diff / files / file / preview / terminal / pull-request / agents + new `data-console`; `zerops` remains only as the narrow fallback. Files tree grouped by service hostname (`/var/www/<host>`, spec §6.1). Diff grouped by service (multi-repo checkpoints, spec §6.4).

## Landing / picker (hosted)

```
┌──────────────────────────────────────────────────────────────────────────────┐
│  [Z] ZEROPS CODE                                                 Karel ▾  ⌘K │
│                                                                              │
│   Your projects                                          ⟳ Refresh   + New   │
│  ┌────────────────────────────────────────────────────────────────────────┐  │
│  │ CONNECTED                                                              │  │
│  │ ● kanban-shop    zcp · EU CENTRAL    2 threads · developing kanbandev  [Open] │
│  │ ● blog           zcp                 task complete                     [Open] │
│  ├────────────────────────────────────────────────────────────────────────┤  │
│  │ READY TO CONNECT                                                       │  │
│  │ ● api-lab        zcp · before Zerops Code          [Enable Zerops Code]│  │
│  │ ◐ shop-v2        zcp · restarting · 12 s                               │  │
│  ├────────────────────────────────────────────────────────────────────────┤  │
│  │ PREPARING                                                              │  │
│  │ ◌ newproj        creating the container · 40 s of ~5 min   Wait for it │  │
│  ├────────────────────────────────────────────────────────────────────────┤  │
│  │ NOT AVAILABLE                                                          │  │
│  │ ○ legacy         project stopped                                       │  │
│  └────────────────────────────────────────────────────────────────────────┘  │
│   Open in the Zerops GUI ↗                          Connect another device → │
└──────────────────────────────────────────────────────────────────────────────┘
```

The GUI's content-pane grammar (search field, "Created ↓" sort pill, card/list toggle, `zui-header-actions`) on a `max-w-[1000px]` container; rows are the GUI's service-stack cards flattened (`#f3f5f7`, 8px). Sign-in is the GUI login (`01-login.png`) restyled into `ZeropsLandingShell`: mark + ZEROPS mono wordmark, "Developer-first cloud platform", email/password, passkey, GitHub `#24292e`, GitLab `#9b51e0`, TOTP step, register with Turnstile or the app.zerops.io hand-off (Z3C-3). On a container origin the picker is skipped (single-project mode).

## Settings

`settings/SettingsSidebarNav.tsx` sections: General · Appearance (theme library keeps custom/VS Code import; "Zerops" is the default card, T3 Chat/Grove/… stay as options) · Keybindings · Coding agents (was Providers; accent swatch fallback changed from `#2563eb`, R5 §3) · Integrations · Source control (hidden on Zerops projects) · **Devices** (was Connections; labels `Zerops Code · <browser> on <os>`, "Connect another device") · **Account** (was `/settings/zerops`: email, orgs, sign out, Open Zerops GUI) · Archive.

## Command palette

`CommandPalette.tsx` styled as the GUI's palette (`modules/feature/command-palette`: 16/18 padding, hairline, scope chip teal-tinted, 40px rows, key chips). Added intents: "New thread in <project>", "Open project…", "Toggle project rail", "Open Data Console for <db>", "Open <host> ↗", "Show logs for <host>" (prefill), "Cloud IDE". Removed: worktree/checkout/pairing intents.

## Desktop
Desktop is the same bundle in Electron and, per D6, one smoke build — Rail changes nothing structural there. What it adds or changes:

- **Titlebar / WCO**: the 52px `--workspace-topbar-height` and `.wco` overrides (`index.css:79-137`) keep working; the rail must respect `--workspace-controls-right` so it does not sit under the traffic lights when the sidebar is collapsed (`workspaceTitlebar.ts`). Window background and titlebar symbol colours in `apps/desktop/src/window/DesktopWindow.ts:34-35, 128-130` (`#0a0a0a`/`#ffffff`, `#1f2937`/`#f8fafc`) become generated from ZEROPS_THEME (`#0c0f0e`/`#eceff3`, `#e9eeec`/`#1a1a1a`) via the shared `brandColors.ts` R5 §4 proposes, so the pre-paint frame matches the canvas.
- **Naming**: `APP_BASE_NAME` in `apps/web/src/branding.ts:20-27` and `apps/desktop/src/app/DesktopEnvironment.ts:88-110` → "Zerops Code"; stage label keeps the pill pattern (Dev/Nightly/Latest). `t3code://app` / `t3code-dev://app` origins stay allowlisted (spec §2.4) — the scheme rename is out of scope per D6.
- **Icons**: a Zerops `.icon` Icon Composer project per channel in `assets/{dev,nightly,prod}/app-icon.icon` (`scripts/lib/brand-assets.ts:1-32`), else `icons:check` fails (R5 risk 8). Mark on a teal `#00ccbb` tile light / mint on `#0c0f0e` dark; nightly variant desaturated, dev with the stage glyph.
- **Remote-only build**: on Zerops the server runs in the container, never on the laptop, so the desktop's "host a server / allow remote connections" mode (AGENTS.md "The desktop app can also be used as the host server") is dropped from the Zerops build: no local server runner, no pairing URL, no `T3 Connect`. The desktop is a thin shell whose only job is native windows + the Browser preview kind (`PreviewPanel`, "Only available in the desktop app" on `03-right-panel-picker.png`) — which is the one reason to keep it: previewing a deployed subdomain beside the thread.
- **Multi-window**: one window per project is the natural desktop shape (one environment per window); the project pill's "Open in new window" is the only desktop-specific menu item.
- **Honest note**: the rail costs ~280px; on a 13" MacBook at default scaling the window is 1440 wide and the rail fits only with the sidebar collapsed or the right panel closed. The dot-column fallback is what most laptop users will see with a diff open. That is the price of Angle C and it should be said in the release notes rather than hidden.

## Mobile
## What the phone is for

Glance, answer, nudge. Not a console. The three things a person does with an agent from a phone: see whether it is still working / stuck / waiting, answer the question it asked, and send the next sentence. The rail therefore does not exist on the phone; its two jobs are split — the **strip phrase becomes the subtitle of every thread row and the header of the thread**, and the **map becomes a form sheet** pulled up from the project chip. Cards stay in the feed. Nothing else from Angle C carries over; a persistent rail on a 390px screen would be a lie about the device.

There is no Zerops mobile precedent (`viewport width=1200`, `12-mobile-dashboard.png` is a scaled desktop), so the phone is the one place the concept defines Zerops rather than borrows it. The rule: **Zerops palette, typography, status vocabulary and copy; native shapes and navigation.** Keep iOS 26 Liquid Glass headers and composer, form sheets with detents, UIMenu, SF Symbols, haptics, the Mail search toolbar (R3 §4 "must stay native"); do not port Material pills onto iOS. Android keeps the token-styled in-flow chrome (`AndroidScreenHeader`, `AndroidHomeFab`).

## Screens (native-stack, flat thread routes kept, `src/Stack.tsx:229-233`)

| Screen | Change |
|---|---|
| Sign in | new: Zerops email/password → TOTP; registration via a Turnstile WebView or the app.zerops.io hand-off (spec §4.3); reuses `SettingsAuthRouteScreen` slot |
| Projects | new: the picker as a grouped list (Connected / Ready to connect / Preparing / Not available), row actions Open / Enable Zerops Code / Wait; replaces `ConnectOnboardingRouteScreen` |
| Home | `HomeScreen.tsx`: header = project pill (mark + name ▾) with the project status line under it; thread rows gain the strip phrase + tone dot as subtitle (`ThreadListRow`); FAB / + = New thread |
| Thread | `ThreadDetailScreen.tsx`: header subtitle = strip phrase (glass header, `GLASS_HEADER_OPTIONS`); Zerops cards in `ThreadFeed`; question card already **replaces the composer** (`ThreadDetailScreen.tsx:700-730`) — that is the docked-at-composer pattern, native; credential card same slot; quick-action chips above the composer when idle |
| Project sheet | new form sheet (detents [0.55, 0.92]) = the map: groups, rows with dot + label, row actions (Open ↗, Browse data, Logs); running-tool line and feed line at the bottom |
| Data Console | full-screen modal WebView on the container origin (read paths), from a Data row |
| Settings | Account (Zerops), Devices (was Environments/Connections; "Connect another device" = the existing QR/token flow in `ConnectionsNewRouteScreen.tsx` with the word pairing removed), Appearance, Archive |

Live Activity / Dynamic Island: the strip phrase is exactly the right payload — "developing kanbandev" → "waiting for you" with the tone colour (`widgets/AgentActivity.tsx` phase tints routed through tokens). This is the one mobile feature the desktop cannot match and it is free given the feed.

## Wireframes

Home:
```
┌──────────────────────────────────────┐
│ [Z] kanban-shop ▾           ⌕   ◎    │
│ ● ACTIVE · 3 services                │
├──────────────────────────────────────┤
│ TODAY                                │
│ ● Build a kanban board          2m   │
│   developing kanbandev               │
│ ▲ Fix login redirect            1h   │
│   waiting for you                    │
│ ○ Add Redis cache               3h   │
│   task complete                      │
├──────────────────────────────────────┤
│ YESTERDAY                            │
│ ○ Set up staging                1d   │
│   kanbanstage deployed ✓             │
│                                      │
│                            ( + )     │
└──────────────────────────────────────┘
```

Thread:
```
┌──────────────────────────────────────┐
│ ‹  Build a kanban board       ◎  ⋯   │
│    ⟳ developing kanbandev            │
├──────────────────────────────────────┤
│             ┌──────────────────────┐ │
│             │ Vytvoř mi kanban.    │ │
│             └──────────────────────┘ │
│ ┌ PLAN ─────────────── confirmed ✓ ┐ │
│ │ kanbandev · kanbanstage · db     │ │
│ └──────────────────────────────────┘ │
│ ┌ IMPORT ──────────── 3/3 finished ┐ │
│ │ ✓ kanbandev ✓ kanbanstage ✓ db   │ │
│ └──────────────────────────────────┘ │
│ Infrastructure is up. Writing the…   │
│ ▸ Used 6 tools · 40s                 │
│ ┌ DEPLOY kanbandev ──── ▮▮▮▮▯ 80% ┐ │
│ └──────────────────────────────────┘ │
├──────────────────────────────────────┤
│ [Deploy] [Show logs] [Add Redis]     │
│ ╭──────────────────────────────────╮ │
│ │ Ask for changes…           (↑)   │ │
│ ╰──────────────────────────────────╯ │
└──────────────────────────────────────┘
```

Project sheet (map + strip):
```
┌──────────────────────────────────────┐
│                ━━━━                  │
│ kanban-shop             ● ACTIVE     │
│ EU CENTRAL · FULL ACCESS             │
├──────────────────────────────────────┤
│ RUNTIMES                             │
│ ● kanbandev   nodejs@22    ACTIVE    │
│   mounted · Open ↗                   │
│ ○ kanbanstage nodejs@22    READY     │
│ DATA                                 │
│ ● db          postgresql   ACTIVE    │
│   Browse data ›                      │
│ INFRASTRUCTURE                       │
│ ● zcp   Control Plane      ACTIVE    │
│   Cloud IDE ↗ · Terminal ›           │
├──────────────────────────────────────┤
│ ⟳ zerops_deploy running · 0:42       │
│ updated 3 s ago · live               │
└──────────────────────────────────────┘
```

## Tablet

iPad split view already exists (`features/layout/AdaptiveWorkspaceLayout.tsx`, `ThreadNavigationSidebar.tsx`, `workspace-inspector-pane.tsx`). The rail returns here as the **inspector pane** on the right of the thread (same content as the web rail, same 280pt), the sidebar lists threads, the project pill sits in the sidebar header. Landscape iPad = the web thread view; portrait = phone thread with the sheet. This is the cheapest place to prove the rail on touch.

## Honest gaps

Mobile has zero Zerops code on `main` (R3 §6); everything above is downstream of S5 (identity bootstrap through `packages/client-runtime/src/zerops/*`) and of promoting `apps/web/src/zerops/{strip,serviceMap,cards,quickActions}.ts` to `client-runtime` first. The composer caret stays iOS blue unless `T3ComposerEditorView.swift:763` is edited. Turnstile on a WebView is unproven.

## Cross-client system
## One source

`ZEROPS_THEME: ThemeDefinition` (light + `variants.dark`, `sidebarArtwork: false`) appended to `BUILT_IN_THEMES` in `packages/shared/src/themePalettes.ts` and made the **default on both clients** through the paths that already exist (R5 option a): web `applyThemePalette` → `--app-theme-*` → `index.css:1552-1617` remap; mobile `createMobileThemeVariables` (`apps/mobile/src/lib/mobileTheme.ts:208-283`) → `scripts/generate-uniwind-themes.mts` → `@variant zerops-light/dark`. Then a root `scripts/generate-theme-tokens.ts` (mirrors `icons:export`/`icons:check`, root `package.json:19-20`) renders the six hand-synced copies from it and the hand-written defaults are deleted: `index.css :root` / `@variant dark` (l.1388-1494), `index.html` boot palettes + `SPLASH_COLORS` + `<meta theme-color>`, `themePalette.ts` `T3_CODE_*` mirrors, `themePreview.ts` `STANDARD_THEME_PREVIEW_COLORS`, `apps/mobile/global.css` light/dark block, `DesktopWindow.ts` colours, `manifest.webmanifest`. `MOBILE_DEFAULT_THEME_ID` → `zerops`; `getMobileUniwindThemeName` (`mobileThemeRuntime.ts:41-46`) returns `zerops-<appearance>` for the default. Fallback for omitted roles (`getDefaultThemeColors`, `themePalette.ts:1224`) points at ZEROPS_THEME, never T3 Chat (R5 risk 5 — no pink flash).

## Shared vs per-client

**Shared (`packages/shared`, `packages/client-runtime`, `packages/contracts`)**: colour roles; the status-vocabulary table as data (`zeropsStatus.ts`: platform status → tone + label + transient flag; both clients render from it); strip phrases (`strip.ts` promoted from `apps/web/src/zerops/`); card decoders (`cards/payloads.ts` + `decode.ts`); quick actions; service-map view model (`serviceMap.ts` grouping, pair folding, production links); candidate/provisioning/readiness logic (`candidates.ts`, `provisioning.ts`, `containerHealth.ts`, `firstPrompt.ts`); brand SVG path data (mark, mono mark, provider marks) as plain strings rendered by each client's SVG primitive; copy strings for the Zerops surfaces (one `zeropsCopy.ts`, so "Enable Zerops Code", "waiting for you", "restarting" are spelled once).

**Per-client**: every shape. Web: pills, 8px cards, band, rail, glass composer geometry, lucide. iOS: Liquid Glass, form sheets, SF Symbols, native header items, UIMenu. Android: in-flow token-styled chrome, Tabler, FAB. Desktop: window colours, WCO insets, icons. Fonts: Roboto self-hosted (web) / bundled (mobile) — same family, separate loading.

## Drift guards

1. **Byte-equality generator test** (exists for mobile: `generate-uniwind-themes.test.ts:13-25`, `--check` flag) extended to every generated file from the root script; runs in the CI lint job the way `mobile-fingerprint-check.yml` does.
2. **Role coverage + contrast assertions** on ZEROPS_THEME in `packages/shared`: `Object.keys(colors) ⊇ THEME_COLOR_ROLES` for both appearances; ≥4.5 for text/canvas, text/surface, messageActionForeground/messageAction, accentSurfaceForeground/accentSurface, error/warning/update foreground-on-surface pairs; ≥3.0 for focus/canvas; every value opaque (R5 §4, using `themeContrastRatio`, `themePalette.ts:957`).
3. **Vocabulary lint**: a test over `apps/{web,mobile}/src` user-facing strings forbidding "T3 Code", "T3 Connect", "Tailscale", "pairing", "worktree", "Local checkout", "environment" (allowlisted identifiers and `t3code:` storage keys excluded, D6 out-of-scope) and "control plane" as a self-description (R6 §3.1).
4. **Status-vocabulary parity**: both clients' status renderers take `zeropsStatus.ts` as input; a table test asserts every `SETTLED_ZEROPS_SERVICE_STATUSES` entry plus the transient branch has a tone and a label in both.
5. **No-mutation guard** stays (`ZeropsQuickActions.test.tsx` "cannot reach Zerops or the RPC layer at all") and is copied for the rail, the band, and the credential card (which may reach one server RPC only, the zcp verb).
6. **Perf guard**: a CSS lint over `index.css` and the zerops component folder for `infinite` keyframes without `steps()` timing, and a rule that no rail row mounts a `Spinner`.

## Visual regression harness

Web: a fixed-fixture route (`test-t3-app` already exists for the integrated pass, AGENTS.md "Verifying") rendering the thread view, landing/picker, docked cards, rail states (empty / degraded / restarting) in light and dark at 1280 / 1440 / 1920, screenshotted with the existing puppeteer tooling and compared with a perceptual diff against committed baselines under `apps/web/test-baselines/` (PR evidence, not committed assets — AGENTS.md forbids `.github/pr-assets/`; baselines are test fixtures, which is allowed). Mobile: `scripts/mobile-showcase.ts` (captures Home / Thread / Terminal / Review / Settings per theme id, `docs/operations/mobile-app-store-screenshots.md`) gains Projects, Project sheet, and Thread-with-question; the showcase theme list defaults to `zerops`. Desktop: one screenshot of the WCO titlebar per channel from the smoke build.

## Migration
Effort classes: S = days, M = one to two weeks, L = a stream. R2/R3/R5 tracks cited.

**P0 — Skin and words (S–M, nothing throwaway).** ZEROPS_THEME as default on web and mobile through the existing theme path (R5 step 1; R2 track a: `themePalettes.ts`, `themePalette.ts` reserved ids + `T3_CODE_*`, `index.html` boot copy, `index.css` `:root`/dark, `ThemePreviewCircles.tsx`, `manifest.webmanifest`; R3 tier A: `global.css` 65×2, fonts in `app.config.ts:241-264`, `withAndroidModern{PopupMenu,AlertDialog}.cjs`, splash/adaptive colours). Roboto self-hosted. Pill buttons via `ui/button.tsx` base class (R2 track c, cheap first pass). Brand touchpoints (R2 track b ~15 files; R3 tier B ~40 files): `branding.ts`, `DesktopEnvironment.ts`, `SidebarChrome.tsx` wordmark, splash, favicons, `AuthSurfaceShell` gradient, `BrandMark`/`T3Wordmark`/`CompactBrandTitle`, Live Activity title. Vocabulary sweep with the lint from the cross-client section; Settings → Connections becomes Devices, Providers becomes Coding agents, `/settings/zerops` becomes Account. Provider accent fallback swatch off `#2563eb`. Ships as a visible win on the first day and is required by every later phase.

**P1 — Band, rail, dock, cards (M).** The structural core of Angle C, web only. Band: rewrite `ZeropsLifecycleStrip.tsx` from a header button into a full-width stripe rendered under `WorkspacePageHeader` in `ChatView.tsx` (~6906-6928); sidebar rows get the strip subtitle (`Sidebar.tsx` row renderer + `Sidebar.logic.ts` status colours routed through `zeropsStatus.ts`). Rail: promote `ZeropsPanel`/`ZeropsServiceMap.tsx` out of the right panel into a new `ProjectRail.tsx` column mounted in `AppSidebarLayout.tsx`/`ChatView.tsx` with the 1440/1280 breakpoints; `rightPanelStore` kind `zerops` stays as the narrow fallback (R2 track d, partial). Dock: replace `BranchToolbar` at `ChatView.tsx:7180` with `ZeropsQuickActions` on Zerops projects. Cards: restyle `ZeropsToolCard.tsx` `CardFrame` into process shells, decide the fold escape in `MessagesTimeline.tsx:2605` (plan/import/deploy/verify/error out, mount/subdomain in). Header: hide `GitActionsControl`, relabel `OpenInPicker` to Cloud IDE. Throwaway: the `bg-card/20 rounded-xl border-border/55` ad-hoc frames on every Zerops component (R2 §5) go; the `/zerops` route (`ZeropsProjectsPage.tsx`) folds into the picker in P2.

**P2 — Single product entry (M).** Origin-aware gate: a Zerops branch of `requires-auth` in `routes/__root.tsx:62-83` keyed on the z3 descriptor, identity connect against `window.location.origin` (`connectZeropsIdentity` already takes `httpBaseUrl`), `resolveChatIndexView` widened, `clientMetadata.ts:80-94` labelled on Zerops rather than `hosted`, Turnstile hostname allowlist or the app.zerops.io hand-off on container origins (R2 §5 inferred change set — verify against spec §3.2/§4.3 before shaping). Picker restyle into the GUI content-pane grammar with row-state provisioning and "restarting · N s" (`ZeropsProjectPicker.tsx`, `ZeropsProvisioningPanel.tsx`, `useZeropsProvisioning.ts`). Palette intents (`CommandPalette.tsx:476-478` pattern). `ManualConnectFallback` kept reachable from Devices. Risk sits in auth core; this phase is gated on a live retest on `z3-eval`.

**P3 — Docked cards and hand-offs (L).** Question card docked in the needs-you tint (`ComposerPendingUserInputPanel.tsx` — its current mount site relative to the composer was not traced in this pass and must be before design). Credential card: new component + one server RPC + the zcp verb (S6 "design it with the owner"); until the verb lands the card renders only the GUI deep link. `data-console` right-panel kind (embed on the 8080 origin; read the embed contract in `docs/spec-dataconsole.md` first). Rail row actions (Open, Browse, Logs, Cloud IDE, Terminal, SSH). Files tree grouped by hostname, diff grouped by service (depends on S3 git).

**P4 — Mobile (L, S5 + S6 mobile).** First promote `apps/web/src/zerops/{strip,serviceMap,cards/*,quickActions,candidates,provisioning,containerHealth,firstPrompt}.ts` to `packages/client-runtime` (all UI-free TS, R3 §6). Then: sign-in/TOTP/registration WebView, Projects screen, Home subtitles, Thread header subtitle, cards in `ThreadFeed`, project sheet, Data Console modal, Devices, Live Activity payload (R3 tier D). Roboto + token routing of the 155 literals worth routing (`threadPresentation.ts`, `ConnectionStatusDot.tsx`, `AgentActivity.tsx`; terminal ANSI and shiki stay). iPad inspector pane = rail.

**P5 — Desktop smoke (S).** `DesktopWindow.ts` colours from the generator, `.icon` projects per channel, `APP_BASE_NAME`, drop the host-server mode from the Zerops build, one WCO screenshot per channel.

**P6 — Generator and deletion (S–M).** Root `generate-theme-tokens.ts`, delete the six hand-written copies, drift tests in CI (R5 option a steps 2–3). Deliberately last: it is hygiene, and P0 already works without it.

Order rationale: P0 first because it is cheap and every screenshot after it stops looking like T3; P1 before P2 because the rail is the thesis and can be judged on `z3-eval` without touching auth; P3 after P1 because docked cards need the band's "waiting for you" and the credential verb needs the owner; P4 last because it is blocked on S5 and on the promotion.

## Risks
1. **Horizontal budget — the angle's weak point.** Sidebar 256 + thread min 640 (`threadSidebarWidth.ts`) + rail 280 + a diff panel does not fit 1440, let alone 1280. Mitigation: three rail states (full ≥1440, 44px dot column 1280–1439, right-panel kind below) with the band carrying the phrase in all three; say plainly that on a 13" laptop with a diff open "always in view" means "a column of dots with tooltips". If that is judged too weak, the alternative is folding the map into the sidebar under the project pill — cheaper, but it turns the sidebar into the GUI's 480px column and loses thread density.

2. **A live rail competing with the thread for attention.** Mitigation: the rail is tone-on-tone and nearly still — dot colour changes at 250ms, one duty-cycled pulse per transient row, no counters, no per-row spinners, the running-tool line is the only element that moves; the band is the only tinted stripe on screen.

3. **Unfolded cards flooding the timeline.** Twenty `zerops_*` calls in a turn would bury the prose. Mitigation: only milestone kinds escape the fold (plan, import, deploy, verify, error); mount/subdomain stay inside "Used N tools" but are named in its header ("Used 6 tools · mounted kanbandev · subdomain on"); the product call in `verified.md` is made explicitly rather than by default.

4. **Feed liveness is unproven (Z3F-8).** A rail that lags is worse than a tab you open on demand. Mitigation: the doorbell tri-state is always rendered as the footer line ("updated N s ago · live | polling | last read failed"); the 90 s nudge window at 3 s (spec §5.1) covers the post-tool settle; a service created in the GUI is the S6 acceptance test and the rail is where it must appear.

5. **Blue action vs blue info vs teal brand vs orange warning vs Claude orange (R5 §3).** Mitigation: blue is action and selection only; the running tint `#eff9fd` is the sole informational blue and never carries blue text; teal is identity (pill, mark, connected dots) and never text; warning is a filled badge with a word; the Claude mark stays the asterisk glyph in `currentColor`. Every dot has a label.

6. **The single-bundle gate touches auth core.** `__root.tsx`, `hostedPairing.ts`, `_chat.tsx` redirects; a mistake breaks the manual one-time-token fallback that must stay reachable outside Zerops projects. Mitigation: P2 is its own phase, gated on a live retest with both a container origin and the hosted origin, and the fallback is a test ("outside a Zerops project nothing changes").

7. **Credentials.** The card exists to keep secrets out of the conversation, but the zcp verb is not designed and zcp's `appendCredentialContract` today tells the agent to ask the user — under z3 that would land the token in chat. Mitigation: until the verb ships the card renders only the GUI deep link, and `ZCP_HARNESS=z3` (brief §8.2) makes zcp emit the credential-class ask as a card, never a question.

8. **Mobile is designed on paper.** Zero Zerops code on `main`, no screenshot of the native app exists, Turnstile-in-WebView unverified. Mitigation: the phone concept is deliberately thin (subtitles, a sheet, cards, Live Activity) and every piece rides on logic promoted from web first, so the first mobile build proves the promotion before it proves the design.

9. **Performance.** Roboto webfont, rail re-renders, pulse animations. Mitigation: font preloaded with `swap`; topology publishes only on change (spec §5.1) and rows are memoised by hostname+status; `steps()` keyframes only; no Material Icons webfont; the band and rail add zero `backdrop-filter` surfaces.

## Signature moments
- The band flips. 'setting up infrastructure · import' turns into 'infrastructure ready · 3 services' as the three rail rows settle from pulsing blue 'creating' to green ACTIVE one after another — the GUI's process steps and the thread agreeing in the same second, without a click.
- The deploy card grows a URL chip and, in the same frame, the kanbandev rail row gains 'Open ↗' and the verify card prints 'HTTP 200 ✓' with two green checks — three surfaces, one fact.
- A question docks at the composer in the needs-you orange, the band says 'waiting for you', the sidebar row says it too, and the phone's Live Activity says it — the user answers by pressing 2, everywhere.
- The credential card: a lock, 'Stored as a service secret on kanbandev. Never enters this thread.', one button — and afterwards a timeline receipt with no value in it.
- On the phone, every thread row's subtitle is the strip phrase; pull up the project sheet and the same dots and uppercase labels the desktop rail shows are there, with 'updated 3 s ago · live' at the bottom.
- 'Enable Zerops Code' pressed in the picker: the row itself counts 'restarting · 12 s' through the 502 window and slides from Ready to connect into Connected — a restart shown as a calm state, not an error.
