"use strict";
// P8 (S2, visual-polish): commit-time column stability. Committing a cell
// edit can grow/shrink that cell's rendered width, reflowing every OTHER
// column in the row (~10px, live-observed) because an unconstrained
// table.grid sizes its columns from content. Fix: after the grid's FIRST
// full render, measure each header cell's live offsetWidth and write those
// pixel widths back as an explicit <colgroup>, then switch the table to
// table-layout:fixed so later content changes can't reflow sibling columns.
// Column count is fixed for a table's lifetime (a load-more append never
// adds/removes a column), so this runs exactly once, right after the first
// render -- never again for that table.
//
// jsdom performs no layout, so a real render's offsetWidth is always 0 --
// the freeze must skip cleanly rather than pin every column to a bogus 0px.
// Scenario 1 pins that skip. Scenario 2 proves the WRITE path itself by
// stubbing HTMLElement.prototype.offsetWidth (verified empirically: writable
// and picked up by elements created after the stub, since app.js's table is
// built by the SAME render call that reads it) to simulate a real browser's
// measured layout -- live pixel-accuracy is confirmed by the post-deploy
// re-gallery (bbox stability), not by jsdom.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

function service() {
  return {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "deleteRow", enabled: true, readOnly: false, reason: "" }],
  };
}

function routes(method, p) {
  if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service()], allowWrites: true });
  if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
  if (p.startsWith("/api/table")) return jsonRoute({
    columns: [
      { name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" },
      { name: "val", dataType: "text", pk: false, editable: true, reason: "" },
    ],
    rows: [["1", "hello"]], rowKeyCols: ["id"],
  });
  return null;
}

// 1. Default jsdom (offsetWidth always 0): the freeze must skip cleanly --
//    no colgroup, no table-layout:fixed, no crash.
async function scenarioUnmeasurableLayoutSkipsCleanly() {
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid tbody.gridbody td"), { desc: "grid renders" });
  const table = c.document.querySelector("#content table.grid");
  assert.strictEqual(table.querySelector("colgroup"), null, "jsdom reports offsetWidth 0 for every header cell -- the freeze skips rather than pin every column to a bogus 0px");
  assert.strictEqual(table.style.tableLayout, "", "table-layout is left alone when nothing was measurable");
  c.close();
}

// 2. Stubbed layout (offsetWidth > 0, as a real browser would report):
//    the freeze writes one <col> per header cell and switches to fixed layout.
async function scenarioMeasurableLayoutFreezesColumns() {
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  // Stub BEFORE the service selects and the grid renders -- app.js's table is
  // built and frozen within the SAME synchronous render call triggered by
  // the tree-node click below, so the stub must already be in place when
  // that happens (there is no reachable seam to inject it in between).
  Object.defineProperty(c.window.HTMLElement.prototype, "offsetWidth", { configurable: true, get() { return 120; } });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid tbody.gridbody td"), { desc: "grid renders" });

  const table = c.document.querySelector("#content table.grid");
  const headerCount = table.querySelectorAll("thead th").length;
  assert.strictEqual(headerCount, 3, "precondition: 2 data columns + 1 delete column");
  const cols = table.querySelectorAll("colgroup col");
  assert.strictEqual(cols.length, headerCount, "one <col> is written per rendered header cell (data columns + delete column)");
  for (const col of cols) assert.strictEqual(col.style.width, "120px", "each <col> pins the header cell's measured width");
  assert.strictEqual(table.style.tableLayout, "fixed", "the table switches to table-layout:fixed once column widths are pinned");
  c.close();
}

async function main() {
  await scenarioUnmeasurableLayoutSkipsCleanly();
  await scenarioMeasurableLayoutFreezesColumns();
  console.log("grid-column-freeze.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
