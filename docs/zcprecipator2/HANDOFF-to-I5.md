# HANDOFF-to-I5.md — zcprecipator2 post-v35-commission resume

**For**: fresh Claude Code instance picking up zcprecipator2 after the v35 showcase run the user commissioned against `v8.105.0`.

**Branch**: `main`. Last tag: `v8.105.0`. Last commit: `3a38e1b chore: refresh platform version catalog snapshot`.

**Repo state**: clean. All 11 commits from the C-7e..C-14 rollout window landed green and shipped in `v8.105.0`.

---

## What shipped in v8.105.0 (C-7e..C-14 + 2 chore/docs)

| # | Hash | Commit |
|---|---|---|
| 1 | `df07788` | feat(zcprecipator2): **C-7e** — `cmd/zcp/check` CLI shim tree (16 subcommands) |
| 2 | `7ff9d57` | feat(zcprecipator2): **C-7.5** — editorial-review role (substep + 7 dispatch-runnable checks) |
| 3 | `897407c` | feat(zcprecipator2): **C-8** — `writer_manifest_honesty` → 6 routing dimensions |
| 4 | `aef63af` | feat(zcprecipator2): **C-9** — delete `knowledge_base_exceeds_predecessor` |
| 5 | `92e0f43` | feat(zcprecipator2): **C-10** — shrink `StepCheck` payload to `{name, detail, preAttestCmd, expectedExit}` (BREAKING) |
| 6 | `78b959e` | feat(zcprecipator2): **C-11** — empty `NextSteps[]` at close completion |
| 7 | `6338f7c` | docs(zcprecipator2): **C-12** — land `DISPATCH.md` |
| 8 | `c01bf11` | feat(zcprecipator2): **C-13** — build-time lints on the atomic content tree |
| 9 | `a3efe05` | feat(zcprecipator2): **C-14** — `zcp dry-run recipe` harness + calibration scaffolds |
| 10 | `3dc2cc5` | docs: record v34 session metrics in recipe-version-log + taxonomy |
| 11 | `3a38e1b` | chore: refresh platform version catalog snapshot |

Every commit passed `go test ./... -count=1` (22 packages) + `make lint-local` (0 issues + `recipe_atom_lint` 0 violations across 121 atom files).

---

## Where the rollout stands

**Everything through C-14 is shipped.** The remaining rollout items (per [`06-migration/rollout-sequence.md`](06-migration/rollout-sequence.md)) are all post-v35:

1. **T-1..T-12 post-v35 rollback-criteria trigger scripts** — each trigger wires a measurable bar to a specific rollback action (revert commit X, re-enable check Y). These need v35 session-log data to calibrate against.
2. **C-15 — delete `recipe.md` monolith + `recipe_topic_registry.go`**. Gated on v35 validating the C-5 cutover empirically (if v35 goes well, the legacy content path is confirmed dead; C-15 removes it).
3. **v35.5 minimal commission** — user operational step, post-C-15. Confirms minimal-tier Path B (main-inline writer) works against the landed code.

---

## Required reading (in order — ~20 min)

1. **[`docs/zcprecipator2/implementation-notes.md`](implementation-notes.md)** — per-commit running notes through C-14 at the bottom. Read top-to-bottom from C-0. Every commit's LoC delta / verification / known-deferred is captured. Post-v35 follow-ups referenced from C-14's "Known deferred" are the load-bearing section for I5.
2. **[`docs/zcprecipator2/06-migration/rollout-sequence.md`](06-migration/rollout-sequence.md)** — commit plan. Read §C-15 (deletion) + §T-1..T-12 (rollback triggers) + the tail §"Parallelization opportunities" / §"Breaks-alone matrix". Everything through C-14 is history; these are what remain.
3. **[`docs/zcprecipator2/05-regression/calibration-bars-v35.md`](05-regression/calibration-bars-v35.md)** — the 97 bars the v35 run is measured against. Post-v35, every bar must resolve to PASS/FAIL with evidence. C-14 shipped measurement scaffolds (`scripts/measure_calibration_bars.sh` + `scripts/extract_calibration_evidence.py`) with SKIP-only output pending real session-log data.
4. **[`docs/zcprecipator2/HANDOFF-to-I4.md`](HANDOFF-to-I4.md)** — the handoff you're superseding. Still the source of truth for the operating rules + Q1–Q6 resolutions. Skim — the "Current state" + "Next commit" sections are fully superseded.
5. **[`CLAUDE.md`](../../CLAUDE.md)** — project root. TDD discipline + conventions. Auto-loaded; re-read if you drift.

---

## The v35 artifacts to expect from the user

