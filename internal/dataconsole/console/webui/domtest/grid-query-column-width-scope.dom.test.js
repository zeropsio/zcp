"use strict";
// Fix (post-#6 review): every SQL query result in a service persisted column
// widths under the SAME storage key ([service, "$result", "result"]) because
// gridColumnWidthKey fell back to that fixed pair whenever there is no tree
// node -- a query result never has one -- and freezeGridColumns restores
// widths by column NAME. So a width set on a column named "id" in one query
// silently applied to an unrelated query that also has an "id" column. The
// key must fold in the result's own column SHAPE (its column names, in
// order) so differently-shaped results never collide, while re-running a
// query that returns the SAME shape still restores its own widths. Real
// tables are unaffected -- their key already carries the unique node path
// segments.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };
const DATA_DEFAULT = 160;

function service() {
  return { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [{ id: "querySQL", enabled: true, readOnly: true, reason: "" }] };
}

// Two differently-shaped queries that happen to share a column named "id" --
// exactly the collision the fix must prevent.
const SHAPES = {
  "SELECT id, name FROM t": ["id", "name"],
  "SELECT id, status FROM t": ["id", "status"],
};

function queryResultFor(stmt) {
  const cols = SHAPES[stmt];
  return jsonRoute({
    columns: cols.map((name) => ({ name, dataType: "text", pk: false, editable: false, reason: "query results are read-only" })),
    rows: [cols.map((_, i) => "v" + i)],
    rowKeyCols: null,
  });
}

function routes(method, p, body) {
  if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service()], allowWrites: true });
  if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
  if (p === "/api/query") return queryResultFor(JSON.parse(body).stmt);
  return null;
}

function runSQL(c, stmt) {
  c.document.getElementById("qtext").value = stmt;
  click(c.document.getElementById("runq"));
}

function widths(c) {
  return Array.from(c.document.querySelectorAll("#qresult table.grid colgroup col"), (col) => Number.parseFloat(col.style.width));
}

function pointer(c, target, type, clientX) {
  target.dispatchEvent(new c.window.MouseEvent(type, { bubbles: true, cancelable: true, clientX }));
}

async function scenarioQueryColumnWidthsScopedByResultShape() {
  const c = buildConsole({ url: "http://localhost/#t=FAKE&svc=db", routes });
  await waitFor(() => c.document.getElementById("querylink"), { desc: "query link render" });
  click(c.document.getElementById("querylink"));
  await waitFor(() => c.document.getElementById("runq"), { desc: "query console render" });

  runSQL(c, "SELECT id, name FROM t");
  await waitFor(() => c.document.getElementById("grid-column-label-1")?.textContent === "name", { desc: "first query grid render" });
  assert.deepStrictEqual(widths(c), [DATA_DEFAULT, DATA_DEFAULT], "fresh query grid starts at the default column width");

  // Widen the "id" column of this query result.
  const idHandle = c.document.querySelectorAll("#qresult th .column-resizer")[0];
  pointer(c, idHandle, "pointerdown", DATA_DEFAULT);
  pointer(c, c.window, "pointermove", 260);
  pointer(c, c.window, "pointerup", 260);
  assert.deepStrictEqual(widths(c), [260, DATA_DEFAULT]);

  // A differently-shaped query result (still has an "id" column) must NOT
  // inherit the width persisted above -- the collision the fix closes.
  runSQL(c, "SELECT id, status FROM t");
  await waitFor(() => c.document.getElementById("grid-column-label-1")?.textContent === "status", { desc: "second query grid render" });
  assert.deepStrictEqual(widths(c), [DATA_DEFAULT, DATA_DEFAULT], "a differently-shaped query result does not inherit another query's column widths");

  // Re-running the FIRST shape still restores its own persisted width.
  runSQL(c, "SELECT id, name FROM t");
  await waitFor(() => c.document.getElementById("grid-column-label-1")?.textContent === "name", { desc: "first query grid re-render" });
  assert.deepStrictEqual(widths(c), [260, DATA_DEFAULT], "re-running a query with the same result shape restores its own persisted width");

  c.close();
}

async function main() {
  await scenarioQueryColumnWidthsScopedByResultShape();
  console.log("grid-query-column-width-scope.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
