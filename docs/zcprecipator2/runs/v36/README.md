# runs/v36 — nestjs-showcase against v8.108.0 + Cx-PLAN-NIL hotfix (v8.108.1)

**Date**: 2026-04-21
**Tier**: showcase
**Slug**: nestjs-showcase
**Session ID** (workflow): `7743c6d8c8a912fd`
**Session ID** (Claude UUID): `8271b0d3-57d1-4dac-a17c-a36181150071`
**Verdict**: **ACCEPT-WITH-FOLLOW-UP** — fix stack closes F-1..F-6; close step unreached (agent force-exported at finalize-complete). See [`verdict.md`](verdict.md).

---

## Slots

```
RUN_REF:            v36
SESSION_ID:         7743c6d8c8a912fd  (workflow) / 8271b0d3-57d1-4dac-a17c-a36181150071 (Claude)
CLOSE_DATE:         2026-04-21 16:04 UTC (session ended; close-step never called)
TIER:               showcase
SLUG:               nestjs-showcase
RUN_OUTCOME:        finalize-complete — close step never called, exported via zcp sync recipe export without session gate
DELIVERABLE_TREE:   /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v36/
SESSIONS_LOGS:      <DELIVERABLE_TREE>/SESSIONS_LOGS/
MAIN_JSONL:         <SESSIONS_LOGS>/main-session.jsonl  (625 JSONL lines, 2.36 MB, 208 tool calls)
TIMELINE_MD:        <DELIVERABLE_TREE>/TIMELINE.md
COMMISSIONED_BY:    user
AGENT_MODEL:        claude-opus-4-7 (from session JSONL model field)
```

**Note on tag**: commissioned against `v8.108.0`; the live binary serving the run was the hotfixed `v8.108.1` (Cx-PLAN-NIL-GUIDANCE landed at 14:01 UTC, run started 14:14 UTC). Research-step tier-only guidance lookups (`recipe-types`, `showcase-service-keys`) resolved correctly, confirming the patch was in effect.

---

## TL;DR

v36 is the **first showcase run to reach finalize-complete** since the zcprecipator2 architecture rolled out. All five F-1..F-6 defects from v35 are closed or UNREACHED cleanly under live conditions:

- **F-1** (brief overflow): envelope fired at readmes substep; 15 `dispatch-brief-atom` retrievals, max 11.5 KB per response, all under 32 KB. Writer dispatch carried the stitched brief correctly.
- **F-2** (check Detail Go notation): zero Go struct.field patterns in any `checkResult.checks[i].detail` string across the session. Atom content still names both `fact_title` and `FactRecord.Title` in prose — that's pedagogy, not regression.
- **F-3** (iterate fake-pass): no `action=iterate` called. Guard exists but was never triggered.
- **F-4** (skip on mandatory): 1× `action=skip step=deploy substep=browser-walk` at 15:09:38 — engine refused correctly ("deploy is mandatory and cannot be skipped"). Agent pivoted to fact-recording + fallback curl evidence. This is F-4 behaving as specified.
- **F-5** (unknown topic): zero unknown-topic responses. Zero "does not apply to your recipe shape" responses. The Cx-PLAN-NIL hotfix (v8.108.1) worked — research-step tier-only topics resolved against the synthetic plan.
- **F-6** (knowledge misses manifest): agent did not issue any `zerops_knowledge` query for manifest-contract atoms; instead retrieved them via the `dispatch-brief-atom` envelope. Synonym index was unused — UNREACHED-no-query.

Close step never ran. Editorial-review and code-review sub-agents were not dispatched. The agent completed finalize (both attempts clean — one fix round for version-anchor prose) and then exported via `zcp sync recipe export --session-unset`, bypassing the close-step gate. §7a editorial-review and §8 close-browser-walk remain unmeasurable.

---

## Defect closure summary

| # | Defect | v35 behavior | v36 observation | Closed on v36? |
|---|---|---|---|---|
| F-1 | Dispatch brief overflow | 71 KB response, spillover file, 3 writer dispatches, broken wire contract | envelope emitted 15 atomIds; 15 dispatches, max 11 532 B, all <32 KB; 1 writer + 1 fix pass (vs 3+) | **PASS** |
| F-2 | Check Detail Go notation | 11× `FactRecord.Title` in check detail, 14 envelope-variant retries | 0× Go struct.field in any check detail; writer brief prose names both `fact_title` + `FactRecord.Title` for author reference — correct pedagogy | **PASS** |
| F-3 | Iterate fake-pass | 12 substeps walked in 84 s with zero tool evidence | no `action=iterate` calls. Guard exists but was never triggered | **UNREACHED (fix-shipped — build-time test stands in)** |
| F-4 | Skip on mandatory step | not exercised on v35 (F-4 is telemetry; v35 never tried) | 1× `skip=deploy browser-walk` at 15:09:38 → engine refused → agent pivot to fact-record + curl evidence; clean recovery | **PASS (gate held; recovery worked)** |
| F-5 | Unknown / zero-byte guidance | 3× hallucinated topics (2 errors + 1 zero-byte) | 0× unknown topics; 0× "does not apply"; Cx-PLAN-NIL hotfix let research-step tier-only lookups resolve | **PASS** |
| F-6 | Knowledge misses manifest | `choose-queue` top hit for manifest-schema query — agent never recovered | agent did not issue any `zerops_knowledge` query for manifest terms; envelope-delivered atoms satisfied need | **UNREACHED-no-query (fix-shipped — synonym index not exercised at runtime)** |

