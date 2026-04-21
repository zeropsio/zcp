# runs/v35 — nestjs-showcase against v8.105.0

**Date**: 2026-04-21
**Tier**: showcase
**Slug**: nestjs-showcase
**Session ID**: `8324884b199361d9`
**Verdict**: **PAUSE + engine-level defects identified** — not ROLLBACK. See [`verdict.md`](verdict.md).

---

## TL;DR

v35 deployed successfully (5/5 features green on dev + stage, stack healthy) but **never reached finalize**. Deploy-step full-check gate stuck on `writer_manifest_completeness` through 11 rounds + 3 writer sub-agents + 1 `iterate` cycle + 1 rejected `skip` attempt. Force-exported.

Six engine-level defects identified, all **pre-rollout** (sit in runtime layer C-7e..C-14 did not touch):

| # | Defect | Blocks run? |
|---|---|---|
| F-1 | Readmes-substep dispatch brief overflows MCP tool-response token cap (71 KB → spillover file) | **Primary cause.** Main agent loses wire contract, paraphrases manifest shape wrongly. |
| F-2 | Check `Detail` strings use Go struct-field notation (`FactRecord.Title`) instead of JSON-key notation (`fact_title`) | Compound with F-1 — main agent tries envelope variants instead of correct JSON key. |
| F-3 | `action=iterate` allows fake-passing all substeps on next iteration (no evidence requirement) | Engine correctness hole — wastes loop depth, erodes step-graph invariant. |
| F-4 | Main agent attempts `action=skip` on mandatory step | Retry-exhaustion telemetry — symptom of F-1/F-2 upstream. |
| F-5 | Main agent requests guidance topics that don't exist; some return zero-byte silently | Hallucinated topic IDs (`dual-runtime-consumption` etc.). No valid-topic list surfaced. |
| F-6 | `zerops_knowledge` misses `manifest-contract` atom under obvious keyword queries | One escape hatch from F-1/F-2 dead-end also fails. |

---

## Files in this folder

| File | What it is |
|---|---|
| [`analysis.md`](analysis.md) | Narrative post-mortem. Six fundamentals with evidence pointers. Appendices A + B list critical timestamps + sub-agent wall times. |
| [`verdict.md`](verdict.md) | Decision doc. Why PAUSE not ROLLBACK; T-1 / T-8 / T-9 measurement tightenings; new bars B-9..B-14 landed in `calibration-bars.md`. |
| [`calibration-bars.md`](calibration-bars.md) | Snapshot of the 108 bars v35 was measured against (97 original + 6 new runtime-integrity + the C-1 / C-2 tightening). |
| [`rollback-criteria.md`](rollback-criteria.md) | Snapshot of the rollback T-triggers v35 was arbitrated against, with T-1/T-8/T-9 inline tightening. |
| [`flow-main.md`](flow-main.md) | Main-agent session trace (192 tool calls, 2:55:11 wall). |
| `sub-*.md` | Per-subagent traces (7 subagents: 3 scaffolders, 1 feature, 3 writers). |
| [`flow-dispatches/`](flow-dispatches/) | Verbatim dispatch prompts for each sub-agent. Critical for F-1 evidence: see [`write-3-per-codebase-readmes.md`](flow-dispatches/write-3-per-codebase-readmes.md) — main agent tells writer to excavate wire contract from a spillover file. |
| [`role_map.json`](role_map.json) | Maps subagent-ID prefixes to role slugs for `extract_flow.py` re-runs. |

---

## Where else to look

- [`../../HANDOFF-to-I6.md`](../../HANDOFF-to-I6.md) — fix-stack handoff for a fresh instance; five `Cx-` commits to close F-1..F-6.
- [`../../05-regression/defect-class-registry.md §16.1–16.6`](../../05-regression/defect-class-registry.md) — structured defect rows with `test_scenario` + `calibration_bar` per defect.
- Raw source: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v35/` — deliverable tree + session JSONL + `TIMELINE.md` (user-authored post-run narrative).
- Regenerate traces: `python3 docs/zcprecipator2/scripts/extract_flow.py /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v35 --tier showcase --ref v35 --role-map docs/zcprecipator2/runs/v35/role_map.json --out-dir <path>`.

---

## What v35 does *not* tell us

- **Whether the zcprecipator2 rewrite (C-7e..C-14) works.** The run didn't get far enough. Bars §3–§8, §11a editorial-review — all unmeasurable on v35. v36 (post-fix-stack) is the actual verdict.
- **Editorial-review efficacy.** Close never ran. v34 remains the only data point for that role.
- **Minimal-tier Path B behavior.** Not exercised.
