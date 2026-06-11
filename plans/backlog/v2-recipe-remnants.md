# Retire the v2 recipe remnants left in core

**Surfaced by:** the authoring-boundary ship (2026-06-11,
`docs/spec-authoring-boundary.md`). The boundary moved everything
recipe-AUTHORING into `internal/authoring/`, but the dispatch-blocked v2
recipe machinery still lives in core and is the only reason "Aleš's scope"
is a path-prefix rule PLUS an exception list instead of a pure prefix.

**Inventory (all dispatch-dead or near-dead):**
- `internal/tools/workflow_recipe.go` + `workflow_checks_recipe.go` — v2
  recipe sub-mode handlers; `workflow="recipe"` is hard-blocked in
  `handleWorkflowAction`, so only the non-recipe-reachable remnants (e.g.
  `generate-finalize` on workflow types) keep them compiled.
- `internal/content/workflows/recipe.md` + `internal/content/workflows/recipe/`
  — v2 recipe workflow content; never renders through the blocked dispatch.
  (recipe.md still names `zerops_recipe` — harmless while unreachable,
  dies with the cleanup.)
- `internal/ops/checks/` workflow-import exemption (`ops-checks-legacy`
  depguard rule + architecture-test rule) — the rule comments already say
  "Once recipe v2 is deleted this exception goes away."
- `authoring/publish/session_load.go`'s `workflow.RecipeState` close-gate —
  reads v2 sessions for `zcp sync recipe export`; v3 uses the
  `.refinement-closed` marker. Is the v2 gate path dead in practice? If yes,
  deleting it shrinks the L3 identifier allowlist from 4 entries to 1
  (`CanonicalEnvFolders`).

**Why backlogged, not done now:** deletion needs Aleš's confirmation that no
v2-shaped run he cares about still exists (export close-gate reads real
session files), and the `generate-finalize` reachability question needs a
proper trace. Out of scope of the boundary ship.

**Trigger to promote:** Aleš confirms v3-only, or the next time anyone
touches `ops/checks/` / the v2 handlers for another reason.

**Payoff:** pure-prefix ownership rule, two depguard/architecture exceptions
deleted, L3 allowlist shrinks, `internal/content/workflows/recipe*` leaves
core content.
