"use strict";

// Tier-A terminal login flow (docs/spec-welcome-mode.md §4, W-AUTH): the
// webview's {type:"authorize-terminal"} click opens a real terminal running
// the agent's verbatim login command; completion is detected by the P2
// credential watcher (simulated here via a direct state recompute, see the
// comment on credFs below), which runs `zcp agent mark-oauth` and releases
// the flow. Shares the SAME one-flight slot as the bridge flow
// (bridge_flow.test.js) — enforced host-side, spec §4.

const test = require("node:test");
const assert = require("node:assert/strict");
const path = require("node:path");
const { EventEmitter } = require("node:events");
const { loadWelcome, makeFakeTimers, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

const AUTH_FLOW_CAP_MS = 10 * 60 * 1000;
const CLAUDE_CRED_SUFFIX = path.join(".claude", ".credentials.json");

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
  return { stub, panel, welcome, ctx, deps };
}

function authMessages(panel) {
  return panel.postedMessages.filter((m) => m.type === "auth");
}

// credFs stands in for deps.fs: existsSync reports the claude-code
// credential path present iff the test has flipped `present` — the same
// signal watchCredDir's real fs.watch event would report, without needing
// a real filesystem or waiting on real fs.watch timing (see the flakiness
// note on this exact scenario in watchers.test.js). watch() is a no-op
// stub since these tests trigger the recompute directly (firing "ready"),
// not through a real watcher event.
function credFs() {
  let present = false;
  return {
    fs: {
      existsSync: (p) => present && String(p).endsWith(CLAUDE_CRED_SUFFIX),
      watch: () => ({ close() {} }),
    },
    setPresent: (v) => { present = v; },
  };
}

// fakeSpawn stands in for deps.spawn: records every call and returns an
// EventEmitter shaped like Node's real ChildProcess, firing "error" or
// "exit" asynchronously (via setImmediate) — real child_process.spawn()
// essentially never throws synchronously; failures surface through these
// events, which is exactly what runMarkOAuth's .on() handlers listen for.
function fakeSpawn(calls, behavior) {
  return (cmd, args, opts) => {
    calls.push({ cmd, args, opts });
    const child = new EventEmitter();
    setImmediate(() => {
      if (behavior === "enoent") child.emit("error", Object.assign(new Error("spawn zcp ENOENT"), { code: "ENOENT" }));
      else if (behavior === "fail") child.emit("exit", 1);
      else child.emit("exit", 0);
    });
    return child;
  };
}

function flushAsync() {
  return new Promise((resolve) => setImmediate(resolve));
}

test("authorize-terminal for claude-code creates a terminal, sends the login command, and shows it", () => {
  const { panel, stub } = openWelcome();

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "claude-code" });

  assert.equal(stub.terminals.length, 1);
  const term = stub.terminals[0];
  assert.equal(term.name, "Zerops: Claude Code login");
  assert.deepStrictEqual(term.sent, [{ text: "claude /login", addNewLine: true }]);
  assert.equal(term.shownCount, 1);

  // In flight: a second authorize-terminal (even for a different agent) replies busy.
  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "codex" });
  assert.equal(stub.terminals.length, 1, "one-flight: a second terminal must not be created while one is in flight");
  assert.ok(authMessages(panel).some((m) => m.phase === "busy" && m.agentId === "codex"));
});

test("authorize-terminal for codex sends the exact registry login command", () => {
  const { panel, stub } = openWelcome();

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "codex" });

  assert.equal(stub.terminals.length, 1);
  const term = stub.terminals[0];
  assert.equal(term.name, "Zerops: Codex login");
  assert.deepStrictEqual(term.sent, [{ text: "codex login --device-auth", addNewLine: true }]);
});

test("authorize-terminal for an agent with no Tier-A command replies unsupported and creates no terminal", () => {
  const { panel, stub } = openWelcome();

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "grok" });

  assert.equal(stub.terminals.length, 0);
  assert.ok(authMessages(panel).some((m) => m.phase === "unsupported" && m.agentId === "grok"));
});

test("authorize-terminal for antigravity (no Tier-A command) replies unsupported and creates no terminal", () => {
  const { panel, stub } = openWelcome();

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "antigravity" });

  assert.equal(stub.terminals.length, 0);
  assert.ok(authMessages(panel).some((m) => m.phase === "unsupported" && m.agentId === "antigravity"));
});

