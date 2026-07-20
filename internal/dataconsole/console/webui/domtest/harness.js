"use strict";
// jsdom DOM harness for the Data Console SPA (S15a — see plans/
// dataconsole-excellence-program-2026-07-16.md §6 DD-7).
//
// Loads the REAL shipped dist/index.html + dist/*.js — the same markup and
// module load order a browser executes, parsed straight out of index.html so
// the harness self-updates if S15 changes the script list. Nothing here
// reimplements app.js rendering logic; tests drive it exactly as a user or
// the VS Code extension host would:
//   - boot: a `#t=` URL fragment (standalone) or a `dataconsole-init`
//     postMessage (embedded) — both real app.js entry points (bootAuth /
//     onHostMessage), never a direct internal function call.
//   - navigate: dispatch real "click" events on rendered elements (the
//     `.onclick` property IS a real DOM event handler — dispatchEvent
//     invokes it exactly as a browser would on a user click).
//   - network: app.js's two real transports are driven, never bypassed —
//     standalone's `window.fetch` and embedded's postMessage `dc-rpc` /
//     `dc-rpc-result` broker (the VS Code host relay, mocked here; the
//     excellence-program plan §2 "Verified-composite" calls this exact
//     shape out: "a mocked-broker DOM harness").
//
// Deliberately NOT used: calling app.js's top-level functions directly.
// jsdom's `window.eval` does not attach top-level `function` declarations
// from a "use strict" script to `window` (verified empirically — unlike a
// real browser's <script> tag) so those symbols are not reachable from
// outside; driving through clicks/postMessage/fetch sidesteps that gap
// entirely and is more faithful to "the real render path" besides.

const fs = require("fs");
const path = require("path");
const { JSDOM } = require("jsdom");

const DIST_DIR = path.join(__dirname, "..", "dist");
const INDEX_HTML = path.join(DIST_DIR, "index.html");

// scriptOrder parses the `<script src="...">` tags out of the real
// index.html, in document order, so the harness always loads exactly what a
// browser would load — no hand-maintained copy of the script list to drift.
function scriptOrder(html) {
  const re = /<script src="([^"]+)"><\/script>/g;
  const out = [];
  let m;
  while ((m = re.exec(html))) out.push(m[1]);
  if (out.length === 0) throw new Error("domtest harness: no <script src> tags found in dist/index.html — markup shape changed, update the parser");
  return out;
}

function lowerHeaders(h) {
  const out = {};
  for (const k of Object.keys(h || {})) out[k.toLowerCase()] = h[k];
  return out;
}

// fakeFetchResponse adapts a {status, headers, bodyBytes} route result to the
// subset of the fetch Response surface app.js actually calls (.ok, .status,
// .headers.get, .json, .text, .arrayBuffer, .blob) — duck-typed, since jsdom
// ships no `fetch`/`Response` implementation at all (verified empirically).
function fakeFetchResponse(window, rr) {
  const status = rr.status || 200;
  const headers = lowerHeaders(rr.headers);
  const bytes = rr.bodyBytes || Buffer.alloc(0);
  return {
    ok: status >= 200 && status < 300,
    status,
    headers: { get: (k) => (k == null ? null : (headers[String(k).toLowerCase()] ?? null)) },
    json: async () => JSON.parse(bytes.toString("utf8")),
    text: async () => bytes.toString("utf8"),
    arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
    blob: async () => new window.Blob([bytes]),
  };
}

function notFound(method, pathWithQuery) {
  // eslint-disable-next-line no-console
  console.error("[domtest] no fixture route for", method, pathWithQuery);
  return { status: 404, headers: { "content-type": "application/json" }, bodyBytes: Buffer.from(JSON.stringify({ code: "not_found", message: "no fixture route: " + method + " " + pathWithQuery })) };
}

