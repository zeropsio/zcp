const vscode = require("vscode");
const fs = require("fs");
const path = require("path");

// ZCP agent launcher (zcp-bootstrap) — single auth-aware model.
//
// The launcher reads the LIVE zembed env store (/etc/zerops-zembed/env.json,
// which zembed rewrites on every env change without restart), NOT process.env
// (a running extension host froze that at code-server boot), and reacts to
// changes via fs.watch — no restart, no polling.
//
// Every agent in REGISTRY is shown, gated by three independent axes:
//
//   availability — ZCP_AGENTS (a zcp-owned PRESENTATION env: which agents
//   this container offers, in which order). No store, or a store without the
//   key, offers every agent (today's images predate this env); an explicitly
//   present but unusable value fails CLOSED to zero — never open to "all".
//
//   installed — a real PATH probe of the agent's CLI binary (`bin`) against
//   this extension host's own process.env.PATH. A CLI binary being installed
//   does not prove its VS Code plugin (claude-code's "extension" open mode)
//   is too — see runAgentAction's fallback.
//
//   authorized — per-agent ZCP_AGENT_{AUTH_TYPE,OAUTH,TOKEN}_<SUFFIX> envs,
//   written by the Zerops GUI once the user authorizes that agent there (auth
//   is never performed in the extension).
//
// Not installed → a muted row, no action buttons. Installed + authorized →
// an action button per open mode (extension / terminal). Installed + not
// authorized → a hint to authorize in the Zerops UI panel beside the editor.

const CLAUDE_OPEN_COMMAND = "claude-vscode.editor.open";
const ZEMBED_DIR = "/etc/zerops-zembed";
const ENV_FILE = path.join(ZEMBED_DIR, "env.json");
const STARTUP_FILE = path.join(__dirname, "startup.json");

// Agent registry. The launch commands BYPASS permission prompts and were
// verified against the real CLI binaries / official docs; pinned by the Go
// test TestBootstrapExtension_AgentCommandsPinned so they cannot silently
// drift. `bin` is the PATH-probed executable name (isAgentInstalled).
// `suffix` is the per-agent env-key suffix (uppercase id, "-" → "_") used to
// read the auth envs. `opens` lists the available launch modes in priority
// order — the launcher renders one button per entry for an authorized agent.
const REGISTRY = {
  "claude-code": {
    id: "claude-code", label: "Claude Code", suffix: "CLAUDE_CODE", bin: "claude",
    desc: "Opens the Claude plugin, or a terminal at max effort (permissions bypassed)",
    opens: [
      { mode: "extension", command: CLAUDE_OPEN_COMMAND },
      { mode: "terminal", command: "claude --dangerously-skip-permissions --effort max" },
    ],
  },
  "codex": {
    id: "codex", label: "Codex", suffix: "CODEX", bin: "codex",
    desc: "Runs codex with its sandbox disabled — full host access, skips all approvals",
    opens: [{ mode: "terminal", command: "codex --dangerously-bypass-approvals-and-sandbox" }],
  },
  "antigravity": {
    id: "antigravity", label: "Antigravity", suffix: "ANTIGRAVITY", bin: "agy",
    desc: "Runs agy — auto-approves all tool permissions",
    opens: [{ mode: "terminal", command: "agy --dangerously-skip-permissions", initialPromptFlag: "--prompt-interactive" }],
  },
  "grok": {
    id: "grok", label: "Grok Build", suffix: "GROK", bin: "grok",
    desc: "Runs grok --yolo — auto-approves all tool executions (deny rules still apply)",
    opens: [{ mode: "terminal", command: "grok --yolo" }],
  },
  "cursor": {
    id: "cursor", label: "Cursor CLI", suffix: "CURSOR", bin: "cursor-agent",
    desc: "Runs cursor-agent with --force --approve-mcps — auto-allows commands/edits and auto-approves MCP servers",
    opens: [{ mode: "terminal", command: "cursor-agent --force --approve-mcps" }],
  },
};

// The launcher considers every agent in this canonical order before
// resolveAvailableAgentIds filters/reorders it. Keep in sync with the
// REGISTRY keys.
const ALL_AGENT_IDS = ["claude-code", "codex", "antigravity", "grok", "cursor"];

// readZembedEnv returns the parsed live env store, or null if it can't be read
// (absent / mid-write / malformed). null lets callers distinguish "no store"
// (offer everything) from "a transient read glitch" (the watch path ignores
// it rather than misinterpreting it as a real change).
function readZembedEnv() {
  try {
    return JSON.parse(fs.readFileSync(ENV_FILE, "utf8"));
  } catch (_) {
    return null;
  }
}