When the user hands you the v35 run, they'll likely provide:

- **A session log** — JSONL trace of the `zerops_workflow` call/response pairs + main-agent tool calls. Path TBD; the user may drop it in a scratch directory or attach it inline.
- **The shipped deliverable tree** — the recipe output directory (`apidev/`, `appdev/`, `workerdev/`, `environments/`, `ZCP_CONTENT_MANIFEST.json`, `TIMELINE.md`). Likely at `/var/www/zcprecipator/<slug>/` on the zerops container, or exported as a `.tar.gz`.
- **Their qualitative read** — what worked, what didn't, which bars they think fired, which user-visible behaviors were surprising.

**First I5 action**: ask the user for these three artifacts before touching any code. Calibration analysis is the whole point of I5; the artifacts are the inputs.

---

## Work fronts, ranked by probable order

### Front A — measurement evaluators (C-14 known-deferred)

Fill in the stubs in [`scripts/extract_calibration_evidence.py`](../../scripts/extract_calibration_evidence.py):

- `extract_deploy_rounds` — count `step=deploy substep=readmes action=complete Passed=false` events.
- `extract_finalize_rounds` — same pattern for finalize retry rounds.
- `extract_substep_order` — list any attestation whose substep name violates the canonical order (Fix C + Fix D guards should have rejected these in-process; this is a session-log cross-check).
- `extract_todowrite_rewrites` — count TodoWrite calls whose `todos` argument wholly replaces the prior list (target: 0 per session).
- `extract_editorial_payload` — find the last `close.editorial-review` completion's attestation, `json.loads` it, return the parsed `EditorialReviewReturn` shape.

Then wire each extractor into [`scripts/measure_calibration_bars.sh`](../../scripts/measure_calibration_bars.sh)'s `SKIP` sections. Target: every §1–§11 bar in `calibration-bars-v35.md` emits a concrete `PASS` / `FAIL` / `SKIP` with evidence.

**Calibration loop**: run against v34 session log first (known baseline), then against v35. Expected: v34 produces the known-regression pattern; v35 produces the target pattern. Divergences are the work.

### Front B — T-1..T-12 rollback-criteria trigger scripts

Per `rollout-sequence.md §T-1..T-12`, each trigger pairs:

- A measurable bar (e.g. "v35 deploy.readmes rounds > 3").
- A rollback action (e.g. "revert C-7.5, re-enable the pre-editorial-review close substep").

T-1..T-12 are small, bar-specific shell scripts. The measurement scripts from Front A produce the bar evidence; the trigger scripts consume it. Land T-1..T-12 once Front A is working so the trigger-firing path is end-to-end testable.

### Front C — C-15 (delete `recipe.md` monolith)

Per `rollout-sequence.md §C-15`, once v35 validates C-5 cutover empirically:

- Delete `internal/content/workflows/recipe.md` (~3,438 lines).
- Delete `internal/workflow/recipe_topic_registry.go` + its test file.
- Remove any remaining references in Go code (expected: zero after C-5 cutover; any found indicate incomplete cutover).
- Full regression pass — every test layer must still pass post-deletion.

**Only after**: `make lint-local` + `go test ./... -count=1` + `zcp dry-run recipe --tier=showcase --against=docs/zcprecipator2/04-verification` all pass with the legacy path removed.

### Front D — v35.5 minimal commission (operational, user)

Post-C-15. Confirms minimal-tier Path B (main-inline writer) works against the landed code. No code change on our side; this is a user-commissioning step.

---

## Operating rules (unchanged from I4 handoff)

1. **TDD non-negotiable** — every behavior change has a failing test first. For Front A the tests are Python unit tests against synthetic session-log fixtures; for Front B they're shell tests with fake evidence inputs; for Front C they're the existing Go test suite (removal must leave tests green).
2. **After each commit**: `go test ./... -count=1` + `make lint-local`. Both green before advancing. `recipe_atom_lint` is part of `lint-local` — any atom-tree edit must stay lint-clean.
3. **User-review gates** — none scheduled post-v35 except the post-C-15 pause before v35.5 commission (operational, not code).
4. **Each commit appends to `docs/zcprecipator2/implementation-notes.md`** — same pattern as C-7e..C-14: LoC delta, what landed, verification, breaks-alone consequence, ordering deps, known follow-ups.
5. **`pre-existing working-tree modifications are NOT your concern`** — this handoff ships with a clean tree; future instances may again see platform-catalog drift on `internal/knowledge/testdata/active_versions.json` (auto-regenerated by `bin/zcp catalog sync` during `make lint-local`). Handle as a separate `chore: refresh platform version catalog snapshot` commit.

---

