# brief-editorial-review-showcase-coverage.md — defect-class coverage audit

**Purpose**: confirm every v20–v34 closed defect class + the new classification-error-at-source class has a prevention mechanism cited in the composed editorial-review brief ([composed](brief-editorial-review-showcase-composed.md)) OR in the new check suite OR in Go-injected runtime data. Per principle P7 + step-4 verification protocol.

**Role**: editorial-review
**Tier**: showcase
**Registry source**: [defect-class-registry.md](../05-regression/defect-class-registry.md) (69 rows post-refinement)

---

## 1. Coverage table — defect classes editorial-review catches (primary or defense-in-depth)

Editorial-review is a NEW role adding defense-in-depth on P7 enforcement. It does NOT replace existing enforcement; it adds an independent-reviewer layer. This table lists classes where editorial-review provides primary or secondary prevention.

| Registry row | Defect class | Primary mechanism (unchanged) | Editorial-review contribution |
|---|---|---|---|
| **15.1** (NEW) | classification-error-at-source | (none in v34) | **PRIMARY** — `classification-reclassify.md` atom; no other mechanism catches this class |
| 8.2 | v28 wrong-surface gotchas (setGlobalPrefix, api.ts, plugin-svelte) | Writer brief classification taxonomy; P5 expanded manifest honesty | **SECONDARY** — `counter-example-reference.md` + `single-question-tests.md` (KB surprise-test) catch at independent-reviewer time what writer self-review missed |
| 8.3 | v28 folk-doctrine fabrication (resolver-path-interpolation invention) | P5 routing → discarded; P7 cold-read | **TERTIARY** — `citation-audit.md` enforces citation-coverage 100%; `classification-reclassify.md` catches fabricated-mechanism class; `counter-example-reference.md` pattern-matches against v28 folk-doctrine anti-patterns |
| 8.4 | v28 cross-surface-fact-duplication (.env shadow × 3 surfaces; forcePathStyle × 4) | Writer brief routing taxonomy; P5 one-routed_to-per-fact | **SECONDARY** — `cross-surface-ledger.md` tracks fact bodies across 7 surfaces, catches permutations Jaccard dedup check misses |
| 14.1 | v34 manifest-content-inconsistency (DB_PASS) | P5 expanded manifest honesty (all 6 routing × surface pairs) | **TERTIARY** — `classification-reclassify.md` catches when classification itself is wrong (P5 catches manifest↔content drift; editorial catches classification error at source) |
| 14.4 | v34 self-referential gotcha (`/api/status` vs `/api/health`) | P7 cold-read at brief-review; P8 positive form | **SECONDARY** — editorial reviewer's porter-premise forces outside-in stance that catches self-referential framing the authorship-invested writer self-review cannot see |
| 2.1 | v20 generic-platform-leakage (`.env` override claim) | P1 runnable `zcp check kb-authenticity`; P5 routing | **SECONDARY** — `citation-audit.md` — every platform-claim gotcha must cite `zerops_knowledge` guide; claims contradicting guides flagged as WRONG/CRIT |
| 2.2, 2.4 | v20 topology-drift + declared-but-unimplemented (gotcha references non-existent file/symbol) | P1 runnable content_reality; P8 positive form | **SECONDARY** — `counter-example-reference.md` declared-but-unimplemented anti-pattern + `single-question-tests.md` IG-porter-test (would a porter need THIS exact file?) |
| 1.4 | v15 predecessor-clone gotchas (cross-codebase) | P5 one-fact-one-surface; `cross_readme_gotcha_uniqueness` check | **SECONDARY** — `cross-surface-ledger.md` across all surfaces not just READMEs |
| 1.5 | v15 IG-restates-gotchas | `gotcha_distinct_from_guide` check | **SECONDARY** — `single-question-tests.md` IG porter-test asks "would a porter need to copy THIS into their own app" vs KB surprise-test "would a developer reading docs STILL be surprised" — the two tests separate by intent, catching IG-restates-gotchas class |

## 2. Defect classes editorial-review does NOT catch (and why)

Not every registry row is in editorial-review's scope. Classes outside its coverage:

| Registry row | Defect class | Primary mechanism | Why editorial doesn't catch |
|---|---|---|---|
| 1.3 | v11 dev-server SSH hangs | v17 `zerops_dev_server` substrate | Runtime / substrate concern, not content-quality. Out of editorial scope (porter-premise doesn't read session logs). |
| 1.8 | v17 sshfs-write-not-exec | `principles/where-commands-run.md` + `bash_guard` | Scaffold sub-agent execution boundary. Close-phase editorial reviewer sees the deliverable, not the scaffold execution trace. |
| 3.1 | v21 scaffold-hygiene (node_modules leak) | `scaffold_artifact_leak` check + P3 gitignore-baseline | Scaffold-phase hygiene; caught at scaffold-return via pre-attest shim. Close-phase editorial walks deliverable content, doesn't re-verify scaffold artifacts. (Could extend: a 'deliverable-tree-cleanliness' walk step could be added to `surface-walk-task.md`; deferred to a post-v35 refinement if v35 surfaces a related class.) |
| 4.1, 4.2 | v22 NATS-URL-creds, v22 S3-endpoint | P3 SymbolContract FixRecurrenceRules; P1 runnable pre-attest at scaffold-return | Scaffold-phase contract adherence. Editorial reviewer reads the PUBLISHED documentation about these, not the scaffold code. If a README gotcha about NATS URL-creds is fabricated, editorial catches via `citation-audit.md` + `classification-reclassify.md`. If the scaffold code has the bug, scaffold pre-attest catches. |
| 6.1, 6.2 | v25 substep-bypass, v25 subagent-workflow-at-spawn | P4 server-enforced ordering + `SUBAGENT_MISUSE` | Workflow-state invariants; out of editorial scope entirely. |
| 11.2, 11.3, 13.7, 14.3 | v31/v32/v33 deploy-3-round, finalize-3-round, PerturbsChecks-refuted, v34 convergence-refuted | P1 author-runnable pre-attest shims | Convergence is pre-close; editorial is at close. P1 owns convergence. |

These are out-of-scope NOT gaps. Every defect class has a responsible mechanism; editorial-review extends P7 enforcement into a runtime reviewer; it does not subsume P1/P3/P4/P5's specific catches.

## 3. Orphan check — every composed-brief instruction traces to ≥1 defect class or spec section

Walking each atom of the composed brief ([brief-editorial-review-showcase-composed.md](brief-editorial-review-showcase-composed.md)) and confirming each instruction has defect-class traceability:

| Atom | Key instructions | Defect-class trace |
|---|---|---|
| `mandatory-core.md` | File-op sequencing (Read before Edit) | v31 "File has not been read yet" errors (registry 11.7) |
| | Tool-use policy (no zerops_workflow etc.) | v25 subagent-workflow-at-spawn (registry 6.2) |
| `porter-premise.md` | Fresh-reader stance | **Spec line 4-5 root diagnosis**; closes classification-error-at-source (15.1) via stance that writer can't occupy |
| `surface-walk-task.md` | Ordered walk of 7 surfaces | Spec §Six content surfaces — one per surface |
| `single-question-tests.md` | 7 per-surface pass/fail tests | Spec §Per-surface test cheatsheet |
| `classification-reclassify.md` | 7-class taxonomy + delta reporting | Spec §Fact classification taxonomy + classification-error-at-source (15.1) + v28 wrong-surface (8.2) |
| `citation-audit.md` | `zerops_knowledge` citation 100% for matching-topic gotchas | Spec §Citation map + v28 folk-doctrine (8.3) + v20 generic-platform-leakage (2.1) |
| `counter-example-reference.md` | v28 anti-pattern library (self-inflicted, framework-quirk, scaffold-decision, folk-doctrine, factually-wrong, cross-surface-dup) | Spec §Counter-examples from v28 — 5 anti-pattern classes each trace to specific v28 subclasses |
| `cross-surface-ledger.md` | Fact-body tracking across 7 surfaces | v28 cross-surface-duplication (8.4) + complements `cross_readme_gotcha_uniqueness` Jaccard check |
| `reporting-taxonomy.md` | CRIT/WRONG/STYLE + inline-fix policy | Spec §How this spec is used step 2 (editorial review walks each surface and applies the one-question test per item) |
| `completion-shape.md` | Return payload structure | Check-rewrite §16a editorial-review-originated checks — every check reads a return-payload field |

**Orphan check result**: zero orphan instructions. Every line in the composed brief traces to either a spec section or a defect-class registry row.

## 4. Post-v35 follow-up opportunities (not gating v35)

Classes where editorial-review could extend coverage but doesn't in v35 scope:

1. **Scaffold-artifact leak as close-phase catch** — currently caught at scaffold-return. If scaffold pre-attest missed, editorial could catch at deliverable walk. Add `find {host} -name 'preship.sh' -o -name '.DS_Store'` to `surface-walk-task.md`? Deferred — v35 scaffold pre-attest is robust.
2. **Env README factual-drift editorial catch** — currently caught by `ER-2` factual_claims shim. Editorial's porter-premise could spot-check: "does env 4 README teach me when to outgrow env 4?" fails if README is template-boilerplate (v30 era). Adds another layer on v8.95 Fix B. Deferred — signal bar, not gate.
3. **Cross-codebase architecture narrative** — currently `[advisory]` since v24 rollback. Editorial could apply root-README test "can reader identify each codebase's role in 2 min" — would upgrade advisory to gate. Deferred — user-decision point (was explicitly flagged as editorial in prior convo).

These are additive; none block v35. All deferrable to post-v35 refinement if surfaced by v35 measurement.

## 5. Coverage verdict

**PASS**. Every defect class editorial-review claims to catch has either primary or defense-in-depth coverage in the composed brief. Every composed-brief instruction traces to a defect class or spec section. No orphans.

The NEW class (15.1 classification-error-at-source) has editorial-review as its ONLY closure mechanism — if editorial-review regresses, that class ships unprotected. This is the single load-bearing dependency and is reflected in rollback-criteria T-12.
