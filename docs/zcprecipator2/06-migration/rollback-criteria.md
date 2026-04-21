# rollback-criteria.md — v35 go/no-go thresholds

**Purpose**: explicit, numeric, time-boxed decision criteria for v35 (first run under zcprecipator2). Derived from the 97 bars in [`calibration-bars-v35.md`](../05-regression/calibration-bars-v35.md) and the 68 closed-defect rows in [`defect-class-registry.md`](../05-regression/defect-class-registry.md). The rollback procedure is a `git revert` of the rollout-sequence commit range from [`rollout-sequence.md`](rollout-sequence.md) combined with state cleanup.

Decision classes — every v35 measurement lands in exactly one:
- **ROLLBACK**: regression against a gate bar crosses the threshold → revert the rollout-sequence commits and hold on v36 until the regression class is root-caused.
- **PAUSE**: regression is bounded but unexpected → hold on v36 planning; do not roll back v35; schedule a focused post-mortem before further migration work.
- **ACCEPT-WITH-FOLLOW-UP**: bar passed but with context that warrants a follow-up patch → proceed to v36 planning; create a targeted patch.
- **PROCEED**: all gate bars clean → v36 lands as a second full-architecture run to confirm the thesis across two runs.

---

## 1. Measurement window

v35 is **one commissioned showcase run + one commissioned minimal run** against the new path, each measured at these time points:

| Window | Scope | Wall duration |
|---|---|---|
| W-1 pre-ship | `zcp dry-run recipe` + lint + test suite all pass against C-14 scripts | ≤ 30 min |
| W-2 during-run | session-log tailing + substrate-invariant tripwires (O-3 very-long, O-6 SUBAGENT_MISUSE, O-7 git-lock, O-9 zcp-side-exec, O-14 git-repo-missing) | live |
| W-3 post-close | run `scripts/measure_calibration_bars.sh` against session log + exported deliverable tree; produce `reports/v35-measurement-<timestamp>.md` | ≤ 30 min after close |
| W-4 decision | apply the decision matrix in §2; land verdict + rationale in a new `docs/zcprecipator2/07-v35-verdict/` directory; if ROLLBACK, execute §4 procedure within 2 hours | ≤ 4 hours after W-3 |

Total time budget from close to verdict: **≤ 4.5 hours**.

---

## 2. Decision matrix

Decision = strictest outcome across all gates. ROLLBACK > PAUSE > ACCEPT-WITH-FOLLOW-UP > PROCEED.

### 2.1. ROLLBACK triggers (any ONE of these → rollback)

Derived from [calibration-bars-v35.md §13 headline bars](../05-regression/calibration-bars-v35.md) + v34-empirically-refuted axes from [defect-class-registry.md rows 11.2, 11.3, 13.7, 14.1, 14.2, 14.3](../05-regression/defect-class-registry.md).

