"use strict";

// Panel accessibility (docs/spec-welcome-mode.md §6 a11y, invariant W15): on
// a state-delta re-render, keyboard focus is retained when the focused node
// survives; when it disappears (e.g. `Authorize` -> `Open terminal`), focus
// moves to the replacement primary action, falling back to the row
// container — never dropped to body. The row's new state is announced once,
// concisely, through a polite live region. Drives the REAL webview script
// via jsdom (harness.js's loadWebviewDom) — focus behavior cannot be
// falsified any other way in this suite family.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWebviewDom } = require("./harness.js");

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
        { id: "anthropic-skills", state: "absent", managed: false },
      ],
      dataStudio: { available: false },
      bridge: { status: "unknown" },
      environment: { zembed: false },
      diagnostics: { zembedSeen: false, extensionVersion: "-", serviceId: "-", lastBridgeOutcome: "-", embedded: null },
    },
    overrides
  );
}

test("focus is RETAINED when the focused control survives an unrelated state delta", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code" }), agentFixture({ id: "codex" })] }) });

  const authBtn = document.querySelector('[data-agent-row="claude-code"] [data-authorize]');
  authBtn.focus();
  assert.equal(document.activeElement, authBtn);

  // A DIFFERENT agent's state changes — claude-code's own row (and its
  // focused Authorize button) is untouched.
  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code" }), agentFixture({ id: "codex", state: "authorized" })] }) });

  assert.equal(document.activeElement, authBtn, "focus must survive a re-render that doesn't touch the focused row");
});

test("focus MOVES to the replacement primary action when the focused control disappears (Authorize -> Open terminal)", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "not-authorized" })] }) });

  const authBtn = document.querySelector('[data-agent-row="claude-code"] [data-authorize]');
  authBtn.focus();
  assert.equal(document.activeElement, authBtn);

  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "authorized" })] }) });

  const termBtn = document.querySelector('[data-agent-row="claude-code"] [data-open-terminal]');
  assert.equal(document.activeElement, termBtn, "focus must move to the row's new primary action");
});

test("focus NEVER drops to body: it falls back to the row container when every action disappears (Authorize -> not-installed)", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "not-authorized", installed: true })] }) });

  const authBtn = document.querySelector('[data-agent-row="claude-code"] [data-authorize]');
  authBtn.focus();

  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "not-authorized", installed: false })] }) });

  const row = document.querySelector('[data-agent-row="claude-code"]');
  assert.equal(document.activeElement, row, "focus must fall back to the row container, never to body");
  assert.notEqual(document.activeElement, document.body);
});

test("focus MOVES off a transport-phase transition too (Authorize -> authorizing has no actions -> falls back to the row)", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "not-authorized" })] }) });

  const authBtn = document.querySelector('[data-agent-row="claude-code"] [data-authorize]');
  authBtn.focus();

  postToWebview({ type: "auth", agentId: "claude-code", phase: "contacting" });

  const row = document.querySelector('[data-agent-row="claude-code"]');
  assert.equal(document.activeElement, row, "authorizing has no actions — focus falls back to the row, never body");
});

test("focus RETAINED across a transport-phase transition when the previously-focused control still has a home (dashboard-unreachable keeps a Try again primary action)", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "not-authorized" })] }) });
  postToWebview({ type: "auth", agentId: "claude-code", phase: "no-dashboard" });

  const tryAgainBtn = document.querySelector('[data-agent-row="claude-code"] [data-authorize]');
  assert.equal(tryAgainBtn.hidden, false);
  tryAgainBtn.focus();
  assert.equal(document.activeElement, tryAgainBtn);

  // A different agent's push must not disturb this one's focus.
  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "not-authorized" }), agentFixture({ id: "codex" })] }) });
  postToWebview({ type: "auth", agentId: "claude-code", phase: "no-dashboard" });

  assert.equal(document.activeElement, tryAgainBtn, "the SAME button node survives an idempotent re-render of the same phase");
});

// ---- live region: announced once per delta -----------------------------

test("the row's status line is a polite live region, announced once per state delta (idempotent re-render leaves it untouched)", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "not-authorized" })] }) });

  const statusEl = document.querySelector('[data-agent-row="claude-code"] [data-agent-status]');
  assert.equal(statusEl.getAttribute("aria-live"), "polite");
  const firstText = statusEl.textContent;
  assert.notEqual(firstText, "");

  // Re-pushing the IDENTICAL state must not re-write the live region's text
  // (a redundant identical write would cost a second, spurious AT
  // announcement for a delta that never happened).
  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "not-authorized" })] }) });
  assert.equal(statusEl.textContent, firstText);
});

test("a real state delta updates the live region to new, distinct text", () => {
  const { document, postToWebview } = loadWebviewDom();
  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "not-authorized" })] }) });
  const statusEl = document.querySelector('[data-agent-row="claude-code"] [data-agent-status]');
  const before = statusEl.textContent;

  postToWebview({ type: "state", payload: baseState({ agents: [agentFixture({ id: "claude-code", state: "authorized" })] }) });

  assert.notEqual(statusEl.textContent, before);
  assert.notEqual(statusEl.textContent, "");
});
