"use strict";

const assert = require("assert");
const EventEmitter = require("events");
const { createConsoleClient, allowed, isMutating } = require("../templates/vscode-studio/lib/consoleClient");

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

(async function main() {
  // A disallowed shape is refused before any network call.
  let captured = [];
  let client = createConsoleClient({ port: 1234, token: "secret", http: fakeHttp(captured, function () { return {}; }) });
  const blocked = await client.request({ method: "GET", path: "/proxy/8081/" });
  assert.strictEqual(blocked.status, 403, "disallowed shape returns 403");
  assert.strictEqual(captured.length, 0, "blocked request never hits the network");

  // WRITE GATE: write mode is OFF by default, so a mutating shape is refused
  // host-side and never reaches the console.
  const gated = await client.request({ method: "POST", path: "/api/cell", body: JSON.stringify({ a: 1 }), confirm: true });
  assert.strictEqual(gated.status, 403, "mutation blocked when write mode is off");
  assert.strictEqual(captured.length, 0, "blocked mutation never hits the console");

  // Once write mode is host-enabled, the mutation flows: bearer + X-Confirm + json
  // body to the FIXED loopback.
  client.setWriteEnabled(true);
  const ok = await client.request({ method: "POST", path: "/api/cell", body: JSON.stringify({ a: 1 }), confirm: true });
  assert.strictEqual(captured.length, 1);
  const c = captured[0];
  assert.strictEqual(c.opts.host, "127.0.0.1", "broker always dials loopback (never a webview-supplied host)");
  assert.strictEqual(c.opts.port, 1234, "broker always dials the bound console port");
  assert.strictEqual(c.opts.headers.Authorization, "Bearer secret", "broker injects the bearer host-side");
  assert.strictEqual(c.opts.headers["X-Confirm"], "true", "confirm intent forwarded");
  assert.strictEqual(c.body.toString(), '{"a":1}', "json body forwarded verbatim");
  assert.strictEqual(ok.ok, true);

  // Reads are never gated by write mode.
  captured = [];
  const roClient = createConsoleClient({ port: 1234, token: "secret", http: fakeHttp(captured, function () { return { status: 200, body: "[]" }; }) });
  const read = await roClient.request({ method: "GET", path: "/api/table?service=db" });
  assert.strictEqual(read.ok, true, "reads work with write mode off");
  assert.strictEqual(captured.length, 1, "read reaches the console regardless of write mode");

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

  console.log("consoleclient.test.js OK");
})().catch(function (e) {
  console.error(e && e.stack ? e.stack : e);
  process.exit(1);
});
