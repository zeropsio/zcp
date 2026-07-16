"use strict";

// Guided toggle (docs/spec-welcome-mode.md §5, W-GUIDED; docs/spec-guided-mode.md
// §1-§3): the webview's {type:"guided-toggle", enable} click -> host guards
// (workspace, authoring, one-flight lock, dirty AGENTS.md/CLAUDE.md) -> folder
// selection (single vs quickpick) -> `zcp init [--guided]`, fixed argv, no
// shell -> an HONEST completion report (exit code AND a marker re-read, never
// output-prose parsing). The guided lock (`guidedFlow` in welcome.js) is
// SEPARATE from the P3 authFlow lock — guided and an agent authorization may
// run concurrently.

const test = require("node:test");
const assert = require("node:assert/strict");
const path = require("node:path");
const { EventEmitter } = require("node:events");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

const GUIDED_MARKER_REL = path.join(".zcp", "state", "guided");
const GUIDED_BUSY_MESSAGE = "A guided toggle is already running.";
const GUIDED_AUTHORING_MESSAGE = "Guided is user-only; authoring mode is active.";
const GUIDED_DIRTY_MESSAGE = "Save AGENTS.md/CLAUDE.md first — zcp init rewrites them.";
const GUIDED_ENOENT_MESSAGE = "zcp binary not found in PATH.";
const GUIDED_MARKER_MISMATCH_MESSAGE = "zcp init finished but the guided marker doesn't match — check the Zerops Welcome output.";
const GUIDED_PARTIAL_FAILURE_MESSAGE = "zcp init failed part-way — the preference may be recorded but surfaces are partially refreshed. Re-run from the toggle or run zcp init in a terminal (see output).";

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
      workspaceFolders: [],
    },
    extraDeps
  );
  welcome.open(ctx, deps);
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  return { stub, panel, welcome, ctx, deps };
}

function guidedResults(panel) {
  return panel.postedMessages.filter((m) => m.type === "guided-result");
}

// fsMarker stands in for deps.fs: existsSync reports the guided marker
// present iff its path ends with the marker's fixed relative path AND the
// test has asked for "present" — everything else (credential probes the
// post-completion state push also runs) reads absent, never leaking into an
// unrelated assertion.
function fsMarker(present) {
  return { existsSync: (p) => String(p).endsWith(GUIDED_MARKER_REL) && present };
}

// fakeSpawn mirrors terminal_flow.test.js's own helper: records every call
// and returns an EventEmitter shaped like Node's real ChildProcess, firing
// "error" or "exit" asynchronously (via setImmediate) — real
// child_process.spawn() essentially never throws synchronously.
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

// flush drains several rounds of the microtask+macrotask queues — the
// guided flow crosses at least one real setImmediate boundary before
// reaching deps.spawn (the folder-selection await) and another for the
// fake spawn's own exit/error emission, so a single flush is not enough;
// looping a handful of times is a cheap, deterministic way to clear both
// without hand-counting hops per test.
function flush(rounds = 4) {
  let p = Promise.resolve();
  for (let i = 0; i < rounds; i++) p = p.then(() => new Promise((resolve) => setImmediate(resolve)));
  return p;
}

