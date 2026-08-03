"use strict";

// Diagnostics tile (docs/spec-welcome-mode.md §7/§8): a small muted,
// read-only signal for debugging a container/webview mismatch —
// zembedSeen, the installed extension version (read once, cached), a
// SHORTENED serviceId, and the last bridge-flow outcome. Never env values,
// never tokens, never paths beyond the extension version / a shortened
// serviceId (spec §8 W-SEC).

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

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
  return { stub, panel, welcome, ctx, deps, extensionDir };
}

function lastStatePayload(panel) {
  const states = panel.postedMessages.filter((m) => m.type === "state");
  return states[states.length - 1].payload;
}

test("no zembed store: zembedSeen is false and serviceId is the empty sentinel", () => {
  const { panel } = openWelcome({ readZembedEnv: () => null });

  panel.webview.__fireMessage({ type: "ready" });

  const diag = lastStatePayload(panel).diagnostics;
  assert.equal(diag.zembedSeen, false);
  assert.equal(diag.serviceId, "-");
});

test("a zembed store without a serviceId key also reports the empty sentinel", () => {
  const { panel } = openWelcome({ readZembedEnv: () => ({ ZCP_AGENTS: "claude-code" }) });

  panel.webview.__fireMessage({ type: "ready" });

  const diag = lastStatePayload(panel).diagnostics;
  assert.equal(diag.zembedSeen, true, "a real (readable) store was seen, even without a serviceId key");
  assert.equal(diag.serviceId, "-");
});

test("a present serviceId is shortened, never rendered in full", () => {
  const { panel } = openWelcome({ readZembedEnv: () => ({ serviceId: "svc-abcdef0123456789" }) });

  panel.webview.__fireMessage({ type: "ready" });

  const diag = lastStatePayload(panel).diagnostics;
  assert.equal(diag.serviceId, "svc-abcd…");
});

test("the extension version is read from the installed package.json", () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "ready" });

  // vscode-bootstrap-package.json's shipped "version" field — see
  // internal/content/templates/vscode-bootstrap-package.json.
  assert.equal(lastStatePayload(panel).diagnostics.extensionVersion, "0.1.30");
});

test("the extension version is read once and cached across multiple state pushes", () => {
  const { panel, extensionDir } = openWelcome();
  fs.writeFileSync(path.join(extensionDir, "package.json"), JSON.stringify({ version: "9.9.9" }));

  panel.webview.__fireMessage({ type: "ready" });
  assert.equal(lastStatePayload(panel).diagnostics.extensionVersion, "9.9.9", "first read must reflect the on-disk file");

  fs.writeFileSync(path.join(extensionDir, "package.json"), JSON.stringify({ version: "1.2.3" }));
  panel.webview.__fireMessage({ type: "ready" }); // a second, independent state push

  assert.equal(
    lastStatePayload(panel).diagnostics.extensionVersion,
    "9.9.9",
    "cached: a changed on-disk file must not be re-read mid-session"
  );
});

test("a missing/unreadable package.json degrades to the empty sentinel without throwing", () => {
  const { panel, extensionDir } = openWelcome();
  fs.rmSync(path.join(extensionDir, "package.json"));

  assert.doesNotThrow(() => panel.webview.__fireMessage({ type: "ready" }));
  assert.equal(lastStatePayload(panel).diagnostics.extensionVersion, "-");
});

test("lastBridgeOutcome starts at the empty sentinel and reflects the bridge flow's last phase", () => {
  // codex's binary isn't installed here -> handleAuthorize's isAgentActionable
  // gate rejects it with "unsupported" before ever sending a bridge message —
  // bridge support is no longer a fixed zcp-owned agent list (P2), so an
  // "unsupported" outcome now has to come from the availability/installed
  // axes instead.
  const { panel } = openWelcome({ isAgentInstalled: (bin) => bin !== "codex" });

  panel.webview.__fireMessage({ type: "ready" });
  assert.equal(lastStatePayload(panel).diagnostics.lastBridgeOutcome, "-");

  panel.webview.__fireMessage({ type: "authorize", agentId: "codex" });
  panel.webview.__fireMessage({ type: "ready" });

  assert.equal(lastStatePayload(panel).diagnostics.lastBridgeOutcome, "unsupported");
});

test("diagnostics.embedded starts null (unknown) before any ready message has reported it", () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "ready" }); // no embedded field at all

  assert.equal(lastStatePayload(panel).diagnostics.embedded, null);
});

test("diagnostics.embedded reflects a ready message's embedded:true", () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "ready", embedded: true });

  assert.equal(lastStatePayload(panel).diagnostics.embedded, true);
});

test("diagnostics.embedded reflects a ready message's embedded:false", () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "ready", embedded: false });

  assert.equal(lastStatePayload(panel).diagnostics.embedded, false);
});

test("a non-boolean embedded on a later ready is treated as absent — diagnostics.embedded keeps its last known value", () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "ready", embedded: true }); // establish a known value
  panel.webview.__fireMessage({ type: "ready", embedded: "not-a-boolean" }); // malformed -> ignored

  assert.equal(lastStatePayload(panel).diagnostics.embedded, true, "a malformed embedded field must not overwrite the last known value");
});

test("the full state payload never leaks a raw token/secret value from the zembed env", () => {
  const FAKE_TOKEN = "sk-super-secret-fake-token-value-123456";
  const { panel } = openWelcome({
    readZembedEnv: () => ({
      ZCP_AGENT_TOKEN_CODEX: FAKE_TOKEN,
      ZCP_AGENT_OAUTH_CLAUDE_CODE: "true",
      serviceId: "svc-abcdef0123456789",
    }),
  });

  panel.webview.__fireMessage({ type: "ready" });

  const payload = lastStatePayload(panel);
  const serialized = JSON.stringify(payload);
  assert.equal(serialized.includes(FAKE_TOKEN), false, "the raw token value must never reach the webview payload");
  assert.equal(serialized.includes("svc-abcdef0123456789"), false, "the full (unshortened) service id must never reach the webview payload");
  assert.equal(payload.diagnostics.serviceId, "svc-abcd…");
});
