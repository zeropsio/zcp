#!/usr/bin/env node
"use strict";

// gallery.js — A5's UX state-gallery sweep. Captures a COMPLETE, captioned
// screenshot gallery of the REAL deployed Managed Data Console UI (every
// family x every state) as camera-only input for two independent UX rubric
// critics. This is capture, not pass/fail: it uses lib/harness.js +
// lib/engines.js directly, NOT lib/runner.js's scenario registry (no
// addFinding, no PASS/FAIL verdicts).
//
// Output: evidence/GALLERY/<NN>-<slug>.png (auto-numbered by harness.shot in
// true capture order, shared counter across every call below) + a captioned
// evidence/GALLERY/index.md.
//
// Fixtures: every seeded row/key/object/doc/point is uitest_gal_*-prefixed,
// seeded lazily per section and torn down once at the very end (+ a self-heal
// teardown at the top, in case a prior run aborted mid-sweep) — mirrors the
// data discipline every scenarios/*.js file already follows.

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { execFileSync } = require("child_process");
const harness = require("./lib/harness");
const engines = require("./lib/engines");
const { loadConfig } = require("./lib/config");

const ROOT = __dirname;
const EVIDENCE_DIR = path.join(ROOT, "evidence", "GALLERY");
const SCENARIO_ID = "GALLERY";

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
function cfg() {
  return loadConfig();
}

// ============================================================================
// index.md bookkeeping — every explicit shot() call below is captioned; any
// shot a harness HELPER takes internally (the native write-mode confirm
// modal, a FAIL-* evidence shot) is not captioned at the call site but still
// shares the same evidence/GALLERY/ counter, so finalizeIndex() backfills a
// generic caption for anything on disk that wasn't explicitly recorded —
// guaranteeing the index lists every file, in true numeric capture order.
// ============================================================================

const INDEX = []; // {file, surface, state, note}
const SKIPPED = []; // {state, why}

async function shot(pageObj, slug, surface, state, note) {
  const file = await harness.shot(pageObj, SCENARIO_ID, slug);
  INDEX.push({ file, surface, state, note: note || "" });
  console.log("  [" + INDEX.length + "+] " + file);
  return file;
}

function parseNN(file) {
  const m = /^(\d+)-/.exec(path.basename(file));
  return m ? parseInt(m[1], 10) : 0;
}

function finalizeIndex() {
  const onDisk = fs.existsSync(EVIDENCE_DIR) ? fs.readdirSync(EVIDENCE_DIR).filter((f) => f.endsWith(".png")) : [];
  const covered = new Set(INDEX.map((e) => path.basename(e.file)));
  for (const f of onDisk) {
    if (covered.has(f)) continue;
    const slug = f.replace(/^\d+-/, "").replace(/\.png$/, "");
    INDEX.push({
      file: path.relative(ROOT, path.join(EVIDENCE_DIR, f)),
      surface: "(internal)",
      state: slug,
      note:
        "Auto-captured by a harness helper mid-flow (e.g. the native VS Code write-mode confirm modal, or a FAIL-* " +
        "evidence shot), not an explicit gallery shot() call — filename is the only caption source.",
    });
  }
  INDEX.sort((a, b) => parseNN(a.file) - parseNN(b.file));
}

function mdEscape(s) {
  return String(s == null ? "" : s)
    .replace(/\|/g, "\\|")
    .replace(/\n/g, " ");
}

function writeIndexMd() {
  const lines = [];
  lines.push("# Data Console UX gallery — capture index");
  lines.push("");
  lines.push(
    "Captured " + new Date().toISOString() + " against the live deployed container (`" + (cfg().DC_URL || "") +
      "`). " + INDEX.length + " shots total. This file is a camera's log: it states what each shot shows, not " +
      "whether the state is good or bad — that judgment belongs to the two UX rubric critics reviewing this gallery."
  );
  lines.push("");
  if (SKIPPED.length) {
    lines.push("## Not captured");
    lines.push("");
    for (const s of SKIPPED) lines.push("- **" + s.state + "** — " + s.why);
    lines.push("");
  }
  lines.push("## Shots (capture order)");
  lines.push("");
  lines.push("| # | File | Surface | State | Reviewer note |");
  lines.push("|---|------|---------|-------|----------------|");
  for (const e of INDEX) {
    lines.push(
      "| " + parseNN(e.file) + " | `" + e.file + "` | " + mdEscape(e.surface) + " | " + mdEscape(e.state) + " | " +
        mdEscape(e.note) + " |"
    );
  }
  lines.push("");
  fs.mkdirSync(EVIDENCE_DIR, { recursive: true });
  fs.writeFileSync(path.join(EVIDENCE_DIR, "index.md"), lines.join("\n") + "\n");
}

// ============================================================================
// generic tree helpers (local copies — every scenarios/*.js file keeps its
// own per the fan-out convention; lib/harness.js is out of bounds to edit).
// Glyph semantics verified live against console/webui/dist/app.js: a
// container's .kind glyph is EXACTLY "▸" while collapsed and flips to "▾" once
// expanded (glyphFor()/expandContainer()) — every other kind (tabular "▦",
// blob "◇", or a KV per-type glyph) is a true leaf. Matching on the exact
// glyph (not "kind != container") avoids the toggle-loop bug an expanded-vs-
// unexpanded-container mixup would cause (clicking an already-expanded
// container just re-collapses it).
// ============================================================================

async function waitTreeSettled(frame, timeoutMs) {
  const deadline = Date.now() + (timeoutMs || 4000);
  let last = -1;
  let streak = 0;
  while (Date.now() < deadline) {
    const n = await frame.evaluate(() => document.querySelectorAll("#tree .node").length).catch(() => 0);
    if (n > 0 && n === last) {
      streak++;
      if (streak >= 2) return;
    } else {
      streak = 0;
    }
    last = n;
    await sleep(150);
  }
}

async function waitForGrid(frame, ms) {
  await frame.waitForSelector("table.grid", { timeout: ms || 15000 }).catch(() => {});
}

// revealAndClick expands containers breadth-first until a node named `name`
// is visible, then clicks it (mirrors tabular.js's revealAndClickNode).
async function revealAndClick(frame, name, tries) {
  tries = tries || 25;
  for (let i = 0; i < tries; i++) {
    const res = await frame.evaluate((n) => {
      const rows = Array.from(document.querySelectorAll("#tree .node"));
      const target = rows.find((r) => {
        const nm = r.querySelector(".nname");
        return nm && nm.textContent === n;
      });
      if (target) {
        target.click();
        return "clicked";
      }
      const glyphOf = (r) => {
        const k = r.querySelector(".kind");
        return k ? k.textContent : "";
      };
      const collapsed = rows.find((r) => glyphOf(r) === "▸");
      if (collapsed) {
        collapsed.click();
        return "expanding";
      }
      return "stuck";
    }, name);
    if (res === "clicked") return true;
    await sleep(350);
  }
  return false;
}

