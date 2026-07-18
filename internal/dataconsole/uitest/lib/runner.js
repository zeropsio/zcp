"use strict";

// Scenario registry + manifest + findings writer, shared by every fan-out
// agent's scenario file. A scenario self-registers by calling register() at
// require time (see scenarios/core.js's bottom line); run.js requires every
// scenarios/*.js file so the registry is fully populated before it reads argv.

const fs = require("fs");
const path = require("path");

const ROOT = path.join(__dirname, "..");
const FINDINGS_DIR = path.join(ROOT, "findings");
// UITEST_TAG isolates parallel fan-out agents: each writes its own findings
// JSONL + manifest so concurrent runs never clobber each other. Default "a1"
// keeps the original single-run filenames.
const TAG = process.env.UITEST_TAG || "a1";
const FINDINGS_FILE = path.join(FINDINGS_DIR, TAG + ".jsonl");
const MANIFEST_FILE = path.join(FINDINGS_DIR, TAG === "a1" ? "manifest.json" : "manifest-" + TAG + ".json");

const registry = new Map();

function register(scenario) {
  if (!scenario || !scenario.id) throw new Error("runner.register: scenario needs an id");
  if (typeof scenario.fn !== "function") throw new Error('runner.register: scenario "' + scenario.id + '" needs a fn(ctx)');
  if (registry.has(scenario.id)) throw new Error('runner.register: duplicate scenario id "' + scenario.id + '"');
  registry.set(scenario.id, scenario);
}

function all() {
  return Array.from(registry.values());
}
function get(id) {
  return registry.get(id);
}

function ensureFindingsDir() {
  fs.mkdirSync(FINDINGS_DIR, { recursive: true });
}

// appendFinding writes one JSONL record, append-only, so a re-run never loses a
// prior run's evidence trail.
function appendFinding(record) {
  ensureFindingsDir();
  fs.appendFileSync(FINDINGS_FILE, JSON.stringify(record) + "\n");
}

// runOne executes one scenario's fn(ctx), catching any thrown error as a FAIL
// (a legitimately-failing assertion inside the scenario is instead recorded via
// ctx.addFinding — see scenarios/core.js's G1 verdict rule: a thrown error means
// the HARNESS couldn't drive the UI, not that a checked assertion came back false).
async function runOne(scenario, ctx) {
  const findings = [];
  const addFinding = (f) => {
    f = f || {};
    const record = Object.assign(
      {
        id: f.id || scenario.id + "-" + (findings.length + 1),
        scenario: scenario.id,
        lane: "F",
        family: f.family || scenario.family || "",
        severity: f.severity || "S3",
        title: "",
        repro: "",
        expected: "",
        actual: "",
        evidence: [],
        engine_truth: "",
        status: "new",
      },
      f
    );
    findings.push(record);
    appendFinding(record);
    return record;
  };

  const startedAt = Date.now();
  let status = "PASS";
  let error = null;
  try {
    await scenario.fn(Object.assign({}, ctx, { addFinding: addFinding }));
  } catch (e) {
    status = "FAIL";
    error = e && e.stack ? e.stack : String(e);
  }
  return {
    id: scenario.id,
    family: scenario.family || "",
    status: status,
    error: error,
    findings: findings,
    durationMs: Date.now() - startedAt,
  };
}

function writeManifest(results) {
  ensureFindingsDir();
  const manifest = {
    generatedAt: new Date().toISOString(),
    results: results.map((r) => ({
      id: r.id,
      family: r.family,
      status: r.status,
      error: r.error || null,
      findingsCount: r.findings.length,
      evidenceDir: path.join("evidence", r.id),
      durationMs: r.durationMs,
    })),
  };
  fs.writeFileSync(MANIFEST_FILE, JSON.stringify(manifest, null, 2) + "\n");
  return manifest;
}

function printManifest(manifest) {
  console.log("");
  console.log("=== scenario manifest ===");
  for (const r of manifest.results) {
    let line = r.status.padEnd(4) + " " + r.id.padEnd(10) + " " + r.evidenceDir;
    if (r.findingsCount) line += "  (" + r.findingsCount + " finding(s))";
    console.log(line);
    if (r.error) console.log("     " + r.error.split("\n")[0]);
  }
  console.log("==========================");
  console.log("");
}

module.exports = {
  register: register,
  all: all,
  get: get,
  runOne: runOne,
  writeManifest: writeManifest,
  printManifest: printManifest,
  appendFinding: appendFinding,
  FINDINGS_DIR: FINDINGS_DIR,
  FINDINGS_FILE: FINDINGS_FILE,
  MANIFEST_FILE: MANIFEST_FILE,
};
