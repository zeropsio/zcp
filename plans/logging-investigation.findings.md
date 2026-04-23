# Logging Subsystem — Investigation Findings (2026-04-23)

> **Status**: ✅ Evidence acted on in full — see `plans/logging-refactor.md`
> for the shipped refactor. This document is preserved as the frozen
> snapshot of what was found on 2026-04-23 before any code moved. Any
> live-backend claim below that needs re-verification is covered by the
> permanent `api`-tagged contract tests at
> `internal/platform/logfetcher_build_contract_test.go`.

---

## 0. Artifacts Produced in This Session

| File | Purpose | Build tag |
|---|---|---|
| `internal/platform/logfetcher_probe_test.go` | One-shot probe of backend query params (facility, severity, search, since, limit bounds). | `probe` |
| `internal/platform/logfetcher_probe2_test.go` | Build-stack probe + timestamp precision + cursor probes. | `probe` |
| `internal/platform/logfetcher_probe3_test.go` | Focused probe: tag filters, `from=<id>` cursor semantics. | `probe` |
| `internal/platform/logfetcher_build_contract_test.go` | Three permanent contract tests pinning backend behaviour. | `api` |

Run probes: `ZCP_API_KEY=… go test ./internal/platform/ -tags=probe -v -count=1`
Run contracts: `ZCP_API_KEY=… go test ./internal/platform/ -tags=api -run TestAPI_LogBackend_ -v -count=1`

All three contract tests pass against the live backend as of 2026-04-23.

---

## 1. The Five Load-Bearing Findings

### F1. Server-side time filter **does not exist**

`since=`, `from=`, `fromDate=`, `timestamp=` are all silently ignored. A probe
against the 2999-01-01 future timestamp returns the same items as baseline. A
1970-01-01 past timestamp returns the same items as baseline.

**Consequence**: client-side Since filtering is our only defense. Every other
approach (upstream feature request, server-side cursor, pagination token) needs
to be filed as an external ask, not implemented against hypothetical server
support.

**Pinned by**: `TestAPI_LogBackend_NoServerSideTimeFilter`.

### F2. `tags=<tag>` **IS** a server-side filter, and it's clean

The backend honours `tags=zbuilder@<appVersionId>` exactly. Matching tag returns
only that build's entries; bogus tag returns zero items.

Every entry on the build service-stack has the tag `zbuilder@<appVersionId>` —
no non-builder entries leak onto this stack. So the tag filter alone is
sufficient to scope a query to a single build, with no time window needed.

**Consequence**: this is a **fundamentally better fix than P1.2 as specified**.
Per-build scoping via identity beats per-build scoping via time range. No
sub-second edge cases, no reliance on PipelineStart being populated, no client-
side work.

**Pinned by**: `TestAPI_LogBackend_TagFilterWorks`.

### F3. Current lex-compare Since filter **is broken** at sub-second boundaries

`logfetcher.go:136` does `sinceStr := params.Since.Format(time.RFC3339)` then
compares against on-wire timestamps lexicographically. Real timestamps come back
with 4–9 fractional digits (never fixed-width); `Format(time.RFC3339)` drops
fractional. Comparisons fail because ASCII `Z` (0x5A) > `.` (0x2E):

- Entry `2026-04-22T06:04:29Z` (no fractional) lex-compares as **after**
  `2026-04-22T06:04:29.440767629Z` — wrong.
- Entry `2026-04-22T06:04:29.9Z` (0.9s, semantically after PipelineStart at
  0.44s) lex-compares as **before** the no-fractional `sinceStr` — dropped.
- Entry 1ns after PipelineStart lex-compares as before the no-fractional
  `sinceStr` — dropped.

Swapping to `Format(time.RFC3339Nano)` does **not** fully fix this: when the
entry happens to have fewer fractional digits than Since (e.g. entry `...:29Z`
vs sinceStr `...:29.440767629Z`), the entry still lex-compares as after.

**The only correct implementation is: parse both sides to `time.Time` and
compare with `time.Before` / `time.After`.**

**Consequence**: even if we keep the time-filter approach (see §2), the
comparison must be rewritten. Until it is, the filter introduces silent
dropping of legitimate post-Since entries — the opposite of what it's meant to
accomplish.

**Pinned by**: `TestAPI_LogBackend_LexCompareFailsAtSubSecondBoundaries`.

### F4. `search=` parameter **is silently ignored**

