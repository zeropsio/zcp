"use strict";

const cp = require("child_process");
const { discoverToUIMap } = require("./discoverToUIMap");
const { runStudioVerb: defaultRunStudioVerb } = require("./transport");

const DEFAULT_BIN = "zcp";

function makeNonce() {
  return Date.now().toString(36) + Math.random().toString(36).slice(2, 12);
}

function escapeHtml(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, function (c) {
    return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
  });
}

function appendLine(outputChannel, line) {
  if (outputChannel && typeof outputChannel.appendLine === "function") {
    outputChannel.appendLine(line);
  }
}

function errorMessage(err) {
  if (!err) return "unknown error";
  return err && err.message ? err.message : String(err);
}

function logError(outputChannel, operation, err) {
  const message = errorMessage(err);
  appendLine(outputChannel, operation + " failed: " + message);
  if (err && err.stack) {
    appendLine(outputChannel, err.stack);
  }
}

function logResult(outputChannel, operation, result) {
  if (!result || result.ok) return;
  let line = operation + " failed: " + (result.error || "unknown error");
  if (result.timeout) line += " [timeout]";
  if (result.cancelled) line += " [cancelled]";
  if (result.needsInit) line += " [needsInit]";
  appendLine(outputChannel, line);
  if (result.cause && result.cause.stack) {
    appendLine(outputChannel, result.cause.stack);
  }
}

function createAbortController() {
  if (typeof AbortController === "function") {
    return new AbortController();
  }
  const listeners = [];
  const signal = {
    aborted: false,
    addEventListener: function (name, fn) {
      if (name === "abort" && typeof fn === "function") listeners.push(fn);
    },
    removeEventListener: function (name, fn) {
      if (name !== "abort") return;
      const i = listeners.indexOf(fn);
      if (i >= 0) listeners.splice(i, 1);
    },
  };
  return {
    signal: signal,
    abort: function () {
      if (signal.aborted) return;
      signal.aborted = true;
      listeners.slice().forEach(function (fn) {
        fn();
      });
      if (typeof signal.onabort === "function") signal.onabort();
    },
  };
}

// The Zerops hexagon mark - brand-fixed teal, rendered in the shell header.
// The activity-bar icon is a separate monochrome media/data.svg that VS Code masks.
var ZS_LOGO =
  '<svg class="zs-logo" viewBox="0 0 237 284" aria-hidden="true">' +
  '<path d="M110.596 1.457 14.238 38.285A22.422 22.422 0 0 0 0 59.194v92.714l44.283-25.449v-52.13L118.5 45.852V0c-2.701.006-5.379.5-7.904 1.457ZM45.068 209.084l73.432-42.321v-51.122L5.045 181.057A10.2 10.2 0 0 0 0 189.802v34.249a22.42 22.42 0 0 0 14.238 20.684l96.358 36.828a22.4 22.4 0 0 0 7.904 1.457v-45.852l-73.432-28.084Z" fill="#3DB1A2"/>' +
  '<path d="M232.291 101.066a9.36 9.36 0 0 0 4.709-8.24V59.194a22.43 22.43 0 0 0-3.889-12.654 22.43 22.43 0 0 0-10.349-8.255L126.348 1.457A22.425 22.425 0 0 0 118.5 0v45.853l72.871 28.027-72.871 41.985v51.122l113.791-65.921ZM126.348 281.563l96.414-36.828A22.426 22.426 0 0 0 237 224.051v-93.667l-44.284 25.561v52.859L118.5 237.168v45.852a22.418 22.418 0 0 0 7.848-1.457Z" fill="#24A492"/>' +
  "</svg>";

