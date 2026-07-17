"use strict";

// UI-drive harness for the Managed Data Console — drives the REAL assembled UI
// (code-server -> VS Code workbench -> nested webview iframes -> the console SPA)
// with puppeteer-core against Karel's live dev container. Prior unit/jsdom/HTTP
// testing exercised the SPA's logic and the server's routes separately; it never
// caught bugs that only exist in the assembled whole (nested webview bridging,
// the native confirm-modal round trip, a sidebar re-browse rebinding the broker).
// This harness drives exactly that whole, the same way a human would.
//
// Frame topology (proven live, see uitest/README.md): the sidebar ("Managed
// Data" activity bar view) and the console panel (the SPA) are each their own
// VS Code WebviewPanel/WebviewView, each rendered as a chain of nested SAME-
// ORIGIN iframes (outer VS Code webview host iframe -> inner content iframe ->
// the actual document). page.frames() flattens the whole tree, so every helper
// here finds its target frame by PROBING CONTENT (a selector only that frame's
// document has), never by index or a fixed nesting depth — the exact nesting
// depth is a VS Code implementation detail we don't want to pin.

const fs = require("fs");
const path = require("path");
const puppeteer = require("puppeteer-core");
const { loadConfig, ensureAuthToken } = require("./config");

const ROOT = path.join(__dirname, "..");
const EVIDENCE_DIR = path.join(ROOT, "evidence");

const VIEWPORT = { width: 1440, height: 900 };
const NAV_TIMEOUT_MS = 45000;
const FRAME_TIMEOUT_MS = 20000;
const MODAL_TIMEOUT_MS = 15000;
const TREE_SETTLE_TIMEOUT_MS = 20000;
const TOAST_DRAIN_TIMEOUT_MS = 4000;

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function headful() {
  return process.env.HEADFUL === "1";
}

// ---------- evidence (screenshots) ----------
// The runner calls setScenario(id) before invoking a scenario's fn, so internal
// assert-fail screenshots (a timeout inside setWriteMode, a missing frame, ...)
// land in the right evidence/<scenario>/ folder even though helpers like
// setWriteMode/clickService don't take a scenario id in their own signature —
// only the scenario-authored shot(page, scenario, name) calls do, per contract.
let _currentScenario = "_shared";
function setScenario(id) {
  _currentScenario = id || "_shared";
}
function currentScenario() {
  return _currentScenario;
}

const _shotCounters = Object.create(null);

// shot takes a full-page screenshot to evidence/<scenario>/NN-name.png, NN
// auto-incrementing per scenario so evidence sorts in the order it was taken.
async function shot(page, scenario, name) {
  const dir = path.join(EVIDENCE_DIR, scenario);
  fs.mkdirSync(dir, { recursive: true });
  const n = (_shotCounters[scenario] = (_shotCounters[scenario] || 0) + 1);
  const nn = String(n).padStart(2, "0");
  const safeName = String(name).replace(/[^a-z0-9._-]+/gi, "-");
  const file = path.join(dir, nn + "-" + safeName + ".png");
  await page.screenshot({ path: file, fullPage: true });
  return path.relative(ROOT, file);
}

// failShot is the internal "something timed out — grab evidence before throwing"
// helper used by every wait in this file, so a G1 failure always leaves a picture
// of what the browser actually showed, not just a stack trace.
async function failShot(page, name) {
  try {
    return await shot(page, currentScenario(), "FAIL-" + name);
  } catch (_) {
    return null; // best effort — never let evidence-capture mask the real error
  }
}

// ---------- frame discovery ----------
// findFrameByProbe polls page.frames() until one frame's document matches every
// selector in `selectors` (AND), or throws after timeoutMs. Nested webview
// iframes attach asynchronously (their own content document loads after the
// outer iframe element exists), so a single page.frames() snapshot right after a
// click is not enough — this retries.
async function findFrameByProbe(page, selectors, timeoutMs) {
  const sels = Array.isArray(selectors) ? selectors : [selectors];
  const deadline = Date.now() + timeoutMs;
  let lastErr = null;
  while (Date.now() < deadline) {
    for (const f of page.frames()) {
      try {
        const ok = await f.evaluate((list) => list.every((s) => !!document.querySelector(s)), sels);
        if (ok) return f;
      } catch (e) {
        lastErr = e; // detached / mid-navigation / cross-origin — keep scanning
      }
    }
    await sleep(150);
  }
  throw new Error(
    "findFrameByProbe: no frame matched [" + sels.join(", ") + "] within " + timeoutMs + "ms" +
      (lastErr ? " (last probe error: " + lastErr.message + ")" : "")
  );
}

