# HANDOFF-to-I10-v39-prep.md — context-tightening edition

**For**: fresh Claude Code instance picking up zcprecipator2 after v38 analysis shipped as PAUSE. Your job is to **land the 5-commit v39 stack**, tag the release, hand back for v39 commission, then analyse v39.

**Reading order** (≈ 60 min):

1. This document front-to-back.
2. [`runs/v38/verdict.md`](runs/v38/verdict.md) — PAUSE direction stands; detailed reasoning superseded by CORRECTIONS.
3. [`runs/v38/CORRECTIONS.md`](runs/v38/CORRECTIONS.md) — what v38's first analysis missed. Sets v39's scope.
4. [`plans/v39-fix-stack.md`](plans/v39-fix-stack.md) — the 5 commits you execute. THIS IS THE PLAN.
5. [`../spec-content-surfaces.md`](../spec-content-surfaces.md) — the spec every content-emitting path must honor. Commit 1 gold test binds to §5. Examples-bank seeding draws from §11.
6. [`spec-recipe-analysis-harness.md`](spec-recipe-analysis-harness.md) — analysis harness unchanged from v38.
7. [`HANDOFF-to-I8-v37-prep.md`](HANDOFF-to-I8-v37-prep.md) §6 — analysis discipline rules. Inherited unchanged + one addition (Rule 9 — see §6 below).
8. [`../../CLAUDE.md`](../../CLAUDE.md) — auto-loaded; TDD + operating norms.

**Branch**: `main`. **Previous tag**: `v8.112.0`. **Your target**: `v8.113.0`.

---

## 1. Slots to fill at start

```
FIX_STACK_TAG:            v8.113.0
FIX_STACK_COMMITS:
                          Commit 1  engine-template grounding       44463b9
                          Commit 2  manifest export whitelist       ec8a0fe
                          Commit 3  content-authoring context pack  c3a6362 (example bank + writer brief input block)
                                                                    e025dd3 (finalize-yaml visibility + generate-step examples + per-codebase zerops.yaml + audit trims)
                          Commit 4  knowledge-lookup workflow step  7db26dc
                          Commit 5  writer brief slim + starter     c4291b6
                                    todos
V38_RETRO_REPORT:         /tmp/v38-retro-after-v113.json
V38_RETRO_EXPECTED_DELTAS_CONFIRMED:
                          Commit 1 gold-test — v38 env READMEs at
                            /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/environments/
                            CONTAIN forbidden phrases the gold test flags:
                            env 1/README.md has "expanded toolchain" (Cluster A);
                            env 4/README.md has "daily snapshot" (Cluster C) and
                            "managed services scale up" variant (Cluster D).
                            Confirmed via `grep -l` against the shipped v38 tree.
                          Commit 3b gold — finalize-step guidance for showcase
                            plans now renders each env's import.yaml schema-only
                            BEFORE the agent writes envComments. F-21 ("2 GB
                            quota" fabrication class) closes at source —
                            TestFinalizeStepGuidance_IncludesRenderedYaml pins it.
                          Commit 4 citations gate — v38 writer manifest schema had
                            no citations[] field at all; every content_gotcha +
                            content_ig entry in the manifest shape the v38 run
                            emitted would fail the citations-present gate
                            retroactively (the v38 deliverable ships without a
                            manifest at the root — F-23 — so direct grep is not
                            possible, but the old schema had no guide_fetched_at
                            timestamp anywhere).
                          Writer brief size at HEAD: 57.5KB (testShowcasePlan
                            fixture, no per-codebase yamls on disk). At v8.112.0
                            (measured from runs/v38/dispatch-integrity/engine-
                            writer.txt) it was 59.4KB. Net −1.9KB on the atom
                            corpus; real sessions add ~2-4KB of inlined per-
                            codebase zerops.yaml for 3-codebase showcase
                            (captured content, not prose rules). The aspirational
                            25KB target from plans/v39-fix-stack.md is not met;
                            per user direction the audit optimized TIGHTNESS
                            (dropping redundant prose: citation-map 6.2→3.8KB,
                            self-review-per-surface 5.5→4.1KB, content-surface-
                            contracts drop-examples trim, fact-recording-
                            discipline principle off brief, classification-
                            taxonomy + routing-matrix → 1.5KB pointer + runtime
                            action=classify) over byte target. Size tripwire
                            TestBuildWriterBrief_UnderSizeLimit pins at 65KB.
V39_COMMISSION_DATE:      <unfilled — user commissions>
V39_SESSION_ID:           <unfilled>
V39_OUTCOME:              <unfilled>
V39_VERDICT:              <unfilled>
```

