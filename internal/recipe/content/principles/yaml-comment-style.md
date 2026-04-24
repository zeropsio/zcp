# YAML comment style

ASCII `#` only, one hash per line, one space after, then prose.

- No dividers (runs of `-`, `=`, `*`, `#`, `_`)
- No banners (multi-line boxes, `# === Section ===`)
- No decoration

Wrap at ~65 chars. Use **multi-line blocks** — a run of adjacent `#`
lines reads as one prose paragraph. Bare `#` lines stay inside the
block as paragraph separators.

**One causal word per block is enough.** The validator checks each
block (not each line) for `because` / `so that` / `otherwise` /
`trade-off` / em-dash. Do NOT stuff every line with `because` — the
reference (`laravel-showcase-app/zerops.yaml`) lets a block's first
paragraph carry rationale and the rest carry detail.

Short labels (≤40 chars) pass unconditionally — `# Base image`,
`# Bucket policy` need no rationale.

GOOD (multi-line block, one causal word, natural wrap):

```
# Config, route, and view caches MUST be built at runtime.
# Build runs at /build/source/ but the app serves from
# /var/www/ — caching during build bakes wrong paths.
#
# Migrations run exactly once per deploy via zsc execOnce,
# regardless of how many containers start in parallel.
```

BAD (single-line run-on, stuffed causal words):

```
# Dev setup — deploys the source tree so that SSH sessions and `zerops_dev_server` can drive `nest start --watch` without a rebuild. `zsc noop --silent` keeps the container idle so that an external watcher owns the process, otherwise every code edit would force a redeploy.
```

BAD (decorative divider): `# ----- DEV SETUP -----`.

Shape reference: `/Users/fxck/www/laravel-showcase-app/zerops.yaml`.
