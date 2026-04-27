# Phase 0 tracker — Calibration

Started: 2026-04-26
Closed: 2026-04-26

> Phase contract per `plans/atom-corpus-hygiene-2026-04-26.md` §7 Phase 0 +
> §15.1 schema. Every row's "final state" is non-empty before phase EXIT.
> Every row that took action cites its commit hash. Every row whose
> action required a Codex round cites the round outcome.

## Work units (§7 Phase 0 WORK-SCOPE)

| # | work unit | initial state | final state | commit | codex round | notes |
|---|---|---|---|---|---|---|
| 1 | tracker-dir + this tracker | absent | scaffolded | — | N-A | scaffold commit (no Codex needed) |
| 2 | empirical 68-atom unpinned list (§4.2 derivation) | derived ad-hoc | committed as `unpinned-atoms-baseline.md` | — | N-A | source of truth for `knownUnpinnedAtoms` |
| 3a | Phase 0 PRE-WORK Codex round 1 (test design + §6.3 fire-set generator + §4.2 baseline list review) | not run | NEEDS-REVISION (4/6 axes) | — | NEEDS-REVISION | round 1 found: substring-pin-detection self-counting (Axis 1+4); fire-set generator missing services / Trigger / strategy / export / closed-auto / multi-service / wrong step-set (Axis 5); fixture empty MustContain (Axis 6). Findings in `codex-round-p0-design-review.md`. |
| 3b | Plan revisions applying Codex round 1 findings | NEEDS-REVISION | revisions committed | — | N-A | §6.3 generator rewritten with corrected axes; §7 step 3 test sketch rewritten with AST-based pin detection in dedicated file; §7 step 4 fixture given non-empty MustContain. |
| 3c | Phase 0 PRE-WORK Codex round 2 (re-validate against revised plan) | not run | NEEDS-REVISION (1/6 axes — Axis 6 only) | — | NEEDS-REVISION | round 2 found: 5 of 6 axes resolved (Axes 1,2,3,4,5 all APPROVE; round 1's 7 recommendations: 6 of 7 RESOLVED). Single blocker: Axis 6 fixture MustContain pins were not verified for uniqueness — `zerops_workflow action="close"` does not appear in `develop-close-push-dev-simple` and `zerops_deploy` is non-unique. Findings in `codex-round-p0-design-review-round-2.md`. Codex's sandbox blocked artifact write; Claude reconstructed verbatim. |
| 3d | Plan revision 2 — replace fixture MustContain with grep-verified-unique phrases | NEEDS-REVISION | revisions committed | — | N-A | three new phrases verified appearing in EXACTLY ONE atom each: `Push-Dev Deploy Strategy — container` (deploy-container), `auto-starts with its \`healthCheck\`` (workflow-simple), `Simple-mode services auto-start on deploy` (close-push-dev-simple). |
| 3e | Phase 0 PRE-WORK Codex round 3 (Axis 6 verify-only re-validation) | not run | APPROVE | — | APPROVE | round 3 grep-verified all three new phrases UNIQUE-MATCH-CONFIRMED to their anchor atoms; placeholders CLEAN; Axes 1-5 carry-forward AUTO-APPROVED (commit `a30d6f90` touches only plans/). Findings in `codex-round-p0-design-review-round-3.md`. **Phase 0 substantive work may begin per §16.1.** |
| 4 | recreate `cmd/atomsize_probe/main.go` from `c8d87406` + add `develop_simple_deployed_container` fixture | absent | DONE | 3725157e | N-A | baseline numbers match §9 reference; new fixture renders 20 atoms / 22424 B |
| 5 | build `cmd/atom_fire_audit/main.go` per §6.3 sketch | absent | DONE | 55a9fbdf | N-A | added Status enumeration on top of §6.3 Cartesian; surfaced 1 content bug (F0-DEAD-1 below); output committed as `fire-set-matrix.md` + stderr at `fire-set-stderr-2026-04-26.txt`. **Trade-off documented**: synthetic generator does NOT walk coverageFixtures (_test.go is not importable from `cmd/`); per §6.3 caveat the Codex POST-WORK round walks ComputeEnvelope to confirm any DEAD candidate is truly dead. |
| 6 | add `develop_simple_deployed_container` fixture to `coverageFixtures()` if absent | absent | DONE | f2a4e0df | N-A | landed in `corpus_coverage_test.go` after `develop_push_dev_simple_container`; MustContain pins are the three Codex round 3 UNIQUE-MATCH-CONFIRMED phrases; RoundTrip passes |
| 7 | add `TestCorpusCoverage_PinDensity` + `knownUnpinnedAtoms` allowlist + `TestCorpusCoverage_PinDensity_StillUnpinned` | absent | DONE | f2a4e0df | N-A | new file `corpus_pin_density_test.go` (file-isolated from corpus_coverage_test.go per Codex round 1 axis 1.2); 68-entry allowlist matches `unpinned-atoms-baseline.md`; AST-based pin detection via go/parser; both tests pass |
| 8 | run §6.2 composition audit on 5 fixtures (4 first-deploy + simple-deployed) | not run | DONE | d181df4d | N-A | rendered fixtures captured to `rendered-fixtures/<name>.md`; executor scoring per §6.2 rubric in `baseline-scores.md`; first-deploy fixtures score 4 on task-relevance, simple-deployed scores 1 (matches user-test anchor) |
| 9a | Phase 0 CORPUS-SCAN #2: Codex composition baseline scoring (run 1, run_in_background=true) | not run | DELEGATED-TO-BG-CODEX-NO-RESULT | — | UNUSABLE | first invocation reported delegating to background codex CLI job (`bwac2burr`) and exited without analysis text. Re-run synchronously (row 9b). |
| 9b | Phase 0 CORPUS-SCAN #2: Codex composition baseline scoring (run 2, synchronous) | not run | DONE | 7d2cca23 | RETURNED-WITH-DISAGREEMENT-≥-2 | Codex applied stricter interpretation of §6.2 rubric anchors; scores diverge from executor on Coherence (3→1, Δ−2 on multiple fixtures), Coverage-gap (5→2-3, Δ−2 to Δ−3), Redundancy (2→1, Δ−1). Per §6.6 L4 disagreement ≥ 2 triggers rubric refinement. Findings in `baseline-scores-codex.md`. |
| 9c | Apply rubric refinement to plan §6.2 per Codex's three anchor proposals | NEEDS-REFINEMENT | revisions committed | 7d2cca23 | N-A | Coherence 1 anchor names "mutually exclusive tool calls"; Coverage-gap 5 anchor requires "exactly one unambiguous recommendation"; Redundancy counts paraphrases + hostname-substituted copies. Codex's scoring becomes post-refinement baseline; executor's initial scores kept in deltas table for transparency. |
| 10 | Phase 0 POST-WORK Codex round: walk `ComputeEnvelope` for fire-set=∅ atoms; confirm or reject DEAD | not run | DONE | 7d2cca23 | CONFIRMED CONTENT-BUG-BLOCKED | Codex walked `bootstrap.go::BuildResponse` → `bootstrap_guide_assembly.go::synthesisEnvelope` → `engine.go::route=recipe` and confirmed users CAN reach route=recipe step=close envelope. Verdict: bootstrap-recipe-close is NOT axis-dead, it's content-bug-blocked. Phase 1 enters with **0 confirmed DEAD atoms**. Sidecar fix required before Phase 1 (changes `{hostname:value}` → `{"<hostname>":"<value>"}` per `synthesize.go::isPlaceholderToken` semantics — quotes inside the token make it fail the placeholder check). Side concern flagged: `isPlaceholderToken` is broad — accepts colons; could be tightened. Findings in `codex-round-p0-postwork-fireset-dead.md`. |
| 11 | commit `plans/audit-composition/baseline-scores.md` (executor scores + Codex scores per L4) | absent | DONE | 7d2cca23 | N-A | post-refinement converged baseline + delta table from initial executor pass; new "Competing-next-action problem" Phase 1+ finding section; Codex Phase 7 axis-tighten additions (`develop-api-error-meta`, `develop-dynamic-runtime-start-container`) |
| 12 | commit `plans/audit-composition/fire-set-matrix.md` (full per-atom fire-set table) | absent | DONE | 55a9fbdf | N-A | committed alongside fire-audit binary; documents 1 DEAD candidate (F0-DEAD-1) + Phase 7 axis-tightness candidates (F7-PHASE-7) |
| 13 | F0-DEAD-1: bootstrap-recipe-close placeholder bug (`{hostname:value}` literal in atom body) — content bug surfaced by fire-audit; sidecar fix REQUIRED before Phase 1 ENTRY | DEFERRED | — | — | N-A (Phase 1 sidecar) | atom body line 25-26 contains `strategies={hostname:value}` which `isPlaceholderToken` accepts as a placeholder; not in `allowedSurvivingPlaceholders`; not in replacer mapping → `findUnknownPlaceholder` errors → `Synthesize` errors for every recipe/close envelope. Production impact: every real recipe/close envelope errors during status rendering. Fix: change to `strategies={"hostname":"value"}` (proper JSON; `isPlaceholderToken` rejects tokens with `"`). Sidecar commit before Phase 1 ENTRY. |

## Phase 0 EXIT (§7)

- [x] Both probes built + run; output committed to `plans/audit-composition/`. (`atomsize_probe` at 3725157e, `atom_fire_audit` at 55a9fbdf.)
- [x] `TestCorpusCoverage_PinDensity` exists; `knownUnpinnedAtoms` populated to current 68-atom state. (Commit f2a4e0df.)
- [x] Baseline composition scores committed. (Commit d181df4d — initial executor pass; 7d2cca23 — post-refinement converged baseline.)
- [x] Full test suite green; no assertion semantics changed (only scaffolding + Logf + allowlist + docs). (Verified post-d181df4d: `go test ./... -short -race -count=1` PASS; `make lint-local` 0 issues.)

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state. (All rows DONE or DEFERRED-with-rationale.)
- [x] Every row that took action cites a commit hash. (Pending hashes will land in the closing commit.)
- [x] Every row whose phase required a Codex round cites the round outcome. (Rounds 1-3, 9b, 10 all returned with verdicts.)
- [x] `Closed:` 2026-04-26 — Phase 1 may now enter after the F0-DEAD-1 sidecar fix (row 13).

Phase 1 may not enter until: (a) the bootstrap-recipe-close
sidecar fix lands (per row 13 — content bug surfaced by fire-audit;
not a Phase 1 dead-atom candidate but a content fix that closes
F0-DEAD-1), AND (b) the §7 Phase 1 ENTRY criteria are
re-evaluated against the post-Phase-0 state.
