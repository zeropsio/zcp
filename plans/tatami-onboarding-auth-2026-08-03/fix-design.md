# Fix design — /login identity wipe (approved GO-WITH-CHANGES)

> **UN-HELD (2026-08-03, after a same-day hold): the causal slice proceeds.**
> The hold was placed when the `onClientUsersChanges$` attribution was
> retracted (lockstep exoneration). It is lifted because the emitter is now
> pinned by EXHAUSTIVE ELIMINATION on observed wire data: the writer
> enumeration is closed (`{"userId":X}` only producible by a 1-arg
> `storeUserData`); `user-base.effect.ts:61` is refuted (needs an empty
> clientUserList payload — the whole session has exactly one `/user/info`,
> length 1 ACTIVE); the org-switcher path is excluded (its `refresh: true`
> forces `window.location.replace` — the session has a single main-document
> navigation at t=10). That leaves `app.effect.ts:537` inside
> `onClientUsersChanges$` — exactly what this design targets. The same-tick
> MECHANISM by which the `:533` filter passes remains unproven (static
> ordering analysis argues against every modelled variant; live
> `wipe-probe.mjs` stack capture pending platform recovery) — the design is
> deliberately robust to all candidate mechanisms, including a field-divergence
> merge defect (that variant lands in ticket #8). Glitch-free single-selector
> derivation was evaluated and scoped to ticket #8, NOT this fix: it defends
> against inconsistent sampling (unproven here), while verify-before-wipe
> defends against wrong cached data (proven — the ES-lagged seed put a wholly
> pool-owned row in the cache for ~400 ms; a glitch-free read of a wrong cache
> still wipes). Principle: a destructive, session-irreversible action must
> never fire on cached projection state alone. Both slices land on FE branch
> `kh-client-user-removal-verify`.

Designer: fe-investigator (Opus). Reviewer: Codex (second opinion, 2026-08-03,
verdict GO-WITH-CHANGES). Evidence base: `findings-repro.md` (capture) +
`findings-fe.md` (static analysis). Implementer: Sonnet subagent, RED-first.

## Approved shape

**Verify-before-wipe with sole ownership in a new focused effect.**

1. NEW pure module `apps/zerops/src/modules/app/client-user-removal.ts`:
   - `suspectsRemoval(clientUsers, activeClientUser)` — today's
     `app.effect.ts:528+:533` logic moved verbatim (row for the active clientId
     exists, none matches the active user id).
   - `confirmsRemoval(userInfo, ctx)` — TRUE only if a well-formed `/user/info`
     payload for the SAME user carries no ACTIVE membership for the active
     clientId. STRICT FAIL-SAFE: malformed/absent `clientUserList`, missing
     ids, or a payload for a different user ⇒ FALSE (never wipe on bad data).
2. NEW focused `ClientUserRemovalEffect` registered beside `AppEffect`; the
   wipe branch is DELETED from `onClientUsersChanges$` (sole ownership; the
   `user-base.effect.ts:56-73` sibling keeps only its `/user/info`-driven
   zero-list case and never sees verification traffic).
   Flow: suspicion (same filters, via `suspectsRemoval`) → `exhaustMap` →
   **direct `UserBaseApi.load$()`** (NOT a `loadUser` action round-trip — no
   global `loadUserSuccess` side effects, no dialog-style error surfacing, no
   correlation-by-convention) → `timeout(10s)` → context revalidation →
   `confirmsRemoval` → the existing wipe sequence; inner
   `catchError(() => EMPTY)` — fail SAFE (stay logged in), track silently for
   diagnosis, no background error dialog.
3. **Context revalidation before wiping** (a verification started for org A
   must not wipe org B): active user id, selected clientUserId, active client
   id, and auth state all unchanged since suspicion; response user id matches.
4. **Self-heal (same release, separate commit)**: when auth is Authorized,
   active `clientUserId` absent, `/user/info` belongs to the authenticated
   user, and EXACTLY ONE ACTIVE membership exists → re-derive
   `activeClientUserId`. Never overwrite a truthy selection; never auto-select
   among multiple memberships. Repairs already-wedged persisted states
   (`{clientUserId: undefined}` in localStorage).

## Required tests

Pure spec `client-user-removal.spec.ts` (table-driven, no mocks):
- RED-1: `confirmsRemoval(userInfoWith1ActiveRow, ctx) === false` — fixtures
  from the live capture (the bug).
- RED-2: placeholder-row suspicion fixture — LABELED HYPOTHETICAL until the
  live wipe-moment dump proves the hybrid scalar-vs-nested mismatch (a
  self-consistent v3 row would NOT fire `suspectsRemoval`; Codex caught this).
- GREEN-3: genuine removal (no row for that client) still confirms.
- GREEN-4: multi-org removal keeps today's behavior (confirms → login).

Effect spec for `ClientUserRemovalEffect` (small dep surface; local jest.mock
header per repo ESM pattern, cf. `zcp-pool-claim-base.effect.spec.ts`):
- active membership returned → NO destructive actions;
- confirmed removal → EXACTLY ONE wipe sequence;
- repeated suspicions while HTTP pending → one request (exhaustMap);
- timeout/error → no wipe, effect stays alive;
- user/org context change before response → no wipe;
- corrected entity emission does not start an infinite verification loop.

## Codex approval conditions (all bound into the implementation)

1. Direct `UserBaseApi.load$()` under `exhaustMap` (no action round-trip).
2. Recheck user / selected row / client / auth context before wiping.
3. Client-user-removal flow has sole wipe ownership.
4. Effect-level tests; pure predicate tests alone insufficient.
5. Strict, fail-safe confirmation.
6. Entity merge/version problem tracked as parallel high-priority
   investigation (NOT this fix): the zef entity merger
   (`libs/zef/src/entities/entity-manager.reducer.ts:24,:76`) recursively
   merges with no version ordering and no replace-vs-patch distinction;
   `ClientUser` normalizes only `client`, nested `user` stays embedded
   (`zef-lib.module.ts:83`) — so `userId` scalar and `user.id` nested can come
   from different responses, and late lower-version responses can overwrite
   newer data. Affects entities beyond this bug.
7. Self-heal ships in the same release (need not block the causal fix).

Note on `/user/info` authority: empirically confirmed in the capture — at the
wipe moment it returned the correct 1-ACTIVE-membership payload (that is how
the sibling wiper was ruled out). If a backend contract ever says otherwise,
re-open.

## Open evidence item (does not block the fix)

The wipe-moment dump (activeClientUser vs entity row at the instant of firing)
is queued on the repro driver — blocked on the tatami registration outage. It
informs the cache-layer investigation and converts RED-2 from hypothetical to
captured; the fix's correctness does not depend on it (the guard verifies
against the authoritative source regardless of which projection field lied).

