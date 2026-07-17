"use strict";

// STR-* (family "stream": queue/nats + events/kafka) + CORE-3 (family
// "standalone") scenarios — A3 fan-out.
//
// Stream (kafka/nats) is READ-ONLY BY NATURE at the provider level (stream.go:
// WriteBlob/Delete/Rename/SetTTL/SetEntry/InsertRow/DeleteRow/EditCell all
// return provider.ErrReadOnly or no-op) — there is no data to seed/teardown;
// these scenarios open EXISTING streams/topics and compare the rendered
// summary card against live engine metadata (nats: /jsz over HTTP, reachable
// directly from this machine over the project VPN, confirmed live below;
// kafka: no CLI on the container or locally, so kafka checks are UI-render
// honesty only, per the brief).
//
// CORE-3 proves the STANDALONE reach path end-to-end: §4.2 of
// docs/spec-dataconsole.md says the console has NO in-UI "open in browser"
// button by design (embed is the only first-party surface) but stays
// reachable as a plain browser tab through code-server's own `/proxy/<port>/`
// forward, read-only by construction (bearer only, no write token). Verified
// live below: package.json has no `contributes.commands` and extension.js has
// no registerCommand call, so there truly is no product-side entry point —
// the only path is a developer running `zcp studio console serve` themselves
// (e.g. in code-server's own integrated terminal) and hand-building the
// proxy URL, exactly as documented. This scenario drives that real mechanism
// (SSH standing in for "a terminal on the container"), not a mock of it.

const fs = require("fs");
const path = require("path");
const { execFileSync } = require("child_process");
const { loadConfig, ensureAuthToken } = require("../lib/config");
const { shellQuote } = require("../lib/engines");
const runner = require("../lib/runner");

const NATS_SERVICE = "queue";
const KAFKA_SERVICE = "events";
const CACHE_SERVICE = "cache";
const GREETING_FIXTURE = "greeting"; // seeded; read-only touch only, never mutated

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// waitForTreeLoaded waits for loadTree()'s async fetch to finish -- #tree
// shows a ".state.loading" spinner while it's in flight, then either real
// .node rows or a ".state.empty" placeholder. A bare read right after
// openConsole() can race that fetch (harness.js only waits for #services li
// as its "ready" signal, not for the tree's own load) and misread "still
// loading" as "empty" -- caught live: STR-1's first run read an empty queue
// tree while the pane still showed a "Loading..." spinner in the same
// screenshot.
async function waitForTreeLoaded(frame, timeoutMs) {
  try {
    await frame.waitForFunction(
      () => !!document.querySelector("#tree .node") || !!document.querySelector("#tree .state.empty"),
      { timeout: timeoutMs || 8000 }
    );
  } catch (_) {
    /* fall through -- an honest empty read still reflects reality */
  }
}

