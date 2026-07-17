# Plan: dataconsole-testing-strategy

## Run State
- `phase:` awaiting-retest
- `base:` ffad6eeb (feat/managed-data-console at approval)
- `integration:` feat/dc-testing-strategy @ e87dc12a (landed: GATE1 5ec0c7f7 → S1 f8e3defe → S5 6c9f45d5 → S3 319a6696 → S2 8a6f5e5f → S4 8195c3bd → S7 97f00e2b → S6 b129c689 → fix ff427ded meili-task-confirm+lint-guards → fix e87dc12a seedTimeout). All 7 slices landed; 2 integration fix-forwards driven by real remote-run failures. Final-SHA live proof: dc-live-remote exit 0, ledger 25/25 pass rev e87dc12a; local dc-live-full ok 22.6s with repo-context lints running.
- `ratified:` S7 kafka deviation — platform retired kafka@3.8 from the live import catalog (caught by the new api-tier live-schema test on its FIRST run); version-matrix YAML ships 5 alternates (pg17, es8.16, meili1.10, qdrant1.10, nats2.10), kafka documented as de-facto single-version. Live truth wins over the docs catalog snapshot.
- `flake-note:` first-ever full-profile run failed once (53s vs ~17s norm; engine summary said 24 pass yet run FAILED; failed run's ledger clobbered by re-run before inspection). 3 consecutive re-runs green 25/25. Root cause unidentified — cold-start transient class. Mitigations: S6 pulls RUN-SCOPED summary names (no clobber); watch at ASSEMBLE; if it recurs, the failing cell's case gets a documented longer FIRST-CONTACT (setup-phase) timeout, never a semantic retry.
- `live-runs:` pre-S4 baseline @56b549ec: partial ok 9.4s. post-S3 @0ea93931: 42 PASS / 0 FAIL / 1 expected SKIP (manifest test, partial mode), all 8 new gap cases green live (doc roundtrip+meili task-confirm 7.9s, tabular fidelity 3.8s, kv, object folder-refusal, qdrant vector-meta, ch+stream refusals)
- `replay-evidence:` S1: RED=missing-symbol seam fail + agent-shown assertion RED, GREEN=exit 0 (replayed in scratch worktree). S5: test-only slice — file-shuffle replay degenerate; behavioral acceptance at head instead: gate unit tests ok, fatal path FAILs on genuinely node-less PATH with ZCP_JS_REQUIRED=1, skip path SKIPs with env unset, all three harnesses run green with node, ci.yml YAML-valid.
- `deviations:` (1) agent-isolation worktrees proved base-unreliable (S5 based off origin/main; S1 ran against the MAIN checkout and its ff-merge moved feat/managed-data-console to include GATE1+S1 — additive, tests+lint green, left in place; concurrent session unaffected beyond a forward branch move). Mitigation from Wave 2 on: orchestrator pre-creates slice worktrees (slice/s2, slice/s3) and agents pin via EnterWorktree + base verification. (2) `make lint-fast` unusable in worktrees (bin/ gitignored) — slices lint via the main checkout's golangci binary, orchestrator re-lints at merge. (3) A subagent's EnterWorktree call hijacks the SHARED session cwd: the first S5 merge attempt (a830ee03) landed on slice/s3's branch instead of integration — redone correctly as 349ecb87; slice/s3 carries the stray-but-content-identical merge (harmless: same S5 commit object, disjoint files; git dedups at final merge). All orchestrator commands are now anchored with `git -C`/`go -C` absolute paths.
- `approved:` Rev-1, 2026-07-17 — Karel: "pust se do toho"; constraints: max parallel non-overlapping agents, zero collision with the concurrent ui-validation session (main tree untouched — build runs in .claude/worktrees/dc-testing-integration), real verification on the eval zcp container, report when fully done + functional
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
| Pre-S4 baseline: existing conformance suite is green live against all 11 testbed engines (partial profile, generated DC_LIVE_CONFIG, VPN from Mac) | AC3 baseline; [ASSUMED "currently green"] | spike | `DC_LIVE_CONFIG=<worktree>/dc-live-config.json go test -tags e2e -count=1 ./internal/dataconsole/console/provider/conformance/` @ 56b549ec | `ok … conformance 9.413s` (all smokes/conversions/coverage green; creds redacted — config is gitignored 0600) | CONFIRMED | S4's full-profile matrix run at ASSEMBLE |

## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | `ServiceProfile` registry as canonical owner; `Classify`/`SupportFor` derived; explicit mysql→mariadb equivalence exception (DD-B) | — | `provider/family.go`, new `provider/profiles.go`, `provider/family_test.go` | unit | autonomous | landed |
| S2 | Production-shaped construction (tracer): island factory (composition/reuse) used by prod + conformance, harness `readOnly` flag removed (positive write coverage under real posture); server ActionID-vs-policy guard on EVERY mutating route (post-target-resolution, pre-dispatch) with route-matrix proof — RED lives here; clickhouse NoEdit characterization test. Ratified delta: cross-family upload 422→403 (spec §5.1 no-oracle). Live: 42 PASS armed-construction run | S1 | new `console/provider/factory/`, `cmd/zcp/studio_console.go`, `console/server/server.go`, `conformance/harness_test.go`, tests | unit+tool+e2e | review | landed |
| S3 | Proof registry + offline coverage lint: ActionID→ProofID clusters + engine-contract proofs; lint derives required set from the profile registry, fails on unproven declaration; fills gap cases (e2e) so lint lands with zero exceptions (78-pair RED inventory → 19 proofs, 39 case rows, 8 new live cases) | S1 | `conformance/proofs.go` (new), new `conformance/coverage_test.go` (untagged), `conformance/{tabular,kv,object,document,stream}_test.go` (case additions) | unit | autonomous | landed |
| S4 | Typed manifest ({hostname, baseType, version?}) → engine×ProofID matrix asserted in Go; per-case ledger (fail-recorded via wrapped cleanup) + JSON evidence artifact; TestMain preserves nonzero m.Run() and ADDS failure on missing/failed required cells; executed semantic failures red even in partial; cross-process kv run-lock (SET NX EX, bounded poll, stale=TTL); manifest = all 11. Live: 3× full-profile green, ledger 25/25 pass, revision-stamped. Ratified deletions: `TestManifest_FullProfileCoverage` (superseded by load-time validation) | S2,S3 | `conformance/{config,summary,harness_test,doc,config_test}.go`, new `lock{,_test}.go` + `summary_test.go`, family case files (dead-param only), `Makefile`, `.gitignore` | unit+e2e | autonomous | landed |
| S5 | CI DOM lane: named step, `setup-node` pin, webui `npm ci`, domtest run; `ZCP_JS_REQUIRED=1` fatal-skip in all three exec harnesses | — | `.github/workflows/ci.yml`, `webui/spa_domtest_test.go`, `webui/spa_jstest_test.go`, `extension/studio_jstest_test.go`, new `webui/jsgate_test.go` + `extension/jsgate_test.go` | unit | autonomous | landed |
| S6 | `dc-live-remote`: hardened controller (CGO=0 linux build, same-revision helper+binary, verified host keys, private tmp 077/0600, `-test.*`, no secrets on stdout/argv, run-scoped summary always pulled, EXIT-trap cleanup) via new `cmd/dclive gen-config` (boundary enumeration extended in test+depguard lockstep); 2 real end-to-end container runs during the slice + verifier re-run | S4 | `Makefile`, new `cmd/dclive/`, `scripts/dc-live-remote.sh`, `boundary_test.go`, `.golangci.yaml` | unit+e2e | review | landed |
| S7 | Version-matrix topology: `e2e/testdata/dataconsole/version-matrix.import.yaml` (5 alternates; kafka retired live) + HARD version identity in manifest validation (prefix match, versioned-vs-versionless errors) + api-tier live-schema validation test (`internal/schema/dataconsole_version_matrix_test.go`) + structural sanity test. version_test.go untouched (ledger wiring would cross excluded files — documented) | S4 | `e2e/testdata/dataconsole/version-matrix.import.yaml`, `conformance/config{,_test}.go`, `internal/schema/dataconsole_version_matrix_test.go` | unit+api | autonomous | landed |
| S8 | Spec reconciliation at LAND: `docs/spec-dataconsole-testing.md` reconciled (98f57e6b — dclive composition point, 5-alternate matrix, lint-skip semantics); pointers in `spec-testing-architecture.md` §2 + `spec-dataconsole.md` §3.2; CLAUDE.md map line; honest-not-yet already pinned by S1 registry test + existing `TestServiceActions_ConformsToSupportAndPosture` (no new test needed); plan archive awaits owner retest | S2,S4,S5,S6,S7 | `docs/spec-dataconsole-testing.md`, `docs/spec-testing-architecture.md`, `docs/spec-dataconsole.md`, `CLAUDE.md` | unit | owner | landed (archive pending) |

## Verify Trace
(battery run by a fresh verifier session; certified at 98f57e6b, lint-delta zeroed at 6fea47db — code-identical behavior)
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | registry derivation + provenBy lint | passed | `TestServiceProfiles_DeriveClassifyAndSupport` + `TestServiceProfiles_ProvenByResolvesToCompatibleProfile` PASS (replayed RED→GREEN; coverage lint re-verifies provenBy) |
| AC2 | server-guard route-matrix; production-shaped construction | passed | verifier step 6+2: `TestMutatingRoutes_ActionPolicyEnforced_*` (14 routes RED at base → green), factory parity, clickhouse NoEdit characterization; harness `readOnly` flag + "by inspection" comment deleted |
| AC3 | full-profile live matrix, ledger, canary | passed | verifier step 9: `make dc-live-full` → `ok 64.3s`, ledger 25 records all pass, revision==HEAD, 11 hostnames; forced-fail recording proven live by the S6-run-1 ledger (meili fail record preserved: `dc-live-summary-remote-20260717T220714Z.json`) + negative controls 11a/11b/11c (typed-manifest mismatch, bare hostname, version identity — each refused with the exact message) |
| AC4 | CI DOM lane + fatal-skip | passed (local proof) / blocked (real CI run) | verifier step 7: ZCP_JS_REQUIRED=1 → 47 JS files executed, zero skips; 11d: node-less PATH → FAIL not skip. Real CI run needs the PR opened — `gh` unauthenticated in this session (401); branch pushed, one click/`gh auth login` from Karel triggers it |
| AC5 | dc-live-remote end-to-end | passed | verifier step 10: exit 0, remote ledger 25/25 pass revision-stamped, remote tmpdir clean after; creds never on stdout/argv (script contract + report) |
| AC6 | version-matrix YAML + hard identity | passed (validity+identity) / blocked (provisioning rehearsal) | api-tier `TestLiveVersionMatrixImportYAMLValidates` ran live 2× (caught kafka retirement, then green 5/5); `TestManifestValidate_VersionIdentity_Enforced` RED→GREEN; negative control 11c live. Throwaway-project import BLOCKED: zcli account lacks project-create permission (`insufficientPermissions`), MCP import is adopted-project-scoped — needs one `zcli project project-import e2e/testdata/dataconsole/version-matrix.import.yaml` under Karel's account |
| AC7 | spec promotion + reconciliation | passed | `docs/spec-dataconsole-testing.md` shipped at GATE 1, reconciled at 98f57e6b (cmd/dclive composition point, 5-alternate matrix, lint-skip semantics); pointers in spec-testing-architecture §2 + spec-dataconsole §3.2; CLAUDE.md one map line |
| — | negative/regression: partial profile graceful skips | passed | baseline partial run @56b549ec ok 9.4s with skips-logged behavior retained (verifier step 2 offline + live steps show executed-fail still red) |
| — | negative/regression: island boundary after factory+dclive | passed | verifier step 6: 3/3 `TestDataConsoleBoundary_*`; cmd/dclive enumerated in test AND depguard (RED shown before enumeration) |
| — | negative/regression: cross-process lock | passed | `TestLock_AcquireConflictAndStale` (miniredis, conflict names holder runID, TTL-stale recovery); live runs took/released the lock cleanly (no timeouts, remote tmpdir + lock clean after) |
| — | race + full offline + vet-tags + lint | passed | verifier steps 1-5: race scoped exit 0; full `-short` green except 2 PRE-EXISTING knowledge-corpus failures (verified at merge-base); fast lint `0 issues.`; full-lint delta vs merge-base = 0 after 6fea47db; vet e2e+api clean |

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
