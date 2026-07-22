"use strict";

// Single-model launcher behavior (extension.js): buildView folds availability
// (ZCP_AGENTS), installed (PATH probe), and auth (per-agent envs) into one
// agent list — no legacy filtering, no "zero agents -> auto-open Claude"
// fallback. See availability_detection.test.js for the direct
// resolveAvailableAgentIds/isAgentInstalled unit coverage this file
// complements behaviorally, by driving activate() with the vscode stub.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { loadExtension } = require("./harness.js");

const ZEMBED_ENV_PATH = path.join("/etc/zerops-zembed", "env.json");

// withZembedEnv monkeypatches the shared fs.readFileSync — "fs" is a Node
// core-module singleton, so this reaches the fresh extension.js copy
// loadExtension() just required too — so a test can control exactly what
// extension.js's own readZembedEnv() sees, WITHOUT writing to the real
// (root-owned, non-existent in this harness) /etc/zerops-zembed/env.json.
// `env === undefined` simulates an unreadable store (readZembedEnv() ->
// null); any other value is JSON-stringified as the store content.
function withZembedEnv(env) {
  const original = fs.readFileSync;
  fs.readFileSync = (p, ...rest) => {
    if (p === ZEMBED_ENV_PATH) {
      if (env === undefined) {
        const err = new Error("ENOENT: no such file or directory, open '" + p + "'");
        err.code = "ENOENT";
        throw err;
      }
      return JSON.stringify(env);
    }
    return original(p, ...rest);
  };
  return () => { fs.readFileSync = original; };
}

function newCtx(extensionDir) {
  return { subscriptions: [], extensionPath: extensionDir };
}

function launcherPanel(stub) {
  return stub.panels.find((p) => p.viewType === "zcpLauncher");
}

test("fresh store (no agent envs, no ZCP_AGENTS), no editors: the launcher opens listing all five, no Claude-plugin exec", async () => {
  const restoreEnv = withZembedEnv({});
  try {
    const { stub, extension, extensionDir } = loadExtension();
    await extension.activate(newCtx(extensionDir));

    const panels = stub.panels.filter((p) => p.viewType === "zcpLauncher");
    assert.equal(panels.length, 1, "the launcher panel must open with no editors present");
    for (const label of ["Claude Code", "Codex", "Antigravity", "Grok Build", "Cursor CLI"]) {
      assert.ok(panels[0].webview.html.includes(label), `expected the ${label} row in the launcher HTML`);
    }
    assert.equal(
      stub.executedCommands.some((c) => c.id === "claude-vscode.editor.open"),
      false,
      "initial open must never auto-run the Claude plugin command"
    );
  } finally {
    restoreEnv();
  }
});

test('store with ZCP_AGENTS "codex" shows only the Codex row', async () => {
  const restoreEnv = withZembedEnv({ ZCP_AGENTS: "codex" });
  try {
    const { stub, extension, extensionDir } = loadExtension();
    await extension.activate(newCtx(extensionDir));

    const html = launcherPanel(stub).webview.html;
    assert.ok(html.includes("Codex"));
    assert.ok(!html.includes("Claude Code"));
  } finally {
    restoreEnv();
  }
});

test("an explicit empty ZCP_AGENTS opens the launcher with the no-agents-enabled copy", async () => {
  const restoreEnv = withZembedEnv({ ZCP_AGENTS: "" });
  try {
    const { stub, extension, extensionDir } = loadExtension();
    await extension.activate(newCtx(extensionDir));

    const html = launcherPanel(stub).webview.html;
    assert.ok(html.includes("No coding agents are enabled for this container."));
  } finally {
    restoreEnv();
  }
});

test("a launch message for a NOT-installed agent does nothing", async () => {
  const emptyDir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-launcher-empty-"));
  const originalPath = process.env.PATH;
  process.env.PATH = emptyDir; // controlled PATH: guaranteed not to carry a real "codex" binary
  const restoreEnv = withZembedEnv({ ZCP_AGENTS: "codex", ZCP_AGENT_OAUTH_CODEX: "true" });
  try {
    const { stub, extension, extensionDir } = loadExtension();
    await extension.activate(newCtx(extensionDir));

    launcherPanel(stub).webview.__fireMessage({ type: "launch", id: "codex", mode: "terminal" });

    assert.equal(stub.terminals.length, 0, "codex is authorized but not installed — no terminal must be created");
  } finally {
    process.env.PATH = originalPath;
    restoreEnv();
  }
});

