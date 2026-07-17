"use strict";
// P3 (S3, visual-polish): the query pane's error must read as the SAME
// "boxed" error surface as everywhere else (UX finding
// UX-query-error-inline-vs-toast: a SQL syntax error rendered as bare
// colored text with no border/background, a different visual shape than the
// toast box used for e.g. a document create conflict). Fix: `.err`
// (dc-errors.js's errorHTML, already the query pane's error element) gets a
// background + left accent border + padding in style.css -- ONE canon, so
// every `.err` surface (query, search, gate() fallbacks) gains the same box,
// not a query-only special case.
//
// jsdom applies no CSS cascade, so this only pins the DOM shape (the shared
// `.err` class + the rejection detail text passing through
// userErrorMessage); the visual box itself is confirmed by the post-deploy
// re-gallery.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute } = require("./harness");

const PROJECT = { id: "p1", name: "Proj" };

async function scenarioQueryErrorIsBoxedWithDetail() {
  const service = { hostname: "db", type: "postgresql:single@18", support: "supported", actions: [{ id: "querySQL", enabled: true, readOnly: true, reason: "" }] };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: PROJECT, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [] });
    if (p === "/api/query") return jsonRoute(
      { code: "invalid", message: 'invalid request: syntax error at or near "SELEKT"', requestId: "req-1", service: "db", family: "tabular", action: "querySQL" },
      { status: 400 },
    );
    return null;
  };
  const c = buildConsole({ url: "http://localhost/#t=FAKE&svc=db", routes });
  await waitFor(() => c.document.getElementById("querylink"), { desc: "query link render" });
  click(c.document.getElementById("querylink"));
  await waitFor(() => c.document.getElementById("runq"), { desc: "query console render" });
  c.document.getElementById("qtext").value = "SELEKT * FROM t";
  click(c.document.getElementById("runq"));
  await waitFor(() => c.document.querySelector("#qresult .err"), { desc: "query error renders" });
  const err = c.document.querySelector("#qresult .err");
  assert.ok(err, "the query error renders inside #qresult carrying the shared boxed .err class");
  assert.ok(
    err.textContent.includes('Invalid request — syntax error at or near "SELEKT".'),
    "the rejection's specific detail passes through userErrorMessage, not a generic dump: " + err.textContent,
  );
  c.close();
}

async function main() {
  await scenarioQueryErrorIsBoxedWithDetail();
  console.log("query-error-boxed.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
