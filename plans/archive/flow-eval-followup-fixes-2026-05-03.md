# Plan — flow-eval follow-up: Tool batching hint + 502 warning context

Date: 2026-05-03
Source: behavioral eval suite `20260503-173119` (post-top3 fix; 4/6 ok)
Pressure-tested investigation: 3 parallel Explore agents (mechanism + warning gen + firehose root-cause)

## Scope

This plan addresses the two **HIGH-priority recurring frictions** still present in the
post-top3 suite. The **MEDIUM-priority "develop firehose v2" issue** is backlogged
(`plans/backlog/develop-response-atom-proliferation.md`) with diagnostic findings —
its root cause was rediagnosed during investigation and warrants more design work.

Out of scope (handled in separate places):
- Develop firehose v2 (atom proliferation across close-mode/env/strategy combinations) — backlog.
- `zerops_knowledge` per-deps filtering (recipe-team-adjacent, separate plan if needed).
- Bootstrap two-step handshake announcement — single-eval friction, defer.

## Goals

| Issue | Pre-state (suite 20260503-173119) | Post-state (cíl) |
|---|---|---|
| #3 Tool batching | 2/4 retros: "two extra round trips per tool" | Bootstrap + develop response advise `ToolSearch` batching upfront |
| #2 502 warning | 2/4 retros: "looks like deploy failure but isn't" | Dev-mode dynamic 502 warning names `zerops_dev_server start` as next step |

Final gate: re-run `flow-eval all`; the two specific friction patterns disappear from
retros (no "two extra round trips", no "looks like deploy failure"). Other carry-over
frictions (Go dev-server compile race, DB env var convention) are out of scope.

## Critical correction from investigation

**My initial Top 3 analysis stated** that `ToolSearch select:foo,bar,baz` syntax
"doesn't work in ZCP" — that conclusion was based on an Explore agent that
searched only inside the ZCP repo. **Reality**: `select:` is Claude Code's harness
ToolSearch syntax (documented in tool description), not a ZCP server feature.
Claude Code provides ToolSearch and supports `select:` filter — agents in retros
correctly identified the right batching syntax. The fix is teaching agents to
use it earlier, not implementing it server-side.

This correction is non-trivial: had I not investigated, I would have proposed
a server-side `select:` parser that's pure dead code.

---

## Phase A — Tool batching hint (Issue #3)

### A.1 Bootstrap-active atom: `bootstrap-tool-preload`

**What**: New atom in `internal/content/atoms/bootstrap-tool-preload.md`.

**Frontmatter**:
```yaml
---
id: bootstrap-tool-preload
priority: 1
phases: [bootstrap-active]
title: "Pre-load deferred tool schemas in one ToolSearch call"
---
```

**Body** (~10 lines, single rule):
- Tells agent: the standard bootstrap+develop sequence calls
  `zerops_workflow`, `zerops_discover`, `zerops_import`, `zerops_deploy`,
  `zerops_verify`, `zerops_logs`, `zerops_events`, `zerops_dev_server` (in container).
- Recommends: at the very first turn, batch-load schemas:
  `ToolSearch query="select:zerops_workflow,zerops_discover,zerops_import,zerops_deploy,zerops_verify,zerops_logs,zerops_events"`.
- Notes: `zerops_dev_server` is container-only; load it conditionally based on environment.

**Why frontmatter priority 1**: it's the cheapest possible action and saves multiple
round-trips downstream. Should render at top of bootstrap response.

### A.2 Develop-active atom: `develop-tool-preload`

**What**: Same shape, scoped to develop phase. Different tool list:
`zerops_workflow,zerops_deploy,zerops_verify,zerops_logs,zerops_events,zerops_manage,zerops_env`.
(Discover already loaded from bootstrap; subdomain rarely needed in develop.)

### A.3 Tests

- `TestAtomLint*` (existing) must still pass with new atoms.
- `TestScenarios_GoldenComparison` snapshot regen needed (atoms added → response shape).
- No new unit logic; pure content addition.

### Verification gate

- Lint + race + tests green.
- E2E: re-run flow-eval `classic-go-simple` (smallest scenario) — retro should mention
  the preload hint as a positive signal, not a friction.

---

## Phase B — 502 warning **subtractive root fix** (Issue #2)

**Reshaped 2026-05-03 after user pushback:** original Phase B kept the HTTP
probe and rewrote the warning text. User correctly pointed out that the
probe runs at all is the bug — the platform side enables L7 routing
correctly; 502 is the deferred-start runtime's steady state by design;
probing for HTTP readiness on something that's intentionally not serving
yet generates noise that has to be explained away. Subtractive fix removes
the source of friction instead of layering interpretation atop it.

### B.1 Diagnosis from investigation

Warning text generated at two sites:

1. `internal/tools/deploy_subdomain.go:108-109` (auto-enable path during deploy)
2. `internal/tools/subdomain.go:79-80` (explicit `zerops_subdomain action=enable` path)

Both compose: `"subdomain %s not HTTP-ready: %v (next zerops_verify may need to retry)"`.

