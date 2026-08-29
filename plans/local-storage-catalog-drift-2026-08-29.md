# Plan: local-storage-catalog-drift

## Run State
- `phase:` assemble
- `base:` 1d863df5577990a00832e81347d81e61c4d8ebef
- `integration:` 92b74a6e^..d9b6c402
- `approved:` Rev-4, 2026-08-29 — owner clarified that only schema drift/sync must lose the token; normal runtime must retain the authenticated ACTIVE availability overlay
- `codex:` REVISE incorporated — `plans/local-storage-catalog-drift-2026-08-29.codex-review.md`
- `next:` schema/public-authority work is verified; formal whole-feature handoff still requires rerunning the pre-existing deploy E2E against the documented `eval-zcp` rig
<!-- material edit to Frame or Slice Register after approval resets phase to awaiting-approval -->

## Frame
**Outcome**: ZCP accepts and preserves `local-storage:single@1` as first-class managed Local Storage; committed schema artifacts and drift mirror the unauthenticated public schemas, while the normal runtime cache overlays the authenticated ACTIVE service-version set before exposing its catalog.

| obs | evidence |
|---|---|
| The live schema and official docs name the concrete type `local-storage:single@1`; the ACTIVE endpoint reports the same identifier. | [KB:`../zerops-docs/apps/docs/content/local-storage/overview.mdx:15-25`], live schema GET + authenticated service-stack-type search (2026-08-29) |
| Exact live enum members are rejected because `local-storage` is absent from topology's storage/managed classification and the catalog matcher gates `:` forms on that classification. | [SELF-VERIFIED:`internal/schema/catalog.go:90-109`; `internal/topology/predicates.go:5-52`; live `TestLiveAllTypesAudit`] |
| Local Storage is a distinct managed storage: single-only, no profile/horizontal containers, vertical CPU/RAM/disk scaling, volume mounting rather than generated connection envs. | [KB:`../zerops-docs/apps/docs/content/local-storage/overview.mdx:9-67`; live import-schema condition] |
| Before this work the committed schemas/catalog were last synced 2026-06-14 and the then-authenticated `schema check` reported drift in both schemas. | [SELF-VERIFIED:git history for `internal/schema/testdata/import_yml_schema.json` + the former `internal/knowledge/testdata/active_versions.json`; pre-change `go run ./cmd/zcp schema check`] |
| Scheduled CI supplies no Zerops credential; missing or invalid auth is converted to exit 0 `SKIP`. | [SELF-VERIFIED:`.github/workflows/schema-drift.yml:17-31`; `cmd/zcp/schema.go:89-104`; clean-env reproduction] |
| The live public import schema has 204 service-type enum values while the former ACTIVE-filtered committed copy has 184; the enum nodes carry no activity status and `local-storage:single@1` is present in both sets. | [SELF-VERIFIED:live public schema GET + deterministic enum comparison, 2026-08-29] |

- AC1: `local-storage:single@1` is schema-valid, managed infrastructure, Local Storage (not runtime/DB/object/shared), and bootstrap dependency validation accepts it without connection-env readiness requirements; its composite syntax is admissible while legacy mode/HA capability remains false — planned evidence: topology/schema/workflow/tool unit tests + live audit.
- AC2: catalog and version-check surfaces show the exact single-only Local Storage identifier without implying an OS prefix or HA variant — planned evidence: knowledge formatting goldens/tests.
- AC3: export/launch/bundle and authoring preserve `local-storage:single@1`, never fabricate `:ha`, legacy `mode:`, DB profile/env semantics, and count `run.volume.hostname` as a dependency reference — planned evidence: ops/tool/authoring tests.
- AC4: secondary managed-service registries classify Local Storage as file storage with unsupported Data Console access, not an unknown/DB family — planned evidence: provider/adapter tests.
- AC5: embedded schemas and the derived version snapshot are exact projections of one unauthenticated public-schema fetch, while the normal runtime cache separately removes inactive concrete service versions and preserves active `local-storage:single@1` — planned evidence: tokenless sync/hash comparison + runtime provider/filter tests.
- AC6: scheduled schema drift carries no Zerops/GitHub Environment secret; strict mode exits non-zero only when the public schema is unavailable/invalid, while drift remains exit 2 and the non-strict local command keeps skip-on-fetch-failure compatibility — planned evidence: command tests + clean-env drive + workflow inspection.

**Non-goals**: provision or mount a live Local Storage service; add a bare `local-storage@1` authoring alias; redesign all storage taxonomy; create a separate availability drift workflow · **Constraints**: public schemas own committed artifacts and syntax/existence; the runtime ACTIVE read owns current concrete service-version availability; topology owns classification; schema tooling performs no authenticated platform call; no live platform mutation; RED→GREEN→REFACTOR.

**Risk class**: high — trigger: security/credential surface + owner asked