Both `search=sshfs` (all entries match) and `search=nonexistent-xyz` return the
baseline bytes-identical response of 5 items / 2504 bytes. The log backend
does not implement content search on the REST endpoint.

**Consequence**: the `zerops_logs` MCP tool at `internal/tools/logs.go:17`
exposes a `search` JSON-schema parameter that is a no-op. Agents that rely on
it to narrow down logs get misleading answers.

**Action**: either remove `search` from the tool schema, or implement it as a
client-side substring filter inside `logfetcher.go` (trivial — apply it after
sort, before Limit trim).

### F5. `facility` **is** a real filter, and we never set it

Without `facility` the backend returns the full mixed stream — including
`facility=3` daemon noise (sshfs mount errors, systemd-networkd timeouts) that
has nothing to do with the build or the application.

With `facility=16` (APPLICATION / local0), only application logs return.
Zcli always sets `facility=16` for `zcli service log`. ZCP never does.

**Consequence**: `FetchBuildWarnings` today can surface system-daemon
errors as "build warnings", confusing the agent. On our probe project the
build stack only carried `zbuilder@…` tags — but when combined with `severity=warning`
on any stack with active daemon noise, the result includes warnings that aren't
from the build.

On the runtime stack this is worse: `FetchRuntimeLogs` returns sshfs mount
failures unrelated to the user's app.

---

## 2. What This Means for P1.2 in `friction-root-causes.md`

The shipped plan text:

```go
params := platform.LogFetchParams{
    ServiceID: *event.Build.ServiceStackID,
    Severity:  "warning",
    Limit:     100,
}
if event.Build.PipelineStart != nil {
    if t, err := time.Parse(time.RFC3339Nano, *event.Build.PipelineStart); err == nil {
        params.Since = t
    }
}
```

Correct in spirit. Two concrete problems:

1. **The Since filter that it relies on is currently lex-broken** (F3). Either
   the fix must land in `logfetcher.go:136` in the same change, or the change
   is silently partial.

2. **It does not address the facility/tag surface** — still returns daemon
   noise (F5), still operates on a wider surface than "this build's logs" (F2).

### Recommended upgrade path

**Step 1** (minimal, unblocks the friction-root-causes plan):

- In `logfetcher.go`, replace the lex compare with `time.Parse` + `Before`:
  ```go
  if !params.Since.IsZero() {
      filtered := entries[:0]
      for _, e := range entries {
          et, err := time.Parse(time.RFC3339, e.Timestamp)
          if err != nil {
              continue // skip malformed
          }
          if !et.Before(params.Since) {
              filtered = append(filtered, e)
          }
      }
      entries = filtered
  }
  ```
- Keep P1.2's `params.Since = PipelineStart` + `Limit=100` change.
- Pin with a unit test that mocks entries with mixed fractional precision.
- The existing `TestAPI_LogBackend_LexCompareFailsAtSubSecondBoundaries`
  continues to serve as a reminder for why the comparator must stay parsed.

**Step 2** (more robust, subsumes P1.2 cleanly):

- Promote `tags` to a first-class `LogFetchParams` field.
- `FetchBuildWarnings` sets `Tags=[]string{"zbuilder@" + event.ID}` and
  `Facility="application"` instead of relying on Since. No client-side filter
  needed because the server-side tag filter is authoritative and exact.
- Remove `Since` from this code path since tag identity is stronger.
- Keep the `Since` plumbing for the `zerops_logs` MCP tool where agents pass a
  relative duration — that path still needs the fix from Step 1.

The two steps are compatible and can land in sequence. Step 2 is the ideal end
state; Step 1 is the minimum to make P1.2's claim true.

### What else needs the same treatment

`FetchBuildLogs` (`build_logs.go:11`) — runs on build failure, no time filter.
If two builds fail consecutively on the same build stack, the second failure's
log fetch will include the first failure's output. Same fix applies: either
anchor by Since (Step 1) or scope by tag (Step 2).

`FetchRuntimeLogs` (`build_logs.go:63`) — runs on DEPLOY_FAILED. The runtime
stack is persistent across deploys; stale runtime crashes bleed in. Tag doesn't
apply here — runtime logs don't carry a `zbuilder@` tag. Options:
- Anchor by Since using `event.Build.PipelineFinish` (time the container most
  recently started) — needs Step 1 fix to work correctly.
- Add `containerId=<uuid>` filter if we can surface the runtime container UUID.
  Needs upstream mapper extension (`output.EsContainer.Id`) and a way to
  resolve "current container for service X after deploy Y".

