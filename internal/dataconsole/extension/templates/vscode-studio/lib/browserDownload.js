"use strict";

// A browser download cannot be initiated from the Data Console webview itself:
// its CSP deliberately has no connect-src and webviews do not honor <a download>
// as a normal top-level browser download. This module owns a tiny, temporary
// loopback handoff. The browser sees only a one-use random ticket; the extension
// host keeps the console bearer and the fixed source address private.

const crypto = require("crypto");
const http = require("http");
const { pipeline, Transform } = require("stream");

const CLAIM_TTL_MS = 30000;
const TICKET_BYTES = 32;
const LISTEN_HOST = "127.0.0.1";

function safeFilename(value) {
  let name = String(value || "").replace(/\\/g, "/");
  name = name.slice(name.lastIndexOf("/") + 1);
  name = name.replace(/[\u0000-\u001f\u007f]/g, "").trim();
  if (!name || name === "." || name === "..") name = "download";
  // Keep the response header bounded even when a compromised webview supplies
  // an absurd name. The UTF-8 filename* parameter below preserves normal names.
  return name.slice(0, 180);
}

function dispositionFilename(value) {
  const header = String(value || "");
  if (/[\u0000-\u001f\u007f]/.test(header) || !/^attachment(?:\s*;|$)/i.test(header)) return "";
  let match = /(?:^|;)\s*filename\*=UTF-8''([^;]+)/i.exec(header);
  if (match) {
    try { return decodeURIComponent(match[1]); } catch (_) { return ""; }
  }
  match = /(?:^|;)\s*filename="((?:\\.|[^"])*)"/i.exec(header);
  if (match) return match[1].replace(/\\(.)/g, "$1");
  match = /(?:^|;)\s*filename=([^;]+)/i.exec(header);
  return match ? match[1].trim() : "";
}