// openFirstLeaf opens whatever the first true leaf under the current root is,
// expanding at most one container-chain deep at a time — used for the
// per-service "default view" shots where the specific node name is unknown/
// irrelevant, just "the first thing there is to look at".
async function openFirstLeaf(frame, tries) {
  tries = tries || 15;
  for (let i = 0; i < tries; i++) {
    const res = await frame.evaluate(() => {
      const rows = Array.from(document.querySelectorAll("#tree .node"));
      const glyphOf = (r) => {
        const k = r.querySelector(".kind");
        return k ? k.textContent : "";
      };
      const leaf = rows.find((r) => glyphOf(r) !== "▸" && glyphOf(r) !== "▾");
      if (leaf) {
        leaf.click();
        return "clicked";
      }
      const collapsed = rows.find((r) => glyphOf(r) === "▸");
      if (collapsed) {
        collapsed.click();
        return "expanding";
      }
      return "empty";
    });
    if (res === "clicked") return true;
    if (res === "empty") return false;
    await sleep(400);
  }
  return false;
}

// ============================================================================
// SQL/KV fixtures (postgres via engines.psql, valkey via engines.redis — both
// already shipped by lib/engines.js).
// ============================================================================

const TAB = "uitest_gal_tab";
const EMPTY_TAB = "uitest_gal_empty";
const WIDE_TAB = "uitest_gal_wide";

function seedTabular() {
  engines.psql("DROP TABLE IF EXISTS " + TAB);
  engines.psql(
    "CREATE TABLE " + TAB + " (id serial PRIMARY KEY, txt text NOT NULL, num numeric(10,2), flag boolean, notes text)"
  );
  engines.psql(
    "INSERT INTO " + TAB + " (txt, num, flag, notes) VALUES " +
      "('first row', 12.50, true, 'sample note')," +
      "('second row', 7.25, false, 'another note')," +
      "('third row', 100.00, true, NULL)"
  );
  engines.psql("DROP TABLE IF EXISTS " + EMPTY_TAB);
  engines.psql("CREATE TABLE " + EMPTY_TAB + " (id serial PRIMARY KEY, txt text)");
  engines.psql("DROP TABLE IF EXISTS " + WIDE_TAB);
  const longText = "gallery-long-value-" + "x".repeat(400);
  engines.psql(
    "CREATE TABLE " + WIDE_TAB +
      " (id serial PRIMARY KEY, col_a text, col_b text, col_c text, col_d text, col_e text, col_f text, col_g text, col_h text)"
  );
  engines.psql(
    "INSERT INTO " + WIDE_TAB + " (col_a,col_b,col_c,col_d,col_e,col_f,col_g,col_h) VALUES " +
      "('" + longText + "', 'bravo', 'charlie', 'delta', 'echo', 'foxtrot', 'golf', 'hotel')," +
      "('row two', 'bravo2', 'charlie2', 'delta2', 'echo2', 'foxtrot2', 'golf2', 'hotel2')"
  );
}
function teardownTabular() {
  for (const t of [TAB, EMPTY_TAB, WIDE_TAB]) {
    try {
      engines.psql("DROP TABLE IF EXISTS " + t);
    } catch (_) {
      /* best effort */
    }
  }
}

const KV_STR = "uitest_gal_kv_string";
const KV_HASH = "uitest_gal_kv_hash";
const KV_LIST = "uitest_gal_kv_list";
const KV_SET = "uitest_gal_kv_set";
const KV_ZSET = "uitest_gal_kv_zset";

function seedKV() {
  engines.redis(["SET", KV_STR, "gallery sample string value"]);
  engines.redis(["HSET", KV_HASH, "field_a", "value_a", "field_b", "value_b"]);
  engines.redis(["RPUSH", KV_LIST, "item1", "item2", "item3"]);
  engines.redis(["SADD", KV_SET, "member1", "member2"]);
  engines.redis(["ZADD", KV_ZSET, "1", "low", "5", "mid", "9", "high"]);
}
function teardownKV() {
  for (const k of [KV_STR, KV_HASH, KV_LIST, KV_SET, KV_ZSET]) {
    try {
      engines.redis(["DEL", k]);
    } catch (_) {
      /* best effort */
    }
  }
}

// ============================================================================
// Object storage fixtures — hand-rolled AWS SigV4 (MinIO-compatible
// path-style), no deps, mirroring kv-object.js's proven-live signer (trimmed
// to PUT/DELETE only — this script doesn't need LIST). Duplicated here
// deliberately: kv-object.js exports nothing (module.exports = {}) and the
// established convention is every scenario file stays self-contained rather
// than cross-importing (see uitest/README.md).
// ============================================================================

function sha256hex(buf) {
  return crypto.createHash("sha256").update(buf).digest("hex");
}
function hmac(key, data) {
  return crypto.createHmac("sha256", key).update(data, "utf8").digest();
}
function s3SignedRequest(method, key, opts) {
  opts = opts || {};
  const c = cfg();
  const region = "us-east-1";
  const service = "s3";
  const endpoint = new URL(c.DC_S3_ENDPOINT);
  const now = new Date();
  const amzDate = now.toISOString().replace(/[:-]/g, "").replace(/\.\d{3}Z$/, "Z");
  const dateStamp = amzDate.slice(0, 8);
  const body = opts.body || Buffer.alloc(0);
  const payloadHash = sha256hex(body);
  const encodedKey = key ? key.split("/").map(encodeURIComponent).join("/") : "";
  const canonicalURI = "/" + encodeURIComponent(c.DC_S3_BUCKET) + (encodedKey ? "/" + encodedKey : "");
  const headers = { host: endpoint.host, "x-amz-content-sha256": payloadHash, "x-amz-date": amzDate };
  if (opts.contentType) headers["content-type"] = opts.contentType;
  const sortedKeys = Object.keys(headers).sort();
  const canonicalHeaders = sortedKeys.map((k) => k + ":" + headers[k] + "\n").join("");
  const signedHeaders = sortedKeys.join(";");
  const canonicalRequest = [method, canonicalURI, "", canonicalHeaders, signedHeaders, payloadHash].join("\n");
  const credentialScope = dateStamp + "/" + region + "/" + service + "/aws4_request";
  const stringToSign = ["AWS4-HMAC-SHA256", amzDate, credentialScope, sha256hex(Buffer.from(canonicalRequest))].join("\n");
  const kDate = hmac("AWS4" + c.DC_S3_SECRET_KEY, dateStamp);
  const kRegion = hmac(kDate, region);
  const kService = hmac(kRegion, service);
  const kSigning = hmac(kService, "aws4_request");
  const signature = crypto.createHmac("sha256", kSigning).update(stringToSign).digest("hex");
  headers.authorization =
    "AWS4-HMAC-SHA256 Credential=" + c.DC_S3_ACCESS_KEY + "/" + credentialScope + ", SignedHeaders=" + signedHeaders +
    ", Signature=" + signature;
  const url = endpoint.origin + canonicalURI;
  return { url, headers, body };
}
async function s3(method, key, opts) {
  const { url, headers, body } = s3SignedRequest(method, key, opts || {});
  const hasBody = !(method === "GET" || method === "DELETE" || method === "HEAD");
  return fetch(url, { method, headers, body: hasBody ? body : undefined });
}

