"use strict";
// B4 (S2): content generation guard. state.reopen() (re-fired by
// applyWriteMode) re-fetches the previously-open blob and, before this fix,
// UNCONDITIONALLY overwrote #content when it resolved -- if the user had
// since navigated elsewhere, their new view was clobbered by the stale
// reopen and stayed clobbered (no further event un-clobbers it). Mirrors the
// tree's treeGen idiom (tree-race.dom.test.js) for #content: a monotonic
// contentGen minted by every content-level render entry (openBlob, openTable,
// openQuery, openSearch, selectService's placeholder), checked after every
// await before touching #content -- including maybeTTL's own async TTL-bar
// append, a second, independent site that can append onto a since-superseded
// view.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

// 1. The RED scenario as specified: interleave a slow reopen (of a blob) with
//    a subsequent openTable navigation -- the table view must survive.
async function scenarioSlowReopenMustNotClobberSubsequentNavigation() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }],
  };
  const blobResolvers = [];
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({
      nodes: [
        { name: "f.txt", kind: "blob", path: { service: "db", segments: ["f.txt"] }, meta: { size: 5 } },
        { name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } },
      ],
    });
    if (p.startsWith("/api/blob")) return new Promise((resolve) => blobResolvers.push(resolve));
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [{ name: "val", dataType: "text", pk: false, editable: false, reason: "" }],
      rows: [["row1"]], rowKeyCols: [],
    });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: false, service: "db" });
  await waitFor(() => c.document.querySelectorAll("#tree .node").length === 2, { desc: "tree nodes render" });

  // Open the blob -- first /api/blob call -- and resolve it so state.reopen
  // is set to this blob's opener.
  click(c.document.querySelectorAll("#tree .node")[0]);
  await waitFor(() => blobResolvers.length === 1, { desc: "first blob fetch in flight" });
  blobResolvers[0](blobRoute("hello", { contentType: "text/plain" }));
  await waitFor(() => c.document.querySelector("#content .toolbar"), { desc: "blob view renders" });

  // A write-mode confirmation re-fires state.reopen() -- the SAME blob opener
  // again -- but this second fetch hangs.
  hostPostMessage(c.window, { type: "dataconsole-write-mode", writeEnabled: true });
  await waitFor(() => blobResolvers.length === 2, { desc: "reopen's (second) blob fetch in flight" });
  await waitFor(() => c.document.querySelectorAll("#tree .node").length === 2, { desc: "tree refresh after write mode" });

  // Before it resolves, the user navigates to the table.
  click(c.document.querySelectorAll("#tree .node")[1]);
  await waitFor(() => c.document.querySelector("#content table.grid"), { desc: "table view renders" });

  // NOW the stale reopen resolves -- it must NOT clobber the table view.
  blobResolvers[1](blobRoute("hello", { contentType: "text/plain" }));
  await new Promise((resolve) => setTimeout(resolve, 50));

  assert.ok(c.document.querySelector("#content table.grid"), "the table view must survive a stale reopen's late resolution");
  assert.strictEqual(c.document.getElementById("dlblob"), null, "a stale blob reopen must not re-render the blob toolbar over the table view");
  c.close();
}

// 2. maybeTTL's own async tail (a SEPARATE await, after the main blob render
//    already completed) must not append a TTL bar onto a since-superseded
//    view either.
async function scenarioStaleMaybeTTLMustNotAppendToSupersededView() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }, { id: "setTTL", enabled: true, readOnly: false, reason: "" }],
  };
  const k1StatResolvers = [];
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({
      nodes: [
        { name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { size: 5 } },
        { name: "k2", kind: "blob", path: { service: "cache", segments: ["k2"] }, meta: { size: 5 } },
      ],
    });
    if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    if (p.startsWith("/api/stat")) {
      // k1's stat call hangs (simulating the slow tail); k2's resolves normally
      // so its OWN (legitimate, current-view) TTL bar renders.
      const segs = new URL("http://x" + p).searchParams.get("segs") || "";
      if (segs.includes("k1")) return new Promise((resolve) => k1StatResolvers.push(resolve));
      return jsonRoute({ name: "k2", kind: "blob", path: { service: "cache", segments: ["k2"] }, meta: { ttlSeconds: null } });
    }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.querySelectorAll("#tree .node").length === 2, { desc: "tree nodes render" });

  // Open k1 -- its maybeTTL /api/stat fetch hangs.
  click(c.document.querySelectorAll("#tree .node")[0]);
  await waitFor(() => k1StatResolvers.length === 1, { desc: "k1's TTL stat fetch in flight" });

  // Navigate to k2 before k1's stat resolves -- k2's own TTL bar renders
  // normally (proves the guard doesn't just suppress ALL ttl bars).
  click(c.document.querySelectorAll("#tree .node")[1]);
  await waitFor(() => c.document.querySelectorAll("#content .ttlbar").length === 1, { desc: "k2's own TTL bar renders" });

  // k1's stale stat resolves now -- it must not append a SECOND TTL bar (for
  // k1) onto k2's (the current) view.
  k1StatResolvers[0](jsonRoute({ name: "k1", kind: "blob", path: { service: "cache", segments: ["k1"] }, meta: { ttlSeconds: null } }));
  await new Promise((resolve) => setTimeout(resolve, 50));

  assert.strictEqual(c.document.querySelectorAll("#content .ttlbar").length, 1, "a stale maybeTTL completion must not append a second TTL bar onto a superseded view");
  c.close();
}

