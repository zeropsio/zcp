"use strict";

// Static structural pins for the Start/Build/Tour welcome.html redesign
// (slice 3): string/regex checks over the raw template — no DOM runtime in
// this harness (see bridge_relay_ratelimit.test.js's own comment on the
// same constraint). Cross-pins against welcome.js keep the two files'
// duplicated display copy (EXTERNAL_URLS, CTA_PROMPTS) from silently
// drifting apart, the same reason welcome_source_pins.test.js exists for
// welcome.js's own duplicated copy.

const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const assert = require("node:assert/strict");
const { TEMPLATES_DIR } = require("./harness.js");

function htmlSource() {
  return fs.readFileSync(path.join(TEMPLATES_DIR, "vscode-bootstrap-welcome.html"), "utf8");
}

function jsSource() {
  return fs.readFileSync(path.join(TEMPLATES_DIR, "vscode-bootstrap-welcome.js"), "utf8");
}

// Canonical registry order (extension.js's ALL_AGENT_IDS) — the agent rows
// must render in exactly this order in the markup; applyState() reorders the
// LIVE DOM to match the payload order, but the payload itself is already
// this order absent a ZCP_AGENTS restriction.
const AGENT_ROW_IDS = ["claude-code", "codex", "antigravity", "grok", "cursor"];

test("exactly five data-agent-row ids, in canonical registry order", () => {
  const html = htmlSource();
  // Anchored to the actual row markup (never a bare data-agent-row="...")
  // so the webview script's own template-literal selectors
  // (`[data-agent-row="${id}"]`) don't get counted as rows.
  const ids = [...html.matchAll(/<div class="agent-row" data-agent-row="([^"]+)">/g)].map((m) => m[1]);
  assert.deepStrictEqual(ids, AGENT_ROW_IDS);
});

test("exactly seven data-slide values, 0..6", () => {
  const html = htmlSource();
  const slides = [...html.matchAll(/data-slide="(\d+)"/g)].map((m) => Number(m[1]));
  assert.deepStrictEqual(slides, [0, 1, 2, 3, 4, 5, 6]);
});