function shellCss() {
  return [
    ":root{",
    "--zt:#2fb3a3;--zt2:#24a492;--ztb:#3dd6c2;--ztdim:rgba(47,179,163,.13);--ztring:rgba(47,179,163,.5);",
    "--fg:var(--vscode-foreground,#cccccc);--fgm:var(--vscode-descriptionForeground,#9d9d9d);--fgd:#6f6f6f;--fgh:#fff;",
    "--bg:var(--vscode-sideBar-background,var(--vscode-editor-background,#1e1e1e));",
    "--card:var(--vscode-editorWidget-background,#252527);--bd:var(--vscode-panel-border,#2b2b2b);--bdstr:#454547;",
    "--mono:var(--vscode-editor-font-family,'SF Mono',Menlo,Consolas,monospace);}",
    "*{box-sizing:border-box}",
    "body{font-family:var(--vscode-font-family,-apple-system,'Segoe UI',system-ui,sans-serif);color:var(--fg);background:var(--bg);margin:0;font-size:13px;-webkit-font-smoothing:antialiased;}",
    "code{font-family:var(--mono);font-size:11.5px;background:rgba(140,140,140,.18);padding:1px 5px;border-radius:4px;}",
    ".zs-head{display:flex;align-items:center;gap:11px;padding:14px 15px 13px;border-bottom:1px solid var(--bd);}",
    ".zs-logo{width:22px;height:26px;flex:0 0 auto;filter:drop-shadow(0 2px 6px rgba(36,164,146,.4));}",
    ".zs-brand{font-size:14px;font-weight:600;color:var(--fgh);letter-spacing:-.2px;line-height:1.12;}",
    ".zs-exp{display:inline-block;margin-left:7px;padding:1px 6px;border-radius:8px;font-size:9.5px;font-weight:600;text-transform:uppercase;letter-spacing:.5px;color:var(--fgm);background:rgba(140,140,140,.14);border:1px solid var(--bd);vertical-align:middle;}",
    ".zs-proj{font-size:11.5px;color:var(--fgm);margin-top:1px;}",
    ".zs-main{padding:15px 15px 30px;display:flex;flex-direction:column;gap:20px;}",
    ".zs-card h2{font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:.7px;color:var(--fgm);margin:0 0 10px;}",
    ".zs-cardhead{display:flex;align-items:center;justify-content:space-between;gap:8px;margin:0 0 6px;}",
    ".zs-cardhead h2{margin:0;}",
    ".zs-muted{color:var(--fgm);font-size:12px;line-height:1.5;}",
    ".zs-list{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:7px;}",
    // Rows are VERTICAL cards (not a single horizontal flex line): the panel
    // is a ~280-400px sidebar, where a one-line row forced the hostname, type,
    // badge, and action button to fight for the same horizontal space — they
    // wrapped mid-word and overlapped. Stacking into labelled lines keeps
    // every element on its own row and lets long text ellipsis or wrap
    // cleanly within the card. Head = the title block (host over type) +
    // status badge; then the actions line; then the transient status line.
    ".zs-row{display:flex;flex-direction:column;align-items:stretch;gap:7px;padding:10px 12px;border-radius:8px;background:var(--card);border:1px solid var(--bd);transition:border-color .12s;min-width:0;}",
    ".zs-row:hover{border-color:var(--bdstr);}",
    // wrap is the overflow safety valve at extreme widths: .zs-rowmain (host/type)
    // is the one item designed to shrink/ellipsis; the icon and pills (below)
    // hold their content size and fall to a second line rather than being
    // crushed below legibility.
    ".zs-rowhead{display:flex;flex-wrap:wrap;align-items:flex-start;gap:8px;min-width:0;}",
    ".zs-rowmain{min-width:0;flex:1;display:flex;flex-direction:column;gap:2px;}",
    ".zs-host{display:block;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12.5px;font-weight:600;color:var(--fgh);}",
    ".zs-svc-type{display:block;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:11.5px;line-height:1.3;color:var(--fgm);}",
    // flex-shrink:0 (matches .zs-tier below): a status word ("ACTIVE"/"RUNNING"/…)
    // is short, bounded vocabulary — it must never be crushed below legibility.
    // At a width too narrow for icon+pills+rowmain on one line, .zs-rowhead's
    // wrap sends this to its own line instead; overflow/ellipsis stay only as a
    // guard against a pathologically long status value.
    ".zs-badge{font-size:10px;font-weight:600;padding:2px 7px;border-radius:10px;background:rgba(140,140,140,.16);color:var(--fgm);white-space:nowrap;flex:0 0 auto;min-width:0;max-width:48%;overflow:hidden;text-overflow:ellipsis;}",
    ".zs-badge.ok{background:var(--ztdim);color:var(--ztb);}",
    ".zs-tier{font-size:10px;font-weight:600;padding:2px 7px;border-radius:10px;white-space:nowrap;flex:0 0 auto;}",
    ".zs-tier-ready{background:rgba(107,208,127,.16);color:#6bd07f;}",
    ".zs-tier-view{background:rgba(220,180,90,.16);color:#dcb45a;}",
    ".zs-tier-not-yet{background:rgba(140,140,140,.16);color:var(--fgm);}",
    ".zs-rowact{display:flex;align-items:center;gap:8px;min-width:0;flex-wrap:wrap;}",
    ".zs-actbtns{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex:0 0 auto;margin-left:auto;max-width:100%;flex-wrap:wrap;}",
    ".zs-btn{display:inline-flex;align-items:center;gap:6px;padding:5px 12px;border-radius:7px;font-size:12px;font-weight:500;cursor:pointer;border:1px solid var(--bdstr);background:rgba(140,140,140,.10);color:var(--fg);font-family:inherit;white-space:nowrap;}",
    ".zs-btn:hover{border-color:var(--ztring);color:var(--ztb);}",
    ".zs-btn-sm{padding:4px 10px;font-size:11.5px;}",
    ".zs-preview{font-size:11.5px;color:var(--ztb);text-decoration:none;}",
    ".zs-preview:hover{text-decoration:underline;}",
    ".zs-tick{display:inline-flex;align-items:center;gap:7px;font-size:11px;color:var(--fgm);}",
    ".zs-dot{width:7px;height:7px;border-radius:50%;background:var(--zt);box-shadow:0 0 0 3px var(--ztdim);flex:0 0 auto;}",
    // Empty status lines collapse so an unused slot adds no gap.
    ".zs-status{display:block;font-size:11.5px;line-height:1.35;color:var(--fgm);overflow-wrap:anywhere;}",
    ".zs-status:empty{display:none;}",
    ".zs-row-link{cursor:pointer;}",
    ".zs-row-link:hover{border-color:var(--ztring);}",
    ".zs-svc-icon{width:23px;height:23px;flex:0 0 auto;display:inline-flex;align-items:center;justify-content:center;background:#fff;border-radius:5px;padding:3px;box-shadow:0 1px 2px rgba(0,0,0,.28);}",
    ".zs-svc-icon svg{max-width:100%;max-height:100%;width:auto;height:auto;display:block;}",
    ".zs-svc-icon-none{background:rgba(140,140,140,.14);box-shadow:none;}",
    ".zs-warn{color:#e0a23c;border-color:rgba(224,162,60,.42);}",
    ".zs-warn:hover{color:#f0b860;border-color:rgba(224,162,60,.7);}",
    ".zs-footer{background:transparent;border-color:transparent;padding-left:0;padding-right:0;}",
    ".zs-cta{padding:24px 16px;color:var(--fgm);line-height:1.55;}",
  ].join("\n");
}

