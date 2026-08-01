"use strict";

// Launch gate (docs/spec-welcome-mode.md §5.1/§5.2, invariant W10): the ONLY
// launch gates for a bridge `launch-agent` command are identity gates (known
// registry id AND ZCP_AGENTS membership) — no installed-probe gate, no
// auth-flag gate. The executor always selects the agent's `terminal` open
// mode explicitly (never opens[0], which for claude-code is the extension),
// seeded with the fixed ONBOARD_PROMPT via the shared seedOpenWithPrompt/
// shellQuoteArg helpers (§5.1) — the SAME helpers onboard.test.js pins for
// the per-row onboard path. See command_channel.test.js for the dedup-store
// (W11) and announce (W12) coverage.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWelcome, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

const BRIDGE_CHANNEL = "@zerops/zcp-agent-auth-bridge";
const ALLOWLISTED_ORIGIN = "https://app.zerops.io";
const ONBOARD_PROMPT = "Onboard me to Zerops.";
const EVENT_ID = "11111111-1111-4111-8111-111111111111";

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
    antigravity: {
      ...TEST_REGISTRY.antigravity,
      opens: [{ mode: "terminal", command: "agy --dangerously-skip-permissions", initialPromptFlag: "--prompt-interactive" }],
    },
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
  return { stub, panel };
}

function bridgeOutcomeMessages(panel) {
  return panel.postedMessages.filter((m) => m.type === "bridge-outcome");
}

function fireLaunch(panel, eventId, agentId, origin) {
  panel.webview.__fireMessage({
    type: "bridge-window-message",
    origin: origin || ALLOWLISTED_ORIGIN,
    data: { channel: BRIDGE_CHANNEL, version: 1, type: "launch-agent", eventId, agentId, createdAt: Date.now() },
  });
}

test("claude-code launch selects the mode:\"terminal\" registry entry, never opens[0] (the extension)", () => {
  const calls = [];
  const { panel } = openWelcome({ runAgentAction: (agent, mode) => calls.push({ agent, mode }) });

  fireLaunch(panel, EVENT_ID, "claude-code");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].mode, "terminal");
  assert.equal(calls[0].agent.opens.length, 1, "only the terminal open ships to the executor");
  assert.equal(calls[0].agent.opens[0].mode, "terminal");
  assert.equal(calls[0].agent.opens[0].command, "claude --dangerously-skip-permissions --effort max '" + ONBOARD_PROMPT + "'");
});

test("seeded argv: the fixed ONBOARD_PROMPT is appended POSIX-single-quoted, positionally — exactly as seedOpenWithPrompt produces", () => {
  const calls = [];
  const { panel } = openWelcome({ runAgentAction: (agent, mode) => calls.push({ agent, mode }) });

  fireLaunch(panel, EVENT_ID, "codex");

  assert.equal(calls[0].agent.opens[0].command, "codex --dangerously-bypass-approvals-and-sandbox '" + ONBOARD_PROMPT + "'");
});

test("seeded argv via initialPromptFlag: antigravity's live-verified --prompt-interactive flag is used", () => {
  const calls = [];
  const { panel } = openWelcome({ runAgentAction: (agent, mode) => calls.push({ agent, mode }) });

  fireLaunch(panel, EVENT_ID, "antigravity");

  assert.equal(calls[0].agent.opens[0].command, "agy --dangerously-skip-permissions --prompt-interactive '" + ONBOARD_PROMPT + "'");
});

test("an agentId outside the known registry replies launch-failed/unknown-agent, with no launch", () => {
  const calls = [];
  const { panel } = openWelcome({ runAgentAction: (agent, mode) => calls.push({ agent, mode }) });

  fireLaunch(panel, EVENT_ID, "not-a-real-agent");

  assert.equal(calls.length, 0);
  const outcomes = bridgeOutcomeMessages(panel);
  assert.equal(outcomes.length, 1);
  assert.deepStrictEqual(
    { type: outcomes[0].payload.type, agentId: outcomes[0].payload.agentId, eventId: outcomes[0].payload.eventId, reason: outcomes[0].payload.reason },
    { type: "launch-failed", agentId: "not-a-real-agent", eventId: EVENT_ID, reason: "unknown-agent" }
  );
});

