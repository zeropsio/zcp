# Codex round P0 PRE-WORK — cycle-3 plan validation

Date: 2026-04-27
Round type: PRE-WORK (per cycle-1 §10.1 P0 row 1; cycle-3 plan §5 Phase 0 step 4)
Plan reviewed: `plans/atom-corpus-hygiene-followup-2-2026-04-27.md`
Reviewer: Codex
Reviewer brief: validate the 5 findings (F1, F3, F4, F5) + the new Axis N rule against current corpus state; cite file:line for every claim.

---

## Round 1 — 2026-04-27

### Per-finding validation (round 1)

- **F1 `develop-first-deploy-scaffold-yaml.md`**: APPROVE — anchors L41-L45 + L47 match; cross-link redundancy at L24 + L39 + `develop-deploy-modes.md` L18-L25 confirms safe drop.
- **F3 `develop-push-dev-deploy-local.md`**: APPROVE — L13-L17 anchor match; rewrite must preserve CWD/`workingDir`, no-`sourceService`, stage-target signals.
- **F3 `develop-deploy-files-self-deploy.md`**: APPROVE — L23-L24 anchor match; rephrase must preserve "later self-deploys have no source to upload" recovery guardrail.
- **F3 `strategy-push-git-trigger-actions.md`**: APPROVE (KEEP correct) — `zcli push` literal at L12-L14, YAML at L75, error context at L109-L112.
- **F3 `strategy-push-git-intro.md`**: APPROVE (KEEP correct) — Actions row distinguishes webhook vs Actions via `zcli push` at L21-L22.
- **F3 `develop-platform-rules-local.md`** L31: **NEEDS-REVISION** — "DROP entire row" overstates duplication; L31 also carries push-dev-vs-git-push uncommitted-tree distinction (push-dev ships uncommitted tree; git-push needs commits) which is unique to this row. `develop-push-dev-deploy-local.md` only carries CWD/no-`sourceService` at L13-L17, NOT the strategy-uncommitted distinction.
- **F3 `develop-first-deploy-asset-pipeline-container.md`**: APPROVE — L17 anchor match; `zcli push` → `zerops_deploy` swap preserves asset-pipeline warning (L14-L17 + L41-L46).
- **F3 `develop-strategy-review.md`**: APPROVE — L15-L17 anchor match; parenthetical drop only; load-bearing strategy options preserved at L15-L23.
- **F3 `develop-first-deploy-asset-pipeline-local.md`**: APPROVE — L31-L33 anchor match; `zcli push` → `zerops_deploy` swap preserves working-dir-ships-stage signal.
- **F4 `develop-push-dev-workflow-dev.md`**: **NEEDS-REVISION** — L16-L29 anchors match, BUT proposed rewrite drops diagnostic response fields `running`, `healthStatus`, `startMillis` from L22-L23. For HIGH-risk atom, recovery (signal #4) requires reading these fields BEFORE issuing follow-up call.
- **F5 `develop-static-workflow.md`**: **NEEDS-REVISION** — step-1 anchor at L13 is correct, but the same universal atom ALSO leaks env detail at L27-L28 ("dev container over SSH") which the plan does NOT call out as an explicit F5 work unit. Either add explicit row OR require Axis N CORPUS-SCAN to catch it.

### Axis N rule validation (round 1)

1. **Distinction from Axis K**: APPROVE — Axis K = outside-envelope cross-flow leakage; Axis N = within-flow universal-atom env-detail. Distinct.
2. **Quality of the 2 examples**: APPROVE — `develop-static-workflow.md` (no `environments:` axis; leaks at L13 + L27-28) and `develop-strategy-review.md` (no `environments:` axis; leaks at L15-L17) are clean exemplars.
3. **Inverse unification rule false-positive risk**: **NEEDS-REVISION** — "Risk: low" framing too weak. Env-split atoms can encode load-bearing guardrails: `develop-platform-rules-local.md` L14-L24 (use `Bash run_in_background=true` harness; `zerops_dev_server` is container-only) vs `develop-platform-rules-container.md` L20-L24 (use `zerops_dev_server`; do NOT hand-roll `ssh <host> "cmd &"` backgrounding). These are signal #3 (tool-selection) + signal #5 (do-not). Rule needs explicit DO-NOT-UNIFY exception when env-split IS the guardrail.

### Round 1 VERDICT

`VERDICT: NEEDS-REVISION`

### Round 1 author note

One Codex concern (paraphrased as "F3 path note: plan lists `develop-strategy-push-git-trigger-actions.md` and `develop-strategy-push-git-intro.md` but on-disk files lack the `develop-` prefix") was a misread caused by my Codex prompt — my prompt added the `develop-` prefix incorrectly, and Codex echoed the wrong path back as a plan defect. The plan §5 Phase 2 table (L257-L258) actually uses correct disk names (`strategy-push-git-trigger-actions`, `strategy-push-git-intro`). NOT a plan defect; will fix prompt for round 2.

### Plan revisions applied (round 1 → round 2)

1. **§3 Axis N inverse rule** (concern #5): added DO-NOT-UNIFY exception block (lines 125-135 of revised plan) — env splits encoding tool-selection / recovery / do-not guardrails are NEVER unification candidates. Phase 4 CORPUS-SCAN classifier MUST apply this exception.
2. **§5 Phase 2 F3 platform-rules-local row** (concern #2): changed from "DROP entire row" to "REPHRASE — drop `zcli push` mention; PRESERVE push-dev-vs-git-push uncommitted-tree distinction". Plan line 259.
3. **§5 Phase 3 F4 proposed rewrite** (concern #3): added "The response carries `running`, `healthStatus`, `startMillis`, and on failure a `reason` code (see `develop-dev-server-reason-codes`) — read it before issuing another call." line in the restart bullet (plan lines 313-316 of revised text).
4. **§5 Phase 4 F5** (concern #4): added explicit (b) work unit for L27-L28 "dev container over SSH" qualifier drop (plan lines 382-390 of revised text).

---

## Round 2 — 2026-04-27

Dispatched: 2026-04-27 with corrected prompt (no `develop-` prefix on `strategy-*` atoms) + revised plan.
Status: COMPLETE.

### Per-revision validation (round 2)

- **Revision 1** (§3 DO-NOT-UNIFY exception): APPROVE — exception correctly captures env-split atoms whose split IS tool-selection/recovery/do-not guardrail; Phase 4 CORPUS-SCAN MUST apply it. Verified against `develop-platform-rules-local.md` L14 + L23 and `develop-platform-rules-container.md` L20 (`plans/atom-corpus-hygiene-followup-2-2026-04-27.md:125`).
- **Revision 2** (§5 Phase 2 platform-rules-local row): APPROVE — REPHRASE preserves push-dev-vs-git-push uncommitted-tree distinction; drops only `zcli push` mechanism mention. Verified against `develop-platform-rules-local.md` L31 (`plans/atom-corpus-hygiene-followup-2-2026-04-27.md:259`).
- **Revision 3** (§5 Phase 3 F4 rewrite): APPROVE — preserves `running`, `healthStatus`, `startMillis`, `reason` with cross-ref to reason-codes; keeps log recovery via `action=logs logLines=60`. Verified against `develop-push-dev-workflow-dev.md` L22 + L31 (`plans/atom-corpus-hygiene-followup-2-2026-04-27.md:313`, `:324`).
- **Revision 4** (§5 Phase 4 F5): APPROVE — both work units listed; (b) is surgical (only "on a dev container over SSH" qualifier; preserves "push-dev for fast iteration" strategy-fit signal). Verified against `develop-static-workflow.md` L13 + L27 (`plans/atom-corpus-hygiene-followup-2-2026-04-27.md:382`, `:385`).

### Re-validation of round-1 APPROVE items (round 2)

All still APPROVE; no new drift surfaced:
- F1 `develop-first-deploy-scaffold-yaml.md` L41 + L47 — drops still applicable.
- F3 `develop-push-dev-deploy-local.md` L13-L17, `develop-deploy-files-self-deploy.md` L23-L24, `develop-first-deploy-asset-pipeline-container.md` L17, `develop-strategy-review.md` L15, `develop-first-deploy-asset-pipeline-local.md` L31 — all still applicable.
- F3 KEEP atoms `strategy-push-git-trigger-actions.md` L12+L75+L112, `strategy-push-git-intro.md` L22 — still load-bearing.
- F5 `develop-static-workflow.md` base content (L11, L20, L23, L27, L30) remains coherent around planned env-leak edits.
- Axis K vs N distinction (plan §3 L94) holds.
- Axis N example quality (plan §3 L104, L106, L110) holds.

### Round 2 VERDICT

`VERDICT: APPROVE`

Plan cleared for Phase 1 entry.
