const vscode = require("vscode");
const fs = require("fs");
const path = require("path");

// ZCP agent launcher (zcp-bootstrap) — dual-mode.
//
// The launcher reads the LIVE zembed env store (/etc/zerops-zembed/env.json,
// which zembed rewrites on every env change without restart), NOT process.env
// (a running extension host froze that at code-server boot), and reacts to
// changes via fs.watch — no restart, no polling.
//
// Two modes, feature-detected from the store on every read:
//
//   Legacy mode (no per-agent auth envs present — today's default, current
//   production GUI): on startup / reload, if no editors are open, list the
//   agents named in ZCP_AGENT_TYPES as click-to-launch cards; with none set,
//   fall back to auto-opening the Claude Code plugin. UNCHANGED behavior.
//
//   Auth mode (the new Zerops GUI writes per-agent ZCP_AGENT_{AUTH_TYPE,OAUTH,
//   TOKEN}_<SUFFIX> envs): show ALL agents with their authorization status
//   (token / OAuth) and, when authorized, an action button per open mode
//   (extension / terminal); when not, a hint to authorize in the Zerops UI
//   panel beside the editor (auth is never performed in the extension).

const CLAUDE_OPEN_COMMAND = "claude-vscode.editor.open";
const CLAUDE_EXT_ID = "anthropic.claude-code";
const ZEMBED_DIR = "/etc/zerops-zembed";
const ENV_FILE = path.join(ZEMBED_DIR, "env.json");

// Agent registry. The launch commands BYPASS permission prompts and were
// verified against the real CLI binaries / official docs; pinned by the Go
// test TestBootstrapExtension_AgentCommandsPinned so they cannot silently
// drift. `suffix` is the per-agent env-key suffix (uppercase id, "-" → "_")
// used to read the auth envs in auth mode. `opens` lists the available launch
// modes in priority order — auth mode renders one button per entry; a legacy
// click uses opens[0] (Claude → its plugin, the rest → a panel terminal).
const REGISTRY = {
  "claude-code": {
    id: "claude-code", label: "Claude Code", suffix: "CLAUDE_CODE",
    desc: "Opens the Claude Code extension — permissions bypassed",
    opens: [
      { mode: "extension", command: CLAUDE_OPEN_COMMAND },
      { mode: "terminal", command: "claude" },
    ],
  },
  "codex": {
    id: "codex", label: "Codex", suffix: "CODEX",
    desc: "Runs codex with its sandbox disabled — full host access, skips all approvals",
    opens: [{ mode: "terminal", command: "codex --dangerously-bypass-approvals-and-sandbox" }],
  },
  "antigravity": {
    id: "antigravity", label: "Antigravity", suffix: "ANTIGRAVITY",
    desc: "Runs agy — auto-approves all tool permissions",
    opens: [{ mode: "terminal", command: "agy --dangerously-skip-permissions" }],
  },
  "grok": {
    id: "grok", label: "Grok", suffix: "GROK",
    desc: "Runs grok — executes tools without approval prompts",
    opens: [{ mode: "terminal", command: "grok" }],
  },
};

// Auth mode renders every agent, in this order. Legacy mode filters REGISTRY
// by ZCP_AGENT_TYPES instead. Keep in sync with the REGISTRY keys.
const ALL_AGENT_IDS = ["claude-code", "codex", "antigravity", "grok"];

// readZembedEnv returns the parsed live env store, or null if it can't be read
// (absent / mid-write / malformed). null lets the watch path ignore a transient
// read rather than misinterpret it as "no agents".
function readZembedEnv() {
  try {
    return JSON.parse(fs.readFileSync(ENV_FILE, "utf8"));
  } catch (_) {
    return null;
  }
}

// ---- legacy mode (ZCP_AGENT_TYPES) --------------------------------------

// parseAgentTypes splits ZCP_AGENT_TYPES (comma/whitespace separated) into an
// ordered, deduped list of known agents. Unknown tokens are dropped; the
// container controls display order.
function parseAgentTypes(raw) {
  if (!raw || typeof raw !== "string") return [];
  const seen = Object.create(null);
  const out = [];
  for (const tok of raw.split(/[\s,]+/)) {
    const id = tok.trim().toLowerCase();
    if (!id || !REGISTRY[id] || seen[id]) continue;
    seen[id] = true;
    out.push(REGISTRY[id]);
  }
  return out;
}

