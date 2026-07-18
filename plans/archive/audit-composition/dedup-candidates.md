# Phase 2 dedup candidates — Codex CORPUS-SCAN (2026-04-26)

Round type: CORPUS-SCAN  
Reviewer: Codex  
Scope: Full corpus cross-atom duplicate hunt for `internal/content/atoms/*.md`.

Inputs read:

1. `internal/content/atoms/bootstrap-adopt-discover.md`
2. `internal/content/atoms/bootstrap-classic-plan-dynamic.md`
3. `internal/content/atoms/bootstrap-classic-plan-static.md`
4. `internal/content/atoms/bootstrap-close.md`
5. `internal/content/atoms/bootstrap-discover-local.md`
6. `internal/content/atoms/bootstrap-env-var-discovery.md`
7. `internal/content/atoms/bootstrap-intro.md`
8. `internal/content/atoms/bootstrap-mode-prompt.md`
9. `internal/content/atoms/bootstrap-provision-local.md`
10. `internal/content/atoms/bootstrap-provision-rules.md`
11. `internal/content/atoms/bootstrap-recipe-close.md`
12. `internal/content/atoms/bootstrap-recipe-import.md`
13. `internal/content/atoms/bootstrap-recipe-match.md`
14. `internal/content/atoms/bootstrap-resume.md`
15. `internal/content/atoms/bootstrap-route-options.md`
16. `internal/content/atoms/bootstrap-runtime-classes.md`
17. `internal/content/atoms/bootstrap-verify.md`
18. `internal/content/atoms/bootstrap-wait-active.md`
19. `internal/content/atoms/develop-api-error-meta.md`
20. `internal/content/atoms/develop-auto-close-semantics.md`
21. `internal/content/atoms/develop-change-drives-deploy.md`
22. `internal/content/atoms/develop-checklist-dev-mode.md`
23. `internal/content/atoms/develop-checklist-simple-mode.md`
24. `internal/content/atoms/develop-close-manual.md`
25. `internal/content/atoms/develop-close-push-dev-dev.md`
26. `internal/content/atoms/develop-close-push-dev-local.md`
27. `internal/content/atoms/develop-close-push-dev-simple.md`
28. `internal/content/atoms/develop-close-push-dev-standard.md`
29. `internal/content/atoms/develop-close-push-git-container.md`
30. `internal/content/atoms/develop-close-push-git-local.md`
31. `internal/content/atoms/develop-closed-auto.md`
32. `internal/content/atoms/develop-deploy-files-self-deploy.md`
33. `internal/content/atoms/develop-deploy-modes.md`
34. `internal/content/atoms/develop-dev-server-reason-codes.md`
35. `internal/content/atoms/develop-dev-server-triage.md`
36. `internal/content/atoms/develop-dynamic-runtime-start-container.md`
37. `internal/content/atoms/develop-dynamic-runtime-start-local.md`
38. `internal/content/atoms/develop-env-var-channels.md`
39. `internal/content/atoms/develop-first-deploy-asset-pipeline-container.md`
40. `internal/content/atoms/develop-first-deploy-asset-pipeline-local.md`
41. `internal/content/atoms/develop-first-deploy-env-vars.md`
42. `internal/content/atoms/develop-first-deploy-execute-cmds.md`
43. `internal/content/atoms/develop-first-deploy-execute.md`
44. `internal/content/atoms/develop-first-deploy-intro.md`
45. `internal/content/atoms/develop-first-deploy-promote-stage.md`
46. `internal/content/atoms/develop-first-deploy-scaffold-yaml.md`
47. `internal/content/atoms/develop-first-deploy-verify-cmds.md`
48. `internal/content/atoms/develop-first-deploy-verify.md`
49. `internal/content/atoms/develop-first-deploy-write-app.md`
50. `internal/content/atoms/develop-http-diagnostic.md`
51. `internal/content/atoms/develop-implicit-webserver.md`
52. `internal/content/atoms/develop-intro.md`
53. `internal/content/atoms/develop-knowledge-pointers.md`
54. `internal/content/atoms/develop-local-workflow.md`
55. `internal/content/atoms/develop-manual-deploy.md`
56. `internal/content/atoms/develop-mode-expansion.md`
57. `internal/content/atoms/develop-platform-rules-common.md`
58. `internal/content/atoms/develop-platform-rules-container.md`
59. `internal/content/atoms/develop-platform-rules-local.md`
60. `internal/content/atoms/develop-push-dev-deploy-container.md`
61. `internal/content/atoms/develop-push-dev-deploy-local.md`
62. `internal/content/atoms/develop-push-dev-workflow-dev.md`
63. `internal/content/atoms/develop-push-dev-workflow-simple.md`
64. `internal/content/atoms/develop-push-git-deploy.md`
65. `internal/content/atoms/develop-ready-to-deploy.md`
66. `internal/content/atoms/develop-static-workflow.md`
67. `internal/content/atoms/develop-strategy-awareness.md`
68. `internal/content/atoms/develop-strategy-review.md`
69. `internal/content/atoms/develop-verify-matrix.md`
70. `internal/content/atoms/export.md`
71. `internal/content/atoms/idle-adopt-entry.md`
72. `internal/content/atoms/idle-bootstrap-entry.md`
73. `internal/content/atoms/idle-develop-entry.md`
74. `internal/content/atoms/idle-orphan-cleanup.md`
75. `internal/content/atoms/strategy-push-git-intro.md`
76. `internal/content/atoms/strategy-push-git-push-container.md`
77. `internal/content/atoms/strategy-push-git-push-local.md`
78. `internal/content/atoms/strategy-push-git-trigger-actions.md`
79. `internal/content/atoms/strategy-push-git-trigger-webhook.md`

## 1. §4.3 Candidate Verification Table

