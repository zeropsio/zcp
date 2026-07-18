# Codex round P2 PER-EDIT — REPHRASE diff review

Date: 2026-04-27
Round type: PER-EDIT (per §10.1 P2 row 2 + amendment 1)
Plan: plans/atom-corpus-hygiene-followup-2026-04-27.md §3 Axis K
Diff file reviewed: /tmp/phase2-rephrase-diff.patch (~267 lines, 11 atoms)
Reviewer: Codex
Reviewer brief: confirm every §3 Axis K HIGH-risk signal that triggered the REPHRASE classification is preserved in the executor's edit; flag any regression.

## Per-atom verdict

### #1 — develop-platform-rules-local
Signals flagged: Signal #3/#5; git-push must verify a user-owned committed repo, ZCP must not initialize the user's repo, and default push-dev needs no git state.
Pre-edit signals present: The old block said to verify the working dir is a git repo with at least one commit before `zerops_deploy strategy=git-push`; if absent, ask the user to initialize/commit themselves; ZCP does NOT initialize git in the user's working directory; default `zerops_deploy` uses push-dev/no git state.
Post-edit signals present: The post-edit atom still says `strategy=git-push` needs a committed local git repo at internal/content/atoms/develop-platform-rules-local.md:52. It requires verification with `git status` and `git log` before `zerops_deploy strategy=git-push` at internal/content/atoms/develop-platform-rules-local.md:53 and internal/content/atoms/develop-platform-rules-local.md:54. It tells the agent to ask the user to run `git init && git add -A && git commit -m 'initial'` when no work tree/commit exists at internal/content/atoms/develop-platform-rules-local.md:54 and internal/content/atoms/develop-platform-rules-local.md:55. It preserves the explicit "ZCP does NOT initialize git" guardrail at internal/content/atoms/develop-platform-rules-local.md:56 and internal/content/atoms/develop-platform-rules-local.md:57. It preserves the default push-dev/no-git-state contrast at internal/content/atoms/develop-platform-rules-local.md:57 and internal/content/atoms/develop-platform-rules-local.md:58.
Verdict: PRESERVED
Notes: The rationale about identity/default branch/gitignore was removed, but the operational guardrail and recovery direction remain explicit.

### #2 — export
Signals flagged: Signal #5; buildFromGit import field-list and "Do NOT copy pipeline fields" guardrail.
Pre-edit signals present: The old block said buildFromGit pulls code plus `zerops.yaml`; `zeropsSetup` selects the setup block; import services only need selector/platform fields; Do NOT copy build/run/deploy pipeline fields because they live in repo `zerops.yaml`.
Post-edit signals present: The post-edit atom preserves the buildFromGit import selector list, saying services need only `type`, `zeropsSetup`, `buildFromGit`, and platform settings at internal/content/atoms/export.md:19 and internal/content/atoms/export.md:20. It preserves the explicit "Do NOT copy build/run/deploy pipeline fields" guardrail at internal/content/atoms/export.md:20 and internal/content/atoms/export.md:21. It preserves that those fields live in the repo's `zerops.yaml` at internal/content/atoms/export.md:21 and internal/content/atoms/export.md:22.
Verdict: PRESERVED
Notes: The mechanism explanation was compressed without losing the field-list or the negative-copy guardrail.

