// CDP-level capture for the tatami onboarding driver.
//
// The browser console cannot show a WebSocket upgrade's HTTP status, and the
// DOM `close` event's code never reaches the console either. Both numbers are
// the point of this investigation, so we take them from two independent
// sources: CDP `Network.webSocket*` for the handshake, and an in-page
// WebSocket wrapper for the close code/reason.

const BODY_INTEREST = [
  'file-browsing-access',
  '/registration',
  '/login',
  '/user',
  '/client-user',
  '/service-stack',
  '/project',
  '/container',
];

const interesting = (url) => BODY_INTEREST.some((p) => url.includes(p));

export class Capture {
  constructor(label) {
    this.label = label;
    this.t0 = Date.now();
    this.http = new Map();      // cdp requestId -> record
    this.ws = new Map();        // cdp requestId -> record
    this.pageWs = [];           // in-page WebSocket wrapper events
    this.console = [];
    this.pageErrors = [];
    this.storage = [];          // named snapshots
    this.notes = [];
    this._bodyWaiters = [];
  }

  ms() {
    return Date.now() - this.t0;
  }

  note(msg, extra) {
    const e = { t: this.ms(), msg, ...(extra ? { extra } : {}) };
    this.notes.push(e);
    console.log(`[${this.label} +${e.t}ms] ${msg}${extra ? ' ' + JSON.stringify(extra) : ''}`);
  }

  toJSON() {
    return {
      label: this.label,
      startedAt: new Date(this.t0).toISOString(),
      http: [...this.http.values()].sort((a, b) => a.t - b.t),
      websockets: [...this.ws.values()].sort((a, b) => a.t - b.t),
      pageWebSocketEvents: this.pageWs,
      console: this.console,
      pageErrors: this.pageErrors,
      storageSnapshots: this.storage,
      notes: this.notes,
    };
  }
}

// Attach CDP capture to a page, including auto-attach to OOPIF/worker targets
// so requests made from inside the *.app-tatami.zerops.dev embed are seen too.
export async function attachCapture(page, cap) {
  const root = await page.context().newCDPSession(page);
  await wireSession(root, cap, 'page');

  // Auto-attach related targets (iframes, workers) flattened onto the same
  // connection; playwright surfaces them as CDPSession children via events.
  try {
    await root.send('Target.setAutoAttach', {
      autoAttach: true,
      waitForDebuggerOnStart: false,
      flatten: true,
      filter: [{ type: 'iframe', exclude: false }, { type: 'worker', exclude: false }],
    });
  } catch (e) {
    cap.note('Target.setAutoAttach unavailable', { err: String(e).slice(0, 120) });
  }

  // Playwright routes child sessions through the browser context; we also
  // hook every new frame's own session as a belt-and-braces measure.
  page.on('frameattached', async (frame) => {
    try {
      const s = await page.context().newCDPSession(frame);
      await wireSession(s, cap, `frame:${frame.url().slice(0, 60)}`);
      cap.note('attached CDP to frame', { url: frame.url().slice(0, 120) });
    } catch {
      /* same-process frames share the page session; nothing to do */
    }
  });

  return root;
}

