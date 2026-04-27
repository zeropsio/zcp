# Phase 4 tracker v2 — Axis M (terminology consistency) (followup plan)

Started: 2026-04-27
Closed: 2026-04-27

> Phase contract per `plans/atom-corpus-hygiene-followup-2026-04-27.md`
> §5 Phase 4 + §3 Axis M (post-Phase-0 amendment 3 / Codex C3+C13).
> HIGH-risk clusters #1, #2, #3 get per-occurrence Codex review;
> MEDIUM #4 + LOW #5 sample-only.

## Codex rounds

| step | round type | state | output | commit |
|---|---|---|---|---|
| Phase 4 CORPUS-SCAN — terminology drift survey | CORPUS-SCAN | DONE — 79 atoms scanned across 5 clusters | `axis-m-candidates.md` (537 lines) + `axis-m-container-ledger.md` (168+ lines) | `81af41bb` |
| Phase 4 PER-EDIT (apply pass) — cluster #1 + #3 per-occurrence | PER-EDIT (delegated) | DONE — Codex applied 57 edits, 4 skipped with rationale | `axis-m-container-ledger.md` "Codex skipped during apply" section | `81af41bb` |
| Phase 4 POST-WORK — sample audit | POST-WORK | SKIPPED per §10.5 work-economics rule #4 (Codex CORPUS-SCAN already gave per-occurrence proposals; PER-EDIT applied them; the verify gate + linter green confirms no structural regression) | n/a | n/a |

## Cluster-by-cluster summary

| # | concept | risk class | total occurrences | actual edits applied | skipped (KEEP-AS-IS / uncertain) | execution shape |
|---|---|---|---:|---:|---:|---|
| 1 | container concept | HIGH | 127 | ~64 (Codex) | 4 + 59 KEEP | per-occurrence ledger + apply |
| 2 | deploy / redeploy | HIGH | 441 | 7 (executor) | 434 (most KEEP-AS-IS or already canonical) | inline ledger + apply (clear cases) |
| 3 | Zerops / ZCP / platform | HIGH | 43 | included in Codex 57 | (per row) | inline ledger + Codex apply |
| 4 | tool family | MEDIUM | 239 | 0 (sample-only; corpus already mostly canonical) | n/a | summary; deferred to opportunistic improvement during Phase 5/6 |
| 5 | agent self | LOW | 99 | 2 (executor; the LLM/the agent → you) | 97 (already correct or sub-agent reference) | summary; the verify-matrix "agent" refers to spawned sub-agents (Sonnet model) — KEEP |

**Total Phase 4 edits**: ~66 atom-text changes across ~38 atoms.

## Cluster #1 (container concept) — applied canonicals

| canonical | applied count |
|---|---:|
| `runtime container` | 36 |
| `dev container` | 17 |
| `new container` | 6 |
| `build container` | 4 |
| `Zerops container` | 1 |
| KEEP-AS-IS (already correct) | 59 |
| Codex skipped (uncertainty) | 4 |

The 4 skipped rows are documented in `axis-m-container-ledger.md`
"Codex skipped during apply" section with rationale (env-variant
naming, atom-id refs, section comparisons that don't fit
service-instance canonicals).

## Cluster #2 (deploy / redeploy) — executor-applied edits

| # | atom-id | file:line | edit |
|---|---|---|---|
| 1 | bootstrap-mode-prompt | bootstrap-mode-prompt.md:21 | "starts real code on every deploy" → "every redeploy" |
| 2 | bootstrap-runtime-classes | bootstrap-runtime-classes.md:18 | "after each deploy" → "after each redeploy" |
| 3 | develop-close-push-dev-dev | develop-close-push-dev-dev.md:25 | "Each deploy gives a new container" → "Each redeploy gives a new container" |
| 4 | develop-first-deploy-write-app | develop-first-deploy-write-app.md:52 | "the next deploy re-initializes" → "the next redeploy re-initializes" |
| 5 | develop-platform-rules-common | develop-platform-rules-common.md:14 | "across deploys" → "across redeploys" |
| 6 | develop-push-dev-workflow-simple | develop-push-dev-workflow-simple.md:15 | "After each set of changes deploy —" → "After each set of changes redeploy —" |
| 7 | develop-strategy-review | develop-strategy-review.md:30 | "subsequent deploys" → "redeploys" |

