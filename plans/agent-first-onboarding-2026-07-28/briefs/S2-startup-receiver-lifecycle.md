# Slice brief: S2 — Startup policy rename + receiver lifecycle + `zerops.panel`

Self-contained. Cite spec §s, never the plan. Repo: /Users/macbook/Documents/Zerops-MCP/zcp.
Depends: S1 landed (command channel + dedup store exist; you extend when the surface boots
and when it may die).

**Outcome** (observable): `startup.json` carries `{"agentFirst": true|false}` (fail-closed);
under agentFirst + embedded (predicate below) the singleton surface boots on EVERY window
init, unfocused, announces immediately, and sits in `awaiting-mode` (dark) until a directive
or a 10 s window; `set-mode "standard"`/expiry applies container rules (empty workbench →
panel; restored editors → self-close); the surface never self-closes mid-intent; the manual
command is `zerops.panel` (self-close-exempt); the legacy Agents-view title button is GONE;
`app.zerops.io` suppress → legacy launcher fallback still works; legacy launcher surfaces are
context-key-hidden while agent-first is active. Standalone always shows the panel.

**Allowed scope**
- Files (write-set):
  - `internal/content/templates/vscode-bootstrap-extension.js`
  - `internal/content/templates/vscode-bootstrap-welcome.js` + `.html`
  - `internal/content/templates/vscode-bootstrap-package.json`
  - `internal/init/adapters/claude.go`, `bootstrap_install_test.go`, `launcher_test.go`
  - `internal/content/welcomejs/`: NEW `receiver_lifecycle.test.js`; UPDATE
    `welcome_dark.test.js`, `handshake.test.js`, `message_allowlist.test.js` (fetches
    `zerops.welcome` by name at :26) and `welcome_panel.test.js` (same at :12)
    (+ harness seams as needed)
- Explicitly excluded: panel UI content (S5), skillpacks (S3), dataconsole (S4), FE (S6),
  §4.3 message semantics (S1 owns them — you may call, not redefine).

**Spec citations**: `docs/spec-welcome-mode.md` §1.1 (agentFirst policy), §1.2 (suppress +
context-key-gated legacy surfaces), §1.3 (receiver lifecycle, embed classification, reload
semantics), §1.4 (`zerops.panel`, manual exemption, ready handshake), §5.3 (receiver tab
closed only after `relay-forwarded`), §11 (`autoOpenWelcome` + `closeSidebar` + command
identity deletions). Invariants W3, W13.

**Owner ruling (FRAME, 2026-07-28)**: the legacy Agents-view title-bar button
(`vscode-bootstrap-package.json:26`, `when: view == zcpAgents`) is **deleted** with the
`zerops.welcome` identity — do not retarget it.

**Load-bearing code facts** (verified 2026-07-28):
- Go write side: `writeBootstrapStartupConfig` claude.go:359-373 (struct field
  `AutoOpenWelcome` :361), derivation `bootstrapAutoOpenWelcome` :347-357 (parseable HTTP(S)
  URL, non-empty host, hostname != `app.zerops.io` → true), call site :312 from
  `os.Getenv("zeropsSubdomain")`. Test
  `TestInstallBootstrap_WritesStartupPolicyFromZeropsSubdomain` bootstrap_install_test.go:156
  (asserts field at :203). Rename field + JSON key to `agentFirst` — no compat shim (§1.1).
- JS read side: `STARTUP_FILE` extension.js:35; `hasInitWelcomeMode()` :96-103 (fail-closed —
  keep that shape); sticky flag `customGuiOnboardingMode` :346, set at :480; the custom-GUI
  branch :481-486 currently opens `zerops.welcome` unconditionally then runs
  `workbench.action.closeSidebar` — the closeSidebar action is §11-DELETED (onboarding wants
  Explorer visible, §5.3).
- `hasEditors` is read ONLY in `showInitial` extension.js:367 (legacy path). §1.3 requires
  `hadRestoredEditors` captured BEFORE the receiver tab is created — hoist the
  `tabGroups` read above the branch at :480; the receiver tab never counts toward it.
