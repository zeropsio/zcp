const vscode = require("vscode");
const fs = require("fs");
const os = require("os");
const path = require("path");
const crypto = require("crypto");
const { spawn: defaultSpawn } = require("child_process");

// Zerops "Get Started" welcome panel — the webview host module for the
// zerops.welcome command. extension.js requires this file LAZILY, only
// inside the command handler (see its activate()), so this file's own top
// level must stay side-effect-free beyond plain declarations: simply
// requiring it is what the dark-load tests assert never happens before the
// command runs (docs/spec-welcome-mode.md §1, W-ENTRY / W3).
//
// Collaborators (agent registry, zembed reader, runAgentAction) arrive by
// dependency injection from extension.js's command handler — the
// safety-pinned launch commands stay in exactly one copy. extension.js's
// call site never changes to pass the test-only overrides below (homeDir,
// workspaceRoot, fs) — resolveDeps() fills them in here instead, so a test
// that needs them calls open() directly (see welcomejs/harness.js
// loadWelcome()).

// Placeholder substituted in welcome.html for the per-open crypto nonce
// (CSP script-src/style-src, §8 W-SEC). Every occurrence is replaced with
// the same nonce value — never a hardcoded/derived one.
const NONCE_PLACEHOLDER = "__CSP_NONCE__";

// Exact-match allowlist for the sole outbound host action P1 wires: opening
// an external URL. A webview-supplied url is checked against this SET —
// never used to build a URL, never pattern-matched (§8 W-SEC). Every entry is
// live-verified (docs/features/quickstart/scaling/etc, the recipes catalog,
// the walkthrough video) — no placeholder TODO links.
const EXTERNAL_URLS = new Set([
  "https://docs.zerops.io",
  "https://docs.zerops.io/quickstart",
  "https://docs.zerops.io/features/coding-agents",
  "https://docs.zerops.io/zcp/quickstart",
  "https://docs.zerops.io/features/infrastructure",
  "https://docs.zerops.io/features/scaling",
  "https://docs.zerops.io/features/env-variables",
  "https://docs.zerops.io/zerops-yaml/specification",
  "https://app.zerops.io/recipes",
  "https://app.zerops.io/recipes/showcase-recipe",
  "https://www.youtube.com/watch?v=spdmTicsIgg",
]);

// Duplicated from extension.js's own ZEMBED_DIR/ENV_FILE: welcome.js only
// needs the raw path to WATCH — the actual read+parse stays the injected,
// safety-owned deps.readZembedEnv() (one copy, in extension.js). Watching
// the wrong/stale path here would just mean no live updates, never a wrong
// read, so this duplication carries none of the risk the "one copy" rule
// (§1) guards against for the launch commands.
const ZEMBED_DIR = "/etc/zerops-zembed";
const ZEMBED_ENV_FILE = path.join(ZEMBED_DIR, "env.json");

// Credential probes exist only for agents with a live-verified artifact path
// (spec §3, v1: claude-code, codex). Every other agent (antigravity, grok,
// cursor) has no verifiable local signal — computeAgentState renders those
// from the platform flag alone.
const CRED_PROBE = {
  "claude-code": path.join(".claude", ".credentials.json"),
  "codex": path.join(".codex", "auth.json"),
};

// The directory each cred probe's file lives under — what the watchers
// below attach to (a dir-level watch survives the file itself not existing
// yet, and catches atomic-replace writes the file-level probe would miss
// between polls).
const CRED_WATCH_DIR = {
  "claude-code": ".claude",
  "codex": ".codex",
};

// The ONE sanctioned .zcp/state read (docs/spec-guided-mode.md §2): presence
// of this file, never its contents.
const GUIDED_MARKER_REL = path.join(".zcp", "state", "guided");

// Shared debounce for every watcher below — a single filesystem write can
// emit more than one fs.watch event (spec §3).
const STATE_PUSH_DEBOUNCE_MS = 400;

// ---- agent authorization (docs/spec-welcome-mode.md §4, W-AUTH) ----------

// Channel name + version the embedding Zerops GUI's receiver validates
// (frontend-legacy prototype/zcp-claude-auth-bridge, treated as fixed —
// see the P3 brief's "Frontend receiver contract"). Also duplicated
// (deliberately, like ZEMBED_DIR above) in welcome.html's inline script: the
// webview executes in a separate JS context with no require() boundary to
// this file, so it carries its own literal copy for its dumb-pipe relay
// filter. This copy is host-side and authoritative; the webview's is
// defense-in-depth only — this file re-validates every relayed message
// (§4, §8 W-SEC: "Host validates relayed messages again").
const BRIDGE_CHANNEL = "@zerops/zcp-agent-auth-bridge";
const BRIDGE_VERSION = 1;

// isAllowedGuiOrigin reports whether a received message's origin is TRUSTED
// for validating an INBOUND bridge ack: prod (app.zerops.io, exact host,
// default port only), a real dot-boundary subdomain of the Zerops-internal
// stage/dev domain (*.zerops.dev, default port only), a local dev server
// (http://localhost, any port — matches nginx's frame-ancestors
// "http://localhost:*"), or an exact origin the container operator opted in
// via ZCP_WELCOME_BRIDGE_ORIGINS (see resolveBridgeExtraOrigins below).
//
// Deliberately does NOT trust *.zerops.app by pattern. *.zerops.app is the
// shared CUSTOMER namespace — every Zerops service gets a public
// *.zerops.app URL, and the code-server's CSP frame-ancestors
// (internal/content/templates/nginx.conf.tmpl) lets ANY *.zerops.app page
// embed a victim's code-server. Trusting the suffix would let a malicious
// evil-….zerops.app page embed an authenticated code-server, receive the
// broadcast bridge trigger (incl. eventId), and forge an accepted:true ack —
// a trusted-response forgery that also suppresses the terminal fallback for
// the 10-minute flow cap. A specific *.zerops.app test/custom GUI is trusted
// ONLY when the operator names its exact origin via the env var above —
// never by suffix.
//
// PARSES the origin and checks scheme + exact host/port, or a real
// dot-boundary suffix — never a substring test, which is bypassable (e.g.
// "https://zerops.app.attacker.com"). Used to validate INBOUND acks; the
// OUTBOUND trigger stays broadcast (target "*", see sendBridgeMessage) since
// the webview can't read the cross-origin parent's origin and the trigger
// carries no secret — the frontend receiver is the confidentiality gate
// there (spec-welcome-mode.md §4 W-AUTH). This function is host-side and the
// SOLE origin authority: welcome.html's webview cannot see
// ZCP_WELCOME_BRIDGE_ORIGINS, so it no longer makes this decision — it only
// relays by channel, forwarding the browser-supplied origin for this
// function to judge.
function isAllowedGuiOrigin(origin, extraOrigins) {
  // Operator-configured exact origins (ZCP_WELCOME_BRIDGE_ORIGINS) — the only
  // way a *.zerops.app test/custom GUI is trusted: by exact origin, never by
  // the shared customer namespace's suffix.
  if (extraOrigins && extraOrigins.indexOf(origin) !== -1) return true;
  let u;
  try { u = new URL(origin); } catch (_) { return false; }
  const h = u.hostname;
  if (u.protocol === "https:") {
    if (u.port !== "") return false; // exact origin: default 443 only
    if (h === "app.zerops.io") return true;
    // real subdomain of the Zerops-exclusive stage/dev domain (non-empty
    // label before ".zerops.dev" — rejects a bare-dot host); NOT *.zerops.app
    // (customer namespace, see comment above).
    return h.length > ".zerops.dev".length && h.endsWith(".zerops.dev");
  }
  if (u.protocol === "http:") {
    return h === "localhost"; // any port, local dev (matches nginx frame-ancestors)
  }
  return false;
}

// resolveBridgeExtraOrigins reads ZCP_WELCOME_BRIDGE_ORIGINS — a comma-
// separated list of exact origins the container operator additionally trusts
// for inbound bridge acks (see isAllowedGuiOrigin above) — from the LIVE
// zembed store ONLY, never the extension host's frozen process.env: a running
// host froze process.env at code-server boot, so a value there would keep
// trusting an origin the operator has since removed from the live store (a
// stale-trust window). A readable store is authoritative (a missing/empty key
// means no extras); an unreadable store (readZembedEnv returns null) fails
// closed to no extras. This is the ONLY way a *.zerops.app test/custom GUI is
// trusted: the operator names its exact origin here, never by pattern.
//
// Resolved FRESH at every ack (handleBridgeWindowMessage), never cached: an
// operator adding OR revoking a trusted origin takes effect immediately,
// without reopening the panel. Each entry is canonicalized through
// new URL().origin so a non-canonical env value (trailing slash, explicit
// :443, uppercase host) still matches the browser-canonical event.origin;
// unparseable or opaque ("null") entries are dropped.
function resolveBridgeExtraOrigins(deps) {
  const env = deps.readZembedEnv();
  const raw = env ? env.ZCP_WELCOME_BRIDGE_ORIGINS : undefined;
  if (typeof raw !== "string" || raw === "") return [];
  const out = [];
  for (const entry of raw.split(",")) {
    const s = entry.trim();
    if (s === "") continue;
    let o;
    try { o = new URL(s).origin; } catch (_) { continue; }
    if (o && o !== "null") out.push(o);
  }
  return out;
}

// spec §4: how long we wait for the GUI's open-agent-auth-ack. The GUI now
// acks accepted:true only AFTER its dialog actually dispatches, itself
// bounded by its own ≤10s container-readiness check — 12s covers that plus
// margin. The standalone/no-GUI case (no receiver listening at all) pays
// this same, slower, rare fallback too.
const ACK_TIMEOUT_MS = 12_000;

// Defense-in-depth size cap on a relayed bridge message's data (spec §8
// W-SEC "size-capped") — generous for {channel,version,type,eventId,
// accepted,reason} but rejects a pathological payload before it is ever
// interpreted. Duplicated in welcome.html for the webview's own pre-filter;
// this copy is the one that actually matters (§8: "Host validates relayed
// messages again").
const BRIDGE_RELAY_MAX_BYTES = 1024;

// ---- community skill packs (docs/spec-welcome-mode.md §6) ----------------

// PACKS is the shipped allowlist for the "pack-action" step: the ONLY ids a
// {type:"pack-action"} click may install/remove (mirrors the retired curated
// SKILLS allowlist's own "never a path/id from the webview" discipline).
// Each id is installed/removed by the `zcp skills pack-add`/`pack-remove
// --json` CLI (a parallel Go slice) — this file never reads or writes pack
// content itself, and no longer reads a manifest file directly either: every
// pack row's live state comes from `zcp skills pack-status --json`
// (runPackStatus below), the CLI's own single state authority.
const PACKS = ["matt-pocock-skills", "superpowers", "andrej-karpathy-skills", "anthropic-skills"];
const PACK_IDS = new Set(PACKS);

let panel = null; // singleton — re-invoking open() reveals this, never recreates it
let disposables = []; // welcome-panel-scoped disposables (watchers, view-state listener): cleared on dispose
let pushTimer = null; // shared debounce timer for schedulePush()
let onboardInFlightUntil = 0; // single-flight guard: rapid onboard clicks must not spawn competing agent sessions (module-level, survives panel reopen)

// At most one authorization flow in flight per panel (spec §4) — module-level
// like `panel` above, safe for the same reason (welcomejs/harness.js gives
// every test its own uncached module instance). The panel offers bridge
// authorization only, so the slot only ever holds a bridge flow.
// Shape: { kind: "bridge", agentId, eventId, ackTimer }
let authFlow = null;

// At most one guided toggle in flight per panel (spec §5) — a SEPARATE lock
// from authFlow above: an agent authorization and a guided toggle may run
// concurrently, they just don't share a slot. Cleared ONLY by the spawned
// child's own exit/error handler (finishGuidedToggle) — never by a panel
// dispose, since the child keeps running regardless of whether anything is
// left to show its result to (see disposeWatchers's comment on this).
let guidedFlow = null; // { enable, child? } while a `zcp init [--guided]` run is in progress

// At most one skill-pack operation in flight per panel, and — per spec — ONE
// mutating operation (guided OR pack) in flight per panel overall: guidedFlow
// and packFlow each refuse when EITHER is held (see handleGuidedToggle's and
// handlePackAction's own busy checks), unlike guidedFlow/authFlow above,
// which are genuinely independent locks. Cleared only by the spawned CLI's
// own close/error handler, same non-negotiable-on-dispose treatment as
// guidedFlow (see disposeWatchers's comment on why).
let packFlow = null; // { id, action, child? } while a `zcp skills pack-add|pack-remove <id> --json` run is in progress

