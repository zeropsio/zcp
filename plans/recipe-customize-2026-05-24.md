# Recipe Customization — Variant Compiler Design

**Status:** Draft 1 — design proposal, awaiting Karel's review
**Surfaced:** 2026-05-24 — WeTransfer scenario in `eval-zcp` triggered classic-route fallback because (a) showcase recipe lacked object-storage that user needed and (b) project already had `db/appdev/appstage` from another app. Agent went classic + invented `wt*` hostnames, losing every recipe benefit.

## Problem

Today two routes, nothing between:

| Route | What agent gets | What agent gives up |
|---|---|---|
| `recipe` (verbatim) | Canonical import.yml + RCO rewrites runtime hostnames + upstream `buildFromGit` for app code | Cannot rename managed, cannot add missing dep, cannot drop unwanted, cannot mix recipes |
| `classic` | Free hand on yaml | Recipe markdown, upstream app repo URL, env-key knowledge, tested topology |

4 customization scenarios all fall back to classic (verdict: hard) — `internal/workflow/route.go:229-242`, `internal/workflow/recipe_override.go:91-100`, `internal/content/atoms/bootstrap-recipe-import.md:36`.

### Why "just open the verbatim gate" doesn't work

Karel's initial proposal: let agent edit recipe import.yml + use inline `services[].zeropsYaml` field (live schema confirmed). One atom change, no new code.

This was rated **last of 8 alternatives** by Codex pass 1 (out of 19,396 lines of independent analysis). Codex pass 2 (fresh angle, no preconception of Karel's idea) independently rejected it as "shortest path to silent failure". Reasons:

1. **Wrong-service hijack — code-confirmed.** `internal/ops/env_refs.go:91-112` picks the **longest-matching live hostname prefix**. If project has another app's `db` and agent renames their `db` → `db_wt` but forgets `${db_hostname}` in upstream `zerops.yaml`, the stale ref **silently validates against the foreign db** and app silently connects to wrong infra. Pinned by `TestValidateEnvReferences_LoneRefIgnored`.

2. **Soft fallback to verbatim** — `internal/workflow/bootstrap_guide_assembly.go:95-98`: if rewrite fails, provision atom **falls back to verbatim with no signal**. For customization, this would hide every contract violation.

3. **`liveServices=nil` bug** — `internal/tools/workflow_bootstrap.go:41` passes nil to plan completion. Collision checks against live services don't run. Customization can't rely on this until fixed.

4. **`ValidateEnvReferences` runs only at deploy preflight** — `internal/ops/deploy_validate.go:404-435`. Agent-side edit-time, dangling refs are invisible until 24h later when first deploy fires.

5. **`buildFromGit` is upstream-only** — upstream `zerops.yaml` has fixed `${db_*}` refs. Rename without source rewrite means deploy time mismatch. Inline `zeropsYaml` field overrides this but **only for setups ZCP explicitly composes** — agent-edited inline composition is exactly the silent failure surface.

6. **28 tests + 11-case RCO table** pin today's invariants. Opening the gate without typed contract reshapes all of them simultaneously.

7. **Recipe identity drift** — slug is how telemetry/scenarios attribute behavior to recipes. After "started from X, then edited" the slug means nothing.

8. **Hardcoded literals beyond YAML** — string interpolation like `\`http://${SERVICE}_internal:3000\`` in app code is grep-invisible. ZCP cannot fix what it cannot see, but the gate would imply it can.

The verbatim gate makes the **agent** the consistency engine. ZCP today is built on the opposite assumption — narrow typed mutation. The gap can't be patched with one atom edit + a regex validator.

## Design — Recipe Variant Compiler

Same `recipe` route, **variant mode** inside it. When the agent's plan diverges from the canonical recipe, ZCP enters variant mode and runs a **compiler** before provision.

```
                    ┌─────────────────────────────────────────────┐
                    │  recipe route (one route, two modes)        │
                    │                                             │
  agent picks recipe ┌─────────────────┐    ┌─────────────────┐  │
  → discover         │  EXACT mode     │    │  VARIANT mode   │  │
                    │  (today)        │    │                 │  │
                    │                 │    │  + binding      │  │
                    │  RCO runtime    │    │    manifest     │  │
                    │  rename only    │    │  + compiler     │  │
                    │                 │    │  + materialized │  │
                    │                 │    │    source       │  │
                    │                 │    │  + contract     │  │
                    └─────────────────┘    └─────────────────┘  │
                            ↓                       ↓            │
                    canonical YAML        compiled YAML +        │
                    (unchanged)           variant contract       │
                    → provision           → provision            │
                    → close (current)     → close + contract     │
                    └─────────────────────────────────────────────┘
                            ↓                       ↓
                    deploy: current      deploy: + variant
                    preflight             preflight (contract check)
```

### Variant compiler inputs

**Recipe slot table** (extracted from recipe import.yml + recipe markdown's `zerops.yaml` snippets at compile time):
- `<slug>:<sourceHostname>` — fully qualified slot ID (`laravel-showcase:db`, `laravel-showcase:appdev`)
- Slot properties: type, mode, `zeropsSetup`, default hostname, env refs **consumed** by each runtime
- Ref graph: `appdev` consumes `db`, `redis`, `storage`

**Binding manifest** (agent submits in plan):
```json
{
  "bindings": [
    {"source": "laravel-showcase:db", "target": "maindb", "resolution": "CREATE"},
    {"source": "laravel-showcase:appdev", "target": "wtdev", "resolution": "CREATE"},
    {"source": "laravel-showcase:storage", "target": "wtstorage", "resolution": "CREATE"}
  ],
  "additions": [
    {"hostname": "search", "type": "meilisearch@1.20", "reason": "app needs full-text search", "mustConsume": false}
  ],
  "drops": ["laravel-showcase:redis"]
}
```

**No inference by hostname prefix. No closest-match. No cross-recipe matching unless agent explicitly binds.**

### Compiler output (CompiledRecipeVariant)

- Compiled import YAML (with renamed services, dropped services, added services)
- Generated `zerops.yaml` per runtime (with `${db_*}` → `${maindb_*}` rewrites — full ref graph traversal)
- Validation report: stale refs forbidden, missing refs forbidden, type ambiguity rejected
- Recipe source materialization plan (clone + patch the upstream repo to local working tree)
- Schema validation result (import YAML + each generated `zerops.yaml`)
- Variant contract written to session state

If compilation fails → **discover-complete fails**. Provision never starts with a half-valid variant.

### Source materialization (not `buildFromGit`)

For variant bootstraps, runtime services **do not** use upstream `buildFromGit`. Reason: upstream repo's `zerops.yaml` has hardcoded `${db_*}` refs that the compiler just rewrote. Build would silently use stale refs.

Instead:
1. ZCP creates runtime services with `startWithoutCode: true`
2. ZCP writes variant contract to state
3. Agent (in develop workflow) clones source into local working tree
4. Agent applies ZCP-computed `zerops.yaml` rewrites (provided as a diff)
5. First deploy validates against the variant contract — every rewritten ref must hit a live service; every dropped service ref must be absent.

Exact recipes continue to use `buildFromGit` unchanged.

### Per-scenario behavior

| Scenario | Variant behavior |
|---|---|
| Hostname collision rename | Agent binds `laravel-showcase:db → maindb`. Compiler rewrites refs `${db_*}` → `${maindb_*}` in all runtime setups. Deploy preflight blocks if any `${db_*}` survives. |
| Add missing dep | Agent declares `additions: [{hostname: "storage", type: "object-storage"}]`. Default: infra-only (no contract obligation). Set `mustConsume: true` to require source/env-read evidence at deploy. |
| Drop service | Agent declares `drops: ["laravel-showcase:redis"]`. Compiler rejects if any retained runtime's ref graph shows consumption of `redis` without replacement binding. Preflight scans source/env reads for removed prefix. |
| Mix recipes | Agent picks multiple recipe sources, binds slots by fully-qualified IDs. Shared services require explicit binding + type compatibility. Default-hostname coincidence across recipes is never auto-merged. v1: narrow scope — share managed deps only, no runtime mixing. |

## What this design prevents (silent failure mode coverage)

| Failure mode | Today | After |
|---|---|---|
| Wrong-service hijack via longest-prefix match | Silent | Impossible — explicit slot IDs, no inference |
| Stale `${old_*}` after rename | Lone-ref grace skips, 24h later | Compiler refuses + preflight blocks |
| Dropped service still consumed | Lone-ref grace skips, 24h later | Compiler refuses based on recipe ref graph |
| Added service not consumed | Goes unflagged | Default OK as infra-only; `mustConsume` upgrades to contract |
| Upstream `zerops.yaml` ref drift | Silent at build | Materialized source + computed rewrites + preflight |
| Hostname collision with live service | `liveServices=nil` skips | Plan-complete validates against live services (bug fix on the way) |
| Soft fallback to verbatim | Hides every variant violation | Hard-fail for variants — verbatim fallback gated on `RecipeVariantContract == nil` |
| Schema drift in generated yaml | Caught at deploy | Caught at compile time |

## Implementation phases

| Phase | Scope | Verifiable by | Effort |
|---|---|---|---|
| **1** | Bug fixes — `workflow_bootstrap.go:41` pass live services; `bootstrap_guide_assembly.go:95-98` no soft fallback for variants | Existing recipe tests still pass + 2 new regression tests | 0.5d |
| **2** | Recipe slot extraction — parse import.yml + recipe markdown `zerops.yaml` snippets into `RecipeSlot` + ref graph | Golden tests across all 36 recipes — every slot, setup, ref stable | 2d |
| **3** | Variant request types + binding validator — `RecipeVariantRequest`, `RecipeBinding`, schema, plan-time validation | Compile tests: rename, drop-consumed, add infra-only, type mismatch | 3d |
| **4** | Compiler — rename rewrites, drop drops, add inserts, generates final import.yml + per-runtime `zerops.yaml` | All scenarios from §"Per-scenario behavior" pass | 4d |
| **5** | Variant contract + state persistence — `BootstrapState.VariantContract`, JSON marshal, resume | Resume test, BC test on old state | 1d |
| **6** | Materialized-source bootstrap — agent receives source clone + `zerops.yaml` diff, deploy preflight enforces contract | E2E test in eval-zcp: WT scenario end-to-end | 3d |
| **7** | Atom corpus changes — variant guidance, slot-table interpretation, binding syntax, repair atoms | Atom lint + golden tests | 2d |
| **8** | Spec invariants — write F6 v2 (variant compiler), pin tests | Spec test references | 1d |

**Total**: ~16 person-days. Each phase verifiable before next starts.

## Backwards compat

- **36 existing recipes**: zero change. Exact mode = current verbatim path. Corpus tests assert byte-identical output for no-customization case.
- **Existing user sessions**: `BootstrapState.VariantContract` is optional. Old JSON unmarshals unchanged → exact mode by default.
- **Existing ServiceMeta**: optional `variantSource` field. Missing = legacy recipe/classic. No invariant tests change.
- **MCP tool surface**: `zerops_workflow action="complete" step="discover"` gains optional `recipeBindings` sidecar field. Empty = exact mode.

## What this design refuses

- **Soft-rename without source rewrite**. Either compiler proves source can be rewritten and rewrites it, or refuses + routes agent to classic. No "warn and proceed".
- **Hostname-pattern matching across recipes**. `db` and `db` from two different recipes are never treated as the same service.
- **Mid-session recipe corpus drift**. Variant contract pins a source snapshot/digest; corpus changes during a session don't break in-flight bootstrap.

## What was considered and rejected

| Design | Verdict | Why |
|---|---|---|
| A — Open verbatim gate (Karel's initial) | Rejected | Agent becomes consistency engine; 8 silent failure modes; longest-prefix hijack code-confirmed |
| B — Third route `recipe-seed` | Rejected | Duplicates route taxonomy; customization is a mode, not a lifecycle |
| C — RewriteRecipeImportYAML typed delta | Insufficient | Sees only import YAML — cannot prove source repo refs, app env reads, deploy correctness |
| D — Recipe template engine | Rejected | Requires re-authoring 36 recipes; shifts reliability to template authors |
| E — Shadow recipe cache | Subsumed | Useful primitive (in phase 5) but not a design on its own |
| F — Declarative customization DSL | Rejected | New parser surface; doesn't add reliability over typed bindings |
| H — Fork companion repo + git deploy | Rejected | Heavier user setup; git auth complexity; recipe customization shouldn't require fork |
| Scaffold-tool from `plans/backlog/recipe-scaffold-tool.md` | Adjacent | Different scope — agent gets content for `classic` use. Doesn't address rename/add/drop within recipe identity. Can ship in parallel. |

## Critical decisions for Karel

1. **Customized variants always materialize source + disable upstream `buildFromGit` when bindings change?** Recommended: yes. Otherwise refs drift silently.

2. **API shape — sidecar `recipeBindings` field vs source fields on `RuntimeTarget`/`Dependency`?** Recommended: sidecar table with namespaced source IDs. Doesn't pollute plan schema.

3. **Override for managed rename/drop when compiler can't prove source?** Recommended: no override — route to classic instead. Override = silent failure surface.

4. **Mix recipes v1 — allow shared managed deps across recipes?** Recommended: yes, but only with explicit namespaced binding + type compatibility. Runtime mixing in v2.

5. **Exact recipe route — permanently verbatim, or eventually through compiler in read-only mode?** Recommended: keep verbatim for BC now; later run exact recipes through compiler in assertion-only mode to validate the recipe contract itself.

## Open

- Aleš coordination: `internal/recipe/` is his scope (per `CLAUDE.local.md`). The variant compiler lives in `internal/workflow/` but recipe slot extraction needs to read recipe markdown structure. Coordinate before phase 2 starts.
- Atom budget: phase 7 adds 3-5 atoms (slot table, binding syntax, materialized source, preflight repair). Need to fit corpus pin density rules + axis-K/L/M lint.
- Phase 6 e2e test relies on eval-zcp infrastructure — flow-eval-local has the scaffolding; need to extend with WT scenario.

## Why this is worth doing

Karel's WT scenario today: agent went classic, invented `wt*` hostnames, lost recipe topology + recipe markdown + upstream knowledge. End state functional but every customization decision was re-discovered from scratch.

After variant compiler: agent submits 3-binding manifest, ZCP compiles to renamed laravel-showcase variant + drops unneeded service + adds storage, generates source rewrites, deploys with contract enforcement. Zero silent failure surface; recipe knowledge preserved; structurally cannot wire to wrong service.

This is the difference between "agent edits text and hopes" and "ZCP compiles binding contract and proves".
