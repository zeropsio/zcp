"use strict";
// FIX 5 (S2, spec-adjudicated): a view-only service must not render a DISABLED
// mutation affordance -- it must render NONE at all. Confirmed live on
// clickhouse (insert-row + per-row delete rendered disabled, title "service is
// view-only") and qdrant (a disabled write control on points). Root cause: the
// server still advertises these actions with enabled:false (so the rail tier
// badge can explain the service's posture), but several SPA render gates
// checked only hasAction() (presence) instead of actionEnabled() -- rendering
// a disabled-but-present control via actionButton()'s own disabled/reason
// branch, or (upload bar) addUploadBar()'s own internal disabled fallback.
// This is a DISTINCT case from write-mode-off, which already hides everything
// (CORE-2) -- these scenarios are all write-mode ON, service view-only.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

function disabledAction(id) { return { id, enabled: false, readOnly: false, reason: "service is view-only" }; }
function enabledAction(id) { return { id, enabled: true, readOnly: false, reason: "" }; }

// 1. Object-family blob toolbar (rename/delete) + upload bar: a view-only
//    service (actions present, disabled) renders NONE of them.
async function scenarioObjectFamilyDisabledRendersNothing() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "view-only",
    actions: [enabledAction("readBlob"), disabledAction("writeBlob"), disabledAction("renameObject"), disabledAction("deleteNode"), disabledAction("uploadObject")],
  };
  const c = buildConsole({
    url: "http://localhost/", embedded: true,
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "f.txt", kind: "blob", path: { service: "storage", segments: ["f.txt"] }, meta: { size: 5 } }] });
      if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
      return null;
    },
  });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "storage" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });

  assert.strictEqual(c.document.querySelector("#tree .uploadbar"), null, "a view-only service's root tree renders NO upload bar (not even a disabled one)");

  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content .toolbar"), { desc: "blob toolbar render" });
  assert.strictEqual(c.document.getElementById("saveblob"), null, "writeBlob disabled -> no Save button");
  assert.strictEqual(c.document.getElementById("renameblob"), null, "renameObject disabled -> no Rename button (not even disabled-with-reason)");
  assert.strictEqual(c.document.getElementById("delblob"), null, "deleteNode disabled -> no Delete button (not even disabled-with-reason)");
  c.close();
}

// 2. Same object family, full write -- the affordances DO render (and are enabled).
async function scenarioObjectFamilyEnabledRendersControls() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "supported",
    actions: [enabledAction("readBlob"), enabledAction("writeBlob"), enabledAction("renameObject"), enabledAction("deleteNode"), enabledAction("uploadObject")],
  };
  const c = buildConsole({
    url: "http://localhost/", embedded: true,
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "f.txt", kind: "blob", path: { service: "storage", segments: ["f.txt"] }, meta: { size: 5 } }] });
      if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
      return null;
    },
  });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "storage" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });

  const bar = c.document.querySelector("#tree .uploadbar");
  assert.ok(bar, "a full-write service's root tree renders the upload bar");
  assert.strictEqual(bar.querySelector("button[disabled]"), null, "the upload control is not disabled when the action is enabled");

  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content .toolbar"), { desc: "blob toolbar render" });
  assert.ok(c.document.getElementById("saveblob"), "writeBlob enabled -> Save button renders");
  const rename = c.document.getElementById("renameblob");
  const del = c.document.getElementById("delblob");
  assert.ok(rename && !rename.disabled, "renameObject enabled -> Rename button renders enabled");
  assert.ok(del && !del.disabled, "deleteNode enabled -> Delete button renders enabled");
  c.close();
}

