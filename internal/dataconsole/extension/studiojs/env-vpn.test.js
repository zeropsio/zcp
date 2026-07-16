"use strict";

// S3 — Env & VPN slice contract (L-EV-1/L-EV-2/L-EV-3).
//
// Pins the honesty floor: render shows the Sync .env action, the live
// `zcli vpn up <projectId>` command (projectId from the uiMap, never hard-coded),
// and BOTH VPN tiers — the Tier-1 "no VPN" line is true only because the Tier-2
// line stands beside it, so we assert both copies are present rather than a
// blanket "no VPN" assertion (the Tier-1 copy legitimately contains "no VPN").
// Then: both handlers export their pinned `type` + a handle fn, and the sync-env
// handler calls runVerb(["sync-env"]) and posts an ok:true result.

const assert = require("assert");

const card = require("../templates/vscode-studio/cards/env-vpn");
const syncEnv = require("../templates/vscode-studio/handlers/sync-env");
const vpnStatus = require("../templates/vscode-studio/handlers/vpn-status");

function fail(msg) {
  console.error("env-vpn.test.js FAIL: " + msg);
  process.exit(1);
}

(async function main() {
  // (1) render() — Sync action, live VPN command, project id, both tier copies.
  assert.strictEqual(card.id, "env-vpn", "card id is env-vpn");
  const PROJECT_ID = "proj-xyz-789";
  const uiMap = {
    project: { id: PROJECT_ID, name: "demo", status: "ACTIVE" },
    services: [],
    warnings: [],
  };
  const html = card.render(uiMap);

  assert.ok(
    html.indexOf('data-action="sync-env"') >= 0,
    "render must include a data-action=\"sync-env\" element"
  );
  assert.ok(html.indexOf("zcli vpn up") >= 0, "render must include the zcli vpn up command");
  assert.ok(
    html.indexOf(PROJECT_ID) >= 0,
    "render must include the live project id from the uiMap, not a hard-coded value"
  );
  // Both tiers present == honesty floor satisfied (never a blanket "no VPN").
  assert.ok(
    html.indexOf("Tier 1") >= 0 && html.indexOf("deploy, preview, and sync env need no VPN") >= 0,
    "render must include the Tier-1 copy"
  );
  assert.ok(
    html.indexOf("Tier 2") >= 0 &&
      html.indexOf("run your local app against a managed service (DB/cache), connect the project VPN:") >= 0,
    "render must include the Tier-2 copy"
  );
  // The project id is HTML-escaped through the same path as the command, so a
  // hostile id can't break out of the rendered fragment.
  const evil = card.render({ project: { id: '<img src=x onerror=1>' } });
  assert.ok(evil.indexOf("<img src=x") < 0, "project id must be HTML-escaped in render");

  // (2) Both handlers export their pinned type + a handle function.
  assert.strictEqual(syncEnv.type, "sync-env", "sync-env handler type");
  assert.strictEqual(typeof syncEnv.handle, "function", "sync-env handle is a function");
  assert.strictEqual(vpnStatus.type, "vpn-status", "vpn-status handler type");
  assert.strictEqual(typeof vpnStatus.handle, "function", "vpn-status handle is a function");

  // (3) sync-env.handle calls runVerb(["sync-env"]) and posts an ok:true result.
  const posts = [];
  let calledArgs = null;
  const ctx = {
    runVerb: function (args) {
      calledArgs = args;
      return { ok: true, data: { variables: 3, path: ".env" } };
    },
    postMessage: function (m) {
      posts.push(m);
    },
  };
  await syncEnv.handle({ type: "sync-env" }, ctx);

  assert.deepStrictEqual(calledArgs, ["sync-env"], "sync-env must call runVerb([\"sync-env\"])");
  assert.strictEqual(posts.length, 1, "sync-env must post exactly one result");
  assert.strictEqual(posts[0].type, "sync-env-result", "result message type");
  assert.strictEqual(posts[0].ok, true, "result ok:true on a successful verb");
  assert.strictEqual(posts[0].variables, 3, "result carries the variable count through");
  assert.strictEqual(posts[0].path, ".env", "result carries the .env path through");

  console.log("env-vpn.test.js OK");
})().catch(function (err) {
  fail((err && err.message) || String(err));
});