function renderShell(uiMap, cards, nonce, outputChannel) {
  const projectName = (uiMap && uiMap.project && uiMap.project.name) || "";
  const body = cards
    .map(function (c) {
      try {
        return c.render(uiMap);
      } catch (err) {
        logError(outputChannel, "card " + (c && c.id ? c.id : "unknown") + " render", err);
        return "";
      }
    })
    .join("\n");
  const clientScripts = cards
    .map(function (c) {
      return typeof c.clientScript === "string" ? c.clientScript : "";
    })
    .join("\n");
  return (
    '<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">' +
    '<meta http-equiv="Content-Security-Policy" content="default-src \'none\'; style-src \'unsafe-inline\'; script-src \'nonce-' +
    nonce +
    "';\">" +
    "<style>" + shellCss() + "</style></head><body>" +
    '<header class="zs-head">' + ZS_LOGO +
    // &nbsp; glues "· Managed" together so a narrow-panel line break can only land
    // before the middle dot, never strand it alone at a line's end.
    '<div><div class="zs-brand">Zerops ·&nbsp;Managed Data<span class="zs-exp">experimental</span></div>' +
    '<div class="zs-proj">' + escapeHtml(projectName) + "</div></div></header>" +
    '<main class="zs-main">' + body + "</main>" +
    '<script nonce="' + nonce + '">' +
    "const vscodeApi=acquireVsCodeApi();" +
    'document.addEventListener("click",function(ev){' +
    'var el=ev.target.closest("[data-action]");if(!el)return;' +
    'var msg={type:el.getAttribute("data-action")};' +
    'for(var k in el.dataset){if(k!=="action")msg[k]=el.dataset[k];}' +
    "vscodeApi.postMessage(msg);});" +
    clientScripts +
    "</script></body></html>"
  );
}

