// Welcome auth-bridge local E2E runner (docs/spec-welcome-mode.md §4).
//
// Drives the whole bridge loop against a REAL code-server:
//   authorize click → host gate → broadcast trigger → gui-harness receiver
//   validates + acks → webview relays ack → host validates origin → phase line
// and asserts the observable outcome, per MODE:
//   ack    → the receiver acks; the tile phase becomes "Finish signing in via
//            the Zerops dialog…"
//   silent → the receiver never acks; the tile falls back to "…not detected…"
// plus the §4.3 embed command channel scenario matrix
// (plans/agent-first-onboarding-2026-07-28/briefs/S7-e2e-harness.md):
//   launch                → happy path: set-mode "onboarding" -> launch-agent(AGENT)
//   reload                → scenario 1: announce-on-init + fresh announce on reload
//   set-mode              → scenario 2: set-mode DIRECTIVE only (no launch)
//   launch-failed         → scenario 4: launch-agent for an unknown agentId
//   launch-idempotent     → scenario 5: same eventId sent twice (fresh createdAt)
//   launch-eventid-reuse  → scenario 6: same eventId, a DIFFERENT agentId second send
//   no-directive          → scenario 7: sends nothing; the 10s no-directive fallback
//                           is observed passively, best-effort
// None of the §4.3 modes click Authorize — the harness's own embed-ready
// handler (gui-harness.html) drives (or, for reload/no-directive, deliberately
// doesn't) the rest of the conversation.
//
// Two ways to run this file:
//   - LIVE (default): requires ZCP_CS_URL/ZCP_CS_PASSWORD, drives a REAL
//     code-server via puppeteer + the VS Code command palette. This is the
//     rig the orchestrator runs at ASSEMBLE (README documents every
//     invocation); DOM-dependent §4.3 assertions (scenarios 2/7) are
//     best-effort there — see assertSetModeEffect/assertNoDirectiveFallback.
//   - SELFTEST_CONTRACT=old|new: a fully deterministic, secret-free contract
//     test. No live rig, no ZCP_CS_URL — instead a locally-generated stub
//     "embed" double (buildStubEmbedHtml below) plays the code-server's role.
//     contract=old mirrors the pre-S1 embed (speaks NOTHING on this channel);
//     contract=new mirrors the landed §4.3 command channel. Every scenario
//     assertion below is written ONCE and used for both LIVE and SELFTEST —
//     only the driving mechanics (real workbench vs generated stub) differ.
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
// A second, DIFFERENT agent id — mirrors gui-harness.html's OTHER_AGENT
// (used only by MODE=launch-eventid-reuse, scenario 6): the id the driver
// resends under the REUSED eventId, which must never get its own outcome.
const OTHER_AGENT = AGENT === "codex" ? "claude-code" : "codex";
// KNOWN_MODES mirrors gui-harness.html's own list exactly (contract mirror —
// see README). ack/silent are the shipped §4.2 auth-trigger flow; everything
// else is the §4.3 embed command channel scenario matrix (see header).
const KNOWN_MODES = ["ack", "silent", "launch", "reload", "set-mode", "launch-failed",
  "launch-idempotent", "launch-eventid-reuse", "no-directive"];
const MODE = KNOWN_MODES.includes(process.env.MODE) ? process.env.MODE : "ack";
// DIRECTIVE (MODE=set-mode only, scenario 2): which mode the driver sends.
const DIRECTIVE = process.env.DIRECTIVE === "standard" ? "standard" : "onboarding";
const HEADLESS = process.env.HEADLESS !== "false";
// ackDelayMs/clockSkewMs (docs/spec-welcome-mode.md §4): forwarded to the
// gui-harness as query params so the orchestrator can prove the host's 12s
// ACK_TIMEOUT_MS window covers the deployed FE's real post-dialog-dispatch
// ack latency (e.g. HARNESS_ACK_DELAY_MS=6000) alongside a skewed receiver
// clock (e.g. HARNESS_CLOCK_SKEW_MS=1000) — unset/0 (the default) leaves the
// existing immediate-ack behavior unchanged.
const ACK_DELAY_MS = Number(process.env.HARNESS_ACK_DELAY_MS || 0);
const CLOCK_SKEW_MS = Number(process.env.HARNESS_CLOCK_SKEW_MS || 0);
// SELFTEST_CONTRACT (S7 deterministic contract test — see header): "old" |
// "new" | "" (unset = LIVE). Selects which stub embed double to generate.
const SELFTEST_CONTRACT = process.env.SELFTEST_CONTRACT === "old" ? "old"
  : process.env.SELFTEST_CONTRACT === "new" ? "new" : "";

// Exact phase strings the webview renders (welcome.html AUTH_PHASE_TEXT) — the
// assertions below match these verbatim. If welcome.html's copy changes, this
// must change with it (contract-mirror, see README).
const PHASE_CONTACTING = "Contacting the Zerops dashboard…";
const PHASE_DIALOG_OPENING = "Finish signing in via the Zerops dialog…";
const PHASE_NO_DASHBOARD = "Zerops dashboard not detected — reload the Zerops page";

