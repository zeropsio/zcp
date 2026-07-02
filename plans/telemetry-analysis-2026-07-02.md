# ZCP Anonymous Usage Telemetry — Research Analysis

Date: 2026-07-02 · Status: research synthesis feeding `prd-telemetry-2026-07-02.md`
Inputs: 5 parallel research tracks (codebase seams, eval evidence, prior art, tech
stack, privacy) + independent Codex architecture pass + cross-report critique.

## 1. What the data must answer (analytics goals)

1. **Usage overview** — which tools/actions/workflows run, how often, on which
   ZCP versions, container vs local.
2. **Loop detection** — sessions where the agent hammers the same tool/action
   (eval ground truth: 20–140+ repeats observed in real runs).
3. **Error pareto** — which stable error codes dominate, per tool/version.
4. **Funnels** — bootstrap → deploy → success per session.
5. **Version adoption** — how fast releases roll out (auto-update efficacy).

## 2. Codebase findings (seams track — all claims file:line-verified)

- **MCP path has ONE existing choke point**: `srv.AddReceivingMiddleware(s.observe())`
  (`internal/server/server.go:136`). All 24 tools (incl. ZCP_AUTHORING-gated ones)
  register on the same `mcp.Server`; `observe()` already measures duration and counts
  calls, but discards tool name and outcome. Telemetry = extending this middleware,
  zero per-tool wiring.
- **Tool name is free** (`CallToolParamsRaw.Name`); **action needs a 1-field JSON peek**
  (`{Action string}` tiny struct against `Arguments json.RawMessage`) — reads exactly
  one allowlisted enum key, never the full args. **Outcome**: `CallToolResult.IsError`
  is free; the stable machine `code` (37-code catalog, `internal/platform/errors.go`)
  requires decoding `Content[0].Text` JSON on the error path only (`convertError`
  puts `ErrorWire` there, not in `StructuredContent`).
- **CLI path has NO choke point**: `cmd/zcp/main.go` is a hand-rolled 9-way switch;
  several branches `log.Fatalf` (= `os.Exit`, defers skipped). Reliable CLI telemetry
  needs the single-exit-point refactor (bubble errors to `main`), which is also the
  Clean-Code-correct shape.
- **Lifecycle hooks exist**: `setupCrashLog()` at start (mints into `~/.zcp/`),
  `logShutdown()` after `run()` — already computes uptime + shutdown reason; a final
  flush drops in naturally. `signal.NotifyContext` handles SIGINT/SIGTERM.
- **Work Session ≠ telemetry session.** WorkSession is "one LLM task per process",
  close is *derived* (no synchronous close event to hook). Telemetry session = OS
  process lifetime, self-minted UUID. Do not conflate.
- **Install-id home**: `~/.zcp/` (precedent: `serve.log`), NOT `~/.cache/zcp/`
  (cache semantics, evictable) and NOT `.zcp/state/` (per-project).
- **Patterns to copy**: `runtime.Detect()` resolve-once-thread-as-value for the
  consent gate (exactly how ZCP_AUTHORING avoids drift); `update.Once` fire-and-forget
  background goroutine ("no MCP tool, no notification to LLM"); the authoring
  `gate_refusal_ledger.go` file-safety idioms (append-only, stderr-on-failure) for
  the spool. `TestNoStdoutOutsideJSONPath` automatically covers `internal/telemetry/`.
- **Layering**: cross-cutting peer like `internal/update`; call sites L4-only
  (`cmd/zcp`, `internal/server`). No depguard edit mechanically required; we ADD deny
  entries so `ops`/`workflow` can never import telemetry (enforces single choke point).

## 3. Eval evidence (486 runs scanned; error codes cross-greped in transcripts)

Real loop/friction patterns and whether tuple-repetition telemetry catches them:

| Pattern (freq) | Shape in telemetry | Detectable? |
|---|---|---|
| Adopt-discover ambiguity bounce (97) | `workflow/complete/discover` + `INVALID_PARAMETER` ×2 | yes |
| git-push-setup blind-guess loop (36) | `git-push-setup` + `GIT_TOKEN_INVALID` ×3-4 | yes |
| ToolSearch cold-start bounce (141) | harness-level, not a `zerops_*` tool | out of surface by construction |
| Deploy→import-override dance (16) | alternating `deploy`+`DIAGNOSIS_REQUIRED` / `import` | yes |
| zcli/SSH escape hatch (24) | `SSH_DEPLOY_FAILED` then session silence | **only the precursor + absence** |
| zerops.yaml self-fix retry (23) | `INVALID_ZEROPS_YML` then quick 2nd deploy | yes (+ gap timing) |
| Recipe plan-type mismatch → reset (~10) | `INVALID_PARAMETER` ×N then route change | yes (needs `route` dim) |

Two structural limits confirmed:
1. **The zcli/raw-API escape is invisible in principle** — those calls never enter
   ZCP. Telemetry sees the failing precursor and the silence after. (The flow-eval
   RED-FLAG rule stays the only direct detector for this.)
2. **`INVALID_PARAMETER` conflates ≥3 distinct root causes.** A stable
   `error_subcode` enum (allowlisted dim) in the error path would multiply the
   value of the error pareto — enum-shaped, in-scope, scheduled post-v1.
3. Telling "identical stuck retry" from "progressive-guess retry" needs an args
   hash — touches the locked NO-arguments line; explicitly NOT in v1 (see PRD §15).

Bonus: eval runs are **ground truth with known loops** — the loop-detection SQL gets
validated against them before any external data exists (rollout phase P1 gate).

## 4. Prior art (what we adopt / avoid)

