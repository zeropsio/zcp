# 02 — Bidirectional bridge contract (announce, commands, agent-ready)

- `status:` closed
- `type:` grilling
- `assignee:` krls2020 (session 2026-07-28)
- `blocked-by:` 01, 03

## Question

Ticket 01 replaced the env-watch launch trigger with an explicit FE→embed command channel: the
receiver webview announces itself to `window.top` immediately after init, the FE retains
`ev.source` and commands the embed; delivery is retry-until-ready with `eventId` dedup. Pin the
full message contract:

1. **"Ready" as an observable.** What concretely proves "terminal initialized AND agent started
   with the prompt" — terminal creation, shell-integration command-start events, or a bounded
   heuristic? (Facts from ticket 03: the honest ceiling is "shell began executing the command
   line".) What is the earliest honest moment, and what does agent-ready claim exactly?
2. **Message taxonomy & shapes.** Channel/versioning (reuse `@zerops/zcp-agent-auth-bridge` v1
   vs. new channel), and the payload of each message:
   - *announce* (embed→FE, instant after init): what embed state does it carry?
   - *mode directive* (FE→embed): "onboarding" vs "standard" presentation — one message family
     with *launch-agent* or separate?
   - *launch-agent* (FE→embed): agent id, prompt semantics (fixed onboarding prompt vs. sent
     text), eventId.
   - *agent-ready* (embed→FE): agent id, correlation to the command's eventId.
   No secrets in any payload.
3. **Validation posture for inbound commands.** Exact-origin allowlist (incl.
   `ZCP_WELCOME_BRIDGE_ORIGINS`) + version + eventId dedup + freshness — confirm the §4 ack
   posture reversed, and where the dedup set lives (extension host memory? bounded?).
4. **Failure/timeout semantics.** Retry-until-ready is the FE's loop (re-send on re-announce
   until agent-ready). Does the container ever send an explicit "launch failed" (binary missing,
   spawn error), or does the FE own the timeout and the container only reports success? The
   overlay must never hang forever.
5. **Where the contract lives** in the rewritten spec (the § the FE team reads).

## Answer

Resolved 2026-07-28 (grilling with owner). The full message contract, in final form:

### 1. `agent-ready` claims exactly S2 — sent immediately

`agent-ready` asserts: **"the launch command was executed — a terminal exists and the command
line carrying the onboarding prompt was dispatched to it"** (ticket 03's signal 2). Nothing
stronger. It is sent immediately after dispatch — no wait for shell-integration activation
(signal 3 is race-prone, may never fire, and does not distinguish failure any better than S2),
no grace period watching for early exit. A false positive is cheap: the overlay drops and the
user sees a terminal with a visible, recoverable error.

### 2. Channel — reuse `@zerops/zcp-agent-auth-bridge` v1, extended types

