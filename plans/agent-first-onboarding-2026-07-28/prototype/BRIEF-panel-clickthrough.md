# Brief: Panel UX click-through prototype (wayfinder ticket 06)

Build ONE self-contained HTML file:

`plans/agent-first-onboarding-2026-07-28/prototype/panel-clickthrough.html`

It is a **throwaway UI prototype** of the new "Zerops panel" — the reduced welcome surface of a
VS Code (code-server) webview panel. It renders **three structurally different variants** of the
panel, switchable via a floating bottom bar and `?variant=` URL param, plus a **state simulator**
drawer that flips the panel through realistic states. No build step, no external requests, no
frameworks — plain HTML/CSS/JS in one file, opened via `file://` in a browser. Dark theme only.

Top-of-file comment (required):
`PROTOTYPE — wayfinder ticket 06 Panel UX. Three variants of the reduced Zerops panel; switch via ?variant= or the floating bar. Throwaway; never shipped.`

## What the panel is

A single webview panel inside code-server showing, top to bottom (order fixed by prior
decisions — every variant keeps Data Studio at the top):

1. **Data Studio entry** — opens the data browser tab (stub action here).
2. **Coding agents** — five rows, each an agent the user can launch or authorize.
3. **Skill packs + Zerops Guided** — workspace setup: installable skill packs and the Guided
   toggle.

## Domain model (shared by all variants — implement as one JS state object + render functions)

### Agents (fixed order)

```js
const AGENTS = [
  { id: "claude-code", label: "Claude Code",  glyph: "C", hasExtension: true },
  { id: "codex",       label: "Codex",        glyph: "X" },
  { id: "antigravity", label: "Antigravity",  glyph: "A" },
  { id: "grok",        label: "Grok Build",   glyph: "G" },
  { id: "cursor",      label: "Cursor CLI",   glyph: "R" },
];
```

Per-agent state (cycle order for chip-clicks):
`authorized → unauthorized → not-installed → authorizing → reconnect → authorized …`

Row rendering per state (copy is FINAL — use verbatim; it is written for a developer seeing
the surface for the first time, never internal vocabulary):

- **authorized** — status text `Ready`. Actions: primary button `Open terminal`; for
  `hasExtension` agents also secondary button `Open extension`.
- **unauthorized** — sub-line `Connect your account to use this agent here.` Action: primary
  button `Authorize`. Clicking it moves the row to `authorizing` (the simulator then advances
  phases on a timer: 1.2 s per phase, then lands on `authorized`).
- **not-installed** — sub-line `Not included in this container.` No actions (informative row,
  visually muted).
- **authorizing** — three sub-phases with status text, no actions (show a small spinner or
  pulsing dot): `Contacting the Zerops dashboard…` → `Finish signing in in the Zerops dialog…`
  → `Finishing authorization…`.
- **reconnect** — status text `Can't reach the Zerops dashboard — retrying…` + secondary button
  `Try again` (re-enters `authorizing`).

### Packs

```js
const PACKS = [
  { id: "matt-pocock-skills", label: "Matt Pocock's Skills", repo: "mattpocock/skills",
    desc: "TypeScript, AI SDK, and dev-workflow skills", granular: true },
  { id: "superpowers", label: "Superpowers", repo: "obra/superpowers",
    desc: "TDD, systematic debugging, review, and planning", atomicNote: "Installs as one set — 14 skills." },
  { id: "andrej-karpathy-skills", label: "Andrej Karpathy's Skills", repo: "multica-ai/andrej-karpathy-skills",
    desc: "LLM/ML research and explanation skills" },
  { id: "anthropic-skills", label: "Anthropic Skills", repo: "anthropics/skills",
    desc: "Document, data, and productivity skills from Anthropic" },
];
```

Per-pack state (cycle order for chip-clicks):
`absent → installing → installed → subset (matt only; skip for others) → incomplete → modified → broken → retired → removing → absent …`

Pack row rendering (toggle switch on the right, like a settings row; state copy verbatim):

- **absent** — toggle off, no status line.
- **installing** — toggle disabled, status `Installing…` (spinner).
- **removing** — toggle disabled, status `Removing…`.
- **installed** — toggle on, status `Installed`.
- **subset** (Matt only) — toggle on, status `9 of 22 skills installed`, extra secondary button
  `Customize` (opens the picker, below).
