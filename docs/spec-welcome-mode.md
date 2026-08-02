# Spec: Agent-first mode (FE-driven onboarding + container agent panel)

The container's onboarding no longer happens inside vscode. The embedding Zerops frontend owns
the first-run experience — a fullscreen wizard (agent pick + authorization) rendered OVER the
prewarming code-server embed — and the container is a **validated command executor**: it
announces itself over the bridge, executes the FE's `launch-agent` command in a maximized
terminal carrying the fixed onboarding prompt, and signals `agent-ready` back to the top window.
The in-vscode surface reduces to one singleton webview — the **agent panel** (launcher rows +
skill packs + guided + Data Studio entry) — which doubles as the bridge relay. The walk-through
welcome content, the CTA journey, and the kickoff process wrapper are deleted (§11). The legacy
agent launcher survives ONLY as the `app.zerops.io` suppress-fallback (§1); folding it into the
panel is out of scope.

Roles, fixed: **the FE is the brain** (wizard state, when to command, retry policy), **the
container is the executor** (origin-validated, deduped, idempotent; it never launches on its own
observation). Environment flags remain the source for *state display* in the panel — never a
launch trigger.

---

## 1. Startup policy & presentation (W-ENTRY)

### 1.1 Instance policy — `startup.json`

`zcp init` derives instance-level presentation policy from the container's `zeropsSubdomain`
system env and writes a single boolean into the versioned extension's `startup.json`:
`{ "agentFirst": true|false }`. A parseable HTTP(S) URL whose host is not `app.zerops.io`
enables agent-first mode; an app-host, missing, or unusable value preserves the legacy
launcher/restored-editor policy. Activation reads the file fail-closed (missing/corrupt →
legacy behavior). This is init-time instance policy, not proof of the embedding parent; the
extension version bump ships code + policy atomically (§2). The former field name
`autoOpenWelcome` is deleted with the surface it described — no compatibility shim.

### 1.2 Runtime suppress — `app.zerops.io` fallback

