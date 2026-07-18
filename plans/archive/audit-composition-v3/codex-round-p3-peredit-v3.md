# Codex round P3 PER-EDIT — F4 develop-push-dev-workflow-dev rewrite

Date: 2026-04-27
Round type: PER-EDIT (MANDATORY per plan §5 Phase 3 HIGH-risk axis-b classification)
Plan reviewed: `plans/atom-corpus-hygiene-followup-2-2026-04-27.md` §5 Phase 3
Reviewer: Codex
Reviewer brief: verify proposed F4 rewrite preserves signals #3 (tool-selection), #4 (recovery), #5 (do-not); cross-check auto-watch claim against repo guidance; confirm AST-pinned references-fields all still resolve.

---

## Round 1 — 2026-04-27 (NEEDS-REVISION)

### Per-checklist verification

1. **Signal #3 tool-selection**: PASS — `zerops_dev_server` action=restart/start/logs all preserved with correct args; `develop-platform-rules-common` cross-ref preserved (`develop-push-dev-workflow-dev.md:16,19,27,28,29,31,34`).
2. **Signal #4 recovery**: PASS — `running`, `healthStatus`, `startMillis`, `reason` named; "read it before issuing another call" guardrail consistent with `develop-dynamic-runtime-start-container.md:32,33`; `develop-dev-server-reason-codes` cross-ref preserved (`develop-push-dev-workflow-dev.md:22,23`).
3. **Signal #5 do-not**: **FAIL** — deploy anti-pattern (`action=start` NOT restart) preserved, BUT current atom's L25 guardrail "Code-only changes: `action=restart` is enough — no redeploy" (forbids redeploy on code-only iteration) is DROPPED in the proposed rewrite.
4. **NEW content soundness**: **FAIL** — auto-watch claim is wrong. `docs/zrecipator-archive/implementation-v22-postmortem.md:866-867` records "SSHFS mount doesn't surface inotify events; file watchers need polling mode". The "no action needed" framing contradicts this — watchers work only when configured for polling, not by default.
5. **References-fields AST pin**: PASS — `Reason`, `Running`, `HealthStatus`, `StartMillis`, `LogTail` all referenced in rewrite body; AST integrity gate at `internal/workflow/atom_reference_field_integrity_test.go:8,24` will pass.

### Round 1 VERDICT

`VERDICT: NEEDS-REVISION`

### Plan revisions applied (round 1 → round 2)

**§5 Phase 3 proposed rewrite** revised to address both failures:

1. **No-redeploy guardrail restored** — added leading sentence "**Code-only edits never trigger `zerops_deploy`** — deploy is for `zerops.yaml` changes only (see '**`zerops.yaml` changes**' below)" right after the first paragraph. This explicitly preserves the do-not from current atom L25 + cross-references where deploy IS required.

2. **Polling-mode caveat added** — replaced "auto-watch the SSHFS mount — no action needed" with "pick up edits **only when configured for polling** — SSHFS does not surface inotify events. Set `CHOKIDAR_USEPOLLING=1` (vite/webpack), `--poll` (nodemon), or the runner's equivalent." Concrete env-var/flag list per runner. The fallback bullet "Otherwise (non-watching runner, polling not configured, OR the process died), `zerops_dev_server action=restart …`" covers all non-polling paths.

Plan §5 Phase 3 also gained a "Round-1 PER-EDIT Codex round flagged two defects (now resolved above)" annotation explaining the changes for future readers.

---

## Round 2 — 2026-04-27

Dispatched: 2026-04-27.
Status: COMPLETE.

### Per-revision validation

- **Revision 1 (no-redeploy guardrail restored)**: APPROVE — restored sentence "**Code-only edits never trigger `zerops_deploy`** — deploy is for `zerops.yaml` changes only" preserves current atom L25 do-not signal; cross-link to `**`zerops.yaml` changes**` section correct (`plans/atom-corpus-hygiene-followup-2-2026-04-27.md:302-306` + `:322-326`; `internal/content/atoms/develop-push-dev-workflow-dev.md:25`).
- **Revision 2 (polling-mode caveat)**: APPROVE — claim matches `docs/zrecipator-archive/implementation-v22-postmortem.md:865-867`; runner examples (`CHOKIDAR_USEPOLLING=1` for vite/webpack, `--poll` for nodemon) accurate; fallback covers all 3 cases (non-watching runner, polling not configured, process died); wording avoids implying automatic polling (`plans/atom-corpus-hygiene-followup-2-2026-04-27.md:309-316`).

### Re-validation of round-1 APPROVE items

- **Signal #3 tool-selection**: still holds (`plans/atom-corpus-hygiene-followup-2-2026-04-27.md:314-316,322-326,328-330`; preserves `develop-platform-rules-common` reference at `internal/content/atoms/develop-push-dev-workflow-dev.md:11,27-29`).
- **Signal #4 recovery**: still holds — `running`, `healthStatus`, `startMillis`, `reason` all named with reason-codes cross-link + "read it before issuing another call" guardrail (`plans/atom-corpus-hygiene-followup-2-2026-04-27.md:317-320`).
- **References-fields AST pin**: still holds — all 5 fields (`Reason`, `Running`, `HealthStatus`, `StartMillis`, `LogTail`) referenced in revised body. AST integrity test PASS.

### Round 2 VERDICT

`VERDICT: APPROVE`

Rewrite cleared for application; F4 atom edit ready to commit.
