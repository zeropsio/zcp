"use strict";
// S15 SPA-unification invariants, driven through the REAL render path (fetch /
// postMessage -> app.js -> DOM), never a direct call into app.js internals. These
// pin the canon the unification delivers (plans/dataconsole-contracts-draft-
// 2026-07-16.md §P.4/§P.5, ui-walk.md U-01/U-02/U-03/U-06/U-09/U-10, UI-AUD-03/04):
//   1. ONE grid renderer — a SQL query result reuses the SAME grid as ReadTable
//      (same `.gridwrap table.grid` markup) and renders explicitly read-only.
//   2. Editability is SERVER truth — a Column.editable=false cell (PK) renders
//      locked, a Column.editable=true cell renders with the edit affordance.
//   3. view-only-no-key — an empty rowKeyCols renders the distinct visible label.
//   4. State canon — empty tree renders one "Empty" state; a per-redis-type glyph
//      distinguishes hash vs list in the tree.
//   5. Vector collapse — a vector-bearing point summarizes dims, hides raw floats.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

function vectorRoute(obj) {
  const body = Buffer.from(JSON.stringify(obj), "utf8");
  return {
    status: 200,
    headers: {
      "content-type": "application/octet-stream",
      "x-dataconsole-contenttype": "application/json",
      "x-dataconsole-truncated": "false",
      "x-dataconsole-vector": "true",
      "x-dataconsole-size": String(body.length),
    },
    bodyBytes: body,
  };
}

// 1. A query result renders through the ONE grid renderer, explicitly read-only.
async function scenarioQueryReusesGrid() {
  const service = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [{ id: "querySQL", enabled: true, readOnly: true, reason: "" }] };
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=db",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
      if (p === "/api/query") return jsonRoute({ columns: [{ name: "c", dataType: "int", pk: false, editable: false, reason: "query results are read-only" }], rows: [["1"]], rowKeyCols: null });
      return null;
    },
  });
  await waitFor(() => c.document.getElementById("querylink"), { desc: "query link render" });
  click(c.document.getElementById("querylink"));
  await waitFor(() => c.document.getElementById("runq"), { desc: "query console render" });
  c.document.getElementById("qtext").value = "SELECT 1";
  click(c.document.getElementById("runq"));
  await waitFor(() => c.document.querySelector("#qresult .gridwrap table.grid"), { desc: "query grid render" });
  const qres = c.document.getElementById("qresult");
  assert.ok(qres.querySelector(".gridwrap table.grid"), "query result reuses the shared grid markup (.gridwrap table.grid)");
  assert.strictEqual(qres.querySelectorAll("td.editable").length, 0, "query grid has NO editable cell — editable:false is honored (U-01)");
  assert.ok(qres.querySelector(".toolbar .note"), "query grid shows the explicit read-only note, not a silent read-only grid");
  assert.strictEqual(qres.querySelector("tbody.gridbody td").textContent, "1", "query cell value renders");
  c.close();
}

// 2. Per-column editability is SERVER truth: PK cell locked, value cell editable.
async function scenarioServerTruthEditability() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [
      { id: "editCell", enabled: true, readOnly: false, reason: "" },
      { id: "deleteRow", enabled: true, readOnly: false, reason: "" },
    ],
  };
  const c = buildConsole({
    url: "http://localhost/",
    embedded: true,
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
      if (p.startsWith("/api/table")) return jsonRoute({
        columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }, { name: "val", dataType: "text", pk: false, editable: true, reason: "" }],
        rows: [["1", "hello"]], rowKeyCols: ["id"],
      });
      if (p.startsWith("/api/stat")) return jsonRoute({ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } });
      return null;
    },
  });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid tbody.gridbody td"), { desc: "grid rows" });
  const cells = c.document.querySelectorAll("#content tbody.gridbody tr:first-child td");
  assert.ok(cells[0].classList.contains("locked"), "PK column cell (editable:false) renders LOCKED, not editable");
  assert.ok(!cells[0].classList.contains("editable"), "PK column cell is NOT editable (server truth, not a client guess)");
  assert.ok(cells[1].classList.contains("editable"), "non-PK column cell (editable:true) renders with the edit affordance (U-06)");
  assert.ok(c.document.querySelector("#content .rowdel"), "row-delete affordance renders when the table has a key + delete is enabled");
  c.close();
}

