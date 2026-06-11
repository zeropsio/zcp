# Retire the v2 recipe remnants left in core

**Surfaced by:** the authoring-boundary ship (2026-06-11,
`docs/spec-authoring-boundary.md`). The boundary moved everything
recipe-AUTHORING into `internal/authoring/`, but the dispatch-blocked v2
recipe machinery still lives in core and is the only reason "Aleš's scope"
is a path-prefix rule PLUS an exception list instead of a pure prefix.

**Inventory:**
- `internal/tools/workflow_recipe.go` + `workflow_checks_recipe.go` — v2
  recipe sub-mode handlers; `workflow="recipe"` is hard-blocked in
  `handleWorkflowAction`, so only the non-recipe-reachable remnants (e.g.
  `generate-finalize` on workflow types) keep them compiled.
- `internal/tools/guidance.go` (`zerops_guidance`) — v2 recipe-authoring
  topic guidance; the ONLY live render path for the recipe workflow corpus
  (nil-plan topic resolution, no session required). Now registered behind
  the ZCP_AUTHORING gate (review finding, 2026-06-11); dies with v2.
- `internal/content/workflows/recipe.md` + `internal/content/workflows/recipe/`
  — v2 recipe workflow content; unreachable through the blocked dispatch,
  still rendered by gate-on `zerops_guidance`. (recipe.md still names
  `zerops_recipe` — maintainer-only surface now, dies with the cleanup.)
- `internal/workflow/engine.go` boot-time auto-claim can adopt a stale
  dead-PID v2 recipe session regardless of the gate (`NewEngine`
  single-session claim) — practically unreachable for end users (v2 recipe
  sessions can't be started since the block), but the claim path should
  exclude `Workflow=="recipe"` or die with v2.
- `internal/server/instructions.go` recipe-session hint — keys on a live
  v2 REGISTRY session (not the gated v3 store); same practical-dead status.
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