const OBJ_PREFIX = "uitest_gal/";
const TINY_PNG_B64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=";

async function seedObject() {
  await s3("PUT", OBJ_PREFIX + "readme.txt", { body: Buffer.from("hello from the gallery sweep\n"), contentType: "text/plain" });
  await s3("PUT", OBJ_PREFIX + "pic.png", { body: Buffer.from(TINY_PNG_B64, "base64"), contentType: "image/png" });
  await s3("PUT", OBJ_PREFIX + "data.bin", {
    body: Buffer.from([0, 1, 2, 3, 255, 254, 253, 0, 10, 13]),
    contentType: "application/octet-stream",
  });
  await s3("PUT", OBJ_PREFIX + "sub/nested.txt", { body: Buffer.from("nested gallery file\n"), contentType: "text/plain" });
}
async function teardownObject() {
  for (const k of ["readme.txt", "pic.png", "data.bin", "sub/nested.txt"]) {
    try {
      await s3("DELETE", OBJ_PREFIX + k);
    } catch (_) {
      /* best effort */
    }
  }
}

// ============================================================================
// Document-family fixtures — elasticsearch + typesense (curl-over-SSH, since
// these hostnames only resolve inside the container's network) + qdrant.
// ============================================================================

function httpJSON(authArgs, method, url, bodyObj) {
  let cmd = "curl -s -X " + method + " " + authArgs + " " + url;
  if (bodyObj !== undefined) {
    cmd += " -H " + engines.shellQuote("Content-Type: application/json") + " --data-binary " + engines.shellQuote(JSON.stringify(bodyObj));
  }
  const out = engines.container(cmd);
  if (!out) return null;
  try {
    return JSON.parse(out);
  } catch (_) {
    return out;
  }
}
function esUrl(p) {
  const c = cfg();
  return "http://" + c.DC_ES_HOST + ":" + c.DC_ES_PORT + p;
}
function tsUrl(p) {
  const c = cfg();
  return "http://" + c.DC_TYPESENSE_HOST + ":" + c.DC_TYPESENSE_PORT + p;
}
function qdUrl(p) {
  const c = cfg();
  return "http://" + c.DC_QDRANT_HOST + ":" + c.DC_QDRANT_PORT + p;
}
function esRequest(method, p, body) {
  const c = cfg();
  return httpJSON("-u " + engines.shellQuote(c.DC_ES_USER + ":" + c.DC_ES_PASSWORD), method, esUrl(p), body);
}
function tsRequest(method, p, body) {
  const c = cfg();
  return httpJSON("-H " + engines.shellQuote("X-TYPESENSE-API-KEY: " + c.DC_TYPESENSE_KEY), method, tsUrl(p), body);
}
function qdRequest(method, p, body) {
  const c = cfg();
  return httpJSON("-H " + engines.shellQuote("api-key: " + c.DC_QDRANT_KEY), method, qdUrl(p), body);
}

const ES_INDEX = "uitest_gal_es";
const ES_DOCS = [
  { id: "gal_doc1", title: "Gallery sample one", body: "gallery capture fixture alpha" },
  { id: "gal_doc2", title: "Gallery sample two", body: "gallery capture fixture beta" },
  { id: "gal_doc3", title: "Gallery sample three", body: "gallery capture fixture gamma searchterm" },
];
function seedES() {
  try {
    esRequest("DELETE", "/" + ES_INDEX);
  } catch (_) {
    /* best effort */
  }
  for (const d of ES_DOCS) esRequest("PUT", "/" + ES_INDEX + "/_doc/" + d.id, d);
  esRequest("POST", "/" + ES_INDEX + "/_refresh");
}
function teardownES() {
  try {
    esRequest("DELETE", "/" + ES_INDEX);
  } catch (_) {
    /* best effort */
  }
}

const TS_COLLECTION = "uitest_gal_docs";
const TS_DOCS = [
  { id: "gal_doc1", title: "Gallery sample one", body: "gallery capture fixture alpha" },
  { id: "gal_doc2", title: "Gallery sample two", body: "gallery capture fixture beta" },
];
function seedTS() {
  try {
    tsRequest("DELETE", "/collections/" + TS_COLLECTION);
  } catch (_) {
    /* best effort */
  }
  tsRequest("POST", "/collections", { name: TS_COLLECTION, fields: [{ name: ".*", type: "auto" }], enable_nested_fields: true });
  for (const d of TS_DOCS) tsRequest("POST", "/collections/" + TS_COLLECTION + "/documents", d);
}
function teardownTS() {
  try {
    tsRequest("DELETE", "/collections/" + TS_COLLECTION);
  } catch (_) {
    /* best effort */
  }
}

const VEC_COLLECTION = "uitest_gal_vec";
const VEC_POINTS = [
  { id: 1, vector: [0.1, 0.2, 0.3, 0.4], payload: { name: "gallery point alpha" } },
  { id: 2, vector: [0.9, 0.8, 0.7, 0.6], payload: { name: "gallery point beta" } },
];
function seedQdrant() {
  try {
    qdRequest("DELETE", "/collections/" + VEC_COLLECTION);
  } catch (_) {
    /* best effort */
  }
  qdRequest("PUT", "/collections/" + VEC_COLLECTION, { vectors: { size: 4, distance: "Cosine" } });
  qdRequest("PUT", "/collections/" + VEC_COLLECTION + "/points?wait=true", { points: VEC_POINTS });
}
function teardownQdrant() {
  try {
    qdRequest("DELETE", "/collections/" + VEC_COLLECTION);
  } catch (_) {
    /* best effort */
  }
}

