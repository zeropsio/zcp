"use strict";

// The agent panel's structural contract (docs/spec-welcome-mode.md §6/§7):
// every §3 matrix state and §4.2 transport phase maps to exactly one row
// state (the §6 table), the collapsed-list rule, the empty-available honest
// state, the Data Studio box's informative-disabled state, and "no onboard
// action exists anywhere" (§6). Drives the REAL webview script via jsdom
// (harness.js's loadWebviewDom) — the only way to falsify the row-state
// projection against the actual artifact zcp ships.
//
// Also the relocated half of the retired ui_structure.test.js (the split
// this slice's brief calls for): the surfaces the redesign KEEPS — the five
// agent-row ids (W9), the three pack-row ids + gstack absence (W7), the
// guided chip + locked note (W6), AUTH_PHASE_TEXT (§4.2), the no-innerHTML +
// nonce pins (§9), and the handleBridgeSend createdAt re-stamp (the ONLY
// in-repo pin of W5's "stamped by the sending browser context"). The
// journey/tour halves died with the walk-through surface (§11).

const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWebviewDom, TEMPLATES_DIR } = require("./harness.js");

function htmlSource() {
  return fs.readFileSync(path.join(TEMPLATES_DIR, "vscode-bootstrap-welcome.html"), "utf8");
}

// ---- fixtures --------------------------------------------------------

function agentFixture(overrides) {
  return Object.assign(
    { id: "claude-code", label: "Claude Code", state: "not-authorized", probeVerified: true, installed: true },
    overrides
  );
}

function baseState(overrides) {
  return Object.assign(
    {
      agents: [],
      anyAuthorized: false,
      guided: { state: "disabled" },
      packs: [
        { id: "matt-pocock-skills", state: "absent", managed: false, retired: false, revision: "rev:1", selected: [], catalog: [] },
        { id: "superpowers", state: "absent", managed: false },
        { id: "andrej-karpathy-skills", state: "absent", managed: false },
      ],
      dataStudio: { available: false },
      bridge: { status: "unknown" },
      environment: { zembed: false },
      diagnostics: { zembedSeen: false, extensionVersion: "-", serviceId: "-", lastBridgeOutcome: "-", embedded: null },
    },
    overrides
  );
}

function pushState(postToWebview, agents) {
  postToWebview({ type: "state", payload: baseState({ agents }) });
}

function rowState(document, id) {
  const row = document.querySelector(`[data-agent-row="${id}"]`);
  return row ? row.getAttribute("data-row-state") : null;
}

function visibleActions(document, id) {
  const row = document.querySelector(`[data-agent-row="${id}"]`);
  const out = [];
  const authBtn = row.querySelector("[data-authorize]");
  const termBtn = row.querySelector("[data-open-terminal]");
  const extBtn = row.querySelector("[data-open-extension]");
  if (authBtn && !authBtn.hidden) out.push(authBtn.textContent);
  if (termBtn && !termBtn.hidden) out.push("Open terminal");
  if (extBtn && !extBtn.hidden) out.push("Open extension");
  return out;
}

// ---- 1. the §6 row-state matrix, table-driven -----------------------

const MATRIX_CASES = [
  { name: "not-authorized matrix -> not-authorized row, Authorize only", agent: { state: "not-authorized", installed: true }, phase: undefined, want: "not-authorized", actions: ["Authorize"] },
  { name: "local-only matrix -> local-only row, no actions", agent: { state: "local-only", installed: true }, phase: undefined, want: "local-only", actions: [] },
  { name: "authorized matrix -> authorized row, Open terminal (+ Open extension for claude-code)", agent: { id: "claude-code", state: "authorized", installed: true }, phase: undefined, want: "authorized", actions: ["Open terminal", "Open extension"] },
  { name: "authorized-token matrix -> authorized row", agent: { id: "codex", state: "authorized-token", installed: true }, phase: undefined, want: "authorized", actions: ["Open terminal"] },
  { name: "reconnect matrix -> reconnect row, Reconnect action", agent: { state: "reconnect", installed: true }, phase: undefined, want: "reconnect", actions: ["Reconnect"] },
  { name: "not installed (probe), any matrix -> not-installed row, no actions", agent: { state: "authorized", installed: false }, phase: undefined, want: "not-installed", actions: [] },
  { name: "transport contacting -> authorizing row, no actions", agent: { state: "not-authorized", installed: true }, phase: "contacting", want: "authorizing", actions: [] },
  { name: "transport dialog-opening -> authorizing row, no actions", agent: { state: "not-authorized", installed: true }, phase: "dialog-opening", want: "authorizing", actions: [] },
  { name: "transport no-dashboard -> dashboard-unreachable row, Try again action", agent: { state: "not-authorized", installed: true }, phase: "no-dashboard", want: "dashboard-unreachable", actions: ["Try again"] },
  { name: "transport gui-not-ready -> dashboard-unreachable row, Try again action", agent: { state: "not-authorized", installed: true }, phase: "gui-not-ready", want: "dashboard-unreachable", actions: ["Try again"] },
];

