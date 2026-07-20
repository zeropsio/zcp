"use strict";

// KV-* (family "kv") + OBJ-* (family "object") scenarios — A3 fan-out.
//
// KV drives valkey (service "cache") through the real embedded console UI.
// OBJ drives object storage (service "storage", S3-compatible) the same way.
// Engine oracle for KV is redis-cli (lib/engines.js). There is NO S3 CLI on
// the container (`mc` resolves to GNU Midnight Commander, not the MinIO
// client — checked live; no aws/mcli/s5cmd/s3cmd/rclone either), so OBJ's
// oracle is a hand-rolled AWS SigV4 client below (Node's builtin crypto +
// fetch only — this harness ships zero deps beyond puppeteer-core, and a
// dependency-free signer keeps that true). Verified live against the real
// bucket (PUT/GET/LIST/DELETE all round-tripped correctly) before being
// wired into these scenarios.
//
// Data discipline: every KV key starts with "uitest" (both flat "uitest_x"
// and namespaced "uitest:ns:x" — both are unambiguously ours; neither
// collides with the seeded fixtures leaderboard/queue:jobs/user:1/user:2/
// tags/greeting, confirmed via a live KEYS scan before writing anything).
// Every OBJ object lives under the "uitest/" key prefix in the shared
// bucket. Both teardown in a finally block, at start (self-heal) and end.

const crypto = require("crypto");
const fs = require("fs");
const path = require("path");
const { loadConfig } = require("../lib/config");
const runner = require("../lib/runner");

const KV_PREFIX = "uitest";
const CACHE_SERVICE = "cache";
const OBJECT_SERVICE = "storage";
const OBJ_PREFIX = "uitest/";
const GREETING_FIXTURE = "greeting"; // seeded; never touched

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// ============================================================================
// tree / grid DOM helpers (local — core.js's equivalents are scenario-scoped,
// lib/harness.js is out of bounds to edit)
// ============================================================================

async function treeNodeNames(frame) {
  return frame.evaluate(() => Array.from(document.querySelectorAll("#tree .node .nname")).map((el) => el.textContent));
}

async function treeHasNode(frame, name) {
  return (await treeNodeNames(frame)).includes(name);
}

// treeNodeInfo reads every root-level row's name + glyph title (KV entryType,
// when the server declared one) without clicking anything — cheap enough to
// call before deciding which node to open. Scoped to #tree's DIRECT
// .node-wrap children only (the root level) — a node's own expanded children
// live in a SIBLING ".children" div nested one level deeper (see childrenInfo
// below), never as direct children of #tree, so this never picks them up.
async function treeNodeInfo(frame) {
  return frame.evaluate(() => {
    return Array.from(document.querySelectorAll("#tree > .node-wrap > .node")).map((row) => {
      const nm = row.querySelector(".nname");
      const kind = row.querySelector(".kind");
      return { name: nm ? nm.textContent : null, glyph: kind ? kind.textContent : null, title: kind ? kind.getAttribute("title") : null };
    });
  });
}

// childrenInfo reads the DIRECT children of an already-expanded container
// node named `parentName` (its sibling ".children" div's direct .node-wrap
// entries) — the correctly-scoped counterpart to treeNodeInfo for any level
// below root. Returns null if parentName isn't found, [] if found but not
// (yet) expanded/childless.
async function childrenInfo(frame, parentName) {
  return frame.evaluate((pname) => {
    const wraps = Array.from(document.querySelectorAll("#tree .node-wrap"));
    const parentWrap = wraps.find((w) => {
      const nm = w.querySelector(":scope > .node .nname");
      return nm && nm.textContent === pname;
    });
    if (!parentWrap) return null;
    const childrenDiv = parentWrap.querySelector(":scope > .children");
    if (!childrenDiv) return [];
    return Array.from(childrenDiv.querySelectorAll(":scope > .node-wrap > .node")).map((row) => {
      const nm = row.querySelector(".nname");
      const kind = row.querySelector(".kind");
      return { name: nm ? nm.textContent : null, glyph: kind ? kind.textContent : null, title: kind ? kind.getAttribute("title") : null };
    });
  }, parentName);
}

// clickTreeNode waits for a node named `name` to appear anywhere in the
// CURRENTLY VISIBLE tree, then clicks it. The wait matters: harness.js's
// openConsole()/openService() only probe for #services li as their "ready"
// signal (the SPA frame is interactive), not for the tree's own async
// loadTree() fetch to finish -- a bare click immediately after opening a
// service can race that fetch and silently find nothing (caught live: KV-6's
// first navigation read the still-showing "Browse cache in the tree."
// placeholder because the click ran before the seeded key was in the DOM at
// all). A short wait here is a no-op once the tree has already loaded, so
// this is never slower for an already-populated tree and only helps a
// just-opened one; a genuine absence (the caller expects `false`) just takes
// up to the timeout to conclude instead of returning instantly.
async function clickTreeNode(frame, name, timeoutMs) {
  try {
    await frame.waitForFunction(
      (n) => Array.from(document.querySelectorAll("#tree .node .nname")).some((el) => el.textContent === n),
      { timeout: timeoutMs || 8000 },
      name
    );
  } catch (_) {
    /* fall through -- the click below honestly returns false on genuine absence */
  }
  const clicked = await frame.evaluate((n) => {
    const rows = Array.from(document.querySelectorAll("#tree .node"));
    const row = rows.find((r) => {
      const nm = r.querySelector(".nname");
      return nm && nm.textContent === n;
    });
    if (!row) return false;
    row.click();
    return true;
  }, name);
  return clicked;
}

// blobViewState snapshots the #content pane after openBlob() has rendered —
// used both positively (does Save/Delete/Rename show when expected) and
// negatively (do they stay absent read-only / on a collection). When
// `expectName` is given, waits for the toolbar's <b> title to match it
// before reading -- #dlblob ALONE isn't a safe "ready" signal for a second
// navigation in the same scenario, because the PREVIOUS blob's toolbar
// (also carrying #dlblob) is still in the DOM until openBlob()'s new
// content.innerHTML assignment replaces it, so a bare #dlblob wait can
// resolve instantly against stale content. Matching the title name is the
// reliable "this is the NEW key's render" signal. Caught live: KV-6's
// ansi-value check read the ".state.loading" spinner (fixed by waiting), and
// a title-based wait additionally guards the sequential-navigation case a
// bare element-presence wait cannot.
async function blobViewState(frame, expectName) {
  try {
    if (expectName) {
      await frame.waitForFunction(
        (n) => {
          const b = document.querySelector("#content .toolbar b");
          return !!b && b.textContent === n && !!document.getElementById("dlblob");
        },
        { timeout: 8000 },
        expectName
      );
    } else {
      await frame.waitForSelector("#dlblob", { timeout: 8000 });
    }
  } catch (_) {
    /* fall through -- an honest empty/placeholder read still reflects reality */
  }
  return frame.evaluate(() => ({
    hasGrid: !!document.querySelector("table.grid"),
    hasSave: !!document.getElementById("saveblob"),
    hasDelete: !!document.getElementById("delblob"),
    hasRename: !!document.getElementById("renameblob"),
    hasDownload: !!document.getElementById("dlblob"),
    hasImage: !!document.querySelector("img.imgpreview"),
    preText: document.querySelector("pre.blob") ? document.querySelector("pre.blob").textContent : null,
    editorText: document.getElementById("blobedit") ? document.getElementById("blobedit").value : null,
    placeholderText: (document.querySelector("#content .placeholder") || {}).textContent || null,
    metaText: (document.querySelector("#content .toolbar .meta") || {}).textContent || null,
  }));
}

// gridData reads the currently-open grid. It first waits for the grid to
// actually finish loading -- openTable() shows a .state.loading placeholder
// while ReadTable()'s async fetch is in flight, then replaces it with the
// real <table class="grid"> (populated with real rows OR the single
// .state.empty "No rows" row) only once that resolves. A bare read right
// after a click can race that fetch and misread "still loading" as "zero
// rows" -- caught live (KV-2's set grid: gridData() read [] a few ms before
// the same DOM had 3 correct rows, confirmed by the very next screenshot).
// Waiting for tbody.gridbody to have ANY child (a real row or the
// empty-state row) resolves the moment rendering completes either way, so a
// genuinely empty table still reads as [] honestly -- it just never reads
// early.
async function gridData(frame) {
  try {
    await frame.waitForFunction(
      () => {
        const body = document.querySelector("table.grid tbody.gridbody");
        return !!body && body.children.length > 0;
      },
      { timeout: 8000 }
    );
  } catch (_) {
    /* fall through -- read whatever is actually there rather than throw */
  }
  return frame.evaluate(() => ({
    heads: Array.from(document.querySelectorAll("table.grid thead th")).map((th) => th.textContent),
    rows: Array.from(document.querySelectorAll("table.grid tbody.gridbody tr")).map((tr) =>
      Array.from(tr.querySelectorAll("td")).map((td) => td.textContent)
    ),
    hasInsert: !!document.getElementById("insertrow"),
    viewOnlyBadge: (document.querySelector(".toolbar .badge.view-only") || {}).textContent || null,
  }));
}

async function toolbarButtons(frame) {
  return frame.evaluate(() => Array.from(document.querySelectorAll(".toolbar button")).map((b) => b.id || b.textContent));
}

// setInput clears an input via the DOM select() API (not a simulated
// triple-click -- unreliable against a pre-filled, already-focused input in
// this environment, see the .celledit fix above) and types `value` into it.
async function setInput(frame, sel, value) {
  await frame.evaluate((s) => {
    const el = document.querySelector(s);
    if (el) { el.focus(); if (typeof el.select === "function") el.select(); }
  }, sel);
  if (value) await frame.type(sel, value);
}

async function blurActive(frame) {
  await frame.evaluate(() => {
    if (document.activeElement && typeof document.activeElement.blur === "function") document.activeElement.blur();
  });
}

// waitToastGone drains the current toast before the next mutation fires.
// README gotcha #8: toasts self-remove after 2.6s and waitToast() reads the
// FIRST .toast in DOM order -- back-to-back mutations closer together than
// that window let a later waitToast() read an EARLIER, still-fading toast
// instead of its own. Caught live: KV-3's create-key attempts loop originally
// used a flat 300ms gap and read a stale "Key created." toast (left over from
// the prior successful string-type create) for BOTH the hash and zset
// attempts, which actually 400 server-side (confirmed via a direct HTTP
// replay of the identical request) -- the engine truth (TYPE=none) was
// always right, only the captured toast TEXT was stale. Call this right
// after every harness.waitToast() and before triggering the next mutation.
async function waitToastGone(frame, timeoutMs) {
  try {
    await frame.waitForFunction(() => !document.querySelector(".toast.good, .toast.bad, .toast.warn"), { timeout: timeoutMs || 3500 });
  } catch (_) {
    /* best effort -- proceed even if a toast lingers past the timeout */
  }
}

// waitTreeSettled polls #tree's node count until it stops changing across
// two checks spaced apart, so a caller's next clickTreeNode() never lands
// mid-reload. Needed because app.js's applyWriteMode() ITSELF calls
// loadTree() when a service is already active (in addition to any explicit
// selectService() a caller triggers) -- appendTreePage() never clears prior
// node content before appending a fresh page's nodes, so two overlapping
// reloads for the same root leave every node DUPLICATED (caught live: KV-4's
// #tree showed the full 7-item list twice after openService(write=true), and
// the click that was supposed to open uitest_kv4_str silently landed on
// nothing because the tree was still being torn down/rebuilt underneath it).
async function waitTreeSettled(frame, timeoutMs) {
  const deadline = Date.now() + (timeoutMs || 4000);
  let last = -1;
  let stableStreak = 0;
  while (Date.now() < deadline) {
    const n = await frame.evaluate(() => document.querySelectorAll("#tree .node").length).catch(() => 0);
    if (n > 0 && n === last) {
      stableStreak++;
      if (stableStreak >= 2) return;
    } else {
      stableStreak = 0;
    }
    last = n;
    await sleep(150);
  }
}