// zcp init resolves startup presentation from the platform-provided
// zeropsSubdomain and writes the result beside this extension. Keep activation
// fail-closed: an absent, malformed, or non-boolean policy preserves the
// historical launcher/restored-editor behavior.
function hasAgentFirstPolicy() {
  try {
    const config = JSON.parse(fs.readFileSync(STARTUP_FILE, "utf8"));
    return !!config && config.agentFirst === true;
  } catch (_) {
    return false;
  }
}

// resolveAvailableAgentIds(env) reads ZCP_AGENTS: image/recipe PRESENTATION
// policy (which agents this container offers, in which order) — NOT
// authorization and NOT a security boundary; auth stays the per-agent envs,
// "installed" stays the PATH probe. No store, or a store without the key,
// offers every agent. Once the key is present, an unusable value (not a
// string) fails CLOSED to zero — it never falls back to "all": an explicit
// restriction that resolves to nothing is an honest empty state, not
// fail-open.
function resolveAvailableAgentIds(env) {
  if (env === null || !("ZCP_AGENTS" in env)) return ALL_AGENT_IDS;
  const raw = env.ZCP_AGENTS;
  if (typeof raw !== "string") return [];
  const seen = Object.create(null);
  const out = [];
  for (const tok of raw.split(/[\s,]+/)) {
    const id = tok.trim().toLowerCase();
    if (!id || !REGISTRY[id] || seen[id]) continue;
    seen[id] = true;
    out.push(id);
  }
  return out;
}

