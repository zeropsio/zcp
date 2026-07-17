"use strict";

// R2-* (family "round2") — A15 fan-out: live regression coverage for the
// round-2 fix bundle, written BEFORE that code is deployed to the container
// (see README.md's data-discipline section — every key/table here starts
// "uitest_r2", this file's EXCLUSIVE prefix, never touched by any other
// scenario file).
//
// The round-2 source (read locally from console/webui/dist/app.js +
// console/provider/tabular/errors.go, NOT yet live) carries a dense set of
// fix-tag comments (B1-B9, C1-C7) covering: the confirm-modal B1 lifecycle
// (a rejection keeps the modal open with typed input intact, instead of the
// old hideModal()-before-await shape TAB-6/a2.jsonl already caught live),
// B2 keyboard/focus (Escape + backdrop-click both cancel), B3 the read-only
// full-value cell view, B6 an honest row-identity delete-confirm, B7 the SQL
// NULL affordance, B8 per-folder (not just bucket-root) upload bars, B9
// qdrant's search-link gated off, and the tabular provider's ErrInvalid vs
// ErrUpstream reclassification (ARCH: engineErr in provider/tabular/errors.go)
// so a SQL syntax typo reads as an honest rejection instead of "the platform
// broke" (UX-A10-01-upstream-mislabel, already an open finding against the
// CURRENT deployment).
//
// R2-1 (object rename) and R2-2 (KV TTL set/persist) are DIFFERENT in kind
// from the rest: the brief frames both as "already works on the live
// (round-1) container, regressed transiently in round-2's WIP, and was
// fixed before this deploy" -- i.e. round-1 KV-4 already proved TTL live,
// and rename is a pre-existing flow. Both are therefore unconditional hard
// assertions (same shape as CORE-1/KV-4): they must pass BOTH before and
// after the deploy, no behavior-change expected either way.
//
// R2-3 through R2-10 test round-2-ONLY behavior that the current live
// container does not have yet. Per the brief, every one of those is
// structured around a single switch:
//
//   const POST_DEPLOY = process.env.UITEST_R2_POST === "1";
//
// When unset (the pre-deploy dry run), an assertion that would only hold
// once the new code is live is recorded as an INFORMATIONAL addFinding
// (severity S3, status "info") describing the observed (old) behavior --
// never a thrown error, so `node run.js --family round2` stays a clean
// PASS-with-findings run against the CURRENT deployment. When set (run
// right after the deploy), the SAME check is promoted to a real finding
// (higher severity, status "new") if the new behavior did not land. A
// handful of invariants that must hold regardless of which code is live
// (a rejected mutation must never actually apply; Escape/backdrop must
// never fire a request even if the close animation itself isn't fixed yet)
// are asserted unconditionally on both sides of that switch.
//
// Self-contained per the fan-out convention (core.js's own comment on this):
// this file keeps its own small helper set (tree reveal, S3 signer, modal
// state readers) rather than importing another scenario file's internals --
// kv-object.js/tabular.js/document.js all export {} for the same reason.

const crypto = require("crypto");
const runner = require("../lib/runner");
const { loadConfig } = require("../lib/config");

const POST_DEPLOY = process.env.UITEST_R2_POST === "1";

const DB_SERVICE = "db";
const CACHE_SERVICE = "cache";
const OBJECT_SERVICE = "storage";
const VECTORS_SERVICE = "vectors"; // qdrant

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// ============================================================================
// shared local helpers (self-contained -- see header)
// ============================================================================

// waitTreeSettledLocal + enterService mirror kv-object.js's waitTreeSettled/
// openService exactly: app.js's write-mode toggle does NOT reactively
// re-render the top-level service hint, so a caller that just flipped write
// mode needs a forced selectService() (via clickService) before hint-level
// affordances (Add key, upload bar, Search link) reflect the new state --
// and that forced reload can itself leave #tree mid-reload for a moment.
async function waitTreeSettledLocal(frame, timeoutMs) {
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

// closeStrayModal defensively closes any #modal left open by a PRIOR
// scenario before this one starts touching the SPA. This matters
// specifically because R2-4 deliberately probes whether Escape/backdrop
// close the modal -- pre-deploy they DON'T (see R2-4's own findings), so a
// naive run would leave #modal (position:fixed, inset:0, z-index:20 --
// a full-viewport overlay) sitting open and covering everything in the
// frame, including #editswitch. Puppeteer's click() dispatches a real mouse
// event at the target's bounding-box center, which the browser then
// hit-tests normally: with the overlay on top, a click "on #editswitch"
// actually lands on the modal backdrop instead, silently no-opping the
// switch and hanging setWriteMode's settle-wait (caught live: this exact
// chain corrupted R2-5/R2-6/R2-7 in the first dry run before this guard was
// added). Never assumes Escape/backdrop are the working close path (that's
// what R2-4 is testing) -- uses #modalcancel, which TAB-7 already proved
// live as a deterministic, NEVER-submits close (cancelModal() only calls
// hideModal(), no API call). Falls back to #modalok only when Cancel is
// itself hidden by design (a viewOnly info dialog, e.g. R2-5's full-value
// view), whose OK is a documented no-op (showModal's viewOnly onOK is an
// empty async function) -- never clicked on an ordinary mutating modal.
async function closeStrayModal(frame) {
  try {
    const state = await frame.evaluate(() => {
      const m = document.getElementById("modal");
      const c = document.getElementById("modalcancel");
      return {
        open: !!(m && !m.classList.contains("hidden")),
        cancelHidden: c ? c.classList.contains("hidden") : true,
        cancelDisabled: c ? c.disabled : true,
      };
    });
    if (!state.open) return;
    if (!state.cancelHidden && !state.cancelDisabled) {
      await frame.click("#modalcancel");
    } else if (state.cancelHidden) {
      await frame.evaluate(() => {
        const ok = document.getElementById("modalok");
        if (ok && !ok.disabled) ok.click();
      });
    }
    // else: Cancel exists but disabled (mid in-flight "Working…") -- a
    // transient state; leave it rather than fight it, the caller's own
    // waits will surface anything genuinely stuck.
    await frame
      .waitForFunction(
        () => {
          const m = document.getElementById("modal");
          return !!(m && m.classList.contains("hidden"));
        },
        { timeout: 5000 }
      )
      .catch(() => {});
  } catch (_) {
    /* best effort -- if this can't clean up, the caller's own waits surface the real problem */
  }
}

async function enterService(ctx, service, write) {
  const spa = await ctx.harness.openConsole(ctx.page, service);
  await closeStrayModal(spa);
  await ctx.harness.setWriteMode(ctx.page, spa, write);
  if (write) {
    await waitTreeSettledLocal(spa);
    await ctx.harness.clickService(spa, service);
    await waitTreeSettledLocal(spa);
  }
  return ctx.harness.spaFrame ? await ctx.harness.spaFrame(ctx.page) : spa;
}

// revealNode expands tree containers (breadth-ish: each iteration clicks
// whichever collapsed "▸" container it finds first, anywhere in the tree)
// until a node named `name` is visible anywhere, then clicks it. A trimmed
// copy of core.js's revealAndClick, renamed to avoid any confusion with that
// file's own local copy (never imported -- self-contained convention). The
// default `tries` is raised well past core.js's 20: the shared object-storage
// bucket root can carry several other agents' own top-level prefixes at any
// given moment (other scenario families each own a different prefix), and
// this algorithm may have to expand past every one of them before reaching
// "uitest_r2" — see R2-8's comment on the multi-entry root.
async function revealNode(frame, name, tries) {
  tries = tries || 30;
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
      const unexpanded = rows.find((r) => {
        const kindEl = r.querySelector(".kind");
        if (!kindEl || kindEl.textContent !== "▸") return false;
        const wrap = r.closest(".node-wrap");
        return wrap && !wrap.querySelector(":scope > .children");
      });
      if (unexpanded) {
        unexpanded.click();
        return "expanding";
      }
      return "stuck";
    }, name);
    if (res === "clicked") return true;
    await sleep(300);
  }
  return false;
}

