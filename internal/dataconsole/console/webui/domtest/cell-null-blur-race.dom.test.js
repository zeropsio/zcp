"use strict";
// C5: two bugs in editCell's NULL affordance (B7, cell-null-affordance.dom.test.js).
//
// (a) Keyboard operability: focusing the NULL button (e.g. via Tab) fires the
// input's blur BEFORE the button's own click/keydown -- a real browser always
// fires blur on the outgoing element first. The existing mousedown
// preventDefault only protects a MOUSE click (it stops the browser's default
// focus-shift on mousedown, so blur never fires for THAT path); it does
// nothing for keyboard focus travel. With an UNCHANGED input, that blur's
// commit() treats it as a no-op and does `td.textContent = fmt(oldVal)`,
// which wipes out BOTH the input AND the (still being tabbed-to) NULL button
// -- so a keyboard-only user can never reach it. With a CHANGED input, that
// blur commits the typed STRING as a real write, and the button's own
// (still-live) click then fires a SECOND, racing null write.
//
// (b) The unchanged-value comparison collapsed null and "" onto the same
// state: an oldVal of null rendered the input as "" (fmt/oldVal==null->""),
// and doCommit's own `nv === String(oldVal == null ? "" : oldVal)` check
// then read a re-typed "" as EQUAL to the null it started from -- so
// NULL -> "" (a real, distinct edit) could never be committed at all.
//
// Fix: input.onblur checks `e.relatedTarget === nullBtn` and defers to the
// button's own click instead of committing first; the unchanged check
// requires oldVal to be non-null before comparing to a string at all.
// jsdom fidelity note: a raw mousedown/click dispatch does NOT itself shift
// focus in jsdom (established by cell-null-affordance.dom.test.js's own
// mechanism-pin scenario) -- but an explicit `.focus()` call DOES fire a real
// blur with `relatedTarget` set correctly (verified empirically), so this
// test calls `.focus()` directly to stand in for what Tab (or a real
// browser's un-prevented mousedown) would do, then dispatches the click
// exactly as a user's mouse-up would.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function openEditableGrid(rows) {
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
      rows, rowKeyCols: ["id"],
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

// 1. An UNCHANGED input: focus moving to the NULL button must not remove it
//    from the DOM (keyboard-reachability) and must not itself commit.
async function scenarioNullButtonSurvivesUnchangedInputBlur() {
  const { c, cellEdits } = await openEditableGrid([["1", "hello"]]);
  const cell = c.document.querySelector("#content td.editable");
  click(cell);
  await waitFor(() => cell.querySelector("input.celledit"), { desc: "inline editor opens" });
  const nullBtn = cell.querySelector("button.cellnull");
  assert.ok(nullBtn, "precondition: the NULL button renders");

  nullBtn.focus(); // fires input.onblur with relatedTarget === nullBtn, exactly as Tab would
  assert.strictEqual(cell.querySelector("button.cellnull"), nullBtn, "the NULL button must survive an unchanged-input blur when focus is moving to it");
  assert.strictEqual(cell.querySelector("input.celledit") !== null, true, "the input must still be present too (nothing was committed/torn down)");
  assert.strictEqual(cellEdits.length, 0, "focus moving to the NULL button must not itself commit a request");

  click(nullBtn);
  await waitFor(() => cellEdits.length === 1, { desc: "activating the NULL button (now reachable) commits" });
  assert.strictEqual(cellEdits[0].newValue, null, "the NULL button's own commit carries a true null");
  c.close();
}

// 2. A CHANGED input, then the NULL button: exactly ONE request fires, and it
//    is the null commit -- not a string-write racing the null-write.
async function scenarioChangedInputThenNullCommitsExactlyOnce() {
  const { c, cellEdits } = await openEditableGrid([["1", "hello"]]);
  const cell = c.document.querySelector("#content td.editable");
  click(cell);
  await waitFor(() => cell.querySelector("input.celledit"), { desc: "inline editor opens" });
  const input = cell.querySelector("input.celledit");
  input.value = "typed-then-null";
  const nullBtn = cell.querySelector("button.cellnull");

  // Focus moves from the (changed) input to the NULL button (as a real
  // mousedown or Tab would, firing blur first), then the button is activated.
  nullBtn.focus();
  click(nullBtn);
  await waitFor(() => cellEdits.length >= 1, { desc: "at least one request fires" });
  await new Promise((resolve) => setTimeout(resolve, 30)); // let a second, racing request land if the bug is present
  assert.strictEqual(cellEdits.length, 1, "changing the input then activating NULL must send exactly ONE request, not a string-write racing the null-write; got " + JSON.stringify(cellEdits));
  assert.strictEqual(cellEdits[0].newValue, null, "the single request sent is the NULL commit, not the stale typed string");
  c.close();
}

// 3. A NULL cell, committed as a real empty string: distinct from "unchanged".
async function scenarioNullCellTypedEmptyStringCommitsAsRealEdit() {
  const { c, cellEdits } = await openEditableGrid([["1", null]]);
  const cell = c.document.querySelector("#content td.editable");
  assert.strictEqual(cell.textContent, "∅", "precondition: the cell renders the NULL presentation");
  click(cell);
  await waitFor(() => cell.querySelector("input.celledit"), { desc: "inline editor opens" });
  const input = cell.querySelector("input.celledit");
  assert.strictEqual(input.value, "", "a NULL cell's editor opens with an empty input, not the literal word null");

  const w = c.window;
  input.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  await waitFor(() => cellEdits.length === 1, { desc: "committing empty string on a NULL cell sends a request" });
  assert.strictEqual(Object.prototype.hasOwnProperty.call(cellEdits[0], "newValue"), true, "the request carries a newValue field");
  assert.strictEqual(cellEdits[0].newValue, "", "the committed value is a real empty string, not swallowed as a no-op on a null cell");
  c.close();
}

// 4. Regression guard: a normal blur (unrelated to the NULL button) still
//    commits exactly as before -- the relatedTarget check must not suppress
//    legitimate blur-commits.
async function scenarioNormalBlurStillCommitsUnaffected() {
  const { c, cellEdits } = await openEditableGrid([["1", "hello"]]);
  const cell = c.document.querySelector("#content td.editable");
  click(cell);
  await waitFor(() => cell.querySelector("input.celledit"), { desc: "inline editor opens" });
  const input = cell.querySelector("input.celledit");
  input.value = "changed-for-real";
  input.blur();
  await waitFor(() => cellEdits.length === 1, { desc: "a normal blur (not moving to the NULL button) still commits" });
  assert.strictEqual(cellEdits[0].newValue, "changed-for-real", "the legitimate blur-commit path still sends the typed value");
  c.close();
}

async function main() {
  await scenarioNullButtonSurvivesUnchangedInputBlur();
  await scenarioChangedInputThenNullCommitsExactlyOnce();
  await scenarioNullCellTypedEmptyStringCommitsAsRealEdit();
  await scenarioNormalBlurStillCommitsUnaffected();
  console.log("cell-null-blur-race.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
