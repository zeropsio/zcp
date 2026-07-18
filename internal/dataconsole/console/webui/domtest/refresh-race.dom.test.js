"use strict";
// C4: the #refresh ("Re-discover services") handler awaited /api/refresh,
// then UNCONDITIONALLY called selectService() (or openPendingService()) on
// resolve -- selectService() resets #content to its "Browse <service> in the
// tree" placeholder and mints a fresh contentGen. If the user opens a table
// while a slow refresh is still in flight, the refresh's late resolution
// clobbers that navigation: the table view is replaced by the placeholder
// even though the user is looking straight at it. Fix: snapshot contentGen
// before the await; if it changed by the time refresh resolves, still update
// the services rail (state.services / renderServices()) but skip the
// #content reset -- the newer view already showing belongs to a later
// generation and must survive.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function scenarioSlowRefreshMustNotClobberInFlightNavigation() {
  const service = {
    hostname: "db", type: "postgresql:single@18", support: "supported", actions: [],
  };
  const refreshResolvers = [];
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({
      nodes: [{ name: "t", kind: "tabular", path: { service: "db", segments: ["public", "t"] } }],
    });
    if (p.startsWith("/api/table")) return jsonRoute({
      columns: [{ name: "val", dataType: "text", pk: false, editable: false, reason: "" }], rows: [["row1"]], rowKeyCols: [],
    });
    if (method === "POST" && p === "/api/refresh") return new Promise((resolve) => refreshResolvers.push(resolve));
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node render" });

  // Kick off refresh -- it hangs.
  click(c.document.getElementById("refresh"));
  await waitFor(() => refreshResolvers.length === 1, { desc: "refresh request in flight" });

  // Before it resolves, open the table.
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content table.grid"), { desc: "table view renders" });

  // NOW the stale refresh resolves.
  refreshResolvers[0](jsonRoute({ services: [service] }));
  await new Promise((resolve) => setTimeout(resolve, 50));

  assert.ok(c.document.querySelector("#content table.grid"), "the table view must survive a stale refresh's late resolution");
  assert.strictEqual(c.document.querySelectorAll("#services li").length, 1, "the services rail is still refreshed from the (stale) response");
  c.close();
}

async function main() {
  await scenarioSlowRefreshMustNotClobberInFlightNavigation();
  console.log("refresh-race.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
