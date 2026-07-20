"use strict";

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };
const SERVICE = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [] };
const COLS = [
  { name: "id", dataType: "bigint", pk: true, editable: false, reason: "primary key", sortable: true },
  { name: "name", dataType: "text", pk: false, editable: false, reason: "read-only", sortable: true },
  { name: "state", dataType: "AggregateFunction(sum, UInt64)", pk: false, editable: false, reason: "view-only", sortable: false, sortReason: "aggregate state" },
];

function treeRoute() {
  return jsonRoute({ nodes: [{ name: "events", kind: "tabular", path: { service: "db", segments: ["public", "events"] } }] });
}

async function boot(routes, writeEnabled = false) {
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "table node" });
  click(c.document.querySelector("#tree .node"));
  return c;
}

async function scenarioMutationsPreserveRelationStateAndRefreshCount() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [
      { id: "editCell", enabled: true, readOnly: false, reason: "" },
      { id: "deleteRow", enabled: true, readOnly: false, reason: "" },
      { id: "insertRow", enabled: true, readOnly: false, reason: "" },
    ],
  };
  let countCalls = 0;
  const mutationCalls = [];
  const pageRequests = [];
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return treeRoute();
    if (p.startsWith("/api/table/count")) { countCalls++; return jsonRoute({ count: 50000 }); }
    if (p.startsWith("/api/table")) {
      const u = new URL("http://x" + p);
      pageRequests.push(u);
      const offset = Number(u.searchParams.get("cursor") || 0);
      return jsonRoute({ columns: COLS.slice(0, 2).map((c, i) => Object.assign({}, c, { editable: i === 1 })), rows: [[String(offset + 1), "value"]], rowKeyCols: ["id"], nextCursor: String(offset + 100), numbered: true });
    }
    if (p === "/api/cell" || p === "/api/row") { mutationCalls.push(method + " " + p); return jsonRoute({ affected: 1, key: { id: "new" } }); }
    return null;
  };
  const c = await boot(routes, true);
  await waitFor(() => c.document.querySelector('th[data-column-index="1"] button.sortable'), { desc: "mutable relation grid" });
  click(c.document.querySelector('th[data-column-index="1"] button.sortable'));
  await waitFor(() => c.document.querySelector(".page-number") && Array.from(c.document.querySelectorAll(".page-number")).some((b) => b.textContent === "2"), { desc: "sorted paginator" });
  click(Array.from(c.document.querySelectorAll(".page-number")).find((b) => b.textContent === "2"));
  await waitFor(() => pageRequests.at(-1).searchParams.get("cursor") === "100", { desc: "mutation precondition page two" });

  const assertRefresh = async (before, label) => {
    await waitFor(() => countCalls > before && pageRequests.at(-1).searchParams.get("cursor") === "100" &&
      pageRequests.at(-1).searchParams.get("sort") === "name" && pageRequests.at(-1).searchParams.get("direction") === "asc", { desc: label + " preserves sort/page and refreshes count" });
  };

  let before = countCalls;
  click(c.document.querySelector("tbody td.editable"));
  const input = c.document.querySelector("input.celledit");
  input.value = "edited";
  input.blur();
  await waitFor(() => mutationCalls.includes("POST /api/cell"), { desc: "cell edit applied" });
  await assertRefresh(before, "cell edit");

  before = countCalls;
  click(c.document.querySelector("button.rowdel"));
  await waitFor(() => !c.document.getElementById("modal").classList.contains("hidden"), { desc: "delete confirm" });
  click(c.document.getElementById("modalok"));
  await waitFor(() => mutationCalls.includes("DELETE /api/row"), { desc: "row delete applied" });
  await assertRefresh(before, "row delete");

  before = countCalls;
  click(c.document.getElementById("insertrow"));
  await waitFor(() => c.document.querySelector("#modalbody input[data-col]"), { desc: "insert form" });
  const fields = c.document.querySelectorAll("#modalbody input[data-col]");
  fields[0].value = "new";
  fields[1].value = "value";
  click(c.document.getElementById("modalok"));
  await waitFor(() => mutationCalls.includes("POST /api/row"), { desc: "row insert applied" });
  await assertRefresh(before, "row insert");
  c.close();
}

