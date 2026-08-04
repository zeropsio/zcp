"use strict";
// Search prompt and zero-hit states use the shared blank-slate renderer. The
// zero-hit title echoes hostile user input as text, never as live markup.

const assert = require("assert");
const { buildConsole, waitFor, click, jsonRoute } = require("./harness");

const PAYLOAD = '<img src=x onerror="window.__blankSlateXSS=true">';

async function main() {
  const service = {
    hostname: "docs", type: "elasticsearch:single@9", family: "document", support: "supported",
    actions: [{ id: "searchDocs", enabled: true, readOnly: true, reason: "" }],
  };
  const routes = (method, p) => {
    if (p === "/api/services") return jsonRoute({ project: { id: "p1", name: "Proj" }, services: [service], allowWrites: true });
    if (p.startsWith("/api/tree")) return jsonRoute({ nodes: [
      { name: "articles", kind: "container", path: { service: "docs", segments: ["articles"] } },
      { name: "archive", kind: "container", path: { service: "docs", segments: ["archive"] } },
    ] });
    if (p.startsWith("/api/search")) return jsonRoute({ nodes: [] });
    return null;
  };
  const c = buildConsole({ url: "http://localhost/#t=FAKE&svc=docs", routes });
  c.window.__blankSlateXSS = false;
  await waitFor(() => c.document.getElementById("searchlink"), { desc: "search link" });
  click(c.document.getElementById("searchlink"));
  await waitFor(() => c.document.getElementById("sq"), { desc: "search console" });

  click(c.document.getElementById("runs"));
  await waitFor(() => c.document.querySelector("#sresult > .state.empty"), { desc: "search prompt slate" });
  const prompt = c.document.querySelector("#sresult > .state.empty");
  assert.strictEqual(prompt.querySelector(".state-title").textContent, "Enter search text", "an empty search renders the prompt title through the blank-slate canon");
  assert.ok(prompt.querySelector(".state-detail").textContent.length > 0, "the search prompt includes a next-step detail");

  c.document.getElementById("sq").value = PAYLOAD;
  click(c.document.getElementById("runs"));
  await waitFor(() => {
    const title = c.document.querySelector("#sresult .state-title");
    return title && title.textContent.includes(PAYLOAD);
  }, { desc: "escaped zero-hit query echo" });

  const slate = c.document.querySelector("#sresult > .state.empty");
  assert.strictEqual(slate.querySelector(".state-title").textContent, `No matches for "${PAYLOAD}"`, "the original query is preserved in the zero-hit title");
  assert.strictEqual(slate.querySelectorAll("img").length, 0, "the hostile query injects no image element");
  assert.strictEqual(slate.querySelectorAll("[onerror]").length, 0, "the hostile query injects no event handler");
  assert.ok(!slate.innerHTML.includes("<img"), "the serialized slate contains no raw hostile tag");
  assert.strictEqual(c.window.__blankSlateXSS, false, "the hostile query remains inert");
  c.close();

  console.log("blank-slate-search-xss.dom.test.js OK");
}

main().catch((e) => { console.error(e); process.exit(1); });
