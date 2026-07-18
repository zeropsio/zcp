# PRD: ZCP Anonymous Usage Telemetry

Date: 2026-07-02 · Status: PROPOSED (awaiting approval) · Analysis: `telemetry-analysis-2026-07-02.md`

## 1. Problem

ZCP ships to thousands of installs with zero visibility into how it is used. We
cannot answer: which tools/workflows agents actually run, where agents get stuck
looping on tool calls, which error codes dominate in the wild, whether bootstrap
funnels complete, or how fast releases adopt. The behavioral eval suite covers
scenarios we *imagine*; telemetry covers what *actually happens*.

## 2. Goals (the questions the data must answer)

- G1 Usage: tool/action call volume, latency, per version/OS/env (container|local).
- G2 Loops: sessions where the same (tool, action[, error_code]) repeats N× —
  centrally queryable, validated against eval ground truth.
- G3 Errors: pareto of stable error codes per tool/version.
- G4 Funnels: bootstrap → deploy → success completion per session.
- G5 Adoption: active installs per zcp version over time.

## 3. Non-goals / explicit v1 cut list

- NO user/account/project attribution, ever (locked).
- NO tool argument capture — not even hashed (hashes of paths/hostnames leak structure).
- NO free-text capture (error messages, prompts, YAML).
- NO client authentication (a secret shipped in a public binary is theater).
- NO exactly-once delivery (dedup-friendly analytics instead).
- NO sampling (loop/funnel queries need full per-session fidelity; tail bounded by caps).
- NO remote config / kill-switch service (env opt-out + server-side drop rules suffice).
- NO real-time client-side loop intervention (observe first; separate future product).
- NO user-facing telemetry UI beyond `zcp telemetry` status/enable/disable/id.
- Detecting the zcli/raw-API escape hatch directly — structurally invisible to ZCP
  instrumentation; we see the failing precursor + session silence only.

## 4. Architecture overview

```
zcp binary (public, thousands of installs)
├─ internal/telemetry        client: consent, IDs, queue, spool, sender
├─ internal/telemetry/wire   SINGLE OWNER: event structs + dims allowlist (stdlib-only)
├─ internal/server           observe() middleware → Emit(tool_call)
└─ cmd/zcp                   CLI events, session start/end, final flush
        │  HTTPS POST /v1/events (gzip JSON batches, no auth)
        ▼
zcp-telemetry (internal Zerops project)
├─ ingest    Go service (cmd/zcp-ingest, same repo, imports wire) —
│            validate → allowlist-enforce → dedup → batch insert; never persists IP
├─ clickhouse@25.3 (HA)   raw events, TTL 15 months + aggregate MVs (forever)
└─ observability stack    Grafana + ClickHouse datasource plugin, dashboards+alerts
```

Identity: random install UUID (`~/.zcp/telemetry/install.json`, 0600) + random
per-process session UUID + atomic per-session `seq`. Consent: opt-out —
`ZCP_TELEMETRY=0` or `DO_NOT_TRACK=1` disables; first process after install/update
shows disclosure and emits nothing; events start with the next process.

## 5. Client design (`internal/telemetry`)

### 5.1 Placement & layering
- Cross-cutting peer package (model: `internal/update`). Call sites L4 ONLY:
  `internal/server` (middleware) + `cmd/zcp` (CLI, lifecycle).
- `internal/telemetry/wire`: stdlib-only leaf holding event structs, enums, dims
  allowlist, `protocol_version`/`schema_version` constants. Imported by client AND
  ingest — the tell and the check share one owner and cannot drift.
- Depguard: add deny entries so `ops/`, `workflow/`, `platform/`, `topology/` can
  never import `internal/telemetry` (single-choke-point enforcement) + matching
  rule in `internal/topology/architecture_test.go`.

### 5.2 Consent resolution (once, then threaded)
- Resolved ONCE at process start (the `runtime.Detect()` pattern): disabled when
  `ZCP_TELEMETRY ∈ {0,false,off,no}` or `DO_NOT_TRACK ∈ {1,true}` or disclosure
  marker absent (§10) or install.json unwritable. Never re-read env at call sites.
- `ZCP_TELEMETRY_DEBUG=1`: serialize events to stderr instead of sending (trust +
  self-debugging surface; also what tests exercise).
- Opt-out is silent and permanent — no re-prompt, ever.

### 5.3 Emit path — never block, never break a tool call
- `Emit(Event)` = validate-lite + seq assignment + non-blocking send into a bounded
  channel (cap 4096). Queue full → drop newest, bump atomic dropped counter.
