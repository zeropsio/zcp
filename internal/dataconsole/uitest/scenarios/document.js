"use strict";

// DOC-1..DOC-6 — document family (elasticsearch, typesense, meilisearch full
// write; qdrant view-only) against the REAL deployed container.
//
// Scope: spec-dataconsole.md §7.5 "document" contract (index/collection browse,
// per-id docs as JSON, bounded read-only search, create+edit with the
// id-immutability guard, meili task-confirmed writes, qdrant collapsed-vector
// view-only). Cross-cuts I-1 (success never lies), I-2 (honest structured
// errors), I-4 (create/identity). KI-1 (CORE-1-S1-delete-rejected, filed by
// A1): a body-addressed mutation returning a generic 400 "Invalid request." is
// a KNOWN issue -- every mutation check below recognizes that shape and files a
// finding referencing KI-1 by id rather than treating it as a fresh unknown.
//
// Engine identity quirks (load-bearing for DOC-3/DOC-4, verified against
// provider/document/*.go before writing this file): Elasticsearch addresses a
// document by the URL id and only refuses a body carrying a MISMATCHED `_id`
// (an ordinary "id" field is inert); Typesense and Meilisearch instead ROUTE by
// a body field (typesense: "id"; meilisearch: the index's primaryKey, "id"
// here) and REQUIRE it to equal the path id. So every create/edit body built
// below includes a matching "id" field -- required for typesense/meilisearch,
// harmless for elasticsearch -- so a rejection observed live is a genuine
// product finding, never this harness misconstructing the body.
//
// Multi-engine scenarios: each DOC-N scenario id spans 3-4 engines in one run
// (run.js has no per-engine scenario selection). A genuine harness-can't-drive
// error for ONE engine is caught at that engine's boundary, recorded as an S1
// finding (an inability to even drive the UI is itself often symptomatic), and
// the loop continues to the next engine -- maximizing information from one
// pass rather than losing every other engine's results to one engine's wall.

const runner = require("../lib/runner");
const { loadConfig } = require("../lib/config");

const PREFIX = "uitest_docs"; // es/typesense/meili container name
const VEC = "uitest_vec"; // qdrant collection name

const ES_SERVICE = "es";
const DOCS_SERVICE = "docs"; // typesense
const SEARCH_SERVICE = "search"; // meilisearch
const VECTORS_SERVICE = "vectors"; // qdrant

const XSS_TITLE = '<script>window.__uitest_xss=1</script>';
const XSS_BODY = '<img src=x onerror="window.__uitest_xss=2">';

const FIXTURE_DOCS = [
  { id: "uitest_plain1", title: "Plain document", body: "hello world", count: 42 },
  // body deliberately avoids any accent-folded collision with the "hello"
  // query DOC-2 uses to find exactly uitest_plain1 -- typesense/meilisearch
  // both diacritic-fold by default (a "hello" query legitimately matches
  // "héllo"), unlike elasticsearch's default standard analyzer. Confirmed
  // live: an earlier fixture using "héllo wörld" here made "hello" match
  // BOTH docs on typesense/meilisearch -- a fixture-design bug, not a search
  // correctness bug in the product.
  { id: "uitest_uni1", title: "Příliš žluťoučký 💾", body: "unicode text sample: 日本語 emoji 🎉 déjà vu" },
  { id: "uitest_nest1", title: "Nested doc", meta: { tags: ["a", "b", "c"], info: { x: 1, y: true } } },
  { id: "uitest_xss1", title: XSS_TITLE, body: XSS_BODY },
];
const FIXTURE_IDS = FIXTURE_DOCS.map((d) => d.id);

const VEC_POINTS = [
  { id: 101, vector: [0.1, 0.2, 0.3, 0.4], payload: { name: "point alpha", tag: "first" } },
  { id: 102, vector: [0.9, 0.8, 0.7, 0.6], payload: { name: "point beta", tag: "second" } },
  { id: 103, vector: [0.5, 0.5, 0.5, 0.5], payload: { name: XSS_TITLE, note: XSS_BODY } },
];

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// TOAST_LIFETIME_MS mirrors app.js's toast() (2.6s self-removal, style.css).
// drainToast() is called after every waitToast() in a multi-mutation scenario
// (DOC-3 chains 3 create attempts, DOC-4 chains edit+delete) so the NEXT
// waitToast() cannot pick up a still-fading PRIOR toast (README Gotcha #8).
const TOAST_LIFETIME_MS = 2600;
async function drainToast() {
  await sleep(TOAST_LIFETIME_MS + 250);
}

function cfg() {
  return loadConfig();
}

// ---- generic HTTP-over-SSH-curl (every engine speaks HTTP; see lib/engines.js
// container()/shellQuote() -- this composes on top, never raw string interp) ----
function httpJSON(engines, authArgs, method, url, bodyObj) {
  let cmd = "curl -s -X " + method + " " + authArgs + " " + url;
  if (bodyObj !== undefined) {
    cmd += " -H " + engines.shellQuote("Content-Type: application/json") +
      " --data-binary " + engines.shellQuote(JSON.stringify(bodyObj));
  }
  const out = engines.container(cmd);
  if (!out) return null;
  try { return JSON.parse(out); } catch (_) { return out; }
}

function esUrl(path) { const c = cfg(); return "http://" + c.DC_ES_HOST + ":" + c.DC_ES_PORT + path; }
function tsUrl(path) { const c = cfg(); return "http://" + c.DC_TYPESENSE_HOST + ":" + c.DC_TYPESENSE_PORT + path; }
function mkUrl(path) { const c = cfg(); return "http://" + c.DC_MEILI_HOST + ":" + c.DC_MEILI_PORT + path; }
function qdUrl(path) { const c = cfg(); return "http://" + c.DC_QDRANT_HOST + ":" + c.DC_QDRANT_PORT + path; }

function esRequest(engines, method, path, bodyObj) {
  const c = cfg();
  const auth = "-u " + engines.shellQuote(c.DC_ES_USER + ":" + c.DC_ES_PASSWORD);
  return httpJSON(engines, auth, method, esUrl(path), bodyObj);
}
function tsRequest(engines, method, path, bodyObj) {
  const hdr = "-H " + engines.shellQuote("X-TYPESENSE-API-KEY: " + cfg().DC_TYPESENSE_KEY);
  return httpJSON(engines, hdr, method, tsUrl(path), bodyObj);
}
function mkRequest(engines, method, path, bodyObj) {
  const hdr = "-H " + engines.shellQuote("Authorization: Bearer " + cfg().DC_MEILI_KEY);
  return httpJSON(engines, hdr, method, mkUrl(path), bodyObj);
}
function qdRequest(engines, method, path, bodyObj) {
  const hdr = "-H " + engines.shellQuote("api-key: " + cfg().DC_QDRANT_KEY);
  return httpJSON(engines, hdr, method, qdUrl(path), bodyObj);
}

// ---- elasticsearch fixtures/oracle ----
function esSetup(engines) {
  try { esRequest(engines, "DELETE", "/" + PREFIX); } catch (_) { /* best effort */ }
  for (const d of FIXTURE_DOCS) esRequest(engines, "PUT", "/" + PREFIX + "/_doc/" + d.id, d);
  esRequest(engines, "POST", "/" + PREFIX + "/_refresh");
}
function esTeardown(engines) {
  try { esRequest(engines, "DELETE", "/" + PREFIX); } catch (_) { /* best effort */ }
}
function esDocCount(engines) {
  const r = esRequest(engines, "GET", "/" + PREFIX + "/_count");
  return r && typeof r.count === "number" ? r.count : null;
}
function esGetDoc(engines, id) {
  const r = esRequest(engines, "GET", "/" + PREFIX + "/_doc/" + id);
  return r && r.found ? r._source : null;
}

// ---- typesense fixtures/oracle ----
function tsSetup(engines) {
  try { tsRequest(engines, "DELETE", "/collections/" + PREFIX); } catch (_) { /* best effort */ }
  tsRequest(engines, "POST", "/collections", { name: PREFIX, fields: [{ name: ".*", type: "auto" }], enable_nested_fields: true });
  for (const d of FIXTURE_DOCS) tsRequest(engines, "POST", "/collections/" + PREFIX + "/documents", d);
}
function tsTeardown(engines) {
  try { tsRequest(engines, "DELETE", "/collections/" + PREFIX); } catch (_) { /* best effort */ }
}
function tsDocCount(engines) {
  const r = tsRequest(engines, "GET", "/collections/" + PREFIX);
  return r && typeof r.num_documents === "number" ? r.num_documents : null;
}
function tsGetDoc(engines, id) {
  const r = tsRequest(engines, "GET", "/collections/" + PREFIX + "/documents/" + id);
  return r && !r.message ? r : null;
}

