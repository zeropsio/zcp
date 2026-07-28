"use strict";

// Embed command channel (docs/spec-welcome-mode.md §4.3, invariants W11/W12):
// announce (embed-ready) -> command (set-mode/launch-agent) -> outcome
// (agent-ready/launch-failed) -> relay-forwarded receipt. All protocol logic
// lives host-side (welcome.js); these tests fire messages directly at the
// panel exactly as the webview's dumb-pipe relay would deliver them, and read
// the host's outbound instructions off panel.postedMessages exactly as the
// webview would receive them. See launch_gate.test.js for the launch-gate
// (W10) coverage specifically, and message_allowlist.test.js for the
// gate-level "no live auth flow required" pin.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("fs");
const path = require("path");
const { loadWelcome, TEMPLATES_DIR, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

const BRIDGE_CHANNEL = "@zerops/zcp-agent-auth-bridge";
const ALLOWLISTED_ORIGIN = "https://app.zerops.io";

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
    codex: { ...TEST_REGISTRY.codex, opens: [{ mode: "terminal", command: "codex --dangerously-bypass-approvals-and-sandbox" }] },
  };
}

function openWelcome(extraDeps) {
  const { stub, extensionDir, welcome } = loadWelcome();
  const ctx = { subscriptions: [], extensionPath: extensionDir };
  const deps = Object.assign(
    {
      REGISTRY: registryWithCommands(),
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

function bridgeSendMessages(panel) {
  return panel.postedMessages.filter((m) => m.type === "bridge-send");
}

function bridgeOutcomeMessages(panel) {
  return panel.postedMessages.filter((m) => m.type === "bridge-outcome");
}

function fireReady(panel, embedded) {
  panel.webview.__fireMessage({ type: "ready", embedded });
}

function fireLaunch(panel, eventId, agentId, origin) {
  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: origin || ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "launch-agent", eventId, agentId },
  });
}

function fireSetMode(panel, eventId, mode, origin) {
  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: origin || ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "set-mode", eventId, mode },
  });
}

function fireRelayForwarded(panel, eventId) {
  panel.webview.__fireMessage({ type: "relay-forwarded", eventId });
}

// ---- embed-ready announce -------------------------------------------------