// setInput clears an input via the DOM select() API (not a simulated
// multi-click -- unreliable against a pre-filled, already-focused input in
// this environment, per kv-object.js's identical note) and types `value`.
async function setInput(frame, sel, value) {
  await frame.evaluate((s) => {
    const el = document.querySelector(s);
    if (el) {
      el.focus();
      if (typeof el.select === "function") el.select();
    }
  }, sel);
  if (value) await frame.type(sel, value);
}

// readModalState snapshots the SPA's own #modal (never the native VS Code
// dialog) -- hidden/open, title, and the OK/Cancel button posture -- shared
// by every scenario below that needs to tell "modal stayed open" apart from
// "modal closed" without assuming which one is currently true.
async function readModalState(frame) {
  return frame.evaluate(() => {
    const modal = document.getElementById("modal");
    const title = document.getElementById("modaltitle");
    const ok = document.getElementById("modalok");
    const cancel = document.getElementById("modalcancel");
    const err = document.getElementById("modalerr");
    return {
      hidden: modal ? modal.classList.contains("hidden") : null,
      title: title ? title.textContent : null,
      okText: ok ? ok.textContent : null,
      okDanger: ok ? ok.classList.contains("danger") : null,
      cancelHidden: cancel ? cancel.classList.contains("hidden") : null,
      hasInlineErr: !!err,
      inlineErrText: err ? err.textContent : null,
    };
  });
}

// ============================================================================
// object-storage oracle -- hand-rolled AWS SigV4 (MinIO-compatible
// path-style), no deps. Copied+adapted from kv-object.js's proven-live
// implementation (that file exports {}, so this is a deliberate, convention-
// sanctioned duplication, not drift -- see header).
// ============================================================================

