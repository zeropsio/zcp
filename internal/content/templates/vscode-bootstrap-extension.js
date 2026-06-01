const vscode = require("vscode");
const fs = require("fs");
const path = require("path");

// ZCP agent launcher (zcp-bootstrap).
//
// Base layer: on startup / window reload, if no editors are open, show a
// webview that lists the agents named in ZCP_AGENT_TYPES; with none set it
// falls back to auto-opening the Claude Code plugin (the historical behavior).
//
// Live layer: watch the zembed env store and, when the resolved agent set
// actually changes, reopen the launcher — no restart, no polling. The agent
// set is read from the LIVE store (/etc/zerops-zembed/env.json, which zembed
// rewrites on every env change without restart), NOT from process.env, which
// a running extension host froze at code-server boot.

const CLAUDE_OPEN_COMMAND = "claude-vscode.editor.open";
const CLAUDE_EXT_ID = "anthropic.claude-code";
const ZEMBED_DIR = "/etc/zerops-zembed";
const ENV_FILE = path.join(ZEMBED_DIR, "env.json");

// Agent registry. Commands that bypass permission prompts were verified
// against the real CLI binaries / official docs; pinned by the Go test
// TestBootstrapExtension_AgentCommandsPinned so they cannot silently drift.
const REGISTRY = {
  "claude-code": { id: "claude-code", label: "Claude Code", action: "plugin", command: CLAUDE_OPEN_COMMAND, desc: "Opens the Claude Code extension — permissions bypassed" },
  "codex": { id: "codex", label: "Codex", action: "terminal", command: "codex --dangerously-bypass-approvals-and-sandbox", desc: "Runs codex with its sandbox disabled — full host access, skips all approvals" },
  "opencode-ai": { id: "opencode-ai", label: "opencode", action: "terminal", command: "opencode --dangerously-skip-permissions", desc: "Runs opencode — auto-approves all permissions" },
  "antigravity": { id: "antigravity", label: "Antigravity", action: "terminal", command: "agy --dangerously-skip-permissions", desc: "Runs agy — auto-approves all tool permissions" },
  "grok": { id: "grok", label: "Grok", action: "terminal", command: "grok", desc: "Runs grok — executes tools without approval prompts" },
};

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

function agentTypesFrom(env) {
  if (env && typeof env.ZCP_AGENT_TYPES === "string") return env.ZCP_AGENT_TYPES;
  if (env && "ZCP_AGENT_TYPES" in env) return ""; // present but null/empty
  return process.env.ZCP_AGENT_TYPES || ""; // no store (non-container) → frozen-env fallback
}

function resolveAgents(env) {
  return parseAgentTypes(agentTypesFrom(env));
}

// ---- Claude fallback (historical behavior) -----------------------------

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

// ---- webview launcher ---------------------------------------------------

function makeNonce() { return Date.now().toString(36) + Math.random().toString(36).slice(2, 12); }

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function renderHtml(agents, nonce) {
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

function runAgentAction(agent) {
  if (agent.action === "plugin") {
    vscode.commands.executeCommand(agent.command).then(
      () => console.log("[zcp-bootstrap] ran plugin command: " + agent.command),
      (err) => console.error("[zcp-bootstrap] plugin command failed:", err));
    return;
  }
  // Open in the integrated terminal PANEL as a NEW terminal: it coexists with
  // any the user already has (another entry in the panel's terminal list) and
  // takes focus so they can type right away. location=Panel forces the panel
  // regardless of the user's terminal.integrated.defaultLocation.
  const term = vscode.window.createTerminal({ name: "ZCP: " + agent.id, location: vscode.TerminalLocation.Panel });
  term.sendText(agent.command, true);
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
  console.log("[zcp-bootstrap] opened panel terminal for " + agent.id + ": " + agent.command);
}

// ---- launcher lifecycle + live watcher ---------------------------------

let currentPanel = null;
let lastShownKey = null; // resolved agent set last reflected in the UI
let panelMaximized = false; // whether we've already maximized the terminal panel this session

function agentsKey(agents) { return agents.map((a) => a.id).join(","); }

function openLauncher(agents) {
  if (currentPanel) { try { currentPanel.dispose(); } catch (_) {} currentPanel = null; }
  const panel = vscode.window.createWebviewPanel("zcpLauncher", "ZCP Launcher", vscode.ViewColumn.One, { enableScripts: true, retainContextWhenHidden: false });
  currentPanel = panel;
  panel.onDidDispose(() => { if (currentPanel === panel) currentPanel = null; });
  panel.webview.html = renderHtml(agents, makeNonce());
  panel.webview.onDidReceiveMessage((msg) => {
    if (!msg || msg.type !== "select") return;
    const agent = agents.find((a) => a.id === msg.id);
    if (!agent) return;
    console.log("[zcp-bootstrap] selected agent=" + agent.id);
    panel.dispose();
    runAgentAction(agent);
  });
  lastShownKey = agentsKey(agents);
}

async function showInitial() {
  const hasEditors = vscode.window.tabGroups.all.some((g) => g.tabs.length > 0);
  const agents = resolveAgents(readZembedEnv());
  if (hasEditors) {
    console.log("[zcp-bootstrap] editors open → skip initial open");
    lastShownKey = agentsKey(agents); // baseline so an unrelated env change won't pop later
    return;
  }
  if (agents.length) {
    console.log("[zcp-bootstrap] initial launcher: [" + agentsKey(agents) + "]");
    openLauncher(agents);
  } else {
    console.log("[zcp-bootstrap] no agents → Claude fallback");
    lastShownKey = "";
    await openClaudePlugin();
  }
}

// onEnvChange fires on any zembed env.json write. It reopens the launcher ONLY
// when the RESOLVED agent set actually changed — a change to some other env var
// rewrites env.json but yields the same set, so it is ignored.
function onEnvChange() {
  const env = readZembedEnv();
  if (env === null) { console.log("[zcp-bootstrap] env.json unreadable (transient) → ignore"); return; }
  const agents = resolveAgents(env);
  const key = agentsKey(agents);
  if (key === lastShownKey) {
    console.log("[zcp-bootstrap] env.json changed; agent set unchanged ([" + (key || "none") + "]) → ignore");
    return;
  }
  console.log("[zcp-bootstrap] live change → agents now: [" + (key || "none") + "]");
  if (agents.length) openLauncher(agents); // auto-open on a real change
  else lastShownKey = key; // cleared → record it, but don't barge in with Claude
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
// cards — so the user can launch another agent any time WITHOUT disturbing the
// editor area or anything on the right. Clicking a card runs the agent (panel
// terminal / plugin). The editor-tab launcher (startup / reload / live change)
// is unchanged; this is the on-demand entry point that leaves the rest as-is.
// (An activity-bar icon can only open a sidebar view — it can't open the
// editor-tab webview without switching the side bar — so the cards live here.)

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
      const agents = resolveAgents(readZembedEnv());
      view.webview.html = agents.length ? renderHtml(agents, makeNonce()) : renderEmptyHtml();
    };
    render();
    view.webview.onDidReceiveMessage((msg) => {
      if (!msg || msg.type !== "select") return;
      const agent = resolveAgents(readZembedEnv()).find((a) => a.id === msg.id);
      if (agent) { console.log("[zcp-bootstrap] (sidebar) selected agent=" + agent.id); runAgentAction(agent); }
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