Defect grades use the HANDOFF-to-I7 taxonomy: PASS (closed under triggering condition), REGRESSED-CLOSURE (closure mechanism failed), NEW-CLASS (new defect surfaced), UNREACHED (triggering condition didn't occur).

---

## New defects surfaced

| # | Class | Evidence | Severity | Registry |
|---|---|---|---|---|
| F-7 | Predicate-gated topic returns misleading "does not apply" message when `state.Recipe.Plan` is nil at research step (tier-only predicates run against nil and return false) | v36-attempt-1 (session `43814d9c5e09e85d`) before hotfix: 4× "Topic X does not apply to your recipe shape" on showcase-gated topics at research step | **closed pre-run** by Cx-PLAN-NIL-GUIDANCE (commit `c512757`, tag v8.108.1) | add row 16.7 |
| F-8 | Agent can bypass close-step gate by exporting via `zcp sync recipe export` without `--session` flag (CLI note: "skipping close-step gate") | row 204: `zcp sync recipe export "/var/www/..." --app-dir ... --include-timeline` (no --session) → "note: no session context ... skipping close-step gate." | **signal** — gate is advisory rather than enforced; editorial-review + code-review + close-browser-walk can be skipped by agent election | add row 16.8 |

See [`analysis.md §F-7`](analysis.md#f-7-predicate-gated-topic-on-nil-plan-closed-pre-run-via-cx-plan-nil-guidance) and [`analysis.md §F-8`](analysis.md#f-8-close-step-gate-bypass-via-sessionless-export) for evidence chains.

---

## Files in this folder

| File | What it is |
|---|---|
| [`analysis.md`](analysis.md) | Narrative post-mortem with per-defect verification, secondary observations S-1..S-5, calibration coverage gaps, appendices. |
| [`verdict.md`](verdict.md) | Decision doc: ACCEPT-WITH-FOLLOW-UP + rationale + halted/not-halted lists + audit trail. |
| [`calibration-bars.md`](calibration-bars.md) | v35 sheet with v36 observed values. Sections §3–§6 + §9 B-9..B-14 now measurable. §7 + §8 + §11a editorial-review remain unmeasurable (close unreached). |
| [`rollback-criteria.md`](rollback-criteria.md) | T-1..T-12 + T-13 (new: Cx-commit runtime regression) evaluated against v36. |
| [`flow-showcase-v36-main.md`](flow-showcase-v36-main.md) | Main-agent trace (208 tool calls, 1:49:25 wall). |
| `flow-showcase-v36-sub-*.md` | Per-subagent traces: scaffold-{apidev,appdev,workerdev}, feature, writer-1, writer-2-fix. |
| [`flow-showcase-v36-dispatches/`](flow-showcase-v36-dispatches/) | Verbatim dispatch prompts for each Agent-tool dispatch (6 total). |
| [`role_map.json`](role_map.json) | Subagent-ID prefix → role slug mapping. |

---

## Where else to look

- [`../HANDOFF-to-I6.md`](../HANDOFF-to-I6.md) — fix-stack spec (Cx-BRIEF-OVERFLOW ... Cx-KNOWLEDGE-INDEX-MANIFEST), delivered in v8.108.0.
- [`../HANDOFF-to-I7-v36-analysis.md`](../HANDOFF-to-I7-v36-analysis.md) — analysis handoff this verdict was authored against.
- [`../implementation-notes.md`](../implementation-notes.md) §Cx-CHECK-WIRE-NOTATION..§Cx-KNOWLEDGE-INDEX-MANIFEST + §Cx-PLAN-NIL-GUIDANCE.
- [`../../05-regression/defect-class-registry.md §16.1–16.8`](../../05-regression/defect-class-registry.md).
- Raw source: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v36/` — deliverable tree + SESSIONS_LOGS + TIMELINE.md + environments/.
- Regenerate traces: `python3 docs/zcprecipator2/scripts/extract_flow.py /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v36 --tier showcase --ref v36 --role-map docs/zcprecipator2/runs/v36/role_map.json --out-dir docs/zcprecipator2/runs/v36`.

---

## What v36 does tell us

- The v8.108.0 Cx-stack works. F-1..F-6 all PASS / UNREACHED-fix-shipped. **The architecture earns its keep** — the stack's design holds under the first live run that actually exercises it past deploy.
- Writer needs 1 fix pass on showcase tier, down from 3 on v35. Fix pass handled intro length, marker syntax (`#ZEROPS_EXTRACT_END#`), comment-ratio gate, and SIGTERM block — all cosmetic, not structural.
- Finalize's reclassification + version-anchor prose check caught two non-obvious issues (env 4 `minContainers:1` vs :2 comment mismatch, `nats v2` / `AWS SDK v3` inline version anchors). Two edits + one re-finalize. Not a defect class.

## What v36 does NOT tell us

- **Editorial-review efficacy** — not exercised. v34 remains the only data point.
- **Code-review agent behavior post-Cx** — not exercised.
- **Close-browser-walk** — not exercised.
- **`writer_manifest_honesty` all 6 dimensions** — reached finalize which runs honesty checks, but close's cross-surface honesty re-check didn't run.
- **C-11 `NextSteps=[]` on close completion** — not exercised.
- **§16a dispatch-runnable contract for editorial-review** — not exercised.
- **Minimal-tier Path B (main-inline writer)** — v35.5 minimal still pending. Showcase reaching finalize is independent evidence that minimal Path B is unblocked but not proof it works.
