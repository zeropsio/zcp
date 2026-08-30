# Zerops Code (z3) — UI redesign concept: one map, one phrase, three shells

Date: 2026-08-29. Owner: Karel. Status: **concept — nothing implemented**. Research, analysis and
a proposal for redesigning the z3 clients (web · desktop · mobile) so they look and feel like
Zerops, as one coherent system. Read this file first; the evidence behind every claim lives in
`plans/z3-ui-redesign-2026-08-29-research/` (seven research reports, four competing proposals,
three judge verdicts, the synthesis, the completeness critic) and in the screenshot set at
`~/.zerops-dev/z3-ui-redesign-2026-08-29/shots/` (outside the repo: 9.6 MB of PNGs).

This is a plan: transient, never a source of truth. What survives goes to `docs/spec-z3.md`
(a new "client design system" section) and to tests, per `fork.md §5`.

---

## 0. Summary — what to decide and why

**The thesis in one line:** the project's state never scrolls away, the service map is a
first-class surface, and one lifecycle phrase says the same thing on every shell at the same
instant — inside T3's proven working shell, painted in the Zerops grammar.

**Five decisions carry the concept:**

1. **Zerops is a theme first, a shell second.** z3 already has a 57-role theme engine that
   feeds web *and* mobile from one `ThemeDefinition` (`packages/shared/src/themePalettes.ts`).
   The only thing not driven by it is the *default* look, hand-written on both clients. A
   `ZEROPS_THEME` as the default, plus a small set of fixed brand tokens, gives every client the
   Zerops canvas, cards, blue action colour and graphite-green dark mode through machinery that
   exists today — no new package, no new build step for phase one.
2. **State goes vertical, not horizontal.** A 28 px full-width **lifecycle band** under the
   header carries the pinned strip phrase (`● DEVELOPING KANBANDEV`, `○ WAITING FOR YOU`,
   `✓ TASK COMPLETE`) at zero horizontal cost. The 280 px "always-in-view rail" one proposal
   wanted does not fit a 1440 px laptop next to a diff panel (verified arithmetic, §3.3).
3. **The service map opens by default** on a Zerops project (remembered per thread) and
   degrades by a container query on the main column, not a fixed 980 px viewport query. The
   GUI screenshot shows the services column is the *first* thing a Zerops user sees; leaving
   it one keystroke away was called fatal by all three judges.
4. **T3's thread list (`Sidebar.tsx`, 3960 lines) stays whole.** Two surgical edits — the
   Zerops mark pill + ⌘K pill in its header, and `● dot + phrase` as every row's second line —
   make it read as the GUI's left column without demoting cross-project thread browsing, the
   daily surface of an agent tool. The alternative (rebuild the GUI's 480 px project card
   column) has the highest brand fidelity and the highest regression risk; it is **D1** for you.
5. **Recognition does not live under `zerops/**`.** The four surfaces that carry the "this is
   Zerops" signal — sidebar header, `ui/*` primitives, the composer's needs-you tone, the band
   — are outside the directories S4/S6 own. So phases 0–1 make the product unmistakably Zerops
   while those streams keep merging cleanly; the Zerops-specific surfaces (map, cards, picker,
   landing) are restyled last, in one phase, from primitives that already changed.

**What was measured, not assumed:** the Zerops GUI's live tokens in both themes (light and dark,
381/406 custom properties, 210 differ); the z3 client running locally with a real agent turn;
the live container origin (`/z3/` today shows T3's "Pair with this environment" — the Zerops
landing exists only in the separately hosted build); and every file:line the concept cites,
checked by a judge panel and an adversarial verification pass (§10).

**What it costs, honestly:** Roboto self-hosted; a theme generator with `--check` in CI
(bigger than it looks — six heterogeneous outputs, one single-target precedent); Icon Composer
`.icon` projects per channel (macOS desk work); and a mobile client with **zero Zerops code on
`main`** — every phone screen here is a specification downstream of promoting eight pure-TS web
modules to `packages/client-runtime`.

**What you must decide before anything starts:** D0 (evolve `apps/web` vs the "new UI app"
`fork.md` reserves), D1 (sidebar stays vs project column), D2 (which cards escape the fold),
D3 (map default-open) — §9.

---

## 1. Method and evidence

| Step | What | Where the output is |
|---|---|---|
| Live GUI | Logged into app.zerops.io with the non-member test account (credentials read from `~/.zerops-dev/accounts/nonmember.env`, never printed), captured dashboard / project / service / envs / settings / recipes in light and dark, at 1440 and 390 wide; dumped every CSS custom property in both themes via `getComputedStyle` | `shots/gui-nonmember/*.png`, `research/zerops-gui-theme-tokens-computed.json`, `research/R7-zerops-live-tokens.md` |
| Live z3 origin | Opened `https://zcp-26a7-8080.prg1.zerops.app/z3/` | first screen = T3 "Pair with this environment" (§4.1) |
| Local z3 client | Ran the fork's dev server (`node scripts/dev-runner.ts dev`), added a throwaway project, ran one real Claude turn, captured home / thread / tools / diff / files / terminal / palette / model picker / settings / dark / 390 px | `shots/t3-live/*.png` |
| Code readers | Six parallel readers: GUI design language (R1), z3 web (R2), z3 mobile (R3), z3 desktop (R4), cross-client tokens (R5), brand + naming + product constraints (R6); a completeness critic with five gap fills | `research/R1…R7.md`, `research/critic.md` |
| Design panel | Four independent concepts (skin-first, project-first IA, agent console, system-first), three judges (Zerops designer, z3 engineer, first-time user), one synthesis | `research/proposals/*.md`, `research/judges.md`, `research/synthesis.md` |
| Verification | Two skeptics per load-bearing claim (code lens, spec lens), an arbiter on disagreement | §10 |

Judge totals (sum of three): system-first 143 · project-first 135 · skin-first 132 ·
agent-console 125. Every judge verified at least three concrete claims per proposal against the
code; the fatal flaws they found (the rail's width arithmetic, the sidebar rewrite's feature
regression, the skin's hidden map, the system's front-loaded infrastructure) shape §3–§8.

---

## 2. Findings

### 2.1 The Zerops design DNA (R1, R7)

Five traits make a screen recognisably Zerops; everything else is Angular Material residue.

| Trait | Fact (verified live / in `frontend-legacy`) |
|---|---|
| Depth by tint, never shadow | canvas `#eceff3` → card `#f3f5f7` → white tiles; `.mat-card { box-shadow: none }`; the grey-tinted triple shadow only on popovers/dialogs |
| Micro-label grammar | 8–10 px uppercase 500/600 tracked labels beside 20 px/500 names; hostname `zcp:8080` with `:port` at .6 opacity; `.u-desc` 13 px / 1.75 / .7 |
| Status = dot + tiny uppercase word | `● ACTIVE`; pulse/ripple = in flight; Material 400-level hues per platform status; the GUI never shows a bare dot |
| Pills everywhere | 40 px teal-tinted org pill, ⌘K pill (radius 80), sort pill, white flat pills inside the mint `#e8f7ec` zcp panel; chips at 10 px radius (green FULL ACCESS, purple EU CENTRAL, white info-chips) |
| Blue acts, teal identifies | every CTA, link, focus ring and selected-card outline is `#0077cc`; teal `#00ccbb` only in the logo pill, badges, tints — **2.03:1 on white, never text**; dark theme is a graphite-green ramp with mint `#00e5c0` as the single accent |

Live dark values (authoritative, from the served page with `html.zef-dark-theme`): canvas
`#0c0f0e`, card `#141918`, raised `#1b2220`, overlay `#1e2624`, text `#e9eeec`, secondary
`#9faea9`, hover `rgba(255,255,255,.06)`, form border `1px rgba(255,255,255,.07)`, focus
`#5ab3ff`, active tag `rgba(0,229,192,.12)`, error surface `#312828`, warning surface `#453f36`,
**zcp mint panel dark `rgba(86,211,100,.14)`** (the SCSS-derived `rgba(76,175,80,.10)` in
earlier reports is superseded). Full sheet: `research/R7-zerops-live-tokens.md`.

