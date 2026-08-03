# Repro driver findings — experiment #1 (fresh vs control traffic diff)

Author: repro-driver agent (Opus), 2026-08-03. Persisted by the orchestrator.
Driver: `tools/tatami-onboard-driver/`. Evidence: `evidence/` in this directory.
Companion static analysis: `findings-fe.md` (hypotheses H1–H4 referenced below).

> **RETRACTION (2026-08-03, later the same day): the identity-wipe root cause
> named below (`onClientUsersChanges$`) is WRONG — the effect is exonerated.**
> Deeper analysis of the same capture shows every payload feeding the ClientUser
> row is internally consistent (registration t=4096 correct → /user/info t=4746
> correct → search seed t=4827 wholly regressed to the pool owner `_v3` → WS
> t=5220 corrected `_v4`); there is no partial overwrite. Both operands of the
> `app.effect.ts:533` filter denormalize from that same row and regress in
> lockstep, so the filter never passes. `user-base.effect.ts:56-73` (both
> clientUserList payloads in the window: length 1, ACTIVE) and
> `login.effect.ts:92-116` (no `loginSuccess` on registration) are excluded
> too. A full `{clientUserId, userId}` write provably happened (the drain gated
> on it and ran) and a later write dropped `clientUserId` — the wiping writer
> is a FOURTH, still-unidentified path; prime suspect `setUserId` →
> `user-base.reducer.ts:16-46`. Wipe-moment stack-trace instrumentation is
> ready (`wipe-probe.mjs`); blocked on the registration outage. The wipe
> SECTION below is kept for the (correct) capture data it contains; its causal
> attribution is superseded.
> **Later the same day: see the "Offline full-session re-scan" addendum at the
> end of this file — `setUserId` was then hard-eliminated, candidate #1
> (`onLoadUserSuccessHandleClientUsers$`) refuted by the full-session wire, and
> the evidence now points BACK at `onClientUsersChanges$`, firing at the
> CORRECTION frame (t=5220) against a lagging `activeClientUser$`.**

## Q1 — Did the bug reproduce? NO. Zero WS failures in 5 shell-WS attempts.

| run | path | outcome |
|---|---|---|
| `fresh-2026-08-03T08-18-59.json` | **fresh** | **SUCCESS** — WS 101, 27 frames, terminal live, button "Start Authorization" |
| `loop-102315-1/` | **fresh** | **never reached the wizard** — `POST /registration` → 500. No WS attempt at all |
| `control-run2/` | control | driver bug (invented an unregistered account) — no WS |
| `control-run3/` | control | driver bug (`/login/i` matched "Login using Passkey") — no WS |
| `control-run4/` | control | **SUCCESS** — WS 101, 63 frames |
| `mcp-probe/`, `mcp-probe2/`, `mcp-probe3/` | control | **SUCCESS** ×3 — WS 101 each |

Fresh-path attempts: 2 — one succeeded, one died at registration (a 500, not a WS
failure). Total shell-WS attempts across all captures: 5. All 5 got HTTP 101.
None ever closed, none produced a `frameError`, none produced a close code.
No failing-WS capture exists, which constrains what D1/D3 below can prove.

## Q2 — The four discriminator verdicts

**(a) Handshake on failures → UNRESOLVED (no failure captured).** Healthy
baseline for comparison:

```
fresh   created@5770ms   101 @+28ms   open@5797ms   closed=never  frameError=none  27 frames
control created@25690ms  101 @+51ms   open@25740ms  closed=never  frameError=none  63 frames
```

H1 neither confirmed nor denied. The driver now records
`created→handshakeResponse→closed` deltas plus the in-page close **code**
(CDP's `webSocketClosed` carries no code — an in-page WebSocket wrapper supplies
it), so the first failing run settles it unattended.

**(b) containerId byte-compare → IDENTICAL. H3 (structural) DEAD.**

```
fresh   WS containerId = fAEzz301QzyHyOXA3Ou7qA   POST body = fAEzz301QzyHyOXA3Ou7qA
control WS containerId = fAEzz301QzyHyOXA3Ou7qA   POST body = fAEzz301QzyHyOXA3Ou7qA
```

