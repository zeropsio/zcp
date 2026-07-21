"use strict";

const assert = require("assert");
const http = require("http");
const { PassThrough, Readable } = require("stream");
const { createBrowserDownloader, CLAIM_TTL_MS } = require("../templates/vscode-studio/lib/browserDownload");
const vscodeStub = require("./vscode-stub");

function get(uri) {
  return new Promise(function (resolve, reject) {
    const req = http.get(uri, function (res) {
      const chunks = [];
      res.on("data", function (chunk) { chunks.push(chunk); });
      res.on("end", function () {
        resolve({ status: res.statusCode, headers: res.headers, body: Buffer.concat(chunks) });
      });
    });
    req.on("error", reject);
  });
}

function request(uri, method) {
  return new Promise(function (resolve, reject) {
    const req = http.request(uri, { method: method }, function (res) {
      const chunks = [];
      res.on("data", function (chunk) { chunks.push(chunk); });
      res.on("end", function () { resolve({ status: res.statusCode, headers: res.headers, body: Buffer.concat(chunks) }); });
    });
    req.on("error", reject);
    req.end();
  });
}

async function waitFor(check) {
  const deadline = Date.now() + 2000;
  while (Date.now() < deadline) {
    if (check()) return;
    await new Promise(function (resolve) { setTimeout(resolve, 5); });
  }
  throw new Error("timed out waiting for test condition");
}

function getAllowAbort(uri) {
  return new Promise(function (resolve) {
    const chunks = [];
    const req = http.get(uri, function (res) {
      res.on("data", function (chunk) { chunks.push(chunk); });
      res.on("end", function () { resolve({ aborted: false, status: res.statusCode, body: Buffer.concat(chunks) }); });
      res.on("aborted", function () { resolve({ aborted: true, status: res.statusCode, body: Buffer.concat(chunks) }); });
      res.on("error", function () { resolve({ aborted: true, status: res.statusCode, body: Buffer.concat(chunks) }); });
    });
    req.on("error", function () { resolve({ aborted: true, status: 0, body: Buffer.concat(chunks) }); });
  });
}

async function testBrowserDownload_StreamsOneUseTicket() {
  const payload = Buffer.from("full bytes beyond preview", "utf8");
  const sourceCalls = [];
  const client = {
    openReadStream: function (path, opts) {
      sourceCalls.push({ path: path, signal: opts.signal });
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: {
          "content-length": String(payload.length),
          "content-disposition": 'attachment; filename="../../report.txt"',
        },
        body: Readable.from([payload]),
      });
    },
  };
  const opened = [];
  let browserResponse;
  let asExternalCalls = 0;
  const vscode = {
    Uri: { parse: function (value) { return { toString: function () { return value; } }; } },
    env: {
      asExternalUri: function () { asExternalCalls++; return Promise.reject(new Error("must not run")); },
      openExternal: function (uri) {
        const value = uri.toString();
        opened.push(value);
        return get(value).then(function (res) { browserResponse = res; return true; });
      },
    },
  };
  let rngBytes = 0;
  const browserDownload = createBrowserDownloader({
    http: http,
    randomBytes: function (n) { rngBytes = n; return Buffer.alloc(n, 0xab); },
  });

  const result = await browserDownload({
    vscode: vscode,
    client: client,
    service: "store",
    segments: ["folder", "report.txt"],
    fallbackName: "..\\..\\fallback.txt",
  });

  assert.deepStrictEqual(result, { ok: true, code: "completed" }, "success waits for browser transfer completion");
  assert.strictEqual(CLAIM_TTL_MS, 30000, "claim TTL is exactly 30 seconds");
  assert.strictEqual(rngBytes, 32, "ticket uses 256 bits from the CSPRNG");
  assert.strictEqual(opened.length, 1, "one local URI is opened");
  const openedURL = new URL(opened[0]);
  assert.strictEqual(openedURL.hostname, "127.0.0.1", "browser handoff uses IPv4 loopback exactly");
  assert.match(openedURL.pathname, /^\/download\/[a-f0-9]{64}$/, "browser URL contains only a 256-bit hex ticket path");
  for (const secret of ["store", "folder", "report.txt", "Bearer", "writeToken", "base64", "b64"]) {
    assert.ok(!opened[0].includes(secret), "browser URI must not contain " + secret);
  }
  assert.strictEqual(asExternalCalls, 0, "local URI goes directly to openExternal without asExternalUri");
  assert.strictEqual(sourceCalls.length, 1, "valid ticket claims exactly one source stream");
  assert.strictEqual(
    sourceCalls[0].path,
    "/api/download?service=store&segs=%5B%22folder%22%2C%22report.txt%22%5D",
    "claimed ticket uses its stored service/path"
  );
  assert.deepStrictEqual(browserResponse.body, payload, "browser receives exact source bytes without base64 transport");
  assert.strictEqual(browserResponse.headers["content-type"], "application/octet-stream");
  assert.strictEqual(browserResponse.headers["content-disposition"], 'attachment; filename="report.txt"');
  assert.strictEqual(browserResponse.headers["x-content-type-options"], "nosniff");
  assert.strictEqual(browserResponse.headers["cache-control"], "no-store");
  assert.strictEqual(browserResponse.headers["referrer-policy"], "no-referrer");
  assert.strictEqual(browserResponse.headers["content-length"], String(payload.length));
}

