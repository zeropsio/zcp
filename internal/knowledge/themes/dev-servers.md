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
| `servers[].watch` | optional `{"paths": [...], "ignore": [...]}`: the supervisor restarts the server on a change under `paths` (relative to the repo root). Absent: the command's own watcher (`--watch`, vite) does it |
| `servers[].healthPath` | optional, default `/`; the HTTP path a health probe of `port` reads — set it for an API that answers 404 on `/` |
| `servers[].service` | only when several dev services share one code volume: the dev service whose supervisor runs this server |
| `servers[].autostart` | optional, default `true`; `false` keeps a declared server stopped until you start it — declare heavy servers you are not working on this way |

The supervisor ignores keys it does not know.

## Watch and ignore

- A change is a write, create, delete or move. Reading a file, or touching only its access time, restarts nothing.
- `.git` and `node_modules` never restart a server.
- An `ignore` entry is a path from the repo root, matched folder by folder (`*` within one folder, `**` across any number of them); it ignores what it names and everything below it.
- A build output inside a watched folder needs its own entry: `go build -o app` also writes `app-go-tmp-umask` beside `app`, hence `apps/api/app*`; `uv run` rewrites `<pkg>.egg-info` at any depth, hence `**/*.egg-info`; a compiled `target/` or `dist/` folder is `apps/api/target`.
- A server whose every recent start changed its own watched files is no longer restarted by a change made during its start; its last restart reason in `zerops_dev_server action=status` names the path to add to `ignore`. A change after the start still restarts it.
- Give a watched server a plain start command. Never add the runtime's own watcher on top (`node --watch`, `--watch-path`, nodemon) — it goes blind after the first `git checkout`.

## Changing the declaration

- `zerops_dev_server action=start name=... command=... port=...` adds a server or changes its entry; a later start needs only the name.
- After editing the file by hand (watch, ignore, autostart), run `zcp-devsup ctl reload` on the dev service over SSH, or `zerops_dev_server action=restart name=...`: an added server gets its first state, a removed one stops, a changed one restarts if it is meant to run.
- An invalid file changes nothing: the supervisor keeps what runs, starts nothing new, and status reports the error.

## Statuses and crashes

- `running`; `starting` — its port does not accept connections yet; `stopped`; `crashed` — with its exit.
- A server that crashes while starting (within 10 s of its start, or before its port first accepts) is started once more after its siblings settle; status says it comes back. Check `zerops_dev_server action=status` again before fixing anything.
- A second crash, or a crash after the server listened, stays down until you start it again or a watched change restarts it.
- A redeploy starts every `autostart` server; a container restart starts every server that was meant to run.
- Each server has its own log: `zerops_dev_server action=logs name=...`.

## The task limit

- Every dev server on a dev service, and the supervisor itself, share one task limit: on Zerops 300 processes and threads together (the runtime unit's systemd default). A fork past it fails with `EAGAIN` — `resource temporarily unavailable`, `os error 11`.
- A server that crashed, or could not start, because the limit was reached is started again after a backoff, one server at a time, up to 6 times; that is not its one more start. Servers started together start 1 s apart. Status names the limit and how many tasks run now.
- Tools that size thread pools by CPU count can reach it alone. Cap them in the server's `command`: `RAYON_NUM_THREADS=4` (uv, ruff and other Rust tools), `UV_CONCURRENT_BUILDS=2 UV_CONCURRENT_INSTALLS=2` (uv), `--child-concurrency=2` (pnpm install). The supervisor sets none of these for you.
- Stop servers you are not using: each one's processes and threads count against the same limit.