function renderCTA(transport, nonce) {
  const msg = transport.needsInit
    ? "Run <code>zcp init</code> in this project first, then reload - Zerops Managed Data reads your project through ZCP."
    : "Could not read the project topology: " + escapeHtml(transport.error || "unknown error");
  return (
    '<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8">' +
    '<meta http-equiv="Content-Security-Policy" content="default-src \'none\'; style-src \'unsafe-inline\'; script-src \'nonce-' +
    nonce +
    "';\">" +
    "<style>" + shellCss() + "</style></head><body>" +
    '<header class="zs-head">' + ZS_LOGO +
    '<div class="zs-brand">Zerops ·&nbsp;Managed Data</div></header>' +
    '<div class="zs-cta"><p>' + msg + "</p></div>" +
    "</body></html>"
  );
}

async function runTransport(workspaceRoot, deps) {
  deps = deps || {};
  const runner = deps.runStudioVerb || defaultRunStudioVerb;
  const verbDeps = {};
  Object.keys(deps).forEach(function (key) {
    if (key !== "runStudioVerb") verbDeps[key] = deps[key];
  });
  const r = await runner(workspaceRoot, ["topology"], verbDeps);
  if (!r.ok) {
    return { ok: false, error: r.error, needsInit: r.needsInit, timeout: r.timeout, cancelled: r.cancelled, cause: r.cause };
  }
  if (!r.data) {
    return { ok: false, error: "empty topology output" };
  }
  try {
    return { ok: true, uiMap: discoverToUIMap(r.data) };
  } catch (err) {
    return { ok: false, error: errorMessage(err), cause: err };
  }
}

