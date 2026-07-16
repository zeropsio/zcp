"use strict";

// S6 — Live topology refresh seam test (L-CO-4: debounced live update, no-op
// suppression).
//
// (1) The card renders the unobtrusive "● live" tick and its clientScript runs a
//     setInterval that posts {type:"refresh"} through the shell's EXISTING
//     vscodeApi — it must NOT call acquireVsCodeApi again (the shell already did;
//     a second call throws).
// (2) The handler declares the exact { type:"refresh", handle } shape the router
//     discovers (its `type` is the webview->host allowlist key).
// (3) No-op suppression + change detection: identical topology polls coalesce
//     into a single refreshTopology() call; a real topology change triggers
//     another. lastHash is module-level, so we require the handler once and drive
//     it through both phases in order.

const assert = require("assert");
const card = require("../templates/vscode-studio/cards/refresh");
const handler = require("../templates/vscode-studio/handlers/refresh");

// ---- (1) card render + clientScript ----------------------------------------
assert.strictEqual(card.id, "refresh", "card id is 'refresh'");

const html = card.render({ services: [] });
assert.strictEqual(typeof html, "string", "render returns a string");
assert.ok(html.indexOf("data-zs-sync-tick") >= 0, "render includes the live tick element");
assert.ok(html.indexOf("live") >= 0, "render labels the tick 'live'");

assert.strictEqual(typeof card.clientScript, "string", "card has a clientScript string");
assert.ok(card.clientScript.indexOf("setInterval") >= 0, "clientScript arms a setInterval");
assert.ok(card.clientScript.indexOf("refresh") >= 0, "clientScript posts a 'refresh' message");
assert.ok(card.clientScript.indexOf("vscodeApi") >= 0, "clientScript uses the shell's vscodeApi");
assert.ok(
  card.clientScript.indexOf("acquireVsCodeApi") < 0,
  "clientScript must NOT call acquireVsCodeApi again (reuse the shell's instance)"
);

// ---- (2) handler shape ------------------------------------------------------
assert.strictEqual(handler.type, "refresh", "handler type is exactly 'refresh'");
assert.strictEqual(typeof handler.handle, "function", "handler exports a handle function");

// ---- (3) no-op suppression + change detection -------------------------------
// Phase A: a fixed uiMap polled twice -> refreshTopology called exactly ONCE
// (the second, identical poll is suppressed).
const uiMapA = {
  project: { id: "p", name: "demo", status: "ACTIVE" },
  services: [
    { id: "svc-app", hostname: "app", type: "nodejs@22", status: "ACTIVE", category: "runtime" },
  ],
  warnings: [],
};

let refreshed = 0;
let current = uiMapA;
const ctx = {
  runTransport: function () {
    return { ok: true, uiMap: current };
  },
  refreshTopology: function () {
    refreshed += 1;
  },
};

(async function main() {
  await handler.handle({ type: "refresh" }, ctx);
  await handler.handle({ type: "refresh" }, ctx);
  assert.strictEqual(refreshed, 1, "two identical polls trigger exactly one refresh (no-op suppressed)");

  // Phase B: a DIFFERENT topology -> refreshTopology fires again (now 2 total).
  current = {
    project: { id: "p", name: "demo", status: "ACTIVE" },
    services: [
      { id: "svc-app", hostname: "app", type: "nodejs@22", status: "ACTIVE", category: "runtime" },
      { id: "svc-db", hostname: "db", type: "postgresql@16", status: "ACTIVE", category: "managed" },
    ],
    warnings: [],
  };
  await handler.handle({ type: "refresh" }, ctx);
  assert.strictEqual(refreshed, 2, "a real topology change triggers another refresh");

  // And the new topology, re-polled identically, is suppressed again.
  await handler.handle({ type: "refresh" }, ctx);
  assert.strictEqual(refreshed, 2, "the changed topology, polled again unchanged, is suppressed");

  // Transport failure is a hard no-op (no refresh, last good paint stays).
  const failCtx = {
    runTransport: function () { return { ok: false, error: "boom" }; },
    refreshTopology: function () { throw new Error("must not refresh on transport failure"); },
  };
  await handler.handle({ type: "refresh" }, failCtx);

  console.log("refresh.test.js OK");
})().catch(function (err) {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
