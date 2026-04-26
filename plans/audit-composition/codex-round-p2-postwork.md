# Codex round: Phase 2 POST-WORK validation (2026-04-26)

Round type: POST-WORK per §10.1 Phase 2 row 3
Reviewer: Codex (post-work fresh agent)
Inputs read: dedup-candidates.md, phase-2-tracker.md, all 14 touched atoms, 5 dedup commit diffs, atoms_lint.go, corpus_coverage_test.go, atoms_lint_test.go, recipe_atom_lint_test.go

> **Artifact write protocol note (carries over).** Codex sandbox
> blocks artifact writes; this artifact was reconstructed verbatim
> from Codex's text response.

## Question 1 — Residual dup hunt

### Post-#3 residual (SSHFS)
- `develop-push-dev-workflow-dev.md:16` — restates "Edit code on `/var/www/{hostname}/`"
- `develop-push-dev-workflow-simple.md:15` — same edit-on-mount path guidance
- `develop-push-dev-deploy-container.md:15-19` — restates deploy-from-mount mechanics verbatim
- `develop-http-diagnostic.md:29-31` — mount-log guidance (now has cross-link but still carries the content)
- Accepted non-residuals: `develop-first-deploy-write-app.md:14-16` and `develop-first-deploy-intro.md:32-33` keep first-deploy-specific empty SSHFS state (time-specific, tracker rationale justified)

### Post-#14 residual (deploy=new container)
- `develop-dynamic-runtime-start-container.md:63-66` — restates "rebuild drops the dev process"
- `develop-close-push-dev-dev.md:25-29` — restates "Each deploy gives a new container with no dev server"
- `develop-push-dev-workflow-dev.md:27-29` — restates rebuilt-container start-vs-restart for `zerops.yaml` changes
- `develop-change-drives-deploy.md:12-13` — restates deployFiles persistence boundary while cross-linking to canonical (borderline acceptable)

### Post-#4 residual (zerops_dev_server shape)
- `develop-push-dev-workflow-dev.md:22-23` — repeats `running`, `healthStatus`, `startMillis`, `reason` outside canonical
- `develop-dev-server-reason-codes.md:24-27` — repeats observable field set `running`, `healthStatus`, `startMillis`, `logTail`
- Accepted axis-specific: `develop-dev-server-triage.md:39-47` uses those fields as a decision matrix (behavior, not just field list)

### Post-#7 residual (verify cadence)
- `develop-http-diagnostic.md:15-17` — states generic "zerops_verify first" cadence in non-canonical diagnostic atom
- `develop-close-push-dev-standard.md:17-24` — carries explicit `zerops_verify` commands for dev and stage close (close-sequence command block, not just cadence)

### Post-#5 residual (restart-vs-deploy conflict)
**Blocking residual conflict remains:** `develop-push-dev-workflow-dev.md:25` says code-only changes use `zerops_dev_server action=restart` and need no redeploy. `develop-dev-server-triage.md:45-47` says a running server with HTTP 5xx should be fixed by editing code "then deploy." Both atoms can fire for develop-active dynamic deployed container dev envelopes.

### Other residuals (not in the 5 dedups)
- Push-git mechanics duplication (`develop-push-git-deploy.md:14-35`): tracker rows #1 and #2 mark as DEFERRED-TO-PHASE-6 — deferral confirmed, makes sense.
- Local-mode topology/runtime duplication: tracker row #6, DEFERRED-TO-PHASE-6 — confirmed.
- Browser verification protocol: tracker row #13, DEFERRED-TO-PHASE-6 — confirmed. `develop-platform-rules-container.md:37-38` still carries Agent Browser line while `develop-verify-matrix` owns protocol.
- DeployFiles class semantics: tracker row #9, DEFERRED-TO-PHASE-6 — confirmed.

## Question 2 — Axis regression check