test("every §3 matrix state + §4.2 transport phase maps to exactly one row state, table-driven against the §6 table", () => {
  for (const c of MATRIX_CASES) {
    const { document, postToWebview } = loadWebviewDom();
    const agent = agentFixture(c.agent);
    pushState(postToWebview, [agent]);
    if (c.phase) postToWebview({ type: "auth", agentId: agent.id, phase: c.phase });

    assert.equal(rowState(document, agent.id), c.want, c.name);
    assert.deepStrictEqual(visibleActions(document, agent.id), c.actions, c.name + " (actions)");
  }
});

// ---- 2. collapsed list + expander --------------------------------------

test("zero active agents renders the full list with no expander", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushState(postToWebview, [
    agentFixture({ id: "claude-code", state: "not-authorized" }),
    agentFixture({ id: "codex", state: "not-authorized" }),
  ]);

  const expander = document.querySelector("[data-agent-expander]");
  assert.equal(expander.hidden, true);
  assert.equal(document.querySelector('[data-agent-row="claude-code"]').hidden, false);
  assert.equal(document.querySelector('[data-agent-row="codex"]').hidden, false);
});

test(">=1 active agent collapses to active rows + expander revealing the rest; toggling flips the label", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushState(postToWebview, [
    agentFixture({ id: "claude-code", state: "authorized" }),
    agentFixture({ id: "codex", state: "not-authorized" }),
    agentFixture({ id: "antigravity", state: "not-installed" }),
  ]);

  assert.equal(document.querySelector('[data-agent-row="claude-code"]').hidden, false, "the active row stays visible");
  assert.equal(document.querySelector('[data-agent-row="codex"]').hidden, true, "an inactive row collapses behind the expander");
  const expander = document.querySelector("[data-agent-expander]");
  assert.equal(expander.hidden, false);
  assert.equal(expander.textContent, "+ Add another agent");
  assert.equal(expander.getAttribute("aria-expanded"), "false");

  expander.click();

  assert.equal(document.querySelector('[data-agent-row="codex"]').hidden, false, "expanding reveals the rest");
  assert.equal(expander.textContent, "Hide available agents");
  assert.equal(expander.getAttribute("aria-expanded"), "true");

  expander.click();

  assert.equal(document.querySelector('[data-agent-row="codex"]').hidden, true, "collapsing hides it again");
});

test("the authorizing transport phase counts as active for the collapsed-list rule", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushState(postToWebview, [agentFixture({ id: "claude-code", state: "not-authorized" })]);
  postToWebview({ type: "auth", agentId: "claude-code", phase: "contacting" });

  const expander = document.querySelector("[data-agent-expander]");
  assert.equal(expander.hidden, true, "a single authorizing agent is itself the active set — no expander needed");
  assert.equal(document.querySelector('[data-agent-row="claude-code"]').hidden, false);
});

// ---- 3. empty availability ------------------------------------------

test("an explicitly empty available set renders the honest empty-availability state", () => {
  const { document, postToWebview } = loadWebviewDom();
  pushState(postToWebview, []);

  const emptyEl = document.querySelector("[data-agents-empty]");
  assert.equal(emptyEl.hidden, false);
  assert.equal(emptyEl.textContent, "No coding agents are enabled for this container.");
  for (const id of ["claude-code", "codex", "antigravity", "grok", "cursor"]) {
    assert.equal(document.querySelector(`[data-agent-row="${id}"]`).hidden, true);
  }
});

// ---- 4. Data Studio box -----------------------------------------------

test("Data Studio box is informative-disabled without the Studio extension", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ dataStudio: { available: false } }) });

  const btn = document.querySelector("[data-open-datastudio]");
  assert.equal(btn.disabled, true);
  assert.notEqual(document.querySelector("[data-ds-desc]").textContent, "", "expected a one-line diagnostic, never a silently dead button");
});

