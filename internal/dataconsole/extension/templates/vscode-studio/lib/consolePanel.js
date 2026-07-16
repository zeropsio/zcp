"use strict";

const fs = require("fs");
const path = require("path");

// The Data Console as a first-party VS Code WebviewPanel. The SPA IS the webview
// content (loaded as webview resources, not framed from a public URL), so the
// code-server service worker serves it natively and there is no /dcproxy/, no
// cross-origin iframe, no frame-ancestors. Data flows webview -> host broker ->
// loopback console; the bearer stays host-side.

// ASSETS are index.html's relative refs, rewritten to webview resource URIs.
const ASSETS = ["style.css", "contract.js", "dc-format.js", "dc-actions.js", "dc-rows.js", "dc-errors.js", "dc-embed.js", "app.js"];

// buildHtml rewrites the materialized index.html's asset refs to webview URIs and
// injects the webview CSP. default-src 'none' + NO connect-src is load-bearing
// security: the webview physically cannot fetch the console; all data is brokered
// host-side. img-src blob:/data: keeps inline object-URL previews working.
function buildHtml(vscode, webview, mediaDir, readFile) {
  const read = readFile || function (p) { return fs.readFileSync(p, "utf8"); };
  let html = read(path.join(mediaDir, "index.html"));
  for (const name of ASSETS) {
    const uri = webview.asWebviewUri(vscode.Uri.file(path.join(mediaDir, name))).toString();
    html = html.replace('href="' + name + '"', 'href="' + uri + '"');
    html = html.replace('src="' + name + '"', 'src="' + uri + '"');
  }
  const csp =
    "default-src 'none'; " +
    "img-src " + webview.cspSource + " blob: data:; " +
    "style-src " + webview.cspSource + " 'unsafe-inline'; " +
    "script-src " + webview.cspSource + "; " +
    "font-src " + webview.cspSource + ";";
  return html.replace("<head>", '<head>\n  <meta http-equiv="Content-Security-Policy" content="' + csp + '">');
}

// hostDownload reads a blob through the broker and saves it with a native dialog
// — bytes are written host-side and never re-enter the webview (which blocks
// <a download>).
function hostDownload(vscode, broker, msg) {
  const q = "service=" + encodeURIComponent(msg.service) + "&segs=" + encodeURIComponent(JSON.stringify(msg.segs || []));
  return Promise.resolve(broker.request({ method: "GET", path: "/api/blob?" + q }))
    .then(function (res) {
      if (!res || !res.ok) return null;
      return Promise.resolve(vscode.window.showSaveDialog({ defaultUri: vscode.Uri.file(msg.name || "object") }))
        .then(function (uri) {
          if (uri) return vscode.workspace.fs.writeFile(uri, Buffer.from(res.bytes || []));
        });
    })
    .catch(function () { /* cancelled / write failed */ });
}

// hostUpload picks a file with a native dialog and writes it through the broker
// (PUT /api/blob with base64) — File/FormData cannot stream through postMessage.
function hostUpload(vscode, broker, webview, msg) {
  return Promise.resolve(vscode.window.showOpenDialog({ canSelectMany: false }))
    .then(function (uris) {
      if (!uris || !uris[0]) return null;
      const uri = uris[0];
      return Promise.resolve(vscode.workspace.fs.readFile(uri)).then(function (data) {
        const buf = Buffer.from(data);
        const name = String(uri.fsPath || "file").split(/[\\/]/).pop();
        const segs = (msg.segs || []).concat(name);
        return Promise.resolve(broker.request({
          method: "PUT", path: "/api/blob", confirm: true,
          body: JSON.stringify({ path: { service: msg.service, segments: segs }, data: buf.toString("base64") }),
        })).then(function (res) {
          webview.postMessage({ type: "dataconsole-uploaded", ok: !!(res && res.ok), service: msg.service });
        });
      });
    })
    .catch(function () { webview.postMessage({ type: "dataconsole-uploaded", ok: false, service: msg.service }); });
}

