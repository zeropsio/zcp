# api-codebase-addendum

You scaffold an API codebase. Production dependencies include the framework, the ORM, and every managed-service client named in `EnvVarsByKind`. You write HTTP infrastructure plus migrations and seeds; feature routes are out-of-scope.

## Files you write

- **Framework entrypoint** (`src/main.ts` or equivalent). Four invariants apply, every one satisfied via the framework's idiom:
  - **Routable bind** — the HTTP server listens on `0.0.0.0`, not `localhost` or `127.0.0.1`. The L7 balancer routes to the container's pod IP; loopback is unreachable.
  - **Trust proxy** — the framework's proxy-trust setting is enabled for exactly one upstream hop so `req.protocol`, `req.ip`, and forwarded-for headers reflect the client rather than the balancer.
  - **Graceful shutdown** — the app enables shutdown hooks on SIGTERM (`app.enableShutdownHooks()` for NestJS, equivalent idiom elsewhere). Rolling deploys give the process thirty seconds to stop accepting new work, drain in-flight work, close long-lived connections, and exit.
  - **Logger** — structured logger wired at bootstrap so the feature sub-agent and the deploy sweep see consistent log output.

- **Health route** — `GET /api/health` returns `{ ok: true }` with `Content-Type: application/json`. No service calls; this is the liveness probe.

- **Status route** — `GET /api/status` returns a flat JSON object with one string value per managed service: `{ db: "ok" | "error", cache: ..., queue: ..., storage: ..., search: ... }`. Keys match `EnvVarsByKind` kinds in scope. Each value comes from a try/catch-wrapped live ping; the endpoint returns 200 regardless so the dashboard UI observes degraded state. This is not the platform readiness probe — it is the UI's health view.

- **CORS** — the origin allow-list is the frontend's dev + stage hostnames from `Hostnames` (roles `app` or `frontend`), not `origin: *`. The api handles CORS; the frontend does not.

- **Service clients** — one init per kind in `EnvVarsByKind`. Every client reads env var names from the contract. Every client has a destructor / shutdown hook that closes its connection. S3 clients use `endpoint = storage_apiUrl` (https), pass user and pass as structured credentials, and set `forcePathStyle: true`. NATS clients pass `user` and `pass` as separate `connect()` options (never `nats://user:pass@host` URLs); Redis/Valkey connections carry no password segment.

- **Entities + DB schema** — TypeORM / active-record / equivalent entities for the primary data model. `synchronize: false` on the datasource — you write an explicit migration script.

- **Migrate script** (`src/migrate.ts` or equivalent). Standalone DataSource script that creates tables idempotently. On error: log and `process.exit(1)`. The explicit non-zero exit is the loud-failure contract — init scripts must surface failure to `execOnce` rather than swallowing into `console.error`.

- **Seed script** (`src/seed.ts` or equivalent). Seeds 3–5 demo rows when the primary table is empty. When a search engine is in scope, always push all rows to the search index (await the completion task; do not short-circuit on row count — row-count guards hide async-durable sibling work and ship a silent gotcha). Idempotency comes from the execOnce key declared in the later-written zerops.yaml, not from a row-count guard.

- **`.env.example`** — every env var your code reads, keyed by `EnvVarsByKind` plus framework mode flags.

- **`.gitignore`** — `node_modules`, `dist`, `.env`, `.DS_Store`, plus framework-specific cache directories.

## Files you do NOT write at this substep

- `README.md` — authored later at deploy.readmes. Delete any README the framework scaffolder emits.
- `zerops.yaml` — authored at a later substep after your scaffold returns.
- `.git/` — remove via `ssh {{.Hostname}} "rm -rf /var/www/.git"` after the scaffolder returns (unless you passed `--skip-git`).
- Feature routes (items, cache, jobs, storage, search, mail) and any UI or worker handlers. The feature sub-agent owns those as a single unit.