async function testBrowserDownload_SourceFailureClosesHandoff() {
  const sourceBody = new PassThrough();
  const client = {
    openReadStream: function () {
      setImmediate(function () {
        sourceBody.write(Buffer.from("partial"));
        setImmediate(function () { sourceBody.destroy(new Error("private upstream detail")); });
      });
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { "content-length": "128", "content-disposition": 'attachment; filename="broken.bin"' },
        body: sourceBody,
      });
    },
  };
  let browserRequest;
  const vscode = {
    Uri: { parse: function (value) { return { toString: function () { return value; } }; } },
    env: {
      openExternal: function (uri) {
        browserRequest = new Promise(function (resolve) {
          const chunks = [];
          const req = http.get(uri.toString(), function (res) {
            res.on("data", function (chunk) { chunks.push(chunk); });
            res.on("end", function () { resolve({ aborted: false, body: Buffer.concat(chunks) }); });
            res.on("aborted", function () { resolve({ aborted: true, body: Buffer.concat(chunks) }); });
            res.on("error", function () { resolve({ aborted: true, body: Buffer.concat(chunks) }); });
          });
          req.on("error", function () { resolve({ aborted: true, body: Buffer.concat(chunks) }); });
        });
        return Promise.resolve(true);
      },
    },
  };
  const browserDownload = createBrowserDownloader({
    http: http,
    randomBytes: function (n) { return Buffer.alloc(n, 0xcd); },
  });

  const result = await browserDownload({ vscode: vscode, client: client, service: "store", segments: ["broken.bin"] });
  const browserOutcome = await browserRequest;
  assert.deepStrictEqual(
    result,
    { ok: false, code: "source-failed", message: "The download source failed." },
    "an upstream stream error is distinct from a browser cancellation and never leaks its raw cause"
  );
  assert.strictEqual(browserOutcome.aborted, true, "source failure closes the browser response before declared length");
  assert.deepStrictEqual(browserOutcome.body, Buffer.from("partial"), "fixture proves the error happened after streaming began");
  assert.strictEqual(sourceBody.destroyed, true, "failed upstream body is destroyed during terminal cleanup");
}