Verified before deciding: **no deployed receiver for the channel exists** — the FE receiver
lives only on FE feature branches (`feat/zcp-agent-auth-bridge` etc.), NOT on `origin/devel`;
production app.zerops.io never listens. The container-side sender ships (v9.132.2) but a
broadcast with no receiver is inert; the only live receiver is the febridge test deploy via
`ZCP_WELCOME_BRIDGE_ORIGINS` opt-in. So there is no deployed contract to protect: the new
messages are **new `type` values on the same channel, `version: 1` unchanged**, channel name
kept (owner's call — no rename).

### 3. Message shapes

All payloads credential-free; envelope uniform (`channel`, `version`, `type`, `eventId`
UUIDv4, `createdAt` browser-clock). Outbound (embed→FE) is broadcast `targetOrigin "*"` as
shipped; inbound (FE→embed) is origin-gated host-side.

- **`embed-ready`** (embed→FE, announce): `agents: [{id, authorized}]` in `ZCP_AGENTS` order
  + `bootstrapVersion` (diagnostics). **No `installed` axis** — the probe can lie (0.1.5 PATH
  false-negative) and sending it invites FE gating; all FE-authorizable agents are preinstalled.
  Sent **once per webview init** (FE listens before creating the iframe; any reload re-announces
  naturally). Never repeated as state-sync — auth completion is known to the FE from its own
  dialog.
- **`set-mode`** (FE→embed): `mode: "onboarding" | "standard"`. Separate type, idempotent,
  re-sent by the FE in response to every `embed-ready`. "onboarding" = dark waiting + Explorer,
  no panel; "standard" = container-owned rules apply (env defaults + `hasEditors`).
- **`launch-agent`** (FE→embed): `agentId` only — **text-free**; the fixed prompt
  `"Onboard me to Zerops."` is container-owned (free text over the bridge would be an injection
  surface into a `--dangerously-skip-permissions` agent). Semantics: "launch this agent with
  the onboarding prompt in a maximized terminal."
- **`agent-ready`** (embed→FE): `agentId` + the command's `eventId` (correlation).
- **`launch-failed`** (embed→FE): `agentId` + `eventId` + `reason: "unknown-agent" |
  "terminal-error"` — see §5.

### 4. Retry & idempotence

FE retry (per re-announce, until answered) re-sends the **same `eventId`** with a **freshly
re-stamped `createdAt`** — eventId is intent identity, createdAt is envelope freshness. The
container answers every valid `launch-agent` with **exactly one outcome per eventId**
(`agent-ready` | `launch-failed`), stored and **idempotently re-acked** on duplicates (a lost
first answer must not hang the overlay). A new intent (user "try again", later re-onboard, dev
entry) mints a new eventId — one dedup rule, no special cases.

### 5. Validation posture — shipped §4 ack posture, reversed; NO authorized gate

Webview relay stays a dumb pipe (channel filter + `BRIDGE_RELAY_MAX_BYTES`, origin forwarded
unexamined). Host-side single pipeline for all inbound: `isAllowedGuiOrigin` (incl.
`ZCP_WELCOME_BRIDGE_ORIGINS`) → `version === 1` → **type allowlist** (`set-mode`,
`launch-agent`) → freshness on `createdAt` → eventId dedup. `agentId` gate: known registry id
∧ present in `ZCP_AGENTS` — a command can never start a binary outside the registry. Dedup set:
**extension-host memory, bounded (TTL + cap), storing eventId → outcome** for the idempotent
re-ack; a code-server restart clears it, which is correct (the restart killed the terminals
too, so a fresh retry SHOULD launch again).

**No authorized-flag gate on launch.** The zembed env propagation lags ~5–10 s behind the
GUI's flag write, so gating on the observed flag would typically reject the freshly-authorized
launch. The authority is the origin-allowlisted FE that just performed the auth itself (same
trust model as the shipped ack). Worst case: the terminal shows the agent's own login screen —
visible, recoverable, consistent with ticket 01's "a probe never gates a launch".

### 6. Failure semantics — `launch-failed` pre-dispatch only

- **Pre-dispatch** (container knows for sure): agentId gate rejection or
  createTerminal/relay error → explicit `launch-failed` with reason. No blind FE timeout where
  the container can tell the truth immediately.
- **Post-dispatch** (after `agent-ready`): nothing is messaged — the overlay has dropped and
  the terminal itself is the error surface (`command not found`, agent crash). A late
  `launch-failed` after `agent-ready` would force the FE state machine to back out of a
  terminal state.
- FE owns the overall timeout backstop for a dead embed (no announce / no answer); its
  duration and UX are ticket 08's.

### 7. Contract home

One home: the **bridge section of the rewritten `spec-welcome-mode.md`** — §4.1 envelope +
validation (shared), §4.2 auth trigger flow (shipped mechanics unchanged), §4.3 embed command
channel (the five types above + the one-answer-per-eventId rule + FE retry loop). Exact
numbering is ticket 07's. **No copy of the contract in `frontend-legacy`** — the FE branch
references this section; a second copy would rot.
