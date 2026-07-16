# Spec: Welcome mode (container onboarding surface)

The welcome screen is the container-side onboarding surface of the code-server product: one
persistent webview panel that walks a fresh user from "empty container" to "authorized agent,
guided mode decided, skills installed, first build started". It ships **dark** inside the
`zcp-bootstrap` extension — deployed everywhere, visible nowhere — and is revealed exclusively by
the VS Code command **`zerops.welcome`** ("Zerops: Get Started"). The historical launcher behavior
of `zcp-bootstrap` (startup tab, activity-bar Agents view, zembed auth-mode) is untouched by
welcome; "dark" means *welcome never auto-opens*, not that the extension shows nothing.

Design lineage: PRD v6 (`mock/vscode-welcome-onboarding` branch) reconciled with the sendMessage
auth-bridge discovery (frontend-legacy `prototype/zcp-claude-auth-bridge`) and a Codex adversarial
review. The auto-open "primary onboarding" mode from PRD v6 (`ZCP_WELCOME` gate) is deliberately
NOT implemented; if it ever lands, it is a separate additive switch and this spec's dark contract
still holds for every deployment without it.

---

## 1. Entry + panel lifecycle (W-ENTRY)

- The only entry point is the contributed command `zerops.welcome`. No `onStartupFinished` code
  path may construct, reveal, or precompute anything welcome-owned. No webview serializer is
  registered — after a window reload the panel is gone until the user re-invokes the command.
- The welcome host code lives in a **separate module file** (`welcome.js`) inside the extension
  dir, `require`d lazily inside the command handler only. `extension.js`'s top level and
  `activate()` gain nothing welcome-related beyond the `registerCommand` call itself. A welcome
  module load/open failure surfaces via `showErrorMessage` + an output channel and must leave the
  launcher fully functional.
- The command handler receives its collaborators by **dependency injection** from the bootstrap
  module (agent registry, zembed reader, `runAgentAction`) — the safety-pinned launch commands
  exist in exactly one copy.
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

Per agent, two independent inputs — the platform flag (zembed env `ZCP_AGENT_OAUTH_<SUFFIX>` /
`ZCP_AGENT_TOKEN_<SUFFIX>`, written by the Zerops GUI or `zcp agent mark-oauth`) and the local
credential artifact (agent-owned file, e.g. `~/.claude/.credentials.json`, `~/.codex/auth.json`) —
compose a **matrix**, never a boolean union:

| Platform flag | Local credential | UI state |
|---|---|---|
| absent | absent | Not authorized |
| absent | present | Locally logged in — platform sync pending |
| present | present | Authorized |
| present | absent | **Reconnect** (rebuild-orphaned flag) |
| token env present | n/a | Authorized (token) |

Credential probes exist only for agents whose artifact path is live-verified (v1: claude-code,
codex). Agents without a verified probe render from the platform flag alone. Cursor is present in
the launcher registry but has no verified probe or bridge support; its tile (like antigravity,
grok) routes to the Zerops panel.

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

**Bridge (primary, v1: claude-code only).** The webview posts a **credential-free trigger** to
the embedding Zerops GUI:

```
window.top.postMessage({ channel: "@zerops/zcp-agent-auth-bridge", version: 1,
  type: "open-agent-auth", agentType: "claude-code",
  eventId: <crypto.randomUUID()>, createdAt: Date.now() }, <pinned GUI origin>)
```

- Target origins come from a **build-time allowlist** in the extension — never from the message,
  the workspace, or the env store. The message carries no serviceStackId/clientId/token — the
  receiver resolves identity from its own app state.
- The sender waits for the receiver's **ACK** (`type:"open-agent-auth-ack"`, matching `eventId`,
  validated origin): `accepted:true` → "authorization dialog opening in the Zerops panel";
  `accepted:false, reason:"unsupported-agent"` → route to Tier-A/panel; **timeout** (no Angular
  parent listening — e.g. code-server opened directly) → the UI states "Zerops dashboard not
  detected" and OFFERS the terminal fallback. It never auto-launches the fallback (a lost ACK
  must not create two concurrent login flows).
- Completion is observed, not messaged: the GUI writes the platform flag → zembed (~5–10 s) →
  watcher → state delta.

**Tier-A terminal fallback (v1: claude-code, codex).** `createTerminal()` +
`sendText(<loginCommand>)` with the login command taken verbatim from the frontend registry
(`claude /login`, `codex login --device-auth`); completion detected by the credential-file watch.
On success the host runs **`zcp agent mark-oauth <agent>`** so the platform flag, the sidebar
launcher (which reads env only), and the Zerops GUI agree with local reality. `mark-oauth`
failures degrade to the "Locally logged in — platform sync pending" state, never block.

**`zcp agent mark-oauth <agent>`** (Go, `cmd/zcp` → `ops`): accepts only an enum of known agent
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

Unlocks when ≥1 agent is Authorized. Two paths ("Build something new" / "Integrate my existing
app"); with multiple authorized agents the user picks one explicitly (no "first in registry").
Launch reuses the injected `runAgentAction`; the kickoff prompt is **clipboard-first** (copied +
one-line instruction) — never a blind delayed `sendText` into a terminal that may not be running
the agent. Per-agent seeding may upgrade this only with live-proven initial-prompt support.

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
actions (docs links) working.

## Invariants (pinned)

| # | Invariant | Pinned by |
|---|---|---|
| W1 | Go version const == manifest version, always | `TestBootstrapExtVersion_ParityWithManifest` |
| W2 | Versioned immutable install; atomic index; same-version no-op; old dirs intact | `TestInstallBootstrap_VersionedDirNoOp`, `TestInstallBootstrap_UpgradeKeepsOldDir` |
| W3 | Dark: no welcome module load, watcher, or panel before the command; load failure leaves the launcher healthy | `welcomejs` dark/lazy tests + Go template pins (`TestBootstrapExtension_WelcomeLazyPins`) |
| W4 | Auth state is the §3 matrix (incl. Reconnect), never a boolean union | `welcomejs` state-matrix tests |
| W5 | Bridge payload is credential-free, UUIDv4 + TTL, pinned-origin; ACK-gated; timeout offers (never auto-runs) the fallback; one flow in flight | `welcomejs` bridge tests |
| W6 | Guided toggle spawns fixed argv in the selected folder, no shell; success = exit code + marker re-read; partial failure reported honestly | `welcomejs` guided tests |
| W7 | Skills installs are allowlisted slugs, containment-checked, atomic, no silent overwrite; `guided` reserved | `welcomejs` skills tests + `TestWelcomeSkillsMaterialized` |
| W8 | The extension never runs a login flow, never reads credential values, never calls the platform from JS — platform writes go through `zcp agent mark-oauth` (enum-only) | `welcomejs` message-allowlist tests + Go `TestAgentMarkOAuth_*` |