async function testBrowserDownload_InvalidContentLengthFailsClosed() {
  for (const length of [undefined, "01", "-1", "12, 13", ["12", "13"]]) {
    const client = {
      openReadStream: function () {
        const headers = { "content-disposition": 'attachment; filename="unknown.bin"' };
        if (length !== undefined) headers["content-length"] = length;
        return Promise.resolve({
          status: 200,
          ok: true,
          headers: headers,
          body: Readable.from([Buffer.from("bytes with no trustworthy declared size")]),
        });
      },
    };
    let browserRequest;
    const vscode = {
      Uri: { parse: function (value) { return { toString: function () { return value; } }; } },
      env: {
        openExternal: function (uri) {
          browserRequest = get(uri.toString());
          return Promise.resolve(true);
        },
      },
    };
    const browserDownload = createBrowserDownloader({
      http: http,
      randomBytes: function (n) { return Buffer.alloc(n, 0xef); },
    });

    const result = await browserDownload({ vscode: vscode, client: client, service: "store", segments: ["unknown.bin"] });
    const browserResponse = await browserRequest;
    assert.deepStrictEqual(
      result,
      { ok: false, code: "source-failed", message: "The download source failed." },
      "Content-Length " + JSON.stringify(length) + " is refused before becoming an attachment"
    );
    assert.notStrictEqual(browserResponse.status, 200, "invalid metadata never emits a successful attachment response");
    assert.strictEqual(browserResponse.headers["content-disposition"], undefined, "invalid metadata never emits attachment headers");
  }
}

async function testBrowserDownload_BrowserAbortCancelsUpstream() {
  const sourceBody = new PassThrough();
  let sourceSignal;
  const client = {
    openReadStream: function (_, opts) {
      sourceSignal = opts.signal;
      setImmediate(function () { sourceBody.write(Buffer.alloc(64 << 10, 0x61)); });
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { "content-length": String(8 << 20), "content-disposition": 'attachment; filename="large.bin"' },
        body: sourceBody,
      });
    },
  };
  let browserClosed = false;
  const vscode = {
    Uri: { parse: function (value) { return { toString: function () { return value; } }; } },
    env: {
      openExternal: function (uri) {
        const req = http.get(uri.toString(), function (res) {
          res.once("data", function () {
            browserClosed = true;
            res.destroy();
          });
        });
        req.on("error", function () {});
        return Promise.resolve(true);
      },
    },
  };
  const browserDownload = createBrowserDownloader({
    http: http,
    randomBytes: function (n) { return Buffer.alloc(n, 0x12); },
  });

  const result = await browserDownload({ vscode: vscode, client: client, service: "store", segments: ["large.bin"] });
  assert.deepStrictEqual(
    result,
    { ok: false, code: "browser-aborted", message: "The browser cancelled the download." },
    "a client socket close is classified as browser cancellation, not source failure"
  );
  assert.strictEqual(browserClosed, true, "fixture closed the browser response after receiving bytes");
  assert.strictEqual(sourceSignal.aborted, true, "browser cancellation aborts the authenticated console request");
  assert.strictEqual(sourceBody.destroyed, true, "browser cancellation destroys the upstream body");
}

async function testBrowserDownload_ClaimIsAtomicAndURLCannotRetargetSource() {
  const payload = Buffer.from("one claimant only");
  const sourceBody = new PassThrough();
  const sourcePaths = [];
  const client = {
    openReadStream: function (path) {
      sourcePaths.push(path);
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { "content-length": String(payload.length), "content-disposition": 'attachment; filename="safe.txt"' },
        body: sourceBody,
      });
    },
  };
  const seen = {};
  const vscode = {
    Uri: { parse: function (value) { return { toString: function () { return value; } }; } },
    env: {
      openExternal: async function (uri) {
        const localURI = uri.toString();
        seen.wrongPath = await get(localURI + "?service=evil&segs=%5B%22retargeted%22%5D");
        seen.wrongMethod = await request(localURI, "POST");
        const winner = get(localURI);
        await waitFor(function () { return sourcePaths.length === 1; });
        seen.duplicate = await get(localURI);
        sourceBody.end(payload);
        seen.winner = await winner;
        return true;
      },
    },
  };
  const browserDownload = createBrowserDownloader({
    http: http,
    randomBytes: function (n) { return Buffer.alloc(n, 0x34); },
  });

  const result = await browserDownload({
    vscode: vscode,
    client: client,
    service: "real-store",
    segments: ["real", "value.txt"],
    fallbackName: "value.txt",
  });
  assert.deepStrictEqual(result, { ok: true, code: "completed" });
  assert.strictEqual(seen.wrongPath.status, 404, "query/path changes are rejected without consuming the ticket");
  assert.strictEqual(seen.wrongMethod.status, 405, "non-GET is rejected without consuming the ticket");
  assert.strictEqual(seen.wrongMethod.headers.allow, "GET", "wrong method advertises the only accepted method");
  assert.strictEqual(seen.duplicate.status, 410, "a concurrent second exact GET loses the atomic claim");
  assert.strictEqual(seen.winner.status, 200, "the first exact GET owns the stream");
  assert.deepStrictEqual(seen.winner.body, payload);
  assert.deepStrictEqual(sourcePaths, [
    "/api/download?service=real-store&segs=%5B%22real%22%2C%22value.txt%22%5D",
  ], "browser URL input cannot replace the service/path captured before listener creation");
}

