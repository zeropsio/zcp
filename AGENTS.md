# ZCP — Zerops Control Plane

Go binary: MCP server + CLI for Zerops PaaS.
Read private tooling/access policy in `CLAUDE.local.md` when present; worktrees also check the main checkout (`git worktree list`). Never commit it/credentials or edit it without a local-policy request.

## Where knowledge lives

Map only. Authority: tests → code → specs → this map. Homes:

- Behavior → test + spec; mechanism → code/doc-comment; design → `docs/spec-*.md`, promoted before citing.
- Platform facts → `../zerops-docs/` or live, never training data; recipe gotchas → `internal/knowledge/recipes/<slug>.md`; universal mechanics → `internal/content/atoms/`.
- Disciplines → `AGENTS.md`; dev-loop ops → `CLAUDE.local.md`.
- `plans/` is a local, untracked scratch directory for whoever uses it, never authoritative.

Read the home before changes:

- `docs/spec-workflows.md` — workflows, envelopes, deploy/close modes, public access, recovery, export, launch.
- `docs/spec-work-session.md` — PID sessions, compaction, auto-close.
- `docs/spec-knowledge-distribution.md` — atom corpus/authoring.
- `docs/spec-knowledge-architecture.md` — tools-only retrieval.
- `docs/spec-content-surfaces.md` — recipe quality/fact classification.
- `docs/spec-authoring-boundary.md` — maintainer domain/gate.
- `docs/spec-guided-mode.md` — guided init/marker/block/skill.
- `docs/spec-architecture.md` — layers/dependencies/packages.
- `docs/spec-local-dev.md` — local/container development.
- `docs/spec-capture-inspector.md` — evidence/UI security/CLI boundary.
- `docs/spec-welcome-mode.md` — onboarding/bridge/bootstrap/skills/wizard.
- `docs/spec-scenarios.md` — walkthroughs/eval matrix (`scenarios_test.go`, `TestEvalMatrix_*`).
- `docs/spec-testing-architecture.md` — test/eval tiers/drift guards/manifest.
- `docs/spec-eval-farm.md` — bundles/runs/safety/oracles/verdicts/observer/console.
- `docs/schema-integration.md` — validation ownership.
- `docs/spec-oss-port-flow.md` — foreign OSS porting gate.
- `docs/spec-dataconsole.md` — data surfaces/write authority/families/fidelity/install.
- `docs/spec-dataconsole-testing.md` — proof coverage/matrix/live lanes/versions.
- `docs/spec-mate.md` — envelopes/install/client/lifecycle/SSH git/fork; `zcp git` in the dev service, relayed credentials (§6.8); trust and credentials (§3); org mode, the Chief (§2.11); fork rules → `../z3/docs/internals/zerops/`.
- `docs/spec-telemetry.md` — ingest/trusted IP.
- `schema.URLs` — ZCP_API_HOST-derived live schemas (`schema.CanonicalAPIHost` for dev); `internal/platform/errors.go` — error catalog.

## Architecture

L1 `platform/` → L2 `topology/` → L3 peers `workflow/` + `ops/` → L4 `cmd/`, `server/`, `tools/`. Type strings at entry boundaries; shared types start in topology, never workflow. Dependencies → `docs/spec-architecture.md`, `.golangci.yaml::depguard`, `internal/topology/architecture_test.go`.

## TDD — Mandatory

RED → GREEN → REFACTOR, with the failing test run before the implementation exists; pure refactors skip RED, keep affected layers green. Table-driven `Test{Op}_{Scenario}_{Result}`; `t.Parallel()` only with safe global state (document why not); long tests honor `testing.Short()`.

Checks: platform/ops type/interface → unit/tool/integration/e2e; handler → tool/integration/e2e; new tool → also `annotations_test.go`. Layers: `./internal/...`, `./internal/tools/...`, mock `./integration/`, live `./e2e/ -tags e2e`.

## Commands

- `make setup` — dev tools and git hooks.
- `make check-changed` — affected short tests and native lint from the staged diff (also pre-commit); `scripts/check-changed.sh --base <ref>` checks a review range; `--plan` prints selection and unresolved ownership.
- Complete acceptance runs in CI; `make test-short`, `make test-race`, `make lint-fast` and `make lint-local` remain manual all-package diagnostics. Pre-push does not repeat lint.
- Knowledge sync (gitignored markdown, pull before build): `zcp sync pull recipes [<slug>]` / `pull guides`; `zcp sync push recipes <slug> [--dry-run]` / `push guides` → GitHub PR; `zcp sync cache-clear [<slug>]`; `zcp sync recipe {create-repo,push-app,publish,export}`. Loop: pull → edit → push → merge → cache-clear → pull. Config: `.sync.yaml`, `.env STRAPI_API_TOKEN`.

## How to work

- Resolve agent-facing service hostnames to IDs internally; shell/SQL composition uses POSIX single-quoting via `shellQuote()`, never `fmt.Sprintf`.
- Errors: `fmt.Errorf("op: %w", err)`, never bare `return err`. Split by cohesion; English code/comments/docs/commits; atomic commits, no Co-Authored-By or amend; verify each refactor phase.
- Renames: search calls/types/strings/tests separately, no AST assumptions.
- No global mutable state (except `sync.Once` init); no `go.mod` replace directives; no `panic()` — return errors; use concrete known types. Never add parallel tests to global-state packages without thread safety; copy under mutex, release before I/O.
- Atoms: single-owner platform mechanics, no spec IDs/handler verbs/invisible fields/plan paths/env-only title qualifiers. Extend `internal/content/atoms_lint{,_axes}.go`, never per-topic atom tests (`TestAtomAuthoringLint`, `docs/spec-knowledge-distribution.md` §11).
- Recipe-specific findings → `internal/knowledge/recipes/<slug>.md`; `zcp sync push recipes <slug>`, never atoms/audits (`docs/spec-content-surfaces.md`).

## Traps

Read the home before new call sites:

- Authoring registers inside its gate; core/authoring dependencies use the allowlist (`docs/spec-authoring-boundary.md`, `TestAuthoringBoundary_*`).
- Guided state is local, never runtime.Info; exclude authoring (`docs/spec-guided-mode.md`, `TestBuildAgentsMD_AuthoringExcludesGuided`).
- GitPushState owns deploy delivery, not close mode (`docs/spec-workflows.md` §4.3/S5, `TestResolve_ConfiguredDrivesGitPushDelivery`).
- Self/cross deploy differ; DM-2 requires deployFiles=[.], no client stat-check (`docs/spec-workflows.md` §8 DM-1…DM-5).
- Runtime meta is pair-keyed via ManagedRuntimeIndex/FindServiceMeta, never hostname alone (`docs/spec-workflows.md` §8 E8).
- Non-idempotent APIs check REST state first (`ops.Subdomain`, `docs/spec-workflows.md` §8 O3); validation/classification/authority → `docs/schema-integration.md`.
- Production launch is pipeline-first; staged secret is ZCP_LAUNCH_TOKEN, not ZEROPS_* (`docs/spec-workflows.md` §10/§10.2b/P-LP-14).
- Export uses live git remote, schema validation and type-variant HA, never mode: (`docs/spec-workflows.md` §9 E1-E6).
- Recover via action=status/envelope, errors stay leaf payloads (`docs/spec-workflows.md` P4).
- Searches lag writes; absence is not truth. Freshen by-ID/direct reads; HasDeployedCode requires active appVersion source from direct reads (`docs/spec-workflows.md` §3.5).
- Never gate corrective zerops_deploy; import override and DM-2 are hard gates (`TestDeployLocal_*Proceeds`, `TestImport_OverrideOnFailedRequiresAck`).
- Only ops.ProjectActivity/ops.IsProcessLive owns busy truth: live cancelable process, never appVersion phase/build presence; adopt freshens GetProcess (`TestProjectActivity`, `TestAdoptGate_*`).
- Never fabricate user credentials; surface missing/invalid contracts; only platform mints delegated launch tokens, never expose in response/state/audit (`TestConvertError_CredentialContract`).
- ensurePublicAccess owns access; DTO fields are not import intent; strip in prod (`docs/spec-workflows.md` §8 O3/E9, `TestEnsurePublicAccess_*`, `TestServiceEligible_*`, `TestBuildLaunchBundle_StripsSubdomainAccess`).
- Ingest clientIP uses authoritative X-Real-IP, never client XFF (`docs/spec-telemetry.md` §6, `TestClientIP_SpoofedXFF_Ignored`).
- Use workflow.IsOpen/DeriveCloseState, never raw ClosedAt emptiness (`TestNoRawClosedAtReads`).
- Repo identity/emission uses topology.CanonicalRepoURL; store meta.RemoteURL verbatim (`TestCanonicalRepoURL*`).
- Wrap SSH errors via tools.withSSHStderr to preserve Output (`TestGitPushSetupContainer_ProbeFailure_SurfacesGitStderr`).
- Agent markdown uses zerops_knowledge uri=, never bare zerops:// or resources capability (`TestNoBareZeropsURIInAgentContent`, `TestServer_DoesNotAdvertiseResourcesCapability`).
- Markers use line-anchored content.IndexMarkerLine; damaged/absent blocks prepend without losing content (`TestNoRawMarkerMatching`, `TestInit_MidLine*`, `TestInit_Damaged*MD_NoDataLoss`).
- Init steps default fatal; non-deliverables register step.degraded, never repair environment-owned broken paths (`TestRun_SkillRoots_UnusableRoot_DegradesNotFatal`, `TestRun_RequiredStepFailure_Aborts`).
- Tool 2nd return stays nil; structuredContent strips guidance; use text envelopes (`docs/spec-mate.md` §1, `TestNoStructuredContentOnToolResults`).
- Gate every Mate effect on runtime.Info.MateEnabled, including OFF convergence (`docs/spec-mate.md` §2.0, `TestRunNginx_MateDisabled_RendersNoMateSurface`, `TestRun_MateDisabled_NoUnitFile_NoOp`).
- Unknown mate serve flags are fatal; use LaunchEnvLines for settings older bundles lack (`docs/spec-mate.md` §2.2, `TestServeArgv_FlagsAreContract1`, `TestStart_Mate_Argv`).
- A `zcp git` relay frame is answered only from zcp-side records and the call's own argv, never the dev checkout's config or ServiceMeta.RemoteURL; HQ mints for any repository the application mounts (`docs/spec-mate.md` §6.8, `TestBrokeredGit_Relay_RemoteAddedInCheckoutConfig_Declined`).
- A new zcp-devsup ctl verb or flag is a capability: send it only when `ctl version` lists it; pool images pin older supervisors whose ctl fails on what it does not know (`TestDevServer_SupervisorWithoutEnvCapability_NotSentEnv`).
- JSON stdout only, debug stderr; cmd/ CLI exempt (`TestNoStdoutOutsideJSONPath`).
- tools/eval/cmd call ops.*, never client ListServices/GetServiceEnv/GetProjectEnv (`TestNoDirectClientCallsInToolsEvalCmd`).
- vscode-bootstrap template edits require BootstrapExtVersion bump (`TestBootstrapExtVersion_ParityWithManifest`).
- Service userData writes use hand-rolled platform.CreateServiceEnvVar with sensitive, never SDK UserDataPost/Put (`TestSDKUserDataBody_StillLacksSensitive`, `TestCreateServiceEnvVar_SendsSensitiveInBody`).
- Data Console writes authorize caller X-Write-Token via safety.Policy.AuthorizeWrite, never read bearer/X-Confirm (`docs/spec-dataconsole.md` §5, `TestWriteToken_DualClient_CallerBound`).

## Maintenance

Keep only traps an agent can violate before checks, or knowledge routes. One line per route/trap, cite its spec/test; traps ≤~30 lines. No cached values/summaries, dates, hashes, quotes or journals. Delete stale/redundant content; history is git.

Error codes → `internal/platform/errors.go`. Global state: `// non-parallel: <reason>` in the package seed test.