- ONE worker goroutine owns batching, HTTP, spool I/O, retry state.
- Flush: every 5 s, or 100 events, or 128 KiB encoded — whichever first.
  HTTP timeout 2 s. Send failure → exponential backoff 5 s→5 min, full jitter;
  batch goes to spool. Startup jitter 0–2 s (release-wave smearing).
- Panic isolation: emit + extraction wrapped in `recover()`; a worker panic disables
  telemetry for the process (counted). Telemetry never returns an error to dispatch.
- MCP middleware (extend existing `observe()`):
  - tool name: `CallToolParamsRaw.Name` (free);
  - action: single-key JSON peek `{Action string}` — the ONLY argument field read;
  - workflow dims for `zerops_workflow`: additional allowlisted enum peeks
    (`step`, `route`) via the same tiny-struct technique;
  - outcome: `IsError` + (error path only) decode `Content[0].Text` → `code`;
    unknown/absent → `UNKNOWN`. Unknown enum values → `unknown`, never raw strings.

### 5.4 Storm guard (loops must not amplify themselves)
- Client token bucket: 2 events/s sustained, burst 200, per process.
- Over limit → drop event bodies, emit ≤1 `client_throttle` event/min carrying
  `dropped_count`.
- Why detection survives: burst 200 ≫ the ≥8-repeat loop threshold (eval max
  observed single-loop run ≈140 calls); the throttle event itself is a loud loop
  signal. `session_start`/`session_end`/`client_throttle` are exempt from the bucket.

### 5.5 Disk spool (crash/offline tolerance, bounded)
- `~/.zcp/telemetry/spool-v1/`, gzip JSONL segments (write `*.tmp`, fsync, rename).
- Bounds: 10 MiB total or 7 days — drop oldest segments first.
- Drain oldest-first before fresh flushes, without starving the live queue.
- Corrupt segment → rename `*.bad`, delete on next start. File idioms copied from
  `gate_refusal_ledger.go` (append-safety, stderr-on-failure).

### 5.6 Lifecycle & CLI
- Session = OS process lifetime (NOT WorkSession — its close is derived, async).
  `session_start` on serve start; `session_end` in `logShutdown()` (already computes
  uptime + shutdown reason enum — reuse as dims).
- Shutdown: after `run()` returns, `telemetry.Shutdown(fresh 750ms ctx)` — drain,
  one send attempt, spool leftovers. NO durability promise on SIGKILL/crash/reap.
- CLI: refactor `cmd/zcp/main.go` to a single exit point (replace `log.Fatalf`
  branches with bubbled errors; one `os.Exit` after flush). Emits `cli_command`
  events (command enum, duration, outcome). This refactor is Clean-Code-correct
  independent of telemetry.
- Stdout discipline: `TestNoStdoutOutsideJSONPath` already covers the new package;
  all diagnostics → stderr.

## 6. Wire protocol

Batch envelope: `{protocol_version: 1, batch_id: uuid, sent_at, events: [...]}`.

Event (v1 fields):

```json
{
  "schema_version": 1,
  "event_id": "<session_id>:<seq>",
  "install_id": "uuid", "session_id": "uuid", "seq": 42,
  "event_type": "tool_call | cli_command | session_start | session_end | client_throttle",
  "client_time": "RFC3339ms", "monotonic_ms": 91342,
  "zcp_version": "v6.31.0", "os": "darwin", "arch": "arm64",
  "runtime_env": "container | local",
  "usage_channel": "external | internal_dev | internal_eval | ci",
  "tool": "zerops_deploy", "command": "", "action": "deploy",
  "duration_ms": 1842, "success": false, "error_code": "DIAGNOSIS_REQUIRED",
  "workflow_route": "adopt", "workflow_step": "discover", "close_mode": "auto",
  "dims": {"shutdown_reason": "signal"},
  "dropped_count": 0
}
```

- `dims`: ≤12 keys, key ≤32 chars, value ≤64 chars, enum-only per the wire allowlist.
- Versioning: additive-only within a `schema_version`; new dims need NO migration
  (allowlist edit + ingest redeploy). Ingest normalizes every supported wire version
  (≥18 months of old clients) into the current row shape. Unknown future
  `schema_version` → 400; client drops on any 4xx (no retry).
- Dirty/dev versions coerced to `dev` (cardinality guard).

## 7. Ingest service (`cmd/zcp-ingest`)

- `POST /v1/events`, JSON, gzip accepted. No auth (see §3) — defense is validation.
- Caps: 64 KiB compressed / 256 KiB decoded / 100 events per batch / 4 KiB per event.
- Validation: strict required fields; reject unknown enum values (except client-mapped
  `unknown`), unknown dim keys, out-of-enum dim values, control chars, over-length
  strings, non-UUID ids. Clamp client timestamps >30 d skew (store both, flag).
