"use strict";

// Panel-scoped watchers (docs/spec-welcome-mode.md §3): zembed env file,
// ~/.claude + ~/.codex credential dirs, and the guided marker all funnel
// into one debounced full-state push. Directories that don't exist yet must
// not crash — and must still be caught once they appear (a login flow can
// create ~/.claude minutes after the panel opened). Every watcher is
// panel-scoped: disposing the panel must close every one of them, and
// reopening (reveal) must never accumulate a second set.
//
// These tests bypass extension.js's command handler and call welcome.open()
// directly (via loadWelcome()) so they can inject deps (homeDir,
// workspaceRoot, fs) that extension.js's fixed production call site never
// passes — see the note on loadWelcome() in harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { EventEmitter } = require("node:events");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

function baseDeps(extra) {
  return Object.assign(
    {
      REGISTRY: TEST_REGISTRY,
      ALL_AGENT_IDS: TEST_AGENT_IDS,
      readZembedEnv: () => null,
      runAgentAction: () => {},
      homeDir: "/nonexistent/zcp-welcomejs-home",
      workspaceRoot: null,
    },
    extra
  );
}

function openWelcome(extraDeps) {
  const { stub, extensionDir, welcome } = loadWelcome();
  const ctx = { subscriptions: [], extensionPath: extensionDir };
  const deps = baseDeps(extraDeps);
  welcome.open(ctx, deps);
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  return { stub, panel, welcome, ctx, deps };
}

// makeCountingFs stands in for deps.fs: every watch() call is recorded (so a
// test can assert WHICH paths got watched) and every returned watcher counts
// its own close() calls, so disposal can be proven without touching the real
// filesystem or waiting on real fs.watch timing.
function makeCountingFs({ existsSync } = {}) {
  const watches = [];
  let closes = 0;
  return {
    fsImpl: {
      existsSync: existsSync || (() => false),
      watch: (target) => {
        watches.push(target);
        return { close: () => { closes++; } };
      },
    },
    watches,
    closesCount: () => closes,
  };
}

function waitFor(predicate, { timeoutMs = 3000, intervalMs = 50 } = {}) {
  return new Promise((resolve) => {
    const deadline = Date.now() + timeoutMs;
    const tick = () => {
      const val = predicate();
      if (val) { resolve(val); return; }
      if (Date.now() >= deadline) { resolve(undefined); return; }
      setTimeout(tick, intervalMs);
    };
    tick();
  });
}

test("missing ~/.claude and ~/.codex at open falls back to watching HOME, without crashing", () => {
  const { fsImpl, watches } = makeCountingFs({ existsSync: () => false });
  const homeDir = "/tmp/zcp-welcomejs-missing-home";

  assert.doesNotThrow(() => openWelcome({ fs: fsImpl, homeDir, workspaceRoot: null }));

  const homeWatches = watches.filter((w) => w === homeDir);
  assert.ok(homeWatches.length >= 2, "both the claude-code and codex probes should fall back to watching HOME");
});

// This exercises a REAL fs.watch registration against a REAL tmp HOME, which
// makes it hostage to a genuine macOS FSEvents reliability gap: under heavy
// CONCURRENT watch registration (many welcomejs test files opening panels —
// each with their own watchers — in parallel processes), a freshly
// registered watch occasionally never delivers a single event, verified by
// direct instrumentation (this is not a debounce/timing issue — waiting
// longer doesn't help a watch that FSEvents never armed). A fresh watch
// registration on a fresh path is independent of any previous attempt, so a
// bounded retry — each against its own new tmp dir and panel — reliably
// absorbs this without weakening what the test proves (the watcher path
// genuinely detects a dir created after open and pushes local-only).
test("a credential dir created after open is detected and pushes local-only", async () => {
  const ATTEMPTS = 5;
  let found;
  for (let attempt = 1; attempt <= ATTEMPTS && !found; attempt++) {
    const tmpHome = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-welcomejs-home-"));
    const { panel } = openWelcome({ homeDir: tmpHome, workspaceRoot: null });

    panel.webview.__fireMessage({ type: "ready" });
    assert.equal(panel.postedMessages.filter((m) => m.type === "state").length, 1);

    fs.mkdirSync(path.join(tmpHome, ".claude"));
    fs.writeFileSync(path.join(tmpHome, ".claude", ".credentials.json"), "{}");

    found = await waitFor(() => {
      const msgs = panel.postedMessages.filter((m) => m.type === "state");
      const last = msgs[msgs.length - 1];
      return last && last.payload.agents.some((a) => a.id === "claude-code" && a.state === "local-only");
    });
    panel.dispose();
  }
  assert.ok(found, `expected a state push showing claude-code as local-only after the cred file appeared (${ATTEMPTS} attempts)`);
});