// openService opens `service` embedded, drives write mode to `write`, and
// (when enabling) forces a fresh selectService() render the same way CORE-1
// does — app.js's top-level hint/rail doesn't reactively re-render on a bare
// write-mode toggle, so a stale hint would hide createkeylink/upload bars.
// Waits for the write-mode-triggered reload to settle BEFORE forcing that
// second render, and again afterward, so the returned frame's tree is never
// mid-reload for the caller's first clickTreeNode() (see waitTreeSettled).
async function openService(ctx, service, write) {
  const spa = await ctx.harness.openConsole(ctx.page, service);
  await ctx.harness.setWriteMode(ctx.page, spa, write);
  if (write) {
    await waitTreeSettled(spa);
    await ctx.harness.clickService(spa, service);
    await waitTreeSettled(spa);
  }
  return ctx.harness.spaFrame ? await ctx.harness.spaFrame(ctx.page) : spa;
}

// ============================================================================
// KV oracle (lib/engines.js redis()) + teardown
// ============================================================================

function kvTeardown(engines) {
  try {
    const out = engines.redis(["KEYS", KV_PREFIX + "*"]);
    const leftovers = String(out || "")
      .split("\n")
      .map((s) => s.trim())
      .filter(Boolean);
    for (const k of leftovers) {
      try {
        engines.redis(["DEL", k]);
      } catch (_) {
        /* best effort */
      }
    }
  } catch (_) {
    /* best effort */
  }
}

// ============================================================================
// OBJ oracle — hand-rolled AWS SigV4 (MinIO-compatible path-style), no deps.
// Verified live: PUT 200 / GET 200(match) / LIST 200(XML) / DELETE 204 /
// GET-after-delete 404, against the real bucket (see scratchpad probe run
// before this file was written).
// ============================================================================

function s3Cfg() {
  const cfg = loadConfig();
  for (const k of ["DC_S3_ENDPOINT", "DC_S3_ACCESS_KEY", "DC_S3_SECRET_KEY", "DC_S3_BUCKET"]) {
    if (!cfg[k]) throw new Error("s3Cfg: " + k + " missing from local.config.json");
  }
  return cfg;
}
function sha256hex(buf) {
  return crypto.createHash("sha256").update(buf).digest("hex");
}
function hmac(key, data) {
  return crypto.createHmac("sha256", key).update(data, "utf8").digest();
}
function canonicalQueryString(params) {
  const keys = Object.keys(params || {}).sort();
  return keys.map((k) => encodeURIComponent(k) + "=" + encodeURIComponent(String(params[k]))).join("&");
}
function s3SignedRequest(method, key, opts) {
  opts = opts || {};
  const cfg = s3Cfg();
  const region = "us-east-1"; // MinIO-compatible: any consistent region string works
  const service = "s3";
  const endpoint = new URL(cfg.DC_S3_ENDPOINT);
  const now = new Date();
  const amzDate = now.toISOString().replace(/[:-]/g, "").replace(/\.\d{3}Z$/, "Z");
  const dateStamp = amzDate.slice(0, 8);
  const body = opts.body || Buffer.alloc(0);
  const payloadHash = sha256hex(body);
  const encodedKey = key ? key.split("/").map(encodeURIComponent).join("/") : "";
  const canonicalURI = "/" + encodeURIComponent(cfg.DC_S3_BUCKET) + (encodedKey ? "/" + encodedKey : "");
  const qs = canonicalQueryString(opts.query);
  const headers = { host: endpoint.host, "x-amz-content-sha256": payloadHash, "x-amz-date": amzDate };
  if (opts.contentType) headers["content-type"] = opts.contentType;
  const sortedKeys = Object.keys(headers).sort();
  const canonicalHeaders = sortedKeys.map((k) => k + ":" + headers[k] + "\n").join("");
  const signedHeaders = sortedKeys.join(";");
  const canonicalRequest = [method, canonicalURI, qs, canonicalHeaders, signedHeaders, payloadHash].join("\n");
  const credentialScope = dateStamp + "/" + region + "/" + service + "/aws4_request";
  const stringToSign = ["AWS4-HMAC-SHA256", amzDate, credentialScope, sha256hex(Buffer.from(canonicalRequest))].join("\n");
  const kDate = hmac("AWS4" + cfg.DC_S3_SECRET_KEY, dateStamp);
  const kRegion = hmac(kDate, region);
  const kService = hmac(kRegion, service);
  const kSigning = hmac(kService, "aws4_request");
  const signature = crypto.createHmac("sha256", kSigning).update(stringToSign).digest("hex");
  headers.authorization =
    "AWS4-HMAC-SHA256 Credential=" + cfg.DC_S3_ACCESS_KEY + "/" + credentialScope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature;
  const url = endpoint.origin + canonicalURI + (qs ? "?" + qs : "");
  return { url, headers, body };
}
async function s3(method, key, opts) {
  const { url, headers, body } = s3SignedRequest(method, key, opts || {});
  const hasBody = !(method === "GET" || method === "DELETE" || method === "HEAD");
  return fetch(url, { method, headers, body: hasBody ? body : undefined });
}
function decodeXMLEntities(s) {
  return String(s || "")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'");
}
// s3ListAll pages ListObjectsV2 (no delimiter — recursive) for `prefix`.
async function s3ListAll(prefix) {
  const out = [];
  let token = null;
  for (;;) {
    const query = { "list-type": "2", "prefix": prefix };
    if (token) query["continuation-token"] = token;
    const r = await s3("GET", "", { query });
    const xml = await r.text();
    if (r.status !== 200) throw new Error("s3ListAll: " + r.status + " " + xml.slice(0, 300));
    const re = /<Contents>([\s\S]*?)<\/Contents>/g;
    let m;
    while ((m = re.exec(xml))) {
      const key = (/<Key>([\s\S]*?)<\/Key>/.exec(m[1]) || [])[1];
      const size = (/<Size>([\s\S]*?)<\/Size>/.exec(m[1]) || [])[1];
      if (key) out.push({ key: decodeXMLEntities(key), size: size ? parseInt(size, 10) : null });
    }
    const truncated = /<IsTruncated>true<\/IsTruncated>/.test(xml);
    if (!truncated) break;
    token = (/<NextContinuationToken>([\s\S]*?)<\/NextContinuationToken>/.exec(xml) || [])[1];
    if (!token) break;
  }
  return out;
}
async function objTeardown() {
  try {
    const objs = await s3ListAll(OBJ_PREFIX);
    for (const o of objs) {
      try {
        await s3("DELETE", o.key);
      } catch (_) {
        /* best effort */
      }
    }
  } catch (_) {
    /* best effort */
  }
}

const TINY_PNG_B64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=";

// ============================================================================
// KV-1 — key list + filter
// ============================================================================

async function runKV1(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  kvTeardown(engines);
  const flat = ["uitest_kv1_a", "uitest_kv1_b", "uitest_kv1_c", "uitest_kv1_d"];
  const namespaced = ["uitest:kv1ns:one", "uitest:kv1ns:two", "uitest:kv1other:x"];
  try {
    for (const k of flat) engines.redis(["SET", k, "v"]);
    for (const k of namespaced) engines.redis(["SET", k, "v"]);

    const spa = await harness.openConsole(page, CACHE_SERVICE);
    await harness.setWriteMode(page, spa, false);
    await sleep(150);
    const rootInfo = await treeNodeInfo(spa);
    await evidence("01-root-tree");

    const rootNames = rootInfo.map((n) => n.name);
    for (const k of flat) {
      if (!rootNames.includes(k)) {
        addFinding({
          severity: "S1",
          title: 'Flat seeded key "' + k + '" not visible at KV tree root',
          repro: "redis SET " + k + " v; openConsole(cache); read #tree root node names",
          expected: "key present as a root-level leaf",
          actual: "root names: " + JSON.stringify(rootNames),
          evidence: ["evidence/KV-1/01-root-tree.png"],
        });
      }
    }
    // Namespaced keys (colon-delimited) are grouped into synthetic containers
    // per kv.go's own doc comment ("':' segment become synthetic containers").
    // Record what actually renders rather than assuming a fixed nesting depth.
    const hasNsContainer = rootNames.includes("uitest");
    if (!hasNsContainer) {
      addFinding({
        severity: "S2",
        title: 'No "uitest" synthetic container at tree root for colon-namespaced keys',
        repro: "redis SET uitest:kv1ns:one v (and siblings); openConsole(cache); read #tree root",
        expected: 'a container node named "uitest" grouping every uitest:* key',
        actual: "root names: " + JSON.stringify(rootNames),
        evidence: ["evidence/KV-1/01-root-tree.png"],
      });
    } else {
      const opened = await clickTreeNode(spa, "uitest");
      await sleep(250);
      const lvl2 = await childrenInfo(spa, "uitest");
      await evidence("02-uitest-container-expanded");
      addFinding({
        severity: "S3",
        title: "KV-1 namespacing observation (informational, not necessarily a bug)",
        repro: 'expand "uitest" container after seeding uitest:kv1ns:{one,two} + uitest:kv1other:x',
        expected: "n/a — recording actual structure for the fix-loop/UX lane",
        actual: "opened=" + opened + "; level-2 nodes: " + JSON.stringify((lvl2 || []).map((n) => n.name + ":" + n.glyph)),
        evidence: ["evidence/KV-1/02-uitest-container-expanded.png"],
        status: "info",
      });
    }

    // No dedicated filter/search input exists for the KV tree (checked against
    // the shipped app.js: openSearch()/#sidx/#sq are wired only for document
    // services via ACTION.searchDocs, never for the tree itself) — honest
    // absence, not a bug (the brief hedges with "if present").
    const hasFilterInput = await spa.evaluate(() => !!document.querySelector("#tree input, #rail input, input#treefilter, input#search"));
    addFinding({
      severity: "S3",
      title: "KV-1: no tree filter/search input exists for the KV key tree",
      repro: "openConsole(cache); look for any filter/search <input> near #tree/#rail",
      expected: "n/a — informational",
      actual: "hasFilterInput=" + hasFilterInput,
      evidence: [],
      status: "info",
    });
  } finally {
    kvTeardown(engines);
  }
}

// ============================================================================
// KV-2 — type rendering (string/hash/list/set/zset) vs redis-cli truth
// ============================================================================

