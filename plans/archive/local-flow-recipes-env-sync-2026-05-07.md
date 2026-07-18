# Plan: Local-flow EnvPlan primitive + recipe local + three-channel env

> **Status**: Proposed (revision 2 — replaces 2026-05-07 v1).
> **Date**: 2026-05-07
> **Predecessor**: `plans/archive/local-flow-fundamentals-2026-05-06.md`
> (Phases 5-12 shipped; this plan picks up architectural concerns
> the retrospective + design pass surfaced).
> **Scope**: Three coupled themes — EnvPlan primitive (foundational),
> three-channel env model, recipe-route in local-mode. Plus design-only
> sketch for brownfield-adopt subroute.
> **Scope OUT**: Brownfield-adopt implementation (separate wave per
> design-only sketch in Theme 3). Recipe engine internals
> (`internal/recipe/`, `workflow_recipe.go`) — Aleš's scope.

---

## Why this plan exists

After `local-flow-fundamentals` (Phase 5-12) shipped, two architectural
concerns surfaced that the bug-fix wave didn't reach:

1. **Recipe local-flow gap**: container-shape recipe import.yml provisions
   `appdev` + `appstage` runtimes; in local mode the user's CWD replaces
   `appdev`, leaving the bootstrap path broken (provisions appdev anyway,
   or fails the bootstrap check). Spec was silent on the local-mode
   variant of recipe routing.
2. **Env update mechanism gap**: ZCP's `generate-dotenv` writes a flat
   `.env` from resolved sources, but had no contract for *what does the
   LLM do when env state changes* — three input channels (project env,
   zerops.yaml, service env) with different timing and propagation, no
   user-override channel that survives regen, no provenance tracking.

The journey through this plan's design pass (with Codex stress-test)
revealed that **both gaps share an architectural core**: a typed
state-convergence function over multiple env source layers, with
explicit precedence, provenance, and write policy. The same primitive
also generalizes to brownfield-adopt (scenario C) and will later serve
container-mode env review, CI export, env-promotion diffs.

This plan therefore introduces a foundational primitive (Theme 0), uses
it to implement the three-channel env model (Theme 2) and the recipe
local-flow (Theme 1), and sketches brownfield-adopt as a subroute under
local mode (Theme 3, design-only).

**Authoritative design**: `docs/spec-env-handling.md` carries the
mental model, source precedence rules, render policy, edge case
decisions, alternatives-rejected rationale, and future extension
points. This plan is the implementation roadmap; the spec is the
load-bearing design document. Keep them coherent.

---

## How it works in practice

> Read this section to understand *what the user/agent sees and does*.
> Architectural rationale follows below; phase-level implementation in
> the Theme sections.

### The mental model — three channels, one rendered output

```
INPUT CHANNELS (sources of env state)                    OUTPUT (sink)
────────────────────────────────────                     ────────────
 ┌─ project.envVariables                          ┐
 │  Zerops project state, cross-service           │
 │  Lives: import.yml + zerops_env action=set     │
 │  E.g.: APP_KEY (generated 1×), JWT_SECRET      │
 │                                                │     ┌──────────┐
 ├─ zerops.yaml run.envVariables                  ├─→  │  .env    │
 │  Per-service deployed runtime, in git repo     │     │ (CWD,    │
 │  E.g.: APP_ENV=production, DATABASE_URL=       │     │ ZCP-     │
 │       ${db_connectionString}                   │     │ rendered)│
 │                                                │     └──────────┘
 └─ .env.local                                    ┘
    User-authored local-only overlay
    Lives: CWD, gitignored, ZCP create-once-never-overwrite
    E.g.: APP_ENV=local, LOG_LEVEL=debug, override DATABASE_URL
```

**Ownership rules**:

| Use case | Channel |
|---|---|
| Same value local + deployed (secrets, API keys) | `project.envVariables` |
| Derived from managed service | `zerops.yaml run.envVariables` with `${svc_*}` ref |
| Deployed-only (prod feature flag, NODE_ENV=production) | `zerops.yaml run.envVariables` hardcoded |
| Local-only override (APP_ENV=local, debug flags) | `.env.local` |

**`.env` is fully derived**. Re-running `generate-dotenv` reproduces it
deterministically from the three channels. Delete `.env` → next regen
restores it. Edit `.env` directly → next regen refuses with dry-run diff,
asks user to move keys to `.env.local`.

**`.env.local` is the user-override channel**. ZCP creates it ONCE during
bootstrap/adoption (seeded with detected local-mode flags), then never
touches it again. User edits freely; values always win at merge.

### Scenario A — Recipe-driven local

User: "set up nodejs-hello-world recipe in local mode."

```
1. zerops_workflow workflow="bootstrap" → matches recipe route, env=local
2. ZCP transforms import.yml:
   - drops services with zeropsSetup: dev (the would-be appdev)
   - strips buildFromGit from remaining runtimes (force first local deploy)
3. zerops_import → creates {appstage in READY_TO_DEPLOY, db}
4. Atom guides agent: empty-CWD check → git clone <recipe.repo> .
5. ZCP creates .env.local SEEDED from recipe's dev setup block:
       # Created by ZCP. Edit freely — ZCP will not overwrite this file.
       # ZCP merges these values into .env at every generate-dotenv.
       APP_ENV=local
       LOG_LEVEL=debug
6. zerops_env action=generate-dotenv setup=prod
   → .env rendered: APP_KEY (project) + DATABASE_URL=${db_conn} (zerops.yaml)
                    + APP_ENV=local + LOG_LEVEL=debug (.env.local overlay)
7. composer install / npm install (per recipe stack)
8. Run locally: app starts, connects to db via VPN
9. zerops_deploy targetService=appstage workingDir=<cwd>
   → first local deploy — pipeline verified end-to-end, stage URL goes live
```

