"use strict";
// B1 (S2): modal lifecycle must never lose the user's input. Before this fix,
// #modalok's handler hid the modal BEFORE awaiting run() -- a rejection lost
// the typed form entirely, and a slow write had zero in-flight state (no
// disabled buttons, no "Working..." label). New contract: Confirm disables
// both buttons and shows "Working..."; SUCCESS closes the modal (the run's
// own toast, e.g. "Saved.", still fires as today); a REJECTION keeps the
// modal open with the typed values intact and renders an inline .err line;
// buttons re-enable. `timeout` is the one exception (accepted-not-confirmed,
// U-14) -- it still closes the modal via the existing warn toast, since that
// outcome is not a failure.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function scenarioRejectKeepsModalOpenThenSucceeds() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "insertRow", enabled: true, readOnly: false, reason: "" }],
  };
  const rowCalls = [];
  const resolvers = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }, { name: "val", dataType: "text", pk: false, editable: true, reason: "" }],
      rows: [], rowKeyCols: ["id"],
    });
    if (method === "POST" && p === "/api/row") { rowCalls.push(body); return new Promise((resolve) => resolvers.push(resolve)); }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.getElementById("insertrow"), { desc: "insert row button render" });
  click(c.document.getElementById("insertrow"));
  await waitFor(() => c.document.querySelector('#modalbody input[data-col="val"]'), { desc: "insert-row form render" });

  const input = c.document.querySelector('#modalbody input[data-col="val"]');
  input.value = "typed-value";
  const okBtn = c.document.getElementById("modalok");
  const cancelBtn = c.document.getElementById("modalcancel");
  click(okBtn);
  await waitFor(() => rowCalls.length === 1, { desc: "insert request sent" });

  // Mid-flight: both buttons disabled, Confirm relabeled, modal still open.
  assert.strictEqual(okBtn.disabled, true, "Confirm is disabled while the write is in flight");
  assert.strictEqual(cancelBtn.disabled, true, "Cancel is disabled while the write is in flight");
  assert.strictEqual(okBtn.textContent, "Working…", "Confirm shows an in-flight label");
  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "the modal stays open while the write is in flight");

  // Reject -- a 400 ErrInvalid.
  resolvers[0](jsonRoute({ code: "invalid", message: "bad" }, { status: 400 }));
  await waitFor(() => !okBtn.disabled, { desc: "buttons re-enable after the rejection" });

  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "a rejected write keeps the modal open");
  assert.strictEqual(c.document.querySelector('#modalbody input[data-col="val"]').value, "typed-value", "the typed input survives a rejected write");
  assert.strictEqual(okBtn.textContent, "Confirm", "Confirm's label restores after the rejection");
  assert.strictEqual(cancelBtn.disabled, false, "Cancel re-enables after the rejection");
  const err = c.document.querySelector(".modalbox .err");
  assert.ok(err, "an inline .err line renders inside .modalbox on rejection");
  assert.ok(/invalid/i.test(err.textContent), "the inline error reflects the failure (got: " + (err && err.textContent) + ")");

  // Retry -- this time it succeeds; the SAME typed value is resent.
  click(okBtn);
  await waitFor(() => rowCalls.length === 2, { desc: "retry insert request sent" });
  assert.ok(/typed-value/.test(rowCalls[1]), "the retry resends the preserved typed value");
  resolvers[1](jsonRoute({ statement: "INSERT", affected: 1, key: { id: 9 } }));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "modal closes on success" });
  c.close();
}

async function scenarioTimeoutClosesWithWarnToast() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "writeBlob", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "f.txt", kind: "blob", path: { service: "db", segments: ["f.txt"] }, meta: { size: 5 } }] });
    if (method === "GET" && p.startsWith("/api/blob")) return blobRoute("hello", { contentType: "text/plain" });
    if (method === "PUT" && p === "/api/blob") return jsonRoute({ code: "timeout", message: "accepted" }, { status: 504 });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.getElementById("saveblob"), { desc: "save button render" });
  click(c.document.getElementById("saveblob"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden") === false, { desc: "overwrite confirm modal render" });
  click(c.document.getElementById("modalok"));
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "timeout closes the modal (not a failure, U-14)" });
  await waitFor(() => c.document.querySelector(".toast.warn"), { desc: "the existing warn toast still fires on timeout" });
  c.close();
}

async function main() {
  await scenarioRejectKeepsModalOpenThenSucceeds();
  await scenarioTimeoutClosesWithWarnToast();
  console.log("modal-lifecycle.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
