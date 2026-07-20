"use strict";

const assert = require("assert");
const EventEmitter = require("events");
const { createConsoleClient, allowed, isMutating } = require("../templates/vscode-studio/lib/consoleClient");
const { routes: generatedRoutes } = require("../templates/vscode-studio/lib/consoleRoutes");

// fakeHttp captures outgoing requests and replies with a canned response.
function fakeHttp(captured, responder) {
  return {
    request: function (opts, cb) {
      const chunks = [];
      const req = {
        write: function (b) { chunks.push(Buffer.isBuffer(b) ? b : Buffer.from(b)); },
        end: function () {
          captured.push({ opts: opts, body: Buffer.concat(chunks) });
          const res = new EventEmitter();
          const r = responder(opts) || {};
          res.statusCode = r.status || 200;
          res.headers = r.headers || { "content-type": "application/json" };
          setImmediate(function () {
            if (r.body != null) res.emit("data", Buffer.from(r.body));
            res.emit("end");
          });
          cb(res);
        },
        on: function () { return req; },
      };
      return req;
    },
  };
}

// Allowlist mirrors server.go apiRoutes; query strings are ignored.
assert.ok(allowed("GET", "/api/services"));
assert.ok(allowed("GET", "/api/tree?service=db&segs=%5B%5D"), "query string ignored");
assert.ok(allowed("POST", "/api/cell"));
assert.ok(allowed("DELETE", "/api/node"));
assert.ok(!allowed("GET", "/api/secrets"), "unknown path rejected");
assert.ok(!allowed("DELETE", "/api/services"), "wrong method rejected");
assert.ok(!allowed("GET", "/proxy/8081/"), "non-api path rejected — no SSRF to code-server");
assert.ok(!allowed("GET", "/"), "root rejected");

// Mutating shapes (server.go mutating:true). Reads + query + refresh are not.
assert.ok(isMutating("POST", "/api/cell") && isMutating("DELETE", "/api/node") && isMutating("PUT", "/api/ttl"));
assert.ok(!isMutating("GET", "/api/table") && !isMutating("POST", "/api/query") && !isMutating("POST", "/api/refresh"));

// STRUCTURAL parity: allowed()/isMutating() are DERIVED from consoleRoutes.js
// (generated from server.go apiRoutes — see consoleroutes_test.go), not a
// hand-kept copy. Every shape the generated artifact declares must be allowed,
// with the matching mutating classification; a shape absent from the artifact
// must be refused. This is a structural check (it walks whatever the artifact
// contains) rather than a fixed re-listing, so it stays true even as routes
// are added later, and it would fail if consoleClient.js ever stopped reading
// the artifact (e.g. reverted to a hand-kept copy that silently drifted).
assert.ok(Array.isArray(generatedRoutes) && generatedRoutes.length > 0, "consoleRoutes.js has at least one route");
for (const rt of generatedRoutes) {
  assert.ok(allowed(rt.method, rt.path), rt.method + " " + rt.path + " is in consoleRoutes.js and must be allowed");
  assert.strictEqual(
    isMutating(rt.method, rt.path),
    rt.mutating,
    rt.method + " " + rt.path + " mutating flag must match consoleRoutes.js"
  );
}
assert.ok(!allowed("GET", "/api/not-a-real-route"), "a shape absent from consoleRoutes.js is refused");

