"use strict";

// Zerops Managed Data activation shell.
//
// The Data Console is one singleton WebviewPanel per workspace
// (docs/spec-dataconsole.md §4.4). Every entry point funnels into the SAME
// per-workspace session manager (lib/consoleSession.js), which owns the
// console child process and the panel (lib/consolePanel.js — the singleton
// reveal+switch entry funnel):
//
//   - the contributed commands zcpStudio.open (no target) and
//     zcpStudio.openService (hostname argument, the deep-link form);
//   - the activity-bar stub view below, which exists ONLY because VS Code
//     cannot open an editor panel from a bare activity-bar item
//     (vscode#149556 — a view container must contain a view). It forwards to
//     zcpStudio.open and collapses the sidebar on EVERY visible=true
//     transition, not only the first resolveWebviewView (a resolved stub is
//     merely re-revealed on later clicks), behind a single-flight guard so a
//     click storm opens exactly one panel. A brief view flash is unavoidable
//     (resolveWebviewView cannot pre-fire hidden, vscode#152382) — the
//     contract is "no *populated* sidebar", not "no view".
//
// createActivation(deps) takes an injected vscode + createConsoleSessionManager
// so this whole module is unit-testable with plain `node` — see
// studiojs/stub_view.test.js. The real activate()/deactivate() VS Code calls
// lazily resolve the real "vscode" module (and the real session manager) only
// when actually invoked, so merely requiring this file never touches "vscode".

const VIEW_ID = "zcpStudioView";
const OPEN_COMMAND = "zcpStudio.open";
const OPEN_SERVICE_COMMAND = "zcpStudio.openService";
const COLLAPSE_SIDEBAR_COMMAND = "workbench.action.closeSidebar";

function workspaceRoot(vscode) {
  const folders = vscode.workspace.workspaceFolders;
  return folders && folders[0] ? folders[0].uri.fsPath : process.cwd();
}

// createStubViewProvider builds the minimal activity-bar stub: a non-interactive
// webview view whose only job is forwarding to openCommand and collapsing the
// sidebar, on every visible=true transition, behind a single-flight guard.
function createStubViewProvider(vscode, openCommand) {
  let inFlight = false;

  function fire() {
    if (inFlight) return; // single-flight: a click storm opens exactly one panel
    inFlight = true;
    Promise.resolve()
      .then(function () {
        return vscode.commands.executeCommand(openCommand);
      })
      .then(function () {
        return vscode.commands.executeCommand(COLLAPSE_SIDEBAR_COMMAND);
      })
      .catch(function () {
        /* best effort — a failed open must never wedge the stub */
      })
      .then(function () {
        inFlight = false;
      });
  }

  return {
    resolveWebviewView: function (view) {
      view.webview.options = { enableScripts: false };
      view.webview.html = "<!DOCTYPE html><html><body></body></html>";
      if (typeof view.onDidChangeVisibility === "function") {
        view.onDidChangeVisibility(function () {
          if (view.visible) fire();
        });
      }
      if (view.visible !== false) fire();
    },
  };
}

// createActivation builds the extension's real activate/deactivate against
// injected deps, defaulting to the real "vscode" module + the real session
// manager. Only reached when activate()/deactivate() is actually called, so a
// plain `require()` of this file never touches "vscode".
function createActivation(deps) {
  deps = deps || {};
  const vscode = deps.vscode || require("vscode");
  const createConsoleSessionManager =
    deps.createConsoleSessionManager || require("./lib/consoleSession").createConsoleSessionManager;
  const resolveOpenTarget = deps.resolveOpenTarget || require("./lib/consolePanel").resolveOpenTarget;

  function activate(ctx) {
    const mgr = createConsoleSessionManager();
    ctx.subscriptions.push({
      dispose: function () {
        mgr.dispose();
      },
    });

    function open(service) {
      return mgr.open({
        workspaceRoot: workspaceRoot(vscode),
        extensionPath: ctx.extensionPath,
        service: service,
      });
    }

    ctx.subscriptions.push(
      vscode.commands.registerCommand(OPEN_COMMAND, function () {
        return open("");
      })
    );
    ctx.subscriptions.push(
      vscode.commands.registerCommand(OPEN_SERVICE_COMMAND, function (hostname) {
        return open(resolveOpenTarget(hostname));
      })
    );

    // Registered unconditionally — package.json's own "when": "!isWeb" on this
    // view is what hides the icon in code-server (the web workbench); desktop
    // VS Code (isWeb false) keeps it, since it has no Zerops panel of its own.
    ctx.subscriptions.push(
      vscode.window.registerWebviewViewProvider(VIEW_ID, createStubViewProvider(vscode, OPEN_COMMAND))
    );
  }

  function deactivate() {}

  return { activate: activate, deactivate: deactivate };
}

function activate(ctx) {
  return createActivation({}).activate(ctx);
}

function deactivate() {
  return createActivation({}).deactivate();
}

module.exports = {
  activate: activate,
  deactivate: deactivate,
  createActivation: createActivation,
  createStubViewProvider: createStubViewProvider,
  VIEW_ID: VIEW_ID,
  OPEN_COMMAND: OPEN_COMMAND,
  OPEN_SERVICE_COMMAND: OPEN_SERVICE_COMMAND,
};
