# Logging Subsystem — Fundamental Refactor

> **Status**: ✅ SHIPPED 2026-04-23. All seven phases green. Pre-existing
> `internal/knowledge/testdata/active_versions.json` merge conflict is
> unrelated to this work.
> **Evidence base**: `plans/logging-investigation.findings.md` (2026-04-23 live probes).
> **Supersedes**: the narrow fix scoped in `plans/friction-root-causes.md` §2 P1.2.
> **Depends on**: nothing — self-contained.
> **Shipped behind**: per-phase test gates; every phase landed with its RED tests authored first and full-suite green before moving on.

---

## 1. Why Refactor Instead of Patching

The originally-described problem — **stale build warnings from previous builds surface on a successful redeploy** — is a visible symptom of a deeper fault: **every layer of log fetching is wrong at least once**.

Live Zerops API probes (see findings §1, pinned by `internal/platform/logfetcher_build_contract_test.go`) show:

1. **The log backend has no server-side time filter.** Every query I probed (`since=`, `from=<date>`, `fromDate=`, `timestamp=`) returned bytes-identical baseline responses. ZCP's client-side `Since` filter is structurally the only defense.
2. **That client-side filter is buggy.** `logfetcher.go:136` formats `Since` as `time.RFC3339` (no fractional seconds) and compares lexicographically against entry timestamps that arrive with 4–9 fractional digits. ASCII ordering puts `Z` (0x5A) after `.` (0x2E), so entries at `...T06:04:29.9Z` (clearly AFTER `PipelineStart=...T06:04:29.440...Z`) lex-compare as **before** and get dropped. Switching to `RFC3339Nano` does NOT fix this — entries with fewer fractional digits than Since still misorder.
3. **The backend exposes a tag filter that is authoritative.** `tags=zbuilder@<appVersionId>` returns exactly that build's entries, zero for a bogus tag. This is a **stronger primitive** than time windowing — it scopes by build identity rather than by time range approximation.
4. **The `search` parameter advertised by `zerops_logs` MCP tool is a server-side no-op.** Both `search=sshfs` (should match all) and `search=nonexistent-xyz` (should match none) returned identical results. Agents that rely on it to narrow down logs are misled.
5. **The `facility` parameter is never set.** Zcli always sends `facility=16` for application logs. Without it the backend mixes `facility=3` daemon noise (sshfs mount errors, systemd timeouts) into our queries. On a failing deploy, `FetchRuntimeLogs` surfaces SSH infrastructure errors as if they were the user's crash stack.

Patching only (1)+(2) — the shipped P1.2 design — would:
- Leave (3) unused, keeping time-window scoping when identity-based is available.
- Leave (4) as a permanent surface lie.
- Leave (5) as a permanent noise source in deploy results.

Those remainders are all in the same ~200-line code path. Fixing them together is the same blast radius as fixing just one, with much higher confidence the problem stays fixed.

---

## 2. Goals

**G1.** `FetchBuildWarnings` and `FetchBuildLogs` return **only the current build's log entries**, deterministically, with zero leakage from prior builds on the same build service-stack. Regression-tested against the live Zerops API.

**G2.** `FetchRuntimeLogs` returns **only logs from the current container generation** (i.e. after the most recent container creation for that service), with zero bleed from previous deploys. Regression-tested.

**G3.** The `zerops_logs` MCP tool is **honest about its own surface**: `search`, `severity`, `since`, `limit` all do what the JSON schema advertises.

**G4.** The `logfetcher` comparison semantics are **correct at sub-second resolution**: the `Since` filter uses parsed `time.Time` comparison, not string compare.

**G5.** Every layer (platform, ops, tool) has unit-test coverage that **would fail** if any of G1–G4 regressed. Coverage is not blind: `MockLogFetcher` applies the same filters the real fetcher applies, so "passing mock tests" means "filter logic actually ran".

**G6.** Code contains no knowledge of what the backend silently ignores — there's one honest layer (the client wire protocol) and one honest layer above it (parse/filter). Layers below don't leak implementation details upward.

**G7.** The `api`-tagged contract tests in `internal/platform/logfetcher_build_contract_test.go` stay green after every phase. They are the canary for "Zerops changed something upstream".

---

## 3. Non-Goals