// isAgentInstalled probes the UNION of this extension host's own
// process.env.PATH and the live zembed store's PATH (env, when readable) for
// `bin` as an executable file — a hit on either counts. Host-PATH-only was a
// live-verified 0.1.5 regression: code-server's extension host froze a PATH
// NARROWER than the runtime profile PATH (it lacked the agent bin dirs), so
// every agent probed "Not installed" in a container where terminals — which
// source the profile, whose PATH the zembed store mirrors — launch them
// fine. No shell, no child process. Unix semantics only (this extension only
// ever runs in-container or on macOS); no PATHEXT handling.
function isAgentInstalled(bin, env) {
  const storePath = env && typeof env.PATH === "string" ? env.PATH : "";
  const dirs = (process.env.PATH || "").split(path.delimiter)
    .concat(storePath.split(path.delimiter));
  for (const dir of dirs) {
    if (!dir) continue;
    const candidate = path.join(dir, bin);
    try {
      if (!fs.statSync(candidate).isFile()) continue;
      fs.accessSync(candidate, fs.constants.X_OK);
      return true;
    } catch (_) {
      continue;
    }
  }
  return false;
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

// ---- view resolution ------------------------------------------------------

// buildView resolves the three axes into the one shape the renderer and
// pickAction both consume: every available agent, in order, with its
// installed probe and auth status attached.
function buildView(env) {
  const status = env || {};
  const agents = resolveAvailableAgentIds(env).map((id) =>
    Object.assign({}, REGISTRY[id], { installed: isAgentInstalled(REGISTRY[id].bin, env) }, agentStatus(status, REGISTRY[id])));
  return { agents };
}

// viewKey is the signature the watcher (and the initial-open baseline)
// compares to decide whether a store write actually altered what the UI
// shows: an availability reorder/removal, an installed flip (a binary
// appearing or vanishing between recomputes), or an auth change all produce a
// different key; an unrelated env var write does not.
function viewKey(view) {
  return view.agents.map((a) => a.id + ":" + (a.installed ? "1" : "0") + ":" + (a.authType || "-") + ":" + (a.authorized ? "1" : "0")).join(",");
}

// pickAction maps an inbound webview "launch" message to {agent, mode}, or
// null. It fires only for an agent that is — in the view it's checked
// against — available, installed, authorized, and offers the requested mode.
// Callers rebuild the view from a fresh readZembedEnv() right before calling
// this, so a click can never launch a since-revoked/removed agent.
function pickAction(view, msg) {
  if (!msg || msg.type !== "launch") return null;
  const agent = view.agents.find((a) => a.id === msg.id);
  if (agent && agent.installed && agent.authorized && agent.opens.some((o) => o.mode === msg.mode)) {
    return { agent, mode: msg.mode };
  }
  return null;
}

// ---- webview rendering ----------------------------------------------------

function makeNonce() { return Date.now().toString(36) + Math.random().toString(36).slice(2, 12); }

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function statusBadge(a) {
  if (!a.installed) return "Not installed in this container";
  if (a.authorized) {
    if (a.authType === "oauth") return "✓ Authorized (OAuth)";
    if (a.authType === "token") return "✓ Authorized (token)";
    return "✓ Authorized";
  }
  if (a.authType === "oauth") return "○ Needs authorization";
  if (a.authType === "token") return "⚠ Token missing";
  return "— Not configured";
}

function renderEmptyHtml() {
  return `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline';">
<style>body{font-family:var(--vscode-font-family);color:var(--vscode-descriptionForeground);padding:16px;font-size:13px;line-height:1.5}</style>
</head><body>No coding agents are enabled for this container.</body></html>`;
}

// The one launcher renderer: every available agent with its installed +
// authorization status. A not-installed agent is a muted row with no action
// buttons. An installed + authorized agent exposes an action button per open
// mode (extension / terminal). Installed + not authorized shows a hint to
// authorize in the Zerops UI panel beside the editor. The token value never
// enters the DOM — only its presence drove `authorized` upstream.
function renderLauncherHtml(agents, nonce) {
  if (agents.length === 0) return renderEmptyHtml();
  const cards = agents.map((a) => {
    const badge = statusBadge(a);
    let body;
    if (!a.installed) {
      body = `<div class="card-desc hint">${escapeHtml(a.desc)}</div>`;
    } else if (a.authorized) {
      body = `<div class="acts">${a.opens.map((o) =>
          `<button class="act" data-id="${escapeHtml(a.id)}" data-mode="${escapeHtml(o.mode)}">${o.mode === "extension" ? "Open extension" : "Open terminal"}</button>`).join("")}</div>
        <div class="card-desc">${escapeHtml(a.desc)}</div>`;
    } else {
      body = `<div class="card-desc hint">Authorize ${escapeHtml(a.label)} in the Zerops panel beside this editor — it becomes available here once authorized.</div>`;
    }
    return `
      <div class="card${a.installed ? "" : " muted"}">
        <div class="card-head">
          <span class="card-title">${escapeHtml(a.label)}</span>
          <span class="badge ${a.installed && a.authorized ? "ok" : "todo"}">${escapeHtml(badge)}</span>
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
  .card.muted { opacity: .55; }
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

// ---- launching --------------------------------------------------------

function runTerminal(agent, open, opts) {
  const onboarding = !!(opts && opts.onboarding);
  // Open in the integrated terminal PANEL as a NEW terminal: it coexists with
  // any the user already has (another entry in the panel's terminal list) and
  // takes focus so they can type right away. location=Panel forces the panel
  // regardless of the user's terminal.integrated.defaultLocation.
  const term = vscode.window.createTerminal({ name: "ZCP: " + agent.id, location: vscode.TerminalLocation.Panel });
  term.sendText(open.command, true);
  term.show(); // reveal panel + make this the active terminal + focus it
  // Once the xterm has mounted: give the panel more height, then re-assert
  // focus. terminal.focus targets the panel's ACTIVE terminal — the one we
  // just show()'d, so it lands on ours.
  //
  // VS Code exposes ONLY a toggle for panel-maximize — no maximize command,
  // no query for the current state — so "is it already maximized" can only
  // ever be a guess. For a non-onboarding call (panel/legacy launcher) that
  // guess is the session-scoped panelMaximized flag below: maximize once,
  // never again this session. That flag is exactly what goes STALE — once
  // anything un-maximizes the panel behind its back (a reload, the user, a
  // second launch in a dev loop), it still reads true and every later call
  // silently skips the toggle forever in that window.
  //
  // The onboarding launch (docs/spec-welcome-mode.md §5.3, opts.onboarding)
  // must not inherit that staleness, so it ignores the guess and always
  // toggles. This is deterministic in the case that actually matters: a
  // fresh window's panel is never maximized yet, and onboarding is the
  // FIRST terminal this window creates in the common case. Accepted
  // residual risk, in the same spirit as §5.4's other accepted sharp edges:
  // if the panel is somehow ALREADY maximized when an onboarding launch
  // fires (e.g. a same-window dev-loop re-run), this un-maximizes it
  // instead — there is no query that would let this code tell the
  // difference.
  setTimeout(() => {
    term.show();
    if (onboarding || !panelMaximized) {
      vscode.commands.executeCommand("workbench.action.toggleMaximizedPanel").then(() => { panelMaximized = true; }, () => {});
    }
    vscode.commands.executeCommand("workbench.action.terminal.focus").then(undefined, () => {});
  }, 250);
  console.log("[zcp-bootstrap] opened panel terminal for " + agent.id + ": " + open.command);
}

function runAgentAction(agent, mode, opts) {
  const open = agent.opens.find((o) => o.mode === mode) || agent.opens[0];
  if (open.mode === "extension") {
    // Seeded launches may carry command arguments (Claude's editor.open
    // accepts sessionId, initialPrompt). Plain launches carry no args and
    // therefore preserve the historical command call exactly.
    const args = Array.isArray(open.args) ? open.args : [];
    vscode.commands.executeCommand(open.command, ...args).then(
      () => console.log("[zcp-bootstrap] ran plugin command: " + open.command),
      (err) => {
        // The CLI binary being installed does not prove its VS Code plugin
        // is: fall back to a terminal open mode when the agent has one.
        const fallback = agent.opens.find((o) => o.mode === "terminal");
        if (fallback) {
          console.warn("[zcp-bootstrap] plugin command failed (" + err + "), falling back to terminal for " + agent.id);
          runTerminal(agent, fallback, opts);
        } else {
          console.error("[zcp-bootstrap] plugin command failed:", err);
        }
      });
    return;
  }
  runTerminal(agent, open, opts);
}

// ---- launcher lifecycle + live watcher ------------------------------------

let currentPanel = null;
let lastShownKey = null; // view signature last reflected in the UI
let panelMaximized = false; // whether we've already maximized the terminal panel this session
let agentFirstActive = false; // sticky for this activation; env writes must not restore the launcher

// AGENT_FIRST_CONTEXT_KEY gates the legacy launcher's own manifest
// contributions (docs/spec-welcome-mode.md §1.2): the activity-bar Agents
// view's `when` clause is `!zcpAgentFirst`, so it renders only under legacy
// policy or after an app.zerops.io suppress falls back — never alongside the
// agent-first receiver/panel (two launch surfaces must never render at once).
const AGENT_FIRST_CONTEXT_KEY = "zcpAgentFirst";
function setLegacySurfacesHidden(hidden) {
  vscode.commands.executeCommand("setContext", AGENT_FIRST_CONTEXT_KEY, hidden).then(undefined, () => {});
}

function openLauncher(view) {
  if (currentPanel) { try { currentPanel.dispose(); } catch (_) {} currentPanel = null; }
  const panel = vscode.window.createWebviewPanel("zcpLauncher", "ZCP Launcher", vscode.ViewColumn.One, { enableScripts: true, retainContextWhenHidden: false });
  currentPanel = panel;
  panel.onDidDispose(() => { if (currentPanel === panel) currentPanel = null; });
  panel.webview.html = renderLauncherHtml(view.agents, makeNonce());
  panel.webview.onDidReceiveMessage((msg) => {
    // Revalidate against a FRESH read: `view` is a snapshot from render time,
    // and an authorize/removal/uninstall can land between then and the click.
    const action = pickAction(buildView(readZembedEnv()), msg);
    if (!action) return;
    console.log("[zcp-bootstrap] launch agent=" + action.agent.id + " mode=" + action.mode);
    panel.dispose();
    runAgentAction(action.agent, action.mode);
  });
  lastShownKey = viewKey(view);
}

function showInitial() {
  const hasEditors = vscode.window.tabGroups.all.some((g) => g.tabs.length > 0);
  const view = buildView(readZembedEnv());
  if (hasEditors) {
    console.log("[zcp-bootstrap] editors open → skip initial open");
    lastShownKey = viewKey(view); // baseline so an unrelated env change won't pop later
    return;
  }
  console.log("[zcp-bootstrap] initial launcher: " + viewKey(view));
  openLauncher(view); // always — even an explicitly empty set is an honest state to show
}

// fallBackToLegacyLauncher runs when the receiver webview reports (via
// welcome.js's welcome-suppress message) that it is embedded in the production
// Zerops dashboard (app.zerops.io), where agent-first onboarding is not wired
// up yet. Agent-first mode opened the receiver INSTEAD of the launcher, so a
// bare dispose would leave the editor blank; restore the pre-onboarding
// behavior by dropping agent-first mode (so later env changes drive the
// launcher again), revealing the now-unhidden legacy surfaces, and opening the
// legacy agent launcher. See docs/spec-welcome-mode.md §1.2.
function fallBackToLegacyLauncher() {
  console.log("[zcp-bootstrap] receiver suppressed on app.zerops.io → legacy launcher");
  agentFirstActive = false;
  setLegacySurfacesHidden(false);
  openLauncher(buildView(readZembedEnv()));
  // Agent-first activation kept Explorer visible already (§5.3), but reassert
  // it here too: the pre-onboarding launcher expects the file browser open.
  vscode.commands.executeCommand("workbench.view.explorer").then(undefined, () => {});
}

// onEnvChange fires on any zembed env.json write. It reopens the launcher ONLY
// when the view signature actually changed (see viewKey).
function onEnvChange() {
  if (agentFirstActive) {
    console.log("[zcp-bootstrap] env.json changed in agent-first mode → legacy launcher stays suppressed");
    return;
  }
  const env = readZembedEnv();
  if (env === null) { console.log("[zcp-bootstrap] env.json unreadable (transient) → ignore"); return; }
  const view = buildView(env);
  const key = viewKey(view);
  if (key === lastShownKey) {
    console.log("[zcp-bootstrap] env.json changed; view unchanged (" + key + ") → ignore");
    return;
  }
  console.log("[zcp-bootstrap] live change → view now: " + key);
  openLauncher(view);
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

// ---- activity-bar launcher view --------------------------------------------
// The Zerops-logo activity-bar icon opens this sidebar view with the agent
// cards — so the user can launch (or check install/auth status of) an agent
// any time WITHOUT disturbing the editor area or anything on the right.
// Clicking an action runs the agent (panel terminal / plugin). The
// editor-tab launcher (startup / reload / live change) is unchanged; this is
// the on-demand entry point that leaves the rest as-is. (An activity-bar icon
// can only open a sidebar view — it can't open the editor-tab webview without
// switching the side bar — so the cards live here too.)

const VIEW_ID = "zcpAgents";

const agentsViewProvider = {
  resolveWebviewView(view) {
    view.webview.options = { enableScripts: true };
    const render = () => { view.webview.html = renderLauncherHtml(buildView(readZembedEnv()).agents, makeNonce()); };
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

  // The agent panel / receiver stays lazy in every mode. Agent-first mode
  // invokes this same command after registration, on every window init. No
  // top-level require here: require("./welcome.js") happens ONLY inside the
  // handler below, so a broken welcome.js can never break activation or the
  // launcher above it. See docs/spec-welcome-mode.md §1 (W-ENTRY) / §1.4.
  //
  // opts is undefined for a real manual invocation (Command Palette, "Zerops:
  // Open Panel") — welcome.js's open() treats that as manual (self-close
  // exempt, §1.4). The boot-always call below is the ONLY caller that passes
  // { manual: false, hadRestoredEditors }, opting into the §1.3 receiver
  // lifecycle instead.
  ctx.subscriptions.push(vscode.commands.registerCommand("zerops.panel", (opts) => {
    try {
      require("./welcome.js").open(ctx, {
        REGISTRY, ALL_AGENT_IDS,
        readZembedEnv, runAgentAction,
        resolveAvailableAgentIds, isAgentInstalled,
        onSuppressed: fallBackToLegacyLauncher,
      }, opts);
    } catch (err) {
      console.error("[zcp-bootstrap] panel failed to open:", err);
      vscode.window.showErrorMessage("Zerops: Open Panel failed to open (see Extension Host output for details).");
    }
  }));

  agentFirstActive = hasAgentFirstPolicy();
  if (agentFirstActive) {
    // Only flip the context key when agent-first is actually active — the
    // legacy path below must run zero startup commands, exactly as before
    // this concept existed (the key stays unset, and an unset context key
    // reads as falsy for the `!zcpAgentFirst` manifest `when` clause too).
    setLegacySurfacesHidden(true);
    // hadRestoredEditors is captured BEFORE the receiver panel exists — the
    // receiver tab itself must never count toward it (§1.3).
    const hadRestoredEditors = vscode.window.tabGroups.all.some((g) => g.tabs.length > 0);
    await vscode.commands.executeCommand("zerops.panel", { manual: false, hadRestoredEditors });
  } else {
    showInitial();
  }
  startEnvWatcher(ctx);
}

function deactivate() {}

module.exports = { activate, deactivate, resolveAvailableAgentIds, isAgentInstalled, viewKey };
