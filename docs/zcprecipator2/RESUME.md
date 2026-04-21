# RESUME — zcprecipator2 research state

**Last updated**: 2026-04-20 (steps 1-6 complete + research-refinement 2026-04-20 complete — editorial-review role added)
**Current instance**: Opus 4.7 (1M context), fresh session primed on RUNBOOK + README + CLAUDE.md + recipe-version-log (top §1-§5, architectural insights v20→v23, milestones table, v21–v34 per-version entries) + `internal/content/workflows/recipe.md` block structure + all 12 `internal/tools/workflow_checks_*.go` non-test files.

**Refinement note (2026-04-20)**: a review session surfaced that `docs/spec-content-surfaces.md` line 317-319 prescribes an editorial-review role that the original research phase absorbed into writer self-review, collapsing author and judge onto one agent. A refinement pass added the editorial-review sub-agent role to the architecture. This updated RESUME reflects the post-refinement state. The refinement touched: atomic-layout.md, data-flow-showcase.md, data-flow-minimal.md, check-rewrite.md, rollout-sequence.md (inserted C-7.5), rollback-criteria.md (T-11, T-12), defect-class-registry.md (row 15.1 + extended 8.2/8.3/14.1/14.4), calibration-bars-v35.md (§11a ER-1..ER-5, §13 headline updated to 6 bars), and 8 new step-4 verification files under 04-verification/ (editorial-review × {showcase, minimal} × {composed, simulation, diff, coverage}). Research-phase-done signoff is maintained; the 68-artifact baseline extends to 77 artifacts with the refinement.

---

## Open decisions — user-resolved (this session)

Recorded verbatim from the kickoff prompt. These govern step execution until overridden.

