"use strict";

// CORE-1 — write-mode lifecycle across a same-panel service switch.
//
// Regression under test: a sidebar re-browse (clicking "Browse data" on a
// DIFFERENT service while the console panel is already open) rebinds the host
// broker to a fresh console session. consolePanel.js carries the prior broker's
// write-enabled flag onto the new one and re-syncs the webview — but that's a
// unit-tested CLAIM (consolepanel.test.js::testWriteModePreservedAcrossReveal);
// this scenario proves it holds through the REAL nested-webview postMessage
// bridge, not a fake one. If it ever regresses, the SPA's toggle would stay
// visually green while the broker silently reverted to read-only, so every
// mutation after a re-browse would 403 with no visible cause — a bug a human
// would experience as "writes randomly stopped working."
//
// G1 verdict rule (per the harness brief): a legitimately failing assertion
// below is recorded via addFinding and does NOT fail this scenario's run status
// — only a thrown error (the harness couldn't drive the UI at all) does that.

const runner = require("../lib/runner");
const { loadConfig } = require("../lib/config");

const KEY = "uitest_core_tmp";
const FIXTURE_KEY = "greeting"; // seeded; read-only touch, NEVER mutated
const DB_SERVICE = "db";
const CACHE_SERVICE = "cache";

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function teardown(engines) {
  try {
    engines.redis(["DEL", KEY]);
  } catch (_) {
    /* best effort */
  }
  try {
    const out = engines.redis(["KEYS", "uitest_*"]);
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

async function readSwitchState(frame) {
  return frame.evaluate(() => {
    const el = document.getElementById("editswitch");
    return {
      exists: !!el,
      hidden: el ? el.classList.contains("hidden") : null,
      on: el ? el.classList.contains("on") : null,
    };
  });
}

async function activeServiceLabel(frame) {
  return frame.evaluate(() => {
    const li = document.querySelector("#services li.active span");
    return li ? li.textContent : null;
  });
}

async function findTreeLeaf(frame, name) {
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

async function treeHasLeaf(frame, name) {
  return frame.evaluate((n) => {
    const names = Array.from(document.querySelectorAll("#tree .node .nname"));
    return names.some((el) => el.textContent === n);
  }, name);
}

async function run(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;

  teardown(engines); // clean slate first -- self-heal from a prior aborted run
  engines.redis(["SET", KEY, "hello"]);

  try {
    // ---- 1. open db, write toggle must read OFF (self-heal if ambient state says on) ----
    let spa = await harness.openConsole(page, DB_SERVICE);
    await evidence("01-db-opened");

    await harness.setWriteMode(page, spa, false); // self-heal: guarantee OFF regardless of ambient state
    const offState = await readSwitchState(spa);
    await evidence("01b-write-toggle-off");
    if (!offState.exists || offState.hidden || offState.on) {
      addFinding({
        severity: "S1",
        title: "Write toggle not present/visible/OFF at session start even after self-heal",
        repro: "openConsole(page, 'db'); setWriteMode(page, spa, false); read #editswitch",
        expected: "{exists:true, hidden:false, on:false}",
        actual: JSON.stringify(offState),
        evidence: ["evidence/CORE-1/01b-write-toggle-off.png"],
      });
    }

    // ---- 2. enable write mode -- shot the native modal BEFORE confirming ----
    await harness.setWriteMode(page, spa, true);
    await evidence("02-write-mode-on");
    const onState = await readSwitchState(spa);
    if (!onState.on) {
      addFinding({
        severity: "S1",
        title: "Write mode did not turn ON after confirming the native 'Enable writes' modal",
        repro: "setWriteMode(page, spa, true) on db",
        expected: "#editswitch.on == true",
        actual: JSON.stringify(onState),
        evidence: ["evidence/CORE-1/02-write-mode-on.png"],
      });
    }

    // ---- 3. sidebarBrowse to cache -- THE reveal path ----
    const cacheFrame = await harness.sidebarBrowse(page, CACHE_SERVICE);
    try {
      await cacheFrame.waitForFunction(
        (svc) => {
          const li = document.querySelector("#services li.active span");
          return !!li && li.textContent === svc;
        },
        { timeout: 15000 },
        CACHE_SERVICE
      );
    } catch (_) {
      /* fall through -- the assertion below records the mismatch either way */
    }
    // dataconsole-switch-service and dataconsole-write-mode are posted back to
    // back from the SAME source (consolePanel.js show()); waiting for the first
    // to land all but guarantees the second has too, but give it one settle tick.
    await sleep(200);
    await evidence("03-sidebar-rebrowse-cache");

    const afterSwitch = await readSwitchState(cacheFrame);
    const activeLabel = await activeServiceLabel(cacheFrame);
    if (activeLabel !== CACHE_SERVICE) {
      addFinding({
        severity: "S2",
        title: "Sidebar re-browse did not switch the SPA's active rail service",
        repro: 'sidebarBrowse(page, "cache") while db was active',
        expected: 'rail active service == "cache"',
        actual: "active=" + JSON.stringify(activeLabel),
        evidence: ["evidence/CORE-1/03-sidebar-rebrowse-cache.png"],
      });
    }
    if (!afterSwitch.on) {
      // THE regression this scenario exists to catch.
      addFinding({
        id: "CORE-1-S1-writemode-divergence",
        severity: "S1",
        title: "Write mode silently dropped after a sidebar re-browse (broker rebind divergence)",
        repro:
          'db: setWriteMode(page,spa,true) -> confirmed ON (see 02-write-mode-on.png); ' +
          'sidebarBrowse(page,"cache") -> #editswitch.on reads false (see 03-sidebar-rebrowse-cache.png)',
        expected: "#editswitch.on stays true across a same-panel re-browse",
        actual: "#editswitch.on == false; state=" + JSON.stringify(afterSwitch),
        evidence: ["evidence/CORE-1/02-write-mode-on.png", "evidence/CORE-1/03-sidebar-rebrowse-cache.png"],
        engine_truth: "n/a -- UI-state divergence, not a data mutation",
      });
    }

    // Re-probe rather than trust a stale frame handle across the switch.
    spa = await harness.spaFrame(page);

    // ---- 4/5. delete uitest_core_tmp via the cache tree ----
    const openedLeaf = await findTreeLeaf(spa, KEY);
    if (!openedLeaf) {
      addFinding({
        severity: "S1",
        title: 'Seeded key "' + KEY + '" not visible in the cache tree',
        repro: "redis SET " + KEY + " hello; openConsole(cache); look for #tree .node .nname === key",
        expected: "key visible as a tree leaf",
        actual: "not found",
        evidence: [await evidence("04-key-not-in-tree")],
      });
      return; // can't continue the delete/verify chain without the leaf
    }
    await evidence("04-key-opened");

    await spa.waitForSelector("#delblob", { timeout: 15000 });
    await spa.click("#delblob");
    await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
    await evidence("05-spa-delete-modal"); // the SPA's OWN modal, not the native VS Code one
    await spa.click("#modalok");

    const toast = await harness.waitToast(spa);
    await evidence("06-after-delete");

    // ---- 6. O-ENGINE verify -- the actual arbiter, not the toast ----
    const stillInTree = await treeHasLeaf(spa, KEY);
    const exists = engines.redis(["EXISTS", KEY]);
    const engineDeleted = String(exists).trim() === "0";
    const toastGood = !!toast && toast.kind === "good";

    // Branch on what actually happened -- these are DIFFERENT bug classes and
    // must not collapse into one another: a toast that never claimed success
    // is an outright failure (I-2: honest rejection), not a "success lie"
    // (I-1: success lie is specifically success CLAIMED but not applied).
    if (toastGood && !engineDeleted) {
      addFinding({
        id: "CORE-1-S1-success-lie",
        severity: "S1",
        title: "UI reported a successful delete but the key still EXISTS on the engine (success-lie, I-1)",
        repro: "redis EXISTS " + KEY + " immediately after the UI delete + a 'good' toast",
        expected: "EXISTS 0",
        actual: "toast=" + JSON.stringify(toast) + "; EXISTS " + exists,
        evidence: ["evidence/CORE-1/06-after-delete.png"],
        engine_truth: "redis EXISTS " + KEY + " = " + exists,
      });
    } else if (!toastGood) {
      addFinding({
        id: "CORE-1-S1-delete-rejected",
        severity: "S1",
        title: 'Deleting a plain top-level KV string key fails outright via the UI (toast: "' + (toast ? toast.text : "none") + '")',
        repro:
          'redis SET ' + KEY + ' hello (a flat key, no ":" segments); cache: open it; click #delblob; confirm #modalok',
        expected: "200 {ok:true}; toast 'Deleted.'; key removed from #tree and the engine",
        actual: (toast ? JSON.stringify(toast) : "no toast observed within timeout") +
          "; stillInTree=" + stillInTree + "; engine EXISTS=" + exists,
        evidence: ["evidence/CORE-1/05-spa-delete-modal.png", "evidence/CORE-1/06-after-delete.png"],
        engine_truth: "redis EXISTS " + KEY + " = " + exists + " (unchanged -- delete never applied)",
      });
    } else if (stillInTree) {
      // toast genuinely said success AND the engine agrees it's gone, but the
      // tree still renders the stale entry -- a narrower refresh-lag bug.
      addFinding({
        severity: "S2",
        title: "Deleted key still visible in the tree after a genuinely successful delete",
        repro: "delete " + KEY + " via UI; toast said success; engine confirms gone; re-check #tree",
        expected: "key absent from #tree",
        actual: "key still present in #tree despite engine EXISTS=" + exists,
        evidence: ["evidence/CORE-1/06-after-delete.png"],
        engine_truth: "redis EXISTS " + KEY + " = " + exists,
      });
    }

    // ---- 7. disable write mode -- write affordances must disappear ----
    await harness.setWriteMode(page, spa, false);
    await evidence("07-write-mode-off");
    const offAgain = await readSwitchState(spa);
    if (offAgain.on) {
      addFinding({
        severity: "S1",
        title: "Write toggle still ON after disabling write mode",
        repro: "setWriteMode(page, spa, false)",
        expected: "#editswitch.on == false",
        actual: JSON.stringify(offAgain),
        evidence: ["evidence/CORE-1/07-write-mode-off.png"],
      });
    }

    // Force a fresh selectService() render (the top-level hint doesn't
    // reactively re-render on a bare write-mode toggle) and open the read-only
    // fixture key to check its edit/delete affordances are gone too -- read
    // ONLY, never mutated.
    await harness.clickService(spa, CACHE_SERVICE);
    await sleep(150);
    const hintHasAddKey = await spa.evaluate(() => !!document.getElementById("createkeylink"));
    if (hintHasAddKey) {
      addFinding({
        severity: "S2",
        title: '"Add key" affordance still rendered after disabling write mode',
        repro: "setWriteMode(off); clickService(spa,'cache'); look for #createkeylink",
        expected: "#createkeylink absent",
        actual: "#createkeylink present",
        evidence: [await evidence("07c-addkey-hint-still-present")],
      });
    }

    const openedFixture = await findTreeLeaf(spa, FIXTURE_KEY);
    if (openedFixture) {
      await sleep(150);
      const affordances = await spa.evaluate(() => ({
        hasSave: !!document.getElementById("saveblob"),
        hasDelete: !!document.getElementById("delblob"),
        hasRename: !!document.getElementById("renameblob"),
      }));
      await evidence("07d-fixture-readonly-view");
      if (affordances.hasSave || affordances.hasDelete || affordances.hasRename) {
        addFinding({
          severity: "S1",
          title: "Edit/delete/rename affordances still rendered on a blob view after disabling write mode",
          repro: "setWriteMode(off); open '" + FIXTURE_KEY + "'; look for #saveblob/#delblob/#renameblob",
          expected: "all three absent",
          actual: JSON.stringify(affordances),
          evidence: ["evidence/CORE-1/07d-fixture-readonly-view.png"],
        });
      }
    }
  } finally {
    // ---- 8. teardown ----
    teardown(engines);
  }
}

// ============================================================
// shared local helpers for CORE-2/4/5/6/7 (self-contained per the fan-out
// convention -- each scenario file keeps its own small helper set rather than
// cross-importing; see tabular.js/kv-object.js/document.js for the same
// pattern).
// ============================================================

// core2Reveal expands tree containers breadth-first until a node named `name`
// is visible, then clicks it. A trimmed local copy of tabular.js's
// revealAndClickNode (deliberately never falls back to clicking #refresh --
// see that file's comment on the refresh-race it would otherwise trigger).
async function revealAndClick(frame, name, tries) {
  tries = tries || 20;
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

// gridSummary reads just enough of the currently-open table.grid to answer
// "does any mutation affordance render" without pulling in tabular.js's full
// readGrid() shape.
async function gridSummary(frame) {
  return frame.evaluate(() => {
    const table = document.querySelector("table.grid");
    if (!table) return null;
    return {
      editableCells: table.querySelectorAll("td.editable").length,
      rowDelButtons: table.querySelectorAll("button.rowdel").length,
      hasInsertBtn: !!document.getElementById("insertrow"),
      rows: table.querySelectorAll("tbody.gridbody tr").length,
      hasEmptyCell: !!table.querySelector("td.state.empty"),
    };
  });
}

// setInputLocal clears an input via the DOM select() API and types `value` --
// a trimmed copy of kv-object.js's setInput.
async function setInputLocal(frame, sel, value) {
  await frame.evaluate((s) => {
    const el = document.querySelector(s);
    if (el) {
      el.focus();
      if (typeof el.select === "function") el.select();
    }
  }, sel);
  if (value) await frame.type(sel, value);
}

// treeDupeInfo reads every #tree .node .nname (any depth) and reports
// duplicate names -- the KI-2 tree-generation-guard regression signature.
async function treeDupeInfo(frame) {
  return frame.evaluate(() => {
    const names = Array.from(document.querySelectorAll("#tree .node .nname")).map((el) => el.textContent);
    const counts = {};
    for (const n of names) counts[n] = (counts[n] || 0) + 1;
    return { names, dupes: Object.entries(counts).filter(([, c]) => c > 1) };
  });
}

// ============================================================
// CORE-2 — write-off honesty (db + cache): with write mode OFF, ZERO mutation
// affordances render anywhere -- no editable cells, no row-delete buttons, no
// Insert row, no upload bar, no Add key, no blob toolbar write buttons, no TTL
// set/persist buttons. Read-only browsing (grid + blob) still works.
// ============================================================

const CORE2_TAB = "uitest_core2_tab";
const CORE2_STR = "uitest_core2_str";
const CORE2_HASH = "uitest_core2_hash";

function core2Teardown(engines) {
  try {
    engines.psql("DROP TABLE IF EXISTS " + CORE2_TAB);
  } catch (_) {
    /* best effort */
  }
  try {
    engines.redis(["DEL", CORE2_STR, CORE2_HASH]);
  } catch (_) {
    /* best effort */
  }
}

async function runCore2(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  core2Teardown(engines);
  try {
    // ---- db (postgres), write mode OFF ----
    engines.psql("CREATE TABLE " + CORE2_TAB + " (id serial PRIMARY KEY, txt text)");
    engines.psql("INSERT INTO " + CORE2_TAB + " (txt) VALUES ('a'), ('b')");

    let spa = await harness.openConsole(page, DB_SERVICE);
    await harness.setWriteMode(page, spa, false);
    await harness.clickService(spa, DB_SERVICE); // force a fresh selectService()/loadTree()
    await sleep(400);
    const opened = await revealAndClick(spa, CORE2_TAB);
    if (!opened) {
      addFinding({
        severity: "S1",
        title: "CORE-2: could not reveal " + CORE2_TAB + " in the db tree (write mode off)",
        repro: "openConsole('db'); write mode off; reveal " + CORE2_TAB,
        expected: "table reachable",
        actual: "revealAndClick gave up",
        evidence: [await evidence("01-db-reveal-failed")],
      });
    } else {
      await spa.waitForSelector("table.grid", { timeout: 15000 }).catch(() => {});
      await sleep(200);
      const grid = await gridSummary(spa);
      await evidence("02-db-table-readonly");
      if (!grid) {
        addFinding({
          severity: "S1",
          title: "CORE-2: opening " + CORE2_TAB + " with write mode off did not render a grid (read-only browsing broken)",
          repro: "write mode off; open " + CORE2_TAB,
          expected: "table.grid renders",
          actual: "no grid",
          evidence: ["evidence/CORE-2/02-db-table-readonly.png"],
        });
      } else if (grid.editableCells > 0 || grid.rowDelButtons > 0 || grid.hasInsertBtn) {
        addFinding({
          severity: "S1",
          title: "CORE-2: db grid renders a mutation affordance with write mode OFF",
          repro: "write mode off; open " + CORE2_TAB + "; inspect grid for td.editable / button.rowdel / #insertrow",
          expected: "zero editable cells, zero rowdel buttons, no insertrow button",
          actual: JSON.stringify(grid),
          evidence: ["evidence/CORE-2/02-db-table-readonly.png"],
        });
      } else if (grid.rows !== 2) {
        addFinding({
          severity: "S3",
          title: "CORE-2: db read-only grid row count unexpected",
          repro: "SELECT count(*) FROM " + CORE2_TAB,
          expected: "2",
          actual: String(grid.rows),
          evidence: ["evidence/CORE-2/02-db-table-readonly.png"],
        });
      }
    }

    // ---- cache (KV), write mode OFF ----
    engines.redis(["SET", CORE2_STR, "hello"]);
    engines.redis(["HSET", CORE2_HASH, "f1", "v1"]);
    spa = await harness.sidebarBrowse(page, CACHE_SERVICE);
    await harness.setWriteMode(page, spa, false);
    await harness.clickService(spa, CACHE_SERVICE);
    await sleep(300);
    const hintState = await spa.evaluate(() => ({
      hasCreateKeyLink: !!document.getElementById("createkeylink"),
      hasUploadBar: !!document.querySelector(".uploadbar"),
    }));
    await evidence("03-cache-hint-readonly");
    if (hintState.hasCreateKeyLink || hintState.hasUploadBar) {
      addFinding({
        severity: "S1",
        title: "CORE-2: cache hint renders a create/upload affordance with write mode OFF",
        repro: "write mode off; selectService('cache')",
        expected: "no #createkeylink, no .uploadbar",
        actual: JSON.stringify(hintState),
        evidence: ["evidence/CORE-2/03-cache-hint-readonly.png"],
      });
    }

    const openedStr = await revealAndClick(spa, CORE2_STR);
    if (openedStr) {
      await spa.waitForSelector("#dlblob", { timeout: 10000 }).catch(() => {});
      await sleep(150);
      const blobAff = await spa.evaluate(() => ({
        hasSave: !!document.getElementById("saveblob"),
        hasDelete: !!document.getElementById("delblob"),
        hasRename: !!document.getElementById("renameblob"),
        hasSetTTL: !!document.getElementById("setttl"),
        hasClrTTL: !!document.getElementById("clrttl"),
      }));
      await evidence("04-cache-string-readonly");
      if (Object.values(blobAff).some(Boolean)) {
        addFinding({
          severity: "S1",
          title: "CORE-2: KV string blob view renders a write affordance with write mode OFF",
          repro: "write mode off; open " + CORE2_STR,
          expected: "no save/delete/rename/setttl/clrttl",
          actual: JSON.stringify(blobAff),
          evidence: ["evidence/CORE-2/04-cache-string-readonly.png"],
        });
      }
    } else {
      addFinding({
        severity: "S2",
        title: "CORE-2: could not reveal " + CORE2_STR + " in the cache tree (write mode off)",
        repro: "openConsole('cache'); write mode off; reveal " + CORE2_STR,
        expected: "key reachable",
        actual: "revealAndClick gave up",
        evidence: [await evidence("04b-cache-str-reveal-failed")],
      });
    }

    await harness.clickService(spa, CACHE_SERVICE);
    await sleep(200);
    const openedHash = await revealAndClick(spa, CORE2_HASH);
    if (openedHash) {
      await spa.waitForSelector("table.grid", { timeout: 10000 }).catch(() => {});
      await sleep(150);
      const hashGrid = await gridSummary(spa);
      await evidence("05-cache-hash-readonly");
      if (!hashGrid) {
        addFinding({
          severity: "S1",
          title: "CORE-2: opening a hash key with write mode off did not render a grid",
          repro: "write mode off; open " + CORE2_HASH,
          expected: "table.grid renders",
          actual: "no grid",
          evidence: ["evidence/CORE-2/05-cache-hash-readonly.png"],
        });
      } else if (hashGrid.editableCells > 0 || hashGrid.rowDelButtons > 0) {
        addFinding({
          severity: "S1",
          title: "CORE-2: KV hash grid renders a mutation affordance with write mode OFF",
          repro: "write mode off; open " + CORE2_HASH,
          expected: "zero editable cells, zero rowdel buttons",
          actual: JSON.stringify(hashGrid),
          evidence: ["evidence/CORE-2/05-cache-hash-readonly.png"],
        });
      }
    } else {
      addFinding({
        severity: "S2",
        title: "CORE-2: could not reveal " + CORE2_HASH + " in the cache tree (write mode off)",
        repro: "openConsole('cache'); write mode off; reveal " + CORE2_HASH,
        expected: "key reachable",
        actual: "revealAndClick gave up",
        evidence: [await evidence("05b-cache-hash-reveal-failed")],
      });
    }
  } finally {
    core2Teardown(engines);
  }
}

// ============================================================
// CORE-4 — reveal/reopen: a HIDE (switch to another editor tab; the panel
// stays alive, retainContextWhenHidden) must preserve write mode across a
// reveal (regression check on the original broker-rebind bug) and must render
// the tree exactly once (KI-2 at the reveal path); a full CLOSE of the Data
// Console TAB + reopen is a DIFFERENT code path (consolePanel.js's
// onDidDispose -> consoleSession.js's killEntry tears down both the panel
// entry AND the underlying `zcp studio console serve` child process), so per
// the source read a reopen after a full close should spawn a genuinely fresh
// broker with writeEnabled defaulting false. This scenario drives BOTH paths
// against the real assembled system and states which behavior was actually
// observed rather than trusting the source read alone.
// ============================================================

async function closeDataConsoleTab(page) {
  return page.evaluate(() => {
    const tabs = Array.from(document.querySelectorAll(".tab"));
    const tab = tabs.find((t) => ((t.getAttribute("aria-label") || "") + " " + (t.textContent || "")).indexOf("Data Console") >= 0);
    if (!tab) return "no-tab";
    const closeBtn = tab.querySelector(".codicon-close");
    if (!closeBtn) return "no-close-btn";
    closeBtn.click();
    return "clicked";
  });
}

async function runCore4(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  try {
    // ---- 1. open db, enable write ----
    let spa = await harness.openConsole(page, DB_SERVICE);
    await harness.setWriteMode(page, spa, true);
    await evidence("01-db-write-on");
    const onState = await readSwitchState(spa);
    if (!onState.on) {
      addFinding({
        severity: "S1",
        title: "CORE-4: write mode did not turn ON before the hide/reveal test",
        repro: "openConsole('db'); setWriteMode(true)",
        expected: "#editswitch.on == true",
        actual: JSON.stringify(onState),
        evidence: ["evidence/CORE-4/01-db-write-on.png"],
      });
      return;
    }

    // ---- 2. hide: open a different editor tab via the command palette --
    // this does NOT dispose the webview panel (retainContextWhenHidden keeps
    // it alive off-screen), only switches which tab is visible/active. ----
    await page.keyboard.press("F1");
    await page.waitForSelector(".quick-input-widget input", { timeout: 10000 });
    await page.keyboard.type("New Untitled Text File");
    await sleep(300);
    await page.keyboard.press("Enter");
    await sleep(600);
    await evidence("02-other-tab-open-hides-panel");

    // ---- 3. sidebarBrowse('db') again -- the reveal path ----
    spa = await harness.sidebarBrowse(page, DB_SERVICE);
    await evidence("03-revealed");
    const revealState = await readSwitchState(spa);
    const dupeInfo = await treeDupeInfo(spa);
    await evidence("04-reveal-tree-state");
    if (!revealState.on) {
      addFinding({
        id: "CORE-4-S1-writemode-lost-on-hide-reveal",
        severity: "S1",
        title: "CORE-4: write mode was lost across a hide (other tab) + reveal (broker-rebind regression)",
        repro: "db: setWriteMode(true); open a new untitled file (hides, does not close, the Data Console panel); sidebarBrowse('db') again",
        expected: "#editswitch.on stays true (per consolePanel.js show(): a reveal carries the prior broker's isWriteEnabled() onto the new one)",
        actual: "#editswitch.on == false; state=" + JSON.stringify(revealState),
        evidence: ["evidence/CORE-4/01-db-write-on.png", "evidence/CORE-4/03-revealed.png"],
      });
    }
    if (dupeInfo.dupes.length) {
      addFinding({
        id: "CORE-4-S1-tree-dupe-on-reveal",
        severity: "S1",
        title: "CORE-4: tree renders duplicate root nodes on a hide+reveal (KI-2 regression at the reveal path)",
        repro: "db: write on; hide via a new untitled tab; sidebarBrowse('db') again; read #tree .node .nname",
        expected: "each node exactly once",
        actual: "duplicated: " + JSON.stringify(dupeInfo.dupes) + "; full: " + JSON.stringify(dupeInfo.names),
        evidence: ["evidence/CORE-4/04-reveal-tree-state.png"],
      });
    }

    // ---- 4. close the Data Console TAB entirely ----
    const closeResult = await closeDataConsoleTab(page);
    await evidence("05-after-tab-close-attempt");
    if (closeResult !== "clicked") {
      addFinding({
        severity: "S2",
        title: "CORE-4: could not close the Data Console tab to test the full-dispose reopen path",
        repro: "look for .tab[aria-label*='Data Console'] .codicon-close in the main frame",
        expected: "tab found and its close icon clickable",
        actual: "closeResult=" + closeResult,
        evidence: ["evidence/CORE-4/05-after-tab-close-attempt.png"],
      });
      return;
    }
    try {
      await page.waitForFunction(
        () =>
          !Array.from(document.querySelectorAll(".tab")).some(
            (t) => ((t.getAttribute("aria-label") || "") + " " + (t.textContent || "")).indexOf("Data Console") >= 0
          ),
        { timeout: 10000 }
      );
    } catch (_) {
      /* fall through -- the reopen probe below still tells the real story */
    }

    // ---- 5. re-open -- clean boot? ----
    spa = await harness.openConsole(page, DB_SERVICE);
    await evidence("06-reopened-after-full-close");
    const reopenState = await readSwitchState(spa);
    const reopenDupeInfo = await treeDupeInfo(spa);
    await evidence("07-reopen-tree-state");
    // Observational half of the scenario: state what was ACTUALLY seen either
    // way, per the brief ("VERIFY the actual designed behavior ... check and
    // assert accordingly, and STATE in the report which behavior you
    // observed"), not just pass/fail against one guess.
    addFinding({
      id: "CORE-4-reopen-writemode-observed",
      severity: reopenState.on ? "S2" : "S3",
      title:
        "CORE-4: a full Data-Console-tab close + reopen " +
        (reopenState.on
          ? "KEPT write mode ON (contradicts the source-read design: onDispose kills the console child + panel entry, so a reopen should spawn a genuinely fresh, read-only broker)"
          : "reset write mode to OFF (matches the source-read design: onDispose tears down both the panel entry and the underlying console child process, so the reopen's broker is brand new with writeEnabled defaulting false)"),
      repro: "db: write on; close the Data Console tab (.codicon-close); openConsole('db') again",
      expected: "n/a -- this IS the observed-behavior answer the brief asked for",
      actual: "#editswitch state after reopen=" + JSON.stringify(reopenState),
      evidence: ["evidence/CORE-4/06-reopened-after-full-close.png"],
      status: reopenState.on ? "new" : "info",
    });
    if (reopenDupeInfo.dupes.length) {
      addFinding({
        severity: "S2",
        title: "CORE-4: tree renders duplicate root nodes on the full-close reopen path",
        repro: "close the Data Console tab; openConsole('db') again; read #tree",
        expected: "each node exactly once",
        actual: "duplicated: " + JSON.stringify(reopenDupeInfo.dupes),
        evidence: ["evidence/CORE-4/07-reopen-tree-state.png"],
      });
    }
  } finally {
    try {
      const spa = await harness.spaFrame(page);
      await harness.setWriteMode(page, spa, false); // leave write mode off for whichever scenario runs next
    } catch (_) {
      /* best effort */
    }
  }
}

// ============================================================
// CORE-5 — state canon: empty-grid state (never a blank void) and inline
// error state (syntax-error query), both against the real console.
// ============================================================

const CORE5_EMPTY_TAB = "uitest_core_empty";

function core5Teardown(engines) {
  try {
    engines.psql("DROP TABLE IF EXISTS " + CORE5_EMPTY_TAB);
  } catch (_) {
    /* best effort */
  }
}

async function runCore5(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  core5Teardown(engines);
  try {
    engines.psql("CREATE TABLE " + CORE5_EMPTY_TAB + " (id serial PRIMARY KEY, txt text)");

    let spa = await harness.openConsole(page, DB_SERVICE);
    await harness.setWriteMode(page, spa, false);
    await harness.clickService(spa, DB_SERVICE);
    await sleep(400);
    const opened = await revealAndClick(spa, CORE5_EMPTY_TAB);
    if (!opened) {
      addFinding({
        severity: "S1",
        title: "CORE-5: could not reveal the empty fixture table " + CORE5_EMPTY_TAB + " in the tree",
        repro: "CREATE TABLE " + CORE5_EMPTY_TAB + " (empty); openConsole('db'); reveal in tree",
        expected: "table reachable",
        actual: "revealAndClick gave up",
        evidence: [await evidence("01-reveal-failed")],
      });
    } else {
      await spa.waitForSelector("table.grid", { timeout: 15000 }).catch(() => {});
      await sleep(200);
      const emptyState = await spa.evaluate(() => {
        const table = document.querySelector("table.grid");
        const body = table ? table.querySelector("tbody.gridbody") : null;
        const emptyCell = body ? body.querySelector("td.state.empty") : null;
        return {
          hasTable: !!table,
          rowCount: body ? body.querySelectorAll("tr").length : 0,
          hasEmptyCell: !!emptyCell,
          emptyText: emptyCell ? emptyCell.textContent : null,
          contentBlank: (document.getElementById("content") || { innerHTML: "" }).innerHTML.trim() === "",
        };
      });
      await evidence("02-empty-table-state");
      if (emptyState.contentBlank || !emptyState.hasTable) {
        addFinding({
          severity: "S1",
          title: "CORE-5: an empty table renders a blank void instead of the grid's empty-state row",
          repro: "open " + CORE5_EMPTY_TAB + " (0 rows)",
          expected: 'table.grid renders with a td.state.empty row ("No rows")',
          actual: JSON.stringify(emptyState),
          evidence: ["evidence/CORE-5/02-empty-table-state.png"],
        });
      } else if (!emptyState.hasEmptyCell || emptyState.rowCount !== 1) {
        addFinding({
          severity: "S2",
          title: "CORE-5: an empty table's grid does not render the canon td.state.empty row",
          repro: "open " + CORE5_EMPTY_TAB + " (0 rows)",
          expected: 'exactly one <tr> containing td.state.empty ("No rows")',
          actual: JSON.stringify(emptyState),
          evidence: ["evidence/CORE-5/02-empty-table-state.png"],
        });
      }
    }

    // ---- error state: syntax-error query ----
    // #querylink lives on the placeholder HINT only (selectService's render),
    // not on an opened table's grid -- the empty-table check above replaced
    // #content with the grid, so #querylink is gone from the DOM at this
    // point. Re-select the service to land back on the hint before checking
    // for it (same sequencing TAB-8 uses: clickService lands on the hint,
    // #querylink is read there, THEN the query pane opens).
    await harness.clickService(spa, DB_SERVICE);
    await sleep(300);
    const hint = await spa.evaluate(() => !!document.getElementById("querylink"));
    if (hint) {
      await spa.click("#querylink");
      await spa.waitForSelector("#qtext", { timeout: 10000 });
      await spa.evaluate((tab) => {
        document.getElementById("qtext").value = "SELEKT * FROM " + tab;
      }, CORE5_EMPTY_TAB);
      await spa.click("#runq");
      await sleep(800);
      await evidence("03-syntax-error-state");
      const errState = await spa.evaluate(() => ({
        hasErr: !!document.querySelector("#qresult .err"),
        crashed: !document.getElementById("qresult"),
        text: (document.querySelector("#qresult .err-msg") || {}).textContent || null,
      }));
      if (errState.crashed || !errState.hasErr) {
        addFinding({
          severity: "S1",
          title: "CORE-5: a syntax-error query does not render the canon .err state",
          repro: "run 'SELEKT * FROM " + CORE5_EMPTY_TAB + "'",
          expected: "#qresult .err renders with a message, no crash",
          actual: JSON.stringify(errState),
          evidence: ["evidence/CORE-5/03-syntax-error-state.png"],
        });
      }
    } else {
      addFinding({
        severity: "S2",
        title: "CORE-5: no #querylink offered for postgres -- cannot exercise the error-state check",
        repro: "openConsole('db')",
        expected: "#querylink present",
        actual: "absent",
        evidence: [await evidence("03b-no-querylink")],
      });
    }
  } finally {
    core5Teardown(engines);
  }
}

// ============================================================
// CORE-6 — error honesty across three surfaces: (a) KV duplicate create
// (action-aware conflict copy), (b) tabular invalid numeric edit (honest
// rejection + visible cell revert), (c) elasticsearch invalid-JSON create
// (the specific client-side message, never the bare generic fallback).
// ============================================================

const CORE6_DUP_KEY = "uitest_core_dup";
const CORE6_TAB = "uitest_core6_tab";
const CORE6_ES_INDEX = "uitest_core6_es";

function core6Teardown(engines) {
  try {
    engines.redis(["DEL", CORE6_DUP_KEY]);
  } catch (_) {
    /* best effort */
  }
  try {
    engines.psql("DROP TABLE IF EXISTS " + CORE6_TAB);
  } catch (_) {
    /* best effort */
  }
}

function core6EsRequest(engines, method, path, bodyText) {
  const cfg = loadConfig();
  const auth = "-u " + engines.shellQuote(cfg.DC_ES_USER + ":" + cfg.DC_ES_PASSWORD);
  const url = "http://" + cfg.DC_ES_HOST + ":" + cfg.DC_ES_PORT + path;
  let cmd = "curl -s -X " + method + " " + auth + " " + url;
  if (bodyText !== undefined) {
    cmd += " -H " + engines.shellQuote("Content-Type: application/json") + " --data-binary " + engines.shellQuote(bodyText);
  }
  const out = engines.container(cmd);
  if (!out) return null;
  try {
    return JSON.parse(out);
  } catch (_) {
    return out;
  }
}
function core6EsDocCount(engines) {
  const r = core6EsRequest(engines, "GET", "/" + CORE6_ES_INDEX + "/_count");
  return r && typeof r.count === "number" ? r.count : null;
}
function core6EsTeardown(engines) {
  try {
    core6EsRequest(engines, "DELETE", "/" + CORE6_ES_INDEX);
  } catch (_) {
    /* best effort */
  }
}

// ---- (a) KV duplicate create: conflict copy must be the CREATE wording ----
async function core6a(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  const originalVal = "original-value";
  engines.redis(["SET", CORE6_DUP_KEY, originalVal]);

  let spa = await harness.openConsole(page, CACHE_SERVICE);
  await harness.setWriteMode(page, spa, true);
  await harness.clickService(spa, CACHE_SERVICE);
  await sleep(300);
  const hasLink = await spa.evaluate(() => !!document.getElementById("createkeylink"));
  if (!hasLink) {
    addFinding({
      severity: "S1",
      title: "CORE-6a: #createkeylink not present after enabling write mode on cache",
      repro: "openConsole('cache'); setWriteMode(true)",
      expected: "#createkeylink present",
      actual: "absent",
      evidence: [await evidence("00a-no-createkeylink")],
    });
    return;
  }
  await spa.click("#createkeylink");
  await spa.waitForSelector("#modal:not(.hidden)", { timeout: 15000 });
  await setInputLocal(spa, "#kvname", CORE6_DUP_KEY);
  await spa.select("#kvtype", "string");
  await setInputLocal(spa, "#kvval", "clobber-attempt");
  await spa.click("#modalok");
  // Round-2 modal contract (spec §7.4): the rejection keeps the modal OPEN with
  // the typed input and renders the conflict INLINE (#modalerr) — no toast.
  let inlineErr = "";
  try {
    await spa.waitForSelector("#modalerr", { timeout: 8000 });
    inlineErr = await spa.evaluate(() => (document.getElementById("modalerr") || {}).textContent || "");
  } catch (_) { /* absent — recorded below */ }
  const stillOpen = await spa.evaluate(() => !document.getElementById("modal").classList.contains("hidden"));
  await evidence("01-kv-duplicate-conflict"); // capture with the inline error visible in the open modal
  const afterVal = engines.redis(["GET", CORE6_DUP_KEY]);

  const EXPECTED_CONFLICT_TEXT = "Already exists — choose a different id.";
  if (!stillOpen || inlineErr.indexOf(EXPECTED_CONFLICT_TEXT) < 0) {
    addFinding({
      severity: "S1",
      title: 'CORE-6a: duplicate-create rejection did not keep the modal open with the inline "' + EXPECTED_CONFLICT_TEXT + '" error',
      repro: "seed " + CORE6_DUP_KEY + "; Add key with the same name again; confirm",
      expected: 'modal stays open; #modalerr contains "' + EXPECTED_CONFLICT_TEXT + '"',
      actual: "open=" + stillOpen + "; inline err=" + JSON.stringify(inlineErr),
      evidence: ["evidence/CORE-6/01-kv-duplicate-conflict.png"],
    });
  }
  // Close the rejected modal (it stays open by design) so core6b starts clean.
  await spa.click("#modalcancel");
  await spa.waitForSelector("#modal.hidden", { timeout: 5000 }).catch(() => {});
  await sleep(200);
  if (afterVal !== originalVal) {
    addFinding({
      severity: "S1",
      title: "CORE-6a: KV duplicate-create conflict was reported but the engine value changed anyway",
      repro: "create-collide " + CORE6_DUP_KEY,
      expected: 'value stays "' + originalVal + '"',
      actual: "value now " + JSON.stringify(afterVal),
      evidence: ["evidence/CORE-6/01-kv-duplicate-conflict.png"],
      engine_truth: "redis GET " + CORE6_DUP_KEY + " = " + JSON.stringify(afterVal),
    });
  }
}

// ---- (b) tabular invalid numeric edit: honest toast + visible revert ----
async function core6b(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  engines.psql("CREATE TABLE " + CORE6_TAB + " (id serial PRIMARY KEY, txt text, num numeric(10,2))");
  engines.psql("INSERT INTO " + CORE6_TAB + " (txt, num) VALUES ('row1', 10.00)");

  let spa = await harness.openConsole(page, DB_SERVICE);
  await harness.setWriteMode(page, spa, true);
  await harness.clickService(spa, DB_SERVICE);
  await sleep(400);
  const opened = await revealAndClick(spa, CORE6_TAB);
  if (!opened) {
    addFinding({
      severity: "S1",
      title: "CORE-6b: could not reveal " + CORE6_TAB + " in the tree",
      repro: "openConsole('db'); write on; reveal " + CORE6_TAB,
      expected: "table reachable",
      actual: "revealAndClick gave up",
      evidence: [await evidence("02-reveal-failed")],
    });
    return;
  }
  await spa.waitForSelector("table.grid", { timeout: 15000 });
  await sleep(200);
  const heads = await spa.evaluate(() => Array.from(document.querySelectorAll("table.grid thead th")).map((th) => th.textContent));
  const numColIdx = heads.findIndex((h) => h.indexOf("num") === 0);
  const rowSel = "table.grid tbody.gridbody tr:nth-child(1) td:nth-child(" + (numColIdx + 1) + ")";

  const before = await spa.evaluate((s) => (document.querySelector(s) || {}).textContent, rowSel);
  await spa.click(rowSel);
  await spa.waitForSelector(rowSel + " input.celledit", { timeout: 10000 });
  await spa.evaluate(
    (s, v) => {
      const el = document.querySelector(s);
      if (el) {
        el.value = v;
        el.focus();
      }
    },
    rowSel + " input.celledit",
    "not-a-number"
  );
  await page.keyboard.press("Enter");
  const toast = await harness.waitToast(spa);
  await sleep(300);
  const after = await spa.evaluate((s) => (document.querySelector(s) || {}).textContent, rowSel);
  const stillHasInput = await spa.evaluate((s) => !!document.querySelector(s + " input.celledit"), rowSel);
  const engineNum = String(engines.psql("SELECT num FROM " + CORE6_TAB + " WHERE txt='row1'")).trim();
  await evidence("03-invalid-numeric-edit");

  const toastHonest = !toast || toast.kind !== "good";
  const cellReverted = after === before && !stillHasInput;
  const engineUnchanged = engineNum === "10.00";
  if (!toastHonest || !cellReverted || !engineUnchanged) {
    addFinding({
      severity: "S1",
      title: "CORE-6b: invalid numeric cell edit is not honestly rejected end-to-end",
      repro: "write on; open " + CORE6_TAB + "; edit 'num' cell to 'not-a-number'; Enter",
      expected: "no success toast; cell visibly reverts to " + JSON.stringify(before) + "; engine num stays 10.00",
      actual:
        "toast=" + JSON.stringify(toast) + "; cellBefore=" + JSON.stringify(before) + " cellAfter=" + JSON.stringify(after) +
        " stillHasInput=" + stillHasInput + "; engine num=" + engineNum,
      evidence: ["evidence/CORE-6/03-invalid-numeric-edit.png"],
      engine_truth: "num = " + engineNum,
    });
  }
}

// ---- (c) es invalid-JSON create: specific message, not the bare generic ----
async function core6c(ctx) {
  const { page, harness, engines, evidence, addFinding } = ctx;
  core6EsTeardown(engines);
  core6EsRequest(engines, "PUT", "/" + CORE6_ES_INDEX + "/_doc/seed1", JSON.stringify({ id: "seed1", title: "seed" }));
  core6EsRequest(engines, "POST", "/" + CORE6_ES_INDEX + "/_refresh");
  const countBefore = core6EsDocCount(engines);

  let spa = await harness.sidebarBrowse(page, "es");
  await harness.setWriteMode(page, spa, true);
  spa = await harness.spaFrame(page);
  await harness.clickService(spa, "es");
  await spa.waitForSelector("#searchlink", { timeout: 15000 });
  await spa.click("#searchlink");
  await spa.waitForSelector(".searchbar", { timeout: 15000 });
  const hasIdx = await spa.evaluate(
    (n) => Array.from((document.getElementById("sidx") || { options: [] }).options).some((o) => o.value === n),
    CORE6_ES_INDEX
  );
  if (!hasIdx) {
    addFinding({
      severity: "S2",
      title: "CORE-6c: seeded index " + CORE6_ES_INDEX + " not offered in the search pane's index selector",
      repro: "PUT " + CORE6_ES_INDEX + "/_doc/seed1; openSearchPane(es)",
      expected: CORE6_ES_INDEX + " present in #sidx options",
      actual: "absent",
      evidence: [await evidence("04-no-index-in-selector")],
    });
    return;
  }
  await spa.select("#sidx", CORE6_ES_INDEX);
  await spa.waitForSelector("#adddoc", { timeout: 15000 });
  await spa.click("#adddoc");
  await spa.waitForSelector("#docbody", { timeout: 15000 });
  await spa.type("#docid", "uitest_core6_bad");
  await spa.type("#docbody", '{"broken');
  await spa.click("#modalok");
  // Invalid JSON is refused CLIENT-side (createDocForm's JSON.parse guard
  // throws "Body is not valid JSON.") and, per the round-2 modal contract, the
  // rejection keeps the modal OPEN with the typed body and renders the message
  // INLINE (#modalerr) — no toast at all. A toast read here would only ever
  // pick up a stale toast from the previous sub-test (gotcha #8).
  let jsonErr = "";
  try {
    await spa.waitForSelector("#modalerr", { timeout: 8000 });
    jsonErr = await spa.evaluate(() => (document.getElementById("modalerr") || {}).textContent || "");
  } catch (_) { /* absent — recorded below */ }
  const jsonStillOpen = await spa.evaluate(() => !document.getElementById("modal").classList.contains("hidden"));
  const bodyKept = await spa.evaluate(() => (document.getElementById("docbody") || {}).value || "");
  await sleep(300);
  const countAfter = core6EsDocCount(engines);
  await evidence("05-es-invalid-json-create");

  const SPECIFIC = "Body is not valid JSON.";
  if (!jsonStillOpen || jsonErr.indexOf(SPECIFIC) < 0 || bodyKept !== '{"broken') {
    addFinding({
      severity: "S2",
      title: 'CORE-6c: invalid-JSON create did not keep the modal open with the inline "' + SPECIFIC + '" error and the typed body',
      repro: 'es search -> Add document; body={"broken; confirm',
      expected: 'modal stays open; #modalerr includes "' + SPECIFIC + '"; #docbody keeps the typed text',
      actual: "open=" + jsonStillOpen + "; err=" + JSON.stringify(jsonErr) + "; body=" + JSON.stringify(bodyKept),
      evidence: ["evidence/CORE-6/05-es-invalid-json-create.png"],
    });
  }
  if (countAfter !== countBefore) {
    addFinding({
      severity: "S1",
      title: "CORE-6c: invalid-JSON create changed the es doc count",
      repro: "Add document with malformed JSON body",
      expected: "doc count unchanged (" + countBefore + ")",
      actual: String(countAfter),
      evidence: ["evidence/CORE-6/05-es-invalid-json-create.png"],
      engine_truth: "es count before=" + countBefore + " after=" + countAfter,
    });
  }
  // The rejected modal stays open by design — close it so the shared browser
  // session hands the next scenario a clean page.
  await spa.click("#modalcancel");
  await spa.waitForSelector("#modal.hidden", { timeout: 5000 }).catch(() => {});
}

async function runCore6(ctx) {
  const { engines } = ctx;
  core6Teardown(engines);
  try {
    await core6a(ctx);
    await core6b(ctx);
    await core6c(ctx);
  } finally {
    core6Teardown(engines);
    core6EsTeardown(engines);
  }
}

// ============================================================
// CORE-7 — rapid hopping: sidebar-browse through all 11 services back-to-back
// with minimal waits between hops (deliberately stressing the SAME tree-load
// generation guard KI-2 fixed, generalized from same-service refresh bursts
// to CROSS-service hopping), then on the FINAL service assert a clean render:
// correct family view, no duplicated tree nodes, no stuck spinner.
// ============================================================

const CORE7_SERVICES = ["db", "mariadb", "storage", "cache", "ch", "es", "docs", "search", "vectors", "queue", "events"];

async function core7QuickProbe(frame, service, budgetMs) {
  const t0 = Date.now();
  try {
    await frame.waitForFunction(
      (svc) => {
        const li = document.querySelector("#services li.active span");
        if (!li || li.textContent !== svc) return false;
        if (document.querySelector("#tree .state.loading")) return false;
        return document.querySelectorAll("#tree .node").length > 0 || !!document.querySelector("#tree .state.empty");
      },
      { timeout: budgetMs },
      service
    );
    return { settled: true, ms: Date.now() - t0 };
  } catch (_) {
    return { settled: false, ms: Date.now() - t0 };
  }
}

async function runCore7(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  const sidebar = await harness.openSidebar(page);
  const timings = [];
  let spa = null;

  for (let i = 0; i < CORE7_SERVICES.length; i++) {
    const svc = CORE7_SERVICES[i];
    const isLast = i === CORE7_SERVICES.length - 1;
    const sel = '[data-action="openConsole"][data-service="' + svc + '"]';
    try {
      await sidebar.waitForSelector(sel, { timeout: 15000 });
      await sidebar.click(sel);
    } catch (e) {
      addFinding({
        severity: "S1",
        title: "CORE-7: sidebar Browse-data button missing/unclickable for " + svc,
        repro: "hop through all 11 services; click " + sel,
        expected: "button present and clickable",
        actual: String(e && e.message ? e.message : e),
        evidence: [],
      });
      continue;
    }
    try {
      spa = await harness.findFrameByProbe(page, ["#rail", "#topbar", "#services li"], 15000);
    } catch (e) {
      addFinding({
        severity: "S1",
        title: "CORE-7: SPA frame not found while hopping to " + svc,
        repro: "hop through all 11 services",
        expected: "frame reachable",
        actual: String(e && e.message ? e.message : e),
        evidence: [],
      });
      continue;
    }
    const probe = await core7QuickProbe(spa, svc, isLast ? 20000 : 2000);
    timings.push({ service: svc, ms: probe.ms, settled: probe.settled });
    console.log("CORE-7: " + svc + " settle=" + probe.settled + " in " + probe.ms + "ms");
    if (!isLast) await sleep(120); // deliberately minimal -- keep hops rapid, not fully serialized
  }
  await evidence("01-final-service-state");

  addFinding({
    id: "CORE-7-hop-timings",
    severity: "S3",
    title: "CORE-7: per-service hop settle timings across all 11 services",
    repro: "sidebar-hop through " + CORE7_SERVICES.join(", ") + " back-to-back with a 120ms gap between clicks",
    expected: "n/a -- informational timing record",
    actual: JSON.stringify(timings),
    evidence: [],
    status: "info",
  });

  const lastService = CORE7_SERVICES[CORE7_SERVICES.length - 1];
  if (!spa) return;
  const finalTreeState = await spa.evaluate((svc) => {
    const li = document.querySelector("#services li.active span");
    const names = Array.from(document.querySelectorAll("#tree .node .nname")).map((el) => el.textContent);
    const counts = {};
    for (const n of names) counts[n] = (counts[n] || 0) + 1;
    return {
      activeLabel: li ? li.textContent : null,
      activeMatches: !!li && li.textContent === svc,
      names,
      dupes: Object.entries(counts).filter(([, c]) => c > 1),
      hasStuckSpinner: !!document.querySelector(".spinner"),
      hasErr: !!document.querySelector("#content .err"),
    };
  }, lastService);
  await evidence("02-final-tree-inspected");

  if (!finalTreeState.activeMatches) {
    addFinding({
      severity: "S1",
      title: "CORE-7: after hopping through all 11 services, the rail's active label does not match the final service",
      repro: "hop through " + CORE7_SERVICES.join(", "),
      expected: 'active label == "' + lastService + '"',
      actual: JSON.stringify(finalTreeState),
      evidence: ["evidence/CORE-7/02-final-tree-inspected.png"],
    });
  }
  if (finalTreeState.dupes.length) {
    addFinding({
      id: "CORE-7-final-tree-dupes",
      severity: "S1",
      title: "CORE-7: the final service's tree shows duplicated nodes after rapid hopping through all 11 services",
      repro: "hop through " + CORE7_SERVICES.join(", ") + " back-to-back with minimal waits",
      expected: "each node exactly once on the final (" + lastService + ") tree",
      actual: "duplicated: " + JSON.stringify(finalTreeState.dupes) + "; full: " + JSON.stringify(finalTreeState.names),
      evidence: ["evidence/CORE-7/02-final-tree-inspected.png"],
    });
  }
  if (finalTreeState.hasStuckSpinner) {
    addFinding({
      severity: "S1",
      title: "CORE-7: a stuck .spinner remains visible on the final service after settling",
      repro: "hop through all 11 services; inspect the final (" + lastService + ") frame after settle",
      expected: "no .spinner once settled",
      actual: JSON.stringify(finalTreeState),
      evidence: ["evidence/CORE-7/02-final-tree-inspected.png"],
    });
  }

  if (finalTreeState.names.length > 0) {
    const firstName = finalTreeState.names[0];
    const clicked = await spa.evaluate((n) => {
      const rows = Array.from(document.querySelectorAll("#tree .node"));
      const row = rows.find((r) => {
        const nm = r.querySelector(".nname");
        return nm && nm.textContent === n;
      });
      if (!row) return false;
      row.click();
      return true;
    }, firstName);
    await sleep(500);
    await evidence("03-final-service-node-opened");
    const familyView = await spa.evaluate(() => ({
      hasErr: !!document.querySelector("#content .err"),
      hasStreamCard: !!document.querySelector(".streamcard"),
      hasAnyRender: (document.getElementById("content") || { innerHTML: "" }).innerHTML.trim() !== "",
    }));
    if (!clicked || familyView.hasErr || !familyView.hasAnyRender) {
      addFinding({
        severity: "S1",
        title: "CORE-7: opening a node on the final service after rapid hopping did not render the expected family view",
        repro: "hop through all 11 services; open '" + firstName + "' on " + lastService,
        expected: "a family-appropriate render (e.g. .streamcard for " + lastService + "), no inline error",
        actual: "clicked=" + clicked + "; " + JSON.stringify(familyView),
        evidence: ["evidence/CORE-7/03-final-service-node-opened.png"],
      });
    }
  }
}

runner.register({ id: "CORE-1", family: "kv+tabular+write-mode", fn: run });
runner.register({ id: "CORE-2", family: "core", fn: runCore2 });
runner.register({ id: "CORE-4", family: "core", fn: runCore4 });
runner.register({ id: "CORE-5", family: "core", fn: runCore5 });
runner.register({ id: "CORE-6", family: "core", fn: runCore6 });
runner.register({ id: "CORE-7", family: "core", fn: runCore7 });

module.exports = { run: run, KEY: KEY, FIXTURE_KEY: FIXTURE_KEY };
