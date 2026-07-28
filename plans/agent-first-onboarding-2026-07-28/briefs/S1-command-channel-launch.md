# Slice brief: S1 — Bridge command channel + terminal-only launch (tracer)

Self-contained: no other file is required to execute this. Cite spec §s, never the plan.
Repo: /Users/macbook/Documents/Zerops-MCP/zcp, branch per your worktree (base = integration).

**Outcome** (observable): the container end of the §4.3 embed command channel works: the
welcome webview announces `embed-ready` on init, the host accepts `set-mode` / `launch-agent`
through the full inbound pipeline WITHOUT requiring a live auth flow, executes a terminal-only
launch carrying the fixed onboarding prompt, answers exactly one outcome per `eventId`
(`agent-ready` | `launch-failed`) with idempotent re-acks and the `relay-forwarded` teardown
receipt. All existing auth-bridge (§4.2) behavior stays green — and so does the ENTIRE old
onboarding surface: `handleOnboard`, the old `{type:"onboard"}` sender, and the kickoff
wrapper stay alive and functional (S5 deletes them together with the panel that renders
them; deleting them here would leave a dead visible button).

**Allowed scope**
- Files (write-set):
  - `internal/content/templates/vscode-bootstrap-welcome.js`
  - `internal/content/templates/vscode-bootstrap-welcome.html`
  - `internal/content/templates/vscode-bootstrap-package.json` (version bump only)
  - `internal/init/adapters/claude.go` (`BootstrapExtVersion` bump ONLY)
  - `internal/content/welcomejs/`: NEW `command_channel.test.js`, NEW `launch_gate.test.js`;
    UPDATE `message_allowlist.test.js` (+ `harness.js`/`vscode-stub.js` if the test harness
    needs new seams)
  - `tools/welcome-bridge-harness/gui-harness.html` + `run.mjs` (minimal: validate/emit the
    five new types + one `MODE=launch` happy path; existing modes stay green)
- Explicitly excluded: `vscode-bootstrap-extension.js` (startup branching is S2), panel UI
  content (S5), `internal/skillpacks/` (S3), FE repo (S6 lane). Do NOT rename
  `autoOpenWelcome` or `zerops.welcome` (S2). Do NOT delete `handleOnboard`, the old onboard
  sender, `armKickoffMarker`/`kickoffMarkerPath`, the wrapper installer, or
  `onboard.test.js` — all S5's (they die with the surface that renders them).

**Spec citations**: `docs/spec-welcome-mode.md` §4.1 (envelope/validation), §4.3 (five types,
dedup store, retry/idempotence, relay-forwarded), §5.1 (terminal-only delivery), §5.2 (one
launch-gate rule), §5.4 (post-dispatch silence), §9 (security floor). Invariants W10, W11,
W12. (§11's kickoff/onboard deletions are S5's, not yours.)

**Load-bearing code facts** (verified 2026-07-28, cite-checked):
- The inbound pipeline has FIVE stages you must touch coherently: (1) webview pre-filter
  `vscode-bootstrap-welcome.html:1522-1548` — narrows to exactly six primitive fields
  (`channel,version,type,eventId,accepted,reason`) + 1024 B cap + 20/s flood cap; extend with
  per-type field allowlists (`set-mode`: +`mode`; `launch-agent`: +`agentId`), keep the caps;
  it forwards unknown types today (channel-match only at `:1523`). (2) host shape gate
  `isWellFormedBridgeRelay` welcome.js:945-958. (3) host dispatch switch `handleMessage`
  welcome.js:1713-1758 (`bridge-window-message` at :1752 is the only route in; the NEW
  webview→host `relay-forwarded` receipt needs a new case). (4) flow gate
  `handleBridgeWindowMessage` welcome.js:966-1015 — **the `!authFlow` guard at :967-970 is
  the real blocker**: command types must dispatch through a path NOT gated on a live auth
  flow; the ack path keeps its flow gate. (5) host→webview direction: the six-case if-chain
  welcome.html:1550-1557 needs a case for handing outcomes to the relay.
- Origin/validation pipeline order for inbound commands (§4.1): `isAllowedGuiOrigin`
  (welcome.js:127-147, extras via live zembed store :166-179) → `version===1` → type
  allowlist → `createdAt` freshness → `eventId` dedup.
- Outbound is broadcast `targetOrigin "*"`; `handleBridgeSend` (welcome.html:1499-1512)
  re-stamps `payload.createdAt` on the browser clock at :1510 — every outbound emission
  (announce, outcomes, idempotent re-acks) goes through it and gets a FRESH stamp (§4.1);
  stored outcomes hold semantic fields only.
- **Announce trap**: `sendBridgeMessage` silently no-ops when `panel` is null
  (welcome.js:858); the webview's listener is not installed until it posts `ready`
  (host case at welcome.js:1726). `embed-ready` must be emitted from the `ready` handler,
  never from `open()`. Payload: `agents: [{id, authorized}]` in `ZCP_AGENTS` order +
  `bootstrapVersion`; NO `installed` axis (§4.3).
- Dedup store (§4.3, extension-host memory): record `{agentId, status:"in-flight"}` BEFORE
  the first side effect; duplicate mid-execution coalesces to the one execution's outcome —
  never a second launch; same `eventId` with different `agentId` → rejected as malformed;
  completed outcomes retained ≥2 min with a cap (≥256, oldest-completed evicted first);
  in-flight never evicted; restart clears (correct). Outcome persisted to the store FIRST,
  then handed to the relay; teardown allowed only after the webview's `relay-forwarded`
  receipt (keyed by eventId); if the receiver dies pre-receipt, re-ack from the store on the
  next announce.
