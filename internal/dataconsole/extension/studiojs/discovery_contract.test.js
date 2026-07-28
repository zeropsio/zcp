"use strict";

// E2 directory-discovery contract for the handler router — the parallel-safety
// guarantee (buildRouter throws on a duplicate `type` instead of a silent
// shadow). The E1 half of this contract (cards/ directory discovery) died with
// the Studio sidebar in S4: lib/cards.js and the cards/ dir are deleted along
// with the sidebar card-list subsystem (docs/spec-dataconsole.md §4.4 — one
// embedded surface, no populated sidebar). enumerateHandlers/buildRouter
// survive as the generic directory-discovery contract this file pins; the
// shipped handlers/ dir itself is now EMPTY (both handlers/console.js and
// handlers/refresh.js are deleted with the sidebar) — asserted below.

const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const {
  enumerateHandlers,
  buildRouter,
} = require("../templates/vscode-studio/lib/handlers");

function tmpDir(prefix) {
  return fs.mkdtempSync(path.join(os.tmpdir(), prefix));
}

// (1) The shipped handlers/ dir is empty post-S4 — no router consumer remains
// (consolePanel.js's WebviewPanel dispatches dc-* messages directly; there is
// no more sidebar webview session to route through this allowlist).
const realHandlersDir = path.join(__dirname, "..", "templates", "vscode-studio", "handlers");
assert.deepStrictEqual(
  enumerateHandlers(realHandlersDir),
  [],
  "the shipped handlers/ dir is empty — the sidebar's handlers/console.js and handlers/refresh.js are gone"
);

// (2) The handler router still builds the allowlist from whatever IS
// discovered — pinned generically (a future consumer of directory discovery
// gets this contract for free).
const hTmp = tmpDir("zs-h-");
fs.writeFileSync(path.join(hTmp, "alpha.js"), "module.exports={type:'alpha',handle:function(){}};");
fs.writeFileSync(path.join(hTmp, "beta.js"), "module.exports={type:'beta',handle:function(){}};");
const router = buildRouter(enumerateHandlers(hTmp));
assert.ok(router.allow.has("alpha") && router.allow.has("beta"), "allowlist built from discovery");

// (3) Duplicate handler TYPE across the discovered set is a hard failure.
const dupTmp = tmpDir("zs-dup-");
fs.writeFileSync(path.join(dupTmp, "one.js"), "module.exports={type:'deploy',handle:function(){}};");
fs.writeFileSync(path.join(dupTmp, "two.js"), "module.exports={type:'deploy',handle:function(){}};");
assert.throws(
  () => buildRouter(enumerateHandlers(dupTmp)),
  /duplicate handler type/,
  "two handlers with the same type must throw"
);

// (4) Allowlist enforcement: unknown types are dropped, known types dispatch.
(async () => {
  try {
    let called = false;
    const r = buildRouter([{ type: "ok", handle: () => { called = true; } }]);

    const dropped = await r.dispatch({ type: "not-allowed" }, {});
    assert.strictEqual(dropped, false, "unknown message type must be dropped");
    assert.strictEqual(called, false, "dropped message must not invoke a handler");

    const handled = await r.dispatch({ type: "ok" }, {});
    assert.strictEqual(handled, true, "allowed message type must dispatch");
    assert.strictEqual(called, true, "allowed handler must run");

    console.log("discovery_contract.test.js OK");
  } catch (err) {
    console.error(err && err.stack ? err.stack : err);
    process.exit(1);
  }
})();
