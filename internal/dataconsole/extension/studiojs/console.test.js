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

function emitReady(child, port, token, writeToken) {
  child.stdout.emit(
    "data",
    Buffer.from(
      JSON.stringify({ url: "http://127.0.0.1:" + port, sessionToken: token, writeToken: writeToken || "wttok", pid: port, allowWrites: false }) + "\n"
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
    let writeEnabled = false;
    const client = {
      cfg: cfg,
      request: function () { return Promise.resolve({}); },
      setWriteEnabled: function (v) { writeEnabled = !!v; },
      isWriteEnabled: function () { return writeEnabled; },
    };
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
  assert.strictEqual(makeClient.created[0].cfg.writeToken, "wttok", "broker holds the independent write token host-side");
}

// The host write-confirm callback runs the native modal and RETURNS its result —
// accept → true, decline → false — with NO network arm step (write authority is
// now presented per request, caller-bound). The panel manager turns that boolean
// into the broker's writeEnabled; there is no arm() to call. Removing the arm step
// is what closed the standalone-write gap: there is no process latch to flip.
async function testConfirmWritesReturnsModalResult() {
  vscode.__reset();
  const spawn = createFakeSpawn();
  const panels = fakePanels();
  const makeClient = fakeClientFactory();
  const mgr = newMgr(spawn, panels, makeClient);

  const opened = mgr.open({ workspaceRoot: "/w/confirm", extensionPath: "/ext", service: "db", postMessage: function () {} });
  emitReady(spawn.children[0], 4200, "tok", "the-write-token");
  await opened;
  await tick();

  const broker = makeClient.created[0];
  assert.strictEqual(broker.cfg.writeToken, "the-write-token", "broker received the ready-line write token");
  assert.strictEqual(typeof broker.arm, "undefined", "broker has no arm() — writes are caller-bound, not process-global");
  const confirmWrites = panels.shows[0].opts.confirmWrites;
  assert.strictEqual(typeof confirmWrites, "function", "panel receives the host write-confirm callback");

  // Accepted modal → true (the panel manager flips the broker's writeEnabled on).
  vscode.__pushWarningMessageResult("Enable writes");
  const accepted = await confirmWrites();
  assert.strictEqual(accepted, true, "accepted modal → writes confirmed");

  // Declined modal → false (no queued result → modal returns undefined).
  const declined = await confirmWrites();
  assert.strictEqual(declined, false, "declined modal → writes not confirmed");
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
  // The runtime arm step is gone — write authority is caller-bound (per-request
  // write token), never a process-global latch. No source may resurrect it.
  const clientSrc = fs.readFileSync(path.join(__dirname, "..", "templates", "vscode-studio", "lib", "consoleClient.js"), "utf8");
  for (const src of [sessionSrc, clientSrc]) {
    for (const forbidden of ["arm-writes", "X-Arm-Token", "armToken", "broker.arm"]) {
      assert.ok(!src.includes(forbidden), "no source may reference the removed arm path: " + forbidden);
    }
  }
  assert.ok(clientSrc.includes("X-Write-Token"), "consoleClient.js must attach the per-request write token");

  const appSrc = fs.readFileSync(path.join(__dirname, "..", "..", "console", "webui", "dist", "app.js"), "utf8");
  assert.ok(!/\bprompt\(/.test(appSrc), "app.js must not use window.prompt (a no-op in a webview) — use promptModal");
  assert.ok(appSrc.includes("rpcFetch") && appSrc.includes("acquireVsCodeApi"), "app.js must carry the embedded postMessage transport");
  // The STANDALONE SPA holds only the read bearer (from the URL fragment) and stays
  // view-only by construction: it must carry NO write-token path — never attaching
  // one, and never able to lift the server-side write gate.
  assert.ok(!appSrc.includes("writeToken") && !appSrc.includes("X-Write-Token"), "app.js (standalone SPA) must carry no write-token path — it stays view-only");

  // Standalone read-only: edit affordances render ONLY when embedded AND write-mode
  // is on. A non-embedded (browser tab) SPA is never in edit mode — every mutation
  // 403s server-side without the write token, so showing write UI would mislead.
  assert.ok(/function editing\(\)\s*\{\s*return state\.embedded && state\.writeEnabled;\s*\}/.test(appSrc),
    "editing() must require embedded — the standalone SPA is view-only, no write affordances");
  assert.ok(appSrc.includes('badge.textContent = "read-only"'), "standalone must show a persistent read-only indicator");
  // Orphans removed with the standalone write toggle (delete, don't disable): the
  // server's allowWrites flag no longer drives a client toggle, and there is no
  // local editMode latch left behind.
  assert.ok(!appSrc.includes("allowWrites"), "app.js must not read the server allowWrites flag — standalone is unconditionally view-only");
  assert.ok(!appSrc.includes("editMode"), "the standalone editMode latch is removed (no orphan state)");
  // The vestigial legacy standalone-iframe auth path is deleted (origin-less
  // postMessage that set state.token) — delete, don't disable.
  assert.ok(!appSrc.includes("dataconsole-auth"), "the dead dataconsole-auth postMessage handler is deleted");
}

(async function main() {
  await testOpenSpawnsWriteCapablePanel();
  await testConfirmWritesReturnsModalResult();
  await testSecondOpenReusesProcessAndReveals();
  await testPanelDisposeKillsProcess();
  testNoLegacyEmbedSurfacesInSource();
  console.log("console.test.js OK");
})().catch(function (err) {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
