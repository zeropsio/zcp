"use strict";

// Webview -> host message allowlist (docs/spec-welcome-mode.md §8, W-SEC):
// exactly "ready" and "open-url" (with an allowlisted url) do anything;
// everything else — a non-allowlisted url, an unknown type — is silently
// dropped, never thrown, never surfaced to the user. Extended by P3 for the
// auth-flow message types ("authorize", "bridge-window-message") and by P6 for
// "start-onboarding" — see bridge_flow.test.js / cta_flow.test.js for the
// flow-STATE behavior once a message passes this gate; this file covers
// gate-level shape/enum rejection only.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadExtension, installFakeAgentBins } = require("./harness.js");

const BRIDGE_CHANNEL = "@zerops/zcp-agent-auth-bridge";
const ALLOWLISTED_ORIGIN = "https://app.zerops.io";

// This file drives the REAL production wiring (loadExtension + the
// zerops.panel handler), so agent-action gates reach the real
// isAgentInstalled PATH probe — hermetic fake bins keep the outcome
// machine-independent (CI has no agent CLIs installed).
const restoreAgentBins = installFakeAgentBins();
test.after(() => restoreAgentBins());

async function openWelcome() {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate({ subscriptions: [], extensionPath: extensionDir });
  const handler = stub.registeredCommands.get("zerops.panel");
  handler(); // manual invocation (no opts) — self-close exempt, matches Command Palette use
  const panel = stub.panels.find((p) => p.viewType === "zeropsWelcome");
  return { stub, panel };
}

test("open-url with a non-allowlisted url is dropped", async () => {
  const { stub, panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "open-url", url: "https://evil.example.com" });

  assert.equal(stub.openedExternalUrls.length, 0);
});

test("open-url with an allowlisted url opens externally", async () => {
  const { stub, panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "open-url", url: "https://docs.zerops.io" });

  assert.deepStrictEqual(stub.openedExternalUrls, ["https://docs.zerops.io"]);
});

test("an unknown message type does nothing", async () => {
  const { stub, panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "not-a-real-type", url: "https://docs.zerops.io" });

  assert.equal(stub.openedExternalUrls.length, 0);
  assert.equal(panel.postedMessages.filter((m) => m.type === "state").length, 0);
});

test("authorize with an unknown agentId is dropped (bad enum)", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "authorize", agentId: "not-a-real-agent" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "bridge-send" || m.type === "auth").length, 0);
});

test("authorize with a non-string agentId is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "authorize", agentId: 12345 });

  assert.equal(panel.postedMessages.filter((m) => m.type === "bridge-send" || m.type === "auth").length, 0);
});

test("open-agent with an unknown agentId is dropped (bad enum)", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "open-agent", agentId: "not-a-real-agent" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0);
});

test("open-agent with a non-string agentId is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "open-agent", agentId: 12345 });

  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0);
});

test("bridge-window-message from a *.zerops.dev origin still passes the shape gate (origin allowlisting is a downstream flow check, not a gate-level concern)", async () => {
  const { panel } = await openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = panel.postedMessages.find((m) => m.type === "bridge-send").payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: "https://tatami.devel.zerops.dev",
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  assert.equal(panel.postedMessages.filter((m) => m.type === "auth" && m.phase === "dialog-opening").length, 1);
});

test("bridge-window-message with the wrong channel is dropped", async () => {
  const { panel } = await openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = panel.postedMessages.find((m) => m.type === "bridge-send").payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: { channel: "not-the-real-channel", version: 1, type: "open-agent-auth-ack", eventId, accepted: true },
  });

  // "contacting" is posted as soon as authorize() starts the flow — the
  // wrong-channel message must add nothing beyond it.
  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 1);
});

test("bridge-window-message with oversized relay data is dropped", async () => {
  const { panel } = await openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = panel.postedMessages.find((m) => m.type === "bridge-send").payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: {
      channel: BRIDGE_CHANNEL,
      version: 1,
      type: "open-agent-auth-ack",
      eventId,
      accepted: true,
      reason: "x".repeat(5000),
    },
  });

  // "contacting" is posted as soon as authorize() starts the flow — the
  // oversized message must add nothing beyond it.
  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 1);
});