test("an agentId this container doesn't offer (ZCP_AGENTS excludes it) replies launch-failed/unknown-agent, even though the registry knows it", () => {
  const calls = [];
  const { panel } = openWelcome({
    resolveAvailableAgentIds: () => ["claude-code"],
    runAgentAction: (agent, mode) => calls.push({ agent, mode }),
  });

  fireLaunch(panel, EVENT_ID, "codex");

  assert.equal(calls.length, 0);
  assert.equal(bridgeOutcomeMessages(panel)[0].payload.reason, "unknown-agent");
});

test("W10: the installed-probe result is NEVER consulted — a launch proceeds even when isAgentInstalled reports false", () => {
  const calls = [];
  const { panel } = openWelcome({
    isAgentInstalled: () => false,
    runAgentAction: (agent, mode) => calls.push({ agent, mode }),
  });

  fireLaunch(panel, EVENT_ID, "codex");

  assert.equal(calls.length, 1, "no probe ever gates a bridge launch");
  assert.equal(bridgeOutcomeMessages(panel)[0].payload.type, "agent-ready");
});

test("W10: the authorization flag is NEVER consulted — a launch proceeds with no auth env present at all", () => {
  const calls = [];
  const { panel } = openWelcome({
    readZembedEnv: () => ({}), // no ZCP_AGENT_OAUTH_*/ZCP_AGENT_TOKEN_* whatsoever
    runAgentAction: (agent, mode) => calls.push({ agent, mode }),
  });

  fireLaunch(panel, EVENT_ID, "codex");

  assert.equal(calls.length, 1, "the auth flag is never a launch gate — zembed lag would otherwise reject a freshly-authorized launch");
  assert.equal(bridgeOutcomeMessages(panel)[0].payload.type, "agent-ready");
});

test("a terminal-creation throw from the executor replies launch-failed/terminal-error, pre-dispatch", () => {
  const { panel } = openWelcome({
    runAgentAction: () => { throw new Error("boom: no terminal panel available"); },
  });

  fireLaunch(panel, EVENT_ID, "codex");

  const outcomes = bridgeOutcomeMessages(panel);
  assert.equal(outcomes.length, 1);
  assert.deepStrictEqual(
    { type: outcomes[0].payload.type, reason: outcomes[0].payload.reason },
    { type: "launch-failed", reason: "terminal-error" }
  );
});

test("post-dispatch, nothing further is ever sent — no notification, no auth-phase row state, for a successful launch (§5.4)", () => {
  const { panel, stub } = openWelcome();

  fireLaunch(panel, EVENT_ID, "codex");

  assert.equal(bridgeOutcomeMessages(panel).length, 1);
  assert.equal(stub.infoMessages.length, 0, "§5.4: no notification");
  assert.equal(panel.postedMessages.filter((m) => m.type === "auth").length, 0, "§5.4: no row/auth-phase state");
});

test("launch-agent from a non-allowlisted origin is dropped — no launch, no outcome", () => {
  const calls = [];
  const { panel } = openWelcome({ runAgentAction: (agent, mode) => calls.push({ agent, mode }) });

  fireLaunch(panel, EVENT_ID, "codex", "https://evil.example.com");

  assert.equal(calls.length, 0);
  assert.equal(bridgeOutcomeMessages(panel).length, 0);
});

// ---- §5.3 onboarding layout (established only at launch-command execution)
//
// closeAllEditors closed the receiver webview too — the ONLY relay able to
// carry the launch outcome to window.top (§1.3/§4.3) — one line before
// finishLaunch handed it the agent-ready outcome, so the emission silently
// no-oped on the nulled panel and every real launch ended in the FE's 30s
// intent timeout while the agent ran behind the dark layer (live-reproduced
// on tatami). Editor cleanup is now tab-level, with the receiver's own tab
// always excluded from the close set — see establishOnboardingLayout in
// welcome.js. The vscode-stub's closeAllEditors AND tabGroups.close() both
// faithfully dispose any webview panel backing a tab they close (like real
// VS Code), so a regression back to either mechanism is caught by its
// actual effect (a disposed receiver + a dropped outcome), not merely by a
// command name appearing in executedCommands.

