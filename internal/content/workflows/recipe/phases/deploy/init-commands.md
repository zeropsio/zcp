# Substep: init-commands

This substep completes when every `initCommand` declared on `setup: dev` has been observed to run successfully in the target's runtime logs. `initCommands` include migrations, seeders, search-index builders, cache warmups — anything the recipe needs to run exactly once (or once per deploy) to prepare application state.

The zerops.yaml authoring for these keys (per-deploy vs static-key, idempotency shape, ordering) lives in the generate phase's `zerops-yaml/seed-execonce-keys.md` atom. This substep is the procedural flow for triggering those keys and confirming they completed.

## When initCommands run

The platform invokes `setup: dev` `initCommands` during deploy activation — on every fresh deploy, including the first one on an idle-start container. You do not invoke them by hand; the `zerops_deploy setup=dev` call you already made in the `deploy-dev` substep fired them. This substep's job is to confirm they actually ran and ran cleanly.

## Verification shape

For each target that declares `initCommands` in its `setup: dev`, read the container's runtime logs and look for the framework-specific output each command emits (applied-migration rows, "20 articles seeded", "Meilisearch: indexed 20 documents", framework-specific cache-cleared lines):

```
zerops_logs serviceHostname="{target}" limit=200 severity=INFO since=10m
```

Expected outcomes:

- **Output present, success line visible** — initCommands ran cleanly. Proceed to the next substep.
- **Output present, error logged** — one initCommand crashed. The deploy response typically returns `DEPLOY_FAILED` with `error.meta[].metadata.command` identifying which command failed. Fix the command (application code or zerops.yaml) and redeploy through `deploy-dev`. Do not re-run the failing command by hand over SSH — the platform's execOnce gate is the reproduction case, and running it manually hides that.
- **Output completely absent** — probe before assuming anything:
  - Read the zerops.yaml back from the mount; confirm `setup: dev` actually declares `initCommands`.
  - Confirm the deploy transitioned to ACTIVE. If it is still DEPLOYING, wait and re-read logs.
  - Widen the `since` window to `since=30m`.
  - Drop `severity=INFO` to see every log line.

## Post-deploy data verification

After a successful deploy, confirm the expected application state exists — do not infer "initCommands ran" from "deploy returned ACTIVE" alone. If a prior failed deploy burned the execOnce key for a command, the subsequent successful deploy may skip that command silently. Query the database for seeded records, verify the search index contains documents, confirm the cache is populated. If the data is missing, the execOnce key was burned; recovery is below.

## Recovering a burned execOnce key

A seed that crashed mid-insert can leave the per-deploy execOnce key marked done while the data is partial. The next retry of the same deploy will not re-run the seed because the platform considers it already executed. Symptom: the seeder output appears in the FIRST deploy's logs, then is absent on every subsequent retry, and the database contains partial data.

Two recovery paths:

1. **Force a fresh deploy version** — touch any source file (a whitespace edit is enough), then redeploy through `deploy-dev`. The new deploy version makes the per-deploy execOnce key re-fire. This is the preferred path because it preserves "never manually patch workspace state."
2. **Hand-run the seed command once** — `ssh {hostname} "cd /var/www && {seed_command}"` then redeploy to confirm the fix lands. Use this only when the seed depends on a schema that exists only after a successful initCommand run.

Every data gap at this substep is resolved either by recovery path (1) or (2) above. If initCommands truly did not fire after a successful ACTIVE deploy and neither recovery path applies, stop this substep and surface the condition. A recipe that only works because a human hand-ran migrate plus seed over SSH during the workspace build ships broken to end users who never see that manual fix — the substep gate blocks that outcome by design.

## Attestation shape

One line per target that declared initCommands: the target name, each command observed to have run (by the framework-specific output signature), and the post-deploy data check result.
