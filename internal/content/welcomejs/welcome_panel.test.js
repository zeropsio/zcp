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

// Accessibility structural pins (docs/spec-welcome-mode.md §7 W-CTA polish):
// these check the raw HTML string only (there is no DOM in this harness —
// see the note on welcome.js's inline script never being executed by
// node:test), the same style as the CSP checks above.

test("every actionable data-* control is a real <button>, never a div/span with a click handler", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;

  for (const attr of ["data-open-url", "data-authorize", "data-authorize-terminal", "data-guided-toggle", "data-path"]) {
    const tagsBefore = [...html.matchAll(new RegExp(`<(\\w+)[^>]*\\b${attr}\\b`, "g"))].map((m) => m[1]);
    assert.ok(tagsBefore.length > 0, `expected at least one element carrying ${attr}`);
    for (const tag of tagsBefore) assert.equal(tag, "button", `${attr} must be on a <button>, found <${tag}>`);
  }
});

test("focus-visible outline is defined against the VS Code focus border variable", async () => {
  const { panel } = await openWelcome();
  assert.match(panel.webview.html, /button:focus-visible\s*{[^}]*--vscode-focusBorder/);
});

test("transitions are disabled under prefers-reduced-motion", async () => {
  const { panel } = await openWelcome();
  assert.match(panel.webview.html, /@media \(prefers-reduced-motion:\s*reduce\)/);
});

test("the CTA result and per-agent auth phase lines are polite live regions", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;

  assert.match(html, /data-cta-result[^>]*aria-live="polite"/, "cta-result must be aria-live=polite");
  assert.match(html, /data-guided-result[^>]*aria-live="polite"/, "guided-result must be aria-live=polite");
  const phaseTags = [...html.matchAll(/<span class="agent-phase" data-agent-phase="[^"]+"([^>]*)>/g)];
  assert.equal(phaseTags.length, 5, "expected all five agent tiles' phase lines");
  for (const [, attrs] of phaseTags) assert.match(attrs, /aria-live="polite"/);
});

test("the video/docs buttons disclose that they open externally", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;
  const watchButtons = [...html.matchAll(/<button[^>]*data-open-url="https:\/\/[^"]+"[^>]*>([^<]*)<\/button>/g)].map((m) => m[1]);
  assert.ok(watchButtons.length >= 2, "expected the watch-video and docs buttons");
  for (const label of watchButtons) assert.match(label, /opens in your browser/i);
});
