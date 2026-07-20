"use strict";

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Project" };
const SERVICE = {
  hostname: "store",
  type: "object-storage",
  support: "supported",
  actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }],
};
const NODES = [
  { name: "one.txt", kind: "blob", path: { service: "store", segments: ["one.txt"] }, meta: { size: 3 } },
  { name: "two.txt", kind: "blob", path: { service: "store", segments: ["two.txt"] }, meta: { size: 3 } },
];

function routes(method, pathWithQuery) {
  if (pathWithQuery === "/api/services") return jsonRoute({ project: PROJECT, services: [SERVICE], allowWrites: false });
  if (pathWithQuery.startsWith("/api/tree")) return jsonRoute({ nodes: NODES });
  if (pathWithQuery.startsWith("/api/blob")) {
    const segs = new URL("http://fixture" + pathWithQuery).searchParams.get("segs") || "";
    return blobRoute(segs.includes("two.txt") ? "two" : "one", { contentType: "text/plain" });
  }
  if (pathWithQuery.startsWith("/api/download")) return blobRoute("full download bytes", { contentType: "application/octet-stream" });
  return null;
}

async function openEmbeddedBlob(c, index) {
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "embedded ready" });
  if (!c.document.querySelectorAll("#tree .node").length) {
    hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: false, service: "store" });
    await waitFor(() => c.document.querySelectorAll("#tree .node").length === 2, { desc: "embedded object tree" });
  }
  click(c.document.querySelectorAll("#tree .node")[index]);
  await waitFor(() => c.document.getElementById("dlblob"), { desc: "blob download button" });
}

async function scenarioEmbeddedResultIsCorrelatedAndVisible() {
  const c = buildConsole({ embedded: true, routes });
  await openEmbeddedBlob(c, 0);

  click(c.document.getElementById("dlblob"));
  const first = await waitFor(
    () => c.rpcLog.find((m) => m.type === "dc-download"),
    { desc: "embedded download request" }
  );
  assert.match(first.id, /^d[1-9][0-9]*$/, "each embedded download carries an opaque correlation id");
  assert.deepStrictEqual(Array.from(first.segs), ["one.txt"]);
  assert.ok(!("b64" in first) && !("url" in first) && !("token" in first), "download request carries no bytes, ticket URL, or credential");

  hostPostMessage(c.window, { type: "dataconsole-download-result", id: first.id, ok: true, code: "completed" });
  const good = await waitFor(() => c.document.querySelector(".toast.good"), { desc: "download success toast" });
  assert.strictEqual(good.textContent, "Downloaded.");

  click(c.document.getElementById("dlblob"));
  const second = await waitFor(
    () => c.rpcLog.filter((m) => m.type === "dc-download")[1],
    { desc: "second embedded download request" }
  );
  assert.notStrictEqual(second.id, first.id, "concurrent/repeated requests receive distinct ids");
  hostPostMessage(c.window, {
    type: "dataconsole-download-result",
    id: second.id,
    ok: false,
    code: "source-failed",
    message: "The download source failed.",
  });
  const bad = await waitFor(() => c.document.querySelector(".toast.bad"), { desc: "download failure toast" });
  assert.strictEqual(bad.textContent, "The download source failed.");
  c.close();
}

async function scenarioStaleAndUnknownResultsAreIgnored() {
  const c = buildConsole({ embedded: true, routes });
  await openEmbeddedBlob(c, 0);
  click(c.document.getElementById("dlblob"));
  const request = await waitFor(() => c.rpcLog.find((m) => m.type === "dc-download"), { desc: "download before navigation" });

  click(c.document.querySelectorAll("#tree .node")[1]);
  await waitFor(
    () => (c.document.querySelector("#content .toolbar b") || {}).textContent === "two.txt",
    { desc: "newer blob view" }
  );
  hostPostMessage(c.window, {
    type: "dataconsole-download-result",
    id: request.id,
    ok: false,
    code: "source-failed",
    message: "stale result must stay invisible",
  });
  hostPostMessage(c.window, {
    type: "dataconsole-download-result",
    id: "unknown-id",
    ok: true,
    code: "completed",
  });
  await new Promise((resolve) => setTimeout(resolve, 50));
  assert.strictEqual(c.document.querySelector("#content .toolbar b").textContent, "two.txt", "late download results never overwrite the current view");
  assert.strictEqual(c.document.querySelector(".toast"), null, "stale and unknown download results produce no misleading feedback");
  c.close();
}

async function scenarioStandaloneKeepsDirectBrowserFallback() {
  const paths = [];
  const standaloneRoutes = (method, pathWithQuery) => {
    paths.push(pathWithQuery);
    return routes(method, pathWithQuery);
  };
  const c = buildConsole({ url: "http://localhost/#t=read-token&svc=store", routes: standaloneRoutes });
  let clickedAnchor = null;
  c.window.URL.createObjectURL = () => "blob:download-result-test";
  c.window.URL.revokeObjectURL = () => {};
  c.window.HTMLAnchorElement.prototype.click = function () { clickedAnchor = this; };

  await waitFor(() => c.document.querySelectorAll("#tree .node").length === 2, { desc: "standalone object tree" });
  click(c.document.querySelectorAll("#tree .node")[0]);
  await waitFor(() => c.document.getElementById("dlblob"), { desc: "standalone blob download button" });
  click(c.document.getElementById("dlblob"));
  await waitFor(() => clickedAnchor, { desc: "standalone object URL download" });

  assert.ok(paths.some((p) => p.startsWith("/api/download?")), "standalone downloads full content through /api/download");
  assert.strictEqual(paths.filter((p) => p.startsWith("/api/blob?")).length, 1, "/api/blob remains the preview request only");
  assert.strictEqual(clickedAnchor.download, "one.txt");
  assert.strictEqual(clickedAnchor.href, "blob:download-result-test");
  assert.strictEqual(c.rpcLog.some((m) => m.type === "dc-download"), false, "standalone mode never asks an extension host to download");
  c.close();
}

async function main() {
  await scenarioEmbeddedResultIsCorrelatedAndVisible();
  await scenarioStaleAndUnknownResultsAreIgnored();
  await scenarioStandaloneKeepsDirectBrowserFallback();
  console.log("download-result.dom.test.js OK");
}

main().catch((e) => { console.error(e && e.stack ? e.stack : e); process.exit(1); });