| Trigger | Measurement | Threshold for rollback | Why |
|---|---|---|---|
| **T-1** Deploy-complete content-check rounds (C-1) | count of `is_error:true` on `complete step=deploy` | **> 2** rounds | The v31→v34 empirically-refuted axis. This IS the thesis. If v35 rounds ≥ v34 (4) under the new architecture, P1 author-runnable pre-attest has empirically failed. |
| **T-2** Finalize-complete content-check rounds (C-2) | count of `is_error:true` on `complete step=finalize` | **> 1** round | Same class as T-1 on finalize phase. v34 was 3 rounds; v33 was 2. Target is 1. Regression at > 1 rounds = P1 doesn't collapse finalize convergence. |
| **T-3** Substrate invariant broken | any of O-3 / O-6 / O-7 / O-8 / O-9 / O-14 / O-15 / O-17 | `> 0` events | The rewrite was supposed to leave substrate untouched. A substrate invariant breaking means the rewrite unintentionally regressed something v34 had clean. |
| **T-4** Cross-scaffold env-var coordination regression (CS-1 + CS-2) | `zcp check symbol-contract-env-consistency` on deliverable + runtime crash logs | CS-1 non-zero OR CS-2 > 0 events | P3 `SymbolContract` failed its direct v34 closure (DB_PASS/DB_PASSWORD). If CS-1 fails on v35 under the new architecture, P3 doesn't work. |
| **T-5** Manifest-honesty v34 class regression (M-3b) | `(routed_to=claude_md, published_gotcha)` mismatches | `> 0` matches | P5 two-way graph's direct v34 closure. Regression means P5 doesn't close what v34 surfaced. |
| **T-6** Phantom output tree (O-10) | `find /var/www -maxdepth 2 -type d -name 'recipe-*'` | `> 0` directories | v33 class; closed by v8.103 + v8.104 Fix A in the old path. P8 positive allow-list was supposed to close it structurally in the new path. Regression = P8 failure at closing a class old-path already closed. |
| **T-7** Self-inflicted gotchas shipped (CQ-2) | writer-classification manifest cross-check | `> 0` facts | v28/v34 class. Core content-quality gate. |
| **T-8** Very-long main bash calls (O-3) | bash calls ≥ 120s | `> 0` | Substrate performance invariant. Signals spawn-shape or SSH-boundary regression. |
| **T-9** Wall clock blowout (O-1 / O-1M) | session timestamps | showcase `> 135 min` (1.5× v34 baseline) OR minimal `> 90 min` | Non-linear regression that masks a different problem. |
| **T-10** Content-tree lint regression | `make lint-local` | any lint failure on `internal/content/workflows/recipe/` tree post-run | Build-level invariant broken — authors can't ship future atom changes until resolved. |
| **T-11** Editorial-review wrong-surface CRIT shipped after inline-fix | `Sub[editorial-review].return.CRIT_count` post inline-fix | `> 0` CRITs remained in deliverable | Refinement 2026-04-20: editorial-review is supposed to catch wrong-surface items BEFORE close completes. If CRITs ship despite editorial review, either (a) reviewer didn't catch the class (reviewer-brief gap — revisit `counter-example-reference.md` atom), (b) main-agent didn't apply the inline-fix (workflow gap — revisit P4 enforcement), or (c) editorial-review didn't dispatch (C-7.5 regression). All three are rollback-class. |
| **T-12** Classification-error-at-source regression | `Sub[editorial-review].return.reclassification_delta` | `> 0` AND v35 verdict doc confirms writer's self-classification was wrong on at least one reclassified fact | Refinement 2026-04-20: if the editorial reviewer finds the writer classified a fact wrong (e.g., scaffold-decision routed to gotcha-surface), the error source is the writer + the classification-taxonomy atom. If reviewer catches and inline-fixes, it's registered as PAUSE (not rollback). If reviewer catches but the wrong classification survives to export (inline-fix not applied OR reviewer itself wrong-classified), that's T-12 rollback. |

All triggers are measured mechanically by `scripts/measure_calibration_bars.sh` + session-log parse + editorial-review return payload inspection. No judgment calls.

### 2.2. PAUSE triggers (hold on v36 but do not rollback)

Bars that fail without invalidating the architecture. These warrant investigation before v36 but don't require reverting v35.

| Trigger | Measurement | Threshold for pause |
|---|---|---|
| **P-1** C-3 first-deploy-pass rate (v35 is one run; the gate is "did first deploy pass?") | `complete step=deploy` first attempt | not passed (0/1) AND rounds ≤ 2 (T-1 not triggered) — acceptable if rounds converge in ≤ 2 |
| **P-2** C-5 post-writer in-main `Edit` count | Edits after writer returns | `> 3` per file |
| **P-3** C-9 `Scope="downstream"` adoption | fact-record emission | `< 2` facts |
| **P-4** C-11 TodoWrite full-rewrite at step-entry | TodoWrite content delta | `> 1` full-rewrite |
| **P-5** CQ-1 gotcha-origin ratio genuine | manual classification | `< 80%` AND `≥ 60%` (between 60 and 80 = pause; < 60 = rollback via T-7) |
| **P-6** M-3 any `(routed_to × surface)` mismatch OTHER than M-3b | manifest × README grep | `> 0` on any of M-3a/c/d/e/f |
| **P-7** CR-1 CRIT shipped after close-fix | code-review + post-close audit | `> 0` |
| **P-8** CR-3 WRONG count | code-review output | `> 3` (v34 baseline was 3; exceeding is pause-class) |
| **P-9** Dispatch-integrity build lints (B-1..B-7) on atoms post-v35-cleanup | build-time grep | any failure on an atom edited post-v35 |
| **P-10** Substep out-of-order (C-6) | workflow-action-complete validation | `> 0` errors (clean v34; regression but not rollback) |

### 2.3. ACCEPT-WITH-FOLLOW-UP triggers

Bars that pass but signal a patch is needed before v36 hardens the architecture.

