# CODEBASE_ZEROPS_COMMENTS

## Pass — explains trade-offs per field

```yaml
run:
  # httpSupport: true registers with the L7 balancer. Without it the
  # balancer won't route — subdomain returns 502.
  ports:
    - port: 3000
      httpSupport: true

  # execOnce + static key: platform records the key and skips re-runs.
  # Pair with --retryUntilSuccessful so a transient DB outage doesn't
  # permanently burn the key.
  initCommands:
    - zsc execOnce db-migrate-v1 -- npm run migration:run --retryUntilSuccessful
```

## Fail — narrates the field name

```yaml
run:
  # deployFiles: ./ ships the working tree
  deployFiles: ./
  # initCommands runs these on deploy
  initCommands:
    - zsc execOnce db-migrate-v1 -- npm run migration:run
```

Reader can read the field — comment must teach WHY `./` vs `./dist/~`,
or delete.
