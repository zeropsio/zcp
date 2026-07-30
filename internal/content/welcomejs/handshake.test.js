"use strict";

// Ready handshake -> full §3 state payload (docs/spec-welcome-mode.md §1
// W-ENTRY; §3 W-STATE), plus reveal/focus re-reads (missed watcher events
// must not leave stale UI, per §1). This supersedes the P1-era "ready
// handshake posts state with all 5 registered agents" test that lived in
// welcome_panel.test.js: that test asserted the static a.status==="checking"
// skeleton, which P2 replaces with the real per-agent §3 state.
//
// Like watchers.test.js, these tests call welcome.open() directly (via
// loadWelcome()) instead of going through extension.js's command handler, so
// they can control readZembedEnv/homeDir/workspaceRoot per case. They omit
// the 3rd (opts) argument entirely, which open() treats as a manual
// invocation (§1.4) — this handshake shape is unchanged by the §1.3 receiver
// lifecycle (manual is exempt from it); see receiver_lifecycle.test.js for
// the boot-always (opts={manual:false,...}) behavior, and
// command_channel.test.js for the embed-ready announce this same "ready"
// case sends (unaffected here — a shape/payload suite, not a protocol one).

const test = require("node:test");
const assert = require("node:assert/strict");
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
  return { stub, panel, welcome, ctx, deps };
}

test("ready posts a full state payload with all agents, guided, and environment fields", () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "ready" });

  const stateMsgs = panel.postedMessages.filter((m) => m.type === "state");
  assert.equal(stateMsgs.length, 1, "ready must post exactly one state message");
  const payload = stateMsgs[0].payload;

  assert.equal(payload.agents.length, TEST_AGENT_IDS.length);
  for (const a of payload.agents) {
    assert.equal(a.state, "not-authorized", `agent ${a.id} should start not-authorized (no flags, no creds)`);
    assert.ok(a.label, `agent ${a.id} must carry a label`);
  }
  // End-to-end proof of the real collector's CRED_PROBE registry (spec §3:
  // "Credential probes exist only for agents whose artifact path is
  // live-verified — v1: claude-code, codex"), independent of whether either
  // agent's cred file actually exists on this run's (nonexistent) homeDir.
  const byId = Object.fromEntries(payload.agents.map((a) => [a.id, a]));
  assert.equal(byId["claude-code"].probeVerified, true);
  assert.equal(byId["codex"].probeVerified, true);
  for (const id of ["antigravity", "grok", "cursor"]) {
    assert.equal(byId[id].probeVerified, false, `${id} has no verified probe (spec §3)`);
  }
  assert.equal(payload.anyAuthorized, false);
  assert.deepStrictEqual(payload.guided, { state: "unknown" }, "no workspaceRoot -> guided unknown");
  assert.deepStrictEqual(payload.environment, { zembed: false });
  assert.deepStrictEqual(payload.bridge, { status: "unknown" });
  assert.deepStrictEqual(payload.packs, []);
});

test("re-invoking the command on an existing panel (reveal) pushes fresh state", () => {
  // A boolean flip (not a raw call-count threshold): open() itself now reads
  // readZembedEnv() once per invocation too (bridgeExtraOrigins, spec §4
  // W-AUTH), so the exact number of reads before the webview's first state
  // push is an implementation detail this test shouldn't pin — what matters
  // is that the flag is absent through the FIRST state push and present by
  // the reveal's.
  let flagged = false;
  const { panel, welcome, ctx, deps } = openWelcome({
    readZembedEnv: () => (flagged ? { ZCP_AGENT_OAUTH_CLAUDE_CODE: "true" } : null),
  });

  panel.webview.__fireMessage({ type: "ready" });
  assert.equal(panel.postedMessages.filter((m) => m.type === "state").length, 1);
  assert.equal(panel.postedMessages[0].payload.agents.find((a) => a.id === "claude-code").state, "not-authorized");

  flagged = true; // the platform flag lands between the two reads
  welcome.open(ctx, deps); // re-invoking zerops.panel on the existing panel

  const msgs = panel.postedMessages.filter((m) => m.type === "state");
  assert.equal(msgs.length, 2, "reveal must push a fresh state message");
  assert.equal(panel.revealCount, 1, "reveal must reveal the existing panel, not recreate it");
  assert.equal(
    msgs[1].payload.agents.find((a) => a.id === "claude-code").state,
    "reconnect",
    "the flag flipped to present with no local cred between the two reads -> reconnect"
  );
});

test("becoming visible via onDidChangeViewState (tab switch, no command re-run) pushes fresh state", () => {
  // Boolean flip, not a raw call-count threshold — see the reveal test above.
  let flagged = false;
  const { panel } = openWelcome({
    readZembedEnv: () => (flagged ? { ZCP_AGENT_TOKEN_CODEX: "some-token-value" } : null),
  });

  panel.webview.__fireMessage({ type: "ready" });
  assert.equal(panel.postedMessages.filter((m) => m.type === "state").length, 1);

  panel.__setVisible(false); // e.g. the user switched to another editor tab
  assert.equal(panel.postedMessages.filter((m) => m.type === "state").length, 1, "hiding must not push");

  flagged = true; // the platform flag lands while the panel is hidden
  panel.__setVisible(true); // switched back — no command re-invocation

  const msgs = panel.postedMessages.filter((m) => m.type === "state");
  assert.equal(msgs.length, 2, "becoming visible again must push a fresh state message");
  assert.equal(msgs[1].payload.agents.find((a) => a.id === "codex").state, "authorized-token");
});

test("guided/environment reflect a real workspaceRoot + zembed store on ready", () => {
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_AGENT_TOKEN_CODEX: "tok" }),
    workspaceRoot: "/tmp/zcp-welcomejs-ws-handshake", // no .zcp/state here -> disabled, not unknown
    // A non-null workspaceRoot makes "ready" also trigger a pack-status
    // refresh (docs/spec-welcome-mode.md §4) — a harmless never-firing stub
    // keeps this test from invoking the REAL zcp binary (installed on PATH
    // on a dev machine) against a directory that was never actually created.
    spawn: () => new (require("node:events").EventEmitter)(),
  });

  panel.webview.__fireMessage({ type: "ready" });

  const payload = panel.postedMessages.find((m) => m.type === "state").payload;
  assert.deepStrictEqual(payload.environment, { zembed: true });
  assert.deepStrictEqual(payload.guided, { state: "disabled" }, "a real workspace with no marker file is disabled, not unknown");
});