## What's already in place (do not redo)

- **`zcp check <name>` shim tree** — 16 subcommands under `cmd/zcp/check/`. Author-runnable axis open.
- **Editorial-review role** — `close.editorial-review` substep + 10 brief atoms + phase atom + 7 dispatch-runnable checks + validator. Fires first in the showcase close sequence.
- **`writer_manifest_honesty` 6-dimension expansion** — declarative dimension table in `internal/ops/checks/manifest.go`. Adding a 7th dimension is a struct-literal append.
- **StepCheck post-C-10 shape** — `{Name, Status, Detail, PreAttestCmd, ExpectedExit}`. `StampCoupling` + `NextRoundPrediction` gone. Every post-C-10 check emits `PreAttestCmd` (`zcp check <name>` invocation for §16/§18; `EditorialReviewPreAttestNote` marker for §16a).
- **Empty close NextSteps** — `buildClosePostCompletion` returns `[]string{}` unconditionally.
- **DISPATCH.md** — dispatcher-facing composition guide at `docs/zcprecipator2/DISPATCH.md`. Never transmitted to sub-agents.
- **`recipe_atom_lint`** — 7 rules (B-1..B-5 + B-7 + H-4) mechanically enforcing P2 / P6 / P8 invariants across the 121-atom tree.
- **`zcp dry-run recipe`** — golden-diff harness for stitcher output. Exit 0 on showcase + minimal tiers + dual-runtime minimal. Pre-v35 ship gate.
- **Measurement scaffolds** — `scripts/measure_calibration_bars.sh` + `scripts/extract_calibration_evidence.py`. Scaffold shape only; Front A fills these in.

---

## Invariants a post-v35 cleanup MUST NOT break

1. **121-atom tree lint-clean**. Every atom under `internal/content/workflows/recipe/` passes `recipe_atom_lint`. Any atom edit during post-v35 iteration must keep this true.
2. **Gate↔shim one-implementation invariant**. Every `§16` / `§18` check has exactly one predicate in `internal/ops/checks/`; gate and shim call the same Go function.
3. **§16a dispatch-runnable contract**. Editorial-review's 7 checks have no shell-shim equivalent by design — the reviewer IS the runner. Any post-v35 refactor that tries to add a shell form for them is a spec violation.
4. **Fix D ordering**. `close.code-review` requires `close.editorial-review` complete first (engine_recipe.go Fix D). Any substep reorder must update the guard in lockstep.
5. **`Build*DispatchBrief` is pure composition**. Tier-gating lives at dispatch sites (`composeDispatchBriefForSubStep`), not in the stitchers. A post-v35 refactor that short-circuits a stitcher on tier would break `zcp dry-run recipe`'s composition coverage.
6. **`StepCheck` post-C-10 shape is locked**. `PreAttestCmd` + `ExpectedExit` are the runnable-form contract. Re-introducing verbose diagnostic fields (ReadSurface / HowToFix / CoupledWith / PerturbsChecks) is a spec violation.

---

## Expected pace (post-v35)

- **Front A (measurement evaluators)**: ~3-4 hours against real v35 session log. Each extractor is ~50 LoC; shell wiring + report generation + v34-baseline validation account for the rest.
- **Front B (T-1..T-12)**: ~2 hours combined. Each trigger is a ~30-line shell script consuming Front A's evidence output.
- **Front C (C-15 cleanup)**: ~30 min. Deletion + regression pass.
- **Front D (v35.5 commission)**: operational, user.

Total remaining: ~6-7 focused hours + 1 user-gated pause (post-C-15 before v35.5).

---

## If something is unclear, ask the user

The v35 artifacts (session log + deliverable tree + qualitative read) are the inputs. You cannot do Front A meaningfully without them. The user may have already done informal analysis — ask them what surprised them before diving into automated bar evaluation.

Everything else is captured in the implementation notes + rollout sequence.

---

## Starting action

1. Ask the user for the v35 artifacts (session log + deliverable tree path + their qualitative read of the run).
2. Verify baseline: `git log --oneline -3` shows `3a38e1b` on top; `git tag -l 'v*' --sort=-v:refname | head -1` shows `v8.105.0`.
3. `go test ./... -count=1 -short` + `make lint-local` — both green confirm the v8.105.0 ship baseline is intact locally.
4. Once you have the v35 artifacts, begin **Front A**: fill in `extract_calibration_evidence.py` stubs against the real session log. Commit each extractor independently (TDD against synthetic fixtures first, then real log).
5. Stop and report after Front A's first evaluator goes end-to-end (raw log → PASS/FAIL + evidence). That's the unblock signal for Front B.

Good luck.
