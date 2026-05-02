# Audit — `Synthesize` cache layer impact of new `ExportStatus` envelope field

**Step**: Phase 0a.0.5 of `plans/atom-corpus-verification-2026-05-02.md`.
**Date**: 2026-05-02.
**Question**: Does any cache layer key on `StateEnvelope` such that adding
`ExportStatus string` to the envelope shape would require updating cache
keys to avoid stale-collision (envelopes with the same Phase but
different ExportStatus reusing one cache entry)?

## Method

`grep -rn -i "cache" internal/workflow/`. Read `internal/workflow/synthesize.go`
end-to-end.

## Findings

The only cache in `internal/workflow/synthesize.go` is `corpusOnce sync.Once`
(lines 580-611) backing `LoadAtomCorpus`. This caches the **parsed atom
corpus** (74 embedded files → `[]KnowledgeAtom`) once per process. Cache
key is implicit (no key — there is one corpus, immutable post-build).

`Synthesize(envelope StateEnvelope, corpus []KnowledgeAtom)` (lines 60-181)
**is stateless per call**: it walks the corpus, runs axis matching against
the envelope, sorts, renders, returns. No per-envelope memoization, no
hash-keyed lookup of prior rendered bodies. Each invocation re-evaluates
from scratch.

Other `cache` matches in the package are unrelated string content:

- `valkey@7.2`/`keydb@6` services with hostname `cache` in test fixtures
  (`adopt_test.go`, `recipe_*_test.go`).
- `RecipeBlueprint.CacheLib` field (recipe configuration, not synthesizer
  state).
- Comment about Docker-build cache in `recipe_multibase.go`.
- `recipe_validate.go` mentioning the cache service kind.

None of these touch envelope-keyed memoization.

## Decision

**No cache layer keys on envelope shape.** Adding `ExportStatus string` to
`StateEnvelope` has no cache-collision risk. `Synthesize` will see the new
field on each invocation via the existing axis-match path.

`LoadAtomCorpus` is unaffected because the corpus is keyed only by file
contents (re-parsed once per process), and the `exportStatus:` frontmatter
key joins the existing parsed `AxisVector` shape.

No further action required for cache safety. Phase 0a may proceed.
