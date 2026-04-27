# Composition re-score — post-followup (Phase 5.2 / Phase 7 prep)

Round: CORPUS-SCAN composition cross-validation per §10.1 P5+P7
Date: 2026-04-27
Reviewer: Codex
Plan: §5 Phase 5.2 + §15.3 G3 (post-Phase-0 amendment 6 / Codex C6+C15)
Rubric: plans/atom-corpus-hygiene-2026-04-26.md §6.2 (1-5 scale)

## Score table (post-followup, 5 fixtures)

| Fixture | Coherence | Density | Redundancy | Coverage-gap | Task-relevance |
|---|---|---|---|---|---|
| standard | 4 | 3 | 2 | 4 | 4 |
| implicit-webserver | 3 | 3 | 2 | 3 | 4 |
| two-pair | 4 | 3 | 1 | 4 | 4 |
| single-service | 4 | 3 | 2 | 4 | 4 |
| simple-deployed | 4 | 3 | 3 | 5 | 4 |

## Δ vs §4.2 baseline (Phase 0 baseline)

| Fixture | Coherence Δ | Density Δ | Redundancy Δ | Coverage-gap Δ | Task-relevance Δ |
|---|---|---|---|---|---|
| standard | +3 | +0 | +1 | +1 | +1 |
| implicit-webserver | +2 | +0 | +1 | +1 | +1 |
| two-pair | +3 | +1 | +0 | +2 | +1 |
| single-service | +3 | +0 | +1 | +1 | +1 |
| simple-deployed | +2 | +0 | +1 | +1 | +0 |

## §15.3 G3 disposition (per-fixture)

| Fixture | redundancy G3 | coverage-gap G3 | overall G3 |
|---|---|---|---|
| standard | PASS | PASS | PASS |
| implicit-webserver | PASS | PASS | PASS |
| two-pair | FAIL | PASS | FAIL |
| single-service | PASS | PASS | PASS |
| simple-deployed | PASS | PASS | PASS |

## Per-fixture qualitative justification

### develop_first_deploy_standard_container
**Coherence = 4**: The main path now reads as one usable first-deploy sequence: scaffold, write, deploy `appdev`, start/verify, promote to `appstage`, verify. The remaining awkward transitions are the two generic platform/deployFiles blocks interrupting the first-deploy narrative and the late container-specific Platform rules block after the first-deploy verdict.
**Density = 3**: The render contains many load-bearing instructions, but it is still around 24 KB and spends substantial space on defensive background, tables, and fallback diagnostics. This fits the 2.0-2.9 facts/KB anchor rather than the lean 3.0+ anchor.
**Redundancy = 2**: Restated facts: `develop-first-deploy-branch` + `develop-first-deploy-run` both say deploy before probing/inspection and verify after deploy; `develop-deploy-modes` + `develop-deploy-files-self-deploy` + `develop-first-deploy-scaffold-yaml` all restate self-deploy needs `deployFiles: [.]` or `[./]`; `develop-deploy-modes` + `develop-deploy-files-cross-deploy` + `develop-first-deploy-scaffold-yaml` restate cross-deploy uses build output such as `./out`; `develop-env-var-channels` + `develop-platform-rules-common` + `develop-first-deploy-scaffold-yaml` restate env-var live timing/cross-ref location; `develop-verify-matrix` + `develop-first-deploy-verify` + `develop-auto-close-semantics` restate deploy success must be followed by verify and close requires verified services.
**Coverage-gap = 4**: Supported: discover env keys with `zerops_discover includeEnvs=true`; scaffold `zerops.yaml`; write real app code; run `zerops_deploy targetService="appdev"` without `strategy`; start dynamic dev process via `zerops_dev_server`; verify `appdev`; promote with `zerops_deploy sourceService="appdev" targetService="appstage"`; verify `appstage`; inspect `apiMeta`, logs, and failed checks on errors; understand auto-close. Gap: the exact `zerops_dev_server action="start"` command is specified by arg family but not rendered as a concrete command using `appdev`, port, command, and healthPath.
**Task-relevance = 4**: About 18 of 23 atoms are directly relevant to the most-likely task, first deploying a standard dynamic app and promoting it to stage; generic `apiMeta`, env-var channels, knowledge-on-demand, and broad platform rules are partial rather than central.

