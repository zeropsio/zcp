# Synthesis — where the investigation stands (2026-08-03, after experiment #1)

> **UPDATE (same day, twice revised): §2's attribution went through a full
> cycle** — retracted (lockstep exoneration), then the fourth-writer hunt
> hard-eliminated `setUserId` and closed the writer enumeration
> (`{"userId":X}` only producible by `storeUserData(X, undefined)`), then the
> offline full-session re-scan refuted the empty-clientUserList candidate and
> bounded the wipe to (5085, 5339] — whose ONLY identity event is the t=5220
> CORRECTION frame. **Unified hypothesis (one stack trace from proof):
> `onClientUsersChanges$` fires at the CORRECTION, with `list$()` already
> corrected (_v4) while the withLatestFrom-buffered `activeClientUser$` lags on
> the regressed pool row (_v3) → :533 mismatch → wipe.** Full chain:
> `findings-repro.md` addendum + `findings-fe.md` addenda. The pinning hunt
> returned NEGATIVE (no modelled mechanism lets the filter pass — deterministic
> ordering argues the other way), yet the EMITTER is pinned by exhaustive
> elimination on the wire: only `app.effect.ts:537` remains
> (`user-base.effect.ts:61` refuted by the single length-1 `/user/info`;
> org-switcher excluded by the single main-document navigation). Mechanism
> stays open (unknown same-tick anomaly vs merge-defect field divergence);
> live `wipe-probe.mjs` stack decides (blocked on the registration outage).
> **The causal verify-before-wipe fix is UN-HELD** — it targets the pinned
> emitter and is robust to every candidate mechanism; both slices land on FE
> branch `kh-client-user-removal-verify` (fix-implementer). Glitch-free
> single-selector derivation scoped to ticket #8.
> **RESOLVED (findings-repro Addendum 4): writer CONFIRMED =
> `onClientUsersChanges$` / `app.effect.ts:537` — the original prediction.**
> The intervening "timing exclusion" was a cross-transport timestamp artifact
> (CDP vs page-binding — never comparable); a same-channel `JSON.parse` hook
> shows the stale pool row parsed 1–3 ms before the wipe, 3/3, correlation
> 11/11 (stale seed ⇒ wipe, ~70% base rate; fresh seed ⇒ healthy). Root
> cause: ES-lagged `/client-user/search` seed returns the pre-claim pool
> row; the wiper fires on it unverified. Branch
> `kh-client-user-removal-verify` targets the confirmed emitter and LANDS
> (verify-before-wipe + self-heal). Open residue → ticket #8: why the
> `withLatestFrom` buffer read the real user while `list$` delivered the
> pool row (static analysis predicts lockstep — `evidence/parse{1,2,3}/` is
> the input). Verification bar: ~10 fresh runs, zero wipes, on a deployment
> carrying the fix.**
> Two latent defects
> logged on the way: an inverted `distinctUntilChanged` comparator in
> `entity-manager-entity.service.ts:345` (id CHANGE suppresses emission —
> pins `activeClientUser$` to a stale row; feeds the entity-cache follow-up)
> and an unguarded `cu.user.id` in `app.effect.ts:533` (a WS `add` frame
> without `user` would TypeError inside the filter and kill the effect).

Inputs: `findings-fe.md` (static analysis, H1–H4), `findings-repro.md` (live
CDP capture, discriminator verdicts). Orchestrator's verdict over both.

## 1. The PRD's target symptom did NOT reproduce — and the hypothesis field is now narrow

One clean fresh-registration run + four control runs: **5/5 shell-WS handshakes
succeeded** (101 in 28–51 ms, frames flowing, auth button enabled). The
discriminators still did real work:

- **DEAD: H3 structural** (stale containerId latch) — fresh and control resolve
  byte-identical containerIds; both `manualOpen` call sites converge.
- **DEAD: H4** (token URL-encoding) — all observed tokens are 22-char base64url;
  no encodable chars exist.
- **DEAD: the owner's hypothesis read literally** ("identity not established ⇒
  wrong credentials on the wire") — the only credential is the localStorage
  bearer, identical across reload; clientId never reaches the token mint or the
  WS. AND the strongest counterexample is live: in our capture the identity WAS
  wiped (see §3) and the terminal still connected fine.