- Embed classification (§1.3, live-measured fixtures — use these verbatim):
  predicate `Array.from(location.ancestorOrigins).some(o => o !== self.origin)` evaluated in
  the webview; measured chains: standalone `[cs,cs]` (not embedded), custom-GUI embed
  `[cs,cs,http://localhost:50153]`, app.zerops.io Embedded Editor
  `[cs,cs,https://app.zerops.io]`. `window.top !== window` is true in ALL shapes (useless).
  The §1.2 suppress check (welcome.html:959-974, handler welcome.js:1716-1724,
  `fallBackToLegacyLauncher` extension.js:385-393) reads the same chain — keep it working.
- Receiver lifecycle (§1.3): boot-always unfocused; announce immediately (S1's announce path);
  `awaiting-mode` renders dark, must NOT self-close before a valid directive or 10 s expiry;
  a valid directive cancels the timer; env-derived state never overrides a directive; on
  `standard` (or expiry): `hadRestoredEditors` → self-close, else reveal panel; never
  self-close while a launch intent is in flight (S1's dedup store knows in-flight +
  unforwarded outcomes — consume that, don't duplicate it); reload → fresh announce.
  Standalone: panel always opens rendering content, no awaiting-mode, no announce duty.
- `zerops.panel` (§1.4): registration extension.js:466-478 (sole), execution :482 (sole),
  manifest `vscode-bootstrap-package.json:12` (command) + `:26` (view/title — DELETE).
  Manual invocation → focused, content, self-close-exempt for the panel's lifetime; singleton
  reveal-not-recreate (welcome.js:2113-2119, creation :2127-2132, dispose :2135-2138); no
  serializer (:2109-2111 documents it — keep).
- Rename touch list (complete): claude.go:361 (+`:347-357` fn name), bootstrap_install_test.go:197-203,
  launcher_test.go:217-218, :224, :225, :243-244, extension.js:99, comment claude.go:319.
  `welcome_dark.test.js:78` asserts the ordered pair
  `["zerops.welcome","workbench.action.closeSidebar"]` — BOTH elements change for two
  independent reasons (rename + deletion); fix both, deliberately.
- Legacy-surface hiding (§1.2): the startup tab + activity-bar Agents view get a
  context-key-gated `when` clause (set the key from the same policy read) — hidden while
  agent-first is active, revealed under legacy policy/suppress. Two launch surfaces must
  never render at once.
- Version bump: `BootstrapExtVersion` + template package.json `version`, same commit as the
  first template edit (parity test).

**RED test list**:
- `receiver_lifecycle.test.js` (W13): chain fixtures classify standalone/embedded (three
  measured chains verbatim) · boot-always creates unfocused receiver + immediate announce ·
  `awaiting-mode` ignores env state and does not self-close pre-directive · 10 s expiry →
  container rules applied · `set-mode "onboarding"` → stays dark, no panel paint ·
  `set-mode "standard"` + `hadRestoredEditors` → self-close · standard + empty workbench →
  panel · never self-close mid-intent (in-flight launch) · manual `zerops.panel` → exempt ·
  `hadRestoredEditors` captured before receiver creation (receiver tab not counted).
- `welcome_dark.test.js` (update): `agentFirst` key read fail-closed · custom-GUI branch no
  longer calls `closeSidebar` · command id is `zerops.panel`.
- `handshake.test.js` (update): ready handshake unchanged in shape; announce added at ready.
- Go: `TestInstallBootstrap_WritesStartupPolicyFromZeropsSubdomain` renamed-field variant
  (assert literal JSON key `agentFirst`; app-host/missing/garbage → false).
- `launcher_flow.test.js` must stay green (legacy launcher survives as suppress-fallback).

**Protocol**: RED → GREEN → REFACTOR, one named test at a time.
1. `node --test internal/content/welcomejs/<file>` / `go test ./internal/init/... -run <Name>
   -short -count=1 -v`; confirm RED for the right reason.
2. Implement until green. 3. Refactor; `make lint-fast`.

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
- [ ] Named tests pass (`-count=1 -v` / `node --test`)
- [ ] Full welcomejs run green; `go test ./internal/init/... -short` green
- [ ] `make lint-fast` clean
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full
