# Phase 4 tracker — F5 + Axis N corpus-wide

Started: 2026-04-27
Closed: 2026-04-27

Plan: `plans/atom-corpus-hygiene-followup-2-2026-04-27.md` §5 Phase 4.

## ENTRY check

- [x] Phase 3 EXIT met (commit `97267c1f`; tracker `phase-3-tracker-v3.md` closed; Codex round-2 APPROVE).

## Phase 4 work units

| # | work unit | initial state | final state | commit | codex round | notes |
|---|---|---|---|---|---|---|
| 1 | §11.6 Axis N spec addition | absent in `docs/spec-knowledge-distribution.md` | added (88 lines) after §11.5 cluster #1; includes definition + DO-NOT-UNIFY exception | c48568b3 | – | mirror format from §11.5 K/L/M; corpus-scan ledger reference at `plans/audit-composition-v3/axis-n-candidates.md` |
| 2 | F5 (a) `develop-static-workflow.md` L13 drop env-leak | "Edit files locally, or on the SSHFS mount in container mode." | "Edit files." | c48568b3 | – (LOW-risk narrow) | universal atom; per-env edit-location lives in platform-rules-{local,container} |
| 3 | F5 (b) `develop-static-workflow.md` L27-28 drop env qualifier | "`push-dev` for fast iteration on a dev container over SSH." | "`push-dev` for fast iteration." | c48568b3 | round-1 catch | Strategy-fit signal preserved; env qualifier removed |
| 4 | CORPUS-SCAN | not run | COMPLETE: 25 candidates classified (5 DROP-LEAK, 19 KEEP-LOAD-BEARING, 1 SPLIT-CANDIDATE, 0 UNIFICATION-CANDIDATE) | c48568b3 | CORPUS-SCAN APPROVE (`a2c7421f89a390a54`) | output: `plans/audit-composition-v3/axis-n-candidates.md` |
| 5 | DROP-LEAK Edit 1 (`develop-first-deploy-intro.md` L31, priority 1) | "Don't skip to edits before the first deploy lands — SSHFS mounts can be empty and HTTP probes return errors before any code is delivered." | "Don't skip to edits before the first deploy lands — HTTP probes return errors before any code is delivered." | c48568b3 | round 1 APPROVE | universal HTTP-probe rationale preserved; SSHFS leak dropped |
| 6 | DROP-LEAK Edit 2 (`develop-http-diagnostic.md` L6 + L25-27, priority 2-3) | references-atoms only mentions container; body L25 hard-codes container path | references-atoms lists both platform-rules atoms; body L25-29 uses project-relative path + cross-links to BOTH atoms | c48568b3 | round 1 NEEDS-REVISION → round 2 prompt-confusion (verified text vs disk); round-1 concerns addressed in proposed revision; final form verified by POST-WORK round | iteration: initial brace-expansion `{container,local}` parsed as placeholder by synthesizer; rewritten as literal atom names |
| 7 | DROP-LEAK Edit 3 (`develop-push-git-deploy.md` SPLIT-CANDIDATE) | universal atom with container-only body | **DEFERRED** (status quo preserved) | – | round 1 NEEDS-REVISION; deferred per round-2 APPROVE | tightening axis would break `corpus_coverage_test.go:624` MustContain pin (no atom would supply "GIT_TOKEN" on local-env push-git develop-active envelope after tighten); proper fix needs new local-env atom — out of cycle-3 scope. Tracked in `deferred-followups-v3.md` DF-1. |
| 8 | DROP-LEAK Edit 4 (`develop-implicit-webserver.md` L24, priority 4) | "1. Write or edit files at `/var/www/<hostname>/`." | "1. Write or edit application files." | c48568b3 | – (LOW-risk narrow); round-1 APPROVE preview | path detail lives in platform-rules-{local,container} |
| 9 | DROP-LEAK Edit 5 (`develop-strategy-awareness.md` L13, priority 5) | "`push-dev` (SSH self-deploy from the dev container), `push-git`..." | "`push-dev` (direct deploy from your workspace), `push-git`..." | c48568b3 | – (LOW-risk narrow); round-1 APPROVE preview | strategy taxonomy preserved; env-specific framing replaced with env-agnostic |
| 10 | PER-EDIT Codex round 1 (priority 1-3 + SPLIT-CANDIDATE) | not run | NEEDS-REVISION (edits 2 + 3) | – | NEEDS-REVISION (`aef2a81baf1a7eefa`) | Edit 2: add platform-rules-local cross-link; Edit 3: drop title `(container)` suffix + tightening breaks tests |
| 11 | Plan + ledger revisions per round 1 | original | revised: §3 already had DO-NOT-UNIFY (from round-1 of Phase 0); §5 Phase 4 step 4 deferral note for Edit 3; axis-n-candidates.md SPLIT-CANDIDATE status DEFERRED with full rationale; deferred-followups-v3.md DF-1 created | c48568b3 | – | strategic decision: defer SPLIT-CANDIDATE to follow-up cycle (out of cycle-3 scope to author new local-env atom) |
| 12 | PER-EDIT Codex round 2 | not run | APPROVE on Edit 3 deferral; round-2 prompt confusion on Edit 2 verification (Codex checked disk where edits hadn't landed yet — interpreted as "not applied" but proposed text was sound) | c48568b3 | round 2 APPROVE on deferral; substantive Edit 2 verification deferred to POST-WORK round | (`acfa4b3da903f8d93`) |
| 13 | Apply edits 1, 2 (revised), 4, 5 | not applied | APPLIED (Edit 3 NOT applied — deferred) | c48568b3 | – | initial Edit 2 with `{container,local}` brace expansion broke synthesize; rewritten as literal atom names |
| 14 | POST-WORK Codex round | not run | APPROVE | c48568b3 | APPROVE (`a9a90b863cedaa944`) | platform-rules cross-link co-fire verified (StateEnvelope.Environment is scalar; ComputeEnvelope always sets it); all 6 per-edit signal preservation PASS; Edit 2 placeholder parse correctness PASS; deferred SPLIT-CANDIDATE status quo preserved |

## Probe re-run (post-Phase-4 vs post-Phase-3)

| Fixture | post-Phase-3 | post-Phase-4 | Δ |
|---|---:|---:|---:|
| develop_first_deploy_standard_container | 20,260 | 20,314 | +54 |
| develop_first_deploy_implicit_webserver_standard | 21,568 | 21,608 | +40 |
| develop_first_deploy_two_runtime_pairs_standard | 22,011 | 22,065 | +54 |
| develop_first_deploy_standard_single_service | 20,198 | 20,259 | +61 |
| develop_simple_deployed_container | 16,092 | 16,166 | +74 |

**Per-fixture impact: +40 to +74 B**. Driver: Edit 2 (`develop-http-diagnostic.md`) cross-link expansion adds ~+84 B per render (universal atom; fires on every develop-active envelope). Other edits saved -22 to -30 B per render but cumulatively can't offset Edit 2's growth.

Plan §4.3 estimate "~200-1,000 B aggregate" recovery NOT met on the 5 measured fixtures. The Phase 4 value is **signal purity** (universal atoms no longer carry env-leaky detail; per-env routing via platform-rules cross-links), not byte recovery on these fixtures. F5 atom (`develop-static-workflow.md`) saved bytes too but doesn't fire on my fixtures (`runtimes: [static]` axis); savings would manifest on static-runtime envelopes (not in 5-fixture set).

## Verify gate

- [x] `make lint-local` 0 issues post-Phase-4.
- [x] `go test ./internal/content/... ./internal/workflow/... -short -count=1 -race` green post-Phase-4 (incl. AST integrity + corpus_coverage MustContain — `develop-http-diagnostic` body still contains `storage/logs/laravel.log`, `var/log/...` strings).
- [x] Codex POST-WORK round APPROVE (`codex-round-p4-postwork-v3.md`).

## Phase 4 EXIT readiness (per §5 Phase 4 EXIT)

- [x] §11.6 Axis N added to `docs/spec-knowledge-distribution.md`.
- [x] `axis-n-candidates.md` committed.
- [x] F5 atom edits + corpus-wide Axis N applies committed (5 atom edits applied; SPLIT-CANDIDATE deferred per DF-1).
- [x] Codex POST-WORK APPROVE.
- [x] Probe re-run; aggregate impact measured (signal-purity gain, slight byte regression on these 5 fixtures).
- [x] `phase-4-tracker-v3.md` committed.

## Deferred follow-ups created

- DF-1 (axis-n SPLIT-CANDIDATE for `develop-push-git-deploy.md` + new local-env atom authoring) → `plans/audit-composition-v3/deferred-followups-v3.md`.