- **OPEN: H1** (FE aborts the handshake — the console string "closed before the
  connection is established" is a client-side abort, not a server rejection).
  Unresolvable without a failing capture; the driver now auto-records
  created→response→closed deltas + in-page close codes.
- **OPEN (narrowed): H2** (container not shell-ready, FE connects blind with no
  status gate). The claim provably does NOT touch the container row
  (`lastUpdate == created`, 23 h old), so H2 survives only as "the owner hit a
  freshly refreshed pool whose shell wasn't up yet" — testable from `created`
  in any failing capture; the driver auto-runs the 60s-retry-without-reload
  discriminator on the first failure.

Root cause of the owner's WS stall therefore remains UNPROVEN — the driver is
armed to settle H1-vs-H2 unattended on the first failing run. What it needs is
fresh runs, which are currently blocked (§4).

## 2. Decision: what we fix NOW is the `/login` identity wipe

The repro DID root-cause a real, 100%-reproducible fresh-registration bug, and
it is the owner's own §1 observation ("closing the dashboard shows no creating
identity until a full page reload"):

> The pre-claim `client-user/search` seed returns the pool's placeholder row;
> the claim rewrites that row in place and the corrected version arrives via WS
> ~400 ms later. In that window `onClientUsersChanges$`
> (`app.effect.ts:517-558`) sees no row matching `cu.user.id`, concludes the
> user was removed, fires `storeUserData(id)` + `NO_ACTIVE_ACCOUNTS` +
> `zefGo(LOGIN_ROUTE)`, and nothing ever re-derives `activeClientUserId`.
> Recovery = re-login + org chooser.

This is FE ordering (a transient-window wipe with an authoritative source
available), squarely ours per the PRD ("if it's FE ordering, fix the
gate/sequence with RED-first specs"). Fix track: design (fe-investigator) →
Codex second opinion → Sonnet implements RED-first on a `kh-` branch.

It is provably NOT the WS cause (wipe + working terminal coexisted in one
capture), so fixing it does not close the PRD's headline symptom — it closes
the identity half of the owner's report and removes a guaranteed-broken state
from every tatami fresh registration.

## 3. Environment findings that gate further testing

- **The pool is stale: v9.137.0 / bootstrap 0.1.25.** The relay fix
  (v9.137.1 / 0.1.27, zcp `45183766`) never reached the pool containers — any
  launch-step (post-OAuth) testing on tatami since 2026-08-02 is invalid until
  the owner refreshes the pool. (Pool refresh = owner-coordinated, per PRD §9.)
  Note the relay bug CANNOT be the auth-dialog stall (different step, after
  launch), so the PRD's target symptom is not explained away by the stale pool.
- **BLOCKER: tatami registration 500s since ~08:23 UTC** (8/8 failures, with
  and without `claimZcpPool`; one success at 08:19). Blocks all fresh-path
  runs, the H1/H2 discrimination, and the 5/5 adversarial loop. A background
  probe retries every 10 min (no pool claims) and flags recovery. Platform-side
  — owner/platform team attention wanted.

## 4. Problem #3 (MCP initialize in container) — resolved as follow-up, not a zcp defect

- `zcp discover` is not a CLI verb — it falls through to `runServe()`, which is
  why the owner's diagnostic ask backfired (his keystrokes were parsed as
  JSON-RPC). Use `zcp version` in future asks.
- In-container: `ZCP_API_KEY` present (66 chars), full env provisioned, and a
  real `initialize` handshake against `zcp serve` succeeds (v9.137.0).
- Codex's "MCP client failed to start" therefore needs codex's OWN stderr to
  diagnose; split as its own follow-up. PRD DoD #4: satisfied (split +
  container evidence captured in `findings-repro.md` + `evidence/mcp-probe3/`).

## 5. Standing next steps

1. (running) Registration-recovery probe → on recovery, `loop.sh` fresh runs
   until a failing capture settles H1 vs H2, then 5/5 green loop (PRD DoD #3).
2. (in flight) Identity-wipe fix: design → Codex review → Sonnet RED-first
   implementation on FE `kh-` branch.
3. (owner) Pool refresh to v9.137.1/0.1.27; heads-up on registration 500.
4. (later) Driver commit + README under `tools/tatami-onboard-driver/`
   (exists, uncommitted), spec/journal hygiene.
