"use strict";

// A fresh, isolated stand-in for the "vscode" module the zcp-bootstrap
// extension.js and welcome.js require. Every side effect the real API would
// cause is RECORDED instead, so tests can assert on it without a real VS
// Code host. createVscodeStub() is a FACTORY (not a singleton export) so
// each test gets its own state — module-level singletons in the extension
// under test (the launcher panel, the welcome panel) must never leak
// between test cases just because they'd otherwise share one stub.

function makePanel(state, viewType, title, viewColumn, options, preserveFocus) {
  let disposed = false;
  let onMessage = null;
  let onDispose = null;
  let onViewState = null;

  const panel = {
    viewType,
    title,
    viewColumn,
    options,
    // Test-only visibility into whether creation stole focus (not part of
    // the real vscode.WebviewPanel API — real VS Code only exposes this as a
    // side effect on the editor group, which this stub does not model).
    preserveFocus: !!preserveFocus,
    revealCount: 0,
    visible: true, // a freshly created/shown panel starts visible, matching real VS Code
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
    onDidChangeViewState(cb) {
      onViewState = cb;
      return { dispose() { onViewState = null; } };
    },
    reveal() {
      panel.revealCount++;
      panel.visible = true;
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      if (onDispose) onDispose();
    },
    get disposed() {
      return disposed;
    },
    // Test-only helper (not part of the real vscode.WebviewPanel API):
    // simulates VS Code firing onDidChangeViewState, e.g. the user switching
    // tabs back to this panel WITHOUT re-invoking the command.
    __setVisible(visible) {
      panel.visible = visible;
      if (onViewState) onViewState({ webviewPanel: panel });
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
    closeTerminalListeners: [], // callbacks registered via window.onDidCloseTerminal
    outputChannels: [], // every channel createOutputChannel has returned, in creation order
    clipboardWrites: [], // every string passed to env.clipboard.writeText
    infoMessages: [], // every string passed to window.showInformationMessage
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
      // showOptions is either a plain ViewColumn (the historical call shape)
      // or { viewColumn, preserveFocus } (real VS Code supports both) — the
      // stub normalizes either into a plain panel.viewColumn, matching real
      // VS Code's WebviewPanel (which always reports a resolved column, never
      // the showOptions object it was opened with).
      createWebviewPanel: (viewType, title, showOptions, options) => {
        const isObjectForm = showOptions && typeof showOptions === "object";
        const resolvedColumn = isObjectForm ? showOptions.viewColumn : showOptions;
        const preserveFocus = isObjectForm && !!showOptions.preserveFocus;
        return makePanel(state, viewType, title, resolvedColumn, options, preserveFocus);
      },
      createTerminal: (opts) => {
        const term = {
          name: opts && opts.name,
          sent: [], // {text, addNewLine} per sendText() call, in order
          shownCount: 0,
          sendText: (text, addNewLine) => { term.sent.push({ text, addNewLine }); },
          show: () => { term.shownCount++; },
        };
        state.terminals.push(term);
        return term;
      },
      onDidCloseTerminal: (cb) => {
        state.closeTerminalListeners.push(cb);
        return {
          dispose: () => {
            const i = state.closeTerminalListeners.indexOf(cb);
            if (i >= 0) state.closeTerminalListeners.splice(i, 1);
          },
        };
      },
      // Test-only helper (not part of the real vscode.window API): simulates
      // VS Code firing onDidCloseTerminal for the given terminal.
      __fireCloseTerminal: (term) => {
        for (const cb of state.closeTerminalListeners.slice()) cb(term);
      },
      registerWebviewViewProvider: (id, provider) => {
        state.registeredViews.push({ id, provider });
        return { dispose() {} };
      },
      showErrorMessage: (msg) => {
        state.errorMessages.push(msg);
        return Promise.resolve(undefined);
      },
      createOutputChannel: (name) => {
        const channel = {
          name,
          lines: [], // every appendLine()/append() call, in order
          shownCount: 0,
          disposedCount: 0,
          appendLine: (line) => { channel.lines.push(line); },
          append: (text) => { channel.lines.push(text); },
          show: () => { channel.shownCount++; },
          dispose: () => { channel.disposedCount++; },
        };
        state.outputChannels.push(channel);
        return channel;
      },
      // Real VS Code resolves to the picked item, or undefined on Escape/
      // blur. welcome.js's guided-toggle flow always injects deps.showQuickPick
      // for tests that reach it (multi-root); this default only guards
      // against an un-injected call falling through to something crashy.
      showQuickPick: (_items, _options) => Promise.resolve(undefined),
      // Same treatment for the skills-install modal confirmation: real VS
      // Code resolves to the clicked item's string, or undefined on
      // dismiss/Escape. welcome.js's handleSkillAdd always injects
      // deps.showWarningMessage for tests that reach the "locally modified"
      // branch; this default only guards an un-injected call.
      showWarningMessage: (_message, _options, ..._items) => Promise.resolve(undefined),
      // The CTA's post-clipboard-copy nudge (docs/spec-welcome-mode.md §7
      // W-CTA) — real VS Code resolves to the clicked item's string, or
      // undefined if the user takes no action on the toast; welcome.js
      // never inspects the resolved value, so a fire-and-forget resolve is
      // enough here.
      showInformationMessage: (message, ..._items) => {
        state.infoMessages.push(message);
        return Promise.resolve(undefined);
      },
    },
    env: {
      openExternal: (uri) => {
        state.openedExternalUrls.push(uri && uri.toString ? uri.toString() : String(uri));
        return Promise.resolve(true);
      },
      // Clipboard-first CTA kickoff (spec §7 W-CTA): the ONLY mechanism
      // welcome.js may use to hand a kickoff prompt to the agent — never
      // terminal.sendText, never a delayed injection.
      clipboard: {
        writeText: (text) => {
          state.clipboardWrites.push(text);
          return Promise.resolve();
        },
      },
    },
  };

  return Object.assign({ exports }, state);
}

module.exports = { createVscodeStub };
