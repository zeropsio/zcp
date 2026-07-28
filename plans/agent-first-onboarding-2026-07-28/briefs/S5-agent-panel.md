# Slice brief: S5 — Agent panel §6 + §7 UI + §11 deletions (walk-through/CTA/kickoff/onboard)

Self-contained. Cite spec §s, never the plan. Repo: /Users/macbook/Documents/Zerops-MCP/zcp.
Depends: S2 landed (receiver lifecycle decides WHEN the panel renders — you build WHAT it
renders), S3 landed (`pack-set` CLI + catalog reporting exist), S4 landed (`zcpStudio.open`
is a real contributed command the Data Studio box executes).

**Outcome** (observable): the singleton surface renders the §6 agent panel — header; Data
Studio compact box top-right (stacks between header and agents at narrow widths); agent rows
mapping EVERY §3 matrix state and §4.2 transport phase to exactly one row state; collapsed
list + `+ Add another agent` expander; skills section opening with the verbatim ownership
line + pack rows + Matt's Customize picker; guided row with toggle. Deleted here, together
with the surface that rendered them (§11): the welcome walk-through, the CTA journey +
`anyRunnable`, `handleOnboard` + the old `{type:"onboard"}` sender, and the ENTIRE
kickoff-wrapper machinery (JS + Go + the .py template). A11y per W15.

**Allowed scope**
- Files (write-set): `internal/content/templates/vscode-bootstrap-welcome.js` + `.html`,
  `vscode-bootstrap-package.json` (version bump), `internal/init/adapters/claude.go`
  (version bump + DELETE `patchVSCodeClaudeWrapper` :520-551, `installKickoffWrapper`
  :553-567, call site :264), DELETE
  `internal/content/templates/vscode-claude-kickoff-wrapper.py`, `claude_test.go` +
  `launcher_test.go` (wrapper assertions out; update the agent-command doc-comment
  provenance — live versions verified 2026-07-28: claude 2.1.220, codex 0.145.0, agy 1.1.5,
  grok 0.2.112, cursor-agent 2026.07.20 — and note the live auto-submit finding),
  `internal/content/welcomejs/`: NEW panel-structure + panel-a11y + picker suites (port
  assertions from the archived `pack_picker.test.js`); DELETE `cta_flow.test.js` AND
  `onboard.test.js` (its seed-helper pins live in `launch_gate.test.js` since S1); SPLIT
  `ui_structure.test.js`; UPDATE `open_agent.test.js` (promptless explicit-mode selection +
  W10 gate inversions — you change `handleOpenAgent`), `state_matrix.test.js`,
  `welcome_panel.test.js`, `welcome_source_pins.test.js` (+ harness seams).
