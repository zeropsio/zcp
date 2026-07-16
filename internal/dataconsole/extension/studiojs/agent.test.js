"use strict";

// S4 — Agent status + launcher contract (L-AG-1/L-AG-2/L-AG-3/L-AG-4,
// R-SEC-LOCAL, NN6).
//
// Plain node, no vscode stub needed: the agent-launch handler require()s
// "vscode" LAZILY inside handle(), so loading the module is clean under node.
// We never call agent-launch.handle() here (that would need a live editor) — we
// pin its safety law by reading the source text instead.

const assert = require("assert");
const fs = require("fs");
const path = require("path");

const card = require("../templates/vscode-studio/cards/agent");
const statusHandler = require("../templates/vscode-studio/handlers/agent-status");
const launchHandler = require("../templates/vscode-studio/handlers/agent-launch");

// (1) Card render exposes both actions + a status placeholder; the clientScript
//     reacts to the host's agent-status-result message.
assert.strictEqual(card.id, "agent", "card id is 'agent'");
assert.strictEqual(card.title, "Claude Code", "card title is 'Claude Code'");
const html = card.render({ services: [] });
assert.ok(
  html.indexOf('data-action="agent-launch"') >= 0,
  "render wires the Launch Claude Code button"
);
assert.ok(
  html.indexOf('data-action="agent-status"') >= 0,
  "render wires the Re-check button"
);
assert.ok(
  html.indexOf('id="zs-agent-status"') >= 0,
  "render includes the status placeholder"
);
assert.strictEqual(typeof card.clientScript, "string", "card has a clientScript string");
assert.ok(
  card.clientScript.indexOf("agent-status-result") >= 0,
  "clientScript reacts to the host's agent-status-result message"
);

// (2) Both handlers export the assigned types with handle functions.
assert.strictEqual(statusHandler.type, "agent-status", "status handler type");
assert.strictEqual(typeof statusHandler.handle, "function", "status handler.handle is a function");
assert.strictEqual(launchHandler.type, "agent-launch", "launch handler type");
assert.strictEqual(typeof launchHandler.handle, "function", "launch handler.handle is a function");

// (3) agent-status.handle posts a well-shaped result with a boolean `authorized`
//     field (reflects this machine's real ~/.claude state — shape is what we pin).
let posted = null;
const ctx = {
  postMessage: function (m) {
    posted = m;
  },
};
statusHandler.handle({ type: "agent-status" }, ctx);
assert.ok(posted, "status handler posted a message");
assert.strictEqual(posted.type, "agent-status-result", "posted message type");
assert.strictEqual(typeof posted.authorized, "boolean", "authorized is a boolean");

// (4) Source-level safety pin (R-SEC-LOCAL / L-AG-4): the launch handler must
//     NEVER carry the permission-bypass flag.
const launchSrc = fs.readFileSync(
  path.join(__dirname, "../templates/vscode-studio/handlers/agent-launch.js"),
  "utf8"
);
assert.ok(
  launchSrc.indexOf("dangerously-skip-permissions") < 0,
  "agent-launch must NOT pass --dangerously-skip-permissions"
);

console.log("agent.test.js OK");
