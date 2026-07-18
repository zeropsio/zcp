# Telemetry Production Readiness — plan

Date: 2026-07-02 · Status: PROPOSED · Follows: `prd-telemetry-2026-07-02.md` (P0
built + live in zcp-telemetry project), spec: `docs/spec-telemetry.md`.
Driver: four launch requirements from the user + one security finding.

## Requirements (user, verbatim intent)

- R1 Telemetry must NEVER take a project down (user's project or the pipeline).
- R2 Survive rapid schema iteration: many bugfixes/changes, DOZENS of client
  versions active simultaneously, without breakage.
- R3 Everything correctly documented; a human must be able to READ the Grafana
  boards; a full review pass of what is logged.
- R4 Every logged value must earn its place — the data must let us actually
  DIAGNOSE what went wrong, not just count that something did.
- R5 (security finding) Public unauthenticated ingest endpoint — take a stance.

## R5 stance — decided

A shared secret / HMAC key shipped inside a public binary is extractable in
minutes (strings on the binary, or MITM of one's own client); it stops nobody
deliberate, only random scanner noise — which strict schema validation already
rejects with 400. It is auth THEATER; PRD §3/§7 and the prior-art research
(Homebrew/Go/Next.js — none ship client secrets) already rejected it.
"Internal-network only" would kill the product at launch (the clients ARE the
public installs). Position:

- **Internal phase (now): accept the recommendation's spirit** — subdomain
  DISABLED, ingest accepts only in-project traffic (`http://ingest:8080`);
  container + VPN clients use the internal URL. Zero public surface while the
  data is test noise. (Done 2026-07-02.)
- **Launch (public): no client secret, defense where it works** — strict
  validation, per-IP + per-install + cardinality limits (exist), poisoning-
  resistant analytics (uniq-by-install everywhere, exist), pipeline-health
  dashboard with abuse anomaly panels (S6), server-side env-configurable
  install-id/IP blocklist as an ops lever (S2), and the kill switch is
  subdomain-off (instant, tested). Optionally a static noise-filter header,
  explicitly documented as NOT security. Rationale lives in spec §6.

## Verified current-state gaps feeding the slices

- G1 Ingest acks 202 then a failed CH insert silently DROPS rows
  (`batcher.flush` logs + swallows; live-hit during rollout: rows=2 lost).
  Memory IS bounded (buffer drains every 2 s even on failure) — the gap is
  loss without retry or accounting, not OOM.
- G2 Ingest strict validation rejects unknown dim keys / enum values → a NEWER
  client vs OLDER ingest drops whole events (client discards on 4xx). Deadly
  under R2's "dozens of versions" reality.
- G3 CH schema changes have NO migration mechanism (bootstrap is
  IF-NOT-EXISTS-only; the tool_daily/command change needed a manual DROP dance
  + hit the restart race live).
- G4 `INVALID_PARAMETER`-class codes conflate ≥3 root causes (eval evidence);
  tool_call events outside `zerops_workflow` carry no route/step context —
  diagnosis dead-ends (R4).
- G5 Dashboards lack panel descriptions/units; no per-session drill-down —
  a human cannot reconstruct "what happened in this session" (R3/R4).
- G6 `monotonic_ms` is emitted on the wire but never stored/used — review
  every field (R3): each is kept-with-purpose or removed.
- G7 CLI one-shot processes never drain the spool (drain only fires on 5 s
  worker ticks); a CLI-only user's spool ages out at 7 d. Serve-mode drains
  fine. Decide: drain-one-segment on Shutdown, or accept + document.

## Slices (sequential, Sonnet 5 executors; gates per repo rules)

### S1 — client "never harm the host" hardening + chaos pins (R1)
- Chaos test table (new `internal/telemetry/chaos_test.go`): unwritable HOME,
  read-only FS mid-run, disk-full spool write, corrupt install.json + spool,
  invalid `ZCP_TELEMETRY_ENDPOINT` URL, blackholed endpoint (connect timeout),
  slow-loris server (header stall > 2 s), TLS failure, Emit-after-Shutdown,
  concurrent Emit storm during Shutdown, HOME with spaces/unicode. EVERY case
  asserts: no panic, no error surfaces to caller, tool path unaffected,
  process exits promptly.
- Benchmark pin: `Emit` disabled ≤ ~100 ns, enabled non-blocking path ≤ ~5 µs;
  document numbers in spec §9 T1.
- Guard Emit against closed-channel panic explicitly (recover exists — add the
  direct pin).
- G7 decision: implement drain-one-segment attempt inside `Shutdown` after the
  queue flush, budget-permitting (spec §5.5 edit) — CLI-only installs stop
  leaking spool.

### S2 — ingest resilience + tolerant validation + ops levers (R1, R2, R5)
- G1: bounded failed-batch retry — failed insert rows go to a bounded retry
  buffer (cap ~50k rows, drop-oldest with counter), retried next flush;
  `rows_dropped_total` + `insert_failures_total` surfaced in logs +
  `/healthz` body (pipeline-health panel reads them via a tiny `/statsz`
  JSON — counts only, no content).
- G2: tolerant mode per spec update — unknown dim KEY: drop the dim, keep the
  event, count `dims_dropped`; unknown values of open-shape fields: coerce
  `unknown`; unknown `schema_version` NEWER than supported: accept when the
  version-1 core (ids, times, event_type) parses, stash unrecognized fields
  nowhere (dropped), count `forward_compat_accepted` — reject only structural
  garbage. Closed structural enums (event_type) stay strict.
- R5: env-configurable blocklists `INGEST_BLOCK_INSTALLS`, `INGEST_BLOCK_IPS`
  (comma-separated; in-memory match; 403; never logged).
- Release-ordering guard: 202 response gains `max_schema_version`; client
  stderr-warns once per process when server < client (visibility for the
  "forgot to deploy ingest first" mistake).

### S3 — ClickHouse migration mechanism (R2, fixes G3)
- `telemetry.schema_migrations` table (id, applied_at) + ordered idempotent
  migration steps embedded in ingest (`internal/ingest/migrations/*.sql`,
  numbered); bootstrap = base schema (fresh installs) THEN pending migrations
  (existing installs). Baseline migration 0001 records current shape.
- Restart-race note dies: migrations are versioned, re-runs are no-ops.
- Spec §7/§8 updated: "additive-only" now enforced BY MECHANISM (a migration),
  not by convention.

### S4 — diagnosis-grade signal (R4, fixes G4, G6)
- `error_subcode`: extend `ErrorWire` with optional stable `subcode` (enum,
  catalog in `internal/platform/errors.go` next to codes); populate the
  worst conflations first: INVALID_PARAMETER → {AMBIGUOUS_SCOPE,
  PLAN_TYPE_MISMATCH, WORKER_PLAN_SHAPE}, API_ERROR → carry platform
  error-code class. Middleware peeks `subcode` like `code`. Wire: new
  optional field (additive, schema_version stays 1 per §8 policy).
- Workflow-context stamping: the server middleware remembers the last
  route/step seen from a `zerops_workflow` call per PROCESS (one session) and
  stamps `workflow_route`/`workflow_step` onto every subsequent tool_call —
  every deploy/import failure becomes attributable to its workflow phase.
  Enum-only, no argument reading beyond the existing peeks.
- G6: REMOVE `monotonic_ms` from the wire (no consumer; seq covers ordering) —
  doubles as the first live exercise of the additive/removal policy: ingest
  tolerates its absence AND presence (S2 tolerant mode proves R2 works).
- Field review ledger: table in the spec appendix — every event field:
  purpose, example, dashboard(s) using it, verdict kept/removed.

### S5 — documentation (R3)
- Spec §4 gains the field-by-field reference (the review ledger from S4).
- `docs/telemetry-runbook.md`: operating the pipeline — deploy order (ingest
  BEFORE client release, always), migration flow, blocklist use, outage
  behavior (what clients do, what recovers automatically, what data is lost
  when), reading /statsz, the binary-wipe-on-restart caveat for the test
  container.
- Update `../zcp-telemetry/README.md` to match (internal endpoint, subdomain
  policy per R5 stance).

### S6 — Grafana v2 (R3, R4) — done inline by the orchestrator, not an agent
- Every panel gets `description` (what it shows, how to read it, what "bad"
  looks like), proper units (ms, percentunit), sane legends.
- NEW "Session Explorer" dashboard: `$session` variable → ordered event
  timeline (seq, event_type, tool+command/action, route/step, success,
  error_code+subcode, duration, gap-to-previous) — answers "co se v té session
  stalo" in one view.
- NEW "Sessions overview": last-N sessions table (start, duration, events,
  errors, top error, deepest funnel stage, channel, version) with data-link
  into Session Explorer.
- Pipeline health board: ingest accept/reject/duplicate/dropped counters
  (from /statsz), insert failures, forward-compat accepts, blocklist hits,
  clock-skew distribution.
- Version board: adoption + per-version failure-rate comparison (R2 gives
  dozens of versions — the board that tells us WHICH version broke).

### S7 — verify + live re-rollout
- Full gates incl. -race; plan-fidelity walk S1–S6; redeploy ingest + zcp
  container binary; live E2E: version-skew simulation (old client payload vs
  new ingest, new-client payload vs current ingest), CH-outage drill (stop
  db? use wrong CH_HOST env on a scratch ingest) proving bounded retry +
  counters; session-explorer populated by a real container session.

## Out of scope (explicit)
- Public launch itself (P2 of the PRD: disclosure page, LIA, release).
- Auth beyond the R5 stance. Sampling. Remote config.

## Order & gates
S1 → S2 → S3 → S4 → S5 (agents, sequential, same tree) · S6 orchestrator ·
S7 verify. Each slice: `go build ./... && go test ./... -short && make
lint-fast && make lint-local` green before the next starts.
