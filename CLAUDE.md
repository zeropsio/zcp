# ZCP — Zerops Control Plane

Single Go binary: MCP server + CLI for managing Zerops PaaS.

---

## Source of Truth

```
1. Tests (table-driven, executable)    ← AUTHORITATIVE for behavior
2. Code (Go types, interfaces)         ← AUTHORITATIVE for implementation
3. Specs (docs/spec-*.md)              ← AUTHORITATIVE for workflow design
4. Plans (plans/*.md)                  ← TRANSIENT (roadmap, expires)
5. CLAUDE.md                           ← OPERATIONAL (invariants, conventions)
```

**CLAUDE.md tracks invariants, not structure.** Don't list packages, file
paths, or struct fields here — those drift; `ls`, `grep`, and AST do not.
Add a fact only if it can't be derived by reading code.

Key specs:
- `docs/spec-workflows.md` — workflow steps, invariants, envelope/plan/atom pipeline
- `docs/spec-work-session.md` — per-PID Work Session, compaction survival, auto-close
- `docs/spec-knowledge-distribution.md` — atom corpus authoring contract
- `docs/spec-scenarios.md` — per-phase walkthroughs, pinned by `internal/workflow/scenarios_test.go`
- `docs/spec-local-dev.md` — local-machine vs container differences
- `docs/spec-content-surfaces.md` — recipe content-quality contract (seven surfaces)
- `docs/spec-authoring-boundary.md` — maintainer-only authoring domain (`internal/authoring/`), ZCP_AUTHORING gate, boundary laws + contracts
- `docs/spec-oss-port-flow.md` — gated `zerops_port` tool (foreign OSS → curated recipe): agent-driven port→debug→harden→capture loop, recon/fix-class/escalation, FitCeiling, capture to `zeropsio/recipes`

Live Zerops schemas (authoritative for YAML field validation) — fetched
**host-derived from `ZCP_API_HOST`** at runtime (`schema.URLs`), pinned to
`schema.CanonicalAPIHost` (`api.app-prg1.zerops.io`) for dev tooling + the empty-host default:
- import: `…/api/rest/public/settings/import-project-yml-json-schema.json`
- zerops.yaml: `…/api/rest/public/settings/zerops-yml-json-schema.json`

Error codes catalog: `internal/platform/errors.go`.

---

## TDD — Mandatory

RED → GREEN → REFACTOR. Pure refactors skip RED — verify all layers stay green.

**Change impact — tests at every affected layer must pass:**
- Interface/type change in `platform` or `ops` → unit + tool + integration + e2e
- Tool handler change → tool + integration + e2e
- New MCP tool → tool + `annotations_test.go` + integration + e2e

Layers: unit (`./internal/...`), tool (`./internal/tools/...`),
integration (`./integration/` mock), e2e (`./e2e/ -tags e2e` real Zerops).

Test rules: table-driven; naming `Test{Op}_{Scenario}_{Result}`;
`t.Parallel()` only where global state allows (document why not);
long tests check `testing.Short()`. Automated tiers: edit → turn →
commit → CI; see `.claude/settings.json`.

---

## Commands

```
make setup             Bootstrap dev env (lint + git hooks)
make lint-fast         ~3s native fast linters
make lint-local        ~15s full golangci lint
go test ./... -short   All tests fast
go test ./... -race    All tests with race detector
```

### Knowledge sync — recipe/guide markdown is gitignored, pull before build

```
zcp sync pull recipes [<slug>]                Pull from Strapi
zcp sync pull guides                          Pull from zeropsio/docs
zcp sync push recipes <slug> [--dry-run]      Push edits → GitHub PR
zcp sync push guides                          Push guide edits → PR
zcp sync cache-clear [<slug>]                 Invalidate Strapi cache
zcp sync recipe {create-repo,push-app,publish,export}  Recipe repo lifecycle
```

Workflow: pull → edit `.md` → push → merge → cache-clear → pull.
Config: `.sync.yaml` + `.env STRAPI_API_TOKEN`.

#### TEMPORARY: in-repo mailpit recipe (remove once it lands in Strapi)