| Concept from §4.3 | §4.3 count | Current count | Canonical atom | Non-canonical atoms with citations | Recoverable bytes estimate | Disposition |
|---|---:|---:|---|---|---:|---|
| `/var/www/<hostname>` SSHFS path and mount semantics | 27 atoms | 7 true restatements; other hits are command examples, export tasks, or local-mode contrasts | `develop-platform-rules-container` owns the container rule: code lives on SSHFS at `/var/www/<hostname>/`, edits use file tools, shell commands use SSH only for runtime CLIs [internal/content/atoms/develop-platform-rules-container.md:13-19] | `develop-first-deploy-write-app` repeats empty mount and direct write semantics [internal/content/atoms/develop-first-deploy-write-app.md:13-15], [internal/content/atoms/develop-first-deploy-write-app.md:43-51]; `develop-push-dev-workflow-dev` repeats edit-on-mount semantics [internal/content/atoms/develop-push-dev-workflow-dev.md:16-17]; `develop-push-dev-workflow-simple` repeats edit-on-mount semantics [internal/content/atoms/develop-push-dev-workflow-simple.md:14-15]; `develop-push-dev-deploy-container` repeats deploy-from-mount semantics [internal/content/atoms/develop-push-dev-deploy-container.md:15-19]; `develop-http-diagnostic` repeats read-log-on-mount and do-not-tail-over-SSH semantics [internal/content/atoms/develop-http-diagnostic.md:28-31]; `develop-first-deploy-intro` repeats that pre-first-deploy SSHFS can be empty [internal/content/atoms/develop-first-deploy-intro.md:32-33] | 650 B | DEDUP |
| `${hostname_KEY}` cross-service env reference syntax | 4 atoms | 5 true restatements | `develop-first-deploy-env-vars` owns the syntax, deploy-time rewrite, typo behavior, and re-check command [internal/content/atoms/develop-first-deploy-env-vars.md:18-35] | `bootstrap-env-var-discovery` states keys and `${hostname_varName}` syntax [internal/content/atoms/bootstrap-env-var-discovery.md:21-23]; `develop-first-deploy-scaffold-yaml` repeats `<KEY>: <value or ${service_KEY} cross-ref>` and points to syntax [internal/content/atoms/develop-first-deploy-scaffold-yaml.md:31-37]; `develop-first-deploy-verify` repeats `${hostname_KEY}` drift check [internal/content/atoms/develop-first-deploy-verify.md:24-29]; `bootstrap-provision-local` repeats `${hostname_varName}` resolution for dotenv generation [internal/content/atoms/bootstrap-provision-local.md:32-37]; `export` contains `${host_KEY}` keep-as-is export handling, which is export-axis-specific [internal/content/atoms/export.md:132-139] | 380 B | DEDUP |
| `deploy = new container`; only `deployFiles`-covered content persists | 3 atoms | 4 true restatements | `develop-platform-rules-common` owns the invariant: deploy creates a new container, local runtime files are lost, and only `deployFiles` content survives [internal/content/atoms/develop-platform-rules-common.md:13-14] | `develop-change-drives-deploy` repeats persistence via `deployFiles` and points back to common rules [internal/content/atoms/develop-change-drives-deploy.md:12-15]; `develop-dynamic-runtime-start-container` repeats redeploy creates a new container and old dev process is gone [internal/content/atoms/develop-dynamic-runtime-start-container.md:63-68]; `develop-close-push-dev-dev` repeats new container plus previous dev server gone [internal/content/atoms/develop-close-push-dev-dev.md:25-31]; `develop-push-dev-workflow-dev` repeats redeploy replaces the container [internal/content/atoms/develop-push-dev-workflow-dev.md:28-30] | 410 B | DEDUP |
| Managed-service env-var catalog | `bootstrap-env-var-discovery` plus related mentions | 4 axis-separated surfaces | `bootstrap-env-var-discovery` owns the catalog table for PostgreSQL, MariaDB, Valkey, KeyDB, NATS, Kafka, ClickHouse, Elasticsearch, Meilisearch, Typesense, Qdrant, object-storage, and shared-storage [internal/content/atoms/bootstrap-env-var-discovery.md:25-41] | `develop-first-deploy-env-vars` needs the first-deploy usage rule and no-guess warning [internal/content/atoms/develop-first-deploy-env-vars.md:11-16]; `bootstrap-recipe-import` needs provision-attestation guidance for env key summaries [internal/content/atoms/bootstrap-recipe-import.md:44-48]; `develop-dynamic-runtime-start-local` needs local `.env` generation context [internal/content/atoms/develop-dynamic-runtime-start-local.md:54-63] | 0 B | AXIS-JUSTIFIED |
| Standard-mode dev + stage pair semantics | 5+ atoms | 10 axis-separated mentions | `develop-first-deploy-promote-stage` owns first-deploy promotion mechanics and no-second-build rule [internal/content/atoms/develop-first-deploy-promote-stage.md:15-25]; `develop-auto-close-semantics` owns close semantics for both halves [internal/content/atoms/develop-auto-close-semantics.md:24-27] | `bootstrap-mode-prompt` defines standard as dev + stage pair in discovery [internal/content/atoms/bootstrap-mode-prompt.md:18-24]; `bootstrap-classic-plan-dynamic` asks user to confirm dev/stage pairing [internal/content/atoms/bootstrap-classic-plan-dynamic.md:11-16]; `bootstrap-classic-plan-static` asks whether stage pair is wanted [internal/content/atoms/bootstrap-classic-plan-static.md:16-21]; `bootstrap-close` describes handoff envelope with chosen mode and stage pairing [internal/content/atoms/bootstrap-close.md:22-26]; `develop-mode-expansion` owns adding a stage sibling later [internal/content/atoms/develop-mode-expansion.md:13-17], [internal/content/atoms/develop-mode-expansion.md:42-52]; `develop-close-push-dev-standard` owns close-time dev then stage sequence [internal/content/atoms/develop-close-push-dev-standard.md:14-27]; `develop-checklist-dev-mode` owns dev block versus stage block shape [internal/content/atoms/develop-checklist-dev-mode.md:13-19]; `bootstrap-provision-local` owns local-mode standard exception, stage only on Zerops [internal/content/atoms/bootstrap-provision-local.md:12-25] | 120 B | AXIS-JUSTIFIED |
| `sudo apk add` / `sudo apt-get install`; runtime packages belong in `run.prepareCommands` | 2+ atoms | 1 direct rule | `develop-platform-rules-common` owns both package-install and prepareCommands placement [internal/content/atoms/develop-platform-rules-common.md:11-12], [internal/content/atoms/develop-platform-rules-common.md:18-21] | No current non-canonical atom repeats both the sudo package-install rule and prepareCommands placement as a standalone rule. | 0 B | ALREADY-DEDUPED |
| `zerops_dev_server` field shape and action family | 2 atoms | 5 restatements | `develop-dynamic-runtime-start-container` owns action commands, parameters, idempotent status check, response fields, and redeploy restart rule [internal/content/atoms/develop-dynamic-runtime-start-container.md:18-68] | `develop-platform-rules-container` repeats response fields and warns against hand-rolled background SSH [internal/content/atoms/develop-platform-rules-container.md:20-26]; `develop-close-push-dev-dev` repeats start command, status-first, response fields, and worker no-HTTP behavior [internal/content/atoms/develop-close-push-dev-dev.md:19-35]; `develop-dev-server-triage` repeats status/start commands and response interpretation [internal/content/atoms/develop-dev-server-triage.md:29-63]; `develop-push-dev-workflow-dev` repeats restart command, response fields, logs command, and reason-code handling [internal/content/atoms/develop-push-dev-workflow-dev.md:19-40] | 520 B | DEDUP |
| `agent-browser` browser verification | 3+ atoms | 2 direct mentions | `develop-verify-matrix` owns the verification protocol: run `zerops_verify` first, then use a verify agent driving `agent-browser` for web-facing services [internal/content/atoms/develop-verify-matrix.md:24-38], with verdict handling [internal/content/atoms/develop-verify-matrix.md:40-48] | `develop-platform-rules-container` only says `agent-browser.dev` is available for browser verification [internal/content/atoms/develop-platform-rules-container.md:38-39] | 90 B | DEDUP |

