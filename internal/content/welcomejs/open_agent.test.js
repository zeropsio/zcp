"use strict";

// open-agent (docs/spec-welcome-mode.md §6, §5.2 W10): the panel's per-row
// `Open terminal`/`Open extension` actions — {type:"open-agent", agentId,
// mode} launches exactly one agent via deps.runAgentAction, explicit mode
// selection (never reg.opens[0] preference), no prompt either way (contrast
// the bridge's onboarding launch-agent, launch_gate.test.js, which seeds
// ONBOARD_PROMPT). The ONLY gate is identity (known registry id AND present
// in ZCP_AGENTS) — W10 inversion from the retired runnable (installed AND
// authorized) gate: "no probe ever gates a launch... re-validated host-side
// per action for panel launches too" is one universal rule for the bridge
// launch and the panel's Open terminal alike (§5.2).

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

function registryWithExtension() {
  return {
    ...TEST_REGISTRY,
    "claude-code": {
      ...TEST_REGISTRY["claude-code"],
      opens: [
        { mode: "extension", command: "claude-vscode.editor.open" },
        { mode: "terminal", command: "claude --dangerously-skip-permissions" },
      ],
    },
  };
}

function openWelcome(extraDeps) {
  const { stub, extensionDir, welcome } = loadWelcome();
  const ctx = { subscriptions: [], extensionPath: extensionDir };
  const deps = Object.assign(
    {
      REGISTRY: registryWithExtension(),
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
  return { calls, runAgentAction: (agent, mode, opts) => calls.push({ agent, mode, opts }) };
}

function fireOpen(panel, agentId, mode) {
  panel.webview.__fireMessage({ type: "open-agent", agentId, mode });
}

test("open-agent mode:terminal for an agent this container offers calls runAgentAction with ONLY the terminal open, promptless", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({
    REGISTRY: { ...registryWithExtension(), codex: { ...TEST_REGISTRY.codex, opens: [{ mode: "terminal", command: "codex --dangerously-bypass-approvals-and-sandbox" }] } },
    runAgentAction,
  });

  fireOpen(panel, "codex", "terminal");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].mode, "terminal");
  assert.equal(calls[0].agent.opens.length, 1, "only the requested open ships to the executor");
  assert.equal(calls[0].agent.opens[0].mode, "terminal");
  assert.equal(calls[0].agent.opens[0].command, "codex --dangerously-bypass-approvals-and-sandbox", "no prompt is appended — contrast the bridge's onboarding launch");
  assert.equal(authMessages(panel).length, 0, "a successful open posts no auth phase message");
});

test("open-agent mode:extension for claude-code calls runAgentAction with ONLY the extension open", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ runAgentAction });

  fireOpen(panel, "claude-code", "extension");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].mode, "extension");
  assert.equal(calls[0].agent.opens.length, 1);
  assert.equal(calls[0].agent.opens[0].mode, "extension");
});

test("W10: open-agent proceeds even when the agent is NOT authorized — identity gate only, no auth-flag gate", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ runAgentAction }); // no zembed flags at all

  fireOpen(panel, "codex", "terminal");

  assert.equal(calls.length, 1, "hiding the button client-side for a not-authorized row is convenience, not authority");
});

test("W10: open-agent proceeds even when isAgentInstalled reports false — no probe ever gates a launch", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ isAgentInstalled: () => false, runAgentAction });

  fireOpen(panel, "codex", "terminal");

  assert.equal(calls.length, 1, "the installed probe is display-only (0.1.5 false-negative regression)");
});

test("open-agent for an agent this container doesn't offer (ZCP_AGENTS excludes it) replies unsupported and never launches", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({
    resolveAvailableAgentIds: () => ["claude-code"],
    runAgentAction,
  });

  fireOpen(panel, "codex", "terminal");

  assert.equal(calls.length, 0);
  assert.ok(authMessages(panel).some((m) => m.phase === "unsupported" && m.agentId === "codex"));
});

test("open-agent mode:extension for an agent with no extension open in the registry replies unsupported and never launches", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ runAgentAction });

  fireOpen(panel, "codex", "extension"); // codex's registry entry declares only a terminal open

  assert.equal(calls.length, 0);
  assert.ok(authMessages(panel).some((m) => m.phase === "unsupported" && m.agentId === "codex"));
});

test("open-agent for an unknown agent id is silently dropped by the allowlist gate — no runAgentAction, no auth message", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ runAgentAction });

  fireOpen(panel, "not-a-real-agent", "terminal");

  assert.equal(calls.length, 0);
  assert.equal(authMessages(panel).length, 0);
});

test("open-agent with a non-string agentId is dropped by the allowlist gate", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ runAgentAction });

  panel.webview.__fireMessage({ type: "open-agent", agentId: 12345, mode: "terminal" });

  assert.equal(calls.length, 0);
  assert.equal(authMessages(panel).length, 0);
});

test("open-agent with a mode outside terminal/extension is dropped by the allowlist gate (bad enum)", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ runAgentAction });

  fireOpen(panel, "codex", "sideways");

  assert.equal(calls.length, 0);
  assert.equal(authMessages(panel).length, 0);
});

test("open-agent with a missing mode is dropped by the allowlist gate", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel } = openWelcome({ runAgentAction });

  panel.webview.__fireMessage({ type: "open-agent", agentId: "codex" });

  assert.equal(calls.length, 0);
  assert.equal(authMessages(panel).length, 0);
});

test("open-agent (panel's Open terminal) never establishes the §5.3 onboarding layout — only the bridge's onboarding launch owns the user's editors", () => {
  const { calls, runAgentAction } = recordingRunAgentAction();
  const { panel, stub } = openWelcome({
    REGISTRY: { ...registryWithExtension(), codex: { ...TEST_REGISTRY.codex, opens: [{ mode: "terminal", command: "codex --dangerously-bypass-approvals-and-sandbox" }] } },
    runAgentAction,
  });

  fireOpen(panel, "codex", "terminal");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].opts, undefined, "the panel path must never pass the onboarding layout option");
  assert.equal(stub.executedCommands.filter((c) => c.id === "workbench.action.closeAllEditors").length, 0, "must not close the user's editors");
  assert.equal(stub.executedCommands.filter((c) => c.id === "workbench.view.explorer").length, 0, "must not force Explorer open");
});