async function runKV2(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  kvTeardown(engines);
  const K = {
    string: "uitest_kv2_string",
    hash: "uitest_kv2_hash",
    list: "uitest_kv2_list",
    set: "uitest_kv2_set",
    zset: "uitest_kv2_zset",
  };
  try {
    engines.redis(["SET", K.string, "hello"]);
    engines.redis(["HSET", K.hash, "f1", "v1", "f2", "v2", "f3", "v3"]);
    engines.redis(["RPUSH", K.list, "l1", "l2", "l3"]);
    engines.redis(["SADD", K.set, "s1", "s2", "s3"]);
    engines.redis(["ZADD", K.zset, "1.5", "z1", "2.5", "z2", "3.5", "z3"]);

    const spa = await harness.openConsole(page, CACHE_SERVICE);
    await harness.setWriteMode(page, spa, false);
    await sleep(150);
    const rootInfo = await treeNodeInfo(spa);
    await evidence("01-root-with-all-types");

    const glyphExpected = { string: "◇", hash: "#", list: "≡", set: "∈", zset: "⇅" };
    for (const [type, key] of Object.entries(K)) {
      const row = rootInfo.find((n) => n.name === key);
      if (!row) {
        addFinding({
          severity: "S1",
          title: 'KV-2: seeded "' + type + '" key "' + key + '" not visible at tree root',
          repro: "seed via redis-cli; openConsole(cache); look in #tree",
          expected: "leaf present",
          actual: "root names: " + JSON.stringify(rootInfo.map((n) => n.name)),
          evidence: ["evidence/KV-2/01-root-with-all-types.png"],
        });
        continue;
      }
      if (row.title !== type || row.glyph !== glyphExpected[type]) {
        addFinding({
          severity: "S2",
          title: "KV-2: glyph/title mismatch for " + type + " key",
          repro: 'redis TYPE ' + key + ' == "' + type + '"; read tree row .kind glyph+title',
          expected: 'glyph="' + glyphExpected[type] + '" title="' + type + '"',
          actual: 'glyph="' + row.glyph + '" title="' + row.title + '"',
          evidence: ["evidence/KV-2/01-root-with-all-types.png"],
        });
      }
    }

    // string -> blob view, exact value match
    await clickTreeNode(spa, K.string);
    await sleep(150);
    let bv = await blobViewState(spa, K.string);
    await evidence("02-string-blob");
    const engineStr = engines.redis(["GET", K.string]);
    if (bv.hasGrid || (bv.preText !== engineStr && bv.editorText !== engineStr)) {
      addFinding({
        severity: "S1",
        title: "KV-2: string key did not render as a blob leaf matching engine value",
        repro: "open " + K.string,
        expected: 'blob view, text === "' + engineStr + '"',
        actual: JSON.stringify(bv),
        evidence: ["evidence/KV-2/02-string-blob.png"],
        engine_truth: "redis GET " + K.string + " = " + JSON.stringify(engineStr),
      });
    }

    // hash -> grid [field,value], HGETALL truth
    await clickTreeNode(spa, K.hash);
    await sleep(150);
    let gd = await gridData(spa);
    await evidence("03-hash-grid");
    const hgetall = String(engines.redis(["HGETALL", K.hash]) || "").split("\n").filter(Boolean);
    const engineHash = {};
    for (let i = 0; i < hgetall.length; i += 2) engineHash[hgetall[i]] = hgetall[i + 1];
    const uiHash = {};
    for (const r of gd.rows) uiHash[r[0]] = r[1];
    // Header text carries a trailing " 🔑" badge for the PK column (field is
    // rowKeyCols[0] for a hash) -- match by prefix, not strict equality.
    if (JSON.stringify(uiHash) !== JSON.stringify(engineHash) || gd.heads[0].indexOf("field") !== 0 || gd.heads.indexOf("value") < 0) {
      addFinding({
        severity: "S1",
        title: "KV-2: hash grid content/columns mismatch vs HGETALL",
        repro: "open " + K.hash,
        expected: "columns include field,value; rows == " + JSON.stringify(engineHash),
        actual: "heads=" + JSON.stringify(gd.heads) + " rows(as map)=" + JSON.stringify(uiHash),
        evidence: ["evidence/KV-2/03-hash-grid.png"],
        engine_truth: "redis HGETALL " + K.hash + " = " + JSON.stringify(engineHash),
      });
    }

    // list -> grid [index,value], LRANGE truth, no row key (view-only)
    await clickTreeNode(spa, K.list);
    await sleep(150);
    gd = await gridData(spa);
    await evidence("04-list-grid");
    const lrange = String(engines.redis(["LRANGE", K.list, "0", "-1"]) || "").split("\n").filter(Boolean);
    const uiListVals = gd.rows.map((r) => r[1]);
    if (JSON.stringify(uiListVals) !== JSON.stringify(lrange)) {
      addFinding({
        severity: "S1",
        title: "KV-2: list grid content mismatch vs LRANGE",
        repro: "open " + K.list,
        expected: JSON.stringify(lrange),
        actual: JSON.stringify(uiListVals),
        evidence: ["evidence/KV-2/04-list-grid.png"],
        engine_truth: "redis LRANGE " + K.list + " 0 -1 = " + JSON.stringify(lrange),
      });
    }
    if (!gd.viewOnlyBadge || gd.viewOnlyBadge.indexOf("no row key") < 0) {
      addFinding({
        severity: "S3",
        title: "KV-2: list grid missing the expected view-only/no-row-key badge",
        repro: "open " + K.list + " (server sends RowKeyCols empty for lists)",
        expected: 'toolbar badge containing "no row key"',
        actual: 'viewOnlyBadge="' + gd.viewOnlyBadge + '"',
        evidence: ["evidence/KV-2/04-list-grid.png"],
      });
    }

    // set -> grid [member], SMEMBERS truth
    await clickTreeNode(spa, K.set);
    await sleep(150);
    gd = await gridData(spa);
    await evidence("05-set-grid");
    const smembers = String(engines.redis(["SMEMBERS", K.set]) || "").split("\n").filter(Boolean).sort();
    const uiSet = gd.rows.map((r) => r[0]).sort();
    if (JSON.stringify(uiSet) !== JSON.stringify(smembers)) {
      addFinding({
        severity: "S1",
        title: "KV-2: set grid content mismatch vs SMEMBERS",
        repro: "open " + K.set,
        expected: JSON.stringify(smembers),
        actual: JSON.stringify(uiSet),
        evidence: ["evidence/KV-2/05-set-grid.png"],
        engine_truth: "redis SMEMBERS " + K.set + " = " + JSON.stringify(smembers),
      });
    }

    // zset -> grid [member,score], ZRANGE WITHSCORES truth -- scores must match EXACTLY
    await clickTreeNode(spa, K.zset);
    await sleep(150);
    gd = await gridData(spa);
    await evidence("06-zset-grid");
    const zraw = String(engines.redis(["ZRANGE", K.zset, "0", "-1", "WITHSCORES"]) || "").split("\n").filter(Boolean);
    const engineZset = {};
    for (let i = 0; i < zraw.length; i += 2) engineZset[zraw[i]] = zraw[i + 1];
    const uiZset = {};
    for (const r of gd.rows) uiZset[r[0]] = r[1];
    if (JSON.stringify(uiZset) !== JSON.stringify(engineZset)) {
      addFinding({
        severity: "S1",
        title: "KV-2: zset grid scores mismatch vs ZRANGE WITHSCORES (exact-match check)",
        repro: "open " + K.zset,
        expected: JSON.stringify(engineZset),
        actual: JSON.stringify(uiZset),
        evidence: ["evidence/KV-2/06-zset-grid.png"],
        engine_truth: "redis ZRANGE " + K.zset + " 0 -1 WITHSCORES = " + JSON.stringify(engineZset),
      });
    }
  } finally {
    kvTeardown(engines);
  }
}

// ============================================================================
// KV-3 — add-key modal: styling regression, all 5 types, duplicate, edge inputs
// ============================================================================

async function runKV3(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  kvTeardown(engines);
  try {
    const spa = await openService(ctx, CACHE_SERVICE, true);
    const hintLink = await spa.evaluate(() => !!document.getElementById("createkeylink"));
    if (!hintLink) {
      addFinding({
        severity: "S1",
        title: "KV-3: #createkeylink not present after enabling write mode on cache",
        repro: "openService(cache, write=true)",
        expected: "#createkeylink present in the placeholder hint",
        actual: "absent",
        evidence: [await evidence("00-no-createkeylink")],
      });
      return;
    }
    await spa.click("#createkeylink");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    await evidence("01-add-key-modal"); // regression evidence for the UX lane: styled dark <select>, box-sized inputs

    const modalShape = await spa.evaluate(() => ({
      title: (document.getElementById("modaltitle") || {}).textContent,
      hasNameInput: !!document.getElementById("kvname"),
      hasTypeSelect: !!document.getElementById("kvtype"),
      hasValInput: !!document.getElementById("kvval"),
      typeOptions: Array.from((document.getElementById("kvtype") || { options: [] }).options || []).map((o) => o.value),
    }));
    if (!modalShape.hasNameInput || !modalShape.hasTypeSelect || !modalShape.hasValInput) {
      addFinding({
        severity: "S1",
        title: "KV-3: add-key modal missing an expected field",
        repro: "click #createkeylink",
        expected: "kvname + kvtype(select) + kvval all present",
        actual: JSON.stringify(modalShape),
        evidence: ["evidence/KV-3/01-add-key-modal.png"],
      });
    }
    await spa.click("#modalcancel"); // close the probe-only open, real per-type attempts follow

    // --- create one of each type, per app.js's createKeyForm wire shape ---
    // FIX regression check (A7, re-live-verified after the per-type kv-create
    // fix landed): #kvtype's onchange now re-renders #kvextra to the
    // type-specific fields kv.go's CreateKey actually requires -- hash gets
    // #kvfield(+optional #kvval), zset gets #kvfield(member)+#kvscore. Prior
    // to that fix this array hardcoded expectOK:false for hash/zset (the old
    // form only ever sent {path,type,value}, so both 400'd deterministically)
    // -- flipped to expectOK:true now that the extra fields exist, and each
    // attempt also verifies the TYPE-SPECIFIC payload round-tripped (not just
    // that *some* key of the right TYPE exists), so a create that silently
    // dropped the field/score would still be caught.
    const attempts = [
      {
        type: "string", name: "uitest_kv3_string", expectOK: true,
        fill: async () => setInput(spa, "#kvval", "sval"),
        verify: () => {
          const v = engines.redis(["GET", "uitest_kv3_string"]);
          return v === "sval" ? null : "GET uitest_kv3_string = " + JSON.stringify(v) + ", expected \"sval\"";
        },
      },
      {
        type: "hash", name: "uitest_kv3_hash", expectOK: true,
        fill: async () => { await setInput(spa, "#kvfield", "f1"); await setInput(spa, "#kvval", "hval"); },
        verify: () => {
          const v = engines.redis(["HGET", "uitest_kv3_hash", "f1"]);
          return v === "hval" ? null : "HGET uitest_kv3_hash f1 = " + JSON.stringify(v) + ", expected \"hval\"";
        },
      },
      {
        type: "list", name: "uitest_kv3_list", expectOK: true,
        fill: async () => setInput(spa, "#kvval", "lval"),
        verify: () => {
          const v = engines.redis(["LINDEX", "uitest_kv3_list", "0"]);
          return v === "lval" ? null : "LINDEX uitest_kv3_list 0 = " + JSON.stringify(v) + ", expected \"lval\"";
        },
      },
      {
        type: "set", name: "uitest_kv3_set", expectOK: true,
        fill: async () => setInput(spa, "#kvval", "setval"),
        verify: () => {
          const v = String(engines.redis(["SISMEMBER", "uitest_kv3_set", "setval"]) || "").trim();
          return v === "1" ? null : "SISMEMBER uitest_kv3_set setval = " + JSON.stringify(v) + ", expected \"1\"";
        },
      },
      {
        type: "zset", name: "uitest_kv3_zset", expectOK: true,
        fill: async () => { await setInput(spa, "#kvfield", "m1"); await setInput(spa, "#kvscore", "1.5"); },
        verify: () => {
          const v = parseFloat(engines.redis(["ZSCORE", "uitest_kv3_zset", "m1"]));
          return Math.abs(v - 1.5) <= 0.001 ? null : "ZSCORE uitest_kv3_zset m1 = " + v + ", expected 1.5";
        },
      },
    ];
    for (const a of attempts) {
      await spa.click("#createkeylink");
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
      await setInput(spa, "#kvname", a.name);
      await spa.select("#kvtype", a.type); // Puppeteer's select() dispatches a real 'change' event -- fires #kvtype's onchange, re-rendering #kvextra
      if (a.type === "hash" || a.type === "zset") {
        await spa.waitForSelector("#kvfield", { timeout: 5000 }).catch(() => {});
      }
      await a.fill();
      await spa.click("#modalok");
      const toast = await harness.waitToast(spa, 8000);
      await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
      await sleep(200);
      const engineType = String(engines.redis(["TYPE", a.name]) || "").trim();
      const created = engineType !== "none";
      const shot = await evidence("02-create-" + a.type);
      if (created !== a.expectOK) {
        addFinding({
          id: "KV-3-createkey-" + a.type + "-broken",
          severity: "S1",
          title: "KV-3: creating a " + a.type + " key via the Add-key modal " + (a.expectOK ? "failed but should have succeeded" : "unexpectedly succeeded"),
          repro: 'click #createkeylink; name="' + a.name + '"; type="' + a.type + '"; fill the type-specific fields; #modalok',
          expected: a.expectOK ? "key created (TYPE == " + a.type + ")" : "refused",
          actual: "toast=" + JSON.stringify(toast) + "; engine TYPE=" + engineType,
          evidence: [shot],
          engine_truth: "redis TYPE " + a.name + " = " + engineType,
        });
      } else if (a.expectOK && created) {
        const mismatch = a.verify();
        if (mismatch) {
          addFinding({
            id: "KV-3-createkey-" + a.type + "-payload-mismatch",
            severity: "S1",
            title: "KV-3: " + a.type + " key was created via the Add-key modal but its type-specific payload did not round-trip",
            repro: 'click #createkeylink; name="' + a.name + '"; type="' + a.type + '"; fill the type-specific fields; #modalok',
            expected: "the field/value (hash), first value (string/list/set), or member+score (zset) round-trips exactly",
            actual: "toast=" + JSON.stringify(toast) + "; " + mismatch,
            evidence: [shot],
            engine_truth: mismatch,
          });
        }
      }
      await sleep(300); // let this toast fully clear before the next mutation (gotcha #8)
    }

    // --- duplicate name conflict (reuse the string key created above) ---
    await spa.click("#createkeylink");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    await setInput(spa, "#kvname", "uitest_kv3_string");
    await spa.select("#kvtype", "string");
    await setInput(spa, "#kvval", "clobber-attempt");
    await spa.click("#modalok");
    // Round-2 modal contract: a rejection keeps the modal OPEN with the typed
    // input intact and renders the conflict INLINE (#modalerr) — no toast, no
    // auto-close (spec §7.4).
    let dupErr = "";
    try {
      await spa.waitForSelector("#modalerr", { timeout: 8000 });
      dupErr = await spa.evaluate(() => (document.getElementById("modalerr") || {}).textContent || "");
    } catch (_) { /* absent — recorded below */ }
    const dupStillOpen = await spa.evaluate(() => !document.getElementById("modal").classList.contains("hidden"));
    const dupNameKept = await spa.evaluate(() => (document.getElementById("kvname") || {}).value || "");
    await evidence("03-duplicate-key-conflict");
    const afterDupVal = engines.redis(["GET", "uitest_kv3_string"]);
    if (afterDupVal !== "sval") {
      addFinding({
        severity: "S1",
        title: "KV-3: duplicate-create changed the engine value (silent clobber or dishonest rejection)",
        repro: 'create "uitest_kv3_string" again with a different value',
        expected: 'refused; value stays "sval"',
        actual: "inline err=" + JSON.stringify(dupErr) + "; value now=" + JSON.stringify(afterDupVal),
        evidence: ["evidence/KV-3/03-duplicate-key-conflict.png"],
        engine_truth: "redis GET uitest_kv3_string = " + JSON.stringify(afterDupVal),
      });
    }
    if (!dupStillOpen || !dupErr || dupNameKept !== "uitest_kv3_string") {
      addFinding({
        severity: "S2",
        title: "KV-3: duplicate-create rejection did not keep the modal open with the typed input + inline error (round-2 modal contract)",
        repro: "create an already-existing key name; confirm",
        expected: "modal stays open, #modalerr rendered, #kvname keeps the typed name",
        actual: "open=" + dupStillOpen + "; err=" + JSON.stringify(dupErr) + "; name=" + JSON.stringify(dupNameKept),
        evidence: ["evidence/KV-3/03-duplicate-key-conflict.png"],
      });
    } else if (dupErr.indexOf("Already exists") < 0) {
      addFinding({
        severity: dupErr.indexOf("reload and retry") >= 0 ? "S3" : "S2",
        title: "KV-3: duplicate-key conflict wording is not the action-aware create-collision message",
        repro: "create an already-existing key name",
        expected: 'inline error contains "Already exists"',
        actual: 'inline err="' + dupErr + '"',
        evidence: ["evidence/KV-3/03-duplicate-key-conflict.png"],
      });
    }
    // Close the rejected modal before the next sub-test (it stays open by design).
    await spa.click("#modalcancel");
    await spa.waitForSelector("#modal.hidden", { timeout: 5000 }).catch(() => {});
    await sleep(200);

    // --- edge inputs: empty value, unicode key, key with spaces ---
    const edgeCases = [
      { name: "uitest_kv3_empty", val: "", label: "empty value" },
      { name: "uitest_kv3_žluť", val: "unicode-key-val", label: "unicode key" },
      { name: "uitest_kv3 with spaces", val: "spaced-key-val", label: "key with spaces" },
    ];
    for (const e of edgeCases) {
      await spa.click("#createkeylink");
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
      await setInput(spa, "#kvname", e.name);
      await spa.select("#kvtype", "string");
      await setInput(spa, "#kvval", e.val);
      await spa.click("#modalok");
      const toast = await harness.waitToast(spa, 8000);
      await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
      await sleep(250);
      const exists = String(engines.redis(["EXISTS", e.name]) || "").trim() === "1";
      const shot = await evidence("04-edge-" + e.label.replace(/\s+/g, "-"));
      if (!exists) {
        addFinding({
          severity: "S2",
          title: "KV-3 edge input (" + e.label + "): key was not created",
          repro: 'create key name=' + JSON.stringify(e.name) + " value=" + JSON.stringify(e.val),
          expected: "key created (redis keys are binary-safe; empty string values are valid)",
          actual: "toast=" + JSON.stringify(toast) + "; EXISTS=0",
          evidence: [shot],
          engine_truth: "redis EXISTS " + e.name + " = 0",
        });
      }
      await sleep(300);
    }
  } finally {
    kvTeardown(engines);
  }
}

