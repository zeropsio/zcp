"use strict";

// open-welcome handler — S5 (L-BR-3: no auto-open into the workspace).
//
// Opens the Zerops docs EXTERNALLY (the user's browser) — it never reveals an
// editor, walkthrough, or webview inside the workspace, so it can't violate the
// no-auto-open floor even when invoked. It runs only because the user clicked
// the "Zerops docs" button in the welcome card.
//
// vscode is required LAZILY inside handle() so the module loads cleanly under
// plain node in tests (the router require()s every handler at activation).

const DOCS_URL = "https://docs.zerops.io";

module.exports = {
  type: "open-welcome",
  handle: async function handle(msg, ctx) {
    const vscode = require("vscode");
    await vscode.env.openExternal(vscode.Uri.parse(DOCS_URL));
  },
};
