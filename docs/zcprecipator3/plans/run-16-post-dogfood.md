# Run-16 — what to do after the dogfood

Tranches 0-5 of [run-16-readiness.md](run-16-readiness.md) shipped on
this branch. Tranches 6 + 7 are dogfood-gated. This doc is the compact
checklist for the post-dogfood return.

## Step 1 — run the dogfood

After tranches 0-5 are merged + the binary is released, run a recipe-
authoring dogfood through the new pipeline. Use a small-shape recipe
first (single-codebase, no parent) to smoke-test, then a showcase-shape
(api + frontend + worker + 5 managed services) for full coverage.

## Step 2 — run the operational gates

```
make verify-dogfood-no-manual-subdomain-enable
make verify-claudemd-zerops-free
```

Both gates must PASS:

- **`verify-dogfood-no-manual-subdomain-enable`** (R-15-1 closure) —
  zero manual `zerops_subdomain action=enable` invocations in the
  dogfood session jsonl. Recipe-authoring auto-enable now works via
  the dual-signal predicate (`detail.SubdomainAccess` OR
  `Ports[].HTTPSupport`); a manual enable means a regression.

- **`verify-claudemd-zerops-free`** (§0.6 mechanism gate) — every
  `codebase/<h>/claude-md` fragment authored by the claudemd-author
  sub-agent contains zero Zerops content. Static prohibited tokens
  (`## Zerops`, `zsc`, `zerops_*`, `zcp`, `zcli`) plus every hostname
  in `plan.Services` (read from `<run>/plan.json` when present).

If either gate FAILs, fix the underlying defect before proceeding.

## Step 3 — analyze the run

This is the **first run on the run-16 architecture**. Treat it like
run-15's forensic analysis was treated: deep, structured, with a clear
defect-numbering convention so future runs can refer back. The
analysis prompt for a fresh instance is in §A below — paste it into a
new chat alongside the run dir to drive the audit.

**Output**: `docs/zcprecipator3/runs/<run-N>/ANALYSIS.md` plus the
companion docs (TIMELINE.md, CONTENT_COMPARISON.md, PROMPT_ANALYSIS.md
where relevant). Match the run-15 shape — TL;DR up top, per-cluster
readiness disposition, R-16-N defect numbering with severity
(HIGH/MEDIUM/LOW), honest content grade vs reference recipes, run-17
targeting at the bottom.

## Step 4 — Tranche 6 (legacy atom deletion)

**Conditional**: ships only if the dogfood produced acceptable output
across all surfaces without regressions traced to missing atoms.

Per [run-16-readiness.md §11 Tranche 6](run-16-readiness.md):

```
git rm internal/recipe/content/briefs/scaffold/content_authoring.md   # -500 LoC
git rm internal/recipe/content/briefs/feature/content_extension.md    # -250 LoC
git rm internal/recipe/content/briefs/finalize/intro.md               # part of -160 LoC
git rm internal/recipe/content/briefs/finalize/validator_tripwires.md
git rm internal/recipe/content/briefs/finalize/anti_patterns.md
```

Verify nothing references the deleted atoms:

```
grep -rln "content_authoring\.md\|content_extension\.md" internal/
grep -rln "briefs/finalize/" internal/recipe/briefs.go
```

Run `go test ./... -short` + `make lint-local`. If the deletion breaks
anything, the legacy atom was load-bearing in a way the new atoms
don't replace — patch the new atom set instead and delay deletion
further.

**Specifically**: tranche 6 ships only after the **showcase-shape**
dogfood passes, not the small-shape. Showcase exercises 6 tier yamls ×
~8 service blocks of env-content authoring that the small shape never
touches.

## Step 5 — Tranche 7 (sign-off)

Per [run-16-readiness.md §11 Tranche 7](run-16-readiness.md):

