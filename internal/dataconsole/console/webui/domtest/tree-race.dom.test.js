"use strict";
// KI-2 regression: two close-together root tree loads must never both land --
// not just "no duplicate nodes", but "a stale generation's nodes (and their
// closures) never reach the DOM at all". appendTreePage()'s DOM-clear step only
// removes ".loadmore"/".state" placeholders, never previously appended node
// wrappers, so without a generation guard the slower response's continuation
// re-appends onto the faster one's live tree -- every root container is listed
// twice, and (the severe dimension) a duplicated node's onclick closure can be
// bound to the WRONG service: after a db->mariadb switch, clicking a stale
// duplicate fired a request whose envelope said service "db" (the superseded
// service), and with write mode on a stale closure could mutate the wrong
// service. Family-agnostic (pure client-side race), so one fixture per
// scenario stands for all engines.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

// 1. Two root loads for the SAME service (e.g. applyWriteMode()'s own
//    loadTree() refresh racing the load selectService() kicked off at boot).
async function scenarioSameServiceRace() {
  const service = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [] };
  const treeResolvers = [];
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return new Promise((resolve) => treeResolvers.push(resolve));
    return null;
  };
  const treePage = () => jsonRoute({ nodes: [{ name: "f.txt", kind: "blob", path: { service: "db", segments: ["f.txt"] }, meta: { size: 5 } }] });

  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready sent to host" });

  // Boot: dataconsole-init deep-links straight into "db" -- selectService() fires
  // loadTree() as the FIRST root tree load, which hangs on treeResolvers[0].
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => treeResolvers.length === 1, { desc: "first (boot) tree fetch in flight" });

  // A write-mode confirmation from the host re-triggers loadTree() for the same
  // active service -- the SECOND root tree load, racing the first.
  hostPostMessage(c.window, { type: "dataconsole-write-mode", writeEnabled: true });
  await waitFor(() => treeResolvers.length === 2, { desc: "second (write-mode refresh) tree fetch in flight" });

  // Resolve the SECOND (faster) response first -- it renders the live tree.
  treeResolvers[1](treePage());
  await waitFor(() => c.document.querySelectorAll("#tree .node-wrap").length === 1, { desc: "second response renders its node" });

  // Now resolve the FIRST (slower) response -- STALE by the time it lands.
  treeResolvers[0](treePage());
  // No event to wait on for "nothing happened"; give the stale continuation's
  // microtasks a chance to run, then assert it did not duplicate the node.
  await new Promise((resolve) => setTimeout(resolve, 50));

  const count = c.document.querySelectorAll("#tree .node-wrap").length;
  c.close();
  assert.strictEqual(count, 1, "a stale (superseded) tree response must not duplicate the root container's nodes; got " + count);
}

