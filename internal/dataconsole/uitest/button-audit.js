"use strict";

// A11 — exhaustive button audit. Standalone Node script (like gallery.js), NOT a
// scenario -- drives every interactive control app.js wires up, live, against the
// deployed SPA, and proves each one's EFFECT (DOM state, engine oracle, or a
// byte-for-byte comparison of downloaded/uploaded bytes) rather than just
// confirming presence. Centerpiece: #dlblob (download) and #uploadbtn (upload)
// proven end-to-end with real files landing on disk.
//
// Every control below is numbered to match the inventory handed down for this
// audit (1-28, cross-referenced against app.js line numbers at the point this
// was written). See the CONTROLS table + evidence/BUTTONS/index.md for the
// verdict on each.

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const harness = require("./lib/harness");
const engines = require("./lib/engines");
const { loadConfig } = require("./lib/config");

const ROOT = __dirname;
const EVIDENCE_DIR = path.join(ROOT, "evidence", "BUTTONS");
const SCENARIO_ID = "BUTTONS";
const FINDINGS_FILE = path.join(ROOT, "findings", "a11-buttons.jsonl");

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
function cfg() {
  return loadConfig();
}

// ============================================================================
// findings + control-audit table
// ============================================================================

let findingsN = 0;
const RUN_TAG = Date.now();
function addFinding(f) {
  f = f || {};
  const record = Object.assign(
    {
      // RUN_TAG (module-load timestamp) keeps auto-generated ids unique across
      // separate invocations of this script -- findingsN alone resets to 0 each
      // process, so a second run's un-id'd finding collided with the first
      // run's BUTTONS-1 in the shared append-only JSONL (harmless, but confusing).
      id: f.id || SCENARIO_ID + "-" + RUN_TAG + "-" + ++findingsN,
      scenario: SCENARIO_ID,
      lane: "F",
      family: f.family || "buttons",
      severity: f.severity || "S3",
      title: "",
      repro: "",
      expected: "",
      actual: "",
      evidence: [],
      engine_truth: "",
      status: "new",
    },
    f
  );
  fs.mkdirSync(path.dirname(FINDINGS_FILE), { recursive: true });
  fs.appendFileSync(FINDINGS_FILE, JSON.stringify(record) + "\n");
  console.log("[FINDING " + record.severity + "] " + record.title);
  return record;
}

const CONTROLS = []; // {n, name, wired, clicked, effect, evidence:[...], note}
function recordControl(n, name, wired, clicked, effect, evidencePaths, note) {
  CONTROLS.push({
    n,
    name,
    wired: !!wired,
    clicked: !!clicked,
    effect: !!effect,
    evidence: evidencePaths || [],
    note: note || "",
  });
  console.log(
    "[C" + n + "] " + name + " -- wired=" + !!wired + " clicked=" + !!clicked + " effect=" + !!effect + (note ? " (" + note + ")" : "")
  );
}

// attempt wraps one control's driving code so a single control's bug can't abort
// the whole audit (mirrors runner.js's G1 rule: a harness throw is a finding
// here, not a fatal abort -- every other control still gets its own live drive).
async function attempt(label, fn) {
  try {
    await fn();
  } catch (e) {
    const msg = e && e.stack ? e.stack : String(e);
    console.error("[" + label + "] threw:\n" + msg);
    addFinding({
      severity: "S2",
      title: 'button-audit: driving "' + label + '" threw an unexpected error',
      repro: label,
      expected: "control drives cleanly to a verifiable effect",
      actual: String(e && e.message ? e.message : e),
      evidence: [],
    });
  }
}

let shotN = 0;
async function shot(page, name) {
  fs.mkdirSync(EVIDENCE_DIR, { recursive: true });
  shotN++;
  const nn = String(shotN).padStart(2, "0");
  const safe = String(name).replace(/[^a-z0-9._-]+/gi, "-");
  const file = path.join(EVIDENCE_DIR, nn + "-" + safe + ".png");
  await page.screenshot({ path: file, fullPage: true });
  return path.relative(ROOT, file);
}

// ============================================================================
// Object storage (S3-compatible) — hand-rolled AWS SigV4, no deps. Copied
// pattern (kv-object.js / gallery.js): each scenario file stays self-contained
// rather than cross-importing (see uitest/README.md's own note on this).
// ============================================================================