function attachmentDisposition(upstreamValue, fallbackName) {
  const name = safeFilename(dispositionFilename(upstreamValue) || fallbackName);
  const ascii = name.replace(/[^\x20-\x7e]|["\\]/g, "_");
  const encoded = encodeURIComponent(Buffer.from(name, "utf8").toString("utf8"))
    .replace(/[!'()*]/g, function (c) { return "%" + c.charCodeAt(0).toString(16).toUpperCase(); });
  if (ascii === name) return 'attachment; filename="' + ascii + '"';
  return 'attachment; filename="' + ascii + '"; filename*=UTF-8\'\'' + encoded;
}

function publicFailure(code) {
  const messages = {
    cancelled: "Download cancelled.",
    expired: "Download link expired before the browser claimed it.",
    "open-rejected": "The browser did not accept the download.",
    "open-failed": "The browser could not be opened.",
    "source-failed": "The download source failed.",
    "browser-aborted": "The browser cancelled the download.",
  };
  return { ok: false, code: code, message: messages[code] || "Download failed." };
}

function createBrowserDownloader(deps) {
  deps = deps || {};
  const httpMod = deps.http || http;
  const randomBytes = deps.randomBytes || crypto.randomBytes;
  const pipelineFn = deps.pipeline || pipeline;
  const setTimer = deps.setTimeout || setTimeout;
  const clearTimer = deps.clearTimeout || clearTimeout;

  return function browserDownload(opts) {
    opts = opts || {};
    const vscode = opts.vscode;
    const client = opts.client;
    const service = String(opts.service || "");
    const segments = Array.isArray(opts.segments) ? opts.segments.map(function (s) { return String(s); }) : [];
    const fallbackName = safeFilename(opts.fallbackName);
    const sourcePath = "/api/download?service=" + encodeURIComponent(service) +
      "&segs=" + encodeURIComponent(JSON.stringify(segments));
    const ticket = randomBytes(TICKET_BYTES).toString("hex");
    const ticketPath = "/download/" + ticket;
    const outerSignal = opts.signal;

    return new Promise(function (resolve) {
      let state = "starting";
      let terminal = false;
      let claimTimer = null;
      let externalAccepted = false;
      let transferFinished = false;
      let upstreamBody = null;
      let sourceFailed = false;
      let browserResponse = null;
      let sourceAbort = null;
      const sockets = new Set();

      const server = httpMod.createServer(function (req, res) {
        res.setHeader("Connection", "close");
        if (req.url !== ticketPath) {
          res.statusCode = 404;
          res.end("Not found.");
          return;
        }
        if (req.method !== "GET") {
          res.statusCode = 405;
          res.setHeader("Allow", "GET");
          res.end("Method not allowed.");
          return;
        }
        // Atomic in Node's single event-loop turn: state changes before the
        // promise that opens the source is created, so a concurrent duplicate
        // can never observe pending after this point.
        if (state !== "listening") {
          res.statusCode = 410;
          res.end("Download ticket is no longer available.");
          return;
        }
        state = "claimed";
        browserResponse = res;
        if (claimTimer) {
          clearTimer(claimTimer);
          claimTimer = null;
        }
        sourceAbort = new AbortController();

        res.once("close", function () {
          if (!res.writableFinished && !terminal) {
            finalize(publicFailure(sourceFailed ? "source-failed" : "browser-aborted"));
          }
        });

        Promise.resolve()
          .then(function () { return client.openReadStream(sourcePath, { signal: sourceAbort.signal }); })
          .then(function (source) {
            if (terminal) {
              if (source && source.body && typeof source.body.destroy === "function") source.body.destroy();
              return;
            }
            if (!source || !source.ok || !source.body) {
              if (source && source.body && typeof source.body.destroy === "function") source.body.destroy();
              respondSourceFailure();
              return;
            }
            upstreamBody = source.body;
            upstreamBody.once("error", function () { sourceFailed = true; });
            upstreamBody.once("aborted", function () { sourceFailed = true; });
            const length = source.headers && source.headers["content-length"];
            // S3 guarantees an exact non-negative length. Fail closed if that
            // contract is ever weakened or a malformed/multi-value header
            // reaches the bridge: a successful attachment with no exact size
            // could silently truncate while looking complete to the browser.
            if (typeof length !== "string" || !/^(0|[1-9][0-9]*)$/.test(length)) {
              upstreamBody.destroy();
              respondSourceFailure();
              return;
            }
            const declaredLength = BigInt(length);
            let transferredBytes = 0n;
            const byteCounter = new Transform({
              transform: function (chunk, encoding, callback) {
                const size = Buffer.isBuffer(chunk) ? chunk.length : Buffer.byteLength(chunk, encoding);
                transferredBytes += BigInt(size);
                if (transferredBytes > declaredLength) {
                  sourceFailed = true;
                  callback(new Error("download source exceeded declared length"));
                  return;
                }
                callback(null, chunk);
              },
            });
            state = "streaming";
            res.statusCode = 200;
            res.setHeader("Content-Type", "application/octet-stream");
            res.setHeader("Content-Disposition", attachmentDisposition(source.headers && source.headers["content-disposition"], fallbackName));
            res.setHeader("X-Content-Type-Options", "nosniff");
            res.setHeader("Cache-Control", "no-store");
            res.setHeader("Referrer-Policy", "no-referrer");
            res.setHeader("Content-Length", length);
            pipelineFn(upstreamBody, byteCounter, res, function (err) {
              if (terminal) return;
              if (err) {
                finalize(publicFailure(sourceFailed ? "source-failed" : "browser-aborted"));
                return;
              }
              if (transferredBytes !== declaredLength) {
                sourceFailed = true;
                finalize(publicFailure("source-failed"));
                return;
              }
              transferFinished = true;
              maybeComplete();
            });
          })
          .catch(function () {
            if (!terminal) respondSourceFailure();
          });

        function respondSourceFailure() {
          if (terminal || res.headersSent) {
            finalize(publicFailure("source-failed"));
            return;
          }
          res.statusCode = 502;
          res.setHeader("Content-Type", "text/plain; charset=utf-8");
          res.setHeader("X-Content-Type-Options", "nosniff");
          res.setHeader("Cache-Control", "no-store");
          res.setHeader("Referrer-Policy", "no-referrer");
          res.end("Download failed.", function () { finalize(publicFailure("source-failed")); });
        }
      });

      server.on("connection", function (socket) {
        sockets.add(socket);
        socket.once("close", function () { sockets.delete(socket); });
      });
      server.on("clientError", function (_, socket) {
        if (socket && socket.writable) socket.end("HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n");
      });
      server.once("error", function () {
        finalize(publicFailure("open-failed"));
      });

      function removeOuterAbort() {
        if (outerSignal && typeof outerSignal.removeEventListener === "function") {
          outerSignal.removeEventListener("abort", cancelOuter);
        }
      }

      function closeAndResolve(result) {
        function done() { resolve(result); }
        if (server.listening) {
          try { server.close(done); } catch (_) { done(); }
        } else {
          done();
        }
      }

      function finalize(result) {
        if (terminal) return;
        terminal = true;
        state = result.ok ? "completed" : result.code;
        if (claimTimer) {
          clearTimer(claimTimer);
          claimTimer = null;
        }
        removeOuterAbort();
        if (!result.ok) {
          if (sourceAbort) sourceAbort.abort();
          if (upstreamBody && typeof upstreamBody.destroy === "function") upstreamBody.destroy();
          if (browserResponse && !browserResponse.writableFinished && typeof browserResponse.destroy === "function") {
            browserResponse.destroy();
          }
          sockets.forEach(function (socket) { socket.destroy(); });
        }
        closeAndResolve(result);
      }

      function maybeComplete() {
        if (externalAccepted && transferFinished) finalize({ ok: true, code: "completed" });
      }

      function cancelOuter() {
        finalize(publicFailure("cancelled"));
      }

      if (outerSignal && outerSignal.aborted) {
        finalize(publicFailure("cancelled"));
        return;
      }
      if (outerSignal && typeof outerSignal.addEventListener === "function") {
        outerSignal.addEventListener("abort", cancelOuter, { once: true });
      }

      server.listen(0, LISTEN_HOST, function () {
        if (terminal) return;
        state = "listening";
        claimTimer = setTimer(function () { finalize(publicFailure("expired")); }, CLAIM_TTL_MS);
        if (claimTimer && typeof claimTimer.unref === "function") claimTimer.unref();
        const address = server.address();
        const localURI = "http://" + LISTEN_HOST + ":" + address.port + ticketPath;
        let uri;
        try {
          uri = vscode.Uri.parse(localURI);
        } catch (_) {
          finalize(publicFailure("open-failed"));
          return;
        }
        Promise.resolve()
          .then(function () { return vscode.env.openExternal(uri); })
          .then(function (accepted) {
            if (terminal) return;
            if (accepted !== true) {
              finalize(publicFailure("open-rejected"));
              return;
            }
            externalAccepted = true;
            maybeComplete();
          })
          .catch(function () {
            finalize(publicFailure("open-failed"));
          });
      });
    });
  };
}

const browserDownload = createBrowserDownloader();

module.exports = {
  CLAIM_TTL_MS: CLAIM_TTL_MS,
  browserDownload: browserDownload,
  createBrowserDownloader: createBrowserDownloader,
};