// 3. Tabular-family grid (insert row / per-row delete): a view-only service
//    (TAB-9: clickhouse) renders NEITHER control, not a disabled one.
async function scenarioTabularFamilyDisabledRendersNothing() {
  const service = {
    hostname: "ch", type: "clickhouse:single@1", support: "view-only",
    actions: [disabledAction("insertRow"), disabledAction("deleteRow")],
  };
  const c = buildConsole({
    url: "http://localhost/", embedded: true,
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "ch", segments: ["default", "t"] } }] });
      if (p.startsWith("/api/table")) return jsonRoute({
        columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }, { name: "val", dataType: "text", pk: false, editable: false, reason: "service is view-only" }],
        rows: [["1", "x"]], rowKeyCols: ["id"],
      });
      return null;
    },
  });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "ch" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid"), { desc: "grid render" });

  assert.strictEqual(c.document.getElementById("insertrow"), null, "insertRow disabled -> no Insert row button (not even disabled-with-reason)");
  assert.strictEqual(c.document.querySelectorAll("#content .rowdel").length, 0, "deleteRow disabled -> no per-row delete button, and no delete column at all");
  c.close();
}

// 4. Same tabular family, full write -- both controls DO render, enabled.
async function scenarioTabularFamilyEnabledRendersControls() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [enabledAction("insertRow"), enabledAction("deleteRow")],
  };
  const c = buildConsole({
    url: "http://localhost/", embedded: true,
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
      if (p.startsWith("/api/table")) return jsonRoute({
        columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }, { name: "val", dataType: "text", pk: false, editable: true, reason: "" }],
        rows: [["1", "x"]], rowKeyCols: ["id"],
      });
      return null;
    },
  });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid"), { desc: "grid render" });

  const insert = c.document.getElementById("insertrow");
  assert.ok(insert && !insert.disabled, "insertRow enabled -> Insert row button renders enabled");
  const rowdel = c.document.querySelector("#content .rowdel");
  assert.ok(rowdel && !rowdel.disabled, "deleteRow enabled -> per-row delete button renders enabled");
  c.close();
}

// 5. KV TTL control: a view-only service (setTTL present, disabled) renders
//    NEITHER the "Set TTL" nor "Persist" button, though the informational
//    "TTL: ..." label (a READ, not a mutation affordance) still shows.
async function scenarioTTLDisabledRendersNothing() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "view-only",
    actions: [enabledAction("readBlob"), disabledAction("setTTL")],
  };
  const c = buildConsole({
    url: "http://localhost/", embedded: true,
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { size: 5 } }] });
      if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
      if (p.startsWith("/api/stat")) return jsonRoute({ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { ttlSeconds: null } });
      return null;
    },
  });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content .ttlbar"), { desc: "ttl bar render" });
  assert.strictEqual(c.document.getElementById("setttl"), null, "setTTL disabled -> no Set TTL button (not even disabled-with-reason)");
  assert.strictEqual(c.document.getElementById("clrttl"), null, "setTTL disabled -> no Persist button (not even disabled-with-reason)");
  c.close();
}

// 6. Same KV family, full write -- both TTL controls DO render, enabled.
async function scenarioTTLEnabledRendersControls() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [enabledAction("readBlob"), enabledAction("setTTL")],
  };
  const c = buildConsole({
    url: "http://localhost/", embedded: true,
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { size: 5 } }] });
      if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
      if (p.startsWith("/api/stat")) return jsonRoute({ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { ttlSeconds: null } });
      return null;
    },
  });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content .ttlbar"), { desc: "ttl bar render" });
  const setttl = c.document.getElementById("setttl");
  const clrttl = c.document.getElementById("clrttl");
  assert.ok(setttl && !setttl.disabled, "setTTL enabled -> Set TTL button renders enabled");
  assert.ok(clrttl && !clrttl.disabled, "setTTL enabled -> Persist button renders enabled");
  c.close();
}

async function main() {
  await scenarioObjectFamilyDisabledRendersNothing();
  await scenarioObjectFamilyEnabledRendersControls();
  await scenarioTabularFamilyDisabledRendersNothing();
  await scenarioTabularFamilyEnabledRendersControls();
  await scenarioTTLDisabledRendersNothing();
  await scenarioTTLEnabledRendersControls();
  console.log("view-only-affordances.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
