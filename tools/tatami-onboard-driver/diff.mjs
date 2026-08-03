// Mechanical diff of two capture files produced by driver.mjs.
//   node diff.mjs <fresh.json> <control.json>
//
// Compares exactly the fields the investigation turns on: the
// file-browsing-access call (service-stack id, headers, body, response), the
// minted accessToken's JWT claims, the shell/stream WS URL + handshake status
// + close code, and the identity surface (storage keys, cookies, clientId).

import { readFileSync } from 'node:fs';

const [, , freshPath, ctrlPath] = process.argv;
if (!freshPath || !ctrlPath) {
  console.error('usage: node diff.mjs <fresh.json> <control.json>');
  process.exit(2);
}

const trunc = (s, n = 6) =>
  !s ? s : String(s).length <= 2 * n + 3 ? String(s) : `${String(s).slice(0, n)}…${String(s).slice(-n)}`;

function decodeJwt(tok) {
  if (!tok) return null;
  const parts = String(tok).split('.');
  if (parts.length < 2) return { notAJwt: true, len: String(tok).length };
  try {
    const pad = (s) => s + '='.repeat((4 - (s.length % 4)) % 4);
    const b = (s) => JSON.parse(Buffer.from(pad(s.replace(/-/g, '+').replace(/_/g, '/')), 'base64').toString('utf8'));
    return { header: b(parts[0]), payload: b(parts[1]) };
  } catch (e) {
    return { decodeError: String(e).slice(0, 100) };
  }
}

function extract(j) {
  const fba = j.http
    .filter((h) => h.url.includes('file-browsing-access'))
    .map((h) => {
      let body = null;
      try {
        body = JSON.parse(h.responseBody || 'null');
      } catch {
        /* non-JSON body */
      }
      return {
        t: h.t,
        stackId: (h.url.match(/service-stack\/([^/]+)\/file-browsing-access/) || [])[1],
        status: h.status,
        reqBody: h.postData,
        authHeader: trunc(h.requestHeaders?.authorization || h.requestHeaders?.Authorization),
        reqHeaderNames: Object.keys(h.requestHeaders || {}).sort(),
        cookieHeader: h.requestHeaders?.cookie ? trunc(h.requestHeaders.cookie, 24) : '(none)',
        listUrl: body?.listUrl,
        token: trunc(body?.accessToken),
        tokenClaims: decodeJwt(body?.accessToken)?.payload,
        rawResponse: h.status >= 400 ? (h.responseBody || '').slice(0, 500) : undefined,
      };
    });

  const ws = j.websockets
    .filter((w) => w.url.includes('shell/stream'))
    .map((w) => {
      const u = new URL(w.url);
      const tok = u.searchParams.get('accessToken');
      return {
        t: w.t,
        host: u.host,
        containerId: u.searchParams.get('containerId'),
        token: trunc(tok),
        tokenClaims: decodeJwt(tok)?.payload,
        handshakeStatus: w.handshakeStatus ?? '(no handshake response)',
        handshakeStatusText: w.handshakeStatusText,
        upgradeReqHeaders: w.handshakeRequestHeaders,
        upgradeResHeaders: w.handshakeResponseHeaders,
        frameError: w.frameError,
        framesReceived: w.framesReceived || 0,
        closedAt: w.closedAt,
      };
    });

  const closes = j.pageWebSocketEvents
    .filter((e) => e.ev === 'close' && String(e.url).includes('shell/stream'))
    .map((e) => ({ t: e.t, code: e.code, reason: e.reason || '(empty)', wasClean: e.wasClean }));

  const lastStorage = j.storageSnapshots[j.storageSnapshots.length - 1] || {};
  return {
    fba,
    ws,
    closes,
    allWsUrls: j.websockets.map((w) => `${w.handshakeStatus ?? '-'} ${w.url.split('?')[0]}`),
    storageKeys: Object.keys(lastStorage.localStorage || {}).sort(),
    sessionKeys: Object.keys(lastStorage.sessionStorage || {}).sort(),
    cookies: (lastStorage.cookie || '').split('; ').map((c) => c.split('=')[0]).sort(),
    userData: lastStorage.localStorage?.['@zerops/zerops/user-data']?.slice(0, 300),
    errors: j.http.filter((h) => h.status >= 400).map((h) => `${h.status} ${h.method} ${h.url.slice(0, 130)}`),
    failed: j.http.filter((h) => h.failed).map((h) => `${h.failed.errorText} ${h.method} ${h.url.slice(0, 130)}`),
    bootstrap: j.console.filter((c) => /bootstrap=/.test(c.text)).map((c) => c.text.slice(0, 240)),
    bridgeLines: j.console.filter((c) => /code-server bridge/.test(c.text)).map((c) => `${c.t} ${c.text.slice(0, 200)}`),
    states: j.notes.filter((n) => n.msg.startsWith('state ->')).map((n) => `${n.t} ${n.msg}`),
  };
}

const A = extract(JSON.parse(readFileSync(freshPath, 'utf8')));
const B = extract(JSON.parse(readFileSync(ctrlPath, 'utf8')));

const show = (title, a, b) => {
  console.log(`\n### ${title}`);
  console.log('  fresh  :', typeof a === 'string' ? a : JSON.stringify(a, null, 2).replace(/\n/g, '\n           '));
  console.log('  control:', typeof b === 'string' ? b : JSON.stringify(b, null, 2).replace(/\n/g, '\n           '));
};

console.log('===== FRESH vs CONTROL =====');
show('wizard/dialog states', A.states, B.states);
show('POST file-browsing-access', A.fba, B.fba);
show('shell/stream WebSocket', A.ws, B.ws);
show('WS close codes (in-page)', A.closes, B.closes);
show('all websockets (status + url)', A.allWsUrls, B.allWsUrls);
show('localStorage keys', A.storageKeys, B.storageKeys);
show('sessionStorage keys', A.sessionKeys, B.sessionKeys);
show('cookie names', A.cookies, B.cookies);
show('user-data blob', A.userData, B.userData);
show('HTTP >=400', A.errors, B.errors);
show('network failures', A.failed, B.failed);
show('bootstrap= console lines', A.bootstrap, B.bootstrap);
