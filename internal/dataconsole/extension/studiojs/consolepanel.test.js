"use strict";

const assert = require("assert");
const path = require("path");
const { createConsolePanelManager, buildHtml } = require("../templates/vscode-studio/lib/consolePanel");

const DIST = path.join(__dirname, "..", "..", "console", "webui", "dist");
const fakeUri = { file: (p) => ({ fsPath: p }) };

function fakeWebview() {
  const wv = {
    posted: [],
    _html: "",
    cspSource: "vscode-resource://x",
    asWebviewUri: (uri) => ({ toString: () => "webview://" + uri.fsPath }),
    postMessage: function (m) { this.posted.push(m); return true; },
    onDidReceiveMessage: function (fn) { this._receiver = fn; },
    __receive: function (m) { return this._receiver && this._receiver(m); },
  };
  Object.defineProperty(wv, "html", { get() { return this._html; }, set(v) { this._html = v; } });
  return wv;
}

function fakePanel(wv) {
  return {
    webview: wv,
    reveal: function () {},
    onDidDispose: function (fn) { this._onDispose = fn; },
    dispose: function () { if (this._onDispose) this._onDispose(); },
  };
}

// fakeBroker records requests + tracks the host-side write gate.
function fakeBroker(reply) {
  let writeEnabled = false;
  return {
    calls: [],
    request: function (req) { this.calls.push(req); return Promise.resolve(reply(req)); },
    setWriteEnabled: function (v) { writeEnabled = !!v; },
    isWriteEnabled: function () { return writeEnabled; },
  };
}

// buildHtml rewrites the relative asset refs to webview URIs and injects a
// webview CSP with the cspSource and NO connect-src (the webview cannot fetch).
function testBuildHtml() {
  const wv = fakeWebview();
  const html = buildHtml({ Uri: fakeUri }, wv, DIST);
  assert.ok(html.includes('src="webview://' + path.join(DIST, "app.js") + '"'), "app.js rewritten to a webview URI");
  assert.ok(html.includes('href="webview://' + path.join(DIST, "style.css") + '"'), "style.css rewritten");
  assert.ok(!/(href|src)="(app\.js|style\.css|dc-embed\.js|contract\.js)"/.test(html), "no raw relative asset refs remain");
  assert.ok(html.includes("Content-Security-Policy"), "injects a CSP meta");
  assert.ok(html.includes("script-src vscode-resource://x"), "script-src uses the webview cspSource");
  assert.ok(html.includes("img-src vscode-resource://x blob: data:"), "img-src allows blob/data previews");
  assert.ok(!/connect-src/.test(html), "no connect-src — the webview cannot fetch (data is brokered)");
}

// index.html — not this file — owns which assets it needs: a fixture markup with
// an EXTRA <script> tag (standing in for a future SPA file) is discovered and
// rewritten with NO hand-list edit here, because there is no hand list anymore.
function testBuildHtml_ExtraScriptTagDiscoveredWithNoHandListEdit() {
  const wv = fakeWebview();
  const fixture =
    "<head></head><body>" +
    '<link rel="stylesheet" href="style.css">' +
    '<script src="app.js"></script>' +
    '<script src="future-feature.js"></script>' +
    "</body>";
  const html = buildHtml({ Uri: fakeUri }, wv, "/media", function () { return fixture; });
  assert.ok(
    html.includes('src="webview://' + path.join("/media", "future-feature.js") + '"'),
    "a script tag absent from any hand list is still discovered and rewritten to a webview URI"
  );
  assert.ok(!/src="future-feature\.js"/.test(html), "no raw relative ref remains for the newly discovered file");
}

// A ref buildHtml cannot safely resolve under mediaDir (here: a ".." path
// traversal segment) is refused rather than silently rewritten to something that
// could escape mediaDir, and the post-rewrite verify pass FAILS LOUD.
function testBuildHtml_UnrewritableRefThrows() {
  const wv = fakeWebview();
  const fixture = '<head></head><body><script src="../../etc/evil.js"></script></body>';
  assert.throws(
    function () { buildHtml({ Uri: fakeUri }, wv, "/media", function () { return fixture; }); },
    /unrewritten relative asset ref/,
    "a path-traversal ref throws instead of silently resolving outside mediaDir"
  );
}

