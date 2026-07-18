# Run-42 Forensics — undocumented anomalies across runs 40/41/42

Scope: session-log forensics for `nestjs-showcase` runs 40, 41, 42.
Method: Python JSONL crunching against `main-session.jsonl` + `subagents/*.jsonl`
(scripts at `/tmp/zcprec_*.py`, cleaned up). Cross-checked against
`plans/run-42-validation.md`.

## Counter table

| metric                          | run-40 | run-41 | run-42 |
|---------------------------------|-------:|-------:|-------:|
| Total recipe tool calls (main)  |     44 |     35 |     50 |
| record-fragment (main)          |      5 |      0 |     11 |
| complete-phase (main)           |     12 |      7 |      9 |
| build-subagent-prompt (main)    |     13 |     14 |     15 |
| Agent dispatches (main)         |     13 |     14 |     16 |
| recipe errors (main)            |      4 |      3 |      4 |
| complete-phase refusals (main)  |      2 |      1 |      2 |
| zerops_knowledge calls (main)   |      3 |      1 |      0 |

Sub-agent counts (per role) and the refinement state-machine differ:

| role / dispatch sequence | run-40 | run-41 | run-42 |
|--------------------------|--------|--------|--------|
| refinement (pass 1)      | 1      | 1      | 1      |
| refinement-pass-2        | 0      | 1      | 1      |
| refinement-rulewalk      | 0      | 0      | **1**  |
| features-frontend resume | 0      | 0      | **1**  |

## BLOCKING

### B-1 — refinement-2 brief silently omits the `classification` argument
Recurring across runs 40 and 42. In run-42, refinement-pass-2 emitted 17
findings → main agent issued `record-fragment` for the 8 distinct fragments,
two of which (`codebase/api/integration-guide/3`,
`codebase/worker/integration-guide/5`) errored with
`classification is required for fragments on surface "CODEBASE_IG"` — same
shape as the run-40 KB miss.

- run-40: `docs/zcprecipator3/runs/40/SESSION_LOGS/main-session.jsonl:298,303`
- run-42: `docs/zcprecipator3/runs/42/SESSION_LOGS/main-session.jsonl:249-256`

I grepped the run-42 refinement-pass-2 dispatch prompt — string
`classification` does not appear anywhere. The agent recovered by guessing
`platform-invariant` / `intersection`, but it took two wasted record-fragment
calls per ambiguous-class fragment plus a slow re-read cycle. Same fix should
work for refinement-pass-1 (run-42 ln=128-142 in
`subagents/agent-a5668cb6fec32cadf.jsonl`: the first three `record-fragment`
calls included no classification; the next three retried with the class —
exact same pattern).

### B-2 — features-frontend silently terminated mid-pass in run-42
The original `features-frontend` sub-agent
(`subagents/agent-a48097e027fb27d84.jsonl`, 941s, 164 lines) ended with a
clean summary text but only emitted **1 `record-fact`**, vs. the run-40/41
peers that emitted 11 / 15. The main agent dispatched a NEW
`features-frontend-resume` sub-agent (`subagents/agent-ab8710ec7a81ec2fd.jsonl`,
560s, 10 record-facts) with a "pick up where the previous agent left off"
brief.

- dispatch at `runs/42/SESSION_LOGS/main-session.jsonl:123`
- prompt explicitly says "previous frontend sub-agent stopped partway"

The original agent did not signal failure — terminal summary claimed completion
but on-disk state didn't match. There is no current substrate primitive to
detect a sub-agent that emits a "done" message while skipping its core
deliverable. The main agent caught it manually before calling complete-phase
on the feature phase, but only by inspection. Worth a feature-phase
verification gate (e.g. count expected record-facts vs emitted).

### B-3 — refinement-rulewalk is an undocumented THIRD refinement entry
Run-42 dispatched a third refinement-class sub-agent AFTER finalize already
closed: `refinement-rulewalk`
(`subagents/agent-a3170eb4fedfb04ce.jsonl`, dispatched at
`main-session.jsonl:290`, 332s, 116 lines).