async function teardownAllFixtures() {
  teardownTabular();
  teardownKV();
  try {
    await teardownObject();
  } catch (_) {
    /* best effort */
  }
  teardownES();
  teardownTS();
  teardownQdrant();
}

// ============================================================================
// section 1 — workbench + sidebar
// ============================================================================

async function section1(page) {
  console.log("== section 1: workbench + sidebar ==");
  await harness.openSidebar(page);
  await sleep(300);
  await shot(
    page,
    "sidebar-managed-data",
    "sidebar (Managed Data activity-bar view)",
    "all 11 service rows, freshly opened",
    "Every managed service row with its family/tier badge — check row order, label wording, and badge consistency across families."
  );

  const spa = await harness.openConsole(page, "db");
  await harness.setWriteMode(page, spa, false); // self-heal ambient state before the baseline "freshly opened" shot
  await shot(
    page,
    "console-panel-hint-db",
    "Data Console panel (db)",
    "freshly opened, no tree node selected, write mode off",
    "#content shows the placeholder hint only — no table/grid/blob opened yet."
  );
  return spa;
}

// ============================================================================
// section 2 — per-service default views, write mode OFF throughout
// ============================================================================

const ALL_SERVICES = ["db", "mariadb", "ch", "cache", "storage", "es", "docs", "search", "vectors", "queue", "events"];
const FAMILY_LABEL = {
  db: "tabular (postgres, full)",
  mariadb: "tabular (mariadb, full)",
  ch: "tabular (clickhouse, view-only)",
  cache: "kv (valkey, full)",
  storage: "object (S3-compatible, full)",
  es: "document (elasticsearch, full)",
  docs: "document (typesense, full)",
  search: "document (meilisearch, full)",
  vectors: "document/vector (qdrant, view-only)",
  queue: "stream (nats, view-only)",
  events: "stream (kafka, view-only)",
};

async function defaultShot(page, spa, svc) {
  const opened = await openFirstLeaf(spa, 15);
  await sleep(350);
  await shot(
    page,
    "default-" + svc,
    svc + " (" + FAMILY_LABEL[svc] + ")",
    opened ? "first tree item opened, write mode off" : "empty tree, write mode off",
    opened
      ? "Default read-only render of whatever the first available item is."
      : "No pre-existing content on this service at capture time — honest empty-tree state, not a bug by itself."
  );
}

async function section2(page, spaDb) {
  console.log("== section 2: per-service default views (write mode OFF) ==");
  let spa = spaDb;
  await harness.setWriteMode(page, spa, false);
  await defaultShot(page, spa, "db");

  for (const svc of ALL_SERVICES.slice(1)) {
    spa = await harness.sidebarBrowse(page, svc);
    await harness.setWriteMode(page, spa, false);
    await defaultShot(page, spa, svc);
  }
  return spa;
}

// ============================================================================
// section 3 — write-mode lifecycle (db)
// ============================================================================

async function section3(page) {
  console.log("== section 3: write-mode lifecycle (db) ==");
  let spa = await harness.sidebarBrowse(page, "db");
  await harness.setWriteMode(page, spa, false);
  await shot(
    page,
    "writemode-toggle-off",
    "db topbar",
    "write toggle OFF",
    "Full page at the moment write mode reads off; the toggle switch itself lives in the topbar."
  );

  await harness.setWriteMode(page, spa, true); // internally screenshots the native "Enable writes" modal before confirming
  spa = await harness.spaFrame(page);
  await shot(
    page,
    "writemode-toggle-on",
    "db topbar",
    "write toggle ON (after confirming the native Enable-writes modal)",
    "The native VS Code confirm modal for this exact transition was auto-captured by the harness immediately before this shot (see the adjacent 'write-mode-modal-before-confirm' file)."
  );
  return spa;
}

// ============================================================================
// section 4 — tabular (db)
// ============================================================================

async function section4(page, spa) {
  console.log("== section 4: tabular (db) ==");
  seedTabular();
  await sleep(250);
  await harness.clickService(spa, "db");
  await waitTreeSettled(spa);

  await revealAndClick(spa, TAB);
  await waitForGrid(spa);
  await sleep(250);
  await shot(
    page,
    "tabular-grid-write-on",
    "db / " + TAB,
    "populated grid, write mode ON, locked PK column visible",
    "The 'id' column should render locked/non-editable while the other columns are editable — compare column styling left to right."
  );

  const heads = await spa.evaluate(() => Array.from(document.querySelectorAll("table.grid thead th")).map((th) => th.textContent));
  const txtCol = heads.findIndex((h) => h.indexOf("txt") === 0);
  const cellSel = "table.grid tbody.gridbody tr:nth-child(1) td:nth-child(" + (txtCol + 1) + ")";
  await spa.click(cellSel);
  await spa.waitForSelector(cellSel + " input.celledit", { timeout: 10000 }).catch(() => {});
  await shot(
    page,
    "tabular-cell-editor-open",
    "db / " + TAB,
    "a single non-PK cell in edit mode",
    "In-place input replaces the cell's text; neighboring cells should stay visually stable (no layout shift)."
  );
  await page.keyboard.press("Escape");
  await sleep(300);

  await spa.waitForSelector("#insertrow", { timeout: 10000 });
  await spa.click("#insertrow");
  await spa.waitForSelector(".insertform", { timeout: 10000 });
  await shot(
    page,
    "tabular-insert-row-form",
    "db / " + TAB,
    "Insert-row form open",
    "One input per column inside the shared SPA modal — check labeling and any required-field cues."
  );
  await spa.click("#modalcancel");
  await sleep(300);

  await spa.waitForSelector("table.grid tbody.gridbody tr:nth-child(1) button.rowdel", { timeout: 10000 });
  await spa.click("table.grid tbody.gridbody tr:nth-child(1) button.rowdel");
  await spa.waitForSelector("#modal:not(.hidden)", { timeout: 10000 });
  await shot(
    page,
    "tabular-delete-row-confirm-modal",
    "db / " + TAB,
    "delete-row confirm modal",
    "The SPA's own confirm modal (not the native VS Code one) — check the destructive/danger styling on the confirm button."
  );
  await spa.click("#modalcancel");
  await sleep(300);

  await harness.clickService(spa, "db");
  await waitTreeSettled(spa);
  await spa.waitForSelector("#querylink", { timeout: 10000 });
  await spa.click("#querylink");
  await spa.waitForSelector("#qtext", { timeout: 10000 });
  await spa.evaluate((sql) => {
    document.getElementById("qtext").value = sql;
  }, "SELECT * FROM " + TAB + " ORDER BY id");
  await spa.click("#runq");
  await waitForGrid(spa);
  await sleep(300);
  await shot(
    page,
    "tabular-query-results",
    "db / query pane",
    "SELECT query results rendered as a grid",
    "Query results should read as read-only (no editable cells, no delete column, no Insert-row button) even though session write mode is ON."
  );

  await spa.evaluate(() => {
    document.getElementById("qtext").value = "SELEKT * FROM " + "uitest_gal_tab";
  });
  await spa.click("#runq");
  await sleep(800);
  await shot(
    page,
    "tabular-query-syntax-error",
    "db / query pane",
    "deliberate SQL syntax error",
    "Inline error state (#qresult .err) after running an invalid query — checking for an honest message, no crash/blank pane."
  );

  await harness.clickService(spa, "db");
  await waitTreeSettled(spa);
  await revealAndClick(spa, EMPTY_TAB);
  await waitForGrid(spa);
  await sleep(250);
  await shot(
    page,
    "tabular-empty-table",
    "db / " + EMPTY_TAB,
    "a genuinely empty (0-row) table",
    "Grid's own empty-state row is expected, never a blank void."
  );

  await harness.clickService(spa, "db");
  await waitTreeSettled(spa);
  await revealAndClick(spa, WIDE_TAB);
  await waitForGrid(spa);
  await sleep(250);
  await shot(
    page,
    "tabular-wide-longtext",
    "db / " + WIDE_TAB,
    "8-column table with one very long text value",
    "Horizontal overflow should stay contained inside the grid's own scroll wrapper, not push the whole page; the long value may be visually truncated — check whether there's any way to read it in full."
  );

  await harness.clickService(spa, "db");
  await waitTreeSettled(spa);
  return spa;
}