async function testPanelWiring() {
  const wv = fakeWebview();
  const panel = fakePanel(wv);
  const fakeVscode = { ViewColumn: { One: 1 }, Uri: fakeUri, window: { createWebviewPanel: function () { return panel; } } };
  const broker = fakeBroker(function () {
    return { status: 200, ok: true, headers: { "content-type": "application/json", "x-request-id": "rid" }, bytes: Buffer.from('{"ok":true}') };
  });
  const mgr = createConsolePanelManager({ vscode: fakeVscode, readFile: function () { return "<head></head><body></body>"; } });
  mgr.show("k", { mediaDir: "/m", broker: broker, service: "db", confirmWrites: function () { return true; }, onDispose: function () {} });

  // dc-ready -> dataconsole-init with the deep-link service, and NO bearer.
  wv.__receive({ type: "dc-ready" });
  const init = wv.posted.find((m) => m.type === "dataconsole-init");
  assert.ok(init, "host answers dc-ready with dataconsole-init");
  assert.strictEqual(init.service, "db", "init deep-links the requested service");
  assert.ok(!("token" in init) && !("bearer" in init), "init carries NO bearer to the webview");

  // dc-rpc -> broker.request -> dc-rpc-result (base64 body + relayed headers).
  wv.posted.length = 0;
  await wv.__receive({ type: "dc-rpc", id: "r1", method: "GET", path: "/api/services" });
  assert.strictEqual(broker.calls.length, 1, "dc-rpc reaches the host broker");
  assert.strictEqual(broker.calls[0].path, "/api/services");
  const res = wv.posted.find((m) => m.type === "dc-rpc-result");
  assert.ok(res && res.id === "r1", "broker reply is posted back with the id");
  assert.strictEqual(Buffer.from(res.b64, "base64").toString(), '{"ok":true}', "body relayed as base64");
  assert.strictEqual(res.headers["x-request-id"], "rid", "x-request-id relayed for error correlation");
  assert.ok(!JSON.stringify(wv.posted).includes("Bearer"), "no bearer ever crosses to the webview");
}

