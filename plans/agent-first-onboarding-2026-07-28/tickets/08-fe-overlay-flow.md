# 08 — FE overlay flow (agent pick + auth + bridge state machine)

- `status:` closed
- `type:` grilling
- `assignee:` krls2020 (session 2026-07-28)
- `blocked-by:` 02

## Question

Scope amendment from ticket 01: the FE side lives in `../frontend-legacy` and is ours. Design
the fullscreen onboarding flow built on the shipped ZCP-pool claim overlay machinery
(`zcp-pool-claim-base.*`, `ZcpClaimOverlayService` — today dismissed by the code-server iframe's
`load`, newly by *agent-ready*):

1. **Overlay content**: agent pick (which agents, from what source — `ZCP_AGENTS` via the
   announce payload?), the auth step per agent (existing auth-dialog machinery), progress states
   while waiting for agent-ready.
2. **FE bridge state machine**: capture announce → retain `ev.source` → send mode directive /
   launch-agent → retry on re-announce until agent-ready (contract from ticket 02). Where does
   the wizard's one-shot state live in FE terms (store, cookie, platform state) — i.e. what
   makes the FE not re-send after a completed onboarding, including the closed-tab-mid-flow
   case?
3. **Overlay drop**: on agent-ready; timeout/failure fallback so the user is never stranded
   (composes with ticket 02 §4).

## Answer

Resolved 2026-07-28 (grilling with owner, after a code study of the shipped claim flow in
`../frontend-legacy-bridge`, branch `kh-zcp-agent-auth-bridge`). Owner's closing directive:
implement this as architecturally clean, robust, and by-design correct as possible, within the
existing FE and zcp conventions.

### 1. Skeleton — the wizard is a richer layer over the shipped claim flow

The onboarding wizard **replaces the dumb spinner cover** of the ZCP-pool claim flow; everything
else about the skeleton stays. Verified mechanics being reused: `?zcp=true` on login/registration
→ `claimZcpPool` cookie (10 min, survives OAuth redirects) → on `storeUserDataSuccess` + pending
cookie the cover goes up instantly → behind it the drain waits for the ZCP stack + `-zagent`
userData, then `prewarm(stack.id)` → the app-root `CodeServerOverlayFeature` opens the embed
**in the background behind the layer** (detached fullscreen on `/project/:projectId`, no dock
placeholder there). The embed therefore boots — and announces `embed-ready` — *in parallel with*
the wizard's pick/auth steps; announce never blocks any wizard step.

Superseded and deleted (no-backward-compat preference): the iframe-`load` + 3 s dismissal
fallback, the 45 s reveal backstop, and the dead-letter `ZCP_VSCODE_READY_MESSAGE` listener
(defined today, emitted by nothing, validated against no origin) — dismissal is now the wizard's
own state machine ending in `agent-ready`.

Standard (non-onboarding) visits are untouched: control-plane page keeps "Click to start
editing" → embed opens docked → the in-vscode panel appears per container rules (ticket 01 §3);
the FE answers every `embed-ready` outside an active wizard with `set-mode standard`.

### 2. Entry & one-shot — explicit entry only; abandonment is deliberately unhandled

The wizard shows **only on explicit entry**: the registration cookie drain, and the dev entry
(ticket 09). Never derived from authorized-agents state. A user who abandons mid-wizard (closed
tab; cookie already drained, zero agents authorized) is **not recovered**: they return via the
standard path and authorize from scratch through the panel's Authorize row — the recovery
deliberately has **no "Onboard me to Zerops" prompted-launch state** (owner). Skip lives on the
pick step only ("Skip for now": sends `set-mode standard`, drops the layer, leaves the embed as
is); once auth completes the flow runs to the end on its own.

### 3. Wizard state machine

`claiming` (drain resolving stack + userData; embed not yet open) → `picking` (roster shown;
embed prewarming in parallel) → `authorizing` (existing auth dialog over the layer) →
`launching` (waiting state; `launch-agent` sent, 30 s timeout) → `done` (layer drops) |
`failed` (Continue → close everything). Skip exits from `picking`.

### 4. Agent pick

- **Roster = `ZCP_AGENTS` only**, rendered immediately from the FE's own `-zagent` userData
  (already subscribed by the drain before prewarm); the announce payload confirms/refreshes but
  is never waited on. Offering anything outside `ZCP_AGENTS` would be a lie — the container's
  agentId gate rejects it.
- **Single-select**: one agent is picked, authorized, and launched with the onboarding prompt.
  No multi-auth queue in the wizard (the dialog's walker stays for other surfaces); remaining
  agents authorize later from the panel.
- **No roster editing** in the wizard (adding an agent = userData write + service restart —
  panel/service-card territory, not onboarding).

### 5. Auth step — existing machinery, zero rebuild

The wizard dispatches `zcpAgentAuthDialogActions.manualOpen` (same path the bridge accept uses
today) and the **existing `zcp-agent-auth-dialog` opens over the wizard layer unchanged** — its
chrome, terminal-driven CLI OAuth driver, handlers, and outputs (`markAuthorized` /
`setApiToken` userData writes) all stay as shipped. The architecture invariant holds: trigger →
FE, FE resolves auth in its dialog, FE writes the env. The wizard learns of completion from its
own store (the FE always knows its own auth outcome — ticket 02: no bridge state-sync; the
container keeps reading envs for display and never gates launch on the flag). `manualOpenResult
ok:false` routes to the wizard's failure state; the dialog machinery is not touched.

### 6. Launch — automatic, no CTA; ready = S2 unchanged

When the wizard sees the picked agent's authorization complete, it **auto-sends `launch-agent`**
(fresh `eventId` = this intent) and enters the waiting state — no "Onboard me" button. The
overlay drops on `agent-ready`, revealing fullscreen vscode with the maximized terminal running
the agent with "Onboard me to Zerops.". Owner explicitly confirmed ticket 02's S2 semantics: no
waiting for the agent's API response — the command visibly dispatched into a live terminal is
the ready moment.

### 7. Failure & timeout — one honest state, one button

`launch-failed` (pre-dispatch: `unknown-agent` / `terminal-error` — no special branch per
reason) and the FE's own **30 s timeout** from `launch-agent` send (dead embed / no answer)
converge on a single failure state: short copy + one **Continue** button. Continue closes the
wizard layer **and the code-server overlay**, landing the user on the project detail page in
the Zerops GUI — never a broken fullscreen iframe, never stranded. No retry button, no silent
auto-reveal: recovery is the standard path (click-to-start → panel), consistent with §2.

### 8. Architecture homes (FE conventions)

- **Wizard state = signals service in root** — evolution of `ZcpClaimOverlayService` (same
  pattern: transient UI, no app state; it already owns cover visibility + prewarm). The cookie
  drain effect stays, raising the wizard instead of the dumb cover.
- **Announce/inbound listener stays in `CodeServerOverlayFeature`** — extending the shipped
  bridge listener (while-open, outside the Angular zone, origin + iframe-identity walk +
  freshness + eventId dedup) with the new embed→FE types (`embed-ready`, `agent-ready`,
  `launch-failed`), routed into the wizard service. Rationale: announces arrive on every embed
  boot, wizard or not — the listener belongs to the embed, and the FE must answer `set-mode`
  regardless.
- **Retained `ev.source` + origin live in the service, never in an ngrx action** (same
  discipline as `#pendingBridgeAcks` — Window refs stay out of the store). `set-mode` is
  re-sent on every `embed-ready` with the value derived from wizard state; `launch-agent`
  retries with the same `eventId` on re-announce per ticket 02.