// ---- meilisearch fixtures/oracle (async -- every write task-polled to terminal) ----
async function mkWaitTask(engines, taskUid, timeoutMs) {
  const deadline = Date.now() + (timeoutMs || 10000);
  let last = null;
  while (Date.now() < deadline) {
    last = mkRequest(engines, "GET", "/tasks/" + taskUid);
    if (last && (last.status === "succeeded" || last.status === "failed" || last.status === "canceled")) return last;
    await sleep(300);
  }
  return last;
}
async function mkSetup(engines) {
  try {
    const del = mkRequest(engines, "DELETE", "/indexes/" + PREFIX);
    if (del && del.taskUid != null) await mkWaitTask(engines, del.taskUid);
  } catch (_) { /* best effort */ }
  const create = mkRequest(engines, "POST", "/indexes", { uid: PREFIX, primaryKey: "id" });
  if (create && create.taskUid != null) await mkWaitTask(engines, create.taskUid);
  const add = mkRequest(engines, "POST", "/indexes/" + PREFIX + "/documents", FIXTURE_DOCS);
  if (add && add.taskUid != null) await mkWaitTask(engines, add.taskUid);
}
async function mkTeardown(engines) {
  try {
    const del = mkRequest(engines, "DELETE", "/indexes/" + PREFIX);
    if (del && del.taskUid != null) await mkWaitTask(engines, del.taskUid, 5000);
  } catch (_) { /* best effort */ }
}
function mkDocCount(engines) {
  const r = mkRequest(engines, "GET", "/indexes/" + PREFIX + "/stats");
  return r && typeof r.numberOfDocuments === "number" ? r.numberOfDocuments : null;
}
function mkGetDoc(engines, id) {
  const r = mkRequest(engines, "GET", "/indexes/" + PREFIX + "/documents/" + id);
  return r && !r.code ? r : null;
}

// ---- qdrant fixtures/oracle (view-only in the product; still needs seeding) ----
function qdSetup(engines) {
  try { qdRequest(engines, "DELETE", "/collections/" + VEC); } catch (_) { /* best effort */ }
  qdRequest(engines, "PUT", "/collections/" + VEC, { vectors: { size: 4, distance: "Cosine" } });
  qdRequest(engines, "PUT", "/collections/" + VEC + "/points?wait=true", { points: VEC_POINTS });
}
function qdTeardown(engines) {
  try { qdRequest(engines, "DELETE", "/collections/" + VEC); } catch (_) { /* best effort */ }
}
function qdPointCount(engines) {
  const r = qdRequest(engines, "GET", "/collections/" + VEC);
  return r && r.result && typeof r.result.points_count === "number" ? r.result.points_count : null;
}

// ENGINES: the three full-write document engines DOC-1..DOC-5 loop over.
// createBody: elasticsearch ignores an "id" field (identity is the URL id;
// only a MISMATCHED "_id" is refused), but typesense/meilisearch ROUTE by a
// body field and require it to equal the target id -- see file header.
const ENGINES = [
  {
    key: "es", service: ES_SERVICE, label: "elasticsearch",
    setup: esSetup, teardown: esTeardown, getDoc: esGetDoc, docCount: esDocCount,
    createBody: (id) => ({ id, title: "UI-created " + id, body: "created via DOC-3" }),
  },
  {
    key: "typesense", service: DOCS_SERVICE, label: "typesense",
    setup: tsSetup, teardown: tsTeardown, getDoc: tsGetDoc, docCount: tsDocCount,
    createBody: (id) => ({ id, title: "UI-created " + id, body: "created via DOC-3" }),
  },
  {
    key: "meilisearch", service: SEARCH_SERVICE, label: "meilisearch",
    setup: mkSetup, teardown: mkTeardown, getDoc: mkGetDoc, docCount: mkDocCount,
    createBody: (id) => ({ id, title: "UI-created " + id, body: "created via DOC-3" }),
  },
];

// ---- SPA driving helpers (document-family tree/search/blob DOM shapes) ----

// waitForActiveService waits for #services li.active span to actually equal
// `service`. sidebarBrowse()/browseVia()'s OWN frame probe (#rail/#topbar/
// #services li) only proves the rail is populated -- true as soon as ANY
// service has ever loaded, not that THIS click's switch has landed yet. CORE-1
// (scenarios/core.js) carries this exact extra wait after its own
// sidebarBrowse call for the same reason; every entry point here needs it too,
// or a read can land mid-switch and observe the PREVIOUS service's tree
// (confirmed live: an early read captured "es"'s own tree, doubled, under a
// "typesense" label -- see the DOC-1 fix note in the file's evidence trail).
async function waitForActiveService(frame, service) {
  await frame.waitForFunction(
    (svc) => {
      const li = document.querySelector("#services li.active span");
      return !!li && li.textContent === svc;
    },
    { timeout: 15000 },
    service
  );
}

// enterService browses to `service` (sidebar re-browse -- mechanically the
// same as a first open, see harness.js), waits for the switch to actually
// land, drives write mode to the desired state, then re-probes the frame
// (Gotcha #9: never trust a handle across a switch/toggle even though the
// current implementation usually keeps it valid).
async function enterService(ctx, service, writeOn) {
  const { page, harness } = ctx;
  let spa = await harness.sidebarBrowse(page, service);
  await waitForActiveService(spa, service);
  await harness.setWriteMode(page, spa, writeOn);
  spa = await harness.spaFrame(page);
  return spa;
}

// rootNodeNames reads the TOP-LEVEL tree entries only (direct #tree children),
// never descending into an already-expanded container's .children.
async function rootNodeNames(frame) {
  return frame.evaluate(() =>
    Array.from(document.querySelectorAll("#tree > .node-wrap > .node .nname")).map((el) => el.textContent)
  );
}

// waitForRootNode polls until a ROOT-level container named `name` exists in
// #tree, OUTSIDE of a loading state. #tree gets rebuilt (cleared, then
// refetched) not only by a service switch but also as a SIDE EFFECT of
// setWriteMode() landing (applyWriteMode() calls loadTree() again to refresh
// affordances) -- a single-shot lookup can land in the brief window where the
// tree was just cleared and not yet repopulated. Confirmed live: without this
// wait, expandTreeNode() right after setWriteMode(true) intermittently failed
// to find a container that was, provably, back a moment later.
async function waitForRootNode(frame, name, timeoutMs) {
  try {
    await frame.waitForFunction(
      (n) => {
        if (document.querySelector("#tree > .state.loading")) return false;
        const wraps = Array.from(document.querySelectorAll("#tree > .node-wrap"));
        return wraps.some((w) => {
          const nm = w.querySelector(":scope > .node .nname");
          return nm && nm.textContent === n;
        });
      },
      { timeout: timeoutMs || 15000 },
      name
    );
    return true;
  } catch (_) {
    return false;
  }
}

// expandTreeNode clicks a ROOT-level container node by name and waits for its
// .children to land (created + no longer showing the loading placeholder).
async function expandTreeNode(frame, name) {
  if (!(await waitForRootNode(frame, name))) return false;
  const clicked = await frame.evaluate((n) => {
    const wraps = Array.from(document.querySelectorAll("#tree > .node-wrap"));
    const wrap = wraps.find((w) => {
      const nm = w.querySelector(":scope > .node .nname");
      return nm && nm.textContent === n;
    });
    if (!wrap) return false;
    wrap.querySelector(":scope > .node").click();
    return true;
  }, name);
  if (!clicked) return false;
  try {
    await frame.waitForFunction(
      (n) => {
        const wraps = Array.from(document.querySelectorAll("#tree > .node-wrap"));
        const wrap = wraps.find((w) => {
          const nm = w.querySelector(":scope > .node .nname");
          return nm && nm.textContent === n;
        });
        const kids = wrap ? wrap.querySelector(":scope > .children") : null;
        return !!kids && !kids.querySelector(".state.loading");
      },
      { timeout: 15000 },
      name
    );
  } catch (_) {
    return false;
  }
  return true;
}

