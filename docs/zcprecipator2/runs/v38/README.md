# runs/v38 — nestjs-showcase against v8.110.0 (post-v37 Cx stack)

**Status**: **PRE-COMMISSION PLACEHOLDER**. This folder is seeded by [`../../HANDOFF-to-I9-v38-prep.md`](../../HANDOFF-to-I9-v38-prep.md). It will be filled in after v38 runs.

**Target tag**: `v8.110.0` — built from the [v38-fix-stack](../../plans/v38-fix-stack.md) commits (8 Cx-commits closing F-17 / F-21 / F-22 / F-23 / F-24 + harness-v2).

**Predecessor**: [`v37/`](../v37/) — verdict PAUSE, root cause F-17 (main-agent paraphrase of atom envelope), 3 defect classes regressed despite source-HEAD Cx stack.

---

## Slots to fill post-commission

```
RUN_REF:            v38
TAG_UNDER_TEST:     v8.110.0
V38_COMMISSION_DATE:  <unfilled>
SESSION_ID:         <unfilled — workflow>
CLAUDE_UUID:        <unfilled>
CLOSE_DATE:         <unfilled>
TIER:               showcase
SLUG:               nestjs-showcase
RUN_OUTCOME:        <unfilled>
DELIVERABLE_TREE:   /Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/
SESSIONS_LOGS:      <DELIVERABLE_TREE>/SESSIONS_LOGS/
MAIN_JSONL:         <SESSIONS_LOGS>/main-session.jsonl
TIMELINE_MD:        <DELIVERABLE_TREE>/TIMELINE.md
COMMISSIONED_BY:    <unfilled>
AGENT_MODEL:        <unfilled>
```

---

## What v38 is confirming

Per [HANDOFF-to-I9 §5](../../HANDOFF-to-I9-v38-prep.md), v38 is the first run under the **engine-built subagent brief** architecture. Specifically:

1. **F-17 closed?** — main agent can no longer paraphrase atoms. It must call `zerops_workflow action=build-subagent-brief role=<role>` and pass the returned prompt verbatim to `Task()`. Engine-side guard refuses `Task` dispatches that don't match the last-built brief hash. Evidence will be: writer dispatch prompt byte-for-byte matches `BuildWriterBrief(plan)` Go-source output.
2. **F-13 reach-through closed?** — writer dispatch prompt contains zero mentions of `INTEGRATION-GUIDE.md` or `GOTCHAS.md` (atoms already clean at HEAD; fix is making sure atom clean-up reaches runtime).
3. **F-9 reach-through closed?** — writer dispatch prompt contains zero slug-named env folders (`ai-agent`, `remote-dev`, etc.). Writer doesn't author env READMEs at all under the v38 atom-scope reduction.
4. **F-23 closed?** — `ZCP_CONTENT_MANIFEST.json` appears in the deliverable tree at the recipe output root, overlayed from the mount by finalize.
5. **F-21 closed or under-threshold?** — finalize envComment factuality failures ≤ 1 check (not the 15-check cycle-6 regression on v37).
6. **F-22 closed?** — `bootstrap-seed-v1` style execOnce keys don't trigger `no_version_anchors_in_published_content`.
7. **F-24 closed?** — zero browser-timeout cascades. `RecoverFork` reaps Chrome processes (not just the daemon pattern). Close-browser-walk completes without user intervention. At most 1 `forceReset=true` retry per walk.
8. **Harness v2 sharpness** — B-15 catches `environments*` siblings, B-21 ignores post-close sessionless exports, B-23 recognises dispatch description "Author recipe READMEs…" as writer.

---

## Required folder contents (will be filled at analysis time)

Mirror [v37's](../v37/) shape, per [`../README.md`](../README.md) folder contract:

| File | Required | Source |
|---|---|---|
| `README.md` | ✅ seeded, this file | updated at analysis time |
| `machine-report.json` | produced by `zcp analyze recipe-run` | harness |
| `verification-checklist.md` | produced by `zcp analyze generate-checklist` + analyst fill | harness + analyst |
| `verdict.md` | PROCEED / ACCEPT-WITH-FOLLOW-UP / PAUSE / ROLLBACK-Cx | analyst |
| `role_map.json` | maps subagent-id prefix → role | hand-written |
| `flow-showcase-v38-main.md` | main agent trace | `extract_flow.py` |
| `flow-showcase-v38-sub-*.md` | per-subagent traces | `extract_flow.py` |
| `flow-showcase-v38-dispatches/*.md` | verbatim dispatch prompts | `extract_flow.py` |
| `analysis.md` (optional) | narrative post-mortem if v37-style depth is needed | hand-written |
| `CORRECTIONS.md` (conditional) | only if a first-pass verdict misses defects and is revised | hand-written |

---

## Entry points for the analyst

When v38 artifacts land:

1. Read [`../../HANDOFF-to-I9-v38-prep.md`](../../HANDOFF-to-I9-v38-prep.md) §6 (analysis discipline rules — inherited from I8).
2. Read [`../v37/verdict.md`](../v37/verdict.md) to understand what was broken and what v38 is proving.
3. Run harness — `zcp analyze recipe-run` against the deliverable + `generate-checklist` against the report. Commit the two files immediately.
4. Fill the checklist row-by-row with Read-receipts.
5. Diff every dispatch prompt against the engine-built brief (F-17 verification is the headline).
6. Write verdict with SHA front matter + citations.

**Do not** start the analysis before v38 has been commissioned against v8.110.0. This folder is a skeleton; its `verdict.md` landing in a commit is the signal analysis is complete.
