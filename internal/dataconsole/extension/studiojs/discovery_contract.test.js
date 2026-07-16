"use strict";

// E1/E2 directory-discovery contract — the parallel-safety guarantee that lets
// S2-S6 run concurrently. Each sibling adds a file into cards/ or handlers/ and
// it is picked up with NO central-registration edit. The one hazard directory
// discovery cannot protect — two handlers declaring the same `type` — is a hard
// failure here, not a silent shadow.

const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { enumerateCards } = require("../templates/vscode-studio/lib/cards");
const {
  enumerateHandlers,
  buildRouter,
} = require("../templates/vscode-studio/lib/handlers");

function tmpDir(prefix) {
  return fs.mkdtempSync(path.join(os.tmpdir(), prefix));
}

// (1) The real cards/ dir is discovered and the two service cards register.
const realCardsDir = path.join(__dirname, "..", "templates", "vscode-studio", "cards");
const realCards = enumerateCards(realCardsDir);
assert.ok(
  realCards.find((c) => c.id === "runtime"),
  "the Runtime service card must be discovered from the shipped cards/ dir"
);
assert.ok(
  realCards.find((c) => c.id === "managed"),
  "the Managed service card must be discovered from the shipped cards/ dir"
);
realCards.forEach((c) => {
  assert.strictEqual(typeof c.render, "function", "card " + c.id + " must have render()");
});

// (1b) Cards render in product-owned `order`, not alphabetical filename order:
// runtime(10) before managed(20) before env-vpn(30) before agent(40) before footer(50).
const ids = realCards.map((c) => c.id);
assert.deepStrictEqual(
  ids,
  ["runtime", "managed", "env-vpn", "agent", "refresh"],
  "cards render in ascending `order`, not by filename"
);

// (2) A pure file-add into cards/ is picked up; *.test.js is skipped (so a
// co-located test never registers as a card).
const cardsTmp = tmpDir("zs-cards-");
fs.writeFileSync(
  path.join(cardsTmp, "demo.js"),
  "module.exports={id:'demo',title:'Demo',render:function(){return '<i>demo</i>';}};"
);
fs.writeFileSync(
  path.join(cardsTmp, "demo.test.js"),
  "throw new Error('a .test.js must never be loaded as a card');"
);
const discovered = enumerateCards(cardsTmp);
assert.strictEqual(discovered.length, 1, "only demo.js discovered (.test.js skipped)");
assert.strictEqual(discovered[0].id, "demo");

// (3) The handler router builds the allowlist from the discovered set.
const hTmp = tmpDir("zs-h-");
fs.writeFileSync(path.join(hTmp, "alpha.js"), "module.exports={type:'alpha',handle:function(){}};");
fs.writeFileSync(path.join(hTmp, "beta.js"), "module.exports={type:'beta',handle:function(){}};");
const router = buildRouter(enumerateHandlers(hTmp));
assert.ok(router.allow.has("alpha") && router.allow.has("beta"), "allowlist built from discovery");

// (4) Duplicate handler TYPE across the discovered set is a hard failure.
const dupTmp = tmpDir("zs-dup-");
fs.writeFileSync(path.join(dupTmp, "one.js"), "module.exports={type:'deploy',handle:function(){}};");
fs.writeFileSync(path.join(dupTmp, "two.js"), "module.exports={type:'deploy',handle:function(){}};");
assert.throws(
  () => buildRouter(enumerateHandlers(dupTmp)),
  /duplicate handler type/,
  "two handlers with the same type must throw"
);

// (5) Allowlist enforcement: unknown types are dropped, known types dispatch.
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
