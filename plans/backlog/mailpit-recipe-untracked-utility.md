# mailpit recipe provisions an untracked, re-adoptable utility service

**Status:** open / deferred
**Surfaced by:** Codex R3 audit (2026-06-04), BUG 4.
**Severity:** low — pre-existing (not an R3 regression), and mailpit is a documented stopgap.

## Problem
`internal/knowledge/recipes/mailpit.import.yml` declares a single service
(`alpine@3.20` + `buildFromGit`) with **no `zeropsSetup`**. `ParseRecipeImportShape`
classifies a no-`zeropsSetup` service as a managed dep, so the recipe has zero
RUNTIMES → `DeriveRecipePlan` returns no targets → `BootstrapCompleteRecipePlan`
treats it as a managed-only bootstrap and writes **no ServiceMeta**. But the
service is a real runtime-like container (alpine + buildFromGit), so a later
`zerops_discover` / route inference can treat it as **unmanaged / adoptable**
(route.go ~:311, discover.go ~:168) — the agent may be prompted to re-adopt a
service the recipe just created. Provisioned-but-untracked, the bug class R3
otherwise eliminated — but only for this one malformed recipe.

This is NOT new in R3: the deleted slot-matcher also never wrote a meta for a
service without `zeropsSetup`. R3 just makes the gap visible (every other recipe
now tracks every runtime).

## Why deferred
mailpit is an **in-repo stopgap** (see CLAUDE.md "TEMPORARY: in-repo mailpit
recipe") — to be removed once it lands in the Strapi catalog. Fixing the corpus
entry is the cleanest path and will happen at that migration.

## Options when promoted
1. **Corpus fix (preferred):** give mailpit's service a `zeropsSetup` (it IS a
   runtime) so it derives a tracked simple target. Do this when re-authoring
   mailpit in Strapi.
2. **Model utility imports:** a first-class "utility service" classification in
   the shape (runtime-like, tracked, but not an app codebase) — only worth it if
   more utility recipes appear.

## Trigger to promote
mailpit lands in Strapi (the `sync pull` overwriting `mailpit.md` is the signal,
per CLAUDE.md) — fix the corpus entry then. Or: a second no-`zeropsSetup`
runtime-like recipe appears → model utility imports.
