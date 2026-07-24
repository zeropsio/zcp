# Spec: Welcome mode (container onboarding surface)

The welcome screen is the container-side onboarding surface of the code-server product: one
persistent webview panel that walks a fresh user from "empty container" to "authorized agent,
guided mode decided, skills installed, first build started". It ships inside the
`zcp-bootstrap` extension and is normally **dark** — deployed everywhere, visible nowhere until
the VS Code command **`zerops.welcome`** ("Zerops: Get Started") is invoked. During `zcp init`,
an additive **custom-GUI onboarding mode** auto-opens for a valid non-app container `zeropsSubdomain`
but suppresses itself at runtime when the production `app.zerops.io` dashboard is the embedding GUI
(parent-origin check in `welcome.html`, see §1): `onStartupFinished` opens this same singleton
welcome panel as the first bootstrap surface and idempotently closes the primary sidebar/Explorer.
The extension's launcher
(startup tab, activity-bar Agents view) is a **single auth-aware model** over the same three
per-agent axes welcome renders (§3: availability × installed × authorization) — the historical
dual-mode launcher (`ZCP_AGENT_TYPES` legacy filter + auto-open-Claude fallback) is deleted, and
`ZCP_AGENT_TYPES` is consumed nowhere (it survives only in the env-classification allowlist for
services that still carry it). "Dark" therefore means *welcome never auto-opens when custom-GUI
onboarding mode is absent*, not that the extension shows nothing.

Design lineage: PRD v6 (`mock/vscode-welcome-onboarding` branch) reconciled with the sendMessage
auth-bridge discovery (frontend-legacy `prototype/zcp-claude-auth-bridge`) and a Codex adversarial
review. The original PRD v6 proposed a new `ZCP_WELCOME` flag. The shipped form introduces no new
env: init derives instance-level presentation mode from Zerops' existing `zeropsSubdomain` system
env and writes only the resulting boolean to the versioned extension's `startup.json`. This is
init-time instance policy, not proof of the browser parent that opened the current iframe; the
selected mode therefore stays fixed until a newer extension install/re-init and applies even when
the service is opened directly or from another GUI. True per-embed origin selection remains
outside this contract. `ZCP_WELCOME_BRIDGE_ORIGINS` remains independent origin-authentication
configuration for the auth bridge and does not control startup presentation.

---

## 1. Entry + panel lifecycle (W-ENTRY)

- The canonical entry point is the contributed command `zerops.welcome`. Default/dark mode has no
  `onStartupFinished` path that constructs, reveals, or precomputes anything welcome-owned.
  Custom-GUI onboarding mode invokes that same entry point during `onStartupFinished`, regardless
  of restored editor tabs, then runs `workbench.action.closeSidebar`; it never uses a visibility
  toggle or persists a layout setting. No webview serializer is registered — after a window reload
  default mode waits for the user command, while custom-GUI mode opens a fresh singleton panel.