// childNodeNamesOf reads the DIRECT children of an already-expanded root-level
// container (doc ids for es/typesense/meili; point ids for qdrant).
async function childNodeNamesOf(frame, containerName) {
  return frame.evaluate((n) => {
    const wraps = Array.from(document.querySelectorAll("#tree > .node-wrap"));
    const wrap = wraps.find((w) => {
      const nm = w.querySelector(":scope > .node .nname");
      return nm && nm.textContent === n;
    });
    const kids = wrap ? wrap.querySelector(":scope > .children") : null;
    if (!kids) return null;
    return Array.from(kids.querySelectorAll(":scope > .node-wrap > .node .nname")).map((el) => el.textContent);
  }, containerName);
}

// clickChildNode opens one leaf (doc/point) nested under an expanded root-level
// container -- scoped so a same-named leaf under a DIFFERENT container (e.g.
// qdrant's pre-existing "items" collection) can never be clicked by mistake.
async function clickChildNode(frame, containerName, childName) {
  return frame.evaluate((cn, kn) => {
    const wraps = Array.from(document.querySelectorAll("#tree > .node-wrap"));
    const wrap = wraps.find((w) => {
      const nm = w.querySelector(":scope > .node .nname");
      return nm && nm.textContent === cn;
    });
    const kids = wrap ? wrap.querySelector(":scope > .children") : null;
    if (!kids) return false;
    const rows = Array.from(kids.querySelectorAll(":scope > .node-wrap > .node"));
    const row = rows.find((r) => {
      const nm = r.querySelector(".nname");
      return nm && nm.textContent === kn;
    });
    if (!row) return false;
    row.click();
    return true;
  }, containerName, childName);
}

// clickSearchResult opens a result rendered flat into #sresult (NOT nested
// under #tree -- runSearch() appends renderNode() output directly).
async function clickSearchResult(frame, name) {
  return frame.evaluate((n) => {
    const rows = Array.from(document.querySelectorAll("#sresult .node"));
    const row = rows.find((r) => {
      const nm = r.querySelector(".nname");
      return nm && nm.textContent === n;
    });
    if (!row) return false;
    row.click();
    return true;
  }, name);
}

// openSearchPane returns to the service's hint (re-selecting the rail entry
// resets #content to the hint even if a blob/search view is currently shown)
// then opens the search pane from there -- robust regardless of prior state.
async function openSearchPane(ctx, spa, service) {
  await ctx.harness.clickService(spa, service);
  await spa.waitForSelector("#searchlink", { timeout: 15000 });
  await spa.click("#searchlink");
  await spa.waitForSelector(".searchbar", { timeout: 15000 });
}

// runDocSearch selects the index, clears+types the query (the input is reused
// across calls within one openSearchPane render, so it is NOT empty by default
// on a second query) and runs it.
async function runDocSearch(spa, indexName, query) {
  await spa.select("#sidx", indexName);
  await spa.evaluate(() => { document.getElementById("sq").value = ""; });
  if (query) await spa.type("#sq", query);
  await spa.click("#runs");
}

async function readBlobView(frame) {
  return frame.evaluate(() => {
    const pre = document.querySelector("pre.blob");
    const ta = document.querySelector("#blobedit");
    return {
      hasSave: !!document.getElementById("saveblob"),
      hasDelete: !!document.getElementById("delblob"),
      hasRename: !!document.getElementById("renameblob"),
      preText: pre ? pre.textContent : null,
      editorText: ta ? ta.value : null,
    };
  });
}

async function readVectorView(frame) {
  return frame.evaluate(() => {
    const box = document.querySelector(".vectorbox");
    if (!box) return { hasVectorBox: false };
    const summary = box.querySelector(".vecsummary");
    const raw = box.querySelector("pre.blob.vecraw");
    const doc = box.querySelector("pre.blob:not(.vecraw)");
    return {
      hasVectorBox: true,
      summaryText: summary ? summary.textContent : null,
      rawHiddenInitially: raw ? raw.classList.contains("hidden") : null,
      rawText: raw ? raw.textContent : null,
      payloadText: doc ? doc.textContent : null,
      hasSave: !!document.getElementById("saveblob"),
      hasDelete: !!document.getElementById("delblob"),
      hasRename: !!document.getElementById("renameblob"),
    };
  });
}

async function setEditorValue(frame, selector, text) {
  await frame.evaluate((sel, val) => { document.querySelector(sel).value = val; }, selector, text);
}

// confirmSpaModal handles the SPA's OWN #modal/#modalok (Gotcha #6) -- never
// the native VS Code dialog, which only guards the write-mode toggle.
async function confirmSpaModal(spa, evidence, name) {
  await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
  if (evidence && name) await evidence(name);
  await spa.click("#modalok");
}

