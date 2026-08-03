// Identity instrumentation shared by identity-probe.mjs and driver.mjs.
//
// Purpose: capture, at the exact instant the `/login` identity wipe fires, the
// two values that app.effect.ts:533 compares —
//
//   activeClientUser.userId      (the scalar side of the filter)
//   clientUserRow.user.id        (the nested side, per row in the entity list)
//
// Static reading says both denormalize from the same entity dict keyed by the
// same row id, so they should never disagree. Something makes them diverge on
// exactly one row. This dumps both, plus every intermediate version of that
// row, so the divergence is visible rather than inferred.
//
// Primary channel is a stubbed Redux DevTools bridge: NgRx StoreDevtools calls
// send(action, state) with the WHOLE state on every action, which is the only
// way to read the entity dict from outside the bundle. installIdentityHooks()
// reports whether that channel is live (`devtoolsSeen`); if the production
// bundle ships without StoreDevtools it stays dark and the wire-level capture
// in capture.mjs (the /client-user/search bodies + listStream frames) is the
// fallback reconstruction.

export const WIPE_ACTION_RE = /user-data|userdata|no.?active.?accounts|login-no-accounts|client.?user/i;

// Installed before any app script runs. Everything here is page-side.
export async function installIdentityHooks(page, sink) {
  await page.exposeFunction('__ident', (rec) => sink(rec));

  await page.addInitScript(() => {
    const send = (rec) => {
      try {
        window.__ident(rec);
      } catch {
        /* binding torn down mid-navigation */
      }
    };
    // NgRx effects dispatch synchronously through the Actions Subject, so the
    // DISPATCHING effect sits below the Subject-notification frames. V8's
    // default limit of 10 truncates exactly there and leaves only the writer
    // visible — lift it and keep the whole synchronous chain.
    Error.stackTraceLimit = 200;
    const stack = () =>
      (new Error().stack || '').split('\n').slice(2).map((s) => s.trim());

    // ---------------------------------------------------------------- 1. storage
    const WATCH = ['@zerops/zerops/user-data', '@zerops/zef/auth'];
    const setItem = Storage.prototype.setItem;
    Storage.prototype.setItem = function (k, v) {
      if (WATCH.includes(k)) {
        let shape = v === '' ? '(empty string)' : v;
        try {
          const o = JSON.parse(v);
          shape =
            k === '@zerops/zef/auth'
              ? { keys: Object.keys(o), userId: o.userId }
              : { keys: Object.keys(o), userId: o.userId, clientUserId: o.clientUserId };
        } catch {
          /* keep raw */
        }
        send({ kind: 'setItem', key: k, value: shape, path: location.pathname, stack: stack() });
      }
      return setItem.call(this, k, v);
    };

    // ------------------------------------------------- 1b. what the app PARSED
    // CDP timestamps mark when the BROWSER received bytes; they cannot say when
    // Angular's HttpClient actually handed a payload to the store. Wrapping
    // JSON.parse records exactly what the app consumed and when, in the same
    // clock as the storage writes — which is what an ordering argument needs.
    const origParse = JSON.parse;
    JSON.parse = function (text, reviver) {
      const out = origParse.call(this, text, reviver);
      try {
        if (typeof text === 'string' && text.length < 400000 &&
            /roleCode|clientUserList|NO_ACTIVE_ACCOUNTS/.test(text)) {
          const rows = [];
          const scan = (o, d) => {
            if (!o || typeof o !== 'object' || d > 5) return;
            if (Array.isArray(o)) return o.forEach((x) => scan(x, d + 1));
            if ('roleCode' in o && 'clientId' in o) {
              rows.push({ id: o.id, userId: o.userId, nestedUserId: o.user && o.user.id,
                          email: o.user && o.user.email, v: o._version, status: o.status });
            }
            for (const k of Object.keys(o)) scan(o[k], d + 1);
          };
          scan(out, 0);
          send({ kind: 'PARSED', bytes: text.length,
                 hasNoActiveAccounts: /NO_ACTIVE_ACCOUNTS/.test(text),
                 clientUserListLen: (out && out.clientUserList && out.clientUserList.length) ??
                                    (out && out.user && out.user.clientUserList && out.user.clientUserList.length) ?? null,
                 rows });
        }
      } catch { /* never break the app's own parse */ }
      return out;
    };

    // ---------------------------------------------------------------- 2. navigation
    for (const m of ['pushState', 'replaceState']) {
      const orig = history[m];
      history[m] = function (s, t, url) {
        send({ kind: m, url: String(url), from: location.pathname, stack: stack() });
        return orig.apply(this, arguments);
      };
    }

    // ---------------------------------------------------------------- 3. NgRx state
    // Walk the state for anything that looks like a clientUser row and for the
    // user-base slice. Kept generic: the exact slice names are bundle-internal
    // and differ from the source tree's FEATURE_NAME constants after minification.
    const CU_ROW_KEYS = ['clientId', 'roleCode', 'user'];
    const looksLikeClientUserRow = (o) =>
      o && typeof o === 'object' && !Array.isArray(o) && CU_ROW_KEYS.every((k) => k in o);

    function harvest(state) {
      const rows = [];
      let userBase = null;
      const seen = new Set();
      (function walk(node, path, depth) {
        if (!node || typeof node !== 'object' || depth > 6 || seen.has(node)) return;
        seen.add(node);
        if (looksLikeClientUserRow(node)) {
          rows.push({
            at: path,
            id: node.id,
            clientId: node.clientId,
            userId: node.userId,
            nestedUserId: node.user && node.user.id,
            nestedUserEmail: node.user && node.user.email,
            accountName: node.client && node.client.accountName,
            version: node._version,
            status: node.status,
            // the divergence the fix designer is after, computed in place
            scalarVsNestedMatch: node.userId === (node.user && node.user.id),
          });
        }
        if (
          !userBase &&
          ('activeClientUserId' in node || 'activeUserId' in node)
        ) {
          userBase = { at: path, activeUserId: node.activeUserId, activeClientUserId: node.activeClientUserId };
        }
        for (const k of Object.keys(node)) walk(node[k], path ? path + '.' + k : k, depth + 1);
      })(state, '', 0);
      return { rows, userBase };
    }

    let devtoolsSeen = 0;
    const connection = {
      init: () => {},
      subscribe: () => () => {},
      unsubscribe: () => {},
      error: () => {},
      send: (action, state) => {
        devtoolsSeen += 1;
        const type = typeof action === 'string' ? action : action && action.type;
        if (!type) return;
        if (devtoolsSeen <= 3) {
          send({
            kind: 'devtools-alive',
            type,
            hasState: !!state,
            stateKeys: state ? Object.keys(state).slice(0, 40) : null,
          });
        }
        // Always keep a rolling record of the identity-relevant actions.
        if (window.__WIPE_RE.test(type)) {
          const h = state ? harvest(state) : { rows: [], userBase: null };
          send({
            kind: 'identity-action',
            type,
            payload: JSON.stringify(action).slice(0, 600),
            userBase: h.userBase,
            clientUserRows: h.rows,
            path: location.pathname,
          });
        }
      },
    };
    window.__REDUX_DEVTOOLS_EXTENSION__ = {
      connect: () => connection,
      send: () => {},
      disconnect: () => {},
    };
    window.__REDUX_DEVTOOLS_EXTENSION_COMPOSE__ = () => (f) => f;
    window.__devtoolsSeen = () => devtoolsSeen;
  });

  // The regex lives on window so the page-side hook and Node agree on it.
  await page.addInitScript(`window.__WIPE_RE = ${WIPE_ACTION_RE.toString()};`);
}

// Whether the stubbed devtools channel actually received anything — i.e.
// whether this bundle ships StoreDevtools at all.
export async function devtoolsAlive(page) {
  return page.evaluate(() => (window.__devtoolsSeen ? window.__devtoolsSeen() : 0)).catch(() => 0);
}
