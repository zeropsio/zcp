---
surface: ig-item
verdict: fail
reason: wrong-surface
title: "dev-setup initCommands taught as IG item (v38 apidev CRIT #6)"
---

> ### Add `initCommands` to the dev setup block
>
> When you port your own API code to Zerops, add `setup: dev` to
> `zerops.yaml` with an `initCommands` block that runs `npm install` and
> `npm run migration:run`. This is what the dev slot needs to reach a
> working state before the agent drives it.

**Why this fails the IG-item test.**
IG items teach PORTERS bringing their own code. A porter adds a `setup:
prod` or equivalent block for the deploy they care about — the dev slot
(`setup: dev`) is a recipe-specific scaffold decision that the porter
may or may not replicate. Teaching "dev setup needs own initCommands"
as a porter guideline reroutes a scaffold-decision to the porter
surface.

**Correct routing**: this belongs in the recipe's per-codebase
`CLAUDE.md` (operational guide for the specific repo). The PORTER IG
item should be about `setup: prod`'s initCommands for migrations —
`zsc execOnce php artisan migrate` (or framework equivalent) with the
concurrent-replica-safety rationale. Spec §7 classification:
scaffold-decision → CLAUDE.md, not IG.
