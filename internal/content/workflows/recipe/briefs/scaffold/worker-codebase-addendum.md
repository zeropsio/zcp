# worker-codebase-addendum

You scaffold a worker codebase: a standalone long-running process that subscribes to a message broker and processes work. No HTTP listener. You write infrastructure only; real job handlers are feature-sub-agent territory.

## Files you write

- **Framework bootstrap** (`src/main.ts` or equivalent). A standalone application context — no HTTP server is started. The bootstrap enables graceful shutdown hooks on SIGTERM so the process has thirty seconds to drain the subscription, drain the broker connection, release other long-lived resources, and exit. The bootstrap calls the worker's `start()` and logs a readiness line once the subscription is live; the outer `.catch` on bootstrap prints the error and exits non-zero so container-start failures surface to the platform readiness probe.

- **Worker service** — one module that subscribes to every subject named in `NATSSubjects` this worker handles at this substep. Every subscription declares the queue name from `NATSQueues` as the queue-group option (competing-consumer semantics: with `minContainers > 1`, each message is processed once across replicas, not N times). The NATS client is initialized with user and pass as separate `connect()` options — the `servers` list carries only `${queue_hostname}:${queue_port}`; URL-embedded credentials silently drop in current NATS client builds. The subscription loop wraps each message handler in try/catch so a single malformed message does not crash the entire subscription.

- **Shutdown hook** — `onModuleDestroy` (or the framework's equivalent destructor). Sequence: drain the subscription, drain the broker connection, close any other long-lived clients. The hook is awaited; a synchronous registration that never awaits the drain is caught by the close-step code review.

- **Shared entities** — when the worker shares the database with an api role, import or mirror the entities the api scaffold produced. Schema alignment comes from the contract — the api role owns the migration; the worker never invents worker-only columns.

- **`.gitignore`** and **`.env.example`** — same baseline as other scaffold dispatches in this run.

## Principles that apply

Graceful shutdown, competing consumer, and structured credentials are load-bearing for the worker role. Routable bind, trust proxy, and stripped build root do not apply (no HTTP listener, no static-deploy surface). Record a fact after satisfying each applicable principle so the writer captures the framework idiom you used.

## Files you do NOT write at this substep

- `README.md` — authored later at deploy.readmes.
- `zerops.yaml` — authored at a later substep after your scaffold returns.
- `.git/` — deleted post-scaffold (the `skip-git` contract rule).
- Real job handlers — the feature sub-agent owns the actual subject-to-business-logic mapping for items-sync, mail-dispatch, search-reindex, and similar.
