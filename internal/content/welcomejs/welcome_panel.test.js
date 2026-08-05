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
  const handler = stub.registeredCommands.get("zerops.panel");
  handler(); // manual invocation (no opts) — self-close exempt, matches Command Palette use
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

test("welcome.html hides the body until the runtime parent-origin gate reveals it", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;

  assert.match(html, /<body data-preload>/, "body must start hidden (data-preload)");
  assert.match(html, /body\[data-preload\]\s*{\s*display:\s*none/, "nonce'd CSS must hide the preloading body (no inline style)");
  assert.match(html, /location\.ancestorOrigins/, "the gate reads the embedding ancestor origins");
  assert.match(html, /removeAttribute\("data-preload"\)/, "standalone (no foreign ancestor) reveals immediately");
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

  for (const attr of [
    "data-open-url", "data-authorize", "data-open-terminal", "data-open-extension",
    "data-agent-expander", "data-guided-toggle", "data-pack-toggle", "data-pack-details",
    "data-pack-customize", "data-open-datastudio",
  ]) {
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

test("the guided result and per-agent status lines are polite live regions", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;

  assert.match(html, /data-guided-result[^>]*aria-live="polite"/, "guided-result must be aria-live=polite");
  const statusTags = [...html.matchAll(/<span class="agent-status" data-agent-status="[^"]+"([^>]*)>/g)];
  assert.equal(statusTags.length, 5, "expected all five agent rows' status lines");
  for (const [, attrs] of statusTags) assert.match(attrs, /aria-live="polite"/);
});

// Row-local failure UX (spec §6 revision): each pack row's OWN result line
// is a polite live region, replacing the retired single shared pack-result
// line.
test("every pack row's own result line is a polite live region", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;

  const resultTags = [...html.matchAll(/<span class="pack-result" data-pack-result="[^"]+"([^>]*)>/g)];
  assert.equal(resultTags.length, 3, "expected all three pack rows' own result lines");
  for (const [, attrs] of resultTags) assert.match(attrs, /aria-live="polite"/);
});

// The reduced panel (docs/spec-welcome-mode.md §6/§11) keeps exactly one
// external link (the diagnostics footer's "Zerops docs", the sole surviving
// EXTERNAL_URLS member — see welcome_source_pins.test.js). It still carries
// an explicit "opens in your browser" aria-label, which is what a screen
// reader actually announces regardless of the visible link text.
test("every external-opening control discloses via its accessible name that it opens outside the editor", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;
  const buttons = [...html.matchAll(/<button[^>]*\bdata-open-url="https:\/\/[^"]+"[^>]*>/g)].map((m) => m[0]);
  assert.equal(buttons.length, 1, "expected exactly the one surviving external-opening control (docs)");
  for (const tag of buttons) {
    assert.match(tag, /aria-label="[^"]*opens in your browser[^"]*"/i, `expected an "opens in your browser" disclosure on ${tag}`);
  }
});
