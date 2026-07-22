// Welcome auth-bridge local E2E runner (docs/spec-welcome-mode.md §4).
//
// Drives the whole bridge loop against a REAL code-server:
//   authorize click → host gate → broadcast trigger → gui-harness receiver
//   validates + acks → webview relays ack → host validates origin → phase line
// and asserts the observable outcome, per MODE:
//   ack    → the receiver acks; the tile phase becomes "Opening authorization…"
//   silent → the receiver never acks; the tile falls back to "…not detected…"
//
// The gui-harness is served from localhost (welcome.js trusts http://localhost
// on any port for inbound acks, and the container nginx frame-ancestors allows
// http://localhost:* to embed code-server). The code-server auth cookie is
// Partitioned, so the session is established INSIDE the embedded iframe (under
// the localhost top-level), not carried from a first-party visit.

import http from "node:http";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import puppeteer from "puppeteer-core";

const HARNESS_DIR = path.dirname(fileURLToPath(import.meta.url));
const ARTIFACTS_DIR = path.join(HARNESS_DIR, "artifacts");
const PACKAGE_JSON_PATH = path.join(HARNESS_DIR, "..", "..", "internal", "content", "templates", "vscode-bootstrap-package.json");

// ---- config -------------------------------------------------------------
const CS_URL = process.env.ZCP_CS_URL || "";
const CS_PASSWORD = process.env.ZCP_CS_PASSWORD || "";
const AGENT = (process.env.AGENT || "claude-code").trim();
const MODE = process.env.MODE === "silent" ? "silent" : "ack";
const HEADLESS = process.env.HEADLESS !== "false";
// ackDelayMs/clockSkewMs (docs/spec-welcome-mode.md §4): forwarded to the
// gui-harness as query params so the orchestrator can prove the host's 12s
// ACK_TIMEOUT_MS window covers the deployed FE's real post-dialog-dispatch
// ack latency (e.g. HARNESS_ACK_DELAY_MS=6000) alongside a skewed receiver
// clock (e.g. HARNESS_CLOCK_SKEW_MS=1000) — unset/0 (the default) leaves the
// existing immediate-ack behavior unchanged.
const ACK_DELAY_MS = Number(process.env.HARNESS_ACK_DELAY_MS || 0);
const CLOCK_SKEW_MS = Number(process.env.HARNESS_CLOCK_SKEW_MS || 0);

// Exact phase strings the webview renders (welcome.html AUTH_PHASE_TEXT) — the
// assertions below match these verbatim. If welcome.html's copy changes, this
// must change with it (contract-mirror, see README).
const PHASE_CONTACTING = "Contacting the Zerops dashboard…";
const PHASE_DIALOG_OPENING = "Opening authorization in the Zerops panel…";
const PHASE_NO_DASHBOARD = "Zerops dashboard not detected — use Terminal login";

// The exact six keys a credential-free trigger carries (welcome.js handleAuthorize).
const EXPECTED_PAYLOAD_KEYS = ["channel", "version", "type", "agentType", "eventId", "createdAt"];

if (!CS_URL) fail("ZCP_CS_URL is required (e.g. https://zcp-xxxx-8080.prg1.zerops.app)");
if (!CS_PASSWORD) fail("ZCP_CS_PASSWORD is required (the code-server VSCODE_PASSWORD)");
const CS_ORIGIN = new URL(CS_URL).origin;

function fail(msg) { console.error("FATAL: " + msg); process.exit(2); }

// ---- tiny static file server (no deps) ----------------------------------
function serveHarness(dir) {
  const server = http.createServer((req, res) => {
    const rel = decodeURIComponent((req.url || "/").split("?")[0]);
    const name = rel === "/" ? "gui-harness.html" : rel.replace(/^\/+/, "");
    const file = path.join(dir, name);
    if (!file.startsWith(dir) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
      res.writeHead(404); res.end("not found"); return;
    }
    const type = file.endsWith(".html") ? "text/html; charset=utf-8"
      : file.endsWith(".js") ? "text/javascript; charset=utf-8" : "application/octet-stream";
    res.writeHead(200, { "content-type": type });
    fs.createReadStream(file).pipe(res);
  });
  return new Promise((resolve) => {
    // Bind to loopback; the browser is pinned to localhost -> 127.0.0.1 via a
    // host-resolver rule below so the page origin is exactly http://localhost.
    server.listen(0, "127.0.0.1", () => {
      const port = server.address().port;
      resolve({ base: `http://localhost:${port}`, close: () => server.close() });
    });
  });
}