test("disposing the panel closes every watcher it opened", () => {
  const { fsImpl, watches, closesCount } = makeCountingFs();
  const { panel } = openWelcome({ fs: fsImpl, homeDir: "/tmp/zcp-welcomejs-home", workspaceRoot: null });

  assert.ok(watches.length > 0, "expected at least one watcher to be attached");

  panel.dispose();

  assert.equal(closesCount(), watches.length, "every attached watcher must be closed on dispose");
});

test("revealing an already-open panel does not attach additional watchers", () => {
  const { fsImpl, watches } = makeCountingFs();
  const { welcome, ctx, deps } = openWelcome({ fs: fsImpl, homeDir: "/tmp/zcp-welcomejs-home", workspaceRoot: null });

  const countAfterOpen = watches.length;
  assert.ok(countAfterOpen > 0);

  welcome.open(ctx, deps); // re-invoke the command on the existing panel

  assert.equal(watches.length, countAfterOpen, "reveal must not start a second set of watchers");
});

// Finding 5 (MEDIUM): an fs.watch()'d FSWatcher's asynchronous 'error' event
// (EMFILE, ENOSPC) is delivered on 'error', never as a thrown exception — a
// watcher with NO 'error' listener makes Node's EventEmitter throw it
// straight into the extension host on the next tick. Every watcher this file
// creates must attach one, degrading to "close that watcher" instead. Real
// EventEmitter instances (not hand-rolled {close(){}} stubs) are used here
// specifically so a missing listener actually reproduces Node's real
// unhandled-'error'-throws behavior, not just "we forgot to call .on()".
test("an fs.watch 'error' event degrades quietly (closes that watcher) instead of throwing into the host", () => {
  const watchers = []; // every real EventEmitter-based fake watcher created
  const fsImpl = {
    existsSync: () => false,
    watch: () => {
      const emitter = new EventEmitter();
      let closeCount = 0;
      emitter.close = () => { closeCount++; };
      watchers.push({ emitter, closeCount: () => closeCount });
      return emitter;
    },
  };

  const { panel } = openWelcome({ fs: fsImpl, homeDir: "/tmp/zcp-welcomejs-err-home", workspaceRoot: null });
  assert.ok(watchers.length > 0, "expected at least one watcher to be attached");

  assert.doesNotThrow(() => {
    for (const { emitter } of watchers) {
      emitter.emit("error", Object.assign(new Error("EMFILE: too many open files"), { code: "EMFILE" }));
    }
  }, "a watcher's error event must never throw into the extension host");

  for (const w of watchers) {
    assert.ok(w.closeCount() >= 1, "an errored watcher must be closed, not left dangling");
  }

  assert.doesNotThrow(() => panel.webview.__fireMessage({ type: "ready" }));
  assert.ok(panel.postedMessages.some((m) => m.type === "state"), "the panel must keep responding after every watcher has errored out");
});