---

## 3. Concrete Code Changes Worth Making

Ordered by confidence and blast radius.

### Change 1 — Fix the lex-compare bug (1 file, ~6 LOC)

`internal/platform/logfetcher.go:134-144` — replace string compare with
parsed compare. Add unit test with synthetic entries at variable fractional
widths. Pin with `TestAPI_LogBackend_LexCompareFailsAtSubSecondBoundaries`.

### Change 2 — Ship P1.2 as specified, with Change 1 as prerequisite

`internal/ops/build_logs.go:29-57` — add Since from PipelineStart, widen Limit
to 100. RED tests already specified in `friction-root-causes.md` §2 P1.2.

### Change 3 — Remove no-op `search` from zerops_logs (or implement it client-side)

`internal/tools/logs.go:17` — either remove the `Search` field from `LogsInput`
and stop threading it to `LogFetchParams`, or implement client-side substring
match in `logfetcher.go` between sort and Limit trim. Client-side is a 5-LOC
addition and preserves the schema promise.

Recommended: **implement client-side**, since users will likely pass narrow
search terms expecting the behaviour the schema advertises.

### Change 4 — Plumb `facility` (and later `tags`) through LogFetchParams

`internal/platform/types.go:171` — extend `LogFetchParams`:

```go
type LogFetchParams struct {
    ServiceID string
    Severity  string
    Since     time.Time
    Limit     int
    Search    string
    Facility  string   // NEW: "application" | "webserver" | ""  (empty = no filter)
    Tags      []string // NEW: server-side tag CSV filter
}
```

Wire `facility=16` for `application`, `facility=17` for `webserver`.
`FetchBuildWarnings` and `FetchBuildLogs` set `Facility="application"`.
`FetchRuntimeLogs` sets `Facility="application"` (runtime app logs).

### Change 5 — Switch `FetchBuildWarnings` to tag identity

After Change 4, `build_logs.go` becomes:

```go
func FetchBuildWarnings(ctx, ..., event, limit) []string {
    return fetcher.FetchLogs(ctx, logAccess, LogFetchParams{
        ServiceID: *event.Build.ServiceStackID,
        Severity:  "warning",
        Limit:     limit,
        Facility:  "application",
        Tags:      []string{"zbuilder@" + event.ID},
    })
}
```

No client-side Since filter, no PipelineStart dependency, no mixed-facility
noise. Previous builds' warnings physically cannot match the tag filter.

### Change 6 — Extend event mapper to surface dropped upstream fields

`internal/platform/zerops_event_mappers.go:84-106` — map `ContainerCreationStart`,
`StartDate`, `EndDate`, `CacheSnapshotId`, `ServiceStackName`. These unlock:
- Better anchors for `FetchRuntimeLogs` time filter (`ContainerCreationStart`).
- Visibility into cache effectiveness.
- Better build-event messages that name the build stack.

Not urgent. Do when something consumes them.

### Change 7 — Bound `LogFetchParams.Limit` defensively

Probe showed the backend silently returns empty at `limit=50000`. ZCP has no
upper bound, so a buggy caller (or a future agent) could request enormous
limits that return nothing and look like a bug. Clamp to `[1, 1000]` matching
zcli convention.

---

## 4. What NOT to Do

- **Don't file upstream for a `since=` server param yet.** If we go tag-based
  (Change 5) we don't need it. Only file if we hit a case tag doesn't cover.
