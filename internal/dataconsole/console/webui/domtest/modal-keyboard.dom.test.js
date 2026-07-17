"use strict";
// B2 (S2): modal keyboard/focus contract. Before this fix, no #modal responded
// to Escape or a backdrop click, and Tab ejected focus to the page body while
// the modal was open (live-proven). New contract, wired in showModal/hideModal:
//   (a) Escape cancels -- the same path as #modalcancel -- UNLESS a B1 write is
//       in flight (Cancel is disabled then, and Escape must respect that).
//   (b) clicking the #modal backdrop itself (not a descendant) cancels.
//   (c) a focus trap: opening focuses the first focusable in .modalbox; Tab/
//       Shift+Tab wrap within .modalbox; closing restores focus to whatever
//       was focused before the modal opened.
// Must not interfere with the grid cell editor's own Escape handling, which
// lives entirely outside #modal (cell-edit-escape.dom.test.js pins that
// separately) -- the guard is #modal's own hidden/visible state.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

function kvService() {
  return {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [{ id: "createKey", enabled: true, readOnly: false, reason: "" }],
  };
}

async function openCreateKeyModal(routes) {
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.getElementById("createkeylink"), { desc: "add key link render" });
  click(c.document.getElementById("createkeylink"));
  await waitFor(() => c.document.getElementById("kvname"), { desc: "create-key form render" });
  return c;
}

// 1. Escape cancels an idle (not in-flight) modal.
async function scenarioEscapeCancels() {
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [kvService()], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    return null;
  };
  const c = await openCreateKeyModal(routes);
  const w = c.window;
  c.document.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "Escape closes an idle modal" });
  c.close();
}

// 2. Escape must NOT cancel while a B1 write is in flight (Cancel disabled).
async function scenarioEscapeIgnoredWhileInFlight() {
  const resolvers = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [kvService()], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    if (method === "POST" && p === "/api/kv/create") return new Promise((resolve) => resolvers.push(resolve));
    return null;
  };
  const c = await openCreateKeyModal(routes);
  c.document.getElementById("kvname").value = "k1";
  click(c.document.getElementById("modalok"));
  await waitFor(() => resolvers.length === 1, { desc: "create request in flight" });
  assert.strictEqual(c.document.getElementById("modalcancel").disabled, true, "Cancel is disabled mid-flight (precondition)");

  const w = c.window;
  c.document.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  await new Promise((resolve) => setTimeout(resolve, 30));
  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "Escape is ignored while a write is in flight");
  c.close();
}

// 3. Backdrop click cancels; a click on .modalbox itself does not.
async function scenarioBackdropClickCancelsButBoxDoesNot() {
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [kvService()], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    return null;
  };
  const c = await openCreateKeyModal(routes);
  click(c.document.querySelector(".modalbox"));
  await new Promise((resolve) => setTimeout(resolve, 30));
  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "clicking inside .modalbox must not close the modal");

  click(c.document.getElementById("modal"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "clicking the backdrop itself closes the modal" });
  c.close();
}

// 4. Focus trap: open focuses the first focusable; Tab/Shift+Tab wrap; close
//    restores the previously-focused element.
async function scenarioFocusTrapAndRestore() {
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [kvService()], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.getElementById("createkeylink"), { desc: "add key link render" });

  const opener = c.document.getElementById("createkeylink");
  opener.focus();
  assert.strictEqual(c.document.activeElement, opener, "precondition: the opener is focused before the modal opens");

  click(opener);
  await waitFor(() => c.document.getElementById("kvname"), { desc: "create-key form render" });
  const box = c.document.querySelector(".modalbox");
  const focusables = Array.from(box.querySelectorAll("input, select, button")).filter((el) => !el.disabled);
  assert.ok(focusables.length >= 2, "the create-key modal has at least two focusable controls");
  assert.strictEqual(c.document.activeElement, focusables[0], "opening the modal focuses the first focusable control in .modalbox");

  const w = c.window;
  const last = focusables[focusables.length - 1];
  last.focus();
  last.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true }));
  assert.strictEqual(c.document.activeElement, focusables[0], "Tab on the last focusable wraps to the first");

  focusables[0].focus();
  focusables[0].dispatchEvent(new w.KeyboardEvent("keydown", { key: "Tab", shiftKey: true, bubbles: true, cancelable: true }));
  assert.strictEqual(c.document.activeElement, last, "Shift+Tab on the first focusable wraps to the last");

  click(c.document.getElementById("modalcancel"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "Cancel closes the modal" });
  assert.strictEqual(c.document.activeElement, opener, "closing the modal restores focus to the element that opened it");
  c.close();
}

async function main() {
  await scenarioEscapeCancels();
  await scenarioEscapeIgnoredWhileInFlight();
  await scenarioBackdropClickCancelsButBoxDoesNot();
  await scenarioFocusTrapAndRestore();
  console.log("modal-keyboard.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