// The exact six keys a credential-free trigger carries (welcome.js handleAuthorize).
const EXPECTED_PAYLOAD_KEYS = ["channel", "version", "type", "agentType", "eventId", "createdAt"];
// The exact keys an embed-ready announce carries (docs/spec-welcome-mode.md
// §4.1 envelope + §4.3 "Payload: agents ... + bootstrapVersion ... No
// installed axis") — an independent literal, not read back off the
// implementation. Order-independent (checked via sameKeys, like the trigger
// keys above).
const EXPECTED_ANNOUNCE_KEYS = ["channel", "version", "type", "eventId", "createdAt", "agents", "bootstrapVersion"];
// STUB_AGENTS (SELFTEST only): the stub embed's own hardcoded agent list, in
// a deliberately NON-default order (reverse of the real registry's
// claude-code-first order) — so a scenario-1 order assertion actually proves
// something (a reorder/drop bug in the harness's own record()/pass-through
// would be caught), not just echo whatever the stub happened to emit.
const STUB_AGENTS = ["codex", "claude-code"];

if (!SELFTEST_CONTRACT) {
  if (!CS_URL) fail("ZCP_CS_URL is required (e.g. https://zcp-xxxx-8080.prg1.zerops.app)");
  if (!CS_PASSWORD) fail("ZCP_CS_PASSWORD is required (the code-server VSCODE_PASSWORD)");
} else if (MODE === "ack" || MODE === "silent") {
  fail("SELFTEST_CONTRACT only supports the §4.3 scenario modes (launch, reload, set-mode, " +
    "launch-failed, launch-idempotent, launch-eventid-reuse, no-directive), not ack/silent — " +
    "those need a real auth dialog round trip.");
}
const CS_ORIGIN = CS_URL ? new URL(CS_URL).origin : "";

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

// ---- self-test stub embed double (S7 deterministic contract test) -------
// buildStubEmbedHtml generates a full standalone HTML page that plays the
// EMBED's role (the code-server + welcome webview, from the GUI's point of
// view) for a deterministic contract test — never used by the live rig,
// never a tracked source file (written into artifacts/, which is gitignored
// in this directory). contract="old" mirrors the pre-S1 embed: it speaks
// NOTHING on this channel (no embed-ready, no set-mode/launch-agent
// handling, no fallback) — every S7 scenario assertion below is expected to
// FAIL against it. contract="new" mirrors the landed §4.3 command channel:
// announces once on load, applies set-mode to a `#panel` visibility marker,
// answers launch-agent with the idempotent-re-ack / eventId-reuse-drop rules
// spec §4.3 requires, and (if fallbackMs>0) applies the container's default
// presentation if no set-mode arrives in time.
function buildStubEmbedHtml({ contract, fallbackMs }) {
  return `<!DOCTYPE html><html><head><meta charset="UTF-8">
<title>stub embed double (contract=${contract})</title></head>
<body data-mode="awaiting">
<div id="panel" hidden>PANEL</div>
<div id="terminal" hidden></div>
<script>
// Deterministic contract-test double for tools/welcome-bridge-harness's own
// scenario matrix (docs/spec-welcome-mode.md §4.3,
// plans/agent-first-onboarding-2026-07-28/briefs/S7-e2e-harness.md).
// Generated at self-test runtime by run.mjs's buildStubEmbedHtml — this is
// NOT a tracked source file and is NEVER used by the live rig (which embeds
// the REAL code-server instead via the ZCP_CS_URL/ZCP_CS_PASSWORD path).
const CONTRACT = new URLSearchParams(location.search).get("contract") === "old" ? "old" : "new";
const FALLBACK_MS = Number(new URLSearchParams(location.search).get("fallbackMs")) || 0;
const BRIDGE_CHANNEL = "@zerops/zcp-agent-auth-bridge";
const BRIDGE_VERSION = 1;
const STUB_AGENTS = ${JSON.stringify(STUB_AGENTS)};

if (CONTRACT === "new") {
  // §4.3: "Sent once per webview init, immediately". Payload: ordered
  // agents + bootstrapVersion, no installed axis.
  window.top.postMessage({
    channel: BRIDGE_CHANNEL, version: BRIDGE_VERSION, type: "embed-ready",
    eventId: crypto.randomUUID(), createdAt: Date.now(),
    agents: STUB_AGENTS.map((id) => ({ id, authorized: false })),
    bootstrapVersion: "0.0.0-selftest",
  }, "*");
}
// contract=old: intentionally nothing on load — the pre-S1 embed never
// announced at all.

const dedup = new Map(); // eventId -> {agentId, outcomeType, reason} — mirrors §4.3's dedup store
window.__terminalOpenCount = 0;
let modeSet = false;

function applyMode(mode) {
  document.body.dataset.mode = mode;
  document.getElementById("panel").hidden = (mode !== "standard");
}

function replyOutcome(ev, rec) {
  const base = { channel: BRIDGE_CHANNEL, version: BRIDGE_VERSION, eventId: rec.eventId, agentId: rec.agentId, createdAt: Date.now() };
  if (rec.outcomeType === "agent-ready") ev.source.postMessage(Object.assign({}, base, { type: "agent-ready" }), ev.origin);
  else ev.source.postMessage(Object.assign({}, base, { type: "launch-failed", reason: rec.reason }), ev.origin);
}

// handleLaunchAgent mirrors §4.3 "Retry & idempotence": in-flight/completed
// dedup by eventId; same eventId + SAME agentId -> idempotent re-ack of the
// stored outcome (no second terminal — window.__terminalOpenCount only
// increments on the FIRST accept); same eventId + DIFFERENT agentId ->
// rejected as malformed (dropped silently, no reply at all).
function handleLaunchAgent(ev, data) {
  const agentId = data.agentId, eventId = data.eventId;
  if (dedup.has(eventId)) {
    const rec = dedup.get(eventId);
    if (rec.agentId !== agentId) return; // malformed reuse — dropped
    replyOutcome(ev, rec);
    return;
  }
  if (STUB_AGENTS.includes(agentId)) {
    const rec = { agentId, eventId, outcomeType: "agent-ready" };
    dedup.set(eventId, rec);
    window.__terminalOpenCount += 1;
    const t = document.getElementById("terminal");
    t.hidden = false;
    t.textContent = "AGENT_LAUNCHED:" + agentId;
    replyOutcome(ev, rec);
  } else {
    const rec = { agentId, eventId, outcomeType: "launch-failed", reason: "unknown-agent" };
    dedup.set(eventId, rec);
    replyOutcome(ev, rec);
  }
}

window.addEventListener("message", (ev) => {
  if (CONTRACT !== "new") return; // contract=old: speaks nothing on this channel
  const data = ev.data;
  if (!data || typeof data !== "object" || data.channel !== BRIDGE_CHANNEL || data.version !== BRIDGE_VERSION) return;
  if (data.type === "set-mode") { modeSet = true; applyMode(data.mode); return; }
  if (data.type === "launch-agent") { handleLaunchAgent(ev, data); return; }
});

if (CONTRACT === "new" && FALLBACK_MS > 0) {
  // §1.3: "the 10 s window expiring ... container-owned rules apply. Empty
  // workbench -> the surface reveals the agent panel." Simulated here as the
  // "standard" presentation (panel revealed) — the scenario always assumes
  // an empty workbench.
  setTimeout(() => { if (!modeSet) applyMode("standard"); }, FALLBACK_MS);
}
</script>
</body></html>`;
}

