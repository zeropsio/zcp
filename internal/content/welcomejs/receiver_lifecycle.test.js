"use strict";

// Receiver lifecycle (docs/spec-welcome-mode.md §1.3, invariant W13): under
// agent-first mode, embedded (per the ancestor-chain classification below),
// the singleton surface boots on every window init, unfocused, announces
// immediately, and sits in `awaiting-mode` (dark) until a valid directive or
// the 10s no-directive window. `set-mode "standard"` (or expiry) applies
// container rules (empty workbench -> reveal panel; restored editors ->
// self-close); the surface never self-closes mid-intent; a manual
// `zerops.panel` invocation is exempt from the whole lifecycle for good.
//
// These tests drive welcome.open() directly (loadWelcome(), like
// handshake.test.js/watchers.test.js), passing the NEW 3rd `opts` argument
// extension.js's boot-always call site uses ({manual:false,
// hadRestoredEditors}) — every OTHER call site in this suite omits opts
// entirely and keeps the historical manual/exempt behavior unchanged (see
// welcome.js's open()).

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("fs");
const path = require("path");
const { loadWelcome, TEMPLATES_DIR, TEST_REGISTRY, TEST_AGENT_IDS, makeFakeTimers } = require("./harness.js");

const AWAITING_MODE_TIMEOUT_MS = 10_000;
const BRIDGE_CHANNEL = "@zerops/zcp-agent-auth-bridge";
const ALLOWLISTED_ORIGIN = "https://app.zerops.io";

function openReceiver(extraDeps, opts) {
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
  welcome.open(ctx, deps, opts);
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  return { stub, panel, welcome, ctx, deps };
}

function fireReady(panel, embedded) {
  panel.webview.__fireMessage({ type: "ready", embedded });
}

function fireSetMode(panel, eventId, mode) {
  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "set-mode", eventId, mode, createdAt: Date.now() },
  });
}

function fireLaunch(panel, eventId, agentId) {
  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "launch-agent", eventId, agentId, createdAt: Date.now() },
  });
}

function revealMessages(panel) {
  return panel.postedMessages.filter((m) => m.type === "reveal");
}

function bridgeSendMessages(panel) {
  return panel.postedMessages.filter((m) => m.type === "bridge-send");
}

function readWelcomeHtmlSource() {
  return fs.readFileSync(path.join(TEMPLATES_DIR, "vscode-bootstrap-welcome.html"), "utf8");
}

// ---- embed classification (§1.3) — live-measured fixtures, verbatim ------

test("welcome.html's embed-classification predicate is the exact pinned expression", () => {
  const src = readWelcomeHtmlSource();
  assert.match(
    src,
    /Array\.from\(location\.ancestorOrigins\)\.some\(\(o\)\s*=>\s*o\s*!==\s*self\.origin\)/,
    "the §1.3 predicate must be the live-proven, pinned expression"
  );
});

test("the embed-classification predicate matches the three live-measured ancestor chains verbatim", () => {
  // No DOM in this harness (see welcome_panel.test.js's own note on why
  // welcome.html's inline script is never executed by node:test) — the
  // predicate is copied here verbatim (pinned against welcome.html's actual
  // source by the test above) and evaluated against the exact fixtures
  // measured live during /flow. Independent oracle: expected classifications
  // come from the spec's own measured description, not from re-deriving the
  // implementation's own logic.
  const predicate = (ancestorOrigins, selfOrigin) => Array.from(ancestorOrigins).some((o) => o !== selfOrigin);
  const SELF_ORIGIN = "cs";
  assert.equal(predicate(["cs", "cs"], SELF_ORIGIN), false, "standalone code-server: both chain entries are the webview's own origin");
  assert.equal(predicate(["cs", "cs", "http://localhost:50153"], SELF_ORIGIN), true, "custom-GUI embed: a foreign ancestor origin present");
  assert.equal(predicate(["cs", "cs", "https://app.zerops.io"], SELF_ORIGIN), true, "app.zerops.io Embedded Editor: a foreign ancestor origin present");
});

