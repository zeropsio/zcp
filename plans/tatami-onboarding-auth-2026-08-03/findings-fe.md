# FE identity/session investigation — findings

Author: fe-investigator agent (Opus), 2026-08-03. Static read-only analysis of
`../frontend-legacy` @ `kh-agent-first-onboarding` (e065c5341). Persisted by the
orchestrator. Companion evidence: `findings-repro.md` (CDP traffic diff).

## Headline: the HTTP identity CANNOT differ between the two paths

Every credential the app can attach to a request: exactly one, the Bearer read
per-request from localStorage by `libs/zef/src/auth/auth-token.interceptor.ts:42`.
A full page reload does NOT re-mint/refresh/re-scope it — `onCheckToken$`
(`libs/zef/src/auth/auth.effect.ts:97-138`) only VALIDATES the stored token and
re-emits it unchanged. No clientId header, no cookie; the only other interceptor
(`interceptors/api-base.interceptor.ts`) just rewrites `/api` → apiUrl.

So the owner's hypothesis read literally as "the HTTP identity differs" is NOT
supported by the code. What genuinely differs across the reload is (a) WHICH
CONTAINER ID GOT LATCHED and (b) HOW LONG THE POOL CONTAINER HAD BEEN SETTLING.

Second, and it reframes the whole investigation: **"WebSocket is closed before
the connection is established." is Chrome's warning for THE PAGE calling
`close()` while `readyState===CONNECTING`.** A server rejection gives a
different string ("Error during WebSocket handshake: Unexpected response code:
401") — exactly what the PRD's own bogus-token probe produced. rxjs 7.8.2 does
that close on teardown (`node_modules/rxjs/src/internal/observable/dom/WebSocketSubject.ts:378-395`
closes at readyState 0 or 1), then `onclose(1006)` → `observer.error` → `retry()`.

## Data flow: what the WS URL is built from

`feature/terminal/terminal.api.ts:50-58` builds
`wss://${host}/api/rest/shell/stream?accessToken=${token}&containerId=${containerId}`

- `host` ← `extractApiHost(listUrl)` (`terminal.api.ts:60-66`); `listUrl` ←
  POST `/api/service-stack/{id}/file-browsing-access` response body. Platform-chosen.
- `accessToken` ← same POST response; POST authenticated ONLY by the
  localStorage bearer. Response also carries `expiration`, which `auth$` discards.
- `containerId` ← dialog meta ← `zcp-agent-auth-dialog.effect.ts:243-254`,
  `containers?.[0].id` under `take(1)`.
- The SAME containerId is sent TWICE: POST body (`terminal.api.ts:21-22`) and WS query.

Both paths converge on the SAME effect `onManualOpenLoadAndOpen$`. Wizard path
dispatches `manualOpen` from `feature/code-server-overlay/code-server-overlay.feature.ts:389-430`;
manual service-card path from `feature/zagent-service-card/zagent-service-card.feature.ts:365-369`.
The clientIds are equivalent and NEITHER reaches the WS. The only thing the two
paths can disagree about is which container row was first in the store when
`take(1)` fired.

Load-bearing: `retry` wraps the whole inner pipe INCLUDING `auth$`
(`terminal.feature.ts:308-314`, `:419-433`), so each retry re-runs the POST →
fresh token, SAME containerId. That is precisely the observed 3×-fresh-token
signature and proves the failing input is stable in memory. Attempts at
t=0,+5s,+10s,+15s,+20s ⇒ TOTAL RETRY BUDGET ~20s. Anything slower than that
fails all five and looks permanent while a reload minutes later works — so
"a timing race would self-heal" is NOT a safe elimination.

## Ranked hypotheses + traffic signatures

### H1 — The FE aborts the handshake
Socket closed by rxjs teardown at readyState 0, from either the
connection-trigger tap completing the previous subject (`terminal.feature.ts:407-409`)
or `takeUntilDestroyed` (`:440`). The dialog's `<z-terminal>` lives inside a
zef-dialog whose content is a TemplateRef projected into a MatDialog CDK overlay
(`libs/zef/src/dialog/dialog.component.ts:64-72`), created/destroyed with the
overlay, gated on `@if (state.serviceStackId && state.containerId && terminalReady())`
where `state` is a plain mutable sink written key-by-key by `$connect`
(`libs/zef/src/core/reactive-component-base.ts:33-57`). Fits: each abort → 1006
→ retry → fresh POST → fresh token → same containerId; stable within the session
because the aborting condition is a property of the component tree (wizard layer
+ prewarmed fullscreen overlay exist only on the fresh path).

CAVEAT, stated plainly: no repeating remount trigger was located by reading
alone — `terminalReady` is set once, the walker remount only runs post-success,
the close-reset pipe is gated on a real open→closed transition. H1 leads on the
strength of the console string, not on a located trigger.

**Signature**: no `webSocketHandshakeResponseReceived` at all (or a 101), and
`webSocketClosed` within ~100ms of `webSocketCreated`. DENIED by any 401/403
handshake response.

### H2 — Container not shell-ready, and nothing in the FE waits for it
Three missing gates: (1) `containers?.[0]` has no status filter
(`zcp-agent-auth-dialog.effect.ts:243-254`); (2) the dialog mounts `<z-terminal>`
with NO `[containers]` (`zcp-agent-auth-dialog.feature.html:436-441`), putting
TerminalFeature in "simple mode" where `#containerActive$` degenerates to
`of(true)` (`terminal.feature.ts:123-130`) so it connects blind — every other
multi-container call site passes `[containers]`; (3) the drain advances on the
stack ROW appearing, since `isControlPlaneService` is type-only with no status
check (`core/zerops-services/zerops-services.utils.ts:90-91`, used at
`zcp-pool-claim-base.effect.ts:116-119`). Note commit `a9b1fa7eb` set the active
set to "not in {DELETING, STOPPED, MOVING}" — MOVING is plausibly the state a
just-transferred pool project's container sits in. The POST is a control-plane
call that succeeds regardless of runtime state; the shell proxy needs a live
PTY. Strongest explanation that requires NO identity difference at all.

**Signature**: read `status` from the container listStream frame — fresh path
should show something other than ACTIVE. CHEAPEST DECISIVE TEST OF ALL: after a
fresh failure, wait 60s and click Retry WITHOUT reloading. If it connects, H2 is
proven and H1/H3/H4 are dead.

### H3 — Stale containerId latched from a lagging list
`take(1)` (`zcp-agent-auth-dialog.effect.ts:246`) latches the first container;
the id lives in dialog meta for the whole session and the retry loop never
re-resolves it. Zerops list/search endpoints are ES-backed and lag writes, so a
read seconds after a claim can return a row the claim already superseded. POST
still succeeds (containerId is optional in `auth$`'s signature,
`terminal.api.ts:16`, and the endpoint is stack-scoped) while the WS cannot
attach.

**Signature — instant byte-compare**: if the fresh path's WS containerId differs
from the control path's, H3 is CONFIRMED. Identical ⇒ H3 is dead. (PRD records
the failing one: `Paorv4qvSWO9iRJvmXBSjw`.)

