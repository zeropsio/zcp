# Retest pack: service-env-sensitive-model

Zero-context bar: Karel executes every step below with no other file open, in minutes. Every step ties to an ACx. Branch: `fix/service-env-sensitive-model` @ db3c3a15 (checked out in the main tree).

## Run
Exact commands, each with the ONE line that means "pass":

| command | expected line |
|---|---|
| `go test ./internal/platform -run 'TestSDKUserDataBody_StillLacksSensitive\|TestCreateServiceEnvVar_\|TestUserDataWrite_' -short -count=1 -v` | `ok  	github.com/zeropsio/zcp/internal/platform` (AC1) |
| `go test ./internal/ops -run 'TestEnvSet_ServiceScope\|TestEnvSetSecretService_\|TestEnvDelete_ServiceScope_\|TestFetchServiceUserEnvs_\|TestDiscover_IncludeEnvs_YamlBakedListedOnce\|TestAppVersionEnvVars_NewModel' -short -count=1 -v` | `ok  	github.com/zeropsio/zcp/internal/ops` (AC1/AC3/AC4/AC5) |
| `go test ./internal/tools -run 'TestGitPushSetup_Confirm_GitTokenIsSensitive\|TestStageLaunchToken_IsSensitive\|TestExport_ServiceUserEnv_CarriedNotYamlBaked\|TestLaunchBundle_ServiceUserEnv_CarriedNotYamlBaked' -short -count=1 -v` | `ok  	github.com/zeropsio/zcp/internal/tools` (AC2/AC3) |
| `go test ./internal/platform -run 'TestClassifyAppVersionUserData_NewModel_UserIsRunEnvSystemIsIntrinsic\|TestServiceEnvType_ClosedEnum' -short -count=1 -v` | `ok` (AC4/AC6) |
| `make lint-local` | `0 issues.` |
| `make vet-tags` | (no output = clean; AC6) |
| `make test-race` | every package `ok`, no `FAIL`/`DATA RACE` |
| live write contract — `ZCP_API_KEY=<eval-project integration token> ZCP_API_HOST=api.app-prg1.zerops.io ZCP_E2E_ENV_SERVICE=alptest go test ./e2e/ -tags e2e -run 'TestE2E_ServiceEnv_SensitiveRoundTrip\|TestLaunchBaseline_' -count=1 -v` (already run by the assembler: PASS 12.7 s + PASS) | `--- PASS: TestE2E_ServiceEnv_SensitiveRoundTrip` and `PASS` for the canary (AC1/AC6) |

## Drive
Steps against a live ZCP container (localflow `zcp`, VPN up: `zcli vpn up gRLfpBNrSziMKj0VEfk6vw`) running the fixed binary (`./eval/scripts/build-deploy.sh` then `ssh zcp "cd /var/www && zcp init"`, reload code-server). Each tied to an ACx:

1. AC1 — in the agent session on `zcp`: `zerops_env action=set serviceHostname=zcp variables=["ZCP_RETEST_PROBE=hello"]` — expect: success with `stored:[{key:"ZCP_RETEST_PROBE",…}]`, NO `Field 'sensitive' (field is required)`; in the Zerops GUI the var appears under the service's env as a sensitive (masked) variable; then `zerops_env action=delete serviceHostname=zcp variables=["ZCP_RETEST_PROBE"]` — expect: success.
2. AC5 — `zerops_env action=set serviceHostname=app variables=["NODE_ENV=production"]` on a service whose zerops.yaml bakes `NODE_ENV` (any runtime with `run.envVariables`; skip if none handy) — expect: error mentioning `zerops.yaml run.envVariables` (not a raw "Deleting system userData is forbidden").
3. AC2 + P4 (in-container delivery of a `sensitive:true` var) — `zerops_workflow action="git-push-setup" service="<dev service>" remoteUrl="https://github.com/<you>/<repo>" gitToken="<fine-grained PAT>"` — expect: `gitPushState: configured` (the handler writes `GIT_TOKEN` sensitive:true and then authenticates a FRESH SSH session with it — this is the live proof that sensitive vars reach the container); re-run with the same inputs — expect: no-op / configured again, no error.
4. AC3 — `zerops_workflow action="export" …` (or launch-production dry path) on a service with a `zerops_env set` var — expect: `envSecrets` in the bundle lists that key (placeholder per classification) and NOT `hostname`/`PATH`/yaml-baked keys.
5. AC4 — `zerops_discover includeEnvs=true` — expect: the runtime's yaml-baked vars listed once each with `source: "zerops.yaml"`; no key appears twice.

