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

test("exactly six data-slide values, 0..5", () => {
  const html = htmlSource();
  const slides = [...html.matchAll(/data-slide="(\d+)"/g)].map((m) => Number(m[1]));
  assert.deepStrictEqual(slides, [0, 1, 2, 3, 4, 5]);
});

// Cross-pin TOUR_SLIDE_COUNT itself against the two markup counts it must
// stay in lockstep with: renderTour's prev/next-disabled and "N of
// TOUR_SLIDE_COUNT" position text both key off this one constant, so a drift
// (e.g. bumped to 7 without a matching seventh slide/dot) would pass the
// six-slide/six-dot markup pins above in isolation and still render a blank
// step or a wrong position count at runtime.
test("TOUR_SLIDE_COUNT matches both the data-slide section count and the data-dot button count", () => {
  const html = htmlSource();
  const m = html.match(/const TOUR_SLIDE_COUNT = (\d+);/);
  assert.ok(m, "expected `const TOUR_SLIDE_COUNT = N;` in welcome.html's script");
  const tourSlideCount = Number(m[1]);

  const slideSections = [...html.matchAll(/<section class="tour-slide[^>]*data-slide="(\d+)"[^>]*>/g)];
  const dotButtons = [...html.matchAll(/<button class="tour-dot[^>]*data-dot="(\d+)"/g)];

  assert.equal(tourSlideCount, slideSections.length, "TOUR_SLIDE_COUNT must match the number of tour-slide sections");
  assert.equal(tourSlideCount, dotButtons.length, "TOUR_SLIDE_COUNT must match the number of tour-dot buttons");
});

test("the start panel carries exactly three way-cards: agent, recipes, concepts", () => {
  const html = htmlSource();
  const cards = [...html.matchAll(/data-way-card="([^"]+)"/g)].map((m) => m[1]);
  assert.deepStrictEqual(cards, ["agent", "recipes", "concepts"]);
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
  for (let n = 1; n <= 5; n++) {
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

test('the "Terminal login" control is gone from every agent row', () => {
  const html = htmlSource();
  const startIdx = html.indexOf('<div class="agent-rows"');
  const endIdx = html.indexOf("data-agents-empty");
  assert.ok(startIdx >= 0 && endIdx > startIdx, "expected an agent-rows container followed by data-agents-empty");
  const rowsSection = html.slice(startIdx, endIdx);
  assert.ok(!rowsSection.includes("Terminal login"), "Terminal login markup must be gone from the agent rows");
  assert.ok(!rowsSection.includes("data-authorize-terminal"), "data-authorize-terminal wiring must be gone from the agent rows");
});

test('every agent row carries both "Onboard me" and plain "Open" actions', () => {
  const html = htmlSource();
  for (const id of AGENT_ROW_IDS) {
    assert.match(html, new RegExp(`data-onboard="${id}"[^>]*>Onboard me</button>`));
    assert.match(html, new RegExp(`data-open-agent="${id}"[^>]*>Open</button>`));
  }
});

test("diagnostics hooks are present", () => {
  const html = htmlSource();
  for (const hook of ["data-diag-zembed", "data-diag-version", "data-diag-service", "data-diag-bridge", "data-diag-embed"]) {
    assert.match(html, new RegExp(hook));
  }
});

test("handleBridgeSend re-stamps payload.createdAt on the browser clock, between the guard and the postMessage", () => {
  const html = htmlSource();
  const m = html.match(/function handleBridgeSend\(msg\) \{([\s\S]*?)\n\}/);
  assert.ok(m, "expected handleBridgeSend in welcome.html");
  const body = m[1];

  const guardIdx = body.indexOf('if (!msg || !msg.payload || typeof msg.target !== "string") return;');
  const stampIdx = body.indexOf("msg.payload.createdAt = Date.now();");
  const sendIdx = body.indexOf("window.top.postMessage(msg.payload, msg.target);");
  assert.ok(guardIdx >= 0, "expected the guard clause");
  assert.ok(stampIdx >= 0, "expected the browser-clock re-stamp line");
  assert.ok(sendIdx >= 0, "expected the postMessage call");
  assert.ok(guardIdx < stampIdx && stampIdx < sendIdx, "expected guard -> stamp -> postMessage, in that order");
});

test("AUTH_PHASE_TEXT carries the contacting and gui-not-ready phases with their exact copy", () => {
  const html = htmlSource();
  assert.ok(html.includes('contacting: "Contacting the Zerops dashboard…"'), "expected the contacting phase copy");
  assert.ok(
    html.includes('"gui-not-ready": "The dashboard couldn\'t open the dialog — reload the Zerops page"'),
    "expected the gui-not-ready phase copy"
  );
});

test("every external-opening control discloses via its accessible name that it opens outside the editor", () => {
  const html = htmlSource();
  const buttons = [...html.matchAll(/<button[^>]*\bdata-open-url="https:\/\/[^"]+"[^>]*>/g)].map((m) => m[0]);
  // 11 EXTERNAL_URLS, three of them used twice (showcase-recipe: recipes card
  // + the permanent showcase box under the tour; quickstart: start-panel
  // footer + the permanent showcase box's second card; infrastructure: tour
  // slides 1 and 5) => 8 singles + 3 doubles = 14. The demo video itself
  // renders as a static YouTube iframe with no data-open-url (see the
  // dedicated iframe pin below) — it's reached only via the separate "Watch
  // on YouTube" fallback link, so the count is unaffected by the video
  // redesign.
  assert.equal(buttons.length, 14, "expected every external-opening control (11 URLs; showcase/quickstart/infrastructure used twice each)");
  for (const tag of buttons) {
    assert.match(tag, /aria-label="[^"]*opens in your browser[^"]*"/i, `expected an "opens in your browser" disclosure on ${tag}`);
  }
});

test("rail titles match the Overview/Coding agents/Core concepts redesign", () => {
  const html = htmlSource();
  for (const title of ["Overview", "Coding agents", "Core concepts"]) {
    assert.match(html, new RegExp(`<span class="rail-title">${title}</span>`), `expected rail title "${title}"`);
  }
});

test("the before-start block has no details/summary disclosure anywhere — the guided row AND the pack list are both always visible", () => {
  const html = htmlSource();
  assert.doesNotMatch(html, /<details/, "the curated-skills details disclosure is retired; nothing in this file uses <details> anymore");
  assert.doesNotMatch(html, /<summary/, "no <summary> element should remain anywhere in the file");
});

// Canonical pack order (spec: community skill packs replaced the curated
// single-skill catalog) — matches PACK_ROW_IDS in welcome.html's own script
// and PACKS in welcome.js.
const PACK_ROW_IDS = ["matt-pocock-skills", "superpowers", "andrej-karpathy-skills", "anthropic-skills"];

test("exactly four data-pack-row ids, in canonical order (gstack excluded — internal/skillpacks/registry.go)", () => {
  const html = htmlSource();
  const ids = [...html.matchAll(/<div class="pack-row" data-pack-row="([^"]+)">/g)].map((m) => m[1]);
  assert.deepStrictEqual(ids, PACK_ROW_IDS);
});

test("gstack has no row/toggle anywhere in the markup or the webview script's PACK_ROW_IDS", () => {
  const html = htmlSource();
  assert.doesNotMatch(html, /gstack/, "gstack was excluded from the Go registry (56MB monorepo, wrong product shape) — it must not appear here either");
});

test("every pack row carries a data-pack-toggle button matching its row id", () => {
  const html = htmlSource();
  for (const id of PACK_ROW_IDS) {
    const re = new RegExp(`data-pack-toggle="${id}"`);
    assert.match(html, re, `expected a data-pack-toggle for ${id}`);
  }
});

// Row-local failure UX (spec §6 revision): each pack row now carries its OWN
// result line (aria-live polite) and its own "Show details" button, replacing
// the retired single shared .pack-result/data-pack-result line.
test("every pack row carries its own aria-live result line and details button, matching its row id", () => {
  const html = htmlSource();
  for (const id of PACK_ROW_IDS) {
    const resultRe = new RegExp(`<span class="pack-result" data-pack-result="${id}" aria-live="polite"></span>`);
    assert.match(html, resultRe, `expected a per-row result line for ${id}`);
    const detailsRe = new RegExp(`<button type="button" class="agent-act" data-pack-details="${id}" hidden>Show details</button>`);
    assert.match(html, detailsRe, `expected a Show details button for ${id}`);
  }
});

test("no bare (id-less) data-pack-result remains anywhere in the file", () => {
  const html = htmlSource();
  assert.doesNotMatch(html, /data-pack-result(?!=)/, "the single shared pack-result line was retired in favor of one per row");
});

test("the pack-status status-text span exists per row, matching its row id", () => {
  const html = htmlSource();
  for (const id of PACK_ROW_IDS) {
    const re = new RegExp(`<span class="pack-status" data-pack-status="${id}"></span>`);
    assert.match(html, re, `expected a pack-status span for ${id}`);
  }
});

test("PACK_STATE_STATUS_TEXT carries the incomplete/modified/broken/checking copy", () => {
  const html = htmlSource();
  assert.ok(html.includes('broken: "Needs attention — see details"'));
  assert.ok(html.includes('modified: "Needs attention — see details"'));
  assert.ok(html.includes('incomplete: "Partially installed — see details"'));
  assert.ok(html.includes('checking: "Checking…"'));
});

test("PACK_RESULT_CODE_TEXT maps every stable failure code from the CLI contract to its fixed copy", () => {
  const html = htmlSource();
  const expected = {
    "git-missing": "Git is required to download skill packs.",
    "download-failed": "Couldn't download this pack. Check the connection and try again.",
    "local-changes": "Local changes were preserved. See details for the affected skills.",
    "legacy-state": "This pack's install state needs attention.",
    "corrupt-state": "This pack's install state needs attention.",
    "busy": "Another skill-pack operation is running in this workspace.",
  };
  for (const [code, text] of Object.entries(expected)) {
    assert.ok(html.includes(`"${code}": "${text}"`), `expected PACK_RESULT_CODE_TEXT["${code}"] === "${text}"`);
  }
  // collision is handled separately (the CLI's own message verbatim, since
  // it names the colliding skill) — never given a fixed copy override.
  assert.doesNotMatch(html, /"collision":\s*"/, "collision must not have a fixed copy entry — it renders the CLI's message verbatim");
});

test("pack-action posts action:add|remove derived from the toggle's current aria-pressed, not the retired enable field", () => {
  const html = htmlSource();
  const scriptIdx = html.indexOf('<script nonce="__CSP_NONCE__">');
  const script = html.slice(scriptIdx);
  assert.match(script, /vscode\.postMessage\(\{ type: "pack-action", id, action: currentlyInstalled \? "remove" : "add" \}\)/);
  assert.doesNotMatch(script, /type:\s*"pack-toggle"/, "the retired pack-toggle message type must not be posted anywhere");
});

test("pack-details is posted with no id — the host reveals one shared output channel", () => {
  const html = htmlSource();
  const scriptIdx = html.indexOf('<script nonce="__CSP_NONCE__">');
  const script = html.slice(scriptIdx);
  assert.match(script, /vscode\.postMessage\(\{ type: "pack-details" \}\)/);
});

test("the pack list caption reads verbatim (spec §6 revision: .agents/skills/ + .claude/skills/, curated sources not contents)", () => {
  const html = htmlSource();
  assert.match(
    html,
    /Community Agent Skills — installed into <code>\.agents\/skills\/<\/code> and <code>\.claude\/skills\/<\/code>, covering Claude Code, Codex, Grok, Antigravity, Cursor, Gemini CLI, and opencode\. Zerops curates the sources, not their contents; packs never auto-update\. Claude Code and Codex pick up changes live — start a new session in other agents\./
  );
});

test('the Zerops Guided row carries the "Experimental · Claude Code only" chip', () => {
  const html = htmlSource();
  assert.match(html, /<span class="guided-chip">Experimental · Claude Code only<\/span>/);
});

// The locked note is now GUIDED-ONLY (spec §6 revision: skill packs dropped
// the claude-code gate entirely) — its copy no longer mentions packs at all.
test("the guided locked note reads the guided-only copy, with no mention of skill packs", () => {
  const html = htmlSource();
  assert.match(html, /data-guided-locked-note hidden>Authorize Claude Code first to use Zerops Guided\.<\/p>/);
  assert.doesNotMatch(html, /Guided and skill packs currently work with Claude Code only/);
});

test("no data-skill / skills-list remnants remain anywhere in the file", () => {
  const html = htmlSource();
  assert.doesNotMatch(html, /data-skill/);
  assert.doesNotMatch(html, /skills-list/);
  assert.doesNotMatch(html, /class="skill-/);
});

test("the showcase deploy button lives in the permanent box under the tour, outside any tour-slide section", () => {
  const html = htmlSource();
  // Every tour-slide section (0..5, per the pin above) must NOT carry the
  // showcase URL — slide 6 (the old Showcase slide) is gone entirely, and
  // the deploy path now lives in a permanent box rendered after the tour.
  const slideMatches = [...html.matchAll(/<section class="tour-slide[^>]*data-slide="\d+"[^>]*>([\s\S]*?)<\/section>/g)];
  assert.ok(slideMatches.length === 6, "sanity: expected exactly six tour-slide sections");
  for (const m of slideMatches) {
    assert.doesNotMatch(m[1], /showcase-recipe/, "the showcase URL must not live inside any tour-slide section");
  }

  // Anchored to the LAST slide MATCH's own end offset, not a bare
  // lastIndexOf('data-slide="') — the webview script's own tour-navigation
  // code (`document.querySelector(`[data-slide="${tourSlide}"]`)`) contains
  // that same substring further down the file and would otherwise win.
  const lastSlide = slideMatches[slideMatches.length - 1];
  const lastSlideEndIdx = lastSlide.index + lastSlide[0].length;
  const boxIdx = html.indexOf('<div class="showcase-grid">');
  assert.ok(boxIdx > lastSlideEndIdx, "expected the permanent showcase box to render after the last tour slide, outside it");
  const boxSection = html.slice(boxIdx, html.indexOf('<div class="docs-row">', boxIdx));
  assert.match(boxSection, /data-open-url="https:\/\/app\.zerops\.io\/recipes\/showcase-recipe"/, "expected the showcase deploy button inside the permanent box");
  assert.doesNotMatch(html, /data-back-start/, "data-back-start must not exist anywhere (host JS or markup)");
});

test('the recipes card and the permanent showcase box both carry "creates a new project" copy', () => {
  const html = htmlSource();
  const recipesCard = html.match(/<article class="way way-compact way-recipe" data-way-card="recipes">([\s\S]*?)<\/article>/);
  assert.ok(recipesCard, "expected the recipes way-card");
  assert.match(recipesCard[1], /creates a new project/);

  const boxIdx = html.indexOf('<div class="showcase-grid">');
  assert.ok(boxIdx >= 0, "expected the permanent showcase-grid box under the tour");
  const boxSection = html.slice(boxIdx, html.indexOf('<div class="docs-row">', boxIdx));
  assert.match(boxSection, /creates a new project/);
});

test("the overview live strip's state/CTA hooks and the tour nav button exist in the MARKUP, not just a script selector", () => {
  const html = htmlSource();
  // Scoped to before the inline <script> tag: a hook referenced only inside
  // the webview script's own template-literal/querySelector strings (e.g.
  // `document.querySelector("[data-overview-cta]")`) would otherwise let a
  // deleted markup element pass this check — the element itself must exist.
  const scriptIdx = html.indexOf('<script nonce="__CSP_NONCE__">');
  assert.ok(scriptIdx > 0, "expected the inline script tag");
  const markup = html.slice(0, scriptIdx);
  assert.match(markup, /data-overview-agents/);
  assert.match(markup, /data-overview-cta/);
  assert.match(markup, /data-goto-tour/);
});

test("the shared agent-status derivation carries its per-category copy", () => {
  const html = htmlSource();
  // The harness has no DOM/JS execution (see the file header comment), so
  // these are source-string pins on deriveAgentStatus's/railStatusText's
  // literal branch copy — proof that each independent axis (empty
  // availability, nothing installed, reconnect, local-only) gets its own
  // rail message instead of collapsing into a blanket "sign in".
  for (const text of ['"No agents enabled"', '"None installed"', '"Reconnect required"', '"Sync pending"']) {
    assert.ok(html.includes(text), `expected the rail status branch ${text}`);
  }
});

test("CSP meta carries the exact frame-src value for the in-panel YouTube embed", () => {
  const html = htmlSource();
  assert.match(
    html,
    /content="default-src 'none'; style-src 'nonce-__CSP_NONCE__'; script-src 'nonce-__CSP_NONCE__'; frame-src https:\/\/www\.youtube-nocookie\.com;"/
  );
});

// The demo video is a static iframe in the markup (no click-facade, no swap
// script — a prior design clicked a facade that swapped in a real iframe,
// which needed a second click because the webview's CSP has no `allow`
// chain for autoplay; rendering the real iframe directly makes YouTube's own
// play button the one and only click). Pinned as a source string, not just
// "the embed URL appears somewhere": exactly one <iframe> in the whole file,
// and its src is exactly the youtube-nocookie embed constant with no
// autoplay param.
test("the video iframe is static markup: exactly one <iframe> in the file, src exactly the youtube-nocookie embed constant", () => {
  const html = htmlSource();
  const iframes = [...html.matchAll(/<iframe\b[^>]*>/g)];
  assert.equal(iframes.length, 1, "expected exactly one <iframe> in welcome.html");
  assert.match(iframes[0][0], /src="https:\/\/www\.youtube-nocookie\.com\/embed\/spdmTicsIgg"/, "expected the iframe src to be exactly the youtube-nocookie embed constant, no autoplay param");
});

// The redesigned concepts way-card dropped the six overview chips (they
// duplicated the tour's own navigation) in favor of one sentence naming the
// tour's scope, plus a way-meta line carrying the count/duration that used
// to live on the card footer's way-note.
test("the concepts way-card carries the one-sentence tour summary, not the retired six-chip mini-grid", () => {
  const html = htmlSource();
  const card = html.match(/<article class="way way-compact" data-way-card="concepts">([\s\S]*?)<\/article>/);
  assert.ok(card, "expected the concepts way-card");
  assert.match(card[1], /Take a six-step tour of projects, services, private networking,\s*deployments, and Project Core\./);
  assert.match(card[1], /<span class="way-meta">6 concepts · about 2 min<\/span>/);
  assert.doesNotMatch(card[1], /class="core-grid"/, "the six-chip core-grid was retired from the overview concepts card");
});