test("the start panel carries exactly four way-cards", () => {
  const html = htmlSource();
  const cards = [...html.matchAll(/data-way-card="[^"]+"/g)];
  assert.equal(cards.length, 4);
});

function extractExternalUrlsFromJs(src) {
  const m = src.match(/const EXTERNAL_URLS = new Set\(\[([\s\S]*?)\]\)/);
  assert.ok(m, "expected EXTERNAL_URLS in welcome.js");
  return [...m[1].matchAll(/"([^"]+)"/g)].map((x) => x[1]);
}

test("data-open-url values are exactly the EXTERNAL_URLS set, cross-pinned both directions", () => {
  const html = htmlSource();
  const js = jsSource();
  const htmlUrls = new Set([...html.matchAll(/data-open-url="([^"]+)"/g)].map((m) => m[1]));
  const jsUrls = new Set(extractExternalUrlsFromJs(js));

  assert.equal(jsUrls.size, 11, "sanity: welcome.js's own allowlist should carry 11 URLs");
  for (const u of htmlUrls) assert.ok(jsUrls.has(u), `welcome.html uses ${u}, not in welcome.js's EXTERNAL_URLS`);
  for (const u of jsUrls) assert.ok(htmlUrls.has(u), `welcome.js allows ${u}, never used in welcome.html`);
});

test("exactly one nonce'd style tag and one nonce'd script tag", () => {
  const html = htmlSource();
  assert.equal((html.match(/<style nonce="__CSP_NONCE__">/g) || []).length, 1);
  assert.equal((html.match(/<script nonce="__CSP_NONCE__">/g) || []).length, 1);
});

test("no .innerHTML, no inline style attributes, no raw <a href=\"http", () => {
  const html = htmlSource();
  assert.doesNotMatch(html, /\.innerHTML/);
  assert.doesNotMatch(html, / style="/);
  assert.doesNotMatch(html, /<a href="http/);
});

test("no leftover PREVIEW widget from the design prototype", () => {
  const html = htmlSource();
  assert.doesNotMatch(html, /PREVIEW/);
  assert.doesNotMatch(html, /agentCatalog/);
  assert.doesNotMatch(html, /actionFor/);
});

test("tour slides use the hidden-attribute mechanism, not opacity/visibility alone", () => {
  const html = htmlSource();
  // Slide 0 starts active (no hidden attribute); every other slide starts
  // hidden — a real display:none, so its links/buttons are unfocusable
  // until the tour script activates it.
  assert.match(html, /<section class="tour-slide is-active" data-slide="0">/);
  for (let n = 1; n <= 6; n++) {
    const re = new RegExp(`<section class="tour-slide" data-slide="${n}" hidden>`);
    assert.match(html, re, `slide ${n} must start with the hidden attribute`);
  }
});

test("data-agents-empty exists", () => {
  const html = htmlSource();
  assert.match(html, /data-agents-empty/);
});

function extractCtaPromptsFromJs(src) {
  const m = src.match(/const CTA_PROMPTS = \{([\s\S]*?)\};/);
  assert.ok(m, "expected CTA_PROMPTS in welcome.js");
  const newM = m[1].match(/new:\s*"([^"]+)"/);
  const existingM = m[1].match(/existing:\s*"([^"]+)"/);
  assert.ok(newM && existingM, "expected both new/existing CTA_PROMPTS entries");
  return { new: newM[1], existing: existingM[1] };
}

test("the two CTA prompt texts appear verbatim and match welcome.js's CTA_PROMPTS", () => {
  const html = htmlSource();
  const js = jsSource();
  const prompts = extractCtaPromptsFromJs(js);

  const newMatch = html.match(/<pre class="kick-prompt" data-kick-prompt="new">([^<]+)<\/pre>/);
  const existingMatch = html.match(/<pre class="kick-prompt" data-kick-prompt="existing">([^<]+)<\/pre>/);
  assert.ok(newMatch, "expected the 'new' kickoff prompt in welcome.html");
  assert.ok(existingMatch, "expected the 'existing' kickoff prompt in welcome.html");
  assert.equal(newMatch[1], prompts.new);
  assert.equal(existingMatch[1], prompts.existing);
});

test('"Terminal login" markup exists only within the claude-code and codex rows', () => {
  const html = htmlSource();
  // Bound the scan to the agent-rows container itself: splitting the raw
  // source on the row marker alone would let the LAST row's "block" run off
  // into whatever follows in the file (here, the webview script's own
  // AUTH_PHASE_TEXT/unsupportedPhaseText copy, which also says "Terminal
  // login") and produce a false positive.
  const startIdx = html.indexOf('<div class="agent-rows"');
  const endIdx = html.indexOf("data-agents-empty");
  assert.ok(startIdx >= 0 && endIdx > startIdx, "expected an agent-rows container followed by data-agents-empty");
  const rowsSection = html.slice(startIdx, endIdx);

  const rowBlocks = rowsSection.split(/(?=<div class="agent-row" data-agent-row=")/);
  let sawAny = false;
  for (const block of rowBlocks) {
    const idMatch = block.match(/^<div class="agent-row" data-agent-row="([^"]+)"/);
    if (!idMatch) continue;
    const hasTerminal = block.includes("Terminal login");
    if (hasTerminal) sawAny = true;
    const expectTerminal = idMatch[1] === "claude-code" || idMatch[1] === "codex";
    assert.equal(hasTerminal, expectTerminal, `row ${idMatch[1]}: Terminal login present=${hasTerminal}, expected=${expectTerminal}`);
  }
  assert.ok(sawAny, "sanity: Terminal login must appear in at least one row");
});

test("diagnostics hooks are present", () => {
  const html = htmlSource();
  for (const hook of ["data-diag-zembed", "data-diag-version", "data-diag-service", "data-diag-bridge"]) {
    assert.match(html, new RegExp(hook));
  }
});

test("every external-opening control discloses via its accessible name that it opens outside the editor", () => {
  const html = htmlSource();
  const buttons = [...html.matchAll(/<button[^>]*\bdata-open-url="https:\/\/[^"]+"[^>]*>/g)].map((m) => m[0]);
  assert.equal(buttons.length, 12, "expected every external-opening control (11 URLs, infrastructure used twice)");
  for (const tag of buttons) {
    assert.match(tag, /aria-label="[^"]*opens in your browser[^"]*"/i, `expected an "opens in your browser" disclosure on ${tag}`);
  }
});
