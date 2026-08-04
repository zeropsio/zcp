"use strict";
// A table with no rows and a query returning no rows are different user
// states. Both still render through the grid's one empty td, spanning every
// visible header cell (including the write-only delete column when present).

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };
const COLUMNS = [
  { name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" },
  { name: "name", dataType: "text", pk: false, editable: true, reason: "" },
];

function enabledAction(id, readOnly = false) {
  return { id, enabled: true, readOnly, reason: "" };
}

async function scenarioEmptyTableCopyAndColspan() {
  const service = {
    hostname: "db", type: "postgresql:single@18", family: "tabular", support: "supported",
    actions: [enabledAction("insertRow"), enabledAction("deleteRow")],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "people", kind: "tabular", path: { service: "db", segments: ["public", "people"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute({ columns: COLUMNS, rows: [], rowKeyCols: ["id"] });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "table node" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content td.state.empty"), { desc: "empty table slate" });

  const empty = c.document.querySelector("#content td.state.empty");
  assert.strictEqual(empty.querySelector(".state-title").textContent, "No rows yet", "a table's own emptiness uses table copy");
  assert.strictEqual(empty.querySelector(".state-detail").textContent, "Use Insert row above to add the first one.", "the slate points to the already-rendered toolbar action");
  assert.ok(c.document.getElementById("insertrow"), "precondition: Insert row is available in the toolbar");
  assert.strictEqual(empty.querySelector("button"), null, "the mutating action is not duplicated inside the slate");
  assert.strictEqual(empty.colSpan, c.document.querySelectorAll("#content table.grid thead th").length, "the empty td spans data columns and the delete column");
  c.close();
}

async function scenarioEmptyQueryCopyAndColspan() {
  const service = {
    hostname: "db", type: "postgresql:single@18", family: "tabular", support: "supported",
    actions: [enabledAction("querySQL", true)],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    if (p === "/api/query") return jsonRoute({ columns: COLUMNS, rows: [], rowKeyCols: null });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/#t=FAKE&svc=db", routes });
  await waitFor(() => c.document.getElementById("querylink"), { desc: "query link" });
  click(c.document.getElementById("querylink"));
  await waitFor(() => c.document.getElementById("qtext"), { desc: "query console" });
  c.document.getElementById("qtext").value = "SELECT id, name FROM people WHERE false";
  click(c.document.getElementById("runq"));
  await waitFor(() => c.document.querySelector("#qresult td.state.empty"), { desc: "empty query result slate" });

  const empty = c.document.querySelector("#qresult td.state.empty");
  assert.strictEqual(empty.querySelector(".state-title").textContent, "Query returned no rows", "query-result emptiness is worded distinctly from table emptiness");
  assert.ok(!empty.textContent.includes("No rows yet"), "query results never reuse the table-empty copy");
  assert.strictEqual(empty.colSpan, c.document.querySelectorAll("#qresult table.grid thead th").length, "the query empty td spans every query-result header");
  c.close();
}

async function main() {
  await scenarioEmptyTableCopyAndColspan();
  await scenarioEmptyQueryCopyAndColspan();
  console.log("blank-slate-grid-copy.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