function agentTypesFrom(env) {
  if (env && typeof env.ZCP_AGENT_TYPES === "string") return env.ZCP_AGENT_TYPES;
  if (env && "ZCP_AGENT_TYPES" in env) return ""; // present but null/empty
  return process.env.ZCP_AGENT_TYPES || ""; // no store (non-container) → frozen-env fallback
}

function resolveAgents(env) {
  return parseAgentTypes(agentTypesFrom(env));
}

// ---- auth mode (per-agent ZCP_AGENT_{AUTH_TYPE,OAUTH,TOKEN}_<SUFFIX>) ----

// Auth mode is active iff the live store carries ANY per-agent auth env. The
// new Zerops GUI writes them; the old GUI never did, so their absence is the
// backward-compatible "render the legacy launcher" signal — no extra flag env
// is required from the platform.
const AUTH_ENV_RE = /^ZCP_AGENT_(AUTH_TYPE|OAUTH|TOKEN)_/;

function isAuthMode(env) {
  return !!env && Object.keys(env).some((k) => AUTH_ENV_RE.test(k));
}

// agentStatus reads the three suffixed envs for one agent.
//   authType   = ZCP_AGENT_AUTH_TYPE_<SUFFIX>   ("oauth" | "token" | undefined)
//   authorized = ZCP_AGENT_OAUTH_<SUFFIX> === "true" || !!ZCP_AGENT_TOKEN_<SUFFIX>
// authorized collapses "OAuth flow done" and "token present" into one
// ready-to-use signal. The token VALUE is sensitive and only ever
// presence-checked here — it never reaches the UI.
function agentStatus(env, agent) {
  const at = env["ZCP_AGENT_AUTH_TYPE_" + agent.suffix];
  const authType = at === "oauth" || at === "token" ? at : undefined;
  const authorized = env["ZCP_AGENT_OAUTH_" + agent.suffix] === "true" || !!env["ZCP_AGENT_TOKEN_" + agent.suffix];
  return { authType, authorized };
}

function resolveAgentStates(env) {
  return ALL_AGENT_IDS.map((id) => Object.assign({}, REGISTRY[id], agentStatus(env, REGISTRY[id])));
}

function statusBadge(st) {
  if (st.authorized) {
    if (st.authType === "oauth") return "✓ Authorized (OAuth)";
    if (st.authType === "token") return "✓ Authorized (token)";
    return "✓ Authorized";
  }
  if (st.authType === "oauth") return "○ Needs authorization";
  if (st.authType === "token") return "⚠ Token missing";
  return "— Not configured";
}

// ---- view resolution (mode + payload) -----------------------------------

function buildView(env) {
  if (isAuthMode(env)) return { mode: "auth", states: resolveAgentStates(env) };
  return { mode: "legacy", agents: resolveAgents(env) };
}

function viewHasContent(view) {
  return view.mode === "auth" ? view.states.length > 0 : view.agents.length > 0;
}

// viewKey is the signature the watcher compares to decide whether a store write
// actually altered what the UI shows. Auth mode folds in each agent's auth
// state, so authorizing/revoking — or a legacy→auth transition — re-renders; a
// change to some unrelated env var yields the same key and is ignored.
function viewKey(view) {
  if (view.mode === "auth") {
    return "auth:" + view.states.map((s) => s.id + ":" + (s.authType || "-") + ":" + (s.authorized ? "1" : "0")).join(",");
  }
  return "legacy:" + view.agents.map((a) => a.id).join(",");
}

// pickAction maps an inbound webview message to {agent, mode}, or null. Legacy
// "select" launches the agent's primary open mode; auth "launch" carries the
// chosen mode and fires only for an authorized agent with that mode available.
function pickAction(view, msg) {
  if (!msg) return null;
  if (view.mode === "auth" && msg.type === "launch") {
    const st = view.states.find((s) => s.id === msg.id);
    if (st && st.authorized && st.opens.some((o) => o.mode === msg.mode)) return { agent: st, mode: msg.mode };
    return null;
  }
  if (view.mode === "legacy" && msg.type === "select") {
    const agent = view.agents.find((a) => a.id === msg.id);
    if (agent) return { agent, mode: agent.opens[0].mode };
  }
  return null;
}

// ---- Claude fallback (legacy, historical behavior) ----------------------

async function waitForCommand(commandId, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const cmds = await vscode.commands.getCommands(true);
      if (cmds.includes(commandId)) return true;
    } catch (_) {}
    await new Promise((r) => setTimeout(r, 250));
  }
  return false;
}

