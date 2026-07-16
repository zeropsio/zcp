"use strict";
// Durable invariant: untrusted data the SPA renders into the DOM is always
// HTML-safe, at every call site tested here — service hostname (rail),
// tree/table node name, grid column name, grid row/cell value, and blob
// name (toolbar). This must hold BEFORE and AFTER the S15 renderer
// unification (plans/dataconsole-excellence-program-2026-07-16.md §6 DD-7,
// §7 S15a) — it encodes a security contract, not today's markup shape.
//
// Each scenario drives the REAL render path (fetch/postMessage -> app.js ->
// DOM), never a direct call into app.js internals, then asserts three
// independent things a naive `innerHTML +=` regression would break:
//   1. no live <script>/onerror-bearing element exists in the rendered
//      subtree (the payload's detonator never fires: window.__xssFired
//      stays false — the strongest possible proof of "did not execute"),
//   2. the raw dangerous substring is absent from the serialized markup,
//   3. the ORIGINAL value is still recoverable via .textContent (escaping
//      must not corrupt or drop the value, only neutralize it as markup).

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute } = require("./harness");

// One payload covering every fragment the brief calls out: <script>, a
// quote, an ampersand, and an <img onerror> detonator.
const XSS_PAYLOAD = '<script>window.__xssFired=true</script><img src=x onerror="window.__xssFired=true"> "quoted" & ampersand';

function assertSafelyRendered(window, root, label) {
  assert.strictEqual(window.__xssFired, false, label + ": no injected handler executed (XSS canary stayed false)");
  assert.ok(!root.innerHTML.includes("<script>"), label + ": no raw <script> tag in the rendered markup");
  assert.strictEqual(root.querySelectorAll("script").length, 0, label + ": no live <script> element in the DOM");
  assert.strictEqual(root.querySelectorAll("img[onerror]").length, 0, label + ": no live onerror-bearing element in the DOM");
}

async function scenarioRailHostname() {
  const c = buildConsole({
    url: "http://localhost/#t=FAKE",
    routes: (method, p) => (p === "/api/services"
      ? jsonRoute({ project: { id: "p1", name: "Proj" }, services: [{ hostname: XSS_PAYLOAD, type: "postgresql:single@18", support: "supported", actions: [] }], allowWrites: true })
      : null),
  });
  c.window.__xssFired = false;
  await waitFor(() => c.document.querySelectorAll("#services li").length > 0, { desc: "services rail render" });
  const rail = c.document.getElementById("services");
  assertSafelyRendered(c.window, rail, "service hostname (rail)");
  assert.strictEqual(rail.querySelector("li span").textContent, XSS_PAYLOAD, "rail hostname: textContent recovers the raw value — escaping did not corrupt it");
  c.close();
}

async function scenarioTreeNodeName() {
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=db",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [{ hostname: "db", type: "postgresql:single@18", support: "supported", actions: [] }], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: XSS_PAYLOAD, kind: "blob", path: { service: "db", segments: ["x"] }, meta: { size: 3 } }] });
      return null;
    },
  });
  c.window.__xssFired = false;
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  const tree = c.document.getElementById("tree");
  assertSafelyRendered(c.window, tree, "tree node name");
  assert.strictEqual(tree.querySelector(".nname").textContent, XSS_PAYLOAD, "tree node name: textContent recovers the raw value");
  c.close();
}

async function scenarioGridColumnAndCell() {
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=db",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [{ hostname: "db", type: "postgresql:single@18", support: "supported", actions: [] }], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
      if (p.startsWith("/api/table")) return jsonRoute({ columns: [{ name: XSS_PAYLOAD, dataType: "text", pk: false }], rows: [[XSS_PAYLOAD]], rowKeyCols: [] });
      return null;
    },
  });
  c.window.__xssFired = false;
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid"), { desc: "grid render" });
  const grid = c.document.querySelector("#content .gridwrap");
  assertSafelyRendered(c.window, grid, "grid column/cell");
  assert.strictEqual(grid.querySelector("th").textContent, XSS_PAYLOAD, "column header: textContent recovers the raw value");
  assert.strictEqual(grid.querySelector("td").textContent, XSS_PAYLOAD, "row cell value: textContent recovers the raw value");
  c.close();
}

async function scenarioBlobName() {
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=db",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [{ hostname: "db", type: "postgresql:single@18", support: "supported", actions: [] }], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: XSS_PAYLOAD, kind: "blob", path: { service: "db", segments: ["x"] }, meta: { size: 5 } }] });
      if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
      return null;
    },
  });
  c.window.__xssFired = false;
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content .toolbar"), { desc: "blob toolbar render" });
  const toolbar = c.document.querySelector("#content .toolbar");
  assertSafelyRendered(c.window, toolbar, "blob name (toolbar)");
  assert.strictEqual(toolbar.querySelector("b").textContent, XSS_PAYLOAD, "blob name: textContent recovers the raw value");
  c.close();
}

async function main() {
  await scenarioRailHostname();
  await scenarioTreeNodeName();
  await scenarioGridColumnAndCell();
  await scenarioBlobName();
  console.log("xss.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
