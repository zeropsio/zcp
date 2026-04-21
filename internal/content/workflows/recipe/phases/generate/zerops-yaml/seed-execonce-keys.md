# Two execOnce keys, two lifetimes

`zsc execOnce <key>` gates a command on the literal key value. Two key shapes are correct for different jobs — pick the shape by asking whether the command should re-converge on every deploy or run exactly once per service lifetime.

## Per-deploy key — `${appVersionId}`

Runs once per deploy across replicas. Correct for commands that are **idempotent by design** and should re-converge every deploy: schema migrations (`CREATE TABLE IF NOT EXISTS`, additive column adds, data backfills), schema-sync helpers that are safe to re-apply.

## Static key — any stable string (for example `bootstrap-seed-r01`)

Runs once per service lifetime, across all deploys. Correct for commands that are **not idempotent by design** and must not re-run on every deploy: seeds that insert initial rows, one-shot provisioners (create the search-engine index, upload initial S3 objects), bootstrap operations (create a default tenant).

## Canonical shape — one command, one matching key

```yaml
initCommands:
  - zsc execOnce ${appVersionId} --retryUntilSuccessful -- npx ts-node src/migrate.ts
  - zsc execOnce bootstrap-seed-r01 --retryUntilSuccessful -- npx ts-node src/seed.ts
```

A revision suffix on the static key (for example `r01`, `r02`) is the way to force a re-run when the seed data itself changes: bump the suffix, the next deploy runs the command once under the new key, and then never again under it.

## When one command does several non-idempotent things

When the seed command inserts rows **and** creates a search index **and** warms a cache, those three steps are either (a) all gated on a single static key so all three run exactly once per service lifetime, or (b) decomposed into separate `initCommands` entries — each with the key shape that matches its own lifetime. Use the static-key shape when any of the operations is non-idempotent.

An in-script row-count short-circuit (`if (count > 0) return`) combined with a per-deploy key on a seed creates a state mismatch: any idempotency-sensitive sibling work (search-index creation, cache warmup, S3-object upload) inside the guarded branch skips as well, and a database populated but a search index empty produces silent 500s on later search requests. Pick the key shape that matches the operation's lifetime; the guard is not the right lever.

## Pre-attest check

Seed operations use a static key. A grep confirms no seed command is mistakenly gated on `${appVersionId}`:

```
grep -nE 'zsc execOnce \$\{appVersionId\}.*seed' zerops.yaml
```

Non-empty output means a seed is still gated on the per-deploy key and will fail to re-run when the data changes without a deploy-file touch.