Trace: main agent at ln=286 called `enter-phase phase=refinement` (post-finalize),
ln=289 `build-subagent-prompt briefKind=refinement`, ln=290 dispatched as
"refinement-rulewalk". The sub-agent itself called `complete-phase
phase=refinement` (twice — first refused because pre-stitch, then OK after
calling `stitch-content`). The status before/after:

- main ln=287 (post enter-phase): `completed=[..., finalize, research, provision]`
  (no `refinement`)
- main ln=295 (status post-rulewalk): `completed=[..., refinement, ...]`

So `refinement` was entered and re-completed AFTER `finalize` had already
closed at ln=279. This is NOT a documented contract step. The
`run-42-validation.md` calls this a "triple-refinement state-machine
confusion"; my read is more specific: the brief at ln=289 used
`briefKind=refinement` again (same kind as ln=197), pointing at the rule-walk
rules in `derived_rules.md`. The subagent's exit text claims "Engine will
dispatch refinement2 (cross-surface audit) next" — i.e. the rulewalk agent
thought it was the FIRST refinement pass and didn't know refinement-pass-2
had already run. The phase ordering currently lets refinement re-enter
after finalize closes, which the rulewalk agent then exploits.

Fix recommendation: engine should reject `enter-phase phase=refinement`
when `completed` already contains `refinement`, OR the briefKind contract
needs explicit "follow-up rulewalk" branch.

## NOTABLE

### N-1 — run-42 sub-agents are 1.5-3.4× slower than run-40/41 peers
`scaffold-api` 561s/666s → **1888s** (3.4×). `features-backend`
1212s/1178s → **2017s** (1.7×). Across the run-42 sub-agent fleet the
median wall-time roughly doubled. Pattern persists even for sub-agents
whose tool-call counts are similar (e.g., `claudemd-author-api`:
74s/46s → 177s). Possible causes: bigger briefs (refinement-2
substrate hardening), slower zerops_knowledge round-trips, model
variance. Not a bug, but a measurable cost.

Source: per-subagent first/last ts in
`subagents/agent-*.jsonl` (computed via duration script).

### N-2 — refinement-pass-1's contract still leaks "no classification"
Same shape as B-1, but inside the sub-agent itself.
Run-42 `refinement-pass-1` made 6 `record-fragment` calls; 3 of them omitted
`classification` and got rejected, then 3 retries succeeded
(`subagents/agent-a5668cb6fec32cadf.jsonl:128-142`). Refinement-pass-1's
brief presumably tells it `mode=replace, keep classification` but the
classification value isn't echoed back into the brief.

### N-3 — run-42 main agent recipe-call ratio: 11 record-fragments out of 50 (22%)
Up from run-40 (5/44, 11%) and run-41 (0/35). Reason: post-refinement-2
the validator triage moved into main-agent space (refinement-2 is
diagnosis-only by contract). This is **by design** but inflates main-agent
context window meaningfully — 8 fragments × replace bodies ≈ ~30KB of
write payload that the main agent now holds.

### N-4 — main-agent path-resolution miss before refinement-2 ACTs
4 file-not-found errors at `main-session.jsonl:220-222,229` — the main
agent tried `/var/www/zcprecipator/nestjs-showcase/environments/<N>/import.yaml`
when the actual stitched paths are `nestjs-showcase/<N - Name>/import.yaml`
(no `environments/` prefix). Agent recovered with an `ls` at ln=230 then
re-Read. Minor wasted turn, but it's an authoring-contract signal: the
brief should hand the agent canonical stitched paths, not paths under
`environments/`.

### N-5 — features-frontend ended with valid-looking summary despite incomplete work
Tail of `subagents/agent-a48097e027fb27d84.jsonl` reads "Frontend is
ready and waiting for backend endpoints to land" — i.e. the agent
self-stopped pending a dependency. The substrate has no signal for
"I'm waiting on external work, dispatch me again later." Main agent
inferred the gap from on-disk inspection (resume brief mentions
"trust on-disk state over the summary text" — that's a smoking-gun
hint that the substrate is papering over a known-incomplete signal).