- Responses: 202 `{accepted, rejected, duplicate, retry_after_ms}`; 400/413 (client
  drops); 429/503 (client spools + backs off).
- Dedup: key `(session_id, seq)`; in-memory LRU-TTL (24 h) + `ReplacingMergeTree`
  collapse + `uniqCombined64(event_id)` in duplicate-sensitive queries.
- IP hygiene (non-negotiable): request IP used in-memory for rate limiting only;
  never inserted, never logged, no hash-and-store (a stable hash is still an
  identifier). Access-log scrubbing verified at infra level (§15 open item).
- Abuse handling (realistic): per-IP token bucket 60 req/min burst 600; per-install
  10k events/day; per-session 5k tool_call cap (then throttle-only); new-install
  cardinality guard (>1k new install-ids/hour/IP → 429); strict schema as the main
  wall. Theater (rejected): embedded keys, HMAC-in-binary, obfuscated URLs.
- Insert: `clickhouse-go` v2 native protocol, `PrepareBatch`, flush ~5-10k rows or
  1–5 s; `async_insert` kept only as defense-in-depth setting.

## 8. Storage (ClickHouse `clickhouse@25.3`, HA)

Raw table (loop/funnel-locality ordering + dedup):

```sql
CREATE TABLE telemetry.events
(
  schema_version UInt16, protocol_version UInt16,
  event_id String, batch_id UUID,
  install_id UUID, session_id UUID, seq UInt64,
  event_type LowCardinality(String),
  event_time DateTime64(3,'UTC'),            -- server-clamped canonical time
  client_time DateTime64(3,'UTC'),
  received_at DateTime64(3,'UTC') DEFAULT now64(3),
  clock_skew_ms Int64,
  zcp_version LowCardinality(String), os LowCardinality(String),
  arch LowCardinality(String), runtime_env LowCardinality(String),
  usage_channel LowCardinality(String),
  tool LowCardinality(String), command LowCardinality(String),
  action LowCardinality(String),
  duration_ms UInt32 CODEC(T64, ZSTD),
  success UInt8, error_code LowCardinality(String),
  workflow_route LowCardinality(String), workflow_step LowCardinality(String),
  close_mode LowCardinality(String),
  dims Map(LowCardinality(String), LowCardinality(String)),
  dropped_count UInt32,
  ingest_version UInt64 MATERIALIZED toUnixTimestamp64Milli(received_at)
)
ENGINE = ReplicatedReplacingMergeTree(ingest_version)
PARTITION BY toYYYYMM(event_time)
ORDER BY (session_id, seq)
TTL toDate(event_time) + INTERVAL 15 MONTH DELETE;
```

- ORDER BY `(session_id, seq)`: loop-detection + funnels range-scan one session
  contiguously AND the key doubles as the dedup identity. Dashboards never scan raw —
  they hit the MVs.
- Aggregates (kept forever — pure anonymous aggregates): `tool_daily`
  (AggregatingMergeTree: calls, uniq installs/sessions, failures, duration
  quantiles per day/tool/action/version/env/channel/error_code) + `workflow_daily`
  (route/step/close_mode/success per day) — both fed by materialized views.
- Loop queries (validated against eval ground truth in P1):
  - run-length: `arrayCompact(groupArray(...))` RLE over (tool, action) per session,
    flag max run ≥5;
  - error-retry: same (tool, action, error_code) failing ≥3× within 10 min;
  - funnel: `windowFunnel(7200)(event_time, step='discover', tool='zerops_deploy', success=1)`;
  - route-reset: `INVALID_PARAMETER` repeats followed by `workflow_route` change.

## 9. Dashboards (Grafana, starter set)

1. Adoption: daily active installs (uniq install_id) by version/os/env/channel.
2. Tool usage: calls + p50/p95 latency + failure rate per tool/action.
3. Error pareto: error_code × tool, week-over-week delta.
4. Loop board: sessions flagged by run-length/error-retry queries, top offenders
   by (tool, action, error_code) — the "kde se to točí" view.
5. Funnels: bootstrap→deploy→success completion rate by route.
6. Pipeline health: ingest 4xx/5xx rates, dedup ratio, dropped/throttle counts,
   clock-skew distribution + alerting.

## 10. Privacy & disclosure

- Disclosure gate: first process after install/update prints the notice (CLI:
  stdout; serve mode: stderr), writes the marker into `install.json`, and emits
  NOTHING. Events start with the next process. No event ever precedes disclosure.
- `zcp init` adds a one-line telemetry note (docs link + opt-out) to the generated
  agent context so LLM-driven usage surfaces it too.
- Public docs page: exact event JSON shape (field-by-field), what/why/where/
  retention/opt-out, erasure mechanics (user reads install-id via
  `zcp telemetry id`, submits it; we purge matching rows).