Bootstrap result: stage runtime running user's local code, `.env` is
working artifact, `.env.local` carries developer-specific overrides.

### Scenario B — Greenfield, empty CWD

User: "I want to start a Node + Postgres project in local mode."

```
1. ZCP discovers intent (route=classic, env=local), classifies stack
2. ZCP synthesizes:
   - import.yml: project + services (app + db) + project.envVariables
   - zerops.yaml in CWD: build/run blocks + run.envVariables
   - minimal app skeleton (package.json, src/index.js)
   - .env.local seeded with NODE_ENV=development
3. zerops_import → Zerops creates project, generates system vars
4. zerops_env action=generate-dotenv setup=app
   → .env: project-env values + resolved zerops.yaml refs + .env.local overlay
5. npm install && npm run dev
6. Iterate locally, push to deploy when ready
```

### Scenario C — Brownfield, existing project + .env (DESIGN-ONLY this wave)

User: "deploy this existing Express app to Zerops in local mode."

```
1. ZCP reads CWD: existing .env, package.json, framework signals
2. ZCP runs zerops_env action=classify-dotenv → distribution proposal:
   {
     to-project-env:     [JWT_SECRET, STRIPE_API_KEY],
     to-zerops-yaml:     [DATABASE_URL=${db_conn}, NODE_ENV=production],
     to-env-local:       [APP_ENV=local, LOG_LEVEL=debug],
     ambiguous:          [some external URL, requires user confirmation]
   }
3. User confirms / corrects classification
4. ZCP backs up original .env → .zcp/state/backups/dotenv/<ts>.env (0600)
5. ZCP writes import.yml + zerops.yaml + .env.local from proposal
6. zerops_import → resolved system vars now available
7. generate-dotenv → fresh .env rendered
8. User runs locally, deploys when validated
```

### Lifecycle events (cross-scenario)

| Event | What changes | Channel | After regen |
|---|---|---|---|
| Add shared secret (NEW_API_KEY) | `zerops_env action=set scope=project` | project.envVariables | Appears in `.env` |
| Add deployed-only var (PROD_FLAG) | edit `zerops.yaml run.envVariables` | zerops.yaml | Appears in `.env` (and deployed on push) |
| Add local-only override (DEBUG=verbose) | edit `.env.local` | `.env.local` | Appears in `.env`, persists across regens |
| Override ZCP-managed key locally | put key in `.env.local` (e.g. `DATABASE_URL=postgres://localdev/...`) | `.env.local` | User value wins in `.env` |
| Add managed service (redis) | `zerops_import` extension + edit `zerops.yaml` (REDIS_URL=${redis_*}) | zerops.yaml | REDIS_URL appears in `.env` |
| Rotate secret (APP_KEY) | `zerops_env action=set scope=project` | project.envVariables | New value in `.env` (unless `.env.local` masks; warn) |
| Retake ZCP-managed key | delete from `.env.local` | (release) | Base value resumes |
| User commits `.env.local` accidentally | (detected) | warn high-severity | "Move shared values to project env or zerops.yaml; document examples in `.env.local.example`" |
| User edits `.env` directly | (detected on next regen) | refuse | Dry-run diff: "extra keys / changed values present in `.env` that aren't in any source — move to `.env.local` or use `force=true`" |
| Multi-machine clone | new dev clones repo | (per-developer) | `.env.local` not in git; `.env.local.example` (committed) shows expected keys |
| VPN down during regen | API resolve fails | refuse | Prior `.env` left intact; clear error: "VPN required for system var resolve, retry after `zcli vpn up`" |

### Edge cases (the non-obvious ones)

- **Multi-setup zerops.yaml** (e.g. monorepo with `app` + `worker` blocks):
  `generate-dotenv setup=<name>` is mandatory; ZCP refuses bare invocation
  with "specify --setup; available: app, worker". Optional
  `--output .env.<setup>` for advanced cases (default writes single
  `.env` per call).
- **Framework `.env.local` collision** (Vite, Next, Symfony all natively
  load `.env.local`): harmless when values match (which they do, ZCP
  merges `.env.local` into `.env`). ZCP warns hard if `.env.local` is
  git-tracked — that promotes per-developer state to team state.
- **Variable interpolation in dotenv** (`URL=${PROTOCOL}://${HOST}`):
  ZCP preserves raw values; only resolves Zerops `${svc_var}` refs from
  zerops.yaml. Generic shell-style interpolation passes through verbatim.
- **Concurrent regen**: advisory lock per output file (released on
  process exit); atomic rename for write.

### What user/agent does vs what ZCP does

| ZCP does | User/agent does |
|---|---|
| Generates `.env` from sources | Edits `.env.local` for local overrides |
| Creates `.env.local` once during bootstrap (seeded) | Adds new shared secrets via `zerops_env action=set scope=project` |
| Resolves `${svc_*}` refs | Adds new deployed vars via `Edit zerops.yaml` |
| Refuses to clobber unowned `.env` edits | Adds new managed services via `zerops_import` extension |
| Surfaces dry-run diff before write | Reviews dry-run before approving regen |
| Backs up brownfield `.env` before adoption | Confirms classification proposal in scenario C |
| Adds `.env`, `.env.local` to `.gitignore` | Maintains `.env.local.example` for team-shared documentation |
| Warns on git-tracked `.env.local` | Resolves warnings (move to project env, etc.) |

---

## Architectural decisions (locked)

### A1 — Three-channel env model with `.env.local` as ZCP-merge-time overlay

`.env.local` is **NOT a framework-loaded overlay** (which would fail
for Laravel/Django/plain Go). It's a **ZCP-merge-time source**: ZCP
reads it during `generate-dotenv` and merges into `.env`, which is the
single artifact every framework reads. This gives framework-agnostic
override semantics without inventing per-framework adapters.

### A2 — EnvPlan state-convergence primitive (Theme 0 core)

A typed `EnvPlan` carries each rendered key with metadata:

```go
type EnvKey struct {
    Key       string
    Value     string
    Source    EnvSource          // project | yaml-setup | local-overlay | brownfield-import
    Scope     EnvScope           // shared | deployed-runtime | local-override | managed-ref
    Sinks     []EnvSink          // .env | .env.local | shell-export | zerops-yaml
    Conflict  ConflictStatus     // clean | shadow | override
}

type EnvPlan struct {
    Setup     string             // setup block selected from zerops.yaml
    Keys      []EnvKey
    Generated time.Time
}
```

Renderers (`.env`, dry-run diff, shell export, future container-mode
review) are thin formatters over `EnvPlan`. Pinned by `TestEnvPlan_*`
tests.

### A3 — Recipe local = drop appdev + strip buildFromGit + force first deploy

Local-mode recipe transform produces single-runtime topology where stage
runtime starts in `READY_TO_DEPLOY` (no upstream auto-seed). First local
deploy is mandatory in bootstrap flow → pipeline verified end-to-end as
part of bootstrap, not deferred. Symmetric with classic local-mode shape.

### A4 — `setup` parameter is first-class

`generate-dotenv` takes `setup=<name>` parameter. `serviceHostname` is
deprecated for env-render entry points (still used elsewhere). Multi-setup
zerops.yaml refuses bare invocation; lists available setup names.

### A5 — Dry-run mode for safety (replaces manifest-based ownership)

`zerops_env action="generate-dotenv" preview=true` returns the EnvPlan
diff against existing `.env` without writing. Default write refuses when
existing `.env` has extra keys or changed values not in any source —
caller must `force=true` or move keys to `.env.local`. No hidden manifest
state; everything reconstructable from sources.

### A6 — `.env.local` create-once-never-overwrite

ZCP may create `.env.local` once during explicit bootstrap/adoption,
seeded with detected local-mode flags (recipe `dev` setup overrides for
scenario A; framework defaults for scenario B; classified entries for
scenario C). After creation, ZCP NEVER writes to `.env.local`. The file
is the user's no-touch zone.

### A7 — Service-level user-defined envVariables wholesale-excluded (provisional)

Until Zerops API surfaces user-vs-system provenance flag, service-level
envVariables are NOT included in `.env` rendering wholesale. Only project
envVariables and resolved zerops.yaml refs flow to `.env`. Promotion to
included-with-shadow-warning waits for API enhancement.

### A8 — VPN-down policy: fail, leave prior `.env` intact

If `${svc_var}` resolution fails (Zerops API down or service not yet
RUNNING), `generate-dotenv` returns error with VPN/retry hint and does
NOT write a partial `.env` or placeholder values. Prior `.env` remains
the operative file.

---

## How an LLM implementer should approach this plan

1. Read top-to-bottom before starting.
2. **Order**: Theme 0 first (foundational primitive, no behavior change
   for existing callers). Then Theme 2 (uses Theme 0, atoms describe
   model). Then Theme 1 (recipe local-flow, uses both).
3. TDD per ZCP convention: RED → GREEN → tests + lint + race → commit.
   Pure refactors skip RED.
4. Container regression non-negotiable: every Theme 0 phase has explicit
   container-mode test coverage. Existing `generate-dotenv` callers
   (other than local-mode-bootstrap) must see no behavior change unless
   the test is explicitly updated.
5. **Aleš coordination triggers**:
   - Recipe corpus shape changes (parallel `<slug>.local.import.yml`).
   - Recipe synthesizer integration with bootstrap-consumable variants.
   - Recipe markdown edits beyond minor cross-references.
6. **Theme 3 is design-only this wave**. Brownfield-adopt implementation
   ships as a separate plan. The architectural decisions A1-A8 are
   chosen so Theme 3's future implementation doesn't require revisiting
   Theme 0/1/2.

---

## Theme 0 — EnvPlan primitive (foundational)

### Phase 0A — `EnvPlan` type + metadata