test("spawn argv/cwd/shell pin — enable", async () => {
  const spawnCalls = [];
  const folder = "/tmp/zcp-guided-ws-enable";
  const { panel } = openWelcome({ workspaceFolders: [folder], spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  assert.equal(spawnCalls.length, 1);
  assert.deepStrictEqual(spawnCalls[0], { cmd: "zcp", args: ["init", "--guided"], opts: { cwd: folder, shell: false } });
});

test("spawn argv/cwd/shell pin — disable", async () => {
  const spawnCalls = [];
  const folder = "/tmp/zcp-guided-ws-disable";
  const { panel } = openWelcome({ workspaceFolders: [folder], spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: false });
  await flush();

  assert.equal(spawnCalls.length, 1);
  assert.deepStrictEqual(spawnCalls[0], { cmd: "zcp", args: ["init"], opts: { cwd: folder, shell: false } });
});

test("a dirty AGENTS.md under the selected folder blocks the run with no spawn", async () => {
  const spawnCalls = [];
  const folder = "/tmp/zcp-guided-ws-dirty-agents";
  const { panel } = openWelcome({
    workspaceFolders: [folder],
    textDocuments: () => [{ isDirty: true, uri: { fsPath: path.join(folder, "AGENTS.md") } }],
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  assert.equal(spawnCalls.length, 0);
  assert.deepStrictEqual(guidedResults(panel), [{ type: "guided-result", ok: false, message: GUIDED_DIRTY_MESSAGE }]);
});

test("a dirty CLAUDE.md under the selected folder also blocks the run", async () => {
  const spawnCalls = [];
  const folder = "/tmp/zcp-guided-ws-dirty-claude";
  const { panel } = openWelcome({
    workspaceFolders: [folder],
    textDocuments: () => [{ isDirty: true, uri: { fsPath: path.join(folder, "CLAUDE.md") } }],
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: false });
  await flush();

  assert.equal(spawnCalls.length, 0);
  assert.deepStrictEqual(guidedResults(panel), [{ type: "guided-result", ok: false, message: GUIDED_DIRTY_MESSAGE }]);
});

test("a dirty AGENTS.md OUTSIDE the selected folder does not block the run", async () => {
  const spawnCalls = [];
  const folder = "/tmp/zcp-guided-ws-clean";
  const { panel } = openWelcome({
    workspaceFolders: [folder],
    textDocuments: () => [{ isDirty: true, uri: { fsPath: "/somewhere/else/AGENTS.md" } }],
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  assert.equal(spawnCalls.length, 1, "a dirty doc elsewhere must not block this folder's run");
});

test("ZCP_AUTHORING truthy in the zembed store blocks the run", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({
    workspaceFolders: ["/tmp/zcp-guided-ws-authoring"],
    readZembedEnv: () => ({ ZCP_AUTHORING: "1" }),
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  assert.equal(spawnCalls.length, 0);
  assert.deepStrictEqual(guidedResults(panel), [{ type: "guided-result", ok: false, message: GUIDED_AUTHORING_MESSAGE }]);
});

test("ZCP_AUTHORING via process.env blocks the run when the zembed store has no opinion", async () => {
  const original = process.env.ZCP_AUTHORING;
  process.env.ZCP_AUTHORING = "1";
  try {
    const spawnCalls = [];
    const { panel } = openWelcome({
      workspaceFolders: ["/tmp/zcp-guided-ws-authoring-env"],
      spawn: fakeSpawn(spawnCalls, "ok"),
    });

    panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
    await flush();

    assert.equal(spawnCalls.length, 0);
    assert.deepStrictEqual(guidedResults(panel), [{ type: "guided-result", ok: false, message: GUIDED_AUTHORING_MESSAGE }]);
  } finally {
    if (original === undefined) delete process.env.ZCP_AUTHORING;
    else process.env.ZCP_AUTHORING = original;
  }
});

test("a zembed store present but silent on ZCP_AUTHORING still falls back to process.env", async () => {
  const original = process.env.ZCP_AUTHORING;
  process.env.ZCP_AUTHORING = "1";
  try {
    const spawnCalls = [];
    const { panel } = openWelcome({
      workspaceFolders: ["/tmp/zcp-guided-ws-authoring-env2"],
      readZembedEnv: () => ({ ZCP_AGENT_TYPES: "claude-code" }), // a real store, but no ZCP_AUTHORING key
      spawn: fakeSpawn(spawnCalls, "ok"),
    });

    panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
    await flush();

    assert.equal(spawnCalls.length, 0);
    assert.deepStrictEqual(guidedResults(panel), [{ type: "guided-result", ok: false, message: GUIDED_AUTHORING_MESSAGE }]);
  } finally {
    if (original === undefined) delete process.env.ZCP_AUTHORING;
    else process.env.ZCP_AUTHORING = original;
  }
});

test("no workspace folder rejects with a short message and spawns nothing", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ workspaceFolders: [], spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  assert.equal(spawnCalls.length, 0);
  const results = guidedResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].ok, false);
  assert.equal(typeof results[0].message, "string");
});

test("multiple workspace folders consult showQuickPick; the chosen folder becomes cwd", async () => {
  const spawnCalls = [];
  const picks = [];
  const folders = ["/tmp/zcp-guided-ws-a", "/tmp/zcp-guided-ws-b"];
  const { panel } = openWelcome({
    workspaceFolders: folders,
    showQuickPick: async (items, options) => { picks.push({ items, options }); return folders[1]; },
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  assert.equal(picks.length, 1, "expected exactly one quickpick prompt");
  assert.deepStrictEqual(picks[0].items, folders);
  assert.equal(spawnCalls.length, 1);
  assert.equal(spawnCalls[0].opts.cwd, folders[1], "the CHOSEN folder must become cwd, not the first one");
});

test("cancelling the multi-root picker releases the lock and spawns nothing", async () => {
  const spawnCalls = [];
  const folders = ["/tmp/zcp-guided-ws-c", "/tmp/zcp-guided-ws-d"];
  let pickCalls = 0;
  const { panel } = openWelcome({
    workspaceFolders: folders,
    showQuickPick: async () => { pickCalls++; return pickCalls === 1 ? undefined : folders[0]; },
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();
  assert.equal(spawnCalls.length, 0, "a cancelled picker must never spawn");

  // Released: a fresh toggle now reaches the picker again (not rejected
  // busy) and this time proceeds to spawn.
  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();
  assert.equal(pickCalls, 2, "the lock release must let a second toggle reach the picker");
  assert.equal(spawnCalls.length, 1, "the second, non-cancelled attempt must spawn");
});

test("a spawn ENOENT reports the binary-not-found message", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({
    workspaceFolders: ["/tmp/zcp-guided-ws-enoent"],
    spawn: fakeSpawn(spawnCalls, "enoent"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  assert.deepStrictEqual(guidedResults(panel), [{ type: "guided-result", ok: false, message: GUIDED_ENOENT_MESSAGE }]);
});

test("exit 0 with a matching marker reports ok and pushes fresh state", async () => {
  const spawnCalls = [];
  const folder = "/tmp/zcp-guided-ws-match";
  const { panel } = openWelcome({
    workspaceFolders: [folder],
    fs: fsMarker(true), // marker present, matching enable:true
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  const results = guidedResults(panel);
  assert.deepStrictEqual(results, [{ type: "guided-result", ok: true, enabled: true }]);
  assert.ok(panel.postedMessages.some((m) => m.type === "state"), "expected a fresh state push after completion");
});

test("exit 0 with the marker matching a DISABLE also reports ok", async () => {
  const spawnCalls = [];
  const folder = "/tmp/zcp-guided-ws-match-off";
  const { panel } = openWelcome({
    workspaceFolders: [folder],
    fs: fsMarker(false), // marker absent, matching enable:false
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: false });
  await flush();

  assert.deepStrictEqual(guidedResults(panel), [{ type: "guided-result", ok: true, enabled: false }]);
});

test("exit 0 with a mismatched marker reports an honest failure, not a false success", async () => {
  const spawnCalls = [];
  const folder = "/tmp/zcp-guided-ws-mismatch";
  const { panel } = openWelcome({
    workspaceFolders: [folder],
    fs: fsMarker(false), // marker absent even though the run asked to enable
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  assert.deepStrictEqual(guidedResults(panel), [{ type: "guided-result", ok: false, message: GUIDED_MARKER_MISMATCH_MESSAGE }]);
});

test("a non-zero exit reports the honest partial-failure copy, never a silent success", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({
    workspaceFolders: ["/tmp/zcp-guided-ws-fail"],
    spawn: fakeSpawn(spawnCalls, "fail"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  assert.deepStrictEqual(guidedResults(panel), [{ type: "guided-result", ok: false, message: GUIDED_PARTIAL_FAILURE_MESSAGE }]);
});

test("a second guided-toggle while one is running replies busy and starts no second spawn", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({
    workspaceFolders: ["/tmp/zcp-guided-ws-busy"],
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  panel.webview.__fireMessage({ type: "guided-toggle", enable: false });

  const busy = guidedResults(panel).filter((m) => m.message === GUIDED_BUSY_MESSAGE);
  assert.equal(busy.length, 1);
  assert.equal(busy[0].ok, false);

  await flush();
  assert.equal(spawnCalls.length, 1, "only the first toggle may spawn");
});

test("an in-flight agent authorization does not block a guided toggle (independent locks)", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({
    workspaceFolders: ["/tmp/zcp-guided-ws-independent"],
    spawn: fakeSpawn(spawnCalls, "ok"),
  });

  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" }); // starts the P3 auth flow's lock
  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();

  assert.equal(spawnCalls.length, 1, "guided must proceed even while an authorization is in flight");
  assert.equal(guidedResults(panel).some((m) => m.message === GUIDED_BUSY_MESSAGE), false);
});

test("a guided toggle in flight does not block an agent authorization (independent locks)", async () => {
  const { panel } = openWelcome({
    workspaceFolders: ["/tmp/zcp-guided-ws-independent2"],
    spawn: fakeSpawn([], "ok"),
  });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });

  const bridgeSends = panel.postedMessages.filter((m) => m.type === "bridge-send");
  assert.equal(bridgeSends.length, 1, "an authorization must proceed even while a guided toggle is in flight");
});

test("open() creates the guided output channel once and registers it in ctx.subscriptions", () => {
  const { stub, ctx } = openWelcome();
  assert.equal(stub.outputChannels.length, 1);
  assert.equal(stub.outputChannels[0].name, "Zerops Welcome");
  assert.ok(ctx.subscriptions.includes(stub.outputChannels[0]));
});

test("revealing an already-open panel does not create a second output channel", () => {
  const { stub, welcome, ctx, deps } = openWelcome();
  welcome.open(ctx, deps);
  assert.equal(stub.outputChannels.length, 1);
});
