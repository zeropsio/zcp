// Wipe-moment identity dump.
//
//   SMOKE=1 node wipe-probe.mjs     -> log in with an existing account and
//                                      verify the hooks fire (the wipe will
//                                      NOT happen; this only proves plumbing)
//   node wipe-probe.mjs             -> fresh registration + claim; dumps the
//                                      activeClientUser vs clientUser-row
//                                      comparison at the instant of the wipe
//
// Env: ACCOUNT_EMAIL / ACCOUNT_PASSWORD (smoke), EVIDENCE_DIR, HEADLESS,
//      STACK_ID (smoke navigation target), WATCH_MS.

import { chromium } from 'playwright';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { installIdentityHooks, devtoolsAlive } from './identity-hooks.mjs';

const BASE = process.env.TATAMI_BASE || 'https://tatami.devel.zerops.dev';
const EVIDENCE =
  process.env.EVIDENCE_DIR ||
  '/Users/macbook/Documents/Zerops-MCP/zcp/plans/tatami-onboarding-auth-2026-08-03/evidence';
const HEADLESS = process.env.HEADLESS !== '0';
const SMOKE = process.env.SMOKE === '1';
const WATCH_MS = Number(process.env.WATCH_MS || 45000);

mkdirSync(EVIDENCE, { recursive: true });
const stamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const t0 = Date.now();
const ms = () => Date.now() - t0;
const events = [];

const ep = Math.floor(Date.now() / 1000);
const account = SMOKE
  ? { email: process.env.ACCOUNT_EMAIL, password: process.env.ACCOUNT_PASSWORD }
  : {
      email: `kh-test-${ep}@example.com`,
      password: `Zc${ep}!Aa9x`,
      org: `khtest${ep}`,
      name: `kh test ${ep}`,
    };

// Wire-level fallback: if the bundle ships no StoreDevtools, the entity dict
// can still be reconstructed from what fed it. Keep every clientUser payload.
const wire = [];

const browser = await chromium.launch({ headless: HEADLESS });
const context = await browser.newContext({ viewport: { width: 1680, height: 1050 } });
const page = await context.newPage();

await installIdentityHooks(page, (rec) => {
  const e = { t: ms(), ...rec };
  events.push(e);
  if (e.kind === 'identity-action') {
    console.log(`[+${e.t}ms] ACTION ${e.type}`);
    console.log(`           userBase: ${JSON.stringify(e.userBase)}`);
    for (const r of e.clientUserRows || []) {
      console.log(
        `           row id=${r.id} clientId=${r.clientId} scalar userId=${r.userId} nested user.id=${r.nestedUserId} ` +
          `email=${r.nestedUserEmail} account=${r.accountName} v=${r.version} MATCH=${r.scalarVsNestedMatch}`,
      );
    }
  } else if (e.kind === 'devtools-alive') {
    console.log(`[+${e.t}ms] DEVTOOLS LIVE — action=${e.type} hasState=${e.hasState}`);
    console.log(`           stateKeys=${JSON.stringify(e.stateKeys)}`);
  } else {
    console.log(`[+${e.t}ms] ${e.kind} ${e.key || e.url || ''} ${JSON.stringify(e.value ?? '')}`.slice(0, 260));
  }
});

// Wire-level capture of every clientUser payload (REST seed + WS frames).
const cdp = await page.context().newCDPSession(page);
await cdp.send('Network.enable', { maxPostDataSize: 65536 });
const pending = new Map();
const respAt = new Map();
cdp.on('Network.requestWillBeSent', (e) => pending.set(e.requestId, e.request.url));
// `Network.responseReceived` is synchronous — this is the true arrival time.
// Timestamping at loadingFinished+getResponseBody instead adds a CDP
// round-trip and makes payloads look LATER than the app actually saw them,
// which is exactly the kind of artifact that inverts a causal ordering.
cdp.on('Network.responseReceived', (e) => respAt.set(e.requestId, ms()));
cdp.on('Network.loadingFinished', async (e) => {
  const url = pending.get(e.requestId);
  if (!url || !/client-user|user\/info|registration/.test(url)) return;
  const tResponse = respAt.get(e.requestId) ?? ms();
  try {
    const b = await cdp.send('Network.getResponseBody', { requestId: e.requestId });
    wire.push({ t: tResponse, tRecorded: ms(), url, body: b.body.slice(0, 8000) });
  } catch {
    /* body already evicted */
  }
});
cdp.on('Network.webSocketFrameReceived', (e) => {
  const p = String(e.response.payloadData || '');
  if (/client-user__/.test(p)) wire.push({ t: ms(), url: '(ws frame)', body: p.slice(0, 8000) });
});

