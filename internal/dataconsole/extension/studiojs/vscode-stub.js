"use strict";

// Minimal `vscode` API stub + a module-resolution hook, so the Studio extension
// (which does require("vscode")) can be unit-tested with plain `node`, no live
// VS Code. Require THIS file before requiring the extension under test.
//
// This is test-only scaffolding and lives OUTSIDE the shipped extension tree
// (internal/dataconsole/extension/templates/vscode-studio/), so it is never
// embedded or materialized into a user's editor.

const Module = require("module");
const origResolve = Module._resolveFilename;
Module._resolveFilename = function (request, ...rest) {
  if (request === "vscode") return require.resolve("./vscode-stub.js");
  return origResolve.call(this, request, ...rest);
};

const warningResults = [];
const warningMessages = [];
const commands = [];
const outputChannels = [];

function defaultAsExternalUri(uri) {
  return Promise.resolve(uri);
}

function defaultOpenExternal() {
  return Promise.resolve(true);
}

const vscode = {
  __warningMessages: warningMessages,
  __commands: commands,
  __outputChannels: outputChannels,
  __pushWarningMessageResult: function (result) {
    warningResults.push(result);
  },
  __reset: function () {
    warningResults.length = 0;
    warningMessages.length = 0;
    commands.length = 0;
    outputChannels.length = 0;
    vscode.env.asExternalUri = defaultAsExternalUri;
    vscode.env.openExternal = defaultOpenExternal;
  },
  ViewColumn: { One: 1 },
  Uri: {
    parse: (s) => ({ toString: () => s }),
    file: (p) => ({ fsPath: p, toString: () => "file://" + p }),
  },
  window: {
    registerWebviewViewProvider: () => ({ dispose() {} }),
    createOutputChannel: (name) => {
      const channel = {
        name: name,
        lines: [],
        appendLine(line) {
          this.lines.push(String(line));
        },
        dispose() {},
      };
      outputChannels.push(channel);
      return channel;
    },
    createWebviewPanel: () => ({
      webview: { html: "", onDidReceiveMessage() {}, postMessage() {} },
      onDidDispose() {},
      dispose() {},
    }),
    showWarningMessage: function () {
      warningMessages.push(Array.from(arguments));
      return Promise.resolve(warningResults.length ? warningResults.shift() : undefined);
    },
  },
  commands: {
    executeCommand: function (command, arg) {
      commands.push({ command: command, arg: arg });
      return Promise.resolve();
    },
  },
  env: {
    asExternalUri: defaultAsExternalUri,
    openExternal: defaultOpenExternal,
  },
  workspace: { workspaceFolders: [{ uri: { fsPath: process.cwd() } }] },
};

module.exports = vscode;