- Explicitly excluded: `vscode-bootstrap-extension.js` (lifecycle — landed in S2; read-only
  for you), §4.3 message semantics (S1's; you may invoke the launch seam, not change it),
  `internal/skillpacks/**` (S3's), `internal/dataconsole/**` (S4's).

**Spec citations**: `docs/spec-welcome-mode.md` §6 (layout, row-state table, collapsed list,
DS box, a11y), §7 (skills surface contract + guided row), §4.2 (transport phases →
"Authorizing"/"Dashboard unreachable" rows), §5.2 (identity gates re-validated host-side per
action — hiding a button is not authority), §5.4 (steady state after failure = plain
authorized row; NO launch-failed row state), §9 (security floor), §11 (deletions);
`docs/spec-skill-packs.md` (picker mechanics; promoted at GATE 1); `docs/spec-guided-mode.md`
(guided toggle contract). Invariants W4, W7, W15.

**Structure contract**: `prototype/panel-clickthrough.html` (variant D) pins LAYOUT +
BEHAVIOR + STATES only — never its pixels; visual design is yours to produce properly.
Copy voice (§6, map-pinned): every line written from the POV of a developer seeing the
surface for the first time — no internal state vocabulary, no architecture-as-explanation,
no positioning statements.

**Load-bearing code facts** (verified 2026-07-28):
- Row states (spec §6 table, verbatim contract): Not authorized → `Authorize`; Locally logged
  in — platform sync pending → none; Authorized/token → `Open terminal` + `Open extension`
  (where registry declares one); Reconnect (flag present + credential absent — matrix state
  ONLY, transport never borrows the name) → `Authorize`; Authorizing
  (contacting/dialog-opening) → none; Dashboard unreachable (no-dashboard/gui-not-ready) →
  `Try again` (fresh trigger); Not installed → informative. Active set = authorized ∪
  authorizing ∪ reconnect. Collapsed list: ≥1 active → active rows + expander revealing the
  rest (toggle to `Hide available agents`); zero active → full list, no expander. Empty
  `ZCP_AGENTS` → honest "No coding agents are enabled for this container". NO onboard action
  exists anywhere (§6).
- `Open terminal` = terminal open mode, NO prompt; `Open extension` = plugin open promptless
  (claude's `opens[0]`, registry vscode-bootstrap-extension.js:48-51) — both through the S1
  launch seam with §5.2 identity gates re-validated per click. `handleOpenAgent`
  (welcome.js:1564-1582) currently uses `reg.opens[0]` — rework to explicit mode selection.
- Deletions (§11) in YOUR files: the multi-step walk-through webview content;
  `handleStartOnboarding` welcome.js:1508-1552 + `CTA_PROMPTS` :1469-1472 + clipboard-first
  delivery (`deps.clipboard.writeText` :1538); `handleOnboard` :1643-1706 + the old
  `{type:"onboard"}` sender in welcome.html (button emit sites, e.g. ~:646/:1647);
  `kickoffMarkerPath` :1603-1605 + `armKickoffMarker` :1607-1617 (+ arm branch :1683-1685);
  the `anyRunnable` aggregate (state_matrix has 2 cases; handshake payload carries it; kill
  every reference); hint/video/tour content. Go side: the wrapper installer pair + call site
  + the `claudeCode.claudeProcessWrapper` settings write — follow the chain, no orphans.
  The legacy launcher files are NOT yours (survives as suppress-fallback).
- Skills section (§7): ownership line VERBATIM: "Skill packs are just a shortcut — this
  workspace is yours, and you or your agent can add skills to it directly at any time."
  Pack rows carry state copy for absent/installing/installed/subset/incomplete/modified/
  broken/retired. Matt's pack gets the Customize picker: rendered from the CLI-reported
  `catalog` field (NEVER a second hard-coded list), default selection, per-category
  select-all, pending "N to add, M to remove" summary, Apply posts the FULL desired set with
  the last-read revision; `conflict` → re-read status + re-render, never silent retry;
  `broken` refuses picker interaction → points at `pack-remove`. `PACKS`/`PACK_IDS`
  (welcome.js:206-207) must keep matching the Go catalog
  (`TestCatalogIDs_MatchWelcomeExtensionAllowlist`, skillpacks/catalog_test.go:73).
- Picker tests: port ASSERTIONS from `git show
  archived/welcome-ux-redesign:internal/content/welcomejs/pack_picker.test.js` —
  re-implemented UI, ported behavior; adapt selectors to your new structure, keep the
  behavioral oracle.
- Guided row (§7 + spec-guided-mode): ON/OFF toggle spawning canonical CLI (`zcp init
  --guided` / `zcp init`), fixed argv, no shell, cwd = selected workspace folder (multi-root
  → picker; none → disabled); disabled under `ZCP_AUTHORING`; one in flight per window;
  dirty AGENTS.md/CLAUDE.md buffers block; success = exit 0 AND marker re-read; partial
  failure honestly reported. `guided_flow.test.js` (28 cases) SURVIVES — keep green, extend
  only if the row structure changes assertions.
- Data Studio box (§6): ONE action executing `zcpStudio.open` cross-extension
  (`spec-dataconsole.md` §4.4); no per-service list; missing/failed Studio extension →
  informative-disabled with one-line diagnostic, never a dead button.
- Welcomejs suite discipline (adversary-verified 2026-07-28):
  - DELETE `cta_flow.test.js` (13 cases — W-CTA dies).
  - SPLIT `ui_structure.test.js` (40 cases): ~half pin surfaces the spec KEEPS — relocate
    (don't lose): the four `data-pack-row` ids + aria/status derivation (W7), guided chip +
    locked note (W6), `AUTH_PHASE_TEXT` (§4.2), the five agent-row ids (W9), no-`innerHTML`
    + nonce pins (§9), and the `handleBridgeSend` `createdAt` re-stamp — the ONLY in-repo pin
    of W5's "stamped by the sending browser context". The journey/tour halves die.
  - `onboard.test.js`: DELETE the whole suite — the final tree keeps no "onboard" suite;
    the seed-helper behavior is pinned in `launch_gate.test.js` (S1). Verify those pins
    still pass after your deletions.
  - `welcome_source_pins.test.js`: keep the `BRIDGE_SUPPORTED_AGENTS`-absence pin; re-derive
    the `EXTERNAL_URLS` count from the §6 panel's actual link set.
  - `state_matrix.test.js`: the §3 matrix survives (incl. Reconnect); the 2 `anyRunnable`
    cases die.
  - `welcome_panel.test.js`: security/a11y floor survives; the CTA live-region case dies;
    extend per W15.
- A11y (W15/§6): on a state-delta re-render, keyboard focus is retained when the focused node
  survives; when it disappears (e.g. `Authorize` → `Open terminal`), focus moves to the
  replacement primary action, falling back to the row container — never dropped to body; the
  row's new state announced ONCE, concisely, via a polite live region.
- Version bump: `BootstrapExtVersion` + template package.json, same commit as the first
  template edit.

**RED test list**:
- `panel_structure.test.js`: every §3 matrix state + §4.2 transport phase → exactly one row
  state (table-driven against the §6 table) · collapsed-list membership + expander toggle ·
  empty-available honest state · DS box informative-disabled without the Studio extension ·
  no onboard action present.
- `panel_a11y.test.js` (W15): focus moves to replacement primary action on state swap ·
  focus retained when node survives · never lands on body · exactly one polite live-region
  announcement per delta.
- `pack_picker.test.js` (ported assertions): renders from reported `catalog` · default
  selection · select-all per category · pending summary counts · Apply posts full set +
  last-read revision · `conflict` → re-read + re-render · `broken` refuses interaction.
- Updated `state_matrix`/`welcome_panel`/`welcome_source_pins` per the split discipline.

**Protocol**: RED → GREEN → REFACTOR, one named test at a time.
`node --test internal/content/welcomejs/<file>`; Go parity via
`go test ./internal/init/... -run TestBootstrapExtVersion -short`. Then `make lint-fast`.

**BUILD addendum** (embedded verbatim):
- Never batch-write tests: RED → GREEN → REFACTOR one named test at a time.
- Independent oracle: expected values come from the spec §/a known-good literal, never
  recomputed the implementation's own way.
- Assert on public seams only (`ops.*` / tool output) — never an internal
  `platform`/`workflow` helper.
- Table-driven, `Test{Op}_{Scenario}_{Result}` naming; one layer-matrix pass line per
  CLAUDE.md-touched layer.
- `make lint-fast` clean before the slice reports done.

**Report contract**: RED output + exit code · GREEN output + exit code · files touched ·
layer-matrix pass lines · independent-oracle note.

**Stop conditions**: scope drift · a material unknown · an acceptance-criteria change · a
repeated unexplained check failure.

**Definition of Done**
- [ ] RED replay: fails at slice base SHA, passes at slice head
- [ ] Named tests pass; FULL `node --test internal/content/welcomejs/` green (incl. survivors
      `bridge_flow`, `guided_flow`, `availability_detection`, `launcher_flow`, `watchers`,
      `diagnostics`, `pack_install`)
- [ ] Grep `anyRunnable|CTA_PROMPTS|start-onboarding|claudeProcessWrapper|claude-kickoff|kickoffMarker|handleOnboard` → zero product-code hits
- [ ] `make lint-fast` clean
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full