### H4 — No encodeURIComponent on the token
`terminal.api.ts:57` interpolates `?accessToken=${token}` raw; a `+` in a
standard-base64 token decodes server-side as a space → 401. Last because it
would be flaky rather than deterministic and would break the control path about
as often. One grep of the captured URLs settles it.

### Demoted — "wrong clientId / token minted for the wrong identity"
Unreachable in this code. clientId is passed ONLY to the container
listSubscribe search (`zcp-agent-auth-dialog.effect.ts:190-202`); it never
reaches `file-browsing-access` and never reaches the WS. A wrong clientId
returns an EMPTY list → the 10s timeout at `:262-267` →
`manualOpenResult{ok:false}` → `authDialogUnavailable()` → the wizard's FAILED
screen. The dialog opened with a terminal, so the list resolved and the clientId
was good. `storeUserDataSuccess`/`activeClientUser$` only gate WHEN the drain
runs (`zcp-pool-claim-base.effect.ts:57-62`, `:120-122`); nothing downstream
reads them.

## Other anomalies (log, not causal)

- `expiration` from file-browsing-access is discarded (`terminal.api.ts:24-27`)
  — the terminal never knows when its token dies.
- The shell WS host is inferred from the FILE-BROWSING `listUrl`
  (`terminal.api.ts:60-66`) — undeclared coupling; on an unparseable listUrl,
  `extractApiHost` returns `''` and the code only console.warns, then builds a
  same-origin `wss:///api/rest/shell/stream?...`
