# platform-principles / stripped-build-root

The `zerops.yaml` `deployFiles` list is the manifest of what ships to production. Everything that ships is something a reader or operator sees; everything that does not ship stays out of the list. Keep the build root lean — no authoring-time test artifacts, no git metadata, no scaffolder debris.

## deployFiles is the allow-list

The `deployFiles` list names every path that ships. Use a positive enumeration of the runtime artifacts (the built app, its `package.json`, `node_modules` if runtime-resolved, the migrations directory the runtime reads) rather than a broad `./` catch-all. A narrow allow-list keeps scaffolder experiments and test probes out of the deployed image without a separate prune step.

## What does not ship

- **Scaffolder test artifacts** — `preship.sh`, `*.assert.sh`, and any self-test shell scripts the scaffolder may have generated as part of author-time verification. These are authoring scaffolding, not runtime code.
- **`.git` directory** — git metadata has no place in a production image. It bloats the deploy artifact and can expose internal history.
- **Dev-only dependencies in a prod `node_modules`** — use the prod buildCommands to install production dependencies only, or list `node_modules` outside `deployFiles` and let the prod build reinstall.

## Removing the `.git` directory

Framework scaffolders that initialize git during scaffold offer either:

- a `--skip-git` flag on the CLI, or
- a post-scaffold cleanup: `ssh {host} "rm -rf /var/www/.git"`.

Use the flag when the scaffolder supports it; otherwise the explicit cleanup. Either way, no `.git` is present in the build root at deploy time.

## Pre-attest before returning

```
ssh {host} "find /var/www -maxdepth 2 -type f \\( -name 'preship.sh' -o -name '*.assert.sh' \\) | head -n1 | grep -q . ; test $? -eq 1"
ssh {host} "test ! -d /var/www/.git"
```

The first succeeds when no preship or assert scripts remain in the top two directory levels. The second succeeds when `.git` has been removed.