## 2. Pass 2 New Dup Candidates Ranked By Recoverable Bytes

### 1. Push-git commit/push mechanics — 760 B recoverable

Atoms carrying it:

- `strategy-push-git-push-container` has the canonical container flow: set `GIT_TOKEN`, commit under `/var/www`, first push with `remoteUrl`, retry committed-code errors, and later pushes without `remoteUrl` [internal/content/atoms/strategy-push-git-push-container.md:13-55].
- `develop-push-git-deploy` repeats token scope, project env var, container commit, first push, subsequent push, and CI/manual stage handling [internal/content/atoms/develop-push-git-deploy.md:12-37].
- `develop-close-push-git-container` repeats already-configured commit and push plus fallback to strategy setup [internal/content/atoms/develop-close-push-git-container.md:13-27].
- `export` repeats repo URL fetch, commit, first push, later push, deploy-tool-owned git shape, error handling, and optional strategy persistence [internal/content/atoms/export.md:52-59], [internal/content/atoms/export.md:174-214].
- `strategy-push-git-push-local` has the local variant: user credentials, repo and origin preflight, first push, auth warnings, and container differences [internal/content/atoms/strategy-push-git-push-local.md:13-53].
- `develop-close-push-git-local` repeats local commit/push and fallback to strategy setup [internal/content/atoms/develop-close-push-git-local.md:13-32].
- `develop-platform-rules-local` repeats local git-push preflight and user-owned repo constraints [internal/content/atoms/develop-platform-rules-local.md:58-68].

Canonical home and reasoning:

- Canonical homes are `strategy-push-git-push-container` for container push mechanics [internal/content/atoms/strategy-push-git-push-container.md:11-55] and `strategy-push-git-push-local` for local push mechanics [internal/content/atoms/strategy-push-git-push-local.md:11-53].
- `develop-push-git-deploy` should become a short develop-loop pointer because it currently restates setup material while the strategy atoms are already split by environment [internal/content/atoms/develop-push-git-deploy.md:14-37].
- `export` should keep export-specific tasks, but push mechanics can point to the strategy atom because the deploy-tool-owned git shape and GIT_TOKEN errors are identical to the push setup surface [internal/content/atoms/export.md:186-214].

Non-canonical list with byte estimates:

- `develop-push-git-deploy` [internal/content/atoms/develop-push-git-deploy.md:14-37] — 330 B.
- `develop-close-push-git-container` [internal/content/atoms/develop-close-push-git-container.md:13-27] — 150 B.
- `export` [internal/content/atoms/export.md:188-214] — 180 B.
- `develop-close-push-git-local` [internal/content/atoms/develop-close-push-git-local.md:13-32] — 100 B.

Cluster axis: strategy-setup versus develop-active versus export-active.

Risk: Medium. `export` has phase-specific task sequencing and must not lose its export task ordering [internal/content/atoms/export.md:25-39].

### 2. SSHFS mount/path semantics — 650 B recoverable

Atoms carrying it:

- `develop-platform-rules-container` states the canonical mount path, direct file-tool usage, SSH caveat, one-shot SSH command examples, and mount recovery [internal/content/atoms/develop-platform-rules-container.md:13-37].
- `develop-first-deploy-write-app` repeats empty mount, direct write, and command-over-SSH guidance [internal/content/atoms/develop-first-deploy-write-app.md:13-15], [internal/content/atoms/develop-first-deploy-write-app.md:43-51].
- `develop-push-dev-workflow-dev` repeats edit-on-mount [internal/content/atoms/develop-push-dev-workflow-dev.md:16-17].
- `develop-push-dev-workflow-simple` repeats edit-on-mount [internal/content/atoms/develop-push-dev-workflow-simple.md:14-15].
- `develop-push-dev-deploy-container` repeats deploy-from-mount [internal/content/atoms/develop-push-dev-deploy-container.md:15-19].
- `develop-http-diagnostic` repeats read framework logs directly from mount instead of SSH tail [internal/content/atoms/develop-http-diagnostic.md:28-31].
- `develop-first-deploy-intro` repeats empty SSHFS before first deploy [internal/content/atoms/develop-first-deploy-intro.md:32-33].

Canonical home and reasoning:

- `develop-platform-rules-container` is the broadest environment-level home. It already owns SSHFS mount path, direct file operations, SSH caveats, command examples, and mount recovery [internal/content/atoms/develop-platform-rules-container.md:13-37].

Non-canonical list with byte estimates:

- `develop-first-deploy-write-app` [internal/content/atoms/develop-first-deploy-write-app.md:43-51] — 270 B.
- `develop-push-dev-workflow-dev` [internal/content/atoms/develop-push-dev-workflow-dev.md:16-17] — 70 B.
- `develop-push-dev-workflow-simple` [internal/content/atoms/develop-push-dev-workflow-simple.md:14-15] — 80 B.
- `develop-push-dev-deploy-container` [internal/content/atoms/develop-push-dev-deploy-container.md:15-19] — 120 B.
- `develop-http-diagnostic` [internal/content/atoms/develop-http-diagnostic.md:28-31] — 80 B.
- `develop-first-deploy-intro` [internal/content/atoms/develop-first-deploy-intro.md:32-33] — 30 B.

Cluster axis: container platform rule versus first-deploy, push-dev, deploy, and diagnostic surfaces.

Risk: Low to medium. Some one-line local mentions are needed where the next action depends on mount availability, especially before first deploy [internal/content/atoms/develop-first-deploy-intro.md:32-33].

### 3. `zerops_dev_server` action and response shape — 520 B recoverable

Atoms carrying it:

- `develop-dynamic-runtime-start-container` owns start, status, restart, logs, stop, parameters, response fields, and redeploy behavior [internal/content/atoms/develop-dynamic-runtime-start-container.md:18-68].
- `develop-platform-rules-container` repeats response fields and warns against hand-rolled background SSH [internal/content/atoms/develop-platform-rules-container.md:20-26].
- `develop-close-push-dev-dev` repeats start command, status-first behavior, response fields, and no-HTTP worker behavior [internal/content/atoms/develop-close-push-dev-dev.md:19-35].
- `develop-dev-server-triage` repeats status and start commands, response interpretation, and redeploy re-check [internal/content/atoms/develop-dev-server-triage.md:29-63].
- `develop-push-dev-workflow-dev` repeats restart, response fields, logs, and reason-code diagnosis [internal/content/atoms/develop-push-dev-workflow-dev.md:19-40].

Canonical home and reasoning:

- `develop-dynamic-runtime-start-container` is the topical atom and carries the widest command family plus fields [internal/content/atoms/develop-dynamic-runtime-start-container.md:18-68].
- `develop-dev-server-reason-codes` remains the reason-code table, referenced by canonical start guidance [internal/content/atoms/develop-dynamic-runtime-start-container.md:67-68] and by the current workflow atoms [internal/content/atoms/develop-push-dev-workflow-dev.md:23-24].

