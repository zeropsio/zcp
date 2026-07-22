"use strict";

// Single-model launcher axes that replaced legacy ZCP_AGENT_TYPES filtering:
//   availability — resolveAvailableAgentIds(env) reads ZCP_AGENTS, a zcp-owned
//   PRESENTATION env (which agents this container offers, in which order) —
//   never authorization, never a security boundary. Auth stays the per-agent
//   envs; "installed" (below) stays the PATH probe.
//   installed — isAgentInstalled(bin) probes THIS process's own
//   process.env.PATH for an executable file. No shell, no child process.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { loadExtension, TEMPLATES_DIR } = require("./harness.js");

const ALL_FIVE = ["claude-code", "codex", "antigravity", "grok", "cursor"];

// ---- resolveAvailableAgentIds ---------------------------------------------

test("resolveAvailableAgentIds: null store (no zembed store at all) offers all five in canonical order", () => {
  const { extension } = loadExtension();
  assert.deepStrictEqual(extension.resolveAvailableAgentIds(null), ALL_FIVE);
});

test("resolveAvailableAgentIds: a store without the ZCP_AGENTS key offers all five", () => {
  const { extension } = loadExtension();
  assert.deepStrictEqual(extension.resolveAvailableAgentIds({ serviceId: "svc-1" }), ALL_FIVE);
});

test('resolveAvailableAgentIds: "codex, claude-code" keeps exactly those two, order preserved', () => {
  const { extension } = loadExtension();
  assert.deepStrictEqual(extension.resolveAvailableAgentIds({ ZCP_AGENTS: "codex, claude-code" }), ["codex", "claude-code"]);
});

test("resolveAvailableAgentIds: trims, lowercases, dedupes, and drops unknown tokens", () => {
  const { extension } = loadExtension();
  assert.deepStrictEqual(extension.resolveAvailableAgentIds({ ZCP_AGENTS: " CODEX ,codex,unknown , grok" }), ["codex", "grok"]);
});

test("resolveAvailableAgentIds: an empty string yields zero agents", () => {
  const { extension } = loadExtension();
  assert.deepStrictEqual(extension.resolveAvailableAgentIds({ ZCP_AGENTS: "" }), []);
});

test("resolveAvailableAgentIds: a value with only unknown tokens yields zero agents", () => {
  const { extension } = loadExtension();
  assert.deepStrictEqual(extension.resolveAvailableAgentIds({ ZCP_AGENTS: "unknown-only" }), []);
});

test("resolveAvailableAgentIds: a non-string value fails CLOSED to zero agents, not open to all", () => {
  const { extension } = loadExtension();
  assert.deepStrictEqual(extension.resolveAvailableAgentIds({ ZCP_AGENTS: null }), []);
});

test("resolveAvailableAgentIds: legacy ZCP_AGENT_TYPES has no effect without ZCP_AGENTS", () => {
  const { extension } = loadExtension();
  assert.deepStrictEqual(extension.resolveAvailableAgentIds({ ZCP_AGENT_TYPES: "claude-code" }), ALL_FIVE);
});

// ---- isAgentInstalled ------------------------------------------------------

function withPath(dirs, fn) {
  const original = process.env.PATH;
  process.env.PATH = dirs.join(path.delimiter);
  try {
    fn();
  } finally {
    process.env.PATH = original;
  }
}

function makeBin(dir, name, mode) {
  const bin = path.join(dir, name);
  fs.writeFileSync(bin, "#!/bin/sh\n");
  fs.chmodSync(bin, mode);
  return bin;
}

test("isAgentInstalled: an executable file on PATH is installed", () => {
  const { extension } = loadExtension();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-bin-"));
  makeBin(dir, "my-agent", 0o755);
  withPath([dir], () => {
    assert.equal(extension.isAgentInstalled("my-agent"), true);
  });
});

test("isAgentInstalled: a missing binary is not installed", () => {
  const { extension } = loadExtension();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-bin-"));
  withPath([dir], () => {
    assert.equal(extension.isAgentInstalled("nonexistent-agent"), false);
  });
});

test("isAgentInstalled: a non-executable file (0o644) is not installed", () => {
  const { extension } = loadExtension();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-bin-"));
  makeBin(dir, "my-agent", 0o644);
  withPath([dir], () => {
    assert.equal(extension.isAgentInstalled("my-agent"), false);
  });
});

test("isAgentInstalled: a directory sharing the binary's name is not installed", () => {
  const { extension } = loadExtension();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-bin-"));
  fs.mkdirSync(path.join(dir, "my-agent"));
  withPath([dir], () => {
    assert.equal(extension.isAgentInstalled("my-agent"), false);
  });
});

test("isAgentInstalled: a PATH entry whose directory name has a space still resolves", () => {
  const { extension } = loadExtension();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp bin dir "));
  makeBin(dir, "my-agent", 0o755);
  withPath([dir], () => {
    assert.equal(extension.isAgentInstalled("my-agent"), true);
  });
});

test("isAgentInstalled: an empty PATH is never installed", () => {
  const { extension } = loadExtension();
  withPath([""], () => {
    assert.equal(extension.isAgentInstalled("my-agent"), false);
  });
});

// ---- isAgentInstalled: zembed-PATH union -----------------------------------
// Live-verified 0.1.5 regression: code-server's extension host froze a PATH
// NARROWER than the runtime profile PATH (it lacked the agent bin dirs), so a
// host-PATH-only probe reported every agent "Not installed" in a container
// where terminals (which source the profile) launch them fine. The probe
// therefore searches the UNION of the host PATH and the zembed store's PATH —
// a hit on either counts.

test("isAgentInstalled: a binary only on the zembed store's PATH is installed", () => {
  const { extension } = loadExtension();
  const hostDir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-bin-host-"));
  const runtimeDir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-bin-runtime-"));
  makeBin(runtimeDir, "my-agent", 0o755);
  withPath([hostDir], () => {
    assert.equal(extension.isAgentInstalled("my-agent", { PATH: runtimeDir }), true);
  });
});

test("isAgentInstalled: a null env degrades to the host PATH only", () => {
  const { extension } = loadExtension();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-bin-"));
  makeBin(dir, "my-agent", 0o755);
  withPath([dir], () => {
    assert.equal(extension.isAgentInstalled("my-agent", null), true);
  });
  withPath([""], () => {
    assert.equal(extension.isAgentInstalled("my-agent", null), false);
  });
});

test("isAgentInstalled: a non-string zembed PATH value is ignored", () => {
  const { extension } = loadExtension();
  const hostDir = fs.mkdtempSync(path.join(os.tmpdir(), "zcp-bin-host-"));
  withPath([hostDir], () => {
    assert.equal(extension.isAgentInstalled("my-agent", { PATH: 12345 }), false);
  });
});

// ---- template source pins ---------------------------------------------------

test("template source: still reads the live zembed store, never mentions ZCP_AGENT_TYPES, and never spawns a process to probe", () => {
  const src = fs.readFileSync(path.join(TEMPLATES_DIR, "vscode-bootstrap-extension.js"), "utf8");
  assert.match(src, /\/etc\/zerops-zembed/);
  assert.doesNotMatch(src, /ZCP_AGENT_TYPES/);
  assert.doesNotMatch(src, /child_process|\bspawn\(|\bexecSync?\(/);
});