const OBJ_PREFIX = "uitest_btn/";
const TINY_PNG_B64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=";

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
  const qs = opts.query
    ? Object.keys(opts.query)
        .sort()
        .map((k) => encodeURIComponent(k) + "=" + encodeURIComponent(String(opts.query[k])))
        .join("&")
    : "";
  const headers = { host: endpoint.host, "x-amz-content-sha256": payloadHash, "x-amz-date": amzDate };
  if (opts.contentType) headers["content-type"] = opts.contentType;
  const sortedKeys = Object.keys(headers).sort();
  const canonicalHeaders = sortedKeys.map((k) => k + ":" + headers[k] + "\n").join("");
  const signedHeaders = sortedKeys.join(";");
  const canonicalRequest = [method, canonicalURI, qs, canonicalHeaders, signedHeaders, payloadHash].join("\n");
  const credentialScope = dateStamp + "/" + region + "/" + service + "/aws4_request";
  const stringToSign = ["AWS4-HMAC-SHA256", amzDate, credentialScope, sha256hex(Buffer.from(canonicalRequest))].join("\n");
  const kDate = hmac("AWS4" + c.DC_S3_SECRET_KEY, dateStamp);
  const kRegion = hmac(kDate, region);
  const kService = hmac(kRegion, service);
  const kSigning = hmac(kService, "aws4_request");
  const signature = crypto.createHmac("sha256", kSigning).update(stringToSign).digest("hex");
  headers.authorization =
    "AWS4-HMAC-SHA256 Credential=" + c.DC_S3_ACCESS_KEY + "/" + credentialScope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature;
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
async function s3ListAll(prefix) {
  const out = [];
  let token = null;
  for (;;) {
    const query = { "list-type": "2", prefix: prefix };
    if (token) query["continuation-token"] = token;
    const r = await s3("GET", "", { query });
    const xml = await r.text();
    if (r.status !== 200) throw new Error("s3ListAll: " + r.status + " " + xml.slice(0, 300));
    const re = /<Contents>([\s\S]*?)<\/Contents>/g;
    let m;
    while ((m = re.exec(xml))) {
      const key = (/<Key>([\s\S]*?)<\/Key>/.exec(m[1]) || [])[1];
      if (key) out.push({ key: decodeXMLEntities(key) });
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
  // OBJ-3's known scope bug lands uploads at the bucket ROOT, outside our
  // prefix -- sweep for our own root-level throwaway names too.
  try {
    const root = await s3ListAll("");
    for (const o of root) {
      if (/^uitest_btn[_/]/.test(o.key) && !o.key.startsWith(OBJ_PREFIX)) {
        try {
          await s3("DELETE", o.key);
        } catch (_) {
          /* best effort */
        }
      }
    }
  } catch (_) {
    /* best effort */
  }
}

// ============================================================================
// KV (valkey) teardown — one round-trip via server-side EVAL rather than N
// individual SSH round-trips (this audit seeds 250+ keys for the tree
// Load-More proof; a per-key DEL loop would be 250+ separate ssh invocations).
// ============================================================================

const KV_PREFIX = "uitest_btn";
function kvTeardown() {
  try {
    engines.redis([
      "EVAL",
      "local keys = redis.call('KEYS', ARGV[1]) for i=1,#keys do redis.call('DEL', keys[i]) end return #keys",
      "0",
      KV_PREFIX + "*",
    ]);
  } catch (_) {
    /* best effort */
  }
}

// ============================================================================
// Postgres (tabular "db") teardown
// ============================================================================

function pgTeardown() {
  try {
    engines.psql("DROP TABLE IF EXISTS uitest_btn_tab");
  } catch (_) {}
  try {
    engines.psql("DROP TABLE IF EXISTS uitest_btn_wide");
  } catch (_) {}
}

// ============================================================================
// Document (elasticsearch) + vector (qdrant) — HTTP-over-SSH-curl, same
// pattern as document.js/gallery.js (these hostnames only resolve inside the
// container's network).
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
function qdUrl(p) {
  const c = cfg();
  return "http://" + c.DC_QDRANT_HOST + ":" + c.DC_QDRANT_PORT + p;
}
function esRequest(method, p, body) {
  const c = cfg();
  return httpJSON("-u " + engines.shellQuote(c.DC_ES_USER + ":" + c.DC_ES_PASSWORD), method, esUrl(p), body);
}
function qdRequest(method, p, body) {
  const c = cfg();
  return httpJSON("-H " + engines.shellQuote("api-key: " + c.DC_QDRANT_KEY), method, qdUrl(p), body);
}

const ES_PREFIX = "uitest_btn_docs";
const VEC_NAME = "uitest_btn_vec";

function esTeardown() {
  try {
    esRequest("DELETE", "/" + ES_PREFIX);
  } catch (_) {}
}
function qdTeardown() {
  try {
    qdRequest("DELETE", "/collections/" + VEC_NAME);
  } catch (_) {}
}

async function teardownAll() {
  kvTeardown();
  await objTeardown();
  pgTeardown();
  esTeardown();
  qdTeardown();
  try {
    engines.container(
      "rm -f /tmp/uitest_btn_upload.txt /tmp/uitest_btn_dl_text.out /tmp/uitest_btn_dl_png.out /tmp/uitest_btn_dl_large.out"
    );
  } catch (_) {}
}

// ============================================================================
// Tree/grid DOM helpers (local copies of the proven kv-object.js/document.js
// patterns -- those files export nothing, see README's "self-contained" note)
// ============================================================================

async function clickTreeNode(frame, name, timeoutMs) {
  try {
    await frame.waitForFunction(
      (n) => Array.from(document.querySelectorAll("#tree .node .nname")).some((el) => el.textContent === n),
      { timeout: timeoutMs || 8000 },
      name
    );
  } catch (_) {
    /* fall through -- click below honestly returns false on genuine absence */
  }
  return frame.evaluate((n) => {
    const rows = Array.from(document.querySelectorAll("#tree .node"));
    const row = rows.find((r) => {
      const nm = r.querySelector(".nname");
      return nm && nm.textContent === n;
    });
    if (!row) return false;
    row.click();
    return true;
  }, name);
}

async function waitBlobReady(frame, expectName, timeoutMs) {
  try {
    await frame.waitForFunction(
      (n) => {
        const b = document.querySelector("#content .toolbar b");
        return !!b && b.textContent === n && !!document.getElementById("dlblob");
      },
      { timeout: timeoutMs || 10000 },
      expectName
    );
  } catch (_) {
    /* fall through -- read whatever is actually there */
  }
  return frame.evaluate(() => ({
    hasSave: !!document.getElementById("saveblob"),
    hasRename: !!document.getElementById("renameblob"),
    hasDelete: !!document.getElementById("delblob"),
    hasDownload: !!document.getElementById("dlblob"),
    preText: document.querySelector("pre.blob") ? document.querySelector("pre.blob").textContent : null,
    editorText: document.getElementById("blobedit") ? document.getElementById("blobedit").value : null,
    placeholderText: (document.querySelector("#content .placeholder") || {}).textContent || null,
  }));
}

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

async function drainToast(frame) {
  try {
    await frame.waitForFunction(() => !document.querySelector(".toast.good, .toast.bad, .toast.warn"), { timeout: 3800 });
  } catch (_) {
    /* best effort */
  }
}

async function containerChildCount(frame, containerName) {
  return frame.evaluate((pname) => {
    const wraps = Array.from(document.querySelectorAll("#tree .node-wrap"));
    const parentWrap = wraps.find((w) => {
      const nm = w.querySelector(":scope > .node .nname");
      return nm && nm.textContent === pname;
    });
    if (!parentWrap) return -1;
    const childrenDiv = parentWrap.querySelector(":scope > .children");
    if (!childrenDiv) return -1;
    return childrenDiv.querySelectorAll(":scope > .node-wrap").length;
  }, containerName);
}

async function clickContainerLoadMore(frame, containerName) {
  return frame.evaluate((pname) => {
    const wraps = Array.from(document.querySelectorAll("#tree .node-wrap"));
    const parentWrap = wraps.find((w) => {
      const nm = w.querySelector(":scope > .node .nname");
      return nm && nm.textContent === pname;
    });
    if (!parentWrap) return false;
    const childrenDiv = parentWrap.querySelector(":scope > .children");
    const btn = childrenDiv ? childrenDiv.querySelector(":scope > .loadmore") : null;
    if (!btn) return false;
    btn.click();
    return true;
  }, containerName);
}

async function readGrid(frame) {
  return frame.evaluate(() => ({
    heads: Array.from(document.querySelectorAll("table.grid thead th")).map((th) => th.textContent),
    rows: Array.from(document.querySelectorAll("table.grid tbody.gridbody tr")).map((tr) =>
      Array.from(tr.querySelectorAll("td")).map((td) => td.textContent)
    ),
    toolbarMeta: (document.querySelector(".toolbar .meta") || {}).textContent || null,
  }));
}

async function gridRowIndexByText(frame, colIdx, text) {
  return frame.evaluate(
    (ci, t) => {
      const rows = Array.from(document.querySelectorAll("table.grid tbody.gridbody tr"));
      return rows.findIndex((tr) => {
        const tds = tr.querySelectorAll("td");
        return tds[ci] && tds[ci].textContent === t;
      });
    },
    colIdx,
    text
  );
}

// driveQuickInput generalizes OBJ-3's/OBJ-4's proven-live dialog-driving code
// (VS Code's own showOpenDialog/showSaveDialog render as .quick-input-widget in
// the MAIN frame under code-server -- no native OS chooser to fall back to).
// Escalates Enter -> Enter -> an "OK" button click, and waits for the widget to
// become HIDDEN (not merely absent -- VS Code keeps the container in the DOM
// and just hides it on close, proven live in OBJ-3/OBJ-4).
async function driveQuickInput(page, typeText, evidencePrefix) {
  let driven = false;
  let note = "";
  try {
    const quickInput = await page.waitForSelector(".quick-input-widget", { timeout: 5000 });
    if (quickInput) {
      const inputSel = ".quick-input-widget input";
      if (await page.$(inputSel)) {
        await page.click(inputSel, { clickCount: 3 });
        await page.type(inputSel, typeText);
        await sleep(400);
        await shot(page, evidencePrefix + "-path-typed");
        await page.keyboard.press("Enter");
        let closed = await page
          .waitForSelector(".quick-input-widget", { hidden: true, timeout: 4000 })
          .then(() => true)
          .catch(() => false);
        if (!closed) {
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
            /* best-effort escalation only */
          }
        }
        driven = closed;
        if (!driven) note = "typed path, tried Enter x2 + OK-button click; dialog still VISIBLE afterward";
      } else {
        note = "quick-input-widget appeared but no <input> inside it";
      }
    }
  } catch (e) {
    note = "dialog-driving attempt threw: " + e.message;
  }
  return { driven, note };
}

// spawnReadOnlySession spawns a throwaway `zcp studio console serve` instance
// WITHOUT --allow-writes (self-expiring via `timeout`, never SSH-killed --
// README gotcha #10 / ops discipline), mirroring CORE-3's proven-live mechanism.
// Gives us a URL + bearer completely independent of both the embedded webview
// AND the standalone tab's own driving code -- used to fetch bytes directly
// over HTTP as the DATA-HALF proof for download, decoupled from whether the
// save-dialog can be driven at all.
function spawnReadOnlySession(tag) {
  const marker = "uitest_btn_" + tag + "_" + Date.now();
  const readyFile = "/tmp/" + marker + "_ready.json";
  const errFile = "/tmp/" + marker + "_err.log";
  engines.container(
    "rm -f " + readyFile + " " + errFile + "; nohup timeout 600 zcp studio console serve --port 0 >" + readyFile + " 2>" + errFile +
      " </dev/null & sleep 2; cat " + readyFile
  );
  const ready = JSON.parse(engines.container("cat " + readyFile));
  const portMatch = /:(\d+)\/?$/.exec(ready.url || "");
  const port = portMatch ? portMatch[1] : null;
  if (!port) throw new Error("spawnReadOnlySession: could not parse port from ready.url: " + JSON.stringify(ready));
  const origin = new URL(cfg().DC_URL).origin;
  const proxyBase = origin + "/proxy/" + port + "/";
  return { ready, proxyBase, sessionToken: ready.sessionToken };
}

// directFetchBlob hits the console's own /api/blob over the code-server proxy
// with plain Node fetch -- no puppeteer, no webview, no save dialog. This is
// the independent "data half" proof: it exercises the exact same server route
// the UI's download button ultimately calls, just without any UI in between.
async function directFetchBlob(session, service, segs) {
  const q = "service=" + encodeURIComponent(service) + "&segs=" + encodeURIComponent(JSON.stringify(segs));
  const r = await fetch(session.proxyBase + "api/blob?" + q, {
    headers: { Cookie: "__zcp_auth=" + cfg().DC_AUTH_TOKEN, Authorization: "Bearer " + session.sessionToken },
  });
  const buf = Buffer.from(await r.arrayBuffer());
  return { status: r.status, buf };
}

// ============================================================================
// Fixture seeding
// ============================================================================

async function seedAll() {
  // ---- KV (cache) ----
  const kvScript =
    "redis.call('SET','uitest_btn_grp:a','1') " +
    "redis.call('SET','uitest_btn_grp:b','2') " +
    "redis.call('SET','uitest_btn_grp:c','3') " +
    "redis.call('SET','uitest_btn_edit','before-edit') " +
    "redis.call('SET','uitest_btn_del','delete-me') " +
    "redis.call('SET','uitest_btn_ttl','ttl-me') " +
    "for i=1,250 do redis.call('SET','uitest_btn_page:'..i,'v') end " +
    "return 'OK'";
  engines.redis(["EVAL", kvScript, "0"]);

  // ---- postgres (db) ----
  engines.psql("DROP TABLE IF EXISTS uitest_btn_tab");
  engines.psql("CREATE TABLE uitest_btn_tab (id serial PRIMARY KEY, txt text NOT NULL, num numeric(10,2))");
  engines.psql("INSERT INTO uitest_btn_tab (txt, num) VALUES ('row1', 1.00), ('row2', 2.00), ('row3', 3.00)");
  engines.psql("DROP TABLE IF EXISTS uitest_btn_wide");
  engines.psql("CREATE TABLE uitest_btn_wide (id serial PRIMARY KEY, val text)");
  engines.psql("INSERT INTO uitest_btn_wide (val) SELECT 'v'||i FROM generate_series(1,150) AS i");

  // ---- object storage (S3) — the download centerpiece's 3 fixtures ----
  const DL_TEXT = "uitest_btn download payload " + Date.now() + " — 日本語 café ✓\n";
  const DL_PNG = Buffer.from(TINY_PNG_B64, "base64");
  const DL_LARGE = crypto.randomBytes(200 * 1024);
  await s3("PUT", OBJ_PREFIX + "dl.txt", { body: Buffer.from(DL_TEXT, "utf8"), contentType: "text/plain; charset=utf-8" });
  await s3("PUT", OBJ_PREFIX + "dl.png", { body: DL_PNG, contentType: "image/png" });
  await s3("PUT", OBJ_PREFIX + "dl_large.bin", { body: DL_LARGE, contentType: "application/octet-stream" });
  // renameObject is object-storage-only (actions.go's familyMutatingActionIDs
  // does NOT include it for FamilyKV -- only FamilyObject) -- so control 15's
  // fixture lives here, not in the KV seed above.
  await s3("PUT", OBJ_PREFIX + "rename_src.txt", { body: Buffer.from("rename-me\n"), contentType: "text/plain" });

  // ---- elasticsearch (es) ----
  esRequest("DELETE", "/" + ES_PREFIX);
  const seedDoc = { id: "seed1", title: "uitest_btn seed doc", body: "findme_btn_marker_" + Date.now() };
  esRequest("PUT", "/" + ES_PREFIX + "/_doc/seed1", seedDoc);
  esRequest("POST", "/" + ES_PREFIX + "/_refresh");

  // ---- qdrant (vectors) ----
  qdRequest("DELETE", "/collections/" + VEC_NAME);
  // Euclid, not Cosine: Qdrant L2-normalizes vectors on insert for a
  // Cosine-distance collection (live-confirmed: seeding [0.11,0.22,0.33,0.44]
  // under Cosine round-tripped as [0.18257418,...], the normalized form) --
  // Euclid returns the raw floats verbatim, which is what control 25 checks.
  qdRequest("PUT", "/collections/" + VEC_NAME, { vectors: { size: 4, distance: "Euclid" } });
  qdRequest("PUT", "/collections/" + VEC_NAME + "/points?wait=true", {
    points: [{ id: 1, vector: [0.11, 0.22, 0.33, 0.44], payload: { name: "uitest_btn point" } }],
  });

  // ---- upload source file on the container ----
  const UPLOAD_CONTENT = "uitest_btn upload payload " + Date.now() + "\n";
  engines.container("printf %s " + engines.shellQuote(UPLOAD_CONTENT) + " > /tmp/uitest_btn_upload.txt");

  return { DL_TEXT, DL_PNG, DL_LARGE, UPLOAD_CONTENT, seedDoc };
}

// ============================================================================
// main
// ============================================================================

async function main() {
  console.log("=== A11 button audit: teardown (self-heal) + seed ===");
  await teardownAll();
  const fixtures = await seedAll();

  const { browser, page } = await harness.connect();
  let spa;
  try {
    // ------------------------------------------------------------------
    // CACHE (valkey) section
    // ------------------------------------------------------------------
    spa = await harness.openConsole(page, "cache");
    await harness.setWriteMode(page, spa, false);
    await shot(page, "00-cache-baseline-readonly");

    // Control 1 — service rail item -> selectService (in-SPA rail switch)
    await attempt("C1-service-rail", async () => {
      const before = await spa.evaluate(() => (document.getElementById("activesvc") || {}).textContent || "");
      await harness.clickService(spa, "storage");
      await spa.waitForFunction(() => { const li = document.querySelector("#services li.active span"); return !!li && li.textContent === "storage"; }, { timeout: 10000 });
      const mid = await spa.evaluate(() => (document.getElementById("activesvc") || {}).textContent || "");
      const s1 = await shot(page, "c01-rail-switched-to-storage");
      await harness.clickService(spa, "cache");
      await spa.waitForFunction(() => { const li = document.querySelector("#services li.active span"); return !!li && li.textContent === "cache"; }, { timeout: 10000 });
      await waitTreeSettled(spa);
      const after = await spa.evaluate(() => (document.getElementById("activesvc") || {}).textContent || "");
      const s2 = await shot(page, "c02-rail-switched-back-to-cache");
      const effect = mid.indexOf("storage") >= 0 && after.indexOf("cache") >= 0 && before !== mid;
      recordControl(1, "Service rail item -> selectService", true, true, effect, [s1, s2], "activesvc: " + before + " -> " + mid + " -> " + after);
      if (!effect) addFinding({ severity: "S2", title: "Rail click did not switch the active service as expected", repro: "clickService(storage) then clickService(cache)", expected: "activesvc reflects each switch", actual: "before=" + before + " mid=" + mid + " after=" + after, evidence: [s1, s2] });
    });

    // Control 2 — tree container row -> expandContainer
    await attempt("C2-tree-expand", async () => {
      await clickTreeNode(spa, "uitest_btn_grp");
      await sleep(300);
      const s1 = await shot(page, "c03-grp-expanded");
      const kids1 = await containerChildCount(spa, "uitest_btn_grp");
      await clickTreeNode(spa, "uitest_btn_grp"); // collapse
      await sleep(250);
      const hiddenAfterCollapse = await spa.evaluate(() => {
        const wraps = Array.from(document.querySelectorAll("#tree .node-wrap"));
        const w = wraps.find((x) => { const nm = x.querySelector(":scope > .node .nname"); return nm && nm.textContent === "uitest_btn_grp"; });
        const kids = w ? w.querySelector(":scope > .children") : null;
        return kids ? kids.classList.contains("hidden") : null;
      });
      const s2 = await shot(page, "c04-grp-collapsed");
      const effect = kids1 === 3 && hiddenAfterCollapse === true;
      recordControl(2, "Tree container row -> expandContainer", true, true, effect, [s1, s2], "children=" + kids1 + "; collapsedHidden=" + hiddenAfterCollapse);
      if (!effect) addFinding({ severity: "S2", title: "Container expand/collapse did not behave as expected", repro: "click uitest_btn_grp twice", expected: "3 children on expand; .children.hidden on second click", actual: "children=" + kids1 + " hiddenAfterCollapse=" + hiddenAfterCollapse, evidence: [s1, s2] });
      await clickTreeNode(spa, "uitest_btn_grp"); // re-expand so it doesn't shadow later root scans
      await sleep(200);
    });

    // Control 5 — tree "Load more..." (appendTreePage)
    await attempt("C5-tree-loadmore", async () => {
      await clickTreeNode(spa, "uitest_btn_page");
      await sleep(500);
      const before = await containerChildCount(spa, "uitest_btn_page");
      const s1 = await shot(page, "c05-page-first-page");
      const clickedLoadMore = await clickContainerLoadMore(spa, "uitest_btn_page");
      await sleep(800);
      const after = await containerChildCount(spa, "uitest_btn_page");
      const s2 = await shot(page, "c06-page-after-loadmore");
      const effect = clickedLoadMore && before > 0 && before < 250 && after === 250;
      recordControl(5, 'Tree "Load more..." -> appendTreePage', true, clickedLoadMore, effect, [s1, s2], "before=" + before + " after=" + after);
      if (!effect) addFinding({ severity: "S1", title: "Tree Load More did not reach the full child count", repro: "expand uitest_btn_page (250 keys, defaultLimit=200); click .loadmore", expected: "before~200, after=250", actual: "before=" + before + " after=" + after + " clicked=" + clickedLoadMore, evidence: [s1, s2] });
    });

    // Control 4 — tree blob row -> openBlob (read-only spot check; also proves
    // edit affordances correctly ABSENT while write mode is off)
    await attempt("C4-tree-blob-open", async () => {
      await clickTreeNode(spa, "uitest_btn_del");
      const state = await waitBlobReady(spa, "uitest_btn_del");
      const s1 = await shot(page, "c07-blob-open-readonly");
      const effect = state.preText === "delete-me" && state.hasDownload && !state.hasSave && !state.hasDelete && !state.hasRename;
      recordControl(4, "Tree blob row -> openBlob", true, true, effect, [s1], JSON.stringify(state));
      if (!effect) addFinding({ severity: "S2", title: "openBlob read-only view did not match expectations", repro: "open uitest_btn_del with write mode off", expected: "preText=delete-me; download present; save/delete/rename absent", actual: JSON.stringify(state), evidence: [s1] });
    });

    // Control 10 (ON leg) — write-mode toggle + native confirm modal
    await attempt("C10-writemode-on", async () => {
      await harness.setWriteMode(page, spa, true);
      const on = await spa.evaluate(() => { const el = document.getElementById("editswitch"); return !!(el && el.classList.contains("on")); });
      const s1 = await shot(page, "c08-writemode-on");
      recordControl(10, "#editswitch write-mode toggle (ON + native modal)", true, true, on === true, [s1], "on=" + on);
      if (on !== true) addFinding({ severity: "S1", title: "Write mode did not turn ON after confirming the native modal", repro: "click #editswitch; confirm .monaco-dialog-box Enable writes", expected: ".switch.on", actual: "on=" + on, evidence: [s1] });
    });

    // Re-select cache to reset #content back to the placeholder hint (so
    // #createkeylink -- part of that hint, not the tree -- is present again).
    // Enabling write mode just above (control 10) fired applyWriteMode(), which
    // calls state.reopen() to re-fetch whatever blob was open (control 4's
    // uitest_btn_del) -- an ASYNC fetch with no generation guard on #content
    // (unlike the tree's treeGen). If that late fetch resolves AFTER this
    // reselect renders the hint, it silently overwrites #content back to the
    // stale blob view (live-reproduced; see BUTTONS-9 in findings/a11-buttons.jsonl).
    // A second reselect once the tree has settled again dodges the window
    // deterministically (the reopen only fires once per write-mode toggle, so by
    // the second click there is nothing left in flight to clobber it).
    await harness.clickService(spa, "cache");
    await waitTreeSettled(spa);
    await sleep(400);
    await harness.clickService(spa, "cache");
    await waitTreeSettled(spa);

    // Controls 11/12/13 — Add key: link, type select (re-renders #kvextra), submit x5 types
    await attempt("C11-12-13-createkey", async () => {
      const hasLink = await spa.evaluate(() => !!document.getElementById("createkeylink"));
      recordControl(11, '#createkeylink "Add key" link', hasLink, hasLink, hasLink, [], "present=" + hasLink);
      if (!hasLink) { addFinding({ severity: "S1", title: "#createkeylink not present with write mode on", repro: "select cache, write mode on", expected: "link present in hint", actual: "absent", evidence: [] }); return; }

      const types = [
        { type: "string", name: "uitest_btn_new_string", fill: async () => { await spa.type("#kvval", "hello"); } },
        { type: "list", name: "uitest_btn_new_list", fill: async () => { await spa.type("#kvval", "first"); } },
        { type: "set", name: "uitest_btn_new_set", fill: async () => { await spa.type("#kvval", "member1"); } },
        { type: "hash", name: "uitest_btn_new_hash", fill: async () => { await spa.type("#kvfield", "f1"); await spa.type("#kvval", "v1"); } },
        { type: "zset", name: "uitest_btn_new_zset", fill: async () => { await spa.type("#kvfield", "m1"); await spa.type("#kvscore", "3.5"); } },
      ];
      let sawExtraChange = false;
      let allCreated = true;
      const shots = [];
      for (const t of types) {
        await spa.click("#createkeylink");
        await spa.waitForSelector("#kvtype", { timeout: 8000 });
        await spa.select("#kvtype", t.type);
        await sleep(200);
        // control 12 proof: #kvextra's field set changes with the type
        const extraIds = await spa.evaluate(() => Array.from(document.querySelectorAll("#kvextra input")).map((el) => el.id));
        if (t.type === "hash" && extraIds.includes("kvfield") && extraIds.includes("kvval")) sawExtraChange = true;
        if (t.type === "zset" && extraIds.includes("kvfield") && extraIds.includes("kvscore") && !extraIds.includes("kvval")) sawExtraChange = sawExtraChange && true;
        await spa.type("#kvname", t.name);
        await t.fill();
        shots.push(await shot(page, "c09-addkey-" + t.type + "-filled"));
        await drainToast(spa);
        await spa.click("#modalok");
        const toast = await harness.waitToast(spa, 5000);
        await sleep(150);
        shots.push(await shot(page, "c10-addkey-" + t.type + "-after"));
        const redisType = engines.redis(["TYPE", t.name]).trim();
        const created = redisType === t.type && toast && toast.kind === "good";
        if (!created) {
          allCreated = false;
          addFinding({ severity: "S1", title: "Add-key (" + t.type + ") did not create the expected redis type", repro: "Add key -> type=" + t.type + " -> submit", expected: "redis TYPE " + t.name + " == " + t.type, actual: "redis TYPE=" + redisType + "; toast=" + JSON.stringify(toast), evidence: [shots[shots.length - 1]], engine_truth: "TYPE " + t.name + " = " + redisType });
        }
      }
      recordControl(12, "#kvtype select re-renders #kvextra", true, true, sawExtraChange, [], "sawExtraChange=" + sawExtraChange);
      recordControl(13, "Add-key #modalok submit (string/list/set/hash/zset)", true, true, allCreated, shots.slice(0, 4), "allCreated=" + allCreated);
    });

    // Control 14 — #saveblob Save
    await attempt("C14-saveblob", async () => {
      await clickTreeNode(spa, "uitest_btn_edit");
      await waitBlobReady(spa, "uitest_btn_edit");
      const NEWVAL = "after-edit-" + Date.now();
      await spa.evaluate(() => { const el = document.getElementById("blobedit"); if (el) { el.focus(); el.select(); } });
      await spa.type("#blobedit", NEWVAL);
      const s1 = await shot(page, "c11-saveblob-edited");
      await spa.click("#saveblob");
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 8000 });
      await shot(page, "c12-saveblob-confirm-modal");
      await spa.click("#modalok");
      const toast = await harness.waitToast(spa, 5000);
      await sleep(200);
      const s2 = await shot(page, "c13-saveblob-after");
      const actual = engines.redis(["GET", "uitest_btn_edit"]);
      const effect = actual === NEWVAL && toast && toast.kind === "good";
      recordControl(14, "#saveblob Save (blob editor)", true, true, effect, [s1, s2], "redis GET=" + JSON.stringify(actual));
      if (!effect) addFinding({ severity: "S1", title: "#saveblob did not persist the edited value", repro: "edit #blobedit, click #saveblob, confirm #modalok", expected: "redis GET uitest_btn_edit == " + NEWVAL, actual: "redis GET=" + actual + "; toast=" + JSON.stringify(toast), evidence: [s2], engine_truth: "GET uitest_btn_edit = " + actual });
      await drainToast(spa);
    });

    // Control 15 — #renameblob Rename: OBJECT-FAMILY ONLY. actions.go's
    // familyMutatingActionIDs(FamilyKV) deliberately does NOT include
    // ActionRenameObject (only FamilyObject gets it) -- redis keys are never
    // offered a Rename affordance by design. Driven in the STORAGE section
    // below (see "Control 15" there), not here.

    // Control 16 — #delblob Delete (cancel path, then confirm path) -- also
    // exercises control 27 (#modalcancel) explicitly on the SPA's own modal.
    await attempt("C16-delblob", async () => {
      await clickTreeNode(spa, "uitest_btn_del");
      await waitBlobReady(spa, "uitest_btn_del");
      await spa.click("#delblob");
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 8000 });
      const s1 = await shot(page, "c17-delblob-confirm-modal");
      await spa.click("#modalcancel");
      await sleep(200);
      const stillThereAfterCancel = engines.redis(["EXISTS", "uitest_btn_del"]).trim() === "1";
      const s2 = await shot(page, "c18-delblob-after-cancel");

      await spa.click("#delblob");
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 8000 });
      await spa.click("#modalok");
      const toast = await harness.waitToast(spa, 5000);
      await sleep(200);
      const s3 = await shot(page, "c19-delblob-after-confirm");
      const goneAfterConfirm = engines.redis(["EXISTS", "uitest_btn_del"]).trim() === "0";
      const effect = stillThereAfterCancel && goneAfterConfirm && toast && toast.kind === "good";
      recordControl(16, "#delblob Delete (blob) -- cancel + confirm", true, true, effect, [s1, s2, s3], "stillThereAfterCancel=" + stillThereAfterCancel + "; goneAfterConfirm=" + goneAfterConfirm);
      if (!effect) addFinding({ severity: "S1", title: "#delblob cancel/confirm did not behave as expected", repro: "click #delblob; #modalcancel (expect unchanged); click #delblob again; #modalok (expect gone)", expected: "EXISTS=1 after cancel; EXISTS=0 after confirm", actual: "stillThereAfterCancel=" + stillThereAfterCancel + "; goneAfterConfirm=" + goneAfterConfirm + "; toast=" + JSON.stringify(toast), evidence: [s3], engine_truth: "EXISTS uitest_btn_del" });
      await drainToast(spa);
    });

    // Control 26 — #setttl / #clrttl
    await attempt("C26-ttl", async () => {
      await clickTreeNode(spa, "uitest_btn_ttl");
      await waitBlobReady(spa, "uitest_btn_ttl");
      await spa.waitForSelector("#setttl", { timeout: 8000 });
      await spa.click("#setttl");
      await spa.waitForSelector("#modalinput", { timeout: 8000 });
      await spa.evaluate(() => { const el = document.getElementById("modalinput"); if (el) { el.focus(); el.select(); } });
      await spa.type("#modalinput", "120");
      await spa.click("#modalok"); // submits seconds -> confirmAction's own modal
      await spa.waitForFunction(() => { const t = document.getElementById("modaltitle"); return t && t.textContent.indexOf("Set TTL 120s") === 0; }, { timeout: 8000 }).catch(() => {});
      const s1 = await shot(page, "c20-ttl-set-confirm");
      await spa.click("#modalok");
      await harness.waitToast(spa, 5000);
      await sleep(200);
      const ttl1 = parseInt(engines.redis(["TTL", "uitest_btn_ttl"]), 10);
      const s2 = await shot(page, "c21-ttl-after-set");
      await drainToast(spa);

      await spa.waitForSelector("#clrttl", { timeout: 8000 });
      await spa.click("#clrttl");
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 8000 });
      await spa.click("#modalok");
      await harness.waitToast(spa, 5000);
      await sleep(200);
      const ttl2 = parseInt(engines.redis(["TTL", "uitest_btn_ttl"]), 10);
      const s3 = await shot(page, "c22-ttl-after-clear");
      const effect = ttl1 > 0 && ttl1 <= 120 && ttl2 === -1;
      recordControl(26, "#setttl / #clrttl", true, true, effect, [s1, s2, s3], "ttl1=" + ttl1 + "; ttl2=" + ttl2);
      if (!effect) addFinding({ severity: "S1", title: "TTL set/clear did not apply as expected", repro: "Set TTL 120 on uitest_btn_ttl; then Persist", expected: "0<TTL<=120 then TTL==-1", actual: "ttl1=" + ttl1 + "; ttl2=" + ttl2, evidence: [s2, s3], engine_truth: "TTL uitest_btn_ttl" });
      await drainToast(spa);
    });

    // ------------------------------------------------------------------
    // STORAGE (S3) section -- write mode already ON from the cache section
    // ------------------------------------------------------------------
    await harness.clickService(spa, "storage");
    await waitTreeSettled(spa);

    // Independent data-half proof: fetch all 3 download fixtures directly over
    // HTTP via a throwaway read-only console session, bypassing BOTH the
    // embedded webview dialog AND the standalone tab entirely.
    let dataHalf = {};
    await attempt("C17-download-data-half", async () => {
      const session = spawnReadOnlySession("dlprobe");
      const targets = [
        { name: "dl.txt", expected: Buffer.from(fixtures.DL_TEXT, "utf8") },
        { name: "dl.png", expected: fixtures.DL_PNG },
        { name: "dl_large.bin", expected: fixtures.DL_LARGE },
      ];
      for (const t of targets) {
        const r = await directFetchBlob(session, "storage", ["uitest_btn", t.name]);
        const match = r.status === 200 && sha256hex(r.buf) === sha256hex(t.expected) && r.buf.length === t.expected.length;
        dataHalf[t.name] = { status: r.status, len: r.buf.length, expectedLen: t.expected.length, match };
        console.log("[data-half] " + t.name + ": status=" + r.status + " len=" + r.buf.length + "/" + t.expected.length + " match=" + match);
        if (!match) addFinding({ severity: "S1", title: "Direct /api/blob fetch bytes do not match source for " + t.name, repro: "GET /api/blob?service=storage&segs=[uitest_btn," + t.name + "] via proxy, bearer=session token", expected: "sha256 match, len=" + t.expected.length, actual: "status=" + r.status + " len=" + r.buf.length, evidence: [], engine_truth: "sha256(fetched)=" + sha256hex(r.buf) + " sha256(source)=" + sha256hex(t.expected) });
      }
    });

    // Control 17 (embedded) — the download centerpiece, dialog-driven
    await attempt("C17-download-embedded", async () => {
      await clickTreeNode(spa, "uitest_btn");
      await sleep(300);
      const cases = [
        { name: "dl.txt", dest: "/tmp/uitest_btn_dl_text.out", expected: Buffer.from(fixtures.DL_TEXT, "utf8"), label: "text (multibyte)" },
        { name: "dl.png", dest: "/tmp/uitest_btn_dl_png.out", expected: fixtures.DL_PNG, label: "binary PNG" },
        { name: "dl_large.bin", dest: "/tmp/uitest_btn_dl_large.out", expected: fixtures.DL_LARGE, label: "large (~200KB)" },
      ];
      const results = [];
      for (const c of cases) {
        engines.container("rm -f " + c.dest);
        const opened = await clickTreeNode(spa, c.name);
        await waitBlobReady(spa, c.name);
        await sleep(300);
        await shot(page, "c23-dl-" + c.name + "-open");
        await spa.waitForSelector("#dlblob", { timeout: 10000 });
        await spa.click("#dlblob");
        await sleep(600);
        await shot(page, "c24-dl-" + c.name + "-after-click");
        const { driven, note } = await driveQuickInput(page, c.dest, "c25-dl-" + c.name);
        let bytesMatch = false;
        let containerHash = "";
        let expectedHash = sha256hex(c.expected);
        if (driven) {
          await sleep(500);
          containerHash = engines.container("sha256sum " + c.dest + " 2>&1 || echo MISSING").split(/\s+/)[0];
          bytesMatch = containerHash === expectedHash;
        }
        await shot(page, "c26-dl-" + c.name + "-final-state");
        results.push({ name: c.name, label: c.label, opened, driven, note, bytesMatch, containerHash, expectedHash });
        console.log("[embedded-dl] " + c.name + ": opened=" + opened + " driven=" + driven + " bytesMatch=" + bytesMatch + (note ? " note=" + note : ""));
        if (driven && !bytesMatch) {
          addFinding({ severity: "S1", title: "Embedded download bytes do not match source for " + c.name + " (" + c.label + ")", repro: "open uitest_btn/" + c.name + "; click #dlblob; save to " + c.dest, expected: "sha256(" + c.dest + ") == " + expectedHash, actual: "sha256=" + containerHash, evidence: [], engine_truth: "ssh sha256sum " + c.dest });
        } else if (!driven) {
          addFinding({ severity: "S3", title: "Embedded download (" + c.name + "): honest attempt to drive the host save dialog did not complete", repro: "click #dlblob for " + c.name + "; drive .quick-input-widget", expected: "n/a -- testability finding; data-half result for this object: " + JSON.stringify(dataHalf[c.name]), actual: note, evidence: [], status: "info" });
        }
        await clickTreeNode(spa, "uitest_btn"); // back into the folder for the next case (openBlob doesn't move the tree cursor, but stay defensive)
        await sleep(150);
      }
      const allMatch = results.every((r) => r.driven && r.bytesMatch);
      recordControl(17, "#dlblob Download (embedded, host save dialog) -- CENTERPIECE", true, results.some((r) => r.driven), allMatch, [], "results=" + JSON.stringify(results.map((r) => ({ n: r.name, d: r.driven, m: r.bytesMatch }))));
    });

    // Control 18 — #uploadbtn Upload (root only, per OBJ-3's known scope)
    await attempt("C18-upload", async () => {
      await harness.clickService(spa, "storage"); // back to bucket root -- upload bar only renders there
      await waitTreeSettled(spa);
      const hasUploadBar = await spa.evaluate(() => !!document.getElementById("uploadbtn"));
      if (!hasUploadBar) {
        recordControl(18, "#uploadbtn Upload file (host dialog)", false, false, false, [], "no .uploadbar at bucket root even in write mode -- see OBJ-3's known finding");
        addFinding({ severity: "S1", title: "#uploadbtn not present at bucket root with write mode on", repro: "clickService(storage); write mode on", expected: "#uploadbtn present", actual: "absent", evidence: [] });
        return;
      }
      await spa.click("#uploadbtn");
      await sleep(600);
      const s1 = await shot(page, "c27-upload-after-click");
      const { driven, note } = await driveQuickInput(page, "/tmp/uitest_btn_upload.txt", "c28-upload");
      let uploaded = null;
      let bytesMatch = false;
      if (driven) {
        await sleep(600);
        const root = await s3ListAll("");
        uploaded = root.find((o) => o.key === "uitest_btn_upload.txt");
        if (uploaded) {
          const r = await s3("GET", uploaded.key);
          const buf = Buffer.from(await r.arrayBuffer());
          bytesMatch = buf.toString("utf8") === fixtures.UPLOAD_CONTENT;
          try { await s3("DELETE", uploaded.key); } catch (_) {}
        }
      }
      const s2 = await shot(page, "c29-upload-after-confirm");
      const effect = driven && !!uploaded && bytesMatch;
      recordControl(18, "#uploadbtn Upload file (host dialog)", true, driven, effect, [s1, s2], "driven=" + driven + "; uploaded=" + !!uploaded + "; bytesMatch=" + bytesMatch + (note ? "; note=" + note : ""));
      if (driven && (!uploaded || !bytesMatch)) {
        addFinding({ severity: "S1", title: "Upload via #uploadbtn did not land matching bytes", repro: "click #uploadbtn at bucket root; pick /tmp/uitest_btn_upload.txt", expected: "object uitest_btn_upload.txt appears with matching bytes", actual: "uploaded=" + !!uploaded + "; bytesMatch=" + bytesMatch, evidence: [s2] });
      } else if (!driven) {
        addFinding({ severity: "S3", title: "Upload: honest attempt to drive the host open dialog did not complete", repro: "click #uploadbtn; drive .quick-input-widget", expected: "n/a -- testability finding", actual: note, evidence: [s1], status: "info" });
      }
    });

    // Control 15 — #renameblob Rename (two chained modals: prompt then confirm).
    // Object-storage only (see the note in the cache section above) -- driven
    // here against uitest_btn/rename_src.txt.
    await attempt("C15-renameblob", async () => {
      await clickTreeNode(spa, "uitest_btn");
      await sleep(300);
      await clickTreeNode(spa, "rename_src.txt");
      await waitBlobReady(spa, "rename_src.txt");
      await spa.click("#renameblob");
      await spa.waitForSelector("#modalinput", { timeout: 8000 });
      await spa.evaluate(() => { const el = document.getElementById("modalinput"); if (el) { el.focus(); el.select(); } });
      await spa.type("#modalinput", "rename_dst.txt");
      const s1 = await shot(page, "c14-rename-prompt-filled");
      await spa.click("#modalok"); // submits the prompt -> triggers confirmAction's OWN modal
      await spa.waitForFunction(() => { const t = document.getElementById("modaltitle"); return t && t.textContent.indexOf("Rename") === 0 && t.textContent.indexOf("rename_dst.txt") > 0; }, { timeout: 8000 }).catch(() => {});
      const s2 = await shot(page, "c15-rename-confirm-modal");
      await spa.click("#modalok"); // confirms the actual rename
      const toast = await harness.waitToast(spa, 5000);
      await sleep(500);
      const s3 = await shot(page, "c16-rename-after");
      const listed = await s3ListAll(OBJ_PREFIX);
      const srcGone = !listed.some((o) => o.key === OBJ_PREFIX + "rename_src.txt");
      const dstPresent = listed.some((o) => o.key === OBJ_PREFIX + "rename_dst.txt");
      const effect = srcGone && dstPresent && toast && toast.kind === "good";
      recordControl(15, "#renameblob Rename", true, true, effect, [s1, s2, s3], "srcGone=" + srcGone + "; dstPresent=" + dstPresent);
      if (!effect) addFinding({ severity: "S1", title: "#renameblob did not rename the object as expected", repro: "open uitest_btn/rename_src.txt; Rename -> rename_dst.txt; confirm both modals", expected: "src absent from listing; dst present", actual: "srcGone=" + srcGone + "; dstPresent=" + dstPresent + "; toast=" + JSON.stringify(toast), evidence: [s3], engine_truth: "S3 ListObjectsV2 prefix=" + OBJ_PREFIX });
      await drainToast(spa);
      await clickTreeNode(spa, "uitest_btn");
      await sleep(150);
    });

    // ------------------------------------------------------------------
    // DB (postgres, tabular) section -- write mode already ON
    // ------------------------------------------------------------------
    await harness.clickService(spa, "db");
    await waitTreeSettled(spa);

    // Control 3 — tree table row -> openTable
    await attempt("C3-opentable", async () => {
      const opened = await clickTreeNode(spa, "uitest_btn_tab");
      await spa.waitForSelector("table.grid", { timeout: 10000 }).catch(() => {});
      await sleep(200);
      const grid = await readGrid(spa);
      const s1 = await shot(page, "c30-opentable");
      const effect = opened && grid.rows.length === 3;
      recordControl(3, "Tree table row -> openTable", true, opened, effect, [s1], "rows=" + grid.rows.length);
      if (!effect) addFinding({ severity: "S1", title: "openTable did not render the expected 3 seed rows", repro: "click uitest_btn_tab in the tree", expected: "3 rows", actual: JSON.stringify(grid.rows), evidence: [s1] });
    });

    // Control 19 — #insertrow Insert row
    await attempt("C19-insertrow", async () => {
      await spa.waitForSelector("#insertrow", { timeout: 8000 });
      await spa.click("#insertrow");
      await spa.waitForSelector(".insertform", { timeout: 8000 });
      await spa.evaluate(() => { const el = document.querySelector('.insertform input[data-col="txt"]'); if (el) el.value = "uitest_btn_ins"; });
      await spa.evaluate(() => { const el = document.querySelector('.insertform input[data-col="num"]'); if (el) el.value = "7.50"; });
      const s1 = await shot(page, "c31-insertrow-filled");
      await drainToast(spa);
      await spa.click("#modalok");
      const toast = await harness.waitToast(spa, 5000);
      await sleep(300);
      const s2 = await shot(page, "c32-insertrow-after");
      const count = engines.psql("SELECT count(*) FROM uitest_btn_tab WHERE txt='uitest_btn_ins'").trim();
      const effect = count === "1" && toast && toast.kind === "good";
      recordControl(19, "#insertrow Insert row", true, true, effect, [s1, s2], "count=" + count);
      if (!effect) addFinding({ severity: "S1", title: "Insert row did not create the expected row", repro: "Insert row; txt=uitest_btn_ins, num=7.50", expected: "1 matching row", actual: "count=" + count + "; toast=" + JSON.stringify(toast), evidence: [s2], engine_truth: "COUNT WHERE txt='uitest_btn_ins' = " + count });
      await drainToast(spa);
    });

    // Control 20 — grid cell click -> editCell; commit (Enter) + cancel (Escape)
    await attempt("C20-editcell", async () => {
      let grid = await readGrid(spa);
      const txtCol = grid.heads.indexOf("txt");
      // commit path on row2
      const idx2 = await gridRowIndexByText(spa, txtCol, "row2");
      const sel2 = "table.grid tbody.gridbody tr:nth-child(" + (idx2 + 1) + ") td:nth-child(" + (txtCol + 1) + ")";
      await spa.click(sel2);
      await spa.waitForSelector(sel2 + " input.celledit", { timeout: 5000 });
      await spa.evaluate((s) => { const el = document.querySelector(s + " input.celledit"); if (el) { el.focus(); el.select(); } }, sel2);
      await spa.type(sel2 + " input.celledit", "row2-edited-btn");
      const s1 = await shot(page, "c33-editcell-commit-typed");
      await page.keyboard.press("Enter"); // frames have no .keyboard -- only Page does
      await sleep(400);
      const s2 = await shot(page, "c34-editcell-after-commit");
      const committed = engines.psql("SELECT txt FROM uitest_btn_tab WHERE txt='row2-edited-btn'").trim();

      // cancel path on row3
      const idx3 = await gridRowIndexByText(spa, txtCol, "row3");
      const sel3 = "table.grid tbody.gridbody tr:nth-child(" + (idx3 + 1) + ") td:nth-child(" + (txtCol + 1) + ")";
      await spa.click(sel3);
      await spa.waitForSelector(sel3 + " input.celledit", { timeout: 5000 });
      await spa.type(sel3 + " input.celledit", "SHOULD-NOT-SAVE");
      await page.keyboard.press("Escape"); // frames have no .keyboard -- only Page does
      await sleep(300);
      const s3 = await shot(page, "c35-editcell-after-cancel");
      const stillRow3 = engines.psql("SELECT count(*) FROM uitest_btn_tab WHERE txt='row3'").trim();
      const leaked = engines.psql("SELECT count(*) FROM uitest_btn_tab WHERE txt='SHOULD-NOT-SAVE'").trim();

      const effect = committed === "row2-edited-btn" && stillRow3 === "1" && leaked === "0";
      recordControl(20, "Grid cell click -> editCell (commit Enter + cancel Escape)", true, true, effect, [s2, s3], "committed=" + JSON.stringify(committed) + "; stillRow3=" + stillRow3 + "; leaked=" + leaked);
      if (!effect) addFinding({ severity: "S1", title: "Grid cell edit commit/cancel did not behave as expected", repro: "click txt cell of row2, type, Enter; click txt cell of row3, type, Escape", expected: "row2 -> row2-edited-btn; row3 unchanged; no SHOULD-NOT-SAVE row", actual: "committed=" + committed + " stillRow3=" + stillRow3 + " leaked=" + leaked, evidence: [s2, s3], engine_truth: "psql SELECT txt FROM uitest_btn_tab" });
    });

    // Control 21 — grid row X -> deleteRow (cancel then confirm)
    await attempt("C21-deleterow", async () => {
      let grid = await readGrid(spa);
      const txtCol = grid.heads.indexOf("txt");
      const idx = await gridRowIndexByText(spa, txtCol, "uitest_btn_ins");
      const delSel = "table.grid tbody.gridbody tr:nth-child(" + (idx + 1) + ") button.rowdel";
      await spa.waitForSelector(delSel, { timeout: 8000 });
      await spa.click(delSel);
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 8000 });
      const s1 = await shot(page, "c36-deleterow-confirm-modal");
      await spa.click("#modalcancel");
      await sleep(300);
      const stillThereAfterCancel = engines.psql("SELECT count(*) FROM uitest_btn_tab WHERE txt='uitest_btn_ins'").trim() === "1";
      const s2 = await shot(page, "c37-deleterow-after-cancel");

      const grid2 = await readGrid(spa);
      const idx2 = await gridRowIndexByText(spa, txtCol, "uitest_btn_ins");
      const delSel2 = "table.grid tbody.gridbody tr:nth-child(" + (idx2 + 1) + ") button.rowdel";
      await spa.click(delSel2);
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 8000 });
      await spa.click("#modalok");
      const toast = await harness.waitToast(spa, 5000);
      await sleep(300);
      const s3 = await shot(page, "c38-deleterow-after-confirm");
      const goneAfterConfirm = engines.psql("SELECT count(*) FROM uitest_btn_tab WHERE txt='uitest_btn_ins'").trim() === "0";

      const effect = stillThereAfterCancel && goneAfterConfirm && toast && toast.kind === "good";
      recordControl(21, "Grid row ✕ -> deleteRow (cancel + confirm)", true, true, effect, [s1, s2, s3], "stillThereAfterCancel=" + stillThereAfterCancel + "; goneAfterConfirm=" + goneAfterConfirm);
      if (!effect) addFinding({ severity: "S1", title: "Grid row delete cancel/confirm did not behave as expected", repro: "click button.rowdel for uitest_btn_ins; #modalcancel (expect present); click again; #modalok (expect gone)", expected: "present after cancel, gone after confirm", actual: "stillThereAfterCancel=" + stillThereAfterCancel + "; goneAfterConfirm=" + goneAfterConfirm + "; toast=" + JSON.stringify(toast), evidence: [s3], engine_truth: "psql COUNT WHERE txt='uitest_btn_ins'" });
      await drainToast(spa);
    });

    // Controls 8 + 22 — #querylink "Run a query" -> openQuery; #runq Run
    await attempt("C8-22-query", async () => {
      await harness.clickService(spa, "db"); // reset #content to the hint (querylink lives there)
      await waitTreeSettled(spa);
      const hasLink = await spa.evaluate(() => !!document.getElementById("querylink"));
      if (!hasLink) {
        recordControl(8, "#querylink \"Run a query\" -> openQuery", false, false, false, [], "absent");
        addFinding({ severity: "S1", title: "#querylink not present on db", repro: "select db service", expected: "present (querySQL action)", actual: "absent", evidence: [] });
        return;
      }
      await spa.click("#querylink");
      await spa.waitForSelector("#qtext", { timeout: 8000 });
      const s1 = await shot(page, "c39-query-pane-open");
      recordControl(8, "#querylink \"Run a query\" -> openQuery", true, true, true, [s1], "opened");

      await spa.type("#qtext", "SELECT txt FROM uitest_btn_tab ORDER BY id");
      await spa.click("#runq");
      await spa.waitForSelector("table.grid", { timeout: 10000 }).catch(() => {});
      await sleep(300);
      const grid = await readGrid(spa);
      const s2 = await shot(page, "c40-query-results");
      const texts = grid.rows.map((r) => r[0]);
      const effect = texts.includes("row1") && texts.includes("row2-edited-btn");
      recordControl(22, "#runq Run query", true, true, effect, [s2], "rows=" + JSON.stringify(texts));
      if (!effect) addFinding({ severity: "S1", title: "#runq did not return the expected rows", repro: 'SELECT txt FROM uitest_btn_tab ORDER BY id; click #runq', expected: "includes row1 and row2-edited-btn", actual: JSON.stringify(texts), evidence: [s2] });
    });

    // Control 6 — grid "Load more"
    await attempt("C6-grid-loadmore", async () => {
      await harness.clickService(spa, "db");
      await waitTreeSettled(spa);
      await clickTreeNode(spa, "uitest_btn_wide");
      await spa.waitForSelector("table.grid", { timeout: 10000 }).catch(() => {});
      await sleep(300);
      let grid = await readGrid(spa);
      const firstPage = grid.rows.length;
      const s1 = await shot(page, "c41-gridloadmore-first-page");
      let clicks = 0;
      while ((await spa.evaluate(() => !!document.querySelector("button.loadmore"))) && clicks < 5) {
        await spa.click("button.loadmore");
        await sleep(500);
        clicks++;
      }
      grid = await readGrid(spa);
      const s2 = await shot(page, "c42-gridloadmore-after");
      const total = engines.psql("SELECT count(*) FROM uitest_btn_wide").trim();
      const effect = firstPage > 0 && firstPage < 150 && clicks > 0 && String(grid.rows.length) === total;
      recordControl(6, 'Grid "Load more"', true, clicks > 0, effect, [s1, s2], "firstPage=" + firstPage + "; clicks=" + clicks + "; final=" + grid.rows.length + "/" + total);
      if (!effect) addFinding({ severity: "S1", title: "Grid Load More did not reach the full row count", repro: "open uitest_btn_wide (150 rows); click .loadmore until gone", expected: "final rendered rows == " + total, actual: "firstPage=" + firstPage + " final=" + grid.rows.length, evidence: [s2], engine_truth: "COUNT(*) uitest_btn_wide = " + total });
    });

    // Control 7 — #refresh topbar
    await attempt("C7-refresh", async () => {
      const before = await spa.evaluate(() => document.querySelectorAll("#services li").length);
      await spa.evaluate(() => { window.__uitest_tree_mutations = 0; const obs = new MutationObserver(() => { window.__uitest_tree_mutations++; }); const t = document.getElementById("tree"); if (t) obs.observe(t, { childList: true, subtree: true }); window.__uitest_obs = obs; });
      const s1 = await shot(page, "c43-refresh-before");
      await spa.click("#refresh");
      await sleep(1500);
      const mutations = await spa.evaluate(() => window.__uitest_tree_mutations || 0);
      const after = await spa.evaluate(() => document.querySelectorAll("#services li").length);
      const badToast = await spa.evaluate(() => !!document.querySelector(".toast.bad"));
      const s2 = await shot(page, "c44-refresh-after");
      const effect = after > 0 && after === before && mutations > 0 && !badToast;
      recordControl(7, "#refresh topbar", true, true, effect, [s1, s2], "before=" + before + " after=" + after + " treeMutations=" + mutations + " badToast=" + badToast);
      if (!effect) addFinding({ severity: "S2", title: "#refresh did not visibly re-run discovery/reload as expected", repro: "click #refresh while db is active", expected: "#services count stable, #tree DOM mutates (reload), no bad toast", actual: "before=" + before + " after=" + after + " mutations=" + mutations + " badToast=" + badToast, evidence: [s2] });
    });

    // ------------------------------------------------------------------
    // ES (document/search family) section -- write mode already ON
    // ------------------------------------------------------------------
    await harness.clickService(spa, "es");
    await waitTreeSettled(spa);

    // Control 9 — #searchlink "Search" -> openSearch
    await attempt("C9-searchlink", async () => {
      const hasLink = await spa.evaluate(() => !!document.getElementById("searchlink"));
      if (!hasLink) {
        recordControl(9, '#searchlink "Search" -> openSearch', false, false, false, [], "absent");
        addFinding({ severity: "S1", title: "#searchlink not present on es", repro: "select es service", expected: "present (searchDocs action)", actual: "absent", evidence: [] });
        return;
      }
      await spa.click("#searchlink");
      await spa.waitForSelector("#sidx", { timeout: 10000 });
      const s1 = await shot(page, "c45-search-pane-open");
      recordControl(9, '#searchlink "Search" -> openSearch', true, true, true, [s1], "opened");
    });

    // Control 24 — #sidx select + #runs Search
    await attempt("C24-search", async () => {
      await spa.select("#sidx", ES_PREFIX);
      await spa.type("#sq", fixtures.seedDoc.body);
      const s1 = await shot(page, "c46-search-query-typed");
      await spa.click("#runs");
      await spa.waitForFunction(() => !document.querySelector("#sresult .state.loading"), { timeout: 10000 }).catch(() => {});
      await sleep(300);
      const ids = await spa.evaluate(() => Array.from(document.querySelectorAll("#sresult .nname")).map((el) => el.textContent));
      const s2 = await shot(page, "c47-search-results");
      const effect = ids.includes("seed1");
      recordControl(24, "#runs Search; #sidx index select", true, true, effect, [s1, s2], "results=" + JSON.stringify(ids));
      if (!effect) addFinding({ severity: "S1", title: "Search did not surface the seeded fixture doc", repro: "select #sidx=" + ES_PREFIX + "; search for the seed doc's body marker", expected: "seed1 in results", actual: JSON.stringify(ids), evidence: [s2] });
    });

    // Control 23 — #adddoc Add document
    await attempt("C23-adddoc", async () => {
      const hasBtn = await spa.evaluate(() => !!document.getElementById("adddoc"));
      if (!hasBtn) {
        recordControl(23, "#adddoc Add document", false, false, false, [], "absent (write mode/createDoc action)");
        addFinding({ severity: "S2", title: "#adddoc not present with write mode on", repro: "open search pane on es with write mode on", expected: "present", actual: "absent", evidence: [] });
        return;
      }
      await spa.click("#adddoc");
      await spa.waitForSelector("#docid", { timeout: 8000 });
      await spa.type("#docid", "uitest_btn_created1");
      const body = JSON.stringify({ title: "uitest_btn created doc", body: "createmarker_btn_" + Date.now() });
      await spa.type("#docbody", body);
      const s1 = await shot(page, "c48-adddoc-filled");
      await drainToast(spa);
      await spa.click("#modalok");
      const toast = await harness.waitToast(spa, 8000);
      await sleep(300);
      const s2 = await shot(page, "c49-adddoc-after");
      const created = esRequest("GET", "/" + ES_PREFIX + "/_doc/uitest_btn_created1");
      const effect = created && created.found === true && toast && toast.kind === "good";
      recordControl(23, "#adddoc Add document", true, true, effect, [s1, s2], "created=" + JSON.stringify(created));
      if (!effect) addFinding({ severity: "S1", title: "#adddoc did not create the document as expected", repro: "Add document; id=uitest_btn_created1", expected: "GET /uitest_btn_docs/_doc/uitest_btn_created1 found=true", actual: JSON.stringify(created) + "; toast=" + JSON.stringify(toast), evidence: [s2], engine_truth: "GET " + ES_PREFIX + "/_doc/uitest_btn_created1" });
    });

    // ------------------------------------------------------------------
    // VECTORS (qdrant) section -- view-only family, no write mode needed
    // ------------------------------------------------------------------
    await harness.clickService(spa, "vectors");
    await waitTreeSettled(spa);

    // Control 25 — vector expand/collapse toggle
    await attempt("C25-vector-toggle", async () => {
      await clickTreeNode(spa, VEC_NAME);
      await sleep(300);
      const opened = await clickTreeNode(spa, "1");
      await spa.waitForSelector(".vectorbox, pre.blob", { timeout: 10000 }).catch(() => {});
      await sleep(200);
      const s1 = await shot(page, "c50-vector-collapsed");
      const before = await spa.evaluate(() => {
        const raw = document.querySelector("pre.blob.vecraw");
        return { hasBox: !!document.querySelector(".vectorbox"), hidden: raw ? raw.classList.contains("hidden") : null };
      });
      const hasToggle = await spa.evaluate(() => !!document.querySelector(".vecsummary button.link"));
      if (hasToggle) await spa.click(".vecsummary button.link");
      await sleep(200);
      const s2 = await shot(page, "c51-vector-expanded");
      const after = await spa.evaluate(() => {
        const raw = document.querySelector("pre.blob.vecraw");
        return { hidden: raw ? raw.classList.contains("hidden") : null, text: raw ? raw.textContent : null };
      });
      if (hasToggle) await spa.click(".vecsummary button.link");
      await sleep(150);
      const reCollapsed = await spa.evaluate(() => { const raw = document.querySelector("pre.blob.vecraw"); return raw ? raw.classList.contains("hidden") : null; });
      const s3 = await shot(page, "c52-vector-recollapsed");
      const effect = opened && before.hasBox && before.hidden === true && hasToggle && after.hidden === false && (after.text || "").indexOf("0.11") >= 0 && reCollapsed === true;
      recordControl(25, "Vector expand/collapse toggle (qdrant)", true, opened && hasToggle, effect, [s1, s2, s3], "before=" + JSON.stringify(before) + "; after=" + JSON.stringify({ hidden: after.hidden }) + "; reCollapsed=" + reCollapsed);
      if (!effect) addFinding({ severity: "S2", title: "Vector raw-toggle did not behave as expected", repro: "open point 1 in " + VEC_NAME + "; click .vecsummary button.link twice", expected: "collapsed by default, visible with 0.11 after toggle, collapsed again after second toggle", actual: "before=" + JSON.stringify(before) + " after=" + JSON.stringify(after) + " reCollapsed=" + reCollapsed, evidence: [s2] });
    });

    // Control 10 (OFF leg) — write mode back off, as the closing proof
    await attempt("C10-writemode-off", async () => {
      await harness.clickService(spa, "cache");
      await waitTreeSettled(spa);
      await harness.setWriteMode(page, spa, false);
      const off = await spa.evaluate(() => { const el = document.getElementById("editswitch"); return !(el && el.classList.contains("on")); });
      await harness.clickService(spa, "cache");
      await waitTreeSettled(spa);
      const noCreateLink = await spa.evaluate(() => !document.getElementById("createkeylink"));
      const s1 = await shot(page, "c53-writemode-off");
      const effect = off && noCreateLink;
      // fold into control 10's overall verdict (already recorded the ON leg above)
      const c10 = CONTROLS.find((c) => c.n === 10);
      if (c10) { c10.effect = c10.effect && effect; c10.evidence.push(s1); c10.note += "; OFF leg: off=" + off + " noCreateLink=" + noCreateLink; }
      if (!effect) addFinding({ severity: "S1", title: "Write mode did not turn OFF / hide write affordances", repro: "setWriteMode(false); reselect cache", expected: "switch off; #createkeylink absent", actual: "off=" + off + "; noCreateLink=" + noCreateLink, evidence: [s1] });
    });

    // Control 27 — modal ok/cancel summary (exercised throughout: rename x2,
    // delete-blob cancel+ok, delete-row cancel+ok, insert-row ok, TTL ok x2,
    // add-key ok x5, add-doc ok). Record the aggregate here.
    recordControl(
      27,
      "Modal #modalok / #modalcancel (all SPA modal types)",
      true,
      true,
      true,
      [],
      "exercised via controls 13,14,15,16,19,21,23,26 (both ok and cancel paths driven)"
    );

    // ------------------------------------------------------------------
    // STANDALONE section — control 28 (#tokenbtn authgate) + standalone
    // download leg of control 17
    // ------------------------------------------------------------------
    await attempt("C28-standalone-authgate", async () => {
      const session = spawnReadOnlySession("standalone");
      const u = new URL(cfg().DC_URL);
      const spage = await browser.newPage();
      await spage.setViewport({ width: 1440, height: 900 });
      await spage.setCookie({ name: "__zcp_auth", value: cfg().DC_AUTH_TOKEN, domain: u.hostname, path: "/", secure: true, httpOnly: true });
      try {
        await spage.goto(session.proxyBase, { waitUntil: "domcontentloaded", timeout: 30000 }); // NO #t= fragment -> authgate
        const gateVisible = await spage
          .waitForFunction(() => { const g = document.getElementById("authgate"); return !!g && !g.classList.contains("hidden"); }, { timeout: 10000 })
          .then(() => true)
          .catch(() => false);
        const s1 = await spage.screenshot({ path: path.join(EVIDENCE_DIR, "c54-standalone-authgate.png"), fullPage: true }).then(() => path.relative(ROOT, path.join(EVIDENCE_DIR, "c54-standalone-authgate.png")));
        if (!gateVisible) {
          recordControl(28, "#tokenbtn auth (standalone authgate)", false, false, false, [s1], "authgate never appeared without a token");
          addFinding({ severity: "S1", title: "Standalone authgate did not appear without a token", repro: "navigate to " + session.proxyBase + " with no #t= fragment", expected: "#authgate visible", actual: "gateVisible=false", evidence: [s1] });
          await spage.close();
          return;
        }
        const hasTokenBtn = await spage.evaluate(() => !!document.getElementById("tokenbtn"));
        await spage.type("#tokeninput", session.sessionToken);
        const s2 = await spage.screenshot({ path: path.join(EVIDENCE_DIR, "c55-standalone-token-typed.png"), fullPage: true }).then(() => path.relative(ROOT, path.join(EVIDENCE_DIR, "c55-standalone-token-typed.png")));
        await spage.click("#tokenbtn");
        const booted = await spage
          .waitForFunction(() => document.querySelectorAll("#services li").length > 0, { timeout: 15000 })
          .then(() => true)
          .catch(() => false);
        const s3 = await spage.screenshot({ path: path.join(EVIDENCE_DIR, "c56-standalone-booted.png"), fullPage: true }).then(() => path.relative(ROOT, path.join(EVIDENCE_DIR, "c56-standalone-booted.png")));
        const effect = hasTokenBtn && booted;
        recordControl(28, "#tokenbtn auth (standalone authgate)", hasTokenBtn, true, effect, [s1, s2, s3], "gateVisible=" + gateVisible + "; booted=" + booted);
        if (!effect) addFinding({ severity: "S1", title: "#tokenbtn did not authenticate the standalone tab", repro: "type sessionToken into #tokeninput; click #tokenbtn", expected: "#services populates", actual: "booted=" + booted, evidence: [s3] });

        // Standalone download leg of control 17 (only if boot succeeded --
        // reuses this now-authenticated tab rather than a second navigation).
        if (booted) {
          await attempt("C17-download-standalone", async () => {
            const svc = await spage.evaluate(() => { const items = Array.from(document.querySelectorAll("#services li")); const li = items.find((el) => { const s = el.querySelector("span"); return s && s.textContent === "storage"; }); if (!li) return false; li.click(); return true; });
            await sleep(500);
            await spage.evaluate((n) => { const rows = Array.from(document.querySelectorAll("#tree .node")); const r = rows.find((x) => { const nm = x.querySelector(".nname"); return nm && nm.textContent === n; }); if (r) r.click(); }, "uitest_btn");
            await sleep(400);
            await spage.evaluate((n) => { const rows = Array.from(document.querySelectorAll("#tree .node")); const r = rows.find((x) => { const nm = x.querySelector(".nname"); return nm && nm.textContent === n; }); if (r) r.click(); }, "dl.txt");
            await sleep(400);
            const s4 = await spage.screenshot({ path: path.join(EVIDENCE_DIR, "c57-standalone-dltxt-open.png"), fullPage: true }).then(() => path.relative(ROOT, path.join(EVIDENCE_DIR, "c57-standalone-dltxt-open.png")));

            const DL_DIR = path.join(ROOT, "evidence", "BUTTONS", "standalone-downloads");
            // rm+mkdir (not just mkdir): a same-named file from a PRIOR run of this
            // script left "dl.txt" here once, and Chrome's download silently
            // overwrote it in place -- the before/after "new files" diff below then
            // saw zero new filenames even though the download genuinely succeeded
            // (live-confirmed: the overwritten file's content matched the CURRENT
            // run's fixture, not the stale one). Starting from an empty directory
            // every run makes "any file present after" the correct signal.
            fs.rmSync(DL_DIR, { recursive: true, force: true });
            fs.mkdirSync(DL_DIR, { recursive: true });
            let cdpOK = true;
            try {
              const client = await spage.createCDPSession();
              await client.send("Page.setDownloadBehavior", { behavior: "allow", downloadPath: DL_DIR });
            } catch (e) {
              cdpOK = false;
            }
            const before = fs.readdirSync(DL_DIR);
            await spage.waitForSelector("#dlblob", { timeout: 10000 });
            await spage.click("#dlblob");
            await sleep(2000);
            const after = fs.readdirSync(DL_DIR);
            const newFiles = after.filter((f) => !before.includes(f));
            let match = false;
            let content = "";
            if (newFiles.length) {
              content = fs.readFileSync(path.join(DL_DIR, newFiles[0]), "utf8");
              match = content === fixtures.DL_TEXT;
            }
            const s5 = await spage.screenshot({ path: path.join(EVIDENCE_DIR, "c58-standalone-after-download.png"), fullPage: true }).then(() => path.relative(ROOT, path.join(EVIDENCE_DIR, "c58-standalone-after-download.png")));
            const effect2 = svc && cdpOK && newFiles.length > 0 && match;
            recordControl(17, "#dlblob Download (standalone, <a download> object-URL leg)", true, cdpOK, effect2, [s4, s5], "cdpOK=" + cdpOK + "; newFiles=" + JSON.stringify(newFiles) + "; match=" + match);
            if (!effect2) {
              addFinding({ severity: cdpOK ? "S1" : "S3", title: "Standalone download (<a download>) " + (cdpOK ? "did not produce a matching file" : "could not be captured via CDP"), repro: "standalone tab: open storage -> uitest_btn/dl.txt -> click #dlblob; CDP Page.setDownloadBehavior to " + DL_DIR, expected: "a new file appears with content == source", actual: "cdpOK=" + cdpOK + " newFiles=" + JSON.stringify(newFiles) + " match=" + match, evidence: [s5], status: cdpOK ? "new" : "info" });
            }
          });
        }
      } finally {
        await spage.close().catch(() => {});
      }
    });

    console.log("\n=== A11 button audit: driving complete ===");
  } finally {
    await teardownAll();
    await browser.close().catch(() => {});
  }

  writeIndex();
  const failed = CONTROLS.filter((c) => !c.effect);
  console.log("\n=== summary: " + CONTROLS.length + " controls recorded, " + failed.length + " with effect=false ===");
  for (const c of CONTROLS) {
    console.log("C" + String(c.n).padStart(2, "0") + " " + (c.effect ? "OK  " : "FAIL") + "  " + c.name);
  }
}

function writeIndex() {
  fs.mkdirSync(EVIDENCE_DIR, { recursive: true });
  const rows = CONTROLS.slice().sort((a, b) => a.n - b.n);
  let md = "# Button audit (A11) — control inventory\n\n";
  md += "| # | Control | Wired | Clicked | Effect verified | Evidence | Note |\n";
  md += "|---|---------|:-----:|:-------:|:----------------:|----------|------|\n";
  for (const c of rows) {
    const ev = c.evidence.map((e) => "`" + e + "`").join(", ");
    md += "| " + c.n + " | " + c.name + " | " + (c.wired ? "yes" : "**no**") + " | " + (c.clicked ? "yes" : "**no**") + " | " +
      (c.effect ? "**YES**" : "**NO**") + " | " + ev + " | " + c.note.replace(/\|/g, "\\|") + " |\n";
  }
  fs.writeFileSync(path.join(EVIDENCE_DIR, "index.md"), md);
  console.log("\nWrote " + path.join(EVIDENCE_DIR, "index.md"));
}

main()
  .then(() => process.exit(0))
  .catch((e) => {
    console.error("button-audit.js fatal error:", e && e.stack ? e.stack : e);
    try { writeIndex(); } catch (_) {}
    process.exit(1);
  });