// isLightColor is a cheap perceived-brightness check (ITU-R BT.601) used to
// flag a <select> whose computed background resolves to something light
// against the dark VS Code theme -- the exact class of "real reported design
// bug" DOC-2 is instructed to check.
function isLightColor(rgb) {
  const m = /rgba?\((\d+),\s*(\d+),\s*(\d+)/.exec(rgb || "");
  if (!m) return false;
  const r = Number(m[1]), g = Number(m[2]), b = Number(m[3]);
  return (r * 299 + g * 587 + b * 114) / 1000 > 150;
}

// assertXSSSafe is the shared DOC-5/DOC-6 payload-safety check: no execution,
// no injected <img>, and the literal markup visible as escaped TEXT (never
// silently stripped -- I-3 value fidelity applies to XSS-shaped values too).
async function assertXSSSafe(evidenceFn, addFinding, spa, eng, surface, idHint) {
  const state = await spa.evaluate(() => {
    const pre = document.querySelector("pre.blob");
    const ta = document.querySelector("#blobedit");
    const text = pre ? pre.textContent : (ta ? ta.value : null);
    return { flag: window.__uitest_xss, hasBadImg: !!document.querySelector('img[src="x"], img[onerror]'), text };
  });
  if (state.flag !== undefined) {
    addFinding({
      id: "DOC-5-" + eng.key + "-" + surface + "-executed", severity: "S1",
      title: eng.label + ": XSS payload EXECUTED when " + idHint + " opened via " + surface,
      repro: "open uitest_xss1 via " + surface + " on " + eng.service,
      expected: "window.__uitest_xss stays undefined",
      actual: "window.__uitest_xss=" + state.flag,
      evidence: [await evidenceFn(eng.key + "-XSS-EXECUTED-" + surface)],
    });
  }
  if (state.hasBadImg) {
    addFinding({
      id: "DOC-5-" + eng.key + "-" + surface + "-img-injected", severity: "S1",
      title: eng.label + ": XSS <img> tag injected into the DOM when " + idHint + " opened via " + surface,
      repro: "open uitest_xss1 via " + surface + " on " + eng.service,
      expected: "no img[src=x]/img[onerror] node anywhere in the frame",
      actual: "found", evidence: [await evidenceFn(eng.key + "-XSS-IMG-" + surface)],
    });
  }
  // The blob view renders PRETTY-PRINTED JSON (application/json), so a field
  // value's own internal double quotes are naturally JSON-escaped (\") in the
  // displayed text -- checking for the raw unescaped XSS_BODY substring can
  // never match valid JSON serialization. Parse the displayed text and
  // compare the DECODED field values instead: a stronger check anyway, since
  // it directly verifies value fidelity (I-3) rather than assuming one
  // particular escaping/formatting of the source text.
  let literalVisible = !!(state.text && state.text.indexOf("<script>") >= 0);
  if (literalVisible) {
    try {
      const parsed = JSON.parse(state.text);
      literalVisible = parsed.title === XSS_TITLE && parsed.body === XSS_BODY;
    } catch (_) {
      literalVisible = state.text.indexOf(XSS_TITLE) >= 0; // non-JSON surface (unexpected here, but don't crash)
    }
  }
  if (!literalVisible) {
    addFinding({
      severity: "S2",
      title: eng.label + ": XSS fixture's literal markup not visibly rendered as text via " + surface,
      repro: "open uitest_xss1 via " + surface + " on " + eng.service,
      expected: "rendered text includes the literal '<script>...' and img markup as visible text",
      actual: JSON.stringify(state.text), evidence: [],
    });
  }
}

// ============================== DOC-1 ==============================
// index/collection browse + doc listing: container visible, expands to the
// seeded doc/point set, rendered count honest vs engine truth. Seeds AFTER
// the first open (deliberately) to also exercise the sidebar re-browse's
// tree-refresh behavior the brief calls out.

async function runDOC1(ctx) {
  for (const eng of ENGINES) await doc1FullWrite(ctx, eng);
  await doc1Vectors(ctx);
}

async function doc1FullWrite(ctx, eng) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  try {
    await eng.teardown(engines);
    let spa = await harness.sidebarBrowse(page, eng.service);
    await waitForActiveService(spa, eng.service);
    await harness.setWriteMode(page, spa, false);
    spa = await harness.spaFrame(page);
    const before = await rootNodeNames(spa);
    await evidence(eng.key + "-01-before-seed");
    if (before.indexOf(PREFIX) >= 0) {
      addFinding({
        severity: "S3",
        title: eng.label + ": stale \"" + PREFIX + "\" container visible before seeding (leftover from a prior run?)",
        repro: "sidebarBrowse(" + eng.service + ") before seeding fixtures",
        expected: PREFIX + " absent from the root tree pre-seed",
        actual: "root nodes: " + JSON.stringify(before), evidence: [],
      });
    }

    await eng.setup(engines);
    spa = await harness.sidebarBrowse(page, eng.service);
    await waitForActiveService(spa, eng.service);
    await sleep(200);
    const after = await rootNodeNames(spa);
    await evidence(eng.key + "-02-after-seed-rebrowse");
    if (after.indexOf(PREFIX) < 0) {
      addFinding({
        severity: "S1",
        title: eng.label + ": newly-seeded container \"" + PREFIX + "\" not visible after a sidebar re-browse",
        repro: "seed " + PREFIX + " via curl; sidebarBrowse(page,\"" + eng.service + "\")",
        expected: "root tree includes \"" + PREFIX + "\"",
        actual: "root nodes: " + JSON.stringify(after), evidence: [],
        engine_truth: eng.label + " confirms the container/index exists (seeded via curl)",
      });
      return;
    }

    // Duplicate-root check BEFORE expanding: two back-to-back sidebarBrowse
    // calls to the SAME service (exactly what just happened: before-seed open,
    // then after-seed re-browse) is a live, reproducible trigger for a tree
    // render race -- see the dedicated finding below.
    const dupeCounts = {};
    for (const n of after) dupeCounts[n] = (dupeCounts[n] || 0) + 1;
    const dupes = Object.keys(dupeCounts).filter((n) => dupeCounts[n] > 1);
    if (dupes.length) {
      addFinding({
        id: "DOC-1-" + eng.key + "-dupe-root-nodes", severity: "S2",
        title: eng.label + ": re-browsing the SAME already-open service renders DUPLICATE root tree nodes",
        repro: "sidebarBrowse(" + eng.service + ") [open #1]; seed fixtures (~1-2s of curl calls); " +
          "sidebarBrowse(" + eng.service + ") again [open #2, same service] -- root tree ends up with each " +
          "container listed twice (both open #1's and open #2's loadTree() results present at once)",
        expected: "re-browsing the same service replaces the tree, one entry per container",
        actual: "duplicated root names: " + JSON.stringify(dupes) + "; full list: " + JSON.stringify(after),
        evidence: [await evidence(eng.key + "-02b-duplicate-root-nodes")],
        engine_truth: eng.label + " reports exactly one \"" + PREFIX + "\" container",
      });
    }

    const expanded = await expandTreeNode(spa, PREFIX);
    await sleep(200);
    await evidence(eng.key + "-03-expanded");
    if (!expanded) {
      addFinding({
        severity: "S1",
        title: eng.label + ": could not expand \"" + PREFIX + "\" container in the tree",
        repro: "click the " + PREFIX + " tree node",
        expected: "container expands to show doc leaves",
        actual: "click had no effect / children never landed", evidence: [],
      });
      return;
    }
    const children = await childNodeNamesOf(spa, PREFIX);
    const engineCount = eng.docCount(engines);
    const sortedChildren = (children || []).slice().sort();
    const sortedFixture = FIXTURE_IDS.slice().sort();
    if (JSON.stringify(sortedChildren) !== JSON.stringify(sortedFixture)) {
      addFinding({
        severity: "S1",
        title: eng.label + ": tree doc listing does not match the seeded fixture set",
        repro: "expand " + PREFIX + "; read child .nname list",
        expected: JSON.stringify(sortedFixture), actual: JSON.stringify(sortedChildren),
        evidence: [await evidence(eng.key + "-03b-children-mismatch")],
        engine_truth: eng.label + " doc count = " + engineCount,
      });
    } else if (engineCount !== FIXTURE_IDS.length) {
      addFinding({
        severity: "S2",
        title: eng.label + ": engine doc count does not match the seeded fixture count (tree happened to agree with the fixture, not the engine)",
        repro: "compare " + eng.label + " doc count to the seeded fixture count",
        expected: String(FIXTURE_IDS.length), actual: String(engineCount), evidence: [],
      });
    }
  } catch (e) {
    addFinding({
      severity: "S1",
      title: eng.label + ": harness could not drive DOC-1 to completion",
      repro: "DOC-1 / " + eng.service,
      expected: "tree browse completes", actual: String(e && e.message ? e.message : e), evidence: [],
    });
  } finally {
    await eng.teardown(engines);
  }
}

async function doc1Vectors(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  try {
    qdTeardown(engines);
    let spa = await harness.sidebarBrowse(page, VECTORS_SERVICE);
    await waitForActiveService(spa, VECTORS_SERVICE);
    await harness.setWriteMode(page, spa, false);
    spa = await harness.spaFrame(page);
    qdSetup(engines);
    spa = await harness.sidebarBrowse(page, VECTORS_SERVICE);
    await waitForActiveService(spa, VECTORS_SERVICE);
    await sleep(200);
    const after = await rootNodeNames(spa);
    await evidence("vectors-01-after-seed");
    if (after.indexOf(VEC) < 0) {
      addFinding({
        severity: "S1",
        title: "qdrant: newly-seeded collection \"" + VEC + "\" not visible after a sidebar re-browse",
        repro: "seed " + VEC + " via curl; sidebarBrowse(page,\"vectors\")",
        expected: "root tree includes \"" + VEC + "\"",
        actual: "root nodes: " + JSON.stringify(after), evidence: [],
      });
      return;
    }
    const vdupeCounts = {};
    for (const n of after) vdupeCounts[n] = (vdupeCounts[n] || 0) + 1;
    const vdupes = Object.keys(vdupeCounts).filter((n) => vdupeCounts[n] > 1);
    if (vdupes.length) {
      addFinding({
        id: "DOC-1-vectors-dupe-root-nodes", severity: "S2",
        title: "qdrant: re-browsing the SAME already-open service renders DUPLICATE root tree nodes",
        repro: "sidebarBrowse(vectors) [open #1]; seed fixtures; sidebarBrowse(vectors) again [open #2, same service]",
        expected: "re-browsing the same service replaces the tree, one entry per collection",
        actual: "duplicated root names: " + JSON.stringify(vdupes) + "; full list: " + JSON.stringify(after),
        evidence: [await evidence("vectors-01b-duplicate-root-nodes")],
        engine_truth: "qdrant reports exactly one \"" + VEC + "\" collection",
      });
    }

    const expanded = await expandTreeNode(spa, VEC);
    await sleep(200);
    await evidence("vectors-02-expanded");
    if (!expanded) {
      addFinding({
        severity: "S1",
        title: "qdrant: could not expand \"" + VEC + "\" collection in the tree",
        repro: "click the " + VEC + " tree node",
        expected: "expands to show points", actual: "click had no effect / children never landed", evidence: [],
      });
      return;
    }
    const children = (await childNodeNamesOf(spa, VEC) || []).slice().sort();
    const expectedIds = VEC_POINTS.map((p) => String(p.id)).sort();
    if (JSON.stringify(children) !== JSON.stringify(expectedIds)) {
      addFinding({
        severity: "S1",
        title: "qdrant: tree point listing does not match the seeded fixture set",
        repro: "expand " + VEC + "; read child .nname list",
        expected: JSON.stringify(expectedIds), actual: JSON.stringify(children),
        evidence: [await evidence("vectors-02b-children-mismatch")],
        engine_truth: "qdrant points_count = " + qdPointCount(engines),
      });
    }
  } catch (e) {
    addFinding({
      severity: "S1", title: "qdrant: harness could not drive DOC-1 to completion",
      repro: "DOC-1 / vectors", expected: "tree browse completes",
      actual: String(e && e.message ? e.message : e), evidence: [],
    });
  } finally {
    qdTeardown(engines);
  }
}

