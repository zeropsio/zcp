# Run-29 prep — `STAGE_*_URL` → `*_URL` convention rename + provision.md URL-constant step

Authored 2026-05-06 between v9.69.1 and v9.69.2 dispatch. Two changes to
ship before run-29 redispatch: (1) clean naming for the project-scope
URL constants so tier 4-5 production yamls don't carry `STAGE_*` keys
that semantically don't belong there, (2) discrete provision.md step so
the agent reliably sets the constants instead of self-correcting at
scaffold (or skipping entirely, as observed mid-flight in the in-progress
run-29).

Both changes follow system.md §4 TEACH-side principles. No new validator
regex shipping as Blocking; the rename is a positive convention shift,
the provision step is a positive sequence rule.

## Framing — why both at once

- The rename without the provision step still leaves the agent running
  scaffold-time discovery → coin-flip URL-constants creation.
- The provision step without the rename ships tier 4-5 with `STAGE_*`
  keys that are functionally correct but semantically misleading.
- Doing them together avoids two consecutive cap-bumping releases
  (v9.69.2, v9.69.3) chasing the same conceptual gap.

## Naming convention

Old → new:

| Old key | New key | Tier 0-1 meaning | Tier 2-5 meaning |
|---|---|---|---|
| `STAGE_API_URL` | `API_URL` | URL of the **stage** api (`apistage-...-3000.prg1.zerops.app`) | URL of the **only** api (`api-...-3000.prg1.zerops.app`) |
| `STAGE_FRONTEND_URL` | `FRONTEND_URL` | URL of the **stage** frontend (`appstage-...prg1.zerops.app`) | URL of the **only** frontend (`app-...prg1.zerops.app`) |
| `DEV_API_URL` | unchanged | URL of the **dev** api (`apidev-...-3000.prg1.zerops.app`) | (engine drops at tier 2-5) |
| `DEV_FRONTEND_URL` | unchanged | URL of the **dev** frontend (`appdev-...-5173.prg1.zerops.app`) | (engine drops at tier 2-5) |

Why this shape:

- Tier 0-1 (dev-pair tiers): 4 keys. `API_URL` = production-side api at this
  tier (stage). `DEV_API_URL` = dev-side api at this tier. Same for frontend.
- Tier 2-5 (single-slot tiers): 2 keys. `API_URL` + `FRONTEND_URL` only.
  No dev/stage distinction at single-slot.
- The codebase yaml's `appdev/zerops.yaml prod setup` references `${API_URL}`;
  resolves correctly at every tier (workspace + tier 0-1: stage api;
  tier 2-5: the single api).
- The codebase yaml's `appdev/zerops.yaml dev setup` references `${DEV_API_URL}`;
  resolves correctly at workspace + tier 0-1; the dev setup never deploys
  at tier 2-5 (no dev runtime), so the unresolved-token-at-tier-2-5 case
  doesn't arise.

This matches how `apidev/zerops.yaml run.envVariables` will reference
`CORS_ORIGINS: ${FRONTEND_URL},${DEV_FRONTEND_URL}` at workspace and tier
0-1 (allow-list both); tier 2-5 emit drops `${DEV_FRONTEND_URL}` as part
of single-slot rewrite, leaving `CORS_ORIGINS: ${FRONTEND_URL}`.

## Engine behavior — already correct, no rewrite changes needed

[yaml_emitter.go::rewriteURLsForSingleSlot](internal/recipe/yaml_emitter.go) already:

- Drops keys with `DEV_` prefix at tier 2-5.
- Rewrites slot-named hostnames (`apistage-` → `api-`, `appdev-` → `app-`,
  etc.) in URL VALUES.

Both behaviors stay correct under the new naming. No engine code change.

## Provision.md NEW step

Insert as new step 4 (between current step 3 `Set project-level shared secrets`
and current step 4 `Mount dev codebases`); renumber subsequent steps.

