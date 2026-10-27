# Pools and Box — Swarm pools, a part's dev pool, Zerops Box

Read this when an app needs a Swarm pool or a Zerops Box. Either is infrastructure the app uses — never the app's own runtime, and neither is a managed database. Code the app's users send runs in a Box sandbox, never in the app's container: *Running code the app's users send* below.

## Swarm pool

**Type**: `swarm@1` (also `swarm-vm@1`). A pool is a service of up to 10 containers that its orchestrator hands out.
**Import**: `minContainers` / `maxContainers` between 0 and 10 (a higher maximum is capped at 10), plus `verticalAutoscaling`. Import a pool with `minContainers: 0`: containers the platform creates for the minimum have no primary snapshot and cannot be reset. Pre-warm containers through the orchestrator instead — `POST /container` with `{"count": N}`.
**Image**: comes from the app's zerops.yml setup, deployed with `zcli push`; `run.base` is the language base (e.g. `python@3.12`). A deploy reaches only the containers created after it.
**Orchestrator**: answers at `http://orch.<pool>.zerops`. An app service authenticates with the env reference `${<pool>_API_TOKEN}`; a call without it gets `401`.
**Tokens**: the pool's `API_TOKEN` (the bearer of every orchestrator call) and `ADMIN_TOKEN` (sent in the `X-Swarm-Admin-Token` header, only to take a container from its holder) are generated with the pool. Both are secrets: an app wires `API_TOKEN` by reference in its `run.envVariables` — `POOL_TOKEN: ${<pool>_API_TOKEN}` — and never copies the value into code, a file or another variable.
**Acquire**: `POST /container/acquire` with `start`, `clean`, `leaseTtlSeconds` (`0` = the lease never expires). The lease it returns must accompany every later call for that container — as the `X-Swarm-Lease` header or the `lease` query parameter.
**Release**: `release` frees the container; `reset=true` also restores the container's primary snapshot. A lease that expires frees its container silently, unreset. A container the platform created for `minContainers` has no primary snapshot: `reset=true` is refused (`400`, `containerAction`, reason `no-primary-snapshot`, not retryable) and the reservation stays; `DELETE /container/{id}` with its lease removes it (`202`, gone in about 7 s), and the pool creates fresh containers, with a snapshot, on later acquires.
**Exec**: `POST /container/{id}/exec` — the output streams as NDJSON, one command at a time.
**Clean**: a `clean` acquire resets only a container marked dirty, and writes over SSH never mark it: a container released without `reset` (or freed by an expiring lease) is handed out with the previous holder's files in `/var/www`, `/home/zerops`, `/tmp` and `/opt`. A reset restores the image: everything `run.prepareCommands` wrote survives, `/var/www` included; what was written or started since is gone, and `run.start` does not run again — `initCommands` do.
**Removals**: container removals run one at a time.
**Full pool**: when every container that can serve an acquire is leased and the pool holds its maximum, an acquire (or `POST /container/run`) is refused at once with reason `all-reserved` — nothing is queued and nothing created; it succeeds only after a release. A container freed with `reset=true` is handed out again at once; an empty pool creates one in about 10 s, and simultaneous acquires create one container at a time.
**Lowering the maximum**: setting `maxContainers` below the containers the pool holds deletes the highest-numbered ones within about 30 s, leased ones included, with their `/var/www`; their leases then answer `container-not-found`.
**Stop**: a stopped pool's orchestrator does not answer. A restart, or a stop and start, keeps every container's id, number, hostname and lease, its `/var/www` and `/home/zerops`; processes and `/tmp` are lost.
**Env**: env variables set on the pool service reach its containers, including leased ones for processes started after the change; the pool's own `API_TOKEN` does not.
**Isolation**: never set `envIsolation: none` on a pool — every service of the project would get its `ADMIN_TOKEN` and `API_TOKEN`. A service that needs the pool's token references it under a different name (`POOL_TOKEN: ${<pool>_API_TOKEN}`). A variable named `<pool>_API_TOKEN` holding `${<pool>_API_TOKEN}` stays the literal text, and while it exists every other reference to that name on the service does too.
**Addressing**: each container answers on its own hostname `node-id-<n>.runtime.<pool>.zerops`, from anywhere in the project, on every port it listens on; `run.ports` governs only the public subdomain. SSH into pool containers follows the pool's `sshIsolation`, set as a service env variable. A per-port subdomain routes to the containers listening on that port.