// 2. A CROSS-service race (db -> mariadb switch while db's fetch is still in
//    flight): not just "exactly one node" but "clicking that node queries the
//    CURRENT service" -- proves a stale node's closure never lands, since a
//    stale closure bound to the superseded service is the severe dimension A2
//    found live (a wrong-service request, and under write mode a wrong-service
//    mutation).
async function scenarioCrossServiceRaceClickQueriesCurrentService() {
  const dbSvc = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }] };
  const mariaSvc = { hostname: "mariadb", type: "mariadb:single@10", support: "supported", actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }] };
  const treeResolvers = {};
  const blobCalls = [];
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [dbSvc, mariaSvc], allowWrites: true });
    if (p.startsWith("/api/tree")) {
      const svc = new URL("http://x" + p).searchParams.get("service");
      return new Promise((resolve) => { (treeResolvers[svc] = treeResolvers[svc] || []).push(resolve); });
    }
    if (p.startsWith("/api/blob")) {
      blobCalls.push(new URL("http://x" + p).searchParams.get("service"));
      return blobRoute("hello", { contentType: "text/plain" });
    }
    return null;
  };
  const nodeFor = (svc) => jsonRoute({ nodes: [{ name: "f.txt", kind: "blob", path: { service: svc, segments: ["f.txt"] }, meta: { size: 5 } }] });

  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => (treeResolvers.db || []).length === 1, { desc: "db tree fetch in flight" });

  // Switch to mariadb via a REAL sidebar click while db's tree fetch is pending.
  await waitFor(() => c.document.querySelectorAll("#services li").length === 2, { desc: "services rail render" });
  click(c.document.querySelectorAll("#services li")[1]); // [dbSvc, mariaSvc] -> index 1 is mariadb
  await waitFor(() => (treeResolvers.mariadb || []).length === 1, { desc: "mariadb tree fetch in flight" });

  // mariadb (the now-current service) resolves FIRST.
  treeResolvers.mariadb[0](nodeFor("mariadb"));
  await waitFor(() => c.document.querySelectorAll("#tree .node-wrap").length === 1, { desc: "mariadb node renders" });

  // db (stale -- superseded by the switch) resolves LAST.
  treeResolvers.db[0](nodeFor("db"));
  await new Promise((resolve) => setTimeout(resolve, 50));

  const count = c.document.querySelectorAll("#tree .node-wrap").length;
  assert.strictEqual(count, 1, "a cross-service stale response must not add a second (wrong-service) node; got " + count);

  // Click the sole rendered node -- its closure must query the CURRENT service,
  // never a stale closure bound to the superseded "db".
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => blobCalls.length === 1, { desc: "blob request fired" });
  c.close();
  assert.strictEqual(blobCalls[0], "mariadb", "clicking the rendered node must query the CURRENT service (mariadb), never a stale closure bound to the superseded service (db)");
}

// 3. A2's deterministic live repro shape: a service switch fires loadTree(),
//    then the #refresh ("Re-discover services") button fires a SECOND loadTree()
//    for the same service while the first is still in flight.
async function scenarioSwitchThenRefreshRace() {
  const service = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [] };
  const treeResolvers = [];
  let refreshCalls = 0;
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [service], allowWrites: true });
    if (method === "POST" && p === "/api/refresh") { refreshCalls++; return jsonRoute({ services: [service] }); }
    if (p.startsWith("/api/tree")) return new Promise((resolve) => treeResolvers.push(resolve));
    return null;
  };
  const treePage = () => jsonRoute({ nodes: [{ name: "f.txt", kind: "blob", path: { service: "db", segments: ["f.txt"] }, meta: { size: 5 } }] });

  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });

  // The initial deep-link select is the "switch" -- its tree load hangs.
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: true, service: "db" });
  await waitFor(() => treeResolvers.length === 1, { desc: "first (switch) tree fetch in flight" });

  // #refresh fires a SECOND loadTree for the same (still-active) service while
  // the first is still in flight.
  await waitFor(() => c.document.getElementById("refresh"), { desc: "refresh button present" });
  click(c.document.getElementById("refresh"));
  await waitFor(() => refreshCalls === 1, { desc: "/api/refresh was called" });
  await waitFor(() => treeResolvers.length === 2, { desc: "second (refresh) tree fetch in flight" });

  // The second (refresh) load resolves first.
  treeResolvers[1](treePage());
  await waitFor(() => c.document.querySelectorAll("#tree .node-wrap").length === 1, { desc: "refresh's response renders" });

  // The first (switch) load resolves last -- stale.
  treeResolvers[0](treePage());
  await new Promise((resolve) => setTimeout(resolve, 50));

  const count = c.document.querySelectorAll("#tree .node-wrap").length;
  c.close();
  assert.strictEqual(count, 1, "switch-then-refresh interleaving must not duplicate tree nodes; got " + count);
}

async function main() {
  await scenarioSameServiceRace();
  await scenarioCrossServiceRaceClickQueriesCurrentService();
  await scenarioSwitchThenRefreshRace();
  console.log("tree-race.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