### develop_first_deploy_implicit_webserver_standard
**Coherence = 3**: The first-deploy route is mostly readable, but the fixture has a visible strategy clash around background SSH: the generic container Platform rules warn against hand-rolled `ssh <hostname> "cmd &"` for long-running processes, while `develop-first-deploy-frontend-assets` tells the agent to start Vite with `nohup npm run dev ... &`. That does not break the deploy command itself, but it forces a mental reset for frontend-runtime handling.
**Density = 3**: The implicit-webserver-specific facts are useful, but the render remains about 26 KB with repeated deploy, env, diagnostic, and platform material. It remains acceptable but rewriteable.
**Redundancy = 2**: Restated facts: `develop-first-deploy-branch` + `develop-first-deploy-run` restate deploy-first/verify-next sequencing; `develop-deploy-modes` + `develop-deploy-files-self-deploy` + `develop-first-deploy-scaffold-yaml` restate self-deploy `deployFiles: [.]`; `develop-deploy-modes` + `develop-deploy-files-cross-deploy` + `develop-first-deploy-scaffold-yaml` restate cross-deploy build-output cherry-pick; `develop-env-var-channels` + `develop-first-deploy-env-vars` + `develop-first-deploy-scaffold-yaml` restate env-var channels/cross-service references; `develop-implicit-webserver-runtime` + `develop-first-deploy-frontend-assets` both describe implicit webserver serving behavior and asset placement consequences; `develop-verify-matrix` + `develop-first-deploy-verify` restate verify status interpretation.
**Coverage-gap = 3**: Supported: scaffold implicit-webserver YAML with omitted `run.start`/`run.ports` and `documentRoot`; write app files; deploy `appdev`; run frontend asset build before verify; verify `appdev`; promote to `appstage`; verify `appstage`; diagnose HTTP errors with verify/logs; parse `apiMeta`. Gap: start/avoid-start guidance for frontend dev server is competing because `develop-first-deploy-frontend-assets` recommends `ssh ... nohup npm run dev ... &` while `develop-platform-rules-container` says not to hand-roll background SSH for long-running dev processes, so the likely next-action "start HMR/dev server" lacks exactly one authoritative recommendation.
**Task-relevance = 4**: About 20 of 24 atoms are relevant or partially relevant to first deploying a `php-nginx` standard app; implicit webserver and frontend asset atoms are highly relevant, while generic env, knowledge, and platform atoms are partial.

### develop_first_deploy_two_runtime_pairs_standard
**Coherence = 4**: The output clearly covers both `appdev/appstage` and `apidev/apistage`; the per-service duplicate dynamic-runtime and promotion atoms are awkward but do not recommend incompatible tool calls. The main narrative remains usable.
**Density = 3**: The fixture has more operational facts than the baseline because both runtime pairs are named, but the repeated per-service sections and broad shared guidance keep it in the acceptable/rewriteable band.
**Redundancy = 1**: Restated facts: `develop-dynamic-runtime-start-container` renders twice with hostname substitution for the same dev-server action family; `develop-first-deploy-promote-stage` renders twice with the same dev-to-stage cross-deploy rule; `develop-first-deploy-branch` + `develop-first-deploy-run` both say deploy before probing and verify next; `develop-deploy-modes` + `develop-deploy-files-self-deploy` + `develop-first-deploy-scaffold-yaml` restate self-deploy `deployFiles: [.]`; `develop-deploy-modes` + `develop-deploy-files-cross-deploy` + `develop-first-deploy-scaffold-yaml` restate cross-deploy build output/cherry-pick semantics; `develop-env-var-channels` + `develop-first-deploy-env-vars` + `develop-first-deploy-scaffold-yaml` restate env-var placement/cross-ref rules; `develop-verify-matrix` + `develop-first-deploy-verify` + `develop-auto-close-semantics` restate verify-every-service and close criteria; `develop-first-deploy-write-code` + `develop-first-deploy-verify` both restate common first-deploy misconfigs such as `0.0.0.0`, `run.start`, port mismatch, and env-name drift. Under §6.2, "per-service hostname-substituted copies" count as restated facts, so this remains in the 7+ anchor.
**Coverage-gap = 4**: Supported: scaffold entries for both runtime pairs; write real code; deploy `appdev` and `apidev`; start/restart dynamic dev processes; verify both dev services; promote `appdev -> appstage` and `apidev -> apistage`; verify both stage services; use logs and failed checks for diagnosis; apply auto-close only after all services verify. Gap: the dynamic dev-server start commands are not concrete per service, so the likely next-action "start each dev process before verify" requires deriving command/port/healthPath from YAML rather than following a rendered command.
**Task-relevance = 4**: About 21 of 25 atoms are relevant to first deploying two standard runtime pairs; duplication hurts density and redundancy, not relevance, while generic `apiMeta`, env, knowledge, and platform context remain partial.