## A part's dev pool

A part's dev environment may be a pool: each branch gets one leased container (`zerops_lease`), and `zerops_dev_server`, `zerops_deploy` and `zerops_verify` take the lease alias `<pool>-<number>` as the hostname.

**Import**:

```yaml
services:
  - hostname: devpool
    type: swarm@1
    minContainers: 0
    maxContainers: <1-10>
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: <GB>
    envVariables:
      sshIsolation: "vpn service@<zcp hostname>"
    envSecrets:
      GIT_TOKEN: <the user sets the value>
```

- `minContainers: 0`: a container the platform creates for the minimum has no primary snapshot and cannot be reset, so releasing its lease deletes it, and it never counts as the pool's warm container. The pool keeps a warm container through the orchestrator instead (**Warm container** below).
- `enableSubdomainAccess: true`: each lease's preview is the pool's subdomain on the lease's preview port, and it answers 502 until the pool's subdomain access is on.
- `minRam`: a lease's dev servers and every check you run in it (a typecheck, tests, a build) share the container's RAM, so size `minRam` for the dev server plus the heaviest check run beside it, and run heavy checks one at a time and single-threaded where the tool offers it (TypeScript 7's `tsc --singleThreaded`, a test runner's single-worker mode). A check killed for memory ends its SSH call with exit 255 and no output, while the dev server can run on.
- `sshIsolation` goes in `envVariables`: a pool container answers SSH only when it is set as a service env variable admitting the zcp service. The top-level import field did not reach pool containers.
- `GIT_TOKEN` is a secret the user owns: name it in `envSecrets`, never write a value. Acquire clones with the pool's own `GIT_TOKEN`.
- The service env reaches the pool's containers.
- zcp calls the orchestrator with the pool's `API_TOKEN`. When zcp's own API token reads the pool's secrets masked, the zcp service takes it by reference under a different name: `<pool>_LEASE_TOKEN: ${<pool>_API_TOKEN}` in its env.

**zerops.yml setup**:

```yaml
zerops:
  - setup: devpool
    build:
      base: <language base>
      addToRunPrepare:
        - <every file the dependency install reads: the manifest and its lockfile — in a workspace monorepo, every member's manifest and the workspace definition too>
    run:
      base: <language base>
      ports:
        - port: <previewPortBase + 1>
          httpSupport: true
        # ... one entry per preview port, up to previewPortBase + maxContainers
      prepareCommands:
        - sudo sh -c 'u=https://github.com/zeropsio/zcp/releases/latest/download/zcp-devsup-linux-amd64; f=/usr/local/bin/zcp-devsup; if command -v curl >/dev/null; then curl -fsSL "$u" -o "$f"; else wget -qO "$f" "$u"; fi && chmod 0755 "$f"'
        - <copy them into /var/www, keeping their paths, and install the dependencies there>
      start: zcp-devsup run
```

