#!/usr/bin/env node
"use strict";

// CLI: node run.js --scenario CORE-1 | --all
// See README.md for the env/config contract and data discipline.

const fs = require("fs");
const path = require("path");
const runner = require("./lib/runner");
const harness = require("./lib/harness");
const engines = require("./lib/engines");

// Every scenarios/*.js file self-registers into the runner at require time.
// Discovered by glob (sorted) so parallel fan-out agents each own one new file
// and never edit this one.
const SCENARIOS_DIR = path.join(__dirname, "scenarios");
fs.readdirSync(SCENARIOS_DIR)
  .filter((f) => f.endsWith(".js"))
  .sort()
  .forEach((f) => require(path.join(SCENARIOS_DIR, f)));

function parseArgs(argv) {
  const out = { all: false, scenario: null, family: null };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "--all") out.all = true;
    else if (a === "--scenario") out.scenario = argv[++i];
    else if (a.indexOf("--scenario=") === 0) out.scenario = a.slice("--scenario=".length);
    else if (a === "--family") out.family = argv[++i];
    else if (a.indexOf("--family=") === 0) out.family = a.slice("--family=".length);
  }
  return out;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const known = runner.all();

  let targets;
  if (args.all) {
    targets = known;
  } else if (args.family) {
    targets = known.filter((s) => (s.family || "") === args.family);
    if (targets.length === 0) {
      console.error('run.js: no scenarios with family "' + args.family + '". known: ' + known.map((s) => s.id + "(" + (s.family || "") + ")").join(", "));
      process.exit(2);
    }
  } else if (args.scenario) {
    const sc = runner.get(args.scenario);
    if (!sc) {
      console.error('run.js: unknown scenario "' + args.scenario + '". known: ' + known.map((s) => s.id).join(", "));
      process.exit(2);
    }
    targets = [sc];
  } else {
    console.error("usage: node run.js --scenario <ID> | --family <fam> | --all");
    console.error("known scenarios: " + known.map((s) => s.id).join(", "));
    process.exit(2);
    return;
  }

  console.log("connecting (launch + auth + wait for workbench)...");
  const { browser, page } = await harness.connect();
  console.log("connected.");

  const results = [];
  try {
    for (const scenario of targets) {
      console.log("--- running " + scenario.id + " ---");
      harness.setScenario(scenario.id);
      const ctx = {
        page: page,
        harness: harness,
        engines: engines,
        evidence: (name) => harness.shot(page, scenario.id, name),
      };
      const result = await runner.runOne(scenario, ctx);
      results.push(result);
      console.log(
        "--- " + scenario.id + ": " + result.status + " (" + result.durationMs + "ms, " + result.findings.length + " finding(s)) ---"
      );
      if (result.error) console.log(result.error);
    }
  } finally {
    await browser.close();
  }

  const manifest = runner.writeManifest(results);
  runner.printManifest(manifest);

  const anyFail = results.some((r) => r.status === "FAIL");
  process.exit(anyFail ? 1 : 0);
}

main().catch((e) => {
  console.error("run.js: fatal:", e && e.stack ? e.stack : e);
  process.exit(1);
});