// ============================================================================
// section 5 — kv (cache)
// ============================================================================

async function section5(page, spa) {
  console.log("== section 5: kv (cache) ==");
  seedKV();
  await sleep(250);
  spa = await harness.sidebarBrowse(page, "cache");
  await harness.setWriteMode(page, spa, true); // carries over from db; no-op if already on
  await harness.clickService(spa, "cache");
  await waitTreeSettled(spa);
  await shot(
    page,
    "kv-key-list-mixed-types",
    "cache / root",
    "key list with all 5 redis types present",
    "Each type should render a distinct glyph (string/hash/list/set/zset) — compare against the seeded uitest_gal_kv_* keys."
  );

  await revealAndClick(spa, KV_STR);
  await sleep(250);
  await shot(
    page,
    "kv-string-blob-view",
    "cache / " + KV_STR,
    "string blob view, write mode ON",
    "Toolbar (save/delete/rename) and the TTL bar should both be visible since write mode is on."
  );

  await harness.clickService(spa, "cache");
  await waitTreeSettled(spa);
  await revealAndClick(spa, KV_HASH);
  await waitForGrid(spa);
  await sleep(250);
  await shot(
    page,
    "kv-hash-grid-view",
    "cache / " + KV_HASH,
    "hash rendered as a field/value grid",
    "The field (key) column should read as locked while the value column is editable."
  );

  await harness.clickService(spa, "cache");
  await waitTreeSettled(spa);
  await spa.waitForSelector("#createkeylink", { timeout: 10000 });
  await spa.click("#createkeylink");
  await spa.waitForSelector("#modal:not(.hidden)", { timeout: 10000 });
  await spa.select("#kvtype", "string");
  await shot(
    page,
    "kv-addkey-modal-string",
    "cache / Add-key modal",
    "type=string selected",
    "Baseline modal shape: name + type select + a single value field."
  );
  await spa.select("#kvtype", "hash");
  await spa.waitForSelector("#kvfield", { timeout: 5000 }).catch(() => {});
  await shot(
    page,
    "kv-addkey-modal-hash",
    "cache / Add-key modal",
    "type=hash selected",
    "Form should grow a field-name input in addition to name/type/value."
  );
  await spa.select("#kvtype", "zset");
  await spa.waitForSelector("#kvscore", { timeout: 5000 }).catch(() => {});
  await shot(
    page,
    "kv-addkey-modal-zset",
    "cache / Add-key modal",
    "type=zset selected",
    "Form should show member + score inputs instead of a plain value."
  );
  await spa.click("#modalcancel");
  await sleep(300);

  await harness.clickService(spa, "cache");
  await waitTreeSettled(spa);
  await revealAndClick(spa, KV_STR);
  await spa.waitForSelector(".ttlbar", { timeout: 10000 }).catch(() => {});
  await spa.click("#setttl");
  await spa.waitForSelector("#modal:not(.hidden)", { timeout: 10000 });
  await shot(
    page,
    "kv-ttl-modal",
    "cache / " + KV_STR,
    "Set-TTL prompt modal",
    "First half of a chained two-modal flow (enter seconds, then a separate confirm step) — this shot is the entry prompt."
  );
  await spa.click("#modalcancel");
  await sleep(300);

  await harness.clickService(spa, "cache");
  await waitTreeSettled(spa);
  await revealAndClick(spa, KV_HASH);
  await waitForGrid(spa);
  await sleep(200);
  const hasRowDel = await spa
    .waitForSelector("button.rowdel", { timeout: 8000 })
    .then(() => spa.evaluate(() => !!document.querySelector("button.rowdel:not([disabled])")))
    .catch(() => false);
  if (hasRowDel) {
    await spa.click("table.grid tbody tr button.rowdel");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 10000 });
    await shot(
      page,
      "kv-delete-confirm-modal",
      "cache / " + KV_HASH,
      "per-field delete confirm modal (hash entry)",
      "Hash/list/set/zset keys have no whole-key delete button (only a plain string blob does per README gotcha #7) — this is the per-row/-field delete affordance instead."
    );
    await spa.click("#modalcancel");
    await sleep(300);
  } else {
    SKIPPED.push({ state: "kv-delete-confirm-modal", why: "button.rowdel not present/enabled on the seeded hash key at capture time" });
  }

  await harness.clickService(spa, "cache");
  await waitTreeSettled(spa);
  return spa;
}

// ============================================================================
// section 6 — object (storage)
// ============================================================================

