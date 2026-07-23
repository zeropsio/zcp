"use strict";

// Per-row onboarding (docs/spec-welcome-mode.md §7): a runnable agent starts
// with the fixed onboarding prompt already SUBMITTED. The Claude plugin's
// editor.open only prefills, so its submit is delivered out-of-band by the
// process wrapper — handleOnboard arms a HOME-based kickoff marker and opens a
// fresh (unseeded) plugin panel. Terminal agents carry the prompt in argv.
// The shared registry launch commands (Open/CTA) are never mutated.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

const ONBOARD_PROMPT = "Onboard me to Zerops.";
const HOME = "/home/zcp-onboard-test";

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

// Captures the kickoff marker write without touching the real filesystem.
function fsStub() {
  const writes = [];
  return {
    writes,
    impl: {
      mkdirSync: () => {},
      writeFileSync: (path, data) => writes.push({ path, data }),
      existsSync: () => false,
    },
  };
}

function openWelcome(agentId, envKey, registry = registryWithCommands()) {
  const calls = [];
  const fake = fsStub();
  const { stub, extensionDir, welcome } = loadWelcome();
  welcome.open(
    { subscriptions: [], extensionPath: extensionDir },
    {
      REGISTRY: registry,
      ALL_AGENT_IDS: TEST_AGENT_IDS,
      readZembedEnv: () => ({ [envKey]: "true" }),
      runAgentAction: (agent, mode) => calls.push({ agent, mode }),
      fs: fake.impl,
      homeDir: HOME,
      workspaceRoot: null,
      setTimeout: (fn) => { fn(); return 0; }, // fire the deferred welcome-close synchronously
    }
  );
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  panel.webview.__fireMessage({ type: "onboard", agentId });
  return { panel, calls, registry, markerWrites: fake.writes };
}

test("Claude onboard arms the wrapper marker and opens a fresh (unseeded) plugin panel", () => {
  const { calls, registry, markerWrites } = openWelcome("claude-code", "ZCP_AGENT_TOKEN_CLAUDE_CODE");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].mode, "extension");
  assert.notEqual(calls[0].agent, registry["claude-code"]);
  // The plugin open carries NO prompt arg — the wrapper delivers the submit.
  assert.equal(calls[0].agent.opens[0].args, undefined);
  assert.equal(calls[0].agent.opens[0].command, "claude-vscode.editor.open");
  // The terminal fallback is still seeded so an absent plugin keeps the promise.
  assert.equal(calls[0].agent.opens[1].command, registry["claude-code"].opens[1].command + " '" + ONBOARD_PROMPT + "'");
  // The kickoff marker is armed at ~/.zcp/state with the prompt.
  assert.equal(markerWrites.length, 1);
  assert.match(markerWrites[0].path, /[/\\]\.zcp[/\\]state[/\\]claude-kickoff\.json$/);
  assert.equal(JSON.parse(markerWrites[0].data).prompt, ONBOARD_PROMPT);
  // The shared registry entry is never mutated.
  assert.equal(registry["claude-code"].opens[0].args, undefined, "shared registry entry must remain unseeded");
  assert.equal(registry["claude-code"].opens[1].command, "claude --dangerously-skip-permissions --effort max");
});

test("onboard closes the welcome surface so the agent takes the full width", () => {
  const { panel } = openWelcome("claude-code", "ZCP_AGENT_TOKEN_CLAUDE_CODE");
  assert.equal(panel.disposed, true);
});

test("terminal onboard appends the shell-quoted positional initial prompt and arms no marker", () => {
  const { calls, registry, markerWrites } = openWelcome("codex", "ZCP_AGENT_TOKEN_CODEX");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].mode, "terminal");
  assert.equal(calls[0].agent.opens[0].command, registry.codex.opens[0].command + " '" + ONBOARD_PROMPT + "'");
  assert.equal(registry.codex.opens[0].command, "codex --dangerously-bypass-approvals-and-sandbox");
  assert.equal(markerWrites.length, 0, "a terminal agent never touches the kickoff marker");
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
  const fake = fsStub();
  const { stub, extensionDir, welcome } = loadWelcome();
  welcome.open(
    { subscriptions: [], extensionPath: extensionDir },
    {
      REGISTRY: registryWithCommands(),
      ALL_AGENT_IDS: TEST_AGENT_IDS,
      readZembedEnv: () => null,
      runAgentAction: (agent, mode) => calls.push({ agent, mode }),
      fs: fake.impl,
      homeDir: HOME,
      workspaceRoot: null,
      setTimeout: (fn) => { fn(); return 0; },
    }
  );
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");

  panel.webview.__fireMessage({ type: "onboard", agentId: "codex" });

  assert.equal(calls.length, 0);
  assert.equal(fake.writes.length, 0);
  assert.equal(panel.disposed, false, "a rejected onboard must not close the welcome");
  assert.ok(panel.postedMessages.some((m) => m.type === "auth" && m.agentId === "codex" && m.phase === "unsupported"));
});
