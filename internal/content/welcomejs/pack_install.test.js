"use strict";

// Community skill-pack action (docs/spec-welcome-mode.md §6): the webview's
// {type:"pack-action", id, action:"add"|"remove"} click -> host guards
// (workspace, FRESH workspace trust, the shared guided/pack one-mutating-op
// lock — NO claude-code-runnable check anymore, packs are inert workspace
// files) -> `zcp skills pack-add|pack-remove <id> --json`, fixed argv, no
// shell, cwd = the selected folder -> settles on the child's `close` event
// (streams drained) with the single JSON object it printed as the HONEST
// completion authority (exit 0 AND parsed ok:true = success; anything else
// surfaces the CLI's own code/message when parseable, else a fallback) ->
// always triggers a follow-up `zcp skills pack-status --json` refresh. Every
// pack row's LIVE state (docs/spec-welcome-mode.md §4) now comes from that
// pack-status contract exclusively — never a manifest existsSync probe —
// cached per folder with a monotonic generation guard against a stale run
// ever overwriting a newer one.

const test = require("node:test");
const assert = require("node:assert/strict");
const { EventEmitter } = require("node:events");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

const WS = "/tmp/zcp-welcomejs-pack-ws";
const PACK_ID = "superpowers";

const GUIDED_ENOENT_MESSAGE = "zcp binary not found in PATH.";
const GUIDED_BUSY_MESSAGE = "A guided or skill-pack operation is already running.";
const PACK_OP_FAILED_ADD = "Installing the skill pack failed — see the Zerops Welcome output.";
const PACK_OP_FAILED_REMOVE = "Removing the skill pack failed — see the Zerops Welcome output.";

function openWelcome(extraDeps) {
  const { stub, extensionDir, welcome } = loadWelcome();
  const ctx = { subscriptions: [], extensionPath: extensionDir };
  const deps = Object.assign(
    {
      REGISTRY: TEST_REGISTRY,
      ALL_AGENT_IDS: TEST_AGENT_IDS,
      // NO agent authorized at all, by default, throughout this file (spec
      // §6 revision: skill packs no longer require claude-code, or any
      // agent, to be runnable) — every test below proves packs work from
      // this zero-agent baseline unless it explicitly overrides the gate
      // being tested (workspace/trust/busy).
      readZembedEnv: () => null,
      runAgentAction: () => {},
      homeDir: "/nonexistent/zcp-welcomejs-home",
      workspaceRoot: WS,
      workspaceFolders: [WS],
    },
    extraDeps
  );
  welcome.open(ctx, deps);
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  return { stub, panel, welcome, ctx, deps };
}

function packResults(panel) {
  return panel.postedMessages.filter((m) => m.type === "pack-result");
}

// addCalls narrows a recorded spawnCalls list to just the pack-add/
// pack-remove invocations — every completed pack-action ALSO triggers a
// follow-up `zcp skills pack-status --json` refresh (spec §4), which is a
// SEPARATE, independently-tested spawn; tests that only care about the
// actual add/remove call use this instead of a raw spawnCalls.length.
function addCalls(spawnCalls) {
  return spawnCalls.filter((c) => c.args[1] === "pack-add" || c.args[1] === "pack-remove");
}

function lastState(panel) {
  const msgs = panel.postedMessages.filter((m) => m.type === "state");
  return msgs[msgs.length - 1].payload;
}