Fill slots as phases complete.

---

## 2. The v38 meta-lesson that set v39's direction

v38 proved Cx-5's `BuildSubagentBrief` byte-correct AND proved the writer still produced folk-doctrine with a 60KB brief AND proved editorial-review caught 5 CRITs on a 72%-truncated 13KB brief.

The read: brief volume is the problem, not the solution. The earlier v39 plan (git SHA `ff3dcfb`) tried to fix this by adding enforcement — 11 commits of checks, gates, retroactive scans. The user pushed back: "why not tighten the context so the agent produces good output first-time, instead of catching bad output with machine checks."

The user was right. v39 rewrote around three principles:

**(a) Agents see source-of-truth, not inventions.** Writing envComments? Engine renders the yaml and shows it to you first — you can't invent a number that's not there. Writing gotchas? Facts log is the input, pre-routed; you paraphrase from zerops_knowledge, not from memory. Writing CLAUDE.md? The scaffold file tree is visible in your input.

**(b) Examples beat rules.** Replace prose teaching with annotated examples. A bank of ~15-20 example-files seeded from spec-content-surfaces.md §11 (bad cases) + v38 post-correction content (good cases). Engine injects 2-3 relevant examples into each content-emission substep. Pattern-matching against concrete shapes beats parsing prose rules.

**(c) Right-size the brief to the role.** Writer drops 60KB → ~18KB. Classification tables move to a runtime `action=classify` lookup. Wrong-role atoms (fact-recording-discipline) get dropped. Main agent gets a 3KB comment-authoring topic injected at generate/finalize, not a sub-section of a 3000-line doc.

The bet: most of the machine-check infrastructure the earlier plan wanted becomes unnecessary if the input side is right. One hard gate survives — `readmes_citations_present` in Commit 4 — because it turns a judgment call ("is this folk-doctrine?") into a file-existence check ("did the knowledge fetch happen before the bullet was written?").

**Your job on v39 analysis: verify all three principles hold.** Retrospective run proves the new checks catch known v38 defects. Forward run against v39 proves editorial-review CRIT count drops from 5 to ≤ 1, writer brief stays ≤ 25KB, envComments contain no invented numbers.

---

## 3. Defect inventory at v39-prep time

| Defect | Status at v8.112.0 | v39 commit closing it |
|---|---|---|
| F-9 / F-12 / F-13 / F-24 | Closed | none (stays closed) |
| F-17 (architectural) | Closed | none (stays closed) |
| F-17-runtime (paraphrase at dispatch) | Open | **Self-closed** — 60KB → 18KB brief has no compressible redundancy; main agent forwards verbatim. No guard needed. |
| F-23 (manifest in tarball) | Open | Commit 2 |
| F-GO-TEMPLATE (hardcoded env prose) | Open | Commit 1 |
| F-ENFORCEMENT-ASYMMETRY (main-agent thin teaching) | Open | Commit 3 (content-authoring context pack) |
| F-21 (envComment factuality) | Partial | Commit 3b (yaml-visible-before-authoring closes at source) |
| F-14/15/16 writer first-pass compliance | Partial | Commit 3c (examples) + Commit 4 (citation gate) |
| Main-agent TodoWrite re-planning noise (28 calls in v38) | Open | Commit 5b (starter todos) |

All closures are at authoring time via tighter context, except Commit 1 (Go code, no agent) and Commit 4 (the one hard gate).

