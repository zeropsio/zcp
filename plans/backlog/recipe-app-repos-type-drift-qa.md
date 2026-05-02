# Recipe app repos lack systemic type/dependency QA

**Surfaced**: 2026-05-02 — agent session feedback. Brand-new ZCP session bootstrapped `nodejs-hello-world` recipe, agent edited `req.params.id` in a normal Express handler. `ts-node` compile failed with:

```
src/app.ts(68,23): error TS2345: Argument of type 'string | string[]' is not assignable to parameter of type 'string'.
```

Cause: `github.com/zerops-recipe-apps/nodejs-hello-world-app/package.json` declares `"express": "^4.21.2"` + `"@types/express": "^5.0.0"` — version-incompatible. Express 5's `@types/express` widened `req.params` to `string | string[]` (ParsedQs strict); Express 4 runtime returns plain `string`. Either bump express to 5 OR pin `@types/express` to `^4`. Agent worked around with `parseInt(String(req.params.id), 10)` to satisfy v5 typings.

Recipe is the first-touch surface for every recipe-route user, so this drift hits everyone editing the placeholder, not just the agent who reported it. Likely the same drift exists in some subset of the other 35+ `zerops-recipe-apps/*` repos — none of them have a systemic gate that runs `npm install && npm run build` (or the runtime equivalent: `cargo build`, `mvn compile`, `dotnet build`, etc.) on every change.

**Why deferred**: lives outside the zcp monorepo. Per-repo manual fix is fast (~1 h sweep across all recipe app repos) but non-systemic — drift returns the moment a dependency version moves. Systemic CI gate is the right structural fix but requires deciding ownership: does each `zerops-recipe-apps/*` repo own its own CI workflow, or does zcp monorepo host a cross-repo verification cron? Either choice ripples through release tooling.

**Trigger to promote**:
- Second observed type/dependency drift in any recipe app repo (one is a curiosity; two is a class).
- Decision is taken on recipe app repo ownership / CI shape (e.g. as part of a recipe content quality push).
- A user-visible recipe failure that isn't trivially worked around like this one was.

## Sketch

Two complementary tracks:

1. **Per-repo manual sweep — fast, targeted.** For each `zerops-recipe-apps/*` clone:
   - Run the recipe's natural build command (e.g. `npm run build`, `tsc --noEmit`, `cargo check`).
   - When it fails, pin or bump the offending dep so the build is green against the runtime version declared in the matching `internal/knowledge/recipes/<slug>.import.yml`.
   - Commit + tag.
   - Repeat across the 35+ recipes.

2. **Systemic CI gate — durable, structural.** Two shapes worth comparing:
   - **Per-repo workflow.** Each `zerops-recipe-apps/*` repo gets a `.github/workflows/build.yml` that runs the matching build command on push + PR. Owners stay distributed. Cost: 35+ workflow files to author + maintain.
   - **Cross-repo cron in zcp monorepo.** zcp's CI clones every recipe app repo nightly (or on `internal/knowledge/recipes/*` changes), runs each one's build, and fails the zcp pipeline if any breaks. Cost: one workflow + lookup table; tight feedback loop on knowledge edits but slower signal on app-repo edits.

Cross-repo cron is structurally cleaner — single source of truth — but per-repo workflow gives faster signal to the recipe maintainer. Hybrid (per-repo for fast feedback + monorepo cron for cross-repo invariants) is probably the right shape.

## Risks

- Recipe app repos may not all build standalone (some assume Zerops env vars at compile time, e.g. `${db_*}` rewrites). The CI gate may need a placeholder env injection layer.
- Bumping versions in app repos is a per-recipe judgment call — some pin Express 4 deliberately for compat reasons, even when the typings drifted. Don't blanket-bump; investigate each.
- Mass changes across 35+ external repos means 35+ open PRs / branch dance / GitHub Actions runs. Not throwaway work.

## Refs

- Originating session: agent session log 2026-05-02 (see `req.params.id` ts-node failure)
- Recipe app repos org: `github.com/zerops-recipe-apps/*`
- Recipe knowledge index: `internal/knowledge/recipes/*.{md,import.yml}` — one entry per recipe, points at the matching app repo
- Companion item from same session feedback: develop-first-deploy atoms aren't route-aware (see deferred bod 2 — separate plan when promoted)
