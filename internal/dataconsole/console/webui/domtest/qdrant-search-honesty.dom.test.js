"use strict";
// B9 (S3): the "Search ▸" link must not render a dead end. actions.go's
// familyReadActionIDs grants searchDocs to the WHOLE document family
// (elasticsearch/meilisearch/typesense/qdrant alike) and readAction() enables
// it for any support tier other than "not yet" -- so the server advertises
// searchDocs.enabled=true for qdrant too, even though qdrant implements no
// free-text searcher: document.go's `searcher` interface's type assertion
// fails for qdrant (its engine has no `search` method, only scroll/get), so
// Provider.Search always returns ErrUnsupported for it (pinned server-side by
// TestSearch_Qdrant_Unsupported) -- a 422 on every attempt. The server does
// not scope searchDocs to actual capability, and per the review brief this is
// a SPA-side fix only (no Go changes): gate the link additionally on the
// service's engine (baseType(s.type) !== "qdrant"), so the SPA does not
// advertise a control that always fails for this one engine. A real
// searchable engine (elasticsearch here) is the regression companion --
// features.dom.test.js's scenarioDocumentSearch already proves the full
// search flow works end to end; this only re-checks link PRESENCE.

const assert = require("assert");
const { buildConsole, waitFor, jsonRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function selectAndCheckSearchLink(service) {
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/", embedded: true, routes });
  await waitFor(() => c.rpcLog.some((m) => m.type === "dc-ready"), { desc: "dc-ready" });
  hostPostMessage(c.window, { type: "dataconsole-init", writeEnabled: false, service: service.hostname });
  // Wait for THIS service's own selection, not just any placeholder -- the
  // static index.html markup already ships a #content .placeholder before
  // any service is picked, so that alone is not proof selectService ran.
  await waitFor(() => c.document.getElementById("activesvc").textContent.includes(service.hostname), { desc: "service selected" });
  const present = !!c.document.getElementById("searchlink");
  c.close();
  return present;
}

async function main() {
  // Server advertises searchDocs.enabled=true for qdrant (mirrors the real
  // server's ServiceActions/readAction output for a view-only document-family
  // service) -- the SPA must gate it out anyway.
  const qdrant = {
    hostname: "vectors", type: "qdrant:single@1", support: "view-only",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }, { id: "searchDocs", enabled: true, readOnly: true, reason: "" }],
  };
  assert.strictEqual(await selectAndCheckSearchLink(qdrant), false, "qdrant never renders Search ▸ -- its engine has no free-text searcher (always 422s)");

  // A real searchable engine still gets the link (regression companion).
  const es = {
    hostname: "es", type: "elasticsearch:single@9", support: "supported",
    actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }, { id: "searchDocs", enabled: true, readOnly: true, reason: "" }],
  };
  assert.strictEqual(await selectAndCheckSearchLink(es), true, "a real searchable engine (elasticsearch) still renders Search ▸");

  console.log("qdrant-search-honesty.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