**Why**: Current `internal/ops/env_generate.go` returns a flat string
`map[string]string`. No provenance, no scope, no conflict tracking.
Renderers can't say "this key came from `.env.local` overlay" or "this
key is shadowed by a service-level user-defined env."

**What**:
- New `internal/ops/env_plan.go` with `EnvPlan`, `EnvKey`, `EnvSource`,
  `EnvScope`, `EnvSink`, `ConflictStatus` types.
- `BuildEnvPlan(ctx, project, setup, cwd) (*EnvPlan, error)` — gathers
  sources, applies precedence, produces typed plan.
- `(*EnvPlan).Render(sink EnvSink) ([]byte, error)` — formats for
  `.env` / shell-export / dry-run-diff sinks.
- Stable key ordering: alphabetical within each source, sources merged
  in precedence order (project → yaml-setup → local-overlay).

**Tests**:
- `TestBuildEnvPlan_SourcePrecedence`.
- `TestBuildEnvPlan_OverlayWinsOnConflict`.
- `TestBuildEnvPlan_StableKeyOrdering`.
- `TestEnvPlan_RenderDotenv_FormatStability`.

**Size**: ~150 LOC + tests.

### Phase 0B — Refactor `env_generate.go` to use EnvPlan

**Why**: `env_generate.go` currently does flat resolve + write. After 0A
exists, refactor it to: build EnvPlan via 0A, render via
`Plan.Render(SinkDotenv)`, atomic write.

**What**:
- `EnvGenerateDotenv` becomes a thin wrapper over `BuildEnvPlan` +
  `Render` + `atomicWrite`.
- All existing tests must still pass (no behavior change for current
  callers — `setup` parameter defaults to legacy `serviceHostname`
  matching for backwards compat during migration).

**Tests**:
- All existing `TestEnvGenerateDotenv_*` stay green.
- `TestEnvGenerateDotenv_UsesEnvPlanInternally` (asserts call path).

**Size**: ~80 LOC delta + zero new tests (existing tests pin behavior).

### Phase 0C — `setup` parameter (first-class)

**Why**: `serviceHostname` is overloaded — recipe setup names (`dev`,
`prod`, `worker`) aren't always service hostnames. Multi-setup
`zerops.yaml` needs explicit selection.

**What**:
- `zerops_env` action `generate-dotenv` accepts `setup` parameter
  (string).
- When `setup` is empty: detect setup blocks in CWD's `zerops.yaml`.
  - Single block: use that one (no error).
  - Multiple blocks: refuse with `ErrSetupRequired`, list available
    setup names.
  - Zero blocks: legacy fallback to `serviceHostname` matching.
- Existing `serviceHostname` parameter still accepted (deprecation
  warning in result for next major).

**Tests**:
- `TestGenerateDotenv_SetupExplicit_PicksMatchingBlock`.
- `TestGenerateDotenv_SetupMissing_MultipleBlocks_Refuses`.
- `TestGenerateDotenv_SetupMissing_SingleBlock_AutoPicks`.
- `TestGenerateDotenv_LegacyHostname_StillWorks_WithWarning`.

**Size**: ~60 LOC + tests.

### Phase 0D — Dry-run mode

**Why**: Codex's safety recommendation to replace manifest-based
ownership tracking. Before any write, surface what would change.

**What**:
- `zerops_env action="generate-dotenv" preview=true` builds EnvPlan,
  reads existing `.env`, computes diff, returns:
  ```
  {
    plan: EnvPlan,
    diff: { added: [], modified: [], removed: [], unowned: [] },
    wouldWrite: bool,
  }
  ```
- `unowned`: keys in existing `.env` not in EnvPlan and not in `.env.local`
  → user manually edited `.env`.
- Default write (`preview=false`) refuses when `unowned` non-empty
  unless `force=true`.

**Tests**:
- `TestGenerateDotenv_PreviewReturnsDiff`.
- `TestGenerateDotenv_RefusesUnownedEdits`.
- `TestGenerateDotenv_ForceOverridesUnowned`.

**Size**: ~70 LOC + tests.

### Phase 0E — Atomic write + advisory lock

**Why**: Concurrent regens race. Atomic rename prevents torn files but
doesn't serialize the read-modify-write.

**What**:
- Lock file `.zcp/state/locks/dotenv-<setup>.lock` (flock-style).
- Acquire on regen entry, release on completion (defer).
- Lock file gitignored.

**Tests**:
- `TestGenerateDotenv_ConcurrentInvocations_Serialize`.

**Size**: ~30 LOC + tests.

### Phase 0F — VPN-down / API-resolve-fail policy

**Why**: A6 — don't write placeholder, fail with prior `.env` intact.

**What**:
- `BuildEnvPlan` distinguishes "ref unresolved (transient)" vs "ref
  invalid (permanent)".
- Transient → returns `ErrRefResolveTransient` with VPN hint.
- Permanent (e.g. ref to non-existent service) → `ErrRefInvalid`.
- Caller's existing `.env` not touched on either.

**Tests**:
- `TestBuildEnvPlan_TransientResolveFail_ReturnsErrRefResolveTransient`.
- `TestGenerateDotenv_VPNDown_LeavesPriorEnvIntact`.

**Size**: ~40 LOC + tests.

### Phase 0G — Brownfield import surface (skeleton for Theme 3)

