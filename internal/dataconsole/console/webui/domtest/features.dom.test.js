"use strict";
// S22 feature-UI invariants, driven through the REAL render path (fetch -> app.js
// -> DOM). Pins: the stream metadata card (U-04 — a kafka/nats summary renders as a
// LABELLED card with PARSED fields, never an editable-looking blob; parsing also
// fixes the escaped-source artifact UI-AUD-02), and document search (U-13 — a match
// renders as a clickable id node, escaped like any untrusted name).

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute, blobRoute, hostPostMessage } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

function streamBlobRoute(obj) {
  const body = Buffer.from(JSON.stringify(obj), "utf8");
  return {
    status: 200,
    headers: {
      "content-type": "application/octet-stream",
      "x-dataconsole-contenttype": "application/json",
      "x-dataconsole-truncated": "false",
      "x-dataconsole-streammetadata": "true",
      "x-dataconsole-size": String(body.length),
    },
    bodyBytes: body,
  };
}

// 1. A stream summary renders as a labelled metadata card, not an editable <pre>,
//    and wildcard subjects render literally (UI-AUD-02), not as escaped JSON source.
async function scenarioStreamCard() {
  const service = { hostname: "queue", type: "nats:single@2", support: "view-only", actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }] };
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=queue",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "EVENTS", kind: "blob", path: { service: "queue", segments: ["EVENTS"] } }] });
      if (p.startsWith("/api/blob")) return streamBlobRoute({ stream: "EVENTS", subjects: ["events.>"], messages: 30, consumers: 2 });
      return null;
    },
  });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content .streamcard"), { desc: "stream card render" });
  const card = c.document.querySelector("#content .streamcard");
  assert.ok(card.querySelector(".streamlabel"), "the stream summary renders a LABELLED card (not a bare blob)");
  assert.ok(/not message content/i.test(card.querySelector(".streamlabel").textContent), "the card states it is metadata, not message content (U-04)");
  assert.strictEqual(c.document.querySelectorAll("#content pre.blob").length, 0, "a stream summary is NOT rendered as an editable-looking <pre> blob");
  assert.ok(/events\.>/.test(card.textContent), "wildcard subject renders literally (events.>), not escaped JSON source \\u003e (UI-AUD-02)");
  assert.ok(/30/.test(card.textContent), "the message count is shown");
  c.close();
}

// 2. Kafka consumer-groups unavailable-by-privilege surfaces as an explicit label.
async function scenarioStreamConsumerUnavailable() {
  const service = { hostname: "events", type: "kafka:single@3", support: "view-only", actions: [{ id: "readBlob", enabled: true, readOnly: true, reason: "" }] };
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=events",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [{ name: "orders", kind: "blob", path: { service: "events", segments: ["orders"] } }] });
      if (p.startsWith("/api/blob")) return streamBlobRoute({ topic: "orders", partitions: 6, partitionIds: [0, 1, 2, 3, 4, 5], consumerGroups: { available: false, reason: "unavailable" } });
      return null;
    },
  });
  await waitFor(() => c.document.querySelector("#tree .node"), { desc: "tree node" });
  click(c.document.querySelector("#tree .node"));
  await waitFor(() => c.document.querySelector("#content .streamcard"), { desc: "stream card render" });
  const card = c.document.querySelector("#content .streamcard");
  assert.ok(/unavailable/i.test(card.textContent), "consumer groups unavailable-by-privilege is shown honestly as 'unavailable', never a false zero");
  assert.ok(/6/.test(card.textContent), "the partition count is shown");
  c.close();
}

// 3. Document search: a match renders as a clickable id node; the match id is
//    escaped (an untrusted id can never inject markup — search results are ids only).
async function scenarioDocumentSearch() {
  const service = { hostname: "es", type: "elasticsearch:single@9", support: "supported", actions: [{ id: "searchDocs", enabled: true, readOnly: true, reason: "" }, { id: "readBlob", enabled: true, readOnly: true, reason: "" }] };
  const evil = '<img src=x onerror="window.__xssFired=true">';
  const c = buildConsole({
    url: "http://localhost/#t=FAKE&svc=es",
    routes: (method, p) => {
      if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
      if (p.startsWith("/api/tree")) {
        // root lists the index; drilling into it lists no docs (keeps the fixture
        // finite — the lone-container auto-expand must not re-list the same node).
        const segs = new URLSearchParams(p.split("?")[1] || "").get("segs") || "[]";
        if (segs === "[]") return jsonRoute({ nodes: [{ name: "articles", kind: "container", path: { service: "es", segments: ["articles"] }, hasChildren: true }] });
        return jsonRoute({ nodes: [] });
      }
      if (p.startsWith("/api/search")) return jsonRoute({ nodes: [{ name: evil, kind: "blob", path: { service: "es", segments: ["articles", evil] } }] });
      return null;
    },
  });
  await waitFor(() => c.document.getElementById("searchlink"), { desc: "search link render" });
  click(c.document.getElementById("searchlink"));
  await waitFor(() => c.document.getElementById("runs"), { desc: "search pane render" });
  c.document.getElementById("sq").value = "hello";
  click(c.document.getElementById("runs"));
  await waitFor(() => c.document.querySelector("#sresult .node"), { desc: "search result render" });
  assert.ok(!c.window.__xssFired, "an untrusted match id never executes as markup (search results are escaped)");
  assert.strictEqual(c.document.querySelector("#sresult .node .nname").textContent, evil, "the match id renders as escaped text (recovered verbatim via textContent)");
  c.close();
}

async function main() {
  await scenarioStreamCard();
  await scenarioStreamConsumerUnavailable();
  await scenarioDocumentSearch();
  console.log("features.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
