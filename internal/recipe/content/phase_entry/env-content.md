# Env-content phase — single sub-agent dispatches root + per-tier surfaces

After codebase-content completes, one `env-content` sub-agent runs and
authors:

- `root/intro` (Surface 1)
- `env/<N>/intro` for N in 0..5 (Surface 2)
- `env/<N>/import-comments/project` and `env/<N>/import-comments/<host>`
  for every service block (Surface 3, ~54 fragments across 6 tiers)

The brief carries:

- Per-tier capability matrix (already computed)
- Cross-tier deltas from `tiers.go::Diff`
- Engine-emitted `tier_decision` facts (§5.3) — one per cross-tier
  whole-tier delta + one per per-service mode change. The agent
  extends the auto-derived `TierContext` slot via `fill-fact-slot`
  when the engine prose is too thin.
- Pointer to spec-content-surfaces.md for surface contracts
- Parent-recipe pointer block (when present) — read parent's per-tier
  intros and cross-reference instead of re-author

The brief is pointer-based — the sub-agent reads the spec and parent
content on demand via `Read` rather than receiving them embedded.

## Complete-phase gate

`root/intro` recorded; `env/<N>/intro` for every tier; per-service-block
import-comments for every codebase + managed service across every tier.
EnvGates() validators run.
