# Phase 1 tracker — Dead-atom sweep

Started: 2026-04-26
Closed: 2026-04-26

> Phase contract per `plans/atom-corpus-hygiene-2026-04-26.md` §7
> Phase 1 + §15.1 schema. Phase 1's headline goal — delete atoms
> with empty fire-set — became a no-op after the F0-DEAD-1 sidecar
> fix (commit 984a657d) resolved the only candidate as a content
> bug. Phase 1 still records single/few-fixture atoms in
> `merge-candidates.md` for Phase 7 review.

## Work units (§7 Phase 1 WORK-SCOPE)

| # | atom | fire-set count | final state | commit | codex round | notes |
|---|---|---|---|---|---|---|
| 1 | bootstrap-recipe-close | 10 (post-fix) | KEPT | 984a657d (sidecar) | APPROVE (via Phase 0 POST-WORK 7d2cca23) | was DEAD-LOOKING due to placeholder bug; sidecar fix restored full fire-set |
| 2 | export | 1 | MARKED-MERGE-CANDIDATE | — | N-A | only fires on `export-active/container`; correct narrow targeting per axis frontmatter; recorded in merge-candidates.md |
| 3 | bootstrap-classic-plan-dynamic | 2 | MARKED-MERGE-CANDIDATE | — | N-A | fires on bootstrap classic/discover/svc-dynamic × {container, local}; runtime-axis-correct; merge candidate only if Phase 7 finds an axis-overlap target |
| 4 | bootstrap-classic-plan-static | 2 | MARKED-MERGE-CANDIDATE | — | N-A | same shape as #3 but `runtimes:[static]` |
| 5 | bootstrap-resume | 2 | MARKED-MERGE-CANDIDATE | — | N-A | fires on idle/incomplete × env; correct narrow targeting (the resume atom IS specifically for incomplete-bootstrap recovery) |
| 6 | develop-closed-auto | 2 | MARKED-MERGE-CANDIDATE | — | N-A | fires on develop-closed-auto/{container, local}; phase-specific; correct narrow targeting |
| 7 | idle-adopt-entry | 2 | MARKED-MERGE-CANDIDATE | — | N-A | fires on idle/adopt × env; IdleScenario-specific |
| 8 | idle-bootstrap-entry | 2 | MARKED-MERGE-CANDIDATE | — | N-A | fires on idle/bootstrapped × env; IdleScenario-specific |
| 9 | idle-develop-entry | 2 | MARKED-MERGE-CANDIDATE | — | N-A | fires on idle/bootstrapped × env (different rendering vs idle-bootstrap-entry); IdleScenario-specific |
| 10 | idle-orphan-cleanup | 2 | MARKED-MERGE-CANDIDATE | — | N-A | fires on idle/orphan × env; IdleScenario-specific |
| 11 | strategy-push-git-intro | 2 | MARKED-MERGE-CANDIDATE | — | N-A | fires on strategy-setup/{env}/push-git/unset; trigger-axis-correct |
| 12 | strategy-push-git-trigger-actions | 2 | MARKED-MERGE-CANDIDATE | — | N-A | fires on strategy-setup/{env}/push-git/actions; trigger-axis-correct |
| 13 | strategy-push-git-trigger-webhook | 2 | MARKED-MERGE-CANDIDATE | — | N-A | fires on strategy-setup/{env}/push-git/webhook; trigger-axis-correct |
| 14 | (all other atoms) | 3+ envelopes | KEPT | — | N-A | not marginal per §7 step 3 |

## Phase 1 EXIT (§7)

- [x] All atoms with fire-set = ∅ deleted (ratchet via fire-set re-run): **0 atoms qualified post-sidecar**.
- [x] `plans/audit-composition/merge-candidates.md` committed listing fire-set = 1 atoms for Phase 7 review (12 entries; counts 1-2 captured per §7 step 3 wider reading).
- [x] Test suite green; pin-density allowlist unchanged (no deletions, no pin moves).

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Every row that took action cites a commit hash. (Only row 1 took action — sidecar 984a657d.)
- [x] Every row whose phase required a Codex round cites the round outcome. (Codex Phase 0 POST-WORK 7d2cca23 confirmed 0 DEAD; no Phase 1 PRE-WORK Codex round needed since N=0 dead candidates per §10.5 round-skipping rule #4 "has Codex already answered this question in a recent round?".)
- [x] `Closed:` 2026-04-26.

## Codex round disposition (§10.1 Phase 1 + §10.5)

Per §10.1 Phase 1 row 1 (PRE-WORK): "Of these N candidate dead
atoms, which truly are dead vs which look-dead because the
envelope generator missed a state?" — N=0 candidates after the
F0-DEAD-1 sidecar fix. Phase 0 POST-WORK round (7d2cca23)
already walked `ComputeEnvelope` for the only candidate
(bootstrap-recipe-close) and confirmed CONTENT-BUG-BLOCKED, NOT
axis-dead. Per §10.5 work-economics rule #4, re-asking the same
question in a Phase 1 round would be wasted compute. The Phase 0
POST-WORK round subsumes Phase 1 PRE-WORK for this iteration.

Phase 1 PER-EDIT round (§10.1 Phase 1 row 2) — "Optional, only
for atoms with non-trivial git-history archaeology" — also
skipped (no edits made in Phase 1).

## Phase 2 ENTRY check

Phase 2 ENTRY: "Phase 1 EXIT satisfied. Fire-set matrix reflects
post-Phase-1 state."

- [x] Phase 1 EXIT all bullets ✓.
- [x] Fire-set matrix reflects post-Phase-1 state (== post-sidecar
      state — Phase 1 made no atom changes).
- Phase 2 may enter.