async function wireSession(session, cap, origin) {
  await session.send('Network.enable', { maxPostDataSize: 65536 }).catch(() => {});
  await session.send('Page.enable').catch(() => {});
  await session.send('Runtime.enable').catch(() => {});

  session.on('Network.requestWillBeSent', (e) => {
    cap.http.set(e.requestId, {
      t: cap.ms(),
      origin,
      requestId: e.requestId,
      method: e.request.method,
      url: e.request.url,
      requestHeaders: e.request.headers,
      postData: e.request.postData,
      hasPostData: e.request.hasPostData,
      resourceType: e.type,
      documentURL: e.documentURL,
      redirectOf: e.redirectResponse
        ? { status: e.redirectResponse.status, url: e.redirectResponse.url }
        : undefined,
    });
  });

  session.on('Network.responseReceived', (e) => {
    const r = cap.http.get(e.requestId);
    if (!r) return;
    r.status = e.response.status;
    r.statusText = e.response.statusText;
    r.responseHeaders = e.response.headers;
    r.remoteAddress = `${e.response.remoteIPAddress}:${e.response.remotePort}`;
    r.protocol = e.response.protocol;
    r.tRes = cap.ms();
  });

  session.on('Network.loadingFinished', async (e) => {
    const r = cap.http.get(e.requestId);
    if (!r || !interesting(r.url)) return;
    try {
      const b = await session.send('Network.getResponseBody', { requestId: e.requestId });
      r.responseBody = b.base64Encoded
        ? `(base64 ${b.body.length}b)`
        : b.body.slice(0, 20000);
    } catch (err) {
      r.responseBodyError = String(err).slice(0, 160);
    }
  });

  session.on('Network.loadingFailed', (e) => {
    const r = cap.http.get(e.requestId);
    if (!r) return;
    r.failed = { errorText: e.errorText, canceled: e.canceled, blockedReason: e.blockedReason };
    r.tRes = cap.ms();
  });

  // ---- WebSocket lifecycle: the numbers the console cannot show ----
  session.on('Network.webSocketCreated', (e) => {
    cap.ws.set(e.requestId, {
      t: cap.ms(),
      origin,
      requestId: e.requestId,
      url: e.url,
      initiatorStack: (e.initiator?.stack?.callFrames || [])
        .slice(0, 4)
        .map((f) => `${f.functionName || '(anon)'}@${f.url.split('/').pop()}:${f.lineNumber}`),
      events: [],
    });
  });

  session.on('Network.webSocketWillSendHandshakeRequest', (e) => {
    const w = cap.ws.get(e.requestId);
    if (!w) return;
    w.handshakeRequestHeaders = e.request.headers;
    w.events.push({ t: cap.ms(), ev: 'handshakeRequest' });
  });

  session.on('Network.webSocketHandshakeResponseReceived', (e) => {
    const w = cap.ws.get(e.requestId);
    if (!w) return;
    w.handshakeStatus = e.response.status;
    w.handshakeStatusText = e.response.statusText;
    w.handshakeResponseHeaders = e.response.headers;
    w.handshakeResponseHeadersText = e.response.headersText;
    w.handshakeRequestHeadersText = e.response.requestHeadersText;
    w.events.push({ t: cap.ms(), ev: 'handshakeResponse', status: e.response.status });
  });

  session.on('Network.webSocketFrameError', (e) => {
    const w = cap.ws.get(e.requestId);
    if (!w) return;
    w.frameError = e.errorMessage;
    w.events.push({ t: cap.ms(), ev: 'frameError', errorMessage: e.errorMessage });
  });

  session.on('Network.webSocketClosed', (e) => {
    const w = cap.ws.get(e.requestId);
    if (!w) return;
    w.closedAt = cap.ms();
    w.events.push({ t: cap.ms(), ev: 'closed' });
  });

  session.on('Network.webSocketFrameSent', (e) => {
    const w = cap.ws.get(e.requestId);
    if (!w) return;
    w.framesSent = (w.framesSent || 0) + 1;
    if (w.framesSent <= 12) {
      w.events.push({ t: cap.ms(), ev: 'sent', payload: String(e.response.payloadData).slice(0, 400) });
    }
  });

  session.on('Network.webSocketFrameReceived', (e) => {
    const w = cap.ws.get(e.requestId);
    if (!w) return;
    w.framesReceived = (w.framesReceived || 0) + 1;
    if (w.framesReceived <= 40) {
      w.events.push({
        t: cap.ms(),
        ev: 'recv',
        payload: String(e.response.payloadData).slice(0, 2000),
      });
    }
  });

  // Console at every level, including console.debug from iframes.
  session.on('Runtime.consoleAPICalled', (e) => {
    const text = (e.args || [])
      .map((a) => (a.value !== undefined ? String(a.value) : a.description || a.type))
      .join(' ');
    cap.console.push({ t: cap.ms(), origin, level: e.type, text: text.slice(0, 4000) });
  });

  session.on('Runtime.exceptionThrown', (e) => {
    cap.pageErrors.push({
      t: cap.ms(),
      origin,
      text: (e.exceptionDetails?.exception?.description || e.exceptionDetails?.text || '').slice(0, 2000),
    });
  });
}

// In-page WebSocket wrapper — the only reliable source of the close CODE.
export async function installWsProbe(page, cap) {
  await page.exposeFunction('__wsReport', (rec) => {
    cap.pageWs.push({ t: cap.ms(), ...rec });
  });
  await page.addInitScript(() => {
    const Orig = window.WebSocket;
    let seq = 0;
    function Patched(url, protocols) {
      const ws = protocols === undefined ? new Orig(url) : new Orig(url, protocols);
      const id = ++seq;
      const u = String(url);
      const report = (rec) => {
        try {
          window.__wsReport({ id, url: u, ...rec });
        } catch {
          /* binding torn down on navigation */
        }
      };
      report({ ev: 'created' });
      ws.addEventListener('open', () => report({ ev: 'open' }));
      ws.addEventListener('error', () => report({ ev: 'error', readyState: ws.readyState }));
      ws.addEventListener('close', (e) =>
        report({ ev: 'close', code: e.code, reason: e.reason, wasClean: e.wasClean }),
      );
      return ws;
    }
    Patched.prototype = Orig.prototype;
    for (const k of ['CONNECTING', 'OPEN', 'CLOSING', 'CLOSED']) Patched[k] = Orig[k];
    window.WebSocket = Patched;
  });
}

export async function snapshotStorage(page, cap, name) {
  const snap = await page
    .evaluate(() => {
      const dump = (s) => {
        const o = {};
        for (let i = 0; i < s.length; i++) {
          const k = s.key(i);
          o[k] = (s.getItem(k) || '').slice(0, 4000);
        }
        return o;
      };
      return {
        url: location.href,
        localStorage: dump(localStorage),
        sessionStorage: dump(sessionStorage),
        cookie: document.cookie,
      };
    })
    .catch((e) => ({ error: String(e).slice(0, 200) }));
  cap.storage.push({ t: cap.ms(), name, ...snap });
  return snap;
}
