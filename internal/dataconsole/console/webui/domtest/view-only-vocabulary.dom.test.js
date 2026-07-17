"use strict";
// P2 (S3, visual-polish): view-only vocabulary + in-pane cue.
//   (a) The rail's tier badge for a view-only service must say the full
//       "view-only" (matching the Studio sidebar card's wording in
//       extension/templates/vscode-studio/cards/managed.js: `tier === "view"
//       ? "view-only" : ...`), not the abbreviated "view" it used to render.
//   (b) The topbar states the ACTIVE service's view-only posture directly
//       (next to #activesvc), not only via the rail pill -- so the content
//       pane itself carries the cue once a service is open (today the ONLY
//       cue is the rail pill, which scrolls out of view or is hidden
//       entirely when embedded with a deep-linked service).

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

// 1. Rail badge: a view-only service reads "view-only", not "view".
async function scenarioRailBadgeSaysViewOnly() {
  const service = { hostname: "ch", type: "clickhouse:single@1", support: "view-only", actions: [] };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/#t=FAKE", routes });
  await waitFor(() => c.document.querySelector("#services li .badge"), { desc: "rail renders" });
  const badge = c.document.querySelector("#services li .badge");
  assert.strictEqual(badge.textContent, "view-only", "the rail tier badge for a view-only service reads the full word, matching the Studio sidebar card");
  c.close();
}

// Regression companion: a full-write service still reads "ready", a
// not-yet-browsable service still reads "not yet" (only the view-only label changed).
async function scenarioOtherRailBadgesUnchanged() {
  const ready = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [] };
  const notyet = { hostname: "mystery", type: "unknown:single@1", support: "not yet", actions: [] };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [ready, notyet], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/#t=FAKE", routes });
  await waitFor(() => c.document.querySelectorAll("#services li .badge").length === 2, { desc: "rail renders both services" });
  const badges = Array.from(c.document.querySelectorAll("#services li .badge")).map((b) => b.textContent);
  assert.deepStrictEqual(badges, ["ready", "not yet"], "the ready and not-yet labels are unchanged by the view-only wording fix");
  c.close();
}

// 2a. Topbar: the active service's view-only posture is stated in-pane.
async function scenarioTopbarBadgeShowsForViewOnlyActiveService() {
  const service = { hostname: "ch", type: "clickhouse:single@1", support: "view-only", actions: [] };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/#t=FAKE", routes });
  await waitFor(() => c.document.querySelector("#services li"), { desc: "rail renders" });
  click(c.document.querySelector("#services li"));
  await waitFor(() => c.document.getElementById("activesvc").textContent.includes("ch"), { desc: "service selected" });
  const badge = c.document.getElementById("activesvcbadge");
  assert.ok(badge, "the topbar carries an active-service view-only badge element");
  assert.strictEqual(badge.classList.contains("hidden"), false, "a view-only active service shows the topbar badge");
  assert.ok(badge.classList.contains("view-only"), "the topbar badge reuses the existing .badge.view-only styling");
  c.close();
}

// 2b. Topbar: a full-write active service shows no such badge.
async function scenarioTopbarBadgeHiddenForFullWriteActiveService() {
  const service = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [] };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/#t=FAKE", routes });
  await waitFor(() => c.document.querySelector("#services li"), { desc: "rail renders" });
  click(c.document.querySelector("#services li"));
  await waitFor(() => c.document.getElementById("activesvc").textContent.includes("db"), { desc: "service selected" });
  const badge = c.document.getElementById("activesvcbadge");
  assert.ok(badge, "the topbar badge element exists in the markup even when unused");
  assert.strictEqual(badge.classList.contains("hidden"), true, "a full-write active service hides the topbar view-only badge");
  c.close();
}

async function main() {
  await scenarioRailBadgeSaysViewOnly();
  await scenarioOtherRailBadgesUnchanged();
  await scenarioTopbarBadgeShowsForViewOnlyActiveService();
  await scenarioTopbarBadgeHiddenForFullWriteActiveService();
  console.log("view-only-vocabulary.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