- **No WebSocket stream follow.** `zcli` exposes `--follow` via `/stream`. Our flow is polled and stateless; adding a stream duplicates lifecycle state for no agent-facing benefit.
- **No `from=<msgId>` cursor in MCP surface.** The cursor is real and useful for reconnection, but the stateless stdio MCP model has no convenient place to remember a last-seen id per call. If a future session-aware flow needs it, revisit then.
- **No upstream feature request yet.** Tag-based scoping covers the build case; container-id scoping covers the runtime case. If a case arises where neither works, file an ask then.
- **No retirement of the `Since` field.** `zerops_logs` still accepts it from agents (relative durations like `1h`) — the client-side filter becomes correct, not removed.
- **No change to `PollBuild` cadence.** Logs are fetched once on terminal state; this plan doesn't touch polling.
- **No migration of existing `internal/knowledge` version-pin test failures.** They are an unrelated merge conflict on `active_versions.json` (`UU` at session start) — out of scope.

---

## 4. Architecture

### 4.1 Typed filter surface — `LogFetchParams`

```go
// LogFetchParams contains parameters for fetching logs from the backend.
//
// Server-side filters (backend honours these):
//   - ServiceID (serviceStackId)
//   - Severity  (minimumSeverity — syslog 0..7)
//   - Facility  ("application" | "webserver" | "")
//   - Tags      (CSV-joined tags=; exact match)
//   - ContainerID (containerId; exact match on UUID)
//   - Limit     (1..1000, clamped)
//
// Client-side filters (backend ignores; applied after fetch):
//   - Since     (parsed time.Time; entry kept when !entry.Before(Since))
//   - Search    (case-sensitive substring match on Message)
type LogFetchParams struct {
    ServiceID   string
    Severity    string    // "" | "emergency" | ... | "debug" | "all"
    Facility    string    // "" | "application" | "webserver"
    Tags        []string
    ContainerID string
    Since       time.Time
    Limit       int
    Search      string
}
```

The separation is deliberate — `LogFetchParams` documents which fields cost a network round-trip and which don't. Readers know without reading `logfetcher.go` that `Since` is approximate after a big `Limit` (since `Since` is applied post-fetch) and that `Facility` + `Tags` tighten before network transfer.

### 4.2 Request-building rules

- `ServiceID` → `serviceStackId=<uuid>` (required for meaningful response).
- `Severity` → `minimumSeverity=<0..7>` via `mapSeverityToNumeric`; empty or `"all"` = omit.
- `Facility` → `facility=16` (application) or `facility=17` (webserver); empty = omit.
- `Tags` → `tags=<csv>`; empty = omit.
- `ContainerID` → `containerId=<uuid>`; empty = omit.
- `Limit` → `limit=<clamped>`. Clamp: `<1` → `100` (default); `>1000` → `1000`.
- `desc=1` — always. We rely on "newest first" to pair with the tail-trim.

### 4.3 Post-fetch pipeline

Applied in exact order inside `FetchLogs`:

1. **Parse response items** into `[]LogEntry`.
2. **Sort ascending by timestamp** (string sort is fine for relative ordering within a single response — backend emits consistent precision per event, and the follow-up filter is parsed).
3. **Apply `Since`** via parsed `time.Time` comparison. Entries with unparseable timestamps are dropped (logged via debug, not promoted to an error — this is a forward-compatible default).
4. **Apply `Search`** via substring match on `Message`. Case-sensitive (same as upstream REST search semantics per zcli).
5. **Tail-trim to `Limit`** — keep the newest N entries.

Order is load-bearing: `Since`-before-`Limit` means Limit counts only eligible entries; `Search`-before-`Limit` means a restrictive search doesn't leak to older-than-Since entries.

### 4.4 Consumer contracts

```
FetchBuildWarnings(event, limit)
  ServiceID  = *event.Build.ServiceStackID
  Severity   = "warning"
  Facility   = "application"
  Tags       = ["zbuilder@" + event.ID]
  Limit      = limit            // 100 default from caller
  // No Since — tag identity is authoritative

FetchBuildLogs(event, limit)
  ServiceID  = *event.Build.ServiceStackID
  Severity   = ""               // include everything
  Facility   = "application"
  Tags       = ["zbuilder@" + event.ID]
  Limit      = limit

FetchRuntimeLogs(serviceID, containerCreationStart, limit)
  ServiceID  = serviceID
  Severity   = ""
  Facility   = "application"
  Since      = containerCreationStart    // anchors to THIS container's lifetime
  Limit      = limit
  // Tag doesn't apply — runtime logs don't carry a zbuilder@ tag
  // containerId would be ideal but we don't have the UUID surfaced today
```