// ============================================================================
// KV-4 — edit (blob + grid cell) + TTL
// ============================================================================

async function runKV4(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  kvTeardown(engines);
  const STR_KEY = "uitest_kv4_str";
  const HASH_KEY = "uitest_kv4_hash";
  try {
    engines.redis(["SET", STR_KEY, "original"]);
    engines.redis(["HSET", HASH_KEY, "f1", "v1"]);

    const spa = await openService(ctx, CACHE_SERVICE, true);

    // --- blob edit ---
    await clickTreeNode(spa, STR_KEY);
    await spa.waitForSelector("#blobedit", { timeout: 15000 }).catch(() => null);
    const hasEditor = await spa.evaluate(() => !!document.getElementById("blobedit"));
    await evidence("01-string-editor-open");
    if (!hasEditor) {
      addFinding({
        severity: "S1",
        title: "KV-4: string blob is not editable in write mode (no #blobedit textarea)",
        repro: "write mode on; open " + STR_KEY,
        expected: "#blobedit present (small string, well under EDIT_CAP)",
        actual: "absent",
        evidence: ["evidence/KV-4/01-string-editor-open.png"],
      });
    } else {
      await spa.click("#blobedit", { clickCount: 3 });
      await spa.type("#blobedit", "edited-value");
      await spa.click("#saveblob");
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
      await evidence("02-save-confirm-modal");
      await spa.click("#modalok");
      const toast = await harness.waitToast(spa);
      await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
      await sleep(200);
      const engineVal = engines.redis(["GET", STR_KEY]);
      await evidence("03-after-save");
      if (engineVal !== "edited-value") {
        addFinding({
          id: "KV-4-blob-save-scope",
          severity: "S1",
          title: "KV-4: blob save (PUT /api/blob) did not apply the new value",
          repro: 'open ' + STR_KEY + '; edit #blobedit to "edited-value"; #saveblob; confirm #modalok',
          expected: 'redis GET ' + STR_KEY + ' == "edited-value"',
          actual: "toast=" + JSON.stringify(toast) + "; engine value=" + JSON.stringify(engineVal),
          evidence: ["evidence/KV-4/02-save-confirm-modal.png", "evidence/KV-4/03-after-save.png"],
          engine_truth: "redis GET " + STR_KEY + " = " + JSON.stringify(engineVal),
        });
      }
    }

    // --- hash grid cell edit: value editable, field locked ---
    await clickTreeNode(spa, HASH_KEY);
    await sleep(150);
    let gd = await gridData(spa);
    await evidence("04-hash-grid-open");
    const fieldLocked = await spa.evaluate(() => {
      const td = document.querySelector("table.grid tbody tr td");
      return td ? !td.classList.contains("editable") : null;
    });
    if (fieldLocked !== true) {
      addFinding({
        severity: "S2",
        title: "KV-4: hash grid's field (key) column is editable when it should be locked",
        repro: "open " + HASH_KEY + " in write mode, inspect first td.editable",
        expected: "field column NOT editable (has a sibling 'value' column -- entryEditPlan 'locked')",
        actual: "fieldLocked=" + fieldLocked,
        evidence: ["evidence/KV-4/04-hash-grid-open.png"],
      });
    }
    const valueCellEditable = await spa.evaluate(() => {
      const tds = document.querySelectorAll("table.grid tbody tr td");
      return tds[1] ? tds[1].classList.contains("editable") : null;
    });
    if (valueCellEditable !== true) {
      addFinding({
        severity: "S1",
        title: "KV-4: hash grid's value column is not editable in write mode",
        repro: "open " + HASH_KEY + " in write mode",
        expected: "value column editable",
        actual: "valueCellEditable=" + valueCellEditable,
        evidence: ["evidence/KV-4/04-hash-grid-open.png"],
      });
    } else {
      await spa.click("table.grid tbody tr td.editable");
      await spa.waitForSelector(".celledit", { timeout: 5000 });
      // editCell() already calls input.focus() when it swaps the <td> for
      // this <input>; a synthetic triple-click on top of that didn't reliably
      // select-all in this environment (caught live: typing after it APPENDED
      // instead of replacing, "v1" + "v1-edited" = "v1v1-edited" in the
      // engine). Select via the DOM API directly instead of relying on a
      // simulated multi-click gesture.
      await spa.evaluate(() => {
        const el = document.querySelector(".celledit");
        if (el) { el.focus(); el.select(); }
      });
      await spa.type(".celledit", "v1-edited");
      await blurActive(spa);
      const toast = await harness.waitToast(spa);
      await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
      await sleep(200);
      const engineHVal = engines.redis(["HGET", HASH_KEY, "f1"]);
      await evidence("05-hash-cell-edited");
      if (engineHVal !== "v1-edited") {
        addFinding({
          severity: "S1",
          title: "KV-4: hash field cell edit (PUT /api/entry) did not apply",
          repro: "open " + HASH_KEY + "; edit value cell for field f1 to 'v1-edited'; blur",
          expected: 'redis HGET ' + HASH_KEY + ' f1 == "v1-edited"',
          actual: "toast=" + JSON.stringify(toast) + "; engine=" + JSON.stringify(engineHVal),
          evidence: ["evidence/KV-4/05-hash-cell-edited.png"],
          engine_truth: "redis HGET " + HASH_KEY + " f1 = " + JSON.stringify(engineHVal),
        });
      }
    }

    // --- TTL set + clear on the string key ---
    await clickTreeNode(spa, STR_KEY);
    await sleep(150);
    const hasTTLBar = await spa.evaluate(() => !!document.querySelector(".ttlbar"));
    await evidence("06-ttlbar");
    if (!hasTTLBar) {
      addFinding({
        severity: "S2",
        title: "KV-4: .ttlbar not rendered for a KV string in write mode",
        repro: "open " + STR_KEY + " with write mode on",
        expected: ".ttlbar present with Set TTL / Persist buttons",
        actual: "absent",
        evidence: ["evidence/KV-4/06-ttlbar.png"],
      });
    } else {
      // #setttl is a CHAINED two-modal flow: promptModal (enter seconds) ->
      // its onValue synchronously calls confirmAction (a SECOND, different
      // #modal content: "Set TTL 120s on ...? EXPIRE 120s") -- #modalok must
      // be clicked TWICE. Caught live: clicking it only once left the confirm
      // dialog sitting open (visible in evidence/KV-4/07-ttl-set.png) while
      // the harness moved on and read a stale TTL of -1, nothing to do with
      // the product actually failing to set it.
      await spa.click("#setttl");
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
      await setInput(spa, "#modalinput", "120");
      await spa.click("#modalok"); // submits the prompt value -> chains into the confirm modal
      await spa.waitForFunction(
        () => {
          const t = document.getElementById("modaltitle");
          return !!t && t.textContent.indexOf("Set TTL 120s") === 0;
        },
        { timeout: 5000 }
      );
      await evidence("07-ttl-set-confirm-modal");
      await spa.click("#modalok"); // now actually confirms EXPIRE 120s
      await harness.waitToast(spa);
      await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
      await sleep(200);
      const ttl1 = parseInt(engines.redis(["TTL", STR_KEY]), 10);
      await evidence("07-ttl-set");
      if (!(ttl1 > 0 && ttl1 <= 120)) {
        addFinding({
          severity: "S1",
          title: "KV-4: Set TTL did not apply the expected range",
          repro: "open " + STR_KEY + "; Set TTL -> 120",
          expected: "0 < TTL <= 120",
          actual: "redis TTL = " + ttl1,
          evidence: ["evidence/KV-4/07-ttl-set.png"],
          engine_truth: "redis TTL " + STR_KEY + " = " + ttl1,
        });
      }
      await sleep(300);
      await spa.click("#clrttl");
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
      await spa.click("#modalok");
      await harness.waitToast(spa);
      await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
      await sleep(200);
      const ttl2 = parseInt(engines.redis(["TTL", STR_KEY]), 10);
      await evidence("08-ttl-cleared");
      if (ttl2 !== -1) {
        addFinding({
          severity: "S1",
          title: "KV-4: Persist (clear TTL) did not clear the expiry",
          repro: "open " + STR_KEY + "; Persist",
          expected: "redis TTL == -1 (no expiry)",
          actual: "redis TTL = " + ttl2,
          evidence: ["evidence/KV-4/08-ttl-cleared.png"],
          engine_truth: "redis TTL " + STR_KEY + " = " + ttl2,
        });
      }
    }
  } finally {
    kvTeardown(engines);
  }
}

