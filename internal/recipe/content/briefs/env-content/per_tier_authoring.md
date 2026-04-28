# Per-tier authoring workflow

You author env-level surfaces (root + per-tier intros + import-comments)
across 6 tiers (0..5). The brief carries:

- Per-tier capability matrix (already computed)
- Cross-tier deltas from `tiers.go::Diff`
- Engine-emitted `tier_decision` facts (one per cross-tier whole-tier
  delta + one per per-service mode change)
- The plan snapshot (codebases + services)
- Parent-recipe pointer (when present)

## Workflow

1. **Read the spec** — `docs/spec-content-surfaces.md` Surface 1 (root
   intro), Surface 2 (env intro), Surface 3 (env import-comments).
2. **Author root/intro** — 1-sentence framing of the recipe. ≤ 500
   chars, no markdown headings.
3. **For each tier 0..5**:
   - Author `env/<N>/intro` (1-2 sentences naming who this tier is for
     + the cost/availability tradeoff). ≤ 350 chars, no `## ` headings,
     no `<!-- #ZEROPS_EXTRACT_*` tokens (engine stamps those at stitch).
   - For each service block in the tier import.yaml, author
     `env/<N>/import-comments/<host>` (≤ 8 lines explaining the tier's
     mode/scaling choice for that service). Use the engine-emitted
     `tier_decision` facts as the authoritative rationale; extend
     `TierContext` via `fill-fact-slot` if the auto-derived prose is
     insufficient.
   - Author `env/<N>/import-comments/project` (≤ 8 lines, project-block
     comment).
4. **Cross-reference parent** when present — read parent's per-tier
   intros and dedup before authoring.

## Voice

Friendly authority — name the audience, the cost ceiling, the
availability tradeoff. Avoid "see Zerops docs"; the comment IS the
explanation.

## Self-validate

`zerops_recipe action=complete-phase phase=env-content` runs EnvGates()
validators. Fix violations via `record-fragment mode=replace` until
the gate passes.