Two things the GUI has that the product must not inherit: five infinite animations (pulse,
ripple, bounce, shimmer, bokeh — T3's `AGENTS.md` forbids continuously repainting animations)
and **no mobile layout at all** (`<meta viewport width=1200>`, zero app-shell media queries;
the "mobile" screenshots are a scaled desktop). There is no Zerops mobile precedent; §6 defines
one.

Continuity that must be kept: the mint **zcp panel** (four white pills *Web Terminal · SSH
(Terminal) · Cloud IDE · Desktop IDEs*, then the edge-to-edge agent tray with `✳ Claude Code ·
AUTHORIZED` in `#2e7d32` and a 3 px haloed dot) — the one card a Zerops user already has open
in the next browser tab. "Zerops Control Plane" is the container's name; z3 never calls itself
a control plane (T3's marketing line "the open-source control plane for coding agents" dies).

### 2.2 z3 today — web (R2)

Tailwind v4 + Base UI + cva primitives (`apps/web/src/components/ui/*`); ~60 semantic CSS vars in
`index.css` (light `:root` @1388, dark `@variant dark` @1451, sidebar palette @1494, contrast
derivation @1874). Layout constants: `--radius 0.625rem`, `--control-radius 0.5rem`, topbar 52 px
(WCO override), sidebar 256 / min 208, thread min 640 (`threadSidebarWidth.ts`), composer
`max-w-3xl` in a 22 px glass shell whose clip-path geometry is hard-coded by design. Right panel
kinds `diff | files | file | preview | terminal | pull-request | agents | zerops`, inline ≥ 981 px,
a Sheet below (`rightPanelLayout.ts:1`).

The theme engine: a `ThemeDefinition` (57 roles) is applied as `--app-theme-*` on `<html>` and
remapped onto the semantic vars under `html[data-theme-id]` (`index.css:1552-1617`; on web
`--primary ← messageAction`, `--ring ← focus`, `--accent ← accentSurface`). **The default "T3
Code" look is not a theme** — it is the bare `:root` literals, mirrored by hand in six places
(`index.html` boot palettes, `themePalette.ts` `T3_CODE_*`, `themePreview.ts`,
`appearanceFonts.ts`, `DesktopWindow.ts`, mobile `global.css`). Built-in themes T3 Chat / Grove /
Ocean / Ember / Iris, custom themes, VS Code theme import, contrast and glass sliders, font
pickers, per-provider accent swatches (first swatch `#2563eb` — indistinguishable from Zerops
blue). Hard-coded colour outside the token layer: 217 Tailwind palette utilities (hotspots
`ResourceTelemetryDiagnostics` 38, `Sidebar.tsx` 34, `Sidebar.logic.ts` 32,
`pullRequestPresentation` 21), the blue `AuthSurfaceShell` gradient, GitHub-alert colours,
thread status colours.

Zerops surfaces that exist on `main`: landing shell + hosted landing, project picker
(Connected / Ready to connect / Preparing / Not available), provisioning panel, lifecycle
strip (a button beside `ChatHeader`), service map (right-panel kind `zerops`, 58-line
`ZeropsPanel` + 149-line `ZeropsServiceMap`), tool cards (`ZeropsToolCard`, decoded totally,
mounted inside the collapsed tool group), quick actions (prefill only, test-pinned), Settings →
Zerops. All use ad-hoc `rounded-xl border-border/55 bg-card/20` frames. No credential card, no
Data Console embed.

Mobile-web already adapts: sheet sidebar < 768 px, composer collapse < 640 px with a
view-transition morph, right panel as a sheet ≤ 980 px, dialogs as bottom sheets.

### 2.3 z3 today — mobile (R3)

React Native / Expo, native-stack with flat thread routes (so iOS 26 header morphing works),
Liquid Glass headers and composer (`expo-glass-effect`), form sheets with detents, `UIMenu`, SF
Symbols with a Tabler map on Android, token-styled in-flow Android chrome. 65 semantic
`--color-*` tokens in `global.css` (light/dark, hand-authored) plus 57 generated adaptive
tokens; **named themes are generated from the same `ThemeDefinition` as web**
(`createMobileThemeVariables` → `generate-uniwind-themes.mts`, byte-equality drift test,
`--check` flag). 155 hex literals in 18 files (terminal ANSI, shiki, diff colours, iOS system
tints, Live Activity). Fonts DM Sans. **Zero Zerops code on `main`** — the POC's mobile
screens exist only at tag `poc-2026-08-28`; `packages/client-runtime` already exports the
Zerops API/session/registration/new-project half, imported by web 12× and mobile 0×.

### 2.4 z3 today — desktop (R4)

The web bundle in one Electron `BrowserWindow` at `t3code://app/`, a privileged scheme proxied
to the **local backend's** HTTP origin; the main window is created only after that backend
reports ready. A remote-only Zerops build therefore needs a new renderer source (packaged
`apps/web/dist` served from the scheme handler, or the hosted origin) and an unconditional
window open — a structural change, not feature hiding. Brand is hard-coded in three unlinked
places (`DesktopEnvironment.ts:88`, `build-desktop-artifact.ts:55,2144-2242`, the web wordmark);
`DesktopAppBranding.stageLabel` is a closed union `Alpha|Dev|Nightly`. Tailscale is already
removed; local spawn, WSL and SSH launch remain. macOS `hiddenInset` with a 90 px web inset;
Windows/Linux WCO overlay is 40 px tall against a 52 px web topbar. No release/signing workflow
exists in the fork.

### 2.5 Brand, naming, product constraints (R6)

The mark is a four-path SVG (`libs/zui/src/logo/`, teals `#3cbdb2` / `#00b1a3`), the wordmark
custom letter paths; docs ship a mono variant and a brand pack. Marketing uses Geologica for
display, Roboto for UI, JetBrains Mono for code; the GUI is Roboto + Roboto Mono + DejaVu Sans
Mono; the docs are Inter. Tone: short declarative sentences, second person, "developer-first"
as the one self-descriptor, anti-hype. 87 service-type SVG icons and seven `currentColor`
provider marks exist in `frontend-legacy` and are reusable.

Glossary the UI adopts (T3 word → Zerops word): environment → **project**; provider → **coding
agent**; pairing / pairing code → **Sign in with Zerops** / "Connect another device with a
one-time link"; Connections → **Devices**; worktree, Local checkout, T3 Connect, Tailscale →
gone; "Open in editor" → **Cloud IDE**; the `zcp` service → **Zerops Control Plane** under
Infrastructure; commit & push → zcp's pipeline, never the client's (D4). Footprint in the fork's
UI code: "T3 Code" 49 files, "pairing" 68, "worktree" 145 — a lint, not a sweep by hand.

Product constraints the UI must render (spec-z3 §2–§5, brief §5/§7/§8): one project per
environment; map groups Runtimes / Data / Infrastructure with per-row hostname, type, status
(settled set + `transient`), adoptionState, mounted, Open URL, dev↔stage pair, prod link; feed
availability as a tri-state (absent / degraded-with-last-good / doorbell down = one quiet line);
strip phrases pinned in `strip.ts`; cards as total decoders; question cards with Other and keys;
credentials never through the chat; quick actions prefill only; restart = upgrade ≈ 19 s with a
502 window rendered as "restarting"; identity sessions re-mint silently.

### 2.6 The bundle topology — settled here because nobody had named it

"Hosted" is overloaded across the brief, the spec and the code. The shapes that exist:

| Shape | Who serves it | Auth-gate state today | First screen today |
|---|---|---|---|
| **A · container-served** under `/z3/` on the project's 8080 origin (spec §2.4, what a user reaches from the GUI) | nginx → z3 server static dir (`apps/server/src/http.ts:378-435`), built by `cli.ts build/pack` with only `VITE_BASE_PATH` | `requires-auth` (`__root.tsx:62-83`; `isHostedStaticApp()` false — no `VITE_HOSTED_APP_CHANNEL`, origin ≠ `app.t3.codes`) | **T3 "Pair with this environment"** — verified live 2026-08-28 |
| **B · hosted static** at a Zerops URL (brief D6 "web is the product") | any static host, built with `VITE_HOSTED_APP_CHANNEL` | `hosted-static` → `ZeropsHostedLanding` when no environment | the Zerops landing (retest step 1 came from such a dev run, `verified.md:286`) |
| **C · desktop** `t3code://app` | Electron scheme proxy → today the local backend | `authenticated` via the local bootstrap | T3 home |
| **D · mobile** | Expo app | pairing screens | T3 home |

The concept requires **one Zerops-aware bundle keyed on the server, not the build**. The
verification pass (§10, claim 11) found this is *smaller* than the readers assumed: the server
already advertises `bootstrapMethods: ["zerops-identity", "one-time-token"]` inside a Zerops
container, the `requires-auth` state already carries that descriptor to the client, and
`ZeropsSessionProvider` already wraps the whole router. What walls the door off in shape A are
**two UI gates**: `PairingRouteSurface`'s method-blind copy (it renders the token box whatever
the descriptor says) and `resolveChatIndexView`'s hosted-static-only rule for the landing. Both
are UI-layer decisions the design directs; the only new logic is deriving the identity base URL
from `window.location` under `/z3/` (a few lines). Risks: the same two-status test is duplicated
in four route files (`_chat.tsx:186-194`, `settings.tsx`, `projects.$projectKey.tsx`,
`pair.tsx`) and must widen together; Z3C-6 (token in exactly one body field) must stay green.
Coordinate with S4, which owns those files. Two more shape-A facts from the fills: `cli.ts pack`
unconditionally bakes T3's favicon and apple-touch-icon into `dist/client`, and Turnstile's site
key is hostname-bound, so a `*.prg1.zerops.app` container origin is unlikely to be allow-listed —
registration in shape A hands off to app.zerops.io until the platform decides (D12).

### 2.7 Contradictions the readers surfaced, and how the concept resolves them

- **Teal has five values in circulation.** Canonical per use: mark `#3cbdb2` + `#00b1a3`;
  pill tint = `#00ccbb` at 13 %; dark accent mint `#00e5c0`; text-safe teal `#007e72` (on
  white only); `#00ccbb` and `#00a49a` never as text. A contrast test fails the build on teal
  foreground over a light surface.
- **The GUI's pulsing "unauthorized" dot vs the no-infinite-animation rule.** Resolved by the
  dot + word rule: `○ NOT AUTHORIZED` in `#b26a00` with a static halo, no pulse; in-flight
  states use the existing duty-cycled `steps()` keyframes only.
- **Cards live inside the collapsed tool group today** (retest step 4, "product call
  pending"). Resolved as D2 with a cheap in-idiom mechanism (§5.3).
- **Desktop SSH launch:** `fork.md §4` keeps it, brief S5 removes it. Owner decision (D11).
- **The S7 work on unmerged worktrees** (`wt/s7`, `wt/s7-web`: agent-auth snapshot, RPC,
  `ZeropsAgentAuthCard` mounted in the panel and the thread surface) is the z3 counterpart of
  the GUI's agent tray. The concept's mint panel *is* that card's home; the two efforts touch
  `ZeropsPanel.tsx` and the thread surface — coordinate before P2.
- **Accessibility / i18n:** no i18n layer exists in z3 or the GUI; copy changes are string
  edits; `aria-*` in 197 web files, `prefers-reduced-motion` honoured — the redesign keeps both.

---

## 3. Design principles — the DNA applied

| # | Rule for Zerops Code | What it costs |
|---|---|---|
| P1 | **Depth by tint.** Cards flat and borderless in light, 1 px `rgba(255,255,255,.06)` in dark; shadows only on popovers/dialogs; one `FlatCard` replaces every ad-hoc frame | the glass composer keeps its shadowed shell — it is chrome; accepted inconsistency |
| P2 | **One `MicroLabel`**: 10 px / 600 / uppercase / .06em / 45 % — group titles, status words, card kickers, chip text | deliberate deviation from the GUI's 8 px (unreadable on high-DPI laptops, impossible on phones); 11 px on mobile |
| P3 | **`StatusDot` + `MicroLabel`, never a bare dot** — `● ACTIVE`, `◌ CREATING`, `○ WAITING FOR YOU`; hue-only statuses are deuteranopia-confusable | pulse = the existing `steps(6)` `status-pulse` keyframes (`index.css:238-251`), never `infinite` opacity loops |
| P4 | **Pills** for primary/secondary buttons and every CTA; chips at 10 px; ghost/icon buttons keep 8 px | the shape, not Material's 17 px-on-38 px metric |
| P5 | **Blue `#0077cc` acts, teal identifies.** `--primary ← messageAction`; teal only as mark, pill tint, `update` role, connected/authorized dots | provider accent fallback moves off `#2563eb` |
| P6 | **The client renders, the agent mutates.** No map/band/card/quick-action module imports a mutating RPC (`ZeropsQuickActions.test.tsx` "cannot reach Zerops or the RPC layer at all", widened) | read-only / caller-bound hand-offs stay: Open URL, files via mounts, terminal, logs, Data Console |
| P7 | **Every state carries a phrase, never a blank.** Pinned in `strip.ts`; absent ≠ empty (`available:false` ⇒ the map is gone) | |
| P8 | **Native containers, Zerops contents** (mobile). Anything the OS draws stays OS-drawn; anything z3 draws takes the grammar | Roboto beside SF in an iOS nav bar is a visible seam (D5) |

---

## 4. Information architecture — web and desktop

**Nouns the user sees:** Account (orgs) → **Project** (= environment; the word never renders) →
**Thread**. **Services** are rows of the project's map, never navigation. **Devices**, **Coding
agents**. The T3 project record at `/var/www` is invisible.

**Shell — T3's three columns, Zerops' grammar.** Nothing is added to the horizontal budget.

```
┌────────────────┬──────────────────────────────────────────┬─────────────────┐
│ SIDEBAR 256    │ HEADER 52px  ⬡ kanban / Build the kanban  │ RIGHT PANEL     │
│  [Z] mark pill │              [Cloud IDE]  ▤ map  ▯       │  SERVICES       │
│  ( ⌕ ⌘K )      ├──────────────────────────────────────────┤  (default-open  │
│  ⬡ kanban ●    │ BAND 28px  ● DEVELOPING KANBANDEV     2 ▸ │   on a Zerops   │
│  ┌ thread ───┐ ├──────────────────────────────────────────┤   project)      │
│  │ ● DEVELOP…│ │ TIMELINE (virtualised; milestone cards    │  RUNTIMES       │
│  └───────────┘ │  outside the fold)                        │  DATA           │
│    ○ WAITING…  │ COMPOSER  ← question / credential dock    │  INFRASTRUCTURE │
│  ⚙ ☁ ▥         │ CONTEXT STRIP  kanban · kanbandev (…)     │   mint zcp panel│
└────────────────┴──────────────────────────────────────────┴─────────────────┘
```

- **Sidebar** — `Sidebar.tsx` stays whole as a component boundary (virtualisation, dnd-kit
  pinned reorder, snooze shelves, prewarm; no fourth branch in `AppSidebarLayout.tsx:229-238`).
  The header edit is clean: `SidebarChromeHeader` lives in `sidebar/SidebarChrome.tsx:36-118`,
  imported once — the Zerops mark on a teal-tinted pill + the ⌘K pill replace the T3 wordmark
  and the stage art without reopening the big file. The status edit is **not** clean and the
  verification pass corrected it (§10, claim 6): the thread status phrase is rendered *inside*
  `Sidebar.tsx` (`topStatus` 848-898, render 1453-1493, on the row's first line beside the
  favicon and title; a slim variant at 1232-1310 has no phrase slot), and the same fact is
  rendered again by the command palette (`ThreadStatusIndicators.tsx:466,522,597`, fed by
  `Sidebar.logic.ts:653`'s vocabulary with pinned tests), by `LegacySidebar.tsx`, and on mobile
  (`threadPresentation.ts:55-119`, `AgentActivity.tsx`). `StatusDot` + phrase is therefore an
  in-place edit of `SidebarThreadRow` plus three fan-outs (~7 sites) — AGENTS.md's "hit every
  surface" rule, not a component swap. Project rows carry the Zerops project name from the
  topology feed, never `www`; "All projects" scope and the `add-project` palette intent are
  hidden on Zerops environments.
- **Header** — 52 px; breadcrumb `⬡ kanban / <thread>`; **Cloud IDE** (replaces the
  `OpenInPicker` editor list); map toggle; panel controls. `GitActionsControl` (Commit ▾) is
  hidden on Zerops projects — never two commit pipelines.
- **Band** — new, 28 px, full width, tinted by tone, rendered under `WorkspacePageHeader` in
  `ChatView.tsx`; content `zeropsStripState(lifecycle, {pendingUserInput})` verbatim; the
  right end shows the pending-question count and a chevron that focuses the map.
- **Right panel** — kinds stay, plus `data-console`; `pull-request` hidden on Zerops; **`zerops`
  opens by default** the first time a Zerops project connects for a thread (`rightPanelStore`
  persists per thread, so closing it sticks). The inline/sheet decision moves from the fixed
  `(max-width: 980px)` viewport query to the **main column's width (≥ 1040 px)**. Verified cost
  (§10, claim 5): this is not a CSS swap — `shouldUseRightPanelSheet` is a JS boolean from a
  `matchMedia` hook that decides which subtree mounts, consumed at six sites in `ChatView.tsx`
  (two feed non-CSS logic: `canMaximizeRightPanel`, `inlineRightPanelOwnsTitleBar`); it needs a
  ResizeObserver-based `useContainerWidth` hook and a declared containment ancestor. An M, not
  an S, and the inline panel's own width is `PreviewPanelShell` (min 360 / default 540 / ≤ 70 %
  of viewport), not the 28 rem sheet cap.
- **Context strip** — the slot at `ChatView.tsx:7178-7188` that renders `BranchToolbar`
  ("Local checkout · main") becomes `kanban · kanbandev` (the mounted repo this thread's
  checkpoints target) + quick-action pills.

**Reverse states** (AGENTS.md: "a one-way door is a bug"): sign in ↔ sign out; connect device ↔
remove device; map open ↔ closed per thread; band click ↔ panel close; question answered ↔ Other
↔ reopen from the band; prefill ↔ clear; credential stored ↔ Replace (revoke stays in the GUI,
said so); Zerops theme ↔ library theme ↔ Reset to Zerops; pin/snooze/archive pairs survive
because `Sidebar.tsx` survives. One action has no reverse and says so: **"Enable Zerops Code"
restarts the container (~20 s)** — the pill carries the cost as a sub-label.

---

## 5. Visual system and the Zerops surfaces

### 5.1 Tokens — `ZEROPS_THEME` on the 57 roles (light / dark)

Full role-by-role table with contrast ratios: `research/R5-cross-client-design-system.md §2`.
Abridged:

| Role group | Light | Dark | Consumer |
|---|---|---|---|
| canvas · chrome · toolbar · sidebar | `#eceff3` | `#0c0f0e` | `--background`, topbar, mobile screen/status bar |
| surface · sidebarRowSelected | `#ffffff` | `#141918` | `--card`; a selected thread is a Zerops card |
| surfaceRaised · surfaceOverlay | `#ffffff` | `#1b2220` · `#1e2624` | composer glass, popovers |
| secondary · muted · accentSurface | `#f3f5f7` · `#f2f5f7` · `#f0f3f5` | `#232b29` · `#151b1a` · `#232b29` | flat pills, chips, hover rows |
| text · textMuted · placeholder | `#1a1a1a` · `#5f6a72` · `#757575` | `#e9eeec` · `#9faea9` · `#5e6e69` | |
| sidebarMutedForeground | `#4f5a62` | `#9faea9` | darker so the derived `--sidebar-icon-color` (60 % mix) clears 3:1 |
| border · input | `#e0e0e0` · `#d4dfea` | `#262f2d` · `#2a3331` | |
| toolbarControl / hover | `#e3e6ea` / `#d9dce0` | `#1b2220` / `#2a3331` | ⌘K pill (4 % / 8 % black flattened) |
| **messageAction → `--primary`** / hover | `#0077cc` / `#005fa3` | `#0077cc` / `#008ff5` | send, CTAs, switches; 4.66:1 both ways |
| focus → `--ring` | `#0077cc` | `#5ab3ff` | live GUI value |
| accent (mobile primary, md-link) | `#0077cc` on white | `#58a6ff` with `#0c0f0e` text | white on `#58a6ff` fails AA |
| messageSurface (user bubble) | `#e3f4ff` | `#1e2e3b` | Zerops core-blue tint, not iMessage blue |
| update / fg / surface | `#02b1a3` / `#007e72` / `#def8f6` | `#00e5c0` / `#58efd4` / `#113630` | the one place teal carries meaning |
| error / surface | `#cc0011` / `#fdefef` | `#e57373` / `#312828` | `#cc0011` on `#141918` is 3.03 → red 300 in dark; surface = live value |
| warning / fg / surface | `#ffa726` / `#bb4d00` / `#fdf6ef` | `#ffa726` / `#ffb74d` / `#453f36` | orange is indicator-only (1.94 on white) |
| codeBackground · terminalBackground | `#f2f5f7` | `#121716` · `#0c0f0e` | cursor `#007e72` / `#00e5c0` |

Theme colours are stored opaque (`themePalette.ts:310-312`); Zerops paints 4 %/8 % overlays, so
the generator flattens per declared surface and records which. `getDefaultThemeColors`
(`themePalette.ts:1224`) and `index.html`'s `DEFAULT_THEME_PALETTES` today fall back to T3 Chat
— both must point at `ZEROPS_THEME` in the same slice that registers it, or a partial theme
flashes pink.

### 5.2 Fixed brand tokens — `packages/shared/src/brand.ts`, not a new package

Status tones, the mint panel, chip tints, the mark's path data, the icon map and the copy
glossary are **not** theme roles, so a user on "Grove" keeps the Zerops status grammar and the
mint zcp panel (the precedent: `--success`/`--info` are theme-independent, `index.css:1429`).
They live beside `themePalettes.ts` in `packages/shared` — already *owned core* in `fork.md §3`
— with no new workspace member or zone row (two of three judges called a `packages/brand`
package ceremony; agreed). Two precisions from verification (§10, claim 14): the package's
exports map is an explicit 1:1 list, so the file needs its own `exports` entry (that much build
wiring exists); and the subpath must **not** contain the substring `zerops` — the ported zone
imports `@t3tools/shared` subpaths and the architecture test forbids anything matching `zerops`
there. Hence `brand.ts`, not `brand.ts`.

| Tone | Dot · text · surface (light) | Dark |
|---|---|---|
| ok | `#66bb6a` · `#2e7d32` · `#e8f7ec` | `#56d364` · `#56d364` · `rgba(86,211,100,.14)` over `#141918` |
| busy | `#42a5f5` · — · `#eff9fd` | `#58a6ff` · `#1e2e3b` |
| attention | `#ffa726` · `#b26a00` · `#fff4e0` | `#e8a33d` · `#ffb74d` · `#453f36` |
| failed | `#ef5350` · — · `#fdefef` | `#f47067` · `#312828` |
| off | `#bdbdbd` | `#5e6e69` |
| identity | mark `#3cbdb2` + `#00b1a3`; pill tint `rgba(0,204,187,.13)` | mint `#00e5c0`, pill `rgba(0,229,192,.13)` |
| chips | access-green `rgba(76,175,80,.15)`/`#388e3c` · region-purple `rgba(156,39,176,.15)`/`#7b1fa2` · info-chip white .9 | live dark equivalents in R7 |

Platform status collapses onto the five tones by a table: `ACTIVE/RUNNING` → ok;
`READY_TO_DEPLOY` → busy, settled (no pulse); `STOPPED/REPAIRING` → attention; `*FAILED/DELETING`
→ failed; `DELETED` → off; everything `transient` → busy + pulse. Agent auth: `● AUTHORIZED`
(ok, haloed) / `○ NOT AUTHORIZED` (attention, static).

### 5.3 Type, shape, icons, motion

- **Roboto** 400/500/700, self-hosted woff2 (`apps/web/public/fonts`, preload, `swap`; no
  Google Fonts fetch from a container origin); `index.css @theme` + `appearanceFonts.ts`.
  Mobile: `@expo-google-fonts/roboto` replacing DM Sans through the same `expo-font` path.
  Mono stays T3's SF Mono/Menlo stack for code, diff, terminal (perf; Ghostty and Pierre own
  their fonts); Roboto Mono only for hostnames/URLs/log lines inside Zerops cards. Geologica
  (marketing display face) is reserved for the landing headline only, if at all.
- **Scale (web):** body 14 · row hostname 14/500 with `:port` at 60 % · card title 14/500 ·
  project name 20/500 · description 13/1.6 at 70 % · MicroLabel 10/600 · draft hero 32/400.
- **Shape:** `--radius` stays 10 px (= `.mat-card`); `--control-radius` 8 px; dialogs 16 px;
  chips 10 px; info-chips 8 px; key chip 3 px; composer 22 px untouched. **Selection = 2 px
  `#0077cc` outline** (`rgba(0,229,192,.4)` dark), never a filled background. The dark dialog
  scrim stays; the GUI's white scrim is legacy.
- **Icons:** lucide (web), SF Symbols → Tabler (mobile); **no Material Icons webfont**; a
  ~15-glyph map in `brand.ts`; the mark as path data rendered by `<svg>` / `react-native-svg`
  (the `T3Wordmark.tsx` pattern); provider marks as `currentColor` SVGs ported from `libs/zui`.
  The 87 service-type icons are *not* used in map rows (the GUI's own cards show none) — D7.
- **Motion:** easing `cubic-bezier(.4,0,.2,1)`, 150/200/300 ms; in-flight = `status-pulse`
  `steps(6)`; deleted, not ported: ripple, bounce, shimmer, bokeh, sticky-header scale; mobile's
  infinite `withRepeat` pulses become a 3-frame duty cycle; `prefers-reduced-motion` disables all.

### 5.4 Screens (web)

**Sign-in — one bundle, one door.** The GUI login card transplanted (`01-login.png`): mark + grey
wordmark + "Code", "Cloud platform for humans and their coding agents", GitHub `#24292e` and
GitLab `#9b51e0` pills, `or`, outlined Email/Password, blue "Login using email" pill, "Create a
new Zerops account" (Turnstile or the app.zerops.io hand-off), footer "Not using Zerops? Connect
a server with a one-time link". On a container origin one line under the wordmark: `● ACTIVE
kanban · zcp`. The blue `AuthSurfaceShell` gradient is deleted.

**Picker — the GUI dashboard, transplanted** (project-first's best surface, taken whole):

```
┌────────────────────────────────────────────────────────────────────────────┐
│ [Z] KRLS ▾  ( ⌕ ⌘K )    Projects            ⌕ Search projects…  Created ↓  │
├────────────────────────────────────────────────────────────────────────────┤
│ CONNECTED                                                                  │
│ ┌──────────────────────────────┐ ┌──────────────────────────────┐          │
│ │ ● ACTIVE  kanban          ›  │ │ ● ACTIVE  shop            ›  │          │
│ │ [FULL ACCESS][EU CENTRAL]    │ │ [FULL ACCESS][EU CENTRAL]    │          │
│ │ zcp · ready · 3 services     │ │ zcp · ready · 5 services     │          │
│ │ 2 threads · 1 working        │ │ task complete                │          │
│ │ (          Open          )   │ │ (          Open          )   │          │
│ └──────────────────────────────┘ └──────────────────────────────┘          │
│ READY TO CONNECT                                                           │
│ ┌──────────────────────────────┐ ┌──────────────────────────────┐          │
│ │ ● ACTIVE  legacy             │ │ ◌ RESTARTING  blog     12 s  │          │
│ │ zcp · needs Zerops Code      │ │ upgrading the container      │          │
│ │ ( Enable Zerops Code ) ~20 s │ │                              │          │
│ └──────────────────────────────┘ └──────────────────────────────┘          │
│ PREPARING                          ┌ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┐         │
│ ┌──────────────────────────────┐        ＋  New project                   │
│ │ ◌ CREATING  new-shop         │  └ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ┘         │
│ │ waiting for the container    │                                           │
│ │ 0:41 / 5:00  ( Stop waiting )│                                           │
│ └──────────────────────────────┘                                           │
│ NOT AVAILABLE   demo — project STOPPED  ·  tools — no public subdomain      │
└────────────────────────────────────────────────────────────────────────────┘
```

Groups and reasons from `candidates.ts`; the PREPARING card *is* the provisioning waiter with
its caps shown as elapsed/cap; `pool-exhausted` is a card state with copy; every zcp container in
every org is one card.

**Thread — band, timeline, dock, map.**

```
┌──────────────┬────────────────────────────────────────────────┬────────────────┐
│ [Z] Code     │ ⬡ kanban / Build the kanban   [Cloud IDE] ▤ ▯ │ SERVICES     × │
│ ( ⌕  ⌘K )    ├────────────────────────────────────────────────┤ live · 2 s ago │
│              │ ● DEVELOPING KANBANDEV                    2 ▸ │ RUNTIMES       │
│ ⬡ kanban ●   ├────────────────────────────────────────────────┤ ● ACTIVE       │
│ ┌──────────┐ │           ┌──────────────────────────────────┐ │ kanbandev:3000 │
│ │Build the │ │           │ Vytvoř mi kanban.                │ │ nodejs@22      │
│ │kanban    │ │           └──────────────────────────────────┘ │ [MOUNTED](Open)│
│ │● DEVELOP…│ │ I'll propose three services first.             │ └ ○ READY      │
│ └──────────┘ │ ┌ PLAN · 3 SERVICES ───────────────────────┐   │   kanbanstage  │
│  Add auth    │ │ ○ kanbandev    nodejs@22       dev       │   │ DATA           │
│  ○ WAITING…  │ │ ○ kanbanstage  nodejs@22       stage     │   │ ● ACTIVE db    │
│              │ │ ○ db           postgresql@18             │   │ (Data Console) │
│ ⬡ shop ●     │ └──────────────────────────────────────────┘   │ INFRASTRUCTURE │
│  Fix cart    │ ▸ Read 3 files and ran 2 commands              │ ╔════════════╗ │
│  ◐ 1d        │ ┌ DEPLOY · KANBANDEV ──────────────────────┐   │ ║● ACTIVE zcp║ │
│              │ │ ✓ build 41 s  ✓ deploy  ✓ subdomain      │   │ ║(Web Term.) ║ │
│              │ │ [ ◎ kanbandev-24cb.prg1.zerops.app  ↗ ]  │   │ ║(Cloud IDE) ║ │
│              │ └──────────────────────────────────────────┘   │ ║(SSH)(Dsktp)║ │
│              │ ┌ VERIFY · KANBANDEV ── ✓ HTTP 200 · healthy┐  │ ║✳ Claude    ║ │
│              │ └──────────────────────────────────────────┘   │ ║ ● AUTHORIZED║ │
│              │ ┌────────────────────────────────────────┐     │ ╚════════════╝ │
│              │ │ Ask for changes…                       │     │                │
│              │ │ ✳ Claude Code ▾ High ▾ Full access ▾(↑)│     │                │
│ ⚙  ☁  ▥      │ └────────────────────────────────────────┘     │                │
│              │  kanban · kanbandev  (Deploy)(Logs)(Redis)     │                │
└──────────────┴────────────────────────────────────────────────┴────────────────┘
```

**The dock.** Two facts the synthesis verified in code overturn one proposal's "relocate the
question card": the card is *already* docked at the composer (`ChatComposer.tsx:2996,3027`,
inside `.chat-composer-top-drawer`, whose tone is a single `data-variant` ternary at `:2979`
with a four-tone tint system in `index.css:929-947`). The work is tone (`info` → attention
orange) and anatomy (option tiles with digit key chips, a visible `Other…`, the 2 px blue
outline on the chosen option). The **credential card** shares the drawer with a distinct anatomy
— lock kicker, target host, one masked field, "Store on kanbandev", a timeline receipt with no
value in it — and **renders only the "set it in the Zerops GUI" deep link until the zcp storage
verb exists** (D4): a field that goes nowhere would imply the chat-paste path the brief forbids.

**Settings & palette.** Sections: General · Appearance (the whole theme library stays; Zerops
is the default card; "Reset to Zerops"; the stage-artwork row goes) · Keybindings · **Coding
agents** · Integrations · Source control (hidden on Zerops) · **Devices** · **Zerops** (account,
orgs, sign out, Open Zerops) · Archive. Command palette restyled to the GUI palette grammar
(teal-tinted scope chip, 10/700 group titles, 40 px rows, key-chip footer); new intents *New
thread in <project>*, *Open service map*, *Open <hostname> ↗*, *Show logs for <host>* (prefill),
*Open Cloud IDE*, *Switch project*, *New project*; `add-project` folder browsing removed on Zerops.

### 5.5 The Zerops surfaces

- **Service map.** Header `SERVICES` + a liveness line (`live · updated 2 s ago` / `polling` /
  `last read failed · retrying`); `available:false` ⇒ absent; `degraded` ⇒ last-good rows + one
  orange line; doorbell down ⇒ one quiet grey line. Groups RUNTIMES / DATA / INFRASTRUCTURE in
  the pinned order (`serviceMap.ts:20-25` — "what the user builds, what it stores, what runs
  it"). `ServiceRow`: `● ACTIVE` · `kanbandev:3000` (14/500, port at 60 %) · `nodejs@22`
  `[MOUNTED]` `( Open )` · the dev↔stage pair folded with `└` · `production: shop-prod ↗`. A
  Data row gains a `Data Console` pill. The zcp row under Infrastructure is the **MintPanel**:
  `● ACTIVE zcp`, four white pills in the GUI's 2-column grid, the edge-to-edge agent tray. SSH
  and Desktop IDEs link out to the GUI service page (both need VPN — out of scope). A running
  tool is attributed to the map, not a row (`ZeropsRecentTool` carries no hostname, and the
  code says so). Ladder: inline ≥ 1040 px of main column → sheet → absent.
- **Lifecycle band.** 28 px, 12/500, tone tint; content `zeropsStripState` verbatim; precedence
  *waiting for you* › *<tool> running* › phase; the strip's `Spinner` becomes a `StatusDot` with
  the duty-cycled pulse. `ZeropsStripLine` is already split from its feed container for testing;
  the band reuses the split.
- **Cards and the fold.** Kinds `plan / import / mount / deploy / verify / subdomain / error`,
  decoded totally, generic block on failure. Restyled as **process shells**: 8 px radius, a
  tone-tinted header stripe with a MicroLabel kicker (`DEPLOY · KANBANDEV`), 17 px step glyphs in
  2 px-bordered circles on a `30px 1fr` grid (queued `clock` → running `play` → done `check` →
  failed `!`), body 12/400 lh 1.7, a white info-chip with a green globe for the URL, log lines on
  `codeBackground` in a 200 px scroll for errors.
  **The fold, precisely** (§10, claims 2–3, and fill F3): three mechanisms hide a card today.
  (1) The **turn fold** — a settled turn collapses its middle entries behind `Worked for Ns ›`
  (`deriveTurnFolds`, `MessagesTimeline.logic.ts:597-648`; this is what the live capture shows).
  (2) The **work-group summary** — a group of tool entries collapses behind a composed sentence
  whose "other" branch is literally `Used N tools`, and `mcp_tool_call` buckets to "other", so
  that *is* the label for a group of `zerops_*` calls. (3) The **overflow toggle** `+N previous
  tool calls` for long groups. Agent-spawn rows are exempted from (2) and (3) with the comment
  "a running fleet must never hide behind a '+N tool calls' toggle"; a `zeropsMilestone`
  predicate must be added at **both** branch points of (2)/(3) *and* to (1)'s `hiddenEntryIds`
  for plan / import / deploy / verify / error to stay visible; mount / subdomain stay folded and
  are named in the summary (D2).
  **A live card path does not exist.** `ZeropsToolCard` mounts only for a *settled* result
  (`MessagesTimeline.tsx:2599-2606`); the live row (`LiveWorkEntryTimelineRow`) never decodes a
  card, so "import with per-service progress" and "deploy with build progress" (brief §7) have
  no in-timeline progress today — the map's 3 s nudge poll is the only live signal. The concept
  adds a live card variant fed by the lifecycle feed's `recentTools` + the map's transient rows
  (new work, P2), rendered in the same process-shell frame with the running step glyph.
- **Question card / credential card / quick actions.** As in §5.4; quick actions prefill only;
  the no-mutation test widens to the band, the map and the credential card (which may reach
  exactly one server RPC — the zcp verb).
- **Data Console and hand-offs.** A `data-console` right-panel kind: an iframe of the
  caller-bound console on the same 8080 origin, **read-only in v1** (write authority is a
  per-request `X-Write-Token`; threading it through an embed is S6 design work — D6). Cloud IDE
  opens code-server on the same origin in a new tab; Web Terminal opens z3's terminal with cwd
  `/var/www`; SSH copies the command.

---

## 6. Mobile — a Zerops language for a device Zerops never designed for

There is no precedent to copy. Three laws:

1. **Anything the OS draws stays OS-drawn** — native-stack with flat thread routes, glass
   `UINavigationBar` with scroll-edge effects, native header items (SF Symbols only), form
   sheets with detents, `UIMenu`, the Mail search toolbar, keyboard stack, haptics, predictive
   back, Android's in-flow chrome and FAB.
2. **Anything z3 draws inside those containers takes the grammar** — canvas `#eceff3`/`#0c0f0e`,
   white cards at **16 px** (today 24), no card shadows, `● ACTIVE` dot-and-word, MicroLabel at
   11 px, pills for actions, primary `#0077cc` (today near-black), user bubble `#e3f4ff` with
   dark text (today iMessage blue).
3. **One phrase, four places, one function** — the strip string appears simultaneously in the
   thread row subtitle, the thread header subtitle, the map sheet header and the **Live
   Activity / Dynamic Island** — all from `strip.ts` once promoted to `client-runtime`. This
   is the one feature desktop cannot match, and it is free: `widgets/AgentActivity.tsx` already
   renders a phase with tints — T3's vocabulary instead of the Zerops phrase.

**What the phone is for:** watch, answer, nudge — see which thread needs you, answer a question
card, approve a plan, read a deploy card and open the URL, glance at the map, send one sentence.
Long specs are typed on a laptop.

| Route | Shape |
|---|---|
| Sign in (new, S5) | email/password + TOTP as a form sheet; registration → app.zerops.io or a Turnstile WebView; replaces the host+code fields for Zerops, the one-time-link path stays below a divider |
| Home | project cards `● ACTIVE kanban · 3 svcs` with the live phrase; thread rows whose second line is `StatusDot` + phrase |
| Projects | the picker as a form sheet, four groups, one action pill per row, "Enable Zerops Code" with the ~20 s note |
| Thread | header subtitle = the phrase; Zerops cards in `ThreadFeed`; ▤ opens the map sheet; **the question card already replaces the composer** (`ThreadDetailScreen.tsx:696-735`) — it needs tone, descriptions and Other, not relocation |
| Map sheet (new) | detents `[0.55, 0.92]`; `kanban · SERVICES` + phrase; groups; ServiceRows; the MintPanel |
| Settings | Zerops account · Projects · Devices · Coding agents · Appearance (Zerops = default) · Archive |
| Tablet | `AdaptiveWorkspaceLayout`'s inspector pane takes the map — the web three-column view |

```
HOME                              THREAD                            MAP SHEET
┌────────────────────────────┐  ┌────────────────────────────┐  ┌────────────────────────────┐
│ [Z] Code          ⌕   ⋯    │  │ ‹ kanban  Build the kanban │  │            ━━━━            │
├────────────────────────────┤  │   ● DEVELOPING KANBANDEV ▤ │  │ kanban · SERVICES    Done  │
│ ⬡ KANBAN    ● ACTIVE 3 svcs│  ├────────────────────────────┤  │ ● DEVELOPING KANBANDEV     │
│ ┌────────────────────────┐ │  │      ┌───────────────────┐ │  ├────────────────────────────┤
│ │ Build the kanban    5m │ │  │      │ Vytvoř mi kanban. │ │  │ RUNTIMES                   │
│ │ ● DEVELOPING KANBANDEV │ │  │      └───────────────────┘ │  │ ┌────────────────────────┐ │
│ └────────────────────────┘ │  │ ┌ PLAN · 3 SERVICES ─────┐ │  │ │ ● ACTIVE kanbandev:3000│ │
│ ┌────────────────────────┐ │  │ │ ○ kanbandev  nodejs@22 │ │  │ │ nodejs@22 · mounted    │ │
│ │ Add auth            1h │ │  │ │ ○ db         postgres  │ │  │ │               ( Open ) │ │
│ │ ○ WAITING FOR YOU      │ │  │ └────────────────────────┘ │  │ │ └ ○ READY  kanbanstage │ │
│ └────────────────────────┘ │  │ ┌ DEPLOY · KANBANDEV ────┐ │  │ └────────────────────────┘ │
│ ⬡ SHOP      ● ACTIVE 5 svcs│  │ │ ✓ build ✓ deploy       │ │  │ DATA                       │
│ ┌────────────────────────┐ │  │ │ [◎ kanbandev-24cb… ↗]  │ │  │ ● ACTIVE db  (Data Console)│
│ │ Fix cart            2d │ │  │ └────────────────────────┘ │  │ INFRASTRUCTURE             │
│ │ ● TASK COMPLETE        │ │  │ ┌ VERIFY ─ ✓ HTTP 200 ───┐ │  │ ╔════════════════════════╗ │
│ └────────────────────────┘ │  │ └────────────────────────┘ │  │ ║ ● ACTIVE zcp           ║ │
│                            │  │ (Deploy)(Logs)(Add Redis)  │  │ ║ (Web Terminal)(Cloud…) ║ │
│                    ( ＋ )  │  │ ╭────────────────────────╮ │  │ ║ ✳ Claude · AUTHORIZED  ║ │
│                            │  │ │ Ask for changes…  (↑)  │ │  │ ╚════════════════════════╝ │
└────────────────────────────┘  └────────────────────────────┘  │ live · updated 2 s ago     │
                                                                └────────────────────────────┘
```

Seams, stated: Roboto next to SF in system-drawn bars (D5); the composer caret stays iOS blue
unless `T3ComposerEditorView.swift:763` is edited; Turnstile in a WebView is unverified;
everything above is downstream of promoting `apps/web/src/zerops/{candidates, provisioning,
containerHealth, firstPrompt, serviceMap, strip, quickActions, cards/*}.ts` — UI-free TypeScript
— to `packages/client-runtime`.

---

## 7. Desktop

Same bundle in Electron, one smoke build. Five changes: (1) first frame from generated brand
colours instead of literals — the verified surface (§10, claim 15) is ~9 literal sites in two
files (`DesktopWindow.ts` three titlebar constants, the background function, three duplicated
values inside the WSL splash builder; `preview/Manager.ts` two PiP backgrounds) plus a 13-colour
`DEFAULT_ANNOTATION_THEME` and no IPC field yet to carry colours to the preview window — a small
slice, not a three-line change (canvas `#eceff3`/`#0c0f0e`, symbols `#1a1a1a`/`#e9eeec`;
`TITLEBAR_HEIGHT 40` already matches the GUI app bar); (2) WCO —
nothing new, but the smoke build must confirm the mark pill is not clipped under the reserved
inset; (3) naming — `APP_BASE_NAME` in `branding.ts` and `DesktopEnvironment.ts:88`, menu labels,
the stage pill without artwork; the `t3code://` scheme stays (out of scope) and goes into
`hacks.md`; (4) icons — a Zerops `.icon` Icon Composer project per channel, macOS desk work;
(5) the one surface that earns desktop's existence: the `preview` panel becomes the in-app
preview of a deployed subdomain ("Preview here" on a map row and on the verify card's URL chip).
Structural prerequisite (R4): with the local backend gone, `t3code://app` needs a renderer source
(packaged `apps/web/dist` served from the scheme handler, or the hosted origin) and the window
must open without a backend latch. Two update pills, one colour: desktop self-update and "Enable
the new Zerops Code on kanban" both render the `update` role.

---

## 8. Cross-client design system, governance, build strategy

### 8.1 One source, three projections

```
packages/shared/src/themePalettes.ts   ZEROPS_THEME (57 roles, light + variants.dark)
packages/shared/src/brand.ts     status tones · mint panel · chip tints · mark path data
                                       · icon map · radius/type constants · copy glossary
scripts/generate-theme-tokens.ts       root, beside export-brand-icons.ts; `generate:theme(:check)`
   ├─► apps/web/src/generated/zerops-theme.css     :root{--app-theme-*} + @variant dark
   │        (index.css keeps only the semantic remap; the hand-written :root/.dark blocks go)
   ├─► apps/web/index.html boot snippet            SPLASH_COLORS / DEFAULT_THEME_PALETTES / theme-color
   ├─► apps/web/public/manifest.webmanifest
   ├─► packages/shared/src/generated/brandColors.ts → themePreview.ts, DesktopWindow.ts,
   │        app.config.ts splash, withAndroidModern{PopupMenu,AlertDialog}.cjs, Live Activity
   ├─► apps/mobile/global.css @variant light/dark  (extend renderVariant; today adaptive vars only)
   └─► apps/mobile/src/generated-uniwind-*.{css,json}  unchanged pipeline
scripts/export-brand-icons.ts          .icon per channel → app icons, favicons, apple-touch
```

Shared: colour roles, fixed tokens, constants, mark data, glossary; pure behaviour promoted to
`client-runtime/src/zerops/`; the written **component vocabulary** — `StatusDot · MicroLabel ·
Chip · Pill · FlatCard · MintPanel · LifecycleBand · ServiceRow · ZeropsCard · QuestionCard ·
CredentialCard` — anatomy and states, never pixels. Per client: primitives, layout, navigation,
sheets, glass, menus, keyboard, haptics, WCO. No UI-shape code is shared today (`client-runtime`
is pure TS) and stays that way.

### 8.2 Drift guards — tests, not review habits

1. Byte-equality of every generated file + `generate:theme --check` in CI (the pattern of
   `generate-uniwind-themes.test.ts`).
2. Role coverage (`⊇ THEME_COLOR_ROLES`, both appearances), every value opaque, contrast ≥ 4.5
   for text/canvas, text/surface, action, accentSurface and each semantic foreground-on-surface
   pair; ≥ 3.0 focus/canvas and the 60 %-mixed sidebar icon; teal foreground over a light
   surface fails.
3. No-literal lint: no `#hex`, `rgba(`, or Tailwind palette utility under `components/zerops/**`,
   `components/ui/**`, `apps/mobile/src/features/zerops/**` (zero tolerance), repo-wide with an
   allowlist later (icons, ANSI, shiki/Pierre). Burn-down: 217 web utilities, 155 mobile literals.
4. Vocabulary lint over user-facing strings: no T3 Code / T3 Connect / Tailscale / pairing /
   worktree / Local checkout / "environment" as a noun / "control plane" as a self-description
   (zcp's `TestNoBareZeropsURIInAgentContent` pattern).
5. Phrase parity is structural once `strip.ts` is shared; status-vocabulary parity is a table
   test over `SETTLED_ZEROPS_SERVICE_STATUSES` + transient in both clients.
6. Perf guard: no `infinite` keyframe without `steps()` outside the allowlist; no `Spinner` in a
   map row or the band.
7. No-mutation guard for the band, the map and the credential card — **a new, narrower
   assertion**, not a copy (§10, claim 13): the existing quick-actions test is a raw-text
   substring check on one file; the map's presentational file has no RPC imports (its container
   `ZeropsPanel.tsx` does), and the band's container legitimately imports the feed-subscription
   hook. The widened guard forbids *mutating* RPC names and allows `subscribe*` hooks.

Visual regression: mobile already has `pnpm screenshots:mobile` (scenes per theme id, so
registering `zerops` captures it) — add Projects, Map sheet, thread-with-question,
thread-with-credential; web gains `scripts/web-showcase.ts` over the same seeded environment
(landing · picker · thread states · map · settings · palette at 1440×900 and 390×844, both
themes); desktop `smoke-test.mjs` + `capturePage()`. Captures are reviewed PR artifacts, not a
pixel gate (font rasterisation flakes); the gate is the list above.

Where decisions live: a new `docs/spec-z3.md §8 "The client design system"` with DS-rows pinned
by these tests (DS-1 every chrome colour is a role or a brand token; DS-2 generated copies are
byte-equal; DS-3 blue acts, teal identifies; DS-4 dot + word; DS-5 no infinite animation outside
the duty-cycled keyframes; DS-6 one phrase function; DS-7 no Zerops surface imports a mutating
RPC). The working spec (anatomy/states per vocabulary component, icon map, glossary) lives in
`z3/docs/internals/zerops/design-system.md`, dated, like the other ledger files.

### 8.3 Build strategy — evolve `apps/web`, or the "new UI app"?

`fork.md §1` says "a full UI rewrite is planned" and reserves "the new UI app (later)" in the
owned-product zone. The concept was built so that the rewrite is **not** a prerequisite:

| Option | What it means | When it is right |
|---|---|---|
| **Evolve `apps/web`** (recommended for P0–P2) | Zerops theme as default, primitives restyled, band + default-open map + GUI picker, Zerops surfaces rebuilt from the new primitives; `Sidebar.tsx`, timeline, composer, diff, terminal — the load-bearing T3 assets — stay | Always, for the next two phases; the recognition surfaces are outside `zerops/**` and every Zerops logic module is pure TS that moves up, not out |
| **New UI app** over `client-runtime` + `contracts` | A fresh React (or other) shell that re-implements thread list, timeline, composer, diff, terminal against the same RPC/state layer | Only if D1 goes toward the GUI's project-card column *and* the owner wants a clean break from T3's shell; it re-implements ~15 k lines of proven UI (Sidebar 3960, ChatView 7398, ChatComposer 3599, MessagesTimeline 2728) before it looks like anything |

Recommendation: evolve now; keep "new UI app" as the option the vocabulary + `client-runtime`
promotion makes possible later, not as the path to a Zerops look. This is D0.

---

## 9. Migration and routing

**Effort:** S ≤ 1 slice · M 2–4 slices · L a stream. **Routing per `/route`:** slices are Sonnet
subagents with self-contained briefs (RED → GREEN in their own worktree); reviews and the
per-phase judge are Opus; Fable orchestrates, never implements inline.

**The ordering rule, enforced as a PR gate:** phases P0–P1 touch **no file under
`apps/web/src/components/zerops/**` or `apps/web/src/zerops/**`** (S4 owns the auth gate, S6 the
map/cards/picker, S7 the agent-auth card). Design lands there only in P2, after those streams
stabilise.

| Phase | What | Effort | Throwaway |
|---|---|---|---|
| **P0 — words and marks** | product name, mark pill replacing the wordmark, splash, favicons, titles, client labels, section renames (Devices, Coding agents), vocabulary lint; no colour change. Web ~12 files (`branding.ts`, `SidebarChrome.tsx`, `SplashScreen.tsx`, `index.html`, `settingsSearch.ts`, `clientMetadata.ts`, delete `SidebarStageBackdrop.tsx`…), desktop `DesktopEnvironment.ts` + menu, mobile `BrandMark`/`T3Wordmark`→`ZeropsMark`, `CompactBrandTitle`, ~26 string files (bundle ids + `t3code://` stay) | S–M | none |
| **P1 — the Zerops look, without touching `zerops/**`** | `ZEROPS_THEME` + `brand.ts`; default on both clients through the existing theme path (`useTheme.ts`, boot script, `getDefaultThemeColors`, `MOBILE_DEFAULT_THEME_ID`); Roboto; primitives to pills/chips/flat cards/key chips/snacks + new `status-dot`/`micro-label`; **the band** in `ChatView.tsx`; **question-card tone** (`ChatComposer.tsx:2979`); sidebar row subtitle + StatusDot; mobile radii 24→16, `ControlPill` primary, `StatusPill` tones, bubble | M | the hand-edited boot palette block (P4 generates it) |
| **P2 — Zerops surfaces in Zerops idiom** *(S4/S6/S7-coupled)* | ServiceRow + MintPanel + liveness line (the S7 `ZeropsAgentAuthCard` from `wt/s7-web` is the tray's occupant — one rendering of agent auth, not two beside `ProviderInstanceCard`); default-open map + the `useContainerWidth` ladder; milestone fold escape at all three fold points; card process shells **+ the live card path**; landing + picker anatomy + provisioning; **the door on the container origin** (`PairingRouteSurface` branches on `bootstrapMethods`, `resolveChatIndexView` widened, the identity base URL from `window.location` — coordinated with S4); question tiles; credential card (GUI-link-only until the verb); `data-console` kind; quick actions into the context strip; `GitActionsControl` hidden; palette intents; the composer's "Full access" label renamed (it collides with the GUI's FULL ACCESS role chip — fill F1) | M–L | every ad-hoc frame; `routes/zerops.tsx` + `ZeropsProjectsPage` fold into `/` |
| **P3 — mobile** | promote the eight pure modules to `client-runtime` **first**; sign-in/TOTP; Projects; provisioning; Enable; Home cards + subtitles; thread subtitle; cards in `ThreadFeed`; map sheet; Devices; Live Activity off `strip.ts`; showcase scenes | L | the pairing-code screens for Zerops projects (kept as the manual fallback) |
| **P4 — generator, deletion, harness** | `generate-theme-tokens.ts` + test + CI `--check`; delete the six hand copies; no-literal lint repo-wide; `web-showcase.ts` | L (six targets, one precedent) | none |
| **P5 — desktop smoke** | window colours from the generator, `.icon` per channel, naming, renderer source without the local backend, `capturePage()` | S | none |

Order rationale: P0 is free and removes the most visible wrongness. P1 is where the product
starts looking like Zerops and collides with nobody. P2 waits for S4/S6 and is the last product
phase. P3 is gated on the promotion. P4 is hygiene the product already works without — the one
place this concept overrules the winning proposal's own order. P5 any time after P1.

Never throwaway: every module under `apps/web/src/zerops/` — they move up to `client-runtime`.

---

## 10. Verification of the load-bearing claims

Sixteen claims the concept depends on were each attacked by two skeptics (a code lens and a
spec lens, Sonnet) with an Opus arbiter on disagreement — 37 agents, full evidence in
`research/verification.md`. Eight hold as stated; eight were imprecise and are corrected in the
text above. None of the corrections changes the shape of the concept; two change its cost, one
lowers it.

| # | Claim | Verdict → what changed |
|---|---|---|
| 1 | The question card is already docked at the composer; the graft is tone, not relocation | holds |
| 2 | Agent-spawn rows are exempted from the fold — the precedent for milestone cards | holds; the exemption sits at two branch points |
| 3 | The collapsed label is not the literal "Used N tools" | **corrected**: three fold mechanisms; `Used N tools` *is* the label for a `zerops_*` group; the turn fold `Worked for Ns` hides it too (§5.5) |
| 4 | The 280 px rail cannot fit a 1440 px laptop | holds in substance; the inline panel is `PreviewPanelShell` (min 360), not the 28 rem sheet — 256+640+280+360 = 1536 > 1440 |
| 5 | Container query swap is cheap and in-idiom | **corrected**: a JS boolean at six sites; needs a ResizeObserver hook — M (§4) |
| 6 | Sidebar.tsx stays whole; only header + row subtitle change | **corrected**: the status phrase is an in-place edit inside `Sidebar.tsx` + three fan-outs (§4) |
| 7 | `ZeropsPanel` + the `zerops` kind are small, live, S6-owned; keep both | holds; `wt/s7-web` also edits `ZeropsPanel.tsx` |
| 8 | 57 roles; registering the theme id drives the mobile generator and showcase | holds; cost = 114 hand-authored values, a pinned name list, two web id lists and the boot palette; a missing definition silently renders T3 Chat colours |
| 9 | On mobile the question card already replaces the composer | holds |
| 10 | Mobile has zero Zerops code; every screen is downstream of the promotion | holds; necessary, not sufficient (native hooks + Turnstile WebView still needed) |
| 11 | The descriptor-keyed `requires-auth` branch is the one prerequisite design cannot supply | **corrected in the concept's favour**: descriptor, server policy, session and `connectZeropsIdentity` already ship in the container bundle; two UI gates block the door and design directs them (§2.6) |
| 12 | Duty-cycled `steps()` pulses satisfy the no-repaint rule | holds as consistent with the rule's rationale; AGENTS.md has no explicit carve-out |
| 13 | The no-mutation guard already exists and widens to band + map | **corrected**: a raw-text check on one file; the widened guard is a new, narrower assertion (§8.2) |
| 14 | `packages/shared` needs no build wiring or zone row for the brand file | **corrected**: an `exports` entry is required; the subpath must avoid the substring `zerops` (§5.2) |
| 15 | Desktop's colour surface is two constants + one function | **corrected**: ~9 sites in two files + an annotation theme + an IPC field (§7) |
| 16 | Map group order and "a running tool belongs to the map" are decided in code | holds |

Gap fills that changed the text: F1 (S7 worktrees build the GUI's agent-auth state machine 1:1;
`ProviderInstanceCard` already renders the same fact → one tray, not two; the "Full access"
label collides with the GUI's role chip; the GUI's own authorized dot is the static-halo pattern
to copy), F2 (bundle variants; `cli.ts pack` bakes T3 favicons; Turnstile hostname binding), F3
(no live card path; question-card requirements are fully implemented; "infra session held by
thread X" and the next-task shortcut do not exist yet), F4 (six colour paths bypass the roles,
including Ghostty's ANSI-16 compiled into WASM with no override hook — the dark canvas under
default ANSI blue must be captured before P1 ships; none of the four teals passes AA with white
text), F5 (live: the container bundle's baked env is `VITE_HOSTED_APP_CHANNEL:""`,
`VITE_BASE_PATH:"/z3"`; the GUI's generic status dot is identical in light and dark while the
agent-tray dot adapts; z3's dark toggle is `html.dark` via `t3code:theme-appearance-mode`).

---

## 11. Decisions for the owner

- **D0 — Build strategy.** Evolve `apps/web` (recommended) or start the "new UI app" `fork.md`
  reserves. The concept assumes evolve; a rewrite is not needed for the Zerops look.
- **D1 — `Sidebar.tsx` stays whole**; only its header and row subtitle change. Or: the GUI's
  project-card column instead of the cross-project thread list — highest brand fidelity, a
  workflow regression by every judge's account; decide before P1, it changes P1 and P2 entirely.
- **D2 — Which cards escape the fold.** Concept: plan / import / deploy / verify / error escape;
  mount / subdomain stay folded and are named in the summary.
- **D3 — The map opens by default** on a Zerops project, remembered per thread. Cost: one column
  of thread width on a 1280 px laptop.
- **D4 — Credential card** renders only the GUI deep link until the zcp storage verb exists
  (verb shape is yours, brief §8.3).
- **D5 — Roboto on iOS** beside SF in system-drawn bars — decide on real showcase captures;
  fallback: Roboto only inside cards and the feed.
- **D6 — Data Console v1 read-only** in the embed.
- **D7 — Service-type icons in map rows:** ship without (faithful to the GUI's cards).
- **D8 — OpenCode** in the coding-agent list or not.
- **D9 — No `packages/brand`**; fixed tokens in `packages/shared/src/brand.ts`.
- **D10 — Generator last** (P4), product first (P1) — overturning the winning proposal's order.
- **D11 — Desktop SSH launch:** `fork.md` keeps it, brief S5 removes it.
- **D12 — Registration on a container origin:** Turnstile hostname allow-list from the platform,
  or the app.zerops.io hand-off stays the only sign-up path.
- **D13 — Mobile scope:** ship the phone as "watch, answer, nudge" (this concept) or hold it
  until S5/S7 land — nothing on the phone is buildable before the `client-runtime` promotion.

---

## 12. Risks

1. **P1 lands and it still reads as T3.** The hero composer and the folder-shaped project row
   stay. Mitigation: five glance points (mark pill + ⌘K pill, the band, `● UPPERCASE` on every
   row, the mint panel, tinted process-step cards); P2's picker is a literal GUI page. Residual,
   accepted: a T3 user recognises the bones; a Zerops user recognises the surfaces.
2. **The door on the container origin touches S4's files.** The two UI gates are small and
   additive (§2.6), but they sit in `PairingRouteSurface`, `-chatIndexView.ts` and four route
   files that share one two-status test; widening one without the others splits behaviour.
   Mitigation: one slice, coordinated with S4, live-retested on `z3-eval` from both the
   container origin and a hosted build; the manual one-time-link fallback outside Zerops
   projects stays a test; interim copy says "Connect with a one-time link", never "Pair".
3. **The generator is an L.** Six heterogeneous outputs; one single-target precedent. It is P4;
   if it slips, nothing visible slips with it.
4. **Opaque roles vs an alpha-heavy source.** Flatten per declared surface, record the surface,
   express hover as roles.
5. **The pink flash.** Fallbacks point at `ZEROPS_THEME` in the registering slice.
6. **Contrast and CVD.** Every ratio in §5 is a failing assertion; DS-4 makes dot + word a test.
7. **Drift by a thousand literals.** Zero tolerance in Zerops directories at P2, allowlisted
   repo-wide at P4; every UI brief says "tokens only" and the lint enforces it when the brief
   forgets.
8. **Feed liveness is unproven (Z3F-8); a default-open map that lags lies.** The liveness line
   is always rendered; a service created in the GUI appearing without refresh is the S6
   acceptance test.
9. **The mint panel promises what S7 has not built.** v1 rows are read-only with "Authorize in
   Zerops ↗"; SSH / Desktop IDEs are labelled link-outs; the four-pill grammar stays for S7 to
   fill — and S7's `ZeropsAgentAuthCard` on `wt/s7-web` is the tray's future occupant.
10. **Mobile is on paper.** Thin by design; every piece rides on promoted logic; the showcase
    harness catches a wrong screen before TestFlight.
11. **Performance.** One webfont (self-hosted, subset, preloaded); no Material Icons font; the
    band and map add no `backdrop-filter`; rows memoise; only `steps()` keyframes; the timeline
    stays virtualised.
12. **A theme library that can un-Zerops the product.** The Zerops surfaces read tones from
    `brand.ts`, so the grammar survives any theme; "Reset to Zerops" is one click; the
    user chose it.

---

## Appendix — pointers

- Research: `plans/z3-ui-redesign-2026-08-29-research/` — `R1` GUI design language · `R2` web ·
  `R3` mobile · `R4` desktop · `R5` cross-client tokens (57-role mapping with contrast ratios) ·
  `R6` brand/naming/constraints · `R7` live tokens light + dark · `F1` agent-auth continuity
  (S7 worktrees) · `F2` bundle topology · `F3` thread/composer anatomy + gap matrix vs brief
  §7/§8 · `F4` theme customization scope + colour paths outside the roles · `F5` live capture
  (dark tokens, first screen) · `critic.md` · `proposals/` (four angles) · `judges.md` ·
  `synthesis.md` · `verification.md` (16 claims, 37 agents).
- Screenshots (outside the repo): `~/.zerops-dev/z3-ui-redesign-2026-08-29/shots/`
  — `gui-nonmember/` (app.zerops.io light/dark/390 px), `t3-live/` (the fork's client, local, with
  a real thread), `t3/` (T3 marketing).
- Live token dump: `research/zerops-gui-theme-tokens-computed.json` (light + dark + diff).
- Specs the concept extends: `docs/spec-z3.md` §2–§7; brief `plans/z3-brief-2026-08-28.md` §5,
  §7, §8; fork rules `../z3/docs/internals/zerops/fork.md`.
