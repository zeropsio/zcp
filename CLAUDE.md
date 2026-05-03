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
- **Check-before-mutate for non-idempotent platform APIs** — read state via
  REST-authoritative endpoint, short-circuit when desired state holds.
  Canonical: `ops.Subdomain`. Spec: `spec-workflows.md §8 O3`.
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
  it for explicit-recovery callers. Agents/recipes never call
  `zerops_subdomain action=enable` in happy path. Pinned by
  `TestServiceEligible_*`, `TestMaybeAutoEnable_ServiceStackIsNotHttp_BenignSkip`.
  Spec: `spec-workflows.md §4.8` + O3.
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
