"use strict";
// C1: the grid's "Load more…" pagination (gridLoadMore) awaited its next page
// with NO content-generation check, then unconditionally appended the result
// onto whatever tbody a fresh content.querySelector("tbody.gridbody") found --
// re-queried AFTER the await. If the user opens table A, clicks Load more, and
// navigates to table B before A's page resolves, #content has already been
// replaced with B's grid (a brand-new <tbody>) by the time A's page lands --
// but the stale continuation still finds SOME tbody (B's) via that fresh query
// and appends A's rows onto it, and rewires a NEW "Load more" button (for A's
// pagination cursor) into B's view too. Fix: snapshot the content generation
// AND the exact original <tbody> element at pagination start (before the
// await) -- on resolve, drop the append unless the generation is unchanged
// AND that specific tbody is still connected to the document.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function scenarioStaleLoadMoreMustNotAppendUnderADifferentTable() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported", actions: [],
  };
  const loadMoreResolvers = [];
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({
      nodes: [
        { name: "a", kind: "tabular", path: { service: "db", segments: ["a"] } },
        { name: "b", kind: "tabular", path: { service: "db", segments: ["b"] } },
      ],
    });
    if (p.startsWith("/api/table")) {
      const u = new URL("http://x" + p);
      const segs = JSON.parse(u.searchParams.get("segs") || "[]");
      const cursor = u.searchParams.get("cursor");
      const col = [{ name: "val", dataType: "text", pk: false, editable: false, reason: "" }];
      if (segs[0] === "a" && !cursor) return jsonRoute({ columns: col, rows: [["a-row1"]], rowKeyCols: [], nextCursor: "c1" });
      if (segs[0] === "a" && cursor === "c1") return new Promise((resolve) => loadMoreResolvers.push(resolve));
      if (segs[0] === "b") return jsonRoute({ columns: col, rows: [["b-row1"]], rowKeyCols: [] });
      return null;
    }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelectorAll("#tree .node").length === 2, { desc: "tree nodes render" });

  // Open table A -- it has a nextCursor, so a "Load more" button renders.
  click(c.document.querySelectorAll("#tree .node")[0]);
  await waitFor(() => c.document.querySelector(".loadmore"), { desc: "A's load-more button renders" });
  assert.strictEqual(c.document.querySelector("#content tbody.gridbody tr").textContent, "a-row1", "precondition: A's row is showing");

  // Click Load more -- A's next page hangs.
  click(c.document.querySelector(".loadmore"));
  await waitFor(() => loadMoreResolvers.length === 1, { desc: "A's load-more fetch in flight" });

  // Before it resolves, navigate to table B.
  click(c.document.querySelectorAll("#tree .node")[1]);
  await waitFor(() => /^b$/.test((c.document.querySelector("#content .toolbar b") || {}).textContent || ""), { desc: "B's table view renders" });
  const bRowCountBefore = c.document.querySelectorAll("#content tbody.gridbody tr").length;
  assert.strictEqual(bRowCountBefore, 1, "precondition: B's grid shows exactly its own one row");

  // NOW A's stale load-more resolves.
  loadMoreResolvers[0](jsonRoute({ columns: [{ name: "val", dataType: "text", pk: false, editable: false, reason: "" }], rows: [["a-row2"]], rowKeyCols: [] }));
  await new Promise((resolve) => setTimeout(resolve, 50));

  assert.ok(/^b$/.test(c.document.querySelector("#content .toolbar b").textContent), "B's table view must still be showing after A's stale load-more resolves");
  const rows = Array.from(c.document.querySelectorAll("#content tbody.gridbody tr")).map((tr) => tr.textContent);
  assert.strictEqual(rows.length, 1, "B's grid must still have exactly one row; got: " + JSON.stringify(rows));
  assert.strictEqual(rows[0], "b-row1", "B's grid must show only its own row");
  assert.ok(!rows.includes("a-row2"), "A's stale load-more row must never appear under B's grid");
  c.close();
}

async function main() {
  await scenarioStaleLoadMoreMustNotAppendUnderADifferentTable();
  console.log("grid-loadmore-race.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