1. **Spec rewrite** — `docs/spec-content-surfaces.md §Surface 6`
   replaced per [§15 of the readiness doc](run-16-readiness.md#15-spec-surface-6-rewrite--lands-alongside-engine-implementation-tranche-7-commit-1).
   Drop-in replacement provided in §15 verbatim.

2. **CHANGELOG entry** — name what the run-16 architecture pivot
   delivered. Verdict-table rows pre-specified in [§11 Tranche 7
   commit 2 of the readiness doc](run-16-readiness.md):

   | Artifact | TEACH/DISCOVER | Status |
   |---|---|---|
   | Sub-agent-authored CLAUDE.md via dedicated `claudemd-author` | DISCOVER | ✅ |
   | Slot-shape refusal at record-fragment time | TEACH | ✅ |
   | 7-phase pipeline (research → provision → scaffold → feature → codebase-content → env-content → finalize) | — | ✅ |
   | FactRecord polymorphic Kind discriminator | TEACH | ✅ |
   | Engine-emit Class B/C umbrella facts | TEACH | ✅ |
   | Per-managed-service fact shells (fill-fact-slot pattern) | Hybrid | ✅ |
   | Engine-emit tier_decision facts | TEACH | ✅ |
   | Subdomain dual-signal eligibility (R-15-1) | TEACH | ✅ |
   | validateCrossSurfaceDuplication / validateCrossRecipeDuplication | TEACH (defensible) | ⚠️ Notice |
   | validateCodebaseCLAUDE reshape | TEACH | ✅ |

3. **Archive the plan docs**:

   ```
   git mv docs/zcprecipator3/plans/run-16-prep.md docs/zcprecipator3/plans/archive/
   git mv docs/zcprecipator3/plans/run-16-readiness.md docs/zcprecipator3/plans/archive/
   git mv docs/zcprecipator3/plans/run-16-post-dogfood.md docs/zcprecipator3/plans/archive/
   ```

4. **Spec amendments** — anything that drifted during implementation
   that wasn't caught by §15 lands as a follow-up commit on
   `spec-content-surfaces.md` and/or `system.md`.

## Reviewer-fix recap (already landed pre-dogfood)

A separate code review surfaced 6 defects + 3 minors against the
tranche 0-5 implementation; all addressed before this commit. See the
final commit message for the full list. The dogfood runs against the
post-fix implementation, not the original landing.

## Reverting tranches 1-5 if dogfood reveals fundamental issue

If the dogfood reveals the architecture pivot itself (not a tranche-6
deletion concern) is broken in a way no atom patch can fix, the revert
path is `git revert <T1>..<T5>` on the tranche commits. The Tranche 0
work (R-15-1 + verify gates + scaffold subdomain teaching + run-15
fact annotation) is independent and stays.

---

## §A — Analysis prompt for the fresh instance

> Paste the block below into a fresh chat, alongside `RUN_DIR=
> docs/zcprecipator3/runs/<N>` for the run being analyzed. The fresh
> instance has no context from the run-16 implementation; the prompt
> primes it with what changed and what to look for.

```
You are auditing the FIRST recipe-authoring dogfood on the run-16
architecture (recipe pipeline pivot from "deploy phases author content"
to "deploy phases record facts; content phases synthesize surfaces").
Run dir: <RUN_DIR>.

Reading order before any analysis:

1. docs/zcprecipator3/plans/run-16-readiness.md §1 (the architecture
   pivot in three sentences) and §13 (R-15-N defect closure mapping).
   This names what run-16 was supposed to fix.
2. docs/zcprecipator3/plans/run-16-post-dogfood.md (this doc) §A so
   you know which questions to answer.
3. docs/zcprecipator3/runs/15/ANALYSIS.md — model your output on this
   shape (TL;DR, per-cluster disposition, R-N defect numbering, content
   grade, next-run targeting).
4. The run dir itself: SESSION_LOGS/, environments/facts.jsonl,
   per-codebase READMEs and CLAUDE.md, plan.json, environments/<tier>/
   import.yaml comments.

Per the §0 verification protocol of run-16-readiness.md: negative-
existence claims require unbounded reads. Do not claim "X is not in
file F" based on a head-truncated grep — read F in full or use grep -n
without piping to head.

## §1 — Architecture-pivot claim verification

For each claim below, walk the production artifacts and grade it
HIT (architecture worked as designed), MISS (architecture didn't
deliver the promised behaviour), or REGRESSION (worse than run-15).

(a) Deploy phases (scaffold + feature) record facts only — do NOT
author IG/KB/yaml-comment/CLAUDE.md fragments. Check session jsonls
for record-fragment calls during phase=scaffold or phase=feature
against `codebase/<h>/{intro,integration-guide*,knowledge-base,
zerops-yaml-comments/*,claude-md*}` ids. Any such call is a MISS —
the agent didn't internalize the new flow despite the
decision_recording.md atom + phase_entry/scaffold.md teaching.

(b) `porter_change` + `field_rationale` facts get recorded at densest
context. Count facts by Kind in environments/facts.jsonl. Compare
against the count of non-obvious decisions visible in source/yaml.
Empty fact stream means the agent skipped recording — same failure
shape as run-14's "no facts beyond browser-verification".

(c) Engine-emitted shells fill via fill-fact-slot. For every fact
where EngineEmitted was originally true (per readiness §7.1: per-
codebase Class B + own-key-aliases umbrella + per-managed-service
shells), check the post-fill record: EngineEmitted=false, Why non-
empty, CandidateHeading non-empty (except worker-no-http, where
heading is intentionally agent-filled at codebase-content time, not
fill-fact-slot time). Count unfilled shells; any > 0 is a MISS.

(d) Phase-5 codebase-content sub-agent dispatch — both
`codebase-content` and `claudemd-author` per codebase, in parallel
in a single message. Grep main session jsonl for the dispatch pattern;
look for two Agent tool calls in one assistant message (parallel),
once per codebase. Serial dispatch is a MISS (5-15 min lost; the
phase-entry atom mandates parallel).

(e) Phase-6 env-content sub-agent dispatch — single sub-agent for
all 6 tiers' env intros + import-comments. One Agent call.

(f) Slot-shape refusal at record-fragment time delivered same-context
recovery. Search session jsonls for `record-fragment` responses with
`error: record-fragment: ...` and check whether the agent re-authored
in the next turn. Recovery within 1-2 turns is HIT; recovery after
3+ turns or no recovery is a MISS.

(g) CLAUDE.md authored by claudemd-author is Zerops-free.
make verify-claudemd-zerops-free should PASS. If it fails, list every
prohibited token + file path; this is R-16-1 (HIGH).

(h) Recipe-authoring auto-enable — make
verify-dogfood-no-manual-subdomain-enable PASSes (R-15-1 fix held).
If it fails, R-15-1 regressed; this is R-16-2 (HIGH).

(i) Cross-surface duplication actually drops vs run-15. Walk each
codebase README's IG vs KB and count topic overlaps the
validateCrossSurfaceDuplication validator would flag. Run-15 baseline
was 2 dups (apidev X-Cache, appdev duplex:'half'). Target: 0 in run-
16 due to single-author phase 5. Any dup is R-16-3 (LOW; structural
fix didn't catch it, and the heuristic Notice validator should have).

(j) Cross-recipe duplication when parent != nil. Skip if this run
has no parent. When parent present, walk parent's IG topics vs
child's; any re-authored topic is R-16-4 (LOW).

(k) IG slotted form per codebase (1..5 items, IG #1 engine-stamped).
Count `### N.` items in each codebase README. Any over 5 is a
validator miss; any unnumbered `### ` heading inside the IG markers
(R-15-5 anti-pattern) is R-16-5 (MEDIUM).

(l) zerops.yaml comments injected line-anchor at stitch. Read each
codebase's published `zerops.yaml` (in stitched output, not the
source root). Comments above named blocks (run.envVariables,
run.initCommands, build, etc.) should match the recorded
field_rationale facts. Missing comments where field_rationale
recorded → R-16-6 (LOW; line-anchor regex didn't match).

(m) Tier_decision facts feed env import.yaml comments. Read tier 5's
import.yaml; managed-service blocks (db, cache, etc.) should carry
mode-flip explanations grounded in the engine-emitted tier_decision
TierContext + agent-extended TierContext where applicable. Generic
"mode: HA at tier 5" with no tier-context teaching is a MISS.

## §2 — Stealth-regression hunt (run-15 §6 shape)

Walk run-15's stealth-regressions and verify each was actually closed
in production, not just unit-tested:

- §A premise (subdomain): pre-fix tests passed but production failed.
  Check whether tranche-0 fix produces the expected behaviour in
  THIS dogfood.
- §B Resolver threading: check Recipe-knowledge slugs section in
  every dispatched scaffold prompt.

Then hunt for new stealth-regressions: places where the engine code
shipped + unit tests passed but production output diverges from spec.
Run-16 candidate stealth-regression sites:
- Did the brief composer's FactsLog threading actually deliver facts
  to the codebase-content brief? (D-4 was the stub-readFactsForBrief
  defect; verify the production fix held.)
- Did the slot-shape refusal at record-fragment time produce useful
  refusal messages, or did the agent get stuck?
- Did fill-fact-slot's ReplaceByTopic atomic rewrite hold under the
  full run's append + replace mix?

## §3 — R-16-N defect numbering

Match run-15's table shape:

| Defect | Severity | Cluster | Symptom | Mechanism | Closure target |
|---|---|---|---|---|---|

Severity tiers:
- HIGH = blocks acceptance, requires hotfix before next run
- MEDIUM = ships to deliverable, validator gap, fix in next run
- LOW = quality concern, slot for future readiness

## §4 — Honest content grade vs reference recipes

Per run-15 §11: grade each surface 0-10 against
laravel-jetstream-app/laravel-showcase-app references:

- §1 root README + §2 per-tier README intros — env-content output
- §3 import.yaml comments — env-content output
- §4 IG (per codebase) — codebase-content output
- §5 KB (per codebase) — codebase-content output
- §6 CLAUDE.md (per codebase) — claudemd-author output
- §7 zerops.yaml comments (per codebase) — codebase-content output

Honest grade — split self-grade vs honest if they diverge. Run-15
landed 7.5/10 honest. Target: 8.0+ in run-16 (architecture pivot
should produce richer per-codebase content because content sub-agents
have full context). Below 7.5 is a regression.

## §5 — Acceptance criteria disposition

run-16-readiness.md §11 + §17 list run-16's acceptance bar. Walk
each one and mark PASS / FAIL / N/A:

- All 7 phases reachable in adjacent-forward order
- Engine-emitted shells appear in fragment composition
- Slot-shape refusal blocks invalid fragments with recoverable error
- claudemd-author dispatched in parallel with codebase-content
- env-content sub-agent dispatched once after codebase-content
- Both verify-* gates PASS
- Zero R-15-N regressions
- Cross-surface dup count drops to 0 (vs run-15's 2)

## §6 — Run-17 targeting

Based on R-16-N defects + grade gaps, what should run-17 readiness
target?
- HIGH defects → hotfix tranches before run-17 dogfood
- MEDIUM defects → run-17 readiness candidates
- LOW defects → backlog
- Architecture refinements (e.g. fact-shell pattern extensions, new
  validator backstops, atom curation) → run-17 readiness §X

Note clearly which tranches are dogfood-conditional for run-17 vs
which can ship pre-dogfood. Run-15 → run-16 deferred T6 + T7 to
post-dogfood; run-17 may have its own deferral pattern.

## §7 — Companion documents (run-15 shape)

Produce alongside ANALYSIS.md when relevant:
- TIMELINE.md — minute-by-minute reconstruction from session jsonls.
  Useful when defects involve agent-recovery latency.
- CONTENT_COMPARISON.md — per-surface grade table with side-by-side
  reference recipe excerpts. Useful when grading honestly is contested.
- PROMPT_ANALYSIS.md — dispatch-prompt comparison if a brief shape
  defect surfaces.

## §8 — TL;DR shape

Match run-15's TL;DR opening line: `Run N closed in deliverable, did
[/did not] close in readiness goals.` Then 3-5 bullets naming the
HITs + MISSes + REGRESSIONs and the headline grade.
```

The above prompt is intentionally exhaustive — the first run on a new
architecture deserves more analysis surface than incremental runs. If
the dogfood produces clean output across all sections, the run-17
readiness can shrink the analysis scope; if defects surface, the
exhaustive prompt is the diagnostic tool.