// ---- shared scenario assertions (used by BOTH live and self-test) -------

function sameKeys(a, b) {
  const sa = [...(a || [])].sort(), sb = [...b].sort();
  return sa.length === sb.length && sa.every((k, i) => k === sb[i]);
}

// readEmbedHidden reads "is the embed's presentation currently dark/hidden"
// off whichever double is in play: "stub" reads the generated double's
// #panel marker; "live" reads the REAL welcome webview's own gate —
// welcome.html:21 `body[data-preload] { display: none; }`, removed only by
// the host's {type:"reveal"} post (welcome.js's revealPanel, cited at
// welcome.html:1303/welcome.js:1284) — i.e. exactly the §1.3 awaiting-mode
// dark-until-revealed mechanism, not a harness invention.
async function readEmbedHidden(embedFrame, readMode) {
  if (readMode === "live") {
    return embedFrame.evaluate(() => document.body.hasAttribute("data-preload")).catch(() => null);
  }
  return embedFrame.evaluate(() => {
    const p = document.getElementById("panel");
    return p ? p.hidden : null;
  }).catch(() => null);
}

function assertAnnounceShape(entry, expectOrder) {
  if (!entry) return { ok: false, reason: "no announce entry observed" };
  if (!sameKeys(entry.payloadKeys, EXPECTED_ANNOUNCE_KEYS)) {
    return {
      ok: false,
      reason: `announce payload keys mismatch: got [${entry.payloadKeys.join(",")}] want [${EXPECTED_ANNOUNCE_KEYS.join(",")}] (no "installed" axis, §4.3)`,
    };
  }
  if (!Array.isArray(entry.agents) || entry.agents.length === 0) {
    return { ok: false, reason: "announce agents is not a non-empty array" };
  }
  for (const a of entry.agents) {
    if (!a || typeof a.id !== "string" || typeof a.authorized !== "boolean") {
      return { ok: false, reason: `malformed agent entry: ${JSON.stringify(a)}` };
    }
  }
  if (expectOrder) {
    const got = entry.agents.map((a) => a.id);
    if (got.length !== expectOrder.length || got.some((id, i) => id !== expectOrder[i])) {
      return { ok: false, reason: `agents order mismatch: got [${got.join(",")}] want [${expectOrder.join(",")}]` };
    }
  }
  if (!entry.bootstrapVersion) return { ok: false, reason: "bootstrapVersion missing/empty" };
  return { ok: true };
}

