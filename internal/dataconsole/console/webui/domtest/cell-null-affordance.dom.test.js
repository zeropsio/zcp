"use strict";
// B7 (S3): explicit NULL affordance in the tabular cell editor. Clearing a
// cell's <input> and blurring commits an empty STRING -- SQL NULL was
// unreachable from the UI (live-confirmed on both tabular engines), even
// though the server already handles it correctly: server.go's decode() uses
// json.Decoder.UseNumber() (CellEdit.NewValue is `any`), a JSON `null`
// decodes to a Go nil regardless, and tabular.go's bindArg(nil) passes it
// straight through to database/sql, which binds a nil arg as SQL NULL
// natively -- EditCell's `UPDATE ... SET col = ?` already round-trips NULL
// correctly. The gap was purely client-side: nothing could ever SEND null.
//
// Fix: a small ghost "∅ NULL" button next to the inline editor's <input> (a
// *sibling* of the input, tab-reachable) commits with a true JSON null
// newValue instead of reading input.value. Scoped to the TABULAR (non-KV)
// path only -- a redis field has no NULL concept (you DEL it, a different
// operation), so no null button renders in a KV grid.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function openEditableGrid() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "editCell", enabled: true, readOnly: false, reason: "" }],
  };
  const cellEdits = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }, { name: "val", dataType: "text", pk: false, editable: true, reason: "" }],
      rows: [["1", "hello"]], rowKeyCols: ["id"],
    });
    if (method === "POST" && p === "/api/cell") { cellEdits.push(JSON.parse(body)); return jsonRoute({ statement: "UPDATE", affected: 1 }); }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content td.editable"), { desc: "editable cell render" });
  return { c, cellEdits };
}

// 1. Clicking the NULL ghost button commits a true JSON null (not "").
async function scenarioNullButtonCommitsJSONNull() {
  const { c, cellEdits } = await openEditableGrid();
  const cell = c.document.querySelector("#content td.editable");
  click(cell);
  await waitFor(() => cell.querySelector("input.celledit"), { desc: "inline editor opens" });
  const nullBtn = cell.querySelector("button.cellnull");
  assert.ok(nullBtn, "the inline editor renders a NULL ghost button for a tabular cell");
  assert.ok(nullBtn.classList.contains("ghost"), "the NULL button carries the ghost style");
  assert.strictEqual(nullBtn.tabIndex, 0, "the NULL button is tab-reachable");

  click(nullBtn);
  await waitFor(() => cellEdits.length === 1, { desc: "the NULL button commits a request" });
  assert.strictEqual(Object.prototype.hasOwnProperty.call(cellEdits[0], "newValue"), true, "the request carries a newValue field");
  assert.strictEqual(cellEdits[0].newValue, null, "the NULL button sends a true JSON null, not an empty string");
  await waitFor(() => cell.textContent === "∅", { desc: "the cell renders the null presentation (∅) afterward" });
  c.close();
}

// 2. A NULL commit that fails the honest error path (e.g. a NOT NULL column)
//    is reported like any other rejected edit -- the cell keeps its old value.
async function scenarioNullRejectionReportsHonestly() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "editCell", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }, { name: "val", dataType: "text", pk: false, editable: true, reason: "" }],
      rows: [["1", "hello"]], rowKeyCols: ["id"],
    });
    if (method === "POST" && p === "/api/cell") return jsonRoute({ code: "invalid", message: "null value in column \"val\" violates not-null constraint" }, { status: 400 });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content td.editable"), { desc: "editable cell render" });
  const cell = c.document.querySelector("#content td.editable");
  click(cell);
  await waitFor(() => cell.querySelector("button.cellnull"), { desc: "NULL button renders" });
  click(cell.querySelector("button.cellnull"));
  await waitFor(() => cell.textContent === "hello", { desc: "a rejected NULL commit restores the old value" });
  assert.ok(c.document.querySelector(".toast.bad"), "a rejected NULL commit reports failure honestly (not silently swallowed)");
  c.close();
}

// 3. A KV (non-tabular) cell editor renders NO null button -- redis fields
//    have no NULL concept (DEL is a different operation).
async function scenarioKVCellEditorHasNoNullButton() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [{ id: "editKVEntry", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "h1", kind: "tabular", path: { service: "cache", segments: ["h1"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [{ name: "field", dataType: "string", pk: true, editable: false, reason: "" }, { name: "value", dataType: "string", pk: false, editable: true, reason: "" }],
      rows: [["f1", "v1"]], rowKeyCols: ["field"],
    });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content td.editable"), { desc: "editable cell render" });
  const cell = c.document.querySelector("#content td.editable");
  click(cell);
  await waitFor(() => cell.querySelector("input.celledit"), { desc: "inline editor opens" });
  assert.strictEqual(cell.querySelector("button.cellnull"), null, "a KV entry cell editor renders no NULL button");
  c.close();
}

// 4. Escape/blur semantics are unchanged by the new button (regression guard
//    on cell-edit-escape.dom.test.js's contract).
async function scenarioEscapeStillCancelsUnaffected() {
  const { c } = await openEditableGrid();
  const cell = c.document.querySelector("#content td.editable");
  click(cell);
  await waitFor(() => cell.querySelector("input.celledit"), { desc: "inline editor opens" });
  const input = cell.querySelector("input.celledit");
  input.value = "typed-but-cancelled";
  const w = c.window;
  input.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  input.dispatchEvent(new w.FocusEvent("blur"));
  await new Promise((resolve) => setTimeout(resolve, 30));
  assert.strictEqual(cell.textContent, "hello", "Escape still restores the original value with the NULL button present");
  c.close();
}

// 5. Mechanism pin: mousedown on the NULL button prevents its default (a real
//    browser shifts focus -- and so fires blur -- on mousedown BEFORE click;
//    without this, clicking NULL would first commit whatever is still in the
//    input via blur, racing the null commit). jsdom does not itself simulate
//    mousedown-triggered focus shift (verified empirically: dispatching
//    mousedown never moves jsdom's document.activeElement or fires blur), so
//    the downstream race cannot be reproduced here -- this pins the DEFENSIVE
//    MECHANISM directly instead.
async function scenarioNullButtonMousedownPreventsDefault() {
  const { c } = await openEditableGrid();
  const cell = c.document.querySelector("#content td.editable");
  click(cell);
  await waitFor(() => cell.querySelector("button.cellnull"), { desc: "NULL button renders" });
  const w = c.window;
  const ev = new w.MouseEvent("mousedown", { bubbles: true, cancelable: true });
  cell.querySelector("button.cellnull").dispatchEvent(ev);
  assert.strictEqual(ev.defaultPrevented, true, "mousedown on the NULL button prevents default, so it never steals focus (and so never fires blur) before its click commits");
  c.close();
}

async function main() {
  await scenarioNullButtonCommitsJSONNull();
  await scenarioNullRejectionReportsHonestly();
  await scenarioKVCellEditorHasNoNullButton();
  await scenarioEscapeStillCancelsUnaffected();
  await scenarioNullButtonMousedownPreventsDefault();
  console.log("cell-null-affordance.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
