"use strict";
// B8 (S3): object folder upload. addUploadBar was gated on root===true at
// BOTH its call sites inside appendTreePage, so an upload bar only ever
// rendered at the bucket root -- there was no UI path to upload INTO a
// folder at all (live-confirmed known gap). Fix: expandContainer, once its
// (nested) children page finishes loading, appends its OWN upload bar
// carrying that folder's segs -- the plumbing (Path.segments through
// n.path.segments) already threads it; the fix is purely about WHEN the bar
// is added, not a new transport. One bar per expanded container: re-expanding
// an already-loaded container (the toggle-hidden branch) does not reload or
// re-add anything, so it can never duplicate.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function scenarioExpandedFolderGetsItsOwnUploadBar() {
  const service = {
    hostname: "storage", type: "s3:single@1", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }, { id: "uploadObject", enabled: true, readOnly: false, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) {
      const segs = JSON.parse(new URL("http://x" + p).searchParams.get("segs") || "[]");
      if (segs.length === 0) {
        // Root: TWO nodes (a folder + a file) so the lone-container auto-expand
        // does not fire -- the folder is expanded via a real user click.
        return jsonRoute({
          nodes: [
            { name: "folder/", kind: "container", path: { service: "storage", segments: ["folder"] } },
            { name: "readme.txt", kind: "blob", path: { service: "storage", segments: ["readme.txt"] }, meta: { size: 5 } },
          ],
        });
      }
      // Inside the folder: one file, no sub-containers.
      return jsonRoute({ nodes: [{ name: "a.txt", kind: "blob", path: { service: "storage", segments: ["folder", "a.txt"] }, meta: { size: 3 } }] });
    }
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "storage" });
  await waitFor(() => c.document.querySelectorAll("#tree .node-wrap").length === 2, { desc: "root tree renders both nodes" });

  // Before expanding: no upload bar exists INSIDE any .children container (the
  // root's own bar, if any, is a direct child of #tree, not of .children).
  assert.strictEqual(c.document.querySelectorAll("#tree .children .uploadbar").length, 0, "precondition: no nested upload bar before the folder is expanded");

  const folderRow = c.document.querySelector("#tree .node-wrap .node");
  click(folderRow);
  await waitFor(() => c.document.querySelector("#tree .children .node-wrap"), { desc: "folder expands and its child renders" });

  const kids = c.document.querySelector("#tree .node-wrap .children");
  const bar = kids.querySelector(":scope > .uploadbar");
  assert.ok(bar, "the expanded folder gets its own upload bar, appended inside its .children container");

  // Its upload message carries the folder's OWN segs (["folder"]), not the root's.
  const uploadBtn = bar.querySelector("#uploadbtn");
  assert.ok(uploadBtn, "the nested upload bar renders the embedded (host-dialog) upload control");
  click(uploadBtn);
  const uploadMsg = c.rpcLog.filter((m) => m.type === "dc-upload").pop();
  assert.ok(uploadMsg, "clicking the nested upload control posts a dc-upload host message");
  // JSON-compare, not deepStrictEqual: uploadMsg.segs is an Array from the
  // jsdom window's OWN realm (app.js runs via window.eval), so it is
  // structurally equal but never reference-equal to a same-process literal.
  assert.strictEqual(JSON.stringify(uploadMsg.segs), JSON.stringify(["folder"]), "the nested upload bar's message carries the folder's own segs, not the root's");

  // One bar per expanded container: collapsing + re-expanding must not add a second.
  click(folderRow); // collapse
  await waitFor(() => kids.classList.contains("hidden"), { desc: "folder collapses" });
  click(folderRow); // re-expand
  await waitFor(() => !kids.classList.contains("hidden"), { desc: "folder re-expands" });
  assert.strictEqual(kids.querySelectorAll(":scope > .uploadbar").length, 1, "collapsing and re-expanding an already-loaded folder never duplicates its upload bar");
  c.close();
}

async function main() {
  await scenarioExpandedFolderGetsItsOwnUploadBar();
  console.log("object-folder-upload.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