| atom | dedup # | cross-link target | does target co-fire on relevant envelopes? | regression? |
|---|---|---|---|---|
| `develop-first-deploy-write-app` | #3 | `develop-platform-rules-container` | YES — both fire on develop-active/container | No |
| `develop-http-diagnostic` | #3 | `develop-platform-rules-container` | PARTIAL — target is container-only; source has no container filter, so local diagnostics get a mismatched cross-link | Yes (existing/pre-Phase-2 axis issue, not introduced by #3) |
| `develop-push-dev-deploy-container` | #3 | `develop-deploy-modes`, `develop-deploy-files-self-deploy`, `develop-platform-rules-container` | YES — source is push-dev/container; all targets are develop-active or container | No |
| `develop-push-dev-workflow-dev` | #3, #14 | `develop-dev-server-reason-codes`, `develop-platform-rules-container`, `develop-platform-rules-common` | YES — source is deployed/dev/push-dev/container; all targets co-fire | No |
| `develop-push-dev-workflow-simple` | #3 | `develop-platform-rules-container` | YES — source is deployed/simple/push-dev/container; target is develop-active/container | No |
| `develop-close-push-dev-dev` | #14 | `develop-dev-server-reason-codes`, `develop-dynamic-runtime-start-container`, `develop-platform-rules-common` | YES — source is deployed/dev/push-dev/container; all targets co-fire | No |
| `develop-dynamic-runtime-start-container` | #14 | `develop-dev-server-reason-codes`, `develop-platform-rules-common`, `develop-platform-rules-container` | PARTIAL — reason-codes requires `deployStates: [deployed]`; source has no deployState filter, so never-deployed dynamic envelopes won't co-fire reason-codes; pre-existing issue, not Phase-2 regression | No (pre-existing) |
| `develop-platform-rules-container` | #4 | `develop-dynamic-runtime-start-container`, `develop-dev-server-reason-codes` | PARTIAL — source fires on all develop-active/container; targets are narrower (dynamic/deployed only); link is conditional on the "Long-running dev processes" bullet, which is acceptable | No |
| `develop-close-push-dev-standard` | #7 | `develop-first-deploy-promote-stage`, `develop-auto-close-semantics`, `develop-dynamic-runtime-start-container` | **NO for `develop-first-deploy-promote-stage`**: source is deployed/standard close; target is never-deployed/standard first-deploy. These envelopes never co-fire. Regression introduced by Phase 2. | **YES** |
| `develop-first-deploy-intro` | #7 | `develop-first-deploy-scaffold-yaml`, `develop-verify-matrix` | YES — source is never-deployed; verify-matrix is develop-active; co-fires | No |
| `develop-change-drives-deploy` | #5 | `develop-auto-close-semantics`, `develop-platform-rules-common`, `develop-push-dev-workflow-dev`, `develop-push-dev-workflow-simple` | YES — source is broad develop-active; all targets co-fire on relevant mode-specific envelopes | No |

## Question 3 — MustContain pin migration audit

- `"persistence boundary"` appears in exactly one atom: `develop-change-drives-deploy.md:12`. Confirmed.
- The `develop_push_dev_dev_container` fixture pin was migrated from `"edit → deploy"` to `"persistence boundary"` (`corpus_coverage_test.go:210-218`). No other MustContain-pinned phrase was dropped by any of the five Phase 2 commits.

## Verdict

**Phase 2 EXIT clean: NO** (at the time of round)

**Blocking findings:**

1. **Cross-link axis regression** — `develop-close-push-dev-standard.md` links to `develop-first-deploy-promote-stage` (`develop-close-push-dev-standard.md:4-10`), but source fires on deployed standard close and target fires on never-deployed standard first-deploy. These envelopes never co-fire.
   - Proposed fix: Remove `develop-first-deploy-promote-stage` from the `references-atoms` list in `develop-close-push-dev-standard`; add a deployed-standard-close canonical if one is needed, or link to `develop-auto-close-semantics` which already co-fires.
   - Classification: **Phase 2 ship-blocker (real regression)**

2. **Restart-vs-deploy conflict survives Phase 2** — `develop-push-dev-workflow-dev.md:25` says code-only changes restart the dev server (no deploy needed). `develop-dev-server-triage.md:45-47` says HTTP 5xx while server is running → "edit code then deploy." Both atoms fire on the same develop-active/dynamic/deployed/dev/container envelope.
   - Proposed fix: Change triage's 5xx action to "edit code, then follow the mode-specific iteration atom" — or add an explicit dev-mode exception to triage: "dev mode: restart; other modes: deploy."
   - Classification: **Phase 2 ship-blocker (real regression)**

3. **`develop-http-diagnostic` container-filter missing** — atom has no container filter but links to container-only platform rules and carries mount-specific log guidance.
   - Proposed fix: Add `environments: [container]` frontmatter or split into local/container variants.
   - Classification: **Phase 6+ follow-up** (pre-existing axis issue, not introduced by Phase 2; no fixture relies on local HTTP diagnostic)

## Post-round resolution (executor edit, commit <pending>)

- **Blocker 1 RESOLVED**: removed `develop-first-deploy-promote-stage` from `develop-close-push-dev-standard`'s `references-atoms`; inlined the "no second build, stage auto-starts" facts directly in the close atom (the agent reading close-push-dev-standard now sees those facts in the rendered output rather than chasing a non-co-firing cross-link). +50 B in close atom but axis-correct.
- **Blocker 2 RESOLVED**: `develop-dev-server-triage.md` line 45-47 rewritten to defer to `develop-change-drives-deploy` for the mode-specific cadence — "Edit code, then follow the mode-specific iteration cadence (dev-mode: `action=restart`; simple/stage: `zerops_deploy`)". Adds 133 B but resolves the contradiction.
- **Finding 3 (Phase 6+ follow-up)**: deferred per Codex's classification.

Post-fix Phase 2 cumulative recovery: 3071 B aggregate (was 3204 B; lost 133 B to conflict resolution but contradictions are now gone — quality-over-bytes per CLAUDE.local.md "Engineering Priority").
