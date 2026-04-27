# Final Codex SHIP VERDICT — atom-corpus-hygiene-followup-2026-04-27

Round: FINAL-VERDICT per §10.3 + §15.3 G7
Date: 2026-04-27
Reviewer: Codex
Plan: plans/atom-corpus-hygiene-followup-2026-04-27.md

## §15.3 G1-G8 gate-by-gate analysis

### G1 — phase trackers
Status: FAIL
Evidence: `phase-0-tracker-v2.md` through `phase-6-tracker-v2.md` exist and each has `Closed: 2026-04-27` (`phase-0-tracker-v2.md:3-4`, `phase-1-tracker-v2.md:3-4`, `phase-2-tracker-v2.md:3-4`, `phase-3-tracker-v2.md:3-4`, `phase-4-tracker-v2.md:3-4`, `phase-5-tracker-v2.md:3-4`, `phase-6-tracker-v2.md:3-4`). `phase-7-tracker-v2.md` is missing, despite follow-up §5 Phase 7 EXIT requiring it (`plans/atom-corpus-hygiene-followup-2026-04-27.md:818-826`) and §8 requiring all 8 phases 0-7 closed (`plans/atom-corpus-hygiene-followup-2026-04-27.md:875-878`).

### G2 — TestCorpusCoverage_PinDensity (knownUnpinnedAtoms empty)
Status: PASS
Evidence: `knownUnpinnedAtoms` is an empty map in `internal/workflow/corpus_pin_density_test.go:27-38`; `TestCorpusCoverage_PinDensity` exists at `internal/workflow/corpus_pin_density_test.go:93-111`. Indirect test-pass evidence exists via the Phase 0 verify gate record (`phase-0-tracker-v2.md:39`, `phase-0-tracker-v2.md:57-58`). This verdict round did not run tests per action-safety instructions.

### G3 — composition rubric strict-improvement
Status: SHIP-WITH-NOTES
Evidence: Final Phase 7 re-score is in `post-followup-scores.md:175-263`. Baseline criteria come from inherited §15.3 (`plans/atom-corpus-hygiene-2026-04-26.md:1832-1840`) and follow-up §8 (`plans/atom-corpus-hygiene-followup-2026-04-27.md:877-887`).
Per-fixture verdict (from post-followup-scores.md):
- standard: PASS (`post-followup-scores.md:186`, `post-followup-scores.md:196`, `post-followup-scores.md:206`)
- implicit-webserver: PASS (`post-followup-scores.md:187`, `post-followup-scores.md:197`, `post-followup-scores.md:207`)
- two-pair: STRUCTURAL-FAIL with rationale: Redundancy remains 1 because per-service render duplication plus shared deploy/env/verify restatements keep it in the 7+ anchor (`post-followup-scores.md:188`, `post-followup-scores.md:198`, `post-followup-scores.md:208`, `post-followup-scores.md:228-233`)
- single-service: COMPARISON-LIMITATION. The earlier baseline fixture scores PASS (`post-followup-scores.md:13-17`, `post-followup-scores.md:23-27`, `post-followup-scores.md:31-37`), but the final Phase 7 table substitutes `hypothetical-single`, which has no §4.2 baseline and is explicitly not a strict-improvement proof (`post-followup-scores.md:190`, `post-followup-scores.md:200`, `post-followup-scores.md:210`, `post-followup-scores.md:263`).
- simple-deployed: PASS (`post-followup-scores.md:189`, `post-followup-scores.md:199`, `post-followup-scores.md:209`)
Aggregate: 4/5 PASS or pre-authorized comparison-limitation disposition, 1/5 STRUCTURAL-FAIL. Aggregate G3 disposition is SHIP-WITH-NOTES, matching `post-followup-scores.md:259-263`.

### G4 — verify gate
Status: FAIL
Evidence: The strict gate requires `go test ./... -count=1 -race` and `make lint-local` green per inherited §15.3 G4 (`plans/atom-corpus-hygiene-2026-04-26.md:1837`) and follow-up final ship gate (`plans/atom-corpus-hygiene-followup-2026-04-27.md:1064-1073`). The only full-suite + lint-local tracker record is Phase 0 (`phase-0-tracker-v2.md:39`, `phase-0-tracker-v2.md:57-58`), before Phases 2-6 content edits and Phase 7 evidence artifacts. Later tracker records show narrower package tests (`phase-3-tracker-v2.md:99-102`) and per-atom workflow/content tests (`axis-b-candidates-v2.md:160-190`), but no final full `go test ./... -count=1 -race` + `make lint-local` evidence after Phase 6/7. This verdict round was explicitly instructed not to run the commands.

### G5 — L5 live smoke
Status: PASS
Evidence: Phase 7 binding G5 artifact reports `Status: BINDING GREEN` (`g5-smoke-test-results-post-followup.md:1-12`), valid markdown structure (`g5-smoke-test-results-post-followup.md:77-81`), and final `G5 BINDING GATE: GREEN` (`g5-smoke-test-results-post-followup.md:122-132`). The probe/live mismatch is documented as a structural testing-infra note, not a corpus regression (`g5-smoke-test-results-post-followup.md:94-120`).

### G6 — eval-scenario regression
Status: PASS
Evidence: Phase 7 binding G6 artifact reports PASS in 4m58.57954095s (`g6-eval-regression-post-followup.md:1-10`, `g6-eval-regression-post-followup.md:22-28`). Regression comparison shows PASS vs PASS, 21% faster duration, 0 wasted calls, 0 iterate cycles, and final URL 200 (`g6-eval-regression-post-followup.md:44-54`). Agent assessment states no information gaps and no wasted tool calls (`g6-eval-regression-post-followup.md:63-126`; `g6-eval-2026-04-27-final/result.json:4-16`). Tool-call archive includes the required workflow/discover/deploy/verify path (`g6-eval-2026-04-27-final/tool-calls.json:8-30`, `g6-eval-2026-04-27-final/tool-calls.json:108-130`). Final `G6 BINDING GATE: GREEN` is at `g6-eval-regression-post-followup.md:147-160`.

### G7 — Codex SHIP VERDICT (this round)
Status: FAIL

### G8 — probe binary cleanup
Status: PENDING — executor commits cleanup at Phase 7 EXIT
Evidence: `cmd/atomsize_probe/main.go` and `cmd/atom_fire_audit/main.go` are still present at this verdict-round time. Inherited G8 requires probe binaries deleted (`plans/atom-corpus-hygiene-2026-04-26.md:1841`). This is a pre-authorized executor follow-up disposition.

## Plan-specific acceptance criteria (§8)

- [ ] All 8 phases (0-7) closed per §15.2 trackers
- [ ] §15.3 G1-G8 satisfied
- [ ] Codex SHIP VERDICT
- [x] Body recovery: additional ≥6,000 B + cumulative ≥17,000 B
- [ ] Axes K + L + M documented in atom-authoring contract

Body-recovery evidence: Phase 6 reports this-cycle aggregate −17,887 B and cumulative −29,231 B (`phase-6-tracker-v2.md:37-49`; `post-followup-scores.md:249-257`), exceeding follow-up §8 targets (`plans/atom-corpus-hygiene-followup-2026-04-27.md:887-893`). Axes K/L/M are defined and executed in the plan/artifacts (`plans/atom-corpus-hygiene-followup-2026-04-27.md:68-261`, `axis-k-candidates.md:9-22`, `axis-m-candidates.md:8-20`), but I found no durable update in `docs/spec-knowledge-distribution.md`; the active contract in `CLAUDE.md:181-189` does not include K/L/M.

## Verdict

VERDICT: NO-SHIP
