"use strict";
// Column resizing evolves the old edit-stability freeze: every column now has
// an explicit width even when layout is not measurable, while a measurable
// first render still supplies the initial data-column width. The action column
// is independently locked at 44px. A committed value change must not alter any
// explicit width or the table's effective width.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

function service() {
  return {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [
      { id: "editCell", enabled: true, readOnly: false, reason: "" },
      { id: "deleteRow", enabled: true, readOnly: false, reason: "" },
    ],
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
  if (method === "POST" && p === "/api/cell") return jsonRoute({ affected: 1 });
  return null;
}

// 1. Default jsdom (offsetWidth always 0): use the known useful 160px fallback.
async function scenarioUnmeasurableLayoutUsesUsefulFallback() {
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid tbody.gridbody td"), { desc: "grid renders" });
  const table = c.document.querySelector("#content table.grid");
  const cols = Array.from(table.querySelectorAll("colgroup col"), (col) => col.style.width);
  assert.deepStrictEqual(cols, ["160px", "160px", "44px"], "unmeasurable data columns receive the useful fallback; action stays locked");
  assert.strictEqual(table.style.tableLayout, "fixed");
  assert.strictEqual(table.style.width, "364px");
  c.close();
}

// 2. Stubbed layout initializes data widths from the live header measurement,
//    then a committed edit leaves those widths unchanged.
async function scenarioMeasurableLayoutPreservesWidthsAcrossEdit() {
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
  assert.deepStrictEqual(Array.from(cols, (col) => col.style.width), ["120px", "120px", "44px"], "data columns use measured widths while the action column stays fixed");
  assert.strictEqual(table.style.tableLayout, "fixed", "the table switches to table-layout:fixed once column widths are pinned");
  assert.strictEqual(table.style.width, "284px");

  click(table.querySelector("tbody td:nth-child(2)"));
  const input = table.querySelector("input.celledit");
  assert.ok(input, "editable value opens the real cell editor");
  input.value = "a much longer committed value that must not reflow columns";
  input.blur();
  await waitFor(() => table.querySelector("tbody td:nth-child(2)").textContent.includes("much longer committed"), { desc: "cell commit" });
  assert.deepStrictEqual(Array.from(cols, (col) => col.style.width), ["120px", "120px", "44px"], "committing content cannot reflow any column");
  assert.strictEqual(table.style.width, "284px", "effective table width is stable after edit");
  c.close();
}

async function main() {
  await scenarioUnmeasurableLayoutUsesUsefulFallback();
  await scenarioMeasurableLayoutPreservesWidthsAcrossEdit();
  console.log("grid-column-freeze.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
