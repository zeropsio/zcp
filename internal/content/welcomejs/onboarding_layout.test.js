"use strict";

// §5.3 onboarding layout, terminal-panel half (docs/spec-welcome-mode.md):
// "the launched terminal maximized" — established deterministically at
// launch-command execution time. VS Code's panel-maximize is a TOGGLE with
// no query for current state, so extension.js's runTerminal tracks a
// session-scoped `panelMaximized` guess and only acts once per session. That
// guess goes STALE the moment anything un-maximizes the panel behind its
// back (a reload, the user, a second launch in the same window) — the
// onboarding path must never inherit that staleness: this file drives the
// REAL extension.js + welcome.js wiring (unlike launch_gate.test.js, which
// mocks runAgentAction) so the actual toggle command is observable via
// stub.executedCommands, and fakes the global setTimeout extension.js's
// runTerminal schedules its 250ms post-mount maximize decision on (an
// untestable raw timer with no deps injection seam, unlike welcome.js's
// own deps.setTimeout).

const test = require("node:test");
const assert = require("node:assert/strict");
const { mock } = require("node:test");
const fs = require("node:fs");
const path = require("node:path");
const { loadExtension } = require("./harness.js");

const ZEMBED_ENV_PATH = path.join("/etc/zerops-zembed", "env.json");
const BRIDGE_CHANNEL = "@zerops/zcp-agent-auth-bridge";
const ALLOWLISTED_ORIGIN = "https://app.zerops.io";

function newCtx(extensionDir) {
  return { subscriptions: [], extensionPath: extensionDir };
}

function writeStartupConfig(extensionDir, value) {
  fs.writeFileSync(path.join(extensionDir, "startup.json"), value);
}

function withZembedEnv(env) {
  const original = fs.readFileSync;
  fs.readFileSync = (p, ...rest) => {
    if (p === ZEMBED_ENV_PATH) return JSON.stringify(env);
    return original(p, ...rest);
  };
  return () => { fs.readFileSync = original; };
}

// dispatchRegisteredCommands makes the stub's executeCommand actually invoke
// the matching registered handler (mirrors welcome_dark.test.js) — needed so
// activate()'s own internal `executeCommand("zerops.panel", ...)` call
// really wires up the real welcome.js against the real extension.js
// collaborators (readZembedEnv, runAgentAction), not a no-op recording.
function dispatchRegisteredCommands(stub) {
  const recordCommand = stub.exports.commands.executeCommand;
  stub.exports.commands.executeCommand = async (id, ...args) => {
    await recordCommand(id, ...args);
    const handler = stub.registeredCommands.get(id);
    if (handler) return handler(...args);
    return undefined;
  };
}

function welcomePanel(stub) {
  return stub.panels.find((p) => p.viewType === "zeropsWelcome");
}

function fireLaunch(panel, eventId, agentId) {
  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "launch-agent", eventId, agentId, createdAt: Date.now() },
  });
}

function toggleCount(stub) {
  return stub.executedCommands.filter((c) => c.id === "workbench.action.toggleMaximizedPanel").length;
}

// flushMicrotasks drains the ENTIRE microtask queue via a macrotask boundary
// (setImmediate, left real — only setTimeout is mocked above), unlike a
// single `await Promise.resolve()`: dispatchRegisteredCommands wraps
// executeCommand in its own async function, so the toggle's `.then(() => {
// panelMaximized = true })` settles a few microtask hops deep, not one.
// Getting this wrong would make a test pass whether or not panelMaximized
// actually latched before the next launch — exactly the false-positive this
// suite exists to rule out.
function flushMicrotasks() {
  return new Promise((resolve) => setImmediate(resolve));
}

test("an onboarding launch in a fresh window deterministically maximizes the terminal panel", async () => {
  const restoreEnv = withZembedEnv({ ZCP_AGENTS: "codex" });
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const { stub, extension, extensionDir } = loadExtension();
    writeStartupConfig(extensionDir, JSON.stringify({ agentFirst: true }));
    dispatchRegisteredCommands(stub);

    await extension.activate(newCtx(extensionDir));
    const panel = welcomePanel(stub);
    assert.ok(panel, "agent-first activation must open the receiver");

    fireLaunch(panel, "11111111-1111-4111-8111-111111111111", "codex");
    mock.timers.tick(250); // let runTerminal's post-mount maximize decision fire
    await flushMicrotasks();

    assert.equal(toggleCount(stub), 1, "a fresh window's panel is never maximized yet — the onboarding launch must toggle it");
  } finally {
    mock.timers.reset();
    restoreEnv();
  }
});

test("the stale-flag failure mode is gone from the onboarding path: a second onboarding launch in the same window still maximizes, even though the session-scoped panelMaximized guess already latched true", async () => {
  const restoreEnv = withZembedEnv({ ZCP_AGENTS: "codex" });
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    const { stub, extension, extensionDir } = loadExtension();
    writeStartupConfig(extensionDir, JSON.stringify({ agentFirst: true }));
    dispatchRegisteredCommands(stub);

    await extension.activate(newCtx(extensionDir));
    const panel = welcomePanel(stub);

    // First onboarding launch: latches the session-scoped panelMaximized
    // guess to true (pre-fix, this alone would silently gate every later
    // launch's maximize for the rest of the window's life).
    fireLaunch(panel, "11111111-1111-4111-8111-111111111111", "codex");
    mock.timers.tick(250);
    await flushMicrotasks(); // let the first toggle's .then() latch panelMaximized = true
    assert.equal(toggleCount(stub), 1);

    // Second onboarding launch (a dev-loop retry — a fresh eventId, same
    // window): the owner's reported defect. Pre-fix this is silently
    // skipped because panelMaximized already reads true; the fix must
    // toggle again regardless of that stale guess.
    fireLaunch(panel, "22222222-2222-4222-8222-222222222222", "codex");
    mock.timers.tick(250);
    await flushMicrotasks();

    assert.equal(toggleCount(stub), 2, "the onboarding path must not skip the toggle because of the stale session-scoped panelMaximized flag");
  } finally {
    mock.timers.reset();
    restoreEnv();
  }
});