Non-canonical list with byte estimates:

- `develop-platform-rules-container` [internal/content/atoms/develop-platform-rules-container.md:20-26] — 130 B.
- `develop-close-push-dev-dev` [internal/content/atoms/develop-close-push-dev-dev.md:25-35] — 170 B.
- `develop-dev-server-triage` [internal/content/atoms/develop-dev-server-triage.md:31-63] — 140 B after keeping only triage-specific decision rows.
- `develop-push-dev-workflow-dev` [internal/content/atoms/develop-push-dev-workflow-dev.md:23-40] — 80 B.

Cluster axis: dynamic-runtime start versus close, triage, and iteration workflow.

Risk: Medium. Triage needs enough local command shape to be executable, so dedup should replace repeated field lists, not remove the status/start decision table [internal/content/atoms/develop-dev-server-triage.md:17-24], [internal/content/atoms/develop-dev-server-triage.md:39-47].

### 4. Local-mode topology and local runtime loop — 520 B recoverable

Atoms carrying it:

- `bootstrap-discover-local` defines local topology: standard creates stage plus managed services, simple creates one service, dev/managed-only creates managed services only; no `{name}dev` service on Zerops [internal/content/atoms/bootstrap-discover-local.md:12-24].
- `bootstrap-provision-local` repeats local import shape, local-mode standard stage properties, no SSHFS, dotenv generation, and VPN guidance [internal/content/atoms/bootstrap-provision-local.md:12-41].
- `develop-local-workflow` repeats edit locally, use VPN, deploy when ready, no SSHFS, committed tree [internal/content/atoms/develop-local-workflow.md:11-20].
- `develop-dynamic-runtime-start-local` repeats local dev server runs on the user's machine, background task primitive, dotenv generation, VPN, and no `zerops_dev_server` [internal/content/atoms/develop-dynamic-runtime-start-local.md:13-67].
- `develop-platform-rules-local` repeats working-directory code, no SSHFS, local process handling, VPN, dotenv bridge, localhost health checks, zcli push, and git-push preflight [internal/content/atoms/develop-platform-rules-local.md:11-68].
- `develop-close-push-dev-local` repeats committed tree, no SSHFS, no dev container, and local+standard stage-only close [internal/content/atoms/develop-close-push-dev-local.md:14-23].
- `develop-push-dev-deploy-local` repeats zcli push from working directory, stage target, repo root, and no source service [internal/content/atoms/develop-push-dev-deploy-local.md:13-23].

Canonical home and reasoning:

- `bootstrap-discover-local` owns local topology during discovery [internal/content/atoms/bootstrap-discover-local.md:12-31].
- `develop-platform-rules-local` owns develop-time local environment rules [internal/content/atoms/develop-platform-rules-local.md:11-68].
- Dedup should merge repeated develop-time local rules into `develop-platform-rules-local` while preserving bootstrap/provision phase-specific actions.

Non-canonical list with byte estimates:

- `develop-local-workflow` [internal/content/atoms/develop-local-workflow.md:11-20] — 110 B.
- `develop-dynamic-runtime-start-local` [internal/content/atoms/develop-dynamic-runtime-start-local.md:54-67] — 160 B.
- `develop-close-push-dev-local` [internal/content/atoms/develop-close-push-dev-local.md:14-23] — 100 B.
- `develop-push-dev-deploy-local` [internal/content/atoms/develop-push-dev-deploy-local.md:13-23] — 150 B.

Cluster axis: bootstrap-local topology versus develop-local operating rules.

Risk: Medium. Bootstrap-local and develop-local fire in different phases, so phase-specific one-liners must remain [internal/content/atoms/bootstrap-provision-local.md:30-41].

### 5. `zerops_verify`-first cadence and browser escalation — 430 B recoverable

Atoms carrying it:

- `develop-verify-matrix` owns verify-every-service, non-web versus web-facing paths, `zerops_verify` first, browser agent escalation, and verdict protocol [internal/content/atoms/develop-verify-matrix.md:10-48].
- `develop-first-deploy-intro` repeats first-deploy verify as step 4 and envelope deployed flip after deploy plus passing verify [internal/content/atoms/develop-first-deploy-intro.md:24-30].
- `develop-first-deploy-verify` repeats verify result statuses, failed-check reading, and common first-deploy misconfigs [internal/content/atoms/develop-first-deploy-verify.md:13-31].
- `develop-http-diagnostic` repeats `zerops_verify` as canonical first diagnostic step [internal/content/atoms/develop-http-diagnostic.md:14-16], and says last-resort local curl usually duplicates verify [internal/content/atoms/develop-http-diagnostic.md:32-35].
- `develop-close-push-dev-standard` repeats verify after dev deploy and stage deploy [internal/content/atoms/develop-close-push-dev-standard.md:17-22].
- `develop-close-push-dev-simple` repeats deploy then verify [internal/content/atoms/develop-close-push-dev-simple.md:13-18].
- `develop-close-push-dev-local` repeats deploy then verify [internal/content/atoms/develop-close-push-dev-local.md:17-23].

Canonical home and reasoning:

- `develop-verify-matrix` is the broadest verification-selection atom and already distinguishes non-web from browser-backed web verification [internal/content/atoms/develop-verify-matrix.md:15-28].
- `develop-first-deploy-verify` should retain first-deploy-specific misconfigs [internal/content/atoms/develop-first-deploy-verify.md:20-31].

Non-canonical list with byte estimates:

- `develop-first-deploy-intro` [internal/content/atoms/develop-first-deploy-intro.md:28-30] — 70 B.
- `develop-http-diagnostic` [internal/content/atoms/develop-http-diagnostic.md:14-16], [internal/content/atoms/develop-http-diagnostic.md:32-35] — 120 B.
- `develop-close-push-dev-standard` [internal/content/atoms/develop-close-push-dev-standard.md:17-22] — 90 B.
- `develop-close-push-dev-simple` [internal/content/atoms/develop-close-push-dev-simple.md:13-18] — 60 B.
- `develop-close-push-dev-local` [internal/content/atoms/develop-close-push-dev-local.md:17-23] — 90 B.

Cluster axis: verify matrix versus first-deploy, diagnostic, and close surfaces.

Risk: Low. Close atoms need executable final command blocks; trim surrounding explanation rather than removing command order.

### 6. Standard-mode promotion and both-halves auto-close — 360 B recoverable

Atoms carrying it:

- `develop-first-deploy-promote-stage` owns first-deploy standard promotion, dev-to-stage cross deploy, no second build, and both-halves auto-close requirement [internal/content/atoms/develop-first-deploy-promote-stage.md:15-25].
- `develop-auto-close-semantics` owns auto-close condition and standard pairs requiring both halves [internal/content/atoms/develop-auto-close-semantics.md:13-27].
- `develop-close-push-dev-standard` repeats dev-first, start dev server, verify dev, promote stage, verify stage, no second build, and stage auto-start [internal/content/atoms/develop-close-push-dev-standard.md:14-31].
- `develop-checklist-dev-mode` repeats dev setup block versus stage setup block behavior [internal/content/atoms/develop-checklist-dev-mode.md:13-19].
- `bootstrap-mode-prompt` defines standard mode as dev + stage pair [internal/content/atoms/bootstrap-mode-prompt.md:18-24].
- `develop-mode-expansion` repeats adding a stage sibling and later dev-to-stage verification [internal/content/atoms/develop-mode-expansion.md:13-17], [internal/content/atoms/develop-mode-expansion.md:42-52].

