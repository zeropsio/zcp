"use strict";
// P1 (S3, visual-polish): modal Confirm severity. Every confirm modal's OK
// button was hardcoded `.danger` (red) regardless of what it actually did --
// Add key/Insert row/Set TTL/Add document looked exactly as alarming as
// Delete. Fix: showModal()/confirmAction() take a `kind` ("danger" default,
// "primary" for a create/insert/TTL-set/non-destructive prompt) -- only
// destructive flows (delete row/key/entry/node, overwrite-on-save, and
// rename's COPY+DELETE confirm -- it deletes the source, so it stays danger
// even though the PROMPT step that gathers the new name does not) keep the
// red styling. A non-danger Confirm needs no new CSS: dropping `.danger`
// already falls back to the base `button` (accent) look.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

function isDanger(c) { return c.document.getElementById("modalok").classList.contains("danger"); }

// 1. Deleting a tabular row: destructive -> danger.
async function scenarioDeleteRowConfirmIsDanger() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "deleteRow", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }],
      rows: [["1"]], rowKeyCols: ["id"],
    });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content .rowdel"), { desc: "row-delete button render" });
  click(c.document.querySelector("#content .rowdel"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "delete confirm modal renders" });
  assert.ok(isDanger(c), "deleting a row is destructive -- Confirm carries .danger");
  c.close();
}

// 2. Inserting a row: a create -- Confirm gets the standard primary look, not danger.
async function scenarioInsertRowConfirmIsPrimary() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "insertRow", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }],
      rows: [["1"]], rowKeyCols: ["id"],
    });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.getElementById("insertrow"), { desc: "insert-row button render" });
  click(c.document.getElementById("insertrow"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "insert-row modal renders" });
  assert.ok(!isDanger(c), "inserting a row is a create -- Confirm does NOT carry .danger");
  c.close();
}

// 3. Adding a KV key: a create -- Confirm gets the standard primary look.
async function scenarioAddKeyConfirmIsPrimary() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [{ id: "createKey", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.getElementById("createkeylink"), { desc: "add key link render" });
  click(c.document.getElementById("createkeylink"));
  await waitFor(() => c.document.getElementById("kvname"), { desc: "create-key form renders" });
  assert.ok(!isDanger(c), "adding a key is a create -- Confirm does NOT carry .danger");
  c.close();
}

// 4. Overwriting a blob on Save: destructive (clobbers existing content) -> danger.
async function scenarioOverwriteBlobSaveConfirmIsDanger() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }, { id: "writeBlob", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "f.txt", kind: "blob", path: { service: "storage", segments: ["f.txt"] }, meta: { size: 5 } }] });
    if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "storage" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.getElementById("saveblob"), { desc: "save button render" });
  click(c.document.getElementById("saveblob"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "overwrite confirm modal renders" });
  assert.ok(isDanger(c), "overwriting a blob on Save is destructive -- Confirm carries .danger");
  c.close();
}

// 5. Rename: the PROMPT step (typing the new name) is not itself destructive
//    -- primary. The nested confirm that follows it deletes the source
//    (COPY+DELETE) -- it stays danger.
async function scenarioRenamePromptPrimaryThenNestedConfirmDanger() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "supported",
    actions: [{ id: "renameObject", enabled: true, readOnly: false, reason: "" }, { id: "readBlob", enabled: true, readOnly: true, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "old.txt", kind: "blob", path: { service: "storage", segments: ["old.txt"] }, meta: { size: 5 } }] });
    if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    if (method === "POST" && p === "/api/rename") return jsonRoute({ ok: true });
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
  assert.ok(!isDanger(c), "the rename PROMPT (typing a new name) is not destructive -- Confirm does NOT carry .danger");

  c.document.getElementById("modalinput").value = "new.txt";
  click(c.document.getElementById("modalok"));
  // See nested-confirm-epoch.dom.test.js: the prompt's OK synchronously chains
  // into the nested confirmAction's showModal(), but a waitFor() that could
  // resolve on its own first (synchronous) poll would race ahead of the outer
  // completion -- a real macrotask delay lets the full chain settle first.
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.ok(/Rename.*old\.txt.*new\.txt/.test(c.document.getElementById("modaltitle").textContent || ""), "the nested rename confirm is showing");
  assert.ok(isDanger(c), "the nested rename confirm deletes the source (COPY+DELETE) -- it stays .danger");
  c.close();
}

// 6. Set TTL: the PROMPT step (typing seconds) is primary; the nested EXPIRE
//    confirm is also primary -- setting a TTL is not destructive.
async function scenarioSetTTLPromptAndNestedConfirmBothPrimary() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }, { id: "setTTL", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { size: 5 } }] });
    if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    if (p.startsWith("/api/stat")) return jsonRoute({ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { ttlSeconds: null } });
    if (method === "PUT" && p === "/api/ttl") return jsonRoute({ ok: true });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.getElementById("setttl"), { desc: "Set TTL button render" });
  click(c.document.getElementById("setttl"));
  await waitFor(() => c.document.getElementById("modalinput"), { desc: "TTL prompt modal renders" });
  assert.ok(!isDanger(c), "the Set TTL PROMPT (typing seconds) is not destructive -- Confirm does NOT carry .danger");

  c.document.getElementById("modalinput").value = "120";
  click(c.document.getElementById("modalok"));
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.ok(/Set TTL 120s/.test(c.document.getElementById("modaltitle").textContent || ""), "the nested TTL confirm is showing");
  assert.ok(!isDanger(c), "setting a TTL is not destructive -- the nested EXPIRE confirm does NOT carry .danger");
  c.close();
}

// 7. Persist (clear TTL): explicitly NOT destructive per the review brief.
async function scenarioPersistTTLConfirmIsPrimary() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }, { id: "setTTL", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { size: 5 } }] });
    if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    if (p.startsWith("/api/stat")) return jsonRoute({ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { ttlSeconds: 60 } });
    if (method === "PUT" && p === "/api/ttl") return jsonRoute({ ok: true });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.getElementById("clrttl"), { desc: "Persist button render" });
  click(c.document.getElementById("clrttl"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "persist confirm modal renders" });
  assert.ok(!isDanger(c), "Persist (clear TTL) is not destructive -- Confirm does NOT carry .danger");
  c.close();
}

async function main() {
  await scenarioDeleteRowConfirmIsDanger();
  await scenarioInsertRowConfirmIsPrimary();
  await scenarioAddKeyConfirmIsPrimary();
  await scenarioOverwriteBlobSaveConfirmIsDanger();
  await scenarioRenamePromptPrimaryThenNestedConfirmDanger();
  await scenarioSetTTLPromptAndNestedConfirmBothPrimary();
  await scenarioPersistTTLConfirmIsPrimary();
  console.log("modal-confirm-severity.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