async function section6(page, spa) {
  console.log("== section 6: object (storage) ==");
  await seedObject();
  await sleep(350);
  spa = await harness.sidebarBrowse(page, "storage");
  await harness.setWriteMode(page, spa, true);
  await harness.clickService(spa, "storage");
  await waitTreeSettled(spa);
  await revealAndClick(spa, "uitest_gal");
  await sleep(300);
  await shot(
    page,
    "object-bucket-tree-folders",
    "storage / uitest_gal",
    "folder expanded showing files + a nested subfolder",
    "Check that folders and files are visually distinguishable and file sizes look byte-plausible."
  );

  await revealAndClick(spa, "readme.txt");
  await sleep(250);
  await shot(
    page,
    "object-text-preview",
    "storage / uitest_gal/readme.txt",
    "text object preview",
    "Plain-text content should render verbatim."
  );

  await harness.clickService(spa, "storage");
  await waitTreeSettled(spa);
  await revealAndClick(spa, "uitest_gal");
  await sleep(250);
  await revealAndClick(spa, "pic.png");
  await sleep(300);
  await shot(
    page,
    "object-image-preview",
    "storage / uitest_gal/pic.png",
    "image object preview",
    "A 1x1 PNG fixture — check that an actual <img> preview renders rather than a broken-image icon."
  );

  await harness.clickService(spa, "storage");
  await waitTreeSettled(spa);
  await revealAndClick(spa, "uitest_gal");
  await sleep(250);
  await revealAndClick(spa, "data.bin");
  await sleep(250);
  await shot(
    page,
    "object-binary-download-only",
    "storage / uitest_gal/data.bin",
    "non-previewable binary object",
    "Should show an honest 'binary, use Download' placeholder rather than garbled text or a blank pane."
  );

  await harness.clickService(spa, "storage");
  await waitTreeSettled(spa);
  await sleep(250);
  await shot(
    page,
    "object-upload-bar-root",
    "storage / bucket root",
    "upload affordance at the true bucket root, write mode ON",
    "By the product's own design, upload is only reachable at the absolute bucket root, never inside a subfolder — this shot is that root view."
  );

  return spa;
}

// ============================================================================
// section 7 — document (es + docs/typesense)
// ============================================================================

async function section7(page, spa) {
  console.log("== section 7: document (es + docs/typesense) ==");
  seedES();
  seedTS();
  await sleep(350);

  spa = await harness.sidebarBrowse(page, "es");
  await harness.setWriteMode(page, spa, true);
  await harness.clickService(spa, "es");
  await waitTreeSettled(spa);
  await shot(page, "document-es-index-tree", "es / root", "index tree, collapsed", "Root-level container for the seeded uitest_gal_es index.");

  await revealAndClick(spa, ES_INDEX);
  await sleep(300);
  await shot(
    page,
    "document-es-doc-list-expanded",
    "es / " + ES_INDEX,
    "index expanded showing doc ids",
    "Each id under the index should correspond 1:1 with a seeded doc (gal_doc1/2/3)."
  );

  await revealAndClick(spa, "gal_doc1");
  await sleep(250);
  await shot(
    page,
    "document-es-doc-detail",
    "es / " + ES_INDEX + "/gal_doc1",
    "single document JSON detail view",
    "Full document body rendered as JSON; save/delete affordances should be visible since write mode is on."
  );

  await harness.clickService(spa, "es");
  await waitTreeSettled(spa);
  await spa.waitForSelector("#searchlink", { timeout: 10000 });
  await spa.click("#searchlink");
  await spa.waitForSelector(".searchbar", { timeout: 10000 });
  await spa.select("#sidx", ES_INDEX);
  await spa.evaluate(() => {
    document.getElementById("sq").value = "";
  });
  await spa.type("#sq", "searchterm");
  await spa.click("#runs");
  await spa.waitForFunction(() => !document.querySelector("#sresult .state.loading"), { timeout: 15000 }).catch(() => {});
  await shot(
    page,
    "document-es-search-results",
    "es / search pane",
    "search for a distinctive term with exactly one match",
    "Should show exactly gal_doc3 (the only fixture doc whose body contains 'searchterm')."
  );

  await spa.evaluate(() => {
    document.getElementById("sq").value = "";
  });
  await spa.type("#sq", "zzz_gallery_nomatch_zzz");
  await spa.click("#runs");
  await spa.waitForFunction(() => !document.querySelector("#sresult .state.loading"), { timeout: 15000 }).catch(() => {});
  await shot(
    page,
    "document-es-search-zero-hit",
    "es / search pane",
    "search with zero matches",
    "Honest empty-result state expected, not a blank pane."
  );

  await spa.waitForSelector("#adddoc", { timeout: 10000 });
  await spa.click("#adddoc");
  await spa.waitForSelector("#docbody", { timeout: 10000 });
  await shot(
    page,
    "document-es-create-doc-modal",
    "es / create-doc modal",
    "create-document modal open",
    "id input + a JSON textarea for the document body."
  );
  await spa.type("#docid", "gal_doc1"); // existing id -> expected conflict
  await spa.type("#docbody", JSON.stringify({ id: "gal_doc1", title: "clobber attempt", body: "should be refused" }));
  await spa.click("#modalok");
  const badToast = await harness.waitToast(spa, 8000);
  await shot(
    page,
    "document-es-duplicate-id-bad-toast",
    "es / create-doc modal",
    "duplicate-id create attempt, toast still visible",
    "Toast observed at capture time: " + JSON.stringify(badToast)
  );

  await harness.drainToast(spa);
  await sleep(250);
  await harness.clickService(spa, "es");
  await waitTreeSettled(spa);
  await spa.click("#searchlink");
  await spa.waitForSelector(".searchbar", { timeout: 10000 });
  await spa.select("#sidx", ES_INDEX);
  await spa.waitForSelector("#adddoc", { timeout: 10000 });
  await spa.click("#adddoc");
  await spa.waitForSelector("#docbody", { timeout: 10000 });
  await spa.type("#docid", "gal_doc_new");
  await spa.type("#docbody", JSON.stringify({ id: "gal_doc_new", title: "Gallery new doc", body: "created live during the gallery sweep" }));
  await spa.click("#modalok");
  const goodToast = await harness.waitToast(spa, 8000);
  await shot(
    page,
    "document-es-create-success-good-toast",
    "es / create-doc modal",
    "valid create, toast still visible",
    "Toast observed at capture time: " + JSON.stringify(goodToast)
  );
  await harness.drainToast(spa);
  await sleep(250);

  // typesense (docs) -- the "one of docs/search" partner, lighter pass.
  spa = await harness.sidebarBrowse(page, "docs");
  await harness.setWriteMode(page, spa, true);
  await harness.clickService(spa, "docs");
  await waitTreeSettled(spa);
  await revealAndClick(spa, TS_COLLECTION);
  await sleep(300);
  await shot(
    page,
    "document-docs-typesense-index-tree",
    "docs (typesense) / " + TS_COLLECTION,
    "collection expanded showing doc ids",
    "Same family surface as es, different engine — compare rendering."
  );
  await revealAndClick(spa, "gal_doc1");
  await sleep(250);
  await shot(
    page,
    "document-docs-typesense-doc-detail",
    "docs (typesense) / " + TS_COLLECTION + "/gal_doc1",
    "single document JSON detail view",
    "Confirm the JSON detail rendering is consistent with elasticsearch's."
  );

  return spa;
}