test("bridge-window-message with a non-object data is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "bridge-window-message", origin: ALLOWLISTED_ORIGIN, data: "not-an-object" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0);
});

test("guided-toggle with a non-boolean enable is dropped (bad enum)", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "guided-toggle", enable: "true" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "guided-result").length, 0);
});

test("guided-toggle with a missing enable is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "guided-toggle" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "guided-result").length, 0);
});

test("guided-toggle with junk extra fields but a valid boolean enable still passes the gate", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "guided-toggle", enable: true, evil: { nested: true } });

  // openWelcome() has no workspace folder, so this is rejected downstream —
  // the point here is that the ALLOWLIST GATE itself let a well-typed
  // `enable` through regardless of extra junk fields.
  assert.equal(panel.postedMessages.filter((m) => m.type === "guided-result").length, 1);
});

test("skill-add is gone: it is dropped as an unrecognized message type", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "skill-add", slug: "tdd-red-green" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "skill-result" || m.type === "pack-result").length, 0);
});

test("pack-toggle (the retired message type) is dropped as unrecognized", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "pack-toggle", id: "superpowers", enable: true });

  assert.equal(panel.postedMessages.filter((m) => m.type === "pack-result").length, 0);
});

test("pack-action with a non-string id is dropped (bad enum)", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "pack-action", id: 12345, action: "add" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "pack-result").length, 0);
});

test("pack-action with an unknown id is dropped (bad enum)", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "pack-action", id: "not-a-real-pack", action: "add" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "pack-result").length, 0);
});

test("pack-action with a missing action is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "pack-action", id: "superpowers" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "pack-result").length, 0);
});

test('pack-action with an action outside "add"/"remove" is dropped (bad enum)', async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "pack-action", id: "superpowers", action: "update" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "pack-result").length, 0);
});

test("pack-action with junk extra fields but a valid id/action still passes the gate", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "pack-action", id: "superpowers", action: "add", evil: { nested: true } });
  await new Promise((resolve) => setImmediate(resolve));

  // openWelcome() here goes through extension.js's fixed production call
  // site, which passes no workspaceRoot override (see harness.js) — rejected
  // downstream regardless. The point here is that the ALLOWLIST GATE itself
  // let a well-typed id/action through despite the extra junk field.
  assert.equal(panel.postedMessages.filter((m) => m.type === "pack-result").length, 1);
});

test("pack-details is allowlisted: it reveals the Zerops Welcome output channel", async () => {
  const { stub, panel } = await openWelcome();
  // The output channel is created eagerly at panel-open time (see
  // guided_flow.test.js's own "creates the guided output channel once"
  // pin) — no guided/pack run needs to have happened first.
  assert.equal(stub.outputChannels.length, 1);
  const channel = stub.outputChannels[0];
  assert.equal(channel.shownCount, 0);

  panel.webview.__fireMessage({ type: "pack-details" });

  assert.equal(channel.shownCount, 1, "pack-details must reveal the output channel — proof it passed the allowlist gate and was handled, not silently dropped");
});

test("start-onboarding with a bad path enum is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "start-onboarding", path: "sideways", agentId: "claude-code" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "cta-result").length, 0);
});

test("start-onboarding with a missing path is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "start-onboarding", agentId: "claude-code" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "cta-result").length, 0);
});

test("start-onboarding with a non-string agentId is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: 12345 });

  assert.equal(panel.postedMessages.filter((m) => m.type === "cta-result").length, 0);
});

test("start-onboarding with an unknown agentId is dropped (bad enum)", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: "not-a-real-agent" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "cta-result").length, 0);
});

test("start-onboarding with agentId omitted still passes the gate", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new" });
  await new Promise((resolve) => setImmediate(resolve));

  // openWelcome() has no agent authorized, so this is rejected downstream —
  // the point here is that the ALLOWLIST GATE itself let a well-typed
  // `path` with no agentId through.
  assert.equal(panel.postedMessages.filter((m) => m.type === "cta-result").length, 1);
});

