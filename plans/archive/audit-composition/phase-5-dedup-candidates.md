# Phase 5 dedup candidates — broad-atom cross-cluster restatements (Phase 5)

Round: PRE-WORK / CORPUS-SCAN per §10.1 P5
Date: 2026-04-27
Reviewer: Codex
Plan: §5 Phase 5.1 + amendment 6 / Codex C6+C15 (G3 closure)

## Methodology

Read all 6 broad atoms in full. Skimmed the remaining 73 atoms for facts that also appear in the broad atoms. Per §6.1: a 'fact' is a load-bearing operational instruction; cross-atom duplication of mode-name lists, status-code tables, env-var rules, deploy semantics, etc., counts as a dedup candidate. Per-fact canonical home picked using §6.1 rule: lowest priority OR broadest axis OR topical owner.

## Cluster summary

| broad atom | total facts | facts also in 1+ other atom | est. recoverable bytes (cumulative across non-canonical drops) |
|---|---:|---:|---:|
| develop-api-error-meta | 5 | 0 | 0 |
| develop-env-var-channels | 4 | 1 | 343 |
| develop-verify-matrix | 5 | 2 | 1,492 |
| develop-platform-rules-common | 6 | 5 | 1,994 |
| develop-auto-close-semantics | 5 | 3 | 2,014 |
| develop-change-drives-deploy | 3 | 3 | 1,741 |
| Additional atoms found (if any) | 19 | 10 | 5,189 |
| **Total** | 28 | 10 | 5,189 |

## Dedup candidates ranked by recoverable bytes

