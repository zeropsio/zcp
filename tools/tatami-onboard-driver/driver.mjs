// Tatami fresh-registration onboarding driver.
//
// Drives BOTH paths of the agent-authorization bug in one browser session and
// writes CDP-level capture for each:
//
//   fresh   — /registration?zcp=true -> claim drain -> wizard -> pick agent
//             -> `authorizing` -> auth dialog -> terminal WebSocket (fails)
//   control — full page reload of the SAME account -> zcp service detail
//             -> manual "Trigger authorization process" -> same dialog (works)
//
// The diff between the two captures is the deliverable. See ./README.md.

import { chromium } from 'playwright';
import { mkdirSync, writeFileSync, readFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

const BASE = process.env.TATAMI_BASE || 'https://tatami.devel.zerops.dev';
const EVIDENCE =
  process.env.EVIDENCE_DIR ||
  '/Users/macbook/Documents/Zerops-MCP/zcp/plans/tatami-onboarding-auth-2026-08-03/evidence';
const HEADLESS = process.env.HEADLESS !== '0';
const RUN = process.env.RUN || 'both'; // fresh | control | both
const AGENT = process.env.AGENT || 'Claude Code';
const SLOW = Number(process.env.SLOWMO || 0);
// How long to sit in `authorizing` watching the terminal WS retry loop.
const AUTHORIZING_WATCH_MS = Number(process.env.WATCH_MS || 75000);

const { Capture, attachCapture, installWsProbe, snapshotStorage } = await import('./capture.mjs');

const stamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

mkdirSync(EVIDENCE, { recursive: true });

// ---------------------------------------------------------------- helpers

const trunc = (s, n = 6) =>
  !s ? s : String(s).length <= 2 * n + 3 ? String(s) : `${String(s).slice(0, n)}…${String(s).slice(-n)}`;

function decodeJwt(tok) {
  if (!tok || typeof tok !== 'string') return null;
  const parts = tok.split('.');
  if (parts.length < 2) return { notAJwt: true, len: tok.length, sample: trunc(tok) };
  try {
    const pad = (s) => s + '='.repeat((4 - (s.length % 4)) % 4);
    const b = (s) => JSON.parse(Buffer.from(pad(s.replace(/-/g, '+').replace(/_/g, '/')), 'base64').toString('utf8'));
    return { header: b(parts[0]), payload: b(parts[1]), sigLen: (parts[2] || '').length };
  } catch (e) {
    return { decodeError: String(e).slice(0, 120), len: tok.length, sample: trunc(tok) };
  }
}

async function readWizard(page) {
  return page
    .evaluate(() => {
      const w = document.querySelector('z-zcp-onboard-wizard');
      const overlay = w?.querySelector('.__overlay');
      const msg = w?.querySelector('.__msg')?.textContent?.trim() || '';
      const sub = w?.querySelector('.__sub')?.textContent?.trim() || '';
      const tiles = [...(w?.querySelectorAll('.__agent-btn') || [])].map((b) => ({
        text: (b.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 40),
        pressed: b.getAttribute('aria-pressed'),
        badge: !!b.querySelector('.__badge'),
      }));
      const cont = [...(w?.querySelectorAll('button.__continue') || [])].map((b) =>
        (b.textContent || '').trim(),
      );
      let state = 'idle';
      if (overlay) {
        if (tiles.length) state = 'picking';
        else if (/Setting up your workspace/i.test(msg)) state = 'claiming';
        else if (/Waiting for you to finish signing in/i.test(msg)) state = 'authorizing';
        else if (/ is ready$/i.test(msg)) state = 'launch-ready';
        else if (/^Starting /i.test(msg)) state = 'launching';
        else if (/couldn.t reach your workspace/i.test(msg)) state = 'failed';
        else state = 'overlay?';
      }
      const pane = document.querySelector('.cdk-overlay-pane');
      const dlgButtons = pane
        ? [...pane.querySelectorAll('button')].map((b) => (b.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 44)).filter(Boolean)
        : [];
      const stepper = pane
        ? [...pane.querySelectorAll('.__step')].map((s) => ({
            label: s.querySelector('.__step-label')?.textContent?.trim(),
            done: s.className.includes('--done'),
            active: s.className.includes('--active'),
            error: s.className.includes('--error'),
          }))
        : [];
      const termText = pane ? (pane.querySelector('.__terminal-wrapper')?.innerText || '').slice(0, 600) : '';
      return { state, msg, sub, tiles, cont, dialogPresent: !!pane, dlgButtons, stepper, termText, url: location.href };
    })
    .catch((e) => ({ state: 'eval-error', err: String(e).slice(0, 200) }));
}

// Poll the wizard/dialog and screenshot every time the rendered state changes.
function startStateWatcher(page, cap, prefix) {
  let last = null;
  let n = 0;
  let stop = false;
  const seen = [];
  (async () => {
    while (!stop) {
      const w = await readWizard(page);
      const key = `${w.state}|${w.dialogPresent}|${w.dlgButtons?.join(',')}|${w.stepper?.map((s) => `${s.label}${s.done ? 'D' : ''}${s.active ? 'A' : ''}${s.error ? 'E' : ''}`).join('>')}`;
      if (key !== last) {
        last = key;
        n += 1;
        const file = join(EVIDENCE, `${prefix}-${String(n).padStart(2, '0')}-${w.state}${w.dialogPresent ? '-dialog' : ''}.png`);
        await page.screenshot({ path: file, fullPage: false }).catch(() => {});
        cap.note(`state -> ${w.state}${w.dialogPresent ? ' (dialog)' : ''}`, {
          msg: w.msg,
          dlgButtons: w.dlgButtons,
          shot: file.split('/').pop(),
        });
        seen.push({ t: cap.ms(), ...w, shot: file.split('/').pop() });
      }
      await sleep(250);
    }
  })();
  return {
    stop: () => {
      stop = true;
    },
    seen,
  };
}

async function waitFor(page, fn, { timeout = 60000, poll = 300, what = 'condition' } = {}) {
  const t0 = Date.now();
  while (Date.now() - t0 < timeout) {
    const w = await readWizard(page);
    if (fn(w)) return w;
    await sleep(poll);
  }
  throw new Error(`timeout ${timeout}ms waiting for ${what}`);
}

function saveCapture(cap, name) {
  const file = join(EVIDENCE, `${name}-${stamp}.json`);
  writeFileSync(file, JSON.stringify(cap.toJSON(), null, 2));
  console.log(`\n== wrote ${file}`);
  return file;
}

// ------------------------------------------------------------------ paths

async function freshPath(page, cap, account) {
  const watcher = startStateWatcher(page, cap, 'fresh');

  cap.note('goto /registration?zcp=true');
  await page.goto(`${BASE}/registration?zcp=true`, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await sleep(3500);
  await page.screenshot({ path: join(EVIDENCE, 'fresh-00-registration.png') });
  await snapshotStorage(page, cap, 'registration-loaded');

  const texts = await page.locator('input[type="text"]').all();
  if (texts.length < 2) throw new Error(`registration form not as expected: ${texts.length} text inputs`);
  await texts[0].fill(account.org);
  await texts[1].fill(account.name);
  await page.locator('input[name="email"]').fill(account.email);
  await page.locator('input[type="password"]').first().fill(account.password);
  cap.note('registration form filled', { email: account.email, org: account.org });
  await page.screenshot({ path: join(EVIDENCE, 'fresh-00b-registration-filled.png') });

  await page.getByRole('button', { name: /Register to Zerops/i }).click();
  cap.note('submitted registration');

  // claiming cover -> picking (drain has a 30 s cap of its own)
  const picked = await waitFor(page, (w) => w.state === 'picking' || w.state === 'failed', {
    timeout: 75000,
    what: 'wizard picking',
  });
  await snapshotStorage(page, cap, 'wizard-picking');
  cap.note('wizard reached', { state: picked.state, url: picked.url, tiles: picked.tiles });
  if (picked.state !== 'picking') {
    watcher.stop();
    return { watcher, aborted: `wizard ended in ${picked.state}` };
  }

  const tile = page.locator('z-zcp-onboard-wizard .__agent-btn').filter({ hasText: AGENT }).first();
  await tile.click();
  cap.note(`picked agent "${AGENT}"`);

  // authorizing -> auth dialog -> terminal WS attempts
  await waitFor(page, (w) => w.state !== 'picking', { timeout: 30000, what: 'leave picking' }).catch((e) =>
    cap.note('did not leave picking', { err: String(e).slice(0, 120) }),
  );

  cap.note(`watching authorizing state for ${AUTHORIZING_WATCH_MS}ms`);
  const t0 = Date.now();
  let connectedAt = null;
  while (Date.now() - t0 < AUTHORIZING_WATCH_MS) {
    await sleep(1000);
    const w = await readWizard(page);
    if (!connectedAt && w.dlgButtons?.some((b) => /Start Authorization/i.test(b))) {
      connectedAt = cap.ms();
      cap.note('TERMINAL CONNECTED (button enabled)', { at: connectedAt });
      if (process.env.STOP_ON_CONNECT !== '0') break;
    }
    if (w.state === 'launch-ready' || w.state === 'done' || w.state === 'failed') {
      cap.note('fresh path left authorizing -> ' + w.state);
      break;
    }
  }
  cap.note(connectedAt ? 'RESULT: terminal connected' : 'RESULT: terminal NEVER connected (bug reproduced)');

  // H2 decisive test — only meaningful after a real failure. The terminal's
  // own retry budget is ~20 s (5 attempts, 5 s apart); if the container simply
  // was not shell-ready, a re-trigger a minute later connects. Re-triggering
  // via dismiss -> re-pick re-runs the container resolve AND the token mint
  // without a reload, so it isolates readiness from anything a reload changes.
  if (!connectedAt && process.env.H2_RETEST !== '0') {
    cap.note('H2 RETEST: waiting 60s, then re-triggering the dialog WITHOUT a reload');
    await sleep(60000);
    await page.keyboard.press('Escape');
    await sleep(2000);
    const back = await readWizard(page);
    cap.note('H2 RETEST: after dismiss', { state: back.state, dialog: back.dialogPresent });
    const tile2 = page.locator('z-zcp-onboard-wizard .__agent-btn').filter({ hasText: AGENT }).first();
    if (await tile2.count()) {
      await tile2.click();
      cap.note('H2 RETEST: re-picked agent');
      let ok = false;
      const t2 = Date.now();
      while (Date.now() - t2 < 45000) {
        await sleep(1000);
        const w = await readWizard(page);
        if (w.dlgButtons?.some((b) => /Start Authorization/i.test(b))) {
          ok = true;
          break;
        }
      }
      await page.screenshot({ path: join(EVIDENCE, 'fresh-h2-retest.png') });
      cap.note(
        ok
          ? 'H2 RETEST RESULT: CONNECTED after the 60s wait — container readiness (H2) SUPPORTED, H1/H3/H4 dead'
          : 'H2 RETEST RESULT: STILL FAILING after the 60s wait — H2 NOT supported',
      );
    } else {
      cap.note('H2 RETEST: no roster tile after dismiss — cannot re-trigger');
    }
  }
  await snapshotStorage(page, cap, 'fresh-authorizing-end');
  const final = await readWizard(page);
  cap.note('fresh path final', { state: final.state, dialog: final.dialogPresent, dlgButtons: final.dlgButtons });
  await page.screenshot({ path: join(EVIDENCE, 'fresh-zz-final.png') });
  watcher.stop();
  return { watcher, final };
}

async function controlPath(page, cap, stackId, account) {
  const watcher = startStateWatcher(page, cap, 'control');

  cap.note('full page reload -> service detail', { stackId });
  await page.goto(`${BASE}/service-stack/${stackId}`, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await sleep(6000);

  // A freshly-registered session is bounced to /login on any full reload even
  // though its stored token is still valid — log back in and retry the route.
  if (/\/login/.test(page.url())) {
    cap.note('reload landed on /login despite a valid stored token — logging in', { url: page.url() });
    await page.screenshot({ path: join(EVIDENCE, 'control-00a-bounced-to-login.png') });
    const emailInput = page.locator('input[name="email"], input[type="email"]').first();
    if (await emailInput.count()) {
      await emailInput.fill(account.email);
      await page.locator('input[type="password"]').first().fill(account.password);
      // "Login using Passkey" also matches a bare /login/i — be exact.
      await page.getByRole('button', { name: /Login using email/i }).first().click();
      await sleep(9000);
    } else {
      cap.note('on /login but no email field — already authenticated, org chooser expected');
    }
    cap.note('after login', { url: page.url() });
    await page.screenshot({ path: join(EVIDENCE, 'control-00b-after-login.png') });

    // A session whose stored clientUserId was lost lands on the org chooser —
    // picking the org is what re-writes `@zerops/zerops/user-data` in full.
    const orgPicked = await page.evaluate(() => {
      if (!/choose your organization|select.*organization/i.test(document.body.innerText)) return null;
      const el = [...document.querySelectorAll('button,a,[role="button"],mat-card,li')]
        .filter((x) => {
          const s = (x.textContent || '').trim();
          return s.length > 2 && s.length < 60 && x.getBoundingClientRect().height > 10;
        })
        .filter((x) => /^kh/i.test((x.textContent || '').trim()))
        .sort((a, b) => a.textContent.length - b.textContent.length)[0];
      if (el) {
        el.click();
        return (el.textContent || '').trim().slice(0, 40);
      }
      return '(chooser shown, no match)';
    });
    if (orgPicked) {
      cap.note('org chooser handled', { picked: orgPicked });
      await sleep(8000);
      await page.screenshot({ path: join(EVIDENCE, 'control-00c-after-org-select.png') });
    }
    await snapshotStorage(page, cap, 'control-after-login');
    await page.goto(`${BASE}/service-stack/${stackId}`, { waitUntil: 'domcontentloaded', timeout: 60000 });
    await sleep(8000);
  }
  await snapshotStorage(page, cap, 'control-service-detail');
  await page.screenshot({ path: join(EVIDENCE, 'control-00-service-detail.png') });

  // The zagent card's per-agent chip opens a popover carrying the manual
  // "Trigger authorization process" action (the owner's working path).
  const chip = page.locator('.__zagent-chip').filter({ hasText: AGENT }).first();
  let opened = false;
  if (await chip.count()) {
    await chip.click();
    await sleep(1200);
    await page.screenshot({ path: join(EVIDENCE, 'control-01-chip-popover.png') });
    const trigger = page.getByText(/(Trigger|Re-trigger) authorization process/i).first();
    if (await trigger.count()) {
      await trigger.click();
      cap.note('clicked manual "Trigger authorization process"');
      opened = true;
    } else {
      cap.note('popover open but no trigger action found');
    }
  } else {
    cap.note('no .__zagent-chip on service detail');
  }

  if (!opened) {
    // Fallback control: the terminal page mints the same file-browsing-access
    // token and opens the same shell/stream WS.
    cap.note('falling back to /terminal control surface');
    await page.goto(`${BASE}/service-stack/${stackId}/terminal`, {
      waitUntil: 'domcontentloaded',
      timeout: 60000,
    });
  }

  await sleep(30000);
  await page.screenshot({ path: join(EVIDENCE, 'control-zz-final.png') });
  await snapshotStorage(page, cap, 'control-end');
  const final = await readWizard(page);
  cap.note('control path final', { state: final.state, dialog: final.dialogPresent, dlgButtons: final.dlgButtons, term: final.termText?.slice(0, 200) });
  watcher.stop();
  return { watcher, final, opened };
}

// Bonus: if a terminal actually connected, run the two container probes the
// PRD wants for the sibling MCP-startup problem.
async function containerProbe(page, cap) {
  const pane = page.locator('.cdk-overlay-pane, z-terminal').first();
  if (!(await pane.count())) return cap.note('containerProbe: no terminal surface');
  const term = page.locator('.xterm-helper-textarea, .xterm').first();
  if (!(await term.count())) return cap.note('containerProbe: no xterm');
  try {
    await term.click({ timeout: 5000 });
    // `discover` is NOT a zcp CLI verb (cliDispatch in cmd/zcp/main.go) — it
    // falls through to MCP serve mode, so probe the things that actually
    // answer the question. Env is printed as name=<n chars> only: never dump
    // a token into an evidence file.
    const cmds = process.env.PROBE_CMDS_FILE
      ? JSON.parse(readFileSync(process.env.PROBE_CMDS_FILE, 'utf8'))
      : process.env.PROBE_CMDS
      ? JSON.parse(process.env.PROBE_CMDS)
      : [
          'zcp version',
          'env | grep -E "^(ZEROPS_|ZCP_|zerops)" | awk -F= \'{print $1"="length($2)" chars"}\'',
          'zcp serve < /dev/null 2>&1 | head -20',
        ];
    for (const cmd of cmds) {
      await page.keyboard.type(cmd);
      await page.keyboard.press('Enter');
      cap.note('container probe sent', { cmd });
      await sleep(8000);
      await page.screenshot({ path: join(EVIDENCE, `control-probe-${cmd.slice(0, 12).replace(/\W+/g, '_')}.png`) });
    }
    const text = await page.locator('.xterm').first().innerText().catch(() => '');
    cap.note('container probe output captured', { chars: text.length });
    writeFileSync(join(EVIDENCE, `container-probe-${stamp}.txt`), text);
  } catch (e) {
    cap.note('containerProbe failed', { err: String(e).slice(0, 200) });
  }
}

// -------------------------------------------------------------------- main

const accountFile = join(EVIDENCE, 'account.json');
let account;
if (process.env.ACCOUNT_EMAIL) {
  account = {
    email: process.env.ACCOUNT_EMAIL,
    password: process.env.ACCOUNT_PASSWORD,
    org: 'reuse',
    name: 'reuse',
  };
} else if (RUN === 'control' && existsSync(accountFile)) {
  account = JSON.parse(readFileSync(accountFile, 'utf8'));
} else {
  const ep = Math.floor(Date.now() / 1000);
  account = {
    email: `kh-test-${ep}@example.com`,
    password: `Zc${ep}!Aa9x`,
    org: `khtest${ep}`,
    name: `kh test ${ep}`,
  };
  writeFileSync(accountFile, JSON.stringify(account, null, 2));
}
console.log(`account: ${account.email}`);

const browser = await chromium.launch({ headless: HEADLESS, slowMo: SLOW, args: ['--window-size=1680,1050'] });
const context = await browser.newContext({ viewport: { width: 1680, height: 1050 } });
const page = await context.newPage();

const capFresh = new Capture('fresh');
const capCtrl = new Capture('control');
let cap = capFresh;
// One probe/attach for the page; the active capture is swapped between paths.
const proxy = new Proxy(
  {},
  {
    get(_t, prop) {
      const v = cap[prop];
      return typeof v === 'function' ? v.bind(cap) : v;
    },
    set(_t, prop, val) {
      cap[prop] = val;
      return true;
    },
  },
);
await installWsProbe(page, proxy);
await attachCapture(page, proxy);

let stackId = process.env.STACK_ID || null;
const files = {};

try {
  if (RUN === 'fresh' || RUN === 'both') {
    const r = await freshPath(page, capFresh, account);
    files.fresh = saveCapture(capFresh, 'fresh');
    if (r.aborted) console.log(`fresh path aborted: ${r.aborted}`);
  }

  if (!stackId) {
    const hit = [...capFresh.http.values()].find((h) => /\/service-stack\/[^/]+\/file-browsing-access/.test(h.url));
    if (hit) stackId = hit.url.match(/\/service-stack\/([^/]+)\/file-browsing-access/)[1];
  }
  if (!stackId) {
    stackId = await page
      .evaluate(() => {
        const a = [...document.querySelectorAll('a[href*="/service-stack/"]')][0];
        return a ? (a.getAttribute('href').match(/\/service-stack\/([^/?#]+)/) || [])[1] : null;
      })
      .catch(() => null);
  }
  console.log(`stackId: ${stackId || '(unresolved)'}`);

  if ((RUN === 'control' || RUN === 'both') && stackId) {
    cap = capCtrl;
    const r = await controlPath(page, capCtrl, stackId, account);
    if (process.env.PROBE === '1' && r.final?.dialogPresent) await containerProbe(page, capCtrl);
    files.control = saveCapture(capCtrl, 'control');
  }
} catch (e) {
  console.error('DRIVER ERROR', e);
  await page.screenshot({ path: join(EVIDENCE, `error-${stamp}.png`) }).catch(() => {});
  saveCapture(cap, `${cap.label}-error`);
} finally {
  writeFileSync(
    join(EVIDENCE, `run-${stamp}.json`),
    JSON.stringify({ stamp, base: BASE, account: { email: account.email }, stackId, files, run: RUN }, null, 2),
  );
  await browser.close().catch(() => {});
}

// ------------------------------------------------------- inline quick diff

function summarize(c) {
  const j = c.toJSON();
  const fba = j.http.filter((h) => h.url.includes('file-browsing-access'));
  const shell = j.websockets.filter((w) => w.url.includes('shell/stream'));
  return {
    label: j.label,
    fileBrowsingAccess: fba.map((h) => {
      let body = null;
      try {
        body = JSON.parse(h.responseBody || 'null');
      } catch {
        /* non-JSON */
      }
      return {
        t: h.t,
        url: h.url,
        status: h.status,
        postData: h.postData,
        authHeader: trunc(h.requestHeaders?.authorization || h.requestHeaders?.Authorization),
        listUrl: body?.listUrl,
        accessToken: trunc(body?.accessToken),
        accessTokenJwt: decodeJwt(body?.accessToken),
      };
    }),
    shellWs: shell.map((w) => ({
      t: w.t,
      urlRedacted: w.url.replace(/accessToken=([^&]+)/, (_m, t) => `accessToken=${trunc(decodeURIComponent(t))}`),
      accessTokenJwt: decodeJwt(new URL(w.url).searchParams.get('accessToken')),
      containerId: new URL(w.url).searchParams.get('containerId'),
      handshakeStatus: w.handshakeStatus,
      handshakeStatusText: w.handshakeStatusText,
      frameError: w.frameError,
      framesReceived: w.framesReceived || 0,
      closedAt: w.closedAt,
      handshakeRequestHeaders: w.handshakeRequestHeaders,
      handshakeResponseHeaders: w.handshakeResponseHeaders,
    })),
    pageWsCloses: j.pageWebSocketEvents
      .filter((e) => e.ev === 'close' && e.url.includes('shell/stream'))
      .map((e) => ({ t: e.t, code: e.code, reason: e.reason, wasClean: e.wasClean })),
    bootstrapLines: j.console.filter((c2) => /bootstrap=/.test(c2.text)).map((c2) => c2.text.slice(0, 300)),
    errors4xx5xx: j.http.filter((h) => h.status >= 400).map((h) => `${h.status} ${h.method} ${h.url.slice(0, 120)}`),
  };
}

const summary = { fresh: summarize(capFresh), control: summarize(capCtrl) };
writeFileSync(join(EVIDENCE, `summary-${stamp}.json`), JSON.stringify(summary, null, 2));
console.log('\n===== SUMMARY =====');
console.log(JSON.stringify(summary, null, 2));