// ============================== DOC-2 ==============================
// search pane: renders (with a dark-styled index <select> -- the reported
// design bug), one distinctive query returns exactly the matching doc, a
// no-match query renders the honest empty state.

async function runDOC2(ctx) {
  for (const eng of ENGINES) await doc2ForEngine(ctx, eng);
}

async function doc2ForEngine(ctx, eng) {
  const { engines, evidence, addFinding } = ctx;
  try {
    await eng.teardown(engines);
    await eng.setup(engines);
    const spa = await enterService(ctx, eng.service, false);
    await openSearchPane(ctx, spa, eng.service);
    await spa.select("#sidx", PREFIX);

    const style = await spa.evaluate(() => {
      const el = document.getElementById("sidx");
      if (!el) return null;
      const cs = getComputedStyle(el);
      return { bg: cs.backgroundColor, color: cs.color, bodyBg: getComputedStyle(document.body).backgroundColor };
    });
    await evidence(eng.key + "-01-searchbar");
    if (!style) {
      addFinding({
        severity: "S2", title: eng.label + ": #sidx select not found in the search pane",
        repro: "openSearchPane(" + eng.service + ")", expected: "#sidx present", actual: "missing", evidence: [],
      });
    } else if (isLightColor(style.bg) || style.bg === "rgba(0, 0, 0, 0)") {
      addFinding({
        severity: "S2",
        title: eng.label + ": search index <select> closed box is not dark-styled",
        repro: "openSearchPane(" + eng.service + "); getComputedStyle(#sidx).backgroundColor",
        expected: "a dark background matching the VS Code theme (cf. body bg " + style.bodyBg + ")",
        actual: "backgroundColor=" + style.bg + " color=" + style.color,
        evidence: [await evidence(eng.key + "-01b-select-style")],
      });
    }

    await runDocSearch(spa, PREFIX, "hello");
    await spa.waitForFunction(() => !document.querySelector("#sresult .state.loading"), { timeout: 15000 });
    await evidence(eng.key + "-02-search-one-match");
    const oneMatch = await spa.evaluate(() => Array.from(document.querySelectorAll("#sresult .nname")).map((el) => el.textContent));
    if (oneMatch.length !== 1 || oneMatch[0] !== "uitest_plain1") {
      addFinding({
        severity: "S1",
        title: eng.label + ": search for a distinctive term did not return exactly the one matching doc",
        repro: 'search(' + eng.service + ', index=' + PREFIX + ', q="hello")',
        expected: '["uitest_plain1"]', actual: JSON.stringify(oneMatch),
        evidence: [await evidence(eng.key + "-02b-unexpected-results")],
        engine_truth: "fixture: only uitest_plain1's body contains \"hello\"",
      });
    }

    await runDocSearch(spa, PREFIX, "zzz_uitest_nomatch_zzz");
    await spa.waitForFunction(() => !document.querySelector("#sresult .state.loading"), { timeout: 15000 });
    await evidence(eng.key + "-03-search-no-match");
    const emptyState = await spa.evaluate(() => {
      const el = document.querySelector("#sresult .state.empty");
      return el ? el.textContent : null;
    });
    const anyNodes = await spa.evaluate(() => document.querySelectorAll("#sresult .node").length);
    if (!emptyState || anyNodes > 0) {
      addFinding({
        severity: "S1",
        title: eng.label + ": a no-match search does not render the honest empty state",
        repro: 'search(' + eng.service + ', index=' + PREFIX + ', q="zzz_uitest_nomatch_zzz")',
        expected: "#sresult shows .state.empty, zero .node rows",
        actual: "emptyState=" + JSON.stringify(emptyState) + " nodeRows=" + anyNodes,
        evidence: [await evidence(eng.key + "-03b-not-empty")],
      });
    }
  } catch (e) {
    addFinding({
      severity: "S1", title: eng.label + ": harness could not drive DOC-2 to completion",
      repro: "DOC-2 / " + eng.service, expected: "search pane flow completes",
      actual: String(e && e.message ? e.message : e), evidence: [],
    });
  } finally {
    await eng.teardown(engines);
  }
}

// ============================== DOC-3 ==============================
// create doc via UI: valid create (good toast + visible in UI + engine
// confirms), invalid JSON (honest client-side rejection, no engine change),
// duplicate id (I-4 collision-refusing create -- recorded either way, per
// engine, as a spec-conformance note for the fix loop).

async function runDOC3(ctx) {
  for (const eng of ENGINES) await doc3ForEngine(ctx, eng);
}

