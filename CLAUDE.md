# ZCP — Zerops Control Plane

Single Go binary merging ZAIA CLI + ZAIA-MCP. AI-driven Zerops PaaS management via MCP protocol.

---

## Source of Truth

```
1. Tests (table-driven, executable)    ← AUTHORITATIVE for behavior
2. Code (Go types, interfaces)         ← AUTHORITATIVE for implementation
3. Specs (docs/spec-*.md)             ← AUTHORITATIVE for workflow design
4. Plans (plans/*.md)                  ← TRANSIENT (roadmap, expires)
5. CLAUDE.md                           ← OPERATIONAL (workflow, conventions)
```

Key specs:
- `docs/spec-workflows.md` — workflow step specs, invariants, envelope/plan/atom pipeline, state model
- `docs/spec-work-session.md` — per-PID Work Session for develop (lifecycle visibility, compaction survival, auto-close)
- `docs/spec-knowledge-distribution.md` — atom corpus authoring model (axes, priorities, placeholders) and synthesizer contract
- `docs/spec-scenarios.md` — per-phase scenario walkthroughs, pinned by `internal/workflow/scenarios_test.go`
- `docs/spec-local-dev.md` — local-machine vs container environment differences
- `docs/spec-content-surfaces.md` — recipe content-quality contract (seven surfaces, classification taxonomy)
- `docs/zrecipator-archive/spec-recipe-quality-process.md` — recipe audit process (archived)

Zerops platform schemas (live, authoritative for YAML field validation):
- **Import YAML**: `https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yml-json-schema.json`
- **zerops.yaml**: `https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json`

---

## Architecture

```
cmd/zcp/main.go → internal/server → MCP tools → internal/ops → internal/platform → Zerops API
                                                                internal/auth
                                                                internal/knowledge (text search)
```

| Package | Responsibility | Key file |
|---------|---------------|----------|
| `cmd/zcp` | Entrypoint, STDIO server | `main.go` |
| `internal/server` | MCP server setup, registration | `server.go` |
| `internal/tools` | MCP tool handlers | `discover.go`, `manage.go`, ... |
| `internal/ops` | Business logic, validation | `discover.go`, `manage.go`, ... |
| `internal/platform` | Zerops API client, types, errors | `client.go`, `errors.go` |
| `internal/auth` | Token resolution (env var / zcli), project discovery | `auth.go` |
| `internal/knowledge` | Text search, embedded docs, session-aware briefings, runtime-aware mode adaptation | `engine.go`, `briefing.go` |
| `internal/runtime` | Container vs local detection | `runtime.go` |
| `internal/content` | Embedded templates + atom corpus (`atoms/*.md`) + recipe authoring workflow (only `workflows/recipe.md` remains) | `content.go`, `atoms_test.go` |
| `internal/workflow` | Workflow orchestration + Layer 2/3 pipeline: atom parsing, synthesis, typed Plan, envelope composition, bootstrap/develop conductors. zcprecipator2 recipe workflow is frozen here (see `internal/workflow/README.md`); recipe work lives at `internal/recipe/`. | `synthesize.go`, `build_plan.go`, `compute_envelope.go`, `envelope.go`, `atom.go`, `session.go`, `work_session.go` |
| `internal/recipe` | zcprecipator3 recipe engine — typed tiers / roles / surfaces / facts, 5-phase state machine, yaml emitter, chain resolver, sub-agent brief composer, MCP `zerops_recipe` handlers. See `docs/zcprecipator3/plan.md`. | `tiers.go`, `workflow.go`, `yaml_emitter.go`, `briefs.go`, `handlers.go` |
| `internal/init` | `zcp init` subcommand — config file generation | `init.go` |
| `internal/eval` | LLM recipe eval + headless recipe creation via Claude CLI | `runner.go`, `prompt.go`, `recipe_create.go` |
| `internal/schema` | Live Zerops YAML schema fetching, caching, enum extraction, LLM formatting | `schema.go`, `cache.go`, `format.go` |
| `internal/catalog` | API-driven version catalog sync for test validation | `sync.go` |
| `internal/sync` | Bidirectional recipe/guide sync: API pull, GitHub push, Strapi cache | `push_recipes.go`, `transform.go` |

Error codes: see `internal/platform/errors.go` for all codes (AUTH_REQUIRED, SERVICE_NOT_FOUND, etc.)

---

## TDD — Mandatory

1. **RED**: Write failing test BEFORE implementation
2. **GREEN**: Minimal code to pass
3. **REFACTOR**: Clean up, tests stay green

### Seed test pattern

Write ONE seed test per new package — establishes naming, structure, helpers. Follow for all subsequent tests.

