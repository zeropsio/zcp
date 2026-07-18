"use strict";
// C6: two focus-trap bugs (modal-keyboard.dom.test.js's B2 contract).
//
// (a) showModal() focuses focusablesIn(.modalbox)[0] BEFORE openCellView (its
// caller) hides #modalcancel -- and focusablesIn() itself never checked
// visibility, only `disabled`/`tabIndex`. #modalcancel sits BEFORE #modalok
// in the DOM (index.html), so the view-only cell modal ended up focusing the
// (about to be hidden) Cancel button, and any later Tab cycle -- since
// focusablesIn() still doesn't filter it out -- could land back on it, an
// invisible focus target.
//
// (b) onModalKeydown's Tab handler did `if (!focusables.length) return;`
// BEFORE calling preventDefault() -- with a B1 write in flight (both buttons
// disabled) and no other form fields, that is a real, reachable zero-
// focusables state, and returning without preventDefault hands Tab back to
// whatever default handling exists outside our code, instead of keeping it
// claimed by the open modal.
//
// Fix: focusablesIn() excludes anything `.closest(".hidden")`; showModal
// takes a `viewOnly` option so openCellView's Cancel-hiding happens BEFORE
// the initial focus call (not after); Tab always preventDefault()s while the
// modal is open, parking focus on .modalbox itself (tabindex="-1") when
// there is nothing else to focus.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

// 1. Opening the cell-view modal (openCellView) must never focus the hidden
//    Cancel button, and Tab from the sole visible control (OK) must wrap to
//    itself, never landing on the hidden Cancel.
async function scenarioCellViewModalNeverFocusesHiddenCancel() {
  const service = {
    hostname: "ch", type: "clickhouse:single@1", support: "view-only", actions: [],
  };
  const table = {
    columns: [{ name: "val", dataType: "text", pk: false, editable: false, reason: "" }],
    rows: [["plain"]], rowKeyCols: [],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "ch", segments: ["t"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute(table);
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "ch" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content tbody.gridbody td"), { desc: "grid cell render" });
  click(c.document.querySelector("#content tbody.gridbody td"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "cell view modal opens" });

  const cancelBtn = c.document.getElementById("modalcancel");
  const okBtn = c.document.getElementById("modalok");
  assert.strictEqual(cancelBtn.classList.contains("hidden"), true, "precondition: Cancel is hidden in the view modal");
  assert.notStrictEqual(c.document.activeElement, cancelBtn, "opening the view modal must never focus the hidden Cancel button");
  assert.strictEqual(c.document.activeElement, okBtn, "the view modal focuses its sole visible OK button");

  // Tab from the last (and only) visible focusable wraps to the first --
  // itself -- never reaching the hidden Cancel.
  const w = c.window;
  const ev = new w.KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true });
  c.document.dispatchEvent(ev);
  assert.strictEqual(c.document.activeElement, okBtn, "Tab from the last visible focusable wraps back to itself, never to the hidden Cancel");
  c.close();
}

// 2. Zero focusables (mid-flight: both buttons disabled, no other fields):
//    Tab must still be claimed (preventDefault) and focus parks on
//    .modalbox itself, never left to drift to document.body.
async function scenarioZeroFocusablesTrapsTabOnModalbox() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "supported",
    actions: [{ id: "deleteNode", enabled: true, readOnly: false, reason: "" }, { id: "readBlob", enabled: true, readOnly: true, reason: "" }],
  };
  const resolvers = [];
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "f.txt", kind: "blob", path: { service: "storage", segments: ["f.txt"] }, meta: { size: 5 } }] });
    if (p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    if (method === "DELETE" && p === "/api/node") return new Promise((resolve) => resolvers.push(resolve));
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "storage" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.getElementById("delblob"), { desc: "delete button render" });
  click(c.document.getElementById("delblob"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "delete confirm modal opens" });
  click(c.document.getElementById("modalok"));
  await waitFor(() => resolvers.length === 1, { desc: "delete request in flight" });

  assert.strictEqual(c.document.getElementById("modalok").disabled, true, "precondition: OK disabled mid-flight");
  assert.strictEqual(c.document.getElementById("modalcancel").disabled, true, "precondition: Cancel disabled mid-flight");

  const w = c.window;
  const ev = new w.KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true });
  c.document.dispatchEvent(ev);
  assert.strictEqual(ev.defaultPrevented, true, "Tab is claimed (preventDefault) even with zero focusable controls, so it can never fall through to the page's native tab order");
  assert.strictEqual(c.document.activeElement, c.document.querySelector(".modalbox"), "with zero focusable controls, focus parks on .modalbox itself instead of drifting away");
  c.close();
}

async function main() {
  await scenarioCellViewModalNeverFocusesHiddenCancel();
  await scenarioZeroFocusablesTrapsTabOnModalbox();
  console.log("modal-focus-trap-hidden-cancel.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
