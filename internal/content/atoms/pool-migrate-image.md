---
id: pool-migrate-image
priority: 3
phases: [pool-migrate-active]
title: "Deploy a part's pool setup to its pool"
references-fields: []
---

### Deploy a part's pool setup to its pool

Once the pool holds the dev service's env the step is `image`: the pool gets
its image from the part's pool setup — the `zerops.yaml` setup named after the
pool — deployed from a commit of the dev service's repository, not from its
working tree. A deploy of a pool bakes what it ships into every container the
pool creates after it, so an uncommitted file would reach every lease.

Write the pool setup and `previewPortBase` in the dev service's checkout and
commit them yourself; the flow commits nothing. From then on every answer
carries `image`: the checks of the setup in the checkout's latest commit
(`image.commit`).

| `image.missing` | what the commit needs |
|---|---|
| `commit` | a commit in the dev service's checkout |
| `config-file` | a `zerops.yaml` (or `zerops.yml`) that parses |
| `pool-setup` | a setup named after the pool |
| `start` | `run.start: zcp-devsup run`: a pool container runs its dev servers under the supervisor |
| `install` | a `run.prepareCommands` line installing `zcp-devsup` — its message names the whole line to add |
| `preview-port-base` | `previewPortBase` in `.zcp/dev-servers.json`: a lease answers its preview on `previewPortBase` + its container's number |
| `preview-ports` | every port from `previewPortBase + 1` to `previewPortBase + maxContainers` in `run.ports`, each with `httpSupport: true` |
| `service` | with several parts, each dev server in `.zcp/dev-servers.json` names its part's pool as `"service"`: a pool's container runs the servers naming its pool and those naming none |

A complete pool setup for a Node part whose pool holds three containers, with
`"previewPortBase": 4000` in `.zcp/dev-servers.json`:

```yaml
zerops:
  - setup: apppool
    build:
      base: ubuntu/nodejs@22
      addToRunPrepare:
        - package.json
        - package-lock.json
    run:
      base: ubuntu/nodejs@22
      ports:
        - port: 4001
          httpSupport: true
        - port: 4002
          httpSupport: true
        - port: 4003
          httpSupport: true
      prepareCommands:
        - sudo sh -c 'u=https://github.com/zeropsio/zcp/releases/latest/download/zcp-devsup-linux-amd64; f=/usr/local/bin/zcp-devsup; if command -v curl >/dev/null; then curl -fsSL "$u" -o "$f"; else wget -qO "$f" "$u"; fi && chmod 0755 "$f"'
        - cp package.json package-lock.json /var/www/ && cd /var/www && npm ci
      start: zcp-devsup run
```

Once the declaration names the pools, the dev service's own supervisor runs
none of those servers after its next reload or restart: run them on the
parts' leases from then on.

While `image.missing` names anything there is no `next`, and
`nextUnavailable` says what to change. Once nothing is missing, `next` is
`zerops_deploy sourceService="<dev>" targetService="<pool>" setup="<pool>" sha="<commit>"`:
make it as given. `image.uncommitted` names the setup's files you changed
since that commit — the deploy ships the commit without those changes.

**Dependencies.** `image.findings` never hold the deploy, but every lease
pays for them:

| `image.findings` | what to change |
|---|---|
| `deps-lockfile` | list every file the dependency install reads in `build.addToRunPrepare` — the manifest and its lockfile, and in a workspace monorepo every member's manifest and the workspace definition — so `run.prepareCommands` installs from them and the image rebuilds when one of them changes |
| `deps-baked` | install the dependencies into `/var/www` in `run.prepareCommands`: what it writes survives every reset of a container, and the checkout keeps it. What `build.deployFiles` ships into `/var/www` is wiped by every reset. A branch whose lockfile differs declares its incremental install as `prepare` in `.zcp/dev-servers.json` |

**Done.** The step is done once nothing is missing and the pool runs an app
version built after the pool setup last changed (`image.deployed.current`);
the step is then `move`. A later commit that changes the pool setup makes the
step `image` again; a change of another part's setup in the same file leaves
it done (`image.deployed.setupUnchanged`). While a build of the pool runs,
`next` waits on it.

A deploy of a pool reaches only the containers it creates afterwards: a
container the pool already holds keeps its image until
`zerops_lease action=rollout pool="<pool>"` replaces it, and a held lease
keeps its image until it is released.