// fakeSpawn mirrors a real `zcp skills pack-add|pack-remove --json` run:
// records every call and returns an EventEmitter shaped like Node's real
// ChildProcess (with its own `stdout` stream), firing asynchronously via
// setImmediate — real child_process.spawn() essentially never resolves
// synchronously. "ok" emits a valid {ok:true} JSON on stdout then closes 0;
// "fail" closes 1 with NO parseable stdout (proving the FALLBACK message
// path, not a CLI-reported code/message — see the dedicated tests below for
// that); "enoent" fires the child's own "error" event instead of ever
// closing (a spawn failure, never a CLI run at all).
function fakeSpawn(calls, behavior) {
  return (cmd, args, opts) => {
    calls.push({ cmd, args, opts });
    const child = new EventEmitter();
    child.stdout = new EventEmitter();
    setImmediate(() => {
      if (behavior === "enoent") {
        child.emit("error", Object.assign(new Error("spawn zcp ENOENT"), { code: "ENOENT" }));
      } else if (behavior === "fail") {
        child.emit("close", 1);
      } else {
        child.stdout.emit("data", JSON.stringify({ version: 1, ok: true }));
        child.emit("close", 0);
      }
    });
    return child;
  };
}

// fakePackStatusSpawn stands in for `zcp skills pack-status --json` — used
// by the row-state tests below, which don't care about a pack-add/remove
// call at all.
function fakePackStatusSpawn(packs) {
  return () => {
    const child = new EventEmitter();
    child.stdout = new EventEmitter();
    setImmediate(() => {
      child.stdout.emit("data", JSON.stringify({ version: 1, packs }));
      child.emit("close", 0);
    });
    return child;
  };
}

// flush drains several rounds of the microtask+macrotask queues — mirrors
// guided_flow.test.js's own helper (the pack flow crosses the same kind of
// setImmediate boundaries via the fake spawn's close/error emission).
function flush(rounds = 4) {
  let p = Promise.resolve();
  for (let i = 0; i < rounds; i++) p = p.then(() => new Promise((resolve) => setImmediate(resolve)));
  return p;
}

// ---- allowlist-adjacent gate tests (see message_allowlist.test.js for the
// exhaustive shape/enum coverage; these live here because they're part of
// the same gate story as the rest of this file) --------------------------

test("skill-add is no longer a recognized message type", async () => {
  const { panel } = openWelcome({ spawn: fakeSpawn([], "ok") });

  panel.webview.__fireMessage({ type: "skill-add", slug: PACK_ID });
  await flush();

  assert.equal(panel.postedMessages.filter((m) => m.type === "skill-result" || m.type === "pack-result").length, 0);
});

test("pack-toggle (the retired message type) is dropped as unrecognized — no spawn, no result", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "pack-toggle", id: PACK_ID, enable: true });
  await flush();

  assert.equal(spawnCalls.length, 0);
  assert.equal(packResults(panel).length, 0);
});

test("pack-action with an unknown id is dropped at the allowlist gate — no spawn, no result", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "pack-action", id: "not-a-real-pack", action: "add" });
  await flush();

  assert.equal(spawnCalls.length, 0);
  assert.equal(packResults(panel).length, 0);
});

// gstack (garrytan/gstack) was deliberately excluded from the Go registry
// (internal/skillpacks/registry.go: 56MB application monorepo, wrong
// product shape for a wholesale .claude/skills/ copy) — it must never reach
// handlePackAction, dropped at the allowlist gate exactly like any other
// unrecognized id.
test('pack-action with id "gstack" is dropped at the allowlist gate — no spawn, no result', async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "pack-action", id: "gstack", action: "add" });
  await flush();

  assert.equal(spawnCalls.length, 0);
  assert.equal(packResults(panel).length, 0);
});

// ---- gate: workspace / trust / shared lock — NO claude-code check --------

test("no workspace folder open is rejected with no spawn", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ workspaceRoot: null, workspaceFolders: [], spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.equal(spawnCalls.length, 0);
  const results = packResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].ok, false);
  assert.equal(typeof results[0].message, "string");
});

test("an untrusted workspace is rejected with no spawn", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ isTrusted: () => false, spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.equal(spawnCalls.length, 0);
  const results = packResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].ok, false);
});