async function openClaudePlugin() {
  const ext = vscode.extensions.getExtension(CLAUDE_EXT_ID);
  if (!ext) { console.warn("[zcp-bootstrap] anthropic.claude-code not installed"); return; }
  try { if (!ext.isActive) await ext.activate(); } catch (err) { console.error("[zcp-bootstrap] claude activate failed:", err); }
  if (!(await waitForCommand(CLAUDE_OPEN_COMMAND, 8000))) { console.warn("[zcp-bootstrap] claude command never registered"); return; }
  try { await vscode.commands.executeCommand(CLAUDE_OPEN_COMMAND); console.log("[zcp-bootstrap] opened Claude Code as tab"); }
  catch (err) { console.error("[zcp-bootstrap] claude open failed:", err); }
  try { await vscode.commands.executeCommand("workbench.action.closeAuxiliaryBar"); } catch (_) {}
}

// ---- webview rendering --------------------------------------------------

function makeNonce() { return Date.now().toString(36) + Math.random().toString(36).slice(2, 12); }

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// Legacy launcher: clickable cards that launch the agent on click (historical
// behavior, unchanged).
function renderLegacyHtml(agents, nonce) {
  const cards = agents.map((a) => `
      <button class="card" data-id="${escapeHtml(a.id)}">
        <div class="card-title">${escapeHtml(a.label)}</div>
        <div class="card-desc">${escapeHtml(a.desc || a.id)}</div>
      </button>`).join("\n");
  return `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';">
<style>
  body { font-family: var(--vscode-font-family); color: var(--vscode-foreground); background: var(--vscode-editor-background); padding: 40px; margin: 0; }
  h1 { font-size: 22px; font-weight: 600; margin: 0 0 6px; }
  .sub { opacity: .7; margin: 0 0 28px; font-size: 13px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; max-width: 920px; }
  .card { text-align: left; cursor: pointer; border: 1px solid var(--vscode-panel-border, rgba(128,128,128,.35)); border-radius: 10px; padding: 18px;
          background: var(--vscode-button-secondaryBackground, rgba(128,128,128,.08)); color: var(--vscode-foreground); transition: border-color .12s, transform .06s; }
  .card:hover { border-color: var(--vscode-focusBorder); transform: translateY(-2px); }
  .card-title { font-size: 16px; font-weight: 600; margin-bottom: 6px; }
  .card-desc { font-size: 12.5px; opacity: .75; line-height: 1.4; }
</style></head><body>
  <h1>Choose your coding agent</h1>
  <p class="sub">Pick one to start — it opens in this window.</p>
  <div class="grid">${cards}</div>
  <script nonce="${nonce}">
    const api = acquireVsCodeApi();
    for (const el of document.querySelectorAll(".card")) {
      el.addEventListener("click", () => api.postMessage({ type: "select", id: el.getAttribute("data-id") }));
    }
  </script></body></html>`;
}

// Auth launcher: every agent with its authorization status. Authorized agents
// expose an action button per open mode (extension / terminal); the rest show a
// hint to authorize in the Zerops UI panel beside the editor. The token value
// never enters the DOM — only its presence drove `authorized` upstream.
function renderAuthHtml(states, nonce) {
  const cards = states.map((st) => {
    const badge = statusBadge(st);
    const body = st.authorized
      ? `<div class="acts">${st.opens.map((o) =>
          `<button class="act" data-id="${escapeHtml(st.id)}" data-mode="${escapeHtml(o.mode)}">${o.mode === "extension" ? "Open extension" : "Open terminal"}</button>`).join("")}</div>
        <div class="card-desc">${escapeHtml(st.desc)}</div>`
      : `<div class="card-desc hint">Authorize ${escapeHtml(st.label)} in the Zerops panel beside this editor — it becomes available here once authorized.</div>`;
    return `
      <div class="card">
        <div class="card-head">
          <span class="card-title">${escapeHtml(st.label)}</span>
          <span class="badge ${st.authorized ? "ok" : "todo"}">${escapeHtml(badge)}</span>
        </div>
        ${body}
      </div>`;
  }).join("\n");
  return `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';">
<style>
  body { font-family: var(--vscode-font-family); color: var(--vscode-foreground); background: var(--vscode-editor-background); padding: 40px; margin: 0; }
  h1 { font-size: 22px; font-weight: 600; margin: 0 0 6px; }
  .sub { opacity: .7; margin: 0 0 28px; font-size: 13px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 16px; max-width: 920px; }
  .card { text-align: left; border: 1px solid var(--vscode-panel-border, rgba(128,128,128,.35)); border-radius: 10px; padding: 18px;
          background: var(--vscode-button-secondaryBackground, rgba(128,128,128,.08)); color: var(--vscode-foreground); }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; margin-bottom: 12px; }
  .card-title { font-size: 16px; font-weight: 600; }
  .badge { font-size: 11.5px; font-weight: 600; white-space: nowrap; }
  .badge.ok { color: var(--vscode-testing-iconPassed, #3fb950); }
  .badge.todo { opacity: .7; }
  .acts { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 8px; }
  .act { cursor: pointer; border: 1px solid var(--vscode-button-border, transparent); border-radius: 6px; padding: 6px 12px; font-size: 12.5px;
         background: var(--vscode-button-background); color: var(--vscode-button-foreground); }
  .act:hover { background: var(--vscode-button-hoverBackground); }
  .card-desc { font-size: 12.5px; opacity: .75; line-height: 1.4; }
  .card-desc.hint { opacity: .85; }
</style></head><body>
  <h1>Your coding agents</h1>
  <p class="sub">Authorize agents in the Zerops panel; authorized ones open here.</p>
  <div class="grid">${cards}</div>
  <script nonce="${nonce}">
    const api = acquireVsCodeApi();
    for (const el of document.querySelectorAll(".act")) {
      el.addEventListener("click", () => api.postMessage({ type: "launch", id: el.getAttribute("data-id"), mode: el.getAttribute("data-mode") }));
    }
  </script></body></html>`;
}

