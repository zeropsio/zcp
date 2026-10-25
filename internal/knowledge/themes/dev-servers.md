# Dev Servers — the zcp-devsup supervisor, .zcp/dev-servers.json, watch and ignore

The dev servers of a dev service run under `zcp-devsup`, the supervisor its dev setup's `run.start` runs (`start: zcp-devsup run`). They are declared in `.zcp/dev-servers.json` beside `zerops.yaml` in the repo root — commit it with the code. `zerops_dev_server` writes the file on a server's first start and drives the supervisor: start, stop, restart, status, logs. Never start, find or kill a dev-server process yourself.

## The declaration

```json
{
  "version": 1,
  "servers": [
    { "name": "web", "port": 3000, "command": "bun --watch index.ts", "cwd": "apps/web" },
    { "name": "api", "port": 8080, "command": "go build -o app . && exec ./app", "cwd": "apps/api",
      "watch": { "paths": ["apps/api"], "ignore": ["apps/api/app*"] }, "healthPath": "/health" },
    { "name": "worker", "command": "bun src/worker.ts", "cwd": "apps/worker" },
    { "name": "admin", "port": 4200, "command": "npx ng serve --host 0.0.0.0 --port 4200", "cwd": "apps/admin",
      "autostart": false }
  ]
}
```

| Key | Meaning |
|---|---|
| `version` | required; `1` |
| `servers[].name` | required, unique; lowercase letters, digits and dashes, starting with a letter, up to 40 characters. It is the `name` `zerops_dev_server` takes |
| `servers[].port` | optional, unique in the file; absent for a server without a listener (a worker) |
| `servers[].command` | required; runs as `/bin/sh -c <command>` in `cwd`, with the service's env vars, as its own process group |
| `servers[].cwd` | optional, default `.`; relative to the repo root and must stay inside it |
| `servers[].watch` | optional `{"paths": [...], "ignore": [...]}`: the supervisor restarts the server on a change under `paths` (relative to the repo root). Absent: the command's own watcher (`--watch`, vite) does it. Either way the server reloads by itself, so a lease sync never asks you to restart it |
| `servers[].healthPath` | optional, default `/`; the HTTP path a health probe of `port` reads — set it for an API that answers 404 on `/` |
| `servers[].service` | optional; whose supervisor runs this server: in a repository whose parts are each a dev pool, the part's pool (an entry naming none runs in every part's lease); when several dev services share one code volume, the dev service |
| `servers[].autostart` | optional, default `true`; `false` keeps a declared server stopped until you start it |
| `servers[].preview` | optional `true`; marks the server `previewPortBase` forwards to when more than one server listens (with one listening server, that one). It needs a `port`; at most one entry carries it |

The top-level keys below are read in a dev pool's lease only — a dev service ignores them (`zerops_knowledge uri="zerops://themes/pools-and-box"`, section "A part's dev pool"):

| Key | Meaning |
|---|---|
| `previewPortBase` | optional port number. The supervisor forwards `previewPortBase + ZEROPS_StableNumber` to the preview server's port, so each lease answers on its own per-port subdomain — the dev-server result's `previewUrl`. `ZEROPS_StableNumber` starts at 1, so the pool's `run.ports` declares `previewPortBase + 1` through `previewPortBase + maxContainers` with `httpSupport: true`. The number is the container's, not the branch's: a new lease of the part can answer on another port, so read `previewUrl` from each lease |
| `siblingEnv` | optional `{"<VAR>": "<pool>"}`: a variable a part's dev servers read, and the pool whose lease of the same branch it reaches. Every server started in a lease gets `<VAR>` = that lease's private container hostname, `node-id-<n>.runtime.<pool>.zerops` (its own for its own pool); the app adds `http://` and the port. It serves server-side calls only — a dev proxy, server rendering, one service calling another. A pool the branch holds no lease on leaves `<VAR>` to the container's own env value. Declare a variable only when a dev server actually reads it: every lease of that pool that joins or leaves the branch restarts the dev servers running in the branch's other leases, read or not — each restart leaves a heavy dev server down or slow until it has started again. A name is never `ZEROPS_*`, `hostname`, `serviceId`, `projectId`, `PATH` or `HOME` |
| `siblingPreviewEnv` | optional `{"<VAR>": "<pool>"}`, top level: a variable for code that runs in the browser, and the pool whose lease of the same branch it reaches. Every server started in a lease gets `<VAR>` = that lease's preview URL (`https://`, the pool's subdomain on `previewPortBase` + that lease's container's `ZEROPS_StableNumber` — the `previewUrl` its dev-server result reports), resolved again whenever a lease of the branch joins or leaves: the port follows the container, so never copy it by hand. `siblingEnv` gives the container hostname, for server-side calls. A name is in one of the two, never both, and follows `siblingEnv`'s name rules; declare it only when a dev server reads it, for the same restarts. Without a lease of the branch on that pool, or without `previewPortBase`, `<VAR>` keeps the container's own env value, and the start result lists it in `leftToContainerEnv` |
| `siblingEnv` and a browser client | a browser cannot resolve the private hostname `siblingEnv` hands out: for a page that calls a sibling part from the browser, name the variable in `siblingPreviewEnv` instead |
| `prepare` | optional shell command: the incremental install a branch's own lockfile needs on top of the dependencies the pool's image baked in (e.g. `npm install --prefer-offline`). An acquire runs it in the repo root after the checkout, before the declared servers start, and reports it; a failed one leaves the lease held. The supervisor never runs it |

