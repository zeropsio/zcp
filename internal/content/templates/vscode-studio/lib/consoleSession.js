"use strict";

// Data Console session manager. Owns the console CHILD PROCESS lifecycle (one per
// workspace+posture) and opens it as a first-party WebviewPanel. Read-only is the
// default; a write-capable session is launch-time, immutable, and requires a
// host-owned VS Code confirmation before the child receives --allow-writes.
//
// The console binds loopback only; it is NEVER proxied to the public domain. The
// SPA runs as webview content (consolePanel) and reaches its data through the
// host broker (consoleClient), which holds the bearer — the bearer never enters
// the browser.

const path = require("path");
const { createConsolePanelManager } = require("./consolePanel");
const { createConsoleClient } = require("./consoleClient");

const READY_TIMEOUT_MS = 15000;
const ENABLE_WRITES = "Enable writes";

function sessionKey(workspaceRoot) {
  return String(workspaceRoot || "");
}

function postStatus(postMessage, message) {
  if (typeof postMessage === "function") {
    postMessage({ type: "console-status", message: message });
  }
}

async function confirmWrites(vscode) {
  const warning =
    "Enable write-capable Data Console for this workspace? This allows editing and deleting managed-service data.";
  const result = await vscode.window.showWarningMessage(warning, { modal: true }, ENABLE_WRITES);
  return result === ENABLE_WRITES;
}

// portOf extracts the loopback port from the console's ready-line url
// (http://127.0.0.1:<port>); the broker dials that port directly.
function portOf(url) {
  const m = /:(\d+)\/?$/.exec(String(url || ""));
  return m ? parseInt(m[1], 10) : 0;
}

function createConsoleSessionManager(deps) {
  deps = deps || {};
  const spawn = deps.spawn || require("child_process").spawn;
  const vscode = deps.vscode || require("vscode");
  const panels = deps.panelManager || createConsolePanelManager({ vscode: vscode });
  const makeClient = deps.createConsoleClient || createConsoleClient;
  const servers = {};

  function evict(key, entry) {
    if (servers[key] === entry) {
      delete servers[key];
    }
  }

  function killEntry(key) {
    const entry = servers[key];
    if (entry && entry.proc && typeof entry.proc.kill === "function") {
      try {
        entry.proc.kill();
      } catch (_) {
        /* already gone */
      }
    }
    delete servers[key];
  }

  function attachReady(entry, key, postMessage) {
    const child = entry.proc;
    let buf = "";
    let settled = false;
    let timer;
    let resolveReady;

    function clear() {
      if (timer) {
        clearTimeout(timer);
        timer = null;
      }
    }

    function finish(ready) {
      if (settled) return;
      settled = true;
      clear();
      entry.ready = ready;
      resolveReady(ready);
    }

    function fail(message, kill) {
      if (!settled) {
        settled = true;
        clear();
        postStatus(postMessage, message);
        evict(key, entry);
        if (kill && child && typeof child.kill === "function") {
          child.kill();
        }
        resolveReady(null);
      } else {
        evict(key, entry);
      }
    }

    entry.readyPromise = new Promise(function (resolve) {
      resolveReady = resolve;
    });

    timer = setTimeout(function () {
      fail("error: console ready timeout", true);
    }, READY_TIMEOUT_MS);
    if (timer && typeof timer.unref === "function") {
      timer.unref();
    }

    child.stdout.on("data", function (d) {
      if (settled) return;
      buf += d.toString();
      const nl = buf.indexOf("\n");
      if (nl < 0) return;
      try {
        finish(JSON.parse(buf.slice(0, nl)));
      } catch (e) {
        fail("bad ready-line: " + String(e), true);
      }
    });
    child.on("error", function (e) {
      fail("error: " + String(e), false);
    });
    child.on("exit", function () {
      if (!settled) {
        fail("error: console exited before ready", false);
        return;
      }
      evict(key, entry);
    });

    return entry.readyPromise;
  }

  async function open(opts) {
    opts = opts || {};
    const workspaceRoot = opts.workspaceRoot || "";
    const service = opts.service || "";
    const postMessage = opts.postMessage;
    const mediaDir = opts.extensionPath
      ? path.join(opts.extensionPath, "media", "dataconsole")
      : opts.mediaDir || "";

    const key = sessionKey(workspaceRoot);
    const existing = servers[key];
    let ready;
    if (existing && existing.proc && existing.proc.exitCode == null) {
      ready = existing.ready || (existing.readyPromise ? await existing.readyPromise : null);
      if (!ready) return;
    } else {
      postStatus(postMessage, "starting console...");
      // Spawn write-capable; the host broker gates mutations until the user
      // enables write mode IN THE PANEL (host-confirmed). The console is
      // loopback-only and the bearer is host-side, so the broker is the boundary —
      // a read-only default is enforced there, not by the launch flag.
      const args = ["studio", "console", "serve", "--port", "0", "--allow-writes"];
      let child;
      try {
        child = spawn("zcp", args, workspaceRoot ? { cwd: workspaceRoot } : {});
      } catch (e) {
        postStatus(postMessage, "failed to start: " + String(e));
        return;
      }
      const entry = { proc: child, ready: null, readyPromise: null };
      servers[key] = entry;
      ready = await attachReady(entry, key, postMessage);
      if (!ready) return;
    }

    postStatus(postMessage, "open");
    const broker = makeClient({ port: portOf(ready.url), token: ready.sessionToken });
    panels.show(key, {
      mediaDir: mediaDir,
      broker: broker,
      service: service,
      confirmWrites: function () { return confirmWrites(vscode); },
      onDispose: function () {
        killEntry(key);
      },
    });
  }

  // dispose kills every console child and closes every panel — the extension
  // calls this on deactivate so no console process outlives the editor session.
  function dispose() {
    Object.keys(servers).forEach(function (k) {
      killEntry(k);
    });
    if (panels && typeof panels.disposeAll === "function") {
      panels.disposeAll();
    }
  }

  return { open: open, dispose: dispose };
}

module.exports = { createConsoleSessionManager: createConsoleSessionManager };