async function testHostFileOps() {
  const wv = fakeWebview();
  const panel = fakePanel(wv);
  const wrote = [];
  const fakeVscode = {
    ViewColumn: { One: 1 },
    Uri: fakeUri,
    window: {
      createWebviewPanel: function () { return panel; },
      showSaveDialog: function (o) { return Promise.resolve({ fsPath: "/save/" + (o.defaultUri ? o.defaultUri.fsPath : "x") }); },
      showOpenDialog: function () { return Promise.resolve([{ fsPath: "/picked/note.txt" }]); },
    },
    workspace: {
      fs: {
        writeFile: function (uri, buf) { wrote.push({ uri: uri, buf: buf }); return Promise.resolve(); },
        readFile: function () { return Promise.resolve(Buffer.from("hello")); },
      },
    },
  };
  const reqs = [];
  const downloads = [];
  function fakeBrowserDownload(opts) {
    downloads.push(opts);
    return Promise.resolve({ ok: true, code: "completed" });
  }
  const broker = fakeBroker(function () { return { status: 200, ok: true, headers: {}, bytes: Buffer.from("FILEBYTES") }; });
  broker.request = function (r) { reqs.push(r); return Promise.resolve({ status: 200, ok: true, headers: {}, bytes: Buffer.from("FILEBYTES") }); };
  broker.setWriteEnabled(true);
  const mgr = createConsolePanelManager({
    vscode: fakeVscode,
    readFile: function () { return "<head></head>"; },
    browserDownload: fakeBrowserDownload,
  });
  mgr.show("k", { mediaDir: "/m", broker: broker, service: "store", confirmWrites: function () { return true; }, onDispose: function () {} });

  // Download delegates to the one-use browser bridge. Neither the capped
  // buffered broker request nor VS Code's remote save dialog/file write runs;
  // the webview receives only a correlated completion result, never bytes/URL.
  await wv.__receive({ type: "dc-download", id: "d1", service: "store", segs: ["a.txt"], name: "a.txt" });
  assert.strictEqual(downloads.length, 1, "download delegates exactly once to browserDownload");
  assert.strictEqual(downloads[0].client, broker, "browserDownload receives the bound host broker");
  assert.strictEqual(downloads[0].service, "store");
  assert.deepStrictEqual(downloads[0].segments, ["a.txt"]);
  assert.strictEqual(downloads[0].fallbackName, "a.txt");
  assert.ok(downloads[0].signal, "panel supplies a cancellation signal owned by the panel entry");
  assert.ok(!reqs.some((r) => r.method === "GET" && r.path.indexOf("/api/blob") === 0), "download never uses capped buffered /api/blob");
  assert.strictEqual(wrote.length, 0, "download never writes through workspace.fs");
  const downloadResult = wv.posted.find((m) => m.type === "dataconsole-download-result");
  assert.deepStrictEqual(downloadResult, { type: "dataconsole-download-result", id: "d1", ok: true, code: "completed" });
  assert.ok(!("b64" in downloadResult) && !("url" in downloadResult), "download result carries no bytes or ticket URL");

  // upload: showOpenDialog -> readFile -> broker PUT /api/blob (base64), no megabytes over postMessage.
  reqs.length = 0;
  wv.posted.length = 0;
  await wv.__receive({ type: "dc-upload", service: "store", segs: ["dir"] });
  const put = reqs.find((r) => r.method === "PUT" && r.path === "/api/blob");
  assert.ok(put, "upload PUTs the blob via the broker");
  const body = JSON.parse(put.body);
  assert.deepStrictEqual(body.path.segments, ["dir", "note.txt"], "upload targets segs + the picked filename");
  assert.strictEqual(Buffer.from(body.data, "base64").toString(), "hello", "file bytes base64-encoded host-side");
  assert.ok(wv.posted.some((m) => m.type === "dataconsole-uploaded" && m.ok), "webview told the upload succeeded");
}

async function testWriteModeToggle() {
  const wv = fakeWebview();
  const panel = fakePanel(wv);
  let confirmAnswer = true;
  let confirmCalls = 0;
  const fakeVscode = { ViewColumn: { One: 1 }, Uri: fakeUri, window: { createWebviewPanel: function () { return panel; } } };
  const broker = fakeBroker(function () { return { status: 403, ok: false, headers: {}, bytes: Buffer.from("{}") }; });
  const mgr = createConsolePanelManager({ vscode: fakeVscode, readFile: function () { return "<head></head>"; } });
  mgr.show("k", {
    mediaDir: "/m", broker: broker, service: "db",
    confirmWrites: function () { confirmCalls++; return Promise.resolve(confirmAnswer); },
    onDispose: function () {},
  });

  // init reflects the broker's current (off) state, never a bearer.
  wv.__receive({ type: "dc-ready" });
  const init = wv.posted.find((m) => m.type === "dataconsole-init");
  assert.strictEqual(init.writeEnabled, false, "starts read-only (write mode off)");

  // enable: host confirm runs, broker flips on, webview told writeEnabled:true.
  wv.posted.length = 0;
  await wv.__receive({ type: "dc-write-mode", enable: true });
  assert.strictEqual(confirmCalls, 1, "enabling write mode asks for host confirmation");
  assert.strictEqual(broker.isWriteEnabled(), true, "broker now forwards mutations");
  assert.ok(wv.posted.some((m) => m.type === "dataconsole-write-mode" && m.writeEnabled === true), "webview told write mode is on");

  // declined enable: broker stays off.
  broker.setWriteEnabled(false);
  confirmAnswer = false;
  wv.posted.length = 0;
  await wv.__receive({ type: "dc-write-mode", enable: true });
  assert.strictEqual(broker.isWriteEnabled(), false, "declined confirmation keeps write mode off");
  assert.ok(wv.posted.some((m) => m.type === "dataconsole-write-mode" && m.writeEnabled === false), "webview told write mode stayed off");

  // disable: no confirm, broker off immediately.
  broker.setWriteEnabled(true);
  confirmCalls = 0;
  wv.posted.length = 0;
  await wv.__receive({ type: "dc-write-mode", enable: false });
  assert.strictEqual(confirmCalls, 0, "disabling needs no confirmation");
  assert.strictEqual(broker.isWriteEnabled(), false, "broker stops forwarding mutations");
}

