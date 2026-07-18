# Retest pack: dataconsole-testing-strategy

Zero-context bar: everything below runs from
`/Users/macbook/Documents/Zerops-MCP/zcp/.claude/worktrees/dc-testing-integration`
(branch `feat/dc-testing-strategy`, pushed to origin). VPN to `zcp-eval-clean`
must be up (`zcli vpn up l3wH59qoS7KZgYNSr5vsBg`) for the two live steps; the
gitignored `dc-live-config.json` already sits in the worktree root.

## Run

| command | expected line |
|---|---|
| `go test ./internal/dataconsole/... ./cmd/... -short -count=1` | every package `ok` |
| `go test ./internal/dataconsole/console/provider/ -run 'TestServiceProfiles' -count=1 -v` | 2× `--- PASS` (AC1) |
| `go test ./internal/dataconsole/console/server/ -run 'TestMutatingRoutes_ActionPolicy' -count=1 -v` | `PASS` (AC2 — 14-route matrix + positive control) |
| `go test ./internal/dataconsole/console/provider/conformance/ -run 'TestConformanceCoverage' -count=1 -v` | 3× `--- PASS`, none skipped (AC1/AC6 lint) |
| `ZCP_JS_REQUIRED=1 go test ./internal/dataconsole/console/webui/ ./internal/dataconsole/extension/ -count=1` | both `ok`, nothing skipped (AC4) |
| `./bin/golangci-lint run --fast-only ./...` | `0 issues.` |
| `make dc-live-full DC_LIVE_CONFIG=$PWD/dc-live-config.json` | `ok … conformance` + 25/25 pass ledger at `internal/dataconsole/console/provider/conformance/dc-live-summary.json` (AC3) |
| `make dc-live-remote` | exit 0; new `dc-live-summary-remote-*.json` in worktree root with 25 pass records (AC5) |

## Drive

1. AC3 negative — `make dc-live-full DC_LIVE_CONFIG=$PWD/dc-live-config.json DC_LIVE_MANIFEST=db=valkey` — expect: load error naming db / valkey / postgresql, exit non-zero.
2. AC6 negative — `... DC_LIVE_MANIFEST=db=postgresql@17` — expect: load error "requests version 17 … declares version 18".
3. AC4 fatal-skip — `ZCP_JS_REQUIRED=1 PATH="/usr/bin:/bin:$(dirname $(readlink -f $(which go)))" go test ./internal/dataconsole/console/webui/ -run TestDataConsoleSPAPureJS -count=1` — expect: FAIL "node missing; failing instead of skipping" (not a skip).
4. AC4 real CI — open the PR (branch is pushed; `gh` in this session was unauthenticated): `gh pr create --draft --base feat/managed-data-console` — expect: CI job runs the named "Data Console JS/DOM (required)" step green.
5. AC6 provisioning rehearsal (needs project-create permission your zcli has and mine didn't): `zcli project project-import e2e/testdata/dataconsole/version-matrix.import.yaml` — expect: project `zcp-dc-versions` with db17/es816/search110/vectors110/queue210 provisioning; then delete the project. (YAML already validated against the LIVE import schema by `go test -tags api -run TestLiveVersionMatrixImportYAMLValidates ./internal/schema/`.)

## What changed

- S1: `ServiceProfile` registry = enumerable single owner; `Classify`/`SupportFor` derived; mysql `ProvenBy: mariadb` (lint-verified).
- S2: one island factory builds providers for prod + conformance (armed posture — harness's private read-only switch deleted); server enforces each mutating route's ActionID against the service policy (uniform `ErrReadOnly`); cross-family upload 422→403 ratified per spec §5.1.
- S3: proof vocabulary (19 ProofIDs) + offline coverage lint (declared ⇒ proven, zero exceptions) + 8 new live gap cases.
- S4: typed `DC_LIVE_MANIFEST` (hostname=baseType[@version]) validated against config; full profile asserts the engine×proof matrix in Go; per-case JSON ledger (fail-recording cleanup, revision+runID stamped); cross-process valkey run-lock.
- S5: CI runs the JS/DOM suites (pinned Node 22 + `npm ci`); `ZCP_JS_REQUIRED=1` turns node/jsdom skips into failures.
- S6: `make dc-live-remote` — cross-compiled suite + `cmd/dclive gen-config` run ON the eval container over SSH, run-scoped ledger pulled back, remote dir cleaned; boundary enumeration extended deliberately (test + depguard in lockstep).
- S7: version-matrix import YAML (5 live-verified alternates; kafka@3.8 retired from the platform catalog) + hard version identity at manifest load.
- Fix-forwards from real runs: meili seed is task-CONFIRMED (queue backlog left the fixture index nonexistent); seed gets its own 120s setup budget; repo-context lints skip only when the source tree is absent (compiled-binary runs); `proofIDsFor` moved into its e2e-tagged caller; full-lint delta zeroed.

## Rollback

`git revert 5ec0c7f7..f482b135` on `feat/dc-testing-strategy` (or simply don't
merge the branch — `feat/managed-data-console` only contains the shared GATE1+S1
prefix `5ec0c7f7`+`f8e3defe`, which is behavior-preserving). Follow-up: none.

Final certified code tip: **f482b135** (independent verifier: "SHIP, no
blockers, no open caveats"; includes all 10 code-review finding fixes,
re-proven live: dc-live-full 25/25 + dc-live-remote 25/25 at that revision).

## Docs

`docs/spec-dataconsole-testing.md` (new, §1-§9) · `docs/spec-dataconsole.md`
§3.2 (cmd/dclive composition point) · `docs/spec-testing-architecture.md` §2
(pointer) · CLAUDE.md key-specs map (one line).

## Known debt surfaced (pre-existing, NOT this branch's)

- ~32 full-lint issues at the merge-base (26× contextcheck server.go, errcheck
  kv_redis_test.go, staticcheck object.go, tparallel error_envelope_test.go…) —
  they block hook-gated commits/pushes for everyone; this flow used documented
  `--no-verify` after verifying pre-existence twice. Recommend a 30-min cleanup
  slice.
- `internal/knowledge` + `internal/authoring/recipe` fail `-short` locally
  ("knowledge corpus not synced") — environment, pre-existing at merge-base.
- One cold-start flake class observed and root-caused live (meili task queue);
  fixed via task-confirmed seed + seed budget. If a required cell ever reds on
  a first-contact timeout again, the ledger names it (run-scoped remote
  artifacts prevent clobbering).
