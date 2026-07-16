"use strict";

const assert = require("assert");
const EventEmitter = require("events");
const {
  runStudioVerb,
  StudioTransportTimeoutError,
  StudioTransportCancelledError,
} = require("../templates/vscode-studio/lib/transport");

function createChild() {
  const child = new EventEmitter();
  child.killed = false;
  child.kill = function () {
    child.killed = true;
    child.emit("exit", null, "SIGTERM");
  };
  return child;
}

function createFakeExecFile() {
  const calls = [];
  function execFile(command, args, options, callback) {
    const child = createChild();
    calls.push({ command: command, args: args, options: options, callback: callback, child: child });
    return child;
  }
  execFile.calls = calls;
  return execFile;
}

async function testSuccessParsesJSON() {
  const execFile = createFakeExecFile();
  const p = runStudioVerb("/work", ["topology"], { execFile: execFile });
  assert.strictEqual(typeof p.then, "function", "runStudioVerb returns a Promise");
  assert.strictEqual(execFile.calls.length, 1, "execFile called asynchronously through the injected dependency");
  assert.strictEqual(execFile.calls[0].command, "zcp", "default binary is zcp");
  assert.deepStrictEqual(execFile.calls[0].args, ["studio", "topology"], "studio verb args are prefixed");
  execFile.calls[0].callback(null, '{"ok":true}', "");

  const r = await p;
  assert.strictEqual(r.ok, true, "successful command returns ok");
  assert.strictEqual(r.stdout, '{"ok":true}', "stdout is preserved");
  assert.deepStrictEqual(r.data, { ok: true }, "JSON stdout is parsed into data");
}

async function testNonJSONStdoutIsTolerated() {
  const execFile = createFakeExecFile();
  const p = runStudioVerb("/work", ["sync-env"], { execFile: execFile });
  execFile.calls[0].callback(null, "plain text", "");
  const r = await p;
  assert.strictEqual(r.ok, true, "non-JSON stdout is still a successful command");
  assert.strictEqual(r.stdout, "plain text", "plain stdout is preserved");
  assert.strictEqual(r.data, null, "non-JSON stdout leaves data null");
}

async function testENOENTNeedsInit() {
  const execFile = createFakeExecFile();
  const p = runStudioVerb("/work", ["topology"], { execFile: execFile });
  const err = new Error("spawn zcp ENOENT");
  err.code = "ENOENT";
  execFile.calls[0].callback(err, "", "");
  const r = await p;
  assert.strictEqual(r.ok, false, "ENOENT is a transport failure");
  assert.strictEqual(r.needsInit, true, "ENOENT maps to needsInit");
  assert.ok(r.error.indexOf("ENOENT") >= 0, "ENOENT message is preserved");
}

async function testNonZeroUsesStderr() {
  const execFile = createFakeExecFile();
  const p = runStudioVerb("/work", ["deploy"], { execFile: execFile });
  const err = new Error("Command failed");
  err.code = 42;
  execFile.calls[0].callback(err, "ignored stdout", "build failed\n");
  const r = await p;
  assert.strictEqual(r.ok, false, "non-zero command is a failure");
  assert.strictEqual(r.error, "build failed", "stderr is the surfaced error");
  assert.strictEqual(r.stdout, "ignored stdout", "stdout remains available for diagnostics");
}

async function testTimeoutIsClassedAndKillsChild() {
  const execFile = createFakeExecFile();
  const p = runStudioVerb("/work", ["deploy"], { execFile: execFile, timeoutMs: 1 });
  const child = execFile.calls[0].child;
  const r = await p;
  assert.strictEqual(r.ok, false, "timeout is a failure result");
  assert.strictEqual(r.timeout, true, "timeout result is marked");
  assert.ok(r.cause instanceof StudioTransportTimeoutError, "timeout cause uses the timeout class");
  assert.strictEqual(child.killed, true, "timed-out process is killed");
}

async function testCancellationIsClassedAndKillsChild() {
  const execFile = createFakeExecFile();
  const controller = new AbortController();
  const p = runStudioVerb("/work", ["topology"], {
    execFile: execFile,
    signal: controller.signal,
    timeoutMs: 1000,
  });
  const child = execFile.calls[0].child;
  controller.abort();
  const r = await p;
  assert.strictEqual(r.ok, false, "cancelled command is a failure result");
  assert.strictEqual(r.cancelled, true, "cancelled result is marked");
  assert.ok(r.cause instanceof StudioTransportCancelledError, "cancel cause uses the cancellation class");
  assert.strictEqual(child.killed, true, "cancelled process is killed");
}

(async function main() {
  await testSuccessParsesJSON();
  await testNonJSONStdoutIsTolerated();
  await testENOENTNeedsInit();
  await testNonZeroUsesStderr();
  await testTimeoutIsClassedAndKillsChild();
  await testCancellationIsClassedAndKillsChild();
  console.log("transport.test.js OK");
})().catch(function (err) {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