```md
4. **Set cross-service URL constants** (when the recipe has a frontend
   that bakes URLs at build time, OR an API that needs CORS allow-list):

   Project-scope URL constants resolve at provision time (before any
   peer service deploys), so the SPA's build-time `VITE_API_URL` bake
   and the api's `CORS_ORIGINS` runtime allow-list both have real values
   from the very first deploy. Cross-service refs (`${api_zeropsSubdomain}`)
   only mint after the peer's first deploy — too late for a
   fresh-project build.

   Construction template (4 keys for dev-pair tiers; the engine drops
   `DEV_*` at single-slot tier emit):

   ```
   zerops_env project=true action=set variables=[
     "API_URL=https://<apistage-host>-${zeropsSubdomainHost}-<api-port>.prg1.zerops.app",
     "FRONTEND_URL=https://<appstage-host>-${zeropsSubdomainHost}<-port-or-empty>.prg1.zerops.app",
     "DEV_API_URL=https://<apidev-host>-${zeropsSubdomainHost}-<api-port>.prg1.zerops.app",
     "DEV_FRONTEND_URL=https://<appdev-host>-${zeropsSubdomainHost}-<dev-port>.prg1.zerops.app"
   ]
   ```

   Port mapping by role:
   - NestJS / Express / Fastify api → `-3000`
   - Vite dev server → `-5173`
   - `base: static` (production frontend) → no port suffix
   - Worker → no URL constant (no public surface)

   Then record into the plan so the engine emits them in tier yamls'
   `project.envVariables` block at finalize:

   ```
   zerops_recipe action=update-plan slug=<slug> plan='{
     "projectEnvVars": {
       "0": {"API_URL": "...", "FRONTEND_URL": "...", "DEV_API_URL": "...", "DEV_FRONTEND_URL": "..."},
       "1": {"API_URL": "...", "FRONTEND_URL": "...", "DEV_API_URL": "...", "DEV_FRONTEND_URL": "..."}
     }
   }'
   ```

   Two channels (live workspace + plan record) are required:
   - `zerops_env action=set` populates the workspace project's live env
     so scaffold sub-agents bake real URLs into bundles immediately.
   - `update-plan projectEnvVars` records the same constants for tier
     0/1 emit at finalize. The engine's `rewriteURLsForSingleSlot`
     handles tier 2-5 reshape automatically (drops `DEV_*`, rewrites
     slot-named hostnames to bare).

   Workerless / api-only recipes skip this step. Static-only recipes
   that don't have an API skip too.

   ### Tier-conditional meaning of the keys

   - **Workspace + tier 0-1 (dev-pair tiers)**: `API_URL` resolves to
     the **stage-side / production-setup** API (`apistage-...`); 
     `DEV_API_URL` resolves to the **dev-setup** API (`apidev-...`).
     Codebase yaml's `appdev/zerops.yaml prod setup` references
     `${API_URL}`; the same file's `dev setup` references `${DEV_API_URL}`.
   - **Tier 2-5 (single-slot tiers)**: `API_URL` is **the only api**
     (no dev/stage distinction at single-slot). The engine's
     `rewriteURLsForSingleSlot` drops `DEV_API_URL` / `DEV_FRONTEND_URL`
     keys at emit and rewrites the slot-named hostname (`apistage-` →
     `api-`, `appstage-` → `app-`) in the value.

   Naming logic: the key `API_URL` is the project's API URL constant
   at this tier. At dev-pair tiers the value happens to be the
   stage-side URL because dev has its own dedicated key; at single-slot
   tiers the value is the only available URL.
```

## Files that change

### Atoms (TEACH side)

| File | Change |
|---|---|
| [phase_entry/provision.md](internal/recipe/content/phase_entry/provision.md) | Add NEW step 4 (URL constants); renumber 4→5, 5→6, 6→7, 7→8 |
| [principles/cross-service-urls.md](internal/recipe/content/principles/cross-service-urls.md) | Rename `STAGE_API_URL` → `API_URL`, `STAGE_FRONTEND_URL` → `FRONTEND_URL` throughout (worked examples + bake-trap teaching). The "STAGE_" was a name; the role is "the project's api URL" / "the project's frontend URL" — keep the friendly-authority voice |
| [briefs/scaffold/platform_principles.md](internal/recipe/content/briefs/scaffold/platform_principles.md) | Rename in worked example at line 64 (`STAGE_API_URL: ${STAGE_API_URL}` shadow trap example becomes `API_URL: ${API_URL}`) |
| [briefs/env-content/per_tier_authoring.md](internal/recipe/content/briefs/env-content/per_tier_authoring.md) | Rename in worked examples; if any examples mix tier-0/1 vs tier-2/5 emit shapes, reconcile per the new naming |
| [briefs/refinement/synthesis_workflow.md](internal/recipe/content/briefs/refinement/synthesis_workflow.md) | Rename in any worked examples |
| [briefs/scaffold/spa_static_runtime.md](internal/recipe/content/briefs/scaffold/spa_static_runtime.md) | Rename in build-time-bake worked example |

