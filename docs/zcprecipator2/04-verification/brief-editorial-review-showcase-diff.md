# brief-editorial-review-showcase-diff.md — diff vs v34 captured dispatch

**Purpose**: diff the new architecture's composed editorial-review brief (from [brief-editorial-review-showcase-composed.md](brief-editorial-review-showcase-composed.md)) against the v34 equivalent. Per P7 + step-4 verification protocol.

**Role**: editorial-review
**Tier**: showcase

---

## 1. v34 predecessor: NONE

**The editorial-review role is genuinely new**. Prior architecture (v34 and earlier) has no editorial-review sub-agent and no captured dispatch payload under `docs/zcprecipator2/01-flow/flow-showcase-v34-dispatches/`. The role was prescribed by [spec-content-surfaces.md line 317-319](../../spec-content-surfaces.md) but absorbed into the writer sub-agent's own `self-review-per-surface.md` atom — author and judge collapsed onto one agent.

This diff is therefore **all-new**. There is no v34 dispatch text to remove-and-account-for; there is only added content. The standard step-4 "disposition per removed line" table does not apply.

Instead this doc serves two functions:

1. **Document what content was ABSORBED into writer self-review in v34** and now graduates to a distinct editorial-review dispatch.
2. **Audit that every editorial-review atom's content traces to either the spec or a prior defect-class that v34 under-enforced.**

## 2. Content absorbed in v34 → now editorial-review role

v34 writer sub-agent's brief (`readme-with-fragments` for minimal / `content-authoring-brief` for showcase) includes a self-review section. Content absorbed there that now lives in editorial-review atoms:

| v34 absorbed content (writer self-review) | New editorial-review atom |
|---|---|
| "Before returning, check that each gotcha names a Zerops mechanism and a concrete failure" | `single-question-tests.md` (KB surprise-test, applied independently by reviewer) |
| "Check that your classification matches the taxonomy before publishing" | `classification-reclassify.md` (reviewer re-classifies independently) |
| "Ensure gotchas don't restate IG items" | `counter-example-reference.md` (pattern-match against v28 cross-surface-duplication and v20 ig-leaning) |
| "Include citations to `zerops_knowledge` guides when relevant" | `citation-audit.md` (reviewer audits citations independently) |
| (Implicit: writer self-assesses each surface) | `surface-walk-task.md` + per-surface single-question-tests |
| (Implicit: writer self-polices cross-surface consistency) | `cross-surface-ledger.md` (reviewer tracks independently) |

**Disposition**: these items are NOT removed from writer self-review (`briefs/writer/self-review-per-surface.md` retains them as writer's own pre-return check). They are DUPLICATED into editorial-review as the independent-reviewer cross-check. Duplication here is DELIBERATE — per [atomic-layout.md §2 + §3.3](../03-architecture/atomic-layout.md), author and judge are intentionally split; BOTH apply the tests; editorial catches what writer self-review missed because reviewer has no authorship investment.

## 3. New atoms with no v34 analogue

Three atoms are genuinely net-new with no writer-side equivalent:

### 3.1. `porter-premise.md`

No v34 predecessor. The v8.94 writer brief has a "fresh-context" premise (no session transcript, no debug narrative) — this addresses author-side pollution. Porter-premise is the equivalent for the judge-side: "you are the reader, not the author; not only do you have no session context, you have no recipe authorship context either."

**Defect classes closed** that v34 didn't address:
- v28 wrong-surface (writer self-review absorbed this check but on same mental-model context)
- v34 self-referential (`/api/status` vs `/api/health` class — writer couldn't see the self-reference because writer's framing matched the recipe's feature-coverage namespace; reviewer sees from outside)

### 3.2. `surface-walk-task.md`

No v34 predecessor as an ordered walk. v34 writer self-review is implicit — writer wrote the surfaces so writer reviews them in authorship order. Reviewer's surface-walk-task imposes an ordered outside-in walk (root → env → per-codebase) — how a porter would approach the deliverable, not how the writer produced it.

**Defect classes closed**:
- Cross-surface drift (v28 same-fact-on-multiple-surfaces class): reviewer walks surface-by-surface with cross-surface-ledger running; writer walks surface-by-surface but in authorship order (per-codebase usually).

### 3.3. `cross-surface-ledger.md`

No v34 predecessor as a structured running tally. v34 `cross_readme_gotcha_uniqueness` check (Jaccard 0.6 threshold) catches similarity between READMEs but not cross-surface duplication spanning README + env import.yaml comment + CLAUDE.md. The ledger tracks fact bodies across all 7 surfaces.

**Defect classes closed**:
- v28 cross-surface-fact-duplication (registry row 8.4) — 4 surfaces carrying the same fact body, one with factual error. Jaccard check catches README-to-README only; ledger catches all permutations.

## 4. Size comparison

| Content | v34 | New architecture |
|---|---|---|
| Editorial-review dispatch (role nonexistent in v34) | — | ~9-10 KB transmitted prompt |
| Writer self-review atom (in v34 content-authoring-brief) | ~2-3 KB embedded | ~2-3 KB unchanged (writer keeps self-review) |
| Combined editorial-quality enforcement at close | writer self-review only (~2-3 KB at writer dispatch) | writer self-review (~2-3 KB) + editorial-review dispatch (~9-10 KB) = ~11-13 KB total |

**Net increase**: ~9-10 KB added per showcase run. Trade: paying ~10 KB for an independent-reviewer dispatch that closes the classification-error-at-source class v34's 85%-gotcha-origin regression surfaced.

## 5. Silent-drops audit

Per step-4 protocol: confirm zero content silently dropped between v34 and new architecture. Since the role is all-new (§1), the relevant silent-drops check is: *does the new role preserve everything v34 writer self-review did, OR does it deliberately reduce scope?*

**Answer**: new editorial-review DUPLICATES the writer self-review content (§2) into the reviewer's independent pass. Writer self-review is retained. Nothing is dropped. The new role is ADDITIVE — it does not remove writer-side responsibility, it adds an independent check on top.

**Confirmed**: zero silent drops. The refinement is purely additive at the dispatch-role level.

## 6. Disposition summary

- **Removed from v34**: none (no v34 editorial-review to remove).
- **Added**: 10 atoms under `briefs/editorial-review/`; 7 editorial-review-originated dispatch-runnable checks; 5 calibration bars (ER-1..ER-5); 2 rollback triggers (T-11, T-12); 1 registry row (15.1 classification-error-at-source) + 4 extended existing rows.
- **Duplicated from writer self-review**: classification-check, surface-walk-test, citation-check, cross-surface-check — intentional duplication per P7 "author vs judge split."
- **Net content delta**: ~+10 KB per showcase run transmitted prompt + ~2-3 KB return payload + ~+800 lines of atom .md content.

## 7. Diff verdict

Editorial-review role is net-additive. No content silently dropped. Duplication of writer self-review into independent reviewer is deliberate per the spec-prescribed split. Cold-read (see [brief-editorial-review-showcase-simulation.md](brief-editorial-review-showcase-simulation.md)) passes conditional on 5 in-atom clarifications.