- Custom-GUI onboarding mode is presentation policy, not origin authentication. It has TWO gates:
  1. **Init (optimistic default):** `zcp init` writes `{ "autoOpenWelcome": true|false }` from the
     container `zeropsSubdomain` — a parseable HTTP(S) URL whose host is not `app.zerops.io` enables
     it; app-host/unusable values preserve the launcher. Activation reads that file fail-closed.
  2. **Runtime (embedding GUI):** the container is identical whichever GUI embeds its code-server,
     so init cannot tell the production dashboard from a custom embed. `welcome.html` therefore
     decides at load from the **parent origin**: the `<body>` starts hidden (`data-preload`, a
     nonce'd rule — never an inline style), and the inline script reveals it UNLESS
     `window.location.ancestorOrigins` contains an `app.zerops.io` frame, in which case it posts
     `{type:"welcome-suppress"}` and the host closes the panel (the body is never painted — no
     flash) **and falls back to the legacy agent launcher** (`onSuppressed` → `showInitial`-equiv
     `openLauncher`, dropping custom-GUI mode). Custom-GUI mode opened the welcome INSTEAD of the
     launcher, so a bare dispose would leave the editor blank; the fallback restores the pre-welcome
     onboarding. Every other embedder (a Tatami/custom GUI, `localhost`) and standalone code-server
     reveal the welcome instead. The production `app.zerops.io` dashboard drives its own onboarding.
  `ZGUI_DATA_APP_URL` and `ZCP_WELCOME_BRIDGE_ORIGINS` do not control startup presentation. Once
  enabled for an activation, watched zembed changes may refresh welcome-owned state but must never
  reopen the legacy launcher over the welcome panel.
- The welcome host code lives in a **separate module file** (`welcome.js`) inside the extension
  dir, `require`d lazily by the shared welcome opener. Default activation loads nothing
  welcome-related beyond registering the command; custom-GUI activation loads the module only when
  it actually invokes that opener. A welcome module load/open failure surfaces via
  `showErrorMessage` + an output channel and must leave the launcher functional.
- The command handler receives its collaborators by **dependency injection** from the bootstrap
  module (agent registry, zembed reader, `runAgentAction`, the `ZCP_AGENTS` availability resolver,
  the installed-binary probe) — the safety-pinned launch commands, the availability contract, and
  the probe each exist in exactly one copy (extension.js).
- The panel is a **singleton**: re-invoking the command reveals the existing panel (never
  dispose/recreate — that wipes in-progress UI state). `retainContextWhenHidden` keeps a hidden
  panel alive. Closing the panel disposes every welcome-owned watcher; reopening must not
  accumulate watchers. On reveal/focus the host re-reads state (missed watcher events must not
  leave stale UI).
- Webview↔host startup is a **ready handshake**: the webview HTML carries no injected state; the
  client posts `{type:"ready"}` and the host replies with the full state. Later changes arrive as
  `{type:"state", payload}` deltas.

## 2. Extension install/upgrade contract (W-INSTALL)

Owner: `internal/init/adapters/claude.go`.

- `bootstrapExtVersion` (Go const) and the template `vscode-bootstrap-package.json` `version` are
  **parity-pinned** (`TestBootstrapExtVersion_ParityWithManifest`). Any content change to the
  extension ships with a version bump — code-server reloads off the index version.
- Install materializes into a **versioned, immutable dir** `extensions/zcp-bootstrap-<version>/`:
  the complete file tree is written BEFORE the `extensions.json` index is switched, and the index
  write is **atomic** (temp file + rename). Old versioned dirs (and the legacy unversioned
  `zcp-bootstrap/`) are **never deleted** — a running extension host may still serve them.
- Same-version re-init is a **content no-op** (`TestInstallBootstrap_VersionedDirNoOp`); an
  upgrade leaves the previous dir byte-intact (`TestInstallBootstrap_UpgradeKeepsOldDir`) and
  prints a "reload the code-server window to activate" notice. No `require.cache` manipulation —
  window reload is the supported activation boundary.

## 3. State model (W-STATE)

Per agent, three independent axes — never collapsed into each other:

**Availability** (`ZCP_AGENTS`, zcp-owned): a comma/whitespace-separated ordered list of agent
ids naming which agents this container *offers*, read live from the zembed store. It is
image/recipe **presentation policy** — not authorization, not a security boundary. No store, or a
store without the key → every registry agent. A present key parses as trim + lowercase + drop
unknown ids + dedupe (first occurrence, order preserved); a present-but-unusable value (non-string)
or a value resolving to nothing yields **zero agents, fail-closed** — never a fallback to "all".
The state payload's `agents[]` contains only available ids, in configured order; `ZCP_AGENT_TYPES`
is ignored everywhere.

**Installed**: a real probe of the agent's registry-declared binary (`claude`, `codex`, `agy`,
`grok`, `cursor-agent`) — regular file + `X_OK`, no shell, no child process — against the
**union** of the extension host's own `process.env.PATH` and the live zembed store's `PATH`.
Host-PATH-only was a live-verified regression (0.1.5): code-server's extension host freezes a
PATH narrower than the runtime profile PATH terminals get, while the store's PATH mirrors the
image's real search path — a hit on either counts. Re-probed at every state recompute / launcher
render.

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

Credential probes exist only for agents whose artifact path is live-verified (v1: claude-code,
codex). Agents without a verified probe render from the platform flag alone.

