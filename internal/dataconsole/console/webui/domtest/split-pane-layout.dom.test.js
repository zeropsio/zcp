"use strict";

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const TREE_MIN = 180;
const DATA_MIN = 320;
const DIVIDER_SIZE = 8;
const MAIN_WIDTH = 800;
const TREE_MAX = MAIN_WIDTH - DATA_MIN - DIVIDER_SIZE;

function setMainWidth(c, width) {
  const main = c.document.getElementById("main");
  main.getBoundingClientRect = () => ({ width, height: 600, top: 0, right: width, bottom: 600, left: 0 });
  Object.defineProperty(main, "clientWidth", { configurable: true, get: () => width });
  c.window.dispatchEvent(new c.window.Event("resize"));
}

function pointer(c, target, type, clientX) {
  const ev = new c.window.MouseEvent(type, { bubbles: true, cancelable: true, clientX });
  target.dispatchEvent(ev);
}

function key(c, target, key) {
  target.dispatchEvent(new c.window.KeyboardEvent("keydown", { bubbles: true, cancelable: true, key }));
}

function treeWidth(c) {
  return Number.parseFloat(c.document.getElementById("tree").style.width);
}

async function scenarioPointerKeyboardBoundsAndIsolation() {
  const c = buildConsole({ embedded: true });
  setMainWidth(c, MAIN_WIDTH);

  const tree = c.document.getElementById("tree");
  const content = c.document.getElementById("content");
  const divider = c.document.getElementById("tree-divider");
  assert.ok(divider, "index contains a real explorer/data divider");
  assert.strictEqual(divider.getAttribute("role"), "separator");
  assert.strictEqual(divider.getAttribute("aria-orientation"), "vertical");
  assert.strictEqual(divider.tabIndex, 0, "divider is keyboard focusable");
  assert.ok(/explorer/i.test(divider.getAttribute("aria-label") || ""), "divider has an accessible name");
  assert.strictEqual(divider.getAttribute("aria-valuemin"), String(TREE_MIN));
  assert.strictEqual(divider.getAttribute("aria-valuemax"), String(TREE_MAX));
  assert.strictEqual(divider.getAttribute("aria-valuenow"), "320");

  let treeEvents = 0;
  let contentEvents = 0;
  tree.addEventListener("pointerdown", () => { treeEvents++; });
  content.addEventListener("pointerdown", () => { contentEvents++; });

  pointer(c, divider, "pointerdown", 320);
  pointer(c, c.window, "pointermove", 9999);
  pointer(c, c.window, "pointerup", 9999);
  assert.strictEqual(treeWidth(c), TREE_MAX, "pointer resize clamps to the data-pane minimum");
  assert.strictEqual(divider.getAttribute("aria-valuenow"), String(TREE_MAX));

  key(c, divider, "Home");
  assert.strictEqual(treeWidth(c), TREE_MIN, "Home reaches the explorer minimum");
  key(c, divider, "End");
  assert.strictEqual(treeWidth(c), TREE_MAX, "End reaches the viewport-derived explorer maximum");
  key(c, divider, "ArrowLeft");
  assert.strictEqual(treeWidth(c), TREE_MAX - 16, "keyboard resize uses a useful 16px step");
  assert.strictEqual(treeEvents, 0, "divider interaction never lands on the explorer");
  assert.strictEqual(contentEvents, 0, "divider interaction never lands on the data pane");
  c.close();
}

async function scenarioEmbeddedStateRoundTripsAcrossFreshInstances() {
  const first = buildConsole({ embedded: true });
  setMainWidth(first, MAIN_WIDTH);
  const divider = first.document.getElementById("tree-divider");
  pointer(first, divider, "pointerdown", 320);
  pointer(first, first.window, "pointermove", 400);
  pointer(first, first.window, "pointerup", 400);
  assert.strictEqual(treeWidth(first), 400);
  const persisted = first.getState();
  assert.strictEqual(persisted.dataConsoleLayout.explorerWidth, 400, "resize persists through vscodeApi.setState");
  first.close();

  const second = buildConsole({ embedded: true, vscodeState: persisted });
  setMainWidth(second, MAIN_WIDTH);
  assert.strictEqual(treeWidth(second), 400, "a fresh embedded SPA restores vscodeApi.getState");
  second.close();
}

