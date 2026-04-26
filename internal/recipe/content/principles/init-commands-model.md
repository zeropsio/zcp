# execOnce — key shape by lifetime

Three key shapes, three lifetimes. Pick by asking whether the command
re-converges every deploy, once per service lifetime with canonical
vocab, or once per service lifetime with a versioned re-run lever.

- **Per-deploy — `${appVersionId}`**: re-runs every deploy. Only for
  idempotent work (migrations with `IF NOT EXISTS`, additive columns,
  backfills safe to re-apply).
- **Canonical static — `bootstrap-seed`, `seed-r01`**: once per service
  lifetime. For non-idempotent work (seeds, search-index bootstrap,
  one-shot provisioners). Use a short canonical name when you never
  expect to re-run.
- **Arbitrary static with version — `<slug>.<operation>.v1`** (e.g.
  `nestjs-showcase.seed.v1`, `laravel-showcase.scout-import.v1`):
  once per service lifetime, same semantics as canonical static, but
  the `.v1` suffix is a documented re-run lever. Bump `.v1` → `.v2`
  to deliberately re-trigger when the DATA changes. Use when you
  want both once-per-lifetime semantics AND a discoverable way to
  re-run without the ambient knowledge that `r01` → `r02` does it.

```yaml
initCommands:
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js
  - zsc execOnce bootstrap-seed --retryUntilSuccessful -- node dist/seed.js
  - zsc execOnce nestjs-showcase.scout-import.v1 --retryUntilSuccessful -- node dist/reindex.js
```

## In-script guard pitfall

`if (count > 0) return` inside a seed gated on `${appVersionId}` looks
idempotent but skips any non-idempotent sibling work (search-index
creation, cache warmup) inside the guarded branch. DB populated + index
empty → silent 500s. Match key shape to lifetime; the guard is not the
right lever.

## Decomposition

When one command does multiple non-idempotent things, either gate all
on one static key or split into separate `initCommands` with shapes
matching each operation's own lifetime. Don't mix lifetimes under
one key.

**Distinct keys per step.** When you split work into multiple
`initCommands`, each step needs a DISTINCT lock key. Two commands
sharing the same `${appVersionId}` collapse to one lock — the first
runner wins and writes the success marker; the second sees the marker
and skips silently even though the command tail differs.

```yaml
# WRONG — both commands share the same ${appVersionId} lock; only one runs
initCommands:
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/seed.js

# RIGHT — each step gets its own distinct key under the same deploy version
initCommands:
  - zsc execOnce ${appVersionId}-migrate --retryUntilSuccessful -- node dist/migrate.js
  - zsc execOnce ${appVersionId}-seed --retryUntilSuccessful -- node dist/seed.js
```
