"use strict";
// B3 (S2): a truncated value is unreadable in read-only posture -- every grid
// cell ellipsizes (style.css table.grid td) and, before this fix, a
// NON-editable cell (locked/PK, view-only service, query result) had no way
// to see the full value at all: no click, and no title hover either
// (live-proven dead end). Contract:
//   (a) every non-editable data cell opens a read-only value modal on click
//       (modal title = column name, body = the full ESCAPED value in
//       <pre class="blob">, a single OK button -- no Confirm/danger styling);
//   (b) the title stays a STATIC affordance hint ("Click to edit" / the lock
//       reason / "Click to view full value"), NEVER the raw cell value: a
//       value can carry markup, and echoing it into an attribute would put a
//       raw "<script>…" substring into the serialized DOM. That is inert as a
//       title, but it violates the standing XSS invariant (xss.dom.test.js:
//       "no raw <script> in the rendered markup" -- a defense-in-depth rule so
//       no later innerHTML-reparse can ever revive it). The full value is read
//       through the escaped click-view modal, which needs no raw-value title;
//   (c) editable cells are UNCHANGED -- clicking still opens the inline editor.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };
const LONG_VAL = "x".repeat(80);

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

// 1. A locked (PK) cell in a write-enabled grid: click opens the view modal;
//    an editable sibling cell in the SAME row still opens the inline editor.
async function scenarioLockedCellOpensViewModalEditableUnchanged() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "editCell", enabled: true, readOnly: false, reason: "" }],
  };
  const table = {
    columns: [
      { name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" },
      { name: "val", dataType: "text", pk: false, editable: true, reason: "" },
    ],
    rows: [["4", "hello"]], rowKeyCols: ["id"],
  };
  const c = await openGrid(service, table);
  const cells = c.document.querySelectorAll("#content tbody.gridbody td");
  const lockedCell = cells[0];
  const editableCell = cells[1];
  assert.strictEqual(lockedCell.className, "locked", "the PK cell is still visibly locked");
  assert.ok(!lockedCell.classList.contains("editable"), "a locked cell is never also marked editable");
  assert.strictEqual(lockedCell.title, "primary key", "the locked cell's title is its lock reason, never the raw value");

  click(lockedCell);
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "locked-cell click opens a modal" });
  assert.strictEqual(c.document.getElementById("modaltitle").textContent, "id", "the view modal's title is the column name");
  const pre = c.document.querySelector("#modalbody pre.blob");
  assert.ok(pre, "the view modal body is a <pre class=\"blob\">");
  assert.strictEqual(pre.textContent, "4", "the view modal shows the full cell value");
  assert.strictEqual(c.document.getElementById("modalcancel").classList.contains("hidden"), true, "the view modal has no Cancel -- single OK button, no Confirm semantics");
  assert.strictEqual(c.document.getElementById("modalok").textContent, "OK", "the view modal's sole button reads OK, not Confirm");
  assert.strictEqual(c.document.getElementById("modalok").classList.contains("danger"), false, "the view modal's OK button carries no destructive styling");

  click(c.document.getElementById("modalok"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "OK closes the view modal" });

  // The editable sibling cell still opens the inline editor, unchanged.
  click(editableCell);
  await waitFor(() => editableCell.querySelector("input.celledit"), { desc: "editable cell still opens the inline editor" });
  c.close();
}

// 2. A cell in a grid with editing entirely off (view-only service) is also
//    click-to-view -- not just the "locked" (PK-in-write-mode) tier.
async function scenarioViewOnlyServiceCellOpensViewModal() {
  const service = {
    hostname: "ch", type: "clickhouse:single@1", support: "view-only",
    actions: [],
  };
  const table = {
    columns: [{ name: "val", dataType: "text", pk: false, editable: false, reason: "service is view-only" }],
    rows: [["plain"]], rowKeyCols: [],
  };
  const c = await openGrid(service, table);
  const cell = c.document.querySelector("#content tbody.gridbody td");
  assert.ok(!cell.classList.contains("editable"), "a view-only-service cell is never editable");
  assert.strictEqual(cell.title, "Click to view full value", "a view-only cell hints the click-to-view path");
  click(cell);
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "view-only-service cell click opens a modal" });
  assert.strictEqual(c.document.querySelector("#modalbody pre.blob").textContent, "plain", "the modal shows the full value");
  c.close();
}

