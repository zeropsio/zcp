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
  const { payload, target } = sent[0];
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
  // Broadcast, not a pinned origin: the webview can't read the cross-origin
  // parent's real origin, and the payload carries no secret — the frontend
  // receiver is the actual security gate (spec-welcome-mode.md §4 W-AUTH).
  assert.equal(target, "*");
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

test("a relayed ack from a *.zerops.dev stage origin is accepted (generic GUI origin, not just prod)", () => {
  const { panel } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: "https://tatami.devel.zerops.dev",
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "claude-code", phase: "dialog-opening" });
});

test("a relayed ack from an arbitrary *.zerops.app subdomain is rejected without an operator opt-in (shared customer namespace)", () => {
  const { panel } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: "https://febridge-24cb.prg1.zerops.app",
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  assert.equal(authMessages(panel).length, 0, "an unconfigured *.zerops.app origin must not move the flow — it is the shared customer namespace, not a GUI trust boundary");
});

test("a relayed ack from a ZCP_WELCOME_BRIDGE_ORIGINS-configured origin is accepted (comma-separated list, entries trimmed)", () => {
  const extraOrigin = "https://febridge-24cb.prg1.zerops.app";
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_WELCOME_BRIDGE_ORIGINS: ` https://other-test.prg1.zerops.app , ${extraOrigin} ,` }),
  });
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: extraOrigin,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "claude-code", phase: "dialog-opening" });
});

test("ZCP_WELCOME_BRIDGE_ORIGINS opt-in is exact-match only — a different *.zerops.app origin not on the list stays rejected", () => {
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_WELCOME_BRIDGE_ORIGINS: "https://febridge-24cb.prg1.zerops.app" }),
  });
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: "https://other.prg1.zerops.app",
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  assert.equal(authMessages(panel).length, 0, "a non-listed origin on the same shared namespace must still be rejected");
});

test("ZCP_WELCOME_BRIDGE_ORIGINS falls back to process.env when the zembed store has no opinion", () => {
  const extraOrigin = "https://febridge-24cb.prg1.zerops.app";
  const original = process.env.ZCP_WELCOME_BRIDGE_ORIGINS;
  process.env.ZCP_WELCOME_BRIDGE_ORIGINS = extraOrigin;
  try {
    const { panel } = openWelcome(); // default readZembedEnv: () => null
    panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
    const eventId = bridgeSendMessages(panel)[0].payload.eventId;

    panel.webview.__fireMessage({
      type: "bridge-window-message",
      origin: extraOrigin,
      data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
    });

    const auth = authMessages(panel);
    assert.equal(auth.length, 1);
    assert.deepStrictEqual(auth[0], { type: "auth", agentId: "claude-code", phase: "dialog-opening" });
  } finally {
    if (original === undefined) delete process.env.ZCP_WELCOME_BRIDGE_ORIGINS;
    else process.env.ZCP_WELCOME_BRIDGE_ORIGINS = original;
  }
});

test("a relayed ack from a look-alike suffix-bypass origin is still ignored (substring, not dot-boundary, match)", () => {
  const { panel } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: "https://zerops.app.attacker.com",
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  assert.equal(authMessages(panel).length, 0, "a look-alike host must not move the flow");
});

test("a relayed ack with a foreign eventId is ignored", () => {
  const { panel } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });

  fireAck(panel, "00000000-0000-4000-8000-000000000000", { accepted: true });

  assert.equal(authMessages(panel).length, 0, "a foreign eventId must not move the flow");
});