// ============================================================================
// section 8 — qdrant (vectors)
// ============================================================================

async function section8(page, spa) {
  console.log("== section 8: qdrant (vectors) ==");
  seedQdrant();
  await sleep(300);
  spa = await harness.sidebarBrowse(page, "vectors");
  // Write mode is left ON (global session state, carried from db/cache/
  // storage/es) deliberately -- the harder, more meaningful case for
  // confirming qdrant stays inert even when write mode is globally enabled.
  await harness.clickService(spa, "vectors");
  await waitTreeSettled(spa);
  await revealAndClick(spa, VEC_COLLECTION);
  await sleep(300);
  await shot(
    page,
    "qdrant-points-list",
    "vectors / " + VEC_COLLECTION,
    "collection expanded showing points, write mode ON",
    "vectors is view-only by design — confirm no create/upload affordance appears even with write mode on."
  );

  await revealAndClick(spa, "1");
  await spa.waitForSelector(".vectorbox, pre.blob", { timeout: 10000 }).catch(() => {});
  await sleep(250);
  const collapsedState = await spa.evaluate(() => ({
    hasSave: !!document.getElementById("saveblob"),
    hasDelete: !!document.getElementById("delblob"),
    hasRename: !!document.getElementById("renameblob"),
  }));
  await shot(
    page,
    "qdrant-point-detail-collapsed",
    "vectors / " + VEC_COLLECTION + "/1",
    "point detail, vector collapsed (default)",
    "Write-affordance probe at capture time (write mode ON globally): " + JSON.stringify(collapsedState) +
      " -- all false/absent is the honest view-only outcome."
  );

  await spa.click(".vecsummary button.link");
  await sleep(200);
  await shot(
    page,
    "qdrant-point-detail-vector-expanded",
    "vectors / " + VEC_COLLECTION + "/1",
    "raw vector floats revealed",
    "Toggled via the 'Show raw vector' link; compare against the collapsed shot for layout stability."
  );

  return spa;
}

// ============================================================================
// section 9 — write mode back OFF, then stream (queue + events)
// ============================================================================

async function section9(page, spa) {
  console.log("== section 9: write mode OFF, then stream (queue + events) ==");
  await harness.setWriteMode(page, spa, false);

  spa = await harness.sidebarBrowse(page, "queue");
  await harness.clickService(spa, "queue");
  await waitTreeSettled(spa);
  await shot(
    page,
    "stream-queue-tree",
    "queue (nats)",
    "stream list, write mode off (read-only by nature at the provider level)",
    "Pre-existing streams, not seeded by this sweep."
  );
  const streamNames = await spa.evaluate(() => Array.from(document.querySelectorAll("#tree .node .nname")).map((el) => el.textContent));
  const streamTarget = streamNames.includes("EVENTS") ? "EVENTS" : streamNames[0];
  if (streamTarget) {
    await revealAndClick(spa, streamTarget);
    await sleep(300);
    await shot(
      page,
      "stream-queue-metadata-card",
      "queue (nats) / " + streamTarget,
      "stream metadata card",
      "Labelled summary card, never a raw/editable-looking blob."
    );
  } else {
    SKIPPED.push({ state: "stream-queue-metadata-card", why: "no streams present on queue at capture time" });
  }

  spa = await harness.sidebarBrowse(page, "events");
  await harness.clickService(spa, "events");
  await waitTreeSettled(spa);
  await shot(page, "stream-events-tree", "events (kafka)", "topic list", "Pre-existing topics, not seeded by this sweep.");
  const topicNames = await spa.evaluate(() => Array.from(document.querySelectorAll("#tree .node .nname")).map((el) => el.textContent));
  if (topicNames[0]) {
    await revealAndClick(spa, topicNames[0]);
    await sleep(300);
    await shot(
      page,
      "stream-events-metadata-card",
      "events (kafka) / " + topicNames[0],
      "topic metadata card",
      "Same card family as nats — compare field sets between engines."
    );
  } else {
    SKIPPED.push({ state: "stream-events-metadata-card", why: "no topics present on events at capture time" });
  }

  return spa;
}

// ============================================================================
// section 10 — states canon (loading spinner best-effort, VPN-gate skip note)
// ============================================================================

async function section10(page, spa) {
  console.log("== section 10: states canon ==");
  // clickService (in-SPA rail switch) deliberately does NOT wait for settle
  // (per harness.js), unlike sidebarBrowse/openConsole -- racing an immediate
  // read against it is the only honest way to try to catch #tree .state.loading.
  await harness.clickService(spa, "ch");
  const caughtLoading = await spa.evaluate(() => !!document.querySelector("#tree .state.loading")).catch(() => false);
  await shot(
    page,
    "loading-spinner-attempt",
    "ch (in-SPA rail switch)",
    caughtLoading ? "caught mid-load" : "resolved before the screenshot could catch it",
    caughtLoading
      ? "Genuine #tree .state.loading spinner caught live."
      : "Best-effort race did not catch the spinner -- the tree fetch resolved faster than the read+screenshot round-trip."
  );
  if (!caughtLoading) {
    SKIPPED.push({
      state: "loading spinner (clean catch)",
      why: "one best-effort race attempted (see loading-spinner-attempt); the tree settled before the screenshot, so no genuinely mid-load frame was captured",
    });
  }
  await waitTreeSettled(spa);

  SKIPPED.push({
    state: "VPN-gate / unreachable-service state",
    why: "would require stopping a live managed service or breaking VPN reachability to the container -- explicitly out of bounds for this sweep, not attempted.",
  });

  return spa;
}

// ============================================================================
// section 11 — narrow layout (1000x800)
// ============================================================================