// 3. A long value on a NON-editable cell is fully readable via the click-view
//    modal (the full text, not truncated) -- the read path never depends on a
//    raw-value title.
async function scenarioLongValueReadableViaModal() {
  const service = {
    hostname: "ch", type: "clickhouse:single@1", support: "view-only", actions: [],
  };
  const table = {
    columns: [{ name: "val", dataType: "text", pk: false, editable: false, reason: "" }],
    rows: [[LONG_VAL]], rowKeyCols: [],
  };
  const c = await openGrid(service, table);
  const cell = c.document.querySelector("#content tbody.gridbody td");
  assert.strictEqual(cell.title, "Click to view full value", "the long-value title is the static hint, never the raw value");
  click(cell);
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "long-value cell click opens the view modal" });
  assert.strictEqual(c.document.querySelector("#modalbody pre.blob").textContent, LONG_VAL, "the view modal shows the complete long value");
  c.close();
}

// 4. A short editable value's title is the click-to-edit hint.
async function scenarioShortValueKeepsClickHint() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "editCell", enabled: true, readOnly: false, reason: "" }],
  };
  const table = {
    columns: [
      { name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" },
      { name: "val", dataType: "text", pk: false, editable: true, reason: "" },
    ],
    rows: [["1", "hi"]], rowKeyCols: ["id"],
  };
  const c = await openGrid(service, table);
  const cell = c.document.querySelectorAll("#content tbody.gridbody td")[1];
  assert.strictEqual(cell.title, "Click to edit", "a short editable value keeps the click-to-edit hint");
  c.close();
}

// 5. Security: a value carrying HTML-special characters never lands as a raw
//    substring in the serialized cell markup (title stays a static hint), and
//    the click-view modal shows it ESCAPED -- aligned with xss.dom.test.js's
//    standing "no raw <script> in the markup" invariant, no per-feature carve-out.
async function scenarioHTMLSpecialValueNeverRawInMarkup() {
  const service = {
    hostname: "ch", type: "clickhouse:single@1", support: "view-only", actions: [],
  };
  const evil = '<script>window.__xssFired=true</script><img src=x onerror="window.__xssFired=true"> ' + "pad".repeat(30);
  const table = {
    columns: [{ name: "val", dataType: "text", pk: false, editable: false, reason: "" }],
    rows: [[evil]], rowKeyCols: [],
  };
  const c = await openGrid(service, table);
  c.window.__xssFired = false;
  const grid = c.document.querySelector("#content .gridwrap");
  const cell = c.document.querySelector("#content tbody.gridbody td");
  assert.strictEqual(cell.title, "Click to view full value", "the title is the static hint, not the raw markup-bearing value");
  assert.ok(!grid.innerHTML.includes("<script>"), "no raw <script> substring in the serialized grid markup (title carries no raw value)");
  assert.strictEqual(cell.textContent, evil, "the cell's textContent still recovers the raw value exactly");

  click(cell);
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "evil-value cell click opens the view modal" });
  const body = c.document.getElementById("modalbody");
  assert.strictEqual(body.querySelector("pre.blob").textContent, evil, "the view modal recovers the full value via textContent");
  assert.ok(!body.innerHTML.includes("<script>"), "no raw <script> substring in the view-modal markup either");
  assert.strictEqual(body.querySelectorAll("script").length, 0, "no live <script> element in the view modal");
  assert.strictEqual(body.querySelectorAll("img[onerror]").length, 0, "no live onerror element in the view modal");
  assert.strictEqual(c.window.__xssFired, false, "the payload never executes");
  c.close();
}

async function main() {
  await scenarioLockedCellOpensViewModalEditableUnchanged();
  await scenarioViewOnlyServiceCellOpensViewModal();
  await scenarioLongValueReadableViaModal();
  await scenarioShortValueKeepsClickHint();
  await scenarioHTMLSpecialValueNeverRawInMarkup();
  console.log("cell-full-view.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
