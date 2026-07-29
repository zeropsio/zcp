"use strict";

// Loads the REAL shipped zcp-bootstrap templates (internal/content/templates)
// against a fresh vscode-stub.js instance, so this suite proves the actual
// artifact zcp ships, not a copy. Each call to loadExtension() copies the
// templates into a brand-new tmp dir under their INSTALLED sibling names
// (mirroring internal/init/adapters/claude.go:installBootstrapExtension) —
// this gives two things at once: `require("./welcome.js")` resolves exactly
// as it does once installed, and every test gets an uncached module
// instance, so a module-level singleton (the launcher panel, the welcome
// panel) can never leak state from one test into the next.

const fs = require("fs");
const os = require("os");
const path = require("path");
const Module = require("module");
const { createVscodeStub } = require("./vscode-stub.js");

const TEMPLATES_DIR = path.join(__dirname, "..", "templates");

// tmplName -> the sibling filename the Go installer writes it under inside
// the extension dir. Only the files this suite's `require`/`fs.readFileSync`
// calls actually touch at runtime need to be here (package.json/logo.svg are
// installer-only concerns, pinned by the Go tests instead).
const TEMPLATE_FILES = {
  "vscode-bootstrap-extension.js": "extension.js",
  "vscode-bootstrap-welcome.js": "welcome.js",
  "vscode-bootstrap-welcome.html": "welcome.html",
  "vscode-bootstrap-package.json": "package.json",
};

let currentStub = null;
let hooked = false;

// installHook redirects every `require("vscode")` to whichever stub
// loadExtension() most recently created — installed once per process and
// left in place; every other request passes through to the real loader
// unchanged (relative requires like "./welcome.js" and builtins like "fs"
// are never touched).
function installHook() {
  if (hooked) return;
  hooked = true;
  const orig = Module._load;
  Module._load = function (request, parent, isMain) {
    if (request === "vscode") {
      if (!currentStub) throw new Error("require(\"vscode\") with no active welcomejs stub — call loadExtension() first");
      return currentStub.exports;
    }
    return orig.call(this, request, parent, isMain);
  };
}

// loadExtension copies the real templates into a fresh tmp dir (skipping any
// not yet authored, so a test can meaningfully RED against a template that
// doesn't exist yet instead of failing opaquely on ENOENT) and requires
// extension.js from there. welcome.js is NOT required here — extension.js
// only requires it lazily, inside the registered command handler, which is
// exactly the behavior the dark tests assert.
function loadExtension() {
  installHook();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-welcomejs-"));
  for (const [tmplName, siblingName] of Object.entries(TEMPLATE_FILES)) {
    const src = path.join(TEMPLATES_DIR, tmplName);
    if (!fs.existsSync(src)) continue;
    fs.copyFileSync(src, path.join(dir, siblingName));
  }
  currentStub = createVscodeStub();
  const extension = require(path.join(dir, "extension.js"));
  return { stub: currentStub, extensionDir: dir, extension };
}

// loadWelcome copies welcome.js + welcome.html only (no extension.js) into a
// fresh tmp dir and requires welcome.js DIRECTLY. Production's only call site
// (extension.js's zerops.welcome handler) passes a fixed deps object with no
// test-only overrides (homeDir, workspaceRoot, fs) — see
// docs/spec-welcome-mode.md §3 — so tests exercising those overrides call
// welcome.open() themselves instead of going through extension.js's handler.
function loadWelcome() {
  installHook();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-welcomejs-"));
  for (const [tmplName, siblingName] of Object.entries(TEMPLATE_FILES)) {
    if (tmplName === "vscode-bootstrap-extension.js") continue;
    const src = path.join(TEMPLATES_DIR, tmplName);
    if (!fs.existsSync(src)) continue;
    fs.copyFileSync(src, path.join(dir, siblingName));
  }
  currentStub = createVscodeStub();
  const welcome = require(path.join(dir, "welcome.js"));
  return { stub: currentStub, extensionDir: dir, welcome };
}