async function section11(page) {
  console.log("== section 11: narrow layout (1000x800) ==");
  await page.setViewport({ width: 1000, height: 800 });
  await sleep(400);

  await harness.openSidebar(page);
  await shot(page, "narrow-sidebar", "sidebar", "1000x800 viewport", "Compare row/badge layout against the 1440-wide version (shot 1).");

  let spa = await harness.sidebarBrowse(page, "db");
  await harness.setWriteMode(page, spa, false);
  await harness.clickService(spa, "db");
  await waitTreeSettled(spa);
  await revealAndClick(spa, TAB); // fixture still alive -- torn down only at the very end
  await waitForGrid(spa);
  await sleep(250);
  await shot(
    page,
    "narrow-tabular-grid",
    "db / " + TAB,
    "1000x800 viewport",
    "Check horizontal scroll containment and column truncation at this width."
  );

  spa = await harness.sidebarBrowse(page, "cache");
  await harness.setWriteMode(page, spa, false);
  await revealAndClick(spa, KV_STR);
  await sleep(250);
  await shot(page, "narrow-kv-blob", "cache / " + KV_STR, "1000x800 viewport", "Blob toolbar layout at narrow width.");

  await harness.setWriteMode(page, spa, true); // internally screenshots the native modal again, this time at 1000x800
  spa = await harness.spaFrame(page);
  await revealAndClick(spa, KV_STR);
  await spa.waitForSelector("#delblob", { timeout: 10000 }).catch(() => {});
  await spa.click("#delblob");
  await spa.waitForSelector("#modal:not(.hidden)", { timeout: 10000 });
  await shot(page, "narrow-modal", "cache / " + KV_STR, "delete-confirm modal at 1000x800", "Modal sizing/centering at narrow width.");
  await spa.click("#modalcancel");
  await harness.setWriteMode(page, spa, false);

  await page.setViewport({ width: 1440, height: 900 });
  await sleep(300);
}

// ============================================================================
// section 12 — standalone SPA (mechanism per CORE-3 / scenarios/stream-standalone.js)
// ============================================================================

function sshRaw(cmd, timeoutMs) {
  const c = cfg();
  const host = c.DC_SSH_HOST || "zcp";
  return execFileSync("ssh", ["-o", "ConnectTimeout=8", host, cmd], { encoding: "utf8", timeout: timeoutMs || 20000 }).trim();
}

async function section12(page) {
  console.log("== section 12: standalone SPA ==");
  const c = cfg();
  const marker = "uitest_gal_standalone_" + Date.now();
  const readyFile = "/tmp/" + marker + "_ready.json";
  const errFile = "/tmp/" + marker + "_err.log";
  let ready;
  try {
    sshRaw(
      "rm -f " + readyFile + " " + errFile + "; nohup timeout 300 zcp studio console serve --port 0 >" + readyFile + " 2>" + errFile +
        " </dev/null & sleep 2; cat " + readyFile
    );
    ready = JSON.parse(sshRaw("cat " + readyFile));
  } catch (e) {
    SKIPPED.push({
      state: "standalone-spa-readonly",
      why: "could not spawn/parse `zcp studio console serve --port 0` over SSH: " + String(e && e.message ? e.message : e),
    });
    return;
  }
  const portMatch = /:(\d+)\/?$/.exec(ready.url || "");
  const port = portMatch ? portMatch[1] : null;
  if (!port) {
    SKIPPED.push({ state: "standalone-spa-readonly", why: "no port found in the ready-line url: " + JSON.stringify(ready) });
    return;
  }
  const origin = new URL(c.DC_URL).origin;
  const proxyBase = origin + "/proxy/" + port + "/";
  const standaloneURL = proxyBase + "#t=" + encodeURIComponent(ready.sessionToken);

  const browser = page.browser();
  const spage = await browser.newPage();
  await spage.setViewport({ width: 1440, height: 900 });
  const u = new URL(c.DC_URL);
  await spage.setCookie({ name: "__zcp_auth", value: c.DC_AUTH_TOKEN, domain: u.hostname, path: "/", secure: true, httpOnly: true });
  try {
    await spage.goto(standaloneURL, { waitUntil: "domcontentloaded", timeout: 30000 });
    await spage.waitForSelector("#services li", { timeout: 20000 }).catch(() => {});
    await spage.evaluate((svc) => {
      const items = Array.from(document.querySelectorAll("#services li"));
      const li = items.find((el) => {
        const span = el.querySelector("span");
        return span && span.textContent === svc;
      });
      if (li) li.click();
    }, "cache");
    await sleep(500);
    await shot(
      spage,
      "standalone-spa-readonly",
      "standalone tab (plain browser tab, bearer-only, fragment auth)",
      "cache browsed read-only, no write toggle",
      "NOT the code-server workbench -- a completely separate plain-tab session reached via `zcp studio console serve` + code-server's own /proxy/<port>/ forward. The write-mode switch should be absent/inert and a visible 'read-only' badge should be present."
    );
  } finally {
    await spage.close().catch(() => {});
  }
}

// ============================================================================
// main
// ============================================================================

async function main() {
  fs.mkdirSync(EVIDENCE_DIR, { recursive: true });
  console.log("self-heal: tearing down any leftover uitest_gal_* fixtures from a prior aborted run...");
  try {
    await teardownAllFixtures();
  } catch (e) {
    console.error("self-heal teardown warning:", e && e.message ? e.message : e);
  }

  console.log("connecting (launch + auth + wait for workbench)...");
  const { browser, page } = await harness.connect();
  harness.setScenario(SCENARIO_ID);
  console.log("connected.");

  try {
    let spa = await section1(page);
    spa = await section2(page, spa);
    spa = await section3(page);
    spa = await section4(page, spa);
    spa = await section5(page, spa);
    spa = await section6(page, spa);
    spa = await section7(page, spa);
    spa = await section8(page, spa);
    spa = await section9(page, spa);
    spa = await section10(page, spa);
    await section11(page);
    await section12(page);
  } finally {
    console.log("tearing down uitest_gal_* fixtures...");
    try {
      await teardownAllFixtures();
    } catch (e) {
      console.error("teardown error:", e && e.message ? e.message : e);
    }
    finalizeIndex();
    writeIndexMd();
    console.log("wrote " + INDEX.length + " index entries to " + path.join(EVIDENCE_DIR, "index.md"));
    await browser.close();
  }
}

main().catch((e) => {
  console.error("gallery.js: fatal:", e && e.stack ? e.stack : e);
  process.exit(1);
});
