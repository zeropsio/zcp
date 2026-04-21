# runs/v36 — <slug> against v8.108.0 (PLACEHOLDER — fill in post-run)

**This is a skeleton created alongside [`../HANDOFF-to-I7-v36-analysis.md`](../HANDOFF-to-I7-v36-analysis.md) so the fresh analysis instance has somewhere to land artifacts.** The analysis instance rewrites this file following the schema below after the v36 run closes.

---

## Slots to fill

Populate at the start of analysis. If any slot is `<unknown>`, stop and ask the user — do not proceed on guesses.

```
RUN_REF:            v36
SESSION_ID:         <fill-in>
CLOSE_DATE:         <fill-in UTC>
TIER:               <showcase | minimal>
SLUG:               <fill-in>
RUN_OUTCOME:        <close-complete | force-exported | session-abandoned>
DELIVERABLE_TREE:   <fill-in>
SESSIONS_LOGS:      <DELIVERABLE_TREE>/SESSIONS_LOGS/
MAIN_JSONL:         <SESSIONS_LOGS>/main-session.jsonl
TIMELINE_MD:        <DELIVERABLE_TREE>/TIMELINE.md
COMMISSIONED_BY:    user
AGENT_MODEL:        <fill-in>
```

---

## Expected schema (rewrite this section after analysis)

Following the v35 README shape at [`../v35/README.md`](../v35/README.md):

### TL;DR (≤12 lines)

Headline outcome + whether fix stack closed F-1..F-6 + where the run reached (deploy / finalize / close) + the verdict class.

### Defect closure summary

| # | Defect | v35 behavior | v36 observation | Closed on v36? |
|---|---|---|---|---|
| F-1 | Dispatch brief overflow | 71 KB response, spillover, broken writer dispatch | *fill-in* | *PASS / REGRESSED-CLOSURE / NEW-CLASS / UNREACHED* |
| F-2 | Check Detail Go notation | 11× "FactRecord.Title" detail, agent tried 14 envelope variants | *fill-in* | *fill-in* |
| F-3 | Iterate fake-pass | 12 substeps walked in 84s with zero tool calls | *fill-in* | *fill-in* |
| F-4 | Skip on mandatory step | 1× attempted; engine refused correctly | *fill-in* | *fill-in* |
| F-5 | Unknown / zero-byte guidance | 3× hallucinated topics (2 errors + 1 zero-byte) | *fill-in* | *fill-in* |
| F-6 | Knowledge engine misses manifest | `choose-queue` top hit for manifest-schema query | *fill-in* | *fill-in* |

### Files

| File | What it is |
|---|---|
| `analysis.md` | Narrative post-mortem + per-defect verification sections + appendices A/B. |
| `verdict.md` | Decision (PROCEED / ACCEPT-WITH-FOLLOW-UP / PAUSE / ROLLBACK-Cx / ROLLBACK) + rationale. |
| `calibration-bars.md` | Derivative of v35 sheet with v36 observed values + new bars if any class surfaced. |
| `rollback-criteria.md` | Derivative of v35 sheet with T-1/T-8/T-9 tightenings preserved + new triggers if needed. |
| `flow-main.md` | Main-agent trace (extracted via `scripts/extract_flow.py`). |
| `sub-*.md` | Per-subagent traces. |
| `flow-dispatches/` | Verbatim dispatch prompts per sub-agent dispatched via Agent tool. |
| `role_map.json` | Subagent-ID prefix → role slug mapping (populated before trace extraction). |

### Where else to look

- Fix-stack handoff that drove this run's commissioning: [`../HANDOFF-to-I6.md`](../HANDOFF-to-I6.md).
- Analysis handoff for this run: [`../HANDOFF-to-I7-v36-analysis.md`](../HANDOFF-to-I7-v36-analysis.md).
- Defect registry rows 16.1–16.6 (v35 defects): [`../../05-regression/defect-class-registry.md`](../../05-regression/defect-class-registry.md).
- Raw source: `<DELIVERABLE_TREE>` — session JSONL + TIMELINE.md + exported deliverable files.

### What v36 does or doesn't tell us

*fill-in after analysis based on how far the run reached.*

---

## Next — decision by verdict class

Rewrite this section based on the verdict:
- **PROCEED**: v37 commission as second-confirmation; C-15 + v35.5 minimal unblock.
- **ACCEPT-WITH-FOLLOW-UP**: targeted chore patch + v37 commission.
- **PAUSE**: new fix stack (HANDOFF-to-I8) before v37.
- **ROLLBACK-Cx**: revert offending Cx-commit + v8.108.1 patch tag + v37 with reverted state.
- **ROLLBACK**: revert rollout sequence per `runs/v35/rollback-criteria.md §4` + full post-mortem before any re-commission.
