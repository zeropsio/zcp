# Slice brief: S7 — E2E harness full-conversation battery

Self-contained. Cite spec §s, never the plan. Repo: /Users/macbook/Documents/Zerops-MCP/zcp.
Depends: S1, S2, S5 landed (container side whole). The LIVE battery additionally needs the
FE lane (S6e) done and runs at ASSEMBLE — your deliverable is deterministic.

**Outcome** (observable): `tools/welcome-bridge-harness` drives and asserts the full §4.3
conversation against a real embedded code-server: announce → `set-mode` → `launch-agent` →
`agent-ready`, plus the failure/idempotence paths; README's contract-mirror section covers
the new types; existing auth-bridge modes stay green.

**Allowed scope**
- Files (write-set): `tools/welcome-bridge-harness/{run.mjs, gui-harness.html, README.md,
  package.json}`, `Makefile` (the `welcome-bridge-e2e` target family only).
- Explicitly excluded: any `internal/**` file — the harness is a consumer; a needed product
  change is a stop condition, not a harness workaround.

**Spec citations**: `docs/spec-welcome-mode.md` §4.1 (envelope), §4.3 (types, one outcome per
eventId, idempotent re-ack, retry semantics), §1.3 (announce on every webview init,
awaiting-mode), §5.1/§5.3 (launch effect: maximized terminal, receiver teardown after
relay-forwarded).

**Existing structure** (verified 2026-07-28): `run.mjs` — config/env :27-39, phase strings
:44-46, `EXPECTED_PAYLOAD_KEYS` :49, static server :58-79, login-in-frame (Partitioned
cookie — authenticate INSIDE the iframe) :118-125, palette-driven welcome open :262-286,
outcome assertion :313-362. `gui-harness.html` — receiver double: channel :101, validation
:222-245, ack :261-272. README documents localhost-not-127.0.0.1 (origin trust + nginx
`frame-ancestors`) and the contract-mirror rule. Modes: `MODE=ack|silent`, `AGENT`,
`HEADLESS`, `HARNESS_ACK_DELAY_MS`, `HARNESS_CLOCK_SKEW_MS`.

S1 already added minimal type support + a `MODE=launch` happy path — extend, don't rewrite.

**Scenario matrix to land** (each a MODE or flag; assert observable outcomes, snapshot the
bridge log):
1. announce-on-init: harness observes `embed-ready` (ordered agents + bootstrapVersion, no
   `installed` field) on every embed reload (reload the iframe, expect a fresh announce).
2. set-mode standard/onboarding: directive delivered; standard on an empty workbench →
   panel appears (frame with panel markup); onboarding → surface stays dark (no panel
   markup painted).
3. launch happy path: `set-mode "onboarding"` → `launch-agent(agentId)` → `agent-ready`
   correlated by the SAME eventId; a terminal appears in the workbench frame carrying the
   agent command (assert via the workbench DOM/xterm text, best-effort).
4. launch-failed: unknown agentId → `launch-failed reason="unknown-agent"`, no terminal.
5. idempotent re-ack: send the same `launch-agent` eventId twice (second with fresh
   createdAt) → exactly one terminal, two identical outcomes (second is a re-ack).
6. eventId reuse with different agentId → no outcome (dropped as malformed), no terminal.
7. 10 s no-directive fallback: never send set-mode → after ~10 s the container applies
   default rules (panel on empty workbench).
- Keep `MODE=ack|silent` (§4.2 auth flow) green unchanged.

**Environment**: needs `ZCP_CS_URL` + `ZCP_CS_PASSWORD` (README shows the zembed extraction
via `ssh zerops@zcp`). The localflow rig container must run the NEW bundle — deploying it is
the ORCHESTRATOR'S job, not yours; develop against unit-level runs and document the live
invocation. Do not embed any secret in any file (README rule: no secrets in this directory).

**RED discipline (valid at slice base)**: the harness gains DETERMINISTIC contract tests —
a stub embed double speaking the OLD (pre-S1) contract, driven by the new scenario
assertions. At your slice BASE these tests FAIL (the drivers/assertions don't exist or fail
against the old contract); at your HEAD they PASS against a NEW-contract stub double and
still FAIL against the old-contract double (prove both, capture both outputs). The LIVE rig
battery is NOT yours — it runs at ASSEMBLE via the documented invocations; your DoD is
deterministic tests + drivers + README.

**Protocol**: per scenario: write the assertion → prove it can fail (old-contract double) →
implement the driver → `node --check run.mjs` + a stub-server smoke run. `make lint-fast`
(Go untouched — still run it to prove the tree).

**BUILD addendum** (embedded verbatim):
- Never batch-write tests: one scenario at a time.
- Independent oracle: expected values come from the spec §/a known-good literal, never
  recomputed the implementation's own way.
- Assert on observable outcomes (bridge log entries, DOM state, phase lines) — never
  harness-internal state.
- One layer-matrix pass line (e2e/harness).
- Lint clean before the slice reports done.

**Report contract**: per-scenario RED evidence + the stub-GREEN output · files touched ·
the exact documented live invocation(s) for ASSEMBLE · independent-oracle note.

**Stop conditions**: scope drift · a material unknown · an acceptance-criteria change · a
repeated unexplained check failure. Specifically: any needed change to `internal/**` — halt
and hand off (that's a product gap, not a harness problem).

**Definition of Done**
- [ ] Every scenario has a driver + assertion + documented invocation
- [ ] Existing `MODE=ack|silent` paths untouched and green (stub run)
- [ ] README contract-mirror section updated (new types, phase strings, payload keys)
- [ ] No secret in any file
- [ ] `make lint-fast` clean
- [ ] Report contract filled in full