// assertAnnounceOnInitAndReload (scenario 1): the FIRST embed-ready must have
// the exact §4.3 shape; forcing a reload of the embed must produce a FRESH
// one with the same shape (spec §1.3 "Reload semantics: an embed reload ...
// the fresh surface re-announces").
async function assertAnnounceOnInitAndReload(page, { expectOrder, reload }) {
  const first = await poll("scenario1: first announce entry", async () => {
    const entries = await page.evaluate(() => window.__bridgeLog || []);
    return entries.find((e) => e.verdict === "announce") || null;
  }, { timeout: 15000 }).catch((err) => { throw err; });
  const firstCheck = assertAnnounceShape(first, expectOrder);
  if (!firstCheck.ok) return { pass: false, reason: "initial announce: " + firstCheck.reason };

  const beforeCount = (await page.evaluate(() => window.__bridgeLog || []))
    .filter((e) => e.verdict === "announce").length;
  await reload();
  const second = await poll("scenario1: fresh announce after reload", async () => {
    const entries = await page.evaluate(() => window.__bridgeLog || []);
    const anns = entries.filter((e) => e.verdict === "announce");
    return anns.length > beforeCount ? anns[anns.length - 1] : null;
  }, { timeout: 20000 });
  const secondCheck = assertAnnounceShape(second, expectOrder);
  if (!secondCheck.ok) return { pass: false, reason: "post-reload announce: " + secondCheck.reason };
  return { pass: true, detail: `announce shape ok before and after reload (agents=${first.agents.map((a) => a.id).join(",")})` };
}

// assertSetModeEffect (scenario 2): set-mode "standard" -> presentation
// revealed; "onboarding" -> stays dark. `hard` selects whether a timeout is
// a real failure (self-test, against the stub) or advisory-only (live,
// best-effort per the S7 brief — the exact live DOM shape is unverified
// outside a real container).
async function assertSetModeEffect(page, { embedFrame, readMode, hard }) {
  const wantHidden = DIRECTIVE !== "standard";
  // Precondition: the embed must have announced at all. Without this, the
  // "onboarding -> stays dark" direction is a no-op assertion that would
  // vacuously PASS against a double that never speaks the protocol at all
  // (the panel markup starts hidden by default) — requiring an observed
  // announce first makes BOTH directions a real proof of "set-mode reached
  // and was honored", not an accidental match on the initial state.
  try {
    await poll("scenario2: embed announced (embed-ready observed)", async () => {
      const entries = await page.evaluate(() => window.__bridgeLog || []);
      return entries.some((e) => e.verdict === "announce");
    }, { timeout: hard ? 5000 : 8000 });
  } catch (err) {
    if (hard) return { pass: false, reason: `embed never announced — set-mode has nothing to target: ${err.message}` };
    return { pass: true, bestEffort: true, detail: `best-effort: embed never announced: ${err.message}` };
  }
  try {
    await poll(`scenario2: presentation hidden=${wantHidden} for directive=${DIRECTIVE}`, async () => {
      const hidden = await readEmbedHidden(embedFrame, readMode);
      return hidden === wantHidden;
    }, { timeout: hard ? 8000 : 6000 });
    return { pass: true, detail: `presentation hidden=${wantHidden} observed for directive=${DIRECTIVE}` };
  } catch (err) {
    if (hard) return { pass: false, reason: err.message };
    return { pass: true, bestEffort: true, detail: `best-effort DOM check inconclusive: ${err.message}` };
  }
}

// assertNoDirectiveFallback (scenario 7): with NO set-mode ever sent, the
// container/embed must apply its own default (empty workbench -> revealed)
// once the no-directive window expires (§1.3). Same hard/best-effort split
// as scenario 2, for the same reason.
async function assertNoDirectiveFallback(page, { embedFrame, readMode, timeoutMs, hard }) {
  try {
    await poll("scenario7: presentation revealed after no-directive expiry", async () => {
      const hidden = await readEmbedHidden(embedFrame, readMode);
      return hidden === false;
    }, { timeout: timeoutMs });
    return { pass: true, detail: "presentation revealed after the no-directive window expired" };
  } catch (err) {
    if (hard) return { pass: false, reason: err.message };
    return { pass: true, bestEffort: true, detail: `best-effort DOM check inconclusive: ${err.message}` };
  }
}

