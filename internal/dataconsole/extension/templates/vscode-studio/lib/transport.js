"use strict";

const cp = require("child_process");

const DEFAULT_TIMEOUT_MS = 300000;
const DEFAULT_MAX_BUFFER = 16 * 1024 * 1024;
const ZCP_BIN = "zcp";

class StudioTransportTimeoutError extends Error {
  constructor(args, timeoutMs) {
    super("zcp studio " + String((args && args[0]) || "verb") + " timed out after " + timeoutMs + "ms");
    this.name = "StudioTransportTimeoutError";
    this.code = "ETIMEDOUT";
    this.timeout = true;
    this.args = args || [];
    this.timeoutMs = timeoutMs;
  }
}

class StudioTransportCancelledError extends Error {
  constructor(args) {
    super("zcp studio " + String((args && args[0]) || "verb") + " cancelled");
    this.name = "StudioTransportCancelledError";
    this.code = "ECANCELLED";
    this.cancelled = true;
    this.args = args || [];
  }
}

function asString(value) {
  if (value == null) return "";
  return typeof value === "string" ? value : value.toString();
}

function parseJSON(stdout) {
  const text = asString(stdout).trim();
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch (_) {
    return null;
  }
}

function resultFromError(err, stdout, stderr) {
  const code = err && err.code;
  if (code === "ENOENT") {
    return {
      ok: false,
      stdout: asString(stdout),
      error: (err && err.message) || "zcp not found",
      needsInit: true,
      cause: err,
    };
  }

  const message =
    asString(stderr).trim() ||
    (err && err.message) ||
    ("zcp studio command failed" + (code != null ? " (" + code + ")" : ""));
  return {
    ok: false,
    stdout: asString(stdout),
    error: message,
    cause: err,
  };
}

function addAbortListener(signal, onAbort) {
  if (!signal) return function () {};
  if (signal.aborted) {
    onAbort();
    return function () {};
  }
  if (typeof signal.addEventListener === "function") {
    signal.addEventListener("abort", onAbort, { once: true });
    return function () {
      signal.removeEventListener("abort", onAbort);
    };
  }
  const previous = signal.onabort;
  signal.onabort = function () {
    if (typeof previous === "function") previous.apply(signal, arguments);
    onAbort();
  };
  return function () {
    signal.onabort = previous;
  };
}

function killChild(child) {
  if (child && typeof child.kill === "function") {
    try {
      child.kill();
    } catch (_) {
      /* process already gone */
    }
  }
}

// Async one-shot `zcp studio <verb>` transport. Returns a Promise resolving to
// { ok, stdout, data?, error, needsInit }. `deps.execFile` and related knobs are
// injectable so tests can drive process timing without spawning a real binary.
function runStudioVerb(workspaceRoot, args, deps) {
  deps = deps || {};
  args = Array.isArray(args) ? args : [];
  const execFile = deps.execFile || cp.execFile;
  const bin = deps.bin || ZCP_BIN;
  const timeoutMs = deps.timeoutMs == null ? DEFAULT_TIMEOUT_MS : deps.timeoutMs;
  const maxBuffer = deps.maxBuffer == null ? DEFAULT_MAX_BUFFER : deps.maxBuffer;
  const signal = deps.signal;

  if (signal && signal.aborted) {
    const cancelErr = new StudioTransportCancelledError(args);
    return Promise.resolve({
      ok: false,
      stdout: "",
      error: cancelErr.message,
      cancelled: true,
      cause: cancelErr,
    });
  }

  return new Promise(function (resolve) {
    let child = null;
    let settled = false;
    let timer = null;
    let removeAbortListener = function () {};

    function cleanup() {
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
      removeAbortListener();
      removeAbortListener = function () {};
    }

    function finish(result) {
      if (settled) return;
      settled = true;
      cleanup();
      resolve(result);
    }

    function timeout() {
      const err = new StudioTransportTimeoutError(args, timeoutMs);
      killChild(child);
      finish({
        ok: false,
        stdout: "",
        error: err.message,
        timeout: true,
        cause: err,
      });
    }

    function cancel() {
      const err = new StudioTransportCancelledError(args);
      killChild(child);
      finish({
        ok: false,
        stdout: "",
        error: err.message,
        cancelled: true,
        cause: err,
      });
    }

    try {
      child = execFile(
        bin,
        ["studio"].concat(args),
        {
          cwd: workspaceRoot,
          encoding: "utf8",
          maxBuffer: maxBuffer,
        },
        function (err, stdout, stderr) {
          if (err) {
            finish(resultFromError(err, stdout, stderr));
            return;
          }
          const out = asString(stdout);
          finish({ ok: true, stdout: out, data: parseJSON(out) });
        }
      );
    } catch (err) {
      finish(resultFromError(err, "", ""));
      return;
    }

    if (!settled && timeoutMs > 0) {
      timer = setTimeout(timeout, timeoutMs);
    }

    if (!settled) {
      removeAbortListener = addAbortListener(signal, cancel);
    }
  });
}

module.exports = {
  runStudioVerb: runStudioVerb,
  StudioTransportTimeoutError: StudioTransportTimeoutError,
  StudioTransportCancelledError: StudioTransportCancelledError,
};
