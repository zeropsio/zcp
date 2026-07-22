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

// Bridge support is no longer a fixed zcp-owned agent list (P2 deleted
// BRIDGE_SUPPORTED_AGENTS): ANY agent this container offers and has
// installed goes through the bridge — the Zerops GUI receiver is the
// capability authority, rejecting what it can't handle via its own
// accepted:false/"unsupported-agent" ack (see the test below this one for
// that path). zcp's own "unsupported" rejection is limited to the
// availability + installed axes (isAgentActionable), covered next.

test("authorize for grok (any available+installed agent, not just claude-code) sends a bridge-send with agentType grok", () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "authorize", agentId: "grok" });

  const sent = bridgeSendMessages(panel);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].payload.agentType, "grok");
});

test("authorize for an agent whose binary is not installed replies unsupported and sends no bridge message", () => {
  const { panel } = openWelcome({ isAgentInstalled: (bin) => bin !== "codex" });

  panel.webview.__fireMessage({ type: "authorize", agentId: "codex" });

  assert.equal(bridgeSendMessages(panel).length, 0);
  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "codex", phase: "unsupported" });
});

test("authorize for an agent this container doesn't offer (resolver omits it) replies unsupported and sends no bridge message", () => {
  const { panel } = openWelcome({ resolveAvailableAgentIds: () => ["claude-code"] });

  panel.webview.__fireMessage({ type: "authorize", agentId: "codex" });

  assert.equal(bridgeSendMessages(panel).length, 0);
  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "codex", phase: "unsupported" });
});

test("a ZCP_AGENTS edit mid-flight (the flow's agent drops out of availability) releases the flow to idle instead of holding the lock", () => {
  let offered = ["codex", "claude-code"];
  const { panel, welcome, ctx, deps } = openWelcome({ resolveAvailableAgentIds: () => offered });

  panel.webview.__fireMessage({ type: "authorize", agentId: "codex" });
  assert.equal(bridgeSendMessages(panel).length, 1, "sanity: the flow started");

  offered = ["claude-code"]; // ZCP_AGENTS edit mid-flight: codex no longer offered

  welcome.open(ctx, deps); // reveal -> postState -> reconcileAuthFlow observes the change

  assert.ok(
    authMessages(panel).some((m) => m.phase === "idle" && m.agentId === "codex"),
    "the stale flow must release to idle, not sit held for the 10-minute cap"
  );

  // Released: a fresh authorize for a DIFFERENT agent must not reply busy.
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  assert.ok(
    !authMessages(panel).some((m) => m.phase === "busy" && m.agentId === "claude-code"),
    "the lock must be free once the flow's own agent stopped being actionable"
  );
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

test("an accepted ack reports dialog-opening and RELEASES the flow — a dismissed GUI dialog must not dead-zone re-authorization", () => {
  const { panel } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  fireAck(panel, eventId, { accepted: true });

  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "claude-code", phase: "dialog-opening" });

  // Released: the trigger did its job (the GUI opened its dialog). The GUI
  // cannot report a dismissal, so holding the single-flow lock here would
  // make every re-click a silent "busy" until the cap — the live-reported
  // dismiss-then-reclick dead zone. A second authorize therefore starts a
  // FRESH flow (new eventId), never busy.
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const sends = bridgeSendMessages(panel);
  assert.equal(sends.length, 2, "a re-click after an accepted ack must send a fresh bridge trigger");
  assert.notEqual(sends[1].payload.eventId, eventId, "the re-click mints a new eventId");
  assert.ok(!authMessages(panel).some((m) => m.phase === "busy"), "no busy phase after an accepted ack released the flow");
});

