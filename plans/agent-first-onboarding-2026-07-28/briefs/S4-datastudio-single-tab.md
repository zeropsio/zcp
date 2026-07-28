# Slice brief: S4 — Data Studio single-tab conversion

Self-contained. Cite spec §s, never the plan. Repo: /Users/macbook/Documents/Zerops-MCP/zcp.
Depends: none (independent of the welcome-template chain).

**Outcome** (observable): the Data Console is one singleton WebviewPanel per workspace with
in-tab service switching; the sidebar card-list subsystem is deleted; the Studio extension
contributes `zcpStudio.open` + `zcpStudio.openService`; the activity-bar icon opens the tab
via a minimal stub view that collapses on every reveal; the embedded SPA's service rail is
ALWAYS visible (deep-linked or not).

**Allowed scope**
- Files (write-set), EXACT — under `internal/dataconsole/extension/templates/vscode-studio/`:
  - MODIFY: `package.json` (commands + stub view + version), `extension.js` (stub view
    implemented INLINE here — no new lib file; command registration; provider reduced to the
    stub), `lib/handlers.js` (router shrinks), `lib/consolePanel.js` (entry funnel)
  - DELETE: `lib/webviewSession.js`, `lib/cards.js`, `cards/managed.js`, `cards/refresh.js`,
    `handlers/refresh.js`, `handlers/console.js`, `lib/discoverToUIMap.js`,
    `lib/svc-icons.js`
  - Outside the template dir: MODIFY `internal/dataconsole/console/webui/dist/dc-embed.js` +
    `internal/dataconsole/console/webui/dist/app.js` (the :347-350 call site),
    `internal/dataconsole/console/webui/spa/dc-embed.test.js`,
    `internal/init/adapters/studio.go` + `studio_test.go` (version bump)
  - `internal/dataconsole/extension/studiojs/`, exact dispositions (verify each against
    actual imports before acting; a disposition that proves wrong is a report note, not a
    silent deviation): DELETE `webview_session.test.js`, `managed.test.js`,
    `refresh.test.js`, `console.test.js`, `discover_to_uimap.test.js`,
    `shell_render.test.js` (tests of deleted files); UPDATE `discovery_contract.test.js`
    (cards enumeration dies, handlers enumeration shrinks) + `consolepanel.test.js` (extend
    with the reveal+switch and entry-point pins); NEW `stub_view.test.js`; KEEP
    `browserdownload.test.js`, `consoleclient.test.js`, `transport.test.js` green
  - `lib/transport.js`: check consumers first — modify/delete ONLY if its remaining
    consumers are all in the deleted set; EITHER outcome (touched or untouched) is stated
    explicitly in the report with the consumer list