- Preview ports: with `previewPortBase` in `.zcp/dev-servers.json`, a lease answers on `previewPortBase + ZEROPS_StableNumber`. A per-port subdomain routes only to containers listening on that port, and a container's `ZEROPS_StableNumber` starts at 1, so every port from `previewPortBase + 1` to `previewPortBase + maxContainers` is declared in `run.ports` with `httpSupport: true`.
- Dependencies: bake them into `/var/www` in `run.prepareCommands`. `run.prepareCommands` see only what `build.addToRunPrepare` copies — never the repository — so list there every file the install reads; the image rebuilds when one of them changes. They survive a reset, and the checkout keeps them (ignored files stay). A branch whose lockfile differs declares its incremental install as `prepare` in `.zcp/dev-servers.json` (e.g. `"prepare": "npm install --prefer-offline"`); acquire runs it in `/var/www` after the checkout, before the supervisor starts the declared servers, and reports it — a failed one leaves the lease held.
- Workspace monorepo (a package manager's workspace — pnpm, yarn, npm and the like): the install resolves every member, so `build.addToRunPrepare` lists every member's manifest (each `package.json` under the workspace's folders), the workspace definition (`pnpm-workspace.yaml`, or the root `package.json` whose `workspaces` lists the members), the lockfile (`pnpm-lock.yaml`, `yarn.lock`, `package-lock.json`) and any patch directory the lockfile references (`patches/`). A build command can gather them from the full tree into one folder that `addToRunPrepare` lists; `run.prepareCommands` copy them into `/var/www` keeping their paths and install the whole workspace from the lockfile (`pnpm install --frozen-lockfile`), so the installed tree matches the lockfile: a package manager that checks the tree before it runs a script (pnpm 11) reads a filtered install as stale — a `--filter "<pkg>..."` that keeps the part's whole closure included — and a later `pnpm run` or `pnpm exec` in a lease starts a full install first, heavy enough to run the lease out of memory. An image kept filtered calls the part's tools by path (`./node_modules/.bin/<tool>`), never through the package manager. And a part's checks can reach beyond its own package closure: a test or a typecheck that imports another member's sources needs that member's dependencies baked too, or the errors of the missing packages bury the change's own.
- Thread limit: the image build and `run.prepareCommands` run under a limit on threads, and a VM that starts one per core it sees runs into it — the Erlang VM under Elixir fails with `Failed to create dirty cpu scheduler thread …, error = 11` and the deploy ends `PREPARING_RUNTIME_FAILED` (or the build fails). Cap the VM's threads on the command: `ELIXIR_ERL_OPTIONS="+S 4:4 +SDcpu 2:2 +A 4"` (4 schedulers, 2 dirty CPU schedulers, 4 async threads) in front of the `mix` command in `run.prepareCommands`, or in `build.envVariables` for a build command; another runtime caps its worker or thread count its own way.
- Supervisor: the image carries `zcp-devsup` (installed in `run.prepareCommands`) and `start: zcp-devsup run`. `run.start` runs only in a container the pool just created — never again after a reset — so acquire and `zerops_dev_server` start the supervisor in a lease themselves. A pool whose image has no `zcp-devsup` is no dev pool: acquire says so and `zerops_dev_server` refuses its leases. A dev service's setup carries the same two lines (see `zerops_knowledge uri="zerops://themes/dev-servers"`).
- A deploy of the pool changes only containers created after it; existing containers, leased or free, keep their version until `zerops_lease action=rollout pool="<pool>"` replaces the free outdated ones. A lease is skipped and keeps its version until it is released and the pool is rolled out again.

**Several parts, one branch**: when a change edits and runs code in several parts, each its own dev pool, the branch holds one lease per such part. A part it only calls stays unleased; its callers reach that part's stage, which tracks `main`.

- Acquire: `zerops_lease action=acquire pools=["apipool","webpool"] branch="<branch>"` checks every pool, the branch and one repository before it takes any container, then leases one per pool at once, all or none — a failure hands back every container the call took. A branch new to origin is cut from one `main` commit for every part. A pool the branch already holds answers with that lease and `reused: true` (one lease per repository, branch and pool: another conversation on the branch shares it). A part added later joins with `pool="<pool>"` and the same branch. Each lease the acquire took carries `devServers` — each server's `status`, `running` or `starting` while it still compiles, and the preview server's `url` — and the `siblingEnv` values its servers got; check a `starting` one with `zerops_dev_server action=status hostname="<alias>" name="<server>"`.
- Wiring: `.zcp/dev-servers.json` names in `siblingEnv` each variable a part's servers read and the pool it reaches, and each entry names its part's pool as `service`:

```json
{
  "version": 1,
  "siblingEnv": { "API_HOST": "apipool" },
  "servers": [
    { "name": "api", "port": 8080, "command": "<the api's dev command, listening on 0.0.0.0:8080>", "cwd": "apps/api", "service": "apipool" },
    { "name": "web", "port": 3000, "command": "<the web's dev command>", "cwd": "apps/web", "service": "webpool" }
  ]
}
```

- Every server started in a lease gets the variable set to the container hostname of the branch's lease on that pool (`node-id-<n>.runtime.<pool>.zerops`, its own for its own pool); the app adds `http://` and the port. It serves server-side calls (a dev proxy, server rendering); a browser cannot resolve it. For code that runs in the browser, name the variable in `siblingPreviewEnv` instead (`"siblingPreviewEnv": {"VITE_HQ_URL": "hqpool"}`): it gets the preview URL (`https://`) of the branch's lease on that pool, resolved again whenever a lease of the branch joins or leaves — the port follows the container, so never copy it by hand. A name is in one of the two maps, never both. A pool the branch holds no lease on leaves the variable to the container's own value: set it in the calling part's pool service env to the called part's stage hostname (a `siblingPreviewEnv` variable to the stage's public URL) — `API_HOST: <the api's stage hostname>` beside `sshIsolation` in its import, or `zerops_env action=set serviceHostname="webpool" variables=["API_HOST=<the api's stage hostname>"]` — and run the called part's dev server on its stage's port. A supervisor already running keeps the env it started with. The start result's `siblingEnv` names each variable `set` and each `leftToContainerEnv`. A variable's name is never `ZEROPS_*`, `hostname`, `serviceId`, `projectId`, `PATH` or `HOME`; a broken rule invalidates the file. The pool's `zcp-devsup` must be new enough to take the values: on an older one a start that would set one is refused, naming the way to a newer one.
- A server a sibling calls listens on `0.0.0.0`: the preview reaches a loopback-only server, a sibling does not.
- `service`: a lease runs the entries naming its pool and those naming none, so an entry without `service` runs in every part's lease. An entry a lease's `zerops_dev_server` start adds names its pool. `port` and `preview: true` stay unique across the whole file.
- Sync: commit in one lease over SSH, then `zerops_lease action=sync lease="<alias>"` pushes the branch and fast-forwards every other lease of it. It never forces, and refuses a lease of `main`, uncommitted or untracked files in the source, and a push origin rejects — when origin moved on, integrate `origin/<branch>` there and sync again. Each other lease answers `up-to-date` or `fast-forwarded`; `has-own-commits`, `local-changes-in-the-way`, `on-another-branch`, `unreachable` and `failed` leave it as it was, with a note — so sync before you commit in another lease. A fast-forwarded lease lists in `unwatchedServers` only its runtime entries (`ctl run`), which serve the old code until restarted; a declared server reloads by itself — through its `watch`, or its command's own watcher — so a sync never names one. A lease whose `.zcp/dev-servers.json` changed has its supervisor reload it (`reloaded`).
- Release: `zerops_lease action=release branch="<branch>"` (with `repo` when the branch is leased for several repositories) reads every lease's unsaved work first; any found refuses them all, naming each lease's files and commits, and `force=true` releases every lease, discarding it. A lease the orchestrator would not release stays held with an `error`; the same call again finishes the branch.
- Rewire: an acquire or release that adds or removes a lease of the branch hands every other lease the recomputed values (a release, before its container goes), and their supervisors restart the servers running under the old ones (`rewired`); a lease it cannot reach is a note naming `zerops_dev_server action=restart hostname="<alias>" name="<server>"`. A lease that ends outside zcp is not chased: status marks a lease still wired to its container `wiring: "stale"`, with the restart that fixes it.

**Warm container**: each pool keeps one warm container — free, running, reset, on the pool's current version — that an acquire takes at once, where a create takes about 16 s on a container pool and minutes on a VM pool.

- Release: once its check for unsaved work passes, a release keeps its container as the pool's warm one, reset, when the pool has none besides it and the container runs the pool's current version; otherwise it removes the container with its own lease. The result says which — "kept as the pool's warm container", or removed and why (the pool already had its warm container, the container ran an outdated version, or the pool is full). A removal the platform refuses falls back to the reset and is named.
- Full pool: in a pool that serves an HTTP port and holds its maximum — a container being removed or stuck in a failed state counts — a release removes its container rather than keep it warm. A reset takes the container off the pool's subdomain routes, and with no room to create a container nothing puts it back, so the next lease on it would answer 502. The pool's next acquire creates a container instead (a cold start of about 16 s on a container pool, minutes on a VM pool), whose preview answers; the pool has no warm container while it is full, and status's `warm.last` says so.
- Top-up: after every acquire and release, zcp creates what the pool lacks through the orchestrator's `POST /container`, in the background and within `maxContainers` — never through `minContainers`, whose containers cannot be reset and never count as warm.
- Status: `zerops_lease action=status` reports each pool's `warm` — `target`, `ready` (absent with a note when it could not be counted), `preparing` and `last`, what the last top-up did, a failed or full one included.
- Clone: the warm container already holds the pool's repository — the one its last lease checked out — at `main`, cloned by zcp after it creates or keeps the warm container, so an acquire on it only fetches and checks out what its branch changes (z3-sized repository: 3–5 s instead of 9–10 s). An acquire that comes while that clone still runs waits for it; a clone of another repository, or one that did not finish, is removed and the checkout starts clean. The pool image needs `git`, `flock` and `setsid` (Ubuntu bases have them). `zerops_lease action=status` reports the last clone as `warm.clone`.
- An acquire that comes while a container is being created can be refused `containers-preparing`; zcp retries it for up to a minute (on a VM pool, to the acquire's own deadline) and the lease's note names the wait.

**Idleness**: `zerops_lease action=status` reads each held lease in one SSH call that changes nothing.

- `activity`: `lastChangeAt` is the checkout's last change — the newest of HEAD's last move (a commit, a checkout) and any modified or untracked file git does not ignore; a deleted tracked file or a rename does not move it. `previewLastConnAt` and `previewOpenConns` are the preview's last connection and the connections open now, from a `zcp-devsup` new enough to report them. `acquiredAt` is when the lease was taken.
- `unsaved`: the release's own check — uncommitted and untracked files (the first 20, all counted) and commits on no remote branch.
- A lease with no activity for 24 h — no change, no preview connection, no acquire — is marked `idle` with `since` (its last activity) and `next`: with nothing unsaved, `zerops_lease action=release lease="<alias>"`; with unsaved work, what it holds and how to keep it (commit and push, or park it), never `force`.
- A preview connection open now is in use, however long ago it opened, so that lease is never idle. A fact that could not be read — an older `zcp-devsup`, an unreachable container — leaves the lease unmarked, never idle.
- The leases of one branch across several pools are idle together or not at all, and `next` names the branch's release, or what each lease holds.
- A full pool's refusal lists the leases zcp holds the same way, oldest activity first, each with what a release would lose.

**Parking**: a lease you will come back to need not run meanwhile. `zerops_lease action=park lease="<alias>"` unmounts it, then stops its container and keeps it reserved with its `/var/www` and `$HOME` (an Ubuntu base empties `/tmp` at every start); every process ends, its dev servers and supervisor included. A stopped container still counts toward the pool's maximum.

- While parked, nothing answers on it: status lists it `parked` with `parkedAt`; `zerops_dev_server`, `zerops_deploy` and `zerops_verify` refuse it, naming the unpark; a sync from it is refused, and a sync from another lease of the branch skips it (`parked`). It keeps its hostname, so the branch's other leases stay wired to it and reach nothing there until it runs again.
- `zerops_lease action=unpark lease="<alias>"` starts the container, waits for SSH, mounts `/var/www/<alias>` and starts the supervisor when none runs, with the branch's sibling values as they now stand; the declared servers start again.
- The platform routes the pool's subdomain to a container started again only once the pool's containers change, so the unpark replaces the pool's warm container (or creates it when the pool has none) and its preview answers again within seconds. In a full pool whose other containers are all held, its preview answers 502 until an acquire that creates a container or a release that removes one; the unpark's note says so.
- A release of a parked lease starts it first so its unsaved work can be read, and stops it again when the release is refused. `force=true` releases it as it stands, stopped.
- Nothing parks a lease by itself: an idle lease with unsaved work is only offered the park.

**Releasing**: Nothing releases, stops or deletes a lease because it is idle — idleness is a label, and a person decides. zcp records no conversation per lease: status and a full pool's refusal list every lease this zcp service holds, other conversations' included. So ask the person before releasing a lease this conversation did not take, and never force one to free a container. Files git ignores (a local `.env`, a SQLite file, build output) and stashes are counted by no release check: they go with any release.

## Zerops Box

**Type**: `ubuntu/zbox@1` (an import may write `zbox@1`, stored as `ubuntu/zbox@1`), in beta. A Box is a single Ubuntu VM, not a container, that runs sandboxes as microVMs. It is not a zerops.yml `run.base`: no build, no deploy, no container counts.
**Region**: a Box can be created only in a project in region prg1. In any other region its create fails with the platform's "not enough hardware capacity to run your container" message and the platform removes the service, so `zerops_import` refuses a Box there before it creates anything, and so does a bootstrap plan that would create one.
**After import**: a Box created by import or in the GUI starts empty — no base and no domain, so a sandbox has nothing to boot from and its ports get no URL. `zerops_import` finishes the setup of each Box it creates before it returns — the `default` base pulled, and the 8081 subdomain added as a sandbox domain when subdomain access is on — and answers one `boxSetup` block per Box; `zerops_box action=setup serviceHostname=<box>` finishes it later (a pull still running, subdomain access turned on afterwards). Outside zcp, `zbox setup` does both (by hand: `zbox base add <image> --name default`, then `zbox workspace domain add <its 8081 subdomain> --form dash`).
**Lifecycle**: a reload restarts only the orchestrator and keeps running sandboxes. A restart, a stop and every resize (CPU and RAM as well as disk) stop the VM: running sandboxes come back `PARKED` without their memory — park the ones to keep first. Disk can only grow. zcp never restarts a Box for an env change.
**Ports**: 8080 serves the API and the web console, 8081 the sandbox ingress. Sandbox port URLs need subdomain access (`enableSubdomainAccess: true`; the GUI form leaves it off) and the 8081 subdomain among the workspace's domains. A project service reaches a sandbox's `project` port at `http://<box>.zerops:8081` with the port's host in the `Host` header.
**API**: other services reach the workspace API at `http://<box>:8080` with the workspace API key as the bearer token. Every route is under `/api/rest/`; only `GET /api/rest/ping` answers without the key, and a key in the query string is refused. Requests that consume capacity — create, fork, park, wake, snapshot, restore or terminate a sandbox, pull a base, create a template — need an `Idempotency-Key` header and answer with an operation: long-poll it with `GET /api/rest/operation/{id}?waitSeconds=30`. A sandbox create requires `networkMode`. Errors carry the HTTP status and `{"error":{"code","message","meta"}}`, e.g. `401 unauthorized`, `409 workspaceFull`. The OpenAPI document is at `/api/rest/swagger/openapi.yml`.
**Limits**: through the public API URL the Zerops edge ends a request after 60 s and refuses uploads over 50 MiB. An exec runs 60 s by default and 10 min at most, with 4 MiB each of stdout and stderr in its answer — start a background process for longer work.
**Keys**: the API key is the Box's `ZBOX_WORKSPACE_API_KEY` — the whole authority over the workspace, with no scopes. An app wires it by reference in its `run.envVariables` — `BOX_API_KEY: ${<box>_ZBOX_WORKSPACE_API_KEY}` — and never copies the value. Editing it rotates it: the new key applies on the Box's next reload (`zerops_manage action=reload`), and until then only the old key works. `ZBOX_WORKSPACE_VOLUME_KEY` unlocks the Box's encrypted data disk and cannot be changed; no app needs it. The Box's other variables (sandbox secrets, integration credentials) apply at once. Box never delivers a `ZBOX_` variable into a sandbox.
**Sandboxes**: no Docker yet; images must be `linux/amd64`; inside a sandbox a project service resolves only as `<host>.zerops`, never by its bare hostname.

## Running code the app's users send

Code the app does not control — a playground's snippets, a grader's submissions, a notebook's cells, an agent's tool calls, plugins — runs in a Box sandbox, never inside the app's own container: namespaces, chroot or a subprocess there still share the container's kernel, its files and its project network. Each sandbox is its own microVM, reads none of the project's values unless they are named for it (*Untrusted code* below), and its network mode decides what it reaches. Import the Box (`ubuntu/zbox@1`) beside the runtime — in a bootstrap plan as a dependency of the runtime (`"resolution": "CREATE"`), or in its own `zerops_import`. It is never a runtime target, and it exists only in a project in region prg1. Every shape below is the workspace API as an app in the project met it (OpenAPI document and live, 2026-10-24).

**Wiring**: the app's zerops.yaml names the API and the key by reference in `run.envVariables`, so the key reaches only the app's process — never copy its value:

```yaml
run:
  envVariables:
    BOX_API_URL: http://<box>:8080
    BOX_API_KEY: ${<box>_ZBOX_WORKSPACE_API_KEY}
```

Every call below goes to `$BOX_API_URL` with `Authorization: Bearer $BOX_API_KEY`; a wrong key answers `401 unauthorized`.

**Base**: a create names its base by digest. `GET /api/rest/base-list` answers `{"items": [...]}`: take the `digest` of the item named `default` whose `status` is `READY`. The name in place of the digest answers `404 baseNotFound`. Read it when the app starts rather than writing it into code: a base pulled again gets a new digest.

**Create**: `POST /api/rest/sandbox` with a new `Idempotency-Key` header and

```json
{"baseDigest": "<digest>", "networkMode": "AIRGAPPED", "integrations": [], "waitSeconds": 30}
```

A missing `Idempotency-Key` or `networkMode` answers `400`. `AIRGAPPED` gives the sandbox no network interface at all — the mode for strangers' code, and exec still works; `INTERNET` adds the internet and no project service. With `waitSeconds` the answer holds the finished operation and the sandbox: `operation.status` is `FINISHED`, `sandboxes[0].id` is the sandbox and `sandboxes[0].status` is `RUNNING`. A sandbox gets 1 vCPU and 512 MiB unless the body sets `cpus` and `memoryBytes` (a multiple of 16 MiB, at least 64 MiB). The same key with the same body replays the first answer; with another body it answers `409 idempotencyKeyConflict`.

**Exec**: `POST /api/rest/sandbox/{id}/exec` with

```json
{"command": ["python3", "-c", "<source>"], "timeoutSeconds": 10, "workingDir": "/workspace"}
```

`command` is argv, run with no shell (wrap pipes and redirects in `sh -c`), and there is no stdin — larger input goes in a file first (`PUT /api/rest/sandbox/{id}/file?path=<path>`, the bytes as the body). It runs as root, in `/` unless `workingDir` names another directory. The answer is `{"sessionId": "…", "exitCode": 0, "stdout": "42\n", "stderr": "", "timedOut": false, "stdoutTruncated": false, "stderrTruncated": false}`. A command past its `timeoutSeconds` is killed and keeps what it printed: `timedOut` is true and `exitCode` is null. A program that does not exist exits `127`. Each of `stdout` and `stderr` is cut at 4 MiB, with `stdoutTruncated` / `stderrTruncated` set. The timeout is 60 s by default and 10 min at most. The `default` base is Ubuntu with Python 3, Node.js, gcc and git; in any sandbox `uname -r` reads 6.18.53 and the hostname is the sandbox's id.

**Terminate**: `DELETE /api/rest/sandbox/{id}` with an `Idempotency-Key` answers its `TERMINATE` operation, which finishes within milliseconds: `GET /api/rest/operation/{id}?waitSeconds=30` returns it `FINISHED`, and the sandbox then reads `TERMINATED`. Terminate in a `finally`: idle and absolute TTLs are not enforced yet, so a sandbox the app forgets keeps running and holds its share of the Box.

**Per run**: for strangers' code, create one sandbox per run and terminate it afterwards — no file or process of one run reaches the next. From an app in the project a create took about 250 ms, an exec tens of milliseconds and a terminate about 10 ms: about 0.3 s a run. A Box as imported (2 CPUs, about 3.25 GiB for sandboxes) holds six default sandboxes at once; the next create answers `409 workspaceFull`, its `metadata` naming the `resource`, `requested` and `free` — tell the user to try again, or give each sandbox less `memoryBytes`.

**Duration**: the 60 s and 50 MiB caps of the Zerops edge apply only through the Box's public API URL. Inside the project, at `http://<box>:8080`, a request lasts as long as its exec: a 70 s exec answered after 70 s.

## Untrusted code

Every container of a project, a pool's included, gets the project's Shared vault values and its own service's (Zerops still injects unreferenced ones today). In a project with `envIsolation: none` it also gets every other service's values as `<host>_KEY`, secrets included — a pool's `<pool>_API_TOKEN`, a database's password, the zcp service's `ZCP_API_KEY`. Code running in a pool container can read all of them. Where that code is not yours (an agent's experiments, users' submissions):

- Keep the project, and every service whose values the code must not read, on `envIsolation: service` (the default): it stops the `<host>_KEY` copies. A service set to `envIsolation: none` hands its values to every other container. Keep secrets out of the Shared vault, which reaches every container either way.
- Run code that must read none of the project's values in a Box sandbox: each sandbox is its own microVM and gets only the variables named for it with `--secret` (`--secrets all` hands it every variable the Box sees, project values included), never a `ZBOX_` one. Its default network mode, `PROJECT_INTERNET`, still reaches every project service, the database included: give untrusted code `INTERNET` or `AIRGAPPED` (a workspace policy's `allowedNetworkModes` enforces it), and hand it API credentials through the credentials proxy (an integration), which keeps the value out of the sandbox. A base built from a Dockerfile runs its `RUN` steps on the Box VM with the project network — build only Dockerfiles you trust.
