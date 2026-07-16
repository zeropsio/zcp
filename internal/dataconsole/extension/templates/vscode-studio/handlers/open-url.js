"use strict";

// open-url handler (type "openUrl") — opens an arbitrary https URL in the user's
// real browser. The Runtime card uses it for a service's live subdomain link.
//
// Why a handler and not a bare <a href>: a webview anchor's external-navigation
// behaviour is reliable on desktop VS Code but NOT in code-server (the in-container
// editor), where the proxy/cookie context can swallow it. asExternalUri maps the
// URL into one the USER's browser can actually reach, then openExternal hands it
// off — the same proven path the Data Console open uses. Public https subdomains
// pass through asExternalUri unchanged; the call is harmless and future-proof.
//
// Only https/http URLs are honoured (defence against a malformed data-url); vscode
// is required LAZILY so the module loads cleanly under plain node in tests.

module.exports = {
  type: "openUrl",
  handle: async function handle(msg, ctx) {
    const raw = msg && msg.url ? String(msg.url) : "";
    if (!/^https?:\/\//i.test(raw)) return;
    const vscode = require("vscode");
    let target = raw;
    try {
      const ext = await vscode.env.asExternalUri(vscode.Uri.parse(raw));
      target = ext.toString();
    } catch (_) {
      /* fall back to the raw URL */
    }
    await vscode.env.openExternal(vscode.Uri.parse(target));
  },
};