### Engine code

| File | Change |
|---|---|
| [briefs_content_phase.go](internal/recipe/briefs_content_phase.go) | Update any literal-string references to the old keys |
| [yaml_emitter.go](internal/recipe/yaml_emitter.go) | NO CHANGE — `rewriteURLsForSingleSlot` keys on `DEV_` prefix; STAGE_* → *_URL rename doesn't affect this |
| [ops/checks/env_self_shadow.go](internal/ops/checks/env_self_shadow.go) | Update any literal-string references |

### Tests

| File | Change |
|---|---|
| [briefs_content_phase_test.go](internal/recipe/briefs_content_phase_test.go) | Update assertions referencing `STAGE_API_URL`/`STAGE_FRONTEND_URL` |
| [yaml_emitter_test.go](internal/recipe/yaml_emitter_test.go) | Update fixture data |
| [content_lint_test.go](internal/recipe/content_lint_test.go) | Update if it scans atom files for the old keys |
| [ops/checks/env_self_shadow_test.go](internal/ops/checks/env_self_shadow_test.go) | Update |
| [ops/env_shadow_test.go](internal/ops/env_shadow_test.go) | Update |
| [workflow/engine_recipe_project_env_test.go](internal/workflow/engine_recipe_project_env_test.go) | Update |
| [workflow/recipe_templates_dualruntime_test.go](internal/workflow/recipe_templates_dualruntime_test.go) | Update |
| [workflow/recipe_templates_project_env_test.go](internal/workflow/recipe_templates_project_env_test.go) | Update |

### Knowledge corpus + workflow docs + spec docs

| File | Change |
|---|---|
| [internal/knowledge/guides/environment-variables.md](internal/knowledge/guides/environment-variables.md) | Rename in any worked example |
| [internal/content/workflows/recipe.md](internal/content/workflows/recipe.md) | Rename |
| [internal/content/workflows/recipe/phases/deploy/feature-sweep-stage.md](internal/content/workflows/recipe/phases/deploy/feature-sweep-stage.md) | Rename |
| [internal/content/workflows/recipe/phases/finalize/entry.md](internal/content/workflows/recipe/phases/finalize/entry.md) | Rename |
| [internal/content/workflows/recipe/phases/finalize/project-env-vars.md](internal/content/workflows/recipe/phases/finalize/project-env-vars.md) | Rename |
| [internal/content/workflows/recipe/phases/generate/zerops-yaml/dual-runtime-consumption.md](internal/content/workflows/recipe/phases/generate/zerops-yaml/dual-runtime-consumption.md) | Rename |
| [internal/content/workflows/recipe/phases/provision/import-yaml/dual-runtime.md](internal/content/workflows/recipe/phases/provision/import-yaml/dual-runtime.md) | Rename |
| [docs/zcprecipator3/system.md](docs/zcprecipator3/system.md) | Rename in run-23/22 retrospective entries that reference these keys |
| [docs/zcprecipator3/pipeline-actor-map.md](docs/zcprecipator3/pipeline-actor-map.md) | Rename |

### NOT changed (freeze list)

These paths describe the engine's PAST state at specific points in time;
renaming them would falsify the historical record:

- `docs/zcprecipator3/runs/*/` — every run directory (7, 19, 20, 22-28).
- `docs/zcprecipator3/simulations/*/` — replay artifacts.
- `docs/zcprecipator3/plans/*` other than this plan — prior fix plans
  (run-20-prep.md, run-28-fixes-2026-05-06.md, etc.) describe the engine
  state at the point they were authored.
