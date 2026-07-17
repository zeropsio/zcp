"use strict";

// TAB-1..TAB-10 — tabular family: postgres (db, full read/write), mariadb
// (full read/write), clickhouse (ch, view-only per spec-dataconsole.md §6/§7.5).
//
// Every scenario is self-contained (seeds its own uitest_-prefixed fixtures at
// the top, drops them in a finally) so `node run.js --scenario TAB-N` works in
// isolation, matching the single-invocation-per-scenario running mode. Engine
// oracles (psql / mariadb CLI / clickhouse HTTP) are composed in THIS file per
// the fan-out brief — lib/engines.js only ships the postgres+redis oracles A1
// needed for CORE-1; shellQuote()/container() are reused from there so every
// embedded value (password, SQL text) still goes through the same POSIX
// single-quote discipline (CLAUDE.md "Shell/SQL composition").
//
// G1 verdict rule (per harness README): a legitimately failing assertion is
// recorded via addFinding and does NOT fail the scenario's run status — only a
// thrown error (the harness couldn't drive the UI at all) does that.

const runner = require("../lib/runner");
const { loadConfig } = require("../lib/config");

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// trackEvidence wraps ctx.evidence (which just returns a path per call, per
// run.js: `evidence: (name) => harness.shot(page, scenario.id, name)`) so
// every scenario below can write `evidence.__lastPath` right after taking a
// shot instead of threading a local variable through every addFinding call.
function trackEvidence(rawEvidence) {
  const fn = async (name) => {
    const p = await rawEvidence(name);
    fn.__lastPath = p;
    return p;
  };
  fn.__lastPath = "";
  return fn;
}

// ================= engine descriptors =================
const PG = { id: "pg", service: "db", label: "postgres" };
const MARIA = { id: "maria", service: "mariadb", label: "mariadb" };
const ENGINES = [PG, MARIA];

const LONG_TEXT = "x".repeat(500);
const UNICODE_TEXT = "Příliš žluťoučký 💾";

// ================= engine command builders =================
// Mirrors lib/engines.js's psql()/redis() shape (shellQuote every embedded
// value, run over engines.container()) but for mariadb (CLI) and clickhouse
// (HTTP interface) which lib/engines.js does not ship.
function pgSQL(engines, sql) {
  const cfg = loadConfig();
  const cmd =
    "PGPASSWORD=" + engines.shellQuote(cfg.DC_PG_PASSWORD) +
    " psql -h " + engines.shellQuote(cfg.DC_PG_HOST) +
    " -U " + engines.shellQuote(cfg.DC_PG_USER) +
    " -d " + engines.shellQuote(cfg.DC_PG_DB) +
    " -v ON_ERROR_STOP=1 -tAc " + engines.shellQuote(sql);
  return engines.container(cmd);
}
function mariaSQL(engines, sql) {
  const cfg = loadConfig();
  const cmd =
    // --default-character-set=utf8mb4 is load-bearing: the mariadb CLI's
    // connection charset otherwise defaults to latin1 (confirmed live via
    // SELECT @@character_set_client/_connection/_results), which silently
    // mangles any multi-byte UTF-8 text sent through -e on the way INTO the
    // table -- a harness fixture-seeding bug, not a product bug, but one that
    // would masquerade as a mariadb-specific unicode-rendering finding if left
    // unfixed (the console would just be faithfully rendering already-corrupt
    // stored bytes).
    "mariadb --default-character-set=utf8mb4 -h " + engines.shellQuote(cfg.DC_MARIADB_HOST) +
    " -u " + engines.shellQuote(cfg.DC_MARIADB_USER) +
    " -p" + engines.shellQuote(cfg.DC_MARIADB_PASSWORD) +
    " " + engines.shellQuote(cfg.DC_MARIADB_DB) +
    " -N -B -e " + engines.shellQuote(sql);
  return engines.container(cmd);
}
function chSQL(engines, sql) {
  const cfg = loadConfig();
  const url =
    "http://" + cfg.DC_CH_HOST + ":" + cfg.DC_CH_HTTP_PORT +
    "/?user=" + encodeURIComponent(cfg.DC_CH_USER) +
    "&password=" + encodeURIComponent(cfg.DC_CH_PASSWORD) +
    "&database=" + encodeURIComponent(cfg.DC_CH_DB);
  const cmd = "curl -sf " + engines.shellQuote(url) + " --data-binary " + engines.shellQuote(sql);
  return engines.container(cmd);
}
function runSQL(engines, engine, sql) {
  if (engine.id === "pg") return pgSQL(engines, sql);
  if (engine.id === "maria") return mariaSQL(engines, sql);
  throw new Error("runSQL: unknown engine " + engine.id);
}
function engineCount(engines, engine, table) {
  return parseInt(String(runSQL(engines, engine, "SELECT count(*) FROM " + table)).trim(), 10);
}

// ================= fixtures: uitest_tab (4 rows, all column kinds) =================
function tabDDL_pg() {
  return [
    "DROP TABLE IF EXISTS uitest_tab",
    "CREATE TABLE uitest_tab (id serial PRIMARY KEY, txt text NOT NULL, num numeric(10,2), " +
      "flag boolean, ts timestamp, js jsonb, maybe_null text)",
    "INSERT INTO uitest_tab (txt, num, flag, ts, js, maybe_null) VALUES " +
      "('plain', 12.34, true, '2026-01-01 10:00:00', '{\"a\":1}', 'present')," +
      "('" + UNICODE_TEXT + "', 1.50, false, '2026-01-02 11:00:00', '{\"note\":\"unicode\"}', 'present')," +
      "('" + LONG_TEXT + "', 100.00, true, '2026-01-03 12:00:00', '{\"long\":true}', 'present')," +
      "('row4', 0, false, '2026-01-04 13:00:00', 'null', NULL)",
  ];
}
function tabDDL_maria() {
  return [
    "DROP TABLE IF EXISTS uitest_tab",
    "CREATE TABLE uitest_tab (id INT AUTO_INCREMENT PRIMARY KEY, txt TEXT NOT NULL, num DECIMAL(10,2), " +
      "flag TINYINT(1), ts TIMESTAMP NULL DEFAULT NULL, js JSON, maybe_null TEXT) " +
      "CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
    "INSERT INTO uitest_tab (txt, num, flag, ts, js, maybe_null) VALUES " +
      "('plain', 12.34, 1, '2026-01-01 10:00:00', '{\"a\":1}', 'present')," +
      "('" + UNICODE_TEXT + "', 1.50, 0, '2026-01-02 11:00:00', '{\"note\":\"unicode\"}', 'present')," +
      "('" + LONG_TEXT + "', 100.00, 1, '2026-01-03 12:00:00', '{\"long\":true}', 'present')," +
      "('row4', 0, 0, '2026-01-04 13:00:00', 'null', NULL)",
  ];
}
function seedTab(engines, engine) {
  const stmts = engine.id === "pg" ? tabDDL_pg() : tabDDL_maria();
  for (const s of stmts) runSQL(engines, engine, s);
}
function dropTab(engines, engine) {
  try { runSQL(engines, engine, "DROP TABLE IF EXISTS uitest_tab"); } catch (_) { /* best effort */ }
}

// ================= fixtures: uitest_wide (60 rows, paging) =================
function wideDDL_pg() {
  return [
    "DROP TABLE IF EXISTS uitest_wide",
    "CREATE TABLE uitest_wide (id serial PRIMARY KEY, val text, note text, created_at timestamp DEFAULT now())",
    "INSERT INTO uitest_wide (val, note) SELECT 'val-'||i, 'note-'||i FROM generate_series(1,60) AS i",
  ];
}
function wideDDL_maria() {
  return [
    "DROP TABLE IF EXISTS uitest_wide",
    "CREATE TABLE uitest_wide (id INT AUTO_INCREMENT PRIMARY KEY, val text, note text, " +
      "created_at timestamp DEFAULT CURRENT_TIMESTAMP) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
    "INSERT INTO uitest_wide (val, note) WITH RECURSIVE seq(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM seq WHERE i<60) " +
      "SELECT CONCAT('val-',i), CONCAT('note-',i) FROM seq",
  ];
}
function seedWide(engines, engine) {
  const stmts = engine.id === "pg" ? wideDDL_pg() : wideDDL_maria();
  for (const s of stmts) runSQL(engines, engine, s);
}
function dropWide(engines, engine) {
  try { runSQL(engines, engine, "DROP TABLE IF EXISTS uitest_wide"); } catch (_) { /* best effort */ }
}

// ================= fixtures: uitest_ch (clickhouse, view-only) =================
function seedCh(engines) {
  chSQL(engines, "DROP TABLE IF EXISTS uitest_ch");
  chSQL(engines, "CREATE TABLE uitest_ch (id UInt32, name String, val Float64, created DateTime) ENGINE = MergeTree() ORDER BY id");
  chSQL(engines, "INSERT INTO uitest_ch (id, name, val, created) VALUES " +
    "(1,'a',1.1,'2026-01-01 10:00:00'),(2,'b',2.2,'2026-01-02 11:00:00'),(3,'c',3.3,'2026-01-03 12:00:00')");
}
function dropCh(engines) {
  try { chSQL(engines, "DROP TABLE IF EXISTS uitest_ch"); } catch (_) { /* best effort */ }
}

// ================= SPA tree helpers =================
// revealAndClickNode expands containers breadth-first until a tree node named
// `name` is visible, then clicks it. Handles both the auto-expanded lone-schema
// case (appendTreePage's "a lone container never costs a click" drill-through,
// app.js:368) and the multi-schema case (needs an explicit click through a
// container first).
//
// Deliberately does NOT fall back to clicking #refresh: an earlier version did,
// and it uncovered a real product race (see TAB-1's dedicated
// refresh-race sub-test) -- clicking #refresh while a service-switch's own
// loadTree(root:true) fetch is still in flight causes BOTH responses to append
// into the tree container (appendTreePage's cleanup only removes .loadmore/
// .state sentinels, never prior .node-wrap content), producing duplicate nodes,
// one of which can end up bound to the wrong service in its onclick closure.
// That's a genuine finding, not something this shared helper should trigger as
// a side effect and destabilize every OTHER scenario with. Patience (more
// tries, no recovery action) is what every non-TAB-1 caller actually needs.
async function revealAndClickNode(frame, name, tries) {
  tries = tries || 30;
  for (let i = 0; i < tries; i++) {
    const res = await frame.evaluate((n) => {
      const rows = Array.from(document.querySelectorAll("#tree .node"));
      const target = rows.find((r) => {
        const nm = r.querySelector(".nname");
        return nm && nm.textContent === n;
      });
      if (target) { target.click(); return "clicked"; }
      const unexpanded = rows.find((r) => {
        const kindEl = r.querySelector(".kind");
        if (!kindEl || kindEl.textContent !== "▸") return false; // "▸" == collapsed container
        const wrap = r.closest(".node-wrap");
        return wrap && !wrap.querySelector(":scope > .children");
      });
      if (unexpanded) { unexpanded.click(); return "expanding"; }
      return "stuck";
    }, name);
    if (res === "clicked") return true;
    await sleep(350);
  }
  return false;
}