---

## 4. Operating order

### Phase 1 — Land the v39 fix stack (4–5 days)

Execute [`plans/v39-fix-stack.md`](plans/v39-fix-stack.md) commit-by-commit. All five are mostly parallel-safe (see plan §7). Suggested single-person order:

1. **Commit 1** (longest scope; 2-3 days including the Day-1 bullet audit). Flag (b) bullets back to the user before dropping them or promoting to schema.
2. **Commit 2** (30 min). Trivial; land early to unblock anyone debugging F-23.
3. **Commit 3** (2 days). Example bank + engine inject points. Seeding the bank is the design work — aim for ~5 examples per surface, ~15-20 files total. Draw bad examples from spec-content-surfaces.md §11; draw good examples from v38 post-correction content (per-codebase READMEs after editorial-review fixes).
4. **Commit 4** (4 hours). Schema extension on writer completion-shape + one step-gate check.
5. **Commit 5** (4 hours). Brief slim + starter todos + new classify action.

Green gate per commit: RED test present + implementation + `go test <package> -race -count=1` green + `make lint-local` green.

Tag as `v8.113.0` after all 5 land.

### Phase 2 — Retrospective harness run + tag

Run the harness against v38 deliverable:

```
./bin/zcp analyze recipe-run \
  /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38 \
  /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/SESSIONS_LOGS \
  --out /tmp/v38-retro-after-v113.json
```

Expected (per §1 `V38_RETRO_EXPECTED_DELTAS_CONFIRMED`): Commit 1's gold test flags v38's fabricated env-README bullets; Commit 4's citation check flags v38's writer manifest as missing guide_fetched_at timestamps.

If these don't fire, the checks aren't doing what they claim — iterate before tagging.

Tag `v8.113.0`.

### Phase 3 — Hand back for v39 commission (15 min)

1. Fill `FIX_STACK_COMMITS` slot in §1 with the 5 SHAs.
2. Notify user: fix stack shipped; v39 can be commissioned.
3. **Do not commission autonomously.**

### Phase 4 — v39 analysis

Inherit discipline from [`HANDOFF-to-I8-v37-prep.md §6`](HANDOFF-to-I8-v37-prep.md) + Rule 9 (see §6 below):

1. **Harness first.** `./bin/zcp analyze recipe-run ...` + `generate-checklist`. Commit evidence floor immediately.
2. **Flow traces.** role_map.json + extract_flow.py.
3. **Fill checklist.** Read every writer-authored file PLUS env READMEs PLUS root README. For each content-quality cell, grade against spec tests AND verify the prose claim maps to source data (Rule 9).
4. **Read editorial-review's full completion payload.** Not just the attest result — the per-surface findings table.
5. **Measure the three principle signals**:
   - Writer brief size at dispatch (target ≤ 25KB).
   - `zerops_knowledge` call count in writer sub-agent log (target: ≥ 1 per citation-map-topic gotcha bullet).
   - envComment claimed numbers vs rendered yaml (target: every number in a comment appears in the yaml).
6. **Spot-check for source-fabrications.** grep current `recipe_templates.go` output against the rendered env READMEs — any divergence means the three-way-equality gold test has a hole.
7. **Draft verdict.md** with hook-enforced SHAs + citations + soft-keyword cap.
8. **One commit at end** for the analysis bundle (+ evidence-floor commit from step 1).

### Phase 5 — Verdict decision

**PROCEED** (all must hold):
- All 5 v39 commits closed their targets (see §3 defect inventory).
- Editorial-review returns ≤ 1 CRIT on first pass (v38 had 5).
- Writer brief ≤ 25KB at dispatch (v38 was 60KB).
- envComments contain no claimed number absent from the rendered yaml.
- Zero `readmes_citations_present` failures.
- Deploy retry rounds ≤ 1, finalize rounds ≤ 1.
- Manifest in deliverable tarball.

