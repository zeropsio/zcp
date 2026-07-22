"use strict";

// Webview -> host message allowlist (docs/spec-welcome-mode.md §8, W-SEC):
// exactly "ready" and "open-url" (with an allowlisted url) do anything;
// everything else — a non-allowlisted url, an unknown type — is silently
// dropped, never thrown, never surfaced to the user. Extended by P3 for the
// auth-flow message types ("authorize", "authorize-terminal",
// "bridge-window-message") and by P6 for "start-onboarding" — see
// bridge_flow.test.js / terminal_flow.test.js / cta_flow.test.js for the
// flow-STATE behavior once a message passes this gate; this file covers
// gate-level shape/enum rejection only.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadExtension, installFakeAgentBins } = require("./harness.js");

const BRIDGE_CHANNEL = "@zerops/zcp-agent-auth-bridge";
const ALLOWLISTED_ORIGIN = "https://app.zerops.io";

// This file drives the REAL production wiring (loadExtension + the
// zerops.welcome handler), so agent-action gates reach the real
// isAgentInstalled PATH probe — hermetic fake bins keep the outcome
// machine-independent (CI has no agent CLIs installed).
const restoreAgentBins = installFakeAgentBins();
test.after(() => restoreAgentBins());

async function openWelcome() {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate({ subscriptions: [], extensionPath: extensionDir });
  const handler = stub.registeredCommands.get("zerops.welcome");
  handler();
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

test("authorize-terminal with an unknown agentId is dropped (bad enum)", async () => {
  const { stub, panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: "not-a-real-agent" });

  assert.equal(stub.terminals.length, 0);
  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0);
});

test("authorize-terminal with a non-string agentId is dropped", async () => {
  const { stub, panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "authorize-terminal", agentId: 12345 });

  assert.equal(stub.terminals.length, 0);
  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0);
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

  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0);
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

  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0);
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

test("skill-add with a non-string slug is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "skill-add", slug: 12345 });

  assert.equal(panel.postedMessages.filter((m) => m.type === "skill-result").length, 0);
});

test("skill-add with a missing slug is dropped", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "skill-add" });

  assert.equal(panel.postedMessages.filter((m) => m.type === "skill-result").length, 0);
});

test("skill-add with junk extra fields but a valid string slug still passes the gate", async () => {
  const { panel } = await openWelcome();

  panel.webview.__fireMessage({ type: "skill-add", slug: "not-a-real-skill", evil: { nested: true } });
  await new Promise((resolve) => setImmediate(resolve));

  // openWelcome() here goes through extension.js's fixed production call
  // site, which passes no workspaceRoot override (see harness.js), and
  // "not-a-real-skill" isn't in the shipped allowlist either — both are
  // rejected downstream regardless. The point here is that the ALLOWLIST
  // GATE itself let a well-typed `slug` through despite the extra junk field.
  assert.equal(panel.postedMessages.filter((m) => m.type === "skill-result").length, 1);
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

test("bridge-window-message with a non-primitive accepted field is dropped", async () => {
  const { panel } = await openWelcome();
  panel.webview.__fireMessage({ type: "authorize", agentId: "claude-code" });
  const eventId = panel.postedMessages.find((m) => m.type === "bridge-send").payload.eventId;

  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "open-agent-auth-ack", eventId, accepted: { evil: true } },
  });

  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0);
});