async function testBrowserDownload_ExpiryAndOpenFailureCloseListener() {
  let timerCallback;
  let timerDelay;
  let expiredURI;
  let sourceCalls = 0;
  const client = { openReadStream: function () { sourceCalls++; throw new Error("must not open"); } };
  const browserDownload = createBrowserDownloader({
    http: http,
    randomBytes: function (n) { return Buffer.alloc(n, 0x56); },
    setTimeout: function (fn, delay) {
      timerCallback = fn;
      timerDelay = delay;
      return { unref: function () {} };
    },
    clearTimeout: function () {},
  });
  const vscode = {
    Uri: { parse: function (value) { return { toString: function () { return value; } }; } },
    env: {
      openExternal: function (uri) {
        expiredURI = uri.toString();
        return new Promise(function () {});
      },
    },
  };
  const pending = browserDownload({ vscode: vscode, client: client, service: "store", segments: ["late.bin"] });
  await waitFor(function () { return !!timerCallback; });
  assert.strictEqual(timerDelay, 30000, "claim expiry is armed for exactly 30 seconds after listen");
  timerCallback();
  assert.deepStrictEqual(
    await pending,
    { ok: false, code: "expired", message: "Download link expired before the browser claimed it." }
  );
  assert.strictEqual(sourceCalls, 0, "an unclaimed/expired ticket never opens the authenticated source");
  await assert.rejects(get(expiredURI), "expiry closes the temporary listener");

  for (const mode of ["false", "reject", "throw"]) {
    let uriValue;
    const failingVscode = {
      Uri: { parse: function (value) { return { toString: function () { return value; } }; } },
      env: {
        openExternal: function (uri) {
          uriValue = uri.toString();
          if (mode === "throw") throw new Error("private synchronous opener detail");
          return mode === "false" ? Promise.resolve(false) : Promise.reject(new Error("private opener detail"));
        },
      },
    };
    const fresh = createBrowserDownloader({ http: http, randomBytes: function (n) { return Buffer.alloc(n, mode === "false" ? 0x67 : mode === "reject" ? 0x78 : 0x79); } });
    const result = await fresh({ vscode: failingVscode, client: client, service: "store", segments: ["never.bin"] });
    assert.strictEqual(result.ok, false);
    assert.strictEqual(result.code, mode === "false" ? "open-rejected" : "open-failed");
    assert.ok(!result.message.includes("private opener detail"), "opener failure result is public/sanitized");
    await assert.rejects(get(uriValue), "failed browser open closes the temporary listener");
  }
}

async function testBrowserDownload_OuterCancellationClosesUnclaimedListener() {
  const aborter = new AbortController();
  let localURI;
  let sourceCalls = 0;
  const client = { openReadStream: function () { sourceCalls++; throw new Error("must not open"); } };
  const vscode = {
    Uri: { parse: function (value) { return { toString: function () { return value; } }; } },
    env: {
      openExternal: function (uri) {
        localURI = uri.toString();
        return new Promise(function () {});
      },
    },
  };
  const browserDownload = createBrowserDownloader({
    http: http,
    randomBytes: function (n) { return Buffer.alloc(n, 0x89); },
  });
  const pending = browserDownload({
    vscode: vscode,
    client: client,
    service: "store",
    segments: ["cancel.bin"],
    signal: aborter.signal,
  });
  await waitFor(function () { return !!localURI; });
  aborter.abort();
  assert.deepStrictEqual(await pending, { ok: false, code: "cancelled", message: "Download cancelled." });
  assert.strictEqual(sourceCalls, 0, "panel cancellation before claim never opens the source");
  await assert.rejects(get(localURI), "panel cancellation closes the unclaimed listener");
}

