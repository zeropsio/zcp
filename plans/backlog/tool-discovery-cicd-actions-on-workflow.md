# Tool discovery: CI/CD actions hide inside `zerops_workflow`, not obvious from deferred-tool list

**Surfaced:** 2026-05-04, eval suite `20260504-065807`
`delivery-git-push-actions-setup` retro.

**Why deferred:** discovery / packaging concern. Real but isolated to
agents starting cold without prior knowledge of the workflow tool's
action surface.

## What

The three actions an agent needs for CI/CD setup —
`close-mode`, `git-push-setup`, `build-integration` — live as `action=`
values on the `zerops_workflow` tool. The deferred-tool list shown to a
fresh agent only carries the tool NAME, not its actions. So an agent
looking for "how do I wire CI/CD on Zerops" sees no obvious match in
the tool list and goes spelunking through `zerops_knowledge` first
(which dispatches them away from the right tool).

Agent quote: "I initially went down the wrong path looking up CI/CD
via `zerops_knowledge` thinking I'd need to hand-author a workflow YAML
and paste in instructions — when in fact the platform generates the
exact file for you. Future agent: skip the knowledge spelunking, load
the workflow tool schema first, and read the action descriptions on it
carefully."

The friction is purely upstream of `zerops_workflow` — once the agent
loads its schema, the action descriptions are clear.

## Trigger to promote

Promote if more eval scenarios show agents detouring through
`zerops_knowledge` for tasks that have direct workflow-tool actions.
Cheap fix; could ship as a description tweak any time.

## Sketch

Two options, not mutually exclusive:

1. **Cross-link from `zerops_knowledge` queries.** When the query
   matches "ci/cd", "github actions", "git push setup", "deploy
   pipeline", surface a hint:
   "→ for CI/CD setup, see `zerops_workflow` action=git-push-setup /
   action=build-integration. Knowledge briefings cover concepts only."

2. **Improve the `zerops_workflow` tool name/description in the
   deferred-tool list.** Currently the description mentions actions in
   prose; consider leading with "wires deploy lifecycle, CI/CD setup,
   close-mode" so a keyword-matching agent finds it on first scan.

Option (1) covers the misroute; (2) is a pure improvement.

## Risks

- Adding cross-references in knowledge responses pulls
  recipe-engine-adjacent (Aleš's scope). The knowledge tool itself
  lives in `internal/tools/knowledge.go`; the data lives in `internal/knowledge/`.
  A simple "see also" hint in the knowledge tool wrapper is workflow-
  team-only, but adding new knowledge content would cross the
  boundary — worth checking before scope creep.
