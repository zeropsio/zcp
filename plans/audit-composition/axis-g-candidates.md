# Phase 5 axis-G candidates — Codex CORPUS-SCAN (2026-04-26)

Round type: CORPUS-SCAN per §10.1 Phase 5 row 1
Reviewer: Codex
Inputs read: `CLAUDE.md`, `CLAUDE.local.md`, `internal/topology/types.go`, `internal/platform/types.go`, `internal/tools/discover.go`, `internal/ops/discover.go`, `internal/ops/helpers.go`, `internal/tools/logs.go`, `internal/tools/env.go`, `internal/tools/knowledge.go`, and all 79 files under `internal/content/atoms/*.md`.

## Per-atom audit

### `bootstrap-env-var-discovery`

- **Atom catalog at file:line 25-41**: Embedded managed-service env-var key catalog by service type, including PostgreSQL/MariaDB/Valkey/KeyDB/NATS/Kafka/ClickHouse/search/object-storage/shared-storage keys. Atom scope is bootstrap-active classic/adopt provision at lines 1-7, and already instructs `zerops_discover includeEnvs=true` at lines 17-19.
- **Authoritative tool**: `zerops_discover includeEnvs=true`; the tool schema says omitting `service` lists all services and `includeEnvs` returns env var keys at `internal/tools/discover.go:38-43`, the tool calls `ops.Discover` at `internal/tools/discover.go:47-59`, and `ops.Discover` attaches service/project envs when `includeEnvs` is true at `internal/ops/discover.go:47-57`, `internal/ops/discover.go:99-113`, `internal/ops/discover.go:233-253`; keys-only output is produced by `envVarsToMaps` at `internal/ops/helpers.go:139-160`.
- **Recoverable bytes**: 1960 B
- **Envelope-context check**: CALLABLE. This atom fires at bootstrap provision after import/adopt planning, and `zerops_discover` can be called without a hostname to list all services/env keys, per `service` omission semantics at `internal/tools/discover.go:38-43` and all-services path at `internal/ops/discover.go:99-113`.
- **Disposition**: REPLACE-WITH-TOOL-LINK

## Total recoverable bytes

| Bucket | Count | Bytes |
|---|---:|---:|
| REPLACE-WITH-TOOL-LINK | 1 | 1960 B |
| REPLACE-WITH-FRONTMATTER (references-fields) | 0 | 0 B |
| KEEP-AS-IS-WITH-REASON | 0 | 0 B |
| Total Phase 5 recoverable | 1 | **1960 B** |

## Phase 5 work plan (priority by bytes)

1. `bootstrap-env-var-discovery` — 1960 B — delete the static service-type env-var table and keep a terse instruction to call `zerops_discover includeEnvs=true`, then record keys from the live response.

## Risks + watch items

- The env-var table is safe to remove only in the provision envelope. If similar guidance fires during bootstrap discover before services exist, it must stay or move later; this atom's own frontmatter is provision-scoped at `internal/content/atoms/bootstrap-env-var-discovery.md:1-7`.
- Replacing the table with a tool-link can cost an extra LLM/tool round-trip if the agent would otherwise rely on the embedded table, but this atom already requires the discover call at `internal/content/atoms/bootstrap-env-var-discovery.md:17-23`, so the intended path should not add a new call.
- I did not claim service-status or deploy-state duplication from `internal/topology/types.go`: it defines `RuntimeClass` at lines 3-12, `Mode` at lines 14-29, `DeployStrategy` at lines 31-53, and `PushGitTrigger` at lines 55-67. Wire service/build/process status constants are in `internal/platform/types.go:119-139`.
- References-fields limits: Go symbol renames are caught by the atom reference-field integrity path, but semantic drift is not; a static prose table can still become stale even when referenced Go fields remain valid.