async function scenarioExactCountSortAndBoundedPages() {
  const pageRequests = [];
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [SERVICE], allowWrites: false });
    if (p.startsWith("/api/tree")) return treeRoute();
    if (p.startsWith("/api/table/count")) return jsonRoute({ count: 50000 });
    if (p.startsWith("/api/table")) {
      const u = new URL("http://x" + p);
      pageRequests.push(u);
      const offset = Number(u.searchParams.get("cursor") || 0);
      const rows = Array.from({ length: 100 }, (_, i) => [String(offset + i + 1), u.searchParams.get("direction") || "none", "42"]);
      return jsonRoute({ columns: COLS, rows, rowKeyCols: ["id"], nextCursor: String(offset + 100), numbered: true });
    }
    return null;
  };
  const c = await boot(routes);
  await waitFor(() => /1.100 of 50,000/.test((c.document.querySelector(".page-range") || {}).textContent || ""), { desc: "exact total range" });

  const sort = c.document.querySelector('th[data-column-index="1"] button.sortable');
  assert.ok(sort, "sortable server-declared column renders a real button");
  assert.strictEqual(sort.closest("th").getAttribute("aria-sort"), "none", "unsorted header exposes aria-sort=none");
  assert.ok(!c.document.querySelector('th[data-column-index="2"] button.sortable'), "server-declared non-sortable aggregate state has no sort button");
  assert.ok(!c.document.querySelector('th[data-column-index="2"]').hasAttribute("aria-sort"),
    "a non-sortable relation column exposes no sorting semantics");
  assert.strictEqual(c.document.querySelectorAll(".paginator .page-number").length <= 7, true, "50k rows still render a bounded page-number window");
  assert.ok(c.document.querySelector(".page-first"), "exact paginator exposes a distinct first-page control");
  assert.ok(c.document.querySelector(".page-last"), "exact paginator exposes a last-page control");

  click(sort);
  await waitFor(() => pageRequests.some((u) => u.searchParams.get("sort") === "name" && u.searchParams.get("direction") === "asc"), { desc: "ascending server sort request" });
  await waitFor(() => c.document.querySelector('th[data-column-index="1"]').getAttribute("aria-sort") === "ascending", { desc: "ascending aria-sort" });
  click(c.document.querySelector('th[data-column-index="1"] button.sortable'));
  await waitFor(() => pageRequests.some((u) => u.searchParams.get("sort") === "name" && u.searchParams.get("direction") === "desc"), { desc: "descending server sort request" });
  await waitFor(() => c.document.querySelector('th[data-column-index="1"]') && c.document.querySelector('th[data-column-index="1"]').getAttribute("aria-sort") === "descending", { desc: "descending page render" });
  assert.strictEqual(pageRequests.at(-1).searchParams.get("cursor"), "0", "sorting resets the global relation page to offset zero");

  const page2 = Array.from(c.document.querySelectorAll(".page-number")).find((b) => b.textContent === "2");
  click(page2);
  await waitFor(() => pageRequests.at(-1).searchParams.get("cursor") === "100", { desc: "numbered page offset" });
  await waitFor(() => /101.200 of 50,000/.test((c.document.querySelector(".page-range") || {}).textContent || ""), { desc: "exact range follows numbered page" });
  c.close();
}

async function scenarioCountFailureFallsBackAndNoPKIsHonest() {
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [SERVICE], allowWrites: false });
    if (p.startsWith("/api/tree")) return treeRoute();
    if (p.startsWith("/api/table/count")) return jsonRoute({ code: "timeout", message: "timeout" }, { status: 504 });
    if (p.startsWith("/api/table")) return jsonRoute({ columns: COLS.slice(1), rows: [["a", "1"]], rowKeyCols: [], nextCursor: "100", bestEffort: true, numbered: true });
    return null;
  };
  const c = await boot(routes);
  await waitFor(() => c.document.querySelector(".paginator.fallback"), { desc: "fallback paginator" });
  assert.ok(/live.*best-effort/i.test(c.document.querySelector("#content").textContent), "a no-PK relation is visibly live/best-effort");
  assert.ok(c.document.querySelector(".page-prev").disabled, "fallback starts with previous disabled");
  assert.ok(!c.document.querySelector(".page-next").disabled, "fallback preserves next from the readable page");
  assert.strictEqual(c.document.querySelectorAll(".page-number").length, 0, "fallback never invents numbered random access without a total");
  c.close();
}

