"use strict";

const assert = require("assert");
const EventEmitter = require("events");
const fs = require("fs");
const path = require("path");
const vscode = require("./vscode-stub");
const { createConsoleSessionManager } = require("../templates/vscode-studio/lib/consoleSession");

function createFakeSpawn() {
  const calls = [];
  const children = [];
  function spawn(command, args, options) {
    const child = new EventEmitter();
    child.stdout = new EventEmitter();
    child.exitCode = null;
    child.killed = false;
    child.kill = function () {
      child.killed = true;
      child.exitCode = 1;
      child.emit("exit", 1);
    };
    calls.push({ command, args, options, child });
    children.push(child);
    return child;
  }
  spawn.calls = calls;
  spawn.children = children;
  return spawn;
}

function emitReady(child, port, token) {
  child.stdout.emit(
    "data",
    Buffer.from(
      JSON.stringify({ url: "http://127.0.0.1:" + port, sessionToken: token, pid: port, allowWrites: false }) + "\n"
    )
  );
}

async function tick() {
  await new Promise(function (resolve) { setImmediate(resolve); });
}

function fakePanels() {
  const shows = [];
  return {
    shows: shows,
    show: function (key, opts) {
      shows.push({ key: key, opts: opts });
      return { dispose: function () {} };
    },
    disposeAll: function () {},
  };
}

function fakeClientFactory() {
  const created = [];
  function make(cfg) {
    const client = { cfg: cfg, request: function () { return Promise.resolve({}); } };
    created.push(client);
    return client;
  }
  make.created = created;
  return make;
}

function newMgr(spawn, panels, makeClient) {
  return createConsoleSessionManager({ spawn: spawn, vscode: vscode, panelManager: panels, createConsoleClient: makeClient });
}

async function testOpenSpawnsWriteCapablePanel() {
  vscode.__reset();
  const spawn = createFakeSpawn();
  const panels = fakePanels();
  const makeClient = fakeClientFactory();
  const mgr = newMgr(spawn, panels, makeClient);

  const opened = mgr.open({ workspaceRoot: "/w/a", extensionPath: "/ext", service: "db", postMessage: function () {} });
  assert.strictEqual(spawn.calls.length, 1, "open spawns one console process");
  // The console is ALWAYS spawned write-capable; the broker gates mutations until
  // the user enables write mode in the panel (host-confirmed). No spawn-time prompt.
  assert.ok(spawn.calls[0].args.includes("--allow-writes"), "console is spawned write-capable (broker gates)");
  assert.strictEqual(vscode.__warningMessages.length, 0, "opening never prompts at spawn time (confirm is on the panel toggle)");
  emitReady(spawn.children[0], 4101, "tok");
  await opened;
  await tick();

  assert.strictEqual(vscode.__commands.length, 0, "native embed never calls a command (no simpleBrowser.show)");
  assert.strictEqual(panels.shows.length, 1, "open shows the console panel");
  const show = panels.shows[0];
  assert.strictEqual(show.opts.service, "db", "panel deep-links the clicked service");
  assert.strictEqual(show.opts.mediaDir, path.join("/ext", "media", "dataconsole"), "panel loads the materialized SPA");
  assert.strictEqual(typeof show.opts.confirmWrites, "function", "panel receives the host write-confirm callback");
  assert.strictEqual(makeClient.created.length, 1, "a host broker is bound to the process");
  assert.strictEqual(makeClient.created[0].cfg.port, 4101, "broker dials the ready loopback port");
  assert.strictEqual(makeClient.created[0].cfg.token, "tok", "broker holds the bearer host-side");
}

async function testSecondOpenReusesProcessAndReveals() {
  vscode.__reset();
  const spawn = createFakeSpawn();
  const panels = fakePanels();
  const mgr = newMgr(spawn, panels, fakeClientFactory());

  const first = mgr.open({ workspaceRoot: "/w/reuse", extensionPath: "/ext", service: "db", postMessage: function () {} });
  assert.strictEqual(spawn.calls.length, 1, "first open spawns");
  emitReady(spawn.children[0], 4103, "reuse-token");
  await first;

  await mgr.open({ workspaceRoot: "/w/reuse", extensionPath: "/ext", service: "cache", postMessage: function () {} });
  assert.strictEqual(spawn.calls.length, 1, "second same-workspace open reuses the live process");
  assert.strictEqual(panels.shows.length, 2, "panel is shown again (reveal + switch-service)");
  assert.strictEqual(panels.shows[1].opts.service, "cache", "reuse deep-links the newly requested service");
}

async function testPanelDisposeKillsProcess() {
  vscode.__reset();
  const spawn = createFakeSpawn();
  const panels = fakePanels();
  const mgr = newMgr(spawn, panels, fakeClientFactory());

  const opened = mgr.open({ workspaceRoot: "/w/life", extensionPath: "/ext", service: "db", postMessage: function () {} });
  emitReady(spawn.children[0], 4104, "life-token");
  await opened;
  await tick();

  assert.strictEqual(spawn.children[0].killed, false, "process alive while panel open");
  panels.shows[0].opts.onDispose(); // simulate the panel being closed
  assert.strictEqual(spawn.children[0].killed, true, "closing the panel kills the console child");
}

function testNoLegacyEmbedSurfacesInSource() {
  const sessionSrc = fs.readFileSync(path.join(__dirname, "..", "templates", "vscode-studio", "lib", "consoleSession.js"), "utf8");
  for (const forbidden of ["simpleBrowser", "asExternalUri", "dcproxy", "#t="]) {
    assert.ok(!sessionSrc.includes(forbidden), "consoleSession.js must not reference the deleted embed path: " + forbidden);
  }
  const appSrc = fs.readFileSync(path.join(__dirname, "..", "..", "dataconsole", "console", "webui", "dist", "app.js"), "utf8");
  assert.ok(!/\bprompt\(/.test(appSrc), "app.js must not use window.prompt (a no-op in a webview) — use promptModal");
  assert.ok(appSrc.includes("rpcFetch") && appSrc.includes("acquireVsCodeApi"), "app.js must carry the embedded postMessage transport");
}

(async function main() {
  await testOpenSpawnsWriteCapablePanel();
  await testSecondOpenReusesProcessAndReveals();
  await testPanelDisposeKillsProcess();
  testNoLegacyEmbedSurfacesInSource();
  console.log("console.test.js OK");
})().catch(function (err) {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
