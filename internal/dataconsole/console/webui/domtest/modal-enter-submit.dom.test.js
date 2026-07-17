"use strict";
// B5 (S3): Enter submits multi-field modal forms. promptModal already wires
// Enter-to-submit on its single input; Insert-row/Add-key/Add-document did
// not -- their inputs had no keydown wiring and there is no <form> element to
// pick up native Enter-submits-form behavior. Fix: a keydown Enter on any
// `input` (not textarea, not select) inside .modalbody clicks #modalok --
// wired once, centrally, via event delegation on #modalbody so it survives
// every showModal() replacing #modalbody's children. Enter in a <textarea>
// must NOT submit (it inserts a newline, as usual).

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

// 1. Insert-row: Enter on one of its (id-less, data-col-addressed) inputs
//    submits the form exactly like clicking Confirm.
async function scenarioEnterOnInsertRowInputSubmits() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported",
    actions: [{ id: "insertRow", enabled: true, readOnly: false, reason: "" }],
  };
  const rowCalls = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }] });
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [{ name: "id", dataType: "int", pk: true, editable: false, reason: "primary key" }, { name: "val", dataType: "text", pk: false, editable: true, reason: "" }],
      rows: [], rowKeyCols: ["id"],
    });
    if (method === "POST" && p === "/api/row") { rowCalls.push(body); return jsonRoute({ statement: "INSERT", affected: 1, key: { id: 1 } }); }
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
  input.value = "enter-submitted";
  const w = c.window;
  input.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
  await waitFor(() => rowCalls.length === 1, { desc: "Enter on an input submits the insert-row form" });
  assert.ok(/enter-submitted/.test(rowCalls[0]), "the submitted body carries the typed value");
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "the modal closes on the Enter-triggered success" });
  c.close();
}

// 2. Add-document: Enter on the id INPUT submits; Enter inside the JSON body
//    TEXTAREA must NOT submit (it is a multi-line field).
async function scenarioEnterInTextareaDoesNotSubmitButInputDoes() {
  const service = {
    hostname: "es", type: "elasticsearch:single@9", support: "supported",
    actions: [{ id: "searchDocs", enabled: true, readOnly: true, reason: "" }, { id: "createDoc", enabled: true, readOnly: false, reason: "" }],
  };
  const createCalls = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "articles", kind: "container", path: { service: "es", segments: ["articles"] }, hasChildren: true }] });
    if (method === "POST" && p === "/api/document/create") { createCalls.push(body); return jsonRoute({ id: "doc1" }); }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "es" });
  await waitFor(() => c.document.getElementById("searchlink"), { desc: "search link render" });
  click(c.document.getElementById("searchlink"));
  await waitFor(() => c.document.getElementById("adddoc"), { desc: "add document link render" });
  click(c.document.getElementById("adddoc"));
  await waitFor(() => c.document.getElementById("docbody"), { desc: "add-document form render" });

  const w = c.window;
  const textarea = c.document.getElementById("docbody");
  textarea.value = '{"title":"x"}';
  textarea.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
  await new Promise((resolve) => setTimeout(resolve, 30));
  assert.strictEqual(createCalls.length, 0, "Enter inside a <textarea> must not submit the modal form");
  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "the modal is still open after Enter in the textarea");

  const idInput = c.document.getElementById("docid");
  idInput.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
  await waitFor(() => createCalls.length === 1, { desc: "Enter on the id INPUT submits the form" });
  c.close();
}

async function main() {
  await scenarioEnterOnInsertRowInputSubmits();
  await scenarioEnterInTextareaDoesNotSubmitButInputDoes();
  console.log("modal-enter-submit.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