- **Don't add `from=<msgId>` cursor support.** Our MCP tool semantics are
  stateless — we don't have a convenient place to remember `lastMsgId`. The
  cursor is useful for WebSocket stream reconnection (which we don't use).
- **Don't adopt the `/stream` WebSocket endpoint.** Our deploy flow is a
  polling SearchAppVersions cadence; adding a second stream would duplicate
  state, complicate error handling, and muddle the lifecycle. Keep the single
  polled REST surface.
- **Don't add a separate facility filter to the `zerops_logs` MCP tool.**
  Agents shouldn't need to know about syslog facilities. Always set `facility=16`
  from inside `FetchLogs` for application logs unless a specific `containerId`
  or runtime-stack request overrides.

---

## 5. Updated Answers to `plans/logging-investigation.md` Open Questions

Numbered per that document's §3.

1. **Server-side time bound on result set?** No. Client-side only. (F1)
2. **`event.Build.ServiceStackID` identical across builds?** Yes — build stack
   is persistent (documented category `BUILD`). Confirmed by mapper +
   `zerops_event_mappers.go`. A second probe with a multi-build project would
   pin this live; the mechanism is clear enough to proceed.
3. **What does `limit=1000` return?** 1000 items, ~500KB. `limit=5000` returns
   5000 items, ~2.5MB. `limit=50000` returns 0 (silent truncation). No HTTP
   errors. Backend is forgiving but not unbounded.
4. **Timestamp format stability?** Variable fractional precision (4–9 digits
   observed). The only safe comparison is parsed. (F3)
5. **`minimumSeverity` enum?** 0–7 (syslog). Invalid values (99, -1) silently
   become no-filter. `0` (emergency) returns zero items as expected.
6. **`search` semantics?** Silently ignored on the REST endpoint. (F4)
7. **`zerops_logs` tool surface?** Exposes `Severity` (honoured),
   `Since` (honoured client-side with lex bug), `Limit` (honoured),
   `Search` (no-op). Agent's default is "last 1 hour, 100 entries".
8. **Runtime log freshness?** Runtime stack is persistent; stale crashes bleed
   across container recreates. Same mechanism as build stack. Needs Since or
   containerId filter.
9. **Managed service logs?** Not probed — worth a follow-up. If they go
   through the same endpoint, all above findings apply.
10. **Build polling interaction?** `PollBuild` does not consume logs. Logs are
    fetched once after the build terminates, by `deploy_poll.go:55,79,84`.
11. **`FetchBuildLogs` stale-risk?** Yes — same mechanism as warnings. Needs
    Change 1+2 or Change 5.
12. **`FetchRuntimeLogs` stale-risk?** Yes — same mechanism. Needs
    Since+ContainerId-based anchor.
13. **Other FetchLogs entry points?** No. `logfetcher.go` is the single entry.
14. **Agent log usage value/noise tradeoff?** 100-entry default with
    application-facility filter + tag filter gives the agent actionable signal
    without daemon noise. 20 was too tight; 1000 is too large for context
    efficiency.
15. **"Logs between build A and build B" query shape?** Covered by combining
    `serviceStackId=<build-stack>` with `tags=zbuilder@A` and `tags=zbuilder@B`
    separately. Not needed as a primitive.
16. **Envelope vs on-demand?** Keep on-demand via `zerops_logs`. The
    `FetchBuildWarnings` surface in deploy results is the one tactical
    exception and is already working that way.

---

## 6. Regression Coverage Gaps

### Unit gap
`platform.MockLogFetcher.FetchLogs` returns `f.entries` unconditionally,
ignoring `params.Since` / `Severity` / `Limit`. This means every existing
`FetchBuildWarnings` unit test passes regardless of what params the code
under test sets. P1.2's RED phase needs either:
- A new mock that actually applies `params.Since` / `Severity` filtering, or
- Inlined real sort+filter logic in the test (exercising `logfetcher.go`
  directly via `httptest.NewServer`) — cleaner, avoids mock drift.

### Integration gap
`internal/eval/scenarios/` has no scenario that deploys twice consecutively
and checks warning freshness. The plan lists `deploy-warnings-fresh-only.md`
as to-be-authored. When that scenario lands, the `ThreeApproachesCompared`
style comparison can move from `api`-tagged platform test to a proper eval
fixture.

### Platform-contract gap (filled by this investigation)
The three new `api`-tagged contract tests protect against:
- Zerops shipping a server-side time filter without us noticing (test 1).
- Zerops changing tag semantics (test 2).
- ZCP reintroducing lex-compare after Change 1 (test 3).

These run against the live project at cost of a few HTTP calls. Worth gating
in CI behind `ZCP_API_KEY` the same way existing `api` tests are.

---

## 7. Recommended Priority

For the `friction-root-causes.md` workstream:
1. **Change 1** (fix lex compare in `logfetcher.go`) — prerequisite for P1.2.
2. **Change 2** (ship P1.2 as specified, with Change 1 under it).
3. **Change 3** (remove/implement `search`) — independent; fixes a user-visible
   surface lie.
4. **Change 4 + 5** (facility + tag-based filter for build warnings) — future
   plan, supersedes the Change 2 time-filter when ready.
5. **Change 6 + 7** (richer event mapping, defensive limit bound) — nice-to-have.

Changes 1–3 fit inside the friction-root-causes P1 workstream. Changes 4–7
are their own small plan that can follow after the friction work merges.
