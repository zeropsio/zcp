"use strict";
// Durable invariant: a mutating affordance renders ONLY when BOTH halves of
// the AND hold — the server-declared action is `enabled` AND the session
// itself is embedded+write-enabled (app.js's `editing()`). This pins the
// caller-bound write posture (spec-dataconsole.md §5) at the UI layer: the
// SPA must never show a write control the server capability payload didn't
// grant, and must never show one just because the server allows it while
// the local session hasn't (yet) turned write mode on. Must hold before and
// after the S15 renderer unification (excellence-program plan §6 DD-7).
//
// Uses the blob Save button (`#saveblob`) as the probe: app.js only emits the
// <button> markup at all when `editing() && actionEnabled(...)` — see app.js
// openBlob. The renameObject/deleteNode/insertRow/row-delete/upload-bar
// controls follow the same present-only-when-enabled shape (FIX 5,
// view-only-affordances.dom.test.js) — a disabled-but-present control for a
// hasAction-but-disabled action is no longer a valid gating shape anywhere in
// the SPA.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

async function saveButtonPresent({ embedded, writeEnabled, actionEnabled }) {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "writeBlob", enabled: actionEnabled, readOnly: false, reason: actionEnabled ? "" : "session is read-only" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "f.txt", kind: "blob", path: { service: "db", segments: ["f.txt"] }, meta: { size: 5 } }] });
    if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    return null;
  };
  const url = embedded ? "http://localhost/" : "http://localhost/#t=FAKE&svc=db";
  const c = buildConsole({ url, embedded, routes });
  if (embedded) {
    await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready sent to host" });
    hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled, service: "db" });
  }
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content .toolbar"), { desc: "blob toolbar render" });
  const present = !!c.document.getElementById("saveblob");
  c.close();
  return present;
}

async function main() {
  assert.strictEqual(
    await saveButtonPresent({ embedded: true, writeEnabled: true, actionEnabled: true }),
    true,
    "embedded + session write-enabled + action enabled -> the mutating control renders"
  );
  assert.strictEqual(
    await saveButtonPresent({ embedded: true, writeEnabled: true, actionEnabled: false }),
    false,
    "embedded + session write-enabled but action DISABLED (server capability) -> the mutating control does not render"
  );
  assert.strictEqual(
    await saveButtonPresent({ embedded: true, writeEnabled: false, actionEnabled: true }),
    false,
    "embedded + action enabled but the session is NOT write-enabled (host has not confirmed) -> the mutating control does not render"
  );
  assert.strictEqual(
    await saveButtonPresent({ embedded: false, writeEnabled: false, actionEnabled: true }),
    false,
    "standalone (view-only by construction, never write-enabled) + action enabled -> the mutating control never renders"
  );
  console.log("capability-gating.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
