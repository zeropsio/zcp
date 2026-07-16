"use strict";

// Panel lifecycle (docs/spec-welcome-mode.md §1, W-ENTRY; §8, W-SEC):
// singleton reveal, dispose clears the singleton, nonce'd CSP. The ready ->
// state handshake and its payload shape (the real §3 auth matrix, added in
// P2) live in handshake.test.js instead.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadExtension } = require("./harness.js");

async function openWelcome() {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate({ subscriptions: [], extensionPath: extensionDir });
  const handler = stub.registeredCommands.get("zerops.welcome");
  handler();
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  return { stub, extensionDir, handler, panel };
}

test("dispose clears the singleton so the next command run opens a fresh panel", async () => {
  const { stub, handler, panel } = await openWelcome();

  panel.dispose();
  handler();

  const panels = stub.panels.filter((p) => p.viewType === "zeropsWelcome");
  assert.equal(panels.length, 2, "a dispose must let the next run create a NEW panel");
  assert.equal(panels[1].revealCount, 0, "the new panel must not be a reveal of the disposed one");
});

test("the rendered HTML carries a real nonce, not the placeholder", async () => {
  const { panel } = await openWelcome();

  assert.doesNotMatch(panel.webview.html, /__CSP_NONCE__/, "nonce placeholder must be fully substituted");
  const nonceAttrs = panel.webview.html.match(/nonce="([^"]+)"/g) || [];
  assert.ok(nonceAttrs.length >= 2, "expected a nonce attribute on both the style and script tags");
  const values = new Set(nonceAttrs.map((a) => a.slice(7, -1)));
  assert.equal(values.size, 1, "every nonce attribute must carry the same value");
  const [nonce] = [...values];
  assert.ok(nonce.length >= 16, "nonce should be a real random value, not a short placeholder");
});

test("CSP meta pins default-src none with nonce'd style/script", async () => {
  const { panel } = await openWelcome();

  assert.match(panel.webview.html, /default-src 'none'/);
  assert.match(panel.webview.html, /style-src 'nonce-[^']+'/);
  assert.match(panel.webview.html, /script-src 'nonce-[^']+'/);
});
