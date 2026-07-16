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

// TEST_REGISTRY/TEST_AGENT_IDS mirror the shape (id/label/suffix) of
// extension.js's real REGISTRY/ALL_AGENT_IDS — which extension.js does not
// export — so state-shape tests can drive buildState()/collectors with
// realistic per-agent suffixes without duplicating the fixture per file.
const TEST_REGISTRY = {
  "claude-code": { id: "claude-code", label: "Claude Code", suffix: "CLAUDE_CODE" },
  "codex": { id: "codex", label: "Codex", suffix: "CODEX" },
  "antigravity": { id: "antigravity", label: "Antigravity", suffix: "ANTIGRAVITY" },
  "grok": { id: "grok", label: "Grok", suffix: "GROK" },
  "cursor": { id: "cursor", label: "Cursor", suffix: "CURSOR" },
};
const TEST_AGENT_IDS = ["claude-code", "codex", "antigravity", "grok", "cursor"];

module.exports = { loadExtension, loadWelcome, TEMPLATES_DIR, TEST_REGISTRY, TEST_AGENT_IDS };