test("authorize-terminal for cursor (no Tier-A command) replies unsupported and creates no terminal", () => {
  const { panel, stub } = openWelcome();

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "cursor" });

  assert.equal(stub.terminals.length, 0);
  assert.ok(authMessages(panel).some((m) => m.phase === "unsupported" && m.agentId === "cursor"));
});

test("authorize-terminal for an uninstalled agent replies unsupported before the LOGIN_COMMANDS lookup, creating no terminal", () => {
  const { panel, stub } = openWelcome({ isAgentInstalled: (bin) => bin !== "codex" });

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "codex" });

  assert.equal(stub.terminals.length, 0);
  assert.ok(authMessages(panel).some((m) => m.phase === "unsupported" && m.agentId === "codex"));
});

test("a credential appearing after a terminal login runs mark-oauth and releases the flow", () => {
  const { fs, setPresent } = credFs();
  const spawnCalls = [];
  const { panel } = openWelcome({ fs, spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "claude-code" });
  assert.equal(authMessages(panel).filter((m) => m.phase === "idle").length, 0, "not released yet");

  setPresent(true);
  panel.webview.__fireMessage({ type: "ready" }); // triggers a state recompute -> reconcile observes the credential

  assert.equal(spawnCalls.length, 1);
  assert.deepStrictEqual(spawnCalls[0], { cmd: "zcp", args: ["agent", "mark-oauth", "claude-code"], opts: { shell: false } });
  assert.ok(
    authMessages(panel).some((m) => m.phase === "idle" && m.agentId === "claude-code"),
    "flow must release to idle once the credential appears"
  );

  // Released: a fresh authorize-terminal now succeeds instead of replying busy.
  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "codex" });
  assert.equal(authMessages(panel).filter((m) => m.phase === "busy").length, 0, "must be released, not busy");
});

test("a failed mark-oauth spawn logs a warning without throwing, and the agent stays local-only", async () => {
  const { fs, setPresent } = credFs();
  const spawnCalls = [];
  const { panel } = openWelcome({ fs, spawn: fakeSpawn(spawnCalls, "enoent") });

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "claude-code" });
  setPresent(true);

  const originalWarn = console.warn;
  const warnings = [];
  console.warn = (...args) => warnings.push(args.map(String).join(" "));
  try {
    assert.doesNotThrow(() => panel.webview.__fireMessage({ type: "ready" }));
    await flushAsync();
  } finally {
    console.warn = originalWarn;
  }

  assert.ok(warnings.some((w) => w.includes("claude-code")), `expected a warning naming the agent, got: ${JSON.stringify(warnings)}`);

  const lastState = panel.postedMessages.filter((m) => m.type === "state").pop();
  assert.equal(
    lastState.payload.agents.find((a) => a.id === "claude-code").state,
    "local-only",
    "the platform flag was never set, so the agent must still read local-only"
  );
});

test("closing the login terminal releases the flow", () => {
  const { panel, stub } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "claude-code" });
  const term = stub.terminals[0];

  // Closing an unrelated terminal must not release the flow.
  stub.exports.window.__fireCloseTerminal({ name: "unrelated" });
  assert.equal(authMessages(panel).filter((m) => m.phase === "idle").length, 0);

  stub.exports.window.__fireCloseTerminal(term);

  assert.ok(authMessages(panel).some((m) => m.phase === "idle" && m.agentId === "claude-code"));

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "codex" });
  assert.equal(stub.terminals.length, 2, "released: a second terminal can now be created");
});

test("a 10-minute cap releases an in-flight terminal login", () => {
  const timers = makeFakeTimers();
  const { panel, stub } = openWelcome({ setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout });

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "claude-code" });
  const capCall = timers.calls.find((c) => c.ms === AUTH_FLOW_CAP_MS);
  assert.ok(capCall, "expected a 10-minute cap timer to be scheduled");

  timers.fire(capCall.id);

  assert.ok(authMessages(panel).some((m) => m.phase === "idle" && m.agentId === "claude-code"));
  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "codex" });
  assert.equal(stub.terminals.length, 2, "released: a second terminal can now be created");
});