// selectedWorkspaceRoot is the workspace folder a guided toggle OR pack
// action last actually operated on (spec §3: "guided = presence of the
// marker in the SELECTED workspace folder" — the same "selected folder"
// concept now governs pack-status too) — set once selectWorkspaceFolder
// resolves a folder, whether that's the sole folder or the multi-root
// quickpick's pick, by EITHER handleGuidedToggle or handlePackAction.
// deps.workspaceRoot is fixed to the FIRST folder for the life of the panel
// (resolveDeps), so in a multi-root workspace it can name a DIFFERENT folder
// than the one either operation was just run in; collectFullState below
// prefers this field once it's set — for BOTH collectGuided and
// collectPacksState. Deliberately sticky across a panel dispose+reopen, like
// lastBridgeOutcome above: it names "the" guided/pack-relevant folder for
// this window, not in-flight state.
let selectedWorkspaceRoot = null;

// guidedMarkerWatcher/guidedMarkerWatcherRoot track which folder's marker
// the panel-scoped watcher (see reattachGuidedMarkerWatcher, watchers
// section below) currently points at, so a guided toggle OR pack action
// against a NEW folder can re-point it live instead of leaving it watching
// the stale one. packManifestsWatcher/packManifestsWatcherRoot are its exact
// sibling for the pack-manifests directory (reattachPackManifestWatcher,
// below).
let guidedMarkerWatcher = null;
let guidedMarkerWatcherRoot; // undefined = "never attached yet" — distinct from a real null (no folder)
let packManifestsWatcher = null;
let packManifestsWatcherRoot; // undefined = "never attached yet" — distinct from a real null (no folder)

// lastBridgeOutcome mirrors the BRIDGE flow's own phase transitions — a
// diagnostics-tile signal for "what
// happened last time this container attempted the bridge", module-level
// like panel/authFlow above (see harness.js's per-test uncached-module
// note). Deliberately survives a panel dispose+reopen (unlike authFlow,
// which is released on dispose): it is a debugging aid about the LAST
// attempt, not live in-flight state.
let lastBridgeOutcome = "-";

// lastEmbedded mirrors the webview's own {type:"ready"} report of whether it
// is running inside an iframe (`window.top !== window`) — a diagnostics-tile
// signal for "is anything even able to hear the bridge broadcast" (spec §4:
// the trigger is useless with no embedding GUI listening). null = unknown
// (no ready message has yet reported a valid boolean); a malformed embedded
// field on a later ready is treated as absent and leaves this untouched —
// same tolerate-and-ignore style as every other webview-reported field in
// this file. Module-level like lastBridgeOutcome above (survives a panel
// dispose+reopen; harness.js gives every test its own uncached module
// instance).
let lastEmbedded = null;

// Streams every guided toggle AND skill-pack action run's stdout/stderr —
// created ONCE, lazily, inside open()'s panel-creation branch (never on a
// reveal or a dispose+reopen) and left in ctx.subscriptions rather than the
// panel-scoped `disposables` above: closing the welcome panel mid-run must
// not lose where the output went (spec §5).
let guidedOutputChannel = null;

const GUIDED_NO_WORKSPACE_MESSAGE = "No workspace folder open — open a folder first.";
const GUIDED_AUTHORING_MESSAGE = "Guided is user-only; authoring mode is active.";
// GUIDED_BUSY_MESSAGE is handleGuidedToggle's OWN busy rejection copy —
// guided and a skill-pack action still hold ONE shared mutating-operation
// lock per panel (spec §6), but a pack-action's OWN busy rejection now goes
// through postPackResult's code:"busy" path instead (welcome.html's
// PACK_RESULT_CODE_TEXT owns that row-local copy) — the two surfaces
// (guided's single shared line vs a pack row's own line) intentionally carry
// separate wording for the same underlying lock.
const GUIDED_BUSY_MESSAGE = "A guided or skill-pack operation is already running.";
const GUIDED_DIRTY_MESSAGE = "Save AGENTS.md/CLAUDE.md first — zcp init rewrites them.";
const GUIDED_ENOENT_MESSAGE = "zcp binary not found in PATH.";
const GUIDED_MARKER_MISMATCH_MESSAGE = "zcp init finished but the guided marker doesn't match — check the Zerops Welcome output.";
// zcp init is non-transactional — the marker is written before the init
// steps run (docs/spec-guided-mode.md §3) — so a part-way failure can leave
// the marker flipped while other surfaces are stale. Report that honestly:
// never a silent success, never a claimed rollback (spec §5).
const GUIDED_PARTIAL_FAILURE_MESSAGE = "zcp init failed part-way — the preference may be recorded but surfaces are partially refreshed. Re-run from the toggle or run zcp init in a terminal (see output).";

// handleGuidedToggle's claude-code-runnable rejection (spec §6): guided
// currently requires claude-code SPECIFICALLY runnable (installed &&
// authorized/authorized-token) — not any runnable agent, and — unlike
// before — NOT skill packs either: packs dropped this gate entirely (spec
// §6 revision: they're inert workspace files, installing one needs no agent
// running at all). Matches welcome.html's data-guided-locked-note copy
// verbatim — see isClaudeCodeRunnable below for the host-side re-check this
// backs.
const GUIDED_CLAUDE_CODE_REQUIRED_MESSAGE = "Authorize Claude Code first to use Zerops Guided.";

// ---- pure state (docs/spec-welcome-mode.md §3, W-STATE / W4) -------------

// computeAgentState implements the §3 matrix EXACTLY: the platform flag and
// the local credential artifact are two INDEPENDENT inputs that compose a
// matrix, never a boolean union. authType (ZCP_AGENT_AUTH_TYPE_<SUFFIX>) is
// accepted because the collector reads it for free alongside
// flagOAuth/flagToken, but every matrix row is already fully determined by
// the other four fields, so it never changes the result.
function computeAgentState({ flagOAuth, flagToken, authType, credPresent, credVerifiable }) {
  if (flagToken) return "authorized-token"; // token env present -> authorized-token, regardless of cred
  if (flagOAuth) {
    if (credPresent) return "authorized";
    if (credVerifiable) return "reconnect"; // flag present, verified probe finds no local cred: rebuild-orphaned flag
    // Agents without a verified probe render from the platform flag alone
    // (spec §3) — there is no local signal to contradict it with.
    return "authorized";
  }
  return credPresent ? "local-only" : "not-authorized";
}

// shortenServiceId renders a service id for the diagnostics tile: enough to
// recognize/compare across a screenshot or a support conversation, never the
// full value (spec §8 W-SEC: no env values/paths beyond what the user
// needs). No serviceId key in the zembed store -> "-", the same sentinel
// every other diagnostics field uses when it has nothing.
function shortenServiceId(id) {
  if (!id) return "-";
  return id.length > 8 ? id.slice(0, 8) + "…" : id;
}

// buildState assembles the full webview payload from already-collected
// inputs — pure, no I/O of its own (collectFullState below does the
// reading). anyAuthorized gates steps 2+ (§7 W-CTA): only "authorized" and
// "authorized-token" unlock it — "local-only" is platform-unverified and
// must NOT unlock. anyRunnable (below) is the SEPARATE launch gate: an
// authorized flag for a binary that isn't actually on this container's PATH
// must not unlock a launch surface.
function buildState(inputs) {
  const {
    registry = {}, agentIds = [], zembedEnv: env = null, creds = {}, installed = {},
    guided = { state: "unknown" }, packs = [],
    extensionVersion = "-", lastBridgeOutcome = "-", embedded = null,
  } = inputs || {};

  const agents = agentIds.map((id) => {
    const reg = registry[id] || {};
    const suffix = reg.suffix || "";
    const flagOAuth = !!env && env["ZCP_AGENT_OAUTH_" + suffix] === "true";
    const flagToken = !!env && !!env["ZCP_AGENT_TOKEN_" + suffix];
    const authType = env ? env["ZCP_AGENT_AUTH_TYPE_" + suffix] : undefined;
    const cred = creds[id] || { present: false, verifiable: false };
    const state = computeAgentState({
      flagOAuth, flagToken, authType,
      credPresent: cred.present, credVerifiable: cred.verifiable,
    });
    return { id, label: reg.label || id, state, probeVerified: cred.verifiable, installed: !!installed[id] };
  });

  const zembedSeen = !!env;

  return {
    agents,
    anyAuthorized: agents.some((a) => a.state === "authorized" || a.state === "authorized-token"),
    // The launch gate (spec §7 W-CTA): installed AND authorized/
    // authorized-token. A platform flag alone (anyAuthorized above) is not
    // enough to unlock a launch surface when the agent's binary isn't on
    // this container's PATH.
    anyRunnable: agents.some((a) => a.installed && (a.state === "authorized" || a.state === "authorized-token")),
    guided,
    packs,
    bridge: { status: "unknown" }, // P3 fills
    environment: { zembed: zembedSeen },
    // Small muted diagnostics tile (welcome.html): container/runtime signal
    // ONLY, never env values/tokens/paths beyond the two fields below that
    // are deliberately truncated (spec §8 W-SEC). embedded (true/false/
    // null=unknown) is the webview's own self-report of whether it is
    // rendering inside an iframe — "you're clicking in a tab no GUI can
    // hear" is otherwise silent and hard to diagnose.
    diagnostics: {
      zembedSeen,
      extensionVersion,
      serviceId: shortenServiceId(env && typeof env.serviceId === "string" ? env.serviceId : null),
      lastBridgeOutcome,
      embedded,
    },
  };
}

// ---- deps resolution -------------------------------------------------

// resolveDeps fills in every optional collaborator extension.js's fixed call
// site never passes. workspaceRoot uses !== undefined (not ||) so a test can
// explicitly assert "no workspace folder" with workspaceRoot: null, distinct
// from "unspecified, use the real default".
function resolveDeps(deps) {
  const d = deps || {};
  const allAgentIds = d.ALL_AGENT_IDS || [];
  return {
    REGISTRY: d.REGISTRY || {},
    ALL_AGENT_IDS: allAgentIds,
    readZembedEnv: d.readZembedEnv || (() => null),
    runAgentAction: d.runAgentAction || (() => {}),
    // resolveAvailableAgentIds (ZCP_AGENTS presentation axis) / isAgentInstalled
    // (PATH probe axis) — the same two collaborators extension.js's launcher
    // already resolves for itself, injected here so welcome.js composes the
    // identical §3 matrix. Production (the zerops.welcome command handler)
    // always injects the real single-copy resolver/probe; the permissive
    // defaults below exist ONLY for tests/portable direct open() callers that
    // skip them, mirroring production's own key-absent/no-store behavior
    // (every agent offered, nothing probed as missing) so those callers keep
    // seeing every agent exactly as before this axis existed — a fail-closed
    // default here would render every action dead for no user gain.
    resolveAvailableAgentIds: d.resolveAvailableAgentIds || (() => allAgentIds),
    isAgentInstalled: d.isAgentInstalled || (() => true),
    fs: d.fs || fs,
    homeDir: d.homeDir || os.homedir(),
    workspaceRoot: d.workspaceRoot !== undefined ? d.workspaceRoot : defaultWorkspaceRoot(),
    // Every open workspace folder's fsPath, resolved once like workspaceRoot
    // above (folders don't change without a window reload) — the shared
    // guided/pack-action folder-selection seam (single vs quickpick, spec
    // §5/§6, selectWorkspaceFolder below), kept separate from workspaceRoot
    // since collectGuided/collectPacksState/watchGuidedMarker/
    // watchPackManifests all key off the SELECTED folder instead (see
    // selectedWorkspaceRoot).
    workspaceFolders: d.workspaceFolders !== undefined ? d.workspaceFolders : defaultWorkspaceFolders(),
    // Read FRESH at use time (a function, like readZembedEnv), never
    // resolved once: unlike workspaceFolders, which text document is dirty
    // can change at any moment while the panel sits open.
    textDocuments: d.textDocuments || defaultTextDocuments,
    // Timer + spawn seams for the auth flow below (ACK/cap timers, mark-oauth
    // invocation) — real by default, swappable in tests for a fake clock /
    // recorded child-process calls (welcomejs/harness.js makeFakeTimers()).
    setTimeout: d.setTimeout || ((fn, ms) => setTimeout(fn, ms)),
    clearTimeout: d.clearTimeout || ((id) => clearTimeout(id)),
    spawn: d.spawn || defaultSpawn,
    // Multi-root folder picker for the guided toggle (spec §5) — injectable
    // so tests control which folder gets "picked" without a real UI.
    showQuickPick: d.showQuickPick || ((items, options) => vscode.window.showQuickPick(items, options)),
    // Workspace-trust gate for skill-pack operations (spec §6: "Untrusted
    // ... contexts refuse writes"). A FUNCTION, read FRESH at each
    // pack-action click — never resolved once at panel-open time like
    // workspaceRoot: a trust grant/revoke while the panel sits open must be
    // observed immediately, not the value captured when the panel opened.
    // Tri-state like the real API (true/false/undefined) — only a LITERAL
    // false rejects, so an older host (or a caller that never sets it)
    // degrades to "trusted".
    isTrusted: d.isTrusted || defaultIsTrusted,
    // Clipboard-first CTA kickoff (spec §7 W-CTA) — the ONLY mechanism
    // handleStartOnboarding may use to hand a kickoff prompt to the agent:
    // NEVER terminal.sendText, NEVER a delayed setTimeout injection (a
    // terminal may not even be running the agent). Injectable for tests,
    // real vscode.env.clipboard by default.
    clipboard: d.clipboard || { writeText: (text) => vscode.env.clipboard.writeText(text) },
    // Post-copy nudge alongside the clipboard write above — same
    // injectable-for-tests treatment as showQuickPick above.
    showInformationMessage: d.showInformationMessage || ((message, ...items) => vscode.window.showInformationMessage(message, ...items)),
  };
}

