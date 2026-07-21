"use strict";

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const DATA_MIN = 96;
const DATA_MAX = 640;
const DATA_DEFAULT = 160;
const ACTION_WIDTH = 44;

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

function fixture(callLog) {
  return (method, path) => {
    callLog.push(method + " " + path);
    if (path === "/api/services") return jsonRoute({ project: PROJECT, services: [service()], allowWrites: true });
    if (path.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
    if (path.startsWith("/api/table/count")) return jsonRoute({ count: 250 });
    if (path.startsWith("/api/table")) return jsonRoute({
      columns: [
        { name: "id", dataType: "int", pk: true, editable: false, reason: "primary key", sortable: true },
        { name: "val", dataType: "text", pk: false, editable: true, reason: "", sortable: true },
      ],
      rows: [["1", "hello"]], rowKeyCols: ["id"], numbered: true, nextCursor: "100",
    });
    return null;
  };
}

async function openGrid(vscodeState) {
  const calls = [];
  const c = buildConsole({ embedded: true, vscodeState, routes: fixture(calls) });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid tbody td"), { desc: "grid" });
  await waitFor(() => c.document.querySelector(".paginator.exact"), { desc: "exact paginator" });
  return { c, calls };
}

function pointer(c, target, type, clientX) {
  target.dispatchEvent(new c.window.MouseEvent(type, { bubbles: true, cancelable: true, clientX }));
}

function key(c, target, keyName) {
  target.dispatchEvent(new c.window.KeyboardEvent("keydown", { bubbles: true, cancelable: true, key: keyName }));
}

function widths(c) {
  return Array.from(c.document.querySelectorAll("table.grid colgroup col"), (col) => Number.parseFloat(col.style.width));
}

function assertEffectiveTableWidth(c, expected) {
  const table = c.document.querySelector("table.grid");
  const explicit = widths(c);
  assert.deepStrictEqual(explicit, expected);
  assert.strictEqual(Number.parseFloat(table.style.width), expected.reduce((sum, value) => sum + value, 0), "effective table width is the sum of explicit columns");
  assert.strictEqual(table.style.tableLayout, "fixed");
}

async function scenarioPointerKeyboardPersistenceAndSortIsolation() {
  const { c, calls } = await openGrid();
  const handles = c.document.querySelectorAll("th .column-resizer");
  assert.strictEqual(handles.length, 2, "each data column has one resize separator");
  assert.strictEqual(c.document.querySelector("th.delcol .column-resizer"), null, "the action/delete column is locked");
  const idHandle = handles[0];
  assert.strictEqual(idHandle.getAttribute("role"), "separator");
  assert.strictEqual(idHandle.getAttribute("aria-orientation"), "vertical");
  assert.strictEqual(idHandle.tabIndex, 0);
  assert.strictEqual(idHandle.getAttribute("aria-label"), "Resize data column");
  assert.strictEqual(c.document.getElementById(idHandle.getAttribute("aria-describedby")).textContent, "id 🔑", "separator is described by its visible column label");
  assert.strictEqual(idHandle.getAttribute("aria-valuemin"), String(DATA_MIN));
  assert.strictEqual(idHandle.getAttribute("aria-valuemax"), String(DATA_MAX));
  assert.strictEqual(idHandle.getAttribute("aria-valuenow"), String(DATA_DEFAULT));
  assertEffectiveTableWidth(c, [DATA_DEFAULT, DATA_DEFAULT, ACTION_WIDTH]);

  const beforeTreeWidth = c.document.getElementById("tree").style.width;
  pointer(c, idHandle, "pointerdown", DATA_DEFAULT);
  pointer(c, c.window, "pointermove", 230);
  pointer(c, c.window, "pointerup", 230);
  click(idHandle);
  assert.strictEqual(idHandle.getAttribute("aria-valuenow"), "230");
  assert.strictEqual(c.document.getElementById("tree").style.width, beforeTreeWidth, "column resize cannot move the explorer divider");
  assert.strictEqual(calls.some((line) => /[?&]sort=/.test(line)), false, "resize never triggers sorting");
  assertEffectiveTableWidth(c, [230, DATA_DEFAULT, ACTION_WIDTH]);

  const valHandle = handles[1];
  key(c, valHandle, "Home");
  assert.strictEqual(valHandle.getAttribute("aria-valuenow"), String(DATA_MIN), "Home reaches the useful minimum");
  key(c, valHandle, "ArrowRight");
  assert.strictEqual(valHandle.getAttribute("aria-valuenow"), String(DATA_MIN + 16));
  key(c, valHandle, "End");
  assert.strictEqual(valHandle.getAttribute("aria-valuenow"), String(DATA_MAX), "End reaches the useful maximum");
  pointer(c, valHandle, "pointerdown", DATA_MAX);
  pointer(c, c.window, "pointermove", 250);
  pointer(c, c.window, "pointerup", 250);
  assertEffectiveTableWidth(c, [230, 250, ACTION_WIDTH]);

  click(c.document.querySelector("th[data-column-index='0'] button.sortable"));
  await waitFor(() => calls.some((line) => /[?&]sort=id(?:&|$)/.test(line)), { desc: "sorted relation request" });
  await waitFor(() => c.document.querySelector("th[data-column-index='0']")?.getAttribute("aria-sort") === "ascending", { desc: "sorted relation render" });
  await waitFor(() => c.document.querySelector("th .column-resizer")?.getAttribute("aria-valuenow") === "230", { desc: "width restored after sort render" });
  assertEffectiveTableWidth(c, [230, 250, ACTION_WIDTH]);

  click(c.document.querySelector(".paginator .page-next"));
  await waitFor(() => calls.some((line) => /[?&]cursor=100(?:&|$)/.test(line)), { desc: "next relation page" });
  await waitFor(() => c.document.querySelector("th .column-resizer")?.getAttribute("aria-valuenow") === "230", { desc: "width restored after page render" });
  assertEffectiveTableWidth(c, [230, 250, ACTION_WIDTH]);

  const persisted = c.getState();
  assert.deepStrictEqual(persisted.dataConsoleLayout.columnWidths['["db","public","t"]'], { id: 230, val: 250 });
  c.close();

  const fresh = await openGrid(persisted);
  assertEffectiveTableWidth(fresh.c, [230, 250, ACTION_WIDTH]);
  assert.strictEqual(fresh.c.document.querySelector("th .column-resizer").getAttribute("aria-valuenow"), "230", "fresh embedded SPA restores per-table widths");
  fresh.c.close();
}

async function main() {
  await scenarioPointerKeyboardPersistenceAndSortIsolation();
  console.log("grid-column-resize.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
