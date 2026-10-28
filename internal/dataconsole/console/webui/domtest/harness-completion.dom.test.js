"use strict";
const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const { buildConsole, waitFor, jsonRoute, hostPostMessage } = require("./harness");

(async () => {
  let release;
  const c = buildConsole({ embedded: true, routes: () => new Promise(resolve => { release = resolve; }) });
  try {
    const changed = waitFor(c, () => c.document.body.dataset.done);
    queueMicrotask(() => { c.document.body.dataset.done = "mutation receipt"; });
    assert.equal(await changed, "mutation receipt", "DOM mutation completes the wait without a settling interval");
    hostPostMessage(c.window, { type: "dataconsole-init", token: "FAKE" });
    await waitFor(c, () => typeof release === "function", { desc: "held transport entered" });
    release(jsonRoute({ project: { name: "receipt" }, services: [], allowWrites: false }));
    await c.settled();
    assert.equal(c.document.getElementById("project").textContent, "receipt", "drain includes replies posted by promise continuations behind its first marker");
  } finally {
    c.close();
  }
  // A pending promise alone must never let a missing completion pass silently.
  const child = spawnSync(process.execPath, ["-e", `
    const { buildConsole, waitFor } = require('./harness');
    const c = buildConsole({ embedded: true });
    waitFor(c, () => false, { desc: 'missing receipt' }).catch(error => {
      c.close(); process.stderr.write(error.message); process.exitCode = 1;
    });
  `], { cwd: __dirname, encoding: "utf8" });
  assert.equal(child.status, 1, "drained work rejects an unsatisfied wait");
  assert.match(child.stderr, /waitFor has no pending work: missing receipt/);
  console.log("harness-completion.dom.test.js OK");
})().catch(error => { console.error(error); process.exitCode = 1; });