**Why**: Theme 3 needs `EnvSource: brownfield-import` to slot in. Skeleton
ensures Theme 0 doesn't have to be revisited later.

**What**:
- `EnvSource` enum value `SourceBrownfieldImport` defined.
- `BuildEnvPlan` accepts optional `brownfieldOverrides map[string]string`
  parameter; merges at appropriate precedence (between project and
  yaml-setup, since brownfield values were "user's previous truth").
- Unused this wave; reserved for Theme 3.

**Tests**:
- `TestBuildEnvPlan_BrownfieldImport_MergesAtCorrectPrecedence`.

**Size**: ~30 LOC + tests.

**Theme 0 total**: ~460 LOC, 7 phases, foundational.

---

## Theme 2 — Three-channel env model + atoms

### Phase 2A — `develop-local-env-channels.md` atom (foundational)

**Why**: Atoms today don't articulate the three-channel model. LLM agents
guess where to put new vars; failure mode is "added everywhere" or
"added in wrong channel, doesn't reach consumers."

**What**: New atom `internal/content/atoms/develop-local-env-channels.md`:
- Filters: `routes: [classic, recipe]`, `environments: [local]`,
  `phases: [develop-active]`.
- Content: the three-channel table from "How it works in practice" §
  "Mental model"; ownership rules; brief decision tree.
- Cross-ref: `develop-env-var-channels.md` (existing, env-agnostic) gets
  a local-mode addendum pointing to the new atom.

**Tests**:
- Axis-filter tests via `atoms_lint`.
- `TestSynthesize_LocalDevelopEnvChannelsAtomFires`.

**Size**: ~80 LOC content + tests.

### Phase 2B — `.env.local` create-once-never-overwrite mechanism

**Why**: A6 — ZCP creates `.env.local` once during bootstrap, never
afterwards.