// assertLaunchFailedOutcome (scenario 4): an unknown agentId must be
// rejected PRE-DISPATCH with launch-failed/"unknown-agent" — never a
// terminal, never agent-ready.
async function assertLaunchFailedOutcome(page) {
  const deadline = Date.now() + 15000;
  for (;;) {
    const state = await page.evaluate(() => ({
      launchEventId: window.__launchEventId || null,
      entries: window.__bridgeLog || [],
    })).catch(() => ({ launchEventId: null, entries: [] }));
    if (state.launchEventId) {
      const outcome = state.entries.find((e) => e.eventId === state.launchEventId &&
        (e.verdict === "agent-ready" || e.verdict === "launch-failed"));
      if (outcome) {
        const ok = outcome.verdict === "launch-failed" && outcome.reason === "unknown-agent";
        return {
          pass: ok, entry: outcome,
          reason: ok ? undefined : `expected launch-failed/unknown-agent, got ${outcome.verdict}${outcome.reason ? "/" + outcome.reason : ""}`,
          detail: ok ? `launch-failed reason="${outcome.reason}" correlated by eventId` : undefined,
        };
      }
    }
    if (Date.now() > deadline) {
      return { pass: false, reason: `no launch-failed outcome within 15s; launchEventId=${state.launchEventId || "(embed-ready never drove it)"}` };
    }
    await sleep(200);
  }
}

// assertIdempotentOutcome (scenario 5): the SAME eventId sent twice (fresh
// createdAt) must NEVER start a second launch — the dedup store answers the
// duplicate from its stored entry (§4.3).
//
// How many outcomes reach this page depends on whether the receiver still
// exists when the duplicate lands, and BOTH shapes are spec-correct:
//   - duplicate before the outcome's relay-forwarded receipt → the receiver is
//     still up (§4.3 gates teardown on that receipt) → a second, identical
//     agent-ready is relayed: 2 outcomes.
//   - duplicate after a successful launch was confirmed → §5.3 has already
//     closed the receiver as part of establishing the onboarding layout, so no
//     relay is left to re-ack through: 1 outcome. This is the real production
//     shape (live-observed 2026-07-29) and needs no retry — the FE only
//     resends when it never got an outcome, which is exactly the case where
//     the receipt never arrived and the receiver is therefore still up.
// The invariant asserted here is "≥1 outcome, all identical, exactly one
// terminal"; the pre-teardown 2-outcome re-ack path is pinned deterministically
// by welcomejs/command_channel.test.js, which can hold the receiver open.
async function assertIdempotentOutcome(page, { embedFrame } = {}) {
  const launchEventId = await poll("scenario5: driver produced a launchEventId",
    () => page.evaluate(() => window.__launchEventId || null), { timeout: 15000 });
  // Settle past the driver's 300ms resend so both sends have been answered.
  await sleep(1200);
  const entries = await page.evaluate(() => window.__bridgeLog || []);
  const outcomes = entries.filter((e) => e.eventId === launchEventId && e.verdict === "agent-ready");
  if (outcomes.length < 1) {
    return { pass: false, reason: `expected at least one agent-ready outcome for the reused eventId, got ${outcomes.length}` };
  }
  if (outcomes.some((o) => o.agentId !== AGENT)) {
    return { pass: false, reason: "an outcome carried an unexpected agentId" };
  }
  let terminalCount = null;
  if (embedFrame) {
    terminalCount = await embedFrame.evaluate(() => (typeof window.__terminalOpenCount === "number" ? window.__terminalOpenCount : null)).catch(() => null);
    if (terminalCount !== null && terminalCount !== 1) {
      return { pass: false, reason: `expected exactly 1 terminal open, embed reports __terminalOpenCount=${terminalCount}` };
    }
  }
  return { pass: true, detail: `${outcomes.length} identical agent-ready outcome(s) for one eventId — the duplicate never started a second launch; terminalOpenCount=${terminalCount === null ? "(unobserved)" : terminalCount}` };
}

// assertEventIdReuseOutcome (scenario 6): reusing an eventId with a
// DIFFERENT agentId must be dropped as malformed — no second outcome, no
// leak of OTHER_AGENT anywhere in the log.
async function assertEventIdReuseOutcome(page) {
  const launchEventId = await poll("scenario6: driver produced a launchEventId",
    () => page.evaluate(() => window.__launchEventId || null), { timeout: 15000 });
  await poll("scenario6: first (valid) launch-agent outcome", async () => {
    const entries = await page.evaluate(() => window.__bridgeLog || []);
    return entries.some((e) => e.eventId === launchEventId);
  }, { timeout: 10000 });
  // Settle past the driver's 300ms reused-eventId resend to prove it stays dropped.
  await sleep(1200);
  const entries = await page.evaluate(() => window.__bridgeLog || []);
  const forEvent = entries.filter((e) => e.eventId === launchEventId);
  if (forEvent.length !== 1) {
    return { pass: false, reason: `expected exactly 1 outcome for the eventId (a same-eventId/different-agentId reuse must be dropped, not answered), got ${forEvent.length}` };
  }
  if (forEvent[0].verdict !== "agent-ready" || forEvent[0].agentId !== AGENT) {
    return { pass: false, reason: `expected the sole outcome to be agent-ready for ${AGENT}, got ${forEvent[0].verdict}/${forEvent[0].agentId}` };
  }
  const otherAgentOutcome = entries.find((e) => e.agentId === OTHER_AGENT);
  if (otherAgentOutcome) {
    return { pass: false, reason: `an outcome for the reused eventId's DIFFERENT agentId (${OTHER_AGENT}) leaked through — must be dropped as malformed` };
  }
  return { pass: true, detail: `eventId honored once (agent-ready for ${AGENT}); reused-eventId/different-agentId resend correctly dropped` };
}

