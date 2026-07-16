const vscode = require("vscode");
const fs = require("fs");
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
// safety-pinned launch commands stay in exactly one copy.

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

let panel = null; // singleton — re-invoking open() reveals this, never recreates it
let disposables = []; // welcome-panel-scoped disposables, cleared on dispose; P2's watchers land here

// buildState is P1's static skeleton: every agent renders "checking" — the
// real §3 auth matrix, guided/skills/bridge detection are P2+'s job. Shaped
// so later slices can fill in fields without changing what the client
// already reads. version is intentionally omitted rather than duplicating
// adapters.BootstrapExtVersion as a second, driftable copy.
function buildState(deps) {
  const registry = (deps && deps.REGISTRY) || {};
  const ids = (deps && deps.ALL_AGENT_IDS) || [];
  return {
    agents: ids.map((id) => {
      const reg = registry[id];
      return { id, label: (reg && reg.label) || id, status: "checking" };
    }),
    guided: { known: false },
    skills: [],
    bridge: { known: false },
  };
}

function readWelcomeHtml(ctx, nonce) {
  const htmlPath = path.join(ctx.extensionPath, "welcome.html");
  const raw = fs.readFileSync(htmlPath, "utf8");
  return raw.split(NONCE_PLACEHOLDER).join(nonce);
}

function postState(deps) {
  if (!panel) return;
  try {
    panel.webview.postMessage({ type: "state", payload: buildState(deps) });
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
  for (const d of disposables) {
    try { d.dispose(); } catch (_) {}
  }
  disposables = [];
}

// open shows the singleton welcome panel: creates it on the first call,
// reveals (never disposes/recreates) it on every call after — see
// docs/spec-welcome-mode.md §1. No serializer is registered: after a window
// reload the panel is gone until the user re-invokes the command.
function open(ctx, deps) {
  if (panel) {
    panel.reveal();
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
  newPanel.webview.onDidReceiveMessage((msg) => handleMessage(msg, deps));
  newPanel.onDidDispose(() => {
    if (panel === newPanel) panel = null;
    disposeWatchers();
  });
  panel = newPanel;
}

module.exports = { open, buildState };
