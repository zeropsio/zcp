# Logging Subsystem — Investigation Plan

> **Status**: Investigation / discovery document. Not an implementation plan — entry points + open questions + experiments to run before we decide what to change.
> **Relationship to `plans/friction-root-causes.md`**: that plan ships a narrow, surgical fix for stale build-warning leakage (P1.2 — client-side `Since` filter anchored to `event.Build.PipelineStart`). This document frames the **broader** logging story: what we know, what we don't, where the design is likely weak. The P1.2 fix is compatible with every direction this investigation takes.

---

## 1. Why a Separate Investigation

The stale-warning bug is one visible symptom. The logging layer sits at the intersection of three moving parts:

- **Zerops log backend** — persistent per-service-stack store, queried via a signed URL with a narrow filter surface (service stack id, severity, limit, search, desc).
- **ZCP log fetcher** (`internal/platform/logfetcher.go`) — client-side post-processing: sort, client-side `Since` filter, limit tail.
- **Consumers** — `FetchBuildWarnings`, `FetchBuildLogs`, `FetchRuntimeLogs` in `internal/ops/build_logs.go`; the MCP `zerops_logs` tool; event-driven build polling (`internal/ops/progress.go`).

None of these are currently covered by end-to-end tests that exercise real retention, truncation, or scale. The P1.2 fix handles the dominant failure mode (warnings from previous builds on the same build stack) but leaves several second-order questions unanswered. Those questions are worth answering before we decide whether to invest in a larger logging redesign or call the narrow fix sufficient.

---

## 2. What We Know (preserved findings, as of 2026-04-22)

These are things we've read out of the code. Live-test confirmations are noted inline; everything else is code-inferred and may be wrong in practice.

### 2.1 Data types

- `platform.AppVersionEvent` (`internal/platform/types.go:197-207`) carries:
  - `ID string` — unique per deploy event (per-build UUID).
  - `ProjectID`, `ServiceStackID`, `Source`, `Status`, `Sequence`.
  - `Build *BuildInfo` — optional pointer to build detail.
  - `Created string`, `LastUpdate string` — RFC3339Nano timestamps.
- `platform.BuildInfo` (`internal/platform/types.go:210-215`) carries:
  - `ServiceStackID *string` — the **build service-stack** id (distinct from target runtime stack).
  - `PipelineStart *string`, `PipelineFinish *string`, `PipelineFailed *string` — RFC3339Nano strings marking build-pipeline phases.