// Finding 5's second half: the HOME->target credential-dir swap lacked a
// generation guard, so a stale HOME callback — already queued when the
// legitimate HOME event closed HOME and attached the target watcher — could
// fire again afterward, closing the freshly attached target watcher out from
// under itself and re-attaching (and double-pushing state) needlessly.
test("a stale HOME callback (queued before the target watcher attaches) is ignored after the swap", () => {
  const homeDir = "/tmp/zcp-welcomejs-gen-home";
  const claudeTarget = path.join(homeDir, ".claude");
  const calls = []; // { target, cb, watcher }
  let claudeExists = false;
  const fsImpl = {
    existsSync: (p) => p === claudeTarget && claudeExists,
    watch: (target, cb) => {
      const watcher = { closeCount: 0, close() { this.closeCount++; } };
      calls.push({ target, cb, watcher });
      return watcher;
    },
  };

  openWelcome({ fs: fsImpl, homeDir, workspaceRoot: null });

  // Both cred dirs (.claude, .codex) fall back to watching HOME (neither
  // exists yet). startWatchers processes CRED_WATCH_DIR's entries in fixed
  // {claude-code, codex} order, so the FIRST HOME-target call is always
  // claude-code's own watcher instance, independent of codex's parallel one.
  const homeCalls = calls.filter((c) => c.target === homeDir);
  assert.ok(homeCalls.length >= 1, "expected at least one HOME-level watch (claude-code's cred fallback)");
  const claudeHomeCall = homeCalls[0];

  claudeExists = true; // simulate a login creating ~/.claude
  const callsBeforeSwap = calls.length;

  claudeHomeCall.cb(); // first (legitimate) HOME event: swap to the target watcher
  assert.equal(calls.length, callsBeforeSwap + 1, "expected exactly one new watch() call (the target) after the swap");
  assert.equal(claudeHomeCall.watcher.closeCount, 1, "the HOME watcher must be closed once superseded");
  const targetCall = calls[calls.length - 1];
  assert.equal(targetCall.target, claudeTarget);

  claudeHomeCall.cb(); // a SECOND, stale invocation of the SAME (already-superseded) HOME callback

  assert.equal(calls.length, callsBeforeSwap + 1, "a stale HOME event must not attach a second target watcher");
  assert.equal(targetCall.watcher.closeCount, 0, "a stale HOME event must not close the freshly attached target watcher");
});

test("guided marker watcher target follows the .zcp/state -> .zcp -> none fallback chain", () => {
  const workspaceRoot = "/tmp/zcp-welcomejs-ws";
  const stateDir = path.join(workspaceRoot, ".zcp", "state");
  const zcpDir = path.join(workspaceRoot, ".zcp");

  const cases = [
    { name: "prefers .zcp/state when it exists", exists: (p) => p === stateDir, want: stateDir },
    { name: "falls back to .zcp when only it exists", exists: (p) => p === zcpDir, want: zcpDir },
    { name: "attaches no watcher when neither exists", exists: () => false, want: null },
  ];

  for (const c of cases) {
    const { fsImpl, watches } = makeCountingFs({ existsSync: c.exists });
    openWelcome({ fs: fsImpl, homeDir: "/nonexistent/zcp-welcomejs-home", workspaceRoot });
    if (c.want) {
      assert.ok(watches.includes(c.want), `${c.name}: expected a watch() call on ${c.want}, got [${watches}]`);
    } else {
      assert.equal(watches.includes(stateDir) || watches.includes(zcpDir), false, c.name);
    }
  }
});

// Pack-manifests watcher (docs/spec-welcome-mode.md §6): reuses the SAME
// fallback-then-swap mechanism (watchWithFallback) the credential dirs use
// above, not watchGuidedMarker's coarser one-shot chain — an external `zcp
// skills pack-add` run (never through this panel) can create .zcp/state/
// skill-packs from nothing on a brand-new workspace, so the panel must
// tolerate the dir not existing yet and pick up its later creation, exactly
// like a credential dir appearing after a login.
test("pack-manifests watcher target follows the skill-packs-dir -> workspace-root fallback", () => {
  const workspaceRoot = "/tmp/zcp-welcomejs-pack-ws";
  const skillPacksDir = path.join(workspaceRoot, ".zcp", "state", "skill-packs");

  const cases = [
    { name: "watches the skill-packs dir directly when it already exists", exists: (p) => p === skillPacksDir, want: skillPacksDir },
    { name: "falls back to the workspace root when it doesn't exist yet", exists: () => false, want: workspaceRoot },
  ];

  for (const c of cases) {
    const { fsImpl, watches } = makeCountingFs({ existsSync: c.exists });
    openWelcome({ fs: fsImpl, homeDir: "/nonexistent/zcp-welcomejs-home", workspaceRoot });
    assert.ok(watches.includes(c.want), `${c.name}: expected a watch() call on ${c.want}, got [${watches}]`);
  }
});

