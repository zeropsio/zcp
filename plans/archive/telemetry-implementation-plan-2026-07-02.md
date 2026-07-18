# Implementation Plan: ZCP Telemetry (P0 build)

Date: 2026-07-02 · Executors: Sonnet 5 subagents, one slice per agent, strictly
sequential · Contract: `docs/spec-telemetry.md` (THE authority — on conflict,
spec wins over this plan; report the conflict) · PRD:
`plans/prd-telemetry-2026-07-02.md` · Infra bundle (outside repo):
`/Users/macbook/Documents/Zerops-MCP/zcp-telemetry/`.

## Execution rules (every agent)

1. Read FIRST: `docs/spec-telemetry.md` (whole), this file (your slice +
   these rules), `CLAUDE.md` (conventions + traps). Then the files your slice
   names.
2. TDD: for new behavior write the failing test first, then implement. Pure
   plumbing (wiring a param through) may skip RED but must keep all layers green.
3. Gates before you return (ALL must pass): `go build ./...`,
   `go test ./... -short`, `make lint-fast`.
4. FORBIDDEN: `git commit`/`git push` (working tree only — the user reviews);
   `make install`; `make release*`; editing `docs/spec-telemetry.md` or this
   plan; writing to stdout from `internal/` (MCP STDIO discipline —
   `TestNoStdoutOutsideJSONPath` will catch you); `panic()`; global mutable
   state (except `sync.Once`); `interface{}`/`any` where the concrete type is
   known; bare `return err` (wrap: `fmt.Errorf("op: %w", err)`).
5. NO silent scope cuts: if a plan item can't be done as written, STOP work on
   that item and put it in your report as BLOCKED with the reason — do not
   improvise a different design.
6. Return a report ≤60 lines: per-item status (done/partial/BLOCKED), files
   touched, gate outputs (one line each), deviations, notes for the next slice.
7. Naming: `Test{Op}_{Scenario}_{Result}`, table-driven where natural.
   `t.Parallel()` only where no global state (document why not otherwise).

## Slice S1 — `internal/telemetry/wire` (schema + validation single owner)

New package, **stdlib-only** (this is a hard rule — it is imported by both the
client and the ingest).

Files:
- `internal/telemetry/wire/wire.go` — `ProtocolVersion = 1`, `SchemaVersion = 1`;
  `Event` struct with exact JSON tags per spec §4.2; `Batch` struct per §4.1;
  event-type consts (`EventToolCall = "tool_call"`, …); channel consts;
  runtime-env consts; dim-key consts (v1: `DimShutdownReason = "shutdown_reason"`).
- `internal/telemetry/wire/validate.go` — `ValidateLite(e *Event) error`
  (client-side: shape + dims caps) and `ValidateStrict(e *Event) error`
  (ingest-side: everything in spec §4.3 — closed enums, shape patterns, length
  limits, non-empty required fields, UUID shape for install/session ids,
  `event_id == session_id:seq`). Shared helpers; precompiled regexps as
  package-level `var` (immutable — allowed). Limits as exported consts
  (`MaxDims = 12`, `MaxDimKeyLen = 32`, `MaxDimValueLen = 64`,
  `MaxEventBytes = 4096`, `MaxBatchEvents = 100`).
- `internal/telemetry/wire/validate_test.go` — table-driven: valid event per
  type; each rejection class from spec §4.3 (unknown event_type, unknown dim
  key, over-length dim value, bad error_code shape, bad UUID, seq/event_id
  mismatch, >12 dims); lite-vs-strict difference (lite accepts unknown route
  shape? no — lite checks shape+caps only, strict adds closed-enum + required
  checks; encode this distinction in the table).

Notes: version coercion (non-semver → `dev`) is a CLIENT emit concern (S2), not
wire validation — wire only shape-checks. Do NOT add enum lists for
tool/route/step/close_mode (spec §4.3 anti-drift rule).

## Slice S2 — `internal/telemetry` client core

Depends on S1. New files (cohesion-driven, suggested split):
- `config.go` — `Config` (Enabled, PreDisclosure, Channel, Endpoint, Debug,
  InstallID, SessionID, ZcpVersion, OS, Arch, RuntimeEnv) + `Resolve(...)`
  implementing spec §3.1–§3.4 precedence exactly. Inputs passed explicitly
  (env lookup func, home dir, version string, runtime-env string, stderr
  writer) so tests inject everything. `Resolve` performs the disclosure print +
  stamp when in pre-disclosure mode (spec §3.3).
- `install.go` — load/create `~/.zcp/telemetry/install.json` and
  `install-internal.json` (internal channels), 0600/0700, write tmp+rename;
  UUIDv4 via `crypto/rand` (16 bytes, set version/variant bits) — no new dep.
- `client.go` — `New(cfg Config) *Client`; `Emit(wire.Event)` non-blocking
  (chan cap 4096, drop-newest + atomic dropped counter); single worker
  goroutine: batcher (5 s / 100 events / 128 KiB), sender (http.Client
  Timeout 2 s, gzip body), backoff 5 s→5 min full jitter, startup jitter 0–2 s;
  `Shutdown(ctx)` per spec §5.5; debug mode writes JSONL to stderr instead of
  sending. Disabled config → `Emit` is a cheap no-op (nil-safe: a nil *Client
  is also a no-op — server/CLI wiring relies on this).
- `storm.go` — token bucket 2/s burst 200 + throttle-event emission (≤1/min)
  per spec §5.2.
- `spool.go` — spec §5.4: gzip JSONL segments, tmp+fsync+rename, 10 MiB/7 d
  bounds oldest-first, corrupt→`*.bad` (deleted next start), oldest-first drain
  interleaved with live flushes. Reuse the file-safety idioms from
  `internal/authoring/recipe/gate_refusal_ledger.go` (pattern only — do not
  import it).
- Tests (`*_test.go` alongside): spec invariants T3, T4, T5, T6, T7 + config
  precedence table + install-file corruption → disabled + shutdown-drains-and-
  spools (use an httptest sink; simulate send failure). Seq monotonicity under
  concurrent Emit (`-race` clean).

Notes: worker owns ALL I/O (no mutex held during I/O anywhere); events carry
`client_time` RFC3339ms + `monotonic_ms` from a process-start reference;
`zcp_version` non-semver → `dev` here. HTTP 4xx → drop; 429/5xx/timeout →
spool. Emitter seam for upper layers:
`type Emitter interface { Emit(wire.Event) }` (exported here, consumed by S3/S4).

## Slice S3 — server middleware emission

Depends on S2. Files: `internal/server/server.go` (+ its tests).

Items:
1. Add `telemetry telemetry.Emitter` to `Server` (nil = disabled), injected via
   `server.New` — extend the constructor signature/options in the least
   invasive existing style; update all `New` call sites (grep: `server.New(`)
   including tests.
2. Extend `observe()` (single receiving middleware, `server.go` ~:136/:307):
   keep existing duration log + counter; add wire.Event construction per spec
   §5.3: tool name from `CallToolParamsRaw.Name`; `action` peek via
   `struct{ Action string }` unmarshal of `Arguments`; for tool
   `zerops_workflow` also peek `struct{ Step, Route string }` (single combined
   peek struct is fine); outcome from `CallToolResult.IsError`; error path:
   decode `struct{ Code string }` from `Content[0].(*mcp.TextContent).Text`,
   absent/undecodable → `UNKNOWN`; shape-invalid values → `unknown`. Wrap the
   whole extraction in `defer recover()` (spec B3); emit AFTER the handler
   returns (never in its path).
3. Tests (server package): success call emits correct event; error call carries
   the `code` from an `ErrorWire`-shaped payload; args containing fake secrets/
   paths never appear in the emitted event (spec T2 — use an in-memory Emitter
   recording events); nil Emitter = zero allocations beyond today's behavior
   (no-op path, spec T1); a panicking Emitter does not affect the tool result.

## Slice S4 — CLI single-exit refactor + lifecycle + `zcp telemetry`

