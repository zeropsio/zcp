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

### No-HTTP workers — the `port=0 healthPath=""` carve-out

Worker codebases (NestJS standalone application context, queue
consumers, batch jobs) have NO HTTP surface — no `ports:` block in
the scaffolded yaml, no readiness route. The `port` and `healthPath`
fields still apply; pass them as `port=0 healthPath=""`:

```
zerops_dev_server action=start hostname=<worker>dev \
  command="npm run start:dev" \
  port=0 \
  healthPath=""
```

The dev container still has `start: zsc noop --silent`. The worker
process is owned by `zerops_dev_server` the same as any HTTP
runtime — code edits trigger a watcher rebuild, the platform doesn't
redeploy.

**Verification of running:** worker has no `/health` to curl, so
liveness comes from logs. Tail with `zerops_logs serviceHostname=<worker>dev`
and watch for the framework's "started" / "subscribed" line and the
worker's first heartbeat / consume / processing entry.

**Mandatory fact attestation.** After the dev-server tool returns
success AND a successful log tail confirms running, record:

```
record-fact topic=worker_dev_server_started kind=porter_change \
  scope=<worker>dev/runtime \
  why="Worker process owned by zerops_dev_server; code edits don't force redeploy." \
  candidateClass=scaffold-decision \
  candidateSurface=CODEBASE_ZEROPS_COMMENTS
```

(`scaffold-decision` + `CODEBASE_ZEROPS_COMMENTS` per the
classification × surface table — the dev-server invocation is a
recipe-internal scaffold decision; if any prose surfaces it at all,
it surfaces as a `# zsc noop` block-comment rationale on the dev
yaml's `start:` field.)

The scaffold complete-phase gate refuses the phase if any dev
codebase with `start: zsc noop --silent` lacks this attestation.
Bypass is intentional: a `worker_no_dev_server` fact with reason
suppresses the requirement for one-shot batch codebases that don't
need a watcher loop.

Skipping `zerops_dev_server` for a worker because "the tool's port
field doesn't apply" was the run-19 trap — the worker process never
actually ran on dev, behavior was attested only on workerstage's
compiled entry. The `port=0 healthPath=""` invocation is the
canonical worker dev loop.

## `deployFiles` on dev

Dev self-deploys require `deployFiles: .` — narrowing to
`[dist, package.json]` wipes the source tree on the next cycle.
Cross-deploys (dev → stage) use narrow lists.

## Watcher PID volatility (nest, etc.)

`nest start --watch` rotates its child process on every rebuild. A
pidfile captured at first start is stale after any source-change
rebuild — `kill -0 <pid>` against the saved pid returns false even
though the watcher is healthy. Use the listening port (`netstat -lnt`,
`ss -lnt`, or the dev-server's status endpoint) for liveness, not a
saved pidfile.

Other watch-loop dev servers (vite, webpack-dev-server) generally keep
a stable parent process — pidfile-based liveness works there. The
nest-watcher pattern is the outlier; treat watcher PIDs as ephemeral
by default unless the framework documents otherwise.

## Implicit-webserver + frontend-bundle case

Implicit-webserver runtimes (`php-nginx`, `php-apache`, `nginx`,
`static`) omit `run.start` on both dev and prod — the `zsc noop` rule
does not apply to the backend. BUT if the codebase compiles a
frontend (Laravel + Vite, Rails + esbuild), the bundler is a
long-running dev process and belongs under `zerops_dev_server` the
same way. Check the frontend before skipping this atom.