// buildConsole loads the real SPA into a fresh jsdom window and wires
// whichever transport(s) the scenario needs. `routes(method, pathWithQuery,
// bodyText)` returns a canned {status, headers, bodyBytes} or null/undefined
// (treated as 404, logged for debuggability); it is invoked from BOTH
// transports so one fixture function serves either.
//
// `embedded: true` stubs `acquireVsCodeApi` BEFORE app.js evaluates (app.js
// reads it once, at module-eval time, to decide its transport) and relays
// `dc-rpc` webview->host calls back through a REAL `window.postMessage`
// `dc-rpc-result` reply — the same round trip the VS Code extension host
// performs, not a shortcut around it.
function buildConsole({
  url = "http://localhost/",
  embedded = false,
  routes = () => null,
  vscodeState = undefined,
  localStorageState = undefined,
} = {}) {
  const sourceHTML = fs.readFileSync(INDEX_HTML, "utf8");
  const order = scriptOrder(sourceHTML);
  // jsdom does not fetch the linked stylesheet in outside-only mode. Inline
  // the REAL shipped CSS so layout-contract tests exercise computed styles
  // from dist/style.css without duplicating any declarations in the harness.
  const css = fs.readFileSync(path.join(DIST_DIR, "style.css"), "utf8");
  const html = sourceHTML.replace(/<link rel="stylesheet" href="style\.css">/, `<style>${css}</style>`);

  const dom = new JSDOM(html, { url, runScripts: "outside-only" });
  const window = dom.window;
  const document = window.document;
  const localStorageKey = "zcp.dataconsole.layout.v1";

  if (localStorageState !== undefined) {
    window.localStorage.setItem(localStorageKey, JSON.stringify(localStorageState));
  }

  window.fetch = async (input, init) => {
    const u = new window.URL(String(input), window.location.href);
    const method = (init && init.method) || "GET";
    const rr = (await routes(method, u.pathname + u.search, init && init.body)) || notFound(method, u.pathname + u.search);
    return fakeFetchResponse(window, rr);
  };

  const rpcLog = [];
  let webviewState = vscodeState === undefined ? undefined : JSON.parse(JSON.stringify(vscodeState));
  if (embedded) {
    window.acquireVsCodeApi = () => ({
      getState: () => webviewState,
      setState: (next) => {
        webviewState = next == null ? next : JSON.parse(JSON.stringify(next));
        return next;
      },
      postMessage: (msg) => {
        rpcLog.push(msg);
        if (msg.type !== "dc-rpc") return; // dc-ready / dc-write-mode / dc-download / dc-upload: no default host behavior
        Promise.resolve(routes(msg.method || "GET", msg.path, msg.body)).then((rr) => {
          const resolved = rr || notFound(msg.method || "GET", msg.path);
          const bytes = resolved.bodyBytes || Buffer.alloc(0);
          window.postMessage({
            type: "dc-rpc-result",
            id: msg.id,
            ok: (resolved.status || 200) >= 200 && (resolved.status || 200) < 300,
            status: resolved.status || 200,
            headers: lowerHeaders(resolved.headers),
            b64: bytes.toString("base64"),
          }, "*");
        });
      },
    });
  }

  for (const src of order) {
    window.eval(fs.readFileSync(path.join(DIST_DIR, src), "utf8"));
  }

  return {
    dom,
    window,
    document,
    rpcLog,
    getState: () => webviewState,
    getLocalStorageState: () => {
      const raw = window.localStorage.getItem(localStorageKey);
      return raw == null ? undefined : JSON.parse(raw);
    },
    close: () => dom.window.close(),
  };
}

// waitFor polls `check()` (real timers, not a fixed tick count — robust to
// however many microtask/macrotask hops the render path takes) until it
// returns a truthy value or `timeoutMs` elapses, in which case it throws
// with `desc` so a failing test names what it was waiting for.
function waitFor(check, { timeoutMs = 2000, intervalMs = 5, desc = "condition" } = {}) {
  const start = Date.now();
  return new Promise((resolve, reject) => {
    const tick = () => {
      let v;
      try { v = check(); } catch (_) { v = undefined; }
      if (v) { resolve(v); return; }
      if (Date.now() - start >= timeoutMs) { reject(new Error("waitFor timed out after " + timeoutMs + "ms: " + desc)); return; }
      setTimeout(tick, intervalMs);
    };
    tick();
  });
}

// click dispatches a real "click" Event so the element's `.onclick` property
// handler fires exactly as it would from a user click in a browser.
function click(el) {
  const w = el.ownerDocument.defaultView;
  el.dispatchEvent(new w.MouseEvent("click", { bubbles: true, cancelable: true }));
}

function jsonRoute(obj, opts = {}) {
  return {
    status: opts.status || 200,
    headers: Object.assign({ "content-type": "application/json" }, opts.headers || {}),
    bodyBytes: Buffer.from(JSON.stringify(obj), "utf8"),
  };
}

// blobRoute builds a /api/blob-shaped response: the two X-DataConsole-*
// headers app.js reads (openBlob) to decide preview mode + truncation.
function blobRoute(text, opts = {}) {
  return {
    status: opts.status || 200,
    headers: Object.assign({
      "content-type": "application/octet-stream",
      "x-dataconsole-contenttype": opts.contentType || "text/plain",
      "x-dataconsole-truncated": opts.truncated ? "true" : "false",
    }, opts.headers || {}),
    bodyBytes: Buffer.from(text, "utf8"),
  };
}

// hostPostMessage sends a message AS THE HOST/embedder would — used to drive
// `dataconsole-init` / `dataconsole-write-mode` in embedded scenarios. A real
// `postMessage`, not a direct call into app.js's message handler (which,
// per the harness-level doc comment, is not reachable from outside anyway).
function hostPostMessage(window, data) {
  window.postMessage(data, "*");
}

module.exports = { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage };
