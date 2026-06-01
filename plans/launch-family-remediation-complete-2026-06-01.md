# Launch-Family Remediation — COMPLETE (2026-06-01)

All 13 open findings from `plans/launch-family-validation-verdict-2026-05-29.md`
(launch-production + export + cicd + git-push) are now **implemented, tested, and
committed** on branch `launch-family-fixes` (off `session-lifecycle-rebuild`).

**Gates (whole-repo):** `go build ./...` ✓ · `go test ./... -race` ✓ ·
`make lint-local` (strict lint + atom-tree + template-vars) → **0 issues**.
Every fix ships RED→GREEN pins. No `make install` / `make release`.

## Findings → fix → pin

| ID | Sev | Fix (commit) | Pin |
|---|---|---|---|
| LAUNCH-1 | crit | pipeline check keys on PROD hostname, iterates all runtimes; resume + state persist `RuntimeProds`; no silent "configured" (`f7027719`) | `TestExecuteLaunchPipelineCheck_SourceNotEqualProd`, `_RuntimeNotInImport_NotSilent`, `_MultiRuntime`, `TestRuntimeProdsFromBundleInputs` |
| LAUNCH-2 | high | existing-project routes through shared `finalizeImportedRuntimes` (detect ImportError → poll → pipeline-check); fixed latent new-project audit-on-poll-fail (`89663313`) | `TestLaunchExistingProject_ImportError_ReportsFailed` |
| LAUNCH-3/4 | high | read-side source-control gate iterates `resolveLaunchRuntimes`; `missingScopeFields` accepts Promotables-only; first multi-runtime handler test (`6741e2ca`) | `TestHandleLaunchProduction_MultiRuntime_ReadSideGate_FiresOnUnconfiguredB` |
| EXPORT-1 | high | `meta.ModeFor(targetService)` instead of `meta.Mode` (stage-half resolves ModeStage) (`1347209e`) | `TestHandleExport_StageHalf_ResolvesModeStage` + `service_meta_test` projection |
| GAP4-1 | high | `BuildGitOriginSyncCommand` gains `test -d .git \|\| git init` guard (deploy-path parity) (`1347209e`) | `TestBuildGitOriginSyncCommand_Shape` |
| cicd-DEAD | med | deleted dead `internal/ops/cicd` (live path uses bare ZEROPS_TOKEN) (`1347209e`) | — |
| XCUT-2 | high | git-push-setup stamps `configured` only on confirmed terminal restart; else `ErrAPITimeout` + recovery (`f05115ed`) | `TestGitPushSetupContainer_RestartPollFails_NoStamp` |
| WF-1 | high | `DeployIntent.Resolve` reads `SetupName`/`StageSetupName` (fallback to recipe convention); renamed setup flows to committed CI (`3dd65d4c`) | `TestResolve_Table` renamed-setup cases |
| DEPLOY-3 | med | shared `gitPushEnvRefPreflight` (env-ref preflight on local git-push too) (`ecddb2f9`) | container-path pins (shared helper) |
| TOPO-1/WF-2/DELIV-2 | high | `NewServiceMeta` constructor (routes adopt_local) + `normalizeDeployDims` on read (atoms fire for local-stage) (`d9dc0895`) | `TestTOPO1_SnapshotHealsEmptyDeployDims`, `TestNewServiceMeta_StampsThreeDims` |
| GAP0-1/GAP0-2 | high | carry runtime USER-set env (Type=SECRET) as `envSecrets`; secret-safe default (unclassified → REPLACE_ME, never leak) + named warnings (`9f3e4e83`) | `TestComposeServiceEnvSecrets_Buckets`, `TestBuildExport_EmitsServiceEnvSecrets`, `TestBuildLaunch_EmitsPerRuntimeServiceEnvSecrets` |
| WF-1-export-variant | cleanup | deleted the inert dev/stage variant dimension (composer discarded it); hostname determines the half (`50375605`) | export 7→6-state narrowing; scenario/golden updates |
| (XCUT-1, WF-5b) | — | already fixed by `session-lifecycle-rebuild` (P2 lock, I10 launch demotion) | verified in the verdict pass |

## Decisions taken (owner-confirmed)
- **Multi-runtime launch:** wired fully (read-side gate over all runtimes + tests).
- **cicd package:** deleted (Clean Code; live path correct with bare ZEROPS_TOKEN).
- **GAP0-1:** full implement; **GAP0-2 delivered via secret-safe default + named
  per-env warnings** (the agent reclassifies through the existing `EnvClassifications`
  input) rather than dedicated blocking classify-prompt rows — the safe default means
  service envs never block publish-ready or leak on first call.

## Live validation (eval-zcp, current code)
- Empirical bug repros (pre-fix): LAUNCH-1 deterministic; XCUT-1 200/200 damage.
- GAP0-1 premise confirmed live: `zerops_env set serviceHostname` persists as Type=SECRET.
- Launch composer → `CreateAndImportProject` ACCEPTED (basic + with envSecrets:
  plain-config carried verbatim, external-secret → REPLACE_ME, no leak).
- Whole-family full launch cycle (runtime + managed db + envSecrets → build → ACTIVE):
  see the `familyval` live test (`internal/tools/zz_familyval_live_test.go`,
  `//go:build familyval`, operator-run).

## Backward-compat
- `WorkflowInput.Variant` retained as deprecated **accepted-but-ignored** (the MCP
  input schema is `additionalProperties:false`; removing it would reject installed
  agents/recipes that still pass `variant`).
- ServiceMeta normalize-on-read heals existing on-disk metas (no migration).

## Not done by design / out of scope
- WF-5 *core* (launch's parallel state model) is the deliberate I10 overlay design,
  not a bug (WF-5b shadowing was fixed by session-lifecycle-rebuild).