| Trigger | Measurement | Threshold |
|---|---|---|
| **F-1** Wall clock at upper bound of gate | O-1 `[S]` between 90 and 135 min | within gate but warrants profiling |
| **F-2** Main bash total time ramping | O-2 between 10 and 15 min | within gate; profile for regression class |
| **F-3** CR-3 WRONG count at v34 baseline | CR-3 = 3 | baseline held but not improved — code-review atom may benefit from sharpening |
| **F-4** IG item standalone borderline | CQ-10 edges | scan for formatting drift |
| **F-5** Cross-README dedup resolved via multi-round iteration | CQ-11 passed but only after round 2 | P1 should have made it round-1; patch the writer brief cross-reference rule |
| **F-6** Any of the 6 deferred-deletion check candidates from [check-rewrite.md §15](../03-architecture/check-rewrite.md) fired zero times on v35 | check fire-count = 0 | candidate for deletion in a follow-up patch per RESUME decision #5 upgrade path |
| **F-7** Minimal-tier Path A writer (main-inline) passed but coverage doc A1 simulation caveat surfaced ([RESUME.md step-4 findings](../RESUME.md)) | v35.5 minimal measurement | evaluate Path A vs Path B in the patch queue |
| **F-8** [data-flow-minimal.md §11](../03-architecture/data-flow-minimal.md) escalation-trigger question surfaced live during v35.5 minimal run | any of the 4 escalation questions fired | commission a focused minimal-tier investigation |

### 2.4. PROCEED (all gates clean)

Every ROLLBACK and PAUSE and ACCEPT-WITH-FOLLOW-UP trigger is clean. v36 commissioned as the second-run confirmation; if v36 also clean, the architecture is validated and the 6 deferred-deletion check candidates can be demoted per their F-6 measurement.

---

## 3. Cross-category gate check

Beyond triggers, every `[gate]`-severity bar in [calibration-bars-v35.md §1–§10](../05-regression/calibration-bars-v35.md) is evaluated individually. Any `[gate]` failure that is NOT already covered by §2.1 or §2.2 above triggers a judgment call:

| Is the failing bar covered by a principle? | Does the failure reproduce a closed defect class? | Action |
|---|---|---|
| Yes | Yes (pre-v20 or v20–v34 row in registry) | ROLLBACK — principle-claimed coverage empirically failed |
| Yes | No | PAUSE — principle held in one dimension but something adjacent failed |
| No | Yes | PAUSE — defect class recurred despite no principle claiming it; need a new principle |
| No | No | ACCEPT-WITH-FOLLOW-UP — new defect class surfaced; add to registry + v36 calibration bar |

This safety-net rule covers bars not explicitly listed above (§1 substrate beyond the listed O-*, §3–§8 content-quality beyond the headlines, §9 dispatch-integrity on non-post-run edits).

---

## 4. Rollback procedure

If a ROLLBACK trigger fires (§2.1), execute the following steps. Target total wall time ≤ 2 hours.

### 4.1. Decision gate

