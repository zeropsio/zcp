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

test("Claude onboard arms the wrapper marker and opens a fresh session in the welcome's column", () => {
  const { calls, registry, markerWrites, panel } = openWelcome("claude-code", "ZCP_AGENT_TOKEN_CLAUDE_CODE");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].mode, "extension");
  assert.notEqual(calls[0].agent, registry["claude-code"]);
  // editor.open(sessionId=undefined, initialPrompt=undefined, viewColumn): a
  // FRESH session (the wrapper delivers the submit, no prompt arg) opened in
  // the welcome's OWN column so the agent is full width without disposing the
  // welcome (disposing races the session subscribe — the inconsistent onboard).
  assert.deepEqual(calls[0].agent.opens[0].args, [undefined, undefined, panel.viewColumn]);
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

test("onboard retains the welcome (does not dispose it) — the agent takes its column instead", () => {
  const { panel } = openWelcome("claude-code", "ZCP_AGENT_TOKEN_CLAUDE_CODE");
  assert.equal(panel.disposed, false, "disposing the welcome races the agent's session subscribe and drops the onboarding turn");
});

test("a rapid second onboard is single-flighted (no competing session spawned)", () => {
  const fake = { writes: [], impl: { mkdirSync: () => {}, writeFileSync: (p, d) => fake.writes.push({ p, d }), existsSync: () => false } };
  const calls = [];
  const { stub, extensionDir, welcome } = loadWelcome();
  welcome.open(
    { subscriptions: [], extensionPath: extensionDir },
    {
      REGISTRY: registryWithCommands(),
      ALL_AGENT_IDS: TEST_AGENT_IDS,
      readZembedEnv: () => ({ ZCP_AGENT_TOKEN_CLAUDE_CODE: "true" }),
      runAgentAction: (agent, mode) => calls.push({ agent, mode }),
      fs: fake.impl,
      homeDir: HOME,
      workspaceRoot: null,
    }
  );
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  panel.webview.__fireMessage({ type: "onboard", agentId: "claude-code" });
  panel.webview.__fireMessage({ type: "onboard", agentId: "claude-code" }); // rapid repeat

  assert.equal(calls.length, 1, "the second click within the single-flight window must not launch a competing session");
  assert.equal(fake.writes.length, 1, "and must not re-arm the one-shot marker");
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