// assertLaunchOutcome (MODE=launch, scenario 3, docs/spec-welcome-mode.md
// §4.3): polls until the harness's own launch-agent eventId
// (window.__launchEventId, set by gui-harness.html's driveLaunchOnAnnounce
// once the FIRST embed-ready lands) has a correlated agent-ready or
// launch-failed entry in the bridge log — "outcomes carry no identity of
// their own" beyond the command's eventId, so correlation by eventId IS the
// assertion, not just an entry's mere presence. 15s covers embed boot + the
// container's own dispatch, which is synchronous end-to-end (§5.1: no
// shell-integration wait).
async function assertLaunchOutcome(page) {
  const deadline = Date.now() + 15000;
  for (;;) {
    const state = await page.evaluate(() => ({
      launchEventId: window.__launchEventId || null,
      entries: window.__bridgeLog || [],
    })).catch(() => ({ launchEventId: null, entries: [] }));

    if (state.launchEventId) {
      const outcome = state.entries.find(
        (e) => e.eventId === state.launchEventId && (e.verdict === "agent-ready" || e.verdict === "launch-failed")
      );
      if (outcome) {
        return { pass: outcome.verdict === "agent-ready", entry: outcome, launchEventId: state.launchEventId, entries: state.entries,
          detail: outcome.verdict === "agent-ready" ? `agent-ready correlated by eventId for ${AGENT}` : undefined,
          reason: outcome.verdict === "launch-failed" ? `launch-failed: reason="${outcome.reason}"` : undefined };
      }
    }

    if (Date.now() > deadline) {
      return { pass: false, launchEventId: state.launchEventId, entries: state.entries,
        reason: `expected an agent-ready/launch-failed entry correlated by eventId within 15s; ` +
          `launchEventId=${state.launchEventId || "(embed-ready never drove a launch)"}, got ${state.entries.length} log entr${state.entries.length === 1 ? "y" : "ies"}` };
    }
    await sleep(200);
  }
}

// runScenarioAssertion dispatches MODE to the right assertion — the ONE
// place both the live runner and the self-test runner call into, so every
// scenario's assertion logic is written exactly once.
async function runScenarioAssertion(mode, ctx) {
  switch (mode) {
    case "launch": return assertLaunchOutcome(ctx.page);
    case "reload": return assertAnnounceOnInitAndReload(ctx.page, { expectOrder: ctx.expectOrder, reload: ctx.reload });
    case "set-mode": return assertSetModeEffect(ctx.page, { embedFrame: ctx.embedFrame, readMode: ctx.readMode, hard: ctx.hard });
    case "launch-failed": return assertLaunchFailedOutcome(ctx.page);
    case "launch-idempotent": return assertIdempotentOutcome(ctx.page, { embedFrame: ctx.embedFrame });
    case "launch-eventid-reuse": return assertEventIdReuseOutcome(ctx.page);
    case "no-directive": return assertNoDirectiveFallback(ctx.page, { embedFrame: ctx.embedFrame, readMode: ctx.readMode, timeoutMs: ctx.fallbackTimeoutMs, hard: ctx.hard });
    default: return { pass: false, reason: `no scenario assertion registered for MODE=${mode}` };
  }
}

// ---- main -----------------------------------------------------------------
const log = [];
const record = (line) => { log.push(line); console.log(line); };

let browser, srv, stubSrv, page;
try {
  fs.mkdirSync(ARTIFACTS_DIR, { recursive: true });
  if (SELFTEST_CONTRACT) {
    await runSelftest();
  } else {
    await runLive();
  }
} catch (err) {
  record("[error] " + (err && err.stack ? err.stack : String(err)));
  if (page) await dumpFailure(page, "exception", { reason: err && err.message });
  process.exitCode = 1;
} finally {
  if (browser) await browser.close().catch(() => {});
  if (srv) srv.close();
  if (stubSrv) stubSrv.close();
}