async function scenarioStandaloneUsesLocalStorageFallback() {
  const routes = (_method, path) => path === "/api/services"
    ? jsonRoute({ project: { id: "p1", name: "Proj" }, services: [], allowWrites: false })
    : null;
  const first = buildConsole({ url: "http://localhost/#t=read-token", routes });
  await waitFor(() => first.document.getElementById("project").textContent === "Proj", { desc: "first standalone start" });
  setMainWidth(first, MAIN_WIDTH);
  const divider = first.document.getElementById("tree-divider");
  pointer(first, divider, "pointerdown", 320);
  pointer(first, first.window, "pointermove", 410);
  pointer(first, first.window, "pointerup", 410);
  const stored = first.getLocalStorageState();
  assert.strictEqual(stored.dataConsoleLayout.explorerWidth, 410, "standalone resize persists to localStorage");
  first.close();

  const second = buildConsole({ url: "http://localhost/#t=read-token", localStorageState: stored, routes });
  await waitFor(() => second.document.getElementById("project").textContent === "Proj", { desc: "second standalone start" });
  setMainWidth(second, MAIN_WIDTH);
  assert.strictEqual(treeWidth(second), 410, "a fresh standalone SPA restores localStorage");
  second.close();
}

async function scenarioTabularGridConsumesRemainingPanelHeight() {
  const service = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [] };
  const routes = (_method, path) => {
    if (path === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [service], allowWrites: false });
    if (path.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
    if (path.startsWith("/api/table/count")) return jsonRoute({ count: 250 });
    if (path.startsWith("/api/table")) return jsonRoute({
      columns: [
        { name: "id", dataType: "int", pk: true, editable: false, reason: "primary key", sortable: true },
        { name: "wide_value", dataType: "text", pk: false, editable: false, reason: "read-only", sortable: true },
      ],
      rows: [["1", "x".repeat(500)]], rowKeyCols: ["id"], numbered: true, nextCursor: "100",
    });
    return null;
  };
  const c = buildConsole({ embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: false, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector(".paginator.exact"), { desc: "tabular paginator" });

  const content = c.document.getElementById("content");
  const tree = c.document.getElementById("tree");
  const gridwrap = content.querySelector(":scope > .gridwrap");
  const toolbar = content.querySelector(":scope > .toolbar");
  const paginator = content.querySelector(":scope > .paginator");
  const header = gridwrap.querySelector("thead th");
  const contentStyle = c.window.getComputedStyle(content);
  const treeStyle = c.window.getComputedStyle(tree);
  const gridStyle = c.window.getComputedStyle(gridwrap);
  const headerStyle = c.window.getComputedStyle(header);

  assert.ok(content.classList.contains("tabular-content"), "tabular render opts into the bounded flex-column mode");
  assert.strictEqual(contentStyle.display, "flex");
  assert.strictEqual(contentStyle.flexDirection, "column");
  assert.strictEqual(contentStyle.overflow, "hidden", "tabular #content is not a second vertical scroll owner");
  assert.strictEqual(treeStyle.overflow, "auto", "explorer keeps its own vertical scroll");
  assert.strictEqual(gridStyle.overflow, "auto", "horizontal and vertical table overflow stay in the grid viewport");
  assert.strictEqual(gridStyle.flexGrow, "1", "grid viewport consumes actual remaining height");
  assert.strictEqual(gridStyle.minHeight, "0px", "grid viewport can shrink without a nested bottom cutoff");
  assert.ok(gridStyle.maxHeight === "none" || gridStyle.maxHeight === "", "grid has no viewport-height cap");
  assert.strictEqual(headerStyle.position, "sticky");
  assert.strictEqual(headerStyle.top, "0px");
  assert.strictEqual(toolbar.parentElement, content, "toolbar stays outside the scrolling grid");
  assert.strictEqual(paginator.parentElement, content, "paginator stays outside the scrolling grid");

  click(c.document.querySelector("#services li"));
  assert.strictEqual(content.classList.contains("tabular-content"), false, "non-table content restores normal #content scrolling");
  assert.strictEqual(c.window.getComputedStyle(content).overflow, "auto");
  c.close();
}

async function main() {
  await scenarioPointerKeyboardBoundsAndIsolation();
  await scenarioEmbeddedStateRoundTripsAcrossFreshInstances();
  await scenarioStandaloneUsesLocalStorageFallback();
  await scenarioTabularGridConsumesRemainingPanelHeight();
  console.log("split-pane-layout.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
