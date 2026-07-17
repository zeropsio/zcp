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

runner.register({ id: "CORE-1", family: "kv+tabular+write-mode", fn: run });

module.exports = { run: run, KEY: KEY, FIXTURE_KEY: FIXTURE_KEY };
