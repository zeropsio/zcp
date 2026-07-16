"use strict";

// Bridge auth flow (docs/spec-welcome-mode.md §4, W-AUTH; §8, W-SEC): the
// webview's credential-free {type:"authorize"} click -> host builds the §4
// payload and instructs the webview to relay it -> the webview relays the
// GUI's ack back up -> the host validates it (origin + eventId, defense in
// depth) and drives the phase machine. All protocol logic lives host-side
// (welcome.js); these tests fire messages directly at the panel, exactly as
// the dumb-pipe webview would relay them.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWelcome, makeFakeTimers, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

const BRIDGE_CHANNEL = "@zerops/zcp-agent-auth-bridge";
const ALLOWLISTED_ORIGIN = "https://app.zerops.io";
const ACK_TIMEOUT_MS = 3000;

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

function bridgeSendMessages(panel) {
  return panel.postedMessages.filter((m) => m.type === "bridge-send");
}

function authMessages(panel) {
  return panel.postedMessages.filter((m) => m.type === "auth");
}

function fireAck(panel, eventId, rest) {
  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: Object.assign({ channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId }, rest),
  });
}

test("authorize claude-code sends a bridge-send instruction with the exact §4 payload shape", () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });

  const sent = bridgeSendMessages(panel);
  assert.equal(sent.length, 1, "expected exactly one bridge-send instruction");
  const { payload, targets } = sent[0];
  assert.equal(payload.channel, "@zerops/zcp-agent-auth-bridge");
  assert.equal(payload.version, 1);
  assert.equal(payload.type, "open-agent-auth");
  assert.equal(payload.agentType, "claude-code");
  assert.match(
    payload.eventId,
    /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i,
    "eventId must be a UUIDv4"
  );
  assert.equal(typeof payload.createdAt, "number");
  assert.deepStrictEqual(targets, ["https://app.zerops.io"]);
});

test("authorize for a non-bridge-supported agent replies unsupported without sending a bridge message", () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "authorize", agentId: "codex" });

  assert.equal(bridgeSendMessages(panel).length, 0);
  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "codex", phase: "unsupported" });
});

test("a second authorize while one is in flight replies busy and starts no new flow", () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });

  assert.equal(bridgeSendMessages(panel).length, 1, "a second authorize must not send a second bridge-send");
  const busy = authMessages(panel).filter((m) => m.phase === "busy");
  assert.equal(busy.length, 1);
  assert.equal(busy[0].agentId, "claude-code");
});

test("an accepted ack moves the flow to dialog-opening and keeps it in flight", () => {
  const { panel } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  fireAck(panel, eventId, { accepted: true });

  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "claude-code", phase: "dialog-opening" });

  // Still in flight: a second authorize now replies busy, not a fresh bridge-send.
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  assert.equal(bridgeSendMessages(panel).length, 1, "still only the original bridge-send");
  assert.ok(authMessages(panel).some((m) => m.phase === "busy"), "flow must still be in flight after an accepted ack");
});

test("an ack with reason unsupported-agent releases the flow and reports unsupported", () => {
  const { panel } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  fireAck(panel, eventId, { accepted: false, reason: "unsupported-agent" });

  const auth = authMessages(panel);
  assert.deepStrictEqual(auth[auth.length - 1], { type: "auth", agentId: "claude-code", phase: "unsupported" });

  // Released: a fresh authorize now sends a NEW bridge-send with a different eventId.
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const sent = bridgeSendMessages(panel);
  assert.equal(sent.length, 2, "flow must be released so a new authorize starts a fresh bridge-send");
  assert.notEqual(sent[1].payload.eventId, eventId);
});

test("an ACK timeout releases the flow, reports no-dashboard, and never auto-launches the terminal fallback", () => {
  const timers = makeFakeTimers();
  const { panel, stub } = openWelcome({ setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout });

  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const ackCall = timers.calls.find((c) => c.ms === ACK_TIMEOUT_MS);
  assert.ok(ackCall, "expected a 3000ms ACK timer to be scheduled");

  timers.fire(ackCall.id);

  const auth = authMessages(panel);
  assert.deepStrictEqual(auth[auth.length - 1], { type: "auth", agentId: "claude-code", phase: "no-dashboard" });
  assert.equal(stub.terminals.length, 0, "a lost ACK must never auto-launch the terminal fallback");

  // Released: a fresh authorize now sends a new bridge-send.
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  assert.equal(bridgeSendMessages(panel).length, 2);
});

test("an accepted ack after the ACK timer already fired is ignored (stale eventId, flow already released)", () => {
  const timers = makeFakeTimers();
  const { panel } = openWelcome({ setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout });

  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;
  const ackCall = timers.calls.find((c) => c.ms === ACK_TIMEOUT_MS);
  timers.fire(ackCall.id);

  const countBefore = authMessages(panel).length;
  fireAck(panel, eventId, { accepted: true });

  assert.equal(authMessages(panel).length, countBefore, "a late ack for an already-released flow must be dropped");
});

test("a relayed ack from a non-allowlisted origin is ignored", () => {
  const { panel } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: "https://evil.example.com",
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  assert.equal(authMessages(panel).length, 0, "an untrusted origin must not move the flow");

  // Still in flight, unaffected: a second authorize still replies busy.
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  assert.ok(authMessages(panel).some((m) => m.phase === "busy"));
});

test("a relayed ack with a foreign eventId is ignored", () => {
  const { panel } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });

  fireAck(panel, "00000000-0000-4000-8000-000000000000", { accepted: true });

  assert.equal(authMessages(panel).length, 0, "a foreign eventId must not move the flow");
});
