"use strict";

const assert = require("assert");
const { buildRouter } = require("../templates/vscode-studio/lib/handlers");
const { createWebviewSession } = require("../templates/vscode-studio/lib/webviewSession");

function createOutputRecorder() {
  const lines = [];
  return {
    lines: lines,
    appendLine: function (line) {
      lines.push(String(line));
    },
  };
}

function createView() {
  return {
    webview: {
      html: "",
      postMessage: function () {
        return Promise.resolve(true);
      },
      onDidReceiveMessage: function () {},
    },
    onDidDispose: function () {},
  };
}

async function testRefreshCoalescesConcurrentCalls() {
  let calls = 0;
  let release;
  const output = createOutputRecorder();
  const view = createView();
  const session = createWebviewSession({
    view: view,
    cards: [
      {
        id: "demo",
        render: function (uiMap) {
          return "<section>" + uiMap.project.name + "</section>";
        },
      },
    ],
    router: buildRouter([]),
    workspaceRoot: "/work",
    outputChannel: output,
    runTransport: function () {
      calls += 1;
      return new Promise(function (resolve) {
        release = function () {
          resolve({ ok: true, uiMap: { project: { name: "demo" }, services: [], warnings: [] } });
        };
      });
    },
  });

  const p1 = session.refreshTopology();
  const p2 = session.refreshTopology();
  assert.strictEqual(calls, 1, "concurrent refreshes share one underlying transport call");
  release();
  await Promise.all([p1, p2]);
  assert.ok(view.webview.html.indexOf("<section>demo</section>") >= 0, "refresh renders the resolved UI map");
}

async function testHandlerRejectionIsReported() {
  const output = createOutputRecorder();
  const session = createWebviewSession({
    view: createView(),
    cards: [],
    router: buildRouter([
      {
        type: "explode",
        handle: async function () {
          throw new Error("boom");
        },
      },
    ]),
    workspaceRoot: "/work",
    outputChannel: output,
    runTransport: async function () {
      return { ok: true, uiMap: { project: { name: "demo" }, services: [], warnings: [] } };
    },
  });

  await session.handleMessage({ type: "explode" });
  assert.ok(
    output.lines.some(function (line) {
      return line.indexOf("handler explode failed") >= 0 && line.indexOf("boom") >= 0;
    }),
    "handler rejection is written to the output channel"
  );
}

async function testDisposeAbortsInFlightVerb() {
  let signal = null;
  const session = createWebviewSession({
    view: createView(),
    cards: [],
    router: buildRouter([]),
    workspaceRoot: "/work",
    outputChannel: createOutputRecorder(),
    runStudioVerb: function (workspaceRoot, args, deps) {
      signal = deps.signal;
      return new Promise(function (resolve) {
        deps.signal.addEventListener("abort", function () {
          resolve({ ok: false, stdout: "", error: "cancelled", cancelled: true });
        });
      });
    },
  });

  const p = session.runVerb(["deploy"]);
  assert.ok(signal, "runVerb passes an abort signal to transport");
  assert.strictEqual(signal.aborted, false, "signal starts active");
  session.dispose();
  const r = await p;
  assert.strictEqual(signal.aborted, true, "dispose aborts the active signal");
  assert.strictEqual(r.cancelled, true, "aborted run resolves as cancelled");
}

(async function main() {
  await testRefreshCoalescesConcurrentCalls();
  await testHandlerRejectionIsReported();
  await testDisposeAbortsInFlightVerb();
  console.log("webview_session.test.js OK");
})().catch(function (err) {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