async function treeNodeNames(frame) {
  return frame.evaluate(() => Array.from(document.querySelectorAll("#tree .node .nname")).map((el) => el.textContent));
}

async function treeNodeMeta(frame, name) {
  return frame.evaluate((n) => {
    const rows = Array.from(document.querySelectorAll("#tree .node"));
    const row = rows.find((r) => { const nm = r.querySelector(".nname"); return nm && nm.textContent === n; });
    if (!row) return undefined;
    const meta = row.querySelector(".nmeta");
    return meta ? meta.textContent : null;
  }, name);
}

// ================= SPA grid helpers =================
async function waitForGrid(frame, ms) {
  await frame.waitForSelector("table.grid", { timeout: ms || 20000 });
}

async function readGrid(frame) {
  return frame.evaluate(() => {
    const table = document.querySelector("table.grid");
    if (!table) return null;
    const headers = Array.from(table.querySelectorAll("thead th")).map((th) => ({
      text: th.textContent, title: th.getAttribute("title") || "",
    }));
    const rows = Array.from(table.querySelectorAll("tbody.gridbody tr")).map((tr) =>
      Array.from(tr.querySelectorAll("td")).map((td) => ({
        text: td.textContent,
        editable: td.classList.contains("editable"),
        locked: td.classList.contains("locked"),
        title: td.getAttribute("title") || "",
      }))
    );
    const toolbarMeta = document.querySelector(".toolbar .meta");
    const noKeyBadge = document.querySelector(".toolbar .badge.view-only");
    const delBtns = Array.from(document.querySelectorAll("button.rowdel"));
    const insertBtn = document.getElementById("insertrow");
    return {
      headers,
      rows,
      hasDelCol: !!table.querySelector("thead th.delcol"),
      rowDelButtons: delBtns.length,
      // "present" alone conflates two very different bugs: a genuinely
      // clickable affordance in a view-only family (a real functional/
      // security-adjacent violation) vs. one that's rendered but disabled=true
      // (an affordance-honesty/visual-consistency issue -- inert, but the
      // spec's own state canon promises view-only tiers a DISTINCT read-only
      // rendering, not an interactive-looking-but-dead one). Capture both so
      // callers can tell them apart.
      rowDelAnyEnabled: delBtns.some((b) => !b.disabled),
      rowDelTitles: delBtns.map((b) => b.title),
      toolbarMetaText: toolbarMeta ? toolbarMeta.textContent : null,
      noKeyBadgeText: noKeyBadge ? noKeyBadge.textContent : null,
      hasInsertBtn: !!insertBtn,
      insertBtnEnabled: !!insertBtn && !insertBtn.disabled,
      insertBtnTitle: insertBtn ? insertBtn.title : "",
    };
  });
}

function colIdxOf(headers, name) {
  let idx = headers.findIndex((h) => h.text === name);
  if (idx < 0) idx = headers.findIndex((h) => h.text.indexOf(name) === 0);
  return idx;
}
function rowIdxByCellText(rows, colIdx, text) {
  return rows.findIndex((row) => row[colIdx] && row[colIdx].text === text);
}
function cellSelector(rowIdx1, colIdx1) {
  return "table.grid tbody.gridbody tr:nth-child(" + rowIdx1 + ") td:nth-child(" + colIdx1 + ")";
}

// setCellInput clicks a grid cell open (row/col are 0-based) and programmatically
// sets the resulting input.celledit's value + focuses it -- the actual commit
// (Enter) or cancel (Escape) key is dispatched separately by the caller via
// page.keyboard, so the real onkeydown handler in app.js is what's exercised,
// not a synthesized substitute.
async function setCellInput(frame, rowIdx0, colIdx0, newText) {
  const sel = cellSelector(rowIdx0 + 1, colIdx0 + 1);
  await frame.waitForSelector(sel, { timeout: 10000 });
  await frame.click(sel);
  const inputSel = sel + " input.celledit";
  await frame.waitForSelector(inputSel, { timeout: 10000 });
  await frame.evaluate((s, v) => {
    const el = document.querySelector(s);
    if (el) { el.value = v; el.focus(); }
  }, inputSel, newText);
  return { sel, inputSel };
}

// drainToast waits out any currently-visible toast before the caller triggers
// the NEXT mutation. Toasts self-remove after 2.6s and waitToast reads the
// FIRST .toast(.good|.bad|.warn) in DOM order (README gotcha #8) -- a second
// mutation fired inside that window can have its own result misread as the
// FIRST mutation's still-fading toast. Any single scenario chaining more than
// one mutation on the same frame must call this between them.
async function drainToast(frame, maxWaitMs) {
  const deadline = Date.now() + (maxWaitMs || 3200);
  while (Date.now() < deadline) {
    const present = await frame.evaluate(() => !!document.querySelector(".toast.good, .toast.bad, .toast.warn"));
    if (!present) return;
    await sleep(150);
  }
}

async function cellHasInput(frame, rowIdx0, colIdx0) {
  const sel = cellSelector(rowIdx0 + 1, colIdx0 + 1) + " input.celledit";
  return frame.evaluate((s) => !!document.querySelector(s), sel);
}

// ================= shared open helpers =================
// openNode retries ONCE (via a fresh sidebarBrowse re-browse) when the first
// attempt hits the wrong-service stale-node race (see the finding filed
// below) -- that race is a genuine, already-documented product bug (TAB-1's
// refresh-race sub-test + this function's own finding), but without a retry
// it silently forfeits every OTHER assertion the calling scenario wanted to
// make for this engine (openNode returning null makes every caller `continue`
// past the whole engine). The finding is filed on first occurrence regardless
// of whether the retry then succeeds.
async function openNode(ctx, engine, nodeName, writeMode) {
  const first = await openNodeAttempt(ctx, engine, nodeName, writeMode, false);
  if (first.spa) return first.spa;
  if (first.reason !== "wrongService") return null;
  const retry = await openNodeAttempt(ctx, engine, nodeName, writeMode, true);
  return retry.spa || null;
}

async function openNodeAttempt(ctx, engine, nodeName, writeMode, isRetry) {
  const { page, harness, addFinding } = ctx;
  const spa = isRetry
    ? await harness.sidebarBrowse(page, engine.service)
    : await harness.openConsole(page, engine.service);
  await harness.setWriteMode(page, spa, writeMode);
  // Proactively wait for the rail to confirm the switch landed on the RIGHT
  // service before touching the tree at all -- cheap insurance against the
  // wrong-service stale-node race (see the finding filed below): if a
  // same-named node from a PRIOR engine's session is still mid-clear when we
  // start searching, we want to have at least given the switch every chance
  // to settle first.
  try {
    await spa.waitForFunction(
      (svc) => {
        const li = document.querySelector("#services li.active span");
        return !!li && li.textContent === svc;
      },
      { timeout: 8000 },
      engine.service
    );
  } catch (_) { /* fall through -- the post-click wrong-service check below still catches it */ }
  await sleep(isRetry ? 900 : 500);
  const ok = await revealAndClickNode(spa, nodeName);
  if (!ok) {
    if (!isRetry) {
      addFinding({
        severity: "S1",
        title: '"' + nodeName + '" not revealable in the tree (' + engine.label + ")",
        repro: "openConsole('" + engine.service + "'); look for '" + nodeName + "' under #tree .node .nname",
        expected: "node reachable in the tree (directly, or after expanding a schema/database container)",
        actual: "revealAndClickNode gave up after repeated tries; tree names seen: " + JSON.stringify(await treeNodeNames(spa)),
        evidence: [await ctx.evidence(engine.id + "-" + nodeName + "-reveal-failed")],
      });
    }
    return { spa: null, reason: "notRevealable" };
  }
  try {
    await waitForGrid(spa);
  } catch (_) {
    // The click landed but no grid ever appeared -- distinguish an honest
    // inline error (a real, evidence-worthy product outcome) from a truly
    // stuck/blank pane (a harness-can't-proceed situation).
    const errInfo = await spa.evaluate((svc) => {
      const err = document.querySelector("#content .err");
      const where = document.querySelector("#content .err-where");
      const treeNames = Array.from(document.querySelectorAll("#tree .node .nname")).map((el) => el.textContent);
      const counts = {};
      for (const n of treeNames) counts[n] = (counts[n] || 0) + 1;
      const duped = Object.keys(counts).filter((n) => counts[n] > 1);
      return {
        hasErr: !!err,
        text: err ? err.textContent : "",
        whereText: where ? where.textContent : "",
        wrongService: !!where && where.textContent.split(" · ")[0] !== svc && where.textContent.split(" · ")[0] !== "",
        treeHasDupes: duped.length > 0,
        contentHTML: (document.getElementById("content") || {}).innerHTML || "",
      };
    }, engine.service);
    const shot = await ctx.evidence(engine.id + "-" + nodeName + "-grid-timeout" + (isRetry ? "-retry" : ""));
    if (errInfo.hasErr && errInfo.wrongService) {
      // Same root cause as TAB-1's refresh-race duplicate-node finding: an
      // overlapping loadTree(root:true) call left a stale duplicate node in
      // the tree whose onclick closure is bound to a DIFFERENT service than
      // the one currently active/displayed. Here it 404s (the wrong service's
      // same-named object was already dropped by this file's own teardown),
      // but the same mechanism would silently READ -- or, with write mode on,
      // WRITE -- to the wrong service with no visible cue, which is why this
      // is S1 rather than S2 (a plain "one open failed" finding). Filed once,
      // on the first (non-retry) attempt only -- the caller retries via a
      // fresh sidebarBrowse to recover and continue testing regardless.
      if (!isRetry) {
        addFinding({
          id: "TAB-family-" + engine.id + "-stale-node-wrong-service",
          severity: "S1",
          title: "A tree node's click resolved against the WRONG service, not the one currently active (" + engine.label + ")",
          repro: "reveal+click '" + nodeName + "' after openConsole('" + engine.service + "'); the tree contained " +
            (errInfo.treeHasDupes ? "duplicate node(s) (see TAB-1's refresh-race finding for the duplication mechanism)"
              : "no visible duplicates this time, but the error envelope still names the wrong service"),
          expected: "the request targets '" + engine.service + "' (the active/displayed service)",
          actual: 'error envelope service·family = "' + errInfo.whereText + '" (expected "' + engine.service +
            ' · tabular"); text=' + JSON.stringify(errInfo.text),
          evidence: [shot],
          engine_truth: "n/a -- UI/request targeting divergence, not a data mutation",
        });
      }
      return { spa: null, reason: "wrongService" };
    }
    if (errInfo.hasErr) {
      if (!isRetry) {
        addFinding({
          severity: "S2",
          title: "Opening '" + nodeName + "' rendered an error instead of the grid (" + engine.label + ")",
          repro: "reveal+click '" + nodeName + "' in the tree after openConsole('" + engine.service + "')",
          expected: "table.grid renders",
          actual: "inline error rendered instead: " + JSON.stringify(errInfo.text),
          evidence: [shot],
        });
      }
      return { spa: null, reason: "otherError" };
    }
    if (!isRetry) {
      addFinding({
        severity: "S1",
        title: "Clicking '" + nodeName + "' produced neither a grid nor an inline error (" + engine.label + ")",
        repro: "reveal+click '" + nodeName + "' in the tree after openConsole('" + engine.service + "')",
        expected: "table.grid renders",
        actual: "content: " + JSON.stringify(errInfo.contentHTML).slice(0, 500),
        evidence: [shot],
      });
    }
    return { spa: null, reason: "blank" };
  }
  await sleep(250);
  return { spa, reason: null };
}