// ---- manual invocation reveals content (§1.4) -----------------------------
// Regression (live battery 2026-07-29): a manual `zerops.panel` invocation on
// an ALREADY-EXISTING dark awaiting-mode receiver revealed the tab and
// re-posted state, but never told the webview to stop rendering dark — so the
// panel a user explicitly asked for stayed blank until the 10s window expired.
// §1.4 is explicit: "A manual invocation is explicit user intent: it opens the
// panel focused, RENDERING CONTENT, and exempts the surface from the §1.3
// self-close rule."

test("a manual open of an existing dark awaiting-mode receiver reveals its content (§1.4)", () => {
  const timers = makeFakeTimers();
  const { panel, welcome, ctx, deps } = openReceiver(
    { setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout },
    { manual: false, hadRestoredEditors: false }
  );
  fireReady(panel, true); // embedded -> awaiting-mode, dark, timer armed
  assert.equal(revealMessages(panel).length, 0, "boot-always embedded starts dark");

  welcome.open(ctx, deps); // manual invocation (no opts) — user asked for it

  assert.equal(revealMessages(panel).length, 1, "a manual invocation must reveal content, not leave the surface dark");
});

test("a manual open cancels the awaiting-mode timer — its expiry can never re-decide presentation", () => {
  const timers = makeFakeTimers();
  const { panel, welcome, ctx, deps } = openReceiver(
    { setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout },
    { manual: false, hadRestoredEditors: true } // restored editors: expiry would self-close
  );
  fireReady(panel, true);
  const awaitingTimerId = timers.calls.find((c) => c.ms === AWAITING_MODE_TIMEOUT_MS).id;

  welcome.open(ctx, deps); // manual -> exempt

  assert.equal(timers.fire(awaitingTimerId), false, "the awaiting-mode timer must be cancelled by a manual open, never left to fire");
  assert.equal(panel.disposed, false);
});

// ---- boot-always, unfocused, announces immediately ------------------------

test("a boot-always open (manual:false) creates the receiver panel UNFOCUSED", () => {
  const { panel } = openReceiver({}, { manual: false, hadRestoredEditors: false });
  assert.equal(panel.preserveFocus, true, "boot-always must not steal focus from restored editors (§1.3)");
});

test("a manual open (no opts) creates the receiver panel focused — unchanged historical behavior", () => {
  const { panel } = openReceiver({});
  assert.equal(panel.preserveFocus, false);
});

test("the receiver announces embed-ready immediately once ready fires, boot-always or manual alike", () => {
  const { panel } = openReceiver({}, { manual: false, hadRestoredEditors: false });
  fireReady(panel, true);
  assert.equal(bridgeSendMessages(panel).filter((m) => m.payload.type === "embed-ready").length, 1);
});

// ---- awaiting-mode: no self-close before a directive or the 10s window ----

test("awaiting-mode (embedded, boot-always) does not self-close before a directive, regardless of env-derived state", () => {
  const { panel } = openReceiver({ readZembedEnv: () => ({ ZCP_AGENTS: "claude-code" }) }, { manual: false, hadRestoredEditors: true });
  fireReady(panel, true);
  assert.equal(panel.disposed, false, "awaiting-mode must not self-close before a directive arrives");
  assert.equal(revealMessages(panel).length, 0, "awaiting-mode must not reveal either, before a directive or expiry");
});

test("the 10s no-directive window expiring applies container rules — restored editors -> self-close", () => {
  const timers = makeFakeTimers();
  const { panel } = openReceiver({ setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout }, { manual: false, hadRestoredEditors: true });
  fireReady(panel, true);

  const armed = timers.calls.find((c) => c.ms === AWAITING_MODE_TIMEOUT_MS);
  assert.ok(armed, "a 10s awaiting-mode timer must be armed on an embedded, non-manual receiver");
  assert.equal(panel.disposed, false, "must not self-close before expiry");

  timers.fire(armed.id);
  assert.equal(panel.disposed, true, "expiry with restored editors must self-close (never keep a Zerops tab over a resume)");
});

test("the 10s no-directive window expiring, empty workbench -> reveals the panel instead of closing", () => {
  const timers = makeFakeTimers();
  const { panel } = openReceiver({ setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout }, { manual: false, hadRestoredEditors: false });
  fireReady(panel, true);

  const armed = timers.calls.find((c) => c.ms === AWAITING_MODE_TIMEOUT_MS);
  timers.fire(armed.id);

  assert.equal(panel.disposed, false);
  assert.equal(revealMessages(panel).length, 1, "an empty workbench must reveal the panel's content");
});

