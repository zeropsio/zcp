"use strict";

// Data Console handler (type "openConsole"). Opens the console EMBEDDED in a VS
// Code WebviewPanel. The child is always spawned write-capable; write mode is a
// runtime toggle in the panel (host-confirmed), and the server-side per-request
// write token is the mutation boundary — a webview message alone never grants write
// authority. Shares the ONE session manager with the standalone browser opener, so
// the two openers reuse a single per-workspace console process.

const sessionManager = require("../lib/consoleSessionSingleton");

function handle(msg, ctx) {
  msg = msg || {};
  ctx = ctx || {};
  return sessionManager().open({
    workspaceRoot: ctx.workspaceRoot,
    extensionPath: ctx.extensionPath,
    service: msg.service || "",
    postMessage: ctx.postMessage,
  });
}

// dispose is exported so the extension can kill console children on deactivate
// (the panel's own onDidDispose kills its child too — this covers editor exit).
module.exports = { type: "openConsole", handle: handle, dispose: function () { sessionManager.disposeShared(); } };