// setWriteMode is the in-panel write toggle. Enabling requires a host
// confirmation (entry.confirmWrites — a VS Code modal); the broker then forwards
// mutations. Disabling is immediate. A webview message alone never grants write:
// the host dialog + user consent is the gate, the broker is the enforcement.
function setWriteMode(entry, webview, enable) {
  function apply(ok) {
    entry.broker.setWriteEnabled(ok);
    webview.postMessage({ type: "dataconsole-write-mode", writeEnabled: entry.broker.isWriteEnabled() });
  }
  if (!enable) {
    apply(false);
    return Promise.resolve();
  }
  return Promise.resolve(entry.confirmWrites ? entry.confirmWrites() : true)
    .then(function (confirmed) { apply(!!confirmed); })
    .catch(function () { apply(false); });
}

function createConsolePanelManager(deps) {
  deps = deps || {};
  const vscode = deps.vscode || require("vscode");
  const readFile = deps.readFile;
  const panels = {}; // key -> { panel, broker, service, allowWrites, disposed }

  function column() {
    return (vscode.ViewColumn && vscode.ViewColumn.One) || 1;
  }

  // show creates or reveals the console panel for a process key. The broker is
  // (re)bound each call so a restarted console process is picked up. A same-key
  // reveal with a new service deep-links via a host->webview switch message
  // (no reload — the SPA listens for it).
  function show(key, opts) {
    opts = opts || {};
    const mediaDir = opts.mediaDir;
    const onDispose = opts.onDispose || function () {};

    let entry = panels[key];
    if (entry && entry.panel && !entry.disposed) {
      entry.broker = opts.broker;
      entry.service = opts.service || "";
      entry.confirmWrites = opts.confirmWrites;
      if (typeof entry.panel.reveal === "function") entry.panel.reveal(column());
      if (entry.service) entry.panel.webview.postMessage({ type: "dataconsole-switch-service", service: entry.service });
      return entry.panel;
    }

    const panel = vscode.window.createWebviewPanel(
      "zcpDataConsole",
      "Data Console",
      column(),
      { enableScripts: true, retainContextWhenHidden: true, localResourceRoots: [vscode.Uri.file(mediaDir)] }
    );
    entry = { panel: panel, broker: opts.broker, service: opts.service || "", confirmWrites: opts.confirmWrites, disposed: false };
    panels[key] = entry;

    panel.webview.html = buildHtml(vscode, panel.webview, mediaDir, readFile);

    panel.webview.onDidReceiveMessage(function (msg) {
      msg = msg || {};
      if (msg.type === "dc-ready") {
        panel.webview.postMessage({ type: "dataconsole-init", service: entry.service, writeEnabled: entry.broker.isWriteEnabled() });
        return;
      }
      if (msg.type === "dc-write-mode") {
        return setWriteMode(entry, panel.webview, !!msg.enable);
      }
      if (msg.type === "dc-rpc") {
        return Promise.resolve(entry.broker.request({
          method: msg.method, path: msg.path, body: msg.body, confirm: msg.confirm, upload: msg.upload,
        }))
          .then(function (res) {
            panel.webview.postMessage({
              type: "dc-rpc-result", id: msg.id,
              status: res.status, ok: res.ok, headers: res.headers || {},
              b64: Buffer.from(res.bytes || []).toString("base64"),
            });
          })
          .catch(function () {
            panel.webview.postMessage({ type: "dc-rpc-result", id: msg.id, status: 500, ok: false, headers: {}, b64: "" });
          });
      }
      if (msg.type === "dc-download") {
        return hostDownload(vscode, entry.broker, msg);
      }
      if (msg.type === "dc-upload") {
        return hostUpload(vscode, entry.broker, panel.webview, msg);
      }
    });

    panel.onDidDispose(function () {
      entry.disposed = true;
      delete panels[key];
      try { onDispose(); } catch (_) { /* best effort */ }
    });

    return panel;
  }

  function disposeAll() {
    Object.keys(panels).forEach(function (k) {
      const e = panels[k];
      if (e && e.panel && typeof e.panel.dispose === "function") {
        try { e.panel.dispose(); } catch (_) { /* already gone */ }
      }
    });
  }

  return { show: show, disposeAll: disposeAll };
}

module.exports = { createConsolePanelManager: createConsolePanelManager, buildHtml: buildHtml, ASSETS: ASSETS };
