"use strict";

// Source-string pins for two changes to welcome.js: the bridge-support gate
// no longer duplicates the Zerops GUI receiver's own capability list
// (bridge_flow.test.js carries the behavioral proof of the gate itself),
// and EXTERNAL_URLS is re-derived from the §6 panel's actual link set — the
// walk-through/CTA surface's ten doc/recipe/video links are deleted with it
// (docs/spec-welcome-mode.md §11); only the diagnostics footer's "Zerops
// docs" link survives.

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

test("EXTERNAL_URLS carries exactly the one surviving panel link, no placeholder, no retired walk-through URLs", () => {
  const src = welcomeSource();
  assert.ok(src.includes('"https://docs.zerops.io"'), "expected the docs link in EXTERNAL_URLS");
  assert.doesNotMatch(src, /"https:\/\/zerops\.io"/, "the bare placeholder URL must be gone");
  for (const retired of [
    "https://docs.zerops.io/quickstart",
    "https://docs.zerops.io/features/coding-agents",
    "https://docs.zerops.io/zcp/quickstart",
    "https://app.zerops.io/recipes",
    "https://www.youtube.com/watch?v=spdmTicsIgg",
  ]) {
    assert.doesNotMatch(src, new RegExp(retired.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")), `retired walk-through URL ${retired} must be gone`);
  }
});
