"use strict";
// P6 (S3, visual-polish): image honesty.
//   (a) A tree thumbnail (img.thumb) that fails to decode -- a tiny or
//       corrupt image whose bytes don't actually render even though the
//       server declared an image/* content-type -- must fall back to the
//       standard type glyph, never leave an invisible chip in its place.
//   (b) The full preview (img.imgpreview) renders its pixel dimensions
//       ("W x H px") once loaded, so "how big is this image" doesn't require
//       Download-and-inspect.
//
// jsdom implements neither actual image decoding NOR window.URL.createObjectURL
// (verified empirically: the real one throws "not a function") -- both
// scenarios stub createObjectURL/revokeObjectURL locally (this file only; the
// shared harness stays a faithful subset of the real browser surface for
// every OTHER test) and dispatch a synthetic load/error Event exactly as a
// real browser would once decoding finishes, driving the SAME handlers
// app.js wires.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

function stubObjectURLs(window) {
  let n = 0;
  window.URL.createObjectURL = () => "blob:http://localhost/fake-" + (++n);
  window.URL.revokeObjectURL = () => {};
}

function imageBlobRoute(bytes, opts = {}) {
  return {
    status: 200,
    headers: Object.assign({
      "content-type": "application/octet-stream",
      "x-dataconsole-contenttype": "image/png",
      "x-dataconsole-truncated": "false",
    }, opts.headers || {}),
    bodyBytes: Buffer.from(bytes),
  };
}

// (a) A thumb that fails to decode swaps back to the standard glyph.
async function scenarioTreeThumbFallsBackToGlyphOnDecodeError() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "pic.png", kind: "blob", path: { service: "storage", segments: ["pic.png"] }, meta: { size: 40 } }] });
    if (p.startsWith("/api/blob")) return imageBlobRoute("not-really-a-png");
    return null;
  };
  const c = buildConsole({ url: "http://localhost/#t=FAKE&svc=storage", routes });
  stubObjectURLs(c.window);
  await waitFor(() => c.document.querySelector("#tree img.thumb"), { desc: "lazy thumbnail renders" });
  const row = c.document.querySelector("#tree .node");
  const img = row.querySelector("img.thumb");
  assert.ok(img, "precondition: the thumbnail img is present");

  const w = c.window;
  img.dispatchEvent(new w.Event("error"));
  await waitFor(() => !row.querySelector("img.thumb"), { desc: "the failed thumb is replaced" });
  const fallback = row.querySelector(".kind");
  assert.ok(fallback, "a decode-failed thumb falls back to the standard type glyph, never an invisible chip");
  assert.strictEqual(fallback.textContent, "◇", "the fallback shows the blob's normal kind glyph (◇)");
  c.close();
}

// (b) The preview shows "W x H px" once the image loads.
async function scenarioPreviewShowsDimensionsOnLoad() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "pic.png", kind: "blob", path: { service: "storage", segments: ["pic.png"] }, meta: { size: 40 } }] });
    if (p.startsWith("/api/blob")) return imageBlobRoute("not-really-a-png");
    return null;
  };
  const c = buildConsole({ url: "http://localhost/#t=FAKE&svc=storage", routes });
  stubObjectURLs(c.window);
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content img.imgpreview"), { desc: "preview renders" });
  const img = c.document.querySelector("#content img.imgpreview");
  const w = c.window;
  Object.defineProperty(img, "naturalWidth", { value: 800, configurable: true });
  Object.defineProperty(img, "naturalHeight", { value: 600, configurable: true });
  img.dispatchEvent(new w.Event("load"));
  await waitFor(() => c.document.querySelector(".imgdim") && /800/.test(c.document.querySelector(".imgdim").textContent), { desc: "dimension label populates" });
  assert.strictEqual(c.document.querySelector(".imgdim").textContent, "800 × 600 px", "the preview states its pixel dimensions once loaded");
  c.close();
}

async function main() {
  await scenarioTreeThumbFallsBackToGlyphOnDecodeError();
  await scenarioPreviewShowsDimensionsOnLoad();
  console.log("image-honesty.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