### develop_first_deploy_standard_single_service
**Coherence = 4**: The fixture is mostly cohesive despite the status listing only `appdev` while the stage hostname appears as metadata and later guidance promotes to `appstage`. The stage guidance is still inferable from `stage=appstage`, but its late appearance is an awkward transition.
**Density = 3**: The render is about 24 KB and mixes concrete first-deploy commands with broad background. It is usable but still has enough explanatory prose and repeated rules to stay at Density 3.
**Redundancy = 2**: Restated facts: `develop-first-deploy-branch` + `develop-first-deploy-run` restate deploy-before-probe and verify-next; `develop-deploy-modes` + `develop-deploy-files-self-deploy` + `develop-first-deploy-scaffold-yaml` restate self-deploy `deployFiles: [.]`; `develop-deploy-modes` + `develop-deploy-files-cross-deploy` + `develop-first-deploy-scaffold-yaml` restate cross-deploy output directory semantics; `develop-env-var-channels` + `develop-first-deploy-env-vars` + `develop-first-deploy-scaffold-yaml` restate env-var channel and cross-service reference rules; `develop-verify-matrix` + `develop-first-deploy-verify` + `develop-auto-close-semantics` restate verify requirements and close criteria.
**Coverage-gap = 4**: Supported: discover envs; scaffold `zerops.yaml`; write real app; deploy `appdev`; start dynamic dev server; verify `appdev`; promote to `appstage`; verify `appstage`; diagnose failed deploys/verifies. Gap: because `appstage` is absent from the Services list, the next-action "confirm the stage target exists before promotion" is not supported by the rendered status block even though later atoms name `appstage`.
**Task-relevance = 4**: About 18 of 23 atoms are relevant or partial for a first deploy of `appdev` with a stage sibling; generic API error, knowledge, and platform blocks are partial.

### develop_simple_deployed_container
**Coherence = 4**: The simple-mode edit loop is clear: edit `/var/www/weatherdash/`, deploy, verify, and close. The main awkwardness is that generic standard/cross-deploy explanations appear in a single-service simple fixture, but they do not contradict the concrete simple-mode path.
**Density = 3**: The render is smaller than the first-deploy fixtures and contains concrete simple-mode commands, but several broad reference blocks and duplicated deploy/verify guidance keep it below the 4-point density anchor.
**Redundancy = 3**: Restated facts: `develop-change-drives-deploy` + `develop-simple-development-workflow` + `develop-simple-close-task` all restate that every edit/config change must run `zerops_deploy targetService="weatherdash"` followed by `zerops_verify`; `develop-deploy-modes` + `develop-push-dev-strategy` + `develop-deploy-files-self-deploy` restate that self-deploy needs `[.]` and narrower patterns destroy source; `develop-deploy-modes` + `develop-deploy-files-cross-deploy` + `develop-push-dev-strategy` restate cross-deploy uses build output/cherry-picks. I count three restated facts, which is the §6.2 Redundancy 3 anchor and satisfies the required move from baseline 2.
**Coverage-gap = 5**: Supported: inspect current state; edit code on `/var/www/weatherdash/`; run `zerops_deploy targetService="weatherdash" setup="prod"`; run `zerops_verify serviceHostname="weatherdash"`; diagnose HTTP failures through verify, URL, logs, and framework logs; change env vars with correct live-timing expectations; inspect `apiMeta` on API errors; change strategy with `zerops_workflow action="strategy"`; expand to standard via bootstrap with `isExisting=true`; close by successful deploy plus verify. No likely next-action gap remains.
**Task-relevance = 4**: About 18 of 22 atoms are relevant or partial to the most-likely task, editing an already deployed simple service and redeploying. `Mode expansion`, strategy switching, and broad knowledge/API/platform material are lower-probability support, not central noise.

## Aggregate verdict

VERDICT: G3-FAIL

Residual gaps and targeted Phase 5.3 patches:

- `develop_first_deploy_two_runtime_pairs_standard` fails redundancy G3 because per-service copies plus shared deploy/verify/env restatements keep Redundancy at 1. Patch by rendering `develop-dynamic-runtime-start-container` once with a service list/table and rendering `develop-first-deploy-promote-stage` once with pair rows (`appdev -> appstage`, `apidev -> apistage`).
- Collapse deployFiles repetition by making `develop-deploy-modes` the single canonical contrast and having `develop-first-deploy-scaffold-yaml`, `develop-deploy-files-self-deploy`, and `develop-deploy-files-cross-deploy` cross-reference only the specific decision they need.
- Collapse verify repetition by keeping route selection in `develop-verify-matrix`, failure interpretation in `develop-first-deploy-verify`, and close criteria in `develop-auto-close-semantics`; avoid re-saying "verify every service" in all three.
- For implicit webserver, resolve the HMR dev-server conflict by either routing Vite long-running process management through `zerops_dev_server` or explicitly carving frontend HMR out of the "no hand-rolled background SSH" rule with a single authoritative atom.

## Phase 5.3 patch log

Patch status:

- P1 — applied. Edited `develop-first-deploy-scaffold-yaml` and `develop-deploy-files-self-deploy`; `develop-deploy-files-cross-deploy` was not present in `internal/content/atoms/`. Gates: `go test ./internal/workflow/ -run 'TestCorpusCoverage|TestSynthesize' -count=1` GREEN; `go test ./internal/content/ -count=1` GREEN.
- P2 — applied. Edited `develop-first-deploy-env-vars` and `develop-first-deploy-scaffold-yaml`. Gates: workflow GREEN; content GREEN.
- P3 — applied. Edited `develop-verify-matrix`, `develop-first-deploy-verify`, and `develop-auto-close-semantics`. Gates: workflow GREEN; content GREEN. After fixing a scaffold YAML indentation typo found during diff review, both gates were rerun and stayed GREEN.

Atom byte deltas:

| Atom | Before | After | Delta |
|---|---:|---:|---:|
| `develop-first-deploy-scaffold-yaml` | 2096 | 1969 | -127 |
| `develop-deploy-files-self-deploy` | 1425 | 1135 | -290 |
| `develop-first-deploy-env-vars` | 1273 | 1340 | +67 |
| `develop-verify-matrix` | 1709 | 1652 | -57 |
| `develop-first-deploy-verify` | 1347 | 1276 | -71 |
| `develop-auto-close-semantics` | 1237 | 1246 | +9 |

Post-5.3 probe:

- Command: `PROBE_DUMP_DIR=plans/audit-composition/rendered-fixtures-post-followup go run ./cmd/atomsize_probe`
- `develop_first_deploy_two_runtime_pairs_standard` `synthesize_bodies_join`: 24543 B.

Signal and pin audit:

- Preserved `Any service self-deploying MUST have deployFiles: [.] or [./]` (Signal #5, do-not/never operational choice).
- Preserved `A narrower pattern destroys the target's working tree` and the numbered recovery/failure sequence (Signal #4, recovery guidance).
- Preserved `do not guess alternatives` in `develop-first-deploy-env-vars` (Signal #1/#3, tool/action guardrail).
- Preserved `Deploy success does not prove the app works for end users` in `develop-verify-matrix` (Signal #3, tool-selection guardrail).
- MustContain pins migrated: none; no edited phrase was pinned by `coverageFixtures().MustContain`.

## Post-Phase-5.3 re-score

Input read: `plans/audit-composition/rendered-fixtures-post-followup/develop_first_deploy_two_runtime_pairs_standard.md` (25,781 B on disk in this checkout). Rubric: §6.2 Redundancy and Coverage-gap anchors in `plans/atom-corpus-hygiene-2026-04-26.md`.

### Score table

| Fixture | Coherence | Density | Redundancy | Coverage-gap | Task-relevance |
|---|---|---|---|---|---|
| standard | 4 | 3 | 2 | 4 | 4 |
| implicit-webserver | 3 | 3 | 2 | 3 | 4 |
| two-pair | 4 | 3 | 1 | 4 | 4 |
| single-service | 4 | 3 | 2 | 4 | 4 |
| simple-deployed | 4 | 3 | 3 | 5 | 4 |

Only the two-pair fixture was re-read for this pass. Its numeric Redundancy and Coverage-gap scores remain unchanged from post-Phase-5.1; the row is re-justified below against the post-Phase-5.3 render.

### Two-pair deltas

| Dimension | §4.2 baseline | Post-Phase-5.1 prior | Post-Phase-5.3 | Δ vs §4.2 | Δ vs Phase-5.1 |
|---|---:|---:|---:|---:|---:|
| Redundancy | 1 | 1 | 1 | +0 | +0 |
| Coverage-gap | 4 | 4 | 4 | +0 | +0 |

### Two-pair justification

**Redundancy = 1**: The render still has 7+ cross-atom restated facts under §6.2 counting rules. Observed evidence:

- `develop-dynamic-runtime-start-container` is rendered twice as the same `### Dynamic-runtime dev server` body: once for `appdev` and once for `apidev`; the only substantive body difference is the hostname in the SSH anti-pattern line (`ssh appdev "cmd &"` vs `ssh apidev "cmd &"`).
- `develop-first-deploy-promote-stage` is rendered twice as the same `### Promote the first deploy to stage` body: `appdev -> appstage` and `apidev -> apistage`.
- Deploy-before-probe / verify-next is restated by the branch guidance (`deploy, verify`) and `develop-first-deploy-run` (`deploy first, then inspect`; "Run verify next").
- Self-deploy `deployFiles` safety is restated by `develop-deploy-modes` (`MUST be [.] or [./]`) and `develop-deploy-files-self-deploy` (`Any service self-deploying MUST have deployFiles: [.] or [./]`).
- Cross-deploy artifact semantics are restated by `develop-deploy-modes` (cross-deploy cherry-picks build output), the deployFiles decision table (`[./out]` / `[./out/~]`), and the promote-stage bodies ("No second build — cross-deploy packages the dev tree straight into stage").
- Env-var placement/discovery is restated by `develop-env-var-channels`, `develop-first-deploy-env-vars`, and `develop-first-deploy-scaffold-yaml` (`run.envVariables` appears in all three regions; `develop-env-var-channels` is referenced three times).
- Verify/close requirements are restated by branch guidance, `develop-verify-matrix`, four concrete verify commands in `develop-first-deploy-verify`, and `develop-auto-close-semantics`.
- First-deploy misconfig checks are restated by `develop-first-deploy-write-code` and `develop-first-deploy-verify`: `0.0.0.0`, `run.start`, health endpoint/port, and env-name drift all recur.

The dominant residual duplication is STRUCTURAL: two per-service render repetitions of `develop-dynamic-runtime-start-container` and `develop-first-deploy-promote-stage` are caused by the two-runtime-pair fixture shape. The Phase 5.3 shared-rule trims reduced corpus-level deployFiles/env/verify duplication, but not enough to move the fixture out of the 7+ anchor.

**Coverage-gap = 4**: The likely next actions remain supported: scaffold setup entries for both runtime hostnames; discover env keys with `zerops_discover includeEnvs=true`; write real code; deploy `appdev` and `apidev`; start dynamic dev processes via the `zerops_dev_server` action family; verify `appdev`, `apidev`, `appstage`, and `apistage`; promote `appdev -> appstage` and `apidev -> apistage`; diagnose with `apiMeta`, logs, failed checks, and platform rules; and close only after both dev/stage halves pass deploy + verify. One low-probability gap remains: the dynamic dev-server section names required args (`hostname command port healthPath`) but does not render concrete per-service `zerops_dev_server action="start"` calls with each service's actual command, port, and health path.

### §15.3 G3 disposition

| Fixture | Redundancy | Coverage-gap | Status |
|---|---:|---:|---|
| standard | 2 | 4 | PASS |
| implicit-webserver | 2 | 3 | PASS |
| two-pair | 1 | 4 | FAIL (STRUCTURAL residual) |
| single-service | 2 | 4 | PASS |
| simple-deployed | 3 | 5 | PASS |

G3 pass count: 4/5. Two-pair does not close strict G3 because Redundancy remains below 2 and Coverage-gap is not flat-at-5. Because the remaining blocker is dominated by engine-level per-service render repetition rather than a corpus-level content gap, aggregate disposition is SHIP-WITH-NOTES, not Phase 5 EXIT.

### Aggregate verdict

VERDICT: SHIP-WITH-NOTES

## Final Phase 7 re-score (post-Phase-6 corpus)

Round: CORPUS-SCAN composition cross-validation per §10.1 P7
Date: 2026-04-27
Reviewer: Codex
Plan: §5 Phase 7 + §15.3 G3 (post-Phase-0 amendments 5+6)

### Score table

| Fixture | Coh | Den | Red | Cov-gap | Task-rel |
|---|---:|---:|---:|---:|---:|
| standard | 4 | 3 | 2 | 4 | 4 |
| implicit-webserver | 3 | 3 | 2 | 3 | 4 |
| two-pair | 4 | 3 | 1 | 4 | 4 |
| simple-deployed | 4 | 3 | 3 | 5 | 4 |
| hypothetical-single | 4 | 3 | 2 | 4 | 4 |

### Δ vs §4.2 baseline + Δ vs post-Phase-5.3

| Fixture | Coh Δ | Den Δ | Red Δ | Cov-gap Δ | Task-rel Δ | Notes |
|---|---|---|---|---|---|---|
| standard | +3 / +0 | +0 / +0 | +1 / +0 | +1 / +0 | +1 / +0 | vs §4.2 baseline, then vs post-Phase-5.3 |
| implicit-webserver | +2 / +0 | +0 / +0 | +1 / +0 | +1 / +0 | +1 / +0 | vs §4.2 baseline, then vs post-Phase-5.3 |
| two-pair | +3 / +0 | +1 / +0 | +0 / +0 | +2 / +0 | +1 / +0 | Redundancy still 7+ restated facts; structural G3 miss |
| simple-deployed | +2 / +0 | +0 / +0 | +1 / +0 | +1 / +0 | +0 / +0 | Coverage-gap reaches flat-at-5 |
| hypothetical-single | +0 / +0 | +0 / +0 | +0 / +0 | +0 / +0 | +0 / +0 | Hypothetical fixture has no §4.2 row; first-available post-Phase-5.3 score used as baseline |

### §15.3 G3 final disposition

| Fixture | Redundancy G3 | Coverage-gap G3 | overall |
|---|---|---|---|
| standard | PASS | PASS | PASS |
| implicit-webserver | PASS | PASS | PASS |
| two-pair | STRUCTURAL-FAIL | PASS | STRUCTURAL-FAIL |
| simple-deployed | PASS | PASS | PASS |
| hypothetical-single | STRUCTURAL-FAIL | STRUCTURAL-FAIL | STRUCTURAL-FAIL |

### Per-fixture qualitative justification (verbatim analysis)

#### develop_first_deploy_standard_container
**Coh = 4**: This matches the §6.2 Coherence 4 anchor: mostly cohesive with 1-2 awkward transitions. The first-deploy path is readable from branch selection through scaffold, code, deploy, dynamic dev-server start, verify, promote, and stage verify. The awkward transitions are the broad deployFiles/env/platform reference blocks before the concrete first-deploy commands and the late container platform block after the first-deploy verdict.
**Den = 3**: At 21,526 B, the fixture still carries enough reference prose, defensive tables, and generic platform material to fit the §6.2 Density 3 anchor (2.0-2.9 facts/KB, acceptable but rewriteable). It is operationally useful, but not lean enough for Density 4.
**Red = 2**: I count 6 cross-atom restated facts, which maps to the §6.2 Redundancy 2 anchor (4-6 restated facts). Restated facts: `develop-first-deploy-branch` lines 13-25 + `develop-first-deploy-run` lines 347-364 both say first deploy before probing/inspection and verify next; `develop-deploy-modes` lines 80-96 + `develop-deploy-files-self-deploy` lines 230-253 restate self-deploy requires `[.]` or `[./]`; `develop-deploy-modes` lines 80-107 + `develop-first-deploy-scaffold-yaml` lines 166-188 + `develop-first-deploy-promote-stage` lines 392-404 restate cross-deploy/build-output semantics; `develop-env-var-channels` lines 108-126 + `develop-first-deploy-env-vars` lines 127-153 + `develop-first-deploy-scaffold-yaml` lines 173-179 restate runtime env-var placement/cross-service syntax; `develop-verify-matrix` lines 365-391 + `develop-first-deploy-verify` lines 405-430 + `develop-auto-close-semantics` lines 325-343 restate verify requirements and close criteria; `develop-first-deploy-write-code` lines 286-294 + `develop-first-deploy-verify` lines 412-421 restate common first-deploy failures such as bind address, start command, port, and env-name drift.
**Cov-gap = 4**: Likely next-actions: discover env keys (covered by `develop-first-deploy-env-vars`), scaffold YAML (covered by `develop-first-deploy-scaffold-yaml`), write app code (covered), deploy `appdev` (covered by `develop-first-deploy-run`), start dynamic process (covered by `develop-dynamic-runtime-start-container`), verify `appdev` (covered), promote to `appstage` (covered), verify `appstage` (covered), diagnose failed deploy/verify (covered). One low-probability gap remains: the dev-server atom names the `zerops_dev_server action="start"` arg family but does not render a concrete command populated with this service's command, port, and health path.
**Task-rel = 4**: The most likely task is first deploying a standard dynamic app and promoting it to stage. Roughly 75-89% of atoms are relevant or partially relevant, matching §6.2 Task-relevance 4; generic `apiMeta`, knowledge, and broad platform atoms are support material rather than central task steps.

#### develop_first_deploy_implicit_webserver_standard
**Coh = 3**: This matches §6.2 Coherence 3. Sections are readable, but the agent must mentally reset around frontend process guidance: `develop-implicit-webserver-runtime` says there is no server to start and not to SSH in to start one (lines 213-230), `develop-first-deploy-frontend-assets` tells the agent to run Vite with background SSH (lines 393-424), and `develop-platform-rules-container` warns not to hand-roll background SSH (lines 468-481). The named deploy/verify calls still agree, so this is not Coherence 1.
**Den = 3**: At 22,880 B, the fixture contains concrete implicit-webserver and frontend asset guidance, but also broad deployFiles, env, diagnostics, knowledge, and platform sections. That is §6.2 Density 3: acceptable but rewriteable.
**Red = 2**: I count 6 restated facts, the §6.2 Redundancy 2 anchor. Restated facts: `develop-first-deploy-branch` lines 13-25 + `develop-first-deploy-run` lines 348-365 restate deploy-first/verify-next; `develop-deploy-modes` lines 80-96 + `develop-deploy-files-self-deploy` lines 258-281 restate self-deploy `[.]` / `[./]`; `develop-deploy-modes` lines 80-107 + `develop-first-deploy-scaffold-yaml` lines 166-188 + `develop-first-deploy-promote-stage` lines 428-440 restate cross-deploy build-output semantics; `develop-env-var-channels` lines 108-126 + `develop-first-deploy-env-vars` lines 127-153 + `develop-first-deploy-scaffold-yaml` lines 173-179 restate env-var placement and cross-service references; `develop-implicit-webserver-runtime` lines 213-240 + `develop-first-deploy-frontend-assets` lines 393-427 both describe implicit-webserver serving/asset consequences; `develop-verify-matrix` lines 366-392 + `develop-first-deploy-verify` lines 441-467 restate verify status interpretation and fallback.
**Cov-gap = 3**: Likely next-actions are scaffold implicit-webserver YAML, write app files, deploy `appdev`, prepare frontend assets, verify, promote, verify stage, and diagnose web errors. All are present, but §6.2 Coverage-gap 3 applies because one likely next-action has competing recommendations: iterative frontend/HMR handling is split between a background SSH Vite command and a platform rule saying long-running background SSH should not be hand-rolled.
**Task-rel = 4**: The most likely task is first deploying a `php-nginx`/implicit-webserver standard app. The implicit-webserver and frontend asset atoms are central; generic env/API/platform/knowledge atoms are partial. That stays in the 75-89% relevant §6.2 anchor.

#### develop_first_deploy_two_runtime_pairs_standard
**Coh = 4**: This fits §6.2 Coherence 4. The document is mostly cohesive and consistently covers `appdev/appstage` plus `apidev/apistage`; the awkward transitions are the per-service repeated dynamic-server and promote-stage blocks, which interrupt the flow but do not create incompatible tool recommendations.
**Den = 3**: At 23,546 B, it remains in the §6.2 Density 3 band. The fixture has many load-bearing facts, but per-service repeated sections and broad shared reference material keep it acceptable but rewriteable rather than lean.
**Red = 1**: I count 8 cross-atom restated facts, so this remains in the §6.2 Redundancy 1 anchor (7+ restated facts), not the 4-6 bucket. Restated facts: (1) `develop-dynamic-runtime-start-container` renders as hostname-substituted copies for `appdev` lines 256-282 and `apidev` lines 283-309; (2) `develop-first-deploy-promote-stage` renders as hostname-substituted copies for `appdev -> appstage` lines 424-436 and `apidev -> apistage` lines 437-449; (3) `develop-first-deploy-branch` lines 15-27 + `develop-first-deploy-run` lines 376-396 both restate deploy before probing/inspection and verify next; (4) `develop-deploy-modes` lines 82-97 + `develop-deploy-files-self-deploy` lines 232-255 restate self-deploy `[.]` / `[./]`; (5) `develop-deploy-modes` lines 82-107 + `develop-first-deploy-scaffold-yaml` lines 166-190 + both `develop-first-deploy-promote-stage` blocks lines 424-449 restate cross-deploy build-output/cherry-pick semantics; (6) `develop-env-var-channels` lines 110-128 + `develop-first-deploy-env-vars` lines 129-155 + `develop-first-deploy-scaffold-yaml` lines 175-181 + `develop-platform-rules-common` lines 215-231 restate env-var placement, live timing, and cross-service syntax; (7) `develop-verify-matrix` lines 397-423 + `develop-first-deploy-verify` lines 450-482 + `develop-auto-close-semantics` lines 354-372 + branch guidance lines 26-27 restate verify-every-service and close criteria; (8) `develop-first-deploy-write-code` lines 315-327 + `develop-first-deploy-verify` lines 457-468 restate common first-deploy misconfigs including `0.0.0.0`, `run.start`, port mismatch, and env-name drift. Result: held at 1 STRUCTURAL.
**Cov-gap = 4**: Likely next-actions: scaffold both runtime setup entries, discover env keys, write code, deploy `appdev`, deploy `apidev`, start/restart both dynamic dev processes, verify both dev services, promote both stage pairs, verify both stage services, and diagnose failures. These are covered. The low-probability gap is still that `zerops_dev_server action="start"` is described by arg family, not rendered as concrete per-service commands using each service's command, port, and health path.
**Task-rel = 4**: The task is first deploying two standard runtime pairs. Most atoms are relevant or partial; generic API, knowledge, env, and platform material remain support content. The per-service duplication hurts redundancy, not relevance, so the fixture remains in the 75-89% §6.2 anchor.

#### develop_first_deploy_standard_single_service
**Coh = 4**: This is §6.2 Coherence 4. The guidance is mostly a single first-deploy narrative for `appdev`, with one awkward transition: the status lists only `appdev` while later promotion guidance uses `appstage` from the `stage=appstage` metadata, so the stage target is inferable but not as naturally grounded as in the two-service status block.
**Den = 3**: At 21,370 B, it remains §6.2 Density 3. Concrete first-deploy commands are present, but broad deployFiles/env/platform/knowledge sections keep it rewriteable.
**Red = 2**: I count 6 restated facts, matching the §6.2 Redundancy 2 anchor. Restated facts: `develop-first-deploy-branch` lines 12-24 + `develop-first-deploy-run` lines 346-363 restate deploy-first/verify-next; `develop-deploy-modes` lines 79-95 + `develop-deploy-files-self-deploy` lines 229-252 restate self-deploy `[.]` / `[./]`; `develop-deploy-modes` lines 79-106 + `develop-first-deploy-scaffold-yaml` lines 163-187 + `develop-first-deploy-promote-stage` lines 391-403 restate cross-deploy output semantics; `develop-env-var-channels` lines 107-125 + `develop-first-deploy-env-vars` lines 126-152 + `develop-first-deploy-scaffold-yaml` lines 172-178 restate env-var placement/cross-service syntax; `develop-verify-matrix` lines 364-390 + `develop-first-deploy-verify` lines 404-427 + `develop-auto-close-semantics` lines 324-342 restate verify and close criteria; `develop-first-deploy-write-code` lines 285-293 + `develop-first-deploy-verify` lines 411-420 restate first-deploy misconfig checks.
**Cov-gap = 4**: Likely next-actions are discover envs, scaffold, write code, deploy `appdev`, start dynamic dev server, verify `appdev`, promote to `appstage`, verify `appstage`, and diagnose failures. All are covered except one low-probability gap: because `appstage` is not listed as a service in the status block, the rendered document does not itself support confirming the stage target exists before promotion.
**Task-rel = 4**: The likely task is first deploying `appdev` with a stage sibling. Most atoms are relevant or partial, while generic `apiMeta`, knowledge, and platform blocks are support material. This stays within the 75-89% §6.2 relevance anchor.

#### develop_simple_deployed_container
**Coh = 4**: This is §6.2 Coherence 4. The simple-mode edit loop is cohesive: inspect/current state, edit `/var/www/weatherdash/`, deploy, verify, close. The awkward transition is that standard/cross-deploy and mode-expansion material appears inside a simple single-service fixture, but it does not contradict the main path.
**Den = 3**: At 16,737 B, the fixture is smaller and has concrete simple-mode commands, but it still includes broad deployFiles, strategy, mode-expansion, API, and platform reference material. That remains §6.2 Density 3, not Density 4.
**Red = 3**: I count 3 restated facts, matching the §6.2 Redundancy 3 anchor. Restated facts: `develop-change-drives-deploy` lines 47-56 + `develop-simple-development-workflow` lines 208-219 + `develop-simple-close-task` lines 362-370 restate edit/config change -> deploy -> verify; `develop-deploy-modes` lines 57-84 + `develop-push-dev-strategy` lines 143-158 + `develop-deploy-files-self-deploy` lines 164-187 restate self-deploy `[.]` and narrower-pattern destruction; `develop-deploy-modes` lines 57-84 + `develop-push-dev-strategy` lines 151-158 + `develop-mode-expansion` lines 320-361 restate cross-deploy/dev-to-stage artifact semantics.
**Cov-gap = 5**: Likely next-actions are inspect state, edit code on `/var/www/weatherdash/`, deploy with `zerops_deploy targetService="weatherdash" setup="prod"`, verify with `zerops_verify serviceHostname="weatherdash"`, diagnose HTTP failures, handle env-var live timing, change deploy strategy, expand simple to standard if requested, and close after deploy+verify. Each has exactly one unambiguous recommendation, so this reaches the §6.2 Coverage-gap 5 anchor.
**Task-rel = 4**: The likely task is editing an already deployed simple service and redeploying. Most atoms directly support that loop; strategy switching and mode expansion are lower-probability but still plausible support. This remains the 75-89% §6.2 anchor.

### Cumulative byte recovery

| Slice | First cycle | This cycle (P0→P6) | Cumulative |
|---|---:|---:|---:|
| 4 first-deploy | −7,461 B | −15,537 B | −22,998 B |
| 5 fixtures aggregate | −11,344 B | −17,887 B | −29,231 B |

§8 binding targets: additional ≥6,000 B (this cycle) **MET 3×**;
cumulative ≥17,000 B **MET 1.7×**.

### Final aggregate verdict

VERDICT: SHIP-WITH-NOTES

The corpus meets the byte-recovery targets, and the non-decreasing dimensions hold across the baseline set. It is not CLEAN-SHIP because `develop_first_deploy_two_runtime_pairs_standard` still fails the strict Redundancy G3 gate at 7+ restated facts, dominated by structural per-service rendering plus shared deploy/env/verify restatements. The hypothetical single-service fixture is also not a strict-improvement proof because its first-available baseline equals the current score and Redundancy/Coverage-gap are not flat-at-5; treat that as a comparison limitation plus a note, not evidence that the original baseline set regressed.