async function doc3ForEngine(ctx, eng) {
  const { engines, evidence, addFinding, harness } = ctx;
  try {
    await eng.teardown(engines);
    await eng.setup(engines);
    let spa = await enterService(ctx, eng.service, true);

    // ---- valid create ----
    const newID = "uitest_new1";
    await openSearchPane(ctx, spa, eng.service);
    await spa.select("#sidx", PREFIX);
    await spa.waitForSelector("#adddoc", { timeout: 15000 });
    await spa.click("#adddoc");
    await spa.waitForSelector("#docbody", { timeout: 15000 });
    await evidence(eng.key + "-01-create-modal");
    await spa.type("#docid", newID);
    const createBody = eng.createBody(newID);
    await spa.type("#docbody", JSON.stringify(createBody));
    await spa.click("#modalok");
    const goodToast = await harness.waitToast(spa);
    await evidence(eng.key + "-02-after-create");
    // A "warn"/timeout toast is I-1's HONEST accepted-not-confirmed outcome,
    // not a failure -- give the write a few more seconds to land before
    // judging (the server's own meili task-poll already waited up to 10s;
    // this is generous follow-up slack, not a re-litigation of that timeout).
    let engineDoc = eng.getDoc(engines, newID);
    if (!engineDoc && goodToast && goodToast.kind === "warn") {
      for (let i = 0; i < 4 && !engineDoc; i++) { await sleep(2000); engineDoc = eng.getDoc(engines, newID); }
    }
    if (goodToast && goodToast.kind === "warn") {
      if (engineDoc) {
        // honest accepted-not-confirmed that DID eventually apply -- exactly
        // the spec's contract working as designed; not a finding.
      } else {
        addFinding({
          severity: "S2",
          title: eng.label + ": document create timed out (accepted-not-confirmed) and never applied even after a follow-up wait",
          repro: "write mode ON; " + eng.service + " -> search -> Add document; id=" + newID + " body=" + JSON.stringify(createBody),
          expected: "either applies within a few seconds of the timeout, or the timeout reflects a genuine, investigable slow/stuck write",
          actual: "toast=" + JSON.stringify(goodToast) + "; engine still has no doc after +8s follow-up",
          evidence: [], engine_truth: "not found after follow-up polling",
        });
      }
    } else if (!(goodToast && goodToast.kind === "good")) {
      if (goodToast && /invalid request/i.test(goodToast.text || "")) {
        addFinding({
          id: "DOC-3-" + eng.key + "-KI-1", severity: "S1",
          title: eng.label + ": document create hit the KI-1 generic 400 (\"Invalid request.\")",
          repro: "write mode ON; " + eng.service + " -> search -> Add document; id=" + newID + " body=" + JSON.stringify(createBody),
          expected: "200 {ok:true,id:...}; toast 'Document created.'",
          actual: JSON.stringify(goodToast), evidence: [],
          engine_truth: "doc present on engine after create: " + !!engineDoc,
        });
      } else {
        addFinding({
          severity: "S1", title: eng.label + ": creating a valid new document did not report success",
          repro: "write mode ON; " + eng.service + " -> search -> Add document; id=" + newID + " body=" + JSON.stringify(createBody),
          expected: "good toast, doc visible + engine confirms", actual: JSON.stringify(goodToast),
          evidence: [], engine_truth: "doc present on engine after create: " + !!engineDoc,
        });
      }
    } else if (!engineDoc) {
      addFinding({
        id: "DOC-3-" + eng.key + "-success-lie", severity: "S1",
        title: eng.label + ": UI reported a successful document create but the engine does not have it (success-lie, I-1)",
        repro: "create " + newID + "; toast said success; engine GET",
        expected: "engine returns the document", actual: "engine GET returned nothing",
        evidence: [], engine_truth: JSON.stringify(engineDoc),
      });
    } else if (String(engineDoc.id) !== newID) {
      addFinding({
        severity: "S2", title: eng.label + ": created document's id does not round-trip",
        repro: "create " + newID, expected: "engine doc.id == " + newID,
        actual: "engine doc.id == " + JSON.stringify(engineDoc.id), evidence: [],
      });
    }
    const blobView = await readBlobView(spa);
    if (!blobView.preText && !blobView.editorText) {
      addFinding({
        severity: "S2", title: eng.label + ": newly created document did not auto-open after create",
        repro: "create " + newID, expected: "blob detail view opens showing the new doc",
        actual: JSON.stringify(blobView), evidence: [await evidence(eng.key + "-02b-no-autoopen")],
      });
    }

    // ---- invalid JSON body ----
    await drainToast(); // let the valid-create toast above fully self-remove first (Gotcha #8)
    await openSearchPane(ctx, spa, eng.service);
    await spa.select("#sidx", PREFIX);
    await spa.click("#adddoc");
    await spa.waitForSelector("#docbody", { timeout: 15000 });
    await spa.type("#docid", "uitest_shouldnotexist");
    await spa.type("#docbody", '{"broken');
    const countBefore = eng.docCount(engines);
    await spa.click("#modalok");
    // Invalid JSON is refused CLIENT-side and, per the round-2 modal contract,
    // the rejection keeps the modal OPEN with the typed body and renders
    // "Body is not valid JSON." INLINE (#modalerr) — no toast, no auto-close.
    let jsonErr = "";
    try {
      await spa.waitForSelector("#modalerr", { timeout: 8000 });
      jsonErr = await spa.evaluate(() => (document.getElementById("modalerr") || {}).textContent || "");
    } catch (_) { /* absent — recorded below */ }
    const jsonOpen = await spa.evaluate(() => !document.getElementById("modal").classList.contains("hidden"));
    await evidence(eng.key + "-03-invalid-json");
    const countAfter = eng.docCount(engines);
    if (!jsonOpen || jsonErr.indexOf("Body is not valid JSON.") < 0) {
      addFinding({
        severity: "S2", title: eng.label + ": invalid-JSON create did not keep the modal open with the inline 'Body is not valid JSON.' error",
        repro: 'Add document; body={"broken; confirm', expected: "modal stays open; #modalerr says 'Body is not valid JSON.'",
        actual: "open=" + jsonOpen + "; err=" + JSON.stringify(jsonErr), evidence: [],
      });
    }
    // Close the rejected modal (stays open by design) before the next sub-test.
    await spa.click("#modalcancel");
    await spa.waitForSelector("#modal.hidden", { timeout: 5000 }).catch(() => {});
    await sleep(200);
    if (countAfter !== countBefore) {
      addFinding({
        severity: "S1", title: eng.label + ": invalid JSON create changed the engine's document count",
        repro: "Add document with malformed JSON body",
        expected: "doc count unchanged (" + countBefore + ")", actual: String(countAfter), evidence: [],
        engine_truth: eng.label + " count before=" + countBefore + " after=" + countAfter,
      });
    }

    // ---- duplicate id (I-4: recorded either way as a spec-conformance note) ----
    await drainToast(); // let the invalid-JSON toast above fully self-remove first (Gotcha #8)
    await openSearchPane(ctx, spa, eng.service);
    await spa.select("#sidx", PREFIX);
    await spa.click("#adddoc");
    await spa.waitForSelector("#docbody", { timeout: 15000 });
    const dupID = "uitest_plain1";
    await spa.type("#docid", dupID);
    await spa.type("#docbody", JSON.stringify(eng.createBody(dupID)));
    await spa.click("#modalok");
    const dupToast = await harness.waitToast(spa);
    await evidence(eng.key + "-04-duplicate-id");
    const dupDocAfter = eng.getDoc(engines, dupID);
    // "already exists" is the action-aware create-conflict wording (dc-errors.js
    // maps ErrConflict per action); the older phrasings stay matched so the
    // detector works against pre-fix builds too.
    const dupHonestConflict = !!dupToast && dupToast.kind === "bad" && /already exists|changed since|conflict/i.test(dupToast.text || "");
    addFinding({
      severity: dupHonestConflict ? "S3" : "S1",
      title: eng.label + ": duplicate-id create " + (dupHonestConflict ? "honestly refused (conflict)" : "did NOT honestly refuse -- check for a silent clobber"),
      repro: "Add document; id=" + dupID + " (already exists); body=" + JSON.stringify(eng.createBody(dupID)),
      expected: "spec I-4: collision-refusing create (ErrConflict), never a silent clobber",
      actual: "toast=" + JSON.stringify(dupToast) + "; engine doc unchanged=" +
        (dupDocAfter && dupDocAfter.title === "Plain document"),
      evidence: [], engine_truth: JSON.stringify(dupDocAfter),
    });
  } catch (e) {
    addFinding({
      severity: "S1", title: eng.label + ": harness could not drive DOC-3 to completion",
      repro: "DOC-3 / " + eng.service, expected: "create-doc flow completes",
      actual: String(e && e.message ? e.message : e), evidence: [],
    });
  } finally {
    await eng.teardown(engines);
  }
}

// ============================== DOC-4 ==============================
// edit + delete doc: edit a field via the blob textarea, engine confirms the
// new value; delete via the SPA confirm modal, gone from UI + engine 404s it.
// KI-1: a body-addressed 400 "Invalid request." is recorded against KI-1, not
// re-filed as a fresh unknown.

async function runDOC4(ctx) {
  for (const eng of ENGINES) await doc4ForEngine(ctx, eng);
}