Apples-to-apples: run4's control used the **zagent chip → "Trigger authorization
process"** (the owner's exact manual path). The button flipped "Waiting for
container…" → "Start Authorization" in 320 ms. The two `manualOpen` call sites
resolve the same container when the list is settled. The ES-lag variant of H3
("the owner's read returned a superseded row") is untouched and still needs a
failing capture.

**(c) Container status → ACTIVE, and the claim never touched the row.**

```
POST /container/search (container__…zcp-agent-auth-dialog__list-subscription) → totalHits 1
id=fAEzz301QzyHyOXA3Ou7qA  status=ACTIVE  _version=2
created=2026-08-02T09:02:20Z  lastUpdate=2026-08-02T09:02:20Z
name=zcp-runtime-1-10  instanceId=2PVZ8wkeRlG2ZBrAVsMOow  nestId=tatami
```

`lastUpdate == created`, 23 h before the claim — **the pool claim did not modify
the container row at all**, so a claim per se does not push a container into
`MOVING`. H2 survives only as "the pool had been refreshed shortly before the
owner's run, so the container genuinely was new" — directly testable from
`created` in a failing capture.

Side note: the proxy URLs are keyed by **`instanceId`**
(`list-directory/2PVZ8wkeRlG2ZBrAVsMOow`) while the WS query carries
`containerId`. Worth knowing if those ever disagree.

**(d) 60s-wait-retry-without-reload → COULD NOT RUN (needs a failure), but
automated.** When a fresh run ends unconnected the driver waits 60 s, dismisses
the dialog and re-picks the agent WITHOUT a reload (re-runs both container
resolve and token mint), then writes `H2 RETEST RESULT: CONNECTED …(H2
SUPPORTED, H1/H3/H4 dead)` or `STILL FAILING`. Disable with `H2_RETEST=0`.

**Bonus — H4 DEAD.** Five independent tokens: every one `len=22`, no `+`, `/`,
`=`, no char outside `[A-Za-z0-9_-]` (standard Zerops id format); WS segment not
percent-encoded and `decodeURIComponent(seg) === seg`. The missing
`encodeURIComponent` at `terminal.api.ts:57` is latent, not active.

## Q3 — Bootstrap version, and where the wizard actually stalled

`bootstrap=` is only announced when the code-server embed loads (fresh path
only):

- fresh run, t=17147 ms: `[code-server bridge] embed-ready agents=5 bootstrap=0.1.25`
- container `zcp version`: **`v9.137.0` (9acfcd72, 2026-07-30T09:24:39Z)** — consistent with 0.1.25.

**The pool is NOT on v9.137.1 / 0.1.27.** The relay fix (`45183766`) is absent
from pool containers. This invalidates any launch-step testing done on tatami
since 2026-08-02.

But this does NOT explain the owner's stall:

1. The wizard sat in `authorizing` for the full 75 s watch — correctly:
   `authorizing` only exits on `markAuthorized`, which needs a real OAuth
   completion; the driver never clicked "Start Authorization". The wizard did
   NOT stall on the terminal; the terminal was connected and the button enabled
   the whole time. Zero data on the relay step from these runs.
2. The relay bug and the owner's symptom are at different steps. The relay bug
   kills `agent-ready` AFTER launch. The owner's symptom is "Waiting for
   container…" with a black terminal pane — the AUTH dialog's terminal, before
   authorization. `terminalConnected` is literally
   `TerminalFeature.connectionState().status === 'connected'`, set only when the
   first WS message arrives. "Stuck on Waiting for container…" is a genuine
   shell-WS failure and cannot be the already-fixed relay bug in disguise.

## Q4 — mcp-probe*: container output obtained. Problem #3 is NOT a zcp defect.

- **`zcp discover` does not exist as a CLI verb.** `cliDispatch()`
  (`cmd/zcp/main.go:64-88`) has no `discover` entry and `run()` falls through to
  `runServe()` for anything unrecognised — asking the owner to run `zcp
  discover` started an MCP server that read his keystrokes as JSON-RPC
  (`invalid character 'e' looking for beginning of value`); that's why his
  answer never arrived. Ask for `zcp version` or an MCP tool call instead.