async function openTabWritable(ctx, engine) {
  seedTab(ctx.engines, engine);
  await sleep(150);
  return openNode(ctx, engine, "uitest_tab", true);
}

// classifyWriteOutcome distinguishes four outcomes for a (toast, engine-matches-
// intent) pair. Naively lumping "toast wasn't good" and "engine didn't match"
// together as one "did not commit" bucket hides WHICH side is lying: a toast
// claiming success while the engine disagrees is a success-lie (I-1 class, the
// UI over-claims); a toast claiming FAILURE while the engine shows the write
// actually applied is the mirror-image failure-lie (arguably worse -- a user
// sees a scary "Save failed" for a write that worked and may destructively
// retry). Both are real, different bugs from a plain "the edit was rejected,
// consistently" outcome.
function classifyWriteOutcome(toast, engineMatchesNewValue) {
  const toastGood = !!toast && toast.kind === "good";
  if (toastGood && engineMatchesNewValue) return "ok";
  if (toastGood && !engineMatchesNewValue) return "success-lie";
  if (!toastGood && engineMatchesNewValue) return "failure-lie";
  return "consistent-failure";
}

function oppositeBoolText(cur) {
  const t = String(cur).trim().toLowerCase();
  if (t === "true") return "false";
  if (t === "false") return "true";
  if (t === "1") return "0";
  if (t === "0") return "1";
  if (t === "t") return "f";
  if (t === "f") return "t";
  return "false";
}

// ============================================================
// TAB-1 — tree: schemas -> tables visible; a just-created table's reveal path;
// row-count tree metadata honesty (if the server renders one).
// ============================================================
async function runTab1(ctx) {
  const { page, harness, engines, addFinding } = ctx;
  const evidence = trackEvidence(ctx.evidence);
  for (const engine of ENGINES) {
    dropTab(engines, engine);
    try {
      const spa = await harness.openConsole(page, engine.service);
      await harness.setWriteMode(page, spa, false);
      await harness.clickService(spa, engine.service); // force a fresh selectService()/loadTree()
      await sleep(500);
      const before = await treeNodeNames(spa);
      await evidence(engine.id + "-01-tree-before-seed");

      seedTab(engines, engine);
      await sleep(300);

      let names = await treeNodeNames(spa);
      let refreshLevel = names.includes("uitest_tab") ? "auto-visible" : null;

      if (!refreshLevel) {
        await spa.click("#refresh");
        await sleep(700);
        names = await treeNodeNames(spa);
        refreshLevel = names.includes("uitest_tab") ? "topbar-refresh" : null;
      }
      let spaAfter = spa;
      if (!refreshLevel) {
        spaAfter = await harness.sidebarBrowse(page, engine.service);
        await sleep(500);
        names = await treeNodeNames(spaAfter);
        refreshLevel = names.includes("uitest_tab") ? "sidebar-rebrowse" : "still-not-visible";
      }
      await evidence(engine.id + "-02-tree-after-seed-" + refreshLevel);

      if (refreshLevel === "still-not-visible") {
        addFinding({
          severity: "S2",
          title: "A freshly created table never appears in the tree, even after a sidebar re-browse (" + engine.label + ")",
          repro: "openConsole('" + engine.service + "'); CREATE TABLE uitest_tab ...; #refresh; then sidebarBrowse again",
          expected: "uitest_tab visible in the tree via at least one of: no action / #refresh / sidebar re-browse",
          actual: "still absent; tree names: " + JSON.stringify(names),
          evidence: [evidence.__lastPath || ""],
        });
        continue;
      }

      const ok = await revealAndClickNode(spaAfter, "uitest_tab", 6);
      if (!ok) {
        addFinding({
          severity: "S1",
          title: "uitest_tab visible by name but not clickable/openable (" + engine.label + ")",
          repro: "table name appears in #tree after " + refreshLevel + " but clicking it never opens a grid",
          expected: "clicking the table node opens its grid",
          actual: "table.grid never appeared",
          evidence: [await evidence(engine.id + "-03-click-failed")],
        });
        continue;
      }
      await waitForGrid(spaAfter);
      await sleep(200);
      const grid = await readGrid(spaAfter);
      await evidence(engine.id + "-04-table-opened");

      const engineRows = engineCount(engines, engine, "uitest_tab");
      if (!grid || grid.rows.length !== engineRows) {
        addFinding({
          severity: "S1",
          title: "Row count in the opened grid does not match the engine (" + engine.label + ")",
          repro: "SELECT count(*) FROM uitest_tab vs. rendered tbody.gridbody tr count",
          expected: String(engineRows) + " rows",
          actual: (grid ? grid.rows.length : "no grid") + " rows rendered",
          evidence: [evidence.__lastPath || ""],
          engine_truth: "COUNT(*) = " + engineRows,
        });
      }

      const meta = await treeNodeMeta(spaAfter, "uitest_tab");
      if (meta != null) {
        const digits = (String(meta).match(/\d+/) || [])[0];
        if (digits && parseInt(digits, 10) !== engineRows) {
          addFinding({
            severity: "S2",
            title: "Tree row-count metadata for a table is dishonest vs. the engine (" + engine.label + ")",
            repro: "read #tree .node .nmeta for uitest_tab; compare to SELECT count(*)",
            expected: String(engineRows),
            actual: meta,
            evidence: [await evidence(engine.id + "-05-nmeta")],
            engine_truth: "COUNT(*) = " + engineRows,
          });
        }
      }

      // ---- deliberate refresh-race sub-test ----
      // Discovered incidentally while building this harness (a slow-to-settle
      // tree during TAB-2's setup made an earlier revealAndClickNode variant's
      // #refresh fallback fire mid-flight). clickService() fires
      // loadTree(root:true), which synchronously resets #tree to a loading
      // placeholder and kicks off an async /api/tree fetch; clicking #refresh
      // immediately after (without waiting for that fetch to settle) fires a
      // SECOND loadTree(root:true) for the same container. appendTreePage's
      // cleanup only ever removes .loadmore/.state sentinels, never prior
      // .node-wrap content, so if the first fetch's nodes land before the
      // second reset, the second response appends its own copy on top.
      // Racing this reliably depends on ambient backend latency (how long the
      // FIRST /api/tree fetch stays in flight), which this harness doesn't
      // control -- so widen the window with a burst of rapid #refresh clicks
      // (each re-fires loadTree(root:true)) rather than a single double-click,
      // and give it a few attempts within this run.
      let raceNames = [];
      for (let attempt = 0; attempt < 3; attempt++) {
        await harness.clickService(spaAfter, engine.service);
        for (let i = 0; i < 4; i++) {
          await spaAfter.click("#refresh"); // fired without waiting -- widen the race window
        }
        await sleep(1200); // let every in-flight loadTree call settle
        raceNames = await treeNodeNames(spaAfter);
        const counts = {};
        for (const n of raceNames) counts[n] = (counts[n] || 0) + 1;
        if (Object.values(counts).some((c) => c > 1)) break; // reproduced -- stop early, evaluate below
      }
      await evidence(engine.id + "-06-refresh-race");
      const raceCounts = {};
      for (const n of raceNames) raceCounts[n] = (raceCounts[n] || 0) + 1;
      const dupes = Object.entries(raceCounts).filter(([, c]) => c > 1);
      if (dupes.length) {
        addFinding({
          id: "TAB-1-" + engine.id + "-refresh-race-duplicates",
          severity: "S2",
          title: "Clicking Refresh while a service switch's tree load is still in flight duplicates tree nodes (" + engine.label + ")",
          repro: "clickService(spa, '" + engine.service + "') [fires loadTree(root:true)]; immediately click #refresh " +
            "[fires a second loadTree(root:true) for the same container] without waiting for the first to settle",
          expected: "tree shows each node exactly once regardless of overlapping refreshes",
          actual: "duplicated names: " + JSON.stringify(dupes) + "; full list: " + JSON.stringify(raceNames),
          evidence: [evidence.__lastPath || ""],
        });

        // A duplicate node's onclick closure can end up bound to a stale
        // service (this is precisely what made TAB-2 time out waiting for a
        // grid: the click landed, but /api/table came back "Not found" for
        // the wrong service). Verify directly: click uitest_tab again (there
        // are now >=2 copies) and confirm it still opens correctly.
        await revealAndClickNode(spaAfter, "uitest_tab", 3);
        await sleep(800);
        const postClick = await spaAfter.evaluate(() => ({
          hasGrid: !!document.querySelector("table.grid"),
          errText: (document.querySelector("#content .err-msg") || {}).textContent || null,
          errWhere: (document.querySelector("#content .err-where") || {}).textContent || null,
        }));
        await evidence(engine.id + "-07-refresh-race-click-result");
        if (!postClick.hasGrid) {
          addFinding({
            id: "TAB-1-" + engine.id + "-refresh-race-wrong-service",
            severity: "S1",
            title: "A duplicated tree node from the refresh race fails to open (wrong service / stale closure), " +
              "instead of the grid (" + engine.label + ")",
            repro: "after the duplicate-node race above, click the (now duplicated) 'uitest_tab' node",
            expected: "opens uitest_tab on " + engine.service + " normally",
            actual: JSON.stringify(postClick),
            evidence: [evidence.__lastPath || ""],
          });
        }
      }
    } finally {
      dropTab(engines, engine);
    }
  }
}