**What**:
- New `internal/ops/env_local_overlay.go` with:
  - `EnsureEnvLocal(cwd, seed map[string]string) error` — writes
    `.env.local` if absent (with header comment + seed entries); returns
    `ErrAlreadyExists` if present (caller decides if that's an error).
- Recipe-local bootstrap (Theme 1) calls `EnsureEnvLocal` with extracted
  dev-setup overrides.
- Greenfield bootstrap (Theme 2 atom guidance) calls `EnsureEnvLocal`
  with framework-default flags (NODE_ENV=development for node, etc.).
- Header is fixed:
  ```
  # Created by ZCP. Edit freely — ZCP merges these values into .env at
  # every generate-dotenv but will not overwrite this file.
  # Add ".env.local" to .gitignore if not already there.
  ```

**Tests**:
- `TestEnsureEnvLocal_CreatesWhenAbsent`.
- `TestEnsureEnvLocal_RefusesWhenPresent`.
- `TestEnsureEnvLocal_HeaderStable`.

**Size**: ~50 LOC + tests.

### Phase 2C — Lifecycle event atoms

**Why**: Each lifecycle row in "How it works in practice" needs an atom
so agents have explicit guidance for the common cases.

**What** (atoms):
- `develop-local-env-add-shared-secret.md` — agent uses
  `zerops_env action=set scope=project`, then regen.
- `develop-local-env-add-deployed-only.md` — edit zerops.yaml run.envVariables,
  deploy.
- `develop-local-env-add-local-override.md` — edit `.env.local`, regen.
- `develop-local-env-add-managed-service.md` — extend project, edit
  zerops.yaml `${svc_var}` ref, regen.
- `develop-local-env-rotate-secret.md` — set in project, regen, warn
  about session invalidation.
- `develop-local-env-retake-key.md` — delete from `.env.local`, regen.

All filters: `routes: [classic, recipe]`, `environments: [local]`,
`phases: [develop-active]`.

**Tests**:
- Axis-filter tests via `atoms_lint`.
- `TestSynthesize_LifecycleAtoms_FireOnLocalDevelop`.

**Size**: ~250 LOC content + tests.

### Phase 2D — `develop-local-env-troubleshoot.md` atom

**Why**: Edge cases need explicit guidance — committed `.env.local`,
unowned edits, VPN down, multi-setup confusion. Reactive atom for when
an error surfaces.

**What**: New atom describing recovery paths for each error in lifecycle
table edge cases:
- "ZCP refused write because of unowned `.env` edits" → move keys to
  `.env.local` or `--force`.
- "Tracking `.env.local` in git" → move shared values to project env.
- "VPN down on regen" → `zcli vpn up`, retry.
- "Multi-setup ambiguity" → specify `--setup`, list of setups in error.

**Tests**:
- Filter via `atoms_lint`.

**Size**: ~80 LOC content + tests.

### Phase 2E — Status check via EnvPlan dry-run

**Why**: Lifecycle "is `.env` stale?" detection needs a check.
EnvPlan dry-run is the natural primitive.

**What**:
- New `internal/tools/workflow_checks_local_env.go::checkLocalDotenvFresh`.
- Wired into `zerops_workflow action="status"` lifecycle (local mode only).
- Logic: builds EnvPlan via 0A, compares to existing `.env`, surfaces
  status:
  - `fresh` (no diff)
  - `stale` (yaml or project env changed since `.env` mtime)
  - `unowned-edits` (`.env` has keys not in plan, not in `.env.local`)
  - `missing` (`.env` doesn't exist)
  - `vpn-down` (transient resolve fail)
- Recovery hint: `tool=zerops_env`, `action=generate-dotenv`,
  `args.setup=<detected>`, `args.preview=true` for inspection first.

**Tests**:
- `TestCheckLocalDotenvFresh_*` table.

**Size**: ~80 LOC + tests.

**Theme 2 total**: ~540 LOC, 5 phases.

---

## Theme 1 — Recipe-route in local-flow

### Phase 1A — `LocalizeRecipeImportYAML` (drop dev + strip buildFromGit)

**Why**: Today's recipe import.yml provisions appdev + appstage with
buildFromGit. Local mode needs single runtime in READY_TO_DEPLOY for
A3 force-first-deploy semantics.

**What**:
```go
// internal/workflow/recipe_import_local.go (new)

// LocalizeRecipeImportYAML transforms a container-shape recipe import.yml
// into local-mode shape: drops services with zeropsSetup: dev, strips
// buildFromGit from remaining runtime services. Uses yaml.Node round-trip
// to preserve comments and field ordering.
func LocalizeRecipeImportYAML(yamlContent string) (string, error)
```

**Wire-in**:
- `internal/workflow/bootstrap_guide_assembly.go::formatRecipeImportYAMLForGuide`
  + `internal/workflow/engine.go::BootstrapCompletePlan`: route through
  `LocalizeRecipeImportYAML` when `EnvLocal`.

**Tests**:
- `TestLocalizeRecipeImportYAML_DropsZeropsSetupDev`.
- `TestLocalizeRecipeImportYAML_StripsBuildFromGit`.
- `TestLocalizeRecipeImportYAML_PreservesStageZeropsSetupAndManaged`.
- `TestLocalizeRecipeImportYAML_NoOpForRecipesAlreadyLocalShape` (e.g.
  `nextjs-ssr-hello-world`).
- `TestLocalizeRecipeImportYAML_PreservesYAMLNodeOrdering`.
- `TestRewriteRecipeImportYAML` stays green (container-mode regression).

**Size**: ~100 LOC + tests.

### Phase 1B — Loosen `workflow_checks` + `bootstrap_outputs`

**Why**: `checkServiceRunning` expects DevHostname; after 1A there isn't
one. `bootstrap_outputs` should write `Mode=local-stage`.

**What**:
- `internal/tools/workflow_checks.go::checkServiceRunning`: when
  `EnvLocal` + recipe route, skip DevHostname existence check; only
  validate stage runtime in READY_TO_DEPLOY (after 1A) + managed deps.
- `internal/workflow/bootstrap_outputs.go`: when `EnvLocal` + plan has
  stage runtime, write `Mode=PlanModeLocalStage` with
  `StageHostname=<runtime>`.

**Tests**:
- `TestCheckServiceRunning_LocalRecipe_SkipsDevHostname`.
- `TestCheckServiceRunning_LocalRecipe_AcceptsReadyToDeploy`.
- `TestCheckServiceRunning_ContainerRecipe_StillRequiresDev` (regression).
- `TestBootstrapOutputs_LocalRecipe_WritesLocalStageMode`.

**Size**: ~50 LOC + tests.

### Phase 1C — Plumb `RecipeMatch.Repo`

**Why**: Recipe markdown frontmatter has `repo:`; `RecipeMatch` drops it.
Atom needs URL for `git clone`.

**What**:
- `internal/workflow/route.go`: add `Repo string` field to `RecipeMatch`.
- `internal/workflow/recipe_corpus_store.go`: read frontmatter `repo:`
  during corpus load, populate.
- `bootstrap-recipe-match.md` atom template-vars surface includes `repo`.

**Tests**:
- `TestRecipeCorpusStore_LoadsRepoFromFrontmatter`.
- `TestBuildBootstrapRouteOptions_RecipeRouteCarriesRepoURL`.

**Size**: ~30 LOC + tests.

### Phase 1D — Atoms (clone + provision + force-deploy + match modification)

**Why**: Recipe local-mode flow needs explicit atom guidance. Without it
agents guess (Codex confirmed this is what happens today).

**What**:
1. **NEW** `bootstrap-recipe-local-clone.md`:
   - Filters: `routes: [recipe]`, `environments: [local]`,
     `steps: [discover]`.
   - Content: empty-CWD verify → `git clone <recipe.repo> .` → upstream
     remote stays connected (note about `git remote set-url origin`).
2. **NEW** `bootstrap-recipe-import-local.md`:
   - Filters: `routes: [recipe]`, `environments: [local]`,
     `steps: [provision]`.
   - Content:
     - "ZCP transformed import.yml: appdev dropped, buildFromGit stripped.
       Stage starts in READY_TO_DEPLOY. Subdomain URL not live until first
       local deploy completes."
     - "After services ready: `zerops_env action=generate-dotenv setup=prod`
       → `.env`. ZCP also creates `.env.local` seeded with dev-setup
       overrides (APP_ENV=local, etc.)."
     - "Run app locally (`composer install && php artisan serve` or
       framework equivalent). VPN required for managed-service access:
       `zcli vpn up`."
     - "**First deploy is mandatory bootstrap step**:
       `zerops_deploy targetService=<stage> workingDir=<cwd>`. This
       verifies the build pipeline + runtime + env wiring. Without it,
       stage subdomain returns 502/503."
3. **MODIFY** `bootstrap-recipe-match.md`:
   - Add qualifier: "(container only; in local mode you'll clone the
     recipe repo locally — see `bootstrap-recipe-local-clone`)."