// Fail-closed: if the panel has NO confirmWrites callback (a missing host gate),
// enabling write mode must be treated as NOT approved — writes stay off. An absent
// gate can never be silent implicit consent.
async function testWriteModeFailsClosedWithoutConfirm() {
  const wv = fakeWebview();
  const panel = fakePanel(wv);
  const fakeVscode = { ViewColumn: { One: 1 }, Uri: fakeUri, window: { createWebviewPanel: function () { return panel; } } };
  const broker = fakeBroker(function () { return { status: 403, ok: false, headers: {}, bytes: Buffer.from("{}") }; });
  const mgr = createConsolePanelManager({ vscode: fakeVscode, readFile: function () { return "<head></head>"; } });
  // No confirmWrites passed → entry.confirmWrites is undefined.
  mgr.show("k", { mediaDir: "/m", broker: broker, service: "db", onDispose: function () {} });

  wv.posted.length = 0;
  await wv.__receive({ type: "dc-write-mode", enable: true });
  assert.strictEqual(broker.isWriteEnabled(), false, "missing confirmWrites callback keeps write mode OFF (fail-closed)");
  assert.ok(wv.posted.some((m) => m.type === "dataconsole-write-mode" && m.writeEnabled === false), "webview told write mode stayed off");
}

// A same-key reveal rebinds a FRESH broker (open() makeClient()s a new one each
// time). The host-confirmed write-enabled state MUST carry onto it and the webview
// MUST be re-synced — otherwise the SPA keeps its green write toggle while the new
// broker silently reverts to read-only, so every mutation after a re-browse fails
// "write mode is off" (the divergence the review found).
async function testWriteModePreservedAcrossReveal() {
  const wv = fakeWebview();
  const panel = fakePanel(wv);
  const fakeVscode = { ViewColumn: { One: 1 }, Uri: fakeUri, window: { createWebviewPanel: function () { return panel; } } };
  const brokerA = fakeBroker(function () { return { status: 200, ok: true, headers: {}, bytes: Buffer.from("{}") }; });
  const mgr = createConsolePanelManager({ vscode: fakeVscode, readFile: function () { return "<head></head><body></body>"; } });
  mgr.show("k", { mediaDir: "/m", broker: brokerA, service: "db", confirmWrites: function () { return true; }, onDispose: function () {} });
  await wv.__receive({ type: "dc-write-mode", enable: true });
  assert.strictEqual(brokerA.isWriteEnabled(), true, "sanity: write mode enabled on the first broker");

  // Re-browse (another "Browse data" click) → open() rebinds a fresh broker B.
  const brokerB = fakeBroker(function () { return { status: 200, ok: true, headers: {}, bytes: Buffer.from("{}") }; });
  wv.posted.length = 0;
  mgr.show("k", { mediaDir: "/m", broker: brokerB, service: "cache", confirmWrites: function () { return true; }, onDispose: function () {} });
  assert.strictEqual(brokerB.isWriteEnabled(), true, "the rebound broker INHERITS the host-confirmed write-enabled state (no silent revert to read-only)");
  assert.ok(wv.posted.some((m) => m.type === "dataconsole-write-mode" && m.writeEnabled === true), "the webview is re-synced to the rebound broker's write state on reveal");
}

