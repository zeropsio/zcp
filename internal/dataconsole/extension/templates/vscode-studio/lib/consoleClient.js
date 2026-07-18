"use strict";

// Host-side Data Console broker. The webview NEVER reaches the console over the
// network — its CSP has no connect-src, so it cannot fetch at all. Instead it
// posts RPC requests and the EXTENSION HOST proxies them to the loopback console
// with the per-process bearer. The bearer therefore never enters the browser.
//
// The broker holds the bearer and will execute whatever the webview asks, so it
// is the new trust boundary. Two structural guards keep an XSS in the SPA's
// rendering of untrusted managed-service data from turning the host into a
// localhost SSRF / confused deputy:
//   1. the destination is FIXED to the console's own 127.0.0.1:<port> — a webview
//      can never redirect the host at another host/port; and
//   2. only the exact /api method+path shapes the console exposes are allowed
//      (mirrors server.go apiRoutes) — anything else is refused before any call.

const http = require("http");
const { routes } = require("./consoleRoutes");

// ALLOW/MUTATING are DERIVED from consoleRoutes.js — GENERATED from
// internal/dataconsole/console/server/server.go apiRoutes (the single owner; see
// consoleroutes_test.go's TestConsoleRoutesJS_DriftGuard in that package). Query
// strings are ignored; the path is matched verbatim. Because these are computed
// from the same generated table rather than hand-copied, the broker's allowlist
// cannot silently drift from the route table it mirrors.
const ALLOW = new Set(routes.map(function (r) { return r.method + " " + r.path; }));

// MUTATING is the subset of ALLOW that writes (server.go routes with mutating:true).
// GET reads, POST /api/query (READ ONLY tx) and POST /api/refresh (re-discover) are
// NOT mutating. The broker refuses these unless write-mode is host-enabled.
const MUTATING = new Set(
  routes.filter(function (r) { return r.mutating; }).map(function (r) { return r.method + " " + r.path; })
);

function shape(method, path) {
  const s = String(path == null ? "" : path);
  const q = s.indexOf("?");
  const pathname = q >= 0 ? s.slice(0, q) : s;
  return String(method || "GET").toUpperCase() + " " + pathname;
}

function allowed(method, path) {
  return ALLOW.has(shape(method, path));
}

function isMutating(method, path) {
  return MUTATING.has(shape(method, path));
}

function jsonResult(status, code, message) {
  return {
    status: status,
    ok: false,
    headers: { "content-type": "application/json" },
    bytes: Buffer.from(JSON.stringify({ code: code, status: status, message: message })),
  };
}

// createConsoleClient binds a broker to ONE console process (its loopback port +
// bearer). request() resolves to { status, ok, headers (lower-cased), bytes }.
function createConsoleClient(opts) {
  opts = opts || {};
  const host = opts.host || "127.0.0.1";
  const port = opts.port;
  const token = opts.token || "";
  // writeToken is the caller-bound WRITE CAPABILITY: an independent secret the server
  // requires on each mutating request. It is held only here (host-side) and attached
  // ONLY on a mutating request once writeEnabled — never on a read, never driven by
  // webview input. The webview never sees it, so a webview message can never write.
  const writeToken = opts.writeToken || "";
  const httpMod = opts.http || http;
  // writeEnabled is the host-confirmed write gate, set host-side after the native
  // confirm modal. It gates whether the broker attaches the writeToken to a mutating
  // request; the SERVER is the real gate (it checks the writeToken per request). The
  // broker also refuses MUTATING shapes locally until writeEnabled, so a webview
  // message alone never mutates.
  let writeEnabled = !!opts.writeEnabled;

  function request(req) {
    req = req || {};
    const method = String(req.method || "GET").toUpperCase();
    const path = String(req.path || "");
    return new Promise(function (resolve) {
      if (!allowed(method, path)) {
        resolve(jsonResult(403, "forbidden", "blocked by broker allowlist"));
        return;
      }
      const mutating = isMutating(method, path);
      if (mutating && !writeEnabled) {
        resolve(jsonResult(403, "read-only", "write mode is off"));
        return;
      }
      const headers = { Authorization: "Bearer " + token };
      // Present the write capability ONLY on a mutating request the host has enabled;
      // never on a read, and never from webview input (only writeEnabled gates it).
      if (mutating && writeEnabled) headers["X-Write-Token"] = writeToken;
      let bodyBuf = null;
      if (req.upload && req.upload.buffer != null) {
        // multipart assembled host-side (the webview cannot stream File/FormData).
        headers["Content-Type"] = req.upload.contentType || "application/octet-stream";
        bodyBuf = Buffer.isBuffer(req.upload.buffer) ? req.upload.buffer : Buffer.from(req.upload.buffer);
      } else if (req.body != null && method !== "GET" && method !== "HEAD") {
        bodyBuf = Buffer.from(typeof req.body === "string" ? req.body : JSON.stringify(req.body));
        headers["Content-Type"] = "application/json";
      }
      // Content-Length must be explicit: Node's http.request only defaults
      // useChunkedEncodingByDefault to true for POST/PUT, not DELETE (or other
      // methods) -- an unframed body on those ships as if there were no body at
      // all, which the server then reads as zero bytes. bodyBuf.length is a
      // byte count (a Buffer), correct for multibyte content.
      if (bodyBuf) headers["Content-Length"] = String(bodyBuf.length);
      if (req.confirm) headers["X-Confirm"] = "true";

      const clientReq = httpMod.request({ host: host, port: port, method: method, path: path, headers: headers }, function (res) {
        const chunks = [];
        res.on("data", function (c) { chunks.push(c); });
        res.on("end", function () {
          const lower = {};
          Object.keys(res.headers || {}).forEach(function (k) { lower[k.toLowerCase()] = res.headers[k]; });
          const status = res.statusCode || 0;
          resolve({ status: status, ok: status >= 200 && status < 300, headers: lower, bytes: Buffer.concat(chunks) });
        });
      });
      clientReq.on("error", function (e) {
        resolve(jsonResult(503, "unreachable", String((e && e.message) || e)));
      });
      if (bodyBuf) clientReq.write(bodyBuf);
      clientReq.end();
    });
  }

  return {
    request: request,
    allowed: allowed,
    setWriteEnabled: function (v) { writeEnabled = !!v; },
    isWriteEnabled: function () { return writeEnabled; },
  };
}

module.exports = { createConsoleClient: createConsoleClient, allowed: allowed, isMutating: isMutating };