### #3 — develop-ready-to-deploy
Signals flagged: Signal #4; startWithoutCode plus override recovery path for READY_TO_DEPLOY when ACTIVE is needed before code exists.
Pre-edit signals present: The old block said re-import with `startWithoutCode: true` and `override=true` when the container must become ACTIVE before real code; regenerate import YAML with `startWithoutCode`; call `zerops_import ... override=true`; override avoids `serviceStackNameUnavailable`; empty deploy lifts service to ACTIVE.
Post-edit signals present: The post-edit atom preserves the recovery option heading with `startWithoutCode: true` and `override=true` at internal/content/atoms/develop-ready-to-deploy.md:28. It preserves the condition "need the container ACTIVE before deploying real code" at internal/content/atoms/develop-ready-to-deploy.md:29. It instructs regenerating import YAML with `startWithoutCode: true` at internal/content/atoms/develop-ready-to-deploy.md:30 and internal/content/atoms/develop-ready-to-deploy.md:31. It preserves the exact `zerops_import content="<yaml>" override=true` action at internal/content/atoms/develop-ready-to-deploy.md:31. It explains that `override` replaces the existing service stack and avoids `serviceStackNameUnavailable` at internal/content/atoms/develop-ready-to-deploy.md:32 and internal/content/atoms/develop-ready-to-deploy.md:33. It preserves that Zerops triggers an empty deploy and lifts the service to ACTIVE at internal/content/atoms/develop-ready-to-deploy.md:33 and internal/content/atoms/develop-ready-to-deploy.md:34.
Verdict: PRESERVED
Notes: The examples of why ACTIVE may be needed were removed, but the recovery path remains actionable and explicit.

### #4 — develop-first-deploy-write-app
Signals flagged: Signal #1/#4; do not run `git init` on SSHFS/ZCP-side mount, plus recovery if it happened.
Pre-edit signals present: The old block said don't run `git init` from the ZCP-side mount; push-dev owns container-side git state; `git init` creates root-owned `.git/objects/` and breaks deploy handler `git add`; recovery is `ssh <hostname> "sudo rm -rf /var/www/.git"` and next deploy re-initializes.
Post-edit signals present: The post-edit atom preserves the "Don't run `git init` from the ZCP-side mount" guardrail at internal/content/atoms/develop-first-deploy-write-app.md:48. It preserves that push-dev deploy handlers manage container-side git state at internal/content/atoms/develop-first-deploy-write-app.md:48 and internal/content/atoms/develop-first-deploy-write-app.md:49. It preserves that running `git init` on SSHFS creates root-owned `.git/objects/` and breaks container-side `git add` at internal/content/atoms/develop-first-deploy-write-app.md:49, internal/content/atoms/develop-first-deploy-write-app.md:50, and internal/content/atoms/develop-first-deploy-write-app.md:51. It preserves the recovery command `ssh <hostname> "sudo rm -rf /var/www/.git"` at internal/content/atoms/develop-first-deploy-write-app.md:51 and internal/content/atoms/develop-first-deploy-write-app.md:52. It preserves that the next deploy re-initializes git state at internal/content/atoms/develop-first-deploy-write-app.md:52.
Verdict: PRESERVED
Notes: The shortened text still carries both the hard negation and the repair path.

### #5 — strategy-push-git-push-local
Signals flagged: Signal #2/#5; local credentials only, project `GIT_TOKEN` ignored, ZCP never runs `git init`/`git config`, committed branch HEAD is pushed, uncommitted changes are warnings only.
Pre-edit signals present: The old list said ZCP never sets `GIT_TOKEN` for local path and local push reads nothing from project `GIT_TOKEN`; ZCP never runs `git init`, `git config`, or alters the repo beyond first-push remote add; pushed state is branch HEAD and uncommitted working-tree changes are warnings only.
Post-edit signals present: The post-edit atom preserves that local push-git uses local credentials only at internal/content/atoms/strategy-push-git-push-local.md:44. It preserves that project `GIT_TOKEN` does not apply at internal/content/atoms/strategy-push-git-push-local.md:44 and internal/content/atoms/strategy-push-git-push-local.md:45. It preserves that ZCP never runs `git init` or `git config` on the repo at internal/content/atoms/strategy-push-git-push-local.md:46. It preserves the only-repo-change exception, `git remote add` on first push, at internal/content/atoms/strategy-push-git-push-local.md:46 and internal/content/atoms/strategy-push-git-push-local.md:47. It preserves that pushed state is branch HEAD at internal/content/atoms/strategy-push-git-push-local.md:48. It preserves that uncommitted working-tree changes are warned via `warnings[]` but not pushed at internal/content/atoms/strategy-push-git-push-local.md:48 and internal/content/atoms/strategy-push-git-push-local.md:49.
Verdict: PRESERVED
Notes: The local/container contrast is shorter, but the credential boundary and repo-mutation guardrails remain explicit.

