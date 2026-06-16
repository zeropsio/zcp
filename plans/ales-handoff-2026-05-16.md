# Aleš handoff — recipe-tool scope items surfaced during 2026-05-14/16 matrix work

**Origin**: Karel's plan `plans/archive/path-to-everything-tested-2026-05-16.md` Sprints 1-4. Four items touch Aleš's scope (`internal/recipe/`, `internal/tools/workflow_recipe.go`, `internal/tools/workflow_checks_recipe.go`, `internal/content/workflows/recipe/`, `docs/zcprecipator*/`) per CLAUDE.local.md fence; documenting here so Karel can forward when convenient.

All four were ALREADY known territory; this doc captures the specific friction shape each surfaced + the smallest recommended fix.

---

## 1. `zerops_recipe` tool description — AUTHOR-only marker

**Surface**: `internal/recipe/handlers.go` tool registration (description string for the MCP tool).

**Issue**: Scenario #1 (kanban-laravel-minimal-dev-only) first attempt (pre-fix `7a990205`) saw the agent route the user's "udělej mi kanban na receptu zerops-laravel-minimal" prompt to `zerops_recipe` instead of `zerops_workflow workflow="bootstrap" route="recipe" slug=...`. The user-facing intent was "deploy from existing recipe"; `zerops_recipe` is recipe-AUTHORING (corpus-maintainer flow).

**My defense-in-depth**: Sprint 1 fix `7a990205` split the routing table row in `claude_shared.md` into AUTHOR vs USE rows + added Don't-column language. Confirmed working via re-run.

**Recommended canonical fix (Aleš)**: tighten the tool description itself so an agent reading just the schema (without claude_shared.md context) routes correctly.

Current (recall — I cannot edit):
```
"zcprecipator3 recipe engine"
```

Proposed:
```
"zcprecipator3 recipe-AUTHORING engine — for recipe-corpus maintainers
writing a new recipe to publish. For end-user DEPLOYMENT of an
existing recipe ('I want to deploy with the laravel-minimal recipe',
'use the nextjs-ssr recipe to scaffold my project'), use
zerops_workflow action=\"start\" workflow=\"bootstrap\"
route=\"recipe\" slug=\"...\" instead."
```

Scope: one-line description change. No behavior shift.

---

## 2. Recipe slug as customizable starting point

**Surface**: `internal/tools/workflow_recipe.go` + `internal/recipe/` recipe-corpus driver.

**Issue**: Surfaced indirectly across scenarios #1 + #2 retros: user names a recipe slug + adds task description ("build a Kanban using laravel-minimal recipe"). Current handling treats the slug as identity — recipe runs through its full author-phase pipeline. There's no knob for "scaffold from this recipe BUT add feature X on top" — user has to pivot to classic-route + hand-author.

**Workaround in matrix today**: Agent in #1 successfully built Kanban on top of laravel-minimal by using the recipe's seed app (via `buildFromGit`) + scaffolding Kanban UI on the mount post-deploy. But it took 14:09 because there's no "scaffold + iterate" handoff atom; agent figured it out from first principles.

**Recommended fix**: introduce a user-intent field on the recipe-route input that augments (not replaces) the recipe's default scope. Could be as small as an optional `customIntent: "build a Kanban on top of the recipe's seed app"` that the recipe handler appends to the develop-phase brief.

**Scope**: medium — handler change + atom additions for "recipe + custom intent" flow.

---

## 3. `emit-yaml shape=workspace` honor `bootstrapMode=dev`

**Surface**: `internal/recipe/` `emit-yaml` action when `shape=workspace`.

**Issue**: Currently `emit-yaml shape=workspace` always emits a dev+stage pair shape, ignoring the recipe's `bootstrapMode` field. Scenario #1 (dev-only) wanted single-runtime; scenario #2 (standard pair) wanted both. The current emitter forces standard pair always.

**Workaround**: the bootstrap route-menu has its own dev-only path via `bootstrapMode` in the plan shape; agents using `workflow=bootstrap route=recipe` get the right topology. The `emit-yaml shape=workspace` path is hit by recipe-author flow (Aleš scope), but the same plumbing could serve the bootstrap-recipe-import atom if it honored bootstrapMode.

**Recommended fix**: `emit-yaml shape=workspace` reads `bootstrapMode` from the recipe session state (set during the discover step) and emits dev-only / standard-pair / simple-mode accordingly. The output yaml's services list reshapes; no new schema fields.

**Scope**: medium — handler logic + per-mode yaml templates.

---

## 4. `zerops_import` accepts explicit `projectId` parameter

**Surface**: `internal/tools/import.go` (NOT in Aleš's strict scope, but adjacent — flag for the recipe team because launch-production uses a similar pattern).

**Issue**: `zerops_import` uses the ambient ZCP-container-bound project at register time. Multi-project workflows (launch-production targeting a new prod project) can't reuse it — they go through `ProjectAdminClient.CreateAndImportProject` (different code path). Recipe-author flow doesn't currently need cross-project import, but the symmetric pattern would simplify the existing-prod-project handling in launch-production.

**My adjacent fix**: Sprint 2 launch-production work (S2.2) didn't address this — `ExistingProjectID` is a `WorkflowInput` field consumed by launch-production handler, separate from `zerops_import`. Tier-3 scenario #10 (launch-to-existing-prod-project) tests the launch-production handler path; the zerops_import surface stays single-project-bound.

**Recommended fix**: add `projectId` to `zerops_import` input schema; when set, use that instead of the ambient binding. Validate scope-token match. This is `internal/tools/import.go` — peripheral to Aleš's recipe scope but worth flagging here so the team aligns.

**Scope**: medium — input schema change + per-call client binding.

---

## Cross-cutting observation — recipe matcher slug aliasing (Sprint 1 fix)

Not Aleš-scope but worth flagging for awareness: I landed a fix in `internal/knowledge/recipe_matcher.go::scoreRecipe` to accept `zerops-<slug>` as an exact-match alias for `<slug>` (commit `a83be8fc`). Users name recipes by their branded "zerops-laravel-minimal" form; the corpus stores them without the prefix. The matcher previously returned zero candidates for branded-named intents, surfacing route-menu without recipe options and falling agents to classic+hand-scaffold.

The fix touches `internal/knowledge/` (NOT `internal/recipe/`) so it's outside Aleš's scope, but the recipe-author flow could mirror it if `zerops_recipe action="start"` rejects branded slugs — should accept `slug="zerops-laravel-minimal"` as alias for `slug="laravel-minimal"`.

---

## Suggested ordering

If Aleš has bandwidth, item priority by user-facing impact observed in the matrix:

1. **#1 (tool description)** — one-line change, biggest defense-in-depth value. Eliminates the routing mistake at the schema layer.
2. **#3 (emit-yaml bootstrapMode honor)** — unblocks dev-only / standard-pair selection from the recipe-author flow.
3. **#2 (recipe + custom intent)** — bigger handler work; punt unless there's user signal for it.
4. **#4 (zerops_import projectId)** — peripheral; the existing launch-production path covers the actual cross-project import need.

Cross-cutting observation requires no Aleš work — already landed in `internal/knowledge/`.
