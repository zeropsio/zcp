"use strict";

// Extension activation contract (docs/spec-dataconsole.md §4.4 — entry points).
// extension.js's activate() wires THREE things through one shared session
// manager: the zcpStudio.open / zcpStudio.openService commands, and the
// activity-bar stub view. createActivation(deps) takes an injected `vscode` +
// `createConsoleSessionManager` so this exercises the real extension.js logic
// without a live VS Code (and without touching the shared studiojs vscode-stub,
// which only fakes the small surface the OLD sidebar provider needed).

const assert = require("assert");
const {
  createActivation,
  VIEW_ID,
  OPEN_COMMAND,
  OPEN_SERVICE_COMMAND,
} = require("../templates/vscode-studio/extension");

function flush() {
  return new Promise(function (resolve) { setImmediate(resolve); });
}

function fakeVscode() {
  const registered = new Map(); // command name -> handler
  const executed = []; // { command, arg }
  const errorMessages = [];
  const views = new Map(); // viewId -> provider
  return {
    __registered: registered,
    __executed: executed,
    __errorMessages: errorMessages,
    __views: views,
    workspace: { workspaceFolders: [{ uri: { fsPath: "/work" } }] },
    commands: {
      registerCommand: function (name, fn) {
        registered.set(name, fn);
        return { dispose: function () { registered.delete(name); } };
      },
      executeCommand: function (name, arg) {
        executed.push({ command: name, arg: arg });
        const fn = registered.get(name);
        if (!fn) return Promise.resolve(); // an unregistered (built-in) command just succeeds
        return Promise.resolve(fn(arg));
      },
    },
    window: {
      registerWebviewViewProvider: function (id, provider) {
        views.set(id, provider);
        return { dispose: function () { views.delete(id); } };
      },
      showErrorMessage: function () {
        errorMessages.push(Array.from(arguments));
        return Promise.resolve();
      },
    },
  };
}

function fakeView(initialVisible) {
  return {
    visible: initialVisible !== false,
    _onVisibility: null,
    webview: { options: null, html: "" },
    onDidChangeVisibility: function (fn) { this._onVisibility = fn; },
    fireVisibility: function () { if (this._onVisibility) this._onVisibility(); },
  };
}

function fakeCtx() {
  return { subscriptions: [], extensionPath: "/ext" };
}

// fakeMgrFactory stands in for consoleSession's createConsoleSessionManager —
// it records every open() call and lets a test control what open() resolves
// to (a controllable pending promise drives the click-storm test).
function fakeMgrFactory(openImpl) {
  const opens = [];
  let disposeCalls = 0;
  let factoryCalls = 0;
  const factory = function () {
    factoryCalls++;
    return {
      open: function (opts) {
        opens.push(opts);
        return openImpl ? openImpl(opts) : Promise.resolve();
      },
      dispose: function () { disposeCalls++; },
    };
  };
  factory.__opens = opens;
  factory.__disposeCalls = function () { return disposeCalls; };
  factory.__factoryCalls = function () { return factoryCalls; };
  return factory;
}

function testActivateRegistersCommandsAndStubView() {
  const vscode = fakeVscode();
  const mgrFactory = fakeMgrFactory();
  const activation = createActivation({ vscode: vscode, createConsoleSessionManager: mgrFactory });
  const ctx = fakeCtx();
  activation.activate(ctx);

  assert.ok(vscode.__registered.has(OPEN_COMMAND), "zcpStudio.open command registered");
  assert.ok(vscode.__registered.has(OPEN_SERVICE_COMMAND), "zcpStudio.openService command registered");
  assert.ok(vscode.__views.has(VIEW_ID), "the stub view provider is registered for the activity-bar view");
  assert.strictEqual(mgrFactory.__factoryCalls(), 1, "activate creates exactly ONE console session manager — the singleton every entry point shares");

  // Deactivation tears down the session manager (no console child outlives the editor).
  assert.ok(ctx.subscriptions.length > 0, "activate registers at least one disposable");
  ctx.subscriptions.forEach(function (d) { if (d && typeof d.dispose === "function") d.dispose(); });
  assert.strictEqual(mgrFactory.__disposeCalls(), 1, "disposing the extension's subscriptions disposes the session manager exactly once");
}

async function testOpenCommand_NoTarget_OpensWithEmptyService() {
  const vscode = fakeVscode();
  const mgrFactory = fakeMgrFactory();
  const activation = createActivation({ vscode: vscode, createConsoleSessionManager: mgrFactory });
  activation.activate(fakeCtx());

  await vscode.commands.executeCommand(OPEN_COMMAND);

  assert.strictEqual(mgrFactory.__opens.length, 1, "zcpStudio.open calls mgr.open exactly once");
  assert.strictEqual(mgrFactory.__opens[0].service, "", "no-target open resolves to an empty service — the rail picks the last/first browsable one");
  assert.strictEqual(mgrFactory.__opens[0].extensionPath, "/ext", "open carries the extension path through for the media dir");
  assert.strictEqual(vscode.__errorMessages.length, 0, "no error dialog");
}