- Upstream SDK DTO `output.AppVersionBuild` exposes additional fields ZCP's mapper currently drops:
  - `ServiceStackName`, `ServiceStackTypeVersionId`
  - `ContainerCreationStart`, `StartDate`, `EndDate`
  - `CacheSnapshotId`
  - Relevant file: `internal/platform/zerops_event_mappers.go:85-106` (mapper we'd extend if we want the extra fields).
- `platform.LogFetchParams` (`internal/platform/types.go:164-170`):
  ```go
  type LogFetchParams struct {
      ServiceID string
      Severity  string       // error | warning | info | debug | all
      Since     time.Time    // client-side filter — see 2.3
      Limit     int
      Search    string
  }
  ```

### 2.2 What the log backend accepts

Request params built in `logfetcher.go:80-96`:
- `serviceStackId` — mandatory.
- `limit` — how many entries to return.
- `desc` — order (set to `1` for descending).
- `minimumSeverity` — syslog-style integer (`warning` → 4, i.e. levels ≤ 4 which is emergency..warning).
- `search` — substring match.

The endpoint URL prefix is obtained dynamically via `GetProjectLog` (`internal/platform/zerops_search.go:160-183`) which returns a signed `LogAccess.URL`. Auth is signed-URL-based rather than header-token-based.

What the backend does **not** accept:
- `since=` / `from=` / `to=` — **no server-side time filter.**
- `appVersionId=` / `buildId=` — **no build-scoped filter.** All filtering is by service-stack id.
- No cursor / pagination token surface visible from the code.

### 2.3 What the client-side fetcher does

`fetchServiceLogs` (`logfetcher.go:64-151`) applies post-processing:
1. Sort entries ascending by timestamp (line 130-132).
2. If `params.Since` is non-zero, drop entries with `e.Timestamp < sinceStr` (line 134-144). The comparison is **lexicographic on RFC3339Nano strings**, which is correct for well-formed timestamps but fragile if the backend ever emits a different format.
3. If `params.Limit` is set, tail-trim to `Limit` entries (line 146-148).

Trim ordering matters: `Since` applies before tail-trim, so `Limit` counts only post-`Since` entries. Good.

### 2.4 Build service-stack persistence

- Zerops category system enumerates `"BUILD"` as a first-class service-stack category (`types.go:46-52`). That implies the build stack is a durable entity with a stable UUID across builds, not an ephemeral per-deploy container.
- `FetchBuildWarnings` passes `*event.Build.ServiceStackID` as the log filter (`build_logs.go:44-48`). If the stack were ephemeral, logs from it wouldn't persist for later queries — which would contradict the observed "stale warnings" behavior.
- **Not live-verified**: we have not observed the build stack across two builds of the same service to confirm the UUID is identical. Worth doing (see §3 experiments).

### 2.5 Current consumers

- `FetchBuildLogs` (`build_logs.go:11-23`) — fetches all severities, no `Since`, used on BUILD_FAILED / PREPARING_RUNTIME_FAILED.
- `FetchBuildWarnings` (`build_logs.go:29-57`) — severity=warning, no `Since` in current code, used on successful deploys.
- `FetchRuntimeLogs` (`build_logs.go:63-75`) — fetches from runtime container stack, no `Since`, used on DEPLOY_FAILED (initCommand stderr).
- The MCP `zerops_logs` tool — separate surface, agent-invoked; log handler lives in `internal/tools/` (not yet read in this investigation).

### 2.6 The narrow fix being shipped (P1.2)

`FetchBuildWarnings` will be changed to parse `event.Build.PipelineStart` (when present) and pass it as `params.Since`, and widen `Limit` from 20 to 100 to reduce the chance of a chatty build exhausting the window with post-`Since` entries before older ones can be filtered out.

Details: see `plans/friction-root-causes.md` §2 P1.2.

This fix is tactical: it doesn't address `FetchBuildLogs`, `FetchRuntimeLogs`, or the `zerops_logs` tool. Those may or may not need similar treatment — this investigation will determine it.

---

## 3. Open Questions

Grouped by what kind of evidence they need.

### 3.1 Server-side behavior (needs live API experiments)

1. **Does the log backend have any implicit time bound on its result set?** If yes, what is it (last 24h? last 1000 entries?)? If no, does retention on the backend ever discard entries? This determines whether `Limit=100 + client-Since` is safe at scale or whether a chatty long-lived build stack can exhaust it.

2. **Is `event.Build.ServiceStackID` the same string across two consecutive builds of the same target service?** If yes, stale warnings are inherent and the `Since` filter is the only defense. If the id *does* change, the stale-warning bug may have a different root cause.

3. **What does the backend return when `limit` is, say, 1000?** Is the response truncated somewhere at the HTTP level? Is there an implicit cap we'd hit before `Limit`?

4. **Timestamp format stability** — does the backend ever emit a non-RFC3339Nano timestamp? The client-side `Since` lexicographic compare breaks silently if so.

5. **`minimumSeverity` enum** — `warning = 4` is the only mapping we've read. What integer does the backend accept for `error`, `info`, `debug`? Does it accept `error` as a string, or only an integer?

6. **`search` field semantics** — substring? Regex? Case sensitivity? Full-text index? Multiple terms?

### 3.2 Tool-surface behavior (needs code reading)

7. **`zerops_logs` tool** — what does its input schema look like? Does it expose `Since` / `Severity` / `Search` to the agent? Does its default behavior drown the agent in old logs?

8. **Runtime log freshness** — `FetchRuntimeLogs` runs against `result.TargetServiceID` (runtime container stack), not the build stack. Do runtime stacks also accumulate across deploys (and thus across container recreates), or does each deploy reset the log? A deploy = new container, but does the stack's log carry over?

9. **Managed service logs** — do DBs, caches, and object-storage expose logs through the same endpoint surface? Are they ever queried by ZCP? If yes, same filter questions apply.

10. **Build polling interaction** — `PollBuild` (`internal/ops/progress.go`) tracks the event. Does it also consume logs along the way, and if so through which path?

### 3.3 Consistency questions across consumers

11. `FetchBuildLogs` (on failure) has **no `Since` filter** in current code. Does the same stale-data risk apply? A failing build after a previous failed build on the same stack might show the old error. Need to verify.

12. `FetchRuntimeLogs` (on DEPLOY_FAILED) same question — does runtime log bleed across container recreates when the stack-id is stable?

13. Are there any code paths that fetch logs via direct HTTP or a different wrapper, bypassing `logfetcher.go`? Grep for `FetchLogs(` alternatives.

### 3.4 UX / agent-workflow questions

14. When does the agent actually need logs, and how much context is useful vs. noise? The current 20/50/100 limits are educated guesses — are they grounded in observed usage?

15. Is there value in exposing a structured "give me logs between build A and build B" query to agents directly, or is per-deploy scoping (what P1.2 does internally) the only useful shape?

16. Should we ever surface logs via the envelope / workflow status flow, or only on-demand via `zerops_logs`? Today the latter is the rule; any tactical exception?

---

## 4. Experiments to Run (ordered by cheapness)

### 4.1 Code-reading experiments (free, no API calls)

- **E1** — read `internal/tools/` for the `zerops_logs` handler. Answer: what does the agent-facing tool accept and how does it post-process? Populates §3.2 Q7.
- **E2** — grep `FetchLogs(` across the repo to confirm `logfetcher.go` is the only entry. Populates §3.3 Q13.
- **E3** — read `internal/ops/progress.go` to see if `PollBuild` consumes logs during polling. Populates §3.2 Q10.
- **E4** — read `internal/platform/zerops_event_mappers.go:85-106` to see what upstream DTO fields we drop that might help (e.g. `EndDate` as a better `PipelineFinish` anchor). Populates §2.1 inventory.

### 4.2 Live API experiments (uses test project — requires one deploy each)

- **E5** — deploy a trivial change to `appdev` (or a scratch service), capture `event.Build.ServiceStackID`. Deploy again. Compare the two `ServiceStackID` values.
  - **If identical** → confirms persistence, validates the P1.2 assumption live.
  - **If different** → stale-warning root cause is different; re-think.
- **E6** — with the known service-stack ID from E5, hit the log endpoint with `limit=1`, capture the oldest returned timestamp. Iterate with `limit=10`, `limit=100`, `limit=1000`. Answers §3.1 Q1 + Q3.
- **E7** — intentionally trigger a warning on one build (e.g. `deployFiles: - ./nonexistent` with a dangling path), then deploy a clean build. After the clean build, call `FetchBuildWarnings` with current (unfixed) code and with the P1.2 fix applied. Populates §3.3 Q11 and validates the fix in a real scenario.
- **E8** — deploy a build that crashes on runtime (`run.start: exit 1`), capture `FetchRuntimeLogs` output. Deploy again with a clean start, check if previous crash lines surface. Populates §3.2 Q8 + §3.3 Q12.
- **E9** — query the log endpoint with an invalid `minimumSeverity` integer (e.g. `99`, `-1`) to probe the enum bounds. Answers §3.1 Q5.

### 4.3 Stress / long-run experiments (more expensive)

- **E10** — let a build stack accumulate N builds (20+) over a day; then query warnings with the P1.2 fix. Measure whether `Limit=100` + `Since=PipelineStart` reliably returns only fresh warnings. Answers §3.1 Q1 at scale.

---

## 5. Candidate Improvements (to evaluate after experiments)

Framed as hypotheses. Each must be validated against experiment results before committing to implementation.

### 5.1 Consumer-side hardening

- Apply the same `Since=PipelineStart` pattern to `FetchBuildLogs` (on failure) if E7/E11 shows stale bleed. Straightforward if needed.
- Apply `Since` to `FetchRuntimeLogs` using a different anchor (container creation time? deploy event time?) if E8 shows stale runtime bleed.
- Standardize: a single helper `fetchLogsForBuild(ctx, event, severity, limit) []string` that wraps all three use cases with a consistent time window.

### 5.2 Richer event mapping

- Extend `zerops_event_mappers.go` to propagate `ContainerCreationStart`, `StartDate`, `EndDate`, `CacheSnapshotId` into `BuildInfo`. Motivation: better anchor options for filters; visibility into cache effectiveness.

### 5.3 `zerops_logs` tool UX

- If §3.2 Q7 reveals the tool has suboptimal defaults (e.g. returns everything without `Since`), consider adding `sinceBuildId=` or `sinceBuildStart=` convenience params that resolve to the right timestamp automatically.
- Structured output that includes log timestamps and severity so the agent can reason about freshness itself.

### 5.4 Upstream Zerops request

- File an upstream request for a server-side `since=` param. Client-side filter after full fetch is O(N) where N could grow unbounded. Only the backend can cap the scan.
- File a request for server-side `appVersionId=` filter. That would give us exact build scoping instead of time-range approximation.

### 5.5 Observability into our own log-fetching

- Log (ironically) the `Since` used, the total entries fetched pre-filter, and the total post-filter in a debug logger. Gives us telemetry to tune `Limit` widths per consumer.

### 5.6 Test coverage

- Unit tests for `logfetcher.go` client-side `Since` behavior with fuzz'd timestamp formats (covers §3.1 Q4).
- Integration test in `internal/eval/` that runs two deploys with different warning conditions and asserts freshness (corresponds to the `deploy-warnings-fresh-only` scenario in the friction root-cause plan, plus extended coverage for runtime logs).

---

## 6. What's Explicitly Out of Scope of This Investigation

- Logging of ZCP's own operations (structured logging, log levels, debug output). That's a separate concern from fetching platform logs.
- Recipe eval log capture — covered by the eval framework, separate subsystem.
- Observability / metrics generally. This document is narrowly about the fetch-and-surface path for Zerops platform logs.
- `zerops_events` tool — queries events, not logs. Different endpoint, different concerns.

---

## 7. Suggested Sequence When Picking This Up

1. Run E1–E4 (code reading, zero cost). Update §2 knowledge base with findings.
2. Run E5 first live experiment — confirms or invalidates the stale-warning root-cause assumption at the foundation. Decide: continue investigation or stop here if root cause is different.
3. Run E6–E9 in one session against the test project. Updates §3.1 Q1–Q5 and §3.3 Q11–Q12 with live data.
4. Based on results, pick candidate improvements from §5 that are justified. Write a follow-up implementation plan.
5. Upstream request filed separately (§5.4) — not dependent on the implementation plan.

The P1.2 tactical fix in the main friction plan is independent — it ships whether or not this investigation happens. It just might be refined or extended once we know more.

---

## 8. Evidence Index

Code references gathered during the initial investigation:

- `internal/platform/types.go:164-170` — `LogFetchParams`
- `internal/platform/types.go:197-207` — `AppVersionEvent`
- `internal/platform/types.go:210-215` — `BuildInfo`
- `internal/platform/types.go:46-52` — service-stack category enum (`BUILD`)
- `internal/platform/logfetcher.go:64-151` — `fetchServiceLogs` implementation
- `internal/platform/logfetcher.go:80-96` — backend query param construction
- `internal/platform/logfetcher.go:134-144` — client-side `Since` filter
- `internal/platform/zerops_search.go:160-183` — `GetProjectLog` signed URL endpoint
- `internal/platform/zerops_event_mappers.go:85-106` — event-to-BuildInfo mapper (drops upstream fields)
- `internal/ops/build_logs.go:11-57` — `FetchBuildLogs`, `FetchBuildWarnings`
- `internal/ops/build_logs.go:63-75` — `FetchRuntimeLogs`
- `internal/ops/progress.go` — `PollBuild` (not yet read in depth)
- Upstream DTO: `output.AppVersionBuild` — additional fields `ContainerCreationStart`, `StartDate`, `EndDate`, `CacheSnapshotId`, `ServiceStackName`, `ServiceStackTypeVersionId`

Open unknowns explicitly flagged in §3 as needing live verification.