function defaultWorkspaceRoot() {
  try {
    const folders = vscode.workspace && vscode.workspace.workspaceFolders;
    if (folders && folders.length > 0) return folders[0].uri.fsPath;
  } catch (_) {}
  return null;
}

function defaultWorkspaceFolders() {
  try {
    const folders = vscode.workspace && vscode.workspace.workspaceFolders;
    if (folders && folders.length > 0) return folders.map((f) => f.uri.fsPath);
  } catch (_) {}
  return [];
}

function defaultTextDocuments() {
  try {
    return (vscode.workspace && vscode.workspace.textDocuments) || [];
  } catch (_) {
    return [];
  }
}

function defaultIsTrusted() {
  try {
    return vscode.workspace && vscode.workspace.isTrusted;
  } catch (_) {
    return undefined;
  }
}

// ---- effectful input collectors -------------------------------------
// Each collector is null/missing-tolerant: a container without zembed, a
// fresh HOME with no agent ever logged in, or a window with no workspace
// folder open all degrade to a safe default, never throw.

// collectCred existence-checks ONE agent's credential artifact — never reads
// its contents (spec §3/§8: presence-only, no credential value ever enters
// this process' view of the world).
function collectCred(fsImpl, homeDir, agentId) {
  const rel = CRED_PROBE[agentId];
  if (!rel) return { present: false, verifiable: false };
  let present = false;
  try { present = fsImpl.existsSync(path.join(homeDir, rel)); } catch (_) { present = false; }
  return { present, verifiable: true };
}

function collectAllCreds(fsImpl, homeDir, agentIds) {
  const out = {};
  for (const id of agentIds) out[id] = collectCred(fsImpl, homeDir, id);
  return out;
}

// collectGuided presence-checks the ONE sanctioned .zcp/state read (spec §3,
// docs/spec-guided-mode.md §2) — never opens or parses the marker file. No
// workspace folder selected -> "unknown" (undecidable without a folder).
function collectGuided(fsImpl, workspaceRoot) {
  if (!workspaceRoot) return { state: "unknown" };
  let present = false;
  try { present = fsImpl.existsSync(path.join(workspaceRoot, GUIDED_MARKER_REL)); } catch (_) { present = false; }
  return { state: present ? "enabled" : "disabled" };
}

// PACK_MANIFESTS_DIR_REL is the skill-packs manifest directory's path
// relative to a workspace root — the target watchPackManifests (the
// panel-scoped watcher, below) points at. The CLI still writes its manifest
// files here per pack (docs/spec-dataconsole.md-style single-owner state),
// but this file no longer reads them directly (see packsStatusCache below):
// a manifest write is only ever a trigger to re-run pack-status, never a
// truth source itself.
const PACK_MANIFESTS_DIR_REL = path.join(".zcp", "state", "skill-packs");

// packsStatusCache holds the last-known `zcp skills pack-status --json`
// result for ONE folder (spec §4/§6) — the CLI's own pack-status contract is
// now the SOLE state authority for every pack row; this file no longer
// derives installed/absent/etc from a manifest existsSync probe. `root` lets
// collectPacksState below tell "no result yet for THIS folder" (renders
// every row "checking") apart from "a stale result for a folder we've since
// left" — a folder switch naturally invalidates the cache via this root
// mismatch, no separate clear is needed.
let packsStatusCache = null; // { root, packs: [{id, state, managed}] } | null

// packsStatusGeneration guards a stale (superseded) pack-status run's result
// from ever overwriting a NEWER run's (spec §4: "a monotonically increasing
// request generation; a stale result never overwrites a newer one"). Every
// trigger — ready, reveal/focus, a completed pack/guided operation, the
// debounced manifest watcher — starts a fresh run via runPackStatus and
// increments this counter; a run whose captured generation no longer matches
// the current one when it completes is dropped silently, same discipline as
// watchWithFallback's own per-watcher generation guard above.
let packsStatusGeneration = 0;

// Defense-in-depth size cap on a captured `pack-status`/pack-add/pack-remove
// --json run's stdout (spec §6 "bounded stdout capture") — generous for the
// small JSON object either contract prints, but stops a pathological or
// truncated/garbage capture from ever growing unbounded before parsing.
const PACK_JSON_STDOUT_CAP_BYTES = 64 * 1024;

// parsePackJSON extracts the single JSON object either the pack-status or
// pack-add/pack-remove --json contract prints on stdout — tolerant of
// surrounding whitespace, but a truncated (cap-exceeded), garbage, or
// non-object capture parses to null rather than throwing: the caller treats
// null as "nothing usable came back", never a crash.
function parsePackJSON(raw) {
  if (typeof raw !== "string") return null;
  const trimmed = raw.trim();
  if (trimmed === "") return null;
  try {
    const parsed = JSON.parse(trimmed);
    return parsed && typeof parsed === "object" ? parsed : null;
  } catch (_) {
    return null;
  }
}

// runPackStatus spawns `zcp skills pack-status --json` for `root` (spec §4)
// — every pack row's live state comes from here, not a live fs probe. No
// workspace folder abandons any in-flight run and clears the cache outright
// (collectPacksState already renders [] with no root, spec's "no packs at
// all" case); a spawn failure, an unparseable response, or a superseded
// (stale-generation) completion all leave whatever was cached (or
// "checking") in place rather than ever show a wrong state.
function runPackStatus(deps, root) {
  if (!root) {
    packsStatusGeneration++; // abandon any in-flight run for a folder we've left
    // Only push state if there's actually something to invalidate — a caller
    // triggering this with no folder open (the common no-workspace case) and
    // no prior cache must not double the caller's own already-pushed state
    // (every trigger site here already calls postState itself).
    if (packsStatusCache !== null) {
      packsStatusCache = null;
      postState(deps);
    }
    return;
  }
  const myGen = ++packsStatusGeneration;
  let child;
  try {
    child = deps.spawn("zcp", ["skills", "pack-status", "--json"], { cwd: root, shell: false });
  } catch (err) {
    console.warn("[zcp-welcome] zcp skills pack-status failed to start:", err);
    return;
  }
  if (!child || typeof child.on !== "function") return; // a minimal test stub with nothing to observe

  let stdoutCaptured = "";
  if (child.stdout && typeof child.stdout.on === "function") {
    child.stdout.on("data", (chunk) => {
      stdoutCaptured = (stdoutCaptured + chunk.toString()).slice(0, PACK_JSON_STDOUT_CAP_BYTES);
    });
  }

  let settled = false;
  const settle = () => {
    if (settled) return;
    settled = true;
    if (myGen !== packsStatusGeneration) return; // superseded by a newer run — see the doc-comment above
    const parsed = parsePackJSON(stdoutCaptured);
    if (!parsed || !Array.isArray(parsed.packs)) {
      console.warn("[zcp-welcome] zcp skills pack-status returned an unparsable response");
      return; // keep whatever was cached (or "checking") rather than show a wrong state
    }
    packsStatusCache = { root, packs: parsed.packs };
    postState(deps);
  };
  // close (streams drained), not exit — mirrors handlePackAction's own
  // discipline below: parsing stdoutCaptured before it is guaranteed fully
  // buffered would risk an incomplete read.
  child.on("error", (err) => {
    console.warn("[zcp-welcome] zcp skills pack-status failed:", err);
    settle();
  });
  child.on("close", settle);
}

// collectPacksState renders the four shipped PACKS ids from packsStatusCache
// (spec §3/§4/§6) — never a live fs probe: state now lives entirely behind
// the CLI's pack-status contract. No cached result yet FOR THIS FOLDER (a
// fresh panel/reveal/folder-select before its first pack-status run lands)
// renders every row "checking" — the webview disables the toggle for that
// state. No workspace folder selected means nothing could ever have been
// installed, so it reports an empty list rather than four rows (mirrors the
// retired collectSkillsState's own no-workspace behavior).
function collectPacksState(root) {
  if (!root) return [];
  const cached = packsStatusCache && packsStatusCache.root === root ? packsStatusCache.packs : null;
  const byId = {};
  if (cached) for (const p of cached) byId[p.id] = p;
  return PACKS.map((id) => {
    const found = byId[id];
    return found ? { id, state: found.state, managed: !!found.managed } : { id, state: "checking", managed: false };
  });
}

// Cached module-level like panel/authFlow above (see harness.js's per-test
// uncached-module note): the installed extension dir's OWN package.json
// version cannot change without an upgrade + window reload, which tears
// down this whole module instance anyway — re-reading it on every state
// push (every watcher tick) would be pure waste.
let cachedExtensionVersion = null;

// readExtensionVersion mirrors readWelcomeHtml's use of the REAL fs module,
// never deps.fs: ctx.extensionPath/package.json is a real, always-present
// sibling of welcome.html in the installed extension dir, regardless of
// what a test fakes deps.fs to be.
function readExtensionVersion(extensionPath) {
  if (cachedExtensionVersion !== null) return cachedExtensionVersion;
  try {
    const pkg = JSON.parse(fs.readFileSync(path.join(extensionPath, "package.json"), "utf8"));
    cachedExtensionVersion = typeof pkg.version === "string" && pkg.version ? pkg.version : "-";
  } catch (err) {
    console.warn("[zcp-welcome] reading extension package.json failed:", err);
    cachedExtensionVersion = "-";
  }
  return cachedExtensionVersion;
}

// collectInstalled probes deps.isAgentInstalled (spec §3, the PATH-probe
// axis) for each agent id's registry-declared `bin` — an id absent from the
// registry, or a registry entry with no `bin`, probes as not installed
// rather than calling isAgentInstalled with a missing binary name. `env` (the
// live zembed store, possibly null) rides along so the probe can search the
// store's PATH too — the extension host's own frozen PATH is narrower than
// the runtime profile PATH (see isAgentInstalled in extension.js).
function collectInstalled(deps, agentIds, env) {
  const out = {};
  for (const id of agentIds) {
    const bin = deps.REGISTRY[id] && deps.REGISTRY[id].bin;
    out[id] = !!bin && deps.isAgentInstalled(bin, env);
  }
  return out;
}

function collectFullState(deps) {
  const env = deps.readZembedEnv();
  // The availability axis (ZCP_AGENTS, spec §3) narrows which agents even
  // appear in the payload — creds/installed are only ever collected for the
  // ids this container actually offers.
  const availableIds = deps.resolveAvailableAgentIds(env);
  // selectedWorkspaceRoot (the folder a guided OR pack action actually ran
  // against) takes priority over deps.workspaceRoot's fixed first-folder
  // default — see its own doc-comment above (Finding 4 / spec §3 "selected
  // workspace folder") — for BOTH collectGuided and collectPacksState: a
  // multi-root pack action against folder B must not read folder A's
  // pack-status cache back.
  const root = selectedWorkspaceRoot || deps.workspaceRoot;
  return buildState({
    registry: deps.REGISTRY,
    agentIds: availableIds,
    zembedEnv: env,
    creds: collectAllCreds(deps.fs, deps.homeDir, availableIds),
    installed: collectInstalled(deps, availableIds, env),
    guided: collectGuided(deps.fs, root),
    packs: collectPacksState(root),
    extensionVersion: readExtensionVersion(deps.extensionPath),
    lastBridgeOutcome,
    embedded: lastEmbedded,
  });
}

function readWelcomeHtml(ctx, nonce) {
  const htmlPath = path.join(ctx.extensionPath, "welcome.html");
  const raw = fs.readFileSync(htmlPath, "utf8");
  return raw.split(NONCE_PLACEHOLDER).join(nonce);
}