async function scenarioCountCompletionNeverLabelsPendingPageRows() {
  let resolveCount;
  let resolvePageTwo;
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [SERVICE], allowWrites: false });
    if (p.startsWith("/api/tree")) return treeRoute();
    if (p.startsWith("/api/table/count")) return new Promise((resolve) => { resolveCount = resolve; });
    if (p.startsWith("/api/table")) {
      const u = new URL("http://x" + p);
      const offset = Number(u.searchParams.get("cursor") || 0);
      if (offset === 100) return new Promise((resolve) => { resolvePageTwo = resolve; });
      const rows = Array.from({ length: 100 }, (_, i) => [String(i + 1), "first", "1"]);
      return jsonRoute({ columns: COLS, rows, rowKeyCols: ["id"], nextCursor: "100", numbered: true });
    }
    return null;
  };
  const c = await boot(routes);
  await waitFor(() => c.document.querySelector(".paginator.fallback .page-next"), { desc: "fallback next before count" });
  click(c.document.querySelector(".page-next"));
  await waitFor(() => typeof resolvePageTwo === "function", { desc: "page two held" });

  resolveCount(jsonRoute({ count: 500 }));
  await waitFor(() => c.document.querySelector(".paginator.exact"), { desc: "count completes while page two pending" });
  assert.strictEqual(c.document.querySelector(".page-range").textContent, "1–100 of 500",
    "the exact range must continue to describe the still-visible first-page rows");
  assert.strictEqual(c.document.querySelector("tbody td").textContent, "1", "first-page rows remain visible while page two is pending");

  const secondRows = Array.from({ length: 100 }, (_, i) => [String(i + 101), "second", "1"]);
  resolvePageTwo(jsonRoute({ columns: COLS, rows: secondRows, rowKeyCols: ["id"], nextCursor: "200", numbered: true }));
  await waitFor(() => c.document.querySelector(".page-range").textContent === "101–200 of 500", { desc: "page two range lands with rows" });
  assert.strictEqual(c.document.querySelector("tbody td").textContent, "101", "page-two range and rows publish together");
  c.close();
}

async function scenarioPageSizeRetainsExactTotalWithoutRecount() {
  let countCalls = 0;
  let resolveSmallPage;
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [SERVICE], allowWrites: false });
    if (p.startsWith("/api/tree")) return treeRoute();
    if (p.startsWith("/api/table/count")) { countCalls++; return jsonRoute({ count: 500 }); }
    if (p.startsWith("/api/table")) {
      const u = new URL("http://x" + p);
      if (u.searchParams.get("limit") === "25") return new Promise((resolve) => { resolveSmallPage = resolve; });
      const rows = Array.from({ length: 100 }, (_, i) => [String(i + 1), "wide", "1"]);
      return jsonRoute({ columns: COLS, rows, rowKeyCols: ["id"], nextCursor: "100", numbered: true });
    }
    return null;
  };
  const c = await boot(routes);
  await waitFor(() => c.document.querySelector(".page-range").textContent === "1–100 of 500", { desc: "initial exact total" });

  const select = c.document.querySelector(".page-size select");
  select.value = "25";
  select.dispatchEvent(new c.window.Event("change", { bubbles: true }));
  await waitFor(() => typeof resolveSmallPage === "function", { desc: "25-row page held" });
  assert.strictEqual(countCalls, 1, "changing only the window size must reuse the known relation total");
  assert.strictEqual(c.document.querySelector(".page-range").textContent, "1–100 of 500",
    "the old range remains truthful until the smaller page rows land");
  assert.strictEqual(c.document.querySelector(".page-size select").value, "100",
    "displayed page size remains atomic with the displayed rows");

  const rows = Array.from({ length: 25 }, (_, i) => [String(i + 1), "small", "1"]);
  resolveSmallPage(jsonRoute({ columns: COLS, rows, rowKeyCols: ["id"], nextCursor: "25", numbered: true }));
  await waitFor(() => c.document.querySelector(".page-range").textContent === "1–25 of 500", { desc: "small page lands" });
  assert.strictEqual(c.document.querySelector(".page-size select").value, "25", "new page size publishes with its rows");
  assert.strictEqual(countCalls, 1, "page-size response does not trigger a late recount");
  c.close();
}