- Launch executor (§5.1/§5.2): select `reg.opens.find(o => o.mode === "terminal")` —
  NEVER `opens[0]` (claude's `opens[0]` is the extension; registry at
  vscode-bootstrap-extension.js:44-73, injected via deps). Seed with `seedOpenWithPrompt`
  (welcome.js:1619-1632) + `shellQuoteArg` (:1594) + `ONBOARD_PROMPT` (:1590) — these
  SURVIVE. Gates: known registry id ∧ `ZCP_AGENTS` membership
  (`resolveAvailableAgentIds`, injected) — NO installed-probe gate, NO auth-flag gate (W10;
  zembed lag ~5-10 s is why — do not "fix" it). `agent-ready` = S2, sent immediately after
  dispatch to a live terminal — no shell-integration wait, no grace period. `launch-failed`
  pre-dispatch only, `reason: "unknown-agent" | "terminal-error"`. Post-dispatch: NOTHING
  (§5.4 — no notification, no panel action, no late message).
- `handleOnboard` (welcome.js:1643-1706), `handleOpenAgent` (:1564-1582), the kickoff
  marker fns (:1603-1617, :1683-1685) all SURVIVE untouched — the NEW bridge executor is a
  separate path beside them. Reuse `seedOpenWithPrompt`/`shellQuoteArg`/`ONBOARD_PROMPT`
  (:1590-1632); your `launch_gate.test.js` pins the seeded-argv behavior on the BRIDGE path
  (the old `onboard.test.js` pins stay green untouched until S5 deletes the suite).
- Version bump: `BootstrapExtVersion` (claude.go:32, currently "0.1.18") + the template
  `vscode-bootstrap-package.json` `version` — SAME commit as the first template edit
  (`TestBootstrapExtVersion_ParityWithManifest`; an unbumped edit never reaches a fleet).
- Security floor (§9): `launch-agent` is text-free (agentId only) — reject any text field;
  strict webview→host allowlist; no env value/credential in any payload, log, or DOM.

**RED test list** (JS suites run via `node --test internal/content/welcomejs/<file>`):
- `command_channel.test.js`: one-outcome-per-eventId (dup after completion → same outcome
  re-acked with fresh createdAt) · in-flight recorded before first side effect (dup
  mid-execution coalesces, exactly one launch) · same eventId + different agentId → malformed,
  dropped · completed-store bounds (cap evicts oldest-completed, in-flight never) · restart
  clears store · outcome→relay→`relay-forwarded` receipt gates teardown; receiver death
  pre-receipt → re-ack on next announce · announce emitted from `ready` (not before), payload
  = ordered agents + bootstrapVersion, no `installed`.
- `launch_gate.test.js` (W10): terminal open selected by `mode==="terminal"` never
  `opens[0]` · seeded argv: POSIX-quoted `ONBOARD_PROMPT` appended positionally (or via
  `initialPromptFlag`) exactly as `seedOpenWithPrompt` produces — oracle literals from the
  registry commands · unknown agentId → `launch-failed "unknown-agent"`, no side effect ·
  agentId outside `ZCP_AGENTS` → same · installed-probe result NEVER consulted · auth flag
  NEVER consulted · terminal-creation throw → `launch-failed "terminal-error"` pre-dispatch ·
  post-dispatch failure → no further message (§5.4).
- `message_allowlist.test.js` (update): `set-mode`/`launch-agent` accepted through the
  pipeline with NO live auth flow · non-allowlisted inbound type still dropped · oversized/
  flooded relay still dropped · ack path still requires its flow.
- Go: `TestBootstrapExtVersion_ParityWithManifest` stays green post-bump
  (`go test ./internal/init/... -short`).

**Protocol**: RED → GREEN → REFACTOR, one named test at a time.
1. Write the named test(s) first; confirm they fail for the right reason (assertion, or the
   exact missing-symbol error on a new seam): `node --test internal/content/welcomejs/<file>`
   (JS) / `go test ./internal/init/... -run <Name> -short -count=1 -v` (Go).
2. Implement until green: same command, must pass.
3. Refactor with tests green; re-run; then `make lint-fast`.

**BUILD addendum** (embedded verbatim):
- Never batch-write tests: RED → GREEN → REFACTOR one named test at a time.
- Independent oracle: expected values come from the spec §/a known-good literal, never
  recomputed the implementation's own way.
- Assert on public seams only (`ops.*` / tool output) — never an internal
  `platform`/`workflow` helper.
- Table-driven, `Test{Op}_{Scenario}_{Result}` naming; one layer-matrix pass line per
  CLAUDE.md-touched layer.
- `make lint-fast` clean before the slice reports done.

**Report contract** (all required — never summarize away a failure)
- RED output + exit code (proves the test failed pre-implementation)
- GREEN output + exit code
- Files touched (exact list)
- Layer-matrix pass lines (JS unit + Go unit here)
- Independent-oracle note

**Stop conditions** (halt + handoff, don't push through): scope drift · a material unknown ·
an acceptance-criteria change · a repeated unexplained check failure.

**Definition of Done**
- [ ] RED replay: fails at slice base SHA, passes at slice head
- [ ] Named tests pass with `-count=1 -v` / `node --test`
- [ ] Full `node --test internal/content/welcomejs/` green (regressions: `bridge_flow`,
      `bridge_relay_ratelimit`, `origin_allowlist`, `onboard`, `cta_flow` untouched and green
      — the old surface must keep working beside the new channel)
- [ ] `make lint-fast` clean
- [ ] No file outside Allowed scope touched
- [ ] Report contract filled in full
