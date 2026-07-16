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
// never used to build a URL, never pattern-matched (§8 W-SEC).
const EXTERNAL_URLS = new Set([
  "https://docs.zerops.io",
  "https://zerops.io", // TODO(welcome): real 5-min walkthrough URL
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

// Build-time origin allowlist for the credential-free auth-bridge trigger
// (spec §4): the target of the webview's window.top.postMessage, and the
// ONLY origins a relayed inbound message is trusted from. Never derived
// from the workspace, env, or an inbound message — staging/dev origins are
// added here deliberately, by a person editing this file.
const ORIGIN_ALLOWLIST = ["https://app.zerops.io"];

// Bridge support matrix v1 (spec §4): only claude-code has a receiver on
// the other end. Every other agent's authorize click is rejected with
// phase "unsupported" — its tile keeps the "authorize in the Zerops panel"
// hint instead (welcome.html).
const BRIDGE_SUPPORTED_AGENTS = new Set(["claude-code"]);

// Tier-A terminal fallback v1 (spec §4): login commands taken VERBATIM from
// the frontend registry. An agent absent here has no terminal path — its
// authorize-terminal click is rejected with phase "unsupported".
const LOGIN_COMMANDS = {
  "claude-code": "claude /login",
  "codex": "codex login --device-auth",
};

const ACK_TIMEOUT_MS = 3000; // spec §4: how long we wait for the GUI's open-agent-auth-ack
const AUTH_FLOW_CAP_MS = 10 * 60 * 1000; // spec §4: releases a stuck bridge or terminal flow after 10 minutes

// Defense-in-depth size cap on a relayed bridge message's data (spec §8
// W-SEC "size-capped") — generous for {channel,version,type,eventId,
// accepted,reason} but rejects a pathological payload before it is ever
// interpreted. Duplicated in welcome.html for the webview's own pre-filter;
// this copy is the one that actually matters (§8: "Host validates relayed
// messages again").
const BRIDGE_RELAY_MAX_BYTES = 1024;

let panel = null; // singleton — re-invoking open() reveals this, never recreates it
let disposables = []; // welcome-panel-scoped disposables (watchers, view-state listener): cleared on dispose
let pushTimer = null; // shared debounce timer for schedulePush()

// At most one authorization flow in flight per panel (spec §4), bridge OR
// terminal — module-level like `panel` above, safe for the same reason
// (welcomejs/harness.js gives every test its own uncached module instance).
// Shape: { kind: "bridge", agentId, eventId, ackTimer, capTimer } |
//        { kind: "terminal", agentId, terminal, capTimer, closeDisposable }
let authFlow = null;

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

// buildState assembles the full webview payload from already-collected
// inputs — pure, no I/O of its own (collectFullState below does the
// reading). anyAuthorized gates steps 2+ (§7 W-CTA): only "authorized" and
// "authorized-token" unlock it — "local-only" is platform-unverified and
// must NOT unlock.
function buildState(inputs) {
  const { registry = {}, agentIds = [], zembedEnv: env = null, creds = {}, guided = { state: "unknown" } } = inputs || {};

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
    return { id, label: reg.label || id, state, probeVerified: cred.verifiable };
  });

  return {
    agents,
    anyAuthorized: agents.some((a) => a.state === "authorized" || a.state === "authorized-token"),
    guided,
    skills: [], // P5 fills
    bridge: { status: "unknown" }, // P3 fills
    environment: { zembed: !!env },
  };
}

// ---- deps resolution -------------------------------------------------

// resolveDeps fills in every optional collaborator extension.js's fixed call
// site never passes. workspaceRoot uses !== undefined (not ||) so a test can
// explicitly assert "no workspace folder" with workspaceRoot: null, distinct
// from "unspecified, use the real default".
function resolveDeps(deps) {
  const d = deps || {};
  return {
    REGISTRY: d.REGISTRY || {},
    ALL_AGENT_IDS: d.ALL_AGENT_IDS || [],
    readZembedEnv: d.readZembedEnv || (() => null),
    runAgentAction: d.runAgentAction || (() => {}),
    fs: d.fs || fs,
    homeDir: d.homeDir || os.homedir(),
    workspaceRoot: d.workspaceRoot !== undefined ? d.workspaceRoot : defaultWorkspaceRoot(),
    // Timer + spawn seams for the auth flow below (ACK/cap timers, mark-oauth
    // invocation) — real by default, swappable in tests for a fake clock /
    // recorded child-process calls (welcomejs/harness.js makeFakeTimers()).
    setTimeout: d.setTimeout || ((fn, ms) => setTimeout(fn, ms)),
    clearTimeout: d.clearTimeout || ((id) => clearTimeout(id)),
    spawn: d.spawn || defaultSpawn,
  };
}