async function testBrowserDownload_PrematureEOFFailsExactLength() {
  const payload = Buffer.from("short");
  const client = {
    openReadStream: function () {
      return Promise.resolve({
        status: 200,
        ok: true,
        headers: { "content-length": "12", "content-disposition": 'attachment; filename="short.bin"' },
        body: Readable.from([payload]),
      });
    },
  };
  let browserRequest;
  const vscode = {
    Uri: { parse: function (value) { return { toString: function () { return value; } }; } },
    env: {
      openExternal: function (uri) {
        browserRequest = getAllowAbort(uri.toString());
        return Promise.resolve(true);
      },
    },
  };
  const browserDownload = createBrowserDownloader({
    http: http,
    randomBytes: function (n) { return Buffer.alloc(n, 0x9a); },
  });

  const result = await browserDownload({ vscode: vscode, client: client, service: "store", segments: ["short.bin"] });
  const browserOutcome = await browserRequest;
  assert.deepStrictEqual(
    result,
    { ok: false, code: "source-failed", message: "The download source failed." },
    "clean upstream EOF before the declared byte count is still a source failure"
  );
  assert.strictEqual(browserOutcome.aborted, true, "short transfer cannot look complete to the browser");
  assert.deepStrictEqual(browserOutcome.body, payload, "byte counting observes the stream without buffering/re-encoding it");
}

async function testBrowserDownload_SynchronousSourceFailureIsContained() {
  const client = {
    openReadStream: function () { throw new Error("private synchronous source detail"); },
  };
  let browserRequest;
  const vscode = {
    Uri: { parse: function (value) { return { toString: function () { return value; } }; } },
    env: {
      openExternal: function (uri) {
        browserRequest = get(uri.toString());
        return Promise.resolve(true);
      },
    },
  };
  const browserDownload = createBrowserDownloader({
    http: http,
    randomBytes: function (n) { return Buffer.alloc(n, 0xac); },
  });

  const result = await browserDownload({ vscode: vscode, client: client, service: "store", segments: ["sync.bin"] });
  const response = await browserRequest;
  assert.deepStrictEqual(result, { ok: false, code: "source-failed", message: "The download source failed." });
  assert.strictEqual(response.status, 502, "synchronous source failure returns a safe browser response");
  assert.ok(!JSON.stringify(result).includes("private synchronous"), "raw synchronous source failures never escape");
}

async function testVscodeStub_RecordsExternalHandoff() {
  vscodeStub.__reset();
  const uri = vscodeStub.Uri.parse("http://127.0.0.1:1234/download/ticket");
  await vscodeStub.env.asExternalUri(uri);
  await vscodeStub.env.openExternal(uri);
  assert.deepStrictEqual(vscodeStub.__asExternalUris, [uri], "the test stub exposes accidental asExternalUri calls");
  assert.deepStrictEqual(vscodeStub.__openExternalUris, [uri], "the test stub exposes direct openExternal calls");
  vscodeStub.__reset();
  assert.strictEqual(vscodeStub.__asExternalUris.length, 0);
  assert.strictEqual(vscodeStub.__openExternalUris.length, 0);
}

(async function main() {
  await testBrowserDownload_StreamsOneUseTicket();
  await testBrowserDownload_SourceFailureClosesHandoff();
  await testBrowserDownload_InvalidContentLengthFailsClosed();
  await testBrowserDownload_BrowserAbortCancelsUpstream();
  await testBrowserDownload_ClaimIsAtomicAndURLCannotRetargetSource();
  await testBrowserDownload_ExpiryAndOpenFailureCloseListener();
  await testBrowserDownload_OuterCancellationClosesUnclaimedListener();
  await testBrowserDownload_PrematureEOFFailsExactLength();
  await testBrowserDownload_SynchronousSourceFailureIsContained();
  await testVscodeStub_RecordsExternalHandoff();
  console.log("browserdownload.test.js OK");
})().catch(function (err) {
  console.error(err && err.stack ? err.stack : err);
  process.exit(1);
});
