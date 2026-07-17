"use strict";
// C2: promptModal's onValue calls confirmAction, a SECOND modal reusing the
// shared #modal -- but the #modalok completion handler called hideModal()
// UNCONDITIONALLY after `await run()`. Since the prompt's run() synchronously
// chains into confirmAction's showModal() (which shows the SECOND modal, all
// within the same microtask that resolves the prompt's onOK), the outer
// completion's hideModal() then tore down the confirm modal onValue had just
// opened -- so Rename and Set TTL's actual write (/api/rename, /api/ttl)
// never fired. Fix: an epoch minted in showModal; the #modalok completion
// closes the modal ONLY if the epoch it captured at click-time is still
// current -- a nested confirmAction() opened during run() bumps the epoch, so
// the outer (stale) completion must not hideModal() out from under it.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

// 1. Rename: prompt -> type new name -> OK must open (and keep open) the
//    confirm modal, and confirming THAT must fire /api/rename.
async function scenarioRenamePromptThenConfirmBothFire() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "supported",
    actions: [{ id: "renameObject", enabled: true, readOnly: false, reason: "" }, { id: "readBlob", enabled: true, readOnly: true, reason: "" }],
  };
  const renameCalls = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "old.txt", kind: "blob", path: { service: "storage", segments: ["old.txt"] }, meta: { size: 5 } }] });
    if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    if (method === "POST" && p === "/api/rename") { renameCalls.push(JSON.parse(body)); return jsonRoute({ ok: true }); }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "storage" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.getElementById("renameblob"), { desc: "rename button render" });
  click(c.document.getElementById("renameblob"));
  await waitFor(() => c.document.getElementById("modalinput"), { desc: "rename prompt modal renders" });

  c.document.getElementById("modalinput").value = "new.txt";
  click(c.document.getElementById("modalok"));

  // The prompt's OK click synchronously chains into confirmAction's
  // showModal() -- the confirm modal's title is already set by the time
  // click() returns. But a waitFor() that succeeds on its own very first
  // (synchronous) poll would race AHEAD of the outer #modalok completion's
  // own hideModal() call, which lands a tick later (proven empirically: the
  // bug's erroneous hideModal() fires within one microtask drain, not
  // synchronously) -- so this uses a real macrotask delay to let that full
  // chain settle BEFORE asserting the confirm modal is still up.
  await new Promise((resolve) => setTimeout(resolve, 20));

  assert.ok(/Rename.*old\.txt.*new\.txt/.test(c.document.getElementById("modaltitle").textContent || ""), "the confirm modal's summary is showing");
  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "the confirm modal must stay visible, not be hidden by the prompt modal's own completion");
  assert.strictEqual(renameCalls.length, 0, "no rename request fires until the confirm modal is itself confirmed");

  click(c.document.getElementById("modalok"));
  await waitFor(() => renameCalls.length === 1, { desc: "confirming the rename modal fires /api/rename" });
  assert.strictEqual(renameCalls[0].to.segments.join("/"), "new.txt", "the rename request carries the new name");
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "the confirm modal closes after the rename succeeds" });
  c.close();
}

// 2. Same shape for Set TTL (promptModal -> confirmAction -> /api/ttl).
async function scenarioSetTTLPromptThenConfirmBothFire() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }, { id: "setTTL", enabled: true, readOnly: false, reason: "" }],
  };
  const ttlCalls = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { size: 5 } }] });
    if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    if (p.startsWith("/api/stat")) return jsonRoute({ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { ttlSeconds: null } });
    if (method === "PUT" && p === "/api/ttl") { ttlCalls.push(JSON.parse(body)); return jsonRoute({ ok: true }); }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.getElementById("setttl"), { desc: "Set TTL button renders" });
  click(c.document.getElementById("setttl"));
  await waitFor(() => c.document.getElementById("modalinput"), { desc: "TTL prompt modal renders" });

  c.document.getElementById("modalinput").value = "120";
  click(c.document.getElementById("modalok"));

  // See the Rename scenario above for why this needs a real delay rather than
  // a waitFor() that could resolve on its own first (synchronous) poll.
  await new Promise((resolve) => setTimeout(resolve, 20));

  assert.ok(/Set TTL 120s/.test(c.document.getElementById("modaltitle").textContent || ""), "the confirm modal's TTL summary is showing");
  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "the confirm modal must stay visible after the prompt modal's own completion");
  assert.strictEqual(ttlCalls.length, 0, "no TTL request fires until the confirm modal is itself confirmed");

  click(c.document.getElementById("modalok"));
  await waitFor(() => ttlCalls.length === 1, { desc: "confirming the TTL modal fires /api/ttl" });
  assert.strictEqual(ttlCalls[0].ttlSeconds, 120, "the TTL request carries the typed seconds");
  c.close();
}

// 3. Regression guard: a normal (non-nested) single modal still closes on
//    success -- the epoch fix must not leave EVERY modal stuck open.
async function scenarioPlainSingleModalStillClosesOnSuccess() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "supported",
    actions: [{ id: "deleteNode", enabled: true, readOnly: false, reason: "" }, { id: "readBlob", enabled: true, readOnly: true, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "f.txt", kind: "blob", path: { service: "storage", segments: ["f.txt"] }, meta: { size: 5 } }] });
    if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    if (method === "DELETE" && p === "/api/node") return jsonRoute({ ok: true });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "storage" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.getElementById("delblob"), { desc: "delete button render" });
  click(c.document.getElementById("delblob"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "delete confirm modal renders" });
  click(c.document.getElementById("modalok"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "a normal (non-nested) modal still closes on success" });
  c.close();
}

async function main() {
  await scenarioRenamePromptThenConfirmBothFire();
  await scenarioSetTTLPromptThenConfirmBothFire();
  await scenarioPlainSingleModalStillClosesOnSuccess();
  console.log("nested-confirm-epoch.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