// ============================================================================
// KV-5 — delete: flat, namespaced, grid-type (KI-1 scope)
// ============================================================================

async function runKV5(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  kvTeardown(engines);
  const FLAT_KEY = "uitest_kv5_flat";
  const NS_KEY = "uitest:kv5ns:leaf";
  const HASH_KEY = "uitest_kv5_hash";
  try {
    engines.redis(["SET", FLAT_KEY, "v"]);
    engines.redis(["SET", NS_KEY, "v"]);
    engines.redis(["HSET", HASH_KEY, "f1", "v1", "f2", "v2"]);

    const spa = await openService(ctx, CACHE_SERVICE, true);

    // --- 1. flat key delete (KI-1 baseline reproduction) ---
    await clickTreeNode(spa, FLAT_KEY);
    await sleep(150);
    await spa.waitForSelector("#delblob", { timeout: 15000 });
    await spa.click("#delblob");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    await evidence("01-flat-delete-modal");
    await spa.click("#modalok");
    const toast1 = await harness.waitToast(spa);
    await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
    await sleep(250);
    const stillInTree1 = await treeHasNode(spa, FLAT_KEY);
    const exists1 = String(engines.redis(["EXISTS", FLAT_KEY]) || "").trim();
    await evidence("02-flat-delete-result");
    const toastGood1 = !!toast1 && toast1.kind === "good";
    if (toastGood1 && exists1 !== "0") {
      addFinding({
        id: "KI-1-scope-flat-success-lie",
        severity: "S1",
        title: "KI-1 scope: flat key delete claimed success but key still EXISTS (success-lie)",
        repro: "redis SET " + FLAT_KEY + " v (flat, no ':'); open; #delblob; confirm",
        expected: "EXISTS 0",
        actual: "toast=" + JSON.stringify(toast1) + "; EXISTS=" + exists1,
        evidence: ["evidence/KV-5/01-flat-delete-modal.png", "evidence/KV-5/02-flat-delete-result.png"],
        engine_truth: "redis EXISTS " + FLAT_KEY + " = " + exists1,
      });
    } else if (!toastGood1) {
      addFinding({
        id: "KI-1-scope-flat-delete-rejected",
        severity: "S1",
        title: 'KI-1 scope: flat top-level KV string key delete fails outright (toast: "' + (toast1 ? toast1.text : "none") + '")',
        repro: "redis SET " + FLAT_KEY + " v; cache: open; #delblob; confirm #modalok",
        expected: "200 {ok:true}; toast 'Deleted.'; key removed from tree + engine",
        actual: (toast1 ? JSON.stringify(toast1) : "no toast") + "; stillInTree=" + stillInTree1 + "; EXISTS=" + exists1,
        evidence: ["evidence/KV-5/01-flat-delete-modal.png", "evidence/KV-5/02-flat-delete-result.png"],
        engine_truth: "redis EXISTS " + FLAT_KEY + " = " + exists1 + " (matches KI-1/CORE-1-S1-delete-rejected)",
      });
      addFinding({
        id: "KI-1-diagnostic-server-side-proven-correct",
        severity: "S2",
        title: "KI-1 diagnostic: direct HTTP replay of the EXACT same DELETE /api/node body against a fresh write-capable console process succeeds (200 {ok:true})",
        repro:
          "SSH: zcp studio console serve --port 0 --allow-writes (fresh instance); " +
          "GET /api/tree?service=cache&segs=[] with the session bearer to obtain the real node.path JSON; " +
          "curl -X DELETE /api/node with that exact {path:{service,segments}} body + X-Write-Token + X-Confirm: true",
        expected: "n/a -- scoping data for the fix loop",
        actual: "raw HTTP replay returned 200 {\"ok\":true} and the key was actually removed (EXISTS 0 confirmed) -- " +
          "the Go server's decode/enrichRouteContext/ProviderFor/Delete path is correct for this exact request shape. " +
          "The bug is therefore isolated to the EMBEDDED session path specifically (webview -> vscodeApi.postMessage " +
          "'dc-rpc' -> consolePanel.js onDidReceiveMessage -> consoleClient.js http.request), most plausibly something " +
          "stateful tied to a long-lived/reused console child process or broker (this diagnostic used a FRESH process; " +
          "CORE-1/KV-5's reproduction used the long-running one consoleSession.js reuses across the whole test day) -- " +
          "or a stale webview generation (retainContextWhenHidden:true panels keep whatever app.js/consoleClient.js was " +
          "loaded at panel-creation time; a reveal does not re-set panel.webview.html). Recommend the fix loop instrument " +
          "consoleClient.js's actual outgoing request (method/path/headers/body) on the live long-running process rather " +
          "than re-deriving it from source, since source-level reading here provably matches a WORKING direct-HTTP case.",
        evidence: [],
      });
    }

    // --- 2. namespaced key delete ---
    await harness.clickService(spa, CACHE_SERVICE);
    await sleep(200);
    await clickTreeNode(spa, "uitest"); // synthetic container from the colon prefix
    await sleep(250);
    await clickTreeNode(spa, "kv5ns");
    await sleep(250);
    const nsShot = await evidence("03-ns-container-expanded");
    const nsOpened = await clickTreeNode(spa, "leaf");
    if (!nsOpened) {
      addFinding({
        severity: "S2",
        title: "KV-5: could not navigate to the namespaced leaf via the tree (uitest -> kv5ns -> leaf)",
        repro: "redis SET " + NS_KEY + " v; expand uitest -> kv5ns",
        expected: 'a leaf named "leaf" reachable',
        actual: "not found after expansion",
        evidence: [nsShot],
      });
    } else {
      await sleep(150);
      await spa.waitForSelector("#delblob", { timeout: 15000 });
      await spa.click("#delblob");
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
      await evidence("04-ns-delete-modal");
      await spa.click("#modalok");
      const toast2 = await harness.waitToast(spa);
      await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
      await sleep(250);
      const exists2 = String(engines.redis(["EXISTS", NS_KEY]) || "").trim();
      await evidence("05-ns-delete-result");
      const toastGood2 = !!toast2 && toast2.kind === "good";
      if (toastGood2 !== (exists2 === "0")) {
        addFinding({
          id: toastGood2 ? "KI-1-scope-namespaced-success-lie" : "KI-1-scope-namespaced-delete-rejected",
          severity: "S1",
          title: "KI-1 scope: namespaced key (\":\" segments) delete " + (toastGood2 ? "claimed success but engine disagrees" : "was rejected/inconsistent"),
          repro: "redis SET " + NS_KEY + " v; navigate uitest -> kv5ns -> leaf; #delblob; confirm",
          expected: "toast success iff EXISTS 0",
          actual: "toast=" + JSON.stringify(toast2) + "; EXISTS=" + exists2,
          evidence: ["evidence/KV-5/04-ns-delete-modal.png", "evidence/KV-5/05-ns-delete-result.png"],
          engine_truth: "redis EXISTS " + NS_KEY + " = " + exists2,
        });
      } else {
        // FIX regression check (A7, re-live-verified after KI-1's
        // Content-Length fix landed): the flat-key delete above is now
        // expected to succeed too, so "namespaced succeeds" is no longer a
        // divergence from the flat case -- both should agree. Wording kept
        // dynamic (not hardcoded to one outcome) so a future regression on
        // either path still reads honestly instead of contradicting itself.
        addFinding({
          id: "KI-1-scope-namespaced-" + (toastGood2 ? "delete-works" : "delete-fails"),
          severity: toastGood2 ? "S3" : "S1",
          title: "KI-1 scope answer: namespaced key delete " + (toastGood2 ? "succeeds (matches the flat-key case, both fixed)" : "FAILS (the flat-key case above should be checked for the same regression)"),
          repro: "redis SET " + NS_KEY + " v; navigate uitest -> kv5ns -> leaf; #delblob; confirm",
          expected: "toast success and EXISTS 0, matching the flat-key delete's outcome",
          actual: "toast=" + JSON.stringify(toast2) + "; EXISTS=" + exists2,
          evidence: ["evidence/KV-5/04-ns-delete-modal.png", "evidence/KV-5/05-ns-delete-result.png"],
          engine_truth: "redis EXISTS " + NS_KEY + " = " + exists2,
          status: toastGood2 ? "info" : "new",
        });
      }
    }

    // --- 3. grid-type (hash) key: is there a whole-key delete affordance at all? ---
    await harness.clickService(spa, CACHE_SERVICE);
    await sleep(200);
    await clickTreeNode(spa, HASH_KEY);
    await sleep(150);
    const hashToolbar = await toolbarButtons(spa);
    const hashHasWholeKeyDelete = hashToolbar.some((b) => String(b).toLowerCase().indexOf("delete") >= 0 || b === "delblob");
    const hashShot = await evidence("06-hash-opened-as-table");
    addFinding({
      id: "KI-1-scope-grid-type-no-wholekey-delete-affordance",
      severity: "S2",
      title: "KI-1 scope answer: a hash/list/set/zset key has NO whole-key delete affordance at all (openTable's toolbar never wires deleteNode)",
      repro: "open a hash key (renders via openTable, not openBlob)",
      expected: "n/a -- documenting: renderGrid()'s toolbar only ever offers Insert row; deleteNode()/#delblob is wired exclusively from openBlob() (kind:'blob' leaves), which a collection never is. The KI-1 400 (DELETE /api/node) is therefore UNREACHABLE for a whole hash/list/set/zset key via the UI -- only per-ENTRY deletion (DELETE /api/entry, a different handler) is offered, via each row's ✕ button.",
      actual: "toolbar buttons for opened hash key: " + JSON.stringify(hashToolbar) + "; hasWholeKeyDelete=" + hashHasWholeKeyDelete,
      evidence: [hashShot],
      status: "info",
    });

    // Exercise the per-ENTRY delete that DOES exist for hash (different code
    // path than KI-1's /api/node -- a related, still-useful data point).
    const gd = await gridData(spa);
    if (gd.rows.length > 0) {
      await spa.waitForSelector("button.rowdel", { timeout: 5000 }).catch(() => null);
      const rowDelPresent = await spa.evaluate(() => !!document.querySelector("button.rowdel:not([disabled])"));
      if (rowDelPresent) {
        await spa.click("table.grid tbody tr button.rowdel");
        await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
        await evidence("07-entry-delete-modal");
        await spa.click("#modalok");
        const toast3 = await harness.waitToast(spa);
        await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
        await sleep(250);
        const hexists = String(engines.redis(["HEXISTS", HASH_KEY, "f1"]) || "").trim();
        await evidence("08-entry-delete-result");
        if (!!toast3 && toast3.kind === "good" && hexists !== "0") {
          addFinding({
            severity: "S1",
            title: "KV-5: hash entry delete (DELETE /api/entry) claimed success but field still exists",
            repro: "open " + HASH_KEY + "; delete row f1 via button.rowdel; confirm",
            expected: "redis HEXISTS " + HASH_KEY + " f1 == 0",
            actual: "toast=" + JSON.stringify(toast3) + "; HEXISTS=" + hexists,
            evidence: ["evidence/KV-5/07-entry-delete-modal.png", "evidence/KV-5/08-entry-delete-result.png"],
            engine_truth: "redis HEXISTS " + HASH_KEY + " f1 = " + hexists,
          });
        } else {
          addFinding({
            id: "KI-1-scope-entry-delete-" + (hexists === "0" ? "works" : "also-broken"),
            severity: hexists === "0" ? "S3" : "S1",
            title: "KI-1 scope: per-entry delete (/api/entry, DIFFERENT handler than KI-1's /api/node) " + (hexists === "0" ? "works correctly" : "is ALSO broken"),
            repro: "open " + HASH_KEY + "; delete row f1 via button.rowdel; confirm",
            expected: "n/a -- scope data point",
            actual: "toast=" + JSON.stringify(toast3) + "; HEXISTS=" + hexists,
            evidence: ["evidence/KV-5/07-entry-delete-modal.png", "evidence/KV-5/08-entry-delete-result.png"],
            engine_truth: "redis HEXISTS " + HASH_KEY + " f1 = " + hexists,
            status: "info",
          });
        }
      }
    }
  } finally {
    kvTeardown(engines);
  }
}