async function testOpenServiceCommand_MissingHostname_NoTargetNoErrorDialog() {
  const vscode = fakeVscode();
  const mgrFactory = fakeMgrFactory();
  const activation = createActivation({ vscode: vscode, createConsoleSessionManager: mgrFactory });
  activation.activate(fakeCtx());

  await vscode.commands.executeCommand(OPEN_SERVICE_COMMAND); // no argument at all
  await vscode.commands.executeCommand(OPEN_SERVICE_COMMAND, null);
  await vscode.commands.executeCommand(OPEN_SERVICE_COMMAND, 42);

  assert.strictEqual(mgrFactory.__opens.length, 3, "every invocation reaches mgr.open");
  mgrFactory.__opens.forEach(function (o, i) {
    assert.strictEqual(o.service, "", "invocation " + i + ": a missing/non-string hostname resolves to no-target");
  });
  assert.strictEqual(vscode.__errorMessages.length, 0, "a missing/malformed hostname never pops an error dialog");
}

async function testOpenServiceCommand_UnknownHostname_PassesThroughAsDeepLink() {
  const vscode = fakeVscode();
  const mgrFactory = fakeMgrFactory();
  const activation = createActivation({ vscode: vscode, createConsoleSessionManager: mgrFactory });
  activation.activate(fakeCtx());

  await vscode.commands.executeCommand(OPEN_SERVICE_COMMAND, "ghost-service");

  assert.strictEqual(
    mgrFactory.__opens[0].service,
    "ghost-service",
    "an unrecognized hostname passes through unvalidated — only the rail's live /api/services can tell unknown from real"
  );
  assert.strictEqual(vscode.__errorMessages.length, 0, "an unknown hostname never pops an error dialog either — the rail keeps it pending");
}

async function testStubView_FirstReveal_ForwardsThenCollapses() {
  const vscode = fakeVscode();
  const mgrFactory = fakeMgrFactory();
  const activation = createActivation({ vscode: vscode, createConsoleSessionManager: mgrFactory });
  activation.activate(fakeCtx());

  const provider = vscode.__views.get(VIEW_ID);
  const view = fakeView(true);
  provider.resolveWebviewView(view); // VS Code cannot pre-fire hidden (vscode#152382) — view starts visible
  await flush();

  assert.deepStrictEqual(
    vscode.__executed.map(function (e) { return e.command; }),
    [OPEN_COMMAND, "workbench.action.closeSidebar"],
    "the first reveal forwards to zcpStudio.open, then collapses the sidebar, in that order"
  );
  assert.strictEqual(mgrFactory.__opens.length, 1);
}

async function testStubView_RevealAfterCollapse_FiresAgain() {
  const vscode = fakeVscode();
  const mgrFactory = fakeMgrFactory();
  const activation = createActivation({ vscode: vscode, createConsoleSessionManager: mgrFactory });
  activation.activate(fakeCtx());

  const provider = vscode.__views.get(VIEW_ID);
  const view = fakeView(true);
  provider.resolveWebviewView(view); // first reveal
  await flush();
  vscode.__executed.length = 0;

  // The stub was collapsed by the first reveal; VS Code does NOT call
  // resolveWebviewView again for a later click on the same activity-bar item —
  // it merely fires onDidChangeVisibility on the SAME already-resolved view.
  view.visible = true;
  view.fireVisibility();
  await flush();

  assert.deepStrictEqual(
    vscode.__executed.map(function (e) { return e.command; }),
    [OPEN_COMMAND, "workbench.action.closeSidebar"],
    "a later reveal of the already-resolved stub forwards and collapses again — not only the first resolveWebviewView"
  );
  assert.strictEqual(mgrFactory.__opens.length, 2, "each separate reveal reaches the entry funnel (panel already open just reveals it — consolePanel's job)");
}

async function testStubView_SingleFlightUnderClickStorm() {
  const vscode = fakeVscode();
  let resolveOpen;
  const pending = new Promise(function (resolve) { resolveOpen = resolve; });
  const mgrFactory = fakeMgrFactory(function () { return pending; });
  const activation = createActivation({ vscode: vscode, createConsoleSessionManager: mgrFactory });
  activation.activate(fakeCtx());

  const provider = vscode.__views.get(VIEW_ID);
  const view = fakeView(true);
  provider.resolveWebviewView(view); // fires the first open — stays in flight

  // Click storm: the sidebar icon fires repeat visibility transitions while
  // the first open() has not settled yet.
  view.fireVisibility();
  view.fireVisibility();
  view.fireVisibility();
  await flush();

  assert.strictEqual(mgrFactory.__opens.length, 1, "a click storm while the open is in flight still opens exactly one panel");
  assert.strictEqual(
    vscode.__executed.filter(function (e) { return e.command === "workbench.action.closeSidebar"; }).length,
    0,
    "collapse has not run yet — the in-flight open has not settled"
  );

  resolveOpen();
  await flush();
  assert.strictEqual(
    vscode.__executed.filter(function (e) { return e.command === "workbench.action.closeSidebar"; }).length,
    1,
    "collapse runs once the in-flight open settles"
  );

  // The single-flight guard resets after settling — a later click opens again.
  view.fireVisibility();
  await flush();
  assert.strictEqual(mgrFactory.__opens.length, 2, "the guard resets after settling — a later click opens again");
}

(async function main() {
  testActivateRegistersCommandsAndStubView();
  await testOpenCommand_NoTarget_OpensWithEmptyService();
  await testOpenServiceCommand_MissingHostname_NoTargetNoErrorDialog();
  await testOpenServiceCommand_UnknownHostname_PassesThroughAsDeepLink();
  await testStubView_FirstReveal_ForwardsThenCollapses();
  await testStubView_RevealAfterCollapse_FiresAgain();
  await testStubView_SingleFlightUnderClickStorm();
  console.log("stub_view.test.js OK");
})().catch(function (e) {
  console.error(e && e.stack ? e.stack : e);
  process.exit(1);
});
