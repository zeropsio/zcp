# sync push silently skips zerops.yaml on length-shrink

**Surfaced**: 2026-05-19 + laravel-showcase recipe fix (PR #4) on
`zerops-recipe-apps/laravel-showcase-app`. Local edit dropped
`--retryUntilSuccessful` flags (~120 bytes) → new yaml ~124 bytes SHORTER than
upstream → `internal/sync/push_recipes.go:223` skip-condition fired silently.
PR went out with README.md updated but zerops.yaml file unchanged. Worked
around by manual `gh api --method PUT contents/zerops.yaml` on the PR branch.

**Why deferred**: workaround is two extra CLI calls and a one-time annoyance
per shrinking edit; the underlying safeguard ("API's integration-guide YAML
may be a subset — don't regress") is still load-bearing for cases where pull
genuinely drops content. Fix needs a design decision (structural compare vs
opt-out flag vs warning surface), not a 5-line patch.

**Trigger to promote**: second occurrence (any push where a shrinking edit
silently divides README from zerops.yaml on the merged PR), or if a recipe
ships with mismatched README/zerops.yaml because nobody noticed the skip.

## Sketch

Three candidate fixes, increasing scope:

1. **Surface a warning** — when skip fires, push output prints
   `WARN <slug>: zerops.yaml skipped (new=<N> bytes, existing=<M> bytes; use --force-yaml to override)`.
   No behavior change, just visibility. Cheapest. Doesn't prevent the bug,
   just makes it loud.
2. **`--force-yaml` opt-out** — adds a CLI flag that bypasses the length
   gate when the operator explicitly wants the shorter content. Still
   defaults to current safe behavior; operator opts in when they know
   what they're doing.
3. **Structural compare** — instead of `len(new) >= len(existing)`, compare
   parsed YAML structure. A "subset" detection that catches API regression
   (missing top-level keys, missing setup) but allows comment / flag /
   key-name diffs. More work; the right design but requires yaml parse +
   diff logic.

Recommended path: ship (1) immediately as part of any next sync push touch.
(2) when an operator actually needs it. (3) only if (2) proves insufficient.

## Risks

- (1) only — operator may miss the warning and ship a half-updated PR
  anyway. Warning needs to be visible enough that it's hard to ignore.
- (3) — yaml parse failure on edge content (multi-document yaml,
  preprocessor templates) might block legitimate pushes.

## Refs

- `internal/sync/push_recipes.go:218-228` — skip block + comment
- `docs/recipes/README.md:71-83` — recipe push docs (claims single
  source of truth, doesn't mention skip)
- PR #4 on `zerops-recipe-apps/laravel-showcase-app` (2026-05-19) —
  reproducer
