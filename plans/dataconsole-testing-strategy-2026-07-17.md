# Plan: dataconsole-testing-strategy

## Run State
- `phase:` awaiting-approval
- `base:` 3a82424681998a1452a907889b2c1a698acc40f5
- `integration:` —
- `approved:` —
- `codex:` CONFIRMED — v1 (12 findings, 4 blockers) incorporated; v2 "confirm after three corrections" incorporated (S2 rescope: clickhouse NoEdit already constructor-owned [tabular.go:142], RED moves to the server ActionID route-matrix guard; S4 deps S2+S3 + sharper cell semantics; mysql provenBy lint-verified). Reviews: /tmp/codex-out-1784317606-98647-9501.md, /tmp/codex-out-1784318323-1367-1734.md
- `next:` OWNER GATE 1 — Karel approves the register; on approval promote spec skeleton + set phase: build

## Frame

**Outcome**: The Data Console has one governed testing architecture: a canonical
`ServiceProfile` registry is the single enumerable owner of type→family→support;
every capability it declares is proven by a required per-engine live conformance
proof (engine × ProofID matrix asserted in Go, JSON ledger as evidence);
production and conformance build providers through ONE island factory with
constructor-owned view-only enforcement; the offline JS/DOM suites gate CI
instead of silently skipping; the live lane runs one-command from any
SSH-connected machine; alternate-version compatibility runs hard-assert
provisioned identity; the whole is promoted to `docs/spec-dataconsole-testing.md`.

**Design decisions** (trade-offs settled with Codex v1):
- Proof granularity: two levels — `ServiceActions` map onto semantic `ProofID`
  clusters (smallest independent backend failure mode), plus an explicit
  engine-contract proof layer for facts actions cannot express (task-confirm,
  vector meta, value fidelity, error parity, key echo, pagination, RO-query).
  Rejected: coarse `Capabilities` (toothless) and raw per-action cases (noisy).
- Gate primitive: Go-asserted engine×ProofID matrix; JSON is the serialized
  ledger (evidence), never the gate. Rejected: parse-the-JSON gating.
- Test doubles: the three fake layers (protocol doubles, server doubles, seed
  fixtures) stay SEPARATE — they represent different seams; unifying them would
  erase exactly the differences the live lane exposes. Reuse instead: one
  provider factory, proof IDs, contract fixtures, `console/seed` builders.

