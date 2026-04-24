# Dev loop — `zsc noop` + `zerops_dev_server`

Dev and prod containers run different `run.start` commands.

## Dev vs prod process model (dynamic runtimes)

Dynamic runtimes (nodejs, php, go, python, deno, bun):

- **dev** — `start: zsc noop --silent`, NO `healthCheck`,
  `buildCommands` installs deps only. The agent owns the long-running
  process via `zerops_dev_server` so code edits don't force redeploy.
- **prod** — `start: <compiled-entry>` (e.g. `node dist/main.js`) with
  full build chain, `readinessCheck`, `healthCheck`, narrow
  `deployFiles`.

Collapsing the two ("dev auto-starts the compiled entry") breaks tier
0 + 1 iteration — every edit forces redeploy.

## `zerops_dev_server`

`action=start|status|restart|logs|stop`. Call `action=status` first
when uncertain. Fields: `command` (what the app would run), `port`
(dev-slot port), `healthPath` (route that returns 200 when ready).

Never shell-background (`ssh <h>dev "cmd &"`) — backgrounded commands
hold the SSH channel until the 120s timeout fires. The tool detaches
with `ssh -T -n` + `setsid`.

## `deployFiles` on dev

Dev self-deploys require `deployFiles: .` — narrowing to
`[dist, package.json]` wipes the source tree on the next cycle.
Cross-deploys (dev → stage) use narrow lists.

## Implicit-webserver + frontend-bundle case

Implicit-webserver runtimes (`php-nginx`, `php-apache`, `nginx`,
`static`) omit `run.start` on both dev and prod — the `zsc noop` rule
does not apply to the backend. BUT if the codebase compiles a
frontend (Laravel + Vite, Rails + esbuild), the bundler is a
long-running dev process and belongs under `zerops_dev_server` the
same way. Check the frontend before skipping this atom.
