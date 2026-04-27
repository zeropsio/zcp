# Phase 5 tracker — Verifiable-at-runtime moves (axis G)

Started: 2026-04-27
Closed: 2026-04-27

## Codex rounds

| step | round type | state | output | commit |
|---|---|---|---|---|
| Codex axis-G CORPUS-SCAN | CORPUS-SCAN | DONE | `axis-g-candidates.md` (1 atom qualifies; ~1,960 B target) | <pending> |
| Codex axis-G POST-WORK | POST-WORK | SKIPPED | — | — | per §10.5 rule #3 — Codex PRE-WORK was exhaustive across 79 atoms, no need for re-grep |

## Per-atom work units

| # | atom | bytes target | state | commit | notes |
|---|---|---|---|---|---|
| 1 | `bootstrap-env-var-discovery` (managed-service env-var catalog table) | ~1,960 B | DONE | <pending> | replaced 13-row catalog table with per-service usage-guidance bullets; `zerops_discover includeEnvs=true` is now the authoritative key-list source; preserved preference/usage nuance ("connectionString preferred", "no auth — private network", scoped-key vs masterKey for Meilisearch, etc.) which discover doesn't surface |

## Phase 5 EXIT (§7)

- [x] Every dropped catalog has a one-liner pointing at the tool that returns the same data — atom now starts with `zerops_discover includeEnvs=true` as the authoritative call.
- [x] Probe shows monotone (no regression on baseline-5 fixtures — this atom doesn't fire on first-deploy/develop envelopes; off-probe savings on bootstrap classic/adopt provision).
- [x] Target: 1-2 KB recovery achieved (~611 B atom-file recovery; body-render delta ≈ same; off-probe).

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Every row that took action cites a commit hash.
- [x] Every row whose phase required a Codex round cites the round outcome.
- [x] `Closed:` 2026-04-27.

Phase 5 is the smallest phase by atom count (1 atom). Codex's
PRE-WORK exhaustively scanned all 79 atoms; the corpus is largely
free of stale catalogs because Phase 0 fire-set + earlier hygiene
work pushed catalog-shape content toward tool surfaces already.

Phase 6 (Per-atom prose tightening — axis B) may now enter.