### N-6 — recurring pattern: complete-phase finalize refused once per run
Every run had at least one finalize refusal:
- run-40 `:240` — "requires refinement sub-agent dispatch first"
- run-41 `:229` — same
- run-42 `:193, :271` — same (twice in run-42 because of refinement-2)

This is the expected engine refusal (it's documented). What's
noise-worthy is that the run-42 first refusal AT ln=193 happened
because the agent went straight from env-content completion → finalize
without entering refinement. Recurring with run-40, run-41. Engine
nudge is working; brief could pre-empt the wasted call.

### N-7 — env-content emitted 94 record-fragments in run-42 vs 85 in 40/41
9-fragment delta in `env-content` sub-agent
(`subagents/agent-a33238334c2262b8b.jsonl`). Likely refinement-2-driven
expansion of env-comment scope. Worth verifying that the extra 9
fragments aren't redundant.

## NOISE — ruled out

- **The "triple-refinement" naming** is real (rulewalk is a separate
  third pass), but it's NOT "refinement-1 ran twice + refinement-2 once"
  as the validation report's wording implies. Sequence is: refinement-pass-1
  → refinement-pass-2 → finalize closed (with retry) → refinement re-entered
  → refinement-rulewalk → close. Three DISTINCT refinement-class passes.
- **"Parent-recipe fetch by main agent at research phase"** — this is NOT
  present in run-42 main session at all
  (`main-session.jsonl:13-30` shows start → update-plan → complete-phase
  research with zero `zerops_knowledge` calls). Was present in run-40
  `:20` and run-41 `:21` (both fetching `nestjs-minimal`). The
  validation report's "documented for substrate cleanup" framing is
  accurate but credit run-42 for not regressing here.
- **Action typos** — none found. My initial enum-mismatch report was a
  false positive from a wrong action allow-list; every action across
  all three runs is valid (`start`, `update-plan`, `enter-phase`,
  `build-subagent-prompt`, `record-fragment`, `stitch-content`,
  `complete-phase`, `emit-yaml`, `status`, `set`, `mount`).
- **Rapid build-subagent-prompt sequences** (ln 137-148 in run-42 etc.)
  are not retries — they're parallel-issued prompts for the 3 codebases
  × 2 briefKinds (codebase-content + claudemd-author). Confirmed by
  distinct fragmentIds.
- **Main agent's "all 17 valid, ACT on every one" claim** is consistent
  with 8 record-fragments — 17 findings collapse to 8 unique
  fragmentIds; agent's actual edits cover all 8. The wording was
  imprecise but the action was correct. Refinement-pass-2 also
  correctly emitted ZERO record-fragments (diagnosis-only contract
  honored).

## Recurring patterns flagged for substrate work

| pattern | seen in | priority |
|---------|---------|----------|
| classification omission in refinement record-fragment briefs | 40, 42 | B-1 |
| complete-phase finalize without refinement → refused | 40, 41, 42 | N-6 |
| record-fragment post-finalize-close (env-content edits) | 40, 42 | benign |
| sub-agent self-stops with rosy summary while incomplete | 42 (only) | B-2 |
| refinement-class re-entry after finalize | 42 (only) | B-3 |

## Top BLOCKING + NOTABLE summary

1. **B-1 classification omission in refinement-2 brief** — every record-fragment
   on CODEBASE_KB / CODEBASE_IG surfaces hits the same error then retries.
   Recurring run-40 + run-42.
2. **B-2 features-frontend silent self-stop** — sub-agent claims done in
   summary text but skipped >90% of its record-fact emissions, forcing a
   resume dispatch in run-42.
3. **B-3 third refinement pass (rulewalk) re-enters refinement post-finalize**
   — undocumented state-machine path, exploited by the agent without engine
   refusal.
4. **N-1 run-42 sub-agents 1.5-3.4× slower** than run-40/41 peers
   (scaffold-api went from ~600s to 1888s).
5. **N-4 main agent missed stitched-path prefix** during refinement-2 triage
   (`environments/` vs `<N> — Name/`), 4 file-not-found errors before ls
   recovery.