// ============================================================================
// KV-6 — edge values: 100KB string, ANSI/control chars, binary
// ============================================================================

async function runKV6(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  kvTeardown(engines);
  const BIG_KEY = "uitest_kv6_100kb";
  const ANSI_KEY = "uitest_kv6_ansi";
  const BIN_KEY = "uitest_kv6_binary";
  try {
    const bigValue = "abcdefghij".repeat(10000); // exactly 100,000 bytes
    engines.redis(["SET", BIG_KEY, bigValue]);
    const ansiValue = "[31mred[0m\tworld\ncontrol:bell";
    engines.redis(["SET", ANSI_KEY, ansiValue]);
    // Binary value: redis-cli's plain argv doesn't interpret \x escapes; use
    // -x (read value from stdin, binary-safe) via a raw printf pipe.
    const cfg = loadConfig();
    const { shellQuote } = require("../lib/engines");
    const binCmd =
      "printf '\\x00\\x01\\x02binary\\xff' | redis-cli -h " + shellQuote(cfg.DC_REDIS_HOST) +
      " -a " + shellQuote(cfg.DC_REDIS_PASSWORD) + " --no-auth-warning -x SET " + shellQuote(BIN_KEY);
    engines.container(binCmd);

    const spa = await openService(ctx, CACHE_SERVICE, false);

    // 100KB string
    await clickTreeNode(spa, BIG_KEY);
    await sleep(200);
    let bv = await blobViewState(spa, BIG_KEY);
    await evidence("01-100kb-string");
    const rendered = bv.preText || bv.editorText || "";
    if (rendered.length !== bigValue.length) {
      addFinding({
        severity: "S2",
        title: "KV-6: 100KB string did not render at full length",
        repro: "redis SET " + BIG_KEY + " <100000 bytes>; open",
        expected: "rendered length == 100000 (under both DISPLAY_CAP 1MiB and EDIT_CAP 512KiB)",
        actual: "rendered length=" + rendered.length + "; meta=" + bv.metaText,
        evidence: ["evidence/KV-6/01-100kb-string.png"],
      });
    }

    // ANSI/control chars
    await clickTreeNode(spa, ANSI_KEY);
    await sleep(200);
    bv = await blobViewState(spa, ANSI_KEY);
    await evidence("02-ansi-control-chars");
    const ansiRendered = bv.preText != null ? bv.preText : bv.editorText;
    addFinding({
      id: "KV-6-ansi-control-chars",
      severity: ansiRendered === ansiValue ? "S3" : "S2",
      title: "KV-6: ANSI/control-char value rendering (" + (ansiRendered === ansiValue ? "renders as raw textContent, no visible breakage" : "differs from source value") + ")",
      repro: "redis SET " + ANSI_KEY + " '\\x1b[31mred\\x1b[0m\\tworld\\ncontrol:\\x07bell'; open",
      expected: "n/a -- recording actual rendering (textContent-based, so no HTML/script injection risk either way)",
      actual: "rendered===source: " + (ansiRendered === ansiValue) + "; renderedLength=" + (ansiRendered || "").length + " sourceLength=" + ansiValue.length,
      evidence: ["evidence/KV-6/02-ansi-control-chars.png"],
      status: "info",
    });

    // Binary value -- kv ReadBlob always reports contentType "text/plain" for
    // KV strings (kv.go:225), so isTextual() is always true here even for
    // genuinely non-UTF8 bytes -- TextDecoder (fatal:false default) should
    // substitute U+FFFD rather than crash. Confirming no breakage, not a
    // byte-exact round trip (SSH text round-trip isn't a reliable oracle for
    // that; STRLEN is).
    await clickTreeNode(spa, BIN_KEY);
    await sleep(200);
    bv = await blobViewState(spa, BIN_KEY);
    await evidence("03-binary-value");
    const strlen = engines.redis(["STRLEN", BIN_KEY]);
    const contentBroke = bv.preText == null && bv.editorText == null && !bv.placeholderText;
    if (contentBroke) {
      addFinding({
        severity: "S1",
        title: "KV-6: binary-ish value broke the blob view (no pre/editor/placeholder rendered)",
        repro: "redis -x SET " + BIN_KEY + " '\\x00\\x01\\x02binary\\xff'; open",
        expected: "renders (possibly with replacement chars) OR an honest placeholder -- never a blank pane",
        actual: JSON.stringify(bv),
        evidence: ["evidence/KV-6/03-binary-value.png"],
        engine_truth: "redis STRLEN " + BIN_KEY + " = " + strlen,
      });
    } else {
      addFinding({
        id: "KV-6-binary-value-renders",
        severity: "S3",
        title: "KV-6: binary value with null/high bytes renders without breaking the UI (kv ReadBlob always reports text/plain, so it's decoded as UTF-8 with replacement chars)",
        repro: "redis -x SET " + BIN_KEY + " '\\x00\\x01\\x02binary\\xff'; open",
        expected: "n/a -- informational",
        actual: "preText/editorText present, length=" + ((bv.preText || bv.editorText || "").length) + "; STRLEN=" + strlen,
        evidence: ["evidence/KV-6/03-binary-value.png"],
        status: "info",
      });
    }
  } finally {
    kvTeardown(engines);
  }
}

// ============================================================================
// OBJ-1 — tree
// ============================================================================

async function runOBJ1(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  await objTeardown();
  try {
    await s3("PUT", OBJ_PREFIX + "uitest-readme.txt", { body: Buffer.from("hello from uitest OBJ-1\n"), contentType: "text/plain" });
    await s3("PUT", OBJ_PREFIX + "pic.png", { body: Buffer.from(TINY_PNG_B64, "base64"), contentType: "image/png" });
    await s3("PUT", OBJ_PREFIX + "sub/nested.txt", { body: Buffer.from("nested file\n"), contentType: "text/plain" });

    const s3Truth = await s3ListAll(OBJ_PREFIX);

    const spa = await harness.openConsole(page, OBJECT_SERVICE);
    await harness.setWriteMode(page, spa, false);
    await sleep(200);
    const rootNames = await treeNodeNames(spa);
    await evidence("01-bucket-root");

    if (!rootNames.includes("uitest")) {
      addFinding({
        severity: "S1",
        title: 'OBJ-1: no "uitest" folder visible at the object-storage tree root',
        repro: "PUT uitest/uitest-readme.txt etc via S3; openConsole(storage)",
        expected: '"uitest" container present at root',
        actual: "root names: " + JSON.stringify(rootNames),
        evidence: ["evidence/OBJ-1/01-bucket-root.png"],
      });
      return;
    }
    // Per object.go's List(): the bucket itself has no separate tree level
    // (one bucket per service, path starts empty) -- so the observed shape is
    // "root -> uitest folder -> objects", not "root -> bucket -> uitest ->
    // objects" as the brief's phrasing suggested. Recording the actual shape.
    addFinding({
      id: "OBJ-1-tree-shape",
      severity: "S3",
      title: 'OBJ-1: tree shape is root -> "uitest" folder -> objects (bucket level is implicit, one bucket per service -- there is no separate "bucket" tree node)',
      repro: "openConsole(storage); read #tree root",
      expected: "n/a -- informational, corrects the brief's assumed 3-level shape",
      actual: "root names: " + JSON.stringify(rootNames),
      evidence: ["evidence/OBJ-1/01-bucket-root.png"],
      status: "info",
    });

    await clickTreeNode(spa, "uitest");
    await sleep(250);
    const lvl2 = (await childrenInfo(spa, "uitest")) || [];
    await evidence("02-uitest-expanded");
    const lvl2Names = lvl2.map((n) => n.name);
    for (const expected of ["uitest-readme.txt", "pic.png", "sub"]) {
      if (!lvl2Names.includes(expected)) {
        addFinding({
          severity: "S1",
          title: 'OBJ-1: "' + expected + '" not visible inside the uitest folder',
          repro: "expand uitest",
          expected: expected + " present",
          actual: "level-2 names: " + JSON.stringify(lvl2Names),
          evidence: ["evidence/OBJ-1/02-uitest-expanded.png"],
        });
      }
    }
    // sizes honest vs S3 LIST truth
    const sizeTruth = {};
    for (const o of s3Truth) sizeTruth[o.key.replace(OBJ_PREFIX, "")] = o.size;
    const uiSizes = await spa.evaluate(() => {
      const out = {};
      document.querySelectorAll("#tree .node-wrap > .node").forEach((row) => {
        const nm = row.querySelector(".nname");
        const meta = row.querySelector(".nmeta");
        if (nm && meta) out[nm.textContent] = meta.textContent;
      });
      return out;
    });
    for (const [name, size] of Object.entries(sizeTruth)) {
      if (name.indexOf("/") >= 0) continue; // nested.txt lives one level deeper
      const shown = uiSizes[name];
      if (shown == null) continue; // container rows carry no size chip
      // human() renders "<n> B"/"<n.d> KB" -- just confirm a byte figure for
      // small files is present and not wildly off, not a strict format match.
      if (size < 1024 && shown.indexOf(" B") < 0) {
        addFinding({
          severity: "S3",
          title: "OBJ-1: size chip for " + name + " does not look byte-accurate",
          repro: "compare tree .nmeta chip vs S3 LIST Size",
          expected: size + " B (approx)",
          actual: 'chip="' + shown + '"',
          evidence: ["evidence/OBJ-1/02-uitest-expanded.png"],
        });
      }
    }

    await clickTreeNode(spa, "sub");
    await sleep(250);
    const lvl3Names = await treeNodeNames(spa);
    await evidence("03-sub-expanded");
    if (!lvl3Names.includes("nested.txt")) {
      addFinding({
        severity: "S1",
        title: 'OBJ-1: "nested.txt" not visible inside uitest/sub',
        repro: "expand uitest -> sub",
        expected: "nested.txt present",
        actual: "level-3 names: " + JSON.stringify(lvl3Names),
        evidence: ["evidence/OBJ-1/03-sub-expanded.png"],
      });
    }
  } finally {
    await objTeardown();
  }
}

// ============================================================================
// OBJ-2 — previews (text / image / non-previewable binary)
// ============================================================================

