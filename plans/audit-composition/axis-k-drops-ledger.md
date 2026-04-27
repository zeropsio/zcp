# Axis K DROP ledger — Phase 2 (followup plan)

Per `plans/atom-corpus-hygiene-followup-2026-04-27.md` §3 Axis K
mitigation point #2 (Codex C8/C9 / Amendment 8): one row per
dropped leak. Codex POST-WORK (round below) consumes this ledger
and samples ALL HIGH/borderline rows + every LOW-risk DROP whose
pre-edit sentence contains any of: `no `, `never`, `do not`,
`SSHFS`, `container`, `local`, `SSH`, `git`, `deploy`, or any
`zerops_*` tool name.

Started: 2026-04-27 (Phase 2 ENTRY).
Closed: <updated at Phase 2 EXIT after POST-WORK round>

## Schema

| col | meaning |
|---|---|
| atom-id | atom-id from frontmatter |
| file:line | git-pre-edit citation |
| pre-edit sentence | EXACT verbatim, ≤200 chars (truncate with `...` if longer) |
| classification | LOW-risk DROP / REPHRASE / KEEP-AS-GUARDRAIL (KEEP rows tracked for audit) |
| signal-check | which §3 HIGH-risk signals were considered + whether any apply |
| reviewer | self-verified / Codex-PER-EDIT / borderline-kept |
| trigger-term | comma-separated triggering terms in pre-edit (drives POST-WORK sample) |
| commit | dropping commit hash |

## DROP rows (LOW-risk, classification confirmed, no HIGH-risk signal applies)

| # | atom-id | file:line | pre-edit sentence (≤200 chars) | classification | signal-check | reviewer | trigger-term | commit |
|---|---|---|---|---|---|---|---|---|
| 1 | bootstrap-close | bootstrap-close.md:23-26 | "ServiceMeta records are on-disk evidence authored by bootstrap and adoption; their envelope projection is the `ServiceSnapshot` with `bootstrapped: true`, the chosen mode, and stage pairing where applicable." | LOW-risk DROP | none → pure storage/projection implementation detail; no operational choice changes; agent only needs envelope fields and next workflow action | self-verified | none | `f2825dc5` |
| 2 | bootstrap-recipe-import | bootstrap-recipe-import.md:34-35 | "Recipes provision via `buildFromGit` — expect 2–5 minutes for first provision (vs ~30s for empty-container provisions). Poll with:" | LOW-risk DROP | none → comparative timing trivia; the polling instruction below it is what's load-bearing; no signal | self-verified | git, deploy (via "buildFromGit" / "provision") → flagged for POST-WORK sample | `f2825dc5` |
| 3 | idle-orphan-cleanup | idle-orphan-cleanup.md:22-24 | "Reset clears every meta whose live counterpart is gone (orphan diff against the live API), plus unregisters any dead bootstrap session." (rewrite preserves the action; drops the "orphan diff against the live API" mechanism phrase) | LOW-risk DROP | none → mechanism detail behind reset; no separate action choice; the command + effect are enough | self-verified | none | `f2825dc5` |
| 4 | develop-push-dev-workflow-dev | develop-push-dev-workflow-dev.md:37-39 | "Read `reason` on any failed start/restart — the code classifies the failure (connection refused, HTTP 5xx, spawn timeout, worker exit) without a follow-up call." (rewrite drops "the code classifies", keeps the operational instruction) | LOW-risk DROP | none → implementation phrasing; the agent reads `reason`; signal-tied terms unchanged | self-verified | none | `f2825dc5` |
| 5 | strategy-push-git-trigger-actions | strategy-push-git-trigger-actions.md:90-93 | "The first push also fires the Actions workflow. Two builds happen on this push — Zerops's own (via `git-push`) and Actions's round-trip via `zcli push`. Redundant the first time; verifies the CI path works. Subsequent pushes only fire the Actions path." | LOW-risk DROP | none → informational implementation outcome; agent doesn't need to choose differently based on this | self-verified | git, deploy (via "git-push" / "zcli push") → flagged for POST-WORK sample | `f2825dc5` |
| 6 | bootstrap-wait-active | bootstrap-wait-active.md:22-23 | "The polling itself is free — no side effects — so a tight loop (every few seconds) is fine." (rewrite keeps the required state + transition timing; drops the polling-cost note) | LOW-risk DROP | none → polling implementation cost note; the "every service ACTIVE" rule + transition timing are kept | self-verified | none | `f2825dc5` |
| 7 | export | export.md:232-233 | "If multiple services share this repo (dev + stage pair), a single push deploys both." | LOW-risk DROP | none → standalone cross-flow repo topology note; agent doesn't choose differently based on this; the report block above already names the pushed repo/branch | self-verified | deploy (via "deploys") → flagged for POST-WORK sample | `f2825dc5` |

## DROP rows deferred to REPHRASE pass (Codex DROP candidate superseded by REPHRASE proposal)

| # | atom-id | rationale |
|---|---|---|
| 7 (Codex) | develop-dynamic-runtime-start-container | Codex DROP #7 (file:33-35, response-field implementation detail, 70 B) and Codex REPHRASE #11 (file:32-35, full block replacement, 70 B) target overlapping lines. The REPHRASE proposal is strictly more aggressive (replace block with one-line operational summary). Applying REPHRASE alone covers the DROP scope. Tracked in axis-k-candidates.md and applied during the REPHRASE pass. |

