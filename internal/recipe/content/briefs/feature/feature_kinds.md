# Feature kind catalog

A showcase recipe proves the Zerops platform surface by implementing one
feature of each kind. You pick the specific framework idiom; what each
kind MUST prove is typed below.

## crud
One end-to-end resource with `list + create + show + update + delete`
exposed over HTTP. Proves the database connection, schema migration,
JSON request/response handling, and URL routing. State persists across
container restarts.

## cache-demo
One endpoint that demonstrates cache-hit vs cache-miss timing. Emits a
header or body field exposing which path served the response (e.g.
`X-Cache: HIT`). Proves the cache service is reachable, auth-free (for
Valkey), and survives container restarts.

## queue-demo
One endpoint that enqueues a job; a worker (possibly in a separate
codebase) consumes and records the result. Proves the broker connection,
queue group semantics, and worker SIGTERM drain.

## storage-upload
One endpoint that uploads a file to object storage and returns a URL
that retrieves it. Proves `forcePathStyle: true` routing, credential
injection, and private-policy signed-URL generation.

## search-items
One endpoint that full-text-searches the resource `list` introduced in
`crud`. Proves the search service integration (index build on seed, per-
request query, result ranking).

# Scaffold symbol table

The scaffold sub-agent(s) produce a symbol table that feature work reads
so naming stays consistent. Expected fields:

- `codebases[].hostname` — the service hostname each codebase deploys to
- `codebases[].entities` — the resource names (e.g. `Item`, `Job`) + their
  database table names
- `codebases[].endpoints` — path prefix + handler entry-point file
- `services[]` — managed service hostname + env-var prefix (`db_*`,
  `cache_*`, `broker_*`, `storage_*`, `search_*`)

Features extend existing code; features do not introduce new codebases or
new managed services beyond what `Plan.Services` declares.

# Record facts during implementation

Same fact schema as scaffold (see decision_recording_slim.md for the canonical schema). Feature-phase facts
commonly surface: platform × library intersections (broker client quirks),
cross-service contract mismatches (worker reading from broker but writing
back via HTTP), and object-storage path-style nuances.