- **Env (names + value lengths only):** `ZCP_API_KEY` present (66 chars), plus
  `ZCP_VSCODE_AUTH_ENABLED`, `zeropsSubdomainHost`, the usual `ZEROPS_*` set.
  Credentials are provisioned.
- **A real MCP handshake succeeds** (`mcp-probe3/container-probe-*.txt`):

```
$ zcp serve <<< '{"jsonrpc":"2.0","id":1,"method":"initialize", …}' | head -c 300
{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"logging":{},"tools":{"listChanged":true}},
 "protocolVersion":"2024-11-05","serverInfo":{"name":"zcp","version":"v9.137.0"}}}
```

Not missing credentials, not stdout pollution (serve repoints
`os.Stdout`→stderr at `cmd/zcp/main.go:291-292`; slog → stderr at
`internal/server/server.go:84`). Split problem #3 into its own follow-up;
reopen with codex's own stderr.

## The `/login` identity wipe — root-caused, and NOT the WS cause

100% reproducible. After a successful registration the SPA sits on `/login` all
session; `localStorage['@zerops/zerops/user-data']` = `{"userId":…}` with no
`clientUserId`; a reload bounces to `/login` again with a valid token and
`/user/info` 200; recovery needs re-login PLUS picking the org.

Cause, from the capture: `POST /client-user/search` at t=4827 (the
list-subscription seed, pre-claim) returned the **pool's placeholder row** —
`_version: 3`, `user.email: zcp.pool@zerops.io`, `client.accountName:
"zcp-pool7"`. The claim rewrites that same row in place (same id
`RACfQqoFT52PCFa41a23Nw`); the corrected `_version: 4` row with the new user
arrives over the websocket at t=5220, ~400 ms too late. In that window
`onClientUsersChanges$` (`app.effect.ts:517-558`) sees a row for the active
clientId but none matching `cu.user.id`, concludes the user was removed, and
fires `storeUserData(id)` + `NO_ACTIVE_ACCOUNTS` + `zefGo(LOGIN_ROUTE)`. Then
`Roles.Authorized` (`app.permissions.ts:27-37`) is false forever — nothing
re-derives `activeClientUserId` from `/user/info`.

The sibling wiper `user-base.effect.ts:56-73` is ruled out: `/user/info`
returned `clientUserList.length: 1`, ACTIVE.

**Provably not the WS cause** — identity was wiped in this capture AND the
terminal connected fine.

## Re-run commands

```sh
cd tools/tatami-onboard-driver && npm install

RUN=both node driver.mjs                       # the experiment (registers a throwaway account)
ACCOUNT_EMAIL=… ACCOUNT_PASSWORD=… RUN=control STACK_ID=wFOabueWTtGVhH3sZ5qdJg PROBE=1 node driver.mjs
./loop.sh 5                                    # N fresh runs, one-line verdict each
node diff.mjs <fresh.json> <control.json>      # mechanical diff
node identity-probe.mjs                        # storage writes + history + NgRx actions (never yet run for real)
```

Env: `HEADLESS=0`, `WATCH_MS`, `AGENT`, `H2_RETEST=0`, `PROBE_CMDS_FILE`,
`EVIDENCE_DIR`, `STACK_ID`. Working account: `evidence/account.json`
(`kh-test-1785745139@example.com`), stack `wFOabueWTtGVhH3sZ5qdJg`, container
`fAEzz301QzyHyOXA3Ou7qA`. Flakiness: `getByRole('button',{name:/login/i})`
matches "Login using Passkey" — use `/Login using email/i`. After the identity
wipe a re-login lands on the **org chooser**, not the dashboard (driver handles
it).

## BLOCKER — tatami registration 500ing since ~08:23 UTC

```
POST https://api.app-tatami.zerops.dev/api/rest/public/registration
→ 500 {"error":{"code":"500","message":"Internal Server Error","meta":null}}
```

8 attempts 08:23–09:0x, with AND without `claimZcpPool: true` — not pool
exhaustion, not claim-specific. One registration succeeded at 08:19; everything
after failed. Blocks every fresh-path run: D1, D3, the H2 re-test, and the
PRD's 5/5 adversarial verification. Needs the owner or a platform check.

## Hypothesis set standing