// isTrusted is a FUNCTION now, read fresh at every click (spec §6) — never a
// snapshot boolean captured once at panel-open time. A grant/revoke that
// happens WHILE the panel sits open must be observed immediately, in both
// directions.
test("workspace trust is read FRESH at click time, in both directions", async () => {
  const spawnCalls = [];
  let trusted = true;
  const { panel } = openWelcome({ isTrusted: () => trusted, spawn: fakeSpawn(spawnCalls, "ok") });

  trusted = false; // revoked AFTER the panel already opened
  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();
  assert.equal(addCalls(spawnCalls).length, 0, "a revocation after open must still be observed at this click");
  assert.equal(packResults(panel)[0].ok, false);

  trusted = true; // granted back
  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();
  assert.equal(addCalls(spawnCalls).length, 1, "a grant after the earlier rejection must also be observed immediately");
});

// The headline gate change (spec §6 revision): packs work with ZERO
// runnable agents — no claude-code, no agent at all. Every test in this file
// already proves this implicitly (readZembedEnv defaults to () => null in
// openWelcome above), but this test states it explicitly as its own claim.
test("packs work with NO runnable agent at all — the operation proceeds to spawn", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ readZembedEnv: () => null, spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.equal(addCalls(spawnCalls).length, 1, "with zero agents authorized, the pack action must still spawn — packs no longer gate on claude-code");
  assert.deepStrictEqual(packResults(panel), [{ type: "pack-result", id: PACK_ID, ok: true }]);
});

test("a second pack-action while one is running replies busy (coded, no spawn)", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  panel.webview.__fireMessage({ type: "pack-action", id: "andrej-karpathy-skills", action: "add" });

  const busy = packResults(panel).filter((m) => m.code === "busy");
  assert.equal(busy.length, 1);
  assert.equal(busy[0].ok, false);
  assert.equal(busy[0].message, undefined, "the busy rejection carries a CODE, not a bare message — welcome.html owns the copy");

  await flush();
  // The first action's own success also triggers a follow-up pack-status
  // refresh (spec §4) — that's a SEPARATE spawn, not a second pack-action.
  assert.equal(addCalls(spawnCalls).length, 1, "only the first action may spawn");
});

test("a guided toggle in flight blocks a pack-action (shared one-mutating-op lock)", async () => {
  const packSpawnCalls = [];
  let guidedChild;
  const spawn = (cmd, args, opts) => {
    if (args[0] === "init") {
      guidedChild = new EventEmitter(); // never auto-fires — holds the shared lock open
      return guidedChild;
    }
    packSpawnCalls.push({ cmd, args, opts });
    const child = new EventEmitter();
    child.stdout = new EventEmitter();
    setImmediate(() => {
      child.stdout.emit("data", JSON.stringify({ version: 1, ok: true }));
      child.emit("close", 0);
    });
    return child;
  };
  // Guided still requires claude-code — inject it just for THIS gate check.
  const { panel } = openWelcome({ readZembedEnv: () => ({ ZCP_AGENT_TOKEN_CLAUDE_CODE: "test-token" }), spawn });

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true });
  await flush();
  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.equal(packSpawnCalls.length, 0, "the pack action must not spawn while guided holds the shared lock");
  assert.deepStrictEqual(packResults(panel), [{ type: "pack-result", id: PACK_ID, ok: false, code: "busy" }]);
});

// ---- spawn protocol --------------------------------------------------

test("spawn argv/cwd/shell pin — pack-add (add)", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  const calls = addCalls(spawnCalls);
  assert.equal(calls.length, 1);
  assert.deepStrictEqual(calls[0], { cmd: "zcp", args: ["skills", "pack-add", PACK_ID, "--json"], opts: { cwd: WS, shell: false } });
});

test("spawn argv/cwd/shell pin — pack-remove (remove)", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "remove" });
  await flush();

  const calls = addCalls(spawnCalls);
  assert.equal(calls.length, 1);
  assert.deepStrictEqual(calls[0], { cmd: "zcp", args: ["skills", "pack-remove", PACK_ID, "--json"], opts: { cwd: WS, shell: false } });
});