| # | Decision | Answer (this run) | Source |
|---|---|---|---|
| 1 | Minimal-tier flow input | **Reconstruct from code + published deliverables (no commissioning).** Resolved 2026-04-20 after consulting `docs/recipes/recipe-taxonomy.md`: Type 3 (minimal) vs Type 4 (showcase) share the same content-surface rules (README fragment shape, zerops.yaml comment depth/ratio, import.yaml WHY-style, CLAUDE.md byte floor); showcase only adds a worker + Valkey + NATS + Object Storage + Meilisearch on top of framework+DB. Minimal flow reconstructed from `internal/content/workflows/recipe.md` (declared substep guides + dispatched briefs) + `internal/workflow/recipe_*.go` (emission layer) + published minimal deliverables (`nestjs-minimal-v1..v3`, `laravel-minimal-v1..v26`), with showcase v34 as the substrate-behavior reference. Escalation rule: commission a targeted minimal run **only if** step 3 surfaces a brief-composition question live evidence is needed to resolve. Known derivation gap: the main-agent inline feature-writing tool trace (the one behavior minimal has that showcase doesn't). Step 1's minimal artifacts must mark cells "derived from spec + deliverable comparison, no live tool timeline" where applicable. | §4.1 RUNBOOK / §8.1 README |
| 2 | Session-log reading granularity | **Split per sub-agent** — one artifact per (run × source). | §4.2 RUNBOOK / §8.2 README |
| 3 | TodoWrite disposition | **Check-off-only.** Keep TodoWrite as a mirror of server substep state; drop the full-rewrite pattern at step-entries. | §4.3 RUNBOOK / §8.3 README |
| 4 | Migration shape (parallel-run vs cleanroom) | **Deferred to step 6.** Do not resolve now. | §4.4 RUNBOOK / §8.4 README |
| 5 | Check-deletion threshold | **Conservative.** Only delete a check when provably redundant given new principle enforcement; every delete carries a one-sentence justification + a test scenario proving upstream handles it. | §4.5 RUNBOOK / §8.5 README |
| 6 | Step parallelization | **Sequential 1 → 2 → 3; parallel 4+5; sequential 6.** | §4.6 RUNBOOK / §8.6 README |

---

## Progress

### Required reading — COMPLETE

- [x] `docs/zcprecipator2/RUNBOOK.md` — read end-to-end
- [x] `docs/zcprecipator2/README.md` — read end-to-end (all 12 sections)
- [x] `CLAUDE.md` — auto-loaded in context
- [x] `docs/zrecipator-archive/recipe-version-log.md` — top (lines 1–300: why-we-log, how-to-explore, how-to-analyze, how-to-evaluate, rating methodology, cross-version summary) + architectural insights (lines 388–478: v20→v23 trajectory, v8.86 shape, verification-direction inversion) + v25 through v34 (lines 1570–2434) in full
- [ ] `internal/content/workflows/recipe.md` — intentionally deferred; per runbook §2 item 4, we reference specific blocks during step 2, no need to memorize up front

### Step 2 — Global knowledge inventory — COMPLETE (2026-04-20)

Artifact inventory under [`docs/zcprecipator2/02-knowledge/`](02-knowledge/):

- [knowledge-matrix-showcase.md](02-knowledge/knowledge-matrix-showcase.md) — full (phase × substep × agent) matrix for showcase tier. Main-agent rows across 6 phases (research/provision/generate/deploy/finalize/close) with 18 substep cells; 6 sub-agent descriptor tables (scaffold×3 + feature + writer + code-review); eager-injection + substep-scoped injection summaries; check surface per phase; coverage audit in §6.
- [knowledge-matrix-minimal.md](02-knowledge/knowledge-matrix-minimal.md) — same matrix shape for minimal tier; 13 substep cells (showcase is 18 — delta breakdown at §7); 2 sub-agent descriptor tables (discretionary writer + ungated code-review); tier-gated checks enumerated in §5; confidence tags (high/medium/low) on every cell per RESUME decision #1.
- [redundancy-map.md](02-knowledge/redundancy-map.md) — 10 redundancy classes with evidence pointers. Marquee: §6 TodoWrite vs zerops_workflow (12 full-rewrites observed, authoritative-state parallel). Others: SSH-rule × 4 paths, file-op sequencing × 5, forbidden-tools list × 6 dispatches, platform principles × multi-path, NATS separate user/pass × 4. Size summary at §10 = ~30 KB redundant context per run (~7.5%).
- [misroute-map.md](02-knowledge/misroute-map.md) — 10 misroute classes with defect-class pointers. v25 substep-bypass class §1 (CLOSED by v8.90, architectural misroute risk stays live — needs substep-order index maintenance invariant). v22/v34 cross-scaffold principle delivery. v33 phantom-tree / dispatcher-vs-transmitted mixing. v32 dispatch compression. feature-sweep-stage 25KB over-carry of content-authoring-brief. Summary by axis at §11.
- [gap-map.md](02-knowledge/gap-map.md) — 10 gap classes with fix-mechanism proposals. v34 cross-scaffold env-var coordination (DB_PASS/DB_PASSWORD) §1 — requires symbol-naming contract (principle #3) + runnable cross-codebase env-var check. v34 manifest↔content inconsistency §2 — requires principle #5 (two-way routing graph). v8.94 workspace_manifest partial-realization §4 — decision needed: wire or delete. Minimal-tier canonical output-tree inconsistency §10.

**Success-criteria check (RUNBOOK §3 step 2)**:

| Criterion | Status |
|---|---|
| Every cell in both tier matrices is populated with evidence (file:line or trace timestamp) | ✅ showcase §1-§5 all cells carry evidence; minimal matrix §1-§5 all cells carry evidence with explicit confidence tags |
| Every item in the redundancy / gap / misroute maps cites a concrete defect class from v20-v34 | ✅ each §1-§11 maps back to named defect classes (v17, v21, v22, v25, v28, v29, v31-v34) |
| No hand-waving: no cell reading "X is probably delivered somewhere" | ✅ absences are marked "—" or "NOT cell-evidenced"; low-confidence minimal cells carry `(proxy)` / `(schematic)` tags |
| Minimal matrix cell count ≥ ~60% of showcase | ✅ ~65% per [knowledge-matrix-minimal.md §7](02-knowledge/knowledge-matrix-minimal.md) |
| TodoWrite vs zerops_workflow pattern (~12 events) included | ✅ [redundancy-map.md §6](02-knowledge/redundancy-map.md) |
| v25 substep-bypass class included (live risk even post-v8.90) | ✅ [misroute-map.md §1](02-knowledge/misroute-map.md) |
| v34 DB_PASS/DB_PASSWORD class included | ✅ [gap-map.md §1](02-knowledge/gap-map.md) |

### Step 1 — Flow reconstruction — COMPLETE (2026-04-20)

Artifact inventory under [`docs/zcprecipator2/01-flow/`](01-flow/):

**Showcase v34** (observed — from `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v34/SESSIONS_LOGS/`):
- [flow-showcase-v34-main.md](01-flow/flow-showcase-v34-main.md) — 169 events, 0 errored
- [flow-showcase-v34-sub-scaffold-apidev.md](01-flow/flow-showcase-v34-sub-scaffold-apidev.md) — 82 events, 3 errored
- [flow-showcase-v34-sub-scaffold-appdev.md](01-flow/flow-showcase-v34-sub-scaffold-appdev.md) — 26 events, 0 errored
- [flow-showcase-v34-sub-scaffold-workerdev.md](01-flow/flow-showcase-v34-sub-scaffold-workerdev.md) — 41 events, 1 errored
- [flow-showcase-v34-sub-feature.md](01-flow/flow-showcase-v34-sub-feature.md) — 90 events, 3 errored
- [flow-showcase-v34-sub-writer.md](01-flow/flow-showcase-v34-sub-writer.md) — 20 events, 0 errored
- [flow-showcase-v34-sub-code-review.md](01-flow/flow-showcase-v34-sub-code-review.md) — 89 events, 0 errored
- [flow-showcase-v34-dispatches/](01-flow/flow-showcase-v34-dispatches/) — 6 Agent dispatches captured verbatim (prompts 6256–15627 chars)

**Minimal** (reconstructed — per decision #1):
- [flow-minimal-spec-main.md](01-flow/flow-minimal-spec-main.md) — spec-derived walk-through with citations to `recipe.md` + `recipe_*.go` + `nestjs-minimal-v3/TIMELINE.md`
- [flow-minimal-spec-dispatches/readme-with-fragments.md](01-flow/flow-minimal-spec-dispatches/readme-with-fragments.md) — brief template (recipe.md L2205-L2388)
- [flow-minimal-spec-dispatches/code-review-subagent.md](01-flow/flow-minimal-spec-dispatches/code-review-subagent.md) — brief template (recipe.md L3050-L3158)

**Cross-tier**:
- [flow-comparison.md](01-flow/flow-comparison.md) — headline structural diffs, substep deltas, topic branching, dispatch counts, flagged-event-class deltas, reconstruction-path honesty statement

**Extractor reused for v35+** (reproducibility):
- [scripts/extract_flow.py](scripts/extract_flow.py) + [scripts/role_map_v34.json](scripts/role_map_v34.json) — produces dispatch captures + per-source trace markdown from any `SESSIONS_LOGS/` tree. Re-runnable against future versions without code changes.

**Success-criteria check (RUNBOOK §3 step 1)**:

| Criterion | Status |
|---|---|
| Every substep in v34 showcase has a documented (tool, result, next-decision) line | ✅ main trace contains 169 tool rows; 31 `zerops_workflow` calls with phase/substep annotation |
| Every sub-agent dispatch has its transmitted prompt captured verbatim | ✅ 6/6 dispatches captured at prompt lengths matching the Agent-tool input |
| Minimal tier has the same level of detail for the selected run | ⚠ **Reduced scope per decision #1**: no live session log → spec-derived walk-through, dispatch templates rather than verbatim dispatches, proxy sizes from showcase where topics are shared. Gaps explicitly enumerated in `flow-minimal-spec-main.md` "Gaps relative to live-session evidence" + `flow-comparison.md` §8 |
| `flow-comparison.md` enumerates every structural difference | ✅ 8 sections: headline diffs, substep-count deltas, topic-delivery deltas, dispatch deltas, flagged-event deltas, check-surface (deferred to step 2), non-deltas, reconstruction-honesty statement |

### Step 3 — Architecture design — COMPLETE (2026-04-20)

Artifact inventory under [`docs/zcprecipator2/03-architecture/`](03-architecture/):

- [principles.md](03-architecture/principles.md) — 8 pressure-tested invariants with defect-class trace. P1–P7 kept from README §5 (all earn their keep); **P8 added** — positive allow-lists, not enumerated prohibitions (v33 three invention classes + README §2 "replaced with positive allow-lists" constraint). §9 is the cross-audit: every v8.78→v8.104 defect class covered by ≥1 principle; no orphans; three classes with defense-in-depth.
- [atomic-layout.md](03-architecture/atomic-layout.md) — 86-atom tree under `internal/content/workflows/recipe/` replacing the 3,438-line monolith. Every atom ≤300 lines. Strict audience separation: `phases/` (main), `briefs/scaffold|feature|writer|code-review/` (single sub-agent role each), `principles/` (pointer-included at stitch time only). §3 has the block-by-block mapping from every current `<block>` to new atom(s) with line citations; §4 specifies the new `SymbolContract` plan field + 12 seeded FixRecurrenceRules (NATS separate creds, S3 forcePathStyle, worker SIGTERM, env-self-shadow, gitignore baseline, skip-git, etc.); §7 handles tier branching.
- [data-flow-showcase.md](03-architecture/data-flow-showcase.md) — ASCII sequence diagrams per phase × (server→main, main→sub-agent, sub-agent→main, main→server, checker→failure-payload). §9 specifies the new failure payload shape (no `ReadSurface/Required/Actual/CoupledWith/HowToFix/PerturbsChecks` — P1 replaces those with author-runnable `preAttestCmd`).
- [data-flow-minimal.md](03-architecture/data-flow-minimal.md) — same phase × arrow coverage for minimal tier. §1 delta summary shows 5 fewer gated substeps + 0 dispatched sub-agents default (tier-branched within the same atomic tree). §11 explicit "escalation trigger" checklist for step-4 cold-read — lists the 4 questions that could force a commissioned minimal run per RESUME decision #1.
- [check-rewrite.md](03-architecture/check-rewrite.md) — every check in all 12 `workflow_checks_*.go` files with disposition. **Totals: 56 keep, 16 rewrite-to-runnable, 1 delete, 5 new = 78 final.** Only 1 definite deletion (`knowledge_base_exceeds_predecessor` — informational since v8.78, upstreamed by `knowledge_base_gotchas` + `knowledge_base_authenticity` rewrite). 6 additional deletion candidates deferred to step-4 simulation per RESUME decision #5 conservative threshold. §16 proposes 5 new architecture-level checks tied to P3/P5/P6/P8 (symbol_contract_env_var_consistency, visual_style_ascii_only, canonical_output_tree_only, manifest_route_to_populated, no_version_anchors_in_published_content). §18 specifies the 16-command `zcp check <name>` CLI shim surface for author-runnable invocation.

**Success-criteria check (RUNBOOK §3 step 3 / README §3.3 step 3)**:

| Criterion | Status |
|---|---|
| Every principle has ≥1 defect-class trace from v20–v34 | ✅ [principles.md §1–§8](03-architecture/principles.md) — every principle lists cited versions |
| Every defect class closed by v8.78–v8.104 has a principle covering it (if not: new principle) | ✅ [principles.md §9](03-architecture/principles.md) cross-audit table — **P8 added** for v33 enumeration class (phantom tree / Unicode box-drawing / auto-export); no orphans |
| Atomic layout has no topic >300 lines | ✅ [atomic-layout.md §5](03-architecture/atomic-layout.md) — three largest atoms intentionally sized at 200–260 with explicit step-4 split escape hatches |
| Every check in the current suite has a disposition | ✅ [check-rewrite.md §2 file index](03-architecture/check-rewrite.md) + §17 summary matrix (78 rows) |
| Tier coverage: minimal AND showcase artifacts | ✅ [data-flow-showcase.md](03-architecture/data-flow-showcase.md) + [data-flow-minimal.md](03-architecture/data-flow-minimal.md) both produced |
| No check silently omitted | ✅ §2 file index + §17 matrix reconcile |

### Step 5 — Regression fixture — COMPLETE (2026-04-20)

Artifact inventory under [`docs/zcprecipator2/05-regression/`](05-regression/):

- [defect-class-registry.md](05-regression/defect-class-registry.md) — **68 registry rows** (target ≥30 per README §3 step 5; exceeded ~2×). One row per closed defect class v6–v34 with fields `id / origin_run / class / mechanism_closed_in / current_enforcement / new_enforcement / test_scenario / calibration_bar`. v6–v19 entries selected where lineage is still live in v20+ fixes or principles (11 rows); v20–v34 exhaustive per README's conservative-registry directive (57 rows). Every row cites a specific version-log section. Every `test_scenario` is expressed in shell-level / data-level predicates (grep / awk / yq / jq / `find` / session-log parse) — re-runnable against a v2 system. Every `calibration_bar` is numeric / grep-verifiable, never qualitative. §2 coverage audit reconciles the row-per-version count + §3 maps every principle P1–P8 to representative rows. §4 names open follow-ups for step-4 cold-read + step-6 migration (rows carrying `(carry)` as `mechanism_closed_in` — v34 manifest / cross-scaffold / convergence-refuted, v22 cross-codebase coherence, editorial rows).
- [calibration-bars-v35.md](05-regression/calibration-bars-v35.md) — **97 bars** aggregated into 10 categories + tier-specific + cross-category. §1 operational substrate (21 bars — wall / bash / errored / schema / SSH / git / dev-server / seed / phantom / auto-export / Unicode / MCP). §2 convergence (11 bars — deploy ≤ 2 rounds, finalize ≤ 1 round, `Scope="downstream"` adoption, out-of-order substeps, first-substep-complete latency, TodoWrite check-off). §3 content quality writer surface (12 bars — gotcha-origin ≥ 80%, 0 self-inflicted, 0 folk-doctrine, 0 version-anchors, CLAUDE.md ≥ 1200b + ≥2 custom sections, IG per-item standalone, cross-README dedup). §4 env-README Go-template surface (4 bars — persistence-fabrication + factual-drift). §5 env import.yaml comment surface (8 bars — two-axis minContainers, service-uniqueness, comment ratio / depth / factual / cross-env). §6 manifest-consistency P5 surface (11 bars covering all 6 routing × surface pairs). §7 cross-scaffold symbol contract P3 surface (9 bars — env-var consistency, NATS separate creds, S3 endpoint, `.gitignore` baseline, SIGTERM / enableShutdownHooks). §8 close review (9 bars — CRIT shipped after fix, WRONG count, silent-swallow, feature coverage, browser walk). §9 dispatch + brief composition integrity (8 bars — version anchors / dispatcher vocabulary / check-name / Go-path / atom-size / byte-identical-MANDATORY / orphan-prohibition lint). §10 tier-specific showcase-only (8 bars) + minimal-only (6 bars) + shared. §11 cross-category derived (5 bars). §12 coverage audit — every README §10 criterion → ≥1 bar, every registry `calibration_bar` → ≥1 bar, every P1–P8 principle gates ≥1 bar. §13 headline — the 5 single-most-important v35 measurements (C-1 deploy rounds, C-2 finalize rounds, C-9 Scope=downstream, M-3b routed_to=claude_md honesty, CS-1 symbol-contract-env-consistency).

**Success-criteria check (RUNBOOK §3 step 5 / README §3 step 5)**:

| Criterion | Status |
|---|---|
| Every defect class named in `recipe-version-log.md` with ✅/❌ verdict has a registry entry | ✅ registry §2.1 table reconciles 68 rows against v6–v34 per-version entries + v8.XX release notes |
| Every entry has a `test_scenario` expressible without current-system Go code references | ✅ registry §2.2 — every scenario uses shell / data predicates; Go-specific hooks appear only in `current_enforcement` column |
| Calibration bars are numeric / grep-verifiable, not qualitative | ✅ calibration-bars §12.2 cross-check + the file's §1–§11 contain only `== N` / `≤ N` / `≥ N` / `grep -c` / `wc -l` thresholds |
| Conservative registry — every v20–v34 defect class named as closed gets a row | ✅ registry §1.4 covers v20 × 6 rows + v21 × 6 + v22 × 6 + v23 × 4 + v25 × 3 + v26 × 2 + v28 × 4 + v29 × 4 + v30 × 2 + v31 × 8 + v32 × 5 + v33 × 7 + v34 × 4 = 57 rows for v20–v34 alone; every per-version `✅/❌`/`fixed-in-v8.XX` marker in the log yields a row |
| Aggregated thresholds cover operational, convergence, content, manifest-consistency, cross-scaffold, close-review, tier-specific dimensions | ✅ calibration-bars §1–§10 categories exactly match the kickoff prompt's required dimensions |

### Step 4 — Context verification — COMPLETE (2026-04-20; parallel with step 5 per RESUME decision #6)

Artifact inventory under [`docs/zcprecipator2/04-verification/`](04-verification/): **36 files** = 9 (sub-agent role × tier) pairs × 4 artifacts per pair.

**Showcase (6 roles × 4 files = 24)**:
- scaffold-apidev: [composed](04-verification/brief-scaffold-apidev-showcase-composed.md), [simulation](04-verification/brief-scaffold-apidev-showcase-simulation.md), [diff](04-verification/brief-scaffold-apidev-showcase-diff.md), [coverage](04-verification/brief-scaffold-apidev-showcase-coverage.md)
- scaffold-appdev: [composed](04-verification/brief-scaffold-appdev-showcase-composed.md), [simulation](04-verification/brief-scaffold-appdev-showcase-simulation.md), [diff](04-verification/brief-scaffold-appdev-showcase-diff.md), [coverage](04-verification/brief-scaffold-appdev-showcase-coverage.md)
- scaffold-workerdev: [composed](04-verification/brief-scaffold-workerdev-showcase-composed.md), [simulation](04-verification/brief-scaffold-workerdev-showcase-simulation.md), [diff](04-verification/brief-scaffold-workerdev-showcase-diff.md), [coverage](04-verification/brief-scaffold-workerdev-showcase-coverage.md)
- feature: [composed](04-verification/brief-feature-showcase-composed.md), [simulation](04-verification/brief-feature-showcase-simulation.md), [diff](04-verification/brief-feature-showcase-diff.md), [coverage](04-verification/brief-feature-showcase-coverage.md)
- writer: [composed](04-verification/brief-writer-showcase-composed.md), [simulation](04-verification/brief-writer-showcase-simulation.md), [diff](04-verification/brief-writer-showcase-diff.md), [coverage](04-verification/brief-writer-showcase-coverage.md)
- code-review: [composed](04-verification/brief-code-review-showcase-composed.md), [simulation](04-verification/brief-code-review-showcase-simulation.md), [diff](04-verification/brief-code-review-showcase-diff.md), [coverage](04-verification/brief-code-review-showcase-coverage.md)

**Minimal (3 roles × 4 files = 12)**:
- scaffold (single inline — main-inline, no Agent dispatch): [composed](04-verification/brief-scaffold-minimal-composed.md), [simulation](04-verification/brief-scaffold-minimal-simulation.md), [diff](04-verification/brief-scaffold-minimal-diff.md), [coverage](04-verification/brief-scaffold-minimal-coverage.md)
- writer (Path A main-inline default; Path B dispatch optional per data-flow-minimal.md §5a): [composed](04-verification/brief-writer-minimal-composed.md), [simulation](04-verification/brief-writer-minimal-simulation.md), [diff](04-verification/brief-writer-minimal-diff.md), [coverage](04-verification/brief-writer-minimal-coverage.md)
- code-review (ungated close — dispatch fires in practice): [composed](04-verification/brief-code-review-minimal-composed.md), [simulation](04-verification/brief-code-review-minimal-simulation.md), [diff](04-verification/brief-code-review-minimal-diff.md), [coverage](04-verification/brief-code-review-minimal-coverage.md)

**Success-criteria check (RUNBOOK §3 step 4 / README §3 step 4)**:

| Criterion | Status |
|---|---|
| Every composed brief reads cleanly cold — no contradictions, no unresolved ambiguities | ✅ every simulation doc ends with a P7 verdict; all 9 pass conditional on small clarifications (named per-doc in §6 "proposed edits") |
| Every removed-vs-v34 line has a disposition (scar / noise / dispatcher / load-bearing-moved-where) | ✅ every diff doc §1 "Removed from v34 → disposition" table enumerates every v34 segment; §5 "silent-drops audit" confirms zero silent drops for each (role × tier) |
| Every v20–v34 closed defect class has a prevention mechanism cited in the composed brief OR new check suite OR Go-injected runtime data | ✅ every coverage doc's §1 table has zero unaddressed rows; N/A classes carry a one-line justification naming the correct responsible role/phase; §3 "Orphan check" section confirms zero orphans per role |
| Tier coverage: minimal AND showcase artifacts | ✅ 24 showcase files + 12 minimal files = 36 total; showcase covers 6 roles, minimal covers 3 (scaffold single-inline, writer, code-review — feature is main-inline in minimal per data-flow-minimal.md §1) |
| Defect registry from step 5 or README §6 seed used | ✅ coverage docs use README §6 seed list + expanded defect-class audit from principles.md §9 cross-audit table; step-5 registry integration is deferred per task-scope note ("use README §6's seed defect registry if step 5's full registry isn't ready yet"); coverage notes flag follow-up opportunity once step 5 full registry emerges |

**Step-4 findings worth carrying into step 6**:

- **v34 manifest-content-inconsistency primary coverage** = writer brief's `routing-matrix.md` atom + expanded `writer_manifest_honesty` check across all routing dimensions + FactRecord.RouteTo field. Secondary coverage = code-review's `manifest-consumption.md` atom. Defense-in-depth confirmed in both writer-showcase-coverage + writer-minimal-coverage + code-review-*-coverage docs.
- **v34 cross-scaffold-env-var** primary closure = `SymbolContract` byte-identical JSON interpolation across all scaffold dispatches. Tier note: structurally impossible for single-codebase minimal; applies unchanged for dual-runtime minimal.
- **v33 phantom output tree** closure = `canonical-output-tree.md` atom's POSITIVE allow-list (per P8) + `canonical_output_tree_only` new check at close-entry. Not negative "forbidden paths" enumeration which was itself an invention vector (v8.103/v8.104 Fix A was still a prohibition; new form is positive declaration).
- **Minimal tier main-inline writer path (Path A)** has partial structural coverage of v28-debug-agent-writes-content (A1 simulation caveat): main cannot truly forget its deploy rounds. Enforcement shifts from process ("fresh context") to output (pre-attest aggregate + manifest honesty + classification taxonomy). This is the primary minimal-tier caveat flagged for step 6 consideration.
- **Minimal tier composition reduction** is the largest byte-budget win in the rewrite: ~54% reduction for scaffold, ~57% for writer, ~49% for code-review. Current minimal tier consumed showcase-shaped blocks with tier-scaffolding; new composition tier-filters showcase-only concerns at stitch time.
- **Stitcher concerns surfaced**: audience-mode adaptation (main-inline vs dispatched), framework-role filtering of reminder snapshots, hostname-list interpolation for single vs dual-runtime minimal, tier-conditional content-surface sections. None are architectural blockers; all resolvable via Go-template conditionals at `recipe_guidance.go` layer.
- **Open questions propagated to step 5/6**: whether code-review should pointer-include `principles/platform-principles/*` atoms (currently framework-only scope); step 5's deletion candidates (6 deferred checks) should resolve based on step-4 simulation findings — all 6 candidates can safely stay as `keep` / `rewrite-to-runnable` per conservative RESUME decision #5.

### Step 6 — Migration path — COMPLETE (2026-04-20)

Artifact inventory under [`docs/zcprecipator2/06-migration/`](06-migration/):

- [migration-proposal.md](06-migration/migration-proposal.md) — resolves open decision #4. Compares **parallel-run on v35** vs **cleanroom** against concrete step-3 evidence on 4 axes: (1) Go-code surface replaced vs added-alongside (§1) — operational substrate unchanged under either candidate; guidance Go is net-neutral (~-400 / ~+400 LoC); content tree grows ~2× from monolith per P6 dedup reversal; check Go grows ~+400 LoC CLI shim + 5 new checks; `StepCheck` payload shape changes (breaking). (2) Runtime coexistence (§2) — additive for SymbolContract + RouteTo + NextSteps; flag-gated for stitcher body + check payload shape; dual-tree maintenance reintroduces the monolithic-mixing class P6 is closing. (3) Shadow-diff cost (§3) — byte-diff already produced in step-4 against captured v34 dispatches; convergence/SymbolContract/manifest-honesty measurements are not shadowable (require agent execution). (4) Rollback cost (§4) — cleanroom is minutes-latency + clean history; parallel is seconds-latency + ossified dual-path. **Recommendation: CLEANROOM** (§6) — structural value is not incrementally-comparable; substrate is untouched so blast radius is bounded; step-4 coverage + step-5 calibration bars + step-6 rollback-criteria form a complete gate regimen.
- [rollout-sequence.md](06-migration/rollout-sequence.md) — 15-commit cleanroom plan; each commit individually revertable with explicit test-layer coverage per CLAUDE.md. C-0 substrate-invariant regression tests → C-1 SymbolContract plan field → C-2 FactRecord.RouteTo + manifest schema → C-3 atom_manifest.go scaffold → C-4 86 atom files land → **C-5 recipe_guidance.go stitcher rewrite (CUTOVER)** → C-6 5 new architecture-level checks → C-7 16 checks rewritten + `zcp check` CLI shim → C-8 writer_manifest_honesty P5 expansion → C-9 predecessor-floor delete → C-10 StepCheck payload shape shrunk (breaking) → C-11 NextSteps emptied at close → C-12 DISPATCH.md → C-13 build-time lints → C-14 v35 dry-run + calibration-bar scripts → C-15 recipe.md + recipe_topic_registry.go deleted. Summary table reconciles all 8 principles + 68 registry rows + 97 calibration bars against the commit set.
- [rollback-criteria.md](06-migration/rollback-criteria.md) — go/no-go thresholds. ROLLBACK triggers (§2.1) = 10 mechanically-measured gates including C-1 deploy rounds > 2, C-2 finalize rounds > 1, T-3 substrate invariants (O-3/O-6/O-7/O-8/O-9/O-14/O-15/O-17), CS-1/CS-2 cross-scaffold env-var regression, M-3b claude_md-as-gotcha regression, O-10 phantom tree, CQ-2 self-inflicted gotchas, O-3 very-long bash, O-1 wall blowout, recipe-tree lint failure. PAUSE triggers (§2.2) = 10 soft gates. ACCEPT-WITH-FOLLOW-UP triggers (§2.3) = 8 follow-up classes. Rollback procedure (§4) = git revert of the C-0..C-15 range in reverse order + state cleanup + tagging + communication; target ≤ 2 hour wall. Re-attempt path (§4.5) maps each trigger to the failed principle + the revisit scope. §9 headline: "v35 passes if (C-1) ≤2 ∧ (C-2) ≤1 ∧ (C-9) ≥2 ∧ (M-3b) =0 ∧ (CS-1) exits 0; any regression revisits the core thesis."

**Success-criteria check (RUNBOOK §3 step 6 / README §3 step 6)**:

| Criterion | Status |
|---|---|
| Explicit recommendation with justification | ✅ [migration-proposal.md §6](06-migration/migration-proposal.md) — cleanroom, 6 numbered justification points; §2–§5 ground each decision criterion in step-3 evidence |
| Rollback criteria measurable (not "looks wrong") | ✅ [rollback-criteria.md §2.1](06-migration/rollback-criteria.md) — 10 ROLLBACK triggers all map to numeric bars in calibration-bars-v35.md; zero qualitative thresholds |
| v35 calibration bars (from §5) are the go/no-go triggers | ✅ [rollback-criteria.md §9 headline](06-migration/rollback-criteria.md) — maps headline bars to rollback triggers; §3 cross-category gate check extends to every `[gate]`-severity bar in calibration-bars-v35.md |
| Research-mode only — no code changes, no edits outside docs/zcprecipator2/06-migration/ | ✅ only new files: `docs/zcprecipator2/06-migration/{migration-proposal,rollout-sequence,rollback-criteria}.md` + RESUME.md update |
| Every recommendation has numeric/measurable criteria | ✅ rollout-sequence LoC deltas numbered per commit; rollback triggers are grep-counted; zero "looks good" assertions |

---

## Final research-phase summary

### Artifacts produced across all 6 steps (2026-04-20 baseline) + refinement

| Step | Directory | Files | Lines |
|---:|---|---:|---:|
| 1 | [01-flow/](01-flow/) | 17 | 2,494 |
| 2 | [02-knowledge/](02-knowledge/) | 5 | 1,125 |
| 3 | [03-architecture/](03-architecture/) | 5 | ~2,100 (editorial-review additions to atomic-layout + data-flow-showcase + data-flow-minimal + check-rewrite = ~300 lines added) |
| 4 | [04-verification/](04-verification/) | 44 (36 baseline + 8 editorial-review × {showcase, minimal} × {composed, simulation, diff, coverage}) | ~4,300 |
| 5 | [05-regression/](05-regression/) | 2 | ~1,250 (+row 15.1 + extended rows + §11a editorial-review surface) |
| 6 | [06-migration/](06-migration/) | 3 | ~1,100 (+C-7.5 commit description + T-11/T-12 triggers) |
| **Total** | | **77** | **~12,400** |

Plus: [RUNBOOK.md](RUNBOOK.md), [README.md](README.md), this [RESUME.md](RESUME.md), and the extractor under [scripts/](scripts/).

**Refinement delta**: 9 new files (8 verification + 0 net new in other directories; refinement edits are in-place updates to 8 existing files — atomic-layout.md, data-flow-showcase.md, data-flow-minimal.md, check-rewrite.md, rollout-sequence.md, rollback-criteria.md, defect-class-registry.md, calibration-bars-v35.md) + ~1,300 lines added.

### Defect classes covered (from step-5 registry)

**69 rows** in [defect-class-registry.md](05-regression/defect-class-registry.md) post-refinement (68 baseline + 1 new via refinement). Target was ≥30 per README §3 step 5 — exceeded ~2.3×. Distribution:
- v6–v19 lineage seeds: 11 rows
- v20–v34 exhaustive: 57 rows (v20 × 6, v21 × 6, v22 × 6, v23 × 4, v25 × 3, v26 × 2, v28 × 4, v29 × 4, v30 × 2, v31 × 8, v32 × 5, v33 × 7, v34 × 4)
- Refinement 2026-04-20: 1 row (15.1 classification-error-at-source — conceptual class observable retroactively in v28/v29/v34 that prior enforcement doesn't catch; closed by new editorial-review sub-agent role per refinement)

Principle-to-row coverage ([registry §3](05-regression/defect-class-registry.md)): every principle P1–P8 has ≥1 representative row; three rows carry defense-in-depth coverage (v33 phantom tree = P2+P6+P8, v34 manifest inconsistency = P5 primary + P2/P3 supporting + P7 via editorial-review refinement, v25 substep-bypass = P4+P6). **Refinement extends P7 coverage**: editorial-review institutionalizes P7 cold-read at runtime (not just pre-merge review) — rows 8.2, 8.3, 14.1, 14.4, 15.1 gain editorial-review as secondary/tertiary enforcement.

### Principles ratified (step 3)

**8 principles** per [principles.md](03-architecture/principles.md). Starting point was README §5's 7 stake-in-the-ground; step 3 pressure-tested and **added P8 (positive allow-list, not enumerated prohibition)** for v33's three invention classes (phantom tree, Unicode box-drawing, auto-export) where negative enumeration named the forbidden behavior and turned it into an attack menu. Cross-audit ([principles.md §9](03-architecture/principles.md)) confirms every v8.78→v8.104 defect class has ≥1 principle covering it; no orphans.

| # | Principle | Defect classes closed |
|---|---|---|
| P1 | Every content check has an author-runnable pre-attest form | v23 5-round spiral, v31 3-round deploy, v32 3-round deploy, v33 3-round deploy, v34 4-round deploy (Fix E refutation) |
| P2 | Transmitted briefs are leaf artifacts | v17 sshfs-write-as-exec, v32 Read-before-Edit dispatch compression, v33 phantom tree (partial), v33 Unicode box-drawing (partial), v33 diagnostic-probe cadence |
| P3 | Parallel sub-agents share a symbol-naming contract | v22 NATS URL-creds, v22 S3 endpoint, v30 worker SIGTERM, v31 apidev enableShutdownHooks, v34 DB_PASS/DB_PASSWORD, v29 Nest circular-import |
| P4 | Server workflow state IS the plan | v25 substep-bypass, v25 workflow-at-spawn, v34 TodoWrite 12 full-rewrites, v32 close step never completed, v33 auto-export |
| P5 | Fact routing is a two-way graph | v34 manifest-content inconsistency (DB_PASS), v28 33% genuine + 5 wrong-surface, v29 2/14 DISCARD override |
| P6 | Guidance is atomic; version anchors only in archive | recipe.md bloat, v33 phantom tree (partial), v33 Unicode (partial), v32 dispatch compression (partial) |
| P7 | Every brief passes cold-read + defect-coverage test | v32 per-codebase READMEs missing, v33 writer hallucinated paths, v28 writer kept DISCARD, v31 apidev enableShutdownHooks |
| P8 | Positive allow-lists, not enumerated prohibitions | v33 phantom tree, v33 Unicode box-drawing, v33 auto-export, v33 paraphrased env-folder names, v25 SUBAGENT_MISUSE rationalization |

### Migration candidate recommended (step 6)

**Candidate B — Cleanroom.** Structural change is not incrementally comparable; shadow-diff buys nothing step 4 hasn't already produced; parallel-run's dual-tree maintenance reintroduces the monolithic-mixing class P6 is closing; operational substrate is pristine and untouched, so rollback is bounded to the 15-commit rollout-sequence range. See [migration-proposal.md §6](06-migration/migration-proposal.md) for the full justification.

### v35 calibration summary (step 5)

**102 numeric bars** across 10 + 1 categories per [calibration-bars-v35.md](05-regression/calibration-bars-v35.md) (97 baseline + 5 added via refinement — editorial-review surface §11a: ER-1..ER-5):
- **Operational (§1)**: 21 bars — wall ≤ 90 min (showcase) / 60 min (minimal); 0 very-long bash; 0 MCP schema errors; 0 SUBAGENT_MISUSE; 0 git-lock; 0 zcp-side-exec; 0 phantom tree; 0 auto-export; 0 Unicode box-drawing; seed uses static key
- **Convergence (§2)**: 11 bars — **C-1 deploy rounds ≤ 2 (bar #0); C-2 finalize rounds ≤ 1 (bar #0)**; C-9 ≥ 2 downstream facts; C-11 ≤ 1 TodoWrite full-rewrite
- **Content-quality writer surface (§3)**: 12 bars — gotcha-origin ≥ 80% genuine; 0 self-inflicted; 0 folk-doctrine; 0 version anchors; CLAUDE.md ≥ 1200 bytes + ≥ 2 custom sections
- **Env-README Go-template surface (§4)**: 4 bars — 0 cross-tier fabrications; minContainers/mode/objectStorageSize claims match adjacent YAML
- **Env import.yaml comment surface (§5)**: 8 bars — two-axis minContainers teaching; comment ratio ≥ 30%; comment depth ≥ 35% reasoning markers; 0 cross-env refs
- **Manifest-consistency P5 surface (§6)**: 11 bars — all 6 (routed_to × surface) pairs; **M-3b `(routed_to=claude_md, published_gotcha)` = 0 is NEW v34 closure**
- **Cross-scaffold symbol contract P3 surface (§7)**: 9 bars — env-var consistency; NATS separate creds; S3 forcePathStyle; SIGTERM/enableShutdownHooks; .gitignore baseline; byte-identical SymbolContract JSON
- **Close review (§8)**: 9 bars — 0 CRIT shipped after close; WRONG ≤ v34's 3; silent-swallow clean; feature coverage complete
- **Dispatch + brief-composition integrity P2/P6/P8 surface (§9)**: 8 bars — 0 version anchors in atoms; 0 dispatcher vocabulary; 0 internal check names; 0 Go-source paths; 300-line atom cap; byte-identical MANDATORY; 0 orphan prohibitions
- **Tier-specific (§10)**: 8 showcase-only + 6 minimal-only
- **Hybrid/derived (§11)**: 5 cross-category

Headline **6** (updated per refinement 2026-04-20 — single most-important v35 measurements): C-1, C-2, C-9, M-3b, CS-1, **ER-1** (editorial-review CRIT count shipped after inline-fix = 0). See [rollback-criteria.md §9](06-migration/rollback-criteria.md). Refinement added §11a editorial-review surface with ER-1..ER-5 bars; ER-1 is the headline addition because it gates the classification-error-at-source class directly.

### Remaining open questions for the user

Deferred past step 6 — not blockers for the migration decision but warrant explicit user attention before implementation begins. **User resolved all six on 2026-04-20 — answers below in the `## Open questions — user-resolved (handoff)` section. The original question text is preserved here for audit trail.**

1. **SymbolContract placement** — [principles.md §12.1](03-architecture/principles.md) + [atomic-layout.md §4](03-architecture/atomic-layout.md) place it inside `plan.Research`. Alternative: separate computed artifact (`plan.SymbolContract` at the top level). Implementation-phase decision; both work.
2. **Expanded writer_manifest_honesty trigger** — [principles.md §12.2](03-architecture/principles.md) + [check-rewrite.md §12](03-architecture/check-rewrite.md) — runs at `deploy.readmes` complete (current trigger point) OR at `close.code-review` complete (post-writer). Either is cleanroom-compatible; step-4 writer-coverage doc suggests deploy.readmes as primary + close.code-review as secondary via [briefs/code-review/manifest-consumption.md](atom). User preference?
3. **TodoWrite structural refusal vs content-only discipline** — [principles.md §12.3](03-architecture/principles.md) — RESUME decision #3 is "check-off-only via principle atom." Should the Go layer actively reject TodoWrite full-rewrite payloads at step-entry boundaries (structural enforcement) or rely on content-only? Cleanroom lands with content-only; structural refusal is a subsequent opt-in patch.
4. **Minimal-tier writer dispatch Path A vs Path B** — [data-flow-minimal.md §5a](03-architecture/data-flow-minimal.md) — Path A (main-inline default, per current minimal flow) vs Path B (dispatched via `briefs/writer/*`). Step-4 coverage found Path A has a partial-coverage caveat on v28-debug-agent-writes-content class (simulation A1). Recommendation: ship Path A as default, revisit after a commissioned v35.5 minimal run confirms the caveat is within the enforcement model of pre-attest shims + manifest honesty + classification taxonomy.
5. **Commissioned v35.5 minimal run** — [RESUME decision #1](#open-decisions--user-resolved-this-session) + [data-flow-minimal.md §11](03-architecture/data-flow-minimal.md) 4 escalation triggers. Cleanroom rollout (per rollout-sequence.md) doesn't require a minimal run before v35 ships, but v35's decision matrix (rollback-criteria.md) applies to BOTH showcase and minimal. Suggestion: commission minimal after v35 showcase clears the gate, before v36 is scheduled.
6. **v35 model** — [rollback-criteria.md §8](06-migration/rollback-criteria.md): the calibration baseline assumes the same Opus model family that ran v34. If v35 runs on a later model, model-version documentation in the verdict is required; some regressions may have dual causes.

---

## Open questions — user-resolved (handoff)

Resolved 2026-04-20 after step-6 review. These govern the implementation-phase instance's decisions and unlock specific commits in the rollout sequence.

| Q# | Answer | Rationale | Unlocks |
|---|---|---|---|
| **Q1** — SymbolContract placement | **Top-level `plan.SymbolContract`** (computed artifact, not nested under `plan.Research`) | SymbolContract is *derived* from research inputs (framework → standard env var names, services → endpoint shapes). Nesting it under `plan.Research` conflates user input with system derivation. Top-level lets it be regenerated idempotently if Research changes, and makes its derived nature discoverable to every scaffold dispatch without chasing nested fields. | C-1 (adds SymbolContract plan field + derivation helper) |
| **Q2** — `writer_manifest_honesty` trigger point | **Both — primary at `deploy.readmes` complete, secondary at `close.code-review` complete** | Two distinct failure modes, both worth catching: (a) primary catches original writer output drift before any downstream propagation (v34 DB_PASS class fails here); (b) secondary catches post-review drift — when main agent inline-edits gotchas after code-review fires a CRIT, manifest drifts from published content again. Single trigger leaves a gap; both close the class. Check is runnable in <1s — belt-and-suspenders is cheap. | C-8 (expand `writer_manifest_honesty` to all routing dimensions with dual-trigger gating) |
| **Q3** — TodoWrite enforcement mode | **Content-only discipline (NOT structural refusal for cleanroom)** | Structural refusal isn't viable at the cleanroom Go layer: TodoWrite is Claude Code's built-in tool, not an MCP tool, so the Go layer can't intercept server-side — would require hooks-based mechanism outside zcp. Content-only = principle atom tells the agent "server state IS the plan; check-off only." Measure TodoWrite full-rewrite count in v35 calibration bars. If v35 shows > 0 full-rewrites at step-entry boundaries, that's the signal to build hook-based enforcement as a **follow-up patch** — not before. Don't speculatively build infrastructure. | No cleanroom commit; follow-up patch gated on v35 TodoWrite metric |
| **Q4** — Minimal-tier writer dispatch | **Path A (main-agent writes content inline) for v35, Path B (dispatched) gated on v35.5 evidence** | v28 "debugging-agent-writes-content" class produced 33% genuine gotchas on *showcase*. Minimal has structurally fewer debug rounds (1-2 features vs 6, 1-3 codebases vs 3) — context corruption is thinner. Step-4 coverage doc said Path A's caveat is *"within the enforcement model of pre-attest shims + manifest honesty + classification taxonomy"* — those mechanisms exist to catch the v28 class regardless of who authors. If v35.5 minimal shows gotcha-origin < 80%, that's evidence to patch in Path B for v36. Don't pay the +20-30% wall-time hit for speculative writer dispatch. | C-4 (atom files land with Path A stitcher branch default for minimal); C-5 (stitcher emits main-inline path for minimal, writer-dispatch path for showcase) |
| **Q5** — v35.5 minimal commission timing | **After v35 showcase clears all rollback-criteria gates** | Ordering discipline: v35 is the marquee test. If v35 regresses, revert and v35.5 never runs (testing a reverted architecture = wasted run). If v35 passes, v35.5 is a scoped validation against a proven architecture. Parallel or back-to-back muddles the signal — regression can't be cleanly attributed to architecture vs tier-shape variance. Serial, showcase first. | v35.5 commission scheduled after C-14 dry-run + v35 pass; not a code commit, operational step |
| **Q6** — v35 model version | **Lock v35 to `claude-opus-4-7[1m]` (same as v34)** | v34 baseline was on `claude-opus-4-7[1m]`. Any v35 regression would otherwise carry dual-cause ambiguity (architecture vs model). Locking produces clean variable isolation: only architecture changed between v34 and v35. Fallback if model is unavailable at v35 commission time: run on current Opus, **document the model version in the v35 verdict doc, and flag any regression as dual-cause suspect until v35-rerun confirms**. Do NOT run v35 on a newer model without explicit documentation of the model-variance risk. | v35 verdict doc content (C-14 dry-run infrastructure includes model-lock capability; v35 commission uses model-lock flag) |

**Implementation instance**: read this section during required reading. All six questions resolved; no further user input needed to enter C-0. Implementation commits reference these answers by Q number (e.g., C-1 implements Q1's top-level placement; C-8 implements Q2's dual-trigger).

---

## Research phase — DONE + REFINED

Steps 1–6 complete (2026-04-20) + research-refinement 2026-04-20 adding editorial-review sub-agent role. 77 artifacts, ~12,400 lines. Handoff to implementation phase: see [rollout-sequence.md](06-migration/rollout-sequence.md) for the **16-commit plan** (15 baseline + C-7.5 editorial-review role); [rollback-criteria.md](06-migration/rollback-criteria.md) for the v35 gate regimen (10 baseline triggers + T-11, T-12 editorial triggers); [migration-proposal.md](06-migration/migration-proposal.md) for the full cleanroom justification.

### Refinement 2026-04-20 summary

Triggered by a review session recognizing that [`docs/spec-content-surfaces.md`](../spec-content-surfaces.md) line 317-319 prescribes an editorial-review role the original research absorbed into writer self-review, collapsing author + judge. Content quality was identified as the primary commercial axis; the original research rated editorial excellence as "medium confidence — agent-variance-bound" with the floor at ≥80% gotcha-origin. The refinement adds an independent editorial-review sub-agent dispatch at close-phase.

**Scope**:
- 1 new role (`briefs/editorial-review/` with 10 atoms)
- 1 new substep (`close.editorial-review`; showcase gated; minimal ungated-discretionary default-on)
- 1 new commit (C-7.5 between C-7 and C-8)
- 7 new dispatch-runnable checks (ER-originated per [check-rewrite.md §16a](03-architecture/check-rewrite.md))
- 5 new calibration bars (§11a ER-1..ER-5)
- 2 new rollback triggers (T-11, T-12)
- 1 new defect-class registry row (15.1 classification-error-at-source)
- 8 new step-4 verification files (editorial-review × {showcase, minimal} × {composed, simulation, diff, coverage})
- 4 existing registry rows extended (8.2, 8.3, 14.1, 14.4 gain editorial-review as secondary/tertiary defense)

**Scope NOT changed**:
- 8 principles P1–P8 unchanged
- 15-commit cleanroom baseline structure preserved (C-7.5 inserts, does not renumber)
- 6 user decisions Q1–Q6 remain resolved
- Operational substrate untouched
- Step 1 / Step 2 artifacts unchanged

**Why editorial-review closes classification-error-at-source**: P5 expanded manifest honesty catches manifest↔content drift assuming classification is right. Editorial-review reclassifies independently — catches when the writer's classification itself was wrong (e.g., self-inflicted misclassified as platform-invariant). P5 + editorial-review together: full coverage of both manifest-consistency and classification-correctness. The spec explicitly prescribes this reviewer role (spec line 317-319).

**v35 implication**: 6 headline bars instead of 5 (adds ER-1); 16 commits instead of 15; 102 calibration bars instead of 97; 69 registry rows instead of 68. Implementation phase's starting artifact set includes the refinement deltas.

---

## Next action

1. **Pause for user review of step 6 artifacts.** Suggested spot-checks (ordered by load-bearing-ness):
   - **[migration-proposal.md §6 recommendation + justification](06-migration/migration-proposal.md)** — marquee check. Walk the 6 numbered justification points for cleanroom; the load-bearing claim is §6.2 point 1 — "the new architecture's value is structural, not incremental." If this framing is wrong (i.e. the delta is actually more incremental than claimed), parallel-run becomes viable. Cross-check against §1 (surface-delta quantification) and §2 (coexistence analysis) for evidence.
   - **[migration-proposal.md §3 shadow-diff cost](06-migration/migration-proposal.md)** — confirms that shadow-diff buys nothing step 4 hasn't already produced. The load-bearing claim is that byte-diff work already exists in the 36 step-4 files and convergence/SymbolContract/manifest-honesty aren't shadowable without agent execution. Cross-check against step-4 brief-*-diff.md files.
   - **[rollout-sequence.md C-0 through C-15](06-migration/rollout-sequence.md)** — 15-commit plan. Walk the ordering dependencies; confirm each commit's "breaks alone" section matches your mental model. Critical ordering: C-1 → C-3 → C-4 → C-5 cutover; C-5 → C-6 → C-7 → C-8 → C-10 payload shape flip; C-14 dry-run before any v35 commissioning; C-15 old-tree removal last.
   - **[rollout-sequence.md C-5 CUTOVER commit](06-migration/rollout-sequence.md)** — the single largest behavioral change. The risk mitigation is the step-4 composed briefs serving as golden files; C-14 adds the dry-run harness that diffs against them. Verify this mitigation is adequate for the cutover scope.
   - **[rollout-sequence.md C-10 breaking payload shape commit](06-migration/rollout-sequence.md)** — the one commit that is genuinely breaking. Verify the debug-log retention plan is workable; confirm ordering after all check commits landed.
   - **[rollback-criteria.md §2.1 ROLLBACK triggers](06-migration/rollback-criteria.md)** — 10 mechanically-measured gates. Confirm each trigger maps to an objective measurement (grep/jq/wc/session-log parse), not a judgment call. Pay special attention to T-1 (deploy rounds > 2) and T-2 (finalize rounds > 1) — these are the direct v34-refutation tests.
   - **[rollback-criteria.md §4 rollback procedure](06-migration/rollback-criteria.md)** — git revert in reverse order + state cleanup + tagging. Sanity-check: the C-15 delete of recipe.md + recipe_topic_registry.go is reverted correctly (`git revert` preserves history so the old files come back).
   - **[rollback-criteria.md §4.5 re-attempt path](06-migration/rollback-criteria.md)** — trigger → failed principle → revisit scope. Verify the mapping identifies the right design decision to revisit for each trigger class.
   - **Final summary table** in this RESUME.md §"Artifacts produced across all 6 steps" — 68 files, 11,119 lines across 6 step directories. Verify against `find docs/zcprecipator2 -type f -name '*.md' | wc -l` if you want independent confirmation.
   - **Remaining open questions** in §"Remaining open questions for the user" — 6 items deferred past step 6. Each is implementation-phase decision, not research blocker. User answers unlock specific rollout-sequence commits: Q1 → C-1; Q2 → C-8; Q3 → follow-up patch; Q4 → C-4 content split + C-5 stitcher branch; Q5 → v35.5 commission timing; Q6 → v35 verdict-doc content.

2. Research phase complete. Implementation phase begins after user review of step-6 artifacts + open-question resolution. Handoff artifacts for the implementation-phase instance: [`rollout-sequence.md`](06-migration/rollout-sequence.md) (commit plan), [`rollback-criteria.md`](06-migration/rollback-criteria.md) (v35 gates), [`migration-proposal.md`](06-migration/migration-proposal.md) (why cleanroom), plus all 68 prior artifacts as design context.

3. If user resolves all 6 open questions, the implementation-phase instance has zero ambiguity entering C-0. Otherwise, open questions land as `TODO(question-N)` comments in the relevant commit's code to be resolved during that commit's review.

---

## [Archived] Prior "next action" for step 5 → step 6 handoff

Kept for audit trail.

1. **Pause for user review of step 5 artifacts** (parallel with step 4, which runs in a separate instance). Suggested spot-checks for step 5 (ordered by load-bearing-ness):
   - **[defect-class-registry.md §1.5 (v23) through §1.14 (v34)](05-regression/defect-class-registry.md)** — marquee check. Walk the 57 registry rows for v20–v34 against `recipe-version-log.md` per-version entries. Pay special attention to: row 4.4 `v22-post-writer-iteration-leaks-to-main` and row 5.1 `v23-content-fix-loop-convergence-spiral` (both marked "**P1 supersedes**" as new_enforcement since v34 empirically refuted the metadata-on-failure axis); row 14.1 `v34-manifest-content-inconsistency` (`(carry)` + new P5 multi-dimension enforcement); row 14.2 `v34-cross-scaffold-env-var-coordination` (`(carry)` + new P3 SymbolContract); row 14.3 `v34-convergence-architecture-refuted` (`(carry)` — the thesis-critical row).
   - **[defect-class-registry.md §2 coverage audit](05-regression/defect-class-registry.md)** — confirm the row count per origin run (§2.1), the test-scenario Go-independence property (§2.2), the numeric-bar property (§2.3), and the exclusion rationale for v6–v19 + substrate bugs (§2.4).
   - **[defect-class-registry.md §3 principle-to-row coverage](05-regression/defect-class-registry.md)** — verify every principle P1–P8 has representative rows; spot-check any principle that feels under-represented.
   - **[calibration-bars-v35.md §13 headline bars](05-regression/calibration-bars-v35.md)** — the 5 single-most-important v35 measurements. Sanity-check: if any one regresses on v35, the rewrite's core thesis needs revisiting; confirm this is the right list.
   - **[calibration-bars-v35.md §2 convergence bars](05-regression/calibration-bars-v35.md)** — C-1 deploy ≤ 2 rounds and C-2 finalize ≤ 1 round are marked `[gate]` bar #0. These are the direct v34-refutation targets.
   - **[calibration-bars-v35.md §6 manifest-consistency bars](05-regression/calibration-bars-v35.md)** — 6 routing × surface pairs (M-3a through M-3f); confirm M-3b `(routed_to=claude_md, published_gotcha)` is correctly called out as the NEW v34 bar.
   - **[calibration-bars-v35.md §12.1 README §10 coverage](05-regression/calibration-bars-v35.md)** — reconciliation table showing every README success criterion maps to ≥1 bar. Spot for gaps.
   - **[calibration-bars-v35.md §10 tier-specific bars](05-regression/calibration-bars-v35.md)** — minimal-tier bars (T-M-1 through T-M-6) are preliminary per RESUME decision #1 (reconstruct-from-spec); they'll refine after any commissioned minimal run. Verify the preliminary floors are plausible.
   - **[calibration-bars-v35.md §14 bars explicitly NOT gated](05-regression/calibration-bars-v35.md)** — v22 cross-codebase architecture narrative is `[advisory]`-only post-v24 rollback; sanity-check this doesn't silently become a gate elsewhere.

2. Steps 4 + 5 are both complete. Ready for step 6 (migration path) after user review.

3. Known step-6 constraints carried forward: migration proposal compares parallel-run vs cleanroom based on concrete delta evidence from step 3 (Go-code surface change, old/new coexistence feasibility, rollback cost); rollback-criteria measured against §13 headline bars from step 5 calibration-bars; step-4 findings on minimal-inline writer path caveat + stitcher adaptation concerns + deferred-check deletion candidates (all 6 keep per conservative RESUME decision #5) feed migration planning.

---

## [Archived] Prior "next action" for step 3 → step 4/5 handoff

Kept for audit trail.

1. **Pause for user review of step 3 artifacts.** Suggested spot-checks (ordered by load-bearing-ness):
   - **[principles.md §9 cross-audit table](03-architecture/principles.md)** — marquee check. For any v8.78→v8.104 fix in `recipe-version-log.md` (browse v21–v34 entries for the "v8.NN fixes" block at the end of each), verify the closed class appears in the audit table with a principle covering it. Pay special attention to v34 DB_PASS/DB_PASSWORD (P3 + P5 coverage), v33 phantom tree (P2 + P8 double coverage), v34 Fix E refutation (P1 as the response).
   - **[principles.md P1 statement + §Replaces](03-architecture/principles.md)** — verify the claim that v8.96 Theme A (ReadSurface/HowToFix/CoupledWith) + v8.104 Fix E (PerturbsChecks) both failed to collapse rounds (v31 3 rounds → v33 3 rounds → v34 4 rounds deploy) against `recipe-version-log.md §v34` "Fix E refuted by data." This is the single most load-bearing claim in the principle set.
   - **[atomic-layout.md §3 block→atom mapping](03-architecture/atomic-layout.md)** — for 3 representative current blocks (suggested: `scaffold-subagent-brief` L790–1125, `content-authoring-brief` L2390–2736, `code-review-subagent` L3050–3158), walk from the current line range to the new atom list. Verify no current block content is unmapped.
   - **[atomic-layout.md §4 SymbolContract schema + FixRecurrenceRules](03-architecture/atomic-layout.md)** — verify the 12 seeded FixRecurrenceRules correspond to specific v22/v29/v30/v31/v34 defects. Sanity-check: does every rule have a `PreAttestCmd` that could plausibly run inside an SSH session in seconds?
   - **[check-rewrite.md §12 writer_manifest_honesty expansion (P5)](03-architecture/check-rewrite.md)** — the expansion from `(discarded, published_gotcha)` to every `(routed_to, published-surface)` pair is what closes the v34 DB_PASS manifest-inconsistency class. Verify this against [workflow_checks_content_manifest.go:156-185](../../internal/tools/workflow_checks_content_manifest.go) confirming the current scope is single-dimension.
   - **[check-rewrite.md §15 deletion audit (RESUME decision #5)](03-architecture/check-rewrite.md)** — 1 definite deletion + 6 deferred candidates. Verify the deleted check's protection is upstreamed.
   - **[check-rewrite.md §17 summary matrix](03-architecture/check-rewrite.md)** — sanity reconciliation: 56 keep + 16 rewrite + 1 delete + 5 new = 78 total. Compare against the 12-file count (~73 current) — delta +5 comes entirely from the new checks per P3/P5/P6/P8.
   - **[data-flow-showcase.md §4d deploy.readmes composition](03-architecture/data-flow-showcase.md)** — verify the writer dispatch prompt composition steps (fresh-context premise + canonical-output-tree + content-surface-contracts + classification-taxonomy + routing-matrix + citation-map + manifest-contract + self-review-per-surface + completion-shape) are comprehensive. Cross-check against `flow-showcase-v34-dispatches/write-per-codebase-readmes-claude-md.md` (actual v34 dispatch) for any missing piece.
   - **[data-flow-minimal.md §11 escalation triggers](03-architecture/data-flow-minimal.md)** — four concrete questions that would require a commissioned minimal run for step 4. Acknowledge or refute before step 4 begins.
   - **[data-flow-showcase.md §9 failure payload shape](03-architecture/data-flow-showcase.md)** — the ~3× payload reduction (7.5 KB → 2.5 KB per failure round) is a side-effect; the primary claim is "author runs preAttestCmd locally, gate becomes confirmation." Sanity-check: every kept/rewritten check in check-rewrite.md has an actual `preAttestCmd` column populated.

2. On go-ahead, start steps 4 and 5 in parallel (open decision #6). Step 4 (context verification) composes every sub-agent brief the new architecture would transmit and cold-read simulates against v34 captured dispatches; step 5 (regression fixture) builds the defect-class registry from `recipe-version-log.md` with test scenarios + v35 calibration bars. Step 6 sequential after both.

3. Known step-4+5 constraints carried forward:
   - Step 4 outputs go to `docs/zcprecipator2/04-verification/`; artifacts are 4-per-(role × tier) — composed brief + cold-read simulation + diff-against-v34 + defect-class coverage table.
   - Step 5 outputs go to `docs/zcprecipator2/05-regression/`; calibration bars are numeric / grep-verifiable (no qualitative thresholds).
   - Tier coverage still non-optional: minimal AND showcase per step.

## Known constraints entering step 4 (carried forward from earlier steps)

- Every composed brief must read cleanly cold — no contradictions, no unresolved ambiguities.
- Every removed-vs-v34 line has a disposition (scar / noise / dispatcher / load-bearing-moved-where).
- Every v20–v34 closed defect class has a prevention mechanism cited in the appropriate coverage doc (composed brief OR new check suite OR runtime injection).
- RESUME decisions #1 (reconstruct-from-spec for minimal) + #5 (conservative check deletion) carry forward to step 4 verification.