(async function main() {
  // A disallowed shape is refused before any network call.
  let captured = [];
  let client = createConsoleClient({ port: 1234, token: "secret", http: fakeHttp(captured, function () { return {}; }) });
  const blocked = await client.request({ method: "GET", path: "/proxy/8081/" });
  assert.strictEqual(blocked.status, 403, "disallowed shape returns 403");
  assert.strictEqual(captured.length, 0, "blocked request never hits the network");

  // A shape absent from the GENERATED route contract is refused the same way —
  // proves the request() path itself (not just the standalone allowed() helper)
  // is gated by consoleRoutes.js.
  captured = [];
  client = createConsoleClient({ port: 1234, token: "secret", http: fakeHttp(captured, function () { return {}; }) });
  const absent = await client.request({ method: "GET", path: "/api/not-a-real-route" });
  assert.strictEqual(absent.status, 403, "route absent from consoleRoutes.js is refused");
  assert.strictEqual(captured.length, 0, "refused request never hits the network");

  // WRITE GATE: write mode is OFF by default, so a mutating shape is refused
  // host-side and never reaches the console — no write token is attached.
  const gated = await client.request({ method: "POST", path: "/api/cell", body: JSON.stringify({ a: 1 }), confirm: true });
  assert.strictEqual(gated.status, 403, "mutation blocked when write mode is off");
  assert.strictEqual(captured.length, 0, "blocked mutation never hits the console");

  // Once write mode is host-enabled, a MUTATION carries the bearer + X-Confirm + the
  // per-request WRITE TOKEN + json body to the FIXED loopback.
  captured = [];
  client = createConsoleClient({ port: 1234, token: "secret", writeToken: "wtsecret", http: fakeHttp(captured, function () { return { status: 200, body: "{}" }; }) });
  client.setWriteEnabled(true);
  const ok = await client.request({ method: "POST", path: "/api/cell", body: JSON.stringify({ a: 1 }), confirm: true });
  assert.strictEqual(captured.length, 1);
  const c = captured[0];
  assert.strictEqual(c.opts.host, "127.0.0.1", "broker always dials loopback (never a webview-supplied host)");
  assert.strictEqual(c.opts.port, 1234, "broker always dials the bound console port");
  assert.strictEqual(c.opts.headers.Authorization, "Bearer secret", "broker injects the bearer host-side");
  assert.strictEqual(c.opts.headers["X-Confirm"], "true", "confirm intent forwarded");
  assert.strictEqual(c.opts.headers["X-Write-Token"], "wtsecret", "broker attaches the write token on a host-enabled mutation");
  assert.strictEqual(c.body.toString(), '{"a":1}', "json body forwarded verbatim");
  assert.strictEqual(ok.ok, true);

  // KI-1: a body-bearing MUTATING request must carry an explicit Content-Length.
  // Node's http.request defaults useChunkedEncodingByDefault=false for DELETE
  // (true for POST/PUT) -- writing a body without an explicit Content-Length on a
  // DELETE ships with NO body framing at all, so the server reads zero body bytes.
  // Every body-addressed DELETE (/api/node, /api/row) depends on this header.
  // The JSON body includes a multibyte char (ž) so the assertion proves BYTE
  // length, not the JS string's UTF-16 .length.
  captured = [];
  client = createConsoleClient({ port: 1234, token: "secret", writeToken: "wtsecret", http: fakeHttp(captured, function () { return { status: 200, body: "{}" }; }) });
  client.setWriteEnabled(true);
  const deleteBody = JSON.stringify({ path: { service: "cache", segments: ["kžy"] } });
  assert.notStrictEqual(Buffer.byteLength(deleteBody), deleteBody.length, "fixture must contain a multibyte char, or this proves nothing about byte vs char length");
  await client.request({ method: "DELETE", path: "/api/node", body: deleteBody, confirm: true });
  assert.strictEqual(captured.length, 1);
  const delReq = captured[0];
  assert.strictEqual(
    delReq.opts.headers["Content-Length"],
    String(Buffer.byteLength(deleteBody)),
    "DELETE with a JSON body must carry an explicit byte-length Content-Length header"
  );
  assert.strictEqual(delReq.body.length, Buffer.byteLength(deleteBody), "declared Content-Length must match the bytes actually written");

  // Same guard on the upload (PUT) branch, with multibyte binary content.
  captured = [];
  client = createConsoleClient({ port: 1234, token: "secret", writeToken: "wtsecret", http: fakeHttp(captured, function () { return { status: 200, body: "{}" }; }) });
  client.setWriteEnabled(true);
  const uploadBytes = Buffer.from("héllo žlutočký", "utf8");
  await client.request({ method: "PUT", path: "/api/blob", upload: { buffer: uploadBytes, contentType: "text/plain" }, confirm: true });
  assert.strictEqual(captured.length, 1);
  const upReq = captured[0];
  assert.strictEqual(
    upReq.opts.headers["Content-Length"],
    String(uploadBytes.length),
    "upload (PUT) branch must carry an explicit Content-Length sized to the uploaded buffer"
  );
  assert.strictEqual(upReq.body.length, uploadBytes.length, "declared Content-Length must match the bytes actually written");

  // A READ never carries the write token, even with write mode on.
  captured = [];
  client = createConsoleClient({ port: 1234, token: "secret", writeToken: "wtsecret", http: fakeHttp(captured, function () { return { status: 200, body: "[]" }; }) });
  client.setWriteEnabled(true);
  await client.request({ method: "GET", path: "/api/table?service=db" });
  assert.strictEqual(captured.length, 1);
  assert.strictEqual(captured[0].opts.headers["X-Write-Token"], undefined, "reads never carry the write token, even with write mode on");

  // Reads are never gated by write mode (work with write mode off, no token).
  captured = [];
  const roClient = createConsoleClient({ port: 1234, token: "secret", writeToken: "wtsecret", http: fakeHttp(captured, function () { return { status: 200, body: "[]" }; }) });
  const read = await roClient.request({ method: "GET", path: "/api/table?service=db" });
  assert.strictEqual(read.ok, true, "reads work with write mode off");
  assert.strictEqual(captured.length, 1, "read reaches the console regardless of write mode");
  assert.strictEqual(captured[0].opts.headers["X-Write-Token"], undefined, "read with write mode off carries no write token");

  // The write token attaches ONLY from the broker's host-set writeEnabled flag — a
  // webview-driven request() cannot enable writes: passing writeEnabled/writeToken in
  // the request object is ignored, so a mutation stays refused locally (writeEnabled
  // is host state, not a request field).
  captured = [];
  const wvClient = createConsoleClient({ port: 1234, token: "secret", writeToken: "wtsecret", http: fakeHttp(captured, function () { return {}; }) });
  const wvBlocked = await wvClient.request({ method: "POST", path: "/api/cell", body: "{}", confirm: true, writeEnabled: true, writeToken: "attacker" });
  assert.strictEqual(wvBlocked.status, 403, "a webview request cannot enable writes via request() fields");
  assert.strictEqual(captured.length, 0, "webview-forced mutation never hits the console");

  // Even with writes host-enabled, a webview-supplied writeToken field in request()
  // is IGNORED — the broker attaches only its OWN host-held token.
  captured = [];
  const hostClient = createConsoleClient({ port: 1234, token: "secret", writeToken: "wtsecret", http: fakeHttp(captured, function () { return { status: 200, body: "{}" }; }) });
  hostClient.setWriteEnabled(true);
  await hostClient.request({ method: "POST", path: "/api/cell", body: "{}", confirm: true, writeToken: "attacker" });
  assert.strictEqual(captured[0].opts.headers["X-Write-Token"], "wtsecret", "broker attaches its OWN write token, never a webview-supplied one");

  // A GET carries no body but still the bearer.
  captured = [];
  client = createConsoleClient({ port: 1234, token: "secret", http: fakeHttp(captured, function () { return { status: 200, body: "{}" }; }) });
  await client.request({ method: "GET", path: "/api/services" });
  assert.strictEqual(captured[0].body.length, 0, "GET sends no body");
  assert.strictEqual(captured[0].opts.headers.Authorization, "Bearer secret");

  // A connection error becomes a clean 503 envelope, not a throw.
  const errHttp = { request: function () { const r = { write: function () {}, end: function () {}, on: function (n, fn) { if (n === "error") setImmediate(function () { fn(new Error("ECONNREFUSED")); }); return r; } }; return r; } };
  const errClient = createConsoleClient({ port: 1, token: "x", http: errHttp });
  const unreachable = await errClient.request({ method: "GET", path: "/api/services" });
  assert.strictEqual(unreachable.status, 503, "connection error maps to 503");

  // There is no arm(): write authority is presented per request, never flipped
  // process-wide — the removed arm step is what closed the standalone-write gap.
  assert.strictEqual(typeof client.arm, "undefined", "arm() is gone — no process-global write latch");

  // Full-content download is the broker's one unbuffered read seam. It accepts
  // only a generated-allowlisted GET path, dials the same fixed console target,
  // injects only the host-held read bearer, and hands ownership of the live
  // IncomingMessage to the caller without attaching data listeners.
  captured = [];
  let streamReq = null;
  let streamRes = null;
  const streamHttp = {
    request: function (opts, cb) {
      const req = new EventEmitter();
      req.destroyedByCancel = false;
      req.destroy = function () { req.destroyedByCancel = true; };
      req.end = function () {
        captured.push({ opts: opts });
        streamRes = new EventEmitter();
        streamRes.statusCode = 200;
        streamRes.headers = { "content-length": "10485760", "content-type": "application/octet-stream" };
        streamRes.destroyedByCancel = false;
        streamRes.destroy = function () { streamRes.destroyedByCancel = true; };
        setImmediate(function () { cb(streamRes); });
      };
      streamReq = req;
      return req;
    },
  };
  const streamClient = createConsoleClient({ port: 4321, token: "stream-secret", writeToken: "must-not-leak", http: streamHttp });
  const aborter = new AbortController();
  const streamed = await streamClient.openReadStream(
    "/api/download?service=store&segs=%5B%22large.bin%22%5D",
    { signal: aborter.signal }
  );
  assert.strictEqual(captured.length, 1, "allowed streaming GET dials exactly once");
  assert.strictEqual(captured[0].opts.host, "127.0.0.1", "stream destination is fixed loopback");
  assert.strictEqual(captured[0].opts.port, 4321, "stream destination is the captured console port");
  assert.strictEqual(captured[0].opts.method, "GET", "openReadStream is GET-only by construction");
  assert.strictEqual(captured[0].opts.headers.Authorization, "Bearer stream-secret", "stream carries the host bearer");
  assert.strictEqual(captured[0].opts.headers["X-Write-Token"], undefined, "stream never carries the write token");
  assert.strictEqual(streamed.body, streamRes, "caller receives the live response stream, not copied bytes");
  assert.strictEqual(streamed.headers["content-length"], "10485760", "headers are exposed lower-cased");
  assert.strictEqual(streamRes.listenerCount("data"), 0, "broker never buffers the download stream");
  assert.strictEqual("bytes" in streamed, false, "stream result has no buffered/base64-shaped body field");
  aborter.abort();
  assert.strictEqual(streamReq.destroyedByCancel, true, "cancellation destroys the upstream request");
  assert.strictEqual(streamRes.destroyedByCancel, true, "cancellation destroys an already-open upstream response");

  // Cancellation in the dial->response gap rejects immediately. If Node still
  // delivers a late response callback, that response is destroyed and is never
  // handed to the caller. An already-aborted signal never dials at all.
  let delayedCallback = null;
  let delayedRequest = null;
  let delayedDials = 0;
  const delayedHttp = {
    request: function (_, cb) {
      delayedDials++;
      delayedCallback = cb;
      const req = new EventEmitter();
      req.destroyedByCancel = false;
      req.destroy = function () { req.destroyedByCancel = true; };
      req.end = function () {};
      delayedRequest = req;
      return req;
    },
  };
  const delayedClient = createConsoleClient({ port: 4321, token: "secret", http: delayedHttp });
  const beforeResponseAborter = new AbortController();
  const beforeResponse = delayedClient.openReadStream("/api/download?service=store&segs=%5B%22late.bin%22%5D", {
    signal: beforeResponseAborter.signal,
  });
  assert.strictEqual(delayedDials, 1, "non-aborted streaming request reaches the fixed dialer");
  beforeResponseAborter.abort();
  await assert.rejects(
    beforeResponse,
    function (err) { return err && err.name === "AbortError" && err.code === "ABORT_ERR"; },
    "abort before response rejects with the public AbortError shape"
  );
  assert.strictEqual(delayedRequest.destroyedByCancel, true, "pre-response abort destroys the in-flight request");

  const lateResponse = new EventEmitter();
  lateResponse.statusCode = 200;
  lateResponse.headers = { "content-length": "4" };
  lateResponse.destroyedByCancel = false;
  lateResponse.destroy = function () { lateResponse.destroyedByCancel = true; };
  delayedCallback(lateResponse);
  assert.strictEqual(lateResponse.destroyedByCancel, true, "a response arriving after cancellation is destroyed, never returned");

  const alreadyAborted = new AbortController();
  alreadyAborted.abort();
  await assert.rejects(
    delayedClient.openReadStream("/api/download?service=store&segs=%5B%22never.bin%22%5D", { signal: alreadyAborted.signal }),
    function (err) { return err && err.name === "AbortError" && err.code === "ABORT_ERR"; }
  );
  assert.strictEqual(delayedDials, 1, "an already-aborted signal is rejected before any network dial");

  captured = [];
  const blockedStreamClient = createConsoleClient({ port: 4321, token: "secret", http: streamHttp });
  await assert.rejects(
    blockedStreamClient.openReadStream("/api/not-generated"),
    /allowlist/,
    "streaming refuses a path absent from generated consoleRoutes"
  );
  assert.strictEqual(captured.length, 0, "blocked streaming path never dials the network");

  console.log("consoleclient.test.js OK");
})().catch(function (e) {
  console.error(e && e.stack ? e.stack : e);
  process.exit(1);
});
