# YAML comment style

ASCII `#` only, one hash per line, one space after the hash, then
prose. That is the full vocabulary.

- No dividers (runs of `-`, `=`, `*`, `#`, `_`)
- No banners (multi-line boxes, `# === Section ===`)
- No decoration

Wrap comments at ~65 characters. Use **multi-line blocks** for
anything longer than a label — a run of adjacent `#` lines reads as
one prose paragraph. Bare `#` lines stay inside the block and act as
paragraph separators between related thoughts.

**One causal word per block is enough.** The validator checks each
block (not each line) for a `because` / `so that` / `otherwise` /
`trade-off` / em-dash; once the block has one, the whole block passes.
Do not stuff every line with `because` — the reference
(`laravel-showcase-app/zerops.yaml`) lets a block's first paragraph
carry the rationale and the rest carry the detail.

Short labels (≤40 chars) pass unconditionally — `# Base image` and
`# Bucket policy` don't need rationale.

Good (multi-line block, one causal word, wraps naturally):

```
# Config, route, and view caches MUST be built at runtime.
# Build runs at /build/source/ but the app serves from
# /var/www/ — caching during build bakes wrong paths.
#
# Migrations run exactly once per deploy via zsc execOnce,
# regardless of how many containers start in parallel.
# Seeder populates sample data on first deploy so the
# dashboard shows real records immediately.
```

Bad (single-line run-on stuffed with `so that` / `otherwise` to satisfy
a per-line rule that no longer exists):

```
# Dev setup — deploys the full source tree so that SSH sessions and `zerops_dev_server` can drive `nest start --watch` without a rebuild. `zsc noop --silent` keeps the container idle so that an external watcher owns the long-running process, otherwise every code edit would force a redeploy.
```

Bad (decorative divider):

```
# ------------------------------------------------------------
# DEV SETUP
# ------------------------------------------------------------
```

Shape reference: `/Users/fxck/www/laravel-showcase-app/zerops.yaml`.
