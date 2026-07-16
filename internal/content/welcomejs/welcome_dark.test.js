"use strict";

// W3 (docs/spec-welcome-mode.md §1, W-ENTRY): welcome.js must never load —
// and no "zeropsWelcome" panel must ever exist — before the zerops.welcome
// command actually runs; a broken welcome.js must not take the launcher
// down with it. This is the executable proof behind the Go source-level pin
// TestBootstrapExtension_WelcomeLazyPins.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { loadExtension } = require("./harness.js");

function newCtx(extensionDir) {
  return { subscriptions: [], extensionPath: extensionDir };
}

test("activation registers the launcher view but loads no welcome module and opens no welcome panel", async () => {
  const { stub, extension, extensionDir } = loadExtension();

  await extension.activate(newCtx(extensionDir));

  const welcomeJsPath = require.resolve(path.join(extensionDir, "welcome.js"));
  assert.equal(welcomeJsPath in require.cache, false, "welcome.js must not be loaded at activation");
  assert.equal(stub.panels.some((p) => p.viewType === "zeropsWelcome"), false, "no zeropsWelcome panel at activation");
  assert.equal(stub.registeredViews.length, 1, "the zcpAgents launcher view must still register");
});

test("running the zerops.welcome command loads welcome.js exactly once and opens the panel", async () => {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate(newCtx(extensionDir));
  const handler = stub.registeredCommands.get("zerops.welcome");
  assert.ok(handler, "zerops.welcome must be registered");

  handler();

  const welcomeJsPath = require.resolve(path.join(extensionDir, "welcome.js"));
  assert.equal(welcomeJsPath in require.cache, true, "welcome.js must be loaded once the command runs");
  const panels = stub.panels.filter((p) => p.viewType === "zeropsWelcome");
  assert.equal(panels.length, 1, "exactly one welcome panel must be created");
});

test("a second command run reveals the same panel instead of recreating it", async () => {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate(newCtx(extensionDir));
  const handler = stub.registeredCommands.get("zerops.welcome");

  handler();
  handler();

  const panels = stub.panels.filter((p) => p.viewType === "zeropsWelcome");
  assert.equal(panels.length, 1, "second invocation must not create a new panel");
  assert.equal(panels[0].revealCount, 1, "second invocation must reveal the existing panel");
});

test("a broken welcome.js reports an error and leaves the launcher healthy", async () => {
  const { stub, extension, extensionDir } = loadExtension();
  await extension.activate(newCtx(extensionDir));
  const handler = stub.registeredCommands.get("zerops.welcome");

  fs.writeFileSync(path.join(extensionDir, "welcome.js"), "throw new Error('boom');\n");

  assert.doesNotThrow(() => handler(), "a broken welcome.js must not throw out of the command handler");
  assert.equal(stub.errorMessages.length, 1, "showErrorMessage must report the failure");
  assert.equal(stub.panels.some((p) => p.viewType === "zeropsWelcome"), false, "no panel from a failed open");
  assert.equal(stub.registeredViews.length, 1, "the launcher view must still be registered");
});
