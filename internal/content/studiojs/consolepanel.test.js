"use strict";

const assert = require("assert");
const path = require("path");
const { createConsolePanelManager, buildHtml } = require("../templates/vscode-studio/lib/consolePanel");

const DIST = path.join(__dirname, "..", "..", "dataconsole", "console", "webui", "dist");
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
  return { webview: wv, reveal: function () {}, onDidDispose: function () {}, dispose: function () {} };
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
  const broker = fakeBroker(function () { return { status: 200, ok: true, headers: {}, bytes: Buffer.from("FILEBYTES") }; });
  broker.request = function (r) { reqs.push(r); return Promise.resolve({ status: 200, ok: true, headers: {}, bytes: Buffer.from("FILEBYTES") }); };
  broker.setWriteEnabled(true);
  const mgr = createConsolePanelManager({ vscode: fakeVscode, readFile: function () { return "<head></head>"; } });
  mgr.show("k", { mediaDir: "/m", broker: broker, service: "store", confirmWrites: function () { return true; }, onDispose: function () {} });

  // download: broker GET blob -> showSaveDialog -> fs.writeFile(bytes), bytes never re-enter the webview.
  await wv.__receive({ type: "dc-download", service: "store", segs: ["a.txt"], name: "a.txt" });
  assert.ok(reqs.some((r) => r.method === "GET" && r.path.indexOf("/api/blob") === 0), "download GETs the blob via the broker");
  assert.strictEqual(wrote.length, 1, "download writes host-side");
  assert.strictEqual(wrote[0].buf.toString(), "FILEBYTES", "saved bytes came from the broker");

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

(async function main() {
  testBuildHtml();
  await testPanelWiring();
  await testHostFileOps();
  await testWriteModeToggle();
  console.log("consolepanel.test.js OK");
})().catch(function (e) {
  console.error(e && e.stack ? e.stack : e);
  process.exit(1);
});
