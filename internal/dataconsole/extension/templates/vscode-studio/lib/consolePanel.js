"use strict";

const fs = require("fs");
const path = require("path");

// The Data Console as a first-party VS Code WebviewPanel. The SPA IS the webview
// content (loaded as webview resources, not framed from a public URL), so the
// code-server service worker serves it natively and there is no /dcproxy/, no
// cross-origin iframe, no frame-ancestors. Data flows webview -> host broker ->
// loopback console; the bearer stays host-side.

// index.html — not this file — owns which local assets it needs: buildHtml
// PARSES the <script src="...">/<link href="..."> refs out of the actual
// materialized markup instead of hand-listing them, so a new SPA file wired in
// via index.html is picked up automatically.
const SCRIPT_SRC_RE = /<script\b[^>]*\bsrc="([^"]+)"[^>]*>/gi;
const LINK_HREF_RE = /<link\b[^>]*\bhref="([^"]+)"[^>]*>/gi;

// hasScheme reports whether ref is an absolute URL (http:, data:, vscode-resource:,
// ...) rather than a same-directory local asset name.
function hasScheme(ref) {
  return /^[a-z][a-z0-9+.-]*:/i.test(ref);
}

// isLocalRef reports whether ref is safe to resolve under mediaDir: no scheme, not
// root-absolute, and no ".." segment that would escape mediaDir. index.html is
// Go-materialized/trusted content, but buildHtml refuses to guess at anything it
// cannot safely turn into a webview resource URI rather than silently mis-resolving it.
function isLocalRef(ref) {
  if (hasScheme(ref) || ref.charAt(0) === "/") return false;
  return !ref.split("/").includes("..");
}

// assetRefs returns the ordered, de-duplicated <script src>/<link href> refs
// found in html.
function assetRefs(html) {
  const seen = new Set();
  const out = [];
  for (const re of [SCRIPT_SRC_RE, LINK_HREF_RE]) {
    re.lastIndex = 0;
    let m;
    while ((m = re.exec(html)) !== null) {
      if (!seen.has(m[1])) {
        seen.add(m[1]);
        out.push(m[1]);
      }
    }
  }
  return out;
}

// buildHtml rewrites the materialized index.html's asset refs to webview URIs and
// injects the webview CSP. default-src 'none' + NO connect-src is load-bearing
// security: the webview physically cannot fetch the console; all data is brokered
// host-side. img-src blob:/data: keeps inline object-URL previews working.
//
// Every local ref discovered in the source markup is rewritten; anything left
// relative afterwards (a ref buildHtml refused as unsafe, or one that somehow
// escaped the rewrite loop) FAILS LOUD rather than shipping a webview that can
// silently fail to load a script/stylesheet.
function buildHtml(vscode, webview, mediaDir, readFile) {
  const read = readFile || function (p) { return fs.readFileSync(p, "utf8"); };
  let html = read(path.join(mediaDir, "index.html"));
  for (const ref of assetRefs(html)) {
    if (!isLocalRef(ref)) continue; // left as-is; caught by the unrewritten check below
    const uri = webview.asWebviewUri(vscode.Uri.file(path.join(mediaDir, ref))).toString();
    html = html.split('href="' + ref + '"').join('href="' + uri + '"');
    html = html.split('src="' + ref + '"').join('src="' + uri + '"');
  }
  const csp =
    "default-src 'none'; " +
    "img-src " + webview.cspSource + " blob: data:; " +
    "style-src " + webview.cspSource + " 'unsafe-inline'; " +
    "script-src " + webview.cspSource + "; " +
    "font-src " + webview.cspSource + ";";
  html = html.replace("<head>", '<head>\n  <meta http-equiv="Content-Security-Policy" content="' + csp + '">');

  const unrewritten = assetRefs(html).filter(function (ref) { return !hasScheme(ref); });
  if (unrewritten.length > 0) {
    throw new Error(
      "consolePanel.buildHtml: unrewritten relative asset ref(s) in " +
        path.join(mediaDir, "index.html") + ": " + unrewritten.join(", ")
    );
  }
  return html;
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

// setWriteMode is the in-panel write toggle. Enabling requires a host confirmation
// (entry.confirmWrites — a VS Code modal); on approval the broker starts attaching
// the per-request write token, and the SERVER (which checks that token) is the
// mutation boundary. Disabling is immediate. Fail-closed: an ABSENT confirmWrites
// callback is treated as NOT approved (writes stay off), never as implicit consent —
// so a missing gate can never silently enable writes.
function setWriteMode(entry, webview, enable) {
  function apply(ok) {
    entry.broker.setWriteEnabled(ok);
    webview.postMessage({ type: "dataconsole-write-mode", writeEnabled: entry.broker.isWriteEnabled() });
  }
  if (!enable) {
    apply(false);
    return Promise.resolve();
  }
  return Promise.resolve(entry.confirmWrites ? entry.confirmWrites() : false)
    .then(function (confirmed) { apply(!!confirmed); })
    .catch(function () { apply(false); });
}

function createConsolePanelManager(deps) {
  deps = deps || {};
  const vscode = deps.vscode || require("vscode");
  const readFile = deps.readFile;
  const panels = {}; // key -> { panel, broker, service, confirmWrites, disposed }

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
      // A reveal rebinds a FRESH broker (open() always makeClient()s a new one for
      // the reused console session). Carry the host-confirmed write-enabled state
      // from the outgoing broker onto it — the console session (and its write token)
      // is the same, so the new broker inherits the same authority. Without this the
      // webview keeps its green write toggle (retainContextWhenHidden) while the new
      // broker silently reverts to read-only, so every mutation after a re-browse
      // fails with "write mode is off" (the two diverge).
      if (entry.broker && typeof entry.broker.isWriteEnabled === "function" && opts.broker) {
        opts.broker.setWriteEnabled(entry.broker.isWriteEnabled());
      }
      entry.broker = opts.broker;
      entry.service = opts.service || "";
      entry.confirmWrites = opts.confirmWrites;
      if (typeof entry.panel.reveal === "function") entry.panel.reveal(column());
      if (entry.service) entry.panel.webview.postMessage({ type: "dataconsole-switch-service", service: entry.service });
      // Re-sync the SPA's write toggle to the (rebound) broker so they can never
      // diverge on a reveal — authoritative flag from the host, not the stale webview.
      entry.panel.webview.postMessage({ type: "dataconsole-write-mode", writeEnabled: entry.broker.isWriteEnabled() });
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

module.exports = { createConsolePanelManager: createConsolePanelManager, buildHtml: buildHtml };
