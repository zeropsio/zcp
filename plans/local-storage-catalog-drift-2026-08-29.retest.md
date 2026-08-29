# Retest pack: local-storage-catalog-drift

Zero-context bar: run from `/Users/macbook/Documents/Zerops-MCP/zcp-local-storage-catalog` on branch `investigate/local-storage-catalog`. Do not run live mutation tests until the token/SSH alias is confirmed to target the disposable `eval-zcp` project.

## Preflight

1. Confirm `git rev-parse --short HEAD` is `12297e23` or a reviewed descendant.
2. Do not configure a Zerops token or GitHub Environment for schema drift; the workflow must use only public endpoints.
3. Run deploy E2E only after confirming its credential resolves the documented disposable `eval-zcp` project, not `z3-eval` or `zcp-eval-clean`.

## Run

| command | expected line |
|---|---|
| `go test ./internal/topology ./internal/schema ./internal/workflow ./internal/tools -run 'LocalStorage|CatalogStorageAlwaysManaged' -short -count=1 -v` | `PASS` in every package |
| `go test ./internal/knowledge ./internal/ops/bundle ./internal/authoring/port ./internal/authoring/recipe ./internal/dataconsole/console/provider ./internal/dataconsole/zcpadapter -run 'LocalStorage|HAIncapable|ServiceProfiles' -short -count=1 -v` | `PASS` in every package |
| `go test ./cmd/zcp -run 'Schema(Check|Commands|DriftWorkflow)' -count=1 -v` | `PASS` |
| `go test -tags api ./internal/schema -run '^TestLiveAllTypesAudit$' -count=1 -v` | `REJECTED real service types (catalog false-reject = BUG): 0` |
| `env -u ZCP_API_KEY -u ZEROPS_SCHEMA_CHECK_TOKEN go run ./cmd/zcp schema check --strict` | `schema check: OK — committed schema matches live` |
| `make test-race` | `ok` on every package; no `FAIL` or `DATA RACE` |
| `make lint-local` | `0 issues.` |
| `make vet-tags` | both `go vet` commands finish without findings |
| `make e2e-zcp-fast` | final `PASS` |
| `make e2e-zcp-deploy` | final `PASS` — run only after the `eval-zcp` preflight above |

## Drive

1. AC1/AC2 — run `go test ./internal/knowledge -run '^TestFormatStackList_LocalStorageSingle_PreservesExactIdentity$' -count=1 -v`; expect the exact catalog identity test to pass and no `local-storage:ha@1` suggestion.
2. AC3/AC4 — run the second command in the table; expect mounted/unmounted volume guidance, exact single-only authoring, HA exclusion, and unsupported file-family Data Console tests to pass.
3. AC5 — run `env -u ZCP_API_KEY -u ZEROPS_SCHEMA_CHECK_TOKEN go run ./cmd/zcp schema sync`, then strict check; expect 214 versions and `schema check: OK` with no credential.
4. AC6 — GitHub Actions → **Schema Drift** → **Run workflow**, selecting `main`; expect the job to end `OK` without a GitHub Environment or Zerops secret. A public endpoint/poison failure must make strict mode red.

## What changed

- S1/S3: Local Storage has first-class topology/catalog handling and refreshed public schema/catalog artifacts.
- S2: bundle, launch, authoring, port hardening, knowledge, and Data Console preserve its exact mounted single-only semantics.
- S4/S5: schema runtime, sync, drift, catalog projection, and CI use the public schemas only; the ACTIVE API/filter and all schema-drift credentials were removed.
- Verification hardening: E2E exports no longer print vault bodies; stale build-tagged call sites compile again.

## Current live blocker

The 2026-08-29 local run reached `z3-eval`, not the documented `eval-zcp` rig. Generic deploy E2E then failed on unadopted source state, transient `CREATING` provision checks, and absent auto-subdomain output. Do not weaken Local Storage assertions to make that unrelated rig pass; correct the live target/fixture and rerun the exact deploy command.

## Rollback

`git revert 5678a700^..12297e23`

No external credential or GitHub Environment cleanup is required because this branch provisions neither.

## Docs

Promoted contracts: `docs/schema-integration.md` (Local Storage identity + tokenless public drift); `docs/spec-knowledge-architecture.md` §3.1; `docs/spec-workflows.md` §2.3, §9.4, §10; `docs/spec-oss-port-flow.md` §A2/§5; `docs/spec-dataconsole.md` §6; `docs/spec-dataconsole-testing.md` §2.
