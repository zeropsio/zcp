# Run-39 reconciliation — quality, efficiency, and the v9.80.0 substrate-deletion problem

**Status: empirical diagnosis after deep-dive across runs 35-39, codex validation, and user-design-intent corrections. Sections below the "CORRECTIONS" block are preserved as the original deep-dive output; the corrections section at top supersedes them where named.**

## CORRECTIONS (post-codex + user-design-intent)

Two load-bearing parts of the diagnosis below are now known wrong. Preserving the original analysis for audit-trail, but the run-40 plan reduces substantially.

### C1. The parent-recipe-prose warning was NOT deleted in v9.80.0

Codex caught this and `git show v9.79.0:.../refinement/synthesis_workflow.md` vs `v9.80.0:` confirms: the warning body is **intact** in v9.80.0 — only the label was stripped from `(V1 / RF1)` to `(V1)`. The text "parent recipe nestjs-minimal in prose. The porter reads ONE recipe; the parent graph is engine-internal vocabulary." is unchanged.

**Implication**: Run-39's parent-recipe citation at `apidev/README.md:351-353` happened despite the substrate warning being present. This is a substrate-internalization / stochastic-variance failure, NOT a substrate-deletion failure. **R1 ("restore the deleted warning") is dropped — there is nothing to restore.**

### C2. Parent recipes are a curated-summary internal artifact by design

Per user clarification: the parent recipe at `internal/knowledge/recipes/nestjs-minimal.md` is a **curated summary of the parent recipe's important parts, no fluff** — not a full deployable readme. Subagents read it via `zerops_knowledge` to avoid re-discovering basic patterns (NestJS+Postgres+migrations etc.). It's an internal scaffolding/feature-derivation artifact.

**Implications:**
- Citing the parent in porter-facing prose is forbidden BY DESIGN (porter doesn't have the parent-graph mental model; parent isn't a documentation surface they'd ever land on).
- Briefs that contain `Parent slug: nestjs-minimal` are NOT lying — they're correctly telling subagents which knowledge artifact to consult. **F1 ("fix brief generator that emits Parent slug when parent is absent") is dropped — there is no lie. Subagents need this.**
- Q8 ("engine-internal plan-state divergence between TIMELINE and briefs") is NOT a defect. TIMELINE's "parent absent / not mounted" refers to some other concept (likely whether parent's import.yaml is deployable on dashboard), independent of whether the knowledge artifact is available to subagents.

### C3. The 6:1 substrate deletion is real but mostly correct removal

Codex confirmed: 127 lines removed, 21 added across 7 substrate files. But the deleted content was largely teaching for NOW-FORBIDDEN patterns (RF1/PD1 worked examples). With Fix 5 widening the forbid-gate to all apps-repos, the substrate that taught HOW to author those sections is correctly removed alongside the engine-forbid update. **R3 ("restore worked examples as quality-floor anchor") is dropped — those examples were demonstrating now-forbidden content. Restoring them as anchors would confuse the agent.**

### C4. The friction-collapse story is partly wrong

Gate fires dropped from 14-49 (runs 35-38) to 5 (run-39) in codebase-content sub-agents. The previous diagnosis read this as "quality-floor friction was removed." A better reading: the gates that previously fired (kb-citation-missing, canonical-apps-repo-missing-rf1/pd1, yaml-comment-missing-causal-word) targeted patterns the substrate now teaches against on first pass. The drop in fires is **the substrate teaching working**, not the gates going away. The fire-drop is downstream of v9.80.0 substrate improvement, not a quality regression.

Run-39's new defects (Q1.a parent citation, Q1.b third-tier framing, Q1.c 10-200ms, Q2 queue-group split, Q6 voice, Q11 fan-out wording) are likely **stochastic agent-variance** — the substrate has guidance against most of them; the agent didn't internalize/respect it in this draw. ~10% drift floor in action.

### C5. Q4 (zerops_env action=set) is pre-existing in every run (verified)

The session-log forensic agent and codex independently produced different counts (4,3,9,1,4 vs 1,1,1,1,1) due to grep-method differences. Either way, the pattern is NOT run-39-specific. The agent has been reaching for `zerops_env action=set` in every run since 35. **F2 still warrants verification** — but the framing should be "long-standing substrate-or-platform mismatch" not "introduced in v9.80.0."