**ACCEPT-WITH-FOLLOW-UP**: one signal-grade issue (e.g. 2 editorial-review CRITs instead of ≤ 1; one envComment with an invented claim the yaml-visibility didn't catch). Document as v8.113.x patch; v40 follows.

**PAUSE**: any headline gap — F-17-runtime resurfaces, env READMEs still ship fabrications, main agent still re-plans TodoWrite > 5 times, > 2 editorial-review CRITs.

**ROLLBACK-Cx**: one of the 5 commits introduced a worse regression than the defect it closed.

---

## 5. What v39 is NOT testing

- C-15 recipe.md deletion (R2-R7 per [`PLAN.md`](PLAN.md) §2). Deferred until v39 PROCEED.
- Framework diversity (stays nestjs).
- Minimal-tier independent track.
- Publish-pipeline.

---

## 6. Analysis discipline rules (inherited from I8 + Rule 9)

Rules 1-8 per [`HANDOFF-to-I8-v37-prep.md §6`](HANDOFF-to-I8-v37-prep.md) — re-read before writing any verdict prose.

**Rule 9 (new, surfaced by v38 first-pass failure)**: **No PASS grade on content-quality cells without cross-checking prose claims against source data.** If an env README says "Runtime containers carry an expanded toolchain", grep the env's import.yaml for what differs from the adjacent tier; if no field backs the claim, grade FAIL with fabrication citation. The harness can catch this automatically after Commit 1 ships; but until then (and as defense-in-depth forever), the analyst verifies.

---

## 7. Traps and operating rules

Inherited from I8 + I9 + new for v39:

1. **Analysis is NOT implementation.** Document new defects; don't fix them in the analysis commit.
2. **Cite `file:line` or `row:timestamp`.** Never "probably".
3. **One commit at end for analysis.** Code changes in separate commits per CLAUDE.md.
4. **Stop at verdict.** Do NOT autonomously start HANDOFF-to-I11.
5. **Fetch editorial-review's full completion payload.** Last assistant turn in `SESSIONS_LOGS/subagents/agent-*.jsonl` for the editorial-review agent. Summarize by CRIT count in the verdict; list each CRIT in the checklist. Don't summarize as "returned clean" without reading.
6. **Check source-fabrications.** grep `recipe_templates.go` rendered output against deliverable. Divergence = gold test missed a case.
7. **Rule 9 cross-check on every content cell.** Source data first, spec test second. No shortcuts.

---

## 8. Success definition

- [ ] 5 commits merged; `v8.113.0` tagged.
- [ ] `go test ./... -race -count=1` green.
- [ ] `make lint-local` green.
- [ ] Retrospective harness against v38 catches known defects per §1 slot.
- [ ] `FIX_STACK_COMMITS` slot filled.
- [ ] User commissioned v39.
- [ ] `runs/v39/{machine-report.json, verification-checklist.md, verdict.md}` present; hook passes.
- [ ] Verdict shipped.
- [ ] Run-log entry in [`PLAN.md`](PLAN.md) §4.

---

## 9. Starting action

1. Read the 8 items in §1 reading order.
2. Verify tree clean on `main` + tests green.
3. Begin Commit 1 (Day-1 bullet audit — flag (b) bullets to the user before proceeding).
4. Commit 2 in parallel if multi-tasking (30-min fix).
5. Commits 3-5 after Commit 1 audit settles. Most are parallel-safe.
6. Retrospective harness run against v38.
7. Tag v8.113.0.
8. Fill §1 slots. Notify user.
9. Wait for user to commission v39. **Do not commission autonomously.**
10. Post-commission: analysis per Phase 4.
11. Ship v39 verdict.
12. Append v39 entry to [`PLAN.md`](PLAN.md) §4.
13. Stop. Hand back. User decides next.

v38 surfaced that teaching volume was the bottleneck. v39 inverts: tight context, examples, source-visible authoring. Fewer checks, fewer gates, more source-of-truth exposure. The bet is that the agents' output quality tracks with how well the input resembles the right answer — not with how many rules the brief contains.
