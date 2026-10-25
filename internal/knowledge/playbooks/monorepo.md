---
description: Procedural playbook for repos with more than one deployable part (pnpm/turbo workspaces, polyglot repos, docker-compose, SPA + API) — detect parts, choose one dev service or one dev pool per part, write one setup per part.
---

# Monorepo — several parts in one repo

Applies only when the repo holds more than one deployable part. A repo with one
deployable app follows the ordinary develop flow unchanged — stop reading here.

## 1. Detect the parts

Parts are the setups in the repo's `zerops.yaml`. The file is the only truth once
it exists: when it already has several setups, take them as the parts and do not
re-detect.

Without a `zerops.yaml`, read the repo for these signals:

| Signal | Reading |
|---|---|
| `pnpm-workspace.yaml`, `workspaces` in `package.json`, `nx.json`, `turbo.json` | JS workspace — parts are the packages with a start script (`apps/*`) |
| `package.json` packages without a start script | libraries, not parts |
| `go.mod`, `pyproject.toml`, `requirements.txt` in a subfolder | one more part in that language |
| several `Dockerfile`s | one part per Dockerfile |
| `docker-compose.yml` | see §6 |
| one `package.json` with a bundler (vite) and a server | one part — SPA and API in one folder are one app; ask only if the person wants the web as a separate static service |

Propose the parts to the person once, in one sentence ("I found web, api and
packages/shared (a library). Deploy web and api as two parts?"), then write one
setup per part plus a `dev` setup in the single root `zerops.yaml`. The `dev`
setup stays minimal — base, system packages in `run.prepareCommands`, ports —
and installs no dependencies: they are installed in the running dev service
the way the repo's docs install them.

## 2. One dev service or one dev pool per part

1. All runtimes can run in one container (a second runtime comes from
   `sudo zsc install <runtime>` on Ubuntu, `sudo apk add <package>` on Alpine) and the
   dev servers plus the install peak fit its RAM → **one dev service holds the whole
   repo** (default). Each part's dev server runs under its own name in that service.
   A part you add gets its dev server in the same step as its setup. Worked example —
   `appdev` runs the `web` part and the repo gains a `worker` part (no port): after
   adding the `worker` setup to `zerops.yaml`, start the worker on the dev service:

   ```
   zerops_dev_server action=start hostname="appdev" name="worker" command="bun worker.ts" noHttpProbe=true
   ```

   The first start declares it in `.zcp/dev-servers.json` beside the running one:

   ```json
   {"version": 1, "servers": [{"name": "web", "port": 3000, "command": "bun index.ts"}, {"name": "worker", "command": "bun worker.ts"}]}
   ```

   Commit the declaration with the setup.
   `zerops_dev_server action=status hostname="appdev"` then lists both servers. Deploying the part's stage
   (`workerstage`) never runs it on `appdev`.
2. Otherwise — runtimes that cannot share a container, or one container is not
   enough — **one dev pool per part**: each part's dev environment is a Swarm
   pool of its own, and each branch leases one container of it. The pool's
   import stanza, its setup and a declaration several parts share:
   `zerops_knowledge uri="zerops://themes/pools-and-box"`, section "A part's dev pool".
   zcp no longer sets up a shared code volume — one Local Storage volume several
   dev services mount for the repository. A project that already keeps its
   repository on one keeps working, and
   `zerops_workflow workflow="pool-migrate" service="<dev>"` moves it onto one
   dev pool per part.
3. More people on the repo means more Mates, never more dev services.

## 3. Write the setups

- One `build` per part. Build always receives the whole repo; narrow it with the
  package-manager filter and `deployFiles`.
- Put shared settings in a `base` setup and let the others `extends` it. `extends`
  replaces lists whole (`buildCommands`, `deployFiles`, ports) and merges maps, so
  repeat a list in the child when it differs.
- Every setup needs a `run` section, including static parts.
- pnpm is not in the base image: install it in `build.prepareCommands`. The
  Ubuntu Node run image ships it through Corepack: never `npm i -g pnpm` in
  `run.prepareCommands` or over SSH — it fails with `EEXIST`.
- pnpm `node_modules` are symlinks into the repo root, so a part that deploys
  `node_modules` ships the root `node_modules` and the shared packages too.
- A heavy build: `zsc scale cpu max 10m` in the build commands, then
  `zsc scale cpu auto`; a build killed out of memory (`next build`) also
  takes `zsc scale ram max 10m`.

## 3a. Stages in the bootstrap plan

A plan for a repo with several parts names every stage with its setup: put
`"stages": [{"hostname": "webstage", "setupName": "web"}, {"hostname": "apistage", "setupName": "api"}]`
inside `runtime` instead of `stageHostname`, and use one spelling, never both. The dev
service and all its stages are then one pair; a stage left out of `stages` becomes a
standalone `simple` service.

When a part is added to a repo that already has setups, nothing is renamed.
An existing setup and its stage keep their names; only the new part gets a new
setup. Never rename a `setup:` the repo already has — its stage deploys under that
name. A repo whose `appstage` deploys `setup: prod` gains a `worker` part as
`"stages": [{"hostname": "appstage", "setupName": "prod"}, {"hostname": "workerstage", "setupName": "worker"}]`.

## 4. Dev servers

- A repo that documents its own dev command runs that command as it stands, with its
  own watcher, ports and host setting; the watcher rule below is for a command you
  write. Never put your own proxy in front of a dev server that listens on loopback.
- Vite rejects unknown hosts: use the repo's own allowed-hosts setting when it has one;
  in an app you write, set `server.allowedHosts` in `vite.config` to accept the
  service subdomain.
- Watch, ignore and heavy servers that do not autostart:
  `zerops_knowledge uri="zerops://themes/dev-servers"`.
- Stop a dev server by its process group, never by a name match — `pkill -f <name>`
  kills sibling parts.
- A static service cannot proxy requests to the API.
- A dev server serving very many unbundled modules (a large Vite app) can stall
  in a browser over the subdomain while curl loads every module: open it in the
  repo's bundled or preview mode.

## 5. Cross-part URLs

A part never hardcodes another part's URL. Define the URL as a project variable
built on `${zeropsSubdomainHost}` and read that variable at build or run time. A
`${<service>_zeropsSubdomain}` reference stays literal in a project variable.

## 6. docker-compose

Convert compose services to native parts: app services become runtime setups,
databases and caches become managed services. Keep a compose service as a Docker
service only when its image already exists in a registry and the person wants
Docker — say so and ask once.