// close (streams drained), not exit — exit can fire before stdout is
// guaranteed flushed, and the captured stdout must be complete before
// parsing it (spec §6).
test("an 'exit' event alone (no 'close') never settles the operation", async () => {
  const { panel } = openWelcome({
    spawn: () => {
      const child = new EventEmitter();
      child.stdout = new EventEmitter();
      setImmediate(() => {
        child.stdout.emit("data", JSON.stringify({ version: 1, ok: true }));
        child.emit("exit", 0); // deliberately NOT "close"
      });
      return child;
    },
  });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.equal(packResults(panel).length, 0, "an exit-only child must never settle the operation — only close does");
});

// Node's own docs don't guarantee "error" and "close" are mutually exclusive
// for every failure mode — settle() must resolve the operation exactly
// once regardless of which fires, or fires twice.
test("both 'error' and 'close' firing for the same child settle the operation exactly once", async () => {
  const { panel } = openWelcome({
    spawn: () => {
      const child = new EventEmitter();
      child.stdout = new EventEmitter();
      setImmediate(() => {
        child.emit("error", Object.assign(new Error("boom"), { code: "EPIPE" }));
        child.emit("close", 0); // fires AFTER error — must be a no-op
      });
      return child;
    },
  });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.equal(packResults(panel).length, 1, "error+close for the same run must settle exactly once, not twice");
});

test("an unterminated final output line is flushed to the output channel once the child settles", async () => {
  const { stub, panel } = openWelcome({
    spawn: () => {
      const child = new EventEmitter();
      child.stdout = new EventEmitter();
      setImmediate(() => {
        child.stdout.emit("data", "line one\nline two, no trailing newline");
        child.emit("close", 1); // non-JSON stdout -> fallback failure; irrelevant to what's proven here
      });
      return child;
    },
  });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  const channel = stub.outputChannels[0];
  assert.ok(channel.lines.includes("line one"), "expected the newline-terminated line to have streamed normally");
  assert.ok(
    channel.lines.includes("line two, no trailing newline"),
    "expected the trailing partial line to be flushed once the child settled, not silently dropped"
  );
});

// ---- JSON stdout parsing (spec §6: "bounded stdout capture") -------------

test("exit 0 with a parsed ok:true JSON reports success", async () => {
  const spawnCalls = [];
  const { panel } = openWelcome({ spawn: fakeSpawn(spawnCalls, "ok") });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.deepStrictEqual(packResults(panel), [{ type: "pack-result", id: PACK_ID, ok: true }]);
});

test("garbage (non-JSON) stdout with exit 0 is NOT treated as success", async () => {
  const { panel } = openWelcome({
    spawn: () => {
      const child = new EventEmitter();
      child.stdout = new EventEmitter();
      setImmediate(() => {
        child.stdout.emit("data", "not json at all {{{");
        child.emit("close", 0);
      });
      return child;
    },
  });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.deepStrictEqual(packResults(panel), [{ type: "pack-result", id: PACK_ID, ok: false, message: PACK_OP_FAILED_ADD }]);
});

test("stdout beyond the 64KiB capture cap is truncated, parses as invalid JSON, and reports the fallback failure — never a false success", async () => {
  const { panel } = openWelcome({
    spawn: () => {
      const child = new EventEmitter();
      child.stdout = new EventEmitter();
      setImmediate(() => {
        // A well-formed JSON object padded well past the 64KiB cap: the
        // captured (truncated) prefix is no longer valid JSON once its
        // closing brace is cut off.
        const oversized = JSON.stringify({ version: 1, ok: true, message: "x".repeat(100 * 1024) });
        child.stdout.emit("data", oversized);
        child.emit("close", 0);
      });
      return child;
    },
  });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  const results = packResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].ok, false, "a truncated/unparseable capture must never report a false success");
});