Adopt: DO_NOT_TRACK + `ZCP_TELEMETRY=0` (freeze names forever — Salesforce broke
opt-out by renaming); `ZCP_TELEMETRY_DEBUG=1` prints payloads instead of sending
(the single highest-trust/lowest-cost feature, universal among trusted tools);
disclosure-gates-first-event (Homebrew 2016 + .NET 2020 + Go 2023 all cratered on
*ordering*, not on the data itself); publish the exact event JSON shape (Vercel leaked
a plaintext `teamId` through a "useful" field — schema is a public API needing review
discipline); never re-nag after opt-out (Homebrew 2023 lesson).
Considered + not adopted for v1: Go-toolchain-style local counter aggregation
(defeats sequence/funnel analysis — our primary goal needs raw ordered events);
Turborepo-style salted arg-hashing (touches the locked no-arguments line).

## 5. Technology (Zerops-verified against ../zerops-docs)

- **ClickHouse is a managed Zerops service**: `clickhouse@25.3`, HA (3 replicas,
  ReplicatedMergeTree) or single-node; env `portHttp:8123` / native `9000`,
  `password`/`superUserPassword`. Verdict: dogfooding works, no external vendor.
- **Grafana** ships inside the one-click "Advanced Observability" stack (prometheus +
  grafana + grafanadb + backup bucket), not as a standalone recipe — deploy the stack
  in the telemetry project, add the official `grafana-clickhouse-datasource` plugin
  (Grafana-Labs-maintained, production-grade, alerting included).
- **Object storage** (MinIO, S3-compatible) available for cold archive later;
  ClickHouse docs already show `S3()`-based backup/restore against Zerops storage.
- **Ingest**: client → small Go ingest service → ClickHouse via `clickhouse-go` v2
  native-protocol batches (~5-10k rows or 1-5s). Direct client→ClickHouse rejected:
  unrevocable shared credential in a public binary, no server-side allowlist owner,
  no IP hygiene point, wrong insert pattern. `ch-go` unnecessary at 1-10M events/day.
- **Schema**: raw events `ReplacingMergeTree(ingest_version)` ORDER BY
  `(session_id, seq)` (loop/funnel locality + dedup), PARTITION BY month, TTL 15
  months; hot dims as `LowCardinality` columns, long-tail allowlisted dims as
  `Map(LowCardinality,LowCardinality)`; AggregatingMergeTree MVs for daily dashboards
  (kept indefinitely — pure anonymous aggregates).

## 6. Privacy verdict

Design is GDPR-defensible (matches VS Code / Vercel / Homebrew / .NET posture) with
these non-negotiables: (1) **never persist raw IP** — Breyer-style re-identification
is the one place personal-data risk enters; in-memory rate-limit use only, scrubbed
access logs, no hash-and-store; (2) **no event before disclosure has fired on that
install**; (3) documented **Legitimate Interests Assessment** on file (Art 6(1)(f));
(4) **public disclosure page with the exact event schema** + opt-out + retention +
erasure-by-install-id mechanics; (5) retention biased low — raw 15 months justified
by YoY version-adoption analysis, aggregates forever. ePrivacy Art 5(3) technically
engages (install-id file on terminal equipment; analytics ≠ "strictly necessary") —
the entire CLI industry makes the same calculated bet; we file a CNIL-style
audience-measurement fallback argument internally.

## 7. Critique gaps → resolutions (all 10 dispositioned in the PRD)

| # | Gap | Resolution |
|---|---|---|
| 1 | Disclosure channel when the "user" is an LLM | Dual-path: first process after install/update prints disclosure (CLI stdout / serve stderr), stamps marker, emits NOTHING; events start with the next process. `zcp init` adds a one-line AGENTS.md note. PRD §10 |
| 2 | Eval/internal traffic pollutes data | `ZCP_TELEMETRY_CHANNEL` (external default / internal_dev / internal_eval / ci) + separate internal install-id file; dashboards default-filter to external. PRD §11 |
| 3 | Buffer vs exit-flush inconsistency | Near-real-time: bounded queue 4096 + 1 worker, flush 5s/100 events, spool on failure; shutdown = final best-effort flush (750ms), no durability promise on SIGKILL. PRD §5 |
| 4 | CLI `log.Fatalf` bypasses defers | Single-exit-point refactor of `cmd/zcp/main.go` (bubble errors); it is also the standalone-correct cleanup. PRD §5.6 |
| 5 | Sampling vs loop-detection tension | NO sampling; tail bounded instead by client storm guard (2/s, burst 200) + per-session server cap. Loop signal preserved via burst + `client_throttle` counter events. PRD §5.4 |
| 6 | "Public write token" is a shared secret one hop back | No token at all — client auth is theater (Codex concurs); defense = strict schema, enum allowlists, per-IP + per-install rate limits, cardinality guards. PRD §7 |
| 7 | Rate limiter could eat the very loops we hunt | Storm guard emits throttle events carrying dropped counts; burst 200 ≫ the 8-repeat detection threshold; server limits sit far above storm-guard output. PRD §5.4 |
| 8 | No rollout plan | 3-phase: internal-channel dark launch → pipeline validated against eval ground truth → external default-on release with disclosure. PRD §12 |
| 9 | No wire versioning designed | `protocol_version` (envelope) + `schema_version` (event) + additive-only columns + ingest normalization for ≥18 months of old clients. PRD §6 |
| 10 | Infra access logs may persist IPs | Open verification item with the platform team: Zerops ingress/L7 logging behavior for the ingest endpoint; app-level guarantees don't cover infra logs. PRD §15 |

## 8. Single-owner principle applied (Information Contract)

The wire schema + dims allowlist live in ONE package (`internal/telemetry/wire`,
stdlib-only), imported by both the client emitter (the TELL — what gets sent) and the
ingest service (the CHECK — what gets accepted), both built from this repo. They
cannot drift. New dims = one edit + review; ingest redeploy precedes client release.