`containerCreationStart` is plumbed from the event mapper (Phase 5). Until then, `FetchRuntimeLogs` falls back to `event.Build.PipelineFinish` as an approximation.

### 4.5 MCP `zerops_logs` tool contract

Input fields (unchanged schema):
- `serviceHostname` (required) — resolved to `ServiceID`.
- `severity` — mapped into `Severity`.
- `since` — parsed via `ops.parseSince`, passed as `LogFetchParams.Since`. Defaults to 1 hour ago on empty, as today.
- `limit` — clamped.
- `search` — passed as `LogFetchParams.Search`. Now actually filters.

No new fields added. Schema stays stable. The only user-visible change: `search` starts working.

### 4.6 Mock fidelity

`MockLogFetcher` today ignores every filter field. That makes every `FetchBuildWarnings` unit test a green-light regardless of whether the production code sets `Since` at all.

Upgraded contract:
- `WithEntries(entries)` stays.
- `FetchLogs` applies `Severity` (via a small severity-to-numeric map), `Facility`, `Tags` (AND semantics: entry must match at least one tag), `ContainerID`, `Since`, `Search`, `Limit` to the supplied entries before returning.
- Behaviour matches the real fetcher's post-fetch pipeline, minus the HTTP wire step. That way, a unit test at the consumer layer can assert "stale entries were dropped" with the same confidence as the live integration test.

### 4.7 Invariants added

- **I-LOG-1 — parsed-compare Since**: `logfetcher.go` and `MockLogFetcher` both implement `Since` via `time.Parse` + `!t.Before(since)`. A pinning test in `logfetcher_test.go` documents this. Regression test in `logfetcher_build_contract_test.go` documents why lex compare is wrong on live data.
- **I-LOG-2 — tag identity for build scoping**: `FetchBuildWarnings`/`FetchBuildLogs` set `Tags = ["zbuilder@" + event.ID]`. AST-scan contract test in `internal/ops/build_logs_contract_test.go` asserts this (no consumer may drop the tag set).
- **I-LOG-3 — facility=application for managed fetchers**: `FetchBuildWarnings`/`FetchBuildLogs`/`FetchRuntimeLogs` set `Facility="application"`. Contract test pins it.
- **I-LOG-4 — `Limit` clamped to [1,1000]**: `logfetcher.go` guards against both `<1` (defaults to 100) and `>1000` (clamps to 1000).

---

## 5. Phased Workstreams

Each phase has: **RED** (tests that fail without the change), **GREEN** (minimal change to pass), **VERIFY** (full test run). No phase starts until the previous is green.

### Phase 1 — Foundation: LogFetchParams + logfetcher fixes

**Files**
- `internal/platform/types.go` — extend `LogFetchParams`.
- `internal/platform/logfetcher.go` — query builder + post-fetch pipeline.
- `internal/platform/logfetcher_test.go` — 7 new tests.

**RED tests** (add first, must fail at HEAD):
1. `TestFetchLogs_SinceFilter_ParseCompare` — fixture with entries at `t`, `t+0.5s`, `t+2s`; `Since=t+0.3s`; assert only `t+0.5s` and `t+2s` returned. Current lex code drops `t+0.5s`.
2. `TestFetchLogs_FacilityQueryParam` — httptest asserts `facility=16` is sent when `Facility="application"`, omitted when empty.
3. `TestFetchLogs_TagsQueryParam` — asserts `tags=a,b` is sent for `Tags=["a","b"]`.
4. `TestFetchLogs_ContainerIDQueryParam` — asserts `containerId=uuid` sent.
5. `TestFetchLogs_SearchClientSide` — fixture with 3 messages ("foo", "foobar", "baz"); `Search="foo"`; assert 2 returned ("foo", "foobar"), "baz" excluded.
6. `TestFetchLogs_LimitClamp_Low` — `Limit=0` → 100 entries requested; `Limit=-5` → 100.
7. `TestFetchLogs_LimitClamp_High` — `Limit=5000` → server request `limit=1000`.