// Mirrors "a stale HOME callback (queued before the target watcher attaches)
// is ignored after the swap" above — watchWithFallback's generation guard is
// shared code, but this proves the SAME discipline holds through the
// pack-manifests call site (workspaceRoot as the fallback, the skill-packs
// dir as the eventual target) too, not just the credential-dir one.
test("a stale workspace-root callback (queued before the skill-packs watcher attaches) is ignored after the swap", () => {
  const workspaceRoot = "/tmp/zcp-welcomejs-pack-gen-ws";
  const skillPacksTarget = path.join(workspaceRoot, ".zcp", "state", "skill-packs");
  const calls = []; // { target, cb, watcher }
  let skillPacksExists = false;
  const fsImpl = {
    existsSync: (p) => p === skillPacksTarget && skillPacksExists,
    watch: (target, cb) => {
      const watcher = { closeCount: 0, close() { this.closeCount++; } };
      calls.push({ target, cb, watcher });
      return watcher;
    },
  };

  // The watcher callback now feeds schedulePackStatusRefresh (a real,
  // unref'd ~300ms debounce — see the dedicated debounce test below), which
  // would otherwise spawn the REAL child_process after this test's own
  // synchronous assertions finish; a never-firing stub keeps this test fully
  // synchronous and side-effect-free, same as before this file existed.
  openWelcome({ fs: fsImpl, homeDir: "/nonexistent/zcp-welcomejs-home", workspaceRoot, spawn: () => new EventEmitter() });

  const rootCalls = calls.filter((c) => c.target === workspaceRoot);
  assert.ok(rootCalls.length >= 1, "expected a workspace-root-level fallback watch (skill-packs doesn't exist yet)");
  const rootCall = rootCalls[0];

  skillPacksExists = true; // simulate `zcp skills pack-add` creating the tree
  const callsBeforeSwap = calls.length;

  rootCall.cb(); // first (legitimate) root-level event: swap to the target watcher
  assert.equal(calls.length, callsBeforeSwap + 1, "expected exactly one new watch() call (the target) after the swap");
  assert.equal(rootCall.watcher.closeCount, 1, "the root-level watcher must be closed once superseded");
  const targetCall = calls[calls.length - 1];
  assert.equal(targetCall.target, skillPacksTarget);

  rootCall.cb(); // a SECOND, stale invocation of the SAME (already-superseded) root callback

  assert.equal(calls.length, callsBeforeSwap + 1, "a stale root-level event must not attach a second target watcher");
  assert.equal(targetCall.watcher.closeCount, 0, "a stale root-level event must not close the freshly attached target watcher");
});

// Pack-status refresh debounce (docs/spec-welcome-mode.md §4): the
// pack-manifests watcher's event no longer feeds the general schedulePush
// full-state debounce directly — it feeds schedulePackStatusRefresh, its own
// ~300ms debounce that runs `zcp skills pack-status --json` exactly once
// after the LAST event, not once per event. A single `zcp skills pack-add`
// run can touch several files under .zcp/state/skill-packs, each its own fs
// event — a burst of those must collapse into one spawn.
test("a burst of pack-manifest watcher events collapses into exactly one debounced pack-status spawn", async () => {
  const workspaceRoot = "/tmp/zcp-welcomejs-pack-debounce-ws";
  const skillPacksDir = path.join(workspaceRoot, ".zcp", "state", "skill-packs");
  let watchCb;
  const fsImpl = {
    existsSync: (p) => p === skillPacksDir,
    watch: (_target, cb) => { watchCb = cb; return { close() {} }; },
  };
  const spawnCalls = [];
  const spawn = (cmd, args, opts) => {
    spawnCalls.push({ cmd, args, opts });
    const child = new EventEmitter();
    child.stdout = new EventEmitter();
    setImmediate(() => {
      child.stdout.emit("data", JSON.stringify({ version: 1, packs: [] }));
      child.emit("close", 0);
    });
    return child;
  };

  openWelcome({ fs: fsImpl, homeDir: "/nonexistent/zcp-welcomejs-home", workspaceRoot, spawn });
  assert.ok(watchCb, "expected the skill-packs watcher to attach directly (the dir already exists)");

  // Three rapid events, well within the 300ms debounce window.
  watchCb();
  watchCb();
  watchCb();

  assert.equal(spawnCalls.length, 0, "must not spawn before the debounce window elapses");

  const found = await waitFor(() => spawnCalls.length > 0, { timeoutMs: 1500 });
  assert.ok(found, "expected the debounced pack-status spawn to fire");
  assert.equal(spawnCalls.length, 1, "a burst of events must collapse into exactly one spawn, not one per event");
  assert.deepStrictEqual(spawnCalls[0], { cmd: "zcp", args: ["skills", "pack-status", "--json"], opts: { cwd: workspaceRoot, shell: false } });
});