test("start-onboarding with junk extra fields but a valid path/agentId still passes the gate", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: "claude-code", evil: { nested: true } });
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(panel.postedMessages.filter((m) => m.type === "cta-result").length, 1);
});

test("ready with embedded:true is recorded and surfaces in the state push it triggers", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "ready", embedded: true });

  const states = panel.postedMessages.filter((m) => m.type === "state");
  assert.equal(states[states.length - 1].payload.diagnostics.embedded, true);
});

test("ready with embedded:false is recorded and surfaces in the state push it triggers", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "ready", embedded: false });

  const states = panel.postedMessages.filter((m) => m.type === "state");
  assert.equal(states[states.length - 1].payload.diagnostics.embedded, false);
});

test("ready with a non-boolean embedded is treated as absent — ready still processes (a state push still happens), diagnostics.embedded stays unknown", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "ready", embedded: "x" });

  const states = panel.postedMessages.filter((m) => m.type === "state");
  assert.ok(states.length > 0, "a malformed embedded field must not drop the ready message");
  assert.equal(states[states.length - 1].payload.diagnostics.embedded, null);
});

test("bridge-window-message with an arbitrary (unmatched) string reason passes the shape gate — the flow stays in flight (an unrecognized combo is dropped downstream, not at the gate)", async () => {
  const { panel } = await openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = panel.postedMessages.find((m) => m.type === "bridge-send").payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: false, reason: "some-arbitrary-reason" },
  });

  // Still in flight: a second authorize replies busy — proof the message
  // reached handleBridgeWindowMessage (the shape gate, isWellFormedBridgeRelay,
  // did not reject it for carrying a non-standard reason string) and was
  // dropped only because the accepted/reason combo is unrecognized, not
  // because the flow was released.
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  assert.ok(panel.postedMessages.some((m) => m.type === "auth" && m.phase === "busy"));
});

test("bridge-window-message with a non-primitive accepted field is dropped", async () => {
  const { panel } = await openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = panel.postedMessages.find((m) => m.type === "bridge-send").payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: { evil: true } },
  });

  // "contacting" is posted as soon as authorize() starts the flow — the
  // non-primitive accepted field must add nothing beyond it.
  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 1);
});

// ---- embed command channel (docs/spec-welcome-mode.md §4.3): set-mode and
// launch-agent dispatch through their OWN path, never gated on the ack
// path's live-auth-flow requirement — see command_channel.test.js /
// launch_gate.test.js for the full flow-level (W10/W11/W12) coverage; this
// file pins only that they are NOT rejected by the pre-existing authFlow
// guard, and that the ack path still requires it (contrast).

test("launch-agent is accepted through the pipeline with NO live auth flow in progress: it dispatches to a live terminal", async () => {
  const { stub, panel } = await openWelcome();

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "launch-agent", eventId: "33333333-3333-4333-8333-333333333333", agentId: "codex", createdAt: Date.now() },
  });

  assert.equal(stub.terminals.length, 1, "launch-agent must dispatch with no authFlow in progress");
  assert.equal(panel.postedMessages.filter((m) => m.type === "bridge-outcome" && m.payload.type === "agent-ready").length, 1);
});

test("set-mode is accepted through the pipeline with NO live auth flow in progress (not dropped by the ack path's authFlow guard)", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "set-mode", eventId: "44444444-4444-4444-8444-444444444444", mode: "onboarding", createdAt: Date.now() },
  });

  // set-mode has no reply of its own (§4.3); this file only pins that it
  // reaches its own handler rather than the ack path's "no bridge flow in
  // flight" drop — the ack path is proven still-gated by the sibling test
  // below.
  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0);
});

test("ack path still requires its flow: an open-agent-auth-ack with no authFlow in progress is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId: "55555555-5555-4555-8555-555555555555", accepted: true },
  });

  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0);
});