- **incomplete** — toggle on (warn tint), status `Some files are missing — reinstall to repair.`
- **modified** — toggle on (warn tint), status `Changed locally — reinstall to reset.`
- **broken** — toggle replaced by secondary button `Remove`, status
  `Needs cleanup — remove and reinstall.`
- **retired** — toggle on (muted), status `No longer offered — you can remove it.`

**Matt picker** (granular selection — only Matt gets this). Opening `Customize` (or turning
Matt's toggle ON from absent) reveals the picker. Content:

- Two category groups with per-skill checkboxes and a per-category `Select all` control:
  - **Engineering (17)**: ask-matt, diagnosing-bugs, grill-with-docs, triage,
    improve-codebase-architecture, setup-matt-pocock-skills, tdd, to-spec, to-tickets,
    wayfinder, implement, prototype, research, domain-modeling, codebase-design, code-review,
    resolving-merge-conflicts
  - **Productivity (5)**: grill-me, grilling, handoff, teach, writing-great-skills
- `setup-matt-pocock-skills` is pre-checked by default when opening from absent state.
- A live pending summary: `N to add · M to remove` (compute vs the currently-installed set).
- Buttons `Apply` and `Cancel`. Apply → pack goes `installing` → lands `subset` with the new
  count. In the "Pack trouble" scenario, the FIRST Apply instead shows a banner inside the
  picker: `This selection changed somewhere else — reloaded it.` and re-renders (second Apply
  succeeds).

### Zerops Guided (sits with the skills section in every variant)

- Title `Zerops Guided` + pill chip `Experimental · Claude Code only`.
- Description (verbatim, reuse shipped copy): `Turns a plain request into working software: the
  agent aligns on what you need, proposes the smallest fitting Zerops service set, asks your
  consent, then builds a PRD and implements it in thin, tested vertical slices at a live dev
  URL. PRDs persist — follow-up sessions start faster.`
- Toggle switch. If Claude Code is not `authorized`, toggle disabled + note
  `Authorize Claude Code first to use Zerops Guided.`
- Note under it (always): `A running agent session keeps its previous instructions — start a
  new session after changing this.`

### Data Studio entry

- Title `Data Studio`. Description: `Browse and edit the data in this project's databases.`
- Action: primary button `Open Data Studio` (stub — flashes a toast `Would open the Data Studio
  tab`). Small sub-line listing the project's data services: `db · PostgreSQL 17` (one service).

### Panel header (all variants, small)

- Zerops wordmark/logo placeholder (a teal rounded square with "Z" is fine) + title `Zerops`
  + muted subtitle `zcp — localflow`.

## The three variants (must be STRUCTURALLY different — different layout and hierarchy, not
different colors)

### A — "Stack"

