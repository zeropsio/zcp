# App-code — showcase dashboard skeleton

Showcase generates a skeleton dashboard only. Feature controllers and views are authored later by the feature sub-agent at the deploy step, so the code that lands here has one job: prove every managed service in `plan.Research.Targets` is reachable. No feature sections, no forms, no tables, no demos.

## What the scaffold writes for showcase

| Area | Contents |
|---|---|
| Frontend entry | `App.svelte` / equivalent — renders `<StatusPanel />` and nothing else |
| Status panel component | Polls `GET /api/status` every 5s; renders one row per managed service (db, redis, nats, storage, search, …) with a colored dot (green / red / yellow) and the service name — no buttons, no forms, no other state |
| API health route | `GET /api/health` — liveness probe returning `{ ok: true }` |
| API status route | `GET /api/status` — deep connectivity check returning `{ db: "ok", redis: "ok", nats: "ok", storage: "ok", search: "ok" }` with one key per managed service in the plan; pings each service |
| Service clients | Client initialization for every managed service in the plan — TypeORM datasource, Redis client, NATS connect, S3 client, Meilisearch client — imported and configured from env vars named by `plan.SymbolContract.EnvVarsByKind`, without demo routes against them |
| Migrations | Full schema for the primary data model; the feature sub-agent adds endpoints that query it |
| Seed data | 3 to 5 rows of sample data; the feature sub-agent expands the seed when it builds the features that need more |
| Worker (separate codebase) | NATS connect plus one no-op subscriber that logs received messages — no processing, no DB writes, no result storage |
| `zerops.yaml`, `.env.example` | Per the other substep atoms |

## What stays out of the scaffold for showcase

Item CRUD routes, cache-demo routes, search routes, jobs-dispatch routes, storage upload routes, and the frontend components that consume them all belong to the feature sub-agent at deploy. That sub-agent authors API, frontend, and worker changes as one coherent unit so the cross-codebase contract stays consistent.

The scaffold ships a visibly empty dashboard — one green-dot panel — so the browser walk at deploy is meaningful: either every dot is green or it is not. README fragments are not written at this substep; they land at the `readmes` substep of the deploy phase after stage verification.