- Explicitly excluded: `internal/dataconsole/console/server/**` and broker/safety code (the
  write-token posture is untouched — `TestWriteToken_DualClient_CallerBound` must stay
  green), `internal/content/**` (the agent panel's Data Studio box is S5).

**Spec citations**: `docs/spec-dataconsole.md` §2 invariant 7 (one embedded surface, no
populated sidebar), §4.4 (singleton reveal+switch; rail always visible embedded; entry
points incl. stub view; deletion list; reload = re-entry, no serializer).

**Load-bearing code facts** (verified 2026-07-28):
- The panel is ALREADY a workspace-keyed singleton: `createConsolePanelManager`
  `lib/consolePanel.js:131-283`, `show()` :184-270 — existing live entry is revealed not
  recreated (:190-210), broker rebound + write-mode carried (:198-202), switch via
  `{type:"dataconsole-switch-service", service}` (:205), fresh panel :212-217
  (`retainContextWhenHidden:true`), key = `sessionKey(workspaceRoot)`
  `lib/consoleSession.js:22-24`. Your job is to PIN this (tests), not build it.
- `shouldHideServiceRail` lives in the SPA at
  `internal/dataconsole/console/webui/dist/dc-embed.js:17-26` (call site `dist/app.js:347-350`;
  seven existing cases `spa/dc-embed.test.js:15-45`). **`dist/` IS the source** — the SPA is
  framework-free with NO build step (`webui/embed.go:1-2`, `//go:embed dist` :12); edit the
  dist file directly. New rule (§4.4): embedded → rail ALWAYS visible; a deep link only
  preselects. Standalone keeps existing behavior.
- Sidebar subsystem to DELETE — verify against
  `plans/research/datastudio-single-tab-2026-07-28.md` §2A (list confirmed against the tree
  2026-07-28): the `resolveWebviewView` provider portions of `extension.js` (:49-82, :85;
  `VIEW_ID = "zcpStudioView"` :22), `lib/webviewSession.js`, `lib/cards.js`,
  `cards/managed.js`, `cards/refresh.js`, `handlers/refresh.js`, `handlers/console.js`,
  `lib/discoverToUIMap.js`, `lib/svc-icons.js` — the stub view lives INLINE in
  `extension.js` (a ~short resolveWebviewView that fires `zcpStudio.open` + collapses,
  single-flight; no webviewSession dependency). SURVIVES (§2B + verified): `lib/consolePanel.js`,
  `lib/consoleSession.js`, `lib/consoleClient.js`, `lib/consoleRoutes.js` (GENERATED, required
  by `consoleClient.js:18`, pinned by `studiojs/consoleclient.test.js:6`),
  `lib/browserDownload.js`, `lib/transport.js` (check its consumers before deleting anything
  it serves).
- Manifest (`templates/vscode-studio/package.json`): today contributes ONLY
  `viewsContainers.activitybar` + `views.zcpStudio` and NO commands — `zcpStudio.open`
  (no target → rail's last/first browsable service) and `zcpStudio.openService` (hostname
  arg; missing hostname falls back to no-target, never an error dialog) are NEW
  contributions. Multi-root resolves the workspace the way the session manager already keys
  it.
- Stub view (§4.4): VS Code cannot open an editor panel from a bare activity-bar item
  (vscode#149556) — keep a minimal webview view that executes `zcpStudio.open` and collapses
  the sidebar on EVERY `visible=true` transition (not only first `resolveWebviewView`),
  behind a single-flight guard (click storm → one panel). A brief flash is unavoidable
  (vscode#152382); the contract is "no *populated* sidebar".
- Deleted with the sidebar: the card list's refresh/live-watch machinery (`zcp studio
  watch`-driven topology sync) and the sidebar webview session. Topology freshness becomes
  per-load `/api/services` + manual refresh (§4.4) — no push indicator.
- Version bump: `studioExtVersion` `internal/init/adapters/studio.go:21` (currently "0.1.1")
  + `templates/vscode-studio/package.json` `version`, SAME commit as the first template edit
  (`TestStudioExtVersion_ParityWithPackageJSON` studio_test.go:18).
- No `WebviewPanelSerializer` — reload = re-entry from any entry point; the per-workspace
  child process may outlive the panel and is reused (existing behavior, keep).

**RED test list** (studiojs = `node --test internal/dataconsole/extension/studiojs/`):
- singleton reveal+switch: `zcpStudio.open` twice → ONE panel + a
  `dataconsole-switch-service` message, write-mode carried — studiojs
- `zcpStudio.openService` with unknown/missing hostname → no-target open, no error dialog —
  studiojs
- stub view: collapse+forward on first click, on click-after-collapse, and with the panel
  already open; single-flight under a click storm — studiojs
- rail flip: embedded + deep link → rail visible, target preselected; embedded no-target →
  visible; standalone unchanged — `spa/dc-embed.test.js` (update the seven cases + add)
- Go: `TestStudioExtVersion_ParityWithPackageJSON` green post-bump; extension template
  packaging tests (`go test ./internal/dataconsole/... -short`) green after deletions.

**Protocol**: RED → GREEN → REFACTOR, one named test at a time.
`node --test internal/dataconsole/extension/studiojs/<file>` /
`go test ./internal/dataconsole/... -run <Name> -short -count=1 -v`. Then `make lint-fast`.

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
repeated unexplained check failure. Specifically: any change to broker/safety/server code is
scope drift — halt.

**Definition of Done**
- [ ] RED replay: fails at slice base SHA, passes at slice head
- [ ] Named tests pass; full studiojs + spa + `go test ./internal/dataconsole/... -short` green
- [ ] `TestWriteToken_DualClient_CallerBound` untouched and green
- [ ] `make lint-fast` clean
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full
