"use strict";

// CTA (docs/spec-welcome-mode.md §7, W-CTA): the webview's {type:"start-
// onboarding", path, agentId} click -> host re-validates against a FRESH
// state read (never the webview's own idea of who's authorized), launches
// via the injected runAgentAction (HOW is entirely its call), and hands the
// kickoff prompt to the agent through the clipboard — NEVER terminal.
// sendText, NEVER a delayed setTimeout injection (a terminal may not even
// be running the agent). Gate-level shape/enum rejection (bad path, bad
// agentId type) lives in message_allowlist.test.js; this file covers the
// flow's SEMANTIC behavior once a message has passed that gate.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadWelcome, makeFakeTimers, TEST_REGISTRY, TEST_AGENT_IDS } = require("./harness.js");

const CTA_PROMPT_NEW =
  "I want to build something new on Zerops. Ask me what I'm building, then plan the smallest working version and get it running on this project's dev runtime.";
const CTA_PROMPT_EXISTING =
  "I have an existing app in this workspace that I want to run on Zerops. Inspect the repo, tell me your integration plan, then wire it up and get it running on the dev runtime.";
const CTA_NOT_AUTHORIZED_MESSAGE = "Authorize an agent first.";
const CTA_SELECT_AGENT_MESSAGE = "Select which authorized agent should start.";

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

function ctaResults(panel) {
  return panel.postedMessages.filter((m) => m.type === "cta-result");
}

// recordingDeps stands in for runAgentAction/clipboard/showInformationMessage
// — the three seams handleStartOnboarding calls out to — recording every
// call so a test can assert on them without a real webview/terminal/OS
// clipboard.
function recordingDeps(overrides) {
  const runAgentActionCalls = [];
  const clipboardWrites = [];
  const infoMessages = [];
  return {
    runAgentActionCalls,
    clipboardWrites,
    infoMessages,
    deps: Object.assign(
      {
        runAgentAction: (agent, mode) => runAgentActionCalls.push({ agent, mode }),
        clipboard: { writeText: async (text) => { clipboardWrites.push(text); } },
        showInformationMessage: (message) => { infoMessages.push(message); },
      },
      overrides
    ),
  };
}

// flush drains a few microtask+macrotask rounds — handleStartOnboarding
// crosses at least one real await (deps.clipboard.writeText) — mirroring
// skills_install.test.js's own flush() for the same reason.
function flush(rounds = 3) {
  let p = Promise.resolve();
  for (let i = 0; i < rounds; i++) p = p.then(() => new Promise((resolve) => setImmediate(resolve)));
  return p;
}

test("a bad path enum reaching the handler produces no result", async () => {
  const { panel } = openWelcome();

  panel.webview.__fireMessage({ type: "start-onboarding", path: "sideways", agentId: "claude-code" });
  await flush();

  assert.equal(ctaResults(panel).length, 0);
});

test("rejected when no agent is authorized", async () => {
  const { runAgentActionCalls, clipboardWrites, deps: rec } = recordingDeps();
  const { panel } = openWelcome(rec);

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new" });
  await flush();

  assert.deepStrictEqual(ctaResults(panel), [{ type: "cta-result", ok: false, message: CTA_NOT_AUTHORIZED_MESSAGE }]);
  assert.equal(runAgentActionCalls.length, 0);
  assert.equal(clipboardWrites.length, 0);
});

test("an ambiguous (omitted) agentId with two authorized agents is rejected with a selection message", async () => {
  const { runAgentActionCalls, deps: rec } = recordingDeps({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true", ZCP_AGENT_OAUTH_GROK: "true" }),
  });
  const { panel } = openWelcome(rec);

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new" }); // no agentId — ambiguous
  await flush();

  assert.deepStrictEqual(ctaResults(panel), [{ type: "cta-result", ok: false, message: CTA_SELECT_AGENT_MESSAGE }]);
  assert.equal(runAgentActionCalls.length, 0, "must never silently pick a registry default");
});

test("a mismatched explicit agentId with two authorized agents is also rejected — never falls back to the first in registry", async () => {
  const { runAgentActionCalls, deps: rec } = recordingDeps({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true", ZCP_AGENT_OAUTH_GROK: "true" }),
  });
  const { panel } = openWelcome(rec);

  // "cursor" is a known agent id but is NOT one of the two authorized here.
  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: "cursor" });
  await flush();

  assert.deepStrictEqual(ctaResults(panel), [{ type: "cta-result", ok: false, message: CTA_SELECT_AGENT_MESSAGE }]);
  assert.equal(runAgentActionCalls.length, 0);
});

test("zero runnable due to an uninstalled binary: an authorized-but-not-installed agent is rejected as not-authorized", async () => {
  const { runAgentActionCalls, deps: rec } = recordingDeps({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_CODEX: "true" }),
    isAgentInstalled: (bin) => bin !== "codex",
  });
  const { panel } = openWelcome(rec);

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: "codex" });
  await flush();

  assert.deepStrictEqual(ctaResults(panel), [{ type: "cta-result", ok: false, message: CTA_NOT_AUTHORIZED_MESSAGE }]);
  assert.equal(runAgentActionCalls.length, 0);
});