test("embed-ready is emitted ONLY from the ready handler (nothing before it), carrying ordered agents + bootstrapVersion, no installed axis", () => {
  const { panel } = openWelcome({
    resolveAvailableAgentIds: () => ["codex", "claude-code"],
    readZembedEnv: () => ({ ZCP_AGENT_TOKEN_CODEX: "true" }),
  });

  assert.equal(bridgeSendMessages(panel).filter((m) => m.payload.type === "embed-ready").length, 0, "no announce before ready");

  fireReady(panel, true);

  const announces = bridgeSendMessages(panel).filter((m) => m.payload.type === "embed-ready");
  assert.equal(announces.length, 1);
  const payload = announces[0].payload;
  assert.equal(payload.channel, BRIDGE_CHANNEL);
  assert.equal(payload.version, 1);
  assert.match(payload.eventId, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
  assert.equal(typeof payload.createdAt, "number");
  assert.deepStrictEqual(payload.agents, [
    { id: "codex", authorized: true },
    { id: "claude-code", authorized: false },
  ]);
  assert.equal("installed" in payload, false, "embed-ready must never carry the installed axis");
  assert.equal(typeof payload.bootstrapVersion, "string");
});

test("a re-invocation (ready fired again) re-announces — every embed init gets its own embed-ready", () => {
  const { panel } = openWelcome();
  fireReady(panel, true);
  fireReady(panel, true);
  assert.equal(bridgeSendMessages(panel).filter((m) => m.payload.type === "embed-ready").length, 2);
});

// ---- set-mode --------------------------------------------------------------

test("set-mode is dispatched to the onSetMode collaborator with NO live auth flow required", () => {
  const modes = [];
  const { panel } = openWelcome({ onSetMode: (mode) => modes.push(mode) });

  fireSetMode(panel, "set-mode-0000-4000-8000-000000000001", "onboarding");
  fireSetMode(panel, "set-mode-0000-4000-8000-000000000002", "standard");

  assert.deepStrictEqual(modes, ["onboarding", "standard"]);
});

test("set-mode with an invalid mode enum is dropped", () => {
  const modes = [];
  const { panel } = openWelcome({ onSetMode: (mode) => modes.push(mode) });

  fireSetMode(panel, "set-mode-bad0-4000-8000-000000000000", "sideways");

  assert.deepStrictEqual(modes, []);
});

test("set-mode from a non-allowlisted origin is dropped", () => {
  const modes = [];
  const { panel } = openWelcome({ onSetMode: (mode) => modes.push(mode) });

  fireSetMode(panel, "set-mode-evil-4000-8000-000000000000", "onboarding", "https://evil.example.com");

  assert.deepStrictEqual(modes, []);
});

// ---- one outcome per eventId (W11) -----------------------------------------

test("a duplicate launch-agent delivered AFTER completion re-acks the SAME outcome (fresh createdAt), never a second launch", () => {
  const calls = [];
  const { panel } = openWelcome({ runAgentAction: (agent, mode) => calls.push({ agent, mode }) });
  const eventId = "dup-after-0000-4000-8000-000000000000";

  fireLaunch(panel, eventId, "codex");
  fireLaunch(panel, eventId, "codex");

  assert.equal(calls.length, 1, "a duplicate after completion must never start a second launch");
  const outcomes = bridgeOutcomeMessages(panel).filter((m) => m.payload.eventId === eventId);
  assert.equal(outcomes.length, 2, "the completed outcome is idempotently re-acked");
  for (const m of outcomes) {
    assert.equal(m.payload.type, "agent-ready");
    assert.equal(m.payload.agentId, "codex");
    assert.equal(typeof m.payload.createdAt, "number", "every emission — including the re-ack — carries a fresh createdAt");
  }
});

test("a reentrant duplicate delivered MID-EXECUTION (in-flight) coalesces to the one execution's outcome — never a second launch, no separate reply", () => {
  const calls = [];
  let panelRef;
  const eventId = "dup-inflight-4000-8000-000000000000";
  const { panel } = openWelcome({
    runAgentAction: (agent, mode) => {
      calls.push({ agent, mode });
      // Simulate a duplicate arriving WHILE this launch is still executing —
      // the in-flight entry must already be recorded (before this first side
      // effect), so this coalesces silently instead of racing a second launch.
      fireLaunch(panelRef, eventId, "codex");
    },
  });
  panelRef = panel;

  fireLaunch(panel, eventId, "codex");

  assert.equal(calls.length, 1, "in-flight recorded before the first side effect: a mid-execution duplicate must never start a second launch");
  const outcomes = bridgeOutcomeMessages(panel).filter((m) => m.payload.eventId === eventId);
  assert.equal(outcomes.length, 1, "the coalesced duplicate gets no separate reply — one execution, one outcome");
  assert.equal(outcomes[0].payload.type, "agent-ready");
});

test("a launch-agent reusing an eventId with a DIFFERENT agentId is rejected as malformed — no side effect, no reply", () => {
  const calls = [];
  const { panel } = openWelcome({ runAgentAction: (agent, mode) => calls.push({ agent, mode }) });
  const eventId = "mismatch-0000-4000-8000-000000000000";

  fireLaunch(panel, eventId, "codex");
  const outcomesAfterFirst = bridgeOutcomeMessages(panel).length;
  const callsAfterFirst = calls.length;

  fireLaunch(panel, eventId, "claude-code"); // same eventId, DIFFERENT agentId

  assert.equal(calls.length, callsAfterFirst, "a mismatched-agentId reuse must never launch");
  assert.equal(bridgeOutcomeMessages(panel).length, outcomesAfterFirst, "and must add no new outcome");
});

test("completed-store bounds: the cap evicts the oldest completed entry first, once the retention cap is exceeded", () => {
  const calls = [];
  const { panel } = openWelcome({ runAgentAction: (agent, mode) => calls.push({ agent, mode }) });
  const floodCount = 300; // well over the >=256 retention cap

  for (let i = 0; i < floodCount; i++) fireLaunch(panel, "flood-" + i, "codex");

  const callsBeforeOldestRetry = calls.length;
  fireLaunch(panel, "flood-0", "codex"); // the OLDEST — must have been evicted
  assert.equal(calls.length, callsBeforeOldestRetry + 1, "an evicted eventId must be treated as a brand-new launch, not an idempotent re-ack");

  const callsBeforeNewestRetry = calls.length;
  fireLaunch(panel, "flood-" + (floodCount - 1), "codex"); // the NEWEST — must still be retained
  assert.equal(calls.length, callsBeforeNewestRetry, "a still-retained recent eventId must be idempotently re-acked, not re-launched");
});

test("an in-flight entry is never evicted, even under a concurrent flood of other completions past the cap", () => {
  const calls = [];
  let panelRef;
  const probeEventId = "probe-0000-4000-8000-000000000000";
  const { panel } = openWelcome({
    runAgentAction: (agent, mode) => {
      calls.push({ agent, mode });
      if (agent.id === "claude-code") {
        // While the probe launch is still executing (in-flight, not yet
        // completed), flood the store with far more than the cap's worth of
        // OTHER completed launches.
        for (let i = 0; i < 300; i++) fireLaunch(panelRef, "flood-" + i, "codex");
      }
    },
  });
  panelRef = panel;

  fireLaunch(panel, probeEventId, "claude-code");

  assert.equal(calls.filter((c) => c.agent.id === "claude-code").length, 1, "the probe itself must launch exactly once");
  const probeOutcomes = bridgeOutcomeMessages(panel).filter((m) => m.payload.eventId === probeEventId);
  assert.equal(probeOutcomes.length, 1, "an in-flight entry must complete normally — never evicted mid-flight by a concurrent flood");
  assert.equal(probeOutcomes[0].payload.type, "agent-ready");
});

test("a restart (fresh module instance) clears the store — a previously-used eventId is treated as brand new", () => {
  const calls1 = [];
  const { panel: panel1 } = openWelcome({ runAgentAction: (agent, mode) => calls1.push({ agent, mode }) });
  const eventId = "restart-0000-4000-8000-000000000000";
  fireLaunch(panel1, eventId, "codex");
  assert.equal(calls1.length, 1);

  const calls2 = [];
  const { panel: panel2 } = openWelcome({ runAgentAction: (agent, mode) => calls2.push({ agent, mode }) });
  fireLaunch(panel2, eventId, "codex");
  assert.equal(calls2.length, 1, "a fresh module instance must not remember the prior instance's completed eventId");
});

// ---- relay-forwarded gates re-emission (§4.3) ------------------------------

test("an unconfirmed outcome is re-emitted on the next announce; once relay-forwarded confirms it, later announces stop re-emitting it", () => {
  const { panel } = openWelcome();
  fireReady(panel, true); // initial announce
  const eventId = "relay-gate-0000-4000-8000-000000000000";

  fireLaunch(panel, eventId, "codex");
  assert.equal(bridgeOutcomeMessages(panel).filter((m) => m.payload.eventId === eventId).length, 1);

  // The receiver "died" before it could post relay-forwarded back — a fresh
  // announce (the next webview init in production; simulated here by firing
  // "ready" again on the same panel, which exercises the identical host-side
  // re-emit logic) must re-hand the still-unconfirmed outcome to the relay.
  fireReady(panel, true);
  assert.equal(
    bridgeOutcomeMessages(panel).filter((m) => m.payload.eventId === eventId).length,
    2,
    "an unconfirmed outcome must be re-emitted on the next announce"
  );

  fireRelayForwarded(panel, eventId);

  fireReady(panel, true);
  assert.equal(
    bridgeOutcomeMessages(panel).filter((m) => m.payload.eventId === eventId).length,
    2,
    "once relay-forwarded confirms delivery, later announces must not re-emit it"
  );
});

test("a relay-forwarded receipt for an unknown eventId is ignored (no throw, no effect)", () => {
  const { panel } = openWelcome();
  assert.doesNotThrow(() => fireRelayForwarded(panel, "no-such-event-0000-4000-8000-000000000000"));
});

// ---- welcome.html source pins (no jsdom in this harness — see
// bridge_relay_ratelimit.test.js's own header comment for why these read the
// shipped template source directly rather than executing it) ----------------

function readWelcomeHtmlSource() {
  return fs.readFileSync(path.join(TEMPLATES_DIR, "vscode-bootstrap-welcome.html"), "utf8");
}

test("welcome.html's inbound relay still forwards mode (set-mode) and agentId (launch-agent) alongside the shared six fields", () => {
  const src = readWelcomeHtmlSource();
  assert.match(src, /relay\.mode\s*=\s*primitiveField\(ev\.data\.mode,\s*"string"\)/);
  assert.match(src, /relay\.agentId\s*=\s*primitiveField\(ev\.data\.agentId,\s*"string"\)/);
});

test("welcome.html still posts a relay-forwarded receipt back to the host after relaying a bridge-outcome to window.top", () => {
  const src = readWelcomeHtmlSource();
  assert.match(src, /type:\s*"bridge-outcome"/);
  assert.match(src, /vscode\.postMessage\(\{\s*type:\s*"relay-forwarded",\s*eventId:\s*msg\.payload\.eventId\s*\}\)/);
});

// Regression (tracer live-proof 2026-07-29): a duplicated handleBridgeOutcome
// definition shadowed the working one and called an undefined helper OUTSIDE
// any try — every outcome threw before posting, so agent-ready never reached
// the top window. Source pins can't execute the webview, so pin the shape:
// exactly one definition, no stray helper, and the §4.1 fresh createdAt stamp
// in its body.
test("welcome.html defines handleBridgeOutcome exactly once, self-contained, with the fresh createdAt re-stamp", () => {
  const src = readWelcomeHtmlSource();
  const defs = src.match(/function handleBridgeOutcome\(/g) || [];
  assert.strictEqual(defs.length, 1, `handleBridgeOutcome defined ${defs.length}x — a later duplicate silently shadows the working one`);
  assert.doesNotMatch(src, /postToParent/, "handleBridgeOutcome must not call helpers that do not exist in the webview scope");
  const body = src.slice(src.indexOf("function handleBridgeOutcome("));
  assert.match(body.slice(0, body.indexOf("}")), /msg\.payload\.createdAt\s*=\s*Date\.now\(\)/);
});
