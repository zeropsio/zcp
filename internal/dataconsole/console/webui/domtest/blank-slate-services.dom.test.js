"use strict";
// A project with no managed services gets an honest rail note and a content
// blank slate instead of retaining index.html's generic selection prompt.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute } = require("./harness");

async function main() {
  const discovered = {
    hostname: "db", type: "postgresql:single@18", family: "tabular", support: "supported", actions: [],
  };
  const c = buildConsole({
    url: "http://localhost/#t=FAKE",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Empty project" }, services: [], allowWrites: true });
      if (method === "POST" && p === "/api/refresh") return jsonRoute({ services: [discovered] });
      return null;
    },
  });
  await waitFor(() => c.document.querySelector("#content > .state.empty"), { desc: "no-services content slate" });

  const railNote = c.document.querySelector("#services .service-empty");
  assert.ok(railNote, "the managed-services rail contains an explicit empty note");
  assert.strictEqual(railNote.textContent, "No managed services", "the rail note uses concise empty copy");
  assert.ok(railNote.classList.contains("muted"), "the rail note is visually muted");

  const content = c.document.getElementById("content");
  assert.strictEqual(content.children.length, 1, "the content pane has one canonical empty-state root");
  const slate = content.querySelector(":scope > .state.empty");
  assert.strictEqual(slate.querySelector(".state-title").textContent, "No managed services in this project", "the content slate explains the project-level state");
  assert.match(slate.querySelector(".state-detail").textContent, /managed databases, caches, queues, and storage/i, "the detail explains what the console discovers");
  assert.match(slate.querySelector(".state-detail").textContent, /↻/, "the detail points to re-discovery");
  assert.strictEqual(slate.querySelector("button"), null, "the no-services slate invents no CTA");

  click(c.document.getElementById("refresh"));
  await waitFor(() => c.document.querySelector("#services li span"), { desc: "refreshed service rail" });
  assert.strictEqual(c.document.querySelector("#services li span").textContent, "db", "0-to-N refresh renders the newly discovered service in the rail");
  assert.strictEqual(content.querySelector(":scope > .state.empty"), null, "0-to-N refresh removes the stale no-services slate");
  const placeholder = content.querySelector(":scope > .placeholder");
  assert.ok(placeholder, "0-to-N refresh restores the generic selection prompt");
  assert.strictEqual(placeholder.textContent, "Select a service, then an object/table/key.", "the restored prompt matches the initial SPA copy");
  c.close();

  console.log("blank-slate-services.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