Canonical home and reasoning:

- `develop-first-deploy-promote-stage` owns first deploy promotion mechanics [internal/content/atoms/develop-first-deploy-promote-stage.md:15-25].
- `develop-auto-close-semantics` owns closure semantics [internal/content/atoms/develop-auto-close-semantics.md:13-27].
- `develop-close-push-dev-standard` should keep only close-time command sequence plus one canonical link.

Non-canonical list with byte estimates:

- `develop-close-push-dev-standard` [internal/content/atoms/develop-close-push-dev-standard.md:25-31] — 120 B.
- `develop-checklist-dev-mode` [internal/content/atoms/develop-checklist-dev-mode.md:17-19] — 70 B.
- `develop-mode-expansion` [internal/content/atoms/develop-mode-expansion.md:51-52] — 50 B.
- `bootstrap-mode-prompt` [internal/content/atoms/bootstrap-mode-prompt.md:18-24] — 120 B, only if replaced with tighter mode definition.

Cluster axis: bootstrap mode definition, first-deploy promotion, close semantics.

Risk: Medium. The same terms carry different next actions in bootstrap, first-deploy, and close contexts.

### 7. Env-var catalog use and cross-service typo behavior — 340 B recoverable

Atoms carrying it:

- `develop-first-deploy-env-vars` owns no-guess catalog usage, `hostname` not `host`, syntax, deploy-time rewrite, typo behavior, and re-check command [internal/content/atoms/develop-first-deploy-env-vars.md:11-35].
- `bootstrap-env-var-discovery` owns discovery after provision and catalog table [internal/content/atoms/bootstrap-env-var-discovery.md:12-46].
- `develop-first-deploy-write-app` repeats catalog is authoritative and app should read OS env vars at startup [internal/content/atoms/develop-first-deploy-write-app.md:19-21].
- `develop-first-deploy-scaffold-yaml` repeats env var reference shape and points to first-deploy env vars [internal/content/atoms/develop-first-deploy-scaffold-yaml.md:31-37].
- `develop-first-deploy-verify` repeats env var name drift and `${hostname_KEY}` spelling [internal/content/atoms/develop-first-deploy-verify.md:24-29].
- `bootstrap-recipe-import` repeats managed env key summary for attestation [internal/content/atoms/bootstrap-recipe-import.md:44-48].

Canonical home and reasoning:

- `bootstrap-env-var-discovery` owns discovery and catalog contents [internal/content/atoms/bootstrap-env-var-discovery.md:25-41].
- `develop-first-deploy-env-vars` owns application wiring and typo behavior [internal/content/atoms/develop-first-deploy-env-vars.md:11-35].
- Non-canonical atoms should link to these two instead of restating syntax and no-guess wording.

Non-canonical list with byte estimates:

- `develop-first-deploy-write-app` [internal/content/atoms/develop-first-deploy-write-app.md:19-21] — 70 B.
- `develop-first-deploy-scaffold-yaml` [internal/content/atoms/develop-first-deploy-scaffold-yaml.md:31-37] — 90 B.
- `develop-first-deploy-verify` [internal/content/atoms/develop-first-deploy-verify.md:24-29] — 80 B.
- `bootstrap-recipe-import` [internal/content/atoms/bootstrap-recipe-import.md:44-48] — 60 B.
- `bootstrap-provision-local` [internal/content/atoms/bootstrap-provision-local.md:32-37] — 40 B.

Cluster axis: bootstrap discovery versus first-deploy wiring.

Risk: Low. Keep catalog table and first-deploy syntax separate; do not move the full managed-service catalog into develop atoms.

### 8. DeployFiles self-deploy versus cross-deploy semantics — 340 B recoverable

Atoms carrying it:

- `develop-deploy-modes` owns deploy class table, self-deploy `[.]` rule, cross-deploy artifact paths, and build-container filesystem timing [internal/content/atoms/develop-deploy-modes.md:10-39].
- `develop-deploy-files-self-deploy` owns the self-deploy failure mechanism and preflight rejection [internal/content/atoms/develop-deploy-files-self-deploy.md:10-29], plus cross-deploy contrast [internal/content/atoms/develop-deploy-files-self-deploy.md:31-37].
- `develop-push-dev-deploy-container` repeats self versus cross deploy command shapes and `deployFiles` discipline [internal/content/atoms/develop-push-dev-deploy-container.md:21-28].
- `develop-first-deploy-scaffold-yaml` repeats dev `[.]`, standard stage output dir, and content-root tilde handling [internal/content/atoms/develop-first-deploy-scaffold-yaml.md:41-54].
- `develop-change-drives-deploy` repeats `deployFiles` persistence at a high level [internal/content/atoms/develop-change-drives-deploy.md:12-15].

Canonical home and reasoning:

- `develop-deploy-modes` is canonical for class selection and path semantics [internal/content/atoms/develop-deploy-modes.md:10-39].
- `develop-deploy-files-self-deploy` remains canonical for the destructive self-deploy invariant [internal/content/atoms/develop-deploy-files-self-deploy.md:10-29].
- `develop-push-dev-deploy-container` should keep command examples only.

Non-canonical list with byte estimates:

- `develop-push-dev-deploy-container` [internal/content/atoms/develop-push-dev-deploy-container.md:24-28] — 100 B.
- `develop-first-deploy-scaffold-yaml` [internal/content/atoms/develop-first-deploy-scaffold-yaml.md:41-54] — 160 B.
- `develop-change-drives-deploy` [internal/content/atoms/develop-change-drives-deploy.md:12-15] — 80 B.

Cluster axis: deploy concept versus first-deploy scaffold and strategy execution.

Risk: Medium. Scaffold still needs enough local guidance to choose `deployFiles` while writing YAML.

### 9. First-deploy scaffold/write/deploy/verify flow — 300 B recoverable

Atoms carrying it:

- `develop-first-deploy-intro` owns the high-level first-deploy flow: scaffold `zerops.yaml`, write real app, deploy with default strategy, verify, and envelope deployed flip [internal/content/atoms/develop-first-deploy-intro.md:13-33].
- `develop-first-deploy-scaffold-yaml` owns YAML shape and mode-aware tips [internal/content/atoms/develop-first-deploy-scaffold-yaml.md:13-56].
- `develop-first-deploy-write-app` owns application-code checklist [internal/content/atoms/develop-first-deploy-write-app.md:13-59].
- `develop-first-deploy-execute` owns deploy execution and non-success log reading [internal/content/atoms/develop-first-deploy-execute.md:14-23].
- `develop-first-deploy-verify` owns verify status and first-deploy misconfig handling [internal/content/atoms/develop-first-deploy-verify.md:13-33].
- `bootstrap-close` repeats the same develop handoff sequence after bootstrap close [internal/content/atoms/bootstrap-close.md:28-36].

