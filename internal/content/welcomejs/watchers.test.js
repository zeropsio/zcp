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
