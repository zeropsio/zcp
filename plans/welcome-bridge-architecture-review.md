# Welcome + agent-auth bridge: architecture review backlog (handoff for a fresh session)

Purpose: a POLISH-AND-HARDEN review of the welcome/launcher/auth-bridge architecture — remove
what carries no benefit, tighten the problematic spots below. **Not a rebuild.** The live
behavior WORKS as of zcp v9.132.2 (extension 0.1.8) + frontend `feat/zcp-agent-auth-bridge`
@ 7508662e7 (deployed on febridge) — live-verified end to end. Every proposed change must
justify itself against a working system.

## Ground rules

- No wheel reinvention: the contract home is `docs/spec-welcome-mode.md` (esp. §3 state axes,
  §4 bridge protocol, §8 security floor). Read it FIRST; change the spec WITH the code.
- Every zcp template change ships with a `BootstrapExtVersion` bump (W1) and must keep the
  welcomejs suite green (230 tests: `node --test internal/content/welcomejs/**/*.test.js`).
- The E2E proof rig is `make welcome-bridge-e2e` (tools/welcome-bridge-harness; MODE=ack|silent,
  HARNESS_ACK_DELAY_MS, HARNESS_CLOCK_SKEW_MS) against a real code-server — run it before AND
  after. Dev loop into the live container: `make zcp-dev-deploy` (see CLAUDE.local.md
  "Welcome/bridge test rig" for the localflow rig + febridge deploy recipe).
- The frontend half lives in frontend-legacy, branch `feat/zcp-agent-auth-bridge`
  (receiver: code-server-overlay.{bridge,feature}.ts; dialog: zcp-agent-auth-dialog.effect.ts;
  38 jest cases: `npx nx test zerops --testPathPattern=code-server-overlay`). The harness's
  receiver MIRRORS the FE contract — if either end changes, update both + the harness.
- TDD (RED first). Small verified steps. FE and zcp changes coordinated through spec §4.

## What today's stabilization already fixed (context — details in git log / spec)

- Legacy `ZCP_AGENT_TYPES` launcher deleted → single model over availability (`ZCP_AGENTS`,
  fail-closed) × installed (host∪store PATH probe) × auth matrix; `anyRunnable` launch gate.
- Welcome redesigned (Start/Build/Tour), bridge offered to every available+installed agent,
  GUI receiver = capability authority (all five agents).
- Bridge determinism: browser-clock `createdAt` stamping (webview), FE ±5s skew tolerance
  (the old `age < 0` gate was the marquee randomness), truthful ack (accepted only after the
  dialog actually dispatches; `not-ready` reason; no silent 10s timeout), release-on-accepted
  (dismiss→reclick dead zone), 12s ack window, phases contacting/gui-not-ready, footer
  Embed: GUI/standalone diagnostics, FE bridgeContext readiness (clientId via ProjectEntity,
  iframe never clickable with null context).

## Review backlog (prioritized)

P1 — decided direction, needs coordinated execution:
1. **Drop `createdAt` freshness GATING on the FE receiver entirely** (keep the field on the
   wire + ageMs in the debug log). Rationale (owner-confirmed): the trigger carries no
   authority; the real boundary is origin + source-chain + eventId dedup — anyone passing it
   can mint a fresh timestamp, so the TTL protects nothing and historically CAUSED the
   randomness. With browser-clock stamping the gate is currently harmless — this is pure
   de-ritualization. Update FE boundary tests + harness mirror + spec §4 together.
2. **FE embedded-iframe restart recovery** (Codex audit fix #4, not yet done): after a zcp
   container restart there is no deterministic reload/relogin path for the embedded
   code-server — a stale webview/workbench can linger. Design: detect the dead session
   (iframe load generation / ZCP_VSCODE_READY signal) → visible "Reload IDE" state or forced
   iframe regeneration through `/zcp-auth/<password>`.

P2 — investigate / land:
3. **Land `feat/zcp-agent-auth-bridge` into devel** (PR): it now carries the receiver + all
   determinism fixes; rebased on devel @ 2026-07-22. Owner decides timing.
4. **OAUTH flag lifecycle**: on localflow, `ZCP_AGENT_AUTH_TYPE_CLAUDE_CODE=oauth` exists but
   `ZCP_AGENT_OAUTH_CLAUDE_CODE` never landed despite dialog flows → the welcome renders
   claude as "Locally logged in — platform sync pending" forever. Trace the GUI's
   markAuthorized path (walker success → userData write) and surface failures; decide whether
   zcp's `mark-oauth` should also run on bridge-completed logins (currently terminal-flow-only).
5. **zcp bridge observability leftovers** (Codex diagnostics plan, partially done): observe the
   boolean result of `webview.postMessage`; extension-host/session identifier in the footer;
   "Last bridge attempt" as reason+latency, updated at phase time (today it refreshes on the
   next state push).

P3 — polish:
6. FE `containers[0]` pick → platform-designated container (warn+first today; ZCP is
   single-container by design, so low risk).
7. Harness robustness: command-palette open races (attempt 1 usually loses; retries cover it),
   `sawContacting` is best-effort on fast paths (hard-asserted only with ackDelay>0), footer
   version poll (5s). Consider a real-frontend E2E lane (waits for the Material dialog) —
   today the harness deliberately ends at the receiver contract.
8. Launcher auto-reopen UX: `onEnvChange` reopens the launcher tab on any view-key change —
   reconsider barging into a working editor (maybe only when no editors are open).
9. Terminal login for antigravity/grok/cursor stays OFF until each has a live-verified
   credential artifact path (spec §3/§4 rule — completion must be observable). Don't "enable
   for symmetry".
10. Retire `prototype/zcp-claude-auth-bridge` (frontend) — superseded by the feat branch.

## Removal candidates (cleanliness sweep — verify no consumer first)

- FE `createdAt` gate (P1.1) + the 30s TTL narrative in FE comments/spec.
- welcome.js's own `createdAt` mint (a placeholder the webview overwrites) — keep only if the
  spec keeps documenting the field; otherwise drop with FE coordination.
- Any leftover copy in specs describing the pre-truthful-ack semantics (grep "3s"/"3 s",
  "accepted ack" wording drift across docs/spec-welcome-mode.md).
- Welcome EXTERNAL_URLS: confirm all 11 links still resolve (they were live-verified
  2026-07-22); drop any that product no longer wants.