async function testDownloadFailureAndPanelDispose() {
  const wv = fakeWebview();
  const panel = fakePanel(wv);
  const fakeVscode = { ViewColumn: { One: 1 }, Uri: fakeUri, window: { createWebviewPanel: function () { return panel; } } };
  const broker = fakeBroker(function () { return { status: 200, ok: true, headers: {}, bytes: Buffer.from("{}") }; });
  let mode = "failure";
  let activeSignal;
  function download(opts) {
    activeSignal = opts.signal;
    if (mode === "failure") {
      return Promise.resolve({ ok: false, code: "source-failed", message: "The download source failed." });
    }
    return new Promise(function (resolve) {
      opts.signal.addEventListener("abort", function () {
        resolve({ ok: false, code: "cancelled", message: "Download cancelled." });
      }, { once: true });
    });
  }
  const mgr = createConsolePanelManager({
    vscode: fakeVscode,
    readFile: function () { return "<head></head>"; },
    browserDownload: download,
  });
  mgr.show("k", { mediaDir: "/m", broker: broker, service: "store", onDispose: function () {} });

  await wv.__receive({ type: "dc-download", id: "bad", service: "store", segs: ["bad.bin"] });
  assert.deepStrictEqual(
    wv.posted.find((m) => m.type === "dataconsole-download-result"),
    {
      type: "dataconsole-download-result",
      id: "bad",
      ok: false,
      code: "source-failed",
      message: "The download source failed.",
    },
    "browser bridge failures return a correlated, byte-free public result"
  );

  mode = "pending";
  wv.posted.length = 0;
  const pending = wv.__receive({ type: "dc-download", id: "pending", service: "store", segs: ["large.bin"] });
  await new Promise(function (resolve) { setImmediate(resolve); });
  panel.dispose();
  await pending;
  assert.strictEqual(activeSignal.aborted, true, "disposing the panel aborts every active browser download");
  assert.strictEqual(
    wv.posted.some((m) => m.type === "dataconsole-download-result" && m.id === "pending"),
    false,
    "a completed cancellation never posts into a disposed webview"
  );
}

async function testDownloadSynchronousFailureIsSanitized() {
  const wv = fakeWebview();
  const panel = fakePanel(wv);
  const fakeVscode = { ViewColumn: { One: 1 }, Uri: fakeUri, window: { createWebviewPanel: function () { return panel; } } };
  const broker = fakeBroker(function () { return { status: 200, ok: true, headers: {}, bytes: Buffer.from("{}") }; });
  const mgr = createConsolePanelManager({
    vscode: fakeVscode,
    readFile: function () { return "<head></head>"; },
    browserDownload: function () { throw new Error("raw secret-bearing failure"); },
  });
  mgr.show("k", { mediaDir: "/m", broker: broker, service: "store", onDispose: function () {} });

  await wv.__receive({ type: "dc-download", id: "sync", service: "store", segs: ["x.bin"] });
  assert.deepStrictEqual(
    wv.posted.find((m) => m.type === "dataconsole-download-result"),
    {
      type: "dataconsole-download-result",
      id: "sync",
      ok: false,
      code: "open-failed",
      message: "The browser could not be opened.",
    },
    "a synchronous bridge failure is caught and reported without exposing the raw exception"
  );
}

(async function main() {
  testBuildHtml();
  testBuildHtml_ExtraScriptTagDiscoveredWithNoHandListEdit();
  testBuildHtml_UnrewritableRefThrows();
  await testPanelWiring();
  await testHostFileOps();
  await testWriteModeToggle();
  await testWriteModeFailsClosedWithoutConfirm();
  await testWriteModePreservedAcrossReveal();
  await testDownloadFailureAndPanelDispose();
  await testDownloadSynchronousFailureIsSanitized();
  console.log("consolepanel.test.js OK");
})().catch(function (e) {
  console.error(e && e.stack ? e.stack : e);
  process.exit(1);
});