Two aggregates, deliberately distinct: `anyAuthorized` (auth matrix only) and **`anyRunnable`**
(= some agent `installed` **and** Authorized/Authorized-token) — the launch gate. An authorized
platform flag for a binary that isn't on the container's PATH must never unlock a launch surface.
A not-installed agent renders informatively ("Not installed in this container") with no actions;
an explicitly empty available set renders an honest "No coding agents are enabled for this
container" state.

Other state inputs: guided = presence of `.zcp/state/guided` in the selected workspace folder
(the documented contract of `spec-guided-mode.md` §2 — the ONE sanctioned `.zcp/state` read,
presence-only); skills = per-slug dir scan of `.claude/skills/` with shipped-content hash →
absent / installed-current / installed-modified.

Watchers (zembed env file, credential dirs) are welcome-panel-scoped, debounced, tolerate missing
directories (created later → re-attach), survive atomic rename writes, and push deltas — they
never rebuild the panel HTML.

## 4. Agent authorization (W-AUTH)

Two sanctioned paths; the extension NEVER runs a login flow itself, never parses TUI output,
never touches credential values.

**Bridge (primary — every available, installed agent).** zcp holds **no agent-support list of
its own**: which agents the GUI's auth dialog can handle is the embedding frontend's authority,
answered per attempt by its ack (`accepted:false, reason:"unsupported-agent"` routes the tile to
its fallback hint) — a zcp-side copy of that list would only go stale. The host still gates every
authorize click **fresh** on zcp's own axes (known registry id + available per `ZCP_AGENTS` +
installed); an agent failing those is answered with phase `unsupported`, and an in-flight flow
whose agent drops off those axes (live `ZCP_AGENTS` edit, binary removed) is released immediately
— never held for the 10-minute cap. The webview posts a **credential-free trigger** to the
embedding Zerops GUI, broadcast:

```
window.top.postMessage({ channel: "@zerops/zcp-agent-auth-bridge", version: 1,
  type: "open-agent-auth", agentType: "<agent id>",
  eventId: <crypto.randomUUID()>, createdAt: Date.now() }, "*")
```

- `createdAt` is stamped by the **webview**, on the **browser clock**, immediately before the
  broadcast — not by the host, which runs in a separate container process whose clock can skew
  from the browser's. Since the webview and the embedding GUI's page share the same browser
  clock, this eliminates the container↔browser skew class outright; the GUI's own ±5s tolerance
  on `createdAt` stays as defense in depth for whatever residual (browser clock drift) skew
  remains.
- The trigger is **broadcast** (`targetOrigin "*"`), not sent to a pinned origin: the webview
  cannot read its cross-origin parent's real origin, and the payload carries no
  serviceStackId/clientId/token — the receiver resolves identity from its own app state. Broadcast
  is safe because the message itself holds nothing worth protecting; the actual security gate is
  the frontend receiver, which only reacts to a trigger from its own embedded code-server iframe.
- Every **inbound** message is validated by the same **host-side, authoritative**
  `isAllowedGuiOrigin(origin, extraOrigins)` (welcome.js). The webview's raw-message relay
  (welcome.html) is a dumb pipe: it filters by **channel only** and forwards the browser-supplied
  origin unexamined — it cannot decide origin trust itself, since that decision needs the operator
  env below, which the webview has no access to. `isAllowedGuiOrigin` parses the origin and
  accepts `https://app.zerops.io` (exact host, default port), a real dot-boundary subdomain of
  `*.zerops.dev` (default port — never a substring test, which is bypassable, e.g.
  `zerops.app.attacker.com`, or a bare-dot host), `http://localhost` on any port for local dev, and
  any origin the container operator opts into via **`ZCP_WELCOME_BRIDGE_ORIGINS`**
  (comma-separated exact origins). It deliberately does **not** trust `*.zerops.app` by pattern:
  that's the shared customer namespace — every Zerops service gets a public `*.zerops.app` URL,
  and the code-server's CSP `frame-ancestors` lets any `*.zerops.app` page embed a victim's
  code-server, so trusting the suffix would let a malicious page there receive the broadcast
  trigger and forge an `accepted:true` ack. A specific `*.zerops.app` test/custom GUI is trusted
  only by exact operator opt-in, never by suffix.
