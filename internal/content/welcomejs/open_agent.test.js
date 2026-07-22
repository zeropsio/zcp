"use strict";

// open-agent (docs/spec-welcome-mode.md §7): the redesigned UI's per-row
// "Open" button — {type:"open-agent", agentId} launches exactly one agent
// via deps.runAgentAction, with no clipboard write and no kickoff prompt
// (contrast the CTA's {type:"start-onboarding"}, cta_flow.test.js).
// Re-validates against a FRESH state read: the agent must be installed AND
// authorized/authorized-token (runnable) right now, never the webview's own
// idea of it.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

function openWelcome(extraDeps) {
  const { stub, extensionDir, welcome } = loadWelcome();
  const ctx = { subscriptions: [], extensionPath: extensionDir };
  const deps = Object.assign(
    {
      REGISTRY: TEST_REGISTRY,
      ALL_AGENT_IDS: TEST_AGENT_IDS,
      readZembedEnv: () => null,
      runAgentAction: () => {},
      homeDir: "/nonexistent/zcp-welcomejs-home",
      workspaceRoot: null,
    },
    extraDeps
  );
  welcome.open(ctx, deps);
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  return { stub, panel };
}

function authMessages(panel) {
  return panel.postedMessages.filter((m) => m.type === "auth");
}

function recordingRunAgentAction() {
  const calls = [];
  return { calls, runAgentAction: (agent, mode) => calls.push({ agent, mode }) };
}

test("open-agent for an installed + authorized agent calls runAgentAction with the registry entry and its primary open mode", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true" }),
    runAgentAction,
  });

  panel.webview.__fireMessage({ type: "open-agent", agentId: "antigravity" });

  assert.equal(calls.length, 1);
  assert.equal(calls[0].agent, TEST_REGISTRY["antigravity"]);
  assert.equal(calls[0].mode, TEST_REGISTRY["antigravity"].opens[0].mode);
  assert.equal(authMessages(panel).length, 0, "a successful open-agent posts no auth phase message");
});

test("open-agent for an installed + authorized-token agent also launches", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_AGENT_TOKEN_CODEX: "some-token-value" }),
    runAgentAction,
  });

  panel.webview.__fireMessage({ type: "open-agent", agentId: "codex" });

  assert.equal(calls.length, 1);
  assert.equal(calls[0].agent, TEST_REGISTRY["codex"]);
});

test("open-agent for a not-authorized agent replies unsupported and never launches", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ runAgentAction });

  panel.webview.__fireMessage({ type: "open-agent", agentId: "antigravity" });

  assert.equal(calls.length, 0);
  assert.ok(authMessages(panel).some((m) => m.phase === "unsupported" && m.agentId === "antigravity"));
});

test("open-agent for an authorized but uninstalled agent replies unsupported and never launches", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true" }),
    isAgentInstalled: () => false,
    runAgentAction,
  });

  panel.webview.__fireMessage({ type: "open-agent", agentId: "antigravity" });

  assert.equal(calls.length, 0);
  assert.ok(authMessages(panel).some((m) => m.phase === "unsupported" && m.agentId === "antigravity"));
});

test("open-agent for an agent this container doesn't offer (unavailable) replies unsupported and never launches", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true" }),
    resolveAvailableAgentIds: () => ["claude-code"],
    runAgentAction,
  });

  panel.webview.__fireMessage({ type: "open-agent", agentId: "antigravity" });

  assert.equal(calls.length, 0);
  assert.ok(authMessages(panel).some((m) => m.phase === "unsupported" && m.agentId === "antigravity"));
});

test("open-agent for an unknown agent id is silently dropped by the allowlist gate — no runAgentAction, no auth message", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ runAgentAction });

  panel.webview.__fireMessage({ type: "open-agent", agentId: "not-a-real-agent" });

  assert.equal(calls.length, 0);
  assert.equal(authMessages(panel).length, 0);
});

test("open-agent with a non-string agentId is dropped by the allowlist gate", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ runAgentAction });

  panel.webview.__fireMessage({ type: "open-agent", agentId: 12345 });

  assert.equal(calls.length, 0);
  assert.equal(authMessages(panel).length, 0);
});