Canonical home and reasoning:

- `develop-first-deploy-intro` is canonical for the overall sequence [internal/content/atoms/develop-first-deploy-intro.md:17-30].
- The sub-atoms should own detailed step bodies: scaffold [internal/content/atoms/develop-first-deploy-scaffold-yaml.md:13-56], write [internal/content/atoms/develop-first-deploy-write-app.md:17-59], execute [internal/content/atoms/develop-first-deploy-execute.md:14-23], verify [internal/content/atoms/develop-first-deploy-verify.md:13-33].
- `bootstrap-close` should point to develop first-deploy rather than restating all four steps [internal/content/atoms/bootstrap-close.md:28-36].

Non-canonical list with byte estimates:

- `bootstrap-close` [internal/content/atoms/bootstrap-close.md:28-36] — 180 B.
- `develop-first-deploy-intro` [internal/content/atoms/develop-first-deploy-intro.md:19-30] — 120 B if converted to terse index plus sub-atom pointers.

Cluster axis: bootstrap handoff versus develop first-deploy branch.

Risk: Low. This is mostly an outline duplication, not a conflicting behavior.

### 10. Local git-push preflight and credential split — 290 B recoverable

Atoms carrying it:

- `strategy-push-git-push-local` owns local credential model, repo/origin checks, first push, auth warnings, no local credential management, and uncommitted changes warning [internal/content/atoms/strategy-push-git-push-local.md:13-53].
- `develop-close-push-git-local` repeats local credentials, commit, deploy, preflight refusal, and fallback setup [internal/content/atoms/develop-close-push-git-local.md:13-32].
- `develop-platform-rules-local` repeats git-push needs a user-owned repo with at least one commit and says ZCP does not initialize git in the user’s working directory [internal/content/atoms/develop-platform-rules-local.md:58-68].
- `develop-strategy-review` summarizes local push-git as user git credentials [internal/content/atoms/develop-strategy-review.md:18-20].

Canonical home and reasoning:

- `strategy-push-git-push-local` is canonical because it is environment-specific strategy setup and has the complete credential/preflight story [internal/content/atoms/strategy-push-git-push-local.md:11-53].
- Local platform rules should keep only the “needs committed repo” reminder because it is an environment pitfall [internal/content/atoms/develop-platform-rules-local.md:58-68].

Non-canonical list with byte estimates:

- `develop-close-push-git-local` [internal/content/atoms/develop-close-push-git-local.md:13-32] — 150 B.
- `develop-platform-rules-local` [internal/content/atoms/develop-platform-rules-local.md:58-68] — 100 B.
- `develop-strategy-review` [internal/content/atoms/develop-strategy-review.md:18-20] — 40 B.

Cluster axis: local platform rules versus push-git setup.

Risk: Low.

### 11. Agent-browser availability versus verify-agent protocol — 90 B recoverable

Atoms carrying it:

- `develop-verify-matrix` owns browser verification protocol and verdict handling [internal/content/atoms/develop-verify-matrix.md:24-48].
- `develop-platform-rules-container` says `agent-browser.dev` is available and should be used to verify web apps [internal/content/atoms/develop-platform-rules-container.md:38-39].

Canonical home and reasoning:

- `develop-verify-matrix` is canonical for verification behavior because it sequences `zerops_verify`, browser verification, and verdict handling [internal/content/atoms/develop-verify-matrix.md:24-48].
- `develop-platform-rules-container` should retain at most an availability pointer.

Non-canonical list with byte estimates:

- `develop-platform-rules-container` [internal/content/atoms/develop-platform-rules-container.md:38-39] — 90 B.

Cluster axis: platform environment affordance versus verify protocol.

Risk: Low.

### 12. Manual strategy and “ZCP stays out of deploy loop” repeats — 70 B recoverable

Atoms carrying it:

- `develop-close-manual` owns manual strategy closure rule: user controls deploy timing and ZCP should not suggest deploy tools after manual is selected [internal/content/atoms/develop-close-manual.md:12-15].
- `develop-manual-deploy` owns ad-hoc manual deployment commands and states the user controls deploy timing [internal/content/atoms/develop-manual-deploy.md:12-20].
- `develop-strategy-review` summarizes manual as user-orchestrated and ZCP staying out of the loop [internal/content/atoms/develop-strategy-review.md:22-22].

Canonical home and reasoning:

- `develop-close-manual` is canonical for close behavior [internal/content/atoms/develop-close-manual.md:12-15].
- `develop-manual-deploy` is canonical for explicit manual deployment commands [internal/content/atoms/develop-manual-deploy.md:12-20].

Non-canonical list with byte estimates:

- `develop-strategy-review` [internal/content/atoms/develop-strategy-review.md:22-22] — 40 B.
- `develop-manual-deploy` [internal/content/atoms/develop-manual-deploy.md:12-12] — 30 B if shortened.

Cluster axis: strategy review versus manual deployment.

Risk: Low.

## 3. Competing-Next-Action Class

### Conflict 1. Code-only edits: restart-only versus deploy-required

Conflict description:

- `develop-change-drives-deploy` states the general loop as “edit → deploy via active strategy → verify” [internal/content/atoms/develop-change-drives-deploy.md:12-18].
- `develop-push-dev-workflow-dev` states that for dev-mode container code-only changes, `zerops_dev_server action=restart` is enough and no redeploy is needed [internal/content/atoms/develop-push-dev-workflow-dev.md:16-30].
- `develop-push-dev-workflow-simple` states simple mode deploys after each set of changes because the container auto-starts with `healthCheck` [internal/content/atoms/develop-push-dev-workflow-simple.md:14-19].
- `develop-dev-server-triage` states that a running server with 5xx should be diagnosed and code edited, then deployed [internal/content/atoms/develop-dev-server-triage.md:45-47].

Atoms involved:

- `develop-change-drives-deploy` [internal/content/atoms/develop-change-drives-deploy.md:12-18].
- `develop-push-dev-workflow-dev` [internal/content/atoms/develop-push-dev-workflow-dev.md:16-30].
- `develop-push-dev-workflow-simple` [internal/content/atoms/develop-push-dev-workflow-simple.md:14-19].
- `develop-dev-server-triage` [internal/content/atoms/develop-dev-server-triage.md:45-47].

Canonical resolution:

- `develop-push-dev-workflow-dev` should explicitly own the dev-mode dynamic container exception: mounted code changes can be picked up by `zerops_dev_server action=restart` without deploy [internal/content/atoms/develop-push-dev-workflow-dev.md:16-30].
- `develop-change-drives-deploy` should narrow its general rule to closure/durable deploy semantics and link to mode-specific iteration atoms, because its current blanket wording conflicts with dev-mode restart-only iteration [internal/content/atoms/develop-change-drives-deploy.md:12-18].
- `develop-push-dev-workflow-simple` remains correct for simple mode because it says deploy is the post-edit action [internal/content/atoms/develop-push-dev-workflow-simple.md:14-19].