- `#getInputChanges$` (`terminal.feature.ts:267-274`) filters on serviceId only,
  so an undefined containerId would still trigger a connect with the query param
  silently omitted; today's template `@if` prevents it, but the guard is in the
  wrong layer.
- `list$` is called with no orderSelector, so "first container" is
  store-insertion order, not `number` order (`zcp-agent-auth-dialog.effect.ts:243-252`
  warns on >1 and takes the first).

## Decision procedure (for root-cause synthesis)

1. Read the CDP handshake status — splits H1 from H2/H3/H4 in one shot.
2. Byte-compare the two containerIds — settles H3.
3. Read `container.status` from the fresh path's listStream frame — settles H2.
4. Run the 60s wait-then-Retry-without-reload experiment — proves or kills the
   entire timing branch in a minute.

---

# Addendum (same day): the fourth-writer hunt — identity wipe

After the `onClientUsersChanges$` exoneration (see `findings-repro.md`
retraction), a closed enumeration of writers of localStorage
`@zerops/zerops/user-data`:

**The enumeration is closed.** `_updateStorage` (user-base.utils.ts:75-81) has
two callers: `setData()` — reachable ONLY via the `storeUserData` action
(`_onStoreUserData$`, user-base.effect.ts:84-91) — and `removeData()`
(clearUserData + zef auth boot mismatch, both write `''`, never `{"userId":…}`).
An end state `{"userId": X}` without clientUserId can therefore ONLY be produced
by `storeUserData(X, undefined, …)` (JSON.stringify drops undefined keys).
Complete `storeUserData` call-site list: app.effect.ts:537 (1-arg),
user-base.effect.ts:61 (1-arg), and 12 two-arg sites (all sourcing a defined
ClientUser id — login/oauth pages+effects, org switchers, registration.effect).

**`setUserId` — eliminated, hard disproof.** Its reducer branch
(user-base.reducer.ts:16-22) sets `activeUserId` only, never touches
`activeClientUserId`; no effect listens to it; setData unreachable from it.

**Ranked candidates:**

1. **`onLoadUserSuccessHandleClientUsers$` (user-base.effect.ts:56-73) on a
   THIRD, unexamined `/user/info` transiently returning `clientUserList: []`.**
   Filter `d.data?.clientUserList?.length === 0`; output `storeUserData(id)`
   (1-arg) + NO_ACTIVE_ACCOUNTS + `zefGo(LOGIN_ROUTE, {}, {onSameUrlNavigation:
   'reload'})` — matches ALL observed artifacts including "reload bounces to
   /login". Plausible trigger chain: 401 during the claim (server rewriting the
   membership) → refreshToken → `setToken` (zef auth.effect.ts:53-56) →
   `_onSetTokenLoadUser$` (app.effect.ts:175-178) → extra `loadUser()`. The
   earlier exclusion inspected only the two payloads in the t≈4-6s window.
   Wire-observable confirmation: any `/user/info` in the WHOLE session with
   `clientUserList.length === 0`.
2. **`onClientUsersChanges$` (app.effect.ts:517-558)** — kept only because its
   exclusion is derived, not observed. The lockstep argument survived the
   strongest attack available: rxjs 7.8.2 `withLatestFrom` subscribes its
   inputs BEFORE the source (withLatestFrom.ts:76-98), so `activeClientUser$`
   updates before `list$` emits — no sampling skew.
3. **Org switcher** (app-bar.container.ts:186-189,
   client-user-select-pop.container.ts:32-34) — near-dead: needs a real click
   and a clicked ClientUser has a defined id. Promoted instantly if a
   spontaneous full-page `window.location.replace('/dashboard/projects')` is
   observed (its `refresh: true` → app.effect.ts:491-496 hard reload).

