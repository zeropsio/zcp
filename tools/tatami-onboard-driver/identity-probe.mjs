// Identity probe: why does a freshly-registered session end up on /login?
//
// The capture in driver.mjs proved the symptom (URL stuck on /login,
// `@zerops/zerops/user-data` holding only `{"userId":…}` with no clientUserId)
// but not the mechanism. This script instruments the three surfaces that can
// answer it, before the Angular app boots:
//
//   1. localStorage.setItem / removeItem  -> every write of the identity blobs,
//      with the JS stack, so we see whether clientUserId was ever written and
//      what overwrote it.
//   2. history.pushState / replaceState   -> every SPA navigation with a stack,
//      so we see who navigates to /login.
//   3. a stub window.__REDUX_DEVTOOLS_EXTENSION__ -> if the bundle ships
//      StoreDevtools, we get the whole NgRx action stream for free.
//
// Run: node identity-probe.mjs        (registers one throwaway account)

import { chromium } from 'playwright';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const BASE = process.env.TATAMI_BASE || 'https://tatami.devel.zerops.dev';
const EVIDENCE =
  process.env.EVIDENCE_DIR ||
  '/Users/macbook/Documents/Zerops-MCP/zcp/plans/tatami-onboarding-auth-2026-08-03/evidence';
const HEADLESS = process.env.HEADLESS !== '0';
const WATCH_MS = Number(process.env.WATCH_MS || 45000);

mkdirSync(EVIDENCE, { recursive: true });
const stamp = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const events = [];
const t0 = Date.now();
const ms = () => Date.now() - t0;

const ep = Math.floor(Date.now() / 1000);
const account = {
  email: `kh-test-${ep}@example.com`,
  password: `Zc${ep}!Aa9x`,
  org: `khtest${ep}`,
  name: `kh test ${ep}`,
};
console.log(`account: ${account.email}`);

const browser = await chromium.launch({ headless: HEADLESS });
const context = await browser.newContext({ viewport: { width: 1680, height: 1050 } });
const page = await context.newPage();

await page.exposeFunction('__probe', (rec) => {
  events.push({ t: ms(), ...rec });
  if (rec.kind !== 'action') {
    console.log(`[+${ms()}ms] ${rec.kind} ${rec.key || rec.url || ''} ${String(rec.value ?? '').slice(0, 120)}`);
  }
});

await page.addInitScript(() => {
  const send = (rec) => {
    try {
      window.__probe(rec);
    } catch {
      /* binding gone across navigation */
    }
  };
  const stack = () =>
    (new Error().stack || '').split('\n').slice(2, 8).map((s) => s.trim()).join(' | ');

  // --- 1. identity storage writes -------------------------------------
  const WATCH = ['@zerops/zerops/user-data', '@zerops/zef/auth'];
  const ls = Storage.prototype.setItem;
  Storage.prototype.setItem = function (k, v) {
    if (WATCH.includes(k)) {
      let shape = v;
      try {
        const o = JSON.parse(v);
        shape =
          k === '@zerops/zef/auth'
            ? `keys=${Object.keys(o).join(',')} userId=${o.userId}`
            : `keys=[${Object.keys(o).join(',')}] userId=${o.userId} clientUserId=${o.clientUserId}`;
      } catch {
        shape = v === '' ? '(empty string)' : String(v).slice(0, 80);
      }
      send({ kind: 'setItem', key: k, value: shape, path: location.pathname, stack: stack() });
    }
    return ls.call(this, k, v);
  };
  const lr = Storage.prototype.removeItem;
  Storage.prototype.removeItem = function (k) {
    if (WATCH.includes(k)) send({ kind: 'removeItem', key: k, path: location.pathname, stack: stack() });
    return lr.call(this, k);
  };

  // --- 2. SPA navigation ----------------------------------------------
  for (const m of ['pushState', 'replaceState']) {
    const orig = history[m];
    history[m] = function (s, t, url) {
      send({ kind: m, url: String(url), from: location.pathname, stack: stack() });
      return orig.apply(this, arguments);
    };
  }
  window.addEventListener('popstate', () => send({ kind: 'popstate', url: location.pathname }));

  // --- 3. NgRx action stream, if StoreDevtools is in the bundle --------
  window.__REDUX_DEVTOOLS_EXTENSION__ = {
    connect: () => ({
      init: () => {},
      subscribe: () => () => {},
      unsubscribe: () => {},
      send: (action) => {
        const type = typeof action === 'string' ? action : action?.type;
        if (!type) return;
        let extra = '';
        if (/user-data|user data|storeUserData|StoreUserData/i.test(type)) {
          extra = JSON.stringify(action).slice(0, 300);
        }
        send({ kind: 'action', type, extra, path: location.pathname });
      },
      error: () => {},
    }),
    send: () => {},
  };
  window.__REDUX_DEVTOOLS_EXTENSION_COMPOSE__ = () => (f) => f;
});