- The sender posts phase **"contacting"** the moment the flow is installed — before the trigger
  even ships — separating "the trigger left this host" from "the GUI never accepted", then waits
  for the receiver's **ACK** (`type:"open-agent-auth-ack"`, matching `eventId`, origin validated
  by `isAllowedGuiOrigin`): `accepted:true` now means the GUI has actually **dispatched its auth
  dialog** (not mere structural acceptance of the trigger) → phase "dialog-opening" ("authorization
  dialog opening in the Zerops panel") **and the flow is RELEASED right there** — the trigger's
  job ends at delivery. The GUI has no way to report a dialog dismissal, so a flow held past the
  accepted ack turns every re-click after a dismissed dialog into a silent "busy" dead zone
  (live-verified); a re-click just mints a fresh trigger (new `eventId`, GUI dedups per event).
  `accepted:false, reason:"unsupported-agent"` → route to the Zerops panel; `accepted:false,
  reason:"not-ready"` → the GUI validated the trigger but could not open its dialog (bounded by
  its own container-readiness check) → phase "gui-not-ready", released, pointing at reloading the
  Zerops page. **Timeout** (12s — the GUI's own accepted ack is bounded by its ≤10s container-
  readiness check, so 12s covers it with margin; no Angular parent listening at all — e.g. code-
  server opened directly — pays this same, rarer, slower path) → the UI states "Zerops dashboard
  not detected — reload the Zerops page". The bridge flow thus spans click→ack/timeout only. Phase
  taxonomy: `contacting` → (`dialog-opening` | `unsupported` | `gui-not-ready` | `no-dashboard`),
  each terminal for that flow.
- Completion is observed, not messaged: the GUI writes the platform flag → zembed (~5–10 s) →
  watcher → state delta.
- Diagnostics: the webview reports `embedded` (`window.top !== window`) alongside its
  `{type:"ready"}` handshake, rendered in the diagnostics footer as "Embed: Zerops GUI" / "Embed:
  standalone" / "—" (unknown, no ready report yet) — self-explaining when a click lands in a tab
  no GUI can hear.

**Terminal-login fallback: removed.** The per-row "Terminal login" control and its host handler
(`authorize-terminal` message, `handleAuthorizeTerminal`, `LOGIN_COMMANDS`, and the credential-
watch completion that ran `mark-oauth`) are gone: the panel offers **bridge authorization only**
(the "Authorize in Zerops" action), which is the path all agents share. The bridge's own failure
phases no longer point at a terminal button (`no-dashboard`/`gui-not-ready` now say "reload the
Zerops page"). The `authFlow` slot therefore only ever holds a bridge flow.

**`zcp agent mark-oauth <agent>`** (Go, `cmd/zcp` → `ops`) remains for the Zerops GUI / CLI to
reconcile the platform flag independently. It accepts only an enum of known agent
ids, derives service identity from the container env, upserts exactly
`ZCP_AGENT_OAUTH_<SUFFIX>=true` through the existing platform env operation, never accepts
arbitrary key/value/service arguments, never prints credentials.

At most **one authorization flow in flight** per panel (bridge or terminal), enforced host-side.

## 5. Guided step (W-GUIDED)

- UI: a featured "Zerops Guided" row with an ON/OFF toggle + a **static explainer** of what
  guided does (derived from `spec-guided-mode.md`). No configurable-looking axes: the PRD-era
  axes drawer is explicitly rejected until an override contract exists in guided content.
- Toggle = spawn of the canonical CLI (`zcp init --guided` / `zcp init`), fixed argv, **no
  shell**, cwd = the user-selected workspace folder (multi-root → picker; no workspace → toggle
  disabled). Disabled under authoring (`ZCP_AUTHORING`). One toggle in flight per window.
- Before spawning, dirty `AGENTS.md`/`CLAUDE.md` buffers block the run (ask the user to save).
  Output streams to the output channel; success = exit code 0 **and** a marker re-read — never
  parsing of output prose. `zcp init` is non-transactional (the marker is written before the init
  steps run): a failed run reports "preference recorded, surfaces partially refreshed — re-run
  `zcp init`", never a silent success or a claimed rollback.
- The UI notes that an already-running agent session keeps its old instructions — start a new
  session after toggling.

## 6. Skills step (W-SKILLS)

- v1 ships **embedded curated skills** ("Claude skills" label): reviewed `SKILL.md` content with
  provenance front-matter, embedded in the zcp binary, materialized into the extension dir at
  install. Community whole-repo packs are out until a trust/update story exists.
- Install ("Add") copies `<slug>/SKILL.md` → `<workspace>/.claude/skills/<slug>/`, host-side:
  slug must be in the shipped allowlist (never a path from the webview), `guided` is a **reserved
  slug** (owned by `zcp init --guided`), `.claude/skills` is created when missing, destination
  containment is validated and symlinked path components are rejected, creation is atomic, and an
  existing modified file is replaced only after a **modal host confirmation**. Untrusted or
  no-workspace contexts refuse writes.
- Per-slug state (absent / installed-current / installed-modified) derives from a shipped-content
  hash and renders in the tile.

## 7. CTA (W-CTA)

Launch surfaces (kickoff, guided/skills mutating controls) unlock on **`anyRunnable`** (§3) —
never on authorization alone. Two paths ("Build something new" / "Integrate my existing app"),
each with its full kickoff prompt visible; with multiple runnable agents the user picks one
explicitly (no "first in registry"). Launch reuses the injected `runAgentAction`; the kickoff
prompt is **clipboard-first** (copied + one-line instruction) — never a blind delayed `sendText`
into a terminal that may not be running the agent.

The per-row **Onboard me** action (`{type:"onboard"}`) delivers the fixed prompt `"Onboard me to
Zerops."` already **SUBMITTED**, per launch mode:

- **Claude plugin (extension):** its `editor.open` `initialPrompt` only PREFILLS the composer — it
  does not submit. The submit is delivered out-of-band by the **process wrapper** (§7.1): the host
  arms a HOME-based kickoff marker (`~/.zcp/state/claude-kickoff.json`) and opens a FRESH plugin
  panel; the wrapper injects the prompt as a real user turn once that session is live.
- **Terminal agents:** the prompt is appended as the CLI's live-verified initial-prompt argv
  (Codex/Cursor positional; Antigravity `--prompt-interactive`), which auto-submits on start.

The host freshly re-validates runnable state before every launch, arms the marker only for the
plugin path, and shell-quotes the terminal prompt without mutating the shared registry command.
Two independent instant signals fire on click so it never reads as dead while the CLI boots
(~2s): the clicked row's progress line (`onboarding` phase) and a corner toast.

A per-row **Open** action (`{type:"open-agent"}`) launches an agent the host re-validates as
runnable, with no prompt and no clipboard — same launch seam, same fresh-revalidation discipline
(hiding a button is not authority).

### 7.1 Kickoff process wrapper

`zcp init` installs the wrapper (`~/.zcp/bin/claude-kickoff`, template
`vscode-claude-kickoff-wrapper.py`) and points `claudeCode.claudeProcessWrapper` at it, so the
Claude plugin launches the CLI THROUGH it. Pointing the setting at the wrapper (not the bare
binary) means every re-init keeps kickoff working. The wrapper is **transparent** for every spawn
with no marker armed (`auth status`, a plain terminal `claude`, any non-kickoff session) — it
execs the real binary immediately. Only a session spawn (`--input-format stream-json`) with an
armed marker is proxied: it injects the turn on stdin at the `initialize` control-response, and
**mirrors** the same turn (same uuid) onto stdout at `system/init` so the bubble renders at the
earliest in-panel moment — the CLI's own later replay carries the same uuid and is deduped by the
webview (no double bubble; reload rebuilds the identical turn). The marker is consumed once.

## 8. Security floor (W-SEC)

- Webview CSP: `default-src 'none'` (which subsumes connect/frame/img — no fetch, no iframe, no
  remote asset can load; video is an external link in v1) plus nonce'd scripts/styles only, the
  nonce from `crypto`, never time/random arithmetic. Assets are inline.
- Webview→host messages pass a **strict allowlist** (exact type, enum fields, size caps); unknown
  or malformed messages are dropped. Dynamic text renders via `textContent` — no HTML
  interpolation of state.
- No OAuth code, token, env value, or terminal content ever enters the DOM, `setState`, logs, or
  diagnostics; error surfaces redact env values and paths beyond what the user needs.
- The bridge payload contains no identity and no secrets (§4); ACKs are validated by origin +
  eventId before being trusted.

## 9. Outside the Zerops container (W-PORTABLE)

Invoked in a non-Zerops code-server / desktop VS Code: the panel opens, never crashes on the
missing zembed store, marks the bridge "unavailable", disables platform-dependent actions
(mark-oauth, flags-based tiles) with a one-line diagnostic, and leaves intentionally-local
actions (docs links) working. Availability with no store defaults to every registry agent;
the installed probe keeps reporting local PATH truth — so a laptop shows real "Not installed"
rows rather than pretending the container's agent set exists.

## Invariants (pinned)

| # | Invariant | Pinned by |
|---|---|---|
| W1 | Go version const == manifest version, always | `TestBootstrapExtVersion_ParityWithManifest` |
| W2 | Versioned immutable install; atomic index; same-version no-op; old dirs intact | `TestInstallBootstrap_VersionedDirNoOp`, `TestInstallBootstrap_UpgradeKeepsOldDir` |
| W3 | Default mode stays dark/lazy; init optimistically auto-opens for a usable non-app `zeropsSubdomain` (written into `startup.json`), then `welcome.html` suppresses itself at RUNTIME (parent-origin, `data-preload` body + `welcome-suppress` → host closes the panel, no paint, AND falls back to the legacy launcher via `onSuppressed` — never a blank editor) when `app.zerops.io` is an ancestor frame — every other embedder and standalone reveal it; invalid/missing/app `zeropsSubdomain` preserves the launcher/restored-editor policy | `TestBootstrapAutoOpenWelcome_DerivesFromInitZeropsSubdomain`, `TestInstallBootstrap_WritesStartupPolicyFromZeropsSubdomain`, `welcomejs` welcome-suppress + parent-origin gate + custom-GUI legacy-launcher-fallback tests + `TestBootstrapExtension_WelcomeLazyPins` |
| W4 | Auth state is the §3 matrix (incl. Reconnect), never a boolean union | `welcomejs` state-matrix tests |
| W5 | Bridge payload is credential-free, UUIDv4 + TTL, broadcast outbound (target "*"), inbound ACK origin-gated host-side by `isAllowedGuiOrigin` (app.zerops.io + real `*.zerops.dev` subdomains + `localhost` + operator-configured `ZCP_WELCOME_BRIDGE_ORIGINS`; never `*.zerops.app` by pattern — shared customer namespace); offered for every available+installed agent with GUI acks as the capability authority (no zcp-side support list); timeout offers (never auto-runs) the fallback; one flow in flight, released when its agent leaves the availability/installed axes | `welcomejs` bridge tests |
| W6 | Guided toggle spawns fixed argv in the selected folder, no shell; success = exit code + marker re-read; partial failure reported honestly | `welcomejs` guided tests |
| W7 | Skills installs are allowlisted slugs, containment-checked, atomic, no silent overwrite; `guided` reserved | `welcomejs` skills tests + `TestWelcomeSkillsMaterialized` |
| W8 | The extension never runs a login flow, never reads credential values, never calls the platform from JS — platform writes go through `zcp agent mark-oauth` (enum-only) | `welcomejs` message-allowlist tests + Go `TestAgentMarkOAuth_*` |
| W9 | Availability is `ZCP_AGENTS` (zcp-owned, ordered, fail-closed once the key is present); `ZCP_AGENT_TYPES` is consumed nowhere; installed is a real host∪store PATH probe (no shell, no child process) | `welcomejs` availability/detection tests + Go template pins |
| W10 | Launch surfaces (kickoff, Open, CTA) gate on runnable = installed ∧ Authorized/Authorized-token, re-validated host-side per action — never authorization alone, never webview-claimed state | `welcomejs` state-matrix / cta / open-agent tests |