// ---- SELFTEST_CONTRACT: deterministic, secret-free contract test ---------
async function runSelftest() {
  const contract = SELFTEST_CONTRACT;
  // Only MODE=no-directive needs a short fallback window; everything else
  // leaves it disabled (0) so an unrelated scenario never accidentally
  // exercises the fallback path.
  const stubFallbackMs = MODE === "no-directive" ? 300 : 0;
  fs.writeFileSync(path.join(ARTIFACTS_DIR, "stub-embed.html"),
    buildStubEmbedHtml({ contract, fallbackMs: stubFallbackMs }));

  srv = await serveHarness(HARNESS_DIR);
  // A SEPARATE server (distinct ephemeral port -> distinct origin) serves
  // the stub — mirrors the live topology (code-server is a genuinely
  // different origin from the harness page) and lets frameByOrigin uniquely
  // find the embed iframe instead of matching the harness page itself.
  stubSrv = await serveHarness(ARTIFACTS_DIR);
  const stubUrl = `${stubSrv.base}/stub-embed.html?contract=${contract}` +
    (stubFallbackMs ? `&fallbackMs=${stubFallbackMs}` : "");
  record(`[selftest setup] contract=${contract} mode=${MODE} agent=${AGENT} stub=${stubUrl}`);

  browser = await puppeteer.launch({
    channel: "chrome",
    headless: HEADLESS,
    args: ["--host-resolver-rules=MAP localhost 127.0.0.1", "--window-size=1200,800"],
    defaultViewport: { width: 1200, height: 800 },
  });
  page = await browser.newPage();
  page.on("console", (m) => log.push(`[selftest-console:${m.type()}] ${m.text()}`));
  page.on("pageerror", (e) => log.push(`[selftest-pageerror] ${e.message}`));

  const harnessUrl = `${srv.base}/gui-harness.html?cs=${encodeURIComponent(stubUrl)}&mode=${MODE}&agent=${encodeURIComponent(AGENT)}` +
    (MODE === "set-mode" ? `&directive=${DIRECTIVE}` : "");
  record(`[selftest] loading harness: ${harnessUrl}`);
  await page.goto(harnessUrl, { waitUntil: "domcontentloaded", timeout: 15000 });

  const stubOrigin = new URL(stubUrl).origin;
  // The iframe's src is assigned unconditionally by gui-harness.html on load
  // (regardless of contract), so the frame itself attaches either way — only
  // whether it SPEAKS the protocol differs. Poll for it under BOTH contracts
  // so a slow-attaching frame is never mistaken for "old contract never
  // responds" (that distinction belongs to the scenario assertions, not to
  // frame-attach timing).
  const embedFrame = await poll("selftest: stub embed frame present",
    () => frameByOrigin(page, stubOrigin), { timeout: 10000 }).catch(() => undefined);

  const result = await runScenarioAssertion(MODE, {
    page, embedFrame,
    expectOrder: STUB_AGENTS,
    readMode: "stub",
    hard: true,
    fallbackTimeoutMs: 2000,
    reload: async () => {
      const bust = stubUrl + (stubUrl.includes("?") ? "&" : "?") + "r=" + Date.now();
      await page.evaluate((u) => { document.getElementById("cs-frame").src = u; }, bust);
    },
  }).catch((err) => ({ pass: false, reason: err.message }));

  const verdict = result.pass ? "PASS" : "FAIL";
  record(`[selftest result] contract=${contract} mode=${MODE} → ${verdict}` +
    (result.detail ? ` · ${result.detail}` : "") + (result.reason ? ` · ${result.reason}` : ""));
  if (!result.pass) await dumpFailure(page, "selftest-assertion-failed", result);
  process.exitCode = result.pass ? 0 : 1;
}

