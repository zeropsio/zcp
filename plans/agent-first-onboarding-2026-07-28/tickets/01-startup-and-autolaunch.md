# 01 — Startup policy & auto-launch semantics

- `status:` closed
- `type:` grilling
- `assignee:` krls2020 (session 2026-07-28)
- `blocked-by:` —

## Question

One state machine decides what a fresh window does at startup and what the env-watch fires.
Decide:

1. **The launch trigger.** "First agent env set" — is the observed condition the platform flag
   alone (`ZCP_AGENT_OAUTH_<SUFFIX>`/`ZCP_AGENT_TOKEN_<SUFFIX>` via the zembed watcher), or full
   *runnable* (installed ∧ authorized, §3)? Which agent launches when several authorize close
   together — first observed, or first in `ZCP_AGENTS` order?
2. **One-shot semantics.** What records "the onboarding launch already happened", and where
   (a local marker under `.zcp/state/`? extension `globalState`? nothing container-side — the
   FE's overlay presence implying it)? A later authorization of a second agent from the panel
   must never auto-launch a terminal.
3. **Startup branching.** First run (no agent authorized yet): watch + wait, Explorer open,
   nothing else — vs. subsequent visit: auto-open the panel, no launch. Where does the
   onboarding layout (maximized terminal, per the owner screenshot in the charting session)
   get established — at launch time only?
4. **Contexts.** Which embeds run the watch/auto-launch: custom-GUI embed only (the overlay
   flow), or also standalone code-server? The `app.zerops.io` suppress-fallback path stays as
   shipped.
5. **Init policy residue.** What `zcp init` now writes into `startup.json`
   (`autoOpenWelcome` semantics under the new concept).

## Answer

Resolved 2026-07-28 (grilling with owner). The launch model changed mid-grilling: the ticket's
premise (env-watch as the launch trigger) was **superseded** by an explicit FE→embed bridge
command. Decisions, in final form:

### 1. Launch trigger — FE bridge command, never env observation

The onboarding launch fires **only on an explicit `launch-agent` command from the embedding FE**,
delivered over the existing bridge channel in reverse. Envs remain the source for *state display*
(panel rows: authorized/installed), never a launch trigger.

Mechanics (verified in code): the FE cannot initiate contact with the nested cross-origin webview —
the only reliable address is `ev.source` of a message the embed sent first (the shipped ack path,
`gui-harness.html:260` / production FE alike). Therefore:

- **Announce**: immediately after init, the container-side receiver webview posts an
  "embed-ready" announce to `window.top`, carrying the embed's state (what the container knows
  about itself). The FE captures `ev.source`/`ev.origin` and is the brain: it decides what to do
  based on its own context.
- **Command**: on overlay completion (agent pick + auth) the FE posts `launch-agent` to the
  retained source. The webview dumb-pipes it to the extension host, which is the sole origin
  authority (exact-origin allowlist incl. `ZCP_WELCOME_BRIDGE_ORIGINS`, version, `eventId` dedup,
  freshness window — the shipped §4 posture, reversed).
- **Reliability = retry-until-ready**: an embed reload kills the retained reference; the new
  webview re-announces, the FE re-sends until it receives agent-ready (ticket 02). `eventId`
  dedup on the container side prevents double launches. Message taxonomy is ticket 02's to pin.

Interim decisions recorded for the register (still true, now moot as triggers): the installed
probe must never gate a launch (0.1.5 PATH false-negative regression — probe can lie exactly when
it matters; all FE-authorizable agents are preinstalled in the image, per owner); a multi-auth
batch would take the first agent in `ZCP_AGENTS` order (deterministic backstop; the command names
the agent, so this is vestigial).

### 2. One-shot — owned by the FE wizard, no container record

No marker, no `globalState`. The FE sends the command according to its own wizard state; the
container is a validated command executor with dedup. (A container-side marker was rejected:
Zerops has no access to container state, and any env-carried "command" is state-not-event — it
re-triggers on fresh windows or loses the transition on reload, both directions wrong.)

### 3. Startup branching — env-derived defaults + FE directive + container-local editor state

- Zero agents authorized (first run, overlay above): open receiver webview (announce), Explorer
  open, otherwise dark waiting. The onboarding layout (maximized terminal) is established **only
  at launch-command execution time**.
- ≥1 agent authorized (return visit): panel auto-open, **gated by the container-local restored-
  editors rule** (today's `hasEditors`): a resume that restored a terminal / extension tabs does
  NOT force the panel; an empty workbench opens it. The FE may send a mode directive
  ("onboarding" vs "standard") that picks the presentation, but the editor-state logic stays
  container-owned — only the container sees restored tabs, only the FE sees wizard state.
- Reload after launch (terminal alive, agent authorized) falls into the return branch — panel
  beside the surviving terminal is correct.

### 4. Contexts — two-stage resolution kept (optimistic init default + runtime browser truth)

- **Framed under custom GUI** (non-app.zerops.io ancestor): full new flow as above.
- **Framed under app.zerops.io**: runtime suppress → legacy launcher, unchanged (out of scope).
- **Standalone** (direct code-server URL): **panel always**, even with zero authorized agents —
  dark waiting only exists *behind an overlay*; announce goes unanswered, harmlessly.

### 5. `startup.json` residue

Single init-derived bool, same `zeropsSubdomain` derivation, same fail-closed-to-legacy-launcher
behavior — **renamed to match the new meaning** (e.g. `agentFirst`; exact field name pinned by
ticket 07). `autoOpenWelcome` would lie once the welcome surface is gone. No compatibility
shims; the `BootstrapExtVersion` bump ships extension + config atomically.

### Scope amendment (recorded on the map)

FE work in `../frontend-legacy` is now **in scope**: the fullscreen overlay content (agent pick +
auth — built on the shipped ZCP-pool claim overlay machinery, `zcp-pool-claim-base.*`, whose
overlay today drops on iframe `load` and will drop on agent-ready instead), the FE bridge state
machine, and a dev entry to invoke onboarding mode from a logged-in project (today's only entry
is registration `?zcp=true` → cookie drain). Test rig: local GUI dev server over the `localflow`
zcp service (`make zcp-dev-deploy`), with the local GUI origin added to
`ZCP_WELCOME_BRIDGE_ORIGINS`. New tickets 08/09/10; ticket 02 rewritten to the bidirectional
contract.
