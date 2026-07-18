"use strict";

// Zerops Managed Data activation shell.
//
// Durable seams stay directory-discovered: cards/ renders the webview surface,
// handlers/ is the webview->host allowlist, and the session owns async
// transport, refresh, dispatch, rendering, and view disposal.

const vscode = require("vscode");
const path = require("path");
const { enumerateCards } = require("./lib/cards");
const { enumerateHandlers, buildRouter } = require("./lib/handlers");
const { runStudioVerb } = require("./lib/transport");
const {
  createWebviewSession,
  runTransport,
  renderShell,
  renderCTA,
  makeNonce,
} = require("./lib/webviewSession");

const VIEW_ID = "zcpStudioView";

function appendLine(outputChannel, line) {
  if (outputChannel && typeof outputChannel.appendLine === "function") {
    outputChannel.appendLine(line);
  }
}

function createOutputChannel(ctx) {
  if (!vscode.window || typeof vscode.window.createOutputChannel !== "function") {
    return null;
  }
  const channel = vscode.window.createOutputChannel("Zerops Managed Data");
  if (ctx && ctx.subscriptions && channel && typeof channel.dispose === "function") {
    ctx.subscriptions.push(channel);
  }
  return channel;
}

function workspaceRoot() {
  const folders = vscode.workspace.workspaceFolders;
  return folders && folders[0] ? folders[0].uri.fsPath : process.cwd();
}

function activate(ctx) {
  const outputChannel = createOutputChannel(ctx);
  const provider = {
    resolveWebviewView(view) {
      view.webview.options = { enableScripts: true };
      const extDir = ctx.extensionPath;

      let cards = [];
      let router = { allow: new Set(), dispatch: async function () { return false; } };
      try {
        cards = enumerateCards(path.join(extDir, "cards"));
        router = buildRouter(enumerateHandlers(path.join(extDir, "handlers")));
      } catch (err) {
        appendLine(
          outputChannel,
          "Studio discovery failed: " + (err && err.stack ? err.stack : err && err.message ? err.message : String(err))
        );
        view.webview.html = renderCTA({ error: "Studio failed to load: " + (err && err.message) }, makeNonce());
        return;
      }

      // Tear down handler-owned resources (the console handler's child processes)
      // when the extension deactivates — no console server outlives the editor.
      if (typeof router.dispose === "function") {
        ctx.subscriptions.push({ dispose: function () { router.dispose(); } });
      }

      createWebviewSession({
        view: view,
        cards: cards,
        router: router,
        workspaceRoot: workspaceRoot(),
        extensionPath: extDir,
        outputChannel: outputChannel,
        runStudioVerb: runStudioVerb,
      }).start();
    },
  };

  ctx.subscriptions.push(vscode.window.registerWebviewViewProvider(VIEW_ID, provider));
}

function deactivate() {}

module.exports = {
  activate: activate,
  deactivate: deactivate,
  runStudioVerb: runStudioVerb,
  runTransport: runTransport,
  renderShell: renderShell,
  renderCTA: renderCTA,
  VIEW_ID: VIEW_ID,
};