---

# Implementation + review outcome (2026-08-03)

Implemented by the Sonnet fix-implementer on FE branch
`kh-client-user-removal-verify` (from `kh-agent-first-onboarding`): `0ccffa29d`
causal fix (pure module + ClientUserRemovalEffect + wipe branch deleted from
app.effect.ts + specs), `fd5760026` self-heal, `d3d726204` captured-incident
fixture. Gate independently re-run by the orchestrator: 241/241, tsc app+spec
clean, lint clean.

Design-author review: **APPROVE-WITH-NITS** —
- MUST-FIX (applied in follow-up commit): self-heal lacked the payload-owner
  check (`data.id === activeUserId`) — a stale late-resolving `/user/info`
  after a fast logout→login could cross-write the previous user's identity.
- SHOULD-FIX (applied): `throttleTime` on the suspicion stream — `exhaustMap`
  bounds concurrency, not repetition; a persisting suspicion previously meant
  one serialized `/user/info` per entities-slice change. Plus an honest rename
  of the loop-safety test.
- Cosmetic: tombstone comment deleted from app.effect.ts (history lives in
  git); RED-2 spec title shortened; leaf-path `UserBaseApi` import annotated.

Structural findings worth keeping:
- Sibling interaction is safe STRUCTURALLY, not by convention: verification
  calls `UserBaseApi.load$()` directly and never emits `loadUserSuccess`, so
  neither `onLoadUserSuccessHandleClientUsers$` nor the self-heal ever sees
  verification traffic — the payoff of Codex condition #1.
- `suspectsRemoval` uses `cu.user?.id`, closing the latent unguarded-field
  TypeError that could kill the effect stream (findings-fe.md, other
  anomalies).
- Pre-existing gap (NOT introduced, logged for later): if a removal DELETES
  the cache row instead of rewriting it, `activeClientUser$` denormalizes to
  undefined, suspicion never fires, and the stale selection dangles — the
  self-heal doesn't cover it either (it requires clientUserId to be ABSENT).
- Benign flap-heal: after a confirmed wipe the token survives; if a later boot
  finds one ACTIVE membership again, the self-heal re-selects it — correct
  behavior when the backend flapped.

---

# Final outcome (2026-08-03, end of day)

Writer CONFIRMED = `onClientUsersChanges$` (`app.effect.ts:537`) — see
`findings-repro.md` Addendum 4 (same-channel parse instrumentation, 11/11
correlation). The branch targets the confirmed emitter; the intervening
hold/retarget cycle is documented in the addenda and was resolved by
retracting a cross-transport timestamp artifact. The design's principle held
through every reversal: a destructive, session-irreversible action must never
fire on cached projection state alone. RED-2's fixture is upgraded from
hypothesis to captured-by-elimination (buffer-lag mechanism open in the
entity-cache ticket). Verification bar for the live fix: ~10 fresh
registrations with zero wipes (~70% unfixed base rate) on a deployment
carrying the branch.