async function doc4ForEngine(ctx, eng) {
  const { engines, evidence, addFinding, harness } = ctx;
  try {
    await eng.teardown(engines);
    await eng.setup(engines);
    let spa = await enterService(ctx, eng.service, true);

    // ---- edit ----
    // No extra clickService here: enterService() already lands on this
    // service's hint/tree via its own selectService() call. A REDUNDANT
    // immediate re-click (two selectService() calls back to back) is exactly
    // the trigger reproduced in DOC-1/DOC-2 for the tree duplicate-render
    // race -- confirmed live: adding this call here made expandTreeNode fail
    // to find "uitest_docs" at all on all three engines.
    const editID = "uitest_plain1";
    if (!(await expandTreeNode(spa, PREFIX))) throw new Error("could not expand " + PREFIX + " for edit test");
    if (!(await clickChildNode(spa, PREFIX, editID))) throw new Error("could not open " + editID + " for edit test");
    await spa.waitForSelector("#blobedit, #saveblob, pre.blob", { timeout: 15000 }).catch(() => {});
    await evidence(eng.key + "-01-doc-opened-for-edit");
    const hasEditor = await spa.evaluate(() => !!document.getElementById("blobedit"));
    if (!hasEditor) {
      addFinding({
        severity: "S2", title: eng.label + ": document blob view is not editable with write mode on",
        repro: "write mode ON; open " + editID, expected: "#blobedit textarea + #saveblob present",
        actual: "no editor rendered", evidence: [await evidence(eng.key + "-01b-no-editor")],
      });
    } else {
      const current = await spa.evaluate(() => document.getElementById("blobedit").value);
      let obj = null;
      try { obj = JSON.parse(current); } catch (_) { /* leave null */ }
      const newCount = 4242;
      const newText = obj ? JSON.stringify(Object.assign({}, obj, { count: newCount })) : current;
      await setEditorValue(spa, "#blobedit", newText);
      await spa.click("#saveblob");
      await confirmSpaModal(spa, evidence, eng.key + "-02-save-confirm-modal");
      const saveToast = await harness.waitToast(spa);
      await evidence(eng.key + "-03-after-save");
      const engineDoc = eng.getDoc(engines, editID);
      const applied = !!(engineDoc && Number(engineDoc.count) === newCount);
      if (!(saveToast && saveToast.kind === "good")) {
        if (saveToast && /invalid request/i.test(saveToast.text || "")) {
          addFinding({
            id: "DOC-4-" + eng.key + "-edit-KI-1", severity: "S1",
            title: eng.label + ": document edit hit the KI-1 generic 400 (\"Invalid request.\")",
            repro: "write mode ON; open " + editID + "; edit count -> " + newCount + "; Save",
            expected: "200 {ok:true}; toast 'Saved.'", actual: JSON.stringify(saveToast),
            evidence: [], engine_truth: JSON.stringify(engineDoc),
          });
        } else {
          addFinding({
            severity: "S1", title: eng.label + ": editing a document field did not report success",
            repro: "write mode ON; open " + editID + "; edit count -> " + newCount + "; Save",
            expected: "good toast", actual: JSON.stringify(saveToast),
            evidence: [], engine_truth: JSON.stringify(engineDoc),
          });
        }
      } else if (!applied) {
        addFinding({
          id: "DOC-4-" + eng.key + "-edit-success-lie", severity: "S1",
          title: eng.label + ": UI reported a successful edit but the engine still shows the old value (success-lie, I-1)",
          repro: "edit " + editID + ".count -> " + newCount + "; toast said success; engine GET",
          expected: "engine count == " + newCount, actual: JSON.stringify(engineDoc),
          evidence: [], engine_truth: JSON.stringify(engineDoc),
        });
      }
    }

    // ---- delete ----
    await drainToast(); // let the edit-save toast above fully self-remove first (Gotcha #8)
    await harness.clickService(spa, eng.service);
    await expandTreeNode(spa, PREFIX);
    const deleteID = "uitest_nest1";
    if (!(await clickChildNode(spa, PREFIX, deleteID))) throw new Error("could not open " + deleteID + " for delete test");
    await spa.waitForSelector("#delblob", { timeout: 15000 });
    await evidence(eng.key + "-04-doc-opened-for-delete");
    await spa.click("#delblob");
    await confirmSpaModal(spa, evidence, eng.key + "-05-delete-confirm-modal");
    const delToast = await harness.waitToast(spa);
    await sleep(150);
    await evidence(eng.key + "-06-after-delete");
    const engineAfterDelete = eng.getDoc(engines, deleteID);
    const engineStillHas = !!engineAfterDelete;
    const stillInTree = ((await childNodeNamesOf(spa, PREFIX)) || []).indexOf(deleteID) >= 0;
    if (!(delToast && delToast.kind === "good")) {
      if (delToast && /invalid request/i.test(delToast.text || "")) {
        addFinding({
          id: "DOC-4-" + eng.key + "-delete-KI-1", severity: "S1",
          title: eng.label + ": document delete hit the KI-1 generic 400 (\"Invalid request.\")",
          repro: "write mode ON; open " + deleteID + "; Delete; confirm",
          expected: "200 {ok:true}; toast 'Deleted.'", actual: JSON.stringify(delToast),
          evidence: [], engine_truth: "still on engine: " + engineStillHas,
        });
      } else {
        addFinding({
          severity: "S1", title: eng.label + ": deleting a document did not report success",
          repro: "write mode ON; open " + deleteID + "; Delete; confirm",
          expected: "good toast 'Deleted.'", actual: JSON.stringify(delToast),
          evidence: [], engine_truth: "still on engine: " + engineStillHas,
        });
      }
    } else if (engineStillHas) {
      addFinding({
        id: "DOC-4-" + eng.key + "-delete-success-lie", severity: "S1",
        title: eng.label + ": UI reported a successful delete but the document still EXISTS on the engine (success-lie, I-1)",
        repro: eng.label + " GET " + deleteID + " right after a UI delete + good toast",
        expected: "not found", actual: JSON.stringify(engineAfterDelete),
        evidence: [], engine_truth: JSON.stringify(engineAfterDelete),
      });
    } else if (stillInTree) {
      addFinding({
        severity: "S2", title: eng.label + ": deleted document still visible in the tree after a genuinely successful delete",
        repro: "delete " + deleteID + "; toast said success; engine confirms gone; re-check tree",
        expected: "absent from tree", actual: "still present",
        evidence: [await evidence(eng.key + "-06b-stale-tree-entry")],
      });
    }
  } catch (e) {
    addFinding({
      severity: "S1", title: eng.label + ": harness could not drive DOC-4 to completion",
      repro: "DOC-4 / " + eng.service, expected: "edit+delete flow completes",
      actual: String(e && e.message ? e.message : e), evidence: [],
    });
  } finally {
    await eng.teardown(engines);
  }
}

// ============================== DOC-5 ==============================
// XSS honesty: the xss doc opened via the tree AND via a search result must
// never execute, never inject a live <img>, and must show its markup as
// visible escaped TEXT wherever it renders.

async function runDOC5(ctx) {
  for (const eng of ENGINES) await doc5ForEngine(ctx, eng);
}

async function doc5ForEngine(ctx, eng) {
  const { engines, evidence, addFinding, harness } = ctx;
  try {
    await eng.teardown(engines);
    await eng.setup(engines);
    let spa = await enterService(ctx, eng.service, false);

    const preFlag = await spa.evaluate(() => window.__uitest_xss);
    if (preFlag !== undefined) {
      addFinding({
        severity: "S1", title: eng.label + ": window.__uitest_xss already set before opening the XSS doc (contamination)",
        repro: "check window.__uitest_xss before opening uitest_xss1",
        expected: "undefined", actual: String(preFlag), evidence: [],
      });
    }

    // 1) open directly via the tree (no extra clickService -- see DOC-4's note
    // on the duplicate-render race a redundant re-selection triggers)
    await expandTreeNode(spa, PREFIX);
    const openedTree = await clickChildNode(spa, PREFIX, "uitest_xss1");
    if (!openedTree) throw new Error("could not open uitest_xss1 via the tree");
    await spa.waitForSelector("pre.blob, #blobedit", { timeout: 15000 }).catch(() => {});
    await evidence(eng.key + "-01-xss-via-tree");
    await assertXSSSafe(evidence, addFinding, spa, eng, "tree-open", "the xss doc was");

    // 2) open via a search result
    await openSearchPane(ctx, spa, eng.service);
    await spa.select("#sidx", PREFIX);
    await runDocSearch(spa, PREFIX, "onerror");
    await spa.waitForFunction(() => !document.querySelector("#sresult .state.loading"), { timeout: 15000 });
    await evidence(eng.key + "-02-xss-search-results");
    const resultIds = await spa.evaluate(() => Array.from(document.querySelectorAll("#sresult .nname")).map((el) => el.textContent));
    if (resultIds.indexOf("uitest_xss1") < 0) {
      addFinding({
        severity: "S3", title: eng.label + ": search for \"onerror\" did not surface the XSS fixture doc",
        repro: 'search(' + eng.service + ', ' + PREFIX + ', "onerror")',
        expected: "uitest_xss1 present in results", actual: JSON.stringify(resultIds), evidence: [],
      });
    } else {
      const openedSearch = await clickSearchResult(spa, "uitest_xss1");
      if (!openedSearch) throw new Error("could not click uitest_xss1 in search results");
      await spa.waitForSelector("pre.blob, #blobedit", { timeout: 15000 }).catch(() => {});
      await evidence(eng.key + "-03-xss-via-search");
      await assertXSSSafe(evidence, addFinding, spa, eng, "search-open", "the xss doc was");
    }
  } catch (e) {
    addFinding({
      severity: "S1", title: eng.label + ": harness could not drive DOC-5 to completion",
      repro: "DOC-5 / " + eng.service, expected: "XSS-honesty flow completes",
      actual: String(e && e.message ? e.message : e), evidence: [],
    });
  } finally {
    await eng.teardown(engines);
  }
}

// ============================== DOC-6 ==============================
// qdrant view-only + vector collapse: points render with a collapsed vector
// summary (raw floats only after toggling), payloads render (XSS-safe too),
// and no write control is ever ENABLED -- default read-only AND after
// globally enabling write mode (toggled via es, then hop to vectors).

