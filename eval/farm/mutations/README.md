# Mutation batch — planted regressions for the eval farm gate

Each `.patch` here plants exactly ONE regression in zcp. None is ever applied
to this branch — they are build inputs for a mutated *candidate* binary the
farm runs against the gate set (`eval/farm/gate-set.txt`,
`docs/spec-scenarios.md §9.3`). Before a batch verdict is trusted to change
zcp, every patch must have been run once (`docs/spec-scenarios.md §9.4`,
`docs/spec-eval-farm.md §3.3`) and shown to fail ONLY its named cell — every
other gate cell must still pass. `TestMutationPatches_ApplyCleanly` and
`TestMutationPatches_TouchOnlyDeclaredFiles` (`internal/eval/farm/mutations_test.go`)
keep the patches applying cleanly and the table below honest about what each
one touches.

Runbook for building and running a mutated candidate: see "Mutation batch" in
`eval/farm/README.md`.

## The four mutations

| patch | planted regression | must fail | every other gate cell | files touched |
|---|---|---|---|---|
| `01-env-set-no-restart.patch` | `zerops_env action=set` no longer restarts the affected services — `applyAutoRestart` behaves as if `skipRestart=true` always | B3 `env-service-scope-pair` (`containerCheck` on the running process) | still passes | `internal/tools/env.go` |
| `02-subdomain-auto-enable-off.patch` | the dev-server-start hook (`internal/tools/dev_server.go`'s `ensurePublicAccess` call after a successful start) never runs — no dev-only runtime ever gets a subdomain from that hook, even though the deploy hook (`deploy_local.go`/`deploy_ssh.go`/`deploy_batch.go`/`workflow_record_deploy.go`) is untouched | A1 `api-node-postgres-classic-dev` (`toolArg{max:0,call:zerops_subdomain}` + `liveness`: the subdomain can only come from this hook — dev-only, deferred-start, no listener at import) | still passes — every other cell's subdomain comes from the import flag or the deploy hook, neither touched by this patch | `internal/tools/dev_server.go` |
| `03-mount-from-cwd.patch` | deploy preflight resolves the source mount from the process cwd's parent directory instead of `projectRoot` | B6 `mount-edit-deploy` (`toolArg` always `zerops_deploy{workingDir∈/var/www/appdev}` + `liveness`) | still passes | `internal/tools/workflow_checks_deploy.go` |
| `04-adopt-wrong-stage-hostname.patch` | the bootstrap/adopt close path writes `ServiceMeta.StageHostname` as the dev hostname instead of the actual stage hostname | A8 `adopt-existing-standard-pair` (`meta` stageHostname=appstage) | still passes | `internal/workflow/bootstrap_outputs.go` |

## Unit tests each patch breaks (expected — the patch is for the farm, not for `go test`)

- `01-env-set-no-restart.patch`: `TestEnvSet_ProjectScope_NoShadow_Live`.
- `02-subdomain-auto-enable-off.patch`: `TestDevServerStart_Success_EnablesSubdomainOnce`
  in `internal/tools` (1 test; live-verified against this patch — every other
  `internal/tools` test still passes).
- `03-mount-from-cwd.patch`: every `TestDeployPreFlight_*` plus
  `TestDeployBatch_CrossDeploy_PreflightReadsSourceMounts`,
  `TestDeployRecordsServesHTTP`, `TestHandleSetDefaultSetup_SetupNotInYAML_ReturnsRequiresSetupInput`
  in `internal/tools` (20 tests).
- `04-adopt-wrong-stage-hostname.patch`: `TestWriteBootstrapOutputs_ExpansionPreservesExistingFields`,
  `TestWriteBootstrapOutputs_LocalMode_KeyedByDevHostname`,
  `TestWriteProvisionMetas_ExpansionPreservesExistingFields` in `internal/workflow`.