const readState = () =>
  page
    .evaluate(() => ({
      url: location.href,
      userData: localStorage.getItem('@zerops/zerops/user-data'),
      wizard: (() => {
        const w = document.querySelector('z-zcp-onboard-wizard');
        if (!w?.querySelector('.__overlay')) return 'idle';
        if (w.querySelector('.__agent-btn')) return 'picking';
        return w.querySelector('.__msg')?.textContent?.trim() || 'overlay?';
      })(),
      snack: [...document.querySelectorAll('mat-snack-bar-container, .mat-mdc-snack-bar-container, zui-snack')]
        .map((s) => (s.innerText || '').replace(/\s+/g, ' ').trim())
        .join(' || '),
      bodyHasNoAccounts: /No active accounts/i.test(document.body.innerText),
    }))
    .catch(() => ({}));

try {
  await page.goto(`${BASE}/registration?zcp=true`, { waitUntil: 'domcontentloaded', timeout: 60000 });
  await sleep(3500);
  const texts = await page.locator('input[type="text"]').all();
  await texts[0].fill(account.org);
  await texts[1].fill(account.name);
  await page.locator('input[name="email"]').fill(account.email);
  await page.locator('input[type="password"]').first().fill(account.password);
  events.push({ t: ms(), kind: 'MARK', value: 'submitting registration' });
  await page.getByRole('button', { name: /Register to Zerops/i }).click();

  const t1 = Date.now();
  let lastSnack = '';
  while (Date.now() - t1 < WATCH_MS) {
    await sleep(500);
    const s = await readState();
    if (s.snack && s.snack !== lastSnack) {
      lastSnack = s.snack;
      events.push({ t: ms(), kind: 'SNACK', value: s.snack.slice(0, 300) });
      console.log(`[+${ms()}ms] SNACK ${s.snack.slice(0, 200)}`);
    }
    if (s.bodyHasNoAccounts) {
      events.push({ t: ms(), kind: 'NO_ACTIVE_ACCOUNTS visible' });
    }
    if (s.wizard === 'picking') {
      events.push({ t: ms(), kind: 'MARK', value: `picking; url=${s.url}; userData=${s.userData}` });
      console.log(`[+${ms()}ms] picking; url=${s.url}`);
      await page.screenshot({ path: join(EVIDENCE, `probe-picking-${stamp}.png`) });
      break;
    }
  }
  await sleep(6000);
  const fin = await readState();
  events.push({ t: ms(), kind: 'FINAL', value: JSON.stringify(fin).slice(0, 600) });
  await page.screenshot({ path: join(EVIDENCE, `probe-final-${stamp}.png`) });

  // Does a reload repair it? (the owner's control path)
  events.push({ t: ms(), kind: 'MARK', value: 'RELOAD' });
  await page.reload({ waitUntil: 'domcontentloaded' });
  await sleep(9000);
  const afterReload = await readState();
  events.push({ t: ms(), kind: 'AFTER_RELOAD', value: JSON.stringify(afterReload).slice(0, 600) });
  console.log('AFTER RELOAD', JSON.stringify(afterReload).slice(0, 400));
  await page.screenshot({ path: join(EVIDENCE, `probe-after-reload-${stamp}.png`) });
} catch (e) {
  console.error('PROBE ERROR', e);
  events.push({ t: ms(), kind: 'ERROR', value: String(e).slice(0, 400) });
} finally {
  const file = join(EVIDENCE, `identity-probe-${stamp}.json`);
  writeFileSync(file, JSON.stringify({ account: { email: account.email }, events }, null, 2));
  console.log(`\n== wrote ${file}`);
  console.log('\n===== IDENTITY TIMELINE =====');
  for (const e of events) {
    if (e.kind === 'action') continue;
    console.log(
      `+${String(e.t).padStart(6)}ms  ${e.kind.padEnd(12)} ${e.key || e.url || ''} ${String(e.value ?? '').slice(0, 160)}`,
    );
    if (e.stack) console.log(`               stack: ${e.stack.slice(0, 300)}`);
  }
  const actions = events.filter((e) => e.kind === 'action');
  console.log(`\n===== NGRX ACTIONS (${actions.length}) =====`);
  actions.forEach((a) => console.log(`+${a.t}ms ${a.type} ${a.extra || ''}`));
  await browser.close().catch(() => {});
}
