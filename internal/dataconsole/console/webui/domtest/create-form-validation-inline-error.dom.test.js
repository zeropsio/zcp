"use strict";
// C3: createDocForm's invalid-JSON guard and createKeyForm's missing-name /
// missing-field / invalid-score guards each did `toast(msg, true); return;`
// on rejection -- a plain early RETURN, not a throw. The #modalok completion
// (modal-lifecycle.dom.test.js's B1 contract) treats a non-throwing run() as
// SUCCESS: it hides the modal, discarding the user's typed input, with only a
// transient toast (easy to miss) as any indication something was wrong --
// never the inline .err + modal-stays-open treatment a real rejection gets.
// Fix: those guards throw a sanitized validation Error instead (no .code, so
// the timeout special-case never intercepts it) -- the SAME lifecycle that
// already renders a server rejection inline now renders these too.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

function errText(c) {
  const el = c.document.querySelector(".modalbox .err");
  return el ? el.textContent : "";
}

// 1. createDocForm: invalid JSON body.
async function scenarioInvalidDocumentJSONKeepsModalOpenWithInlineError() {
  const service = {
    hostname: "es", type: "elasticsearch:single@8", support: "supported",
    actions: [{ id: "searchDocs", enabled: true, readOnly: true, reason: "" }, { id: "createDoc", enabled: true, readOnly: false, reason: "" }],
  };
  const createCalls = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "docs", kind: "container", path: { service: "es", segments: ["docs"] } }] });
    if (p.startsWith("/api/blob")) return jsonRoute({ ok: true }); // openBlob's fire-and-forget follow-up on a successful create
    if (method === "POST" && p === "/api/document/create") { createCalls.push(JSON.parse(body)); return jsonRoute({ id: "gen1" }); }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "es" });
  await waitFor(() => c.document.getElementById("searchlink"), { desc: "search link render" });
  click(c.document.getElementById("searchlink"));
  await waitFor(() => c.document.getElementById("adddoc"), { desc: "add document button render" });
  click(c.document.getElementById("adddoc"));
  await waitFor(() => c.document.getElementById("docbody"), { desc: "add-document form render" });

  c.document.getElementById("docbody").value = "{bad";
  click(c.document.getElementById("modalok"));
  await waitFor(() => /json/i.test(errText(c)), { desc: "inline validation error renders for invalid JSON" });
  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "the modal stays open on invalid JSON, never a silent success-looking close");
  assert.strictEqual(c.document.getElementById("docbody").value, "{bad", "the typed (invalid) JSON is preserved in the textarea");
  assert.strictEqual(createCalls.length, 0, "no create request is sent for invalid JSON");
  assert.strictEqual(c.document.getElementById("modalok").disabled, false, "Confirm re-enables after the rejection");

  // Correct it -- a valid create still closes the modal normally.
  c.document.getElementById("docbody").value = '{"title":"ok"}';
  click(c.document.getElementById("modalok"));
  await waitFor(() => createCalls.length === 1, { desc: "a valid JSON body sends the create request" });
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "modal closes on a valid create" });
  c.close();
}

// 2. createKeyForm: blank name / missing hash field / non-numeric zset score.
async function scenarioCreateKeyValidationKeepsModalOpen() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [{ id: "createKey", enabled: true, readOnly: false, reason: "" }],
  };
  const createCalls = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    if (method === "POST" && p === "/api/kv/create") { createCalls.push(JSON.parse(body)); return jsonRoute({ ok: true }); }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  const w = c.window;
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.getElementById("createkeylink"), { desc: "add key link render" });

  // (a) blank name.
  click(c.document.getElementById("createkeylink"));
  await waitFor(() => c.document.getElementById("kvname"), { desc: "create-key form render" });
  click(c.document.getElementById("modalok"));
  await waitFor(() => /name/i.test(errText(c)), { desc: "inline error for a blank name" });
  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "a blank key name keeps the modal open");
  assert.strictEqual(createCalls.length, 0, "no create request for a blank name");

  // (b) hash type, missing field.
  c.document.getElementById("kvname").value = "h1";
  c.document.getElementById("kvtype").value = "hash";
  c.document.getElementById("kvtype").dispatchEvent(new w.Event("change", { bubbles: true }));
  await waitFor(() => c.document.getElementById("kvfield"), { desc: "hash field input appears" });
  click(c.document.getElementById("modalok"));
  await waitFor(() => /field/i.test(errText(c)), { desc: "inline error for a missing hash field" });
  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "a missing hash field keeps the modal open");
  assert.strictEqual(createCalls.length, 0, "no create request for a missing hash field");
  assert.strictEqual(c.document.getElementById("kvname").value, "h1", "typed input survives the rejected validation");

  // (c) zset type, non-numeric score -- then correct it and succeed.
  c.document.getElementById("kvtype").value = "zset";
  c.document.getElementById("kvtype").dispatchEvent(new w.Event("change", { bubbles: true }));
  await waitFor(() => c.document.getElementById("kvscore"), { desc: "zset score input appears" });
  c.document.getElementById("kvfield").value = "m1";
  c.document.getElementById("kvscore").value = "not-a-number";
  click(c.document.getElementById("modalok"));
  await waitFor(() => /numeric|score/i.test(errText(c)), { desc: "inline error for a non-numeric score" });
  assert.strictEqual(c.document.getElementById("modal").classList.contains("hidden"), false, "a non-numeric score keeps the modal open");
  assert.strictEqual(createCalls.length, 0, "no create request for a non-numeric score");

  c.document.getElementById("kvscore").value = "4.5";
  click(c.document.getElementById("modalok"));
  await waitFor(() => createCalls.length === 1, { desc: "a corrected, valid submission sends the create request" });
  assert.strictEqual(createCalls[0].score, 4.5);
  await waitFor(() => c.document.getElementById("modal").classList.contains("hidden"), { desc: "modal closes once the submission is valid" });
  c.close();
}

async function main() {
  await scenarioInvalidDocumentJSONKeepsModalOpenWithInlineError();
  await scenarioCreateKeyValidationKeepsModalOpen();
  console.log("create-form-validation-inline-error.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
