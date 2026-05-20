# Preload-hint in MCP-init static instructions, not (only) in atom corpus

**Surfaced**: 2026-05-06 — flow-eval verification of `bootstrap-tool-preload`
+ `develop-tool-preload` atoms shipped in commit ab2c1128 (suite
`20260506-120821`, scenario `classic-python-postgres-dev-only`).

The two new atoms were added to nudge agents toward batching deferred
zerops_* tool schemas via `ToolSearch query="select:tool-a,tool-b,..."`
on the first turn. The eval shows partial success and reveals a
structural cap: agents make ToolSearch calls BEFORE they hit
`zerops_workflow`, so the atom can't reach them in time.

## What the eval showed

Agent's actual ToolSearch sequence in the verification run:

1. `select:mcp__zerops__zerops_discover` — single, **before** atom render
2. `select:mcp__zerops__zerops_workflow` — single, **before** atom render
3. `select:zerops_import,zerops_process` — batched 2, **after** atom rendered (hint absorbed)
4. `select:zerops_deploy,zerops_dev_server,zerops_verify` — batched 3, develop-active boundary

Batching ratio went from ~0/3 calls in the original pgbench session
(2026-05-06 morning) to 2/4 calls in the verification session.
Improvement is real but capped: the FIRST two ToolSearch calls happen
before the agent ever invokes `zerops_workflow`, so the
`bootstrap-tool-preload` atom — which renders inside the
`zerops_workflow start workflow="bootstrap"` response — arrives too
late to influence them.

## Why deferred

`bootstrap-tool-preload` + `develop-tool-preload` ship a real
improvement and the fix below is a structural extension, not a
correction. The current atoms cover the second half of every session
(everything after the agent's first workflow call) and that's where
most ToolSearch calls happen anyway. Cap-fix lands when we see the
first-two-calls problem hurt a real eval — currently it's optimization,
not unblocking.

## Sketch

ZCP serves `mcp.ServerInstructions` at MCP init via
`internal/server/instructions.go`. That payload is delivered ONCE per
session at the very first server handshake, before any tool call.
That's the canonical "always-arrives-first" surface for content the
agent needs before its first ToolSearch.

Add a single 2-3 line block:

```
Tool preload (first-turn optimization)
=======================================
zerops_* MCP tools are deferred behind ToolSearch in some harnesses.
Batch-load with select:tool-a,tool-b,... rather than fetching one at a
time:

ToolSearch query="select:zerops_workflow,zerops_discover,zerops_import,zerops_deploy,zerops_verify,zerops_logs,zerops_events,zerops_dev_server"
```

The two existing atoms (bootstrap-tool-preload, develop-tool-preload)
become redundant on this path and can be deleted. Or kept as the
"reminder on workflow boundary if the agent ignored the init hint" —
double-coverage is mild but not wasteful.

Trade-off: instructions.go grows by ~3 lines, two atoms lose ~600
bytes from corpus rendering. Net negative on size.

## Trigger to promote

Either:

1. **Eval evidence** — another flow-eval session shows the first-two-
   calls problem materially affecting outcomes (e.g., agent gets
   confused about what tools are available because it called workflow
   without knowing discover existed). Currently zero observed.
2. **A neighboring instructions.go change** lands and we can fold this
   into the same commit cheaply.
3. **Harness behavior change** — Claude Code or another host ships a
   policy change that makes deferral more aggressive (more tools, or
   tools loaded later in the session) and the cap-fix becomes
   load-bearing.

If none of the above happen in the next ~3 months, this is probably
not worth shipping standalone. Atom-only coverage of the late-session
calls is good enough for the friction it was solving.

## Trigger to reject

Move to `rejected/` if:

- Claude Code (or whichever harness) stops deferring zerops_* tools.
  Then BOTH the atoms AND any instructions hint become no-ops.
- Or a different fix (e.g., consolidating 24 zerops_* tools into fewer
  umbrella tools) makes the deferral threshold irrelevant. The
  consolidation would be a much bigger change but addresses the same
  root.

## Update 2026-05-20

`classic-go-simple` retro (suite `20260520-171709`) confirms the cap-fix
problem persists post-Phase-3 atoms. Agent self-review verbatim:

> "The deferred tool loading is the biggest time sink. Every `zerops_*`
> tool requires a `ToolSearch` call before you can use it, and the
> guidance repeatedly tells you to batch-load them all in one `select:`
> call upfront. I didn't do that — I loaded them incrementally as I
> needed them, which cost me four separate ToolSearch round-trips across
> the session."

Same cap-pattern as the original 2026-05-06 verification: atom hint
arrives AFTER the agent's first ToolSearch round-trips, so even after
the atom renders, the early calls are already burnt. Four-round-trip
session in this run is concrete cost, not optimization.

Two earlier 2026-05-20 retros (`classic-bun-simple`, `classic-python-postgres-dev-only`)
did not flag this — they happened to batch-load. So the symptom is
intermittent (depends on agent's initial probe pattern), but recurring.

Still doesn't trip the "real eval evidence" promote bar by itself; the
ServerInstructions surface is the right home for next time anyone is
touching `internal/server/instructions.go`.

## Refs

- `internal/server/instructions.go` — current ServerInstructions
  payload (the surface this would extend)
- `internal/content/atoms/bootstrap-tool-preload.md` — sibling content
  atom (shipped, partial coverage)
- `internal/content/atoms/develop-tool-preload.md` — same
- `eval/behavioral/runs/20260506-120821/classic-python-postgres-dev-only/transcript.jsonl`
  — the empirical evidence for partial-batching
- Plan `flow-eval-followup-fixes-2026-05-03.md §A` (originating intent
  for the atom-based fix)
- Codex unified review (2026-05-06): finding-#2 was reclassified from
  "response-contract split" to "atom corpus gap" — this entry argues
  atom-corpus is also not the canonical home for content the agent
  needs PRE-first-call. The canonical home is server instructions.