| fact-id | fact | atom-locations | canonical home | non-canonical edits | bytes recoverable | risk class |
|---|---|---|---|---|---:|---|
| F1 | A service/work session reaches the completed/deployed state only after a successful deploy plus a passing verify. | develop-auto-close-semantics (internal/content/atoms/develop-auto-close-semantics.md:13); develop-verify-matrix (internal/content/atoms/develop-verify-matrix.md:10); develop-first-deploy-verify (internal/content/atoms/develop-first-deploy-verify.md:13); develop-first-deploy-intro (internal/content/atoms/develop-first-deploy-intro.md:28); bootstrap-close (internal/content/atoms/bootstrap-close.md:30); develop-closed-auto (internal/content/atoms/develop-closed-auto.md:10) | develop-auto-close-semantics (topical owner for close/deployed completion semantics) | develop-first-deploy-verify: rephrase L13-18 to status/check interpretation and link auto-close; develop-first-deploy-intro: rephrase L28-30 to link `develop-verify-matrix`; bootstrap-close: rephrase L30-32 to "run first deploy + verify"; develop-closed-auto: drop/rephrase L10-13 to link `develop-auto-close-semantics` | 984 | HIGH |
| F2 | Service configuration changes such as shared storage, scaling, nginx fragments, and re-importing startWithoutCode changes are import-YAML changes applied with `zerops_import override=true`, not code deploys. | develop-platform-rules-common (internal/content/atoms/develop-platform-rules-common.md:26); develop-knowledge-pointers (internal/content/atoms/develop-knowledge-pointers.md:21); develop-ready-to-deploy (internal/content/atoms/develop-ready-to-deploy.md:28) | develop-platform-rules-common (broadest always-applicable platform rule) | develop-knowledge-pointers: rephrase L21-24 to a pointer to `develop-platform-rules-common`; develop-ready-to-deploy: keep READY_TO_DEPLOY recovery but drop repeated override/service-config explanation in L28-34 | 780 | MEDIUM |
| F3 | Explicit `zerops_workflow action="close" workflow="develop"` emits closed state but is rarely needed because starting a new task with a different intent replaces the session. | develop-auto-close-semantics (internal/content/atoms/develop-auto-close-semantics.md:20); develop-change-drives-deploy (internal/content/atoms/develop-change-drives-deploy.md:21); develop-closed-auto (internal/content/atoms/develop-closed-auto.md:15) | develop-auto-close-semantics (topical owner for close behavior) | develop-change-drives-deploy: drop L21-24 and keep only cross-link already in references; develop-closed-auto: rephrase L15-24 to next-action commands plus "full semantics: develop-auto-close-semantics" | 571 | MEDIUM |
| F4 | Web-facing services require `zerops_verify` for the baseline and browser/end-to-end verification through the agent-browser protocol; non-web services stop at `zerops_verify`. | develop-verify-matrix (internal/content/atoms/develop-verify-matrix.md:15); develop-platform-rules-container (internal/content/atoms/develop-platform-rules-container.md:37); develop-http-diagnostic (internal/content/atoms/develop-http-diagnostic.md:15); develop-implicit-webserver (internal/content/atoms/develop-implicit-webserver.md:28) | develop-verify-matrix (topical owner for verify matrix and verdict protocol) | develop-platform-rules-container: drop L37-38 or rephrase to availability-only pointer; develop-http-diagnostic: rephrase L15-17 to "start with `zerops_verify`; see develop-verify-matrix"; develop-implicit-webserver: rephrase L28-29 to link `develop-verify-matrix` | 508 | MEDIUM |
| F5 | Each deploy creates a new runtime container; local runtime files/processes disappear and only `deployFiles`-covered content persists. | develop-platform-rules-common (internal/content/atoms/develop-platform-rules-common.md:13); develop-change-drives-deploy (internal/content/atoms/develop-change-drives-deploy.md:12); develop-dynamic-runtime-start-container (internal/content/atoms/develop-dynamic-runtime-start-container.md:35); develop-close-push-dev-dev (internal/content/atoms/develop-close-push-dev-dev.md:25); develop-platform-rules-container (internal/content/atoms/develop-platform-rules-container.md:16) | develop-platform-rules-common (broadest always-applicable platform rule) | develop-change-drives-deploy: drop L12-13 and rely on existing cross-link; develop-dynamic-runtime-start-container: rephrase L35-37 to "after redeploy, start again; see common rules"; develop-close-push-dev-dev: rephrase L25-26 to "after redeploy"; develop-platform-rules-container: drop/rephrase L16 to avoid repeating the new-container invariant | 497 | HIGH |
| F6 | In dev-mode dynamic containers, code-only edits use `zerops_dev_server action=restart`; `zerops.yaml`/config changes require `zerops_deploy` first. | develop-change-drives-deploy (internal/content/atoms/develop-change-drives-deploy.md:15); develop-push-dev-workflow-dev (internal/content/atoms/develop-push-dev-workflow-dev.md:25); develop-dev-server-triage (internal/content/atoms/develop-dev-server-triage.md:42) | develop-push-dev-workflow-dev (topical owner for dev-mode push-dev iteration) | develop-change-drives-deploy: rephrase L15-17 to cross-link to `develop-push-dev-workflow-dev`; develop-dev-server-triage: rephrase L42-46 to "fix, then follow mode-specific cadence" without restating both commands | 494 | HIGH |
| F7 | Standard-mode pairs require both dev and stage halves to be deployed/verified; skipping stage leaves the session active. | develop-auto-close-semantics (internal/content/atoms/develop-auto-close-semantics.md:24); develop-first-deploy-promote-stage (internal/content/atoms/develop-first-deploy-promote-stage.md:23); develop-close-push-dev-standard (internal/content/atoms/develop-close-push-dev-standard.md:29) | develop-auto-close-semantics (topical owner for close criteria) | develop-first-deploy-promote-stage: keep command block but rephrase L23-26 to link close semantics; develop-close-push-dev-standard: rephrase L29-32 to link close semantics after command block | 459 | MEDIUM |
| F8 | `zerops.yaml` must live at the repository root before deploy; setup names are recipe/setup keys rather than hostnames. | develop-platform-rules-common (internal/content/atoms/develop-platform-rules-common.md:15); develop-first-deploy-scaffold-yaml (internal/content/atoms/develop-first-deploy-scaffold-yaml.md:13); develop-push-dev-deploy-local (internal/content/atoms/develop-push-dev-deploy-local.md:13) | develop-platform-rules-common (broadest always-applicable platform rule) | develop-first-deploy-scaffold-yaml: rephrase L13-15 to "scaffold before deploy; see common rules for root/setup semantics"; develop-push-dev-deploy-local: rephrase L13-15 to avoid restating root requirement | 374 | MEDIUM |
| F9 | `envVariables` in `zerops.yaml` are declarative and do not become live until a deploy; reload does not pick up `run.envVariables`, and build vars affect only the next build. | develop-env-var-channels (internal/content/atoms/develop-env-var-channels.md:14); develop-platform-rules-common (internal/content/atoms/develop-platform-rules-common.md:22); develop-push-dev-workflow-simple (internal/content/atoms/develop-push-dev-workflow-simple.md:23) | develop-env-var-channels (topical owner for env-var live channels) | develop-platform-rules-common: rephrase L22-25 to a one-line cross-link to `develop-env-var-channels`; develop-push-dev-workflow-simple: drop L23-24 or replace with "config-only changes still deploy; env timing in develop-env-var-channels" | 343 | MEDIUM |
| F10 | Simple, standard, local, and first-deploy changes generally require `zerops_deploy`; the restart-only exception is dev-mode dynamic. | develop-change-drives-deploy (internal/content/atoms/develop-change-drives-deploy.md:18); develop-push-dev-workflow-simple (internal/content/atoms/develop-push-dev-workflow-simple.md:15); develop-close-push-dev-simple (internal/content/atoms/develop-close-push-dev-simple.md:13); develop-close-push-dev-local (internal/content/atoms/develop-close-push-dev-local.md:14) | develop-change-drives-deploy (broadest cadence overview) | develop-push-dev-workflow-simple: keep command block but rephrase L15-16; develop-close-push-dev-simple: keep command block and drop/rephrase L13; develop-close-push-dev-local: keep local command block and rephrase L14-15 | 179 | MEDIUM |