test("Data Studio box enables its action when the Studio extension is available", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ dataStudio: { available: true } }) });

  assert.equal(document.querySelector("[data-open-datastudio]").disabled, false);
});

test("clicking Open Data Studio posts open-datastudio", () => {
  const { document, postToWebview, sentMessages } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ dataStudio: { available: true } }) });

  document.querySelector("[data-open-datastudio]").click();

  assert.ok(sentMessages.some((m) => m.type === "open-datastudio"));
});

// ---- 5. no onboard action anywhere (§6) --------------------------------

test("no onboard action exists anywhere: no data-onboard control, no \"onboard\" message ever posted", () => {
  const { document, postToWebview, sentMessages } = loadWebviewDom();
  pushState(postToWebview, [agentFixture({ state: "authorized" })]);

  assert.equal(document.querySelectorAll("[data-onboard]").length, 0);
  for (const el of document.querySelectorAll("button")) el.click();
  assert.ok(!sentMessages.some((m) => m.type === "onboard"), "no control anywhere may post {type:\"onboard\"}");
});

test("welcome.js/welcome.html source no longer mentions onboard/CTA identifiers", () => {
  const html = htmlSource();
  for (const gone of ["data-onboard", "data-goto-build", "data-goto-tour", "start-onboarding", "cta-result", "CTA_PROMPTS"]) {
    assert.doesNotMatch(html, new RegExp(gone), `${gone} must not appear in welcome.html`);
  }
});

// ---- relocated pins (retired ui_structure.test.js's surviving half) -----

const AGENT_ROW_IDS = ["claude-code", "codex", "antigravity", "grok", "cursor"];

test("exactly five data-agent-row ids, in canonical registry order (W9)", () => {
  const html = htmlSource();
  const ids = [...html.matchAll(/<div class="agent-row" data-agent-row="([^"]+)"/g)].map((m) => m[1]);
  assert.deepStrictEqual(ids, AGENT_ROW_IDS);
});

const PACK_ROW_IDS = ["matt-pocock-skills", "superpowers", "andrej-karpathy-skills"];

test("exactly three data-pack-row ids, in canonical order (gstack excluded — internal/skillpacks/catalog.go) (W7)", () => {
  const html = htmlSource();
  const ids = [...html.matchAll(/<div class="pack-row" data-pack-row="([^"]+)">/g)].map((m) => m[1]);
  assert.deepStrictEqual(ids, PACK_ROW_IDS);
  assert.doesNotMatch(html, /gstack/, "gstack was excluded from the Go registry — it must not appear here either");
});

test('the Zerops Guided row carries the "Experimental" chip, with no agent restriction (W6)', () => {
  const html = htmlSource();
  assert.match(html, /<span class="guided-chip">Experimental<\/span>/);
  assert.doesNotMatch(html, /Claude Code only/, "guided works for every agent — the chip must not name one");
});

test("no guided locked note: guided gates on no agent at all (W6)", () => {
  const html = htmlSource();
  assert.doesNotMatch(html, /data-guided-locked-note/);
  assert.doesNotMatch(html, /Authorize Claude Code first/);
});

test("AUTH_PHASE_TEXT carries the contacting and gui-not-ready phases with their exact copy (§4.2)", () => {
  const html = htmlSource();
  assert.ok(html.includes('contacting: "Contacting the Zerops dashboard…"'));
  assert.ok(html.includes('"gui-not-ready": "The dashboard couldn\'t open the dialog — reload the Zerops page"'));
});

test("no .innerHTML, no inline style attributes, no raw <a href=\"http (§9 W-SEC)", () => {
  const html = htmlSource();
  assert.doesNotMatch(html, /\.innerHTML/);
  assert.doesNotMatch(html, / style="/);
  assert.doesNotMatch(html, /<a href="http/);
});

test("exactly one nonce'd style tag and one nonce'd script tag", () => {
  const html = htmlSource();
  assert.equal((html.match(/<style nonce="__CSP_NONCE__">/g) || []).length, 1);
  assert.equal((html.match(/<script nonce="__CSP_NONCE__">/g) || []).length, 1);
});

test("handleBridgeSend re-stamps payload.createdAt on the browser clock, between the guard and the postMessage (W5)", () => {
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