// TEST_REGISTRY/TEST_AGENT_IDS mirror the shape (id/label/suffix/bin/opens)
// of extension.js's real REGISTRY/ALL_AGENT_IDS — which extension.js does
// not export — so state-shape tests can drive buildState()/collectors with
// realistic per-agent suffixes without duplicating the fixture per file.
// `bin` mirrors the real registry's PATH-probed executable name
// (isAgentInstalled reads it via deps.REGISTRY[id].bin, see
// collectInstalled/isAgentActionable in welcome.js). `opens[0].mode` mirrors
// the real registry's launch-mode-per-agent shape (claude-code: extension
// first, terminal fallback; everyone else: terminal only) — the CTA flow
// (cta_flow.test.js) reads exactly this field, via deps.REGISTRY, to call
// deps.runAgentAction(agentEntry, agentEntry.opens[0].mode).
const TEST_REGISTRY = {
  "claude-code": { id: "claude-code", label: "Claude Code", suffix: "CLAUDE_CODE", bin: "claude", opens: [{ mode: "extension" }, { mode: "terminal" }] },
  "codex": { id: "codex", label: "Codex", suffix: "CODEX", bin: "codex", opens: [{ mode: "terminal" }] },
  "antigravity": { id: "antigravity", label: "Antigravity", suffix: "ANTIGRAVITY", bin: "agy", opens: [{ mode: "terminal" }] },
  "grok": { id: "grok", label: "Grok Build", suffix: "GROK", bin: "grok", opens: [{ mode: "terminal" }] },
  "cursor": { id: "cursor", label: "Cursor CLI", suffix: "CURSOR", bin: "cursor-agent", opens: [{ mode: "terminal" }] },
};
const TEST_AGENT_IDS = ["claude-code", "codex", "antigravity", "grok", "cursor"];

// installFakeAgentBins makes every registry agent binary (claude, codex,
// agy, grok, cursor-agent) resolvable as an executable on process.env.PATH
// for the duration of a test file. Tests that drive the REAL production
// wiring (loadExtension() + the zerops.welcome command handler) reach the
// real isAgentInstalled PATH probe — without this, such a test would pass or
// fail depending on which agent CLIs happen to be installed on the machine
// running the suite (CI has none of them). Returns a restore() that puts the
// original PATH back; call it from test.after().
function installFakeAgentBins() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-welcomejs-bins-"));
  for (const bin of ["claude", "codex", "agy", "grok", "cursor-agent"]) {
    const p = path.join(dir, bin);
    fs.writeFileSync(p, "#!/bin/sh\nexit 0\n");
    fs.chmodSync(p, 0o755);
  }
  const originalPath = process.env.PATH;
  process.env.PATH = dir + path.delimiter + (originalPath || "");
  return function restore() {
    process.env.PATH = originalPath;
  };
}

// makeFakeTimers stands in for deps.setTimeout/deps.clearTimeout (the ACK
// and 10-minute cap timers in welcome.js's auth flow, bridge_flow.test.js +
// terminal_flow.test.js): every setTimeout call is recorded — so a test can
// assert the exact delay used, e.g. pinning the 3000ms ACK timeout or the
// 10-minute flow cap — without waiting on a real timer. fire(id) invokes a
// still-pending callback synchronously, as if the delay had elapsed;
// clearTimeout'd or already-fired ids are no-ops, matching real
// setTimeout/clearTimeout semantics. Returned ids are plain numbers (no
// .unref()), which welcome.js's unrefTimer() guard already tolerates.
function makeFakeTimers() {
  let nextId = 1;
  const pending = new Map(); // id -> fn
  const calls = []; // {id, ms} in call order, including already-fired/cleared ones
  return {
    setTimeout: (fn, ms) => {
      const id = nextId++;
      pending.set(id, fn);
      calls.push({ id, ms });
      return id;
    },
    clearTimeout: (id) => { pending.delete(id); },
    fire: (id) => {
      const fn = pending.get(id);
      if (!fn) return false;
      pending.delete(id);
      fn();
      return true;
    },
    calls,
    pendingCount: () => pending.size,
  };
}