## Top fixtures impacted

The top candidates hit all four first-deploy fixtures because `develop-first-deploy-intro`, `develop-first-deploy-verify`, `develop-platform-rules-common`, `develop-verify-matrix`, `develop-auto-close-semantics`, and `develop-change-drives-deploy` are present in the first-deploy render paths. F1, F4, F5, F8, and F9 visibly reduce first-deploy text volume without removing the phase-specific scaffold/deploy/verify command atoms.

The simple-deployed fixture is mainly hit by F3, F5, F6, F9, and F10 through `develop-change-drives-deploy`, `develop-platform-rules-common`, `develop-env-var-channels`, `develop-push-dev-workflow-simple`, and close/simple workflow atoms. The highest-value simple-deployed path is not deleting close command blocks; it is trimming repeated explanation around deploy cadence, env-var liveness, and close semantics while retaining one executable sequence.

## Phase 5 work-unit derivation

ONE COMMIT PER FACT (not per atom). Suggested execution order smallest blast radius first:
1. F10: small cadence wording cleanup; preserves command blocks.
2. F9: env-var liveness has a clear topical owner and low semantic ambiguity.
3. F8: repo-root/setup-name reminder can become pointers without touching behavior.
4. F4: browser/verify protocol dedup; keep HTTP-diagnostic ordering intact.
5. F7: standard-pair close semantics; command blocks remain local.
6. F3: explicit-close semantics; touches broad close/session wording.
7. F6: dev restart-vs-deploy cadence; high-value simple-deployed cleanup but verify rendered output carefully.
8. F5: deploy-new-container persistence; broad atom edit touching several call sites.
9. F2: import override/service-config split; READY_TO_DEPLOY recovery must remain precise.
10. F1: largest recovery and widest fixture impact; do last after lower-risk cross-links prove out.