// Real-fs integration proof (mirrors "a credential dir created after open is
// detected and pushes local-only" above, including its bounded-retry
// treatment for the same genuine macOS FSEvents reliability gap under heavy
// concurrent watch registration): a pack manifest written entirely OUTSIDE
// this panel (an external `zcp skills pack-add` run in a terminal) must
// still surface as a state delta, without the user ever having to
// reveal/refocus the panel. UNLIKE before packs moved behind the CLI's own
// pack-status contract (docs/spec-welcome-mode.md §4), collectPacksState no
// longer reads the manifest file itself — only a `zcp skills pack-status
// --json` run does — so the fake spawn here reads the manifest's real
// on-disk presence to stand in for what the real CLI would report, keeping
// the REAL part of this proof scoped to what it always was: does the real
// fs.watch mechanism notice the external write and trigger a refresh.
test("a pack manifest created after open is detected and pushes an installed state delta", async () => {
  const ATTEMPTS = 5;
  let found;
  for (let attempt = 1; attempt <= ATTEMPTS && !found; attempt++) {
    const tmpWs = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-welcomejs-pack-ws-"));
    const manifestPath = path.join(tmpWs, ".zcp", "state", "skill-packs", "superpowers.json");
    const spawn = () => {
      const child = new EventEmitter();
      child.stdout = new EventEmitter();
      setImmediate(() => {
        const installed = fs.existsSync(manifestPath);
        child.stdout.emit("data", JSON.stringify({ version: 1, packs: [{ id: "superpowers", state: installed ? "installed" : "absent", managed: true }] }));
        child.emit("close", 0);
      });
      return child;
    };
    const { panel } = openWelcome({ homeDir: "/nonexistent/zcp-welcomejs-home", workspaceRoot: tmpWs, spawn });

    panel.webview.__fireMessage({ type: "ready" }); // triggers the FIRST pack-status run (spec §4) — manifest doesn't exist yet
    // Wait for that first run to actually settle showing "absent" BEFORE
    // creating the manifest — otherwise a coincidental late resolution of
    // this first run (after the file already exists) could report
    // "installed" on its own, without the watcher's own refresh ever having
    // fired, defeating the point of this test.
    const settledAbsent = await waitFor(() => {
      const msgs = panel.postedMessages.filter((m) => m.type === "state");
      const last = msgs[msgs.length - 1];
      return last && last.payload.packs.some((p) => p.id === "superpowers" && p.state === "absent");
    });
    if (!settledAbsent) { panel.dispose(); continue; }

    const skillPacksDir = path.join(tmpWs, ".zcp", "state", "skill-packs");
    fs.mkdirSync(skillPacksDir, { recursive: true });
    fs.writeFileSync(manifestPath, "{}");

    found = await waitFor(() => {
      const msgs = panel.postedMessages.filter((m) => m.type === "state");
      const last = msgs[msgs.length - 1];
      return last && last.payload.packs.some((p) => p.id === "superpowers" && p.state === "installed");
    });
    panel.dispose();
  }
  assert.ok(found, `expected a state push showing superpowers as installed after the manifest appeared (${ATTEMPTS} attempts)`);
});
