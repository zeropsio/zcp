"use strict";

// isAllowedGuiOrigin (docs/spec-welcome-mode.md §4, W-AUTH / W5): the inbound
// ack origin check trusts ONLY Zerops-exclusive GUI origins — prod
// (app.zerops.io, default port), a real dot-boundary subdomain of the
// Zerops-internal stage/dev domain (*.zerops.dev, default port), and
// localhost (any port) — plus exact origins the container operator opts in
// via ZCP_WELCOME_BRIDGE_ORIGINS. It deliberately does NOT trust *.zerops.app
// by pattern: that's the shared CUSTOMER namespace (every Zerops service gets
// a public *.zerops.app URL, and the code-server's CSP frame-ancestors lets
// any *.zerops.app page embed a victim's code-server), so a malicious page
// there could receive the bridge trigger and forge an accepted:true ack. A
// specific *.zerops.app test/custom GUI is trusted only by exact operator
// opt-in, never by suffix. The function PARSES the origin (scheme + hostname
// + port) rather than substring-matching, since a substring test is
// bypassable by an attacker-controlled host that merely CONTAINS the allowed
// string (e.g. "zerops.app.attacker.com"). bridge_flow.test.js and
// message_allowlist.test.js exercise the same function through the actual
// message flow; this file pins the function's own accept/reject boundary
// directly.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWelcome } = require("./harness.js");

function loadPureFns() {
  const { welcome } = loadWelcome();
  return welcome;
}

const ACCEPT_CASES = [
  { name: "prod exact host", origin: "https://app.zerops.io" },
  { name: "a real *.zerops.dev stage/preview subdomain", origin: "https://tatami.devel.zerops.dev" },
  { name: "a minimal single-label *.zerops.dev subdomain", origin: "https://x.zerops.dev" },
  { name: "localhost dev server, one port", origin: "http://localhost:1111" },
  { name: "localhost dev server, a different port", origin: "http://localhost:4200" },
];

const REJECT_CASES = [
  { name: "a *.zerops.app subdomain deploy with no operator opt-in (shared customer namespace)", origin: "https://febridge-24cb.prg1.zerops.app" },
  { name: "prod host on a nondefault port", origin: "https://app.zerops.io:4444" },
  { name: "a *.zerops.dev subdomain on a nondefault port", origin: "https://x.zerops.dev:8443" },
  { name: "a leading-dot zerops.dev host (empty label before the suffix)", origin: "https://.zerops.dev" },
  { name: "127.0.0.1 (not in the nginx frame-ancestors localhost allowance)", origin: "http://127.0.0.1:1111" },
  { name: "the bare zerops.dev host (no subdomain label)", origin: "https://zerops.dev" },
  { name: "prod host over plain http (scheme downgrade)", origin: "http://app.zerops.io" },
  { name: "a *.zerops.app suffix bypass via an attacker-owned host", origin: "https://zerops.app.attacker.com" },
  { name: "a look-alike host with no dot-boundary (xzerops.app)", origin: "https://xzerops.app" },
  { name: "an unrelated host", origin: "https://evil.com" },
  { name: "a non-http(s) scheme", origin: "ftp://app.zerops.io" },
  { name: "an empty origin", origin: "" },
  { name: "an unparseable origin", origin: "garbage" },
];

test("isAllowedGuiOrigin accepts every Zerops-exclusive GUI host with no extras configured", () => {
  const { isAllowedGuiOrigin } = loadPureFns();
  for (const c of ACCEPT_CASES) {
    assert.equal(isAllowedGuiOrigin(c.origin, []), true, c.name);
  }
});

test("isAllowedGuiOrigin rejects everything outside the Zerops-exclusive set, including *.zerops.app by pattern", () => {
  const { isAllowedGuiOrigin } = loadPureFns();
  for (const c of REJECT_CASES) {
    assert.equal(isAllowedGuiOrigin(c.origin, []), false, c.name);
  }
});

test("isAllowedGuiOrigin trusts an operator-configured extra origin by exact match only", () => {
  const { isAllowedGuiOrigin } = loadPureFns();
  const extras = ["https://febridge-24cb.prg1.zerops.app"];
  assert.equal(isAllowedGuiOrigin("https://febridge-24cb.prg1.zerops.app", extras), true, "the exact configured origin must be accepted");
  assert.equal(isAllowedGuiOrigin("https://other.prg1.zerops.app", extras), false, "a different *.zerops.app origin must stay rejected — extras opt in an exact origin, never the shared namespace's suffix");
});
