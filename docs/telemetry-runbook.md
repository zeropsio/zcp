# Telemetry Runbook — operating the ZCP usage-telemetry pipeline

Design contract: `docs/spec-telemetry.md` (this runbook does not repeat
behavior the spec already pins — it's the *operator* view: what to do, in
what order, and what to expect when something goes wrong). Infra bundle:
`../zcp-telemetry/` (project import config + deploy config copy).

Every claim below cites the file it was verified against. If code and this
doc ever disagree, the code wins — file an update here.

## 1. Deploy order — ingest BEFORE client, always

**Rule: redeploy `cmd/zcp-ingest` first, release the `zcp` client second.
Never the other way round.**

Why: the wire schema is versioned (`wire.SchemaVersion`,
<!-- verified: internal/telemetry/wire/wire.go:22, const SchemaVersion = 1 --> currently
`1`) and ingest's `POST /v1/events` 202 response carries
`max_schema_version` — the version *this* ingest currently understands
<!-- verified: internal/ingest/handler.go:198-201, resp.MaxSchemaVersion = wire.SchemaVersion -->.
A client whose own `wire.SchemaVersion` is newer than the server's
advertised value stderr-warns **once per process**
<!-- verified: internal/telemetry/client.go:498-519, checkSchemaWarning + schemaWarnOnce -->:

```
telemetry: ingest supports schema_version up to N, this client sends M —
deploy ingest before releasing this client version
```

This is a *visibility* guard, not a safety net — a schema-version mismatch
does not itself corrupt data (tolerant validation, §2 below, keeps the
version-1 core working either direction), but it is the signal that the
release order was gotten wrong, so treat the warning as "redeploy ingest
now."

Deploy mechanics (from the zcp repo root, where `zerops.yml` for the
`ingest` setup lives): `zcli push --project-id <id> --service-id <ingest-id>
--setup ingest -w all`. First start after a schema-affecting change
bootstraps/migrates automatically (§2) — watch `zcli service log ingest`
for `"ingest: schema bootstrap applied"` and any `"ingest: migration
applied"` lines
<!-- verified: internal/ingest/bootstrap.go:122, migrate.go:153 -->.

## 2. Migration flow

`internal/ingest/migrations/*.sql` holds ordered, embedded, idempotent
ClickHouse DDL steps, named `NNNN_description.sql` (4-digit id)
<!-- verified: internal/ingest/migrate.go:17-34 -->. `telemetry.schema_migrations
(id UInt32, applied_at DateTime64(3))` ships as part of the *base* schema
itself (it must exist before the migration runner can query it)
<!-- verified: internal/ingest/schema.sql -->.

Startup sequence, every ingest boot with `CH_SUPER_PASSWORD` set
<!-- verified: internal/ingest/server.go:29-36, bootstrap.go:111-127 -->:

1. `bootstrapSchema` applies the base `schema.sql` (`CREATE ... IF NOT
   EXISTS` — safe on every restart, never alters an existing object).
2. Once that succeeds, `applyMigrations` loads the embedded migrations,
   queries `telemetry.schema_migrations` for ids already recorded, and
   executes only the pending ones **in id order** — each migration's DDL
   runs, THEN its id is recorded. A failed DDL statement is never recorded,
   so a retry (next restart, redeploy, or an HA replica) picks it up from
   scratch instead of skipping it as done
   <!-- verified: internal/ingest/migrate.go:123-156 -->.

**Restart-race is dead**: because migrations are id-tracked, a re-run after
a restart is a no-op — there is no more "deploy the new binary before
dropping the objects you expect it to recreate" dance. To add a schema
change: write a new `internal/ingest/migrations/000N_description.sql` with
idempotent DDL (`ADD COLUMN IF NOT EXISTS`, etc. — the runner's bookkeeping
guards against re-running an *already-recorded* migration, not against two
concurrent appliers racing the same *unrecorded* one; acceptable because v1
runs a single ingest replica), then redeploy ingest. No manual `ALTER`
against ClickHouse, ever.

Every migration statement is written against the `__DB__` token (same
convention `schema.sql` uses) — `renderMigrationDB` substitutes it before
exec, because the admin connection carries no default database
<!-- verified: internal/ingest/migrate.go:88-106 -->.

**New dims vs. new columns** — most schema growth does NOT need a
migration at all: a new `dims` key/value is a `wire` allowlist edit +
ingest redeploy (dims is `Map(LowCardinality, LowCardinality)` — no DDL).
Only a genuinely new top-level column (like `error_subcode`,
`migrations/0002_add_error_subcode.sql`) needs a migration file
<!-- verified: internal/ingest/migrations/0002_add_error_subcode.sql, docs/spec-telemetry.md §8 -->.