try {
  if (SMOKE) {
    console.log(`SMOKE: logging in as ${account.email}`);
    await page.goto(`${BASE}/login`, { waitUntil: 'domcontentloaded', timeout: 60000 });
    await sleep(4000);
    await page.locator('input[name="email"], input[type="email"]').first().fill(account.email);
    await page.locator('input[type="password"]').first().fill(account.password);
    await page.getByRole('button', { name: /Login using email/i }).first().click();
    await sleep(9000);
    // The org chooser is itself a storeUserData dispatch — a perfect trigger
    // to prove the hooks fire without needing the wipe.
    const picked = await page.evaluate(() => {
      const el = [...document.querySelectorAll('button,a,[role="button"],mat-card,li')]
        .filter((x) => /^kh/i.test((x.textContent || '').trim()) && x.getBoundingClientRect().height > 10)
        .sort((a, b) => a.textContent.length - b.textContent.length)[0];
      if (el) { el.click(); return (el.textContent || '').trim().slice(0, 40); }
      return null;
    });
    console.log(`SMOKE: org chooser -> ${picked ?? '(not shown)'}`);
    await sleep(10000);
    if (process.env.STACK_ID) {
      await page.goto(`${BASE}/service-stack/${process.env.STACK_ID}`, { waitUntil: 'domcontentloaded' });
      await sleep(8000);
    }
  } else {
    console.log(`FRESH: registering ${account.email}`);
    await page.goto(`${BASE}/registration?zcp=true`, { waitUntil: 'domcontentloaded', timeout: 60000 });
    await sleep(3500);
    const texts = await page.locator('input[type="text"]').all();
    await texts[0].fill(account.org);
    await texts[1].fill(account.name);
    await page.locator('input[name="email"]').fill(account.email);
    await page.locator('input[type="password"]').first().fill(account.password);
    events.push({ t: ms(), kind: 'MARK', value: 'submitting registration' });
    await page.getByRole('button', { name: /Register to Zerops/i }).click();
    // Both known wipers bundle a `zefAddError('login-no-accounts',
    // 'NO_ACTIVE_ACCOUNTS', ...)` with the wipe. Its presence separates "a
    // documented wiper fired" from "something else wrote the blob".
    const tEnd = Date.now() + WATCH_MS;
    let sawSnack = null;
    while (Date.now() < tEnd) {
      await sleep(300);
      const s = await page.evaluate(() => {
        const txt = document.body.innerText || '';
        const snackEls = [...document.querySelectorAll('mat-snack-bar-container,.mat-mdc-snack-bar-container,zui-snack,.zef-snack')]
          .map((e) => (e.innerText || '').replace(/\s+/g, ' ').trim()).filter(Boolean);
        return { noAccounts: /No active accounts/i.test(txt), snackEls };
      }).catch(() => ({}));
      if (!sawSnack && (s.noAccounts || (s.snackEls || []).length)) {
        sawSnack = { t: ms(), noAccounts: s.noAccounts, snackEls: s.snackEls };
        events.push({ t: ms(), kind: 'SNACK', value: sawSnack });
        console.log(`[+${ms()}ms] SNACK ${JSON.stringify(sawSnack)}`);
      }
    }
    if (!sawSnack) { events.push({ t: ms(), kind: 'SNACK', value: 'NONE SEEN' }); console.log('NO SNACK / NO "No active accounts" TEXT SEEN'); }
    await page.screenshot({ path: join(EVIDENCE, `wipe-probe-${stamp}.png`) });
    events.push({
      t: ms(),
      kind: 'FINAL',
      value: await page.evaluate(() => ({
        url: location.href,
        userData: localStorage.getItem('@zerops/zerops/user-data'),
      })),
    });
  }
} catch (e) {
  console.error('PROBE ERROR', e);
  events.push({ t: ms(), kind: 'ERROR', value: String(e).slice(0, 400) });
} finally {
  const alive = await devtoolsAlive(page);
  const file = join(EVIDENCE, `wipe-probe-${SMOKE ? 'smoke-' : ''}${stamp}.json`);
  writeFileSync(file, JSON.stringify({ account: { email: account.email }, devtoolsActions: alive, events, wire }, null, 2));
  console.log(`\n===== VERDICT =====`);
  console.log(`devtools channel actions seen: ${alive}  ${alive ? '(StoreDevtools IS in the bundle — full state available)' : '(NO StoreDevtools — wire-level reconstruction is the fallback)'}`);
  console.log(`storage writes captured: ${events.filter((e) => e.kind === 'setItem').length}`);
  console.log(`navigations captured:    ${events.filter((e) => e.kind === 'pushState' || e.kind === 'replaceState').length}`);
  console.log(`identity actions:        ${events.filter((e) => e.kind === 'identity-action').length}`);
  console.log(`wire clientUser payloads: ${wire.length}`);

  // ---- correlated timeline: what fed the entity dict, and what the app wrote
  // to disk, interleaved. The wiping write is the one that drops clientUserId;
  // its stack is the only thing that names the emitter, since the three known
  // ones are excluded by the payloads.
  const rowsFromWire = [];
  for (const w of wire) {
    const grab = (o, src) => {
      if (!o || typeof o !== 'object') return;
      if (o.id && o.clientId && o.user) {
        rowsFromWire.push({
          t: w.t,
          src,
          id: o.id,
          scalarUserId: o.userId,
          nestedUserId: o.user.id,
          email: o.user.email,
          version: o._version,
          match: o.userId === o.user.id,
        });
      }
    };
    try {
      const b = JSON.parse(w.body);
      (b.items || []).forEach((it) => grab(it, w.url));
      (b.clientUserList || b.user?.clientUserList || []).forEach((it) => grab(it, w.url));
      (b.data?.update || b.data?.list || []).forEach((it) => grab(it, w.url));
    } catch {
      /* partial body */
    }
  }
  const writes = events.filter((e) => e.kind === 'setItem' && e.key === '@zerops/zerops/user-data');
  const timeline = [
    ...rowsFromWire.map((r) => ({ t: r.t, kind: 'ENTITY-FEED', detail: r })),
    ...writes.map((w) => ({ t: w.t, kind: 'DISK-WRITE', detail: w })),
  ].sort((a, b) => a.t - b.t);

  console.log('\n===== CORRELATED TIMELINE =====');
  for (const e of timeline) {
    if (e.kind === 'ENTITY-FEED') {
      const d = e.detail;
      console.log(
        `+${String(d.t).padStart(6)}ms  ENTITY-FEED  row=${d.id} v=${d.version} scalarUserId=${d.scalarUserId} nested=${d.nestedUserId} match=${d.match} <${d.email}>  src=${String(d.src).replace(/.*public/, '')}`,
      );
    } else {
      const v = e.detail.value;
      const wiped = v && typeof v === 'object' && !v.clientUserId;
      console.log(
        `+${String(e.detail.t).padStart(6)}ms  DISK-WRITE   user-data=${JSON.stringify(v)}${wiped ? '   <<<<< WIPE' : ''}`,
      );
    }
  }

  // The writer is the same for both writes (`setData`); only the DISPATCHING
  // effect differs. Printing both stacks side by side makes the divergent
  // frame — the emitter we are hunting — read straight off the diff.
  const objWrites = writes.filter((w) => w.value && typeof w.value === 'object');
  const healthy = objWrites.find((w) => w.value.clientUserId);
  const wipe = objWrites.find((w) => !w.value.clientUserId);
  const asArr = (s) => (Array.isArray(s) ? s : s ? [String(s)] : []);
  for (const [label, w] of [['HEALTHY write', healthy], ['WIPING write', wipe]]) {
    if (!w) continue;
    console.log(`\n----- ${label} (+${w.t}ms) stack, ${asArr(w.stack).length} frames -----`);
    asArr(w.stack).forEach((f, i) => console.log(`  ${String(i).padStart(2)}  ${f}`));
  }
  if (healthy && wipe) {
    const a = asArr(healthy.stack);
    const b = asArr(wipe.stack);
    const firstDiff = b.findIndex((f, i) => f !== a[i]);
    console.log(`\n----- first divergent frame (index ${firstDiff}) -----`);
    console.log(`  healthy: ${a[firstDiff] ?? '(none)'}`);
    console.log(`  wipe   : ${b[firstDiff] ?? '(none)'}`);
    console.log(`  frames unique to the WIPING stack:`);
    b.filter((f) => !a.includes(f)).forEach((f) => console.log(`    ${f}`));
  }
  console.log(`\n== wrote ${file}`);
  await browser.close().catch(() => {});
}