function createWebviewSession(options) {
  options = options || {};
  const view = options.view;
  const cards = options.cards || [];
  const router = options.router || { dispatch: async function () { return false; } };
  const workspaceRoot = options.workspaceRoot || "";
  const outputChannel = options.outputChannel;
  const runStudioVerb = options.runStudioVerb || defaultRunStudioVerb;
  const transportDeps = options.transportDeps || {};
  const spawn = options.spawn || cp.spawn;
  const bin = transportDeps.bin || DEFAULT_BIN;

  let disposed = false;
  let refreshInFlight = null;
  let watchProc = null;
  let watchDebounce = null;
  let respawnTimer = null;
  const controllers = new Set();

  function postMessage(m) {
    if (view && view.webview && typeof view.webview.postMessage === "function") {
      return view.webview.postMessage(m);
    }
    return false;
  }

  async function runVerb(args) {
    if (disposed) {
      return { ok: false, stdout: "", error: "webview session disposed", cancelled: true };
    }
    const controller = createAbortController();
    controllers.add(controller);
    const deps = {};
    Object.keys(transportDeps).forEach(function (key) {
      deps[key] = transportDeps[key];
    });
    deps.signal = controller.signal;
    try {
      const result = await runStudioVerb(workspaceRoot, args, deps);
      logResult(outputChannel, "zcp studio " + String((args && args[0]) || "verb"), result);
      return result;
    } catch (err) {
      logError(outputChannel, "zcp studio " + String((args && args[0]) || "verb"), err);
      return { ok: false, stdout: "", error: errorMessage(err), cause: err };
    } finally {
      controllers.delete(controller);
    }
  }

  async function readTopology() {
    if (typeof options.runTransport === "function") {
      try {
        const t = await options.runTransport();
        logResult(outputChannel, "zcp studio topology", t);
        return t;
      } catch (err) {
        logError(outputChannel, "zcp studio topology", err);
        return { ok: false, error: errorMessage(err), cause: err };
      }
    }

    const r = await runVerb(["topology"]);
    if (!r.ok) {
      return { ok: false, error: r.error, needsInit: r.needsInit, timeout: r.timeout, cancelled: r.cancelled, cause: r.cause };
    }
    if (!r.data) {
      const result = { ok: false, error: "empty topology output" };
      logResult(outputChannel, "zcp studio topology", result);
      return result;
    }
    try {
      return { ok: true, uiMap: discoverToUIMap(r.data) };
    } catch (err) {
      logError(outputChannel, "topology map", err);
      return { ok: false, error: errorMessage(err), cause: err };
    }
  }

  async function refreshTopology() {
    if (refreshInFlight) return refreshInFlight;
    refreshInFlight = (async function () {
      const nonce = makeNonce();
      const t = await readTopology();
      if (!disposed && view && view.webview) {
        view.webview.html = t.ok ? renderShell(t.uiMap, cards, nonce, outputChannel) : renderCTA(t, nonce);
      }
      return t;
    })();
    try {
      return await refreshInFlight;
    } finally {
      refreshInFlight = null;
    }
  }

  const handlerCtx = {
    workspaceRoot: workspaceRoot,
    extensionPath: options.extensionPath || "",
    refreshTopology: refreshTopology,
    postMessage: postMessage,
    runTransport: readTopology,
    runVerb: runVerb,
  };

  async function handleMessage(msg) {
    try {
      await router.dispatch(msg, handlerCtx);
    } catch (err) {
      logError(outputChannel, "handler " + String((msg && msg.type) || "unknown"), err);
    }
  }

  function startWatch() {
    if (options.watch === false || disposed) return;
    let proc;
    try {
      proc = spawn(bin, ["studio", "watch"], { cwd: workspaceRoot });
    } catch (err) {
      logError(outputChannel, "zcp studio watch", err);
      postMessage({ type: "watch-disconnected" });
      return;
    }
    watchProc = proc;
    let buf = "";
    if (proc.stdout && typeof proc.stdout.on === "function") {
      proc.stdout.on("data", function (chunk) {
        buf += chunk.toString();
        let nl;
        while ((nl = buf.indexOf("\n")) >= 0) {
          const line = buf.slice(0, nl).trim();
          buf = buf.slice(nl + 1);
          if (!line) continue;
          let ev;
          try {
            ev = JSON.parse(line);
          } catch (err) {
            logError(outputChannel, "zcp studio watch line parse", err);
            continue;
          }
          if (ev.type === "topology-changed") {
            clearTimeout(watchDebounce);
            watchDebounce = setTimeout(refreshTopology, 400);
          } else if (ev.type === "connected") {
            postMessage({ type: "watch-connected" });
          } else if (ev.type === "disconnected") {
            postMessage({ type: "watch-disconnected" });
          }
        }
      });
    }
    if (typeof proc.on === "function") {
      proc.on("error", function (err) {
        logError(outputChannel, "zcp studio watch", err);
        postMessage({ type: "watch-disconnected" });
      });
      proc.on("exit", function () {
        postMessage({ type: "watch-disconnected" });
        if (!disposed) {
          clearTimeout(respawnTimer);
          respawnTimer = setTimeout(startWatch, 5000);
        }
      });
    }
  }

  function dispose() {
    disposed = true;
    clearTimeout(watchDebounce);
    clearTimeout(respawnTimer);
    controllers.forEach(function (controller) {
      try {
        controller.abort();
      } catch (_) {
        /* already aborted */
      }
    });
    controllers.clear();
    if (watchProc && typeof watchProc.kill === "function") {
      try {
        watchProc.kill();
      } catch (_) {
        /* already gone */
      }
    }
    watchProc = null;
  }

  function start() {
    if (view && view.webview && typeof view.webview.onDidReceiveMessage === "function") {
      view.webview.onDidReceiveMessage(function (msg) {
        return handleMessage(msg);
      });
    }
    if (view && typeof view.onDidDispose === "function") {
      view.onDidDispose(dispose);
    }
    refreshTopology();
    startWatch();
  }

  return {
    start: start,
    dispose: dispose,
    refreshTopology: refreshTopology,
    handleMessage: handleMessage,
    runVerb: runVerb,
    runTransport: readTopology,
  };
}

module.exports = {
  createWebviewSession: createWebviewSession,
  runTransport: runTransport,
  renderShell: renderShell,
  renderCTA: renderCTA,
  makeNonce: makeNonce,
  shellCss: shellCss,
};