Single centered column, max-width 720px. Order: header → Data Studio as a slim full-width
entry card → section `Coding agents` as a vertical list of full rows (glyph, name, status,
actions right-aligned) → section `Workspace setup` (Guided row first, then the four pack rows;
Matt's picker expands INLINE inside its row, pushing content down). Calm settings-page feel;
scroll reaches everything.

### B — "Rail"

Two panes. Left rail (~280px): Data Studio entry pinned at the top as a rail card, then the
five agents as compact rail items (glyph + name + small state dot; click selects), then one
rail item `Skills & Guided`. Right pane shows the SELECTION: an agent → a detail card (big
title, plain-language status sentence, large action buttons, and for unauthorized a short
explanation `Connect your account to use this agent here.`); `Skills & Guided` → the guided row
+ pack list (Matt picker expands inline). Master–detail; one thing in focus at a time. Default
selection: first authorized agent, else first agent.

### C — "Deck"

Dense dashboard, max-width 980px, minimal vertical space (target: no scroll at 900px viewport
height). Top row: two side-by-side cards `Data Studio` and `Zerops Guided`. Below: card
`Coding agents` where each agent is ONE line (glyph, name, tiny status chip, single primary
action on the right; Claude's `Open extension` moves into a `⋯` overflow menu). Below/beside
(2-col grid if width allows): card `Skill packs` with compact rows (title + toggle only; status
as a tiny chip). Matt's `Customize` opens an in-panel modal overlay (dimmed backdrop) instead
of inline expansion.

## State simulator (shared chrome, obviously NOT part of the design being judged)

Fixed top-right collapsible drawer labeled `⚙ States` (high-contrast, small). Contents:

- **Scenario preset buttons** (apply full state):
  1. `First return` — claude authorized; all others unauthorized; packs absent; guided off.
  2. `Mixed workspace` — claude + codex authorized; antigravity unauthorized; grok
     not-installed; cursor unauthorized; matt subset (9/22), superpowers installed, karpathy
     modified, anthropic absent; guided on.
  3. `Authorize in flight` — like First return but antigravity `authorizing` (auto-advancing
     phases, looping).
  4. `Dashboard unreachable` — like First return but cursor `reconnect`.
  5. `Nothing authorized` — all unauthorized; packs absent; guided off (locked note visible).
  6. `Pack trouble` — matt broken, superpowers installing, karpathy removing, anthropic
     incomplete; agents like Mixed; picker conflict armed (see picker above).
- **Width toggle**: buttons `420` / `720` / `1100` / `Fluid` — the panel renders inside a
  centered frame of that width (with a subtle dashed outline suggesting the webview edge).
- Hint line: `Click any status chip/dot to cycle that row's state.` (Status chips/dots on agent
  and pack rows cycle states on click, in the cycle orders above.)

Default on load: scenario `Mixed workspace`, width `720`, variant `A`.

## Floating variant switcher (shared chrome)

Bottom-center fixed pill, high-contrast (obviously not part of the design): `←` `A — Stack` `→`.
Arrows cycle with wraparound; `ArrowLeft`/`ArrowRight` keys cycle too (NOT when an input,
textarea, or contenteditable has focus); current variant reflected in `?variant=A|B|C` via
`history.replaceState` and read on load.

## Design language (match the real surface)

Use these tokens (from the shipped welcome webview — include as CSS custom props with the same
fallbacks; the file runs standalone so fallbacks always apply):

```css
:root {
  --teal: #2fb3a3; --teal-2: #24a492; --teal-bright: #3dd6c2;
  --teal-dim: rgba(47,179,163,.14); --teal-ring: rgba(47,179,163,.55);
  --fg: #cccccc; --fg-mut: #9d9d9d; --fg-hi: #ffffff;
  --bg: #1e1e1e; --card: #252527; --card-hi: #2c2c2e;
  --bd-soft: #343436; --bd-str: #4a4a4d;
  --mono: "SFMono-Regular", Consolas, monospace;
}
body { background: var(--bg); color: var(--fg);
  font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
```

Idioms to follow (from the shipped surface):

- Cards: `--card` background, 1px `--bd-soft` border, 8–10px radius.
- Toggle switches: 34×19px pill, `aria-pressed`, teal when on (see shipped `.toggle` idiom).
- Glyph tiles: 28×28 rounded square, 1px `--bd-str` border, mono single letter.
- Buttons only — every actionable element is a `<button>`; `:focus-visible` outline
  `2px solid #007fd4`, offset 2.
- Teal is the accent, used sparingly (primary buttons, active states, on-toggles). Warn tint
  for incomplete/modified: a muted amber (e.g. #d7a65f). Errors/broken: muted red (#e07a6a).
- Status chips: small pills, 10–11px, uppercase-ish letterspacing OK.
- `@media (prefers-reduced-motion: reduce)` kills all transitions.
- Section kickers: small uppercase muted labels (see shipped `.section-kicker` feel).

## Copy rules (hard)

Every visible string is written for a developer seeing this panel for the first time. NEVER
show internal vocabulary: no "bridge", "env", "zembed", "probe", "pack-set", "revision",
"eventId", "webview", "container-side". No marketing/positioning sentences. Use the verbatim
copy given above; where a small connective string is needed, keep it plain and factual.

## Quality bar

- All three variants fully render all six scenarios without console errors.
- State changes re-render in place; the simulator drawer and switcher never overlap panel
  content (panel frame gets bottom padding).
- Keyboard: tab order sensible; toggles and buttons operable by keyboard.
- No external network requests of any kind; no fonts fetched; single file.
- Code quality bar is PROTOTYPE: no tests, minimal abstraction, but the state → render loop
  must be one clear function per variant (`renderA(state)`, `renderB(state)`, `renderC(state)`)
  over one shared state object, so behavior stays identical across variants.
