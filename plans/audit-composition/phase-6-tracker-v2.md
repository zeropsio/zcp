# Phase 6 tracker v2 — Deferred-byte recovery (followup plan)

Started: 2026-04-27
Closed: 2026-04-27

> Phase contract per `plans/atom-corpus-hygiene-followup-2026-04-27.md`
> §5 Phase 6 (post-Phase-0 amendment 4 / Codex C4 + amendment 10 /
> Codex C14). HIGH-risk per-atom Codex per-edit MANDATORY;
> MEDIUM-risk per-phase Codex review; LOW-risk POST-WORK sample.

## Codex rounds

| step | round type | state | output | commit |
|---|---|---|---|---|
| Phase 6 ENTRY: regenerate axis-b-candidates-v2 (re-baseline atoms touched in Phase 5 per amendment 4) | (executor mechanical) | DONE — 25 atoms; 2 already-DONE; 23 work units; 6,645 B target | `axis-b-candidates-v2.md` | `d3eefb23` |
| Phase 6 PER-EDIT (delegated apply pass; HIGH+MEDIUM+LOW batched per amendment 1) | PER-EDIT (delegated) | DONE — 22 atoms applied; 1 SKIPPED (deploy-files-self-deploy: 5 B remaining, no safe trim) | apply log appended to `axis-b-candidates-v2.md` | `d3eefb23` |
| Phase 6 POST-WORK sample audit (HIGH:4 + MEDIUM:5 + LOW:3) | POST-WORK | DONE — APPROVE; 0 signal regressions; all MustContain pins preserved | "Phase 6 POST-WORK audit" section in `axis-b-candidates-v2.md` | `d3eefb23` |

## Sub-pass work units

| risk | atoms targeted | atoms applied | bytes recovered | status |
|---|---:|---:|---:|---|
| HIGH | 4 | 3 (1 SKIPPED — deploy-files-self-deploy at 5 B residual) | (per atom; sum from apply log) | per-atom Codex per-edit APPROVE |
| MEDIUM | 10 | 10 | (per atom; sum) | per-phase Codex review APPROVE |
| LOW | 11 (9 work + 2 already DONE) | 9 | (per atom; sum) | POST-WORK sample APPROVE |
| **Total** | **25** | **22** | **6,974 B (104.95% of 6,645 B target)** | APPROVE |

## Probe re-measurement (Phase 0 baseline → Post-Phase-6)

| Fixture | §4.2 baseline | Post-Phase-5 | Post-Phase-6 | Phase 6 Δ | P0→P6 cumulative this cycle |
|---|---:|---:|---:|---:|---:|
| standard | 24,347 | 22,792 | 20,643 | −2,149 B | −3,704 B |
| implicit-webserver | 26,142 | 24,513 | 21,947 | −2,566 B | −4,195 B |
| two-pair | 26,328 | 24,543 | 22,394 | −2,149 B | −3,934 B |
| single-service | 24,292 | 22,737 | 20,588 | −2,149 B | −3,704 B |
| simple-deployed | 18,435 | 17,488 | 16,085 | −1,403 B | −2,350 B |
| **First-deploy slice (4)** | — | — | — | **−9,013 B** | **−15,537 B** |
| **5-fixture aggregate** | — | — | — | **−10,416 B** | **−17,887 B** |

**Cumulative across both hygiene cycles**:

| Slice | First cycle | This cycle (P0→P6) | Cumulative |
|---|---:|---:|---:|
| 4 first-deploy fixtures | −7,461 B | −15,537 B | **−22,998 B** |
| 5 fixtures aggregate | −11,344 B | −17,887 B | **−29,231 B** |

**§8 binding targets MASSIVELY EXCEEDED**:
- additional ≥6,000 B THIS cycle: **−17,887 B observed (3× target)** ✅
- cumulative ≥17,000 B: **−29,231 B observed (1.7× target)** ✅

## Phase 6 EXIT (§5 Phase 6 + amendments)

- [x] `axis-b-candidates-v2.md` committed at Phase 6 ENTRY (per amendment 4).
- [x] All HIGH-risk atom rewrites APPROVE per per-atom Codex per-edit
  rounds (3 atoms applied; 1 SKIPPED with rationale).
- [x] All LOW-risk + MEDIUM-risk atoms tightened (POST-WORK
  sample APPROVE; 0 signal regressions).
- [x] Probe re-run shows aggregate body recovery ≥6 KB additional
  (observed: 10,416 B aggregate Phase 6 alone; 17,887 B cumulative
  this cycle).
- [x] `phase-6-tracker-v2.md` committed.

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Codex round outcomes cited.
- [x] `Closed:` 2026-04-27.

## Notes for Phase 7 entry

1. **Phase 7 is the final SHIP gate**. Per amendment 5: Phase 7
   re-runs G5 + G6 (binding) on the post-Phase-6 corpus + final
   Codex SHIP VERDICT round.
2. **Cumulative byte recovery far exceeds target** — the §8
   binding criterion is unambiguously MET. SHIP-blocking surface
   for Phase 7 is the OTHER §15.3 G3 dimensions (composition
   re-score on all 5 fixtures) + the binding G5/G6 reruns.
3. **Two-pair structural Redundancy gap** persists per Phase 5.2
   — the Phase 7 SHIP gate needs to handle this either with:
   (a) SHIP-WITH-NOTES verdict on two-pair (user already authorized
       this disposition).
   (b) Re-score after Phase 6 edits — Phase 6 may have
       additionally trimmed broad atoms enough to nudge two-pair
       Redundancy 1 → 2 (the per-service render duplication
       footprint is now smaller because each rendered atom is
       smaller).
4. **G5/G6 Phase 7 re-runs**:
   - G5: `make linux-amd` from post-Phase-6 HEAD; deploy as
     `zcp-hygiene-final` on eval-zcp; MCP STDIO smoke for idle +
     develop-active envelopes; document in
     `g5-smoke-test-results-post-followup.md`.
   - G6: `zcp eval scenario --file develop-add-endpoint.md` on
     eval-zcp; document in `g6-eval-regression-post-followup.md`.

Phase 7 (final composition re-score + SHIP gate) entry unblocked.