## 3. Blocklist use (R5 ops lever)

`INGEST_BLOCK_IPS` / `INGEST_BLOCK_INSTALLS` — comma-separated, trimmed,
blank entries dropped — env vars on the `ingest` service
<!-- verified: internal/ingest/config.go:86-87, blocklist.go -->.

- A blocked IP 403s the request **before the body is even read** (blocklist
  check is the handler's very first step).
- A blocked `install_id` 403s as soon as it's seen inside a batch — any
  rows the request already legitimately validated/accepted before that
  point are still queued to the batcher (same discipline as the abuse
  circuit breaker below), only the rest of that request is refused
  <!-- verified: internal/ingest/handler.go:116-159 -->.
- Matched values are **never logged** (same discipline as client IP — B6).
- **No dynamic reload.** Editing the env var requires a redeploy/restart of
  `ingest` to take effect — there is no admin API or file watch.
- This is the kill switch for a single bad actor; the kill switch for the
  *entire* endpoint is disabling the ingest subdomain (§5 below), not a
  blocklist entry.

## 4. Outage behavior — what breaks, what recovers, what's lost

### ClickHouse down / unreachable

- **Ingest keeps accepting HTTP 202** — accept/reject/dedup/validation all
  happen before the row ever reaches the batcher; a downed CH does not
  surface as a client-visible error at all.
- Rows queue in the batcher's in-memory buffer, flushed on a 5k-row/2s
  timer. A failed `Insert()` call requeues its **entire attempted batch**
  for the next flush (prepended ahead of freshly buffered rows), bounded to
  a 50,000-row retry buffer — beyond that, **oldest rows are dropped** to
  make room, counted (never silent)
  <!-- verified: internal/ingest/batcher.go:116-158 -->.
- **What's lost**: nothing, as long as CH recovers before the retry buffer
  fills. At sustained full ingest volume this buffer is a matter of
  minutes, not hours — an extended CH outage WILL drop rows past that
  point. Watch `/statsz`'s `rows_dropped_total` (§6) during an incident.
- Recovery is automatic — no operator action needed once CH is back;
  the next flush drains the retry buffer.

### Ingest down / unreachable (client side)

- The client's single worker goroutine classifies every send attempt:
  `4xx (400/413)` → **drop the batch permanently, never retry** (structural
  garbage — retrying would just repeat the rejection);
  `429/5xx/timeout/network error` → **spool + exponential backoff** (5s →
  5min, full jitter)
  <!-- verified: internal/telemetry/client.go:432-496, jitteredBackoff:529-550 -->.
- Spooled batches live at `~/.zcp/telemetry/spool-v1/` (gzip JSONL
  segments), bounded to **10 MiB total or 7 days**, oldest dropped first;
  a corrupt segment is renamed `*.bad` and deleted at next startup
  <!-- verified: internal/telemetry/spool.go, docs/spec-telemetry.md §5.4 -->.
- Draining is bounded — one oldest segment per periodic tick (every 5s,
  once the worker is past startup jitter) plus one best-effort attempt at
  process Shutdown, so it never starves the live queue
  <!-- verified: internal/telemetry/client.go:383-410, 326-334 -->.
- **What's lost**: nothing for a transient outage (spool absorbs it, drains
  automatically once ingest recovers). For an outage longer than 7 days
  (or on a machine emitting fast enough to exceed 10 MiB of spool), the
  oldest events are dropped, silently from the user's point of view but
  never from ZCP's — `DroppedCount()` folds spool/queue/storm drops into
  every `session_end`'s `dropped_count` field.
- Telemetry NEVER blocks or fails a tool call regardless of how broken
  ingest is (spec B3) — this is a hard invariant pinned by the S1 chaos
  test table, `internal/telemetry/chaos_test.go`.

### Client crash / SIGKILL

No durability promise — anything still in the in-memory queue or the
current in-flight batch at the moment of a SIGKILL is lost; only a graceful
shutdown (SIGINT/SIGTERM/normal exit) gets the 750ms flush window
<!-- verified: cmd/zcp/main.go:34 telemetryShutdownTimeout, docs/spec-telemetry.md §5.5 -->.

## 5. Reading `/statsz`

