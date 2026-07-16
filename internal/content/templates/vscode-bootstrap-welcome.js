const vscode = require("vscode");
const fs = require("fs");
const os = require("os");
const path = require("path");
const crypto = require("crypto");

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

let panel = null; // singleton — re-invoking open() reveals this, never recreates it
let disposables = []; // welcome-panel-scoped disposables (watchers, view-state listener): cleared on dispose
let pushTimer = null; // shared debounce timer for schedulePush()

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
  try {
    panel.webview.postMessage({ type: "state", payload: collectFullState(deps) });
  } catch (err) {
    console.error("[zcp-welcome] postMessage failed:", err);
  }
}

// handleMessage is the strict allowlist gate (§8 W-SEC): exactly "ready" and
// "open-url" (with an allowlisted url) do anything. Every other shape —
// including a well-formed message of an unknown type, or open-url with a
// url outside EXTERNAL_URLS — is silently dropped (counted to console for
// debugging only), never thrown, never surfaced to the user.
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
    default:
      console.log("[zcp-welcome] dropped unknown message type: " + msg.type);
      return;
  }
}

function disposeWatchers() {
  if (pushTimer) { clearTimeout(pushTimer); pushTimer = null; }
  for (const d of disposables) {
    try { d.dispose(); } catch (_) {}
  }
  disposables = [];
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
    disposeWatchers();
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