**GREEN change** (`logfetcher.go`):
- Clamp `Limit` in two places (request URL + tail trim).
- Emit `facility`, `tags`, `containerId` query params.
- Replace lex compare with parsed compare (drop malformed-timestamp entries silently).
- Add client-side `Search` substring filter after `Since`.

**VERIFY**: `go test ./internal/platform/... -count=1`.

### Phase 2 — Mock fidelity

**Files**
- `internal/platform/mock.go` — rewrite `MockLogFetcher.FetchLogs`.
- `internal/platform/mock_logfetcher_test.go` — new.

**RED tests**:
1. `TestMockLogFetcher_SinceFilter` — mock configured with mixed-fractional entries; caller sets `Since`; assert filter applied.
2. `TestMockLogFetcher_TagsFilter` — assert entries without matching Tag are dropped.
3. `TestMockLogFetcher_FacilityFilter` — assert facility filter.
4. `TestMockLogFetcher_SearchFilter` — assert substring match.
5. `TestMockLogFetcher_LimitTrim` — tail-trim.

**GREEN**: implement the same post-fetch pipeline in `MockLogFetcher`. The shared logic can live in a package-private helper `filterEntries(entries, params) []LogEntry` used by both real and mock — single-path (per CLAUDE.md convention).

**VERIFY**: mock tests pass; existing tests using mock that previously relied on "ignore params" behaviour may break → those are the ones P1.2 needed. Update them to pass `WithEntries` populations that match what the consumer should filter in/out. Fail loudly if any previously-passing test now fails — it's evidence the test was broken-but-passing.

### Phase 3 — Consumer migration

**Files**
- `internal/ops/build_logs.go` — update three fetchers.
- `internal/ops/build_logs_test.go` — update tests + new regression tests.
- `internal/ops/build_logs_contract_test.go` — new AST-scan contract test for I-LOG-2, I-LOG-3.
- `internal/tools/deploy_poll.go` — update limit from 20 to 100.

**RED tests**:
1. `TestFetchBuildWarnings_FiltersStaleByTag` — mock 3 entries: two tagged `zbuilder@NEW`, one tagged `zbuilder@OLD`; event has `ID="NEW"`; assert only the two NEW-tagged entries returned.
2. `TestFetchBuildWarnings_SetsApplicationFacility` — inspect the `LogFetchParams` passed by `FetchBuildWarnings` (via a recording mock). Assert `Facility="application"`.
3. `TestFetchBuildWarnings_DropsPipelineStartAnchor` — assert `Since` is zero (tag identity supersedes).
4. `TestFetchRuntimeLogs_AnchoredToContainerCreationStart` — mock 2 entries, one before and one after the passed `containerCreationStart`; assert filter.
5. `TestBuildLogsContract_TagsAlwaysSet` — AST scan of `build_logs.go`: `FetchBuildWarnings` and `FetchBuildLogs` must construct their `LogFetchParams` with a `Tags` entry derived from `event.ID`. Failure message cites spec anchor.

**GREEN**: rewrite the three fetchers per §4.4.

**VERIFY**: `go test ./internal/ops/... ./internal/tools/... -count=1`.

### Phase 4 — MCP `zerops_logs` tool honesty

**Files**
- `internal/tools/logs.go` — pass `Search` through (already does) — verify it now works end-to-end.
- `internal/tools/logs_test.go` — new test.
- `internal/ops/logs.go` — possibly default `Facility="application"` here to keep agent-facing surface minimal.

**RED tests**:
1. `TestZeropsLogsTool_SearchFilters` — integration-style: fixture returns 5 mixed entries; caller invokes tool with `search="error"`; assert only matching entries surfaced, `hasMore` honest.

**GREEN**: Because Phase 1 already implements client-side `Search`, this phase mostly verifies the plumbing is intact. May require nothing to pass if Phase 1 is correct.

**VERIFY**: `go test ./internal/tools/... ./internal/ops/... -count=1`.

### Phase 5 — Event mapper enrichment