// 3. view-only-no-key: an empty rowKeyCols renders a distinct, visible label.
async function scenarioViewOnlyNoKey() {
  const service = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [{ id: "editCell", enabled: true, readOnly: false, reason: "" }] };
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=db",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "v", kind: "tabular", path: { service: "db", segments: ["public", "v"] } }] });
      if (p.startsWith("/api/table")) return jsonRoute({ columns: [{ name: "a", dataType: "text", pk: false, editable: true, reason: "" }], rows: [["x"]], rowKeyCols: [] });
      if (p.startsWith("/api/stat")) return jsonRoute({ name: "v", kind: "tabular", path: { service: "db", segments: ["public", "v"] } });
      return null;
    },
  });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid"), { desc: "grid render" });
  const badge = c.document.querySelector("#content .toolbar .badge.view-only");
  assert.ok(badge, "a PK-less table renders the distinct view-only badge");
  assert.ok(/no row key/i.test(badge.textContent), "the badge names the reason (no row key), never a shared silence (U-02/D-03)");
  assert.strictEqual(c.document.querySelectorAll("#content td.editable").length, 0, "no cell is editable without a row key");
  assert.strictEqual(c.document.querySelectorAll("#content .rowdel").length, 0, "no row-delete button without a row key");
  c.close();
}

// 4a. Per-redis-type tree glyphs distinguish hash from list (UI-AUD-04/U-03).
async function scenarioTreeTypeGlyphs() {
  const service = { hostname: "kv", type: "valkey:single@7", support: "supported", actions: [] };
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=kv",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [
        { name: "h", kind: "tabular", path: { service: "kv", segments: ["h"] }, meta: { entryType: "hash" } },
        { name: "l", kind: "tabular", path: { service: "kv", segments: ["l"] }, meta: { entryType: "list" } },
      ] });
      return null;
    },
  });
  await waitFor(() => c.document.querySelectorAll("#tree .node .kind").length >= 2, { desc: "kv tree render" });
  const glyphs = Array.from(c.document.querySelectorAll("#tree .node .kind")).map((el) => el.textContent);
  assert.notStrictEqual(glyphs[0], glyphs[1], "a hash and a list get visually distinct glyphs in the tree (not one collapsed kind-glyph)");
  assert.strictEqual(c.document.querySelector("#tree .node .kind").getAttribute("title"), "hash", "the glyph carries the redis type as a title");
  c.close();
}

// 4b. State canon: an empty tree renders one honest "Empty" state (U-10).
async function scenarioEmptyTreeState() {
  const service = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [] };
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=db",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
      return null;
    },
  });
  await waitFor(() => c.document.querySelector("#tree .state.empty"), { desc: "empty tree state" });
  assert.strictEqual(c.document.querySelector("#tree .state.empty").textContent, "Empty", "an empty container renders exactly one 'Empty' state");
  c.close();
}

// 5. Vector collapse: a vector-bearing point summarizes dims, hides raw floats.
async function scenarioVectorCollapse() {
  const service = { hostname: "vectors", type: "qdrant:single@1", support: "view-only", actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }] };
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=vectors",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "point-1", kind: "blob", path: { service: "vectors", segments: ["point-1"] } }] });
      if (p.startsWith("/api/blob")) return vectorRoute({ id: 1, payload: { label: "a" }, vector: [0.1, 0.2, 0.3, 0.4] });
      return null;
    },
  });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content .vectorbox"), { desc: "vector view render" });
  const box = c.document.querySelector("#content .vectorbox");
  assert.ok(/4 dims/.test(box.querySelector(".vecsummary").textContent), "the vector view summarizes the dimension count (4 dims), not a wall of floats");
  const raw = box.querySelector("pre.vecraw");
  assert.ok(raw.classList.contains("hidden"), "the raw float array is collapsed (hidden) by default (UI-AUD-03)");
  assert.ok(/label/.test(box.querySelector("pre.blob:not(.vecraw)").textContent), "the id/payload is shown as pretty JSON alongside the collapsed vector");
  c.close();
}

async function main() {
  await scenarioQueryReusesGrid();
  await scenarioServerTruthEditability();
  await scenarioViewOnlyNoKey();
  await scenarioTreeTypeGlyphs();
  await scenarioEmptyTreeState();
  await scenarioVectorCollapse();
  console.log("unification.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
