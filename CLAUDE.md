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
        │  Mode, DeployStrategy, RuntimeClass,     │
        │  PushGitTrigger + predicates + aliases.  │
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

**Dependency rule** (pinned by `.golangci.yml::depguard` +
`internal/architecture_test.go`):

| Rule | Reason |
|------|--------|
| `topology/` imports stdlib only | Foundational vocabulary |
| `platform/` imports no internal/ packages | Bottom of stack |
| `ops/` does NOT import `workflow/`, `tools/`, `recipe/` | Peer/upper |
| `workflow/` does NOT import `ops/`, `tools/`, `recipe/` | Peer/upper |
| New shared type → `topology/` first, never `workflow/` | Promotion rule |

**Cross-cutting packages** (peer-of-equal-level, not strict layered):
`auth/` (pre-engine startup, talks to platform), `runtime/`, `knowledge/`,
`content/`, `recipe/` (v3 engine, separate scope), `eval/`, `preprocess/`,
`schema/`, `catalog/`, `sync/`, `init/`, `update/`, `service/` (exec
wrappers, name-collision-distinct from `topology/`).

Spec: `docs/spec-architecture.md` — per-package mapping + examples.

---

## Conventions

- **4-layer architecture pinned** — `internal/topology/` is the
  foundational layer-2 vocabulary (Mode, DeployStrategy, RuntimeClass,
  PushGitTrigger, predicates, aliases) with zero internal/ imports.
  `internal/ops/` and `internal/workflow/` are peer layer-3 packages —
  neither imports the other; shared types belong in `topology/`.
  `internal/platform/` is the layer-1 raw API (no internal/ imports).
  Upper layers (`internal/server/`, `internal/tools/`, `cmd/`) are
  entry points and can reach down freely. Pinned by `architecture_test.go`
  + `.golangci.yaml::depguard`. Spec: `docs/spec-architecture.md`.
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
- **Subdomain L7 activation is the deploy handler's concern** — `zerops_deploy`
  auto-enables subdomain on first deploy for eligible modes (dev, stage,
  simple, standard, local-stage) and waits HTTP-ready. Agents/recipes never
  call `zerops_subdomain action=enable` in happy path. Spec: `spec-workflows.md §4.8`.
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
  describe observable state (envelope/response, backed by `references-fields`
  AST-pinned to Go struct fields), orchestration, concepts, pitfalls, cross-refs.
  MUST NOT contain spec invariant IDs (`DM-*`, `E*`, `O*`, etc.),
  handler-behavior verbs ("auto-*", "stamps", "activates"), invisible-state
  field names, or plan-doc paths. Three tests enforce: `TestAtomAuthoringLint`,
  `TestAtomReferenceFieldIntegrity`, `TestAtomReferencesAtomsIntegrity`.
  Don't add per-topic `*_atom_contract_test.go` — extend
  `internal/content/atoms_lint.go`. Spec: `spec-knowledge-distribution.md §11`.
- **Service by hostname** — agents/tools speak hostnames; resolve to ID internally.
- **Lifecycle recovery is via `action="status"`** — `zerops_workflow
  action="status"` is the canonical lifecycle envelope (envelope + plan +
  guidance) and the supported recovery primitive after context compaction.
  Mutation tool responses MAY be terse (do not require the lifecycle
  envelope); error responses MUST remain leaf payloads (`convertError`
  does not attach an envelope). Pinned by P4 in `docs/spec-workflows.md`.
  Adding envelope to a mutation response is acceptable when the handler
  has ComputeEnvelope inputs already; not required.
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
- **350-line soft cap per `.go` file** — split when growing. Frozen v2 cluster
  (`internal/workflow/recipe_*.go`) exempt until deletion.
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