function viewHtml(view, nonce) {
  return view.mode === "auth" ? renderAuthHtml(view.states, nonce) : renderLegacyHtml(view.agents, nonce);
}

// ---- launching ----------------------------------------------------------

function runAgentAction(agent, mode) {
  const open = agent.opens.find((o) => o.mode === mode) || agent.opens[0];
  if (open.mode === "extension") {
    vscode.commands.executeCommand(open.command).then(
      () => console.log("[zcp-bootstrap] ran plugin command: " + open.command),
      (err) => console.error("[zcp-bootstrap] plugin command failed:", err));
    return;
  }
  // Open in the integrated terminal PANEL as a NEW terminal: it coexists with
  // any the user already has (another entry in the panel's terminal list) and
  // takes focus so they can type right away. location=Panel forces the panel
  // regardless of the user's terminal.integrated.defaultLocation.
  const term = vscode.window.createTerminal({ name: "ZCP: " + agent.id, location: vscode.TerminalLocation.Panel });
  term.sendText(open.command, true);
  term.show(); // reveal panel + make this the active terminal + focus it
  // Once the xterm has mounted: give the panel more height (maximize it once
  // per session — VS Code only offers a toggle, so the flag prevents a second
  // agent from un-maximizing it), then re-assert focus. terminal.focus targets
  // the panel's ACTIVE terminal — the one we just show()'d, so it lands on ours.
  setTimeout(() => {
    term.show();
    if (!panelMaximized) {
      vscode.commands.executeCommand("workbench.action.toggleMaximizedPanel").then(() => { panelMaximized = true; }, () => {});
    }
    vscode.commands.executeCommand("workbench.action.terminal.focus").then(undefined, () => {});
  }, 250);
  console.log("[zcp-bootstrap] opened panel terminal for " + agent.id + ": " + open.command);
}

// ---- launcher lifecycle + live watcher ----------------------------------

let currentPanel = null;
let lastShownKey = null; // view signature last reflected in the UI
let panelMaximized = false; // whether we've already maximized the terminal panel this session

function openLauncher(view) {
  if (currentPanel) { try { currentPanel.dispose(); } catch (_) {} currentPanel = null; }
  const panel = vscode.window.createWebviewPanel("zcpLauncher", "ZCP Launcher", vscode.ViewColumn.One, { enableScripts: true, retainContextWhenHidden: false });
  currentPanel = panel;
  panel.onDidDispose(() => { if (currentPanel === panel) currentPanel = null; });
  panel.webview.html = viewHtml(view, makeNonce());
  panel.webview.onDidReceiveMessage((msg) => {
    const action = pickAction(view, msg);
    if (!action) return;
    console.log("[zcp-bootstrap] launch agent=" + action.agent.id + " mode=" + action.mode);
    panel.dispose();
    runAgentAction(action.agent, action.mode);
  });
  lastShownKey = viewKey(view);
}

