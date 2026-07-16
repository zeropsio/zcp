"use strict";

// Data Console handler (type "openConsole"). Read-only is the default launch
// posture. Write-capable console sessions are launch-time, immutable, and require
// a host-owned VS Code confirmation before the child process receives
// --allow-writes; a webview message alone never grants write authority.

const { createConsoleSessionManager } = require("../lib/consoleSession");

const mgr = createConsoleSessionManager();

function handle(msg, ctx) {
  msg = msg || {};
  ctx = ctx || {};
  return mgr.open({
    workspaceRoot: ctx.workspaceRoot,
    extensionPath: ctx.extensionPath,
    service: msg.service || "",
    allowWrites: !!(msg && msg.allowWrites),
    postMessage: ctx.postMessage,
  });
}

// dispose is exported so the extension can kill console children on deactivate
// (the panel's own onDidDispose kills its child too — this covers editor exit).
module.exports = { type: "openConsole", handle: handle, dispose: function () { mgr.dispose(); } };