async function runDOC6(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  try {
    qdTeardown(engines);
    qdSetup(engines);
    let spa = await enterService(ctx, VECTORS_SERVICE, false);

    // no extra clickService -- see DOC-4's note on the duplicate-render race
    if (!(await expandTreeNode(spa, VEC))) throw new Error("could not expand " + VEC);
    await evidence("01-vec-expanded");

    if (!(await clickChildNode(spa, VEC, "101"))) throw new Error("could not open point 101");
    await spa.waitForSelector(".vectorbox, pre.blob", { timeout: 15000 }).catch(() => {});
    await evidence("02-point-101-view");
    const v1 = await readVectorView(spa);
    if (!v1.hasVectorBox) {
      addFinding({
        severity: "S1", title: "qdrant: point view does not render the collapsed-vector summary (.vectorbox)",
        repro: "open point 101 in " + VEC,
        expected: ".vectorbox present with .vecsummary + collapsed pre.blob.vecraw",
        actual: "no .vectorbox found", evidence: [await evidence("02b-no-vectorbox")],
      });
    } else {
      if (v1.rawHiddenInitially !== true) {
        addFinding({
          severity: "S2", title: "qdrant: raw vector floats are not collapsed by default",
          repro: "open point 101", expected: "pre.blob.vecraw carries .hidden until toggled",
          actual: "rawHiddenInitially=" + v1.rawHiddenInitially, evidence: [await evidence("02c-raw-not-collapsed")],
        });
      }
      await spa.click(".vecsummary button.link");
      await sleep(150);
      const v1b = await readVectorView(spa);
      await evidence("03-point-101-raw-toggled");
      if (v1b.rawHiddenInitially !== false || !v1b.rawText || v1b.rawText.indexOf("0.1") < 0) {
        addFinding({
          severity: "S2", title: "qdrant: toggling \"Show raw vector\" did not reveal the raw floats",
          repro: "click the vector toggle on point 101", expected: "raw floats visible, includes 0.1",
          actual: JSON.stringify(v1b), evidence: [],
        });
      }
      if (v1.hasSave || v1.hasDelete || v1.hasRename) {
        addFinding({
          severity: "S1", title: "qdrant: a write affordance rendered on a point view with write mode OFF",
          repro: "open point 101 (write mode off)", expected: "no save/delete/rename affordances",
          actual: JSON.stringify({ hasSave: v1.hasSave, hasDelete: v1.hasDelete, hasRename: v1.hasRename }),
          evidence: [await evidence("02d-write-affordance-while-off")],
        });
      }
    }

    // XSS-payload point
    await harness.clickService(spa, VECTORS_SERVICE);
    await expandTreeNode(spa, VEC);
    if (!(await clickChildNode(spa, VEC, "103"))) throw new Error("could not open XSS point 103");
    await spa.waitForSelector(".vectorbox, pre.blob", { timeout: 15000 }).catch(() => {});
    await evidence("04-xss-point-view");
    const flagAfterPoint = await spa.evaluate(() => window.__uitest_xss);
    if (flagAfterPoint !== undefined) {
      addFinding({
        id: "DOC-6-vectors-xss-executed", severity: "S1",
        title: "qdrant: XSS payload in a point's payload EXECUTED (window.__uitest_xss=" + flagAfterPoint + ")",
        repro: "open point 103 (payload carries the XSS fixture)",
        expected: "window.__uitest_xss stays undefined", actual: String(flagAfterPoint),
        evidence: [await evidence("04b-xss-executed")],
      });
    }
    const v3 = await readVectorView(spa);
    if (!(v3.payloadText && v3.payloadText.indexOf("<script>") >= 0)) {
      addFinding({
        severity: "S2", title: "qdrant: XSS payload text not visibly rendered as escaped text in the point's payload pane",
        repro: "open point 103", expected: "payload JSON shows literal '<script>...' as text",
        actual: JSON.stringify(v3.payloadText), evidence: [],
      });
    }

    // default (write mode OFF): no create affordance on the hint either
    await harness.clickService(spa, VECTORS_SERVICE);
    await sleep(150);
    const hintAffordances = await spa.evaluate(() => ({
      hasAddDoc: !!document.getElementById("adddoc"), hasCreateKey: !!document.getElementById("createkeylink"),
    }));
    await evidence("05-hint-no-write-affordances");
    if (hintAffordances.hasAddDoc || hintAffordances.hasCreateKey) {
      addFinding({
        severity: "S1", title: "qdrant: a create affordance renders on a view-only service",
        repro: "selectService(vectors), write mode off", expected: "no create affordances",
        actual: JSON.stringify(hintAffordances), evidence: [],
      });
    }
    // read affordance offered despite no searcher capability -- UX polish note,
    // not a write-safety issue (worth surfacing since DOC-6 is already here).
    const searchLinkPresent = await spa.evaluate(() => !!document.getElementById("searchlink"));
    if (searchLinkPresent) {
      addFinding({
        severity: "S3",
        title: "qdrant: \"Search ▸\" link is offered though the qdrant engine has no searcher (server 422s ErrUnsupported if used)",
        repro: "selectService(vectors) -> hint shows #searchlink",
        expected: "either the link is withheld for engines with no searcher capability, or this is accepted as an honest-error-on-click case",
        actual: "#searchlink present for qdrant", evidence: [await evidence("05b-search-link-present")],
      });
    }

    // write-mode ON globally (toggled via es) -- vectors must still sprout
    // nothing MUTABLE, even if a control renders.
    const esSpa = await enterService(ctx, ES_SERVICE, true);
    let vecSpa = await harness.sidebarBrowse(page, VECTORS_SERVICE);
    await waitForActiveService(vecSpa, VECTORS_SERVICE);
    await sleep(200);
    vecSpa = await harness.spaFrame(page);
    await harness.clickService(vecSpa, VECTORS_SERVICE);
    await expandTreeNode(vecSpa, VEC);
    await clickChildNode(vecSpa, VEC, "101");
    await vecSpa.waitForSelector(".vectorbox, pre.blob", { timeout: 15000 }).catch(() => {});
    await evidence("06-point-101-writemode-on");
    const v1writeOn = await readVectorView(vecSpa);
    const anyRendered = v1writeOn.hasSave || v1writeOn.hasDelete || v1writeOn.hasRename;
    const deleteEnabled = await vecSpa.evaluate(() => {
      const del = document.getElementById("delblob");
      return !!(del && !del.disabled);
    });
    if (deleteEnabled) {
      addFinding({
        severity: "S1", title: "qdrant: a write control is ENABLED (not just rendered) on a view-only point with write mode on",
        repro: "write mode ON globally; open point 101 on vectors",
        expected: "no enabled write control", actual: "delblob.disabled=false",
        evidence: [await evidence("06b-enabled-control")],
      });
    } else if (anyRendered) {
      addFinding({
        severity: "S3",
        title: "qdrant: a write control renders (disabled) on a view-only point once write mode is globally on, rather than being absent",
        repro: "write mode ON globally (toggled on es); open point 101 on vectors",
        expected: "brief's stated design goal: zero write affordances anywhere, absent not disabled",
        actual: JSON.stringify({ hasSave: v1writeOn.hasSave, hasDelete: v1writeOn.hasDelete, hasRename: v1writeOn.hasRename }) +
          " -- server-gated disabled (enabled=false, reason=\"service is view-only\"); Delete never actually fires: " +
          "Provider.Delete/WriteBlob/CreateDoc unconditionally return ErrReadOnly for qdrant regardless of any client " +
          "action, so this is not a security gap. It matches the SAME disabled-with-reason pattern used for clickhouse " +
          "(tabular, view-only) -- looks like the app's deliberate, consistent U-06 convention (presence:hasAction, " +
          "mutability:actionEnabled) rather than a document-family-specific bug. Flagging as a design-consistency " +
          "question against this scenario's stated expectation, for the fix loop to adjudicate -- not a security issue.",
        evidence: [await evidence("06c-disabled-control-rendered")],
      });
    }
    await harness.setWriteMode(page, esSpa, false); // leave the container's ambient write posture off
  } catch (e) {
    addFinding({
      severity: "S1", title: "qdrant: harness could not drive DOC-6 to completion",
      repro: "DOC-6 / vectors", expected: "view-only + vector-collapse flow completes",
      actual: String(e && e.message ? e.message : e), evidence: [],
    });
  } finally {
    qdTeardown(engines);
  }
}

runner.register({ id: "DOC-1", family: "document", fn: runDOC1 });
runner.register({ id: "DOC-2", family: "document", fn: runDOC2 });
runner.register({ id: "DOC-3", family: "document", fn: runDOC3 });
runner.register({ id: "DOC-4", family: "document", fn: runDOC4 });
runner.register({ id: "DOC-5", family: "document", fn: runDOC5 });
runner.register({ id: "DOC-6", family: "document", fn: runDOC6 });

module.exports = { ENGINES, PREFIX, VEC, FIXTURE_DOCS, FIXTURE_IDS, VEC_POINTS, XSS_TITLE, XSS_BODY, sleep };
