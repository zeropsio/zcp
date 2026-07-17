"use strict";
// B6 (S3): delete confirms must name their target. Before this fix,
// deleteRow's confirm was the literal "Delete this row?" / "DELETE row"
// regardless of what was being deleted -- both for a tabular row and for a
// per-field KV entry delete (deleteRow is the ONE shared handler behind the
// grid's per-row delete button for both). New contract:
//   - tabular row: the key columns as "col=value" pairs (ALL key columns,
//     comma-joined) -- e.g. title "Delete row id=4?", action line
//     "DELETE row WHERE id=4"; a multi-column key joins every column.
//   - KV entry: the field/member name -- e.g. "Delete field1?" / "DELETE
//     field1" -- matching the house pattern the whole-key delete (deleteNode)
//     already uses (openBlob's "Delete <name>?").

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function openGrid(service, table) {
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: service.hostname, segments: ["public", "t"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute(table);
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: service.hostname });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid"), { desc: "grid render" });
  return c;
}

// 1. A single-column PK: the confirm names it as id=4.
async function scenarioSingleColumnKeyIdentity() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "deleteRow", enabled: true, readOnly: false, reason: "" }],
  };
  const table = {
    columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }, { name: "val", dataType: "text", pk: false, editable: false, reason: "" }],
    rows: [["4", "hello"]], rowKeyCols: ["id"],
  };
  const c = await openGrid(service, table);
  click(c.document.querySelector("#content .rowdel"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "delete confirm modal renders" });
  assert.strictEqual(c.document.getElementById("modaltitle").textContent, "Delete row id=4?", "the confirm title names the row's key");
  assert.strictEqual(c.document.querySelector("#modalbody .action").textContent, "DELETE row WHERE id=4", "the confirm action line names the row's key");
  c.close();
}

// 2. A multi-column key: every key column joins, comma-separated.
async function scenarioMultiColumnKeyIdentity() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "deleteRow", enabled: true, readOnly: false, reason: "" }],
  };
  const table = {
    columns: [
      { name: "a", dataType: "int", pk: true, editable: false, reason: "primary key" },
      { name: "b", dataType: "int", pk: true, editable: false, reason: "primary key" },
      { name: "val", dataType: "text", pk: false, editable: false, reason: "" },
    ],
    rows: [["1", "2", "hello"]], rowKeyCols: ["a", "b"],
  };
  const c = await openGrid(service, table);
  click(c.document.querySelector("#content .rowdel"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "delete confirm modal renders" });
  assert.strictEqual(c.document.getElementById("modaltitle").textContent, "Delete row a=1, b=2?", "a multi-column key joins every key column");
  c.close();
}

// 3. A KV entry delete names the field/member, matching the whole-key delete's
//    existing "Delete <name>?" pattern.
async function scenarioKVEntryIdentity() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [{ id: "editKVEntry", enabled: true, readOnly: false, reason: "" }],
  };
  const table = {
    columns: [{ name: "field", dataType: "string", pk: true, editable: false, reason: "" }, { name: "value", dataType: "string", pk: false, editable: true, reason: "" }],
    rows: [["field1", "v1"]], rowKeyCols: ["field"],
  };
  const c = await openGrid(service, table);
  click(c.document.querySelector("#content .rowdel"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "delete confirm modal renders" });
  assert.strictEqual(c.document.getElementById("modaltitle").textContent, "Delete field1?", "a KV entry delete names the field/member");
  assert.strictEqual(c.document.querySelector("#modalbody .action").textContent, "DELETE field1", "the KV entry delete's action line names the field/member");
  c.close();
}

async function main() {
  await scenarioSingleColumnKeyIdentity();
  await scenarioMultiColumnKeyIdentity();
  await scenarioKVEntryIdentity();
  console.log("delete-confirm-identity.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