H3 (structural) dead · H4 dead · H1 unresolved (a missing handshake response in
a failing run would be a strong FE-abort signal — healthy proxy answers in
28 ms) · H2 narrowed to "pool was freshly refreshed" only · `/login` identity
wipe: real, root-caused, not the WS cause.

---

# Addendum: offline full-session re-scan (same day)

Source: `evidence/fresh-2026-08-03T08-18-59.json`, whole session (0..64779 ms,
181 HTTP entries, 4 websockets).

## Candidate #1 (`onLoadUserSuccessHandleClientUsers$` on empty clientUserList) — REFUTED

Exactly ONE `GET /user/info` in the entire session: t=4746, 200,
`clientUserList.length === 1`, `[ACTIVE]` (plus its CORS preflight at t=4741,
no body by design). No second or third call exists; no response body is
missing. The effect's filter needs `length === 0` — impossible on this wire.

Token-refresh timeline: **zero 401/403 anywhere, no refresh-token calls** —
the proposed 401→refresh→setToken→loadUser chain did not occur.

## The wipe is bounded to (5085, 5339] — a ~254 ms window

- t=3976 (`/registration?zcp=true`): `user-data` = empty string.
- Full write `{clientUserId, userId}` landed in (4835, 5085] (drain gates on
  `storeUserDataSuccess` with `!!clientUserId`; state watcher saw `idle`@~4835,
  `claiming`@5085).
- t=5339 (`/login`): `user-data` = `{"userId":"MrhnA2BtTOOnRbb3HAn9QQ"}`.
- Single SPA session (one main-document navigation at t=10) — no boot re-run.

## The only identity-relevant event inside the window: the t=5220 CORRECTION frame

Whole-session sweep: `client-user/search` exactly twice, both t=4827/4828 (the
seed + subscription registration); exactly ONE client-user WS frame in the
session — `client-user__update-subscription` at t=5220 (`_version 4`,
corrected). No `user__*` subscription frames at all.

## Unified hypothesis (inference, one stack trace from proof)

At t=5220 the correction moves `list$()` forward while `activeClientUser$`
lags on the regressed row:

- `list$()` = `[ row _v4, user.id = MrhnA2Bt… ]` (corrected)
- `activeClientUser$` still `_v3` → `userId = x8sKSmJ4…` (pool)
- `app.effect.ts:533`: `cu.user.id (MrhnA2Bt…) === userId (x8sKSmJ4…)` → false
  → `length === 0` → **wipe fires**
- `mergeMap` dispatches `storeUserData(id)` with `id` from `activeUser` =
  `MrhnA2Bt…` → writes exactly the observed blob, byte-identical.