The Mailpit recipe (`internal/knowledge/recipes/mailpit.{md,import.yml}`) is
**committed in-repo as a stopgap** because Mailpit is not yet authored in the
Strapi recipe catalog — without it the bootstrap recipe matcher can't surface
"mailpit". Both repos are wired the normal way (`.md` `repo:` →
`zerops-recipe-apps/mailpit-app`; `.import.yml` `buildFromGit:` →
`zeropsio/recipe-mailpit`). `sync pull` is additive (never deletes), so the
committed files survive sync; `.md` is force-tracked via a `!`-allowlist in
`.gitignore` (`.import.yml` is tracked like every recipe's).

**Revert once Mailpit exists in Strapi** (the next `sync pull` overwriting
`mailpit.md` on disk — a git diff — is the signal):
1. Delete the `!internal/knowledge/recipes/mailpit.md` line from `.gitignore`.
2. `git rm --cached internal/knowledge/recipes/mailpit.md` (it becomes a normal
   gitignored/synced `.md`).
3. `mailpit.import.yml` needs nothing — it's already in the standard committed
   form sync refreshes for every recipe.

---

## Architecture — 4 layers + cross-cutting

```
┌──────────────────────────────────────────────────────────────┐
│  Layer 4 — ENTRY POINTS                                      │
│  cmd/zcp/, internal/server/, internal/tools/                 │
│  MCP handler boundary, CLI entrypoints; convert input        │
│  strings → typed (from layer 2) at the boundary.             │
└──────────────────────────────┬───────────────────────────────┘
                               ↓
┌──────────────────────────────────────────────────────────────┐
│  Layer 3 — ORCHESTRATION + OPERATIONS  (peer layers)         │
│  internal/workflow/  ←/→  internal/ops/                      │
│  workflow: engine, sessions, atoms, briefing, route logic.   │
│  ops: discrete platform operations.                          │
│  PEERS: must NOT import each other; share types via layer 2. │
└──────────────┬─────────────────────────────┬─────────────────┘
               ↓                             ↓
        ┌──────────────────────────────────────────┐
        │  Layer 2 — ZCP TOPOLOGY VOCABULARY       │
        │  internal/topology/                      │
        │  Mode, RuntimeClass, CloseDeployMode,    │
        │  GitPushState, BuildIntegration +        │
        │  predicates + aliases.                   │
        │  ZERO non-stdlib imports.                │
        └──────────────────┬───────────────────────┘
                           ↓
┌──────────────────────────────────────────────────────────────┐
│  Layer 1 — RAW PLATFORM API                                  │
│  internal/platform/                                          │
│  Zerops API client, ServiceStack, EnvVar, Process.           │
│  No ZCP-specific concepts. Imports stdlib + 3rd-party only.  │
└──────────────────────────────────────────────────────────────┘
```

**Dependency rule** (pinned by `.golangci.yaml::depguard` +
`internal/topology/architecture_test.go`):

| Rule | Reason |
|------|--------|
| `topology/` imports stdlib only | Foundational vocabulary |
| `platform/` imports no internal/ packages | Bottom of stack |
| `ops/` does NOT import `workflow/`, `tools/`, `authoring/` | Peer/upper |
| `workflow/` does NOT import `ops/`, `tools/`, `authoring/` | Peer/upper |
| core does NOT import `authoring/` (composition root: `server/`) | Authoring boundary L1 |
| `authoring/` imports core only via allowlist (topology, schema, knowledge, platform, sync) | Authoring boundary L2 |
| New shared type → `topology/` first, never `workflow/` | Promotion rule |

**Cross-cutting packages** (peer-of-equal-level, not strict layered) live
under `internal/`; key non-obvious ones: `auth/` runs pre-engine and talks
to platform directly, `authoring/` is the ZCP_AUTHORING-gated maintainer
domain (recipe v3 engine + publish lifecycle + run analysis — see
`docs/spec-authoring-boundary.md`), `service/` exec wrappers are
name-collision-distinct from `topology/`. Full list via `ls internal/`.

Spec: `docs/spec-architecture.md` — per-package mapping + examples.

---

## Conventions

- **The authoring boundary is mechanical, not conventional** —
  `internal/authoring/` (recipe v3 engine + recipe-repo publish lifecycle +
  run-analysis harness + OSS port flow `zerops_port`) is the maintainer-only
  authoring domain. Its MCP surface registers ONLY under `ZCP_AUTHORING=1`
  (single owner `runtime.Info.Authoring`, resolved once by `runtime.Detect`;
  gate is activation, not security); end
  users never see the tools, pay their schema context cost, or read strings
  naming them. Two compile-time laws: core never
  imports authoring (composition root = `internal/server`; `cmd/`
  sits outside the enforced surface by design); authoring
  imports core only via the enumerated allowlist (notably NOT
  `workflow`). Runtime coupling is exactly the C1-C5 contract
  list (probe interface, schema provider, state namespaces, knowledge
  corpus, core-sync surface). Extending any allowlist
  = deliberate contract change: depguard rule + test allowlist + spec in
  one commit. New authoring tools self-register inside the gate (the
  `recipe.Register` model) — never as fields/values on a core tool's input
  schema. Pinned by `TestAuthoringBoundary_*`, depguard
  `core-not-authoring` + `authoring-allowlist`,
  `TestServer_AllToolsRegistered` / `_AuthoringToolsRegistered`,
  `TestAnnotations_AuthoringTools*`. Spec: `docs/spec-authoring-boundary.md`.
  The v2 recipe sub-mode (workflow="recipe" handlers, `zerops_guidance`,
  `internal/content/workflows/recipe*`, the workflow recipe_* cluster,
  `zcp check`/`dry-run recipe` CLI, B-22/C-13 lint gates) was DELETED
  2026-06-12 — `workflow="recipe"` survives only as a redirect message.
- **Deploy config: three recorded dimensions + ONE derived ladder** —
  `ServiceMeta` carries `CloseDeployMode`, `GitPushState` (with `RemoteURL`),
  and `BuildIntegration`, each owned by one user-facing action (`close-mode` /
  `git-push-setup` / `build-integration`). The delivery MECHANISM is derived
  from the ladder (`topology.DeriveDeliveryState`): `GitPushState=configured`
  ⇒ PUSH is the terminal act of development for every close-mode except
  manual (the 2026-06-10 spec-git-delivery-target §2 supersession of the
  earlier "configured push can coexist with auto self-deploy close" cell —
  Karel-confirmed; that cell minted never-pushed `deploy` commits and made
  head-not-pushed the expected launch state). `CloseDeployMode` shrinks to
  done-ness OWNERSHIP (`auto`/`manual`; the legacy `git-push` value folds to
  `auto` at meta parse — `foldLegacyCloseMode`, one-way lazy migration).
  Direct deploys on a configured pair redirect to the push call
  (`repoDeliveryRedirect`; `breakGlass=true` escape preserves everything and
  flags container-ahead-of-repo). `BuildIntegration` requires
  `GitPushState=configured`. Pinned by `TestResolve_ConfiguredDrivesGitPushDelivery`,
  `TestParseMeta_FoldsLegacyGitPushCloseMode`, `TestRepoDeliveryRedirect_*`.
  Spec: `plans/spec-git-delivery-target-2026-06-10.md` §2/§3.
- **BuildIntegration is DECLARED; verification is EARNED** — the enum
  records WHICH integration the user chose (the handler returns
  `status:"declared"`, never `configured`); the sibling
  `BuildIntegrationVerifiedAt` records the last checkable signal ZCP
  observed (actions = workflow file present at the PUSHED head — valid only
  after the launch push-proof; webhook = platform integration-status read on
  the build target). The launch gate's check 6 + `trackTriggerMissingWarning`
  warn on declared-but-unverified; a bare declaration never suppresses them.
  Stamp site is the PUBLISH-side launch gate ONLY (read-side polls never
  mutate meta); changing the declared integration clears the stamp. The BI
  noop re-call returns the FULL handoff (stateless recompute — compaction
  recovery). Pinned by `TestHandleBuildIntegration_Configures` (no-stamp),
  `TestValidateLaunchSourceControl_DeclaredButUnverified_WarnsNotSuppressed`,
  `_LiveEarnPasses_SuppressesWarn`, `_EarnSkippedWhenPushProofFailed`,
  `TestHandleBuildIntegration_NoOpReCall_MatchesFirstCallShape`. Plan:
  `plans/production-transition-2026-06-10.md` (F1).
- **Deploy failure response carries structured classification** — every
  failed `zerops_deploy` populates `failureClassification` (category +
  likelyCause + suggestedAction + signals). Lives on
  `ops.DeployResult.FailureClassification` (build/prepare/init failures) and
  `tools.ErrorWire.FailureClassification` (transport/preflight). Agents read
  this FIRST; `buildLogs`/`runtimeLogs`/`failedPhase` are fall-through
  diagnostic depth. Categories live in `topology.FailureClass` (single
  canonical enum, peer to ops + workflow); classifier + pattern library in
  `internal/ops/deploy_failure*.go`. Pinned by `TestClassifyDeployFailure_*`,
  `TestPollDeployBuild_PopulatesFailureClassification`,
  `TestErrorWire_FailureClassification`. The git-push-setup probe (container +
  local) + origin-sync feed the SAME classifier — they MUST surface
  `SSHExecError.Output` (the git stderr `Error()` omits) via `withSSHStderr`,
  not swallow it (B6/F36 parallel-path with the deploy git-push path, fixed in
  29aa8521). **Every credential-class error** (`GIT_TOKEN_INVALID` /
  `GIT_TOKEN_MISSING`) carries the user-owned-credential contract appended by
  `convertError` (single owner: `errwire.go::appendCredentialContract`) — the
  agent surfaces it and asks the user, NEVER fabricates a token. Pinned by
  `TestGitPushSetupContainer_ProbeFailure_SurfacesGitStderr`,
  `TestConvertError_CredentialContract`,
  `TestClassifyDeployFailure_Transport` (git-repo-not-found signal).
- **Verify checks carry structured Recovery for actionable preconditions** —
  when an infrastructure precondition that the agent can fix is missing
  (e.g. subdomain access disabled), the failing CheckResult MUST carry a
  Recovery struct (tool + action + args) pointing at the exact next call.
  Skip status reserved for non-actionable transients (URL not yet resolved).
  Pinned by TestVerify_* cases asserting Recovery shape on http_root failure.
- **Hard gates protect only genuinely-destructive or impossible operations;
  a corrective redeploy is neither and is NOT gated** — the single hard gate
  is `zerops_import override=true` (wholesale replacement, which wipes
  containers/code/env/mounts) + the DM-2 self-deploy source-destruction check.
  It refuses when target services have failed-appVersion history unless the
  call carries `confirmDestructive: {operation, acknowledgedTargets,
  diagnosedFailureClass?}` matching the structured `wouldDestroy` payload
  returned in the first-call rejection — an ack that actually CLEARS the gate.
  A `zerops_deploy` redeploy of a FAILED / READY_TO_DEPLOY-with-failed-history
  target is **non-destructive** (the prior appVersion keeps serving until a new
  one activates) and is **never refused** — gating it was a category error that
  deadlocked recovery (the only thing clearing a non-running refusal was a
  successful deploy, which it blocked) and pushed the agent into the
  destructive override escape. The agent diagnoses from the
  `failureClassification` the failed-deploy response already carries (single
  owner: `ops.ClassifyDeployFailure`); it never re-fetches via a forced gate.
  When the surviving import-override gate DOES refuse, it emits a COMPLETE,
  non-authoring corrective (R6): the `wouldDestroy` payload carries the gate's
  own per-target `diagnoses[]` (failureClass + cause + `needsStartWithoutCode`,
  the latter from the target's live non-ACTIVE status) AND a structured
  `retryCall` (the same `zerops_import` call referencing the agent's own
  filePath/content — never regenerated YAML — with `override=true`,
  `confirmDestructive` pre-filled, and per-target `startWithoutCode:true`
  patchHints) so one re-call clears the gate and reaches ACTIVE. The
  WAITING_TO_BUILD stuck-build case is classified via a SearchProcesses FAILED
  build-process fallback in `LatestFailedAppVersionContext` (queued-vs-stuck
  guard). `dev_server` pre-spawn is a **precondition** check, not a refusal: a
  non-running target yields `ErrInvalidParameter` ("deploy it RUNNING first" —
  SSH needs a live container), which is not a deadlock because resolving it is
  a (now-ungated) deploy. `NonRunningRecovery` survives only as a NON-BLOCKING
  hint on the `verify::service_running` + `workflow_checks::checkServiceStatusAny`
  status CHECKS (FAILED/READY_TO_DEPLOY-with-history → `zerops_events` read-first,
  never override). Lives on `ErrDiagnosisRequired` (import) + `ErrInvalidParameter`
  (dev_server precondition) + `tools.DiagnosedDestruction` wire shape +
  `ops.LatestFailedAppVersionContext` helper; `topology.Recovery` unifies
  `ops.Recovery`/`tools.RecoveryHint`. Pinned by
  `TestImport_OverrideOnFailedRequiresAck`, `TestLatestFailedAppVersionContext_*`,
  `TestNonRunningRecovery_*`, `TestDeployLocal_*Proceeds` (the deadlock-fix
  regression guard), `TestDevServer_*ReturnsPrecondition` +
  `TestDevServer_RunningPasses`. Verdict + evidence:
  `plans/deploy-gate-category-error-2026-06-04.md`.
- **tools/eval reach platform via ops** — `client.ListServices` /
  `client.GetServiceEnv` is forbidden outside of `internal/ops/`,
  `internal/platform/`, and `internal/workflow/` (peer layer). Use
  `ops.ListProjectServices` / `ops.LookupService` / `ops.FetchServiceEnv`
  instead so caching, retries, and instrumentation land at one site.
  Pinned by `TestNoDirectClientCallsInToolsEvalCmd`.
- **Runtime meta is pair-keyed** — one `ServiceMeta` per dev/stage pair;
  stage is a field on the dev meta. Index via `workflow.ManagedRuntimeIndex(metas)`
  / `workflow.FindServiceMeta(stateDir, hostname)`; never key on `m.Hostname`
  alone. Pinned by `TestNoInlineManagedRuntimeIndex`. Spec: `spec-workflows.md §8 E8`.
- **Adopt route auto-derives the discover plan; the agent authors nothing** —
  `route=adopt` + `complete step="discover"` with an empty/omitted plan derives
  the plan from live discovery: every `adoptableServices()` runtime becomes an
  `isExisting` target, every managed service a shared `EXISTS` dep.
  `workflow.InferServicePairing` is its single production consumer (was orphaned
  when `cb63bf32` removed develop-start auto-adopt without re-wiring it into the
  explicit route). Pairing is never guessed: exactly two same-type adoptable
  runtimes return `ErrAdoptPairingChoice` with copy-pasteable standard-pair +
  independent-dev templates rather than silently committing two dev containers.
  Dispatch keys on `len(plan)==0` (so `plan:[]` derives too) and the explicit-plan
  adopt path is live-service-validated. Residual classic/recipe plan-authoring
  friction (SDK validates schema before the handler diagnostic) is backlogged in
  `plans/backlog/plan-schema-author-friction.md`. Pinned by
  `TestBootstrapCompleteAdoptPlan_*` + `TestHandleBootstrapComplete_Adopt*`.
- **Check-before-mutate for non-idempotent platform APIs** — read state via
  REST-authoritative endpoint, short-circuit when desired state holds.
  Canonical: `ops.Subdomain`. Spec: `spec-workflows.md §8 O3`.
- **The schema is the single client-side source of truth; topology classifies;
  the platform is the authority** — three owners, no overlap. Real operations
  validate live on the platform (`client.ValidateZeropsYaml` for deploy,
  `ImportServices` for import) with NO local schema involvement. Every
  client-side "does this instance accept X?" question (service-type / build-base
  / run-base existence, latest managed version, YAML structure, briefing stack
  list) is answered from the **`schema.Cache`** — host-derived (`schema.URLs(apiHost)`
  mirrors `platform.resolveEndpoint`; the runtime cache follows `ZCP_API_HOST` so
  validation hits the user's instance, `schema sync`/`check` pin
  `schema.CanonicalAPIHost`), 15-min TTL, **embedded-seeded so `Get` is never
  nil**, **poison-guarded** (`cache.go::rejectEmptyEnums` rejects an
  `HTTP 200 {error:502}` empty-enum body, keeps last-good). The schema-derived
  catalog (`*schema.Schemas` methods: `HasServiceType`/`HasRunBase`/`HasBuildBase`
  — equivalence-aware — + `ManagedBaseNames`) replaced the deleted `StackTypeCache`
  + `client.ListServiceStackTypes` stack-types API (its only unique field,
  version `Status`/deprecation, dropped: the curated schema already excludes
  deprecated, platform warns at deploy). **Managed/runtime/utility CLASSIFICATION
  is `internal/topology`** (`IsManagedService`/`IsRuntimeType`/`IsObjectStorageType`/
  `IsSharedStorageType` + storage-alias normalization via `canonicalStorageKind`;
  the schema owns existence, topology owns classification). **Composite-aware
  with bare fallback:** all type/base matching applies `topology.CanonicalBareForm`/
  `CanonicalBaseName` so an authored bare form (`nodejs@22`) matches a composite-only
  live schema (`alpine/nodejs@22`) and a hallucination still rejects (Sunday-release
  2026-05-18 composite migration). Export/launch validate against a
  **structure-only** compiled schema (`ValidateImportYAMLStructure` /
  `ValidateZeropsYAMLStructure`): volatile `services[].type` / `build.base` /
  `run.base` value-enums stripped in-memory, structural guards kept — those
  values come from a live `Discover` (already platform-valid). The schema
  endpoint shares the platform's availability, so there is NO schema-offline
  state to build fallbacks for. Refresh embedded + derive `active_versions.json`
  via `make schema-sync`; `zcp schema check` is the (reachability-gated,
  advisory-on-PR / required-on-cron) drift sentinel. Pinned by
  `TestEmbeddedSchemasSelfConsistent`, `TestValidateImportYAMLStructure_*`,
  `TestValidateZeropsYAMLStructure_*`, `TestNewCache_SeedsAndStaysCold`,
  `TestRejectEmptyEnums`, `TestURLs`, `TestCatalog*` (composite/bare +
  storage-managed coverage pin). Plans:
  `plans/schema-validation-final-2026-06-01.md`,
  `plans/schema-single-source-of-truth-2026-06-02.md`.
- **`run.envVariables` is the canonical setup-entry env-var location** —
  the live JSON schema rejects `envVariables` at the setup-entry top
  level (`additionalProperties: false`); the only valid locations are
  `build.envVariables` and `run.envVariables`. `EnvGenerateDotenv` and
  every env-ref pre-flight (`preflightEnvRefs`, `checkEnvSelfShadow`)
  read `entry.Run.EnvVariables` exclusively. The
  earlier top-level `ZeropsYmlEntry.EnvVariables` field was dead code
  that silently absorbed schema-violating yaml — its presence let four
  parallel readers no-op on every conforming yaml (the canonical
  `run.envVariables` was invisible to them). Atom guidance
  (`develop-first-deploy-scaffold-yaml.md`) places the block under
  `run:`. Pinned by `TestEnvGenerateDotenv_ResolvesRefs/top-level
  envVariables ignored*` and `TestCheckEnvSelfShadow_Table` (every
  fixture uses `e.Run.EnvVariables`).
- **Local-mode preflight respects `workingDir`** — in local mode (no SSH
  deployer; user's dev machine), `workingDir` is the source of truth for
  `zerops.yaml` location; `deployPreFlight` honors it end-to-end, falling
  back to state-derived `projectRoot` only when `workingDir` is empty.
  Without this, preflight validated a different file than `ops.DeployLocal`
  deployed from — both false positives (preflight pass on yaml that's not
  the deployed one) and false negatives (preflight fail at wrong path)
  surfaced. Container-env callers (`deploy_ssh`, `deploy_batch`) pass
  `workingDir=""` because their workingDir names a CONTAINER path,
  irrelevant for dev-side yaml lookup. Pinned by
  `TestDeployPreFlight_LocalMode_*`.
- **Subdomain L7 activation is the deploy handler's concern, platform classifies** —
  `zerops_deploy` auto-enables subdomain on first deploy for eligible modes
  (dev/stage/simple/standard/local-stage) and waits HTTP-ready. Predicate is
  mode-allowlist + `IsSystem()` defensive guard ONLY — earlier DTO checks
  (`SubdomainAccess`/`Ports[].HTTPSupport`) read post-enable state as if it
  were import-yaml intent and broke first-deploy auto-enable (smoking gun in
  `internal/tools/deploy_subdomain.go` doc-comment). Platform-classified
  `serviceStackIsNotHttp` (worker, F8 deferred-start) is silently swallowed
  in `maybeAutoEnableSubdomain` only; `ops.Subdomain.Enable` still surfaces
  it for explicit-recovery callers. **Launch-production deliberately opts
  out** per P-PROD-2 invariant (`docs/spec-launch-production-platform-spike.md`):
  production prefers explicit custom-domain over `*.zerops.app`; the launch
  composer strips `enableSubdomainAccess` from the production import YAML
  and does NOT call `maybeAutoEnableSubdomain`. Pinned by
  `TestBuildLaunchBundle_StripsSubdomainAccess` +
  `TestReadinessRubric_AllPassOnCleanBundle` (the readiness rubric's
  subdomain-disabled check — `launch_readiness.go`, wired into
  ready-to-launch by F6, kept through the bright-oak audit merge).
  Agents/recipes never call `zerops_subdomain action=enable` in happy path
  for dev/stage; launch-production agents surface the choice to the operator
  via the `launch-post-checklist` atom (always attached to the launched
  response — explicit fact + mandatory step 3 to establish HTTP exposure
  before smoke test). Pinned by `TestServiceEligible_*`,
  `TestMaybeAutoEnable_ServiceStackIsNotHttp_BenignSkip`. Spec:
  `spec-workflows.md §4.8` + O3.
- **Container `.claude.json` pre-trusts the workspace and pre-approves
  `ANTHROPIC_API_KEY` when set** — `zcp init` on containers always writes
  `projects[vsCodeWorkDir]` with `hasTrustDialogAccepted` and
  `hasCompletedProjectOnboarding` true, and (gated on the env var) emits
  `customApiKeyResponses.approved = [last20(key)]` matching Claude Code's
  own format. Without both, the VS Code Claude extension's first-run
  flow surfaces a subscription/API-key entry screen that overwrites
  zcp init's setup; with them the extension reads the env-var key
  silently. customApiKeyResponses is OMITTED when the env var is unset
  so OAuth/Subscription users don't get a phantom approval. Pinned by
  `TestContainerSteps_ClaudeConfigs_{ProjectEntry,APIKeyApproved,NoAPIKey}`.
- **VS Code agent launcher is live-resolved, never baked, and dual-mode** — the
  `zcp-bootstrap` extension (installed by `zcp init` only when in-container +
  `ZCP_VSCODE=true`) reads from the LIVE zembed env store
  (`/etc/zerops-zembed/env.json`, which zembed rewrites on every env change
  without restart), NOT from `process.env` (a running extension host froze that
  at code-server boot). `zcp init` bakes NO config file — it only installs the
  template. The launcher feature-detects its mode on every read: presence of ANY
  per-agent auth env (`/^ZCP_AGENT_(AUTH_TYPE|OAUTH|TOKEN)_/`) → **auth mode**;
  absence → **legacy mode**. The namespace-presence switch is the
  backward-compat seam — the current production GUI writes only `ZCP_AGENT_TYPES`
  (no auth envs), so it keeps the legacy behavior untouched; no extra flag env is
  required from the platform. **Legacy mode**: list the agents named in
  `ZCP_AGENT_TYPES` as click-to-launch cards, Claude-plugin fallback when none.
  **Auth mode**: render ALL 4 agents (`claude-code`, `codex`, `antigravity`,
  `grok`) with per-agent authorization status — `authType` =
  `ZCP_AGENT_AUTH_TYPE_<SUFFIX>` (`oauth`/`token`), `authorized` =
  `ZCP_AGENT_OAUTH_<SUFFIX>==="true" || !!ZCP_AGENT_TOKEN_<SUFFIX>` (`<SUFFIX>` =
  uppercase id, `-`→`_`); authorized agents show an action button per open mode,
  unauthorized show a text hint to authorize in the Zerops UI panel beside the
  editor (the extension never performs auth). The token VALUE is presence-only —
  it never reaches the UI. A `fs.watch` on `env.json` reopens the launcher when
  (and only when) the resolved view signature changes (mode + per-agent auth
  state, or the legacy id list) — no polling, unrelated env writes deduped out.
  Per-agent open commands bypass permission prompts and are safety-critical
  (Claude via its plugin `claude-vscode.editor.open` or a terminal
  `claude --dangerously-skip-permissions --effort max`,
  `codex --dangerously-bypass-approvals-and-sandbox`,
  `agy --dangerously-skip-permissions`, bare `grok`) — verified against the real
  binaries / official docs and pinned by
  `TestBootstrapExtension_AgentCommandsPinned` +
  `TestBootstrapExtension_AuthModelPinned` +
  `TestBootstrapExtension_LiveContract` +
  `TestContainerSteps_VSCode_AgentLauncher_LiveNoBakedConfig`. `ZCP_AGENT_TYPES`
  is in the export/launch infra-env allowlist (`topology.classifyInfrastructureKeys`);
  the per-agent `ZCP_AGENT_{AUTH_TYPE,OAUTH,TOKEN}_*` envs are NOT yet classified
  (suffixed keys miss the exact-key allowlist + `CredentialPattern`) — decide when
  the new GUI ships.
- **Engine version stamps the plan** — every fresh recipe session writes
  `plan.EngineVersion = server.Version` before the first `WritePlan()`;
  any complete-phase refuses when missing or mismatched against the running
  binary. Pinned by `TestGateEngineVersionStamped_*`.
- **launch-production is pipeline-first: the import carries NO buildFromGit;
  the first prod build IS the first release** — promoted runtimes import with
  `startWithoutCode: true` (live-verified: service ACTIVE, booted empty
  container, ~12s, no build process) and the application arrives through the
  production pipeline (actions: tag-triggered `zerops-prod.yml` + repo secret;
  webhook: dashboard TAG integration), the SAME mechanism every later release
  uses. This kills the platform's credential-less clone failure class
  (private repo / `.git`-suffix → terminal FAILED, no logs) and the FP-3
  visibility gate with it; private repos are first-class. The launched
  response carries the structured `firstRelease` block (truth + family +
  steps + watch); actions family SUPPRESSES the dashboard
  pipeline-not-configured blockers/atom (platform integration-status is
  expected not-configured there); `BuildIntegration=none` → ASK the user,
  never a silent family default. The repo identity for pipeline WIRING (not
  YAML) still comes from `ServiceMeta.RemoteURL` of a gate-validated meta
  (`GitPushState=configured` + live-origin match + clean tree +
  local-HEAD-on-remote, P-LP-10/11) — never a live SSH read of
  `/var/www/.git/config` (recipe-bootstrap leaves the template URL there).
  Source-control failures chain `git-push-setup` → `zerops_deploy
  strategy=git-push` → `build-integration`. Multi-runtime promotion uses
  `WorkflowInput.Promotables`; the composer dedupes managed deps.
  Existing-project collisions → `existing-project-conflict-prompt` (P-LP-12)
  with skip/replace + `confirmDestructive`. Export keeps buildFromGit by
  design (public template repos). Pinned by
  `TestBuildLaunch_PipelineFirst_NoBuildFromGit`,
  `TestReadinessRubric_PipelineFirst`,
  `TestLaunchedResponse_CarriesFirstReleaseBlock`,
  `TestPipelineBlockers_ActionsFamily_Suppressed`. Spec:
  `docs/spec-workflows.md §10`. Plan: `plans/launch-pipeline-first-2026-06-11.md`.
- **Single-token launch lifecycle: the token enters the conversation ONCE;
  the staged service secret is the working copy; confirm-production closes
  the window PHYSICALLY (P-LP-14)** — Zerops has no single-use token type;
  ONE user-minted integration token (canCreateProjects; gains access to
  projects it creates) covers create-project, the bring-up window, AND
  GitHub Actions. The mutation stages it as the `ops.LaunchTokenEnvKey`
  (`ZCP_LAUNCH_TOKEN` — the platform REJECTS custom envs with a ZEROPS_
  prefix, live-verified; the GitHub repo secret keeps its independent
  `ZEROPS_TOKEN_PROD` name) service-scope SECRET on the source push service
  strictly BEFORE `CreateAndImportProject` (stage failure aborts pre-create:
  no project, no state). Every launch-window operation — prod-ops, pipeline
  resume, reset orphan-delete, confirm-production — resolves the token from
  the staged secret (`launchKeyFromStage`, a platform-API read of the source
  env store: works in local mode and with a stopped dev container; in-request
  only); explicit `launchKey` is fallback-only. The prodCD repo-secret
  conveyance is secret-to-secret (`gh secret set -b "$(ssh … printf
  $ZCP_LAUNCH_TOKEN)"` — no paste placeholder). `action="confirm-production"`
  (explicit `confirmFunctional=true` user ack; prod-liveness warn-only)
  DELETES the staged env FIRST and stamps `WindowClosedAt` second —
  enforcement is the deleted env, never the stamp; the close response carries
  the regenerate NOTE (token id best-effort via
  `platform.ListIntegrationTokens`; integration tokens may READ token lists,
  mutations 403). A FAILED launch's orphan stays recoverable through the
  staged copy: reset's `wouldDestroy.projects[]` lists the project whenever a
  token resolves (deleted FIRST, before the state file, so a failed delete
  leaves the orphan tracked); without any token, reset stays state-file-only.
  The staged key is classify-infrastructure (bundle filter) + dotenv-denylisted;
  the value never crosses response/state/audit surfaces. Pinned by
  `TestExecuteLaunchMutation_StagesTokenBeforeCreate`,
  `TestExecuteExistingProjectMutation_StagesToken`,
  `TestProdOps_ReadsStagedToken` / `_AfterClose_LifecycleMessage`,
  `TestPipelineResume_StagedToken`, `TestLaunchReset_StagedToken`,
  `TestConfirmProduction_*`, `TestLaunchTokenEnvKey_ClassifiedInfrastructure`,
  `TestHandleLaunchReset_WithLaunchKey_DeletesOrphanProject`,
  `TestHandleLaunchReset_LaunchKeyDeleteFails_KeepsState`. Spec:
  `docs/spec-workflows.md §10.2b` + P-LP-14. Plan:
  `plans/launch-single-token-lifecycle-2026-06-11.md`.
- **Export-for-buildFromGit is a single-repo self-referential snapshot** —
  `zerops_workflow workflow="export"` is a stateless three-call narrowing
  (scope-prompt → classify-prompt → publish-ready / validation-failed) keyed
  by per-request `WorkflowInput.{TargetService, Variant, EnvClassifications}`.
  Bundle carries ONE buildFromGit-bearing runtime + N managed deps so
  `${db_*}`/`${redis_*}` resolve at re-import. `services[].mode` is the Zerops
  scaling enum (`HA`/`NON_HA`) — ZCP topology (dev/simple/local-only) is a
  destination-bootstrap concern, NOT import.yaml content. Live
  `git remote get-url origin` is source of truth for `buildFromGit:`;
  `meta.RemoteURL` is a refreshed cache with drift surfaced as warnings.
  Schema-validation errors populate `bundle.errors` and flip the response to
  `status="validation-failed"` before any git-push-setup chain runs. Pinned by
  `TestHandleExport_*`, `TestBuildBundle_*`, `TestValidateImportYAML_*`.
  Spec: `docs/spec-workflows.md §9` + E1-E5.
- **A repo URL's `.git`/slash suffix is not part of repo identity —
  canonicalize at every emit and compare, store verbatim** — Zerops'
  `buildFromGit` clone-preflight REJECTS a trailing `.git` (the conventional
  clone-URL form `git remote get-url origin` and "copy clone URL" buttons hand
  out): the build reaches terminal FAILED in ~0.3s with no build container and
  **no logs**; the same URL without `.git` builds. ZCP owns what it writes into
  `buildFromGit`, so `topology.CanonicalRepoURL` (trim space + one trailing `/`
  + one trailing `.git` + `/` again; host/scheme/auth/case untouched;
  idempotent) is applied at BOTH bundle emit sinks (`ops/bundle/launch.go` +
  `export.go`) AND BOTH identity-compare sites (the launch source-control drift
  gate `launch_source_control_gate.go` + the export remote-cache drift
  `workflow_export_probe.go::refreshRemoteURLCache`) — else a `.git`-only
  difference false-blocks launch or churns the cache. Deliberately NOT
  normalized at storage: `meta.RemoteURL` is kept verbatim (it doubles as the
  `git push` remote, and the validator accepts non-GitHub hosts where suffix
  semantics aren't guaranteed) — canonicalization is a property of USE, not of
  the stored value. Recipe-template emitters (`RecipeAppRepoBase`+slug,
  `utilityBuildFromGitURL`) hardcode `.git`-free URLs, so they need no strip.
  Pinned by `TestCanonicalRepoURL*`, `TestBuildLaunch_BuildFromGitStripsDotGit`,
  `TestComposeImportYAML_MinimalRuntimeOnly`,
  `TestValidateLaunchSourceControl_RemoteDotGitDiffersOnly_NoBlock`,
  `TestRefreshRemoteURLCache` (canonical-equal case), `TestExportFlow_MultiCallThroughServer`.
- **Log time comparison is parse-compare, never lexicographic** — RFC3339
  fractional precision varies (3–9 digits); string compare misorders entries
  at `.` vs `Z`. `internal/platform/logfetcher.go::filterEntries` uses
  `time.Parse` + `time.Before` only. Mock shares the pipeline.
- **Per-build log scoping uses tag identity** — build service-stacks persist;
  querying by `serviceStackId` alone returns historical entries.
  `FetchBuildWarnings`/`FetchBuildLogs` scope by
  `Tags: ["zbuilder@" + event.ID]` + `Facility: "application"`;
  `FetchRuntimeLogs` anchors at `ContainerCreationStart`. Pinned by
  `TestBuildLogsContract_UsesTagIdentityAndApplicationFacility`.
- **Deploy mode asymmetry is first-class** — every `zerops_deploy` is
  `DeployClassSelf` (source==target) or `DeployClassCross` via
  `ops.ClassifyDeploy`. Self-deploy with narrower-than-`[.]` deployFiles
  → `ErrInvalidZeropsYml` (DM-2: source IS target, cherry-pick destroys it).
  Cross-deploy deployFiles is post-build-tree; ZCP doesn't stat-check source
  (DM-3/DM-4 — builder's job). Pinned by `TestValidateZeropsYml_DM2_*`/`DM3_*`.
- **Atom authoring contract is unified** — atoms in `internal/content/atoms/`
  describe observable state, orchestration, concepts, pitfalls, cross-refs;
  observable fields are AST-pinned via `references-fields`. MUST NOT contain
  spec invariant IDs (`DM-*`/`E*`/`O*`), handler-behavior verbs (`auto-*`,
  `stamps`, `activates`), invisible-state field names, plan-doc paths, or
  env-only title/heading qualifiers (`container`/`local` as standalone
  tokens — Axis L is HARD-FORBID). Don't add per-topic
  `*_atom_contract_test.go` — extend `internal/content/atoms_lint.go` (or
  `atoms_lint_axes.go` for axis K/L/M/N). Pinned by `TestAtomAuthoringLint`,
  `TestAtomReferenceFieldIntegrity`, `TestAtomReferencesAtomsIntegrity`.
  Spec: `spec-knowledge-distribution.md §11`.
- **Recipe-specific findings go in recipes, not atoms or audits** — framework
  gotchas (Razor hot-reload, EF Core EnsureCreated, Laravel+Vite manifest
  timing, Next.js cache semantics), library-version pitfalls, framework-specific
  dev workflows: edit `internal/knowledge/recipes/<slug>.md`, then
  `zcp sync push recipes <slug>` to publish via PR. NOT atoms (atoms carry
  platform mechanics that apply to ANY user of a service-stack type; framework
  knowledge would explode the corpus and break axis-filter meaning). NOT audit
  reports (transient). `## Gotchas` section is optional and not enforced —
  add when you have concrete verified pain points, don't fabricate placeholders.
- **Knowledge retrieval is single-format and tools-only** — ZCP serves ALL
  curated knowledge through the `zerops_knowledge` TOOL
  (`uri="zerops://<ns>/<path>"`), never the MCP *resources* protocol; the server
  registers no `AddResourceTemplate` and leaves `HasResources` unset, so the
  initialize handshake advertises NO `capabilities.resources`. Resources are a
  non-universal client capability (codex/grok/antigravity may not implement them)
  and a static GET can't carry ZCP's adaptive, placeholder-substituted knowledge
  (the `resolveAtomURI` inline-atom safety boundary). Every agent-facing emission
  of a `zerops://` URI uses the tool-call form: query= search hits carry a
  `fetch` directive; agent-facing markdown
  never wraps a bare `zerops://` in backticks. Pinned by
  `TestServer_DoesNotAdvertiseResourcesCapability`,
  `TestKnowledgeTool_Query_EmitsFetchHint`,
  `TestNoBareZeropsURIInAgentContent`. Plan:
  `plans/converge-knowledge-retrieval-format-2026-06-04.md`.
- **Service by hostname** — agents/tools speak hostnames; resolve to ID internally.
- **Lifecycle recovery is via `action="status"`** — `zerops_workflow
  action="status"` is the canonical lifecycle envelope (envelope + plan +
  guidance) and the supported recovery primitive after context compaction.
  Mutation responses MAY be terse — envelope optional, attach only when the
  handler already has ComputeEnvelope inputs. Error responses MUST remain
  leaf payloads (`convertError` does not attach an envelope). Pinned by P4
  in `docs/spec-workflows.md`.
- **Auto-close is DERIVED, never stamped** — the auto-close gate is a 3-input
  predicate (deploys + verifies + per-service `meta.CloseDeployMode`) over the
  DECLARED scope, computed by `EvaluateAutoClose` / `DeriveCloseState` on every
  read. The verify must POSTDATE the latest successful deploy (`staleVerify`,
  single owner in `build_plan.go`, called from `serviceAutoCloseReady` +
  `needsVerify` + `renderProgressAndBlockers`): a redeploy replaces the
  container and re-opens verify, so a verify that passed before it is stale —
  gating on a pre-deploy verify auto-closed a session over a dead server
  (B3/F60). Pinned by `TestServiceAutoCloseReady_VerifyOrdering`,
  `TestRecordDeployAttempt_DeployAfterVerify_DoesNotAutoClose`. Auto-complete writes NO `ClosedAt` to disk (only explicit close — which
  DELETES the file — and iteration-cap stamp `ClosedAt`); the `develop-closed-auto`
  phase is recomputed each read. This kills the gate-desync bug class: the old
  trigger was event-only (`RecordDeployAttempt`/`RecordVerifyAttempt`) and tipped
  on one surface but not another once close-mode joined as a state-input with no
  Record* parallel — `MaybeFireAutoClose` was the lazy-stamp band-aid (deleted).
  Because the derived-closed session keeps `ClosedAt==""` on disk, every lifecycle
  reader MUST use `workflow.IsOpen(stateDir, ws)` / `DeriveCloseState`, NEVER a raw
  `ws.ClosedAt == ""` read (which reads a done session as open and stuck-loops the
  agent). The derived completion time is the stable `LastActivityAt`, so the
  envelope stays byte-deterministic for compaction recovery. Pinned by
  `TestDeriveCloseState`, `TestIsOpen`, `TestNoRawClosedAtReads` (AST-forbids raw
  reads outside `DeriveCloseState`/`ResolveLifecycle`/`closeWorkSessionOnCap`),
  `TestHandleCloseMode_FiresAutoCloseWhenScopeReady`,
  `TestHandleDevelopBriefing_DerivedAutoComplete_SameIntent_StartsFresh`. Spec
  §1.3 + the "deriving rather than stamping" section in `docs/spec-work-session.md`.
- **JSON-only stdout** — debug to stderr; MCP protocol depends on it.
  Pinned by `TestNoStdoutOutsideJSONPath` (scans `internal/...` for
  `fmt.Print*`, `fmt.Fprint*(os.Stdout, ...)`, `os.Stdout.Write*`,
  `println`; CLI entrypoints under `cmd/` are out of scope).
- **No progress notification immediately before a tool response** — Claude Code's
  MCP TS client coalesces a `notifications/progress` and the next tool response
  into one stdin chunk if they land within pipe-buffer-flush window, then errors
  with "Received a progress notification for an unknown token" and tears down
  the stdio transport. Every return path in a poll loop MUST run BEFORE
  `onProgress`. Empirically reproduced 7/7 in mcptest 2026-04 testing; the only
  ZCP emit choke-point is `tools/convert.go::buildProgressCallback`, fed by
  `ops/progress.go::pollProcess` and `pollBuild`. Pinned by
  `TestPollProcess_TimeoutSkipsProgressEmit`,
  `TestPollBuild_TimeoutSkipsProgressEmit`.
- **Stateless STDIO tools** — each MCP call is a fresh operation.
  Pinned by `TestNoCrossCallHandlerState` (forbids zero-value
  package-level vars in `internal/tools/` + the store-less authoring
  packages `authoring/{publish,analyze}`; `authoring/recipe` is exempt
  by design — deliberate session Store with plan.json-rehydration
  recovery; initialized vars — regex, lookup tables, interface
  assertions, literals — remain allowed).
- **Shell interpolation via `shellQuote()`** — POSIX single-quote; never strip-only.
- **Error wrapping** — `fmt.Errorf("op: %w", err)`; never bare `return err`.
- **File splits driven by cohesion, not line count** — split a `.go` file
  when responsibilities diverge, not when it crosses an arbitrary length.
- **English everywhere** — code, comments, docs, commits.
- **Phased refactors** — verify each phase before continuing; no half-finished states.
- **Rename safety** — no AST-aware tooling; grep separately for calls, types,
  strings, tests.

## Do NOT

- Use global mutable state (except `sync.Once` for init).
- Use `replace` directives in `go.mod`.
- Use `interface{}`/`any` when the concrete type is known.
- Use `panic()` — return errors.
- Skip error checks (`errcheck` enforces).
- Write tests + implementation in the same commit without RED first.
- Add `t.Parallel()` to packages with global state without thread-safety first.
- Use `fmt.Sprintf` to compose SQL/shell commands.
- Hold mutexes during I/O — copy under lock, release, then I/O.

---

## Maintenance

- New invariant or convention → add bullet + **pin with a test**.
- Plan completed → `git mv plans/X.md plans/archive/X.md`.
- New error code → declare in `internal/platform/errors.go`.
- Global state added → document in package's seed test as `// non-parallel: <reason>`.