**Discriminator #1-vs-#2 (no stack trace needed):** app.effect.ts:534-556
dispatches FOUR actions including `userEntity.updateCache` (:545-549);
user-base.effect.ts:59-72 dispatches THREE with NO update-cache. Presence of a
`user @ update-cache` action between `storeUserData` and `zefAddError` ⇒ #2;
absence ⇒ #1. On the wire alone: a `/user/info` with an empty list ⇒ #1.
Predicted wipe-write stack for #1: distinguishing frame `user-base.effect.ts`
in BOTH the writer and source effect; for #2 the source frame is
`app.effect.ts`.

**Comparator inversion CONFIRMED** (`entity-manager-entity.service.ts:345`):
`disctingFn = (a, b) => a && b && a.id !== b.id` under `distinctUntilChanged`
(which emits when the comparator returns FALSE) suppresses emission exactly
when the id CHANGES — an `entityById$` subscriber stays pinned to the first
row it saw; only in-place updates propagate. Identity-critical subscribers:
`activeUser$`, `activeClientUser$`, `activeClientIdRaw$`→`activeClientId$`→
`isContabo$`, `activeUserClientUsers$` (user-base.entity.ts:19-40). An org
switch would keep the OLD org in the app-bar/roles/loads — currently MASKED
because every org switch passes `refresh: true` → full
`window.location.replace` rebuilds all state. **That hard reload is
load-bearing**: do not optimise it away before fixing the comparator.
`take(1)` callers (code-server-overlay.feature.ts:289-292,
zcp-agent-auth-dialog.feature.ts:102-113) unaffected. → feeds the
entity-cache follow-up ticket.

---

# Addendum 2: the pinning question — negative result, and the elimination that pins the emitter anyway

## No pinning mechanism exists in the modelled code

Both operands of `onClientUsersChanges$` are `combineLatest` over the same two
cold fields on the same `ClientUserEntity` instance (`list$`:
entity-manager-entity.service.ts:302-334; `entityById$`: :342-370;
`_entities$` = cold `store.pipe(select(selectEntities))`, :296 — fresh store
subscription per subscriber, no shareReplay/refCount anywhere in
libs/zef/src/entities, no scheduler hops). `withLatestFrom` subscribes its
inputs BEFORE the source (rxjs 7.8.2), and a Subject notifies in insertion
order — so on the t=5220 correction dispatch `activeClientUser$` updates its
buffer BEFORE `list$` emits: corrected+corrected, filter matches, NO wipe.
Deterministic, and it kills the correction-edge sampling hypothesis the same
way it killed the regression-edge one. `entityById$`'s extra
`distinctUntilChanged` FILTERS, it does not DEFER (synchronous pass-through).