// makeFakeClock stands in for deps.now() (the §4.3 dedup-store retention
// floor + the §4.1 inbound-command freshness check): a plain controllable
// millisecond counter, advanced explicitly by a test rather than racing a
// real clock. start defaults to a fixed, arbitrary instant (never 0 — a real
// createdAt of exactly epoch-0 should behave like any other timestamp).
function makeFakeClock(start) {
  let current = typeof start === "number" ? start : 1_700_000_000_000;
  return {
    now: () => current,
    advance(ms) { current += ms; },
    set(ms) { current = ms; },
  };
}

// loadWebviewDom executes the REAL shipped welcome.html — nonce substituted,
// its inline <script> run for real via jsdom (`runScripts: "dangerously"`) —
// so a suite that needs the webview's own rendering/interaction logic (the
// panel-structure row-state matrix, a11y focus handling, the Customize
// picker) drives the actual artifact zcp ships rather than re-implementing
// its logic. This is the ONLY suite family in this directory that executes
// the webview script; every other file (bridge_relay_ratelimit.test.js,
// command_channel.test.js's own html source-pin section, welcome_panel.test.js)
// deliberately reads the template as a source string instead, a discipline
// that predates jsdom's addition here and stays correct for source-only
// pins (CSP/nonce shape, exact-string mechanism guards).
//
// `acquireVsCodeApi()` is a real VS Code webview global with no jsdom
// equivalent — stubbed via `beforeParse` so it exists BEFORE the inline
// script (which calls it at top level) ever runs. `postMessage` calls are
// captured into `sentMessages`, in call order, exactly as the extension host
// would receive them via `webview.onDidReceiveMessage`.
//
// `postToWebview` delivers one host->webview push SYNCHRONOUSLY — a real
// `MessageEvent` dispatched directly on `window`, not `window.postMessage`
// (which jsdom, like a real browser, delivers on a later task and would
// force every test to await an arbitrary tick). This mirrors how VS Code
// actually delivers `webview.postMessage(...)` into the webview's `window`
// "message" listener.
//
// `location.ancestorOrigins` has no jsdom implementation (a Chromium-only
// API) — the script's own try/catch already degrades that to "ancestorOrigins
// unavailable — fall through to reveal" (see welcome.html), so every DOM
// loaded this way starts revealed (data-preload removed), matching a
// standalone (non-embedded) receiver — exactly the shape this suite family
// needs (no receiver-lifecycle dark-waiting to route around).
function loadWebviewDom() {
  const { JSDOM } = require("jsdom");
  const raw = fs.readFileSync(path.join(TEMPLATES_DIR, "vscode-bootstrap-welcome.html"), "utf8");
  const html = raw.split("__CSP_NONCE__").join("test-nonce");
  const sentMessages = [];
  const dom = new JSDOM(html, {
    runScripts: "dangerously",
    pretendToBeVisual: true,
    url: "https://zcp-welcomejs.invalid/",
    beforeParse(window) {
      window.acquireVsCodeApi = () => ({
        postMessage: (msg) => sentMessages.push(msg),
        getState: () => undefined,
        setState: () => {},
      });
    },
  });
  const { window } = dom;
  function postToWebview(payload) {
    window.dispatchEvent(new window.MessageEvent("message", { data: payload }));
  }
  return { dom, window, document: window.document, sentMessages, postToWebview };
}

module.exports = {
  loadExtension, loadWelcome, loadWebviewDom, TEMPLATES_DIR, TEST_REGISTRY, TEST_AGENT_IDS,
  makeFakeTimers, makeFakeClock, installFakeAgentBins,
};