async function runOBJ2(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  await objTeardown();
  try {
    const textContent = "hello preview text\n";
    await s3("PUT", OBJ_PREFIX + "uitest-readme.txt", { body: Buffer.from(textContent), contentType: "text/plain" });
    await s3("PUT", OBJ_PREFIX + "pic.png", { body: Buffer.from(TINY_PNG_B64, "base64"), contentType: "image/png" });
    await s3("PUT", OBJ_PREFIX + "blob.bin", { body: Buffer.from([0, 1, 2, 3, 255, 254, 253, 0, 10, 13]), contentType: "application/octet-stream" });

    const spa = await harness.openConsole(page, OBJECT_SERVICE);
    await harness.setWriteMode(page, spa, false);
    await sleep(200);
    await clickTreeNode(spa, "uitest");
    await sleep(250);

    // text preview
    await clickTreeNode(spa, "uitest-readme.txt");
    await sleep(250);
    let bv = await blobViewState(spa, "uitest-readme.txt");
    await evidence("01-text-preview");
    if ((bv.preText || bv.editorText) !== textContent.trimEnd() && (bv.preText || "").trim() !== textContent.trim()) {
      addFinding({
        severity: "S2",
        title: "OBJ-2: text object did not render its exact content in blob view",
        repro: "open uitest/uitest-readme.txt",
        expected: JSON.stringify(textContent),
        actual: JSON.stringify(bv),
        evidence: ["evidence/OBJ-2/01-text-preview.png"],
      });
    }

    // image preview (blob view img + tree thumb)
    await harness.clickService(spa, OBJECT_SERVICE);
    await sleep(200);
    await clickTreeNode(spa, "uitest");
    await sleep(250);
    await clickTreeNode(spa, "pic.png");
    await sleep(300);
    const bvImg = await blobViewState(spa, "pic.png");
    const hasThumbInTree = await spa.evaluate(() => !!document.querySelector("img.thumb"));
    await evidence("02-image-preview");
    if (!bvImg.hasImage) {
      addFinding({
        severity: "S1",
        title: "OBJ-2: PNG object did not render img.imgpreview in blob view",
        repro: "open uitest/pic.png",
        expected: "img.imgpreview present and loaded",
        actual: JSON.stringify(bvImg),
        evidence: ["evidence/OBJ-2/02-image-preview.png"],
      });
    }
    addFinding({
      id: "OBJ-2-tree-thumb",
      severity: hasThumbInTree ? "S3" : "S2",
      title: "OBJ-2: tree row " + (hasThumbInTree ? "DOES" : "does NOT") + " show a lazy img.thumb for the PNG",
      repro: "expand uitest, look at pic.png's tree row (lazyThumb() requires ACTION.readBlob enabled + size <= 2MiB)",
      expected: "n/a -- informational (feature is conditional per app.js's lazyThumb)",
      actual: "hasThumbInTree=" + hasThumbInTree,
      evidence: ["evidence/OBJ-2/02-image-preview.png"],
      status: hasThumbInTree ? "info" : "new",
    });

    // non-previewable binary
    await harness.clickService(spa, OBJECT_SERVICE);
    await sleep(200);
    await clickTreeNode(spa, "uitest");
    await sleep(250);
    await clickTreeNode(spa, "blob.bin");
    await sleep(250);
    const bvBin = await blobViewState(spa, "blob.bin");
    await evidence("03-binary-download-only");
    const honestBinaryState = !bvBin.hasImage && bvBin.editorText == null && (bvBin.placeholderText || "").toLowerCase().indexOf("binary") >= 0;
    if (!honestBinaryState) {
      addFinding({
        severity: "S1",
        title: "OBJ-2: non-previewable binary object did not show the honest download-only placeholder",
        repro: "PUT uitest/blob.bin with application/octet-stream + non-textual bytes; open",
        expected: '"Binary content — use Download." placeholder, never an editor/garbled text',
        actual: JSON.stringify(bvBin),
        evidence: ["evidence/OBJ-2/03-binary-download-only.png"],
      });
    }
  } finally {
    await objTeardown();
  }
}

// ============================================================================
// OBJ-3 — upload (embedded host dialog)
// ============================================================================

async function runOBJ3(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  const { shellQuote } = require("../lib/engines");
  await objTeardown();
  const UPLOAD_SRC = "/tmp/uitest-upload.txt";
  const UPLOAD_CONTENT = "uitest OBJ-3 upload payload " + Date.now() + "\n";
  const ROOT_UPLOAD_NAME = "zzz-uitest-upload-test.txt"; // sorts to the bottom; unmistakably a throwaway
  try {
    await s3("PUT", OBJ_PREFIX + "keep.txt", { body: Buffer.from("keep\n"), contentType: "text/plain" });
    engines.container("printf %s " + shellQuote(UPLOAD_CONTENT) + " > " + UPLOAD_SRC);

    const spa = await openService(ctx, OBJECT_SERVICE, true);
    await sleep(200);

    // --- scope check: does .uploadbar appear at the true bucket root? ---
    let hasUploadAtRoot = await spa.evaluate(() => !!document.querySelector(".uploadbar"));
    await evidence("01-root-uploadbar");
    if (!hasUploadAtRoot) {
      addFinding({
        severity: "S1",
        title: "OBJ-3: .uploadbar does not appear at the bucket root even in write mode",
        repro: "openService(storage, write=true); look for .uploadbar under #tree",
        expected: ".uploadbar present (root-level addUploadBar call)",
        actual: "absent",
        evidence: ["evidence/OBJ-3/01-root-uploadbar.png"],
      });
    }

    // --- scope check: does .uploadbar appear INSIDE the uitest/ subfolder? ---
    // Source read (app.js): expandContainer() always calls appendTreePage with
    // root=false, and addUploadBar() is gated on `root && editing() && ...` in
    // BOTH the empty-state and populated branches -- so a subfolder should
    // never get an upload bar, only the absolute tree root does. Confirming live.
    // Scoped specifically to "uitest"'s own .children div -- an UNscoped
    // document.querySelector(".uploadbar") would trivially match the ROOT's
    // still-present upload bar from the check above (nothing removes it when
    // a subfolder expands) and falsely read as "also in the subfolder".
    await clickTreeNode(spa, "uitest");
    await sleep(250);
    const hasUploadInSubfolder = await spa.evaluate(() => {
      const wraps = Array.from(document.querySelectorAll("#tree .node-wrap"));
      const uitestWrap = wraps.find((w) => {
        const nm = w.querySelector(":scope > .node .nname");
        return nm && nm.textContent === "uitest";
      });
      const childrenDiv = uitestWrap ? uitestWrap.querySelector(":scope > .children") : null;
      return !!childrenDiv && !!childrenDiv.querySelector(".uploadbar");
    });
    await evidence("02-subfolder-uploadbar");
    addFinding({
      id: "OBJ-3-upload-root-only-scope",
      severity: hasUploadInSubfolder ? "S3" : "S2",
      title: "OBJ-3 scope: upload affordance is " + (hasUploadInSubfolder ? "ALSO available inside a subfolder (contradicts source read)" : "ONLY available at the bucket root -- expanding into any subfolder (including uitest/) removes .uploadbar entirely"),
      repro: "write mode on; compare .uploadbar presence at tree root vs after expanding uitest/",
      expected: "n/a -- this is the scope finding: appendTreePage()'s addUploadBar() call is gated on `root===true`, and expandContainer() always recurses with root=false, so there is no UI path to upload directly into any folder -- only the bucket's absolute top level",
      actual: "hasUploadAtRoot=" + hasUploadAtRoot + "; hasUploadInSubfolder=" + hasUploadInSubfolder,
      evidence: ["evidence/OBJ-3/01-root-uploadbar.png", "evidence/OBJ-3/02-subfolder-uploadbar.png"],
      status: hasUploadInSubfolder ? "info" : "new",
    });

    if (!hasUploadAtRoot) {
      return; // nothing left to drive
    }

    // --- drive the actual upload at root (only reachable affordance) ---
    await harness.clickService(spa, OBJECT_SERVICE); // back to root -- triggers its own loadTree reload
    await waitTreeSettled(spa); // let that reload finish before touching the upload button (same race as openService's, see waitTreeSettled's comment)
    await spa.waitForSelector("button.uploadbtn", { timeout: 10000 });
    await sleep(150);
    await spa.click("button.uploadbtn");
    await sleep(600);
    await evidence("03-after-upload-click");

    // VS Code's showOpenDialog in a browser (code-server) context renders as
    // its OWN quick-input overlay in the MAIN frame (browsing the container
    // fs) -- not a native Chrome file chooser. Probe for both, honestly.
    let driven = false;
    let testabilityNote = "";
    try {
      const quickInput = await page.waitForSelector(".quick-input-widget", { timeout: 3000 });
      if (quickInput) {
        const inputSel = ".quick-input-widget input";
        const hasInput = await page.$(inputSel);
        if (hasInput) {
          await page.click(inputSel, { clickCount: 3 });
          await page.type(inputSel, UPLOAD_SRC);
          await sleep(400);
          await evidence("04-quick-input-path-typed");
          // VS Code's own quick-input file browser (code-server has no native
          // OS chooser to fall back to): one Enter DOES confirm an exact path
          // (proven live: the tree shows the uploaded file + an "Uploaded."
          // toast afterward). The catch: VS Code keeps .quick-input-widget's
          // CONTAINER in the DOM and just hides it on close (same pattern as
          // the console SPA's own #editswitch/.hidden) -- a bare page.$()
          // PRESENCE check therefore reads "still open" forever even after a
          // genuinely successful confirm, which is exactly what happened live
          // (screenshot showed the file uploaded + "Uploaded." toast while
          // this scenario's own check still reported the dialog stuck).
          // Wait for it to become HIDDEN (Puppeteer's {hidden:true}), not
          // merely absent.
          await page.keyboard.press("Enter");
          let closed = await page
            .waitForSelector(".quick-input-widget", { hidden: true, timeout: 4000 })
            .then(() => true)
            .catch(() => false);
          if (!closed) {
            // Genuinely still visible -- escalate, but never let a failure in
            // this best-effort fallback overwrite whatever the primary Enter
            // already achieved (a stale-handle click racing the widget's own
            // close animation threw here once; the outer catch mis-reported
            // that as a full testability failure even though the upload had
            // already succeeded).
            try {
              await page.keyboard.press("Enter");
              closed = await page
                .waitForSelector(".quick-input-widget", { hidden: true, timeout: 2000 })
                .then(() => true)
                .catch(() => false);
              if (!closed) {
                const okBtn = await page.evaluateHandle(() => {
                  const els = Array.from(document.querySelectorAll(".quick-input-widget button, .quick-input-widget a, .monaco-button"));
                  const visible = els.filter((e) => e.offsetParent !== null);
                  return visible.find((e) => (e.textContent || "").trim() === "OK") || null;
                });
                const okEl = okBtn.asElement();
                if (okEl) await okEl.click();
                closed = await page
                  .waitForSelector(".quick-input-widget", { hidden: true, timeout: 2000 })
                  .then(() => true)
                  .catch(() => false);
              }
            } catch (_) {
              /* best-effort escalation only -- fall through to the final visibility check below */
            }
          }
          driven = closed;
          if (!driven) testabilityNote = "typed the exact path, tried Enter twice and an OK-button click; the dialog was still VISIBLE afterward (not just present in the DOM)";
        } else {
          testabilityNote = "quick-input-widget appeared but no <input> inside it to type a path into";
        }
      }
    } catch (e) {
      testabilityNote = "dialog-driving attempt threw: " + e.message;
    }
    if (!driven) {
      await evidence("04-dialog-undriven-state");
      addFinding({
        id: "OBJ-3-upload-dialog-testability",
        severity: "S3",
        title: "OBJ-3: honest attempt to drive the host upload dialog did not complete",
        repro: "click #uploadbtn at bucket root; probe for .quick-input-widget in the main frame; type the exact path; Enter x2; OK-button click",
        expected: "n/a -- testability finding per instructions (skip rather than fake)",
        actual: testabilityNote || "unknown dialog state",
        evidence: ["evidence/OBJ-3/03-after-upload-click.png", "evidence/OBJ-3/04-dialog-undriven-state.png"],
        status: "info",
      });
      return;
    }

    await sleep(500);
    const s3After = await s3ListAll("");
    const uploaded = s3After.find((o) => o.key === ROOT_UPLOAD_NAME || o.key === UPLOAD_SRC.split("/").pop());
    await evidence("05-after-dialog-confirm");
    if (!uploaded) {
      addFinding({
        severity: "S2",
        title: "OBJ-3: drove the upload dialog to pick " + UPLOAD_SRC + " but no matching object appeared in S3",
        repro: "click #uploadbtn; type path " + UPLOAD_SRC + " into the quick-input; Enter",
        expected: "an object named uitest-upload.txt (or similar) appears at the bucket root",
        actual: "S3 root listing after: " + JSON.stringify(s3After.map((o) => o.key)),
        evidence: ["evidence/OBJ-3/05-after-dialog-confirm.png"],
      });
    } else {
      const toast = await harness.waitToast(spa, 5000);
      await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
      addFinding({
        id: "OBJ-3-upload-works-root-only",
        severity: "S3",
        title: "OBJ-3: upload works when driven at the bucket root (the only reachable affordance)",
        repro: "click #uploadbtn at root; pick " + UPLOAD_SRC + " via the quick-input dialog",
        expected: "n/a -- confirms the mechanism functions; see OBJ-3-upload-root-only-scope for the placement gap",
        actual: "uploaded key=" + uploaded.key + " size=" + uploaded.size + "; toast=" + JSON.stringify(toast),
        evidence: ["evidence/OBJ-3/05-after-dialog-confirm.png"],
        status: "info",
      });
      // immediate cleanup -- this object landed outside uitest/ by construction
      // of the affordance-placement bug above, not by choice; remove it now.
      try {
        await s3("DELETE", uploaded.key);
      } catch (_) {
        /* best effort */
      }
    }
  } finally {
    await objTeardown();
  }
}