// escAttr escapes a value for embedding inside a double-quoted CSS attribute
// selector ([data-service="..."]) — service hostnames are simple identifiers in
// practice, but this stays correct if one ever isn't.
function escAttr(v) {
  return String(v).replace(/[\\"]/g, "\\$&");
}

// ---------- connect ----------
// connect launches Chrome, fetches/caches the nginx gate token, sets the auth
// cookie BEFORE navigating (skips the login form entirely — proven live), and
// waits for the VS Code workbench shell to be present.
async function connect() {
  const cfg = loadConfig();
  const token = ensureAuthToken(cfg);
  const url = cfg.DC_URL;
  if (!url) throw new Error("harness.connect: DC_URL missing from local.config.json");
  if (!cfg.DC_CHROME_PATH || !fs.existsSync(cfg.DC_CHROME_PATH)) {
    throw new Error("harness.connect: DC_CHROME_PATH missing or not found on disk: " + cfg.DC_CHROME_PATH);
  }

  const browser = await puppeteer.launch({
    executablePath: cfg.DC_CHROME_PATH,
    headless: headful() ? false : "new",
    defaultViewport: VIEWPORT,
    args: ["--window-size=" + VIEWPORT.width + "," + VIEWPORT.height],
  });
  const page = await browser.newPage();
  await page.setViewport(VIEWPORT);

  const u = new URL(url);
  await page.setCookie({
    name: "__zcp_auth",
    value: token,
    domain: u.hostname,
    path: "/",
    secure: true,
    httpOnly: true,
  });

  await page.goto(url, { waitUntil: "domcontentloaded", timeout: NAV_TIMEOUT_MS });
  try {
    await page.waitForSelector(".monaco-workbench", { timeout: NAV_TIMEOUT_MS });
  } catch (e) {
    await failShot(page, "connect-no-workbench");
    await browser.close();
    throw new Error("harness.connect: .monaco-workbench never appeared (bad/stale auth cookie? cold boot?): " + e.message);
  }
  return { browser, page };
}

// ---------- sidebar ----------
// openSidebar returns the "Managed Data" sidebar's content frame (probed by
// .zs-rowhead — a service row). Idempotent by design: VS Code's activity bar
// TOGGLES a view closed on a second click of its own icon, so this probes first
// and only clicks the icon if the sidebar isn't already open.
async function openSidebar(page) {
  try {
    return await findFrameByProbe(page, [".zs-rowhead"], 1500);
  } catch (_) {
    // not open yet — fall through and click it open
  }
  const iconSel = '.activitybar a[aria-label="Managed Data"]';
  try {
    await page.waitForSelector(iconSel, { timeout: FRAME_TIMEOUT_MS });
    await page.click(iconSel);
  } catch (e) {
    await failShot(page, "open-sidebar-icon-missing");
    throw new Error("openSidebar: activity bar item not found/clickable: " + e.message);
  }
  try {
    return await findFrameByProbe(page, [".zs-rowhead"], FRAME_TIMEOUT_MS);
  } catch (e) {
    await failShot(page, "open-sidebar-frame-missing");
    throw e;
  }
}

// waitServiceSwitchedAndTreeSettled blocks until BOTH: (1) the rail's active
// service actually equals `service` (frame-ready per findFrameByProbe above
// only proves SOME service has ever loaded — not that THIS click's switch
// landed; every fan-out scenario file independently rediscovered this and
// wrote its own copy of this exact wait, e.g. document.js's
// waitForActiveService, core.js's inline post-sidebarBrowse block, tabular.js's
// openNodeAttempt — folded here so every caller of browseVia gets it for free)
// and (2) #tree has actually settled: no .state.loading anywhere inside it
// (a root load, or a lone-container auto-drill's nested expand, or a
// write-mode-triggered refresh can all leave one mid-flight), AND either a
// real .node rendered or the honest .state.empty placeholder landed (never
// caught mid-clear with neither). Scenarios may still layer their own
// additional waits on top (e.g. for a SEPARATE postMessage like
// dataconsole-write-mode) — this only removes the need to hand-roll the
// service-switch + tree-load race every fan-out file kept rediscovering.
async function waitServiceSwitchedAndTreeSettled(page, frame, service) {
  try {
    await frame.waitForFunction(
      (svc) => {
        const li = document.querySelector("#services li.active span");
        return !!li && li.textContent === svc;
      },
      { timeout: FRAME_TIMEOUT_MS },
      service
    );
  } catch (e) {
    await failShot(page, "active-service-mismatch-" + service);
    throw new Error(
      'browseVia: rail never switched to active service "' + service + '" within ' + FRAME_TIMEOUT_MS + "ms: " + e.message
    );
  }
  try {
    await frame.waitForFunction(
      () => {
        if (document.querySelector("#tree .state.loading")) return false;
        return document.querySelectorAll("#tree .node").length > 0 || !!document.querySelector("#tree .state.empty");
      },
      { timeout: TREE_SETTLE_TIMEOUT_MS }
    );
  } catch (e) {
    await failShot(page, "tree-not-settled-" + service);
    throw new Error(
      'browseVia: #tree for "' + service + '" never settled (still .state.loading, or neither a .node nor .state.empty landed) within ' +
        TREE_SETTLE_TIMEOUT_MS + "ms: " + e.message
    );
  }
}

// browseVia is the shared "click a sidebar Browse-data row, wait for the console
// SPA frame" core. openConsole and sidebarBrowse are the SAME action in the real
// UI (VS Code's panel manager dedupes by key — the first open and every later
// re-browse all go through this one click), so they share one implementation;
// both names are exported because scenarios want to say which INTENT they mean
// (first open vs. deliberately re-browsing to exercise the reveal path).
async function browseVia(page, service) {
  const sidebar = await openSidebar(page);
  const sel = '[data-action="openConsole"][data-service="' + escAttr(service) + '"]';
  try {
    await sidebar.waitForSelector(sel, { timeout: FRAME_TIMEOUT_MS });
    await sidebar.click(sel);
  } catch (e) {
    await failShot(page, "browse-btn-missing-" + service);
    throw new Error('browseVia: sidebar "Browse data" button not found for service "' + service + '": ' + e.message);
  }
  let frame;
  try {
    // #rail/#topbar are STATIC markup — present as soon as the iframe's HTML
    // loads, well before the app is interactive. The app only becomes
    // "embedded" (and un-hides #editswitch, populates the rail, ...) after the
    // async dc-ready -> dataconsole-init postMessage handshake round-trips
    // through the nested webview bridge to the extension host and back — that
    // can lag the iframe load by a second or more. #services li (populated by
    // renderServices(), called from start(), called only after that handshake)
    // is the earliest reliable "actually loaded" signal; probing for it here
    // means every caller of browseVia gets a frame that's truly ready to drive.
    frame = await findFrameByProbe(page, ["#rail", "#topbar", "#services li"], FRAME_TIMEOUT_MS);
  } catch (e) {
    await failShot(page, "spa-frame-missing-" + service);
    throw e;
  }
  await waitServiceSwitchedAndTreeSettled(page, frame, service);
  return frame;
}

// openConsole opens (or reveals) the Data Console panel deep-linked to `service`
// and returns its SPA frame.
async function openConsole(page, service) {
  return browseVia(page, service);
}

// sidebarBrowse re-clicks the SIDEBAR "Browse data" button for `service` — the
// reveal path. Mechanically identical to openConsole; kept as a distinct export
// so a scenario testing the re-browse regression can say so at the call site.
async function sidebarBrowse(page, service) {
  return browseVia(page, service);
}

// spaFrame finds the console SPA's current frame by content probe (#rail +
// #topbar), without clicking anything.
async function spaFrame(page) {
  return findFrameByProbe(page, ["#rail", "#topbar", "#services li"], FRAME_TIMEOUT_MS);
}

// ---------- write mode ----------
// clickMonacoDialogButton clicks the native VS Code modal button with exact text
// `label` ("Cancel" or "Enable writes") in the MAIN frame — the dialog is host
// chrome, never webview content.
async function clickMonacoDialogButton(page, label) {
  const handle = await page.waitForFunction(
    (want) => {
      const btns = Array.from(document.querySelectorAll(".monaco-dialog-box a.monaco-button"));
      return btns.find((b) => (b.textContent || "").trim() === want) || false;
    },
    { timeout: MODAL_TIMEOUT_MS },
    label
  );
  const el = handle.asElement();
  if (!el) throw new Error('clickMonacoDialogButton: button "' + label + '" not found in .monaco-dialog-box');
  await el.click();
}

// setWriteMode drives the write-mode toggle to `on`, including the native
// confirm-modal round trip when enabling. No-ops if already at the desired
// state (self-heals ambient state from a prior session without needing a
// separate "reset" call — every scenario, including fan-out ones, should open
// by calling this to guarantee a known starting posture).
async function setWriteMode(page, frame, on) {
  const desired = !!on;
  const current = await frame.evaluate(() => {
    const el = document.getElementById("editswitch");
    return !!(el && el.classList.contains("on"));
  });
  if (current === desired) return;

  try {
    await frame.waitForSelector("#editswitch:not(.hidden)", { visible: true, timeout: FRAME_TIMEOUT_MS });
    await frame.click("#editswitch");
  } catch (e) {
    await failShot(page, "write-mode-switch-not-clickable");
    throw new Error("setWriteMode: #editswitch not visible/clickable (frame not fully loaded?): " + e.message);
  }

  if (desired) {
    try {
      await page.waitForSelector(".monaco-dialog-box", { visible: true, timeout: MODAL_TIMEOUT_MS });
    } catch (e) {
      await failShot(page, "write-mode-modal-missing");
      throw new Error("setWriteMode: native confirm modal (.monaco-dialog-box) never appeared: " + e.message);
    }
    await shot(page, currentScenario(), "write-mode-modal-before-confirm");
    try {
      await clickMonacoDialogButton(page, "Enable writes");
    } catch (e) {
      await failShot(page, "write-mode-modal-button-missing");
      throw e;
    }
  }

  try {
    await frame.waitForFunction(
      (want) => {
        const el = document.getElementById("editswitch");
        return !!el && el.classList.contains("on") === want;
      },
      { timeout: MODAL_TIMEOUT_MS },
      desired
    );
  } catch (e) {
    await failShot(page, "write-mode-not-reflected-" + desired);
    throw new Error("setWriteMode: .switch.on did not settle to " + desired + ": " + e.message);
  }
}

// ---------- in-SPA rail switch ----------
// clickService clicks the rail's #services li for `service` — the IN-SPA switch
// (as opposed to sidebarBrowse, which re-enters through the sidebar). The
// rendered <li> carries NO data-service attribute (checked against the shipped
// app.js — it's plain <span>hostname</span>), so this matches by the first
// <span>'s text content and dispatches a real DOM click (which the SPA wires via
// li.onclick, so a synthetic click() call triggers it the same as a mouse click).
async function clickService(frame, service) {
  const clicked = await frame.evaluate((svc) => {
    const items = Array.from(document.querySelectorAll("#services li"));
    const li = items.find((el) => {
      const span = el.querySelector("span");
      return span && span.textContent === svc;
    });
    if (!li) return false;
    li.click();
    return true;
  }, service);
  if (!clicked) {
    try {
      const page = frame.page ? frame.page() : null;
      if (page) await failShot(page, "click-service-not-found-" + service);
    } catch (_) {
      /* best effort */
    }
    throw new Error('clickService: no #services li found with text "' + service + '"');
  }
}

// ---------- toast ----------
// waitToast races a .toast(.good|.bad|.warn) appearing against timeoutMs,
// returning {kind, text} or null on timeout (never throws — a missing toast is
// itself scenario-meaningful, so callers decide whether that's a finding).
// Reads the FIRST toast in DOM order: toasts self-remove after 2.6s, so as long
// as scenario steps are spaced out a stale toast won't shadow the next one.
async function waitToast(frame, timeoutMs) {
  const t = timeoutMs || FRAME_TIMEOUT_MS;
  try {
    const handle = await frame.waitForFunction(
      () => {
        const el = document.querySelector(".toast.good, .toast.bad, .toast.warn");
        if (!el) return false;
        const cls = el.className || "";
        const kind = cls.indexOf("bad") >= 0 ? "bad" : cls.indexOf("warn") >= 0 ? "warn" : "good";
        return { kind: kind, text: el.textContent || "" };
      },
      { timeout: t }
    );
    return await handle.jsonValue();
  } catch (_) {
    return null;
  }
}

// drainToast polls until no .toast(.good|.bad|.warn) remains in frame's DOM, or
// timeoutMs elapses (best effort — never throws; a toast that outlives the cap
// just means the caller proceeds with it still fading). Toasts self-remove
// after 2.6s (README gotcha #8): chaining two mutations closer together than
// that lets the SECOND waitToast() read the FIRST, still-fading toast instead
// of its own. Every fan-out scenario file independently reinvented this
// (tabular.js's drainToast, kv-object.js's waitToastGone, document.js's
// fixed-sleep drainToast) — this is the shared version new scenarios should
// call between chained mutations instead of re-rolling another local copy.
async function drainToast(frame, timeoutMs) {
  const deadline = Date.now() + (timeoutMs || TOAST_DRAIN_TIMEOUT_MS);
  while (Date.now() < deadline) {
    let present;
    try {
      present = await frame.evaluate(() => !!document.querySelector(".toast.good, .toast.bad, .toast.warn"));
    } catch (_) {
      return; // frame navigated/detached mid-poll -- nothing left to drain
    }
    if (!present) return;
    await sleep(150);
  }
}

// ---------- geometry ----------
// bbox returns selector's getBoundingClientRect() snapshot, or null if the
// selector doesn't match anything currently in frame. Later layout-regression
// scenarios diff these across states.
async function bbox(frame, selector) {
  return frame.evaluate((sel) => {
    const el = document.querySelector(sel);
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: r.x, y: r.y, width: r.width, height: r.height, top: r.top, left: r.left, right: r.right, bottom: r.bottom };
  }, selector);
}

module.exports = {
  connect,
  openSidebar,
  openConsole,
  sidebarBrowse,
  spaFrame,
  setWriteMode,
  clickService,
  waitToast,
  drainToast,
  shot,
  bbox,
  // exposed for the runner + advanced scenarios; not part of the day-to-day API
  setScenario,
  currentScenario,
  findFrameByProbe,
  clickMonacoDialogButton,
  sleep,
};