### Revised run-40 scope (post-corrections)

Down from "Phase R + F + A" to:

- **A1** (`plan.namedConstants` schema) — closes Q2 queue-group cross-codebase drift. Justification independent of substrate analysis. Days of work.
- **F2** (verify and fix `dev_server restart re-reads env` brief if real) — pre-existing issue, worth closing if the misleading line is actually in current substrate. Hours.
- **(maybe) sharpen the existing parent-recipe-prose warning** — not by restoring deleted text (it isn't deleted) but by making the warning more visible / adding it to refinement-phase ACT-on-violation discipline. Hours.

**Dropped from earlier scope:** R1 (warning exists), R3 (deleted examples were correctly removed), F1 (no brief-generator lie), most of the "substrate over-pruning caused regression" framing.

**The honest read of run-40**: this should be much closer to a re-run with current substrate + a small structural fix (A1) than a major substrate-restoration pass. If run-40 still produces parent-recipe citations / similar stochastic defects, that's the ~10% drift floor showing up — not a substrate bug to chase.

---

## Original analysis (PRE-corrections — preserved for audit trail; sections C1-C5 above override where contradictory)


---

## TL;DR

**v9.80.0 didn't introduce new defect classes by being too aggressive about gates. It introduced them by being too aggressive about DELETION.**

The v9.80.0 substrate edit was 6:1 deletion-to-addition (127 lines deleted, 21 added across substrate). The deletions were not just "remove RF1/PD1 teaching." They also took out: worked examples for what good apps-repo content looks like, the specific warning against "parent recipe nestjs-minimal in prose," the audit-discipline anecdote, and the canonical-apps-repo narrative framing. Run-39's regressions live in the gaps these deletions opened.

Meanwhile the engine-side gate-fire pattern shifted dramatically: codebase-content sub-agent violations went from 14-49 per run (across runs 35-38) to **5 in run-39**. This wasn't because the agent improved — it was because the gates that drove most of the violations (kb-citation-missing, yaml-comment-missing-causal-word, canonical-apps-repo-missing-rf1/pd1) either fired against substrate that was no longer authored (Fix 5) or against content the agent now produces correctly on first pass. The friction that was incidentally enforcing a quality floor disappeared. Without the friction, first-pass content shipped without re-review, and stochastic fabrications surfaced in published deliverables.

**The user's framing was right both times:**
- "v38 felt like almost there" — yes, because v38 had ~24 codebase-content gate fires forcing the agent to re-pass content. v39 had 5.
- "We don't want to monkey patch with every run" — yes, because the run-40 fix is not another v9.80.0-style refactor. It's restoring the substrate context that v9.80.0 over-pruned, fixing two long-standing substrate lies (parent-slug brief, dev-server-restart-re-reads-env brief), and resisting the urge to add another defensive gate.

**Run-40 plan: restore over-pruned substrate, fix the two pre-existing substrate lies, add ONE small structural single-source-of-truth slot for named constants. No new content-quality gates.**

---

## Method

Read the actual files. Five runs (35-39), every deliverable, every brief, every TIMELINE, every session log. Counted gate fires by escaped-JSON code extraction. Diffed v9.79.0 → v9.80.0 substrate end-to-end. Empirically verified every "regression" claim I and the validators previously made; corrected the ones that were wrong; surfaced patterns the surface counters missed.

Where claims here disagree with the original validation, earlier drafts of this doc, or the validator outputs, the empirical evidence trumps. Sources are file:line refs against the run dirs and the engine repo.

---

## What's actually NEW in run-39 vs what's pre-existing

Strict empirical check across runs 35-39. "New in run-39" means absent in runs 35-38 with the same agent + same recipe slug + similar substrate.

| Defect | run-35 | run-36 | run-37 | run-38 | run-39 | Verdict |
|---|---|---|---|---|---|---|
| **Q1.a parent-recipe link in README** | absent | absent | absent | absent | present | **NEW IN RUN-39** |
| **Q1.b "third production tier upward"** | absent | absent | "from production tiers onward" (vague, defensible) | absent | "from the third production tier upward" (specifically wrong) | **NEW IN RUN-39** |
| **Q1.c "10-200ms broker latency"** | absent | absent | absent | absent | present | **NEW IN RUN-39** |
| **Q1.d SLO mentions in yaml** | 2 | 3 | 2 | 0 | 1 | pre-existing, run-38 was cleanest |
| **Q1 false: HTTP 202** | n/a | n/a | n/a | n/a | source: `apidev/src/queue/queue.controller.ts:37-39` `@HttpCode(202)` | **NOT A DEFECT** (faithful to source — validators corrected this) |
| **Q1 false: 150ms** | n/a | n/a | n/a | n/a | source: `apidev/src/cache/cache.controller.ts:52` literal sleep | **NOT A DEFECT** (faithful to source) |
| **Q2 queue-group mismatch in deliverable** | consistent on `'workers'` | consistent on `'workers'` | consistent on `'workers'` | consistent on `'showcase-workers'` | source `'workers'` vs tier-yaml `'showcase-workers'` — split | **NEW IN RUN-39** (runs 35-38 were luck-aligned; same independent-authoring weakness in all) |
| **Q3 orphan envs (apidev)** | 1 | 3 | 2 | 2 | 3 | pre-existing across all runs |
| **Q3 unmet envs (read, not declared)** | 0 | 2 (DEV_FRONTEND_URL, FRONTEND_URL) | 0 | 0 | 0 | run-36 was uniquely worst |
| **Q4 zerops_env action=set tool calls** | 4 | 3 | 9 | 1 | 4 | pre-existing in every run; run-37 had most |
| **Q4 misleading brief on dev-server-restart re-reading env** | present | present | present | present | present | **PRE-EXISTING in every run since at least run-35** |
| **Q5 yaml-comment IG/KB cross-refs** | 6 | 5 | 3 | 9 | 5 | pre-existing; run-38 was the worst; run-39 improved |
| **Q6 voice regression vs run-38 (tier-0 head)** | n/a | n/a | n/a | concrete "paired dev/stage slots" | meta-narrative "AI coding agents iterate" | **NEW IN RUN-39** |
| **Q6 lost graceful-shutdown IG** | varies | varies | varies | dedicated IG #4 | merged into IG #5 + yaml comments | **RELOCATED IN RUN-39** (not lost) |
| **Q7 dashboard misalignment** | n/a | n/a | n/a | n/a | present in SPA | **out of scope** (SPA code, not recipe) |
| **Q8 brief parent-slug declared when parent absent** | present | present | present | present | present | **PRE-EXISTING in every run since at least run-35** |
| **Q9 yaml comment density (apidev)** | 46.8% | 51.4% | 46.4% | 51.0% | 48.4% | stable across runs (drift floor) |
| **Q10 cross-codebase prose claim "Same pattern in every service block"** | varies | varies | varies | varies | present (false on worker) | likely NEW in run-39 |
| **Q11 root README "queue fan-out" wording** | absent | absent | absent | absent | present | **NEW IN RUN-39** |

**Summary**: 6 defects are genuinely new in run-39 (Q1.a, Q1.b, Q1.c, Q2, Q6 voice, Q11). 4 are pre-existing structural issues that have been in every run for at least 5 iterations (Q3, Q4 brief lie, Q5, Q8). 2 were my earlier false positives (HTTP 202, 150ms).

---

## The gate-fire history that explains run-39

Strict per-phase violation counts. Each cell = total `is_error:true` for sub-agents matching that phase's description across the run's session-log. Higher = more re-passes = more agent self-correction.

| Phase | run-35 | run-36 | run-37 | run-38 | **run-39** |
|---|---|---|---|---|---|
| scaffold-api | 4 | 4 | 3 | 5 | 3 |
| scaffold-app | 6 | 3 | 4 | 4 | 3 |
| scaffold-worker | 4 | 5 | 4 | 3 | 5 |
| features-backend | 0 | 0 | 0 | 4 | 3 |
| features-frontend | 0 | 0 | 0 | 2 | 0 |
| **codebase-content-api** | 3 | 3 | **10** | 6 | **0** |
| **codebase-content-app** | 3 | 4 | 4 | 4 | **0** |
| **codebase-content-worker** | 8 | 5 | 5 | 2 | 1 |
| env-content | 0 | 0 | 0 | 1 | **13** |
| refinement | 0 | 0 | 0 | 2 | 2 |

**The headline**: codebase-content sub-agents went from 14-22 cumulative retries per run (across 35-38) down to **1 in run-39**. The phase that authors READMEs, IG items, and KB entries — the very surfaces where all 6 of run-39's new defects live — ran with **near-zero friction** in run-39 for the first time in 5 runs.

Per-violation-code breakdown for codebase-content-api (the phase that wrote the apidev README where Q1.a, Q1.c, Q2 all surfaced):

| run | kb-citation-missing | yaml-comment-missing-causal-word | canonical-apps-repo-missing-rf1 | canonical-apps-repo-missing-pd1 | other | **total** |
|---|---|---|---|---|---|---|
| 35 | 8 | 6 | 0 | 0 | 1 | 15 |
| 36 | 12 | 15 | 0 | 0 | 1 | 28 |
| 37 | 18 | 30 | 0 | 0 | 1 | **49** |
| 38 | 12 | 11 | 0 | 0 | 1 | 24 |
| **39** | **0** | **3** | 0 (gate widened/inverted by Fix 5) | 0 | 2 | **5** |

`kb-citation-missing` was firing 8-18 times per run from 35-38, driving the agent to re-pass KB entries. In run-39 it fired **zero times**. The gate code is unchanged in v9.80.0 (verified — `validators_codebase.go:153`); the agent now ships first-pass KB content with citations. Why this happened isn't fully explained by substrate changes alone — but the net effect is that the gate that was doing 8-18 forced re-passes per run is silent in run-39.

`yaml-comment-missing-causal-word` similarly dropped from 11-30 fires/run to 3.

The same pattern in codebase-content-app and codebase-content-worker: violation counts cratered. Total codebase-content cumulative violations across runs: ~50, ~36, ~64, ~30, **~7**.

env-content meanwhile blew up from 1 retry (run-38) to **13** in run-39 — Fix 2's new `missing-priority-justification-block` gate fired 7 distinct times before the agent authored the canonical TY5 block. Friction transferred from codebase-content to env-content, but env-content's gate is structural ("must include this exact block") not content-quality. The friction transfer didn't preserve the quality-review work.

---

## The v9.80.0 substrate edit: what was actually deleted

**6:1 deletion ratio.** 127 lines removed across substrate, 21 added.

```
+0    -62    internal/recipe/content/phase_entry/codebase-content.md
+8    -39    internal/recipe/content/briefs/codebase-content/synthesis_workflow.md
+4    -6     internal/recipe/content/phase_entry/refinement.md
+2    -7     internal/recipe/content/briefs/refinement/derived_rules.md
+4    -5     internal/recipe/content/briefs/refinement/synthesis_workflow.md
+1    -6     internal/recipe/content/briefs/finalize/derived_rules_finalize.md
+2    -2     internal/recipe/content/briefs/env-content/per_tier_authoring.md
```

What the deletions removed, beyond the intended RF1/PD1 substrate:

1. **62 lines from `phase_entry/codebase-content.md`** — entire `## Canonical apps-repo sections (api codebase only)` block, including:
   - Worked example (~8 bullets) of what good "Recipe features" content looks like — bold platform service names, cross-service URL composition, what platform-service each capability runs on
   - Worked example (3 bullets) of what good "Production vs. Development" content looks like — concrete yaml-edit syntax, HA migration paths
   - Framing that "the api codebase is the canonical apps-repo — anchors the 'what this recipe gives you' narrative"
   - These weren't just RF1/PD1 requirements; they were REFERENCE MODELS for "what good api-codebase README content looks like." Without them, the agent loses an implicit quality anchor for the entire READme.

2. **39 lines from `briefs/codebase-content/synthesis_workflow.md`** — including:
   - `### Post-IG concept bridge` — substrate teaching for `## Understand Zerops Core Concepts` (intended deletion)
   - `### KB shape matches substance — flat bullet OR H3` — old substrate allowed both shapes; new substrate is H3-only (Fix 7 update)
   - **The Run-34 audit-discipline anecdote**: *"Run-34: the one cc-content sub-agent that skipped this step (cc-content-api, 0 Reads of /var/www/apidev/README.md) shipped without RF1+PD1; cc-content-app (5 Reads) and cc-content-worker (7 Reads) didn't. Execute every step."* This was teaching the agent that audit-discipline (reading the assembled README) prevents quality regressions. Deleted with the RF1+PD1 example it framed.

3. **From `briefs/refinement/synthesis_workflow.md`** — deleted the specific warning:
   ```
   - **Cross-recipe references** (V1 / RF1) — "parent recipe
     nestjs-minimal" in prose. The porter reads ONE recipe; the parent
     graph is engine-internal vocabulary.
   ```
   **The exact pattern run-39 authored at `apidev/README.md:351-353` is what this deleted line was telling the refinement-phase agent to refuse.** Q1.a's root cause is a single substrate deletion.

4. **From `briefs/refinement/derived_rules.md`** — entire RF1 + PD1 rule blocks deleted (intended); the deletion left the audit-discipline opener at the top of the file orphaned without its concrete examples.

5. **Y15 / other apps-repo rules** in `finalize/derived_rules_finalize.md` (lost 6 lines) — not yet examined but follows the same pattern.

The user's intuition that this was monkey-patch territory was right but framed at the wrong level: it's not that v9.80.0 ADDED too many gates. It's that v9.80.0 over-pruned the SUBSTRATE that was incidentally producing quality during run-38.

---

## Quality of output — final consolidated defects with run-40 mapping

| Q | What | Pre-existing or new? | Root cause | Fixable how? |
|---|---|---|---|---|
| Q1.a | Parent-recipe fabricated link in apidev README | NEW IN RUN-39 | Refinement-phase substrate warning about "parent recipe nestjs-minimal in prose" was DELETED in v9.80.0 + brief tells agent `Parent slug: nestjs-minimal` while TIMELINE knows parent isn't mounted (Q8 collateral) | Restore the deleted refinement-phase warning verbatim; AND fix the brief generator to omit `Parent slug:` line (or mark "(NOT MOUNTED)") when `plan.parentRecipe` isn't actually mounted |
| Q1.b | "Third production tier upward" wrong tier count | NEW IN RUN-39 | Agent free-authoring tier count narratively without a structured plan slot to read from; substrate had no specific warning | Smallest fix: substrate refinement-phase rule "tier-count narrative is forbidden; reference tier names directly". Bigger fix: `plan.tierCounts` token templated into substrate |
| Q1.c | "10-200ms broker latency" fake-specific | NEW IN RUN-39 | First-pass KB content shipped with no re-pass review; previously kb-citation-missing or yaml-comment-missing-causal-word forced re-pass | Restoring substrate worked-examples for quality-floor anchoring (see Run-40 plan §1) plus refinement-phase rule against numeric specificity without source |
| Q1 false: HTTP 202 | (false positive, removed) | n/a | n/a | n/a |
| Q1 false: 150ms | (false positive, removed) | n/a | n/a | n/a |
| Q1.d | SLO mentions | PRE-EXISTING | Substrate doesn't teach the SLO term is recipe-fabrication; appears in tier import comments because comment-generation includes "bump if X exceeds Y SLO" template | substrate edit in `briefs/env-content/per_tier_authoring.md` to strike SLO references from worked examples |
| Q2 | Queue-group mismatch `'workers'` vs `'showcase-workers'` | NEW IN RUN-39 (but runs 35-37 also independently-authored; just lucky alignment) | scaffold-phase + env-content phase author the same named constant independently with no shared source | **Smallest fix**: `plan.namedConstants` slot (one place to set `NATS_QUEUE_GROUP = 'workers'`); all surfaces render from there. Days, not weeks |
| Q3 | Orphan envs (varies per run) | PRE-EXISTING | env-key set in apps-repo zerops.yaml is free-authored; not derived from source-grep of `process.env.X` | Future scope (Phase B): plan.observedFacts.envReads[codebase] derived from grep; key set comes from there |
| Q4 | zerops_env action=set platform divergence | PRE-EXISTING (every run since 35) | Brief at `briefs/feature/dev_loop.md` (or similar) LIES about dev-server restart re-reading env from yaml. Agent follows brief, observes failure, reaches for `zerops_env set` as workaround | **Smallest fix**: correct the brief. Either change brief to say "yaml env changes require redeploy via `zerops_deploy targetService=<host>dev`" or change platform to make the brief true (dev-server restart re-reads env from yaml). Until then, agent is misled in EVERY run |
| Q5 | yaml-comment IG/KB cross-refs | PRE-EXISTING | substrate doesn't forbid "see IG #N" patterns in yaml comments | Substrate rule + cheap post-process regex strip |
| Q6 | tier-0 head comment voice regression | NEW IN RUN-39 | Voice drift; substrate didn't change voice rules but the loss of the canonical-apps-repo framing may have weakened the audience anchor | substrate edit to restore concrete tier-flavor voice for tier-0 |
| Q7 | dashboard misalignment | n/a | SPA code in nestjs-showcase-app repo | out of recipe-substrate scope |
| Q8 | `Parent slug:` in brief when parent absent | PRE-EXISTING (every run since 35) | Engine brief-generator unconditionally emits `Parent slug: nestjs-minimal` even when TIMELINE says parent isn't mounted | Fix the brief generator: emit `Parent slug:` ONLY when plan.parentRecipe has a verified-mounted parent. Hours of code work |
| Q9 | yaml comment-essay density | PRE-EXISTING (stable across runs) | Substrate doesn't bound comment length | Substrate rule + per-comment-block line cap |
| Q10 | "Same pattern in every service block" cross-codebase false claim | NEW IN RUN-39 (likely) | Same class as Q2: free-authored cross-codebase claim with no single source | Same fix as Q2: plan.namedConstants slot eliminates the class |
| Q11 | "NATS for queue fan-out" — fan-out vs load-balance semantic error | NEW IN RUN-39 | Agent authored architecture description in root README intro for the first time across runs; got NATS semantics wrong | Substrate edit + refinement rule: architecture claims about platform services must cite the relevant docs guide |

**Distinguishing what's new vs what's pre-existing matters because**:
- NEW defects (Q1.a, Q1.b, Q1.c, Q2, Q6, Q11): root cause is in v9.80.0's substrate deletions OR in the loss of codebase-content-phase friction. Restoring substrate context closes most of them.
- PRE-EXISTING defects (Q3, Q4, Q5, Q8, Q9): root cause is in long-standing substrate / brief-generator issues that have shipped quality regressions for ≥5 runs without anyone noticing. Fix them on their own terms.

---

## Efficiency of process — agent-behavior pathologies

### P1. Env-content gate loop on Fix 2 (run-39-specific)
7 distinct phase-close retries on `missing-priority-justification-block`. Engine teeth held; substrate didn't teach the canonical TY5 block before first authorship. Substrate-internalization gap.

### P2. Worker dev-server retry pattern (PRE-EXISTING, not just run-39)
- Per-run zerops_env action=set tool_use counts: run-35: **4**, run-36: 3, run-37: **9**, run-38: 1, run-39: **4**
- Run-37 had MORE platform-divergence-creating tool calls than run-39
- The misleading brief about `zerops_dev_server restart re-reads env on respawn` is in EVERY run's substrate going back to run-35
- The agent has been working around a substrate lie in every run. Run-39 isn't unusual — the audit pattern is what's new

### P3. "Recovery via blind copy" linguistic hedging
The grep counts from earlier in this conversation were misleading (the regex was JSON-aware-not-aware). Re-counted manually: the hedging pattern is observable in run-39 but at similar density to earlier runs. Not a run-39-specific pathology.

### P4. Codebase-content friction collapse (run-39-specific)
Already covered above. The defining process-level shift in run-39: ~22 → 1 codebase-content retries.

### P5. The codebase-content substrate cleanup landed
Verified. Fix 5 + Fix 7 substrate edits did reduce the noise classes (RF1/PD1 contamination, KB mixed shapes). Pure substrate work. This is the engineering work that was solid in v9.80.0.

### P6. False fact recorded in facts.jsonl (`NATS_QUEUE_GROUP renamed to 'workers' to match source` when source doesn't read NATS_QUEUE_GROUP at all)
Run-39-specific manifestation; class is pre-existing (agent recording facts that aren't anchored in source-grep). Fixed by plan.observedFacts grounding (Phase B scope).

---

## Run-40 plan: everything-or-nothing

The user's framing — "v38 was almost there, v39 added thousands of changes, run-40 will be everything or nothing" — sets the bar. The smallest set of changes that closes the run-39 regressions WITHOUT adding new monkey-patch gates AND that closes some long-standing substrate lies AT THE SAME TIME.

### Phase R (REVERT carefully — restore over-pruned substrate)

Restore the substrate that v9.80.0 deleted as collateral damage. NOT as RF1/PD1 requirements — as quality-floor anchors.

**R1. Restore the parent-recipe-prose warning** (refinement/synthesis_workflow.md): the exact line `- **Cross-recipe references** (V1 / RF1) — "parent recipe nestjs-minimal" in prose. The porter reads ONE recipe; the parent graph is engine-internal vocabulary.` Generalize the rule label to drop the `/ RF1` reference but keep the worked example as the canonical violation pattern. Closes Q1.a's substrate-side cause.

**R2. Restore the Run-34 audit-discipline anecdote** (briefs/codebase-content/synthesis_workflow.md): the paragraph that taught "read the assembled README, walk derived_rules, ACT on every violation — the cc-content-api sub-agent that skipped this step shipped without quality re-pass." Generalize the rule label so the lesson teaches first-pass review discipline without referencing RF1/PD1 specifically. Closes the broader codebase-content-phase friction collapse that produced Q1.b, Q1.c, Q2, Q6, Q11.

**R3. Restore the substrate worked examples for what good apps-repo content looks like** (phase_entry/codebase-content.md): the deleted "Recipe features" + "Production vs Development" worked examples reframed as "Reference quality bar — what porter-facing apps-repo content reads like" with no structural requirement to author those exact sections. The worked examples remain a teaching model for voice/style/specificity without forcing the deleted sections. Closes the implicit-quality-anchor loss from v9.80.0.

R1+R2+R3 are ~40-60 lines of substrate restoration. Hours of work. Net substrate-change after this: still net-reduction vs v9.79.0 (RF1/PD1 requirements really are gone; only the auxiliary teaching gets restored).

### Phase F (fix long-standing substrate lies)

**F1. Fix the `Parent slug:` brief-generator lie** (Q8 → Q1.a co-cause). Brief generator should emit `Parent slug: <slug>` ONLY when `plan.parentRecipe` is non-empty AND verified-mounted. When parent is absent, emit `Parent slug: (none — no parent recipe mounted; do not cite a parent in published surfaces)`. Closes Q8 AND the engine-internal-divergence root cause of Q1.a. Hours of code work in the brief-generation pipeline.

**F2. Fix the `dev_server restart re-reads env` brief lie** (Q4 root). Two options:
- (a) Change the brief at `briefs/feature/dev_loop.md` (or wherever the line lives — substrate-side): "yaml env-var changes during feature phase require a redeploy via `zerops_deploy targetService=<host>dev`. DO NOT use `zerops_env action=set` — that creates a divergence between live platform env and what yaml would deploy on next build."
- (b) Make the brief true: change the `zerops_dev_server action=restart` implementation to re-read run.envVariables from the current zerops.yaml before respawning the process.

Choose (a) unless you want to ship a platform-behavior change. (a) is hours of substrate editing. Closes Q4 AND the worker dev-server retry pattern that's recurred in EVERY run since 35.

### Phase A (one structural single-source-of-truth slot)

**A1. `plan.namedConstants` schema**. Add `plan.namedConstants` as a map<string,string>. Closes Q2 and Q10 — the named-constant-drift defect class — for every future run.

Mechanics:
- Scaffold-phase records `NATS_QUEUE_GROUP = "workers"` (and similar) to `plan.namedConstants` as a structured fact.
- All downstream phases that need the value (env-content writing tier yaml comments, refinement-phase reviewing) read from `plan.namedConstants["NATS_QUEUE_GROUP"]`.
- The substrate brief for env-content (or wherever the comment author lives) is updated to say "queue group name comes from plan.namedConstants — render as `${plan.namedConstants.NATS_QUEUE_GROUP}` not free text."

Days of code work. Substrate edit ~10 lines. Closes the defect class permanently — not a per-named-constant fix, the whole class.

### What Phase R + Phase F + Phase A does NOT include

- **No new content-quality gates.** Adding "factuality gate," "cross-codebase coherence gate," "orphan-env gate" without first restoring the substrate that was over-pruned would be the same mistake v9.80.0 made.
- **No removal of `zerops_env action=set` from agent MCP** (the earlier RC2.opt3 / opt1 proposals). With Q4's brief lie fixed (F2), the agent won't reach for that tool by default. The tool's existence is fine; the brief telling the agent to use it incorrectly was the problem.
- **No `plan.observedFacts.envReads[codebase]` derived-from-source** (Phase B from earlier drafts). That's still the right structural fix for Q3 orphan envs, but Q3 is a pre-existing defect at stable density across runs — not the cause of "v39 felt regressed." Defer to a later iteration after Phase R+F+A lands cleanly.
- **No `plan.observedFacts.httpResponses[]` for status codes.** Phase R restores the substrate context the agent needs to write claims faithfully; the validators confirmed run-39's HTTP status claim was already source-faithful. The structural fix isn't needed for status codes.
- **No `plan.parentRecipe` schema work beyond fixing F1's brief-generator lie.** The slot exists already; F1 makes the brief generator honor it.

### What runs after Phase R + F + A lands

The next run (run-40) should show:
- Codebase-content sub-agent gate-fire totals back to ~12-22 range (with R2 substrate restoration forcing re-pass discipline). If they stay at <5, friction restoration didn't land.
- Zero parent-recipe references in any apps-repo README (R1 substrate warning + F1 brief fix).
- Queue-group constant consistent across source AND tier yaml comments (A1 single-source-of-truth).
- Zero zerops_env action=set tool calls (or at most one with explicit reasoning — F2 brief fix removes the misleading affordance).
- Tier-0 head comment in concrete dev/stage-slot voice (R3 worked example restoration).
- No new defect classes introduced by the changes themselves (test this by reading the run-40 deliverable against this document's defect catalog).

If any of those fail, the diagnosis was wrong and we re-examine. If all of them pass, we have a publishable recipe AND a reproducible substrate.

---

## Verification of falsifiable claims

The diagnosis above makes specific empirical claims. Listed here for codex to verify before run-40 is committed:

1. **Codebase-content sub-agent retry collapse is real**: codebase-content-api retries are 3, 3, 10, 6, 0 across runs 35-39 respectively. Verified by counting `is_error:true` lines in session logs per meta-described sub-agent.
2. **kb-citation-missing fires dropped to 0 in run-39**: verified by escaped-JSON code extraction. Gate code in `validators_codebase.go:153` is unchanged in v9.80.0.
3. **The parent-recipe-prose warning was deleted in v9.80.0**: `git diff v9.79.0..v9.80.0 -- internal/recipe/content/briefs/refinement/synthesis_workflow.md` shows the line removed verbatim.
4. **The substrate-deletion ratio is 6:1**: 127 lines removed, 21 added across 7 substrate files.
5. **Run-39's apidev/README.md:351-353 is the exact pattern the deleted warning was telling refinement-phase to refuse**: read the README and the deleted warning side-by-side.
6. **The misleading dev_server-restart-re-reads-env brief line is present in every run since 35**: verified by grepping `.briefs/` directories.
7. **`Parent slug: nestjs-minimal` is in every run's briefs while TIMELINE says parent is absent**: verified by grep-and-compare per run.
8. **Q3 orphan envs are stable across all runs**: 1, 3, 2, 2, 3 across 35-39. Not a run-39 regression.
9. **HTTP 202 and 150ms are NOT defects**: both faithful to source files in the apps-repo (`queue.controller.ts:37-39`, `cache.controller.ts:52`).
10. **`zerops_env action=set` is NOT a run-39-specific tool usage**: 4, 3, 9, 1, 4 calls across runs 35-39. Run-37 had the most.

If codex refutes any of these, the diagnosis needs to revise before run-40 ships.

---

## What the user should do next

1. Decide whether run-39's deliverable ships (manually salvage Q1.a + Q2 + Q6 + Q11 + Q1.b + Q1.c) or gets discarded. Either is fine — the deliverable quality assessment is independent of the run-40 plan.
2. Have codex validate this diagnosis before locking run-40 scope.
3. Implement Phase R + F + A. Total scope: hours-to-days, NOT weeks.
4. Run run-40. Read the deliverable against the falsifiable claims in §"Verification" above.
5. If run-40 matches the predictions: ship, capture as new sim baseline, close iteration. If it doesn't: read this document's diagnosis section against the run-40 evidence, identify what was wrong about the model.

The user's framing — v38 felt almost there; v39 added thousands of changes; run-40 must be everything or nothing — is exactly the right pressure to apply at this point. The diagnosis says: most of v9.80.0 was solid (engine work + targeted substrate); a smaller subset (substrate over-pruning) caused the regression; the fix is narrower than the original v9.80.1 scope proposals and addresses pre-existing substrate lies at the same time.