**Tests**:
- `TestSynthesize_LocalRecipeProvisionAtomFires`.
- `TestSynthesize_ContainerRecipeProvisionUnchanged` (regression).

**Size**: ~180 LOC content + tests.

### Phase 1E — Live verification

**Why**: A3 (strip + force-deploy) is new behavior. Verify against real
Zerops.

**What**:
- New scenario `eval/behavioral/scenarios-local/recipe-nodejs-hello-world.md`:
  - Pre-seed: empty Zerops project, empty CWD.
  - Prompt: "Use the nodejs-hello-world recipe to set up a Node + Postgres
    project."
  - Expected: agent transforms import.yml, clones repo, ZCP creates
    `.env.local`, generates `.env`, app runs locally, **agent deploys
    to stage as bootstrap completion**, stage URL becomes live.
- Run via `make flow-eval-local ID=recipe-nodejs-hello-world`.

**Tests**: scenario fixture + retrospective surfacing.

**Size**: ~120 LOC scenario.

**Theme 1 total**: ~480 LOC, 5 phases.

---

## Theme 3 — Brownfield-adopt subroute (DESIGN-ONLY this wave)

### Architectural sketch

Brownfield-adopt is a **subroute under local-mode bootstrap**, sibling to
recipe-route and classic-greenfield-route. Detected via signal: CWD has
a non-empty `.env` AND framework signals (package.json, composer.json,
go.mod, etc.) AND no Zerops integration (no zerops.yaml, no `.zcp/state`).

```
zerops_workflow workflow="bootstrap" → discover phase:
  cwd-empty? → classic-greenfield route
  has-zerops-yaml? → adopt-existing-yaml route (current behavior)
  has-non-empty-.env + framework-signals? → BROWNFIELD-ADOPT route (new)
  catalog-match? → recipe route
```

### Adoption transaction protocol

```
1. zerops_env action="classify-dotenv" cwd=<dir>
   → returns ClassificationProposal with per-key suggestions:
     {
       toProjectEnv:    [JWT_SECRET, STRIPE_API_KEY],
       toZeropsYaml:    [{key, value, refType?}],
       toEnvLocal:      [APP_ENV=local, LOG_LEVEL=debug],
       requiresService: [{key, suggestedService: postgresql@18}],
       ambiguous:       [{key, candidates, reasoning}],
     }
2. Atom guides agent to surface proposal to user; user confirms / edits.
3. zerops_env action="adopt-dotenv" cwd=<dir> proposal=<confirmed>
   → backs up original `.env` to .zcp/state/backups/dotenv/<ts>.env (0600);
   → writes import.yml + zerops.yaml + .env.local from proposal;
   → returns next-step: zerops_import.
4. zerops_import → Zerops creates project + system vars resolve.
5. generate-dotenv setup=<auto-picked> → fresh .env from new sources.
6. Agent surfaces backup path so user can recover if classification was off.
```

### Classification heuristics (full library in next-wave plan)

Per Codex output:

