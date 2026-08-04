"use strict";
// Blank tree roots use the server-declared service family for their copy and
// expose only a currently usable next step. Read actions need only the service
// capability; mutations additionally need embedded, host-confirmed write mode.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

function action(id, enabled, readOnly) {
  return { id, enabled, readOnly: !!readOnly, reason: enabled ? "" : "service is view-only" };
}

async function emptyTree(service, { embedded = false, writeEnabled = false } = {}) {
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    return null;
  };
  const url = embedded ? "http://localhost/" : `http://localhost/#t=FAKE&svc=${encodeURIComponent(service.hostname)}`;
  const c = buildConsole({ url, embedded, routes });
  if (embedded) {
    await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
    hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled, service: service.hostname });
  }
  await waitFor(() => c.document.querySelector("#tree > .state.empty"), { desc: `${service.family || "unknown"} empty-tree slate` });
  return c;
}

async function scenarioTabularReadCTAOpensQueryConsole() {
  const c = await emptyTree({
    hostname: "db", type: "postgresql:single@18", family: "tabular", support: "supported",
    actions: [action("querySQL", true, true)],
  });
  const slate = c.document.querySelector("#tree > .state.empty");
  assert.strictEqual(slate.querySelector(".state-title").textContent, "No tables yet", "tabular roots name the absent resource");
  assert.match(slate.querySelector(".state-detail").textContent, /tables are created with SQL/i, "tabular roots explain how tables are created");
  const button = slate.querySelector("button");
  assert.ok(button, "an enabled read action renders without write mode");
  assert.strictEqual(button.textContent, "Open query console", "the tabular next step is the query console");
  click(button);
  await waitFor(() => c.document.getElementById("qtext"), { desc: "query console opens from the blank slate" });
  assert.match(c.document.querySelector("#content .toolbar").textContent, /Query — db/, "the CTA opens this service's query console");
  c.close();
}

async function scenarioKVMutationRequiresWriteModeAndCapability() {
  const writable = {
    hostname: "cache", type: "valkey:single@7", family: "kv", support: "supported",
    actions: [action("createKey", true, false)],
  };
  const c = await emptyTree(writable, { embedded: true, writeEnabled: true });
  const slate = c.document.querySelector("#tree > .state.empty");
  assert.strictEqual(slate.querySelector(".state-title").textContent, "No keys yet", "KV roots use key-specific copy");
  const button = slate.querySelector("button");
  assert.ok(button, "write mode plus an enabled createKey action renders the mutation CTA");
  assert.strictEqual(button.textContent, "Add key", "the KV next step is Add key");
  click(button);
  await waitFor(() => !c.document.getElementById("modal").classList.contains("hidden"), { desc: "Add key modal opens" });
  assert.strictEqual(c.document.getElementById("modaltitle").textContent, "Add key", "the CTA opens the existing create-key form");
  c.close();

  const writeOff = await emptyTree(writable, { embedded: true, writeEnabled: false });
  assert.strictEqual(writeOff.document.querySelector("#tree > .state.empty button"), null, "createKey enabled without host-confirmed write mode renders no CTA");
  writeOff.close();

  const viewOnly = await emptyTree({
    hostname: "cache", type: "valkey:single@7", family: "kv", support: "view-only",
    actions: [action("createKey", false, false)],
  }, { embedded: true, writeEnabled: true });
  assert.strictEqual(viewOnly.document.querySelector("#tree > .state.empty button"), null, "a view-only KV service renders no disabled CTA");
  viewOnly.close();
}

async function scenarioRemainingFamiliesUseDeclaredCopy() {
  const cases = [
    { family: "document", hostname: "docs", type: "elasticsearch:single@9", title: "No indexes yet", detail: /indexes appear once the app creates them/i },
    { family: "stream", hostname: "events", type: "nats:single@2", title: "No streams yet", detail: /read-only/i },
    { family: undefined, hostname: "mystery", type: "mystery:single@1", title: "Nothing here yet", detail: null },
  ];
  for (const tc of cases) {
    const c = await emptyTree({ hostname: tc.hostname, type: tc.type, family: tc.family, support: "supported", actions: [] });
    const slate = c.document.querySelector("#tree > .state.empty");
    assert.strictEqual(slate.querySelector(".state-title").textContent, tc.title, `${tc.family || "unknown"} family uses its declared title`);
    if (tc.detail) assert.match(slate.querySelector(".state-detail").textContent, tc.detail, `${tc.family} family explains the state`);
    assert.strictEqual(slate.querySelector("button"), null, `${tc.family || "unknown"} family has no invented CTA`);
    c.close();
  }

  const object = await emptyTree({
    hostname: "storage", type: "s3:single@1", family: "object", support: "supported",
    actions: [action("uploadObject", true, false)],
  }, { embedded: true, writeEnabled: true });
  const objectSlate = object.document.querySelector("#tree > .state.empty");
  assert.strictEqual(objectSlate.querySelector(".state-title").textContent, "Bucket is empty", "object roots use bucket-specific copy");
  assert.match(objectSlate.querySelector(".state-detail").textContent, /upload bar/i, "the object slate points to the existing upload bar when it is present");
  assert.ok(object.document.querySelector("#tree > .uploadbar"), "the existing object-family root upload bar remains the mutation affordance");
  assert.strictEqual(objectSlate.querySelector("button"), null, "the object blank slate does not duplicate the upload action");
  object.close();
}

async function main() {
  await scenarioTabularReadCTAOpensQueryConsole();
  await scenarioKVMutationRequiresWriteModeAndCapability();
  await scenarioRemainingFamiliesUseDeclaredCopy();
  console.log("blank-slate-tree-family.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