```go
func TestDiscover_WithService_Success(t *testing.T) {
    t.Parallel() // OMIT for packages with global state (e.g., output.SetWriter)
    tests := []struct { ... }{ ... }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel() // OMIT for packages with global state
            // ...
        })
    }
}
```

### Testing layers

| Layer | Scope | Command |
|-------|-------|---------|
| Unit | platform, auth, ops | `go test ./internal/platform/... ./internal/auth/... ./internal/ops/...` |
| Tool | MCP handlers | `go test ./internal/tools/...` |
| Integration | Multi-tool flows (mock) | `go test ./integration/` |
| E2E | Real Zerops API | `go test ./e2e/ -tags e2e` |

### Change impact — tests FIRST at ALL affected layers

Before any behavioral change, update/write failing tests at **every affected layer** first (RED). Then implement (GREEN). Pure refactors (no behavior change) skip RED — just verify all layers stay green.

- **Interface/type change** (platform, ops) → unit tests + tool tests + integration + e2e
- **Tool handler change** (tools/) → tool tests + integration + e2e
- **Business logic change** (ops/) → unit tests + tool tests that exercise the logic
- **API client change** (platform/) → unit tests + e2e
- **New MCP tool** → tool test + annotations_test.go + integration flow + e2e step

A change is not complete until all affected layers pass.

### Rules

- **Table-driven tests** — no exceptions
- **`testing.Short()`** — long tests must check and skip
- **`t.Parallel()` only where safe** — document global state preventing it (see seed patterns above)
- **Test naming**: `Test{Op}_{Scenario}_{Result}` (e.g. `TestDiscover_WithService_Success`)
Automated: Tier 1 (edit) → Tier 2 (turn) → Tier 3 (commit) → Tier 4 (CI). See `.claude/settings.json`.

---

## Commands

```bash
go test ./internal/<pkg> -run TestName -v   # Single test
go test ./... -count=1 -short               # All tests (fast)
go test ./... -count=1                      # All tests (full, add -race for race detection)
go build -o bin/zcp ./cmd/zcp              # Build
make setup                                  # Bootstrap dev environment
make lint-fast                              # Fast lint (~3s)
make lint-local                             # Full lint (~15s)
```

### Knowledge sync

Recipe/guide knowledge is gitignored — pull before build, push after editing.

```bash
zcp sync pull recipes                       # Pull all recipes from API
zcp sync pull recipes bun-hello-world       # Pull single recipe
zcp sync pull guides                        # Pull all guides from docs repo (GitHub API)
zcp sync push recipes bun-hello-world       # Push edits → GitHub PR on app repo
zcp sync push recipes bun --dry-run         # Preview what would change
zcp sync push guides                        # Push guide edits → PR on zeropsio/docs
zcp sync cache-clear                        # Invalidate Strapi cache (all recipes)
zcp sync cache-clear bun-hello-world        # Invalidate single recipe
make sync                                   # Pull all (recipes + guides)
```

Workflow: `pull` → edit `.md` → `push` (creates PR) → merge → `cache-clear` → `pull` (gets merged changes).

### Recipe publishing

```bash
zcp sync recipe create-repo laravel-minimal   # Create zerops-recipe-apps/{slug}-app repo
zcp sync recipe publish laravel-minimal ./dir  # Publish environments → PR on zeropsio/recipes
zcp sync recipe export ./dir                   # Create .tar.gz archive of recipe output
```

Config: `.sync.yaml` (committed). Strapi token: `.env` (`STRAPI_API_TOKEN`, see `.env.example`).

---

## Conventions