### #6 — develop-first-deploy-asset-pipeline-container
Signals flagged: Signal #1/#3; frontend HMR dev-server path and "Do NOT add npm run build" guardrail for dev buildCommands.
Pre-edit signals present: The old block said iterative frontend work should start the dev server instead; the dev server watches files and survives template edits; containers restart on every deploy so restart the dev server; Do NOT add `npm run build` to dev `buildCommands` because it breaks HMR-first design.
Post-edit signals present: The post-edit atom preserves that frontend asset-pipeline recipes intentionally omit `npm run build` from dev `buildCommands` at internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:14, internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:15, and internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:16. It preserves the HMR-over-SSH design at internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:17 and internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:18. It instructs starting the dev server over SSH for iterative frontend work at internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:36 and internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:37. It preserves the concrete SSH dev-server command at internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:39, internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:40, and internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:41. It preserves that containers restart on every `zerops_deploy` and the dev server must be restarted after each redeploy at internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:45 and internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:46. It preserves the explicit "Do NOT add `npm run build` to dev `buildCommands`" guardrail at internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:48. It preserves the HMR-first rationale and rebuild penalty at internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:49 and internal/content/atoms/develop-first-deploy-asset-pipeline-container.md:50.
Verdict: PRESERVED
Notes: The guardrail is still direct and the operational HMR path remains clear.

### #7 — develop-first-deploy-asset-pipeline-local
Signals flagged: Signal #1/#2; local Vite HMR path and "Do NOT add npm run build" guardrail for dev buildCommands.
Pre-edit signals present: The old block said run Vite locally with `npm run dev`; Vite writes `public/build/hot` with localhost URLs; hot reload avoids redeploying; deploy when stable; Do NOT add `npm run build` to dev `buildCommands` because it breaks local-HMR-first design.
Post-edit signals present: The post-edit atom preserves that these recipes intentionally omit `npm run build` from dev setup `buildCommands` at internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:14, internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:15, and internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:16. It preserves that the design assumes local Vite HMR and deploys a built artifact to stage at internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:16 and internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:17. It preserves the instruction to run Vite locally with `npm run dev` at internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:38. It preserves that the dev server writes localhost hot-file URLs and Vite helpers route assets to the local Vite server at internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:39 and internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:40. It preserves hot-reload without redeploying and deploy-when-stable guidance at internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:41. It preserves the explicit "Do NOT add `npm run build` to dev `buildCommands`" guardrail at internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:43. It preserves that doing so defeats local-HMR-first setup and causes every deploy to rebuild assets at internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:44 and internal/content/atoms/develop-first-deploy-asset-pipeline-local.md:45.
Verdict: PRESERVED
Notes: The command moved inline, but the local-HMR workflow and negative buildCommands guardrail remain intact.

### #8 — develop-manual-deploy
Signals flagged: Signal #2/#3; dev services do not auto-start after deploy, environment-specific start path, stage/simple auto-start, and code-only dev changes can restart without redeploy.
Pre-edit signals present: The old block said dev services (`zsc noop`) do not auto-start after deploy; start with `zerops_dev_server` in container env or harness background task in local env; stage/simple auto-start with healthCheck and need no `zerops_dev_server`; code-only changes can use `zerops_dev_server action=restart` without redeploy.
Post-edit signals present: The post-edit atom preserves that dev services (`zsc noop`) do not auto-start after any deploy at internal/content/atoms/develop-manual-deploy.md:23 and internal/content/atoms/develop-manual-deploy.md:24. It preserves the container-env start path via `zerops_dev_server` at internal/content/atoms/develop-manual-deploy.md:24 and internal/content/atoms/develop-manual-deploy.md:30. It preserves the local-env background-task path at internal/content/atoms/develop-manual-deploy.md:25, internal/content/atoms/develop-manual-deploy.md:26, internal/content/atoms/develop-manual-deploy.md:32, and internal/content/atoms/develop-manual-deploy.md:33. It preserves that stage/simple services auto-start with `healthCheck` at internal/content/atoms/develop-manual-deploy.md:36. It preserves that no `zerops_dev_server` call is needed for stage/simple services at internal/content/atoms/develop-manual-deploy.md:37. It preserves that code-only dev changes can use `zerops_dev_server action=restart` without redeploy at internal/content/atoms/develop-manual-deploy.md:39 and internal/content/atoms/develop-manual-deploy.md:40.
Verdict: PRESERVED
Notes: The wording now broadens the dev auto-start warning to any deploy, which is stricter than the original and does not weaken the manual-strategy guidance.

