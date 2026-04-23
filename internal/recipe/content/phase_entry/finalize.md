# Finalize phase — writer dispatch, stitch payload, emit all tiers

The scaffold + feature phases built a working deploy at tier 0. Finalize
turns it into the 6-tier publishable artifact.

## Steps

1. **Writer dispatch**: `zerops_recipe action=build-brief slug=<slug>
   briefKind=writer`. The brief walks the surface registry, filters
   facts-log by surface hint, inlines example banks, returns ~8–10 KB
   of content-authoring guidance. Dispatch via `Agent` with the brief
   body verbatim. Description: `writer-<slug>`.

2. **Writer returns a structured completion payload** (see the
   `Completion payload` section of its brief). Do NOT ask the writer to
   write files directly — writer-owned paths are locked at the engine
   boundary.

3. **Stitch**: `zerops_recipe action=stitch-content slug=<slug>
   payload=<writer's JSON>`. The engine writes the payload into the
   recipe file tree at canonical paths. This also regenerates every
   tier's `import.yaml` using the writer-authored `env_import_comments`.

4. **Run gates**: `zerops_recipe action=complete-phase slug=<slug>`.
   The gate set checks env-imports presence, citation timestamps,
   required fact fields, completion payload schema, and main-agent-
   rewrote-writer-path violations. Fix any CRIT violation and retry
   the writer dispatch (fix-dispatch pattern).

5. **Export** (optional): the agent may copy the recipe tree to a
   separate location, or hand control back to the user to run
   `zcp sync recipe publish`. Engine does not auto-export.

## If writer returns a broken payload

One fix-dispatch is permitted: the main agent diffs the broken output
against the brief's completion-payload schema, composes a targeted
correction prompt, and re-dispatches the writer. Do not hand-edit the
writer's output files — that trips the main-agent-rewrote-writer-path
gate and invalidates the recipe.

## Close

After `complete-phase` passes on finalize, the recipe artifact is
complete. Report the output path + the per-tier file count to the user.