const R2_OBJ_PREFIX = "uitest_r2/";

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
async function r2ObjTeardown() {
  try {
    const objs = await s3ListAll(R2_OBJ_PREFIX);
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

// ============================================================================
// redis / postgres teardown
// ============================================================================

function r2KvTeardown(engines) {
  try {
    const out = engines.redis(["KEYS", "uitest_r2*"]);
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

function r2DbTeardown(engines, table) {
  try {
    engines.psql("DROP TABLE IF EXISTS " + table);
  } catch (_) {
    /* best effort */
  }
}

// r2NullState discriminates NULL vs '' vs any-other-value in ONE query --
// psql's unaligned (-A) output renders SQL NULL as an empty field, the SAME
// text an actual empty string produces, so reading `val` directly can never
// tell the two apart. A CASE expression sidesteps that ambiguity entirely.
function r2NullState(engines, table, id) {
  const out = engines.psql(
    "SELECT CASE WHEN val IS NULL THEN 'NULL' WHEN val = '' THEN 'EMPTY' ELSE 'OTHER' END FROM " + table + " WHERE id=" + id
  );
  return String(out).trim();
}

// reportUploadBarFinding records an ABSENT upload bar at `where`, gated by
// the module-level POST_DEPLOY switch (see header): pre-deploy this is an
// honest baseline observation (S3/info), post-deploy it is promoted to a
// real finding (S2/new). Silent when the bar IS present -- addFinding is for
// deviations, matching the rest of this harness's convention.
function reportUploadBarFinding(addFinding, scenarioId, where, present, evidenceShots) {
  if (present) return;
  addFinding({
    severity: POST_DEPLOY ? "S2" : "S3",
    status: POST_DEPLOY ? "new" : "info",
    title: scenarioId + ": no upload bar found at " + where + (POST_DEPLOY ? "" : " (pre-deploy baseline)"),
    repro: "storage, write mode on; expand to " + where + "; look for .uploadbar",
    expected: ".uploadbar present",
    actual: "absent",
    evidence: evidenceShots || [],
  });
}

// ============================================================================
// R2-1 — object rename end-to-end (prompt -> confirm chain), C2 regression
// check: unconditional (must pass both pre- and post-deploy per the brief).
// ============================================================================

const RENAME_OLD = "old.txt";
const RENAME_NEW = "new.txt";
const RENAME_CONTENT = "hello from uitest r2 rename\n";

async function runR2_1(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  await r2ObjTeardown();
  try {
    await s3("PUT", R2_OBJ_PREFIX + RENAME_OLD, { body: Buffer.from(RENAME_CONTENT), contentType: "text/plain" });

    const spa = await enterService(ctx, OBJECT_SERVICE, true);
    const found = await revealNode(spa, RENAME_OLD, 40);
    if (!found) {
      addFinding({
        severity: "S1",
        title: "R2-1: could not reveal seeded " + RENAME_OLD + " in the storage tree",
        repro: "PUT " + R2_OBJ_PREFIX + RENAME_OLD + "; openConsole(storage); reveal in tree",
        expected: "object reachable",
        actual: "revealNode gave up",
        evidence: [await evidence("00-reveal-failed")],
      });
      return;
    }
    // Guard against a stale prior blob's toolbar (kv-object.js's blobViewState
    // documents this exact race): wait for the toolbar title to actually say
    // "old.txt" before trusting #renameblob belongs to THIS node.
    await spa
      .waitForFunction(
        (n) => {
          const b = document.querySelector("#content .toolbar b");
          return !!b && b.textContent === n && !!document.getElementById("renameblob");
        },
        { timeout: 10000 },
        RENAME_OLD
      )
      .catch(() => {});
    await evidence("01-blob-open");

    const hasRenameBtn = await spa.evaluate(() => !!document.getElementById("renameblob"));
    if (!hasRenameBtn) {
      addFinding({
        severity: "S1",
        title: "R2-1: #renameblob not present on an object-storage blob with write mode on",
        repro: "write on; open " + RENAME_OLD,
        expected: "#renameblob present",
        actual: "absent",
        evidence: [await evidence("01b-no-rename-button")],
      });
      return;
    }

    await spa.click("#renameblob");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    const promptState = await readModalState(spa);
    const promptDefault = await spa.evaluate(() => (document.getElementById("modalinput") || {}).value || "");
    await evidence("02-rename-prompt-modal");
    if (promptState.hidden !== false || promptState.title !== "Rename " + RENAME_OLD) {
      addFinding({
        severity: "S1",
        title: "R2-1: rename prompt modal did not open with the expected title",
        repro: "storage: open " + RENAME_OLD + "; click #renameblob",
        expected: 'modal open, title="Rename ' + RENAME_OLD + '"',
        actual: JSON.stringify(promptState) + "; defaultValue=" + JSON.stringify(promptDefault),
        evidence: [await evidence("02b-prompt-mismatch")],
      });
    }

    // The field's real semantics (read from app.js's renameObject): `to` is
    // spliced in as the REPLACEMENT for only the LAST path segment
    // (n.path.segments.slice(0,-1).concat(to.split("/"))) -- the prefilled
    // default is the current BASENAME ("old.txt"), so typing a new basename
    // ("new.txt") renames in place. Typing the full "uitest_r2/new.txt" here
    // would double the folder prefix (uitest_r2/uitest_r2/new.txt) -- that is
    // NOT what a user replacing the prefilled basename would naturally type.
    await setInput(spa, "#modalinput", RENAME_NEW);
    await spa.click("#modalok"); // submits the prompt -> chains into the confirm modal

    let confirmState = null;
    try {
      await spa.waitForFunction(
        () => {
          const t = document.getElementById("modaltitle");
          return !!t && t.textContent.indexOf("Rename") === 0 && t.textContent.indexOf("→") >= 0;
        },
        { timeout: 8000 }
      );
      confirmState = await readModalState(spa);
    } catch (_) {
      confirmState = await readModalState(spa); // record whatever is actually there
    }
    await evidence("03-rename-confirm-modal");

    const expectedConfirmSubstr = "Rename " + RENAME_OLD + " → " + RENAME_NEW;
    if (!confirmState || !confirmState.title || confirmState.title.indexOf(expectedConfirmSubstr) < 0) {
      addFinding({
        severity: "S1",
        title: "R2-1: rename confirm modal did not appear with the expected 'Rename <old> → <new>?' text",
        repro: "storage: open " + RENAME_OLD + "; Rename; type '" + RENAME_NEW + "'; OK",
        expected: 'a SECOND modal opens with title containing "' + expectedConfirmSubstr + '"',
        actual: "promptDefault=" + JSON.stringify(promptDefault) + "; confirmState=" + JSON.stringify(confirmState),
        evidence: [await evidence("03b-confirm-mismatch")],
      });
    }

    await spa.click("#modalok"); // now actually confirms the rename
    const toast = await harness.waitToast(spa);
    await harness.drainToast(spa);
    await sleep(300);
    await evidence("04-after-rename-toast");

    if (!toast || toast.kind !== "good") {
      addFinding({
        severity: "S1",
        title: "R2-1: rename did not report success",
        repro: "confirm the rename " + RENAME_OLD + " → " + RENAME_NEW,
        expected: "good toast",
        actual: JSON.stringify(toast),
        evidence: [await evidence("04b-bad-toast")],
      });
    }

    const foundNew = await revealNode(spa, RENAME_NEW, 40);
    const stillHasOld = await spa.evaluate(
      (n) => Array.from(document.querySelectorAll("#tree .node .nname")).some((el) => el.textContent === n),
      RENAME_OLD
    );
    await evidence("05-tree-after-rename");
    if (!foundNew || stillHasOld) {
      addFinding({
        severity: "S1",
        title: "R2-1: tree does not reflect the rename (new name missing and/or old name still present)",
        repro: "after confirming the rename, re-check the storage tree",
        expected: RENAME_NEW + " visible, " + RENAME_OLD + " absent",
        actual: "foundNew=" + foundNew + " stillHasOld=" + stillHasOld,
        evidence: [await evidence("05b-tree-mismatch")],
      });
    }

    // ---- O-ENGINE verify -- S3 is the actual arbiter, not the toast ----
    const oldResp = await s3("GET", R2_OBJ_PREFIX + RENAME_OLD);
    const newResp = await s3("GET", R2_OBJ_PREFIX + RENAME_NEW);
    const newBody = newResp.status === 200 ? await newResp.text() : null;
    if (oldResp.status !== 404) {
      addFinding({
        id: "R2-1-old-key-survives",
        severity: "S1",
        title: "R2-1: the OLD object key still exists on S3 after a reported-successful rename",
        repro: "GET " + R2_OBJ_PREFIX + RENAME_OLD + " after renaming to " + RENAME_NEW,
        expected: "404",
        actual: "status=" + oldResp.status,
        evidence: [],
        engine_truth: "S3 GET " + RENAME_OLD + " = " + oldResp.status,
      });
    }
    if (newResp.status !== 200 || newBody !== RENAME_CONTENT) {
      addFinding({
        id: "R2-1-new-key-mismatch",
        severity: "S1",
        title: "R2-1: the NEW object key is missing or its bytes do not match the original",
        repro: "GET " + R2_OBJ_PREFIX + RENAME_NEW + " after renaming from " + RENAME_OLD,
        expected: "status=200; body exactly matches the original " + JSON.stringify(RENAME_CONTENT),
        actual: "status=" + newResp.status + "; body=" + JSON.stringify(newBody),
        evidence: [],
        engine_truth: "S3 GET " + RENAME_NEW + " status=" + newResp.status + " body=" + JSON.stringify(newBody),
      });
    }
  } finally {
    await r2ObjTeardown();
  }
}

// ============================================================================
// R2-2 — Set TTL + Persist chained-modal flow (valkey), same C2 class as
// R2-1: unconditional (KV-4 already proved this live against round-1).
// ============================================================================

const TTL_KEY = "uitest_r2_ttl";

async function runR2_2(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  r2KvTeardown(engines);
  try {
    engines.redis(["SET", TTL_KEY, "v"]);

    const spa = await enterService(ctx, CACHE_SERVICE, true);
    const found = await revealNode(spa, TTL_KEY, 20);
    if (!found) {
      addFinding({
        severity: "S1",
        title: "R2-2: could not reveal seeded " + TTL_KEY + " in the cache tree",
        repro: "redis SET " + TTL_KEY + "; openConsole(cache); reveal in tree",
        expected: "key reachable",
        actual: "revealNode gave up",
        evidence: [await evidence("00-reveal-failed")],
      });
      return;
    }
    await spa.waitForSelector(".ttlbar", { timeout: 15000 }).catch(() => {});
    const hasTTLBar = await spa.evaluate(() => !!document.querySelector(".ttlbar"));
    await evidence("01-ttlbar");
    if (!hasTTLBar) {
      addFinding({
        severity: "S1",
        title: "R2-2: .ttlbar not rendered for a KV string in write mode",
        repro: "open " + TTL_KEY + " with write mode on",
        expected: ".ttlbar present with Set TTL / Persist buttons",
        actual: "absent",
        evidence: [await evidence("01b-no-ttlbar")],
      });
      return;
    }

    // #setttl is a CHAINED two-modal flow (prompt -> confirm) — see
    // kv-object.js's KV-4 comment on the same gotcha: #modalok must be
    // clicked TWICE, and the second click must wait for the SECOND modal's
    // own content (title "Set TTL 90s...") before firing, or it can land on
    // the stale first modal and read a false TTL of -1.
    await spa.click("#setttl");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    await setInput(spa, "#modalinput", "90");
    await evidence("02-ttl-prompt-modal");
    await spa.click("#modalok");
    let confirmTitle = "";
    try {
      await spa.waitForFunction(
        () => {
          const t = document.getElementById("modaltitle");
          return !!t && t.textContent.indexOf("Set TTL 90s") === 0;
        },
        { timeout: 8000 }
      );
      confirmTitle = await spa.evaluate(() => document.getElementById("modaltitle").textContent);
    } catch (_) {
      confirmTitle = await spa.evaluate(() => (document.getElementById("modaltitle") || {}).textContent || "");
    }
    await evidence("03-ttl-confirm-modal");
    if (confirmTitle.indexOf("Set TTL 90s") !== 0) {
      addFinding({
        severity: "S2",
        title: "R2-2: Set TTL confirm modal did not show the expected 'Set TTL 90s...' title",
        repro: "Set TTL -> 90",
        expected: 'title starts with "Set TTL 90s"',
        actual: JSON.stringify(confirmTitle),
        evidence: [await evidence("03b-confirm-title-mismatch")],
      });
    }
    await spa.click("#modalok"); // now actually confirms EXPIRE 90s
    const toast1 = await harness.waitToast(spa);
    await harness.drainToast(spa);
    await sleep(200);
    const ttl1 = parseInt(engines.redis(["TTL", TTL_KEY]), 10);
    await evidence("04-after-set-ttl");
    if (!toast1 || toast1.kind !== "good" || !(ttl1 > 0 && ttl1 <= 90)) {
      addFinding({
        severity: "S1",
        title: "R2-2: Set TTL did not apply the expected range",
        repro: "open " + TTL_KEY + "; Set TTL -> 90",
        expected: "good toast; 0 < TTL <= 90",
        actual: "toast=" + JSON.stringify(toast1) + "; redis TTL=" + ttl1,
        evidence: [await evidence("04b-ttl-mismatch")],
        engine_truth: "redis TTL " + TTL_KEY + " = " + ttl1,
      });
    }

    await sleep(300);
    await spa.click("#clrttl");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    await evidence("05-persist-confirm-modal");
    await spa.click("#modalok");
    const toast2 = await harness.waitToast(spa);
    await harness.drainToast(spa);
    await sleep(200);
    const ttl2 = parseInt(engines.redis(["TTL", TTL_KEY]), 10);
    await evidence("06-after-persist");
    if (!toast2 || toast2.kind !== "good" || ttl2 !== -1) {
      addFinding({
        severity: "S1",
        title: "R2-2: Persist (clear TTL) did not clear the expiry",
        repro: "open " + TTL_KEY + "; Persist",
        expected: "good toast; redis TTL == -1 (no expiry)",
        actual: "toast=" + JSON.stringify(toast2) + "; redis TTL=" + ttl2,
        evidence: [await evidence("06b-persist-mismatch")],
        engine_truth: "redis TTL " + TTL_KEY + " = " + ttl2,
      });
    }
  } finally {
    r2KvTeardown(engines);
  }
}

// ============================================================================
// R2-3 — modal keeps typed input on a rejection (KV Add-key duplicate name).
// POST_DEPLOY-gated (B1 lifecycle): pre-deploy the modal is expected to
// close immediately (old hideModal()-before-await shape, per TAB-6's already
// -recorded finding); post-deploy it must stay open with #kvname intact and
// an inline error visible. The engine-unchanged assertion is unconditional.
// ============================================================================

const DUP_KEY = "uitest_r2_dup";
const DUP_ORIGINAL_VAL = "original-r2";

async function runR2_3(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  r2KvTeardown(engines);
  try {
    engines.redis(["SET", DUP_KEY, DUP_ORIGINAL_VAL]);

    const spa = await enterService(ctx, CACHE_SERVICE, true);
    const hasLink = await spa.evaluate(() => !!document.getElementById("createkeylink"));
    if (!hasLink) {
      addFinding({
        severity: "S1",
        title: "R2-3: #createkeylink not present after enabling write mode on cache",
        repro: "openConsole(cache); setWriteMode(true)",
        expected: "#createkeylink present",
        actual: "absent",
        evidence: [await evidence("00-no-createkeylink")],
      });
      return;
    }
    await spa.click("#createkeylink");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    await setInput(spa, "#kvname", DUP_KEY);
    await spa.select("#kvtype", "string");
    await setInput(spa, "#kvval", "clobber-attempt-r2");
    await evidence("01-addkey-filled-duplicate");

    await spa.click("#modalok");
    // Long enough for a synchronous pre-deploy hideModal() to land, short
    // enough to precede the async rejection toast that follows it.
    await sleep(400);
    const immediate = await readModalState(spa);
    const immediateKvname = await spa.evaluate(() => (document.getElementById("kvname") || {}).value || null);
    // Pre-deploy: this is where the async "bad" conflict toast shows up.
    // Post-deploy: no toast fires on this path at all (the rejection is
    // routed inline via showModalError instead) — waitToast just times out
    // harmlessly.
    const toast = await harness.waitToast(spa, 4000);
    await sleep(200);
    const settled = await readModalState(spa);
    const settledKvname = await spa.evaluate(() => (document.getElementById("kvname") || {}).value || null);
    await evidence("02-after-duplicate-submit");

    const stayedOpenWithInput = settled.hidden === false && settledKvname === DUP_KEY && settled.hasInlineErr;

    if (POST_DEPLOY) {
      if (!stayedOpenWithInput) {
        addFinding({
          severity: "S1",
          status: "new",
          title: "R2-3: KV Add-key modal did not stay open with the typed name + inline error on a duplicate-key rejection",
          repro: "cache: Add key '" + DUP_KEY + "' (already exists); confirm",
          expected: "#modal stays open; #kvname still '" + DUP_KEY + "'; an inline error visible in .modalbox",
          actual:
            "immediate=" + JSON.stringify(Object.assign({}, immediate, { kvname: immediateKvname })) +
            "; toast=" + JSON.stringify(toast) +
            "; settled=" + JSON.stringify(Object.assign({}, settled, { kvname: settledKvname })),
          evidence: [await evidence("02b-post-deploy-mismatch")],
        });
      }
    } else {
      addFinding({
        severity: "S3",
        status: "info",
        title: "R2-3 baseline (pre-deploy): observed KV Add-key duplicate-rejection behavior",
        repro: "cache: Add key '" + DUP_KEY + "' (already exists); confirm",
        expected: "n/a -- recording actual pre-deploy behavior for comparison after the round-2 deploy",
        actual:
          "immediate=" + JSON.stringify(Object.assign({}, immediate, { kvname: immediateKvname })) +
          "; toast=" + JSON.stringify(toast) +
          "; settled=" + JSON.stringify(Object.assign({}, settled, { kvname: settledKvname })),
        evidence: [],
      });
    }

    // Unconditional: the rejection must never have actually applied, in
    // either shape.
    const afterVal = engines.redis(["GET", DUP_KEY]);
    if (afterVal !== DUP_ORIGINAL_VAL) {
      addFinding({
        severity: "S1",
        title: "R2-3: duplicate-key rejection was reported but the engine value changed anyway",
        repro: "create-collide " + DUP_KEY,
        expected: 'value stays "' + DUP_ORIGINAL_VAL + '"',
        actual: "value now " + JSON.stringify(afterVal),
        evidence: [await evidence("03-engine-after")],
        engine_truth: "redis GET " + DUP_KEY + " = " + JSON.stringify(afterVal),
      });
    }

    // cleanup: close the modal if it's still open
    const stillOpen = await spa.evaluate(() => !document.getElementById("modal").classList.contains("hidden"));
    if (stillOpen) {
      const cancelUsable = await spa.evaluate(() => {
        const c = document.getElementById("modalcancel");
        return !!c && !c.disabled && !c.classList.contains("hidden");
      });
      if (cancelUsable) await spa.click("#modalcancel").catch(() => {});
    }
  } finally {
    r2KvTeardown(engines);
  }
}

// ============================================================================
// R2-4 — modal keyboard: Escape closes with no request fired; a backdrop
// click closes too. POST_DEPLOY-gated for the "actually closes" behavior;
// the "no request fired" safety invariant is unconditional.
// ============================================================================

const MODAL_TABLE = "uitest_r2_modal";

async function runR2_4(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  r2DbTeardown(engines, MODAL_TABLE);
  let spa = null;
  try {
    engines.psql("CREATE TABLE " + MODAL_TABLE + " (id serial PRIMARY KEY, txt text)");

    spa = await enterService(ctx, DB_SERVICE, true);
    const opened = await revealNode(spa, MODAL_TABLE, 30);
    if (!opened) {
      addFinding({
        severity: "S1",
        title: "R2-4: could not reveal " + MODAL_TABLE + " in the db tree",
        repro: "openConsole(db); write on; reveal " + MODAL_TABLE,
        expected: "table reachable",
        actual: "revealNode gave up",
        evidence: [await evidence("00-reveal-failed")],
      });
      return;
    }
    await spa.waitForSelector("table.grid", { timeout: 15000 });
    await sleep(200);

    // ---- Escape closes, no request fired ----
    await spa.waitForSelector("#insertrow", { timeout: 10000 });
    await spa.click("#insertrow");
    await spa.waitForSelector(".insertform", { timeout: 10000 });
    // Type a marker so a false "no request fired" pass can't hide an actual
    // accidental submit -- if Escape somehow triggered a submit-like path,
    // this row would show up distinctly.
    await spa.type('.insertform input[data-col="txt"]', "uitest_r2_should_not_be_inserted");
    await evidence("01-insert-modal-open-filled");
    const beforeEscCount = parseInt(engines.psql("SELECT count(*) FROM " + MODAL_TABLE), 10);
    await page.keyboard.press("Escape");
    await sleep(400);
    const afterEscHidden = await spa.evaluate(() => document.getElementById("modal").classList.contains("hidden"));
    await evidence("02-after-escape");
    const afterEscCount = parseInt(engines.psql("SELECT count(*) FROM " + MODAL_TABLE), 10);

    if (afterEscCount !== beforeEscCount) {
      addFinding({
        severity: "S1",
        title: "R2-4: pressing Escape on the Insert-row modal fired a request",
        repro: "open Insert-row; type a marker into txt; press Escape",
        expected: "row count unchanged (" + beforeEscCount + ")",
        actual: "row count now " + afterEscCount,
        evidence: [await evidence("02b-escape-inserted-row")],
        engine_truth: "COUNT(*) = " + afterEscCount,
      });
    }
    if (POST_DEPLOY) {
      if (!afterEscHidden) {
        addFinding({
          severity: "S1",
          status: "new",
          title: "R2-4: Escape did not close the Insert-row modal",
          repro: "open Insert-row; press Escape",
          expected: "#modal hidden",
          actual: "modalHidden=" + afterEscHidden,
          evidence: [await evidence("02c-escape-did-not-close")],
        });
      }
    } else {
      addFinding({
        severity: "S3",
        status: "info",
        title: "R2-4 baseline (pre-deploy): observed Escape-close state on the Insert-row modal",
        actual: "modalHidden=" + afterEscHidden,
        evidence: [],
      });
    }

    // ---- backdrop click closes ----
    const stillOpenBefore = await spa.evaluate(() => !document.getElementById("modal").classList.contains("hidden"));
    if (stillOpenBefore) {
      await spa.click("#modalcancel").catch(() => {}); // force-close so the next step starts clean
      await sleep(200);
    }
    await spa.waitForSelector("#insertrow", { timeout: 10000 });
    await spa.click("#insertrow");
    await spa.waitForSelector(".insertform", { timeout: 10000 });
    await spa.type('.insertform input[data-col="txt"]', "uitest_r2_should_not_be_inserted_2");
    await evidence("03-reopened-for-backdrop-test");
    const beforeBackdropCount = parseInt(engines.psql("SELECT count(*) FROM " + MODAL_TABLE), 10);
    // Dispatch a real "click" DOM event directly on #modal (the backdrop
    // element itself), never on a .modalbox descendant. app.js's listener
    // only cancels when e.target === e.currentTarget (the backdrop, not a
    // child) -- dispatching straight on #modal deterministically hits that
    // exact branch without needing cross-nested-iframe pixel geometry to
    // find a point outside the centered .modalbox (fragile: the box's exact
    // width vs. this particular SPA iframe's own width, several layers deep
    // in the webview chain, isn't known from here).
    await spa.evaluate(() => {
      document.getElementById("modal").dispatchEvent(new MouseEvent("click", { bubbles: true, cancelable: true }));
    });
    await sleep(400);
    const afterBackdropHidden = await spa.evaluate(() => document.getElementById("modal").classList.contains("hidden"));
    await evidence("04-after-backdrop-click");
    const afterBackdropCount = parseInt(engines.psql("SELECT count(*) FROM " + MODAL_TABLE), 10);

    if (afterBackdropCount !== beforeBackdropCount) {
      addFinding({
        severity: "S1",
        title: "R2-4: a backdrop click on the Insert-row modal fired a request",
        repro: "open Insert-row; type a marker into txt; dispatch a click on #modal itself",
        expected: "row count unchanged (" + beforeBackdropCount + ")",
        actual: "row count now " + afterBackdropCount,
        evidence: [await evidence("04b-backdrop-inserted-row")],
        engine_truth: "COUNT(*) = " + afterBackdropCount,
      });
    }
    if (POST_DEPLOY) {
      if (!afterBackdropHidden) {
        addFinding({
          severity: "S1",
          status: "new",
          title: "R2-4: a backdrop click did not close the Insert-row modal",
          repro: "open Insert-row; dispatch a click on #modal itself (e.target === e.currentTarget)",
          expected: "#modal hidden",
          actual: "modalHidden=" + afterBackdropHidden,
          evidence: [await evidence("04c-backdrop-did-not-close")],
        });
      }
    } else {
      addFinding({
        severity: "S3",
        status: "info",
        title: "R2-4 baseline (pre-deploy): observed backdrop-click-close state on the Insert-row modal",
        actual: "modalHidden=" + afterBackdropHidden,
        evidence: [],
      });
    }
  } finally {
    // Pre-deploy this scenario's own point is that Escape/backdrop can
    // legitimately leave #modal open (that IS the observed baseline) -- force
    // it closed via the always-safe #modalcancel/#modalok path (see
    // closeStrayModal's doc comment) so this scenario never bleeds a stray
    // full-viewport overlay into whichever scenario runs next.
    if (spa) await closeStrayModal(spa).catch(() => {});
    r2DbTeardown(engines, MODAL_TABLE);
  }
}

// ============================================================================
// R2-5 — full-value read-only view for a non-editable (write-mode-off) cell,
// plus the static-title-hint invariant. POST_DEPLOY-gated for the modal's
// own rendering; the title-never-leaks-the-raw-value check is unconditional
// (a leak would be bad in any version).
// ============================================================================

const VIEW_TABLE = "uitest_r2_view";
const LONG_VALUE = "R2LONG-" + "abcdefghij".repeat(12); // 127 chars, well over 100

async function runR2_5(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  r2DbTeardown(engines, VIEW_TABLE);
  try {
    engines.psql("CREATE TABLE " + VIEW_TABLE + " (id serial PRIMARY KEY, val text)");
    engines.psql("INSERT INTO " + VIEW_TABLE + " (val) VALUES ('" + LONG_VALUE + "')");

    const spa = await enterService(ctx, DB_SERVICE, false); // write mode OFF
    const opened = await revealNode(spa, VIEW_TABLE, 30);
    if (!opened) {
      addFinding({
        severity: "S1",
        title: "R2-5: could not reveal " + VIEW_TABLE + " in the db tree",
        repro: "openConsole(db); write off; reveal " + VIEW_TABLE,
        expected: "table reachable",
        actual: "revealNode gave up",
        evidence: [await evidence("00-reveal-failed")],
      });
      return;
    }
    await spa.waitForSelector("table.grid", { timeout: 15000 });
    await sleep(200);

    const heads = await spa.evaluate(() => Array.from(document.querySelectorAll("table.grid thead th")).map((th) => th.textContent));
    const valIdx = heads.findIndex((h) => h.indexOf("val") === 0);
    const cellSel = "table.grid tbody.gridbody tr:nth-child(1) td:nth-child(" + (valIdx + 1) + ")";
    await spa.waitForSelector(cellSel, { timeout: 10000 });

    const cellInfo = await spa.evaluate(
      (s) => {
        const td = document.querySelector(s);
        return td ? { text: td.textContent, title: td.getAttribute("title"), cls: td.className } : null;
      },
      cellSel
    );
    await evidence("01-grid-with-long-value");

    // Unconditional: the title attribute must never carry the raw long
    // value -- a static hint only, regardless of which code is live.
    if (cellInfo && cellInfo.title && cellInfo.title.indexOf(LONG_VALUE) >= 0) {
      addFinding({
        severity: "S1",
        title: "R2-5: the cell's title attribute leaks the raw full value instead of a static hint",
        repro: "write mode off; open " + VIEW_TABLE + "; read td title attribute",
        expected: 'title is a static hint (e.g. "Click to view full value"), never the raw cell value',
        actual: JSON.stringify(cellInfo),
        evidence: [await evidence("01b-title-leak")],
      });
    }

    await spa.click(cellSel);
    await sleep(400);
    const modalState = await spa.evaluate(() => {
      const modal = document.getElementById("modal");
      const pre = document.querySelector("#modalbody pre.blob");
      const cancel = document.getElementById("modalcancel");
      const ok = document.getElementById("modalok");
      return {
        hidden: modal ? modal.classList.contains("hidden") : null,
        preText: pre ? pre.textContent : null,
        cancelHidden: cancel ? cancel.classList.contains("hidden") : null,
        okText: ok ? ok.textContent : null,
        okDanger: ok ? ok.classList.contains("danger") : null,
      };
    });
    await evidence("02-full-view-modal");

    const renderedFully =
      modalState.hidden === false && modalState.preText === LONG_VALUE && modalState.cancelHidden === true &&
      modalState.okText === "OK" && !modalState.okDanger;

    if (POST_DEPLOY) {
      if (!renderedFully) {
        addFinding({
          severity: "S1",
          status: "new",
          title: "R2-5: clicking a truncated read-only cell did not open the expected read-only full-value view",
          repro: "write mode off; open " + VIEW_TABLE + "; click the val cell",
          expected: "modal opens with pre.blob text == the full value, single OK button, no Cancel, no danger style",
          actual: JSON.stringify(modalState),
          evidence: [await evidence("02b-full-view-mismatch")],
        });
      }
    } else {
      addFinding({
        severity: "S3",
        status: "info",
        title: "R2-5 baseline (pre-deploy): observed click-to-view-full-value behavior",
        actual: JSON.stringify(modalState),
        evidence: [],
      });
    }

    if (modalState.hidden === false) {
      await spa.click("#modalok");
      await sleep(200);
      const closedAfterOK = await spa.evaluate(() => document.getElementById("modal").classList.contains("hidden"));
      await evidence("03-after-ok");
      if (!closedAfterOK) {
        addFinding({
          severity: "S2",
          title: "R2-5: OK did not close the full-value view modal",
          repro: "open the full-value view; click OK",
          expected: "#modal hidden",
          actual: "closedAfterOK=" + closedAfterOK,
          evidence: [await evidence("03b-ok-did-not-close")],
        });
      }
    }
  } finally {
    r2DbTeardown(engines, VIEW_TABLE);
  }
}

// ============================================================================
// R2-6 — SQL NULL affordance on a tabular cell edit, and NULL vs '' being
// distinct values. POST_DEPLOY-gated for the ∅ NULL button's presence; if
// present at all (either side of the deploy), its correctness is asserted
// unconditionally.
// ============================================================================

const NULL_TABLE = "uitest_r2_null";

async function runR2_6(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  r2DbTeardown(engines, NULL_TABLE);
  try {
    engines.psql("CREATE TABLE " + NULL_TABLE + " (id serial PRIMARY KEY, val text)");
    engines.psql("INSERT INTO " + NULL_TABLE + " (val) VALUES ('before')");

    const spa = await enterService(ctx, DB_SERVICE, true);
    const opened = await revealNode(spa, NULL_TABLE, 30);
    if (!opened) {
      addFinding({
        severity: "S1",
        title: "R2-6: could not reveal " + NULL_TABLE + " in the db tree",
        repro: "openConsole(db); write on; reveal " + NULL_TABLE,
        expected: "table reachable",
        actual: "revealNode gave up",
        evidence: [await evidence("00-reveal-failed")],
      });
      return;
    }
    await spa.waitForSelector("table.grid", { timeout: 15000 });
    await sleep(200);

    const heads = await spa.evaluate(() => Array.from(document.querySelectorAll("table.grid thead th")).map((th) => th.textContent));
    const valIdx = heads.findIndex((h) => h.indexOf("val") === 0);
    const cellSel = "table.grid tbody.gridbody tr:nth-child(1) td:nth-child(" + (valIdx + 1) + ")";

    await spa.waitForSelector(cellSel, { timeout: 10000 });
    await spa.click(cellSel);
    await spa.waitForSelector(cellSel + " input.celledit", { timeout: 10000 });
    await evidence("01-cell-editing");

    const nullBtnPresent = await spa.evaluate((s) => !!document.querySelector(s + " button.cellnull"), cellSel);
    if (POST_DEPLOY) {
      if (!nullBtnPresent) {
        addFinding({
          severity: "S1",
          status: "new",
          title: "R2-6: no ∅ NULL button on an editing tabular cell",
          repro: "write on; open " + NULL_TABLE + "; click the val cell",
          expected: "button.cellnull present alongside input.celledit",
          actual: "absent",
          evidence: [await evidence("01b-no-null-button")],
        });
      }
    } else {
      addFinding({
        severity: "S3",
        status: "info",
        title: "R2-6 baseline (pre-deploy): observed ∅ NULL button presence = " + nullBtnPresent,
        actual: "nullBtnPresent=" + nullBtnPresent,
        evidence: [],
      });
    }

    if (nullBtnPresent) {
      await spa.click(cellSel + " button.cellnull");
      const toast1 = await harness.waitToast(spa);
      await harness.drainToast(spa);
      await sleep(200);
      const state1 = r2NullState(engines, NULL_TABLE, 1);
      await evidence("02-after-null-click");
      if (!toast1 || toast1.kind !== "good" || state1 !== "NULL") {
        addFinding({
          severity: "S1",
          title: "R2-6: clicking the ∅ NULL button did not set the value to SQL NULL",
          repro: "write on; open " + NULL_TABLE + "; edit val cell; click ∅ NULL",
          expected: "good toast; val IS NULL",
          actual: "toast=" + JSON.stringify(toast1) + "; state=" + state1,
          evidence: [await evidence("02b-null-mismatch")],
          engine_truth: "NULL/EMPTY/OTHER discriminator = " + state1,
        });
      }

      // ---- second edit: commit an empty string, must be DISTINCT from NULL ----
      await spa.click(cellSel);
      await spa.waitForSelector(cellSel + " input.celledit", { timeout: 10000 });
      // The input already reads "" here (oldVal was just set to null, and
      // editCell renders a null oldVal as an empty string) -- press Enter
      // directly to commit that empty string without typing anything.
      await page.keyboard.press("Enter");
      const toast2 = await harness.waitToast(spa);
      await harness.drainToast(spa);
      await sleep(200);
      const state2 = r2NullState(engines, NULL_TABLE, 1);
      await evidence("03-after-empty-string-commit");
      if (!toast2 || toast2.kind !== "good" || state2 !== "EMPTY") {
        addFinding({
          severity: "S1",
          title: "R2-6: committing an empty string after a NULL value did not distinctly set an empty string",
          repro: "after setting val to NULL via ∅, edit the cell again, leave it empty, press Enter",
          expected: "good toast; val = '' (distinct from NULL)",
          actual: "toast=" + JSON.stringify(toast2) + "; state=" + state2,
          evidence: [await evidence("03b-empty-mismatch")],
          engine_truth: "NULL/EMPTY/OTHER discriminator = " + state2,
        });
      }
    }
  } finally {
    r2DbTeardown(engines, NULL_TABLE);
  }
}

// ============================================================================
// R2-7 — delete-confirm identity: the row's key must appear in the modal
// title, not a bare "DELETE row". POST_DEPLOY-gated for the identity text;
// the delete itself actually applying is unconditional (TAB-7 already
// proved the base flow works — this only adds the title-content check).
// ============================================================================

const DEL_TABLE = "uitest_r2_del";

async function runR2_7(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  r2DbTeardown(engines, DEL_TABLE);
  try {
    engines.psql("CREATE TABLE " + DEL_TABLE + " (id serial PRIMARY KEY, txt text)");
    engines.psql("INSERT INTO " + DEL_TABLE + " (txt) VALUES ('deleteme')");
    const rowId = String(engines.psql("SELECT id FROM " + DEL_TABLE + " WHERE txt='deleteme'")).trim();

    const spa = await enterService(ctx, DB_SERVICE, true);
    const opened = await revealNode(spa, DEL_TABLE, 30);
    if (!opened) {
      addFinding({
        severity: "S1",
        title: "R2-7: could not reveal " + DEL_TABLE + " in the db tree",
        repro: "openConsole(db); write on; reveal " + DEL_TABLE,
        expected: "table reachable",
        actual: "revealNode gave up",
        evidence: [await evidence("00-reveal-failed")],
      });
      return;
    }
    await spa.waitForSelector("table.grid", { timeout: 15000 });
    await sleep(200);

    await spa.waitForSelector("button.rowdel", { timeout: 10000 });
    await spa.click("button.rowdel");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    const title = await spa.evaluate(() => (document.getElementById("modaltitle") || {}).textContent || "");
    await evidence("01-delete-confirm-modal");

    const hasIdentity = title.indexOf("id=" + rowId) >= 0;
    if (POST_DEPLOY) {
      if (!hasIdentity) {
        addFinding({
          severity: "S2",
          status: "new",
          title: "R2-7: delete-confirm modal title does not name the row's key",
          repro: "write on; open " + DEL_TABLE + "; click the row's delete button",
          expected: 'title contains "id=' + rowId + '"',
          actual: JSON.stringify(title),
          evidence: [await evidence("01b-no-identity")],
        });
      }
    } else {
      addFinding({
        severity: "S3",
        status: "info",
        title: "R2-7 baseline (pre-deploy): observed delete-confirm modal title",
        actual: JSON.stringify(title),
        evidence: [],
      });
    }

    await spa.click("#modalok");
    const toast = await harness.waitToast(spa);
    await sleep(300);
    const afterCount = parseInt(engines.psql("SELECT count(*) FROM " + DEL_TABLE), 10);
    await evidence("02-after-delete");
    if (!toast || toast.kind !== "good" || afterCount !== 0) {
      addFinding({
        severity: "S1",
        title: "R2-7: row delete did not apply",
        repro: "click button.rowdel for the seeded row; confirm #modalok",
        expected: "good toast; row count 0",
        actual: "toast=" + JSON.stringify(toast) + "; row count=" + afterCount,
        evidence: [await evidence("02b-delete-mismatch")],
        engine_truth: "COUNT(*) = " + afterCount,
      });
    }
  } finally {
    r2DbTeardown(engines, DEL_TABLE);
  }
}

// ============================================================================
// R2-8 — folder upload bar: an .uploadbar must render inside a NESTED
// folder's own children (not just at the bucket root). Presence-gated per
// folder via reportUploadBarFinding (see header for the POST_DEPLOY shape).
// ============================================================================

const R2_SUB_FILE = "sub/x.txt";

async function runR2_8(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  await r2ObjTeardown();
  try {
    await s3("PUT", R2_OBJ_PREFIX + R2_SUB_FILE, { body: Buffer.from("x\n"), contentType: "text/plain" });

    const spa = await enterService(ctx, OBJECT_SERVICE, true);
    await sleep(300);
    const rootBarPresent = await spa.evaluate(() => !!document.querySelector("#tree > .uploadbar"));
    await evidence("01-bucket-root");
    // Root has multiple entries in the shared bucket (other agents' own
    // prefixes coexist alongside uitest_r2/) — no lone-container auto-expand
    // in play here, so the root-level bar is the direct, unambiguous check.
    reportUploadBarFinding(addFinding, "R2-8", "bucket root", rootBarPresent, [await evidence("01b-root-state")]);

    const foundFolder = await revealNode(spa, "uitest_r2", 40);
    if (!foundFolder) {
      addFinding({
        severity: "S1",
        title: "R2-8: could not reveal the uitest_r2 folder in the storage tree",
        repro: "PUT " + R2_OBJ_PREFIX + R2_SUB_FILE + "; openConsole(storage); reveal uitest_r2",
        expected: "folder reachable",
        actual: "revealNode gave up",
        evidence: [await evidence("02-folder-reveal-failed")],
      });
      return;
    }
    await sleep(300);
    await evidence("02-uitest_r2-expanded");
    const folderBarPresent = await spa.evaluate(() => {
      const wraps = Array.from(document.querySelectorAll("#tree .node-wrap"));
      const parent = wraps.find((w) => {
        const nm = w.querySelector(":scope > .node .nname");
        return nm && nm.textContent === "uitest_r2";
      });
      const kids = parent ? parent.querySelector(":scope > .children") : null;
      return !!(kids && kids.querySelector(":scope > .uploadbar"));
    });
    reportUploadBarFinding(addFinding, "R2-8", "the uitest_r2 folder's own children", folderBarPresent, [
      await evidence("02b-uitest_r2-bar-state"),
    ]);

    const foundSub = await revealNode(spa, "sub", 40);
    if (!foundSub) {
      addFinding({
        severity: "S1",
        title: "R2-8: could not reveal the sub folder inside uitest_r2",
        repro: "expand uitest_r2; reveal sub",
        expected: "folder reachable",
        actual: "revealNode gave up",
        evidence: [await evidence("03-sub-reveal-failed")],
      });
      return;
    }
    await sleep(300);
    await evidence("03-sub-expanded");
    const subBarPresent = await spa.evaluate(() => {
      const wraps = Array.from(document.querySelectorAll("#tree .node-wrap"));
      const parent = wraps.find((w) => {
        const nm = w.querySelector(":scope > .node .nname");
        return nm && nm.textContent === "sub";
      });
      const kids = parent ? parent.querySelector(":scope > .children") : null;
      return !!(kids && kids.querySelector(":scope > .uploadbar"));
    });
    reportUploadBarFinding(addFinding, "R2-8", "the sub folder directly containing the seeded file", subBarPresent, [
      await evidence("03b-sub-bar-state"),
    ]);
  } finally {
    await r2ObjTeardown();
  }
}

// ============================================================================
// R2-9 — rejection detail honesty: a SQL syntax error must show the specific
// engine reason, not the bare generic "Service returned an upstream error."
// (pairs with provider/tabular/errors.go's engineErr reclassification and
// the already-open finding UX-A10-01-upstream-mislabel against round-1).
// POST_DEPLOY-gated.
// ============================================================================

async function runR2_9(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  const spa = await enterService(ctx, DB_SERVICE, false);
  const hint = await spa.evaluate(() => !!document.getElementById("querylink"));
  if (!hint) {
    addFinding({
      severity: "S2",
      title: "R2-9: no #querylink offered for postgres -- cannot exercise the query-error check",
      repro: "openConsole(db)",
      expected: "#querylink present",
      actual: "absent",
      evidence: [await evidence("00-no-querylink")],
    });
    return;
  }
  await spa.click("#querylink");
  await spa.waitForSelector("#qtext", { timeout: 10000 });
  await spa.type("#qtext", "SELEKT 1");
  await spa.click("#runq");
  await sleep(800);
  const errState = await spa.evaluate(() => ({
    hasErr: !!document.querySelector("#qresult .err"),
    crashed: !document.getElementById("qresult"),
    msg: (document.querySelector("#qresult .err-msg") || {}).textContent || null,
  }));
  await evidence("01-syntax-error-query");

  const GENERIC = "Service returned an upstream error.";
  const isGeneric = errState.msg === GENERIC;
  const hasSpecificReason = !!errState.msg && /syntax error|SELEKT/i.test(errState.msg);

  if (POST_DEPLOY) {
    if (errState.crashed || !errState.hasErr || isGeneric || !hasSpecificReason) {
      addFinding({
        severity: "S2",
        status: "new",
        title: "R2-9: SQL syntax-error rejection still shows the generic upstream-error message, not the specific engine reason",
        repro: "db query pane: run 'SELEKT 1'",
        expected:
          'error message contains the specific engine reason (e.g. syntax error at or near "SELEKT"), never the bare "' + GENERIC + '"',
        actual: JSON.stringify(errState),
        evidence: [await evidence("01b-still-generic")],
      });
    }
  } else {
    addFinding({
      severity: "S3",
      status: "info",
      title: "R2-9 baseline (pre-deploy): observed SQL syntax-error message",
      repro: "db query pane: run 'SELEKT 1'",
      expected: "n/a -- recording actual pre-deploy behavior; matches the already-open UX-A10-01-upstream-mislabel finding",
      actual: JSON.stringify(errState),
      evidence: [],
    });
  }
}

// ============================================================================
// R2-10 — view-only vocabulary + search gating for qdrant (vectors): the
// "Search ▸" link must not render (server 422s ErrUnsupported for
// qdrant); the rail badge wording is recorded, never pass/failed, since a
// polish-slice rename is a legitimate design choice either way.
// ============================================================================

async function runR2_10(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  let spa = await enterService(ctx, VECTORS_SERVICE, false);
  spa = await harness.spaFrame(page);
  await harness.clickService(spa, VECTORS_SERVICE); // land on the hint, per document.js's DOC-6 pattern
  await sleep(300);
  const searchLinkPresent = await spa.evaluate(() => !!document.getElementById("searchlink"));
  const badgeText = await spa.evaluate(() => {
    const items = Array.from(document.querySelectorAll("#services li"));
    const li = items.find((el) => {
      const span = el.querySelector("span");
      return span && span.textContent === "vectors";
    });
    const badge = li ? li.querySelector(".badge") : null;
    return badge ? badge.textContent : null;
  });
  await evidence("01-vectors-hint-and-rail");

  // Informational only, both sides of the deploy: the badge label is a
  // wording choice (the round-2 polish slice MAY relabel "view" to
  // "view-only"), not a correctness invariant this scenario adjudicates.
  addFinding({
    severity: "S3",
    status: "info",
    title: "R2-10: observed rail badge text for the view-only 'vectors' (qdrant) service",
    repro: "sidebar rail; read #services li's .badge textContent for hostname 'vectors'",
    expected: "n/a -- recording actual wording for comparison after the round-2 polish slice",
    actual: "badge=" + JSON.stringify(badgeText),
    evidence: [],
  });

  if (searchLinkPresent) {
    addFinding({
      severity: POST_DEPLOY ? "S1" : "S3",
      status: POST_DEPLOY ? "new" : "info",
      title:
        'R2-10: "Search ▸" link is offered for qdrant though the engine has no searcher' +
        (POST_DEPLOY ? "" : " (pre-deploy baseline -- a known pre-existing gap; see document.js's own qdrant search-link finding)"),
      repro: "vectors: selectService -> hint",
      expected: "no #searchlink for a qdrant-backed service (server 422s ErrUnsupported if used)",
      actual: "#searchlink present",
      evidence: [await evidence("02-search-link-present")],
    });
  }
}

runner.register({ id: "R2-1", family: "round2", fn: runR2_1 });
runner.register({ id: "R2-2", family: "round2", fn: runR2_2 });
runner.register({ id: "R2-3", family: "round2", fn: runR2_3 });
runner.register({ id: "R2-4", family: "round2", fn: runR2_4 });
runner.register({ id: "R2-5", family: "round2", fn: runR2_5 });
runner.register({ id: "R2-6", family: "round2", fn: runR2_6 });
runner.register({ id: "R2-7", family: "round2", fn: runR2_7 });
runner.register({ id: "R2-8", family: "round2", fn: runR2_8 });
runner.register({ id: "R2-9", family: "round2", fn: runR2_9 });
runner.register({ id: "R2-10", family: "round2", fn: runR2_10 });

module.exports = {};