// ---- set-mode directives ----------------------------------------------

test('set-mode "onboarding" keeps the receiver dark (no reveal, no close) and cancels the awaiting timer', () => {
  const timers = makeFakeTimers();
  const { panel } = openReceiver({ setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout }, { manual: false, hadRestoredEditors: false });
  fireReady(panel, true);
  assert.equal(timers.pendingCount(), 1, "the awaiting timer must be armed after ready");

  fireSetMode(panel, "onboarding-mode-0-4000-8000-000000000000", "onboarding");

  assert.equal(revealMessages(panel).length, 0, "onboarding stays dark — no reveal message is ever posted");
  assert.equal(panel.disposed, false);
  assert.equal(timers.pendingCount(), 0, "a valid directive must cancel the awaiting-mode timer");
});

test('set-mode "standard" with restored editors self-closes the receiver', () => {
  const { panel } = openReceiver({}, { manual: false, hadRestoredEditors: true });
  fireReady(panel, true);

  fireSetMode(panel, "std-close-0000-4000-8000-000000000000", "standard");

  assert.equal(panel.disposed, true);
});

test('set-mode "standard" with an empty workbench reveals the panel', () => {
  const { panel } = openReceiver({}, { manual: false, hadRestoredEditors: false });
  fireReady(panel, true);

  fireSetMode(panel, "std-reveal-000-4000-8000-000000000000", "standard");

  assert.equal(panel.disposed, false);
  assert.equal(revealMessages(panel).length, 1);
});

// ---- never self-close mid-intent -------------------------------------

test("the receiver never self-closes while a launch intent is in flight, even under a concurrent set-mode standard + restored editors", () => {
  let panelRef;
  let disposedWhileInFlight;
  const { panel } = openReceiver(
    {
      runAgentAction: () => {
        // While THIS launch is still "in-flight" (finishLaunch has not run
        // yet), a set-mode "standard" arrives — container rules must skip
        // closing: a launch intent's outcome relay depends on this very
        // webview staying alive (§1.3/§5.3).
        fireSetMode(panelRef, "mid-intent-set-0-4000-8000-00000000", "standard");
        disposedWhileInFlight = panelRef.disposed;
      },
    },
    { manual: false, hadRestoredEditors: true }
  );
  panelRef = panel;
  fireReady(panel, true);

  fireLaunch(panel, "mid-intent-000-4000-8000-000000000000", "claude-code");

  assert.equal(disposedWhileInFlight, false, "must never self-close while this launch intent is in flight");
});

// ---- manual zerops.panel is exempt for good ----------------------------

test("a manual zerops.panel invocation is exempt from the whole receiver lifecycle — never self-closes even under set-mode standard + restored editors", () => {
  const { panel } = openReceiver({}, { hadRestoredEditors: true }); // no manual:false -> manual invocation (§1.4)
  fireReady(panel, true);

  fireSetMode(panel, "manual-exempt-0-4000-8000-000000000000", "standard");

  assert.equal(panel.disposed, false, "a manual open must never be torn down by the receiver lifecycle");
});

// ---- hadRestoredEditors captured before the receiver exists ------------

test("extension.js hoists the hadRestoredEditors read above the zerops.panel boot-always call — the receiver tab itself must never count toward it", () => {
  const src = fs.readFileSync(path.join(TEMPLATES_DIR, "vscode-bootstrap-extension.js"), "utf8");
  const activateBody = src.slice(src.indexOf("async function activate("));
  assert.ok(activateBody.length > 0, "activate() must exist");
  const tabGroupsIdx = activateBody.indexOf("tabGroups.all.some");
  const executeIdx = activateBody.indexOf('executeCommand("zerops.panel"');
  assert.ok(tabGroupsIdx > -1, "activate() must read tabGroups.all before booting the receiver");
  assert.ok(executeIdx > -1, "activate() must boot the receiver via zerops.panel");
  assert.ok(tabGroupsIdx < executeIdx, "hadRestoredEditors must be captured BEFORE the receiver panel is created");
});