1. Triple-check the trigger evidence from `reports/v35-measurement-<timestamp>.md`. Specifically: the session-log grep producing the count + the deliverable-tree grep producing the match. Record in `docs/zcprecipator2/07-v35-verdict/rollback-evidence.md`.
2. Confirm the triggered bar is not measurement-artifact (e.g. C-1 deploy rounds count must exclude the FIRST attempt if that first attempt didn't actually run the gate — session log shows whether `complete step=deploy` fired at all).
3. If evidence confirms, proceed.

### 4.2. Git operations

Execute on the main branch. These are `git revert` operations, not `git reset` — the rollout history stays in the log.

```
# Identify the rollout-sequence commit range
git log --oneline --grep="zcprecipator2" --all

# Assume SHA range is <C-0-sha>..<C-15-sha>. Revert in reverse order.
git revert --no-commit <C-15-sha>
git revert --no-commit <C-14-sha>
# ... (continue in reverse order through C-0)
git revert --no-commit <C-0-sha>

# Single revert commit
git commit -m "revert: roll back zcprecipator2 v35 rollout-sequence (C-0 through C-15)

v35 run at <date> triggered rollback criterion <T-N> — <summary of failed bar>.
Evidence: docs/zcprecipator2/07-v35-verdict/rollback-evidence.md
v34 substrate restored; rollout-sequence commits revertable for re-attempt after root cause."

# Confirm baseline tests still pass
go test ./... -count=1
make lint-local
```

Critical: use `git revert`, never `git reset --hard` — preserves rollout-sequence commits in the log for later re-attempt (§4.5).

### 4.3. State cleanup

After git revert:

```
# Confirm deleted-in-C-4 tree is gone
ls internal/content/workflows/recipe/ 2>/dev/null  # should not exist

# Confirm recipe.md is back
wc -l internal/content/workflows/recipe.md  # should be 3,438

# Confirm old check files are back
ls internal/tools/workflow_checks_*.go | wc -l  # should be 12 (pre-C-6/C-7)

# Delete any non-committed v35 measurement artifacts if they reference new-path paths
rm -rf reports/v35-measurement-<timestamp>-draft/

# If any in-progress dev work depends on the reverted commits, the owner re-bases their branch
git branch --contains <C-0-sha> | grep -v main
```

No DB state to clean; no external system state modified. Zerops project state is owned by the tests/e2e harness which is substrate and untouched.

### 4.4. Communicate

1. Tag the rollback in the repo: `git tag v35-rollback-<date> -m "v35 rollback — trigger T-N, bar <name>"`
2. Record verdict at `docs/zcprecipator2/07-v35-verdict/verdict-v35.md` with: trigger fired, bar measurement, principle implication, next-step plan (root-cause investigation scope).
3. Update [RESUME.md](../RESUME.md) with: v35 rollback complete, pause on v36, open investigation item.

### 4.5. Re-attempt path

Rollback is not abandonment. The rollout-sequence commits remain in git history; a future v36 can cherry-pick the commits that held cleanly + fix the commit that caused the regression + re-attempt. The principle implication (T-1 → P1 failed; T-4 → P3 failed; T-5 → P5 failed; T-6 → P8 failed) identifies which design decision to revisit:

| Trigger | Failed principle | Revisit scope |
|---|---|---|
| T-1 deploy rounds > 2 | P1 | Are shims author-runnable? Does the payload shape actually cue author-side execution? Or is the problem that author doesn't run shims because the payload still looks like something to "just fix"? |
| T-2 finalize rounds > 1 | P1 (finalize phase) | Same investigation as T-1, finalize-scoped |
| T-3 substrate broken | substrate regression | Which commit regressed? (Expected: C-5 or C-10 — stitcher or payload shape.) The rollout-sequence's C-0 tests should have caught earlier. |
| T-4 cross-scaffold env-var | P3 | Did `SymbolContract` interpolation happen? Did scaffold briefs actually read it? Did the pre-attest shim run? |
| T-5 manifest-honesty (routed_to=claude_md, gotcha) | P5 | Is the expanded check emitting all 6 pair rows? Does the writer atom's routing-matrix clarity map to the check? Did the writer consume the atom? |
| T-6 phantom tree | P8 | Does canonical-output-paths atom positively declare? Is canonical-output-tree-only check firing at close-entry? |
| T-7 self-inflicted gotchas | classification taxonomy + P7 | Writer atom's classification taxonomy may need clarifying; review `briefs/writer/classification-taxonomy.md` + `routing-matrix.md`. **Post-refinement**: editorial-review's `classification-reclassify.md` should have caught this; if T-7 fires despite editorial dispatch, the editorial-review brief atoms need revisit — specifically `counter-example-reference.md` (pattern library gap) or `single-question-tests.md` (self-inflicted litmus test expressed weakly). |
| T-11 editorial-review wrong-surface CRIT shipped | P7 (cold-read + defect coverage) | Editorial reviewer was dispatched but didn't catch the class, OR caught and reported but main-agent didn't inline-fix, OR dispatch itself didn't fire. Walk the close.editorial-review session log: was Sub[editorial-review].return received? Were CRITs listed? Did main apply the inline-fix before close-complete? Atom gaps: re-examine `surface-walk-task.md` coverage, `single-question-tests.md` per-surface tests, `counter-example-reference.md` anti-pattern library. |
| T-12 classification-error-at-source | P5 + classification taxonomy | Editorial reviewer found writer-classification error BUT inline-fix didn't propagate to deliverable export. Either (a) reviewer's reclassification was itself wrong (reviewer's `classification-reclassify.md` atom semantics drift from spec), (b) main-agent didn't apply reclassification to ZCP_CONTENT_MANIFEST.json + didn't rewrite affected surfaces, (c) editorial-review dispatched AFTER content was already exported (workflow ordering bug — revisit C-7.5 substep positioning). |

---

## 5. Evidence collection schema

Every v35 verdict (ROLLBACK, PAUSE, ACCEPT-WITH-FOLLOW-UP, PROCEED) produces a document at `docs/zcprecipator2/07-v35-verdict/` with this structure:

```
docs/zcprecipator2/07-v35-verdict/
├── verdict-v35.md                        one-paragraph verdict + decision + rationale
├── bar-measurements-v35.md               97 bars × {PASS/FAIL + evidence link}
├── session-log-excerpts/
│   ├── c-1-deploy-rounds.md              evidence for C-1 bar
│   ├── c-2-finalize-rounds.md            evidence for C-2 bar
│   ├── c-9-downstream-facts.md           evidence for C-9 bar
│   ├── m-3b-claude-md-as-gotcha.md       evidence for M-3b bar
│   ├── cs-1-symbol-contract-env.md       evidence for CS-1 bar
│   └── ...                                one file per headline bar + per failing bar
├── deliverable-snapshots/
│   ├── showcase/                         export tree for the showcase run
│   └── minimal/                          export tree for the minimal run
└── rollback-evidence.md                  only if ROLLBACK — detailed trigger evidence
```

This structure is the permanent record. Future migrations (v36+) reference prior verdicts for pattern-matching against new defect classes.

---

## 6. What is NOT a rollback trigger

Per [calibration-bars-v35.md §14](../05-regression/calibration-bars-v35.md):

- Cross-codebase architecture narrative in root README (advisory only, rolled back per v24)
- Self-referential gotcha count (signal only; editorial)
- 3-split code-review pattern (signal; target but not gate)
- Environment README tier-transition token presence (signal)
- CLAUDE.md byte-size upper bound (no bar)
- Sub-agent dispatch prompt size upper bound (signal at 20 KB)

These bars' regressions land in the ACCEPT-WITH-FOLLOW-UP bucket at most — never ROLLBACK.

Also explicitly not rollback triggers:
- **Unexpected improvement** (e.g. wall clock dropped to 45 min) — investigated as ACCEPT-WITH-FOLLOW-UP for confirmation, never rollback
- **Bar passed on round N > 1** (e.g. deploy passed on round 2) — covered by rounds threshold, not pass/fail alone
- **Novel defect class surfaced that isn't in the registry** — ACCEPT-WITH-FOLLOW-UP + new registry row + v36 bar; not rollback (rewrite isn't expected to prevent classes unknown at design time)
- **A `[signal]`-severity bar regression** — PAUSE at most

---

## 7. v36 gate (post-PROCEED)

If v35 decisions as PROCEED (no rollback, no pause, no accept-with-follow-up — pristine first run):

- v36 is commissioned as a second-run confirmation under the same architecture. Same decision matrix applies.
- If v36 PROCEEDS cleanly, the 6 deferred-deletion check candidates from [check-rewrite.md §15](../03-architecture/check-rewrite.md) are evaluated per their F-6 fire-count evidence. Demote the ones that fired zero across v35 + v36.
- If v36 regresses against a gate that v35 held, that's a signal the first-pass was lucky. Downgrade architectural confidence; PAUSE on v37 + investigate the class that v35 didn't surface.

Post-v36 PROCEED: the architecture is validated across two runs. Post-v35 (this file) process ends; the `calibration-bars-v35.md` bars graduate to `calibration-bars.md` (versionless) for ongoing measurement.

---

## 8. Open question (documented, not gated)

Per [migration-proposal.md §6.3](migration-proposal.md): the v35 gate regimen assumes a **fresh Opus 4.7 (1M context)** instance executing the recipe against the new architecture. If the v35 run is instead executed by a different model version (e.g. Opus 4.8 or later), the calibration-bars baseline from v34 may have shifted for non-architectural reasons. Document the model used in `docs/zcprecipator2/07-v35-verdict/verdict-v35.md`. If model version changed, the rollback decision matrix is advisory — the thesis test is architecture × model, and regressions may have multiple root causes.

---

## 9. Summary — the 6 headline bars, one-line each

These are the single most-important v35 measurements per [calibration-bars-v35.md §13](../05-regression/calibration-bars-v35.md) (updated refinement 2026-04-20 — 6 headline bars instead of 5; adds ER-1). If forced to read one paragraph:

> **v35 passes if: (C-1) deploy rounds ≤ 2; (C-2) finalize rounds ≤ 1; (C-9) ≥ 2 `Scope="downstream"` facts recorded; (M-3b) zero `(routed_to=claude_md, published_gotcha)` mismatches; (CS-1) `zcp check symbol-contract-env-consistency` exits zero; (ER-1) editorial-review CRIT count shipped after inline-fix = 0. If any one regresses against its threshold, the rewrite's core thesis needs revisiting — execute the rollback procedure in §4.**
