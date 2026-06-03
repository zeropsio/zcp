# Bootstrap provision-step guide dumps all three routes in one block

**Surfaced**: 2026-06-03, `flow-eval adopt-existing-standard-pair` (run
20260603-082235) during the P0c diverse-scenario pass. The agent's retro:
> "The detailed guide returned in the provision step response was the most
> confusing moment. It's a wall of text covering all three routes (recipe,
> classic, adopt) in one block, with the adopt-specific guidance buried at the
> bottom under a 'skip when no new wiring' note. The critical takeaway for adopt
> — that you can just complete provision immediately without running
> `zerops_discover includeEnvs=true` — required reading past several paragraphs
> about classic-route env var discovery."

**Why deferred**: this is the SAME bloat disease P0c is fixing, but on the
BOOTSTRAP side (the provision-step `buildGuide`, not the develop-active atom
pipeline). P0c is scoped to develop guidance; bootstrap-guide de-bloat is a
sibling effort. Not urgent — the agent recovered (read past the irrelevant
sections). Captured so the pattern isn't lost.

## Root question to answer first
Bootstrap atoms carry a `routes:` axis (`recipe`/`classic`/`adopt`), and
`Synthesize` filters on it — so a `route=adopt` provision response SHOULD only
fire adopt-route atoms. The retro says it didn't (all three routes appeared).
Two possibilities to check before fixing:
1. The provision guide isn't assembled purely via the route-axis-gated atom
   pipeline — `buildGuide`/`bootstrap_guide_assembly.go` may inline a
   non-route-gated block (e.g. a shared provision-rules atom with no `routes:`
   axis, or `formatRecipeImportYAMLForGuide`-style static text covering all paths).
2. A genuinely route-agnostic atom (`bootstrap-provision-rules`,
   `bootstrap-env-var-discovery`) fires for all routes and reads as
   "classic-route env discovery" to an adopt agent.

Likely (2): the env-var-discovery / provision-rules atoms are route-agnostic and
their classic-flavored prose dominates the adopt response. Fix = route-gate or
route-flavor those atoms (mirror the P0c develop approach: gate the learned-once /
wrong-route content; lead with the route-specific next step).

## Sketch
- Apply the P0c lens to bootstrap-active atoms: which fire for which route, and is
  the per-route response leading with that route's actual next action?
- For `route=adopt` specifically: the response should open with "adopt = complete
  provision immediately; skip env discovery unless adding new cross-service wiring",
  not bury it under classic-route env-discovery prose.

## Refs
- Retro: `eval/behavioral/runs/20260603-082235/adopt-existing-standard-pair/self-review.md`
- `internal/workflow/bootstrap_guide_assembly.go::buildGuide`, the `bootstrap-*`
  atoms with/without a `routes:` axis (`grep -L routes internal/content/atoms/bootstrap-*.md`).
- Sibling: this P0c work (`plans/bootstrap-restore-four-goals-2026-06-02.md` §P0c)
  is the develop-side template for the same de-bloat.
