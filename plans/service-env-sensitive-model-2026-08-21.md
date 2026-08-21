# Plan: service-env-sensitive-model (lean)

## Run State
- `phase:` build
- `base:` fdb358a7b9b0e4c31e167d47f159ee4e422fdcc1
- `integration:` branch `fix/service-env-sensitive-model` (spec promotion commit on top of base; slice range appended as slices land)
- `approved:` Rev-2 (lean), 2026-08-21 — Karel: "implementuj to tedy celé" after the root-cause-only re-cut; earlier Rev-1 (Codex-expanded) superseded
- `codex:` pass 1 + pass 2 on Rev-1 (reviews `/tmp/codex-out-1787343775-47189-31559.md`, `/tmp/codex-out-1787345151-50189-22088.md`); Rev-2 keeps the findings that touch the root cause (hand-roll decode → `*PlatformError` without body; `FetchServiceUserEnvs` errors instead of empty on a failed yaml fetch; canary name `TestLaunchBaseline_*`; compile order) and drops the additive features (PUT upsert, tri-state input, race recovery, value scrub, read-side `sensitive` exposure, layer constructor for discover/env_generate, in-container e2e)
- `next:` BUILD S1 (write seam) in a worktree from the integration branch → replay → merge → S2 → ASSEMBLE
<!-- material edit to Frame or Slice Register after approval resets phase to awaiting-approval -->

## Frame
**Outcome**: ZCP's service-scope env writes (`zerops_env set`, git-push-setup `GIT_TOKEN`, launch-production token staging, `zcp agent mark-oauth`) work again against the platform's new userData contract, and its reads of the user-set service env (export / launch-production `envSecrets` carry, discover/yaml layer) follow the new `USER|SYSTEM` model — as a root-cause-only change with no new tool inputs or behaviors, shipped in a tagged zcp release.

**Root causes (live-verified 2026-08-21, details in the Evidence Ledger):**
1. `POST /service-stack/{id}/user-data` now REQUIRES `sensitive` (bool); the pinned zerops-go SDK (and upstream main) cannot send it → every service-scope write fails with 400 `invalidUserInput metadata.sensitive:["field is required"]`.
2. Service env types are now only `USER | SYSTEM` (+ `sensitive` flag); legacy `READ_ONLY|EDITABLE|SECRET|INTERNAL|ENV` are gone → `FetchServiceSecretEnvs` (`Type==SECRET`) returns empty → export/launch silently drop user-set env; `classifyAppVersionUserData` maps only legacy enums → yaml-baked layer silently empty in discover / effective env / shadow detection.
3. Yaml-baked `run.envVariables` now ALSO appear on slim `/env` as `USER` (read-only: POST dup → `userDataDuplicateKey`, PUT → `recordIsReadOnly`, DELETE → `userDataDeleteForbidden`); the app-version `userDataList` carries them as `USER` (+ `SYSTEM` intrinsics) and never carries user-set vars → "user-set" must be derived as `/env USER − app-version USER keys`, and a yaml-baked key hit by set/delete must translate to the existing "yaml owns the key" guidance.

