"use strict";
// P4 (S3, visual-polish; regression pin): the grid's "No rows" empty state
// must span the FULL rendered column count -- data columns AND the delete
// column when present -- so it never looks like a lone narrow cell floating
// under a wide header. Verified: renderGrid already computes
// `td.colSpan = cols.length + (showDelete ? 1 : 0)` (app.js, renderGrid's
// empty branch), matching the header's own cell count exactly -- this test
// locks that invariant in rather than re-fixing an already-correct behavior.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

// With a delete column (row key present, write mode on, deleteRow enabled):
// the empty td's colSpan must equal the header's cell count (data cols + 1).
async function scenarioEmptyStateSpansFullWidthWithDeleteColumn() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "deleteRow", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [
        { name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" },
        { name: "val", dataType: "text", pk: false, editable: true, reason: "" },
      ],
      rows: [], rowKeyCols: ["id"],
    });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content td.state.empty"), { desc: "empty grid state renders" });

  const headerCells = c.document.querySelectorAll("#content table.grid thead th").length;
  assert.strictEqual(headerCells, 3, "precondition: 2 data columns + 1 delete column render in the header");
  const emptyTd = c.document.querySelector("#content td.state.empty");
  assert.strictEqual(emptyTd.colSpan, headerCells, "the empty-state td spans every header cell, including the delete column");
  c.close();
}

async function main() {
  await scenarioEmptyStateSpansFullWidthWithDeleteColumn();
  console.log("grid-empty-state-colspan.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
