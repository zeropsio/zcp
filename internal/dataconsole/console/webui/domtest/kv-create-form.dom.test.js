"use strict";
// FIX 6 (S1, structural): the Add-key modal's form must be TYPE-DEPENDENT.
// kv.go's CreateKey (provider/kv/kv.go) requires a distinct shape per redis
// type: hash needs a Field (required, the hash field name), zset needs
// Field(the member, required) + Score (required, numeric) -- but the form only
// ever collected name/type/value, so creating a hash or zset via the UI
// deterministically 400ed (string/list/set, which only need value, worked).
// Field names below (`field`, `score`) match provider.KVCreate's JSON tags
// exactly (provider/types.go) -- the server decodes these verbatim.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

async function main() {
  const service = {
    hostname: "cache", type: "valkey:single@7", support: "supported",
    actions: [{ id: "createKey", enabled: true, readOnly: false, reason: "" }],
  };
  const creates = [];
  const routes = (method, p, body) => {
    if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    if (method === "POST" && p === "/api/kv/create") { creates.push(JSON.parse(body)); return jsonRoute({ ok: true }); }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  const w = c.window;
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "cache" });
  await waitFor(() => c.document.getElementById("createkeylink"), { desc: "add key link render" });

  // ---- string: unchanged behavior -- value only, no field/score sent ----
  click(c.document.getElementById("createkeylink"));
  await waitFor(() => c.document.getElementById("kvname"), { desc: "create-key form render" });
  c.document.getElementById("kvname").value = "str1";
  c.document.getElementById("kvval").value = "hello";
  click(c.document.getElementById("modalok"));
  await waitFor(() => creates.length === 1, { desc: "string create request sent" });
  assert.strictEqual(creates[0].type, "string");
  assert.strictEqual(Object.prototype.hasOwnProperty.call(creates[0], "field"), false, "string create carries no field");
  assert.strictEqual(Object.prototype.hasOwnProperty.call(creates[0], "score"), false, "string create carries no score");

  // ---- hash: switching type reveals a Field input; submission carries it ----
  click(c.document.getElementById("createkeylink"));
  await waitFor(() => c.document.getElementById("kvtype"), { desc: "create-key form re-render" });
  c.document.getElementById("kvname").value = "hash1";
  c.document.getElementById("kvtype").value = "hash";
  c.document.getElementById("kvtype").dispatchEvent(new w.Event("change", { bubbles: true }));
  await waitFor(() => c.document.getElementById("kvfield"), { desc: "hash field input appears" });
  c.document.getElementById("kvfield").value = "f1";
  c.document.getElementById("kvval").value = "v1";
  click(c.document.getElementById("modalok"));
  await waitFor(() => creates.length === 2, { desc: "hash create request sent" });
  assert.strictEqual(creates[1].type, "hash");
  assert.strictEqual(creates[1].field, "f1", "hash create carries the field name the server requires");

  // ---- zset: switching type reveals Member + Score inputs; score is numeric ----
  click(c.document.getElementById("createkeylink"));
  await waitFor(() => c.document.getElementById("kvtype"), { desc: "create-key form re-render" });
  c.document.getElementById("kvname").value = "zset1";
  c.document.getElementById("kvtype").value = "zset";
  c.document.getElementById("kvtype").dispatchEvent(new w.Event("change", { bubbles: true }));
  await waitFor(() => c.document.getElementById("kvscore"), { desc: "zset member/score inputs appear" });
  c.document.getElementById("kvfield").value = "member1";
  c.document.getElementById("kvscore").value = "4.5";
  click(c.document.getElementById("modalok"));
  await waitFor(() => creates.length === 3, { desc: "zset create request sent" });
  assert.strictEqual(creates[2].type, "zset");
  assert.strictEqual(creates[2].field, "member1", "zset create carries the member the server requires");
  assert.strictEqual(creates[2].score, 4.5, "zset create carries a NUMERIC score, not a string");

  c.close();
  console.log("kv-create-form.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