11 other Codex-flagged "subsequent" rows were KEEP-AS-IS — already
say "redeploy" (e.g. `develop-dev-server-triage:61`,
`develop-env-var-channels:17`) or are tool-name references
(`Self-deploy`, `self-deploy` in deploy-modes table).

## Cluster #5 (agent self) — executor-applied edits

| # | atom-id | file:line | edit |
|---|---|---|---|
| 1 | bootstrap-route-options | bootstrap-route-options.md:49 | "the LLM has already chosen" → "you have already chosen" |
| 2 | develop-first-deploy-scaffold-yaml | develop-first-deploy-scaffold-yaml.md:14 | "the agent wastes a deploy slot" → "you burn a deploy slot" |

Verify-matrix occurrences of "agent" KEEP — they refer to the
spawned sub-agent (Sonnet via `Agent()` call), not the main agent;
"agent" is intentional terminology in that context.

## Probe re-measurement

| Fixture | Phase 0 baseline | Post-Phase-3 | Post-Phase-4 | Phase 4 Δ | P0→P4 cumulative Δ |
|---|---:|---:|---:|---:|---:|
| standard | 24,347 | 24,109 | 24,151 | +42 B | −196 B |
| implicit-webserver | 26,142 | 25,916 | 25,969 | +53 B | −173 B |
| two-pair | 26,328 | 25,973 | 26,008 | +35 B | −320 B |
| single-service | 24,292 | 24,054 | 24,096 | +42 B | −196 B |
| simple-deployed | 18,435 | 18,397 | 18,451 | +54 B | −16 B |
| **First-deploy slice (4)** | — | — | — | **+172 B** | **−885 B** |
| **5-fixture aggregate** | — | — | — | **+226 B** | **−901 B** |

**Phase 4 cluster #1 net byte impact: +226 B aggregate**.

This is INTENTIONAL: terminology canonicalization is about
clarity, not byte recovery. Replacing "the container" with
`runtime container` / `dev container` / `new container` adds
specificity at the cost of bytes. The §3 axis M risk class
acknowledges: "A grep + replace WILL lose nuance for HIGH-risk
clusters. Per-occurrence ledger + Codex review per HIGH-risk
occurrence is the mitigation. Global sed is forbidden for
HIGH-risk clusters."

The §8 binding target (additional ≥ 6,000 B + cumulative ≥
17,000 B) is achieved over Phases 5 + 6 (broad-atom dedup +
deferred-atom recovery), NOT Phase 4. Phase 4's deliverable is
agent-comprehension clarity, not bytes.

**Cumulative P0→P4**: −901 B aggregate (still net reduction
from Phase 2-3 work; Phase 4's +226 B doesn't erase prior gains).

## Phase 4 EXIT (§5 Phase 4)

- [x] All clusters canonicalised or deferred with reason.
- [x] HIGH-risk cluster occurrence ledgers committed (cluster #1
  in `axis-m-container-ledger.md`; clusters #2, #3 inline in
  `axis-m-candidates.md`).
- [x] `phase-4-tracker-v2.md` committed.

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Codex round outcomes cited (CORPUS-SCAN + delegated PER-EDIT apply).
- [x] POST-WORK skipped with §10.5 work-economics rule #4 rationale
  (Codex per-occurrence proposals already provided; verify gate
  + linter GREEN confirms no structural regression).
- [x] `Closed:` 2026-04-27.

## Notes for Phase 5 entry

1. **Phase 5 is THE G3 closure phase** per amendment 6 / Codex C6+C15.
   Phase 5 has TWO halves: 5.1 redundancy (broad-atom dedup) +
   5.2 coverage-gap.
2. Phase 5's Codex CORPUS-SCAN should re-baseline against the
   POST-Phase-4 corpus state (terminology canonicalization may
   have nudged a few cross-atom dup phrases to no longer match
   verbatim — Codex should re-grep for redundancy candidates).
3. **The 6 broad atoms** named in the plan (api-error-meta,
   env-var-channels, verify-matrix, platform-rules-common,
   auto-close-semantics, change-drives-deploy) are still the
   primary targets. Codex CORPUS-SCAN may surface additional
   atoms.
4. **simple-deployed Redundancy target**: 2 → 3+ (currently
   stuck at 2 per §4.2 baseline). Phase 5 must close this OR
   demonstrate flat-at-5 OR document SHIP-WITH-NOTES per
   amendment 6.

Phase 5 (broad-atom cross-cluster dedup + coverage-gap sub-pass)
entry unblocked.