### #9 — develop-dev-server-triage
Signals flagged: Signal #2/#3; only dev-mode dynamic needs manual dev-server action, while static/implicit-webserver/simple/stage are platform-owned after deploy.
Pre-edit signals present: The old table separated implicit-webserver, dynamic dev, and dynamic simple/stage; it then said implicit-webserver, static, and simple/stage-mode dynamic end triage because platform owns lifecycle.
Post-edit signals present: The post-edit atom preserves that only `runtimeClass: dynamic` plus `mode: dev` needs a manual dev-server action at internal/content/atoms/develop-dev-server-triage.md:21 and internal/content/atoms/develop-dev-server-triage.md:22. It preserves that the dev-mode `zsc noop` idle container waits for `zerops_dev_server action=start` at internal/content/atoms/develop-dev-server-triage.md:22 and internal/content/atoms/develop-dev-server-triage.md:23. It preserves that implicit-webserver, static, and dynamic plus simple/stage are platform-owned post-deploy at internal/content/atoms/develop-dev-server-triage.md:23 and internal/content/atoms/develop-dev-server-triage.md:24. It preserves that triage ends for those platform-owned shapes at internal/content/atoms/develop-dev-server-triage.md:24.
Verdict: PRESERVED
Notes: The table became prose, but the lifecycle split and manual-action boundary are explicit.

### #10 — develop-static-workflow
Signals flagged: Signal #2; static asset builds run in the Zerops build container during deploy; local builds are preview-only.
Pre-edit signals present: The old block said Tailwind/bundler/SSG builds run in the Zerops build container during deploy, not locally; local `npm run build` is only for preview and Zerops rebuilds at deploy time.
Post-edit signals present: The post-edit atom preserves that the build step runs in the Zerops build container at deploy time at internal/content/atoms/develop-static-workflow.md:23 and internal/content/atoms/develop-static-workflow.md:24. It preserves that local builds are preview-only at internal/content/atoms/develop-static-workflow.md:24 and internal/content/atoms/develop-static-workflow.md:25. It preserves that Zerops rebuilds anyway at internal/content/atoms/develop-static-workflow.md:25.
Verdict: PRESERVED
Notes: The local command example was removed, but the local-vs-build-container mental model remains.

### #11 — develop-dynamic-runtime-start-container
Signals flagged: Operational response-field summary; keep the useful response fields that guide the next call (`running`, `healthStatus`, `reason`, `logTail`) and drop lower-value implementation detail.
Pre-edit signals present: The old block said the response carries `running`, `healthStatus`, `startMillis`, and on failure a concrete `reason` plus `logTail`; diagnose without a follow-up call.
Post-edit signals present: The post-edit atom preserves the operational response fields `running`, `healthStatus`, `reason`, and `logTail` at internal/content/atoms/develop-dynamic-runtime-start-container.md:32. It preserves the instruction to read those fields before making another call at internal/content/atoms/develop-dynamic-runtime-start-container.md:33.
Verdict: PRESERVED
Notes: `startMillis` and the phrase "diagnose without a follow-up call" were removed as intended; the remaining next-action response fields are explicit.

## Aggregate verdict

VERDICT: APPROVE
- APPROVE = every signal preserved across all 11 REPHRASEs
- NEEDS-REVISION = list which atoms need tightening + the specific signal that was lost

## Memory rule re-confirmation

Per feedback_codex_verify_specific_claims.md: cite file:line for every signal-presence claim so the executor can grep-verify.