// ============================================================
// TAB-2 — paging: uitest_wide (60 rows), page size, Load more, honest final
// count vs. engine, and whether the toolbar's "N rows" label updates.
// ============================================================
async function runTab2(ctx) {
  const { engines, addFinding } = ctx;
  const evidence = trackEvidence(ctx.evidence);
  for (const engine of ENGINES) {
    dropWide(engines, engine);
    try {
      seedWide(engines, engine);
      await sleep(150);
      const spa = await openNode(ctx, engine, "uitest_wide", false);
      if (!spa) continue;

      let grid = await readGrid(spa);
      await evidence(engine.id + "-01-first-page");
      const firstPageCount = grid.rows.length;
      const firstPageMeta = grid.toolbarMetaText;
      const engineTotal = engineCount(engines, engine, "uitest_wide");

      let clicks = 0;
      while (await spa.evaluate(() => !!document.querySelector("button.loadmore")) && clicks < 10) {
        await spa.click("button.loadmore");
        await sleep(600);
        clicks++;
      }
      await evidence(engine.id + "-02-after-loadmore-x" + clicks);
      grid = await readGrid(spa);
      const finalCount = grid.rows.length;
      const finalMeta = grid.toolbarMetaText;

      if (clicks === 0 && firstPageCount === engineTotal) {
        // No pagination triggered at all -- page size >= 60. Not a bug, just
        // means this fixture doesn't exercise Load More; note only.
      } else if (finalCount !== engineTotal) {
        addFinding({
          severity: "S1",
          title: "Final rendered row count after exhausting Load More does not match the engine (" + engine.label + ")",
          repro: "open uitest_wide (60 rows); click .loadmore until it disappears (" + clicks + " click(s)); count tbody rows",
          expected: String(engineTotal) + " rows",
          actual: finalCount + " rows rendered",
          evidence: [evidence.__lastPath || ""],
          engine_truth: "COUNT(*) = " + engineTotal,
        });
      }

      if (clicks > 0 && finalMeta === firstPageMeta && /\d+/.test(String(firstPageMeta))) {
        addFinding({
          severity: "S3",
          title: 'Toolbar "N rows" label does not update after Load More (' + engine.label + ")",
          repro: "open uitest_wide; note toolbar .meta text on page 1 (" + firstPageMeta + "); click Load More " + clicks +
            " time(s); re-read toolbar .meta",
          expected: "label reflects the growing/total row count after Load More",
          actual: 'label unchanged: "' + finalMeta + '" while tbody actually renders ' + finalCount + " rows",
          evidence: [evidence.__lastPath || ""],
        });
      }
    } finally {
      dropWide(engines, engine);
    }
  }
}

// ============================================================
// TAB-3 — cell edit + LAYOUT STABILITY: neighbor cell bboxes must not shift
// across click-to-edit / edit-open / commit. Then a normal commit round-trips
// to the engine.
// ============================================================
async function runTab3(ctx) {
  const { page, harness, engines, addFinding } = ctx;
  const evidence = trackEvidence(ctx.evidence);
  for (const engine of ENGINES) {
    dropTab(engines, engine);
    try {
      const spa = await openTabWritable(ctx, engine);
      if (!spa) continue;

      let grid = await readGrid(spa);
      const txtCol = colIdxOf(grid.headers, "txt");
      const idCol = colIdxOf(grid.headers, "id");
      const numCol = colIdxOf(grid.headers, "num");
      const plainRow = rowIdxByCellText(grid.rows, txtCol, "plain");
      const otherRow = plainRow === 0 ? 1 : 0;

      // Capture engine identity BEFORE editing txt (the column we're about to change).
      const targetId = String(runSQL(engines, engine, "SELECT id FROM uitest_tab WHERE txt='plain'")).trim();

      const targetSel = cellSelector(plainRow + 1, txtCol + 1);
      const neighbors = {
        table: "table.grid",
        leftSameRow: cellSelector(plainRow + 1, idCol + 1),
        rightSameRow: cellSelector(plainRow + 1, numCol + 1),
        belowSameCol: cellSelector(otherRow + 1, txtCol + 1),
      };
      async function snapAll() {
        const out = {};
        for (const k of Object.keys(neighbors)) out[k] = await harness.bbox(spa, neighbors[k]);
        return out;
      }

      const before = await snapAll();
      const shotBefore = await evidence(engine.id + "-01-before-click");

      await spa.click(targetSel);
      await spa.waitForSelector(targetSel + " input.celledit", { timeout: 10000 });
      const during = await snapAll();
      const shotDuring = await evidence(engine.id + "-02-editor-open");

      const newVal = "edited-" + engine.id;
      await spa.evaluate((s, v) => { const el = document.querySelector(s); if (el) { el.value = v; el.focus(); } },
        targetSel + " input.celledit", newVal);
      await page.keyboard.press("Enter");
      const toast = await harness.waitToast(spa);
      await sleep(300);
      const after = await snapAll();
      const shotAfter = await evidence(engine.id + "-03-after-commit");

      const shifted = [];
      for (const k of Object.keys(neighbors)) {
        if (k === "table") continue; // table itself may legitimately resize; only NEIGHBOR cells must be stable
        const a = before[k], b = during[k], c = after[k];
        if (!a || !b || !c) { shifted.push(k + ": missing bbox snapshot"); continue; }
        const dab = Math.max(Math.abs(a.x - b.x), Math.abs(a.y - b.y), Math.abs(a.width - b.width), Math.abs(a.height - b.height));
        const dac = Math.max(Math.abs(a.x - c.x), Math.abs(a.y - c.y), Math.abs(a.width - c.width), Math.abs(a.height - c.height));
        if (dab > 2 || dac > 2) shifted.push(k + ": before->during=" + dab.toFixed(1) + "px, before->after=" + dac.toFixed(1) + "px");
      }
      if (shifted.length) {
        addFinding({
          severity: "S2",
          title: "Neighbor grid cells shift position/size when a cell enters edit mode (" + engine.label + ")",
          repro: "write mode on; open uitest_tab; click the 'txt' cell of the 'plain' row; compare neighbor bboxes " +
            "before click / editor open / after commit",
          expected: "neighbor cell bboxes stable within 2px across all three states",
          actual: shifted.join("; "),
          evidence: [shotBefore, shotDuring, shotAfter],
        });
      }

      if (!toast || toast.kind !== "good") {
        addFinding({
          severity: "S1",
          title: "Committing a valid txt cell edit did not produce a success toast (" + engine.label + ")",
          repro: "edit 'txt' cell of 'plain' row to '" + newVal + "'; press Enter",
          expected: "good toast",
          actual: toast ? JSON.stringify(toast) : "no toast observed",
          evidence: [evidence.__lastPath || ""],
        });
      }
      const engineTxt = String(runSQL(engines, engine, "SELECT txt FROM uitest_tab WHERE id=" + targetId)).trim();
      if (engineTxt !== newVal) {
        addFinding({
          severity: "S1",
          title: "Cell edit toast said success but the engine value does not match (success-lie, " + engine.label + ")",
          repro: "edit txt to '" + newVal + "'; SELECT txt FROM uitest_tab WHERE id=" + targetId,
          expected: newVal,
          actual: engineTxt,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "txt = " + JSON.stringify(engineTxt),
        });
      }
    } finally {
      dropTab(engines, engine);
    }
  }
}