## What changed
- S1: `platform.Client.CreateServiceEnvVar(…, sensitive bool)` is hand-rolled (`sdkBase.Post` on the SDK transport) because the pinned zerops-go body lacks the platform's now-REQUIRED `sensitive` field (drift guard `TestSDKUserDataBody_StillLacksSensitive`); both ZCP service-scope writers (`ops.EnvSet` service branch, `ops.EnvSetSecretService`) send `sensitive:true` (= the old masked-SECRET behavior); yaml-baked keys hit by set/delete (`userDataDeleteForbidden`) translate to the existing "yaml owns the key" guidance.
- S2: `platform.ServiceEnvType` = `USER|SYSTEM`; app-version userData classification `USER`→yaml-baked / `SYSTEM`→intrinsic; `ops.FetchServiceUserEnvs` = slim `/env` USER minus the app-version USER keys (error, not empty, when the yaml fetch fails on a live runtime) feeds export/launch `envSecrets`; discover lists a yaml-baked key once; prose + fixtures migrated; e2e round-trip + canary.

## Rollback
`git revert -m 1 db3c3a15 && git revert -m 1 c7d3847e && git revert 34687b3a` (reverts S2 merge, S1 merge, spec promotion — range 34687b3a..db3c3a15 from Run State `integration:`).
After revert: service-scope env writes are broken again (platform contract) — this rollback only makes sense if the platform reverts its API change.

## Docs
Spec §§ touched (promoted at GATE 1): `docs/spec-zerops-env-lifecycle.md` §1, §6, §7, §9 (PENDING-3/4 notes), §12; `docs/spec-workflows.md` git-push-setup bullet + P-LP-14; `docs/spec-env-handling.md` §4.

## Results — assembler-driven run, 2026-08-22 (VPN to localflow; `zcp` container on `v9.151.0-10-g2a04465d`)

Every Drive call went through the container's own `zcp serve` (MCP stdio over SSH) — the exact path the in-container agent uses.

| Step | Result | Evidence |
|---|---|---|
| Run table | PASS (ASSEMBLE battery) | race/lint/vet clean; live e2e round-trip + canary PASS on the eval project |
| Drive 1 (AC1) | PASS | `zerops_env set serviceHostname=zcp ZCP_RETEST_PROBE=hello skipRestart=true` → `stack.updateUserData FINISHED`, no `Field 'sensitive'` error; REST record `type:USER sensitive:true content:hello`; `delete` → FINISHED. Default path (auto-restart) on `probedev`: `set PROBE_USER_VAR=hello-user` → FINISHED + restart FINISHED, record `USER sensitive:true` |
| P4 (fresh-session delivery) | CONFIRMED | fresh `ssh zcp 'printf %s "$ZCP_RETEST_PROBE"'` → `hello` ~10 s after the write, no restart — the mechanism git-push-setup 6c relies on |
| Drive 2 (AC5) | PASS | `set probedev NODE_ENV=production` → `INVALID_PARAMETER` "owned by probedev's zerops.yaml run.envVariables …" (0.1 s, no mutation); `delete PROBE_YAML_VAR` → "yaml-baked … cannot be deleted at service scope"; REST records unchanged |
| Drive 3 (AC2 live git-push-setup) | PASS (with Karel's PAT) | first attempt on `probedev` → `INVALID_PARAMETER` "mode dev does not support push-git" (by design — git-push-setup needs Standard/Simple); re-run on throwaway SIMPLE-mode `probesimple` (bootstrapped classic → import → provision → close): `git-push-setup service=probesimple remoteUrl=https://github.com/krls2020/eval2 gitToken=<PAT>` → `gitPushState:configured` (1.7 s; identity `krls2020`, origin synced, GIT_TOKEN written, fresh-session auth passed); same call again → `rotated:true` configured (1.5 s); REST `GIT_TOKEN type:USER sensitive:true` (len 93, never printed); fresh `ssh probesimple` sees it; repo untouched (dry-run probe only) |
| Drive 4 (AC3 export) | PASS | after deploying the yaml to `probesimple`: export call 2 → `classify-prompt` lists only project `ZCP_API_KEY` + warns about user-set `PROBE_USER_VAR` (no yaml-baked `PROBE_YAML_VAR`/`NODE_ENV`, no SYSTEM key, no `GIT_TOKEN`); call 3 with `envClassifications` → `publish-ready`, bundle `envSecrets: {PROBE_USER_VAR: <@generateRandomString(<32>)>}` |
| Drive 5 (AC4) | PASS | `probedev` deployed with `run.envVariables {PROBE_YAML_VAR, NODE_ENV}` (self-deploy, ACTIVE 1m3s) → `zerops_discover service=probedev includeEnvs=true` lists both once with `source:"zerops.yaml"`, intrinsics plain, `PROBE_USER_VAR` without source; REST shows the mirror as `USER sensitive:false` |

Cleanup done: `zerops_delete` `probedev` + `probesimple` (both `stack.delete FINISHED`, metas pruned, the PAT went with the service env); temp scripts removed from `zcp:/tmp`.