- Legitimate Interests Assessment written and filed internally before external
  rollout; CNIL-style audience-measurement fallback argument documented for the
  ePrivacy Art 5(3) exposure.
- Retention enforced by TTL: raw 15 months (named need: YoY version-adoption),
  aggregates indefinite. `ZCP_TELEMETRY` / `DO_NOT_TRACK` names frozen forever.
- New dims/fields require a privacy review gate: deny-by-default allowlist edit in
  `wire` + explicit check "is this re-identifying beyond the two UUIDs?" (the
  Vercel-teamId lesson).

## 11. Internal usage channels

- `ZCP_TELEMETRY_CHANNEL=external|internal_dev|internal_eval|ci`; default external;
  `CI=true` auto-maps to `ci` when unset. Internal channels use a separate install-id
  file so team/eval noise can never pollute external install counts.
- flow-eval runner + eval-zcp containers export `internal_eval`; local dev shells
  export `internal_dev` (documented in CLAUDE.local.md when implemented).
- Grafana dashboards default-filter `usage_channel = 'external'`.

## 12. Rollout phases (each gated)

- **P0 — build**: `wire` + client + middleware emit + CLI single-exit refactor +
  ingest + DDL + Grafana provisioning in a new internal Zerops project
  (`zcp-telemetry`). Telemetry hard-gated to internal channels only (external emit
  compiled-out-by-flag or channel-gated). Gate: unit+tool+integration green;
  `ZCP_TELEMETRY_DEBUG=1` shows correct payloads; zero measurable tool-call latency
  impact (benchmark middleware).
- **P1 — internal validation**: eval suites + team dev run against the live pipeline
  for ≥1 week. Gate: loop board reproduces the known eval loop patterns
  (adopt-discover bounce, git-push-setup loop, deploy/override dance); dashboards
  answer G1–G5 for internal data; ingest survives a deliberate event-storm test;
  IP verified absent from ingest's durable surfaces (DB rows + service logs).
- **P2 — external launch**: disclosure docs page live; LIA filed; release notes
  announce; binary release flips external default-on (opt-out). Gate: error budget
  on ingest (no 5xx storms), no schema rejects from real traffic, opt-out verified
  end-to-end.
- **P3 — enrichment** (separate plans): `error_subcode` enum taxonomy for
  `INVALID_PARAMETER`-class conflation; funnel refinement; public read-only
  aggregate page (brew.sh/analytics-style) as a trust lever.

## 13. Testing strategy (repo conventions)

- TDD per layer: `wire` allowlist + validation table-driven; queue/spool/storm-guard
  unit tests (incl. drop-policy, corrupt-segment recovery); middleware extraction
  tests with secret/path fixtures asserting NOTHING beyond allowlisted fields is
  captured (the anti-leak pin); ingest handler tests (caps, dedup, enum rejects);
  integration: serve loop with mock ingest.
- Drift guards: architecture test denying `ops`/`workflow` → `telemetry` imports;
  stdout-purity already auto-covers; a `TestWireAllowlistIsClosed` pinning that every
  emitted dim key exists in the allowlist (tell == check, one owner).
- E2E (opt-in env-gated): real ingest + ClickHouse in eval-zcp-adjacent project.

## 14. Effort estimate

| Component | Scope |
|---|---|
| `wire` + client (queue/spool/consent/ids/sender) | ~900–1200 LOC + tests |
| Middleware + CLI single-exit refactor | ~250–400 LOC |
| Ingest service | ~600–900 LOC + tests |
| DDL + MVs + Grafana provisioning + dashboards | config-heavy, ~1–2 days |
| Docs page + LIA + disclosure flows | ~1 day |
| **Total** | **~2–3k LOC, roughly 5–7 dev days + P1 soak week** |

## 15. Open questions (need answers before/during P0)

Resolved 2026-07-02: infra-level balancer IP logs are accepted as-is (user
decision) — ZCP's guarantee stays app-level: ingest never persists/logs IPs.

1. **Ingest deployment shape**: same repo `cmd/zcp-ingest` (recommended — shares
   `wire`, single owner) vs separate repo. Recommendation stands unless build/release
   coupling is unwanted.
2. **Erasure SLA**: commit to a response window for install-id purge requests
   (suggest: manual, best-effort 30 days, documented).
3. **Args-hash for stuck-vs-progressive retry discrimination**: valuable but touches
   the locked NO-arguments line — parked; revisit only with an explicit new decision.
   (Eval-evidence limit #3.)
4. **`session_end` for CLI one-shots**: v1 emits `cli_command` only (no session
   bracketing) — confirm that's acceptable for G1 (it is, unless CLI funnels emerge).