// ============================================================
// TAB-4 — cell types: valid+invalid num, flag toggle, ts, valid+invalid js,
// maybe_null clear (NULL affordance gap), Escape-cancel honesty.
// ============================================================
async function runTab4(ctx) {
  const { page, harness, engines, addFinding } = ctx;
  const evidence = trackEvidence(ctx.evidence);
  for (const engine of ENGINES) {
    dropTab(engines, engine);
    try {
      const spa = await openTabWritable(ctx, engine);
      if (!spa) continue;

      const grid0 = await readGrid(spa);
      const cols = { txt: colIdxOf(grid0.headers, "txt"), num: colIdxOf(grid0.headers, "num"),
        flag: colIdxOf(grid0.headers, "flag"), ts: colIdxOf(grid0.headers, "ts"), js: colIdxOf(grid0.headers, "js"),
        maybe_null: colIdxOf(grid0.headers, "maybe_null") };
      const plainRow = rowIdxByCellText(grid0.rows, cols.txt, "plain");
      const unicodeRow = rowIdxByCellText(grid0.rows, cols.txt, UNICODE_TEXT);
      const row4 = rowIdxByCellText(grid0.rows, cols.txt, "row4");

      async function commitEnter(rowIdx0, colIdx0, val) {
        await drainToast(spa); // guarantee THIS mutation's toast isn't a stale read of the prior one (gotcha #8)
        await setCellInput(spa, rowIdx0, colIdx0, val);
        await page.keyboard.press("Enter");
        const t = await harness.waitToast(spa);
        await sleep(200);
        return t;
      }

      // ---- num: valid then invalid ----
      const oldNum = parseFloat(String(runSQL(engines, engine, "SELECT num FROM uitest_tab WHERE txt='plain'")).trim());
      const tValid = await commitEnter(plainRow, cols.num, "55.55");
      await evidence(engine.id + "-01-num-valid");
      const numNow = parseFloat(String(runSQL(engines, engine, "SELECT num FROM uitest_tab WHERE txt='plain'")).trim());
      const numValidOutcome = classifyWriteOutcome(tValid, Math.abs(numNow - 55.55) <= 0.001);
      if (numValidOutcome !== "ok") {
        addFinding({
          severity: "S1",
          title: (numValidOutcome === "failure-lie"
            ? "Valid numeric edit: toast claimed FAILURE but the engine actually applied it (failure-lie, "
            : numValidOutcome === "success-lie"
            ? "Valid numeric edit: toast claimed success but the engine value does not match (success-lie, "
            : "Valid numeric cell edit did not commit (") + engine.label + ")",
          repro: "edit 'num' cell of 'plain' row (was " + oldNum + ") to 55.55",
          expected: "good toast; engine num=55.55",
          actual: "toast=" + JSON.stringify(tValid) + "; engine num=" + numNow,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "num = " + numNow,
        });
      }

      const tInvalid = await commitEnter(plainRow, cols.num, "abc");
      await evidence(engine.id + "-02-num-invalid");
      const numAfterInvalid = parseFloat(String(runSQL(engines, engine, "SELECT num FROM uitest_tab WHERE txt='plain'")).trim());
      const invalidNumUnchanged = Math.abs(numAfterInvalid - 55.55) <= 0.001; // 55.55 is the only safe outcome
      if (!invalidNumUnchanged) {
        addFinding({
          severity: "S1",
          title: "Non-numeric text was actually written into a numeric column (" + engine.label + ")",
          repro: "edit 'num' cell of 'plain' row to 'abc'",
          expected: "engine num stays 55.55",
          actual: "engine num=" + numAfterInvalid,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "num = " + numAfterInvalid,
        });
      } else if (tInvalid && tInvalid.kind === "good") {
        // Safely no-op'd (engine unchanged) but the UI still claims success --
        // a success-lie on a rejected write, not "bad data got stored".
        addFinding({
          severity: "S1",
          title: "Invalid numeric input silently no-ops but still shows a SUCCESS toast (success-lie, " + engine.label + ")",
          repro: "edit 'num' cell of 'plain' row to 'abc'",
          expected: "either an honest rejection toast, or (if silently ignored) no success claim",
          actual: 'toast claimed success ("' + tInvalid.text + '") but engine num is unchanged at ' + numAfterInvalid,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "num = " + numAfterInvalid,
        });
      }

      // ---- flag: toggle ----
      const oldFlagRaw = String(runSQL(engines, engine, "SELECT flag FROM uitest_tab WHERE txt='plain'")).trim();
      const gridForFlag = await readGrid(spa);
      const curFlagText = gridForFlag.rows[plainRow][cols.flag].text;
      const tFlag = await commitEnter(plainRow, cols.flag, oppositeBoolText(curFlagText));
      await evidence(engine.id + "-03-flag-toggle");
      const newFlagRaw = String(runSQL(engines, engine, "SELECT flag FROM uitest_tab WHERE txt='plain'")).trim();
      const flagOutcome = classifyWriteOutcome(tFlag, newFlagRaw !== oldFlagRaw);
      if (flagOutcome !== "ok") {
        addFinding({
          severity: "S1",
          title: (flagOutcome === "failure-lie"
            ? "Boolean edit: toast claimed FAILURE but the engine actually flipped (failure-lie, "
            : flagOutcome === "success-lie"
            ? "Boolean edit: toast claimed success but the engine value did not flip (success-lie, "
            : "Boolean cell edit did not flip the engine value (") + engine.label + ")",
          repro: "edit 'flag' cell of 'plain' row from " + JSON.stringify(curFlagText) + " to " + oppositeBoolText(curFlagText),
          expected: "good toast; engine flag flips from " + oldFlagRaw,
          actual: "toast=" + JSON.stringify(tFlag) + "; engine flag now " + newFlagRaw,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "flag = " + newFlagRaw,
        });
      }

      // ---- ts: valid ----
      const tTs = await commitEnter(plainRow, cols.ts, "2027-06-15 08:30:00");
      await evidence(engine.id + "-04-ts-valid");
      const tsOut = String(runSQL(engines, engine, "SELECT ts FROM uitest_tab WHERE txt='plain'"));
      const tsMatches = /2027-06-15/.test(tsOut) && /08:30:00/.test(tsOut);
      const tsOutcome = classifyWriteOutcome(tTs, tsMatches);
      if (tsOutcome !== "ok") {
        addFinding({
          severity: "S1",
          title: (tsOutcome === "failure-lie"
            ? "Timestamp edit: toast claimed FAILURE but the engine actually applied it (failure-lie, "
            : tsOutcome === "success-lie"
            ? "Timestamp edit: toast claimed success but the engine value does not match (success-lie, "
            : "Timestamp cell edit did not commit the new value (") + engine.label + ")",
          repro: "edit 'ts' cell of 'plain' row to '2027-06-15 08:30:00'",
          expected: "good toast; engine ts reflects 2027-06-15 08:30:00",
          actual: "toast=" + JSON.stringify(tTs) + "; engine ts=" + JSON.stringify(tsOut),
          evidence: [evidence.__lastPath || ""],
          engine_truth: "ts = " + tsOut,
        });
      }

      // ---- js: valid then invalid ----
      const tJsValid = await commitEnter(plainRow, cols.js, '{"edited":true}');
      await evidence(engine.id + "-05-js-valid");
      const jsOut1 = String(runSQL(engines, engine, "SELECT js FROM uitest_tab WHERE txt='plain'")).trim();
      let jsMatches = false;
      try { jsMatches = JSON.stringify(JSON.parse(jsOut1)) === JSON.stringify({ edited: true }); } catch (_) { /* leave false */ }
      const jsValidOutcome = classifyWriteOutcome(tJsValid, jsMatches);
      if (jsValidOutcome !== "ok") {
        addFinding({
          severity: "S1",
          title: (jsValidOutcome === "failure-lie"
            ? "Valid JSON edit: toast claimed FAILURE but the engine actually applied it (failure-lie, "
            : jsValidOutcome === "success-lie"
            ? "Valid JSON edit: toast claimed success but the engine value does not match (success-lie, "
            : "Valid JSON cell edit did not commit correctly (") + engine.label + ")",
          repro: 'edit \'js\' cell of \'plain\' row to {"edited":true}',
          expected: 'good toast; engine js parses to {"edited":true}',
          actual: "toast=" + JSON.stringify(tJsValid) + "; engine js=" + JSON.stringify(jsOut1),
          evidence: [evidence.__lastPath || ""],
          engine_truth: "js = " + jsOut1,
        });
      }
      const tJsInvalid = await commitEnter(plainRow, cols.js, "{not valid json");
      await evidence(engine.id + "-06-js-invalid");
      const jsOut2 = String(runSQL(engines, engine, "SELECT js FROM uitest_tab WHERE txt='plain'")).trim();
      const jsInvalidUnchanged = jsOut2 === jsOut1;
      addFinding({
        severity: "S3",
        title: "Invalid-JSON cell edit outcome on a JSON column (" + engine.label + ") -- dialect observation",
        repro: "edit 'js' cell of 'plain' row to the malformed text '{not valid json'",
        expected: "n/a -- recording actual behavior for cross-engine comparison",
        actual: "toast=" + JSON.stringify(tJsInvalid) + "; engine js before=" + JSON.stringify(jsOut1) +
          " after=" + JSON.stringify(jsOut2) + "; " + (jsInvalidUnchanged ? "rejected (unchanged)" : "ACCEPTED malformed JSON"),
        evidence: [evidence.__lastPath || ""],
        engine_truth: "js = " + jsOut2,
      });
      if (!jsInvalidUnchanged) {
        addFinding({
          severity: "S1",
          title: "Malformed JSON text was written into a JSON-typed column (" + engine.label + ")",
          repro: "edit 'js' cell to the malformed text '{not valid json'",
          expected: "rejected; engine js stays " + JSON.stringify(jsOut1),
          actual: "engine js is now " + JSON.stringify(jsOut2),
          evidence: [evidence.__lastPath || ""],
          engine_truth: "js = " + jsOut2,
        });
      } else if (tJsInvalid && tJsInvalid.kind === "good") {
        addFinding({
          severity: "S2",
          title: "Malformed JSON silently no-ops but still shows a SUCCESS toast (success-lie, " + engine.label + ")",
          repro: "edit 'js' cell to the malformed text '{not valid json'",
          expected: "either an honest rejection toast, or (if silently ignored) no success claim",
          actual: 'toast claimed success ("' + tJsInvalid.text + '") but engine js is unchanged at ' + JSON.stringify(jsOut1),
          evidence: [evidence.__lastPath || ""],
          engine_truth: "js = " + jsOut2,
        });
      }

      // ---- maybe_null: clear an existing value -- is there any way to reach SQL NULL? ----
      const gridForNull = await readGrid(spa);
      const beforeClearText = gridForNull.rows[unicodeRow][cols.maybe_null].text;
      const tClear = await commitEnter(unicodeRow, cols.maybe_null, "");
      await evidence(engine.id + "-07-maybe-null-cleared");
      // NB: MariaDB's `||` is logical OR unless PIPES_AS_CONCAT is set -- this
      // must NOT reuse postgres's `||` concatenation syntax, or the mariadb
      // probe silently returns a boolean 0/1 instead of the intended string.
      const escapedUnicode = UNICODE_TEXT.replace(/'/g, "''");
      const nullProbeSQL = engine.id === "pg"
        ? "SELECT CASE WHEN maybe_null IS NULL THEN 'NULL' ELSE 'NOTNULL:' || maybe_null END FROM uitest_tab WHERE txt='" + escapedUnicode + "'"
        : "SELECT CASE WHEN maybe_null IS NULL THEN 'NULL' ELSE CONCAT('NOTNULL:', maybe_null) END FROM uitest_tab WHERE txt='" + escapedUnicode + "'";
      const nullCheck = String(runSQL(engines, engine, nullProbeSQL)).trim();
      if (nullCheck !== "NULL") {
        addFinding({
          id: "TAB-4-" + engine.id + "-no-null-affordance",
          severity: "S3",
          title: "No UI affordance to set a non-null cell back to SQL NULL (" + engine.label + ")",
          repro: "cell 'maybe_null' started as '" + beforeClearText + "'; click to edit, clear the input to empty, press Enter",
          expected: "either an explicit way to set NULL, or the empty-string result is clearly labelled (not silently " +
            "conflated with NULL)",
          actual: "clearing the input committed as: " + nullCheck + " (toast=" + JSON.stringify(tClear) + ") -- there is no " +
            "distinct 'set to NULL' action anywhere in the cell editor",
          evidence: [evidence.__lastPath || ""],
          engine_truth: nullCheck,
        });
      }

      // ---- Escape cancels cleanly? (row4, num column; blur-after-DOM-removal hazard) ----
      await drainToast(spa); // the maybe_null clear above has its own toast -- don't let escToast read it
      const engineNumBefore = String(runSQL(engines, engine, "SELECT num FROM uitest_tab WHERE txt='row4'")).trim();
      const gridForEsc = await readGrid(spa);
      const uiTextBefore = gridForEsc.rows[row4][cols.num].text;
      await setCellInput(spa, row4, cols.num, "999");
      await page.keyboard.press("Escape");
      const escToast = await harness.waitToast(spa, 3000);
      await sleep(400);
      await evidence(engine.id + "-08-after-escape");
      const gridAfterEsc = await readGrid(spa);
      const uiTextAfter = gridAfterEsc.rows[row4][cols.num].text;
      const engineNumAfter = String(runSQL(engines, engine, "SELECT num FROM uitest_tab WHERE txt='row4'")).trim();
      const stillHasInput = await cellHasInput(spa, row4, cols.num);
      if (uiTextAfter !== uiTextBefore || engineNumAfter !== engineNumBefore || stillHasInput ||
          (escToast && escToast.kind === "good")) {
        const committedDespiteEscape = engineNumAfter !== engineNumBefore;
        const toastHonesty = !committedDespiteEscape ? "n/a"
          : (escToast && escToast.kind === "good") ? "at least honest about the (buggy) outcome -- toast said success and it did commit"
          : "ALSO a failure-lie on top of the primary bug -- toast said \"" + (escToast ? escToast.text : "no toast") +
            "\" but the write was actually applied";
        addFinding({
          id: "TAB-4-" + engine.id + "-escape-does-not-cancel",
          severity: "S1",
          title: "Escape does not cleanly cancel a cell edit -- the value commits anyway (" + engine.label + ")",
          repro: "write mode on; open uitest_tab; click 'num' cell of 'row4' (value " + uiTextBefore + "); type '999'; " +
            "press Escape (do not press Enter, do not click elsewhere). Root cause per app.js's editCell(): Escape's " +
            "handler sets td.textContent = fmt(oldVal), which removes the still-focused input from the DOM; browsers " +
            "fire a blur event on a focused element that's removed, and input.onblur = commit was never unbound, so " +
            "commit() runs anyway with the just-typed (unsaved) value.",
          expected: "UI cell reverts to " + JSON.stringify(uiTextBefore) + "; engine num stays " + engineNumBefore +
            "; no request sent",
          actual: "UI now shows " + JSON.stringify(uiTextAfter) + "; engine num now " + engineNumAfter +
            "; toast=" + JSON.stringify(escToast) + "; input still present=" + stillHasInput +
            "; toast honesty: " + toastHonesty,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "num = " + engineNumAfter,
        });
      }
    } finally {
      dropTab(engines, engine);
    }
  }
}

// ============================================================
// TAB-5 — locked cells: PK 'id' column renders td.locked (muted, no edit
// affordance, explanatory title), and a click never opens an editor.
// ============================================================
async function runTab5(ctx) {
  const { engines, addFinding } = ctx;
  const evidence = trackEvidence(ctx.evidence);
  for (const engine of ENGINES) {
    dropTab(engines, engine);
    try {
      const spa = await openTabWritable(ctx, engine); // write mode ON is required -- see design note below
      if (!spa) continue;

      const grid = await readGrid(spa);
      const idCol = colIdxOf(grid.headers, "id");
      await evidence(engine.id + "-01-grid-with-pk");

      const idCells = grid.rows.map((r) => r[idCol]);
      const anyEditable = idCells.some((c) => c.editable);
      const allLocked = idCells.every((c) => c.locked);
      const anyMissingTitle = idCells.some((c) => c.locked && !c.title);

      if (anyEditable) {
        addFinding({
          severity: "S1",
          title: "Primary key column is editable in the grid (" + engine.label + ")",
          repro: "write mode on; open uitest_tab; inspect the 'id' column's td classes",
          expected: "every id cell is td.locked, never td.editable",
          actual: "at least one id cell carries the editable class: " + JSON.stringify(idCells),
          evidence: [evidence.__lastPath || ""],
        });
      } else if (!allLocked) {
        addFinding({
          severity: "S2",
          title: "Primary key column cells carry neither .editable nor .locked (" + engine.label + ")",
          repro: "write mode on; open uitest_tab; inspect the 'id' column's td classes",
          expected: "every id cell is td.locked (an explicit, visibly-muted 'why not', per U-06)",
          actual: JSON.stringify(idCells),
          evidence: [evidence.__lastPath || ""],
        });
      }
      if (allLocked && anyMissingTitle) {
        addFinding({
          severity: "S3",
          title: "Locked PK cell has no explanatory title (" + engine.label + ")",
          repro: "inspect td.locked title attribute on the 'id' column",
          expected: 'title explains why, e.g. "primary key" (per spec-dataconsole.md §7.4)',
          actual: JSON.stringify(idCells),
          evidence: [evidence.__lastPath || ""],
        });
      }

      // Attempt a click anyway -- a locked cell should be a pure no-op (no onclick wired).
      await spa.click(cellSelector(1, idCol + 1));
      await sleep(400);
      const gotInput = await cellHasInput(spa, 0, idCol);
      await evidence(engine.id + "-02-after-click-attempt");
      if (gotInput) {
        addFinding({
          severity: "S1",
          title: "Clicking a locked PK cell opened an editor anyway (" + engine.label + ")",
          repro: "click the 'id' column cell of the first row",
          expected: "no input.celledit appears",
          actual: "input.celledit appeared",
          evidence: [evidence.__lastPath || ""],
        });
      }
    } finally {
      dropTab(engines, engine);
    }
  }
}

// ============================================================
// TAB-6 — insert row: valid insert (engine confirms + key echoed), missing
// required field (honest rejection, engine unchanged), invalid type
// (honest rejection, engine unchanged).
// ============================================================
async function runTab6(ctx) {
  const { page, harness, engines, addFinding } = ctx;
  const evidence = trackEvidence(ctx.evidence);
  for (const engine of ENGINES) {
    dropTab(engines, engine);
    try {
      const spa = await openTabWritable(ctx, engine);
      if (!spa) continue;

      const boolTrue = engine.id === "pg" ? "true" : "1";

      async function openInsertForm() {
        await spa.waitForSelector("#insertrow", { timeout: 10000 });
        await spa.click("#insertrow");
        await spa.waitForSelector(".insertform", { timeout: 10000 });
      }
      async function fillForm(vals) {
        await spa.evaluate((v) => {
          Object.keys(v).forEach((col) => {
            const el = document.querySelector('.insertform input[data-col="' + col + '"]');
            if (el) el.value = v[col];
          });
        }, vals);
      }
      async function submitAndWaitToast() {
        await drainToast(spa); // guarantee THIS submit's toast isn't a stale read of a prior insert's (gotcha #8)
        await spa.click("#modalok");
        const t = await harness.waitToast(spa);
        await sleep(200);
        return t;
      }
      // Round-2 modal contract (spec §7.4): a REJECTED submit keeps the modal
      // open with the typed input and renders the error inline (#modalerr) —
      // no toast, no auto-close. Returns the observed state and closes the
      // modal so the next sub-test starts clean.
      async function submitExpectInlineError() {
        await spa.click("#modalok");
        let err = "";
        try {
          await spa.waitForSelector("#modalerr", { timeout: 8000 });
          err = await spa.evaluate(() => (document.getElementById("modalerr") || {}).textContent || "");
        } catch (_) { /* absent — caller records */ }
        const open = await spa.evaluate(() => !document.getElementById("modal").classList.contains("hidden"));
        const txtKept = await spa.evaluate(() => {
          const el = document.querySelector('.insertform input[data-col="num"]');
          return el ? el.value : null;
        });
        await spa.click("#modalcancel");
        await spa.waitForSelector("#modal.hidden", { timeout: 5000 }).catch(() => {});
        await sleep(200);
        return { err, open, numKept: txtKept };
      }

      // ---- valid insert ----
      const marker = "uitest_ins_" + engine.id;
      await openInsertForm();
      await evidence(engine.id + "-01-insert-form-empty");
      await fillForm({ txt: marker, num: "42.50", flag: boolTrue, ts: "2027-06-15 09:00:00", js: '{"inserted":true}' });
      await evidence(engine.id + "-02-insert-form-filled");
      const tValid = await submitAndWaitToast();
      await evidence(engine.id + "-03-after-valid-insert");
      const foundCount = engineCount(engines, engine, "uitest_tab WHERE txt='" + marker + "'");
      if (!tValid || tValid.kind !== "good" || foundCount !== 1) {
        addFinding({
          severity: "S1",
          title: "Valid row insert did not apply (" + engine.label + ")",
          repro: "Insert row; txt=" + marker + ", num=42.50, flag=" + boolTrue + ", ts=2027-06-15 09:00:00, js={\"inserted\":true}",
          expected: "good toast; exactly 1 matching row in the engine",
          actual: "toast=" + JSON.stringify(tValid) + "; engine rows matching=" + foundCount,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "COUNT(*) WHERE txt='" + marker + "' = " + foundCount,
        });
      }
      const gridAfterInsert = await readGrid(spa);
      const visibleInGrid = gridAfterInsert.rows.some((r) => r.some((c) => c.text === marker));
      if (!visibleInGrid) {
        addFinding({
          severity: "S2",
          title: "Inserted row confirmed by the engine but not visible in the re-rendered grid (" + engine.label + ")",
          repro: "insert a row with txt=" + marker + "; inspect the grid immediately after (openTable re-read)",
          expected: "new row visible without further action",
          actual: "not found in rendered grid rows",
          evidence: [evidence.__lastPath || ""],
          engine_truth: "COUNT(*) WHERE txt='" + marker + "' = " + foundCount,
        });
      }

      // ---- missing required field (txt NOT NULL, left blank) ----
      const beforeMissingCount = engineCount(engines, engine, "uitest_tab");
      await openInsertForm();
      await fillForm({ txt: "", num: "1.00", flag: boolTrue, ts: "2027-06-15 09:00:00", js: '{"x":1}' });
      await evidence(engine.id + "-04-insert-missing-required-filled");
      const rMissing = await submitExpectInlineError();
      await evidence(engine.id + "-05-after-missing-required-submit");
      const afterMissingCount = engineCount(engines, engine, "uitest_tab");
      if (afterMissingCount !== beforeMissingCount) {
        addFinding({
          severity: "S1",
          title: "Insert with a blank NOT NULL field changed the row count (" + engine.label + ")",
          repro: "Insert row; leave 'txt' blank (NOT NULL column), fill the rest",
          expected: "honest rejection; row count unchanged (" + beforeMissingCount + ")",
          actual: "inline err=" + JSON.stringify(rMissing.err) + "; row count " + beforeMissingCount + " -> " + afterMissingCount,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "COUNT(*) = " + afterMissingCount,
        });
      } else if (!rMissing.open || !rMissing.err) {
        addFinding({
          severity: "S2",
          title: "Insert rejection did not keep the modal open with an inline error (round-2 modal contract; " + engine.label + ")",
          repro: "Insert row; leave 'txt' blank; submit",
          expected: "modal stays open with #modalerr and the typed values intact",
          actual: "open=" + rMissing.open + "; err=" + JSON.stringify(rMissing.err),
          evidence: [evidence.__lastPath || ""],
        });
      }

      // ---- invalid type (num) ----
      const badMarker = "uitest_badnum_" + engine.id;
      await openInsertForm();
      await fillForm({ txt: badMarker, num: "not-a-number", flag: boolTrue, ts: "2027-06-15 09:00:00", js: '{"x":1}' });
      await evidence(engine.id + "-06-insert-invalid-type-filled");
      const rBadType = await submitExpectInlineError();
      await evidence(engine.id + "-07-after-invalid-type-submit");
      const badTypeCount = engineCount(engines, engine, "uitest_tab WHERE txt='" + badMarker + "'");
      if (badTypeCount !== 0) {
        addFinding({
          severity: "S1",
          title: "Insert with a non-numeric value in a numeric column was accepted (" + engine.label + ")",
          repro: "Insert row; txt=" + badMarker + ", num='not-a-number'",
          expected: "honest rejection; no row created",
          actual: "inline err=" + JSON.stringify(rBadType.err) + "; rows matching=" + badTypeCount,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "COUNT(*) WHERE txt='" + badMarker + "' = " + badTypeCount,
        });
      } else if (!rBadType.open || !rBadType.err) {
        addFinding({
          severity: "S2",
          title: "Invalid-type insert rejection did not keep the modal open with an inline error (round-2 modal contract; " + engine.label + ")",
          repro: "Insert row; num='not-a-number'; submit",
          expected: "modal stays open with #modalerr",
          actual: "open=" + rBadType.open + "; err=" + JSON.stringify(rBadType.err),
          evidence: [evidence.__lastPath || ""],
        });
      }
    } finally {
      dropTab(engines, engine);
    }
  }
}

// ============================================================
// TAB-7 — delete row: confirm modal (danger styling), commit -> engine
// confirms gone; cancel path -> row stays. Watches for KI-1-shaped rejection.
// ============================================================
async function runTab7(ctx) {
  const { harness, engines, addFinding } = ctx;
  const evidence = trackEvidence(ctx.evidence);
  for (const engine of ENGINES) {
    dropTab(engines, engine);
    try {
      const spa = await openTabWritable(ctx, engine);
      if (!spa) continue;

      let grid = await readGrid(spa);
      const txtCol = colIdxOf(grid.headers, "txt");
      const targetRow = rowIdxByCellText(grid.rows, txtCol, "row4");
      const beforeCount = engineCount(engines, engine, "uitest_tab");

      // ---- delete row4 ----
      const delSel = "table.grid tbody.gridbody tr:nth-child(" + (targetRow + 1) + ") button.rowdel";
      await spa.waitForSelector(delSel, { timeout: 10000 });
      await spa.click(delSel);
      await spa.waitForSelector("#modal:not(.hidden)", { timeout: 10000 });
      await evidence(engine.id + "-01-delete-confirm-modal"); // #modalok carries class="danger" statically (index.html)
      await spa.click("#modalok");
      const toast = await harness.waitToast(spa);
      await sleep(400);
      await evidence(engine.id + "-02-after-delete");

      const afterCount = engineCount(engines, engine, "uitest_tab");
      const stillThere = engineCount(engines, engine, "uitest_tab WHERE txt='row4'") > 0;
      const toastGood = !!toast && toast.kind === "good";
      const looksLikeKI1 = !!toast && toast.kind === "bad" && /invalid request/i.test(toast.text || "");

      if (looksLikeKI1) {
        addFinding({
          severity: "S1",
          title: 'Row delete rejected with "Invalid request" -- same shape as KI-1 (' + engine.label + ", scope: /api/row DELETE)",
          repro: "write mode on; open uitest_tab; click button.rowdel for the 'row4' row; confirm #modalok",
          expected: "200 {ok:true}; row removed from grid and engine",
          actual: "toast=" + JSON.stringify(toast) + "; engine row count " + beforeCount + " -> " + afterCount +
            "; references CORE-1-S1-delete-rejected (KI-1) -- that finding was scoped to a valkey blob delete via " +
            "/api/node DELETE; this is the SAME symptom on the tabular /api/row DELETE path for " + engine.label,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "COUNT(*) = " + afterCount + "; row4 still present=" + stillThere,
        });
      } else if (!toastGood || stillThere) {
        addFinding({
          severity: "S1",
          title: "Row delete did not apply (" + engine.label + ")",
          repro: "click button.rowdel for 'row4'; confirm #modalok",
          expected: "good toast; row4 gone from engine",
          actual: "toast=" + JSON.stringify(toast) + "; row4 still present=" + stillThere + "; count " + beforeCount +
            " -> " + afterCount,
          evidence: [evidence.__lastPath || ""],
          engine_truth: "COUNT(*) = " + afterCount,
        });
      } else {
        const gridAfterDel = await readGrid(spa);
        const staleInUI = gridAfterDel.rows.some((r) => r[txtCol] && r[txtCol].text === "row4");
        if (staleInUI) {
          addFinding({
            severity: "S2",
            title: "Deleted row still rendered in the grid despite a genuinely successful delete (" + engine.label + ")",
            repro: "delete 'row4'; toast said success; engine confirms gone; re-check the grid",
            expected: "row4 absent from the re-rendered grid",
            actual: "row4 still present in tbody",
            evidence: [evidence.__lastPath || ""],
            engine_truth: "COUNT(*) = " + afterCount,
          });
        }
      }

      // ---- cancel path: delete 'plain', but click Cancel ----
      grid = await readGrid(spa);
      const plainRow = rowIdxByCellText(grid.rows, txtCol, "plain");
      if (plainRow >= 0) {
        const cancelSel = "table.grid tbody.gridbody tr:nth-child(" + (plainRow + 1) + ") button.rowdel";
        const preCancelCount = engineCount(engines, engine, "uitest_tab");
        await spa.click(cancelSel);
        await spa.waitForSelector("#modal:not(.hidden)", { timeout: 10000 });
        await evidence(engine.id + "-03-cancel-modal-open");
        await spa.click("#modalcancel");
        await sleep(400);
        await evidence(engine.id + "-04-after-cancel");
        const postCancelCount = engineCount(engines, engine, "uitest_tab");
        const modalStillOpen = await spa.evaluate(() => !document.getElementById("modal").classList.contains("hidden"));
        if (postCancelCount !== preCancelCount || modalStillOpen) {
          addFinding({
            severity: "S1",
            title: "Clicking Cancel on the delete-row confirm modal did not cleanly abort (" + engine.label + ")",
            repro: "click button.rowdel for 'plain'; click #modalcancel instead of #modalok",
            expected: "modal closes; no request sent; row count unchanged",
            actual: "row count " + preCancelCount + " -> " + postCancelCount + "; modal still open=" + modalStillOpen,
            evidence: [evidence.__lastPath || ""],
            engine_truth: "COUNT(*) = " + postCancelCount,
          });
        }
      }
    } finally {
      dropTab(engines, engine);
    }
  }
}

// ============================================================
// TAB-8 — query (db only): SELECT renders via the grid with query-result
// posture; syntax-error query is an honest inline error, no crash.
// ============================================================
async function runTab8(ctx) {
  const { page, harness, engines, addFinding } = ctx;
  const evidence = trackEvidence(ctx.evidence);
  const engine = PG;
  dropTab(engines, engine);
  try {
    seedTab(engines, engine);
    await sleep(150);
    const spa = await harness.openConsole(page, engine.service);
    // Write mode ON on purpose: canWrite for a query result is hard-false
    // (renderGrid's opts.node is null for /api/query), so this specifically
    // tests whether that's rendered as an HONEST .locked state (per the code's
    // own U-01/U-06 comments) or silently indistinguishable from a plain cell.
    await harness.setWriteMode(page, spa, true);
    await harness.clickService(spa, engine.service);
    await sleep(400);

    const hint = await spa.evaluate(() => ({
      hasQueryLink: !!document.getElementById("querylink"),
      text: (document.querySelector(".placeholder") || {}).textContent || null,
    }));
    await evidence("01-placeholder-hint");
    if (!hint.hasQueryLink) {
      addFinding({
        severity: "S1",
        title: "No 'Run a query' affordance offered for postgres",
        repro: "selectService('db'); inspect the placeholder hint",
        expected: "#querylink present (querySQL is a full-tier tabular action per spec-dataconsole.md §7.5)",
        actual: JSON.stringify(hint),
        evidence: [evidence.__lastPath || ""],
      });
      return;
    }
    await spa.click("#querylink");
    await spa.waitForSelector("#qtext", { timeout: 10000 });
    await evidence("02-query-pane-open");

    await spa.evaluate(() => { document.getElementById("qtext").value = "SELECT * FROM uitest_tab ORDER BY id"; });
    await spa.click("#runq");
    await waitForGrid(spa);
    await sleep(300);
    const grid = await readGrid(spa);
    await evidence("03-query-results");

    const engineRows = engineCount(engines, engine, "uitest_tab");
    if (grid.rows.length !== engineRows) {
      addFinding({
        severity: "S1",
        title: "Query result row count does not match the engine",
        repro: "SELECT * FROM uitest_tab ORDER BY id",
        expected: String(engineRows) + " rows",
        actual: grid.rows.length + " rows",
        evidence: [evidence.__lastPath || ""],
        engine_truth: "COUNT(*) = " + engineRows,
      });
    }
    const anyEditable = grid.rows.some((r) => r.some((c) => c.editable));
    const anyDeleteBtn = grid.hasDelCol || grid.rowDelButtons > 0;
    if (anyEditable || anyDeleteBtn || grid.hasInsertBtn) {
      addFinding({
        severity: "S1",
        title: "Query result grid exposes write affordances",
        repro: "run a SELECT with write mode on; inspect grid for .editable cells / delete column / Insert row button",
        expected: "none of these -- query results are read-only per spec-dataconsole.md §7.5 (engine-enforced)",
        actual: "anyEditable=" + anyEditable + " anyDeleteBtn=" + anyDeleteBtn + " hasInsertBtn=" + grid.hasInsertBtn,
        evidence: [evidence.__lastPath || ""],
      });
    }
    const anyLocked = grid.rows.some((r) => r.some((c) => c.locked));
    if (!anyLocked) {
      addFinding({
        severity: "S3",
        title: "Query result cells are functionally read-only but carry no visible .locked affordance, even with write mode on",
        repro: "run a SELECT with write mode ON; inspect td classes in the result grid",
        expected: "per app.js's own comments (U-01: \"query columns arrive editable:false ... explicitly read-only\"; " +
          "U-06: \"never a silent no-op that looks identical to an editable one\"), a distinct locked/muted rendering " +
          "was intended",
        actual: "cells carry neither .editable nor .locked -- renderGrid's canWrite is hard-false when opts.node is " +
          "null (query results never pass a node), so appendGridRows's `editEnabled && c && !c.editable` locked-branch " +
          "is unreachable for query cells regardless of session write mode; the result LOOKS like a plain unstyled " +
          "table rather than a visibly read-only one",
        evidence: [evidence.__lastPath || ""],
      });
    }
    if (!grid.noKeyBadgeText && !/read-only/i.test(String((await spa.evaluate(() =>
        (document.querySelector(".toolbar .meta.note") || {}).textContent || ""))))) {
      addFinding({
        severity: "S3",
        title: "Query result toolbar does not label itself read-only",
        repro: "run a SELECT; inspect .toolbar .meta.note",
        expected: 'a "read-only (query result)" note (per app.js openQuery/runQuery)',
        actual: "no matching label found",
        evidence: [evidence.__lastPath || ""],
      });
    }

    // ---- syntax error ----
    await spa.evaluate(() => { document.getElementById("qtext").value = "SELEKT * FROM uitest_tab"; });
    await spa.click("#runq");
    await sleep(800);
    await evidence("04-syntax-error");
    const errState = await spa.evaluate(() => ({
      hasErr: !!document.querySelector("#qresult .err"),
      crashed: !document.getElementById("qresult"),
      text: (document.querySelector("#qresult .err-msg") || {}).textContent || null,
    }));
    if (errState.crashed || !errState.hasErr) {
      addFinding({
        severity: "S1",
        title: "Syntax-error query does not render an honest inline error",
        repro: "run 'SELEKT * FROM uitest_tab'",
        expected: "#qresult .err renders with a message, no crash",
        actual: JSON.stringify(errState),
        evidence: [evidence.__lastPath || ""],
      });
    }
  } finally {
    dropTab(engines, engine);
  }
}

// ============================================================
// TAB-9 — clickhouse view-only honesty: zero write affordances anywhere, even
// with session write mode ON (proves it's server-declared, not client-gated).
// ============================================================
async function runTab9(ctx) {
  const { page, harness, engines, addFinding } = ctx;
  const evidence = trackEvidence(ctx.evidence);
  const service = "ch";
  dropCh(engines);
  try {
    seedCh(engines);
    await sleep(150);
    const spa = await harness.openConsole(page, service);
    await harness.setWriteMode(page, spa, true); // ON on purpose -- see header comment
    await sleep(300);

    const railInfo = await spa.evaluate((svc) => {
      const items = Array.from(document.querySelectorAll("#services li"));
      const li = items.find((el) => { const span = el.querySelector("span"); return span && span.textContent === svc; });
      if (!li) return null;
      const badge = li.querySelector(".badge");
      return { disabled: li.classList.contains("disabled"), badgeText: badge ? badge.textContent : null };
    }, service);
    await evidence("01-rail-badge");
    if (!railInfo || !/view/i.test(String(railInfo.badgeText))) {
      addFinding({
        severity: "S2",
        title: "Clickhouse rail entry not labelled view-only",
        repro: "inspect #services li badge for 'ch'",
        expected: 'badge text indicates "view" (per spec-dataconsole.md §6/§7.5: clickhouse is a view-only tier)',
        actual: JSON.stringify(railInfo),
        evidence: [evidence.__lastPath || ""],
      });
    }

    await harness.clickService(spa, service);
    await sleep(300);
    const hint = await spa.evaluate(() => ({
      hasCreateKeyLink: !!document.getElementById("createkeylink"),
      hasQueryLink: !!document.getElementById("querylink"),
      text: (document.querySelector(".placeholder") || {}).textContent || null,
    }));
    await evidence("02-placeholder-hint");
    if (hint.hasCreateKeyLink) {
      addFinding({
        severity: "S1",
        title: "Clickhouse offers a create-key/create-row affordance despite being view-only",
        repro: "write mode ON; selectService('ch'); inspect placeholder hint",
        expected: "no #createkeylink",
        actual: JSON.stringify(hint),
        evidence: [evidence.__lastPath || ""],
      });
    }

    const ok = await revealAndClickNode(spa, "uitest_ch");
    if (!ok) {
      addFinding({
        severity: "S1",
        title: "uitest_ch not revealable in the tree",
        repro: "openConsole('ch'); look for uitest_ch",
        expected: "table reachable in the tree",
        actual: "revealAndClickNode gave up; tree names: " + JSON.stringify(await treeNodeNames(spa)),
        evidence: [await evidence("03-tree-reveal-failed")],
      });
      return;
    }
    await waitForGrid(spa);
    await sleep(250);
    const grid = await readGrid(spa);
    await evidence("04-ch-grid");

    const engineRows = 3; // seedCh() inserts exactly 3 rows
    const sweep = {
      anyEditable: grid.rows.some((r) => r.some((c) => c.editable)), // real cell edit -- gated purely by write mode, no server signal to check
      anyDeleteBtn: grid.hasDelCol || grid.rowDelButtons > 0,
      anyDeleteBtnEnabled: grid.rowDelAnyEnabled,
      hasInsertBtn: grid.hasInsertBtn,
      insertBtnEnabled: grid.insertBtnEnabled,
    };
    // anyEditable would be a real functional violation regardless (individual
    // cell editing has no server-declared enabled/disabled tri-state to fall
    // back to honestly -- it's binary). Insert/delete DO have that tri-state,
    // so split "rendered and actually clickable" (S1, a real hole) from
    // "rendered but disabled=true" (S2, an affordance-honesty/visual-
    // consistency violation -- inert, but the TAB-9 brief explicitly named
    // this failure mode: "ABSENT, not disabled-dead").
    const activelyExploitable = sweep.anyEditable || sweep.anyDeleteBtnEnabled || sweep.insertBtnEnabled;
    const renderedButInert = !activelyExploitable && (sweep.anyDeleteBtn || sweep.hasInsertBtn);
    if (activelyExploitable) {
      addFinding({
        severity: "S1",
        title: "Clickhouse table grid exposes an ACTUALLY CLICKABLE write affordance despite being view-only",
        repro: "write mode ON; open uitest_ch; inspect grid for .editable cells / an enabled delete button / an enabled Insert row button",
        expected: "none present -- absent, not merely disabled (per spec-dataconsole.md §7.5)",
        actual: JSON.stringify(sweep),
        evidence: [evidence.__lastPath || ""],
      });
    } else if (renderedButInert) {
      addFinding({
        severity: "S2",
        title: "Clickhouse grid renders Insert/delete affordances as disabled instead of omitting them (view-only family)",
        repro: "write mode ON; open uitest_ch; inspect the Insert row button and per-row delete buttons",
        expected: 'per the TAB-9 brief and the state canon\'s read-only-posture promise (spec-dataconsole.md §7.4): ' +
          "ABSENT for a view-only family, not present-but-disabled -- the server's service.actions apparently still " +
          "includes these actions (just enabled:false), so the SPA's hasAction()-only gate (showDelete/insertRow " +
          "button-render checks) renders them anyway, just inert",
        actual: JSON.stringify(sweep) + "; insertBtnTitle=" + JSON.stringify(grid.insertBtnTitle) +
          "; rowDelTitles=" + JSON.stringify(grid.rowDelTitles),
        evidence: [evidence.__lastPath || ""],
      });
    }
    if (grid.rows.length !== engineRows) {
      addFinding({
        severity: "S2",
        title: "Clickhouse grid row count does not match the engine",
        repro: "SELECT count() FROM uitest_ch",
        expected: String(engineRows) + " rows",
        actual: grid.rows.length + " rows",
        evidence: [evidence.__lastPath || ""],
        engine_truth: "count() = " + engineRows,
      });
    }

    // Query pane, if offered, should also be read-only (conditional per brief -- not a finding if simply absent).
    const hasQuery = await spa.evaluate(() => !!document.getElementById("querylink"));
    if (hasQuery) {
      await spa.click("#querylink");
      await spa.waitForSelector("#qtext", { timeout: 10000 });
      await spa.evaluate(() => { document.getElementById("qtext").value = "SELECT * FROM uitest_ch"; });
      await spa.click("#runq");
      await sleep(800);
      await evidence("05-ch-query-pane");
      const qgrid = await readGrid(spa);
      const qWrite = qgrid && (qgrid.rows.some((r) => r.some((c) => c.editable)) || qgrid.hasDelCol || qgrid.hasInsertBtn);
      if (qWrite) {
        addFinding({
          severity: "S1",
          title: "Clickhouse query pane exposes write affordances",
          repro: "open ch query pane; SELECT * FROM uitest_ch",
          expected: "read-only results only",
          actual: JSON.stringify(qgrid),
          evidence: [evidence.__lastPath || ""],
        });
      }
    }
  } finally {
    dropCh(engines);
  }
}

// ============================================================
// TAB-10 — weird data: long text (500 chars, DOM has it all, no title
// tooltip), unicode fidelity, and horizontal scroll containment inside
// .gridwrap (never the page).
// ============================================================
async function runTab10(ctx) {
  const { engines, addFinding } = ctx;
  const evidence = trackEvidence(ctx.evidence);
  for (const engine of ENGINES) {
    dropTab(engines, engine);
    try {
      seedTab(engines, engine);
      await sleep(150);
      const spa = await openNode(ctx, engine, "uitest_tab", false);
      if (!spa) continue;

      const grid = await readGrid(spa);
      const txtCol = colIdxOf(grid.headers, "txt");
      const longRow = rowIdxByCellText(grid.rows, txtCol, LONG_TEXT);
      const unicodeRow = rowIdxByCellText(grid.rows, txtCol, UNICODE_TEXT);
      await evidence(engine.id + "-01-grid");

      if (longRow < 0) {
        addFinding({
          severity: "S2",
          title: "Long (500-char) text value not found verbatim in the rendered grid (" + engine.label + ")",
          repro: "seed a row with a 500-char txt value; read td.textContent for that row's txt cell",
          expected: "full 500 characters present in the DOM (CSS may visually ellipsize, but the text node is complete)",
          actual: "no row's txt cell matched the full 500-char string",
          evidence: [evidence.__lastPath || ""],
        });
      } else {
        const cell = grid.rows[longRow][txtCol];
        if (cell.text.length !== LONG_TEXT.length) {
          addFinding({
            severity: "S1",
            title: "Long text value truncated in the DOM, not just visually ellipsized (" + engine.label + ")",
            repro: "inspect td.textContent.length for the 500-char txt cell",
            expected: "500",
            actual: String(cell.text.length),
            evidence: [evidence.__lastPath || ""],
          });
        }
        if (cell.title) {
          // A title tooltip would be a nice-to-have escape hatch; note if present, not a finding either way.
        } else {
          addFinding({
            severity: "S3",
            title: "No hover tooltip (title attribute) on a CSS-ellipsized long-text cell (" + engine.label + ")",
            repro: "hover the 500-char txt cell in a read-only session (write mode off, or a view-only-tier service)",
            expected: "some way to see the full value without entering edit mode",
            actual: "td carries no title attribute; the only escape hatch is clicking into edit mode, which requires " +
              "write access -- a read-only viewer (standalone SPA, or a view-only-tier service like clickhouse) has " +
              "no way at all to read the full value of an overflowed cell",
            evidence: [evidence.__lastPath || ""],
          });
        }
      }

      if (unicodeRow < 0) {
        addFinding({
          severity: "S1",
          title: "Unicode text value not found verbatim in the rendered grid (" + engine.label + ")",
          repro: "seed a row with txt='" + UNICODE_TEXT + "'; search rendered grid rows for an exact match",
          expected: "exact match present",
          actual: "no row's txt cell matched exactly; closest candidates: " +
            JSON.stringify(grid.rows.map((r) => r[txtCol] && r[txtCol].text).filter((t) => t && /\S/.test(t))),
          evidence: [evidence.__lastPath || ""],
        });
      }

      const scroll = await spa.evaluate(() => {
        const doc = document.scrollingElement;
        const wrap = document.querySelector(".gridwrap");
        return {
          pageScrollWidth: doc ? doc.scrollWidth : null,
          pageClientWidth: doc ? doc.clientWidth : null,
          wrapScrollWidth: wrap ? wrap.scrollWidth : null,
          wrapClientWidth: wrap ? wrap.clientWidth : null,
        };
      });
      await evidence(engine.id + "-02-scroll-metrics");
      if (scroll.pageScrollWidth != null && scroll.pageClientWidth != null && scroll.pageScrollWidth > scroll.pageClientWidth + 1) {
        addFinding({
          severity: "S2",
          title: "Page-level horizontal scroll appears for a wide table (" + engine.label + ")",
          repro: "open uitest_tab (7 columns) in a 1440px-wide viewport; read document.scrollingElement.scrollWidth/clientWidth",
          expected: "scrollWidth <= clientWidth -- overflow must stay inside .gridwrap",
          actual: JSON.stringify(scroll),
          evidence: [evidence.__lastPath || ""],
        });
      }
      if (scroll.wrapScrollWidth != null && scroll.wrapClientWidth != null && scroll.wrapScrollWidth <= scroll.wrapClientWidth) {
        // The grid didn't actually need to scroll at this viewport width -- note only, the "no page scroll" assertion
        // above is then not a meaningful proof of containment (nothing overflowed to contain).
      }
    } finally {
      dropTab(engines, engine);
    }
  }
}

runner.register({ id: "TAB-1", family: "tabular", fn: runTab1 });
runner.register({ id: "TAB-2", family: "tabular", fn: runTab2 });
runner.register({ id: "TAB-3", family: "tabular", fn: runTab3 });
runner.register({ id: "TAB-4", family: "tabular", fn: runTab4 });
runner.register({ id: "TAB-5", family: "tabular", fn: runTab5 });
runner.register({ id: "TAB-6", family: "tabular", fn: runTab6 });
runner.register({ id: "TAB-7", family: "tabular", fn: runTab7 });
runner.register({ id: "TAB-8", family: "tabular", fn: runTab8 });
runner.register({ id: "TAB-9", family: "tabular", fn: runTab9 });
runner.register({ id: "TAB-10", family: "tabular", fn: runTab10 });
