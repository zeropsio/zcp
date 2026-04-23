# internal/workflow — zcprecipator2 (FROZEN)

This package is **zcprecipator2**, frozen at tag `v8.113.0-archive`.

**Do not add features here.** New recipe work lives at [internal/recipe/](../recipe/)
(zcprecipator3). See [docs/zcprecipator3/plan.md](../../docs/zcprecipator3/plan.md)
for the rewrite rationale, the stays-list v3 inherits from v2, and the
strangler-fig transition plan.

## What's frozen

- All `internal/workflow/recipe*.go`
- All `internal/content/workflows/recipe*` (the `recipe.md` monolith + atom tree)

The `.githooks/commit-msg` hook refuses commits that touch those paths. A
genuine v2 hotfix during the v3 transition window can pass the
`[workflow-v2-hotfix]` trailer in the commit message; everything else
should land in `internal/recipe/`.

## What stays alive in this package

Everything that is **not** recipe workflow — bootstrap, develop, build-plan,
compute-envelope, atom machinery, work session, knowledge/briefing glue —
remains the operational substrate for non-recipe workflows and for the shared
envelope pipeline. v3's `internal/recipe/` reuses this substrate verbatim; it
does not replace it.

## Deletion trigger

Per [plan §14 decision 5](../../docs/zcprecipator3/plan.md), `recipe*.go` +
`recipe*` content deletes as soon as one showcase-via-v3 ships with the
quality bars green. Until then, both trees coexist and v3 runs behind the
`--v3` flag.