- **Managed-service candidates** (suggest service + ref): URL-scheme
  match (postgres://, redis://, mongodb://, mysql://, amqp://, nats://);
  hostname patterns (localhost, db, postgres, redis, cache); standard
  ports.
- **Shared app secrets** (project env): `APP_KEY`, `APP_SECRET`, `JWT_*`,
  `SECRET_*`, `*_KEY`, `*_TOKEN`. Preserve existing values (rotation
  breaks sessions).
- **External secrets** (project env): provider prefixes (`STRIPE_*`,
  `OPENAI_*`, `MAILGUN_*`, etc.).
- **Mode/local flags** (split: zerops.yaml=production, `.env.local`=local):
  `NODE_ENV`, `APP_ENV`, `RAILS_ENV`, `ASPNETCORE_ENVIRONMENT`, `DEBUG`.
- **Local-only** (`.env.local`): `LOG_LEVEL=debug`, `MOCK_*`, `LOCAL_*`,
  `XDEBUG_*`.
- **Plain config** (zerops.yaml + optionally `.env.local`): `PORT`,
  `*_TIMEOUT`, public URLs.

### Confirmation rules

Auto-distribute only:
- Mode flags with explicit `local`/`development` value → `.env.local`.
- `LOG_LEVEL=debug` → `.env.local`.
- `DATABASE_URL` to `localhost:5432` when user explicitly requested
  Zerops Postgres and no external DB evidence.

Require explicit confirmation:
- External-looking hostname.
- Any provider/payment token (sensitivity high).
- Any app encryption key (rotation risks).
- Ambiguous URLs (could be local Docker vs external managed DB).

### Trigger to promote (start the next-wave plan)

- Theme 0/1/2 of THIS plan ship.
- A flow-eval-local retrospective surfaces brownfield-adopt friction
  ("user had existing app, ZCP couldn't bootstrap cleanly").
- A second concrete scenario surfaces beyond Express+Postgres.

### Estimated next-wave size

~300 LOC implementation + ~150 LOC classification heuristics + 4-6 atoms
+ live verify scenario.

**This wave: 0 LOC for Theme 3**, just the architectural skeleton in
Theme 0 (`SourceBrownfieldImport` enum) and the design-doc above.

---

## Out of scope — Aleš coordination items

- Recipe corpus shape changes (parallel `<slug>.local.import.yml`).
  Programmatic transform via `LocalizeRecipeImportYAML` is the chosen
  path; if it proves brittle escalate.
- Recipe markdown content rewrites beyond the cross-reference. Recipe
  knowledge files are gitignored (Strapi-synced); `zcp sync push`
  amplification per CLAUDE.local.md.
- Recipe synthesizer integration with bootstrap-consumable variants.
- Brownfield-adopt UI in dashboard / recipe catalog (UX surface, not
  ZCP scope).

---

## Risk register

1. **Theme 0 blast radius beyond local-mode** — `env_generate.go`
   refactor affects every caller. Phase 0B explicitly asserts behavior
   parity for existing tests; Phase 0C deprecation-warning path
   preserves backwards compat for `serviceHostname` callers.

2. **YAML node round-trip in 1A** — recipe YAMLs carry comments + field
   ordering. Use `yaml.Node` parse/marshal (preserves structure), mirror
   `recipe_override.go::RewriteRecipeImportYAML`.

3. **`.env.local` create-once contract leak** — if a code path other than
   `EnsureEnvLocal` ever writes `.env.local`, the contract breaks.
   `internal/ops/env_local_overlay.go` is the single writer; lint test
   (`atoms_lint` extension) forbids `os.WriteFile` calls targeting
   `.env.local` outside that file.

4. **VPN-down false-positive in CI** — Phase 0F policy fails regen on
   transient resolve. CI tests use mock client, not real platform; OK.
   Live e2e tests (`-tags e2e`) need VPN up before running.

5. **Theme 2 atom drift** — six new lifecycle atoms reference each other.
   `atoms_lint` axis tests catch incoherence; reference graph
   maintained via `references-atoms` frontmatter field.

6. **First-deploy step not completed by agent in 1E** — A3 mandates
   first deploy as bootstrap step. If agent skips it, stage subdomain
   stays 502. Atom guidance MUST be unambiguous; flow-eval-local
   retrospective surfaces if agent skips.

7. **`.env.local` framework collision (Vite/Next/Symfony)** — when
   framework also natively loads `.env.local`, double-merge. Same
   values, harmless. Atom guidance documents this; no code change.

8. **Theme 3 design assumptions** — classification heuristics are the
   core risk. Mitigated by: (a) Theme 3 is design-only this wave, (b)
   Theme 0's `SourceBrownfieldImport` enum is the only concrete coupling,
   (c) Theme 3 can pivot in next-wave plan without revisiting 0/1/2.

---

## Hand-off checklist

When starting work in a fresh session:

- Read this plan top-to-bottom.
- Read `docs/spec-env-handling.md` (authoritative design — mental
  model, source precedence, render policy, alternatives rejected).
- Read `docs/spec-local-dev.md` and the existing
  `internal/ops/env_generate.go`.
- Confirm preconditions (`git status` clean, full test sweep + lint
  clean, race clean).
- **Order**: Theme 0 phases 0A-0G → Theme 2 phases 2A-2E → Theme 1
  phases 1A-1E.
- TDD per CLAUDE.md: every phase is RED → GREEN → tests + lint + race
  → commit. Pure refactors skip RED.
- Container regression: every Theme 0 phase has explicit container-mode
  test coverage; existing `generate-dotenv` callers keep behavior
  unless test is explicitly updated.
- Live verify after Theme 1 Phase 1E: `make flow-eval-local
  ID=recipe-nodejs-hello-world`.
- Live verify Theme 0 + Theme 2 via existing
  `go test ./e2e/ -tags e2e -run TestE2E_EnvGenerateDotenv*` (extended
  with new EnvPlan tests).
- Single commit per phase.
- **DO NOT** start Theme 3 implementation — design-only this wave.

---

## Estimated size

- Theme 0: ~460 LOC + tests, 7 phases.
- Theme 1: ~480 LOC + 3 atom files + 1 e2e scenario, 5 phases.
- Theme 2: ~540 LOC + 7 atom files, 5 phases.
- Theme 3: 0 LOC this wave (design-only sketch + Theme 0 enum reservation).
- **Total: ~1480 LOC across ~30 files, 17 phases**.

For comparison: Phase 5-12 wave was ~1100 LOC / 22 files / 8 phases.
This plan is larger because Theme 0 is a foundational refactor that
generalizes beyond local-mode (will pay dividends in container-mode
env review, CI export, env-promotion in future waves).

If size is a concern, an alternative split:
- **Wave 1**: Theme 0 only (foundational, ~460 LOC, 7 phases).
- **Wave 2**: Theme 1 + Theme 2 (uses Wave 1 primitive, ~1020 LOC).
- **Wave 3**: Theme 3 brownfield-adopt (~450 LOC).

Each wave independently shippable; recommended unless single-wave
verifiability is critical.
