"use strict";

// Per-row onboarding (docs/spec-welcome-mode.md §7): a runnable agent starts
// with the fixed onboarding prompt already submitted. The host re-validates
// runnable state and seeds a cloned registry entry, leaving the launch
// commands shared by Open/CTA untouched.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

const ONBOARD_PROMPT = "Onboard me to Zerops. Tell me where I am, what this project already has, and what I should do next.";

function registryWithCommands() {
  return {
    ...TEST_REGISTRY,
    "claude-code": {
      ...TEST_REGISTRY["claude-code"],
      opens: [
        { mode: "extension", command: "claude-vscode.editor.open" },
        { mode: "terminal", command: "claude --dangerously-skip-permissions --effort max" },
      ],
    },
    codex: {
      ...TEST_REGISTRY.codex,
      opens: [{ mode: "terminal", command: "codex --dangerously-bypass-approvals-and-sandbox" }],
    },
    antigravity: {
      ...TEST_REGISTRY.antigravity,
      opens: [{ mode: "terminal", command: "agy --dangerously-skip-permissions", initialPromptFlag: "--prompt-interactive" }],
    },
  };
}

function openWelcome(agentId, envKey, registry = registryWithCommands()) {
  const calls = [];
  const { stub, extensionDir, welcome } = loadWelcome();
  welcome.open(
    { subscriptions: [], extensionPath: extensionDir },
    {
      REGISTRY: registry,
      ALL_AGENT_IDS: TEST_AGENT_IDS,
      readZembedEnv: () => ({ [envKey]: "true" }),
      runAgentAction: (agent, mode) => calls.push({ agent, mode }),
      homeDir: "/nonexistent/zcp-welcomejs-home",
      workspaceRoot: null,
    }
  );
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  panel.webview.__fireMessage({ type: "onboard", agentId });
  return { panel, calls, registry };
}

test("Claude onboard passes editor.open initialPrompt and seeds its terminal fallback", () => {
  const { calls, registry } = openWelcome("claude-code", "ZCP_AGENT_TOKEN_CLAUDE_CODE");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].mode, "extension");
  assert.notEqual(calls[0].agent, registry["claude-code"]);
  assert.deepEqual(calls[0].agent.opens[0].args, [undefined, ONBOARD_PROMPT]);
  assert.equal(calls[0].agent.opens[1].command, registry["claude-code"].opens[1].command + " '" + ONBOARD_PROMPT + "'");
  assert.equal(registry["claude-code"].opens[0].args, undefined, "shared registry entry must remain unseeded");
  assert.equal(registry["claude-code"].opens[1].command, "claude --dangerously-skip-permissions --effort max");
});

test("terminal onboard appends the shell-quoted positional initial prompt", () => {
  const { calls, registry } = openWelcome("codex", "ZCP_AGENT_TOKEN_CODEX");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].mode, "terminal");
  assert.equal(calls[0].agent.opens[0].command, registry.codex.opens[0].command + " '" + ONBOARD_PROMPT + "'");
  assert.equal(registry.codex.opens[0].command, "codex --dangerously-bypass-approvals-and-sandbox");
});

test("Antigravity onboard uses its live-verified --prompt-interactive flag", () => {
  const { calls } = openWelcome("antigravity", "ZCP_AGENT_OAUTH_ANTIGRAVITY");

  assert.equal(calls.length, 1);
  assert.equal(
    calls[0].agent.opens[0].command,
    "agy --dangerously-skip-permissions --prompt-interactive '" + ONBOARD_PROMPT + "'"
  );
});

test("onboard rejects a non-runnable agent without launching", () => {
  const calls = [];
  const { stub, extensionDir, welcome } = loadWelcome();
  welcome.open(
    { subscriptions: [], extensionPath: extensionDir },
    {
      REGISTRY: registryWithCommands(),
      ALL_AGENT_IDS: TEST_AGENT_IDS,
      readZembedEnv: () => null,
      runAgentAction: (agent, mode) => calls.push({ agent, mode }),
      homeDir: "/nonexistent/zcp-welcomejs-home",
      workspaceRoot: null,
    }
  );
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");

  panel.webview.__fireMessage({ type: "onboard", agentId: "codex" });

  assert.equal(calls.length, 0);
  assert.ok(panel.postedMessages.some((m) => m.type === "auth" && m.agentId === "codex" && m.phase === "unsupported"));
});