Additional atoms beyond the 6 that restate facts from the broad atoms and should be in the Phase 5 work-unit list: `develop-first-deploy-verify` (completion/verify status), `develop-first-deploy-intro` (completion after verify), `bootstrap-close` (first-deploy handoff completion), `develop-closed-auto` (close-state reason and next actions), `develop-knowledge-pointers` (import override), `develop-ready-to-deploy` (override recovery), `develop-platform-rules-container` (new-container/browser verification), `develop-http-diagnostic` (verify-first), `develop-implicit-webserver` (verify path), `develop-dynamic-runtime-start-container` (new-container restart), `develop-close-push-dev-dev` (new-container restart), `develop-push-dev-workflow-dev` (dev restart cadence), `develop-dev-server-triage` (mode-specific cadence), `develop-first-deploy-promote-stage` (both halves close), `develop-close-push-dev-standard` (both halves close), `develop-first-deploy-scaffold-yaml` (repo-root rule), `develop-push-dev-deploy-local` (repo-root rule), `develop-push-dev-workflow-simple` (env/cadence), `develop-close-push-dev-simple` and `develop-close-push-dev-local` (cadence command framing).

## Methodology footnotes

- All file:line citations are from actual file reads. Executor must grep-verify before applying.
- Conservative: if a fact in 2 atoms has axis-justified framing in each, KEEP both with rationale.

## Codex apply log

Execution order: F9, F8, F4, F7, F3, F6, F5, F2, F1.

- F9: APPLIED — env-var liveness duplicate replaced with cross-links to `develop-env-var-channels`; verify gate GREEN.
- F8: APPLIED — repo-root/setup-name duplicate replaced with cross-links to `develop-platform-rules-common`; verify gate GREEN.
- F4: APPLIED — web verify/browser duplicate replaced with cross-links to `develop-verify-matrix`; verify gate GREEN.
- F7: APPLIED — standard-pair close duplicate replaced with cross-links to `develop-auto-close-semantics`; verify gate GREEN.
- F3: APPLIED — explicit close duplicate replaced with cross-links to `develop-auto-close-semantics`; verify gate GREEN.
- F6: APPLIED — dev restart-vs-deploy duplicate replaced with cross-links to `develop-push-dev-workflow-dev` / `develop-change-drives-deploy`; verify gate GREEN.
- F5: SKIPPED-WITH-RATIONALE — initial edit failed `TestCorpusCoverage_RoundTrip` because fixture `develop_push_dev_dev_container` still requires load-bearing phrase "persistence boundary"; reverted F5 edits only, then verify gate GREEN.
- F2: APPLIED — broad infrastructure-change duplicate in `develop-knowledge-pointers` replaced with pointer to `develop-platform-rules-common`; READY_TO_DEPLOY recovery text kept to preserve its recovery signal; verify gate GREEN.
- F1: APPLIED — completion/auto-close duplicate replaced with cross-links to `develop-auto-close-semantics`; verify gate GREEN.

Signal audit trail:

- F4 preserved `Do not SSH in to start a server` in `develop-implicit-webserver` verbatim while rephrasing only the verify step.
- F4 preserved `do **not** default to ssh {hostname} curl localhost` in `develop-http-diagnostic` verbatim while rephrasing the first `zerops_verify` item.
- F5 preserved `Mount caveats` and `Never ssh <hostname> cat/ls/tail ... for mount files` when attempting the edit; F5 was then reverted after the coverage failure.
- F6 preserved `do NOT restart` in `develop-dev-server-triage` verbatim while trimming the cadence duplicate.
- F2 preserved the READY_TO_DEPLOY recovery guidance with `startWithoutCode: true`, `override=true`, and `zerops_import content="<yaml>" override=true`.