Recoverable bytes: 260 B.

### Conflict 2. Push-git stage behavior: direct stage cross-deploy versus trigger-dependent downstream build

Conflict description:

- `develop-push-git-deploy` says subsequent deploys push to the remote, then if CI/CD is configured the build triggers automatically, otherwise deploy to stage manually [internal/content/atoms/develop-push-git-deploy.md:24-35].
- `strategy-push-git-intro` says push-git has two concerns: pushing code and what happens after the push, and says a service without downstream trigger is functionally manual [internal/content/atoms/strategy-push-git-intro.md:12-27].
- `strategy-push-git-trigger-webhook` says every push to the branch triggers a Zerops build automatically [internal/content/atoms/strategy-push-git-trigger-webhook.md:54-62].
- `strategy-push-git-trigger-actions` says the repo’s CI runs `zcli push` back to Zerops and the first push may trigger both Zerops git-push and Actions [internal/content/atoms/strategy-push-git-trigger-actions.md:12-13], [internal/content/atoms/strategy-push-git-trigger-actions.md:85-92].
- `develop-close-push-git-container` and `develop-close-push-git-local` only say commit and run `zerops_deploy strategy="git-push"` or go to setup if missing [internal/content/atoms/develop-close-push-git-container.md:13-27], [internal/content/atoms/develop-close-push-git-local.md:13-32].

Atoms involved:

- `develop-push-git-deploy` [internal/content/atoms/develop-push-git-deploy.md:24-35].
- `strategy-push-git-intro` [internal/content/atoms/strategy-push-git-intro.md:12-27].
- `strategy-push-git-trigger-webhook` [internal/content/atoms/strategy-push-git-trigger-webhook.md:54-62].
- `strategy-push-git-trigger-actions` [internal/content/atoms/strategy-push-git-trigger-actions.md:85-92].
- `develop-close-push-git-container` [internal/content/atoms/develop-close-push-git-container.md:13-27].
- `develop-close-push-git-local` [internal/content/atoms/develop-close-push-git-local.md:13-32].

Canonical resolution:

- `strategy-push-git-intro` should own trigger selection and the rule that push-git without downstream trigger is manual-like [internal/content/atoms/strategy-push-git-intro.md:12-27].
- `develop-push-git-deploy` should be reduced to “push code, then follow the configured trigger atom or manual fallback,” because it currently embeds both webhook/actions and manual stage behavior [internal/content/atoms/develop-push-git-deploy.md:24-35].
- Trigger-specific atoms remain canonical for what happens after push: webhook [internal/content/atoms/strategy-push-git-trigger-webhook.md:54-62] and Actions [internal/content/atoms/strategy-push-git-trigger-actions.md:85-92].

Recoverable bytes: 300 B.

### Conflict 3. Browser verification: direct `agent-browser` hint versus full verify-agent protocol

Conflict description:

- `develop-platform-rules-container` says `agent-browser.dev` is on the ZCP host and should be used to verify deployed web apps [internal/content/atoms/develop-platform-rules-container.md:38-39].
- `develop-verify-matrix` says web-facing services must run `zerops_verify` first, then spawn a verify agent that drives `agent-browser`, and it defines verdict handling [internal/content/atoms/develop-verify-matrix.md:24-48].

Atoms involved:

- `develop-platform-rules-container` [internal/content/atoms/develop-platform-rules-container.md:38-39].
- `develop-verify-matrix` [internal/content/atoms/develop-verify-matrix.md:24-48].

Canonical resolution:

- `develop-verify-matrix` should own all browser verification behavior [internal/content/atoms/develop-verify-matrix.md:24-48].
- `develop-platform-rules-container` should keep only an availability pointer or drop the browser line entirely if verify-matrix co-fires where needed [internal/content/atoms/develop-platform-rules-container.md:38-39].

Recoverable bytes: 90 B.

## 4. Total Recoverable Bytes Summary

| Bucket | Candidate count | Recoverable bytes | Notes |
|---|---:|---:|---|
| §4.3 reverified candidates with direct dedup | 5 | 2,050 B | SSHFS 650 B; env-ref syntax 380 B; deploy-new-container 410 B; dev-server shape 520 B; agent-browser 90 B. |
| §4.3 already deduped or axis-justified | 3 | 120 B | Managed env catalog is axis-justified at 0 B; standard pair has only 120 B of safe trim; sudo/prepareCommands is already deduped. |
| Pass 2 new ranked candidates, excluding overlaps already counted | 7 | 3,050 B | Push-git mechanics 760 B; local-mode topology 520 B; verify cadence 430 B; deployFiles class semantics 340 B; first-deploy outline 300 B; local git-push preflight 290 B; standard-pair residual 360 B; manual residual 70 B. |
| Competing-next-action conflict cleanup | 3 | 650 B | Restart-versus-deploy 260 B; push-git downstream trigger 300 B; browser protocol 90 B. |
| Gross total before overlap correction | 18 | 5,870 B | Sums all row-level opportunities. |
| Overlap correction | 4 | -150 B | Agent-browser is counted in §4.3 and conflict cleanup; standard-pair residual overlaps pass-2 and §4.3; verify cadence overlaps browser conflict. |
| Net Phase 2 recoverable estimate | 14 | 5,720 B | Within the planned Phase 2 target of 3-6 KB. |

## 5. Phase 2 Work Plan

1. Dedup push-git mechanics first: make `strategy-push-git-push-container` [internal/content/atoms/strategy-push-git-push-container.md:11-55] and `strategy-push-git-push-local` [internal/content/atoms/strategy-push-git-push-local.md:11-53] canonical; trim `develop-push-git-deploy` [internal/content/atoms/develop-push-git-deploy.md:14-37], close atoms [internal/content/atoms/develop-close-push-git-container.md:13-27], [internal/content/atoms/develop-close-push-git-local.md:13-32], and export push task [internal/content/atoms/export.md:186-214].

2. Resolve push-git downstream trigger conflict: keep trigger selection in `strategy-push-git-intro` [internal/content/atoms/strategy-push-git-intro.md:12-27], webhook behavior in `strategy-push-git-trigger-webhook` [internal/content/atoms/strategy-push-git-trigger-webhook.md:54-62], and Actions behavior in `strategy-push-git-trigger-actions` [internal/content/atoms/strategy-push-git-trigger-actions.md:85-92]; remove trigger decision prose from `develop-push-git-deploy` [internal/content/atoms/develop-push-git-deploy.md:32-35].