- `docs/zcprecipator3/archive/`, `docs/zcprecipator2/`, `docs/zrecipator-archive/` —
  superseded engine versions.
- `docs/zcprecipator3/system.md` retrospective verdict-table entries
  (lines 497-526) — each row describes "what we shipped in run-K
  using the run-K naming." Renaming would make the record false.
- `docs/zcprecipator3/pipeline-actor-map.md` retrospective references
  in the "Historical references" sections — same reason.
- `internal/recipe/testdata/` fixtures simulating prior-run inputs.

The system.md `### What "wrong side" means concretely` table (line 446
onward) walks each historical artifact through the TEACH/DISCOVER lens;
those rows reference the names as they were at the time the lesson
landed. Don't touch.

The CURRENT teaching atoms (everything under `internal/recipe/content/`
that's actively loaded into briefs at dispatch time) get renamed. The
historical record stays.

## Migration concerns (codex pass 1 finding)

`Plan.ProjectEnvVars` is emitted as supplied — no automatic old-key
translation layer. Any in-progress dogfood plan resumed from before this
change would carry old `STAGE_API_URL`/`STAGE_FRONTEND_URL` keys; the
agent's next dispatch into the new atoms would see the new naming and
disagree with the in-progress plan.

Mitigation: this is an intra-release migration only. Released v9.69.x
is on the old naming; released v9.70.0 uses the new naming. Don't resume
a v9.69.x plan against a v9.70.0 binary. New runs only after v9.70.0
ships. Document this constraint in the v9.70.0 release commit message.

## Implementation order

Single commit. The rename is mechanical (two key-name changes everywhere
they appear); provision.md step is a content addition. Both land together
so the test suite at any commit is internally consistent.

1. Author tests pinning the new convention (RED).
2. Run mechanical rename across all 24 source files (`sed` won't work
   cleanly because of context-sensitive references in atoms — manual
   walk per file).
3. Author provision.md step 4 + atom test pinning the section.
4. Make lint-fast + go test internal/recipe + go test ./... -race.

## Verification predicates

A green commit reads as ready iff:

1. `grep -r "STAGE_API_URL\|STAGE_FRONTEND_URL" /Users/fxck/www/zcp/internal/ /Users/fxck/www/zcp/docs/spec-*.md /Users/fxck/www/zcp/docs/zcprecipator3/system.md /Users/fxck/www/zcp/docs/zcprecipator3/pipeline-actor-map.md` — 0 hits.
2. `grep -rn "API_URL\b" internal/recipe/content/phase_entry/provision.md` — non-empty (the new step is present).
3. `make lint-local` — 0 issues.
4. `go test ./... -race -count=1` — all packages green.

Plus a post-rename sanity check:

5. The engine emits `API_URL`/`FRONTEND_URL` in workspace + tier 0/1 yamls
   and drops `DEV_*` at tier 2-5 (existing yaml_emitter behavior; updated
   tests pin the new key names).

## What this fix does NOT do

- Does NOT change `rewriteURLsForSingleSlot` semantics. The DEV_* drop
  + slot-name rewrite stay byte-identical.
- Does NOT add validators that ban `STAGE_*` keys at provision time.
  Old `STAGE_*` shape no longer appears anywhere in the engine corpus
  after the rename; an agent dispatched against the new atoms will
  simply not know the old name.
- Does NOT introduce a per-tier codebase yaml token rewriter (Option C
  from the analysis was the over-engineered path).
- Does NOT rename `DEV_*` keys. Their semantics are clean already.

## Why this is a system.md §4 TEACH-side fix, not catalog-drift

The change is positive shape (engine emits `API_URL`/`FRONTEND_URL` per
tier; brief teaches setting them; rewriter drops `DEV_*` at single-slot).
No regex, no ban-list, no string catalog. The naming convention itself
is the rule.

The provision.md step is positive sequence: enumerate the construction
template alongside APP_SECRET. Both APP_SECRET and the URL constants
are project-scope env vars set at provision time; the brief should treat
them with the same level of explicitness.