test("a stale accepted ack re-delivered after release is ignored (its eventId no longer matches the fresh in-flight flow)", () => {
  const { panel } = openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;
  fireAck(panel, eventId, { accepted: true }); // releases the first flow

  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" }); // fresh flow, new eventId
  fireAck(panel, eventId, { accepted: true }); // STALE eventId from the released first flow

  // Exactly one dialog-opening (the first flow's); the stale ack is dropped
  // on the eventId mismatch and the fresh flow stays in flight (a third
  // authorize replies busy).
  const phases = authMessages(panel).map((m) => m.phase);
  assert.deepStrictEqual(phases, ["dialog-opening"]);
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  assert.ok(authMessages(panel).some((m) => m.phase === "busy"), "the fresh flow is still in flight");
  assert.equal(bridgeSendMessages(panel).length, 2, "no third bridge-send while the fresh flow waits for its ack");
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

test("ZCP_WELCOME_BRIDGE_ORIGINS is read from the LIVE zembed store only — a readable store WITHOUT the key means no extras, even if the extension host's frozen process.env still carries the origin (closes the stale-trust window)", () => {
  const extraOrigin = "https://febridge-24cb.prg1.zerops.app";
  const original = process.env.ZCP_WELCOME_BRIDGE_ORIGINS;
  process.env.ZCP_WELCOME_BRIDGE_ORIGINS = extraOrigin; // frozen at host boot; operator has since revoked it from the live store
  try {
    const { panel } = openWelcome({ readZembedEnv: () => ({}) }); // live store readable, key absent (revoked)
    panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
    const eventId = bridgeSendMessages(panel)[0].payload.eventId;

    panel.webview.__fireMessage({
      type: "bridge-window-message",
      origin: extraOrigin,
      data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
    });

    assert.equal(authMessages(panel).length, 0, "process.env must never be trusted when the live zembed store is readable — a revoked key means no extras");
  } finally {
    if (original === undefined) delete process.env.ZCP_WELCOME_BRIDGE_ORIGINS;
    else process.env.ZCP_WELCOME_BRIDGE_ORIGINS = original;
  }
});

test("ZCP_WELCOME_BRIDGE_ORIGINS fails closed when the zembed store is unreadable (readZembedEnv null: no store or a transient/malformed read) — no extras, even with process.env set", () => {
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

    assert.equal(authMessages(panel).length, 0, "an unreadable store fails closed to no extras, never reaching for frozen process.env");
  } finally {
    if (original === undefined) delete process.env.ZCP_WELCOME_BRIDGE_ORIGINS;
    else process.env.ZCP_WELCOME_BRIDGE_ORIGINS = original;
  }
});

test("ZCP_WELCOME_BRIDGE_ORIGINS is resolved FRESH at the ack, not cached from authorize()/open() time — a revoked origin is dropped without reopening the panel", () => {
  // Mutable stub: trusts the origin while the flow starts, then "revokes" it
  // (the zembed store stops carrying the key) before the ack arrives. If the
  // allowlist were resolved once at authorize()/open() time and cached on
  // the flow, this ack would still be trusted; resolving it fresh at the ack
  // check catches the revocation immediately.
  let origins = "https://custom-test.prg1.zerops.app";
  const { panel } = openWelcome({
    readZembedEnv: () => (origins ? { ZCP_WELCOME_BRIDGE_ORIGINS: origins } : {}),
  });
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  origins = null; // operator revokes the origin between authorize() and the ack

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: "https://custom-test.prg1.zerops.app",
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  assert.equal(authMessages(panel).length, 0, "a revoked origin must be re-checked live at the ack, not trusted from a stale open()-time snapshot");
});

test("control: an origin still configured at ack time is accepted (contrasts with the staleness test above)", () => {
  let origins = "https://custom-test.prg1.zerops.app";
  const { panel } = openWelcome({
    readZembedEnv: () => (origins ? { ZCP_WELCOME_BRIDGE_ORIGINS: origins } : {}),
  });
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: "https://custom-test.prg1.zerops.app",
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "claude-code", phase: "dialog-opening" });
});

test("a ZCP_WELCOME_BRIDGE_ORIGINS entry with a trailing slash still matches the canonical browser origin", () => {
  const canonical = "https://febridge-24cb.prg1.zerops.app";
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_WELCOME_BRIDGE_ORIGINS: canonical + "/" }),
  });
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: canonical,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "claude-code", phase: "dialog-opening" });
});

test("a ZCP_WELCOME_BRIDGE_ORIGINS entry with an explicit default :443 port still matches the canonical (portless) browser origin", () => {
  const canonical = "https://febridge-24cb.prg1.zerops.app";
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_WELCOME_BRIDGE_ORIGINS: canonical + ":443" }),
  });
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: canonical,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "claude-code", phase: "dialog-opening" });
});

test("a ZCP_WELCOME_BRIDGE_ORIGINS entry with an uppercase host still matches the canonical lowercase browser origin", () => {
  const canonical = "https://febridge-24cb.prg1.zerops.app";
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_WELCOME_BRIDGE_ORIGINS: "https://FEBRIDGE-24cb.PRG1.ZEROPS.APP" }),
  });
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: canonical,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "claude-code", phase: "dialog-opening" });
});

test("a junk ZCP_WELCOME_BRIDGE_ORIGINS entry is skipped silently without breaking a valid sibling entry", () => {
  const canonical = "https://febridge-24cb.prg1.zerops.app";
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_WELCOME_BRIDGE_ORIGINS: `not a url, ${canonical}` }),
  });
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = bridgeSendMessages(panel)[0].payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: canonical,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  const auth = authMessages(panel);
  assert.equal(auth.length, 1);
  assert.deepStrictEqual(auth[0], { type: "auth", agentId: "claude-code", phase: "dialog-opening" });
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