| obs | evidence |
|---|---|
| POST without `sensitive` → 400 `{"sensitive":["field is required"]}`; with it → 2xx, reads back `type:"USER", sensitive:<as sent>` | ledger P-write; OpenAPI `RequestUserDataPost.required: [key, content, sensitive]` |
| `GET /env` returns only `SYSTEM|USER` + `sensitive`; old secrets migrated to `sensitive:false`; intrinsics `SYSTEM` | ledger P0 |
| SDK `body.UserDataPost` = `{Key, Content}` in v1.0.20, v1.0.21, upstream main | module cache + GitHub raw |
| `ops/lookup.go:56` is the only production Type comparison; implementors of `CreateServiceEnvVar` exactly two (`zerops_env.go:40`, `mock_methods.go:285`); hand-roll precedent `zerops_delegation.go:97-131` + httptest harness `zerops_delegation_test.go:274-295` | explore lanes + adversarial pass |
| app-version `userDataList` = yaml-baked `USER editable=false` + `SYSTEM`; user-set never there (also after the process FINISHED); yaml-baked keys mirrored on `/env` as `USER` | ledger P3 |
| yaml-baked record: POST dup → `userDataDuplicateKey`, PUT → `recordIsReadOnly`, DELETE → `userDataDeleteForbidden` | ledger P3b |
| `sensitive:true` content reads back verbatim for an admin/write token (ZCP's integration token) | ledger P1 + spec §7 `[LIVE]` |

- AC1: `zerops_env set serviceHostname=X KEY=V` writes again (POST carries `sensitive:true`, exactly the old SECRET behavior) — evidence: httptest body assertion, ops/tool tests reading `Sensitive==true` back from the mock, live e2e round-trip.
- AC2: git-push-setup confirm writes `GIT_TOKEN` (sensitive:true) and stamps `configured`; launch staging and `zcp agent mark-oauth` write again — evidence: existing handler tests green + `Sensitive==true` assertions; owner live retest of git-push-setup on localflow (also proves in-container delivery of a sensitive var).
- AC3: export + launch bundles carry a `zerops_env set` var again and never a yaml-baked or SYSTEM key — evidence: `FetchServiceUserEnvs` tests (USER minus app-version keys; error, not empty, when the yaml fetch fails on a live runtime), gap0/bundle tests.
- AC4: yaml-baked layer is back in discover / effective env / shadow detection (`USER`→yaml-baked, `SYSTEM`→intrinsic), listed once in discover — evidence: classification table test, discover dedupe test.
- AC5: a yaml-baked key hit by `zerops_env set`/`delete` yields the "yaml owns the key" guidance, no mutation — evidence: ops tests on `userDataDeleteForbidden`/`userDataDuplicateKey`.
- AC6: `ServiceEnvType` is exactly `USER|SYSTEM`; every tier compiles (`go vet -tags e2e`); e2e canary `TestLaunchBaseline_*` uses the new set — evidence: closed-enum test, vet, live canary.
- AC7: spec `spec-zerops-env-lifecycle.md` §1/§6/§7/§9/§12 + `spec-workflows.md` git-push-setup line + `spec-env-handling.md` §4 state the new model; no stale Type=SECRET prose in code — evidence: grep empty.
- AC8: tagged release with green `release.yml`.

**Non-goals**: any new tool input/output (`sensitive` opt-out, read-side `sensitive` field) — follow-up if ever needed; PUT/in-place upsert; race recovery; value scrubbing of API errors; layer constructor for discover/env_generate; in-container e2e; project-scope env changes; SDK upstream fix; the Discord user's separate `zcli push` `.github/` report.
**Constraints**: hand-rolled POST on `ZeropsClient.env` (SDK lacks the field) + drift guard; credential values never echo; layering + `tools/eval/cmd` never call client env methods directly; TDD RED first; every commit compiles incl. `-tags e2e`; JSON-only stdout; atomic commits; no `replace` in go.mod.
**Risk class**: high (credential path, live platform) — trigger: load-bearing platform-API assumption + credential surface + `platform.Client` contract change.

**Design (root-cause only)**:
- D1 Service-scope writes send `sensitive:true` on every path (`EnvSet` service branch, `EnvSetSecretService`) — identical to the pre-change behavior (everything ZCP wrote there was SECRET/masked); project scope stays `false`. No new interface. Rejected: tri-state input / default false / heuristic — additive.
- D2 Upsert stays delete-then-create (pre-existing design); only the new `userDataDeleteForbidden` on a yaml-baked record is translated to the yaml-owned guidance. Rejected: PUT in place — improvement, not root cause.
- D3 User-set layer = `/env USER (or empty Type)` − app-version `USER` keys, computed in `FetchServiceUserEnvs`; when the yaml fetch fails on a live runtime → error (never empty). Discover skips yaml keys already listed. Rejected: a cross-consumer layer constructor (env_generate/effective env keep today's behavior; shadow detection already checks yaml first).
- D4 `ServiceEnvType` narrowed to `USER|SYSTEM`; empty treated as USER (mock seeds).

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| P-write — POST requires `sensitive`; with it the record lands `USER` + flag as sent | AC1 | mcp (raw REST, scratch eval/alptest, cleaned) | `POST …/service-stack/{alptest}/user-data {key,content}` / `{…,sensitive:true|false}` → `GET …/env` → `DELETE` | 400 `{"sensitive":["field is required"]}`; then `{"key":"ZCP_PROBE_TMP_B","content":"probe","type":"USER","sensitive":true}` / `…_C … sensitive:false` | CONFIRMED | `TestCreateServiceEnvVar_SendsSensitiveInBody` + e2e round-trip |
| P0 — `/env` intrinsics `SYSTEM`, user-set `USER` | AC3/AC6 | mcp (read-only) | `GET …/service-stack/{localflow/zcp, eval/zcp, eval/app}/env` tally | 9 SYSTEM + 10 USER (all USER user-set: VSCODE_PASSWORD, ZCP_*); 9+7; 9+1 (NODE_ENV) | CONFIRMED | `TestLaunchBaseline_*` canary `USER|SYSTEM` |
| P1 — `sensitive:true` content verbatim for admin/write token | AC2 (`launchKeyFromStage`) | mcp | as P-write | content `"probe"` verbatim | CONFIRMED | spec §7 |
| P3 — app-version `userDataList` = yaml-baked `USER editable=false` + `SYSTEM`; user-set never there (after FINISHED); yaml-baked mirrored on `/env` as `USER` | AC3/AC4 | mcp (scratch eval/app + read-only zcp-example/web, localflow/zcp) | `GET /service-stack/{id}` → `.activeAppVersion.id` → `GET /app-version/{id}`; POST probe → poll `GET /process/{id}` FINISHED → re-read → DELETE | eval/app: 21 items = 20 SYSTEM + NODE_ENV USER editable=false; probe absent from list, present on `/env`; zcp-example/web 6× `Z_* USER editable=false` in list AND on `/env`; localflow/zcp list empty, `/env` 10 user-set | CONFIRMED | `TestClassifyAppVersionUserData_NewModel`, `TestFetchServiceUserEnvs_*`, e2e round-trip asserts probe ∉ app-version list |
| P3b — yaml-baked record mutation codes | AC5 | mcp (eval/app NODE_ENV, unchanged) | POST dup / PUT / DELETE on the yaml-baked record | `userDataDuplicateKey` / `recordIsReadOnly` / `userDataDeleteForbidden`; NODE_ENV intact | CONFIRMED | ops translation tests |
| P4 — `sensitive:true` delivered plaintext in-container | AC2 | — (no VPN from this session) | not run | not run | INCONCLUSIVE → `[ASSUMED]`, proven by the owner's live git-push-setup retest (6c fresh-SSH auth) before release; fallback: `sensitive:false` for `GIT_TOKEN` | owner retest pack |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Write seam: hand-rolled POST with `sensitive` (+ `*PlatformError` on malformed responses, no body echo), drift guard, `sensitive:true` on both ZCP write paths, `userDataDeleteForbidden` translation on set/delete; adds `ServiceEnvUser/System` (legacy consts kept) | — | internal/platform/{client.go,zerops_env.go,zerops_env_test.go(new),types.go(add consts),mock_methods.go,mock.go(doc)}; internal/ops/{env.go,env_test.go}; internal/tools/{workflow_git_push_setup.go(comment),git_push_setup_service_token_test.go,launch_stage_test.go}; integration/multi_tool_test.go(if needed) | unit/tool/integration | review | pending |
| S2 | Read model: `ServiceEnvType`=`USER|SYSTEM` (legacy deleted, fixtures migrated incl. e2e canary), app-version classification `USER`→yaml/`SYSTEM`→intrinsic, `FetchServiceUserEnvs` (= `/env USER` − app-version USER; error on failed yaml fetch), discover dedupe, prose; e2e round-trip test | S1 | internal/platform/{types.go,env_types_test.go,zerops_appversion.go,zerops_appversion_test.go,mock_methods.go,mock.go,client.go(doc)}; internal/ops/{lookup.go,lookup_test.go,discover.go,discover_test.go,env_effective.go(doc),inventory/envs.go(doc),export_bundle.go(doc),bundle/inputs.go(doc),bundle/export.go(doc),bundle/helpers.go(doc+2 strings),bundle/gap0_envsecrets_test.go(doc)}; internal/tools/{workflow_export.go,launch_bundle_compose.go,workflow_export_probe.go(comment),discover.go(includeEnvs desc),env_test.go(seeds),launch_stage_read_test.go,launch_delegation_test.go}; internal/envclass/classify_test.go; e2e/{launch_baseline_test.go,service_env_sensitive_test.go(new)} | unit/tool/integration/e2e(compile) | review | pending |

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | `go test ./internal/platform -run 'TestCreateServiceEnvVar_|TestSDKUserDataBody_StillLacksSensitive|TestUserDataWrite_' -count=1 -v`; `go test ./internal/ops -run 'TestEnvSet_ServiceScope' -count=1 -v`; `go test ./internal/tools -run 'TestEnvTool_Set' -count=1 -v`; live: `go test ./e2e/ -tags e2e -run TestE2E_ServiceEnv_SensitiveRoundTrip -v` | not-run | — |
| AC2 | `go test ./internal/tools -run 'TestGitPushSetup|TestStageLaunchToken|TestExecuteLaunchMutation_StagesTokenBeforeCreate|TestLaunchStaging' -count=1 -v`; `go test ./internal/ops -run 'TestMarkAgentOAuth' -count=1 -v`; owner: live `git-push-setup` confirm on localflow `zcp` (VPN) — proves P4 | not-run | — |
| AC3 | `go test ./internal/ops -run 'TestFetchServiceUserEnvs_' -count=1 -v`; `go test ./internal/ops/bundle ./internal/tools -run 'GAP0|ServiceUserEnv|Export|LaunchBundle' -count=1` | not-run | — |
| AC4 | `go test ./internal/platform -run 'TestClassifyAppVersionUserData_NewModel|TestGetAppVersionUserData' -count=1 -v`; `go test ./internal/ops -run 'TestDiscover.*Yaml|TestServiceHigherLayers|TestAppVersionEnvVars' -count=1 -v` | not-run | — |
| AC5 | `go test ./internal/ops -run 'TestEnvSet_ServiceScope_YamlBaked|TestEnvDelete_ServiceScope_YamlBaked' -count=1 -v` | not-run | — |
| AC6 | `go test ./internal/platform -run TestServiceEnvType_ClosedEnum -count=1`; `go vet -tags e2e ./...`; live: `go test ./e2e/ -tags e2e -run 'TestLaunchBaseline_' -v` | not-run | — |
| AC7 | `grep -rn "Type=SECRET\|SECRET-typed\|ServiceEnvSecret\|ServiceEnvReadOnly\|ServiceEnvEditable\|ServiceEnvInternal\|ServiceEnvEnv\b\|FetchServiceSecretEnvs\|EnvSetSensitiveProject" internal/ cmd/ integration/ e2e/` → empty; spec sections edited (GATE 1 commit) | not-run | — |
| AC8 | `git tag --sort=-v:refname | head -1` resolves to the assembled SHA; `release.yml` run success (URL recorded) | not-run | — |
| — | regression: `go test ./... -short -count=1`; `go test ./... -race -count=1`; `make lint-local`; `go test ./internal/topology/... -count=1` | not-run | — |

## Promotion
- Contracts → `docs/spec-zerops-env-lifecycle.md` §1 (model: `USER|SYSTEM` + `sensitive` REQUIRED on write; yaml-baked mirrored on slim `/env` read-only; app-version list `USER` yaml-baked + `SYSTEM`, user-set never there; mutation codes), §6, §7 (`sensitive` = explicit per-var flag; old secrets migrated to false; ZCP writes service-scope vars `sensitive:true`; admin verbatim / read-only REDACTED; in-container plaintext `[ASSUMED → owner retest]`; project-level persistence unverified since 2026-08), §9 PENDING-3/4 notes, §12 glossary · `docs/spec-workflows.md` git-push-setup line (GIT_TOKEN as service var `sensitive:true`) · `docs/spec-env-handling.md` §4 :213-224 — DONE at GATE 1 (commit on the integration branch).
- Invariants → `TestSDKUserDataBody_StillLacksSensitive`, `TestCreateServiceEnvVar_SendsSensitiveInBody`, `TestFetchServiceUserEnvs_ExcludesSystemAndYamlBaked`, `TestFetchServiceUserEnvs_YamlFetchError_ReturnsError`, `TestClassifyAppVersionUserData_NewModel`, `TestEnvSet_ServiceScope_YamlBakedKey_DeleteForbidden_YamlGuidance`, e2e `TestE2E_ServiceEnv_SensitiveRoundTrip` + `TestLaunchBaseline_*` canary.
- CLAUDE.md trap line (≤1): "Service userData writes require `sensitive` and are HAND-ROLLED (`platform/zerops_env.go` on `ZeropsClient.env`) — never add an env write through the SDK handler (`PostServiceStackUserData` lacks the field; the mock stays green while prod 400s). `TestSDKUserDataBody_StillLacksSensitive`."
- This plan → `plans/archive/` on LAND close
