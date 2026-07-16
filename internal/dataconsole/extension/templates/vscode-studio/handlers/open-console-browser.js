"use strict";

// open-console-browser handler (type "openConsoleBrowser"). Opens the Data Console
// STANDALONE in the user's real browser — as opposed to "openConsole", which opens
// it EMBEDDED in a VS Code WebviewPanel. It reuses the running console session (the
// shared session manager → never spawns a second competing process) and hands the
// browser ONLY the read bearer, through the URL fragment. The standalone SPA is
// view-only BY DESIGN: it never receives the per-request write token, so every
// mutation 403s server-side. That write token stays host-side, used only by the
// embedded broker.
//
// asExternalUri maps the loopback URL into one the USER's browser can actually
// reach: on desktop it passes through unchanged; under code-server it becomes the
// authenticated /proxy/<port>/ URL. The trailing slash BEFORE the '#fragment' is
// load-bearing — the SPA's document-relative asset + /api URLs must resolve under
// the /proxy/<port>/ prefix (without it they escape the proxy path). vscode is
// required lazily (inside the thin handle wrapper) so this module loads under plain
// node for the pure-logic tests.

const sessionManager = require("../lib/consoleSessionSingleton");

// buildStandaloneURL composes the loopback URL the browser opens. Two invariants it
// exists to hold: (1) a trailing slash BEFORE the fragment (code-server proxy-prefix
// correctness) and (2) ONLY the read bearer in the fragment — never a write token
// (standalone is view-only). Pure + exported so both are pinned by test.
function buildStandaloneURL(port, sessionToken, service) {
  const svc = service ? "&svc=" + encodeURIComponent(service) : "";
  return "http://127.0.0.1:" + port + "/#t=" + encodeURIComponent(sessionToken) + svc;
}

// openInBrowser is the testable core (deps injected): ensure the shared session is
// running, build the bearer-only standalone URL, map it for the user's browser via
// asExternalUri, and hand it off with openExternal. Returns the opened URL (or null
// if the console could not be reached). Never touches the write token.
async function openInBrowser(mgr, vscode, msg, ctx) {
  const ep = await mgr.endpoint({
    workspaceRoot: ctx.workspaceRoot,
    postMessage: ctx.postMessage,
  });
  if (!ep || !ep.port || !ep.sessionToken) return null;
  const raw = buildStandaloneURL(ep.port, ep.sessionToken, msg.service);
  let target = raw;
  try {
    const ext = await vscode.env.asExternalUri(vscode.Uri.parse(raw));
    target = ext.toString();
  } catch (_) {
    /* fall back to the raw loopback URL — desktop VS Code reaches localhost directly */
  }
  await vscode.env.openExternal(vscode.Uri.parse(target));
  return target;
}

async function handle(msg, ctx) {
  return openInBrowser(sessionManager(), require("vscode"), msg || {}, ctx || {});
}

module.exports = {
  type: "openConsoleBrowser",
  handle: handle,
  openInBrowser: openInBrowser,
  buildStandaloneURL: buildStandaloneURL,
};