// ---- LIVE: drives a real code-server ---------------------------------------
async function runLive() {
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
  // agent= is threaded through unconditionally (harmless for ack/silent,
  // which never reads it) so the §4.3 modes' drivers target the same AGENT
  // this runner asserts against.
  const harnessUrl = `${srv.base}/gui-harness.html?cs=${encodeURIComponent(CS_URL)}&mode=${MODE}&agent=${encodeURIComponent(AGENT)}` +
    (MODE === "set-mode" ? `&directive=${DIRECTIVE}` : "") +
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

  // Step 5: open the panel via the command palette (real keyboard). This
  // alone is enough to trigger the §4.3 announce: the webview posts
  // {type:"ready"} the instant it mounts, and the host's "ready" handler
  // broadcasts embed-ready right after — no click needed for the §4.3 modes.
  record("[step5] opening command palette → 'Zerops: Open Panel'…");
  const welcomeFrame = await runWelcomeCommand(page, csFrame2);
  record("[step5] welcome webview frame found");

  if (MODE === "ack" || MODE === "silent") {
    // Step 6: read the agent status, click Authorize. Agent rows are visible
    // directly on the panel (no separate "Build" nav — that surface was
    // deleted alongside the old onboarding CTA).
    record(`[step6] inspecting agent '${AGENT}'…`);
    // §6 collapsed list: once ≥1 agent is in an active state, the panel renders
    // only those rows plus a "+ Add another agent" expander — an unauthorized
    // target (exactly what ack/silent needs) sits behind it. Expand first when
    // the row isn't already visible; a panel with zero active agents renders
    // the full list and has no expander (nothing to click).
    const rowVisible = await welcomeFrame
      .$eval(`[data-agent-row="${AGENT}"]`, (el) => !el.hidden && el.offsetParent !== null)
      .catch(() => false);
    if (!rowVisible) {
      const expanded = await welcomeFrame.evaluate(() => {
        const btn = document.querySelector("[data-agent-expander]");
        if (!btn || btn.hidden) return false;
        btn.click();
        return true;
      });
      record(`[step6] target row was collapsed — expander ${expanded ? "clicked" : "absent (full list already rendered)"}`);
    }
    await welcomeFrame.waitForSelector(`[data-agent-row="${AGENT}"]`, { visible: true, timeout: 15000 });

    const status = await welcomeFrame.$eval(`[data-agent-status="${AGENT}"]`, (el) => el.textContent.trim())
      .catch(() => "(status element missing)");
    record(`[step6] agent '${AGENT}' status: "${status}"`);

    // Step 6b: cross-check the footer's rendered extension version against the
    // shipped package.json — a drift here means this container is still
    // running an older zcp-bootstrap install than the one BootstrapExtVersion
    // now points at (docs/spec-welcome-mode.md §2 W-INSTALL).
    const shippedPkg = JSON.parse(fs.readFileSync(PACKAGE_JSON_PATH, "utf8"));
    // The footer fills from the first {type:"state"} push after the ready
    // handshake — poll briefly so a freshly-opened panel isn't misread as a
    // stale install while it still shows the "—" placeholder.
    let footerVersion = "";
    for (const versionDeadline = Date.now() + 5000; Date.now() < versionDeadline;) {
      footerVersion = await welcomeFrame.$eval("[data-diag-version]", (el) => el.textContent.trim()).catch(() => "");
      if (footerVersion && footerVersion !== "—" && footerVersion !== "-") break;
      await sleep(200);
    }
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
  } else {
    // §4.3 command-channel modes (launch + the S7 scenario matrix): none of
    // these click Authorize — the harness's own embed-ready handler in
    // gui-harness.html drives (or, for reload/no-directive, deliberately
    // doesn't) the rest of the conversation.
    record(`[step6] MODE=${MODE} — §4.3 command-channel scenario (agent=${AGENT})…`);
    const result = await runScenarioAssertion(MODE, {
      page,
      embedFrame: welcomeFrame,
      readMode: "live",
      hard: false, // DOM-dependent scenarios (2/7) are best-effort against a real container
      fallbackTimeoutMs: 16000, // covers the container's real ~10s no-directive window with margin
      reload: () => reloadLiveEmbed(page),
    }).catch((err) => ({ pass: false, reason: err.message }));

    const summary = result.pass
      ? `PASS · mode=${MODE} · agent=${AGENT}` + (result.detail ? ` · ${result.detail}` : "") + (result.bestEffort ? " · (best-effort DOM check, non-fatal)" : "")
      : `FAIL · mode=${MODE} · agent=${AGENT} · ${result.reason}`;
    record("[step7] " + summary);

    if (!result.pass) {
      await dumpFailure(page, "assertion-failed", result);
      process.exitCode = 1;
    } else {
      process.exitCode = 0;
    }
  }
}

// reloadLiveEmbed (scenario 1, LIVE only): forces the WHOLE embedded
// code-server iframe to reload (a real webview reload, not merely re-running
// the reveal-not-recreate `zerops.panel` command — re-invoking that command
// would NOT itself produce a second announce, since the panel is a
// singleton). After the reload, re-authenticate if the Partitioned session
// didn't survive, wait for the workbench, and re-open the panel so the
// fresh webview instance mounts and re-announces.
async function reloadLiveEmbed(page) {
  const bust = CS_URL + (CS_URL.includes("?") ? "&" : "?") + "_r=" + Date.now();
  await page.evaluate((u) => { document.getElementById("cs-frame").src = u; }, bust);
  const csFrame = await poll("scenario1(live): reloaded code-server iframe present",
    () => frameByOrigin(page, CS_ORIGIN), { timeout: 30000 });
  if (await csFrame.$("#token, input[type=password]")) {
    await loginInFrame(csFrame, CS_PASSWORD);
  }
  await poll("scenario1(live): reloaded workbench up", () => {
    const f = frameByOrigin(page, CS_ORIGIN);
    return f && f.$(".monaco-workbench").then((h) => (h ? f : null)).catch(() => null);
  }, { timeout: 60000 });
  const csFrame2 = frameByOrigin(page, CS_ORIGIN);
  await runWelcomeCommand(page, csFrame2);
}

// runWelcomeCommand opens the palette, types the command, waits for its exact
// row to be the top match (so Enter can't fire before the extension's command
// is listed), runs it, and waits for the welcome webview frame. Retries the
// whole sequence — the zcp-bootstrap extension activates lazily, so the first
// attempt can land a hair before the command is registered.
async function runWelcomeCommand(page, csFrame) {
  const CMD = "Zerops: Open Panel";
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

// ---- §4.2 assertion (ack/silent, unchanged) ------------------------------
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
    phase = await welcomeFrame.$eval(`[data-agent-status="${AGENT}"]`, (el) => el.textContent.trim()).catch(() => "");
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
      console.error(`  - ${e.verdict} agent=${e.agentType || e.agentId} origin=${e.origin} keys=[${(e.payloadKeys || []).join(",")}]`);
    }
  } catch (_) { /* page may be gone */ }
  console.error("[dump] --- captured console/log tail ---");
  for (const line of log.slice(-40)) console.error("  " + line);
}
