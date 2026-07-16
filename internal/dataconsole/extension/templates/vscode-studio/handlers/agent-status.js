"use strict";

// agent-status handler — S4 (L-AG-1, NN6).
//
// Probes whether Claude Code is authorized on THIS machine by testing for the
// existence of ~/.claude/.credentials.json. This is the ONE allowed direct file
// read in the whole product — every other read goes through the E3 transport
// (`zcp studio <verb>`), never raw .zcp/state. We check existence only; we
// never open, parse, or forward the credentials file (no token ever enters
// Studio — authorization lives in Claude Code's own /login, L-AG-2).
//
// fs/os/path are required LAZILY inside handle() so the module loads cleanly
// under plain node in tests (the router require()s every handler at activation).

module.exports = {
  type: "agent-status",
  handle: function handle(msg, ctx) {
    const fs = require("fs");
    const os = require("os");
    const path = require("path");
    let authorized = false;
    try {
      authorized = fs.existsSync(path.join(os.homedir(), ".claude", ".credentials.json"));
    } catch (_) {
      authorized = false;
    }
    ctx.postMessage({ type: "agent-status-result", authorized: authorized });
  },
};
