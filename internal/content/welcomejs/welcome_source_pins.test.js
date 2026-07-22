"use strict";

// Source-string pins for two P2 changes to welcome.js (docs/spec-welcome-mode.md
// §4/§7): the bridge-support gate no longer duplicates the Zerops GUI
// receiver's own capability list (bridge_flow.test.js carries the
// behavioral proof of the gate itself), and EXTERNAL_URLS carries exactly
// the eleven verified-live doc/recipe/video links, no placeholder TODO.

const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const assert = require("node:assert/strict");
const { TEMPLATES_DIR } = require("./harness.js");

function welcomeSource() {
  return fs.readFileSync(path.join(TEMPLATES_DIR, "vscode-bootstrap-welcome.js"), "utf8");
}

test("welcome.js source no longer mentions BRIDGE_SUPPORTED_AGENTS", () => {
  assert.doesNotMatch(welcomeSource(), /BRIDGE_SUPPORTED_AGENTS/);
});

const EXTERNAL_URLS = [
  "https://docs.zerops.io",
  "https://docs.zerops.io/quickstart",
  "https://docs.zerops.io/features/coding-agents",
  "https://docs.zerops.io/zcp/quickstart",
  "https://docs.zerops.io/features/infrastructure",
  "https://docs.zerops.io/features/scaling",
  "https://docs.zerops.io/features/env-variables",
  "https://docs.zerops.io/zerops-yaml/specification",
  "https://app.zerops.io/recipes",
  "https://app.zerops.io/recipes/showcase-recipe",
  "https://www.youtube.com/watch?v=spdmTicsIgg",
];

test("EXTERNAL_URLS carries exactly the eleven verified-live URLs, no placeholder", () => {
  const src = welcomeSource();
  for (const url of EXTERNAL_URLS) {
    assert.ok(src.includes(`"${url}"`), `expected ${url} in EXTERNAL_URLS`);
  }
  assert.doesNotMatch(src, /"https:\/\/zerops\.io"/, "the bare placeholder URL must be gone");
});
