# Route options: reorder adopt below recipes when both apply

**Status:** deferred — no observed friction post-Phase-2.1/2.2.
**Source:** Codex review of flow-eval suite `20260503-144814` (2026-05-03).

## What

In `internal/workflow/route.go::BuildBootstrapRouteOptions`, the
`routeOptions` order is hardcoded `resume → adopt → recipe → classic` and
pinned by `route_test.go:140`. Codex suggested: when adoption is implicit
(agent didn't explicitly request it) AND recipes match with confidence ≥
0.85, push adopt below recipes.

## Why deferred

The flow-eval friction this would address — adopt ranked above recipes when
recipes match the user's stated stack — was specifically `adopt zcp` (the
ZCP control-plane container itself) ranking #1. Phase 2.1 (self-host
filter) excludes `zcp` from adopt candidates, so the observed friction is
resolved without reordering. Phase 2.2 (stack-mismatch filter) drops
recipes that contradict user-mentioned dependencies — covering the
remaining "wrong DB" trap.

After 2.1 + 2.2, the case where reorder matters is: project has legitimate
non-ZCP unmanaged user services, AND recipes match the user's intent
strongly. None of the 6 eval scenarios exercises this combination. Adding
reorder logic without observable evidence violates "don't add features
beyond what the task requires" (CLAUDE.local.md global instruction).

## Trigger to promote

Promote if a future eval surfaces friction where adopt for a legitimate
non-ZCP service ranks above a strong recipe match and the agent picks
adopt unwisely. Or if a user reports "I had to manually skip adopt because
the recipe was clearly the right choice".

## Sketch when triggered

```go
// In BuildBootstrapRouteOptions, after building keptRecipes:
// - When len(keptRecipes) > 0 AND adoptOpt is implicit (always true in
//   discovery mode), push adopt below keptRecipes.
// - Update route_test.go:140 with a new pin name reflecting the rule.
```

Spec touch: `docs/spec-workflows.md` route ordering section gets a new
clause about conditional adopt placement.

## Risks

- Breaks the existing pin (`adopt before recipe`). The pin's reason
  ("don't bootstrap over user's services unnoticed") is still real for
  users who actually want adopt — they'd see classic + recipes first and
  might not scroll. Counter-argument: the per-option `Why` blurb names
  the existing service hostname, so it's still discoverable.
- Edge case: project has BOTH legitimate adopt candidates and no recipe
  match. Order then becomes `adopt → classic` — same as today. So the
  reorder only matters when recipes exist.