- **JSON-only stdout** — debug to stderr (if `--debug`)
- **Service by hostname** — resolve to ID internally
- **Runtime meta is pair-keyed** — container+standard and local+standard store one `ServiceMeta` file per dev/stage pair; stage is a field on the dev (or stage-keyed) meta, not a separate file. When indexing hostnames → metas use `workflow.ManagedRuntimeIndex(metas)` (slice→map) or `workflow.FindServiceMeta(stateDir, hostname)` (disk lookup); never key on `m.Hostname` alone. Enforced by `TestNoInlineManagedRuntimeIndex`; background in `docs/spec-workflows.md` §8 E8.
- **Check-before-mutate for platform APIs with no idempotent response** — before calling a mutating platform endpoint that might produce garbage side effects on redundant invocation, read current state via a REST-authoritative endpoint (not an ES-backed list) and short-circuit when the desired state already holds. `ops.Subdomain` is the canonical pattern (`internal/ops/subdomain.go`): `GetService` → check `SubdomainAccess` → skip `EnableSubdomainAccess` when already true. Background in `docs/spec-workflows.md` §8 O3.
- **Subdomain L7 activation is a deploy-handler concern** — `zerops_deploy` auto-enables the L7 subdomain on first deploy for eligible modes (dev, stage, simple, standard, local-stage) and waits for HTTP readiness before returning. Agents and recipe pipelines never call `zerops_subdomain action=enable` in the happy path — the tool stays available for recovery, production opt-in, and disable. Background in `docs/spec-workflows.md` §4.8.
- **Log time comparison is parse-compare, never lexicographic** — Zerops log entries arrive with variable fractional-second precision (3–9 digits; verified live 2026-04-23). String compare against an RFC3339 `sinceStr` misorders entries where `.` (0x2E) and `Z` (0x5A) meet. `internal/platform/logfetcher.go::filterEntries` uses `time.Parse` + `time.Before` exclusively. If you write a new log filter, follow the same pattern; the mock (`internal/platform/mock.go::MockLogFetcher`) shares the pipeline so unit tests exercise the real comparison. Background in `plans/logging-refactor.md` §4.7 I-LOG-1.
- **Per-build log scoping is tag identity, not time window** — build service-stacks persist across builds; querying by `serviceStackId` alone returns entries from every historical build. `FetchBuildWarnings` / `FetchBuildLogs` must scope by `Tags: []string{"zbuilder@" + event.ID}` and `Facility: "application"`; `FetchRuntimeLogs` must anchor by `ContainerCreationStart` (or the best fallback per `tools/deploy_poll.go::containerCreationAnchor`). Enforced by `TestBuildLogsContract_UsesTagIdentityAndApplicationFacility` in `internal/ops/`. Background in `plans/logging-refactor.md` §4.7 I-LOG-2, I-LOG-3.
- **Simplest correct solution** — plain functions over abstractions, fewer lines over more.
  But never leave known problems behind: if you encounter flawed architecture,
  duplicated state, or inconsistent patterns while working on a task, fix them
  as part of the same change. Production code in LLM-only development must be
  self-consistent — no human will catch what you skip.
- **Stateless STDIO tools** — each MCP call = fresh operation, not HTTP
- **MockClient + MockExecutor for tests** — `platform.MockClient` for API, in-memory MCP for tools
- **Max 350 lines per .go file** — split when growing
- **English everywhere** — code, comments, docs, commits
- **Shell string interpolation**: use `shellQuote()` (POSIX single-quote escaping), never strip-only
- **`go.sum` committed, no `vendor/`** — reproducible builds via module cache
- **Fix at the source, not downstream** — trace every problem to where it originates
  and fix it there. Before implementing any fix, evaluate whether the current approach
  is fundamentally right — sometimes the correct response is redesign, not a patch.
  If a root-cause fix eliminates downstream problems, delete the downstream code.
  Never add fallbacks — they mask bugs that compound silently in LLM-only development.
- **Phased refactors** — max 5 files per phase, verify before next phase
- **Rename safety** — no AST available; grep separately for calls, types, strings, tests

## Do NOT

- Use global mutable state (except `sync.Once` for init)
- Use `replace` directives in go.mod (temporary dev only, never committed)
- Use `interface{}`/`any` when concrete type is known, or `panic()` — use concrete types, return errors
- Skip error checks — `errcheck` enforces this
- Write tests and implementation in the same commit without RED phase first
- Add `t.Parallel()` to packages with global state without making state thread-safe first
- Use `fmt.Sprintf` for SQL/shell commands — use parameterized queries only
- Hold mutexes during I/O (network, disk) — copy data under lock, release, then I/O
- Return bare `err` without context — always `fmt.Errorf("op: %w", err)`
- Iteratively fix security issues — each fix must be independently validated
- Add fallback/recovery code for problems that should be fixed at their source

---

## Problem-Solving Discipline

- **Root cause, not trigger.** If detection logic is incomplete, fix the detection —
  don't add special cases for individual inputs.
- **Check all parallel paths.** If two code paths do similar validation, bring both
  to parity or extract shared logic. A fix in one that isn't in the other is a future bug.
- **Map the blast radius.** A function change affects every caller. A guidance change
  affects every workflow. Trace all consumers before editing.

---

## Maintenance

| Change | Action |
|--------|--------|
| New package | Update Architecture table |
| New MCP tool | Update Architecture table + register in server.go |
| New convention | Add to Conventions (max 15 bullets) |
| Interface change | Verify key file reference still accurate |
| New error code | Add to `internal/platform/errors.go` |
| Global state added | Document in test seed as non-parallel + add comment |
| Plan completed | Move to plans/archive/ |
| `/status` format change | Update both `bootstrap.md` /status spec AND `ops/verify.go` `statusResponse` struct |