`GET /statsz` on the ingest service returns the ops-counters payload the
pipeline-health dashboard reads — **counts only, never row/event content**
<!-- verified: internal/ingest/handler.go:35-43, 95-106 -->:

```json
{
  "events_accepted_total": 0,
  "events_rejected_total": 0,
  "server_errors_total": 0,
  "rows_dropped_total": 0,
  "insert_failures_total": 0,
  "dims_dropped_total": 0,
  "forward_compat_accepted_total": 0
}
```

| field | non-zero means | where it comes from |
|---|---|---|
| `events_accepted_total` | events accepted into the pipeline — the primary launch health signal (should climb with real traffic) | `Handler.eventsAcceptedTotal` |
| `events_rejected_total` | schema-rejected events — a spike means real traffic is failing validation (the "no schema rejects" launch gate) | `Handler.eventsRejectedTotal` |
| `server_errors_total` | ingest responses ≥500 — should stay **0** (the "no 5xx storms" gate); the handler returns no 5xx by design | `Handler.serverErrorsTotal` |
| `rows_dropped_total` | the retry buffer overflowed during a CH outage (§4) | `batcher.rowsDroppedTotal` |
| `insert_failures_total` | a `ClickHouse.Insert()` call failed (count of *attempts*, not rows) | `batcher.insertFailuresTotal` |
| `dims_dropped_total` | a client sent a dims key/value this ingest doesn't recognize — normal during a rolling client upgrade, not itself a bug (§2/tolerant validation) | `Handler.dimsDroppedTotal` |
| `forward_compat_accepted_total` | a client's `schema_version` is newer than this ingest's — the release-ordering guard's server-side counterpart to the client's stderr warning (§1) | `Handler.forwardCompatAcceptedTotal` |

`GET /healthz` is a separate, simpler endpoint (`{"status":"ok"}` or
`"degraded"` on a failed best-effort CH ping, cached ≤1 ping per 5 s behind an
in-flight guard so a flood can't stampede ClickHouse) — it does not carry these
counters. Unlike `/statsz` (per-IP rate-limited), `/healthz` is left unlimited
so platform health probes always get a fast answer.

## 6. The binary-wipe-on-restart caveat for the `zcp` test container

The `zcp-telemetry` project carries a `zcp` service used to exercise a
locally-built ZCP binary against the live pipeline
<!-- verified: ../zcp-telemetry/README.md "zcp test service" note -->.
That binary is **not** deployed through Zerops' normal build pipeline — it
is pushed by `eval/scripts/build-deploy.sh`: `scp` to a tmp path, then
`sudo install` into `/usr/local/bin/zcp` over SSH
<!-- verified: eval/scripts/build-deploy.sh:27-30 -->. This is a manual,
out-of-band filesystem write, not a `zerops_deploy`/`buildFromGit` artifact.

Because the service was provisioned `startWithoutCode: true` and has never
gone through a real deploy, its "last deployed state" is empty. If the
container is recreated by any Zerops-side event (redeploy, a scale-to-zero/
scale-up cycle, a platform-triggered restart) rather than a plain in-place
process restart, the manually-installed binary is gone and `/usr/local/bin/
zcp` reverts to whatever (nothing) the pipeline actually deployed.

**Practical rule**: after ANY restart of the `zcp` test container, re-run
`./eval/scripts/build-deploy.sh` before trusting a session against it —
don't assume the binary you scp'd last week is still there. This is a
property of `startWithoutCode: true` + manual-install services generally,
not something specific to telemetry — but it bites here because the whole
point of that container is to exercise telemetry end-to-end against a real
binary.

## 7. Erasure requests

A user can request deletion via `zcp telemetry id` (prints the install
UUID, the erasure key)
<!-- verified: internal/telemetry/cli.go:123-133, docs/spec-telemetry.md §3.5 -->:

```sql
ALTER TABLE telemetry.events DELETE WHERE install_id = '<uuid>';
```

Run as `super` against `db:8123` over VPN. Aggregates (`tool_daily`,
`workflow_daily`) are not keyed by `install_id` at row grain and are not
individually erasable this way — they are retained indefinitely by design
(spec §7) since they carry no per-install identity once aggregated.

The operator process must respond to an erasure or access request within
the statutory ~1-month window (GDPR Art 12(3)). v1 advertises no shorter
voluntary SLA in product copy (`zcp telemetry disclosure`,
`docs/telemetry-disclosure.md`) — but the internal handling process above
is time-bounded to that statutory month; see `docs/telemetry-lia.md` for
the accountability record this commitment is drawn from.
