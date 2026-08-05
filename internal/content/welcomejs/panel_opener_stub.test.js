"use strict";

// Agent-first activity-bar entry (docs/spec-welcome-mode.md §1.4): under
// agent-first mode the activity-bar container swaps its visible view from the
// legacy zcpAgents launcher (gated "!zcpAgentFirst") to zcpPanelOpener (gated
// "zcpAgentFirst") — package.json's when-clauses make exactly one of the two
// ever visible, so the Zerops icon itself never disappears in either mode.
// zcpPanelOpener is a minimal stub view (VS Code cannot open an editor panel
// from a bare activity-bar item, vscode#149556 — a view container must
// contain a view): on every visible=true transition it forwards to the
// zerops.panel command AS A MANUAL invocation (no opts — matching a real
// Command Palette call, see welcome.js's `manual = !opts || opts.manual !==
// false`) and collapses the sidebar, behind a single-flight guard — the same
// pattern as the Studio extension's own activity-bar stub
// (internal/dataconsole/extension/templates/vscode-studio/extension.js's
// createStubViewProvider), adapted locally rather than imported across
// extensions.

const test = require("node:test");
const assert = require("node:assert/strict");
const { loadExtension } = require("./harness.js");

function flush() {
  return new Promise((resolve) => setImmediate(resolve));
}

function fakeView(initialVisible) {
  return {
    visible: initialVisible !== false,
    webview: { options: null, html: "" },
    _onVisibility: null,
    onDidChangeVisibility(fn) {
      this._onVisibility = fn;
    },
    fireVisibility() {
      if (this._onVisibility) this._onVisibility();
    },
  };
}

function newCtx(extensionDir) {
  return { subscriptions: [], extensionPath: extensionDir };
}

function panelOpenerProvider(stub) {
  const entry = stub.registeredViews.find((v) => v.id === "zcpPanelOpener");
  assert.ok(entry, "zcpPanelOpener view provider must be registered");
  return entry.provider;
}

test("zcpPanelOpener is registered alongside the legacy zcpAgents view, unconditionally", async () => {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate(newCtx(extensionDir));

  const ids = stub.registeredViews.map((v) => v.id).sort();
  assert.deepEqual(ids, ["zcpAgents", "zcpPanelOpener"]);
});

test("visible=true forwards to zerops.panel with no opts (manual), then collapses the sidebar", async () => {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate(newCtx(extensionDir));
  const provider = panelOpenerProvider(stub);

  const view = fakeView(true); // VS Code cannot pre-fire hidden (vscode#152382) — view starts visible
  provider.resolveWebviewView(view);
  await flush();

  assert.deepEqual(
    stub.executedCommands.map((c) => ({ id: c.id, args: c.args })),
    [
      { id: "zerops.panel", args: [] },
      { id: "workbench.action.closeSidebar", args: [] },
    ],
    "the first reveal forwards to zerops.panel with no opts (manual invocation), then collapses the sidebar, in that order"
  );
});

test("a click storm while the open is in flight fires zerops.panel exactly once", async () => {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate(newCtx(extensionDir));
  const provider = panelOpenerProvider(stub);

  const view = fakeView(true);
  provider.resolveWebviewView(view); // first fire — stays "in flight" until microtasks settle
  view.fireVisibility();
  view.fireVisibility();
  view.fireVisibility();
  await flush();

  const panelCalls = stub.executedCommands.filter((c) => c.id === "zerops.panel");
  assert.equal(panelCalls.length, 1, "a click storm while the open is in flight still opens exactly one panel");
  const collapseCalls = stub.executedCommands.filter((c) => c.id === "workbench.action.closeSidebar");
  assert.equal(collapseCalls.length, 1, "the sidebar collapses exactly once for the whole storm");
});

test("after settling, a later visibility change fires zerops.panel again", async () => {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate(newCtx(extensionDir));
  const provider = panelOpenerProvider(stub);

  const view = fakeView(true);
  provider.resolveWebviewView(view);
  await flush();
  stub.executedCommands.length = 0;

  view.visible = true;
  view.fireVisibility();
  await flush();

  const panelCalls = stub.executedCommands.filter((c) => c.id === "zerops.panel");
  assert.equal(panelCalls.length, 1, "the single-flight guard resets after settling — a later reveal fires again");
});