test("a non-zero exit with parseable JSON surfaces the CLI's own code/message", async () => {
  const { panel } = openWelcome({
    spawn: () => {
      const child = new EventEmitter();
      child.stdout = new EventEmitter();
      setImmediate(() => {
        child.stdout.emit(
          "data",
          JSON.stringify({ version: 1, ok: false, code: "collision", message: "skill 'red-green' already provided by another installed pack" })
        );
        child.emit("close", 1);
      });
      return child;
    },
  });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.deepStrictEqual(packResults(panel), [
    { type: "pack-result", id: PACK_ID, ok: false, message: "skill 'red-green' already provided by another installed pack", code: "collision" },
  ]);
});

test("a non-zero exit with no parseable stdout falls back to the direction-aware failure message", async () => {
  const { panel } = openWelcome({ spawn: fakeSpawn([], "fail") });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "remove" });
  await flush();

  assert.deepStrictEqual(packResults(panel), [{ type: "pack-result", id: PACK_ID, ok: false, message: PACK_OP_FAILED_REMOVE }]);
});

test("a success carrying CLI warnings threads them through to pack-result", async () => {
  const { panel } = openWelcome({
    spawn: () => {
      const child = new EventEmitter();
      child.stdout = new EventEmitter();
      setImmediate(() => {
        child.stdout.emit("data", JSON.stringify({ version: 1, ok: true, warnings: ["some-skill.md kept local edits"] }));
        child.emit("close", 0);
      });
      return child;
    },
  });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.deepStrictEqual(packResults(panel), [{ type: "pack-result", id: PACK_ID, ok: true, warnings: ["some-skill.md kept local edits"] }]);
});

test("a success with an empty warnings array carries no warnings field", async () => {
  const { panel } = openWelcome({
    spawn: () => {
      const child = new EventEmitter();
      child.stdout = new EventEmitter();
      setImmediate(() => {
        child.stdout.emit("data", JSON.stringify({ version: 1, ok: true, warnings: [] }));
        child.emit("close", 0);
      });
      return child;
    },
  });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.deepStrictEqual(packResults(panel), [{ type: "pack-result", id: PACK_ID, ok: true }]);
});

test("a spawn ENOENT reports the binary-not-found message", async () => {
  const { panel } = openWelcome({ spawn: fakeSpawn([], "enoent") });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  assert.deepStrictEqual(packResults(panel), [{ type: "pack-result", id: PACK_ID, ok: false, message: GUIDED_ENOENT_MESSAGE }]);
});

// Finding-3-class robustness (mirrors guided_flow.test.js's own equivalent
// test): an unexpected synchronous throw anywhere past lock acquisition must
// still release packFlow and report an error — handleMessage invokes
// handlePackAction without awaiting it, so an uncaught throw would otherwise
// become an unhandled rejection AND leave the lock permanently held.
test("an unexpected synchronous throw mid-flow still releases the pack lock and reports an error, with no unhandled rejection", async () => {
  const spawnCalls = [];
  // The pack-add call itself throws (an arbitrary throw site past the point
  // packFlow is already held); the follow-up pack-status refresh this test
  // doesn't care about is left alone so it can't obscure what's being proven.
  const throwingSpawn = (cmd, args, opts) => {
    spawnCalls.push({ cmd, args, opts });
    if (args[1] === "pack-status") return fakeSpawn([], "ok")(cmd, args, opts);
    const child = new EventEmitter();
    Object.defineProperty(child, "stdout", {
      get() { throw new Error("boom: unexpected stdout access failure"); },
    });
    return child;
  };
  const { panel } = openWelcome({ spawn: throwingSpawn });

  const unhandled = [];
  const onUnhandled = (err) => unhandled.push(err);
  process.on("unhandledRejection", onUnhandled);
  try {
    panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
    await flush();
  } finally {
    process.off("unhandledRejection", onUnhandled);
  }
  assert.deepStrictEqual(unhandled, [], "handlePackAction must never leak an unhandled promise rejection");

  const results = packResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].ok, false);

  // The lock must be released: a second action now reaches spawn again
  // rather than being rejected busy.
  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();
  assert.equal(addCalls(spawnCalls).length, 2, "the lock must be released after the unexpected throw, allowing a second spawn");
});

