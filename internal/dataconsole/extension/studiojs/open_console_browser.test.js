"use strict";

// Standalone browser opener (handlers/open-console-browser.js, type
// "openConsoleBrowser"). Pins the two security-critical properties of the
// standalone open path:
//   1. the URL carries ONLY the read bearer (never a write token) and has a trailing
//      slash BEFORE the fragment (code-server /proxy/<port>/ prefix correctness);
//   2. the opener REUSES the shared session endpoint (no rival spawn) and hands the
//      bearer-only URL to the browser via asExternalUri + openExternal.
//
// The module loads under plain node because it requires the shared session manager
// through the lazy singleton (no "vscode" at require time) — createConsoleSessionManager
// is only invoked when a real handle() runs, which these tests never do.

const assert = require("assert");
const handler = require("../templates/vscode-studio/handlers/open-console-browser");

assert.strictEqual(handler.type, "openConsoleBrowser", "handler type is openConsoleBrowser");
assert.strictEqual(typeof handler.handle, "function", "handler exports handle()");

// (1) URL shape: trailing slash before the fragment, read bearer + svc in the
// fragment, and NO write token in any form.
assert.strictEqual(
  handler.buildStandaloneURL(4101, "read-bearer", "db"),
  "http://127.0.0.1:4101/#t=read-bearer&svc=db",
  "standalone URL has a trailing slash before the fragment and carries the bearer + svc"
);
assert.strictEqual(
  handler.buildStandaloneURL(4101, "read-bearer", ""),
  "http://127.0.0.1:4101/#t=read-bearer",
  "no service → just the bearer fragment (still trailing-slash before the '#')"
);
const withSvc = handler.buildStandaloneURL(4101, "read-bearer", "my svc");
assert.ok(withSvc.indexOf("&svc=my%20svc") >= 0, "service is URL-encoded in the fragment");
for (const url of [handler.buildStandaloneURL(4101, "read-bearer", "db"), withSvc]) {
  assert.ok(url.indexOf("/#t=") >= 0, "the '#' fragment is preceded by a path '/' (proxy-prefix safe)");
  assert.ok(url.indexOf("writeToken") < 0 && url.indexOf("X-Write-Token") < 0 && url.indexOf("wt=") < 0,
    "standalone URL NEVER carries a write token — the browser session is view-only");
}

// (2) openInBrowser reuses the shared endpoint and opens the bearer-only URL,
// asExternalUri-mapped, via openExternal — never a write token.
async function testOpenInBrowserReusesEndpointAndStaysReadOnly() {
  const opened = [];
  const fakeVscode = {
    Uri: { parse: function (s) { return { toString: function () { return s; } }; } },
    env: {
      // code-server-style mapping: loopback → authenticated /proxy/<port>/ URL,
      // fragment preserved.
      asExternalUri: function (u) {
        const mapped = u.toString().replace("http://127.0.0.1:4101/", "https://ide.example/proxy/4101/");
        return Promise.resolve({ toString: function () { return mapped; } });
      },
      openExternal: function (u) { opened.push(u.toString()); return Promise.resolve(true); },
    },
  };
  const endpointCalls = [];
  const fakeMgr = {
    endpoint: function (opts) {
      endpointCalls.push(opts);
      // The manager NEVER hands a write token to the standalone opener.
      return Promise.resolve({ url: "http://127.0.0.1:4101", port: 4101, sessionToken: "read-bearer" });
    },
  };

  const target = await handler.openInBrowser(fakeMgr, fakeVscode, { service: "db" }, { workspaceRoot: "/w/x", postMessage: function () {} });

  assert.strictEqual(endpointCalls.length, 1, "reuses the shared session endpoint (never spawns a rival)");
  assert.strictEqual(endpointCalls[0].workspaceRoot, "/w/x", "endpoint keyed by the workspace root");
  assert.strictEqual(opened.length, 1, "opens exactly one external URL");
  assert.ok(opened[0].indexOf("/proxy/4101/") >= 0, "opened URL is asExternalUri-mapped for the browser");
  assert.ok(opened[0].indexOf("#t=read-bearer") >= 0, "opened URL carries the read bearer in the fragment");
  assert.ok(opened[0].indexOf("&svc=db") >= 0, "opened URL deep-links the clicked service");
  assert.ok(opened[0].indexOf("writeToken") < 0 && opened[0].indexOf("X-Write-Token") < 0,
    "opened URL never carries a write token");
  assert.strictEqual(target, opened[0], "openInBrowser returns the URL it opened");
}

// A console that never became ready → no browser tab is opened (no bad URL).
async function testUnreachableConsoleOpensNothing() {
  const opened = [];
  const fakeVscode = {
    Uri: { parse: function (s) { return { toString: function () { return s; } }; } },
    env: {
      asExternalUri: function (u) { return Promise.resolve(u); },
      openExternal: function (u) { opened.push(u.toString()); return Promise.resolve(true); },
    },
  };
  const fakeMgr = { endpoint: function () { return Promise.resolve(null); } };
  const target = await handler.openInBrowser(fakeMgr, fakeVscode, { service: "db" }, { workspaceRoot: "/w/x" });
  assert.strictEqual(target, null, "no endpoint → openInBrowser returns null");
  assert.strictEqual(opened.length, 0, "no external URL is opened when the console is unreachable");
}

(async function main() {
  await testOpenInBrowserReusesEndpointAndStaysReadOnly();
  await testUnreachableConsoleOpensNothing();
  console.log("open_console_browser.test.js OK");
})().catch(function (e) {
  console.error(e && e.stack ? e.stack : e);
  process.exit(1);
});