**Assumptions**:
- [VERIFIED] `local-storage:single@1` is ACTIVE and appears exactly in the live import schema — live GET/search evidence recorded above.
- [VERIFIED] Local Storage is single-only and vertical-autoscaling-capable but profile/horizontal-container-incapable — official docs + live schema condition.
- [VERIFIED] Runtime schema cache live-fetches first and uses embedded schemas only as its cold/failure floor — `internal/schema/cache.go:44-132`.
- [VERIFIED] The normal server already owns an authenticated `platform.Client`; its read-only `/service-stack-type/search` result previously powered runtime filtering without a separate credential contract — pre-change `internal/server/server.go` + live ACTIVE query evidence.
- [VERIFIED] Schema sync/check run independently of the runtime client and can compare exact public artifacts without credentials — `cmd/zcp/schema.go` + clean-env strict drive.

**Design decision**: model `local-storage` as a third first-class storage kind and give agent-facing catalog output its exact single-only composite identifier. Separate “may carry a composite `:single|ha` token” from “accepts legacy mode / has HA capability”, so exact Local Storage syntax is accepted without lying to HA consumers. Schema freshness is a byte-stable comparison of canonicalized public schemas against their embedded copies. Current provisionability is a separate runtime-only ACTIVE overlay supplied by the already-authenticated server client; it does not participate in sync, drift, committed artifacts, or CI. Strict mode remains a public fetch/poison policy, not an authentication policy.

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| Public artifact authority and runtime availability are separate concerns. | AC5-AC6 | repo + prior live evidence | exact public/filtered enum comparison + runtime wiring trace | public=204, ACTIVE-filtered=184; Local Storage present in both | CONFIRMED | `docs/schema-integration.md` + runtime filter tests |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | Local Storage core tracer bullet | — | `internal/topology/{predicates.go,type_equivalence.go,types_test.go,type_equivalence_test.go,runtime_class_test.go}`; `internal/schema/{catalog.go,local_storage_test.go}`; `internal/workflow/validate_test.go`; `internal/knowledge/{catalog_view.go,versions_format.go,versions.go,versions_test.go}`; `internal/tools/{workflow_checks.go,workflow_checks_test.go}` | unit/tool/integration | autonomous | landed |
| S3 | Refresh schema and catalog artifacts | S1 | `internal/schema/testdata/{zerops_yml_schema.json,import_yml_schema.json}`; `internal/knowledge/testdata/schema_versions.json`; `internal/schema/{catalog_test.go,schema_test.go}` | unit/integration | autonomous | landed |
| S2 | Downstream bundle, authoring, and console parity | S3 | `internal/ops/bundle/{rules.go,rules_test.go,inputs.go,classify.go,export_test.go,launch.go,launch_test.go}`; `internal/tools/{workflow.go,launch_bundle_compose.go,launch_ready_consent_test.go,launch_readiness.go,launch_readiness_test.go,workflow_launch_production.go}`; `internal/authoring/recipe/{plan.go,yaml_emitter.go,yaml_emitter_test.go,slot_shape_authoring.go,slot_shape_authoring_test.go,briefs_tier_facts.go,briefs_tier_facts_test.go}`; `internal/authoring/port/{capture.go,capture_test.go,harden.go,harden_test.go}`; `internal/dataconsole/console/provider/{profiles.go,family_test.go}`; `internal/dataconsole/zcpadapter/adapter_test.go` | unit/tool/integration | review | landed |
| S4 | Strict schema-drift CI | — | `cmd/zcp/{schema.go,schema_test.go}`; `.github/workflows/schema-drift.yml` | unit | review | landed |
| S5 | Make committed schema artifacts and drift public-only | S4 | `cmd/zcp/{schema.go,schema_test.go,agent.go}`; `internal/schema/sync.go`; `.github/workflows/schema-drift.yml`; `Makefile`; `internal/{catalog,knowledge}` comments/testdata | unit/integration | review | landed |
| S6 | Restore runtime ACTIVE availability overlay | S5 | `internal/schema/{active_filter.go,active_filter_test.go,cache.go,public_authority_test.go}`; `internal/platform/{client.go,zerops_search.go,active_versions_test.go,mock.go,mock_methods.go}`; `internal/server/server.go`; cache call-site tests; `docs/{schema-integration.md,spec-knowledge-architecture.md}` | platform/unit/integration | owner | landed |