// ---- status refresh after an operation (spec §4 trigger) -----------------

test("a successful pack-add triggers a follow-up pack-status run for the SAME folder", async () => {
  const calls = [];
  const spawn = (cmd, args, opts) => {
    calls.push({ cmd, args, opts });
    const child = new EventEmitter();
    child.stdout = new EventEmitter();
    setImmediate(() => {
      if (args[1] === "pack-status") {
        child.stdout.emit("data", JSON.stringify({ version: 1, packs: [{ id: PACK_ID, state: "installed", managed: true }] }));
      } else {
        child.stdout.emit("data", JSON.stringify({ version: 1, ok: true }));
      }
      child.emit("close", 0);
    });
    return child;
  };
  const { panel } = openWelcome({ spawn });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  const statusCalls = calls.filter((c) => c.args[1] === "pack-status");
  assert.equal(statusCalls.length, 1, "expected exactly one follow-up pack-status run after the operation completed");
  assert.deepStrictEqual(statusCalls[0], { cmd: "zcp", args: ["skills", "pack-status", "--json"], opts: { cwd: WS, shell: false } });

  assert.equal(lastState(panel).packs.find((p) => p.id === PACK_ID).state, "installed");
});

test("a FAILED pack-add still triggers a follow-up pack-status refresh", async () => {
  const calls = [];
  const spawn = (cmd, args, opts) => {
    calls.push({ cmd, args, opts });
    const child = new EventEmitter();
    child.stdout = new EventEmitter();
    setImmediate(() => {
      if (args[1] === "pack-status") {
        child.stdout.emit("data", JSON.stringify({ version: 1, packs: [] }));
        child.emit("close", 0);
      } else {
        child.emit("close", 1); // the pack-add itself failed, no parseable stdout
      }
    });
    return child;
  };
  const { panel } = openWelcome({ spawn });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  const statusCalls = calls.filter((c) => c.args[1] === "pack-status");
  assert.equal(statusCalls.length, 1, "a status refresh must fire even after a failed operation");
});

// ---- row state (spec §3/§4): pack-status is the SOLE authority -----------

test("every pack renders checking before any pack-status result has landed", async () => {
  const { panel } = openWelcome({
    spawn: () => {
      const child = new EventEmitter();
      child.stdout = new EventEmitter();
      return child; // never settles
    },
  });

  panel.webview.__fireMessage({ type: "ready" });

  const payload = lastState(panel);
  assert.equal(payload.packs.length, 4, "exactly the four registered packs — gstack excluded");
  for (const p of payload.packs) assert.equal(p.state, "checking", `expected ${p.id} to render checking before any pack-status result`);
});

test("state reports packs[] reflecting the pack-status result per id", async () => {
  const { panel } = openWelcome({
    spawn: fakePackStatusSpawn([
      { id: "matt-pocock-skills", state: "installed", managed: true },
      { id: "anthropic-skills", state: "broken", managed: true },
    ]),
  });

  panel.webview.__fireMessage({ type: "ready" });
  await flush();

  const payload = lastState(panel);
  assert.equal(payload.packs.length, 4);
  const byId = Object.fromEntries(payload.packs.map((p) => [p.id, p.state]));
  assert.equal(byId["matt-pocock-skills"], "installed");
  assert.equal(byId["superpowers"], "checking", "absent from the CLI's own response -> still checking, not a fabricated absent");
  assert.equal(byId["andrej-karpathy-skills"], "checking");
  assert.equal(byId["anthropic-skills"], "broken");
});