test("an explicitly picked agent that is authorized but not installed (not runnable) is rejected with the select-agent message, not a silent fallback to the other runnable agent", async () => {
  const { runAgentActionCalls, deps: rec } = recordingDeps({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true", ZCP_AGENT_OAUTH_CODEX: "true" }),
    isAgentInstalled: (bin) => bin !== "codex",
  });
  const { panel } = openWelcome(rec);

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: "codex" });
  await flush();

  assert.deepStrictEqual(ctaResults(panel), [{ type: "cta-result", ok: false, message: CTA_SELECT_AGENT_MESSAGE }]);
  assert.equal(runAgentActionCalls.length, 0, "must never silently fall back to the other runnable agent");
});

test("happy path still works when isAgentInstalled is explicitly stubbed true", async () => {
  const { runAgentActionCalls, deps: rec } = recordingDeps({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true" }),
    isAgentInstalled: () => true,
  });
  const { panel } = openWelcome(rec);

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: "antigravity" });
  await flush();

  assert.equal(runAgentActionCalls.length, 1);
  assert.equal(ctaResults(panel)[0].ok, true);
});

test("happy path (single authorized, explicit agentId): launches, copies the exact new-build prompt, shows the info message", async () => {
  const { runAgentActionCalls, clipboardWrites, infoMessages, deps: rec } = recordingDeps({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true" }),
  });
  const { panel } = openWelcome(rec);

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: "antigravity" });
  await flush();

  assert.equal(runAgentActionCalls.length, 1);
  assert.equal(runAgentActionCalls[0].agent, TEST_REGISTRY["antigravity"]);
  assert.equal(runAgentActionCalls[0].mode, TEST_REGISTRY["antigravity"].opens[0].mode);
  assert.deepStrictEqual(clipboardWrites, [CTA_PROMPT_NEW]);
  assert.equal(infoMessages.length, 1);
  assert.match(infoMessages[0], /Antigravity/);
  assert.equal(infoMessages[0], "Kickoff prompt copied — paste it into Antigravity to start.");
  assert.deepStrictEqual(ctaResults(panel), [{ type: "cta-result", ok: true, message: infoMessages[0] }]);
});

test("happy path (single authorized, agentId omitted): still resolves implicitly and launches", async () => {
  const { runAgentActionCalls, deps: rec } = recordingDeps({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true" }),
  });
  const { panel } = openWelcome(rec);

  panel.webview.__fireMessage({ type: "start-onboarding", path: "existing" }); // agentId omitted
  await flush();

  assert.equal(runAgentActionCalls.length, 1);
  assert.equal(runAgentActionCalls[0].agent.id, "antigravity");
  assert.equal(ctaResults(panel)[0].ok, true);
});

test("the existing-app path copies the exact existing-app prompt", async () => {
  const { clipboardWrites, deps: rec } = recordingDeps({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true" }),
  });
  const { panel } = openWelcome(rec);

  panel.webview.__fireMessage({ type: "start-onboarding", path: "existing", agentId: "antigravity" });
  await flush();

  assert.deepStrictEqual(clipboardWrites, [CTA_PROMPT_EXISTING]);
});

test("a clipboard write failure still launches the agent but reports the copy failure honestly", async () => {
  const runAgentActionCalls = [];
  const { panel } = openWelcome({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true" }),
    runAgentAction: (agent, mode) => runAgentActionCalls.push({ agent, mode }),
    clipboard: { writeText: async () => { throw new Error("denied"); } },
    showInformationMessage: () => {},
  });

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: "antigravity" });
  await flush();

  assert.equal(runAgentActionCalls.length, 1, "the agent still launches even if the clipboard write fails");
  const results = ctaResults(panel);
  assert.equal(results.length, 1);
  assert.equal(results[0].ok, false);
  assert.match(results[0].message, /clipboard/i);
});

test("pin: the CTA path never uses terminal.sendText or deps.setTimeout", async () => {
  const timers = makeFakeTimers();
  const { runAgentActionCalls, deps: rec } = recordingDeps({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true" }),
    setTimeout: timers.setTimeout,
    clearTimeout: timers.clearTimeout,
  });
  const { panel, stub } = openWelcome(rec);

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: "antigravity" });
  await flush();

  assert.equal(runAgentActionCalls.length, 1, "sanity: the flow actually ran");
  assert.equal(stub.terminals.length, 0, "the CTA path must never create a terminal itself (sendText requires one)");
  assert.equal(timers.calls.length, 0, "the CTA path must never call deps.setTimeout");
});

test("the panel stays open after a start-onboarding click regardless of outcome", async () => {
  const { deps: rec } = recordingDeps({
    readZembedEnv: () => ({ ZCP_AGENT_OAUTH_ANTIGRAVITY: "true" }),
  });
  const { panel } = openWelcome(rec);

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new", agentId: "antigravity" }); // succeeds
  await flush();
  assert.equal(panel.disposed, false, "a successful CTA must not dispose the panel");

  panel.webview.__fireMessage({ type: "start-onboarding", path: "new" }); // now ambiguous-free but re-launch is fine too
  await flush();
  assert.equal(panel.disposed, false, "a repeat CTA click must not dispose the panel either");
});