A dev service with more than one heavy server (Angular, Analog, Next) declares the ones not being worked on `"autostart": false`; two such servers plus a deploy can exhaust the service's RAM and wedge it. `zerops_dev_server action=start hostname=... name=...` refuses a server that would not fit and names the numbers.

The supervisor ignores keys it does not know.

## Watch and ignore

- A change is a write, create, delete or move. Reading a file, or touching only its access time, restarts nothing.
- `.git` and `node_modules` never restart a server.
- An `ignore` entry is a path from the repo root, matched folder by folder (`*` within one folder, `**` across any number of them); it ignores what it names and everything below it: `apps/api/dist`, `**/*.egg-info`.
- A build output inside a watched folder needs its own entry: `go build -o app` also writes `app-go-tmp-umask` beside `app`, hence `apps/api/app*`; `uv run` rewrites `<pkg>.egg-info` at any depth, hence `**/*.egg-info`; a compiled `target/` or `dist/` folder is `apps/api/target`.
- A server whose every recent start changed its own watched files is no longer restarted by a change made during its start; its last restart reason in `zerops_dev_server action=status hostname=... name=...` names the path to add to `ignore`. A change after the start still restarts it.
- The supervisor watches a declared dev server through its `watch.paths` (plus `ignore`): give it a plain start command. Never add a runtime's own watcher on top (`node --watch`, `--watch-path`, nodemon) — it goes blind after the first `git checkout`.

## Changing the declaration

- `zerops_dev_server action=start hostname=... name=... command=... port=...` adds a server or changes its entry; a later start needs only the name.
- After editing the file by hand (watch, ignore, autostart), run `zcp-devsup ctl reload` on the dev service over SSH, or `zerops_dev_server action=restart hostname=... name=...`: an added server gets its first state, a removed one stops, a changed one restarts if it is meant to run.
- An invalid file changes nothing: the supervisor keeps what runs, starts nothing new, and status reports the error.

## Statuses and crashes

- `running`; `starting` — its port does not accept connections yet; `stopped`; `crashed` — with its exit.
- A server that crashes while starting (within 10 s of its start, or before its port first accepts) is started once more after its siblings settle; status says it comes back. Check `zerops_dev_server action=status hostname=... name=...` again before fixing anything.
- A second crash, or a crash after the server listened, stays down until you start it again or a watched change restarts it.
- A redeploy starts every `autostart` server; a container restart starts every server that was meant to run.
- Each server has its own log: `zerops_dev_server action=logs hostname=... name=...`.
- A server that listens on 127.0.0.1 only is reachable only inside its container: start and status say it listens on loopback only and give no address. Bind it to 0.0.0.0 (`--host 0.0.0.0` for Vite and similar), unless a web server in front of it proxies to it.

## The task limit

- Every dev server on a dev service, and the supervisor itself, share one task limit: on Zerops 300 processes and threads together (the runtime unit's systemd default). A fork past it fails with `EAGAIN` — `resource temporarily unavailable`, `os error 11`.
- A server that crashed, or could not start, because the limit was reached is started again after a backoff, one server at a time, up to 6 times; that is not its one more start. Servers started together start 1 s apart. Status names the limit and how many tasks run now.
- Tools that size thread pools by CPU count can reach it alone. Cap them in the server's `command`: `RAYON_NUM_THREADS=4` (uv, ruff and other Rust tools), `UV_CONCURRENT_BUILDS=2 UV_CONCURRENT_INSTALLS=2` (uv), `--child-concurrency=2` (pnpm install). The supervisor sets none of these for you.
- Stop servers you are not using: each one's processes and threads count against the same limit.