test("a successful onboarding launch establishes §5.3's editor-area layout: closes every OTHER editor tab, never the receiver's own, and reveals Explorer", () => {
  const { panel, stub } = openWelcome({ runAgentAction: () => {} });
  const tabGroups = stub.exports.window.tabGroups;
  tabGroups.__addEditorTab("a.js");
  tabGroups.__addEditorTab("b.js");
  const receiverTab = tabGroups.all[0].tabs.find((t) => t.__panel === panel);
  assert.ok(receiverTab, "the receiver panel must already have its own tab open");

  fireLaunch(panel, EVENT_ID, "codex");

  assert.equal(
    stub.executedCommands.some((c) => c.id === "workbench.action.closeAllEditors"),
    false,
    "closeAllEditors must never be used — it would close the receiver too (§5.3)"
  );
  assert.deepEqual(
    tabGroups.all[0].tabs,
    [receiverTab],
    "every OTHER editor tab closes; the receiver's own tab stays open"
  );
  assert.equal(panel.disposed, false, "the receiver panel itself must survive layout establishment");
  assert.ok(
    stub.executedCommands.some((c) => c.id === "workbench.view.explorer"),
    "§5.3: Explorer visible"
  );
});

test("regression: the agent-ready outcome still reaches the receiver even though closing a tab disposes its webview panel (real VS Code behavior) — establishOnboardingLayout never puts the receiver's own tab in the close set", () => {
  const { panel, stub } = openWelcome({ runAgentAction: () => {} });
  const tabGroups = stub.exports.window.tabGroups;
  tabGroups.__addEditorTab("a.js"); // at least one other tab actually exercises tabGroups.close()

  fireLaunch(panel, EVENT_ID, "codex");

  assert.equal(panel.disposed, false, "the receiver must survive its own layout establishment");
  const outcomes = bridgeOutcomeMessages(panel);
  assert.equal(outcomes.length, 1, "the agent-ready outcome must actually reach the still-alive receiver panel");
  assert.equal(outcomes[0].payload.type, "agent-ready");
});

test("fail-safe: with no tabGroups collaborator at all, the layout step skips closing editors entirely (never closeAllEditors) — the launch still completes with agent-ready", () => {
  const { panel, stub } = openWelcome({ runAgentAction: () => {}, tabGroups: null });

  fireLaunch(panel, EVENT_ID, "codex");

  assert.equal(stub.executedCommands.filter((c) => c.id === "workbench.action.closeAllEditors").length, 0);
  const outcomes = bridgeOutcomeMessages(panel);
  assert.equal(outcomes.length, 1);
  assert.equal(outcomes[0].payload.type, "agent-ready");
  assert.ok(
    stub.executedCommands.some((c) => c.id === "workbench.view.explorer"),
    "Explorer is still revealed even with no tab API"
  );
});

test("the onboarding launch tells the executor to establish the layout, so the terminal-panel maximize is deterministic rather than gated on the stale panelMaximized guess", () => {
  const calls = [];
  const { panel } = openWelcome({ runAgentAction: (agent, mode, opts) => calls.push({ agent, mode, opts }) });

  fireLaunch(panel, EVENT_ID, "codex");

  assert.equal(calls.length, 1);
  assert.equal(calls[0].opts && calls[0].opts.onboarding, true, "handleLaunchAgent must pass { onboarding: true } to the executor");
});

test("an identity-gate rejection (unknown agentId) never establishes the onboarding layout — nothing was ever dispatched", () => {
  const { panel, stub } = openWelcome({ runAgentAction: () => {} });

  fireLaunch(panel, EVENT_ID, "not-a-real-agent");

  assert.equal(stub.executedCommands.filter((c) => c.id === "workbench.action.closeAllEditors").length, 0);
  assert.equal(stub.executedCommands.filter((c) => c.id === "workbench.view.explorer").length, 0);
});

test("a terminal-creation throw (pre-dispatch terminal-error) never establishes the onboarding layout either — nothing was ever shown", () => {
  const { panel, stub } = openWelcome({
    runAgentAction: () => { throw new Error("boom: no terminal panel available"); },
  });

  fireLaunch(panel, EVENT_ID, "codex");

  assert.equal(stub.executedCommands.filter((c) => c.id === "workbench.action.closeAllEditors").length, 0);
  assert.equal(stub.executedCommands.filter((c) => c.id === "workbench.view.explorer").length, 0);
});