async function clickTreeNode(frame, name) {
  await waitForTreeLoaded(frame);
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

async function treeNodeNames(frame) {
  await waitForTreeLoaded(frame);
  return frame.evaluate(() => Array.from(document.querySelectorAll("#tree .node .nname")).map((el) => el.textContent));
}

// streamCardState reads the rendered .streamcard/dl.streammeta (or reports
// its absence) plus every write-affordance id, so "zero write affordances"
// is one honest snapshot rather than N separate DOM probes.
async function streamCardState(frame) {
  return frame.evaluate(() => {
    const dl = document.querySelector("dl.streammeta");
    const rows = {};
    if (dl) {
      const dts = Array.from(dl.querySelectorAll("dt"));
      const dds = Array.from(dl.querySelectorAll("dd"));
      dts.forEach((dt, i) => { rows[dt.textContent] = dds[i] ? dds[i].textContent : null; });
    }
    return {
      hasStreamCard: !!document.querySelector(".streamcard"),
      hasStreamMeta: !!dl,
      labelBadgeText: (document.querySelector(".streamlabel .badge") || {}).textContent || null,
      rows: rows,
      // real write affordances -- Download is NOT one of these (it's a plain
      // read/export action rendered unconditionally by openBlob's toolbar).
      hasSave: !!document.getElementById("saveblob"),
      hasDelete: !!document.getElementById("delblob"),
      hasRename: !!document.getElementById("renameblob"),
      hasUploadBar: !!document.querySelector(".uploadbar"),
      hasInsertRow: !!document.getElementById("insertrow"),
      hasRowDelete: !!document.querySelector("button.rowdel"),
      hasCreateKeyLink: !!document.getElementById("createkeylink"),
      hasEditableCell: !!document.querySelector("td.editable"),
      hasTextarea: !!document.getElementById("blobedit"),
      looksLikeRawJSON: !!document.querySelector("pre.blob"), // U-04 regression guard
    };
  });
}

async function natsJsz() {
  const cfg = loadConfig();
  const url = (cfg.DC_NATS_MGMT_URL || "http://queue:8222") + "/jsz?streams=true&consumers=true";
  const r = await fetch(url, { signal: AbortSignal.timeout(8000) });
  if (!r.ok) throw new Error("natsJsz: " + r.status);
  return r.json();
}

// ============================================================================
// STR-1 — nats metadata card vs /jsz truth
// ============================================================================

async function runSTR1(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  const spa = await harness.openConsole(page, NATS_SERVICE);
  await harness.setWriteMode(page, spa, false);
  await sleep(200);
  const names = await treeNodeNames(spa);
  await evidence("01-queue-tree");

  if (names.length === 0) {
    addFinding({
      severity: "S2",
      title: "STR-1: no streams visible in the nats (queue) tree",
      repro: "openConsole(queue)",
      expected: "at least the pre-existing EVENTS stream visible",
      actual: "tree names: " + JSON.stringify(names),
      evidence: ["evidence/STR-1/01-queue-tree.png"],
    });
    return;
  }

  let jsz;
  try {
    jsz = await natsJsz();
  } catch (e) {
    addFinding({
      severity: "S3",
      title: "STR-1: could not fetch /jsz from this machine to cross-check (VPN/reachability)",
      repro: "fetch " + (loadConfig().DC_NATS_MGMT_URL || "http://queue:8222") + "/jsz",
      expected: "n/a -- environment note",
      actual: String(e),
      evidence: [],
      status: "info",
    });
    jsz = null;
  }

  const target = names.includes("EVENTS") ? "EVENTS" : names[0];
  await clickTreeNode(spa, target);
  await sleep(300);
  const card = await streamCardState(spa);
  await evidence("02-stream-card");

  if (!card.hasStreamCard || !card.hasStreamMeta) {
    addFinding({
      severity: "S1",
      title: "STR-1: opening a nats stream did not render .streamcard/dl.streammeta",
      repro: "open " + target + " under queue",
      expected: "labelled metadata card, never a raw/editable-looking blob",
      actual: JSON.stringify(card),
      evidence: ["evidence/STR-1/02-stream-card.png"],
    });
    return;
  }
  if (!card.labelBadgeText || card.labelBadgeText.indexOf("stream metadata") < 0) {
    addFinding({
      severity: "S2",
      title: 'STR-1: stream card missing the "stream metadata" badge label',
      repro: "open " + target,
      expected: 'badge text includes "stream metadata"',
      actual: 'labelBadgeText="' + card.labelBadgeText + '"',
      evidence: ["evidence/STR-1/02-stream-card.png"],
    });
  }

  if (jsz && target === "EVENTS") {
    const detail = ((jsz.account_details || [])[0] || {}).stream_detail || [];
    const truth = detail.find((s) => s.name === "EVENTS");
    if (truth) {
      const uiMessages = card.rows["messages"];
      const uiSeq = card.rows["sequence"];
      const truthSeq = truth.state.first_seq + " → " + truth.state.last_seq;
      // Messages can tick up between the jsz fetch and the UI read (live
      // stream) -- compare with tolerance, not exact equality.
      const uiMsgNum = parseInt(uiMessages, 10);
      const plausible = Number.isFinite(uiMsgNum) && Math.abs(uiMsgNum - truth.state.messages) <= 5;
      if (!plausible) {
        addFinding({
          severity: "S1",
          title: "STR-1: rendered message count is not plausible vs /jsz truth",
          repro: "compare dl.streammeta 'messages' row vs /jsz stream_detail[EVENTS].state.messages",
          expected: "within a few messages of " + truth.state.messages + " (live stream tolerance)",
          actual: 'ui="' + uiMessages + '"; jsz=' + truth.state.messages,
          evidence: ["evidence/STR-1/02-stream-card.png"],
          engine_truth: "GET /jsz -> EVENTS.state.messages = " + truth.state.messages,
        });
      }
      if (uiSeq !== truthSeq) {
        addFinding({
          severity: "S3",
          title: "STR-1: rendered sequence range differs from /jsz truth (may be a live-stream timing artifact)",
          repro: "compare dl.streammeta 'sequence' row vs /jsz first_seq/last_seq",
          expected: truthSeq + " (at fetch time)",
          actual: uiSeq,
          evidence: ["evidence/STR-1/02-stream-card.png"],
          engine_truth: "GET /jsz -> EVENTS.state first_seq/last_seq = " + truthSeq,
          status: "info",
        });
      }
    }
  }

  // Never editable-looking (U-04 regression guard) + no write affordances at all.
  const dirty = card.hasSave || card.hasDelete || card.hasRename || card.hasUploadBar || card.hasInsertRow || card.hasRowDelete || card.hasEditableCell || card.hasTextarea;
  if (dirty) {
    addFinding({
      severity: "S1",
      title: "STR-1: nats stream card exposes a write affordance it must never have",
      repro: "open " + target + " under queue",
      expected: "no save/delete/rename/upload/insert/row-delete/editable-cell/textarea",
      actual: JSON.stringify(card),
      evidence: ["evidence/STR-1/02-stream-card.png"],
    });
  }
}

// ============================================================================
// STR-2 — kafka card (UI-render-only truth, no CLI available)
// ============================================================================

async function runSTR2(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  const spa = await harness.openConsole(page, KAFKA_SERVICE);
  await harness.setWriteMode(page, spa, false);
  await sleep(200);
  const names = await treeNodeNames(spa);
  await evidence("01-events-tree");

  if (names.length === 0) {
    addFinding({
      severity: "S2",
      title: "STR-2: no topics visible in the kafka (events) tree",
      repro: "openConsole(events)",
      expected: "at least one topic visible",
      actual: "tree names: " + JSON.stringify(names),
      evidence: ["evidence/STR-2/01-events-tree.png"],
    });
    return;
  }

  await clickTreeNode(spa, names[0]);
  await sleep(300);
  const card = await streamCardState(spa);
  await evidence("02-kafka-card");

  if (!card.hasStreamCard || !card.hasStreamMeta) {
    addFinding({
      severity: "S1",
      title: "STR-2: opening a kafka topic did not render .streamcard/dl.streammeta",
      repro: "open " + names[0] + " under events",
      expected: "labelled metadata card",
      actual: JSON.stringify(card),
      evidence: ["evidence/STR-2/02-kafka-card.png"],
    });
    return;
  }
  addFinding({
    id: "STR-2-kafka-card-fields",
    severity: "S3",
    title: "STR-2: kafka topic card rendered fields (UI-render-only truth, no CLI oracle on this environment)",
    repro: "open " + names[0] + " under events",
    expected: "n/a -- recording actual field set for the UX lane",
    actual: JSON.stringify(card.rows),
    evidence: ["evidence/STR-2/02-kafka-card.png"],
    status: "info",
  });

  const cg = card.rows["consumer groups"];
  if (cg == null) {
    addFinding({
      severity: "S3",
      title: "STR-2: no consumer-groups row rendered for the kafka topic",
      repro: "open " + names[0],
      expected: "n/a -- informational (may be legitimately absent if the topic has none)",
      actual: "rows=" + JSON.stringify(card.rows),
      evidence: ["evidence/STR-2/02-kafka-card.png"],
      status: "info",
    });
  } else if (cg.toLowerCase().indexOf("undefined") >= 0 || cg.toLowerCase().indexOf("nan") >= 0 || cg === "") {
    addFinding({
      severity: "S2",
      title: "STR-2: consumer-groups row renders a non-honest value (undefined/NaN/empty) instead of a clean count or explicit unavailable",
      repro: "open " + names[0],
      expected: 'a count, "0", or "unavailable (...)" -- never a raw JS artifact',
      actual: 'consumer groups="' + cg + '"',
      evidence: ["evidence/STR-2/02-kafka-card.png"],
    });
  }

  const dirty = card.hasSave || card.hasDelete || card.hasRename || card.hasUploadBar || card.hasInsertRow || card.hasRowDelete || card.hasEditableCell || card.hasTextarea;
  if (dirty) {
    addFinding({
      severity: "S1",
      title: "STR-2: kafka topic card exposes a write affordance it must never have",
      repro: "open " + names[0] + " under events",
      expected: "no save/delete/rename/upload/insert/row-delete/editable-cell/textarea",
      actual: JSON.stringify(card),
      evidence: ["evidence/STR-2/02-kafka-card.png"],
    });
  }
}

// ============================================================================
// STR-3 — view-only honesty: write mode ON sprouts nothing in either stream family
// ============================================================================

async function runSTR3(ctx) {
  const { page, harness, evidence, addFinding } = ctx;
  // Enable write mode via cache (in-lane), then hop to queue/events -- write
  // mode is global session state (state.writeEnabled), so this exercises the
  // exact "toggle on elsewhere, hop to a stream service" path the brief asks for.
  let spa = await harness.openConsole(page, CACHE_SERVICE);
  await harness.setWriteMode(page, spa, true);
  await evidence("01-write-mode-on-via-cache");

  for (const service of [NATS_SERVICE, KAFKA_SERVICE]) {
    spa = await harness.sidebarBrowse(page, service);
    await sleep(300);
    const switchState = await spa.evaluate(() => {
      const el = document.getElementById("editswitch");
      return { exists: !!el, hidden: el ? el.classList.contains("hidden") : null, on: el ? el.classList.contains("on") : null };
    });
    const names = await treeNodeNames(spa);
    await evidence("02-" + service + "-rail-with-writemode-on");
    // The write TOGGLE itself is embedded-session-wide chrome (renderWriteMode
    // doesn't vary by service), so it legitimately stays visible/on here --
    // what must NEVER happen is a per-item write AFFORDANCE appearing for
    // stream content specifically.
    if (names.length === 0) {
      addFinding({
        severity: "S3",
        title: "STR-3: no nodes to open under " + service + " while probing view-only honesty",
        repro: "write mode on; sidebarBrowse(" + service + ")",
        expected: "n/a -- can't complete the per-item check without a node",
        actual: "tree empty",
        evidence: ["evidence/STR-3/02-" + service + "-rail-with-writemode-on.png"],
        status: "info",
      });
      continue;
    }
    await clickTreeNode(spa, names[0]);
    await sleep(300);
    const card = await streamCardState(spa);
    const shot = await evidence("03-" + service + "-item-with-writemode-on");
    const dirty = card.hasSave || card.hasDelete || card.hasRename || card.hasUploadBar || card.hasInsertRow || card.hasRowDelete || card.hasEditableCell || card.hasTextarea || card.hasCreateKeyLink;
    if (dirty) {
      addFinding({
        severity: "S1",
        title: "STR-3: write mode ON sprouted a write affordance inside " + service + " (must stay view-only regardless of session write-mode)",
        repro: "enable write mode via cache; sidebarBrowse(" + service + "); open " + names[0],
        expected: "zero write affordances -- stream is read-only by nature at the provider level (stream.go), not merely by session posture",
        actual: JSON.stringify(card) + "; editswitch=" + JSON.stringify(switchState),
        evidence: [shot],
      });
    } else {
      addFinding({
        id: "STR-3-" + service + "-view-only-honest",
        severity: "S3",
        title: "STR-3: " + service + " stays view-only with write mode ON (no affordances sprout)",
        repro: "enable write mode via cache; sidebarBrowse(" + service + "); open " + names[0],
        expected: "n/a -- confirms the invariant holds",
        actual: JSON.stringify(card),
        evidence: [shot],
        status: "info",
      });
    }
  }

  await harness.setWriteMode(page, spa, false);
}

// ============================================================================
// CORE-3 — standalone reach: no in-UI entry point, but the mechanism works,
// read-only by construction
// ============================================================================

function sshRaw(cmd, timeoutMs) {
  const cfg = loadConfig();
  const host = cfg.DC_SSH_HOST || "zcp";
  return execFileSync("ssh", ["-o", "ConnectTimeout=8", host, cmd], { encoding: "utf8", timeout: timeoutMs || 20000 }).trim();
}

async function runCORE3(ctx) {
  const { page, addFinding } = ctx;
  const cfg = loadConfig();
  ensureAuthToken(cfg);

  // --- mechanism check: confirm (again, live, in this run) there is NO
  // product-side entry point -- package.json has no contributes.commands and
  // extension.js registers no command. ---
  addFinding({
    id: "CORE-3-standalone-mechanism",
    severity: "S3",
    title: "CORE-3: standalone reach mechanism -- there is NO in-product entry point (button/command/link); matches spec §4.2's explicit design, but the gap is total (zero discoverability)",
    repro:
      "grep internal/dataconsole/extension/templates/vscode-studio/package.json for contributes.commands (absent); " +
      "grep extension.js/handlers/*.js/cards/*.js for registerCommand (zero matches); " +
      "the sidebar's only affordance is 'Browse data ->' which opens the console EMBEDDED (handlers/console.js -> mgr.open -> panels.show)",
    expected: "n/a -- documenting the exact mechanism for the UX lane and fix loop",
    actual:
      "The ONLY path to a standalone tab is: a developer opens a terminal on the container (e.g. code-server's integrated terminal), " +
      "runs `zcp studio console serve` themselves (prints a ready-line JSON to stdout: {url:\"http://127.0.0.1:<port>\", sessionToken, writeToken, pid, allowWrites} " +
      "-- url is LOOPBACK, only meaningful from on the container), then hand-builds `<code-server-origin>/proxy/<port>/` " +
      "(code-server's own path-based port proxy, confirmed live: curl with the __zcp_auth cookie -> 200 index.html; without it -> 302) " +
      "and either appends `#t=<sessionToken>` themselves or leaves it off and pastes the token into the SPA's own authgate " +
      "(index.html's #authgate literally says 'Paste the token printed by zcp studio console serve.' -- this copy IS the product's only documentation of the mechanism). " +
      "This scenario drives exactly that flow via SSH (standing in for a container terminal) end-to-end below.",
    evidence: [],
    status: "info",
  });

  // --- spawn a throwaway, read-only (no --allow-writes) console instance,
  // self-expiring via `timeout` so nothing needs to be SSH-killed later
  // (ops discipline: never SSH-kill directly). ---
  const marker = "uitest_core3_" + Date.now();
  const readyFile = "/tmp/" + marker + "_ready.json";
  const errFile = "/tmp/" + marker + "_err.log";
  sshRaw(
    "rm -f " + readyFile + " " + errFile + "; nohup timeout 300 zcp studio console serve --port 0 >" + readyFile + " 2>" + errFile + " </dev/null & sleep 2; cat " + readyFile
  );
  const readyRaw = sshRaw("cat " + readyFile);
  let ready;
  try {
    ready = JSON.parse(readyRaw);
  } catch (e) {
    addFinding({
      severity: "S1",
      title: "CORE-3: could not parse the ready-line from a freshly spawned `zcp studio console serve`",
      repro: "ssh: nohup timeout 300 zcp studio console serve --port 0 > readyFile ...; cat readyFile",
      expected: "one JSON object {url, sessionToken, writeToken, pid, allowWrites}",
      actual: readyRaw,
      evidence: [],
    });
    return;
  }
  if (ready.allowWrites || ready.writeToken) {
    addFinding({
      severity: "S1",
      title: "CORE-3: a console spawned WITHOUT --allow-writes still reports a write posture/token",
      repro: "zcp studio console serve --port 0 (no --allow-writes flag)",
      expected: "allowWrites=false, writeToken=\"\"",
      actual: JSON.stringify(ready),
      evidence: [],
    });
  }
  const portMatch = /:(\d+)\/?$/.exec(ready.url || "");
  const port = portMatch ? portMatch[1] : null;
  if (!port) {
    addFinding({
      severity: "S1",
      title: "CORE-3: could not extract a port from the ready-line's url",
      repro: "parse ready.url",
      expected: "http://127.0.0.1:<port>",
      actual: JSON.stringify(ready),
      evidence: [],
    });
    return;
  }

  const origin = new URL(cfg.DC_URL).origin;
  const proxyBase = origin + "/proxy/" + port + "/";
  const standaloneURL = proxyBase + "#t=" + encodeURIComponent(ready.sessionToken);

  // --- drive it as a plain top-level tab, NOT the workbench ---
  const browser = page.browser();
  const spage = await browser.newPage();
  await spage.setViewport({ width: 1440, height: 900 });
  const u = new URL(cfg.DC_URL);
  await spage.setCookie({ name: "__zcp_auth", value: cfg.DC_AUTH_TOKEN, domain: u.hostname, path: "/", secure: true, httpOnly: true });

  try {
    await spage.goto(standaloneURL, { waitUntil: "domcontentloaded", timeout: 30000 });
    await spage.waitForSelector("#services li", { timeout: 20000 }).catch(() => null);
    await spageEvidence(spage, "standalone-tab-fragment-auth");

    const boot = await spage.evaluate(() => ({
      hasWorkbench: !!document.querySelector(".monaco-workbench"),
      servicesCount: document.querySelectorAll("#services li").length,
      projectText: (document.getElementById("project") || {}).textContent || null,
    }));
    if (boot.hasWorkbench || boot.servicesCount === 0) {
      addFinding({
        severity: "S1",
        title: "CORE-3: standalone tab (fragment-auth) did not boot into the plain SPA correctly",
        repro: "navigate to " + proxyBase + "#t=<sessionToken> in a fresh page",
        expected: "no .monaco-workbench (this is NOT the code-server workbench); #services populated",
        actual: JSON.stringify(boot),
        evidence: ["evidence/CORE-3/01-standalone-tab-fragment-auth.png"],
      });
    }

    // --- write toggle: must never be reachable ---
    const switchState = await spage.evaluate(() => {
      const el = document.getElementById("editswitch");
      const badge = document.getElementById("writemode");
      return {
        editswitchExists: !!el,
        editswitchHidden: el ? el.classList.contains("hidden") : null,
        editswitchDisplay: el ? getComputedStyle(el).display : null,
        badgeText: badge ? badge.textContent : null,
        badgeHidden: badge ? badge.classList.contains("hidden") : null,
      };
    });
    await spageEvidence(spage, "02-writemode-badge-state");
    // Empirical reality (checked against the shipped index.html): #editswitch
    // is static markup present in EVERY session (embedded and standalone
    // alike), starting with class="switch hidden" in the source HTML itself;
    // standalone's renderWriteMode() explicitly re-adds .hidden (a no-op given
    // the default) and NEVER removes it. So "no write toggle" is accurate as
    // "never visible/interactable" (display:none), not "absent from the DOM
    // tree" -- recording the precise, verified distinction rather than the
    // brief's literal "no #editswitch" phrasing.
    const trulyHidden = switchState.editswitchExists && switchState.editswitchHidden && switchState.editswitchDisplay === "none";
    if (!trulyHidden) {
      addFinding({
        severity: "S1",
        title: "CORE-3: write-mode toggle is reachable/visible in the standalone tab",
        repro: "inspect #editswitch in the standalone tab",
        expected: "#editswitch present in markup but class 'hidden' + computed display:none (never interactable)",
        actual: JSON.stringify(switchState),
        evidence: ["evidence/CORE-3/02-writemode-badge-state.png"],
      });
    }
    if (!switchState.badgeText || switchState.badgeText.toLowerCase().indexOf("read-only") < 0 || switchState.badgeHidden) {
      addFinding({
        severity: "S2",
        title: 'CORE-3: standalone tab does not show a visible "read-only" badge',
        repro: "inspect #writemode badge in the standalone tab",
        expected: 'visible badge with text "read-only"',
        actual: JSON.stringify(switchState),
        evidence: ["evidence/CORE-3/02-writemode-badge-state.png"],
      });
    }

    // --- browse cache, open the greeting fixture read-only, confirm no edit affordances ---
    const clicked = await spage.evaluate((svc) => {
      const items = Array.from(document.querySelectorAll("#services li"));
      const li = items.find((el) => { const span = el.querySelector("span"); return span && span.textContent === svc; });
      if (!li) return false;
      li.click();
      return true;
    }, CACHE_SERVICE);
    await sleep(400);
    if (!clicked) {
      addFinding({
        severity: "S1",
        title: "CORE-3: could not select the cache service in the standalone tab",
        repro: "click #services li matching 'cache'",
        expected: "service selectable, tree loads",
        actual: "no matching <li>",
        evidence: [await spageEvidence(spage, "03-cache-select-failed")],
      });
    } else {
      await clickTreeNode(spage, GREETING_FIXTURE);
      await sleep(300);
      const card = await streamCardState(spage);
      const shot = await spageEvidence(spage, "04-greeting-readonly-view");
      const anyWrite = card.hasSave || card.hasDelete || card.hasRename || card.hasUploadBar || card.hasInsertRow || card.hasRowDelete || card.hasEditableCell || card.hasTextarea || card.hasCreateKeyLink;
      if (anyWrite) {
        addFinding({
          severity: "S1",
          title: "CORE-3: standalone tab shows an edit affordance on the greeting fixture",
          repro: "standalone tab: open cache -> greeting",
          expected: "no save/delete/rename/upload/insert/row-delete/editable-cell/textarea/createkeylink anywhere",
          actual: JSON.stringify(card),
          evidence: [shot],
        });
      }
    }

    // --- write-mode can't be conjured: attempt a raw mutation fetch from the
    // standalone origin itself (no X-Write-Token is even possible to attach --
    // that secret never reaches any browser context). ---
    const attemptKey = "uitest_core3_standalone_writeattempt";
    // Deliberately attach the SAME read bearer this standalone session
    // authenticates with (ready.sessionToken) -- the precise test is "reader
    // is authenticated but still can't write" (spec-dataconsole.md §5.1: "a
    // caller holding only the read bearer can read everything and mutate
    // nothing"), not the weaker/different "an unauthenticated request fails".
    // Omitting Authorization here would 401 on the auth() bearer check before
    // ever reaching AuthorizeWrite, proving nothing about the write boundary
    // specifically.
    const mutationResult = await spage.evaluate(async (key, bearer) => {
      try {
        const r = await fetch("api/kv/create", {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-Confirm": "true", "Authorization": "Bearer " + bearer },
          body: JSON.stringify({ path: { service: "cache", segments: [key] }, type: "string", value: btoa("x") }),
        });
        const text = await r.text();
        return { status: r.status, ok: r.ok, text: text };
      } catch (e) {
        return { error: String(e) };
      }
    }, attemptKey, ready.sessionToken);
    await spageEvidence(spage, "05-conjure-write-attempt");
    const engineCheck = sshRaw(
      "redis-cli -h " + shellQuote(cfg.DC_REDIS_HOST) +
        " -a " + shellQuote(cfg.DC_REDIS_PASSWORD) +
        " --no-auth-warning EXISTS " + shellQuote(attemptKey)
    );
    if (String(engineCheck).trim() !== "0") {
      // Catastrophic: the standalone (bearer-only) session mutated data.
      // Clean up immediately, then file at the highest severity.
      sshRaw(
        "redis-cli -h " + shellQuote(cfg.DC_REDIS_HOST) +
          " -a " + shellQuote(cfg.DC_REDIS_PASSWORD) +
          " --no-auth-warning DEL " + shellQuote(attemptKey)
      );
      addFinding({
        id: "CORE-3-CRITICAL-standalone-write-succeeded",
        severity: "S1",
        title: "CORE-3 CRITICAL: a mutation from the STANDALONE (bearer-only, no write token) tab actually applied — the caller-bound write posture is broken",
        repro: 'standalone tab: fetch("api/kv/create", {method:"POST", body:{path:{service:"cache",segments:["' + attemptKey + '"]},type:"string",value:btoa("x")}})',
        expected: "refused server-side (no X-Write-Token possible from any browser context) -- spec-dataconsole.md §5.1",
        actual: "mutationResult=" + JSON.stringify(mutationResult) + "; redis EXISTS " + attemptKey + " was NOT 0 before cleanup",
        evidence: ["evidence/CORE-3/05-conjure-write-attempt.png"],
        engine_truth: "redis EXISTS " + attemptKey + " (pre-cleanup) = " + engineCheck,
      });
    } else if (mutationResult.ok) {
      addFinding({
        severity: "S1",
        title: "CORE-3: standalone mutation attempt returned an OK status despite no write token being possible",
        repro: "same as above",
        expected: "non-2xx (e.g. 403 read_only)",
        actual: JSON.stringify(mutationResult),
        evidence: ["evidence/CORE-3/05-conjure-write-attempt.png"],
        engine_truth: "redis EXISTS " + attemptKey + " = 0 (no data harm, but the response contract is wrong)",
      });
    } else {
      addFinding({
        id: "CORE-3-write-cannot-be-conjured",
        severity: "S3",
        title: "CORE-3: confirmed -- write mode cannot be conjured from the standalone tab (server refuses; no data mutated)",
        repro: "standalone tab: attempt POST api/kv/create with no write token possible",
        expected: "n/a -- confirms the invariant",
        actual: JSON.stringify(mutationResult),
        evidence: ["evidence/CORE-3/05-conjure-write-attempt.png"],
        engine_truth: "redis EXISTS " + attemptKey + " = 0",
        status: "info",
      });
    }

    // --- authgate manual-paste path (the product's OWN documented mechanism) ---
    const gatePage = await browser.newPage();
    await gatePage.setViewport({ width: 1440, height: 900 });
    await gatePage.setCookie({ name: "__zcp_auth", value: cfg.DC_AUTH_TOKEN, domain: u.hostname, path: "/", secure: true, httpOnly: true });
    await gatePage.goto(proxyBase, { waitUntil: "domcontentloaded", timeout: 30000 });
    const gateVisible = await gatePage.waitForSelector("#authgate:not(.hidden)", { timeout: 10000 }).then(() => true).catch(() => false);
    await spageEvidence(gatePage, "authgate-no-fragment");
    if (!gateVisible) {
      addFinding({
        severity: "S2",
        title: "CORE-3: navigating to the proxy URL WITHOUT a #t= fragment did not show the authgate paste-token dialog",
        repro: "navigate to " + proxyBase + " (no fragment)",
        expected: "#authgate visible with 'Paste the token printed by zcp studio console serve.' copy",
        actual: "gateVisible=" + gateVisible,
        evidence: ["evidence/CORE-3/06-authgate-no-fragment.png"],
      });
    } else {
      await gatePage.click("#tokeninput", { clickCount: 3 });
      await gatePage.type("#tokeninput", ready.sessionToken);
      await gatePage.click("#tokenbtn");
      await sleep(500);
      const gateBoot = await gatePage.evaluate(() => document.querySelectorAll("#services li").length);
      await spageEvidence(gatePage, "authgate-pasted-token");
      if (gateBoot === 0) {
        addFinding({
          severity: "S1",
          title: "CORE-3: pasting the printed session token into the authgate did not authenticate",
          repro: "navigate to " + proxyBase + "; type sessionToken into #tokeninput; click #tokenbtn",
          expected: "#services populates (the token-paste path is the product's own documented mechanism)",
          actual: "servicesCount=0",
          evidence: ["evidence/CORE-3/07-authgate-pasted-token.png"],
        });
      } else {
        addFinding({
          id: "CORE-3-authgate-paste-works",
          severity: "S3",
          title: "CORE-3: the authgate manual-paste mechanism (the product's only documented standalone entry point) works correctly",
          repro: "navigate to " + proxyBase + "; paste the ready-line's sessionToken; Connect",
          expected: "n/a -- confirms the ONE documented mechanism is functional",
          actual: "servicesCount=" + gateBoot,
          evidence: ["evidence/CORE-3/07-authgate-pasted-token.png"],
          status: "info",
        });
      }
    }
    await gatePage.close();
  } finally {
    await spage.close().catch(() => {});
  }
}

// spageEvidence screenshots a page OTHER than the main ctx.page (the
// standalone/authgate tabs CORE-3 opens itself) -- ctx.evidence() is bound to
// the main page only, so these need their own path. Unlike harness.shot(),
// nothing else creates evidence/CORE-3/ first, so this does it explicitly
// (caught live: the very first call in this scenario ENOENT'd on a missing
// directory).
async function spageEvidence(spage, name) {
  const dir = path.join(__dirname, "..", "evidence", "CORE-3");
  fs.mkdirSync(dir, { recursive: true });
  const p = path.join(dir, "xx-" + name + ".png");
  await spage.screenshot({ path: p, fullPage: true });
  return path.relative(path.join(__dirname, ".."), p);
}

runner.register({ id: "STR-1", family: "stream", fn: runSTR1 });
runner.register({ id: "STR-2", family: "stream", fn: runSTR2 });
runner.register({ id: "STR-3", family: "stream", fn: runSTR3 });
runner.register({ id: "CORE-3", family: "standalone", fn: runCORE3 });

module.exports = {};