// ---- generic bounded poll -----------------------------------------------
async function poll(label, fn, { timeout = 15000, interval = 200 } = {}) {
  const deadline = Date.now() + timeout;
  let lastErr = null;
  for (;;) {
    try {
      const v = await fn();
      if (v) return v;
    } catch (err) { lastErr = err; }
    if (Date.now() > deadline) {
      throw new Error(`[timeout] ${label} (after ${timeout}ms)` + (lastErr ? ` — last error: ${lastErr.message}` : ""));
    }
    await sleep(interval);
  }
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function frameByOrigin(page, origin) {
  return page.frames().find((f) => {
    try { return new URL(f.url()).origin === origin; } catch (_) { return false; }
  });
}

// Find any frame (any depth) whose document contains `selector`. Frames under
// the code-server origin are all evaluable regardless of the localhost top.
async function frameWithSelector(page, selector) {
  for (const f of page.frames()) {
    try {
      const has = await f.evaluate((s) => !!document.querySelector(s), selector);
      if (has) return f;
    } catch (_) { /* frame detached or not ready */ }
  }
  return null;
}

// Authenticate the code-server session inside a frame that is showing the
// custom /zcp-login page (input#token → navigates to /zcp-auth/<token>).
async function loginInFrame(frame, password) {
  const input = await frame.$("#token, input[type=password], input.password, input");
  if (!input) return false;
  await input.click({ clickCount: 3 });
  await input.type(password, { delay: 5 });
  await input.press("Enter");
  return true;
}

// Bring a frame's document into focus so real keyboard events reach it.
async function focusFrame(frame) {
  const target = (await frame.$(".monaco-workbench")) || (await frame.$("body"));
  if (target) { try { await target.click(); return; } catch (_) { /* fall through */ } }
}

// ---- main ---------------------------------------------------------------
const log = [];
const record = (line) => { log.push(line); console.log(line); };

let browser, srv, page;
try {
  fs.mkdirSync(ARTIFACTS_DIR, { recursive: true });
  srv = await serveHarness(HARNESS_DIR);
  record(`[setup] harness served at ${srv.base} · cs=${CS_ORIGIN} · agent=${AGENT} · mode=${MODE} · headless=${HEADLESS}`);

  browser = await puppeteer.launch({
    channel: "chrome",
    headless: HEADLESS,
    args: ["--host-resolver-rules=MAP localhost 127.0.0.1", "--window-size=1440,900"],
    defaultViewport: { width: 1440, height: 900 },
  });
  page = await browser.newPage();
  page.on("console", (m) => log.push(`[cs-console:${m.type()}] ${m.text()}`));
  page.on("pageerror", (e) => log.push(`[cs-pageerror] ${e.message}`));

  // Step 3: prime the code-server session first-party (validates the password
  // and warms the server). NOTE: the auth cookie is Partitioned, so this
  // session does NOT carry into the localhost-embedded iframe — the iframe is
  // re-authenticated in step 4. This first-party pass is the early, clear
  // password/reachability gate.
  record("[step3] priming code-server session (first-party)…");
  await page.goto(CS_URL, { waitUntil: "domcontentloaded", timeout: 45000 });
  if (await page.$("#token, input[type=password]")) {
    await loginInFrame(page.mainFrame(), CS_PASSWORD);
  }
  await poll("step3: code-server workbench (first-party)",
    () => page.$(".monaco-workbench"), { timeout: 60000 });
  record("[step3] first-party workbench up — password valid");

  // Step 4: load the harness (localhost top-level) with the code-server iframe.
  const harnessUrl = `${srv.base}/gui-harness.html?cs=${encodeURIComponent(CS_URL)}&mode=${MODE}` +
    (ACK_DELAY_MS ? `&ackDelayMs=${ACK_DELAY_MS}` : "") +
    (CLOCK_SKEW_MS ? `&clockSkewMs=${CLOCK_SKEW_MS}` : "");
  record(`[step4] loading harness: ${harnessUrl}`);
  await page.goto(harnessUrl, { waitUntil: "domcontentloaded", timeout: 45000 });

  // The embedded iframe shows /zcp-login under the localhost partition — log in
  // there, then wait for the inner workbench.
  const csFrame = await poll("step4: code-server iframe present",
    () => frameByOrigin(page, CS_ORIGIN), { timeout: 30000 });
  if (await csFrame.$("#token, input[type=password]")) {
    record("[step4] embedded iframe needs login (Partitioned cookie) — authenticating in-frame…");
    await loginInFrame(csFrame, CS_PASSWORD);
  }
  const csFrame2 = await poll("step4: embedded workbench (.monaco-workbench in iframe)", () => {
    const f = frameByOrigin(page, CS_ORIGIN);
    return f && f.$(".monaco-workbench").then((h) => (h ? f : null)).catch(() => null);
  }, { timeout: 60000 });
  record("[step4] embedded code-server workbench up");

  // Step 5: open the welcome panel via the command palette (real keyboard).
  record("[step5] opening command palette → 'Zerops: Get Started'…");
  const welcomeFrame = await runWelcomeCommand(page, csFrame2);
  record("[step5] welcome webview frame found");

  // Step 6: navigate to Build, read the agent status, click Authorize.
  record(`[step6] navigating to Build; inspecting agent '${AGENT}'…`);
  await welcomeFrame.waitForSelector('[data-nav="build"]', { timeout: 15000 });
  await welcomeFrame.click('[data-nav="build"]');
  await welcomeFrame.waitForSelector(`[data-agent-row="${AGENT}"]`, { visible: true, timeout: 15000 });

  const status = await welcomeFrame.$eval(`[data-agent-status="${AGENT}"]`, (el) => el.textContent.trim())
    .catch(() => "(status element missing)");
  record(`[step6] agent '${AGENT}' status: "${status}"`);

  // Step 6b: cross-check the footer's rendered extension version against the
  // shipped package.json — a drift here means this container is still
  // running an older zcp-bootstrap install than the one BootstrapExtVersion
  // now points at (docs/spec-welcome-mode.md §2 W-INSTALL).
  const shippedPkg = JSON.parse(fs.readFileSync(PACKAGE_JSON_PATH, "utf8"));
  const footerVersion = await welcomeFrame.$eval("[data-diag-version]", (el) => el.textContent.trim()).catch(() => "");
  if (footerVersion !== shippedPkg.version) {
    throw new Error(`[finding] welcome footer extension version "${footerVersion}" does not match shipped ` +
      `package.json "${shippedPkg.version}" — the running container is likely serving a stale zcp-bootstrap install.`);
  }
  record(`[step6b] footer extension version matches shipped package.json: ${footerVersion}`);

  const authBtn = await welcomeFrame.$(`[data-authorize="${AGENT}"]`);
  const clickable = authBtn && await authBtn.evaluate((el) => !el.hidden && el.offsetParent !== null);
  if (!clickable) {
    throw new Error(`[finding] [data-authorize="${AGENT}"] is hidden/absent — the agent is likely already ` +
      `authorized, not installed, or unavailable; the bridge cannot be triggered from this row.`);
  }
  await authBtn.click();
  record(`[step6] clicked Authorize in Zerops for '${AGENT}'`);

  // Step 7: poll bridge log + phase line, assert per MODE.
  record(`[step7] polling bridge log + phase (mode=${MODE})…`);
  const result = await assertOutcome(page, welcomeFrame);

  // Step 8: report.
  const summary = result.pass
    ? `PASS · mode=${MODE} · agent=${AGENT} · phase="${result.phase}" · bridge verdict=${result.entry ? result.entry.verdict : "-"} · sawContacting=${result.sawContacting}`
    : `FAIL · mode=${MODE} · agent=${AGENT} · ${result.reason}`;
  record("[step8] " + summary);

  if (!result.pass) {
    await dumpFailure(page, "assertion-failed", result);
    process.exitCode = 1;
  } else {
    process.exitCode = 0;
  }
} catch (err) {
  record("[error] " + (err && err.stack ? err.stack : String(err)));
  if (page) await dumpFailure(page, "exception", { reason: err && err.message });
  process.exitCode = 1;
} finally {
  if (browser) await browser.close().catch(() => {});
  if (srv) srv.close();
}

// runWelcomeCommand opens the palette, types the command, waits for its exact
// row to be the top match (so Enter can't fire before the extension's command
// is listed), runs it, and waits for the welcome webview frame. Retries the
// whole sequence — the zcp-bootstrap extension activates lazily, so the first
// attempt can land a hair before the command is registered.
async function runWelcomeCommand(page, csFrame) {
  const CMD = "Zerops: Get Started";
  for (let attempt = 1; attempt <= 4; attempt++) {
    try {
      await focusFrame(csFrame);
      await openCommandPalette(page, csFrame);
      await page.keyboard.type(CMD, { delay: 15 });
      // Gate Enter on the exact command row appearing as the first result.
      await poll(`step5: '${CMD}' row present (attempt ${attempt})`, async () => {
        const label = await csFrame.$eval(".quick-input-list .monaco-list-row .label-name",
          (el) => el.textContent.trim()).catch(() => "");
        return label === CMD;
      }, { timeout: 4000, interval: 150 });
      await page.keyboard.press("Enter");
      const wf = await poll(`step5: welcome webview frame (attempt ${attempt})`,
        () => frameWithSelector(page, "[data-authorize]"), { timeout: 8000, interval: 250 });
      if (wf) return wf;
    } catch (err) {
      record(`[step5] attempt ${attempt} did not open welcome (${err.message}) — retrying…`);
      await page.keyboard.press("Escape").catch(() => {});
      await sleep(1500);
    }
  }
  throw new Error("[step5] welcome webview frame never appeared after 4 command attempts");
}

// ---- command palette (F1 → Ctrl+Shift+P → Meta+Shift+P) -----------------
async function openCommandPalette(page, csFrame) {
  const combos = [
    async () => page.keyboard.press("F1"),
    async () => chord(page, ["Control", "Shift"], "KeyP"),
    async () => chord(page, ["Meta", "Shift"], "KeyP"),
  ];
  for (const press of combos) {
    await press();
    try {
      await poll("command palette input", () => csFrame.$(".quick-input-box input, .quick-input-widget input"),
        { timeout: 2500, interval: 150 });
      return;
    } catch (_) { /* try the next binding */ }
  }
  throw new Error("[step5] command palette never opened (F1 / Ctrl+Shift+P / Meta+Shift+P all failed)");
}

async function chord(page, mods, key) {
  for (const m of mods) await page.keyboard.down(m);
  await page.keyboard.press(key);
  for (const m of mods.slice().reverse()) await page.keyboard.up(m);
}

// ---- assertion ----------------------------------------------------------
async function assertOutcome(page, welcomeFrame) {
  const wantPhase = MODE === "ack" ? PHASE_DIALOG_OPENING : PHASE_NO_DASHBOARD;
  // 14s base covers the host's 12s ACK_TIMEOUT_MS (docs/spec-welcome-mode.md
  // §4) with margin; a delayed accepted ack (ackDelayMs) needs that much
  // MORE room on top, since the accept itself doesn't arrive until after
  // the delay elapses.
  const deadline = Date.now() + 14000 + ACK_DELAY_MS;
  let phase = "", entries = [];
  // sawContacting proves the phase line passed through "contacting" (spec
  // §4: posted the instant the trigger leaves this host, before any GUI
  // response) — with a real ackDelayMs the phase line is GUARANTEED to sit
  // there for the whole delay, so missing it is a real signal, not a timing
  // fluke; without a delay it's best-effort (the round trip can be faster
  // than this loop's 200ms cadence) and reported, never asserted.
  let sawContacting = false;
  for (;;) {
    entries = await page.evaluate(() => window.__bridgeLog || []).catch(() => []);
    phase = await welcomeFrame.$eval(`[data-agent-phase="${AGENT}"]`, (el) => el.textContent.trim()).catch(() => "");
    if (phase === PHASE_CONTACTING) sawContacting = true;
    const entry = entries.find((e) => e.agentType === AGENT);

    if (MODE === "ack") {
      const acceptEntry = entries.find((e) => e.agentType === AGENT && e.verdict === "accept");
      if (acceptEntry && phase === wantPhase) {
        const keysOk = sameKeys(acceptEntry.payloadKeys, EXPECTED_PAYLOAD_KEYS);
        if (!keysOk) {
          return { pass: false, phase, entry: acceptEntry, sawContacting,
            reason: `bridge payload keys mismatch: got [${(acceptEntry.payloadKeys || []).join(",")}] want [${EXPECTED_PAYLOAD_KEYS.join(",")}]` };
        }
        if (ACK_DELAY_MS > 0 && !sawContacting) {
          return { pass: false, phase, entry: acceptEntry, sawContacting,
            reason: `never observed phase "${PHASE_CONTACTING}" despite a ${ACK_DELAY_MS}ms ackDelayMs — the phase line should have sat on it for the whole delay` };
        }
        return { pass: true, phase, entry: acceptEntry, sawContacting };
      }
    } else {
      // silent: the trigger must have ARRIVED (log entry) and the tile must
      // fall back to no-dashboard after the host's 12s ack timeout.
      if (entry && phase === wantPhase) return { pass: true, phase, entry, sawContacting };
    }

    if (Date.now() > deadline) {
      const entry2 = entries.find((e) => e.agentType === AGENT);
      return { pass: false, phase, entry: entry2, entries, sawContacting,
        reason: `expected phase "${wantPhase}" and a ${MODE === "ack" ? "'accept'" : "trigger"} log entry; ` +
          `got phase "${phase}" and ${entries.length} log entr${entries.length === 1 ? "y" : "ies"}` };
    }
    await sleep(200);
  }
}

function sameKeys(a, b) {
  const sa = [...(a || [])].sort(), sb = [...b].sort();
  return sa.length === sb.length && sa.every((k, i) => k === sb[i]);
}

// ---- failure dump -------------------------------------------------------
async function dumpFailure(page, tag, result) {
  try {
    const shot = path.join(ARTIFACTS_DIR, `fail-${MODE}-${tag}-${Date.now()}.png`);
    await page.screenshot({ path: shot, fullPage: true });
    console.error("[artifact] screenshot: " + shot);
  } catch (e) { console.error("[artifact] screenshot failed: " + e.message); }
  if (result && result.reason) console.error("[dump] reason: " + result.reason);
  if (result && result.phase !== undefined) console.error(`[dump] last phase: "${result.phase}"`);
  try {
    console.error("[dump] frames (" + page.frames().length + "):");
    for (const f of page.frames()) {
      let marks = "";
      try {
        marks = await f.evaluate(() => [
          document.querySelector("[data-authorize]") ? "data-authorize" : "",
          document.querySelector(".monaco-workbench") ? "workbench" : "",
          document.querySelector(".quick-input-widget") ? "quickinput" : "",
          document.title ? "title=" + document.title : "",
        ].filter(Boolean).join(","));
      } catch (e) { marks = "<eval-blocked: " + e.message.split("\n")[0] + ">"; }
      console.error(`  - url=${f.url() || "(about:blank)"} :: ${marks}`);
    }
  } catch (_) { /* page may be gone */ }
  try {
    const entries = await page.evaluate(() => window.__bridgeLog || []);
    console.error("[dump] bridge log (" + entries.length + " entries):");
    for (const e of entries) {
      console.error(`  - ${e.verdict} agent=${e.agentType} origin=${e.origin} keys=[${(e.payloadKeys || []).join(",")}]`);
    }
  } catch (_) { /* page may be gone */ }
  console.error("[dump] --- captured console/log tail ---");
  for (const line of log.slice(-40)) console.error("  " + line);
}