**Files**
- `internal/platform/types.go` — extend `BuildInfo` with `ContainerCreationStart`, `StartDate`, `EndDate`, `CacheSnapshotId`, `ServiceStackName`.
- `internal/platform/zerops_event_mappers.go` — map them.
- `internal/platform/zerops_event_mappers_test.go` — new assertions.
- `internal/ops/build_logs.go` — `FetchRuntimeLogs` accepts `containerCreationStart time.Time`; callers thread it through.

**RED tests**:
1. `TestMapEsAppVersionEvent_MapsContainerCreationStart` — fixture with DTO values populated, assert RFC3339Nano mapping.
2. `TestFetchRuntimeLogs_UsesContainerCreationStartAsAnchor` — contract.

**GREEN**: straightforward field additions.

**VERIFY**: all tests green.

### Phase 6 — Docs, CLAUDE.md, eval scenario

**Files**
- `CLAUDE.md` — two new bullet points under Conventions.
- `docs/spec-workflows.md` — section on log-fetching invariants (I-LOG-1..4).
- `internal/eval/scenarios/deploy-warnings-fresh-only.md` — new scenario.
- `plans/logging-investigation.findings.md` — append DONE section.

**No tests added here** — content-only.

### Phase 7 — Close out

**Files**
- `plans/logging-refactor.md` — mark status `SHIPPED`.
- `plans/logging-investigation.findings.md` — final cross-links.
- `plans/friction-root-causes.md` — annotate P1.2 as superseded (or absorbed into this plan).

**Verification**: full `go test ./... -count=1 -short` + `ZCP_API_KEY=… go test ./internal/platform/ -tags=api -run TestAPI_LogBackend_ -count=1`.

---

## 6. Test Strategy

Four layers, each with purpose:

| Layer | Purpose | Authored in |
|---|---|---|
| Unit (`_test.go`) | Table-driven assertions on pure logic: filter semantics, query builders, mappers. Fast. | Phases 1, 2, 3, 4, 5 |
| Mock-based consumer (`_test.go`) | With upgraded `MockLogFetcher`, prove that consumers apply filters correctly. Fast. | Phases 3, 4 |
| Contract (`_test.go` `-tags=api`) | Pin live Zerops backend behaviour. Catch upstream drift. | Already shipped (Phases 1, 3 extend) |
| AST contract (`_contract_test.go`) | Prevent regression from "tag filter disappeared" / "facility disappeared". Zero-dependency AST scan, same pattern as `pair_keyed_contract_test.go`. | Phase 3 |

**Eval scenario** (`internal/eval/scenarios/deploy-warnings-fresh-only.md`):
- Preseed: service adopted, first deploy with `deployFiles` that produces a warning ("dist path not found").
- Step 1: run `develop` workflow to deploy — expect build warnings with the specific string.
- Step 2: commit a clean change, run `develop` again — assert the second deploy's `buildLogs` field does NOT contain the first deploy's warning substring.
- `forbiddenPatterns`: the first deploy's specific warning string.

---

## 7. Rollback

Each phase is its own commit (or small set of commits). Rollback granularity:

| Phase | Revert command | Consequence |
|---|---|---|
| 1 | `git revert <sha>` | `Since` goes back to broken lex compare; `search` stops working; facility omitted. Tests for 1 fail. |
| 2 | `git revert <sha>` | Unit tests regress to "blind pass"; live behaviour unaffected because `MockLogFetcher` is only used in tests. |
| 3 | `git revert <sha>` | Stale-warning bug returns. The `api`-tagged contract test `TestAPI_LogBackend_TagFilterWorks` stays green (it doesn't depend on consumer code). |
| 4 | `git revert <sha>` | `search` in `zerops_logs` goes back to broken. |
| 5 | `git revert <sha>` | `FetchRuntimeLogs` falls back to pre-enrichment anchor (e.g. no anchor at all in the transition if the new signature already landed in Phase 3). Cross-phase: only revert if Phase 3 revert is also acceptable. |
| 6 | `git revert <sha>` | Docs regress — no code impact. |

No phase is irreversible.

---

## 8. Definition of Done

- [x] **Phase 1** — 7 RED tests added → GREEN after logfetcher.go rewrite (parse-compare Since, facility/tags/containerId query builders, limit clamp, client-side Search). `internal/platform/logfetcher_test.go` 7 new tests all pass.
- [x] **Phase 2** — `MockLogFetcher.FetchLogs` now applies the same server-side-simulated (Severity, Facility, Tags, ContainerID) and client-side (Since, Search, Limit) filters as the real fetcher. 9 new mock-fidelity tests. Surfaced 5 blind tests across `ops/` and `tools/` — each updated with realistic fixtures.
- [x] **Phase 3** — `FetchBuildWarnings` and `FetchBuildLogs` scope by `Tags: []string{"zbuilder@" + event.ID}` + `Facility: "application"`; `FetchRuntimeLogs` takes a `containerCreationStart time.Time` parameter. 5 new RED tests + 1 AST contract test (`TestBuildLogsContract_UsesTagIdentityAndApplicationFacility`) pin I-LOG-2 and I-LOG-3.
- [x] **Phase 4** — `zerops_logs` tool defaults to `Facility: "application"` inside `ops.FetchLogs`, suppressing daemon noise without widening the tool schema. `search` now actually filters (the Phase 1 client-side substring path). 2 new tool-level tests.
- [x] **Phase 5** — `BuildInfo` extended with `ContainerCreationStart`, `StartDate`, `EndDate`, `CacheSnapshotID`, `ServiceStackName`, `ServiceStackTypeVersionID`. Mapper populates them (RFC3339Nano). `containerCreationAnchor` prefers `ContainerCreationStart`; falls back to `PipelineFinish` → `PipelineFailed` → `PipelineStart`. Mapper test + 6-case priority test.
- [x] **Phase 6** — `CLAUDE.md` Conventions gained two new bullets (parse-compare log time; tag identity for build scoping). Eval scenario `internal/eval/scenarios/deploy-warnings-fresh-only.md` authored with preseed script.
- [x] **Phase 7** — `plans/logging-refactor.md` marked shipped. Cross-links added to `plans/logging-investigation.findings.md` and `plans/friction-root-causes.md`.
- [x] `go test ./... -count=1 -short` green (modulo pre-existing `internal/knowledge` `active_versions.json` merge conflict — `UU` at session start).
- [x] `ZCP_API_KEY=... go test ./internal/platform/ -tags=api -run TestAPI_LogBackend_ -count=1` green against the live backend.

---

## 9. Evidence Index

- `plans/logging-investigation.findings.md` — live probe results, 2026-04-23.
- `internal/platform/logfetcher_build_contract_test.go` — 4 `api`-tagged tests.
- `internal/platform/logfetcher_probe_test.go`, `_probe2_test.go`, `_probe3_test.go` — one-shot diagnostics (build tag `probe`); not run in CI.
- Upstream DTO source: `~/go/pkg/mod/github.com/zeropsio/zerops-go@v1.0.18/dto/output/appVersionBuild.go`.
- Upstream CLI reference: `~/go/pkg/mod/github.com/zeropsio/zcli@v1.0.61/src/serviceLogs/handler_printLogs.go` — the `makeQueryParams` we're borrowing from.

---

## 10. Open Risks

**R-1 — Backend adds server-side time filter mid-refactor.**
Mitigation: `TestAPI_LogBackend_NoServerSideTimeFilter` fails immediately; we revisit before shipping.

**R-2 — Backend changes tag semantics.**
Mitigation: `TestAPI_LogBackend_TagFilterWorks` fails; freeze Phase 3 behind a kill-switch (`ZCP_LOG_TAG_FILTER=off` env var) if needed. Kept as emergency lever only — do not ship the env var preemptively.

**R-3 — Mock fidelity regressions surface hidden test failures.**
Mitigation: Phase 2 is designed to surface them. Any "this test was blind" discovery becomes a fix-up commit in Phase 2.

**R-4 — `Search` client-side filter changes "hasMore" semantics.**
Currently `ops.FetchLogs` requests `Limit+1` to detect `hasMore`. When `Search` filters out some of the extra entry, `hasMore` can become falsely `false`. Decision: document this as a known approximation. `hasMore` means "more unfiltered entries exist on the backend"; with Search applied, it becomes "at least Limit matching entries exist; there may or may not be more". Acceptable — agents treat `hasMore` as a hint.
