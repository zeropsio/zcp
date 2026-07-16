"use strict";

// The ONE Data Console session manager, shared across handlers. Both the embedded
// opener (handlers/console.js) and the standalone browser opener
// (handlers/open-console-browser.js) resolve THIS instance, so they key into the
// same per-workspace console process — opening one after the other reuses the
// running server (its url + tokens) instead of spawning a second competing one.
//
// createConsoleSessionManager() eager-requires "vscode", so the instance is created
// LAZILY on first sessionManager() call (never at module load). That keeps the
// handler modules — which require this file at top level — loadable under plain node
// for their pure-logic tests, where "vscode" does not resolve. node's require cache
// makes the resolved instance a process-wide singleton.

const { createConsoleSessionManager } = require("./consoleSession");

let instance = null;

function sessionManager() {
  if (!instance) instance = createConsoleSessionManager();
  return instance;
}

// disposeShared tears down the shared manager if it was ever created (the console
// handler wires this to extension deactivate). A no-op when no console ever opened,
// so it never creates a manager just to dispose it.
sessionManager.disposeShared = function () {
  if (instance) {
    instance.dispose();
    instance = null;
  }
};

module.exports = sessionManager;
