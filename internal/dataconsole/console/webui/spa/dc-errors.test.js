"use strict";

const assert = require("assert");
const errors = require("../dist/dc-errors");

const upstream = errors.errorFromEnvelope(
  {
    code: "upstream",
    status: 502,
    message: "raw driver text must not be the user-facing message",
    requestId: "req-123",
    service: "db",
    family: "tabular",
    action: "readBlob",
  },
  500,
  "header-req"
);

assert.strictEqual(upstream.status, 502, "envelope status wins over fallback status");
assert.strictEqual(upstream.requestId, "req-123", "envelope requestId wins over header fallback");
assert.strictEqual(errors.userErrorMessage(upstream), "Service returned an upstream error.", "upstream code maps to safe user text");
assert.strictEqual(
  errors.errorSummary(upstream),
  "Service returned an upstream error. \u00b7 request req-123",
  "errorSummary includes the request id"
);
assert.ok(errors.errorHTML(upstream).includes("Service returned an upstream error."), "errorHTML contains safe user text");
assert.ok(errors.errorHTML(upstream).includes("Request ID: req-123"), "errorHTML shows request id");
assert.ok(!errors.errorHTML(upstream).includes("raw driver text"), "errorHTML does not leak raw upstream message");

const fallback = errors.errorFromEnvelope({ message: "plain failure" }, 418, "req-header");
assert.strictEqual(fallback.status, 418, "fallback status is used without envelope status");
assert.strictEqual(fallback.requestId, "req-header", "header request id is used without envelope request id");
assert.strictEqual(errors.userErrorMessage(fallback), "plain failure", "unknown code falls back to envelope message");

assert.strictEqual(errors.userErrorMessage({ code: "internal", message: "raw" }), "Internal error.", "internal code is sanitized");
assert.strictEqual(errors.userErrorMessage({ code: "unreachable", message: "dial tcp" }), "Service unreachable.", "unreachable code maps to VPN-friendly text");
assert.strictEqual(errors.userErrorMessage("literal"), "literal", "string errors pass through");

const vpnAction = { id: "showVPNGate", enabled: true, reason: "Bring up the project VPN." };
assert.deepStrictEqual(
  errors.vpnGateDecision({
    service: "db",
    project: { id: "p1" },
    action: vpnAction,
    error: { code: "unreachable", requestId: "req-vpn" },
  }),
  {
    show: true,
    service: "db",
    projectId: "p1",
    reason: "Bring up the project VPN.",
    command: "zcli vpn up p1",
    summary: "Service unreachable. \u00b7 request req-vpn",
  },
  "enabled showVPNGate action plus unreachable error produces the VPN gate data"
);
assert.deepStrictEqual(
  errors.vpnGateDecision({
    service: "db",
    project: { id: "p1" },
    action: vpnAction,
    error: { code: "upstream", requestId: "req-upstream" },
  }),
  { show: false },
  "non-unreachable errors do not trigger the VPN gate"
);
assert.deepStrictEqual(
  errors.vpnGateDecision({
    service: "db",
    project: { id: "p1" },
    action: { id: "showVPNGate", enabled: false, reason: "disabled" },
    error: { code: "unreachable" },
  }),
  { show: false },
  "disabled showVPNGate action does not trigger the VPN gate"
);

console.log("dc-errors.test.js OK");
