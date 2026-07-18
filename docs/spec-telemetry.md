# spec-telemetry — ZCP Anonymous Usage Telemetry

Status: DESIGN CONTRACT (implementation in progress; plan:
`plans/telemetry-implementation-plan-2026-07-02.md`, PRD:
`plans/prd-telemetry-2026-07-02.md`). This spec is the single authority for
telemetry behavior; implementation and tests pin against it.

## §1 Purpose & hard boundaries

ZCP emits anonymous usage events (tool calls, CLI commands, session lifecycle)
to an internal ingest → ClickHouse pipeline so the team can analyze usage,
agent tool-call loops, error pareto, funnels, and version adoption.

Hard boundaries (violations are bugs, not trade-offs):

- **B1** No user/account/project identifiers, ever. Identity = two random UUIDs.
- **B2** No tool arguments, no free text, no hostnames, no paths, no hashes of any
  of those. The ONLY argument bytes ever read are single allowlisted enum keys
  (§5.3) via tiny-struct JSON peeks.
- **B3** Telemetry never blocks, slows measurably, or breaks a tool call or CLI
  command. All failures are swallowed (counted, stderr-logged at most).
- **B4** Telemetry never writes to stdout (MCP STDIO). CLI human output of the
  `zcp telemetry` subcommand and the disclosure notice in CLI mode are exempt
  (they ARE the CLI's stdout product, same as other CLI commands).
- **B5** No event leaves the machine before the disclosure notice has been
  emitted on that install (§3.3).
- **B6** Raw client IP is never persisted anywhere durable (ingest memory-only).
- **B7** Env var names `ZCP_TELEMETRY`, `DO_NOT_TRACK` are frozen forever.

## §2 Identity

- **install_id**: UUIDv4, minted once per install, stored in
  `~/.zcp/telemetry/install.json` (file 0600, dir 0700). Internal channels use
  `~/.zcp/telemetry/install-internal.json` (separate ID namespace so internal
  traffic can never pollute external install counts).
- **session_id**: UUIDv4 minted per OS process (MCP serve mode). NOT the
  workflow WorkSession (whose close is derived/async); telemetry session = process
  lifetime.
- **seq**: atomic uint64 starting at 1 per session; `event_id = session_id:seq`
  is the dedup key.
- UUIDs are generated from `crypto/rand` (no new dependency required).

`install.json` shape: `{"installId": "<uuid>", "disclosedAt": "<RFC3339>",
"disabled": false}`.

## §3 Consent & enablement

### §3.1 Resolution — once per process, then threaded as a value

Consent/config is resolved ONCE at process start (the `runtime.Detect()`
pattern) into an immutable config; call sites never re-read env.

Precedence (first match wins):
1. `ZCP_TELEMETRY` ∈ {`0`,`false`,`off`,`no`} → disabled (explicit opt-out).
2. `DO_NOT_TRACK` ∈ {`1`,`true`} → disabled.
3. `install.json` has `"disabled": true` → disabled.
4. **v1 default-off gate**: `ZCP_TELEMETRY` is NOT an opt-in token
   {`1`,`true`,`on`,`yes`} → disabled (`ReasonDefaultOff`); mint no install id,
   write no stamp, print nothing — a default-off install is inert.
5. No install file / no `disclosedAt` → **pre-disclosure mode** (§3.3).
6. Otherwise → enabled.

v1 is **DEFAULT-OFF**, opt-in via `ZCP_TELEMETRY=1`, uniformly across ALL
channels including internal_dev/eval (eval + dev containers opt in explicitly).
The opt-in gate uses a dedicated token set (`isOptInToken`), never `isTruthy` —
so DO_NOT_TRACK / CI / debug keep their {1,true} meaning. (v2 default-on =
delete rule 4; unset then falls through to disclosure/enabled.)

Install-file read/write errors → disabled for the process (never crash, never
block startup). Opt-out is silent and permanent — no re-prompts, ever.

### §3.2 Channels

`ZCP_TELEMETRY_CHANNEL` ∈ {`external`, `internal_dev`, `internal_eval`, `ci`}.
Unset → `external`, except `CI` env truthy → `ci`. Invalid value → `external`.
Channel is stamped on every event as `usage_channel`.

### §3.3 Disclosure gate (pre-disclosure mode)

The first process on an install (or after opt-in via `zcp telemetry enable`):
prints the disclosure notice — CLI mode: stdout; MCP serve mode: stderr — then
writes `install.json` with fresh `installId` + `disclosedAt`, and emits
NOTHING for the entire process lifetime. Events start with the next process.

Disclosure text (EN, single paragraph): states that ZCP collects anonymous
usage events (tool names, durations, error codes — never arguments, paths, or
identifiers) and shows the opt-out (`ZCP_TELEMETRY=0` or `zcp telemetry
disable`). The FULL GDPR Art-13 notice (controller, legal basis, retention,
recipients, rights, erasure channel) is available on demand via `zcp telemetry
disclosure` (§3.5) — v1 keeps it IN the binary, published nowhere. The short
notice's docs-page link and a confirmed controller/contact are v2
public-launch items (`docs/telemetry-lia.md`, `docs/telemetry-disclosure.md`).

### §3.4 Debug mode

`ZCP_TELEMETRY_DEBUG=1` → events are serialized (one JSON per line) to stderr
instead of being sent; no network, no spool. All other gating identical.

### §3.5 CLI surface

`zcp telemetry status|enable|disable|id|disclosure`:
- `status` — resolved state + the precedence rule that produced it + channel.
- `enable` — records consent (clears `disabled`, stamps disclosure) but does
  NOT emit: v1 is env-gated, so `ZCP_TELEMETRY=1` is still required and the
  message says so (never claims "enabled").
- `disable` — sets `disabled: true` (keeps installId; rows purgeable on
  request). Survives `ZCP_TELEMETRY=1` — rule 3 precedes the opt-in gate.
- `id` — prints the install UUID (the erasure-request key).
- `disclosure` — prints the full GDPR Art-13 notice (stateless; needs no `$HOME`).

## §4 Wire protocol

### §4.1 Envelope

`POST {endpoint}` — default endpoint const
`https://telemetry.zerops.io/v1/events` (production domain TBD), overridable via
`ZCP_TELEMETRY_ENDPOINT` (internal/test use). Body: JSON, gzip-encoded
(`Content-Encoding: gzip`).

```json
{"protocol_version": 1, "batch_id": "<uuid>", "sent_at": "<RFC3339ms>",
 "events": [ ... ]}
```

### §4.2 Event (schema_version 1)

```json
{
  "schema_version": 1,
  "event_id": "<session_id>:<seq>",
  "install_id": "<uuid>", "session_id": "<uuid>", "seq": 42,
  "event_type": "tool_call",
  "client_time": "<RFC3339ms>",
  "zcp_version": "v6.31.0", "os": "darwin", "arch": "arm64",
  "runtime_env": "local",
  "usage_channel": "external",
  "tool": "zerops_deploy", "command": "", "action": "deploy",
  "duration_ms": 1842, "success": false, "error_code": "DIAGNOSIS_REQUIRED",
  "error_subcode": "WORKER_PLAN_SHAPE",
  "workflow_route": "adopt", "workflow_step": "discover", "close_mode": "auto",
  "dims": {"shutdown_reason": "signal"},
  "dropped_count": 0
}
```

`event_type` ∈ {`tool_call`, `cli_command`, `session_start`, `session_end`,
`client_throttle`}. Field usage per type:

| type | filled |
|---|---|
| tool_call | tool, action, duration_ms, success, error_code, error_subcode when split, workflow_route/workflow_step (peeked on a `zerops_workflow` call OR stamped from the last one this process on every other tool_call — §5.3) |
| cli_command | command (top-level), action (subcommand), duration_ms, success |
| session_start | context fields only |
| session_end | duration_ms = uptime, dims.shutdown_reason, dropped_count |
| client_throttle | dropped_count (delta since last throttle event) |

`monotonic_ms` (a client-process-uptime timestamp) shipped in early builds and
was REMOVED (S4, fixes G6): it had no ingest consumer (`seq` already gives
ordering within a session) — the field review ledger (§4.5) is the per-field
audit that caught this. Removal exercises the additive/removal policy (§8)
both directions: a new ingest decoding an old client's JSON (still carrying
`monotonic_ms`) drops the unknown key for free (Go's struct decode); an old
ingest decoding a new client's JSON (missing the key) already treats absent
fields as zero-value. Neither side special-cases it.

### §4.3 Validation ownership — closed enums vs shapes (anti-drift rule)

`internal/telemetry/wire` is the SINGLE OWNER of the schema + validation,
imported by both the client (emit-side lite validation) and the ingest
(strict validation). To avoid re-authoring vocabularies owned elsewhere:

- **Closed enums in wire**: `event_type`, `usage_channel`, `runtime_env`
  (`container`|`local`), dims KEYS (v1: `shutdown_reason`) and per-key value
  sets where enumerable.
- **Shape-validated (pattern + length), NOT enum-validated**: `tool`, `command`,
  `action`, `workflow_route`, `workflow_step`, `close_mode` (owned by tool
  registry / topology — wire must not duplicate them; new tools/routes in newer
  clients must not be rejected by older ingest), `error_code`
  (`^[A-Z][A-Z0-9_]{0,63}$`, owned by `internal/platform/errors.go`), `error_subcode`
  (`^[A-Za-z][A-Za-z0-9_]{0,63}$` — permissive enough for BOTH the ZCP-authored
  `platform.Subcode*` catalog, uppercase snake e.g. `AMBIGUOUS_SCOPE`, AND the
  Zerops platform's own lowerCamelCase error-code vocabulary carried verbatim
  for `API_ERROR`, e.g. `badGateway` — §4.2 "carry platform error-code class",
  S4; wire owns shape only, never either vocabulary), `os`/`arch`
  (GOOS/GOARCH shapes), `zcp_version` (semver shape; anything else coerced to
  `dev` by the client).
- Limits: dims ≤12 keys, key ≤32 chars, value ≤64 chars; event ≤4 KiB encoded;
  batch ≤100 events. Unknown JSON fields: ignored by ingest (forward compat);
  an unrecognized `protocol_version` (the BATCH envelope, spec §4.1) → 400,
  unchanged — no tolerant mode at the envelope level, only per-event.
- Emit sites use wire constants for dim keys — never raw string literals.
- `error_subcode` is OPTIONAL, unlike `error_code` — most call sites haven't
  been split (S4 populated the worst conflations first, §4.2). Absent or
  shape-invalid at the S3/S4 middleware extraction site (`internal/server`)
  leaves it `""`, never a sentinel: a shape-invalid subcode must never itself
  turn into a whole-event rejection at `ValidateLite`/`ValidateStrict`.

#### §4.3.1 Tolerant validation (`ValidateStrict`, S2 G2) — R2's "dozens of client versions" contract

`wire.ValidateStrict(e *Event) (StrictOutcome, error)` never rejects a whole
event over a vocabulary gap between a NEWER client and an OLDER (not yet
redeployed) ingest — only over structural garbage. It MUTATES `e` in place
for the two coercions below; the ingest handler stores the sanitized event,
never the raw wire values.

- **Always required, never tolerated (the "version-1 core")**:
  `event_id`/`install_id`/`session_id` (required, UUID shape), `client_time`
  (required, RFC3339), `seq` (nonzero), `event_id == session_id:seq`
  consistency, and `event_type` (closed enum — the one structural enum that
  STAYS strict always, because storage/dashboards branch on it).
- **Hard resource caps — enforced unconditionally**, regardless of
  `schema_version`: event ≤4 KiB encoded, dims ≤12 keys/≤32/≤64 chars. These
  protect the pipeline from abuse/bugs, not a vocabulary mismatch, so an
  oversize event is still "structural garbage."
- **dims — unrecognized KEY or a known key's unrecognized VALUE**: the whole
  dims entry is dropped (not the event), counted in `StrictOutcome.DimsDropped`
  → ingest running counter `dims_dropped_total` (§6 `/statsz`). Dims is the
  schema's designed-for-growth surface (§8 "new dims = wire allowlist edit"),
  so an entry ingest hasn't caught up to carries no information worth
  rejecting the event over.
- **`usage_channel`/`runtime_env` — unrecognized value**: coerced in place to
  `wire.UnknownChannel`/`wire.UnknownRuntimeEnv` (both literal `"unknown"`,
  added to each enum's own accepted set), flagged in
  `StrictOutcome.{Channel,Runtime}Coerced`. Unlike `event_type`, these two
  tolerate a value newer than what this ingest currently knows.
- **`schema_version` NEWER than `wire.SchemaVersion`**: accepted once the
  core above validates; every other field is accepted AS-IS, unvalidated —
  ingest cannot know what a schema it has never seen means for those fields.
  A JSON field this `Event` struct doesn't know about is already dropped for
  free by the struct decode before `ValidateStrict` ever runs — nothing to
  additionally stash. `StrictOutcome.ForwardCompat` is set → ingest running
  counter `forward_compat_accepted_total` (§6 `/statsz`).
- **`schema_version` OLDER than `wire.SchemaVersion`** (no such version has
  ever shipped — `SchemaVersion` has always been 1): structural garbage,
  rejected (`wire.ErrEnum`).
- **`schema_version == wire.SchemaVersion`** (the common case): full
  `ValidateLite` shape checks apply on top of the core + tolerant
  sanitization above — unchanged strictness for known-schema payloads.

### §4.4 Response & client retry semantics

- `202 {"accepted":n,"rejected":n,"duplicate":n,"retry_after_ms":n,"max_schema_version":n}`.
  `wire.Response` is the single owner of this envelope; `internal/ingest`'s
  `IngestResponse` is a type alias, never a hand-duplicated copy.
- `max_schema_version` is the `schema_version` THIS ingest currently
  supports (`wire.SchemaVersion`) — the release-ordering guard (S2, spec §8
  "backend deploys before client releases"): when a client's own
  `wire.SchemaVersion` is NEWER than the server's advertised value, the
  client stderr-warns exactly ONCE per process (`Client.schemaWarnOnce`) —
  visibility for the "deployed the client before the ingest" mistake. A
  malformed/empty response body never blocks or errors (spec B3).
- 4xx (400/413) → client drops the batch permanently (no retry).
- 429/5xx/timeout → client spools the batch + backs off.

### §4.5 Field review ledger (R3/R4, S4)

Every field this pipeline logs, reviewed for purpose — "every logged value
must earn its place" (R4). Dashboard names marked "(planned, S6)" refer to
the boards named in `plans/telemetry-production-readiness-2026-07-02.md`
S6, not yet built at the time this ledger was written; every dashboard's
default filter is `usage_channel = 'external'` (§7) regardless of the
per-row Dashboard(s) column below.

#### Per-event fields (`wire.Event`, client-emitted)

| field | purpose | example | dashboard(s) | verdict |
|---|---|---|---|---|
| `schema_version` | which `Event` shape this row follows — drives tolerant/forward-compat validation (§4.3.1) | `1` | none directly (validation-only) | kept |
| `event_id` | dedup key `session_id:seq` — ingest LRU dedup + `ReplacingMergeTree` identity | `"3fa8...:42"` | none directly (identity only) | kept |
| `install_id` | anonymous per-install identity — uniq-by-install counts (R5 poisoning resistance) | uuid | Version board (adoption), Pipeline health (blocklist) | kept |
| `session_id` | per-process identity — session grouping, funnel/loop detection | uuid | Session Explorer, Sessions overview (planned, S6) | kept |
| `seq` | per-session monotonic order — within-session event ordering, dedup identity | `42` | Session Explorer ordered timeline (planned, S6) | kept |
| `event_type` | discriminates `tool_call`/`cli_command`/`session_start`/`session_end`/`client_throttle` — routes materialized views | `"tool_call"` | every board (filter dimension) | kept |
| `client_time` | client wall-clock at emit — clamped into `event_time`, feeds `clock_skew_ms` | RFC3339ms | Pipeline health clock-skew distribution (planned, S6) | kept |
| ~~`monotonic_ms`~~ | client-process uptime — NO ingest consumer ever existed (`seq` already orders within-session) | `91342` | none | **REMOVED (G6, S4)** |
| `zcp_version` | which ZCP binary emitted the event — per-version adoption/failure-rate comparison (R2 "dozens of versions") | `"v6.31.0"` | Version board (planned, S6) | kept |
| `os` / `arch` | platform context for triage (a failure pattern specific to one GOOS/GOARCH) | `"darwin"` / `"arm64"` | ad-hoc filter, Pipeline health | kept |
| `runtime_env` | `container`\|`local` — gating/UX differs materially (VPN, auto-adopt), so pareto must split by this | `"local"` | Version board, Session Explorer filter (planned, S6) | kept |
| `usage_channel` | `external`\|`internal_dev`\|`internal_eval`\|`ci` — the default filter EVERY dashboard applies | `"external"` | ALL (default filter) | kept |
| `tool` | which `zerops_*` tool was called — tool-call pareto, funnel stage | `"zerops_deploy"` | Pipeline health, error pareto, Session Explorer | kept |
| `command` | CLI top-level command (`cli_command` events only) | `"sync"` | CLI usage (tool_daily aggregate) | kept |
| `action` | the ONE allowlisted sub-action peeked from Arguments — per-operation pareto | `"deploy"` | Pipeline health, Session Explorer | kept |
| `duration_ms` | operation latency — p50/p95/p99 per tool/action (`mv_tool_daily` `quantilesTiming`) | `1842` | Pipeline health latency, Version board | kept |
| `success` | did the call error — failure-rate aggregation (`mv_tool_daily` `failures`) | `false` | every error-rate panel | kept |
| `error_code` | stable ZCP error code on failure — error pareto | `"DIAGNOSIS_REQUIRED"` | Pipeline health error pareto, Session Explorer | kept |
| `error_subcode` | narrows `error_code` for the worst conflations + carries the platform's own error-code class for `API_ERROR` — distinguishes root causes a bare `error_code` can't (§4.2, plans/telemetry-analysis-2026-07-02.md §3) | `"AMBIGUOUS_SCOPE"` | error pareto drill-down (planned, S6), Session Explorer | **kept (added S4)** |
| `workflow_route` / `workflow_step` | which bootstrap route + step was active — S4 widens this from "only on the `zerops_workflow` call itself" to "stamped onto every tool_call this process", so a deploy/import failure is attributable to its workflow phase (R4) | `"adopt"` / `"discover"` | `workflow_daily` aggregate, Session Explorer | **kept, widened S4** |
| `close_mode` | dev-session auto-close mode (`auto`\|`manual`\|legacy `git-push` fold) | `"auto"` | `workflow_daily` aggregate, Session Explorer | kept |
| `dims` | open-shape narrow map (v1: `shutdown_reason` only) — the schema's designed-for-growth surface (§8 "new dims = wire allowlist edit") | `{"shutdown_reason":"signal"}` | Pipeline health (`dims_dropped_total`), session_end detail | kept |
| `dropped_count` | client-side loss visibility (queue overflow / storm-guard delta) | `0` | Pipeline health client-side loss | kept |

#### Batch envelope fields (`wire.Batch`, not per-event)

| field | purpose | verdict |
|---|---|---|
| `protocol_version` | envelope-shape discriminator — an unrecognized value is the ONE thing that still hard-400s (§4.3, no tolerant mode at envelope level) | kept |
| `batch_id` | groups the rows one HTTP POST inserted — no dashboard consumer, retained for ingest-side tracing only | kept |
| `sent_at` | client send timestamp — no dashboard consumer, retained for ingest-side tracing only | kept |

#### Ingest-derived / storage-only fields (`schema.sql`, never client-emitted)

| field | purpose | dashboard(s) | verdict |
|---|---|---|---|
| `event_time` | `client_time` clamped to ±30 d of server now, else `received_at` — the canonical event timestamp every partition/TTL/materialized view keys off | ALL time-series panels | kept |
| `received_at` | server-side ingest timestamp (CH `DEFAULT now64(3)`) — feeds `clock_skew_ms` and `ingest_version` | Pipeline health clock-skew | kept |
| `clock_skew_ms` | `received_at - client_time`, always computed regardless of the `event_time` clamp — surfaces clients with a badly-wrong clock | Pipeline health clock-skew distribution (planned, S6) | kept |
| `ingest_version` | `MATERIALIZED toUnixTimestamp64Milli(received_at)` — the `ReplacingMergeTree` dedup-on-merge key | none directly (dedup mechanism) | kept |

## §5 Client behavior (`internal/telemetry`)

### §5.1 Pipeline

`Emit(Event)` = lite-validate + seq-assign + non-blocking send into a bounded
channel (cap **4096**); full → drop newest + count. ONE worker goroutine owns
batching, HTTP, spool, retry. Flush at **5 s / 100 events / 128 KiB**, HTTP
timeout **2 s**, failure backoff **5 s → 5 min full jitter**, startup jitter
**0–2 s**. Panic in emit/extraction/worker: recovered; a worker panic disables
telemetry for the rest of the process.

### §5.2 Storm guard

Token bucket per process: **2 events/s sustained, burst 200**, applied to
`tool_call`/`cli_command`. Over limit → drop bodies, count, and emit at most
one `client_throttle` event per minute carrying the dropped delta.
`session_start`/`session_end`/`client_throttle` are exempt.

### §5.3 MCP middleware extraction (the ONLY tap point)

Extends the existing single receiving middleware in `internal/server`
(`observe()`): tool name from `CallToolParamsRaw.Name`; `action` (and for
`zerops_workflow` also `step`, `route`) via single-key tiny-struct peeks of
`Arguments` — nothing else is ever decoded from arguments; outcome from
`CallToolResult.IsError`; on error, `code` AND `subcode` (S4, §4.2) both
decoded from the `ErrorWire` JSON in `Content[0].Text` (`code` absent →
`UNKNOWN`; `subcode` absent or shape-invalid → `""`, not a sentinel — it's
optional). Values failing shape checks → `unknown`.
`ops`/`workflow`/`platform`/`topology` MUST NOT import telemetry (depguard) —
instrumentation stays at the L4 choke point.

**Workflow-context stamping (S4, R4)**: the middleware keeps a per-`Server`
(one process — spec §2 "session_id minted per OS process") `workflowContext`
recording the LAST route/step seen on a `zerops_workflow` call, mutex-guarded
(the MCP SDK dispatches concurrent `tool_use` blocks onto separate
goroutines). Every `zerops_workflow` call both peeks its OWN
`workflow_route`/`workflow_step` (unchanged) and updates the remembered
state (a field is updated only when the call actually supplied it — a
`status`-style call that omits `route` does not erase a previously-
remembered route). Every OTHER tool_call stamps `workflow_route`/
`workflow_step` from that remembered state instead of leaving them empty —
so a `zerops_deploy`/`zerops_import` failure mid-workflow is now
attributable to the workflow phase in flight, not just to its own bare
error_code. Before S4 these two fields were populated ONLY on the
`zerops_workflow` call itself.

### §5.4 Spool

`~/.zcp/telemetry/spool-v1/`: gzip JSONL segments (target 256 KiB), written
`*.tmp` → fsync → rename. Bounds: **10 MiB total or 7 days** — oldest dropped
first. Corrupt segment → rename `*.bad`; `*.bad` deleted at next startup.
Drained oldest-first before fresh flushes without starving the live queue.

### §5.5 Lifecycle

Serve mode: `session_start` after consent resolve; `session_end` at shutdown
(existing `logShutdown` hook: uptime, shutdown-reason enum). Shutdown flush:
fresh **750 ms** context — drain, one send attempt, spool leftovers, THEN one
best-effort spool-segment drain attempt (same context/budget). The second
step exists because the periodic 5 s ticker is the ONLY other
`tryDrainSpool` call site: a short-lived CLI one-shot process routinely exits
before that ticker ever fires, so without a Shutdown-time attempt too, its
spool backlog would never get a chance to send and would just age out at the
7-day bound (fixed gap, formerly G7). No durability promise on SIGKILL/crash.
CLI one-shots: `cli_command` events only, flushed before the single exit
point returns.

## §6 Ingest contract (`cmd/zcp-ingest`)

- `POST /v1/events` (+ `GET /healthz`, `GET /statsz`). No client auth by
  design (a shipped secret is theater); defense = tolerant-but-strict
  validation + limits + blocklist (R5 stance, plan §R5).
- Caps: 64 KiB compressed / 256 KiB decoded / 100 events / 4 KiB per event.
- Tolerant wire validation (§4.3/§4.3.1); per-event accept/reject with
  counts.
- Dedup: in-memory LRU-TTL (24 h, size-bounded) on `event_id`; plus
  `ReplacingMergeTree` collapse in storage.
- Limits: per-IP token bucket 60 req/min burst 600; per-install 10k events/day;
  per-session 5k `tool_call` (then throttle-only); >1k new install-ids/hour/IP
  → 429. All in-memory, per-replica (v1 runs 1 container).
- **Blocklist (R5 ops lever, S2)**: `INGEST_BLOCK_IPS`/`INGEST_BLOCK_INSTALLS`
  env, comma-separated, in-memory exact match, checked before any other
  processing — a blocked IP 403s the whole request before the body is even
  read; a blocked `install_id` 403s as soon as it's seen in the batch
  (preserving whatever the request already legitimately accepted before
  that point, same discipline as the new-install-cardinality breaker below).
  Matched values are NEVER logged, same B6 discipline as the client IP.
  No dynamic reload — a redeploy picks up a new list.
- **Bounded failed-insert retry (G1, S2)**: a failed `Insert()` call queues
  its whole attempted batch for the NEXT flush, prepended ahead of freshly
  buffered rows — a downed ClickHouse no longer silently drops rows (the
  prior gap: memory was already bounded by the 2 s flush drain, but loss was
  unconditional on any insert failure). The retry buffer itself is bounded
  to ~50k rows: once full, the OLDEST rows are dropped to make room, counted
  in `rows_dropped_total` so the loss stays visible instead of silent.
  `insert_failures_total` counts failed `Insert()` attempts (not rows).
- `GET /statsz` (S2 G1/G2, S4): tiny JSON ops-counters payload the
  pipeline-health Grafana panel reads — `events_accepted_total`,
  `events_rejected_total`, `server_errors_total` (S4: make the launch go/no-go
  — no 5xx storms, no schema rejects from real traffic — measurable), plus
  `rows_dropped_total`, `insert_failures_total`, `dims_dropped_total`,
  `forward_compat_accepted_total`. Counts only, never row/event content.
  `/statsz` is per-IP rate-limited like `/v1/events`; `/healthz` is not (its
  ClickHouse ping is cached ≤1 per 5 s behind an in-flight guard, so a flood
  can't stampede CH). The HTTP server sets Read/Write/Idle deadlines
  (Slowloris-safe) — S4 public-ingress hardening.
- Timestamps: `event_time` = `client_time` clamped to ±30 d of server now
  (else `received_at`); `clock_skew_ms` stored.
- IP: used in memory for limiting/blocklist only — never inserted, never
  logged (B6). Derivation (S2): the per-IP key is the balancer-authoritative
  `X-Real-IP` (netip-canonical; client `X-Forwarded-For` is IGNORED — the
  balancer appends it left-most so it is spoofable; `RemoteAddr` fallback for
  in-project traffic). Behind the shared `*.zerops.app` subdomain `X-Real-IP`
  is the constant proxy address, so per-IP limiting degenerates to ONE global
  bucket — accepted for the v1 disposable, monitored test endpoint (kill
  switch = subdomain-off); per-client isolation needs a custom domain (origin
  IP), a v2 default-on precondition. Configured blocklist IPs are canonicalised
  the same way (S6a) so a non-canonical entry still matches the request key.
- Insert: `clickhouse-go` v2 native protocol, batched (~5k rows or 2 s);
  graceful shutdown flushes the open batch (now including any pending
  retry-buffer rows, since the retry buffer is folded into every flush).
- Logging: stderr, no IPs, no payload bodies, no blocklist values.

## §7 Storage

ClickHouse `telemetry.events`: ReplacingMergeTree(ingest_version) — Replicated
variant on HA — ORDER BY `(session_id, seq)` (loop/funnel locality + dedup
identity), PARTITION BY month, **TTL 15 months**; hot dims as LowCardinality
columns; long-tail dims as `Map(LowCardinality, LowCardinality)`. Daily
AggregatingMergeTree aggregates (tool_daily, workflow_daily) fed by
materialized views, retained indefinitely. DDL source of truth:
`internal/ingest/schema.sql` (embedded; the ingest applies it itself at
startup as an idempotent bootstrap when `CH_SUPER_PASSWORD` is set —
`CH_CLUSTER` selects the HA Replicated-engine variant). Dashboards query
aggregates, never raw; default filter `usage_channel = 'external'`.

`telemetry.schema_migrations` (`id UInt32, applied_at`) ships as part of the
base schema itself (chicken-and-egg: the migration runner needs it to exist
before it can query which ids are already applied) and records every
migration id that has run. Schema changes to already-created objects (the
base bootstrap is `CREATE ... IF NOT EXISTS`-only, so it never alters an
existing table) go through an embedded, ordered, idempotent migration step
instead of a manual DDL dance (§8).

`migrations/0002_add_error_subcode.sql` (S4) is the migration mechanism's
first real DDL consumer: `ALTER TABLE __DB__.events ADD COLUMN IF NOT
EXISTS error_subcode LowCardinality(String) AFTER error_code`. Migration
statement text uses the SAME `__DB__` token `schema.sql` uses
(`renderMigrationDB` substitutes it before exec) — `openAdminConn` carries
no default database, so every migration DDL statement must be fully
qualified. `insertColumns` (`internal/ingest/clickhouse.go`) therefore
spans the base `schema.sql` shape PLUS every applied migration, not
`schema.sql` alone — pinned by
`TestRenderBootstrapStatements_EventsColumnsMatchInsertColumns`, which
checks each inserted column against the base DDL OR a migration's `ADD
COLUMN`.

## §8 Schema evolution

Backend deploys before client releases — getting this order wrong is now
visible, not silent: the `202` response's `max_schema_version` (§4.4) lets a
client newer than the deployed ingest warn once to stderr. Additive-only
within a `schema_version`; new dims = wire allowlist edit + ingest redeploy
(no DDL migration) — a dims key/value the deployed ingest doesn't yet
recognize is tolerated (§4.3.1), never a hard failure, so a client can ship
ahead of the ingest that understands its new dims.

**ClickHouse DDL changes to an already-created object (S3, fixes G3)** are
enforced additive-only BY MECHANISM, not by convention:
`internal/ingest/migrations/*.sql` holds ordered, embedded, idempotent
migration steps, named `NNNN_description.sql` (4-digit id). `migrate.go`
loads them (`loadMigrations`), reads which ids `telemetry.schema_migrations`
already has recorded, and applies only the pending ones in id order — each
one's DDL executes, then its id is recorded; a failed DDL statement is never
recorded, so the next run (redeploy, restart, HA-replica retry) retries it
from scratch instead of silently skipping it as done. This is exactly the
restart-race case that used to require a manual DROP dance: migrations are
now versioned, so a re-run is a no-op once applied. `applyMigrations` runs
once `bootstrapSchema` has proven ClickHouse connectivity by applying the
base schema (fresh installs get the full current shape straight from
`schema.sql`'s `CREATE ... IF NOT EXISTS`; existing installs pick up
whatever migrations they haven't seen yet). Authoring convention: each
migration's own DDL must itself be idempotent (`ADD COLUMN IF NOT EXISTS`
etc.) — the runner's applied-id bookkeeping guards against re-running an
already-recorded migration, not against two concurrent appliers racing the
same unrecorded one (acceptable: v1 runs a single ingest replica, §6).

Ingest normalizes all wire versions seen in ≤18-month-old clients; a
`schema_version` newer than ingest currently supports is itself tolerated on
its version-1 core (§4.3.1) rather than rejected outright. Enum values never
change meaning; deprecated values stay accepted until raw TTL expires for
the last emitting version.

## §9 Invariants (pin each with a test)

- T1 A tool call with telemetry disabled/enabled/panicking produces identical
  tool results and no measurable latency delta (middleware no-op path).
  Pinned via `BenchmarkEmit_Disabled`/`BenchmarkEmit_Enabled`
  (`internal/telemetry/chaos_test.go`): budget is Emit disabled ≤ ~100 ns,
  enabled non-blocking enqueue ≤ ~5 µs. Measured (Apple M5 Max, `go test
  -bench`, 2026-07-02): disabled ~47 ns/op, 1 alloc; enabled non-blocking
  enqueue ~1.1-4.5 µs/op across runs (worker-goroutine flush/GC scheduling
  noise), 5-6 allocs — both comfortably inside budget.
- T2 Nothing beyond allowlisted single keys is ever decoded from arguments —
  test with secret/path fixtures asserting emitted events contain none of them.
- T3 Pre-disclosure process emits zero events and stamps the marker exactly once.
- T4 `ZCP_TELEMETRY=0` / `DO_NOT_TRACK=1` → zero events, zero spool writes.
- T5 Queue overflow drops events, never blocks Emit.
- T6 Storm guard: >200-burst loop yields ≤ bucket output + throttle events with
  correct dropped counts.
- T7 Spool respects bounds, survives corrupt segments, replays oldest-first.
- T8 Ingest rejects structural garbage: oversize event, over-length dims,
  non-UUID ids, unrecognized `event_type`, `schema_version` OLDER than
  supported; duplicates counted, not double-inserted. Ingest TOLERATES
  (accepts, never rejects) an unrecognized dims key/value (dropped, counted),
  an unrecognized `usage_channel`/`runtime_env` (coerced to "unknown",
  counted), and a `schema_version` NEWER than supported (accepted when the
  core validates, counted) — spec §4.3.1, pinned in
  `internal/telemetry/wire/validate_test.go` + `internal/ingest/handler_test.go`.
- T9 No IP value reaches any durable ingest output (grep-level + handler tests).
- T10 Depguard/architecture: `ops`/`workflow`/`platform`/`topology` do not
  import telemetry; `wire` is stdlib-only; every tools input struct `Action`
  field carries json tag `action` (middleware peek contract).
- T11 (S4) `error_subcode` reaches the wire end-to-end for the three worst
  INVALID_PARAMETER conflations (bootstrap adopt-pairing ambiguity,
  recipe-plan mismatch, classic-plan shape) AND for `API_ERROR`'s carried
  platform error-code class, dispatched via `errors.Is` against
  workflow-layer sentinels (`ErrAdoptPairingChoice`, `ErrRecipePlanMismatch`,
  `ErrPlanShapeInvalid`) — never string-sniffing the message. Every
  non-`zerops_workflow` tool_call stamps `workflow_route`/`workflow_step`
  from the last `zerops_workflow` call this process. Pinned by
  `internal/platform/zerops_errors_test.go`,
  `internal/workflow/engine_test.go`,
  `internal/tools/workflow_bootstrap_subcode_test.go`,
  `internal/server/telemetry_test.go`.
