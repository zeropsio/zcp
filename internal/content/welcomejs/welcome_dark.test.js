"use strict";

// W3 (docs/spec-welcome-mode.md §1, W-ENTRY): default mode stays dark until
// the zerops.welcome command runs; custom-GUI mode invokes that same command
// on activation and suppresses the legacy launcher. A broken welcome.js must
// not take the launcher down with it. This is the executable proof behind the
// Go source-level pin TestBootstrapExtension_WelcomeLazyPins.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { loadExtension } = require("./harness.js");

const ZEMBED_ENV_PATH = path.join("/etc/zerops-zembed", "env.json");

function newCtx(extensionDir) {
  return { subscriptions: [], extensionPath: extensionDir };
}

function withZembedEnv(initialEnv) {
  let env = initialEnv;
  const original = fs.readFileSync;
  fs.readFileSync = (p, ...rest) => {
    if (p === ZEMBED_ENV_PATH) return JSON.stringify(env);
    return original(p, ...rest);
  };
  return {
    set(nextEnv) { env = nextEnv; },
    restore() { fs.readFileSync = original; },
  };
}

function dispatchRegisteredCommands(stub) {
  const recordCommand = stub.exports.commands.executeCommand;
  stub.exports.commands.executeCommand = async (id, ...args) => {
    await recordCommand(id, ...args);
    const handler = stub.registeredCommands.get(id);
    if (handler) return handler(...args);
    return undefined;
  };
}

function captureZembedWatchers() {
  const callbacks = [];
  const original = fs.watch;
  fs.watch = (target, listener, ...rest) => {
    if (target === ZEMBED_ENV_PATH) {
      callbacks.push(listener);
      return { close() {}, on() {}, unref() {} };
    }
    return original(target, listener, ...rest);
  };
  return {
    fireLauncherWatcher() {
      assert.ok(callbacks.length > 0, "activation must install the zembed watcher");
      callbacks[callbacks.length - 1]();
    },
    restore() { fs.watch = original; },
  };
}

test("custom GUI mode auto-opens welcome and closes the primary sidebar", async () => {
  const zembed = withZembedEnv({
    ZCP_WELCOME_BRIDGE_ORIGINS: "not a url, https://app.zerops.io, https://tatami.zerops.dev/embed",
  });
  try {
    const { stub, extension, extensionDir } = loadExtension();
    stub.exports.window.tabGroups.all = [{ tabs: [{}] }];
    dispatchRegisteredCommands(stub);

    await extension.activate(newCtx(extensionDir));

    assert.deepEqual(
      stub.executedCommands.map((command) => command.id),
      ["zerops.welcome", "workbench.action.closeSidebar"],
      "activation must invoke the canonical welcome command before closing Explorer"
    );
    assert.equal(
      stub.panels.filter((panel) => panel.viewType === "zeropsWelcome").length,
      1,
      "custom-GUI activation must open the singleton welcome panel"
    );
    assert.equal(
      stub.panels.some((panel) => panel.viewType === "zcpLauncher"),
      false,
      "the legacy launcher must not open before the custom-GUI welcome"
    );
  } finally {
    zembed.restore();
  }
});

test("custom GUI mode ignores later env changes instead of reopening launcher", async () => {
  const zembed = withZembedEnv({
    ZCP_WELCOME_BRIDGE_ORIGINS: "https://tatami.zerops.dev",
    ZCP_AGENTS: "claude-code",
  });
  const watchers = captureZembedWatchers();
  try {
    const { stub, extension, extensionDir } = loadExtension();
    dispatchRegisteredCommands(stub);
    await extension.activate(newCtx(extensionDir));

    zembed.set({ ZCP_AGENTS: "codex" });
    const originalSetTimeout = global.setTimeout;
    global.setTimeout = (callback) => {
      callback();
      return {};
    };
    try {
      watchers.fireLauncherWatcher();
    } finally {
      global.setTimeout = originalSetTimeout;
    }

    assert.equal(
      stub.panels.filter((panel) => panel.viewType === "zeropsWelcome").length,
      1,
      "the existing singleton welcome panel must remain open"
    );
    assert.equal(
      stub.panels.some((panel) => panel.viewType === "zcpLauncher"),
      false,
      "a later env update must not reopen the legacy launcher"
    );
  } finally {
    watchers.restore();
    zembed.restore();
  }
});

test("default mode stays lazy and preserves launcher/restored-editor behavior", async () => {
  const defaultCases = [
    ["unreadable store", undefined],
    ["missing key", {}],
    ["non-string value", { ZCP_WELCOME_BRIDGE_ORIGINS: ["https://tatami.zerops.dev"] }],
    ["empty value", { ZCP_WELCOME_BRIDGE_ORIGINS: "  " }],
    ["junk-only value", { ZCP_WELCOME_BRIDGE_ORIGINS: "not a url, ftp://example.com" }],
    ["app-only value", { ZCP_WELCOME_BRIDGE_ORIGINS: " https://app.zerops.io:443/dashboard " }],
  ];

  for (const [label, env] of defaultCases) {
    const zembed = withZembedEnv(env);
    try {
      const { stub, extension, extensionDir } = loadExtension();
      await extension.activate(newCtx(extensionDir));

      const welcomeJsPath = require.resolve(path.join(extensionDir, "welcome.js"));
      assert.equal(welcomeJsPath in require.cache, false, `${label}: welcome.js must stay lazy`);
      assert.equal(
        stub.panels.filter((panel) => panel.viewType === "zcpLauncher").length,
        1,
        `${label}: a fresh editor must preserve the legacy launcher`
      );
      assert.equal(
        stub.panels.some((panel) => panel.viewType === "zeropsWelcome"),
        false,
        `${label}: default mode must not auto-open welcome`
      );
      assert.deepEqual(stub.executedCommands, [], `${label}: default mode must not run startup commands`);
    } finally {
      zembed.restore();
    }
  }

  const zembed = withZembedEnv({
    ZCP_WELCOME_BRIDGE_ORIGINS: "https://app.zerops.io",
  });
  try {
    const { stub, extension, extensionDir } = loadExtension();
    stub.exports.window.tabGroups.all = [{ tabs: [{}] }];

    await extension.activate(newCtx(extensionDir));

    assert.equal(stub.panels.length, 0, "restored editor tabs must still suppress the legacy launcher");
    assert.deepEqual(stub.executedCommands, [], "restored default mode must not auto-open welcome");
  } finally {
    zembed.restore();
  }
});

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