async function showInitial() {
  const hasEditors = vscode.window.tabGroups.all.some((g) => g.tabs.length > 0);
  const view = buildView(readZembedEnv());
  if (hasEditors) {
    console.log("[zcp-bootstrap] editors open → skip initial open");
    lastShownKey = viewKey(view); // baseline so an unrelated env change won't pop later
    return;
  }
  if (viewHasContent(view)) {
    console.log("[zcp-bootstrap] initial launcher: " + viewKey(view));
    openLauncher(view);
  } else {
    console.log("[zcp-bootstrap] no agents → Claude fallback");
    lastShownKey = viewKey(view);
    await openClaudePlugin();
  }
}

// onEnvChange fires on any zembed env.json write. It reopens the launcher ONLY
// when the view signature actually changed (see viewKey).
function onEnvChange() {
  const env = readZembedEnv();
  if (env === null) { console.log("[zcp-bootstrap] env.json unreadable (transient) → ignore"); return; }
  const view = buildView(env);
  const key = viewKey(view);
  if (key === lastShownKey) {
    console.log("[zcp-bootstrap] env.json changed; view unchanged (" + key + ") → ignore");
    return;
  }
  console.log("[zcp-bootstrap] live change → view now: " + key);
  if (viewHasContent(view)) openLauncher(view); // auto-open on a real change
  else lastShownKey = key; // legacy cleared → record it, but don't barge in with Claude
}

function startEnvWatcher(ctx) {
  let timer = null;
  try {
    // Watch the file directly: zembed rewrites env.json IN-PLACE (stable
    // inode), so an inode-based file watch persists across writes. Watching
    // the file (not the dir) means we only wake on env.json changes, never on
    // unrelated zembed writes (healthcheck, certs, config).
    const w = fs.watch(ENV_FILE, () => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(onEnvChange, 400); // debounce: a single write emits >1 event
    });
    ctx.subscriptions.push({ dispose: () => { try { w.close(); } catch (_) {} if (timer) clearTimeout(timer); } });
    console.log("[zcp-bootstrap] watching " + ENV_FILE + " for live agent-set changes");
  } catch (err) {
    console.warn("[zcp-bootstrap] fs.watch unavailable (" + err + ") — live updates off, reload still works");
  }
}

// ---- activity-bar launcher view ----------------------------------------
// The Zerops-logo activity-bar icon opens this sidebar view with the agent
// cards — so the user can launch (or check auth status of) an agent any time
// WITHOUT disturbing the editor area or anything on the right. Clicking an
// action runs the agent (panel terminal / plugin). The editor-tab launcher
// (startup / reload / live change) is unchanged; this is the on-demand entry
// point that leaves the rest as-is. (An activity-bar icon can only open a
// sidebar view — it can't open the editor-tab webview without switching the
// side bar — so the cards live here too.)

const VIEW_ID = "zcpAgents";

function renderEmptyHtml() {
  return `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline';">
<style>body{font-family:var(--vscode-font-family);color:var(--vscode-descriptionForeground);padding:16px;font-size:13px;line-height:1.5}code{background:var(--vscode-textCodeBlock-background);padding:1px 5px;border-radius:4px}</style>
</head><body>No agents configured.<br>Set <code>ZCP_AGENT_TYPES</code> on the service.</body></html>`;
}

const agentsViewProvider = {
  resolveWebviewView(view) {
    view.webview.options = { enableScripts: true };
    const render = () => {
      const v = buildView(readZembedEnv());
      if (v.mode === "auth") { view.webview.html = renderAuthHtml(v.states, makeNonce()); return; }
      view.webview.html = v.agents.length ? renderLegacyHtml(v.agents, makeNonce()) : renderEmptyHtml();
    };
    render();
    view.webview.onDidReceiveMessage((msg) => {
      const action = pickAction(buildView(readZembedEnv()), msg);
      if (action) { console.log("[zcp-bootstrap] (sidebar) launch agent=" + action.agent.id + " mode=" + action.mode); runAgentAction(action.agent, action.mode); }
    });
    view.onDidChangeVisibility(() => { if (view.visible) render(); }); // re-read live set when reopened
  },
};

async function activate(ctx) {
  console.log("[zcp-bootstrap] activate");
  ctx.subscriptions.push(vscode.window.registerWebviewViewProvider(VIEW_ID, agentsViewProvider));
  await showInitial();
  startEnvWatcher(ctx);
}

function deactivate() {}

module.exports = { activate, deactivate };
