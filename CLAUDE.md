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

Live Zerops schemas (authoritative for YAML field validation):
- import: `https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yml-json-schema.json`
- zerops.yaml: `https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json`

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
make lint-local        ~15s full lint + atom-tree gates
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
zcp sync recipe {create-repo,publish,export}  Recipe repo lifecycle
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
| `ops/` does NOT import `workflow/`, `tools/`, `recipe/` | Peer/upper |
| `workflow/` does NOT import `ops/`, `tools/`, `recipe/` | Peer/upper |
| New shared type → `topology/` first, never `workflow/` | Promotion rule |

**Cross-cutting packages** (peer-of-equal-level, not strict layered) live
under `internal/`; key non-obvious ones: `auth/` runs pre-engine and talks
to platform directly, `recipe/` is a separate v3-engine scope, `service/`
exec wrappers are name-collision-distinct from `topology/`. Full list via
`ls internal/`.

Spec: `docs/spec-architecture.md` — per-package mapping + examples.

---

## Conventions

- **Deploy config is three orthogonal dimensions** — `ServiceMeta` carries
  `CloseDeployMode`, `GitPushState` (with `RemoteURL`), and `BuildIntegration`,
  each owned by one user-facing action (`close-mode` / `git-push-setup` /
  `build-integration`). The legacy single-field conflation is gone:
  git-push capability and close-mode are independent (configured push can
  coexist with auto close-mode). `BuildIntegration`
  requires `GitPushState=configured`. Atom corpus filters on the three
  matching axes. Spec: `docs/spec-workflows.md §1.1` + `§4.3`.
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
  `TestErrorWire_FailureClassification`.
- **Verify checks carry structured Recovery for actionable preconditions** —
  when an infrastructure precondition that the agent can fix is missing
  (e.g. subdomain access disabled), the failing CheckResult MUST carry a
  Recovery struct (tool + action + args) pointing at the exact next call.
  Skip status reserved for non-actionable transients (URL not yet resolved).
  Pinned by TestVerify_* cases asserting Recovery shape on http_root failure.
- **Diagnose-before-destruct gates always-dangerous operations** —
  `zerops_import override=true` (and future destructive tools) refuse to
  mutate when target services have failed-appVersion history unless the
  call carries `confirmDestructive: {operation, acknowledgedTargets,
  diagnosedFailureClass?}` matching the structured `wouldDestroy` payload
  returned in the first-call rejection. Failure context surfaces lazily
  via `zerops_events` (`internal/ops/events.go::ClassifyDeployFailure`
  reuse path). Recovery hints on `verify::service_running`,
  `workflow_checks::checkServiceStatusAny`, `deploy` pre-flight, and
  `dev_server` pre-spawn point at the same gate. Lives on
  `ErrDiagnosisRequired` error code + `tools.DiagnosedDestruction` wire
  shape + `ops.LatestFailedAppVersionContext` helper. Recovery is
  promoted to `topology.Recovery` so `ops.Recovery` and
  `tools.RecoveryHint` are the same type. Pinned by
  `TestImport_OverrideOnFailedRequiresAck`,
  `TestCheckServiceStatusAny_ReadyToDeployWithFailedAppVersion_RecoveryToImport`,
  `TestLatestFailedAppVersionContext_*`, `TestNonRunningRecovery_*`,
  `TestGateNonRunningOnDeploy_*`, `TestDevServer_FailedRefusesWithRecovery`.
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
- **`run.envVariables` is the canonical setup-entry env-var location** —
  the live JSON schema rejects `envVariables` at the setup-entry top
  level (`additionalProperties: false`); the only valid locations are
  `build.envVariables` and `run.envVariables`. `EnvGenerateDotenv` and
  every env-ref pre-flight (`preflightEnvRefs`, `CheckEnvRefs`,
  `CheckEnvSelfShadow`) read `entry.Run.EnvVariables` exclusively. The
  earlier top-level `ZeropsYmlEntry.EnvVariables` field was dead code
  that silently absorbed schema-violating yaml — its presence let four
  parallel readers no-op on every conforming yaml (the canonical
  `run.envVariables` was invisible to them). Atom guidance
  (`develop-first-deploy-scaffold-yaml.md`) places the block under
  `run:`. Pinned by `TestEnvGenerateDotenv_ResolvesRefs/top-level
  envVariables ignored*` and `TestCheckEnvRefs_Table` /
  `TestCheckEnvSelfShadow_Table` (every fixture uses `e.Run.EnvVariables`).
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
  `TestBuildLaunchBundle_StripsSubdomainAccess` + readinessCheckSubdomainDisabled.
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
  (Claude via its plugin `claude-vscode.editor.open` or bare `claude` terminal,
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
- **launch-production is an orchestrator, not a passive promoter** — every
  promoted runtime's `buildFromGit:` value comes from
  `ServiceMeta.RemoteURL` of a meta whose `GitPushState=GitPushConfigured`
  AND whose live `git remote get-url origin` matches that meta AND whose
  push hostname has a clean working tree + local-HEAD-on-remote (P-LP-10,
  P-LP-11). Live SSH read of `/var/www/.git/config` is NEVER the source —
  recipe-bootstrap leaves the public template URL there indefinitely, and
  the gate exists to catch that loophole structurally. Source-control
  failures surface as `source-control-required` chaining the agent into
  `git-push-setup` → `zerops_deploy strategy=git-push` → `build-integration`
  (the existing develop-side actions; launch does not implement source
  mutations itself). Multi-runtime promotion uses
  `WorkflowInput.Promotables []LaunchPromotableInput`; the composer loops
  + dedupes managed deps so shared infra lands once. Existing-project
  collisions surface as `existing-project-conflict-prompt` (P-LP-12) with
  per-conflict skip/replace + `confirmDestructive` ack for replace. Spec:
  `docs/spec-workflows.md §10`. Plan: `plans/launch-production-source-of-
  truth-2026-05-20.md`.
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
  read. Auto-complete writes NO `ClosedAt` to disk (only explicit close — which
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
  package-level vars in `internal/tools/`; initialized vars — regex,
  lookup tables, interface assertions, literals — remain allowed).
- **Shell interpolation via `shellQuote()`** — POSIX single-quote; never strip-only.
- **Error wrapping** — `fmt.Errorf("op: %w", err)`; never bare `return err`.
- **File splits driven by cohesion, not line count** — split a `.go` file
  when responsibilities diverge, not when it crosses an arbitrary length.
  Frozen v2 cluster (`internal/workflow/recipe_*.go`) exempt until deletion.
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