The divergence direction is the REVERSE of the original theory: not the
regression at t=4827 (lockstep there), but the correction at t=5220 — which
also explains the wipe landing ~135 ms after the correcting frame. Candidate
pinning mechanisms: the inverted `distinctUntilChanged` comparator
(`entity-manager-entity.service.ts:345`) or the emission ordering between
`entityById$` and `selectEntityList` on a single store transition → the
highest-value remaining static question (task #8 territory).

Confirmation instrument ready: `wipe-probe.mjs` captures the JS stack at the
wiping `setItem` (names the emitter outright) + the correlated ENTITY-FEED
divergence timeline. Blocked on the registration outage.

Capture-completeness: bodies captured for every /user/info, /client-user/*,
/registration, /project, /service-stack, /container call; the only truncated
frames (t=9106/9126/9160, 2000-char cap) are transaction-debit stats far
outside the window.

---

# Addendum 2: LIVE wipe capture (3/3) — the emitter is `onLoadUserSuccessHandleClientUsers$`

Evidence: `evidence/wipe-live1..3/`. Registration recovered 09:58:40 UTC;
three fresh runs, wipe reproduced 3/3 with identical shape.

## Timing excludes `onClientUsersChanges$` outright

| run | healthy write | /user/info | WIPE | pool row (/client-user/search) | Δ |
|---|---|---|---|---|---|
| 1 | 4694 | 4782 | 4892 | 4906 | wipe 14 ms EARLIER |
| 2 | 4841 | 4933 | 5007 | 5019 | 12 ms EARLIER |
| 3 | 4606 | 4698 | 4827 | 4846 | 19 ms EARLIER |

The untagged `list$()` feeding `onClientUsersChanges$` is seeded by exactly
that `/client-user/search` response — **the effect's input did not exist at
wipe time**. The correcting frame lands far later (5040/5298/5379). This also
kills the "lagging activeClientUser$ at the correction" hypothesis. At the
wipe instant the entity dict was CLEAN — only the `/registration` and
`/user/info` feeds had landed, both `match=true`, both the real user: no field
divergence, no merge-defect signature visible at fire time.

## The snack discriminator fired

`+5042ms SNACK {"noAccounts":true}` — a documented wiper carrying
`zefAddError('login-no-accounts','NO_ACTIVE_ACCOUNTS',…)` fired; combined with
the timing exclusion, the emitter is **`onLoadUserSuccessHandleClientUsers$`
(user-base.effect.ts:56-73)**, triggered by `loadUserSuccess` from the
`/user/info` 74–129 ms before the wipe.

## The contradiction now owned by the FE side

The filter needs `d.data?.clientUserList?.length === 0`, but the wire
`/user/info` body carries `clientUserList` length 1, ACTIVE, OWNER, nested
user matching. Something between the wire and the filter empties the list —
suspects: the normalizr round-trip (`clientUserList: [ClientUser]` → id array
→ denormalize against a dict that can't resolve at that instant) or
`_onLoadUserSuccessAddToCache$` (:75-77) ordering. **The earlier
"exhaustive elimination" (findings-fe Addendum 2) was falsified at its
premise: the filter does NOT see the wire payload.**

## Method note

Stack traces cannot name the dispatcher: uncapped 162-frame stack, first
divergence from the healthy write at frame 72, but frames 24-30 are an rxjs
scheduler flush that severs the originating effect. Attribution here is by
trigger-timing + the snack. The 52-frame depth difference independently
confirms two different dispatch chains.

## Consequence for the fix branch

`kh-client-user-removal-verify` hardens `onClientUsersChanges$` — defensible
on its own merits, but it will NOT stop the wipe tatami actually exhibits.
DO NOT LAND as the causal fix; retarget pending the wire→filter mechanism
analysis. The self-heal slice likely still repairs the damage on a later
clean `loadUserSuccess` (verification in flight).

---

# Addendum 3: full live wave — 8/8 causal correlation; emitter ambiguity partially restored

Partial retraction of Addendum 2's "wrong effect" call (by its author): the
timing (wipe before the parsed pool-row HTTP body) SURVIVED re-instrumentation
with synchronous `Network.responseReceived` stamps (gap shrank 12-19 ms →
8-9 ms) — but the causal evidence went the other way, decisively:

| runs | outcome | what the `/client-user/search` seed returned |
|---|---|---|
| loop-1/4/5, timed-1/2, wipe-live1/2/3 | WIPED | `zcp.pool@zerops.io` (stale pool owner) |
| loop-2/3, timed-3 | HEALTHY | the real `kh-test-…` user |

**Eight for eight: stale seed ⇒ wipe; fresh seed ⇒ healthy** (and the app
navigates correctly to `/project/<id>/service-stacks`). The CAUSE is
unambiguously the stale pool-owner clientUser row — server-side read
staleness, not a client timing race (trigger-timing gap does not correlate:
82 ms healthy vs 84 ms wiped). The wipe is INTERMITTENT: ~70% (6/8 fresh
registrations). The snack (`NO_ACTIVE_ACCOUNTS`) fired on every wiped run,
never on healthy ones.

The 8-9 ms precedence means only that the dispatcher's input is not the parsed
body of that particular HTTP response — a second read of the same stale server
state lands slightly earlier (a listStream push, or a request outside the
capture filter). So the EMITTER is ambiguous again between the two documented
wipers; the CAUSE is not. Mechanism thread → ticket #8 + fe mechanism
analysis (in flight).

## PRD's target WS symptom: still unreproduced — 10/10 handshakes OK

`loop.sh 5`: 5/5 connected on the FIRST attempt (101 @ +26..38 ms, zero
retries/closes/4xx, 23-24 frames, five distinct pool projects, all reached
`authorizing` with a live terminal). Whole investigation: **10 shell-WS
attempts, 10 × 101, 0 failures.** H1-vs-H2 remains unsettled (the H2 re-test
arms only on a failure). The wipe does NOT break the terminal (3 wiped runs
connected fine) — the identity wipe and the owner's WS stall are independent
problems. If the owner can still hit the WS stall, his session differs in
something not yet varied — pool age is the live suspect (H2).

Caveat: loop runs break out at terminal-connect (~8 s), before the embed
announces bootstrap (~17 s) — `STOP_ON_CONNECT=0` to measure version per run.
The only bootstrap measurement remains 0.1.25 / zcp v9.137.0 (pre-fix pool).

## Verification bar for any fix

~70% base rate ⇒ a single green run proves nothing; require ~10 fresh-run
repeats with zero wipes.

## Housekeeping

12 pool projects claimed today (1+3+5+3); no pool refreshes. Registration
healthy through the wave. Driver updated: `identity-hooks.mjs` uncapped
stacks; `wipe-probe.mjs` true response timestamps + snack detection.

---

# Addendum 4 (FINAL): writer confirmed — `app.effect.ts:537` / `onClientUsersChanges$`

The Addendum 2/3 timing exclusion is FULLY RETRACTED: it compared a
CDP-transport timestamp (`Network.responseReceived`, delivered over the CDP
websocket) against a binding-transport timestamp (`setItem` via
`exposeFunction`) — two independent transports with independent latencies,
never valid for ordering. Re-instrumented so both events flow through ONE
channel (a page-side `JSON.parse` wrapper recording exactly what the app
consumed, timestamped identically to the storage writes):

```
parse1  t=4868  PARSE /client-user/search  rows=zcp.pool@zerops.io v=3  →  WIPE t=4871  (+3ms)
parse2  pool row @5120  →  WIPE @5122  (+2ms)
parse3  pool row @4787  →  WIPE @4788  (+1ms)
```

**The app parses the stale pool row 1–3 ms before the wipe, every time.**
The `/client-user/search` body itself is the trigger; no earlier channel
exists or is needed (complete sweep: the only clientUser-bearing payloads are
/registration, /user/info, /client-user/search, then the late WS correction).

Offline verdicts: `/registration` `user.clientUserList[0].id` present 6/6
(registration candidate dead; two-write FULL→WIPED shape in all 8 wiped runs
confirms independently — healthy runs have exactly one FULL write). No
backend `NO_ACTIVE_ACCOUNTS`/error envelope anywhere in any wiped run — the
snack came from the wiper's own `zefAddError('login-no-accounts', …)`, the
4-action `app.effect` branch.

## Proven chain (correlation 11/11, base rate ~70%)

1. `/registration` → correct row → full write `{clientUserId, userId}`.
2. `/user/info` → length-1 ACTIVE → `onLoadUserSuccessHandleClientUsers$`
   provably cannot fire (raw-passthrough proof, findings-fe Addendum 3).
3. `/client-user/search` seed → STALE pre-claim pool row (ES lag).
4. +1–3 ms: `onClientUsersChanges$` fires → `storeUserData(id)` +
   `zefAddError('login-no-accounts')` + `zefGo(LOGIN_ROUTE)` → WIPE.
5. Corrected WS row lands 400–800 ms later; nothing re-derives
   `activeClientUserId` → durable wedge.

Operand note: for the `:533` filter to pass, the `withLatestFrom`-buffered
`activeClientUser` must have read the REAL user while `list$` delivered the
pool row — the divergence RED-2 modeled, now established by elimination on
live capture. WHY the buffer lagged (static subscription-order analysis
predicts lockstep) remains open → entity-cache follow-up (#8), with
`evidence/parse{1,2,3}/` as input.

## Method lesson (for the driver README)

CDP timestamps and page-side timestamps are NOT comparable for ordering —
any cross-channel ordering claim needs a single-channel instrument (the
`JSON.parse` consumption hook in `identity-hooks.mjs` is that instrument).

## Housekeeping

15 pool projects claimed today total. Decisive evidence:
`evidence/parse{1,2,3}/`.
