# PARKED — anonymous usage telemetry (complete + live)

This branch is parked (2026-07-02): the whole telemetry system — client in
the zcp binary + ingest service + ClickHouse pipeline — lives here as a
single commit, not merged, not released. This file is the entry point.
Delete it when merging.

## State at parking

All gates green: tests incl. `-race`, `make lint-local` 0 issues, S7
plan-fidelity walk clean, production-hardening pass done (chaos suite,
tolerant version-skew validation, embedded CH migrations, error_subcode +
workflow-context stamping, bounded ingest retry).

## Where everything lives

- Design contract: `docs/spec-telemetry.md` (this branch) — incl. per-field
  review ledger.
- Ops guide: `docs/telemetry-runbook.md` (this branch).
- Plans: `plans/{prd,telemetry-analysis,telemetry-implementation-plan,telemetry-production-readiness}-*2026-07-02.md` (this branch).
- Infra bundle OUTSIDE the repo: `../zcp-telemetry/` (import YAML, zerops.yml
  deploy config, ops README).

## Live deployment (exists regardless of this branch)

Zerops project **zcp-telemetry** (org KRLS):
- `db` — ClickHouse HA, schema self-bootstrapped, migrations 0001/0002 applied.
- `ingest` — INTERNAL-ONLY: subdomain deliberately OFF per the R5
  no-client-secret security stance; endpoint `http://ingest:8080/v1/events`
  in-project/VPN.
- Grafana via observability stack — 4 dashboards (usage / loops /
  session-explorer / pipeline-health), ClickHouse + Infinity datasources,
  admin creds in the grafana service env.

Gotcha: the project's `zcp` test container runs a manually-installed branch
binary — any service restart wipes it back to the released (telemetry-less)
binary. Redeploy via scp + `sudo install` (build-deploy.sh style) and
re-check `~/.claude.json` mcpServers.zerops.env carries
`ZCP_TELEMETRY_ENDPOINT`/`CHANNEL`. Detail: `docs/telemetry-runbook.md`.

## Before public launch

PRD P2: disclosure page, LIA, production domain, subdomain re-enable — see
the production-readiness plan's R5 section (this branch).
