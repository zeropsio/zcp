"use strict";
// C7: appendTreePage's lone-container auto-expand (a root whose only child is
// a single folder drills straight through, no extra click needed) RETURNS
// early -- before reaching the root-upload-bar branch below it. So a bucket
// whose root contains exactly one folder got the auto-expanded folder's own
// nested upload bar (B8) but never a root-level one: there was no UI path to
// upload directly into the bucket root at all in that shape. Fix: append the
// root upload bar (same gate: root && editing() && actionEnabled(uploadObject))
// before the auto-expand's early return, so the root always gets its bar
// regardless of what it contains.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function scenarioSingleFolderRootStillGetsRootUploadBar() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }, { id: "uploadObject", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) {
      const segs = JSON.parse(new URL("http://x" + p).searchParams.get("segs") || "[]");
      if (segs.length === 0) {
        // Root: exactly ONE node, a container -- triggers the lone-container auto-expand.
        return jsonRoute({ nodes: [{ name: "folder/", kind: "container", path: { service: "storage", segments: ["folder"] } }] });
      }
      return jsonRoute({ nodes: [{ name: "a.txt", kind: "blob", path: { service: "storage", segments: ["folder", "a.txt"] }, meta: { size: 3 } }] });
    }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "storage" });

  // The lone-container auto-expand drills straight through -- wait for the
  // folder's own (nested) child to render.
  await waitFor(() => c.document.querySelector("#tree .children .node-wrap"), { desc: "the auto-expanded folder's child renders" });

  const rootBar = c.document.querySelector("#tree > .uploadbar");
  assert.ok(rootBar, "the root -- even though it auto-expanded through a single folder -- still gets its own root-level upload bar");

  const nestedBar = c.document.querySelector("#tree .children > .uploadbar");
  assert.ok(nestedBar, "the auto-expanded folder still gets its own nested upload bar (B8 unregressed)");
  assert.notStrictEqual(rootBar, nestedBar, "the root bar and the nested bar are two distinct elements");

  // The root bar's upload targets the ROOT's own (empty) segs, not the folder's.
  // Scoped by class (P10: addUploadBar's markup is class-based, not a
  // per-instance id -- two bars now coexist (root + nested), and an id would
  // collide across them).
  const rootUploadBtn = rootBar.querySelector(".uploadbtn");
  assert.ok(rootUploadBtn, "the root bar renders the embedded upload control");
  click(rootUploadBtn);
  const rootUploadMsg = c.rpcLog.filter((m) => m.type === "dc-upload").pop();
  assert.ok(rootUploadMsg, "clicking the root bar's upload control posts a dc-upload host message");
  assert.strictEqual(JSON.stringify(rootUploadMsg.segs), JSON.stringify([]), "the root bar's upload message carries the root's own (empty) segs, not the folder's");

  c.close();
}

async function main() {
  await scenarioSingleFolderRootStillGetsRootUploadBar();
  console.log("object-root-single-folder-upload-bar.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