**Critical context available** at both sites:
- `meta.Mode` (deploy_subdomain.go reads it for `modeAllowsSubdomain`;
  subdomain.go now looks it up via FindServiceMeta).
- Service type via `ops.LookupService` →
  `ServiceStackTypeInfo.ServiceStackTypeVersionName` (e.g. "nodejs@22").

### B.2 Fix — skip the probe entirely on deferred-start

**Predicate**: skip probe when `topology.IsDeferredStart(meta.Mode, runtimeClass)`
is true, i.e.:
- `meta.Mode in {ModeDev, ModeStandard}` (the dev half of any pair / standalone dev)
- AND runtime class is `RuntimeDynamic` (uses `zsc noop --silent`)

For `RuntimeStatic` (nginx auto-serves), `RuntimeImplicitWeb` (php-nginx
auto-starts), or stage / simple modes (run.start runs the real app),
the probe is meaningful and 502 is a genuine problem — keep probing,
keep the original warning.

**Implementation** (shipped in commit on HEAD):
- New `internal/topology/runtime_class.go` with `RuntimeClassFor(typeVersion)`
  (promoted from workflow's `classifyEnvelopeRuntime`) and `IsDeferredStart(mode, class)`.
- `internal/tools/deploy_subdomain.go::maybeAutoEnableSubdomain` reads service
  type after the eligibility lookup (refactored `serviceEligibleForSubdomain` to
  return `(*platform.ServiceStack, bool)`); composes `skipProbe` from existing
  already-enabled rule + new deferred-start rule.
- `internal/tools/subdomain.go::RegisterSubdomain` gains a `stateDir` parameter;
  new `skipDeferredStartProbe` helper looks up meta + service and gates the
  probe.
- `internal/server/server.go` passes `stateDir` to `RegisterSubdomain`.

**Boundary**: workflow-team scope. `internal/tools/` (layer 4) uses `topology` (layer 2)
predicates and `workflow.FindServiceMeta` (layer 3). No cross-layer issues.

### B.3 Tests (RED → GREEN, all shipped green)

Topology layer (`internal/topology/runtime_class_test.go`):
- `TestRuntimeClassFor` — table over 15 type strings.
- `TestIsDeferredStart` — table over 12 (mode, class) pairs.

Auto-enable path (`internal/tools/deploy_subdomain_test.go`):
- `TestMaybeAutoEnable_DevModeDynamic_SkipsHTTPProbe` — 0 probe calls, 0 warnings.
- `TestMaybeAutoEnable_StandardModeDynamic_SkipsHTTPProbe` — same for standard pair dev half.
- `TestMaybeAutoEnable_DevModeStatic_StillProbes` — nginx in dev mode still probes.
- `TestMaybeAutoEnable_DevModePHPNginx_StillProbes` — php-nginx still probes.
- `TestMaybeAutoEnable_StageDynamic_StillProbes` — stage mode still probes.
- `TestMaybeAutoEnable_NoMeta_DynamicType_StillProbes` — fail-closed when no meta.

Explicit-enable path (`internal/tools/subdomain_test.go`):
- `TestSubdomainTool_Enable_DeferredStart_SkipsProbe` — 502 stub never warns.
- `TestSubdomainTool_Enable_DeferredStart_StaticStillProbes` — nginx 502 still warns.

### Verification gate

- ✅ `go test ./... -short` green.
- ✅ `go test ./internal/topology/ ./internal/workflow/ ./internal/tools/ -race` green.
- ✅ `make lint-fast` green (0 issues).
- 🟡 E2E: flow-eval re-run pending (deferred until Phases for issues 3+4 ship).

---

## Sequencing

Both phases are independent. Recommend:
1. **Phase B first** (code change + tests in 1-2h, surgical). Lower-risk, immediately
   actionable; no atom golden churn.
2. **Phase A second** (content addition, regenerate atom goldens once for the suite).
3. **Single E2E run** after both ship.

If we want minimal context-switching: do them together as one commit per phase, two commits total.

---

## Risk register

| Risk | Likelihood | Mitigation |
|---|---|---|
| `select:` syntax doesn't work for some tool name | Low | Verify by hand-running ToolSearch with the exact list before committing atom. |
| Tool list drifts (rename, addition) | Medium | Keep atom list short — name only the always-needed core. Less-frequent tools (zerops_recipe, zerops_export) excluded. |
| Runtime classifier duplication | Low | Mark with comment; if duplication grows, factor to `topology` package later. |
| Static-runtime regression | Low | New test `TestMaybeAutoEnable_DevModeStatic_502_NoSpuriousHint` pins this. |
| Atom golden snapshot churn | High but planned | Regenerate via `ZCP_UPDATE_ATOM_GOLDENS=1` once. |

## Boundary

- **Phase A** — workflow-team scope (atom corpus content).
- **Phase B** — workflow-team scope (`internal/tools/` deploy + subdomain handlers, `internal/topology/` enum).
- No recipe-team touching.

## Success criteria

1. `flow-eval all` re-run: 0 retros mention "two extra round trips" friction;
   0 retros mention "looks like deploy failure" for dev-mode dynamic.
2. `make lint-local` green; `go test ./... -race` green.
3. Atom-corpus lint passes.