test("state reports an empty packs list when no workspace folder is open", () => {
  const { panel } = openWelcome({ workspaceRoot: null });

  panel.webview.__fireMessage({ type: "ready" });

  assert.deepStrictEqual(lastState(panel).packs, []);
});

// A stale (superseded) pack-status run must never overwrite a NEWER run's
// result (spec §4: "a monotonically increasing request generation").
test("a stale pack-status result never overwrites a newer one", async () => {
  const children = [];
  const spawn = () => {
    const child = new EventEmitter();
    child.stdout = new EventEmitter();
    children.push(child); // never auto-fires — this test drives completion order explicitly
    return child;
  };
  const { panel } = openWelcome({ spawn });

  panel.webview.__fireMessage({ type: "ready" }); // pack-status run #1 (generation 1)
  await flush();
  panel.webview.__fireMessage({ type: "ready" }); // pack-status run #2 (generation 2) — supersedes #1
  await flush();

  assert.equal(children.length, 2, "expected two pack-status spawns, one per ready");

  // The NEWER run (#2) settles FIRST, reporting the pack installed.
  children[1].stdout.emit("data", JSON.stringify({ version: 1, packs: [{ id: PACK_ID, state: "installed", managed: true }] }));
  children[1].emit("close", 0);
  await flush();
  assert.equal(lastState(panel).packs.find((p) => p.id === PACK_ID).state, "installed");

  // The OLDER, now-stale run (#1) settles LATE, reporting the pack absent —
  // this must be dropped, not allowed to clobber the newer, correct result.
  children[0].stdout.emit("data", JSON.stringify({ version: 1, packs: [{ id: PACK_ID, state: "absent", managed: false }] }));
  children[0].emit("close", 0);
  await flush();
  assert.equal(
    lastState(panel).packs.find((p) => p.id === PACK_ID).state,
    "installed",
    "a stale run's late result must not overwrite the newer one"
  );
});

// ---- multi-root (Codex review precedent from the retired pack-toggle
// suite): a multi-root workspace must target the folder the user actually
// picked, never silently folder zero, and read pack-status back from that
// SAME folder. -------------------------------------------------------------

test("multi-root: pack-action spawns in the folder actually picked, and a follow-up status run reads that folder", async () => {
  const spawnCalls = [];
  const folderA = "/tmp/zcp-pack-ws-multiroot-a";
  const folderB = "/tmp/zcp-pack-ws-multiroot-b";
  const spawn = (cmd, args, opts) => {
    spawnCalls.push({ cmd, args, opts });
    const child = new EventEmitter();
    child.stdout = new EventEmitter();
    setImmediate(() => {
      if (args[1] === "pack-status") {
        child.stdout.emit("data", JSON.stringify({ version: 1, packs: [{ id: PACK_ID, state: "installed", managed: true }] }));
      } else {
        child.stdout.emit("data", JSON.stringify({ version: 1, ok: true }));
      }
      child.emit("close", 0);
    });
    return child;
  };
  const { panel } = openWelcome({
    workspaceRoot: folderA, // the fixed "first folder" default — must NOT be used
    workspaceFolders: [folderA, folderB],
    showQuickPick: async () => folderB, // the user picks B in the quickpick
    spawn,
  });

  panel.webview.__fireMessage({ type: "pack-action", id: PACK_ID, action: "add" });
  await flush();

  const addCall = spawnCalls.find((c) => c.args[1] === "pack-add");
  assert.ok(addCall, "expected a pack-add spawn");
  assert.equal(addCall.opts.cwd, folderB, "expected cwd to be the CHOSEN folder, not folder zero");

  const statusCall = spawnCalls.find((c) => c.args[1] === "pack-status");
  assert.ok(statusCall, "expected a follow-up pack-status spawn");
  assert.equal(statusCall.opts.cwd, folderB, "expected the follow-up status run to read B's folder, not A's");

  assert.equal(lastState(panel).packs.find((p) => p.id === PACK_ID).state, "installed", "expected packs[] to reflect B's pack-status result");
});