3. Dedup SSHFS mount/path semantics into `develop-platform-rules-container` [internal/content/atoms/develop-platform-rules-container.md:13-37]; trim repeated mount prose from first-deploy write [internal/content/atoms/develop-first-deploy-write-app.md:43-51], push-dev workflows [internal/content/atoms/develop-push-dev-workflow-dev.md:16-17], [internal/content/atoms/develop-push-dev-workflow-simple.md:14-15], deploy container [internal/content/atoms/develop-push-dev-deploy-container.md:15-19], and HTTP diagnostics [internal/content/atoms/develop-http-diagnostic.md:28-31].

4. Dedup `zerops_dev_server` command/field shape into `develop-dynamic-runtime-start-container` [internal/content/atoms/develop-dynamic-runtime-start-container.md:18-68]; trim repeated response field lists from platform rules [internal/content/atoms/develop-platform-rules-container.md:20-26], close dev [internal/content/atoms/develop-close-push-dev-dev.md:25-35], triage [internal/content/atoms/develop-dev-server-triage.md:31-63], and push-dev workflow [internal/content/atoms/develop-push-dev-workflow-dev.md:23-40].

5. Fix restart-only versus deploy-required wording: narrow `develop-change-drives-deploy` [internal/content/atoms/develop-change-drives-deploy.md:12-18] so it no longer contradicts dev-mode restart-only iteration [internal/content/atoms/develop-push-dev-workflow-dev.md:26-30] while preserving simple-mode deploy guidance [internal/content/atoms/develop-push-dev-workflow-simple.md:14-19].

6. Dedup local-mode develop rules into `develop-platform-rules-local` [internal/content/atoms/develop-platform-rules-local.md:11-68]; keep bootstrap topology in `bootstrap-discover-local` [internal/content/atoms/bootstrap-discover-local.md:12-31] and provision mechanics in `bootstrap-provision-local` [internal/content/atoms/bootstrap-provision-local.md:12-41].

7. Dedup verify cadence into `develop-verify-matrix` [internal/content/atoms/develop-verify-matrix.md:10-48]; keep first-deploy-specific misconfig diagnosis in `develop-first-deploy-verify` [internal/content/atoms/develop-first-deploy-verify.md:20-31] and trim generic verify repetition from close atoms [internal/content/atoms/develop-close-push-dev-standard.md:17-22], [internal/content/atoms/develop-close-push-dev-simple.md:13-18], [internal/content/atoms/develop-close-push-dev-local.md:17-23].

8. Dedup env-ref syntax: keep discovery catalog in `bootstrap-env-var-discovery` [internal/content/atoms/bootstrap-env-var-discovery.md:25-41] and first-deploy wiring in `develop-first-deploy-env-vars` [internal/content/atoms/develop-first-deploy-env-vars.md:11-35]; trim syntax repeats from scaffold [internal/content/atoms/develop-first-deploy-scaffold-yaml.md:31-37], verify [internal/content/atoms/develop-first-deploy-verify.md:24-29], and write-app [internal/content/atoms/develop-first-deploy-write-app.md:19-21].

9. Dedup deployFiles class semantics: keep class table and path timing in `develop-deploy-modes` [internal/content/atoms/develop-deploy-modes.md:10-39], destructive self-deploy mechanism in `develop-deploy-files-self-deploy` [internal/content/atoms/develop-deploy-files-self-deploy.md:10-29], and trim `develop-push-dev-deploy-container` [internal/content/atoms/develop-push-dev-deploy-container.md:24-28] plus scaffold tips [internal/content/atoms/develop-first-deploy-scaffold-yaml.md:41-54].

10. Tighten standard-mode pair repetition: keep promotion in `develop-first-deploy-promote-stage` [internal/content/atoms/develop-first-deploy-promote-stage.md:15-25] and close semantics in `develop-auto-close-semantics` [internal/content/atoms/develop-auto-close-semantics.md:13-27]; trim `develop-close-push-dev-standard` explanation after its command block [internal/content/atoms/develop-close-push-dev-standard.md:25-31].

## 6. Risks + Watch Items

- Axis separation is the main risk. Bootstrap topology, develop iteration, close, export, and strategy setup often need the same noun but not the same next action; examples include local-mode bootstrap [internal/content/atoms/bootstrap-discover-local.md:12-31] versus local-mode develop rules [internal/content/atoms/develop-platform-rules-local.md:11-68].

- Do not collapse `bootstrap-env-var-discovery` into first-deploy env wiring. The catalog table is bootstrap/provision evidence [internal/content/atoms/bootstrap-env-var-discovery.md:25-41], while `develop-first-deploy-env-vars` teaches app wiring and deploy-time typo behavior [internal/content/atoms/develop-first-deploy-env-vars.md:18-30].

- Do not remove executable command blocks from close atoms unless the canonical atom definitely co-fires. Close atoms currently provide final deploy and verify sequences for standard [internal/content/atoms/develop-close-push-dev-standard.md:17-22], simple [internal/content/atoms/develop-close-push-dev-simple.md:15-17], and local [internal/content/atoms/develop-close-push-dev-local.md:17-19].

- Treat `develop-dev-server-triage` carefully. It duplicates `zerops_dev_server` commands, but its value is the expectation and response decision matrix [internal/content/atoms/develop-dev-server-triage.md:17-24], [internal/content/atoms/develop-dev-server-triage.md:39-47].

- Push-git has two axes: environment-specific push mechanics and downstream trigger selection. Container push mechanics belong in `strategy-push-git-push-container` [internal/content/atoms/strategy-push-git-push-container.md:13-55], local push mechanics in `strategy-push-git-push-local` [internal/content/atoms/strategy-push-git-push-local.md:13-53], and trigger selection in `strategy-push-git-intro` [internal/content/atoms/strategy-push-git-intro.md:12-27].

- Export is a special surface. It repeats push-git mechanics, but it also has an ordered task list that should stay self-contained enough for export execution [internal/content/atoms/export.md:25-39].

- Browser verification should not become a bare `agent-browser` hint. The canonical behavior requires `zerops_verify` first, then browser-agent verification for web-facing services [internal/content/atoms/develop-verify-matrix.md:24-38].

- Standard-mode pair guidance is intentionally repeated across mode selection, first-deploy promotion, close semantics, and mode expansion. Only trim explanatory duplication; preserve next-action differences in bootstrap [internal/content/atoms/bootstrap-mode-prompt.md:18-24], first deploy [internal/content/atoms/develop-first-deploy-promote-stage.md:15-25], close [internal/content/atoms/develop-close-push-dev-standard.md:14-31], and expansion [internal/content/atoms/develop-mode-expansion.md:13-17].

- The §4.3 SSHFS count changed because broad grep hits include command examples and local-mode contrasts. The true dedup target is 7 restatements of mount semantics; other `/var/www` hits are task-local commands or export-specific operations [internal/content/atoms/export.md:52-59], [internal/content/atoms/export.md:174-181].

- The `sudo apk add` / `sudo apt-get install` candidate is already deduped. The current direct rule lives only in `develop-platform-rules-common` [internal/content/atoms/develop-platform-rules-common.md:11-12], with prepareCommands placement in the same atom [internal/content/atoms/develop-platform-rules-common.md:18-21].