function postState(deps) {
  if (!panel) return;
  const state = collectFullState(deps);
  // Before pushing state, close out any in-flight auth flow whose
  // completion signal this same recompute just observed — so the client
  // sees the idle transition alongside (never strictly after) the state
  // that motivated it. See reconcileAuthFlow.
  reconcileAuthFlow(deps, state);
  try {
    panel.webview.postMessage({ type: "state", payload: state });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// unrefTimer mirrors unrefWatcher (see below) for setTimeout handles: a real
// Node Timeout supports .unref() so it can't block process exit; test-
// injected fake timer ids generally don't, so the check is required, not
// defensive noise.
function unrefTimer(t) {
  if (t && typeof t.unref === "function") t.unref();
}

// postAuth pushes one {type:"auth"} phase message for one agent — the
// per-tile progress line welcome.html renders from (contacting / busy /
// dialog-opening / no-dashboard / gui-not-ready / unsupported / idle, spec
// §4).
function postAuth(agentId, phase) {
  if (!panel) return;
  try {
    panel.webview.postMessage({ type: "auth", agentId, phase });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// postBridgeAuth is postAuth's bridge-flow-specific sibling: every call site
// that reports a BRIDGE flow's phase to the webview
// goes through this one function instead of postAuth directly, so
// lastBridgeOutcome (the diagnostics tile's signal, above) can never drift
// out of sync with what postAuth actually sent.
function postBridgeAuth(agentId, phase) {
  lastBridgeOutcome = phase;
  postAuth(agentId, phase);
}

// releaseAuthFlow clears the in-flight bridge flow: cancels its timers (and
// disposes any listener it registered). Never posts anything itself — every
// call site decides what (if anything) to tell the UI, since the panel-dispose
// cleanup path (see open()) needs silence.
function releaseAuthFlow(deps) {
  if (!authFlow) return;
  if (authFlow.ackTimer) deps.clearTimeout(authFlow.ackTimer);
  if (authFlow.capTimer) deps.clearTimeout(authFlow.capTimer);
  if (authFlow.closeDisposable) {
    try { authFlow.closeDisposable.dispose(); } catch (_) {}
  }
  authFlow = null;
}

// reconcileAuthFlow runs on every state recompute (postState, above) and
// closes the in-flight bridge flow once its completion signal has arrived, OR
// once its agent stops being actionable:
//   - not actionable: authFlow.agentId is absent from state.agents (a
//     ZCP_AGENTS edit dropped it from the availability axis) or its row's
//     `installed` flipped false (its binary was removed) — released
//     immediately, ahead of the completion check below, since a bridge ack can
//     never arrive for an agent this container no longer offers/has. Without
//     this, the single-flow lock (spec §4: "at most one authorization flow in
//     flight") would sit held, and the webview's phase line would stay stuck
//     on whatever it last showed — "idle" clears it, same as every other
//     release path here.
//   - completion: the zembed watcher flips the agent's computed state to
//     authorized/authorized-token (the platform flag landed, spec §4).
// The bridge ack/timeout (handleBridgeWindowMessage) is the other, timer-
// driven release path.
function reconcileAuthFlow(deps, state) {
  if (!authFlow) return;
  const agent = state.agents.find((a) => a.id === authFlow.agentId);
  if (!agent || !agent.installed) {
    const id = authFlow.agentId;
    releaseAuthFlow(deps);
    postAuth(id, "idle");
    return;
  }
  if (authFlow.kind === "bridge") {
    if (agent.state === "authorized" || agent.state === "authorized-token") {
      const agentId = authFlow.agentId;
      releaseAuthFlow(deps);
      postBridgeAuth(agentId, "idle");
    }
    return;
  }
}

// sendBridgeMessage instructs the webview to relay `payload` to the
// embedding GUI via window.top.postMessage — see welcome.html's
// "bridge-send" handler. Broadcasts (target "*"): the webview cannot read
// the cross-origin parent's real origin to target it precisely, and
// `payload` carries no secret (§4) — the frontend receiver is the actual
// security gate, validating that the message came from the exact embedded
// iframe. All protocol logic (what to send, when) lives here; the webview
// is a dumb pipe.
function sendBridgeMessage(payload) {
  if (!panel) return;
  try {
    panel.webview.postMessage({ type: "bridge-send", payload, target: "*" });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// isAgentActionable is the shared availability+installed gate every
// launch-adjacent action below (bridge authorize, terminal authorize,
// open-agent) re-checks FRESH at click time (never cached on the flow): an
// agent this container doesn't offer (ZCP_AGENTS, spec §3) or whose binary
// isn't on this host's PATH cannot be acted on, regardless of its platform
// auth flag. Which agents the embedding Zerops GUI's bridge dialog can
// itself handle is a SEPARATE, downstream question — its own ack
// (accepted:false, reason:"unsupported-agent", see
// handleBridgeWindowMessage) is the authority there; this gate only ever
// answers "does zcp even offer/have this agent".
function isAgentActionable(agentId, deps) {
  const env = deps.readZembedEnv();
  const available = deps.resolveAvailableAgentIds(env);
  const reg = deps.REGISTRY[agentId];
  return available.includes(agentId) && !!reg && !!reg.bin && deps.isAgentInstalled(reg.bin, env);
}

// isClaudeCodeRunnable re-derives the §3 RUNNABLE state (installed AND
// authorized/authorized-token) for claude-code specifically — the gate
// handleGuidedToggle re-checks fresh at click time (below); skill packs no
// longer use this gate at all (spec §6 revision). Recomputing full state
// here, rather than trusting the webview's last-rendered lock, is the same
// "hiding a control is not authority" discipline isAgentActionable enforces
// above: an authorization revoked or a binary removed between the last state
// push and this click must still be caught host-side.
function isClaudeCodeRunnable(deps) {
  const state = collectFullState(deps);
  const agent = state.agents.find((a) => a.id === "claude-code");
  return !!agent && agent.installed && (agent.state === "authorized" || agent.state === "authorized-token");
}

// handleAuthorize starts (or rejects) the bridge flow for a webview
// {type:"authorize", agentId} click (spec §4). agentId is already known to
// be one of the five launcher-registry ids — handleMessage's allowlist gate
// checked that; isAgentActionable failing is a legitimate "not supported"
// case, answered with phase "unsupported", not a silent drop. Bridge
// support itself is no longer a zcp-owned list here (see
// isAgentActionable's own comment) — an agent zcp offers and has installed
// still goes through the bridge, and the GUI receiver rejects what it can't
// handle via its own ack.
function handleAuthorize(agentId, deps) {
  if (!isAgentActionable(agentId, deps)) {
    postBridgeAuth(agentId, "unsupported");
    return;
  }
  if (authFlow) {
    postBridgeAuth(agentId, "busy");
    return;
  }
  const eventId = crypto.randomUUID();
  const payload = {
    channel: BRIDGE_CHANNEL,
    version: BRIDGE_VERSION,
    type: "open-agent-auth",
    agentType: agentId,
    eventId,
    createdAt: Date.now(),
  };
  authFlow = { kind: "bridge", agentId, eventId, ackTimer: null, capTimer: null };
  // "contacting" separates "the trigger left this host" from "the GUI never
  // accepted" (no-dashboard) — posted immediately once the flow is
  // installed, before the ack timer is armed or the trigger is even sent.
  postBridgeAuth(agentId, "contacting");
  const ackTimer = deps.setTimeout(() => {
    if (!authFlow || authFlow.kind !== "bridge" || authFlow.eventId !== eventId) return;
    releaseAuthFlow(deps);
    postBridgeAuth(agentId, "no-dashboard");
  }, ACK_TIMEOUT_MS);
  unrefTimer(ackTimer);
  authFlow.ackTimer = ackTimer;
  sendBridgeMessage(payload);
}

// isWellFormedBridgeRelay is handleMessage's allowlist check (§8 W-SEC) for
// an inbound {type:"bridge-window-message"} from the webview's dumb-pipe
// relay: exact channel/version, primitive-typed fields, size-capped. Origin
// allowlisting and eventId matching are FLOW-state checks, not shape
// checks — they live in handleBridgeWindowMessage below, alongside the
// other flow-dependent decisions (busy/unsupported).
function isWellFormedBridgeRelay(msg) {
  if (typeof msg.origin !== "string") return false;
  if (!msg.data || typeof msg.data !== "object") return false;
  const d = msg.data;
  if (d.channel !== BRIDGE_CHANNEL) return false;
  if (d.version !== BRIDGE_VERSION) return false;
  if (typeof d.type !== "string") return false;
  if (typeof d.eventId !== "string") return false;
  if (d.accepted !== undefined && typeof d.accepted !== "boolean") return false;
  if (d.reason !== undefined && typeof d.reason !== "string") return false;
  let size = 0;
  try { size = JSON.stringify(d).length; } catch (_) { return false; }
  return size <= BRIDGE_RELAY_MAX_BYTES;
}

// handleBridgeWindowMessage re-validates a relayed ack against the LIVE
// flow state (§8 W-SEC: "Host validates relayed messages again — defense in
// depth") — origin allowlisted, eventId matching the in-flight bridge flow
// — before acting on accepted/reason. Anything else (no bridge flow in
// flight, wrong origin, foreign eventId, an unrecognized accepted/reason
// combination) is dropped silently, per spec §4.
function handleBridgeWindowMessage(msg, deps) {
  if (!authFlow || authFlow.kind !== "bridge") {
    console.log("[zcp-welcome] dropped bridge message: no bridge flow in flight");
    return;
  }
  if (!isAllowedGuiOrigin(msg.origin, resolveBridgeExtraOrigins(deps))) {
    console.log("[zcp-welcome] dropped bridge message: origin not allowlisted");
    return;
  }
  const data = msg.data;
  if (data.eventId !== authFlow.eventId) {
    console.log("[zcp-welcome] dropped bridge message: eventId mismatch");
    return;
  }
  if (data.type !== "open-agent-auth-ack") {
    console.log("[zcp-welcome] dropped bridge message: unexpected type " + data.type);
    return;
  }

  const agentId = authFlow.agentId;
  if (data.accepted === true) {
    // The trigger did its job — the GUI is opening its dialog — so the
    // single-flow lock is released NOW, not held until the platform flag
    // lands or a cap fires. The GUI has no way to report a dismissal:
    // holding the lock here turned every re-click after a dismissed dialog
    // into a silent "busy" dead zone (live-reported on febridge — reload,
    // authorize, dismiss, authorize again → nothing). A re-click simply
    // starts a fresh flow (new eventId; the GUI dedups per eventId), and
    // authorization COMPLETION is observed independently of this lock — the
    // zembed watcher flips the agent's state when the flag lands, and the
    // webview clears the stale phase line on any state change.
    releaseAuthFlow(deps);
    postBridgeAuth(agentId, "dialog-opening");
    return;
  }
  if (data.accepted === false && data.reason === "unsupported-agent") {
    releaseAuthFlow(deps);
    postBridgeAuth(agentId, "unsupported");
    return;
  }
  if (data.accepted === false && data.reason === "not-ready") {
    // The GUI validated the trigger but could not open its dialog (bounded
    // by its own container-readiness check) — a released, terminal outcome
    // for THIS flow, same as unsupported-agent above, never a retry signal.
    releaseAuthFlow(deps);
    postBridgeAuth(agentId, "gui-not-ready");
    return;
  }
  console.log("[zcp-welcome] dropped bridge ack: unexpected accepted/reason combination");
}

// ---- guided toggle (docs/spec-welcome-mode.md §5, W-GUIDED) --------------

// isAuthoringMode mirrors extension.js's own agentTypesFrom precedent:
// prefer the LIVE zembed store when it has an opinion, fall back to the
// extension host's frozen process.env only when the store doesn't carry the
// key at all. Go's own gate (internal/runtime/runtime.go) treats exactly
// "1" as authoring — this mirrors that exactly, never a generic truthy check.
function isAuthoringMode(deps) {
  const env = deps.readZembedEnv();
  const zembedVal = env ? env.ZCP_AUTHORING : undefined;
  if (zembedVal !== undefined) return zembedVal === "1";
  return process.env.ZCP_AUTHORING === "1";
}

// anyDirtyGuardedDoc reports whether AGENTS.md or CLAUDE.md directly under
// selectedFolder is open with unsaved changes — zcp init rewrites both, so
// running over an unsaved edit would silently discard it (spec §5).
function anyDirtyGuardedDoc(deps, selectedFolder) {
  const guarded = new Set([path.join(selectedFolder, "AGENTS.md"), path.join(selectedFolder, "CLAUDE.md")]);
  let docs = [];
  try { docs = deps.textDocuments() || []; } catch (_) { docs = []; }
  return docs.some((d) => d && d.isDirty && d.uri && guarded.has(d.uri.fsPath));
}

// selectWorkspaceFolder resolves which workspace folder a guided OR pack
// toggle runs against (spec §5/§6, shared seam — selectedWorkspaceRoot is
// the sticky result of the last call that actually ran): the sole folder
// needs no prompt; multiple folders ask via deps.showQuickPick — NEVER a
// hardcoded path. Returns null when there is no workspace, or the user
// cancels the picker.
async function selectWorkspaceFolder(deps) {
  const folders = deps.workspaceFolders;
  if (!folders || folders.length === 0) return null;
  if (folders.length === 1) return folders[0];
  const picked = await deps.showQuickPick(folders, { placeHolder: "Select a workspace folder" });
  return picked || null;
}

// streamChildOutput pipes a spawned child's stdout/stderr into the guided
// output channel, one line at a time — displayed only, NEVER parsed for
// success (completion is the caller's own contract: an exit/close code plus,
// for guided, a marker re-read, or, for a pack action, the CLI's own JSON
// response — never output-prose parsing). Returns { flush() }: the caller
// invokes it once the child's streams are fully drained (its own terminal
// event), so a FINAL, newline-less partial line buffered from either stream
// is appended then rather than silently dropped — real process output
// doesn't always end on a newline (e.g. a crash mid-line).
function streamChildOutput(child, channel) {
  if (!channel) return { flush() {} };
  const buffers = {};
  for (const key of ["stdout", "stderr"]) {
    const stream = child[key];
    if (!stream || typeof stream.on !== "function") continue;
    buffers[key] = "";
    stream.on("data", (chunk) => {
      buffers[key] += chunk.toString();
      const lines = buffers[key].split("\n");
      buffers[key] = lines.pop(); // keep the trailing partial line for the next chunk
      for (const line of lines) channel.appendLine(line.replace(/\r$/, ""));
    });
  }
  return {
    flush() {
      for (const key of Object.keys(buffers)) {
        if (buffers[key]) {
          channel.appendLine(buffers[key].replace(/\r$/, ""));
          buffers[key] = "";
        }
      }
    },
  };
}

// postGuidedResult sends a guided toggle run's outcome to the webview (spec
// §5) — the one new outbound message type this slice adds.
function postGuidedResult(result) {
  if (!panel) return;
  try {
    panel.webview.postMessage(Object.assign({ type: "guided-result" }, result));
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// finishGuidedToggle releases the guided lock, reports the outcome (a null
// result — the quickpick-cancel case only — still reports a bare {ok:false}
// so the webview's optimistic "running…" toggle always clears), and always
// pushes fresh state afterward: the marker may have moved regardless of
// which path finished (spec §5: "always release the lock; always push
// fresh state after any outcome").
function finishGuidedToggle(deps, result) {
  guidedFlow = null;
  postGuidedResult(result || { ok: false });
  postState(deps);
}

// handleGuidedToggle drives a webview {type:"guided-toggle", enable} click
// (spec §5, W-GUIDED): guards, folder selection, spawn, and an HONEST
// completion report — exit code AND a marker re-read, never a parse of
// output prose.
async function handleGuidedToggle(enable, deps) {
  if (!deps.workspaceFolders || deps.workspaceFolders.length === 0) {
    postGuidedResult({ ok: false, message: GUIDED_NO_WORKSPACE_MESSAGE });
    return;
  }
  if (isAuthoringMode(deps)) {
    postGuidedResult({ ok: false, message: GUIDED_AUTHORING_MESSAGE });
    return;
  }
  // guided and a skill-pack action share ONE mutating-operation lock per
  // panel (spec §6) — either already in flight rejects the other busy.
  if (guidedFlow || packFlow) {
    postGuidedResult({ ok: false, message: GUIDED_BUSY_MESSAGE });
    return;
  }
  // Fresh re-check at click time, never trusting the webview's last-rendered
  // lock (see isClaudeCodeRunnable's own comment above) — guided currently
  // requires claude-code specifically, not just any runnable agent; skill
  // packs no longer check this at all (spec §6 revision). Hiding the toggle
  // client-side is convenience only, never authority.
  if (!isClaudeCodeRunnable(deps)) {
    postGuidedResult({ ok: false, message: GUIDED_CLAUDE_CODE_REQUIRED_MESSAGE });
    return;
  }

  guidedFlow = { enable };

  // Everything past lock acquisition is wrapped in try/catch (Finding-3-class
  // robustness): handleMessage invokes this handler without awaiting it, so
  // ANY unexpected throw here — not just the two spots already guarded below
  // — must still release guidedFlow and report an error, never leave the
  // lock (and the webview's optimistic "running…" toggle) stuck forever.
  try {
    let selectedFolder;
    try {
      selectedFolder = await selectWorkspaceFolder(deps);
    } catch (err) {
      console.error("[zcp-welcome] guided folder selection failed:", err);
      finishGuidedToggle(deps, null);
      return;
    }
    if (!selectedFolder) {
      finishGuidedToggle(deps, null); // user cancelled the picker — no spawn
      return;
    }

    // Sticky for the panel's lifetime (spec §3 "selected workspace folder"
    // — Finding 4): collectFullState prefers this over deps.workspaceRoot's
    // fixed first-folder default, so a multi-root toggle against folder B
    // doesn't read back folder A's marker (or, now, folder A's pack
    // manifests) and snap the toggle back off. The panel may have been
    // disposed while the picker above was awaited; only a LIVE panel gets
    // its guided-marker/pack-manifests watchers re-pointed (a disposed
    // panel's disposables were already torn down by disposeWatchers).
    selectedWorkspaceRoot = selectedFolder;
    if (panel) {
      reattachGuidedMarkerWatcher(deps, selectedWorkspaceRoot);
      reattachPackManifestWatcher(deps, selectedWorkspaceRoot);
    }

    if (anyDirtyGuardedDoc(deps, selectedFolder)) {
      finishGuidedToggle(deps, { ok: false, message: GUIDED_DIRTY_MESSAGE });
      return;
    }

    if (guidedOutputChannel) {
      const argv = enable ? "zcp init --guided" : "zcp init";
      guidedOutputChannel.appendLine("$ " + argv + " (cwd=" + selectedFolder + ")");
    }

    let child;
    try {
      child = deps.spawn("zcp", enable ? ["init", "--guided"] : ["init"], { cwd: selectedFolder, shell: false });
    } catch (err) {
      finishGuidedToggle(deps, { ok: false, message: GUIDED_ENOENT_MESSAGE });
      return;
    }
    if (!child || typeof child.on !== "function") {
      finishGuidedToggle(deps, { ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE });
      return;
    }
    guidedFlow.child = child; // tag the lock with this run's child — see the staleness checks below

    const streamed = streamChildOutput(child, guidedOutputChannel);

    // Node's own docs don't guarantee "error" and "exit" are mutually
    // exclusive for every failure mode (unlike a plain ENOENT, verified
    // error-only on this runtime) — the guidedFlow.child identity check below
    // mirrors authFlow's eventId/kind checks elsewhere in this file: without
    // it, a second (late, spurious) event for the SAME child could finish a
    // NEWER run that reused the now-released lock. Each callback is ALSO its
    // own try/catch: it runs asynchronously, well outside this function's
    // own try above, and it is the ONLY remaining path back to releasing
    // guidedFlow for this run — an uncaught throw here would leak the lock
    // exactly as an unreleased dispose would (Finding 2's failure mode).
    child.on("error", (err) => {
      try {
        if (!guidedFlow || guidedFlow.child !== child) return;
        streamed.flush();
        if (guidedOutputChannel) guidedOutputChannel.appendLine("[zcp-welcome] zcp init failed to start: " + err);
        const message = err && err.code === "ENOENT" ? GUIDED_ENOENT_MESSAGE : GUIDED_PARTIAL_FAILURE_MESSAGE;
        finishGuidedToggle(deps, { ok: false, message });
      } catch (handlerErr) {
        console.error("[zcp-welcome] guided child 'error' handler failed unexpectedly:", handlerErr);
        finishGuidedToggle(deps, { ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE });
      }
    });

    child.on("exit", (code) => {
      try {
        if (!guidedFlow || guidedFlow.child !== child) return;
        streamed.flush();
        const markerEnabled = collectGuided(deps.fs, selectedFolder).state === "enabled";
        if (code === 0 && markerEnabled === enable) {
          finishGuidedToggle(deps, { ok: true, enabled: enable });
        } else if (code === 0) {
          finishGuidedToggle(deps, { ok: false, message: GUIDED_MARKER_MISMATCH_MESSAGE });
        } else {
          finishGuidedToggle(deps, { ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE });
        }
      } catch (handlerErr) {
        console.error("[zcp-welcome] guided child 'exit' handler failed unexpectedly:", handlerErr);
        finishGuidedToggle(deps, { ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE });
      }
    });
  } catch (err) {
    console.error("[zcp-welcome] guided-toggle failed unexpectedly:", err);
    finishGuidedToggle(deps, { ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE });
  }
}

// ---- skill-pack action (docs/spec-welcome-mode.md §6) --------------------

const PACK_NO_WORKSPACE_MESSAGE = "No workspace folder open — open a folder first.";
const PACK_UNTRUSTED_MESSAGE = "Workspace is not trusted.";

// packOpFailedMessage is the LAST-RESORT fallback for a completed action
// with no usable CLI-reported message (a non-zero exit with unparsable
// stdout, or any unexpected throw past lock acquisition) — worded for
// whichever direction (install/remove) was requested.
function packOpFailedMessage(enable) {
  return (enable ? "Installing" : "Removing") + " the skill pack failed — see the Zerops Welcome output.";
}

// postPackResult sends one {type:"pack-result"} outcome for a single
// {type:"pack-action"} click — the per-row result line renders from this
// (welcome.html owns the code->copy mapping; this file only relays what the
// CLI/host decided). message/code/warnings are present only when the CLI (or
// a pre-spawn host gate reusing the CLI's own "busy" code) actually supplied
// one — ok:true with no warnings carries neither, mirroring the existing
// "a success needs no extra copy, the toggle's own state already shows it"
// discipline.
function postPackResult(id, ok, message, code, warnings) {
  if (!panel) return;
  const msg = { type: "pack-result", id, ok };
  if (message) msg.message = message;
  if (code) msg.code = code;
  if (Array.isArray(warnings) && warnings.length > 0) msg.warnings = warnings;
  try {
    panel.webview.postMessage(msg);
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// finishPackAction releases the pack lock, reports the outcome, pushes fresh
// state, and — unlike the retired finishPackToggle — triggers a fresh
// pack-status run for the folder the action just ran against (spec §4: "a
// pack/guided operation" is one of the four pack-status refresh triggers):
// the CLI's own JSON response is this ONE row's honest outcome, but a
// pack-status re-run is what reconciles every row (e.g. a collision the CLI
// detected against a DIFFERENT pack's install).
function finishPackAction(deps, id, result) {
  packFlow = null;
  postPackResult(id, result.ok, result.message, result.code, result.warnings);
  postState(deps);
  runPackStatus(deps, selectedWorkspaceRoot || deps.workspaceRoot);
}

// handlePackAction drives a webview {type:"pack-action", id, action} click.
// id/action shape (exact PACK_IDS enum + "add"|"remove") is already validated
// by handleMessage's allowlist gate — every semantic guard below (workspace,
// fresh workspace trust, the shared one-mutating-op lock) lives here,
// mirroring handleGuidedToggle's own gate/handler split. UNLIKE guided, this
// gate no longer re-checks claude-code runnable at all (spec §6 revision:
// skill packs are inert workspace files — installing one needs no agent
// running). Folder selection reuses selectWorkspaceFolder — the SAME
// single-vs-quickpick seam guided uses: a multi-root workspace must target
// the folder the user actually picked, never silently folder zero.
//
// Spawns `zcp skills pack-add|pack-remove <id> --json` in the selected
// folder (fixed argv, no shell): streams ALL of its output to the SAME
// "Zerops Welcome" output channel guided uses (unabridged, for
// troubleshooting) AND separately captures a size-capped copy of stdout to
// parse the single JSON object the --json contract prints. Settles on the
// child's `close` event (streams fully drained — exit fires before stdout is
// guaranteed flushed) rather than `exit`, exactly once per run (a `settled`
// guard, mirroring the packFlow.child identity staleness check): success is
// exit 0 AND the parsed JSON's own ok:true — anything else is a failure,
// surfaced with the CLI's own code/message when parsing produced one, else
// packOpFailedMessage's fallback. This is a deliberately THINNER completion
// contract than handleGuidedToggle's own marker re-read: the CLI's JSON
// response is now the single honest source, never re-verified by a second
// state probe here (that discipline moved into the CLI itself).
//
// Everything past lock acquisition is wrapped in try/catch (same
// Finding-3-class robustness as handleGuidedToggle): handleMessage invokes
// this handler without awaiting it, so any unexpected throw here must still
// release packFlow and report an error, never leave the lock (and the
// webview's optimistic "installing…"/"removing…" toggle) stuck forever.
async function handlePackAction(id, action, deps) {
  const enable = action === "add";
  if (!deps.workspaceFolders || deps.workspaceFolders.length === 0) {
    postPackResult(id, false, PACK_NO_WORKSPACE_MESSAGE);
    return;
  }
  // Read FRESH at click time (deps.isTrusted is a function, never a snapshot
  // boolean) — a trust grant/revoke while the panel sits open must be seen
  // immediately (spec §6).
  if (deps.isTrusted() === false) {
    postPackResult(id, false, PACK_UNTRUSTED_MESSAGE);
    return;
  }
  // guided and a skill-pack action share ONE mutating-operation lock per
  // panel (spec §6) — either already in flight rejects the other busy. Coded
  // "busy" (not a bare message) so welcome.html's per-row copy table renders
  // the SAME fixed text this reuses from the CLI's own "busy" code (spec §6:
  // "another skill-pack/guided operation is running in this workspace" is
  // true regardless of which side detected it).
  if (guidedFlow || packFlow) {
    postPackResult(id, false, undefined, "busy");
    return;
  }

  packFlow = { id, action };

  try {
    let selectedFolder;
    try {
      selectedFolder = await selectWorkspaceFolder(deps);
    } catch (err) {
      console.error("[zcp-welcome] pack folder selection failed:", err);
      finishPackAction(deps, id, { ok: false });
      return;
    }
    if (!selectedFolder) {
      finishPackAction(deps, id, { ok: false }); // user cancelled the picker — no spawn
      return;
    }

    // Sticky for the panel's lifetime, shared with guided (Finding 4, spec
    // §3/§6 "selected workspace folder") — see selectedWorkspaceRoot's own
    // doc-comment. The panel may have been disposed while the picker above
    // was awaited; only a LIVE panel gets its watchers re-pointed (a
    // disposed panel's disposables were already torn down by
    // disposeWatchers).
    selectedWorkspaceRoot = selectedFolder;
    if (panel) {
      reattachGuidedMarkerWatcher(deps, selectedWorkspaceRoot);
      reattachPackManifestWatcher(deps, selectedWorkspaceRoot);
    }

    // The webview's action enum ("add"/"remove") is NOT the CLI's own
    // subcommand name — that's pack-add/pack-remove — so it's mapped here,
    // the ONE place this file ever names the CLI's actual verb.
    const cliSubcommand = enable ? "pack-add" : "pack-remove";

    if (guidedOutputChannel) {
      guidedOutputChannel.appendLine("$ zcp skills " + cliSubcommand + " " + id + " --json (cwd=" + selectedFolder + ")");
    }

    let child;
    try {
      child = deps.spawn("zcp", ["skills", cliSubcommand, id, "--json"], { cwd: selectedFolder, shell: false });
    } catch (err) {
      finishPackAction(deps, id, { ok: false, message: GUIDED_ENOENT_MESSAGE });
      return;
    }
    if (!child || typeof child.on !== "function") {
      finishPackAction(deps, id, { ok: false, message: packOpFailedMessage(enable) });
      return;
    }
    packFlow.child = child; // tag the lock with this run's child — see the staleness checks below

    const streamed = streamChildOutput(child, guidedOutputChannel);

    let stdoutCaptured = "";
    if (child.stdout && typeof child.stdout.on === "function") {
      child.stdout.on("data", (chunk) => {
        stdoutCaptured = (stdoutCaptured + chunk.toString()).slice(0, PACK_JSON_STDOUT_CAP_BYTES);
      });
    }

    // settle guards BOTH "error" and "close" firing for the same child
    // (Node's own docs don't guarantee they're mutually exclusive for every
    // failure mode) down to exactly one outcome, and mirrors
    // handleGuidedToggle's own packFlow.child identity staleness check: a
    // second, late event for a SUPERSEDED child must never finish a NEWER
    // run that reused the now-released lock.
    let settled = false;
    const settle = (result) => {
      if (settled) return;
      if (!packFlow || packFlow.child !== child) return;
      settled = true;
      streamed.flush();
      finishPackAction(deps, id, result);
    };

    // Each callback is its own try/catch: it runs asynchronously, well
    // outside this function's own try above, and it is the ONLY remaining
    // path back to releasing packFlow for this run — an uncaught throw here
    // would leak the lock exactly as an unreleased dispose would.
    child.on("error", (err) => {
      try {
        if (guidedOutputChannel) guidedOutputChannel.appendLine("[zcp-welcome] zcp skills failed to start: " + err);
        const message = err && err.code === "ENOENT" ? GUIDED_ENOENT_MESSAGE : packOpFailedMessage(enable);
        settle({ ok: false, message });
      } catch (handlerErr) {
        console.error("[zcp-welcome] pack-action child 'error' handler failed unexpectedly:", handlerErr);
        settle({ ok: false, message: packOpFailedMessage(enable) });
      }
    });

    // close (streams fully drained), not exit — exit can fire before stdout
    // is guaranteed flushed, and stdoutCaptured must be complete before
    // parsing it (spec §6).
    child.on("close", (code) => {
      try {
        const parsed = parsePackJSON(stdoutCaptured);
        const warnings = parsed && Array.isArray(parsed.warnings) ? parsed.warnings : undefined;
        if (code === 0 && parsed && parsed.ok === true) {
          settle({ ok: true, warnings });
        } else {
          const message = parsed && typeof parsed.message === "string" ? parsed.message : undefined;
          const failCode = parsed && typeof parsed.code === "string" ? parsed.code : undefined;
          settle({ ok: false, message: message || packOpFailedMessage(enable), code: failCode, warnings });
        }
      } catch (handlerErr) {
        console.error("[zcp-welcome] pack-action child 'close' handler failed unexpectedly:", handlerErr);
        settle({ ok: false, message: packOpFailedMessage(enable) });
      }
    });
  } catch (err) {
    console.error("[zcp-welcome] pack-action failed unexpectedly:", err);
    finishPackAction(deps, id, { ok: false, message: packOpFailedMessage(enable) });
  }
}

// ---- CTA (docs/spec-welcome-mode.md §7, W-CTA) ---------------------------

// The two kickoff prompts, final copy (spec §7): handed to the agent via
// the clipboard, never typed into the DOM/logs, never altered per-agent.
const CTA_PROMPTS = {
  new: "I want to build something new on Zerops. Ask me what I'm building, then plan the smallest working version and get it running on this project's dev runtime.",
  existing: "I have an existing app in this workspace that I want to run on Zerops. Inspect the repo, tell me your integration plan, then wire it up and get it running on the dev runtime.",
};

const CTA_NOT_AUTHORIZED_MESSAGE = "Authorize an agent first.";
const CTA_SELECT_AGENT_MESSAGE = "Select which authorized agent should start.";
const CTA_CLIPBOARD_FAILED_MESSAGE = "Agent opened, but the kickoff prompt could not be copied to the clipboard.";

function ctaKickoffMessage(label) {
  return "Kickoff prompt copied — paste it into " + label + " to start.";
}

// postCTAResult sends a {type:"cta-result"} outcome for a {type:"start-
// onboarding"} click (spec §7 W-CTA) — success or failure, the panel is
// NEVER disposed here: the user may come back to it.
function postCTAResult(ok, message) {
  if (!panel) return;
  try {
    panel.webview.postMessage({ type: "cta-result", ok, message });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// handleStartOnboarding drives a webview {type:"start-onboarding", path,
// agentId} click (spec §7 W-CTA). Re-validates against a FRESH state read —
// never trusts the webview's own idea of who is authorized, and never falls
// back to "first in registry" when the target agent can't be resolved: with
// zero RUNNABLE agents (installed AND authorized/authorized-token — an
// authorized flag for a binary that isn't on this container's PATH must not
// unlock the launch surface, spec §7) it's a plain rejection; with the
// given agentId (missing, unknown, or naming an agent that ISN'T currently
// runnable) failing to resolve to exactly one CURRENTLY runnable agent,
// it's an explicit "select an agent" rejection instead of a silent guess.
// Launch is entirely the injected runAgentAction's call (HOW — plugin
// command vs panel terminal — welcome adds no launch flags); the kickoff
// prompt is clipboard-first — NEVER terminal.sendText, NEVER a delayed
// setTimeout injection, since a terminal may not even be running the agent.
async function handleStartOnboarding(path, agentId, deps) {
  const prompt = CTA_PROMPTS[path];
  const state = collectFullState(deps);
  const runnable = state.agents.filter((a) => a.installed && (a.state === "authorized" || a.state === "authorized-token"));

  if (runnable.length === 0) {
    postCTAResult(false, CTA_NOT_AUTHORIZED_MESSAGE);
    return;
  }

  let resolved = typeof agentId === "string" ? runnable.find((a) => a.id === agentId) : undefined;
  if (!resolved && agentId === undefined && runnable.length === 1) resolved = runnable[0]; // the one-runnable-agent implicit case
  if (!resolved) {
    postCTAResult(false, CTA_SELECT_AGENT_MESSAGE);
    return;
  }

  const agentEntry = deps.REGISTRY[resolved.id];
  if (!agentEntry || !Array.isArray(agentEntry.opens) || !agentEntry.opens[0]) {
    // Defensive only: state.agents is built FROM deps.REGISTRY (buildState),
    // so a resolved agent id missing its own registry entry should never
    // happen in practice.
    console.error("[zcp-welcome] start-onboarding: no launch mode registered for " + resolved.id);
    postCTAResult(false, CTA_SELECT_AGENT_MESSAGE);
    return;
  }

  deps.runAgentAction(agentEntry, agentEntry.opens[0].mode);

  try {
    await deps.clipboard.writeText(prompt);
  } catch (err) {
    console.error("[zcp-welcome] clipboard.writeText failed:", err);
    postCTAResult(false, CTA_CLIPBOARD_FAILED_MESSAGE);
    return;
  }

  const message = ctaKickoffMessage(agentEntry.label || resolved.id);
  try {
    deps.showInformationMessage(message);
  } catch (err) {
    console.error("[zcp-welcome] showInformationMessage failed:", err);
  }
  postCTAResult(true, message);
}

// ---- open-agent (per-row launch, docs/spec-welcome-mode.md §7) ----------

// handleOpenAgent drives a webview {type:"open-agent", agentId} click: the
// redesigned UI's per-row "Open" button, launching exactly one agent with no
// clipboard write and no kickoff prompt — contrast handleStartOnboarding
// above, the CTA's onboarding-with-a-prompt path. Re-validates against a
// FRESH state read, same discipline as the CTA: the agent must be installed
// AND authorized/authorized-token (runnable) right now, never the webview's
// own idea of it. Same launch seam as the CTA (deps.runAgentAction) — HOW is
// entirely its call.
function handleOpenAgent(agentId, deps) {
  const state = collectFullState(deps);
  const agent = state.agents.find((a) => a.id === agentId);
  const runnable = agent && agent.installed && (agent.state === "authorized" || agent.state === "authorized-token");
  if (!runnable) {
    postAuth(agentId, "unsupported");
    return;
  }
  const reg = deps.REGISTRY[agentId];
  if (!reg || !Array.isArray(reg.opens) || !reg.opens[0]) {
    // Defensive only: state.agents is built FROM deps.REGISTRY (buildState),
    // so a resolved agent id missing its own registry entry should never
    // happen in practice.
    console.error("[zcp-welcome] open-agent: no launch mode registered for " + agentId);
    postAuth(agentId, "unsupported");
    return;
  }
  deps.runAgentAction(reg, reg.opens[0].mode);
}

// ---- onboard (per-row kickoff, docs/spec-welcome-mode.md §7) -------------

// The onboard kickoff prompt — delivered SUBMITTED (contrast the CTA's
// clipboard paste): for the Claude plugin via the process-wrapper marker
// (armKickoffMarker below), for a terminal agent through its live-verified
// initial-prompt CLI shape.
const ONBOARD_PROMPT = "Onboard me to Zerops.";

// POSIX single-quote for a terminal agent's initial-prompt argv (CLAUDE.md:
// shellQuote, never fmt-compose a shell string).
function shellQuoteArg(s) {
  return "'" + String(s).replace(/'/g, "'\\''") + "'";
}

// The process wrapper (installed as claudeCode.claudeProcessWrapper by
// `zcp init`) consumes this marker exactly once, injecting the prompt as a
// real SUBMITTED user turn the moment the plugin's next CLI session goes live.
// HOME-based to match the wrapper's own read path; deps.fs/homeDir are the
// test seams (never the real filesystem in tests).
function kickoffMarkerPath(deps) {
  return path.join(deps.homeDir, ".zcp", "state", "claude-kickoff.json");
}

function armKickoffMarker(prompt, deps) {
  try {
    const marker = kickoffMarkerPath(deps);
    deps.fs.mkdirSync(path.dirname(marker), { recursive: true });
    deps.fs.writeFileSync(marker, JSON.stringify({ prompt, armedAt: Date.now() }), "utf8");
    return true;
  } catch (err) {
    console.error("[zcp-welcome] onboard: could not arm kickoff marker:", err);
    return false;
  }
}

function seedOpenWithPrompt(open, prompt) {
  if (open.mode === "extension") {
    // The Claude plugin's editor.open only PREFILLS its composer — it never
    // submits. The submitted turn is delivered out-of-band by the process
    // wrapper (armKickoffMarker in handleOnboard), so a plugin open carries no
    // prompt arg: it just starts a FRESH conversation for the wrapper to
    // inject into.
    return open;
  }
  const promptFlag = typeof open.initialPromptFlag === "string" && open.initialPromptFlag
    ? " " + open.initialPromptFlag
    : "";
  return Object.assign({}, open, { command: open.command + promptFlag + " " + shellQuoteArg(prompt) });
}

// handleOnboard drives a webview {type:"onboard", agentId} click: launch the
// runnable agent AND hand it the onboarding prompt already submitted. Same
// FRESH runnable re-validation as handleOpenAgent. Delivery is per launch mode:
//   extension (Claude plugin) -> arm the wrapper marker, then open a FRESH
//     panel (editor.open alone only prefills; the wrapper submits the turn).
//   terminal -> append the prompt in the CLI's verified initial-prompt shape.
// A CLONED reg keeps the shared registry commands immutable. Every open mode
// is seeded so an unavailable Claude plugin's terminal fallback keeps the
// same onboarding promise (and the marker is inert for that interactive CLI).
function handleOnboard(agentId, deps) {
  const state = collectFullState(deps);
  const agent = state.agents.find((a) => a.id === agentId);
  const runnable = agent && agent.installed && (agent.state === "authorized" || agent.state === "authorized-token");
  if (!runnable) {
    postAuth(agentId, "unsupported");
    return;
  }
  const reg = deps.REGISTRY[agentId];
  if (!reg || !Array.isArray(reg.opens) || !reg.opens[0]) {
    console.error("[zcp-welcome] onboard: no launch mode registered for " + agentId);
    postAuth(agentId, "unsupported");
    return;
  }

  // Instant "it's starting" feedback, fired BEFORE the launch: the agent panel
  // opens but its CLI boots ~2s before the first reply, and nothing can render
  // inside that panel until its webview subscribes. Two independent signals so
  // the click never reads as dead: the clicked row's progress line, and a
  // corner toast (visible even though focus moves to the agent panel).
  postAuth(agentId, "onboarding");
  try {
    deps.showInformationMessage("Onboarding " + (reg.label || agentId) + " — reading your project, first reply in a moment…");
  } catch (err) {
    console.error("[zcp-welcome] onboard: showInformationMessage failed:", err);
  }

  // Single-flight guard: a rapid second click would arm the one-shot marker
  // AGAIN and spawn a COMPETING fresh session, so the panel the user is looking
  // at can be an uninjected later session — the "click a few times and it
  // randomly starts" symptom. Ignore onboards fired within a short window of
  // the last (module-level, so it also covers a reopened welcome).
  const now = Date.now();
  if (now < onboardInFlightUntil) return;
  onboardInFlightUntil = now + 8000;

  const primary = reg.opens[0];
  // Claude plugin: arm the wrapper so the prompt is actually SUBMITTED
  // (editor.open alone only prefills). Terminal agents carry the prompt in
  // argv via seedOpenWithPrompt below, so they never touch the marker.
  if (primary.mode === "extension") {
    armKickoffMarker(ONBOARD_PROMPT, deps);
  }
  // Open the agent in the welcome's OWN editor column so it takes the full
  // width WITHOUT disposing the welcome. Disposing is the source of the
  // inconsistent onboard: after the panel mounts (a deferred timer) it churns
  // the editor group/focus while the webview is still subscribing and the
  // injected turn is dropped; before launch it stops the fresh session from
  // spawning at all (an unfocused panel in an empty area never subscribes).
  // The welcome is retained behind the agent panel.
  const welcomeColumn = panel && panel.viewColumn ? panel.viewColumn : undefined;
  const launchReg = Object.assign({}, reg, {
    opens: reg.opens.map((open) => {
      if (open.mode === "extension" && welcomeColumn !== undefined) {
        // editor.open(sessionId, initialPrompt, viewColumn): a FRESH session
        // (undefined id), no prompt (the wrapper submits it), in the welcome's
        // column — active, so it spawns and subscribes reliably.
        return Object.assign({}, open, { args: [undefined, undefined, welcomeColumn] });
      }
      return seedOpenWithPrompt(open, ONBOARD_PROMPT);
    }),
  });
  deps.runAgentAction(launchReg, primary.mode);
}

// handleMessage is the strict allowlist gate (§8 W-SEC): exactly the shapes
// below do anything; everything else — including a well-formed message of
// an unknown type, or a message whose fields fail their check — is
// silently dropped (counted to console for debugging only), never thrown,
// never surfaced to the user.
function handleMessage(msg, deps) {
  if (!msg || typeof msg.type !== "string") return;
  switch (msg.type) {
    case "welcome-suppress":
      // Runtime GUI-context gate (welcome.html): the production app.zerops.io
      // dashboard is an ancestor frame, so close the optimistically-opened
      // welcome — that dashboard drives its own onboarding. No payload.
      if (panel) { try { panel.dispose(); } catch (_) {} }
      return;
    case "ready":
      // embedded (window.top !== window, spec §4 diagnostics) is optional
      // and boolean-only — a bad type (missing, or a non-boolean sent by a
      // stale/tampered webview) is treated as absent: it never overwrites
      // whatever lastEmbedded already holds, and never blocks the state push
      // below.
      if (typeof msg.embedded === "boolean") lastEmbedded = msg.embedded;
      postState(deps);
      // Panel-ready is one of the four pack-status refresh triggers (spec
      // §4) — every row renders "checking" off collectPacksState's own
      // no-cache-yet default until this lands.
      runPackStatus(deps, selectedWorkspaceRoot || deps.workspaceRoot);
      return;
    case "open-url":
      if (typeof msg.url === "string" && EXTERNAL_URLS.has(msg.url)) {
        vscode.env.openExternal(vscode.Uri.parse(msg.url));
      } else {
        console.log("[zcp-welcome] dropped open-url: not on the allowlist");
      }
      return;
    case "authorize":
      if (typeof msg.agentId === "string" && deps.ALL_AGENT_IDS.includes(msg.agentId)) {
        handleAuthorize(msg.agentId, deps);
      } else {
        console.log("[zcp-welcome] dropped authorize: bad agentId");
      }
      return;
    case "bridge-window-message":
      if (isWellFormedBridgeRelay(msg)) {
        handleBridgeWindowMessage(msg, deps);
      } else {
        console.log("[zcp-welcome] dropped bridge-window-message: malformed");
      }
      return;
    case "guided-toggle":
      if (typeof msg.enable === "boolean") {
        // Both handlers are already self-contained (their own try/catch
        // resolves any unexpected throw to an explicit result — Finding 3),
        // but handleMessage never awaits them either, so this .catch() is a
        // second, independent line of defense: nothing that changes inside
        // the handler later can turn into an unhandled promise rejection.
        handleGuidedToggle(msg.enable, deps).catch((err) => {
          console.error("[zcp-welcome] unhandled guided-toggle error:", err);
        });
      } else {
        console.log("[zcp-welcome] dropped guided-toggle: bad enable");
      }
      return;
    case "pack-action":
      if (typeof msg.id === "string" && PACK_IDS.has(msg.id) && (msg.action === "add" || msg.action === "remove")) {
        handlePackAction(msg.id, msg.action, deps).catch((err) => {
          console.error("[zcp-welcome] unhandled pack-action error:", err);
        });
      } else {
        console.log("[zcp-welcome] dropped pack-action: bad id/action");
      }
      return;
    case "pack-details":
      // No further payload validation needed (spec §6): reveals the
      // existing "Zerops Welcome" output channel so the user can see a
      // failed/warned pack operation's full output — guarded for null
      // exactly like every other guidedOutputChannel access in this file (a
      // fresh panel before its first guided/pack run has none yet).
      if (guidedOutputChannel) guidedOutputChannel.show(true);
      return;
    case "start-onboarding": {
      const pathOk = msg.path === "new" || msg.path === "existing";
      const agentIdOk = msg.agentId === undefined || (typeof msg.agentId === "string" && deps.ALL_AGENT_IDS.includes(msg.agentId));
      if (pathOk && agentIdOk) {
        handleStartOnboarding(msg.path, msg.agentId, deps);
      } else {
        console.log("[zcp-welcome] dropped start-onboarding: bad path/agentId");
      }
      return;
    }
    case "open-agent":
      if (typeof msg.agentId === "string" && deps.ALL_AGENT_IDS.includes(msg.agentId)) {
        handleOpenAgent(msg.agentId, deps);
      } else {
        console.log("[zcp-welcome] dropped open-agent: bad agentId");
      }
      return;
    case "onboard":
      if (typeof msg.agentId === "string" && deps.ALL_AGENT_IDS.includes(msg.agentId)) {
        handleOnboard(msg.agentId, deps);
      } else {
        console.log("[zcp-welcome] dropped onboard: bad agentId");
      }
      return;
    default:
      console.log("[zcp-welcome] dropped unknown message type: " + msg.type);
      return;
  }
}

// disposeWatchers tears down everything panel-scoped on close: the
// watchers, and — since the ACK/cap timers and the terminal-close listener
// created by an in-flight auth flow are exactly as panel-scoped — that flow
// too (silently: there is no UI left to post an idle transition to).
//
// guidedFlow is DELIBERATELY NOT cleared here (Finding 2, spec §5 "one
// toggle in flight per window"): the spawned `zcp init` keeps running
// regardless of the panel, so dropping the lock here would let a close +
// reopen + toggle spawn a SECOND concurrent `zcp init` against the same
// files. The lock is released ONLY by that child's own exit/error handler
// (finishGuidedToggle, registered in handleGuidedToggle). By the time it
// fires, the panel may be gone, or a DIFFERENT panel may be current —
// postGuidedResult/postState both already guard `if (!panel) return`, so
// that eventual completion is safe either way, landing on whichever panel
// is current if any (guidedOutputChannel outlives the panel too, spec §5).
function disposeWatchers(deps) {
  if (pushTimer) { clearTimeout(pushTimer); pushTimer = null; }
  if (packStatusTimer) { clearTimeout(packStatusTimer); packStatusTimer = null; }
  for (const d of disposables) {
    try { d.dispose(); } catch (_) {}
  }
  disposables = [];
  // Force the next open()'s startWatchers to re-arm the guided-marker AND
  // pack-manifests watchers unconditionally: the ones just disposed above
  // (if any) are gone, but *WatcherRoot would otherwise still name their
  // folder, making reattach*Watcher wrongly think a matching root is
  // already watched and skip re-attaching on reopen.
  guidedMarkerWatcher = null;
  guidedMarkerWatcherRoot = undefined;
  packManifestsWatcher = null;
  packManifestsWatcherRoot = undefined;
  releaseAuthFlow(deps);
}

function schedulePush(deps) {
  if (pushTimer) clearTimeout(pushTimer);
  pushTimer = setTimeout(() => { pushTimer = null; postState(deps); }, STATE_PUSH_DEBOUNCE_MS);
  if (typeof pushTimer.unref === "function") pushTimer.unref(); // see unrefWatcher() above
}

// packStatusTimer is schedulePackStatusRefresh's own debounce handle — kept
// separate from pushTimer above: a manifest write only ever needs to refresh
// packs, never the unrelated agent/guided state schedulePush recomputes off
// its own watchers.
let packStatusTimer = null;
const PACK_STATUS_DEBOUNCE_MS = 300;

// schedulePackStatusRefresh debounces the pack-manifests watcher's own churn
// (a single `zcp skills pack-add` run can touch several files under
// .zcp/state/skill-packs, each its own fs event) into ONE
// `zcp skills pack-status --json` run ~300ms after the last event (spec §4:
// "debounced off the existing pack-manifests watcher events").
function schedulePackStatusRefresh(deps, root) {
  if (packStatusTimer) clearTimeout(packStatusTimer);
  packStatusTimer = setTimeout(() => { packStatusTimer = null; runPackStatus(deps, root); }, PACK_STATUS_DEBOUNCE_MS);
  if (typeof packStatusTimer.unref === "function") packStatusTimer.unref();
}

// ---- watchers (docs/spec-welcome-mode.md §3) -------------------------

// unrefWatcher marks a watch handle as NOT keeping its process alive by
// itself (real fs.FSWatcher supports this; test stubs generally don't, so
// the check is required, not defensive noise). The extension host process
// is kept alive by VS Code itself regardless, so this only matters for
// letting a process exit cleanly when nothing else is pending — e.g. a
// welcomejs test's `node --test` run, which would otherwise hang on a real,
// never-closed watcher (a fresh HOME can genuinely already have ~/.claude).
function unrefWatcher(w) {
  if (w && typeof w.unref === "function") w.unref();
}

// attachWatcherErrorHandler is the ONE place every fs.watch() call below
// registers its 'error' listener (spec §3). An async watcher error (EMFILE
// "too many open files", ENOSPC) is delivered on Node's 'error' event, never
// as a thrown exception from the watch() call itself — an FSWatcher with NO
// 'error' listener makes Node's default EventEmitter behavior throw it
// straight into the extension host on the next tick. Every occurrence here
// must degrade quietly instead: log + close, never escape uncaught. Guarded
// by typeof w.on: a real FSWatcher always has it, a minimal test stub's
// watch() return value may not (existing simpler test doubles keep working
// unchanged).
function attachWatcherErrorHandler(w, label) {
  if (!w || typeof w.on !== "function") return;
  w.on("error", (err) => {
    console.warn("[zcp-welcome] fs.watch(" + label + ") error, closing:", err);
    try { w.close(); } catch (_) {}
  });
}

// Zerops rewrites env.json IN PLACE (stable inode) on every env change, so
// watching the FILE (not its directory) — the same pattern as extension.js's
// launcher watcher — means we wake only on real env changes.
function watchZembedEnv(deps) {
  try {
    const w = deps.fs.watch(ZEMBED_ENV_FILE, () => schedulePush(deps));
    unrefWatcher(w);
    attachWatcherErrorHandler(w, "zembed");
    return { dispose() { try { w.close(); } catch (_) {} } };
  } catch (err) {
    console.warn("[zcp-welcome] fs.watch(zembed) unavailable:", err);
    return null;
  }
}

// watchWithFallback watches a DIR that may not exist yet by falling back to
// its nearest existing ancestor (fallbackDir, always assumed to exist — HOME
// for the credential dirs below, a workspace root for the pack-manifests
// watcher, further below) until targetRelPath (relative to fallbackDir, one
// or more path segments) appears, then swaps to watching it directly. fs.watch
// on a missing path throws immediately, so this is the ONE mechanism every
// "might not exist yet" watcher in this file uses — originally written for
// the credential dirs (a login creates the dir + writes the artifact,
// however the CLI does it) and reused verbatim for the pack-manifests dir
// (an external `zcp skills pack-add` run, never through this panel, creates
// .zcp/.zcp/state/.zcp/state/skill-packs from nothing on a brand-new
// workspace). Every event on either watcher — rename or change, whatever the
// platform reports — just re-triggers a full recompute; there is no cheaper
// reliable way to notice an atomic-replace write landing (spec §3: "survive
// atomic rename writes").
//
// `generation` guards the fallback->target swap: fs.watch callbacks are
// delivered asynchronously and can queue up, so a SECOND (stale) fallback
// event — already in flight when the first one closed the fallback watcher
// and attached the target watcher — must not re-fire the swap (closing the
// freshly attached target watcher out from under itself, then re-attaching
// and double-firing onEvent). Every (re)attach mints a new generation and
// captures it in its own callback's closure; only a callback whose captured
// generation still matches the current one is live.
function watchWithFallback(fsImpl, fallbackDir, targetRelPath, onEvent) {
  const target = path.join(fallbackDir, targetRelPath);
  let watcher = null;
  let generation = 0;

  function attachTarget() {
    const myGen = ++generation;
    try {
      const w = fsImpl.watch(target, () => {
        if (myGen !== generation) return; // superseded — see dispose()/attachFallback() below
        onEvent();
      });
      unrefWatcher(w);
      attachWatcherErrorHandler(w, targetRelPath);
      watcher = w;
    } catch (_) {
      watcher = null;
    }
  }

  function attachFallback() {
    const myGen = ++generation;
    try {
      const w = fsImpl.watch(fallbackDir, () => {
        if (myGen !== generation) return; // this fallback watcher has already been superseded
        let exists = false;
        try { exists = fsImpl.existsSync(target); } catch (_) { exists = false; }
        if (!exists) return;
        if (watcher) { try { watcher.close(); } catch (_) {} }
        attachTarget();
        onEvent();
      });
      unrefWatcher(w);
      attachWatcherErrorHandler(w, targetRelPath + " (fallback)");
      watcher = w;
    } catch (_) {
      watcher = null;
    }
  }

  let targetExists = false;
  try { targetExists = fsImpl.existsSync(target); } catch (_) { targetExists = false; }
  if (targetExists) attachTarget(); else attachFallback();

  return {
    dispose() {
      generation++; // invalidate any callback still queued for the current watcher
      if (watcher) { try { watcher.close(); } catch (_) {} watcher = null; }
    },
  };
}

// watchGuidedMarker follows the .zcp/state -> .zcp -> none fallback chain
// (spec §3). The marker's write is a single fast `zcp init --guided` run
// rather than a long-lived interactive login, so even the coarser .zcp-level
// fallback is enough: the recompute it triggers re-checks the marker fresh,
// by which time the run has finished. No folder open, or neither directory
// exists yet, means no watcher — the caller relies on the reveal/focus
// recompute instead (no polling loop).
function watchGuidedMarker(fsImpl, workspaceRoot, onEvent) {
  if (!workspaceRoot) return null;
  const stateDir = path.join(workspaceRoot, ".zcp", "state");
  const zcpDir = path.join(workspaceRoot, ".zcp");
  let target = null;
  try {
    if (fsImpl.existsSync(stateDir)) target = stateDir;
    else if (fsImpl.existsSync(zcpDir)) target = zcpDir;
  } catch (_) { target = null; }
  if (!target) return null;
  try {
    const w = fsImpl.watch(target, () => onEvent());
    unrefWatcher(w);
    attachWatcherErrorHandler(w, "guided-marker");
    return { dispose() { try { w.close(); } catch (_) {} } };
  } catch (_) {
    return null;
  }
}

// watchPackManifests reuses watchWithFallback (the SAME fallback-then-swap
// discipline the credential dirs use) rather than watchGuidedMarker's
// coarser one-shot chain: unlike the guided marker (always written by a
// `zcp init` run this panel itself just spawned), a pack manifest can be
// written by an EXTERNAL `zcp skills pack-add` run in a terminal the panel
// never sees, and on a brand-new workspace NONE of .zcp/.zcp/state/.zcp/
// state/skill-packs exist yet — watching the workspace root (guaranteed to
// exist for an open folder) as the fallback, and letting the swap cascade
// down as each intermediate directory is created, catches that from a
// completely cold start. No folder open means no watcher, same as
// watchGuidedMarker.
function watchPackManifests(fsImpl, workspaceRoot, onEvent) {
  if (!workspaceRoot) return null;
  return watchWithFallback(fsImpl, workspaceRoot, PACK_MANIFESTS_DIR_REL, onEvent);
}

// reattachGuidedMarkerWatcher (re)points the guided-marker watcher at `root`
// — called once at startWatchers() time (root = the panel's default
// selected folder, before any toggle has run) and again whenever a guided OR
// pack toggle resolves a DIFFERENT folder (Finding 4, spec §3): the panel
// must reflect live changes to the folder the user actually operated on. A
// no-op when `root` already matches what's watched, so repeat toggles
// against the same folder don't churn the watcher.
function reattachGuidedMarkerWatcher(deps, root) {
  if (guidedMarkerWatcherRoot === root) return;
  if (guidedMarkerWatcher) {
    const idx = disposables.indexOf(guidedMarkerWatcher);
    if (idx >= 0) disposables.splice(idx, 1);
    try { guidedMarkerWatcher.dispose(); } catch (_) {}
    guidedMarkerWatcher = null;
  }
  guidedMarkerWatcherRoot = root;
  const w = watchGuidedMarker(deps.fs, root, () => schedulePush(deps));
  if (w) {
    guidedMarkerWatcher = w;
    disposables.push(w);
  }
}

// reattachPackManifestWatcher is reattachGuidedMarkerWatcher's structural
// sibling for the pack-manifests directory — same call sites (startWatchers,
// and whenever either a guided or pack action resolves a folder), same
// no-op-when-unchanged guard, same disposables bookkeeping. UNLIKE
// reattachGuidedMarkerWatcher, its watcher no longer feeds the general
// schedulePush debounce: a manifest write only ever needs to refresh packs
// (spec §4), so it feeds schedulePackStatusRefresh's own dedicated ~300ms
// debounce instead.
function reattachPackManifestWatcher(deps, root) {
  if (packManifestsWatcherRoot === root) return;
  if (packManifestsWatcher) {
    const idx = disposables.indexOf(packManifestsWatcher);
    if (idx >= 0) disposables.splice(idx, 1);
    try { packManifestsWatcher.dispose(); } catch (_) {}
    packManifestsWatcher = null;
  }
  packManifestsWatcherRoot = root;
  const w = watchPackManifests(deps.fs, root, () => schedulePackStatusRefresh(deps, root));
  if (w) {
    packManifestsWatcher = w;
    disposables.push(w);
  }
}

// startWatchers runs ONCE per panel (only from open()'s creation branch,
// never on reveal), so re-invoking the command never accumulates watchers
// (spec §1, W-ENTRY).
function startWatchers(deps) {
  const zembed = watchZembedEnv(deps);
  if (zembed) disposables.push(zembed);

  for (const dirName of Object.values(CRED_WATCH_DIR)) {
    const w = watchWithFallback(deps.fs, deps.homeDir, dirName, () => schedulePush(deps));
    if (w) disposables.push(w);
  }

  const root = selectedWorkspaceRoot || deps.workspaceRoot;
  reattachGuidedMarkerWatcher(deps, root);
  reattachPackManifestWatcher(deps, root);
}

// open shows the singleton welcome panel: creates it (and starts its
// watchers) on the first call, reveals + re-reads state (never
// disposes/recreates) on every call after — see docs/spec-welcome-mode.md
// §1. No serializer is registered: after a window reload the panel is gone
// until the user re-invokes the command.
function open(ctx, deps) {
  const resolved = resolveDeps(deps);
  resolved.extensionPath = ctx.extensionPath; // readExtensionVersion reads this install's own package.json from here
  if (panel) {
    panel.reveal();
    postState(resolved); // re-invoking the command re-reads state (missed watcher events must not leave stale UI)
    // Reveal is one of the four pack-status refresh triggers (spec §4).
    runPackStatus(resolved, selectedWorkspaceRoot || resolved.workspaceRoot);
    return;
  }
  if (!guidedOutputChannel) {
    guidedOutputChannel = vscode.window.createOutputChannel("Zerops Welcome");
    ctx.subscriptions.push(guidedOutputChannel);
  }
  const nonce = crypto.randomBytes(16).toString("base64url");
  const newPanel = vscode.window.createWebviewPanel(
    "zeropsWelcome",
    "Get Started with Zerops",
    vscode.ViewColumn.One,
    { enableScripts: true, retainContextWhenHidden: true }
  );
  newPanel.webview.html = readWelcomeHtml(ctx, nonce);
  newPanel.webview.onDidReceiveMessage((msg) => handleMessage(msg, resolved));
  newPanel.onDidDispose(() => {
    if (panel === newPanel) panel = null;
    disposeWatchers(resolved);
  });
  // Switching back to this panel's tab (no command re-invocation) must also
  // re-read state — the panel may have been hidden through an entire login
  // flow or guided/pack run. Focus is one of the four pack-status refresh
  // triggers (spec §4).
  disposables.push(newPanel.onDidChangeViewState((e) => {
    const visible = e && e.webviewPanel ? e.webviewPanel.visible : newPanel.visible;
    if (visible) {
      postState(resolved);
      runPackStatus(resolved, selectedWorkspaceRoot || resolved.workspaceRoot);
    }
  }));
  startWatchers(resolved);
  panel = newPanel;
}

module.exports = { open, computeAgentState, buildState, PACKS, isAllowedGuiOrigin };
