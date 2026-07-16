"use strict";

// A fresh, isolated stand-in for the "vscode" module the zcp-bootstrap
// extension.js and welcome.js require. Every side effect the real API would
// cause is RECORDED instead, so tests can assert on it without a real VS
// Code host. createVscodeStub() is a FACTORY (not a singleton export) so
// each test gets its own state — module-level singletons in the extension
// under test (the launcher panel, the welcome panel) must never leak
// between test cases just because they'd otherwise share one stub.

function makePanel(state, viewType, title, viewColumn, options) {
  let disposed = false;
  let onMessage = null;
  let onDispose = null;

  const panel = {
    viewType,
    title,
    viewColumn,
    options,
    revealCount: 0,
    postedMessages: [],
    webview: {
      html: "",
      onDidReceiveMessage(cb) {
        onMessage = cb;
        return { dispose() {} };
      },
      postMessage(msg) {
        panel.postedMessages.push(msg);
        return Promise.resolve(true);
      },
      // Test-only helper (not part of the real vscode.Webview API): simulates
      // the webview posting a message to the host.
      __fireMessage(msg) {
        if (onMessage) onMessage(msg);
      },
    },
    onDidDispose(cb) {
      onDispose = cb;
      return { dispose() {} };
    },
    reveal() {
      panel.revealCount++;
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      if (onDispose) onDispose();
    },
    get disposed() {
      return disposed;
    },
  };
  state.panels.push(panel);
  return panel;
}

function createVscodeStub() {
  const state = {
    registeredCommands: new Map(), // id -> handler
    executedCommands: [], // {id, args}
    errorMessages: [], // strings passed to window.showErrorMessage
    openedExternalUrls: [], // strings passed to env.openExternal
    panels: [], // every panel createWebviewPanel has returned, in creation order
    terminals: [], // every terminal createTerminal has returned
    registeredViews: [], // {id, provider} from registerWebviewViewProvider
  };

  const exports = {
    TerminalLocation: { Panel: 1 },
    ViewColumn: { One: 1 },
    Uri: { parse: (s) => ({ toString: () => s }) },
    extensions: {
      // No real anthropic.claude-code in this harness — extension.js's
      // legacy Claude-plugin fallback must degrade to a warning, not throw.
      getExtension: () => undefined,
    },
    commands: {
      registerCommand(id, handler) {
        state.registeredCommands.set(id, handler);
        return { dispose: () => state.registeredCommands.delete(id) };
      },
      executeCommand(id, ...args) {
        state.executedCommands.push({ id, args });
        return Promise.resolve();
      },
      getCommands: () => Promise.resolve(Array.from(state.registeredCommands.keys())),
    },
    window: {
      tabGroups: { all: [] }, // no editors open — matches a fresh container
      createWebviewPanel: (viewType, title, viewColumn, options) => makePanel(state, viewType, title, viewColumn, options),
      createTerminal: (opts) => {
        const term = { name: opts && opts.name, sent: [], sendText: (text) => term.sent.push(text), show: () => {} };
        state.terminals.push(term);
        return term;
      },
      registerWebviewViewProvider: (id, provider) => {
        state.registeredViews.push({ id, provider });
        return { dispose() {} };
      },
      showErrorMessage: (msg) => {
        state.errorMessages.push(msg);
        return Promise.resolve(undefined);
      },
    },
    env: {
      openExternal: (uri) => {
        state.openedExternalUrls.push(uri && uri.toString ? uri.toString() : String(uri));
        return Promise.resolve(true);
      },
    },
  };

  return Object.assign({ exports }, state);
}

module.exports = { createVscodeStub };