async function scenarioSharedNonRelationGridsHaveNoSortARIA() {
  const queryService = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "querySQL", enabled: true, readOnly: true, reason: "" }],
  };
  const queryConsole = buildConsole({
    url: "http://localhost/#t=FAKE&svc=db",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [queryService], allowWrites: false });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
      if (p === "/api/query") return jsonRoute({
        columns: [{ name: "answer", dataType: "integer", editable: false, reason: "query results are read-only" }],
        rows: [["42"]], rowKeyCols: null,
      });
      return null;
    },
  });
  await waitFor(() => queryConsole.document.getElementById("querylink"), { desc: "query link" });
  click(queryConsole.document.getElementById("querylink"));
  await waitFor(() => queryConsole.document.getElementById("runq"), { desc: "query console" });
  queryConsole.document.getElementById("qtext").value = "SELECT 42";
  click(queryConsole.document.getElementById("runq"));
  await waitFor(() => queryConsole.document.querySelector("#qresult th"), { desc: "query result header" });
  assert.ok(!queryConsole.document.querySelector("#qresult th").hasAttribute("aria-sort"),
    "a query-result header exposes no relation sorting semantics");
  queryConsole.close();

  const kvService = { hostname: "cache", type: "valkey:single@7", support: "supported", actions: [] };
  const kvConsole = buildConsole({
    url: "http://localhost/#t=FAKE&svc=cache",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [kvService], allowWrites: false });
      if (p.startsWith("/api/tree")) return jsonRoute({
        nodes: [{ name: "hash", kind: "tabular", path: { service: "cache", segments: ["hash"] }, meta: { entryType: "hash" } }],
      });
      if (p.startsWith("/api/table")) return jsonRoute({
        columns: [{ name: "field", dataType: "string", editable: false }], rows: [["one"]], rowKeyCols: ["field"],
      });
      return null;
    },
  });
  await waitFor(() => kvConsole.document.querySelector("#tree .node"), { desc: "kv node" });
  click(kvConsole.document.querySelector("#tree .node"));
  await waitFor(() => kvConsole.document.querySelector("#content th"), { desc: "kv header" });
  assert.ok(!kvConsole.document.querySelector("#content th").hasAttribute("aria-sort"),
    "a KV header exposes no relation sorting semantics");
  kvConsole.close();
}

async function scenarioRowAndCountEpochsClampAfterRefresh() {
  const counts = [];
  const heldRows = [];
  let countCall = 0;
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [SERVICE], allowWrites: false });
    if (p.startsWith("/api/tree")) return treeRoute();
    if (p.startsWith("/api/table/count")) {
      countCall++;
      if (countCall === 1) return new Promise((resolve) => counts.push(resolve));
      if (countCall === 2) return jsonRoute({ count: 50000 });
      return jsonRoute({ count: 50 });
    }
    if (p.startsWith("/api/table")) {
      const u = new URL("http://x" + p);
      const offset = Number(u.searchParams.get("cursor") || 0);
      if (offset === 100 && !u.searchParams.get("sort")) return new Promise((resolve) => heldRows.push(resolve));
      const rows = Array.from({ length: 100 }, (_, i) => [String(offset + i + 1), u.searchParams.get("sort") ? "sorted" : "fresh", "1"]);
      return jsonRoute({ columns: COLS, rows, rowKeyCols: ["id"], nextCursor: offset < 49900 ? String(offset + 100) : "", numbered: true });
    }
    return null;
  };
  const c = await boot(routes);
  await waitFor(() => c.document.querySelector(".page-refresh"), { desc: "relation refresh" });
  click(c.document.querySelector(".page-refresh"));
  await waitFor(() => /50,000/.test((c.document.querySelector(".page-range") || {}).textContent || ""), { desc: "fresh count epoch" });
  counts[0](jsonRoute({ count: 99999 }));
  await new Promise((resolve) => setTimeout(resolve, 30));
  assert.ok(/50,000/.test(c.document.querySelector(".page-range").textContent), "stale count epoch cannot overwrite the refreshed total");

  const page2 = Array.from(c.document.querySelectorAll(".page-number")).find((b) => b.textContent === "2");
  click(page2);
  await waitFor(() => heldRows.length === 1, { desc: "older row request held" });
  click(c.document.querySelector('th[data-column-index="1"] button.sortable'));
  await waitFor(() => /sorted/.test(c.document.querySelector("tbody").textContent), { desc: "newer sorted page" });
  heldRows[0](jsonRoute({ columns: COLS, rows: [["101", "stale", "1"]], rowKeyCols: ["id"], nextCursor: "200", numbered: true }));
  await new Promise((resolve) => setTimeout(resolve, 30));
  assert.ok(/sorted/.test(c.document.querySelector("tbody").textContent), "stale row epoch cannot overwrite newer sorted data");

  click(c.document.querySelector(".page-last"));
  await waitFor(() => /49901.50000 of 50,000/.test(c.document.querySelector(".page-range").textContent), { desc: "last page before shrink" });
  click(c.document.querySelector(".page-refresh"));
  await waitFor(() => /1.50 of 50/.test(c.document.querySelector(".page-range").textContent), { desc: "shrinking total clamps and refetches page zero" });
  c.close();
}

async function main() {
  await scenarioExactCountSortAndBoundedPages();
  await scenarioCountFailureFallsBackAndNoPKIsHonest();
  await scenarioCountCompletionNeverLabelsPendingPageRows();
  await scenarioPageSizeRetainsExactTotalWithoutRecount();
  await scenarioSharedNonRelationGridsHaveNoSortARIA();
  await scenarioRowAndCountEpochsClampAfterRefresh();
  await scenarioMutationsPreserveRelationStateAndRefreshCount();
  console.log("grid-pagination-sort.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
