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

test("a welcome-suppress message (app.zerops.io ancestor) closes the panel", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "welcome-suppress" });

  assert.equal(panel.disposed, true, "the runtime GUI-context gate must close the optimistically-opened welcome");
});

test("welcome.html hides the body until the runtime parent-origin gate reveals it", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;

  assert.match(html, /<body data-preload>/, "body must start hidden (data-preload)");
  assert.match(html, /body\[data-preload\]\s*{\s*display:\s*none/, "nonce'd CSS must hide the preloading body (no inline style)");
  assert.match(html, /location\.ancestorOrigins/, "the gate reads the embedding ancestor origins");
  assert.match(html, /"app\.zerops\.io"/, "app.zerops.io is the suppressed embedder");
  assert.match(html, /"welcome-suppress"/, "suppression asks the host to close the panel");
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
  // frame-src is the one deliberate carve-out from default-src 'none': it
  // allows the YouTube demo video to play as an in-panel iframe (never a
  // fetch/img/connect source — those stay fully blocked).
  assert.match(panel.webview.html, /frame-src https:\/\/www\.youtube-nocookie\.com/);
});

// Accessibility structural pins (docs/spec-welcome-mode.md §7 W-CTA polish):
// these check the raw HTML string only (there is no DOM in this harness —
// see the note on welcome.js's inline script never being executed by
// node:test), the same style as the CSP checks above.

test("every actionable data-* control is a real <button>, never a div/span with a click handler", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;

  for (const attr of ["data-open-url", "data-authorize", "data-onboard", "data-open-agent", "data-guided-toggle", "data-pack-toggle", "data-pack-details", "data-path", "data-goto-build", "data-goto-tour"]) {
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

// Row-local failure UX (spec §6 revision): each pack row's OWN result line
// is a polite live region, replacing the retired single shared pack-result
// line.
test("every pack row's own result line is a polite live region", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;

  const resultTags = [...html.matchAll(/<span class="pack-result" data-pack-result="[^"]+"([^>]*)>/g)];
  assert.equal(resultTags.length, 4, "expected all four pack rows' own result lines");
  for (const [, attrs] of resultTags) assert.match(attrs, /aria-live="polite"/);
});

// The redesigned build/tour panels wrap several external-opening buttons
// around nested markup (an icon, a badge, an aria-hidden arrow glyph) —
// visible-text-only disclosure (the OLD assertion here) no longer holds for
// those. The intent survives via the accessible name instead: every
// data-open-url control now carries an explicit "opens in your browser"
// aria-label, which is what a screen reader actually announces regardless of
// the visible markup inside the button (cross-pinned as a source string in
// ui_structure.test.js; this is the DOM-level proof against the real
// rendered/nonce'd output).
test("every external-opening control discloses via its accessible name that it opens outside the editor", async () => {
  const { panel } = await openWelcome();
  const html = panel.webview.html;
  const buttons = [...html.matchAll(/<button[^>]*\bdata-open-url="https:\/\/[^"]+"[^>]*>/g)].map((m) => m[0]);
  assert.ok(buttons.length >= 11, "expected every external-opening control");
  for (const tag of buttons) {
    assert.match(tag, /aria-label="[^"]*opens in your browser[^"]*"/i, `expected an "opens in your browser" disclosure on ${tag}`);
  }
});