The container is byte-identical whichever GUI embeds it, so init cannot distinguish the
production dashboard from a custom embed. The singleton surface decides at load from the
**parent origin**: its `<body>` starts hidden (`data-preload`, a nonce'd rule), and the inline
script reveals it UNLESS `window.location.ancestorOrigins` contains an `app.zerops.io` frame —
in which case it posts `{type:"welcome-suppress"}` and the host closes the surface and **falls
back to the legacy agent launcher** (never a blank editor). The production dashboard drives its
own onboarding; every other embedder and standalone proceed agent-first. This is the shipped
two-stage mechanism carried forward unchanged in shape.

The legacy launcher's own surfaces (startup tab, activity-bar Agents view) are **hidden while
agent-first mode is active** — a context-key-gated manifest contribution, not an unconditional
one — and revealed exactly when the legacy policy applies: `agentFirst: false`, or runtime
suppression under `app.zerops.io`. Two parallel launch surfaces must never render at once.

### 1.3 Receiver lifecycle — the singleton surface is the relay

The extension host cannot reach `window.top`; only a webview can. Announce, inbound commands,
and outcomes therefore all ride the singleton surface's webview relay — which means the surface
must exist whenever the channel might be needed.

**Embed classification.** A webview is ALWAYS framed (it lives inside the workbench page), so
`window.top !== window` cannot distinguish an embedded code-server from a standalone one. The
predicate is: **the code-server host page itself has a parent frame** — derived from the
webview's ancestor chain beyond the workbench's own origin(s). The exact expression is
live-proven during `/flow` across the three real shapes (standalone code-server, custom-GUI
embed, `app.zerops.io` embed) and pinned; the §1.2 suppress check already reads the same
ancestor chain.

Under agent-first mode, **embedded** (per the predicate above, not suppressed):

- The singleton surface boots on **every window init**, unfocused (restored editors keep
  focus). The host captures `hadRestoredEditors` BEFORE creating the receiver — the receiver
  tab itself never counts toward it. The surface announces `embed-ready` (§4.3) immediately
  and enters **`awaiting-mode`**: it renders dark and **must not self-close** until either a
  valid directive arrives or the 10 s no-directive window expires. A valid directive cancels
  the timer; env-derived state never overrides a directive and never ends `awaiting-mode`
  early — the env default exists only to pick the presentation if the window expires.
  - **`set-mode "onboarding"`**: the surface stays open and keeps rendering **dark** (nothing
    is painted; in practice it sits behind the FE's wizard overlay). Explorer is open; nothing
    else. The onboarding layout (§5.3) is established only at launch-command execution.
  - **`set-mode "standard"`** — or the 10 s window expiring (an embedder that never speaks
    the protocol; dark waiting is never terminal): container-owned rules apply. Empty
    workbench (`hadRestoredEditors` false) → the surface reveals the agent panel (§6).
    Restored editors → the surface **self-closes**: a resume that restored a terminal or
    editor tabs never keeps a Zerops tab. The transient background tab lives only until the
    directive arrives — sub-second under a live FE.
  - The surface **never self-closes while a launch intent is in flight** (a `launch-agent`
    was accepted and its outcome's relay forwarding, §4.3, is not yet confirmed).
- The restraint rule is container-owned and stays container-owned: only the container sees
  restored tabs, only the FE sees wizard state. The FE picks the mode; the container decides
  presentation within it.

**Standalone** (per the predicate: the host page is not framed): the panel always opens and
renders content — no `awaiting-mode`, no dark waiting (both exist only behind an overlay). The
announce is skipped or goes unanswered, harmlessly.

**Reload semantics**: an embed reload kills the FE's retained webview reference; the fresh
surface re-announces and the FE re-sends per §4.3. A reload after a completed launch (terminal
restored, ≥1 authorized) lands in the standard/`hasEditors` branch — no panel beside the
surviving terminal unless the workbench is empty.

### 1.4 Panel entry

The canonical manual entry point is the contributed command **`zerops.panel`** ("Zerops: Open
Panel"). A manual invocation is explicit user intent: it opens the panel focused, rendering
content, and **exempts the surface from the §1.3 self-close rule** for the rest of its
lifetime (a directive may still switch its rendering mode, never close it). The panel is a
singleton: re-invoking reveals the existing panel (never dispose/recreate — that wipes
in-progress UI state); `retainContextWhenHidden` keeps a hidden panel alive. Closing the panel disposes every panel-owned watcher; reopening must not
accumulate watchers. On reveal/focus the host re-reads state. No webview serializer is
registered — after a window reload the lifecycle in §1.3 governs.

Webview↔host startup is a **ready handshake**: the webview HTML carries no injected state; the
client posts `{type:"ready"}` (plus an `embedded` flag, `window.top !== window`, for
diagnostics) and the host replies with the full state. Later changes arrive as
`{type:"state", payload}` deltas.

The panel host code lives in a separate lazily-`require`d module; default activation loads
nothing beyond registering the command. The handler receives its collaborators by dependency
injection from the bootstrap module (agent registry, zembed reader, launch executor, the
`ZCP_AGENTS` availability resolver, the installed-binary probe) — each exists in exactly one
copy. A module load/open failure surfaces via `showErrorMessage` + output channel and must
leave the legacy launcher functional.

## 2. Extension install/upgrade contract (W-INSTALL)

Owner: `internal/init/adapters/claude.go`. Unchanged from the shipped contract:

- `bootstrapExtVersion` (Go const) and the template `vscode-bootstrap-package.json` `version`
  are **parity-pinned** (`TestBootstrapExtVersion_ParityWithManifest`). Any content change to
  the extension ships with a version bump — code-server reloads off the index version.
- Install materializes into a **versioned, immutable dir** `extensions/zcp-bootstrap-<version>/`:
  the complete file tree is written BEFORE the `extensions.json` index is switched, and the
  index write is **atomic** (temp file + rename). Old versioned dirs (and the legacy
  unversioned `zcp-bootstrap/`) are **never deleted** — a running extension host may still
  serve them.
- Same-version re-init is a **content no-op** (`TestInstallBootstrap_VersionedDirNoOp`); an
  upgrade leaves the previous dir byte-intact (`TestInstallBootstrap_UpgradeKeepsOldDir`) and
  prints a "reload the code-server window to activate" notice. No `require.cache`
  manipulation — window reload is the supported activation boundary.

## 3. State model (W-STATE)

Per agent, three independent axes — never collapsed into each other. All three drive **display
only**; none is a launch gate (§5.2).

**Availability** (`ZCP_AGENTS`, zcp-owned): a comma/whitespace-separated ordered list of agent
ids naming which agents this container *offers*, read live from the zembed store. It is
image/recipe **presentation policy** — not authorization, not a security boundary. No store, or
a store without the key → every registry agent. A present key parses as trim + lowercase + drop
unknown ids + dedupe (first occurrence, order preserved); a present-but-unusable value or a
value resolving to nothing yields **zero agents, fail-closed** — never a fallback to "all".
`ZCP_AGENT_TYPES` is consumed nowhere.

**Installed**: a real probe of the agent's registry-declared binary (`claude`, `codex`, `agy`,
`grok`, `cursor-agent`) — regular file + `X_OK`, no shell, no child process — against the
**union** of the extension host's own `process.env.PATH` and the live zembed store's `PATH`
(host-PATH-only was a live-verified 0.1.5 regression). Re-probed at every state recompute. The
probe is **advisory display truth only**: it renders the informative "Not installed in this
container" row and is never sent over the bridge and never gates any launch — the probe can lie
exactly when it matters.

**Authorization**: the platform flag (zembed env `ZCP_AGENT_OAUTH_<SUFFIX>` /
`ZCP_AGENT_TOKEN_<SUFFIX>`, written by the Zerops GUI or `zcp agent mark-oauth`) and the local
credential artifact (agent-owned file, e.g. `~/.claude/.credentials.json`, `~/.codex/auth.json`)
compose a **matrix**, never a boolean union:

| Platform flag | Local credential | UI state |
|---|---|---|
| absent | absent | Not authorized |
| absent | present | Locally logged in — platform sync pending |
| present | present | Authorized |
| present | absent | **Reconnect** (rebuild-orphaned flag) |
| token env present | n/a | Authorized (token) |

Credential probes exist only for agents whose artifact path is live-verified (claude-code,
codex). Agents without a verified probe render from the platform flag alone. The former
`anyRunnable` launch-gate aggregate is deleted with the CTA (§11).

Other state inputs: guided = presence of `.zcp/state/guided` in the selected workspace folder
(the ONE sanctioned `.zcp/state` read, presence-only — `spec-guided-mode.md` §2); skills =
the skill-pack status model (§7), never a per-slug embedded-content scan.

Watchers (zembed env file, credential dirs) are panel-scoped, debounced, tolerate missing
directories (created later → re-attach), survive atomic rename writes, and push deltas — they
never rebuild the panel HTML and they **never trigger a launch** (the env-watch auto-launch
trigger is deleted, §11).

## 4. Bridge (W-BRIDGE)

One channel, one contract home — this section. The FE implementation references it; no copy of
the contract exists in `frontend-legacy` (a second copy would rot).

### 4.1 Envelope + validation (shared by every message)

Channel **`@zerops/zcp-agent-auth-bridge`, `version: 1`** — the new message types extend the
shipped channel rather than fork it (verified before deciding: no deployed receiver existed to
protect). Uniform envelope on every message:

```
{ channel: "@zerops/zcp-agent-auth-bridge", version: 1, type: <string>,
  eventId: <UUIDv4>, createdAt: <ms, browser clock> }
```

- `createdAt` is stamped by the **sending browser context** on the browser clock immediately
  before posting: the webview for embed→FE messages, the FE page for FE→embed messages. The
  extension host never stamps it — its container clock can skew from the browser's, while
  every browser context on the page shares one clock, eliminating the container↔browser skew
  class; the receiver's freshness tolerance stays as defense in depth. Stored outcomes (§4.3)
  hold semantic fields only; every outbound emission — including an idempotent re-ack — gets
  a fresh stamp at send time.
- **Outbound** (embed→FE) is broadcast `targetOrigin "*"`: the webview cannot read its
  cross-origin parent's origin, and no payload carries anything worth protecting — no
  credential, token, serviceStackId, or free text ever rides the bridge.
- **Inbound** (FE→embed) passes one host-side pipeline, in order: `isAllowedGuiOrigin` →
  `version === 1` → **type allowlist** → freshness on `createdAt` → `eventId` dedup. The
  webview relay is a dumb pipe (channel filter + `BRIDGE_RELAY_MAX_BYTES` size cap, origin
  forwarded unexamined) — origin trust needs operator env the webview cannot read.
- `isAllowedGuiOrigin(origin, extraOrigins)` accepts: `https://app.zerops.io` (exact host,
  default port), a real dot-boundary subdomain of `*.zerops.dev` (never a substring test —
  `zerops.app.attacker.com` must fail), `http://localhost` on any port, and exact origins the
  operator opts into via **`ZCP_WELCOME_BRIDGE_ORIGINS`** (comma-separated). It deliberately
  does **not** trust `*.zerops.app` by pattern: that is the shared customer namespace — every
  Zerops service gets a public `*.zerops.app` URL and the code-server CSP `frame-ancestors`
  admits them, so a suffix trust would let a malicious customer page receive the trigger
  broadcast and forge acks/commands. A specific `*.zerops.app` GUI is trusted only by exact
  operator opt-in.

### 4.2 Auth trigger flow (embed→FE; shipped mechanics unchanged)

The panel's Authorize action posts a **credential-free trigger**, broadcast:

```
{ ...envelope, type: "open-agent-auth", agentType: "<agent id>" }
```

- zcp holds **no agent-support list of its own** — which agents the GUI's auth dialog handles
  is the FE's authority, answered per attempt by its ack. The host still gates every authorize
  click **fresh** on zcp's own axes (known registry id + available per `ZCP_AGENTS` +
  installed); an in-flight flow whose agent drops off those axes is released immediately.
- The sender posts phase **"contacting"** before the trigger ships, then waits for the ACK
  (`type:"open-agent-auth-ack"`, matching `eventId`, origin-validated per §4.1):
  `accepted:true` means the GUI has actually **dispatched its auth dialog** → phase
  "dialog-opening" **and the flow is released right there** — the trigger's job ends at
  delivery (the GUI cannot report a dialog dismissal; holding the flow past the ack turns
  re-clicks into a silent "busy" dead zone — live-verified). A re-click mints a fresh trigger
  (new `eventId`; the GUI dedups per event). `accepted:false, reason:"unsupported-agent"` →
  route to the Zerops panel; `accepted:false, reason:"not-ready"` → phase "gui-not-ready",
  released, pointing at reloading the Zerops page. **Timeout 12 s** (covers the GUI's own ≤10 s
  container-readiness check with margin; also the no-parent-listening case) → "Zerops
  dashboard not detected — reload the Zerops page". Phase taxonomy: `contacting` →
  (`dialog-opening` | `unsupported` | `gui-not-ready` | `no-dashboard`), each terminal.
- Completion is **observed, not messaged**: the GUI writes the platform flag → zembed
  (~5–10 s) → watcher → state delta. At most one auth flow in flight per panel, host-enforced.
- **`zcp agent mark-oauth <agent>`** (Go, `cmd/zcp` → `ops`) remains for the GUI/CLI to
  reconcile the platform flag independently: enum of known agent ids only, service identity
  derived from the container env, upserts exactly `ZCP_AGENT_OAUTH_<SUFFIX>=true` through the
  existing platform env operation, never arbitrary key/value/service, never prints credentials.

### 4.3 Embed command channel (the onboarding handshake)

Five types. The FE cannot initiate contact with the nested cross-origin webview — the only
reliable address is `ev.source` of a message the embed sent first. Hence: announce → retain →
command → outcome.

**`embed-ready`** (embed→FE, announce). Sent **once per webview init**, immediately —
any reload re-announces naturally; never repeated as state-sync (auth completion is known to
the FE from its own dialog). Payload: `agents: [{id, authorized}]` in `ZCP_AGENTS` order +
`bootstrapVersion` (diagnostics). **No `installed` axis** — sending it would invite FE gating
on a probe that can lie; all FE-authorizable agents are preinstalled in the image.

**`set-mode`** (FE→embed): `mode: "onboarding" | "standard"`. Idempotent; the FE re-sends it in
response to **every** `embed-ready`, with the value derived from its wizard state. Effect: §1.3.

**`launch-agent`** (FE→embed): `agentId` only — **text-free**. The fixed prompt is
container-owned (free text over the bridge would be an injection surface into a
`--dangerously-skip-permissions` agent). Semantics: "launch this agent with the onboarding
prompt in a maximized terminal" (§5). The **agentId gate**: known registry id ∧ present in
`ZCP_AGENTS` — a command can never start a binary outside the registry. **No authorized-flag
gate**: zembed env propagation lags ~5–10 s behind the GUI's flag write, so gating on the
observed flag would typically reject the freshly-authorized launch; the authority is the
origin-allowlisted FE that just performed the auth itself. Worst case the terminal shows the
agent's own login screen — visible, recoverable.

**`agent-ready`** (embed→FE): `agentId` + the **command's** `eventId` (correlation; outcomes
carry no identity of their own). It asserts exactly: **"the launch command was executed — a
terminal exists and the command line carrying the onboarding prompt was dispatched to it"**
(signal S2). Nothing stronger: sent immediately after dispatch, no wait for shell-integration
activation (race-prone, may never fire, distinguishes nothing), no grace period watching for
early exit. A false positive is cheap — the overlay drops and the user sees a terminal with a
visible, recoverable error.

**`launch-failed`** (embed→FE): `agentId` + `eventId` + `reason: "unknown-agent" |
"terminal-error"`. **Pre-dispatch only** — sent when the container knows for sure (agentId gate
rejection, terminal-creation/relay error). Post-dispatch, nothing is ever messaged: the overlay
has dropped and the terminal is the error surface (§5.4); a late `launch-failed` would force
the FE to back out of a terminal state.

**Retry & idempotence.** The container answers every valid `launch-agent` with **exactly one
outcome per `eventId`** (`agent-ready` | `launch-failed`) and **idempotently re-acks**
duplicates — a lost first answer must not hang the overlay. The dedup store (extension-host
memory) records `{agentId, status: "in-flight"}` **before the first side effect**: a duplicate
arriving mid-execution is coalesced and answered when the one execution completes — never a
second launch; a message reusing an `eventId` with a **different** `agentId` is rejected as
malformed. Bounds: completed outcomes retained ≥2 minutes with a cap (≥256, oldest-completed
evicted first); in-flight entries are never evicted. A code-server restart clears the store,
which is correct — the restart killed the terminals too, so a fresh retry SHOULD launch again.

The FE retries per re-announce until answered, re-sending the **same `eventId`** (intent
identity) with a freshly re-stamped `createdAt` (envelope freshness). A new intent — a dev
entry re-run — mints a new `eventId`; one dedup rule, no special cases. The FE owns the
overall timeout backstop for a dead embed (§8.1).

**Relay-forwarded confirmation** (host-internal, not a bridge type): the outcome is persisted
in the dedup store first, then handed to the relay webview, which posts it to `window.top` and
immediately posts a local `relay-forwarded` receipt (keyed by `eventId`) back to the host. The
host may tear the receiver down (§5.3) only after that receipt — a host→webview post alone
does not prove the message reached the top window. If the receiver dies before the receipt,
the persisted outcome is re-acked from the store on the next announce.

## 5. Onboarding launch execution (W-LAUNCH)

### 5.1 Terminal-only delivery

Onboarding launches **always run the agent's terminal open mode** — the executor selects the
registry entry with `mode: "terminal"` explicitly, never `opens[0]` preference (for claude the
plugin stays `opens[0]` as the panel's `Open extension` convenience; it is not the onboarding
vehicle). The fixed prompt `"Onboard me to Zerops."` (`ONBOARD_PROMPT`) is spliced into the
agent's launch argv by the shared seeding helper: POSIX single-quoted positional for
codex/cursor/grok/claude-terminal, `--prompt-interactive` for antigravity — auto-submitting as
the session's first turn. `seedOpenWithPrompt`/`shellQuoteArg`/`ONBOARD_PROMPT` are shared by
every terminal agent and survive; the Claude kickoff wrapper and its marker do not (§11).

Auto-submit is an **external CLI capability, not fully proven in-repo** (cursor/grok have no
test pin; claude's rests on upstream issue evidence): every registry agent's initial-prompt
argv is a PROVE-gate item for the implementing `/flow` — a CLI that fails the live proof gets
a deliberate fallback (corrected flag, terminal text delivery, or exclusion from
FE-authorizable onboarding) before BUILD, and the verified argv is pinned per agent.

### 5.2 One launch-gate rule

**No probe ever gates a launch** — not the installed probe (0.1.5: PATH false-negatives exactly
when it matters; all FE-authorizable agents are preinstalled), not the authorization flag
(zembed lag, §4.3). The only launch gates are identity gates: known registry id ∧ `ZCP_AGENTS`
membership, re-validated host-side per action for panel launches too (hiding a button is not
authority). This is one universal rule for the bridge launch and the panel's `Open terminal`
alike.

### 5.3 Onboarding layout

Established **only at launch-command execution time**: Explorer visible, the launched terminal
**maximized**, no editor tabs other than the receiver's own — editor cleanup is **tab-level**
(`vscode.window.tabGroups`), with the receiver's own tab always excluded from the close set;
never a blanket close-every-editor command, which would close the receiver too and silently drop
the `agent-ready` outcome it alone can relay to `window.top` (§1.3/§4.3). The receiver's own tab
is closed only later, by the §4.3 post-receipt rule (`relay-forwarded`); the surface must not be
torn down under its own `agent-ready`. Until execution, the embedded first-run window shows only the
dark receiver + Explorer (§1.3), in practice hidden behind the FE overlay, so the first thing a
user ever sees inside vscode is the agent already running their onboarding.

### 5.4 Post-dispatch failure — the terminal IS the whole answer

After dispatch, the container adds **nothing**: no panel auto-open on early exit, no
notification, no transient row state, no message to the FE. Rationale, bounded by verified
facts: the launch is `sendText` into a live shell — a failed agent leaves the shell alive, the
terminal never closes, and the only "command ended" signal is shell-integration-gated,
race-prone, and may never activate; the most likely post-dispatch surprise (the agent's own
login screen after zembed lag) never ends the command at all, so exit-based detection cannot
catch it. The same holds for the agents' first-run workspace-trust dialogs (claude, codex,
agy, cursor-agent show one in an untrusted cwd): the dialog precedes the seeded turn and the
turn survives it — live-verified 2026-07-28 — so it needs no special handling either. The
dispatched command line lands in shell history — up-arrow + enter is the native retry. Failure is rare by construction (preinstalled agents; the agentId gate covers the
pre-dispatch half explicitly).

Steady state after any such failure: the plain **`authorized`** row (§6) — panel rows derive
from envs only, and no "launch failed" row state exists (it would require exactly the detection
rejected above). `Open terminal` is the recovery affordance. **Accepted sharp edge** (owner,
explicit): after a reload with the dead-agent terminal restored, `hasEditors` suppresses the
panel — the user again sees only the terminal with the error. One rule for **every** launch
path: the onboarding launch and later panel-initiated launches get the same posture; no path
has a special follow-up.

## 6. Agent panel (W-PANEL)

The panel contract pins **layout and behavior, never visual design** — structure and states
below; pixels are produced at implementation time. Copy voice, everywhere: every line is
written from the point of view of a developer seeing the surface for the first time — internal
state vocabulary, our architecture as explanation, and positioning statements are out.

**Layout**: a single-column stack — header; **Data Studio in a compact box top-right** beside
the header (agents are the primary content; Data Studio secondary but immediately visible; at
narrow widths the box stacks between header and agents); agent rows; skill packs; guided.

**Agent rows** — every §3 matrix state and every §4.2 transport phase maps to exactly one row
state (copy is written at implementation in the first-time-developer voice; states, actions,
and collapsed-list membership are the contract):

| Row state (source) | Actions | Active? |
|---|---|---|
| Not authorized (matrix) | `Authorize` (§4.2 trigger) | no |
| Locally logged in — platform sync pending (matrix) | none — informational; the watcher delta resolves it | no |
| Authorized / Authorized-token (matrix) | `Open terminal` + `Open extension` (where the registry declares one) | yes |
| **Reconnect** (matrix: flag present, credential absent) | `Authorize` (re-run the §4.2 flow), copy explains the container lost its sign-in | yes |
| Authorizing (transport: `contacting`/`dialog-opening`) | none — "finish signing in via the Zerops dialog"; the row stays here until the watcher's authoritative state delta (there is no observable dialog-completion phase) | yes |
| Dashboard unreachable (transport: `no-dashboard`/`gui-not-ready`) | `Try again` (mints a fresh trigger) | yes |
| Not installed (probe) | none — informative ("Not installed in this container") | no |

"Reconnect" is **reserved for the matrix state** — transport failures never borrow its name.
`Open terminal` launches the terminal open mode with **no prompt**; `Open extension` opens the
plugin promptless — same launch seam and §5.2 gate discipline as the bridge path. An
explicitly empty available set renders an honest "No coding agents are enabled for this
container" state. **No onboard action exists anywhere** — onboarding is strictly once,
FE-owned; a user who wants the content again types the prompt into any agent CLI themselves.

**Collapsed list**: with ≥1 agent in an active state (authorized / authorizing / reconnect) the
panel renders only those rows plus a subtle **`+ Add another agent`** expander revealing the
rest (unauthorized + not-installed rows), toggling to `Hide available agents`. Zero active
agents → full list, no expander. Effect: a set-up panel is short and skills are visible without
scrolling.

**Skill packs section** (§7) opens with the ownership line, verbatim: *"Skill packs are just a
shortcut — this workspace is yours, and you or your agent can add skills to it directly at any
time."* Pack rows carry state copy for absent / installing / installed / subset / incomplete /
modified / broken / retired; Matt's pack gets the Customize picker (§7). **Guided** renders as
a row with its toggle here (Claude-Code-only lock), per the shipped guided contract (§7).

**Data Studio box**: one action that opens the single-tab Data Console (executes the Studio
extension's `zcpStudio.open` command; contract: `spec-dataconsole.md` §4.4). No per-service
list in the panel — service switching lives in the console's own rail. A missing/failed Studio
extension renders the box informative-disabled with a one-line diagnostic, never a dead
button.

**Accessibility**: on a state-delta re-render, keyboard focus is retained when the focused
node survives; when it disappears (e.g. `Authorize` becomes `Open terminal`), focus moves to
the replacement primary action, falling back to the row container — never dropped to body. The
row's new state is announced once, concisely, through a polite live region — state changes
arrive from watchers while the user may be mid-interaction.

## 7. Skills & guided (W-SKILLS, W-GUIDED)

**Skill packs.** The skills surface is the community skill-pack installer
(`internal/skillpacks/`; `zcp skills pack-add / pack-remove / pack-status / pack-set`) — the
embedded-curated-skills model is gone. Mechanics (catalog, review/selection granularity axes,
the revision-gated declarative `pack-set` apply, migration/detach semantics, resource caps,
locking) live in **`docs/spec-skill-packs.md`** (promoted alongside the port slice). What this
surface's contract requires of it:

- **Granularity axes**: repository-level packs install/remove atomically; `ReviewSkillLevel`
  packs enumerate an exact reviewed catalog; `SelectionSubset` (Matt) additionally lets the
  user pick a subset via the **Customize picker** — rendered from the CLI-reported `catalog`
  field (never a second hard-coded list), default selection, per-category select-all, a
  pending "N to add, M to remove" summary, and Apply posting the full desired set with the
  last-read revision.
- **Revision-gated apply**: `pack-set` is declarative (caller states the full desired set) and
  refuses on revision mismatch with zero writes — the picker's `conflict` response re-reads
  status and re-renders, never silently retries with a stale revision.
- **Preflight-then-atomic**: either the full reconciliation lands or the workspace is
  byte-identical — the UI never assumes partial progress.
- **Skill roots**: `zcp init` creates `.agents/skills/` + `.claude/skills/` unconditionally,
  before any agent session, so agents' native watchers see them at session start.
- A `broken` pack refuses picker interaction and points at `pack-remove`; a `retired`
  `ReviewSkillLevel` pack must still be removable via `pack-set`.

**Guided** (shipped contract, carried): a featured row with ON/OFF toggle + a static explainer
derived from `spec-guided-mode.md`; no configurable-looking axes. Toggle = spawn of the
canonical CLI (`zcp init --guided` / `zcp init`), fixed argv, **no shell**, cwd = the selected
workspace folder (multi-root → picker; no workspace → disabled). Disabled under authoring
(`ZCP_AUTHORING`). One toggle in flight per window. Dirty `AGENTS.md`/`CLAUDE.md` buffers block
the run; success = exit code 0 **and** a marker re-read — never output-prose parsing. A failed
run reports "preference recorded, surfaces partially refreshed — re-run `zcp init`", never a
silent success. The UI notes that a running agent session keeps its old instructions.

## 8. FE contract (W-FE) — what `../frontend-legacy` builds against

The wizard is a richer layer over the shipped ZCP-pool claim-flow skeleton: `?zcp=true` on
login/registration → `claimZcpPool` cookie (10 min, survives OAuth redirects) → on
`storeUserDataSuccess` + pending cookie the layer goes up instantly → behind it the drain
resolves the ZCP stack + `-zagent` userData, then prewarms — the app-root overlay feature opens
the embed fullscreen **behind the layer**, so the embed boots and announces in parallel with
the wizard's steps; announce never blocks any wizard step. Deleted with the wizard (no
backward compat): the iframe-`load` + 3 s dismissal fallback, the 45 s reveal backstop, and the
dead-letter `zcp-vscode-ready` listener — dismissal is the wizard's own state machine: the
happy ending is `agent-ready`, the degraded ending is the reveal window (§8.1) — both drop
the layer into the workspace.

### 8.1 Wizard state machine

`claiming` (drain resolving; embed not yet open) → `picking` (static roster shown; embed
prewarming) → `authorizing` (existing auth dialog over the layer) → `launch-ready`
(post-auth confirmation gate — the layer stays up) → `launching` (intent queued or sent;
30 s absolute cap) → `done` (layer drops — on `agent-ready`, or via the degraded reveal) |
`failed` (only when nothing was ever reachable).

- **Pick**: the roster is the FE's **static agent registry** (`SUPPORTED_AGENT_TYPES` +
  display names + design-system marks), rendered the instant `picking` is entered — never
  parsed from `ZCP_AGENTS`, never waited on, never mutated mid-wizard. The wizard runs
  exclusively in a fresh provisioner-owned pool project whose `ZCP_AGENTS` is absent
  (= full registry, §3), so the static roster is exact by construction; the container's
  identity gate (§5.2) stays the backstop — a `launch-agent` for an unoffered agent comes
  back `launch-failed` → the degraded reveal (the workspace with the agent panel is the
  recovery surface, never a dead-end dialog). INVARIANT: the backstop is a security gate, not a UX
  substitute — shipping a claimed-pool recipe that restricts `ZCP_AGENTS` requires
  revisiting this section first. Already-authorized ids come from the FE's own userData in
  the same emission the drain already waits for, and only steer the skip: picking an
  already-authorized agent jumps straight to `launch-ready`. **Single-select**; no
  multi-auth queue; no roster editing in the wizard (adding an agent = userData write +
  service restart — service-card territory). "Skip for now" exits from `picking` only and
  is the same converged exit as every other non-agent ending: wizard layer **and**
  code-server overlay close, landing on project detail — no standard-mode reveal, no
  queued directive.