async function runStaleMutationCompletionCase(kind) {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [
      { id: "readBlob", enabled: true, readOnly: true, reason: "" },
      { id: "editCell", enabled: true, readOnly: false, reason: "" },
      { id: "deleteRow", enabled: true, readOnly: false, reason: "" },
      { id: "insertRow", enabled: true, readOnly: false, reason: "" },
    ],
  };
  let resolveMutation;
  let browseCalls = 0;
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [
      { name: "table-a", kind: "tabular", path: { service: "db", segments: ["public", "table_a"] } },
      { name: "new.txt", kind: "blob", path: { service: "db", segments: ["new.txt"] }, meta: { size: 3 } },
    ] });
    if (p.startsWith("/api/table/count")) { browseCalls++; return jsonRoute({ count: 1 }); }
    if (p.startsWith("/api/table")) {
      browseCalls++;
      return jsonRoute({
        columns: [
          { name: "id", dataType: "integer", pk: true, editable: false, reason: "primary key", sortable: true },
          { name: "value", dataType: "text", editable: true, sortable: true },
        ],
        rows: [["1", "old"]], rowKeyCols: ["id"], numbered: true,
      });
    }
    if ((kind === "edit" && p === "/api/cell") ||
        ((kind === "delete" || kind === "insert") && p === "/api/row")) {
      return new Promise((resolve) => { resolveMutation = resolve; });
    }
    if (p.startsWith("/api/blob")) return blobRoute("new", { contentType: "text/plain" });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelectorAll("#tree .node").length === 2, { desc: "table and blob nodes" });
  click(c.document.querySelectorAll("#tree .node")[0]);
  await waitFor(() => c.document.querySelector("tbody td.editable"), { desc: "editable table" });
  if (kind === "edit") {
    click(c.document.querySelector("tbody td.editable"));
    const input = c.document.querySelector("input.celledit");
    input.value = "new";
    input.blur();
  } else if (kind === "delete") {
    click(c.document.querySelector("button.rowdel"));
    await waitFor(() => !c.document.getElementById("modal").classList.contains("hidden"), { desc: "delete confirmation" });
    click(c.document.getElementById("modalok"));
  } else {
    click(c.document.getElementById("insertrow"));
    await waitFor(() => c.document.querySelector("#modalbody input[data-col]"), { desc: "insert form" });
    const fields = c.document.querySelectorAll("#modalbody input[data-col]");
    fields[0].value = "2";
    fields[1].value = "inserted";
    click(c.document.getElementById("modalok"));
  }
  await waitFor(() => typeof resolveMutation === "function", { desc: kind + " mutation held" });

  click(c.document.querySelectorAll("#tree .node")[1]);
  await waitFor(() => /^new\.txt$/.test((c.document.querySelector("#content .toolbar b") || {}).textContent || ""), { desc: "newer blob view" });
  const callsBeforeCompletion = browseCalls;
  resolveMutation(jsonRoute({ affected: 1, key: { id: "2" } }));
  await new Promise((resolve) => setTimeout(resolve, 50));

  assert.strictEqual((c.document.querySelector("#content .toolbar b") || {}).textContent, "new.txt",
    "a stale " + kind + " completion must preserve the newer blob view");
  assert.strictEqual(browseCalls, callsBeforeCompletion,
    "a stale " + kind + " completion must not issue an old relation row or count reload");
  c.close();
}

async function scenarioStaleMutationsMustNotReloadOverNewerView() {
  for (const kind of ["edit", "delete", "insert"]) await runStaleMutationCompletionCase(kind);
}

async function main() {
  await scenarioSlowReopenMustNotClobberSubsequentNavigation();
  await scenarioStaleMaybeTTLMustNotAppendToSupersededView();
  await scenarioStaleMutationsMustNotReloadOverNewerView();
  console.log("content-generation-guard.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