// ============================================================================
// OBJ-4 — browser-local download (no host save dialog / Quick Input)
// ============================================================================

async function runOBJ4(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  await objTeardown();
  const downloadDir = path.join(__dirname, "..", "evidence", "OBJ-4", "downloads");
  const CONTENT = "uitest OBJ-4 download payload " + Date.now() + "\n";
  try {
    // The browser, not the remote Studio container, owns the resulting file.
    // Start empty so a prior same-named dl.txt can never fake this run's result.
    fs.rmSync(downloadDir, { recursive: true, force: true });
    fs.mkdirSync(downloadDir, { recursive: true });

    let cdpMode = "";
    let cdpFailure = "";
    try {
      const client = await page.createCDPSession();
      try {
        await client.send("Browser.setDownloadBehavior", {
          behavior: "allow",
          downloadPath: downloadDir,
          eventsEnabled: true,
        });
        cdpMode = "Browser.setDownloadBehavior";
      } catch (browserErr) {
        await client.send("Page.setDownloadBehavior", { behavior: "allow", downloadPath: downloadDir });
        cdpMode = "Page.setDownloadBehavior fallback";
      }
    } catch (e) {
      cdpFailure = e && e.message ? e.message : String(e);
    }

    await s3("PUT", OBJ_PREFIX + "dl.txt", { body: Buffer.from(CONTENT), contentType: "text/plain" });

    const spa = await openService(ctx, OBJECT_SERVICE, false);
    await clickTreeNode(spa, "uitest");
    await sleep(250);
    await clickTreeNode(spa, "dl.txt");
    await sleep(250);
    await spa.waitForSelector("#dlblob", { timeout: 15000 });
    const toastPromise = harness.waitToast(spa, 20000);
    await spa.click("#dlblob");
    await sleep(400);
    await evidence("01-after-download-click");

    const quickInputVisible = await page.waitForFunction(
      () => Array.from(document.querySelectorAll(".quick-input-widget")).some((el) => {
        const style = getComputedStyle(el);
        const rect = el.getBoundingClientRect();
        return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
      }),
      { timeout: 1500 }
    ).then(() => true).catch(() => false);
    if (quickInputVisible) {
      addFinding({
        severity: "S1",
        title: "OBJ-4: Download still opens VS Code Quick Input",
        repro: "open storage/uitest/dl.txt in the embedded Data Console and click Download",
        expected: "the browser accepts a local attachment with no visible .quick-input-widget",
        actual: "a visible .quick-input-widget remained after the click",
        evidence: ["evidence/OBJ-4/01-after-download-click.png"],
      });
    }

    if (!cdpMode) {
      addFinding({
        id: "OBJ-4-browser-download-testability",
        severity: "S3",
        title: "OBJ-4: browser download capture could not be enabled",
        repro: "configure Browser.setDownloadBehavior (or Page fallback) before clicking #dlblob",
        expected: "CDP accepts a browser-local download directory",
        actual: cdpFailure || "both CDP download behavior methods were rejected",
        evidence: ["evidence/OBJ-4/01-after-download-click.png"],
        status: "info",
      });
      await toastPromise;
      return;
    }

    let completed = [];
    const deadline = Date.now() + 20000;
    while (Date.now() < deadline) {
      const files = fs.readdirSync(downloadDir);
      const partial = files.some((name) => name.endsWith(".crdownload"));
      completed = files.filter((name) => !name.endsWith(".crdownload"));
      if (!partial && completed.length > 0) break;
      await sleep(100);
    }
    const toast = await toastPromise;
    await evidence("02-browser-download-complete");
    const downloadedName = completed.includes("dl.txt") ? "dl.txt" : completed[0];
    const downloaded = downloadedName ? fs.readFileSync(path.join(downloadDir, downloadedName)) : null;
    const matches = !!downloaded && downloaded.equals(Buffer.from(CONTENT));
    if (!matches || !toast || toast.kind !== "good" || toast.text !== "Downloaded.") {
      addFinding({
        severity: "S1",
        title: "OBJ-4: browser-local download did not complete with exact source bytes",
        repro: "open storage/uitest/dl.txt; click Download with CDP capture enabled via " + cdpMode,
        expected: "no Quick Input; one complete browser-local dl.txt; bytes=" + JSON.stringify(CONTENT) + "; success toast=Downloaded.",
        actual: "quickInputVisible=" + quickInputVisible + "; files=" + JSON.stringify(completed) + "; bytes=" + (downloaded ? JSON.stringify(downloaded.toString("utf8")) : "MISSING") + "; toast=" + JSON.stringify(toast),
        evidence: ["evidence/OBJ-4/01-after-download-click.png", "evidence/OBJ-4/02-browser-download-complete.png"],
        engine_truth: "browser-local file " + (downloadedName ? path.join(downloadDir, downloadedName) : "missing"),
      });
    }
  } finally {
    await objTeardown();
  }
}

// ============================================================================
// OBJ-5 — delete object (+ empty-folder S3 semantics)
// ============================================================================

async function runOBJ5(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  await objTeardown();
  try {
    await s3("PUT", OBJ_PREFIX + "todelete.txt", { body: Buffer.from("delete me\n"), contentType: "text/plain" });
    await s3("PUT", OBJ_PREFIX + "sub/onlyfile.txt", { body: Buffer.from("only one here\n"), contentType: "text/plain" });

    const spa = await openService(ctx, OBJECT_SERVICE, true);
    await clickTreeNode(spa, "uitest");
    await sleep(250);
    await clickTreeNode(spa, "todelete.txt");
    await sleep(200);
    await spa.waitForSelector("#delblob", { timeout: 15000 });
    await spa.click("#delblob");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    await evidence("01-delete-confirm-modal");
    await spa.click("#modalok");
    const toast = await harness.waitToast(spa);
    await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
    await sleep(300);
    const stillThere = await s3ListAll(OBJ_PREFIX + "todelete.txt");
    await evidence("02-after-delete");
    const toastGood = !!toast && toast.kind === "good";
    if (toastGood !== (stillThere.length === 0)) {
      addFinding({
        id: toastGood ? "KI-1-scope-object-success-lie" : "KI-1-scope-object-delete-rejected",
        severity: "S1",
        title: "KI-1 scope answer: object storage delete " + (toastGood ? "claimed success but S3 still has the object (success-lie)" : "was rejected -- does the SAME bug class hit /api/node for object storage too?"),
        repro: "PUT uitest/todelete.txt; open; #delblob; confirm #modalok",
        expected: "toast success iff the object is actually gone from S3",
        actual: "toast=" + JSON.stringify(toast) + "; still in S3=" + JSON.stringify(stillThere),
        evidence: ["evidence/OBJ-5/01-delete-confirm-modal.png", "evidence/OBJ-5/02-after-delete.png"],
        engine_truth: "S3 LIST prefix=" + OBJ_PREFIX + "todelete.txt -> " + JSON.stringify(stillThere),
      });
    } else {
      addFinding({
        id: "KI-1-scope-object-delete-" + (toastGood ? "works" : "fails-same-as-kv"),
        severity: toastGood ? "S3" : "S1",
        title: "KI-1 scope answer: object storage delete (same /api/node handler as KV) " + (toastGood ? "WORKS (KI-1 does not reproduce for object storage)" : "FAILS the same way as the KV case"),
        repro: "PUT uitest/todelete.txt; open; #delblob; confirm #modalok",
        expected: "n/a -- this IS the scope answer",
        actual: "toast=" + JSON.stringify(toast) + "; still in S3=" + JSON.stringify(stillThere),
        evidence: ["evidence/OBJ-5/01-delete-confirm-modal.png", "evidence/OBJ-5/02-after-delete.png"],
        engine_truth: "S3 LIST prefix=" + OBJ_PREFIX + "todelete.txt -> " + JSON.stringify(stillThere),
        status: "info",
      });
    }

    // Also check that a FAILED delete never removes the row from the tree
    // (UI honesty), and a SUCCESSFUL one does.
    const stillInTree = await treeHasNode(spa, "todelete.txt");
    if (toastGood && stillInTree) {
      addFinding({
        severity: "S2",
        title: "OBJ-5: object genuinely deleted but tree still shows it (refresh-lag)",
        repro: "delete uitest/todelete.txt; re-check #tree",
        expected: "row removed from #tree",
        actual: "still present",
        evidence: ["evidence/OBJ-5/02-after-delete.png"],
      });
    } else if (!toastGood && !stillInTree) {
      addFinding({
        severity: "S1",
        title: "OBJ-5: delete was rejected but the row was removed from the tree anyway (UI dishonesty)",
        repro: "delete uitest/todelete.txt (rejected); re-check #tree",
        expected: "row stays present since the object still exists server-side",
        actual: "row removed despite rejection",
        evidence: ["evidence/OBJ-5/02-after-delete.png"],
      });
    }

    // --- empty-folder S3 semantics: delete the LAST object in sub/, does the
    // folder itself vanish from the parent listing? ---
    await harness.clickService(spa, OBJECT_SERVICE);
    await sleep(200);
    await clickTreeNode(spa, "uitest");
    await sleep(250);
    await clickTreeNode(spa, "sub");
    await sleep(250);
    await clickTreeNode(spa, "onlyfile.txt");
    await sleep(200);
    await spa.waitForSelector("#delblob", { timeout: 15000 });
    await spa.click("#delblob");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    await spa.click("#modalok");
    await harness.waitToast(spa);
    await waitToastGone(spa); // gotcha #8: drain before the next mutation so the next waitToast never reads this one stale
    await sleep(300);
    const s3AfterLastDelete = await s3ListAll(OBJ_PREFIX + "sub/");
    await harness.clickService(spa, OBJECT_SERVICE);
    await sleep(200);
    await clickTreeNode(spa, "uitest");
    await sleep(250);
    const uitestNamesAfter = await treeNodeNames(spa);
    await evidence("03-sub-folder-after-last-object-deleted");
    addFinding({
      id: "OBJ-5-empty-folder-semantics",
      severity: "S3",
      title: 'OBJ-5: after deleting the last object in uitest/sub/, the "sub" folder ' + (uitestNamesAfter.includes("sub") ? "STILL shows in the tree" : "correctly disappears from the tree"),
      repro: "delete the only object under uitest/sub/ (S3 has no real folders -- a prefix disappears once its last key is gone unless an explicit zero-byte marker object exists)",
      expected: "n/a -- informational, S3 has no folder concept; recording actual UI behavior",
      actual: "S3 LIST prefix=uitest/sub/ after last delete: " + JSON.stringify(s3AfterLastDelete) + "; tree at uitest/ shows: " + JSON.stringify(uitestNamesAfter),
      evidence: ["evidence/OBJ-5/03-sub-folder-after-last-object-deleted.png"],
      status: "info",
    });
  } finally {
    await objTeardown();
  }
}

runner.register({ id: "KV-1", family: "kv", fn: runKV1 });
runner.register({ id: "KV-2", family: "kv", fn: runKV2 });
runner.register({ id: "KV-3", family: "kv", fn: runKV3 });
runner.register({ id: "KV-4", family: "kv", fn: runKV4 });
runner.register({ id: "KV-5", family: "kv", fn: runKV5 });
runner.register({ id: "KV-6", family: "kv", fn: runKV6 });
runner.register({ id: "OBJ-1", family: "object", fn: runOBJ1 });
runner.register({ id: "OBJ-2", family: "object", fn: runOBJ2 });
runner.register({ id: "OBJ-3", family: "object", fn: runOBJ3 });
runner.register({ id: "OBJ-4", family: "object", fn: runOBJ4 });
runner.register({ id: "OBJ-5", family: "object", fn: runOBJ5 });

module.exports = {};
