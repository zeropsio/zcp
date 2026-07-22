"use strict";

// welcome.html's inline <script> relays every channel-matching
// window "message" event up to the extension host (see the comment above
// BRIDGE_CHANNEL there) — the host is the origin authority, so the relay
// itself would otherwise let a hostile embedding page flood the extension
// host's IPC/log by posting an unbounded stream of bogus acks
// (docs/spec-welcome-mode.md §8 W-SEC). BRIDGE_RELAY_MAX_PER_SEC caps that to
// 20 relays/second via a rolling 1s window (bridgeRelayWindowStart/
// bridgeRelayCount).
//
// This webview script runs with no require() boundary and no DOM (the
// welcomejs harness has no jsdom — see harness.js), so it can't be driven
// like welcome.js's tests elsewhere in this directory. Instead this file
// pins the mechanism as a SOURCE invariant: it reads the real shipped
// template directly and asserts the rate-limit tokens are present, so a
// future edit that silently drops the cap fails a test instead of shipping
// unnoticed.

const fs = require("fs");
const path = require("path");
const test = require("node:test");
const assert = require("node:assert/strict");
const { TEMPLATES_DIR } = require("./harness.js");

function readWelcomeHtmlSource() {
  return fs.readFileSync(path.join(TEMPLATES_DIR, "vscode-bootstrap-welcome.html"), "utf8");
}

test("welcome.html still declares the bridge relay flood cap at 20/sec", () => {
  const src = readWelcomeHtmlSource();
  assert.match(src, /const\s+BRIDGE_RELAY_MAX_PER_SEC\s*=\s*20\b/, "BRIDGE_RELAY_MAX_PER_SEC must stay declared at 20");
});

test("welcome.html still resets the relay counter on a new 1s window", () => {
  const src = readWelcomeHtmlSource();
  assert.match(src, /bridgeRelayWindowStart\s*>\s*1000/, "the rolling window must still compare elapsed time against 1000ms");
  assert.match(src, /bridgeRelayWindowStart\s*=\s*now/, "a new window must still stamp bridgeRelayWindowStart");
  assert.match(src, /bridgeRelayCount\s*=\s*0/, "a new window must still reset bridgeRelayCount");
});

test("welcome.html still drops relays once the per-window cap is hit", () => {
  const src = readWelcomeHtmlSource();
  assert.match(
    src,
    /bridgeRelayCount\s*>=\s*BRIDGE_RELAY_MAX_PER_SEC/,
    "the flood-drop check must still compare bridgeRelayCount against BRIDGE_RELAY_MAX_PER_SEC"
  );
  assert.match(src, /bridgeRelayCount\+\+/, "an admitted relay must still increment bridgeRelayCount");
});