Gate ∈ autonomous|review|owner · State ∈ pending|building|landed|blocked. Overlapping `Files` never share a wave.

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | `go test ./internal/topology ./internal/schema ./internal/workflow ./internal/tools -run 'LocalStorage|CatalogStorageAlwaysManaged' -short -count=1 -v` | passed | all named Local Storage topology, schema, workflow, readiness, and volume-guidance tests emitted `--- PASS` |
| AC2 | `go test ./internal/knowledge -run 'LocalStorage' -short -count=1 -v` after corpus sync | passed | `TestFormatStackList_LocalStorageSingle_PreservesExactIdentity` emitted `--- PASS` |
| AC3 | `go test ./internal/ops/bundle ./internal/tools ./internal/authoring/port ./internal/authoring/recipe -run 'LocalStorage|HAIncapable' -short -count=1 -v` (mounted + unmounted guidance; exact emit; vertical scaling; no mode/profile/HA) | passed | all bundle, launch, port, hostname, HA-exclusion, and schema-valid emitter tests emitted `--- PASS` |
| AC4 | `go test ./internal/dataconsole/console/provider ./internal/dataconsole/zcpadapter -run 'LocalStorage|ServiceProfiles' -short -count=1 -v` | passed | file-family unsupported profile and adapter connection tests emitted `--- PASS` |
| AC5 | tokenless `schema sync`; independent canonical hash comparison; runtime ACTIVE provider/filter tests including Local Storage | passed | public sync wrote 214 versions and hashes matched; `TestCache_ActiveFilter_AppliesOnSuccess` removed inactive Deno and retained active `local-storage:single@1` |
| AC6 | `go test ./cmd/zcp -run 'Schema(Check|Commands|DriftWorkflow)' -count=1 -v`; clean-env real binary `schema check --strict`; workflow credential scan | passed | all command/workflow tests `PASS`; real binary printed `schema check: OK`; workflow scan printed `workflow_credentials=none` |
| — | negative/regression: `nodejs:ha@22` and `object-storage:ha` remain rejected; DB env/HA behavior unchanged | passed | composite catalog negative cases, existing managed DB rules, and env-readiness regression tests emitted `--- PASS` |
| B1 | `make test-race` | passed | `ok` on every package; no `FAIL` or `DATA RACE` |
| B2 | `make lint-local` | failed | `gocritic` (1), `maintidx` (1), `modernize` (2); fixing before retry |
| B2-retry | `make LINT=/Users/macbook/Documents/Zerops-MCP/zcp/bin/golangci-lint lint-local` | passed | `0 issues.` |
| B3 | `make vet-tags` | failed | stale E2E call: `ops.InitServiceGit` missing fourth argument in `e2e/bootstrap_git_init_test.go` |
| B3-retry | `make vet-tags` | passed | both `go vet -tags api ./...` and `go vet -tags e2e ./...` completed with no findings |
| B4 | `make e2e-zcp-fast` | failed | all selected cases passed except `TestE2E_Discover_MetaFields`: adopted services had no state dir; export test also exposed vault values in logs, so redact before retry |
| B4-retry | `make e2e-zcp-fast` | passed | selected live API/export/discover/knowledge tests all emitted `--- PASS`; exported vault content withheld from logs |
| B5 | `make e2e-zcp-deploy` | failed | live rig drift: source `zcp` is not adopted; two immediate provision checks observed `CREATING`; self-deploy succeeded but subdomain was not enabled |
| B1-final | `make test-race` after all code/test edits | passed | `ok` on every package; no `FAIL` or `DATA RACE` |
| B2-final | `make lint-local` | passed | `0 issues.` |
| B3-final | `make vet-tags` | passed | API and E2E tag vet commands completed with no findings |
| B4-final | `make e2e-zcp-fast` | passed | all selected live tests emitted `PASS`; knowledge version subcheck warned because the remote binary-only fixture does not deploy the snapshot file |
| D1 | `git diff --check` | passed | no whitespace errors |
| B6 | `make flow-eval-local ID=<applicable-scenario>` preflight | blocked | current credential resolves `z3-eval`, not the required disposable `eval-zcp`; destructive cleanup must not run against the wrong project |
| B7 | `make build`; tokenless `./bin/zcp schema check --strict`; tokenless `./bin/zcp catalog sync`; strict recheck | passed | build succeeded; both checks printed `schema check: OK`; sync wrote both schemas and 214 versions without a credential |
| C1 | RED: `go test ./internal/schema ./internal/platform -run 'Active' -count=1` | passed | failed on missing `FilterToActive`, ACTIVE cache provider/status, and platform method |
| C2 | GREEN: targeted ACTIVE tests + `go test ./... -short -count=1` | passed | runtime filter/provider tests and every short package passed |
| C3 | final runtime-overlay regression: `make test-race`; `make lint-local`; `make vet-tags`; `make build`; clean-env `./bin/zcp schema check --strict` | passed | no race/lint/vet/build failures; real binary printed `schema check: OK` without either schema credential variable |

## Promotion
- Contracts → `docs/schema-integration.md` (public artifact drift + runtime ACTIVE availability overlay); `docs/spec-knowledge-architecture.md` §3.1 (exact Local Storage catalog delivery); `docs/spec-workflows.md` §2.3/§9/§10 (Local Storage dependency/export/launch behavior); `docs/spec-oss-port-flow.md` §5 (Local Storage persistence/HA exclusion); `docs/spec-dataconsole.md` §6 + `docs/spec-dataconsole-testing.md` §2 (file-family registry).
- Invariants → tests named in slice briefs + the spec sections above.
- CLAUDE.md trap line (≤1): none expected
- This plan → `plans/archive/` on LAND close