Depends on S2 (and S3's constructor change). Files: `cmd/zcp/main.go`
(+ new `cmd/zcp/telemetry_cmd.go`, tests where extractable).

Items:
1. Single-exit refactor: `main()` becomes a thin wrapper around
  `run(args) int`; every `log.Fatalf` branch in the 9-way switch is replaced by
  error bubbling to one exit point (identical stderr messages + exit codes —
  behavior-preserving). One `os.Exit(code)` in `main()` AFTER telemetry flush.
2. Wire telemetry: resolve `telemetry.Resolve(...)` at process start (after
  `setupCrashLog`); construct `*telemetry.Client`; serve mode: pass Emitter
  into `server.New`, emit `session_start` after resolve, `session_end` inside
  the existing `logShutdown()` hook (uptime → `duration_ms`, shutdown reason
  enum → `dims[wire.DimShutdownReason]`, cumulative dropped → `dropped_count`);
  final `Shutdown(750ms)` after `run` returns. CLI mode: emit one `cli_command`
  event (command = first arg, action = second arg for
  `sync|schema|catalog|service|telemetry`, duration, success = exit code 0),
  flush before exit.
3. `zcp telemetry status|enable|disable|id` per spec §3.5 (human output to
  stdout — CLI is exempt from the JSON-only rule; update the usage/help text).
4. Disclosure emission per spec §3.3 (CLI → stdout, serve → stderr) happens
  inside `Resolve` (S2) — verify both modes surface it exactly once per install.
5. Tests: exit-code preservation for representative error branches (extract
  testable `run`), `telemetry` subcommand behaviors against a temp HOME, CLI
  event emission with in-memory Emitter.

## Slice S5 — ingest service

Depends on S1 only. New: `cmd/zcp-ingest/main.go` (thin) +
`internal/ingest/` (handler, limits, dedup, batcher, config) + tests.
Adds dependency `github.com/ClickHouse/clickhouse-go/v2` (go.mod; no `replace`).

Items:
1. Config from env: `LISTEN_ADDR` (default `:8080`), `CH_HOST`, `CH_PORT`
   (9000), `CH_DATABASE` (`telemetry`), `CH_USER` (`zerops`), `CH_PASSWORD`.
2. HTTP: `POST /v1/events` per spec §6 — size caps BEFORE decompress (64 KiB)
   and after (256 KiB, `io.LimitReader`), gzip per `Content-Encoding`,
   `wire.ValidateStrict` per event, per-event accept/reject counters, response
   per spec §4.4. `GET /healthz` → 200 (+ CH ping best-effort).
3. Dedup: LRU with 24 h TTL on `event_id`, size-bounded (1M entries), counted
   as `duplicate`, not inserted.
4. Limits per spec §6 (per-IP bucket 60/min burst 600 — IP from
   `X-Forwarded-For` first hop fallback `RemoteAddr`, kept ONLY in the limiter
   map, never logged/inserted — spec B6/T9; per-install daily 10k; per-session
   5k tool_call; new-install cardinality guard 1k/h/IP). In-memory with
   periodic GC.
5. Batcher: rows appended to an interface (`chInserter`) so tests mock it;
   real impl uses clickhouse-go v2 `PrepareBatch` INSERT into
   `telemetry.events` (columns per `/Users/macbook/Documents/Zerops-MCP/zcp-telemetry/schema.sql`
   — read it; server-side fields: `event_time` clamp ±30 d, `clock_skew_ms`,
   `received_at` left to CH DEFAULT), flush 5k rows or 2 s, graceful shutdown
   (SIGTERM) flushes.
6. Logging: stderr slog; no IPs, no bodies. NOTE: `cmd/` is exempt from the
   stdout-purity test, but keep stdout clean anyway (service convention).
7. Tests: spec T8 + T9 (handler-level: assert no IP in any log output using a
   captured logger, no IP field in inserted rows), caps, gzip path, dedup,
   rate-limit 429 with `retry_after_ms`, batch flush on size/interval/shutdown.

## Slice S6 — architecture guards

Depends on S1–S5. Files: `.golangci.yaml`,
`internal/topology/architecture_test.go` (or a sibling
`architecture_telemetry_test.go`), possibly `internal/topology/architecture_call_discipline_test.go`
as the AST-scan model.

Items:
1. Depguard: deny `internal/telemetry` (and `internal/telemetry/wire` implied)
   from `ops`, `workflow`, `platform`, `topology` rule blocks; deny everything
   except `internal/telemetry/wire` + stdlib + clickhouse-go for
   `internal/ingest` (new rule block, follow the existing style).
2. Architecture test additions mirroring 1 (the repo pins depguard rules in
   tests — follow the existing table).
3. `wire` stdlib-only pin (import scan, same mechanism as topology's own rule).
4. AST pin (spec T10): every struct field named `Action` in `internal/tools`
   input structs carries json tag `action` — protects the middleware peek
   contract. Model on `scanForDirectClientCalls`.
5. Run `make lint-local` (not just lint-fast) — depguard rule syntax must be
   proven, not assumed.

## Slice S7 — verification + plan-fidelity report (read-mostly)

Depends on all. Items:
1. Full gates: `go test ./... -short`, `go test ./internal/telemetry/... ./internal/ingest/... -race -count=1`,
   `make lint-local`, `go build ./...`.
2. Plan-fidelity walk: EVERY item of S1–S6 quoted against its implementation
   site — done/partial/BLOCKED, with file references. Partial-ness is the news.
3. Spec invariant walk: T1–T10 each mapped to the test that pins it (name the
   test); missing pin = finding, not a footnote.
4. Infra consistency: `schema.sql` columns == wire JSON fields + server-side
   fields; `zerops.yml` env keys == ingest config keys; `import-project.yaml`
   hostnames == zerops.yml env values (`CH_HOST: db`). Report drift.
5. Allowed fixes: trivial mechanical fallout only (a missed call site, a lint
   nit). Anything design-shaped → report, don't fix.
6. Return: the fidelity table + gate outputs + list of deviations across all
   prior slice reports.

## Out of scope for this pass (do NOT build)

Dashboards JSON provisioning; docs disclosure page; LIA document; e2e against
live ClickHouse; `error_subcode` taxonomy; sampling; any auth. (PRD §12 P1+.)