| obs | evidence |
|---|---|
| Two verification worlds; only the hermetic one is automated. CI = `go test -race ./...` + `go vet -tags e2e` (compile-only); no dataconsole live/DOM/uitest job | [SELF-VERIFIED:.github/workflows/ci.yml:37-46] via Explore |
| Conformance is family-generic + opportunistic: nothing required unless in `DC_LIVE_MANIFEST` (comma-separated HOSTNAMES only); documented release manifest = 5 of 11 engines; no version matrix (log-only capture) | [SELF-VERIFIED:conformance/config.go:277-336, Makefile:174-179] via Explore |
| `Classify`/`SupportFor` are switch statements — not enumerable by a lint without AST parsing; mysql is declared SupportFull yet has no deployed engine anywhere | [SELF-VERIFIED:provider/family.go:11-47] |
| Conformance duplicates production factory construction "by inspection" and takes an arbitrary `readOnly` flag — masking POSITIVE write coverage for full engines (clickhouse itself is safe: `tabular.New`'s `normalizeConfig` forces `NoEdit=true` + `readonly=1` DSN, and both prod and conformance pass `Conn`) | [SELF-VERIFIED:conformance/harness_test.go:169-197, tabular/tabular.go:53,142-143] |
| Server mutating routes gate on Origin + `AuthorizeWrite` (write token) only — no per-route ActionID-vs-service-policy check | [SELF-VERIFIED:console/server/server.go:135-151] |
| `ServiceActions` is the connection-free single owner of service-level affordances (Go-owned ActionID set, SPA drift-guarded) | [SELF-VERIFIED:provider/actions.go:58-86, console/contract_test.go] |
| DOM suite (18 files) SKIPS in CI — no `npm ci`; all three node-exec harnesses skip silently when node/jsdom absent | [SELF-VERIFIED:webui/spa_domtest_test.go:23-33] via Explore |
| uitest (puppeteer) is the ACTIVE ui-validation program's harness (Lane F/X, G0 proven) — assembled-UI tier is its own program | [SELF-VERIFIED:plans/dataconsole-ui-validation-program-2026-07-17.md] |
| Hosted CI runner has no VPN/zcli; canary pattern (fail on zero real passes) exists in e2e.yml | [SELF-VERIFIED:.github/workflows/e2e.yml:8-53] |
| Testbed `zcp-eval-clean` has all 11 console-relevant engines live (pg18, mariadb10.6, ch25.3, valkey7.2, S3, es9.2, meili1.44, typesense30.2, qdrant1.12, nats2.12, kafka3.9) | mcp `zerops_discover` (ledger row 1) |
| VPN gives sockets, NOT credentials (REST `FetchServiceEnv` is the creds path); `cmd/dcseed` already connects to every family FROM the container | [KB:vpn.mdx:51,86; zcpadapter/adapter.go:79-193; cmd/dcseed/main.go] |
| HA is endpoint-transparent for the console's primary path; single-mode engines suffice | [KB:valkey/overview.mdx:71-78; clickhouse/overview.mdx:34-93] |
| keydb soft-deprecated; rabbitmq FamilyStream but SupportNotYet; shared-storage has no provider (errors at descriptor build) | [KB + SELF-VERIFIED:provider/family.go:22-45, conformance/config.go:186-189] |

- **AC1** `ServiceProfile` registry is the canonical enumerable owner:
  `Classify`/`SupportFor` derive from it; mysql carries an EXPLICIT
  `provenBy: mariadb` equivalence exception (owner decision DD-B) that the
  coverage lint VERIFIES (provenBy resolves to an existing profile with
  matching family/support/applicable proofs) — equivalence never satisfies
  type/version identity or future dialect-shaped proofs. — evidence: registry
  test derives both functions; lint proves the exception's validity.
- **AC2** Production-shaped construction: ONE island-local provider factory
  (composition/reuse; constructors stay the ultimate view-only owners) used by
  production AND conformance — the harness's arbitrary `readOnly` flag dies, so
  full engines get positive write coverage under the REAL posture; the server
  gains an ActionID-vs-service-policy guard on EVERY mutating route (checked
  after target resolution, before `ProviderFor`/dispatch — body-addressed
  routes included), proven by a route-matrix test; RED replay lives in the NEW
  server guard; clickhouse `NoEdit` gets a characterization test (already
  constructor-owned). — evidence: server-guard RED first then green;
  route-matrix proof; harness drift comment deleted.
- **AC3** Per-engine required matrix: typed manifest ({hostname, baseType,
  version?}); full profile derives + asserts the exact engine×ProofID matrix in
  Go — missing/failed required cell = red; partial may skip only on absence/
  unreachability/setup failure, an EXECUTED semantic assertion failure stays
  red; `TestMain` preserves any nonzero `m.Run()` (the ledger gate only ADDS
  failure); view-only engines carry an offline policy/constructor proof PLUS
  one live writes-armed mutation-refusal cell each; not-yet/unknown offline
  only; per-case ledger (id, host, baseType, family, outcome, phase, reason,
  duration, declared/observed versions, revision) serialized to JSON as
  evidence; cross-process project-scoped lock + run ID. — evidence:
  `make dc-live-full` 11/11 required cells green + ledger artifact.
- **AC4** CI runs the DOM suite on every PR as a named step with pinned Node
  (`setup-node`) + `npm ci`; `ZCP_JS_REQUIRED=1` makes node/jsdom absence FATAL
  in all three exec harnesses. — evidence: CI log shows domtest subtests; local
  proof of the require-switch.
- **AC5** `make dc-live-remote` runs the full live lane from the Mac:
  CGO_ENABLED=0 linux test binary + same-revision config helper shipped over
  SSH (verified host keys), private remote tmp (umask 077/0600), config from
  REST creds never on stdout/argv/logs, `-test.*` invocation, summary always
  retrieved (even on nonzero exit), signal/exit cleanup. Release checklist
  defines required artifact + max age + commit match. — evidence: one command
  end-to-end; secret-grep of ships/logs clean.
- **AC6** Version matrix: import YAML at `e2e/testdata/dataconsole/
  version-matrix.import.yaml` (schema-validated) stands up alternate versions
  (pg 17/16/14, es 8.16, meili 1.10, qdrant 1.10, kafka 3.8, nats 2.10) in a
  throwaway project; the run HARD-asserts platform-declared type/version ==
  requested (observed protocol version recorded separately); standing project
  stays warn-only drift. — evidence: YAML validates; identity-assert test;
  runbook in spec.
- **AC7** Architecture promoted: `docs/spec-dataconsole-testing.md` (5-tier
  map, placement rule, proof-coverage rule, typed manifest, live-lane ops +
  release checklist, version policy incl. monthly provisioning rehearsal);
  pointers in `spec-testing-architecture.md` §2 + `spec-dataconsole.md` §7. —
  evidence: spec exists, references reconciled at LAND.

**Non-goals**: duplicating the ACTIVE ui-validation program (this plan
integrates its tier; shared-storage/unknown honest-not-yet get offline proofs +
a uitest note only) · CI-hosted VPN (stays with e2e.yml's tracked gap;
dc-live-remote slots into a self-hosted runner later) · HA-mode matrix ·
engines for mysql/keydb/rabbitmq/shared-storage (mysql = explicit equivalence
exception; rabbitmq/unknown = offline classification proofs) · unifying the
three fake layers (rejected, see design decisions) · behavioral-eval changes.

**Constraints**: console island imports zero core — the shared factory lives
INSIDE the island, the config generator OUTSIDE it · no retries on semantic
assertions · namespaced fixtures + sweep, stable namespace kept for crash
recovery behind the new lock · secrets never committed/logged/on-argv ·
`e2e` build tag only · spec-testing tier rule governs placement.

**Risk class**: medium — trigger: owner asked; multi-session scope. Live lane
mutates only namespaced fixtures on the eval project; S2 touches the write
path's enforcement (adds a server-side guard — strictly narrowing).

**Assumptions**:
- [VERIFIED] Conformance dials engines directly, config-file-driven, never REST — `conformance/config.go:96-190`.
- [VERIFIED] Container-side credential fetch works for every family — `cmd/dcseed` via zcpadapter (`main.go:1-9`, `adapter.go:102-193`).
- [VERIFIED] SSH from the dev Mac reaches the container non-interactively — uitest `lib/engines.js` + 254 findings lines produced through it.
- [VERIFIED] Testbed composition (11 services + versions) — ledger row 1.
- [VERIFIED] Clickhouse no-edit is constructor-owned (`tabular.go:142-143`); the harness's arbitrary `readOnly` masks positive write coverage for full engines; mutating routes have no ActionID-vs-policy guard (`server.go:135-151`) — ledger row 2.
- [ASSUMED] Conformance full profile currently green 11/11 on the testbed (reported by excellence/ui-validation runs 2026-07-16/17; not load-bearing — S4 re-runs it as its own gate; any red is a real finding).
- [ASSUMED] Platform may upgrade standing-engine versions server-side (meili 1.20→1.44 already observed) — warn-only drift on the standing project is deliberate.

## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| zcp-eval-clean carries one-of-each of all 11 console-relevant managed types | AC3 | mcp | `zerops_discover` (no args) | db=postgresql:single@18, mariadb:single@10.6, ch=clickhouse:single@25.3, cache=valkey:single@7.2, storage=object-storage, es=elasticsearch:single@9.2, search=meilisearch:single@1.44, docs=typesense:single@30.2, vectors=qdrant:single@1.12, queue=nats:single@2.12, events=kafka:single@3.9 — all ACTIVE | CONFIRMED | typed manifest in S4 |
| Clickhouse view-only IS constructor-owned (`normalizeConfig` → `NoEdit=true`, `readonly=1` DSN) — codex v1's masking claim REFUTED for clickhouse; the harness `readOnly` flag instead masks POSITIVE write coverage for full engines, and no server-side ActionID guard exists on mutating routes | AC2 | repo | `grep -n 'NoEdit\|readonly=1' provider/tabular/tabular.go`; `sed -n '135,151p' console/server/server.go` | `tabular.go:142: cfg.NoEdit = true`, `:143: readonly=1`; server routeGroup checks Origin+AuthorizeWrite only | CONFIRMED (corrected) | S2: server-guard RED `TestMutatingRoutes_ActionPolicyEnforced` + clickhouse NoEdit characterization |
| PROVE otherwise skipped — remaining assumptions settled at repo surface during FRAME (no further load-bearing uncertainty) | — | repo | — | — | — | — |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | `ServiceProfile` registry as canonical owner; `Classify`/`SupportFor` derived; explicit mysql→mariadb equivalence exception (DD-B) | — | `provider/family.go`, new `provider/profiles.go`, `provider/family_test.go` | unit | autonomous | pending |
| S2 | Production-shaped construction (tracer): island factory (composition/reuse) used by prod + conformance, harness `readOnly` flag removed (positive write coverage under real posture); server ActionID-vs-policy guard on EVERY mutating route (post-target-resolution, pre-dispatch) with route-matrix proof — RED lives here; clickhouse NoEdit characterization test | S1 | new `console/provider/factory/`, `cmd/zcp/studio_console.go`, `console/server/server.go`, `conformance/harness_test.go`, tests | unit+tool+e2e | review | pending |
| S3 | Proof registry + offline coverage lint: ActionID→ProofID clusters + engine-contract proofs; lint derives required set from the profile registry, fails on unproven declaration | S1 | `conformance/proofs.go` (new), `conformance/*_test.go` (proof tags), new `conformance/coverage_test.go` (untagged) | unit | autonomous | pending |
| S4 | Typed manifest ({hostname, baseType, version?}) → engine×ProofID matrix asserted in Go; per-case ledger (fail-recorded via wrapped cleanup) + JSON evidence artifact; TestMain preserves nonzero m.Run() and ADDS failure on missing/failed required cells; executed semantic failures red even in partial; view-only engines: offline policy proof + one live writes-armed refusal cell each; cross-process project lock + run ID; manifest = all 11 | S2,S3 | `conformance/config.go`, `conformance/summary.go`, `conformance/harness_test.go`, `Makefile` | unit+e2e | autonomous | pending |
| S5 | CI DOM lane: named step, `setup-node` pin, webui `npm ci`, domtest run; `ZCP_JS_REQUIRED=1` fatal-skip in all three exec harnesses | — | `.github/workflows/ci.yml`, `webui/spa_domtest_test.go`, `webui/spa_jstest_test.go`, `extension/studio_jstest_test.go` | unit | autonomous | pending |
| S6 | `dc-live-remote`: hardened controller (CGO=0 linux build, same-revision helper+binary, verified host keys, private tmp 077/0600, `-test.*`, no secrets on stdout/argv, summary always pulled, signal cleanup) + release-checklist contract | S4 | `Makefile`, new `cmd/dclive/`, `scripts/` | unit+e2e | review | pending |
| S7 | Version-matrix topology: `e2e/testdata/dataconsole/version-matrix.import.yaml` (schema-validated) + throwaway-run HARD identity assert (declared type/version == requested; observed recorded separately) + monthly provisioning-rehearsal note. Reach = VPN from the Mac + local config generation (a throwaway project has no zcp container — the remote controller does not apply) | S4 | `e2e/testdata/dataconsole/version-matrix.import.yaml`, schema-validation test, `conformance/version_test.go` | unit+e2e | autonomous | pending |
| S8 | Spec reconciliation at LAND: `docs/spec-dataconsole-testing.md` final; pointers in `spec-testing-architecture.md` §2 + `spec-dataconsole.md` §7; offline honest-not-yet proofs (shared-storage/rabbitmq/unknown); plan archive | S2,S4,S5,S6,S7 | `docs/spec-dataconsole-testing.md`, `docs/spec-testing-architecture.md`, `docs/spec-dataconsole.md`, `provider/family_test.go` | unit | owner | pending |

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | registry test: `Classify`/`SupportFor` outputs derive 1:1 from `ServiceProfile` list; mysql exception visibly encoded | not-run | — |
| AC2 | server-guard RED replay: mutating request whose ActionID the service policy disables → `ErrReadOnly` on EVERY mutating route (route-matrix test, body-addressed included); then green; clickhouse NoEdit characterization; harness "by inspection" comment gone | not-run | — |
| AC3 | `make dc-live-full` over VPN → 11/11 required engine×ProofID cells green; ledger JSON artifact written incl. a forced-failure dry run recording `fail` | not-run | — |
| AC4 | CI run shows named DOM step executing subtests; locally `ZCP_JS_REQUIRED=1` with node hidden → FAIL not skip | not-run | — |
| AC5 | `make dc-live-remote` end-to-end from the Mac; summary lands locally; secret-grep of shipped files + logs → none | not-run | — |
| AC6 | version-matrix YAML schema-validates; identity-assert test red on a mismatched declared version (simulated), green on match | not-run | — |
| AC7 | spec file exists; both pointer specs reconciled; CLAUDE.md map line ≤1 | not-run | — |
| — | negative/regression: partial profile still skips gracefully (missing engine → logged skip, green) | not-run | — |
| — | negative/regression: island boundary green after S2/S6 (`TestDataConsoleBoundary_*` + depguard; factory INSIDE island, dclive/generator OUTSIDE) | not-run | — |
| — | negative/regression: concurrent dev run + release run → lock serializes; stale lock recovered | not-run | — |

## Promotion
- Contracts → `docs/spec-dataconsole-testing.md` (new): 5-tier map (offline Go
  → offline JS/DOM → hermetic server → live per-engine conformance →
  assembled-UI uitest), placement rule, proof-coverage rule (declared ⇒
  proven, exceptions explicit), typed manifest + matrix gate, live-lane ops +
  release checklist (artifact, max age, commit match), version policy
  (standing warn-only / throwaway hard-identity / monthly rehearsal).
- Invariants → `TestServiceProfiles_DeriveClassifyAndSupport` ·
  `TestConformanceCoverage_DeclaredProofHasCase` ·
  `TestViewOnly_ArmedWrites_RefusedIntrinsically` (+ server ActionID guard) ·
  matrix-gate ledger tests · `ZCP_JS_REQUIRED` behavior test.
- CLAUDE.md trap line: none — the coverage lint + matrix gate enforce
  mechanically; CLAUDE.md gets only the spec pointer in the key-specs map.
- This plan → `plans/archive/` on LAND close.