Comparator note (for ticket #8): the inverted `disctingFn` cannot pin an
IN-PLACE same-id update (ids equal → emits). Its real teeth: rxjs updates
`previous` only on EMISSION, so one suppressed different-id emission leaves
every later comparison against the stale id — permanent pinning after a
single different-id transition. Not triggerable in (5085, 5339] (selector and
row id both stable).

## Yet the emitter IS pinned — by exhaustive elimination on the wire

1. Writer enumeration closed: `{"userId":X}` only from 1-arg
   `storeUserData(X, undefined)` (see Addendum 1).
2. `user-base.effect.ts:61` refuted: needs `clientUserList.length === 0`; the
   whole session has exactly one `/user/info` (length 1, ACTIVE).
3. Org-switcher (the only delayed-dispatch path whose cause could sit outside
   the wipe window): its `refresh: true` → `_onHandleStoreUserDataSuccess$` →
   `window.location.replace('/dashboard/projects')`; the session has a single
   main-document navigation (t=10). Excluded.
4. Remaining: `app.effect.ts:537` — `onClientUsersChanges$`.

Conclusion: the emitter is `onClientUsersChanges$`; the MECHANISM by which the
`:533` filter passes is unmodelled (candidates: an unknown same-tick anomaly,
or field divergence `cu.user.id` vs `userId` from a merge defect — the live
stack + operand `_version` dump decides; if both operands read `_v4` at fire
time it is the merge defect and lands in ticket #8).

## Fix recommendation (adopted)

Verify-before-wipe stays the causal fix — it defends against WRONG CACHED DATA,
which is PROVEN (the seed put a wholly pool-owned row in the cache for
~400 ms); glitch-free single-selector derivation defends against inconsistent
sampling, which is unproven, has app-wide blast radius, and belongs in ticket
#8. Plain `combineLatest` of the two streams would NOT be a fix (it glitches
identically); the only correct hardened form is one
`store.select(createSelector(...))` returning the whole tuple from one state
snapshot (feasible: `CollectionManagerService.getSchema` is synchronous).
Principle: a destructive, session-irreversible action must never fire on
cached projection state alone.

---

# Addendum 3: the contradiction dissolved — raw-wire passthrough + the generic snack emitter

## `d.data` IS the raw wire body — no transformation exists

`UserBaseApi.load$()` = plain `this._http.get<User>('/api/user/info')`
(user-base.api.ts:9-11) → `_onLoadUser$` maps the response straight into
`loadUserSuccess(response, …)` (user-base.effect.ts:45-54) →
`zefActionSuccessPayload` is a pure passthrough (`{ data: response, meta,
originalAction }`, libs/zef/src/core/action.ts:30-36). No normalization,
no denormalization, no entity dict. NgRx subscribes effects in
property-definition order, so `onLoadUserSuccessHandleClientUsers$` (:56)
runs BEFORE `_onLoadUserSuccessAddToCache$` (:75). ⇒ With a length-1 wire
payload the `length === 0` filter is FALSE — **that effect provably did not
fire.** (Third falsified attribution, same defect each time: a premise the
wire evidence contradicts.)

## The snack is NOT proof of a documented wiper

`ErrorsEffect._onActionWithErrorAdd$` (libs/zef/src/errors/errors.effect.ts:16-63)
converts ANY failed zef action into `zefAddError(actionType, code, message, …)`
rendered by the same snack pipeline — so a BACKEND response carrying
`{code:'NO_ACTIVE_ACCOUNTS'}` produces the identical snack with no FE wiper
involved. **The snack and the disk write need not share a source.**
Discriminator: the `zefAddError` KEY — `'login-no-accounts'` ⇒ documented
wiper; an action-type key ⇒ generic backend-error path.
`login.effect.ts:86-116` is independently excluded on the registration path
(gates on `loginSuccess`, never dispatched; and its zero-account branch
dispatches NO `storeUserData` at all).

## New candidate examined for the disk write — and refuted by write count

`registration.effect.ts:69-75` emits `storeUserData(id, clientUserList[0].id)`
— if the pool-claim registration payload lacked `clientUserList[0].id`, this
would write `{"userId":X}` in ONE write with no wiper and no snack. But the
live captures show TWO writes per wiped run (full ~4606 → wiped ~4827), which
this mechanism cannot produce. Offline confirmation queued
(`/registration` body: `clientUserList[0].id` presence).

## Branch impact (adopted)

- **Self-heal: correct under EVERY live hypothesis — lands.** At the wipe
  instant it does not fire (its `!activeClientUserId` gate is still truthy
  then); on the NEXT `loadUser()` (boot / setToken) with a clean payload it
  heals. Writer-agnostic.
- **ClientUserRemovalEffect slice: lands as HARDENING, not as "the causal
  fix"** — the branch it owns is genuinely destructive on unverified cached
  state; verification is right regardless of whether it caused this incident.
  Its trigger (WS/list cache) and verifier (`/user/info`) are genuinely
  independent sources — Codex's "one re-read is not verification" concern does
  not apply to it (it WOULD apply to any guard bolted onto a `/user/info`-
  triggered effect).
- No guard on `onLoadUserSuccessHandleClientUsers$` — it provably did not
  fire; that would be a fix for a bug that does not exist.
- Causal-writer identification: reduced to three offline checks on existing
  evidence (registration body; error envelope in window ⇒ snack provenance;
  a pre-HTTP stale client-user read channel that would rescue the
  `app.effect.ts:537` attribution).
