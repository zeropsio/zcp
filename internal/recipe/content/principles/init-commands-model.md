# execOnce — key shape by lifetime

Two key shapes, two lifetimes. Pick by asking whether the command
re-converges every deploy or runs once per service lifetime.

- **Per-deploy — `${appVersionId}`**: re-runs every deploy. Only for
  idempotent work (migrations with `IF NOT EXISTS`, additive columns,
  backfills safe to re-apply).
- **Static — stable string (`bootstrap-seed-r01`)**: once per service
  lifetime. For non-idempotent work (seeds, search-index bootstrap,
  one-shot provisioners). Bump `r01` → `r02` to force re-run when the
  DATA changes, not when surrounding code changes.

```yaml
initCommands:
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- node dist/migrate.js
  - zsc execOnce bootstrap-seed-r01 --retryUntilSuccessful -- node dist/seed.js
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