- **Auth**: the wizard dispatches the existing auth dialog (`manualOpen` path, with
  `successNavigation: 'none'` so auth success does not navigate to the control-plane page
  under the layer) — chrome, CLI OAuth driver, handlers, and userData writes stay as
  shipped. Completion = the dialog's `markAuthorized` action matched on the wizard's stack
  **and** the picked agent (real OAuth success — it fires before the dialog's auto-close),
  NEVER `manualOpenResult`, whose `ok` only ever means "the dialog-open request resolved":
  `ok:false` (no container within 10 s) → `failed`, and the FE bounds its own
  bridge-context wait (15 s) onto the same failure. The dialog dismissed (X/ESC) before
  success → back to `picking` with the pick retained. A dialog already open for another
  flow at dispatch time bounces to `picking` the same way — the wizard never assumes it
  owns a dialog it did not open.
- **Launch-ready**: entered on auth success or the authorized-pick skip. The layer stays
  up, names the picked agent, and explains what happens next; the **primary CTA** mints
  the launch intent (fresh `eventId`), enters `launching`, and starts the **30 s absolute
  intent cap** — launch is always user-initiated, never implicit in auth completing. The
  secondary text exit is the converged non-agent exit above. `launch-ready` is a
  wizard-active state: a re-announce still receives `set-mode "onboarding"`.
- **Launching**: the intent is held as a queued send — posted immediately when a retained
  embed address exists, otherwise on the next `embed-ready` (covers a CTA pressed before a
  slow embed's first announce). On **every** announce while in `launching`, the FE sends
  `set-mode "onboarding"` first, then the launch/retry with the same `eventId` and a fresh
  `createdAt`. A post counts only when `postMessage` returned without throwing; every
  successful post starts/restarts the **5 s reveal window**. Endings:
  - `agent-ready` matching the held eventId (S2, §4.3 — the command visibly dispatched
    into a live terminal; typically 1-2 s) → `done`, the layer drops onto the maximized
    terminal running the onboarding prompt.
  - The reveal window expiring, an explicit `launch-failed` (any reason — the bridge is
    demonstrably alive; keep the reason in diagnostics), or the 30 s cap expiring with at
    least one successful post, all converge on the **degraded reveal**: send `set-mode
    "standard"` (handing presentation to the container's §1.3 rules — a live terminal
    stays in front; an empty workbench reveals the agent panel, the built-in recovery
    surface), clear the intent, then `done`. Silent — no toast, no hint: the workspace
    explains itself (agents routinely sit on interactive prompts the user just clicks).
    An `agent-ready` arriving after a degraded reveal is a no-op.
- **Failure**: only endings with **nothing to reveal** reach `failed`: the 30 s cap with
  zero successful posts (dead embed — no announce ever), and the pre-launch
  auth-unavailable paths (`ok:false`, bridge-context timeout). Short copy + one
  **Continue** button — the converged exit (wizard + overlay close, project detail) —
  never a broken fullscreen iframe. The overlay's reset invalidates the wizard's retained
  embed address (`onEmbedGone`), so "announce seen" always means the current iframe.

**One-shot semantics**: the wizard shows only on explicit entry (the cookie drain, or §8.2);
never derived from authorized-agents state; no container-side record exists. Abandonment
mid-wizard is deliberately unhandled — a full page reload in any state (including
`launch-ready`) is abandonment, no persistence — recovery is the standard path
(click-to-start → panel → Authorize), with **no prompted-launch state**. Onboarding is **strictly once** for users: no
discoverable user-facing entry re-invokes it — the dark §8.2 developer parameter is the sole
exception.

### 8.2 Dev entry — `?zcpOnboard=1` on project detail

One-shot query param `/project/:projectId?zcpOnboard=1`, shipping **dark** (no gate, no UI, no
docs): an FE effect catches it, immediately strips it from the URL (`replaceUrl` — a reload
must not re-trigger), and raises the wizard **at `claiming`**, then performs the drain's
tail: resolve the ZCP stack **in the route's project** (`isControlPlaneService` + `projectId`
filter, never first-match client-wide), subscribe the `-zagent` userData, prewarm, and enter
`picking` **once** with the authorized-agents snapshot (single raise — no early empty
roster, no mid-wizard mutation) — the overlay opens a fresh fullscreen embed behind the
layer, whose boot announces in `awaiting-mode` (§1.3). Pure bypass: the `claimZcpPool`
cookie is never read, written, or cleared. An already-authorized picked agent skips straight
to `launch-ready` — the launch still takes the explicit CTA (the §8.1 queued-send rule
covers a CTA pressed before the fresh embed's announce) — the repeated launch loop dev
testing needs; a second terminal in a running container is an accepted dev consequence. A
project without a ZCP stack is a **silent no-op** (param stripped, `console.warn` at most).
No `isDevMode` gate — the param must behave identically in local dev, the deployed rig, and
production; risk is nil (it only raises the wizard over the user's own project; auth is still
required).

A second, complementary dev aid lives FE-side: the `ZGUI_ENABLE_SIMULATE_ZCP_POOL_CLAIM`
env flag (set only in a developer's local gitignored `.env`; off in every deploy) replays
the full cookie-drain tail — wizard up, ZCP resolve, authorized snapshot, `picking` — on
every reload for the logged-in account, no `?zcp=true` signup. It exercises the REAL drain
path that this parameter deliberately bypasses; the FE keeps the drain tail as one shared
stream so the cookie path and the simulator cannot drift.

### 8.3 Architecture homes (FE conventions)

- **Wizard state = signals service in root** — the evolution of the claim-overlay service
  (transient UI state, not app state; it already owns cover visibility + prewarm). The cookie
  drain effect stays, raising the wizard instead of the dumb cover.
- **The bridge listener stays in the code-server overlay feature** — extending the shipped
  while-open, outside-the-Angular-zone listener (origin + iframe-identity walk + freshness +
  eventId dedup) with the embed→FE types (`embed-ready`, `agent-ready`, `launch-failed`),
  routed into the wizard service. Announces arrive on every embed boot, wizard or not; the FE
  answers every `embed-ready` outside an active wizard with `set-mode "standard"`.
- **Retained `ev.source` + origin live in the service, never in an ngrx action** (Window refs
  stay out of the store). `set-mode` is re-sent on every `embed-ready`; `launch-agent` retries
  with the same `eventId` per §4.3.
- Standard (non-onboarding) visits are untouched: control-plane page keeps "Click to start
  editing" → embed opens docked → the panel appears per container rules (§1.3).
- The FE pins the wizard state machine (incl. the queued launch intent, the 5 s reveal
  window vs the 30 s cap's posted/never-posted split, and the degraded-reveal convergence)
  and a registry-parity test (every supported agent has a display name, a design-system
  mark, and an auth handler) in `frontend-legacy`'s own test suite — the zcp invariant
  table below pins only container-side behavior.

## 9. Security floor (W-SEC)

- Webview CSP: `default-src 'none'` plus nonce'd scripts/styles only, the nonce from `crypto`.
  Assets inline; no fetch, no iframes, no remote assets.
- Webview→host messages pass a **strict allowlist** (exact type, enum fields, size caps);
  unknown or malformed messages are dropped. Dynamic text renders via `textContent` — no HTML
  interpolation of state.
- Inbound bridge commands pass the §4.1 pipeline; `launch-agent` is text-free by contract and
  its agentId is registry-gated — the bridge can never inject text into, or start a binary
  outside, the registry's terminal commands.
- No OAuth code, token, env value, or terminal content ever enters the DOM, `setState`, logs,
  diagnostics, or any bridge payload; error surfaces redact env values and paths.

## 10. Outside the Zerops container (W-PORTABLE)

Invoked in a non-Zerops code-server / desktop VS Code: the panel opens on command, never
crashes on the missing zembed store, marks the bridge "unavailable", disables
platform-dependent actions with a one-line diagnostic, and leaves intentionally-local actions
working. Availability with no store defaults to every registry agent; the installed probe keeps
reporting local PATH truth — a laptop shows real "Not installed" rows rather than pretending
the container's agent set exists. Such hosts classify as standalone under the §1.3 predicate
(the host page is not framed) — no `awaiting-mode`, no announce duty; a framed non-Zerops
embedder instead rides the 10 s no-directive fallback.

## 11. Deletion inventory

Deleted by this concept (never shipped to a customer — no migration, no compat shims):

- **The welcome walk-through surface**: the multi-step webview content (agent step, guided
  step, skills step as a wizard), hint/video content, and the `zerops.welcome` command
  identity — including the legacy Agents-view title-bar button (`view/title` menu entry,
  `when: view == zcpAgents`) that executed it: the button is deleted, not retargeted (under
  the suppress worlds where that view renders, a retargeted panel would immediately
  re-suppress). The singleton webview survives only as the agent panel (§6) + receiver
  (§1.3).
- **The CTA journey (W-CTA)**: both kickoff paths ("Build something new" / "Integrate my
  existing app") **including their kickoff prompts** — the fixed onboarding prompt is the
  single entry; any build-vs-integrate fork happens inside the agent conversation, not in UI.
  The `anyRunnable` aggregate and clipboard-first prompt delivery go with it.
- **The kickoff process wrapper**: `vscode-claude-kickoff-wrapper.py`, `~/.zcp/bin/
  claude-kickoff`, the `claudeCode.claudeProcessWrapper` settings write
  (`patchVSCodeClaudeWrapper`/`installKickoffWrapper` + call site), the HOME kickoff marker
  (`kickoffMarkerPath`/`armKickoffMarker` + the extension-mode arm branch), and their test
  assertions. **Kept**: `seedOpenWithPrompt`, `shellQuoteArg`, `ONBOARD_PROMPT` — every
  terminal agent's argv delivery depends on them (§5.1).
- **The env-watch auto-launch trigger**: no observation of env flags ever starts an agent;
  launches are explicit bridge commands (§4.3) or explicit panel clicks.
- **`autoOpenWelcome`** (renamed `agentFirst`, §1.1) and the old `handleOnboard`/`opens[0]`
  onboarding dispatch (§5.1 replaces it).
- **The custom-GUI startup `closeSidebar` action** — the old mode idempotently closed the
  primary sidebar at open; the agent-first onboarding layout wants Explorer **visible** (§5.3).
- The welcome walk-through's test suites (CTA flow, onboard/kickoff-marker, journey/UI
  structure assertions) — replaced by the panel, command-channel, and receiver-lifecycle
  suites pinning W10–W15.
- FE side: the iframe-`load` + 3 s dismissal, the 45 s reveal backstop, the dead
  `zcp-vscode-ready` listener (§8).

**Survives unchanged**: the legacy agent launcher (startup tab + activity-bar Agents view) as
the `app.zerops.io` suppress-fallback only (§1.2); the §2 install contract; the §4.2 auth
trigger flow; `zcp agent mark-oauth`.

## Invariants (pinned)

Rows marked *(new)* are pinned during the implementing `/flow`; existing test names are live on
`main` today.

| # | Invariant | Pinned by |
|---|---|---|
| W1 | Go version const == manifest version, always | `TestBootstrapExtVersion_ParityWithManifest` |
| W2 | Versioned immutable install; atomic index; same-version no-op; old dirs intact | `TestInstallBootstrap_VersionedDirNoOp`, `TestInstallBootstrap_UpgradeKeepsOldDir` |
| W3 | `startup.json` carries one init-derived bool `agentFirst` from `zeropsSubdomain`; activation fail-closed; runtime `app.zerops.io` ancestor → suppress → legacy launcher fallback, no paint, never a blank editor | `TestInstallBootstrap_WritesStartupPolicyFromZeropsSubdomain` (renamed field) + welcomejs suppress/fallback tests *(updated)* |
| W4 | Auth state is the §3 matrix (incl. Reconnect), never a boolean union | welcomejs state-matrix tests |
| W5 | Bridge envelope/validation per §4.1: credential-free, UUIDv4, `createdAt` stamped by the sending browser context (host never stamps), broadcast outbound, inbound origin-gated host-side by `isAllowedGuiOrigin` (never `*.zerops.app` by pattern); relay is a dumb pipe | welcomejs bridge tests *(extended)* |
| W6 | Guided toggle spawns fixed argv in the selected folder, no shell; success = exit code + marker re-read; partial failure reported honestly | welcomejs guided tests |
| W7 | Skills surface drives `internal/skillpacks` via the CLI JSON contract only; picker renders from the reported catalog; `pack-set` apply is revision-gated declarative | `pack_picker`/`pack_install` JS tests + `TestPackSet_*` *(ported)* |
| W8 | The extension never runs a login flow, never reads credential values, never calls the platform from JS — platform writes go through `zcp agent mark-oauth` (enum-only) | welcomejs message-allowlist tests + Go `TestAgentMarkOAuth_*` |
| W9 | Availability is `ZCP_AGENTS` (ordered, fail-closed once present); `ZCP_AGENT_TYPES` consumed nowhere; installed is a host∪store PATH probe, display-only | welcomejs availability/detection tests |
| W10 | No probe ever gates a launch — identity gates only (registry ∧ `ZCP_AGENTS`), re-validated host-side per action; the auth flag is never a launch gate | *(new)* welcomejs launch-gate tests |
| W11 | One outcome per `launch-agent` `eventId`: `in-flight` recorded before the first side effect (duplicates coalesced, never a second launch; mismatched `agentId` rejected), completed outcomes idempotently re-acked, bounds per §4.3 (in-flight never evicted); restart clears it; receiver teardown only after `relay-forwarded` | *(new)* welcomejs command-channel tests |
| W12 | `launch-agent` is text-free; the prompt is container-owned; `agent-ready` = S2, sent immediately post-dispatch; `launch-failed` pre-dispatch only | *(new)* welcomejs command-channel tests |
| W13 | Embed classification = "the host page is itself framed" (live-proven across standalone / custom-GUI / `app.zerops.io`); announce on every webview init; `awaiting-mode` never self-closes before a directive or the 10 s window; self-close only on standard+`hadRestoredEditors`; never mid-intent; manual `zerops.panel` exempt | *(new)* welcomejs receiver-lifecycle tests |
| W14 | Post-dispatch, the terminal is the sole error surface for every launch path — no panel auto-open, no notification, no row state | *(new)* welcomejs tests + §5.4 |
| W15 | Panel a11y: focus retained or moved to the replacement primary action on re-render (never dropped to body); one polite live-region announcement per state delta | *(new)* welcomejs a11y tests |