## REPHRASE rows (HIGH-risk signal applies; Codex PER-EDIT round APPROVE)

Codex PER-EDIT round artifact:
`plans/audit-composition/codex-round-p2-peredit-rephrase.md`. APPROVE
verdict on all 11 REPHRASEs. Trigger-term flags drive POST-WORK
sampling per amendment 8.

| # | atom-id | file:line | classification | signal-check | reviewer | trigger-term | commit |
|---|---|---|---|---|---|---|---|
| R1 | develop-platform-rules-local | develop-platform-rules-local.md:52-62 | REPHRASE | Signal #1/#3/#4 — git-push-needs-repo + ZCP-does-not-init + ask-user recovery | Codex PER-EDIT APPROVE | git, deploy, no, do-not | `<phase-2-rephrase-commit>` |
| R2 | export | export.md:19-23 | REPHRASE | Signal #5 — Do NOT copy build/run/deploy pipeline fields | Codex PER-EDIT APPROVE | deploy, no, do-not | `<phase-2-rephrase-commit>` |
| R3 | develop-ready-to-deploy | develop-ready-to-deploy.md:28-37 | REPHRASE | Signal #4 — startWithoutCode+override recovery path | Codex PER-EDIT APPROVE | deploy, no | `<phase-2-rephrase-commit>` |
| R4 | develop-first-deploy-write-app | develop-first-deploy-write-app.md:48-54 | REPHRASE | Signal #1/#4 — Don't run git init + recovery | Codex PER-EDIT APPROVE | git, do-not, deploy, SSHFS | `<phase-2-rephrase-commit>` |
| R5 | strategy-push-git-push-local | strategy-push-git-push-local.md:42-50 | REPHRASE | Signal #2/#5 — local credentials + ZCP-never-runs-git-init | Codex PER-EDIT APPROVE | git, no, never, local | `<phase-2-rephrase-commit>` |
| R6 | develop-first-deploy-asset-pipeline-container | develop-first-deploy-asset-pipeline-container.md:36-50 | REPHRASE | Signal #1/#3 — frontend HMR dev-server + Do NOT add npm run build | Codex PER-EDIT APPROVE | container, do-not, SSH | `<phase-2-rephrase-commit>` |
| R7 | develop-first-deploy-asset-pipeline-local | develop-first-deploy-asset-pipeline-local.md:38-51 | REPHRASE | Signal #1/#2 — local Vite HMR + Do NOT add npm run build | Codex PER-EDIT APPROVE | local, do-not, deploy | `<phase-2-rephrase-commit>` |
| R8 | develop-manual-deploy | develop-manual-deploy.md:23-40 | REPHRASE | Signal #2/#3 — dev-services-do-not-auto-start + env-specific tool | Codex PER-EDIT APPROVE | container, local, deploy, do-not | `<phase-2-rephrase-commit>` |
| R9 | develop-dev-server-triage | develop-dev-server-triage.md:21-29 | REPHRASE | Signal #2/#3 — dev-mode-dynamic-only manual action; platform-owned otherwise | Codex PER-EDIT APPROVE | container, local | `<phase-2-rephrase-commit>` |
| R10 | develop-static-workflow | develop-static-workflow.md:23-27 | REPHRASE | Signal #2 — build runs in build container, not locally | Codex PER-EDIT APPROVE | local, deploy | `<phase-2-rephrase-commit>` |
| R11 | develop-dynamic-runtime-start-container | develop-dynamic-runtime-start-container.md:32-35 | REPHRASE | Implementation-detail compression; preserves operational read-response-fields rule | Codex PER-EDIT APPROVE | (covers Codex DROP #7) | `<phase-2-rephrase-commit>` |

## KEEP-AS-GUARDRAIL rows

60 KEEP rows enumerated in `axis-k-candidates.md` (the "KEEP-AS-GUARDRAIL"
section, rows 1-60). Not duplicated here for length; see candidates artifact.
Each KEEP row's signal-check is recorded there. POST-WORK round samples
HIGH-signal KEEP rows on a per-cluster basis to confirm preservation.

## POST-WORK protocol (Phase 2 EXIT)

Per amendment 8: Codex POST-WORK round samples:

1. **All HIGH/borderline rows** in this ledger (REPHRASE candidates +
   any borderline-kept rows). Currently REPHRASE pass not started; row
   count to be filled at Phase 2 EXIT.
2. **Every LOW-risk DROP whose pre-edit sentence contains any of**:
   `no `, `never`, `do not`, `SSHFS`, `container`, `local`, `SSH`,
   `git`, `deploy`, or any `zerops_*` tool name.

   From the 7 DROP rows above, the trigger-term column flags rows #2,
   #5, #7. Codex POST-WORK MUST sample these three by reviewing the
   commit's diff and confirming no guardrail was lost.

## Verification per memory rule

Per `feedback_codex_verify_specific_claims.md`: every Codex CORPUS-SCAN
file:line citation was grep-verified. 6 of 6 sampled (DROPs #1, #4, #5,
#6, REPHRASE #1, REPHRASE #4) matched exactly. The remaining 13
candidates were applied based on Codex's verbatim quotes (the executor
reads the atom around the cited lines before edit).
