"use strict";
// P5 (S3, visual-polish): the SAME document renders its keys in whatever
// order the ENGINE returned them -- insertion order from elasticsearch,
// alphabetical from typesense -- so the identical document looks like two
// different documents depending which engine served it. Key order is
// non-semantic in JSON; re-sorting it is safe (array element order, which IS
// semantic, is left untouched) and makes the doc-detail view stable and
// engine-independent.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function openJSONDoc(text) {
  const service = { hostname: "es", type: "elasticsearch:single@9", support: "supported", actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }] };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "doc1", kind: "blob", path: { service: "es", segments: ["idx", "doc1"] } }] });
    if (p.startsWith("/api/blob")) return blobRoute(text, { contentType: "application/json" });
    return null;
  };
  // Standalone (no write token): editing() is always false, so this lands in
  // the non-editable pre.blob branch -- the doc-detail VIEW path.
  const c = buildConsole({ url: "http://localhost/#t=FAKE&svc=es", routes });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content pre.blob"), { desc: "doc detail renders" });
  const out = c.document.querySelector("#content pre.blob").textContent;
  c.close();
  return out;
}

// Two documents with the SAME keys/values in different source order (mirrors
// es insertion order vs typesense alphabetical) must render byte-identical.
async function scenarioSameDocDifferentKeyOrderRendersIdentically() {
  const insertionOrder = '{"title":"Hello","author":"Ada","tags":["x","y"],"nested":{"z":1,"a":2}}';
  const alphabeticalOrder = '{"author":"Ada","nested":{"a":2,"z":1},"tags":["x","y"],"title":"Hello"}';
  const a = await openJSONDoc(insertionOrder);
  const b = await openJSONDoc(alphabeticalOrder);
  assert.strictEqual(a, b, "the same document's two differently-key-ordered source forms render byte-identical text");
  assert.strictEqual(
    a,
    JSON.stringify({ author: "Ada", nested: { a: 2, z: 1 }, tags: ["x", "y"], title: "Hello" }, null, 2),
    "keys render in stable (sorted) order",
  );
  assert.ok(a.indexOf('"x"') < a.indexOf('"y"'), "array VALUE order is preserved, never re-sorted");
}

// A non-JSON textual blob (no application/json content-type) is never
// touched -- the sort only applies where the server actually declared JSON.
async function scenarioNonJSONTextUnaffected() {
  const service = { hostname: "storage", type: "s3:single@1", support: "supported", actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }] };
  const raw = "z=1\na=2\n"; // plain text that happens to look key-ish, but isn't JSON
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "notes.txt", kind: "blob", path: { service: "storage", segments: ["notes.txt"] } }] });
    if (p.startsWith("/api/blob")) return blobRoute(raw, { contentType: "text/plain" });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/#t=FAKE&svc=storage", routes });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content pre.blob"), { desc: "blob detail renders" });
  assert.strictEqual(c.document.querySelector("#content pre.blob").textContent, raw, "non-JSON textual content renders verbatim, untouched by the doc-detail sort");
  c.close();
}

async function main() {
  await scenarioSameDocDifferentKeyOrderRendersIdentically();
  await scenarioNonJSONTextUnaffected();
  console.log("doc-json-key-order.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
