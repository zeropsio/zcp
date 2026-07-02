-- Embedded ClickHouse schema body for the telemetry ingest bootstrap
-- (bootstrap.go renders __DB__ / __REPLICATED__ and prepends the
-- CREATE DATABASE + GRANT preamble; see renderBootstrapStatements).
-- Column contract mirrors internal/telemetry/wire + server-side fields
-- (docs/spec-telemetry.md §4.2/§7). insertColumns in clickhouse.go MUST
-- stay in lockstep with the events table below PLUS every column added by
-- an embedded migration (internal/ingest/migrations/*.sql, spec §8) — this
-- file is the events table's BASE shape as of the migration mechanism's
-- introduction (S3), not necessarily its full current shape.

-- schema_migrations records which embedded, ordered migration steps
-- (internal/ingest/migrations/*.sql, applied by migrate.go) have already
-- run, so a redeploy/restart replays nothing (docs/spec-telemetry.md §8).
-- Plain (non-Replicated-dedup) engine is deliberate: v1 runs a single
-- ingest replica (spec §6), and the apply path already checks-before-
-- inserting, so no dedup-on-merge behavior is relied upon here.
CREATE TABLE IF NOT EXISTS __DB__.schema_migrations
(
    id         UInt32,
    applied_at DateTime64(3, 'UTC') DEFAULT now64(3)
)
ENGINE = __REPLICATED__MergeTree
ORDER BY id;

CREATE TABLE IF NOT EXISTS __DB__.events
(
    schema_version   UInt16,
    protocol_version UInt16,
    event_id         String,
    batch_id         UUID,

    install_id       UUID,
    session_id       UUID,
    seq              UInt64,

    event_type       LowCardinality(String),
    event_time       DateTime64(3, 'UTC') CODEC(Delta, ZSTD),
    client_time      DateTime64(3, 'UTC') CODEC(Delta, ZSTD),
    received_at      DateTime64(3, 'UTC') DEFAULT now64(3),
    clock_skew_ms    Int64,

    zcp_version      LowCardinality(String),
    os               LowCardinality(String),
    arch             LowCardinality(String),
    runtime_env      LowCardinality(String),
    usage_channel    LowCardinality(String),

    tool             LowCardinality(String),
    command          LowCardinality(String),
    action           LowCardinality(String),
    duration_ms      UInt32 CODEC(T64, ZSTD),
    success          UInt8,
    error_code       LowCardinality(String),

    workflow_route   LowCardinality(String),
    workflow_step    LowCardinality(String),
    close_mode       LowCardinality(String),

    dims             Map(LowCardinality(String), LowCardinality(String)),
    dropped_count    UInt32,

    ingest_version   UInt64 MATERIALIZED toUnixTimestamp64Milli(received_at)
)
ENGINE = __REPLICATED__ReplacingMergeTree(ingest_version)
PARTITION BY toYYYYMM(event_time)
ORDER BY (session_id, seq)
TTL toDate(event_time) + INTERVAL 15 MONTH DELETE;

CREATE TABLE IF NOT EXISTS __DB__.tool_daily
(
    day            Date,
    usage_channel  LowCardinality(String),
    zcp_version    LowCardinality(String),
    runtime_env    LowCardinality(String),
    tool           LowCardinality(String),
    command        LowCardinality(String),
    action         LowCardinality(String),
    error_code     LowCardinality(String),

    calls          AggregateFunction(uniqCombined64, String),
    installs       AggregateFunction(uniqCombined64, UUID),
    sessions       AggregateFunction(uniqCombined64, UUID),
    failures       AggregateFunction(uniqCombined64, String),
    duration_q     AggregateFunction(quantilesTiming(0.5, 0.95, 0.99), UInt32)
)
ENGINE = __REPLICATED__AggregatingMergeTree
PARTITION BY toYYYYMM(day)
ORDER BY (day, usage_channel, zcp_version, runtime_env, tool, command, action, error_code);

CREATE MATERIALIZED VIEW IF NOT EXISTS __DB__.mv_tool_daily
TO __DB__.tool_daily AS
SELECT
    toDate(event_time)                                 AS day,
    usage_channel, zcp_version, runtime_env, tool, command, action, error_code,
    uniqCombined64State(event_id)                      AS calls,
    uniqCombined64State(install_id)                    AS installs,
    uniqCombined64State(session_id)                    AS sessions,
    uniqCombined64StateIf(event_id, success = 0)       AS failures,
    quantilesTimingState(0.5, 0.95, 0.99)(duration_ms) AS duration_q
FROM __DB__.events
WHERE event_type IN ('tool_call', 'cli_command')
GROUP BY day, usage_channel, zcp_version, runtime_env, tool, command, action, error_code;

CREATE TABLE IF NOT EXISTS __DB__.workflow_daily
(
    day            Date,
    usage_channel  LowCardinality(String),
    workflow_route LowCardinality(String),
    workflow_step  LowCardinality(String),
    close_mode     LowCardinality(String),
    success        UInt8,

    events         AggregateFunction(uniqCombined64, String),
    sessions       AggregateFunction(uniqCombined64, UUID),
    installs       AggregateFunction(uniqCombined64, UUID)
)
ENGINE = __REPLICATED__AggregatingMergeTree
PARTITION BY toYYYYMM(day)
ORDER BY (day, usage_channel, workflow_route, workflow_step, close_mode, success);

CREATE MATERIALIZED VIEW IF NOT EXISTS __DB__.mv_workflow_daily
TO __DB__.workflow_daily AS
SELECT
    toDate(event_time)              AS day,
    usage_channel, workflow_route, workflow_step, close_mode, success,
    uniqCombined64State(event_id)   AS events,
    uniqCombined64State(session_id) AS sessions,
    uniqCombined64State(install_id) AS installs
FROM __DB__.events
WHERE event_type = 'tool_call' AND workflow_route != ''
GROUP BY day, usage_channel, workflow_route, workflow_step, close_mode, success;