function defaultWorkspaceRoot() {
  try {
    const folders = vscode.workspace && vscode.workspace.workspaceFolders;
    if (folders && folders.length > 0) return folders[0].uri.fsPath;
  } catch (_) {}
  return null;
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

function collectFullState(deps) {
  const env = deps.readZembedEnv();
  return buildState({
    registry: deps.REGISTRY,
    agentIds: deps.ALL_AGENT_IDS,
    zembedEnv: env,
    creds: collectAllCreds(deps.fs, deps.homeDir, deps.ALL_AGENT_IDS),
    guided: collectGuided(deps.fs, deps.workspaceRoot),
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
// per-tile progress line welcome.html renders from (busy / dialog-opening /
// no-dashboard / unsupported / idle, spec §4).
function postAuth(agentId, phase) {
  if (!panel) return;
  try {
    panel.webview.postMessage({ type: "auth", agentId, phase });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// releaseAuthFlow clears whichever flow is in flight (bridge or terminal):
// cancels its timers and, for a terminal flow, disposes the
// onDidCloseTerminal listener registered for it. Never posts anything
// itself — every call site decides what (if anything) to tell the UI, since
// the panel-dispose cleanup path (see open()) needs silence.
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
// closes an in-flight flow once its completion signal has arrived:
//   - bridge: the zembed watcher flips the agent's computed state to
//     authorized/authorized-token (the platform flag landed, spec §4).
//   - terminal: the agent's credential ARTIFACT appears (spec §4) — the
//     platform flag is not the signal here, since mark-oauth below is what
//     eventually sets it; waiting on it would deadlock the flow.
// A 10-minute cap (started when each flow either reaches dialog-opening or
// is created, see handleAuthorize/handleAuthorizeTerminal) is the other,
// timer-driven release path for both kinds.
function reconcileAuthFlow(deps, state) {
  if (!authFlow) return;
  if (authFlow.kind === "bridge") {
    const agent = state.agents.find((a) => a.id === authFlow.agentId);
    if (agent && (agent.state === "authorized" || agent.state === "authorized-token")) {
      const agentId = authFlow.agentId;
      releaseAuthFlow(deps);
      postAuth(agentId, "idle");
    }
    return;
  }
  const cred = collectCred(deps.fs, deps.homeDir, authFlow.agentId);
  if (cred.present) {
    const agentId = authFlow.agentId;
    runMarkOAuth(deps, agentId);
    releaseAuthFlow(deps);
    postAuth(agentId, "idle");
  }
}

// runMarkOAuth invokes `zcp agent mark-oauth <agentId>` once a Tier-A
// terminal login's credential artifact appears (spec §4): reconciles the
// platform flag, the sidebar launcher (env-only), and the Zerops GUI with
// local reality. Fire-and-forget — a failure degrades to the existing
// "Locally logged in — platform sync pending" (local-only) state and must
// NEVER block or throw (spec §4).
function runMarkOAuth(deps, agentId) {
  let child;
  try {
    child = deps.spawn("zcp", ["agent", "mark-oauth", agentId], { shell: false });
  } catch (err) {
    console.warn("[zcp-welcome] zcp agent mark-oauth " + agentId + " failed to start:", err);
    return;
  }
  if (!child || typeof child.on !== "function") return; // a minimal test stub with nothing to observe
  child.on("error", (err) => {
    console.warn("[zcp-welcome] zcp agent mark-oauth " + agentId + " failed:", err);
  });
  child.on("exit", (code) => {
    if (code !== 0) console.warn("[zcp-welcome] zcp agent mark-oauth " + agentId + " exited with code " + code);
  });
}

// sendBridgeMessage instructs the webview to relay `payload` to every
// target origin via window.top.postMessage — see welcome.html's
// "bridge-send" handler. All protocol logic (what to send, to whom, when)
// lives here; the webview is a dumb pipe.
function sendBridgeMessage(payload, targets) {
  if (!panel) return;
  try {
    panel.webview.postMessage({ type: "bridge-send", payload, targets });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// handleAuthorize starts (or rejects) the bridge flow for a webview
// {type:"authorize", agentId} click (spec §4). agentId is already known to
// be one of the five launcher-registry ids — handleMessage's allowlist gate
// checked that; a value outside BRIDGE_SUPPORTED_AGENTS (v1: only
// claude-code) is a legitimate "not supported by this mechanism" case,
// answered with phase "unsupported", not a silent drop.
function handleAuthorize(agentId, deps) {
  if (!BRIDGE_SUPPORTED_AGENTS.has(agentId)) {
    postAuth(agentId, "unsupported");
    return;
  }
  if (authFlow) {
    postAuth(agentId, "busy");
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
  const ackTimer = deps.setTimeout(() => {
    if (!authFlow || authFlow.kind !== "bridge" || authFlow.eventId !== eventId) return;
    releaseAuthFlow(deps);
    postAuth(agentId, "no-dashboard");
  }, ACK_TIMEOUT_MS);
  unrefTimer(ackTimer);
  authFlow.ackTimer = ackTimer;
  sendBridgeMessage(payload, ORIGIN_ALLOWLIST);
}

// handleAuthorizeTerminal starts (or rejects) the Tier-A terminal flow for a
// webview {type:"authorize-terminal", agentId} click (spec §4). Shares the
// single `authFlow` slot with the bridge flow above — one authorization in
// flight per panel, of either kind.
function handleAuthorizeTerminal(agentId, deps) {
  const cmd = LOGIN_COMMANDS[agentId];
  if (!cmd) {
    postAuth(agentId, "unsupported");
    return;
  }
  if (authFlow) {
    postAuth(agentId, "busy");
    return;
  }
  const reg = deps.REGISTRY[agentId] || {};
  const label = reg.label || agentId;
  let terminal;
  try {
    terminal = vscode.window.createTerminal({ name: "Zerops: " + label + " login" });
  } catch (err) {
    console.error("[zcp-welcome] createTerminal failed:", err);
    return;
  }
  terminal.sendText(cmd, true);
  terminal.show();

  authFlow = { kind: "terminal", agentId, terminal, capTimer: null, closeDisposable: null };

  const capTimer = deps.setTimeout(() => {
    if (!authFlow || authFlow.kind !== "terminal" || authFlow.agentId !== agentId) return;
    releaseAuthFlow(deps);
    postAuth(agentId, "idle");
  }, AUTH_FLOW_CAP_MS);
  unrefTimer(capTimer);
  authFlow.capTimer = capTimer;

  authFlow.closeDisposable = vscode.window.onDidCloseTerminal((closed) => {
    if (closed !== terminal) return;
    if (!authFlow || authFlow.kind !== "terminal" || authFlow.agentId !== agentId) return;
    releaseAuthFlow(deps);
    postAuth(agentId, "idle");
  });
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
  if (!ORIGIN_ALLOWLIST.includes(msg.origin)) {
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
    if (authFlow.ackTimer) { deps.clearTimeout(authFlow.ackTimer); authFlow.ackTimer = null; }
    const capTimer = deps.setTimeout(() => {
      if (!authFlow || authFlow.kind !== "bridge" || authFlow.agentId !== agentId) return;
      releaseAuthFlow(deps);
      postAuth(agentId, "idle");
    }, AUTH_FLOW_CAP_MS);
    unrefTimer(capTimer);
    authFlow.capTimer = capTimer;
    postAuth(agentId, "dialog-opening");
    return;
  }
  if (data.accepted === false && data.reason === "unsupported-agent") {
    releaseAuthFlow(deps);
    postAuth(agentId, "unsupported");
    return;
  }
  console.log("[zcp-welcome] dropped bridge ack: unexpected accepted/reason combination");
}

// handleMessage is the strict allowlist gate (§8 W-SEC): exactly the shapes
// below do anything; everything else — including a well-formed message of
// an unknown type, or a message whose fields fail their check — is
// silently dropped (counted to console for debugging only), never thrown,
// never surfaced to the user.
function handleMessage(msg, deps) {
  if (!msg || typeof msg.type !== "string") return;
  switch (msg.type) {
    case "ready":
      postState(deps);
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
    case "authorize-terminal":
      if (typeof msg.agentId === "string" && deps.ALL_AGENT_IDS.includes(msg.agentId)) {
        handleAuthorizeTerminal(msg.agentId, deps);
      } else {
        console.log("[zcp-welcome] dropped authorize-terminal: bad agentId");
      }
      return;
    case "bridge-window-message":
      if (isWellFormedBridgeRelay(msg)) {
        handleBridgeWindowMessage(msg, deps);
      } else {
        console.log("[zcp-welcome] dropped bridge-window-message: malformed");
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
function disposeWatchers(deps) {
  if (pushTimer) { clearTimeout(pushTimer); pushTimer = null; }
  for (const d of disposables) {
    try { d.dispose(); } catch (_) {}
  }
  disposables = [];
  releaseAuthFlow(deps);
}

function schedulePush(deps) {
  if (pushTimer) clearTimeout(pushTimer);
  pushTimer = setTimeout(() => { pushTimer = null; postState(deps); }, STATE_PUSH_DEBOUNCE_MS);
  if (typeof pushTimer.unref === "function") pushTimer.unref(); // see unrefWatcher() above
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

// Zerops rewrites env.json IN PLACE (stable inode) on every env change, so
// watching the FILE (not its directory) — the same pattern as extension.js's
// launcher watcher — means we wake only on real env changes.
function watchZembedEnv(deps) {
  try {
    const w = deps.fs.watch(ZEMBED_ENV_FILE, () => schedulePush(deps));
    unrefWatcher(w);
    return { dispose() { try { w.close(); } catch (_) {} } };
  } catch (err) {
    console.warn("[zcp-welcome] fs.watch(zembed) unavailable:", err);
    return null;
  }
}

// watchCredDir watches an agent's credential DIR so a login (which creates
// the dir + writes the artifact, however the CLI does it) is caught. The dir
// may not exist yet (agent never logged in) — fs.watch on a missing path
// throws immediately, so we watch HOME instead (non-recursive) until the
// target dir appears, then swap. Every event on either watcher — rename or
// change, whatever the platform reports — just re-triggers a full recompute;
// there is no cheaper reliable way to notice an atomic-replace write landing
// (spec §3: "survive atomic rename writes").
function watchCredDir(fsImpl, homeDir, dirName, onEvent) {
  const target = path.join(homeDir, dirName);
  let watcher = null;

  function attachTarget() {
    try { watcher = fsImpl.watch(target, () => onEvent()); unrefWatcher(watcher); }
    catch (_) { watcher = null; }
  }

  function attachHome() {
    try {
      watcher = fsImpl.watch(homeDir, () => {
        let exists = false;
        try { exists = fsImpl.existsSync(target); } catch (_) { exists = false; }
        if (!exists) return;
        if (watcher) { try { watcher.close(); } catch (_) {} }
        attachTarget();
        onEvent();
      });
      unrefWatcher(watcher);
    } catch (_) { watcher = null; }
  }

  let targetExists = false;
  try { targetExists = fsImpl.existsSync(target); } catch (_) { targetExists = false; }
  if (targetExists) attachTarget(); else attachHome();

  return {
    dispose() { if (watcher) { try { watcher.close(); } catch (_) {} watcher = null; } },
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
    return { dispose() { try { w.close(); } catch (_) {} } };
  } catch (_) {
    return null;
  }
}

// startWatchers runs ONCE per panel (only from open()'s creation branch,
// never on reveal), so re-invoking the command never accumulates watchers
// (spec §1, W-ENTRY).
function startWatchers(deps) {
  const zembed = watchZembedEnv(deps);
  if (zembed) disposables.push(zembed);

  for (const dirName of Object.values(CRED_WATCH_DIR)) {
    const w = watchCredDir(deps.fs, deps.homeDir, dirName, () => schedulePush(deps));
    if (w) disposables.push(w);
  }

  const guided = watchGuidedMarker(deps.fs, deps.workspaceRoot, () => schedulePush(deps));
  if (guided) disposables.push(guided);
}

// open shows the singleton welcome panel: creates it (and starts its
// watchers) on the first call, reveals + re-reads state (never
// disposes/recreates) on every call after — see docs/spec-welcome-mode.md
// §1. No serializer is registered: after a window reload the panel is gone
// until the user re-invokes the command.
function open(ctx, deps) {
  const resolved = resolveDeps(deps);
  if (panel) {
    panel.reveal();
    postState(resolved); // re-invoking the command re-reads state (missed watcher events must not leave stale UI)
    return;
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
  // flow or guided toggle run.
  disposables.push(newPanel.onDidChangeViewState((e) => {
    const visible = e && e.webviewPanel ? e.webviewPanel.visible : newPanel.visible;
    if (visible) postState(resolved);
  }));
  startWatchers(resolved);
  panel = newPanel;
}

module.exports = { open, computeAgentState, buildState };
