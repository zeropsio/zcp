# execOnce — key shape by lifetime

Two key shapes, two lifetimes. Pick by asking whether the command
re-converges every deploy or runs once per service lifetime.

- **Per-deploy — `${appVersionId}`**: re-runs every deploy. Only for
  idempotent work (migrations with `IF NOT EXISTS`, additive columns,
  backfills safe to re-apply).
- **Static — `INIT_SEED`, `INIT_SCOUT_IMPORT`, `INIT_<OPERATION>`**:
  once per service lifetime. For non-idempotent work (seeds,
  search-index bootstrap, one-shot provisioners). The key names the
  OPERATION, never the recipe — every recipe uses the same `INIT_SEED`
  for its seed so the convention reads identically across the whole
  corpus. To deliberately re-run after the DATA changes, bump a version
  suffix (`INIT_SEED` → `INIT_SEED_V2`): the new key has no recorded
  run, so the command fires once under the new name. That suffix is the
  documented re-run lever — discoverable from the yaml, no ambient
  knowledge required.

```yaml
initCommands:
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js
  - zsc execOnce INIT_SEED --retryUntilSuccessful -- node dist/seed.js
  - zsc execOnce INIT_SCOUT_IMPORT --retryUntilSuccessful -- node dist/reindex.js
```

## In-script guard pitfall

A seed gated on `${appVersionId}` re-runs every deploy, so you reach for
an `if (count > 0) return` guard to stop duplicate rows — but that guard
is the bug, not the fix. It skips any non-idempotent sibling work
(search-index creation, cache warmup) inside the guarded branch: DB
populated + index empty → silent 500s on the next deploy. The seed key
is non-idempotent work, so it belongs on a static `INIT_SEED` key that
runs exactly once; match key shape to lifetime, never paper over a
per-deploy seed with an in-script guard.

## Decomposition

When one command does multiple non-idempotent things, either gate all on
one static key or split into separate `initCommands` with shapes matching
each operation's own lifetime. Don't mix lifetimes under one key — a
per-deploy migration and a once-per-lifetime seed are two keys, never one.

**Distinct keys per step.** When you split work into multiple
`initCommands`, each step needs a DISTINCT lock key. Two commands sharing
the same `${appVersionId}` collapse to one lock — the first runner wins
and writes the success marker; the second sees the marker and skips
silently even though the command tail differs.

```yaml
# WRONG — both commands share the same ${appVersionId} lock; only one runs
initCommands:
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/seed.js

# RIGHT — migrate is idempotent (per-deploy, distinct suffix); seed is
# non-idempotent (static INIT_SEED, once per lifetime)
initCommands:
  - zsc execOnce ${appVersionId}-migrate --retryUntilSuccessful -- node dist/migrate.js
  - zsc execOnce INIT_SEED --retryUntilSuccessful -- node dist/seed.js
```