test("a launch message for an installed + authorized agent opens a terminal running its command", async () => {
  const binDir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-launcher-bin-"));
  fs.writeFileSync(path.join(binDir, "codex"), "#!/bin/sh\n");
  fs.chmodSync(path.join(binDir, "codex"), 0o755);
  const originalPath = process.env.PATH;
  process.env.PATH = binDir;
  const restoreEnv = withZembedEnv({ ZCP_AGENTS: "codex", ZCP_AGENT_OAUTH_CODEX: "true" });
  try {
    const { stub, extension, extensionDir } = loadExtension();
    await extension.activate(newCtx(extensionDir));

    launcherPanel(stub).webview.__fireMessage({ type: "launch", id: "codex", mode: "terminal" });

    assert.equal(stub.terminals.length, 1);
    assert.equal(stub.terminals[0].sent[0].text, "codex --dangerously-bypass-approvals-and-sandbox");
  } finally {
    process.env.PATH = originalPath;
    restoreEnv();
  }
});

test("a binary reachable only through the store's PATH is actionable (host PATH narrower than the runtime PATH — the 0.1.5 live regression)", async () => {
  const binDir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-launcher-bin-"));
  fs.writeFileSync(path.join(binDir, "codex"), "#!/bin/sh\n");
  fs.chmodSync(path.join(binDir, "codex"), 0o755);
  const originalPath = process.env.PATH;
  process.env.PATH = ""; // the extension host's own PATH knows nothing
  const restoreEnv = withZembedEnv({ PATH: binDir, ZCP_AGENTS: "codex", ZCP_AGENT_OAUTH_CODEX: "true" });
  try {
    const { stub, extension, extensionDir } = loadExtension();
    await extension.activate(newCtx(extensionDir));

    launcherPanel(stub).webview.__fireMessage({ type: "launch", id: "codex", mode: "terminal" });

    assert.equal(stub.terminals.length, 1, "the store PATH must make codex installed and launchable");
    assert.equal(stub.terminals[0].sent[0].text, "codex --dangerously-bypass-approvals-and-sandbox");
  } finally {
    process.env.PATH = originalPath;
    restoreEnv();
  }
});

// ---- viewKey ----------------------------------------------------------------
//
// The watcher-driven reopen (fs.watch firing on a real store change) isn't
// exercised behaviorally here: readZembedEnv() reads the hardcoded
// /etc/zerops-zembed/env.json, and making fs.watch itself fire against that
// path would mean writing there for real, which this harness can't (and
// shouldn't) do. Instead, viewKey's own key-comparison logic — the thing
// onEnvChange relies on to decide whether to reopen — is unit-tested
// directly.

test("viewKey: an availability reorder changes the key", () => {
  const { extension } = loadExtension();
  const a = { agents: [
    { id: "codex", installed: true, authType: "oauth", authorized: true },
    { id: "grok", installed: true, authType: undefined, authorized: false },
  ] };
  const b = { agents: [
    { id: "grok", installed: true, authType: undefined, authorized: false },
    { id: "codex", installed: true, authType: "oauth", authorized: true },
  ] };
  assert.notEqual(extension.viewKey(a), extension.viewKey(b));
});

test("viewKey: an availability removal changes the key", () => {
  const { extension } = loadExtension();
  const a = { agents: [
    { id: "codex", installed: true, authType: undefined, authorized: false },
    { id: "grok", installed: true, authType: undefined, authorized: false },
  ] };
  const b = { agents: [{ id: "codex", installed: true, authType: undefined, authorized: false }] };
  assert.notEqual(extension.viewKey(a), extension.viewKey(b));
});

test("viewKey: an installed flip changes the key", () => {
  const { extension } = loadExtension();
  const a = { agents: [{ id: "codex", installed: true, authType: undefined, authorized: false }] };
  const b = { agents: [{ id: "codex", installed: false, authType: undefined, authorized: false }] };
  assert.notEqual(extension.viewKey(a), extension.viewKey(b));
});

test("viewKey: an auth flip changes the key", () => {
  const { extension } = loadExtension();
  const a = { agents: [{ id: "codex", installed: true, authType: "oauth", authorized: false }] };
  const b = { agents: [{ id: "codex", installed: true, authType: "oauth", authorized: true }] };
  assert.notEqual(extension.viewKey(a), extension.viewKey(b));
});

test("viewKey: an unrelated difference in agent list shape that resolves to the same fields is stable", () => {
  const { extension } = loadExtension();
  const view = { agents: [{ id: "codex", installed: true, authType: "token", authorized: true, desc: "some description", opens: [] }] };
  const sameButDifferentObjectIdentity = { agents: [{ id: "codex", installed: true, authType: "token", authorized: true, desc: "a totally different description", opens: [{ mode: "terminal" }] }] };
  assert.equal(extension.viewKey(view), extension.viewKey(sameButDifferentObjectIdentity), "viewKey only folds id/installed/authType/authorized — other fields must not affect it");
});
